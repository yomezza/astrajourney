package integration

import (
	"context"
	"fmt"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/yomezza/cosmosimulator/internal/platform/dblayout"

	"github.com/yomezza/cosmosimulator/internal/server"
)

func TestDatabaseSchemaIsolation(t *testing.T) {
	dsn := os.Getenv("TEST_DATABASE_URL")
	if dsn == "" {
		t.Skip("TEST_DATABASE_URL must point to a disposable PostgreSQL database")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	admin, err := pgxpool.New(ctx, dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer admin.Close()
	if !strings.HasSuffix(admin.Config().ConnConfig.Database, "_test") {
		t.Fatal("test database name must end in _test")
	}
	suffix := time.Now().UnixNano()
	names := []string{fmt.Sprintf("cosmos_%d", suffix), fmt.Sprintf(`cosmos "%d; --`, suffix)}
	for _, name := range names {
		defer func(name string) {
			_, _ = admin.Exec(context.Background(), "DROP SCHEMA IF EXISTS "+dblayout.DropList(name)+" CASCADE")
		}(name)
	}
	// A deliberately incompatible table in another schema must never be used.
	if _, err = admin.Exec(ctx, `CREATE TABLE IF NOT EXISTS public.schema_isolation_sentinel (value TEXT)`); err != nil {
		t.Fatal(err)
	}
	defer admin.Exec(context.Background(), `DROP TABLE public.schema_isolation_sentinel`)
	first, err := openDatabase(ctx, dsn, names[0])
	if err != nil {
		t.Fatal(err)
	}
	defer first.Close()
	second, err := openDatabase(ctx, dsn, names[1])
	if err != nil {
		t.Fatal(err)
	}
	defer second.Close()
	for i, db := range []*pgxpool.Pool{first, second} {
		_, err = db.Exec(ctx, `INSERT INTO users(username,password_hash,credits) VALUES('same_player','test',$1)`, int64(100+i))
		if err != nil {
			t.Fatal(err)
		}
		// Hold several connections at once: search_path must apply across the pool.
		connections := []*pgxpool.Conn{}
		for n := 0; n < 3; n++ {
			c, err := db.Acquire(ctx)
			if err != nil {
				t.Fatal(err)
			}
			connections = append(connections, c)
			var schema string
			var credits int64
			err = c.QueryRow(ctx, `SELECT current_schema(),credits FROM users WHERE username='same_player'`).Scan(&schema, &credits)
			if err != nil || schema != names[i] || credits != int64(100+i) {
				t.Fatalf("wrong schema/credits: %q %d %v", schema, credits, err)
			}
		}
		for _, c := range connections {
			c.Release()
		}
	}
	if _, err = first.Exec(ctx, `UPDATE universe_state SET seed=123,storage_version=storage_version+1 WHERE id=1`); err != nil {
		t.Fatal(err)
	}
	// Existing schemas and their data survive repeated startup.
	again, err := openDatabase(ctx, dsn, names[0])
	if err != nil {
		t.Fatal(err)
	}
	defer again.Close()
	var seed int64
	if err = again.QueryRow(ctx, `SELECT seed FROM universe_state WHERE id=1`).Scan(&seed); err != nil || seed != 123 {
		t.Fatal("galaxy regenerated on restart", err)
	}
	var count int
	if err = again.QueryRow(ctx, `SELECT count(*) FROM users`).Scan(&count); err != nil || count != 4 {
		t.Fatalf("initialization is not repeatable: %d %v", count, err)
	}
	if _, err = first.Exec(ctx, `UPDATE world SET last_tick=now()-interval '11 seconds'`); err != nil {
		t.Fatal(err)
	}
	a := server.New(first, server.Config{})
	if err = a.Simulation.Tick(ctx); err != nil {
		t.Fatal(err)
	}
	var tick int64
	if err = second.QueryRow(ctx, `SELECT tick FROM world`).Scan(&tick); err != nil || tick != 0 {
		t.Fatalf("world leaked into another schema: %d %v", tick, err)
	}
	var isolated bool
	if err = first.QueryRow(ctx, `SELECT to_regclass('schema_isolation_sentinel') IS NULL`).Scan(&isolated); err != nil || !isolated {
		t.Fatalf("search_path unexpectedly falls back to public: %v", err)
	}
}
