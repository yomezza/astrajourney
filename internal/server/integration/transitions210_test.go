package integration

import (
	"context"
	"encoding/json"
	"fmt"
	"github.com/yomezza/cosmosimulator/internal/planetserver"
	"io"
	"log/slog"
	"net"
	"net/http"
	"testing"

	"github.com/coder/websocket"
	"github.com/yomezza/cosmosimulator/internal/planet/scene"
	"github.com/yomezza/cosmosimulator/internal/planet/scenestore"
)

func edge210(t *testing.T, f *fixture033, c *http.Cookie, uid, surface, site, ship int64, base string) (*websocket.Conn, map[string]any, *scene.Scene, *scene.Scene) {
	t.Helper()
	conn, w := ws180(t, base, c, map[string]any{"ticket": ticket180(t, f, c, op180(t), site, ship)["ticket"]})
	if w["message_type"] != "welcome" {
		t.Fatal(w)
	}
	conn.CloseNow()
	wait180(t, f, `SELECT count(*) FROM planet_visits WHERE user_id=$1 AND state='disconnected'`, 1, uid)
	var fence scenestore.Fence
	if err := f.db.QueryRow(f.ctx, `SELECT owner_id::text,fence FROM planet_runtime_leases WHERE id=1`).Scan(&fence.Owner, &fence.Number); err != nil {
		t.Fatal(err)
	}
	source, err := scenestore.New(f.db).Load(f.ctx, scene.Key{Surface: surface, Sector: 1}, fence)
	if err != nil {
		t.Fatal(err)
	}
	dest, err := scenestore.New(f.db).Load(f.ctx, scene.Key{Surface: surface, Sector: 2}, fence)
	if err != nil {
		t.Fatal(err)
	}
	var x, y int
	for _, p := range source.Portals() {
		if p.Neighbor == 2 {
			x = p.Ingress.X
			y = p.Ingress.Y
		}
	}
	if x == 0 {
		t.Fatal("fixture has no land portal")
	}
	// This is a fixture-only shortcut to the reserved edge. Production exposes no
	// coordinate setter; pathing and physical walking are already covered in P2-5.
	f.exec(`UPDATE planet_characters SET x_q=$2,y_q=$3,position_version=position_version+1 WHERE user_id=$1`, uid, x, y)
	next, w := ws180(t, base, c, map[string]any{"resume_visit": w["visit"]})
	return next, w, source, dest
}
func readyOffer210(t *testing.T, c *websocket.Conn) map[string]any {
	t.Helper()
	for {
		p := read190(t, c, "transition.offer")
		if p["ready"] == true {
			return p
		}
	}
}
func TestPlanetTransitions210(t *testing.T) {
	f, c, uid, surface, site, ship := prepare180(t)
	addColony160(f, uid, surface, 2, nil, false)
	reconcile160(f)
	if f.integer(`SELECT count(*) FROM planet_landing_sites WHERE surface=$1`, surface) != 1 {
		t.Fatal("neighbor incorrectly got a second module")
	}
	base, stop := startPlanet150(t, f.db)
	defer stop()
	conn, w, _, dest := edge210(t, f, c, uid, surface, site, ship, base)
	offer := readyOffer210(t, conn)
	if offer["neighbor"] != float64(2) || f.integer(`SELECT current_sector FROM planet_visits WHERE user_id=$1`, uid) != 1 {
		t.Fatal("crossed without confirmation", offer)
	}
	command190(t, conn, w, 2, "transition.cancel", map[string]any{"offer": offer["offer"]})
	read190(t, conn, "transition.cancelled")
	for range 12 {
		state := read190(t, conn, "state.delta")
		if state["sector"] != float64(1) {
			t.Fatal("cancel moved location")
		}
	}
	command190(t, conn, w, 3, "move", map[string]int{"x_q": int(w["x_q"].(float64)), "y_q": int(w["y_q"].(float64))})
	offer = readyOffer210(t, conn)
	operation := op180(t)
	command190(t, conn, w, 4, "transition.confirm", map[string]any{"offer": offer["offer"], "operation": operation})
	complete := read190(t, conn, "transition.complete")
	next := complete["welcome"].(map[string]any)
	if next["sector"] != float64(2) || next["connection_epoch"].(float64) <= w["connection_epoch"].(float64) {
		t.Fatal(complete)
	}
	p := dest.Portals()[0]
	if next["x_q"] != float64(p.Ingress.X) || next["y_q"] != float64(p.Ingress.Y) {
		t.Fatal("wrong side ingress", next, p)
	}
	if f.integer(`SELECT current_sector FROM planet_visits WHERE user_id=$1`, uid) != 2 || f.integer(`SELECT last_sector FROM planet_characters WHERE user_id=$1`, uid) != 2 {
		t.Fatal("atomic current location failed")
	}
	command190(t, conn, w, 5, "transition.confirm", map[string]any{"offer": offer["offer"], "operation": operation})
	again := read190(t, conn, "transition.complete")
	if again["operation"] != operation || f.integer(`SELECT count(*) FROM planet_transition_receipts WHERE user_id=$1`, uid) != 1 {
		t.Fatal("repeated transition mutated state")
	}
	// Resume in a neighboring sector WITHOUT a landing module.
	replacement, resumed := ws180(t, base, c, map[string]any{"resume_visit": w["visit"]})
	conn.CloseNow()
	if resumed["sector"] != float64(2) || resumed["actor"] != w["actor"] || resumed["x_q"] != next["x_q"] {
		t.Fatal("resume lost crossed location", resumed)
	}
	request, _ := http.NewRequest(http.MethodGet, base+fmt.Sprintf("/planet/api/v1/scene?visit=%s&epoch=%.0f", resumed["visit"], resumed["connection_epoch"]), nil)
	request.AddCookie(c)
	// Read through a real HTTP endpoint to verify active epoch + no phantom module.
	request.URL.Scheme = "http"
	request.URL.Host = base[len("http://"):]
	response, err := http.DefaultClient.Do(request)
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	var public map[string]any
	if err = json.NewDecoder(response.Body).Decode(&public); err != nil {
		t.Fatal(err)
	}
	if response.StatusCode != 200 || public["landing"] != nil {
		t.Fatal("neighbor requires or displays a nonexistent module", public)
	}
	command190(t, replacement, resumed, 2, "leave", map[string]string{"operation": op180(t)})
	denied := read190(t, replacement, "error")
	if denied["code"] != "return_authorization_required" {
		t.Fatal("neighbor return bypass", denied)
	}
	f.req(c, "POST", "/api/planet/return", map[string]any{"operation": op180(t), "visit": resumed["visit"]}, 403)
	// Fixture-only administrative revocation after verifying the player cannot
	// return from a neighboring sector without the original landing module.
	f.exec(`UPDATE planet_transfer_controls SET generation=generation+1,revoke_requested_at=clock_timestamp() WHERE user_id=$1`, uid)
	wait180(t, f, `SELECT count(*) FROM planet_visits WHERE user_id=$1 AND state='ended'`, 1, uid)
	if err = f.app.Transfer.Reconcile(f.ctx); err != nil {
		t.Fatal(err)
	}
	if f.integer(`SELECT count(*) FROM planet_transfer_controls WHERE reserved_ship=$1`, ship) != 0 {
		t.Fatal("return after crossing kept ship reserved")
	}
}
func TestPlanetTransitionRevocation210(t *testing.T) {
	f, c, uid, surface, site, ship := prepare180(t)
	addColony160(f, uid, surface, 2, nil, false)
	reconcile160(f)
	base, stop := startPlanet150(t, f.db)
	defer stop()
	conn, w, _, _ := edge210(t, f, c, uid, surface, site, ship, base)
	offer := readyOffer210(t, conn)
	_, stranger := f.signup("edge_stranger210")
	f.exec(`UPDATE planet_colonies SET owner_user=$2 WHERE surface=$1 AND sector=2`, surface, stranger)
	command190(t, conn, w, 2, "transition.confirm", map[string]any{"offer": offer["offer"], "operation": op180(t)})
	failed := read190(t, conn, "transition.cancelled")
	if failed["reason"] != "access_denied" || f.integer(`SELECT current_sector FROM planet_visits WHERE user_id=$1`, uid) != 1 || f.integer(`SELECT count(*) FROM planet_transition_receipts`) != 0 {
		t.Fatal("revocation crossed into another owner", failed)
	}
	// The original actor still accepts movement after a rejected commit.
	command190(t, conn, w, 3, "move", map[string]int{"x_q": int(w["x_q"].(float64)), "y_q": int(w["y_q"].(float64))})
	read190(t, conn, "path.accepted")
}

func TestPlanetTransitionCapacity210(t *testing.T) {
	f, c, uid, surface, site, ship := prepare180(t)
	addColony160(f, uid, surface, 2, nil, false)
	reconcile160(f)
	cfg := planetserver.DefaultConfig()
	cfg.SceneCapacity = 1
	app, err := planetserver.New(f.db, cfg, slog.New(slog.NewTextHandler(io.Discard, nil)))
	if err != nil {
		t.Fatal(err)
	}
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(f.ctx)
	done := make(chan error, 1)
	started := make(chan struct{})
	go func() { done <- app.Serve(ctx, listener, func() { close(started) }) }()
	defer func() {
		cancel()
		if err := <-done; err != nil {
			t.Error(err)
		}
	}()
	<-started
	base := "http://" + listener.Addr().String()
	conn, w, _, _ := edge210(t, f, c, uid, surface, site, ship, base)
	failed := read190(t, conn, "transition.cancelled")
	if failed["reason"] != "transition_load_failed" || f.integer(`SELECT current_sector FROM planet_visits WHERE user_id=$1`, uid) != 1 {
		t.Fatal("failed preload lost original sector", failed)
	}
	command190(t, conn, w, 2, "move", map[string]int{"x_q": int(w["x_q"].(float64)), "y_q": int(w["y_q"].(float64))})
	read190(t, conn, "path.accepted")
}
