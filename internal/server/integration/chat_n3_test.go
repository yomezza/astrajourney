package integration

import (
	"context"
	"encoding/json"
	"path/filepath"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/yomezza/cosmosimulator/internal/account"
	"github.com/yomezza/cosmosimulator/internal/chat"
	"github.com/yomezza/cosmosimulator/internal/chatserver"
	"github.com/yomezza/cosmosimulator/internal/testkit/natsfixture"
)

func n3TransportSettings(t *testing.T) (func(*chatserver.Config), *natsfixture.Broker) {
	t.Helper()
	b := natsfixture.New(t, natsfixture.Options{})
	setting := func(c *chatserver.Config) {
		c.NATS = chatserver.NATSConfig{Enabled: true, ServiceIdentity: "chat", Username: "chat", Servers: []string{b.URL}, PasswordFile: b.PasswordFile, CAFile: b.CAFile, ConnectTimeout: 500 * time.Millisecond, ReconnectWait: 250 * time.Millisecond}
	}
	return setting, b
}
func n3Source(t *testing.T, a *chatserver.App) map[string]any {
	t.Helper()
	return a.Diagnostics()["wake_source"].(map[string]any)
}
func TestChatN3CommitRollbackSQLFailureAndUUID(t *testing.T) {
	t.Setenv("CHAT_ENABLED", "true")
	f := newFixture033(t)
	pool, role, _ := n1RuntimePool(t, f)
	svc := &chat.Service{DB: pool}
	u, l, _ := n1User(t, f, svc, "n3commit")
	setting, _ := n3TransportSettings(t)
	cfg := chatserver.Config{}
	setting(&cfg)
	source, e := chatserver.NewNATSWakeSource(cfg.NATS)
	if e != nil {
		t.Fatal(e)
	}
	if e = source.Start(f.ctx); e != nil {
		t.Fatal(e)
	}
	defer func() {
		ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		defer cancel()
		if e := source.Close(ctx); e != nil {
			t.Error(e)
		}
	}()
	stop := svc.ObserveCommits(func(string) { source.AfterCommit() })
	defer stop()
	n1Execute(t, svc, u, l, "message.send", map[string]any{"kind": "global", "body": "Commit before Core signal"})
	eventuallyN1(t, func() bool { return source.Diagnostics()["received_signals"].(uint64) >= 1 })
	if f.integer(`SELECT count(*) FROM chat_messages WHERE body='Commit before Core signal'`) != 1 {
		t.Fatal("signal without committed data")
	}
	count := func() uint64 { return source.Diagnostics()["published_hints"].(uint64) }
	before := count()
	if _, e = svc.Execute(f.ctx, u, l, chat.Command{Type: "unsupported", Request: chat.UUID(), Payload: json.RawMessage(`{}`)}); e == nil {
		t.Fatal("rollback test succeeded")
	}
	if count() != before {
		t.Fatal("rollback published")
	}
	cmd := chat.Command{Type: "favorite.note", Request: chat.UUID(), Payload: json.RawMessage(`{"body":"Exactly one operation"}`)}
	if _, e = svc.Execute(f.ctx, u, l, cmd); e != nil {
		t.Fatal(e)
	}
	eventuallyN1(t, func() bool { return count() == before+1 })
	if _, e = svc.Execute(f.ctx, u, l, cmd); e != nil {
		t.Fatal(e)
	}
	if count() != before+1 || f.integer(`SELECT count(*) FROM chat_saved_items WHERE owner_id=$1 AND body='Exactly one operation'`, u.ID) != 1 {
		t.Fatal("UUID replay duplicated operation or publish")
	}
	var schema string
	if e = f.db.QueryRow(f.ctx, `SELECT current_schema()`).Scan(&schema); e != nil {
		t.Fatal(e)
	}
	table := pgx.Identifier{schema, "chat_outbox"}.Sanitize()
	runtimeRole := pgx.Identifier{role}.Sanitize()
	f.exec(`REVOKE INSERT ON ` + table + ` FROM ` + runtimeRole)
	defer f.db.Exec(f.ctx, `GRANT INSERT ON `+table+` TO `+runtimeRole)
	f.exec(`UPDATE chat_rate_state SET last_sent_at='-infinity' WHERE user_id=$1`, u.ID)
	before = count()
	payload, _ := json.Marshal(map[string]any{"kind": "global", "body": "Must rollback on SQL failure"})
	if _, e = svc.Execute(f.ctx, u, l, chat.Command{Type: "message.send", Request: chat.UUID(), Payload: payload}); e == nil {
		t.Fatal("failed SQL transaction succeeded")
	}
	if count() != before || f.integer(`SELECT count(*) FROM chat_messages WHERE body='Must rollback on SQL failure'`) != 0 {
		t.Fatal("SQL failure published or partially committed")
	}
}
func TestChatN3OfflineStartupOutageRecoveryAndLostSignal(t *testing.T) {
	t.Setenv("CHAT_ENABLED", "true")
	f := newFixture033(t)
	pool, _, _ := n1RuntimePool(t, f)
	restricted := *f
	restricted.db = pool
	setting, b := n3TransportSettings(t)
	b.Stop()
	a, svc, url, origin, stop := chatTestApp(t, &restricted, "b1", nil, setting)
	defer stop()
	leftCookie, leftID := f.signup("n3left")
	rightCookie, _ := f.signup("n3right")
	left, right := n1Dial(t, url, origin, leftCookie.Value), n1Dial(t, url, origin, rightCookie.Value)
	eventuallyN1(t, func() bool { return left.syncs.Load() > 0 && right.syncs.Load() > 0 })
	eventuallyN1(t, func() bool { return n3Source(t, a)["connect_attempts"].(uint64) > 0 })
	if !a.Health().Ready || n3Source(t, a)["healthy"].(bool) {
		t.Fatal("broker outage gated healthy game")
	}
	before := right.syncs.Load()
	n2Command(t, left, "message.send", map[string]any{"kind": "global", "body": "Offline broker does not block COMMIT"})
	eventuallyN1(t, func() bool { return right.syncs.Load() > before })
	if n3Source(t, a)["published_hints"].(uint64) != 0 {
		t.Fatal("offline test used NATS")
	}
	b.Start()
	eventuallyN1(t, func() bool { return n3Source(t, a)["healthy"].(bool) })
	n2Barrier(t, f, a)
	before = right.syncs.Load()
	f.exec(`UPDATE chat_rate_state SET last_sent_at='-infinity' WHERE user_id=$1`, leftID)
	n2Command(t, left, "message.send", map[string]any{"kind": "global", "body": "Actual B1 path"})
	eventuallyN1(t, func() bool { return n3Source(t, a)["received_signals"].(uint64) > 0 && right.syncs.Load() > before })
	// Simulate a producer crash after COMMIT and before Publish, while the
	// subscriber/broker stay healthy. The separate Service has no commit hook.
	noPublisher := &chat.Service{DB: pool}
	u, e := noPublisher.Authenticate(f.ctx, account.TokenHash(leftCookie.Value))
	if e != nil {
		t.Fatal(e)
	}
	l := chat.Lease{User: leftID}
	if e = f.db.QueryRow(f.ctx, `SELECT connection_id::text,generation FROM chat_ws_leases WHERE user_id=$1`, leftID).Scan(&l.Connection, &l.Generation); e != nil {
		t.Fatal(e)
	}
	n2Barrier(t, f, a)
	before = right.syncs.Load()
	received := n3Source(t, a)["received_signals"].(uint64)
	f.exec(`UPDATE chat_rate_state SET last_sent_at='-infinity' WHERE user_id=$1`, leftID)
	n1Execute(t, noPublisher, u, l, "message.send", map[string]any{"kind": "global", "body": "Committed without Core publication"})
	eventuallyN1(t, func() bool { return right.syncs.Load() > before })
	if n3Source(t, a)["received_signals"].(uint64) != received {
		t.Fatal("lost-publication case received a Core signal")
	}
	b.Stop()
	eventuallyN1(t, func() bool { return !n3Source(t, a)["healthy"].(bool) })
	f.exec(`UPDATE chat_rate_state SET last_sent_at='-infinity' WHERE user_id=$1`, leftID)
	before = right.syncs.Load()
	n2Command(t, left, "message.send", map[string]any{"kind": "global", "body": "Runtime broker outage"})
	eventuallyN1(t, func() bool { return right.syncs.Load() > before })
	registrations := n3Source(t, a)["registrations"].(uint64)
	reconciles := a.Diagnostics()["dispatcher"].(map[string]any)["state_reconciliations"].(uint64)
	b.Start()
	eventuallyN1(t, func() bool {
		return n3Source(t, a)["registrations"].(uint64) > registrations && a.Diagnostics()["dispatcher"].(map[string]any)["state_reconciliations"].(uint64) > reconciles
	})
	history, e := svc.History(f.ctx, u, chat.Target{Kind: "global"}, 0, "")
	if e != nil || len(history) != 4 {
		t.Fatal("messages lost across outage", len(history), e)
	}
}
func TestChatN3SocketScopeSessionAndLease(t *testing.T) {
	testChatN1SocketFallbackScopeAndReconnect(t, "b1")
}
func TestChatN3SlowWriter(t *testing.T)             { testChatN1SlowWriterIsolation(t, "b1") }
func TestChatN3SQLReadFailureRecovery(t *testing.T) { testChatN1SQLFailureRecovery(t, "b1") }
func TestChatN3PrivateCorporationAndCleanup(t *testing.T) {
	testChatPrivateCorporationAndCleanup(t, "b1")
}
func TestChatN3SpatialTransition(t *testing.T)       { testChatSpatialTransition(t, "b1") }
func TestChatN3LateCommitBeyondOverlap(t *testing.T) { testChatLateCommitBeyondOverlap(t, "b1") }
func TestChatN3RestartAndRollback(t *testing.T) {
	for _, target := range []string{"a1", "legacy"} {
		t.Run(target, func(t *testing.T) {
			t.Setenv("CHAT_ENABLED", "true")
			f := newFixture033(t)
			pool, _, _ := n1RuntimePool(t, f)
			restricted := *f
			restricted.db = pool
			setting, b := n3TransportSettings(t)
			cookie, uid := f.signup("n3rollback")
			journal := filepath.Join(t.TempDir(), "preserved-journal")
			var saved []string
			for _, mode := range []string{"b1", "b1", target} {
				if mode == target {
					b.Stop()
				}
				a, svc, url, origin, stop := chatTestAppJournal(t, &restricted, mode, nil, journal, setting)
				if mode == "b1" {
					eventuallyN1(t, func() bool { return n3Source(t, a)["healthy"].(bool) })
				}
				client := n1Dial(t, url, origin, cookie.Value)
				eventuallyN1(t, func() bool { return client.syncs.Load() > 0 })
				u, e := svc.Authenticate(f.ctx, account.TokenHash(cookie.Value))
				if e != nil {
					t.Fatal(e)
				}
				history, e := svc.History(f.ctx, u, chat.Target{Kind: "global"}, 0, "")
				if e != nil || len(history) != len(saved) {
					t.Fatal("restart/rollback lost history", e)
				}
				for _, id := range saved {
					found := false
					for _, message := range history {
						found = found || message.ID == id
					}
					if !found {
						t.Fatal("missing previous message")
					}
				}
				f.exec(`UPDATE chat_rate_state SET last_sent_at='-infinity' WHERE user_id=$1`, uid)
				saved = append(saved, n2Command(t, client, "message.send", map[string]any{"kind": "global", "body": "Preserved across B1 restart and rollback"})["message_id"].(string))
				stop()
				if mode == "b1" && n3Source(t, a)["healthy"].(bool) {
					t.Fatal("B1 source survived shutdown")
				}
				if f.integer(`SELECT count(*) FROM chat_ws_leases WHERE user_id=$1 AND expires_at>clock_timestamp()`, uid) != 0 {
					t.Fatal("lease survived stopped Chat")
				}
			}
		})
	}
}

func TestChatN3PublishDeniedKeepsCommittedMessage(t *testing.T) {
	t.Setenv("CHAT_ENABLED", "true")
	f := newFixture033(t)
	pool, _, _ := n1RuntimePool(t, f)
	restricted := *f
	restricted.db = pool
	b := natsfixture.New(t, natsfixture.Options{DenyPublish: true})
	setting := func(c *chatserver.Config) {
		c.NATS = chatserver.NATSConfig{Enabled: true, ServiceIdentity: "chat", Username: "chat", Servers: []string{b.URL}, PasswordFile: b.PasswordFile, CAFile: b.CAFile, ConnectTimeout: 500 * time.Millisecond, ReconnectWait: 250 * time.Millisecond}
	}
	a, svc, url, origin, _ := chatTestApp(t, &restricted, "b1", nil, setting)
	eventuallyN1(t, func() bool { return n3Source(t, a)["healthy"].(bool) })
	ca, _ := f.signup("n3pubdeny1")
	cb, _ := f.signup("n3pubdeny2")
	left, right := n1Dial(t, url, origin, ca.Value), n1Dial(t, url, origin, cb.Value)
	eventuallyN1(t, func() bool { return left.syncs.Load() > 0 && right.syncs.Load() > 0 })
	before := right.syncs.Load()
	n2Command(t, left, "message.send", map[string]any{"kind": "global", "body": "SQL survives denied publish"})
	eventuallyN1(t, func() bool {
		return n3Source(t, a)["authorization_failures"].(uint64) > 0 && right.syncs.Load() > before
	})
	u, e := svc.Authenticate(f.ctx, account.TokenHash(cb.Value))
	if e != nil {
		t.Fatal(e)
	}
	history, e := svc.History(f.ctx, u, chat.Target{Kind: "global"}, 0, "")
	if e != nil || len(history) != 1 || !a.Health().Ready {
		t.Fatal("broker permission error damaged game", e)
	}
}
