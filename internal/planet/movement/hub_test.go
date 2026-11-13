package movement

import (
	"context"
	"errors"
	"github.com/yomezza/cosmosimulator/internal/geography"
	"github.com/yomezza/cosmosimulator/internal/planet/pathing"
	"github.com/yomezza/cosmosimulator/internal/planet/scene"
	"math"
	"strings"
	"testing"
	"time"
)

func scenery(t *testing.T) (*scene.Scene, int, int) {
	t.Helper()
	g := &geography.PlanetSurface{Width: 4, Height: 4, GridW: 2, GridH: 2, SeaLevel: 460, Heights: []int{700, 700, 700, 700}, Water: make([]int, 4), Sectors: []geography.PlanetSector{{Number: 1, Center: geography.SurfacePoint{X: 2, Y: 2}}}}
	s, err := scene.Generate(context.Background(), scene.Key{Surface: 1, Sector: 1}, g, strings.Repeat("a", 64))
	if err != nil {
		t.Fatal(err)
	}
	p := s.Metadata().Landing
	return s, p.SpawnX, p.SpawnY
}
func TestMovementDistanceAndLifecycle(t *testing.T) {
	s, x, y := scenery(t)
	p := pathing.FromQ(x, y)
	raw := &Member{actor: &actor{scene: s}, pos: p, path: []pathing.Point{{X: p.X + 2, Y: p.Y}}}
	raw.advance(.2)
	if math.Abs(math.Hypot(raw.pos.X-p.X, raw.pos.Y-p.Y)-.2) > 1e-8 {
		t.Fatal("distance not server step")
	}
	h := New(context.Background(), 4, 24000)
	defer h.Close()
	m, err := h.Join(Stamp{User: 1, Epoch: 1}, s, x, y)
	if err != nil {
		t.Fatal(err)
	}
	m.Permit(time.Now().Add(time.Second))
	if err = m.Move(2, x+2*1024, y); err != nil {
		t.Fatal(err)
	}
	if err = m.Move(3, -1, y); !errors.Is(err, pathing.ErrBlocked) {
		t.Fatal(err)
	}
	deadline := time.Now().Add(time.Second)
	for m.Snapshot().Generation < 1 && time.Now().Before(deadline) {
		time.Sleep(time.Millisecond)
	}
	frozen := m.Freeze()
	time.Sleep(110 * time.Millisecond)
	now := m.Snapshot()
	if frozen.X != now.X || frozen.Y != now.Y || now.Moving {
		t.Fatal("late path result moved frozen character")
	}
	newer, err := h.Join(Stamp{User: 1, Epoch: 2}, s, x, y)
	if err != nil {
		t.Fatal(err)
	}
	select {
	case <-m.Done():
	default:
		t.Fatal("old controller still live")
	}
	if err = m.Move(5, x, y); !errors.Is(err, ErrStale) {
		t.Fatal(err)
	}
	if h.Count() != 1 {
		t.Fatal("more than one sector actor")
	}
	m.Close()
	if h.Count() != 1 {
		t.Fatal("old close removed successor")
	}
	newer.Close()
	if h.Count() != 0 {
		t.Fatal("empty scene kept ticking")
	}
}
func TestLatestGoalAndPermit(t *testing.T) {
	s, x, y := scenery(t)
	h := New(context.Background(), 4, 24000)
	defer h.Close()
	m, _ := h.Join(Stamp{User: 1, Epoch: 1}, s, x, y)
	m.Permit(time.Now().Add(time.Second))
	if err := m.Move(2, x-1024, y); err != nil {
		t.Fatal(err)
	}
	if err := m.Move(3, x+1024, y); err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(time.Second)
	var state State
	for time.Now().Before(deadline) {
		state = m.Snapshot()
		if len(state.Path) > 0 && state.Seq == 3 {
			break
		}
		time.Sleep(time.Millisecond)
	}
	if len(state.Path) == 0 || state.Path[len(state.Path)-1].X != float64(x/1024+1)+.5 {
		t.Fatal("superseded path won", state)
	}
	m.Permit(time.Now().Add(-time.Second))
	stopped := m.Snapshot()
	time.Sleep(110 * time.Millisecond)
	after := m.Snapshot()
	if after.X != stopped.X || after.Y != stopped.Y || !after.Suspended {
		t.Fatal("expired access permit kept moving")
	}
	if err := m.Move(4, x, y); !errors.Is(err, ErrSuspended) {
		t.Fatal(err)
	}
}

func TestTemporaryPauseResumesAcceptedRoute(t *testing.T) {
	s, x, y := scenery(t)
	h := New(context.Background(), 4, 24000)
	defer h.Close()
	m, err := h.Join(Stamp{User: 1, Epoch: 1}, s, x, y)
	if err != nil {
		t.Fatal(err)
	}
	m.Permit(time.Now().Add(3 * time.Second))
	if err = m.Move(2, x+1024, y); err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(time.Second)
	for len(m.Snapshot().Path) == 0 && time.Now().Before(deadline) {
		time.Sleep(time.Millisecond)
	}
	m.Pause()
	before := m.Snapshot()
	if len(before.Path) == 0 {
		t.Fatal("pause discarded the accepted route")
	}
	time.Sleep(110 * time.Millisecond)
	after := m.Snapshot()
	if after.X != before.X || after.Y != before.Y || !after.Suspended {
		t.Fatal("paused character moved")
	}
	m.Thaw()
	for time.Now().Before(deadline) {
		after = m.Snapshot()
		if after.X == x+1024 && !after.Moving {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatal("route did not resume without another move command", after)
}

func TestSectorPresenceCoalesces(t *testing.T) {
	s, x, y := scenery(t)
	h := New(context.Background(), 4, 24000)
	defer h.Close()
	a, _ := h.Join(Stamp{User: 1, Epoch: 1, Actor: "public-a", Nickname: "Alice"}, s, x, y)
	b, _ := h.Join(Stamp{User: 2, Epoch: 1, Actor: "public-b", Nickname: "Bob"}, s, x, y)
	a.Permit(time.Now().Add(time.Second))
	b.Permit(time.Now().Add(time.Second))
	// A subscriber that does not read has exactly one replaceable frame.
	time.Sleep(230 * time.Millisecond)
	if len(a.presence) != 1 {
		t.Fatal("unbounded/missing mailbox")
	}
	b.Close()
	time.Sleep(130 * time.Millisecond)
	v := <-a.Presence()
	if len(v.Actors) != 1 || v.Actors[0].ID != "public-a" {
		t.Fatal("coalescing retained a ghost", v)
	}
	newer, _ := h.Join(Stamp{User: 1, Epoch: 2, Actor: "public-a", Nickname: "Alice"}, s, x, y)
	a.Close()
	if len(newer.Sector().Actors) != 1 {
		t.Fatal("replacement duplicated actor")
	}
	// An unrelated immutable sector cannot leak into this membership.
	_, x2, y2 := scenery(t)
	// Generate a distinct scene key using the existing canonical fixture.
	g := &geography.PlanetSurface{Width: 4, Height: 4, GridW: 2, GridH: 2, SeaLevel: 460, Heights: []int{700, 700, 700, 700}, Water: make([]int, 4), Sectors: []geography.PlanetSector{{Number: 1, Center: geography.SurfacePoint{X: 2, Y: 2}}}}
	other, err := scene.Generate(context.Background(), scene.Key{Surface: 2, Sector: 1}, g, strings.Repeat("a", 64))
	if err != nil {
		t.Fatal(err)
	}
	stranger, err := h.Join(Stamp{User: 3, Epoch: 1, Actor: "public-c"}, other, x2, y2)
	if err != nil {
		t.Fatal(err)
	}
	if len(newer.Sector().Actors) != 1 || len(stranger.Sector().Actors) != 1 {
		t.Fatal("sector isolation failed")
	}
	crossing, err := h.Join(Stamp{User: 1, Epoch: 3, Actor: "public-a"}, other, x2, y2)
	if err != nil {
		t.Fatal(err)
	}
	newer.Close()
	if len(crossing.Sector().Actors) != 2 || h.Count() != 1 {
		t.Fatal("crossing duplicated actor across sectors")
	}
	if crossing.Stamp().Actor != "public-a" {
		t.Fatal("crossing changed identity")
	}
}

func TestAuthoritativeFacing220(t *testing.T) {
	s, x, y := scenery(t)
	start := pathing.FromQ(x, y)
	for _, c := range []struct {
		dx, dy float64
		facing int
	}{{0, 1, 0}, {-1, 1, 1}, {-1, 0, 2}, {-1, -1, 3}, {0, -1, 4}, {1, -1, 5}, {1, 0, 6}, {1, 1, 7}} {
		m := &Member{actor: &actor{scene: s}, pos: start, path: []pathing.Point{{X: start.X + c.dx, Y: start.Y + c.dy}}}
		m.advance(.1)
		if m.facing != c.facing {
			t.Fatalf("direction %v,%v => %d, expected %d", c.dx, c.dy, m.facing, c.facing)
		}
	}
}
