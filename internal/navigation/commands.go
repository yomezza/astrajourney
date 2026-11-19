package navigation

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/jackc/pgx/v5"

	"github.com/yomezza/cosmosimulator/internal/account"
	"github.com/yomezza/cosmosimulator/internal/commands"
	"github.com/yomezza/cosmosimulator/internal/fleet"
	"github.com/yomezza/cosmosimulator/internal/gameplay"
	textutil "github.com/yomezza/cosmosimulator/internal/platform/text"
	"github.com/yomezza/cosmosimulator/internal/research"
	"github.com/yomezza/cosmosimulator/internal/universe"
)

func (a *Service) NavigationCommand(ctx context.Context, tx pgx.Tx, u account.User, s *fleet.Ship, g *universe.Galaxy, in commands.Command) (bool, error) {
	switch in.Action {
	case "route", "explore", "rescue", "portal_travel", "queue_route", "cancel_route", "resume_research":
		if err := fleet.Available(ctx, tx, s.ID); err != nil {
			return true, err
		}
	}
	switch in.Action {
	case "rename_ship":
		return true, fleet.RenameShip(ctx, tx, *s, in.Name)
	case "select_ship":
		_, err := tx.Exec(ctx, `UPDATE ships SET active=FALSE WHERE user_id=$1 AND active`, u.ID)
		if err == nil {
			_, err = tx.Exec(ctx, `UPDATE ships SET active=TRUE WHERE id=$1 AND user_id=$2`, s.ID, u.ID)
		}
		return true, err
	case "note":
		if in.Star < 1 || in.Star > len(g.Stars) || len(in.Note) > 2000 || len(in.Target) > 80 {
			return true, errors.New("Некорректная заметка")
		}
		_, err := tx.Exec(ctx, `INSERT INTO navigation_notes(user_id,star,object_id,note,bookmark) VALUES($1,$2,$3,$4,$5) ON CONFLICT(user_id,star,object_id) DO UPDATE SET note=EXCLUDED.note,bookmark=EXCLUDED.bookmark`, u.ID, in.Star, in.Target, in.Note, in.Bookmark)
		return true, err
	case "station_label":
		in.Name = strings.TrimSpace(in.Name)
		in.Note = strings.TrimSpace(strings.ReplaceAll(strings.ReplaceAll(in.Note, "\r\n", "\n"), "\r", "\n"))
		if !textutil.Valid(in.Name, false) || utf8.RuneCountInString(in.Name) > 60 || !textutil.Valid(in.Note, true) || utf8.RuneCountInString(in.Note) > 800 {
			return true, errors.New("Личное название: до 60 символов, заметка: до 800")
		}
		var exists bool
		if err := tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM stations WHERE star=$1 AND object_id=$2 AND active)`, in.Star, in.Target).Scan(&exists); err != nil {
			return true, err
		}
		if !exists {
			return true, errors.New("Станция не найдена")
		}
		_, err := tx.Exec(ctx, `INSERT INTO navigation_notes(user_id,star,object_id,note,label) VALUES($1,$2,$3,$4,$5) ON CONFLICT(user_id,star,object_id) DO UPDATE SET note=EXCLUDED.note,label=EXCLUDED.label`, u.ID, in.Star, in.Target, in.Note, in.Name)
		return true, err
	case "pause_research":
		tag, err := tx.Exec(ctx, `UPDATE research_jobs SET paused=TRUE WHERE user_id=$1 AND ship_id=$2`, u.ID, s.ID)
		if err == nil && tag.RowsAffected() == 0 {
			return true, errors.New("Исследование не найдено")
		}
		return true, err
	case "resume_research":
		var star int
		if err := tx.QueryRow(ctx, `SELECT star FROM research_jobs WHERE ship_id=$1`, s.ID).Scan(&star); err != nil {
			return true, err
		}
		if s.Destination != nil || s.Star != star {
			return true, errors.New("Вернитесь в систему исследования")
		}
		return true, research.StartResearch(ctx, tx, u.ID, s, g.Stars[star-1], g)
	case "explore", "route":
		if err := fleet.Idle(ctx, tx, *s); err != nil {
			return true, err
		}
		points := in.Waypoints
		if in.Action == "explore" {
			points = []int{in.To}
		}
		if len(points) == 0 || len(points) > 20 {
			return true, errors.New("Укажите от 1 до 20 точек маршрута")
		}
		total := 0
		jumpDuration := 0
		from := s.Star
		for _, to := range points {
			if to == from {
				continue
			}
			path := g.Path(from, to)
			if len(path) < 2 {
				return true, errors.New("Нет допустимого маршрута")
			}
			total += s.Hull().JumpFuel * (len(path) - 1)
			jumpDuration += s.Hull().JumpSeconds * (len(path) - 1)
			from = to
		}
		if total > s.Fuel {
			return true, errors.New("Недостаточно топлива")
		}
		clean := []int{}
		prev := s.Star
		for _, to := range points {
			if to != prev {
				clean = append(clean, to)
				prev = to
			}
		}
		raw, _ := json.Marshal(clean)
		if _, err := tx.Exec(ctx, `UPDATE ships SET waypoints=$2,reserved_fuel=$3 WHERE id=$1`, s.ID, raw, total); err != nil {
			return true, err
		}
		if in.Action == "explore" {
			if in.To < 1 || in.To > len(g.Stars) {
				return true, errors.New("Некорректная система")
			}
			s.Arrival = nil
			if total > 0 {
				at := time.Now().Add(time.Duration(jumpDuration) * time.Second)
				s.Arrival = &at
			}
			return true, research.StartResearch(ctx, tx, u.ID, s, g.Stars[in.To-1], g)
		}
		return true, nil
	case "cancel_route":
		_, err := tx.Exec(ctx, `UPDATE ships SET waypoints='[]',reserved_fuel=0 WHERE id=$1`, s.ID)
		return true, err
	case "rescue":
		if err := fleet.Idle(ctx, tx, *s); err != nil {
			return true, err
		}
		settings, err := gameplay.ReadGameplay(ctx, tx)
		if err != nil {
			return true, err
		}
		path := []int{}
		target := 0
		for _, candidate := range g.Stars {
			var available bool
			if err := tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM stations WHERE star=$1 AND active)`, candidate.ID).Scan(&available); err != nil {
				return true, err
			}
			if !available {
				continue
			}
			p := g.Path(s.Star, candidate.ID)
			if len(p) > 0 && (target == 0 || len(p) < len(path)) {
				path = p
				target = candidate.ID
			}
		}
		if target == 0 {
			return true, errors.New("В этом компоненте карты нет доступного порта")
		}
		tag, err := tx.Exec(ctx, `UPDATE users SET credits=credits-$2 WHERE id=$1 AND credits>=$2`, u.ID, settings.RescueCost)
		if err != nil {
			return true, err
		}
		if tag.RowsAffected() == 0 {
			return true, errors.New("Недостаточно кредитов")
		}
		route, _ := json.Marshal(path)
		_, err = tx.Exec(ctx, `UPDATE ships SET destination=$2,departure=now(),arrival=now()+interval '30 seconds',travel_path=$3,waypoints='[]',reserved_fuel=0,fuel=GREATEST(fuel,10) WHERE id=$1`, s.ID, target, route)
		return true, err
	case "buy_ship":
		return true, fleet.BuyShip(ctx, tx, u, *s, in)
	case "module_buy", "module_install", "module_remove", "module_sell", "module_scoop":
		return fleet.ModuleCommand(ctx, tx, u.ID, s, in)
	case "extractor":
		return true, errors.New("Добытчики теперь устанавливаются в оснащении корабля")
	case "portal_contribute", "portal_travel":
		return true, a.portalCommand(ctx, tx, u, s, g, in)
	}
	return false, nil
}
