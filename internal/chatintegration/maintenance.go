package chatintegration

import (
	"context"
	"fmt"
	"github.com/jackc/pgx/v5"
	"github.com/yomezza/cosmosimulator/internal/platform/postgres"
	"strings"
	"time"
)

// MaintainPartitions requires the separate DDL/partition-owner connection. The
// runtime login neither has CREATE nor owns the parent message table.
func MaintainPartitions(ctx context.Context, q postgres.DBTX, now time.Time) error {
	if e := Partitions(ctx, q, now); e != nil {
		return e
	}
	rows, e := q.Query(ctx, `SELECT child.relname FROM pg_inherits i JOIN pg_class child ON child.oid=i.inhrelid JOIN pg_namespace ns ON ns.oid=child.relnamespace JOIN pg_class parent ON parent.oid=i.inhparent WHERE ns.nspname=current_schema() AND parent.relname IN('chat_messages_global','chat_messages_planet','chat_messages_sector','chat_messages_corporation','chat_messages_private')`)
	if e != nil {
		return e
	}
	names := []string{}
	for rows.Next() {
		var name string
		if e = rows.Scan(&name); e != nil {
			rows.Close()
			return e
		}
		names = append(names, name)
	}
	e = rows.Err()
	rows.Close()
	if e != nil {
		return e
	}
	for _, name := range names {
		kind := ""
		for _, k := range []string{"global", "planet", "sector", "corporation", "private"} {
			if strings.HasPrefix(name, "chat_messages_"+k+"_") {
				kind = k
				break
			}
		}
		if kind == "" {
			continue
		}
		date := strings.TrimSuffix(strings.TrimPrefix(name, "chat_messages_"+kind+"_"), "_week")
		start, e := time.Parse("20060102", date)
		if e != nil {
			continue
		}
		days := map[string]int{"global": 30, "planet": 30, "sector": 7, "corporation": 90, "private": 180}[kind]
		step := 24 * time.Hour
		if kind == "corporation" || kind == "private" {
			step *= 7
		}
		if start.Add(step).Add(time.Duration(days) * 24 * time.Hour).After(now) {
			continue
		}
		var occupied bool
		if e = q.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM `+pgx.Identifier{name}.Sanitize()+` LIMIT 1)`).Scan(&occupied); e != nil {
			return e
		}
		if !occupied {
			if _, e = q.Exec(ctx, fmt.Sprintf("DROP TABLE IF EXISTS %s", pgx.Identifier{name}.Sanitize())); e != nil {
				return e
			}
		}
	}
	return nil
}
