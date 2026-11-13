package transfer

import (
	"context"
	"errors"
	"net/http"

	"github.com/jackc/pgx/v5"
	"github.com/yomezza/cosmosimulator/internal/planet/access"
	"github.com/yomezza/cosmosimulator/internal/platform/httpkit"
	"github.com/yomezza/cosmosimulator/internal/platform/idempotency"
)

var errProof = errors.New("return_authorization_required")

func consumeProof(ctx context.Context, tx pgx.Tx, uid int64, operation, visit string) error {
	var valid bool
	err := tx.QueryRow(ctx, `SELECT p.state='authorized' AND p.expires_at>clock_timestamp() AND p.visit_id::text=$3 AND p.visit_id=v.visit_id AND p.generation=v.generation AND p.generation=c.generation AND p.connection_epoch=v.connection_epoch AND p.runtime_fence=v.runtime_fence AND p.runtime_fence=l.fence AND l.expires_at>clock_timestamp() AND c.reserved_ship=v.ship AND c.revoke_requested_at IS NULL AND v.state IN('active','disconnected') AND p.surface_id=v.current_surface AND p.sector_number=v.current_sector AND p.landing_site_id=v.landing_site AND p.position_version=x.position_version AND p.position_x_q=x.x_q AND p.position_y_q=x.y_q AND p.distance_q<=$4 AND x.last_surface=v.current_surface AND x.last_sector=v.current_sector AND x.last_revision=v.revision FROM aj_planet_return_authorizations p JOIN planet_visits v ON v.user_id=p.user_id JOIN planet_transfer_controls c ON c.user_id=p.user_id JOIN planet_characters x ON x.user_id=p.user_id CROSS JOIN planet_runtime_leases l WHERE p.user_id=$1 AND p.operation=$2 AND l.id=1 FOR UPDATE OF p,v,c,x`, uid, operation, visit, access.ReturnRadiusQ).Scan(&valid)
	if errors.Is(err, pgx.ErrNoRows) || err == nil && !valid {
		return errProof
	}
	if err != nil {
		return err
	}
	_, err = tx.Exec(ctx, `UPDATE aj_planet_return_authorizations SET state='consumed',consumed_at=clock_timestamp() WHERE user_id=$1 AND operation=$2`, uid, operation)
	return err
}

func recoveryDiagnosis(ctx context.Context, tx pgx.Tx, uid int64) (string, error) {
	var reason string
	err := tx.QueryRow(ctx, `SELECT CASE WHEN v.state='legacy' THEN 'legacy_visit'
 WHEN c.reserved_ship IS NOT NULL AND c.revoke_requested_at IS NULL AND (v.state IN('active','disconnected') OR v.user_id IS NULL) AND (l.owner_id IS NULL OR l.expires_at<clock_timestamp()-interval '15 seconds') THEN 'runtime_lease_expired'
 WHEN v.state IN('active','disconnected') AND c.revoke_requested_at IS NULL AND EXISTS(SELECT 1 FROM aj_planet_recovery_diagnostics d WHERE d.user_id=v.user_id AND d.visit_id=v.visit_id AND d.generation=v.generation AND d.connection_epoch=v.connection_epoch AND d.runtime_fence=l.fence AND d.expires_at>clock_timestamp()) THEN 'verified_scene_failure'
 WHEN v.state IN('active','disconnected') AND c.revoke_requested_at IS NULL AND NOT EXISTS(SELECT 1 FROM planet_landing_sites s WHERE s.id=v.landing_site AND s.enabled) THEN 'landing_unavailable'
 ELSE '' END FROM users u LEFT JOIN planet_visits v ON v.user_id=u.id LEFT JOIN planet_transfer_controls c ON c.user_id=u.id CROSS JOIN planet_runtime_leases l WHERE u.id=$1 AND l.id=1`, uid).Scan(&reason)
	return reason, err
}

// Recover is a separate, audited server diagnosis. It never manufactures a
// normal-return proof and a healthy active visit cannot invoke it to teleport.
func (a *Service) Recover(w http.ResponseWriter, r *http.Request) {
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
	key := "planet-recovery:" + in.Operation
	digest := idempotency.Digest(in)
	var prior string
	err = tx.QueryRow(ctx, `SELECT digest FROM planet_requests WHERE user_id=$1 AND request=$2`, id.User, key).Scan(&prior)
	if err == nil && prior != digest {
		httpkit.Fail(w, 409, "operation_conflict")
		return
	}
	if err != nil && !errors.Is(err, pgx.ErrNoRows) {
		fail(w, err)
		return
	}
	if errors.Is(err, pgx.ErrNoRows) {
		current, e := view(ctx, tx, id.User)
		if e != nil {
			fail(w, e)
			return
		}
		if in.Visit != "" && in.Visit != current.Visit {
			httpkit.Fail(w, 409, "visit_changed")
			return
		}
		reason, e := recoveryDiagnosis(ctx, tx, id.User)
		if e != nil {
			fail(w, e)
			return
		}
		if reason == "" {
			httpkit.Fail(w, 409, "recovery_not_required")
			return
		}
		_, err = tx.Exec(ctx, `INSERT INTO planet_transfer_controls(user_id,generation,revoke_requested_at) VALUES($1,1,clock_timestamp()) ON CONFLICT(user_id) DO UPDATE SET generation=planet_transfer_controls.generation+1,revoke_requested_at=COALESCE(planet_transfer_controls.revoke_requested_at,clock_timestamp())`, id.User)
		if err == nil {
			_, err = tx.Exec(ctx, `UPDATE planet_handoffs SET revoked_at=clock_timestamp() WHERE user_id=$1 AND revoked_at IS NULL`, id.User)
		}
		if err == nil {
			_, err = tx.Exec(ctx, `INSERT INTO planet_requests(user_id,request,digest,result) VALUES($1,$2,$3,'{"ok":true}')`, id.User, key, digest)
		}
		if err == nil {
			_, err = tx.Exec(ctx, `INSERT INTO admin_log(actor,action,data) VALUES($1,'planet_emergency_recovery',jsonb_build_object('operation',$2::text,'visit',$3::text,'reason',$4::text))`, id.User, in.Operation, current.Visit, reason)
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
