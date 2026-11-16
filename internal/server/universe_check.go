package server

import (
	"context"
	"encoding/json"
	"errors"
	"github.com/yomezza/cosmosimulator/internal/platform/configuration"
	"github.com/yomezza/cosmosimulator/internal/platform/dblayout"
	"github.com/yomezza/cosmosimulator/internal/platform/postgres"
	"github.com/yomezza/cosmosimulator/internal/universestore"
	"io"
	"time"
)

func CheckUniverse(ctx context.Context, out io.Writer) error {
	if e := configuration.Initialize(configuration.Cosmos); e != nil {
		return e
	}
	check, cancel := context.WithTimeout(ctx, 5*time.Minute)
	defer cancel()
	db, _, e := postgres.Open(check, configuration.Getenv("DATABASE_URL"), configuration.Getenv("DB_SCHEMA"))
	if e != nil {
		return errors.New("universe check: database connection failed; verify configuration, network and access")
	}
	defer db.Close()
	report, e := universestore.Inspect(check, db)
	if writeErr := json.NewEncoder(out).Encode(report); writeErr != nil {
		return writeErr
	}
	return e
}

// CheckSchemas is a read-only deployment preflight; it starts no game workers.
func CheckSchemas(ctx context.Context, out io.Writer) error {
	if e := configuration.Initialize(configuration.Cosmos); e != nil {
		return e
	}
	check, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	db, root, e := postgres.Open(check, configuration.Getenv("DATABASE_URL"), configuration.Getenv("DB_SCHEMA"))
	if e != nil {
		return errors.New("schema check: database connection failed; verify configuration, network and access")
	}
	defer db.Close()
	report, e := dblayout.Inspect(check, db, root)
	if w := json.NewEncoder(out).Encode(report); w != nil {
		return w
	}
	return e
}
