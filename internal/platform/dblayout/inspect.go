package dblayout

import (
	"context"
	"fmt"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

type SchemaInfo struct {
	Name      string `json:"name"`
	Domain    string `json:"domain"`
	Exists    bool   `json:"exists"`
	Relations int    `json:"relations"`
}
type Report struct {
	Root          string       `json:"root"`
	LayoutVersion int          `json:"layout_version"`
	SearchPath    string       `json:"search_path"`
	Ready         bool         `json:"ready"`
	Schemas       []SchemaInfo `json:"schemas"`
	Issues        []string     `json:"issues"`
}

// Inspect never creates schemas or alters database/role defaults. Its transaction
// is READ ONLY even when called using the migration owner's credentials.
func Inspect(ctx context.Context, pool *pgxpool.Pool, root string) (Report, error) {
	r := Report{Root: root, SearchPath: SearchPath(root), Issues: []string{}}
	tx, e := pool.BeginTx(ctx, pgx.TxOptions{AccessMode: pgx.ReadOnly, IsoLevel: pgx.RepeatableRead})
	if e != nil {
		return r, e
	}
	defer tx.Rollback(ctx)
	installed, e := Installed(ctx, tx, root)
	if e != nil {
		return r, e
	}
	if installed {
		var recorded string
		e = tx.QueryRow(ctx, "SELECT version,root_name FROM "+pgx.Identifier{root, "_astra_schema_layout"}.Sanitize()+" WHERE id=1").Scan(&r.LayoutVersion, &recorded)
		if e != nil {
			return r, e
		}
		if (r.LayoutVersion != 1 && r.LayoutVersion != 2) || recorded != root {
			r.Issues = append(r.Issues, "layout metadata does not match this installation/version")
		}
	}
	var createDB bool
	if e = tx.QueryRow(ctx, `SELECT has_database_privilege(current_database(),'CREATE')`).Scan(&createDB); e != nil {
		return r, e
	}
	for i, name := range Names(root, true) {
		info := SchemaInfo{Name: name, Domain: "bootstrap"}
		if i > 0 {
			info.Domain = Domains[i-1]
		}
		var create bool
		if e = tx.QueryRow(ctx, `SELECT to_regnamespace($1) IS NOT NULL,COALESCE(has_schema_privilege(to_regnamespace($1),'CREATE'),false),(SELECT count(*) FROM pg_class WHERE relnamespace=to_regnamespace($1))`, name).Scan(&info.Exists, &create, &info.Relations); e != nil {
			return r, e
		}
		if !info.Exists && !createDB {
			r.Issues = append(r.Issues, "CREATE on database required for "+name)
		}
		if info.Exists && !create {
			r.Issues = append(r.Issues, "CREATE on schema required for migration: "+name)
		}
		if i > 0 && (!installed || r.LayoutVersion == 1 && info.Domain == "dictionaries") {
			var occupied bool
			if e = tx.QueryRow(ctx, occupiedSQL, name).Scan(&occupied); e != nil {
				return r, e
			}
			if occupied {
				r.Issues = append(r.Issues, "target schema already occupied: "+name)
			}
		}
		r.Schemas = append(r.Schemas, info)
	}
	r.Ready = len(r.Issues) == 0
	if !r.Ready {
		return r, fmt.Errorf("schema preflight found %d issue(s); see issues", len(r.Issues))
	}
	return r, tx.Commit(ctx)
}

const occupiedSQL = `SELECT EXISTS(SELECT 1 FROM pg_class WHERE relnamespace=to_regnamespace($1)) OR EXISTS(SELECT 1 FROM pg_proc WHERE pronamespace=to_regnamespace($1)) OR EXISTS(SELECT 1 FROM pg_type WHERE typnamespace=to_regnamespace($1)) OR EXISTS(SELECT 1 FROM pg_collation WHERE collnamespace=to_regnamespace($1))`
