package planetserver

import (
	"context"
	"errors"
	"github.com/jackc/pgx/v5"
	"github.com/yomezza/cosmosimulator/internal/planet/access"
	"github.com/yomezza/cosmosimulator/internal/planet/building"
	"github.com/yomezza/cosmosimulator/internal/planet/movement"
	"github.com/yomezza/cosmosimulator/internal/planet/pathing"
	"github.com/yomezza/cosmosimulator/internal/planet/scene"
	"github.com/yomezza/cosmosimulator/internal/planet/scenestore"
	"github.com/yomezza/cosmosimulator/internal/planet/visits"
	"time"
)

func (a *App) editBuildings(run context.Context, id access.Identity, w visits.Welcome, f scenestore.Fence, m *movement.Member, c building.Command) error {
	ctx, cancel := context.WithTimeout(run, a.config.DBTimeout)
	defer cancel()
	if err := takeGate(ctx, a.buildGate); err != nil {
		return err
	}
	defer func() { <-a.buildGate }()
	people, finish := a.motion.EditSector(m.Scene().Key())
	var applied *building.Snapshot
	release := true
	defer func() {
		if release {
			finish(applied)
		}
	}()
	pos := m.Snapshot()
	var result building.Snapshot
	err := a.visits.Edit(ctx, id, w, f, m.Scene().Key(), func(tx pgx.Tx) error {
		var e error
		result, e = building.Apply(ctx, tx, id.User, m.Scene(), c, people, pathing.FromQ(pos.X, pos.Y))
		return e
	})
	if err == nil {
		applied = &result
	} else if errors.Is(err, visits.ErrEditCommit) {
		// COMMIT can have an ambiguous outcome. Re-read authority before resuming
		// motion so a committed obstacle cannot be absent from the collision mask.
		reload, done := context.WithTimeout(run, a.config.DBTimeout)
		defer done()
		if s, e := building.Load(reload, a.db, m.Scene().Key()); e == nil {
			applied = &s
		} else {
			release = false
			go func() {
				tick := time.NewTicker(time.Second)
				defer tick.Stop()
				for {
					select {
					case <-a.runContext.Done():
						finish(nil)
						return
					case <-tick.C:
						check, done := context.WithTimeout(a.runContext, a.config.DBTimeout)
						state, e := building.Load(check, a.db, m.Scene().Key())
						done()
						if e == nil {
							finish(&state)
							return
						}
					}
				}
			}()
		}
	}
	return err
}

// Couple the authoritative read with registration. Otherwise the last actor
// could leave after a concurrent build commits, and a new actor could install
// the pre-commit snapshot it read before that edit.
func (a *App) joinBuildings(ctx context.Context, stamp movement.Stamp, s *scene.Scene, x, y int) (*movement.Member, error) {
	if err := takeGate(ctx, a.buildGate); err != nil {
		return nil, err
	}
	defer func() { <-a.buildGate }()
	state, err := building.Load(ctx, a.db, s.Key())
	if err != nil {
		return nil, err
	}
	return a.motion.Join(stamp, s, x, y, state)
}
