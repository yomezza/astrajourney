package chatserver

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// PostgresWakeSource owns one physical PostgreSQL session, outside the query
// pool after Hijack. Pool connection initialization (including restricted roles
// and search_path) is preserved. It never runs DDL or holds a SQL transaction.
type PostgresWakeSource struct {
	pool                                                   *pgxpool.Pool
	wakes                                                  chan struct{}
	mu                                                     sync.Mutex
	cancel                                                 context.CancelFunc
	done                                                   chan struct{}
	closed                                                 bool
	healthy                                                atomic.Bool
	reconcile                                              atomic.Bool
	attempts, registrations, disconnects, signals, ignored atomic.Uint64
	pid                                                    atomic.Uint32
}

func NewPostgresWakeSource(pool *pgxpool.Pool) *PostgresWakeSource {
	return &PostgresWakeSource{pool: pool, wakes: make(chan struct{}, 1)}
}
func (s *PostgresWakeSource) Wakeups() <-chan struct{} { return s.wakes }
func (s *PostgresWakeSource) Healthy() bool            { return s.healthy.Load() }

// A2 must not silently fall back to immediate local post-COMMIT wakeups.
func (s *PostgresWakeSource) AfterCommit()        {}
func (s *PostgresWakeSource) TakeReconcile() bool { return s.reconcile.Swap(false) }
func (s *PostgresWakeSource) wake() {
	select {
	case s.wakes <- struct{}{}:
	default:
	}
}
func (s *PostgresWakeSource) Start(ctx context.Context) error {
	s.mu.Lock()
	if s.closed || s.done != nil {
		s.mu.Unlock()
		return errors.New("PostgreSQL wake source already started or closed")
	}
	run, cancel := context.WithCancel(ctx)
	s.cancel = cancel
	s.done = make(chan struct{})
	first := make(chan error, 1)
	go s.run(run, first)
	s.mu.Unlock()
	// Registration completes (autocommit LISTEN) BEFORE the dispatcher's initial
	// snapshot. Failed registration still starts retries and degraded polling.
	select {
	case err := <-first:
		return err
	case <-ctx.Done():
		return ctx.Err()
	}
}
func (s *PostgresWakeSource) connect(ctx context.Context) (*pgx.Conn, string, error) {
	s.attempts.Add(1)
	check, cancel := context.WithTimeout(ctx, 3*time.Second)
	defer cancel()
	pooled, err := s.pool.Acquire(check)
	if err != nil {
		return nil, "", err
	}
	conn := pooled.Hijack()
	var channel string
	// Match the trigger's TG_TABLE_SCHEMA, and refuse a shadowed relation from
	// another schema. md5 is PostgreSQL's implementation, independent of Go.
	err = conn.QueryRow(check, `SELECT 'cosmos_chat_'||pg_catalog.md5(n.nspname) FROM pg_catalog.pg_class c JOIN pg_catalog.pg_namespace n ON n.oid=c.relnamespace WHERE c.oid='chat_outbox'::regclass AND n.nspname=current_schema() AND c.relkind='r'`).Scan(&channel)
	if err == nil {
		_, err = conn.Exec(check, `LISTEN `+pgx.Identifier{channel}.Sanitize())
	}
	if err != nil {
		closeListenConn(conn)
		return nil, "", err
	}
	return conn, channel, nil
}
func closeListenConn(conn *pgx.Conn) {
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	_ = conn.Close(ctx)
}
func (s *PostgresWakeSource) run(ctx context.Context, first chan<- error) {
	defer close(s.done)
	defer s.healthy.Store(false)
	defer s.pid.Store(0)
	backoff := 250 * time.Millisecond
	for {
		conn, channel, err := s.connect(ctx)
		if err == nil {
			s.pid.Store(conn.PgConn().PID())
			s.registrations.Add(1)
			s.healthy.Store(true)
			// A new session has no replay. Force current-state reconciliation,
			// independent of the previous outbox window or notification loss.
			s.reconcile.Store(true)
			s.wake()
		}
		if first != nil {
			first <- err
			first = nil
		}
		if err == nil {
			backoff = 250 * time.Millisecond
			for {
				n, waitErr := conn.WaitForNotification(ctx)
				if waitErr != nil {
					break
				}
				if n.Channel == channel && n.Payload == "" {
					s.signals.Add(1)
					s.wake()
				} else {
					s.ignored.Add(1)
				}
			}
			s.healthy.Store(false)
			s.pid.Store(0)
			s.disconnects.Add(1)
			closeListenConn(conn)
		}
		if ctx.Err() != nil {
			return
		}
		// Prompt degraded polling even when a healthy 10s timer was pending.
		s.wake()
		timer := time.NewTimer(backoff)
		select {
		case <-ctx.Done():
			timer.Stop()
			return
		case <-timer.C:
		}
		backoff = min(backoff*2, 5*time.Second)
	}
}
func (s *PostgresWakeSource) Close(ctx context.Context) error {
	s.mu.Lock()
	s.closed = true
	if s.cancel != nil {
		s.cancel()
	}
	done := s.done
	s.mu.Unlock()
	if done == nil {
		return nil
	}
	select {
	case <-done:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}
func (s *PostgresWakeSource) Diagnostics() map[string]any {
	return map[string]any{"healthy": s.Healthy(), "backend_pid": s.pid.Load(), "connect_attempts": s.attempts.Load(), "registrations": s.registrations.Load(), "disconnects": s.disconnects.Load(), "signals": s.signals.Load(), "ignored_signals": s.ignored.Load()}
}
