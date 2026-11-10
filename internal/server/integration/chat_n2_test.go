package integration

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/coder/websocket"
	"github.com/jackc/pgx/v5"
	"github.com/yomezza/cosmosimulator/internal/account"
	"github.com/yomezza/cosmosimulator/internal/chat"
	"github.com/yomezza/cosmosimulator/internal/chatintegration"
	"github.com/yomezza/cosmosimulator/internal/chatserver"
)

// DBA's reviewed SQL is run only by the fixture owner in a disposable full
// game schema. It is deliberately NOT wired into Cosmic or Chat migrations.
func n2SQL(t *testing.T, f *fixture033, direction string) {
	t.Helper()
	if !strings.HasSuffix(f.db.Config().ConnConfig.Database, "_test") {
		t.Fatal("test database required")
	}
	var schema string
	if err := f.db.QueryRow(f.ctx, `SELECT current_schema()`).Scan(&schema); err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(schema, "update033_") {
		t.Fatal("owned fixture schema required")
	}
	raw, err := os.ReadFile(filepath.Join("..", "..", "..", "docs", "ideas", "nats-event-bus", "dba", "sql", "notify-"+direction+".example.sql"))
	if err != nil {
		t.Fatal(err)
	}
	tx, err := f.db.Begin(f.ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback(f.ctx)
	if _, err = tx.Exec(f.ctx, `SET LOCAL search_path TO `+pgx.Identifier{schema}.Sanitize()+`,pg_catalog; SET LOCAL lock_timeout='2s'; SET LOCAL statement_timeout='10s'`); err != nil {
		t.Fatal(err)
	}
	if _, err = tx.Exec(f.ctx, string(raw)); err != nil {
		t.Fatal(err)
	}
	if err = tx.Commit(f.ctx); err != nil {
		t.Fatal(err)
	}
}
func n2Source(t *testing.T, f *fixture033) *chatserver.PostgresWakeSource {
	t.Helper()
	s := chatserver.NewPostgresWakeSource(f.db)
	if err := s.Start(f.ctx); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { n2CloseSource(t, s) })
	return s
}
func n2CloseSource(t *testing.T, s *chatserver.PostgresWakeSource) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	if err := s.Close(ctx); err != nil {
		t.Error(err)
	}
}
func n2Drain(s *chatserver.PostgresWakeSource) {
	for {
		select {
		case <-s.Wakeups():
		default:
			return
		}
	}
}
func n2Signals(s *chatserver.PostgresWakeSource) uint64 { return s.Diagnostics()["signals"].(uint64) }
func n2Barrier(t *testing.T, f *fixture033, a *chatserver.App) {
	t.Helper()
	var id int64
	if err := f.db.QueryRow(f.ctx, `INSERT INTO chat_outbox(event_kind) VALUES('n2_barrier') RETURNING id`).Scan(&id); err != nil {
		t.Fatal(err)
	}
	eventuallyN1(t, func() bool { return a.Diagnostics()["dispatcher"].(map[string]any)["highwater"].(int64) >= id })
	time.Sleep(50 * time.Millisecond) // Drain the single local WS writer, not a load measurement.
}
func n2Command(t *testing.T, c *n1Client, kind string, payload any) map[string]any {
	t.Helper()
	id := chat.UUID()
	raw, err := json.Marshal(map[string]any{"type": kind, "request_id": id, "payload": payload})
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err = c.conn.Write(ctx, websocket.MessageText, raw); err != nil {
		t.Fatal(err)
	}
	select {
	case ack := <-c.acks:
		if ack["request_id"] != id || ack["error"] != nil {
			t.Fatal("command failed", kind, ack["error"])
		}
		if v, ok := ack["result"].(map[string]any); ok {
			return v
		}
		return nil
	case <-ctx.Done():
		t.Fatal("missing command ACK")
		return nil
	}
}
func TestChatN2SourceTransactionsReconnectAndIsolation(t *testing.T) {
	t.Setenv("CHAT_ENABLED", "true")
	f := newFixture033(t)
	n2SQL(t, f, "up")
	n2SQL(t, f, "up")
	pool, _, _ := n1RuntimePool(t, f)
	restricted := *f
	restricted.db = pool
	s := n2Source(t, &restricted)
	second := n2Source(t, &restricted)
	if !s.TakeReconcile() {
		t.Fatal("initial registration did not request reconciliation")
	}
	pid := s.Diagnostics()["backend_pid"].(uint32)
	if f.integer(`SELECT count(*) FROM pg_stat_activity WHERE pid=$1 AND state='idle' AND xact_start IS NULL`, pid) != 1 {
		t.Fatal("LISTEN left an open transaction")
	}
	n2Drain(s)
	s.AfterCommit()
	if len(s.Wakeups()) != 0 {
		t.Fatal("A2 used local wake")
	}
	before := n2Signals(s)
	tx, err := pool.Begin(f.ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback(f.ctx)
	var pending int64
	if err = tx.QueryRow(f.ctx, `INSERT INTO chat_outbox(event_kind) VALUES('n2_pending') RETURNING id`).Scan(&pending); err != nil {
		t.Fatal(err)
	}
	time.Sleep(75 * time.Millisecond)
	if n2Signals(s) != before || f.integer(`SELECT count(*) FROM chat_outbox WHERE id=$1`, pending) != 0 {
		t.Fatal("uncommitted notification or row visible")
	}
	if err = tx.Rollback(f.ctx); err != nil {
		t.Fatal(err)
	}
	time.Sleep(75 * time.Millisecond)
	if n2Signals(s) != before || f.integer(`SELECT count(*) FROM chat_outbox WHERE id=$1`, pending) != 0 {
		t.Fatal("rollback leaked")
	}
	tx, err = pool.Begin(f.ctx)
	if err != nil {
		t.Fatal(err)
	}
	for range 3 {
		if _, err = tx.Exec(f.ctx, `INSERT INTO chat_outbox(event_kind) VALUES('n2_commit')`); err != nil {
			t.Fatal(err)
		}
	}
	if err = tx.Commit(f.ctx); err != nil {
		t.Fatal(err)
	}
	eventuallyN1(t, func() bool { return n2Signals(s) == before+1 && n2Signals(second) == before+1 })
	if f.integer(`SELECT count(*) FROM chat_outbox WHERE event_kind='n2_commit'`) != 3 {
		t.Fatal("multi-statement data lost")
	}
	// A payload is not a protocol and must never become a grant or a message.
	var channel string
	if err = f.db.QueryRow(f.ctx, `SELECT 'cosmos_chat_'||md5(current_schema())`).Scan(&channel); err != nil {
		t.Fatal(err)
	}
	f.exec(`SELECT pg_notify($1,'untrusted')`, channel)
	eventuallyN1(t, func() bool { return s.Diagnostics()["ignored_signals"].(uint64) == 1 })
	if n2Signals(s) != before+1 {
		t.Fatal("nonempty NOTIFY accepted")
	}
	// A separate complete schema in the same DB cannot wake this listener.
	other := newFixture033(t)
	n2SQL(t, other, "up")
	otherSource := n2Source(t, other)
	other.exec(`INSERT INTO chat_outbox(event_kind) VALUES('n2_other_schema')`)
	eventuallyN1(t, func() bool { return n2Signals(otherSource) == 1 })
	if n2Signals(s) != before+1 {
		t.Fatal("cross-schema wake")
	}
	var executeAllowed bool
	if err = pool.QueryRow(f.ctx, `SELECT has_function_privilege(current_user,'chat_outbox_notify_statement_v1()','EXECUTE')`).Scan(&executeAllowed); err != nil || executeAllowed {
		t.Fatal("runtime gained direct A2-function execution", err)
	}
	countBeforeEmpty := f.integer(`SELECT count(*) FROM chat_outbox`)
	f.exec(`INSERT INTO chat_outbox(event_kind) SELECT 'n2_empty' WHERE false`)
	eventuallyN1(t, func() bool { return n2Signals(s) == before+2 })
	if f.integer(`SELECT count(*) FROM chat_outbox`) != countBeforeEmpty {
		t.Fatal("empty INSERT changed rows")
	}
	f.exec(`SELECT pg_terminate_backend($1)`, pid)
	eventuallyN1(t, func() bool { return s.Diagnostics()["registrations"].(uint64) >= 2 && s.Healthy() })
	if !s.TakeReconcile() {
		t.Fatal("reconnect did not request state reconciliation")
	}
	f.exec(`INSERT INTO chat_outbox(event_kind) SELECT 'n2_multirow' FROM generate_series(1,3)`)
	eventuallyN1(t, func() bool { return n2Signals(s) == before+3 })
	newPID := s.Diagnostics()["backend_pid"].(uint32)
	n2CloseSource(t, s)
	n2CloseSource(t, second)
	if f.integer(`SELECT count(*) FROM pg_stat_activity WHERE pid=$1`, newPID) != 0 {
		t.Fatal("dedicated connection leaked")
	}
	count := f.integer(`SELECT count(*) FROM chat_outbox`)
	var acl string
	if err = f.db.QueryRow(f.ctx, `SELECT relacl::text FROM pg_class WHERE oid='chat_outbox'::regclass`).Scan(&acl); err != nil {
		t.Fatal(err)
	}
	n2SQL(t, f, "down")
	n2SQL(t, f, "down")
	var afterACL string
	if err = f.db.QueryRow(f.ctx, `SELECT relacl::text FROM pg_class WHERE oid='chat_outbox'::regclass`).Scan(&afterACL); err != nil {
		t.Fatal(err)
	}
	if f.integer(`SELECT count(*) FROM chat_outbox`) != count || acl != afterACL {
		t.Fatal("DOWN changed data or runtime privileges")
	}
	if err = s.Start(f.ctx); err == nil {
		t.Fatal("closed source restarted")
	}
}
func TestChatN2SocketFallbackScopeAndReconnect(t *testing.T) {
	testChatN1SocketFallbackScopeAndReconnect(t, "a2")
}
func TestChatN2SlowWriterIsolation(t *testing.T) { testChatN1SlowWriterIsolation(t, "a2") }
func TestChatN2SQLFailureRecovery(t *testing.T)  { testChatN1SQLFailureRecovery(t, "a2") }

func TestChatN2SourceUnavailableFallback(t *testing.T) {
	t.Setenv("CHAT_ENABLED", "true")
	f := newFixture033(t)
	n2SQL(t, f, "up")
	pool, _, _ := n1RuntimePool(t, f)
	svc := &chat.Service{DB: pool}
	u, lease, _ := n1User(t, f, svc, "n2fallback")
	wire, cut := databaseWire200(t, f)
	cut() // Only the listener's network is down. Authoritative SQL stays healthy.
	source := chatserver.NewPostgresWakeSource(wire)
	registry := chatserver.NewWSRegistry()
	h, ok := registry.Register(u, lease, func() {})
	if !ok {
		t.Fatal("register")
	}
	cfg := chatserver.DefaultDispatcherConfig()
	cfg.Mode = "a2"
	cfg.PollInterval = time.Second
	cfg.DegradedInterval = 500 * time.Millisecond
	d := chatserver.NewEventDispatcher(svc, registry, source, cfg)
	ctx, cancel := context.WithCancel(f.ctx)
	done := make(chan struct{})
	go func() { defer close(done); d.Run(ctx) }()
	defer func() {
		cancel()
		select {
		case <-done:
		case <-time.After(4 * time.Second):
			t.Error("source/dispatcher shutdown leaked")
		}
	}()
	eventuallyN1(t, func() bool { return d.Diagnostics()["scans"].(uint64) > 0 })
	for {
		select {
		case <-h.Sync:
		default:
			goto drained
		}
	}
drained:
	f.exec(`INSERT INTO chat_outbox(event_kind,target_user_id) VALUES('n2_no_notify',$1)`, u.ID)
	select {
	case <-h.Sync:
	case <-time.After(3 * time.Second):
		t.Fatal("degraded polling lost commit")
	}
	if source.Healthy() || source.Diagnostics()["registrations"].(uint64) != 0 {
		t.Fatal("test did not exercise source outage")
	}
}

func TestChatN2MissingTriggerFallback(t *testing.T) {
	t.Setenv("CHAT_ENABLED", "true")
	f := newFixture033(t) // No UP: a registered listener cannot assume a trigger exists.
	pool, _, _ := n1RuntimePool(t, f)
	restricted := *f
	restricted.db = pool
	a, _, url, origin := n1App(t, &restricted, "a2", nil)
	sender, _ := f.signup("n2missingtrigger1")
	receiver, _ := f.signup("n2missingtrigger2")
	left, right := n1Dial(t, url, origin, sender.Value), n1Dial(t, url, origin, receiver.Value)
	eventuallyN1(t, func() bool { return left.syncs.Load() > 0 && right.syncs.Load() > 0 })
	before := right.syncs.Load()
	n2Command(t, left, "message.send", map[string]any{"kind": "global", "body": "Fallback without installed trigger"})
	eventuallyN1(t, func() bool { return right.syncs.Load() > before })
	if a.Diagnostics()["wake_source"].(map[string]any)["signals"].(uint64) != 0 {
		t.Fatal("test unexpectedly received NOTIFY")
	}
	if a.Diagnostics()["service"].(map[string]uint64)["changes_calls"] != 0 {
		t.Fatal("A2 fell back to per-WS Changes")
	}
}

func TestChatN2PrivateCorporationAndCleanup(t *testing.T) {
	t.Setenv("CHAT_ENABLED", "true")
	f := newFixture033(t)
	n2SQL(t, f, "up")
	pool, _, _ := n1RuntimePool(t, f)
	restricted := *f
	restricted.db = pool
	a, svc, url, origin := n1App(t, &restricted, "a2", nil)
	ca, aid := f.signup("n2alice")
	cb, bid := f.signup("n2bob")
	cc, cid := f.signup("n2carl")
	alice, bob, carl := n1Dial(t, url, origin, ca.Value), n1Dial(t, url, origin, cb.Value), n1Dial(t, url, origin, cc.Value)
	eventuallyN1(t, func() bool { return alice.syncs.Load() > 0 && bob.syncs.Load() > 0 && carl.syncs.Load() > 0 })
	n2Barrier(t, f, a)
	beforeB, beforeC := bob.syncs.Load(), carl.syncs.Load()
	dm := n2Command(t, alice, "private.create", map[string]any{"kind": "dm", "user": fmt.Sprint(bid)})["conversation"].(string)
	eventuallyN1(t, func() bool { return bob.syncs.Load() > beforeB })
	n2Barrier(t, f, a)
	if carl.syncs.Load() != beforeC {
		t.Fatal("private invitation broadcast to outsider")
	}
	n2Command(t, bob, "private.create", map[string]any{"kind": "dm", "user": fmt.Sprint(aid)})
	n2Barrier(t, f, a)
	beforeB, beforeC = bob.syncs.Load(), carl.syncs.Load()
	n2Command(t, alice, "message.send", map[string]any{"kind": "private", "target": dm, "body": "N2 private"})
	eventuallyN1(t, func() bool { return bob.syncs.Load() > beforeB })
	n2Barrier(t, f, a)
	if carl.syncs.Load() != beforeC {
		t.Fatal("private message broadcast to outsider")
	}
	identity := func(token string) chat.Identity {
		u, e := svc.Authenticate(f.ctx, account.TokenHash(token))
		if e != nil {
			t.Fatal(e)
		}
		return u
	}
	uCarl, uBob := identity(cc.Value), identity(cb.Value)
	if _, e := svc.History(f.ctx, uCarl, chat.Target{Kind: "private", Target: dm}, 0, ""); e == nil {
		t.Fatal("outsider private history allowed")
	}
	n2Command(t, alice, "private.invite", map[string]any{"target": dm, "user": fmt.Sprint(cid)})
	var invite string
	if e := f.db.QueryRow(f.ctx, `SELECT id::text FROM chat_invites WHERE conversation_id=$1 AND recipient_id=$2 AND status='pending'`, dm, cid).Scan(&invite); e != nil {
		t.Fatal(e)
	}
	f.exec(`UPDATE chat_invites SET expires_at=clock_timestamp()-interval '1 second' WHERE id=$1`, invite)
	n2Barrier(t, f, a)
	beforeC = carl.syncs.Load()
	if e := svc.Maintain(f.ctx); e != nil {
		t.Fatal(e)
	}
	eventuallyN1(t, func() bool { return carl.syncs.Load() > beforeC })
	if f.integer(`SELECT count(*) FROM chat_invites WHERE id=$1 AND status='expired'`, invite) != 1 {
		t.Fatal("invite expiry not durable")
	}
	var company int64
	if e := f.db.QueryRow(f.ctx, `INSERT INTO companies(name,tag,owner_id,credits) VALUES('N2 Test Corporation','N2C',$1,100) RETURNING id`, aid).Scan(&company); e != nil {
		t.Fatal(e)
	}
	f.exec(`UPDATE users SET company_id=$1 WHERE id IN($2,$3)`, company, aid, bid)
	n2Barrier(t, f, a)
	beforeB = bob.syncs.Load()
	n2Command(t, alice, "message.send", map[string]any{"kind": "corporation", "body": "N2 corporation"})
	eventuallyN1(t, func() bool { return bob.syncs.Load() > beforeB })
	if _, e := svc.History(f.ctx, uBob, chat.Target{Kind: "corporation"}, 0, ""); e != nil {
		t.Fatal(e)
	}
	f.exec(`UPDATE users SET company_id=NULL,company_role=NULL WHERE id=$1`, bid)
	if _, e := svc.History(f.ctx, uBob, chat.Target{Kind: "corporation", Target: fmt.Sprint(company)}, 0, ""); e == nil {
		t.Fatal("revoked corporation read allowed")
	}
	tx, e := f.db.Begin(f.ctx)
	if e != nil {
		t.Fatal(e)
	}
	if e = chatintegration.RequestCompany(f.ctx, tx, company); e != nil {
		tx.Rollback(f.ctx)
		t.Fatal(e)
	}
	if e = tx.Commit(f.ctx); e != nil {
		t.Fatal(e)
	}
	for range 4 {
		if e = svc.Maintain(f.ctx); e != nil {
			t.Fatal(e)
		}
	}
	n2Barrier(t, f, a)
	if f.integer(`SELECT count(*) FROM chat_company_cleanup_receipts WHERE company_id=$1`, company) != 1 {
		t.Fatal("company cleanup lost with A2")
	}
	for range 2 {
		if e = f.app.Corporations.FinalizeChatDeletion(f.ctx); e != nil {
			t.Fatal(e)
		}
	}
	if f.integer(`SELECT count(*) FROM companies WHERE id=$1`, company) != 0 {
		t.Fatal("company deletion not finalized")
	}
	if _, e = pool.Exec(f.ctx, `UPDATE users SET role='admin' WHERE id=$1`, cid); e == nil {
		t.Fatal("runtime gained game writes")
	}
	if _, e = pool.Exec(f.ctx, `CREATE TABLE n2_forbidden(id int)`); e == nil {
		t.Fatal("runtime gained DDL")
	}
}

func TestChatN2SpatialTransition(t *testing.T) {
	t.Setenv("CHAT_ENABLED", "true")
	f, c, uid, surface, site, ship := prepare180(t)
	n2SQL(t, f, "up")
	addColony160(f, uid, surface, 2, nil, false)
	reconcile160(f)
	base, stopPlanet := startPlanet150(t, f.db)
	defer stopPlanet()
	conn, w, _, _ := edge210(t, f, c, uid, surface, site, ship, base)
	defer conn.CloseNow()
	offer := readyOffer210(t, conn)
	pool, _, _ := n1RuntimePool(t, f)
	restricted := *f
	restricted.db = pool
	a, svc, url, origin := n1App(t, &restricted, "a2", nil)
	pilot := n1Dial(t, url, origin, c.Value)
	outside, _ := f.signup("n2spatialoutsider")
	outsider := n1Dial(t, url, origin, outside.Value)
	eventuallyN1(t, func() bool { return pilot.syncs.Load() > 0 && outsider.syncs.Load() > 0 })
	n2Barrier(t, f, a)
	before := outsider.syncs.Load()
	n2Command(t, pilot, "message.send", map[string]any{"kind": "sector", "target": fmt.Sprintf("%d/1", surface), "body": "N2 sector one"})
	n2Barrier(t, f, a)
	if outsider.syncs.Load() != before {
		t.Fatal("sector sync leaked to space")
	}
	n2Command(t, pilot, "message.send", map[string]any{"kind": "planet", "target": fmt.Sprint(surface), "body": "N2 planet"})
	u, e := svc.Authenticate(f.ctx, account.TokenHash(c.Value))
	if e != nil {
		t.Fatal(e)
	}
	other, e := svc.Authenticate(f.ctx, account.TokenHash(outside.Value))
	if e != nil {
		t.Fatal(e)
	}
	if _, e = svc.History(f.ctx, other, chat.Target{Kind: "planet", Target: fmt.Sprint(surface)}, 0, ""); e == nil {
		t.Fatal("planet access from space")
	}
	before = pilot.syncs.Load()
	command190(t, conn, w, 2, "transition.confirm", map[string]any{"offer": offer["offer"], "operation": op180(t)})
	complete := read190(t, conn, "transition.complete")
	if complete["welcome"].(map[string]any)["sector"] != float64(2) {
		t.Fatal("transition did not commit")
	}
	eventuallyN1(t, func() bool { return pilot.syncs.Load() > before })
	if _, e = svc.History(f.ctx, u, chat.Target{Kind: "sector", Target: fmt.Sprintf("%d/1", surface)}, 0, ""); e == nil {
		t.Fatal("old sector allowed after transition")
	}
	if _, e = svc.History(f.ctx, u, chat.Target{Kind: "sector", Target: fmt.Sprintf("%d/2", surface)}, 0, ""); e != nil {
		t.Fatal("new sector denied", e)
	}
	conn.CloseNow()
	wait180(t, f, `SELECT count(*) FROM planet_visits WHERE user_id=$1 AND state='disconnected'`, 1, uid)
	if _, e = svc.History(f.ctx, u, chat.Target{Kind: "planet", Target: fmt.Sprint(surface)}, 0, ""); e == nil {
		t.Fatal("stale presence allowed")
	}
}

func TestChatN2LateCommitBeyondOverlap(t *testing.T) {
	for _, mode := range []string{"a1", "a2"} {
		t.Run(mode, func(t *testing.T) {
			t.Setenv("CHAT_ENABLED", "true")
			f := newFixture033(t)
			if mode == "a2" {
				n2SQL(t, f, "up")
			}
			pool, _, _ := n1RuntimePool(t, f)
			restricted := *f
			restricted.db = pool
			a, svc, url, origin := n1App(t, &restricted, mode, nil)
			cookie, uid := f.signup("n2late")
			client := n1Dial(t, url, origin, cookie.Value)
			eventuallyN1(t, func() bool { return client.syncs.Load() > 0 })
			channel := n2Command(t, client, "message.send", map[string]any{"kind": "global", "body": "Before delayed transaction"})["channel_id"].(string)
			tx, e := pool.Begin(f.ctx)
			if e != nil {
				t.Fatal(e)
			}
			defer tx.Rollback(f.ctx)
			var seq int64
			if e = tx.QueryRow(f.ctx, `UPDATE chat_channels SET next_seq=next_seq+1 WHERE id=$1 RETURNING next_seq`, channel).Scan(&seq); e != nil {
				t.Fatal(e)
			}
			mid := chat.UUID()
			at := time.Now()
			if _, e = tx.Exec(f.ctx, `INSERT INTO chat_message_locator(id,kind,sent_at,channel_id,seq,author_id,expires_at) VALUES($1,'global',$2,$3,$4,$5,$6)`, mid, at, channel, seq, uid, at.Add(24*time.Hour)); e != nil {
				t.Fatal(e)
			}
			if _, e = tx.Exec(f.ctx, `INSERT INTO chat_messages(kind,sent_at,id,expires_at,channel_id,seq,author_id,body) VALUES('global',$1,$2,$3,$4,$5,$6,'Delayed committed state')`, at, mid, at.Add(24*time.Hour), channel, seq, uid); e != nil {
				t.Fatal(e)
			}
			var low, high int64
			created := at
			if mode == "a1" {
				created = at.Add(-90 * time.Second)
			} // Same predicate boundary, without a second 65s wall-clock hold.
			if e = tx.QueryRow(f.ctx, `INSERT INTO chat_outbox(event_kind,channel_id,reference_id,created_at) VALUES('channel.changed',$1,$2,$3) RETURNING id`, channel, mid, created).Scan(&low); e != nil {
				t.Fatal(e)
			}
			if e = f.db.QueryRow(f.ctx, `INSERT INTO chat_outbox(event_kind) VALUES('n2_higher_id') RETURNING id`).Scan(&high); e != nil {
				t.Fatal(e)
			}
			eventuallyN1(t, func() bool { return a.Diagnostics()["dispatcher"].(map[string]any)["highwater"].(int64) >= high })
			if low >= high {
				t.Fatal("test IDs are not ordered")
			}
			if mode == "a2" {
				t.Log("Holding a real lower-ID transaction for 65 seconds; no performance workload")
				timer := time.NewTimer(max(time.Millisecond, time.Until(at.Add(65*time.Second))))
				<-timer.C
			}
			before := client.syncs.Load()
			reconciles := a.Diagnostics()["dispatcher"].(map[string]any)["state_reconciliations"].(uint64)
			if e = tx.Commit(f.ctx); e != nil {
				t.Fatal(e)
			}
			window, e := svc.OutboxWindow(f.ctx, time.Minute)
			if e != nil {
				t.Fatal(e)
			}
			rows, e := svc.OutboxPage(f.ctx, window, high, 0, 1000)
			if e != nil {
				t.Fatal(e)
			}
			for _, row := range rows {
				if row.ID == low {
					t.Fatal("test did not place delayed event outside overlap")
				}
			}
			deadline := time.Now().Add(35 * time.Second)
			for client.syncs.Load() <= before || a.Diagnostics()["dispatcher"].(map[string]any)["state_reconciliations"].(uint64) <= reconciles {
				if time.Now().After(deadline) {
					t.Fatal("independent reconciliation failed outside overlap")
				}
				time.Sleep(25 * time.Millisecond)
			}
			u, e := svc.Authenticate(f.ctx, account.TokenHash(cookie.Value))
			if e != nil {
				t.Fatal(e)
			}
			history, e := svc.History(f.ctx, u, chat.Target{Kind: "global"}, 0, "")
			if e != nil {
				t.Fatal(e)
			}
			found := false
			for _, message := range history {
				found = found || message.ID == mid
			}
			if !found {
				t.Fatal("delayed committed message lost")
			}
		})
	}
}

func TestChatN2RestartRetentionAndRollback(t *testing.T) {
	for _, rollback := range []string{"a1", "legacy"} {
		t.Run(rollback, func(t *testing.T) {
			t.Setenv("CHAT_ENABLED", "true")
			f := newFixture033(t)
			n2SQL(t, f, "up")
			pool, _, _ := n1RuntimePool(t, f)
			restricted := *f
			restricted.db = pool
			cookie, uid := f.signup("n2restart")
			var ids []string
			journalPath := filepath.Join(t.TempDir(), "preserved-erasure.log")
			for index, mode := range []string{"a2", "a2", rollback} {
				a, svc, url, origin, stop := chatTestAppJournal(t, &restricted, mode, nil, journalPath)
				client := n1Dial(t, url, origin, cookie.Value)
				eventuallyN1(t, func() bool { return client.syncs.Load() > 0 })
				u, e := svc.Authenticate(f.ctx, account.TokenHash(cookie.Value))
				if e != nil {
					t.Fatal(e)
				}
				history, e := svc.History(f.ctx, u, chat.Target{Kind: "global"}, 0, "")
				if e != nil {
					t.Fatal(e)
				}
				if len(history) != len(ids) {
					t.Fatal("restart/rollback lost committed history")
				}
				for _, id := range ids {
					found := false
					for _, message := range history {
						found = found || message.ID == id
					}
					if !found {
						t.Fatal("history changed")
					}
				}
				f.exec(`UPDATE chat_rate_state SET last_sent_at='-infinity' WHERE user_id=$1`, uid)
				ids = append(ids, n2Command(t, client, "message.send", map[string]any{"kind": "global", "body": fmt.Sprintf("Preserved across restart %d", index)})["message_id"].(string))
				var pid uint32
				if mode == "a2" {
					pid = a.Diagnostics()["wake_source"].(map[string]any)["backend_pid"].(uint32)
				}
				stop()
				if pid != 0 && f.integer(`SELECT count(*) FROM pg_stat_activity WHERE pid=$1`, pid) != 0 {
					t.Fatal("listener survived Chat stop")
				}
				if f.integer(`SELECT count(*) FROM chat_ws_leases WHERE user_id=$1 AND expires_at>clock_timestamp()`, uid) != 0 {
					t.Fatal("lease survived Chat stop")
				}
				if index == 0 {
					f.exec(`UPDATE chat_outbox SET created_at=clock_timestamp()-interval '2 days'`)
					if e = svc.Maintain(f.ctx); e != nil {
						t.Fatal(e)
					}
					if f.integer(`SELECT count(*) FROM chat_outbox`) != 0 {
						t.Fatal("retention fixture not empty")
					}
				}
				if index == 1 {
					n2SQL(t, f, "down")
				}
			}
		})
	}
}
