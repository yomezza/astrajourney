package fleet

import (
	"context"
	"encoding/json"

	"github.com/jackc/pgx/v5"

	"github.com/yomezza/cosmosimulator/internal/events"
	"github.com/yomezza/cosmosimulator/internal/platform/postgres"
)

const shipColumns = `star,destination,arrival,fuel,cargo,id,name,object_id,position_mode,departure,travel_path,local_target,local_mode,local_started,local_arrival,extractor,reserved_fuel,active,waypoints,class,modules_initialized,COALESCE((SELECT capacity FROM ship_module_hulls WHERE class=ships.class),8),COALESCE((SELECT jsonb_agg(m.definition||jsonb_build_object('id',m.id,'installed',m.installed,'paid',m.purchased_price) ORDER BY m.id) FROM ship_modules m WHERE m.ship_id=ships.id),'[]'::jsonb)`

func ReadShip(ctx context.Context, q postgres.Querier, id int64) (Ship, error) {
	return scanShip(q.QueryRow(ctx, `SELECT `+shipColumns+` FROM ships WHERE id=$1`, id))
}

// ReadShips loads a fleet in one query, using the same decoding as ReadShip.
func ReadShips(ctx context.Context, q interface {
	Query(context.Context, string, ...any) (pgx.Rows, error)
}, uid int64) ([]Ship, error) {
	rows, err := q.Query(ctx, `SELECT `+shipColumns+` FROM ships WHERE user_id=$1 ORDER BY id`, uid)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	ships := []Ship{}
	for rows.Next() {
		ship, err := scanShip(rows)
		if err != nil {
			return nil, err
		}
		ships = append(ships, ship)
	}
	return ships, rows.Err()
}

func scanShip(row pgx.Row) (Ship, error) {
	var s Ship
	var raw, path, way, modules []byte
	err := row.Scan(&s.Star, &s.Destination, &s.Arrival, &s.Fuel, &raw, &s.ID, &s.Name, &s.ObjectID, &s.PositionMode, &s.Departure, &path, &s.LocalTarget, &s.LocalMode, &s.LocalStarted, &s.LocalArrival, &s.Extractor, &s.ReservedFuel, &s.Active, &way, &s.Class, &s.ModulesReady, &s.ModuleLimit, &modules)
	if err != nil {
		return s, err
	}
	if err = json.Unmarshal(raw, &s.Cargo); err != nil {
		return s, err
	}
	if err = json.Unmarshal(path, &s.Path); err != nil {
		return s, err
	}
	if err = json.Unmarshal(modules, &s.Modules); err != nil {
		return s, err
	}
	if s.ModulesReady {
		s.Extractor = "none"
		for _, m := range s.Modules {
			if m.Installed && m.Kind == "extractor" {
				s.Extractor = m.Effects.Extractor
			}
		}
	}
	s.Spec = s.Hull()
	s.Mining = s.MiningStats(nil)
	err = json.Unmarshal(way, &s.Waypoints)
	return s, err
}

func SettleShips(ctx context.Context, tx pgx.Tx, uid int64) error {
	rows, err := tx.Query(ctx, `UPDATE ships SET star=destination,destination=NULL,arrival=NULL,object_id='entry',position_mode='approach',departure=NULL,travel_path='[]' WHERE user_id=$1 AND arrival<=now() RETURNING id,star`, uid)
	if err != nil {
		return err
	}
	type arrived struct {
		id   int64
		star int
	}
	list := []arrived{}
	for rows.Next() {
		var v arrived
		if err = rows.Scan(&v.id, &v.star); err != nil {
			rows.Close()
			return err
		}
		list = append(list, v)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return err
	}
	for _, v := range list {
		if err = events.RecordEvent(ctx, tx, uid, "arrival", map[string]any{"star": v.star, "ship": v.id}); err != nil {
			return err
		}
	}
	rows, err = tx.Query(ctx, `UPDATE ships SET object_id=local_target,position_mode=local_mode,local_target=NULL,local_mode=NULL,local_started=NULL,local_arrival=NULL WHERE user_id=$1 AND local_arrival<=now() RETURNING id,star,object_id`, uid)
	if err != nil {
		return err
	}
	list = nil
	objects := []string{}
	for rows.Next() {
		var v arrived
		var obj string
		if err = rows.Scan(&v.id, &v.star, &obj); err != nil {
			rows.Close()
			return err
		}
		list = append(list, v)
		objects = append(objects, obj)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return err
	}
	for i, v := range list {
		if err = events.RecordEvent(ctx, tx, uid, "arrival", map[string]any{"star": v.star, "ship": v.id, "object": objects[i]}); err != nil {
			return err
		}
	}
	return nil
}

func ResearchBusy(ctx context.Context, q postgres.Querier, sid int64) bool {
	var busy bool
	_ = q.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM research_jobs WHERE ship_id=$1 AND NOT paused)`, sid).Scan(&busy)
	return busy
}
