package initialization

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"log"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/yomezza/cosmosimulator/internal/chatintegration"
	"github.com/yomezza/cosmosimulator/internal/discovery"
	"github.com/yomezza/cosmosimulator/internal/fleet"
	"github.com/yomezza/cosmosimulator/internal/namedictionary"
	"github.com/yomezza/cosmosimulator/internal/platform/configuration"
	"github.com/yomezza/cosmosimulator/internal/platform/dblayout"
	"github.com/yomezza/cosmosimulator/internal/platform/postgres"
	"github.com/yomezza/cosmosimulator/internal/universe"
	"github.com/yomezza/cosmosimulator/internal/universestore"
	"github.com/yomezza/cosmosimulator/internal/worldstore"
)

func OpenDatabase(ctx context.Context, dsn, schema string, schemas fs.ReadFileFS) (*pgxpool.Pool, error) {
	started := time.Now()
	connection, cancel := context.WithTimeout(ctx, 10*time.Second)
	db, schema, err := postgres.Open(connection, dsn, schema)
	cancel()
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
	log.Print("Cosmic initialization: acquiring migration/world locks")
	if _, err = tx.Exec(ctx, "SELECT pg_advisory_xact_lock(784216),pg_advisory_xact_lock(784218)"); err != nil {
		return nil, fmt.Errorf("acquire migration/world locks: %w", err)
	}
	log.Print("Cosmic initialization: applying database migrations")
	var exists bool
	if err = tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM pg_catalog.pg_namespace WHERE nspname=$1)`, schema).Scan(&exists); err != nil {
		return nil, err
	}
	if !exists {
		if _, err = tx.Exec(ctx, "CREATE SCHEMA "+pgx.Identifier{schema}.Sanitize()); err != nil {
			return nil, fmt.Errorf("create DB_SCHEMA %q (create it manually or grant CREATE on the database): %w", schema, err)
		}
	}
	log.Printf("Cosmic schema layout: root=%q domains=%d version=2", schema, len(dblayout.Domains))
	if err = dblayout.Prepare(ctx, tx, schema); err != nil {
		return nil, err
	}
	sql, err := schemas.ReadFile("schema.sql")
	if err != nil {
		return nil, err
	}
	if _, err = tx.Exec(ctx, dblayout.DDL(schema, string(sql))); err != nil {
		return nil, fmt.Errorf("initialize DB_SCHEMA %q (USAGE and CREATE on the schema are required): %w", schema, err)
	}
	upgrade, e := schemas.ReadFile("schema033.sql")
	if e != nil {
		return nil, e
	}
	if _, e = tx.Exec(ctx, dblayout.DDL(schema, string(upgrade))); e != nil {
		return nil, fmt.Errorf("upgrade 0.3.3: %w", e)
	}
	upgrade040, e := schemas.ReadFile("schema040.sql")
	if e != nil {
		return nil, e
	}
	if _, e = tx.Exec(ctx, dblayout.DDL(schema, string(upgrade040))); e != nil {
		return nil, fmt.Errorf("upgrade 0.4.0: %w", e)
	}
	upgrade050, e := schemas.ReadFile("schema050.sql")
	if e != nil {
		return nil, e
	}
	if _, e = tx.Exec(ctx, dblayout.DDL(schema, string(upgrade050))); e != nil {
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
	if _, e = tx.Exec(ctx, dblayout.DDL(schema, string(upgrade070))); e != nil {
		return nil, fmt.Errorf("upgrade 0.7.0: %w", e)
	}
	upgrade080, e := schemas.ReadFile("schema080.sql")
	if e != nil {
		return nil, e
	}
	if _, e = tx.Exec(ctx, dblayout.DDL(schema, string(upgrade080))); e != nil {
		return nil, fmt.Errorf("upgrade 0.8.0: %w", e)
	}

	upgrade090, e := schemas.ReadFile("schema090.sql")
	if e != nil {
		return nil, e
	}
	if _, e = tx.Exec(ctx, dblayout.DDL(schema, string(upgrade090))); e != nil {
		return nil, fmt.Errorf("upgrade 0.9.0: %w", e)
	}
	upgrade100, e := schemas.ReadFile("schema100.sql")
	if e != nil {
		return nil, e
	}
	if _, e = tx.Exec(ctx, dblayout.DDL(schema, string(upgrade100))); e != nil {
		return nil, fmt.Errorf("upgrade 0.10.0: %w", e)
	}
	upgrade110, e := schemas.ReadFile("schema110.sql")
	if e != nil {
		return nil, e
	}
	if _, e = tx.Exec(ctx, dblayout.DDL(schema, string(upgrade110))); e != nil {
		return nil, fmt.Errorf("upgrade 0.11.0: %w", e)
	}
	upgrade120, e := schemas.ReadFile("schema120.sql")
	if e != nil {
		return nil, e
	}
	if _, e = tx.Exec(ctx, dblayout.DDL(schema, string(upgrade120))); e != nil {
		return nil, fmt.Errorf("upgrade 0.12.0: %w", e)
	}
	upgrade124, e := schemas.ReadFile("schema124.sql")
	if e != nil {
		return nil, e
	}
	if _, e = tx.Exec(ctx, dblayout.DDL(schema, string(upgrade124))); e != nil {
		return nil, fmt.Errorf("upgrade 0.12.4: %w", e)
	}
	upgrade144, e := schemas.ReadFile("schema144.sql")
	if e != nil {
		return nil, e
	}
	if _, e = tx.Exec(ctx, dblayout.DDL(schema, string(upgrade144))); e != nil {
		return nil, fmt.Errorf("upgrade 0.14.4: %w", e)
	}
	upgrade150, e := schemas.ReadFile("schema150.sql")
	if e != nil {
		return nil, e
	}
	if _, e = tx.Exec(ctx, dblayout.DDL(schema, string(upgrade150))); e != nil {
		return nil, fmt.Errorf("upgrade 0.15.0: %w", e)
	}
	upgrade160, e := schemas.ReadFile("schema160.sql")
	if e != nil {
		return nil, e
	}
	if _, e = tx.Exec(ctx, dblayout.DDL(schema, string(upgrade160))); e != nil {
		return nil, fmt.Errorf("upgrade 0.16.0: %w", e)
	}
	upgrade170, e := schemas.ReadFile("schema170.sql")
	if e != nil {
		return nil, e
	}
	if _, e = tx.Exec(ctx, dblayout.DDL(schema, string(upgrade170))); e != nil {
		return nil, fmt.Errorf("upgrade 0.17.0: %w", e)
	}
	upgrade180, e := schemas.ReadFile("schema180.sql")
	if e != nil {
		return nil, e
	}
	if _, e = tx.Exec(ctx, dblayout.DDL(schema, string(upgrade180))); e != nil {
		return nil, fmt.Errorf("upgrade 0.18.0: %w", e)
	}
	upgrade190, e := schemas.ReadFile("schema190.sql")
	if e != nil {
		return nil, e
	}
	if _, e = tx.Exec(ctx, dblayout.DDL(schema, string(upgrade190))); e != nil {
		return nil, fmt.Errorf("upgrade 0.19.0: %w", e)
	}
	upgrade200, e := schemas.ReadFile("schema200.sql")
	if e != nil {
		return nil, e
	}
	if _, e = tx.Exec(ctx, dblayout.DDL(schema, string(upgrade200))); e != nil {
		return nil, fmt.Errorf("upgrade 0.20.0: %w", e)
	}
	upgrade210, e := schemas.ReadFile("schema210.sql")
	if e != nil {
		return nil, e
	}
	if _, e = tx.Exec(ctx, dblayout.DDL(schema, string(upgrade210))); e != nil {
		return nil, fmt.Errorf("upgrade 0.21.0: %w", e)
	}

	var chatInstalled bool
	if e = tx.QueryRow(ctx, `SELECT to_regclass('chat_schema_state') IS NOT NULL`).Scan(&chatInstalled); e != nil {
		return nil, e
	}
	if configuration.Getenv("CHAT_ENABLED") == "true" || chatInstalled {
		upgrade240, e := schemas.ReadFile("schema240.sql")
		if e != nil {
			return nil, e
		}
		if _, e = tx.Exec(ctx, dblayout.DDL(schema, string(upgrade240))); e != nil {
			return nil, fmt.Errorf("chat migration: %w", e)
		}
		if e = chatintegration.Functions(ctx, tx, schema); e != nil {
			return nil, e
		}
		if e = chatintegration.Partitions(ctx, tx, time.Now()); e != nil {
			return nil, e
		}
	}

	for _, file := range []string{"schema250.sql", "schema251.sql", "schema252.sql", "schema253.sql", "schema254.sql", "schema259.sql", "schema2510.sql", "schema280.sql", "schema290.sql", "schema300.sql"} {
		upgrade, e := schemas.ReadFile(file)
		if e != nil {
			return nil, e
		}
		if _, e = tx.Exec(ctx, dblayout.DDL(schema, string(upgrade))); e != nil {
			return nil, fmt.Errorf("upgrade %s: %w", file, e)
		}
	}

	if _, err = tx.Exec(ctx, dblayout.DDL(schema, universestore.Schema)); err != nil {
		return nil, fmt.Errorf("tabular universe schema: %w", err)
	}
	if _, err = tx.Exec(ctx, dblayout.DDL(schema, namedictionary.Schema)); err != nil {
		return nil, err
	}
	if _, err = tx.Exec(ctx, namedictionary.Seed); err != nil {
		return nil, err
	}
	relational, err := universestore.Installed(ctx, tx)
	if err != nil {
		return nil, err
	}
	var existing universe.Galaxy
	var raw []byte
	fresh := false
	if relational {
		loaded, e := universestore.Load(ctx, tx)
		if e != nil {
			return nil, e
		}
		existing = *loaded
	} else {
		err = tx.QueryRow(ctx, `SELECT data FROM galaxies WHERE name='Edem'`).Scan(&raw)
		if errors.Is(err, pgx.ErrNoRows) {
			fresh = true
			existing = universe.Generate(1000, 20261026)
		} else if err != nil {
			return nil, err
		} else if err = json.Unmarshal(raw, &existing); err != nil {
			return nil, err
		}
		if existing.Name == "" || len(existing.Stars) == 0 {
			return nil, errors.New("legacy universe is empty or corrupt; refusing to regenerate it")
		}
		for i, star := range existing.Stars {
			if star.ID != i+1 {
				return nil, errors.New("legacy universe has missing/duplicate IDs; explicit repair required")
			}
		}
	}
	legacyRaw := append([]byte(nil), raw...)
	if existing.Version < 3 {
		universe.Enrich(&existing)
		universe.UpgradeGeology(&existing)
		existing.Regions = universe.Regions
		raw, err = json.Marshal(existing)
		if err != nil {
			return nil, err
		}
	}
	originalCount := len(existing.Stars)
	if fresh {
		originalCount = 0
	}
	if universe.ArrangeFrontierRings(&existing) {
		raw, err = json.Marshal(existing)
		if err != nil {
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
	}
	if changed, e := worldstore.UpgradeFounderSettlements(ctx, tx, &existing); e != nil {
		return nil, e
	} else if changed {
		raw, err = json.Marshal(existing)
		if err != nil {
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
	}
	if err = namedictionary.NameNew(ctx, tx, &existing, originalCount); err != nil {
		return nil, err
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
	beforeNames, err := json.Marshal(existing)
	if err != nil {
		return nil, err
	}
	log.Printf("Cosmic initialization: discovery registry (%d systems)", len(existing.Stars))
	if err = discovery.Sync(ctx, tx, &existing); err != nil {
		return nil, fmt.Errorf("initialize discovery registry: %w", err)
	}
	raw, err = json.Marshal(existing)
	if err != nil {
		return nil, err
	}
	if string(beforeNames) != string(raw) {
		existing.Revision++
		raw, err = json.Marshal(existing)
		if err != nil {
			return nil, err
		}
	}
	if err = universestore.Save(ctx, tx, &existing); err != nil {
		return nil, fmt.Errorf("save tabular universe: %w", err)
	}
	if !relational {
		if err = universestore.PreserveLegacyExtras(ctx, tx, legacyRaw); err != nil {
			return nil, err
		}
		if err = universestore.Verify(ctx, tx, &existing, legacyRaw); err != nil {
			return nil, fmt.Errorf("verify tabular migration: %w", err)
		}
	}

	// Old installations already have deferred station/system foreign keys.
	// Validate queued row events before index DDL, without committing partial work.
	if _, err = tx.Exec(ctx, `SET CONSTRAINTS ALL IMMEDIATE`); err != nil {
		return nil, err
	}
	if _, err = tx.Exec(ctx, `CREATE UNIQUE INDEX IF NOT EXISTS stations_global_name_unique ON stations(lower(btrim(name)))`); err != nil {
		return nil, err
	}
	if err = universestore.InstallReferences(ctx, tx); err != nil {
		return nil, err
	}
	if err = dblayout.Finish(ctx, tx, schema); err != nil {
		return nil, err
	}
	if err = tx.Commit(ctx); err != nil {
		return nil, err
	}
	ready = true
	log.Printf("Cosmic initialization complete in %s", time.Since(started).Round(time.Millisecond))
	return db, nil
}
