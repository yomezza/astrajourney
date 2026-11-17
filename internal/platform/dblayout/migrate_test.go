package dblayout

import (
	"context"
	"fmt"
	"os"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

func fixture(t *testing.T) (context.Context, *pgxpool.Pool, string) {
	t.Helper()
	dsn := os.Getenv("TEST_DATABASE_URL")
	if dsn == "" {
		t.Skip("disposable TEST_DATABASE_URL required")
	}
	ctx := context.Background()
	cfg, e := pgxpool.ParseConfig(dsn)
	if e != nil {
		t.Fatal(e)
	}
	if !strings.HasSuffix(cfg.ConnConfig.Database, "_test") {
		t.Fatal("disposable database required")
	}
	root := fmt.Sprintf("layout_%d", time.Now().UnixNano())
	cfg.ConnConfig.RuntimeParams["search_path"] = SearchPath(root)
	pool, e := pgxpool.NewWithConfig(ctx, cfg)
	if e != nil {
		t.Fatal(e)
	}
	t.Cleanup(func() { pool.Exec(ctx, "DROP SCHEMA IF EXISTS "+DropList(root)+" CASCADE"); pool.Close() })
	if _, e = pool.Exec(ctx, "CREATE SCHEMA "+pgx.Identifier{root}.Sanitize()); e != nil {
		t.Fatal(e)
	}
	return ctx, pool, root
}

func TestMigrationPreservesOIDsDataPrivilegesAndPartitions(t *testing.T) {
	ctx, db, root := fixture(t)
	exec := func(q string, args ...any) {
		t.Helper()
		if _, e := db.Exec(ctx, q, args...); e != nil {
			t.Fatal(e)
		}
	}
	role := root + "_reader"
	quoted := pgx.Identifier{role}.Sanitize()
	exec("CREATE ROLE " + quoted + " NOLOGIN")
	t.Cleanup(func() { db.Exec(ctx, "DROP OWNED BY "+quoted); db.Exec(ctx, "DROP ROLE "+quoted) })
	exec(`CREATE TABLE users(id bigserial PRIMARY KEY,username text); INSERT INTO users(username) VALUES('preserved');
CREATE TABLE ships(id bigint PRIMARY KEY,user_id bigint REFERENCES users(id));INSERT INTO ships VALUES(7,1);
CREATE TABLE galaxies(name text PRIMARY KEY,data jsonb);INSERT INTO galaxies VALUES('Edem','{"retained":true}');
CREATE TABLE star_systems(id bigint,galaxy_id text) PARTITION BY LIST(galaxy_id);
CREATE TABLE star_systems_p_fixture PARTITION OF star_systems FOR VALUES IN('edem');INSERT INTO star_systems VALUES(1,'edem');
CREATE FUNCTION event_notification() RETURNS int LANGUAGE sql AS 'SELECT 1';
CREATE COLLATION chat_unicode (provider=icu,locale='und',deterministic=false);`)
	exec("GRANT USAGE ON SCHEMA " + pgx.Identifier{root}.Sanitize() + " TO " + quoted)
	exec("GRANT SELECT ON users TO " + quoted)
	names := []string{"users", "ships", "galaxies", "star_systems", "star_systems_p_fixture"}
	oids := map[string]uint32{}
	for _, name := range names {
		var oid uint32
		if e := db.QueryRow(ctx, `SELECT to_regclass($1)::oid`, name).Scan(&oid); e != nil {
			t.Fatal(e)
		}
		oids[name] = oid
	}
	before, e := Inspect(ctx, db, root)
	if e != nil || !before.Ready || before.LayoutVersion != 0 {
		t.Fatal(before, e)
	}
	for pass := 0; pass < 2; pass++ {
		tx, e := db.Begin(ctx)
		if e != nil {
			t.Fatal(e)
		}
		if e = Prepare(ctx, tx, root); e == nil {
			e = Finish(ctx, tx, root)
		}
		if e != nil {
			tx.Rollback(ctx)
			t.Fatal(e)
		}
		if e = tx.Commit(ctx); e != nil {
			t.Fatal(e)
		}
	}
	for _, name := range names {
		var oid uint32
		if e := db.QueryRow(ctx, `SELECT to_regclass($1)::oid`, Qualified(root, name)).Scan(&oid); e != nil || oid != oids[name] {
			t.Fatal("relation replaced", name, oid, oids[name], e)
		}
	}
	var parent uint32
	if e = db.QueryRow(ctx, `SELECT inhparent FROM pg_inherits WHERE inhrelid=$1`, oids["star_systems_p_fixture"]).Scan(&parent); e != nil || parent != oids["star_systems"] {
		t.Fatal("partition attachment", e)
	}
	if _, e = db.Exec(ctx, `INSERT INTO ships VALUES(8,999)`); e == nil {
		t.Fatal("FK lost")
	}
	var id int64
	if e = db.QueryRow(ctx, `INSERT INTO users(username) VALUES('sequence') RETURNING id`).Scan(&id); e != nil || id != 2 {
		t.Fatal("sequence lost", id, e)
	}
	var allowed, create bool
	if e = db.QueryRow(ctx, `SELECT has_schema_privilege($1,$2,'USAGE'),has_schema_privilege($1,$2,'CREATE')`, role, Schema(root, "identity")).Scan(&allowed, &create); e != nil || !allowed || create {
		t.Fatal("namespace privileges", allowed, create, e)
	}
	exec("SET ROLE " + quoted)
	var name string
	e = db.QueryRow(ctx, `SELECT username FROM users WHERE id=1`).Scan(&name)
	exec("RESET ROLE")
	if e != nil || name != "preserved" {
		t.Fatal("table grant lost", name, e)
	}
	report, e := Inspect(ctx, db, root)
	if e != nil || !report.Ready || report.LayoutVersion != 2 {
		t.Fatal(report, e)
	}
	report2, e := Inspect(ctx, db, root)
	if e != nil || !reflect.DeepEqual(report, report2) {
		t.Fatal("read-only check changed catalog", e)
	}
}

func TestOccupiedTargetRollsBack(t *testing.T) {
	ctx, db, root := fixture(t)
	if _, e := db.Exec(ctx, "CREATE TABLE users(id int);CREATE SCHEMA "+pgx.Identifier{Schema(root, "planet")}.Sanitize()+";CREATE TABLE "+Qualified(root, "planet_visits")+"(id int)"); e != nil {
		t.Fatal(e)
	}
	report, e := Inspect(ctx, db, root)
	if e == nil || report.Ready {
		t.Fatal("preflight accepted conflict")
	}
	tx, e := db.Begin(ctx)
	if e != nil {
		t.Fatal(e)
	}
	if e = Prepare(ctx, tx, root); e == nil {
		t.Fatal("adopted foreign namespace")
	}
	tx.Rollback(ctx)
	var source, marker bool
	if e = db.QueryRow(ctx, `SELECT to_regclass($1) IS NOT NULL,to_regclass($2) IS NOT NULL`, pgx.Identifier{root, "users"}.Sanitize(), pgx.Identifier{root, "_astra_schema_layout"}.Sanitize()).Scan(&source, &marker); e != nil || !source || marker {
		t.Fatal("source changed after rollback", e)
	}
}

func TestFailedUpgradeRollsBackMovedObjectsAndSchemas(t *testing.T) {
	ctx, db, root := fixture(t)
	if _, e := db.Exec(ctx, `CREATE TABLE users(id bigserial PRIMARY KEY);INSERT INTO users DEFAULT VALUES;CREATE TABLE galaxies(name text PRIMARY KEY,data jsonb)`); e != nil {
		t.Fatal(e)
	}
	tx, e := db.Begin(ctx)
	if e != nil {
		t.Fatal(e)
	}
	if e = Prepare(ctx, tx, root); e != nil {
		t.Fatal(e)
	}
	if _, e = tx.Exec(ctx, `SELECT 1/0`); e == nil {
		t.Fatal("fault injection failed")
	}
	tx.Rollback(ctx)
	var count int
	var moved, marker bool
	if e = db.QueryRow(ctx, "SELECT count(*) FROM "+pgx.Identifier{root, "users"}.Sanitize()).Scan(&count); e != nil || count != 1 {
		t.Fatal("source lost", e)
	}
	if e = db.QueryRow(ctx, `SELECT to_regnamespace($1) IS NOT NULL,to_regclass($2) IS NOT NULL`, Schema(root, "identity"), pgx.Identifier{root, "_astra_schema_layout"}.Sanitize()).Scan(&moved, &marker); e != nil || moved || marker {
		t.Fatal("partial migration committed", e)
	}
}
