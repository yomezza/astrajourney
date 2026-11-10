package server

import (
	"context"
	"sync/atomic"

	"github.com/nats-io/nats.go"
	"github.com/yomezza/cosmosimulator/internal/platform/gamesignal"
	"github.com/yomezza/cosmosimulator/internal/platform/natscore"
)

// ConfigureNATS is called once, before HTTP/workers. It never changes gateway
// addresses or makes readiness depend on the optional broker.
func (a *App) ConfigureNATS(c natscore.Config) error {
	a.Corporations.CleanupWake = make(chan struct{}, 1)
	var invalid atomic.Uint64
	client, e := natscore.NewSignalClient(c, "cosmos", gamesignal.CosmicSubjects, []string{gamesignal.CleanupSubject}, func(m *nats.Msg) {
		_, ok := gamesignal.DecodeCompany(m.Subject, m.Data)
		if !ok || m.Reply != "" {
			invalid.Add(1)
			return
		}
		// No receipt is inferred from the hint. The worker independently joins the
		// durable request + matching receipt and locks companies before settlement.
		a.Corporations.WakeChatCleanup()
	}, a.Corporations.WakeChatCleanup)
	if e != nil {
		return e
	}
	a.nats = client
	a.natsInvalid = &invalid
	if client != nil {
		a.Corporations.Signals = client.Publish
		a.Administration.Signals = client.Publish
	}
	return nil
}
func (a *App) NATSDiagnostics() map[string]any {
	d := a.nats.Diagnostics()
	if a.natsInvalid != nil {
		d["invalid_signals"] = a.natsInvalid.Load()
	}
	return d
}
func (a *App) startNATS(ctx context.Context) {
	if a.nats != nil {
		a.nats.Start(ctx)
	}
}
