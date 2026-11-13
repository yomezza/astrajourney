package universe

import (
	"math"
	"math/rand"
)

type Region struct {
	ID        int    `json:"id"`
	Name      string `json:"name"`
	Specialty string `json:"specialty"`
}

var Regions = []Region{{0, "Кузни Сварога", "orichalcum"}, {1, "Сады Гесперид", "ambrosia"}, {2, "Пределы Айн Соф", "azoth"}, {3, "Мандала Акаништхи", "vajra"}, {-1, "Безмолвие Техом", "agni"}}

var FrontierRegions = []Region{{4, "Арка Эхо", "orichalcum"}, {5, "Поля Веспера", "ambrosia"}, {6, "Грань Авроры", "azoth"}, {7, "Венец Небулы", "vajra"}, {-2, "Тихий Океанис", "agni"}}

func AllRegions() []Region { return append(append([]Region(nil), Regions...), FrontierRegions...) }

func regionName(id int) string {
	for _, r := range AllRegions() {
		if r.ID == id {
			return r.Name
		}
	}
	return ""
}

type Planet struct {
	CatalogCode string   `json:"catalog_code,omitempty"`
	ID          int      `json:"id"`
	Name        string   `json:"name"`
	Kind        string   `json:"kind"`
	AU          float64  `json:"au"`
	Mass        float64  `json:"mass"`
	Gravity     float64  `json:"gravity"`
	Temperature int      `json:"temperature"`
	Atmosphere  string   `json:"atmosphere"`
	Resources   []string `json:"resources"`
}

func Enrich(g *Galaxy) {
	for i := range g.Stars {
		s := &g.Stars[i]
		if len(s.Planets) > 0 {
			continue
		}
		rng := rand.New(rand.NewSource(g.Seed + int64(s.ID)*7919))
		if s.DiscoverySeed != "" {
			rng = discoveryRNG(s.DiscoverySeed, "planets")
		}
		mass := map[string]float64{"O": 20, "B": 5, "A": 1.8, "F": 1.2, "G": 1, "K": .7, "M": .25}[s.Class]
		if mass == 0 {
			mass = 1
		}
		s.Mass = mass
		s.Luminosity = math.Round(math.Pow(mass, 3.5)*10000) / 10000
		n := 3 + rng.Intn(5)
		au := .12 + rng.Float64()*.15
		for j := 0; j < n; j++ {
			au *= 1.5 + rng.Float64()*.6
			temp := int(278 * math.Pow(s.Luminosity, .25) / math.Sqrt(au))
			kind, atm := "Каменистая", "Разреженная"
			pm := .2 + rng.Float64()*3
			radius := math.Pow(pm, .27)
			ids := []string{"orichalcum", "vajra"}
			if temp < 180 {
				kind = "Ледяная"
				ids = []string{"agni", "azoth"}
			}
			if temp >= 230 && temp <= 320 {
				atm = "Плотная"
				ids = append(ids, "manna", "ambrosia")
			}
			if j > 1 && rng.Float64() < .35 {
				kind = "Газовый гигант"
				atm = "Водородная"
				pm = 30 + rng.Float64()*200
				radius = 4 + rng.Float64()*6
				ids = []string{"agni", "azoth"}
			}
			for _, r := range Regions {
				if r.ID == s.Region && kind != "Газовый гигант" {
					if r.Specialty != "ambrosia" || temp >= 230 && temp <= 320 {
						found := false
						for _, id := range ids {
							if id == r.Specialty {
								found = true
							}
						}
						if !found {
							ids = append(ids, r.Specialty)
						}
					}
				}
			}
			s.Planets = append(s.Planets, Planet{ID: j + 1, Name: s.Name + "-" + string(rune('b'+j)), Kind: kind, AU: math.Round(au*100) / 100, Mass: math.Round(pm*100) / 100, Gravity: math.Round(pm/(radius*radius)*100) / 100, Temperature: temp, Atmosphere: atm, Resources: ids})
		}
	}
	g.Version = 2
}
