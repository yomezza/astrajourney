package initialization

import (
	"context"
	"encoding/json"
	"fmt"
	"io/fs"
	"os"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/yomezza/cosmosimulator/internal/chatintegration"
	"github.com/yomezza/cosmosimulator/internal/fleet"
	"github.com/yomezza/cosmosimulator/internal/platform/postgres"
	"github.com/yomezza/cosmosimulator/internal/universe"
	"github.com/yomezza/cosmosimulator/internal/worldstore"
)

func OpenDatabase(ctx context.Context, dsn, schema string, schemas fs.ReadFileFS) (*pgxpool.Pool, error) {
	db, schema, err := postgres.Open(ctx, dsn, schema)
	if err != nil {
		return nil, err
	}
	ready := false
	defer func() {
		if !ready {
			db.Close()
		}
	}()
	tx, err := db.Begin(ctx)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback(context.Background())
	if _, err = tx.Exec(ctx, "SELECT pg_advisory_xact_lock(784216)"); err != nil {
		return nil, err
	}
	var exists bool
	if err = tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM pg_catalog.pg_namespace WHERE nspname=$1)`, schema).Scan(&exists); err != nil {
		return nil, err
	}
	if !exists {
		if _, err = tx.Exec(ctx, "CREATE SCHEMA "+pgx.Identifier{schema}.Sanitize()); err != nil {
			return nil, fmt.Errorf("create DB_SCHEMA %q (create it manually or grant CREATE on the database): %w", schema, err)
		}
	}
	sql, err := schemas.ReadFile("schema.sql")
	if err != nil {
		return nil, err
	}
	if _, err = tx.Exec(ctx, string(sql)); err != nil {
		return nil, fmt.Errorf("initialize DB_SCHEMA %q (USAGE and CREATE on the schema are required): %w", schema, err)
	}
	upgrade, e := schemas.ReadFile("schema033.sql")
	if e != nil {
		return nil, e
	}
	if _, e = tx.Exec(ctx, string(upgrade)); e != nil {
		return nil, fmt.Errorf("upgrade 0.3.3: %w", e)
	}
	upgrade040, e := schemas.ReadFile("schema040.sql")
	if e != nil {
		return nil, e
	}
	if _, e = tx.Exec(ctx, string(upgrade040)); e != nil {
		return nil, fmt.Errorf("upgrade 0.4.0: %w", e)
	}
	upgrade050, e := schemas.ReadFile("schema050.sql")
	if e != nil {
		return nil, e
	}
	if _, e = tx.Exec(ctx, string(upgrade050)); e != nil {
		return nil, fmt.Errorf("upgrade 0.5.0: %w", e)
	}
	if _, err = tx.Exec(ctx, `ALTER TABLE stations ADD COLUMN IF NOT EXISTS hull_catalogue_version INTEGER NOT NULL DEFAULT 0`); err != nil {
		return nil, err
	}
	if err = fleet.EnsureShipHullConstraint(ctx, tx); err != nil {
		return nil, fmt.Errorf("upgrade hull catalogue: %w", err)
	}
	upgrade070, e := schemas.ReadFile("schema070.sql")
	if e != nil {
		return nil, e
	}
	if _, e = tx.Exec(ctx, string(upgrade070)); e != nil {
		return nil, fmt.Errorf("upgrade 0.7.0: %w", e)
	}
	upgrade080, e := schemas.ReadFile("schema080.sql")
	if e != nil {
		return nil, e
	}
	if _, e = tx.Exec(ctx, string(upgrade080)); e != nil {
		return nil, fmt.Errorf("upgrade 0.8.0: %w", e)
	}

	upgrade090, e := schemas.ReadFile("schema090.sql")
	if e != nil {
		return nil, e
	}
	if _, e = tx.Exec(ctx, string(upgrade090)); e != nil {
		return nil, fmt.Errorf("upgrade 0.9.0: %w", e)
	}
	upgrade100, e := schemas.ReadFile("schema100.sql")
	if e != nil {
		return nil, e
	}
	if _, e = tx.Exec(ctx, string(upgrade100)); e != nil {
		return nil, fmt.Errorf("upgrade 0.10.0: %w", e)
	}
	upgrade110, e := schemas.ReadFile("schema110.sql")
	if e != nil {
		return nil, e
	}
	if _, e = tx.Exec(ctx, string(upgrade110)); e != nil {
		return nil, fmt.Errorf("upgrade 0.11.0: %w", e)
	}
	upgrade120, e := schemas.ReadFile("schema120.sql")
	if e != nil {
		return nil, e
	}
	if _, e = tx.Exec(ctx, string(upgrade120)); e != nil {
		return nil, fmt.Errorf("upgrade 0.12.0: %w", e)
	}
	upgrade124, e := schemas.ReadFile("schema124.sql")
	if e != nil {
		return nil, e
	}
	if _, e = tx.Exec(ctx, string(upgrade124)); e != nil {
		return nil, fmt.Errorf("upgrade 0.12.4: %w", e)
	}
	upgrade144, e := schemas.ReadFile("schema144.sql")
	if e != nil {
		return nil, e
	}
	if _, e = tx.Exec(ctx, string(upgrade144)); e != nil {
		return nil, fmt.Errorf("upgrade 0.14.4: %w", e)
	}
	upgrade150, e := schemas.ReadFile("schema150.sql")
	if e != nil {
		return nil, e
	}
	if _, e = tx.Exec(ctx, string(upgrade150)); e != nil {
		return nil, fmt.Errorf("upgrade 0.15.0: %w", e)
	}
	upgrade160, e := schemas.ReadFile("schema160.sql")
	if e != nil {
		return nil, e
	}
	if _, e = tx.Exec(ctx, string(upgrade160)); e != nil {
		return nil, fmt.Errorf("upgrade 0.16.0: %w", e)
	}
	upgrade170, e := schemas.ReadFile("schema170.sql")
	if e != nil {
		return nil, e
	}
	if _, e = tx.Exec(ctx, string(upgrade170)); e != nil {
		return nil, fmt.Errorf("upgrade 0.17.0: %w", e)
	}
	upgrade180, e := schemas.ReadFile("schema180.sql")
	if e != nil {
		return nil, e
	}
	if _, e = tx.Exec(ctx, string(upgrade180)); e != nil {
		return nil, fmt.Errorf("upgrade 0.18.0: %w", e)
	}
	upgrade190, e := schemas.ReadFile("schema190.sql")
	if e != nil {
		return nil, e
	}
	if _, e = tx.Exec(ctx, string(upgrade190)); e != nil {
		return nil, fmt.Errorf("upgrade 0.19.0: %w", e)
	}
	upgrade200, e := schemas.ReadFile("schema200.sql")
	if e != nil {
		return nil, e
	}
	if _, e = tx.Exec(ctx, string(upgrade200)); e != nil {
		return nil, fmt.Errorf("upgrade 0.20.0: %w", e)
	}
	upgrade210, e := schemas.ReadFile("schema210.sql")
	if e != nil {
		return nil, e
	}
	if _, e = tx.Exec(ctx, string(upgrade210)); e != nil {
		return nil, fmt.Errorf("upgrade 0.21.0: %w", e)
	}

	var chatInstalled bool
	if e = tx.QueryRow(ctx, `SELECT to_regclass('chat_schema_state') IS NOT NULL`).Scan(&chatInstalled); e != nil {
		return nil, e
	}
	if os.Getenv("CHAT_ENABLED") == "true" || chatInstalled {
		upgrade240, e := schemas.ReadFile("schema240.sql")
		if e != nil {
			return nil, e
		}
		if _, e = tx.Exec(ctx, string(upgrade240)); e != nil {
			return nil, fmt.Errorf("chat migration: %w", e)
		}
		if e = chatintegration.Functions(ctx, tx, schema); e != nil {
			return nil, e
		}
		if e = chatintegration.Partitions(ctx, tx, time.Now()); e != nil {
			return nil, e
		}
	}

	var galaxyExists bool
	if err = tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM galaxies WHERE name='Edem')`).Scan(&galaxyExists); err != nil {
		return nil, err
	}
	if !galaxyExists {
		data, err := json.Marshal(universe.Generate(1000, 20261026))
		if err != nil {
			return nil, err
		}
		if _, err = tx.Exec(ctx, `INSERT INTO galaxies(name,data) VALUES('Edem',$1)`, data); err != nil {
			return nil, err
		}
	}
	var raw []byte
	if err = tx.QueryRow(ctx, `SELECT data FROM galaxies WHERE name='Edem'`).Scan(&raw); err != nil {
		return nil, err
	}
	var existing universe.Galaxy
	if err = json.Unmarshal(raw, &existing); err != nil {
		return nil, err
	}
	if existing.Version < 3 {
		universe.Enrich(&existing)
		universe.UpgradeGeology(&existing)
		existing.Regions = universe.Regions
		raw, err = json.Marshal(existing)
		if err != nil {
			return nil, err
		}
		if _, err = tx.Exec(ctx, `UPDATE galaxies SET data=$1 WHERE name='Edem'`, raw); err != nil {
			return nil, err
		}
	}
	oldNames := make(map[int]string, len(existing.Stars))
	for _, star := range existing.Stars {
		oldNames[star.ID] = star.Name
	}
	if universe.DiversifySystemNames(&existing) {
		renames := []map[string]any{}
		for _, star := range existing.Stars {
			if oldNames[star.ID] != star.Name {
				renames = append(renames, map[string]any{"id": star.ID, "old": oldNames[star.ID], "name": star.Name})
			}
		}
		labels, _ := json.Marshal(renames)
		if _, err = tx.Exec(ctx, `UPDATE stations s SET name=m.name||substr(s.name,length(m.old)+1) FROM jsonb_to_recordset($1) AS m(id int,old text,name text) WHERE s.star=m.id AND s.name IN(m.old||' · PORT 1',m.old||' · PORT 2',m.old||' · PORT 3',m.old||' · CORE')`, labels); err != nil {
			return nil, err
		}

		raw, err = json.Marshal(existing)
		if err != nil {
			return nil, err
		}
		if _, err = tx.Exec(ctx, `UPDATE galaxies SET data=$1 WHERE name='Edem'`, raw); err != nil {
			return nil, err
		}
	}
	if universe.ArrangeFrontierRings(&existing) {
		raw, err = json.Marshal(existing)
		if err != nil {
			return nil, err
		}
		if _, err = tx.Exec(ctx, `UPDATE galaxies SET data=$1 WHERE name='Edem'`, raw); err != nil {
			return nil, err
		}
	}
	if changed, e := worldstore.UpgradeManagedGalaxies(ctx, tx, &existing); e != nil {
		return nil, e
	} else if changed {
		raw, err = json.Marshal(existing)
		if err != nil {
			return nil, err
		}
		if _, err = tx.Exec(ctx, `UPDATE galaxies SET data=$1 WHERE name='Edem'`, raw); err != nil {
			return nil, err
		}
	}
	if changed, e := worldstore.UpgradeFounderSettlements(ctx, tx, &existing); e != nil {
		return nil, e
	} else if changed {
		raw, err = json.Marshal(existing)
		if err != nil {
			return nil, err
		}
		if _, err = tx.Exec(ctx, `UPDATE galaxies SET data=$1 WHERE name='Edem'`, raw); err != nil {
			return nil, err
		}
	}
	if changed, e := worldstore.SecureDiscovery(ctx, tx, &existing); e != nil {
		return nil, e
	} else if changed {
		raw, err = json.Marshal(existing)
		if err != nil {
			return nil, err
		}
		if _, err = tx.Exec(ctx, `UPDATE galaxies SET data=$1 WHERE name='Edem'`, raw); err != nil {
			return nil, err
		}
	}
	if err = worldstore.EnsureStations(ctx, tx, &existing); err != nil {
		return nil, err
	}
	// Complete metadata for a v0.3.1 flight without changing its route or ETA.
	rows, err := tx.Query(ctx, `SELECT user_id,star,destination,arrival FROM ships WHERE destination IS NOT NULL AND jsonb_array_length(travel_path)<2`)
	if err != nil {
		return nil, err
	}
	type oldFlight struct {
		id       int64
		star, to int
		arrival  time.Time
	}
	flights := []oldFlight{}
	for rows.Next() {
		var f oldFlight
		if err = rows.Scan(&f.id, &f.star, &f.to, &f.arrival); err != nil {
			rows.Close()
			return nil, err
		}
		flights = append(flights, f)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return nil, err
	}
	for _, f := range flights {
		path := existing.Path(f.star, f.to)
		raw, _ := json.Marshal(path)
		departure := f.arrival.Add(-time.Duration(max(0, len(path)-1)) * 5 * time.Second)
		if _, err = tx.Exec(ctx, `UPDATE ships SET travel_path=$2,departure=$3 WHERE user_id=$1`, f.id, raw, departure); err != nil {
			return nil, err
		}
	}
	if err = tx.Commit(ctx); err != nil {
		return nil, err
	}
	ready = true
	return db, nil
}
