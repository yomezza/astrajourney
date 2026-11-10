package terminal

import (
	"context"
	"errors"
	"net/http"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/yomezza/cosmosimulator/internal/chatintegration"
	"github.com/yomezza/cosmosimulator/internal/commands"
	tradecatalog "github.com/yomezza/cosmosimulator/internal/commerce/catalog"
	"github.com/yomezza/cosmosimulator/internal/corporations"
	"github.com/yomezza/cosmosimulator/internal/events"
	"github.com/yomezza/cosmosimulator/internal/fleet"
	"github.com/yomezza/cosmosimulator/internal/platform/httpkit"
	"github.com/yomezza/cosmosimulator/internal/platform/postgres"
	"github.com/yomezza/cosmosimulator/internal/research"
	"github.com/yomezza/cosmosimulator/internal/universe"
	"github.com/yomezza/cosmosimulator/internal/wallet"
)

func (a *Service) Workbench(w http.ResponseWriter, r *http.Request) {
	u, ok := a.Auth.User(w, r)
	if !ok {
		return
	}
	ctx := r.Context()
	g, err := a.World.Load(ctx)
	if err != nil {
		httpkit.Internal(w, err)
		return
	}
	// Settling a player is transactional and also works without the background loop.
	if err = a.Navigation.AdvancePlayer(ctx, u.ID, g); err != nil {
		httpkit.Internal(w, err)
		return
	}
	if err = a.World.SeedStations(ctx, g); err != nil {
		httpkit.Internal(w, err)
		return
	}
	if _, err = a.DB.Exec(ctx, `UPDATE users SET last_active=now() WHERE id=$1`, u.ID); err != nil {
		httpkit.Internal(w, err)
		return
	}
	var company *int64
	if err = a.DB.QueryRow(ctx, `SELECT company_id FROM users WHERE id=$1`, u.ID).Scan(&company); err != nil {
		httpkit.Internal(w, err)
		return
	}
	out := map[string]any{"universe_revision": g.Revision, "research": research.ResearchView(ctx, a.DB, u.ID), "core_systems": universe.CoreSystems(g), "portal_cores": universe.PortalCores(g), "server_name": "Иггдрасиль", "ship_classes": fleet.Catalogue, "trade_profiles": tradecatalog.TradeProfiles, "extractor_price": 250, "reservation_policy": "full_refund", "time": time.Now()}
	queries := []struct {
		key, sql string
		args     []any
	}{
		{"mining", `SELECT j.*,s.name AS ship_name FROM mining_jobs j JOIN ships s ON s.id=j.ship_id WHERE j.user_id=$1 ORDER BY j.ship_id`, []any{u.ID}},
		{"shipyards", `SELECT y.*,s.name AS station_name,s.specialization FROM shipyard_stock y JOIN stations s ON s.star=y.star AND s.object_id=y.station WHERE s.shipyard AND s.active ORDER BY y.star,y.station,y.ship_class`, nil},
		{"known", `SELECT star,received_at,source FROM surveys WHERE user_id=$1 ORDER BY star`, []any{u.ID}},
		{"notes", `SELECT star,object_id,note,bookmark,label FROM navigation_notes WHERE user_id=$1`, []any{u.ID}},
		{"stations", `SELECT s.*,c.name AS founder_name,c.color AS founder_color,COALESCE(o.name,c.name) AS owner_name,COALESCE(o.tag,c.tag) AS owner_tag,COALESCE(o.color,c.color) AS owner_color,CASE WHEN s.company_id IS NOT NULL THEN 'company' WHEN s.founder IS NOT NULL THEN 'founder' ELSE 'neutral' END AS owner_kind,COALESCE(r.points,0) AS reputation,CASE WHEN c.founder_key='founder-02' AND COALESCE(r.points,0)<20 THEN 2 ELSE 0 END AS fee_percent FROM stations s LEFT JOIN companies c ON c.id=s.founder LEFT JOIN companies o ON o.id=s.company_id LEFT JOIN reputation r ON r.company_id=s.founder AND r.user_id=$1 WHERE s.active ORDER BY s.star,s.object_id`, []any{u.ID}},
		{"data_offers", `SELECT o.*,u.username AS seller_name,(SELECT count(*) FROM unnest(o.systems) s WHERE NOT EXISTS(SELECT 1 FROM surveys v WHERE v.user_id=$1 AND v.star=s)) AS new_systems FROM survey_offers o JOIN users u ON u.id=o.seller ORDER BY o.id DESC LIMIT 500`, []any{u.ID}},
		{"warehouses", `SELECT * FROM warehouses WHERE user_id=$1 ORDER BY star,station`, []any{u.ID}},
		{"orders", `SELECT o.*,u.username AS seller_name FROM sell_orders o JOIN users u ON u.id=o.seller WHERE quantity>0 ORDER BY id DESC LIMIT 500`, nil},
		{"deliveries", `SELECT d.*,u.username AS owner_name,c.username AS carrier_name FROM deliveries d JOIN users u ON u.id=d.owner LEFT JOIN users c ON c.id=d.carrier ORDER BY id DESC LIMIT 200`, nil},
		{"ledger", `SELECT id,amount,balance,reason,created_at FROM financial_ledger WHERE user_id=$1 ORDER BY id DESC LIMIT 100`, []any{u.ID}},
		{"members", `SELECT u.id,u.username,u.company_role,COALESCE(r.name,CASE WHEN c.owner_id=u.id THEN 'Владелец' ELSE 'Участник' END) AS role_name FROM users u JOIN companies c ON c.id=u.company_id LEFT JOIN company_roles r ON r.id=u.company_role WHERE u.company_id=$1 ORDER BY u.id`, []any{company}},
		{"roles", `SELECT * FROM company_roles WHERE company_id=$1 ORDER BY id`, []any{company}},
		{"requests", `SELECT r.*,u.username FROM company_requests r JOIN users u ON u.id=r.user_id WHERE (r.company_id=$1 OR r.user_id=$2) ORDER BY r.id DESC`, []any{company, u.ID}},
		{"company_finance", `SELECT f.*,u.username FROM company_finance f LEFT JOIN users u ON u.id=f.actor WHERE f.company_id=$1 ORDER BY f.id DESC LIMIT 100`, []any{company}},
		{"company_storage", `SELECT * FROM company_storage WHERE company_id=$1 ORDER BY star,station`, []any{company}},
		{"projects", `SELECT * FROM company_projects WHERE company_id=$1 ORDER BY id`, []any{company}},
		{"project_contributions", `SELECT x.*,u.username FROM project_contributions x JOIN company_projects p ON p.id=x.project_id LEFT JOIN users u ON u.id=x.user_id WHERE p.company_id=$1 ORDER BY x.id DESC LIMIT 100`, []any{company}},
		{"portal", `SELECT * FROM portal`, nil},
		{"reputation", `SELECT r.*,c.name FROM reputation r JOIN companies c ON c.id=r.company_id WHERE user_id=$1`, []any{u.ID}},
		{"population", `SELECT count(*) FILTER(WHERE NOT is_bot AND virtual_run IS NULL AND last_active>now()-interval '5 minutes') AS players,count(*) FILTER(WHERE is_bot AND virtual_run IS NULL) AS bots FROM users`, nil},
	}
	for _, q := range queries {
		v, e := postgres.QueryMaps(ctx, a.DB, q.sql, q.args...)
		if e != nil {
			httpkit.Internal(w, e)
			return
		}
		if v == nil {
			v = []map[string]any{}
		}
		if q.key == "known" || q.key == "notes" {
			visible := v[:0]
			for _, row := range v {
				id := 0
				switch x := row["star"].(type) {
				case int32:
					id = int(x)
				case int64:
					id = int(x)
				case int:
					id = x
				}
				if id > 0 && id <= len(g.Stars) && !g.Stars[id-1].Deleted {
					visible = append(visible, row)
				}
			}
			v = visible
		}
		out[q.key] = v
	}
	if company != nil {
		var credits int64
		var owner *int64
		if err = a.DB.QueryRow(ctx, `SELECT credits,owner_id FROM companies WHERE id=$1`, *company).Scan(&credits, &owner); err != nil {
			httpkit.Internal(w, err)
			return
		}
		_, canFinance, accessErr := corporations.CompanyAccess(ctx, a.DB, u.ID, "finance")
		if accessErr != nil {
			httpkit.Internal(w, accessErr)
			return
		}
		if canFinance {
			out["company_balance"] = credits
		} else {
			out["company_balance"] = nil
			out["company_finance"] = []map[string]any{}
		}
		out["company_owner"] = owner
	}
	httpkit.Reply(w, 200, out)
}

func (a *Service) Command(w http.ResponseWriter, r *http.Request) {
	u, ok := a.Auth.User(w, r)
	if !ok {
		return
	}
	var in commands.Command
	if !httpkit.Decode(w, r, &in) {
		return
	}
	g, err := a.World.Load(r.Context())
	if err != nil {
		httpkit.Internal(w, err)
		return
	}
	ctx := r.Context()
	if err = a.World.SeedStations(ctx, g); err != nil {
		httpkit.Internal(w, err)
		return
	}
	tx, err := a.DB.Begin(ctx)
	if err != nil {
		httpkit.Internal(w, err)
		return
	}
	defer tx.Rollback(context.Background())
	if _, err = tx.Exec(ctx, `SELECT pg_advisory_xact_lock(784218)`); err != nil {
		httpkit.Internal(w, err)
		return
	}
	g, err = a.World.Load(ctx)
	if err != nil {
		httpkit.Internal(w, err)
		return
	}
	if err = tx.QueryRow(ctx, `SELECT credits FROM users WHERE id=$1 FOR UPDATE`, u.ID).Scan(&u.Credits); err != nil {
		httpkit.Internal(w, err)
		return
	}
	s, err := fleet.EnsureShip(ctx, tx, u.ID)
	if err != nil {
		httpkit.Internal(w, err)
		return
	}
	if in.Ship > 0 {
		var owner int64
		if err = tx.QueryRow(ctx, `SELECT user_id FROM ships WHERE id=$1`, in.Ship).Scan(&owner); err != nil || owner != u.ID {
			httpkit.Fail(w, 404, "Корабль не найден")
			return
		}
		s, err = fleet.ReadShip(ctx, tx, in.Ship)
		if err != nil {
			httpkit.Internal(w, err)
			return
		}
	}
	if err = wallet.Reason(ctx, tx, in.Action); err != nil {
		httpkit.Internal(w, err)
		return
	}
	if fleet.IsShipOperation(in.Action) {
		if busy, e := fleet.AutomationBusy(ctx, tx, s.ID); e != nil {
			httpkit.Internal(w, e)
			return
		} else if busy {
			httpkit.Fail(w, 409, "Приостановите задачу планировщика")
			return
		}
	}
	signalTarget, err := a.Corporations.SignalTarget(ctx, tx, u.ID, in)
	if err != nil {
		httpkit.Internal(w, err)
		return
	}
	handled, err := a.Navigation.NavigationCommand(ctx, tx, u, &s, g, in)
	if !handled {
		handled, err = a.Commerce.EconomyCommand(ctx, tx, u, &s, g, in)
	}
	if !handled {
		handled, err = a.Corporations.CorporationCommand(ctx, tx, u, &s, g, in)
	}
	if !handled {
		httpkit.Fail(w, 400, "Неизвестное действие")
		return
	}
	if err != nil {
		var pgerr interface{ SQLState() string }
		if errors.Is(err, pgx.ErrNoRows) {
			httpkit.Fail(w, 404, "Объект не найден")
		} else if errors.As(err, &pgerr) {
			if pgerr.SQLState() == "23505" {
				httpkit.Fail(w, 409, "Такое название уже используется")
			} else {
				httpkit.Internal(w, err)
			}
		} else {
			httpkit.Fail(w, 409, err.Error())
		}
		return
	}
	pendingDeletion := false
	if in.Action == "company_dissolve" {
		installed, e := chatintegration.Installed(ctx, tx)
		if e != nil {
			httpkit.Internal(w, e)
			return
		}
		if installed {
			if e = tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM companies WHERE owner_id=$1 AND chat_deleting)`, u.ID).Scan(&pendingDeletion); e != nil {
				httpkit.Internal(w, e)
				return
			}
		}
	}
	if err = events.RecordEvent(ctx, tx, u.ID, in.Action, map[string]any{"ship": s.ID, "star": s.Star, "resource": in.Resource, "quantity": in.Quantity, "amount": in.Amount}); err != nil {
		httpkit.Internal(w, err)
		return
	}
	signals, err := a.Corporations.StageCommand(ctx, tx, signalTarget, in.Action)
	if err != nil {
		httpkit.Internal(w, err)
		return
	}
	if in.Action == "portal_contribute" {
		err = a.World.Commit(ctx, tx)
	} else {
		err = tx.Commit(ctx)
	}
	if err != nil {
		httpkit.Internal(w, err)
		return
	}
	a.Corporations.EmitSignals(signals)
	if pendingDeletion {
		httpkit.Reply(w, 202, map[string]any{"ok": true, "status": "deleting"})
		return
	}
	httpkit.Reply(w, 200, map[string]bool{"ok": true})
}
