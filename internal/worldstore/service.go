// Package worldstore owns persisted universe reads, revisions, stations and galaxy administration.
package worldstore

import (
	"sync"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/yomezza/cosmosimulator/internal/account"
	"github.com/yomezza/cosmosimulator/internal/universe"
)

// Service has explicit dependencies; the composition root owns their lifetime.
type Service struct {
	DB              *pgxpool.Pool
	Auth            *account.Service
	universeMu      sync.Mutex
	universeEpoch   uint64
	universeChecked time.Time
	universe        *universe.Galaxy
	stationMu       sync.Mutex
	stationCount    int
}
