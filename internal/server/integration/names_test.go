package integration

import (
	"encoding/json"
	"testing"
)

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
	f.exec(`UPDATE stations SET name=CASE WHEN object_id='station' THEN 'ED-0001 · PORT 1' ELSE 'Custom port' END WHERE star=1 AND object_id IN('station','station-2')`)
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
	if err = f.db.QueryRow(f.ctx, `SELECT name FROM stations WHERE star=1 AND object_id='station'`).Scan(&name); err != nil || name != "ED-0001 · PORT 1" {
		t.Fatal(name, err)
	}
	if err = f.db.QueryRow(f.ctx, `SELECT name FROM stations WHERE star=1 AND object_id='station-2'`).Scan(&custom); err != nil || custom != "Custom port" {
		t.Fatal("custom port renamed", custom, err)
	}
}
