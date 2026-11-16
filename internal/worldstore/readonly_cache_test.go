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

func readonlyCacheFixture(t *testing.T) (*Service, context.Context) {
	t.Helper()
	dsn := os.Getenv("TEST_DATABASE_URL")
	if dsn == "" {
		t.Skip("TEST_DATABASE_URL must name a disposable PostgreSQL database")
	}
	cfg, _, err := postgres.Config(dsn, "public")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasSuffix(cfg.ConnConfig.Database, "_test") {
		t.Fatal("disposable test DB required")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	t.Cleanup(cancel)
	admin, err := pgxpool.NewWithConfig(ctx, cfg)
	if err != nil {
		t.Fatal(err)
	}
	schema := fmt.Sprintf("readonly_%d", time.Now().UnixNano())
	quoted := pgx.Identifier{schema}.Sanitize()
	if _, err = admin.Exec(ctx, "CREATE SCHEMA "+quoted); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { admin.Exec(context.Background(), "DROP SCHEMA "+quoted+" CASCADE"); admin.Close() })
	if _, err = admin.Exec(ctx, "CREATE TABLE "+quoted+".galaxies(name text PRIMARY KEY,data jsonb NOT NULL)"); err != nil {
		t.Fatal(err)
	}
	raw, _ := json.Marshal(universe.Generate(1, 99))
	if _, err = admin.Exec(ctx, "INSERT INTO "+quoted+".galaxies VALUES('Edem',$1)", raw); err != nil {
		t.Fatal(err)
	}
	cfg, _, err = postgres.Config(dsn, schema)
	if err != nil {
		t.Fatal(err)
	}
	cfg.MaxConns = 1
	db, err := pgxpool.NewWithConfig(ctx, cfg)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(db.Close)
	return &Service{DB: db}, ctx
}

func TestReadOnlyWorldCacheTracksCommittedAndPrivateSnapshots(t *testing.T) {
	s, ctx := readonlyCacheFixture(t)
	cached, err := s.Load(ctx)
	if err != nil {
		t.Fatal(err)
	}
	tx, err := s.DB.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback(context.Background())
	shared, err := s.LoadReadOnly(ctx, tx)
	if err != nil || shared != cached {
		t.Fatalf("warm read-only transaction did not reuse snapshot: %v", err)
	}
	private, err := s.LoadFrom(ctx, tx)
	if err != nil {
		t.Fatal(err)
	}
	private.Revision = 999
	if cached.Revision == 999 {
		t.Fatal("writer modified shared world before commit")
	}
	if _, err = tx.Exec(ctx, `UPDATE galaxies SET data=jsonb_set(data,'{revision}','23') WHERE name='Edem'`); err != nil {
		t.Fatal(err)
	}
	uncommitted, err := s.LoadReadOnly(ctx, tx)
	if err != nil || uncommitted.Revision != 23 {
		t.Fatalf("transaction cannot read its own update: %v", err)
	}
	if s.universe != cached || cached.Revision == 23 {
		t.Fatal("uncommitted world leaked to shared cache")
	}
	if err = tx.Rollback(ctx); err != nil {
		t.Fatal(err)
	}
	// Use a separate connection and COMMIT outside the store's invalidation path.
	config := s.DB.Config()
	external, err := pgxpool.NewWithConfig(ctx, config)
	if err != nil {
		t.Fatal(err)
	}
	defer external.Close()
	if _, err = external.Exec(ctx, `UPDATE galaxies SET data=jsonb_set(data,'{revision}','24') WHERE name='Edem'`); err != nil {
		t.Fatal(err)
	}
	s.universeChecked = time.Time{}
	committed, err := s.Load(ctx)
	if err != nil || committed.Revision != 24 {
		t.Fatalf("ordinary reader did not refresh changed row: %v", err)
	}
	checked := s.universeChecked
	tx, err = s.DB.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.RepeatableRead})
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback(context.Background())
	current, err := s.LoadReadOnly(ctx, tx)
	if err != nil || current.Revision != 24 {
		t.Fatalf("stale cache ignored committed world: %v", err)
	}
	if !s.universeChecked.Equal(checked) {
		t.Fatal("transaction extended ordinary cache freshness")
	}
	if _, err = external.Exec(ctx, `UPDATE galaxies SET data=jsonb_set(data,'{revision}','25') WHERE name='Edem'`); err != nil {
		t.Fatal(err)
	}
	s.universeChecked = time.Time{}
	committed, err = s.LoadFrom(ctx, external)
	if err != nil || committed.Revision != 25 {
		t.Fatalf("ordinary reader did not see newer commit: %v", err)
	}
	stillCurrent, err := s.LoadReadOnly(ctx, tx)
	if err != nil || stillCurrent.Revision != 24 {
		t.Fatalf("repeatable read snapshot escaped transaction: %v", err)
	}
	if s.universe != committed {
		t.Fatal("old transaction replaced newer committed cache")
	}
}

func TestReadOnlyWorldColdCacheUsesOnlyHeldConnection(t *testing.T) {
	s, ctx := readonlyCacheFixture(t)
	tx, err := s.DB.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback(context.Background())
	bounded, done := context.WithTimeout(ctx, time.Second)
	defer done()
	g, err := s.LoadReadOnly(bounded, tx)
	if err != nil || len(g.Stars) != 1 {
		t.Fatalf("cold read-only world required another pool slot: %v", err)
	}
	if s.universe != nil {
		t.Fatal("transactional snapshot populated shared cache")
	}
}
