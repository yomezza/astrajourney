package integration

import (
	"encoding/json"
	"strings"
	"sync"
	"testing"

	"github.com/yomezza/cosmosimulator/internal/logistics"
	"github.com/yomezza/cosmosimulator/internal/universe"
	"github.com/yomezza/cosmosimulator/internal/worldstore"
)

func TestLogisticsTransfers080(t *testing.T) {
	f := newFixture033(t)
	one, uid := f.signup("logistics080")
	two, other := f.signup("logother080")
	sid := f.integer(`SELECT id FROM ships WHERE user_id=$1`, uid)
	otherShip := f.integer(`SELECT id FROM ships WHERE user_id=$1`, other)
	f.exec(`UPDATE ships SET cargo='{"iron":40,"agni":10}',fuel=40 WHERE id=$1`, sid)
	from := logistics.CargoLocation{Kind: "ship", Ship: sid}
	to := logistics.CargoLocation{Kind: "personal", Star: 1, Station: "station"}
	body := map[string]any{"request": "transfer-080-00000001", "mode": "direct", "from": from, "to": to, "ship": sid, "resource": "iron", "quantity": 7}
	f.req(one, "POST", "/api/logistics", body, 200)
	f.req(one, "POST", "/api/logistics", body, 200)
	if f.integer(`SELECT (cargo->>'iron')::int FROM ships WHERE id=$1`, sid) != 33 || f.integer(`SELECT (cargo->>'iron')::int FROM warehouses WHERE user_id=$1 AND star=1 AND station='station'`, uid) != 7 {
		t.Fatal("retry duplicated inventory transfer")
	}
	body["quantity"] = 8
	f.req(one, "POST", "/api/logistics", body, 409)
	body["request"] = "transfer-080-00000002"
	body["quantity"] = 1
	f.req(two, "POST", "/api/logistics", body, 409)
	alias := to
	alias.Company = 999
	alias.Ship = sid
	body["from"] = alias
	body["to"] = to
	f.req(one, "POST", "/api/logistics", body, 400)
	if f.integer(`SELECT (cargo->>'iron')::int FROM warehouses WHERE user_id=$1 AND star=1 AND station='station'`, uid) != 7 {
		t.Fatal("alias made resources")
	}
	body["from"] = from
	body["to"] = logistics.CargoLocation{Kind: "personal", Star: 5, Station: "station"}
	f.req(one, "POST", "/api/logistics", body, 409)
	body["mode"] = "queue"
	f.req(one, "POST", "/api/logistics", body, 200)
	f.req(one, "POST", "/api/logistics", body, 200)
	if f.integer(`SELECT count(*) FROM automation_tasks WHERE user_id=$1`, uid) != 1 {
		t.Fatal("delivery was queued twice")
	}
	for range 12 {
		advanceTasks070(f, uid)
	}
	if f.integer(`SELECT (cargo->>'iron')::int FROM warehouses WHERE user_id=$1 AND star=5 AND station='station'`, uid) != 1 {
		t.Fatal("unload task did not deliver")
	}
	body = map[string]any{"request": "transfer-080-00000003", "mode": "queue", "from": logistics.CargoLocation{Kind: "personal", Star: 1, Station: "station"}, "to": from, "ship": sid, "resource": "iron", "quantity": 4}
	f.req(one, "POST", "/api/logistics", body, 200)
	for range 12 {
		advanceTasks070(f, uid)
	}
	if f.integer(`SELECT (cargo->>'iron')::int FROM ships WHERE id=$1`, sid) != 36 {
		t.Fatal("load task did not retrieve real cargo")
	}
	// A corporate store requires both a completed facility and current permissions.
	f.exec(`UPDATE users SET credits=2000000 WHERE id=$1`, uid)
	f.req(one, "POST", "/api/companies", map[string]any{"action": "leave"}, 200)
	f.req(one, "POST", "/api/companies", map[string]any{"action": "create", "name": "Logistics Lab", "tag": "LAB", "flag": "◆", "color": "#80bfb1"}, 200)
	cid := f.integer(`SELECT company_id FROM users WHERE id=$1`, uid)
	corp := logistics.CargoLocation{Kind: "company", Company: cid, Star: 1, Station: "station"}
	body = map[string]any{"request": "transfer-080-00000004", "mode": "direct", "from": from, "to": corp, "ship": sid, "resource": "iron", "quantity": 5}
	f.req(one, "POST", "/api/logistics", body, 409)
	f.exec(`INSERT INTO company_projects(company_id,name,kind,star,station,status,required) VALUES($1,'Depot','warehouse',1,'station','complete','{}')`, cid)
	f.req(one, "POST", "/api/logistics", body, 200)
	f.exec(`UPDATE users SET company_id=$2 WHERE id=$1`, other, cid)
	body = map[string]any{"request": "transfer-080-00000005", "mode": "direct", "from": corp, "to": logistics.CargoLocation{Kind: "ship", Ship: otherShip}, "ship": otherShip, "resource": "iron", "quantity": 2}
	f.req(two, "POST", "/api/logistics", body, 409)
	f.exec(`INSERT INTO company_roles(company_id,name,permissions) VALUES($1,'Keeper','["warehouse"]')`, cid)
	f.exec(`UPDATE users SET company_role=(SELECT id FROM company_roles WHERE company_id=$2 AND name='Keeper') WHERE id=$1`, other, cid)
	f.req(two, "POST", "/api/logistics", body, 200)
	if f.integer(`SELECT (cargo->>'iron')::int FROM company_storage WHERE company_id=$1`, cid) != 3 {
		t.Fatal("corporate stock mismatch")
	}
	f.req(one, "GET", "/api/logistics", nil, 200)
	// The actual reserve under an active mining cycle is included in capacity.
	f.exec(`UPDATE ships SET cargo='{"iron":55}' WHERE id=$1`, otherShip)
	f.exec(`INSERT INTO mining_jobs(user_id,ship_id,star,planet,resource,quantity,finish_tick) VALUES($1,$2,1,101,'iron',4,99999)`, other, otherShip)
	body["request"] = "transfer-080-00000006"
	f.req(two, "POST", "/api/logistics", body, 409)
}

func TestConcurrentLogistics080(t *testing.T) {
	f := newFixture033(t)
	c, uid := f.signup("lastcargo080")
	sid := f.integer(`SELECT id FROM ships WHERE user_id=$1`, uid)
	f.exec(`UPDATE ships SET cargo='{"iron":5}' WHERE id=$1`, sid)
	body := map[string]any{"request": "concurrent-080-000001", "mode": "direct", "from": logistics.CargoLocation{Kind: "ship", Ship: sid}, "to": logistics.CargoLocation{Kind: "personal", Star: 1, Station: "station"}, "ship": sid, "resource": "iron", "quantity": 5}
	var wg sync.WaitGroup
	for range 2 {
		wg.Add(1)
		go func() { defer wg.Done(); f.req(c, "POST", "/api/logistics", body, 200) }()
	}
	wg.Wait()
	if f.integer(`SELECT (cargo->>'iron')::int FROM warehouses WHERE user_id=$1`, uid) != 5 || f.integer(`SELECT count(*) FROM logistics_operations WHERE user_id=$1`, uid) != 1 {
		t.Fatal("concurrent retry applied twice")
	}
}

func TestAgniShipment080(t *testing.T) {
	f := newFixture033(t)
	c, uid := f.signup("fuelcargo080")
	sid := f.integer(`SELECT id FROM ships WHERE user_id=$1`, uid)
	f.exec(`UPDATE ships SET fuel=0,cargo='{"agni":10}' WHERE id=$1`, sid)
	f.req(c, "POST", "/api/logistics", map[string]any{"request": "fuel-shipment-0800001", "mode": "queue", "from": logistics.CargoLocation{Kind: "ship", Ship: sid}, "to": logistics.CargoLocation{Kind: "personal", Star: 5, Station: "station"}, "ship": sid, "resource": "agni", "quantity": 10}, 200)
	if err := f.app.Planner.AdvanceAutomation(f.ctx); err != nil {
		t.Fatal(err)
	}
	if f.integer(`SELECT (cargo->>'agni')::int FROM ships WHERE id=$1`, sid) != 10 || f.integer(`SELECT count(*) FROM automation_tasks WHERE user_id=$1 AND status='paused'`, uid) != 1 {
		t.Fatal("shipment was burned as ship fuel")
	}
}

func TestGalaxyDeletion080(t *testing.T) {
	f := newFixture033(t)
	admin, uid := f.signup("delete080")
	player, pid := f.signup("keep080")
	f.exec(`UPDATE users SET role='admin' WHERE id=$1`, uid)
	in := universe.GalaxyEdit{Create: true, ID: "delete-test", Name: "Delete Test", Shape: "rings", Seed: 80, Systems: 50, Settings: universe.DefaultGalaxySettings()}
	preview := f.req(admin, "POST", "/api/admin/galaxies/preview", in, 200)
	var p struct {
		Preview string `json:"preview"`
	}
	json.Unmarshal(preview.Body.Bytes(), &p)
	in.Preview = p.Preview
	f.req(admin, "POST", "/api/admin/galaxies/apply", in, 200)
	g, _ := f.app.World.Load(f.ctx)
	star := universe.CoreSystems(g, in.ID)[0]
	f.req(player, "POST", "/api/admin/galaxies/delete-preview", map[string]any{"id": in.ID}, 403)
	f.req(admin, "POST", "/api/admin/galaxies/delete-preview", map[string]any{"id": "edem"}, 400)
	f.exec(`UPDATE ships SET star=$2 WHERE user_id=$1`, pid, star)
	response := f.req(admin, "POST", "/api/admin/galaxies/delete-preview", map[string]any{"id": in.ID}, 200)
	var review struct {
		Preview string `json:"preview"`
		Can     bool   `json:"can_delete"`
	}
	json.Unmarshal(response.Body.Bytes(), &review)
	if review.Can {
		t.Fatal("galaxy with another player's ship was deletable")
	}
	f.req(admin, "POST", "/api/admin/galaxies/delete", map[string]any{"id": in.ID, "preview": review.Preview}, 409)
	f.exec(`UPDATE ships SET star=1 WHERE user_id=$1`, pid)
	f.exec(`INSERT INTO warehouses(user_id,star,station,cargo) VALUES($1,$2,'station','{"iron":0}')`, uid, star)
	response = f.req(admin, "POST", "/api/admin/galaxies/delete-preview", map[string]any{"id": in.ID}, 200)
	json.Unmarshal(response.Body.Bytes(), &review)
	if !review.Can {
		t.Fatal("empty admin galaxy cannot be deleted")
	}
	f.req(admin, "POST", "/api/admin/galaxies/delete", map[string]any{"id": in.ID, "preview": review.Preview}, 200)
	g, _ = f.app.World.Load(f.ctx)
	if !g.Definition(in.ID).Deleted || len(universe.GalaxyStars(g, "edem")) != 3000 || len(g.Stars) != 3050 || g.Stars[star-1].ID != star || !g.Stars[star-1].Deleted || len(g.Path(star, star)) != 0 {
		t.Fatal("deletion damaged stable IDs or other galaxies")
	}
	f.req(admin, "GET", "/api/system?id="+fmtInt080(star), nil, 404)
	f.req(admin, "POST", "/api/admin/teleport", map[string]any{"to": star}, 400)
	var schema string
	f.db.QueryRow(f.ctx, `SELECT current_schema()`).Scan(&schema)
	db, err := openDatabase(f.ctx, f.db.Config().ConnString(), schema)
	if err != nil {
		t.Fatal(err)
	}
	db.Close()
	tx, err := f.db.Begin(f.ctx)
	if err != nil {
		t.Fatal(err)
	}
	if err = worldstore.ReconcileStations(f.ctx, tx, g); err != nil {
		t.Fatal(err)
	}
	if err = tx.Commit(f.ctx); err != nil {
		t.Fatal(err)
	}
	if f.integer(`SELECT count(*) FROM stations WHERE star=$1 AND active`, star) != 0 {
		t.Fatal("restart restored archived ports")
	}
}

func fmtInt080(n int) string { b, _ := json.Marshal(n); return string(b) }

func TestWorkspaceBatchBetweenShips(t *testing.T) {
	f := newFixture033(t)
	owner, uid := f.signup("batch_owner")
	source := f.integer("SELECT id FROM ships WHERE user_id=$1 AND active", uid)
	target := f.integer(`INSERT INTO ships(user_id,name,class,star,active,fuel,cargo,object_id,position_mode)
		VALUES($1,'Cargo receiver','pilgrim',1,false,20,'{}','station','docked') RETURNING id`, uid)
	f.exec(`UPDATE ships SET cargo='{"iron":9,"agni":4}' WHERE id=$1`, source)
	body := map[string]any{
		"request": "workspace-batch-00001", "mode": "direct", "ship": source,
		"from": logistics.CargoLocation{Kind: "ship", Ship: source},
		"to":   logistics.CargoLocation{Kind: "ship", Ship: target},
		"items": []map[string]any{
			{"resource": "iron", "quantity": 3},
			{"resource": "agni", "quantity": 2},
		},
	}
	f.req(owner, "POST", "/api/logistics", body, 200)
	f.exec(`UPDATE ships SET object_id='entry',position_mode='approach' WHERE id=$1`, target)
	f.req(owner, "POST", "/api/logistics", body, 200)
	if iron, gas := f.integer(`SELECT (cargo->>'iron')::int FROM ships WHERE id=$1`, target), f.integer(`SELECT (cargo->>'agni')::int FROM ships WHERE id=$1`, target); iron != 3 || gas != 2 {
		t.Fatalf("destination contains %d iron and %d hydrogen", iron, gas)
	}
	body["request"] = "workspace-batch-00002"
	result := f.req(owner, "POST", "/api/logistics", body, 409)
	if value := result.Body.String(); !strings.Contains(value, `"code":"TRANSFER_REJECTED"`) {
		t.Fatalf("missing machine-readable rejection: %s", value)
	}
	if iron, gas := f.integer(`SELECT (cargo->>'iron')::int FROM ships WHERE id=$1`, source), f.integer(`SELECT (cargo->>'agni')::int FROM ships WHERE id=$1`, source); iron != 6 || gas != 2 {
		t.Fatalf("source contains %d iron and %d hydrogen", iron, gas)
	}
}
