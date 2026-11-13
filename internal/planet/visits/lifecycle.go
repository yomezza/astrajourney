package visits

import (
	"context"
	"errors"
	"fmt"
	"github.com/jackc/pgx/v5"
	"github.com/yomezza/cosmosimulator/internal/planet/access"
	"github.com/yomezza/cosmosimulator/internal/planet/scene"
	"github.com/yomezza/cosmosimulator/internal/planet/scenestore"
	"github.com/yomezza/cosmosimulator/internal/platform/gamesignal"
	"strconv"
)

func fmtInt(v int64) string { return strconv.FormatInt(v, 10) }
func (a *Service) Check(ctx context.Context, id access.Identity, w Welcome, f scenestore.Fence) error {
	tx, err := a.locked(ctx, id.User, f)
	if err != nil {
		return err
	}
	defer tx.Rollback(context.Background())
	if err = check(ctx, tx, id, w, f); err != nil {
		return err
	}
	if _, err = tx.Exec(ctx, `UPDATE planet_visits SET checked_at=clock_timestamp() WHERE user_id=$1 AND checked_at<clock_timestamp()-interval '500 milliseconds'`, id.User); err != nil {
		return err
	}
	return tx.Commit(ctx)
}
func check(ctx context.Context, tx pgx.Tx, id access.Identity, w Welcome, f scenestore.Fence) error {
	if _, err := access.Session(ctx, tx, id.Session); err != nil {
		return fmt.Errorf("visit session check: %w", err)
	}
	var site, ship int64
	var valid bool
	err := tx.QueryRow(ctx, `SELECT v.landing_site,v.ship,c.generation=v.generation AND c.reserved_ship=v.ship AND c.revoke_requested_at IS NULL FROM planet_visits v JOIN planet_transfer_controls c ON c.user_id=v.user_id WHERE v.user_id=$1 AND v.visit_id=$2 AND v.connection_epoch=$3 AND v.runtime_fence=$4 AND v.state='active'`, id.User, w.Visit, w.Epoch, f.Number).Scan(&site, &ship, &valid)
	if errors.Is(err, pgx.ErrNoRows) {
		return ErrReplaced
	}
	if err != nil {
		return fmt.Errorf("visit control check: %w", err)
	}
	if !valid {
		return ErrVisit
	}
	_, err = access.Landing(ctx, tx, id.User, site, ship)
	if err != nil {
		return fmt.Errorf("visit landing check: %w", err)
	}
	if err = sectorCheck(ctx, tx, id.User); err != nil {
		return fmt.Errorf("visit sector check: %w", err)
	}
	return nil
}
func (a *Service) Disconnect(ctx context.Context, id access.Identity, w Welcome, f scenestore.Fence) error {
	tx, err := a.locked(ctx, id.User, f)
	if err != nil {
		return err
	}
	defer tx.Rollback(context.Background())
	d, err := a.lifecycleSnapshot(ctx, tx, id.User)
	if err != nil {
		return err
	}
	tag, err := tx.Exec(ctx, `UPDATE planet_visits SET state='disconnected' WHERE user_id=$1 AND visit_id=$2 AND connection_epoch=$3 AND runtime_fence=$4 AND state='active'`, id.User, w.Visit, w.Epoch, f.Number)
	if err != nil {
		return err
	}
	if err = tx.Commit(ctx); err != nil {
		return err
	}
	if tag.RowsAffected() > 0 {
		a.lifecycleCommitted(gamesignal.VisitDisconnectedSubject, d)
	}
	return nil
}
func end(ctx context.Context, tx pgx.Tx, uid int64, reason string) error {
	_, err := tx.Exec(ctx, `UPDATE planet_visits SET state='ended',end_reason=$2,colony=NULL,ship=NULL,current_surface=NULL,current_sector=NULL,revision=NULL,landing_site=NULL,session_hash=NULL WHERE user_id=$1`, uid, reason)
	return err
}

// Sweep terminates revoked/unauthorized visits; it never changes cosmic controls.
func (a *Service) Sweep(ctx context.Context, f scenestore.Fence) error {
	rows, err := a.DB.Query(ctx, `SELECT user_id FROM planet_visits WHERE state<>'ended' ORDER BY checked_at,user_id LIMIT 100`)
	if err != nil {
		return err
	}
	var users []int64
	for rows.Next() {
		var uid int64
		if err = rows.Scan(&uid); err != nil {
			break
		}
		users = append(users, uid)
	}
	if err == nil {
		err = rows.Err()
	}
	rows.Close()
	if err != nil {
		return err
	}
	for _, uid := range users {
		if err = a.sweepOne(ctx, uid, f); err != nil {
			return err
		}
	}
	return nil
}
func (a *Service) sweepOne(ctx context.Context, uid int64, f scenestore.Fence) error {
	tx, err := a.locked(ctx, uid, f)
	if err != nil {
		return err
	}
	defer tx.Rollback(context.Background())
	var state, session string
	var site, ship *int64
	var valid, revoke bool
	var previousFence int64
	err = tx.QueryRow(ctx, `SELECT v.state,COALESCE(v.session_hash,''),v.landing_site,v.ship,COALESCE(c.generation=v.generation AND c.reserved_ship=v.ship AND c.revoke_requested_at IS NULL,false),COALESCE(c.revoke_requested_at IS NOT NULL,false),v.runtime_fence FROM planet_visits v LEFT JOIN planet_transfer_controls c ON c.user_id=v.user_id WHERE v.user_id=$1 AND v.state<>'ended'`, uid).Scan(&state, &session, &site, &ship, &valid, &revoke, &previousFence)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil
	}
	if err != nil {
		return err
	}
	d, err := a.lifecycleSnapshot(ctx, tx, uid)
	if err != nil {
		return err
	}
	subject := ""
	if state == "legacy" {
		if revoke {
			err = end(ctx, tx, uid, "legacy_return")
			subject = gamesignal.VisitEndedSubject
		}
	} else {
		invalid := !valid || site == nil || ship == nil
		sessionExpired := false
		if !invalid {
			_, e := access.Session(ctx, tx, session)
			if e != nil && !errors.Is(e, access.ErrAuth) {
				return e
			}
			sessionExpired = errors.Is(e, access.ErrAuth)
			_, e = access.Landing(ctx, tx, uid, *site, *ship)
			if e != nil && !errors.Is(e, access.ErrDenied) {
				return e
			}
			invalid = invalid || e != nil
			e = sectorCheck(ctx, tx, uid)
			if e != nil && !errors.Is(e, access.ErrDenied) && !errors.Is(e, ErrVisit) {
				return e
			}
			invalid = invalid || e != nil
		}
		if invalid {
			err = end(ctx, tx, uid, "control_or_access_revoked")
			subject = gamesignal.VisitEndedSubject
		} else if previousFence != f.Number || sessionExpired {
			if state != "disconnected" {
				subject = gamesignal.VisitDisconnectedSubject
			}
			_, err = tx.Exec(ctx, `UPDATE planet_visits SET state='disconnected' WHERE user_id=$1`, uid)
		}
	}
	if err != nil {
		return err
	}
	if _, err = tx.Exec(ctx, `UPDATE planet_visits SET checked_at=clock_timestamp() WHERE user_id=$1`, uid); err != nil {
		return err
	}
	if err = tx.Commit(ctx); err != nil {
		return err
	}
	if subject != "" {
		a.lifecycleCommitted(subject, d)
	}
	return nil
}

// Checkpoint saves only a server-owned position using the active epoch, control
// generation, runtime fence AND character version. No coordinates come from HTTP.
func (a *Service) Checkpoint(ctx context.Context, id access.Identity, w Welcome, f scenestore.Fence, s *scene.Scene, x, y int, finish bool) (int64, error) {
	if w.Generation < 1 || w.PositionVersion < 1 || x < 0 || y < 0 || x >= 256*1024 || y >= 256*1024 || !s.Walkable(x/1024, y/1024) {
		return 0, scene.ErrSource
	}
	tx, err := a.locked(ctx, id.User, f)
	if err != nil {
		return 0, err
	}
	defer tx.Rollback(context.Background())
	if err = check(ctx, tx, id, w, f); err != nil {
		return 0, err
	}
	var valid bool
	err = tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM planet_visits WHERE user_id=$1 AND visit_id=$2 AND generation=$3 AND current_surface=$4 AND current_sector=$5 AND revision=$6)`, id.User, w.Visit, w.Generation, s.Key().Surface, s.Key().Sector, s.Key().Revision).Scan(&valid)
	if err != nil {
		return 0, err
	}
	if !valid {
		return 0, ErrVisit
	}
	var version int64
	err = tx.QueryRow(ctx, `UPDATE planet_characters SET x_q=$2,y_q=$3,position_version=position_version+1 WHERE user_id=$1 AND position_version=$4 AND last_surface=$5 AND last_sector=$6 AND last_revision=$7 RETURNING position_version`, id.User, x, y, w.PositionVersion, s.Key().Surface, s.Key().Sector, s.Key().Revision).Scan(&version)
	if errors.Is(err, pgx.ErrNoRows) {
		return 0, ErrReplaced
	}
	if err != nil {
		return 0, err
	}
	var endedSignal gamesignal.VisitChange
	if finish {
		endedSignal, err = a.lifecycleSnapshot(ctx, tx, id.User)
		if err != nil {
			return 0, err
		}
	}
	if finish {
		if err = end(ctx, tx, id.User, "return"); err != nil {
			return 0, err
		}
	}
	if err = tx.Commit(ctx); err != nil {
		return 0, err
	}
	if finish {
		a.lifecycleCommitted(gamesignal.VisitEndedSubject, endedSignal)
	}
	return version, nil
}

func sectorCheck(ctx context.Context, tx pgx.Tx, uid int64) error {
	var k scene.Key
	if err := tx.QueryRow(ctx, `SELECT current_surface,current_sector,revision FROM planet_visits WHERE user_id=$1 AND state IN('active','disconnected')`, uid).Scan(&k.Surface, &k.Sector, &k.Revision); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return ErrVisit
		}
		return err
	}
	_, err := access.Sector(ctx, tx, uid, k)
	return err
}
