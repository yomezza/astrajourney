package telemetry

import (
	"context"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

// ObserveDB uses at most one independent connection, so a saturated application
// pool cannot hide its own lock waits. No SQL text, client address or values from
// other queries leave pg_stat_activity. Stop before closing the application pool.
func ObserveDB(parent context.Context, db *pgxpool.Pool) func() {
	if !diagnostics.Load() {
		return func() {}
	}
	ctx, cancel := context.WithCancel(parent)
	cfg := db.Config()
	cfg.MaxConns, cfg.MinConns = 1, 0
	cfg.ConnConfig.Tracer = nil
	cfg.ConnConfig.RuntimeParams["application_name"] = "astra-diagnostics"
	pool, err := pgxpool.NewWithConfig(ctx, cfg)
	if err != nil {
		Event(ctx, "db observer unavailable", err)
		cancel()
		return func() {}
	}
	done := make(chan struct{})
	go func() {
		defer close(done)
		ticker := time.NewTicker(time.Second)
		defer ticker.Stop()
		count := 0
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				count++
				if count%5 == 0 {
					s := db.Stat()
					Event(ctx, "db pool", nil, "acquired", s.AcquiredConns(), "total", s.TotalConns(), "max", s.MaxConns(), "empty_acquires", s.EmptyAcquireCount(), "canceled_acquires", s.CanceledAcquireCount())
				}
				check, finish := context.WithTimeout(ctx, 300*time.Millisecond)
				rows, e := pool.Query(check, `SELECT pid,COALESCE(wait_event,''),pg_blocking_pids(pid),GREATEST(0,EXTRACT(epoch FROM clock_timestamp()-query_start)*1000)::bigint FROM pg_stat_activity WHERE datname=current_database() AND wait_event_type='Lock' AND pid<>pg_backend_pid() LIMIT 32`)
				if e == nil {
					for rows.Next() {
						var pid int32
						var wait string
						var blockers []int32
						var ms int64
						if e = rows.Scan(&pid, &wait, &blockers, &ms); e != nil {
							break
						}
						Event(check, "db lock wait", nil, "backend_pid", pid, "wait_event", wait, "blocker_pids", blockers, "duration_ms", ms)
					}
					if e == nil {
						e = rows.Err()
					}
					rows.Close()
				}
				finish()
				if e != nil && ctx.Err() == nil {
					Event(ctx, "db observer unavailable", e)
				}
			}
		}
	}()
	return func() { cancel(); <-done; pool.Close() }
}

func Operation(ctx context.Context, phase string) (context.Context, func(error)) {
	ctx = Phase(ctx, phase)
	start := time.Now()
	return ctx, func(err error) { Event(ctx, "operation ended", err, "duration_ms", time.Since(start).Milliseconds()) }
}
