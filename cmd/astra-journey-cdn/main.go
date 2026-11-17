package main

import (
	"context"
	"flag"
	"github.com/yomezza/cosmosimulator/internal/cdnserver"
	"github.com/yomezza/cosmosimulator/internal/platform/configuration"
	"log"
	"os"
	"os/signal"
	"syscall"
)

func runConsole() error {
	pack := flag.String("install-pack", "", "install a local graphics archive, then exit")
	legacy := flag.String("import-guide", "", "copy legacy GUIDE_MEDIA_DIR into private CDN storage, then exit")
	flag.Parse()
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	if *pack != "" || *legacy != "" {
		if err := configuration.Initialize(configuration.CDN); err != nil {
			return err
		}
		c, err := cdnserver.LoadConfig()
		if err != nil {
			return err
		}
		s, err := cdnserver.New(c)
		if err != nil {
			return err
		}
		defer s.Close()
		if *pack != "" {
			if err = s.Install(*pack); err != nil {
				return err
			}
		}
		if *legacy != "" {
			return s.ImportGuide(*legacy)
		}
		return nil
	}
	return cdnserver.Run(ctx)
}
func main() {
	if err := runPlatform(); err != nil {
		log.Fatal(err)
	}
}
