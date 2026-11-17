package integration

import (
	"encoding/json"
	"github.com/yomezza/cosmosimulator/internal/server"
	"strings"
	"testing"
)

func TestFounderUpgrade300PreservesIdentityAndPlayerNames(t *testing.T) {
	f := newFixture033(t)
	_, uid := f.signup("upgrade_founders300")
	first := f.integer(`SELECT id FROM companies WHERE founder_key='founder-01'`)
	second := f.integer(`SELECT id FROM companies WHERE founder_key='founder-02'`)
	// Model the two-founder company catalogue, while retaining every station ID.
	f.exec(`UPDATE stations SET founder=(SELECT id FROM companies WHERE founder_key='founder-01') WHERE founder IN(SELECT id FROM companies WHERE founder_key IN('founder-03','founder-04'))`)
	f.exec(`DELETE FROM companies WHERE founder_key IN('founder-03','founder-04')`)
	f.exec(`INSERT INTO companies(name,tag,color,flag,owner_id) VALUES('Корпорация-основатель 03','F03','#112233','P',$1)`, uid)
	player := f.integer(`SELECT id FROM companies WHERE tag='F03'`)
	f.exec(`UPDATE game_release SET version='0.29.0',migration=290 WHERE id=1`)
	var before []byte
	if e := f.db.QueryRow(f.ctx, `SELECT COALESCE(jsonb_agg(jsonb_build_array(star,object_id,name,founder) ORDER BY star,object_id),'[]'::jsonb) FROM stations`).Scan(&before); e != nil {
		t.Fatal(e)
	}
	migration, e := server.Assets.ReadFile("schema300.sql")
	if e != nil {
		t.Fatal(e)
	}
	for i := 0; i < 2; i++ {
		if _, e = f.db.Exec(f.ctx, string(migration)); e != nil {
			t.Fatal(e)
		}
	}
	if f.integer(`SELECT count(*) FROM companies WHERE founder`) != 4 || f.integer(`SELECT id FROM companies WHERE founder_key='founder-01'`) != first || f.integer(`SELECT id FROM companies WHERE founder_key='founder-02'`) != second {
		t.Fatal("founder identity changed")
	}
	if f.integer(`SELECT id FROM companies WHERE tag='F03' AND NOT founder AND name='Корпорация-основатель 03'`) != player {
		t.Fatal("player name/tag appropriated")
	}
	var after []byte
	if e = f.db.QueryRow(f.ctx, `SELECT COALESCE(jsonb_agg(jsonb_build_array(star,object_id,name,founder) ORDER BY star,object_id),'[]'::jsonb) FROM stations`).Scan(&after); e != nil {
		t.Fatal(e)
	}
	if string(before) != string(after) {
		t.Fatal("existing stations changed during founder upgrade")
	}
	f.exec(`UPDATE game_release SET version='0.30.77' WHERE id=1`)
	if _, e = f.db.Exec(f.ctx, string(migration)); e != nil {
		t.Fatal(e)
	}
	var version string
	if e = f.db.QueryRow(f.ctx, `SELECT version FROM game_release WHERE id=1`).Scan(&version); e != nil || version != "0.30.77" {
		t.Fatal("operator version did not survive restart", version, e)
	}
}
func TestCosmicOnlyServesAPI300(t *testing.T) {
	f := newFixture033(t)
	for _, p := range []string{"/", "/index.html", "/bootstrap.js", "/styles/terminal.css", "/profile.svg", "/web/unknown/index.html", "/chat/ws/v1", "/planet/ws/v1"} {
		r := f.req(nil, "GET", p, nil, 404)
		if strings.Contains(r.Body.String(), "<!doctype html>") {
			t.Fatal("API returned frontend", p)
		}
	}
	r := f.req(nil, "GET", "/api/client-contract", nil, 200)
	var body map[string]string
	if json.Unmarshal(r.Body.Bytes(), &body) != nil || body["api_contract"] != "astra-api-v1" {
		t.Fatal("missing API compatibility contract")
	}
}
