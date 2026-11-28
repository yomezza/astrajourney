package terminal

import (
	"context"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/yomezza/cosmosimulator/internal/fleet"
	"github.com/yomezza/cosmosimulator/internal/research"
	"github.com/yomezza/cosmosimulator/internal/universe"
	"github.com/yomezza/cosmosimulator/internal/worldstore"
)

type actionProblem string

func (e actionProblem) Error() string { return string(e) }

const localMoveSeconds = 5
const localMoveFuel = 1

func localSites(ctx context.Context, tx pgx.Tx, uid int64, star universe.Star) ([]universe.Site, error) {
	all, err := worldstore.AddCustomSites(ctx, tx, star, universe.SystemSites(star))
	if err != nil {
		return nil, err
	}
	var surveyed bool
	err = tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM surveys WHERE user_id=$1 AND star=$2)`, uid, star.ID).Scan(&surveyed)
	if err != nil {
		return nil, err
	}
	visible, err := research.RevealedSites(ctx, tx, uid, star.ID, all, surveyed)
	if err != nil {
		return nil, err
	}
	known := make(map[string]bool, len(visible))
	for _, site := range visible {
		known[site.ID] = true
	}
	for _, site := range all {
		if site.Kind == "Орбитальная станция" && !known[site.ID] {
			visible = append(visible, site)
		}
	}
	return visible, nil
}

func planLocalMove(ctx context.Context, tx pgx.Tx, uid int64, ship fleet.Ship, star universe.Star, target, mode string) error {
	if ship.Destination != nil || ship.LocalTarget != nil {
		return actionProblem("Корабль в пути")
	}
	var researching, mining bool
	err := tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM research_jobs WHERE ship_id=$1 AND NOT paused),
		EXISTS(SELECT 1 FROM mining_jobs WHERE ship_id=$1)`, ship.ID).Scan(&researching, &mining)
	if err != nil {
		return err
	}
	if researching {
		return actionProblem("Сначала приостановите исследование")
	}
	if mining {
		return actionProblem("Дождитесь завершения добычи")
	}
	sites, err := localSites(ctx, tx, uid, star)
	if err != nil {
		return err
	}
	for _, site := range sites {
		if site.ID != target {
			continue
		}
		if mode != "approach" && mode != "orbit" && mode != "docked" {
			return actionProblem("Некорректный манёвр")
		}
		if mode == "docked" && site.Kind != "Орбитальная станция" || mode == "orbit" && site.Details == nil {
			return actionProblem("Манёвр недоступен для объекта")
		}
		if ship.Fuel-ship.ReservedFuel < localMoveFuel {
			return actionProblem("Недостаточно топлива")
		}
		if fleet.NearObject(ship, target) && ship.PositionMode == mode {
			return actionProblem("Корабль уже на месте")
		}
		return nil
	}
	return actionProblem("Объект не найден")
}

func startLocalMove(ship *fleet.Ship, target, mode string) {
	now := time.Now()
	arrival := now.Add(localMoveSeconds * time.Second)
	ship.LocalTarget, ship.LocalMode = &target, &mode
	ship.LocalStarted, ship.LocalArrival = &now, &arrival
	ship.Fuel -= localMoveFuel
}

func refuelAmount(ship fleet.Ship) (int, error) {
	amount := min(5, ship.Hull().Fuel-ship.Fuel)
	if amount <= 0 {
		return 0, actionProblem("Бак полон")
	}
	if ship.Cargo["agni"] < 1 {
		return 0, actionProblem("Нужен водород в трюме")
	}
	return amount, nil
}

func refuel(ship *fleet.Ship, amount int) {
	ship.Cargo["agni"]--
	ship.Fuel += amount
}
