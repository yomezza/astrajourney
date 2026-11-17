package integration

import (
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

func TestNamingProductionBrowser280(t *testing.T) {
	if os.Getenv("RUN_NAMING_BROWSER") != "1" {
		t.Skip("set RUN_NAMING_BROWSER=1 with Node/Playwright")
	}
	f := newFixture033(t)
	cookie, uid := f.signup("browser280")
	s := neutral250(t, f)
	f.exec(`UPDATE aj_discovery_objects SET discoverer_id=$1,claim_state='claimed',discovered_at=clock_timestamp(),claim_source='survey' WHERE kind='system' AND star_id=$2`, uid, s.ID)
	f.exec(`INSERT INTO surveys(user_id,star,source) VALUES($1,$2,'survey') ON CONFLICT DO NOTHING`, uid, s.ID)
	peer, _ := f.signup("peerbrowser280")
	nonce := fmt.Sprintf("terminal-test-%d", time.Now().UnixNano())
	handler := browserCDN(t, f, f.handler)
	web := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("X-Cosmos-Test-Run", nonce)
		handler.ServeHTTP(w, r)
	}))
	defer web.Close()
	input, _ := json.Marshal(map[string]any{"cookie_name": cookie.Name, "cookie": cookie.Value, "peer_cookie": peer.Value, "star": s.ID, "planet": s.Planets[0].ID, "planet_uuid": s.Planets[0].UUID})
	script, e := filepath.Abs("../../../tests/ui/naming-live.mjs")
	if e != nil {
		t.Fatal(e)
	}
	cmd := exec.CommandContext(f.ctx, "node", script)
	cmd.Stdin = strings.NewReader(string(input))
	cmd.Env = append(os.Environ(), "TERMINAL_TEST_URL="+web.URL, "TERMINAL_TEST_NONCE="+nonce)
	out, e := cmd.CombinedOutput()
	t.Log(string(out))
	if e != nil {
		t.Fatal(e)
	}
	var root string
	if e = f.db.QueryRow(f.ctx, `SELECT current_schema()`).Scan(&root); e != nil {
		t.Fatal(e)
	}
	again, e := openDatabase(f.ctx, f.db.Config().ConnString(), root)
	if e != nil {
		t.Fatal(e)
	}
	again.Close()
	if f.integer(`SELECT count(*) FROM aj_discovery_objects WHERE star_id=$1 AND rename_used_at IS NOT NULL`, s.ID) != 2 {
		t.Fatal("restart changed spent rights")
	}
}
