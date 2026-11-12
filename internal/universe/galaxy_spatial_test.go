package universe

import (
	"math"
	"reflect"
	"testing"
	"time"
)

func TestLargeForestMatchesShortestNetwork(t *testing.T) {
	g := Generate(180, 73)
	g.Catalog = []GalaxyDefinition{{ID: "edem", Shape: "spiral", Settings: DefaultGalaxySettings()}}
	d := &g.Catalog[0]
	d.Settings.ExtraLanes = 0
	d.Settings.CrossLinks = 0
	d.Settings.MaxLaneLength = 10000
	// Use a non-starter galaxy to exclude the two fixed training corridors.
	d.ID = "comparison"
	for i := range g.Stars {
		g.Stars[i].Galaxy = d.ID
	}
	RebuildGalaxyLanes(&g, d.ID, nil)
	weight := func(edges []Lane) float64 {
		sum := 0.0
		for _, e := range edges {
			a, b := g.Stars[e.From-1], g.Stars[e.To-1]
			sum += math.Hypot(a.X-b.X, a.Y-b.Y)
		}
		return sum
	}
	expected := weight(g.Lanes)
	edges := largeGalaxyLanes(g.Stars, d)
	if len(edges) != len(g.Lanes) || math.Abs(weight(edges)-expected) > 1e-7 {
		t.Fatalf("indexed forest differs: edges=%d/%d cost=%f/%f", len(edges), len(g.Lanes), weight(edges), expected)
	}
	if !reflect.DeepEqual(edges, largeGalaxyLanes(g.Stars, d)) {
		t.Fatal("large forest is not deterministic")
	}
}

func TestGalaxyFiftyThousandSystems(t *testing.T) {
	start := time.Now()
	g := Galaxy{Catalog: []GalaxyDefinition{{ID: "large", Name: "Large", Shape: "rings", Seed: 19, Radius: 1250, RegionBase: 8, Opened: true, Settings: DefaultGalaxySettings()}}}
	if err := AppendGalaxySystems(&g, "large", 50000); err != nil {
		t.Fatal(err)
	}
	AssignGalaxyContent(&g, "large")
	RebuildGalaxyLanes(&g, "large", nil)
	if len(g.Stars) != 50000 || len(g.Lanes) == 0 {
		t.Fatal("large galaxy was not generated")
	}
	seen := map[[2]int]bool{}
	for _, e := range g.Lanes {
		key := galaxyEdgeKey(e.From, e.To)
		if e.From < 1 || e.To < 1 || e.From > 50000 || e.To > 50000 || seen[key] || e.From == e.To {
			t.Fatal("invalid corridor", e)
		}
		seen[key] = true
		a, b := g.Stars[e.From-1], g.Stars[e.To-1]
		if a.Region < 0 || b.Region < 0 || math.Hypot(a.X-b.X, a.Y-b.Y) > 1400.000001 {
			t.Fatal("corridor ignores isolation/length", e)
		}
	}
	t.Logf("50000 systems: %d corridors, generated in %s", len(g.Lanes), time.Since(start))
}

func TestManagedGenerationPreservesStarMetadata(t *testing.T) {
	for _, count := range []int{5, 80} {
		expected := Generate(count, 1794461000423)
		actual := generate(count, 1794461000423, false)
		if !reflect.DeepEqual(expected.Stars, actual.Stars) {
			t.Fatal("skipping unused corridors changed generated stars", count)
		}
	}
}

func TestLargeForestBoundaryAndCoincidentStars(t *testing.T) {
	stars := []Star{{ID: 1, X: 0, Region: 0}, {ID: 2, X: 100, Region: 0}, {ID: 3, X: 250, Region: 0}, {ID: 4, X: 250, Region: 0}, {ID: 5, X: 1, Region: -1}}
	d := GalaxyDefinition{Settings: DefaultGalaxySettings()}
	d.Settings.MaxLaneLength = 100
	d.Settings.ExtraLanes = 0
	d.Settings.CrossLinks = 0
	edges := largeGalaxyLanes(stars, &d)
	if len(edges) != 2 {
		t.Fatalf("expected one maximum-length corridor and one zero-length corridor: %v", edges)
	}
	for _, edge := range edges {
		key := galaxyEdgeKey(edge.From, edge.To)
		if key != [2]int{1, 2} && key != [2]int{3, 4} {
			t.Fatal("invalid isolated/disconnected edge", edge)
		}
	}
}
