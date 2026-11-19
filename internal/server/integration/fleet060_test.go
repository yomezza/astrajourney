package integration

import (
	"bytes"
	"encoding/json"
	"testing"

	"github.com/yomezza/cosmosimulator/internal/fleet"
	"github.com/yomezza/cosmosimulator/internal/universe"
)

func TestFleetProgression060(t *testing.T) {
	if len(fleet.Catalogue) != 15 {
		t.Fatal("expected fifteen hulls")
	}
	for _, line := range [][]string{
		{"wanderer", "vesper", "astra", "argus"},
		{"forge", "crucible", "titan"},
		{"ark", "atlas", "leviathan"},
		{"hermes", "iris", "zephyr", "lightning"},
	} {
		for i, id := range line {
			c, ok := fleet.Hull(id)
			if !ok || c.Price < 100000 {
				t.Fatal("invalid hull/price", id, c.Price)
			}
			if i == 0 {
				continue
			}
			previous, _ := fleet.Hull(line[i-1])
			if c.Cargo <= previous.Cargo || c.Fuel <= previous.Fuel || c.JumpSeconds > previous.JumpSeconds || c.JumpFuel > previous.JumpFuel || c.MiningBonus < previous.MiningBonus || c.Price <= previous.Price || c.Length <= previous.Length {
				t.Fatal("upgrade regresses or does not cost/grow more", previous, c)
			}
		}
	}
}

func TestFleetPurchases060(t *testing.T) {
	f := newFixture033(t)
	cookie, uid := f.signup("fleet060")
	f.exec(`UPDATE users SET credits=50000000,level=20 WHERE id=$1`, uid)
	if n := f.integer(`SELECT count(DISTINCT ship_class) FROM shipyard_stock y JOIN stations s ON s.star=y.star AND s.object_id=y.station WHERE s.active AND s.shipyard AND y.quantity>0`); n != 15 {
		t.Fatal("not all hulls are on sale", n)
	}
	if n := f.integer(`SELECT count(*) FROM shipyard_stock WHERE price<100000`); n != 0 {
		t.Fatal("price floor violated")
	}
	if n := f.integer(`SELECT max(n) FROM (SELECT count(*) n FROM shipyard_stock GROUP BY star,station) y`); n > 5 {
		t.Fatal("yard selection too broad", n)
	}
	for _, c := range fleet.Catalogue {
		t.Run(c.ID, func(t *testing.T) {
			var star, quantity int
			var station string
			var price int64
			if err := f.db.QueryRow(f.ctx, `SELECT y.star,y.station,y.price,y.quantity FROM shipyard_stock y JOIN stations s ON s.star=y.star AND s.object_id=y.station WHERE y.ship_class=$1 AND y.quantity>0 AND s.active AND s.shipyard ORDER BY y.star,y.station LIMIT 1`, c.ID).Scan(&star, &station, &price, &quantity); err != nil {
				t.Fatal(err)
			}
			f.exec(`UPDATE ships SET star=$2,object_id=$3,position_mode='docked' WHERE user_id=$1 AND active`, uid, star, station)
			before := f.integer(`SELECT credits FROM users WHERE id=$1`, uid)
			f.cmd(cookie, map[string]any{"action": "buy_ship", "target": c.ID, "star": star, "station": station, "price": price}, 200)
			sid := f.integer(`SELECT max(id) FROM ships WHERE user_id=$1`, uid)
			if f.integer(`SELECT fuel FROM ships WHERE id=$1`, sid) != int64(c.Fuel) || f.integer(`SELECT count(*) FROM ships WHERE id=$1 AND NOT active AND cargo='{}' AND class=$2`, sid, c.ID) != 1 || f.integer(`SELECT credits FROM users WHERE id=$1`, uid) != before-price || f.integer(`SELECT quantity FROM shipyard_stock WHERE star=$1 AND station=$2 AND ship_class=$3`, star, station, c.ID) != int64(quantity-1) {
				t.Fatal("purchase did not apply exact quote, tank or stock")
			}
			f.cmd(cookie, map[string]any{"action": "select_ship", "ship": sid}, 200)
			// Exercise every hull without accumulating a fleet beyond the experience allowance.
			f.exec(`DELETE FROM ships WHERE user_id=$1 AND NOT active AND class NOT IN ('crucible','titan') AND id<>(SELECT min(id) FROM ships WHERE user_id=$1)`, uid)
			f.exec(`UPDATE ships SET star=1,object_id='station' WHERE id=$1`, sid)
			response := f.req(cookie, "GET", "/api/estimate?to=5", nil, 200)
			var estimate struct {
				Fuel    int `json:"fuel"`
				Seconds int `json:"travel_seconds"`
			}
			if err := json.Unmarshal(response.Body.Bytes(), &estimate); err != nil || estimate.Fuel != c.JumpFuel || estimate.Seconds != c.JumpSeconds {
				t.Fatal("wrong route costs", estimate, err)
			}
			f.exec(`UPDATE ships SET fuel=$2,cargo='{"agni":1}' WHERE id=$1`, sid, c.Fuel-1)
			f.req(cookie, "POST", "/api/expedition", map[string]any{"action": "refuel", "ship": sid}, 200)
			if f.integer(`SELECT fuel FROM ships WHERE id=$1`, sid) != int64(c.Fuel) {
				t.Fatal("incorrect refuel capacity")
			}
			if _, err := f.db.Exec(f.ctx, `UPDATE ships SET fuel=$2 WHERE id=$1`, sid, c.Fuel+1); err == nil {
				t.Fatal("database accepts excess fuel")
			}
			f.exec(`INSERT INTO warehouses(user_id,star,station,cargo) VALUES($1,1,'station',jsonb_build_object('iron',$2::int)) ON CONFLICT(user_id,star,station) DO UPDATE SET cargo=EXCLUDED.cargo`, uid, c.Cargo+1)
			f.cmd(cookie, map[string]any{"action": "withdraw", "resource": "iron", "quantity": c.Cargo}, 200)
			f.cmd(cookie, map[string]any{"action": "withdraw", "resource": "iron", "quantity": 1}, 409)
			f.req(cookie, "POST", "/api/expedition", map[string]any{"action": "travel", "to": 5, "ship": sid}, 200)
			if f.integer(`SELECT round(extract(epoch FROM arrival-departure)) FROM ships WHERE id=$1`, sid) != int64(c.JumpSeconds) {
				t.Fatal("wrong actual flight duration")
			}
			f.exec(`UPDATE ships SET destination=NULL,arrival=NULL,departure=NULL,travel_path='[]',star=1,object_id='station',position_mode='docked',cargo='{}' WHERE id=$1`, sid)
		})
	}
	// Both new mining bonuses reach the shared-quota extraction path.
	f.exec(`INSERT INTO surveys(user_id,star) VALUES($1,1) ON CONFLICT DO NOTHING`, uid)
	g, err := f.app.World.Load(f.ctx)
	if err != nil {
		t.Fatal(err)
	}
	resource := ""
	for _, site := range universe.SystemSites(g.Stars[0]) {
		if site.ID == "asteroids" {
			resource = site.Resources[0]
		}
	}
	for _, id := range []string{"crucible", "titan"} {
		sid := f.integer(`SELECT id FROM ships WHERE user_id=$1 AND class=$2`, uid, id)
		f.cmd(cookie, map[string]any{"action": "select_ship", "ship": sid}, 200)
		f.exec(`UPDATE ships SET star=1,destination=NULL,arrival=NULL,departure=NULL,travel_path='[]',waypoints='[]',object_id='asteroids',position_mode='approach' WHERE id=$1`, sid)
		f.req(cookie, "POST", "/api/expedition", map[string]any{"action": "mine", "ship": sid, "planet": 101, "resource": resource}, 200)
		ship, err := fleet.ReadShip(f.ctx, f.db, sid)
		if err != nil {
			t.Fatal(err)
		}
		if n := f.integer(`SELECT quantity FROM mining_jobs WHERE ship_id=$1`, sid); n != int64(ship.ExtractionYieldFor(1, 101, resource)) {
			t.Fatal("wrong extraction bonus", id, n)
		}
	}
}

func TestFleetMigration060(t *testing.T) {
	f := newFixture033(t)
	_, uid := f.signup("migrate060")
	f.exec(`UPDATE users SET credits=234567 WHERE id=$1`, uid)
	f.exec(`UPDATE ships SET fuel=17,cargo='{"agni":2,"iron":7}' WHERE user_id=$1`, uid)
	f.exec(`UPDATE shipyard_stock SET quantity=0,price=28000 WHERE star=1 AND station='station' AND ship_class='wanderer'`)
	f.exec(`DELETE FROM shipyard_stock WHERE ship_class NOT IN('pilgrim','wanderer','forge','ark','hermes')`)
	f.exec(`UPDATE stations SET hull_catalogue_version=0`)
	// Recreate the actual old database constraint, not just the old yard data.
	f.exec(`ALTER TABLE ships DROP CONSTRAINT ships_class_fuel; ALTER TABLE ships ADD CONSTRAINT ships_class_fuel CHECK(fuel>=0 AND fuel<=CASE class WHEN 'pilgrim' THEN 40 WHEN 'wanderer' THEN 80 WHEN 'forge' THEN 60 WHEN 'ark' THEN 80 WHEN 'hermes' THEN 100 ELSE -1 END)`)
	var schema string
	if err := f.db.QueryRow(f.ctx, `SELECT current_schema()`).Scan(&schema); err != nil {
		t.Fatal(err)
	}
	var snapshot []byte
	for pass := range 2 {
		db, err := openDatabase(f.ctx, f.db.Config().ConnString(), schema)
		if err != nil {
			t.Fatal(err)
		}
		db.Close()
		if f.integer(`SELECT credits FROM users WHERE id=$1`, uid) != 234567 || f.integer(`SELECT count(*) FROM ships WHERE user_id=$1 AND fuel=17 AND cargo='{"agni":2,"iron":7}'`, uid) != 1 {
			t.Fatal("assets changed on upgrade")
		}
		if f.integer(`SELECT quantity FROM shipyard_stock WHERE star=1 AND station='station' AND ship_class='wanderer'`) != 0 || f.integer(`SELECT price FROM shipyard_stock WHERE star=1 AND station='station' AND ship_class='wanderer'`) < 100000 {
			t.Fatal("sold-out offer lost or price not upgraded")
		}
		if f.integer(`SELECT count(DISTINCT ship_class) FROM shipyard_stock`) != 15 {
			t.Fatal("missing expanded catalogue")
		}
		var now []byte
		if err = f.db.QueryRow(f.ctx, `SELECT jsonb_agg(to_jsonb(y) ORDER BY star,station,ship_class) FROM shipyard_stock y`).Scan(&now); err != nil {
			t.Fatal(err)
		}
		if pass > 0 && !bytes.Equal(snapshot, now) {
			t.Fatal("restart changed stocks or prices")
		}
		snapshot = now
	}
}
