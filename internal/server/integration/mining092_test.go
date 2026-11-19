package integration

import (
	"encoding/json"
	"testing"

	"github.com/yomezza/cosmosimulator/internal/fleet"
	resourcecatalog "github.com/yomezza/cosmosimulator/internal/resources"
	"github.com/yomezza/cosmosimulator/internal/universe"
)

func TestCurrentMiningStats092(t *testing.T) {
	f := newFixture033(t)
	cookie, uid := f.signup("mining092")
	f.exec(`UPDATE users SET credits=50000 WHERE id=$1`, uid)
	sid := f.integer(`SELECT id FROM ships WHERE user_id=$1 AND active`, uid)
	g, err := f.app.World.Load(f.ctx)
	if err != nil {
		t.Fatal(err)
	}
	resource := ""
	for _, site := range universe.SystemSites(g.Stars[0]) {
		if site.ID == "asteroids" {
			for _, id := range site.Resources {
				if resourcecatalog.ExtractorAccepts("ore", id) {
					resource = id
					break
				}
			}
		}
	}
	if resource == "" {
		t.Fatal("no compatible test deposit")
	}
	view := func() fleet.Ship {
		t.Helper()
		var out struct {
			Ship fleet.Ship `json:"ship"`
		}
		if err := json.Unmarshal(f.req(cookie, "GET", "/api/expedition", nil, 200).Body.Bytes(), &out); err != nil {
			t.Fatal(err)
		}
		return out.Ship
	}
	// Explicit C equipment keeps the original hull-output assertions independent
	// of the new mining hulls' default B loadout.
	equip := func(kind string) int64 {
		t.Helper()
		f.exec(`INSERT INTO ship_modules(ship_id,catalog_id,definition) SELECT $1,id,(to_jsonb(c)-'id')||jsonb_build_object('catalog_id',id) FROM ship_module_catalog c WHERE id=$2`, sid, "extractor-"+kind+"-C")
		id := f.integer(`SELECT max(id) FROM ship_modules WHERE ship_id=$1 AND catalog_id=$2`, sid, "extractor-"+kind+"-C")
		f.cmd(cookie, map[string]any{"action": "module_install", "ship": sid, "id": id}, 200)
		return id
	}
	for _, class := range []string{"pilgrim", "forge", "titan"} {
		t.Run(class, func(t *testing.T) {
			f.exec(`UPDATE ships SET class=$2,star=1,object_id='station',position_mode='docked',fuel=20,cargo='{}' WHERE id=$1`, sid, class)
			f.exec(`DELETE FROM ship_modules WHERE ship_id=$1 AND definition->>'kind'='extractor'`, sid)
			standard := equip("universal")
			base := view()
			want := map[string]int{"pilgrim": 3, "forge": 5, "titan": 8}[class]
			if base.Spec.Mining != want || base.Mining.Current == nil || *base.Mining.Current != want || base.Mining.Min != want || base.Mining.Max != want {
				t.Fatalf("incorrect standard output: %+v %+v", base.Spec, base.Mining)
			}
			f.cmd(cookie, map[string]any{"action": "module_remove", "ship": sid, "id": standard}, 200)
			equip("ore")
			upgraded := view()
			if upgraded.Mining.Current != nil || upgraded.Mining.Min != want+1 || upgraded.Mining.Max != want+3 {
				t.Fatalf("installed extractor not reflected at port: %+v", upgraded.Mining)
			}
			f.exec(`INSERT INTO surveys(user_id,star) VALUES($1,1) ON CONFLICT DO NOTHING`, uid)
			f.exec(`UPDATE ships SET object_id='asteroids',position_mode='approach' WHERE id=$1`, sid)
			atDeposit := view()
			if atDeposit.Mining.Current == nil || *atDeposit.Mining.Current < upgraded.Mining.Min || *atDeposit.Mining.Current > upgraded.Mining.Max {
				t.Fatalf("exact deposit output missing or out of range: %+v", atDeposit.Mining)
			}
			f.req(cookie, "POST", "/api/expedition", map[string]any{"action": "mine", "ship": sid, "planet": 101, "resource": resource}, 200)
			if actual := f.integer(`SELECT quantity FROM mining_jobs WHERE ship_id=$1`, sid); actual != int64(*atDeposit.Mining.Current) {
				t.Fatalf("display %d differs from actual job %d", *atDeposit.Mining.Current, actual)
			}
			f.req(cookie, "POST", "/api/expedition", map[string]any{"action": "cancel_mine", "ship": sid}, 200)
		})
	}

	// Exact figures must not reveal unscanned deposits or apply while manoeuvring.
	f.exec(`DELETE FROM surveys WHERE user_id=$1`, uid)
	if s := view(); s.Mining.Current != nil {
		t.Fatal("unrevealed deposit contributed exact output")
	}
	f.exec(`INSERT INTO surveys(user_id,star) VALUES($1,1)`, uid)
	f.exec(`UPDATE ships SET local_target='station',local_mode='docked',local_started=now(),local_arrival=now()+interval '1 hour' WHERE id=$1`, sid)
	if s := view(); s.Mining.Current != nil {
		t.Fatal("moving ship retained exact deposit output")
	}
}
