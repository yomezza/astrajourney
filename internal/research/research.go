package research

import (
	"context"
	"errors"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/yomezza/cosmosimulator/internal/fleet"
	"github.com/yomezza/cosmosimulator/internal/gameplay"
	"github.com/yomezza/cosmosimulator/internal/universe"
	"github.com/yomezza/cosmosimulator/internal/worldstore"
)

func StartResearch(ctx context.Context, tx pgx.Tx, uid int64, s *fleet.Ship, star universe.Star, g *universe.Galaxy) error {
	var known bool
	if err := tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM surveys WHERE user_id=$1 AND star=$2)`, uid, star.ID).Scan(&known); err != nil {
		return err
	}
	if known {
		return nil
	}
	settings, err := gameplay.ReadGameplay(ctx, tx)
	if err != nil {
		return err
	}
	seconds := s.ScanSeconds(settings.ScanSeconds)
	var existing int64
	err = tx.QueryRow(ctx, `SELECT ship_id FROM research_jobs WHERE user_id=$1 AND star=$2`, uid, star.ID).Scan(&existing)
	if err == nil {
		if existing != s.ID {
			return errors.New("Исследование выполняет другой корабль")
		}
		_, err = tx.Exec(ctx, `UPDATE research_jobs SET paused=FALSE,seconds=$2,next_at=now()+make_interval(secs=>$2) WHERE ship_id=$1`, s.ID, seconds)
		return err
	}
	if !errors.Is(err, pgx.ErrNoRows) {
		return err
	}
	tag, err := tx.Exec(ctx, `UPDATE users SET credits=credits-10 WHERE id=$1 AND credits>=10`, uid)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return errors.New("Недостаточно кредитов")
	}
	researchSites, err := worldstore.AddCustomSites(ctx, tx, star, universe.SystemSites(star))
	if err != nil {
		return err
	}
	_, err = tx.Exec(ctx, `INSERT INTO research_jobs(ship_id,user_id,star,total,next_at,seconds) VALUES($1,$2,$3,$4,COALESCE($5,now())+make_interval(secs=>$6),$6)`, s.ID, uid, star.ID, len(researchSites), s.Arrival, seconds)
	return err
}

func ResearchView(ctx context.Context, db interface {
	Query(context.Context, string, ...any) (pgx.Rows, error)
}, uid int64) []map[string]any {
	rows, err := db.Query(ctx, `SELECT ship_id,star,scanned,total,next_at,paused,seconds FROM research_jobs WHERE user_id=$1 ORDER BY ship_id`, uid)
	if err != nil {
		return []map[string]any{}
	}
	defer rows.Close()
	out := []map[string]any{}
	for rows.Next() {
		var sid int64
		var star, n, total, sec int
		var at time.Time
		var paused bool
		if rows.Scan(&sid, &star, &n, &total, &at, &paused, &sec) != nil {
			break
		}
		out = append(out, map[string]any{"ship": sid, "star": star, "scanned": n, "total": total, "next_at": at, "paused": paused, "seconds": sec})
	}
	return out
}
