package integration

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/yomezza/cosmosimulator/internal/discovery"
	"github.com/yomezza/cosmosimulator/internal/platform/identifiers"
	"github.com/yomezza/cosmosimulator/internal/universe"
)

func uuid250(t *testing.T) string {
	t.Helper()
	id, e := identifiers.UUID()
	if e != nil {
		t.Fatal(e)
	}
	return id
}
func neutral250(t *testing.T, f *fixture033) universe.Star {
	t.Helper()
	g, e := f.app.World.Load(f.ctx)
	if e != nil {
		t.Fatal(e)
	}
	for _, s := range g.Stars {
		if !s.Deleted && s.Founder == "" && len(s.Planets) > 0 {
			return s
		}
	}
	t.Fatal("no neutral star")
	return universe.Star{}
}

func TestDiscoveryClaimsAndNaming250(t *testing.T) {
	f := newFixture033(t)
	one, uid1 := f.signup("first250")
	two, uid2 := f.signup("second250")
	s := neutral250(t, f)
	g0, _ := f.app.World.Load(f.ctx)
	if f.integer(`SELECT count(*) FROM aj_discovery_objects WHERE kind='system'`) != int64(len(g0.Stars)) {
		t.Fatal("fresh-install registry was not seeded after world generation")
	}
	var hidden struct {
		Discoveries []any `json:"discoveries"`
	}
	json.Unmarshal(f.req(one, "GET", fmt.Sprintf("/api/system?id=%d", s.ID), nil, 200).Body.Bytes(), &hidden)
	if len(hidden.Discoveries) != 1 {
		t.Fatal("hidden planet discovery leaked")
	}
	tx, e := f.db.Begin(f.ctx)
	if e != nil {
		t.Fatal(e)
	}
	defer tx.Rollback(context.Background())
	if e = discovery.ClaimScan(f.ctx, tx, uid1, s, 0, 1, true); e != nil {
		t.Fatal(e)
	}
	competing := make(chan error, 1)
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		other, e := f.db.Begin(ctx)
		if e != nil {
			competing <- e
			return
		}
		defer other.Rollback(context.Background())
		if e = discovery.ClaimScan(ctx, other, uid2, s, 0, 1, true); e == nil {
			e = other.Commit(ctx)
		}
		competing <- e
	}()
	// Rollback of the apparent first claimant must release both rights.
	if e = tx.Rollback(f.ctx); e != nil {
		t.Fatal(e)
	}
	if e = <-competing; e != nil {
		t.Fatal(e)
	}
	if f.integer(`SELECT discoverer_id FROM aj_discovery_objects WHERE kind='system' AND star_id=$1`, s.ID) != uid2 {
		t.Fatal("wrong commit winner")
	}
	if f.integer(`SELECT discoverer_id FROM aj_discovery_objects WHERE kind='planet' AND star_id=$1 AND planet_id=$2`, s.ID, s.Planets[0].ID) != uid2 {
		t.Fatal("planet scan claim missing")
	}
	body := map[string]any{"operation": uuid250(t), "object": map[string]any{"kind": "system", "star_id": s.ID, "planet_id": 0}, "name": "Заря Пилотов Тест"}
	f.req(one, "POST", "/api/discovery/name", body, 403)
	first := f.req(two, "POST", "/api/discovery/name", body, 200)
	replay := f.req(two, "POST", "/api/discovery/name", body, 200)
	var firstJSON, replayJSON any
	json.Unmarshal(first.Body.Bytes(), &firstJSON)
	json.Unmarshal(replay.Body.Bytes(), &replayJSON)
	if !reflect.DeepEqual(firstJSON, replayJSON) {
		t.Fatal("replay changed the receipt")
	}
	body["name"] = "Иная Заря"
	f.req(two, "POST", "/api/discovery/name", body, 409)
	body["operation"] = uuid250(t)
	f.req(two, "POST", "/api/discovery/name", body, 409)
	g, e := f.app.World.Load(f.ctx)
	if e != nil || g.Stars[s.ID-1].Name != "Заря Пилотов Тест" || g.Stars[s.ID-1].DiscoverySeed != s.DiscoverySeed {
		t.Fatal("world mirror/cache or seed changed", e)
	}
	if f.integer(`SELECT count(*) FROM aj_discovery_name_history`) != 1 {
		t.Fatal("duplicate rename history")
	}
	f.exec(`UPDATE users SET role='admin' WHERE id=$1`, uid1)
	correction := map[string]any{"operation": uuid250(t), "object": body["object"], "name": "Заря Исправленная Тест", "reason": "moderation"}
	f.req(one, "POST", "/api/admin/discovery/name", correction, 200)
	if f.integer(`SELECT count(*) FROM aj_discovery_objects WHERE kind='system' AND star_id=$1 AND rename_used_at IS NOT NULL`, s.ID) != 1 {
		t.Fatal("admin restored naming right")
	}
	// Two different commands race for the planet's one naming entitlement.
	responses := make(chan int, 2)
	for _, name := range []string{"Планета Рассвет Тест", "Планета Горизонт Тест"} {
		raw, _ := json.Marshal(map[string]any{"operation": uuid250(t), "object": map[string]any{"kind": "planet", "star_id": s.ID, "planet_id": s.Planets[0].ID}, "name": name})
		go func() {
			r := httptest.NewRequest("POST", "/api/discovery/name", strings.NewReader(string(raw)))
			r.AddCookie(two)
			r.Header.Set("Content-Type", "application/json")
			w := httptest.NewRecorder()
			f.handler.ServeHTTP(w, r)
			responses <- w.Code
		}()
	}
	x, y := <-responses, <-responses
	if !((x == 200 && y == 409) || (x == 409 && y == 200)) {
		t.Fatal("concurrent naming entitlement", x, y)
	}
	if f.integer(`SELECT count(*) FROM aj_discovery_name_history WHERE kind='planet' AND star_id=$1`, s.ID) != 1 {
		t.Fatal("concurrent planet rename duplicated history")
	}
}

func TestDiscoveryPersonalCompletion250(t *testing.T) {
	f := newFixture033(t)
	c, uid := f.signup("scan250")
	s := neutral250(t, f)
	f.exec(`UPDATE ships SET star=$2,object_id='',destination=NULL,local_target=NULL WHERE user_id=$1`, uid, s.ID)
	f.cmd(c, map[string]any{"action": "explore", "to": s.ID}, 200)
	f.exec(`UPDATE research_jobs SET next_at=clock_timestamp()-interval '1 day' WHERE user_id=$1`, uid)
	f.advance(uid)
	if f.integer(`SELECT count(*) FROM aj_discovery_objects WHERE star_id=$1 AND discoverer_id=$2`, s.ID, uid) != int64(1+len(s.Planets)) {
		t.Fatal("completion lost the final planet scan or system claim")
	}
	// Founder protection and previously known/purchased data cannot acquire rights.
	g, _ := f.app.World.Load(f.ctx)
	other := g.Stars[len(g.Stars)-1].ID
	f.exec(`INSERT INTO surveys(user_id,star,source) VALUES($1,$2,'purchased') ON CONFLICT DO NOTHING`, uid, other)
	f.req(c, "GET", fmt.Sprintf("/api/system?id=%d", other), nil, 200)
	if f.integer(`SELECT count(*) FROM aj_discovery_objects WHERE star_id=$2 AND discoverer_id=$1`, uid, other) != 0 {
		t.Fatal("purchased/founder data granted ownership")
	}
	// Simulate seeding registry metadata for an already-known old world. Never
	// infer its historical first player from a purchased survey or timestamps.
	f.exec(`DELETE FROM aj_discovery_objects WHERE star_id=$1`, other)
	var untouchedBefore, untouchedAfter string
	if err := f.db.QueryRow(f.ctx, `SELECT xmin::text FROM aj_discovery_objects WHERE kind='system' AND star_id=$1`, s.ID).Scan(&untouchedBefore); err != nil {
		t.Fatal(err)
	}
	tx, err := f.db.Begin(f.ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback(context.Background())
	if _, err = tx.Exec(f.ctx, `SELECT pg_advisory_xact_lock(784218)`); err == nil {
		err = discovery.Sync(f.ctx, tx, g)
	}
	if err == nil {
		err = tx.Commit(f.ctx)
	}
	if err != nil {
		t.Fatal(err)
	}
	if err := f.db.QueryRow(f.ctx, `SELECT xmin::text FROM aj_discovery_objects WHERE kind='system' AND star_id=$1`, s.ID).Scan(&untouchedAfter); err != nil {
		t.Fatal(err)
	}
	if untouchedBefore != untouchedAfter {
		t.Fatal("registry sync rewrote an unchanged object")
	}
	if g.Stars[other-1].Founder == "" && f.integer(`SELECT count(*) FROM aj_discovery_objects WHERE star_id=$1 AND claim_state='legacy_unattributed' AND discoverer_id IS NULL`, other) != int64(1+len(g.Stars[other-1].Planets)) {
		t.Fatal("legacy seeding invented an owner")
	}
	for _, founder := range g.Stars {
		if founder.Founder == "" {
			continue
		}
		if f.integer(`SELECT count(*) FROM aj_discovery_objects WHERE star_id=$1 AND protected_founder_key=$2 AND discoverer_id IS NULL`, founder.ID, founder.Founder) != int64(1+len(founder.Planets)) {
			t.Fatal("founder protection missing")
		}
		f.req(c, "POST", "/api/discovery/name", map[string]any{"operation": uuid250(t), "object": map[string]any{"kind": "system", "star_id": founder.ID, "planet_id": 0}, "name": "Чужое Название Тест"}, 403)
		break
	}
}
