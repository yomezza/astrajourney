package planetserver

import (
	"context"
	"errors"
	"testing"
	"time"
)

func TestVisitCheckGraceRetriesDatabaseTimeoutOnly(t *testing.T) {
	now := time.Date(2026, time.November, 13, 12, 0, 0, 0, time.UTC)
	var g visitCheckGrace
	retry, first := g.failed(now, context.DeadlineExceeded)
	if !retry || !first {
		t.Fatal("first DB timeout should start a grace period")
	}
	retry, first = g.failed(now.Add(5*time.Second), context.DeadlineExceeded)
	if !retry || first {
		t.Fatal("repeated timeout must retry without reinitializing grace")
	}
	retry, first = g.failed(now.Add(planetVisitCheckGrace), context.DeadlineExceeded)
	if retry || first {
		t.Fatal("persistent DB outage must not continue forever")
	}
	if !g.recovered() || g.recovered() {
		t.Fatal("restored validation should clear a single paused state")
	}
	retry, first = g.failed(now.Add(2*planetVisitCheckGrace), context.DeadlineExceeded)
	if !retry || !first {
		t.Fatal("a fresh outage should receive a fresh bounded grace period")
	}
}

func TestVisitCheckGraceNeverRetriesAuthorizationFailure(t *testing.T) {
	var g visitCheckGrace
	for _, err := range []error{errors.New("session_replaced"), errors.New("access_denied"), context.Canceled, errors.New("visit_unavailable")} {
		if retry, first := g.failed(time.Now(), err); retry || first || !g.since.IsZero() {
			t.Fatalf("authorization failure must remain fatal: %v", err)
		}
	}
}
