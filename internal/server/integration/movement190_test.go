package integration

import (
	"context"
	"errors"
	"github.com/coder/websocket"
	"github.com/coder/websocket/wsjson"
	"github.com/yomezza/cosmosimulator/internal/account"
	"github.com/yomezza/cosmosimulator/internal/planet/access"
	"github.com/yomezza/cosmosimulator/internal/planet/scene"
	"github.com/yomezza/cosmosimulator/internal/planet/scenestore"
	"github.com/yomezza/cosmosimulator/internal/planet/visits"
	"math"
	"testing"
	"time"
)

func read190(t *testing.T, c *websocket.Conn, kind string) map[string]any {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	for {
		var v struct {
			Type    string         `json:"type"`
			Payload map[string]any `json:"payload"`
		}
		if err := wsjson.Read(ctx, c, &v); err != nil {
			t.Fatal(err)
		}
		if v.Type == kind {
			return v.Payload
		}
	}
}
func command190(t *testing.T, c *websocket.Conn, w map[string]any, seq int, kind string, payload any) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	if err := wsjson.Write(ctx, c, map[string]any{"v": 1, "type": kind, "seq": seq, "visit": w["visit"], "connection_epoch": w["connection_epoch"], "payload": payload}); err != nil {
		t.Fatal(err)
	}
}
func TestPlanetMovement190(t *testing.T) {
	f, c, uid, _, site, ship := prepare180(t)
	base, stop := startPlanet150(t, f.db)
	defer stop()
	ticket := ticket180(t, f, c, op180(t), site, ship)
	conn, w := ws180(t, base, c, map[string]any{"ticket": ticket["ticket"]})
	if w["message_type"] != "welcome" {
		t.Fatal(w)
	}
	x, y := int(w["x_q"].(float64)), int(w["y_q"].(float64))
	start := time.Now()
	command190(t, conn, w, 2, "move", map[string]int{"x_q": x + 2*1024, "y_q": y})
	read190(t, conn, "path.accepted")
	if err := f.app.Simulation.AdvanceTick(f.ctx, true); err != nil {
		t.Fatal("cosmic tick during movement", err)
	}
	var moving map[string]any
	for time.Since(start) < 3*time.Second {
		moving = read190(t, conn, "state.delta")
		if moving["x_q"].(float64) > float64(x)+1024 {
			break
		}
	}
	dx, dy := moving["x_q"].(float64)-float64(x), moving["y_q"].(float64)-float64(y)
	if math.Hypot(dx, dy)/1024 > 4*(time.Since(start).Seconds()+.1) {
		t.Fatal("client accelerated server movement")
	}
	if dx < 1024 {
		t.Fatal("no server movement", moving)
	}
	// Replacing a controller flushes its current safe RAM position before resume.
	next, w2 := ws180(t, base, c, map[string]any{"resume_visit": w["visit"]})
	if w2["message_type"] != "welcome" || w2["x_q"].(float64) < moving["x_q"].(float64) {
		t.Fatal("resume reset the moving character", w2)
	}
	conn.CloseNow()
	command190(t, next, w2, 2, "move", map[string]int{"x_q": -1, "y_q": y})
	bad := read190(t, next, "error")
	if bad["code"] != "target_blocked" {
		t.Fatal(bad)
	}
	command190(t, next, w2, 3, "move", map[string]int{"x_q": int(w2["x_q"].(float64)) - 1024, "y_q": y})
	read190(t, next, "path.accepted")
	last := read190(t, next, "state.delta")
	for last["moving"] == true {
		last = read190(t, next, "state.delta")
	}
	operation := proof253(t, next, w2, 4)
	f.req(c, "POST", "/api/planet/return", map[string]any{"operation": operation, "visit": w2["visit"]}, 200)
	wait180(t, f, `SELECT count(*) FROM planet_visits WHERE user_id=$1 AND state='ended'`, 1, uid)
	next.CloseNow()
	if f.integer(`SELECT count(*) FROM planet_visits WHERE user_id=$1 AND state='ended'`, uid) != 1 {
		t.Fatal("leave did not commit visit")
	}
	saved := f.integer(`SELECT x_q FROM planet_characters WHERE user_id=$1`, uid)
	if math.Abs(float64(saved)-last["x_q"].(float64)) > 1024 {
		t.Fatal("leave lost the safe position")
	}
	if err := f.app.Transfer.Reconcile(f.ctx); err != nil {
		t.Fatal(err)
	}
	if f.integer(`SELECT count(*) FROM planet_transfer_controls WHERE reserved_ship=$1`, ship) != 0 {
		t.Fatal("moving visit retained ship")
	}
}

func TestPlanetCheckpoints190(t *testing.T) {
	f, c, uid, surface, site, ship := prepare180(t)
	base, stop := startPlanet150(t, f.db)
	defer stop()
	ticket := ticket180(t, f, c, op180(t), site, ship)
	conn, w := ws180(t, base, c, map[string]any{"ticket": ticket["ticket"]})
	var fence scenestore.Fence
	if err := f.db.QueryRow(f.ctx, `SELECT owner_id::text,fence FROM planet_runtime_leases WHERE id=1`).Scan(&fence.Owner, &fence.Number); err != nil {
		t.Fatal(err)
	}
	saved, err := scenestore.New(f.db).Load(f.ctx, scene.Key{Surface: surface, Sector: 1}, fence)
	if err != nil {
		t.Fatal(err)
	}
	portal := saved.Metadata().Portals[0].Ingress
	start := f.integer(`SELECT x_q FROM planet_characters WHERE user_id=$1`, uid)
	command190(t, conn, w, 2, "move", map[string]int{"x_q": portal.X, "y_q": portal.Y})
	read190(t, conn, "path.accepted")
	deadline := time.Now().Add(7 * time.Second)
	for f.integer(`SELECT position_version FROM planet_characters WHERE user_id=$1`, uid) < 2 && time.Now().Before(deadline) {
		time.Sleep(25 * time.Millisecond)
	}
	if f.integer(`SELECT position_version FROM planet_characters WHERE user_id=$1`, uid) < 2 || f.integer(`SELECT x_q FROM planet_characters WHERE user_id=$1`, uid) == start {
		t.Fatal("periodic checkpoint did not persist movement")
	}
	// A newer connection owns the row. Its predecessor's coordinates cannot win.
	next, w2 := ws180(t, base, c, map[string]any{"resume_visit": w["visit"]})
	conn.CloseNow()
	stamp := visits.Welcome{Visit: w["visit"].(string), Epoch: int64(w["connection_epoch"].(float64)), Generation: f.integer(`SELECT generation FROM planet_visits WHERE user_id=$1`, uid), PositionVersion: f.integer(`SELECT position_version FROM planet_characters WHERE user_id=$1`, uid)}
	service := visits.Service{DB: f.db}
	id := access.Identity{User: uid, Session: account.TokenHash(c.Value)}
	before := f.integer(`SELECT x_q FROM planet_characters WHERE user_id=$1`, uid)
	if _, err = service.Checkpoint(f.ctx, id, stamp, fence, saved, int(before), int(w2["y_q"].(float64)), false); !errors.Is(err, visits.ErrReplaced) {
		t.Fatal("old epoch saved a position", err)
	}
	stamp.Epoch = int64(w2["connection_epoch"].(float64))
	f.exec(`UPDATE planet_runtime_leases SET expires_at=clock_timestamp()-interval '1 second'`)
	if _, err = service.Checkpoint(f.ctx, id, stamp, fence, saved, int(before), int(w2["y_q"].(float64)), false); !errors.Is(err, scenestore.ErrFence) {
		t.Fatal("expired runtime saved position", err)
	}
	if f.integer(`SELECT x_q FROM planet_characters WHERE user_id=$1`, uid) != before {
		t.Fatal("failed checkpoint changed position")
	}
	next.CloseNow()
}
