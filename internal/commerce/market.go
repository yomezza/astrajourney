package commerce

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"

	"github.com/jackc/pgx/v5"

	tradecatalog "github.com/yomezza/cosmosimulator/internal/commerce/catalog"
	"github.com/yomezza/cosmosimulator/internal/events"
	"github.com/yomezza/cosmosimulator/internal/fleet"
	"github.com/yomezza/cosmosimulator/internal/platform/httpkit"
	"github.com/yomezza/cosmosimulator/internal/platform/postgres"
	resourcecatalog "github.com/yomezza/cosmosimulator/internal/resources"
	"github.com/yomezza/cosmosimulator/internal/wallet"
	"github.com/yomezza/cosmosimulator/internal/worldstore"
)

// Reservations prepay stock held at the seller's station. No teleportation of cargo.
func (a *Service) GlobalMarket(w http.ResponseWriter, r *http.Request) {
	u, ok := a.Auth.User(w, r)
	if !ok {
		return
	}
	if r.Method == "GET" {
		g, err := a.World.Load(r.Context())
		if err != nil {
			httpkit.Internal(w, err)
			return
		}
		if err = a.World.SeedStations(r.Context(), g); err != nil {
			httpkit.Internal(w, err)
			return
		}
		ports, err := a.globalPorts(r.Context(), u.ID, g)
		if err != nil {
			httpkit.Internal(w, err)
			return
		}
		reservations, err := postgres.QueryMaps(r.Context(), a.DB, `SELECT id,star,station,resource,quantity,cost,status,expires_at FROM reservations WHERE user_id=$1 ORDER BY (status='reserved') DESC,id DESC LIMIT 200`, u.ID)
		if err != nil {
			httpkit.Internal(w, err)
			return
		}
		if reservations == nil {
			reservations = []map[string]any{}
		}

		httpkit.Reply(w, 200, map[string]any{"ports": ports, "reservations": reservations})
		return
	}
	var in struct {
		Action        string `json:"action"`
		Star          int    `json:"star"`
		Station       string `json:"station"`
		Resource      string `json:"resource"`
		Quantity      int    `json:"quantity"`
		ID            int64  `json:"id"`
		ExpectedPrice *int   `json:"expected_price"`
	}
	if !httpkit.Decode(w, r, &in) {
		return
	}
	g, err := a.World.Load(r.Context())
	if err != nil {
		httpkit.Internal(w, err)
		return
	}
	tx, err := a.DB.Begin(r.Context())
	if err != nil {
		httpkit.Internal(w, err)
		return
	}
	defer tx.Rollback(context.Background())
	if r.Method == "POST" {
		if _, err = tx.Exec(r.Context(), `SELECT pg_advisory_xact_lock(784218)`); err != nil {
			httpkit.Internal(w, err)
			return
		}
	}
	var credits int64
	if err = tx.QueryRow(r.Context(), `SELECT credits FROM users WHERE id=$1 FOR UPDATE`, u.ID).Scan(&credits); err != nil {
		httpkit.Internal(w, err)
		return
	}
	ship, err := fleet.EnsureShip(r.Context(), tx, u.ID)
	if err != nil {
		httpkit.Internal(w, err)
		return
	}
	if err = wallet.Reason(r.Context(), tx, "reservation_"+in.Action); err != nil {
		httpkit.Internal(w, err)
		return
	}
	reservationID := in.ID
	var cost int64
	var star, q int
	var res string
	station := in.Station
	if station == "" {
		station = "station"
	}
	problem := ""
	if in.Action == "reserve" {
		star, q, res = in.Star, in.Quantity, in.Resource
		var outstanding int
		if err = tx.QueryRow(r.Context(), `SELECT count(*) FROM reservations WHERE user_id=$1 AND status='reserved'`, u.ID).Scan(&outstanding); err != nil {
			httpkit.Internal(w, err)
			return
		}
		if outstanding >= 100 {
			httpkit.Fail(w, 409, "Достигнут лимит активных броней")
			return
		}
		if star < 1 || star > len(g.Stars) || q < 1 || q > 240 {
			httpkit.Fail(w, 400, "Некорректная сделка")
			return
		}
		if _, ok := resourcecatalog.Lookup(res); !ok {
			httpkit.Fail(w, 400, "Некорректная сделка")
			return
		}
		var exists bool
		if err = tx.QueryRow(r.Context(), `SELECT EXISTS(SELECT 1 FROM stations WHERE star=$1 AND object_id=$2 AND active)`, star, station).Scan(&exists); err != nil {
			httpkit.Internal(w, err)
			return
		}
		if !exists {
			httpkit.Fail(w, 400, "Некорректная сделка")
			return
		}
		if !worldstore.StationAllows(r.Context(), tx, star, station, res) {
			httpkit.Fail(w, 409, "Станция не торгует этим ресурсом")
			return
		}
		stock, _, err := tradecatalog.MarketAt(r.Context(), tx, g.Stars[star-1], station)
		if err != nil {
			httpkit.Internal(w, err)
			return
		}
		var raw []byte

		for _, o := range tradecatalog.Offers(g.Stars[star-1], stock) {
			if o.Resource == res {
				cost = int64(q * tradecatalog.FeeOffer(o, worldstore.PortFee(r.Context(), tx, u.ID, star, station)).Buy)
			}
		}
		if in.ExpectedPrice != nil && cost != int64(q**in.ExpectedPrice) {
			httpkit.Fail(w, 409, "Цена изменилась. Проверьте предложение ещё раз")
			return
		}
		if stock[res] < q {
			problem = "Нет товара на рынке"
		} else if credits < cost {
			problem = "Недостаточно кредитов"
		} else {
			stock[res] -= q
			raw, _ = json.Marshal(stock)
			if _, err = tx.Exec(r.Context(), `UPDATE local_markets SET stock=$2 WHERE star=$1 AND station=$3`, star, raw, station); err == nil {
				err = tx.QueryRow(r.Context(), `INSERT INTO reservations(user_id,star,resource,quantity,cost,station,expires_at) VALUES($1,$2,$3,$4,$5,$6,now()+make_interval(hours=>(SELECT reservation_hours FROM game_settings WHERE id=1))) RETURNING id`, u.ID, star, res, q, cost, station).Scan(&reservationID)
			}
			credits -= cost
		}
	} else if in.Action == "collect" || in.Action == "cancel" {
		var status string
		err = tx.QueryRow(r.Context(), `SELECT star,station,resource,quantity,cost,status FROM reservations WHERE id=$1 AND user_id=$2 FOR UPDATE`, in.ID, u.ID).Scan(&star, &station, &res, &q, &cost, &status)
		if errors.Is(err, pgx.ErrNoRows) {
			httpkit.Fail(w, 404, "Бронь не найдена")
			return
		}
		if err != nil {
			httpkit.Internal(w, err)
			return
		}
		if status != "reserved" {
			problem = "Бронь уже обработана"
		} else if in.Action == "collect" {
			var pending int
			if err = tx.QueryRow(r.Context(), `SELECT COALESCE(sum(quantity),0) FROM mining_jobs WHERE ship_id=$1`, ship.ID).Scan(&pending); err != nil {
				httpkit.Internal(w, err)
				return
			}
			if !fleet.Docked(ship) || ship.Star != star || ship.ObjectID != station {
				problem = "Прибудьте на станцию получения"
			} else if ship.CargoUsed()+pending+q > ship.Capacity() {
				problem = "Трюм заполнен"
			} else {
				ship.Cargo[res] += q
				raw, _ := json.Marshal(ship.Cargo)
				if _, err = tx.Exec(r.Context(), `UPDATE ships SET cargo=$2 WHERE user_id=$1 AND active`, u.ID, raw); err == nil {
					_, err = tx.Exec(r.Context(), `UPDATE local_markets SET credits=credits+$2 WHERE star=$1 AND station=$3`, star, cost, station)
				}
				if err == nil {
					_, err = tx.Exec(r.Context(), `UPDATE reservations SET status='collected' WHERE id=$1`, in.ID)
				}
			}
		} else {
			_, err = tx.Exec(r.Context(), `UPDATE local_markets SET stock=jsonb_set(stock,ARRAY[$2::text],to_jsonb(COALESCE((stock->>$2)::int,0)+$3)) WHERE star=$1 AND station=$4`, star, res, q, station)
			if err == nil {
				_, err = tx.Exec(r.Context(), `UPDATE reservations SET status='cancelled' WHERE id=$1`, in.ID)
			}
			credits += cost
		}
	} else {
		problem = "Неизвестное действие"
	}
	if err != nil {
		httpkit.Internal(w, err)
		return
	}
	if problem != "" {
		httpkit.Fail(w, 409, problem)
		return
	}
	if _, err = tx.Exec(r.Context(), `UPDATE users SET credits=$2 WHERE id=$1`, u.ID, credits); err != nil {
		httpkit.Internal(w, err)
		return
	}
	if err = events.RecordEvent(r.Context(), tx, u.ID, "reservation_"+in.Action, map[string]any{"reservation": reservationID, "star": star, "resource": res, "quantity": q, "cost": cost}); err != nil {
		httpkit.Internal(w, err)
		return
	}
	if err = tx.Commit(r.Context()); err != nil {
		httpkit.Internal(w, err)
		return
	}
	httpkit.Reply(w, 200, map[string]bool{"ok": true})
}
