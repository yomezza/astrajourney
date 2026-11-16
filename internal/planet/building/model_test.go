package building

import (
	"context"
	"errors"
	"github.com/yomezza/cosmosimulator/internal/geography"
	"github.com/yomezza/cosmosimulator/internal/planet/pathing"
	"github.com/yomezza/cosmosimulator/internal/planet/scene"
	"strings"
	"testing"
)

func TestPlacementAndDynamicGrid(t *testing.T) {
	g := &geography.PlanetSurface{Width: 4, Height: 4, GridW: 2, GridH: 2, SeaLevel: 460, Heights: []int{700, 700, 700, 700}, Water: make([]int, 4), Sectors: []geography.PlanetSector{{Number: 1, Center: geography.SurfacePoint{X: 2, Y: 2}}}}
	s, err := scene.Generate(context.Background(), scene.Key{Surface: 1, Sector: 1}, g, strings.Repeat("a", 64))
	if err != nil {
		t.Fatal(err)
	}
	var o Object
	found := false
	for y := 1; y < 63 && !found; y++ {
		for x := 1; x < 63; x++ {
			candidate := Object{Kind: "fence", X: x, Y: y, Rotation: 1}
			if Validate(s, nil, candidate, nil, Position(candidate)) == nil {
				o = candidate
				found = true
				break
			}
		}
	}
	if !found {
		t.Fatal("no buildable spot")
	}
	center := Position(o)
	if !pathing.Clear(s, center) {
		t.Fatal("base unexpectedly blocked")
	}
	grid := NewGrid(s, []Object{o})
	if pathing.Clear(grid, center) {
		t.Fatal("solid didn't block")
	}
	if !pathing.Clear(s, center) {
		t.Fatal("base geography mutated")
	}
	floor := o
	floor.Kind = "foundation"
	if !pathing.Clear(NewGrid(s, []Object{floor}), center) {
		t.Fatal("floor blocks")
	}
	if err = Validate(s, []Object{floor}, o, nil, center); err != nil {
		t.Fatal("floor/object layering", err)
	}
	if err = Validate(s, []Object{o}, o, nil, center); err == nil {
		t.Fatal("overlap accepted")
	}
	if err = Validate(s, nil, o, []pathing.Point{center}, center); err == nil {
		t.Fatal("player covered")
	}
	bad := o
	bad.Rotation = 4
	if !errors.Is(Validate(s, nil, bad, nil, center), ErrPlacement) {
		t.Fatal("invalid rotation accepted")
	}
	bad = o
	bad.Kind = "../../secret"
	if !errors.Is(Validate(s, nil, bad, nil, center), ErrPlacement) {
		t.Fatal("arbitrary asset accepted")
	}
	if err = Validate(s, nil, o, nil, pathing.Point{}); err == nil {
		t.Fatal("remote build accepted")
	}
	landing := s.Metadata().Landing
	bad = o
	bad.X = landing.SpawnX / 1024 / 4
	bad.Y = landing.SpawnY / 1024 / 4
	if err = Validate(s, nil, bad, nil, Position(bad)); err == nil {
		t.Fatal("spawn blocked")
	}
	p := Snapshot{Version: 1, Objects: []Object{{Owner: 7}, {Owner: 8}}}.Public(7)
	if p.Objects[0].Owner != 0 || !p.Objects[0].Mine || p.Objects[1].Mine {
		t.Fatal("owner privacy")
	}
}
