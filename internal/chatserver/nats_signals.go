package chatserver

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"errors"
	"math/rand/v2"
	"net"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/nats-io/nats.go"
)

// NATSWakeSource uses Core only. One supervisor owns each connection and its
// subscription; it automatically reconnects and confirms SUB with PING/PONG.
// No domain operation, SQL ACL, request/reply or JetStream belongs here.
type NATSWakeSource struct {
	config                     NATSConfig
	tls                        *tls.Config
	password                   string
	wakes, publishes           chan struct{}
	mu                         sync.Mutex
	cancel                     context.CancelFunc
	done                       chan struct{}
	closed, healthy, reconcile atomic.Bool

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
func (s *NATSWakeSource) accept(m *nats.Msg) {
	if s.closed.Load() {
		return
	}
	signal, ok := decodeChatSignal(m.Subject, m.Data)
	if !ok || m.Reply != "" {
		s.invalid.Add(1)
		return
	}
	s.received.Add(1)
	if signal.SignalID != "" {
		s.seenMu.Lock()
		if s.seen[signal.SignalID] {
			s.seenMu.Unlock()
			s.duplicates.Add(1)
			return
		}
		if len(s.ring) < 1024 {
			s.ring = append(s.ring, signal.SignalID)
		} else {
			delete(s.seen, s.ring[s.ringIndex])
			s.ring[s.ringIndex] = signal.SignalID
			s.ringIndex = (s.ringIndex + 1) % len(s.ring)
		}
		s.seen[signal.SignalID] = true
		s.seenMu.Unlock()
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

// Cancel the actual socket even during initial INFO/TLS/auth reads, so shutdown
// does not wait for connect_timeout * server count on unreachable/broken peers.
type natsDialer struct {
	ctx     context.Context
	timeout time.Duration
}
type natsSocket struct {
	net.Conn
	stop func() bool
}

func (c *natsSocket) Close() error { c.stop(); return c.Conn.Close() }
func (d natsDialer) Dial(network, address string) (net.Conn, error) {
	c, e := (&net.Dialer{Timeout: d.timeout}).DialContext(d.ctx, network, address)
	if e != nil {
		return nil, e
	}
	return &natsSocket{Conn: c, stop: context.AfterFunc(d.ctx, func() { c.Close() })}, nil
}
func natsErrorCode(e error) string {
	var verify *tls.CertificateVerificationError
	var unknown x509.UnknownAuthorityError
	var host x509.HostnameError
	var invalid x509.CertificateInvalidError
	if errors.As(e, &verify) || errors.As(e, &unknown) || errors.As(e, &host) || errors.As(e, &invalid) || errors.Is(e, nats.ErrTLS) || errors.Is(e, nats.ErrSecureConnRequired) || errors.Is(e, nats.ErrSecureConnWanted) {
		return "tls_verification_failed"
	}
	if errors.Is(e, nats.ErrAuthorization) || errors.Is(e, nats.ErrAuthExpired) || errors.Is(e, nats.ErrAuthRevoked) {
		return "authentication_failed"
	}
	if errors.Is(e, nats.ErrPermissionViolation) {
		return "authorization_failed"
	}
	if errors.Is(e, nats.ErrSlowConsumer) {
		return "slow_consumer"
	}
	return "network_unavailable"
}
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
	nc, e := nats.Connect(strings.Join(s.config.Servers, ","), nats.Secure(s.tls.Clone()), nats.UserInfo(s.config.Username, s.password), nats.NoReconnect(), nats.IgnoreDiscoveredServers(), nats.ReconnectBufSize(0), nats.Timeout(s.config.ConnectTimeout), nats.FlusherTimeout(s.config.ConnectTimeout), nats.PingInterval(5*time.Second), nats.MaxPingsOutstanding(2), nats.SetCustomDialer(natsDialer{ctx, s.config.ConnectTimeout}), nats.Name("cosmos-chat-b1"), nats.ErrorHandler(func(_ *nats.Conn, _ *nats.Subscription, e error) { failed(e) }), nats.ClosedHandler(func(_ *nats.Conn) { failed(nats.ErrConnectionClosed) }))
	if e != nil {
		return e
	}
	defer nc.Close()
	sub, e := nc.Subscribe(ChatOutboxSubject, s.accept)
	if e != nil {
		return e
	}
	defer sub.Unsubscribe()
	if e = sub.SetPendingLimits(64, 64*maxNATSSignal); e != nil {
		return e
	}
	check, cancel := context.WithTimeout(ctx, s.config.ConnectTimeout)
	e = nc.FlushWithContext(check)
	cancel()
	if e != nil {
		return e
	}
	if e = nc.LastError(); e != nil {
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
	return map[string]any{"healthy": s.Healthy(), "last_error_code": s.lastError.Load(), "connect_attempts": s.attempts.Load(), "registrations": s.registrations.Load(), "disconnects": s.disconnects.Load(), "published_hints": s.published.Load(), "received_signals": s.received.Load(), "invalid_signals": s.invalid.Load(), "duplicate_signals": s.duplicates.Load(), "coalesced_publishes": s.coalesced.Load(), "dropped_hints": s.dropped.Load(), "tls_failures": s.tlsFailures.Load(), "authentication_failures": s.authFailures.Load(), "authorization_failures": s.permissionFailures.Load(), "network_failures": s.networkFailures.Load()}
}
