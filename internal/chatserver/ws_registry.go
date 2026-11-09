package chatserver

import (
	"sort"
	"sync"
	"sync/atomic"

	"github.com/yomezza/cosmosimulator/internal/chat"
)

type WSHandle struct {
	Connection chat.DispatchConnection
	Sync       chan struct{}
	stop       func()
}

// WSRegistry is transport-local. A handle is registered only after DB Claim.
// Mailboxes are never closed: unregister/cancel cannot race a send into panic.
type WSRegistry struct {
	mu                sync.Mutex
	connections       map[int64]*WSHandle
	closed            bool
	queued, coalesced atomic.Uint64
}

func NewWSRegistry() *WSRegistry { return &WSRegistry{connections: map[int64]*WSHandle{}} }
func (r *WSRegistry) Register(u chat.Identity, l chat.Lease, stop func()) (*WSHandle, bool) {
	r.mu.Lock()
	if r.closed || u.ID != l.User {
		r.mu.Unlock()
		return nil, false
	}
	old := r.connections[u.ID]
	if old != nil && l.Generation <= old.Connection.Lease.Generation {
		r.mu.Unlock()
		return nil, false
	}
	if old == nil && len(r.connections) >= 256 {
		r.mu.Unlock()
		return nil, false
	}
	h := &WSHandle{Connection: chat.DispatchConnection{Identity: u, Lease: l}, Sync: make(chan struct{}, 1), stop: stop}
	r.connections[u.ID] = h
	r.mu.Unlock()
	if old != nil {
		old.stop()
	}
	return h, true
}
func (r *WSRegistry) Unregister(h *WSHandle) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.connections[h.Connection.Identity.ID] == h {
		delete(r.connections, h.Connection.Identity.ID)
	}
}
func (r *WSRegistry) Current(h *WSHandle) bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	return !r.closed && r.connections[h.Connection.Identity.ID] == h
}
func (r *WSRegistry) Notify(h *WSHandle) bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.closed || r.connections[h.Connection.Identity.ID] != h {
		return false
	}
	select {
	case h.Sync <- struct{}{}:
		r.queued.Add(1)
	default:
		r.coalesced.Add(1)
	}
	return true
}
func (r *WSRegistry) Snapshot() []*WSHandle {
	r.mu.Lock()
	defer r.mu.Unlock()
	out := make([]*WSHandle, 0, len(r.connections))
	for _, h := range r.connections {
		out = append(out, h)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Connection.Identity.ID < out[j].Connection.Identity.ID })
	return out
}
func (r *WSRegistry) Close() {
	r.mu.Lock()
	r.closed = true
	handles := r.connections
	r.connections = map[int64]*WSHandle{}
	r.mu.Unlock()
	for _, h := range handles {
		h.stop()
	}
}
