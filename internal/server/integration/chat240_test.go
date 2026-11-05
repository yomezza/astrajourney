package integration

import (
	"context"
	"encoding/json"
	"fmt"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/yomezza/cosmosimulator/internal/account"
	"github.com/yomezza/cosmosimulator/internal/chat"
	"github.com/yomezza/cosmosimulator/internal/chatintegration"
	"path/filepath"
	"strings"
	"sync"
	"testing"
)

func TestChat240Transactions(t *testing.T) {
	t.Setenv("CHAT_ENABLED", "true")
	f := newFixture033(t)
	s := &chat.Service{DB: f.db}
	j, e := chat.OpenJournal(f.ctx, s, filepath.Join(t.TempDir(), "erasure.log"))
	if e != nil {
		t.Fatal(e)
	}
	defer j.Close()
	s.Journal = j
	alice, aid := f.signup("chat_alice")
	bob, bid := f.signup("chat_bob")
	carl, cid := f.signup("chat_carl")
	_ = cid
	identity := func(token string) chat.Identity {
		u, e := s.Authenticate(f.ctx, account.TokenHash(token))
		if e != nil {
			t.Fatal(e)
		}
		return u
	}
	a, b, c := identity(alice.Value), identity(bob.Value), identity(carl.Value)
	lease := func(u chat.Identity) chat.Lease {
		l, e := s.Claim(f.ctx, u, chat.UUID())
		if e != nil {
			t.Fatal(e)
		}
		return l
	}
	la, lb, lc := lease(a), lease(b), lease(c)
	if _, e = s.Claim(f.ctx, a, chat.UUID()); e == nil {
		t.Fatal("second connection stole lease")
	}
	run := func(u chat.Identity, l chat.Lease, typ string, in any) map[string]any {
		raw, _ := json.Marshal(in)
		result, e := s.Execute(f.ctx, u, l, chat.Command{Type: typ, Request: chat.UUID(), Payload: raw})
		if e != nil {
			t.Fatalf("%s: %v", typ, e)
		}
		v := map[string]any{}
		if json.Unmarshal(result, &v) != nil {
			t.Fatal(string(result))
		}
		return v
	}
	bad := func(u chat.Identity, l chat.Lease, typ string, in any, want string) {
		raw, _ := json.Marshal(in)
		_, e := s.Execute(f.ctx, u, l, chat.Command{Type: typ, Request: chat.UUID(), Payload: raw})
		if e == nil || e.Error() != want {
			t.Fatalf("%s want %s got %v", typ, want, e)
		}
	}
	boot, e := s.Query(f.ctx, a, "bootstrap", chat.Target{}, "", "", "")
	if e != nil {
		t.Fatal(e)
	}
	_ = boot
	v := run(a, la, "message.send", map[string]any{"kind": "global", "body": "Привет 世界 💫"})
	mid := v["message_id"].(string)
	history, e := s.History(f.ctx, b, chat.Target{Kind: "global"}, 0, "")
	if e != nil || len(history) != 1 {
		t.Fatalf("history: %v %v", history, e)
	}
	bad(a, la, "message.send", map[string]any{"kind": "global", "body": "again"}, "rate_limited")
	run(b, lb, "favorite.copy", map[string]any{"id": mid})
	run(a, la, "message.delete", map[string]any{"id": mid})
	if n := f.integer(`SELECT count(*) FROM chat_saved_items WHERE source_message_id=$1`, mid); n != 1 {
		t.Fatal("author delete erased saved copy")
	}
	raw, _ := json.Marshal(map[string]any{"body": "Personal note"})
	cmd := chat.Command{Type: "favorite.note", Request: chat.UUID(), Payload: raw}
	r1, e := s.Execute(f.ctx, a, la, cmd)
	if e != nil {
		t.Fatal(e)
	}
	r2, e := s.Execute(f.ctx, a, la, cmd)
	if e != nil || !jsonEqual240(r1, r2) {
		t.Fatal("replay", e)
	}
	cmd.Payload = json.RawMessage(`{"body":"different"}`)
	if _, e = s.Execute(f.ctx, a, la, cmd); e == nil {
		t.Fatal("digest conflict accepted")
	}
	dm := run(a, la, "private.create", map[string]any{"kind": "dm", "user": fmt.Sprint(bid)})["conversation"].(string)
	run(b, lb, "private.create", map[string]any{"kind": "dm", "user": fmt.Sprint(aid)})
	pv := run(a, la, "message.send", map[string]any{"kind": "private", "target": dm, "body": "Private message"})
	pid := pv["message_id"].(string)
	if f.integer(`SELECT unread_count FROM chat_members WHERE user_id=$1 AND conversation_id=$2`, bid, dm) != 1 {
		t.Fatal("unread")
	}
	run(b, lb, "read", map[string]any{"kind": "private", "target": dm, "ids": []string{pid}})
	if f.integer(`SELECT unread_count FROM chat_members WHERE user_id=$1 AND conversation_id=$2`, bid, dm) != 0 {
		t.Fatal("receipt")
	}
	if _, e = s.History(f.ctx, c, chat.Target{Kind: "private", Target: dm}, 0, ""); e == nil {
		t.Fatal("private leaked")
	}
	run(a, la, "private.invite", map[string]any{"target": dm, "user": fmt.Sprint(cid)})
	var invite string
	if e = f.db.QueryRow(f.ctx, `SELECT id::text FROM chat_invites WHERE conversation_id=$1 AND recipient_id=$2 AND status='pending'`, dm, cid).Scan(&invite); e != nil {
		t.Fatal(e)
	}
	run(c, lc, "private.decide", map[string]any{"id": invite, "decision": "accept"})
	if f.integer(`SELECT count(*) FROM chat_dm_pairs WHERE conversation_id=$1`, dm) != 0 {
		t.Fatal("pair not freed")
	}
	run(b, lb, "private.create", map[string]any{"kind": "dm", "user": fmt.Sprint(aid)})
	h, e := s.History(f.ctx, c, chat.Target{Kind: "private", Target: dm}, 0, "")
	if e != nil || len(h) < 2 {
		t.Fatal("join history", e)
	}
	if f.integer(`SELECT unread_count FROM chat_members WHERE user_id=$1 AND conversation_id=$2`, cid, dm) != 0 {
		t.Fatal("historical unread")
	}
	run(c, lc, "favorite.copy", map[string]any{"id": pid})
	run(a, la, "private.delete", map[string]any{"target": dm, "confirm": true})
	for i := 0; i < 3; i++ {
		if e = s.Maintain(context.Background()); e != nil {
			t.Fatal(e)
		}
	}
	if f.integer(`SELECT count(*) FROM chat_saved_items WHERE source_message_id=$1`, pid) != 1 {
		t.Fatal("group deletion removed independent copy")
	}
	f.exec(`UPDATE users SET role='admin' WHERE id=$1`, aid)
	a = identity(alice.Value)
	m := run(b, lb, "message.send", map[string]any{"kind": "global", "body": "Moderated test"})["message_id"].(string)
	run(c, lc, "favorite.copy", map[string]any{"id": m})
	run(a, la, "message.delete", map[string]any{"id": m, "reason": "Spam"})
	if f.integer(`SELECT count(*) FROM chat_saved_items WHERE source_message_id=$1`, m) != 0 {
		t.Fatal("moderation copy survived")
	}
	// Restore an erased copy then replay the independent erasure journal.
	f.exec(`INSERT INTO chat_saved_items(id,owner_id,kind,body,source_message_id) VALUES($1,$2,'copy','Restored old backup',$3)`, chat.UUID(), bid, m)
	if e = j.Replay(f.ctx, s); e != nil {
		t.Fatal(e)
	}
	if f.integer(`SELECT count(*) FROM chat_saved_items WHERE source_message_id=$1`, m) != 0 {
		t.Fatal("restore resurrected copy")
	}
	bad(a, la, "message.send", map[string]any{"kind": "global", "body": strings.Repeat("🛰", 257)}, "invalid_argument")
	f.exec(`UPDATE chat_ws_leases SET expires_at=clock_timestamp()-interval '1 second' WHERE user_id=$1`, aid)
	bad(a, la, "favorite.note", map[string]any{"body": "stale"}, "stale_connection")
}

func jsonEqual240(a, b []byte) bool {
	var x, y any
	json.Unmarshal(a, &x)
	json.Unmarshal(b, &y)
	ax, _ := json.Marshal(x)
	by, _ := json.Marshal(y)
	return string(ax) == string(by)
}

func TestChat240RestrictedRole(t *testing.T) {
	t.Setenv("CHAT_ENABLED", "true")
	f := newFixture033(t)
	cookie, id := f.signup("chat_role")
	var schema string
	if e := f.db.QueryRow(f.ctx, `SELECT current_schema()`).Scan(&schema); e != nil {
		t.Fatal(e)
	}
	suffix := strings.ReplaceAll(chat.UUID(), "-", "")
	runtimeRole, definerRole := "chatrt_"+suffix, "chatdef_"+suffix
	f.exec(`CREATE ROLE ` + pgx.Identifier{runtimeRole}.Sanitize() + ` NOLOGIN NOINHERIT`)
	f.exec(`CREATE ROLE ` + pgx.Identifier{definerRole}.Sanitize() + ` NOLOGIN NOINHERIT`)
	t.Cleanup(func() {
		for _, r := range []string{runtimeRole, definerRole} {
			f.db.Exec(f.ctx, `REASSIGN OWNED BY `+pgx.Identifier{r}.Sanitize()+` TO CURRENT_USER`)
			f.db.Exec(f.ctx, `DROP OWNED BY `+pgx.Identifier{r}.Sanitize())
			f.db.Exec(f.ctx, `DROP ROLE `+pgx.Identifier{r}.Sanitize())
		}
	})
	tx, e := f.db.Begin(f.ctx)
	if e != nil {
		t.Fatal(e)
	}
	if e = chatintegration.GrantRuntime(f.ctx, tx, schema, runtimeRole, definerRole); e != nil {
		tx.Rollback(f.ctx)
		t.Fatal(e)
	}
	if e = tx.Commit(f.ctx); e != nil {
		t.Fatal(e)
	}
	config := f.db.Config().Copy()
	config.AfterConnect = func(ctx context.Context, c *pgx.Conn) error {
		_, e := c.Exec(ctx, `SET ROLE `+pgx.Identifier{runtimeRole}.Sanitize())
		return e
	}
	pool, e := pgxpool.NewWithConfig(f.ctx, config)
	if e != nil {
		t.Fatal(e)
	}
	defer pool.Close()
	s := &chat.Service{DB: pool}
	u, e := s.Authenticate(f.ctx, account.TokenHash(cookie.Value))
	if e != nil {
		t.Fatal("session EXECUTE", e)
	}
	l, e := s.Claim(f.ctx, u, chat.UUID())
	if e != nil {
		t.Fatal(e)
	}
	payload := json.RawMessage(`{"kind":"corporation","body":"Scoped runtime access"}`)
	if _, e = s.Execute(f.ctx, u, l, chat.Command{Type: "message.send", Request: chat.UUID(), Payload: payload}); e != nil {
		t.Fatal("send via narrow functions", e)
	}
	if _, e = s.History(f.ctx, u, chat.Target{Kind: "corporation"}, 0, ""); e != nil {
		t.Fatal(e)
	}
	for _, q := range []string{`UPDATE users SET credits=credits+1 WHERE id=$1`, `UPDATE planet_visits SET connection_epoch=connection_epoch+1 WHERE user_id=$1`, `SELECT id FROM users WHERE id=$1 FOR SHARE`, `SELECT password_hash FROM users WHERE id=$1`, `UPDATE chat_company_access_fences SET revision=revision+1 WHERE company_id=$1`} {
		if _, e = pool.Exec(f.ctx, q, id); e == nil {
			t.Fatalf("forbidden privilege: %s", q)
		}
	}
}

func TestChat240Search(t *testing.T) {
	t.Setenv("CHAT_ENABLED", "true")
	f := newFixture033(t)
	cookie, _ := f.signup("chat_search")
	s := &chat.Service{DB: f.db}
	u, e := s.Authenticate(f.ctx, account.TokenHash(cookie.Value))
	if e != nil {
		t.Fatal(e)
	}
	l, e := s.Claim(f.ctx, u, chat.UUID())
	if e != nil {
		t.Fatal(e)
	}
	r, e := s.Execute(f.ctx, u, l, chat.Command{Type: "message.send", Request: chat.UUID(), Payload: json.RawMessage(`{"kind":"global","body":"ЁЖИК MixedCase 100% a_b c\\d"}`)})
	if e != nil {
		t.Fatal(e)
	}
	var ack map[string]string
	json.Unmarshal(r, &ack)
	channel := ack["channel_id"]
	// Real partition/index, over 100k rows; no generated second search body.
	f.exec(`INSERT INTO chat_messages(kind,sent_at,id,expires_at,channel_id,seq,body) SELECT 'global',clock_timestamp(),gen_random_uuid(),clock_timestamp()+interval '30 days',$1,i+1,'Обычное сообщение космического терминала '||i::text FROM generate_series(1,100001)i`, channel)
	f.exec(`UPDATE chat_channels SET next_seq=100002 WHERE id=$1`, channel)
	f.exec(`ANALYZE chat_messages`)
	for _, query := range []string{"ёжик", "mixedcase", "100%", "a_b", `c\d`} {
		m, e := s.History(f.ctx, u, chat.Target{Kind: "global"}, 0, query)
		if e != nil || len(m) != 1 {
			t.Fatalf("literal Unicode search %q count=%d err=%v", query, len(m), e)
		}
	}
	var plan []byte
	e = f.db.QueryRow(f.ctx, `EXPLAIN(ANALYZE,BUFFERS,FORMAT JSON) SELECT id FROM chat_messages WHERE kind='global' AND channel_id=$1 AND sent_at>=transaction_timestamp()-interval '30 days' AND lower(body COLLATE chat_unicode) LIKE lower('%ёжик%' COLLATE chat_unicode) ORDER BY seq DESC LIMIT 50`, channel).Scan(&plan)
	if e != nil {
		t.Fatal(e)
	}
	if !strings.Contains(string(plan), "Bitmap Index Scan") {
		t.Fatalf("selective trigram index not used: %.800s", plan)
	}
	t.Logf("100002-row Unicode/escaped literal search uses Bitmap Index Scan; plan bytes=%d", len(plan))
}

func TestChat240CorporateFencesAndCleanup(t *testing.T) {
	t.Setenv("CHAT_ENABLED", "true")
	f := newFixture033(t)
	ownerCookie, owner := f.signup("chat_owner")
	memberCookie, member := f.signup("chat_member")
	s := &chat.Service{DB: f.db}
	j, e := chat.OpenJournal(f.ctx, s, filepath.Join(t.TempDir(), "journal"))
	if e != nil {
		t.Fatal(e)
	}
	defer j.Close()
	s.Journal = j
	var company int64
	if e = f.db.QueryRow(f.ctx, `INSERT INTO companies(name,tag,owner_id,credits) VALUES('Fence corporation','FCX',$1,100) RETURNING id`, owner).Scan(&company); e != nil {
		t.Fatal(e)
	}
	f.exec(`UPDATE users SET company_id=$1 WHERE id IN($2,$3)`, company, owner, member)
	user := func(token string) chat.Identity {
		u, e := s.Authenticate(f.ctx, account.TokenHash(token))
		if e != nil {
			t.Fatal(e)
		}
		return u
	}
	o, m := user(ownerCookie.Value), user(memberCookie.Value)
	lo, e := s.Claim(f.ctx, o, chat.UUID())
	if e != nil {
		t.Fatal(e)
	}
	lm, e := s.Claim(f.ctx, m, chat.UUID())
	if e != nil {
		t.Fatal(e)
	}
	execute := func(u chat.Identity, l chat.Lease, kind string, p any) (json.RawMessage, error) {
		raw, _ := json.Marshal(p)
		return s.Execute(f.ctx, u, l, chat.Command{Type: kind, Request: chat.UUID(), Payload: raw})
	}
	if _, e = execute(o, lo, "mute", map[string]any{"kind": "corporation", "target": fmt.Sprint(company), "user": fmt.Sprint(member), "duration": 300, "reason": "Spam"}); e != nil {
		t.Fatal(e)
	}
	if _, e = execute(m, lm, "message.send", map[string]any{"kind": "corporation", "body": "muted"}); e == nil || e.Error() != "muted" {
		t.Fatal("mute ineffective", e)
	}
	if _, e = s.History(f.ctx, m, chat.Target{Kind: "corporation"}, 0, ""); e != nil {
		t.Fatal("muted cannot read", e)
	}
	if _, e = execute(o, lo, "unmute", map[string]any{"target": fmt.Sprint(company), "user": fmt.Sprint(member), "reason": "Lifted"}); e != nil {
		t.Fatal(e)
	}
	var wg sync.WaitGroup
	errs := make(chan error, 100)
	for i := 0; i < 100; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, e := execute(m, lm, "message.send", map[string]any{"kind": "corporation", "body": "Concurrent old membership"})
			if e != nil && e.Error() != "rate_limited" && e.Error() != "access_denied" && e.Error() != "retry_later" {
				errs <- e
			}
		}()
	}
	f.exec(`UPDATE users SET company_id=NULL,company_role=NULL WHERE id=$1`, member)
	wg.Wait()
	close(errs)
	for e := range errs {
		t.Error(e)
	}
	if _, e = execute(m, lm, "message.send", map[string]any{"kind": "corporation", "body": "After leave"}); e == nil || e.Error() != "access_denied" {
		t.Fatal("stale membership", e)
	}
	// Deletion is only finalized by Cosmic after a matching Chat cleanup receipt.
	f.exec(`UPDATE chat_rate_state SET last_sent_at='-infinity' WHERE user_id=$1`, owner)
	raw, e := execute(o, lo, "message.send", map[string]any{"kind": "corporation", "body": "Company history"})
	if e != nil {
		t.Fatal(e)
	}
	var sent map[string]any
	json.Unmarshal(raw, &sent)
	if _, e = execute(o, lo, "favorite.copy", map[string]any{"id": sent["message_id"]}); e != nil {
		t.Fatal(e)
	}
	balance := f.integer(`SELECT credits FROM users WHERE id=$1`, owner)
	tx, e := f.db.Begin(f.ctx)
	if e != nil {
		t.Fatal(e)
	}
	if e = chatintegration.RequestCompany(f.ctx, tx, company); e != nil {
		t.Fatal(e)
	}
	if e = tx.Commit(f.ctx); e != nil {
		t.Fatal(e)
	}
	if _, e = execute(o, lo, "message.send", map[string]any{"kind": "corporation", "body": "deleting"}); e == nil {
		t.Fatal("send while deleting")
	}
	if e = f.app.Corporations.FinalizeChatDeletion(f.ctx); e != nil {
		t.Fatal(e)
	}
	if f.integer(`SELECT count(*) FROM companies WHERE id=$1`, company) != 1 {
		t.Fatal("early finalize")
	}
	for i := 0; i < 4; i++ {
		if e = s.Maintain(f.ctx); e != nil {
			t.Fatal(e)
		}
	}
	for i := 0; i < 2; i++ {
		if e = f.app.Corporations.FinalizeChatDeletion(f.ctx); e != nil {
			t.Fatal(e)
		}
	}
	if f.integer(`SELECT count(*) FROM companies WHERE id=$1`, company) != 0 {
		t.Fatal("company not finalized")
	}
	if f.integer(`SELECT credits FROM users WHERE id=$1`, owner) != balance+100 {
		t.Fatal("refund not exactly once")
	}
	if f.integer(`SELECT count(*) FROM chat_saved_items WHERE source_message_id=$1`, sent["message_id"]) != 1 {
		t.Fatal("corporate deletion removed independent copy")
	}
}

func TestChat240LateCommittedOutbox(t *testing.T) {
	t.Setenv("CHAT_ENABLED", "true")
	f := newFixture033(t)
	cookie, uid := f.signup("chat_delivery")
	s := &chat.Service{DB: f.db}
	u, e := s.Authenticate(f.ctx, account.TokenHash(cookie.Value))
	if e != nil {
		t.Fatal(e)
	}
	early, e := f.db.Begin(f.ctx)
	if e != nil {
		t.Fatal(e)
	}
	defer early.Rollback(f.ctx)
	if _, e = early.Exec(f.ctx, `INSERT INTO chat_outbox(event_kind,target_user_id) VALUES('late',$1)`, uid); e != nil {
		t.Fatal(e)
	}
	f.exec(`INSERT INTO chat_outbox(event_kind,target_user_id) VALUES('later_id',$1)`, uid)
	cursor, _, stamp, e := s.Changes(f.ctx, u, 0)
	if e != nil {
		t.Fatal(e)
	}
	if e = early.Commit(f.ctx); e != nil {
		t.Fatal(e)
	}
	_, _, next, e := s.Changes(f.ctx, u, cursor)
	if e != nil {
		t.Fatal(e)
	}
	if next == stamp {
		t.Fatal("late committed event lost behind sequence cursor")
	}
}
