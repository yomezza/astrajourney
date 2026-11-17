package integration

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"testing"

	"github.com/yomezza/cosmosimulator/internal/discovery"
	"github.com/yomezza/cosmosimulator/internal/universe"
)

func applyGalaxy280(t *testing.T, f *fixture033, admin *http.Cookie, in universe.GalaxyEdit) {
	t.Helper()
	var result struct {
		Preview string `json:"preview"`
	}
	if e := json.Unmarshal(f.req(admin, "POST", "/api/admin/galaxies/preview", in, 200).Body.Bytes(), &result); e != nil {
		t.Fatal(e)
	}
	in.Preview = result.Preview
	f.req(admin, "POST", "/api/admin/galaxies/apply", in, 200)
}
func TestNamingDictionariesAndEmptyGalaxy280(t *testing.T) {
	f := newFixture033(t)
	admin, uid := f.signup("dictionary280")
	f.exec(`UPDATE users SET role='admin' WHERE id=$1`, uid)
	if f.integer(`SELECT count(*) FROM station_names WHERE part='base'`) != 607 || f.integer(`SELECT count(*) FROM station_names WHERE part='suffix'`) != 37 {
		t.Fatal("unexpected initial vocabulary")
	}
	if f.integer(`SELECT count(*) FROM stations WHERE name ~ '[^ -~]'`) != 0 {
		t.Fatal("station names must be English")
	}
	f.exec(`UPDATE system_names SET active=false; INSERT INTO system_names(word,part) VALUES('TESTSYSTEM','base'),('Kepler','suffix') ON CONFLICT(word,part) DO UPDATE SET active=true`)
	f.exec(`UPDATE planet_names SET active=false; INSERT INTO planet_names(word,part) VALUES('TESTPLANET','base'),('Newton','suffix') ON CONFLICT(word,part) DO UPDATE SET active=true`)
	f.exec(`UPDATE station_names SET active=false; INSERT INTO station_names(word,part) VALUES('TESTPORT','base'),('Harbor','suffix') ON CONFLICT(word,part) DO UPDATE SET active=true`)
	in := universe.GalaxyEdit{Create: true, Empty: true, ID: "empty280", Name: "Empty Galaxy", Shape: "spiral", Seed: 280, Systems: 12, Settings: universe.DefaultGalaxySettings()}
	applyGalaxy280(t, f, admin, in)
	if f.integer(`SELECT count(*) FROM star_systems WHERE galaxy_id='empty280' AND (founder<>'' OR ports<>0)`) != 0 || f.integer(`SELECT count(*) FROM stations s JOIN system_identity i ON i.id=s.star WHERE i.galaxy_id='empty280'`) != 0 {
		t.Fatal("empty galaxy populated")
	}
	if f.integer(`SELECT count(*) FROM star_systems WHERE galaxy_id='empty280' AND name LIKE 'TESTSYSTEM Kepler%'`) != 12 {
		t.Fatal("system dictionary not used")
	}
	if f.integer(`SELECT count(*) FROM system_planets p JOIN system_identity i ON i.id=p.system_id WHERE i.galaxy_id='empty280' AND p.name NOT LIKE 'TESTPLANET Newton%'`) != 0 {
		t.Fatal("planet dictionary not used")
	}
	in.ID = "ports280"
	in.Empty = false
	in.Name = "Port Galaxy"
	in.Systems = 50
	in.Settings.StationPercent = 100
	applyGalaxy280(t, f, admin, in)
	if f.integer(`SELECT count(*) FROM stations s JOIN system_identity i ON i.id=s.star WHERE i.galaxy_id='ports280' AND name LIKE 'TESTPORT Harbor%'`) == 0 {
		t.Fatal("station dictionary not used")
	}
	if f.integer(`SELECT count(*)-count(DISTINCT lower(btrim(name))) FROM stations`) != 0 {
		t.Fatal("station collision")
	}
	f.exec(`UPDATE game_release SET version='9.8.7' WHERE id=1`)
	f.req(nil, "GET", "/api/server-status", nil, 200)
	var root string
	if e := f.db.QueryRow(f.ctx, `SELECT current_schema()`).Scan(&root); e != nil {
		t.Fatal(e)
	}
	again, e := openDatabase(f.ctx, f.db.Config().ConnString(), root)
	if e != nil {
		t.Fatal(e)
	}
	again.Close()
	if f.integer(`SELECT count(*) FROM system_names WHERE active AND word NOT IN('TESTSYSTEM','Kepler')`) != 0 {
		t.Fatal("restart restored disabled words")
	}
	var version string
	if e = f.db.QueryRow(f.ctx, `SELECT version FROM game_release WHERE id=1`).Scan(&version); e != nil || version != "9.8.7" {
		t.Fatal("restart overwrote database version", version, e)
	}
	// Empty mode survives persistence and expansion, including core systems.
	in = universe.GalaxyEdit{ID: "empty280", Name: "Empty Galaxy", Systems: 18, Settings: universe.DefaultGalaxySettings()}
	applyGalaxy280(t, f, admin, in)
	if f.integer(`SELECT count(*) FROM star_systems WHERE galaxy_id='empty280' AND (founder<>'' OR ports<>0)`) != 0 {
		t.Fatal("empty mode lost on expansion")
	}
	// No silent fallback to an embedded vocabulary.
	f.exec(`UPDATE system_names SET active=false`)
	in = universe.GalaxyEdit{Create: true, ID: "nodict280", Name: "No Dictionary", Shape: "spiral", Seed: 280, Systems: 5, Settings: universe.DefaultGalaxySettings()}
	response := f.req(admin, "POST", "/api/admin/galaxies/preview", in, 400)
	if !strings.Contains(response.Body.String(), "requires active") {
		t.Fatal(response.Body.String())
	}
	if f.integer(`SELECT count(*) FROM galaxy_catalog WHERE id='nodict280'`) != 0 {
		t.Fatal("failed preview created a galaxy")
	}
}
func TestSystemDiscovererOwnsPlanetRights280(t *testing.T) {
	f := newFixture033(t)
	first, uid1 := f.signup("planetfirst280")
	system, uid2 := f.signup("systemfirst280")
	s := neutral250(t, f)
	p := s.Planets[0]
	tx, e := f.db.Begin(f.ctx)
	if e != nil {
		t.Fatal(e)
	}
	defer tx.Rollback(context.Background())
	if e = discovery.ClaimScan(f.ctx, tx, uid1, s, 0, 1, false); e != nil {
		t.Fatal(e)
	}
	if e = discovery.ClaimScan(f.ctx, tx, uid2, s, 0, len(universe.SystemSites(s)), true); e != nil {
		t.Fatal(e)
	}
	if e = tx.Commit(f.ctx); e != nil {
		t.Fatal(e)
	}
	body := map[string]any{"operation": uuid250(t), "object": map[string]any{"kind": "planet", "uuid": p.UUID, "star_id": s.ID, "planet_id": p.ID}, "name": "Independent Planet Right"}
	f.req(first, "POST", "/api/discovery/name", body, 403)
	f.req(system, "POST", "/api/discovery/name", body, 200)
	if f.integer(`SELECT discoverer_id FROM aj_discovery_objects WHERE kind='planet' AND star_id=$1 AND planet_id=$2`, s.ID, p.ID) != uid1 {
		t.Fatal("historical authorship changed")
	}
	body["operation"] = uuid250(t)
	f.req(system, "POST", "/api/discovery/name", body, 409)
	body["operation"] = uuid250(t)
	body["object"] = map[string]any{"kind": "system", "uuid": s.UUID, "star_id": s.ID, "planet_id": 0}
	body["name"] = "Independent System Right"
	f.req(system, "POST", "/api/discovery/name", body, 200)
	for _, kind := range []string{"asteroid", "ice", "station"} {
		body["operation"] = uuid250(t)
		body["object"] = map[string]any{"kind": kind, "star_id": s.ID, "planet_id": 101}
		f.req(system, "POST", "/api/discovery/name", body, 400)
	}
	// A planet UUID cannot be substituted for a system UUID or another planet.
	body["operation"] = uuid250(t)
	body["object"] = map[string]any{"kind": "planet", "uuid": s.UUID, "star_id": s.ID, "planet_id": p.ID}
	f.req(system, "POST", "/api/discovery/name", body, 404)
	body["operation"] = uuid250(t)
	body["object"] = map[string]any{"kind": "planet", "uuid": p.UUID, "star_id": s.ID + 1, "planet_id": p.ID}
	f.req(system, "POST", "/api/discovery/name", body, 400)
	var response map[string]any
	if e = json.Unmarshal(f.req(system, "GET", fmt.Sprintf("/api/system?id=%d", s.ID), nil, 200).Body.Bytes(), &response); e != nil {
		t.Fatal(e)
	}
}

func TestStationNameMigrationDeferredFKAndLegacyCollisions280(t *testing.T) {
	f := newFixture033(t)
	// Model a 0.27 installation: deferred system FK exists, unique name index does
	// not. One port is inactive, and two built stations share an old custom name.
	f.exec(`DROP INDEX stations_global_name_unique;
 UPDATE stations SET name='Legacy '||star||' '||object_id||' · PORT 1',name_origin='legacy',active=false;
 INSERT INTO stations(star,object_id,name,specialization,name_origin) VALUES
 (1,'project-a','Custom Harbor','trade','legacy'),
 (1,'project-b','Custom Harbor','trade','legacy'),
 (1,'project-c','Custom Harbor 2','trade','legacy')`)
	var root string
	if e := f.db.QueryRow(f.ctx, `SELECT current_schema()`).Scan(&root); e != nil {
		t.Fatal(e)
	}
	again, e := openDatabase(f.ctx, f.db.Config().ConnString(), root)
	if e != nil {
		t.Fatal(e)
	}
	again.Close()
	var first, second, third string
	f.db.QueryRow(f.ctx, `SELECT name FROM stations WHERE object_id='project-a'`).Scan(&first)
	f.db.QueryRow(f.ctx, `SELECT name FROM stations WHERE object_id='project-b'`).Scan(&second)
	f.db.QueryRow(f.ctx, `SELECT name FROM stations WHERE object_id='project-c'`).Scan(&third)
	if first != "Custom Harbor" || second != "Custom Harbor 3" || third != "Custom Harbor 2" {
		t.Fatal("non-deterministic collision handling", first, second, third)
	}
	if f.integer(`SELECT count(*) FROM stations WHERE name_origin='legacy' OR name LIKE 'Legacy %'`) != 0 {
		t.Fatal("inactive generated stations not migrated")
	}
	if f.integer(`SELECT count(*) FROM admin_log WHERE action='station_name_collision'`) != 1 {
		t.Fatal("collision audit missing")
	}
}
