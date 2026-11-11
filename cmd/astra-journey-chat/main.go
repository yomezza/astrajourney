package main

import (
	"context"
	"log"
	"os"
	"os/signal"
	"syscall"

	"github.com/yomezza/cosmosimulator/internal/chatserver"
)

func runConsole() error {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	return chatserver.Run(ctx)
}

func main() {
	if len(os.Args) == 2 && os.Args[1] == "--maintain" {
		if e := maintain(); e != nil {
			log.Fatal(e)
		}
		return
	}
	if len(os.Args) == 2 && os.Args[1] == "--grant-runtime" {
		if e := grantRuntime(); e != nil {
			log.Fatal(e)
		}
		return
	}
	if err := runPlatform(); err != nil {
		log.Fatal(err)
	}
}
