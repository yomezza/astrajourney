package integration

import (
	"encoding/json"
	"fmt"
	"strings"
	"testing"

	"github.com/yomezza/cosmosimulator/internal/universe"
)

func TestSystemNamingMigration(t *testing.T) {
	g := universe.Generate(1000, 20261026)
	g.NamingVersion = 0
	for i := range g.Stars {
		g.Stars[i].Name = fmt.Sprintf("ED-%04d", g.Stars[i].ID)
		for j := range g.Stars[i].Planets {
			g.Stars[i].Planets[j].Name = fmt.Sprintf("%s-%c", g.Stars[i].Name, 'b'+j)
		}
	}
	g.Stars[9].Name = "Custom name"
	g.Stars[0].Planets[0].Name = "Custom planet"
	x, y := g.Stars[0].X, g.Stars[0].Y
	lanes, _ := json.Marshal(g.Lanes)
	if !universe.DiversifySystemNames(&g) {
		t.Fatal("migration not applied")
	}
	seen := map[string]bool{}
	ed, other, human := 0, 0, 0
	for _, s := range g.Stars {
		if seen[s.Name] {
			t.Fatal("duplicate name", s.Name)
		}
		seen[s.Name] = true
		if strings.HasPrefix(s.Name, "ED-") {
			ed++
		} else if strings.Contains(s.Name, "-") {
			other++
		} else {
			human++
		}
	}
	if ed == 0 || other == 0 || human == 0 {
		t.Fatal("name categories missing")
	}
	after, _ := json.Marshal(g.Lanes)
	if string(lanes) != string(after) || g.Stars[0].X != x || g.Stars[0].Y != y || g.Stars[9].Name != "Custom name" || g.Stars[0].Planets[0].Name != "Custom planet" {
		t.Fatal("migration changed identity/topology or custom names")
	}
	if g.Stars[2].Planets[0].Name != g.Stars[2].Name+"-b" {
		t.Fatal("planet label stale")
	}
	if universe.DiversifySystemNames(&g) {
		t.Fatal("migration repeated")
	}
}

func TestPublicServerStatus(t *testing.T) {
	f := newFixture033(t)
	response := f.req(nil, "GET", "/api/server-status", nil, 200)
	var status struct {
		Name string `json:"name"`
		Bots int    `json:"bots"`
	}
	if err := json.Unmarshal(response.Body.Bytes(), &status); err != nil || status.Name != "Иггдрасиль" || status.Bots != 3 {
		t.Fatal("public server tooltip data", status, err)
	}
}

func TestSystemAndPortNamesStayConsistent(t *testing.T) {
	f := newFixture033(t)
	c, _ := f.signup("names034")
	f.req(c, "GET", "/api/workbench", nil, 200)
	f.exec(`UPDATE universe_state SET naming_version=0,storage_version=storage_version+1 WHERE id=1; UPDATE star_systems SET name='ED-0001' WHERE id=1`)
	f.exec(`UPDATE stations SET name=CASE WHEN object_id='station' THEN 'ED-0001 · PORT 1' ELSE 'Custom port' END WHERE star=1`)
	config := f.db.Config()
	var actual string
	if err := f.db.QueryRow(f.ctx, `SELECT current_schema()`).Scan(&actual); err != nil {
		t.Fatal(err)
	}
	again, err := openDatabase(f.ctx, config.ConnString(), actual)
	if err != nil {
		t.Fatal(err)
	}
	again.Close()
	var name, custom string
	if err = f.db.QueryRow(f.ctx, `SELECT name FROM stations WHERE star=1 AND object_id='station'`).Scan(&name); err != nil || name != "KE-0001 · PORT 1" {
		t.Fatal(name, err)
	}
	if err = f.db.QueryRow(f.ctx, `SELECT name FROM stations WHERE star=1 AND object_id='station-2'`).Scan(&custom); err != nil || custom != "Custom port" {
		t.Fatal("custom port renamed", custom, err)
	}
}
