package integration

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/yomezza/cosmosimulator/internal/platform/telemetry"
)

func TestPlanetDiagnosticsDistinguishesPoolWaitAndSQLLock(t *testing.T) {
	f := newFixture033(t)
	dir := t.TempDir()
	stopLog, err := telemetry.Start("planet", dir, true)
	if err != nil {
		t.Fatal(err)
	}
	defer stopLog()
	cfg := f.db.Config()
	cfg.MaxConns, cfg.MinConns = 1, 0
	pool, err := pgxpool.NewWithConfig(f.ctx, cfg)
	if err != nil {
		t.Fatal(err)
	}
	defer pool.Close()
	stopObserver := telemetry.ObserveDB(f.ctx, pool)
	defer stopObserver()
	held, err := pool.Acquire(f.ctx)
	if err != nil {
		t.Fatal(err)
	}
	released := make(chan struct{})
	go func() { time.Sleep(150 * time.Millisecond); held.Release(); close(released) }()
	const requestID = "2ce847b1-4d8e-4a81-9589-2c4524ad72a2"
	const secret = "do-not-log-sql-or-args"
	r := httptest.NewRequest("GET", "/planet/ws/v1?request_id="+requestID, nil)
	handler := telemetry.TraceHTTP("planet", http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) {
		ctx := telemetry.WithVisit(r.Context(), 7, "diagnostic-visit", 89)
		// Distinct from server-side query time: the sole pool slot is held above.
		if _, e := pool.Exec(ctx, "SELECT 1 /* "+secret+" */"); e != nil {
			t.Error(e)
		}
		<-released
		tx, e := f.db.Begin(ctx)
		if e != nil {
			t.Fatal(e)
		}
		defer tx.Rollback(context.Background())
		if _, e = tx.Exec(ctx, `SELECT pg_advisory_xact_lock(909257)`); e != nil {
			t.Fatal(e)
		}
		query, cancel := context.WithTimeout(telemetry.Phase(ctx, "lock_probe"), 2200*time.Millisecond)
		defer cancel()
		if _, e = pool.Exec(query, `SELECT pg_advisory_xact_lock(909257)`); e == nil {
			t.Fatal("lock unexpectedly succeeded")
		}
	}))
	handler.ServeHTTP(httptest.NewRecorder(), r)
	stopObserver()
	stopLog()
	data, err := os.ReadFile(filepath.Join(dir, "planet.jsonl"))
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"db acquire", "wait_ms", "db lock wait", "blocker_pids", "backend_pid", "db query", requestID, "diagnostic-visit", `"connection_epoch":89`, `"phase":"lock_probe"`} {
		if !strings.Contains(string(data), want) {
			t.Errorf("missing diagnostic %s", want)
		}
	}
	if strings.Contains(string(data), secret) || strings.Contains(string(data), "SELECT pg_advisory") {
		t.Fatal("SQL text leaked")
	}
}
