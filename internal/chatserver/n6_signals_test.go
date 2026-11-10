package chatserver

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/nats-io/nats.go"
	"github.com/yomezza/cosmosimulator/internal/platform/gamesignal"
	"github.com/yomezza/cosmosimulator/internal/platform/natscore"
	"github.com/yomezza/cosmosimulator/internal/testkit/natsfixture"
)

func TestN6ThreeIdentitiesAndInvalidation(t *testing.T) {
	b := natsfixture.New(t, natsfixture.Options{N6: true})
	s, e := NewNATSWakeSource(testNATSConfig(b.Material, b.URL))
	if e != nil {
		t.Fatal(e)
	}
	if e = s.Start(context.Background()); e != nil {
		t.Fatal(e)
	}
	defer closeNATS(t, s)
	c := natscore.Config(testNATSConfig(b.Material, b.URL))
	c.Username = "cosmos"
	c.ServiceIdentity = "cosmos"
	c.PasswordFile = b.CosmosPasswordFile
	receipts := make(chan gamesignal.Message, 1)
	cosmic, e := natscore.NewSignalClient(c, "cosmos", gamesignal.CosmicSubjects, []string{gamesignal.CleanupSubject}, func(m *nats.Msg) {
		if _, ok := gamesignal.DecodeCompany(m.Subject, m.Data); ok && m.Reply == "" {
			select {
			case receipts <- gamesignal.Message{Subject: m.Subject, Data: m.Data}:
			default:
			}
		}
	}, nil)
	if e != nil {
		t.Fatal(e)
	}
	cosmic.Start(context.Background())
	defer func() {
		ctx, cancel := context.WithTimeout(context.Background(), time.Second)
		defer cancel()
		if e := cosmic.Close(ctx); e != nil {
			t.Error(e)
		}
	}()
	waitNATS(t, cosmic.Healthy)
	for _, subject := range gamesignal.CosmicSubjects {
		data := gamesignal.CompanyChange{UserID: "42"}
		switch subject {
		case gamesignal.RoleSubject:
			data = gamesignal.CompanyChange{UserID: "42", CompanyID: "7", Fence: "2"}
		case gamesignal.PermissionsSubject:
			data = gamesignal.CompanyChange{CompanyID: "7", Fence: "2"}
		case gamesignal.DeletionSubject:
			data = gamesignal.CompanyChange{CompanyID: "7", Operation: "12345678-1234-4234-8234-123456789abc"}
		}
		m, ok := gamesignal.EncodeCompany(subject, data)
		if !ok {
			t.Fatal(subject)
		}
		before := s.corporateReceived.Load()
		cosmic.Publish(m)
		waitNATS(t, func() bool { return s.corporateReceived.Load() == before+1 })
		if !s.TakeAuthorityDirty() {
			t.Fatal("authority not invalidated")
		}
		s.accept(&nats.Msg{Subject: m.Subject, Data: m.Data})
		if s.TakeAuthorityDirty() {
			t.Fatal("duplicate not coalesced")
		}
	}
	select {
	case <-s.MaintenanceWakeups():
	default:
		t.Fatal("deletion did not wake durable SQL worker")
	}
	m, _ := gamesignal.EncodeCompany(gamesignal.CleanupSubject, gamesignal.CompanyChange{CompanyID: "7", Operation: "12345678-1234-4234-8234-123456789abc"})
	s.CompanyCleanup(m)
	select {
	case <-receipts:
	case <-time.After(time.Second):
		t.Fatal("cleanup reverse signal missing")
	}
	failures := make(chan error, 8)
	handler := func(_ *nats.Conn, _ *nats.Subscription, e error) {
		select {
		case failures <- e:
		default:
		}
	}
	planet := wireNATS(t, b, "planet", b.PlanetPassword, handler)
	for _, subject := range gamesignal.VisitSubjects {
		m, _ := gamesignal.EncodeVisit(subject, gamesignal.VisitChange{UserID: "42", VisitID: "12345678-1234-4234-8234-123456789abc", Generation: "1", Epoch: "2", RuntimeFence: "1", SurfaceID: "7", Sector: 3})
		before := s.lifecycleReceived.Load()
		planet.Publish(m.Subject, m.Data)
		planet.FlushTimeout(time.Second)
		waitNATS(t, func() bool { return s.lifecycleReceived.Load() == before+1 })
		ids := s.TakeSpatialDirty()
		if len(ids) != 1 || ids[0] != 42 {
			t.Fatal("visit target")
		}
	}
	// Exact ACLs: no identity can impersonate another sender or access JS.
	for _, identity := range []struct {
		name, password string
		pub, sub       string
	}{{"cosmos", b.CosmosPassword, gamesignal.VisitStartedSubject, ChatOutboxSubject}, {"planet", b.PlanetPassword, gamesignal.RoleSubject, gamesignal.CleanupSubject}, {"chat", b.Password, gamesignal.MembershipSubject, gamesignal.CleanupSubject}} {
		conn := wireNATS(t, b, identity.name, identity.password, handler)
		for _, subject := range []string{identity.pub, "$JS.API.INFO"} {
			conn.Publish(subject, []byte("{}"))
			conn.FlushTimeout(time.Second)
			select {
			case e := <-failures:
				if !errors.Is(e, nats.ErrPermissionViolation) {
					t.Fatal("unexpected error")
				}
			case <-time.After(time.Second):
				t.Fatal("publish ACL", identity.name)
			}
		}
		conn.Subscribe(identity.sub, func(*nats.Msg) {})
		conn.FlushTimeout(time.Second)
		select {
		case e := <-failures:
			if !errors.Is(e, nats.ErrPermissionViolation) {
				t.Fatal("unexpected error")
			}
		case <-time.After(time.Second):
			t.Fatal("subscribe ACL", identity.name)
		}
	}
	before := s.invalid.Load()
	s.accept(&nats.Msg{Subject: gamesignal.MembershipSubject, Data: []byte(`{"schema_version":99}`)})
	if s.invalid.Load() != before+1 || s.TakeAuthorityDirty() {
		t.Fatal("invalid event reached ACL refresh")
	}
}
