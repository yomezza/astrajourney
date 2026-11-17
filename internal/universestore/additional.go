package universestore

import (
	"context"
	"github.com/jackc/pgx/v5"
	"github.com/yomezza/cosmosimulator/internal/namedictionary"
	"github.com/yomezza/cosmosimulator/internal/platform/identifiers"
	"github.com/yomezza/cosmosimulator/internal/universe"
)

// PrepareAdditional names natural fields once. Fields have stable identities,
// but intentionally have no player naming entitlement.
func PrepareAdditional(ctx context.Context, tx pgx.Tx, g *universe.Galaxy) error {
	var asteroid, ice namedictionary.Vocabulary
	loaded := false
	for i := range g.Stars {
		s := &g.Stars[i]
		for _, id := range []int{101, 102} {
			if id == 102 && s.ID%2 != 0 {
				continue
			}
			found := false
			for _, o := range s.Objects {
				if o.ID == id {
					found = true
				}
			}
			if found {
				continue
			}
			if !loaded {
				var err error
				asteroid, err = namedictionary.Load(ctx, tx, "asteroid_names")
				if err != nil {
					return err
				}
				ice, err = namedictionary.Load(ctx, tx, "ice_field_names")
				if err != nil {
					return err
				}
				loaded = true
			}
			uuid, err := identifiers.UUID()
			if err != nil {
				return err
			}
			kind, name := "asteroid", asteroid.Name(int64(s.ID-1))
			if id == 102 {
				kind, name = "ice", ice.Name(int64(s.ID-1))
			}
			s.Objects = append(s.Objects, universe.NaturalObject{ID: id, UUID: uuid, Kind: kind, Name: name})
		}
	}
	return nil
}
func loadAdditional(ctx context.Context, q Reader, g *universe.Galaxy) error {
	rows, err := q.Query(ctx, `SELECT system_id,id,object_uuid::text,kind,name FROM system_objects ORDER BY system_id,id`)
	if err != nil {
		return err
	}
	defer rows.Close()
	for rows.Next() {
		var star int
		var o universe.NaturalObject
		if err = rows.Scan(&star, &o.ID, &o.UUID, &o.Kind, &o.Name); err != nil {
			return err
		}
		if star > 0 && star <= len(g.Stars) {
			g.Stars[star-1].Objects = append(g.Stars[star-1].Objects, o)
		}
	}
	return rows.Err()
}
func saveAdditional(ctx context.Context, tx pgx.Tx, g *universe.Galaxy) error {
	batch := &pgx.Batch{}
	for _, s := range g.Stars {
		for _, o := range s.Objects {
			batch.Queue(`INSERT INTO system_objects(system_id,id,object_uuid,kind,name) VALUES($1,$2,$3,$4,$5) ON CONFLICT(system_id,id) DO NOTHING`, s.ID, o.ID, o.UUID, o.Kind, o.Name)
		}
	}
	if batch.Len() == 0 {
		return nil
	}
	return tx.SendBatch(ctx, batch).Close()
}
