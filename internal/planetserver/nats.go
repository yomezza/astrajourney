package planetserver

import (
	"context"
	"crypto/tls"
	"math/rand/v2"
	"sync/atomic"
	"time"

	"github.com/nats-io/nats.go"
	"github.com/yomezza/cosmosimulator/internal/platform/gamesignal"
	"github.com/yomezza/cosmosimulator/internal/platform/natscore"
)

// Only confirmed transitions can call enqueue; publishing never affects their
// result. No subscription, SQL, movement or permissions belong to this worker.
type planetPublisher struct {
	config                                              natscore.Config
	tls                                                 *tls.Config
	password                                            string
	queue                                               chan []byte
	cancel                                              context.CancelFunc
	done                                                chan struct{}
	healthy, closed                                     atomic.Bool
	attempts, connections, published, dropped, failures atomic.Uint64
	lastError                                           atomic.Value
}

func newPlanetPublisher(c natscore.Config) (*planetPublisher, error) {
	if !c.Enabled {
		return nil, nil
	}
	if e := c.Validate("planet", false); e != nil {
		return nil, e
	}
	t, password, e := c.Credentials()
	if e != nil {
		return nil, e
	}
	p := &planetPublisher{config: c, tls: t, password: password, queue: make(chan []byte, 64), done: make(chan struct{})}
	p.lastError.Store("")
	return p, nil
}
func (p *planetPublisher) enqueue(d gamesignal.SectorChange) {
	b, ok := gamesignal.EncodePlanet(d)
	if !ok || !p.healthy.Load() || p.closed.Load() {
		p.dropped.Add(1)
		return
	}
	select {
	case p.queue <- b:
	default:
		p.dropped.Add(1)
	}
}
func (p *planetPublisher) start(ctx context.Context) {
	run, cancel := context.WithCancel(ctx)
	p.cancel = cancel
	go p.run(run)
}
func (p *planetPublisher) session(ctx context.Context) error {
	p.attempts.Add(1)
	fault := make(chan error, 1)
	failed := func(e error) {
		select {
		case fault <- e:
		default:
		}
	}
	nc, e := natscore.Connect(ctx, p.config, p.tls, p.password, func(_ *nats.Conn, _ *nats.Subscription, e error) { failed(e) }, func(*nats.Conn) { failed(nats.ErrConnectionClosed) })
	if e != nil {
		return e
	}
	defer nc.Close()
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
			if e = nc.Publish(gamesignal.PlanetSectorSubject, data); e != nil {
				p.dropped.Add(1)
				return e
			}
			p.published.Add(1)
		}
	}
}
func (p *planetPublisher) run(ctx context.Context) {
	defer close(p.done)
	defer p.healthy.Store(false)
	for {
		e := p.session(ctx)
		p.healthy.Store(false)
		if ctx.Err() != nil {
			return
		}
		p.failures.Add(1)
		p.lastError.Store(natscore.ErrorCode(e))
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
func (p *planetPublisher) close(ctx context.Context) error {
	p.closed.Store(true)
	p.healthy.Store(false)
	p.cancel()
	select {
	case <-p.done:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

// NATSDiagnostics is a Go/log probe; existing HTTP/WS contracts are unchanged.
func (a *App) NATSDiagnostics() map[string]any {
	if a.publisher == nil {
		return map[string]any{"enabled": false}
	}
	p := a.publisher
	return map[string]any{"enabled": true, "healthy": p.healthy.Load(), "connect_attempts": p.attempts.Load(), "registrations": p.connections.Load(), "published_hints": p.published.Load(), "dropped_hints": p.dropped.Load(), "transport_failures": p.failures.Load(), "last_error_code": p.lastError.Load()}
}
