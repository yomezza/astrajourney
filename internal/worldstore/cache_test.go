package worldstore

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/yomezza/cosmosimulator/internal/platform/postgres"
	"github.com/yomezza/cosmosimulator/internal/universe"
)

// A transactional reader must use its own connection while a normal cache
// reader is already waiting for a saturated pool. Do not expose cache locks
// through the production API merely to synchronize an integration fixture.
func TestColdCacheSaturatedPool(t *testing.T) {
	dsn := os.Getenv("TEST_DATABASE_URL")
	if dsn == "" {
		t.Skip("TEST_DATABASE_URL must name a disposable PostgreSQL database")
	}
	config, _, err := postgres.Config(dsn, "public")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasSuffix(config.ConnConfig.Database, "_test") {
		t.Fatal("disposable test database required")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	admin, err := pgxpool.NewWithConfig(ctx, config)
	if err != nil {
		t.Fatal(err)
	}
	defer admin.Close()
	schema := fmt.Sprintf("cache_%d", time.Now().UnixNano())
	quoted := pgx.Identifier{schema}.Sanitize()
	if _, err = admin.Exec(ctx, "CREATE SCHEMA "+quoted); err != nil {
		t.Fatal(err)
	}
	defer admin.Exec(context.Background(), "DROP SCHEMA "+quoted+" CASCADE")
	if _, err = admin.Exec(ctx, "CREATE TABLE "+quoted+".galaxies(name text PRIMARY KEY,data jsonb NOT NULL)"); err != nil {
		t.Fatal(err)
	}
	raw, _ := json.Marshal(universe.Generate(1, 99))
	if _, err = admin.Exec(ctx, "INSERT INTO "+quoted+".galaxies VALUES('Edem',$1)", raw); err != nil {
		t.Fatal(err)
	}
	config, _, err = postgres.Config(dsn, schema)
	if err != nil {
		t.Fatal(err)
	}
	config.MaxConns = 2
	db, err := pgxpool.NewWithConfig(ctx, config)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	ctx, cancel = context.WithTimeout(ctx, 2*time.Second)
	defer cancel()
	tx, err := db.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback(context.Background())
	held, err := db.Acquire(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer held.Release()
	store := &Service{DB: db}
	entered := make(chan struct{}, 1)
	done := make(chan error, 1)
	go func() { _, e := store.LoadFrom(ctx, cacheSignalQuery{db: db, entered: entered}); done <- e }()
	select {
	case <-entered:
	case <-ctx.Done():
		t.Fatal(ctx.Err())
	}
	g, err := store.LoadFrom(ctx, tx)
	if err != nil || len(g.Stars) != 1 {
		t.Fatalf("transactional world read: %v", err)
	}
	if err = store.Commit(ctx, tx); err != nil {
		t.Fatal("commit while cache reader waits for pool", err)
	}
	held.Release()
	if err = <-done; err != nil {
		t.Fatal(err)
	}
}

type cacheSignalQuery struct {
	db      postgres.Querier
	entered chan struct{}
}

func (q cacheSignalQuery) QueryRow(ctx context.Context, sql string, args ...any) pgx.Row {
	select {
	case q.entered <- struct{}{}:
	default:
	}
	return q.db.QueryRow(ctx, sql, args...)
}
