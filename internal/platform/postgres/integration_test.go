package postgres

import (
	"context"
	"fmt"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/yomezza/cosmosimulator/internal/platform/dblayout"
)

func TestOpenDoesNotInitializeGame(t *testing.T) {
	dsn := os.Getenv("TEST_DATABASE_URL")
	if dsn == "" {
		t.Skip("TEST_DATABASE_URL must be a disposable PostgreSQL database")
	}
	config, _, err := Config(dsn, "public")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasSuffix(config.ConnConfig.Database, "_test") {
		t.Fatal("disposable database name must end in _test")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	schema := fmt.Sprintf("connect_only_%d", time.Now().UnixNano())
	db, normalized, err := Open(ctx, dsn, schema)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if normalized != schema {
		t.Fatal("schema changed")
	}
	var created bool
	if err = db.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM pg_catalog.pg_namespace WHERE nspname=$1)`, schema).Scan(&created); err != nil {
		t.Fatal(err)
	}
	if created {
		t.Fatal("connection helper created a schema or bootstrapped a game")
	}
	// Hold concurrent physical connections to verify pool-wide configuration.
	first, err := db.Acquire(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer first.Release()
	second, err := db.Acquire(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer second.Release()
	for _, c := range []*pgxpool.Conn{first, second} {
		var search string
		if err := c.QueryRow(ctx, `SHOW search_path`).Scan(&search); err != nil || search != dblayout.SearchPath(schema) {
			t.Fatalf("isolated search_path not applied: %q, %v", search, err)
		}
	}
}
