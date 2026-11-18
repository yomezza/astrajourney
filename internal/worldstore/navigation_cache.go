package worldstore

import (
	"sync"

	"github.com/yomezza/cosmosimulator/internal/universe"
)

// NavigationMetadata contains read-only, generated facts about an immutable
// world snapshot. Consumers must not mutate its slices or maps.
type NavigationMetadata struct {
	Cores       []int
	PortalCores map[string][]int
	BlackHoles  map[string]any
}

type navigationCache struct {
	mu    sync.Mutex
	world *universe.Galaxy
	data  NavigationMetadata
}

func (a *Service) NavigationMetadata(g *universe.Galaxy) NavigationMetadata {
	c := &a.navigation
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.world != g {
		portals := universe.PortalCores(g)
		holes := map[string]any{}
		for id, cores := range portals {
			holes[id] = map[string]any{"x": 0, "y": 0, "systems": cores}
		}
		c.data = NavigationMetadata{Cores: universe.CoreSystems(g), PortalCores: portals, BlackHoles: holes}
		c.world = g
	}
	return c.data
}
