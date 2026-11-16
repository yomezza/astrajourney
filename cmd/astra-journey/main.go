package main

import (
	"context"
	"log"
	"os"
	"os/signal"
	"syscall"

	"github.com/yomezza/cosmosimulator/internal/server"
)

func runConsole() error {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	if len(os.Args) == 2 && os.Args[1] == "--check-universe" {
		return server.CheckUniverse(ctx, os.Stdout)
	}
	return server.Run(ctx)
}

func main() {
	if err := runPlatform(); err != nil {
		log.Fatal(err)
	}
}
