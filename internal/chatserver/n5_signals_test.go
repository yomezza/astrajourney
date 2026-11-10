package chatserver

import (
	"context"
	"encoding/json"
	"errors"
	"github.com/nats-io/nats.go"
	"github.com/yomezza/cosmosimulator/internal/platform/gamesignal"
	"github.com/yomezza/cosmosimulator/internal/testkit/natsfixture"
	"testing"
	"time"
)

func TestN5SpatialSignalsAndTransportACL(t *testing.T) {
	b := natsfixture.New(t, natsfixture.Options{PlanetSignals: true})
	s, e := NewNATSWakeSource(testNATSConfig(b.Material, b.URL))
	if e != nil {
		t.Fatal(e)
	}
	if e = s.Start(context.Background()); e != nil {
		t.Fatal(e)
	}
	defer closeNATS(t, s)
	failures := make(chan error, 4)
	handler := func(_ *nats.Conn, _ *nats.Subscription, e error) {
		select {
		case failures <- e:
		default:
		}
	}
	planet := wireNATS(t, b, "planet", b.PlanetPassword, handler)
	d := gamesignal.SectorChange{UserID: "42", VisitID: "12345678-1234-4234-8234-123456789abc", Generation: "1", Epoch: "2", RuntimeFence: "1", SurfaceID: "7", From: 1, To: 2, Revision: "0", Operation: "12345678-1234-4234-8234-123456789def"}
	raw, ok := gamesignal.EncodePlanet(d)
	if !ok {
		t.Fatal("encode")
	}
	planet.Publish(gamesignal.PlanetSectorSubject, raw)
	planet.FlushTimeout(time.Second)
	waitNATS(t, func() bool { return s.spatialReceived.Load() == 1 })
	ids := s.TakeSpatialDirty()
	if len(ids) != 1 || ids[0] != 42 {
		t.Fatal("wrong spatial dirty user")
	}
	s.AfterCommit()
	waitNATS(t, func() bool { return s.received.Load() == 1 })
	for _, subject := range []string{ChatOutboxSubject, "cosmos.v1.signal.cosmic.company.role.changed", "$JS.API.INFO"} {
		planet.Publish(subject, []byte("{}"))
		planet.FlushTimeout(time.Second)
		select {
		case e := <-failures:
			if !errors.Is(e, nats.ErrPermissionViolation) {
				t.Fatal("permission error")
			}
		case <-time.After(time.Second):
			t.Fatal("Planet published outside ACL")
		}
	}
	planet.Subscribe(gamesignal.PlanetSectorSubject, func(*nats.Msg) {})
	planet.FlushTimeout(time.Second)
	select {
	case e := <-failures:
		if !errors.Is(e, nats.ErrPermissionViolation) {
			t.Fatal("permission error")
		}
	case <-time.After(time.Second):
		t.Fatal("Planet subscription allowed")
	}
	chatConn := wireNATS(t, b, "chat", b.Password, handler)
	chatConn.Publish(gamesignal.PlanetSectorSubject, raw)
	chatConn.FlushTimeout(time.Second)
	select {
	case e := <-failures:
		if !errors.Is(e, nats.ErrPermissionViolation) {
			t.Fatal("permission error")
		}
	case <-time.After(time.Second):
		t.Fatal("Chat published as Planet")
	}
	// Same operation UUID in distinct users' transactions must not suppress either.
	d.UserID = "43"
	other, _ := gamesignal.EncodePlanet(d)
	s.accept(&nats.Msg{Subject: gamesignal.PlanetSectorSubject, Data: other})
	if ids = s.TakeSpatialDirty(); len(ids) != 1 || ids[0] != 43 {
		t.Fatal("cross-user signal dedupe collision")
	}
	var bad map[string]any
	json.Unmarshal(raw, &bad)
	bad["schema_version"] = 9
	badRaw, _ := json.Marshal(bad)
	before := s.invalid.Load()
	s.accept(&nats.Msg{Subject: gamesignal.PlanetSectorSubject, Data: badRaw})
	if s.invalid.Load() != before+1 || len(s.TakeSpatialDirty()) != 0 {
		t.Fatal("invalid spatial signal accepted")
	}
}
func TestN5OldACLDoesNotDisableOutbox(t *testing.T) {
	b := natsfixture.New(t, natsfixture.Options{})
	s, e := NewNATSWakeSource(testNATSConfig(b.Material, b.URL))
	if e != nil {
		t.Fatal(e)
	}
	if e = s.Start(context.Background()); e != nil {
		t.Fatal(e)
	}
	defer closeNATS(t, s)
	waitNATS(t, func() bool { return s.spatialSubscriptionFailures.Load() > 0 })
	s.AfterCommit()
	waitNATS(t, func() bool { return s.received.Load() > 0 })
	if !s.Healthy() {
		t.Fatal("optional Planet subscription disabled existing B1")
	}
}
