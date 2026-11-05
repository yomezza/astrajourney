package chatserver

import (
	"github.com/yomezza/cosmosimulator/internal/platform/httpkit"
	"net/http"
)

// Admission limits bound waiting SQL/HTTP work without changing game quotas.
func bounded(next http.Handler) http.Handler {
	slots := make(chan struct{}, 64)
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/chat/ws/v1" || r.URL.Path == "/chat/api/v1/health" {
			next.ServeHTTP(w, r)
			return
		}
		select {
		case slots <- struct{}{}:
			defer func() { <-slots }()
			next.ServeHTTP(w, r)
		default:
			httpkit.Fail(w, 429, "server_busy")
		}
	})
}
