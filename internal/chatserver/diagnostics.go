package chatserver

import "github.com/yomezza/cosmosimulator/internal/platform/telemetry"
import "github.com/yomezza/cosmosimulator/internal/platform/natscore"

// Diagnostics contains no user/session/channel labels. It is used by structured
// process logs and owned test probes, never by the browser chat protocol.
func (a *App) Diagnostics() map[string]any {
	v := map[string]any{"mode": a.config.Dispatcher.Mode, "connections": len(a.registry.Snapshot()), "sync_writes": a.syncWrites.Load(), "queued_sync": a.registry.queued.Load(), "coalesced_sync": a.registry.coalesced.Load(), "service": a.Service.DispatchCounters(), "process": telemetry.Process(a.Service.DB)}
	if a.dispatcher != nil {
		v["dispatcher"] = a.dispatcher.Diagnostics()
	}
	if source, ok := a.source.(interface{ Diagnostics() map[string]any }); ok {
		v["wake_source"] = source.Diagnostics()
	}
	return v
}

func (a *App) NATSStatus() natscore.Status {
	s := natscore.Status{State: "disabled", Enabled: a.config.NATS.Enabled, Mode: a.config.Dispatcher.Mode}
	if !s.Enabled {
		return s
	}
	source, ok := a.source.(*NATSWakeSource)
	if !ok {
		s.State = "not_used"
		return s
	}
	d := source.Diagnostics()
	d["enabled"] = true
	s = natscore.StatusFromDiagnostics(d)
	s.Mode = a.config.Dispatcher.Mode
	return s
}
