package visits

import (
	"context"
	"errors"

	"github.com/yomezza/cosmosimulator/internal/planet/access"
	"github.com/yomezza/cosmosimulator/internal/planet/scene"
	"github.com/yomezza/cosmosimulator/internal/planet/scenestore"
)

// DiagnoseSceneFailure accepts only internal typed scene errors. A browser
// cannot choose evidence or a reason; transient DB/network errors do not qualify.
func (a *Service) DiagnoseSceneFailure(ctx context.Context, id access.Identity, w Welcome, f scenestore.Fence, key scene.Key, cause error) error {
	reason := ""
	if errors.Is(cause, scene.ErrCorrupt) {
		reason = "scene_corrupt"
	} else if errors.Is(cause, scene.ErrLayout) || errors.Is(cause, scene.ErrSource) {
		reason = "scene_source_changed"
	}
	if reason == "" {
		return nil
	}
	tx, err := a.locked(ctx, id.User, f)
	if err != nil {
		return err
	}
	defer tx.Rollback(context.Background())
	if _, err = access.Session(ctx, tx, id.Session); err != nil {
		return err
	}
	_, err = tx.Exec(ctx, `INSERT INTO aj_planet_recovery_diagnostics(user_id,visit_id,generation,connection_epoch,runtime_fence,reason,expires_at)
 SELECT v.user_id,v.visit_id,v.generation,v.connection_epoch,$4,$5,clock_timestamp()+interval '30 seconds' FROM planet_visits v JOIN planet_transfer_controls c ON c.user_id=v.user_id
 WHERE v.user_id=$1 AND v.visit_id=$2 AND ($3::bigint=0 OR v.connection_epoch=$3) AND v.state IN('active','disconnected') AND v.generation=c.generation AND c.revoke_requested_at IS NULL AND v.current_surface=$6 AND v.current_sector=$7 AND v.revision=$8
 ON CONFLICT(user_id,visit_id) DO UPDATE SET generation=EXCLUDED.generation,connection_epoch=EXCLUDED.connection_epoch,runtime_fence=EXCLUDED.runtime_fence,reason=EXCLUDED.reason,diagnosed_at=clock_timestamp(),expires_at=EXCLUDED.expires_at`, id.User, w.Visit, w.Epoch, f.Number, reason, key.Surface, key.Sector, key.Revision)
	if err == nil {
		err = tx.Commit(ctx)
	}
	return err
}
func (a *Service) ClearSceneDiagnosis(ctx context.Context, id access.Identity, w Welcome) error {
	_, err := a.DB.Exec(ctx, `DELETE FROM aj_planet_recovery_diagnostics WHERE user_id=$1 AND visit_id=$2 AND connection_epoch=$3`, id.User, w.Visit, w.Epoch)
	return err
}
