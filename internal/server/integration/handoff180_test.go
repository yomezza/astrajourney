package integration

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/yomezza/cosmosimulator/internal/account"
	"github.com/yomezza/cosmosimulator/internal/planet/scenestore"
	"github.com/yomezza/cosmosimulator/internal/planet/visits"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/coder/websocket"
	"github.com/coder/websocket/wsjson"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/yomezza/cosmosimulator/internal/planet/access"
)

func op180(t *testing.T) string {
	t.Helper()
	s, err := access.UUID()
	if err != nil {
		t.Fatal(err)
	}
	return s
}
func prepare180(t *testing.T) (*fixture033, *http.Cookie, int64, int64, int64, int64) {
	t.Helper()
	f, c, uid, surface, _ := landingFixture160(t)
	addColony160(f, uid, surface, 1, nil, false)
	reconcile160(f)
	site := f.integer(`SELECT id FROM planet_landing_sites WHERE surface=$1`, surface)
	ship := f.integer(`SELECT id FROM ships WHERE user_id=$1 AND active`, uid)
	planet := f.integer(`SELECT planet FROM planet_surfaces WHERE id=$1`, surface)
	f.exec(`UPDATE ships SET object_id=$2,position_mode='orbit',destination=NULL,local_target=NULL,waypoints='[]' WHERE id=$1`, ship, fmt.Sprintf("planet-%d", planet))
	f.app.Transfer.Enabled = true
	return f, c, uid, surface, site, ship
}
func ticket180(t *testing.T, f *fixture033, c *http.Cookie, operation string, site, ship int64) map[string]any {
	t.Helper()
	r := f.req(c, "POST", "/api/planet/handoff", map[string]any{"operation": operation, "landing_site": fmt.Sprint(site), "ship": fmt.Sprint(ship)}, 200)
	var v map[string]any
	if err := json.Unmarshal(r.Body.Bytes(), &v); err != nil {
		t.Fatal(err)
	}
	return v
}
func ws180(t *testing.T, base string, cookie *http.Cookie, payload any) (*websocket.Conn, map[string]any) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	h := http.Header{}
	h.Set("Cookie", cookie.String())
	h.Set("Origin", "http://127.0.0.1:8080")
	conn, _, err := websocket.Dial(ctx, strings.Replace(base, "http://", "ws://", 1)+"/planet/ws/v1", &websocket.DialOptions{HTTPHeader: h})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { conn.CloseNow() })
	if err = wsjson.Write(ctx, conn, map[string]any{"v": 1, "type": "hello", "seq": 1, "payload": payload}); err != nil {
		t.Fatal(err)
	}
	var msg struct {
		Type    string         `json:"type"`
		Payload map[string]any `json:"payload"`
	}
	if err = wsjson.Read(ctx, conn, &msg); err != nil {
		t.Fatal("no hello result", err)
	}
	msg.Payload["message_type"] = msg.Type
	return conn, msg.Payload
}
func wait180(t *testing.T, f *fixture033, query string, want int64, args ...any) {
	t.Helper()
	end := time.Now().Add(5 * time.Second)
	for time.Now().Before(end) {
		if f.integer(query, args...) == want {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatal("condition not met", query)
}
func TestPlanetHandoff180(t *testing.T) {
	f, c, uid, surface, site, ship := prepare180(t)
	base, stop := startPlanet150(t, f.db)
	defer stop()
	// Exercise the actual same-origin reverse proxy including WebSocket upgrade.
	t.Setenv("PLANET_INTERNAL_URL", base)
	terminal := httptest.NewServer(f.app.Handler())
	defer terminal.Close()
	operation := op180(t)
	ticket := ticket180(t, f, c, operation, site, ship)
	secret := ticket["ticket"].(string)
	if f.integer(`SELECT count(*) FROM planet_handoffs WHERE token_hash=$1`, secret) != 0 {
		t.Fatal("plaintext ticket persisted")
	}
	conn, w := ws180(t, terminal.URL, c, map[string]any{"ticket": secret})
	if w["message_type"] != "welcome" {
		t.Fatal(w)
	}
	visit := w["visit"].(string)
	epoch := int64(w["connection_epoch"].(float64))
	if f.integer(`SELECT count(*) FROM planet_handoff_uses`) != 1 || f.integer(`SELECT count(*) FROM planet_characters WHERE user_id=$1`, uid) != 1 {
		t.Fatal("partial activation")
	}
	duplicate := ticket180(t, f, c, operation, site, ship)
	if duplicate["visit"] != visit || duplicate["ticket"] != nil {
		t.Fatal("replay issued second ticket", duplicate)
	}
	_, reused := ws180(t, base, c, map[string]any{"ticket": secret})
	if reused["code"] != "ticket_used" {
		t.Fatal("used ticket accepted", reused)
	}
	req, _ := http.NewRequest("GET", terminal.URL+fmt.Sprintf("/planet/api/v1/scene?visit=%s&epoch=%d", visit, epoch), nil)
	req.AddCookie(c)
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	var scene map[string]any
	json.NewDecoder(res.Body).Decode(&scene)
	res.Body.Close()
	if res.StatusCode != 200 || scene["cells"] == nil || scene["objects"] == nil {
		t.Fatal("no authorized scene", res.StatusCode, scene)
	}
	raw, _ := json.Marshal(scene)
	for _, private := range []string{"Seed", "seed", "checksum", "password", "credits"} {
		if strings.Contains(string(raw), private) {
			t.Fatal("private scene data", private)
		}
	}
	if _, err = f.db.Exec(f.ctx, `UPDATE ships SET object_id='entry' WHERE id=$1`, ship); err == nil {
		t.Fatal("DB guard bypass")
	}
	f.req(c, "POST", "/api/workbench", map[string]any{"action": "rescue", "ship": ship}, 409)
	f.exec(`UPDATE users SET role='admin' WHERE id=$1`, uid)
	f.req(c, "POST", "/api/admin/teleport", map[string]any{"ship": ship, "to": 2}, 409)
	// Renaming and other ships are not physical movement of the reserved hull.
	f.exec(`UPDATE ships SET name='Surface Pilgrim' WHERE id=$1`, ship)
	f.exec(`INSERT INTO ships(user_id,active,star,object_id) VALUES($1,false,1,'entry')`, uid)
	other := f.integer(`SELECT id FROM ships WHERE user_id=$1 AND id<>$2`, uid, ship)
	f.exec(`UPDATE ships SET object_id='planet-99' WHERE id=$1`, other)
	second, w2 := ws180(t, base, c, map[string]any{"resume_visit": visit})
	if w2["message_type"] != "welcome" || int64(w2["connection_epoch"].(float64)) <= epoch || w2["x_q"] != w["x_q"] {
		t.Fatal("unsafe resume", w2)
	}
	conn.CloseNow()
	time.Sleep(20 * time.Millisecond)
	if f.integer(`SELECT count(*) FROM planet_visits WHERE user_id=$1 AND state='active'`, uid) != 1 {
		t.Fatal("old disconnect replaced new controller")
	}
	returnOperation := proof253(t, second, w2, 2)
	second.CloseNow()
	wait180(t, f, `SELECT count(*) FROM planet_visits WHERE user_id=$1 AND state='disconnected'`, 1, uid)
	f.req(c, "POST", "/api/planet/return", map[string]any{"operation": returnOperation, "visit": visit}, 200)
	wait180(t, f, `SELECT count(*) FROM planet_visits WHERE user_id=$1 AND state='ended'`, 1, uid)
	if err = f.app.Transfer.Reconcile(f.ctx); err != nil {
		t.Fatal(err)
	}
	if f.integer(`SELECT count(*) FROM planet_transfer_controls WHERE reserved_ship=$1`, ship) != 0 {
		t.Fatal("ship remained reserved")
	}
	f.exec(`UPDATE ships SET object_id='entry' WHERE id=$1`, ship)
	if f.integer(`SELECT count(*) FROM planet_surfaces WHERE id=$1`, surface) != 1 {
		t.Fatal("return changed geography")
	}
	// An unrelated later operation must not be revoked because the same user
	// has historical tickets on this surface.
	later := op180(t)
	f.exec(`UPDATE planet_transfer_controls SET reserved_ship=$2,active_operation=$3 WHERE user_id=$1`, uid, other, later)
	// Completed visits and character/use references are cleaned by the planetary
	// writer before cosmic removes tickets and the canonical surface.
	planet := int(f.integer(`SELECT planet FROM planet_surfaces WHERE id=$1`, surface))
	preview := preview124(f, c, planet)
	f.req(c, "POST", "/api/admin/planet/surface", input124(preview, "delete", "p4-retire-surface"), 409)
	wait180(t, f, `SELECT count(*) FROM planet_local_locations WHERE surface=$1`, 0, surface)
	preview = preview124(f, c, planet)
	f.req(c, "POST", "/api/admin/planet/surface", input124(preview, "delete", "p4-delete-surface"), 200)
	if f.integer(`SELECT count(*) FROM planet_handoffs WHERE surface=$1`, surface) != 0 || f.integer(`SELECT count(*) FROM planet_characters WHERE last_surface=$1`, surface) != 0 {
		t.Fatal("retirement left runtime references")
	}
	if f.integer(`SELECT count(*) FROM planet_transfer_controls WHERE user_id=$1 AND reserved_ship=$2 AND active_operation=$3`, uid, other, later) != 1 {
		t.Fatal("reset revoked an unrelated reservation")
	}

}
func TestPlanetTicketRecovery180(t *testing.T) {
	f, c, uid, _, site, ship := prepare180(t)
	base, stop := startPlanet150(t, f.db)
	op := op180(t)
	first := ticket180(t, f, c, op, site, ship)
	second := ticket180(t, f, c, op, site, ship)
	if first["ticket"] == second["ticket"] {
		t.Fatal("lost-secret retry did not rotate")
	}
	_, denied := ws180(t, base, c, map[string]any{"ticket": first["ticket"]})
	if denied["message_type"] != "error" {
		t.Fatal("old secret accepted")
	}
	other, _ := f.signup("other_handoff180")
	_, denied = ws180(t, base, other, map[string]any{"ticket": second["ticket"]})
	if denied["message_type"] != "error" {
		t.Fatal("another account redeemed ticket")
	}
	f.exec(`UPDATE planet_handoffs SET expires_at=clock_timestamp()-interval '1 second' WHERE user_id=$1`, uid)
	_, denied = ws180(t, base, c, map[string]any{"ticket": second["ticket"]})
	if denied["code"] != "ticket_expired" {
		t.Fatal(denied)
	}
	if err := f.app.Transfer.Reconcile(f.ctx); err != nil {
		t.Fatal(err)
	}
	if f.integer(`SELECT count(*) FROM planet_transfer_controls WHERE reserved_ship=$1`, ship) != 0 {
		t.Fatal("expired ticket locked ship")
	}
	live := ticket180(t, f, c, op180(t), site, ship)
	conn, w := ws180(t, base, c, map[string]any{"ticket": live["ticket"]})
	if w["message_type"] != "welcome" {
		t.Fatal(w)
	}
	conn.CloseNow()
	stop() // Offline service: release only after confirmed lease fencing.
	// A fresh runtime resumes the saved visit rather than creating a new character.
	replacement, stopReplacement := startPlanet150(t, f.db)
	recovered, recovery := ws180(t, replacement, c, map[string]any{"resume_visit": w["visit"]})
	if recovery["message_type"] != "welcome" || recovery["visit"] != w["visit"] || recovery["x_q"] != w["x_q"] {
		t.Fatal("restart lost safe position", recovery)
	}
	recovered.CloseNow()
	stopReplacement()
	wait180(t, f, `SELECT count(*) FROM planet_runtime_leases WHERE owner_id IS NULL OR expires_at<=clock_timestamp()`, 1)
	f.req(c, "POST", "/api/planet/recover", map[string]any{"operation": op180(t), "visit": w["visit"]}, 200)
	if f.integer(`SELECT count(*) FROM planet_transfer_controls WHERE reserved_ship=$1`, ship) != 0 {
		t.Fatal("offline fenced return locked ship")
	}
	_, again := startPlanet150(t, f.db)
	defer again()
	wait180(t, f, `SELECT count(*) FROM planet_visits WHERE user_id=$1 AND state='ended'`, 1, uid)
}
func TestPlanetAccessRevocation180(t *testing.T) {
	f, c, uid, _, site, ship := prepare180(t)
	base, stop := startPlanet150(t, f.db)
	defer stop()
	// Permission is rechecked at activation and during an active visit.
	ticket := ticket180(t, f, c, op180(t), site, ship)
	f.exec(`UPDATE planet_landing_sites SET enabled=false WHERE id=$1`, site)
	_, denied := ws180(t, base, c, map[string]any{"ticket": ticket["ticket"]})
	if denied["message_type"] != "error" {
		t.Fatal("revoked site accepted")
	}
	f.exec(`UPDATE planet_landing_sites SET enabled=true WHERE id=$1`, site)
	conn, w := ws180(t, base, c, map[string]any{"ticket": ticket["ticket"]})
	defer conn.CloseNow()
	if w["message_type"] != "welcome" {
		t.Fatal(w)
	}
	f.req(c, "POST", "/api/logout", map[string]any{}, 200)
	wait180(t, f, `SELECT count(*) FROM planet_visits WHERE user_id=$1 AND state='disconnected'`, 1, uid)
	if err := f.app.Transfer.Reconcile(f.ctx); err != nil {
		t.Fatal(err)
	}
	if f.integer(`SELECT count(*) FROM planet_transfer_controls WHERE reserved_ship=$1`, ship) != 1 {
		t.Fatal("logout bypassed landing-only return")
	}
	// A genuinely revoked landing permission still ends the visit safely.
	f.exec(`UPDATE planet_landing_sites SET enabled=false WHERE id=$1`, site)
	wait180(t, f, `SELECT count(*) FROM planet_visits WHERE user_id=$1 AND state='ended'`, 1, uid)
	if err := f.app.Transfer.Reconcile(f.ctx); err != nil {
		t.Fatal(err)
	}
	if f.integer(`SELECT count(*) FROM planet_transfer_controls WHERE reserved_ship=$1`, ship) != 0 {
		t.Fatal("revoked landing did not release ship")
	}
}

func TestPlanetCorporateAndAtomicActivation180(t *testing.T) {
	f, c, uid, surface, site, ship := prepare180(t)
	base, stop := startPlanet150(t, f.db)
	defer stop()
	_, owner := f.signup("corporate_owner180")
	var company, role int64
	if err := f.db.QueryRow(f.ctx, `INSERT INTO companies(name,tag,color,flag,owner_id) VALUES('Landing Rights','LND','#88aaaa','orbit',$1) RETURNING id`, owner).Scan(&company); err != nil {
		t.Fatal(err)
	}
	if err := f.db.QueryRow(f.ctx, `INSERT INTO company_roles(company_id,name,permissions) VALUES($1,'Visitor','["colonize"]') RETURNING id`, company).Scan(&role); err != nil {
		t.Fatal(err)
	}
	f.exec(`UPDATE users SET company_id=$2,company_role=$3,role='admin' WHERE id=$1`, uid, company, role)
	f.exec(`UPDATE planet_colonies SET owner_user=NULL,owner_company=$2 WHERE surface=$1`, surface, company)
	f.req(c, "POST", "/api/planet/handoff", map[string]any{"operation": op180(t), "landing_site": fmt.Sprint(site), "ship": fmt.Sprint(ship)}, 403)
	f.exec(`UPDATE company_roles SET permissions='["planetary.enter"]' WHERE id=$1`, role)
	ticket := ticket180(t, f, c, op180(t), site, ship)
	// Fail after the character UPSERT. The character, visit and ticket use must
	// roll back together; immutable scene generation may safely remain persisted.
	f.exec(`CREATE FUNCTION fail_activation180() RETURNS trigger LANGUAGE plpgsql AS $$BEGIN RAISE EXCEPTION 'test-only activation failure'; END$$`)
	f.exec(`CREATE TRIGGER fail_activation180 BEFORE INSERT ON planet_visits FOR EACH ROW EXECUTE FUNCTION fail_activation180()`)
	_, failed := ws180(t, base, c, map[string]any{"ticket": ticket["ticket"]})
	if failed["message_type"] != "error" || f.integer(`SELECT count(*) FROM planet_characters`) != 0 || f.integer(`SELECT count(*) FROM planet_handoff_uses`) != 0 {
		t.Fatal("partial ticket activation", failed)
	}
	f.exec(`DROP TRIGGER fail_activation180 ON planet_visits`)
	// A retry after rollback can consume the still-unused ticket.
	var fence scenestore.Fence
	if err := f.db.QueryRow(f.ctx, `SELECT owner_id::text,fence FROM planet_runtime_leases WHERE id=1`).Scan(&fence.Owner, &fence.Number); err != nil {
		t.Fatal(err)
	}
	id := access.Identity{User: uid, Session: account.TokenHash(c.Value)}
	service := visits.Service{DB: f.db}
	intent, err := service.Resolve(f.ctx, id, account.TokenHash(ticket["ticket"].(string)), "")
	if err != nil {
		t.Fatal(err)
	}
	saved, err := scenestore.New(f.db).Load(f.ctx, intent.Key, fence)
	if err != nil {
		t.Fatal(err)
	}
	var wg sync.WaitGroup
	results := make(chan error, 2)
	for range 2 {
		wg.Add(1)
		go func() { defer wg.Done(); _, e := service.Activate(f.ctx, id, intent, saved, fence); results <- e }()
	}
	wg.Wait()
	close(results)
	won, lost := 0, 0
	for e := range results {
		if e == nil {
			won++
		} else if errors.Is(e, visits.ErrUsed) {
			lost++
		} else {
			t.Fatal(e)
		}
	}
	if won != 1 || lost != 1 {
		t.Fatal("competing activation was not single-use", won, lost)
	}
	if f.integer(`SELECT count(*) FROM planet_handoff_uses`) != 1 {
		t.Fatal("missing receipt")
	}
	f.exec(`UPDATE company_roles SET permissions='[]' WHERE id=$1`, role)
	wait180(t, f, `SELECT count(*) FROM planet_visits WHERE user_id=$1 AND state='ended'`, 1, uid)
	if err := f.app.Transfer.Reconcile(f.ctx); err != nil {
		t.Fatal(err)
	}
	if f.integer(`SELECT count(*) FROM planet_transfer_controls WHERE reserved_ship=$1`, ship) != 0 {
		t.Fatal("permission revocation kept reservation")
	}
}

func TestPlanetRestrictedRole180(t *testing.T) {
	f, c, uid, _, site, ship := prepare180(t)
	role := fmt.Sprintf("visit_role_%d", time.Now().UnixNano())
	quoted := pgx.Identifier{role}.Sanitize()
	schema := f.db.Config().ConnConfig.RuntimeParams["search_path"]
	f.exec(`CREATE ROLE ` + quoted + ` NOLOGIN`)
	t.Cleanup(func() {
		f.db.Exec(context.Background(), `DROP OWNED BY `+quoted)
		f.db.Exec(context.Background(), `DROP ROLE `+quoted)
	})
	f.exec(`GRANT USAGE ON SCHEMA ` + schema + ` TO ` + quoted)
	f.exec(`GRANT SELECT ON planet_schema_state,planet_runtime_leases,planet_local_locations,planet_surfaces,planet_surface_controls,planet_runtime_controls,planet_colonies,planet_projects,planet_landing_sites,planet_characters,planet_handoffs,planet_handoff_uses,planet_transfer_controls,planet_visits,planet_session_lookup TO ` + quoted)
	f.exec(`GRANT SELECT(id,company_id,company_role) ON users TO ` + quoted)
	f.exec(`GRANT SELECT(id,user_id,star,object_id,destination,local_target,waypoints) ON ships TO ` + quoted)
	f.exec(`GRANT SELECT(id,owner_id,name) ON companies TO ` + quoted)
	f.exec(`GRANT SELECT(id,company_id,permissions) ON company_roles TO ` + quoted)
	f.exec(`GRANT SELECT(user_id,star) ON surveys TO ` + quoted)
	f.exec(`GRANT UPDATE(owner_id,fence,expires_at) ON planet_runtime_leases TO ` + quoted)
	f.exec(`GRANT SELECT,INSERT ON planet_transition_receipts TO ` + quoted)
	f.exec(`GRANT SELECT,INSERT,UPDATE ON aj_planet_return_authorizations TO ` + quoted)
	f.exec(`GRANT INSERT,DELETE ON planet_local_locations,planet_handoff_uses TO ` + quoted)
	f.exec(`GRANT INSERT,UPDATE ON planet_characters,planet_visits TO ` + quoted)
	cfg := f.db.Config().Copy()
	cfg.ConnConfig.RuntimeParams["role"] = role
	limited, err := pgxpool.NewWithConfig(f.ctx, cfg)
	if err != nil {
		t.Fatal(err)
	}
	base, stop := startPlanet150(t, limited)
	defer func() { stop(); limited.Close() }()
	ticket := ticket180(t, f, c, op180(t), site, ship)
	conn, w := ws180(t, base, c, map[string]any{"ticket": ticket["ticket"]})
	defer conn.CloseNow()
	if w["message_type"] != "welcome" {
		t.Fatal("least-privilege activation", w)
	}
	// Metrics are optional for old restricted roles. Grant only the role column;
	// this does not widen access to balances, password hashes or cosmic writes.
	f.exec(`UPDATE users SET role='admin' WHERE id=$1`, uid)
	requireLoad230(t, base, c, "GET", "/planet/api/v1/metrics", nil, 503)
	f.exec(`GRANT SELECT(role) ON users TO ` + quoted)
	requireLoad230(t, base, c, "GET", "/planet/api/v1/metrics", nil, 200)
	for _, query := range []string{`SELECT password_hash FROM users LIMIT 1`, `SELECT credits FROM users LIMIT 1`, `UPDATE users SET credits=credits+1`, `UPDATE ships SET star=2`, `UPDATE planet_transfer_controls SET generation=generation+1`, `UPDATE planet_handoffs SET expires_at=now()`, `UPDATE planet_landing_sites SET enabled=false`} {
		if _, err = limited.Exec(f.ctx, query); err == nil {
			t.Fatal("role boundary failed", query)
		}
	}
	returnOperation := proof253(t, conn, w, 2)
	f.req(c, "POST", "/api/planet/return", map[string]any{"operation": returnOperation, "visit": w["visit"]}, 200)
	wait180(t, f, `SELECT count(*) FROM planet_visits WHERE user_id=$1 AND state='ended'`, 1, uid)
}
