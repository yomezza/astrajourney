package chatserver

import (
	"context"
	"crypto/tls"
	"errors"
	"math/rand/v2"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/nats-io/nats.go"
	"github.com/yomezza/cosmosimulator/internal/platform/gamesignal"
	"github.com/yomezza/cosmosimulator/internal/platform/natscore"
)

// NATSWakeSource uses Core only. One supervisor owns each connection and its
// subscription; it automatically reconnects and confirms SUB with PING/PONG.
// No domain operation, SQL ACL, request/reply or JetStream belongs here.
type NATSWakeSource struct {
	spatialMu                                                    sync.Mutex
	spatial                                                      map[int64]bool
	spatialReceived, spatialDropped, spatialSubscriptionFailures atomic.Uint64
	config                                                       NATSConfig
	tls                                                          *tls.Config
	password                                                     string
	wakes, publishes                                             chan struct{}
	mu                                                           sync.Mutex
	cancel                                                       context.CancelFunc
	done                                                         chan struct{}
	closed, healthy, reconcile                                   atomic.Bool

	attempts, registrations, disconnects, published, received, invalid, duplicates, coalesced, dropped atomic.Uint64
	lastError                                                                                          atomic.Value
	tlsFailures, authFailures, permissionFailures, networkFailures                                     atomic.Uint64

	seenMu    sync.Mutex
	seen      map[string]bool
	ring      []string
	ringIndex int
}

func NewNATSWakeSource(c NATSConfig) (*NATSWakeSource, error) {
	if e := c.Validate("b1"); e != nil {
		return nil, e
	}
	tlsConfig, password, e := c.credentials()
	if e != nil {
		return nil, e
	}
	s := &NATSWakeSource{config: c, tls: tlsConfig, password: password, wakes: make(chan struct{}, 1), publishes: make(chan struct{}, 1), seen: map[string]bool{}}
	s.lastError.Store("")
	return s, nil
}
func (s *NATSWakeSource) Wakeups() <-chan struct{} { return s.wakes }
func (s *NATSWakeSource) Healthy() bool            { return s.healthy.Load() && !s.closed.Load() }
func (s *NATSWakeSource) TakeReconcile() bool      { return s.reconcile.Swap(false) }
func (s *NATSWakeSource) wake() {
	if !s.closed.Load() {
		select {
		case s.wakes <- struct{}{}:
		default:
		}
	}
}
func (s *NATSWakeSource) AfterCommit() {
	// Never perform network I/O or a local dispatcher wake on the caller's
	// transaction path. Offline hints can be discarded: SQL is authoritative.
	if !s.Healthy() {
		s.dropped.Add(1)
		return
	}
	select {
	case s.publishes <- struct{}{}:
	default:
		s.coalesced.Add(1)
	}
}
func (s *NATSWakeSource) TakeSpatialDirty() []int64 {
	s.spatialMu.Lock()
	defer s.spatialMu.Unlock()
	ids := make([]int64, 0, len(s.spatial))
	for id := range s.spatial {
		ids = append(ids, id)
	}
	s.spatial = nil
	return ids
}
func (s *NATSWakeSource) accept(m *nats.Msg) {
	if s.closed.Load() {
		return
	}
	if m.Subject == gamesignal.PlanetSectorSubject {
		event, ok := gamesignal.DecodePlanet(m.Subject, m.Data)
		if !ok || m.Reply != "" {
			s.invalid.Add(1)
			return
		}
		if s.duplicate("planet:" + event.Data.UserID + ":" + event.SignalID) {
			return
		}
		s.spatialReceived.Add(1)
		id := event.Data.User()
		s.spatialMu.Lock()
		if s.spatial == nil {
			s.spatial = map[int64]bool{}
		}
		if len(s.spatial) < 256 || s.spatial[id] {
			s.spatial[id] = true
		} else {
			s.spatialDropped.Add(1)
		}
		s.spatialMu.Unlock()
		s.wake()
		return
	}
	signal, ok := decodeChatSignal(m.Subject, m.Data)
	if !ok || m.Reply != "" {
		s.invalid.Add(1)
		return
	}
	s.received.Add(1)
	if s.duplicate(chatSignalKey(signal.SignalID)) {
		return
	}
	s.wake()
}
func (s *NATSWakeSource) Start(ctx context.Context) error {
	s.mu.Lock()
	if s.closed.Load() || s.done != nil {
		s.mu.Unlock()
		return errors.New("NATS source already started or closed")
	}
	run, cancel := context.WithCancel(ctx)
	s.cancel = cancel
	s.done = make(chan struct{})
	first := make(chan error, 1)
	go s.run(run, first)
	s.mu.Unlock()
	select {
	case e := <-first:
		return e
	case <-ctx.Done():
		return ctx.Err()
	}
}

func natsErrorCode(e error) string { return natscore.ErrorCode(e) }
func (s *NATSWakeSource) session(ctx context.Context, first chan<- error) error {
	s.attempts.Add(1)
	fault := make(chan error, 1)
	// Old connection callbacks only touch their own bounded mailbox. They must
	// not overwrite the health/error state of a newer subscription generation.
	failed := func(e error) {
		if !s.closed.Load() {
			select {
			case fault <- e:
			default:
			}
		}
	}
	nc, e := natscore.Connect(ctx, natscore.Config(s.config), s.tls, s.password, func(_ *nats.Conn, sub *nats.Subscription, e error) {
		if errors.Is(e, nats.ErrPermissionViolation) && (sub != nil && sub.Subject == gamesignal.PlanetSectorSubject || strings.Contains(e.Error(), `Subscription to "`+gamesignal.PlanetSectorSubject+`"`)) {
			s.spatialSubscriptionFailures.Add(1)
			return
		}
		failed(e)
	}, func(_ *nats.Conn) { failed(nats.ErrConnectionClosed) })
	if e != nil {
		return e
	}
	defer nc.Close()
	sub, e := nc.Subscribe(ChatOutboxSubject, s.accept)
	if e != nil {
		return e
	}
	defer sub.Unsubscribe()
	planetSub, e := nc.Subscribe(gamesignal.PlanetSectorSubject, s.accept)
	if e != nil {
		return e
	}
	defer planetSub.Unsubscribe()
	if e = planetSub.SetPendingLimits(64, 64*maxNATSSignal); e != nil {
		return e
	}

	if e = sub.SetPendingLimits(64, 64*maxNATSSignal); e != nil {
		return e
	}
	check, cancel := context.WithTimeout(ctx, s.config.ConnectTimeout)
	e = nc.FlushWithContext(check)
	cancel()
	if e != nil {
		return e
	}
	select {
	case e = <-fault:
		return e
	default:
	}
	if !nc.IsConnected() {
		return nats.ErrConnectionClosed
	}
	s.healthy.Store(true)
	s.lastError.Store("")
	s.registrations.Add(1)
	s.reconcile.Store(true)
	s.wake()
	if first != nil {
		first <- nil
	}
	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case e := <-fault:
			return e
		case <-s.publishes:
			if !s.Healthy() || !nc.IsConnected() {
				s.dropped.Add(1)
				continue
			}
			if e = nc.Publish(ChatOutboxSubject, newChatSignal()); e != nil {
				s.dropped.Add(1)
				return e
			}
			// Successful Publish is only a client-buffer result, not a receipt.
			// The dispatcher wakes exclusively on the subscriber's validated MSG.
			s.published.Add(1)
		}
	}
}
func (s *NATSWakeSource) run(ctx context.Context, first chan error) {
	defer close(s.done)
	defer s.healthy.Store(false)
	for {
		e := s.session(ctx, first)
		// A successful registration already filled first; a failure must also
		// unblock Start. Do not reuse the first-result channel on reconnect.
		if first != nil {
			select {
			case first <- e:
			default:
			}
			first = nil
		}
		s.healthy.Store(false)
		if ctx.Err() != nil {
			return
		}
		code := natsErrorCode(e)
		s.lastError.Store(code)
		switch code {
		case "tls_verification_failed":
			s.tlsFailures.Add(1)
		case "authentication_failed":
			s.authFailures.Add(1)
		case "authorization_failed":
			s.permissionFailures.Add(1)
		default:
			s.networkFailures.Add(1)
		}
		s.disconnects.Add(1)
		s.wake()
		// Drop queued hints rather than replaying a stale, unbounded backlog.
		select {
		case <-s.publishes:
			s.dropped.Add(1)
		default:
		}
		delay := s.config.ReconnectWait + time.Duration(rand.Int64N(int64(s.config.ReconnectWait/5)+1))
		timer := time.NewTimer(delay)
		select {
		case <-ctx.Done():
			timer.Stop()
			return
		case <-timer.C:
		}
	}
}
func (s *NATSWakeSource) Close(ctx context.Context) error {
	s.mu.Lock()
	s.closed.Store(true)
	s.healthy.Store(false)
	if s.cancel != nil {
		s.cancel()
	}
	done := s.done
	s.mu.Unlock()
	if done == nil {
		return nil
	}
	select {
	case <-done:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}
func (s *NATSWakeSource) Diagnostics() map[string]any {
	return map[string]any{"healthy": s.Healthy(), "last_error_code": s.lastError.Load(), "connect_attempts": s.attempts.Load(), "registrations": s.registrations.Load(), "disconnects": s.disconnects.Load(), "published_hints": s.published.Load(), "received_signals": s.received.Load(), "invalid_signals": s.invalid.Load(), "duplicate_signals": s.duplicates.Load(), "coalesced_publishes": s.coalesced.Load(), "dropped_hints": s.dropped.Load(), "tls_failures": s.tlsFailures.Load(), "authentication_failures": s.authFailures.Load(), "authorization_failures": s.permissionFailures.Load(), "network_failures": s.networkFailures.Load(), "spatial_signals": s.spatialReceived.Load(), "spatial_dropped": s.spatialDropped.Load(), "spatial_subscription_failures": s.spatialSubscriptionFailures.Load()}
}

func (s *NATSWakeSource) duplicate(id string) bool {
	if id != "" {
		s.seenMu.Lock()
		if s.seen[id] {
			s.seenMu.Unlock()
			s.duplicates.Add(1)
			return true
		}
		if len(s.ring) < 1024 {
			s.ring = append(s.ring, id)
		} else {
			delete(s.seen, s.ring[s.ringIndex])
			s.ring[s.ringIndex] = id
			s.ringIndex = (s.ringIndex + 1) % len(s.ring)
		}
		s.seen[id] = true
		s.seenMu.Unlock()
	}
	return false
}

func chatSignalKey(id string) string {
	if id == "" {
		return ""
	}
	return "chat:" + id
}
