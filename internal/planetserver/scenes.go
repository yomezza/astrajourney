package planetserver

import (
	"context"
	"github.com/yomezza/cosmosimulator/internal/platform/telemetry"
	"time"

	"github.com/yomezza/cosmosimulator/internal/planet/movement"
	planetruntime "github.com/yomezza/cosmosimulator/internal/planet/runtime"
	"github.com/yomezza/cosmosimulator/internal/planet/scene"
	"github.com/yomezza/cosmosimulator/internal/planet/scenestore"
)

func (a *App) sceneFence() (scenestore.Fence, error) {
	l, reason := a.coordinator.Current()
	if reason != "" {
		return scenestore.Fence{}, scenestore.ErrFence
	}
	return scenestore.Fence{Owner: l.Owner, Number: l.Fence}, nil
}
func (a *App) newScenes(ctx context.Context) error {
	a.runContext = ctx
	a.motion = movement.New(ctx, a.config.MoveSpeed, a.config.PathBudget)
	r, err := planetruntime.NewRegistry(ctx, a.config.SceneCapacity, a.config.SceneIdleTTL, a.config.SceneLoadTimeout,
		func(ctx context.Context, key scene.Key) (*scene.Scene, error) {
			f, err := a.sceneFence()
			if err != nil {
				return nil, err
			}
			return a.store.Load(ctx, key, f)
		},
		func(ctx context.Context, s *scene.Scene) error {
			f, err := a.sceneFence()
			if err != nil {
				return err
			}
			return a.store.Validate(ctx, s, f)
		})
	if err == nil {
		a.scenes = r
	}
	return err
}

// No global pre-generation or per-empty-sector simulation loops. This worker
// evicts idle immutable snapshots and acknowledges cosmic retirement requests.
func (a *App) maintainScenes(ctx context.Context) {
	ticker := time.NewTicker(time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case now := <-ticker.C:
			a.scenes.Evict(now)
			f, err := a.sceneFence()
			if err != nil {
				continue
			}
			check, cancel := context.WithTimeout(ctx, a.config.DBTimeout)
			err = a.visits.Sweep(check, f)
			var ids []int64
			if err == nil {
				ids, err = a.store.Retired(check)
			}
			if err == nil {
				for _, id := range ids {
					if a.scenes.DrainSurface(id) {
						err = a.store.PurgeRetired(check, id, f)
						if err != nil {
							break
						}
					}
				}
			}
			cancel()
			if err != nil && ctx.Err() == nil {
				telemetry.Event(check, "planet maintenance deferred", err)
			}
		}
	}
}
