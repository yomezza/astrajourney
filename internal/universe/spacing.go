package universe

import "math"

// The placement algorithm stops at a distance of 32. Outside that radius the
// exact nearest distance does not affect placement, so nine cells are sufficient.
type spacingCell struct{ x, y int }
type spacingPoint struct{ x, y float64 }
type spacingIndex map[spacingCell][]spacingPoint

func newSpacingIndex(stars []Star) spacingIndex {
	index := spacingIndex{}
	for _, s := range stars {
		index.add(s.X, s.Y)
	}
	return index
}
func (s spacingIndex) add(x, y float64) {
	cell := spacingCell{int(math.Floor(x / 32)), int(math.Floor(y / 32))}
	s[cell] = append(s[cell], spacingPoint{x, y})
}
func (s spacingIndex) nearest(x, y float64) float64 {
	cell := spacingCell{int(math.Floor(x / 32)), int(math.Floor(y / 32))}
	best := 32.0
	for dx := -1; dx <= 1; dx++ {
		for dy := -1; dy <= 1; dy++ {
			for _, p := range s[spacingCell{cell.x + dx, cell.y + dy}] {
				best = math.Min(best, math.Hypot(x-p.x, y-p.y))
			}
		}
	}
	return best
}
