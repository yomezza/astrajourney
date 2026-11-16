package integration

import (
	"context"
	"encoding/json"
	"errors"
	"sync"
	"testing"

	"github.com/jackc/pgx/v5"
	"github.com/yomezza/cosmosimulator/internal/planet/access"
	"github.com/yomezza/cosmosimulator/internal/planet/building"
	"github.com/yomezza/cosmosimulator/internal/planet/pathing"
	"github.com/yomezza/cosmosimulator/internal/planet/scene"
	"github.com/yomezza/cosmosimulator/internal/planet/scenestore"
)

func TestBuildingPersistenceOwnershipAndConcurrentPlacement259(t *testing.T) {
	f, _, uid, surface, _, _ := prepare180(t)
	_, peer, _ := peer200(t, f, uid, surface)
	_, stop := startPlanet150(t, f.db)
	defer stop()
	var fence scenestore.Fence
	if e := f.db.QueryRow(f.ctx, `SELECT owner_id,fence FROM planet_runtime_leases WHERE id=1`).Scan(&fence.Owner, &fence.Number); e != nil {
		t.Fatal(e)
	}
	base, e := scenestore.New(f.db).Load(f.ctx, scene.Key{Surface: surface, Sector: 1}, fence)
	if e != nil {
		t.Fatal(e)
	}
	can, e := building.Allowed(f.ctx, f.db, peer, base.Key())
	if e != nil || can {
		t.Fatal("visitor can build", e)
	}
	// Permit the second player to build, so removal ownership is checked separately.
	f.exec(`UPDATE company_roles SET permissions=permissions||'"colonize"'::jsonb WHERE id=(SELECT company_role FROM users WHERE id=$1)`, peer)
	var object building.Object
	found := false
	for y := 1; y < 63 && !found; y++ {
		for x := 1; x < 63; x++ {
			o := building.Object{Kind: "fence", X: x, Y: y, Rotation: 3}
			if building.Validate(base, nil, o, nil, building.Position(o)) == nil {
				object = o
				found = true
				break
			}
		}
	}
	if !found {
		t.Fatal("no build space")
	}
	object.ID, _ = access.UUID()
	apply := func(uid int64, c building.Command) (building.Snapshot, error) {
		tx, err := f.db.Begin(f.ctx)
		if err != nil {
			return building.Snapshot{}, err
		}
		defer tx.Rollback(context.Background())
		s, err := building.Apply(f.ctx, tx, uid, base, c, nil, building.Position(object))
		if err == nil {
			err = tx.Commit(f.ctx)
		}
		return s, err
	}
	// Two concurrent placements of different UUIDs in the same layer/cell serialize.
	other := object
	other.ID, _ = access.UUID()
	var wg sync.WaitGroup
	out := make(chan error, 2)
	for _, o := range []building.Object{object, other} {
		wg.Add(1)
		go func(o building.Object) {
			defer wg.Done()
			_, err := apply(uid, building.Command{Action: "place", Object: o})
			out <- err
		}(o)
	}
	wg.Wait()
	close(out)
	success := 0
	for err := range out {
		if err == nil {
			success++
		}
	}
	if success != 1 {
		t.Fatalf("concurrent placements succeeded %d times", success)
	}
	s, e := building.Load(f.ctx, f.db, base.Key())
	if e != nil || len(s.Objects) != 1 {
		t.Fatal("not persisted", e)
	}
	object = s.Objects[0]
	if object.Rotation != 3 {
		t.Fatal("lost rotation")
	}
	repeated, e := apply(uid, building.Command{Action: "place", Object: object})
	if e != nil || repeated.Version != s.Version || len(repeated.Objects) != 1 {
		t.Fatal("idempotency", e)
	}
	if _, e = apply(peer, building.Command{Action: "remove", Object: object}); !errors.Is(e, building.ErrDenied) {
		t.Fatal("removed someone else's object", e)
	}
	raw, _ := json.Marshal(s.Public(peer))
	var public map[string]any
	_ = json.Unmarshal(raw, &public)
	if public["objects"].([]any)[0].(map[string]any)["owner"] != nil {
		t.Fatal("owner ID leaked")
	}
	grid := building.NewGrid(base, s.Objects)
	p := building.Position(object)
	if pathing.Clear(grid, p) {
		t.Fatal("persisted wall doesn't block")
	}
	if s, e = apply(uid, building.Command{Action: "remove", Object: object}); e != nil || len(s.Objects) != 0 {
		t.Fatal("owner remove", e)
	}
	// Saved positions of disconnected characters also protect their return spot.
	pSaved := building.Position(object)
	qx, qy := pSaved.Quantized()
	f.exec(`INSERT INTO planet_characters(user_id,last_surface,last_sector,last_revision,x_q,y_q) VALUES($1,$2,1,0,$3,$4) ON CONFLICT(user_id) DO UPDATE SET last_surface=EXCLUDED.last_surface,last_sector=1,last_revision=0,x_q=EXCLUDED.x_q,y_q=EXCLUDED.y_q`, peer, surface, qx, qy)
	if _, e = apply(uid, building.Command{Action: "place", Object: object}); e == nil || e.Error() != "build_player_here" {
		t.Fatal("covered disconnected character", e)
	}
	// Read via a fresh transaction, as after restart. No in-memory registry is used.
	tx, e := f.db.BeginTx(f.ctx, pgx.TxOptions{AccessMode: pgx.ReadOnly})
	if e != nil {
		t.Fatal(e)
	}
	defer tx.Rollback(f.ctx)
	s, e = building.Load(f.ctx, tx, base.Key())
	if e != nil || len(s.Objects) != 0 || s.Version != 2 {
		t.Fatal("fresh load", s, e)
	}
}

func TestNaturalClearingPersistence2510(t *testing.T) {
	f, _, uid, surface, _, _ := prepare180(t)
	_, stop := startPlanet150(t, f.db)
	defer stop()
	var fence scenestore.Fence
	if err := f.db.QueryRow(f.ctx, `SELECT owner_id,fence FROM planet_runtime_leases WHERE id=1`).Scan(&fence.Owner, &fence.Number); err != nil {
		t.Fatal(err)
	}
	base, e := scenestore.New(f.db).Load(f.ctx, scene.Key{Surface: surface, Sector: 1}, fence)
	if e != nil {
		t.Fatal(e)
	}
	var target scene.Object
	for _, o := range base.Metadata().Objects {
		if o.RX > 0 {
			target = o
			break
		}
	}
	if target.ID == "" {
		t.Fatal("no decor")
	}
	apply := func(id string) (building.Snapshot, error) {
		tx, e := f.db.Begin(f.ctx)
		if e != nil {
			return building.Snapshot{}, e
		}
		defer tx.Rollback(f.ctx)
		s, e := building.Apply(f.ctx, tx, uid, base, building.Command{Action: "clear", Object: building.Object{ID: id}}, nil, pathing.FromQ(target.Position.X, target.Position.Y))
		if e == nil {
			e = tx.Commit(f.ctx)
		}
		return s, e
	}
	s, e := apply(target.ID)
	if e != nil || len(s.Removed) != 1 {
		t.Fatal("clear failed", s, e)
	}
	again, e := apply(target.ID)
	if e != nil || again.Version != s.Version {
		t.Fatal("duplicate clear", e)
	}
	if _, e = apply("landing"); e == nil {
		t.Fatal("deleted landing")
	}
	if _, e = apply("arbitrary"); e == nil {
		t.Fatal("unknown ID")
	}
	saved, e := building.Load(f.ctx, f.db, base.Key())
	if e != nil || len(saved.Removed) != 1 || saved.Removed[0] != target.ID {
		t.Fatal("not persisted", e)
	}
	p := pathing.FromQ(target.Position.X, target.Position.Y)
	if pathing.Clear(base, p) || !pathing.Clear(base.WithoutObjects(saved.Removed), p) {
		t.Fatal("clearing collision incorrect")
	}
}
