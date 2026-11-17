// Package namedictionary loads editable naming vocabulary from PostgreSQL.
package namedictionary

import (
	"context"
	_ "embed"
	"fmt"
	"strings"

	"github.com/jackc/pgx/v5"
	"github.com/yomezza/cosmosimulator/internal/universe"
)

//go:embed schema.sql
var Schema string

//go:embed seed.sql
var Seed string

var Tables = []string{"system_names", "planet_names", "station_names", "asteroid_names", "ice_field_names", "anomaly_names"}

type Vocabulary struct{ Bases, Suffixes []string }

func Load(ctx context.Context, tx pgx.Tx, table string) (Vocabulary, error) {
	var v Vocabulary
	allowed := false
	for _, t := range Tables {
		if t == table {
			allowed = true
		}
	}
	if !allowed {
		return v, fmt.Errorf("unknown name dictionary %q", table)
	}
	rows, err := tx.Query(ctx, "SELECT word,part FROM "+pgx.Identifier{table}.Sanitize()+" WHERE active ORDER BY id")
	if err != nil {
		return v, err
	}
	defer rows.Close()
	for rows.Next() {
		var word, part string
		if err = rows.Scan(&word, &part); err != nil {
			return v, err
		}
		if part == "base" {
			v.Bases = append(v.Bases, strings.TrimSpace(word))
		} else {
			v.Suffixes = append(v.Suffixes, strings.TrimSpace(word))
		}
	}
	if err = rows.Err(); err != nil {
		return v, err
	}
	if len(v.Bases) == 0 || len(v.Suffixes) == 0 {
		return v, fmt.Errorf("name dictionary %s requires active bases and suffixes", table)
	}
	return v, nil
}

// Name is constant-time even with a single remaining combination. The monotonic
// index gives each exhausted cycle its own suffix, without rescanning old names.
func (v Vocabulary) Name(index int64) string {
	combinations := int64(len(v.Bases)) * int64(len(v.Suffixes))
	name := v.Bases[index%int64(len(v.Bases))] + " " + v.Suffixes[(index/int64(len(v.Bases)))%int64(len(v.Suffixes))]
	if index >= combinations {
		name += fmt.Sprintf(" %d", index/combinations+1)
	}
	return name
}

// NameNew never renames already stored systems or planets.
func NameNew(ctx context.Context, tx pgx.Tx, g *universe.Galaxy, from int) error {
	if from >= len(g.Stars) {
		return nil
	}
	systems, err := Load(ctx, tx, "system_names")
	if err != nil {
		return err
	}
	planets, err := Load(ctx, tx, "planet_names")
	if err != nil {
		return err
	}
	used := map[string]bool{}
	for _, s := range g.Stars[:from] {
		used[strings.ToLower(s.Name)] = true
	}
	index := int64(from)
	for i := from; i < len(g.Stars); i++ {
		s := &g.Stars[i]
		for {
			s.Name = systems.Name(index)
			index++
			if !used[strings.ToLower(s.Name)] {
				break
			}
		}
		used[strings.ToLower(s.Name)] = true
		for j := range s.Planets {
			s.Planets[j].Name = planets.Name(int64(s.ID-1)*100 + int64(j))
		}
	}
	return nil
}
