package planetserver

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"sync"
	"sync/atomic"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/yomezza/cosmosimulator/internal/planet/movement"
	planetruntime "github.com/yomezza/cosmosimulator/internal/planet/runtime"
	"github.com/yomezza/cosmosimulator/internal/planet/scenestore"
	"github.com/yomezza/cosmosimulator/internal/planet/visits"
	"github.com/yomezza/cosmosimulator/internal/platform/httpkit"
	"github.com/yomezza/cosmosimulator/internal/platform/telemetry"
)

const Version = "0.24.0"
const Stage = "P2-9"

// App wires only planetary components. New does not start goroutines or migrate.
type App struct {
	checkpoints      telemetry.Samples
	checkpointErrors atomic.Uint64
	transport        transportMetrics
	db               *pgxpool.Pool
	visits           *visits.Service
	config           Config
	logger           *slog.Logger
	coordinator      *planetruntime.Coordinator
	store            *scenestore.Store
	scenes           *planetruntime.Registry
	controllers      map[int64]int
	entryGates       map[int64]chan struct{}
	motion           *movement.Hub
	connections      int
	wsMu             sync.Mutex
	wsWG             sync.WaitGroup
	runContext       context.Context
	stopping         atomic.Bool
	started          atomic.Bool
}

func New(db *pgxpool.Pool, config Config, logger *slog.Logger) (*App, error) {
	if db == nil {
		return nil, errors.New("planetary database pool is required")
	}
	if err := config.Validate(); err != nil {
		return nil, err
	}
	if logger == nil {
		logger = slog.Default()
	}
	c, err := planetruntime.NewCoordinator(db, config.LeaseTTL, logger)
	if err != nil {
		return nil, err
	}
	return &App{db: db, controllers: map[int64]int{}, entryGates: map[int64]chan struct{}{}, visits: &visits.Service{DB: db}, config: config, logger: logger, coordinator: c, store: scenestore.New(db)}, nil
}

func (a *App) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /planet/api/v1/health/live", func(w http.ResponseWriter, r *http.Request) {
		if a.stopping.Load() {
			a.health(w, 503, "stopping")
			return
		}
		a.health(w, 200, "live")
	})
	mux.HandleFunc("GET /planet/api/v1/health/ready", func(w http.ResponseWriter, r *http.Request) {
		if a.stopping.Load() {
			a.health(w, 503, "stopping")
			return
		}
		ctx, cancel := context.WithTimeout(r.Context(), a.config.DBTimeout)
		defer cancel()
		reason := a.coordinator.Ready(ctx)
		if reason != "" {
			a.health(w, 503, reason)
			return
		}
		a.health(w, 200, "ready")
	})
	// Authorization and handoff are enforced before activating a controller.
	mux.HandleFunc("GET /planet/ws/v1", a.websocket)
	mux.HandleFunc("GET /planet/api/v1/scene", a.sceneView)
	mux.HandleFunc("GET /planet/api/v1/metrics", a.metricsView)
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Cache-Control", "no-store")
		w.Header().Set("X-Content-Type-Options", "nosniff")
		if origin := r.Header.Get("Origin"); origin != "" {
			allowed := false
			for _, candidate := range a.config.AllowedOrigins {
				if candidate == origin {
					allowed = true
					break
				}
			}
			if !allowed {
				httpkit.Reply(w, 403, map[string]string{"error": "origin_not_allowed"})
				return
			}
		}
		mux.ServeHTTP(w, r)
	})
}

func (a *App) health(w http.ResponseWriter, status int, reason string) {
	lease, _ := a.coordinator.Current()
	httpkit.Reply(w, status, map[string]any{"schema_version": planetruntime.SchemaVersion, "runtime_instance": lease.Owner, "runtime_fence": lease.Fence, "service": "cosmosimulator-planet", "version": Version, "stage": Stage, "status": reason, "gameplay_ready": status == 200 && reason == "ready", "multiplayer_ready": status == 200 && reason == "ready", "landing_ready": status == 200 && reason == "ready"})
}
