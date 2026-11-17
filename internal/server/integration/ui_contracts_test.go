package integration

import (
	"github.com/yomezza/cosmosimulator/internal/webbundle"
	"github.com/yomezza/cosmosimulator/internal/webserver"
	"io/fs"
	"mime"
	"net/http"
	"net/http/httptest"
	"path"
	"path/filepath"
	"strings"
	"testing"

	"github.com/yomezza/cosmosimulator/internal/server"
)

// No pool or session cookie is supplied: these routes must reject access
// before touching PostgreSQL. The browser fixture cannot prove server auth.
func TestUIAnonymousAPIBoundary(t *testing.T) {
	h := server.New(nil, server.Config{}).Handler()
	for _, route := range []struct{ method, path string }{
		{"GET", "/api/state"},
		{"GET", "/api/fleet"},
		{"GET", "/api/galaxy"},
		{"GET", "/api/system?id=1"},
		{"GET", "/api/workbench"},
		{"GET", "/api/admin/users?page=1&q=pilot"},
		{"GET", "/api/planet/surface?star=1&planet=planet-1"},
		{"GET", "/api/planet/state?star=1&planet=planet-1"},
		{"POST", "/api/admin/tick"},
		{"POST", "/api/planet/action"},
		{"POST", "/api/planner"},
		{"POST", "/api/logistics"},
	} {
		t.Run(route.method+" "+route.path, func(t *testing.T) {
			r := httptest.NewRequest(route.method, route.path, strings.NewReader(`{}`))
			r.Header.Set("Content-Type", "application/json")
			w := httptest.NewRecorder()
			h.ServeHTTP(w, r)
			if w.Code != http.StatusUnauthorized {
				t.Fatalf("unauthenticated route returned %d: %s", w.Code, w.Body.String())
			}
			if w.Header().Get("Cache-Control") != "no-store" {
				t.Fatal("private API response must not be cached")
			}
		})
	}
}

func TestUIExternalDelivery(t *testing.T) {
	output := t.TempDir()
	release, err := webbundle.Build("../../../web", output, "v0.30.0")
	if err != nil {
		t.Fatal(err)
	}
	service, err := webserver.New(webserver.Config{StorageRoot: t.TempDir(), CosmicURL: "http://127.0.0.1:1", CDNURL: "http://127.0.0.1:1", PlanetURL: "http://127.0.0.1:1", ChatURL: "http://127.0.0.1:1"})
	if err != nil {
		t.Fatal(err)
	}
	defer service.Close()
	if err = service.Install(filepath.Join(output, release.Archive)); err != nil {
		t.Fatal(err)
	}
	h := service.Handler()
	var resources []struct{ path, media string }
	err = fs.WalkDir(browserAssets, ".", func(name string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if name == "index.html" {
			return nil
		}
		if entry.IsDir() {
			return nil
		}
		media := map[string]string{".html": "text/html", ".js": "text/javascript", ".css": "text/css", ".json": "application/json", ".svg": "image/svg+xml", ".png": "image/png"}[path.Ext(name)]
		if media != "" {
			resources = append(resources, struct{ path, media string }{"/web/" + release.Revision + "/" + name, media})
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	resources = append(resources, struct{ path, media string }{"/", "text/html"})
	for _, resource := range resources {
		t.Run(resource.path, func(t *testing.T) {
			w := httptest.NewRecorder()
			h.ServeHTTP(w, httptest.NewRequest("GET", resource.path, nil))
			media, _, err := mime.ParseMediaType(w.Header().Get("Content-Type"))
			if w.Code != http.StatusOK || err != nil || media != resource.media || w.Body.Len() == 0 {
				t.Fatalf("Web asset: status=%d media=%q bytes=%d err=%v", w.Code, media, w.Body.Len(), err)
			}
			if !strings.Contains(w.Header().Get("Content-Security-Policy"), "script-src 'self'") {
				t.Fatal("static UI must preserve the same-origin script policy")
			}
		})
	}
}
