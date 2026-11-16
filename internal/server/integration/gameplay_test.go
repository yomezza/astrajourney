package integration

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/yomezza/cosmosimulator/internal/platform/dblayout"

	"github.com/yomezza/cosmosimulator/internal/fleet"
	"github.com/yomezza/cosmosimulator/internal/gameplay"
	resourcecatalog "github.com/yomezza/cosmosimulator/internal/resources"
	"github.com/yomezza/cosmosimulator/internal/server"
	"github.com/yomezza/cosmosimulator/internal/universe"
)

func TestGameplay032(t *testing.T) {
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
	schema := fmt.Sprintf("gameplay_%d", time.Now().UnixNano())
	defer base.Exec(ctx, "DROP SCHEMA IF EXISTS "+dblayout.DropList(schema)+" CASCADE")
	db, err := openDatabase(ctx, dsn, schema)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	a := server.New(db, server.Config{})
	h := a.Handler()
	request := func(c *http.Cookie, method, path, body string) *httptest.ResponseRecorder {
		r := httptest.NewRequest(method, path, strings.NewReader(body))
		r.RemoteAddr = "127.0.0.1:123"
		if c != nil {
			r.AddCookie(c)
		}
		if method == "POST" {
			r.Header.Set("Content-Type", "application/json")
		}
		w := httptest.NewRecorder()
		h.ServeHTTP(w, r)
		return w
	}
	check := func(w *httptest.ResponseRecorder, status int) {
		t.Helper()
		if w.Code != status {
			t.Fatalf("%d != %d: %s", w.Code, status, w.Body.String())
		}
	}
	signup := func(name string) (*http.Cookie, int64) {
		w := request(nil, "POST", "/api/register", fmt.Sprintf(`{"username":%q,"password":"long-test-password"}`, name))
		check(w, 200)
		var id int64
		if err = db.QueryRow(ctx, `SELECT id FROM users WHERE username=$1`, name).Scan(&id); err != nil {
			t.Fatal(err)
		}
		return w.Result().Cookies()[0], id
	}
	one, id1 := signup("quota_one")
	two, id2 := signup("quota_two")
	admin, aid := signup("quota_admin")
	if _, err = db.Exec(ctx, `UPDATE users SET role='admin' WHERE id=$1`, aid); err != nil {
		t.Fatal(err)
	}
	post := func(c *http.Cookie, path, body string, status int) {
		t.Helper()
		check(request(c, "POST", path, body), status)
	}
	g, err := gameplay.ReadGameplay(ctx, db)
	if err != nil || g.Common != 5000 || g.RareMin != 300 || g.RareMax != 600 || g.ResetSeconds != 18000 {
		t.Fatal("wrong defaults", g, err)
	}
	check(request(one, "GET", "/api/admin/gameplay", ""), 403)
	post(one, "/api/admin/gameplay", `{}`, 403)
	settings := `{"tick_seconds":2,"date_step":7,"quota_common":5000,"rare_min":300,"rare_max":600,"reset_seconds":18000}`
	post(admin, "/api/admin/gameplay", settings, 200)
	post(admin, "/api/admin/gameplay", `{"tick_seconds":0}`, 400)
	var beforePulse int64
	_ = db.QueryRow(ctx, `SELECT pulses FROM world WHERE id=1`).Scan(&beforePulse)
	post(admin, "/api/admin/tick", `{}`, 200)
	var afterPulse int64
	_ = db.QueryRow(ctx, `SELECT pulses FROM world WHERE id=1`).Scan(&afterPulse)
	if afterPulse != beforePulse+7 {
		t.Fatal("date step ignored")
	}
	for _, p := range []*http.Cookie{one, two} {
		post(p, "/api/expedition", `{"action":"scan"}`, 200)
		finishResearchForTest(t, a)
		post(p, "/api/expedition", `{"action":"move","target":"asteroids","mode":"approach"}`, 200)
		post(p, "/api/expedition", `{"action":"mine","planet":101,"resource":"iron"}`, 409)
	}
	if _, err = db.Exec(ctx, `UPDATE ships SET local_arrival=now()-interval '1 second'`); err != nil {
		t.Fatal(err)
	}
	for _, p := range []*http.Cookie{one, two} {
		check(request(p, "GET", "/api/expedition", ""), 200)
	}
	if _, err = db.Exec(ctx, `UPDATE resource_quotas SET used=capacity-4 WHERE star=1 AND object=101 AND resource='*'`); err != nil {
		t.Fatal(err)
	}
	var wg sync.WaitGroup
	codes := make(chan int, 2)
	for _, p := range []*http.Cookie{one, two} {
		wg.Add(1)
		go func(c *http.Cookie) {
			defer wg.Done()
			codes <- request(c, "POST", "/api/expedition", `{"action":"mine","planet":101,"resource":"iron"}`).Code
		}(p)
	}
	wg.Wait()
	close(codes)
	for code := range codes {
		if code != 200 {
			t.Fatal("last partial extraction failed", code)
		}
	}
	var total int
	if err = db.QueryRow(ctx, `SELECT sum(quantity) FROM mining_jobs WHERE user_id=ANY($1::bigint[])`, []int64{id1, id2}).Scan(&total); err != nil || total != 4 {
		t.Fatal("quota race over-extracted", total, err)
	}
	post(one, "/api/expedition", `{"action":"cancel_mine"}`, 200)
	var used int
	_ = db.QueryRow(ctx, `SELECT used FROM resource_quotas WHERE star=1 AND object=101 AND resource='*'`).Scan(&used)
	if used >= 5000 || used < 4997 {
		t.Fatal("cancellation did not refund reserved quota", used)
	}
	reset := `{"tick_seconds":2,"date_step":7,"quota_common":5000,"rare_min":300,"rare_max":600,"reset_seconds":18000,"reset_now":true}`
	post(admin, "/api/admin/gameplay", reset, 200)
	check(request(one, "GET", "/api/expedition", ""), 200)
	post(two, "/api/expedition", `{"action":"cancel_mine"}`, 200)
	_ = db.QueryRow(ctx, `SELECT used FROM resource_quotas WHERE star=1 AND object=101 AND resource='*'`).Scan(&used)
	if used != 0 {
		t.Fatal("old-window cancellation altered new quota", used)
	}
	var rareCap int
	_ = db.QueryRow(ctx, `SELECT capacity FROM resource_quotas WHERE star=1 AND object=101 AND resource='orichalcum'`).Scan(&rareCap)
	if rareCap < 300 || rareCap > 600 {
		t.Fatal("rare quota outside range", rareCap)
	}
	if _, err = db.Exec(ctx, `UPDATE resource_quotas SET used=capacity-2 WHERE star=1 AND object=101 AND resource='orichalcum'`); err != nil {
		t.Fatal(err)
	}
	post(one, "/api/expedition", `{"action":"mine","planet":101,"resource":"orichalcum"}`, 200)
	post(two, "/api/expedition", `{"action":"mine","planet":101,"resource":"orichalcum"}`, 409)
	post(admin, "/api/admin/tick", `{}`, 200)
	var cargo []byte
	_ = db.QueryRow(ctx, `SELECT cargo FROM ships WHERE user_id=$1`, id1).Scan(&cargo)
	var inventory map[string]int
	_ = json.Unmarshal(cargo, &inventory)
	if inventory["orichalcum"] != 2 || inventory["iron"] != 0 {
		t.Fatal("job delivery wrong", inventory)
	}
	if _, err = db.Exec(ctx, `UPDATE resource_quotas SET reset_at=now()-interval '1 second' WHERE star=1 AND object=101`); err != nil {
		t.Fatal(err)
	}
	check(request(one, "GET", "/api/expedition", ""), 200)
	_ = db.QueryRow(ctx, `SELECT used FROM resource_quotas WHERE star=1 AND object=101 AND resource='orichalcum'`).Scan(&used)
	if used != 0 {
		t.Fatal("wall-clock recovery failed")
	}
	post(admin, "/api/admin/gameplay", `{"tick_seconds":2,"date_step":7,"quota_common":10000,"rare_min":400,"rare_max":500,"reset_seconds":7200}`, 200)
	check(request(one, "GET", "/api/expedition", ""), 200)
	var cap int
	_ = db.QueryRow(ctx, `SELECT capacity FROM resource_quotas WHERE star=1 AND object=101 AND resource='*'`).Scan(&cap)
	if cap != 5000 {
		t.Fatal("active window changed without reset")
	}
	if _, err = db.Exec(ctx, `UPDATE resource_quotas SET reset_at=now()-interval '1 second' WHERE star=1 AND object=101`); err != nil {
		t.Fatal(err)
	}
	check(request(one, "GET", "/api/expedition", ""), 200)
	_ = db.QueryRow(ctx, `SELECT capacity FROM resource_quotas WHERE star=1 AND object=101 AND resource='*'`).Scan(&cap)
	if cap != 10000 {
		t.Fatal("new window ignored settings")
	}
	// Flights preserve route duration when the admin advances simulation time.
	post(one, "/api/expedition", `{"action":"travel","to":5}`, 200)
	w := request(one, "GET", "/api/fleet", "")
	check(w, 200)
	var fleet struct {
		Ships []fleet.Ship `json:"ships"`
	}
	if err = json.Unmarshal(w.Body.Bytes(), &fleet); err != nil {
		t.Fatal(err)
	}
	if len(fleet.Ships) != 1 || len(fleet.Ships[0].Path) != 2 || fleet.Ships[0].Departure == nil {
		t.Fatal("flight metadata missing")
	}
	shipID := fleet.Ships[0].ID
	post(admin, "/api/admin/tick", `{}`, 200)
	var depart, arrival time.Time
	_ = db.QueryRow(ctx, `SELECT departure,arrival FROM ships WHERE user_id=$1`, id1).Scan(&depart, &arrival)
	if arrival.Sub(depart) < 4900*time.Millisecond || arrival.Sub(depart) > 5100*time.Millisecond {
		t.Fatal("forced time distorted route duration", arrival.Sub(depart))
	}
	if _, err = db.Exec(ctx, `UPDATE ships SET arrival=now()-interval '1 second' WHERE user_id=$1`, id1); err != nil {
		t.Fatal(err)
	}
	w = request(one, "GET", "/api/fleet", "")
	check(w, 200)
	_ = json.Unmarshal(w.Body.Bytes(), &fleet)
	if fleet.Ships[0].Star != 5 || fleet.Ships[0].ObjectID != "entry" || fleet.Ships[0].ID != shipID {
		t.Fatal("arrival position or identity wrong")
	}
	post(one, "/api/expedition", `{"action":"buy","resource":"agni","quantity":1}`, 409)
	post(one, "/api/expedition", `{"action":"move","target":"station-2","mode":"docked"}`, 200)
	if _, err = db.Exec(ctx, `UPDATE ships SET local_arrival=now()-interval '1 second' WHERE user_id=$1`, id1); err != nil {
		t.Fatal(err)
	}
	var goods []string
	if err = db.QueryRow(ctx, `SELECT goods FROM stations WHERE star=5 AND object_id='station-2'`).Scan(&goods); err != nil || len(goods) == 0 {
		t.Fatal("station catalogue", goods, err)
	}
	post(one, "/api/expedition", fmt.Sprintf(`{"action":"buy","resource":%q,"quantity":1}`, goods[0]), 200)
	w = request(two, "GET", "/api/fleet", "")
	check(w, 200)
	_ = json.Unmarshal(w.Body.Bytes(), &fleet)
	if fleet.Ships[0].ID == shipID {
		t.Fatal("other player's fleet leaked")
	}
	post(one, "/api/expedition", `{"action":"move","target":"station","mode":"orbit"}`, 409)
	post(one, "/api/expedition", `{"action":"scan"}`, 200)
	finishResearchForTest(t, a)
	post(one, "/api/expedition", `{"action":"move","target":"planet-1","mode":"orbit"}`, 200)
	if _, err = db.Exec(ctx, `UPDATE ships SET local_arrival=now()-interval '1 second' WHERE user_id=$1`, id1); err != nil {
		t.Fatal(err)
	}
	w = request(one, "GET", "/api/fleet", "")
	check(w, 200)
	_ = json.Unmarshal(w.Body.Bytes(), &fleet)
	if fleet.Ships[0].PositionMode != "orbit" || fleet.Ships[0].ObjectID != "planet-1" {
		t.Fatal("planetary orbit was not persisted")
	}
	// A v0.3.1 in-flight row is upgraded without changing destination, ETA or cargo.
	future := time.Now().Add(time.Minute)
	if _, err = db.Exec(ctx, `UPDATE ships SET star=1,destination=5,arrival=$2,departure=NULL,travel_path='[]' WHERE user_id=$1`, id1, future); err != nil {
		t.Fatal(err)
	}
	again, err := openDatabase(ctx, dsn, schema)
	if err != nil {
		t.Fatal(err)
	}
	again.Close()
	var path []byte
	_ = db.QueryRow(ctx, `SELECT arrival,departure,travel_path FROM ships WHERE user_id=$1`, id1).Scan(&arrival, &depart, &path)
	if arrival.Sub(future) > time.Millisecond || len(path) < 3 {
		t.Fatal("legacy flight migration lost state")
	}
	// Pagination/search are server-side and bounded to 10 rows.
	for i := 0; i < 17; i++ {
		if _, err = db.Exec(ctx, `INSERT INTO users(username,password_hash) VALUES($1,'unused')`, fmt.Sprintf("search_user_%02d", i)); err != nil {
			t.Fatal(err)
		}
	}
	w = request(admin, "GET", "/api/admin/users?page=2&q=SEARCH_USER_", "")
	check(w, 200)
	var page struct {
		Items              []map[string]any `json:"items"`
		Total, Page, Pages int
	}
	if err = json.Unmarshal(w.Body.Bytes(), &page); err != nil {
		t.Fatal(err)
	}
	if page.Total != 17 || page.Page != 2 || len(page.Items) != 7 {
		t.Fatal("pagination failed", page)
	}
	if strings.Contains(w.Body.String(), "password") {
		t.Fatal("password data leaked")
	}
	check(request(one, "GET", "/api/admin/users?q=search", ""), 403)
	check(request(one, "GET", "/api/events", ""), 403)
	check(request(admin, "GET", "/api/admin/log", ""), 200)
}

func TestResourceDistribution(t *testing.T) {
	codes := map[string]bool{}
	for _, r := range resourcecatalog.All {
		m := resourcecatalog.Meta(r.ID)
		if m.Code == "" || codes[m.Code] {
			t.Fatal("missing/duplicate resource code")
		}
		codes[m.Code] = true
	}
	iron, rare, withoutIron := 0, 0, 0
	for star := 1; star <= 1000; star++ {
		hasIron := false
		for _, id := range universe.VariedResources(star, 101, "Пояс астероидов") {
			if id == "iron" {
				iron++
				hasIron = true
			}
			if resourcecatalog.Meta(id).Rarity == "Редкий" {
				rare++
			}
		}
		if !hasIron {
			withoutIron++
		}
	}
	if withoutIron == 0 || iron < 500 || rare >= iron {
		t.Fatal("resource distribution is uniform or rare too common", iron, rare, withoutIron)
	}
}

func TestEdemChronology(t *testing.T) {
	for pulses, want := range map[int64]string{0: "ЭД 0001·001·000", 1000: "ЭД 0001·002·000", 359999: "ЭД 0001·360·999", 360000: "ЭД 0002·001·000"} {
		if gameplay.EdemDate(pulses) != want {
			t.Fatal("invalid date", pulses, gameplay.EdemDate(pulses))
		}
	}
}
