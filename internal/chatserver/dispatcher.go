package chatserver

import (
	"context"
	"sync/atomic"
	"time"

	"github.com/yomezza/cosmosimulator/internal/chat"
)

type DispatchBackend interface {
	OutboxWindow(context.Context, time.Duration) (chat.DispatchWindow, error)
	OutboxPage(context.Context, chat.DispatchWindow, int64, int64, int) ([]chat.DispatchEvent, error)
	ResolveRecipients(context.Context, []chat.DispatchEvent, []chat.DispatchConnection) (map[int64]bool, error)
}
type dispatchCycle struct {
	window       chat.DispatchWindow
	after, floor int64
}
type EventDispatcher struct {
	backend                                  DispatchBackend
	registry                                 *WSRegistry
	source                                   WakeSource
	config                                   DispatcherConfig
	cycle                                    *dispatchCycle
	highwater                                int64
	initialized                              bool
	reconcile                                bool
	seen                                     map[int64]bool
	ring                                     []int64
	ringIndex                                int
	scans, failures, rows, duplicates, wakes atomic.Uint64
	water                                    atomic.Int64
	degraded                                 atomic.Bool
}

func NewEventDispatcher(b DispatchBackend, r *WSRegistry, s WakeSource, c DispatcherConfig) *EventDispatcher {
	return &EventDispatcher{backend: b, registry: r, source: s, config: c, seen: map[int64]bool{}}
}
func (d *EventDispatcher) remember(id int64) {
	if len(d.ring) < 32768 {
		d.ring = append(d.ring, id)
	} else {
		delete(d.seen, d.ring[d.ringIndex])
		d.ring[d.ringIndex] = id
		d.ringIndex = (d.ringIndex + 1) % len(d.ring)
	}
	d.seen[id] = true
}

// step processes at most one bounded page. highwater advances only after the
// entire fixed-window scan succeeds. Late lower IDs are recovered by overlap;
// no cursor persists across process restarts (fresh WS always bootstraps).
func (d *EventDispatcher) step(ctx context.Context) (bool, error) {
	d.scans.Add(1)
	if d.cycle == nil {
		w, err := d.backend.OutboxWindow(ctx, d.config.Overlap)
		if err != nil {
			return false, err
		}
		floor := d.highwater
		if !d.initialized {
			floor = w.Max
		}
		// Startup filtering is local to this scan, never a committed cursor.
		d.cycle = &dispatchCycle{window: w, floor: floor}
	}
	c := d.cycle
	events, err := d.backend.OutboxPage(ctx, c.window, c.floor, c.after, 1000)
	if err != nil {
		return false, err
	}
	d.rows.Add(uint64(len(events)))
	fresh := make([]chat.DispatchEvent, 0, len(events))
	for _, e := range events {
		if d.seen[e.ID] {
			d.duplicates.Add(1)
		} else {
			fresh = append(fresh, e)
		}
	}
	handles := d.registry.Snapshot()
	if (len(fresh) > 0 || d.reconcile) && len(handles) > 0 {
		connections := make([]chat.DispatchConnection, 0, len(handles))
		for _, h := range handles {
			connections = append(connections, h.Connection)
		}
		allowed := map[int64]bool{}
		if len(fresh) > 0 {
			var e error
			allowed, e = d.backend.ResolveRecipients(ctx, fresh, connections)
			if e != nil {
				return false, e
			}
		}
		if d.reconcile {
			// A SQL outage can exceed overlap/retention. Refresh current valid
			// sessions through SQL, rather than replaying cached permissions.
			refresh := make([]chat.DispatchEvent, 0, len(handles))
			for _, h := range handles {
				id := h.Connection.Identity.ID
				refresh = append(refresh, chat.DispatchEvent{Kind: "reconcile", Target: &id})
			}
			current, e := d.backend.ResolveRecipients(ctx, refresh, connections)
			if e != nil {
				return false, e
			}
			for id := range current {
				allowed[id] = true
			}
		}
		for _, h := range handles {
			if allowed[h.Connection.Identity.ID] {
				d.registry.Notify(h)
			}
		}
	}
	d.reconcile = false
	for _, e := range fresh {
		d.remember(e.ID)
	}
	if len(events) > 0 {
		c.after = events[len(events)-1].ID
	}
	if len(events) < 1000 || c.after >= c.window.Max {
		d.highwater = max(d.highwater, c.window.Max)
		d.initialized = true
		d.water.Store(d.highwater)
		d.cycle = nil
		return false, nil
	}
	return true, nil
}
func (d *EventDispatcher) Run(ctx context.Context) {
	if err := d.source.Start(ctx); err != nil {
		d.degraded.Store(true)
	}
	defer func() {
		closeCtx, cancel := context.WithTimeout(context.Background(), time.Second)
		defer cancel()
		_ = d.source.Close(closeCtx)
	}()
	timer := time.NewTimer(0)
	defer timer.Stop()
	var retryAt time.Time
	dirty := false
	for {
		select {
		case <-ctx.Done():
			return
		case <-d.source.Wakeups():
			d.wakes.Add(1)
			dirty = true
			select {
			case <-ctx.Done():
				return
			case <-time.After(50 * time.Millisecond):
			}
		case <-timer.C:
		}
		if left := time.Until(retryAt); left > 0 {
			timer.Reset(left)
			continue
		}
		// A wake consumed while paging must cause a NEW window after this one,
		// not be lost as just a continuation of a pre-commit max(id) snapshot.
		if d.cycle == nil {
			dirty = false
		}
		check, cancel := context.WithTimeout(ctx, 3*time.Second)
		backlog, err := d.step(check)
		cancel()
		next := d.config.PollInterval
		if err != nil {
			d.failures.Add(1)
			d.degraded.Store(true)
			d.cycle = nil
			d.reconcile = true
			retryAt = time.Now().Add(d.config.DegradedInterval)
			next = d.config.DegradedInterval
		} else {
			d.degraded.Store(!d.source.Healthy())
			if backlog || dirty {
				next = time.Millisecond
			} else if !d.source.Healthy() {
				next = d.config.DegradedInterval
			}
		}
		timer.Reset(next)
	}
}
func (d *EventDispatcher) Diagnostics() map[string]any {
	return map[string]any{"scans": d.scans.Load(), "scan_errors": d.failures.Load(), "rows": d.rows.Load(), "deduplicated_rows": d.duplicates.Load(), "wakes": d.wakes.Load(), "highwater": d.water.Load(), "degraded": d.degraded.Load()}
}
