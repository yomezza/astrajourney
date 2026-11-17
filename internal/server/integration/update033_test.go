package integration

import (
	"compress/gzip"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/yomezza/cosmosimulator/internal/platform/dblayout"

	"github.com/yomezza/cosmosimulator/internal/platform/postgres"
	"github.com/yomezza/cosmosimulator/internal/server"
	"github.com/yomezza/cosmosimulator/internal/universe"
)

type fixture033 struct {
	t       *testing.T
	ctx     context.Context
	app     *server.App
	db      *pgxpool.Pool
	handler http.Handler
}

func newFixture033(t *testing.T) *fixture033 {
	t.Helper()
	dsn := os.Getenv("TEST_DATABASE_URL")
	if dsn == "" {
		t.Skip("TEST_DATABASE_URL required")
	}
	ctx := context.Background()
	base, err := pgxpool.New(ctx, dsn)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasSuffix(base.Config().ConnConfig.Database, "_test") {
		t.Fatal("disposable database required")
	}
	schema := fmt.Sprintf("update033_%d", time.Now().UnixNano())
	db, err := openDatabase(ctx, dsn, schema)
	if err != nil {
		base.Close()
		t.Fatal(err)
	}
	t.Cleanup(func() {
		db.Close()
		base.Exec(ctx, "DROP SCHEMA "+dblayout.DropList(schema)+" CASCADE")
		base.Close()
	})
	a := server.New(db, server.Config{})
	return &fixture033{t, ctx, a, db, a.Handler()}
}

func (f *fixture033) req(c *http.Cookie, method, path string, body any, status int) *httptest.ResponseRecorder {
	f.t.Helper()
	raw, _ := json.Marshal(body)
	r := httptest.NewRequest(method, path, strings.NewReader(string(raw)))
	r.RemoteAddr = "127.0.0.1:1234"
	if c != nil {
		r.AddCookie(c)
	}
	if method == "POST" {
		r.Header.Set("Content-Type", "application/json")
	}
	w := httptest.NewRecorder()
	f.handler.ServeHTTP(w, r)
	if w.Code != status {
		f.t.Fatalf("%s %s: %d != %d %s", method, path, w.Code, status, w.Body.String())
	}
	return w
}

func (f *fixture033) signup(name string) (*http.Cookie, int64) {
	w := f.req(nil, "POST", "/api/register", map[string]any{"username": name, "password": "1"}, 200)
	var id int64
	if err := f.db.QueryRow(f.ctx, `SELECT id FROM users WHERE username=$1`, name).Scan(&id); err != nil {
		f.t.Fatal(err)
	}
	c := w.Result().Cookies()[0]
	f.req(c, "GET", "/api/expedition", nil, 200)
	return c, id
}

func (f *fixture033) cmd(c *http.Cookie, body map[string]any, status int) {
	f.req(c, "POST", "/api/workbench", body, status)
}

func (f *fixture033) exec(sql string, args ...any) {
	f.t.Helper()
	if _, err := f.db.Exec(f.ctx, sql, args...); err != nil {
		f.t.Fatal(err)
	}
}

func (f *fixture033) integer(sql string, args ...any) int64 {
	f.t.Helper()
	var n int64
	if err := f.db.QueryRow(f.ctx, sql, args...).Scan(&n); err != nil {
		f.t.Fatal(err)
	}
	return n
}

func (f *fixture033) advance(uid int64) {
	f.t.Helper()
	g, err := f.app.World.Load(f.ctx)
	if err != nil {
		f.t.Fatal(err)
	}
	if err = f.app.Navigation.AdvancePlayer(f.ctx, uid, g); err != nil {
		f.t.Fatal(err)
	}
}

func finishResearchForTest(t *testing.T, a *server.App) {
	t.Helper()
	if _, err := a.DB.Exec(context.Background(), `UPDATE research_jobs SET next_at=now()-interval '1 day'`); err != nil {
		t.Fatal(err)
	}
	if err := a.Simulation.Activities(context.Background()); err != nil {
		t.Fatal(err)
	}
}

func Test033ResearchDataAndNotifications(t *testing.T) {
	f := newFixture033(t)
	one, id1 := f.signup("scout033")
	two, id2 := f.signup("buyer033")
	if f.integer(`SELECT count(*) FROM companies WHERE founder`) != 4 {
		t.Fatal("four founder placeholders required")
	}
	f.req(one, "GET", "/api/workbench", nil, 200)
	request := httptest.NewRequest("GET", "/api/workbench", nil)
	request.AddCookie(one)
	request.Header.Set("Accept-Encoding", "gzip")
	compressed := httptest.NewRecorder()
	f.handler.ServeHTTP(compressed, request)
	if compressed.Code != 200 || compressed.Header().Get("Content-Encoding") != "gzip" {
		t.Fatal("missing compression")
	}
	reader, err := gzip.NewReader(compressed.Body)
	if err != nil {
		t.Fatal(err)
	}
	payload, err := io.ReadAll(reader)
	reader.Close()
	if err != nil || !json.Valid(payload) {
		t.Fatal("invalid compressed response", err)
	}

	f.req(one, "GET", "/api/system?id=1", nil, 200)
	estimate := f.req(one, "GET", "/api/estimate?to=5", nil, 200)
	var plan struct {
		Fuel    int `json:"fuel"`
		Cost    int `json:"cost"`
		Objects int `json:"objects"`
	}
	if err := json.Unmarshal(estimate.Body.Bytes(), &plan); err != nil || plan.Fuel != 2 || plan.Cost != 10 || plan.Objects < 5 {
		t.Fatal("expedition estimate", plan, err)
	}
	f.cmd(one, map[string]any{"action": "explore", "to": 1}, 200)
	if f.integer(`SELECT credits FROM users WHERE id=$1`, id1) != 990 || f.integer(`SELECT count(*) FROM surveys WHERE user_id=$1`, id1) != 0 {
		t.Fatal("research must charge once and remain asynchronous")
	}
	f.cmd(one, map[string]any{"action": "pause_research"}, 200)
	f.exec(`UPDATE research_jobs SET next_at=now()-interval '1 day'`)
	f.advance(id1)
	if f.integer(`SELECT count(*) FROM surveys WHERE user_id=$1`, id1) != 0 {
		t.Fatal("paused research advanced")
	}
	f.cmd(one, map[string]any{"action": "resume_research"}, 200)
	finishResearchForTest(t, f.app)
	f.req(one, "GET", "/api/system?id=1", nil, 200)
	if f.integer(`SELECT xp FROM users WHERE id=$1`, id1) != 10 {
		t.Fatal("research reward")
	}
	f.advance(id1)
	if f.integer(`SELECT xp FROM users WHERE id=$1`, id1) != 10 {
		t.Fatal("duplicate reward")
	}
	f.cmd(one, map[string]any{"action": "data_sell", "region": 0, "price": 100}, 200)
	offer := f.integer(`SELECT id FROM survey_offers WHERE seller=$1`, id1)
	f.cmd(two, map[string]any{"action": "data_buy", "id": offer}, 200)
	if f.integer(`SELECT credits FROM users WHERE id=ANY($1::bigint[])`, []int64{id1}) != 1090 || f.integer(`SELECT credits FROM users WHERE id=$1`, id2) != 900 {
		t.Fatal("data payment not conserved")
	}
	f.cmd(two, map[string]any{"action": "data_buy", "id": offer}, 409)
	f.req(two, "GET", "/api/system?id=1", nil, 200)
	f.cmd(two, map[string]any{"action": "data_sell", "region": 0, "price": 100}, 409)
	nid := f.integer(`SELECT id FROM notifications WHERE user_id=$1 ORDER BY id LIMIT 1`, id1)
	f.req(two, "POST", "/api/notifications", map[string]any{"action": "delete", "ids": []int64{nid}}, 200)
	if f.integer(`SELECT count(*) FROM notifications WHERE id=$1`, nid) != 1 {
		t.Fatal("notification ownership violated")
	}
	f.req(one, "POST", "/api/notifications", map[string]any{"action": "delete", "ids": []int64{nid}}, 200)
	if f.integer(`SELECT count(*) FROM notifications WHERE id=$1`, nid) != 0 {
		t.Fatal("notification delete")
	}
}

func Test033FleetStorageOrdersAndDelivery(t *testing.T) {
	f := newFixture033(t)
	one, id1 := f.signup("fleet033")
	two, id2 := f.signup("trade033")
	f.req(one, "GET", "/api/workbench", nil, 200)
	f.exec(`UPDATE users SET credits=100000,level=20 WHERE id=$1`, id1)
	f.req(one, "POST", "/api/expedition", map[string]any{"action": "buy", "resource": "agni", "quantity": 1, "expected_price": 0}, 409)
	if f.integer(`SELECT credits FROM users WHERE id=$1`, id1) != 100000 {
		t.Fatal("stale quote debited balance")
	}
	f.cmd(one, map[string]any{"action": "buy_ship"}, 200)
	if f.integer(`SELECT count(*) FROM ships WHERE user_id=$1`, id1) != 2 {
		t.Fatal("fleet purchase")
	}
	s1 := f.integer(`SELECT id FROM ships WHERE user_id=$1 AND active`, id1)
	s2 := f.integer(`SELECT id FROM ships WHERE user_id=$1 AND NOT active`, id1)
	f.cmd(two, map[string]any{"action": "select_ship", "ship": s2}, 404)
	f.cmd(one, map[string]any{"action": "select_ship", "ship": s2}, 200)
	if f.integer(`SELECT id FROM ships WHERE user_id=$1 AND active`, id1) != s2 {
		t.Fatal("ship selection")
	}
	f.exec(`UPDATE ships SET cargo='{"iron":20}' WHERE id=$1`, s2)
	f.cmd(one, map[string]any{"action": "store", "resource": "iron", "quantity": 20}, 200)
	f.cmd(one, map[string]any{"action": "order_sell", "resource": "iron", "quantity": 10, "price": 5}, 200)
	order := f.integer(`SELECT id FROM sell_orders WHERE seller=$1`, id1)
	var wg sync.WaitGroup
	codes := make(chan int, 2)
	for i := 0; i < 2; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			r := httptest.NewRequest("POST", "/api/workbench", strings.NewReader(fmt.Sprintf(`{"action":"order_buy","id":%d,"quantity":7}`, order)))
			r.Header.Set("Content-Type", "application/json")
			r.AddCookie(two)
			w := httptest.NewRecorder()
			f.handler.ServeHTTP(w, r)
			codes <- w.Code
		}()
	}
	wg.Wait()
	close(codes)
	success := 0
	for code := range codes {
		if code == 200 {
			success++
		} else if code != 409 {
			t.Fatal(code)
		}
	}
	if success != 1 || f.integer(`SELECT quantity FROM sell_orders WHERE id=$1`, order) != 3 {
		t.Fatal("last-stock race")
	}
	if f.integer(`SELECT (cargo->>'iron')::int FROM warehouses WHERE user_id=$1 AND star=1`, id2) != 7 {
		t.Fatal("remote purchase cargo location")
	}
	f.cmd(one, map[string]any{"action": "delivery_create", "resource": "iron", "quantity": 5, "to": 5, "station": "station", "amount": 10, "collateral": 30, "hours": 1}, 200)
	delivery := f.integer(`SELECT id FROM deliveries WHERE owner=$1`, id1)
	f.cmd(one, map[string]any{"action": "delivery_bot", "id": delivery}, 200)
	f.exec(`UPDATE deliveries SET arrival=now()-interval '1 second' WHERE id=$1`, delivery)
	if err := f.app.Commerce.AdvanceLogistics(f.ctx); err != nil {
		t.Fatal(err)
	}
	if f.integer(`SELECT (cargo->>'iron')::int FROM warehouses WHERE user_id=$1 AND star=5`, id1) != 5 {
		t.Fatal("delivery didn't move goods")
	}
	if err := f.app.Commerce.AdvanceLogistics(f.ctx); err != nil {
		t.Fatal(err)
	}
	if f.integer(`SELECT (cargo->>'iron')::int FROM warehouses WHERE user_id=$1 AND star=5`, id1) != 5 {
		t.Fatal("duplicate delivery")
	}
	f.cmd(one, map[string]any{"action": "select_ship", "ship": s1}, 200)
	f.cmd(one, map[string]any{"action": "route", "waypoints": []int{5, 9}}, 200)
	f.advance(id1)
	f.exec(`UPDATE ships SET arrival=now()-interval '1 second' WHERE id=$1`, s1)
	f.advance(id1)
	if f.integer(`SELECT destination FROM ships WHERE id=$1`, s1) != 9 {
		t.Fatal("route continuation")
	}
}

func Test033CorporateRolesAndPortal(t *testing.T) {
	f := newFixture033(t)
	one, id1 := f.signup("owner033")
	two, id2 := f.signup("member033")
	f.exec(`UPDATE users SET credits=2000000 WHERE id=$1`, id1)
	f.req(one, "POST", "/api/companies", map[string]any{"action": "leave"}, 200)
	f.req(one, "POST", "/api/companies", map[string]any{"action": "create", "name": "Test Corporation", "tag": "TST", "color": "#112233", "flag": "◆"}, 200)
	cid := f.integer(`SELECT company_id FROM users WHERE id=$1`, id1)
	if f.integer(`SELECT credits FROM users WHERE id=$1`, id1) != 1000000 {
		t.Fatal("creation fee")
	}
	f.req(two, "POST", "/api/companies", map[string]any{"action": "leave"}, 200)
	f.cmd(two, map[string]any{"action": "apply", "id": cid}, 200)
	rid := f.integer(`SELECT id FROM company_requests WHERE user_id=$1`, id2)
	f.cmd(one, map[string]any{"action": "request_accept", "id": rid}, 200)
	f.cmd(two, map[string]any{"action": "company_withdraw", "amount": 1}, 409)
	for i := 0; i < 7; i++ {
		f.cmd(one, map[string]any{"action": "role_save", "name": fmt.Sprintf("role-%d", i), "permissions": []string{"finance"}}, 200)
	}
	f.cmd(one, map[string]any{"action": "role_save", "name": "role-8", "permissions": []string{}}, 409)
	role := f.integer(`SELECT min(id) FROM company_roles WHERE company_id=$1`, cid)
	f.cmd(one, map[string]any{"action": "role_save", "id": role, "name": "role-1", "permissions": []string{"finance"}}, 409)
	f.cmd(one, map[string]any{"action": "order_buy", "id": 99999999, "quantity": 1}, 404)
	f.cmd(one, map[string]any{"action": "role_assign", "id": id2, "amount": role}, 200)
	f.cmd(one, map[string]any{"action": "company_deposit", "amount": 100}, 200)
	f.cmd(two, map[string]any{"action": "company_withdraw", "amount": 50}, 200)
	if f.integer(`SELECT credits FROM companies WHERE id=$1`, cid) != 50 {
		t.Fatal("corporate wallet")
	}
	f.cmd(one, map[string]any{"action": "project_create", "name": "Shared Store", "target": "warehouse"}, 200)
	project := f.integer(`SELECT id FROM company_projects WHERE company_id=$1`, cid)
	f.exec(`INSERT INTO warehouses(user_id,star,station,cargo) VALUES($1,1,'station','{"iron":100,"silicon":50}') ON CONFLICT(user_id,star,station) DO UPDATE SET cargo=EXCLUDED.cargo`, id1)
	for resource, q := range map[string]int{"iron": 100, "silicon": 50} {
		f.cmd(one, map[string]any{"action": "project_contribute", "id": project, "resource": resource, "quantity": q}, 200)
	}
	if f.integer(`SELECT count(*) FROM company_projects WHERE id=$1 AND status='complete'`, project) != 1 {
		t.Fatal("project progress")
	}
	f.exec(`UPDATE portal SET required=100`)
	f.cmd(one, map[string]any{"action": "portal_contribute", "amount": 100}, 200)
	g, err := f.app.World.Load(f.ctx)
	if err != nil || len(g.Stars) != 4000 {
		t.Fatal("portal galaxy", err)
	}
	core := universe.CoreSystems(g)[0]
	f.exec(`UPDATE ships SET star=$2 WHERE user_id=$1`, id1, core)
	f.cmd(one, map[string]any{"action": "portal_travel"}, 200)
	if f.integer(`SELECT star FROM ships WHERE user_id=$1`, id1) != int64(universe.GalaxyStars(g, "asterion")[0].ID) {
		t.Fatal("portal transition")
	}
}

func Test033UpgradePreservesLegacyAssets(t *testing.T) {
	dsn := os.Getenv("TEST_DATABASE_URL")
	if dsn == "" {
		t.Skip("TEST_DATABASE_URL required")
	}
	ctx := context.Background()
	base, err := pgxpool.New(ctx, dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer base.Close()
	if !strings.HasSuffix(base.Config().ConnConfig.Database, "_test") {
		t.Fatal("disposable database required")
	}
	schema := fmt.Sprintf("legacy032_%d", time.Now().UnixNano())
	defer base.Exec(ctx, "DROP SCHEMA IF EXISTS "+dblayout.DropList(schema)+" CASCADE")
	if _, err = base.Exec(ctx, "CREATE SCHEMA "+pgx.Identifier{schema}.Sanitize()); err != nil {
		t.Fatal(err)
	}
	config, _, err := postgres.Config(dsn, schema)
	if err != nil {
		t.Fatal(err)
	}
	old, err := pgxpool.NewWithConfig(ctx, config)
	if err != nil {
		t.Fatal(err)
	}
	defer old.Close()
	schemaSQL, _ := testAssets.ReadFile("schema.sql")
	if _, err = old.Exec(ctx, string(schemaSQL)); err != nil {
		t.Fatal(err)
	}
	raw, _ := json.Marshal(universe.Generate(1000, 20261026))
	if _, err = old.Exec(ctx, `INSERT INTO galaxies(name,data) VALUES('Edem',$1)`, raw); err != nil {
		t.Fatal(err)
	}
	var uid, sid int64
	if err = old.QueryRow(ctx, `INSERT INTO users(username,password_hash,credits,ore) VALUES('legacy_pilot','hash',12345,7) RETURNING id`).Scan(&uid); err != nil {
		t.Fatal(err)
	}
	if err = old.QueryRow(ctx, `INSERT INTO ships(user_id,cargo,fuel) VALUES($1,'{"iron":9}',12) RETURNING id`, uid).Scan(&sid); err != nil {
		t.Fatal(err)
	}
	if _, err = old.Exec(ctx, `INSERT INTO mining_jobs(user_id,star,planet,resource,quantity,finish_tick) VALUES($1,1,101,'carbon',3,1)`, uid); err != nil {
		t.Fatal(err)
	}
	if _, err = old.Exec(ctx, `INSERT INTO surveys(user_id,star) VALUES($1,5);`, uid); err != nil {
		t.Fatal(err)
	}
	if _, err = old.Exec(ctx, `INSERT INTO reservations(user_id,star,resource,quantity,cost) VALUES($1,1,'agni',2,22)`, uid); err != nil {
		t.Fatal(err)
	}
	if _, err = old.Exec(ctx, `INSERT INTO companies(name,tag,owner_id) VALUES('Existing player corporation','F01',$1)`, uid); err != nil {
		t.Fatal(err)
	}
	db, err := openDatabase(ctx, dsn, schema)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	var id, credits int64
	var fuel int
	var cargo []byte
	if err = db.QueryRow(ctx, `SELECT s.id,u.credits,s.fuel,s.cargo FROM ships s JOIN users u ON u.id=s.user_id WHERE u.id=$1 AND s.active`, uid).Scan(&id, &credits, &fuel, &cargo); err != nil {
		t.Fatal(err)
	}
	var goods map[string]int
	_ = json.Unmarshal(cargo, &goods)
	if id != sid || credits != 12345 || fuel != 12 || goods["iron"] != 9 {
		t.Fatal("legacy assets changed", id, credits, fuel, goods)
	}
	var jobShip int64
	if err = db.QueryRow(ctx, `SELECT ship_id FROM mining_jobs WHERE user_id=$1`, uid).Scan(&jobShip); err != nil || jobShip != sid {
		t.Fatal("legacy mining assignment", err)
	}
	var founderCount int
	if err = db.QueryRow(ctx, `SELECT count(*) FROM companies WHERE founder`).Scan(&founderCount); err != nil || founderCount != 4 {
		t.Fatal("founder tag collision", founderCount, err)
	}
	var oldOwner int64
	if err = db.QueryRow(ctx, `SELECT owner_id FROM companies WHERE tag='F01'`).Scan(&oldOwner); err != nil || oldOwner != uid {
		t.Fatal("player corporation appropriated", err)
	}
	app := server.New(db, server.Config{})
	if err = app.Simulation.AdvanceTick(ctx, true); err != nil {
		t.Fatal(err)
	}
	if err = app.Simulation.AdvanceTick(ctx, true); err != nil {
		t.Fatal(err)
	}
	var carbon int
	if err = db.QueryRow(ctx, `SELECT (cargo->>'carbon')::int FROM ships WHERE id=$1`, sid).Scan(&carbon); err != nil || carbon != 3 {
		t.Fatal("legacy job delivered incorrectly", carbon, err)
	}
	if _, err = db.Exec(ctx, `INSERT INTO ships(user_id,active) VALUES($1,FALSE)`, uid); err != nil {
		t.Fatal(err)
	}
	again, err := openDatabase(ctx, dsn, schema)
	if err != nil {
		t.Fatal(err)
	}
	again.Close()
	var count int
	if err = db.QueryRow(ctx, `SELECT count(*) FROM ships WHERE user_id=$1`, uid).Scan(&count); err != nil || count != 2 {
		t.Fatal("fleet lost on restart", err)
	}
	if err = db.QueryRow(ctx, `SELECT count(*) FROM surveys WHERE user_id=$1 AND star=5`, uid).Scan(&count); err != nil || count != 1 {
		t.Fatal("survey lost", err)
	}
	if err = db.QueryRow(ctx, `SELECT count(*) FROM reservations WHERE user_id=$1 AND cost=22 AND quantity=2 AND status='reserved'`, uid).Scan(&count); err != nil || count != 1 {
		t.Fatal("reservation lost", err)
	}
}

func Test033BuiltPortAndPhysicalCorporateStorage(t *testing.T) {
	f := newFixture033(t)
	c, uid := f.signup("builder033")
	f.exec(`UPDATE users SET credits=2000000 WHERE id=$1`, uid)
	f.req(c, "POST", "/api/companies", map[string]any{"action": "leave"}, 200)
	f.req(c, "POST", "/api/companies", map[string]any{"action": "create", "name": "Builders", "tag": "BLD", "color": "#123456", "flag": "◆"}, 200)
	cid := f.integer(`SELECT company_id FROM users WHERE id=$1`, uid)
	f.req(c, "GET", "/api/workbench", nil, 200)
	for _, kind := range []string{"warehouse", "port"} {
		f.cmd(c, map[string]any{"action": "project_create", "name": "Project " + kind, "target": kind}, 200)
		project := f.integer(`SELECT max(id) FROM company_projects WHERE company_id=$1`, cid)
		f.exec(`INSERT INTO warehouses(user_id,star,station,cargo) VALUES($1,1,'station','{"iron":200,"silicon":100,"orichalcum":20}') ON CONFLICT(user_id,star,station) DO UPDATE SET cargo=EXCLUDED.cargo`, uid)
		need := map[string]int{"iron": 200, "silicon": 100, "orichalcum": 20}
		if kind == "warehouse" {
			need = map[string]int{"iron": 100, "silicon": 50}
		}
		for r, q := range need {
			f.cmd(c, map[string]any{"action": "project_contribute", "id": project, "resource": r, "quantity": q}, 200)
		}
	}
	f.exec(`UPDATE ships SET cargo='{"iron":3}' WHERE user_id=$1`, uid)
	f.cmd(c, map[string]any{"action": "company_store", "resource": "iron", "quantity": 3}, 200)
	f.exec(`UPDATE ships SET object_id='station-2' WHERE user_id=$1`, uid)
	f.cmd(c, map[string]any{"action": "company_cargo_withdraw", "resource": "iron", "quantity": 3}, 409)
	f.exec(`UPDATE ships SET object_id='station' WHERE user_id=$1`, uid)
	f.cmd(c, map[string]any{"action": "company_cargo_withdraw", "resource": "iron", "quantity": 3}, 200)
	project := f.integer(`SELECT id FROM company_projects WHERE company_id=$1 AND kind='port'`, cid)
	target := fmt.Sprintf("project-%d", project)
	f.req(c, "POST", "/api/expedition", map[string]any{"action": "move", "target": target, "mode": "docked"}, 200)
	f.exec(`UPDATE ships SET local_arrival=now()-interval '1 second' WHERE user_id=$1`, uid)
	response := f.req(c, "GET", "/api/expedition", nil, 200)
	var data struct {
		Docked bool `json:"docked"`
	}
	_ = json.Unmarshal(response.Body.Bytes(), &data)
	if !data.Docked {
		t.Fatal("built port cannot be docked")
	}
	f.req(c, "POST", "/api/expedition", map[string]any{"action": "buy", "resource": "agni", "quantity": 1}, 200)
}
