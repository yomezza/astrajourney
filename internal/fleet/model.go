package fleet

import (
	"context"
	"time"

	"github.com/jackc/pgx/v5"
)

type Ship struct {
	Modules      []ShipModule   `json:"modules"`
	ModuleLimit  int            `json:"module_limit"`
	ModulesReady bool           `json:"modules_ready"`
	Class        string         `json:"class"`
	Spec         ShipClass      `json:"spec"`
	Star         int            `json:"star"`
	Destination  *int           `json:"destination"`
	Arrival      *time.Time     `json:"arrival"`
	Fuel         int            `json:"fuel"`
	Cargo        map[string]int `json:"cargo"`
	ID           int64          `json:"id"`
	Name         string         `json:"name"`
	ObjectID     string         `json:"object_id"`
	PositionMode string         `json:"position_mode"`
	Departure    *time.Time     `json:"departure"`
	Path         []int          `json:"path"`
	LocalTarget  *string        `json:"local_target"`
	LocalMode    *string        `json:"local_mode"`
	LocalStarted *time.Time     `json:"local_started"`
	LocalArrival *time.Time     `json:"local_arrival"`
	Extractor    string         `json:"extractor"`
	Mining       MiningStats    `json:"mining"`
	ReservedFuel int            `json:"reserved_fuel"`
	Active       bool           `json:"active"`
	Waypoints    []int          `json:"waypoints"`
}

func CargoSize(c map[string]int) int {
	n := 0
	for _, q := range c {
		n += q
	}
	return n
}

func EnsureShip(ctx context.Context, tx pgx.Tx, id int64) (Ship, error) {
	if _, err := tx.Exec(ctx, `INSERT INTO ships(user_id,active) VALUES($1,TRUE) ON CONFLICT DO NOTHING`, id); err != nil {
		return Ship{}, err
	}
	if err := SettleShips(ctx, tx, id); err != nil {
		return Ship{}, err
	}
	var sid int64
	if err := tx.QueryRow(ctx, `SELECT id FROM ships WHERE user_id=$1 AND active`, id).Scan(&sid); err != nil {
		return Ship{}, err
	}
	return ReadShip(ctx, tx, sid)
}
