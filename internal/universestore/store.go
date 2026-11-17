// Package universestore stores systems/planets as rows; JSON holds object details.
// All mutations use the caller's transaction and never initialize a second pool.
package universestore

import (
	"context"
	"crypto/sha256"
	_ "embed"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"reflect"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/yomezza/cosmosimulator/internal/platform/identifiers"
	"github.com/yomezza/cosmosimulator/internal/universe"
)

//go:embed schema.sql
var Schema string

type Reader interface {
	QueryRow(context.Context, string, ...any) pgx.Row
	Query(context.Context, string, ...any) (pgx.Rows, error)
}

func Installed(ctx context.Context, q interface {
	QueryRow(context.Context, string, ...any) pgx.Row
}) (bool, error) {
	var yes bool
	err := q.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM universe_state WHERE id=1)`).Scan(&yes)
	return yes, err
}
func Version(ctx context.Context, q interface {
	QueryRow(context.Context, string, ...any) pgx.Row
}) (string, error) {
	var v string
	err := q.QueryRow(ctx, `SELECT storage_version::text FROM universe_state WHERE id=1 AND format_version=1`).Scan(&v)
	return v, err
}

// Snapshot gives ordinary pool readers one consistent MVCC snapshot. Existing
// transactions keep their connection and isolation/world-lock contract.
func Snapshot(ctx context.Context, q any) (*universe.Galaxy, string, error) {
	if pool, ok := q.(*pgxpool.Pool); ok {
		tx, e := pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.RepeatableRead, AccessMode: pgx.ReadOnly})
		if e != nil {
			return nil, "", e
		}
		defer tx.Rollback(context.Background())
		g, v, e := load(ctx, tx)
		if e == nil {
			e = tx.Commit(ctx)
		}
		return g, v, e
	}
	reader, ok := q.(Reader)
	if !ok {
		return nil, "", errors.New("universe reader must support Query and QueryRow")
	}
	return load(ctx, reader)
}
func Load(ctx context.Context, q Reader) (*universe.Galaxy, error) {
	g, _, e := load(ctx, q)
	return g, e
}

// Read-committed caller transactions may span a concurrent world commit. Retry
// mixed reads using the monotonically incremented token, without acquiring a
// second connection or introducing a new lock order into gameplay transactions.
func load(ctx context.Context, q Reader) (*universe.Galaxy, string, error) {
	for {
		if e := ctx.Err(); e != nil {
			return nil, "", e
		}
		before, e := Version(ctx, q)
		if e != nil {
			return nil, "", e
		}
		g, v, e := loadOnce(ctx, q)
		after, ve := Version(ctx, q)
		if ve != nil {
			return nil, "", ve
		}
		if before != after || v != "" && v != before {
			continue
		}
		return g, v, e
	}
}
func loadOnce(ctx context.Context, q Reader) (*universe.Galaxy, string, error) {
	g := &universe.Galaxy{}
	var version string
	err := q.QueryRow(ctx, `SELECT name,seed,generator_version,revision,discovery_secret,settlement_version,ring_layout_version,naming_version,storage_version::text FROM universe_state WHERE id=1 AND format_version=1`).Scan(&g.Name, &g.Seed, &g.Version, &g.Revision, &g.DiscoverySecret, &g.SettlementVersion, &g.RingLayoutVersion, &g.NamingVersion, &version)
	if err != nil {
		return nil, "", err
	}
	rows, err := q.Query(ctx, `SELECT id,name,shape,seed,opened,deleted,region_base,radius,extra_lanes,cross_links,max_lane_length,station_percent,max_ports,resource_weights,territories,empty FROM galaxy_catalog ORDER BY ordinal`)
	if err != nil {
		return nil, "", err
	}
	for rows.Next() {
		var d universe.GalaxyDefinition
		var weights, territories []byte
		if err = rows.Scan(&d.ID, &d.Name, &d.Shape, &d.Seed, &d.Opened, &d.Deleted, &d.RegionBase, &d.Radius, &d.Settings.ExtraLanes, &d.Settings.CrossLinks, &d.Settings.MaxLaneLength, &d.Settings.StationPercent, &d.Settings.MaxPorts, &weights, &territories, &d.Empty); err != nil {
			break
		}
		if err = json.Unmarshal(weights, &d.Settings.ResourceWeights); err != nil {
			break
		}
		if err = json.Unmarshal(territories, &d.Territories); err != nil {
			break
		}
		g.Catalog = append(g.Catalog, d)
	}
	e := rows.Err()
	rows.Close()
	if err != nil {
		return nil, "", err
	}
	if e != nil {
		return nil, "", e
	}
	rows, err = q.Query(ctx, `SELECT id,name,specialty FROM universe_regions ORDER BY ordinal`)
	if err != nil {
		return nil, "", err
	}
	for rows.Next() {
		var r universe.Region
		if err = rows.Scan(&r.ID, &r.Name, &r.Specialty); err != nil {
			break
		}
		g.Regions = append(g.Regions, r)
	}
	e = rows.Err()
	rows.Close()
	if err != nil {
		return nil, "", err
	}
	if e != nil {
		return nil, "", e
	}
	rows, err = q.Query(ctx, `SELECT s.id,i.object_uuid::text,s.galaxy_id,s.local_number,s.name,s.catalog_code,s.x,s.y,s.region,s.class,s.deleted,s.mass,s.luminosity,s.founder,s.ports,s.details FROM star_systems s JOIN system_identity i ON i.galaxy_id=s.galaxy_id AND i.id=s.id ORDER BY s.id`)
	if err != nil {
		return nil, "", err
	}
	for rows.Next() {
		var s universe.Star
		var raw []byte
		if err = rows.Scan(&s.ID, &s.UUID, &s.Galaxy, &s.Number, &s.Name, &s.CatalogCode, &s.X, &s.Y, &s.Region, &s.Class, &s.Deleted, &s.Mass, &s.Luminosity, &s.Founder, &s.Ports, &raw); err != nil {
			break
		}
		var d starDetails
		if err = json.Unmarshal(raw, &d); err != nil {
			break
		}
		s.DiscoverySeed = d.DiscoverySeed
		s.ResourceRolls = d.ResourceRolls
		s.ResourceWeights = d.ResourceWeights
		if s.ID != len(g.Stars)+1 {
			err = errors.New("universe: missing stable system ID; refusing to renumber")
			break
		}
		g.Stars = append(g.Stars, s)
	}
	e = rows.Err()
	rows.Close()
	if err != nil {
		return nil, "", err
	}
	if e != nil {
		return nil, "", e
	}
	rows, err = q.Query(ctx, `SELECT system_id,id,object_uuid::text,name,catalog_code,kind,orbit_au,mass,gravity,temperature,details FROM system_planets ORDER BY system_id,ordinal`)
	if err != nil {
		return nil, "", err
	}
	for rows.Next() {
		var p universe.Planet
		var star int
		var raw []byte
		if err = rows.Scan(&star, &p.ID, &p.UUID, &p.Name, &p.CatalogCode, &p.Kind, &p.AU, &p.Mass, &p.Gravity, &p.Temperature, &raw); err != nil {
			break
		}
		var d planetDetails
		if err = json.Unmarshal(raw, &d); err != nil {
			break
		}
		p.Atmosphere = d.Atmosphere
		p.Resources = d.Resources
		if star < 1 || star > len(g.Stars) {
			err = errors.New("universe: orphan planet")
			break
		}
		g.Stars[star-1].Planets = append(g.Stars[star-1].Planets, p)
	}
	e = rows.Err()
	rows.Close()
	if err != nil {
		return nil, "", err
	}
	if e != nil {
		return nil, "", e
	}
	rows, err = q.Query(ctx, `SELECT from_system,to_system FROM universe_lanes ORDER BY ordinal`)
	if err != nil {
		return nil, "", err
	}
	g.Lanes = []universe.Lane{}
	for rows.Next() {
		var l universe.Lane
		if err = rows.Scan(&l.From, &l.To); err != nil {
			break
		}
		g.Lanes = append(g.Lanes, l)
	}
	e = rows.Err()
	rows.Close()
	if err != nil {
		return nil, "", err
	}
	if e == nil {
		e = loadAdditional(ctx, q, g)
	}
	return g, version, e
}

type starDetails struct {
	DiscoverySeed   string                        `json:"discovery_seed,omitempty"`
	ResourceRolls   map[string]map[string]float64 `json:"resource_rolls,omitempty"`
	ResourceWeights map[string]int                `json:"resource_weights,omitempty"`
}
type planetDetails struct {
	Atmosphere string   `json:"atmosphere"`
	Resources  []string `json:"resources"`
}

func bytes(v any) []byte { b, _ := json.Marshal(v); return b }
func Validate(g *universe.Galaxy) error {
	galaxies := map[string]bool{}
	for _, d := range g.Catalog {
		if d.ID == "" || len(d.ID) > 80 || galaxies[d.ID] {
			return errors.New("universe: invalid/duplicate galaxy ID")
		}
		galaxies[d.ID] = true
	}
	for i, s := range g.Stars {
		if s.ID != i+1 || !galaxies[universe.GalaxyKey(s)] || math.IsNaN(s.X+s.Y) || math.IsInf(s.X+s.Y, 0) {
			return fmt.Errorf("universe: invalid system %d; IDs must be retained", s.ID)
		}
		ids := map[int]bool{}
		for _, p := range s.Planets {
			if p.ID < 1 || ids[p.ID] {
				return fmt.Errorf("universe: duplicate/invalid planet in system %d", s.ID)
			}
			ids[p.ID] = true
		}
	}
	lanes := map[universe.Lane]bool{}
	for _, l := range g.Lanes {
		if l.From < 1 || l.To < 1 || l.From > len(g.Stars) || l.To > len(g.Stars) || l.From == l.To || lanes[l] {
			return errors.New("universe: invalid/duplicate lane")
		}
		lanes[l] = true
	}
	return nil
}
func partition(ctx context.Context, tx pgx.Tx, id string) error {
	var schema string
	if e := tx.QueryRow(ctx, `SELECT n.nspname FROM pg_class c JOIN pg_namespace n ON n.oid=c.relnamespace WHERE c.oid='star_systems'::regclass`).Scan(&schema); e != nil {
		return e
	}
	hash := sha256.Sum256([]byte(id))
	name := "star_systems_p_" + hex.EncodeToString(hash[:8])
	qualified := pgx.Identifier{schema, name}.Sanitize()
	var exists bool
	if e := tx.QueryRow(ctx, `SELECT to_regclass($1) IS NOT NULL`, qualified).Scan(&exists); e != nil {
		return e
	}
	if exists {
		var child bool
		if e := tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM pg_inherits WHERE inhrelid=to_regclass($1) AND inhparent='star_systems'::regclass)`, qualified).Scan(&child); e != nil {
			return e
		}
		if !child {
			return errors.New("universe partition name is occupied by another relation")
		}
		return nil
	}
	var literal string
	if e := tx.QueryRow(ctx, `SELECT quote_literal($1::text)`, id).Scan(&literal); e != nil {
		return e
	}
	_, e := tx.Exec(ctx, "CREATE TABLE "+qualified+" PARTITION OF "+pgx.Identifier{schema, "star_systems"}.Sanitize()+" FOR VALUES IN ("+literal+")")
	return e
}

// Save writes changed rows only. Used by rare bulk galaxy administration and
// migration; interactive renames use Rename below and never load the full world.
func Save(ctx context.Context, tx pgx.Tx, g *universe.Galaxy) error {
	if e := Validate(g); e != nil {
		return e
	}
	if _, e := tx.Exec(ctx, `SELECT pg_advisory_xact_lock(784218)`); e != nil {
		return e
	}
	installed, e := Installed(ctx, tx)
	if e != nil {
		return e
	}
	var old *universe.Galaxy
	if installed {
		old, e = Load(ctx, tx)
		if e != nil {
			return e
		}
		if len(g.Stars) < len(old.Stars) {
			return errors.New("universe: physical system removal forbidden; retain deleted tombstones")
		}
	}
	for i, d := range g.Catalog {
		if old != nil && i < len(old.Catalog) && reflect.DeepEqual(old.Catalog[i], d) {
			continue
		}
		_, e = tx.Exec(ctx, `INSERT INTO galaxy_catalog(id,ordinal,name,shape,seed,opened,deleted,region_base,radius,extra_lanes,cross_links,max_lane_length,station_percent,max_ports,resource_weights,territories,empty) VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14,$15,$16,$17) ON CONFLICT(id) DO UPDATE SET ordinal=excluded.ordinal,name=excluded.name,shape=excluded.shape,seed=excluded.seed,opened=excluded.opened,deleted=excluded.deleted,region_base=excluded.region_base,radius=excluded.radius,extra_lanes=excluded.extra_lanes,cross_links=excluded.cross_links,max_lane_length=excluded.max_lane_length,station_percent=excluded.station_percent,max_ports=excluded.max_ports,resource_weights=excluded.resource_weights,territories=excluded.territories,empty=excluded.empty`, d.ID, i, d.Name, d.Shape, d.Seed, d.Opened, d.Deleted, d.RegionBase, d.Radius, d.Settings.ExtraLanes, d.Settings.CrossLinks, d.Settings.MaxLaneLength, d.Settings.StationPercent, d.Settings.MaxPorts, bytes(d.Settings.ResourceWeights), bytes(d.Territories), d.Empty)
		if e != nil {
			return e
		}
		if e = partition(ctx, tx, d.ID); e != nil {
			return e
		}
	}
	batch := &pgx.Batch{}
	flush := func() error {
		if batch.Len() == 0 {
			return nil
		}
		e := tx.SendBatch(ctx, batch).Close()
		batch = &pgx.Batch{}
		return e
	}
	for i := range g.Stars {
		if batch.Len() >= 512 {
			if e = flush(); e != nil {
				return e
			}
		}
		s := &g.Stars[i]
		s.Galaxy = universe.GalaxyKey(*s)
		var prior *universe.Star
		if old != nil && i < len(old.Stars) {
			prior = &old.Stars[i]
			if universe.GalaxyKey(*prior) != universe.GalaxyKey(*s) {
				return errors.New("universe: moving an existing system between galaxies requires explicit migration")
			}
			if s.UUID != "" && s.UUID != prior.UUID {
				return errors.New("universe: system UUID is immutable")
			}
			s.UUID = prior.UUID
		}
		if s.UUID == "" {
			s.UUID, e = identifiers.UUID()
			if e != nil {
				return e
			}
		}
		for j := range s.Planets {
			p := &s.Planets[j]
			if prior != nil {
				for _, op := range prior.Planets {
					if op.ID == p.ID {
						if p.UUID != "" && p.UUID != op.UUID {
							return errors.New("universe: planet UUID is immutable")
						}
						p.UUID = op.UUID
						break
					}
				}
			}
			if p.UUID == "" {
				p.UUID, e = identifiers.UUID()
				if e != nil {
					return e
				}
			}
		}
		if prior != nil && reflect.DeepEqual(*prior, *s) {
			continue
		}
		headerChanged := prior == nil
		if prior != nil {
			before, after := *prior, *s
			before.Planets = nil
			before.Objects = nil
			after.Planets = nil
			after.Objects = nil
			headerChanged = !reflect.DeepEqual(before, after)
		}
		if headerChanged {
			batch.Queue(`INSERT INTO system_identity(id,object_uuid,galaxy_id) VALUES($1,$2,$3) ON CONFLICT(id) DO NOTHING`, s.ID, s.UUID, universe.GalaxyKey(*s))
			batch.Queue(`INSERT INTO star_systems(galaxy_id,id,local_number,name,catalog_code,x,y,region,class,deleted,mass,luminosity,founder,ports,details) VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14,$15) ON CONFLICT(galaxy_id,id) DO UPDATE SET local_number=excluded.local_number,name=excluded.name,catalog_code=excluded.catalog_code,x=excluded.x,y=excluded.y,region=excluded.region,class=excluded.class,deleted=excluded.deleted,mass=excluded.mass,luminosity=excluded.luminosity,founder=excluded.founder,ports=excluded.ports,details=star_systems.details||excluded.details`, universe.GalaxyKey(*s), s.ID, s.Number, s.Name, s.CatalogCode, s.X, s.Y, s.Region, s.Class, s.Deleted, s.Mass, s.Luminosity, s.Founder, s.Ports, bytes(starDetails{s.DiscoverySeed, s.ResourceRolls, s.ResourceWeights}))
		}
		priorPlanets := map[int]universe.Planet{}
		if prior != nil {
			for _, p := range prior.Planets {
				priorPlanets[p.ID] = p
			}
		}
		reorder := prior != nil && len(prior.Planets) != len(s.Planets)
		if prior != nil && !reorder {
			for j, p := range s.Planets {
				if prior.Planets[j].ID != p.ID {
					reorder = true
					break
				}
			}
		}
		if reorder {
			batch.Queue(`UPDATE system_planets SET ordinal=ordinal+100000 WHERE system_id=$1`, s.ID)
		}
		keep := []int{}
		for j, p := range s.Planets {
			keep = append(keep, p.ID)
			if op, ok := priorPlanets[p.ID]; ok && !reorder && reflect.DeepEqual(op, p) {
				continue
			}
			batch.Queue(`INSERT INTO system_planets(system_id,id,object_uuid,ordinal,name,catalog_code,kind,orbit_au,mass,gravity,temperature,details) VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12) ON CONFLICT(system_id,id) DO UPDATE SET ordinal=excluded.ordinal,name=excluded.name,catalog_code=excluded.catalog_code,kind=excluded.kind,orbit_au=excluded.orbit_au,mass=excluded.mass,gravity=excluded.gravity,temperature=excluded.temperature,details=system_planets.details||excluded.details`, s.ID, p.ID, p.UUID, j, p.Name, p.CatalogCode, p.Kind, p.AU, p.Mass, p.Gravity, p.Temperature, bytes(planetDetails{p.Atmosphere, p.Resources}))
		}
		// Generation/admin edits may replace undiscovered bodies. Existing surfaces
		// must never be orphaned, even when the legacy writer allowed a whole rewrite.
		if prior != nil && len(keep) < len(prior.Planets) {
			batch.Queue(`DELETE FROM system_planets WHERE system_id=$1 AND NOT(id=ANY($2::int[]))`, s.ID, keep)
		}
	}
	if e = flush(); e != nil {
		return e
	}
	if e = saveAdditional(ctx, tx, g); e != nil {
		return e
	}
	if old == nil || !reflect.DeepEqual(old.Regions, g.Regions) {
		if _, e = tx.Exec(ctx, `DELETE FROM universe_regions`); e != nil {
			return e
		}
		for i, r := range g.Regions {
			if _, e = tx.Exec(ctx, `INSERT INTO universe_regions VALUES($1,$2,$3,$4)`, i, r.ID, r.Name, r.Specialty); e != nil {
				return e
			}
		}
	}
	if old == nil || !reflect.DeepEqual(old.Lanes, g.Lanes) {
		if _, e = tx.Exec(ctx, `DELETE FROM universe_lanes`); e != nil {
			return e
		}
		b := &pgx.Batch{}
		for i, l := range g.Lanes {
			b.Queue(`INSERT INTO universe_lanes VALUES($1,$2,$3)`, i, l.From, l.To)
		}
		if b.Len() > 0 {
			if e = tx.SendBatch(ctx, b).Close(); e != nil {
				return e
			}
		}
	}
	_, e = tx.Exec(ctx, `INSERT INTO universe_state(id,name,seed,generator_version,revision,discovery_secret,settlement_version,ring_layout_version,naming_version) VALUES(1,$1,$2,$3,$4,$5,$6,$7,$8) ON CONFLICT(id) DO UPDATE SET name=excluded.name,seed=excluded.seed,generator_version=excluded.generator_version,revision=excluded.revision,discovery_secret=excluded.discovery_secret,settlement_version=excluded.settlement_version,ring_layout_version=excluded.ring_layout_version,naming_version=excluded.naming_version,storage_version=universe_state.storage_version+1`, g.Name, g.Seed, g.Version, g.Revision, g.DiscoverySecret, g.SettlementVersion, g.RingLayoutVersion, g.NamingVersion)
	return e
}

// Verify compares the migrated representation with the exact upgraded source,
// before COMMIT. Keep the legacy document untouched and record its digest.
func Verify(ctx context.Context, tx pgx.Tx, expected *universe.Galaxy, legacy []byte) error {
	actual, e := Load(ctx, tx)
	if e != nil {
		return e
	}
	// Nil and empty slices have the same world semantics, but the legacy DTO omits
	// some empty collections. Compare canonical JSON after normalizing membership.
	copy := *expected
	copy.Stars = append([]universe.Star(nil), expected.Stars...)
	for i := range copy.Stars {
		copy.Stars[i].Galaxy = universe.GalaxyKey(copy.Stars[i])
	}
	if string(bytes(&copy)) != string(bytes(actual)) {
		return errors.New("tabular universe round-trip mismatch; migration rolled back")
	}
	hash := sha256.Sum256(legacy)
	_, e = tx.Exec(ctx, `UPDATE universe_state SET legacy_sha256=$1 WHERE id=1`, hex.EncodeToString(hash[:]))
	return e
}
