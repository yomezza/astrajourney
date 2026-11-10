package planetserver

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/yomezza/cosmosimulator/internal/planet/access"
	"github.com/yomezza/cosmosimulator/internal/planet/movement"
	planetruntime "github.com/yomezza/cosmosimulator/internal/planet/runtime"
	"github.com/yomezza/cosmosimulator/internal/planet/scene"
	"github.com/yomezza/cosmosimulator/internal/planet/scenestore"
	"github.com/yomezza/cosmosimulator/internal/planet/visits"
	"github.com/yomezza/cosmosimulator/internal/platform/natscore"
	"github.com/yomezza/cosmosimulator/internal/platform/postgres"
)

// This probe exists ONLY in the Go test binary. No debug route or flag is added
// to the shipped server. Parent integration tests provide an owned *_test schema.
func TestN5PostcommitJoinFailureProbe(t *testing.T) {
	path := os.Getenv("N5_JOIN_PROBE")
	if path == "" {
		t.Skip("owned integration probe")
	}
	var in struct {
		DSN, Schema, Operation string
		User                   int64
		Neighbor               int
		NATS                   natscore.Config
	}
	raw, e := os.ReadFile(path)
	if e != nil {
		t.Fatal("probe input")
	}
	if json.Unmarshal(raw, &in) != nil || !strings.HasPrefix(in.Schema, "update033_") {
		t.Fatal("owned fixture required")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	db, _, e := postgres.Open(ctx, in.DSN, in.Schema)
	if e != nil {
		t.Fatal("probe database")
	}
	defer db.Close()
	if !strings.HasSuffix(db.Config().ConnConfig.Database, "_test") {
		t.Fatal("disposable database required")
	}
	publisher, e := newPlanetPublisher(in.NATS)
	if e != nil {
		t.Fatal(e)
	}
	publisher.start(ctx)
	defer func() {
		stop, done := context.WithTimeout(context.Background(), time.Second)
		defer done()
		_ = publisher.close(stop)
	}()
	for !publisher.healthy.Load() {
		select {
		case <-ctx.Done():
			t.Fatal("probe broker unavailable")
		default:
			time.Sleep(10 * time.Millisecond)
		}
	}
	var id access.Identity
	var w visits.Welcome
	var f scenestore.Fence
	var key scene.Key
	if e = db.QueryRow(ctx, `SELECT session_hash,visit_id::text,connection_epoch,generation,current_surface,current_sector,revision FROM planet_visits WHERE user_id=$1 AND state='active'`, in.User).Scan(&id.Session, &w.Visit, &w.Epoch, &w.Generation, &key.Surface, &key.Sector, &key.Revision); e != nil {
		t.Fatal(e)
	}
	id, e = access.Session(ctx, db, id.Session)
	if e != nil {
		t.Fatal(e)
	}
	if e = db.QueryRow(ctx, `SELECT owner_id::text,fence FROM planet_runtime_leases WHERE id=1`).Scan(&f.Owner, &f.Number); e != nil {
		t.Fatal(e)
	}
	store := scenestore.New(db)
	registry, e := planetruntime.NewRegistry(ctx, 2, time.Minute, 3*time.Second, func(c context.Context, k scene.Key) (*scene.Scene, error) { return store.Load(c, k, f) }, func(c context.Context, s *scene.Scene) error { return store.Validate(c, s, f) })
	if e != nil {
		t.Fatal(e)
	}
	defer registry.Close(context.Background())
	from, e := registry.Acquire(ctx, key)
	if e != nil {
		t.Fatal(e)
	}
	defer from.Release()
	toKey := key
	toKey.Sector = in.Neighbor
	to, e := registry.Acquire(ctx, toKey)
	if e != nil {
		t.Fatal(e)
	}
	defer to.Release()
	for attempt := 0; attempt < 3; attempt++ {
		if e = db.QueryRow(ctx, `SELECT x_q,y_q,position_version FROM planet_characters WHERE user_id=$1`, id.User).Scan(&w.X, &w.Y, &w.PositionVersion); e != nil {
			t.Fatal(e)
		}
		hub := movement.New(ctx, 4, 24000)
		member, e := hub.Join(movement.Stamp{User: id.User, Actor: in.Operation, Visit: w.Visit, Epoch: w.Epoch, Generation: w.Generation, Version: w.PositionVersion}, from.Scene(), w.X, w.Y)
		if e != nil {
			t.Fatal(e)
		}
		hub.Close() // Deliberate RAM-only failure; SQL/presence and original service remain valid.
		offerID, _ := access.UUID()
		a := &App{visits: &visits.Service{DB: db}, motion: hub, publisher: publisher}
		_, next, err := a.commitCrossing(ctx, id, w, f, member, &crossingOffer{Ready: true, pin: to, ID: offerID, Expires: time.Now().Add(time.Second)}, in.Operation, make(chan struct{}, 1))
		if errors.Is(err, visits.ErrReplaced) {
			continue
		}
		if !errors.Is(err, movement.ErrSuspended) || next.Sector != in.Neighbor || next.Epoch <= w.Epoch {
			t.Fatal("expected committed transition then failed RAM join", err)
		}
		for publisher.published.Load() == 0 {
			select {
			case <-ctx.Done():
				t.Fatal("confirmed transition not published after RAM failure")
			default:
				time.Sleep(10 * time.Millisecond)
			}
		}
		return
	}
	t.Fatal("checkpoint interference prevented probe")
}
