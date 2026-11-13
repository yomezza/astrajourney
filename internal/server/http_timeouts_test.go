package server

import (
	"net/http"
	"net/url"
	"testing"
	"time"
)

func TestCosmicHTTPDeadlinesKeepProxiedWebSocketsAlive(t *testing.T) {
	s := newCosmicHTTPServer("127.0.0.1:0", http.NewServeMux())
	if s.ReadTimeout != 0 || s.WriteTimeout != 0 {
		t.Fatalf("upgraded WebSockets inherit absolute HTTP deadlines: read=%s write=%s", s.ReadTimeout, s.WriteTimeout)
	}
	if s.ReadHeaderTimeout != 5*time.Second || s.IdleTimeout != time.Minute {
		t.Fatal("slow HTTP headers and idle HTTP connections must still be bounded")
	}
	for _, path := range []string{"/planet/ws/v1", "/chat/ws/v1"} {
		r := &http.Request{Method: http.MethodGet, URL: &url.URL{Path: path}, Header: make(http.Header)}
		r.Header.Set("Upgrade", "websocket")
		if !proxiedWebSocket(r) {
			t.Errorf("%s was not exempted from the normal 15s deadline", path)
		}
		r.Header.Del("Upgrade")
		if proxiedWebSocket(r) {
			t.Errorf("plain HTTP at %s bypassed the normal deadline", path)
		}
	}
	for _, path := range []string{"/api/state", "/planet/api/v1/scene", "/chat/api/v1/health"} {
		r := &http.Request{Method: http.MethodGet, URL: &url.URL{Path: path}, Header: make(http.Header)}
		r.Header.Set("Upgrade", "websocket")
		if proxiedWebSocket(r) {
			t.Errorf("ordinary endpoint %s bypassed HTTP deadlines", path)
		}
	}
}
