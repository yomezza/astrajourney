package natscore

import (
	"context"
	"crypto/tls"
	"math/rand/v2"
	"sync"
	"sync/atomic"
	"time"

	"github.com/nats-io/nats.go"
	"github.com/yomezza/cosmosimulator/internal/platform/gamesignal"
)

// SignalClient owns bounded Core metadata transport. SQL and domain authority
// remain in the caller; subscription callbacks must not block or perform SQL.
type SignalClient struct {
	config                                              Config
	tls                                                 *tls.Config
	password                                            string
	queue                                               chan gamesignal.Message
	mu                                                  sync.Mutex
	started                                             bool
	publications, subscriptions                         []string
	accept                                              func(*nats.Msg)
	reconcile                                           func()
	cancel                                              context.CancelFunc
	done                                                chan struct{}
	healthy, closed                                     atomic.Bool
	attempts, connections, published, dropped, failures atomic.Uint64
	lastError                                           atomic.Value
}

func NewSignalClient(c Config, identity string, publications, subscriptions []string, accept func(*nats.Msg), reconcile func()) (*SignalClient, error) {
	if !c.Enabled {
		return nil, nil
	}
	if e := c.Validate(identity, false); e != nil {
		return nil, e
	}
	t, password, e := c.Credentials()
	if e != nil {
		return nil, e
	}
	p := &SignalClient{config: c, tls: t, password: password, publications: append([]string(nil), publications...), subscriptions: append([]string(nil), subscriptions...), accept: accept, reconcile: reconcile, queue: make(chan gamesignal.Message, 64), done: make(chan struct{})}
	p.lastError.Store("")
	return p, nil
}

// Publish performs no network I/O and never reports failure to a committed game operation.
func (p *SignalClient) Publish(m gamesignal.Message) {
	if p == nil {
		return
	}
	allowed := false
	for _, subject := range p.publications {
		if subject == m.Subject {
			allowed = true
			break
		}
	}
	if !allowed || len(m.Data) == 0 || len(m.Data) > gamesignal.MaxPayload || !p.healthy.Load() || p.closed.Load() {
		p.dropped.Add(1)
		return
	}
	m.Data = append([]byte(nil), m.Data...)
	select {
	case p.queue <- m:
	default:
		p.dropped.Add(1)
	}
}
func (p *SignalClient) Healthy() bool { return p != nil && p.healthy.Load() && !p.closed.Load() }
func (p *SignalClient) Start(ctx context.Context) {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.started || p.closed.Load() {
		return
	}
	p.started = true
	run, cancel := context.WithCancel(ctx)
	p.cancel = cancel
	go p.run(run)
}
func (p *SignalClient) session(ctx context.Context) error {
	p.attempts.Add(1)
	fault := make(chan error, 1)
	failed := func(e error) {
		select {
		case fault <- e:
		default:
		}
	}
	nc, e := Connect(ctx, p.config, p.tls, p.password, func(_ *nats.Conn, _ *nats.Subscription, e error) { failed(e) }, func(*nats.Conn) { failed(nats.ErrConnectionClosed) })
	if e != nil {
		return e
	}
	defer nc.Close()
	for _, subject := range p.subscriptions {
		sub, err := nc.Subscribe(subject, p.accept)
		if err != nil {
			return err
		}
		defer sub.Unsubscribe()
		if err = sub.SetPendingLimits(64, 64*gamesignal.MaxPayload); err != nil {
			return err
		}
	}
	check, cancel := context.WithTimeout(ctx, p.config.ConnectTimeout)
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
	p.healthy.Store(true)
	p.lastError.Store("")
	p.connections.Add(1)
	if p.reconcile != nil {
		p.reconcile()
	}
	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case e = <-fault:
			return e
		case data := <-p.queue:
			if !p.healthy.Load() || !nc.IsConnected() {
				p.dropped.Add(1)
				continue
			}
			if e = nc.Publish(data.Subject, data.Data); e != nil {
				p.dropped.Add(1)
				return e
			}
			p.published.Add(1)
		}
	}
}
func (p *SignalClient) run(ctx context.Context) {
	defer close(p.done)
	defer p.healthy.Store(false)
	for {
		e := p.session(ctx)
		p.healthy.Store(false)
		if ctx.Err() != nil {
			return
		}
		p.failures.Add(1)
		p.lastError.Store(ErrorCode(e))
		for {
			select {
			case <-p.queue:
				p.dropped.Add(1)
			default:
				goto drained
			}
		}
	drained:
		delay := p.config.ReconnectWait + time.Duration(rand.Int64N(int64(p.config.ReconnectWait/5)+1))
		timer := time.NewTimer(delay)
		select {
		case <-ctx.Done():
			timer.Stop()
			return
		case <-timer.C:
		}
	}
}
func (p *SignalClient) Close(ctx context.Context) error {
	p.mu.Lock()
	p.closed.Store(true)
	p.healthy.Store(false)
	if !p.started {
		p.mu.Unlock()
		return nil
	}
	p.cancel()
	done := p.done
	p.mu.Unlock()
	select {
	case <-done:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

func (p *SignalClient) Diagnostics() map[string]any {
	if p == nil {
		return map[string]any{"enabled": false}
	}
	return map[string]any{"enabled": true, "healthy": p.Healthy(), "connect_attempts": p.attempts.Load(), "registrations": p.connections.Load(), "published_hints": p.published.Load(), "dropped_hints": p.dropped.Load(), "transport_failures": p.failures.Load(), "last_error_code": p.lastError.Load()}
}
