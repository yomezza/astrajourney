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

func TestGalaxyEditorBrowser(t *testing.T) {
	if os.Getenv("RUN_GALAXY_EDITOR_BROWSER") != "1" {
		t.Skip("set RUN_GALAXY_EDITOR_BROWSER=1 with Node/Playwright")
	}
	f := newFixture033(t)
	cookie, uid := f.signup("galaxy_editor")
	f.exec(`UPDATE users SET role='admin' WHERE id=$1`, uid)
	initial := f.integer(`SELECT jsonb_array_length(data->'stars') FROM galaxies WHERE name='Edem'`)
	nonce := fmt.Sprintf("terminal-test-%d", time.Now().UnixNano())
	web := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("X-Cosmos-Test-Run", nonce)
		f.handler.ServeHTTP(w, r)
	}))
	defer web.Close()
	script, err := filepath.Abs("../../../tests/ui/galaxy-editor-live.mjs")
	if err != nil {
		t.Fatal(err)
	}
	input, _ := json.Marshal(map[string]string{"cookie_name": cookie.Name, "cookie": cookie.Value})
	ctx, cancel := context.WithTimeout(f.ctx, 90*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, "node", script)
	cmd.Stdin = strings.NewReader(string(input))
	cmd.Env = append(os.Environ(), "TERMINAL_TEST_URL="+web.URL, "TERMINAL_TEST_NONCE="+nonce)
	out, err := cmd.CombinedOutput()
	t.Log(string(out))
	if err != nil {
		t.Fatal(err)
	}
	if got := f.integer(`SELECT jsonb_array_length(data->'stars') FROM galaxies WHERE name='Edem'`); got != initial+50 {
		t.Fatalf("galaxy was not created exactly once: %d != %d", got, initial+50)
	}
}
