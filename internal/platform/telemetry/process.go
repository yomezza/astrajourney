package telemetry

import (
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"runtime"
	"strings"
	"sync/atomic"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// SQLQueries counts client statements, including transactions and SendBatch.
// It never stores a query, its arguments, a DSN or a context value.
type SQLQueries struct {
	queries  atomic.Uint64
	failures atomic.Uint64
	batches  atomic.Uint64
}

type queryKey struct{}
type queryTiming struct {
	began time.Time
	id    string
	kind  string
}
type acquireKey struct{}
type batchKey struct{}

func (s *SQLQueries) TraceBatchStart(ctx context.Context, _ *pgx.Conn, data pgx.TraceBatchStartData) context.Context {
	s.queries.Add(uint64(data.Batch.Len()))
	s.batches.Add(1)
	if diagnostics.Load() {
		return context.WithValue(ctx, batchKey{}, time.Now())
	}
	return ctx
}

func (s *SQLQueries) TraceBatchQuery(ctx context.Context, conn *pgx.Conn, data pgx.TraceBatchQueryData) {
	if _, enabled := ctx.Value(batchKey{}).(time.Time); enabled && data.Err != nil && !errors.Is(data.Err, pgx.ErrNoRows) {
		Event(ctx, "db batch query", data.Err, "query_id", fmt.Sprintf("%x", sha256.Sum256([]byte(data.SQL)))[:16], "statement", queryKind(data.SQL), "backend_pid", conn.PgConn().PID())
	}
}

func (s *SQLQueries) TraceBatchEnd(ctx context.Context, conn *pgx.Conn, data pgx.TraceBatchEndData) {
	if data.Err != nil {
		s.failures.Add(1)
	}
	if began, ok := ctx.Value(batchKey{}).(time.Time); ok && (data.Err != nil || time.Since(began) >= 250*time.Millisecond) {
		Event(ctx, "db batch", data.Err, "backend_pid", conn.PgConn().PID(), "duration_ms", time.Since(began).Milliseconds())
	}
}

func (s *SQLQueries) TraceQueryStart(ctx context.Context, _ *pgx.Conn, data pgx.TraceQueryStartData) context.Context {
	s.queries.Add(1)
	if diagnostics.Load() {
		return context.WithValue(ctx, queryKey{}, queryTiming{time.Now(), fmt.Sprintf("%x", sha256.Sum256([]byte(data.SQL)))[:16], queryKind(data.SQL)})
	}
	return ctx
}
func (s *SQLQueries) TraceQueryEnd(ctx context.Context, conn *pgx.Conn, data pgx.TraceQueryEndData) {
	if data.Err != nil {
		s.failures.Add(1)
	}
	if timing, ok := ctx.Value(queryKey{}).(queryTiming); ok && (timing.kind == "begin" || timing.kind == "commit" || time.Since(timing.began) >= 250*time.Millisecond || (data.Err != nil && !errors.Is(data.Err, pgx.ErrNoRows))) {
		Event(ctx, "db query", data.Err, "query_id", timing.id, "statement", timing.kind, "backend_pid", conn.PgConn().PID(), "duration_ms", time.Since(timing.began).Milliseconds())
	}
}
func (s *SQLQueries) TraceAcquireStart(ctx context.Context, _ *pgxpool.Pool, _ pgxpool.TraceAcquireStartData) context.Context {
	if diagnostics.Load() {
		return context.WithValue(ctx, acquireKey{}, time.Now())
	}
	return ctx
}
func (s *SQLQueries) TraceAcquireEnd(ctx context.Context, pool *pgxpool.Pool, data pgxpool.TraceAcquireEndData) {
	if began, ok := ctx.Value(acquireKey{}).(time.Time); ok && (data.Err != nil || time.Since(began) >= 50*time.Millisecond) {
		stat := pool.Stat()
		Event(ctx, "db acquire", data.Err, "wait_ms", time.Since(began).Milliseconds(), "pool_acquired", stat.AcquiredConns(), "pool_max", stat.MaxConns(), "pool_total", stat.TotalConns())
	}
}

func Process(db *pgxpool.Pool) map[string]any {
	var m runtime.MemStats
	runtime.ReadMemStats(&m)
	s := db.Stat()
	v := map[string]any{
		"heap_alloc_bytes": m.HeapAlloc, "heap_inuse_bytes": m.HeapInuse, "go_sys_bytes": m.Sys, "goroutines": runtime.NumGoroutine(), "gc_cycles": m.NumGC,
		"pool": map[string]any{"acquired": s.AcquiredConns(), "total": s.TotalConns(), "max": s.MaxConns(), "acquires": s.AcquireCount(), "acquire_ms": float64(s.AcquireDuration()) / float64(time.Millisecond), "empty_acquires": s.EmptyAcquireCount(), "empty_wait_ms": float64(s.EmptyAcquireWaitTime()) / float64(time.Millisecond), "canceled_acquires": s.CanceledAcquireCount()},
	}
	if q, ok := db.Config().ConnConfig.Tracer.(*SQLQueries); ok {
		v["sql_calls"] = q.queries.Load()
		v["sql_errors"] = q.failures.Load()
		v["sql_batches"] = q.batches.Load()
	}
	return v
}

func queryKind(sql string) string {
	query := strings.ToLower(strings.TrimSpace(sql))
	switch {
	case query == "begin" || strings.HasPrefix(query, "begin "):
		return "begin"
	case query == "commit":
		return "commit"
	case strings.Contains(query, "pg_advisory_xact_lock_shared(784218)"):
		return "world_shared_lock"
	case strings.Contains(query, "pg_advisory_xact_lock(784218)"):
		return "world_exclusive_lock"
	case strings.Contains(query, "data->>'revision'"):
		return "world_revision"
	case strings.HasPrefix(query, "update planet_characters set"):
		return "checkpoint_update"
	default:
		return "other"
	}
}
