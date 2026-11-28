package terminal

import (
	"context"
	"errors"
	"net/http"
	"strconv"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/yomezza/cosmosimulator/internal/account"
	"github.com/yomezza/cosmosimulator/internal/fleet"
	"github.com/yomezza/cosmosimulator/internal/gameplay"
	"github.com/yomezza/cosmosimulator/internal/navigation"
	planetaccess "github.com/yomezza/cosmosimulator/internal/planet/access"
	"github.com/yomezza/cosmosimulator/internal/platform/configuration"
	"github.com/yomezza/cosmosimulator/internal/platform/httpkit"
	"github.com/yomezza/cosmosimulator/internal/universe"
)

type studioActivity struct {
	Mining     bool `json:"mining"`
	Research   bool `json:"research"`
	Delivery   bool `json:"delivery"`
	Automation bool `json:"automation"`
	Parked     bool `json:"parked"`
}

func readStudioActivity(ctx context.Context, tx pgx.Tx, ship int64) (studioActivity, error) {
	var a studioActivity
	err := tx.QueryRow(ctx, `SELECT
		EXISTS(SELECT 1 FROM mining_jobs WHERE ship_id=$1),
		EXISTS(SELECT 1 FROM research_jobs WHERE ship_id=$1 AND NOT paused),
		EXISTS(SELECT 1 FROM deliveries WHERE ship_id=$1 AND status='carrying'),
		EXISTS(SELECT 1 FROM automation_tasks WHERE ship_id=$1 AND status='running'),
		EXISTS(SELECT 1 FROM planet_transfer_controls WHERE reserved_ship=$1)`, ship).
		Scan(&a.Mining, &a.Research, &a.Delivery, &a.Automation, &a.Parked)
	return a, err
}

type capability struct {
	Available bool   `json:"available"`
	Reason    string `json:"reason,omitempty"`
}

func (a studioActivity) operationProblem() string {
	if a.Parked {
		return fleet.ErrReserved.Error()
	}
	if a.Automation {
		return "Приостановите задачу планировщика"
	}
	return ""
}

func (a studioActivity) idleProblem(ship fleet.Ship) string {
	if problem := a.operationProblem(); problem != "" {
		return problem
	}
	if ship.Destination != nil || ship.LocalTarget != nil {
		return "Корабль в пути"
	}
	if a.Mining || a.Research || a.Delivery {
		return "Сначала завершите или приостановите текущее задание"
	}
	return ""
}

func availability(reason string) capability {
	return capability{Available: reason == "", Reason: reason}
}

type fittingItem struct {
	ID      int64      `json:"id"`
	Install capability `json:"install"`
	Remove  capability `json:"remove"`
	Sell    capability `json:"sell"`
}

type fittingView struct {
	Available bool                `json:"available"`
	Reason    string              `json:"reason,omitempty"`
	Free      int                 `json:"free"`
	Items     []fittingItem       `json:"items"`
	Offers    []fleet.ModuleOffer `json:"offers"`
}

func shipFitting(ctx context.Context, tx pgx.Tx, s fleet.Ship, a studioActivity) (fittingView, error) {
	out := fittingView{Free: max(0, s.ModuleLimit-s.ModuleUsed()), Items: []fittingItem{}, Offers: []fleet.ModuleOffer{}}
	out.Reason = a.idleProblem(s)
	if out.Reason == "" && (len(s.Waypoints) > 0 || s.ReservedFuel > 0) {
		out.Reason = "Сначала отмените маршрут"
	}
	if !fleet.Docked(s) {
		if out.Reason == "" {
			out.Reason = "Для оснащения и торговли модулями стыкуйтесь со станцией"
		}
	} else {
		var station bool
		err := tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM stations WHERE star=$1 AND object_id=$2 AND active)`, s.Star, s.ObjectID).Scan(&station)
		if err != nil {
			return out, err
		}
		if station {
			out.Offers, err = fleet.ModuleOffers(ctx, tx, s.Star, s.ObjectID)
			if err != nil {
				return out, err
			}
		} else if out.Reason == "" {
			out.Reason = "Станция недоступна"
		}
	}
	out.Available = out.Reason == ""
	for i, module := range s.Modules {
		item := fittingItem{ID: module.ID, Install: availability(out.Reason), Remove: availability(out.Reason), Sell: availability(out.Reason)}
		if out.Available {
			if err := s.CheckFitting(i, true); err != nil {
				item.Install = availability(err.Error())
			}
			if err := s.CheckFitting(i, false); err != nil {
				item.Remove = availability(err.Error())
			}
			if module.Installed {
				item.Sell = availability("Сначала снимите модуль")
			}
		}
		out.Items = append(out.Items, item)
	}
	return out, nil
}

func (a *Service) StudioShip(w http.ResponseWriter, r *http.Request)       { a.studioRead(w, r, false) }
func (a *Service) StudioNavigation(w http.ResponseWriter, r *http.Request) { a.studioRead(w, r, true) }

func (a *Service) studioRead(w http.ResponseWriter, r *http.Request, nav bool) {
	u, ok := a.Auth.User(w, r)
	if !ok {
		return
	}
	sid, ok := studioShipID(w, r)
	if !ok {
		return
	}
	ctx := r.Context()
	tx, err := a.DB.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.RepeatableRead, AccessMode: pgx.ReadOnly})
	if err != nil {
		httpkit.Internal(w, err)
		return
	}
	defer tx.Rollback(context.Background())
	s, err := fleet.ReadOwnedShip(ctx, tx, u.ID, sid)
	if err != nil {
		studioError(w, err)
		return
	}
	activity, err := readStudioActivity(ctx, tx, s.ID)
	if err != nil {
		httpkit.Internal(w, err)
		return
	}
	var out any
	if nav {
		out, err = a.navigationView(ctx, tx, u, s, activity)
	} else {
		out, err = shipView(ctx, tx, s, activity)
	}
	if err != nil {
		studioError(w, err)
		return
	}
	httpkit.Reply(w, 200, out)
}

func shipView(ctx context.Context, tx pgx.Tx, s fleet.Ship, activity studioActivity) (any, error) {
	base, _ := fleet.Hull(s.Class)
	fitting, err := shipFitting(ctx, tx, s, activity)
	if err != nil {
		return nil, err
	}
	refuelProblem := activity.operationProblem()
	if refuelProblem == "" {
		if s.Destination != nil || s.LocalTarget != nil {
			refuelProblem = "Корабль в пути"
		} else if _, err := refuelAmount(s); err != nil {
			refuelProblem = err.Error()
		}
	}
	scoopProblem := activity.idleProblem(s)
	if scoopProblem == "" {
		switch {
		case len(s.Waypoints) > 0 || s.ReservedFuel > 0:
			scoopProblem = "Сначала отмените маршрут"
		case s.ModuleEffect("scoop").Scoop <= 0:
			scoopProblem = "Установите топливозаборник"
		case fleet.Docked(s):
			scoopProblem = "Отстыкуйтесь: топливозаборник работает в открытом космосе"
		case s.Fuel >= s.Hull().Fuel:
			scoopProblem = "Топливный бак заполнен"
		default:
			var ready bool
			err := tx.QueryRow(ctx, `SELECT scoop_ready_at<=clock_timestamp() FROM ships WHERE id=$1`, s.ID).Scan(&ready)
			if err != nil {
				return nil, err
			}
			if !ready {
				scoopProblem = "Топливозаборник остывает: цикл раз в 10 секунд"
			}
		}
	}
	return map[string]any{"ship": s, "base": base, "effective": s.Spec, "docked": fleet.Docked(s), "cargo_used": s.CargoUsed(),
		"module_used": s.ModuleUsed(), "module_limit": s.ModuleLimit, "activity": activity, "fitting": fitting,
		"capabilities": map[string]capability{"rename_ship": availability(""), "refuel": availability(refuelProblem), "module_scoop": availability(scoopProblem)}}, nil
}

type studioSystem struct {
	ID   int     `json:"id"`
	Name string  `json:"name"`
	X    float64 `json:"x"`
	Y    float64 `json:"y"`
}

type studioSite struct {
	ID    string   `json:"id"`
	Name  string   `json:"name"`
	Kind  string   `json:"kind"`
	Orbit float64  `json:"orbit"`
	Modes []string `json:"modes"`
}

type studioBookmark struct {
	Star     int    `json:"star"`
	Target   string `json:"target"`
	Note     string `json:"note"`
	Label    string `json:"label"`
	Bookmark bool   `json:"bookmark"`
}

type studioLandingSite struct {
	ID     string `json:"id"`
	Sector int    `json:"sector"`
	Name   string `json:"name"`
}

func studioLandingSites(ctx context.Context, tx pgx.Tx, uid int64, s fleet.Ship) ([]studioLandingSite, error) {
	out := []studioLandingSite{}
	if s.Destination != nil || s.LocalTarget != nil || len(s.Waypoints) > 0 {
		return out, nil
	}
	rows, err := tx.Query(ctx, `SELECT l.id FROM planet_landing_sites l JOIN planet_surfaces p ON p.id=l.surface
		WHERE p.star=$1 AND 'planet-'||p.planet=$2 AND l.enabled ORDER BY l.id`, s.Star, s.ObjectID)
	if err != nil {
		return nil, err
	}
	ids, err := pgx.CollectRows(rows, pgx.RowTo[int64])
	if err != nil {
		return nil, err
	}
	for _, id := range ids {
		site, err := planetaccess.Landing(ctx, tx, uid, id, s.ID)
		if errors.Is(err, planetaccess.ErrDenied) {
			continue
		}
		if err != nil {
			return nil, err
		}
		out = append(out, studioLandingSite{strconv.FormatInt(site.ID, 10), site.Sector, site.Name})
	}
	return out, nil
}

func (a *Service) navigationView(ctx context.Context, tx pgx.Tx, u account.User, s fleet.Ship, activity studioActivity) (any, error) {
	g, err := a.World.LoadReadOnly(ctx, tx)
	if err != nil {
		return nil, err
	}
	if s.Star < 1 || s.Star > len(g.Stars) || g.Stars[s.Star-1].Deleted {
		return nil, actionProblem("Система корабля недоступна")
	}
	star := g.Stars[s.Star-1]
	galaxy := universe.GalaxyKey(star)
	systems := []studioSystem{}
	systemIDs := map[int]bool{}
	for _, candidate := range g.Stars {
		if !candidate.Deleted && universe.GalaxyKey(candidate) == galaxy {
			systems = append(systems, studioSystem{candidate.ID, candidate.Name, candidate.X, candidate.Y})
			systemIDs[candidate.ID] = true
		}
	}
	visible, err := localSites(ctx, tx, u.ID, star)
	if err != nil {
		return nil, err
	}
	sites := []studioSite{}
	visibleIDs := map[string]bool{}
	for _, site := range visible {
		modes := []string{"approach"}
		if site.Kind == "Орбитальная станция" {
			modes = append(modes, "docked")
		}
		if site.Details != nil {
			modes = append(modes, "orbit")
		}
		sites = append(sites, studioSite{site.ID, site.Name, site.Kind, site.Orbit, modes})
		visibleIDs[site.ID] = true
	}
	rows, err := tx.Query(ctx, `SELECT star,object_id,note,label,bookmark FROM navigation_notes WHERE user_id=$1 ORDER BY star,object_id`, u.ID)
	if err != nil {
		return nil, err
	}
	bookmarks, err := pgx.CollectRows(rows, func(row pgx.CollectableRow) (studioBookmark, error) {
		var b studioBookmark
		err := row.Scan(&b.Star, &b.Target, &b.Note, &b.Label, &b.Bookmark)
		return b, err
	})
	if err != nil {
		return nil, err
	}
	allowedBookmarks := []studioBookmark{}
	for _, b := range bookmarks {
		if systemIDs[b.Star] && (b.Target == "" || b.Star == s.Star && visibleIDs[b.Target]) {
			allowedBookmarks = append(allowedBookmarks, b)
		}
	}
	landing, err := studioLandingSites(ctx, tx, u.ID, s)
	if err != nil {
		return nil, err
	}
	settings, err := gameplay.ReadGameplay(ctx, tx)
	if err != nil {
		return nil, err
	}
	remainingShip := s
	if s.Destination != nil {
		remainingShip.Star = *s.Destination
	}
	remaining := navigation.RoutePlan{Path: []int{remainingShip.Star}, Waypoints: []int{}}
	if len(s.Waypoints) > 0 {
		remaining, err = navigation.PlanRoute(g, remainingShip, s.Waypoints)
		if err != nil {
			remaining = navigation.RoutePlan{Path: []int{}, Waypoints: s.Waypoints}
		}
	}
	var eta *time.Time
	valid := err == nil
	if valid && (s.Arrival != nil || len(s.Waypoints) > 0) {
		from := time.Now()
		if s.Arrival != nil && s.Arrival.After(from) {
			from = *s.Arrival
		}
		at := from.Add(time.Duration(remaining.Seconds) * time.Second)
		eta = &at
	}
	landingProblem := activity.idleProblem(s)
	if landingProblem == "" {
		switch {
		case configuration.Getenv("PLANET_ENABLED") != "true":
			landingProblem = "Планетарная служба отключена"
		case len(landing) == 0:
			landingProblem = "Нет доступной посадочной площадки"
		default:
			var ready bool
			err = tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM planet_runtime_leases WHERE id=1 AND owner_id IS NOT NULL AND expires_at>clock_timestamp())
				AND EXISTS(SELECT 1 FROM planet_schema_state WHERE id=1 AND version=10)`).Scan(&ready)
			if err != nil {
				return nil, err
			}
			if !ready {
				landingProblem = "Планетарная служба недоступна"
			}
		}
	}
	moveProblem := activity.operationProblem()
	if moveProblem == "" {
		switch {
		case s.Destination != nil || s.LocalTarget != nil:
			moveProblem = "Корабль в пути"
		case activity.Research:
			moveProblem = "Сначала приостановите исследование"
		case activity.Mining:
			moveProblem = "Дождитесь завершения добычи"
		case s.Fuel-s.ReservedFuel < localMoveFuel:
			moveProblem = "Недостаточно топлива"
		}
	}
	portals := make(map[string]int)
	for _, d := range g.Catalog {
		if !d.Opened || d.ID == galaxy {
			continue
		}
		if target, err := navigation.PortalDestination(g, s.Star, d.ID); err == nil {
			portals[d.ID] = target
		}
	}
	portalProblem := activity.idleProblem(s)
	if portalProblem == "" && len(portals) == 0 {
		if _, err := navigation.PortalDestination(g, s.Star, ""); err != nil {
			portalProblem = err.Error()
		}
	}
	routeProblem := activity.idleProblem(s)
	if routeProblem == "" && len(s.Waypoints) != 0 {
		routeProblem = "Сначала отмените маршрут"
	}
	return map[string]any{"ship": s, "systems": systems, "sites": sites, "galaxies": universe.PublicGalaxies(g), "bookmarks": allowedBookmarks,
		"landing_sites": landing, "rescue_cost": settings.RescueCost, "remaining_route": remaining, "route_valid": valid, "eta": eta,
		"available_fuel": max(0, s.Fuel-s.ReservedFuel), "portal_destinations": portals,
		"capabilities": map[string]capability{"route": availability(routeProblem), "cancel_route": availability(activity.operationProblem()),
			"move": availability(moveProblem), "rescue": availability(activity.idleProblem(s)), "landing": availability(landingProblem), "portal_travel": availability(portalProblem)}}, nil
}
