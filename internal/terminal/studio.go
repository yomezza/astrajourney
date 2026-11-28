package terminal

import (
	"context"
	"errors"
	"net/http"
	"reflect"
	"strconv"
	"strings"

	"github.com/jackc/pgx/v5"

	"github.com/yomezza/cosmosimulator/internal/account"
	"github.com/yomezza/cosmosimulator/internal/commands"
	"github.com/yomezza/cosmosimulator/internal/events"
	"github.com/yomezza/cosmosimulator/internal/fleet"
	"github.com/yomezza/cosmosimulator/internal/navigation"
	"github.com/yomezza/cosmosimulator/internal/platform/httpkit"
	"github.com/yomezza/cosmosimulator/internal/platform/idempotency"
	"github.com/yomezza/cosmosimulator/internal/universe"
	"github.com/yomezza/cosmosimulator/internal/wallet"
)

type studioCommand struct {
	Request      string `json:"request,omitempty"`
	Action       string `json:"action"`
	Name         string `json:"name,omitempty"`
	Quantity     int    `json:"quantity,omitempty"`
	ID           int64  `json:"id,omitempty"`
	Waypoints    []int  `json:"waypoints,omitempty"`
	Star         int    `json:"star,omitempty"`
	Target       string `json:"target,omitempty"`
	Mode         string `json:"mode,omitempty"`
	Station      string `json:"station,omitempty"`
	Price        int64  `json:"price,omitempty"`
	Confirm      bool   `json:"confirm,omitempty"`
	ExpectedCost *int64 `json:"expected_cost,omitempty"`
	Note         string `json:"note,omitempty"`
	Bookmark     bool   `json:"bookmark,omitempty"`
}

func (in studioCommand) valid(preview bool) bool {
	if preview {
		if in.Request != "" || in.Action != "route" && in.Action != "move" && in.Action != "refuel" && in.Action != "rescue" {
			return false
		}
	} else if len(in.Request) < 16 || len(in.Request) > 64 || strings.ContainsAny(in.Request, "\r\n\x00") {
		return false
	}
	remaining := in
	remaining.Action, remaining.Request = "", ""
	switch in.Action {
	case "rename_ship":
		remaining.Name = ""
	case "refuel":
		if in.Quantity != 1 {
			return false
		}
		remaining.Quantity = 0
	case "module_install", "module_remove", "module_sell":
		if in.ID < 1 {
			return false
		}
		remaining.ID = 0
	case "module_buy":
		if in.Star < 1 || in.Station == "" || len(in.Station) > 80 || in.Target == "" || len(in.Target) > 80 || in.Price < 1 {
			return false
		}
		remaining.Star, remaining.Station, remaining.Target, remaining.Price = 0, "", "", 0
	case "route":
		if len(in.Waypoints) < 1 || len(in.Waypoints) > 20 {
			return false
		}
		for _, to := range in.Waypoints {
			if to < 1 {
				return false
			}
		}
		remaining.Waypoints = nil
	case "cancel_route", "module_scoop":
	case "move":
		if in.Star < 1 || in.Target == "" || len(in.Target) > 80 || in.Mode != "approach" && in.Mode != "orbit" && in.Mode != "docked" {
			return false
		}
		remaining.Star, remaining.Target, remaining.Mode = 0, "", ""
	case "portal_travel":
		if in.Target == "" || len(in.Target) > 80 {
			return false
		}
		remaining.Target = ""
	case "rescue":
		if !preview {
			if !in.Confirm || in.ExpectedCost == nil || *in.ExpectedCost < 0 {
				return false
			}
			remaining.Confirm, remaining.ExpectedCost = false, nil
		}
	case "note":
		if in.Star < 1 || len(in.Target) > 80 || len(in.Note) > 2000 {
			return false
		}
		remaining.Star, remaining.Target, remaining.Note, remaining.Bookmark = 0, "", "", false
	default:
		return false
	}
	return reflect.DeepEqual(remaining, studioCommand{})
}

func studioShipID(w http.ResponseWriter, r *http.Request) (int64, bool) {
	id, err := strconv.ParseInt(r.PathValue("ship_id"), 10, 64)
	if err != nil || id < 1 {
		httpkit.Fail(w, 400, "Укажите корабль")
		return 0, false
	}
	return id, true
}

func studioError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, pgx.ErrNoRows):
		httpkit.Fail(w, 404, "Объект недоступен")
	case errors.Is(err, fleet.ErrReserved):
		httpkit.Fail(w, 409, err.Error())
	default:
		var problem actionProblem
		if errors.As(err, &problem) {
			httpkit.Fail(w, 409, problem.Error())
		} else {
			httpkit.Internal(w, err)
		}
	}
}

func studioGameError(err error) error {
	if err == nil || errors.Is(err, pgx.ErrNoRows) || errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
		return err
	}
	var pgerr interface{ SQLState() string }
	if errors.As(err, &pgerr) {
		if pgerr.SQLState() == "23505" {
			return actionProblem("Такое название уже используется")
		}
		return err
	}
	return actionProblem(err.Error())
}

func (a *Service) StudioCommand(w http.ResponseWriter, r *http.Request) { a.studioAction(w, r, false) }
func (a *Service) StudioPreview(w http.ResponseWriter, r *http.Request) { a.studioAction(w, r, true) }

func (a *Service) studioAction(w http.ResponseWriter, r *http.Request, preview bool) {
	u, ok := a.Auth.User(w, r)
	if !ok {
		return
	}
	sid, ok := studioShipID(w, r)
	if !ok {
		return
	}
	var in studioCommand
	if !httpkit.Decode(w, r, &in) {
		return
	}
	if !in.valid(preview) {
		httpkit.Fail(w, 400, "Некорректная команда")
		return
	}
	ctx := r.Context()
	options := pgx.TxOptions{}
	if preview {
		options = pgx.TxOptions{IsoLevel: pgx.RepeatableRead, AccessMode: pgx.ReadOnly}
	}
	tx, err := a.DB.BeginTx(ctx, options)
	if err != nil {
		httpkit.Internal(w, err)
		return
	}
	defer tx.Rollback(context.Background())
	if !preview {
		if _, err = tx.Exec(ctx, `SELECT pg_advisory_xact_lock(784218)`); err != nil {
			httpkit.Internal(w, err)
			return
		}
		err = tx.QueryRow(ctx, `SELECT credits FROM users WHERE id=$1 FOR UPDATE`, u.ID).Scan(&u.Credits)
		if err != nil {
			studioError(w, err)
			return
		}
	}
	ship, err := fleet.ReadOwnedShip(ctx, tx, u.ID, sid)
	if err != nil {
		studioError(w, err)
		return
	}
	digest := idempotency.Digest(struct {
		Ship    int64
		Command studioCommand
	}{sid, in})
	if !preview {
		var saved string
		err = tx.QueryRow(ctx, `SELECT digest FROM pilot_studio_commands WHERE user_id=$1 AND request_id=$2`, u.ID, in.Request).Scan(&saved)
		if err == nil {
			if digest != saved {
				httpkit.Fail(w, 409, "Этот запрос уже использован для другой команды")
				return
			}
			httpkit.Reply(w, 200, map[string]bool{"ok": true})
			return
		}
		if !errors.Is(err, pgx.ErrNoRows) {
			httpkit.Internal(w, err)
			return
		}
		if err = fleet.SettleShip(ctx, tx, u.ID, sid); err != nil {
			studioError(w, err)
			return
		}
		ship, err = fleet.ReadOwnedShip(ctx, tx, u.ID, sid)
		if err != nil {
			studioError(w, err)
			return
		}
		if err = wallet.Reason(ctx, tx, in.Action); err != nil {
			httpkit.Internal(w, err)
			return
		}
	}
	g, err := a.World.LoadReadOnly(ctx, tx)
	if err != nil {
		httpkit.Internal(w, err)
		return
	}
	if ship.Star < 1 || ship.Star > len(g.Stars) || g.Stars[ship.Star-1].Deleted {
		httpkit.Fail(w, 409, "Система корабля недоступна")
		return
	}
	out, err := a.applyStudioAction(ctx, tx, u, &ship, g, in, preview)
	if err != nil {
		studioError(w, err)
		return
	}
	if preview {
		httpkit.Reply(w, 200, out)
		return
	}
	if err = events.RecordEvent(ctx, tx, u.ID, in.Action, map[string]any{"ship": sid, "star": ship.Star, "object": in.Target, "quantity": in.Quantity, "id": in.ID}); err != nil {
		httpkit.Internal(w, err)
		return
	}
	if _, err = tx.Exec(ctx, `INSERT INTO pilot_studio_commands(user_id,request_id,digest) VALUES($1,$2,$3)`, u.ID, in.Request, digest); err != nil {
		httpkit.Internal(w, err)
		return
	}
	if err = tx.Commit(ctx); err != nil {
		httpkit.Internal(w, err)
		return
	}
	httpkit.Reply(w, 200, map[string]bool{"ok": true})
}

func (a *Service) applyStudioAction(ctx context.Context, tx pgx.Tx, u account.User, ship *fleet.Ship, g *universe.Galaxy, in studioCommand, preview bool) (any, error) {
	if fleet.IsShipOperation(in.Action) || in.Action == "route" || in.Action == "move" || in.Action == "refuel" {
		if err := fleet.Available(ctx, tx, ship.ID); err != nil {
			return nil, err
		}
		busy, err := fleet.AutomationBusy(ctx, tx, ship.ID)
		if err != nil {
			return nil, err
		}
		if busy {
			return nil, actionProblem("Приостановите задачу планировщика")
		}
	}
	switch in.Action {
	case "route":
		if len(ship.Waypoints) > 0 {
			return nil, actionProblem("Сначала отмените маршрут")
		}
		if preview {
			if err := fleet.Idle(ctx, tx, *ship); err != nil {
				return nil, studioGameError(err)
			}
			plan, err := navigation.PlanRoute(g, *ship, in.Waypoints)
			if err != nil {
				return nil, studioGameError(err)
			}
			return struct {
				navigation.RoutePlan
				Action    string `json:"action"`
				Available int    `json:"available_fuel"`
				Reserved  int    `json:"reserved_fuel"`
			}{plan, in.Action, max(0, ship.Fuel-ship.ReservedFuel), ship.ReservedFuel}, nil
		}
	case "refuel":
		if ship.Destination != nil || ship.LocalTarget != nil {
			return nil, actionProblem("Корабль в пути")
		}
		amount, err := refuelAmount(*ship)
		if err != nil {
			return nil, err
		}
		if preview {
			return map[string]any{"action": in.Action, "resource": "agni", "quantity": 1, "fuel": amount, "cost": 0}, nil
		}
		refuel(ship, amount)
		return nil, fleet.SaveShip(ctx, tx, u.ID, *ship)
	case "move":
		if in.Star != ship.Star {
			return nil, actionProblem("Система корабля изменилась")
		}
		if err := planLocalMove(ctx, tx, u.ID, *ship, g.Stars[ship.Star-1], in.Target, in.Mode); err != nil {
			return nil, err
		}
		if preview {
			return map[string]any{"action": in.Action, "target": in.Target, "mode": in.Mode, "seconds": localMoveSeconds, "fuel": localMoveFuel}, nil
		}
		startLocalMove(ship, in.Target, in.Mode)
		return nil, fleet.SaveShip(ctx, tx, u.ID, *ship)
	case "rescue":
		if err := fleet.Idle(ctx, tx, *ship); err != nil {
			return nil, studioGameError(err)
		}
		if preview {
			plan, err := navigation.PlanRescue(ctx, tx, g, *ship)
			if err != nil {
				return nil, studioGameError(err)
			}
			var credits int64
			if err = tx.QueryRow(ctx, `SELECT credits FROM users WHERE id=$1`, u.ID).Scan(&credits); err != nil {
				return nil, err
			}
			return struct {
				navigation.RescuePlan
				Action    string `json:"action"`
				CanAfford bool   `json:"can_afford"`
			}{plan, in.Action, credits >= plan.Cost}, nil
		}
		var cost int64
		if err := tx.QueryRow(ctx, `SELECT rescue_cost FROM game_settings WHERE id=1`).Scan(&cost); err != nil {
			return nil, err
		}
		if *in.ExpectedCost != cost {
			return nil, actionProblem("Стоимость спасения изменилась")
		}
	case "note":
		if in.Star > len(g.Stars) || g.Stars[in.Star-1].Deleted ||
			universe.GalaxyKey(g.Stars[in.Star-1]) != universe.GalaxyKey(g.Stars[ship.Star-1]) {
			return nil, pgx.ErrNoRows
		}
		if in.Target != "" {
			if in.Star != ship.Star {
				return nil, pgx.ErrNoRows
			}
			sites, err := localSites(ctx, tx, u.ID, g.Stars[in.Star-1])
			if err != nil {
				return nil, err
			}
			found := false
			for _, site := range sites {
				found = found || in.Target == site.ID
			}
			if !found {
				return nil, pgx.ErrNoRows
			}
		}
	}
	_, err := a.Navigation.NavigationCommand(ctx, tx, u, ship, g, commands.Command{
		Action: in.Action, ID: in.ID, Ship: ship.ID, Name: in.Name, Waypoints: in.Waypoints, Star: in.Star,
		Station: in.Station, Target: in.Target, Price: in.Price, Note: in.Note, Bookmark: in.Bookmark,
	})
	return nil, studioGameError(err)
}
