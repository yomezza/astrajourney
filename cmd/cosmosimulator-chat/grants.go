package main

import (
	"context"
	"errors"
	"github.com/yomezza/cosmosimulator/internal/chatintegration"
	"github.com/yomezza/cosmosimulator/internal/platform/postgres"
	"log"
	"os"
	"time"
)

func grantRuntime() error {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	runtimeRole, definerRole := os.Getenv("CHAT_RUNTIME_ROLE"), os.Getenv("CHAT_DEFINER_ROLE")
	if runtimeRole == "" || definerRole == "" {
		return errors.New("set CHAT_RUNTIME_ROLE, CHAT_DEFINER_ROLE, DATABASE_URL and DB_SCHEMA for the DBA grant operation")
	}
	db, schema, e := postgres.Open(ctx, os.Getenv("DATABASE_URL"), os.Getenv("DB_SCHEMA"))
	if e != nil {
		return errors.New("DBA connection unavailable")
	}
	defer db.Close()
	tx, e := db.Begin(ctx)
	if e != nil {
		return e
	}
	defer tx.Rollback(context.Background())
	if e = chatintegration.GrantRuntime(ctx, tx, schema, runtimeRole, definerRole); e != nil {
		return e
	}
	if e = tx.Commit(ctx); e != nil {
		return e
	}
	log.Print("Chat runtime grants installed; remove DBA credentials before starting the service")
	return nil
}

func maintain() error {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	dsn := os.Getenv("CHAT_MAINTENANCE_DATABASE_URL")
	if dsn == "" {
		dsn = os.Getenv("DATABASE_URL")
	}
	db, _, e := postgres.Open(ctx, dsn, os.Getenv("DB_SCHEMA"))
	if e != nil {
		return errors.New("Chat maintenance connection unavailable")
	}
	defer db.Close()
	if e = chatintegration.MaintainPartitions(ctx, db, time.Now()); e != nil {
		return e
	}
	log.Print("Chat future partitions and expired empty partitions maintained")
	return nil
}
