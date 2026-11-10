//go:build ignore

// Test-only fixture; not compiled into game services.
package main

import (
    "context"
    "fmt"
    "os"
    "strings"
    "time"

    "github.com/jackc/pgx/v5/pgxpool"
    "github.com/yomezza/cosmosimulator/internal/chatintegration"
    "github.com/yomezza/cosmosimulator/internal/initialization"
    "github.com/yomezza/cosmosimulator/internal/server"
)

func main() {
    dsn := os.Getenv("TEST_DATABASE_URL")
    schema := os.Getenv("SEED_SCHEMA")
    if dsn == "" || (schema != "nats_dba_test" && schema != "nats_dba_test_b") {
        panic("TEST_DATABASE_URL and SEED_SCHEMA=nats_dba_test[_b] required")
    }
    ctx, cancel := context.WithTimeout(context.Background(), 4*time.Minute)
    defer cancel()
    admin, err := pgxpool.New(ctx, dsn)
    if err != nil { panic(err) }
    defer admin.Close()
    var database string
    if err := admin.QueryRow(ctx, "SELECT current_database()").Scan(&database); err != nil { panic(err) }
    if !strings.HasSuffix(database, "_test") {
        panic("refusing non-test database: " + database)
    }
    rolesSQL := "DO $roles$ BEGIN " +
      "IF NOT EXISTS (SELECT 1 FROM pg_roles WHERE rolname='chat_runtime') THEN " +
      "CREATE ROLE chat_runtime NOLOGIN NOINHERIT NOSUPERUSER NOCREATEDB NOCREATEROLE; END IF; " +
      "IF NOT EXISTS (SELECT 1 FROM pg_roles WHERE rolname='chat_definer') THEN " +
      "CREATE ROLE chat_definer NOLOGIN NOINHERIT NOSUPERUSER NOCREATEDB NOCREATEROLE; END IF; " +
      "END $roles$;"
    if _,err := admin.Exec(ctx, rolesSQL); err != nil { panic(err) }
    // REAL existing Cosmic migrations including schema240; no simplified schema.
    db, err := initialization.OpenDatabase(ctx, dsn, schema, server.Assets)
    if err != nil { panic(err) }
    defer db.Close()
    tx, err := db.Begin(ctx)
    if err != nil { panic(err) }
    if err := chatintegration.GrantRuntime(ctx, tx, schema, "chat_runtime", "chat_definer"); err != nil {
        _ = tx.Rollback(ctx)
        panic(err)
    }
    if err := tx.Commit(ctx); err != nil { panic(err) }
    var v int
    if err := db.QueryRow(ctx,"SELECT version FROM chat_schema_state WHERE id=1").Scan(&v); err != nil || v!=1 {
       panic(fmt.Sprintf("Chat schema invalid: %d (%v)",v,err))
    }
    fmt.Printf("SEED OK: database=%s DB_SCHEMA=%s Chat version=%d\n",database,schema,v)
}
