package universe

import (
	"fmt"
	"math"
	"sort"
)

// A colony controls only a small neighborhood around an actual founder port.
// Resource regions and spiral arms do not imply territorial ownership.
type FounderTerritory struct {
	Anchor  int     `json:"anchor"`
	Founder string  `json:"founder"`
	Radius  float64 `json:"radius"`
}

func founderStationChance(d GalaxyDefinition, s Star) int {
	if s.Region < 0 {
		return 0
	}
	// Density smoothly falls to zero before the outer third of the galaxy.
	falloff := math.Max(0, 1-math.Hypot(s.X, s.Y)/(d.Radius*.65))
	return int(math.Round(float64(d.Settings.StationPercent) * falloff * falloff))
}

func assignFounderTerritories(g *Galaxy, id string) {
	d := g.Definition(id)
	d.Territories = nil
	if d.Empty {
		for i := range g.Stars {
			if GalaxyKey(g.Stars[i]) == id {
				g.Stars[i].Founder = ""
			}
		}
		return
	}
	candidates := []Star{}
	for _, s := range g.Stars {
		if GalaxyKey(s) == id && s.Region >= 0 && s.Ports != nil && *s.Ports > 0 && math.Hypot(s.X, s.Y) <= d.Radius*.65 {
			candidates = append(candidates, s)
		}
	}
	sort.Slice(candidates, func(i, j int) bool {
		a, b := candidates[i], candidates[j]
		da, db := math.Hypot(a.X, a.Y), math.Hypot(b.X, b.Y)
		if da == db {
			return a.ID < b.ID
		}
		return da < db
	})
	radius := math.Max(35, math.Min(140, d.Radius*.035))
	for _, s := range candidates {
		covered := false
		for _, area := range d.Territories {
			center := g.Stars[area.Anchor-1]
			if math.Hypot(s.X-center.X, s.Y-center.Y) <= area.Radius {
				covered = true
				break
			}
		}
		if !covered {
			d.Territories = append(d.Territories, FounderTerritory{s.ID, fmt.Sprintf("founder-%02d", 1+stablePercent(d.Seed, s.ID, 53)%2), radius})
		}
	}
	for i := range g.Stars {
		s := &g.Stars[i]
		if GalaxyKey(*s) != id {
			continue
		}
		s.Founder = ""
		if s.Region < 0 {
			continue
		}
		best := math.Inf(1)
		for _, area := range d.Territories {
			center := g.Stars[area.Anchor-1]
			distance := math.Hypot(s.X-center.X, s.Y-center.Y)
			if distance <= area.Radius && distance < best {
				best = distance
				s.Founder = area.Founder
			}
		}
	}
}
