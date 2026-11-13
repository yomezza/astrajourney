package planetserver

import (
	"net/http"
	"testing"
	"time"
)

// net/http may leave request deadlines on a hijacked WebSocket connection.
// A fixed 10s timeout used to terminate healthy planetary visits even while
// the connection was actively receiving movement and sector updates.
func TestPlanetHTTPServerDoesNotExpireHijackedWebSockets(t *testing.T) {
	s := newPlanetHTTPServer(http.NewServeMux())
	if s.ReadTimeout != 0 || s.WriteTimeout != 0 {
		t.Fatalf("WS inherits absolute HTTP deadlines: read=%s write=%s", s.ReadTimeout, s.WriteTimeout)
	}
	if s.ReadHeaderTimeout != 5*time.Second || s.MaxHeaderBytes != 16<<10 {
		t.Fatal("HTTP header limits must remain enabled")
	}
	if s.IdleTimeout != time.Minute {
		t.Fatalf("unexpected HTTP keepalive timeout: %s", s.IdleTimeout)
	}
}
