package terminal

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"strconv"
	"time"

	"github.com/jackc/pgx/v5"

	tradecatalog "github.com/yomezza/cosmosimulator/internal/commerce/catalog"
	"github.com/yomezza/cosmosimulator/internal/events"
	"github.com/yomezza/cosmosimulator/internal/fleet"
	"github.com/yomezza/cosmosimulator/internal/mining"
	"github.com/yomezza/cosmosimulator/internal/platform/httpkit"
	"github.com/yomezza/cosmosimulator/internal/research"
	resourcecatalog "github.com/yomezza/cosmosimulator/internal/resources"
	"github.com/yomezza/cosmosimulator/internal/universe"
	wallets "github.com/yomezza/cosmosimulator/internal/wallet"
	"github.com/yomezza/cosmosimulator/internal/worldstore"
)

func (a *Service) Expedition(w http.ResponseWriter, r *http.Request) {
	u, ok := a.Auth.User(w, r)
	if !ok {
		return
	}
	ctx := r.Context()
	var requestedShip int64
	if r.URL.Query().Has("ship") {
		id, err := strconv.ParseInt(r.URL.Query().Get("ship"), 10, 64)
		if err != nil || id < 1 {
			httpkit.Fail(w, 400, "Укажите корабль")
			return
		}
		requestedShip = id
	}
	g, err := a.World.Load(ctx)
	if err != nil {
		httpkit.Internal(w, err)
		return
	}
	tx, err := a.DB.Begin(ctx)
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
		g, err = a.World.Load(ctx)
		if err != nil {
			httpkit.Internal(w, err)
			return
		}
	}
	// All mutations for one player serialize, including parallel browser requests.
	if err = tx.QueryRow(ctx, `SELECT credits FROM users WHERE id=$1 FOR UPDATE`, u.ID).Scan(&u.Credits); err != nil {
		httpkit.Internal(w, err)
		return
	}
	var ship fleet.Ship
	if requestedShip == 0 {
		ship, err = fleet.EnsureShip(ctx, tx, u.ID)
	} else {
		err = fleet.SettleShips(ctx, tx, u.ID)
		if err == nil {
			var owned int64
			err = tx.QueryRow(ctx, `SELECT id FROM ships WHERE id=$1 AND user_id=$2 FOR UPDATE`, requestedShip, u.ID).Scan(&owned)
			if errors.Is(err, pgx.ErrNoRows) {
				httpkit.Fail(w, 404, "Корабль не найден")
				return
			}
			if err == nil {
				ship, err = fleet.ReadShip(ctx, tx, owned)
			}
		}
	}
	if err != nil {
		httpkit.Internal(w, err)
		return
	}
	if ship.Star < 1 || ship.Star > len(g.Stars) || g.Stars[ship.Star-1].Deleted {
		httpkit.Fail(w, 409, "Система корабля недоступна")
		return
	}
	star := g.Stars[ship.Star-1]
	if _, err = tx.Exec(ctx, `SELECT pg_advisory_xact_lock(784219,$1)`, ship.Star); err != nil {
		httpkit.Internal(w, err)
		return
	}
	marketID := ship.ObjectID
	if !fleet.Docked(ship) {
		marketID = ""
	}
	stock, wallet, err := tradecatalog.MarketAt(ctx, tx, star, marketID)
	if err != nil {
		httpkit.Internal(w, err)
		return
	}

	var job *mining.MiningJob
	var active mining.MiningJob
	err = tx.QueryRow(ctx, `SELECT resource,quantity,finish_tick,started_at,planet FROM mining_jobs WHERE ship_id=$1`, ship.ID).Scan(&active.Resource, &active.Quantity, &active.FinishTick, &active.StartedAt, &active.Planet)
	if err == nil {
		job = &active
	} else if !errors.Is(err, pgx.ErrNoRows) {
		httpkit.Internal(w, err)
		return
	}
	err = nil
	var worldTick int64
	if err = tx.QueryRow(ctx, `SELECT tick FROM world WHERE id=1`).Scan(&worldTick); err != nil {
		httpkit.Internal(w, err)
		return
	}
	reservedCargo := 0
	if job != nil {
		reservedCargo = job.Quantity
	}
	var surveyed bool
	if err = tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM surveys WHERE user_id=$1 AND star=$2)`, u.ID, ship.Star).Scan(&surveyed); err != nil {
		httpkit.Internal(w, err)
		return
	}
	if r.Method == "POST" {
		var in struct {
			Ship           int64  `json:"ship"`
			Action         string `json:"action"`
			To             int    `json:"to"`
			Resource       string `json:"resource"`
			Quantity       int    `json:"quantity"`
			Planet         int    `json:"planet"`
			Target         string `json:"target"`
			Mode           string `json:"mode"`
			ExpectedPrice  *int   `json:"expected_price"`
			ExpectedStar   int    `json:"expected_star"`
			ExpectedObject string `json:"expected_object"`
		}
		if !httpkit.Decode(w, r, &in) {
			return
		}
		if requestedShip != 0 {
			if in.Ship != requestedShip || in.ExpectedStar != ship.Star || in.ExpectedObject != ship.ObjectID {
				httpkit.Fail(w, 409, "Положение корабля изменилось. Проверьте данные")
				return
			}
			if (in.Action == "buy" || in.Action == "sell") && in.ExpectedPrice == nil {
				httpkit.Fail(w, 400, "Проверьте предложение ещё раз")
				return
			}
			if err := fleet.Available(ctx, tx, ship.ID); errors.Is(err, fleet.ErrReserved) {
				httpkit.Fail(w, 409, err.Error())
				return
			} else if err != nil {
				httpkit.Internal(w, err)
				return
			}
		}
		if busy, e := fleet.AutomationBusy(ctx, tx, ship.ID); e != nil {
			httpkit.Internal(w, e)
			return
		} else if busy {
			httpkit.Fail(w, 409, "Приостановите задачу планировщика")
			return
		}
		if in.Ship > 0 && in.Ship != ship.ID {
			httpkit.Fail(w, 409, "Активный корабль изменился. Повторите действие")
			return
		}
		if err = wallets.Reason(ctx, tx, in.Action); err != nil {
			httpkit.Internal(w, err)
			return
		}
		problem := ""
		if ship.Destination != nil || ship.LocalTarget != nil {
			problem = "Корабль в пути"
		} else {
			switch in.Action {
			case "move":
				if fleet.ResearchBusy(ctx, tx, ship.ID) {
					problem = "Сначала приостановите исследование"
					break
				}
				if job != nil {
					problem = "Дождитесь завершения добычи"
					break
				}
				var target *universe.Site
				sitesForMove, e := worldstore.AddCustomSites(ctx, tx, star, universe.SystemSites(star))
				if e != nil {
					err = e
					break
				}
				for _, site := range sitesForMove {
					if site.ID == in.Target {
						copy := site
						target = &copy
					}
				}
				visible, e := research.RevealedSites(ctx, tx, u.ID, star.ID, sitesForMove, surveyed)
				if e != nil {
					err = e
					break
				}
				knownTarget := false
				// Public port coordinates are broadcast by the market. Docking
				// does not disclose the system's unscanned planets or deposits.
				if target != nil && target.Kind == "Орбитальная станция" {
					knownTarget = true
				}
				for _, site := range visible {
					if site.ID == in.Target {
						knownTarget = true
					}
				}
				if !knownTarget {
					target = nil
				}
				if target == nil {
					problem = "Объект не найден"
					break
				}
				if in.Mode != "approach" && in.Mode != "orbit" && in.Mode != "docked" {
					problem = "Некорректный манёвр"
					break
				}
				if in.Mode == "docked" && target.Kind != "Орбитальная станция" || in.Mode == "orbit" && target.Details == nil {
					problem = "Манёвр недоступен для объекта"
					break
				}
				if ship.Fuel-ship.ReservedFuel < 1 {
					problem = "Недостаточно топлива"
					break
				}
				if fleet.NearObject(ship, target.ID) && ship.PositionMode == in.Mode {
					problem = "Корабль уже на месте"
					break
				}
				duration := 5 * time.Second
				now := time.Now()
				arrival := now.Add(duration)
				ship.LocalTarget = &in.Target
				ship.LocalMode = &in.Mode
				ship.LocalStarted = &now
				ship.LocalArrival = &arrival
				ship.Fuel--
			case "cancel_mine":
				if _, err = tx.Exec(ctx, `SELECT resource FROM resource_quotas WHERE star=$1 AND object=$2 AND resource='*' FOR UPDATE`, ship.Star, func() int {
					if job != nil {
						return job.Planet
					}
					return 0
				}()); err != nil {
					break
				}
				if job == nil {
					problem = "Нет активной добычи"
					break
				}
				if _, err = tx.Exec(ctx, `UPDATE resource_quotas q SET used=GREATEST(used-j.quantity,0) FROM mining_jobs j WHERE j.ship_id=$1 AND q.star=j.star AND q.object=j.planet AND q.resource IN ('*',j.resource) AND q.window_start=j.quota_start`, ship.ID); err == nil {
					_, err = tx.Exec(ctx, `DELETE FROM mining_jobs WHERE ship_id=$1`, ship.ID)
				}
				job = nil
			case "scan":
				err = research.StartResearch(ctx, tx, u.ID, &ship, star, g)
				if err != nil {
					problem = err.Error()
					err = nil
				} else {
					err = tx.QueryRow(ctx, `SELECT credits FROM users WHERE id=$1`, u.ID).Scan(&u.Credits)
				}
			case "travel":
				if fleet.ResearchBusy(ctx, tx, ship.ID) {
					problem = "Сначала приостановите исследование"
					break
				}
				if job != nil {
					problem = "Дождитесь завершения добычи"
					break
				}
				path := g.Path(ship.Star, in.To)
				if len(path) < 2 {
					problem = "Нет допустимого маршрута"
				} else if ship.Fuel-ship.ReservedFuel < (len(path)-1)*ship.Hull().JumpFuel {
					problem = "Недостаточно топлива"
				} else {
					ship.Fuel -= (len(path) - 1) * ship.Hull().JumpFuel
					arrival := time.Now().Add(time.Duration((len(path)-1)*ship.Hull().JumpSeconds) * time.Second)
					ship.Arrival = &arrival
					now := time.Now()
					ship.Departure = &now
					ship.Path = path
					ship.Destination = &in.To
				}
			case "refuel":
				if ship.Fuel >= ship.Hull().Fuel {
					problem = "Бак полон"
				} else if ship.Cargo["agni"] < 1 {
					problem = "Нужен водород в трюме"
				} else {
					ship.Cargo["agni"]--
					ship.Fuel += 5
					if ship.Fuel > ship.Hull().Fuel {
						ship.Fuel = ship.Hull().Fuel
					}
				}
			case "buy", "sell":
				if !worldstore.StationAllows(ctx, tx, ship.Star, ship.ObjectID, in.Resource) {
					problem = "Станция не торгует этой категорией"
					break
				}
				if !fleet.Docked(ship) {
					problem = "Для торговли требуется стыковка с портом"
					break
				}
				if _, exists := resourcecatalog.Lookup(in.Resource); !exists || in.Quantity < 1 || in.Quantity > ship.Capacity() {
					problem = "Некорректная сделка"
					break
				}
				var offer tradecatalog.Offer
				for _, o := range tradecatalog.Offers(star, stock) {
					if o.Resource == in.Resource {
						offer = tradecatalog.FeeOffer(o, worldstore.PortFee(ctx, tx, u.ID, ship.Star, ship.ObjectID))
					}
				}
				if in.ExpectedPrice != nil {
					price := offer.Buy
					if in.Action == "sell" {
						price = offer.Sell
					}
					if *in.ExpectedPrice != price {
						problem = "Цена изменилась. Проверьте предложение ещё раз"
						break
					}
				}
				q := in.Quantity
				if in.Action == "buy" {
					cost := int64(q * offer.Buy)
					if stock[in.Resource] < q {
						problem = "Нет товара на рынке"
					} else if ship.CargoUsed()+reservedCargo+q > ship.Capacity() {
						problem = "Трюм заполнен"
					} else if wallet > wallets.MaxCredits-cost {
						problem = "Сумма превышает технический диапазон"
					} else if u.Credits < cost {
						problem = "Недостаточно кредитов"
					} else {
						stock[in.Resource] -= q
						ship.Cargo[in.Resource] += q
						u.Credits -= cost
						wallet += cost
					}
				}
				if in.Action == "sell" {
					cost := int64(q * offer.Sell)
					if ship.Cargo[in.Resource] < q {
						problem = "Нет груза для продажи"
					} else if u.Credits > wallets.MaxCredits-cost {
						problem = "Сумма превышает технический диапазон"
					} else if wallet < cost {
						problem = "У рынка недостаточно кредитов"
					} else {
						ship.Cargo[in.Resource] -= q
						stock[in.Resource] += q
						u.Credits += cost
						wallet -= cost
					}
				}
			case "mine":
				job, err = mining.BeginExtraction(ctx, tx, u.ID, ship, star, in.Planet, in.Resource, 0)
				if err != nil {
					problem = err.Error()
					err = nil
				}

			default:
				problem = "Неизвестное действие"
			}
		}
		if err != nil {
			httpkit.Internal(w, err)
			return
		}
		if problem != "" {
			httpkit.Fail(w, 409, problem)
			return
		}
		stockJSON, _ := json.Marshal(stock)
		if _, err = tx.Exec(ctx, `UPDATE users SET credits=$2 WHERE id=$1`, u.ID, u.Credits); err != nil {
			httpkit.Internal(w, err)
			return
		}
		if err = fleet.SaveShip(ctx, tx, u.ID, ship); err != nil {
			httpkit.Internal(w, err)
			return
		}
		if err = events.RecordEvent(ctx, tx, u.ID, in.Action, map[string]any{"star": ship.Star, "resource": in.Resource, "quantity": in.Quantity, "object": in.Target, "destination": in.To}); err != nil {
			httpkit.Internal(w, err)
			return
		}

		if _, err = tx.Exec(ctx, `UPDATE local_markets SET credits=$2,stock=$3 WHERE star=$1 AND station=$4`, ship.Star, wallet, stockJSON, marketID); err != nil {
			httpkit.Internal(w, err)
			return
		}
	}
	sites, siteErr := worldstore.AddCustomSites(ctx, tx, star, universe.SystemSites(star))
	if siteErr != nil {
		httpkit.Internal(w, siteErr)
		return
	}
	if surveyed {
		sites, err = mining.QuotaView(ctx, tx, star, sites)
		if err != nil {
			httpkit.Internal(w, err)
			return
		}
	}
	sites, err = research.RevealedSites(ctx, tx, u.ID, star.ID, sites, surveyed)
	if err != nil {
		httpkit.Internal(w, err)
		return
	}
	ship.Mining = ship.MiningStats(sites)
	if !surveyed {
		star.Planets = nil
	}
	star = universe.PublicStar(star)
	if err = tx.Commit(ctx); err != nil {
		httpkit.Internal(w, err)
		return
	}
	marketOffers := []tradecatalog.Offer{}
	fee := worldstore.PortFee(ctx, a.DB, u.ID, ship.Star, marketID)
	var goods []string
	if fleet.Docked(ship) {
		if e := a.DB.QueryRow(ctx, `SELECT goods FROM stations WHERE star=$1 AND object_id=$2`, ship.Star, marketID).Scan(&goods); e != nil {
			httpkit.Internal(w, e)
			return
		}
	}
	for _, o := range tradecatalog.StationOffers(star, stock, goods) {
		marketOffers = append(marketOffers, tradecatalog.FeeOffer(o, fee))
	}
	stationPresent := false
	for _, site := range sites {
		if site.Kind == "Орбитальная станция" {
			stationPresent = true
			break
		}
	}

	httpkit.Reply(w, 200, map[string]any{"server_time": time.Now(), "job": job, "world_tick": worldTick, "station": stationPresent, "docked": fleet.Docked(ship), "sites": sites, "ship": ship, "system": star, "region": g.RegionName(star.Region), "surveyed": surveyed, "credits": u.Credits, "resources": resourcecatalog.Catalog(), "offers": marketOffers, "market_credits": wallet, "capacity": ship.Capacity(), "research": research.ResearchView(ctx, a.DB, u.ID)})
}
