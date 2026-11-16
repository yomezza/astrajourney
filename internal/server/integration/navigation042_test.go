package integration

import (
	"bytes"
	"encoding/json"
	"math"
	"testing"

	"github.com/yomezza/cosmosimulator/internal/fleet"
	"github.com/yomezza/cosmosimulator/internal/universe"
)

func TestAdminTeleport(t *testing.T) {
	f := newFixture033(t)
	admin, uid := f.signup("teleport042")
	player, otherUID := f.signup("ordinary042")
	sid := f.integer(`SELECT id FROM ships WHERE user_id=$1`, uid)
	otherShip := f.integer(`SELECT id FROM ships WHERE user_id=$1`, otherUID)
	teleport := func(body map[string]any, status int) {
		t.Helper()
		f.req(admin, "POST", "/api/admin/teleport", body, status)
	}
	f.req(nil, "POST", "/api/admin/teleport", map[string]any{"to": 5}, 401)
	f.req(player, "POST", "/api/admin/teleport", map[string]any{"to": 5}, 403)
	teleport(map[string]any{"to": 5}, 403)
	f.exec(`UPDATE users SET role='admin' WHERE id=$1`, uid)
	for _, to := range []int{-1, 0, 3001, 10001} {
		teleport(map[string]any{"to": to}, 400)
	}
	teleport(map[string]any{"to": 5, "ship": otherShip}, 404)
	if f.integer(`SELECT count(*) FROM admin_log WHERE action='teleport'`) != 0 {
		t.Fatal("failed attempts were applied")
	}
	credits := f.integer(`SELECT credits FROM users WHERE id=$1`, uid)
	f.exec(`UPDATE ships SET fuel=17,cargo='{"iron":7}',destination=5,departure=now(),arrival=now()+interval '1 hour',travel_path='[1,5]',waypoints='[9]',reserved_fuel=2,local_target='asteroids',local_mode='approach',local_started=now(),local_arrival=now()+interval '1 hour' WHERE id=$1`, sid)
	f.exec(`INSERT INTO research_jobs(ship_id,user_id,star,scanned,total,next_at,seconds) VALUES($1,$2,5,2,8,now()+interval '1 hour',1)`, sid, uid)
	// Teleportation also reaches disconnected systems, without inventing a survey.
	teleport(map[string]any{"to": 1000, "ship": sid}, 200)
	s, err := fleet.ReadShip(f.ctx, f.db, sid)
	if err != nil {
		t.Fatal(err)
	}
	if s.Star != 1000 || s.ObjectID != "entry" || s.PositionMode != "approach" || s.Fuel != 17 || s.Cargo["iron"] != 7 || s.Destination != nil || s.Arrival != nil || s.Departure != nil || s.LocalTarget != nil || s.LocalMode != nil || s.LocalStarted != nil || s.LocalArrival != nil || len(s.Path) != 0 || len(s.Waypoints) != 0 || s.ReservedFuel != 0 {
		t.Fatalf("incomplete teleport: %+v", s)
	}
	if f.integer(`SELECT scanned FROM research_jobs WHERE ship_id=$1 AND paused`, sid) != 2 {
		t.Fatal("survey progress must be paused and preserved")
	}
	if f.integer(`SELECT credits FROM users WHERE id=$1`, uid) != credits || f.integer(`SELECT count(*) FROM surveys WHERE user_id=$1 AND star=1000`, uid) != 0 {
		t.Fatal("teleport changed finances or revealed unexplored objects")
	}
	f.req(admin, "GET", "/api/system?id=1000", nil, 200)
	if f.integer(`SELECT count(*) FROM admin_log WHERE actor=$1 AND action='teleport' AND (data->>'from')::int=1 AND (data->>'to')::int=1000 AND (data->>'ship')::bigint=$2`, uid, sid) != 1 {
		t.Fatal("missing audit record")
	}
	f.advance(uid)
	if f.integer(`SELECT star FROM ships WHERE id=$1 AND destination IS NULL`, sid) != 1000 {
		t.Fatal("old flight resumed after teleport")
	}
	// Refused teleports retain mining output and cargo contract state.
	f.exec(`INSERT INTO mining_jobs(user_id,ship_id,star,planet,resource,quantity,finish_tick) VALUES($1,$2,1000,100001,'iron',3,100)`, uid, sid)
	teleport(map[string]any{"to": 1}, 409)
	if f.integer(`SELECT count(*) FROM mining_jobs WHERE ship_id=$1`, sid) != 1 {
		t.Fatal("teleport discarded reserved mining output")
	}
	f.exec(`DELETE FROM mining_jobs WHERE ship_id=$1`, sid)
	f.exec(`INSERT INTO deliveries(owner,carrier,ship_id,from_star,from_station,to_star,to_station,resource,quantity,reward,status,deadline) VALUES($1,$2,$3,1,'station',5,'station','iron',1,10,'carrying',now()+interval '1 day')`, otherUID, uid, sid)
	teleport(map[string]any{"to": 1}, 409)
	f.exec(`DELETE FROM deliveries WHERE ship_id=$1`, sid)
	teleport(map[string]any{"to": 1}, 200)
	f.exec(`UPDATE users SET role='player' WHERE id=$1`, uid)
	teleport(map[string]any{"to": 5}, 403)
	if f.integer(`SELECT star FROM ships WHERE id=$1`, otherShip) != 1 {
		t.Fatal("another player's ship moved")
	}
}

func TestFrontierRingsUpgrade(t *testing.T) {
	f := newFixture033(t)
	_, uid := f.signup("rings050")
	legacy := universe.Generate(1000, 20261026)
	frontier := universe.Generate(1000, 20261027)
	for _, star := range frontier.Stars {
		star.ID += 1000
		star.Galaxy = "Edem-II"
		if star.Region < 0 {
			star.Region = -2
		} else {
			star.Region += 4
		}
		legacy.Stars = append(legacy.Stars, star)
	}
	for _, lane := range frontier.Lanes {
		legacy.Lanes = append(legacy.Lanes, universe.Lane{From: lane.From + 1000, To: lane.To + 1000})
	}
	legacy.Regions = append(legacy.Regions, universe.FrontierRegions...)
	raw, err := json.Marshal(legacy)
	if err != nil {
		t.Fatal(err)
	}
	f.stageLegacy(raw)
	// This fixture represents a pre-discovery-registry world, with different historical galaxy membership.
	f.exec(`TRUNCATE aj_discovery_name_history,aj_discovery_objects`)
	f.exec(`UPDATE portal SET active=TRUE`)
	f.exec(`UPDATE ships SET star=1001,destination=1005,departure=now(),arrival=now()+interval '1 hour',travel_path='[1001,1005]',cargo='{"iron":4}' WHERE user_id=$1`, uid)
	f.exec(`INSERT INTO surveys(user_id,star) VALUES($1,1001)`, uid)
	var schema string
	_ = f.db.QueryRow(f.ctx, `SELECT current_schema()`).Scan(&schema)
	var previous []byte
	for range 2 {
		db, err := openDatabase(f.ctx, f.db.Config().ConnString(), schema)
		if err != nil {
			t.Fatal(err)
		}
		db.Close()
		raw = f.worldJSON()
		var g universe.Galaxy
		if err = json.Unmarshal(raw, &g); err != nil {
			t.Fatal(err)
		}
		if len(g.Stars) != 4000 || len(universe.GalaxyStars(&g, "edem")) != 3000 || len(universe.GalaxyStars(&g, "asterion")) != 1000 || g.Definition("asterion").Name != "Астерион" {
			t.Fatal("galaxy membership lost")
		}
		for i, old := range legacy.Stars {
			if g.Stars[i].ID != old.ID || g.Stars[i].Name != old.Name {
				t.Fatal("legacy identity changed", old.ID)
			}
		}
		belts := [3]int{}
		for _, s := range universe.GalaxyStars(&g, "asterion") {
			r := math.Hypot(s.X, s.Y)
			if s.Region < 0 {
				continue
			}
			belt := int(math.Round((r - 390) / 290))
			if belt < 0 || belt > 2 || math.Abs(r-(390+float64(belt)*290)) > 41 {
				t.Fatal("frontier no longer rings", s.ID, r)
			}
			belts[belt]++
		}
		for _, n := range belts {
			if n < 50 {
				t.Fatal("missing ring", belts)
			}
		}
		if previous != nil && !bytes.Equal(previous, raw) {
			t.Fatal("restart changed the upgraded map again")
		}
		previous = append([]byte(nil), raw...)
		if f.integer(`SELECT count(*) FROM ships WHERE user_id=$1 AND star=1001 AND destination=1005 AND cargo='{"iron":4}' AND travel_path='[1001,1005]'`, uid) != 1 || f.integer(`SELECT count(*) FROM surveys WHERE user_id=$1 AND star=1001`, uid) != 1 {
			t.Fatal("legacy progress lost")
		}
	}
}
