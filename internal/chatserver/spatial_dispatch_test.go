package chatserver

import (
	"context"
	"errors"
	"github.com/nats-io/nats.go"
	"github.com/yomezza/cosmosimulator/internal/chat"
	"github.com/yomezza/cosmosimulator/internal/platform/gamesignal"
	"testing"
)

type spatialBackend struct {
	fakeDispatch
	calls     int
	failScope bool
}

func (b *spatialBackend) ScopeStamp(context.Context, chat.Lease) (string, error) {
	b.calls++
	if b.failScope {
		return "", errors.New("SQL unavailable")
	}
	return "authoritative-current-sector-3", nil
}
func TestSpatialDirtyUsesSQLAndCurrentHandle(t *testing.T) {
	r := NewWSRegistry()
	one := testHandle(t, r, 1, 1)
	two := testHandle(t, r, 2, 1)
	b := &spatialBackend{}
	d := NewEventDispatcher(b, r, NewLocalWakeSource(), DefaultDispatcherConfig())
	source := &NATSWakeSource{wakes: make(chan struct{}, 1), seen: map[string]bool{}}
	payload, ok := gamesignal.EncodePlanet(gamesignal.SectorChange{UserID: "1", VisitID: "12345678-1234-4234-8234-123456789abc", Generation: "1", Epoch: "2", RuntimeFence: "1", SurfaceID: "5", From: 1, To: 2, Revision: "1", Operation: "12345678-1234-4234-8234-123456789def"})
	if !ok {
		t.Fatal("encode")
	}
	source.accept(&nats.Msg{Subject: gamesignal.PlanetSectorSubject, Data: payload})
	source.accept(&nats.Msg{Subject: gamesignal.PlanetSectorSubject, Data: payload})
	ids := source.TakeSpatialDirty()
	if len(ids) != 1 || ids[0] != 1 || source.duplicates.Load() != 1 {
		t.Fatal("dirty/dedupe")
	}
	d.spatial[1] = true
	b.failScope = true
	if e := d.refreshSpatial(context.Background()); e == nil || len(one.Sync) != 0 || !d.spatial[1] {
		t.Fatal("SQL error did not fail closed/retry")
	}
	b.failScope = false
	if e := d.refreshSpatial(context.Background()); e != nil || len(one.Sync) != 1 || len(two.Sync) != 0 || len(d.spatial) != 0 || b.calls != 2 {
		t.Fatal("not an addressed SQL refresh", e)
	}
}
