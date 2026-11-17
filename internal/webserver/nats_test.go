package webserver

import (
	"context"
	"github.com/nats-io/nats.go"
	"github.com/yomezza/cosmosimulator/internal/platform/gamesignal"
	"github.com/yomezza/cosmosimulator/internal/platform/natscore"
	"github.com/yomezza/cosmosimulator/internal/testkit/natsfixture"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func TestNativeNATSInvalidatesPublicMapAndRecovers(t *testing.T) {
	broker := natsfixture.New(t, natsfixture.Options{N6: true})
	var hits atomic.Int64
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits.Add(1)
		w.Write([]byte(`{"schema_version":1,"client_contract":"astra-assets-v1","urls":{}}`))
	}))
	defer upstream.Close()
	s, e := New(Config{StorageRoot: t.TempDir(), CosmicURL: upstream.URL, CDNURL: upstream.URL, PlanetURL: upstream.URL, ChatURL: upstream.URL})
	if e != nil {
		t.Fatal(e)
	}
	defer s.Close()
	e = s.ConfigureNATS(natscore.Config{Enabled: true, Username: "web", ServiceIdentity: "web", Password: broker.WebPassword, Servers: []string{broker.URL}, CAFile: broker.CAFile, ConnectTimeout: 500 * time.Millisecond, ReconnectWait: 250 * time.Millisecond})
	if e != nil {
		t.Fatal(e)
	}
	s.nats.Start(context.Background())
	defer s.nats.Close(context.Background())
	wait := func(f func() bool) {
		t.Helper()
		deadline := time.Now().Add(8 * time.Second)
		for time.Now().Before(deadline) {
			if f() {
				return
			}
			time.Sleep(10 * time.Millisecond)
		}
		t.Fatal("Web NATS condition timed out", s.nats.Diagnostics())
	}
	wait(s.nats.Healthy)
	get := func() {
		t.Helper()
		w := httptest.NewRecorder()
		s.assetMap(w, httptest.NewRequest("GET", "/cdn/api/v1/asset-map", nil))
		if w.Code != 200 {
			t.Fatal(w.Code)
		}
	}
	get()
	get()
	if hits.Load() != 1 {
		t.Fatal("uncached public map")
	}
	publisher, e := nats.Connect(broker.URL, nats.UserInfo("cdn", broker.CDNPassword), nats.RootCAs(broker.CAFile))
	if e != nil {
		t.Fatal(e)
	}
	previous := s.generation.Load()
	message, _ := gamesignal.EncodeAssets(gamesignal.AssetsChangedSubject, strings.Repeat("a", 64))
	publisher.Publish(message.Subject, message.Data)
	publisher.Flush()
	wait(func() bool { return s.generation.Load() > previous })
	get()
	if hits.Load() != 2 {
		t.Fatal("NATS did not invalidate public map")
	}
	publisher.Close()
	broker.Stop()
	wait(func() bool { return !s.nats.Healthy() })
	get()
	if hits.Load() != 2 {
		t.Fatal("broker failure discarded valid map")
	}
	s.cache.Lock()
	s.cache.until = time.Time{}
	s.cache.Unlock()
	get()
	if hits.Load() != 3 {
		t.Fatal("offline periodic reconciliation failed")
	}
	previous = s.generation.Load()
	broker.Start()
	wait(func() bool { return s.nats.Healthy() && s.generation.Load() > previous })
	get()
	if hits.Load() != 4 {
		t.Fatal("reconnection did not reconcile public map")
	}
}
