package fleet

import "testing"

func TestModuleCapacityAndEffects(t *testing.T) {
	s := Ship{Class: "pilgrim", Cargo: map[string]int{"iron": 50}, ModulesReady: true, Modules: []ShipModule{
		{ModuleDefinition: ModuleDefinition{Kind: "cargo", Size: 1, Rating: "C", Effects: ModuleEffects{Cargo: 10}}, Installed: true},
		{ModuleDefinition: ModuleDefinition{Kind: "cargo", Size: 5, Rating: "C", Effects: ModuleEffects{Cargo: 50}}},
		{ModuleDefinition: ModuleDefinition{Kind: "scanner", Size: 2, Rating: "B", Effects: ModuleEffects{Scan: 20}}, Installed: true},
	}}
	if s.Capacity() != 60 || s.CargoUsed() != 55 || s.ModuleUsed() != 3 {
		t.Fatalf("baseline: capacity=%d cargo=%d fitting=%d", s.Capacity(), s.CargoUsed(), s.ModuleUsed())
	}
	s.Modules[1].Installed = true
	if s.Capacity() != 110 || s.CargoUsed() != 50 {
		t.Fatalf("installed rack: capacity=%d used=%d", s.Capacity(), s.CargoUsed())
	}
	s.Cargo["iron"] = 100
	s.Modules[1].Installed = false
	if s.CargoUsed() <= s.Capacity() {
		t.Fatal("removing a loaded cargo rack must overflow")
	}
	if s.ScanSeconds(10) != 8 || s.ScanSeconds(1) != 1 {
		t.Fatal("scanner must speed research up without zero duration")
	}
}
func TestModuleDefinitionValidation(t *testing.T) {
	d := ModuleDefinition{CatalogID: "rack", Name: "Rack", Kind: "cargo", Rating: "C", Size: 5, Price: 100, Effects: ModuleEffects{Cargo: 50}}
	if err := d.Validate(); err != nil {
		t.Fatal(err)
	}
	d.Rating = "S"
	if d.Validate() == nil {
		t.Fatal("S cargo racks forbidden")
	}
	d.Rating = "C"
	d.Effects.Jump = 50
	if d.Validate() == nil {
		t.Fatal("wrong-family effect accepted")
	}
	d.Effects.Jump = 0
	d.Size = 0
	if d.Validate() == nil {
		t.Fatal("zero size accepted")
	}
}

func TestModuleExtractionChoosesCompatibleEquipment(t *testing.T) {
	s := Ship{Class: "pilgrim", ModulesReady: true, Modules: []ShipModule{
		{ModuleDefinition: ModuleDefinition{Kind: "extractor", Effects: ModuleEffects{Extractor: "universal"}}, Installed: true},
		{ModuleDefinition: ModuleDefinition{Kind: "extractor", Effects: ModuleEffects{Extractor: "ore", Yield: 3}}, Installed: true},
		{ModuleDefinition: ModuleDefinition{Kind: "extractor", Effects: ModuleEffects{Extractor: "ice", Yield: 100}}},
	}}
	if s.ExtractionYieldFor(1, 1, "iron") <= s.ExtractionYieldFor(1, 1, "water") {
		t.Fatal("specialized drill must improve ore without improving water extraction")
	}
	s.Modules[0].Installed = false
	if s.ExtractionYieldFor(1, 1, "water") != 0 {
		t.Fatal("a packed ice extractor must not extract water")
	}
}
