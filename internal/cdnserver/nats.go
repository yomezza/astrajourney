package cdnserver

import (
	"github.com/nats-io/nats.go"
	"github.com/yomezza/cosmosimulator/internal/platform/gamesignal"
	"github.com/yomezza/cosmosimulator/internal/platform/natscore"
)

func (s *Service) ConfigureNATS(c natscore.Config) error {
	client, err := natscore.NewSignalClient(c, "cdn", []string{gamesignal.AssetsReadySubject, gamesignal.AssetsChangedSubject}, []string{gamesignal.MediaChangedSubject}, func(m *nats.Msg) {
		// No ACL cache: valid metadata hints do not grant access or delete files.
		if m.Reply != "" || !gamesignal.DecodeMedia(m.Subject, m.Data) {
			s.count.InvalidHints.Add(1)
			return
		}
		s.count.MediaHints.Add(1)
	}, s.publishAssets)
	if err != nil {
		return err
	}
	s.nats = client
	return nil
}
func (s *Service) publishChanged() {
	for _, revision := range s.store.Revisions() {
		if message, ok := gamesignal.EncodeAssets(gamesignal.AssetsChangedSubject, revision); ok {
			s.nats.Publish(message)
		}
	}
}
func (s *Service) publishAssets() {
	for _, revision := range s.store.Revisions() {
		if message, ok := gamesignal.EncodeAssets(gamesignal.AssetsReadySubject, revision); ok {
			s.nats.Publish(message)
		}
	}
}
