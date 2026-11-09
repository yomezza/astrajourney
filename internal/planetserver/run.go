package planetserver

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"net"
	"net/http"
	"os"
	"time"

	"github.com/yomezza/cosmosimulator/internal/platform/configuration"
	"github.com/yomezza/cosmosimulator/internal/platform/postgres"
)

func Run(ctx context.Context) error { return RunStarted(ctx, nil) }

// RunStarted notifies SCM only after the HTTP listener has been bound.
func RunStarted(ctx context.Context, onListening func()) error {
	if err := configuration.Initialize(configuration.Planet); err != nil {
		return err
	}
	config, err := LoadConfig(configuration.Getenv)
	if err != nil {
		return err
	}
	return RunConfig(ctx, config, onListening)
}

func RunConfig(ctx context.Context, config Config, onListening func()) error {
	if ctx.Err() != nil {
		return nil
	}
	if config.DatabaseURL == "" {
		return errors.New("configure database.url in TOML or DATABASE_URL for the planetary service")
	}
	if err := config.Validate(); err != nil {
		return err
	}
	var output io.Writer = os.Stderr
	if config.LogFile != "" {
		f, err := os.OpenFile(config.LogFile, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0600)
		if err != nil {
			return errors.New("cannot open PLANET_LOG_FILE")
		}
		defer f.Close()
		output = f
	}
	logger := slog.New(slog.NewJSONHandler(output, nil))
	check, cancel := context.WithTimeout(ctx, config.DBTimeout)
	db, _, err := postgres.Open(check, config.DatabaseURL, config.Schema)
	cancel()
	if err != nil {
		if ctx.Err() != nil {
			return nil
		}
		return errors.New("planetary PostgreSQL connection failed; check DATABASE_URL, DB_SCHEMA and connectivity")
	}
	defer db.Close()
	app, err := New(db, config, logger)
	if err != nil {
		return err
	}
	listener, err := net.Listen("tcp", config.Addr)
	if err != nil {
		return errors.New("cannot bind PLANET_HTTP_ADDR")
	}
	return app.Serve(ctx, listener, onListening)
}

// Serve owns the listener, but not the caller's database pool. One App serves once.
func (a *App) Serve(ctx context.Context, listener net.Listener, onListening func()) error {
	if ctx.Err() != nil {
		listener.Close()
		return nil
	}
	if !a.started.CompareAndSwap(false, true) {
		listener.Close()
		return errors.New("planetary App already started")
	}
	defer listener.Close()
	run, cancel := context.WithCancel(ctx)
	defer cancel()
	if err := a.newScenes(run); err != nil {
		return err
	}
	step := func() { c, done := context.WithTimeout(run, a.config.DBTimeout); defer done(); a.coordinator.Step(c) }
	step()
	finished := make(chan struct{})
	go func() {
		defer close(finished)
		ticker := time.NewTicker(a.config.LeaseRenew)
		defer ticker.Stop()
		for {
			select {
			case <-run.Done():
				return
			case <-ticker.C:
				step()
			}
		}
	}()
	scenesFinished := make(chan struct{})
	go func() { defer close(scenesFinished); a.maintainScenes(run) }()
	server := &http.Server{Handler: a.Handler(), ReadHeaderTimeout: 5 * time.Second, ReadTimeout: 10 * time.Second, WriteTimeout: 10 * time.Second, IdleTimeout: 60 * time.Second, MaxHeaderBytes: 16 << 10}
	result := make(chan error, 1)
	go func() { result <- server.Serve(listener) }()
	a.logger.Info("planetary HTTP listener started", "address", listener.Addr().String(), "stage", Stage, "version", Version)
	if onListening != nil && ctx.Err() == nil {
		onListening()
	}
	var err error
	select {
	case <-ctx.Done():
	case err = <-result:
	}
	a.wsMu.Lock()
	a.stopping.Store(true)
	a.wsMu.Unlock()
	cancel()
	stop, done := context.WithTimeout(context.Background(), a.config.ShutdownTimeout)
	defer done()
	if e := server.Shutdown(stop); e != nil {
		server.Close()
		a.logger.Warn("planetary HTTP shutdown deadline reached")
	}
	<-finished
	<-scenesFinished
	wsDone := make(chan struct{})
	go func() { a.wsWG.Wait(); close(wsDone) }()
	select {
	case <-wsDone:
	case <-stop.Done():
	}
	a.motion.Close()
	if e := a.scenes.Close(stop); e != nil {
		a.logger.Warn("planetary scene shutdown deadline reached")
	}
	if e := a.coordinator.Release(stop); e != nil {
		a.logger.Warn("planetary lease release failed; lease will expire")
	}
	a.logger.Info("planetary service stopped")
	if errors.Is(err, http.ErrServerClosed) {
		return nil
	}
	return err
}
