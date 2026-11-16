package movement

import (
	"context"
	"math"
	"testing"
	"time"

	"github.com/yomezza/cosmosimulator/internal/planet/pathing"
)

func TestSteeringSpeedCollisionAndLease(t *testing.T) {
	s, x, y := scenery(t)
	h := New(context.Background(), 4, 24000)
	defer h.Close()
	m, err := h.Join(Stamp{User: 1, Epoch: 1}, s, x, y)
	if err != nil {
		t.Fatal(err)
	}
	m.Permit(time.Now().Add(5 * time.Second))
	start := pathing.FromQ(x, y)
	for i, d := range [][2]int{{1, 0}, {1, -1}} {
		if err = m.Steer(int64(i+2), d[0], d[1]); err != nil {
			t.Fatal(err)
		}
		h.mu.Lock()
		m.pos = start
		m.advanceSteering(.2)
		distance := math.Hypot(m.pos.X-start.X, m.pos.Y-start.Y)
		h.mu.Unlock()
		if math.Abs(distance-.2) > 1e-8 {
			t.Fatalf("direction %v changed speed: %v", d, distance)
		}
	}
	if m.Steer(4, 2, 0) == nil {
		t.Fatal("accepted invalid direction")
	}
	if err = m.Steer(4, 0, 0); err != nil {
		t.Fatal(err)
	}
	stopped := m.Snapshot()
	time.Sleep(110 * time.Millisecond)
	if next := m.Snapshot(); next.X != stopped.X || next.Y != stopped.Y || next.Moving {
		t.Fatal("keyup did not stop")
	}
	if err = m.Steer(5, 1, 0); err != nil {
		t.Fatal(err)
	}
	h.mu.Lock()
	m.steeringUntil = time.Now().Add(-time.Second)
	h.mu.Unlock()
	stopped = m.Snapshot()
	time.Sleep(110 * time.Millisecond)
	if next := m.Snapshot(); next.X != stopped.X || next.Y != stopped.Y || next.Moving {
		t.Fatal("lost input lease kept moving")
	}
	// A direct keyboard step obeys the same inflated obstacle/corner mask as A*.
	found := false
	h.mu.Lock()
	for yy := 1; yy < 255 && !found; yy++ {
		for xx := 1; xx < 255; xx++ {
			if s.Walkable(xx, yy) && !s.Walkable(xx+1, yy) {
				m.pos = pathing.Point{X: float64(xx) + .5, Y: float64(yy) + .5}
				before := m.pos
				m.steering = pathing.Point{X: 1}
				m.advanceSteering(.8)
				if m.pos != before || !m.steeringBlocked {
					t.Error("keyboard crossed obstacle")
				}
				found = true
				break
			}
		}
	}
	h.mu.Unlock()
	if !found {
		t.Fatal("fixture has no obstacle boundary")
	}
}

func TestSteeringReplacesMouseAndMouseReplacesSteering(t *testing.T) {
	s, x, y := scenery(t)
	h := New(context.Background(), 4, 24000)
	defer h.Close()
	m, err := h.Join(Stamp{User: 1, Epoch: 1}, s, x, y)
	if err != nil {
		t.Fatal(err)
	}
	m.Permit(time.Now().Add(5 * time.Second))
	if err = m.Move(2, x+2*1024, y); err != nil {
		t.Fatal(err)
	}
	if err = m.Steer(3, 1, 0); err != nil {
		t.Fatal(err)
	}
	h.mu.Lock()
	hasPath := len(m.path) > 0
	keyboard := m.steering.X
	h.mu.Unlock()
	if hasPath || keyboard != 1 {
		t.Fatal("keyboard retained mouse route")
	}
	if err = m.Move(4, x+1024, y); err != nil {
		t.Fatal(err)
	}
	h.mu.Lock()
	keyboard = m.steering.X
	h.mu.Unlock()
	if keyboard != 0 {
		t.Fatal("mouse retained held direction")
	}
	m.Freeze()
	if m.Snapshot().Moving {
		t.Fatal("freeze retained movement")
	}
}
