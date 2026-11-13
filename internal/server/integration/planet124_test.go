package integration

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"reflect"
	"sync"
	"testing"
	"time"

	"github.com/yomezza/cosmosimulator/internal/colonies"
)

func preview124(f *fixture033, c *http.Cookie, planet int) colonies.SurfaceResetInfo {
	f.t.Helper()
	w := f.req(c, "GET", fmt.Sprintf("/api/admin/planet/surface?star=1&planet=%d", planet), nil, 200)
	var v colonies.SurfaceResetInfo
	if err := json.Unmarshal(w.Body.Bytes(), &v); err != nil {
		f.t.Fatal(err)
	}
	return v
}

func input124(info colonies.SurfaceResetInfo, action, request string) map[string]any {
	phrase := "УДАЛИТЬ"
	if action == "regenerate" {
		phrase = "ПЕРЕГЕНЕРИРОВАТЬ"
	}
	return map[string]any{"action": action, "star": info.Star, "planet": info.Planet, "token": info.Token, "confirm": phrase, "forfeit": true, "request": request}
}

func TestPlanetReset124(t *testing.T) {
	f, c, uid, surface, sectors := setupPlanet120(t)
	planet := int(f.integer(`SELECT planet FROM planet_surfaces WHERE id=$1`, surface))
	path := fmt.Sprintf("/api/admin/planet/surface?star=1&planet=%d", planet)
	f.req(c, "GET", path, nil, 403)
	f.req(c, "POST", "/api/admin/planet/surface", map[string]any{}, 403)
	f.exec(`UPDATE users SET role='admin' WHERE id=$1`, uid)
	var initialSeed string
	if err := f.db.QueryRow(f.ctx, `SELECT seed FROM planet_surfaces WHERE id=$1`, surface).Scan(&initialSeed); err != nil {
		t.Fatal(err)
	}
	initial := preview124(f, c, planet)
	if initial.Surface != surface || initial.Sectors != 100 {
		t.Fatal("bad preview", initial)
	}
	invalid := input124(initial, "delete", "invalid-confirm-124")
	invalid["confirm"] = "DELETE"
	f.req(c, "POST", "/api/admin/planet/surface", invalid, 400)
	if f.integer(`SELECT count(*) FROM planet_surfaces WHERE id=$1`, surface) != 1 {
		t.Fatal("confirmation mutated data")
	}
	create := map[string]any{"action": "create", "surface": surface, "sector": sectors[0], "request": "create-reset-124"}
	w := f.req(c, "POST", "/api/planet/action", create, 200)
	var made struct {
		ID int64 `json:"project"`
	}
	_ = json.Unmarshal(w.Body.Bytes(), &made)
	f.req(c, "POST", "/api/planet/action", map[string]any{"action": "contribute", "id": made.ID, "resource": "credits", "amount": 101, "request": "fund-reset-124"}, 200)
	// A fresh state invalidates the confirmation shown before funding.
	f.req(c, "POST", "/api/admin/planet/surface", input124(initial, "delete", "stale-confirm-124"), 409)
	info := preview124(f, c, planet)
	if info.Invested["credits"] != 101 || info.Projects != 1 {
		t.Fatal("missing obligations", info)
	}
	noForfeit := input124(info, "delete", "no-forfeit-124")
	noForfeit["forfeit"] = false
	f.req(c, "POST", "/api/admin/planet/surface", noForfeit, 409)
	contribution := f.integer(`SELECT id FROM planet_contributions WHERE project=$1`, made.ID)
	f.exec(`INSERT INTO planet_refunds(contribution,user_id,resource,amount,star,station) VALUES($1,$2,'credits',10,1,'station')`, contribution, uid)
	blocked := preview124(f, c, planet)
	if blocked.Blocked == "" {
		t.Fatal("refund did not block")
	}
	f.req(c, "POST", "/api/admin/planet/surface", input124(blocked, "delete", "refund-block-124"), 409)
	f.exec(`DELETE FROM planet_refunds WHERE contribution=$1`, contribution)
	balance := f.integer(`SELECT credits FROM users WHERE id=$1`, uid)
	ledger := f.integer(`SELECT count(*) FROM financial_ledger WHERE user_id=$1`, uid)
	ships := f.integer(`SELECT count(*) FROM ships`)
	users := f.integer(`SELECT count(*) FROM users`)
	companies := f.integer(`SELECT count(*) FROM companies`)
	info = preview124(f, c, planet)
	body := input124(info, "delete", "delete-reset-124")
	f.req(c, "POST", "/api/admin/planet/surface", body, 200)
	f.req(c, "POST", "/api/admin/planet/surface", body, 200)
	for _, table := range []string{"planet_surfaces", "planet_sectors", "planet_projects", "planet_colonies", "planet_events", "planet_contributions", "planet_refunds"} {
		if f.integer(`SELECT count(*) FROM `+table) != 0 {
			t.Fatal("orphan", table)
		}
	}
	if f.integer(`SELECT credits FROM users WHERE id=$1`, uid) != balance || f.integer(`SELECT count(*) FROM financial_ledger WHERE user_id=$1`, uid) != ledger {
		t.Fatal("reset modified finance")
	}
	if ships != f.integer(`SELECT count(*) FROM ships`) || users != f.integer(`SELECT count(*) FROM users`) || companies != f.integer(`SELECT count(*) FROM companies`) {
		t.Fatal("independent objects deleted")
	}
	f.req(c, "POST", "/api/planet/action", create, 409)
	w = f.req(c, "GET", fmt.Sprintf("/api/planet/surface?star=1&planet=%d", planet), nil, 200)
	var absent struct {
		Absent bool `json:"absent"`
	}
	_ = json.Unmarshal(w.Body.Bytes(), &absent)
	if !absent.Absent {
		t.Fatal("view recreated deleted surface")
	}
	f.req(c, "GET", fmt.Sprintf("/api/planet/state?surface=%d", surface), nil, 404)
	// Empty tombstone can be regenerated; repeated receipt never creates twice.
	info = preview124(f, c, planet)
	body = input124(info, "regenerate", "regen-reset-124")
	first := f.req(c, "POST", "/api/admin/planet/surface", body, 200)
	again := f.req(c, "POST", "/api/admin/planet/surface", body, 200)
	var firstResult, againResult map[string]any
	_ = json.Unmarshal(first.Body.Bytes(), &firstResult)
	_ = json.Unmarshal(again.Body.Bytes(), &againResult)
	if !reflect.DeepEqual(firstResult, againResult) {
		t.Fatal("non-idempotent regeneration", firstResult, againResult)
	}
	id := f.integer(`SELECT id FROM planet_surfaces WHERE star=1 AND planet=$1`, planet)
	var regeneratedSeed string
	if err := f.db.QueryRow(f.ctx, `SELECT seed FROM planet_surfaces WHERE id=$1`, id).Scan(&regeneratedSeed); err != nil {
		t.Fatal(err)
	}
	if regeneratedSeed == initialSeed {
		t.Fatal("regeneration reused the old seed")
	}
	if id == surface || f.integer(`SELECT count(*) FROM planet_sectors WHERE surface=$1`, id) != 100 {
		t.Fatal("wrong replacement")
	}
	// Two concurrent destructive requests: only one may commit.
	info = preview124(f, c, planet)
	var wg sync.WaitGroup
	codes := make(chan int, 2)
	for i := 0; i < 2; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			raw, _ := json.Marshal(input124(info, "regenerate", fmt.Sprintf("race-reset-124-%d", i)))
			r := httptest.NewRequest("POST", "/api/admin/planet/surface", bytes.NewReader(raw))
			r.Header.Set("Content-Type", "application/json")
			r.AddCookie(c)
			w := httptest.NewRecorder()
			f.handler.ServeHTTP(w, r)
			codes <- w.Code
		}(i)
	}
	wg.Wait()
	close(codes)
	success, conflict := 0, 0
	for code := range codes {
		if code == 200 {
			success++
		} else if code == 409 {
			conflict++
		} else {
			t.Fatal("unexpected race result", code)
		}
	}
	if success != 1 || conflict != 1 {
		t.Fatal("concurrent double reset", success, conflict)
	}
	id = f.integer(`SELECT id FROM planet_surfaces WHERE star=1 AND planet=$1`, planet)
	info = preview124(f, c, planet)
	// Force failure *after* the old records were deleted; rollback must restore all.
	f.exec(`CREATE FUNCTION reject_sector124() RETURNS trigger LANGUAGE plpgsql AS $$ BEGIN RAISE EXCEPTION 'test-only failed sector insert'; END $$; CREATE TRIGGER reject_sector124 BEFORE INSERT ON planet_sectors FOR EACH ROW EXECUTE FUNCTION reject_sector124()`)
	f.req(c, "POST", "/api/admin/planet/surface", input124(info, "regenerate", "rollback-reset-124"), 500)
	if f.integer(`SELECT id FROM planet_surfaces WHERE star=1 AND planet=$1`, planet) != id || f.integer(`SELECT count(*) FROM planet_sectors WHERE surface=$1`, id) != 100 {
		t.Fatal("failed replacement destroyed surface")
	}
	if preview124(f, c, planet).Token != info.Token {
		t.Fatal("failed replacement changed revision")
	}
	if f.integer(`SELECT count(*) FROM admin_log WHERE action='planet_surface_reset'`) < 6 {
		t.Fatal("missing audit trail")
	}
}

func TestPlanetResetVisits124(t *testing.T) {
	f, c, uid, surface, sectors := setupPlanet120(t)
	f.exec(`UPDATE users SET role='admin' WHERE id=$1`, uid)
	planet := int(f.integer(`SELECT planet FROM planet_surfaces WHERE id=$1`, surface))
	f.req(c, "POST", "/api/planet/action", map[string]any{"action": "create", "surface": surface, "sector": sectors[0], "request": "visit-create-124"}, 200)
	pid := f.integer(`SELECT id FROM planet_projects WHERE surface=$1`, surface)
	// A corporate colony retains an independent corporation after reset.
	company := f.integer(`SELECT company_id FROM users WHERE id=$1`, uid)
	f.exec(`UPDATE planet_projects SET owner_user=NULL,owner_company=$2,build_ticks=2 WHERE id=$1`, pid, company)
	f.req(c, "POST", "/api/planet/action", map[string]any{"action": "admin_fill", "id": pid, "request": "visit-fill-124"}, 200)
	building := preview124(f, c, planet)
	if err := f.app.Simulation.AdvanceTick(f.ctx, true); err != nil {
		t.Fatal(err)
	}
	if preview124(f, c, planet).Token != building.Token {
		t.Fatal("ordinary construction progress made confirmation unusable")
	}
	if err := f.app.Simulation.AdvanceTick(f.ctx, true); err != nil {
		t.Fatal(err)
	}
	f.exec(`UPDATE ships SET star=1,object_id=$2,position_mode='orbit',destination=NULL,local_target=NULL WHERE user_id=$1 AND active`, uid, fmt.Sprintf("planet-%d", planet))
	// P2-2 separates visiting from colonization/admin role.
	f.exec(`INSERT INTO company_roles(company_id,name,permissions) VALUES($1,'Planet reset visitor','["planetary.enter"]')`, company)
	f.exec(`UPDATE users SET company_role=(SELECT id FROM company_roles WHERE company_id=$2 AND name='Planet reset visitor') WHERE id=$1`, uid, company)
	// A real first-queue record, migrated as legacy, still blocks unsafe reset.
	f.exec(`INSERT INTO planet_visits(user_id,colony,ship) SELECT $1,c.id,s.id FROM planet_colonies c JOIN ships s ON s.user_id=$1 AND s.active WHERE c.project=$2`, uid, pid)
	info := preview124(f, c, planet)
	if info.Visits != 1 || info.Colonies != 1 || info.Corporate != 1 || info.Blocked == "" {
		t.Fatal("unsafe colony preview", info)
	}
	f.req(c, "POST", "/api/admin/planet/surface", input124(info, "regenerate", "visit-block-124"), 409)
	_, stopPlanet := startPlanet150(t, f.db)
	f.req(c, "POST", "/api/planet/recover", map[string]any{"operation": "00000000-0000-4000-8000-000000000124"}, 200)
	deadline := time.Now().Add(4 * time.Second)
	for f.integer(`SELECT count(*) FROM planet_visits WHERE state<>'ended'`) > 0 && time.Now().Before(deadline) {
		time.Sleep(10 * time.Millisecond)
	}
	stopPlanet()
	info = preview124(f, c, planet)
	f.req(c, "POST", "/api/admin/planet/surface", input124(info, "regenerate", "visit-regen-124"), 200)
	if f.integer(`SELECT count(*) FROM companies WHERE id=$1`, company) != 1 {
		t.Fatal("corporation destroyed")
	}
	if f.integer(`SELECT count(*) FROM planet_visits WHERE state<>'ended'`)+f.integer(`SELECT count(*) FROM planet_colonies`)+f.integer(`SELECT count(*) FROM notifications WHERE kind='colony'`) != 0 {
		t.Fatal("stale colony state")
	}
}
