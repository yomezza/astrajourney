package worldstore

import (
	"context"
	"fmt"
	"regexp"
	"strings"

	"github.com/jackc/pgx/v5"
	"github.com/yomezza/cosmosimulator/internal/namedictionary"
	"github.com/yomezza/cosmosimulator/internal/universe"
)

type stationName struct{ name, origin string }

func stationKey(star int, object string) string { return fmt.Sprintf("%d/%s", star, object) }

// Called under the world writer lock. Includes inactive stations and every galaxy.
func prepareStationNames(ctx context.Context, tx pgx.Tx, g *universe.Galaxy) (map[string]stationName, map[string]bool, error) {
	rows, err := tx.Query(ctx, `SELECT star,object_id,name,name_origin,company_id IS NOT NULL FROM stations ORDER BY star,object_id`)
	if err != nil {
		return nil, nil, err
	}
	type record struct {
		star                 int
		object, name, origin string
		owned                bool
	}
	records := []record{}
	for rows.Next() {
		var r record
		if err = rows.Scan(&r.star, &r.object, &r.name, &r.origin, &r.owned); err != nil {
			rows.Close()
			return nil, nil, err
		}
		records = append(records, r)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return nil, nil, err
	}
	existing := map[string]stationName{}
	used := map[string]bool{}
	suffix := map[string]int{}
	for i := range records {
		r := &records[i]
		if r.origin == "legacy" {
			generated := false
			if !r.owned && r.star > 0 && r.star <= len(g.Stars) {
				generated = regexp.MustCompile(`^station(?:-[0-9]+)?$`).MatchString(r.object) && regexp.MustCompile(` · (?:PORT [0-9]+|CORE)$`).MatchString(r.name)
			}
			if generated {
				r.origin = "generated_pending"
			} else {
				r.origin = "legacy_custom"
			}
		}
	}
	// Reserve every original custom name before assigning collision suffixes.
	// An earlier duplicate must not steal another station's distinct saved name.
	for _, r := range records {
		if r.origin != "generated_pending" {
			used[strings.ToLower(strings.TrimSpace(r.name))] = true
		}
	}
	seen := map[string]bool{}
	for _, r := range records {
		if r.origin == "generated_pending" {
			existing[stationKey(r.star, r.object)] = stationName{"", r.origin}
			continue
		}
		original := r.name
		key := strings.ToLower(strings.TrimSpace(r.name))
		if seen[key] {
			for {
				suffix[original]++
				r.name = fmt.Sprintf("%s %d", original, suffix[original]+1)
				key = strings.ToLower(strings.TrimSpace(r.name))
				if !used[key] {
					break
				}
			}
			if _, err = tx.Exec(ctx, `INSERT INTO admin_log(action,data) VALUES('station_name_collision',jsonb_build_object('star',$1::int,'object',$2::text,'old_name',$3::text,'new_name',$4::text))`, r.star, r.object, original, r.name); err != nil {
				return nil, nil, err
			}
		}
		used[key] = true
		seen[key] = true
		existing[stationKey(r.star, r.object)] = stationName{r.name, r.origin}
		if _, err = tx.Exec(ctx, `UPDATE stations SET name=$3,name_origin=$4 WHERE star=$1 AND object_id=$2 AND (name<>$3 OR name_origin<>$4)`, r.star, r.object, r.name, r.origin); err != nil {
			return nil, nil, err
		}
	}
	return existing, used, nil
}
func stationAllocator(ctx context.Context, tx pgx.Tx, used map[string]bool) (func() (string, error), func() error, error) {
	v, err := namedictionary.Load(ctx, tx, "station_names")
	if err != nil {
		return nil, nil, err
	}
	if _, err = tx.Exec(ctx, `INSERT INTO name_dictionary_state(key) VALUES('stations:cursor') ON CONFLICT DO NOTHING`); err != nil {
		return nil, nil, err
	}
	var index int64
	if err = tx.QueryRow(ctx, `SELECT next_value FROM name_dictionary_state WHERE key='stations:cursor' FOR UPDATE`).Scan(&index); err != nil {
		return nil, nil, err
	}
	next := func() (string, error) {
		for {
			name := v.Name(index)
			index++
			key := strings.ToLower(strings.TrimSpace(name))
			if !used[key] {
				used[key] = true
				return name, nil
			}
		}
	}
	finish := func() error {
		_, e := tx.Exec(ctx, `UPDATE name_dictionary_state SET next_value=$1 WHERE key='stations:cursor'`, index)
		return e
	}
	return next, finish, nil
}
