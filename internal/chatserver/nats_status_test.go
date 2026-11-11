package chatserver

import "testing"

func TestNATSStatusDistinguishesUnusedMode(t *testing.T) {
	a := &App{config: Config{Dispatcher: DispatcherConfig{Mode: "legacy"}, NATS: NATSConfig{Enabled: true}}}
	if s := a.NATSStatus(); s.State != "not_used" || s.Mode != "legacy" || s.Connected {
		t.Fatal(s)
	}
	a.config.NATS.Enabled = false
	if s := a.NATSStatus(); s.State != "disabled" {
		t.Fatal(s)
	}
}
