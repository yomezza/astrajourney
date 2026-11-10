package visits

import (
	"context"
	"errors"

	"github.com/jackc/pgx/v5"
	"github.com/yomezza/cosmosimulator/internal/platform/gamesignal"
)

// lifecycleSnapshot reads the LAST known location before End clears it. It is
// metadata for invalidation only, and never participates in scope decisions.
func (a *Service) lifecycleSnapshot(ctx context.Context, tx pgx.Tx, uid int64) (gamesignal.VisitChange, error) {
	var d gamesignal.VisitChange
	if a.Signals == nil {
		return d, nil
	}
	e := tx.QueryRow(ctx, `SELECT user_id::text,COALESCE(visit_id::text,''),generation::text,connection_epoch::text,runtime_fence::text,COALESCE(current_surface,0)::text,COALESCE(current_sector,0) FROM planet_visits WHERE user_id=$1`, uid).Scan(&d.UserID, &d.VisitID, &d.Generation, &d.Epoch, &d.RuntimeFence, &d.SurfaceID, &d.Sector)
	if errors.Is(e, pgx.ErrNoRows) {
		e = nil
	}
	return d, e
}
func (a *Service) lifecycleCommitted(subject string, d gamesignal.VisitChange) {
	if a.Signals != nil {
		if m, ok := gamesignal.EncodeVisit(subject, d); ok {
			a.Signals(m)
		}
	}
}
