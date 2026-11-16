package integration

import (
	"context"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/yomezza/cosmosimulator/internal/navigation"
	"github.com/yomezza/cosmosimulator/internal/worldstore"
)

type worldFetchCounter struct{ full atomic.Int64 }

func (c *worldFetchCounter) TraceQueryStart(ctx context.Context, _ *pgx.Conn, d pgx.TraceQueryStartData) context.Context {
	if strings.HasPrefix(d.SQL, "SELECT data") && strings.Contains(d.SQL, "FROM galaxies") {
		c.full.Add(1)
	}
	return ctx
}
func (*worldFetchCounter) TraceQueryEnd(context.Context, *pgx.Conn, pgx.TraceQueryEndData) {}

func TestNavigationPollingDoesNotFetchWholeWorld(t *testing.T) {
	f := newFixture033(t)
	_, uid := f.signup("world_poll_stutter")
	config := f.db.Config()
	config.MaxConns = 1
	queries := &worldFetchCounter{}
	config.ConnConfig.Tracer = queries
	db, err := pgxpool.NewWithConfig(f.ctx, config)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	world := &worldstore.Service{DB: db}
	g, err := world.Load(f.ctx)
	if err != nil {
		t.Fatal(err)
	}
	queries.full.Store(0)
	nav := &navigation.Service{DB: db, World: world}
	start := time.Now()
	for range 5 {
		if err = nav.AdvancePlayer(f.ctx, uid, g); err != nil {
			t.Fatal(err)
		}
	}
	t.Logf("five polling advancements took %s, full world fetches=%d", time.Since(start), queries.full.Load())
	if queries.full.Load() != 0 {
		t.Fatalf("unchanged world was fetched %d times under the exclusive world lock", queries.full.Load())
	}
}
