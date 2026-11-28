package navigation

import (
	"context"
	"errors"

	"github.com/jackc/pgx/v5"

	"github.com/yomezza/cosmosimulator/internal/fleet"
	"github.com/yomezza/cosmosimulator/internal/gameplay"
	"github.com/yomezza/cosmosimulator/internal/universe"
)

type RoutePlan struct {
	Path      []int `json:"path"`
	Waypoints []int `json:"waypoints"`
	Fuel      int   `json:"fuel"`
	Seconds   int   `json:"seconds"`
}

func PlanRoute(g *universe.Galaxy, ship fleet.Ship, points []int) (RoutePlan, error) {
	plan := RoutePlan{Path: []int{ship.Star}, Waypoints: []int{}}
	if len(points) == 0 || len(points) > 20 {
		return plan, errors.New("Укажите от 1 до 20 точек маршрута")
	}
	from := ship.Star
	for _, to := range points {
		if to == from {
			continue
		}
		path := g.Path(from, to)
		if len(path) < 2 {
			return plan, errors.New("Нет допустимого маршрута")
		}
		plan.Path = append(plan.Path, path[1:]...)
		plan.Waypoints = append(plan.Waypoints, to)
		from = to
	}
	hull := ship.Hull()
	plan.Fuel = (len(plan.Path) - 1) * hull.JumpFuel
	plan.Seconds = (len(plan.Path) - 1) * hull.JumpSeconds
	if plan.Fuel > ship.Fuel {
		return plan, errors.New("Недостаточно топлива")
	}
	return plan, nil
}

const RescueSeconds = 30
const RescueMinimumFuel = 10

type RescuePlan struct {
	Path        []int `json:"path"`
	Target      int   `json:"target"`
	Cost        int64 `json:"cost"`
	Seconds     int   `json:"seconds"`
	MinimumFuel int   `json:"minimum_fuel"`
}

func PlanRescue(ctx context.Context, tx pgx.Tx, g *universe.Galaxy, ship fleet.Ship) (RescuePlan, error) {
	settings, err := gameplay.ReadGameplay(ctx, tx)
	if err != nil {
		return RescuePlan{}, err
	}
	rows, err := tx.Query(ctx, `SELECT DISTINCT star FROM stations WHERE active`)
	if err != nil {
		return RescuePlan{}, err
	}
	stars, err := pgx.CollectRows(rows, pgx.RowTo[int])
	if err != nil {
		return RescuePlan{}, err
	}
	ports := make(map[int]bool, len(stars))
	for _, star := range stars {
		ports[star] = true
	}
	plan := RescuePlan{Cost: int64(settings.RescueCost), Seconds: RescueSeconds, MinimumFuel: RescueMinimumFuel}
	for _, candidate := range g.Stars {
		if !ports[candidate.ID] {
			continue
		}
		p := g.Path(ship.Star, candidate.ID)
		if len(p) > 0 && (plan.Target == 0 || len(p) < len(plan.Path)) {
			plan.Path, plan.Target = p, candidate.ID
			if len(p) == 1 {
				break
			}
		}
	}
	if plan.Target == 0 {
		return plan, errors.New("В этом компоненте карты нет доступного порта")
	}
	return plan, nil
}
