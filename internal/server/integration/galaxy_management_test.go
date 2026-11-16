package integration

import (
	"encoding/json"
	"fmt"
	"testing"

	"github.com/yomezza/cosmosimulator/internal/universe"
)

func TestManagedGalaxyPreviewAndApply(t *testing.T) {
	f := newFixture033(t)
	admin, uid := f.signup("galaxyadmin050")
	player, _ := f.signup("galaxyplayer050")
	f.exec(`UPDATE users SET role='admin' WHERE id=$1`, uid)
	f.req(player, "GET", "/api/admin/galaxies", nil, 403)
	f.req(player, "POST", "/api/admin/galaxies/preview", universe.GalaxyEdit{}, 403)
	f.req(player, "POST", "/api/admin/galaxies/apply", universe.GalaxyEdit{}, 403)
	g, err := f.app.World.Load(f.ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(universe.GalaxyStars(g, "edem")) != 3000 || g.Definition("asterion").Name != "Астерион" {
		t.Fatal("wrong initial catalogue")
	}
	if len(g.Lanes) >= 4500 {
		t.Fatal("default lanes are too dense", len(g.Lanes))
	}
	cross := 0
	for _, e := range g.Lanes {
		if g.Stars[e.From-1].Region != g.Stars[e.To-1].Region {
			cross++
		}
	}
	if cross < 4 || len(g.Path(1, 2)) == 0 {
		t.Fatal("arms have no bridges", cross)
	}
	if n := f.integer(`SELECT count(DISTINCT star) FROM stations WHERE active`); n < 10 || n > 300 {
		t.Fatal("station percentage wrong", n)
	}
	// Protect existing property at an otherwise undesired port, including custom IDs.
	f.exec(`INSERT INTO stations(star,object_id,name,specialization) VALUES(3,'station-private','Private port','metals')`)
	f.exec(`INSERT INTO warehouses(user_id,star,station,cargo) VALUES($1,3,'station-private','{"iron":7}')`, uid)
	f.exec(`UPDATE ships SET destination=5,arrival=now()+interval '1 hour',departure=now(),travel_path='[1,5]',waypoints='[9]',reserved_fuel=10 WHERE user_id=$1`, uid)
	in := universe.GalaxyEdit{ID: "edem", Name: "Edem", Systems: 3012, Settings: universe.DefaultGalaxySettings()}
	in.Settings.StationPercent = 0
	in.Settings.ExtraLanes = 0
	in.Settings.CrossLinks = 4
	in.Settings.ResourceWeights["vajra"] = 0
	response := f.req(admin, "POST", "/api/admin/galaxies/preview", in, 200)
	var preview struct {
		Preview string `json:"preview"`
		After   struct {
			Systems int `json:"systems"`
			Lanes   int `json:"lanes"`
		} `json:"after"`
	}
	if err = json.Unmarshal(response.Body.Bytes(), &preview); err != nil {
		t.Fatal(err)
	}
	if preview.Preview == "" || preview.After.Systems != 3012 {
		t.Fatal("invalid preview")
	}
	if f.integer(`SELECT count(*) FROM star_systems`) != 3000 || f.integer(`SELECT count(*) FROM admin_log WHERE action='galaxy_update'`) != 0 {
		t.Fatal("preview mutated world")
	}
	f.req(admin, "POST", "/api/admin/galaxies/apply", in, 409)
	in.Preview = preview.Preview
	f.req(admin, "POST", "/api/admin/galaxies/apply", in, 200)
	updated, err := f.app.World.Load(f.ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(updated.Stars) != 3012 || updated.Revision != g.Revision+1 {
		t.Fatal("universe cache stale")
	}
	for i, old := range g.Stars {
		if updated.Stars[i].ID != old.ID || updated.Stars[i].Name != old.Name || updated.Stars[i].X != old.X || updated.Stars[i].Y != old.Y {
			t.Fatal("expansion changed existing identity", old.ID)
		}
	}
	f.req(admin, "POST", "/api/admin/galaxies/apply", in, 409)
	if f.integer(`SELECT count(*) FROM stations WHERE star=3 AND object_id='station-private' AND active`) != 1 || f.integer(`SELECT (cargo->>'iron')::int FROM warehouses WHERE user_id=$1 AND star=3`, uid) != 7 {
		t.Fatal("protected port/property lost")
	}
	if f.integer(`SELECT count(DISTINCT star) FROM stations WHERE active`) > 20 {
		t.Fatal("station setting did not deactivate unused ports")
	}
	if f.integer(`SELECT count(*) FROM stations WHERE NOT active`) == 0 {
		t.Fatal("old port inventory was not archived")
	}
	if f.integer(`SELECT reserved_fuel FROM ships WHERE user_id=$1`, uid) != 2 {
		t.Fatal("surplus route fuel reservation was not released")
	}
	for _, s := range updated.Stars {
		for _, site := range universe.SystemSites(s) {
			for _, r := range site.Resources {
				if r == "vajra" {
					t.Fatal("zero resource weight ignored")
				}
			}
		}
	}
	// A flight and its queued route retain valid corridors after sparsification.
	for _, pair := range [][2]int{{1, 5}, {5, 9}} {
		path := updated.Path(pair[0], pair[1])
		oldPath := g.Path(pair[0], pair[1])
		if len(path) == 0 || len(path) > max(2, len(oldPath)) {
			t.Fatal("committed route became invalid", pair, path)
		}
	}
	f.req(admin, "GET", "/api/admin/changes?search=galaxyadmin050&action=galaxy_update", nil, 200)
	f.req(player, "GET", "/api/admin/changes", nil, 403)
	f.req(player, "GET", "/api/events", nil, 403)
	if f.integer(`SELECT count(*) FROM admin_log WHERE action='galaxy_update' AND actor=$1`, uid) != 1 {
		t.Fatal("missing change history")
	}
	// A changed preview requires a fresh review, and invalid settings never apply.
	in.Systems = 3000
	f.req(admin, "POST", "/api/admin/galaxies/preview", in, 400)
	in.Systems = 3012
	in.Settings.CrossLinks = 41
	f.req(admin, "POST", "/api/admin/galaxies/preview", in, 400)
}

func TestPartialSystemDiscovery(t *testing.T) {
	f := newFixture033(t)
	cookie, uid := f.signup("discovery050")
	read := func(star int) struct {
		Star     universe.Star   `json:"star"`
		Sites    []universe.Site `json:"sites"`
		Surveyed bool            `json:"surveyed"`
	} {
		t.Helper()
		response := f.req(cookie, "GET", fmt.Sprintf("/api/system?id=%d", star), nil, 200)
		var data struct {
			Star     universe.Star   `json:"star"`
			Sites    []universe.Site `json:"sites"`
			Surveyed bool            `json:"surveyed"`
		}
		if err := json.Unmarshal(response.Body.Bytes(), &data); err != nil {
			t.Fatal(err)
		}
		return data
	}
	initial := read(1)
	if len(initial.Sites) != 0 || len(initial.Star.Planets) != 0 || initial.Star.ResourceWeights != nil || initial.Star.Ports != nil {
		t.Fatal("unscanned objects leaked")
	}
	f.req(cookie, "POST", "/api/expedition", map[string]any{"action": "move", "target": "planet-1", "mode": "orbit"}, 409)
	f.req(cookie, "POST", "/api/expedition", map[string]any{"action": "scan"}, 200)
	sid := f.integer(`SELECT id FROM ships WHERE user_id=$1`, uid)
	f.exec(`UPDATE research_jobs SET scanned=2,paused=TRUE WHERE ship_id=$1`, sid)
	partial := read(1)
	if len(partial.Sites) != 2 || partial.Surveyed || partial.Sites[0].Details == nil {
		t.Fatal("partial scan failed", partial)
	}
	if len(read(2).Sites) != 0 {
		t.Fatal("another system leaked")
	}
	f.exec(`UPDATE research_jobs SET paused=FALSE,next_at=now()-interval '1 day' WHERE ship_id=$1`, sid)
	finishResearchForTest(t, f.app)
	full := read(1)
	if !full.Surveyed || len(full.Sites) <= 2 {
		t.Fatal("completed scan did not reveal all objects")
	}
}

func TestAdminChangeLogPages(t *testing.T) {
	f := newFixture033(t)
	admin, uid := f.signup("audit050")
	f.exec(`UPDATE users SET role='admin' WHERE id=$1`, uid)
	f.exec(`INSERT INTO admin_log(actor,action,data) SELECT $1,'tick',jsonb_build_object('number',n) FROM generate_series(1,25) n`, uid)
	f.exec(`INSERT INTO admin_log(actor,action,data) VALUES($1,'galaxy_update','{"name":"Edem"}')`, uid)
	var data struct {
		Items              []map[string]any `json:"items"`
		Total, Page, Pages int
	}
	for _, tc := range []struct {
		query                     string
		count, total, page, pages int
	}{{"?search=AUDIT050", 20, 26, 1, 2}, {"?page=2&search=audit050", 6, 26, 2, 2}, {"?action=galaxy_update", 1, 1, 1, 1}, {"?search=missing", 0, 0, 1, 1}, {"?page=99&action=tick", 5, 25, 2, 2}} {
		response := f.req(admin, "GET", "/api/admin/changes"+tc.query, nil, 200)
		if err := json.Unmarshal(response.Body.Bytes(), &data); err != nil {
			t.Fatal(err)
		}
		if len(data.Items) != tc.count || data.Total != tc.total || data.Page != tc.page || data.Pages != tc.pages {
			t.Fatalf("wrong log page %s: %+v", tc.query, data)
		}
	}
}
