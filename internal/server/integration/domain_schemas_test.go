package integration

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/yomezza/cosmosimulator/internal/account"
	"github.com/yomezza/cosmosimulator/internal/chat"
	"github.com/yomezza/cosmosimulator/internal/chatintegration"
	"github.com/yomezza/cosmosimulator/internal/platform/dblayout"
	"github.com/yomezza/cosmosimulator/internal/universe"
)

func TestDomainSchemasRuntimeRestartAndNewPartitions(t *testing.T) {
	t.Setenv("CHAT_ENABLED", "true")
	f := newFixture033(t)
	cookie, uid := f.signup("domain_admin")
	f.exec(`UPDATE users SET role='admin' WHERE id=$1`, uid)
	root, e := dblayout.RootFromPath(f.db.Config().ConnConfig.RuntimeParams["search_path"])
	if e != nil {
		t.Fatal(e)
	}
	pool, _, _ := n1RuntimePool(t, f)
	service := &chat.Service{DB: pool}
	// Existing definer owners and runtime grants survive re-running startup DDL.
	db, e := openDatabase(f.ctx, f.db.Config().ConnString(), root)
	if e != nil {
		t.Fatal(e)
	}
	db.Close()
	if _, e = service.Authenticate(f.ctx, account.TokenHash(cookie.Value)); e != nil {
		t.Fatal("restricted authentication after restart", e)
	}
	in := universe.GalaxyEdit{Create: true, ID: "domain-partition", Name: "Domain partition", Shape: "rings", Seed: 42, Systems: 5, Settings: universe.DefaultGalaxySettings()}
	response := f.req(cookie, "POST", "/api/admin/galaxies/preview", in, 200)
	var preview struct {
		Preview string `json:"preview"`
	}
	if e = json.Unmarshal(response.Body.Bytes(), &preview); e != nil {
		t.Fatal(e)
	}
	in.Preview = preview.Preview
	f.req(cookie, "POST", "/api/admin/galaxies/apply", in, 200)
	if f.integer(`SELECT count(*) FROM star_systems WHERE galaxy_id='domain-partition'`) != 5 {
		t.Fatal("new galaxy missing")
	}
	if f.integer(`SELECT count(*) FROM pg_inherits i JOIN pg_class p ON p.oid=i.inhparent JOIN pg_class c ON c.oid=i.inhrelid WHERE p.oid='star_systems'::regclass AND c.relnamespace<>p.relnamespace`) != 0 {
		t.Fatal("star partition created outside universe schema")
	}
	if e = chatintegration.MaintainPartitions(f.ctx, f.db, time.Now().AddDate(0, 0, 20)); e != nil {
		t.Fatal("Chat partition maintenance", e)
	}

	rows, e := f.db.Query(f.ctx, `SELECT c.relname FROM pg_class c WHERE c.relnamespace=to_regnamespace($1) AND c.relkind IN('r','p','v')`, root)
	if e != nil {
		t.Fatal(e)
	}
	defer rows.Close()
	for rows.Next() {
		var name string
		if e = rows.Scan(&name); e != nil {
			t.Fatal(e)
		}
		if name != "_astra_schema_layout" && name != "galaxies" {
			t.Fatal("legacy relation recreated", name)
		}
	}
	if e = rows.Err(); e != nil {
		t.Fatal(e)
	}
	var childSchema string
	future := time.Now().UTC().AddDate(0, 0, 20).Format("20060102")
	if e = f.db.QueryRow(f.ctx, `SELECT n.nspname FROM pg_class c JOIN pg_namespace n ON n.oid=c.relnamespace WHERE c.oid=to_regclass($1)`, dblayout.Qualified(root, "chat_messages_global_"+future)).Scan(&childSchema); e != nil || childSchema != dblayout.Schema(root, "chat") {
		t.Fatal("Chat child misplaced", childSchema, e)
	}
}
