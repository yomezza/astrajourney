package httpkit

import (
	"net/http"
	"strconv"
	"strings"
)

// Static world/catalogue responses are large; browsers and standard HTTP clients
// negotiate and decode gzip without a frontend dependency or a second service.
func GzipAPI(next http.Handler) http.Handler {
	compressed := GzipJSON(next)
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasPrefix(r.URL.Path, "/api/") {
			compressed.ServeHTTP(w, r)
			return
		}
		next.ServeHTTP(w, r)
	})
}

func acceptsGzip(header string) bool {
	wildcard := false
	for _, encoding := range strings.Split(header, ",") {
		parts := strings.Split(encoding, ";")
		name := strings.ToLower(strings.TrimSpace(parts[0]))
		if name != "gzip" && name != "*" {
			continue
		}
		quality := 1.0
		for _, p := range parts[1:] {
			key, value, found := strings.Cut(p, "=")
			if found && strings.EqualFold(strings.TrimSpace(key), "q") {
				parsed, err := strconv.ParseFloat(strings.TrimSpace(value), 64)
				if err != nil || parsed < 0 || parsed > 1 {
					quality = 0
				} else {
					quality = parsed
				}
			}
		}
		if name == "gzip" {
			return quality > 0
		}
		wildcard = quality > 0
	}
	return wildcard
}
