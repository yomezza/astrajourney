package server

import (
	"context"
	"errors"
	"fmt"
	"log"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/yomezza/cosmosimulator/internal/initialization"
	"github.com/yomezza/cosmosimulator/internal/platform/configuration"
	"github.com/yomezza/cosmosimulator/internal/platform/httpkit"
	"github.com/yomezza/cosmosimulator/internal/platform/natscore"
)

func Run(ctx context.Context) error {
	if err := configuration.Initialize(configuration.Cosmos); err != nil {
		return err
	}
	natsConfig, err := natscore.LoadConfig(configuration.Getenv)
	if err != nil {
		return err
	}
	if err = natsConfig.Validate("cosmos", false); err != nil {
		return err
	}
	// Fail fast on invalid local secret/CA material; remote outages are optional.
	if natsConfig.Enabled {
		if _, _, err = natsConfig.Credentials(); err != nil {
			return err
		}
	}
	dsn := configuration.Getenv("DATABASE_URL")
	if dsn == "" {
		return errors.New("configure database.url in TOML or DATABASE_URL with a PostgreSQL connection string")
	}
	startupTimeout := 5 * time.Minute
	if value := configuration.Getenv("COSMOS_STARTUP_TIMEOUT"); value != "" {
		startupTimeout, err = time.ParseDuration(value)
		if err != nil || startupTimeout <= 0 {
			return errors.New("cosmos.startup_timeout / COSMOS_STARTUP_TIMEOUT must be a positive duration")
		}
	}
	check, cancel := context.WithTimeout(ctx, startupTimeout)
	db, err := initialization.OpenDatabase(check, dsn, configuration.Getenv("DB_SCHEMA"), Assets)
	cancel()
	if err != nil {
		return fmt.Errorf("Cosmic database initialization (startup timeout %s): %w", startupTimeout, err)
	}
	defer db.Close()
	proxies, err := httpkit.ParseTrustedProxies(configuration.Getenv("TRUSTED_PROXIES"))
	if err != nil {
		return err
	}
	a := New(db, Config{TrustedProxies: proxies, Secure: configuration.Getenv("COOKIE_SECURE") == "true", Origin: strings.TrimRight(configuration.Getenv("PUBLIC_ORIGIN"), "/")})
	if err = a.ConfigureNATS(natsConfig); err != nil {
		return err
	}
	if err = a.Auth.Bootstrap(ctx); err != nil {
		return err
	}
	addr := configuration.Getenv("HTTP_ADDR")
	if addr == "" {
		addr = "127.0.0.1:8080"
	}
	srv := &http.Server{Addr: addr, Handler: a.Handler(), ReadHeaderTimeout: 5 * time.Second, ReadTimeout: 15 * time.Second, WriteTimeout: 15 * time.Second, IdleTimeout: 60 * time.Second}
	run, cancelRun := context.WithCancel(ctx)
	a.startNATS(run)
	defer func() {
		if a.nats != nil {
			stop, done := context.WithTimeout(context.Background(), time.Second)
			defer done()
			_ = a.nats.Close(stop)
			log.Printf("Cosmic NATS diagnostics: %v", a.NATSDiagnostics())
		}
	}()
	var workers sync.WaitGroup
	if a.nats != nil {
		workers.Add(1)
		go func() {
			defer workers.Done()
			tick := time.NewTicker(30 * time.Second)
			defer tick.Stop()
			for {
				select {
				case <-run.Done():
					return
				case <-tick.C:
					log.Printf("Cosmic NATS diagnostics: %v", a.NATSDiagnostics())
				}
			}
		}()
	}
	workers.Add(5)
	go func() { defer workers.Done(); a.Simulation.Simulate(run) }()
	go func() { defer workers.Done(); a.Colonies.RunLandingWorker(run) }()
	go func() { defer workers.Done(); a.runTransfers(run) }()
	go func() { defer workers.Done(); a.Corporations.RunChatCleanup(run) }()
	go func() { defer workers.Done(); a.Knowledge.Run(run) }()
	defer func() { cancelRun(); workers.Wait() }()
	go func() {
		<-run.Done()
		c, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		_ = srv.Shutdown(c)
	}()
	log.Printf("Astra Journey listening on %s", addr)
	err = srv.ListenAndServe()
	if errors.Is(err, http.ErrServerClosed) {
		return nil
	}
	return err
}
