package universe

import (
	"errors"
	"fmt"
	"math"
	"math/rand"
	"sort"

	resourcecatalog "github.com/yomezza/cosmosimulator/internal/resources"
)

const MinGalaxySystems = 5
const MaxGalaxySystems = 50000
const MaxGalaxies = 24
const MaxWorldSystems = MaxGalaxySystems * MaxGalaxies

type GalaxySettings struct {
	ExtraLanes      int            `json:"extra_lanes"`
	CrossLinks      int            `json:"cross_links"`
	MaxLaneLength   int            `json:"max_lane_length"`
	StationPercent  int            `json:"station_percent"`
	MaxPorts        int            `json:"max_ports"`
	ResourceWeights map[string]int `json:"resource_weights"`
}

type GalaxyDefinition struct {
	Deleted     bool               `json:"deleted,omitempty"`
	ID          string             `json:"id"`
	Name        string             `json:"name"`
	Shape       string             `json:"shape"`
	Seed        int64              `json:"seed"`
	Opened      bool               `json:"opened"`
	RegionBase  int                `json:"region_base,omitempty"`
	Radius      float64            `json:"radius"`
	Settings    GalaxySettings     `json:"settings"`
	Territories []FounderTerritory `json:"territories,omitempty"`
}

func DefaultGalaxySettings() GalaxySettings {
	weights := map[string]int{}
	for _, r := range resourcecatalog.All {
		weights[r.ID] = 100
	}
	return GalaxySettings{15, 8, 1400, 18, 3, weights}
}

func GalaxyKey(s Star) string {
	if s.Galaxy == "asterion" || s.Galaxy == "Edem-II" || s.Galaxy == "Астерион" {
		return "asterion"
	}
	if s.Galaxy == "" || s.Galaxy == "Edem" {
		return "edem"
	}
	return s.Galaxy
}

func (g *Galaxy) Definition(id string) *GalaxyDefinition {
	for i := range g.Catalog {
		if g.Catalog[i].ID == id {
			return &g.Catalog[i]
		}
	}
	return nil
}

func GalaxyStars(g *Galaxy, id string) []Star {
	list := []Star{}
	for _, s := range g.Stars {
		if GalaxyKey(s) == id && !s.Deleted {
			list = append(list, s)
		}
	}
	return list
}

// Establish explicit membership before adding any systems. Existing global IDs
// remain indexes into Stars, even when galaxies have interleaved ID ranges.
func InitializeGalaxyCatalog(g *Galaxy) {
	if len(g.Catalog) > 0 {
		return
	}
	ArrangeFrontierRings(g)
	g.Catalog = []GalaxyDefinition{{ID: "edem", Name: "Edem", Shape: "spiral", Seed: g.Seed, Opened: true, Radius: 3600, Settings: DefaultGalaxySettings()}, {ID: "asterion", Name: "Астерион", Shape: "rings", Seed: 20261027, Radius: 1250, Settings: DefaultGalaxySettings()}}
	serials := map[string]int{}
	for i := range g.Stars {
		s := &g.Stars[i]
		id := GalaxyKey(*s)
		serials[id]++
		s.Galaxy = id
		s.Number = serials[id]
		if id == "asterion" {
			g.Catalog[1].Opened = true
		}
		if id == "edem" {
			s.X *= 3
			s.Y *= 3
		}
	}
}

func AppendGalaxySystems(g *Galaxy, id string, target int) error {
	d := g.Definition(id)
	if d == nil {
		return errors.New("Галактика не найдена")
	}
	current := len(GalaxyStars(g, id))
	if target < current || target > MaxGalaxySystems {
		return errors.New("Количество систем можно только увеличивать, максимум 50000")
	}
	if len(g.Stars)+target-current > MaxWorldSystems {
		return errors.New("Достигнут общий предел систем вселенной")
	}
	if current == target {
		return nil
	}
	// New objects get globally unique IDs; generation of existing objects is never repeated.
	generated := generate(target-current, d.Seed+int64(current)*131, false)
	rng := rand.New(rand.NewSource(d.Seed + int64(current)*104729))
	spacing := newSpacingIndex(GalaxyStars(g, id))
	used := map[string]bool{}
	for _, s := range g.Stars {
		used[s.Name] = true
	}
	for i, s := range generated.Stars {
		s.ID = len(g.Stars) + 1
		s.Number = current + i + 1
		s.Galaxy = id
		region := (s.Number-1)%4 + d.RegionBase
		if id == "asterion" {
			region += 4
		}
		s.Region = region
		if s.Number%50 == 0 {
			s.Region = -1
			if id == "asterion" {
				s.Region = -2
			}
		}
		radius := 300 + math.Sqrt(rng.Float64())*2400
		angle := float64(region%4)*math.Pi/2 + radius/1110 + (rng.Float64()-.5)*.5
		if d.Shape == "rings" {
			radius = 390 + float64(s.Number%3)*290 + (rng.Float64()-.5)*70
			angle = rng.Float64() * 2 * math.Pi
		}
		if d.Shape == "elliptical" {
			radius = d.Radius * (.10 + .70*math.Sqrt(rng.Float64()))
			angle = rng.Float64() * 2 * math.Pi
		}
		if d.Shape == "irregular" {
			angle = float64((s.Number-1)%4)*math.Pi/2 + (rng.Float64()-.5)*1.4
			radius = d.Radius * (.18 + rng.Float64()*.60)
		}
		if s.Region < 0 {
			radius = d.Radius * (.9 + rng.Float64()*.08)
			angle = rng.Float64() * 2 * math.Pi
		}
		// Best-of-candidates spacing avoids adding stars directly on existing dots.
		bestX, bestY, bestDistance := 0.0, 0.0, -1.0
		for trial := 0; trial < 8; trial++ {
			x, y := math.Cos(angle)*radius, math.Sin(angle)*radius
			if d.Shape == "elliptical" {
				y *= .55
			}
			nearest := spacing.nearest(x, y)
			if nearest > bestDistance {
				bestX, bestY, bestDistance = x, y, nearest
			}
			if nearest >= 32 {
				break
			}
			angle += (rng.Float64() - .5) * .08
			radius += (rng.Float64() - .5) * 30
		}
		s.X = math.Round(bestX*100) / 100
		s.Y = math.Round(bestY*100) / 100
		spacing.add(s.X, s.Y)
		prefix := "ED"
		if id == "asterion" {
			prefix = "AS"
		}
		s.Name = fmt.Sprintf("%s-%04d", prefix, s.Number)
		if s.Number%5 == 1 {
			s.Name = fmt.Sprintf("KE-%04d", s.ID)
		}
		if s.Number%5 == 2 {
			s.Name = fmt.Sprintf("AX-%04d", s.ID)
		}
		if s.Number%5 >= 3 {
			s.Name = fmt.Sprintf("%s %d", []string{"Дальняя Заря", "Янтарный Предел", "Серебряная Река", "Тихая Лагуна", "Лазурная Искра", "Звёздная Колыбель", "Скрытая Обитель", "Вечерняя Гавань"}[s.Number%8], 1+s.Number/40)
		}
		if used[s.Name] {
			s.Name += fmt.Sprintf(" · %d", s.ID)
		}
		used[s.Name] = true
		for j := range s.Planets {
			s.Planets[j].Name = fmt.Sprintf("%s-%c", s.Name, 'b'+j)
		}
		if g.DiscoverySecret != "" {
			SealStar(&s, g.DiscoverySecret, false)
		}
		g.Stars = append(g.Stars, s)
	}
	d.Opened = true
	return nil
}

func AssignGalaxyContent(g *Galaxy, id string) {
	d := g.Definition(id)
	core := map[int]bool{}
	for _, v := range CoreSystems(g, id) {
		core[v] = true
	}
	for i := range g.Stars {
		s := &g.Stars[i]
		if GalaxyKey(*s) != id {
			continue
		}
		n := 0
		if stablePercent(d.Seed, s.ID, 1) < founderStationChance(*d, *s) {
			n = 1 + stablePercent(d.Seed, s.ID, 2)%d.Settings.MaxPorts
		}
		if core[s.ID] {
			n = max(1, n)
		}
		if s.ID == 1 || s.ID == 5 {
			n = max(3, n)
		}
		s.Ports = &n
		s.ResourceWeights = d.Settings.ResourceWeights
		for j := range s.Planets {
			s.Planets[j].Resources = VariedStarResources(*s, s.Planets[j].ID, s.Planets[j].Kind)
		}
	}
	assignFounderTerritories(g, id)
}

func stablePercent(seed int64, id, salt int) int {
	return int(uint64(seed+int64(id)*104729+int64(salt)*7919)*6364136223846793005>>32) % 100
}

func RebuildGalaxyLanes(g *Galaxy, id string, protected []Lane) {
	d := g.Definition(id)
	stars := GalaxyStars(g, id)
	lanes := []Lane{}
	seen := map[[2]int]bool{}
	add := func(a, b int) {
		if a > b {
			a, b = b, a
		}
		key := [2]int{a, b}
		if a != b && !seen[key] {
			seen[key] = true
			lanes = append(lanes, Lane{a, b})
		}
	}
	for _, e := range g.Lanes {
		if GalaxyKey(g.Stars[e.From-1]) != id {
			add(e.From, e.To)
		}
	}
	if len(stars) > 10000 {
		for _, edge := range largeGalaxyLanes(stars, d) {
			add(edge.From, edge.To)
		}
	} else {
		// A minimum spanning forest favors short corridors; isolated systems stay out.
		regionSet := map[int]bool{}
		for _, s := range stars {
			if s.Region >= 0 {
				regionSet[s.Region] = true
			}
		}
		regionIDs := []int{}
		for r := range regionSet {
			regionIDs = append(regionIDs, r)
		}
		sort.Ints(regionIDs)
		for _, region := range regionIDs {
			members := []Star{}
			for _, s := range stars {
				if s.Region == region {
					members = append(members, s)
				}
			}
			if len(members) < 2 {
				continue
			}
			inTree := make([]bool, len(members))
			distance := make([]float64, len(members))
			parent := make([]int, len(members))
			for i := range distance {
				distance[i] = math.Inf(1)
				parent[i] = -1
			}
			distance[0] = 0
			for range members {
				u := -1
				for j := range members {
					if !inTree[j] && (u < 0 || distance[j] < distance[u]) {
						u = j
					}
				}
				inTree[u] = true
				if parent[u] >= 0 && distance[u] <= float64(d.Settings.MaxLaneLength) {
					add(members[u].ID, members[parent[u]].ID)
				}
				for j := range members {
					if !inTree[j] {
						v := math.Hypot(members[u].X-members[j].X, members[u].Y-members[j].Y)
						if v < distance[j] {
							distance[j] = v
							parent[j] = u
						}
					}
				}
			}
			for _, s := range members {
				if stablePercent(d.Seed, s.ID, 3) >= d.Settings.ExtraLanes {
					continue
				}
				best := 0
				dist := float64(d.Settings.MaxLaneLength)
				for _, p := range members {
					a, b := s.ID, p.ID
					if a > b {
						a, b = b, a
					}
					v := math.Hypot(s.X-p.X, s.Y-p.Y)
					if p.ID != s.ID && !seen[[2]int{a, b}] && v < dist {
						best = p.ID
						dist = v
					}
				}
				if best > 0 {
					add(s.ID, best)
				}
			}
		}
		// Keep only the nearest candidates per adjacent arm pair in one pass.
		// Increasing the requested bridge count does not repeat the all-pairs search.
		type bridgeCandidate struct {
			a, b     int
			distance float64
		}
		candidates := [4][]bridgeCandidate{}
		if d.Settings.CrossLinks > 0 {
			limit := (d.Settings.CrossLinks + 3) / 4
			for arm := 0; arm < 4; arm++ {
				for _, a := range stars {
					if a.Region < 0 || a.Region%4 != arm {
						continue
					}
					for _, b := range stars {
						if b.Region < 0 || b.Region%4 != (arm+1)%4 {
							continue
						}
						x, y := a.ID, b.ID
						if x > y {
							x, y = y, x
						}
						if seen[[2]int{x, y}] {
							continue
						}
						distance := math.Hypot(a.X-b.X, a.Y-b.Y)
						if distance > float64(d.Settings.MaxLaneLength) {
							continue
						}
						list := candidates[arm]
						if len(list) == limit && distance >= list[len(list)-1].distance {
							continue
						}
						list = append(list, bridgeCandidate{a.ID, b.ID, distance})
						sort.Slice(list, func(i, j int) bool { return list[i].distance < list[j].distance })
						if len(list) > limit {
							list = list[:limit]
						}
						candidates[arm] = list
					}
				}
			}
			for i := 0; i < d.Settings.CrossLinks; i++ {
				list := candidates[i%4]
				index := i / 4
				if index < len(list) {
					add(list[index].a, list[index].b)
				}
			}
		}

	}

	for _, e := range protected {
		add(e.From, e.To)
	}
	// Keep the two original training links usable by starter hulls. They are
	// explicit exceptions to the generated length limit, like committed flights.
	if id == "edem" && len(g.Stars) >= 9 {
		add(1, 5)
		add(5, 9)
	}
	sort.Slice(lanes, func(i, j int) bool {
		if lanes[i].From == lanes[j].From {
			return lanes[i].To < lanes[j].To
		}
		return lanes[i].From < lanes[j].From
	})
	g.Lanes = lanes
}

type GalaxyEdit struct {
	Create   bool           `json:"create"`
	Shape    string         `json:"shape"`
	Seed     int64          `json:"seed"`
	ID       string         `json:"id"`
	Name     string         `json:"name"`
	Systems  int            `json:"systems"`
	Settings GalaxySettings `json:"settings"`
	Preview  string         `json:"preview"`
}

func (g *Galaxy) RegionName(id int) string {
	for _, r := range g.Regions {
		if r.ID == id {
			return r.Name
		}
	}
	return regionName(id)
}

func VisibleGalaxies(g *Galaxy) []GalaxyDefinition {
	list := []GalaxyDefinition{}
	for _, d := range g.Catalog {
		if !d.Deleted {
			list = append(list, d)
		}
	}
	return list
}
