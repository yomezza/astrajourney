// Package transfer is the cosmic writer of handoff tickets and ship reservations.
package transfer

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"net/http"
	"strconv"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/yomezza/cosmosimulator/internal/account"
	"github.com/yomezza/cosmosimulator/internal/fleet"
	"github.com/yomezza/cosmosimulator/internal/planet/access"
	"github.com/yomezza/cosmosimulator/internal/platform/httpkit"
	"github.com/yomezza/cosmosimulator/internal/platform/idempotency"
)

type Service struct {
	DB      *pgxpool.Pool
	Enabled bool
}
type View struct {
	PlanetName     string `json:"planet_name,omitempty"`
	ColonyName     string `json:"colony_name,omitempty"`
	Recoverable    bool   `json:"recoverable"`
	RecoveryReason string `json:"recovery_reason,omitempty"`
	Operation      string `json:"operation,omitempty"`
	Site           string `json:"landing_site,omitempty"`
	State          string `json:"state"`
	Visit          string `json:"visit,omitempty"`
	Ship           string `json:"ship,omitempty"`
	Endpoint       string `json:"endpoint"`
}

func view(ctx context.Context, q pgx.Tx, uid int64) (View, error) {
	v := View{State: "none", Endpoint: "/planet/ws/v1"}
	var ship *int64
	err := q.QueryRow(ctx, `SELECT c.reserved_ship,CASE WHEN c.revoke_requested_at IS NOT NULL THEN 'return_pending' WHEN c.reserved_ship IS NOT NULL THEN CASE WHEN v.generation=c.generation AND v.state IN('active','disconnected') THEN v.state ELSE 'pending' END WHEN v.state='legacy' THEN 'legacy' ELSE 'none' END,COALESCE(v.visit_id::text,'') FROM users u LEFT JOIN planet_transfer_controls c ON c.user_id=u.id LEFT JOIN planet_visits v ON v.user_id=u.id WHERE u.id=$1`, uid).Scan(&ship, &v.State, &v.Visit)
	// A missing control must not hide an old informational visit.
	if v.State == "none" {
		var legacy bool
		if err = q.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM planet_visits WHERE user_id=$1 AND state='legacy')`, uid).Scan(&legacy); err == nil && legacy {
			v.State = "legacy"
		}
	}
	if v.State == "pending" {
		err = q.QueryRow(ctx, `SELECT COALESCE(c.active_operation::text,''),COALESCE(h.landing_site::text,'') FROM planet_transfer_controls c LEFT JOIN planet_handoffs h ON h.user_id=c.user_id AND h.operation=c.active_operation WHERE c.user_id=$1`, uid).Scan(&v.Operation, &v.Site)
	}
	if ship != nil {
		v.Ship = strconv.FormatInt(*ship, 10)
	}
	if err == nil {
		v.RecoveryReason, err = recoveryDiagnosis(ctx, q, uid)
		if err == nil {
			err = q.QueryRow(ctx, `SELECT COALESCE(s.name,''),COALESCE(n.current_name,'') FROM users u LEFT JOIN planet_visits v ON v.user_id=u.id LEFT JOIN planet_surfaces s ON s.id=v.current_surface LEFT JOIN aj_colony_names n ON n.colony_id=v.colony WHERE u.id=$1`, uid).Scan(&v.PlanetName, &v.ColonyName)
		}
		v.Recoverable = v.RecoveryReason != ""
	}
	return v, err
}
func begin(ctx context.Context, db *pgxpool.Pool, uid int64) (pgx.Tx, error) {
	tx, err := db.Begin(ctx)
	if err != nil {
		return nil, err
	}
	if _, err = tx.Exec(ctx, `SELECT pg_advisory_xact_lock(784218)`); err == nil {
		_, err = tx.Exec(ctx, `SELECT id FROM users WHERE id=$1 FOR UPDATE`, uid)
	}
	if err != nil {
		tx.Rollback(ctx)
		return nil, err
	}
	return tx, nil
}
func (a *Service) State(w http.ResponseWriter, r *http.Request) {
	id, err := access.Authenticate(r.Context(), a.DB, r)
	if err != nil {
		fail(w, err)
		return
	}
	if err = a.reconcile(r.Context(), id.User); err != nil {
		fail(w, err)
		return
	}
	tx, err := a.DB.BeginTx(r.Context(), pgx.TxOptions{AccessMode: pgx.ReadOnly})
	if err != nil {
		fail(w, err)
		return
	}
	defer tx.Rollback(context.Background())
	v, err := view(r.Context(), tx, id.User)
	if err != nil {
		fail(w, err)
		return
	}
	if err = tx.Commit(r.Context()); err != nil {
		fail(w, err)
		return
	}
	httpkit.Reply(w, 200, v)
}
func (a *Service) Handoff(w http.ResponseWriter, r *http.Request) {
	if !a.Enabled {
		httpkit.Fail(w, 503, "planetary_disabled")
		return
	}
	var in struct {
		Operation string `json:"operation"`
		Site      string `json:"landing_site"`
		Ship      string `json:"ship"`
	}
	if !httpkit.Decode(w, r, &in) {
		return
	}
	site, e1 := strconv.ParseInt(in.Site, 10, 64)
	ship, e2 := strconv.ParseInt(in.Ship, 10, 64)
	if !access.ValidUUID(in.Operation) || e1 != nil || e2 != nil || site < 1 || ship < 1 {
		httpkit.Fail(w, 400, "invalid_handoff")
		return
	}
	ctx := r.Context()
	id, err := access.Authenticate(ctx, a.DB, r)
	if err != nil {
		fail(w, err)
		return
	}
	if err = a.reconcile(ctx, id.User); err != nil {
		fail(w, err)
		return
	}
	tx, err := begin(ctx, a.DB, id.User)
	if err != nil {
		fail(w, err)
		return
	}
	defer tx.Rollback(context.Background())
	if _, err = access.Session(ctx, tx, id.Session); err != nil {
		fail(w, err)
		return
	}
	var ready bool
	err = tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM planet_runtime_leases WHERE id=1 AND owner_id IS NOT NULL AND expires_at>clock_timestamp()) AND EXISTS(SELECT 1 FROM planet_schema_state WHERE id=1 AND version=8)`).Scan(&ready)
	if err != nil {
		fail(w, err)
		return
	}
	if !ready {
		httpkit.Fail(w, 503, "runtime_unavailable")
		return
	}
	if err = fleet.SettleShips(ctx, tx, id.User); err != nil {
		fail(w, err)
		return
	}
	if _, err = access.Landing(ctx, tx, id.User, site, ship); err != nil {
		fail(w, err)
		return
	}
	s, err := fleet.ReadShip(ctx, tx, ship)
	if err != nil {
		fail(w, err)
		return
	}
	// Don't call Idle here: retries already own a reservation. Check actual jobs.
	var busy bool
	err = tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM mining_jobs WHERE ship_id=$1) OR EXISTS(SELECT 1 FROM research_jobs WHERE ship_id=$1 AND NOT paused) OR EXISTS(SELECT 1 FROM deliveries WHERE ship_id=$1 AND status='carrying') OR EXISTS(SELECT 1 FROM automation_tasks WHERE ship_id=$1 AND status='running')`, s.ID).Scan(&busy)
	if err != nil {
		fail(w, err)
		return
	}
	if busy {
		httpkit.Fail(w, 409, "ship_busy")
		return
	}
	digest := idempotency.Digest(in)
	var oldDigest, oldID string
	var gen int64
	err = tx.QueryRow(ctx, `SELECT digest,id::text,generation FROM planet_handoffs WHERE user_id=$1 AND operation=$2`, id.User, in.Operation).Scan(&oldDigest, &oldID, &gen)
	if err != nil && !errors.Is(err, pgx.ErrNoRows) {
		fail(w, err)
		return
	}
	exists := err == nil
	if exists && oldDigest != digest {
		httpkit.Fail(w, 409, "operation_conflict")
		return
	}
	_, err = tx.Exec(ctx, `INSERT INTO planet_transfer_controls(user_id) VALUES($1) ON CONFLICT DO NOTHING`, id.User)
	if err != nil {
		fail(w, err)
		return
	}
	var held *int64
	var operation *string
	var current int64
	var revoking bool
	err = tx.QueryRow(ctx, `SELECT reserved_ship,active_operation::text,generation,revoke_requested_at IS NOT NULL FROM planet_transfer_controls WHERE user_id=$1 FOR UPDATE`, id.User).Scan(&held, &operation, &current, &revoking)
	if err != nil {
		fail(w, err)
		return
	}
	if exists {
		var used bool
		err = tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM planet_handoff_uses WHERE handoff_id=$1)`, oldID).Scan(&used)
		if err != nil {
			fail(w, err)
			return
		}
		if used {
			v, e := view(ctx, tx, id.User)
			if e != nil {
				fail(w, e)
				return
			}
			_ = tx.Rollback(ctx)
			httpkit.Reply(w, 200, v)
			return
		}
		if held == nil || operation == nil || *operation != in.Operation || gen != current || revoking {
			httpkit.Fail(w, 409, "handoff_cancelled")
			return
		}
	} else {
		var visiting bool
		err = tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM planet_visits WHERE user_id=$1 AND state<>'ended')`, id.User).Scan(&visiting)
		if err != nil {
			fail(w, err)
			return
		}
		if held != nil || revoking || visiting {
			httpkit.Fail(w, 409, "visit_already_exists")
			return
		}
		current++
		_, err = tx.Exec(ctx, `UPDATE planet_transfer_controls SET generation=$2,reserved_ship=$3,active_operation=$4,revoke_requested_at=NULL WHERE user_id=$1`, id.User, current, ship, in.Operation)
		if err != nil {
			fail(w, err)
			return
		}
	}
	var token [32]byte
	if _, err = rand.Read(token[:]); err != nil {
		fail(w, err)
		return
	}
	secret := hex.EncodeToString(token[:])
	hid := oldID
	if !exists {
		hid, err = access.UUID()
		if err != nil {
			fail(w, err)
			return
		}
	}
	landing, err := access.Landing(ctx, tx, id.User, site, ship)
	if err != nil {
		fail(w, err)
		return
	}
	var expiry time.Time
	err = tx.QueryRow(ctx, `INSERT INTO planet_handoffs(id,user_id,operation,digest,landing_site,surface,ship,session_hash,generation,revision,token_hash,expires_at) VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,clock_timestamp()+interval '60 seconds') ON CONFLICT(user_id,operation) DO UPDATE SET token_hash=EXCLUDED.token_hash,session_hash=EXCLUDED.session_hash,expires_at=EXCLUDED.expires_at,revoked_at=NULL RETURNING expires_at`, hid, id.User, in.Operation, digest, site, landing.Surface, ship, id.Session, current, landing.Revision, account.TokenHash(secret)).Scan(&expiry)
	if err == nil {
		err = tx.Commit(ctx)
	}
	if err != nil {
		fail(w, err)
		return
	}
	httpkit.Reply(w, 200, map[string]any{"state": "pending", "operation": in.Operation, "ticket": secret, "expires_at": expiry, "endpoint": "/planet/ws/v1", "protocol": 1})
}
func (a *Service) Return(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Operation string `json:"operation"`
		Visit     string `json:"visit"`
	}
	if !httpkit.Decode(w, r, &in) {
		return
	}
	if !access.ValidUUID(in.Operation) || (in.Visit != "" && !access.ValidUUID(in.Visit)) {
		httpkit.Fail(w, 400, "invalid_return")
		return
	}
	ctx := r.Context()
	id, err := access.Authenticate(ctx, a.DB, r)
	if err != nil {
		fail(w, err)
		return
	}
	tx, err := begin(ctx, a.DB, id.User)
	if err != nil {
		fail(w, err)
		return
	}
	defer tx.Rollback(context.Background())
	digest := idempotency.Digest(in)
	key := "planet-return:" + in.Operation
	var old string
	err = tx.QueryRow(ctx, `SELECT digest FROM planet_requests WHERE user_id=$1 AND request=$2`, id.User, key).Scan(&old)
	if err == nil && old != digest {
		httpkit.Fail(w, 409, "operation_conflict")
		return
	}
	if err != nil && !errors.Is(err, pgx.ErrNoRows) {
		fail(w, err)
		return
	}
	if errors.Is(err, pgx.ErrNoRows) {
		if err = consumeProof(ctx, tx, id.User, in.Operation, in.Visit); err != nil {
			fail(w, err)
			return
		}
		v, e := view(ctx, tx, id.User)
		if e != nil {
			fail(w, e)
			return
		}
		if in.Visit != "" && v.Visit != in.Visit {
			httpkit.Fail(w, 409, "visit_changed")
			return
		}
		_, err = tx.Exec(ctx, `INSERT INTO planet_transfer_controls(user_id,generation,revoke_requested_at) VALUES($1,1,clock_timestamp()) ON CONFLICT(user_id) DO UPDATE SET generation=planet_transfer_controls.generation+1,revoke_requested_at=clock_timestamp() WHERE planet_transfer_controls.revoke_requested_at IS NULL`, id.User)
		if err == nil {
			_, err = tx.Exec(ctx, `UPDATE planet_handoffs SET revoked_at=clock_timestamp() WHERE user_id=$1 AND revoked_at IS NULL`, id.User)
		}
		if err == nil {
			_, err = tx.Exec(ctx, `INSERT INTO planet_requests(user_id,request,digest,result) VALUES($1,$2,$3,'{"ok":true}')`, id.User, key, digest)
		}
	}
	if err == nil {
		err = tx.Commit(ctx)
	}
	if err != nil {
		fail(w, err)
		return
	}
	if err = a.reconcile(ctx, id.User); err != nil {
		fail(w, err)
		return
	}
	a.State(w, r)
}
func fail(w http.ResponseWriter, err error) {
	if errors.Is(err, errProof) {
		httpkit.Fail(w, 403, err.Error())
	} else if errors.Is(err, access.ErrAuth) {
		httpkit.Fail(w, 401, err.Error())
	} else if errors.Is(err, access.ErrDenied) {
		httpkit.Fail(w, 403, err.Error())
	} else {
		httpkit.Internal(w, err)
	}
}
