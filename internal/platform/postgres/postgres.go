// Package postgres configures schema-isolated connections for both services.
// It never creates schemas/tables, runs migrations, seeds worlds or starts ticks.
package postgres

import (
	"context"
	"fmt"
	"strings"
	"unicode/utf8"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/yomezza/cosmosimulator/internal/platform/dblayout"
	"github.com/yomezza/cosmosimulator/internal/platform/telemetry"
)

func Config(dsn, schema string) (*pgxpool.Config, string, error) {
	if schema == "" {
		schema = "public"
	}
	// Treat DB_SCHEMA as one quoted identifier, never a SQL/search_path fragment.
	if !utf8.ValidString(schema) || len(schema) > 63 || strings.ContainsRune(schema, 0) || schema == "$user" || schema == "information_schema" || strings.HasPrefix(strings.ToLower(schema), "pg_") {
		return nil, "", fmt.Errorf("DB_SCHEMA must be a valid PostgreSQL schema name (1–63 bytes), not a system schema")
	}
	config, err := pgxpool.ParseConfig(dsn)
	if err != nil {
		return nil, "", err
	}
	if config.ConnConfig.RuntimeParams == nil {
		config.ConnConfig.RuntimeParams = make(map[string]string)
	}
	config.ConnConfig.Tracer = &telemetry.SQLQueries{}
	config.ConnConfig.RuntimeParams["search_path"] = dblayout.SearchPath(schema)
	return config, schema, nil
}

// Open validates connectivity without performing DDL or game initialization.
// The caller owns the pool and supplies a context with an appropriate deadline.
func Open(ctx context.Context, dsn, schema string) (*pgxpool.Pool, string, error) {
	config, schema, err := Config(dsn, schema)
	if err != nil {
		return nil, "", err
	}
	db, err := pgxpool.NewWithConfig(ctx, config)
	if err != nil {
		return nil, "", err
	}
	if err = db.Ping(ctx); err != nil {
		db.Close()
		return nil, "", fmt.Errorf("PostgreSQL connection: %w", err)
	}
	return db, schema, nil
}

// DBTX preserves a caller's transaction: helpers can accept a pgx.Tx or pool
// without silently opening a second transaction/connection inside a use case.
type DBTX interface {
	Exec(context.Context, string, ...any) (pgconn.CommandTag, error)
	Query(context.Context, string, ...any) (pgx.Rows, error)
	QueryRow(context.Context, string, ...any) pgx.Row
}
