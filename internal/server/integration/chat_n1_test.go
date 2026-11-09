package integration

import (
	"context"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/coder/websocket"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/yomezza/cosmosimulator/internal/account"
	"github.com/yomezza/cosmosimulator/internal/chat"
	"github.com/yomezza/cosmosimulator/internal/chatintegration"
	"github.com/yomezza/cosmosimulator/internal/chatserver"
)

func n1User(t *testing.T, f *fixture033, s *chat.Service, name string) (chat.Identity, chat.Lease, *http.Cookie) {
	t.Helper()
	cookie, _ := f.signup(name)
	u, e := s.Authenticate(f.ctx, account.TokenHash(cookie.Value))
	if e != nil {
		t.Fatal(e)
	}
	lease, e := s.Claim(f.ctx, u, chat.UUID())
	if e != nil {
		t.Fatal(e)
	}
	return u, lease, cookie
}
func n1Execute(t *testing.T, s *chat.Service, u chat.Identity, l chat.Lease, kind string, payload any) map[string]any {
	t.Helper()
	raw, _ := json.Marshal(payload)
	result, e := s.Execute(context.Background(), u, l, chat.Command{Type: kind, Request: chat.UUID(), Payload: raw})
	if e != nil {
		t.Fatal(kind, e)
	}
	v := map[string]any{}
	if e = json.Unmarshal(result, &v); e != nil {
		t.Fatal(e)
	}
	return v
}

// Uses the same existing DBA grant contract. No production migrations/grants.
func n1RuntimePool(t *testing.T, f *fixture033) (*pgxpool.Pool, string, string) {
	t.Helper()
	suffix := strings.ReplaceAll(chat.UUID(), "-", "")
	role, definer := "n1rt_"+suffix, "n1def_"+suffix
	f.exec(`CREATE ROLE ` + pgx.Identifier{role}.Sanitize() + ` NOLOGIN NOINHERIT`)
	f.exec(`CREATE ROLE ` + pgx.Identifier{definer}.Sanitize() + ` NOLOGIN NOINHERIT`)
	var schema string
	if e := f.db.QueryRow(f.ctx, `SELECT current_schema()`).Scan(&schema); e != nil {
		t.Fatal(e)
	}
	tx, e := f.db.Begin(f.ctx)
	if e != nil {
		t.Fatal(e)
	}
	if e = chatintegration.GrantRuntime(f.ctx, tx, schema, role, definer); e != nil {
		tx.Rollback(f.ctx)
		t.Fatal(e)
	}
	if e = tx.Commit(f.ctx); e != nil {
		t.Fatal(e)
	}
	config := f.db.Config().Copy()
	config.AfterConnect = func(ctx context.Context, c *pgx.Conn) error {
		_, e := c.Exec(ctx, `SET ROLE `+pgx.Identifier{role}.Sanitize())
		return e
	}
	pool, e := pgxpool.NewWithConfig(f.ctx, config)
	if e != nil {
		t.Fatal(e)
	}
	t.Cleanup(func() {
		pool.Close()
		for _, r := range []string{role, definer} {
			f.db.Exec(f.ctx, `REASSIGN OWNED BY `+pgx.Identifier{r}.Sanitize()+` TO CURRENT_USER`)
			f.db.Exec(f.ctx, `DROP OWNED BY `+pgx.Identifier{r}.Sanitize())
			f.db.Exec(f.ctx, `DROP ROLE `+pgx.Identifier{r}.Sanitize())
		}
	})
	return pool, role, definer
}

func TestChatN1RecipientsAndPostCommit(t *testing.T) {
	t.Setenv("CHAT_ENABLED", "true")
	f := newFixture033(t)
	pool, _, _ := n1RuntimePool(t, f)
	s := &chat.Service{DB: pool}
	a, la, _ := n1User(t, f, s, "n1alice")
	b, lb, _ := n1User(t, f, s, "n1bob")
	c, lc, _ := n1User(t, f, s, "n1carl")
	connections := []chat.DispatchConnection{{Identity: a, Lease: la}, {Identity: b, Lease: lb}, {Identity: c, Lease: lc}}
	var commits atomic.Int64
	unsubscribe := s.ObserveCommits(func(kind string) {
		if kind == "mutation" {
			commits.Add(1)
		}
	})
	defer unsubscribe()
	dm := n1Execute(t, s, a, la, "private.create", map[string]any{"kind": "dm", "user": fmt.Sprint(b.ID)})["conversation"].(string)
	allowed, e := s.ResolveRecipients(f.ctx, []chat.DispatchEvent{{ID: 1, Kind: "private.changed", Reference: &dm}}, connections)
	if e != nil || !allowed[a.ID] || !allowed[b.ID] || allowed[c.ID] {
		t.Fatal("NULL channel invitation audience", allowed, e)
	}
	if _, e = s.History(f.ctx, c, chat.Target{Kind: "private", Target: dm}, 0, ""); e == nil {
		t.Fatal("outsider gained private history")
	}
	message := n1Execute(t, s, a, la, "message.send", map[string]any{"kind": "global", "body": "dispatcher"})
	global := message["channel_id"].(string)
	allowed, e = s.ResolveRecipients(f.ctx, []chat.DispatchEvent{{ID: 2, Channel: &global, Target: &a.ID}}, connections)
	if e != nil || len(allowed) != 1 || !allowed[a.ID] {
		t.Fatal("targeted event broadcast", allowed, e)
	}
	before := commits.Load()
	_, e = s.Execute(f.ctx, a, la, chat.Command{Type: "unsupported", Request: chat.UUID(), Payload: json.RawMessage(`{}`)})
	if e == nil || commits.Load() != before {
		t.Fatal("rollback produced postcommit wake")
	}
	raw := json.RawMessage(`{"body":"once"}`)
	cmd := chat.Command{Type: "favorite.note", Request: chat.UUID(), Payload: raw}
	if _, e = s.Execute(f.ctx, a, la, cmd); e != nil {
		t.Fatal(e)
	}
	if _, e = s.Execute(f.ctx, a, la, cmd); e != nil {
		t.Fatal(e)
	}
	if commits.Load() != before+1 {
		t.Fatal("idempotent replay produced another commit notification")
	}
	// Reader sees only committed rows, including a lower ID committed late.
	tx, e := f.db.Begin(f.ctx)
	if e != nil {
		t.Fatal(e)
	}
	defer tx.Rollback(f.ctx)
	var low, high int64
	if e = tx.QueryRow(f.ctx, `INSERT INTO chat_outbox(event_kind,target_user_id,created_at) VALUES('test',$1,clock_timestamp()-interval '20 seconds') RETURNING id`, a.ID).Scan(&low); e != nil {
		t.Fatal(e)
	}
	if e = f.db.QueryRow(f.ctx, `INSERT INTO chat_outbox(event_kind,target_user_id) VALUES('test',$1) RETURNING id`, a.ID).Scan(&high); e != nil {
		t.Fatal(e)
	}
	w, e := s.OutboxWindow(f.ctx, time.Minute)
	if e != nil {
		t.Fatal(e)
	}
	rows, e := s.OutboxPage(f.ctx, w, high, 0, 1000)
	if e != nil {
		t.Fatal(e)
	}
	for _, row := range rows {
		if row.ID == low {
			t.Fatal("dirty read before commit")
		}
	}
	if e = tx.Commit(f.ctx); e != nil {
		t.Fatal(e)
	}
	w, e = s.OutboxWindow(f.ctx, time.Minute)
	if e != nil {
		t.Fatal(e)
	}
	rows, e = s.OutboxPage(f.ctx, w, high, 0, 1000)
	if e != nil {
		t.Fatal(e)
	}
	found := false
	for _, row := range rows {
		found = found || row.ID == low
	}
	if !found {
		t.Fatal("late lower commit missed overlap")
	}
	pageStart, e := s.OutboxWindow(f.ctx, time.Minute)
	if e != nil {
		t.Fatal(e)
	}
	f.exec(`INSERT INTO chat_outbox(event_kind,target_user_id) SELECT 'bulk',$1 FROM generate_series(1,2001)`, a.ID)
	pageWindow, e := s.OutboxWindow(f.ctx, time.Minute)
	if e != nil {
		t.Fatal(e)
	}
	seen := map[int64]bool{}
	after := pageStart.Max
	for {
		page, e := s.OutboxPage(f.ctx, pageWindow, pageStart.Max, after, 1000)
		if e != nil {
			t.Fatal(e)
		}
		for _, event := range page {
			if seen[event.ID] {
				t.Fatal("repeated real keyset row")
			}
			seen[event.ID] = true
			after = event.ID
		}
		if len(page) < 1000 {
			break
		}
	}
	if len(seen) != 2001 {
		t.Fatal("real paginated outbox skipped rows", len(seen))
	}
	// Actual corporate fence and current session/generation rechecks.
	if _, e = s.History(f.ctx, a, chat.Target{Kind: "corporation"}, 0, ""); e != nil {
		t.Fatal(e)
	}
	var corporation string
	if e = f.db.QueryRow(f.ctx, `SELECT id::text FROM chat_channels WHERE kind='corporation' AND company_id=(SELECT company_id FROM users WHERE id=$1)`, a.ID).Scan(&corporation); e != nil {
		t.Fatal(e)
	}
	allowed, e = s.ResolveRecipients(f.ctx, []chat.DispatchEvent{{ID: 3, Channel: &corporation}}, connections)
	if e != nil || !allowed[b.ID] {
		t.Fatal("corporate member", e)
	}
	var other int64
	if e = f.db.QueryRow(f.ctx, `SELECT id FROM companies WHERE founder ORDER BY id LIMIT 1`).Scan(&other); e != nil {
		t.Fatal(e)
	}
	f.exec(`UPDATE users SET company_id=$2,company_role=NULL WHERE id=$1`, b.ID, other)
	allowed, e = s.ResolveRecipients(f.ctx, []chat.DispatchEvent{{ID: 4, Channel: &corporation}}, connections)
	if e != nil || allowed[b.ID] {
		t.Fatal("revoked corporate membership", allowed, e)
	}
	window, e := s.OutboxWindow(f.ctx, time.Minute)
	if e != nil {
		t.Fatal(e)
	}
	f.exec(`UPDATE chat_invites SET expires_at=clock_timestamp()-interval '1 second' WHERE conversation_id=$1 AND status='pending'`, dm)
	if e = s.Maintain(f.ctx); e != nil {
		t.Fatal(e)
	}
	var expiryTargets int
	if e = f.db.QueryRow(f.ctx, `SELECT count(DISTINCT target_user_id) FROM chat_outbox WHERE id>$1 AND event_kind='invite.changed' AND target_user_id=ANY($2::bigint[])`, window.Max, []int64{a.ID, b.ID}).Scan(&expiryTargets); e != nil || expiryTargets != 2 {
		t.Fatal("invitation expiry did not invalidate both users", expiryTargets, e)
	}
	f.exec(`UPDATE sessions SET expires_at=clock_timestamp()-interval '1 second' WHERE user_id=$1`, c.ID)
	f.exec(`UPDATE chat_ws_leases SET generation=generation+1 WHERE user_id=$1`, b.ID)
	allowed, e = s.ResolveRecipients(f.ctx, []chat.DispatchEvent{{ID: 5, Channel: &global}}, connections)
	if e != nil || len(allowed) != 1 || !allowed[a.ID] {
		t.Fatal("expired session/replaced generation", allowed, e)
	}
}

type n1Client struct {
	conn   *websocket.Conn
	syncs  atomic.Int64
	closed chan struct{}
	acks   chan map[string]any
}

func n1Dial(t *testing.T, url, origin, token string) *n1Client {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	conn, r, e := websocket.Dial(ctx, "ws"+strings.TrimPrefix(url, "http")+"/chat/ws/v1", &websocket.DialOptions{HTTPHeader: http.Header{"Origin": {origin}, "Cookie": {"session=" + token}}})
	if e != nil {
		t.Fatal(e, r)
	}
	c := &n1Client{conn: conn, closed: make(chan struct{}), acks: make(chan map[string]any, 64)}
	go func() {
		defer close(c.closed)
		for {
			_, raw, e := conn.Read(context.Background())
			if e != nil {
				return
			}
			var v map[string]any
			if json.Unmarshal(raw, &v) != nil {
				return
			}
			if v["type"] == "sync" {
				c.syncs.Add(1)
			}
			if v["type"] == "ack" {
				select {
				case c.acks <- v:
				default:
					return
				}
			}
		}
	}()
	t.Cleanup(func() { conn.CloseNow(); <-c.closed })
	return c
}
func eventuallyN1(t *testing.T, f func() bool) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for !f() {
		if time.Now().After(deadline) {
			t.Fatal("condition timeout")
		}
		time.Sleep(10 * time.Millisecond)
	}
}
func n1App(t *testing.T, f *fixture033, mode string, listener net.Listener) (*chatserver.App, *chat.Service, string, string) {
	t.Helper()
	health := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(map[string]string{"status": "ready"})
	}))
	t.Cleanup(health.Close)
	s := &chat.Service{DB: f.db}
	j, e := chat.OpenJournal(f.ctx, s, filepath.Join(t.TempDir(), "journal"))
	if e != nil {
		t.Fatal(e)
	}
	s.Journal = j
	t.Cleanup(func() { j.Close() })
	if listener == nil {
		listener, e = net.Listen("tcp", "127.0.0.1:0")
		if e != nil {
			t.Fatal(e)
		}
	}
	cfg := chatserver.Config{DatabaseURL: "test", Journal: "test", Addr: listener.Addr().String(), CosmicURL: health.URL, PlanetURL: health.URL, Origins: []string{health.URL}, Dispatcher: chatserver.DefaultDispatcherConfig()}
	cfg.Dispatcher.Mode = mode
	cfg.Dispatcher.PollInterval = time.Second
	cfg.Dispatcher.DegradedInterval = 500 * time.Millisecond
	a, e := chatserver.New(s, cfg)
	if e != nil {
		t.Fatal(e)
	}
	ctx, cancel := context.WithCancel(f.ctx)
	done := make(chan error, 1)
	started := make(chan struct{})
	go func() { done <- a.Serve(ctx, listener, func() { close(started) }) }()
	<-started
	t.Cleanup(func() {
		cancel()
		select {
		case e := <-done:
			if e != nil {
				t.Error(e)
			}
		case <-time.After(10 * time.Second):
			t.Error("Chat shutdown timeout")
		}
	})
	return a, s, "http://" + listener.Addr().String(), health.URL
}
func TestChatN1SocketFallbackScopeAndReconnect(t *testing.T) {
	t.Setenv("CHAT_ENABLED", "true")
	f := newFixture033(t)
	a, _, url, origin := n1App(t, f, "a1", nil)
	cookie, id := f.signup("n1socket")
	c := n1Dial(t, url, origin, cookie.Value)
	eventuallyN1(t, func() bool { return c.syncs.Load() > 0 })
	time.Sleep(2200 * time.Millisecond)
	before := c.syncs.Load()
	f.exec(`UPDATE users SET role='admin' WHERE id=$1`, id)
	eventuallyN1(t, func() bool { return c.syncs.Load() > before })
	before = c.syncs.Load()
	f.exec(`INSERT INTO chat_outbox(event_kind,target_user_id) VALUES('external',$1)`, id)
	eventuallyN1(t, func() bool { return c.syncs.Load() > before })
	ctx, cancel := context.WithTimeout(f.ctx, time.Second)
	defer cancel()
	_, r, e := websocket.Dial(ctx, "ws"+strings.TrimPrefix(url, "http")+"/chat/ws/v1", &websocket.DialOptions{HTTPHeader: http.Header{"Origin": {origin}, "Cookie": {"session=" + cookie.Value}}})
	if e == nil || r == nil || r.StatusCode != 409 {
		t.Fatal("second connection accepted")
	}
	c.conn.CloseNow()
	<-c.closed
	eventuallyN1(t, func() bool {
		return f.integer(`SELECT count(*) FROM chat_ws_leases WHERE user_id=$1 AND expires_at>clock_timestamp()`, id) == 0
	})
	fresh := n1Dial(t, url, origin, cookie.Value)
	eventuallyN1(t, func() bool { return fresh.syncs.Load() > 0 })
	f.exec(`UPDATE sessions SET expires_at=clock_timestamp()-interval '1 second' WHERE user_id=$1`, id)
	select {
	case <-fresh.closed:
	case <-time.After(4 * time.Second):
		t.Fatal("expired session remained connected")
	}
	if a.Diagnostics()["service"].(map[string]uint64)["changes_calls"] != 0 {
		t.Fatal("A1 called individual Changes")
	}
}

type n1BlockedConn struct {
	net.Conn
	blocked atomic.Bool
	once    sync.Once
	closed  chan struct{}
}

func (c *n1BlockedConn) Write(b []byte) (int, error) {
	if c.blocked.Load() {
		<-c.closed
		return 0, net.ErrClosed
	}
	return c.Conn.Write(b)
}
func (c *n1BlockedConn) Close() error { c.once.Do(func() { close(c.closed) }); return c.Conn.Close() }

type n1Listener struct {
	net.Listener
	first chan *n1BlockedConn
	once  sync.Once
}

func (l *n1Listener) Accept() (net.Conn, error) {
	c, e := l.Listener.Accept()
	if e != nil {
		return nil, e
	}
	var wrapped *n1BlockedConn
	l.once.Do(func() { wrapped = &n1BlockedConn{Conn: c, closed: make(chan struct{})}; l.first <- wrapped })
	if wrapped != nil {
		return wrapped, nil
	}
	return c, nil
}
func TestChatN1SlowWriterIsolation(t *testing.T) {
	t.Setenv("CHAT_ENABLED", "true")
	f := newFixture033(t)
	listener, e := net.Listen("tcp", "127.0.0.1:0")
	if e != nil {
		t.Fatal(e)
	}
	wrapped := &n1Listener{Listener: listener, first: make(chan *n1BlockedConn, 1)}
	_, _, url, origin := n1App(t, f, "a1", wrapped)
	slowCookie, _ := f.signup("n1slow")
	healthyCookie, _ := f.signup("n1healthy")
	slow := n1Dial(t, url, origin, slowCookie.Value)
	blocked := <-wrapped.first
	healthy := n1Dial(t, url, origin, healthyCookie.Value)
	eventuallyN1(t, func() bool { return slow.syncs.Load() > 0 && healthy.syncs.Load() > 0 })
	blocked.blocked.Store(true)
	before := healthy.syncs.Load()
	raw := []byte(`{"type":"message.send","request_id":"` + chat.UUID() + `","payload":{"kind":"global","body":"slow reader isolation"}}`)
	if e = healthy.conn.Write(f.ctx, websocket.MessageText, raw); e != nil {
		t.Fatal(e)
	}
	eventuallyN1(t, func() bool { return healthy.syncs.Load() > before })
	select {
	case <-slow.closed:
	case <-time.After(5 * time.Second):
		t.Fatal("slow writer did not time out")
	}
}

func TestChatN1SQLFailureRecovery(t *testing.T) {
	t.Setenv("CHAT_ENABLED", "true")
	f := newFixture033(t)
	pool, role, _ := n1RuntimePool(t, f)
	copyFixture := *f
	copyFixture.db = pool
	a, _, url, origin := n1App(t, &copyFixture, "a1", nil)
	cookie, id := f.signup("n1sqlfailure")
	client := n1Dial(t, url, origin, cookie.Value)
	eventuallyN1(t, func() bool { return client.syncs.Load() > 0 })
	var schema string
	if e := f.db.QueryRow(f.ctx, `SELECT current_schema()`).Scan(&schema); e != nil {
		t.Fatal(e)
	}
	table := pgx.Identifier{schema, "chat_outbox"}.Sanitize()
	r := pgx.Identifier{role}.Sanitize()
	f.exec(`REVOKE SELECT ON ` + table + ` FROM ` + r)
	defer f.db.Exec(f.ctx, `GRANT SELECT ON `+table+` TO `+r)
	before := client.syncs.Load()
	f.exec(`INSERT INTO chat_outbox(event_kind,target_user_id) VALUES('external',$1)`, id)
	eventuallyN1(t, func() bool { return a.Diagnostics()["dispatcher"].(map[string]any)["scan_errors"].(uint64) > 0 })
	if client.syncs.Load() != before {
		t.Fatal("unverified fanout during SQL failure")
	}
	f.exec(`GRANT SELECT ON ` + table + ` TO ` + r)
	eventuallyN1(t, func() bool { return client.syncs.Load() > before })
}

func TestChatN1SpatialRecipients(t *testing.T) {
	t.Setenv("CHAT_ENABLED", "true")
	f, c, _, surface, site, ship := prepare180(t)
	base, stop := startPlanet150(t, f.db)
	var once sync.Once
	stopPlanet := func() { once.Do(stop) }
	defer stopPlanet()
	ticket := ticket180(t, f, c, op180(t), site, ship)
	conn, welcome := ws180(t, base, c, map[string]any{"ticket": ticket["ticket"]})
	defer conn.CloseNow()
	if welcome["message_type"] != "welcome" {
		t.Fatal(welcome)
	}
	s := &chat.Service{DB: f.db}
	u, e := s.Authenticate(f.ctx, account.TokenHash(c.Value))
	if e != nil {
		t.Fatal(e)
	}
	l, e := s.Claim(f.ctx, u, chat.UUID())
	if e != nil {
		t.Fatal(e)
	}
	outsider, otherLease, _ := n1User(t, f, s, "n1spatialoutsider")
	connections := []chat.DispatchConnection{{Identity: u, Lease: l}, {Identity: outsider, Lease: otherLease}}
	sector := f.integer(`SELECT current_sector FROM planet_visits WHERE user_id=$1`, u.ID)
	if _, e = s.History(f.ctx, u, chat.Target{Kind: "sector", Target: fmt.Sprintf("%d/%d", surface, sector)}, 0, ""); e != nil {
		t.Fatal(e)
	}
	var channel string
	if e = f.db.QueryRow(f.ctx, `SELECT id::text FROM chat_channels WHERE kind='sector' AND surface_id=$1 AND sector_number=$2`, surface, sector).Scan(&channel); e != nil {
		t.Fatal(e)
	}
	allowed, e := s.ResolveRecipients(f.ctx, []chat.DispatchEvent{{ID: 1, Channel: &channel}}, connections)
	if e != nil || !allowed[u.ID] || allowed[outsider.ID] {
		t.Fatal("spatial audience", allowed, e)
	}
	other := chat.UUID()
	f.exec(`INSERT INTO chat_channels(id,kind,surface_id,sector_number) VALUES($1,'sector',$2,$3)`, other, surface, sector+1)
	allowed, e = s.ResolveRecipients(f.ctx, []chat.DispatchEvent{{ID: 2, Channel: &other}}, connections)
	if e != nil || len(allowed) != 0 {
		t.Fatal("other sector exposed", allowed, e)
	}
	stopPlanet()
	allowed, e = s.ResolveRecipients(f.ctx, []chat.DispatchEvent{{ID: 3, Channel: &channel}}, connections)
	if e != nil || len(allowed) != 0 {
		t.Fatal("stale/disconnected presence", allowed, e)
	}
}
