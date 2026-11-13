package navigation

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/yomezza/cosmosimulator/internal/discovery"
	"github.com/yomezza/cosmosimulator/internal/events"
	"github.com/yomezza/cosmosimulator/internal/fleet"
	"github.com/yomezza/cosmosimulator/internal/universe"
)

func (a *Service) AdvancePlayer(ctx context.Context, uid int64, g *universe.Galaxy) error {
	tx, err := a.DB.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(context.Background())
	if _, err = tx.Exec(ctx, `SELECT pg_advisory_xact_lock(784218)`); err != nil {
		return err
	}
	g, err = a.World.LoadFrom(ctx, tx)
	if err != nil {
		return err
	}
	if _, err = tx.Exec(ctx, `SELECT id FROM users WHERE id=$1 FOR UPDATE`, uid); err != nil {
		return err
	}
	if err = fleet.SettleShips(ctx, tx, uid); err != nil {
		return err
	}
	rows, err := tx.Query(ctx, `SELECT r.ship_id,r.star,r.scanned,r.total,r.next_at,r.seconds FROM research_jobs r JOIN ships s ON s.id=r.ship_id WHERE r.user_id=$1 AND NOT r.paused AND s.destination IS NULL AND s.local_target IS NULL AND s.star=r.star AND r.next_at<=now() ORDER BY r.ship_id FOR UPDATE OF r`, uid)
	if err != nil {
		return err
	}
	type scan struct {
		sid                 int64
		star, n, total, sec int
		at                  time.Time
	}
	jobs := []scan{}
	for rows.Next() {
		var j scan
		if err = rows.Scan(&j.sid, &j.star, &j.n, &j.total, &j.at, &j.sec); err != nil {
			rows.Close()
			return err
		}
		jobs = append(jobs, j)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return err
	}
	for _, j := range jobs {
		steps := 1 + int(time.Since(j.at).Seconds())/j.sec
		n := min(j.total, j.n+steps)
		if j.star < 1 || j.star > len(g.Stars) {
			return fmt.Errorf("invalid scan star")
		}
		if err = discovery.ClaimScan(ctx, tx, uid, g.Stars[j.star-1], j.n, n, false); err != nil {
			return err
		}
		if n == j.total {
			tag, e := tx.Exec(ctx, `INSERT INTO surveys(user_id,star,source) VALUES($1,$2,'survey') ON CONFLICT DO NOTHING`, uid, j.star)
			if e != nil {
				return e
			}
			if tag.RowsAffected() > 0 {
				if err = discovery.ClaimScan(ctx, tx, uid, g.Stars[j.star-1], n, n, true); err != nil {
					return err
				}
				if _, err = tx.Exec(ctx, `UPDATE users SET xp=xp+10 WHERE id=$1`, uid); err != nil {
					return err
				}
				if err = events.RecordEvent(ctx, tx, uid, "research_complete", map[string]any{"star": j.star, "ship": j.sid}); err != nil {
					return err
				}
			}
			_, err = tx.Exec(ctx, `DELETE FROM research_jobs WHERE ship_id=$1`, j.sid)
		} else {
			_, err = tx.Exec(ctx, `UPDATE research_jobs SET scanned=$2,next_at=next_at+make_interval(secs=>$3) WHERE ship_id=$1`, j.sid, n, steps*j.sec)
		}
		if err != nil {
			return err
		}
	}
	rows, err = tx.Query(ctx, `SELECT id FROM ships s WHERE user_id=$1 AND destination IS NULL AND local_target IS NULL AND jsonb_array_length(waypoints)>0 AND NOT EXISTS(SELECT 1 FROM planet_transfer_controls pc WHERE pc.reserved_ship=s.id) AND NOT EXISTS(SELECT 1 FROM mining_jobs WHERE ship_id=s.id) ORDER BY id`, uid)
	if err != nil {
		return err
	}
	sids := []int64{}
	for rows.Next() {
		var sid int64
		if err = rows.Scan(&sid); err != nil {
			rows.Close()
			return err
		}
		sids = append(sids, sid)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return err
	}
	for _, sid := range sids {
		s, e := fleet.ReadShip(ctx, tx, sid)
		if e != nil {
			return e
		}
		if len(s.Waypoints) == 0 {
			continue
		}
		path := g.Path(s.Star, s.Waypoints[0])
		if len(path) < 2 {
			return fmt.Errorf("invalid persisted route")
		}
		cost := s.Hull().JumpFuel * (len(path) - 1)
		if s.Fuel < cost {
			return errors.New("reserved route fuel missing")
		}
		raw, _ := json.Marshal(s.Waypoints[1:])
		route, _ := json.Marshal(path)
		_, err = tx.Exec(ctx, `UPDATE ships SET fuel=fuel-$2,reserved_fuel=GREATEST(0,reserved_fuel-$2),destination=$3,departure=now(),arrival=now()+make_interval(secs=>$4),travel_path=$5,waypoints=$6 WHERE id=$1`, sid, cost, s.Waypoints[0], s.Hull().JumpSeconds*(len(path)-1), route, raw)
		if err != nil {
			return err
		}
	}
	return tx.Commit(ctx)
}
