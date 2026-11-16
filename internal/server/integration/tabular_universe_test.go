package integration

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"github.com/jackc/pgx/v5"
	"os"
	"os/exec"
	"reflect"
	"strings"
	"testing"

	"github.com/yomezza/cosmosimulator/internal/platform/identifiers"
	"github.com/yomezza/cosmosimulator/internal/universe"
	"github.com/yomezza/cosmosimulator/internal/universestore"
)

func withoutUUIDs(g *universe.Galaxy) {
	for i := range g.Stars {
		g.Stars[i].UUID = ""
		for j := range g.Stars[i].Planets {
			g.Stars[i].Planets[j].UUID = ""
		}
	}
}
func TestTabularUniverseMigrationPreservesWorldAndUUIDs(t *testing.T) {
	f, c, uid, surface, _ := setupPlanet120(t)
	before, _, e := universestore.Snapshot(f.ctx, f.db)
	if e != nil {
		t.Fatal(e)
	}
	withoutUUIDs(before)
	raw, _ := json.Marshal(before)
	var legacy map[string]any
	_ = json.Unmarshal(raw, &legacy)
	star := legacy["stars"].([]any)[0].(map[string]any)
	star["custom_spectral_note"] = "retained"
	star["planets"].([]any)[0].(map[string]any)["pressure_bar"] = 1.25
	legacy["custom_world_flag"] = true
	raw, _ = json.Marshal(legacy)
	f.stageLegacy(raw)
	var schema string
	if e = f.db.QueryRow(f.ctx, `SELECT current_schema()`).Scan(&schema); e != nil {
		t.Fatal(e)
	}
	var archived []byte
	if e = f.db.QueryRow(f.ctx, `SELECT data FROM galaxies WHERE name='Edem'`).Scan(&archived); e != nil {
		t.Fatal(e)
	}
	credits := f.integer(`SELECT credits FROM users WHERE id=$1`, uid)
	ships := f.integer(`SELECT count(*) FROM ships WHERE user_id=$1`, uid)
	db, e := openDatabase(f.ctx, f.db.Config().ConnString(), schema)
	if e != nil {
		t.Fatal(e)
	}
	db.Close()
	after, _, e := universestore.Snapshot(f.ctx, f.db)
	if e != nil {
		t.Fatal(e)
	}
	var note string
	var pressure float64
	var flag bool
	f.db.QueryRow(f.ctx, `SELECT details->>'custom_spectral_note' FROM star_systems WHERE id=1`).Scan(&note)
	f.db.QueryRow(f.ctx, `SELECT (details->>'pressure_bar')::float8 FROM system_planets WHERE system_id=1 AND id=1`).Scan(&pressure)
	f.db.QueryRow(f.ctx, `SELECT (details->>'custom_world_flag')::boolean FROM universe_state`).Scan(&flag)
	if note != "retained" || pressure != 1.25 || !flag {
		t.Fatal("legacy extension fields lost")
	}
	firstUUID := after.Stars[0].UUID
	planetUUID := after.Stars[0].Planets[0].UUID
	if !identifiers.ValidUUID(firstUUID) || !identifiers.ValidUUID(planetUUID) || firstUUID == planetUUID {
		t.Fatal("invalid identities")
	}
	copyRaw, _ := json.Marshal(after)
	var stripped universe.Galaxy
	_ = json.Unmarshal(copyRaw, &stripped)
	withoutUUIDs(&stripped)
	expectedRaw, _ := json.Marshal(before)
	actualRaw, _ := json.Marshal(stripped)
	if string(expectedRaw) != string(actualRaw) {
		t.Fatal("migration changed world content")
	}
	hash := sha256.Sum256(archived)
	var recorded string
	if e = f.db.QueryRow(f.ctx, `SELECT legacy_sha256 FROM universe_state`).Scan(&recorded); e != nil || recorded != hex.EncodeToString(hash[:]) {
		t.Fatal("archive checksum", e)
	}
	var retained []byte
	f.db.QueryRow(f.ctx, `SELECT data FROM galaxies WHERE name='Edem'`).Scan(&retained)
	if string(retained) != string(archived) {
		t.Fatal("legacy archive modified")
	}
	if f.integer(`SELECT credits FROM users WHERE id=$1`, uid) != credits || f.integer(`SELECT count(*) FROM ships WHERE user_id=$1`, uid) != ships || f.integer(`SELECT count(*) FROM planet_surfaces WHERE id=$1`, surface) != 1 {
		t.Fatal("player property changed")
	}
	if _, e = f.db.Exec(f.ctx, `UPDATE galaxies SET data='{}' WHERE name='Edem'`); e == nil {
		t.Fatal("old writer was not blocked")
	}
	if _, e = f.db.Exec(f.ctx, `UPDATE system_identity SET object_uuid=gen_random_uuid() WHERE id=1`); e == nil {
		t.Fatal("mutable system UUID")
	}
	if _, e = f.db.Exec(f.ctx, `UPDATE system_planets SET object_uuid=gen_random_uuid() WHERE system_id=1 AND id=$1`, after.Stars[0].Planets[0].ID); e == nil {
		t.Fatal("mutable planet UUID")
	}
	if f.integer(`SELECT count(*) FROM pg_inherits WHERE inhparent='star_systems'::regclass`) != int64(len(after.Catalog)) {
		t.Fatal("partition count")
	}
	db, e = openDatabase(f.ctx, f.db.Config().ConnString(), schema)
	if e != nil {
		t.Fatal(e)
	}
	db.Close()
	restarted, _, e := universestore.Snapshot(f.ctx, f.db)
	if e != nil || !reflect.DeepEqual(after, restarted) {
		t.Fatal("restart changed world/identities", e)
	}
	// The unchanged numeric locator still serves the same player's surface.
	f.req(c, "GET", "/api/state", nil, 200)
}
func TestUUIDRenameTouchesOnlyTargetRow(t *testing.T) {
	f := newFixture033(t)
	cookie, uid := f.signup("uuid_rename")
	f.exec(`UPDATE users SET role='admin' WHERE id=$1`, uid)
	var objectUUID, starVersion, planetVersion string
	f.db.QueryRow(f.ctx, `SELECT object_uuid::text FROM system_planets WHERE system_id=1 AND id=1`).Scan(&objectUUID)
	f.db.QueryRow(f.ctx, `SELECT xmin::text FROM star_systems WHERE id=1`).Scan(&starVersion)
	f.db.QueryRow(f.ctx, `SELECT xmin::text FROM system_planets WHERE system_id=1 AND id=2`).Scan(&planetVersion)
	in := map[string]any{"operation": uuid250(t), "object": map[string]any{"kind": "planet", "uuid": objectUUID}, "name": "Планета UUID проверка", "reason": "migration test"}
	first := f.req(cookie, "POST", "/api/admin/discovery/name", in, 200)
	again := f.req(cookie, "POST", "/api/admin/discovery/name", in, 200)
	if first.Body.String() != again.Body.String() {
		var a, b any
		json.Unmarshal(first.Body.Bytes(), &a)
		json.Unmarshal(again.Body.Bytes(), &b)
		if !reflect.DeepEqual(a, b) {
			t.Fatal("UUID replay changed")
		}
	}
	var actual, name string
	f.db.QueryRow(f.ctx, `SELECT name FROM system_planets WHERE object_uuid=$1`, objectUUID).Scan(&name)
	if name != "Планета UUID проверка" {
		t.Fatal("wrong rename")
	}
	f.db.QueryRow(f.ctx, `SELECT xmin::text FROM star_systems WHERE id=1`).Scan(&actual)
	if actual != starVersion {
		t.Fatal("planet rename rewrote system")
	}
	f.db.QueryRow(f.ctx, `SELECT xmin::text FROM system_planets WHERE system_id=1 AND id=2`).Scan(&actual)
	if actual != planetVersion {
		t.Fatal("renamed another planet")
	}
	in["operation"] = uuid250(t)
	in["object"] = map[string]any{"kind": "planet", "uuid": objectUUID, "star_id": 2}
	f.req(cookie, "POST", "/api/admin/discovery/name", in, 400)
	if f.integer(`SELECT count(*) FROM aj_discovery_name_history WHERE result->'object'->>'uuid'=$1`, objectUUID) != 1 {
		t.Fatal("UUID missing from history")
	}
}

func TestTabularReadOnlyPreflightAndFailedMigrationRollback(t *testing.T) {
	f := newFixture033(t)
	_, uid := f.signup("broken_universe_ref")
	report, e := universestore.Inspect(f.ctx, f.db)
	if e != nil || !report.Ready || report.Storage != "tabular" {
		t.Fatal("tabular preflight", report, e)
	}
	g, _, e := universestore.Snapshot(f.ctx, f.db)
	if e != nil {
		t.Fatal(e)
	}
	raw, _ := json.Marshal(g)
	f.stageLegacy(raw)
	f.exec(`UPDATE ships SET star=900000 WHERE user_id=$1`, uid)
	report, e = universestore.Inspect(f.ctx, f.db)
	if e == nil || report.Ready || report.InvalidReferences["ships.star"] != 1 {
		t.Fatal("preflight accepted orphan", report, e)
	}
	if f.integer(`SELECT count(*) FROM universe_state`) != 0 {
		t.Fatal("preflight wrote storage state")
	}
	var schema string
	f.db.QueryRow(f.ctx, `SELECT current_schema()`).Scan(&schema)
	db, e := openDatabase(f.ctx, f.db.Config().ConnString(), schema)
	if db != nil {
		db.Close()
	}
	if e == nil {
		t.Fatal("migration accepted orphan")
	}
	if f.integer(`SELECT count(*) FROM universe_state`) != 0 || f.integer(`SELECT count(*) FROM star_systems`) != 0 {
		t.Fatal("failed migration partially committed")
	}
	if f.integer(`SELECT star FROM ships WHERE user_id=$1`, uid) != 900000 {
		t.Fatal("migration silently repaired player data")
	}
	var archive []byte
	f.db.QueryRow(f.ctx, `SELECT data FROM galaxies WHERE name='Edem'`).Scan(&archive)
	var source any
	json.Unmarshal(raw, &source)
	var saved any
	json.Unmarshal(archive, &saved)
	if !reflect.DeepEqual(source, saved) {
		t.Fatal("failed migration changed legacy source")
	}
}

type interleavingReader struct {
	universestore.Reader
	once   bool
	mutate func()
}

func (q *interleavingReader) Query(ctx context.Context, sql string, args ...any) (pgx.Rows, error) {
	if !q.once && strings.Contains(sql, "FROM system_planets") {
		q.once = true
		q.mutate()
	}
	return q.Reader.Query(ctx, sql, args...)
}
func TestTabularSnapshotRetriesConcurrentWorldCommit(t *testing.T) {
	f := newFixture033(t)
	reader := &interleavingReader{Reader: f.db, mutate: func() {
		tx, e := f.db.Begin(f.ctx)
		if e != nil {
			t.Fatal(e)
		}
		defer tx.Rollback(f.ctx)
		if _, e = tx.Exec(f.ctx, `UPDATE star_systems SET name='Consistent star' WHERE id=1; UPDATE system_planets SET name='Consistent planet' WHERE system_id=1 AND id=1; UPDATE universe_state SET storage_version=storage_version+1,revision=revision+1`); e != nil {
			t.Fatal(e)
		}
		if e = tx.Commit(f.ctx); e != nil {
			t.Fatal(e)
		}
	}}
	g, _, e := universestore.Snapshot(f.ctx, reader)
	if e != nil {
		t.Fatal(e)
	}
	if g.Stars[0].Name != "Consistent star" || g.Stars[0].Planets[0].Name != "Consistent planet" {
		t.Fatal("mixed world snapshot returned")
	}
}

func TestObjectExtraDetailsSurviveSave(t *testing.T) {
	f := newFixture033(t)
	f.exec(`UPDATE system_planets SET details=details||'{"pressure_bar":1.25,"composition":{"oxygen":0.2}}'::jsonb WHERE system_id=1 AND id=1`)
	g, _, e := universestore.Snapshot(f.ctx, f.db)
	if e != nil {
		t.Fatal(e)
	}
	g.Stars[0].Planets[0].Mass += .1
	g.Revision++
	tx, e := f.db.Begin(f.ctx)
	if e != nil {
		t.Fatal(e)
	}
	defer tx.Rollback(f.ctx)
	if e = universestore.Save(f.ctx, tx, g); e != nil {
		t.Fatal(e)
	}
	if e = tx.Commit(f.ctx); e != nil {
		t.Fatal(e)
	}
	var pressure float64
	if e = f.db.QueryRow(f.ctx, `SELECT (details->>'pressure_bar')::float8 FROM system_planets WHERE system_id=1 AND id=1`).Scan(&pressure); e != nil || pressure != 1.25 {
		t.Fatal("extra JSON attributes lost", e)
	}
}
func TestUniverseCheckConsoleMakesNoChanges(t *testing.T) {
	f := newFixture033(t)
	var schema string
	f.db.QueryRow(f.ctx, `SELECT current_schema()`).Scan(&schema)
	version := f.integer(`SELECT storage_version FROM universe_state`)
	users := f.integer(`SELECT count(*) FROM users`)
	cmd := exec.Command("go", "run", "../../../cmd/astra-journey", "--check-universe")
	cmd.Env = append(os.Environ(), "CONFIG_FILE=", "DATABASE_URL="+f.db.Config().ConnString(), "DB_SCHEMA="+schema)
	out, e := cmd.Output()
	if e != nil {
		t.Fatal("read-only check command failed", e)
	}
	var report universestore.Report
	if e = json.Unmarshal(out, &report); e != nil || !report.Ready || report.Storage != "tabular" {
		t.Fatal("console report", e)
	}
	if f.integer(`SELECT storage_version FROM universe_state`) != version || f.integer(`SELECT count(*) FROM users`) != users {
		t.Fatal("check command wrote game data")
	}
}
