package natscore

import "testing"

func TestStatusSnapshot(t *testing.T) {
	for _, c := range []struct {
		d         map[string]any
		state     string
		connected bool
	}{
		{nil, "disabled", false},
		{map[string]any{"enabled": true}, "connecting", false},
		{map[string]any{"enabled": true, "healthy": true}, "connected", true},
		{map[string]any{"enabled": true, "connect_attempts": uint64(2), "last_error_code": "network_unavailable"}, "disconnected", false},
	} {
		s := StatusFromDiagnostics(c.d)
		if s.State != c.state || s.Connected != c.connected {
			t.Fatalf("wrong status %+v", s)
		}
	}
	if s := StatusFromDiagnostics(map[string]any{"enabled": true, "last_error_code": "secret endpoint/password"}); s.ErrorCode != "" {
		t.Fatal("unfiltered diagnostic exposed")
	}
}
