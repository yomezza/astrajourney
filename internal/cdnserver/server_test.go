package cdnserver

import (
	"bytes"
	"context"
	"encoding/json"
	"image"
	"image/png"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/yomezza/cosmosimulator/internal/assetpack"
	"github.com/yomezza/cosmosimulator/internal/platform/mediastore"
	"github.com/yomezza/cosmosimulator/internal/platform/natscore"
)

func TestPrivateFilesRequireCurrentAuthorizationEvenFor304(t *testing.T) {
	id := "a1234567-1234-4234-8234-123456789abc"
	allow := true
	var receipt mediastore.Receipt
	var token string
	authorizations := 0
	cosmic := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/internal/cdn/v1/media/"+id+"/authorize" {
			w.Write([]byte("game"))
			return
		}
		if !mediastore.Authorized(r, token) {
			w.WriteHeader(403)
			return
		}
		authorizations++
		if !allow || r.Header.Get("Cookie") != "session=test" {
			w.WriteHeader(404)
			return
		}
		json.NewEncoder(w).Encode(receipt)
	}))
	defer cosmic.Close()
	root := t.TempDir()
	s, err := New(Config{StorageRoot: root, CosmicURL: cosmic.URL})
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	token = s.token
	cdn := httptest.NewServer(s.Handler())
	defer cdn.Close()
	client := &mediastore.Client{BaseURL: cdn.URL, TokenFile: s.config.TokenFile, HTTP: cdn.Client()}
	var body bytes.Buffer
	png.Encode(&body, image.NewRGBA(image.Rect(0, 0, 16, 16)))
	receipt, err = client.Do(context.Background(), "PUT", id, bytes.NewReader(body.Bytes()))
	if err != nil {
		t.Fatal(err)
	}
	if _, err = client.Do(context.Background(), "PUT", id, bytes.NewReader(body.Bytes())); err != nil {
		t.Fatal("idempotent replay", err)
	}
	var different bytes.Buffer
	png.Encode(&different, image.NewRGBA(image.Rect(0, 0, 8, 8)))
	if _, err = client.Do(context.Background(), "PUT", id, bytes.NewReader(different.Bytes())); err == nil {
		t.Fatal("operation reused with different bytes")
	}
	get := func(path, cookie, etag string) *http.Response {
		t.Helper()
		req, _ := http.NewRequest("GET", cdn.URL+path, nil)
		req.Header.Set("Cookie", cookie)
		if etag != "" {
			req.Header.Set("If-None-Match", etag)
		}
		response, err := cdn.Client().Do(req)
		if err != nil {
			t.Fatal(err)
		}
		return response
	}
	resp := get("/api/guide/media/"+id, "session=test", "")
	raw, _ := io.ReadAll(resp.Body)
	resp.Body.Close()
	if resp.StatusCode != 200 || assetpack.Hash(raw) != receipt.SHA256 || resp.Header.Get("Cache-Control") != "private, no-cache" {
		t.Fatal(resp.StatusCode, resp.Header)
	}
	etag := resp.Header.Get("ETag")
	resp = get("/api/guide/media/"+id, "session=test", etag)
	resp.Body.Close()
	if resp.StatusCode != 304 {
		t.Fatal("conditional image", resp.StatusCode)
	}
	allow = false
	resp = get("/api/guide/media/"+id, "session=test", etag)
	resp.Body.Close()
	if resp.StatusCode != 404 || authorizations != 3 {
		t.Fatal("revoked access served cached image", resp.StatusCode, authorizations)
	}
	for _, path := range []string{"/private/guide/objects/" + id[:2] + "/" + id + ".png", "/assets/sha256/" + receipt.SHA256 + "/private.png", "/internal/cdn/v1/media/" + id + "/authorize", "/cdn/internal/v1/media/" + id} {
		resp = get(path, "session=test", "")
		resp.Body.Close()
		if resp.StatusCode == 200 {
			t.Fatal("private bypass", path)
		}
	}
	cosmic.Close()
	resp = get("/api/guide/media/"+id, "session=test", "")
	resp.Body.Close()
	if resp.StatusCode != 503 {
		t.Fatal("authorization outage did not close access", resp.StatusCode)
	}
}
func TestExternalAssetsStayAvailableWithoutNATSOrCosmic(t *testing.T) {
	root := t.TempDir()
	source := filepath.Join(t.TempDir(), "source")
	os.MkdirAll(source, 0700)
	os.WriteFile(filepath.Join(source, "test.svg"), []byte(`<svg xmlns="http://www.w3.org/2000/svg"/>`), 0600)
	os.WriteFile(filepath.Join(source, "aliases.json"), []byte(`{"test.svg":["/test.svg"]}`), 0600)
	// The output location is intentionally outside the service binary/storage.
	out := t.TempDir()
	release, urls, err := assetpack.Build(source, out, "v0.29.0")
	if err != nil {
		t.Fatal(err)
	}
	s, err := New(Config{StorageRoot: root, CosmicURL: "http://127.0.0.1:1"})
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	if err = s.Install(filepath.Join(out, release.Archive)); err != nil {
		t.Fatal(err)
	}
	c := natscore.Config{Enabled: true, ServiceIdentity: "cdn", Username: "cdn", Password: "test-secret", Servers: []string{"nats://127.0.0.1:1"}, ConnectTimeout: 100 * time.Millisecond, ReconnectWait: 250 * time.Millisecond}
	if err = s.ConfigureNATS(c); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	s.nats.Start(ctx)
	defer s.nats.Close(context.Background())
	w := httptest.NewRecorder()
	s.Handler().ServeHTTP(w, httptest.NewRequest("GET", urls["/test.svg"], nil))
	if w.Code != 200 || !strings.Contains(w.Header().Get("Cache-Control"), "immutable") {
		t.Fatal(w.Code, w.Header())
	}
	w = httptest.NewRecorder()
	s.Handler().ServeHTTP(w, httptest.NewRequest("GET", "/cdn/api/v1/health/ready", nil))
	if w.Code != 200 {
		t.Fatal("NATS outage affected public readiness", w.Code)
	}
}

func TestLegacyGuideImportPreservesUUIDBytesAndSource(t *testing.T) {
	source, root := t.TempDir(), t.TempDir()
	id := "a1234567-1234-4234-8234-123456789abc"
	var original bytes.Buffer
	png.Encode(&original, image.NewRGBA(image.Rect(0, 0, 16, 16)))
	sourceFile := filepath.Join(source, id+".png")
	if err := os.WriteFile(sourceFile, original.Bytes(), 0600); err != nil {
		t.Fatal(err)
	}
	service, err := New(Config{StorageRoot: root, CosmicURL: "http://127.0.0.1:1"})
	if err != nil {
		t.Fatal(err)
	}
	defer service.Close()
	if err = service.ImportGuide(source); err != nil {
		t.Fatal(err)
	}
	if err = service.ImportGuide(source); err != nil {
		t.Fatal("repeat import", err)
	}
	receipt, err := service.media.inspect(id)
	if err != nil || receipt.ID != id || receipt.SHA256 != assetpack.Hash(original.Bytes()) {
		t.Fatal("import changed identity/bytes", receipt, err)
	}
	retained, err := os.ReadFile(sourceFile)
	if err != nil || !bytes.Equal(retained, original.Bytes()) {
		t.Fatal("legacy source changed")
	}
	if second, err := New(Config{StorageRoot: root, CosmicURL: "http://127.0.0.1:1"}); err == nil {
		second.Close()
		t.Fatal("second process could own the same storage")
	}
	var different bytes.Buffer
	png.Encode(&different, image.NewRGBA(image.Rect(0, 0, 8, 8)))
	os.WriteFile(sourceFile, different.Bytes(), 0600)
	if err = service.ImportGuide(source); err == nil {
		t.Fatal("different bytes overwrote existing UUID")
	}
	receipt, err = service.media.inspect(id)
	if err != nil || receipt.SHA256 != assetpack.Hash(original.Bytes()) {
		t.Fatal("failed import damaged current bytes")
	}
}
