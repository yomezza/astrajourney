// Package server composes the cosmic application. Domain packages never import it.
package server

import (
	"context"
	"github.com/yomezza/cosmosimulator/internal/planet/transfer"
	"net/http"
	"net/netip"
	"os"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/yomezza/cosmosimulator/internal/account"
	"github.com/yomezza/cosmosimulator/internal/administration"
	"github.com/yomezza/cosmosimulator/internal/benchmark"
	"github.com/yomezza/cosmosimulator/internal/colonies"
	"github.com/yomezza/cosmosimulator/internal/commerce"
	"github.com/yomezza/cosmosimulator/internal/corporations"
	"github.com/yomezza/cosmosimulator/internal/ideas"
	"github.com/yomezza/cosmosimulator/internal/initialization"
	"github.com/yomezza/cosmosimulator/internal/logistics"
	"github.com/yomezza/cosmosimulator/internal/navigation"
	"github.com/yomezza/cosmosimulator/internal/notifications"
	"github.com/yomezza/cosmosimulator/internal/planner"
	"github.com/yomezza/cosmosimulator/internal/simulation"
	"github.com/yomezza/cosmosimulator/internal/terminal"
	"github.com/yomezza/cosmosimulator/internal/worldstore"
)

const Version = "0.24.2"

type Config struct {
	Secure         bool
	Origin         string
	TrustedProxies []netip.Prefix
}

// App is the composition root, not a shared domain service. New does not run
// migrations, seed data, register a second writer or start simulation goroutines.
type App struct {
	Transfer       *transfer.Service
	DB             *pgxpool.Pool
	Auth           *account.Service
	World          *worldstore.Service
	Commerce       *commerce.Service
	Corporations   *corporations.Service
	Navigation     *navigation.Service
	Logistics      *logistics.Service
	Planner        *planner.Service
	Colonies       *colonies.Service
	Simulation     *simulation.Service
	Administration *administration.Service
	Terminal       *terminal.Service
	Notifications  *notifications.Service
	Ideas          *ideas.Service
	Benchmark      *benchmark.Service
	startedAt      time.Time
	origin         string
}

func New(db *pgxpool.Pool, config Config) *App {
	a := &App{DB: db, origin: config.Origin, startedAt: time.Now()}
	a.Transfer = &transfer.Service{DB: db, Enabled: os.Getenv("PLANET_ENABLED") == "true"}
	a.Auth = &account.Service{DB: db, Secure: config.Secure, TrustedProxies: config.TrustedProxies}
	a.World = &worldstore.Service{DB: db, Auth: a.Auth}
	a.Commerce = &commerce.Service{DB: db, Auth: a.Auth, World: a.World}
	a.Corporations = &corporations.Service{DB: db, Auth: a.Auth, World: a.World}
	a.Navigation = &navigation.Service{DB: db, Auth: a.Auth, World: a.World}
	a.Logistics = &logistics.Service{DB: db, Auth: a.Auth, World: a.World}
	a.Planner = &planner.Service{DB: db, Auth: a.Auth, World: a.World}
	a.Colonies = &colonies.Service{DB: db, Auth: a.Auth, World: a.World}
	a.Simulation = &simulation.Service{DB: db, World: a.World, Navigation: a.Navigation, Commerce: a.Commerce, Planner: a.Planner}
	a.Administration = &administration.Service{DB: db, Auth: a.Auth, Simulation: a.Simulation}
	a.Terminal = &terminal.Service{DB: db, Auth: a.Auth, World: a.World, Navigation: a.Navigation, Commerce: a.Commerce, Corporations: a.Corporations}
	a.Notifications = &notifications.Service{DB: db, Auth: a.Auth}
	a.Ideas = &ideas.Service{DB: db, Auth: a.Auth}
	a.Benchmark = &benchmark.Service{DB: db, Auth: a.Auth,
		OpenDatabase: func(ctx context.Context, dsn, schema string) (*pgxpool.Pool, error) {
			return initialization.OpenDatabase(ctx, dsn, schema, Assets)
		},
		Handler: func(ctx context.Context, pool *pgxpool.Pool) (http.Handler, error) {
			testApp := New(pool, Config{})
			world, err := testApp.World.Load(ctx)
			if err != nil {
				return nil, err
			}
			if err = testApp.World.SeedStations(ctx, world); err != nil {
				return nil, err
			}
			return testApp.Handler(), nil
		},
	}
	return a
}
