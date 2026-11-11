package main

import (
	"context"
	"log"
	"os"
	"os/signal"
	"syscall"

	"github.com/yomezza/cosmosimulator/internal/planetserver"
)

func runConsole() error {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	return planetserver.Run(ctx)
}

func main() {
	if err := runPlatform(); err != nil {
		log.Fatal(err)
	}
}
