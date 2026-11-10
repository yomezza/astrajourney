// Package corporations owns corporation membership, permissions and corporate commands.
package corporations

import (
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/yomezza/cosmosimulator/internal/platform/gamesignal"

	"github.com/yomezza/cosmosimulator/internal/account"
	"github.com/yomezza/cosmosimulator/internal/worldstore"
)

// Service has explicit dependencies; the composition root owns their lifetime.
type Service struct {
	Signals     gamesignal.Emit
	CleanupWake chan struct{}
	DB          *pgxpool.Pool
	Auth        *account.Service
	World       *worldstore.Service
}
