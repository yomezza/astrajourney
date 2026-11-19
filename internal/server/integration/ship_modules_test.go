package integration

import (
	"encoding/json"
	"fmt"
	"github.com/yomezza/cosmosimulator/internal/fleet"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
)

func TestShipModulesTransactions(t *testing.T) {
	f := newFixture033(t)
	cookie, uid := f.signup("module_pilot")
	_, other := f.signup("module_other")
	sid := f.integer(`SELECT id FROM ships WHERE user_id=$1 AND active`, uid)
	otherShip := f.integer(`SELECT id FROM ships WHERE user_id=$1 AND active`, other)
	f.exec(`UPDATE ships SET star=1,object_id='station',position_mode='docked',cargo='{}' WHERE id=$1`, sid)
	f.exec(`UPDATE users SET credits=1000000 WHERE id=$1`, uid)
	f.req(cookie, "GET", "/api/workbench", nil, 200)
	initial := f.integer(`SELECT count(*) FROM ship_modules WHERE ship_id=$1 AND installed`, sid)
	if initial != 2 {
		t.Fatalf("default loadout=%d", initial)
	}
	f.req(cookie, "GET", fmt.Sprintf("/api/ship/modules?ship=%d", otherShip), nil, 404)
	f.req(cookie, "GET", fmt.Sprintf("/api/ship/modules?ship=%d", sid), nil, 200)
	f.exec(`INSERT INTO station_module_stock(star,station,catalog_id,quantity,price) VALUES(1,'station','scanner-C',1,1500) ON CONFLICT(star,station,catalog_id) DO UPDATE SET quantity=1,price=1500`)
	// The last unit can only be sold once under simultaneous requests.
	before := f.integer(`SELECT credits FROM users WHERE id=$1`, uid)
	codes := make(chan int, 2)
	var wg sync.WaitGroup
	for i := 0; i < 2; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			codes <- moduleRequest(f, cookie, map[string]any{"action": "module_buy", "ship": sid, "star": 1, "station": "station", "target": "scanner-C", "price": 1500})
		}()
	}
	wg.Wait()
	close(codes)
	success := 0
	for code := range codes {
		if code == 200 {
			success++
		} else if code != 409 {
			t.Fatalf("buy returned %d", code)
		}
	}
	if success != 1 || f.integer(`SELECT credits FROM users WHERE id=$1`, uid) != before-1500 {
		t.Fatal("last-unit purchase duplicated or balance incorrect")
	}
	mid := f.integer(`SELECT id FROM ship_modules WHERE ship_id=$1 AND catalog_id='scanner-C'`, sid)
	f.cmd(cookie, map[string]any{"action": "module_install", "ship": sid, "id": mid}, 200)
	f.cmd(cookie, map[string]any{"action": "module_install", "ship": sid, "id": mid}, 409)
	f.cmd(cookie, map[string]any{"action": "module_remove", "ship": sid, "id": mid}, 200)
	f.cmd(cookie, map[string]any{"action": "module_sell", "ship": sid, "id": mid}, 200)
	f.cmd(cookie, map[string]any{"action": "module_sell", "ship": sid, "id": mid}, 409)
	if f.integer(`SELECT credits FROM users WHERE id=$1`, uid) != before-750 {
		t.Fatal("sale paid incorrect refund")
	}
	// Removing the starter rack must not discard existing cargo.
	rack := f.integer(`SELECT id FROM ship_modules WHERE ship_id=$1 AND catalog_id='cargo-1-C'`, sid)
	f.exec(`UPDATE ships SET cargo='{"iron":60}' WHERE id=$1`, sid)
	f.cmd(cookie, map[string]any{"action": "module_remove", "ship": sid, "id": rack}, 409)
	s, err := fleet.ReadShip(f.ctx, f.db, sid)
	if err != nil {
		t.Fatal(err)
	}
	if s.CargoUsed() != 60 || s.Capacity() != 60 {
		t.Fatalf("cargo lost or capacity changed: %+v", s)
	}

	f.exec(`UPDATE ships SET cargo='{}' WHERE id=$1`, sid)
	f.cmd(cookie, map[string]any{"action": "module_remove", "ship": sid, "id": rack}, 200)
	f.cmd(cookie, map[string]any{"action": "module_sell", "ship": sid, "id": rack}, 200)
	tx, err := f.db.Begin(f.ctx)
	if err != nil {
		t.Fatal(err)
	}
	if err = fleet.EnsureModuleDefaults(f.ctx, tx); err != nil {
		tx.Rollback(f.ctx)
		t.Fatal(err)
	}
	if err = tx.Commit(f.ctx); err != nil {
		t.Fatal(err)
	}
	if f.integer(`SELECT count(*) FROM ship_modules WHERE ship_id=$1`, sid) != 1 {
		t.Fatal("restart recreated sold default modules")
	}
	add := func(id string) int64 {
		f.exec(`INSERT INTO ship_modules(ship_id,catalog_id,definition) SELECT $1,id,(to_jsonb(c)-'id')||jsonb_build_object('catalog_id',id) FROM ship_module_catalog c WHERE id=$2`, sid, id)
		return f.integer(`SELECT max(id) FROM ship_modules WHERE ship_id=$1 AND catalog_id=$2`, sid, id)
	}
	scoop := add("scoop-C")
	f.cmd(cookie, map[string]any{"action": "module_install", "ship": sid, "id": scoop}, 200)
	ore := add("extractor-universal-B")
	f.cmd(cookie, map[string]any{"action": "module_install", "ship": sid, "id": ore}, 409)
	drive := add("drive-S")
	f.cmd(cookie, map[string]any{"action": "module_install", "ship": sid, "id": drive}, 200)
	scanner := add("scanner-S")
	f.cmd(cookie, map[string]any{"action": "module_install", "ship": sid, "id": scanner}, 409)
	s, err = fleet.ReadShip(f.ctx, f.db, sid)
	if err != nil {
		t.Fatal(err)
	}
	if s.ModuleUsed() != 10 || s.Hull().JumpSeconds >= 5 {
		t.Fatal("fitting budget or jump drive effects wrong")
	}
	f.exec(`UPDATE ships SET object_id='entry',position_mode='approach',fuel=0 WHERE id=$1`, sid)
	f.cmd(cookie, map[string]any{"action": "module_scoop", "ship": sid}, 200)
	f.cmd(cookie, map[string]any{"action": "module_scoop", "ship": sid}, 409)
	if f.integer(`SELECT fuel FROM ships WHERE id=$1`, sid) != 2 {
		t.Fatal("scoop did not respect its rate/cooldown")
	}
}

func moduleRequest(f *fixture033, cookie *http.Cookie, body map[string]any) int {
	raw, _ := json.Marshal(body)
	r := httptest.NewRequest("POST", "/api/workbench", strings.NewReader(string(raw)))
	r.AddCookie(cookie)
	r.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	f.handler.ServeHTTP(w, r)
	return w.Code
}
