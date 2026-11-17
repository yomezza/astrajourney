package integration

import (
	"encoding/json"
	"fmt"
	"math"
	"net/http"
	"reflect"
	"strings"
	"sync"
	"testing"

	"github.com/yomezza/cosmosimulator/internal/colonies"
	"github.com/yomezza/cosmosimulator/internal/geography"
	"github.com/yomezza/cosmosimulator/internal/platform/dblayout"
)

func landingFixture160(t *testing.T) (*fixture033, *http.Cookie, int64, int64, geography.PlanetSurface) {
	f, c, uid, surface, _ := setupPlanet120(t)
	g := geography.PlanetSurface{SeaLevel: 460, Width: 19, Height: 4, GridW: 191, GridH: 41, Temperature: 15}
	g.Heights = make([]int, g.GridW*g.GridH)
	g.Water = make([]int, len(g.Heights))
	for i := range g.Heights {
		g.Heights[i] = 700
	}
	for i := 0; i < 10; i++ {
		s := geography.PlanetSector{Number: i + 1, Column: i, Center: geography.SurfacePoint{X: 1 + float64(i)*math.Sqrt(3), Y: 2}, Land: 100, Estimates: map[string]string{}}
		if i > 0 {
			s.Neighbors = append(s.Neighbors, i)
		}
		if i < 9 {
			s.Neighbors = append(s.Neighbors, i+2)
		}
		g.Sectors = append(g.Sectors, s)
	}
	raw, _ := json.Marshal(g)
	f.exec(`UPDATE planet_surfaces SET data=$2 WHERE id=$1`, surface, raw)
	f.exec(`DELETE FROM planet_sectors WHERE surface=$1`, surface)
	for _, s := range g.Sectors {
		raw, _ := json.Marshal(s)
		f.exec(`INSERT INTO planet_sectors(surface,number,data) VALUES($1,$2,$3)`, surface, s.Number, raw)
	}
	return f, c, uid, surface, g
}

func addColony160(f *fixture033, uid, surface int64, sector int, company *int64, building bool) int64 {
	f.t.Helper()
	status, progress := "complete", 1
	if building {
		status = "building"
		progress = 0
	}
	var project int64
	var user *int64
	if company == nil {
		user = &uid
	}
	err := f.db.QueryRow(f.ctx, `INSERT INTO planet_projects(surface,sector,initiator,owner_user,owner_company,status,required,received,build_ticks,progress,expires_at,refund_percent) VALUES($1,$2,$3,$4,$5,$6,'{}','{}',1,$7,now()+interval '1 day',90) RETURNING id`, surface, sector, uid, user, company, status, progress).Scan(&project)
	if err != nil {
		f.t.Fatal(err)
	}
	if !building {
		f.exec(`INSERT INTO planet_colonies(project,surface,sector,owner_user,owner_company) VALUES($1,$2,$3,$4,$5)`, project, surface, sector, user, company)
	}
	return project
}

func reconcile160(f *fixture033) {
	f.t.Helper()
	_, err := f.app.Colonies.ReconcileLandingSites(f.ctx)
	if err != nil {
		f.t.Fatal(err)
	}
}

func TestLandingTerritories160(t *testing.T) {
	f, _, uid, surface, _ := landingFixture160(t)
	credit := f.integer(`SELECT credits FROM users WHERE id=$1`, uid)
	addColony160(f, uid, surface, 1, nil, false)
	reconcile160(f)
	if f.integer(`SELECT count(*) FROM planet_landing_sites`) != 1 {
		t.Fatal("first territory missing site")
	}
	var original string
	f.db.QueryRow(f.ctx, `SELECT row(id,colony,x_q,y_q,spawn_x_q,spawn_y_q)::text FROM planet_landing_sites`).Scan(&original)
	addColony160(f, uid, surface, 2, nil, false)
	reconcile160(f)
	if f.integer(`SELECT count(*) FROM planet_landing_sites`) != 1 {
		t.Fatal("neighbor got extra site")
	}
	addColony160(f, uid, surface, 4, nil, false)
	reconcile160(f)
	if f.integer(`SELECT count(*) FROM planet_landing_sites`) != 2 {
		t.Fatal("remote territory missing site")
	}
	addColony160(f, uid, surface, 3, nil, false)
	reconcile160(f)
	if f.integer(`SELECT count(*) FROM planet_landing_sites`) != 2 {
		t.Fatal("bridge created a third site")
	}
	var after string
	f.db.QueryRow(f.ctx, `SELECT row(id,colony,x_q,y_q,spawn_x_q,spawn_y_q)::text FROM planet_landing_sites ORDER BY id LIMIT 1`).Scan(&after)
	if original != after {
		t.Fatal("merge moved existing site")
	}
	reconcile160(f)
	if f.integer(`SELECT count(*) FROM notifications WHERE kind='landing_ready'`) != 2 {
		t.Fatal("replay duplicated readiness notification")
	}
	if f.integer(`SELECT credits FROM users WHERE id=$1`, uid) != credit {
		t.Fatal("initial infrastructure charged player")
	}
	// A single connected three-sector territory loses its middle, leaving an
	// unserved component. This is a controlled fixture mutation, not a new API.
	other, otherID := f.signup("split160")
	_ = other
	addColony160(f, otherID, surface, 7, nil, false)
	addColony160(f, otherID, surface, 8, nil, false)
	addColony160(f, otherID, surface, 9, nil, false)
	reconcile160(f)
	f.exec(`DELETE FROM planet_colonies WHERE surface=$1 AND sector=8`, surface)
	reconcile160(f)
	if f.integer(`SELECT count(*) FROM planet_landing_sites l JOIN planet_colonies c ON c.id=l.colony WHERE c.owner_user=$1`, otherID) != 2 {
		t.Fatal("split left component without site")
	}
}

func TestLandingBackfillAndConcurrentCompletion160(t *testing.T) {
	f, c, uid, surface, _ := landingFixture160(t)
	addColony160(f, uid, surface, 1, nil, true)
	addColony160(f, uid, surface, 2, nil, true)
	if err := f.app.Simulation.AdvanceTick(f.ctx, true); err != nil {
		t.Fatal(err)
	}
	if f.integer(`SELECT count(*) FROM planet_colonies`) != 2 {
		t.Fatal("projects did not complete together")
	}
	if f.integer(`SELECT count(*) FROM planet_landing_sites`) != 0 {
		t.Fatal("tick performed heavy placement")
	}
	var wg sync.WaitGroup
	failures := make(chan error, 2)
	for range 2 {
		wg.Add(1)
		go func() { defer wg.Done(); _, err := f.app.Colonies.ReconcileLandingSites(f.ctx); failures <- err }()
	}
	wg.Wait()
	close(failures)
	for err := range failures {
		if err != nil {
			t.Fatal(err)
		}
	}
	if f.integer(`SELECT count(*) FROM planet_landing_sites`) != 1 {
		t.Fatal("concurrent reconciliation duplicated site")
	}
	addColony160(f, uid, surface, 4, nil, false)
	// Simulate old completed colonies with no work record, then reapply migration.
	f.exec(`DELETE FROM planet_landing_sites WHERE surface=$1`, surface)
	f.exec(`DELETE FROM planet_runtime_controls WHERE surface=$1`, surface)
	sql, err := testAssets.ReadFile("schema160.sql")
	if err != nil {
		t.Fatal(err)
	}
	root, e := dblayout.RootFromPath(f.db.Config().ConnConfig.RuntimeParams["search_path"])
	if e != nil {
		t.Fatal(e)
	}
	f.exec(dblayout.DDL(root, string(sql)))
	reconcile160(f)
	if f.integer(`SELECT count(*) FROM planet_landing_sites`) != 2 {
		t.Fatal("backfill used one site per sector")
	}
	ids := f.integer(`SELECT sum(id) FROM planet_landing_sites`)
	f.exec(dblayout.DDL(root, string(sql)))
	reconcile160(f)
	if f.integer(`SELECT sum(id) FROM planet_landing_sites`) != ids {
		t.Fatal("repeat migration rebuilt modules")
	}
	// Reset removes new dependencies in FK order; the first-phase safety checks stay.
	f.exec(`UPDATE users SET role='admin' WHERE id=$1`, uid)
	planet := int(f.integer(`SELECT planet FROM planet_surfaces WHERE id=$1`, surface))
	info := preview124(f, c, planet)
	f.req(c, "POST", "/api/admin/planet/surface", input124(info, "delete", "delete-landing160"), 200)
	if f.integer(`SELECT count(*) FROM planet_landing_sites`) != 0 || f.integer(`SELECT count(*) FROM planet_runtime_controls`) != 0 {
		t.Fatal("reset retained site dependencies")
	}
}

func TestLandingAccessAndWater160(t *testing.T) {
	f, c, uid, surface, g := landingFixture160(t)
	addColony160(f, uid, surface, 1, nil, false)
	// Water along the common side makes a real independent territory.
	x := (g.Sectors[0].Center.X + g.Sectors[1].Center.X) / 2
	g.Rivers = [][]geography.SurfacePoint{{{X: x, Y: 0}, {X: x, Y: 4}}}
	raw, _ := json.Marshal(g)
	f.exec(`UPDATE planet_surfaces SET data=$2 WHERE id=$1`, surface, raw)
	addColony160(f, uid, surface, 2, nil, false)
	reconcile160(f)
	if f.integer(`SELECT count(*) FROM planet_landing_sites`) != 2 {
		t.Fatal("water barrier did not split territory")
	}
	var company int64
	err := f.db.QueryRow(f.ctx, `INSERT INTO companies(name,tag,color,flag,owner_id) VALUES('Terrain Test','TRN','#4488aa','orbit',$1) RETURNING id`, uid).Scan(&company)
	if err != nil {
		t.Fatal(err)
	}
	f.exec(`UPDATE users SET company_id=$2,company_role=NULL WHERE id=$1`, uid, company)
	corporateProject := addColony160(f, uid, surface, 3, &company, false)
	reconcile160(f)
	if f.integer(`SELECT count(*) FROM planet_landing_sites`) != 3 {
		t.Fatal("personal and company territory merged")
	}
	reader, readerID := f.signup("terrain_reader160")
	f.exec(`INSERT INTO surveys(user_id,star) VALUES($1,1) ON CONFLICT DO NOTHING`, readerID)
	f.req(c, "POST", "/api/workbench", map[string]any{"action": "role_save", "name": "Visitor", "permissions": []string{"colonize"}}, 200)
	role := f.integer(`SELECT id FROM company_roles WHERE company_id=$1 AND name='Visitor'`, company)
	f.exec(`UPDATE users SET company_id=$2,company_role=$3 WHERE id=$1`, readerID, company, role)
	read := func() colonies.LandingState {
		w := f.req(reader, "GET", fmt.Sprintf("/api/planet/state?surface=%d", surface), nil, 200)
		if strings.Contains(w.Body.String(), "x_q") || strings.Contains(w.Body.String(), "seed") {
			t.Fatal("global view leaked private scene data")
		}
		var result struct {
			Landing colonies.LandingState `json:"landing"`
		}
		if err := json.Unmarshal(w.Body.Bytes(), &result); err != nil {
			t.Fatal(err)
		}
		return result.Landing
	}
	planet := f.integer(`SELECT planet FROM planet_surfaces WHERE id=$1`, surface)
	f.exec(`UPDATE ships SET star=1,object_id=$2,position_mode='orbit',destination=NULL,local_target=NULL WHERE user_id=$1`, readerID, fmt.Sprintf("planet-%d", planet))
	f.req(reader, "POST", "/api/planet/action", map[string]any{"action": "land", "id": corporateProject, "request": "deny-landing160"}, 409)
	view := read()
	for _, site := range view.Sites {
		if site.Accessible {
			t.Fatal("colonize granted landing access")
		}
	}
	f.req(c, "POST", "/api/workbench", map[string]any{"action": "role_save", "id": role, "name": "Visitor", "permissions": []string{"planetary.enter"}}, 200)
	f.req(reader, "POST", "/api/planet/action", map[string]any{"action": "land", "id": corporateProject, "request": "allow-landing160"}, 409)
	view = read()
	accessible := 0
	for _, site := range view.Sites {
		if site.Accessible {
			accessible++
		}
	}
	if accessible != 1 || len(view.Territories) != 1 || !reflect.DeepEqual(view.Territories[0].Sectors, []int{3}) {
		t.Fatal("corporate visitor access incorrect", view)
	}
	f.exec(`UPDATE company_roles SET permissions='[]' WHERE id=$1`, role)
	for _, site := range read().Sites {
		if site.Accessible {
			t.Fatal("revoked access stayed cached")
		}
	}
	// A canonical ocean gives an explicit pending component, never a forced platform.
	for i := range g.Heights {
		g.Heights[i] = 100
	}
	g.Rivers = nil
	raw, _ = json.Marshal(g)
	f.exec(`UPDATE planet_surfaces SET data=$2 WHERE id=$1`, surface, raw)
	addColony160(f, uid, surface, 6, nil, false)
	reconcile160(f)
	if f.integer(`SELECT count(*) FROM planet_landing_sites WHERE sector=6`) != 0 {
		t.Fatal("placed module in ocean")
	}
	if f.integer(`SELECT jsonb_array_length(issues) FROM planet_runtime_controls WHERE surface=$1`, surface) == 0 {
		t.Fatal("unplaceable area has no reason")
	}
}
