package integration

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/yomezza/cosmosimulator/internal/platform/dblayout"

	"github.com/yomezza/cosmosimulator/internal/account"
	"github.com/yomezza/cosmosimulator/internal/server"
	"github.com/yomezza/cosmosimulator/internal/universe"
)

func TestValidName(t *testing.T) {
	for _, v := range []struct {
		s  string
		ok bool
	}{{"pioneer_01", true}, {"ab", false}, {"admin<script>", false}, {"space name", false}, {"бот", false}} {
		if account.ValidName(v.s) != v.ok {
			t.Errorf("validName(%q)", v.s)
		}
	}
}

func TestIntegration(t *testing.T) {
	dsn := os.Getenv("TEST_DATABASE_URL")
	if dsn == "" {
		t.Skip("TEST_DATABASE_URL must point to a disposable PostgreSQL database")
	}
	ctx := context.Background()
	admin, err := pgxpool.New(ctx, dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer admin.Close()
	if !strings.HasSuffix(admin.Config().ConnConfig.Database, "_test") {
		t.Fatal("test database name must end in _test")
	}
	schema := "gameplay_" + time.Now().Format("20060102150405.000000000")
	defer admin.Exec(ctx, "DROP SCHEMA IF EXISTS "+dblayout.DropList(schema)+" CASCADE")
	db, err := openDatabase(ctx, dsn, schema)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	a := server.New(db, server.Config{})
	h := a.Handler()
	request := func(method, path, body string, cookie *http.Cookie) *httptest.ResponseRecorder {
		r := httptest.NewRequest(method, path, strings.NewReader(body))
		r.RemoteAddr = "127.0.0.1:1000"
		if method == "POST" {
			r.Header.Set("Content-Type", "application/json")
		}
		if cookie != nil {
			r.AddCookie(cookie)
		}
		w := httptest.NewRecorder()
		h.ServeHTTP(w, r)
		return w
	}
	assert := func(w *httptest.ResponseRecorder, status int) {
		t.Helper()
		if w.Code != status {
			t.Fatalf("status %d != %d: %s", w.Code, status, w.Body.String())
		}
	}
	assert(request("GET", "/api/state", "", nil), 401)
	assert(request("GET", "/api/galaxy", "", nil), 401)
	assert(request("GET", "/api/route?from=1&to=5", "", nil), 401)
	assert(request("POST", "/api/register", `{"username":"pilot","password":""}`, nil), 400)
	w := request("POST", "/api/register", `{"username":"pilot","password":"a-long-test-password"}`, nil)
	assert(w, 200)
	cookies := w.Result().Cookies()
	if len(cookies) != 1 || !cookies[0].HttpOnly {
		t.Fatal("secure session cookie missing")
	}
	cookie := cookies[0]
	w = request("GET", "/api/galaxy", "", cookie)
	assert(w, 200)
	var galaxy universe.Galaxy
	if err = json.Unmarshal(w.Body.Bytes(), &galaxy); err != nil || len(galaxy.Stars) != 3000 {
		t.Fatal("invalid persisted galaxy", err)
	}
	w = request("GET", "/api/route?from=1&to=5", "", cookie)
	assert(w, 200)
	var journey struct {
		Path []int `json:"path"`
	}
	if err = json.Unmarshal(w.Body.Bytes(), &journey); err != nil || len(journey.Path) != 2 {
		t.Fatal("route not returned", err)
	}
	w = request("GET", "/api/route?from=1&to=2", "", cookie)
	assert(w, 200)
	if err = json.Unmarshal(w.Body.Bytes(), &journey); err != nil || len(journey.Path) < 2 {
		t.Fatal("unreachable route returned", err)
	}
	assert(request("GET", "/api/route?from=1&to=3001", "", cookie), 400)

	assert(request("POST", "/api/register", `{"username":"pilot","password":"a-long-test-password"}`, nil), 409)
	assert(request("POST", "/api/login", `{"username":"pilot","password":"wrong"}`, nil), 401)
	assert(request("POST", "/api/login", `{"username":"pilot","password":"a-long-test-password"}`, nil), 200)
	assert(request("GET", "/api/admin/users", "", cookie), 403)
	assert(request("POST", "/api/register", `{"username":"hacker","password":"a-long-test-password","role":"admin"}`, nil), 400)
	assert(request("POST", "/api/sell", `{}`, cookie), 409)
	for i := 0; i < 3; i++ {
		if _, err = db.Exec(ctx, `UPDATE world SET last_tick=now()-interval '11 seconds'`); err != nil {
			t.Fatal(err)
		}
		if err = a.Simulation.Tick(ctx); err != nil {
			t.Fatal(err)
		}
	}
	if err = a.Simulation.Tick(ctx); err != nil {
		t.Fatal(err)
	}
	w = request("GET", "/api/state", "", cookie)
	assert(w, 200)
	var state struct {
		User      account.User   `json:"user"`
		Tick      int64          `json:"tick"`
		Companies []account.User `json:"companies"`
	}
	if err = json.Unmarshal(w.Body.Bytes(), &state); err != nil {
		t.Fatal(err)
	}
	if state.Tick != 3 || state.User.Ore != 3 || state.User.Credits != 1000 {
		t.Fatalf("unexpected state: %+v", state)
	}
	bots := 0
	for _, u := range state.Companies {
		if u.Bot {
			bots++
			if u.Credits >= 1000 || u.Credits < 0 || u.Ore != 0 {
				t.Fatalf("bot budget was not debited: %+v", u)
			}
		}
	}
	var money int64
	if err = db.QueryRow(ctx, `SELECT (SELECT sum(credits) FROM users WHERE is_bot)+(SELECT credits FROM local_markets WHERE star=1)`).Scan(&money); err != nil || money != 8000 {
		t.Fatal("bot trade created or destroyed currency", money, err)
	}
	if bots != 3 {
		t.Fatalf("bots: %d", bots)
	}
	assert(request("POST", "/api/sell", `{}`, cookie), 200)
	assert(request("POST", "/api/sell", `{}`, cookie), 409)
	w = request("GET", "/api/state", "", cookie)
	assert(w, 200)
	_ = json.Unmarshal(w.Body.Bytes(), &state)
	if state.User.Credits != 1015 || state.User.Ore != 0 {
		t.Fatal("incorrect sale")
	}
	r := httptest.NewRequest("POST", "/api/sell", strings.NewReader(`{}`))
	r.Header.Set("Content-Type", "application/json")
	r.Header.Set("Origin", "https://evil.example")
	r.AddCookie(cookie)
	w = httptest.NewRecorder()
	h.ServeHTTP(w, r)
	assert(w, 403)
	t.Setenv("ADMIN_USERNAME", "operator")
	t.Setenv("ADMIN_PASSWORD", "operator-long-test-password")
	if err = a.Auth.Bootstrap(ctx); err != nil {
		t.Fatal(err)
	}
	w = request("POST", "/api/login", `{"username":"operator","password":"operator-long-test-password"}`, nil)
	assert(w, 200)
	assert(request("GET", "/api/admin/users", "", w.Result().Cookies()[0]), 200)
	assert(request("POST", "/api/logout", `{}`, cookie), 200)
	assert(request("GET", "/api/state", "", cookie), 401)
	if _, err = db.Exec(ctx, `UPDATE sessions SET expires_at=$1`, time.Now().Add(-time.Hour)); err != nil {
		t.Fatal(err)
	}
	assert(request("GET", "/api/health", "", nil), 200)
	assert(request("GET", "/", "", nil), 404)
}
