package chatserver

import (
	"github.com/yomezza/cosmosimulator/internal/platform/httpkit"
	"net/http"
	"time"
)

// Admission limits bound waiting SQL/HTTP work without changing game quotas.
func bounded(next http.Handler) http.Handler {
	slots := make(chan struct{}, 64)
	waiting := make(chan struct{}, 64)
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
			// Absorb a bounded burst without expanding active SQL work. The
			// queue and its wait both remain bounded during genuine overload.
			select {
			case waiting <- struct{}{}:
			default:
				w.Header().Set("Retry-After", "1")
				httpkit.Fail(w, 429, "server_busy")
				return
			}
			timer := time.NewTimer(250 * time.Millisecond)
			defer timer.Stop()
			select {
			case slots <- struct{}{}:
				<-waiting
				defer func() { <-slots }()
				next.ServeHTTP(w, r)
			case <-r.Context().Done():
				<-waiting
				return
			case <-timer.C:
				<-waiting
				w.Header().Set("Retry-After", "1")
				httpkit.Fail(w, 429, "server_busy")
			}
		}
	})
}
