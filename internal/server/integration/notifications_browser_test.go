package integration

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestNotificationsCompactBrowser(t *testing.T) {
	if os.Getenv("RUN_NOTIFICATIONS_BROWSER") != "1" {
		t.Skip("set RUN_NOTIFICATIONS_BROWSER=1 with Node/Playwright")
	}
	f, cookie, uid, surface, sectors := setupPlanet120(t)
	peer, peerID := f.signup("notification_peer")
	ship := f.integer(`SELECT id FROM ships WHERE user_id=$1 AND active`, uid)
	second := f.integer(`INSERT INTO ships(user_id,name,class,star,active,fuel,cargo) VALUES($1,'Notification scout','pilgrim',1,false,20,'{"iron":3}') RETURNING id`, uid)
	project := addColony160(f, uid, surface, sectors[0], nil, false)
	planet := f.integer(`SELECT planet FROM planet_surfaces WHERE id=$1`, surface)
	// Older unread records deliberately fall outside the latest-500 API window.
	f.exec(`INSERT INTO notifications(user_id,kind,message,seen) SELECT $1,'message','Older event '||n,false FROM generate_series(1,505) n`, uid)
	ids := map[string]int64{}
	for _, kind := range []string{"arrival", "landing_ready", "mining_complete", "research_complete", "delivery_complete", "company_invitation", "project_complete", "portal_open", "colony", "message", "constructor"} {
		data, _ := json.Marshal(map[string]any{"star": 1, "ship": second, "resource": "iron", "quantity": 3, "planet": "Test garden", "planet_index": planet, "sector": sectors[0], "project": project})
		message := ""
		if kind == "message" {
			message = "<img src=x onerror=alert(1)> Long text " + strings.Repeat("подробности ", 120)
		}
		var id int64
		if err := f.db.QueryRow(f.ctx, `INSERT INTO notifications(user_id,kind,level,message,data) VALUES($1,$2,$3,$4,$5) RETURNING id`, uid, kind, map[bool]string{true: "warning", false: "success"}[kind == "constructor"], message, data).Scan(&id); err != nil {
			t.Fatal(err)
		}
		ids[kind] = id
	}
	f.exec(`INSERT INTO notifications(user_id,kind,message) VALUES($1,'message','Peer private notification')`, peerID)
	peerNote := f.integer(`SELECT id FROM notifications WHERE user_id=$1`, peerID)
	// Both bulk operations must remain restricted to the authenticated owner.
	f.req(cookie, "POST", "/api/notifications", map[string]any{"action": "seen", "ids": []int64{peerNote}}, 200)
	f.req(cookie, "POST", "/api/notifications", map[string]any{"action": "delete", "ids": []int64{peerNote}}, 200)
	if f.integer(`SELECT count(*) FROM notifications WHERE id=$1 AND NOT seen`, peerNote) != 1 {
		t.Fatal("notification ownership was bypassed")
	}
	nonce := fmt.Sprintf("terminal-test-%d", time.Now().UnixNano())
	web := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("X-Cosmos-Test-Run", nonce)
		f.handler.ServeHTTP(w, r)
	}))
	defer web.Close()
	input, _ := json.Marshal(map[string]any{"cookie_name": cookie.Name, "cookie": cookie.Value, "peer_cookie": peer.Value, "ids": ids, "ship": ship, "second": second, "project": project})
	script, err := filepath.Abs("../../../tests/ui/notifications-live.mjs")
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(f.ctx, 3*time.Minute)
	defer cancel()
	cmd := exec.CommandContext(ctx, "node", script)
	cmd.Stdin = strings.NewReader(string(input))
	cmd.Env = append(os.Environ(), "TERMINAL_TEST_URL="+web.URL, "TERMINAL_TEST_NONCE="+nonce)
	out, err := cmd.CombinedOutput()
	t.Log(string(out))
	if err != nil {
		t.Fatal(err)
	}
}
