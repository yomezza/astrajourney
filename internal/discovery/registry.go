// Package discovery owns first-discovery records and global naming rights.
package discovery

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/jackc/pgx/v5"
	"github.com/yomezza/cosmosimulator/internal/platform/naming"
	"github.com/yomezza/cosmosimulator/internal/platform/postgres"
	"github.com/yomezza/cosmosimulator/internal/universe"
)

// Sync is called by every world writer, under the existing world transaction
// lock. Existing names are authoritative; new records receive no invented owner.
func Sync(ctx context.Context, tx pgx.Tx, g *universe.Galaxy) error {
	rows, err := tx.Query(ctx, `SELECT kind,star_id,planet_id,current_name,name_key FROM aj_discovery_objects`)
	if err != nil {
		return err
	}
	type oldName struct{ name, key string }
	old := map[string]oldName{}
	key := func(kind string, s, p int) string { return fmt.Sprintf("%s/%d/%d", kind, s, p) }
	for rows.Next() {
		var k, n, nk string
		var s, p int
		if err = rows.Scan(&k, &s, &p, &n, &nk); err != nil {
			rows.Close()
			return err
		}
		old[key(k, s, p)] = oldName{n, nk}
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return err
	}
	records := []map[string]any{}
	unique := map[string]bool{}
	add := func(kind string, s universe.Star, p int, name *string) error {
		code := fmt.Sprintf("%s:star-%d", universe.GalaxyKey(s), s.ID)
		if p > 0 {
			code += fmt.Sprintf(":planet-%d", p)
		}
		original := *name
		nk := naming.Key(*name)
		if v, ok := old[key(kind, s.ID, p)]; ok {
			*name = v.name
			nk = v.key
		}
		scope := fmt.Sprintf("planet/%d/%s", s.ID, nk)
		if kind == "system" {
			scope = "system/" + universe.GalaxyKey(s) + "/" + nk
		}
		if !s.Deleted && unique[scope] {
			return fmt.Errorf("discovery preflight: duplicate canonical name in %s (object %s)", scope, code)
		}
		if !s.Deleted {
			unique[scope] = true
		}
		records = append(records, map[string]any{"kind": kind, "star": s.ID, "planet": p, "galaxy": universe.GalaxyKey(s), "code": code, "original": original, "name": *name, "key": nk, "founder": s.Founder, "deleted": s.Deleted})
		return nil
	}
	for i := range g.Stars {
		s := &g.Stars[i]
		s.CatalogCode = fmt.Sprintf("%s:star-%d", universe.GalaxyKey(*s), s.ID)
		if err = add("system", *s, 0, &s.Name); err != nil {
			return err
		}
		for j := range s.Planets {
			s.Planets[j].CatalogCode = fmt.Sprintf("%s:planet-%d", s.CatalogCode, s.Planets[j].ID)
			if err = add("planet", *s, s.Planets[j].ID, &s.Planets[j].Name); err != nil {
				return err
			}
		}
	}
	raw, err := json.Marshal(records)
	if err != nil {
		return err
	}
	_, err = tx.Exec(ctx, `INSERT INTO aj_discovery_objects(kind,star_id,planet_id,galaxy_key,catalog_code,original_name,current_name,name_key,claim_state,protected_founder_key)
 SELECT x.kind,x.star,x.planet,x.galaxy,x.code,x.original,x.name,x.key,
 CASE WHEN x.deleted THEN 'retired' WHEN x.founder<>'' THEN 'protected'
 WHEN EXISTS(SELECT 1 FROM surveys v WHERE v.star=x.star)
 OR (x.kind='planet' AND (EXISTS(SELECT 1 FROM research_jobs j WHERE j.star=x.star AND j.scanned>=x.planet)
 OR EXISTS(SELECT 1 FROM planet_surfaces ps WHERE ps.star=x.star AND ps.planet=x.planet))) THEN 'legacy_unattributed' ELSE 'unclaimed' END,NULLIF(x.founder,'')
 FROM jsonb_to_recordset($1) x(kind text,star int,planet int,galaxy text,code text,original text,name text,key text,founder text,deleted boolean)
 ON CONFLICT(kind,star_id,planet_id) DO UPDATE SET
 protected_founder_key=COALESCE(aj_discovery_objects.protected_founder_key,EXCLUDED.protected_founder_key),
 claim_state=CASE WHEN EXCLUDED.claim_state='retired' THEN 'retired' WHEN aj_discovery_objects.claim_state='unclaimed' AND EXCLUDED.protected_founder_key IS NOT NULL THEN 'protected' ELSE aj_discovery_objects.claim_state END`, raw)
	return err
}

// ClaimScan belongs to the same transaction that advances the personal scan.
// Corporate visibility and purchased surveys never call this function.
func ClaimScan(ctx context.Context, tx pgx.Tx, uid int64, s universe.Star, from, to int, completed bool) error {
	if s.Deleted || s.Founder != "" {
		return nil
	}
	sites := universe.SystemSites(s)
	for _, site := range sites[max(0, min(from, len(sites))):max(0, min(to, len(sites)))] {
		if site.Details == nil {
			continue
		}
		if _, err := tx.Exec(ctx, `UPDATE aj_discovery_objects SET claim_state='claimed',discoverer_id=$1,discovered_at=clock_timestamp(),claim_source='planet_scan',updated_at=clock_timestamp() WHERE kind='planet' AND star_id=$2 AND planet_id=$3 AND claim_state='unclaimed' AND protected_founder_key IS NULL AND EXISTS(SELECT 1 FROM users WHERE id=$1 AND NOT is_bot)`, uid, s.ID, site.Planet); err != nil {
			return err
		}
	}
	if completed {
		_, err := tx.Exec(ctx, `UPDATE aj_discovery_objects SET claim_state='claimed',discoverer_id=$1,discovered_at=clock_timestamp(),claim_source='survey',updated_at=clock_timestamp() WHERE kind='system' AND star_id=$2 AND planet_id=0 AND claim_state='unclaimed' AND protected_founder_key IS NULL AND EXISTS(SELECT 1 FROM users WHERE id=$1 AND NOT is_bot)`, uid, s.ID)
		return err
	}
	return nil
}

func Metadata(ctx context.Context, q interface {
	Query(context.Context, string, ...any) (pgx.Rows, error)
}, uid int64, star int, planets []int) ([]map[string]any, error) {
	return postgres.QueryMaps(ctx, q, `SELECT o.kind,o.star_id,o.planet_id,o.catalog_code,o.original_name,o.current_name,o.claim_state,o.discovered_at,o.name_revision,o.protected_founder_key,COALESCE(u.username,'') AS discoverer,COALESCE((o.discoverer_id=$1 AND o.claim_state='claimed' AND o.rename_used_at IS NULL AND o.protected_founder_key IS NULL),false) AS rename_available FROM aj_discovery_objects o LEFT JOIN users u ON u.id=o.discoverer_id WHERE o.star_id=$2 AND (o.kind='system' OR o.planet_id=ANY($3::int[])) ORDER BY o.planet_id`, uid, star, planets)
}
