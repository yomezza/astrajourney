package integration

import (
	"context"
	"fmt"
	"github.com/coder/websocket/wsjson"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestPlanetLiveVisitSurvivesRenewalContention256(t *testing.T) {
	f, c, uid, _, site, ship := prepare180(t)
	base, stop := startPlanet150(t, f.db)
	defer stop()
	t.Setenv("PLANET_INTERNAL_URL", base)
	proxy := httptest.NewServer(f.app.Handler())
	defer proxy.Close()
	base = proxy.URL
	conn, w := ws180(t, base, c, map[string]any{"ticket": ticket180(t, f, c, op180(t), site, ship)["ticket"]})
	defer conn.CloseNow()
	request, _ := http.NewRequest("GET", fmt.Sprintf("%s/planet/api/v1/scene?visit=%s&epoch=%.0f", base, w["visit"], w["connection_epoch"]), nil)
	request.AddCookie(c)
	response, err := http.DefaultClient.Do(request)
	if err != nil {
		t.Fatal(err)
	}
	response.Body.Close()
	if response.StatusCode != 200 {
		t.Fatalf("scene HTTP %d", response.StatusCode)
	}
	fence := f.integer(`SELECT fence FROM planet_runtime_leases WHERE id=1`)
	lockDone := make(chan error, 1)
	go func() {
		tx, e := f.db.Begin(f.ctx)
		if e != nil {
			lockDone <- e
			return
		}
		defer tx.Rollback(context.Background())
		if _, e = tx.Exec(f.ctx, `SELECT id FROM planet_runtime_leases WHERE id=1 FOR UPDATE`); e != nil {
			lockDone <- e
			return
		}
		// LeaseRenew=1s and DBTimeout=200ms; hold across a renewal attempt.
		time.Sleep(1400 * time.Millisecond)
		lockDone <- tx.Rollback(f.ctx)
	}()
	start := time.Now()
	transient := 0
	read, done := context.WithTimeout(f.ctx, 66*time.Second)
	defer done()
	for time.Since(start) < 61*time.Second {
		var frame map[string]any
		if err = wsjson.Read(read, conn, &frame); err != nil {
			t.Fatalf("live scene disconnected after %s: %v", time.Since(start), err)
		}
		if frame["type"] == "error" {
			code := frame["payload"].(map[string]any)["code"]
			if code != "runtime_unavailable" || time.Since(start) > 4*time.Second {
				t.Fatalf("persistent server failure: %v", frame)
			}
			transient++
			if transient > 2 {
				t.Fatal("repeating validation failure", frame)
			}
		}
	}
	if err = <-lockDone; err != nil {
		t.Fatal(err)
	}
	if got := f.integer(`SELECT fence FROM planet_runtime_leases WHERE id=1`); got != fence {
		t.Fatalf("runtime fenced itself: %d -> %d", fence, got)
	}
	next, w2 := ws180(t, base, c, map[string]any{"resume_visit": w["visit"]})
	defer next.CloseNow()
	conn.CloseNow() // Delayed old-controller cleanup must not affect the successor.
	if w2["visit"] != w["visit"] || w2["connection_epoch"].(float64) <= w["connection_epoch"].(float64) {
		t.Fatal("resume lost identity/epoch", w2)
	}
	read190(t, next, "sector.snapshot")
	if f.integer(`SELECT count(*) FROM planet_visits WHERE user_id=$1 AND state='active' AND connection_epoch=$2`, uid, int64(w2["connection_epoch"].(float64))) != 1 {
		t.Fatal("old cleanup damaged resumed visit")
	}
	operation := proof253(t, next, w2, 2)
	f.req(c, "POST", "/api/planet/return", map[string]any{"operation": operation, "visit": w2["visit"]}, 200)
	wait180(t, f, `SELECT count(*) FROM planet_visits WHERE user_id=$1 AND state='ended'`, 1, uid)
	// The fixture has no Cosmic reconciliation worker; run its normal handler.
	if err = f.app.Transfer.Reconcile(f.ctx); err != nil {
		t.Fatal(err)
	}
	wait180(t, f, `SELECT count(*) FROM planet_transfer_controls WHERE reserved_ship=$1`, 0, ship)
}
