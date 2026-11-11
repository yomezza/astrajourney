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
	return server.Run(ctx)
}

func main() {
	if err := runPlatform(); err != nil {
		log.Fatal(err)
	}
}
