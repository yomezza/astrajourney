package integration

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/yomezza/cosmosimulator/internal/platform/dblayout"

	"github.com/yomezza/cosmosimulator/internal/commerce"
	"github.com/yomezza/cosmosimulator/internal/fleet"
	resourcecatalog "github.com/yomezza/cosmosimulator/internal/resources"
	"github.com/yomezza/cosmosimulator/internal/server"
	"github.com/yomezza/cosmosimulator/internal/universe"
)

func TestExpedition(t *testing.T) {
	dsn := os.Getenv("TEST_DATABASE_URL")
	if dsn == "" {
		t.Skip("TEST_DATABASE_URL required")
	}
	ctx := context.Background()
	admin, err := pgxpool.New(ctx, dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer admin.Close()
	if !strings.HasSuffix(admin.Config().ConnConfig.Database, "_test") {
		t.Fatal("test database required")
	}
	schema := fmt.Sprintf("voyage_%d", time.Now().UnixNano())
	defer admin.Exec(ctx, "DROP SCHEMA IF EXISTS "+dblayout.DropList(schema)+" CASCADE")
	db, err := openDatabase(ctx, dsn, schema)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	a := server.New(db, server.Config{})
	handler := a.Handler()
	var cookie *http.Cookie
	req := func(method, path, body string) *httptest.ResponseRecorder {
		r := httptest.NewRequest(method, path, strings.NewReader(body))
		if cookie != nil {
			r.AddCookie(cookie)
		}
		if method == "POST" {
			r.Header.Set("Content-Type", "application/json")
		}
		w := httptest.NewRecorder()
		handler.ServeHTTP(w, r)
		return w
	}
	check := func(w *httptest.ResponseRecorder, status int) {
		t.Helper()
		if w.Code != status {
			t.Fatalf("%d != %d: %s", w.Code, status, w.Body.String())
		}
	}
	check(req("GET", "/api/expedition", ""), 401)
	w := req("POST", "/api/register", `{"username":"voyager","password":"long-test-password"}`)
	check(w, 200)
	cookie = w.Result().Cookies()[0]
	get := func() struct {
		Ship     fleet.Ship    `json:"ship"`
		System   universe.Star `json:"system"`
		Credits  int64         `json:"credits"`
		Market   int64         `json:"market_credits"`
		Surveyed bool          `json:"surveyed"`
	} {
		t.Helper()
		w := req("GET", "/api/expedition", "")
		check(w, 200)
		var d struct {
			Ship     fleet.Ship    `json:"ship"`
			System   universe.Star `json:"system"`
			Credits  int64         `json:"credits"`
			Market   int64         `json:"market_credits"`
			Surveyed bool          `json:"surveyed"`
		}
		if err = json.Unmarshal(w.Body.Bytes(), &d); err != nil {
			t.Fatal(err)
		}
		return d
	}
	post := func(body string, status int) { t.Helper(); check(req("POST", "/api/expedition", body), status) }
	before := get()
	if before.Ship.Fuel != 30 || before.Ship.Star != 1 || len(before.System.Planets) != 0 {
		t.Fatal("invalid initial ship or exploration leak")
	}
	post(`{"action":"mine","planet":1,"resource":"orichalcum"}`, 409)
	post(`{"action":"scan"}`, 200)
	finishResearchForTest(t, a)
	scanned := get()
	if scanned.Credits != 990 || len(scanned.System.Planets) < 3 {
		t.Fatal("survey failed")
	}
	post(`{"action":"scan"}`, 200)
	if get().Credits != 990 {
		t.Fatal("survey charged twice")
	}
	id := scanned.System.Planets[0].Resources[0]
	mine := fmt.Sprintf(`{"action":"mine","planet":1,"resource":%q}`, id)
	if _, err = db.Exec(ctx, `UPDATE ships SET object_id='planet-1',position_mode='orbit' WHERE user_id=(SELECT id FROM users WHERE username='voyager')`); err != nil {
		t.Fatal(err)
	}
	post(mine, 200)
	post(mine, 409)
	if get().Ship.Cargo[id] != 0 {
		t.Fatal("mining delivered before tick")
	}
	if err = a.Simulation.AdvanceTick(ctx, true); err != nil {
		t.Fatal(err)
	}
	if get().Ship.Cargo[id] != 3 {
		t.Fatal("mining cooldown failed")
	}
	if _, err = db.Exec(ctx, `UPDATE ships SET object_id='station',position_mode='docked' WHERE user_id=(SELECT id FROM users WHERE username='voyager')`); err != nil {
		t.Fatal(err)
	}
	before = get()
	post(fmt.Sprintf(`{"action":"sell","resource":%q,"quantity":3}`, id), 200)
	afterSale := get()
	if afterSale.Credits+afterSale.Market != before.Credits+before.Market || afterSale.Ship.Cargo[id] != 0 {
		t.Fatal("sale conservation failed")
	}
	if _, err = db.Exec(ctx, `UPDATE resource_quotas SET used=capacity-2 WHERE star=1 AND object=1`); err != nil {
		t.Fatal(err)
	}
	if _, err = db.Exec(ctx, `UPDATE ships SET object_id='planet-1',position_mode='orbit' WHERE user_id=(SELECT id FROM users WHERE username='voyager')`); err != nil {
		t.Fatal(err)
	}
	post(mine, 200)
	if err = a.Simulation.AdvanceTick(ctx, true); err != nil {
		t.Fatal(err)
	}
	if _, err = db.Exec(ctx, `UPDATE ships SET object_id='station',position_mode='docked' WHERE user_id=(SELECT id FROM users WHERE username='voyager')`); err != nil {
		t.Fatal(err)
	}
	before = get()
	post(`{"action":"buy","resource":"agni","quantity":2}`, 200)
	after := get()
	if after.Credits+after.Market != before.Credits+before.Market || after.Ship.Cargo["agni"] != before.Ship.Cargo["agni"]+2 {
		t.Fatal("trade conservation failed")
	}
	post(`{"action":"buy","resource":"agni","quantity":-1}`, 409)
	post(`{"action":"buy","resource":"agni","quantity":60}`, 409)
	post(`{"action":"sell","resource":"ambrosia","quantity":60}`, 409)
	post(`{"action":"refuel"}`, 200)
	if get().Ship.Fuel != 35 {
		t.Fatal("refuel failed")
	}
	// Only one of two concurrent 31-unit buys can fit in a 60-unit hold.
	if _, err = db.Exec(ctx, `UPDATE local_markets SET stock=jsonb_set(stock,'{manna}','100') WHERE star=1`); err != nil {
		t.Fatal(err)
	}
	var wg sync.WaitGroup
	codes := make(chan int, 2)
	for i := 0; i < 2; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			codes <- req("POST", "/api/expedition", `{"action":"buy","resource":"manna","quantity":31}`).Code
		}()
	}
	wg.Wait()
	close(codes)
	success := 0
	for code := range codes {
		if code == 200 {
			success++
		} else if code != 409 {
			t.Fatalf("unexpected concurrent status %d", code)
		}
	}
	if success != 1 || fleet.CargoSize(get().Ship.Cargo) > 60 {
		t.Fatal("concurrent cargo overfill")
	}
	if _, err = db.Exec(ctx, `UPDATE ships SET fuel=0 WHERE user_id=(SELECT id FROM users WHERE username='voyager')`); err != nil {
		t.Fatal(err)
	}
	post(`{"action":"travel","to":5}`, 409)
	if _, err = db.Exec(ctx, `UPDATE ships SET fuel=35 WHERE user_id=(SELECT id FROM users WHERE username='voyager')`); err != nil {
		t.Fatal(err)
	}
	post(`{"action":"travel","to":2}`, 409)
	post(`{"action":"travel","to":1000}`, 409)
	before = get()
	post(`{"action":"travel","to":5}`, 200)
	after = get()
	if after.Ship.Destination == nil || after.Ship.Fuel != before.Ship.Fuel-2 {
		t.Fatal("travel not started")
	}
	post(`{"action":"scan"}`, 409)
	post(`{"action":"buy","resource":"manna","quantity":1}`, 409)
	if _, err = db.Exec(ctx, `UPDATE ships SET arrival=now()-interval '1 second' WHERE user_id=(SELECT id FROM users WHERE username='voyager')`); err != nil {
		t.Fatal(err)
	}
	after = get()
	if after.Ship.Star != 5 || after.Ship.Destination != nil || after.Surveyed {
		t.Fatal("arrival or survey isolation failed")
	}
	// The old bot currency faucet is gone: repeated trades conserve the station+bot balance.
	if _, err = db.Exec(ctx, `UPDATE users SET credits=0 WHERE is_bot`); err != nil {
		t.Fatal(err)
	}
	tx, err := db.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if err = commerce.TradeBots(ctx, tx); err != nil {
		t.Fatal(err)
	}
	if err = tx.Commit(ctx); err != nil {
		t.Fatal(err)
	}
	var money int
	if err = db.QueryRow(ctx, `SELECT sum(credits) FROM users WHERE is_bot`).Scan(&money); err != nil || money != 0 {
		t.Fatal("bankrupt bots spent money", err)
	}
}

func TestPlanetGeneration(t *testing.T) {
	g := universe.Generate(1000, 123)
	names := map[string]bool{}
	for _, r := range g.Regions {
		if names[r.Name] {
			t.Fatal("duplicate region")
		}
		names[r.Name] = true
	}
	if len(names) != 5 {
		t.Fatal("regions missing")
	}
	for _, s := range g.Stars {
		if s.Mass <= 0 || s.Luminosity <= 0 || len(s.Planets) < 3 {
			t.Fatal("star or planets missing")
		}
		last := 0.
		for _, p := range s.Planets {
			if p.AU <= last || p.Gravity <= 0 || p.Temperature < 0 {
				t.Fatal("invalid physical parameters")
			}
			last = p.AU
			for _, id := range p.Resources {
				if _, ok := resourcecatalog.Lookup(id); !ok {
					t.Fatal("unknown resource")
				}
			}
		}
	}
	coords := g.Stars[0]
	g.Version = 1
	g.Stars[0].Planets = nil
	universe.Enrich(&g)
	if g.Stars[0].X != coords.X || g.Stars[0].Class != coords.Class || len(g.Stars[0].Planets) != len(coords.Planets) {
		t.Fatal("enrichment replaced old stars")
	}
}
