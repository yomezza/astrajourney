package integration

import (
	"encoding/json"
	"net/http/httptest"
	"os"
	"slices"
	"strings"
	"sync"
	"testing"

	"github.com/yomezza/cosmosimulator/internal/platform/dblayout"
	"github.com/yomezza/cosmosimulator/internal/workspaces"
)

func TestWorkspacePreferences(t *testing.T) {
	f := newFixture033(t)
	const path = "/api/workspaces"
	type document struct {
		Revision int64             `json:"revision"`
		Layout   workspaces.Layout `json:"layout"`
	}
	read := func(w *httptest.ResponseRecorder) document {
		t.Helper()
		var d document
		if err := json.Unmarshal(w.Body.Bytes(), &d); err != nil {
			t.Fatal(err)
		}
		return d
	}
	f.req(nil, "GET", path, nil, 401)
	cookie, uid := f.signup("workspace_owner")
	other, otherUID := f.signup("workspace_other")
	// Re-run the real migration against an installation with existing players
	// and no workspace table, as from an earlier release.
	var root string
	if err := f.db.QueryRow(f.ctx, "SELECT current_schema()").Scan(&root); err != nil {
		t.Fatal(err)
	}
	f.exec("DROP TABLE " + dblayout.Schema(root, "identity") + ".pilot_workspace_ids")
	f.exec("DROP TABLE " + dblayout.Schema(root, "identity") + ".pilot_workspace_preferences")
	reopened, err := openDatabase(f.ctx, os.Getenv("TEST_DATABASE_URL"), root)
	if err != nil {
		t.Fatal(err)
	}
	reopened.Close()

	d := read(f.req(cookie, "GET", path, nil, 200))
	foreign := read(f.req(other, "GET", path, nil, 200))
	if d.Revision != 1 || len(d.Layout.Workspaces) != 1 || d.Layout.Active == foreign.Layout.Active {
		t.Fatalf("unexpected defaults: %+v %+v", d, foreign)
	}
	active := f.integer("SELECT id FROM ships WHERE user_id=$1 AND active", uid)
	d.Layout.Workspaces[0].Name = strings.Repeat("👩‍🚀", 33)
	f.req(cookie, "POST", path, d, 400)
	d.Layout.Workspaces[0].Name = strings.Repeat("👩‍🚀", 32)
	d.Layout.Workspaces[0].ShipID = f.integer("SELECT id FROM ships WHERE user_id=$1 AND active", otherUID)
	f.req(cookie, "POST", path, d, 400)
	d.Layout.Workspaces[0].ShipID = active
	d.Layout.Workspaces[0].Modules = []workspaces.Module{{
		ID: "72820000-0000-4000-8000-000000000001", Type: "cargo", Binding: "fixed", ShipID: active, Filter: "quantity",
		Geometry: &workspaces.Geometry{X: 60, Y: 120, W: 420, H: 340, Pinned: true, Minimized: true},
	}}
	// Names may contain 32 multi-codepoint graphemes.
	saved := read(f.req(cookie, "POST", path, d, 200))
	replay := read(f.req(cookie, "POST", path, d, 200))
	if saved.Revision != 2 || replay.Revision != 2 {
		t.Fatalf("save/replay: %+v %+v", saved, replay)
	}
	d.Layout.Workspaces[0].Name = "Stale"
	f.req(cookie, "POST", path, d, 409)
	d = saved
	d.Layout.Workspaces = []workspaces.Workspace{}
	f.req(cookie, "POST", path, d, 400)
	d = read(f.req(cookie, "GET", path, nil, 200))
	d.Layout.Default = foreign.Layout.Default
	f.req(cookie, "POST", path, d, 400)
	d.Layout.Default = d.Layout.Active
	d.Layout.Workspaces = append(d.Layout.Workspaces, foreign.Layout.Workspaces[0])
	f.req(cookie, "POST", path, d, 422)
	d.Layout.Workspaces = d.Layout.Workspaces[:1]
	d.Layout.Workspaces[0].Modules[0].Type = "chat"
	f.req(cookie, "POST", path, d, 400)

	// Two writers with the same base revision cannot overwrite one another.
	var wg sync.WaitGroup
	results := make(chan int, 2)
	for _, name := range []string{"First writer", "Second writer"} {
		wg.Go(func() {
			next := document{saved.Revision, saved.Layout}
			next.Layout.Workspaces = slices.Clone(saved.Layout.Workspaces)
			next.Layout.Workspaces[0].Name = name
			body, _ := json.Marshal(next)
			r := httptest.NewRequest("POST", path, strings.NewReader(string(body)))
			r.AddCookie(cookie)
			r.Header.Set("Content-Type", "application/json")
			w := httptest.NewRecorder()
			f.handler.ServeHTTP(w, r)
			results <- w.Code
		})
	}
	wg.Wait()
	statuses := []int{<-results, <-results}
	slices.Sort(statuses)
	if !slices.Equal(statuses, []int{200, 409}) {
		t.Fatalf("concurrent status: %v", statuses)
	}
	d = read(f.req(cookie, "GET", path, nil, 200))
	for i := byte('1'); i <= '9'; i++ {
		d.Layout.Workspaces = append(d.Layout.Workspaces, workspaces.Workspace{
			ID: "42820000-0000-4000-8000-00000000000" + string(i), Name: "Workspace", Modules: []workspaces.Module{},
		})
	}
	d.Layout.Active = d.Layout.Workspaces[9].ID
	d = read(f.req(cookie, "POST", path, d, 200))
	d.Layout.Workspaces = append(d.Layout.Workspaces, workspaces.Workspace{
		ID: "52820000-0000-4000-8000-000000000001", Name: "Eleventh", Modules: []workspaces.Module{},
	})
	f.req(cookie, "POST", path, d, 400)
	if got := f.integer("SELECT id FROM ships WHERE user_id=$1 AND active", uid); got != active {
		t.Fatalf("workspace operation changed active ship: %d -> %d", active, got)
	}
	if got := read(f.req(other, "GET", path, nil, 200)); got.Layout.Active != foreign.Layout.Active || got.Revision != 1 {
		t.Fatalf("other account changed: %+v", got)
	}
	if got := f.integer("SELECT count(*) FROM pilot_workspace_preferences WHERE user_id=$1", uid); got != 1 {
		t.Fatalf("workspace preferences rows: %d", got)
	}
}
