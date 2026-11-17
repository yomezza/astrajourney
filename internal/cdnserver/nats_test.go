package cdnserver

import (
	"context"
	"encoding/json"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/nats-io/nats.go"
	"github.com/yomezza/cosmosimulator/internal/assetpack"
	"github.com/yomezza/cosmosimulator/internal/platform/gamesignal"
	"github.com/yomezza/cosmosimulator/internal/platform/natscore"
	"github.com/yomezza/cosmosimulator/internal/testkit/natsfixture"
)

func TestNativeNATSAssetSignalsAndBrokerRestart(t *testing.T) {
	broker := natsfixture.New(t, natsfixture.Options{N6: true})
	wire, err := nats.Connect(broker.URL, nats.UserInfo("n3_test_operator", broker.OperatorPassword), nats.RootCAs(broker.CAFile))
	if err != nil {
		t.Fatal(err)
	}
	defer wire.Close()
	sub, err := wire.SubscribeSync(gamesignal.AssetsReadySubject)
	if err != nil {
		t.Fatal(err)
	}
	if err = wire.Flush(); err != nil {
		t.Fatal(err)
	}
	source, out := t.TempDir(), t.TempDir()
	os.WriteFile(filepath.Join(source, "test.svg"), []byte(`<svg xmlns="http://www.w3.org/2000/svg"/>`), 0600)
	os.WriteFile(filepath.Join(source, "aliases.json"), []byte(`{"test.svg":["/test.svg"]}`), 0600)
	release, urls, err := assetpack.Build(source, out, "v0.29.0")
	if err != nil {
		t.Fatal(err)
	}
	service, err := New(Config{StorageRoot: t.TempDir(), CosmicURL: "http://127.0.0.1:1"})
	if err != nil {
		t.Fatal(err)
	}
	defer service.Close()
	if err = service.Install(filepath.Join(out, release.Archive)); err != nil {
		t.Fatal(err)
	}
	config := natscore.Config{Enabled: true, ServiceIdentity: "cdn", Username: "cdn", Password: broker.CDNPassword, Servers: []string{broker.URL}, CAFile: broker.CAFile, ConnectTimeout: 500 * time.Millisecond, ReconnectWait: 250 * time.Millisecond}
	if err = service.ConfigureNATS(config); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	service.nats.Start(ctx)
	defer service.nats.Close(context.Background())
	wait := func(condition func() bool) {
		t.Helper()
		deadline := time.Now().Add(8 * time.Second)
		for time.Now().Before(deadline) {
			if condition() {
				return
			}
			time.Sleep(20 * time.Millisecond)
		}
		t.Fatal("CDN NATS condition timed out", service.nats.Diagnostics())
	}
	wait(service.nats.Healthy)
	message, err := sub.NextMsg(3 * time.Second)
	if err != nil {
		t.Fatal(err)
	}
	var envelope gamesignal.Envelope[gamesignal.AssetChange]
	if json.Unmarshal(message.Data, &envelope) != nil || envelope.Source != "cdn" || envelope.Data.Revision != release.Revision {
		t.Fatal("invalid asset signal")
	}
	broker.Stop()
	wait(func() bool { return !service.nats.Healthy() })
	w := httptest.NewRecorder()
	service.Handler().ServeHTTP(w, httptest.NewRequest("GET", urls["/test.svg"], nil))
	if w.Code != 200 {
		t.Fatal("public files stopped with broker", w.Code)
	}
	broker.Start()
	wait(func() bool {
		return service.nats.Healthy() && service.nats.Diagnostics()["registrations"].(uint64) >= 2
	})
}
