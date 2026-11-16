package universestore

import (
	"context"
	"errors"
	"github.com/jackc/pgx/v5"
)

type Object struct {
	UUID, Galaxy, Founder string
	Star, Planet          int
}

// Resolve uses UUID identity while retaining numeric locators for all existing
// ship, colony, discovery, surface and job relationships.
func Resolve(ctx context.Context, q Reader, kind, uuid string, star, planet int) (Object, error) {
	var o Object
	if kind == "system" {
		err := q.QueryRow(ctx, `SELECT i.object_uuid::text,i.galaxy_id,i.id,s.founder FROM system_identity i JOIN star_systems s ON s.galaxy_id=i.galaxy_id AND s.id=i.id WHERE NOT s.deleted AND (($1::uuid IS NOT NULL AND i.object_uuid=$1::uuid) OR ($1::uuid IS NULL AND i.id=$2))`, nullable(uuid), star).Scan(&o.UUID, &o.Galaxy, &o.Star, &o.Founder)
		return o, err
	}
	err := q.QueryRow(ctx, `SELECT p.object_uuid::text,i.galaxy_id,p.system_id,p.id,s.founder FROM system_planets p JOIN system_identity i ON i.id=p.system_id JOIN star_systems s ON s.galaxy_id=i.galaxy_id AND s.id=i.id WHERE NOT s.deleted AND (($1::uuid IS NOT NULL AND p.object_uuid=$1::uuid) OR ($1::uuid IS NULL AND p.system_id=$2 AND p.id=$3))`, nullable(uuid), star, planet).Scan(&o.UUID, &o.Galaxy, &o.Star, &o.Planet, &o.Founder)
	return o, err
}
func nullable(v string) any {
	if v == "" {
		return nil
	}
	return v
}
func Rename(ctx context.Context, tx pgx.Tx, o Object, name string) (int64, error) {
	var count int64
	if o.Planet == 0 {
		tag, e := tx.Exec(ctx, `UPDATE star_systems SET name=$3 WHERE galaxy_id=$1 AND id=$2 AND NOT deleted`, o.Galaxy, o.Star, name)
		if e != nil {
			return 0, e
		}
		count = tag.RowsAffected()
	} else {
		tag, e := tx.Exec(ctx, `UPDATE system_planets SET name=$2 WHERE object_uuid=$1`, o.UUID, name)
		if e != nil {
			return 0, e
		}
		count = tag.RowsAffected()
	}
	if count != 1 {
		return 0, errors.New("rename target changed")
	}
	var revision int64
	e := tx.QueryRow(ctx, `UPDATE universe_state SET revision=revision+1,storage_version=storage_version+1 WHERE id=1 RETURNING revision`).Scan(&revision)
	return revision, e
}
