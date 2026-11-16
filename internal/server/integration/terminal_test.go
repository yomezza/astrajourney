package integration

import (
	"context"
	"encoding/json"
	"fmt"
	"math"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/yomezza/cosmosimulator/internal/platform/dblayout"

	"github.com/yomezza/cosmosimulator/internal/corporations"
	"github.com/yomezza/cosmosimulator/internal/server"
	"github.com/yomezza/cosmosimulator/internal/universe"
)

func TestTerminal(t *testing.T) {
	dsn := os.Getenv("TEST_DATABASE_URL")
	if dsn == "" {
		t.Skip("TEST_DATABASE_URL required")
	}
	ctx := context.Background()
	base, err := pgxpool.New(ctx, dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer base.Close()
	if !strings.HasSuffix(base.Config().ConnConfig.Database, "_test") {
		t.Fatal("test database required")
	}
	schema := fmt.Sprintf("terminal_%d", time.Now().UnixNano())
	defer base.Exec(ctx, "DROP SCHEMA IF EXISTS "+dblayout.DropList(schema)+" CASCADE")
	db, err := openDatabase(ctx, dsn, schema)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	a := server.New(db, server.Config{})
	h := a.Handler()
	request := func(cookie *http.Cookie, method, path, body string) *httptest.ResponseRecorder {
		r := httptest.NewRequest(method, path, strings.NewReader(body))
		r.RemoteAddr = "127.0.0.1:1111"
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
	check := func(w *httptest.ResponseRecorder, status int) {
		t.Helper()
		if w.Code != status {
			t.Fatalf("status %d != %d: %s", w.Code, status, w.Body.String())
		}
	}
	signup := func(name string) *http.Cookie {
		w := request(nil, "POST", "/api/register", fmt.Sprintf(`{"username":%q,"password":"long-test-password"}`, name))
		check(w, 200)
		return w.Result().Cookies()[0]
	}
	pilot, other := signup("terminal_pilot"), signup("terminal_other")
	var pid, oid int64
	if err = db.QueryRow(ctx, `SELECT id FROM users WHERE username='terminal_pilot'`).Scan(&pid); err != nil {
		t.Fatal(err)
	}
	if err = db.QueryRow(ctx, `SELECT id FROM users WHERE username='terminal_other'`).Scan(&oid); err != nil {
		t.Fatal(err)
	}
	post := func(c *http.Cookie, path, body string, status int) {
		t.Helper()
		check(request(c, "POST", path, body), status)
	}
	post(pilot, "/api/admin/tick", `{}`, 403)
	post(pilot, "/api/admin/user", `{"id":1}`, 403)
	t.Setenv("ADMIN_USERNAME", "terminal_admin")
	t.Setenv("ADMIN_PASSWORD", "long-admin-password")
	if err = a.Auth.Bootstrap(ctx); err != nil {
		t.Fatal(err)
	}
	w := request(nil, "POST", "/api/login", `{"username":"terminal_admin","password":"long-admin-password"}`)
	check(w, 200)
	admin := w.Result().Cookies()[0]
	var starter int64
	if err = db.QueryRow(ctx, `SELECT company_id FROM users WHERE id=$1`, pid).Scan(&starter); err != nil || starter != 1 {
		t.Fatal("starter membership", err)
	}
	post(pilot, "/api/companies", `{"action":"create","name":"First Guild","tag":"FGD","flag":"◈","color":"#123456"}`, 409)
	_, _ = db.Exec(ctx, `UPDATE users SET credits=2000000 WHERE id=ANY($1::bigint[])`, []int64{pid, oid})
	post(pilot, "/api/companies", `{"action":"leave"}`, 200)
	post(pilot, "/api/companies", `{"action":"create","name":"First Guild","tag":"TOOLONG","flag":"◈","color":"#123456"}`, 409)
	post(pilot, "/api/companies", `{"action":"create","name":"First Guild","tag":"FGD","flag":"◈","color":"#123456"}`, 200)
	var company int64
	if err = db.QueryRow(ctx, `SELECT company_id FROM users WHERE id=$1`, pid).Scan(&company); err != nil || company <= 1 {
		t.Fatal("company create", err)
	}
	post(pilot, "/api/companies", `{"action":"leave"}`, 409)
	post(other, "/api/companies", fmt.Sprintf(`{"action":"join","id":%d}`, company), 409)
	post(other, "/api/companies", `{"action":"leave"}`, 200)
	post(other, "/api/companies", `{"action":"create","name":"Second Guild","tag":"FGD","flag":"◈","color":"#123456"}`, 409)
	post(other, "/api/workbench", fmt.Sprintf(`{"action":"apply","id":%d}`, company), 200)
	var appID int64
	_ = db.QueryRow(ctx, `SELECT id FROM company_requests WHERE user_id=$1`, oid).Scan(&appID)
	post(pilot, "/api/workbench", fmt.Sprintf(`{"action":"request_accept","id":%d}`, appID), 200)
	for _, scope := range []string{"all", "system", "region"} {
		w = request(pilot, "GET", "/api/companies?scope="+scope, "")
		check(w, 200)
		var list []corporations.Company
		if err = json.Unmarshal(w.Body.Bytes(), &list); err != nil {
			t.Fatal(err)
		}
		found := false
		for _, c := range list {
			if c.ID == company && c.Members == 2 {
				found = true
			}
		}
		if !found {
			t.Fatalf("company presence missing %s", scope)
		}
	}
	// Re-running installation preserves memberships and the company ID sequence.
	again, err := openDatabase(ctx, dsn, schema)
	if err != nil {
		t.Fatal(err)
	}
	again.Close()
	// Unknown system maps are now private until a survey or data purchase.
	check(request(pilot, "GET", "/api/system?id=3", ""), 200)
	post(pilot, "/api/expedition", `{"action":"scan"}`, 200)
	finishResearchForTest(t, a)
	if _, err = db.Exec(ctx, `UPDATE ships SET object_id='asteroids',position_mode='approach' WHERE user_id=$1`, pid); err != nil {
		t.Fatal(err)
	}
	post(pilot, "/api/expedition", `{"action":"mine","planet":101,"resource":"iron"}`, 200)
	post(pilot, "/api/expedition", `{"action":"travel","to":5}`, 409)
	var before int64
	if err = db.QueryRow(ctx, `SELECT tick FROM world WHERE id=1`).Scan(&before); err != nil {
		t.Fatal(err)
	}
	post(admin, "/api/admin/tick", `{}`, 200)
	var tick int64
	if err = db.QueryRow(ctx, `SELECT tick FROM world WHERE id=1`).Scan(&tick); err != nil || tick != before+1 {
		t.Fatal("manual tick did not advance exactly once")
	}
	if err = a.Simulation.Tick(ctx); err != nil {
		t.Fatal(err)
	}
	var unchanged int64
	_ = db.QueryRow(ctx, `SELECT tick FROM world WHERE id=1`).Scan(&unchanged)
	if unchanged != tick {
		t.Fatal("automatic tick duplicated manual tick")
	}
	var cargo []byte
	var xp int
	if err = db.QueryRow(ctx, `SELECT cargo FROM ships WHERE user_id=$1`, pid).Scan(&cargo); err != nil {
		t.Fatal(err)
	}
	var inventory map[string]int
	_ = json.Unmarshal(cargo, &inventory)
	if inventory["iron"] != 3 {
		t.Fatal("mining not delivered on tick")
	}
	_ = db.QueryRow(ctx, `SELECT xp FROM users WHERE id=$1`, pid).Scan(&xp)
	if xp != 15 {
		t.Fatal("survey/mining XP incorrect", xp)
	}
	// Prepaid remote reservation: money is held in escrow, and goods never teleport.
	post(pilot, "/api/market/global", `{"action":"reserve","star":3,"resource":"agni","quantity":1}`, 400)
	var creditsBefore int64
	_ = db.QueryRow(ctx, `SELECT credits FROM users WHERE id=$1`, pid).Scan(&creditsBefore)
	post(pilot, "/api/market/global", `{"action":"reserve","star":5,"resource":"agni","quantity":2}`, 200)
	var rid, cost int64
	_ = db.QueryRow(ctx, `SELECT id,cost FROM reservations WHERE user_id=$1`, pid).Scan(&rid, &cost)
	var creditsAfter, wallet int64
	_ = db.QueryRow(ctx, `SELECT credits FROM users WHERE id=$1`, pid).Scan(&creditsAfter)
	_ = db.QueryRow(ctx, `SELECT credits FROM local_markets WHERE star=5`).Scan(&wallet)
	if creditsAfter != creditsBefore-cost || wallet != 5000 {
		t.Fatal("escrow bookkeeping incorrect")
	}
	post(pilot, "/api/market/global", fmt.Sprintf(`{"action":"collect","id":%d}`, rid), 409)
	post(other, "/api/market/global", fmt.Sprintf(`{"action":"cancel","id":%d}`, rid), 404)
	post(pilot, "/api/expedition", `{"action":"travel","to":5}`, 200)
	post(admin, "/api/admin/tick", `{}`, 200)
	post(pilot, "/api/expedition", `{"action":"move","target":"station","mode":"docked"}`, 200)
	post(admin, "/api/admin/tick", `{}`, 200)
	codes := make(chan int, 2)
	var wg sync.WaitGroup
	for i := 0; i < 2; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			codes <- request(pilot, "POST", "/api/market/global", fmt.Sprintf(`{"action":"collect","id":%d}`, rid)).Code
		}()
	}
	wg.Wait()
	close(codes)
	success := 0
	for code := range codes {
		if code == 200 {
			success++
		} else if code != 409 {
			t.Fatal("collect race", code)
		}
	}
	if success != 1 {
		t.Fatal("duplicate collection")
	}
	_ = db.QueryRow(ctx, `SELECT cargo FROM ships WHERE user_id=$1`, pid).Scan(&cargo)
	_ = json.Unmarshal(cargo, &inventory)
	if inventory["agni"] != 2 {
		t.Fatal("cargo not collected")
	}
	post(pilot, "/api/market/global", `{"action":"reserve","star":5,"resource":"manna","quantity":1}`, 200)
	var cancelID int64
	_ = db.QueryRow(ctx, `SELECT max(id) FROM reservations WHERE user_id=$1`, pid).Scan(&cancelID)
	post(pilot, "/api/market/global", fmt.Sprintf(`{"action":"cancel","id":%d}`, cancelID), 200)
	post(pilot, "/api/market/global", fmt.Sprintf(`{"action":"cancel","id":%d}`, cancelID), 409)
	// Role checks and account editor; password changes revoke existing sessions.
	edit := fmt.Sprintf(`{"id":%d,"username":"terminal_other","role":"player","credits":2000,"level":2,"xp":200,"password":""}`, oid)
	post(pilot, "/api/admin/user", edit, 403)
	post(admin, "/api/admin/user", edit, 200)
	post(other, "/api/profile/upgrade", `{}`, 200)
	post(other, "/api/profile/upgrade", `{}`, 409)
	var level int
	_ = db.QueryRow(ctx, `SELECT level FROM users WHERE id=$1`, oid).Scan(&level)
	if level != 3 {
		t.Fatal("upgrade failed")
	}
	edit = fmt.Sprintf(`{"id":%d,"username":"terminal_other","role":"player","credits":1000,"level":3,"xp":0,"password":"new-long-password"}`, oid)
	post(admin, "/api/admin/user", edit, 200)
	check(request(other, "GET", "/api/profile", ""), 401)
	check(request(nil, "POST", "/api/login", `{"username":"terminal_other","password":"new-long-password"}`), 200)
	var aid int64
	_ = db.QueryRow(ctx, `SELECT id FROM users WHERE username='terminal_admin'`).Scan(&aid)
	post(admin, "/api/admin/user", fmt.Sprintf(`{"id":%d,"username":"terminal_admin","role":"player","credits":1000,"level":1,"xp":0,"password":""}`, aid), 409)
	// A system without a station cannot expose a local market even after arrival.
	_, err = db.Exec(ctx, `UPDATE ships SET star=3,destination=NULL,arrival=NULL WHERE user_id=$1`, pid)
	if err != nil {
		t.Fatal(err)
	}
	post(pilot, "/api/expedition", `{"action":"buy","resource":"agni","quantity":1}`, 409)
}

func TestCompanyTags(t *testing.T) {
	for _, s := range []string{"EDM", "SOL4", "РУС", "МИР4"} {
		if !corporations.ValidCompanyTag(s) {
			t.Fatal("valid tag rejected", s)
		}
	}
	for _, s := range []string{"AB", "ABCDE", "A B", "<X>", "☀☀☀"} {
		if corporations.ValidCompanyTag(s) {
			t.Fatal("invalid tag accepted", s)
		}
	}
}

func TestIceFieldTemperature(t *testing.T) {
	for _, lum := range []float64{.0078, 1, 280, 35000} {
		for _, site := range universe.SystemSites(universe.Star{ID: 2, Luminosity: lum}) {
			if site.Kind == "Ледяное поле" {
				temp := 278 * math.Pow(lum, .25) / math.Sqrt(site.Orbit)
				if temp > 180 {
					t.Fatal("ice belt overheated", temp)
				}
			}
		}
	}
}
