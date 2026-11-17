package universe

import (
	"fmt"
	"math"

	resourcecatalog "github.com/yomezza/cosmosimulator/internal/resources"
)

type Site struct {
	UUID          string                           `json:"uuid,omitempty"`
	ID            string                           `json:"id"`
	Name          string                           `json:"name"`
	Kind          string                           `json:"kind"`
	Orbit         float64                          `json:"orbit"`
	Resources     []string                         `json:"resources"`
	Planet        int                              `json:"planet"`
	Quality       string                           `json:"quality"`
	Concentration int                              `json:"concentration"`
	Details       *Planet                          `json:"details,omitempty"`
	Quotas        map[string]resourcecatalog.Quota `json:"quotas,omitempty"`
	SharedQuota   *resourcecatalog.Quota           `json:"shared_quota,omitempty"`
}

func HasStation(id int) bool { return id%3 != 0 || id == 1 || id == 5 || originalCore()[id] }

func SystemSites(s Star) []Site {
	sites := []Site{}
	scale := math.Sqrt(math.Max(s.Luminosity, .001))
	for _, p := range s.Planets {
		sites = append(sites, Site{ID: fmt.Sprintf("planet-%d", p.ID), Name: p.Name, Kind: p.Kind, Orbit: p.AU, Resources: p.Resources, Planet: p.ID, Details: &p})
	}
	sites = append(sites, Site{ID: "asteroids", Name: s.Name + " · A", Kind: "Пояс астероидов", Orbit: 2.3 * scale, Resources: VariedStarResources(s, 101, "Пояс астероидов"), Planet: 101})
	if s.ID%2 == 0 {
		sites = append(sites, Site{ID: "ice", Name: s.Name + " · I", Kind: "Ледяное поле", Orbit: 6.5 * scale, Resources: VariedStarResources(s, 102, "Ледяное поле"), Planet: 102})
	}
	ports := 0
	if HasStation(s.ID) {
		ports = 1 + s.ID%3
	}
	if s.Ports != nil {
		ports = *s.Ports
	}
	if ports > 0 {
		for i := 0; i < ports; i++ {
			id := "station"
			if i > 0 {
				id = fmt.Sprintf("station-%d", i+1)
			}
			sites = append(sites, Site{ID: id, Name: fmt.Sprintf("%s · PORT %d", s.Name, i+1), Kind: "Орбитальная станция", Orbit: (1.2 + float64(i)*.8) * scale})
		}
	}
	for i := range sites {
		for _, o := range s.Objects {
			if sites[i].Planet == o.ID {
				sites[i].Name = o.Name
				sites[i].UUID = o.UUID
			}
		}
	}
	return sites
}
