package universestore

import (
	"context"
	"fmt"
	"github.com/jackc/pgx/v5"
)

// InstallReferences runs only in Cosmic's startup transaction, after backfill.
// NOT VALID + VALIDATE reports a bad legacy reference before the cutover commits.
func InstallReferences(ctx context.Context, tx pgx.Tx) error {
	refs := []struct{ table, name, columns, target string }{
		{"ships", "universe_ship_system_fk", "star", "system_identity(id)"},
		{"ships", "universe_ship_destination_fk", "destination", "system_identity(id)"},
		{"stations", "universe_station_system_fk", "star", "system_identity(id)"},
		{"surveys", "universe_survey_system_fk", "star", "system_identity(id)"},
		{"planet_surfaces", "universe_surface_planet_fk", "star,planet", "system_planets(system_id,id)"},
		{"aj_discovery_objects", "universe_discovery_system_fk", "star_id", "system_identity(id)"},
	}
	for _, r := range refs {
		var exists bool
		if e := tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM pg_constraint WHERE conrelid=$1::regclass AND conname=$2)`, r.table, r.name).Scan(&exists); e != nil {
			return e
		}
		if exists {
			continue
		}
		if _, e := tx.Exec(ctx, "ALTER TABLE "+pgx.Identifier{r.table}.Sanitize()+" ADD CONSTRAINT "+pgx.Identifier{r.name}.Sanitize()+" FOREIGN KEY ("+r.columns+") REFERENCES "+r.target+" DEFERRABLE INITIALLY DEFERRED NOT VALID"); e != nil {
			return e
		}
		if _, e := tx.Exec(ctx, "ALTER TABLE "+pgx.Identifier{r.table}.Sanitize()+" VALIDATE CONSTRAINT "+pgx.Identifier{r.name}.Sanitize()); e != nil {
			return fmt.Errorf("universe migration: invalid reference in %s (%s): %w", r.table, r.columns, e)
		}
	}
	return nil
}
