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

	"github.com/yomezza/cosmosimulator/internal/universe"
)

// Real production HTTP handlers and PostgreSQL. Every mutation belongs to an
// isolated newFixture033 schema; the browser cannot target an installed game.
func TestCatalogProductionBrowser(t *testing.T) {
	if os.Getenv("RUN_CATALOG_BROWSER") != "1" {
		t.Skip("set RUN_CATALOG_BROWSER=1 with Node/Playwright")
	}
	f, cookie, uid, surface, sectors := setupPlanet120(t)
	g, err := f.app.World.Load(f.ctx)
	if err != nil {
		t.Fatal(err)
	}
	kinds := []string{"Садовый мир", "Каменистая", "Пустынная", "Ледяная", "Лавовая", "Газовый гигант"}
	g.Stars[0].Planets = nil
	for i, kind := range kinds {
		g.Stars[0].Planets = append(g.Stars[0].Planets, universe.Planet{ID: i + 1, Name: fmt.Sprintf("Catalogue planet %d", i+1), Kind: kind, AU: float64(i + 1), Mass: 1, Gravity: 1, Temperature: 290, Resources: []string{"iron"}})
	}
	for i := 0; i < 18; i++ {
		g.Stars[i].Name = fmt.Sprintf("Catalog %02d", i+1)
		g.Stars[i].Class = string("OBAFGKM"[i%7])
	}
	raw, _ := json.Marshal(g)
	f.exec(`UPDATE galaxies SET data=$1 WHERE name='Edem'`, raw)
	f.exec(`INSERT INTO surveys(user_id,star,source) SELECT $1,n,CASE WHEN n%2=0 THEN 'purchased' ELSE 'survey' END FROM generate_series(1,18) n ON CONFLICT(user_id,star) DO UPDATE SET source=EXCLUDED.source`, uid)
	create := func(index int) int64 {
		r := f.req(cookie, "POST", "/api/planet/action", map[string]any{"action": "create", "surface": surface, "sector": sectors[index], "request": fmt.Sprintf("catalog-create-%d", index)}, 200)
		var out struct {
			ID int64 `json:"project"`
		}
		if err := json.Unmarshal(r.Body.Bytes(), &out); err != nil {
			t.Fatal(err)
		}
		return out.ID
	}
	funding := create(0)
	cancelled := create(1)
	f.req(cookie, "POST", "/api/planet/action", map[string]any{"action": "contribute", "id": cancelled, "resource": "credits", "amount": 100, "request": "catalog-contribution"}, 200)
	f.exec(`INSERT INTO warehouses(user_id,star,station,cargo) VALUES($1,1,'station','{"iron":1000}') ON CONFLICT(user_id,star,station) DO UPDATE SET cargo=EXCLUDED.cargo`, uid)
	f.req(cookie, "POST", "/api/planet/action", map[string]any{"action": "contribute", "id": cancelled, "resource": "iron", "amount": 100, "request": "catalog-iron"}, 200)
	f.exec(`UPDATE warehouses SET cargo='{"iron":1000000000}' WHERE user_id=$1`, uid)
	f.req(cookie, "POST", "/api/planet/action", map[string]any{"action": "cancel", "id": cancelled, "request": "catalog-cancel"}, 200)
	f.exec(`UPDATE warehouses SET cargo='{"iron":0}' WHERE user_id=$1`, uid)
	building := addColony160(f, uid, surface, sectors[2], nil, true)
	complete := addColony160(f, uid, surface, sectors[3], nil, false)
	peerCookie, peerID := f.signup("catalog_peer")
	f.exec(`DELETE FROM surveys WHERE user_id=$1`, peerID)
	private := addColony160(f, peerID, surface, sectors[4], nil, true)
	nonce := fmt.Sprintf("terminal-test-%d", time.Now().UnixNano())
	web := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("X-Cosmos-Test-Run", nonce)
		f.handler.ServeHTTP(w, r)
	}))
	defer web.Close()
	input, _ := json.Marshal(map[string]any{"cookie_name": cookie.Name, "cookie": cookie.Value, "peer_cookie": peerCookie.Value, "funding": funding, "building": building, "complete": complete, "private": private})
	script, err := filepath.Abs("../../../tests/ui/catalog-live.mjs")
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(f.ctx, 2*time.Minute)
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
