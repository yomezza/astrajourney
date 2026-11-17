package integration

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"
)

// Optional real-browser integration. All writable data is owned by newFixture033
// in a disposable _test database; the nonce prevents targeting a normal server.
func TestTerminalBrowser(t *testing.T) {
	if os.Getenv("RUN_TERMINAL_BROWSER") != "1" {
		t.Skip("set RUN_TERMINAL_BROWSER=1 with Node/Playwright installed")
	}
	f := newFixture033(t)
	_, uid := f.signup("terminal_audit")
	f.exec(`UPDATE users SET role='admin',credits=50000000,level=20,xp=10000 WHERE id=$1`, uid)
	f.exec(`INSERT INTO notifications(user_id,kind,message) VALUES($1,'message','Terminal notification smoke')`, uid)
	f.exec(`INSERT INTO surveys(user_id,star,source) VALUES($1,1,'survey') ON CONFLICT DO NOTHING`, uid)
	f.exec(`UPDATE ships SET object_id='station',position_mode='docked',fuel=20,cargo='{"iron":6,"agni":4}' WHERE user_id=$1`, uid)
	f.exec(`INSERT INTO warehouses(user_id,star,station,cargo) VALUES($1,1,'station','{"iron":10,"agni":15}') ON CONFLICT(user_id,star,station) DO UPDATE SET cargo=EXCLUDED.cargo`, uid)
	nonce := fmt.Sprintf("terminal-test-%d", time.Now().UnixNano())
	handler := browserCDN(t, f, f.handler)
	web := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("X-Cosmos-Test-Run", nonce)
		handler.ServeHTTP(w, r)
	}))
	defer web.Close()
	ctx, cancel := context.WithTimeout(f.ctx, 4*time.Minute)
	defer cancel()
	script, err := filepath.Abs("../../../tests/ui/live-scenarios.mjs")
	if err != nil {
		t.Fatal(err)
	}
	cmd := exec.CommandContext(ctx, "node", script)
	cmd.Env = append(os.Environ(), "TERMINAL_TEST_URL="+web.URL, "TERMINAL_TEST_NONCE="+nonce)
	out, err := cmd.CombinedOutput()
	t.Log(string(out))
	if err != nil {
		t.Fatal(err)
	}
	if f.integer(`SELECT count(*) FROM notifications WHERE user_id=$1`, uid) != 0 {
		t.Fatal("terminal did not clear the seeded notification")
	}
}
