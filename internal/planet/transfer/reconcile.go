package transfer

import (
	"context"
	"errors"
	"time"

	"github.com/yomezza/cosmosimulator/internal/planet/access"
)

// Reconcile owns reservation release, including recovery after runtime expiry.
func (a *Service) Reconcile(ctx context.Context) error { return a.reconcile(ctx, 0) }
func (a *Service) reconcile(ctx context.Context, onlyUser int64) error {
	tx, err := a.DB.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(context.Background())
	if _, err = tx.Exec(ctx, `SELECT pg_advisory_xact_lock(784218)`); err != nil {
		return err
	}
	rows, err := tx.Query(ctx, `SELECT c.user_id,c.generation,c.reserved_ship,c.revoke_requested_at IS NOT NULL,COALESCE(CASE WHEN v.state IN('active','disconnected') AND v.generation=c.generation THEN v.session_hash END,h.session_hash,''),COALESCE(h.landing_site,0),COALESCE(h.expires_at<=clock_timestamp(),true),COALESCE(v.state,''),COALESCE(v.generation,0),COALESCE(v.runtime_fence,0),COALESCE(l.fence,0),COALESCE(l.owner_id IS NOT NULL AND l.expires_at>clock_timestamp(),false),EXISTS(SELECT 1 FROM planet_handoff_uses hu WHERE hu.handoff_id=h.id) FROM planet_transfer_controls c LEFT JOIN planet_handoffs h ON h.user_id=c.user_id AND h.operation=c.active_operation LEFT JOIN planet_visits v ON v.user_id=c.user_id CROSS JOIN planet_runtime_leases l WHERE l.id=1 AND ($1::bigint=0 OR c.user_id=$1) AND (c.reserved_ship IS NOT NULL OR c.revoke_requested_at IS NOT NULL) ORDER BY c.checked_at,c.user_id LIMIT 100`, onlyUser)
	if err != nil {
		return err
	}
	type item struct {
		uid, gen, site, vg, vf, lf   int64
		ship                         *int64
		revoke, expired, alive, used bool
		session, state               string
	}
	var items []item
	for rows.Next() {
		var v item
		if err = rows.Scan(&v.uid, &v.gen, &v.ship, &v.revoke, &v.session, &v.site, &v.expired, &v.state, &v.vg, &v.vf, &v.lf, &v.alive, &v.used); err != nil {
			break
		}
		items = append(items, v)
	}
	if err == nil {
		err = rows.Err()
	}
	rows.Close()
	if err != nil {
		return err
	}
	for _, v := range items {
		if _, err = tx.Exec(ctx, `UPDATE planet_transfer_controls SET checked_at=clock_timestamp() WHERE user_id=$1`, v.uid); err != nil {
			return err
		}
		live := (v.state == "active" || v.state == "disconnected")
		invalid := false
		if v.ship != nil && !v.revoke {
			_, e := access.Session(ctx, tx, v.session)
			if e != nil && !errors.Is(e, access.ErrAuth) {
				return e
			}
			invalid = e != nil && !live
			_, e = access.Landing(ctx, tx, v.uid, v.site, *v.ship)
			if e != nil && !errors.Is(e, access.ErrDenied) {
				return e
			}
			invalid = invalid || e != nil
			if invalid {
				_, err = tx.Exec(ctx, `UPDATE planet_transfer_controls SET generation=generation+1,revoke_requested_at=clock_timestamp() WHERE user_id=$1`, v.uid)
				if err != nil {
					return err
				}
				v.revoke = true
			}
		}
		pendingExpired := !v.used && v.expired
		safe := !live || !v.alive || v.vf != v.lf
		if v.state == "legacy" {
			safe = !v.alive
		}
		if (v.revoke || pendingExpired || (v.state == "ended" && v.vg == v.gen)) && safe {
			_, err = tx.Exec(ctx, `UPDATE planet_transfer_controls SET generation=generation+1,reserved_ship=NULL,active_operation=NULL,revoke_requested_at=CASE WHEN $2 THEN revoke_requested_at ELSE NULL END WHERE user_id=$1`, v.uid, v.state == "legacy")
			if err != nil {
				return err
			}
		}
	}
	return tx.Commit(ctx)
}
func (a *Service) Run(ctx context.Context) {
	t := time.NewTicker(time.Second)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			c, cancel := context.WithTimeout(ctx, 2*time.Second)
			_ = a.Reconcile(c)
			cancel()
		}
	}
}
