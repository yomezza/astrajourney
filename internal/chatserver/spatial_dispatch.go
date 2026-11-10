package chatserver

import (
	"context"
	"errors"

	"github.com/jackc/pgx/v5"
	"github.com/yomezza/cosmosimulator/internal/chat"
)

// Payload contains only a dirty user ID here. Its claimed sector/epoch never
// enters the permission path: existing SQL functions determine the current view.
func (d *EventDispatcher) refreshSpatial(ctx context.Context) error {
	if len(d.spatial) == 0 {
		return nil
	}
	handles := []*WSHandle{}
	connections := []chat.DispatchConnection{}
	events := []chat.DispatchEvent{}
	for _, h := range d.registry.Snapshot() {
		id := h.Connection.Identity.ID
		if d.spatial[id] {
			handles = append(handles, h)
			connections = append(connections, h.Connection)
			actor := id
			events = append(events, chat.DispatchEvent{Kind: "spatial.reconcile", Target: &actor})
		}
	}
	if len(handles) == 0 {
		clear(d.spatial)
		return nil
	}
	allowed, e := d.backend.ResolveRecipients(ctx, events, connections)
	if e != nil {
		return e
	}
	backend, ok := d.backend.(interface {
		ScopeStamp(context.Context, chat.Lease) (string, error)
	})
	if !ok {
		return errors.New("spatial refresh requires authoritative ScopeStamp")
	}
	ready := []*WSHandle{}
	for _, h := range handles {
		if !allowed[h.Connection.Identity.ID] {
			continue
		}
		stamp, e := backend.ScopeStamp(ctx, h.Connection.Lease)
		if errors.Is(e, pgx.ErrNoRows) {
			continue
		}
		if e != nil {
			return e
		}
		// ScopeStamp includes chat_lock_planet_presence: current visit, epoch,
		// runtime lease/fence, freshness and geography control. An absent presence
		// yields a non-spatial view, never a grant inferred from NATS.
		if stamp != "" {
			ready = append(ready, h)
		}
	}
	for _, h := range ready {
		if d.registry.Notify(h) {
			d.spatialRechecks.Add(1)
		}
	}
	clear(d.spatial)
	return nil
}
