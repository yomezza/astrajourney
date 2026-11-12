package worldstore

import (
	"github.com/yomezza/cosmosimulator/internal/universe"
	"testing"
)

func TestGalaxyEditSystemLimits(t *testing.T) {
	g := &universe.Galaxy{Stars: make([]universe.Star, 3000), Catalog: []universe.GalaxyDefinition{{ID: "edem", Name: "Edem", Opened: true}}}
	for _, count := range []int{4, 5, 50000, 50001} {
		in := universe.GalaxyEdit{Create: true, ID: "limit-test", Name: "Limit test", Shape: "spiral", Systems: count, Settings: universe.DefaultGalaxySettings()}
		err := validateGalaxyEdit(in, g)
		if valid := count >= 5 && count <= 50000; (err == nil) != valid {
			t.Fatalf("count=%d valid=%v error=%v", count, valid, err)
		}
	}
	in := universe.GalaxyEdit{ID: "edem", Name: "Edem", Systems: 2999, Settings: universe.DefaultGalaxySettings()}
	if validateGalaxyEdit(in, g) == nil {
		t.Fatal("existing systems must not be removed")
	}
	in.Systems = 50000
	if err := validateGalaxyEdit(in, g); err != nil {
		t.Fatal(err)
	}
}
