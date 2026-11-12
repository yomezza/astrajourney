package universe

import (
	"fmt"
	"math"
	"math/rand"
)

type Star struct {
	DiscoverySeed   string                        `json:"discovery_seed,omitempty"`
	ResourceRolls   map[string]map[string]float64 `json:"resource_rolls,omitempty"`
	Deleted         bool                          `json:"deleted,omitempty"`
	Number          int                           `json:"number,omitempty"`
	Founder         string                        `json:"founder,omitempty"`
	Ports           *int                          `json:"ports,omitempty"`
	ResourceWeights map[string]int                `json:"resource_weights,omitempty"`
	Galaxy          string                        `json:"galaxy,omitempty"`
	ID              int                           `json:"id"`
	Name            string                        `json:"name"`
	X               float64                       `json:"x"`
	Y               float64                       `json:"y"`
	Region          int                           `json:"region"`
	Class           string                        `json:"class"`
	Mass            float64                       `json:"mass"`
	Luminosity      float64                       `json:"luminosity"`
	Planets         []Planet                      `json:"planets,omitempty"`
}

type Lane struct {
	From int `json:"from"`
	To   int `json:"to"`
}

type Galaxy struct {
	DiscoverySecret   string             `json:"discovery_secret,omitempty"`
	SettlementVersion int                `json:"settlement_version,omitempty"`
	Revision          int64              `json:"revision,omitempty"`
	Catalog           []GalaxyDefinition `json:"galaxies,omitempty"`
	RingLayoutVersion int                `json:"ring_layout_version,omitempty"`
	NamingVersion     int                `json:"naming_version,omitempty"`
	Name              string             `json:"name"`
	Seed              int64              `json:"seed"`
	Version           int                `json:"version"`
	Stars             []Star             `json:"stars"`
	Regions           []Region           `json:"regions"`
	Lanes             []Lane             `json:"lanes"`
}

// Stable IDs and a private seeded RNG keep world generation independent of ticks.
// Four connected spiral arms and isolated outer stars deliberately form separate regions.
func Generate(count int, seed int64) Galaxy {
	return generate(count, seed, true)
}

// Managed galaxies rebuild corridors themselves; avoid generating an unused
// quadratic legacy network while preserving the exact same stars and RNG.
func generate(count int, seed int64, withLanes bool) Galaxy {
	g := Galaxy{Name: "Edem", Seed: seed, Version: 1, Stars: make([]Star, 0, count), Lanes: []Lane{}}
	rng := rand.New(rand.NewSource(seed))
	classes := []string{"M", "M", "M", "M", "M", "M", "M", "M", "M", "M", "M", "M", "K", "K", "K", "K", "G", "G", "F", "A"}
	isolated := count / 50
	for i := 0; i < count; i++ {
		region := i % 4
		radius := 100 + math.Sqrt(rng.Float64())*850
		angle := float64(region)*math.Pi/2 + radius/370 + (rng.Float64()-.5)*.36
		if i >= count-isolated {
			region = -1
			radius = 1050 + rng.Float64()*150
			angle = rng.Float64() * math.Pi * 2
		}
		g.Stars = append(g.Stars, Star{ID: i + 1, Name: fmt.Sprintf("ED-%04d", i+1), X: math.Round(math.Cos(angle)*radius*100) / 100, Y: math.Round(math.Sin(angle)*radius*100) / 100, Region: region, Class: classes[rng.Intn(len(classes))]})
	}
	if withLanes {
		// Attach each system to its three nearest predecessors in the same arm.
		// The first edge forms a spanning tree; extra edges create alternate routes.
		// No cross-arm edges and no edges to isolated systems are generated.
		for i, s := range g.Stars {
			if s.Region < 0 {
				continue
			}
			best := [3]int{-1, -1, -1}
			dist := [3]float64{math.Inf(1), math.Inf(1), math.Inf(1)}
			for j := 0; j < i; j++ {
				p := g.Stars[j]
				if p.Region != s.Region {
					continue
				}
				d := (s.X-p.X)*(s.X-p.X) + (s.Y-p.Y)*(s.Y-p.Y)
				for k := 0; k < 3; k++ {
					if d < dist[k] {
						for n := 2; n > k; n-- {
							dist[n] = dist[n-1]
							best[n] = best[n-1]
						}
						dist[k] = d
						best[k] = j
						break
					}
				}
			}
			for _, j := range best {
				if j >= 0 {
					g.Lanes = append(g.Lanes, Lane{From: g.Stars[j].ID, To: s.ID})
				}
			}
		}
	}
	g.Regions = Regions
	Enrich(&g)
	UpgradeGeology(&g)
	DiversifySystemNames(&g)
	return g
}

func (g Galaxy) Path(from, to int) []int {
	if from >= 1 && to >= 1 && from <= len(g.Stars) && to <= len(g.Stars) && (g.Stars[from-1].Deleted || g.Stars[to-1].Deleted) {
		return nil
	}
	if from < 1 || to < 1 || from > len(g.Stars) || to > len(g.Stars) {
		return nil
	}
	adj := make([][]int, len(g.Stars)+1)
	for _, e := range g.Lanes {
		adj[e.From] = append(adj[e.From], e.To)
		adj[e.To] = append(adj[e.To], e.From)
	}
	prev := make([]int, len(adj))
	prev[from] = -1
	queue := []int{from}
	for head := 0; head < len(queue); head++ {
		v := queue[head]
		if v == to {
			route := []int{}
			for v != -1 {
				route = append(route, v)
				v = prev[v]
			}
			for i, j := 0, len(route)-1; i < j; i, j = i+1, j-1 {
				route[i], route[j] = route[j], route[i]
			}
			return route
		}
		for _, n := range adj[v] {
			if prev[n] == 0 {
				prev[n] = v
				queue = append(queue, n)
			}
		}
	}
	return nil
}
