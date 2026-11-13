package integration

import (
	"math"
	"testing"

	"github.com/coder/websocket"
	"github.com/yomezza/cosmosimulator/internal/planet/scene"
	"github.com/yomezza/cosmosimulator/internal/planet/scenestore"
)

func proof253(t *testing.T, conn *websocket.Conn, w map[string]any, seq int) string {
	t.Helper()
	operation := op180(t)
	command190(t, conn, w, seq, "return.prepare", map[string]string{"operation": operation})
	proof := read190(t, conn, "return.authorized")
	if proof["operation"] != operation {
		t.Fatal("wrong return proof", proof)
	}
	return operation
}

func TestPlanetReturnProof253(t *testing.T) {
	f, c, uid, surface, site, ship := prepare180(t)
	base, stop := startPlanet150(t, f.db)
	defer stop()
	conn, w := ws180(t, base, c, map[string]any{"ticket": ticket180(t, f, c, op180(t), site, ship)["ticket"]})
	visit := w["visit"].(string)
	f.req(c, "POST", "/api/planet/return", map[string]any{"operation": op180(t), "visit": visit}, 403)
	f.req(c, "POST", "/api/planet/recover", map[string]any{"operation": op180(t), "visit": visit}, 409)
	command190(t, conn, w, 2, "leave", map[string]string{"operation": op180(t)})
	old := read190(t, conn, "error")
	if old["code"] != "return_authorization_required" {
		t.Fatal("legacy return bypass", old)
	}
	stale := proof253(t, conn, w, 3)
	conn.CloseNow()
	wait180(t, f, `SELECT count(*) FROM planet_visits WHERE user_id=$1 AND state='disconnected'`, 1, uid)
	conn, w = ws180(t, base, c, map[string]any{"resume_visit": visit})
	f.req(c, "POST", "/api/planet/return", map[string]any{"operation": stale, "visit": visit}, 403)
	expired := proof253(t, conn, w, 2)
	f.exec(`UPDATE aj_planet_return_authorizations SET authorized_at=clock_timestamp()-interval '2 minutes',expires_at=clock_timestamp()-interval '1 minute' WHERE user_id=$1 AND operation=$2`, uid, expired)
	f.req(c, "POST", "/api/planet/return", map[string]any{"operation": expired, "visit": visit}, 403)
	conn.CloseNow()
	wait180(t, f, `SELECT count(*) FROM planet_visits WHERE user_id=$1 AND state='disconnected'`, 1, uid)
	var fence scenestore.Fence
	if e := f.db.QueryRow(f.ctx, `SELECT owner_id::text,fence FROM planet_runtime_leases WHERE id=1`).Scan(&fence.Owner, &fence.Number); e != nil {
		t.Fatal(e)
	}
	local, e := scenestore.New(f.db).Load(f.ctx, scene.Key{Surface: surface, Sector: 1}, fence)
	if e != nil {
		t.Fatal(e)
	}
	spawn := local.Metadata().Landing
	farX, farY := 0, 0
	for y := 1; y < 255 && farX == 0; y++ {
		for x := 1; x < 255; x++ {
			if local.Walkable(x, y) && math.Hypot(float64(x*1024+512-spawn.SpawnX), float64(y*1024+512-spawn.SpawnY)) > 12*1024 {
				farX, farY = x*1024+512, y*1024+512
				break
			}
		}
	}
	if farX == 0 {
		t.Fatal("fixture lacks a walkable distant cell")
	}
	// Fixture-only placement of the persisted character before a new epoch;
	// the browser/HTTP protocol exposes no coordinate setter.
	f.exec(`UPDATE planet_characters SET x_q=$2,y_q=$3,position_version=position_version+1 WHERE user_id=$1`, uid, farX, farY)
	conn, w = ws180(t, base, c, map[string]any{"resume_visit": visit})
	command190(t, conn, w, 2, "return.prepare", map[string]string{"operation": op180(t)})
	denied := read190(t, conn, "error")
	if denied["code"] != "return_near_landing_required" {
		t.Fatal("far return was not denied", denied)
	}
	f.req(c, "POST", "/api/planet/recover", map[string]any{"operation": op180(t), "visit": visit}, 409)
	// Logging out suspends a character; it cannot serve as a far-return bypass.
	f.req(c, "POST", "/api/logout", map[string]any{}, 200)
	var username string
	if e = f.db.QueryRow(f.ctx, `SELECT username FROM users WHERE id=$1`, uid).Scan(&username); e != nil {
		t.Fatal(e)
	}
	login := f.req(nil, "POST", "/api/login", map[string]any{"username": username, "password": "1"}, 200)
	c = login.Result().Cookies()[0]
	f.req(c, "GET", "/api/planet/visit", nil, 200)
	if f.integer(`SELECT count(*) FROM planet_transfer_controls WHERE user_id=$1 AND reserved_ship IS NOT NULL`, uid) != 1 {
		t.Fatal("logout bypassed the landing requirement")
	}

	conn.CloseNow()
	wait180(t, f, `SELECT count(*) FROM planet_visits WHERE user_id=$1 AND state='disconnected'`, 1, uid)
	f.exec(`UPDATE planet_characters SET x_q=$2,y_q=$3,position_version=position_version+1 WHERE user_id=$1`, uid, spawn.SpawnX, spawn.SpawnY)
	conn, w = ws180(t, base, c, map[string]any{"resume_visit": visit})
	operation := proof253(t, conn, w, 2)
	body := map[string]any{"operation": operation, "visit": visit}
	f.req(c, "POST", "/api/planet/return", body, 200)
	f.req(c, "POST", "/api/planet/return", body, 200)
	wait180(t, f, `SELECT count(*) FROM planet_visits WHERE user_id=$1 AND state='ended'`, 1, uid)
	f.req(c, "GET", "/api/planet/visit", nil, 200)
	if f.integer(`SELECT count(*) FROM aj_planet_return_authorizations WHERE user_id=$1 AND state='consumed'`, uid) != 1 || f.integer(`SELECT count(*) FROM planet_transfer_controls WHERE reserved_ship=$1`, ship) != 0 {
		t.Fatal("proof consumption or reservation release failed")
	}
	f.req(c, "POST", "/api/planet/return", map[string]any{"operation": operation, "visit": op180(t)}, 409)
}
