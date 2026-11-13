package integration

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/coder/websocket"
	"github.com/coder/websocket/wsjson"
)

// Both real accounts are explicitly granted the same corporate landing site.
func peer200(t *testing.T, f *fixture033, first, surface int64) (*http.Cookie, int64, int64) {
	t.Helper()
	c, uid := f.signup("peer200")
	var company, role int64
	if err := f.db.QueryRow(f.ctx, `INSERT INTO companies(name,tag,color,flag,owner_id) VALUES('P2-6 visitors','VIS','#88aaaa','orbit',$1) RETURNING id`, first).Scan(&company); err != nil {
		t.Fatal(err)
	}
	if err := f.db.QueryRow(f.ctx, `INSERT INTO company_roles(company_id,name,permissions) VALUES($1,'Visitor','["planetary.enter"]') RETURNING id`, company).Scan(&role); err != nil {
		t.Fatal(err)
	}
	f.exec(`UPDATE users SET company_id=$2,company_role=$3 WHERE id=$1 OR id=$4`, first, company, role, uid)
	f.exec(`UPDATE planet_projects SET owner_user=NULL,owner_company=$2 WHERE surface=$1`, surface, company)
	f.exec(`UPDATE planet_colonies SET owner_user=NULL,owner_company=$2 WHERE surface=$1`, surface, company)
	star := f.integer(`SELECT star FROM planet_surfaces WHERE id=$1`, surface)
	planet := f.integer(`SELECT planet FROM planet_surfaces WHERE id=$1`, surface)
	ship := f.integer(`SELECT id FROM ships WHERE user_id=$1 AND active`, uid)
	f.exec(`UPDATE ships SET star=$2,object_id=$3,position_mode='orbit',destination=NULL,local_target=NULL,waypoints='[]' WHERE id=$1`, ship, star, fmt.Sprintf("planet-%d", planet))
	f.exec(`INSERT INTO surveys(user_id,star) VALUES($1,$2) ON CONFLICT DO NOTHING`, uid, star)
	reconcile160(f)
	return c, uid, ship
}
func frame200(t *testing.T, c *websocket.Conn, kind string) map[string]any {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 6*time.Second)
	defer cancel()
	for {
		var v map[string]any
		if err := wsjson.Read(ctx, c, &v); err != nil {
			t.Fatal(err)
		}
		if v["type"] == kind {
			return v
		}
	}
}
func actor200(p map[string]any, id string) (map[string]any, bool) {
	list, _ := p["actors"].([]any)
	for _, raw := range list {
		a := raw.(map[string]any)
		if a["actor"] == id {
			return a, true
		}
	}
	return nil, false
}
func TestPlanetPresence200(t *testing.T) {
	f, c, uid, surface, site, ship := prepare180(t)
	peer, peerID, peerShip := peer200(t, f, uid, surface)
	base, stop := startPlanet150(t, f.db)
	defer stop()
	one, w := ws180(t, base, c, map[string]any{"ticket": ticket180(t, f, c, op180(t), site, ship)["ticket"]})
	initial := frame200(t, one, "sector.snapshot")
	initialPayload := initial["payload"].(map[string]any)
	if len(initialPayload["actors"].([]any)) != 1 {
		t.Fatal(initialPayload)
	}
	two, w2 := ws180(t, base, peer, map[string]any{"ticket": ticket180(t, f, peer, op180(t), site, peerShip)["ticket"]})
	actor := w2["actor"].(string)
	if actor == w["actor"] || w2["nickname"] != "peer200" {
		t.Fatal("invalid public identities")
	}
	snapshot := frame200(t, two, "sector.snapshot")
	p := snapshot["payload"].(map[string]any)
	if len(p["actors"].([]any)) != 2 {
		t.Fatal("second player missed initial membership", p)
	}
	enter := frame200(t, one, "actor.enter")
	entered := enter["payload"].(map[string]any)
	if entered["actor"] != actor || entered["nickname"] != "peer200" {
		t.Fatal(entered)
	}
	raw, _ := json.Marshal(entered)
	for _, secret := range []string{"user_id", "session", "password", "credits", "visit", "generation", "seed"} {
		if strings.Contains(string(raw), secret) {
			t.Fatal("private presence field", secret)
		}
	}
	x, y := int(w2["x_q"].(float64)), int(w2["y_q"].(float64))
	command190(t, two, w2, 2, "move", map[string]int{"x_q": x + 2*1024, "y_q": y})
	read190(t, two, "path.accepted")
	deadline := time.Now().Add(4 * time.Second)
	seen := false
	previous := enter["server_seq"].(float64)
	for time.Now().Before(deadline) {
		delta := frame200(t, one, "state.delta")
		if delta["server_seq"].(float64) <= previous {
			t.Fatal("non-monotonic server sequence")
		}
		previous = delta["server_seq"].(float64)
		if a, ok := actor200(delta["payload"].(map[string]any), actor); ok && a["x_q"].(float64) > float64(x)+1024 {
			seen = true
			break
		}
	}
	if !seen {
		t.Fatal("peer movement not delivered")
	}
	// A fresh epoch keeps the public character and the visit, no independent actor.
	next, w3 := ws180(t, base, peer, map[string]any{"resume_visit": w2["visit"]})
	if w3["actor"] != actor || w3["visit"] != w2["visit"] || w3["connection_epoch"].(float64) <= w2["connection_epoch"].(float64) {
		t.Fatal("takeover changed identity", w3)
	}
	two.CloseNow()
	command190(t, next, w3, 2, "resync", map[string]int{"last_server_seq": 0})
	fresh := frame200(t, next, "sector.snapshot")["payload"].(map[string]any)
	if len(fresh["actors"].([]any)) != 2 {
		t.Fatal("duplicate controller in snapshot", fresh)
	}
	command190(t, next, w3, 3, "resync", map[string]int{"last_server_seq": -1})
	if failure := read190(t, next, "error"); failure["code"] != "invalid_resync" {
		t.Fatal("invalid resync accepted", failure)
	}
	// End the actual account session. Its actor disappears from the other client.
	f.exec(`UPDATE sessions SET expires_at=clock_timestamp()-interval '1 second' WHERE user_id=$1`, peerID)
	left := frame200(t, one, "actor.leave")["payload"].(map[string]any)
	if left["actor"] != actor {
		t.Fatal("wrong actor removed", left)
	}
	wait180(t, f, `SELECT count(*) FROM planet_visits WHERE user_id=$1 AND state='disconnected'`, 1, peerID)
	if f.integer(`SELECT count(*) FROM planet_transfer_controls WHERE user_id=$1 AND reserved_ship=$2`, peerID, peerShip) != 1 {
		t.Fatal("session expiry released a ship from the surface")
	}
}
