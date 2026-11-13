package planetserver

import (
	"fmt"
	"github.com/yomezza/cosmosimulator/internal/planet/access"
	"github.com/yomezza/cosmosimulator/internal/planet/scenestore"
	"testing"
)

func TestPublicErrorKeepsWrappedAuthorizationCode(t *testing.T) {
	for _, tc := range []struct {
		err  error
		code string
	}{{access.ErrAuth, "auth_required"}, {access.ErrDenied, "access_denied"}, {scenestore.ErrFence, "runtime_fence_lost"}} {
		if got := publicError(fmt.Errorf("visit check stage: %w", tc.err)); got != tc.code {
			t.Fatalf("public code=%q, want %q", got, tc.code)
		}
	}
}
