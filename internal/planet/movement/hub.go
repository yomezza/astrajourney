// Package movement runs one timer per active sector. It has no SQL or sockets.
package movement

import (
	"context"
	"errors"
	"github.com/yomezza/cosmosimulator/internal/planet/pathing"
	"github.com/yomezza/cosmosimulator/internal/planet/scene"
	"math"
	"sync"
	"time"
)

var ErrBusy = errors.New("path_queue_full")
var ErrRate = errors.New("move_rate_limited")
var ErrStale = errors.New("session_replaced")
var ErrSuspended = errors.New("movement_suspended")

type Stamp struct {
	Actor, Nickname                  string
	User, Epoch, Generation, Version int64
	Visit                            string
}
type State struct {
	Facing     int             `json:"facing"`
	Remaining  int             `json:"remaining"`
	X          int             `json:"x_q"`
	Y          int             `json:"y_q"`
	Moving     bool            `json:"moving"`
	Suspended  bool            `json:"suspended"`
	Generation int64           `json:"path_generation"`
	Seq        int64           `json:"command_seq"`
	Time       int64           `json:"server_time"`
	Path       []pathing.Point `json:"path,omitempty"`
	Error      string          `json:"error,omitempty"`
}
type Hub struct {
	metrics observations
	mu      sync.Mutex
	ctx     context.Context
	cancel  context.CancelFunc
	closed  bool
	speed   float64
	budget  int
	actors  map[scene.Key]*actor
	members map[int64]*Member
	jobs    chan job
	wg      sync.WaitGroup
}
type actor struct {
	sequence int64
	scene    *scene.Scene
	cancel   context.CancelFunc
	members  map[int64]*Member
}
type Member struct {
	hub             *Hub
	actor           *actor
	stamp           Stamp
	facing          int
	pos             pathing.Point
	saved           pathing.Point
	path            []pathing.Point
	generation, seq int64
	error           string
	permit          time.Time
	frozen, closed  bool
	cancelPath      context.CancelFunc
	tokens          float64
	refill          time.Time
	presence        chan Sector
	updates         chan State
	done            chan struct{}
}
type job struct {
	member        *Member
	ctx           context.Context
	generation    int64
	start, target pathing.Point
	queued        time.Time
}

func New(ctx context.Context, speed float64, budget int) *Hub {
	run, cancel := context.WithCancel(ctx)
	h := &Hub{ctx: run, cancel: cancel, speed: speed, budget: budget, actors: map[scene.Key]*actor{}, members: map[int64]*Member{}, jobs: make(chan job, 32)}
	for range 2 {
		h.wg.Add(1)
		go h.worker()
	}
	return h
}
func (h *Hub) Join(stamp Stamp, s *scene.Scene, x, y int) (*Member, error) {
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.closed || h.ctx.Err() != nil {
		return nil, ErrSuspended
	}
	p := pathing.FromQ(x, y)
	if !pathing.Clear(s, p) {
		return nil, pathing.ErrBlocked
	}
	if old := h.members[stamp.User]; old != nil {
		if old.stamp.Epoch >= stamp.Epoch {
			return nil, ErrStale
		}
		h.remove(old)
	}
	a := h.actors[s.Key()]
	if a == nil {
		ctx, cancel := context.WithCancel(h.ctx)
		a = &actor{scene: s, cancel: cancel, members: map[int64]*Member{}}
		h.actors[s.Key()] = a
		h.wg.Add(1)
		go h.run(ctx, a)
	}
	m := &Member{hub: h, actor: a, stamp: stamp, pos: p, saved: p, tokens: 20, refill: time.Now(), presence: make(chan Sector, 1), updates: make(chan State, 1), done: make(chan struct{})}
	a.members[stamp.User] = m
	h.members[stamp.User] = m
	return m, nil
}
func (h *Hub) Current(user int64) *Member { h.mu.Lock(); defer h.mu.Unlock(); return h.members[user] }
func (m *Member) Done() <-chan struct{}   { return m.done }
func (m *Member) Updates() <-chan State   { return m.updates }
func (m *Member) Stamp() Stamp            { m.hub.mu.Lock(); defer m.hub.mu.Unlock(); return m.stamp }
func (m *Member) Scene() *scene.Scene     { return m.actor.scene }
func (m *Member) snapshot(now time.Time) State {
	x, y := m.pos.Quantized()
	moving := len(m.path) > 0 && !m.frozen && now.Before(m.permit)
	return State{Facing: m.facing, Remaining: len(m.path), X: x, Y: y, Moving: moving, Suspended: m.frozen || !now.Before(m.permit), Generation: m.generation, Seq: m.seq, Time: now.UnixMilli(), Path: append([]pathing.Point(nil), m.path...), Error: m.error}
}
func (m *Member) Snapshot() State {
	m.hub.mu.Lock()
	defer m.hub.mu.Unlock()
	return m.snapshot(time.Now())
}
func (m *Member) Permit(until time.Time) {
	m.hub.mu.Lock()
	defer m.hub.mu.Unlock()
	if !m.closed {
		m.permit = until
	}
}
func (m *Member) Freeze() State {
	m.hub.mu.Lock()
	defer m.hub.mu.Unlock()
	m.frozen = true
	m.path = nil
	m.generation++
	if m.cancelPath != nil {
		m.cancelPath()
	}
	return m.snapshot(time.Now())
}

// Pause stops simulation during temporary server validation without discarding
// the accepted route. Freeze remains the cancellation boundary for handoff/exit.
func (m *Member) Pause() {
	m.hub.mu.Lock()
	defer m.hub.mu.Unlock()
	m.frozen = true
}
func (m *Member) Thaw() {
	m.hub.mu.Lock()
	defer m.hub.mu.Unlock()
	if !m.closed {
		m.frozen = false
	}
}
func (m *Member) Saved(version int64, x, y int) {
	m.hub.mu.Lock()
	defer m.hub.mu.Unlock()
	m.stamp.Version = version
	m.saved = pathing.FromQ(x, y)
}
func (m *Member) Dirty() bool {
	m.hub.mu.Lock()
	defer m.hub.mu.Unlock()
	x, y := m.pos.Quantized()
	sx, sy := m.saved.Quantized()
	return x != sx || y != sy
}
func (m *Member) Move(seq int64, x, y int) error {
	h := m.hub
	h.mu.Lock()
	defer h.mu.Unlock()
	now := time.Now()
	if m.closed || h.members[m.stamp.User] != m {
		return ErrStale
	}
	if m.frozen || !now.Before(m.permit) {
		return ErrSuspended
	}
	if seq <= m.seq {
		return ErrStale
	}
	m.tokens = math.Min(20, m.tokens+now.Sub(m.refill).Seconds()*10)
	m.refill = now
	if m.tokens < 1 {
		return ErrRate
	}
	m.tokens--
	target := pathing.FromQ(x, y)
	if !pathing.Clear(m.actor.scene, target) {
		return pathing.ErrBlocked
	}
	ctx, cancel := context.WithTimeout(h.ctx, 200*time.Millisecond)
	next := m.generation + 1
	j := job{m, ctx, next, m.pos, target, time.Now()}
	select {
	case h.jobs <- j:
		h.metrics.queued++
		if m.cancelPath != nil {
			m.cancelPath()
		}
		m.cancelPath = cancel
		m.generation = next
		m.seq = seq
		m.path = nil
		m.error = ""
		return nil
	default:
		h.metrics.rejected++
		cancel()
		return ErrBusy
	}
}
func (h *Hub) worker() {
	defer h.wg.Done()
	for {
		select {
		case <-h.ctx.Done():
			return
		case j := <-h.jobs:
			began := time.Now()
			h.metrics.queue.Observe(began.Sub(j.queued))
			path, err := pathing.Find(j.ctx, j.member.actor.scene, j.start, j.target, h.budget)
			h.metrics.paths.Observe(time.Since(began))
			h.mu.Lock()
			m := j.member
			if errors.Is(err, pathing.ErrBudget) {
				h.metrics.budget++
			}
			if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
				h.metrics.canceled++
			}
			if !m.closed && !m.frozen && m.generation == j.generation && h.members[m.stamp.User] == m {
				if err != nil {
					m.error = pathing.ErrBudget.Error()
					if !errors.Is(err, context.Canceled) && !errors.Is(err, context.DeadlineExceeded) {
						m.error = err.Error()
					}
				} else {
					m.path = append([]pathing.Point(nil), path[1:]...)
					m.error = ""
				}
				h.publish(m, time.Now())
			}
			h.mu.Unlock()
		}
	}
}
func (h *Hub) publish(m *Member, now time.Time) {
	v := m.snapshot(now)
	select {
	case m.updates <- v:
	default:
		select {
		case <-m.updates:
			m.hub.metrics.coalesced++
		default:
		}
		select {
		case m.updates <- v:
		default:
		}
	}
}
func (h *Hub) run(ctx context.Context, a *actor) {
	defer h.wg.Done()
	ticker := time.NewTicker(50 * time.Millisecond)
	defer ticker.Stop()
	last := time.Now()
	emit := 0
	for {
		select {
		case <-ctx.Done():
			return
		case now := <-ticker.C:
			dt := min(.1, max(0, now.Sub(last).Seconds()))
			last = now
			h.mu.Lock()
			h.metrics.lag.Observe(time.Since(now))
			emit++
			for _, m := range a.members {
				if !m.closed && !m.frozen && now.Before(m.permit) {
					m.advance(dt * h.speed)
				}
				if emit%2 == 0 {
					h.publish(m, now)
				}
			}
			if emit%2 == 0 {
				h.broadcast(a, now)
			}
			h.mu.Unlock()
		}
	}
}
func (m *Member) advance(distance float64) {
	for distance > 0 && len(m.path) > 0 {
		target := m.path[0]
		dx, dy := target.X-m.pos.X, target.Y-m.pos.Y
		length := math.Hypot(dx, dy)
		if length < 1e-9 {
			m.path = m.path[1:]
			continue
		}
		step := min(distance, length)
		next := pathing.Point{X: m.pos.X + dx/length*step, Y: m.pos.Y + dy/length*step}
		if !pathing.Segment(m.actor.scene, m.pos, next) {
			m.path = nil
			m.error = pathing.ErrBlocked.Error()
			return
		}
		m.facing = (int(math.Round(math.Atan2(dy, dx)/(math.Pi/4))) + 14) % 8
		m.pos = next
		distance -= step
		if step >= length {
			m.pos = target
			m.path = m.path[1:]
		}
	}
}
func (h *Hub) remove(m *Member) {
	if m.closed {
		return
	}
	m.closed = true
	if m.cancelPath != nil {
		m.cancelPath()
	}
	close(m.done)
	delete(m.actor.members, m.stamp.User)
	if h.members[m.stamp.User] == m {
		delete(h.members, m.stamp.User)
	}
	if len(m.actor.members) == 0 {
		m.actor.cancel()
		if h.actors[m.actor.scene.Key()] == m.actor {
			delete(h.actors, m.actor.scene.Key())
		}
	}
}
func (m *Member) Close() { m.hub.mu.Lock(); defer m.hub.mu.Unlock(); m.hub.remove(m) }
func (h *Hub) Close() {
	h.mu.Lock()
	if !h.closed {
		h.closed = true
		h.cancel()
		for _, m := range h.members {
			h.remove(m)
		}
	}
	h.mu.Unlock()
	h.wg.Wait()
}
func (h *Hub) Count() int { h.mu.Lock(); defer h.mu.Unlock(); return len(h.actors) }
