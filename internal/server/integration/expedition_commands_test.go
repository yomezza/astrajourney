package integration

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"sync"
	"testing"
	"time"
)

func TestStationCommandReceipts(t *testing.T) {
	f := newFixture033(t)
	pilot, uid := f.signup("ticket_pilot")
	other, _ := f.signup("ticket_other")
	active := f.integer("SELECT id FROM ships WHERE user_id=$1 AND active", uid)
	ship := f.integer(`INSERT INTO ships(user_id,name,class,star,active,fuel,cargo,object_id,position_mode)
		VALUES($1,'Cargo runner','pilgrim',1,false,20,'{}','station','docked') RETURNING id`, uid)
	endpoint := fmt.Sprintf("/api/expedition?ship=%d", ship)
	var quotes struct {
		Offers []struct {
			Resource string
			Buy      int
		}
	}
	if err := json.Unmarshal(f.req(pilot, "GET", endpoint, nil, 200).Body.Bytes(), &quotes); err != nil {
		t.Fatal(err)
	}
	var price int
	for _, quote := range quotes.Offers {
		if quote.Resource == "agni" {
			price = quote.Buy
		}
	}
	if price == 0 {
		t.Fatal("test station did not offer hydrogen")
	}
	id := "19b00bfe-59df-4f75-83ea-2ac31cd39c7a"
	statusURL := "/api/expedition/commands/" + id
	assertCode := func(body []byte, expected string) {
		t.Helper()
		var value struct{ Code, Error string }
		if err := json.Unmarshal(body, &value); err != nil || value.Code != expected || value.Error == "" {
			t.Fatalf("want %s and legacy error, got %s", expected, body)
		}
	}
	assertCode(f.req(pilot, "GET", statusURL, nil, 404).Body.Bytes(), "ACTION_UNCONFIRMED")
	f.req(nil, "GET", statusURL, nil, 401)
	input := map[string]any{"request_id": id, "ship": ship, "action": "buy", "resource": "agni",
		"quantity": 2, "expected_price": price + 1, "expected_star": 1, "expected_object": "station"}
	credits := f.integer("SELECT credits FROM users WHERE id=$1", uid)
	stock := f.integer("SELECT (stock->>'agni')::int FROM local_markets WHERE star=1 AND station='station'")
	assertCode(f.req(pilot, "POST", endpoint, input, 409).Body.Bytes(), "MARKET_PRICE_CHANGED")
	if n := f.integer("SELECT count(*) FROM expedition_command_receipts WHERE user_id=$1", uid); n != 0 {
		t.Fatalf("a rejected command was recorded: %d", n)
	}
	if n := f.integer("SELECT credits FROM users WHERE id=$1", uid); n != credits {
		t.Fatalf("a rejected command debited credits: %d != %d", n, credits)
	}

	input["expected_price"] = price
	body, _ := json.Marshal(input)
	var wg sync.WaitGroup
	var calls [2]*httptest.ResponseRecorder
	for i := range calls {
		wg.Go(func() {
			req := httptest.NewRequest("POST", endpoint, bytes.NewReader(body))
			req.Header.Set("Content-Type", "application/json")
			req.AddCookie(pilot)
			calls[i] = httptest.NewRecorder()
			f.handler.ServeHTTP(calls[i], req)
		})
	}
	wg.Wait()
	if calls[0].Code != 200 || calls[1].Code != 200 || calls[0].Body.String() != calls[1].Body.String() {
		t.Fatalf("same command produced different outcomes: %d %s / %d %s", calls[0].Code, calls[0].Body, calls[1].Code, calls[1].Body)
	}
	if got := f.integer("SELECT credits FROM users WHERE id=$1", uid); got != credits-int64(2*price) {
		t.Fatalf("credits changed twice or by wrong amount: got %d", got)
	}
	if got := f.integer("SELECT (cargo->>'agni')::int FROM ships WHERE id=$1", ship); got != 2 {
		t.Fatalf("selected ship received wrong cargo: %d", got)
	}
	if got := f.integer("SELECT (stock->>'agni')::int FROM local_markets WHERE star=1 AND station='station'"); got != stock-2 {
		t.Fatalf("station stock changed twice or by wrong quantity: %d", got)
	}
	if got := f.integer("SELECT id FROM ships WHERE user_id=$1 AND active", uid); got != active {
		t.Fatalf("trade changed the server's active ship: %d", got)
	}

	if status := f.req(pilot, "GET", statusURL, nil, 200); status.Body.String() != calls[0].Body.String() || status.Header().Get("Cache-Control") != "no-store" {
		t.Fatalf("status differs from committed receipt: %s", status.Body)
	}
	assertCode(f.req(other, "GET", statusURL, nil, 404).Body.Bytes(), "ACTION_UNCONFIRMED")
	f.exec("UPDATE ships SET object_id='entry',position_mode='approach' WHERE id=$1", ship)
	if replay := f.req(pilot, "POST", endpoint, input, 200); replay.Body.String() != calls[0].Body.String() {
		t.Fatalf("move prevented recovery of the previous receipt: %s", replay.Body)
	}
	input["quantity"] = 3
	assertCode(f.req(pilot, "POST", endpoint, input, 409).Body.Bytes(), "ACTION_ID_REUSED")
}

func TestStationCommandsBrowser(t *testing.T) {
	if os.Getenv("RUN_WORKSPACES_BROWSER") != "1" {
		t.Skip("set RUN_WORKSPACES_BROWSER=1 with Chromium and Playwright")
	}
	f := newFixture033(t)
	pilot, uid := f.signup("trade_browser")
	active := f.integer("SELECT id FROM ships WHERE user_id=$1 AND active", uid)
	trader := f.integer(`INSERT INTO ships(user_id,name,class,star,active,fuel,cargo,object_id,position_mode)
		VALUES($1,'Trade courier','pilgrim',1,false,20,'{}','station','docked') RETURNING id`, uid)
	nonce := fmt.Sprintf("station-command-%d", time.Now().UnixNano())
	handler := browserCDN(t, f, f.handler)
	web := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("X-Cosmos-Test-Run", nonce)
		handler.ServeHTTP(w, r)
	}))
	defer web.Close()
	data, _ := json.Marshal(map[string]any{
		"cookie_name": pilot.Name, "cookie": pilot.Value, "active": active, "trader": trader,
	})
	ctx, cancel := context.WithTimeout(f.ctx, 2*time.Minute)
	defer cancel()
	chrome := exec.CommandContext(ctx, "node", "../../../tests/ui/station-trade-live.mjs")
	chrome.Env = append(os.Environ(), "TERMINAL_TEST_URL="+web.URL, "TERMINAL_TEST_NONCE="+nonce)
	chrome.Stdin = bytes.NewReader(data)
	output, err := chrome.CombinedOutput()
	t.Log(string(output))
	if err != nil {
		t.Fatal(err)
	}
	if records := f.integer("SELECT count(*) FROM expedition_command_receipts WHERE user_id=$1 AND ship_id=$2", uid, trader); records != 1 {
		t.Fatalf("expected one committed browser purchase: got %d", records)
	}
	if cargo := f.integer(`SELECT (cargo->>'agni')::int FROM ships WHERE id=$1`, trader); cargo != 2 {
		t.Fatalf("browser recovered wrong cargo: got %d", cargo)
	}
}
