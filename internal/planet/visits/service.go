// Package visits is the planetary-only writer of characters and visit state.
package visits

import (
	"context"
	"errors"
	"fmt"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/yomezza/cosmosimulator/internal/planet/access"
	"github.com/yomezza/cosmosimulator/internal/planet/scene"
	"github.com/yomezza/cosmosimulator/internal/planet/scenestore"
	"github.com/yomezza/cosmosimulator/internal/platform/gamesignal"
	"time"
)

var ErrVisit = errors.New("visit_unavailable")
var ErrUsed = errors.New("ticket_used")
var ErrExpired = errors.New("ticket_expired")
var ErrReplaced = errors.New("session_replaced")

type Service struct {
	DB      *pgxpool.Pool
	Signals gamesignal.Emit
}
type Intent struct {
	Hash                   string
	Handoff, Visit         string
	Site, Ship, Generation int64
	Key                    scene.Key
}
type Welcome struct {
	LandingSector   int     `json:"landing_sector"`
	LandingSurface  string  `json:"landing_surface"`
	ReturnRadiusQ   int     `json:"return_radius_q"`
	Actor           string  `json:"actor"`
	Nickname        string  `json:"nickname"`
	Time            int64   `json:"server_time"`
	Generation      int64   `json:"-"`
	PositionVersion int64   `json:"-"`
	Speed           float64 `json:"speed"`
	Planet          string  `json:"planet"`
	Visit           string  `json:"visit"`
	Epoch           int64   `json:"connection_epoch"`
	Surface         string  `json:"surface"`
	Sector          int     `json:"sector"`
	X               int     `json:"x_q"`
	Y               int     `json:"y_q"`
}

func (a *Service) Resolve(ctx context.Context, id access.Identity, hash, resume string) (Intent, error) {
	var in Intent
	in.Hash = hash
	var session string
	var expired, used bool
	if hash != "" {
		err := a.DB.QueryRow(ctx, `SELECT h.id::text,h.landing_site,h.ship,h.generation,h.surface,l.sector,h.revision,h.session_hash,h.expires_at<=clock_timestamp() OR h.revoked_at IS NOT NULL,EXISTS(SELECT 1 FROM planet_handoff_uses u WHERE u.handoff_id=h.id) FROM planet_handoffs h JOIN planet_landing_sites l ON l.id=h.landing_site WHERE h.token_hash=$1 AND h.user_id=$2`, hash, id.User).Scan(&in.Handoff, &in.Site, &in.Ship, &in.Generation, &in.Key.Surface, &in.Key.Sector, &in.Key.Revision, &session, &expired, &used)
		if errors.Is(err, pgx.ErrNoRows) {
			return in, access.ErrDenied
		}
		if err != nil {
			return in, err
		}
		if session != id.Session {
			return in, access.ErrDenied
		}
		if used {
			return in, ErrUsed
		}
		if expired {
			return in, ErrExpired
		}
	} else {
		if !access.ValidUUID(resume) {
			return in, ErrVisit
		}
		err := a.DB.QueryRow(ctx, `SELECT visit_id::text,landing_site,ship,generation,current_surface,current_sector,revision FROM planet_visits WHERE user_id=$1 AND visit_id=$2 AND state IN('active','disconnected')`, id.User, resume).Scan(&in.Visit, &in.Site, &in.Ship, &in.Generation, &in.Key.Surface, &in.Key.Sector, &in.Key.Revision)
		if errors.Is(err, pgx.ErrNoRows) {
			return in, ErrVisit
		}
		if err != nil {
			return in, err
		}
	}
	if _, err := access.Landing(ctx, a.DB, id.User, in.Site, in.Ship); err != nil {
		return in, err
	}
	if _, err := access.Sector(ctx, a.DB, id.User, in.Key); err != nil {
		return in, err
	}
	return in, nil
}
func (a *Service) locked(ctx context.Context, uid int64, f scenestore.Fence) (pgx.Tx, error) {
	tx, err := a.DB.Begin(ctx)
	if err != nil {
		return nil, err
	}
	phase := "world_lock"
	if _, err = tx.Exec(ctx, `SELECT pg_advisory_xact_lock_shared(784218)`); err == nil {
		phase = "runtime_fence"
		var n int64
		err = tx.QueryRow(ctx, `SELECT fence FROM planet_runtime_leases WHERE id=1 AND owner_id=$1 AND fence=$2 AND expires_at>clock_timestamp() AND EXISTS(SELECT 1 FROM planet_schema_state WHERE id=1 AND version=8) FOR SHARE`, f.Owner, f.Number).Scan(&n)
		if errors.Is(err, pgx.ErrNoRows) {
			err = scenestore.ErrFence
		}
	}
	if err == nil {
		phase = "user_gate"
		_, err = tx.Exec(ctx, `SELECT pg_advisory_xact_lock(784219,$1)`, int32(uid))
	}
	if err != nil {
		tx.Rollback(ctx)
		return nil, fmt.Errorf("visit %s: %w", phase, err)
	}
	return tx, nil
}
func (a *Service) Activate(ctx context.Context, id access.Identity, in Intent, s *scene.Scene, f scenestore.Fence) (Welcome, error) {
	var w Welcome
	tx, err := a.locked(ctx, id.User, f)
	if err != nil {
		return w, err
	}
	defer tx.Rollback(context.Background())
	if id, err = access.Session(ctx, tx, id.Session); err != nil {
		return w, err
	}
	site, err := access.Landing(ctx, tx, id.User, in.Site, in.Ship)
	if err != nil {
		return w, err
	}
	if err = scenestore.CheckSource(ctx, tx, s); err != nil {
		return w, err
	}
	if site.Surface != in.Key.Surface || (in.Handoff != "" && site.Sector != in.Key.Sector) || site.Revision != in.Key.Revision || s.Key() != in.Key || (in.Handoff != "" && !s.Walkable(site.X/1024, site.Y/1024)) {
		return w, scene.ErrSource
	}
	if _, err = access.Sector(ctx, tx, id.User, in.Key); err != nil {
		return w, err
	}
	var valid bool
	err = tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM planet_transfer_controls WHERE user_id=$1 AND generation=$2 AND reserved_ship=$3 AND revoke_requested_at IS NULL)`, id.User, in.Generation, in.Ship).Scan(&valid)
	if err != nil {
		return w, err
	}
	if !valid {
		return w, ErrVisit
	}
	if in.Handoff != "" {
		var used, expired bool
		err = tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM planet_handoff_uses WHERE handoff_id=h.id),h.expires_at<=clock_timestamp() OR h.revoked_at IS NOT NULL OR h.session_hash<>$2 OR h.token_hash<>$3 FROM planet_handoffs h WHERE id=$1`, in.Handoff, id.Session, in.Hash).Scan(&used, &expired)
		if err != nil {
			return w, err
		}
		if used {
			return w, ErrUsed
		}
		if expired {
			return w, ErrExpired
		}
		err = tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM planet_visits WHERE user_id=$1 AND state<>'ended')`, id.User).Scan(&valid)
		if err != nil {
			return w, err
		}
		if valid {
			return w, ErrVisit
		}
		w.Visit, err = access.UUID()
		if err != nil {
			return w, err
		}
		w.X, w.Y = site.X, site.Y
		_, err = tx.Exec(ctx, `INSERT INTO planet_characters(user_id,last_surface,last_sector,last_revision,x_q,y_q) VALUES($1,$2,$3,$4,$5,$6) ON CONFLICT(user_id) DO UPDATE SET last_surface=EXCLUDED.last_surface,last_sector=EXCLUDED.last_sector,last_revision=EXCLUDED.last_revision,x_q=EXCLUDED.x_q,y_q=EXCLUDED.y_q,position_version=planet_characters.position_version+1`, id.User, in.Key.Surface, in.Key.Sector, in.Key.Revision, w.X, w.Y)
		if err == nil {
			err = tx.QueryRow(ctx, `INSERT INTO planet_visits(user_id,colony,ship,visit_id,state,generation,runtime_fence,connection_epoch,current_surface,current_sector,revision,landing_site,session_hash,checked_at) VALUES($1,$2,$3,$4,'active',$5,$6,1,$7,$8,$9,$10,$11,clock_timestamp()) ON CONFLICT(user_id) DO UPDATE SET colony=EXCLUDED.colony,ship=EXCLUDED.ship,visit_id=EXCLUDED.visit_id,state='active',generation=EXCLUDED.generation,runtime_fence=EXCLUDED.runtime_fence,connection_epoch=planet_visits.connection_epoch+1,current_surface=EXCLUDED.current_surface,current_sector=EXCLUDED.current_sector,revision=EXCLUDED.revision,landing_site=EXCLUDED.landing_site,session_hash=EXCLUDED.session_hash,end_reason='',entered_at=now(),checked_at=clock_timestamp() RETURNING connection_epoch`, id.User, site.Colony, in.Ship, w.Visit, in.Generation, f.Number, in.Key.Surface, in.Key.Sector, in.Key.Revision, in.Site, id.Session).Scan(&w.Epoch)
		}
		if err == nil {
			_, err = tx.Exec(ctx, `INSERT INTO planet_handoff_uses(handoff_id,visit_id) VALUES($1,$2)`, in.Handoff, w.Visit)
		}
	} else {
		w.Visit = in.Visit
		err = tx.QueryRow(ctx, `SELECT x_q,y_q FROM planet_characters WHERE user_id=$1 AND last_surface=$2 AND last_sector=$3 AND last_revision=$4`, id.User, in.Key.Surface, in.Key.Sector, in.Key.Revision).Scan(&w.X, &w.Y)
		if err != nil {
			return w, err
		}
		if !s.Walkable(w.X/1024, w.Y/1024) {
			return w, scene.ErrSource
		}
		err = tx.QueryRow(ctx, `UPDATE planet_visits SET connection_epoch=connection_epoch+1,runtime_fence=$3,state='active',session_hash=$4,checked_at=clock_timestamp() WHERE user_id=$1 AND visit_id=$2 AND generation=$5 AND state IN('active','disconnected') RETURNING connection_epoch`, id.User, in.Visit, f.Number, id.Session, in.Generation).Scan(&w.Epoch)
	}
	if err != nil {
		return w, err
	}
	w.Generation = in.Generation
	if err = tx.QueryRow(ctx, `SELECT position_version,public_id::text FROM planet_characters WHERE user_id=$1`, id.User).Scan(&w.PositionVersion, &w.Actor); err != nil {
		return w, err
	}
	if err = tx.Commit(ctx); err != nil {
		return w, err
	}
	subject := gamesignal.VisitStartedSubject
	if in.Handoff == "" {
		subject = gamesignal.VisitResumedSubject
	}
	a.lifecycleCommitted(subject, gamesignal.VisitChange{UserID: fmtInt(id.User), VisitID: w.Visit, Generation: fmtInt(w.Generation), Epoch: fmtInt(w.Epoch), RuntimeFence: fmtInt(f.Number), SurfaceID: fmtInt(in.Key.Surface), Sector: in.Key.Sector})
	w.Nickname = id.Nickname
	w.Time = time.Now().UnixMilli()
	w.Planet = site.Name
	w.LandingSector = site.Sector
	w.LandingSurface = fmtInt(site.Surface)
	w.ReturnRadiusQ = access.ReturnRadiusQ
	w.Surface = fmtInt(in.Key.Surface)
	w.Sector = in.Key.Sector
	return w, nil
}
