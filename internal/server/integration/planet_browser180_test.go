package integration

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"github.com/yomezza/cosmosimulator/internal/planetserver"
	"io"
	"log/slog"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"
)

func TestPlanetBrowser180(t *testing.T)            { planetBrowser210(t, false) }
func TestPlanetTransitionsBrowser210(t *testing.T) { planetBrowser210(t, true) }
func TestDiscoveryGuidePlanetBrowser250(t *testing.T) {
	if os.Getenv("RUN_DISCOVERY_BROWSER") != "1" {
		t.Skip("set RUN_DISCOVERY_BROWSER=1 with Node/Playwright")
	}
	t.Setenv("RUN_PLANET_BROWSER", "1")
	planetBrowser210(t, false, "discovery-guide-planet-live.mjs")
}
func planetBrowser210(t *testing.T, transition bool, custom ...string) {
	if os.Getenv("RUN_PLANET_BROWSER") != "1" {
		t.Skip("set RUN_PLANET_BROWSER=1 with Node/Playwright")
	}
	f, c, uid, surface, site, _ := prepare180(t)
	peer, _, _ := peer200(t, f, uid, surface)
	if transition {
		company := f.integer(`SELECT company_id FROM users WHERE id=$1`, uid)
		addColony160(f, uid, surface, 2, &company, false)
		reconcile160(f)
	}
	web := httptest.NewUnstartedServer(http.NotFoundHandler())
	defer web.Close()
	base := "http://" + web.Listener.Addr().String()
	cfg := planetserver.DefaultConfig()
	cfg.AllowedOrigins = []string{base, "http://127.0.0.1:8080"}
	app, err := planetserver.New(f.db, cfg, slog.New(slog.NewTextHandler(io.Discard, nil)))
	if err != nil {
		t.Fatal(err)
	}
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	run, cancel := context.WithCancel(f.ctx)
	done := make(chan error, 1)
	started := make(chan struct{})
	go func() { done <- app.Serve(run, listener, func() { close(started) }) }()
	defer func() {
		cancel()
		if err := <-done; err != nil {
			t.Error(err)
		}
	}()
	<-started
	if transition {
		ship := f.integer(`SELECT id FROM ships WHERE user_id=$1 AND active`, uid)
		conn, _, _, _ := edge210(t, f, c, uid, surface, site, ship, "http://"+listener.Addr().String())
		conn.CloseNow()
		wait180(t, f, `SELECT count(*) FROM planet_visits WHERE user_id=$1 AND state='disconnected'`, 1, uid)
	}
	t.Setenv("PLANET_INTERNAL_URL", "http://"+listener.Addr().String())
	nonce := fmt.Sprintf("terminal-test-%d", time.Now().UnixNano())
	handler := f.app.Handler()
	web.Config.Handler = http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("X-Cosmos-Test-Run", nonce)
		handler.ServeHTTP(w, r)
	})
	web.Start()
	scriptName := "planet-live.mjs"
	if len(custom) > 0 {
		scriptName = custom[0]
		f.exec(`UPDATE users SET role='admin' WHERE id=$1`, uid)
	}
	if transition {
		scriptName = "planet-transition-live.mjs"
	}
	script, err := filepath.Abs("../../../tests/ui/" + scriptName)
	if err != nil {
		t.Fatal(err)
	}
	ctx, stop := context.WithTimeout(f.ctx, 90*time.Second)
	defer stop()
	cmd := exec.CommandContext(ctx, "node", script)
	input, _ := json.Marshal(map[string]any{"cookie": c.Value, "peer_cookie": peer.Value, "site": fmt.Sprint(site)})
	cmd.Stdin = bytes.NewReader(input)
	cmd.Env = append(os.Environ(), "TERMINAL_TEST_URL="+web.URL, "TERMINAL_TEST_NONCE="+nonce)
	out, err := cmd.CombinedOutput()
	t.Log(string(out))
	if err != nil {
		t.Fatal(err)
	}
}
