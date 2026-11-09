package terminal

import (
	"net/http"
	"time"

	"github.com/yomezza/cosmosimulator/internal/account"
	"github.com/yomezza/cosmosimulator/internal/gameplay"
	"github.com/yomezza/cosmosimulator/internal/platform/configuration"
	"github.com/yomezza/cosmosimulator/internal/platform/httpkit"
)

func (a *Service) State(w http.ResponseWriter, r *http.Request) {
	u, ok := a.Auth.User(w, r)
	if !ok {
		return
	}
	var tick int64
	var last time.Time
	if err := a.DB.QueryRow(r.Context(), `SELECT tick,last_tick FROM world WHERE id=1`).Scan(&tick, &last); err != nil {
		httpkit.Internal(w, err)
		return
	}
	rows, err := a.DB.Query(r.Context(), `SELECT id,username,role,is_bot,credits,ore FROM users ORDER BY credits DESC,id LIMIT 20`)
	if err != nil {
		httpkit.Internal(w, err)
		return
	}
	defer rows.Close()
	list := []account.User{}
	for rows.Next() {
		var p account.User
		if err = rows.Scan(&p.ID, &p.Username, &p.Role, &p.Bot, &p.Credits, &p.Ore); err != nil {
			httpkit.Internal(w, err)
			return
		}
		list = append(list, p)
	}
	if rows.Err() != nil {
		httpkit.Internal(w, rows.Err())
		return
	}
	settings, err := gameplay.ReadGameplay(r.Context(), a.DB)
	if err != nil {
		httpkit.Internal(w, err)
		return
	}
	var pulses int64
	if err = a.DB.QueryRow(r.Context(), `SELECT pulses FROM world WHERE id=1`).Scan(&pulses); err != nil {
		httpkit.Internal(w, err)
		return
	}
	httpkit.Reply(w, 200, map[string]any{"chat_enabled": configuration.Getenv("CHAT_ENABLED") == "true", "date": gameplay.EdemDate(pulses), "pulses": pulses, "tick_seconds": settings.TickSeconds, "user": u, "tick": tick, "last_tick": last, "companies": list, "ore_price": 5})
}
