package planetserver

import (
	"context"
	"errors"
	"github.com/yomezza/cosmosimulator/internal/planet/access"
	"github.com/yomezza/cosmosimulator/internal/planet/movement"
	"github.com/yomezza/cosmosimulator/internal/planet/scenestore"
	"github.com/yomezza/cosmosimulator/internal/planet/visits"
	"github.com/yomezza/cosmosimulator/internal/platform/telemetry"
	"time"
)

func takeGate(ctx context.Context, gate chan struct{}) error {
	start := time.Now()
	defer func() {
		if time.Since(start) >= 50*time.Millisecond {
			telemetry.Event(telemetry.Phase(ctx, "memory_user_gate"), "planet gate wait", ctx.Err(), "duration_ms", time.Since(start).Milliseconds())
		}
	}()
	select {
	case gate <- struct{}{}:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}
func (a *App) saveMotion(ctx context.Context, id access.Identity, m *movement.Member, f scenestore.Fence, finish bool) error {
	stamp := m.Stamp()
	state := m.Snapshot()
	if !finish && !m.Dirty() {
		return nil
	}
	w := visits.Welcome{Visit: stamp.Visit, Epoch: stamp.Epoch, Generation: stamp.Generation, PositionVersion: stamp.Version}
	began := time.Now()
	version, err := a.visits.Checkpoint(ctx, id, w, f, m.Scene(), state.X, state.Y, finish)
	a.checkpoints.Observe(time.Since(began))
	if err != nil {
		a.checkpointErrors.Add(1)
	}
	if err == nil {
		m.Saved(version, state.X, state.Y)
	}
	return err
}

// This gate is shared by entry, periodic saves and disconnect. Replacement can
// freeze/flush the old controller before Activate reads the saved character.
func (a *App) flushPrevious(ctx context.Context, id access.Identity, f scenestore.Fence) error {
	m := a.motion.Current(id.User)
	if m == nil {
		return nil
	}
	m.Freeze()
	err := a.saveMotion(ctx, id, m, f, false)
	if errors.Is(err, visits.ErrReplaced) || errors.Is(err, visits.ErrVisit) {
		return nil
	}
	return err
}
func (a *App) disconnectMotion(parent context.Context, id access.Identity, w visits.Welcome, f scenestore.Fence, m *movement.Member, gate chan struct{}, ended *bool) {
	ctx, cancel := context.WithTimeout(context.WithoutCancel(parent), a.config.DBTimeout)
	defer cancel()
	if takeGate(ctx, gate) == nil {
		m.Freeze()
		if !*ended {
			saveErr := a.saveMotion(ctx, id, m, f, false)
			telemetry.Event(telemetry.WithVisit(ctx, id.User, w.Visit, w.Epoch), "planet disconnect save", saveErr)
			disconnectErr := a.visits.Disconnect(ctx, id, w, f)
			telemetry.Event(telemetry.WithVisit(ctx, id.User, w.Visit, w.Epoch), "planet visit disconnected", disconnectErr)
		}
		<-gate
	}
	m.Close()
}
