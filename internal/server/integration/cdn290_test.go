package integration

import (
	"encoding/json"
	"fmt"
	"io/fs"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/yomezza/cosmosimulator/internal/assetpack"
	"github.com/yomezza/cosmosimulator/internal/cdnserver"
	"github.com/yomezza/cosmosimulator/internal/server"
)

func TestCosmicHasNoEmbeddedGraphics290(t *testing.T) {
	err := fs.WalkDir(server.Assets, ".", func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			return nil
		}
		switch filepath.Ext(path) {
		case ".svg", ".png", ".jpg", ".webp", ".gif", ".ico", ".html", ".css", ".js":
			t.Errorf("graphics still embedded: %s", path)
		}
		raw, err := server.Assets.ReadFile(path)
		if err != nil {
			return err
		}
		if strings.Contains(string(raw), "data:image/") {
			t.Errorf("static image data URI remains: %s", path)
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
}
func TestCDNProductionBrowserCache290(t *testing.T) {
	if os.Getenv("RUN_CDN_BROWSER") != "1" {
		t.Skip("set RUN_CDN_BROWSER=1 with Node/Playwright")
	}
	f := newFixture033(t)
	cookie, _ := f.signup("cdnbrowser290")
	var cosmicGraphics, cdnPNGs atomic.Int64
	cosmic := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasPrefix(r.URL.Path, "/assets/") || strings.HasSuffix(r.URL.Path, ".png") || strings.HasSuffix(r.URL.Path, ".svg") {
			cosmicGraphics.Add(1)
		}
		f.handler.ServeHTTP(w, r)
	}))
	defer cosmic.Close()
	out := t.TempDir()
	release, _, err := assetpack.Build("../../../assets/game", out, "v0.29.0")
	if err != nil {
		t.Fatal(err)
	}
	cdn, err := cdnserver.New(cdnserver.Config{StorageRoot: t.TempDir(), CosmicURL: cosmic.URL})
	if err != nil {
		t.Fatal(err)
	}
	defer cdn.Close()
	if err = cdn.Install(filepath.Join(out, release.Archive)); err != nil {
		t.Fatal(err)
	}
	internal := httptest.NewServer(cdn.Handler())
	defer internal.Close()
	handler := browserWebGateway(t, cosmic.URL, internal.URL, "http://127.0.0.1:8081", "http://127.0.0.1:8082")
	nonce := fmt.Sprintf("cdn-test-%d", time.Now().UnixNano())
	endpoint := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("X-Cosmos-Test-Run", nonce)
		if strings.HasSuffix(r.URL.Path, ".png") {
			cdnPNGs.Add(1)
		}
		handler.ServeHTTP(w, r)
	}))
	defer endpoint.Close()
	input, _ := json.Marshal(map[string]string{"cookie_name": cookie.Name, "cookie": cookie.Value})
	script, _ := filepath.Abs("../../../tests/ui/cdn-cache-live.mjs")
	cmd := exec.CommandContext(f.ctx, "node", script)
	cmd.Stdin = strings.NewReader(string(input))
	cmd.Env = append(os.Environ(), "CDN_TEST_URL="+endpoint.URL, "CDN_TEST_NONCE="+nonce)
	output, err := cmd.CombinedOutput()
	t.Log(string(output))
	if err != nil {
		t.Fatal(err)
	}
	if cosmicGraphics.Load() != 0 {
		t.Fatalf("Cosmic handled %d graphics requests", cosmicGraphics.Load())
	}
	if cdnPNGs.Load() != 7 {
		t.Fatalf("expected seven initial PNG transfers and none after navigation; got %d", cdnPNGs.Load())
	}
}
