package webbundle

import (
	"archive/zip"
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

func fixture(t *testing.T, label string) (string, Release) {
	t.Helper()
	root, out := t.TempDir(), t.TempDir()
	for name, b := range map[string]string{"index.html": `<link href="styles/main.css"><script src="bootstrap.js"></script>`, "bootstrap.js": "export const version=" + `"` + label + `";`, "ru.json": "{}", "en.json": "{}", "styles/main.css": "body{color:white}"} {
		p := filepath.Join(root, name)
		if e := os.MkdirAll(filepath.Dir(p), 0755); e != nil {
			t.Fatal(e)
		}
		if e := os.WriteFile(p, []byte(b), 0644); e != nil {
			t.Fatal(e)
		}
	}
	r, e := Build(root, out, "v0.30.0")
	if e != nil {
		t.Fatal(e)
	}
	return filepath.Join(out, r.Archive), r
}
func TestImmutableVersionsAndRollback(t *testing.T) {
	a, one := fixture(t, "one")
	b, two := fixture(t, "two")
	root := t.TempDir()
	store, e := Open(root)
	if e != nil {
		t.Fatal(e)
	}
	if _, e = Open(root); e == nil {
		t.Fatal("second storage owner was allowed")
	}
	defer func() {
		if store != nil {
			store.Close()
		}
	}()
	if _, e = store.Install(a); e != nil {
		t.Fatal(e)
	}
	request := func(p, etag string) *httptest.ResponseRecorder {
		r := httptest.NewRequest("GET", p, nil)
		r.Header.Set("If-None-Match", etag)
		w := httptest.NewRecorder()
		store.ServeHTTP(w, r)
		return w
	}
	html := request("/", "")
	if !strings.Contains(html.Body.String(), "/web/"+one.Revision+"/bootstrap.js") || html.Header().Get("Cache-Control") != "no-cache" {
		t.Fatal("HTML did not pin one bundle")
	}
	url := "/web/" + one.Revision + "/bootstrap.js"
	script := request(url, "")
	if !strings.Contains(script.Header().Get("Cache-Control"), "immutable") || script.Code != 200 {
		t.Fatal("missing immutable response")
	}
	if request(url, script.Header().Get("ETag")).Code != 304 {
		t.Fatal("missing conditional cache hit")
	}
	if _, e = store.Install(b); e != nil {
		t.Fatal(e)
	}
	if !strings.Contains(request(url, "").Body.String(), "one") || !strings.Contains(request("/", "").Body.String(), two.Revision) {
		t.Fatal("old page imports or new activation broken")
	}
	store.Close()
	store = nil
	store, e = Open(root)
	if e != nil {
		t.Fatal(e)
	}
	if store.Revision() != two.Revision {
		t.Fatal("activation lost on restart")
	}
	if e = store.Activate(one.Revision); e != nil {
		t.Fatal(e)
	}
	store.Close()
	store = nil
	if e = os.WriteFile(filepath.Join(root, "packs", one.Revision, "bootstrap.js"), []byte("corrupt"), 0644); e != nil {
		t.Fatal(e)
	}
	store, e = Open(root)
	if e != nil {
		t.Fatal(e)
	}
	if store.Revision() != two.Revision {
		t.Fatal("corrupt bundle did not fall back to verified previous version")
	}
}
func TestInvalidArchivesNeverActivate(t *testing.T) {
	good, release := fixture(t, "valid")
	s, e := Open(t.TempDir())
	if e != nil {
		t.Fatal(e)
	}
	defer s.Close()
	if _, e = s.Install(good); e != nil {
		t.Fatal(e)
	}
	for _, attack := range []string{"../escape.js", "bootstrap.js", "secret.svg"} {
		t.Run(attack, func(t *testing.T) {
			source, e := zip.OpenReader(good)
			if e != nil {
				t.Fatal(e)
			}
			defer source.Close()
			out := filepath.Join(t.TempDir(), "attack.zip")
			f, e := os.Create(out)
			if e != nil {
				t.Fatal(e)
			}
			z := zip.NewWriter(f)
			for _, entry := range source.File {
				r, e := entry.Open()
				if e != nil {
					t.Fatal(e)
				}
				w, e := z.Create(entry.Name)
				if e != nil {
					t.Fatal(e)
				}
				io.Copy(w, r)
				r.Close()
			}
			w, e := z.Create(attack)
			if e != nil {
				t.Fatal(e)
			}
			w.Write([]byte("intruder"))
			z.Close()
			f.Close()
			if _, e = s.Install(out); e == nil {
				t.Fatal("malicious archive accepted")
			}
			if s.Revision() != release.Revision {
				t.Fatal("failed install changed activation")
			}
		})
	}
}
func TestSyncPinsReleaseAndChecksHash(t *testing.T) {
	archive, r := fixture(t, "remote")
	raw, e := os.ReadFile(archive)
	if e != nil {
		t.Fatal(e)
	}
	invalid := true
	endpoint := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		if strings.HasSuffix(req.URL.Path, ".json") {
			copy := r
			if invalid {
				copy.SHA256 = strings.Repeat("0", 64)
			}
			json.NewEncoder(w).Encode(copy)
		} else {
			w.Write(raw)
		}
	}))
	defer endpoint.Close()
	s, e := Open(t.TempDir())
	if e != nil {
		t.Fatal(e)
	}
	defer s.Close()
	syncer := Sync{Store: s, Repository: "owner/repo", Tag: r.Tag, BaseURL: endpoint.URL, Client: endpoint.Client()}
	if _, e = syncer.Check(context.Background()); e == nil || s.Revision() != "" {
		t.Fatal("unverified archive activated")
	}
	invalid = false
	changed, e := syncer.Check(context.Background())
	if e != nil || !changed || s.Revision() != r.Revision {
		t.Fatal(changed, e)
	}
	changed, e = syncer.Check(context.Background())
	if e != nil || changed {
		t.Fatal("same release downloaded again", e)
	}
}
