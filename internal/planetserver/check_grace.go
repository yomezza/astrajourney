package planetserver

import (
	"context"
	"errors"
	"time"
)

// A saturated PostgreSQL connection pool or a briefly blocked world lock
// must not eject an authenticated pilot after a single 2-second DB timeout.
// While temporarily unverified the pilot is frozen; after recovery it resumes.
// Actual revocation (session_replaced/access_denied/etc.) is NEVER retried.
const planetVisitCheckGrace = 12 * time.Second

type visitCheckGrace struct {
	since time.Time
}

func (g *visitCheckGrace) failed(now time.Time, err error) (retry, first bool) {
	if !errors.Is(err, context.DeadlineExceeded) {
		return false, false
	}
	if g.since.IsZero() {
		g.since = now
		first = true
	}
	return now.Sub(g.since) < planetVisitCheckGrace, first
}

func (g *visitCheckGrace) recovered() bool {
	if g.since.IsZero() {
		return false
	}
	g.since = time.Time{}
	return true
}
