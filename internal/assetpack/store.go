package assetpack

import (
	"archive/zip"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/yomezza/cosmosimulator/internal/platform/fileatom"
	"github.com/yomezza/cosmosimulator/internal/platform/httpkit"
)

type stored struct {
	Entry
	File string
}
type Store struct {
	publicMap *httpkit.StaticContent
	Directory string
	root      *os.Root
	mu        sync.RWMutex
	install   sync.Mutex
	files     map[string]stored
	aliases   map[string]string
	revisions map[string]Manifest
	order     []string
}

func Open(directory string) (*Store, error) {
	abs, err := filepath.Abs(directory)
	if err != nil {
		return nil, err
	}
	for _, p := range []string{"public/packs", "private/guide/objects", "private/guide/receipts", "staging/packs", "staging/uploads", "quarantine/guide", "state"} {
		if err = os.MkdirAll(filepath.Join(abs, filepath.FromSlash(p)), 0700); err != nil {
			return nil, err
		}
	}
	root, err := os.OpenRoot(abs)
	if err != nil {
		return nil, err
	}
	s := &Store{Directory: abs, root: root, files: map[string]stored{}, aliases: map[string]string{}, revisions: map[string]Manifest{}}
	dirs, err := os.ReadDir(filepath.Join(abs, "public", "packs"))
	if err != nil {
		root.Close()
		return nil, err
	}
	// Recover activation order without relying on directory/hash sort order.
	var catalog struct {
		Revisions []string `json:"revisions"`
	}
	if raw, e := root.ReadFile("state/catalog.json"); e == nil {
		_ = json.Unmarshal(raw, &catalog)
	}
	rank := map[string]int{}
	for i, r := range catalog.Revisions {
		rank[r] = i + 1
	}
	sort.SliceStable(dirs, func(i, j int) bool { return rank[dirs[i].Name()] < rank[dirs[j].Name()] })
	for _, d := range dirs {
		if !d.IsDir() || !ValidHash(d.Name()) {
			continue
		}
		base := "public/packs/" + d.Name()
		m, e := s.verify(base)
		if e != nil {
			root.Close()
			return nil, fmt.Errorf("installed asset pack %s: %w", d.Name(), e)
		}
		s.add(m, base)
	}
	return s, nil
}
func (s *Store) Close() error { return s.root.Close() }
func (s *Store) verify(base string) (Manifest, error) {
	raw, err := s.root.ReadFile(base + "/manifest.json")
	if err != nil {
		return Manifest{}, err
	}
	m, err := Decode(raw)
	if err != nil {
		return m, err
	}
	for _, e := range m.Entries {
		for _, v := range []struct {
			path, hash string
			size       int64
		}{{e.Path, e.SHA256, e.Size}, {e.Path + ".gz", e.GzipSHA256, e.GzipSize}} {
			if v.hash == "" {
				continue
			}
			f, er := s.root.Open(base + "/" + v.path)
			if er != nil {
				return m, er
			}
			info, er := f.Stat()
			if er != nil || !info.Mode().IsRegular() || info.Size() != v.size {
				f.Close()
				return m, errors.New("asset size/type mismatch")
			}
			digest := sha256.New()
			n, er := io.Copy(digest, io.LimitReader(f, v.size+1))
			f.Close()
			if er != nil || n != v.size || hex.EncodeToString(digest.Sum(nil)) != v.hash {
				return m, errors.New("asset checksum mismatch")
			}
		}
	}
	return m, nil
}
func (s *Store) add(m Manifest, base string) {
	for _, e := range m.Entries {
		url := e.URL()
		s.files[url] = stored{e, base + "/" + e.Path}
		for _, a := range e.Aliases {
			if old, ok := s.aliases[a]; !ok || !strings.Contains(a, "?") || old == url {
				s.aliases[a] = url
			}
		}
	}
	for i, revision := range s.order {
		if revision == m.Revision {
			s.order = append(s.order[:i], s.order[i+1:]...)
			break
		}
	}
	s.order = append(s.order, m.Revision)
	s.revisions[m.Revision] = m
	urls := map[string]string{}
	for _, entry := range m.Entries {
		for _, alias := range entry.Aliases {
			if !strings.Contains(alias, "?") {
				urls[alias] = entry.URL()
			}
		}
	}
	raw, _ := json.Marshal(map[string]any{"schema_version": Schema, "client_contract": Contract, "revision": m.Revision, "urls": urls})
	s.publicMap = httpkit.NewStaticContent(raw)
}

// ServeMap advertises a complete, verified pack. Clients freeze one map per page
// and check required logical aliases before accepting a newer compatible pack.
func (s *Store) ServeMap(w http.ResponseWriter, r *http.Request) {
	s.mu.RLock()
	content := s.publicMap
	s.mu.RUnlock()
	if content == nil {
		http.Error(w, "assets not installed", 503)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-cache")
	content.ServeHTTP(w, r)
}
func (s *Store) Has(revision string) bool {
	s.mu.RLock()
	defer s.mu.RUnlock()
	_, ok := s.revisions[revision]
	return ok
}
func (s *Store) Revisions() []string {
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := make([]string, 0, len(s.revisions))
	for r := range s.revisions {
		out = append(out, r)
	}
	sort.Strings(out)
	return out
}
func (s *Store) Install(archive string) (Manifest, error) {
	s.install.Lock()
	defer s.install.Unlock()
	z, err := zip.OpenReader(archive)
	if err != nil {
		return Manifest{}, err
	}
	defer z.Close()
	if len(z.File) > MaxFiles*2+1 {
		return Manifest{}, errors.New("too many archive files")
	}
	staging, err := os.MkdirTemp(filepath.Join(s.Directory, "staging", "packs"), "install-")
	if err != nil {
		return Manifest{}, err
	}
	defer os.RemoveAll(staging)
	var total uint64
	seen := map[string]bool{}
	for _, entry := range z.File {
		p := entry.Name
		if !ValidPath(p) || !entry.Mode().IsRegular() || seen[strings.ToLower(p)] {
			return Manifest{}, errors.New("invalid/duplicate archive path or file type")
		}
		seen[strings.ToLower(p)] = true
		if entry.UncompressedSize64 > 32<<20 {
			return Manifest{}, errors.New("archive entry too large")
		}
		total += entry.UncompressedSize64
		if total > uint64(MaxPackBytes) {
			return Manifest{}, errors.New("archive too large")
		}
		target := filepath.Join(staging, filepath.FromSlash(p))
		if err = os.MkdirAll(filepath.Dir(target), 0700); err != nil {
			return Manifest{}, err
		}
		in, e := entry.Open()
		if e != nil {
			return Manifest{}, e
		}
		out, e := os.OpenFile(target, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
		if e != nil {
			in.Close()
			return Manifest{}, e
		}
		n, copyErr := io.Copy(out, io.LimitReader(in, int64(entry.UncompressedSize64)+1))
		in.Close()
		syncErr := out.Sync()
		closeErr := out.Close()
		if copyErr != nil || syncErr != nil || closeErr != nil || n != int64(entry.UncompressedSize64) {
			return Manifest{}, errors.New("incomplete archive extraction")
		}
	}
	relative, _ := filepath.Rel(s.Directory, staging)
	base := filepath.ToSlash(relative)
	m, err := s.verify(base)
	if err != nil {
		return m, err
	}
	expected := map[string]bool{"manifest.json": true}
	for _, e := range m.Entries {
		expected[e.Path] = true
		if e.GzipSHA256 != "" {
			expected[e.Path+".gz"] = true
		}
	}
	if len(expected) != len(seen) {
		return m, errors.New("unlisted archive file")
	}
	for p := range expected {
		if !seen[strings.ToLower(p)] {
			return m, errors.New("missing archive file")
		}
	}
	s.mu.RLock()
	for _, e := range m.Entries {
		for _, a := range e.Aliases {
			if old, ok := s.aliases[a]; ok && strings.Contains(a, "?") && old != e.URL() {
				s.mu.RUnlock()
				return m, errors.New("immutable legacy alias conflict")
			}
		}
	}
	s.mu.RUnlock()
	final := "public/packs/" + m.Revision
	if !s.Has(m.Revision) {
		if err = os.Rename(staging, filepath.Join(s.Directory, filepath.FromSlash(final))); err != nil {
			return m, err
		}
	}
	s.mu.Lock()
	s.add(m, final)
	s.mu.Unlock()
	raw, _ := json.MarshalIndent(map[string]any{"schema_version": Schema, "revisions": s.order, "updated_at": time.Now().UTC()}, "", "  ")
	catalog := filepath.Join(s.Directory, "state", "catalog.json")
	if old, e := os.ReadFile(catalog); e == nil {
		if e = fileatom.Write(filepath.Join(s.Directory, "state", "catalog.previous.json"), old, 0600); e != nil {
			return m, e
		}
	}
	if err = fileatom.Write(catalog, raw, 0600); err != nil {
		return m, err
	}
	return m, nil
}
func (s *Store) ServePublic(w http.ResponseWriter, r *http.Request) bool {
	if r.Method != "GET" && r.Method != "HEAD" {
		return false
	}
	s.mu.RLock()
	entry, ok := s.files[r.URL.Path]
	alias := r.URL.Path
	if r.URL.RawQuery != "" {
		alias += "?" + r.URL.RawQuery
	}
	target, aliasOK := s.aliases[alias]
	s.mu.RUnlock()
	if !ok && aliasOK {
		w.Header().Set("Cache-Control", "no-cache")
		http.Redirect(w, r, target, http.StatusTemporaryRedirect)
		return true
	}
	if !ok {
		return false
	}
	w.Header().Set("Vary", "Accept-Encoding")
	etag := entry.SHA256
	file := entry.File
	if entry.GzipSHA256 != "" && r.Header.Get("Range") == "" && acceptsGzip(r.Header.Get("Accept-Encoding")) {
		file += ".gz"
		etag = entry.GzipSHA256
		w.Header().Set("Content-Encoding", "gzip")
	}
	f, err := s.root.Open(file)
	if err != nil {
		http.Error(w, "asset unavailable", 503)
		return true
	}
	defer f.Close()
	info, err := f.Stat()
	if err != nil {
		http.Error(w, "asset unavailable", 503)
		return true
	}
	w.Header().Set("Content-Type", entry.MIME)
	w.Header().Set("Cache-Control", "public, max-age=31536000, immutable")
	w.Header().Set("ETag", `"`+etag+`"`)
	w.Header().Set("X-Content-Type-Options", "nosniff")
	if entry.MIME == "image/svg+xml" {
		w.Header().Set("Content-Security-Policy", "sandbox; default-src 'none'; style-src 'unsafe-inline'")
	}
	http.ServeContent(w, r, entry.Path, info.ModTime(), f)
	return true
}
func acceptsGzip(header string) bool {
	for _, part := range strings.Split(header, ",") {
		p := strings.Split(strings.TrimSpace(part), ";")
		if p[0] != "gzip" {
			continue
		}
		for _, q := range p[1:] {
			param := strings.TrimSpace(q)
			if strings.HasPrefix(param, "q=") {
				v, err := strconv.ParseFloat(strings.TrimPrefix(param, "q="), 64)
				if err != nil || v <= 0 || v > 1 {
					return false
				}
			}
		}
		return true
	}
	return false
}
