// Package scenestore is the planetary writer of immutable local snapshots.
package scenestore

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/yomezza/cosmosimulator/internal/geography"
	"github.com/yomezza/cosmosimulator/internal/planet/scene"
	"github.com/yomezza/cosmosimulator/internal/planet/terrain"
	"github.com/yomezza/cosmosimulator/internal/platform/postgres"
)

var ErrFence = errors.New("planetary runtime fence is no longer valid")

type Fence struct {
	Owner  string
	Number int64
}
type Store struct{ db *pgxpool.Pool }

func New(db *pgxpool.Pool) *Store { return &Store{db: db} }

type source struct {
	raw            []byte
	g              geography.PlanetSurface
	seed, checksum string
}

// Snapshot reads are consistent but do not hold world locks during generation.
func readSource(ctx context.Context, q postgres.DBTX, key scene.Key) (*source, error) {
	var s source
	var version int
	err := q.QueryRow(ctx, `SELECT s.data,s.seed,s.version FROM planet_surfaces s
 JOIN planet_runtime_controls c ON c.surface=s.id
 LEFT JOIN planet_surface_controls r ON r.star=s.star AND r.planet=s.planet
 WHERE s.id=$1 AND c.state='enabled' AND c.geography_revision=$3
 AND COALESCE(r.revision,0)=$3 AND NOT COALESCE(r.deleted,false)
 AND EXISTS(SELECT 1 FROM planet_colonies cl JOIN planet_projects p ON p.id=cl.project
 WHERE cl.surface=s.id AND cl.sector=$2 AND p.status='complete')`, key.Surface, key.Sector, key.Revision).Scan(&s.raw, &s.seed, &version)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, scene.ErrSource
	}
	if err != nil {
		return nil, err
	}
	if version != 1 {
		return nil, scene.ErrVersion
	}
	if err = json.Unmarshal(s.raw, &s.g); err != nil || !terrain.Valid(&s.g) {
		return nil, scene.ErrSource
	}
	s.checksum, err = scene.SourceChecksum(&s.g)
	if err != nil {
		return nil, err
	}
	return &s, nil
}

func checkFence(ctx context.Context, q postgres.DBTX, f Fence, lock bool) error {
	sql := `SELECT fence FROM planet_runtime_leases WHERE id=1 AND owner_id=$1 AND fence=$2 AND expires_at>clock_timestamp() AND EXISTS(SELECT 1 FROM planet_schema_state WHERE id=1 AND version=8)`
	if lock {
		sql += ` FOR SHARE`
	}
	var n int64
	err := q.QueryRow(ctx, sql, f.Owner, f.Number).Scan(&n)
	if errors.Is(err, pgx.ErrNoRows) {
		return ErrFence
	}
	return err
}

func readStored(ctx context.Context, q postgres.DBTX, key scene.Key) (*scene.Scene, error) {
	var gen, format, policy int
	var packed, hash []byte
	var id, source, seed string
	err := q.QueryRow(ctx, `SELECT generator_version,format_version,walk_policy_version,location_id,source_checksum,decor_seed,geometry,geometry_checksum FROM planet_local_locations WHERE surface=$1 AND sector=$2 AND revision=$3`, key.Surface, key.Sector, key.Revision).Scan(&gen, &format, &policy, &id, &source, &seed, &packed, &hash)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	if !scene.ReadableGenerator(gen) || format != 1 || policy != 1 {
		return nil, scene.ErrVersion
	}
	if len(hash) != 32 {
		return nil, scene.ErrCorrupt
	}
	s, err := scene.Decode(packed, [32]byte(hash))
	if err != nil {
		return nil, err
	}
	m := s.Metadata()
	if m.Key != key || m.ID != id || m.SourceChecksum != source || m.Seed != seed {
		return nil, scene.ErrCorrupt
	}
	return s, nil
}

func checkSites(ctx context.Context, q postgres.DBTX, s *scene.Scene) error {
	k := s.Key()
	rows, err := q.Query(ctx, `SELECT geography_revision,layout_version,x_q,y_q,spawn_x_q,spawn_y_q FROM planet_landing_sites WHERE surface=$1 AND sector=$2 AND enabled ORDER BY id`, k.Surface, k.Sector)
	if err != nil {
		return err
	}
	defer rows.Close()
	for rows.Next() {
		var rev int64
		var version int
		var p terrain.Placement
		if err = rows.Scan(&rev, &version, &p.X, &p.Y, &p.SpawnX, &p.SpawnY); err != nil {
			return err
		}
		if rev != k.Revision || version != terrain.PolicyVersion {
			return scene.ErrLayout
		}
		if err = s.ValidateLanding(p); err != nil {
			return err
		}
	}
	return rows.Err()
}

// Load returns the saved version first. It never replaces a bad, old or unknown
// snapshot with freshly generated scenery. Generation holds no SQL transaction.
func (r *Store) Load(ctx context.Context, key scene.Key, f Fence) (*scene.Scene, error) {
	if err := checkFence(ctx, r.db, f, false); err != nil {
		return nil, err
	}
	tx, err := r.db.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.RepeatableRead, AccessMode: pgx.ReadOnly})
	if err != nil {
		return nil, err
	}
	defer tx.Rollback(context.Background())
	src, err := readSource(ctx, tx, key)
	if err != nil {
		return nil, err
	}
	s, err := readStored(ctx, tx, key)
	if err != nil {
		return nil, err
	}
	if s != nil {
		if s.SourceChecksum() != src.checksum {
			return nil, scene.ErrSource
		}
		if err = checkSites(ctx, tx, s); err != nil {
			return nil, err
		}
	}
	if err = tx.Commit(ctx); err != nil {
		return nil, err
	}
	if s != nil {
		if err = r.Validate(ctx, s, f); err != nil {
			return nil, err
		}
		return s, nil
	}
	seed := sha256.Sum256([]byte(fmt.Sprintf("%s/local-v%d/%d/%d/%d", src.seed, scene.GeneratorVersion, key.Surface, key.Sector, key.Revision)))
	s, err = scene.Generate(ctx, key, &src.g, hex.EncodeToString(seed[:]))
	if err != nil {
		return nil, err
	}
	return r.save(ctx, src, s, f)
}

func (r *Store) save(ctx context.Context, src *source, s *scene.Scene, f Fence) (*scene.Scene, error) {
	packed, checksum, err := scene.Encode(s)
	if err != nil {
		return nil, err
	}
	tx, err := r.db.Begin(ctx)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback(context.Background())
	// Same order as cosmic resets: world -> lease. Heavy generation is already done.
	if _, err = tx.Exec(ctx, `SELECT pg_advisory_xact_lock_shared(784218)`); err != nil {
		return nil, err
	}
	if err = checkFence(ctx, tx, f, true); err != nil {
		return nil, err
	}
	k := s.Key()
	var valid bool
	err = tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM planet_surfaces s JOIN planet_runtime_controls c ON c.surface=s.id LEFT JOIN planet_surface_controls r ON r.star=s.star AND r.planet=s.planet WHERE s.id=$1 AND s.data=$4::jsonb AND s.seed=$5 AND s.version=1 AND c.state='enabled' AND c.geography_revision=$3 AND COALESCE(r.revision,0)=$3 AND NOT COALESCE(r.deleted,false) AND EXISTS(SELECT 1 FROM planet_colonies cl JOIN planet_projects p ON p.id=cl.project WHERE cl.surface=s.id AND cl.sector=$2 AND p.status='complete'))`, k.Surface, k.Sector, k.Revision, src.raw, src.seed).Scan(&valid)
	if err != nil {
		return nil, err
	}
	if !valid {
		return nil, scene.ErrSource
	}
	if err = checkSites(ctx, tx, s); err != nil {
		return nil, err
	}
	m := s.Metadata()
	_, err = tx.Exec(ctx, `INSERT INTO planet_local_locations(surface,sector,revision,generator_version,format_version,walk_policy_version,location_id,source_checksum,decor_seed,geometry,geometry_checksum) VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11) ON CONFLICT DO NOTHING`, k.Surface, k.Sector, k.Revision, m.Generator, scene.FormatVersion, m.Policy, m.ID, m.SourceChecksum, m.Seed, packed, checksum[:])
	if err != nil {
		return nil, err
	}
	// A concurrent loader's committed snapshot is authoritative, not our candidate.
	stored, err := readStored(ctx, tx, k)
	if err != nil {
		return nil, err
	}
	if stored == nil || stored.SourceChecksum() != src.checksum {
		return nil, scene.ErrSource
	}
	if err = checkSites(ctx, tx, stored); err != nil {
		return nil, err
	}
	if err = tx.Commit(ctx); err != nil {
		return nil, err
	}
	return stored, nil
}

// Validate must also be called for a warm pin; RAM cannot authorize stale source,
// new landing layouts or a predecessor's runtime lease.
func (r *Store) Validate(ctx context.Context, s *scene.Scene, f Fence) error {
	tx, err := r.db.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.RepeatableRead, AccessMode: pgx.ReadOnly})
	if err != nil {
		return err
	}
	defer tx.Rollback(context.Background())
	if err = checkFence(ctx, tx, f, false); err != nil {
		return err
	}
	src, err := readSource(ctx, tx, s.Key())
	if err != nil {
		return err
	}
	if src.checksum != s.SourceChecksum() {
		return scene.ErrSource
	}
	if err = checkSites(ctx, tx, s); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

// Retired surfaces are removed by their own writer after the registry has drained
// references. Cosmic owns the retirement request and later deletes global data.
func (r *Store) Retired(ctx context.Context) ([]int64, error) {
	rows, err := r.db.Query(ctx, `SELECT surface FROM planet_runtime_controls WHERE state='retiring' ORDER BY surface`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var ids []int64
	for rows.Next() {
		var id int64
		if err = rows.Scan(&id); err != nil {
			return nil, err
		}
		ids = append(ids, id)
	}
	return ids, rows.Err()
}
func (r *Store) PurgeRetired(ctx context.Context, surface int64, f Fence) error {
	tx, err := r.db.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(context.Background())
	if _, err = tx.Exec(ctx, `SELECT pg_advisory_xact_lock_shared(784218)`); err != nil {
		return err
	}
	if err = checkFence(ctx, tx, f, true); err != nil {
		return err
	}
	_, err = tx.Exec(ctx, `UPDATE planet_characters SET last_surface=NULL,last_sector=NULL,last_revision=NULL WHERE last_surface=$1 AND EXISTS(SELECT 1 FROM planet_runtime_controls WHERE surface=$1 AND state='retiring')`, surface)
	if err != nil {
		return err
	}
	_, err = tx.Exec(ctx, `DELETE FROM planet_handoff_uses u USING planet_handoffs h,planet_runtime_controls c WHERE u.handoff_id=h.id AND h.surface=$1 AND c.surface=h.surface AND c.state='retiring'`, surface)
	if err != nil {
		return err
	}
	_, err = tx.Exec(ctx, `DELETE FROM planet_local_locations l USING planet_runtime_controls c WHERE l.surface=$1 AND c.surface=l.surface AND c.state='retiring'`, surface)
	if err != nil {
		return err
	}
	return tx.Commit(ctx)
}

// CheckSource uses the caller transaction for a fresh entry commit check.
func CheckSource(ctx context.Context, q postgres.DBTX, s *scene.Scene) error {
	src, err := readSource(ctx, q, s.Key())
	if err != nil {
		return err
	}
	if src.checksum != s.SourceChecksum() {
		return scene.ErrSource
	}
	return checkSites(ctx, q, s)
}
