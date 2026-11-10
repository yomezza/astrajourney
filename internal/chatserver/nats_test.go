package chatserver

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"encoding/json"
	"errors"
	"net"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/nats-io/nats.go"
	"github.com/yomezza/cosmosimulator/internal/testkit/natsfixture"
)

func testNATSConfig(m natsfixture.Material, url string) NATSConfig {
	return NATSConfig{Enabled: true, ServiceIdentity: "chat", Username: "chat", Servers: []string{url}, PasswordFile: m.PasswordFile, CAFile: m.CAFile, ConnectTimeout: 500 * time.Millisecond, ReconnectWait: 250 * time.Millisecond}
}
func waitNATS(t *testing.T, f func() bool) {
	t.Helper()
	until := time.Now().Add(5 * time.Second)
	for !f() {
		if time.Now().After(until) {
			t.Fatal("NATS functional condition timeout")
		}
		time.Sleep(10 * time.Millisecond)
	}
}
func closeNATS(t *testing.T, s *NATSWakeSource) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	if e := s.Close(ctx); e != nil {
		t.Error(e)
	}
}
func drainNATS(s *NATSWakeSource) {
	for {
		select {
		case <-s.Wakeups():
		default:
			return
		}
	}
}
func wireNATS(t *testing.T, b *natsfixture.Broker, user, password string, handler nats.ErrHandler) *nats.Conn {
	t.Helper()
	roots := x509.NewCertPool()
	raw, e := os.ReadFile(b.CAFile)
	if e != nil || !roots.AppendCertsFromPEM(raw) {
		t.Fatal("fixture CA")
	}
	nc, e := nats.Connect(b.URL, nats.Secure(&tls.Config{MinVersion: tls.VersionTLS12, RootCAs: roots}), nats.UserInfo(user, password), nats.NoReconnect(), nats.Timeout(time.Second), nats.ErrorHandler(handler))
	if e != nil {
		t.Fatal("fixture connection failed")
	}
	t.Cleanup(nc.Close)
	return nc
}
func TestNATSConfigAndLocalMaterial(t *testing.T) {
	m := natsfixture.PKI(t, false)
	c := testNATSConfig(m, "tls://127.0.0.1:14222")
	if e := c.Validate("b1"); e != nil {
		t.Fatal(e)
	}
	tc, password, e := c.credentials()
	if e != nil || password != m.Password || tc.InsecureSkipVerify || tc.MinVersion < tls.VersionTLS12 {
		t.Fatal("secure credentials", e)
	}
	for _, change := range []func(*NATSConfig){func(c *NATSConfig) { c.Enabled = false }, func(c *NATSConfig) { c.Username = "cosmos" }, func(c *NATSConfig) { c.ServiceIdentity = "planet" }, func(c *NATSConfig) { c.Servers = []string{"nats://127.0.0.1:4222"} }, func(c *NATSConfig) { c.Servers = []string{"tls://user:secret@localhost:4222"} }, func(c *NATSConfig) { c.Servers = []string{"tls://localhost:4222/path"} }, func(c *NATSConfig) { c.Servers = []string{"tls://localhost:4222?secret"} }, func(c *NATSConfig) { c.Servers = []string{"tls://localhost:0"} }, func(c *NATSConfig) { c.PasswordFile = "relative.password" }, func(c *NATSConfig) { c.ConnectTimeout = 0 }, func(c *NATSConfig) { c.ReconnectWait = time.Millisecond }} {
		bad := c
		change(&bad)
		if e := bad.Validate("b1"); e == nil {
			t.Fatal("bad NATS profile accepted")
		} else if strings.Contains(e.Error(), m.Password) {
			t.Fatal("configuration leaks a secret")
		}
	}
	for _, mode := range []string{"legacy", "a1", "a2"} {
		disabled := NATSConfig{PasswordFile: "missing", CAFile: "missing"}
		if e := disabled.Validate(mode); e != nil {
			t.Fatal("old mode gated on NATS", e)
		}
	}
	bad := c
	bad.PasswordFile = filepath.Join(t.TempDir(), "missing")
	if _, _, e = bad.credentials(); e == nil {
		t.Fatal("missing password accepted")
	}
	bad = c
	bad.CAFile = filepath.Join(t.TempDir(), "not-ca.pem")
	os.WriteFile(bad.CAFile, []byte("not a CA"), 0600)
	if _, _, e = bad.credentials(); e == nil {
		t.Fatal("bad CA accepted")
	}
}
func TestNATSSecretPermissionsUnix(t *testing.T) {
	if os.PathSeparator == '\\' {
		t.Skip("Windows DACL checked separately on target OS")
	}
	m := natsfixture.PKI(t, false)
	c := testNATSConfig(m, "tls://127.0.0.1:14222")
	if e := os.Chmod(c.PasswordFile, 0644); e != nil {
		t.Fatal(e)
	}
	if _, _, e := c.credentials(); e == nil {
		t.Fatal("world-readable password accepted")
	}
}
func TestNATSEnvelopeAndBounds(t *testing.T) {
	raw := newChatSignal()
	s, ok := decodeChatSignal(ChatOutboxSubject, raw)
	if !ok || s.SignalID == "" {
		t.Fatal("outgoing envelope")
	}
	for _, b := range [][]byte{nil, []byte("{}"), append(raw, []byte("{}")...), []byte(strings.Repeat(" ", 2049)), []byte(`{"schema_version":1,"token":"secret"}`)} {
		if _, ok = decodeChatSignal(ChatOutboxSubject, b); ok {
			t.Fatal("invalid envelope accepted")
		}
	}
	for _, change := range []func(*chatSignal){func(s *chatSignal) { s.SchemaVersion = 2 }, func(s *chatSignal) { s.Source = "planet" }, func(s *chatSignal) { s.Kind = "planet.sector.changed" }, func(s *chatSignal) { s.Data.Hint = "other" }, func(s *chatSignal) { s.SignalID = "private-id" }, func(s *chatSignal) { s.OccurredAt = time.Time{} }, func(s *chatSignal) { s.OccurredAt = s.OccurredAt.In(time.FixedZone("offset", 3600)) }} {
		bad := s
		change(&bad)
		b, _ := json.Marshal(bad)
		if _, ok = decodeChatSignal(ChatOutboxSubject, b); ok {
			t.Fatal("invalid event profile")
		}
	}
	if _, ok = decodeChatSignal("cosmos.v1.signal.planet.sector.changed", raw); ok {
		t.Fatal("foreign subject accepted")
	}
	source := &NATSWakeSource{wakes: make(chan struct{}, 1), publishes: make(chan struct{}, 1), seen: map[string]bool{}}
	source.accept(&nats.Msg{Subject: ChatOutboxSubject, Data: raw})
	source.accept(&nats.Msg{Subject: ChatOutboxSubject, Data: raw})
	if len(source.wakes) != 1 || source.duplicates.Load() != 1 {
		t.Fatal("duplicate/coalescing")
	}
	for range 1100 {
		source.accept(&nats.Msg{Subject: ChatOutboxSubject, Data: newChatSignal()})
	}
	if len(source.seen) > 1024 || len(source.ring) > 1024 {
		t.Fatal("unbounded signal dedupe")
	}
	drainNATS(source)
	source.healthy.Store(true)
	for range 100 {
		source.AfterCommit()
	}
	if len(source.publishes) != 1 || len(source.wakes) != 0 || source.coalesced.Load() != 99 {
		t.Fatal("unbounded publish queue or local B1 wake")
	}
}
func TestNATSRealDeliveryReconnectAndMalformed(t *testing.T) {
	b := natsfixture.New(t, natsfixture.Options{})
	s, e := NewNATSWakeSource(testNATSConfig(b.Material, b.URL))
	if e != nil {
		t.Fatal(e)
	}
	if e = s.Start(context.Background()); e != nil {
		t.Fatal(e)
	}
	t.Cleanup(func() { closeNATS(t, s) })
	if !s.Healthy() || !s.TakeReconcile() {
		t.Fatal("initial subscription")
	}
	drainNATS(s)
	operator := wireNATS(t, b, "n3_test_operator", b.OperatorPassword, nil)
	observed := make(chan []byte, 4)
	sub, e := operator.Subscribe(ChatOutboxSubject, func(m *nats.Msg) {
		select {
		case observed <- m.Data:
		default:
		}
	})
	if e != nil {
		t.Fatal(e)
	}
	defer sub.Unsubscribe()
	if e = operator.FlushTimeout(time.Second); e != nil {
		t.Fatal(e)
	}
	s.AfterCommit()
	waitNATS(t, func() bool { return s.received.Load() == 1 })
	select {
	case payload := <-observed:
		if _, ok := decodeChatSignal(ChatOutboxSubject, payload); !ok || strings.Contains(string(payload), b.Password) {
			t.Fatal("invalid or secret-bearing publication")
		}
	case <-time.After(time.Second):
		t.Fatal("actual Core publication missing")
	}
	drainNATS(s)
	bad := []byte(`{"schema_version":1,"source":"chat","kind":"wrong"}`)
	operator.Publish(ChatOutboxSubject, bad)
	operator.Publish(ChatOutboxSubject, []byte(strings.Repeat("x", 2049)))
	operator.FlushTimeout(time.Second)
	waitNATS(t, func() bool { return s.invalid.Load() >= 2 })
	if len(s.Wakeups()) != 0 {
		t.Fatal("malformed payload woke dispatcher")
	}
	payload := newChatSignal()
	operator.Publish(ChatOutboxSubject, payload)
	operator.Publish(ChatOutboxSubject, payload)
	operator.FlushTimeout(time.Second)
	waitNATS(t, func() bool { return s.duplicates.Load() >= 1 })
	drainNATS(s)
	b.Stop()
	waitNATS(t, func() bool { return !s.Healthy() })
	s.AfterCommit()
	if s.dropped.Load() == 0 {
		t.Fatal("offline commit was not bounded")
	}
	b.Start()
	waitNATS(t, func() bool { return s.Healthy() && s.registrations.Load() >= 2 })
	if !s.TakeReconcile() {
		t.Fatal("reconnect omitted reconciliation")
	}
	s.AfterCommit()
	waitNATS(t, func() bool { return s.received.Load() >= 4 })
	closeNATS(t, s)
	if s.Healthy() {
		t.Fatal("closed source healthy")
	}
}
func TestNATSRemoteTLSAndAuthFailures(t *testing.T) {
	for _, kind := range []string{"wrong-ca", "wrong-name", "wrong-password", "denied-subscription"} {
		t.Run(kind, func(t *testing.T) {
			b := natsfixture.New(t, natsfixture.Options{WrongName: kind == "wrong-name", DenySubscribe: kind == "denied-subscription"})
			cfg := testNATSConfig(b.Material, b.URL)
			want := "tls_verification_failed"
			if kind == "wrong-ca" {
				cfg.CAFile = natsfixture.PKI(t, false).CAFile
			}
			if kind == "wrong-password" {
				p := filepath.Join(t.TempDir(), "wrong.password")
				os.WriteFile(p, []byte("random-test-wrong-password"), 0600)
				cfg.PasswordFile = p
				want = "authentication_failed"
			}
			if kind == "denied-subscription" {
				want = "authorization_failed"
			}
			s, e := NewNATSWakeSource(cfg)
			if e != nil {
				t.Fatal(e)
			}
			_ = s.Start(context.Background())
			t.Cleanup(func() { closeNATS(t, s) })
			waitNATS(t, func() bool { return s.Diagnostics()["last_error_code"] == want })
			if s.Healthy() {
				t.Fatal("bad TLS/auth accepted")
			}
		})
	}
}
func TestNATSChatTransportACL(t *testing.T) {
	b := natsfixture.New(t, natsfixture.Options{})
	for _, subject := range []string{"cosmos.v1.signal.cosmic.company.role.changed", "cosmos.v1.signal.planet.sector.changed", "unrelated.test", "$JS.API.INFO"} {
		t.Run(subject, func(t *testing.T) {
			failures := make(chan error, 4)
			nc := wireNATS(t, b, "chat", b.Password, func(_ *nats.Conn, _ *nats.Subscription, e error) {
				select {
				case failures <- e:
				default:
				}
			})
			nc.Publish(subject, []byte("{}"))
			nc.FlushTimeout(time.Second)
			select {
			case e := <-failures:
				if !errors.Is(e, nats.ErrPermissionViolation) {
					t.Fatal("wrong permission error")
				}
			case <-time.After(time.Second):
				t.Fatal("foreign publish allowed")
			}
			nc.Subscribe(subject, func(*nats.Msg) {})
			nc.FlushTimeout(time.Second)
			select {
			case e := <-failures:
				if !errors.Is(e, nats.ErrPermissionViolation) {
					t.Fatal("wrong subscription error")
				}
			case <-time.After(time.Second):
				t.Fatal("foreign subscription allowed")
			}
		})
	}
}

func TestNATSShutdownDuringInitialHandshake(t *testing.T) {
	m := natsfixture.PKI(t, false)
	l, e := net.Listen("tcp", "127.0.0.1:0")
	if e != nil {
		t.Fatal(e)
	}
	defer l.Close()
	accepted := make(chan net.Conn, 1)
	go func() {
		c, e := l.Accept()
		if e == nil {
			accepted <- c
		}
	}()
	cfg := testNATSConfig(m, "tls://"+l.Addr().String())
	cfg.ConnectTimeout = 10 * time.Second
	s, e := NewNATSWakeSource(cfg)
	if e != nil {
		t.Fatal(e)
	}
	started := make(chan error, 1)
	go func() { started <- s.Start(context.Background()) }()
	var socket net.Conn
	select {
	case socket = <-accepted:
	case <-time.After(time.Second):
		t.Fatal("initial dial not attempted")
	}
	defer socket.Close()
	closeNATS(t, s)
	select {
	case e := <-started:
		if e == nil {
			t.Fatal("unregistered initial source reported ready")
		}
	case <-time.After(time.Second):
		t.Fatal("initial handshake was not cancelled")
	}
}
func TestNATSEnvironmentConfiguration(t *testing.T) {
	m := natsfixture.PKI(t, false)
	env := map[string]string{"DATABASE_URL": "postgres://test:example@localhost/game", "CHAT_ERASURE_JOURNAL": "unused.log", "CHAT_DISPATCHER_MODE": "b1", "NATS_ENABLED": "true", "NATS_SERVICE_IDENTITY": "chat", "NATS_SERVERS": "tls://127.0.0.1:14222", "NATS_USERNAME": "chat", "NATS_PASSWORD_FILE": m.PasswordFile, "NATS_CA_FILE": m.CAFile}
	lookup := func(k string) string { return env[k] }
	c, e := LoadConfig(lookup)
	if e != nil || c.NATS.ConnectTimeout != 2*time.Second || c.NATS.ReconnectWait != time.Second {
		t.Fatal("ENV B1/defaults", e)
	}
	env["NATS_PASSWORD_FILE"] = filepath.Join(t.TempDir(), "missing")
	if _, e = LoadConfig(lookup); e == nil {
		t.Fatal("local missing credential not rejected at configuration load")
	}
	env["CHAT_DISPATCHER_MODE"] = "a1"
	if _, e = LoadConfig(lookup); e != nil {
		t.Fatal("A1 required unused secret", e)
	}
}
