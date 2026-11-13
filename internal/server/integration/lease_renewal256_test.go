package integration

import (
	"context"
	planetruntime "github.com/yomezza/cosmosimulator/internal/planet/runtime"
	"io"
	"log/slog"
	"testing"
	"time"
)

func TestPlanetRenewalKeepsFenceAfterBlockedSQL(t *testing.T) {
	f := newFixture033(t)
	coordinator, err := planetruntime.NewCoordinator(f.db, 15*time.Second, slog.New(slog.NewTextHandler(io.Discard, nil)))
	if err != nil {
		t.Fatal(err)
	}
	coordinator.Step(f.ctx)
	before, reason := coordinator.Current()
	if reason != "" {
		t.Fatal(reason)
	}
	tx, err := f.db.Begin(f.ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback(context.Background())
	if _, err = tx.Exec(f.ctx, `SELECT id FROM planet_runtime_leases WHERE id=1 FOR UPDATE`); err != nil {
		t.Fatal(err)
	}
	blocked, cancel := context.WithTimeout(f.ctx, 50*time.Millisecond)
	coordinator.Step(blocked)
	cancel()
	if err = tx.Rollback(f.ctx); err != nil {
		t.Fatal(err)
	}
	coordinator.Step(f.ctx)
	after, reason := coordinator.Current()
	if reason != "" || after.Fence != before.Fence {
		t.Fatalf("temporary SQL contention replaced a live runtime fence: before=%d after=%d reason=%s", before.Fence, after.Fence, reason)
	}
	if coordinator.Ready(f.ctx) != "" {
		t.Fatal("runtime did not recover")
	}
}
