package main

import (
	"context"
	"flag"
	"github.com/yomezza/cosmosimulator/internal/platform/configuration"
	"github.com/yomezza/cosmosimulator/internal/webserver"
	"log"
	"os"
	"os/signal"
	"syscall"
)

func runConsole() error {
	pack := flag.String("install-pack", "", "install and activate a local Web archive, then exit")
	revision := flag.String("activate", "", "activate a previously installed Web revision, then exit")
	flag.Parse()
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	if *pack == "" && *revision == "" {
		return webserver.Run(ctx)
	}
	if e := configuration.Initialize(configuration.Web); e != nil {
		return e
	}
	c, e := webserver.LoadConfig()
	if e != nil {
		return e
	}
	s, e := webserver.New(c)
	if e != nil {
		return e
	}
	defer s.Close()
	if *pack != "" {
		if e = s.Install(*pack); e != nil {
			return e
		}
		log.Print("Web package installed and activated")
	}
	if *revision != "" {
		if e = s.Activate(*revision); e != nil {
			return e
		}
		log.Print("Web revision activated")
	}
	return nil
}
func main() {
	if e := runPlatform(); e != nil {
		log.Fatal(e)
	}
}
