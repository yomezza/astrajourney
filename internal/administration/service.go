// Package administration owns administrative HTTP actions and audit views.
package administration

import (
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/yomezza/cosmosimulator/internal/platform/gamesignal"

	"github.com/yomezza/cosmosimulator/internal/account"
	"github.com/yomezza/cosmosimulator/internal/simulation"
)

// Service has explicit dependencies; the composition root owns their lifetime.
type Service struct {
	Signals    gamesignal.Emit
	DB         *pgxpool.Pool
	Auth       *account.Service
	Simulation *simulation.Service
}
