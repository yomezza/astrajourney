package integration

import (
	"bytes"
	"encoding/json"
	"fmt"
	"math"
	"testing"

	"github.com/yomezza/cosmosimulator/internal/universe"
	"github.com/yomezza/cosmosimulator/internal/worldstore"
)

func TestFounderColoniesAndFrontier(t *testing.T) {
	f := newFixture033(t)
	g, err := f.app.World.Load(f.ctx)
	if err != nil {
		t.Fatal(err)
	}
	d := g.Definition("edem")
	claimed, inner, innerPorts, outer, outerPorts := 0, 0, 0, 0, 0
	owners := map[string]int{}
	footprint := 0.0
	for _, area := range d.Territories {
		center := g.Stars[area.Anchor-1]
		if math.Hypot(center.X, center.Y) > d.Radius*.65 || area.Radius > d.Radius*.04 {
			t.Fatal("colony too large or outside settled space", area)
		}
		owners[area.Founder]++
		footprint += area.Radius * area.Radius / (d.Radius * d.Radius)
	}
	for _, s := range universe.GalaxyStars(g, "edem") {
		r := math.Hypot(s.X, s.Y)
		if s.Founder != "" {
			claimed++
			inside := false
			for _, area := range d.Territories {
				p := g.Stars[area.Anchor-1]
				if area.Founder == s.Founder && math.Hypot(s.X-p.X, s.Y-p.Y) <= area.Radius {
					inside = true
				}
			}
			if !inside {
				t.Fatal("ownership outside colony", s.ID)
			}
		}
		if r < d.Radius*.3 {
			inner++
			if s.Ports != nil && *s.Ports > 0 && s.Founder != "" {
				innerPorts++
			}
		}
		if r > d.Radius*.7 {
			outer++
			if s.Founder != "" {
				t.Fatal("frontier claimed", s.ID)
			}
			if s.Ports != nil && *s.Ports > 0 && s.ID != 1 && s.ID != 5 {
				outerPorts++
			}
		}
	}
	if len(owners) != 2 || len(d.Territories) < 4 || footprint > .12 || claimed == 0 || claimed > 1200 {
		t.Fatal("colonies cover too much of the galaxy", owners, len(d.Territories), footprint, claimed)
	}
	if inner < 50 || innerPorts < 5 || outer < 300 || outerPorts != 0 {
		t.Fatal("station density lacks a neutral frontier", inner, innerPorts, outer, outerPorts)
	}
	t.Logf("%d founder systems, %d neutral, %d colonies, %.2f%% map footprint; %d/%d inner systems with founder ports; %d clean outer systems", claimed, len(g.Stars)-claimed, len(d.Territories), footprint*100, innerPorts, inner, outer)
}

func TestFounderSettlementMigrationPreservesProperty(t *testing.T) {
	f := newFixture033(t)
	_, uid := f.signup("frontier051")
	g, err := f.app.World.Load(f.ctx)
	if err != nil {
		t.Fatal(err)
	}
	raw, err := json.Marshal(g)
	if err != nil {
		t.Fatal(err)
	}
	var legacy universe.Galaxy
	if err = json.Unmarshal(raw, &legacy); err != nil {
		t.Fatal(err)
	}
	legacy.SettlementVersion = 0
	for i := range legacy.Catalog {
		legacy.Catalog[i].Territories = nil
	}
	outerStar := 0
	for i := range legacy.Stars {
		s := &legacy.Stars[i]
		n := 0
		if s.Region >= 0 {
			s.Founder = fmt.Sprintf("founder-%02d", 1+(s.Region%4)/2)
			if s.ID%3 != 0 {
				n = 2
			}
		}
		s.Ports = &n
		if outerStar == 0 && s.ID != 1 && s.ID != 5 && math.Hypot(s.X, s.Y) > legacy.Definition("edem").Radius*.75 {
			outerStar = s.ID
		}
	}
	if outerStar == 0 {
		t.Fatal("missing frontier fixture")
	}
	tx, err := f.db.Begin(f.ctx)
	if err != nil {
		t.Fatal(err)
	}
	if err = worldstore.EnsureStations(f.ctx, tx, &legacy); err != nil {
		tx.Rollback(f.ctx)
		t.Fatal(err)
	}
	if err = tx.Commit(f.ctx); err != nil {
		t.Fatal(err)
	}
	f.exec(`INSERT INTO stations(star,object_id,name,specialization,founder) VALUES($1,'external-hold','Protected frontier port','metals',(SELECT id FROM companies WHERE founder_key='founder-01'))`, outerStar)
	f.exec(`INSERT INTO warehouses(user_id,star,station,cargo) VALUES($1,$2,'external-hold','{"iron":7}')`, uid, outerStar)
	f.exec(`UPDATE ships SET star=$2,object_id='external-hold',cargo='{"agni":2}',fuel=23 WHERE user_id=$1`, uid, outerStar)
	f.exec(`INSERT INTO surveys(user_id,star) VALUES($1,$2)`, uid, outerStar)
	raw, err = json.Marshal(legacy)
	if err != nil {
		t.Fatal(err)
	}
	f.stageLegacy(raw)
	credits := f.integer(`SELECT credits FROM users WHERE id=$1`, uid)
	var schema string
	if err = f.db.QueryRow(f.ctx, `SELECT current_schema()`).Scan(&schema); err != nil {
		t.Fatal(err)
	}
	var previous []byte
	for range 2 {
		db, e := openDatabase(f.ctx, f.db.Config().ConnString(), schema)
		if e != nil {
			t.Fatal(e)
		}
		db.Close()
		raw = f.worldJSON()
		var upgraded universe.Galaxy
		if err = json.Unmarshal(raw, &upgraded); err != nil {
			t.Fatal(err)
		}
		if upgraded.SettlementVersion != 1 || upgraded.Revision != legacy.Revision+1 {
			t.Fatal("migration revision incorrect")
		}
		for i, s := range legacy.Stars {
			actual := upgraded.Stars[i]
			if actual.ID != s.ID || actual.Name != s.Name || actual.X != s.X || actual.Y != s.Y {
				t.Fatal("system moved or renamed", s.ID)
			}
		}
		if f.integer(`SELECT count(*) FROM stations WHERE star=$1 AND object_id='external-hold' AND active AND founder IS NULL`, outerStar) != 1 {
			t.Fatal("protected port not retained as neutral")
		}
		if f.integer(`SELECT (cargo->>'iron')::int FROM warehouses WHERE user_id=$1 AND star=$2`, uid, outerStar) != 7 || f.integer(`SELECT credits FROM users WHERE id=$1`, uid) != credits || f.integer(`SELECT count(*) FROM ships WHERE user_id=$1 AND star=$2 AND fuel=23 AND cargo='{"agni":2}'`, uid, outerStar) != 1 || f.integer(`SELECT count(*) FROM surveys WHERE user_id=$1 AND star=$2`, uid, outerStar) != 1 {
			t.Fatal("property or discovery lost")
		}
		if f.integer(`SELECT count(*) FROM stations WHERE NOT active`) == 0 {
			t.Fatal("unused old ports were not archived")
		}
		if previous != nil && !bytes.Equal(raw, previous) {
			t.Fatal("migration changed the world again on restart")
		}
		previous = append([]byte(nil), raw...)
	}
}

func TestSecondShipRefueling(t *testing.T) {
	f := newFixture033(t)
	cookie, uid := f.signup("refuel051")
	first := f.integer(`SELECT id FROM ships WHERE user_id=$1 AND active`, uid)
	f.exec(`UPDATE users SET credits=1000000,level=20 WHERE id=$1`, uid)
	f.cmd(cookie, map[string]any{"action": "buy_ship", "target": "wanderer"}, 200)
	second := f.integer(`SELECT id FROM ships WHERE user_id=$1 AND class='wanderer'`, uid)
	f.cmd(cookie, map[string]any{"action": "select_ship", "ship": second}, 200)
	f.req(cookie, "POST", "/api/expedition", map[string]any{"action": "buy", "resource": "agni", "quantity": 2, "ship": second}, 200)
	full := f.req(cookie, "POST", "/api/expedition", map[string]any{"action": "refuel", "ship": second}, 409)
	if !bytes.Contains(full.Body.Bytes(), []byte("Бак полон")) || f.integer(`SELECT (cargo->>'agni')::int FROM ships WHERE id=$1`, second) != 2 {
		t.Fatal("full tank spent Agni or returned wrong reason")
	}
	f.req(cookie, "POST", "/api/expedition", map[string]any{"action": "move", "target": "station-2", "mode": "docked", "ship": second}, 200)
	f.exec(`UPDATE ships SET local_arrival=now()-interval '1 second' WHERE id=$1`, second)
	f.req(cookie, "GET", "/api/fleet", nil, 200)
	f.req(cookie, "POST", "/api/expedition", map[string]any{"action": "refuel", "ship": second}, 200)
	if f.integer(`SELECT fuel FROM ships WHERE id=$1`, second) != 80 || f.integer(`SELECT (cargo->>'agni')::int FROM ships WHERE id=$1`, second) != 1 || f.integer(`SELECT fuel FROM ships WHERE id=$1`, first) != 30 {
		t.Fatal("refuel applied to wrong ship or capacity")
	}
	credits := f.integer(`SELECT credits FROM users WHERE id=$1`, uid)
	f.req(cookie, "POST", "/api/expedition", map[string]any{"action": "buy", "resource": "agni", "quantity": 1, "ship": first}, 409)
	f.req(cookie, "POST", "/api/expedition", map[string]any{"action": "refuel", "ship": first}, 409)
	if f.integer(`SELECT credits FROM users WHERE id=$1`, uid) != credits || f.integer(`SELECT (cargo->>'agni')::int FROM ships WHERE id=$1`, second) != 1 {
		t.Fatal("stale ship action changed inventory")
	}
	f.cmd(cookie, map[string]any{"action": "select_ship", "ship": first}, 200)
	f.req(cookie, "POST", "/api/expedition", map[string]any{"action": "buy", "resource": "agni", "quantity": 1, "ship": first}, 200)
	f.req(cookie, "POST", "/api/expedition", map[string]any{"action": "refuel", "ship": first}, 200)
	if f.integer(`SELECT fuel FROM ships WHERE id=$1`, first) != 35 || f.integer(`SELECT fuel FROM ships WHERE id=$1`, second) != 80 {
		t.Fatal("independent fuel tanks failed")
	}
}
