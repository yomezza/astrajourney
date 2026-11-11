package integration

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/coder/websocket"
	"github.com/nats-io/nats.go"
	"github.com/yomezza/cosmosimulator/internal/platform/gamesignal"
	"github.com/yomezza/cosmosimulator/internal/testkit/natsfixture"
)

func n6API(t *testing.T, base, path string, c *http.Cookie, in any, want int) map[string]any {
	t.Helper()
	var body io.Reader
	method := "GET"
	if in != nil {
		b, e := json.Marshal(in)
		if e != nil {
			t.Fatal(e)
		}
		body = bytes.NewReader(b)
		method = "POST"
	}
	r, e := http.NewRequest(method, base+path, body)
	if e != nil {
		t.Fatal(e)
	}
	r.Header.Set("Origin", base)
	r.Header.Set("Content-Type", "application/json")
	r.AddCookie(c)
	res, e := (&http.Client{Timeout: 8 * time.Second}).Do(r)
	if e != nil {
		t.Fatal(e)
	}
	defer res.Body.Close()
	raw, _ := io.ReadAll(io.LimitReader(res.Body, 1<<20))
	if res.StatusCode != want {
		t.Fatalf("%s status=%d want=%d: %s", path, res.StatusCode, want, raw)
	}
	var v map[string]any
	json.Unmarshal(raw, &v)
	return v
}
func n6Event(t *testing.T, events <-chan gamesignal.Message, subject string) gamesignal.Message {
	t.Helper()
	timer := time.NewTimer(5 * time.Second)
	defer timer.Stop()
	for {
		select {
		case m := <-events:
			if m.Subject == subject {
				return m
			}
		case <-timer.C:
			t.Fatal("missing N6 Core event", subject)
			return gamesignal.Message{}
		}
	}
}
func n6NoEvent(t *testing.T, events <-chan gamesignal.Message, subject string) {
	t.Helper()
	timer := time.NewTimer(200 * time.Millisecond)
	defer timer.Stop()
	for {
		select {
		case m := <-events:
			if m.Subject == subject {
				t.Fatal("rollback published N6 event", subject)
			}
		case <-timer.C:
			return
		}
	}
}
func n6Command(t *testing.T, c *n1Client, kind string, payload any) map[string]any {
	t.Helper()
	id := op180(t)
	raw, _ := json.Marshal(map[string]any{"type": kind, "request_id": id, "payload": payload})
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if e := c.conn.Write(ctx, websocket.MessageText, raw); e != nil {
		t.Fatal(e)
	}
	select {
	case ack := <-c.acks:
		if ack["request_id"] != id {
			t.Fatal("wrong ACK")
		}
		return ack
	case <-ctx.Done():
		t.Fatal("missing ACK")
		return nil
	}
}
func n6Bootstrap(t *testing.T, chat, origin string, c *http.Cookie) map[string]any {
	t.Helper()
	status, raw := n5HTTP(t, chat, "bootstrap", origin, c)
	if status != 200 {
		t.Fatal("bootstrap", status)
	}
	var v map[string]any
	if json.Unmarshal(raw, &v) != nil {
		t.Fatal("bootstrap JSON")
	}
	return v
}
func TestN6NativeThreeServices(t *testing.T) {
	t.Setenv("CHAT_ENABLED", "true")
	f, owner, uid, _, site, ship := prepare180(t)
	member, memberID := f.signup("n6member")
	f.exec(`UPDATE users SET role='admin' WHERE id=$1`, uid)
	var schema string
	if e := f.db.QueryRow(f.ctx, `SELECT current_schema()`).Scan(&schema); e != nil {
		t.Fatal(e)
	}
	dsn := os.Getenv("TEST_DATABASE_URL")
	b := natsfixture.New(t, natsfixture.Options{N6: true})
	cosmicBinary := n5Build(t, "./cmd/astra-journey", "cosmic", false)
	planetBinary := n5Build(t, "./cmd/astra-journey-planet", "planet", false)
	chatBinary := n5Build(t, "./cmd/astra-journey-chat", "chat", false)
	cosmicAddr, planetAddr, chatAddr := n5Port(t), n5Port(t), n5Port(t)
	cosmic, planet, chat := "http://"+cosmicAddr, "http://"+planetAddr, "http://"+chatAddr
	natsDoc := func(identity, secret string) map[string]any {
		return map[string]any{"enabled": true, "service_identity": identity, "username": identity, "servers": []string{b.URL}, "password_file": secret, "ca_file": b.CAFile, "connect_timeout": "500ms", "reconnect_wait": "250ms"}
	}
	cosmicDoc := map[string]any{"database": map[string]any{"url": dsn, "schema": schema}, "cosmos": map[string]any{"http_addr": cosmicAddr, "public_origin": cosmic, "planet_enabled": true, "chat_enabled": true, "planet_internal_url": planet, "chat_internal_url": chat}, "nats": natsDoc("cosmos", b.CosmosPasswordFile)}
	cosmicConfig := n5Config(t, cosmicDoc)
	stopCosmic := n5Process(t, cosmicBinary, cosmicConfig, cosmic+"/api/health")
	// Cosmic initialization is complete before establishing existing restricted Chat roles.
	_, role, _ := n1RuntimePool(t, f)
	u, e := url.Parse(dsn)
	if e != nil {
		t.Fatal(e)
	}
	q := u.Query()
	q.Set("role", role)
	u.RawQuery = q.Encode()
	planetDoc := map[string]any{"database": map[string]any{"url": dsn, "schema": schema}, "planet": map[string]any{"http_addr": planetAddr, "allowed_origins": []string{cosmic, "http://127.0.0.1:8080"}, "db_timeout": "200ms", "lease_ttl": "3s", "lease_renew": "1s", "shutdown_timeout": "2s"}, "nats": natsDoc("planet", b.PlanetPasswordFile)}
	stopPlanet := n5Process(t, planetBinary, n5Config(t, planetDoc), planet+"/planet/api/v1/health/ready")
	chatDoc := map[string]any{"database": map[string]any{"url": u.String(), "schema": schema}, "chat": map[string]any{"http_addr": chatAddr, "cosmic_internal_url": cosmic, "planet_internal_url": planet, "allowed_origins": []string{cosmic}, "erasure_journal": filepath.Join(t.TempDir(), "erasure.log"), "dispatcher_mode": "b1", "fallback_poll_interval": "10s", "degraded_poll_interval": "500ms"}, "nats": natsDoc("chat", b.PasswordFile)}
	stopChat := n5Process(t, chatBinary, n5Config(t, chatDoc), chat+"/chat/api/v1/health")
	wire := n5Wire(t, b)
	events := make(chan gamesignal.Message, 64)
	initialEvents := events
	sub, e := wire.Subscribe("cosmos.v1.signal.>", func(m *nats.Msg) {
		select {
		case initialEvents <- gamesignal.Message{Subject: m.Subject, Data: append([]byte(nil), m.Data...)}:
		default:
		}
	})
	if e != nil {
		t.Fatal(e)
	}
	defer sub.Unsubscribe()
	wire.FlushTimeout(time.Second)
	pilot, other := n1Dial(t, chat, cosmic, owner.Value), n1Dial(t, chat, cosmic, member.Value)
	eventuallyN1(t, func() bool { return pilot.syncs.Load() > 0 && other.syncs.Load() > 0 })
	// A separate process provides the gateway as well as Cosmic readiness.
	status, _ := n5HTTP(t, cosmic, "bootstrap", cosmic, owner)
	if status != 200 {
		t.Fatal("existing Chat gateway", status)
	}
	t.Log("three native -race processes ready; existing HTTP/WS routes retained")
	companyAction := func(c *http.Cookie, in any) {
		n6API(t, cosmic, "/api/companies", c, in, 200)
		n6Event(t, events, gamesignal.MembershipSubject)
	}
	companyAction(owner, map[string]any{"action": "leave"})
	companyAction(owner, map[string]any{"action": "create", "name": "N6 native corporation", "tag": "N6C", "color": "#77aa88", "flag": "◈"})
	company := f.integer(`SELECT company_id FROM users WHERE id=$1`, uid)
	companyAction(member, map[string]any{"action": "leave"})
	n6API(t, cosmic, "/api/workbench", member, map[string]any{"action": "apply", "id": company}, 200)
	request := f.integer(`SELECT id FROM company_requests WHERE user_id=$1`, memberID)
	n6API(t, cosmic, "/api/workbench", owner, map[string]any{"action": "request_accept", "id": request}, 200)
	membership := n6Event(t, events, gamesignal.MembershipSubject)
	if f.integer(`SELECT company_id FROM users WHERE id=$1`, memberID) != company {
		t.Fatal("membership signal before commit")
	}
	n6API(t, cosmic, "/api/workbench", owner, map[string]any{"action": "role_save", "name": "N6 moderator", "permissions": []string{"chat.moderate"}}, 200)
	n6Event(t, events, gamesignal.PermissionsSubject)
	customRole := f.integer(`SELECT id FROM company_roles WHERE company_id=$1`, company)
	before := other.syncs.Load()
	n6API(t, cosmic, "/api/workbench", owner, map[string]any{"action": "role_assign", "id": memberID, "amount": customRole}, 200)
	oldRole := n6Event(t, events, gamesignal.RoleSubject)
	eventuallyN1(t, func() bool { return other.syncs.Load() > before })
	view := n6Bootstrap(t, chat, cosmic, member)
	if !strings.Contains(fmt.Sprint(view["corporation"]), "chat.moderate") {
		t.Fatal("role permission not visible")
	}
	// Real deferred COMMIT failure, after the role update and fence revision.
	f.exec(`CREATE SEQUENCE n6_commit_attempts`)
	f.exec(`GRANT USAGE ON SEQUENCE n6_commit_attempts TO ` + role)
	f.exec(`CREATE FUNCTION n6_abort() RETURNS trigger LANGUAGE plpgsql AS $$ BEGIN PERFORM nextval('n6_commit_attempts'); RAISE EXCEPTION 'n6_injected_commit_failure'; END $$`)
	f.exec(`CREATE CONSTRAINT TRIGGER n6_role_rollback AFTER UPDATE ON company_roles DEFERRABLE INITIALLY DEFERRED FOR EACH ROW EXECUTE FUNCTION n6_abort()`)
	n6API(t, cosmic, "/api/workbench", owner, map[string]any{"action": "role_save", "id": customRole, "name": "N6 moderator", "permissions": []string{}}, 500)
	n6NoEvent(t, events, gamesignal.PermissionsSubject)
	if f.integer(`SELECT last_value FROM n6_commit_attempts`) != 1 {
		t.Fatal("deferred role COMMIT failure was not reached")
	}
	f.exec(`DROP TRIGGER n6_role_rollback ON company_roles`)
	if !strings.Contains(fmt.Sprint(n6Bootstrap(t, chat, cosmic, member)["corporation"]), "chat.moderate") {
		t.Fatal("rollback leaked role revoke")
	}
	before = other.syncs.Load()
	n6API(t, cosmic, "/api/workbench", owner, map[string]any{"action": "role_save", "id": customRole, "name": "N6 moderator", "permissions": []string{}}, 200)
	n6Event(t, events, gamesignal.PermissionsSubject)
	eventuallyN1(t, func() bool { return other.syncs.Load() > before })
	if strings.Contains(fmt.Sprint(n6Bootstrap(t, chat, cosmic, member)["corporation"]), "chat.moderate") {
		t.Fatal("permission revocation stale")
	}
	if status, _ := n5HTTP(t, chat, fmt.Sprintf("corporate.moderation?target=%d", company), cosmic, member); status != 403 {
		t.Fatal("revoked corporate moderation API", status)
	}
	// Include an UNSEEN ID carrying the old revision, not only a cached duplicate.
	oldData, _ := gamesignal.DecodeCompany(oldRole.Subject, oldRole.Data)
	unseenOld, _ := gamesignal.EncodeCompany(oldRole.Subject, oldData.Data)
	// Old/duplicate membership and role signals cannot restore prior permissions.
	for _, m := range []gamesignal.Message{oldRole, membership, unseenOld, oldRole} {
		wire.Publish(m.Subject, m.Data)
	}
	wire.FlushTimeout(time.Second)
	if strings.Contains(fmt.Sprint(n6Bootstrap(t, chat, cosmic, member)["corporation"]), "chat.moderate") {
		t.Fatal("old hint restored permissions")
	}
	for _, grant := range []bool{true, false} {
		before = other.syncs.Load()
		n6API(t, cosmic, "/api/admin/chat/moderator", owner, map[string]any{"user": fmt.Sprint(memberID), "granted": grant}, 200)
		n6Event(t, events, gamesignal.ModeratorSubject)
		eventuallyN1(t, func() bool { return other.syncs.Load() > before })
		user := n6Bootstrap(t, chat, cosmic, member)["user"].(map[string]any)
		if user["game_moderator"] != grant {
			t.Fatal("moderator stale")
		}
	}
	// Start/resume/disconnect/end from actual Planet COMMITs, no state from hints.
	ticket := n6API(t, cosmic, "/api/planet/handoff", owner, map[string]any{"operation": op180(t), "landing_site": fmt.Sprint(site), "ship": fmt.Sprint(ship)}, 200)
	conn, w := ws180(t, planet, owner, map[string]any{"ticket": ticket["ticket"]})
	started := n6Event(t, events, gamesignal.VisitStartedSubject)
	if n5Presence(t, chat, cosmic, owner) != 1 {
		t.Fatal("visit start scope")
	}
	conn.CloseNow()
	n6Event(t, events, gamesignal.VisitDisconnectedSubject)
	if n5Presence(t, chat, cosmic, owner) != 0 {
		t.Fatal("disconnected spatial permission")
	}
	conn, w = ws180(t, planet, owner, map[string]any{"resume_visit": w["visit"]})
	n6Event(t, events, gamesignal.VisitResumedSubject)
	command190(t, conn, w, 2, "leave", map[string]any{"operation": op180(t)})
	read190(t, conn, "visit.ended")
	n6Event(t, events, gamesignal.VisitEndedSubject)
	wire.Publish(started.Subject, started.Data)
	wire.FlushTimeout(time.Second)
	if n5Presence(t, chat, cosmic, owner) != 0 {
		t.Fatal("old start resurrected ended visit")
	}
	t.Log("membership, role/permission/moderator changes, deferred rollback, visit lifecycle and old hints PASS")
	// Private and Global continue to work. Corporation channel exists for cleanup.
	ack := n6Command(t, pilot, "message.send", map[string]any{"kind": "corporation", "body": "N6 corporation data"})
	if ack["error"] != nil {
		t.Fatal("corporation ACK", ack)
	}
	ack = n6Command(t, pilot, "private.create", map[string]any{"kind": "dm", "user": fmt.Sprint(memberID)})
	if ack["error"] != nil {
		t.Fatal("Private ACK", ack)
	}
	// A failed receipt COMMIT must publish nothing and never delete/refund company.
	f.exec(`CREATE CONSTRAINT TRIGGER n6_cleanup_rollback AFTER INSERT ON chat_company_cleanup_receipts DEFERRABLE INITIALLY DEFERRED FOR EACH ROW EXECUTE FUNCTION n6_abort()`)
	n6API(t, cosmic, "/api/workbench", owner, map[string]any{"action": "company_dissolve"}, 202)
	deletion := n6Event(t, events, gamesignal.DeletionSubject)
	deadline := time.Now().Add(15 * time.Second)
	for f.integer(`SELECT last_value FROM n6_commit_attempts`) < 2 {
		if time.Now().After(deadline) {
			t.Fatal("deferred receipt COMMIT failure was not reached")
		}
		time.Sleep(10 * time.Millisecond)
	}
	n6NoEvent(t, events, gamesignal.CleanupSubject)
	operation := ""
	f.db.QueryRow(f.ctx, `SELECT operation_id::text FROM chat_company_deletion_requests WHERE company_id=$1`, company).Scan(&operation)
	fake, _ := gamesignal.EncodeCompany(gamesignal.CleanupSubject, gamesignal.CompanyChange{CompanyID: fmt.Sprint(company), Operation: operation})
	wire.Publish(fake.Subject, fake.Data)
	wire.FlushTimeout(time.Second)
	n6Event(t, events, gamesignal.CleanupSubject)
	n6NoEvent(t, events, gamesignal.CleanupSubject)
	if f.integer(`SELECT count(*) FROM companies WHERE id=$1`, company) != 1 || f.integer(`SELECT count(*) FROM chat_company_cleanup_receipts WHERE company_id=$1`, company) != 0 {
		t.Fatal("Core hint replaced durable receipt")
	}
	f.exec(`DROP TRIGGER n6_cleanup_rollback ON chat_company_cleanup_receipts`)
	fresh, _ := gamesignal.DecodeCompany(deletion.Subject, deletion.Data)
	retry, _ := gamesignal.EncodeCompany(deletion.Subject, fresh.Data)
	wire.Publish(retry.Subject, retry.Data)
	wire.FlushTimeout(time.Second)
	cleanup := n6Event(t, events, gamesignal.CleanupSubject)
	receipt, ok := gamesignal.DecodeCompany(cleanup.Subject, cleanup.Data)
	if !ok || receipt.Data.Operation != operation || f.integer(`SELECT count(*) FROM chat_company_cleanup_receipts WHERE company_id=$1 AND operation_id=$2`, company, operation) != 1 {
		t.Fatal("cleanup signal before durable receipt")
	}
	wait180(t, f, `SELECT count(*) FROM companies WHERE id=$1`, 0, company)
	balance := f.integer(`SELECT credits FROM users WHERE id=$1`, uid)
	wire.Publish(cleanup.Subject, cleanup.Data)
	wire.Publish(cleanup.Subject, fake.Data)
	wire.FlushTimeout(time.Second)
	if f.integer(`SELECT credits FROM users WHERE id=$1`, uid) != balance {
		t.Fatal("duplicate cleanup refunded twice")
	}
	if corp := n6Bootstrap(t, chat, cosmic, member)["corporation"].([]any); len(corp) != 0 {
		t.Fatal("deleted company retained access")
	}
	t.Log("deletion request/failed receipt/fake hint/confirmed receipt/native finalization PASS")
	// Broker outage preserves all three readiness and normal SQL actions.
	b.Stop()
	companyActionOffline := func(action string) { n6API(t, cosmic, "/api/companies", member, map[string]any{"action": action}, 200) }
	// Member is now without a corporation, join starter via real API.
	starter := f.integer(`SELECT id FROM companies WHERE starter LIMIT 1`)
	n6API(t, cosmic, "/api/companies", member, map[string]any{"action": "join", "id": starter}, 200)
	companyActionOffline("leave")
	if f.integer(`SELECT count(*) FROM users WHERE id=$1 AND company_id IS NULL`, memberID) != 1 {
		t.Fatal("broker outage blocked game commit")
	}
	wait180(t, f, `SELECT count(*) FROM planet_transfer_controls WHERE user_id=$1 AND reserved_ship IS NOT NULL`, 0, uid)
	offlineTicket := n6API(t, cosmic, "/api/planet/handoff", owner, map[string]any{"operation": op180(t), "landing_site": fmt.Sprint(site), "ship": fmt.Sprint(ship)}, 200)
	offlineConn, offlineWelcome := ws180(t, planet, owner, map[string]any{"ticket": offlineTicket["ticket"]})
	if offlineWelcome["message_type"] != "welcome" {
		t.Fatal("offline Planet activation")
	}
	offlineConn.CloseNow()
	wait180(t, f, `SELECT count(*) FROM planet_visits WHERE user_id=$1 AND state='disconnected'`, 1, uid)
	offlineConn, offlineWelcome = ws180(t, planet, owner, map[string]any{"resume_visit": offlineWelcome["visit"]})
	command190(t, offlineConn, offlineWelcome, 2, "leave", map[string]any{"operation": op180(t)})
	read190(t, offlineConn, "visit.ended")
	if n5Presence(t, chat, cosmic, owner) != 0 {
		t.Fatal("offline lifecycle fallback")
	}
	ack = n6Command(t, other, "message.send", map[string]any{"kind": "global", "body": "N6 broker offline"})
	if ack["error"] != nil {
		t.Fatal("offline Global", ack)
	}
	b.Start()
	wire = n5Wire(t, b)
	events = make(chan gamesignal.Message, 64)
	reconnectedEvents := events
	sub2, e := wire.Subscribe("cosmos.v1.signal.>", func(m *nats.Msg) {
		select {
		case reconnectedEvents <- gamesignal.Message{Subject: m.Subject, Data: append([]byte(nil), m.Data...)}:
		default:
		}
	})
	if e != nil {
		t.Fatal(e)
	}
	defer sub2.Unsubscribe()
	wire.FlushTimeout(time.Second)
	// A fresh hint after reconnect proves all participants recover via own supervisors.
	time.Sleep(700 * time.Millisecond)
	n6API(t, cosmic, "/api/companies", member, map[string]any{"action": "join", "id": starter}, 200)
	n6Event(t, events, gamesignal.MembershipSubject)
	// Roll back all services to disabled NATS / Chat A1; persisted data survives.
	stopChat()
	stopPlanet()
	stopCosmic()
	b.Stop()
	cosmicDoc["nats"] = map[string]any{"enabled": false}
	planetDoc["nats"] = map[string]any{"enabled": false}
	chatDoc["nats"] = map[string]any{"enabled": false}
	chatDoc["chat"].(map[string]any)["dispatcher_mode"] = "a1"
	n5Process(t, cosmicBinary, n5Config(t, cosmicDoc), cosmic+"/api/health")
	// Keep the same restricted role and SQL grants across disabled/A1 rollback.
	n5Process(t, planetBinary, n5Config(t, planetDoc), planet+"/planet/api/v1/health/ready")
	n5Process(t, chatBinary, n5Config(t, chatDoc), chat+"/chat/api/v1/health")
	c2 := n1Dial(t, chat, cosmic, member.Value)
	ack = n6Command(t, c2, "favorite.note", map[string]any{"body": "N6 rollback preserved service"})
	if ack["error"] != nil {
		t.Fatal("A1 rollback", ack)
	}
	if f.integer(`SELECT count(*) FROM chat_messages WHERE body='N6 broker offline'`) != 1 {
		t.Fatal("outage data lost")
	}
	t.Log("NATS stop/restart and all-service disabled/A1 rollback PASS; no benchmark")
}
