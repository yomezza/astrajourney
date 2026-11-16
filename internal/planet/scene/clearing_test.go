package scene

import (
	"context"
	"github.com/yomezza/cosmosimulator/internal/geography"
	"strings"
	"testing"
)

func TestClearingKeepsSavedGeographyAndOtherObstacles(t *testing.T) {
	g := &geography.PlanetSurface{Width: 4, Height: 4, GridW: 2, GridH: 2, SeaLevel: 460, Heights: []int{700, 700, 700, 700}, Water: make([]int, 4), Sectors: []geography.PlanetSector{{Number: 1, Center: geography.SurfacePoint{X: 2, Y: 2}}}}
	s, e := Generate(context.Background(), Key{Surface: 1, Sector: 1}, g, strings.Repeat("a", 64))
	if e != nil {
		t.Fatal(e)
	}
	for _, o := range s.Metadata().Objects {
		if o.RX == 0 {
			continue
		}
		x, y := o.Position.X/1024, o.Position.Y/1024
		if s.Walkable(x, y) {
			t.Fatal("solid not blocked")
		}
		cleared := s.WithoutObjects([]string{o.ID})
		if !cleared.Walkable(x, y) {
			t.Fatal("removed footprint remains")
		}
		if s.Walkable(x, y) {
			t.Fatal("mutated original")
		}
		for yy := 0; yy < 256; yy++ {
			for xx := 0; xx < 256; xx++ {
				if cleared.Height(xx, yy) != s.Height(xx, yy) || cleared.Water(xx, yy) != s.Water(xx, yy) || cleared.Reserved(xx, yy) != s.Reserved(xx, yy) {
					t.Fatal("geography changed")
				}
				if cleared.Walkable(xx, yy) && !s.TerrainWalkable(xx, yy) {
					t.Fatal("opened non-terrain")
				}
			}
		}
		for _, other := range cleared.Metadata().Objects {
			if other.RX > 0 && cleared.Walkable(other.Position.X/1024, other.Position.Y/1024) {
				t.Fatal("another obstacle opened")
			}
		}
		if len(cleared.Metadata().Objects) != len(s.Metadata().Objects)-1 {
			t.Fatal("wrong removal")
		}
		return
	}
	t.Fatal("no solid fixture")
}
