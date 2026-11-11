package chatserver

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/yomezza/cosmosimulator/internal/platform/natscore"
	"github.com/yomezza/cosmosimulator/internal/testkit/natsfixture"
)

func TestPlainNATSAuthReconnectAndTLSIsolation(t *testing.T) {
	b := natsfixture.New(t, natsfixture.Options{N6: true, Plain: true})
	c := testNATSConfig(b.Material, b.URL)
	c.Password = b.Password
	c.PasswordFile = ""
	c.CAFile = ""
	if e := c.Validate("b1"); e != nil {
		t.Fatal(e)
	}
	tls, password, e := c.credentials()
	if e != nil || tls != nil || password != b.Password {
		t.Fatal("plain credentials", e)
	}
	s, e := NewNATSWakeSource(c)
	if e != nil {
		t.Fatal(e)
	}
	if e = s.Start(context.Background()); e != nil {
		t.Fatal(e)
	}
	defer closeNATS(t, s)
	s.AfterCommit()
	waitNATS(t, func() bool { return s.received.Load() == 1 })
	b.Stop()
	waitNATS(t, func() bool { return !s.Healthy() })
	s.AfterCommit()
	b.Start()
	waitNATS(t, func() bool { return s.registrations.Load() >= 2 && s.Healthy() })
	before := s.received.Load()
	s.AfterCommit()
	waitNATS(t, func() bool { return s.received.Load() > before })
	wrong := c
	wrong.Password = "wrong-test-only-password"
	denied, e := NewNATSWakeSource(wrong)
	if e != nil {
		t.Fatal(e)
	}
	if e = denied.Start(context.Background()); e == nil || natscore.ErrorCode(e) != "authentication_failed" {
		t.Fatal("wrong credentials accepted or unclassified")
	}
	closeNATS(t, denied)
	secure := testNATSConfig(b.Material, strings.Replace(b.URL, "nats://", "tls://", 1))
	required, e := NewNATSWakeSource(secure)
	if e != nil {
		t.Fatal(e)
	}
	if e = required.Start(context.Background()); e == nil {
		t.Fatal("TLS silently fell back to plain")
	}
	closeNATS(t, required)
}

func TestNATSPlainConfigBoundaries(t *testing.T) {
	c := NATSConfig{Enabled: true, ServiceIdentity: "chat", Username: "chat", Password: "test-only", Servers: []string{"nats://127.0.0.1:14222"}, ConnectTimeout: time.Second, ReconnectWait: time.Second}
	if e := c.Validate("b1"); e != nil {
		t.Fatal(e)
	}
	for _, change := range []func(*NATSConfig){
		func(c *NATSConfig) { c.PasswordFile = "/tmp/test.password" },
		func(c *NATSConfig) { c.Password = "" },
		func(c *NATSConfig) { c.Password = "line\nbreak" },
		func(c *NATSConfig) { c.CAFile = "/tmp/ca.pem" },
		func(c *NATSConfig) { c.Servers = []string{"nats://127.0.0.1:14222", "tls://127.0.0.1:14223"} },
		func(c *NATSConfig) { c.Servers = []string{"nats://chat:test-only@127.0.0.1:14222"} },
	} {
		bad := c
		change(&bad)
		if e := bad.Validate("b1"); e == nil {
			t.Fatal("invalid profile accepted")
		} else if strings.Contains(e.Error(), "test-only") {
			t.Fatal("configuration error leaked password")
		}
	}
}
