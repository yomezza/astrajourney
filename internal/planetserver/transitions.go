package planetserver

import (
	"context"
	"errors"
	"github.com/yomezza/cosmosimulator/internal/platform/gamesignal"
	"strconv"
	"time"

	"github.com/yomezza/cosmosimulator/internal/planet/access"
	"github.com/yomezza/cosmosimulator/internal/planet/movement"
	planetruntime "github.com/yomezza/cosmosimulator/internal/planet/runtime"
	"github.com/yomezza/cosmosimulator/internal/planet/scenestore"
	"github.com/yomezza/cosmosimulator/internal/planet/visits"
)

type loadResult struct {
	pin *planetruntime.Pin
	err error
}
type crossingOffer struct {
	ID       string    `json:"offer"`
	Neighbor int       `json:"neighbor"`
	Owner    string    `json:"owner"`
	Expires  time.Time `json:"expires_at"`
	Ready    bool      `json:"ready"`
	cancel   context.CancelFunc
	result   chan loadResult
	pin      *planetruntime.Pin
}

func (o *crossingOffer) close() {
	o.cancel()
	if o.pin != nil {
		o.pin.Release()
		o.pin = nil
	}
}

type transitionCommand struct {
	Offer     string `json:"offer"`
	Operation string `json:"operation,omitempty"`
}
type transitionComplete struct {
	Operation string         `json:"operation"`
	Welcome   visits.Welcome `json:"welcome"`
}
type crossing struct {
	offer          *crossingOffer
	blocked        int
	next           time.Time
	completed      *transitionComplete
	completedOffer string
	previousEpoch  int64
}

func (t *crossing) close() {
	if t.offer != nil {
		t.offer.close()
		t.offer = nil
	}
}
func (t *crossing) result() <-chan loadResult {
	if t.offer != nil && !t.offer.Ready {
		return t.offer.result
	}
	return nil
}
func (a *App) offerTransition(ctx context.Context, id access.Identity, m *movement.Member, t *crossing) (bool, error) {
	state := m.Snapshot()
	if t.blocked != 0 {
		p, ok := visits.Portal(m.Scene(), t.blocked)
		if !ok || !visits.Near(p, state.X, state.Y) {
			t.blocked = 0
		}
	}
	if t.offer != nil || state.Moving || time.Now().Before(t.next) {
		return false, nil
	}
	t.next = time.Now().Add(time.Second)
	for _, p := range m.Scene().Portals() {
		if p.Neighbor == t.blocked || !visits.Near(p, state.X, state.Y) {
			continue
		}
		key := m.Scene().Key()
		key.Sector = p.Neighbor
		check, done := context.WithTimeout(ctx, a.config.DBTimeout)
		territory, err := access.Sector(check, a.db, id.User, key)
		done()
		if errors.Is(err, access.ErrDenied) {
			continue
		}
		if err != nil {
			return false, err
		}
		nonce, err := access.UUID()
		if err != nil {
			return false, err
		}
		run, cancel := context.WithTimeout(ctx, a.config.SceneLoadTimeout)
		offer := &crossingOffer{ID: nonce, Neighbor: p.Neighbor, Owner: territory.Owner, Expires: time.Now().Add(30 * time.Second), cancel: cancel, result: make(chan loadResult)}
		m.Freeze()
		t.offer = offer
		go func() {
			pin, err := a.scenes.Acquire(run, key)
			if err == nil {
				_, _, err = visits.Pair(m.Scene(), pin.Scene())
			}
			if err != nil && pin != nil {
				pin.Release()
				pin = nil
			}
			select {
			case offer.result <- loadResult{pin, err}:
			case <-run.Done():
				if pin != nil {
					pin.Release()
				}
			}
		}()
		return true, nil
	}
	return false, nil
}

// commitCrossing serializes with takeover/checkpoint. Destination loading is
// already complete, and no WS write occurs while holding SQL/user locks.
func (a *App) commitCrossing(ctx context.Context, id access.Identity, w visits.Welcome, f scenestore.Fence, m *movement.Member, o *crossingOffer, operation string, gate chan struct{}) (*movement.Member, visits.Welcome, error) {
	if o == nil || !o.Ready || o.pin == nil || time.Now().After(o.Expires) {
		return m, w, visits.ErrOffer
	}
	if err := takeGate(ctx, gate); err != nil {
		return m, w, err
	}
	defer func() { <-gate }()
	stamp := m.Stamp()
	w.Generation = stamp.Generation
	w.PositionVersion = stamp.Version
	pos := m.Snapshot()
	next, err := a.visits.Transition(ctx, id, w, f, m.Scene(), o.pin.Scene(), pos.X, pos.Y, o.ID, operation)
	if err != nil {
		return m, w, err
	}
	// Transition returned only after the receipt and visit update COMMIT.
	// Publish before RAM Join: a failed Join does not undo authoritative SQL.
	if a.publisher != nil {
		k := m.Scene().Key()
		a.publisher.enqueue(gamesignal.SectorChange{UserID: strconv.FormatInt(id.User, 10), VisitID: next.Visit, Generation: strconv.FormatInt(next.Generation, 10), Epoch: strconv.FormatInt(next.Epoch, 10), RuntimeFence: strconv.FormatInt(f.Number, 10), SurfaceID: strconv.FormatInt(k.Surface, 10), From: k.Sector, To: next.Sector, Revision: strconv.FormatInt(k.Revision, 10), Operation: operation})
	}
	replacement, err := a.motion.Join(movement.Stamp{User: id.User, Actor: stamp.Actor, Nickname: stamp.Nickname, Visit: next.Visit, Epoch: next.Epoch, Generation: next.Generation, Version: next.PositionVersion}, o.pin.Scene(), next.X, next.Y)
	if err != nil {
		m.Close()
		return m, next, err
	}
	replacement.Permit(time.Now().Add(2 * time.Second))
	return replacement, next, nil
}
