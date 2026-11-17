package administration

import (
	"context"
	"net/http"
	"time"

	"github.com/yomezza/cosmosimulator/internal/platform/httpkit"
)

// Public aggregate information contains no account names or private activity.
func (a *Service) ServerStatus(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), 2*time.Second)
	defer cancel()
	var players, bots int
	err := a.DB.QueryRow(ctx, `SELECT count(*) FILTER(WHERE NOT is_bot AND virtual_run IS NULL AND last_active>now()-interval '5 minutes'),count(*) FILTER(WHERE is_bot AND virtual_run IS NULL) FROM users`).Scan(&players, &bots)
	if err != nil {
		httpkit.Internal(w, err)
		return
	}
	var version string
	if err = a.DB.QueryRow(ctx, `SELECT version FROM game_release WHERE id=1`).Scan(&version); err != nil {
		httpkit.Internal(w, err)
		return
	}
	w.Header().Set("Cache-Control", "no-store")
	httpkit.Reply(w, 200, map[string]any{"name": "Иггдрасиль", "players": players, "bots": bots, "version": version})
}
