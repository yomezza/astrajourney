package integration

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"slices"
	"strings"
	"sync"
	"testing"

	"github.com/yomezza/cosmosimulator/internal/workspaces"
)

const studioPath = "/api/studio/v2"
const workspaceV2Path = studioPath + "/layout"

type studioJSON = map[string]any

func studioDecode[T any](t *testing.T, r *httptest.ResponseRecorder) T {
	t.Helper()
	var out T
	if err := json.Unmarshal(r.Body.Bytes(), &out); err != nil {
		t.Fatal(err)
	}
	return out
}

type studioDocument struct {
	Revision int64 `json:"revision"`
	Layout   struct {
		Version    int    `json:"version"`
		Active     string `json:"active"`
		Default    string `json:"default"`
		Workspaces []struct {
			ID         string                `json:"id"`
			Name       string                `json:"name"`
			Ship       int64                 `json:"ship_id"`
			Customized bool                  `json:"customized,omitempty"`
			Modules    []workspaces.Instance `json:"modules"`
		} `json:"workspaces"`
	} `json:"layout"`
}

func TestStudioV2LayoutUpgrade(t *testing.T) {
	f := newFixture033(t)
	owner, uid := f.signup("studio_layout")
	other, _ := f.signup("studio_other")
	f.req(nil, "GET", studioPath+"/catalog", nil, 401)
	catalog := studioDecode[struct {
		Families []struct{ Variants []struct{ ID string } }
	}](t, f.req(owner, "GET", studioPath+"/catalog", nil, 200))
	if len(catalog.Families) != 3 || len(catalog.Families[0].Variants) != 1 || len(catalog.Families[1].Variants) != 3 || len(catalog.Families[2].Variants) != 2 {
		t.Fatalf("catalog: %+v", catalog)
	}
	initialized := studioDecode[studioDocument](t, f.req(other, "GET", workspaceV2Path, nil, 200))
	if len(initialized.Layout.Workspaces[0].Modules) != 4 || initialized.Layout.Workspaces[0].Ship < 1 {
		t.Fatalf("starter: %+v", initialized)
	}

	type oldDocument struct {
		Revision int64             `json:"revision"`
		Layout   workspaces.Layout `json:"layout"`
	}
	old := studioDecode[oldDocument](t, f.req(owner, "GET", "/api/workspaces", nil, 200))
	active := f.integer("SELECT id FROM ships WHERE user_id=$1 AND active", uid)
	old.Layout.Workspaces[0].Customized = true
	old.Layout.Workspaces[0].Modules = []workspaces.Module{
		{ID: "af3410ae-be24-41f8-b2ca-4cf7e7bbdf01", Type: "fleet", Binding: "fixed", ShipID: active, View: "window", Geometry: &workspaces.Geometry{X: 30, Y: 60, W: 220, H: 230, Pinned: true}},
		{ID: "af3410ae-be24-41f8-b2ca-4cf7e7bbdf02", Type: "cargo", Binding: "workspace", Filter: "quantity"},
		{ID: "af3410ae-be24-41f8-b2ca-4cf7e7bbdf03", Type: "map", Binding: "none", Wide: true},
		{ID: "af3410ae-be24-41f8-b2ca-4cf7e7bbdf04", Type: "overview", Binding: "none"},
	}
	old = studioDecode[oldDocument](t, f.req(owner, "POST", "/api/workspaces", old, 200))
	var schema string
	if err := f.db.QueryRow(f.ctx, "SELECT current_schema()").Scan(&schema); err != nil {
		t.Fatal(err)
	}
	f.exec("DROP TRIGGER preserve_studio_v2_uuid ON pilot_workspace_ids")
	f.exec("ALTER TABLE pilot_workspace_preferences DROP COLUMN layout_v2")
	f.exec("DROP TABLE pilot_studio_commands")
	reopened, err := openDatabase(f.ctx, os.Getenv("TEST_DATABASE_URL"), schema)
	if err != nil {
		t.Fatal(err)
	}
	reopened.Close()

	next := studioDecode[studioDocument](t, f.req(owner, "GET", workspaceV2Path, nil, 200))
	mods := next.Layout.Workspaces[0].Modules
	if next.Revision != old.Revision || next.Layout.Version != 2 || next.Layout.Active != old.Layout.Active ||
		mods[0].ID != old.Layout.Workspaces[0].Modules[0].ID || mods[0].Type != "ship" || mods[0].Binding.Ship != active ||
		mods[0].Settings.LegacyView != "window" || mods[0].Settings.Geometry.X != 30 ||
		mods[1].Settings.Sort != "quantity" || mods[2].Type != "navigation" || mods[2].Purpose != "interstellar" || mods[2].Binding.Mode != "workspace" || mods[3].Binding.Mode != "workspace" {
		t.Fatalf("preservation: %+v", mods)
	}
	extra := workspaces.Instance{ID: "af3410ae-be24-41f8-b2ca-4cf7e7bbdf05", Type: "ship", Purpose: "owned",
		Binding: workspaces.Binding{Mode: "fixed", Ship: initialized.Layout.Workspaces[0].Ship}}
	next.Layout.Workspaces[0].Modules = append(mods, extra)
	f.req(owner, "PUT", workspaceV2Path, next, 404)
	next.Layout.Workspaces[0].Modules[len(mods)].Binding.Ship = active
	next.Layout.Workspaces[0].Modules[len(mods)].Settings.Section = "equipment"
	saved := studioDecode[studioDocument](t, f.req(owner, "PUT", workspaceV2Path, next, 200))
	replay := studioDecode[studioDocument](t, f.req(owner, "PUT", workspaceV2Path, next, 200))
	if saved.Revision != next.Revision+1 || replay.Revision != saved.Revision {
		t.Fatalf("revisions: %v %v", saved.Revision, replay.Revision)
	}

	rawLayout, err := json.Marshal(saved)
	if err != nil {
		t.Fatal(err)
	}
	for _, limit := range []struct{ padding, status int }{{8192, 200}, {65536, 400}} {
		req := httptest.NewRequest("PUT", workspaceV2Path, strings.NewReader(string(rawLayout)+strings.Repeat(" ", limit.padding)))
		req.AddCookie(owner)
		req.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()
		f.handler.ServeHTTP(w, req)
		if w.Code != limit.status {
			t.Fatalf("layout body %d: %d, want %d: %s", limit.padding, w.Code, limit.status, w.Body.String())
		}
	}

	otherV2 := studioDecode[studioDocument](t, f.req(other, "GET", workspaceV2Path, nil, 200))
	stolen := saved.Layout.Workspaces[0].Modules[len(mods)]
	stolen.Binding = workspaces.Binding{Mode: "workspace"}
	otherV2.Layout.Workspaces[0].Modules = append(otherV2.Layout.Workspaces[0].Modules, stolen)
	f.req(other, "PUT", workspaceV2Path, otherV2, 422)
	f.exec("DELETE FROM pilot_workspace_ids WHERE id=$1", extra.ID) // legacy binary cleanup
	if f.integer("SELECT count(*) FROM pilot_workspace_ids WHERE id=$1", extra.ID) != 1 {
		t.Fatal("legacy cleanup lost v2 UUID")
	}

	f.exec("INSERT INTO ships(user_id,active,name) VALUES($1,false,'Временный')", uid)
	retired := f.integer("SELECT id FROM ships WHERE user_id=$1 AND NOT active", uid)
	saved.Layout.Workspaces[0].Modules[len(mods)].Binding.Ship = retired
	saved = studioDecode[studioDocument](t, f.req(owner, "PUT", workspaceV2Path, saved, 200))
	f.exec("DELETE FROM ships WHERE id=$1", retired)
	saved.Layout.Workspaces[0].Modules[len(mods)].Settings.Section = "specs"
	saved = studioDecode[studioDocument](t, f.req(owner, "PUT", workspaceV2Path, saved, 200))
	f.req(owner, "GET", fmt.Sprintf("%s/ships/%d", studioPath, retired), nil, 404)

	// V1 remains independently writable. Concurrent edits share the revision.
	old = studioDecode[oldDocument](t, f.req(owner, "GET", "/api/workspaces", nil, 200))
	old.Layout.Workspaces[0].Name = "Старый клиент"
	saved.Layout.Workspaces[0].Name = "Новый клиент"
	var wg sync.WaitGroup
	results := make(chan int, 2)
	for _, entry := range []struct {
		method, path string
		body         any
	}{{"POST", "/api/workspaces", old}, {"PUT", workspaceV2Path, saved}} {
		wg.Go(func() {
			raw, _ := json.Marshal(entry.body)
			req := httptest.NewRequest(entry.method, entry.path, strings.NewReader(string(raw)))
			req.AddCookie(owner)
			req.Header.Set("Content-Type", "application/json")
			w := httptest.NewRecorder()
			f.handler.ServeHTTP(w, req)
			results <- w.Code
		})
	}
	wg.Wait()
	statuses := []int{<-results, <-results}
	slices.Sort(statuses)
	if !slices.Equal(statuses, []int{200, 409}) {
		t.Fatalf("statuses %v", statuses)
	}

	req := httptest.NewRequest("PUT", workspaceV2Path, strings.NewReader("{}"))
	req.AddCookie(owner)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Origin", "https://other.invalid")
	w := httptest.NewRecorder()
	f.handler.ServeHTTP(w, req)
	if w.Code != 403 {
		t.Fatalf("PUT origin: %d %s", w.Code, w.Body.String())
	}
}

func TestStudioV2AddressedCommands(t *testing.T) {
	f := newFixture033(t)
	owner, uid := f.signup("studio_command")
	foreignCookie, foreign := f.signup("studio_foreign")
	active := f.integer("SELECT id FROM ships WHERE user_id=$1 AND active", uid)
	f.exec("INSERT INTO ships(user_id,active,name,class,star,fuel,cargo,object_id,position_mode) VALUES($1,false,'Второй','pilgrim',1,30,'{\"agni\":3}','station','docked')", uid)
	second := f.integer("SELECT id FROM ships WHERE user_id=$1 AND NOT active", uid)
	base := fmt.Sprintf("%s/ships/%d", studioPath, second)
	cmd := base + "/commands"
	preview := base + "/preview"
	f.req(foreignCookie, "GET", base, nil, 404)
	f.req(owner, "GET", base+"/navigation", nil, 200)
	ship := studioDecode[struct {
		Ship    struct{ ID int64 }
		Fitting struct {
			Offers []struct {
				CatalogID string `json:"catalog_id"`
				Price     int64
			}
		}
	}](t, f.req(owner, "GET", base, nil, 200))
	if ship.Ship.ID != second || len(ship.Fitting.Offers) == 0 {
		t.Fatalf("ship view: %+v", ship)
	}
	if f.integer("SELECT count(*) FROM pilot_studio_commands WHERE user_id=$1", uid) != 0 {
		t.Fatal("read created receipt")
	}
	refuel := studioJSON{"request": "v2-refuel-test-0001", "action": "refuel", "quantity": 1}
	f.req(owner, "POST", preview, studioJSON{"action": "refuel", "quantity": 1}, 200)
	if f.integer("SELECT fuel FROM ships WHERE id=$1", second) != 30 {
		t.Fatal("preview changed fuel")
	}
	f.req(foreignCookie, "POST", cmd, refuel, 404)
	f.req(owner, "POST", cmd, refuel, 200)
	f.req(owner, "POST", cmd, refuel, 200)
	if f.integer("SELECT fuel FROM ships WHERE id=$1", second) != 35 ||
		f.integer("SELECT (cargo->>'agni')::int FROM ships WHERE id=$1", second) != 2 {
		t.Fatal("refuel did not apply exactly once")
	}
	f.req(owner, "POST", cmd, studioJSON{"request": refuel["request"], "action": "rename_ship", "name": "Третий"}, 409)
	f.req(owner, "POST", cmd, studioJSON{"request": "v2-rename-test-0001", "action": "rename_ship", "name": "Независимый"}, 200)
	offer := ship.Fitting.Offers[0]
	f.req(owner, "POST", cmd, studioJSON{"request": "v2-buy-module-00001", "action": "module_buy", "star": 1, "station": "station", "target": offer.CatalogID, "price": offer.Price + 1}, 409)
	f.exec("UPDATE users SET credits=100000000 WHERE id=$1", uid)
	f.req(owner, "POST", cmd, studioJSON{"request": "v2-buy-module-00001", "action": "module_buy", "star": 1, "station": "station", "target": offer.CatalogID, "price": offer.Price}, 200)
	module := f.integer("SELECT max(id) FROM ship_modules WHERE ship_id=$1", second)
	f.req(owner, "POST", cmd, studioJSON{"request": "v2-fit-module-00001", "action": "module_install", "id": module}, 200)
	f.req(owner, "POST", cmd, studioJSON{"request": "v2-remove-module-01", "action": "module_remove", "id": module}, 200)

	target := f.integer("SELECT CASE WHEN from_system=1 THEN to_system ELSE from_system END FROM universe_lanes WHERE from_system=1 OR to_system=1 ORDER BY ordinal LIMIT 1")
	points := make([]int64, 20)
	for i := range points {
		points[i] = target
	}
	plan := studioDecode[struct {
		Path      []int64
		Waypoints []int64
		Fuel      int
	}](t,
		f.req(owner, "POST", preview, studioJSON{"action": "route", "waypoints": points}, 200))
	if !slices.Equal(plan.Path, []int64{1, target}) || !slices.Equal(plan.Waypoints, []int64{target}) || plan.Fuel < 1 {
		t.Fatalf("route: %+v", plan)
	}
	f.req(owner, "POST", preview, studioJSON{"action": "route", "waypoints": append(points, target)}, 400)
	f.req(owner, "POST", cmd, studioJSON{"request": "v2-route-start-test", "action": "route", "waypoints": []int64{target}}, 200)
	if f.integer("SELECT reserved_fuel FROM ships WHERE id=$1", second) != int64(plan.Fuel) {
		t.Fatal("route and preview diverged")
	}
	f.exec("UPDATE ships SET destination=$2,departure=now(),arrival=now()+interval '30 seconds' WHERE id=$1", second, target)
	f.req(owner, "POST", cmd, studioJSON{"request": "v2-cancel-route-001", "action": "cancel_route"}, 200)
	if f.integer("SELECT COALESCE(destination,0) FROM ships WHERE id=$1", second) != target ||
		f.integer("SELECT reserved_fuel FROM ships WHERE id=$1", second) != 0 {
		t.Fatal("cancel changed the active transition")
	}
	f.exec("UPDATE ships SET destination=NULL,departure=NULL,arrival=NULL WHERE id=$1", second)
	f.req(owner, "POST", preview, studioJSON{"action": "move", "star": 1, "target": "station", "mode": "approach"}, 200)
	move := studioJSON{"request": "v2-move-test-000001", "action": "move", "star": 1, "target": "station", "mode": "approach"}
	f.req(owner, "POST", cmd, move, 200)
	f.req(owner, "POST", cmd, move, 200)
	f.req(owner, "POST", preview, studioJSON{"action": "rescue"}, 409)
	f.exec("UPDATE ships SET local_arrival=now()-interval '1 second' WHERE id=$1", second)
	f.exec("UPDATE ships SET local_target=NULL,local_mode=NULL,local_arrival=NULL,local_started=NULL WHERE id=$1", second)
	cost := studioDecode[struct{ Cost int64 }](t, f.req(owner, "POST", preview, studioJSON{"action": "rescue"}, 200)).Cost
	credits := f.integer("SELECT credits FROM users WHERE id=$1", uid)
	rescue := studioJSON{"request": "v2-rescue-test-0001", "action": "rescue", "confirm": true, "expected_cost": cost}
	f.req(owner, "POST", cmd, rescue, 200)
	f.req(owner, "POST", cmd, rescue, 200)
	if f.integer("SELECT credits FROM users WHERE id=$1", uid) != credits-cost {
		t.Fatal("rescue was charged incorrectly")
	}
	if f.integer("SELECT count(*) FROM ships WHERE id=$1 AND active AND destination IS NULL", active) != 1 ||
		f.integer("SELECT count(*) FROM ships WHERE user_id=$1", foreign) != 1 {
		t.Fatal("command affected active or foreign ship")
	}
}

func TestStudioV2InventoryAndConcurrentTransfer(t *testing.T) {
	f := newFixture033(t)
	owner, uid := f.signup("studio_storage")
	other, otherUID := f.signup("studio_visitor")
	ship := f.integer("SELECT id FROM ships WHERE user_id=$1 AND active", uid)
	f.exec("UPDATE ships SET cargo='{\"agni\":3}',object_id='station',position_mode='docked' WHERE id=$1", ship)
	path := studioPath + "/inventory"
	f.req(other, "GET", fmt.Sprintf("%s?kind=ship&ship=%d", path, ship), nil, 404)
	cargo := studioDecode[struct {
		Items    []struct{ Quantity int }
		Capacity int
	}](t, f.req(owner, "GET", fmt.Sprintf("%s?kind=ship&ship=%d", path, ship), nil, 200))
	if len(cargo.Items) != 1 || cargo.Items[0].Quantity != 3 || cargo.Capacity <= 0 {
		t.Fatalf("cargo: %+v", cargo)
	}
	f.req(owner, "GET", fmt.Sprintf("%s?kind=personal&ship=%d", path, ship), nil, 200)
	if f.integer("SELECT count(*) FROM warehouses WHERE user_id=$1", uid) != 0 {
		t.Fatal("read created a warehouse")
	}
	f.req(owner, "GET", path+"?kind=personal&star=1&station=station", nil, 200)
	tbody := studioJSON{"mode": "direct", "from": studioJSON{"kind": "ship", "ship": ship},
		"to": studioJSON{"kind": "personal", "star": 1, "station": "station"}, "resource": "agni", "quantity": 2}
	transfer := path + "/transfer"
	var wg sync.WaitGroup
	statuses := make(chan int, 2)
	for _, id := range []string{"v2-transfer-test-001", "v2-transfer-test-002"} {
		wg.Go(func() {
			body := studioJSON{}
			for k, v := range tbody {
				body[k] = v
			}
			body["request"] = id
			raw, _ := json.Marshal(body)
			req := httptest.NewRequest(http.MethodPost, transfer, strings.NewReader(string(raw)))
			req.Header.Set("Content-Type", "application/json")
			req.AddCookie(owner)
			w := httptest.NewRecorder()
			f.handler.ServeHTTP(w, req)
			statuses <- w.Code
		})
	}
	wg.Wait()
	codes := []int{<-statuses, <-statuses}
	slices.Sort(codes)
	if !slices.Equal(codes, []int{200, 409}) {
		t.Fatalf("transfers %v", codes)
	}
	var request string
	if err := f.db.QueryRow(f.ctx, "SELECT request_id FROM logistics_operations WHERE user_id=$1", uid).Scan(&request); err != nil {
		t.Fatal(err)
	}
	tbody["request"] = request
	f.req(owner, "POST", transfer, tbody, 200)
	f.req(other, "POST", transfer, tbody, 404)
	if f.integer("SELECT (cargo->>'agni')::int FROM warehouses WHERE user_id=$1 AND star=1 AND station='station'", uid) != 2 ||
		f.integer("SELECT count(*) FROM warehouses WHERE user_id=$1", otherUID) != 0 {
		t.Fatal("transfer or retry changed incorrect balances")
	}
	f.exec("UPDATE ships SET position_mode='approach' WHERE id=$1", ship)
	f.req(owner, "GET", fmt.Sprintf("%s?kind=personal&ship=%d", path, ship), nil, 409)
	f.req(owner, "GET", path+"?kind=personal&star=1&station=station", nil, 200)

	// Corporate membership allows reading and depositing. Withdrawal requires
	// the warehouse role and rechecks it even on a receipt retry.
	f.req(owner, "POST", "/api/companies", studioJSON{"action": "leave"}, 200)
	f.exec("UPDATE users SET credits=1000000 WHERE id=$1", uid)
	f.req(owner, "POST", "/api/companies", studioJSON{"action": "create", "name": "Studio Storage", "tag": "STO", "flag": "◆", "color": "#80bfb1"}, 200)
	company := f.integer("SELECT company_id FROM users WHERE id=$1", uid)
	corp := fmt.Sprintf("%s?kind=company&star=1&station=station&company=%d", path, company)
	f.exec(`INSERT INTO company_projects(company_id,name,kind,star,station,status,required)
		VALUES($1,'Depot','warehouse',1,'station','complete','{}')`, company)
	f.exec(`INSERT INTO company_storage(company_id,star,station,cargo)
		VALUES($1,1,'station','{"agni":4}')`, company)
	f.req(other, "GET", corp, nil, 404)
	f.exec("UPDATE users SET company_id=$2 WHERE id=$1", otherUID, company)
	otherShip := f.integer("SELECT id FROM ships WHERE user_id=$1 AND active", otherUID)
	f.exec("UPDATE ships SET object_id='station',position_mode='docked' WHERE id=$1", otherShip)
	stock := studioDecode[struct {
		CanWithdraw bool `json:"can_withdraw"`
		CanDeposit  bool `json:"can_deposit"`
	}](t, f.req(other, "GET", corp, nil, 200))
	if stock.CanWithdraw || !stock.CanDeposit {
		t.Fatalf("role capabilities: %+v", stock)
	}
	f.req(other, "GET", fmt.Sprintf("%s?kind=company&ship=%d&company=%d", path, otherShip, company), nil, 200)
	move := studioJSON{"request": "v2-company-withdraw-01", "mode": "direct", "from": studioJSON{"kind": "company", "star": 1, "station": "station", "company": company},
		"to": studioJSON{"kind": "ship", "ship": otherShip}, "resource": "agni", "quantity": 1}
	f.req(other, "POST", transfer, move, 404)
	f.exec(`INSERT INTO company_roles(company_id,name,permissions) VALUES($1,'Keeper','["warehouse"]')`, company)
	f.exec("UPDATE users SET company_role=(SELECT id FROM company_roles WHERE company_id=$2 AND name='Keeper') WHERE id=$1", otherUID, company)
	f.req(other, "POST", transfer, move, 200)
	f.exec("UPDATE users SET company_role=NULL WHERE id=$1", otherUID)
	f.req(other, "POST", transfer, move, 404)
	if f.integer("SELECT (cargo->>'agni')::int FROM company_storage WHERE company_id=$1", company) != 3 {
		t.Fatal("corporate transfer was applied twice")
	}

}
