package terminal

import (
	"context"
	"net/http"
	"time"

	"github.com/yomezza/cosmosimulator/internal/fleet"
	"github.com/yomezza/cosmosimulator/internal/platform/httpkit"
	"github.com/yomezza/cosmosimulator/internal/platform/postgres"
)

func (a *Service) Fleet(w http.ResponseWriter, r *http.Request) {
	u, ok := a.Auth.User(w, r)
	if !ok {
		return
	}
	tx, err := a.DB.Begin(r.Context())
	if err != nil {
		httpkit.Internal(w, err)
		return
	}
	defer tx.Rollback(context.Background())
	var id int64
	var level int
	if err = tx.QueryRow(r.Context(), `SELECT id,level FROM users WHERE id=$1 FOR UPDATE`, u.ID).Scan(&id, &level); err != nil {
		httpkit.Internal(w, err)
		return
	}
	_, err = fleet.EnsureShip(r.Context(), tx, u.ID)
	if err != nil {
		httpkit.Internal(w, err)
		return
	}
	if err = tx.Commit(r.Context()); err != nil {
		httpkit.Internal(w, err)
		return
	}
	ships, err := fleet.ReadShips(r.Context(), a.DB, u.ID)
	if err != nil {
		httpkit.Internal(w, err)
		return
	}
	public, err := postgres.QueryMaps(r.Context(), a.DB, `SELECT s.id,s.name,s.star,s.destination,s.arrival,s.departure,s.travel_path AS path,s.object_id,s.position_mode,s.local_target,s.local_started,s.local_arrival,u.id AS owner,u.username AS owner_name FROM ships s JOIN users u ON u.id=s.user_id WHERE s.user_id<>$1 AND NOT u.is_bot ORDER BY s.id`, u.ID)
	if err != nil {
		httpkit.Internal(w, err)
		return
	}
	httpkit.Reply(w, 200, map[string]any{"ships": ships, "public_ships": public, "ship_limit": fleet.FleetLimit(level), "server_time": time.Now()})
}
