package dblayout

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/jackc/pgx/v5"
)

func Installed(ctx context.Context, q interface {
	QueryRow(context.Context, string, ...any) pgx.Row
}, root string) (bool, error) {
	var yes bool
	e := q.QueryRow(ctx, `SELECT to_regclass($1) IS NOT NULL`, pgx.Identifier{root, "_astra_schema_layout"}.Sanitize()).Scan(&yes)
	return yes, e
}

// Prepare moves existing relations by OID, preserving IDs, rows, constraints,
// owned sequences, indexes, privileges and partition attachments. No data copy.
func Prepare(ctx context.Context, tx pgx.Tx, root string) error {
	installed, e := Installed(ctx, tx, root)
	if e != nil {
		return e
	}
	if installed {
		var version int
		var owner string
		if e = tx.QueryRow(ctx, "SELECT version,root_name FROM "+pgx.Identifier{root, "_astra_schema_layout"}.Sanitize()+" WHERE id=1").Scan(&version, &owner); e != nil {
			return fmt.Errorf("schema layout metadata: %w", e)
		}
		if version != 1 || owner != root {
			return fmt.Errorf("schema layout metadata does not match this installation/version")
		}
	}
	if !installed {
		for _, d := range Domains {
			var occupied bool
			e = tx.QueryRow(ctx, occupiedSQL, Schema(root, d)).Scan(&occupied)
			if e != nil {
				return e
			}
			if occupied {
				return fmt.Errorf("schema layout: target %s is already occupied; refusing to adopt another installation", Schema(root, d))
			}
		}
	}
	for _, d := range Domains {
		if _, e = tx.Exec(ctx, "CREATE SCHEMA IF NOT EXISTS "+pgx.Identifier{Schema(root, d)}.Sanitize()); e != nil {
			return fmt.Errorf("create domain schema %s: %w", d, e)
		}
	}
	if _, e = tx.Exec(ctx, "CREATE TABLE IF NOT EXISTS "+pgx.Identifier{root, "_astra_schema_layout"}.Sanitize()+"(id int PRIMARY KEY CHECK(id=1),version int NOT NULL,root_name text NOT NULL,applied_at timestamptz NOT NULL DEFAULT clock_timestamp())"); e != nil {
		return e
	}
	if _, e = tx.Exec(ctx, "GRANT SELECT ON "+pgx.Identifier{root, "_astra_schema_layout"}.Sanitize()+" TO PUBLIC"); e != nil {
		return e
	}
	// Transfer namespace USAGE, but never grant CREATE to runtime roles.
	rows, e := tx.Query(ctx, `SELECT DISTINCT CASE WHEN a.grantee=0 THEN 'PUBLIC' ELSE r.rolname END FROM pg_namespace n CROSS JOIN LATERAL aclexplode(COALESCE(n.nspacl,acldefault('n',n.nspowner))) a LEFT JOIN pg_roles r ON r.oid=a.grantee WHERE n.nspname=$1 AND a.privilege_type='USAGE'`, root)
	if e != nil {
		return e
	}
	roles := []string{}
	for rows.Next() {
		var role string
		if e = rows.Scan(&role); e != nil {
			rows.Close()
			return e
		}
		roles = append(roles, role)
	}
	e = rows.Err()
	rows.Close()
	if e != nil {
		return e
	}
	for _, role := range roles {
		target := pgx.Identifier{role}.Sanitize()
		if role == "PUBLIC" {
			target = "PUBLIC"
		}
		if _, e = tx.Exec(ctx, "GRANT USAGE ON SCHEMA "+QuotedList(Names(root, true)[1:])+" TO "+target); e != nil {
			return e
		}
	}
	type relation struct {
		oid        uint32
		name, kind string
	}
	var objects []relation
	rows, e = tx.Query(ctx, `SELECT c.oid,c.relname,c.relkind::text FROM pg_class c JOIN pg_namespace n ON n.oid=c.relnamespace WHERE n.nspname=$1 AND c.relkind IN('r','p','v','m','S') ORDER BY CASE WHEN c.relkind IN('r','p') THEN 0 ELSE 1 END,c.relname`, root)
	if e != nil {
		return e
	}
	for rows.Next() {
		var r relation
		if e = rows.Scan(&r.oid, &r.name, &r.kind); e != nil {
			rows.Close()
			return e
		}
		objects = append(objects, r)
	}
	e = rows.Err()
	rows.Close()
	if e != nil {
		return e
	}
	for _, r := range objects {
		domain := Domain(r.name)
		if domain == "" {
			continue
		}
		if installed && r.name == "galaxies" && r.kind == "v" {
			continue
		}
		targetSchema := Schema(root, domain)
		var actual string
		if e = tx.QueryRow(ctx, `SELECT n.nspname FROM pg_class c JOIN pg_namespace n ON n.oid=c.relnamespace WHERE c.oid=$1`, r.oid).Scan(&actual); e != nil {
			return e
		}
		if actual == targetSchema {
			continue
		}
		var collision bool
		if e = tx.QueryRow(ctx, `SELECT to_regclass($1) IS NOT NULL`, Qualified(root, r.name)).Scan(&collision); e != nil {
			return e
		}
		if collision {
			return fmt.Errorf("schema layout: duplicate relation %s in source and target", r.name)
		}
		kind := "TABLE"
		switch r.kind {
		case "v":
			kind = "VIEW"
		case "m":
			kind = "MATERIALIZED VIEW"
		case "S":
			kind = "SEQUENCE"
		}
		if _, e = tx.Exec(ctx, "ALTER "+kind+" "+pgx.Identifier{actual, r.name}.Sanitize()+" SET SCHEMA "+pgx.Identifier{targetSchema}.Sanitize()); e != nil {
			return e
		}
	}
	// Functions are independent objects; moving their tables does not relocate them.
	rows, e = tx.Query(ctx, `SELECT p.proname,pg_get_function_identity_arguments(p.oid) FROM pg_proc p JOIN pg_namespace n ON n.oid=p.pronamespace WHERE n.nspname=$1 AND p.prokind='f'`, root)
	if e != nil {
		return e
	}
	type function struct{ name, args string }
	var functions []function
	for rows.Next() {
		var f function
		if e = rows.Scan(&f.name, &f.args); e != nil {
			rows.Close()
			return e
		}
		functions = append(functions, f)
	}
	e = rows.Err()
	rows.Close()
	if e != nil {
		return e
	}
	for _, f := range functions {
		if Domain(f.name) == "" {
			continue
		}
		if _, e = tx.Exec(ctx, "ALTER FUNCTION "+pgx.Identifier{root, f.name}.Sanitize()+"("+f.args+") SET SCHEMA "+pgx.Identifier{Schema(root, Domain(f.name))}.Sanitize()); e != nil {
			return e
		}
	}
	var collation bool
	if e = tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM pg_collation WHERE collnamespace=to_regnamespace($1) AND collname='chat_unicode')`, root).Scan(&collation); e != nil {
		return e
	}
	if collation {
		if _, e = tx.Exec(ctx, "ALTER COLLATION "+pgx.Identifier{root, "chat_unicode"}.Sanitize()+" SET SCHEMA "+pgx.Identifier{Schema(root, "chat")}.Sanitize()); e != nil {
			return e
		}
	}
	_, e = tx.Exec(ctx, `SELECT set_config('search_path',$1,true)`, MigrationPath(root))
	return e
}
func Finish(ctx context.Context, tx pgx.Tx, root string) error {
	// Retain a root handle for archive inspection. The physical archive table
	// keeps its existing trigger rejecting outdated JSON writers.
	if _, e := tx.Exec(ctx, "CREATE OR REPLACE VIEW "+pgx.Identifier{root, "galaxies"}.Sanitize()+" AS SELECT name,data FROM "+Qualified(root, "galaxies")); e != nil {
		return e
	}
	_, e := tx.Exec(ctx, "INSERT INTO "+pgx.Identifier{root, "_astra_schema_layout"}.Sanitize()+"(id,version,root_name) VALUES(1,1,$1) ON CONFLICT(id) DO UPDATE SET version=excluded.version,root_name=excluded.root_name", root)
	return e
}
func RelationSchema(ctx context.Context, q interface {
	QueryRow(context.Context, string, ...any) pgx.Row
}, name string) (string, error) {
	var schema string
	e := q.QueryRow(ctx, `SELECT n.nspname FROM pg_class c JOIN pg_namespace n ON n.oid=c.relnamespace WHERE c.oid=to_regclass($1)`, pgx.Identifier{name}.Sanitize()).Scan(&schema)
	return schema, e
}
func RootFromPath(path string) (string, error) { // Pool configurations always use our quoted root as the first path component.
	if !strings.HasPrefix(path, "\"") {
		return "", errors.New("unrecognized installation search_path")
	}
	var b strings.Builder
	for i := 1; i < len(path); i++ {
		if path[i] == '"' {
			if i+1 < len(path) && path[i+1] == '"' {
				b.WriteByte('"')
				i++
				continue
			}
			return b.String(), nil
		}
		b.WriteByte(path[i])
	}
	return "", errors.New("invalid installation search_path")
}
