package scene

import (
	"github.com/yomezza/cosmosimulator/internal/planet/terrain"
	"math"
)

// TerrainWalkable reads the saved pre-decoration component. Clearing a tree
// never makes water, a cliff, or a different island traversable.
func (s *Scene) TerrainWalkable(x, y int) bool {
	f := s.cell(x, y)
	return f&core != 0 && f&inside != 0 && f&water == 0
}
func objectCells(o Object, visit func(int, int)) {
	if o.RX <= 0 {
		return
	}
	x, y := o.Position.X/1024, o.Position.Y/1024
	rx, ry := float64(o.RX)/1024+terrain.Radius+math.Sqrt2/2, float64(o.RY)/1024+terrain.Radius+math.Sqrt2/2
	for dy := -int(math.Ceil(ry)); dy <= int(math.Ceil(ry)); dy++ {
		for dx := -int(math.Ceil(rx)); dx <= int(math.Ceil(rx)); dx++ {
			nx, ny := x+dx, y+dy
			if nx >= 0 && ny >= 0 && nx < terrain.Size && ny < terrain.Size {
				visit(nx, ny)
			}
		}
	}
}

// WithoutObjects derives a private navigation view from the saved snapshot.
// Geography, seeds, heights, IDs and the original scene are never regenerated or
// overwritten. Only a removed object's inflated collision footprint is restored.
func (s *Scene) WithoutObjects(removed []string) *Scene {
	if len(removed) == 0 {
		return s
	}
	ids := make(map[string]bool, len(removed))
	for _, id := range removed {
		ids[id] = true
	}
	result := &Scene{meta: s.Metadata(), flags: append([]byte(nil), s.flags...), heights: s.heights}
	result.meta.Objects = nil
	for _, o := range s.meta.Objects {
		if !ids[o.ID] {
			result.meta.Objects = append(result.meta.Objects, o)
			continue
		}
		objectCells(o, func(x, y int) {
			if s.TerrainWalkable(x, y) {
				result.flags[y*terrain.Size+x] |= walkable
			}
		})
	}
	// Retain every remaining obstacle, including overlapping footprints in an
	// older saved scene. Restoration cannot punch holes through another object.
	for _, o := range result.meta.Objects {
		objectCells(o, func(x, y int) { result.flags[y*terrain.Size+x] &= ^walkable })
	}
	return result
}
