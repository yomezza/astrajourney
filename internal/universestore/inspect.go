package universestore

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/yomezza/cosmosimulator/internal/universe"
)

type Report struct {
	Storage           string         `json:"storage"`
	Galaxies          int            `json:"galaxies"`
	Systems           int            `json:"systems"`
	Planets           int            `json:"planets"`
	Lanes             int            `json:"lanes"`
	InvalidReferences map[string]int `json:"invalid_references"`
	Ready             bool           `json:"ready"`
}

// Inspect is enforced read-only by PostgreSQL. It creates no schemas, tables,
// partitions, worlds, admin accounts or sessions and never emits private seeds.
func Inspect(ctx context.Context, db *pgxpool.Pool) (Report, error) {
	report := Report{InvalidReferences: map[string]int{}}
	tx, e := db.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.RepeatableRead, AccessMode: pgx.ReadOnly})
	if e != nil {
		return report, e
	}
	defer tx.Rollback(context.Background())
	var table bool
	if e = tx.QueryRow(ctx, `SELECT to_regclass('universe_state') IS NOT NULL`).Scan(&table); e != nil {
		return report, e
	}
	installed := false
	if table {
		installed, e = Installed(ctx, tx)
		if e != nil {
			return report, e
		}
	}
	var g *universe.Galaxy
	if installed {
		report.Storage = "tabular"
		g, e = Load(ctx, tx)
	} else {
		report.Storage = "legacy-jsonb"
		var raw []byte
		e = tx.QueryRow(ctx, `SELECT data FROM galaxies WHERE name='Edem'`).Scan(&raw)
		if e == nil {
			g = &universe.Galaxy{}
			e = json.Unmarshal(raw, g)
		}
	}
	if e != nil {
		return report, errors.New("universe check: stored world is missing or unreadable")
	}
	// Historical worlds have no catalogue yet; inspect membership without running
	// any generator or changing coordinates. Startup retains the existing upgrades.
	if len(g.Catalog) == 0 {
		seen := map[string]bool{}
		for _, s := range g.Stars {
			k := universe.GalaxyKey(s)
			if !seen[k] {
				g.Catalog = append(g.Catalog, universe.GalaxyDefinition{ID: k})
				seen[k] = true
			}
		}
	}
	if e = Validate(g); e != nil {
		return report, e
	}
	report.Galaxies = len(g.Catalog)
	report.Systems = len(g.Stars)
	report.Lanes = len(g.Lanes)
	ids := []int{}
	pairs := []map[string]int{}
	for _, s := range g.Stars {
		ids = append(ids, s.ID)
		for _, p := range s.Planets {
			report.Planets++
			pairs = append(pairs, map[string]int{"star": s.ID, "planet": p.ID})
		}
	}
	refs := []struct{ table, column string }{{"ships", "star"}, {"ships", "destination"}, {"stations", "star"}, {"surveys", "star"}, {"aj_discovery_objects", "star_id"}}
	for _, r := range refs {
		var exists bool
		if e = tx.QueryRow(ctx, `SELECT to_regclass($1) IS NOT NULL`, r.table).Scan(&exists); e != nil {
			return report, e
		}
		if !exists {
			continue
		}
		var count int
		if e = tx.QueryRow(ctx, "SELECT count(*) FROM "+pgx.Identifier{r.table}.Sanitize()+" WHERE "+pgx.Identifier{r.column}.Sanitize()+" IS NOT NULL AND NOT("+pgx.Identifier{r.column}.Sanitize()+"=ANY($1::int[]))", ids).Scan(&count); e != nil {
			return report, e
		}
		if count > 0 {
			report.InvalidReferences[r.table+"."+r.column] = count
		}
	}
	var surfaces bool
	if e = tx.QueryRow(ctx, `SELECT to_regclass('planet_surfaces') IS NOT NULL`).Scan(&surfaces); e != nil {
		return report, e
	}
	if surfaces {
		var count int
		e = tx.QueryRow(ctx, `SELECT count(*) FROM planet_surfaces s WHERE NOT EXISTS(SELECT 1 FROM jsonb_to_recordset($1) p(star int,planet int) WHERE p.star=s.star AND p.planet=s.planet)`, bytes(pairs)).Scan(&count)
		if e != nil {
			return report, e
		}
		if count > 0 {
			report.InvalidReferences["planet_surfaces.star/planet"] = count
		}
	}
	report.Ready = len(report.InvalidReferences) == 0
	if !report.Ready {
		return report, fmt.Errorf("universe check: invalid object references; migration requires review")
	}
	return report, tx.Commit(ctx)
}
