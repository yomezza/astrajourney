package main

import (
	"context"

	"github.com/yomezza/cosmosimulator/internal/chatserver"
	"golang.org/x/sys/windows/svc"
)

type windowsService struct{}

func (windowsService) Execute(_ []string, requests <-chan svc.ChangeRequest, statuses chan<- svc.Status) (bool, uint32) {
	statuses <- svc.Status{State: svc.StartPending}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	listening := make(chan struct{})
	done := make(chan error, 1)
	go func() { done <- chatserver.RunStarted(ctx, func() { close(listening) }) }()
	current := svc.Status{State: svc.StartPending, Accepts: svc.AcceptStop | svc.AcceptShutdown}
	for {
		select {
		case <-listening:
			listening = nil
			current = svc.Status{State: svc.Running, Accepts: svc.AcceptStop | svc.AcceptShutdown}
			statuses <- current
		case err := <-done:
			if err != nil {
				return true, 1
			}
			return false, 0
		case request := <-requests:
			switch request.Cmd {
			case svc.Interrogate:
				statuses <- current
			case svc.Stop, svc.Shutdown:
				statuses <- svc.Status{State: svc.StopPending}
				cancel()
				if err := <-done; err != nil {
					return true, 1
				}
				return false, 0
			}
		}
	}
}

func runPlatform() error {
	service, err := svc.IsWindowsService()
	if err != nil {
		return err
	}
	if service {
		return svc.Run("AstraJourneyChat", windowsService{})
	}
	return runConsole()
}
