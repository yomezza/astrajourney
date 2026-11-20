package integration

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"testing"
	"time"

	"github.com/yomezza/cosmosimulator/internal/workspaces"
)

func TestWorkspacesProductionBrowser(t *testing.T) {
	if os.Getenv("RUN_WORKSPACES_BROWSER") != "1" {
		t.Skip("set RUN_WORKSPACES_BROWSER=1 with Node/Playwright installed")
	}
	f := newFixture033(t)
	cookie, uid := f.signup("studio_browser")
	f.signup("studio_peer")
	active := f.integer("SELECT id FROM ships WHERE user_id=$1 AND active", uid)
	second := f.integer(`INSERT INTO ships(user_id,name,class,star,active,fuel,cargo)
		VALUES($1,'Studio scout','pilgrim',1,false,20,'{"iron":19}') RETURNING id`, uid)
	removed := f.integer(`INSERT INTO ships(user_id,name,class,star,active,fuel)
		VALUES($1,'Retired scout','pilgrim',1,false,20) RETURNING id`, uid)
	const firstID = "edec00a1-0000-4000-8000-000000000001"
	const secondID = "edec00a1-0000-4000-8000-000000000002"
	const retiredID = "edec00a1-0000-4000-8000-000000000003"
	f.req(cookie, "POST", "/api/workspaces", map[string]any{"revision": 1, "layout": workspaces.Layout{
		Version: 1, Active: firstID, Default: firstID, Workspaces: []workspaces.Workspace{
			{ID: firstID, Name: "First watch", ShipID: active, Modules: []workspaces.Module{}},
			{ID: secondID, Name: "Second watch", ShipID: second, Modules: []workspaces.Module{{
				ID: "edec00a2-0000-4000-8000-000000000001", Type: "cargo", Binding: "workspace", View: "window",
				Geometry: &workspaces.Geometry{X: 500, Y: 230, W: 395, H: 295},
			}}},
			{ID: retiredID, Name: "Retired ship", ShipID: removed, Modules: []workspaces.Module{{
				ID: "edec00a2-0000-4000-8000-000000000002", Type: "fleet", Binding: "workspace", View: "window",
			}}},
		},
	}}, 200)
	f.exec("DELETE FROM ships WHERE id=$1 AND user_id=$2", removed, uid)

	nonce := fmt.Sprintf("terminal-test-%d", time.Now().UnixNano())
	handler := browserCDN(t, f, f.handler)
	web := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("X-Cosmos-Test-Run", nonce)
		handler.ServeHTTP(w, r)
	}))
	defer web.Close()
	ctx, cancel := context.WithTimeout(f.ctx, 3*time.Minute)
	defer cancel()
	cmd := exec.CommandContext(ctx, "node", "../../../tests/ui/workspaces-live.mjs")
	cmd.Env = append(os.Environ(), "TERMINAL_TEST_URL="+web.URL, "TERMINAL_TEST_NONCE="+nonce)
	out, err := cmd.CombinedOutput()
	t.Log(string(out))
	if err != nil {
		t.Fatal(err)
	}
	if got := f.integer("SELECT id FROM ships WHERE user_id=$1 AND active", uid); got != active {
		t.Fatalf("workspace switch changed active ship: got %d, want %d", got, active)
	}
}
