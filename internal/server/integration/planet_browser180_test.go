package integration

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"github.com/yomezza/cosmosimulator/internal/chat"
	"github.com/yomezza/cosmosimulator/internal/chatserver"
	"github.com/yomezza/cosmosimulator/internal/planet/pathing"
	"github.com/yomezza/cosmosimulator/internal/planet/scene"
	"github.com/yomezza/cosmosimulator/internal/planet/scenestore"
	"github.com/yomezza/cosmosimulator/internal/planetserver"
	"io"
	"log/slog"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"sync"
	"testing"
	"time"
)

func TestPlanetModeWithCosmicWorkers(t *testing.T) { planetBrowser210(t, false, "planet-mode-workers") }
func TestPlanetObjectMenuBrowser2510(t *testing.T) {
	planetBrowser210(t, false, "planet-object-menu-live.mjs")
}
func TestPlanetBuildingBrowser259(t *testing.T) {
	planetBrowser210(t, false, "planet-building-live.mjs")
}
func TestPlanetPixelKeyboardBrowser258(t *testing.T) {
	t.Setenv("CHAT_ENABLED", "true")
	planetBrowser210(t, false, "planet-pixel-keyboard-live.mjs")
}
func TestPlanetBrowser180(t *testing.T)            { planetBrowser210(t, false) }
func TestPlanetTransitionsBrowser210(t *testing.T) { planetBrowser210(t, true) }
func TestPlanetWalkingBrowser257(t *testing.T)     { planetBrowser210(t, false, "planet-walking-live.mjs") }
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

	if len(custom) > 0 && custom[0] == "planet-mode-workers" {
		run, stop := context.WithCancel(f.ctx)
		var workers sync.WaitGroup
		for _, work := range []func(context.Context){f.app.Simulation.Simulate, f.app.Colonies.RunLandingWorker, f.app.Transfer.Run, f.app.Corporations.RunChatCleanup, f.app.Knowledge.Run} {
			workers.Add(1)
			go func(fn func(context.Context)) { defer workers.Done(); fn(run) }(work)
		}
		defer func() { stop(); workers.Wait() }()
	}
	peer, _, _ := peer200(t, f, uid, surface)
	if len(custom) > 0 && custom[0] == "planet-walking-live.mjs" {
		// One real PostgreSQL checkpoint exceeds the normal 2s DB budget.
		// The sequence survives the rolled-back UPDATE, so only the first save is delayed.
		f.exec(`CREATE SEQUENCE walking_save_fault;
 CREATE FUNCTION walking_save_delay() RETURNS trigger LANGUAGE plpgsql AS $$ BEGIN
 IF (NEW.x_q<>OLD.x_q OR NEW.y_q<>OLD.y_q) AND nextval('walking_save_fault')=1 THEN PERFORM pg_sleep(3); END IF;
 RETURN NEW; END $$;
 CREATE TRIGGER walking_save_delay BEFORE UPDATE ON planet_characters FOR EACH ROW EXECUTE FUNCTION walking_save_delay()`)
	}
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
	var logOutput io.Writer = io.Discard
	if len(custom) > 0 && custom[0] == "planet-walking-live.mjs" {
		logOutput = os.Stderr
	}
	app, err := planetserver.New(f.db, cfg, slog.New(slog.NewTextHandler(logOutput, nil)))
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
	var chatApp *chatserver.App
	var chatListener net.Listener
	if len(custom) > 0 && custom[0] == "planet-pixel-keyboard-live.mjs" {
		t.Setenv("CHAT_ENABLED", "true")
		service := &chat.Service{DB: f.db}
		journal, e := chat.OpenJournal(f.ctx, service, filepath.Join(t.TempDir(), "erasure.log"))
		if e != nil {
			t.Fatal(e)
		}
		defer journal.Close()
		service.Journal = journal
		chatListener, e = net.Listen("tcp", "127.0.0.1:0")
		if e != nil {
			t.Fatal(e)
		}
		t.Setenv("CHAT_INTERNAL_URL", "http://"+chatListener.Addr().String())
		chatApp, e = chatserver.New(service, chatserver.Config{Addr: chatListener.Addr().String(), DatabaseURL: "configured", Journal: "configured", CosmicURL: base, PlanetURL: "http://" + listener.Addr().String(), Origins: []string{base}})
		if e != nil {
			t.Fatal(e)
		}
	}
	nonce := fmt.Sprintf("terminal-test-%d", time.Now().UnixNano())
	handler := browserCDN(t, f, f.app.Handler())
	web.Config.Handler = http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("X-Cosmos-Test-Run", nonce)
		handler.ServeHTTP(w, r)
	})
	web.Start()
	if chatApp != nil {
		chatRun, chatCancel := context.WithCancel(f.ctx)
		chatDone := make(chan error, 1)
		chatStarted := make(chan struct{})
		go func() { chatDone <- chatApp.Serve(chatRun, chatListener, func() { close(chatStarted) }) }()
		defer func() {
			chatCancel()
			if e := <-chatDone; e != nil {
				t.Error(e)
			}
		}()
		<-chatStarted
	}

	scriptName := "planet-live.mjs"
	if len(custom) > 0 {
		scriptName = custom[0]
		if scriptName == "planet-mode-workers" {
			scriptName = "planet-walking-live.mjs"
		}
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
	payload := map[string]any{"cookie": c.Value, "peer_cookie": peer.Value, "site": fmt.Sprint(site)}
	if len(custom) > 0 && custom[0] == "planet-pixel-keyboard-live.mjs" {
		var fence scenestore.Fence
		if e := f.db.QueryRow(f.ctx, `SELECT owner_id,fence FROM planet_runtime_leases WHERE id=1`).Scan(&fence.Owner, &fence.Number); e != nil {
			t.Fatal(e)
		}
		local, e := scenestore.New(f.db).Load(f.ctx, scene.Key{Surface: surface, Sector: 1}, fence)
		if e != nil {
			t.Fatal(e)
		}
		landing := local.Metadata().Landing
		found := false
		for radius := 2; radius <= 20 && !found; radius++ {
			for dy := -radius; dy <= radius && !found; dy++ {
				for dx := -radius; dx <= radius && !found; dx++ {
					x, y := landing.SpawnX/1024+dx, landing.SpawnY/1024+dy
					if !local.Walkable(x, y) {
						continue
					}
					for _, d := range [][2]int{{1, 0}, {-1, 0}, {0, 1}, {0, -1}} {
						if local.Walkable(x+d[0], y+d[1]) {
							continue
						}
						if _, e := pathing.Find(f.ctx, local, pathing.FromQ(landing.SpawnX, landing.SpawnY), pathing.Point{X: float64(x) + .5, Y: float64(y) + .5}, 24000); e != nil {
							continue
						}
						payload["obstacle"] = map[string]int{"x": x*1024 + 512, "y": y*1024 + 512, "dx": d[0], "dy": d[1]}
						found = true
						break
					}
				}
			}
		}
		if !found {
			t.Fatal("fixture has no reachable collision boundary")
		}
	}
	input, _ := json.Marshal(payload)
	cmd.Stdin = bytes.NewReader(input)
	cmd.Env = append(os.Environ(), "TERMINAL_TEST_URL="+web.URL, "TERMINAL_TEST_NONCE="+nonce)
	out, err := cmd.CombinedOutput()
	t.Log(string(out))
	if err != nil {
		t.Fatal(err)
	}
}
