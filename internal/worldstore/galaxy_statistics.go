package worldstore

import (
	"context"
	"fmt"
	"strconv"
	"strings"
	"sync"

	"github.com/yomezza/cosmosimulator/internal/universe"
)

type galaxyMetrics struct {
	systems, founders, lanes, cross int
	resources                       map[string]int
}

// Only generated facts belong to this cache. Protected stations depend on live
// property and obligations and are queried and merged afresh on every request.
type galaxyStatsCache struct {
	mu    sync.Mutex
	world *universe.Galaxy
	base  map[string]*galaxyMetrics
	ports map[string]bool
}

func (c *galaxyStatsCache) statistics(ctx context.Context, g *universe.Galaxy, protected map[string]bool) (map[string]map[string]any, error) {
	c.mu.Lock()
	if c.world != g {
		base, ports, err := generatedGalaxyStatistics(ctx, g)
		if err != nil {
			c.mu.Unlock()
			return nil, err
		}
		c.world, c.base, c.ports = g, base, ports
	}
	base, generated := c.base, c.ports
	c.mu.Unlock()

	counts := map[string]int{}
	systems := map[string]map[int]bool{}
	addPort := func(key string) {
		prefix, _, _ := strings.Cut(key, "/")
		id, err := strconv.Atoi(prefix)
		if err != nil || id < 1 || id > len(g.Stars) || g.Stars[id-1].Deleted {
			return
		}
		galaxy := universe.GalaxyKey(g.Stars[id-1])
		counts[galaxy]++
		if systems[galaxy] == nil {
			systems[galaxy] = map[int]bool{}
		}
		systems[galaxy][id] = true
	}
	for key := range generated {
		addPort(key)
	}
	for key := range protected {
		if !generated[key] {
			addPort(key)
		}
	}
	out := map[string]map[string]any{}
	for id, m := range base {
		out[id] = map[string]any{
			"systems": m.systems, "lanes": m.lanes, "cross_links": m.cross,
			"station_systems": len(systems[id]), "stations": counts[id],
			"resources": m.resources, "founder_systems": m.founders,
			"neutral_systems": m.systems - m.founders,
		}
	}
	return out, ctx.Err()
}

func generatedGalaxyStatistics(ctx context.Context, g *universe.Galaxy) (map[string]*galaxyMetrics, map[string]bool, error) {
	base := map[string]*galaxyMetrics{}
	metric := func(id string) *galaxyMetrics {
		if base[id] == nil {
			base[id] = &galaxyMetrics{resources: map[string]int{}}
		}
		return base[id]
	}
	for _, d := range g.Catalog {
		metric(d.ID)
	}
	ports := map[string]bool{}
	for _, star := range g.Stars {
		if err := ctx.Err(); err != nil {
			return nil, nil, err
		}
		if star.Deleted {
			continue
		}
		m := metric(universe.GalaxyKey(star))
		m.systems++
		if star.Founder != "" {
			m.founders++
		}
		seen := map[string]bool{}
		// Generate sites once per system for both resource and station counts.
		for _, site := range universe.SystemSites(star) {
			if site.Kind == "Орбитальная станция" {
				ports[fmt.Sprintf("%d/%s", star.ID, site.ID)] = true
			}
			for _, resource := range site.Resources {
				seen[resource] = true
			}
		}
		for resource := range seen {
			m.resources[resource]++
		}
	}
	for _, lane := range g.Lanes {
		from, to := g.Stars[lane.From-1], g.Stars[lane.To-1]
		m := metric(universe.GalaxyKey(from))
		m.lanes++
		if from.Region != to.Region {
			m.cross++
		}
	}
	return base, ports, nil
}
