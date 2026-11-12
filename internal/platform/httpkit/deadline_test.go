package httpkit

import (
	"net/http/httptest"
	"testing"
	"time"
)

func TestGalaxyGenerationDeadlineScope(t *testing.T) {
	for _, tc := range []struct {
		method, path string
		budget       time.Duration
	}{
		{"POST", "/api/admin/galaxies/preview", 2 * time.Minute},
		{"POST", "/api/admin/galaxies/apply", 2 * time.Minute},
		{"GET", "/api/admin/galaxies", 12 * time.Second},
		{"POST", "/api/admin/galaxies/delete", 12 * time.Second},
		{"POST", "/api/expedition", 12 * time.Second},
	} {
		r, cancel := APIDeadline(httptest.NewRequest(tc.method, tc.path, nil))
		deadline, ok := r.Context().Deadline()
		remaining := time.Until(deadline)
		if !ok || remaining > tc.budget || remaining < tc.budget-time.Second {
			t.Fatalf("unexpected deadline for %s %s: %s", tc.method, tc.path, remaining)
		}
		cancel()
	}
}
