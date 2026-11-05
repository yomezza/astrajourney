package server

import (
	"context"
	"errors"
	"log"
	"net/http"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/yomezza/cosmosimulator/internal/initialization"
	"github.com/yomezza/cosmosimulator/internal/platform/httpkit"
)

func Run(ctx context.Context) error {
	dsn := os.Getenv("DATABASE_URL")
	if dsn == "" {
		return errors.New("set DATABASE_URL to a PostgreSQL connection string")
	}
	check, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()
	db, err := initialization.OpenDatabase(check, dsn, os.Getenv("DB_SCHEMA"), Assets)
	if err != nil {
		return err
	}
	defer db.Close()
	proxies, err := httpkit.ParseTrustedProxies(os.Getenv("TRUSTED_PROXIES"))
	if err != nil {
		return err
	}
	a := New(db, Config{TrustedProxies: proxies, Secure: os.Getenv("COOKIE_SECURE") == "true", Origin: strings.TrimRight(os.Getenv("PUBLIC_ORIGIN"), "/")})
	if err = a.Auth.Bootstrap(ctx); err != nil {
		return err
	}
	addr := os.Getenv("HTTP_ADDR")
	if addr == "" {
		addr = "127.0.0.1:8080"
	}
	srv := &http.Server{Addr: addr, Handler: a.Handler(), ReadHeaderTimeout: 5 * time.Second, ReadTimeout: 15 * time.Second, WriteTimeout: 15 * time.Second, IdleTimeout: 60 * time.Second}
	run, cancelRun := context.WithCancel(ctx)
	var workers sync.WaitGroup
	workers.Add(4)
	go func() { defer workers.Done(); a.Simulation.Simulate(run) }()
	go func() { defer workers.Done(); a.Colonies.RunLandingWorker(run) }()
	go func() { defer workers.Done(); a.runTransfers(run) }()
	go func() { defer workers.Done(); a.Corporations.RunChatCleanup(run) }()
	defer func() { cancelRun(); workers.Wait() }()
	go func() {
		<-run.Done()
		c, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		_ = srv.Shutdown(c)
	}()
	log.Printf("Cosmosimulator listening on %s", addr)
	err = srv.ListenAndServe()
	if errors.Is(err, http.ErrServerClosed) {
		return nil
	}
	return err
}
