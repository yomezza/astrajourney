// Package runtime owns the planetary process lease. It never advances cosmic time.
package runtime

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"log/slog"
	"sync"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
)

const SchemaVersion = 8

type Lease struct {
	Owner string
	Fence int64
}

type Coordinator struct {
	db     *pgxpool.Pool
	ttl    time.Duration
	log    *slog.Logger
	mu     sync.RWMutex
	lease  Lease
	reason string
}

func NewCoordinator(db *pgxpool.Pool, ttl time.Duration, logger *slog.Logger) (*Coordinator, error) {
	if logger == nil {
		logger = slog.Default()
	}
	var token [16]byte
	if _, err := rand.Read(token[:]); err != nil {
		return nil, errors.New("cannot create planetary runtime identity")
	}
	return &Coordinator{db: db, ttl: ttl, log: logger, lease: Lease{Owner: hex.EncodeToString(token[:])}, reason: "starting"}, nil
}

func CheckSchema(ctx context.Context, db *pgxpool.Pool) string {
	var version int
	err := db.QueryRow(ctx, `SELECT version FROM planet_schema_state WHERE id=1`).Scan(&version)
	if err != nil {
		var pgerr *pgconn.PgError
		if errors.Is(err, pgx.ErrNoRows) || (errors.As(err, &pgerr) && pgerr.Code == "42P01") {
			return "schema_missing"
		}
		return "database_unavailable"
	}
	if version != SchemaVersion {
		return "schema_incompatible"
	}
	return ""
}

func (c *Coordinator) Current() (Lease, string) {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.lease, c.reason
}
func (c *Coordinator) set(fence int64, reason string) {
	c.mu.Lock()
	changed := c.reason != reason
	c.lease.Fence = fence
	c.reason = reason
	c.mu.Unlock()
	if changed {
		c.log.Info("planetary readiness changed", "ready", reason == "", "reason", reason)
	}
}

// Step is called by one lifecycle goroutine, never by requests or movement.
func (c *Coordinator) Step(ctx context.Context) {
	old, _ := c.Current()
	if reason := CheckSchema(ctx, c.db); reason != "" {
		c.set(0, reason)
		return
	}
	var fence int64
	var err error
	if old.Fence == 0 {
		err = c.db.QueryRow(ctx, `UPDATE planet_runtime_leases SET owner_id=$1,fence=fence+1,expires_at=clock_timestamp()+make_interval(secs=>$2) WHERE id=1 AND (owner_id IS NULL OR expires_at<=clock_timestamp() OR owner_id=$1) RETURNING fence`, old.Owner, c.ttl.Seconds()).Scan(&fence)
	} else {
		err = c.db.QueryRow(ctx, `UPDATE planet_runtime_leases SET expires_at=clock_timestamp()+make_interval(secs=>$3) WHERE id=1 AND owner_id=$1 AND fence=$2 AND expires_at>clock_timestamp() RETURNING fence`, old.Owner, old.Fence, c.ttl.Seconds()).Scan(&fence)
	}
	if err != nil {
		reason := "database_unavailable"
		if errors.Is(err, pgx.ErrNoRows) {
			if old.Fence == 0 {
				reason = "lease_in_use"
			} else {
				reason = "lease_lost"
			}
		}
		c.set(0, reason)
		return
	}
	c.set(fence, "")
}

// Ready always checks DB time and the current fence, not just an in-memory flag.
func (c *Coordinator) Ready(ctx context.Context) string {
	lease, reason := c.Current()
	if reason != "" {
		return reason
	}
	if reason = CheckSchema(ctx, c.db); reason != "" {
		return reason
	}
	var valid bool
	err := c.db.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM planet_runtime_leases WHERE id=1 AND owner_id=$1 AND fence=$2 AND expires_at>clock_timestamp())`, lease.Owner, lease.Fence).Scan(&valid)
	if err != nil {
		return "database_unavailable"
	}
	if !valid {
		return "lease_lost"
	}
	return ""
}

func (c *Coordinator) Release(ctx context.Context) error {
	lease, _ := c.Current()
	c.set(0, "stopping")
	// Owner identity prevents an expired predecessor from clearing a successor.
	_, err := c.db.Exec(ctx, `UPDATE planet_runtime_leases SET owner_id=NULL,expires_at=clock_timestamp() WHERE id=1 AND owner_id=$1`, lease.Owner)
	return err
}
