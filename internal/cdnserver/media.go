package cdnserver

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"image"
	_ "image/jpeg"
	"image/png"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"

	"github.com/yomezza/cosmosimulator/internal/assetpack"
	"github.com/yomezza/cosmosimulator/internal/platform/fileatom"
	"github.com/yomezza/cosmosimulator/internal/platform/httpkit"
	"github.com/yomezza/cosmosimulator/internal/platform/identifiers"
	"github.com/yomezza/cosmosimulator/internal/platform/mediastore"
	_ "golang.org/x/image/webp"
)

var errCorruptMedia = errors.New("media checksum mismatch")

type mediaFiles struct {
	root      *os.Root
	directory string
	mu        sync.Mutex
	slots     chan struct{}
}

func mediaObject(id string) string  { return "private/guide/objects/" + id[:2] + "/" + id + ".png" }
func mediaReceipt(id string) string { return "private/guide/receipts/" + id[:2] + "/" + id + ".json" }
func (m *mediaFiles) inspect(id string) (mediastore.Receipt, error) {
	var out mediastore.Receipt
	raw, err := m.root.ReadFile(mediaReceipt(id))
	if err != nil {
		return out, err
	}
	if json.Unmarshal(raw, &out) != nil || out.ID != id || !assetpack.ValidHash(out.SHA256) {
		return out, errors.New("invalid receipt")
	}
	if out.Deleted {
		return out, os.ErrNotExist
	}
	f, err := m.root.Open(mediaObject(id))
	if err != nil {
		return out, err
	}
	defer f.Close()
	h := sha256.New()
	n, err := io.Copy(h, io.LimitReader(f, mediastore.Limit+1))
	if err != nil || n != out.Size || hex.EncodeToString(h.Sum(nil)) != out.SHA256 {
		return out, errCorruptMedia
	}
	return out, nil
}

type boundedWriter struct {
	w    io.Writer
	left int64
}

func (b *boundedWriter) Write(p []byte) (int, error) {
	if int64(len(p)) > b.left {
		return 0, errors.New("encoded image too large")
	}
	n, e := b.w.Write(p)
	b.left -= int64(n)
	return n, e
}
func (m *mediaFiles) upload(w http.ResponseWriter, r *http.Request, id string) {
	select {
	case m.slots <- struct{}{}:
		defer func() { <-m.slots }()
	default:
		httpkit.Fail(w, 429, "media processing busy")
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, mediastore.Limit)
	raw, err := io.ReadAll(r.Body)
	if err != nil {
		httpkit.Fail(w, 413, "image too large")
		return
	}
	inputHash := assetpack.Hash(raw)
	cfg, format, err := image.DecodeConfig(bytes.NewReader(raw))
	if err != nil || (format != "png" && format != "jpeg" && format != "webp") {
		httpkit.Fail(w, 415, "PNG, JPEG or WebP required")
		return
	}
	if cfg.Width < 1 || cfg.Height < 1 || cfg.Width > 4096 || cfg.Height > 4096 || bytes.Contains(raw, []byte("acTL")) || bytes.Contains(raw, []byte("ANIM")) {
		httpkit.Fail(w, 422, "static image up to 4096x4096 required")
		return
	}
	decoded, actual, err := image.Decode(bytes.NewReader(raw))
	if err != nil || actual != format {
		httpkit.Fail(w, 422, "corrupt image")
		return
	}
	var output bytes.Buffer
	if err = png.Encode(&boundedWriter{&output, mediastore.Limit}, decoded); err != nil {
		httpkit.Fail(w, 413, "encoded image too large")
		return
	}
	receipt := mediastore.Receipt{ID: id, SHA256: assetpack.Hash(output.Bytes()), InputSHA256: inputHash, Size: int64(output.Len()), Width: cfg.Width, Height: cfg.Height, SourceFormat: format}
	m.mu.Lock()
	defer m.mu.Unlock()
	if oldRaw, e := m.root.ReadFile(mediaReceipt(id)); e == nil {
		var old mediastore.Receipt
		if json.Unmarshal(oldRaw, &old) != nil || old.Deleted || (old.InputSHA256 != "" && old.InputSHA256 != inputHash) || old.SHA256 != receipt.SHA256 {
			httpkit.Fail(w, 409, "operation already used")
			return
		}
	}
	// A crash between object and receipt is repaired only by the identical bytes.
	if old, e := m.root.ReadFile(mediaObject(id)); e == nil && assetpack.Hash(old) != receipt.SHA256 {
		httpkit.Fail(w, 409, "object already exists")
		return
	}
	for _, p := range []string{mediaObject(id), mediaReceipt(id)} {
		if err = m.root.MkdirAll(filepath.ToSlash(filepath.Dir(p)), 0700); err != nil {
			httpkit.Fail(w, 503, "storage unavailable")
			return
		}
	}
	path := filepath.Join(m.directory, filepath.FromSlash(mediaObject(id)))
	if err = fileatom.WriteStaged(path, output.Bytes(), 0600, filepath.Join(m.directory, "staging", "uploads")); err != nil {
		httpkit.Fail(w, 503, "storage unavailable")
		return
	}
	body, _ := json.Marshal(receipt)
	if err = fileatom.WriteStaged(filepath.Join(m.directory, filepath.FromSlash(mediaReceipt(id))), body, 0600, filepath.Join(m.directory, "staging", "uploads")); err != nil {
		httpkit.Fail(w, 503, "receipt unavailable")
		return
	}
	httpkit.Reply(w, 200, receipt)
}
func (s *Service) internalMedia(w http.ResponseWriter, r *http.Request) {
	if !mediastore.Authorized(r, s.token) {
		httpkit.Fail(w, 403, "service authentication required")
		return
	}
	id := strings.ToLower(r.PathValue("id"))
	if !identifiers.ValidUUID(id) {
		http.NotFound(w, r)
		return
	}
	switch r.Method {
	case "PUT":
		s.media.upload(w, r, id)
	case "GET":
		receipt, err := s.media.inspect(id)
		if err != nil {
			status := 503
			if os.IsNotExist(err) {
				status = 404
			} else if errors.Is(err, errCorruptMedia) {
				status = 422
			}
			httpkit.Fail(w, status, "media unavailable")
			return
		}
		httpkit.Reply(w, 200, receipt)
	case "DELETE":
		s.media.mu.Lock()
		defer s.media.mu.Unlock()
		raw, err := s.media.root.ReadFile(mediaReceipt(id))
		if os.IsNotExist(err) {
			httpkit.Reply(w, 200, mediastore.Receipt{ID: id, Deleted: true})
			return
		}
		var receipt mediastore.Receipt
		if err != nil || json.Unmarshal(raw, &receipt) != nil || receipt.ID != id {
			httpkit.Fail(w, 503, "storage unavailable")
			return
		}
		if r.URL.Query().Get("sha256") != receipt.SHA256 {
			httpkit.Fail(w, 409, "checksum mismatch")
			return
		}
		receipt.Deleted = true
		body, _ := json.Marshal(receipt)
		if err = fileatom.Write(filepath.Join(s.store.Directory, filepath.FromSlash(mediaReceipt(id))), body, 0600); err != nil {
			httpkit.Fail(w, 503, "storage unavailable")
			return
		}
		if err = s.media.root.Remove(mediaObject(id)); err != nil && !os.IsNotExist(err) {
			httpkit.Fail(w, 503, "storage unavailable")
			return
		}
		httpkit.Reply(w, 200, receipt)
	default:
		w.WriteHeader(405)
	}
}
func (s *Service) privateMedia(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if !identifiers.ValidUUID(id) {
		http.NotFound(w, r)
		return
	}
	request, err := http.NewRequestWithContext(r.Context(), "GET", s.config.CosmicURL+"/internal/cdn/v1/media/"+id+"/authorize", nil)
	if err != nil {
		httpkit.Fail(w, 503, "authorization unavailable")
		return
	}
	request.Header.Set(mediastore.Header, s.token)
	request.Header.Set("Cookie", r.Header.Get("Cookie"))
	response, err := s.internal.Do(request)
	if err != nil {
		httpkit.Fail(w, 503, "authorization unavailable")
		return
	}
	defer response.Body.Close()
	if response.StatusCode != 200 {
		status := response.StatusCode
		if status != 401 && status != 403 && status != 404 {
			status = 503
		}
		httpkit.Fail(w, status, "image unavailable")
		return
	}
	var permitted mediastore.Receipt
	if json.NewDecoder(io.LimitReader(response.Body, 8192)).Decode(&permitted) != nil || !identifiers.ValidUUID(permitted.ID) || !assetpack.ValidHash(permitted.SHA256) {
		httpkit.Fail(w, 503, "invalid authorization response")
		return
	}
	raw, err := s.media.root.ReadFile(mediaReceipt(permitted.ID))
	var stored mediastore.Receipt
	if err != nil || json.Unmarshal(raw, &stored) != nil || stored.Deleted || stored.SHA256 != permitted.SHA256 {
		http.NotFound(w, r)
		return
	}
	f, err := s.media.root.Open(mediaObject(permitted.ID))
	if err != nil {
		http.NotFound(w, r)
		return
	}
	defer f.Close()
	info, err := f.Stat()
	if err != nil || !info.Mode().IsRegular() || info.Size() != stored.Size {
		httpkit.Fail(w, 503, "image unavailable")
		return
	}
	w.Header().Set("Content-Type", "image/png")
	w.Header().Set("Cache-Control", "private, no-cache")
	w.Header().Set("Vary", "Cookie")
	w.Header().Set("ETag", `"`+permitted.SHA256+`"`)
	w.Header().Set("Content-Security-Policy", "sandbox; default-src 'none'")
	http.ServeContent(w, r, "image.png", info.ModTime(), f)
}
