package server

import (
	"net/http"
	"strings"
	"time"
)

// net/http's absolute ReadTimeout/WriteTimeout can be inherited by hijacked
// connections. Cosmic proxies Planet and Chat WebSockets; those connections
// must not expire simply because an HTTP request took 15 seconds.
func newCosmicHTTPServer(addr string, next http.Handler) *http.Server {
	return &http.Server{
		Addr: addr,
		Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if proxiedWebSocket(r) {
				next.ServeHTTP(w, r)
				return
			}
			// Preserve request-level read/write deadlines for ordinary HTTP.
			// Scene generation may take the Planet server's full 60s limit.
			timeout := 15 * time.Second
			if r.Method == http.MethodGet && r.URL.Path == "/planet/api/v1/scene" {
				timeout = 75 * time.Second
			}
			deadline := time.Now().Add(timeout)
			controller := http.NewResponseController(w)
			_ = controller.SetReadDeadline(deadline)
			_ = controller.SetWriteDeadline(deadline)
			defer func() {
				_ = controller.SetReadDeadline(time.Time{})
				_ = controller.SetWriteDeadline(time.Time{})
			}()
			next.ServeHTTP(w, r)
		}),
		ReadHeaderTimeout: 5 * time.Second,
		IdleTimeout:       60 * time.Second,
	}
}

func proxiedWebSocket(r *http.Request) bool {
	if r.Method != http.MethodGet || !strings.EqualFold(r.Header.Get("Upgrade"), "websocket") {
		return false
	}
	return r.URL.Path == "/planet/ws/v1" || r.URL.Path == "/chat/ws/v1"
}
