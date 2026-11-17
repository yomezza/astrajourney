package assetpack

import (
	"archive/zip"
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func fixturePack(t *testing.T) (Release, string, Manifest) {
	t.Helper()
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, "ui"), 0700); err != nil {
		t.Fatal(err)
	}
	for p, b := range map[string]string{"ui/test.svg": `<svg xmlns="http://www.w3.org/2000/svg"><path d="M0 0L1 1"/></svg>`, "aliases.json": `{"ui/test.svg":["/test.svg","/test.svg?v=old"]}`} {
		if err := os.WriteFile(filepath.Join(root, p), []byte(b), 0600); err != nil {
			t.Fatal(err)
		}
	}
	out := t.TempDir()
	release, _, err := Build(root, out, "v0.29.0")
	if err != nil {
		t.Fatal(err)
	}
	archive := filepath.Join(out, release.Archive)
	m, err := readManifestFromArchive(archive)
	if err != nil {
		t.Fatal(err)
	}
	return release, archive, m
}
func TestInstallCacheAndOfflineRestart(t *testing.T) {
	_, archive, m := fixturePack(t)
	root := t.TempDir()
	s, err := Open(root)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = s.Install(archive); err != nil {
		t.Fatal(err)
	}
	e := m.Entries[0]
	request := func(method, path string, headers map[string]string) *httptest.ResponseRecorder {
		t.Helper()
		r := httptest.NewRequest(method, path, nil)
		for k, v := range headers {
			r.Header.Set(k, v)
		}
		w := httptest.NewRecorder()
		if !s.ServePublic(w, r) {
			t.Fatal("resource not found", path)
		}
		return w
	}
	raw := request("GET", e.URL(), nil)
	if raw.Code != 200 || raw.Header().Get("ETag") != `"`+e.SHA256+`"` || !strings.Contains(raw.Header().Get("Cache-Control"), "immutable") {
		t.Fatal(raw.Code, raw.Header())
	}
	if w := request("GET", e.URL(), map[string]string{"If-None-Match": raw.Header().Get("ETag")}); w.Code != 304 || w.Body.Len() != 0 {
		t.Fatal("conditional", w.Code, w.Body.Len())
	}
	if w := request("HEAD", e.URL(), nil); w.Code != 200 || w.Body.Len() != 0 {
		t.Fatal("HEAD", w.Code)
	}
	zipped := request("GET", e.URL(), map[string]string{"Accept-Encoding": "gzip"})
	if zipped.Header().Get("Content-Encoding") != "gzip" || zipped.Header().Get("ETag") == raw.Header().Get("ETag") {
		t.Fatal("gzip representation", zipped.Header())
	}
	if w := request("GET", e.URL(), map[string]string{"Range": "bytes=0-9", "Accept-Encoding": "gzip"}); w.Code != 206 || w.Body.Len() != 10 || w.Header().Get("Content-Encoding") != "" {
		t.Fatal("range", w.Code, w.Header())
	}
	if s.ServePublic(httptest.NewRecorder(), httptest.NewRequest("GET", "/test.svg?v=unknown", nil)) {
		t.Fatal("unrecognized immutable query accepted")
	}
	s.Close()
	s, err = Open(root)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	if !s.Has(m.Revision) {
		t.Fatal("offline pack lost")
	}
}
func TestInstallRejectsPartialTraversalAndUnlistedFiles(t *testing.T) {
	_, archive, m := fixturePack(t)
	s, err := Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	if _, err = s.Install(archive); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"../outside.png", "C:/escape.png", "public/../../private/file.png", "CON.png", "ui/unlisted.png"} {
		t.Run(name, func(t *testing.T) {
			var b bytes.Buffer
			z := zip.NewWriter(&b)
			f, _ := z.Create(name)
			f.Write([]byte("bad"))
			z.Close()
			p := filepath.Join(t.TempDir(), "bad.zip")
			os.WriteFile(p, b.Bytes(), 0600)
			if _, err = s.Install(p); err == nil {
				t.Fatal("unsafe archive accepted")
			}
			if !s.Has(m.Revision) {
				t.Fatal("working pack replaced")
			}
		})
	}
}
func TestReleaseSyncChecksHashAndUsesLocalPackOffline(t *testing.T) {
	release, archive, m := fixturePack(t)
	raw, err := os.ReadFile(archive)
	if err != nil {
		t.Fatal(err)
	}
	corrupt := false
	remote := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasSuffix(r.URL.Path, ".json") {
			json.NewEncoder(w).Encode(release)
			return
		}
		body := raw
		if corrupt {
			body = []byte("broken")
		}
		w.Write(body)
	}))
	defer remote.Close()
	s, err := Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	syncer := Sync{Store: s, Repository: "owner/game", RequiredTag: "v0.29.0", BaseURL: remote.URL, Client: remote.Client()}
	corrupt = true
	if _, err = syncer.Check(context.Background(), false); err == nil {
		t.Fatal("corrupt download accepted")
	}
	if len(s.Revisions()) != 0 {
		t.Fatal("incomplete pack activated")
	}
	corrupt = false
	if changed, err := syncer.Check(context.Background(), false); err != nil || !changed {
		t.Fatal(changed, err)
	}
	if !s.Has(m.Revision) {
		t.Fatal("pack missing")
	}
	remote.Close()
	if !s.Has(m.Revision) {
		t.Fatal("network outage removed pack")
	}
}
func TestBrowserAddressChangesOnlyForModifiedFile(t *testing.T) {
	_, archive, m := fixturePack(t)
	z, err := zip.OpenReader(archive)
	if err != nil {
		t.Fatal(err)
	}
	defer z.Close()
	f, _ := z.Open(m.Entries[0].Path)
	raw, _ := io.ReadAll(f)
	f.Close()
	e := m.Entries[0]
	old := e.URL()
	e.SHA256 = Hash(append(raw, ' '))
	if old == e.URL() {
		t.Fatal("changed contents reused immutable URL")
	}
	if m.Entries[0].URL() != old {
		t.Fatal("unmodified contents changed URL")
	}
}
