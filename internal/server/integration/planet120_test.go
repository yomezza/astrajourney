package integration

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"reflect"
	"sync"
	"testing"
	"time"

	"github.com/yomezza/cosmosimulator/internal/colonies"
	"github.com/yomezza/cosmosimulator/internal/geography"
	"github.com/yomezza/cosmosimulator/internal/server"
	"github.com/yomezza/cosmosimulator/internal/universe"
	"github.com/yomezza/cosmosimulator/internal/wallet"
)

func TestPlanetGeography120(t *testing.T) {
	p := universe.Planet{Name: "Test", Kind: "Садовый мир", Mass: 1, Gravity: 1, Temperature: 290, Resources: []string{"iron", "water"}}
	seed := fmt.Sprintf("%064x", 12345)
	for _, count := range []int{50, 100, 160, 200} {
		g := geography.Generate(p, seed, count)
		if len(g.Sectors) != count {
			t.Fatal("wrong sector count")
		}
		if !reflect.DeepEqual(g, geography.Generate(p, seed, count)) {
			t.Fatal("unstable generation")
		}
		if len(g.Rivers) == 0 {
			t.Fatal("missing river network")
		}
		land, water := 0, 0
		for _, v := range g.Water {
			if v > 0 {
				water++
			} else {
				land++
			}
		}
		if land == 0 || water == 0 {
			t.Fatal("missing continents/ocean")
		}
		for _, s := range g.Sectors {
			if s.Land < 0 || s.Land > 100 || s.Number < 1 || s.Number > count {
				t.Fatal("invalid sector")
			}
			for _, neighbor := range s.Neighbors {
				other := g.Sectors[neighbor-1]
				found := false
				for _, back := range other.Neighbors {
					found = found || back == s.Number
				}
				if !found {
					t.Fatal("nonreciprocal neighbors")
				}
			}
		}
	}
	other := geography.Generate(p, fmt.Sprintf("%064x", 12346), 100)
	if reflect.DeepEqual(other, geography.Generate(p, seed, 100)) {
		t.Fatal("planets share geography")
	}
}

func setupPlanet120(t *testing.T) (*fixture033, *http.Cookie, int64, int64, []int) {
	f := newFixture033(t)
	c, uid := f.signup("colonist120")
	f.exec(`UPDATE users SET level=30,credits=2000000 WHERE id=$1`, uid)
	g, err := f.app.World.Load(f.ctx)
	if err != nil {
		t.Fatal(err)
	}
	p := &g.Stars[0].Planets[0]
	p.Kind = "Садовый мир"
	p.Mass = 1
	p.Gravity = 1
	p.Temperature = 290
	p.Resources = []string{"iron", "water"}
	raw, _ := json.Marshal(g)
	f.writeWorld(raw)
	f.exec(`INSERT INTO surveys(user_id,star) VALUES($1,1) ON CONFLICT DO NOTHING`, uid)
	r := f.req(c, "GET", fmt.Sprintf("/api/planet/surface?star=1&planet=%d", p.ID), nil, 200)
	var result struct {
		ID        int64                   `json:"id"`
		Geography geography.PlanetSurface `json:"geography"`
	}
	if err = json.Unmarshal(r.Body.Bytes(), &result); err != nil {
		t.Fatal(err)
	}
	sectors := []int{}
	for _, s := range result.Geography.Sectors {
		if s.Land >= 30 {
			sectors = append(sectors, s.Number)
		}
	}
	if len(sectors) < 8 {
		t.Fatal("insufficient land")
	}
	return f, c, uid, result.ID, sectors
}

func planetReq120(f *fixture033, c *http.Cookie, in map[string]any) *httptest.ResponseRecorder {
	raw, _ := json.Marshal(in)
	r := httptest.NewRequest("POST", "/api/planet/action", bytes.NewReader(raw))
	r.AddCookie(c)
	r.Header.Set("Content-Type", "application/json")
	r.RemoteAddr = "127.0.0.1:1234"
	w := httptest.NewRecorder()
	f.handler.ServeHTTP(w, r)
	return w
}

func TestPlanetProjects120(t *testing.T) {
	f, c, uid, surface, sectors := setupPlanet120(t)
	create := func(index int) int64 {
		r := f.req(c, "POST", "/api/planet/action", map[string]any{"action": "create", "surface": surface, "sector": sectors[index], "request": fmt.Sprintf("create-120-%d", index)}, 200)
		var v struct {
			ID int64 `json:"project"`
		}
		if err := json.Unmarshal(r.Body.Bytes(), &v); err != nil {
			t.Fatal(err)
		}
		return v.ID
	}
	f.exec(`UPDATE users SET level=29 WHERE id=$1`, uid)
	f.req(c, "POST", "/api/planet/action", map[string]any{"action": "create", "surface": surface, "sector": sectors[0], "request": "level120-deny"}, 409)
	f.exec(`UPDATE users SET level=30 WHERE id=$1`, uid)
	pid := create(0)
	// Same request cannot create twice; a different request cannot reserve the same sector.
	f.req(c, "POST", "/api/planet/action", map[string]any{"action": "create", "surface": surface, "sector": sectors[0], "request": "create-120-0"}, 200)
	f.req(c, "POST", "/api/planet/action", map[string]any{"action": "create", "surface": surface, "sector": sectors[0], "request": "create-120-race"}, 409)
	in := map[string]any{"action": "contribute", "id": pid, "resource": "credits", "amount": 101, "request": "fund-120-one"}
	f.req(c, "POST", "/api/planet/action", in, 200)
	f.req(c, "POST", "/api/planet/action", in, 200)
	if got := f.integer(`SELECT credits FROM users WHERE id=$1`, uid); got != 1999899 {
		t.Fatal("duplicate debit", got)
	}
	in["amount"] = 102
	f.req(c, "POST", "/api/planet/action", in, 409)
	own := f.integer(`SELECT id FROM planet_contributions WHERE project=$1`, pid)
	f.req(c, "POST", "/api/planet/action", map[string]any{"action": "withdraw", "id": pid, "contribution": own, "request": "withdraw-120-one"}, 200)
	if got := f.integer(`SELECT credits FROM users WHERE id=$1`, uid); got != 2000000 {
		t.Fatal("full withdrawal failed", got)
	}
	f.req(c, "POST", "/api/planet/action", map[string]any{"action": "contribute", "id": pid, "resource": "credits", "amount": 101, "request": "fund-120-two"}, 200)
	f.exec(`INSERT INTO warehouses(user_id,star,station,cargo) VALUES($1,1,'station','{"colony_core":1,"iron":1000}') ON CONFLICT(user_id,star,station) DO UPDATE SET cargo=EXCLUDED.cargo`, uid)
	for _, item := range []struct {
		id string
		n  int
	}{{"colony_core", 1}, {"iron", 101}} {
		f.req(c, "POST", "/api/planet/action", map[string]any{"action": "contribute", "id": pid, "resource": item.id, "amount": item.n, "request": "fund-120-" + item.id}, 200)
	}
	f.req(c, "POST", "/api/planet/action", map[string]any{"action": "cancel", "id": pid, "request": "cancel-120-one"}, 200)
	if got := f.integer(`SELECT credits FROM users WHERE id=$1`, uid); got != 1999989 {
		t.Fatal("90% refund wrong", got)
	}
	if got := f.integer(`SELECT (cargo->>'colony_core')::int FROM warehouses WHERE user_id=$1 AND star=1`, uid); got != 1 {
		t.Fatal("indivisible core lost")
	}
	if got := f.integer(`SELECT (cargo->>'iron')::int FROM warehouses WHERE user_id=$1 AND star=1`, uid); got != 989 {
		t.Fatal("material refund wrong", got)
	}
	f.req(c, "POST", "/api/planet/action", map[string]any{"action": "cancel", "id": pid, "request": "cancel-120-other"}, 409)
	otherProject := create(3)
	f.req(c, "POST", "/api/planet/action", map[string]any{"action": "contribute", "id": otherProject, "resource": "colony_core", "amount": 1, "request": "core120-reserved"}, 200)
	another := create(4)
	f.req(c, "POST", "/api/planet/action", map[string]any{"action": "contribute", "id": another, "resource": "colony_core", "amount": 1, "request": "core120-other-project"}, 409)
	// The refunded core is now reserved in the second project and cannot be reused.
	f.req(c, "POST", "/api/planet/action", map[string]any{"action": "contribute", "id": otherProject, "resource": "colony_core", "amount": 1, "request": "core120-overfill"}, 409)
	pid = create(1)
	f.req(c, "POST", "/api/planet/action", map[string]any{"action": "admin_fill", "id": pid, "request": "fill-120-deny"}, 403)
	f.exec(`UPDATE users SET role='admin' WHERE id=$1`, uid)
	f.req(c, "POST", "/api/planet/action", map[string]any{"action": "admin_fill", "id": pid, "request": "fill-120-allow"}, 200)
	if got := f.integer(`SELECT count(*) FROM planet_contributions WHERE project=$1 AND NOT admin_grant`, pid); got != 0 {
		t.Fatal("admin forged a player contribution")
	}
	f.req(c, "POST", "/api/planet/action", map[string]any{"action": "cancel", "id": pid, "request": "cancel-120-building"}, 409)
	f.exec(`UPDATE planet_projects SET build_ticks=2 WHERE id=$1`, pid)
	if err := f.app.Simulation.AdvanceTick(f.ctx, true); err != nil {
		t.Fatal(err)
	}
	if f.integer(`SELECT progress FROM planet_projects WHERE id=$1`, pid) != 1 {
		t.Fatal("tick not applied")
	}
	tx, err := f.db.Begin(f.ctx)
	if err != nil {
		t.Fatal(err)
	}
	tick := f.integer(`SELECT tick FROM world WHERE id=1`)
	if err = colonies.AdvanceColonies(f.ctx, tx, tick); err != nil {
		t.Fatal(err)
	}
	if err = colonies.AdvanceColonies(f.ctx, tx, tick); err != nil {
		t.Fatal(err)
	}
	if err = tx.Commit(f.ctx); err != nil {
		t.Fatal(err)
	}
	if f.integer(`SELECT progress FROM planet_projects WHERE id=$1`, pid) != 1 {
		t.Fatal("same impulse counted twice")
	}
	// Reload persisted state with a fresh App and process another tick.
	next := server.New(f.db, server.Config{})
	if err := next.Simulation.AdvanceTick(f.ctx, true); err != nil {
		t.Fatal(err)
	}
	if f.integer(`SELECT count(*) FROM planet_colonies WHERE project=$1`, pid) != 1 {
		t.Fatal("colony not created")
	}
	if err := next.Simulation.AdvanceTick(f.ctx, true); err != nil {
		t.Fatal(err)
	}
	if f.integer(`SELECT count(*) FROM notifications WHERE kind='colony' AND user_id=$1`, uid) != 1 {
		t.Fatal("duplicate completion")
	}
	f.req(c, "POST", "/api/planet/action", map[string]any{"action": "land", "id": pid, "request": "land-120-remote"}, 409)
	var planet int
	f.db.QueryRow(f.ctx, `SELECT planet FROM planet_surfaces WHERE id=$1`, surface).Scan(&planet)
	f.exec(`UPDATE ships SET object_id=$2,position_mode='orbit',destination=NULL,local_target=NULL WHERE user_id=$1 AND active`, uid, fmt.Sprintf("planet-%d", planet))
	f.req(c, "POST", "/api/planet/action", map[string]any{"action": "land", "id": pid, "request": "land-120-orbit"}, 409)
	pid = create(2)
	f.exec(`UPDATE planet_projects SET expires_at=now()-interval '1 second' WHERE id=$1`, pid)
	if err := f.app.Simulation.AdvanceTick(f.ctx, true); err != nil {
		t.Fatal(err)
	}
	if f.integer(`SELECT count(*) FROM planet_projects WHERE id=$1 AND status='expired'`, pid) != 1 {
		t.Fatal("expiry not recovered")
	}
}

func TestPlanetConcurrencyPrivacy120(t *testing.T) {
	f, c, uid, surface, sectors := setupPlanet120(t)
	other, oid := f.signup("otherplanet120")
	f.exec(`UPDATE users SET level=30 WHERE id=$1`, oid)
	var star, planet int
	f.db.QueryRow(f.ctx, `SELECT star,planet FROM planet_surfaces WHERE id=$1`, surface).Scan(&star, &planet)
	f.req(other, "GET", fmt.Sprintf("/api/planet/surface?star=%d&planet=%d", star, planet), nil, 403)
	f.req(other, "GET", fmt.Sprintf("/api/planet/state?surface=%d", surface), nil, 403)
	f.exec(`INSERT INTO surveys(user_id,star) VALUES($1,$2)`, oid, star)
	var wg sync.WaitGroup
	codes := make(chan int, 2)
	for i, cookie := range []*http.Cookie{c, other} {
		wg.Add(1)
		go func(i int, cookie *http.Cookie) {
			defer wg.Done()
			w := planetReq120(f, cookie, map[string]any{"action": "create", "surface": surface, "sector": sectors[0], "request": fmt.Sprintf("concurrent-120-%d", i)})
			codes <- w.Code
		}(i, cookie)
	}
	wg.Wait()
	close(codes)
	success := 0
	for code := range codes {
		if code == 200 {
			success++
		} else if code != 409 {
			t.Fatal("unexpected create status", code)
		}
	}
	if success != 1 {
		t.Fatal("reservation race", success)
	}
	pid := f.integer(`SELECT id FROM planet_projects WHERE surface=$1 AND sector=$2`, surface, sectors[0])
	owner := f.integer(`SELECT owner_user FROM planet_projects WHERE id=$1`, pid)
	stranger := other
	if owner == oid {
		stranger = c
	}
	f.req(stranger, "GET", fmt.Sprintf("/api/planet/project?id=%d", pid), nil, 403)
	for i := 1; i < 6; i++ {
		cookie := c
		id := uid
		if owner == oid {
			cookie = other
			id = oid
		}
		_ = id
		expected := 200
		if i == 5 {
			expected = 409
		}
		f.req(cookie, "POST", "/api/planet/action", map[string]any{"action": "create", "surface": surface, "sector": sectors[i], "request": fmt.Sprintf("limit-120-%d", i)}, expected)
	}
	// Fifty authenticated readers share persisted geometry without changing it.
	var rawBefore string
	f.db.QueryRow(f.ctx, `SELECT md5(data::text) FROM planet_surfaces WHERE id=$1`, surface).Scan(&rawBefore)
	started := time.Now()
	for range 50 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			r := httptest.NewRequest("GET", fmt.Sprintf("/api/planet/surface?star=%d&planet=%d", star, planet), nil)
			r.AddCookie(c)
			w := httptest.NewRecorder()
			f.handler.ServeHTTP(w, r)
			if w.Code != 200 {
				t.Errorf("reader %d", w.Code)
			}
		}()
	}
	wg.Wait()
	var after string
	f.db.QueryRow(f.ctx, `SELECT md5(data::text) FROM planet_surfaces WHERE id=$1`, surface).Scan(&after)
	if rawBefore != after {
		t.Fatal("surface mutated")
	}
	t.Log("50 map reads:", time.Since(started))
}

func TestPlanetCorporateRightsAndRefunds120(t *testing.T) {
	f, owner, uid, surface, sectors := setupPlanet120(t)
	employee, eid := f.signup("employee120")
	f.exec(`UPDATE users SET level=30,credits=1000 WHERE id=$1`, eid)
	var cid, rid int64
	if err := f.db.QueryRow(f.ctx, `INSERT INTO companies(name,tag,owner_id) VALUES('Planet Test','PT12',$1) RETURNING id`, uid).Scan(&cid); err != nil {
		t.Fatal(err)
	}
	f.db.QueryRow(f.ctx, `INSERT INTO company_roles(company_id,name,permissions) VALUES($1,'Colonist','["colonize"]') RETURNING id`, cid).Scan(&rid)
	f.exec(`UPDATE users SET company_id=$2,company_role=$3 WHERE id IN($1,$4)`, uid, cid, rid, eid)
	f.exec(`INSERT INTO surveys(user_id,star) VALUES($1,1)`, eid)
	create := map[string]any{"action": "create", "surface": surface, "sector": sectors[0], "corporate": true, "request": "corp120-create"}
	f.req(employee, "POST", "/api/planet/action", create, 200)
	pid := f.integer(`SELECT id FROM planet_projects WHERE surface=$1 AND sector=$2`, surface, sectors[0])
	if f.integer(`SELECT owner_company FROM planet_projects WHERE id=$1`, pid) != cid {
		t.Fatal("corporate ownership missing")
	}
	f.exec(`UPDATE company_roles SET permissions='[]' WHERE id=$1`, rid)
	f.req(employee, "POST", "/api/planet/action", map[string]any{"action": "cancel", "id": pid, "request": "corp120-cancel-deny"}, 409)
	f.req(employee, "POST", "/api/planet/action", map[string]any{"action": "contribute", "id": pid, "resource": "credits", "amount": 100, "request": "corp120-fund"}, 200)
	contribution := f.integer(`SELECT id FROM planet_contributions WHERE project=$1`, pid)
	f.req(owner, "POST", "/api/planet/action", map[string]any{"action": "withdraw", "id": pid, "contribution": contribution, "request": "corp120-steal"}, 409)
	f.exec(`UPDATE users SET company_id=NULL,company_role=NULL WHERE id=$1`, eid)
	f.req(employee, "POST", "/api/planet/action", map[string]any{"action": "withdraw", "id": pid, "contribution": contribution, "request": "corp120-refund-after-leave"}, 200)
	f.req(owner, "POST", "/api/planet/action", map[string]any{"action": "cancel", "id": pid, "request": "corp120-cancel-owner"}, 200)
	f.req(owner, "POST", "/api/planet/action", map[string]any{"action": "create", "surface": surface, "sector": sectors[1], "request": "pending120-create"}, 200)
	pid = f.integer(`SELECT id FROM planet_projects WHERE surface=$1 AND sector=$2`, surface, sectors[1])
	f.req(owner, "POST", "/api/planet/action", map[string]any{"action": "contribute", "id": pid, "resource": "credits", "amount": 100, "request": "pending120-fund"}, 200)
	f.exec(`UPDATE users SET credits=$2 WHERE id=$1`, uid, wallet.MaxCredits)
	f.req(owner, "POST", "/api/planet/action", map[string]any{"action": "cancel", "id": pid, "request": "pending120-cancel"}, 200)
	refund := f.integer(`SELECT id FROM planet_refunds WHERE user_id=$1 AND NOT claimed`, uid)
	if f.integer(`SELECT amount FROM planet_refunds WHERE id=$1`, refund) != 90 {
		t.Fatal("escrow refund lost")
	}
	f.exec(`UPDATE users SET credits=credits-90 WHERE id=$1`, uid)
	claim := map[string]any{"action": "claim_refund", "id": refund, "request": "pending120-claim"}
	f.req(owner, "POST", "/api/planet/action", claim, 200)
	f.req(owner, "POST", "/api/planet/action", claim, 200)
	if f.integer(`SELECT credits FROM users WHERE id=$1`, uid) != wallet.MaxCredits {
		t.Fatal("refund duplicated")
	}
}

func TestPlanetSettingsSnapshot120(t *testing.T) {
	f, c, uid, surface, sectors := setupPlanet120(t)
	config, err := colonies.ReadConfig(f.ctx, f.db)
	if err != nil {
		t.Fatal(err)
	}
	f.req(c, "POST", "/api/admin/planet/settings", config, 403)
	f.exec(`UPDATE users SET role='admin' WHERE id=$1`, uid)
	f.req(c, "POST", "/api/planet/action", map[string]any{"action": "create", "surface": surface, "sector": sectors[0], "request": "snapshot120-create"}, 200)
	pid := f.integer(`SELECT id FROM planet_projects WHERE surface=$1 AND sector=$2`, surface, sectors[0])
	config.Cost["iron"] = 123
	config.BuildTicks = 3
	config.Counts = []int{60, 120, 170, 200}
	f.req(c, "POST", "/api/admin/planet/settings", config, 200)
	if f.integer(`SELECT (required->>'iron')::int FROM planet_projects WHERE id=$1`, pid) != 10000 || f.integer(`SELECT build_ticks FROM planet_projects WHERE id=$1`, pid) != 120 {
		t.Fatal("existing commitments changed")
	}
	var data []byte
	f.db.QueryRow(f.ctx, `SELECT data FROM planet_surfaces WHERE id=$1`, surface).Scan(&data)
	var surfaceData geography.PlanetSurface
	json.Unmarshal(data, &surfaceData)
	if len(surfaceData.Sectors) != 100 {
		t.Fatal("old surface regenerated")
	}
}

func TestPlanetContributionPages120(t *testing.T) {
	f, c, uid, surface, sectors := setupPlanet120(t)
	f.req(c, "POST", "/api/planet/action", map[string]any{"action": "create", "surface": surface, "sector": sectors[0], "request": "history120-create"}, 200)
	pid := f.integer(`SELECT id FROM planet_projects WHERE surface=$1 AND sector=$2`, surface, sectors[0])
	// A consistent historical fixture exceeding one page.
	f.exec(`INSERT INTO planet_contributions(project,actor,resource,amount) SELECT $1,$2,'credits',1 FROM generate_series(1,61)`, pid, uid)
	f.exec(`UPDATE users SET credits=credits-61 WHERE id=$1`, uid)
	f.exec(`UPDATE planet_projects SET received='{"credits":61}' WHERE id=$1`, pid)
	var page struct {
		Items []struct {
			ID int64 `json:"id"`
		} `json:"contributions"`
	}
	r := f.req(c, "GET", fmt.Sprintf("/api/planet/project?id=%d", pid), nil, 200)
	if err := json.Unmarshal(r.Body.Bytes(), &page); err != nil {
		t.Fatal(err)
	}
	if len(page.Items) != 50 {
		t.Fatal("unbounded or missing first page")
	}
	cursor := page.Items[len(page.Items)-1].ID
	r = f.req(c, "GET", fmt.Sprintf("/api/planet/project?id=%d&contributions_before=%d", pid, cursor), nil, 200)
	if err := json.Unmarshal(r.Body.Bytes(), &page); err != nil {
		t.Fatal(err)
	}
	if len(page.Items) != 11 || page.Items[0].ID >= cursor {
		t.Fatal("lost historical contributions")
	}
	f.req(c, "POST", "/api/planet/action", map[string]any{"action": "withdraw", "id": pid, "contribution": page.Items[len(page.Items)-1].ID, "request": "history120-withdraw"}, 200)
}
