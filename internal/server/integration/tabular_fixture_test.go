package integration

import (
	"encoding/json"
	"github.com/jackc/pgx/v5"
	"github.com/yomezza/cosmosimulator/internal/platform/dblayout"
	"github.com/yomezza/cosmosimulator/internal/universe"
	"github.com/yomezza/cosmosimulator/internal/universestore"
)

func (f *fixture033) writeWorld(raw []byte) {
	f.t.Helper()
	var g universe.Galaxy
	if e := json.Unmarshal(raw, &g); e != nil {
		f.t.Fatal(e)
	}
	tx, e := f.db.Begin(f.ctx)
	if e != nil {
		f.t.Fatal(e)
	}
	defer tx.Rollback(f.ctx)
	if e = universestore.Save(f.ctx, tx, &g); e != nil {
		f.t.Fatal(e)
	}
	if e = f.app.World.Commit(f.ctx, tx); e != nil {
		f.t.Fatal(e)
	}
}

// Only legacy-upgrade fixtures deliberately return this disposable schema to a
// pre-cutover layout. Production writers cannot modify the archived document.
func (f *fixture033) stageLegacy(raw []byte) {
	f.t.Helper()
	tx, e := f.db.Begin(f.ctx)
	if e != nil {
		f.t.Fatal(e)
	}
	defer tx.Rollback(f.ctx)
	root, e := dblayout.RootFromPath(f.db.Config().ConnConfig.RuntimeParams["search_path"])
	if e != nil {
		f.t.Fatal(e)
	}
	rows, e := tx.Query(f.ctx, `SELECT conrelid::regclass::text,conname FROM pg_constraint WHERE connamespace=ANY(ARRAY(SELECT oid FROM pg_namespace WHERE nspname=ANY($1::text[]))) AND contype='f' AND conname LIKE 'universe_%_fk'`, dblayout.Names(root, true))
	if e != nil {
		f.t.Fatal(e)
	}
	type fk struct{ table, name string }
	var keys []fk
	for rows.Next() {
		var k fk
		if e = rows.Scan(&k.table, &k.name); e != nil {
			f.t.Fatal(e)
		}
		keys = append(keys, k)
	}
	rows.Close()
	for _, k := range keys {
		if _, e = tx.Exec(f.ctx, "ALTER TABLE "+k.table+" DROP CONSTRAINT "+pgx.Identifier{k.name}.Sanitize()); e != nil {
			f.t.Fatal(e)
		}
	}
	if _, e = tx.Exec(f.ctx, `TRUNCATE universe_state,galaxy_catalog,system_identity,star_systems,system_planets,universe_lanes,universe_regions CASCADE`); e != nil {
		f.t.Fatal(e)
	}
	if _, e = tx.Exec(f.ctx, `INSERT INTO `+dblayout.Qualified(root, "galaxies")+`(name,data) VALUES('Edem',$1) ON CONFLICT(name) DO UPDATE SET data=excluded.data`, raw); e != nil {
		f.t.Fatal(e)
	}
	if e = f.app.World.Commit(f.ctx, tx); e != nil {
		f.t.Fatal(e)
	}
}
func (f *fixture033) worldJSON() []byte {
	f.t.Helper()
	g, _, e := universestore.Snapshot(f.ctx, f.db)
	if e != nil {
		f.t.Fatal(e)
	}
	raw, e := json.Marshal(g)
	if e != nil {
		f.t.Fatal(e)
	}
	return raw
}
