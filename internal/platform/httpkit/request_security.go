package httpkit

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/netip"
	"strings"
	"time"
)

func ParseTrustedProxies(value string) ([]netip.Prefix, error) {
	out := []netip.Prefix{}
	for _, item := range strings.Split(value, ",") {
		item = strings.TrimSpace(item)
		if item == "" {
			continue
		}
		prefix, err := netip.ParsePrefix(item)
		if err != nil {
			return nil, fmt.Errorf("TRUSTED_PROXIES requires comma-separated IP CIDRs: %w", err)
		}
		out = append(out, prefix.Masked())
	}
	return out, nil
}

func jsonBodyLimit(r *http.Request) int64 {
	if r.Method == "POST" && (r.URL.Path == "/api/admin/guide/articles" || r.URL.Path == "/api/admin/guide/preview") {
		return 1024 * 1024
	}
	if r.Method == "POST" && r.URL.Path == "/api/ideas" {
		return 16 * 1024
	}
	return 4096
}

// Read the entire bounded JSON document before invoking any handler. In
// particular, a slow sender must not hold user rows or the world mutation lock.
func BufferJSON(w http.ResponseWriter, r *http.Request) bool {
	data, err := io.ReadAll(http.MaxBytesReader(w, r.Body, jsonBodyLimit(r)))
	if err != nil {
		Fail(w, 400, "Некорректный JSON")
		return false
	}
	if !json.Valid(data) {
		Fail(w, 400, "Некорректный JSON")
		return false
	}
	r.Body = io.NopCloser(bytes.NewReader(data))
	return true
}

// Context deadlines bound waits for pool connections and locks even after the
// request body has already arrived. HTTP write deadlines alone do not do that.
const GalaxyGenerationTimeout = 2 * time.Minute

func GalaxyGenerationRequest(r *http.Request) bool {
	return r.Method == http.MethodPost && (r.URL.Path == "/api/admin/galaxies/preview" || r.URL.Path == "/api/admin/galaxies/apply")
}

func APIDeadline(r *http.Request) (*http.Request, context.CancelFunc) {
	timeout := 12 * time.Second
	if GalaxyGenerationRequest(r) {
		timeout = GalaxyGenerationTimeout
	}
	ctx, cancel := context.WithTimeout(r.Context(), timeout)
	return r.WithContext(ctx), cancel
}
