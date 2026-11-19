package terminal

import (
	"github.com/yomezza/cosmosimulator/internal/fleet"
	"github.com/yomezza/cosmosimulator/internal/platform/httpkit"
	"github.com/yomezza/cosmosimulator/internal/platform/postgres"
	"github.com/yomezza/cosmosimulator/internal/universe"
	"net/http"
	"strconv"
)

func (a *Service) ShipModules(w http.ResponseWriter, r *http.Request) {
	u, ok := a.Auth.User(w, r)
	if !ok {
		return
	}
	ctx := r.Context()
	sid, err := strconv.ParseInt(r.URL.Query().Get("ship"), 10, 64)
	if err != nil || sid < 1 {
		httpkit.Fail(w, 400, "Укажите корабль")
		return
	}
	var owned bool
	if err = a.DB.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM ships WHERE id=$1 AND user_id=$2)`, sid, u.ID).Scan(&owned); err != nil {
		httpkit.Internal(w, err)
		return
	}
	if !owned {
		httpkit.Fail(w, 404, "Корабль не найден")
		return
	}
	ship, err := fleet.ReadShip(ctx, a.DB, sid)
	if err != nil {
		httpkit.Internal(w, err)
		return
	}
	world, err := a.World.LoadReadOnly(ctx, a.DB)
	if err != nil {
		httpkit.Internal(w, err)
		return
	}
	if ship.Star < 1 || ship.Star > len(world.Stars) {
		httpkit.Fail(w, 409, "Система корабля недоступна")
		return
	}
	galaxy := universe.GalaxyKey(world.Stars[ship.Star-1])
	// Use the selected ship's location for fitting; station browsing is limited to the player's galaxy.
	var playerStar int
	if err = a.DB.QueryRow(ctx, `SELECT star FROM ships WHERE user_id=$1 AND active`, u.ID).Scan(&playerStar); err != nil {
		httpkit.Internal(w, err)
		return
	}
	if playerStar > 0 && playerStar <= len(world.Stars) {
		galaxy = universe.GalaxyKey(world.Stars[playerStar-1])
	}
	stations, err := postgres.QueryMaps(ctx, a.DB, `SELECT star,object_id,name,specialization FROM stations WHERE active ORDER BY star,object_id`)
	if err != nil {
		httpkit.Internal(w, err)
		return
	}
	visible := []map[string]any{}
	for _, p := range stations {
		id := 0
		switch n := p["star"].(type) {
		case int32:
			id = int(n)
		case int64:
			id = int(n)
		}
		if id > 0 && id <= len(world.Stars) && !world.Stars[id-1].Deleted && universe.GalaxyKey(world.Stars[id-1]) == galaxy {
			visible = append(visible, p)
		}
	}
	star := ship.Star
	station := ship.ObjectID
	if value := r.URL.Query().Get("star"); value != "" {
		star, err = strconv.Atoi(value)
		if err != nil {
			httpkit.Fail(w, 400, "Некорректная станция")
			return
		}
		station = r.URL.Query().Get("station")
	}
	available := false
	for _, p := range visible {
		if p["object_id"] == station {
			switch id := p["star"].(type) {
			case int32:
				available = int(id) == star
			case int64:
				available = int(id) == star
			}
			if available {
				break
			}
		}
	}
	offers := []map[string]any{}
	if available {
		if err = fleet.EnsureModuleStock(ctx, a.DB, star, station); err != nil {
			httpkit.Internal(w, err)
			return
		}
		offers, err = postgres.QueryMaps(ctx, a.DB, `SELECT c.id AS catalog_id,c.name,c.kind,c.rating,c.size,c.effects,c.description,o.price,o.quantity FROM station_module_stock o JOIN ship_module_catalog c ON c.id=o.catalog_id WHERE o.star=$1 AND o.station=$2 AND o.enabled AND c.enabled ORDER BY c.kind,c.size,c.rating LIMIT 500`, star, station)
		if err != nil {
			httpkit.Internal(w, err)
			return
		}
	}
	if offers == nil {
		offers = []map[string]any{}
	}
	httpkit.Reply(w, 200, map[string]any{"ship": ship, "used": ship.ModuleUsed(), "cargo_used": ship.CargoUsed(), "docked": fleet.Docked(ship), "stations": visible, "star": star, "station": station, "offers": offers})
}
