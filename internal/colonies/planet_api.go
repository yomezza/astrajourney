package colonies

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"strconv"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"

	"github.com/yomezza/cosmosimulator/internal/corporations"
	"github.com/yomezza/cosmosimulator/internal/fleet"
	"github.com/yomezza/cosmosimulator/internal/geography"
	"github.com/yomezza/cosmosimulator/internal/inventory"
	"github.com/yomezza/cosmosimulator/internal/logistics"
	"github.com/yomezza/cosmosimulator/internal/platform/httpkit"
	"github.com/yomezza/cosmosimulator/internal/platform/idempotency"
	"github.com/yomezza/cosmosimulator/internal/platform/postgres"
	"github.com/yomezza/cosmosimulator/internal/universe"
	"github.com/yomezza/cosmosimulator/internal/wallet"
)

type planetAction struct {
	Name             string `json:"name"`
	ExpectedRevision int64  `json:"expected_revision"`
	Correction       bool   `json:"correction"`
	Reason           string `json:"reason"`
	Action           string `json:"action"`
	Request          string `json:"request"`
	ID               int64  `json:"id"`
	Surface          int64  `json:"surface"`
	Sector           int    `json:"sector"`
	Corporate        bool   `json:"corporate"`
	Resource         string `json:"resource"`
	Amount           int64  `json:"amount"`
	Contribution     int64  `json:"contribution"`
}

func (a *Service) ColonyAction(w http.ResponseWriter, r *http.Request) {
	u, ok := a.Auth.User(w, r)
	if !ok {
		return
	}
	var in planetAction
	if !httpkit.Decode(w, r, &in) {
		return
	}
	if in.Action == "rename_colony" {
		a.renameColony(w, r, u, in)
		return
	}
	if len(in.Request) < 8 || len(in.Request) > 100 {
		httpkit.Fail(w, 400, "Нужен идентификатор операции")
		return
	}
	if in.Action == "land" || in.Action == "leave" {
		httpkit.Fail(w, 409, "landing_site_required")
		return
	}
	ctx := r.Context()
	if _, err := a.World.Load(ctx); err != nil {
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
	var role string
	var level int
	if err = tx.QueryRow(ctx, `SELECT role,level FROM users WHERE id=$1 FOR UPDATE`, u.ID).Scan(&role, &level); err != nil {
		httpkit.Internal(w, err)
		return
	}
	if in.Action == "admin_fill" && role != "admin" {
		httpkit.Fail(w, 403, "Требуется роль администратора")
		return
	}
	u.Role = role
	digest := idempotency.Digest(in)
	var previous string
	var result []byte
	err = tx.QueryRow(ctx, `SELECT digest,result FROM planet_requests WHERE user_id=$1 AND request=$2`, u.ID, in.Request).Scan(&previous, &result)
	if err == nil {
		if previous != digest {
			httpkit.Fail(w, 409, "Идентификатор операции уже использован")
			return
		}
		var receipt struct {
			Reset bool `json:"surface_reset"`
		}
		_ = json.Unmarshal(result, &receipt)
		if receipt.Reset {
			httpkit.Fail(w, 409, "Генерация поверхности изменилась")
			return
		}
		httpkit.Reply(w, 200, json.RawMessage(result))
		return
	}
	if !errors.Is(err, pgx.ErrNoRows) {
		httpkit.Internal(w, err)
		return
	}
	var response any = map[string]any{"ok": true}
	var p ColonyProject
	if in.Action == "claim_refund" {
		err = claimPlanetRefund(ctx, tx, in.ID, u.ID)
	} else if in.Action == "create" {
		c, e := ReadConfig(ctx, tx)
		err = e
		if err == nil && level < c.MinLevel {
			err = errors.New("Для колонизации недостаточно стажа")
		}
		var planet universe.Planet
		if err == nil {
			planet, _, err = a.checkSurface(ctx, tx, u.ID, in.Surface)
		}
		if err == nil && planet.Kind != "Садовый мир" {
			err = errors.New("Планета непригодна для колонизации")
		}
		var data []byte
		var sector geography.PlanetSector
		if err == nil {
			err = tx.QueryRow(ctx, `SELECT data FROM planet_sectors WHERE surface=$1 AND number=$2`, in.Surface, in.Sector).Scan(&data)
		}
		if err == nil {
			err = json.Unmarshal(data, &sector)
		}
		if err == nil && sector.Land < float64(c.MinLand) {
			err = errors.New("Недостаточная доля суши")
		}
		var owner, corp *int64
		if err == nil && in.Corporate {
			cid, allowed, e := corporations.CompanyAccess(ctx, tx, u.ID, "colonize")
			err = e
			if err == nil && !allowed {
				err = errors.New("Недостаточно корпоративных полномочий")
			}
			corp = &cid
		} else if err == nil {
			owner = &u.ID
			var count int
			err = tx.QueryRow(ctx, `SELECT count(*) FROM planet_projects WHERE owner_user=$1 AND status IN('funding','building','complete')`, u.ID).Scan(&count)
			if err == nil && count >= c.PersonalLimit {
				err = errors.New("Достигнут лимит личных секторов")
			}
		}
		if err == nil {
			raw, _ := json.Marshal(c.Cost)
			var id int64
			err = tx.QueryRow(ctx, `INSERT INTO planet_projects(surface,sector,initiator,owner_user,owner_company,required,build_ticks,refund_percent,expires_at) VALUES($1,$2,$3,$4,$5,$6,$7,$8,now()+make_interval(days=>$9)) ON CONFLICT(surface,sector) WHERE status IN('funding','building','complete') DO NOTHING RETURNING id`, in.Surface, in.Sector, u.ID, owner, corp, raw, c.BuildTicks, c.RefundPercent, c.FundingDays).Scan(&id)
			if errors.Is(err, pgx.ErrNoRows) {
				err = errors.New("Сектор уже занят")
			}
			if err == nil {
				err = planetEvent(ctx, tx, id, u.ID, "create", in)
				response = map[string]any{"ok": true, "project": id}
			}
		}
	} else {
		p, err = readColonyProject(ctx, tx, in.ID)
		if err == nil {
			member, manage, e := projectAccess(ctx, tx, u.ID, p)
			err = e
			expired := false
			if err == nil && p.Status == "funding" {
				err = tx.QueryRow(ctx, `SELECT expires_at<=clock_timestamp() FROM planet_projects WHERE id=$1`, p.ID).Scan(&expired)
			}
			if err == nil && expired {
				err = cancelColony(ctx, tx, &p, 0, "expired")
				if err == nil {
					err = tx.Commit(ctx)
				}
				if err != nil {
					httpkit.Internal(w, err)
				} else {
					httpkit.Fail(w, 409, "Срок финансирования истёк")
				}
				return
			}
			if err == nil {
				switch in.Action {
				case "contribute":
					if !member {
						err = errors.New("Нет доступа к финансированию проекта")
						break
					}
					if p.Status != "funding" {
						err = errors.New("Финансирование проекта закрыто")
						break
					}
					required, valid := p.Required[in.Resource]
					if !valid || in.Amount < 1 || in.Amount > required-p.Received[in.Resource] {
						err = errors.New("Некорректный вклад")
						break
					}
					var sourceStar int
					var sourceStation string
					if in.Resource == "credits" {
						err = wallet.Reason(ctx, tx, "Взнос в колонизацию")
						if err == nil {
							err = wallet.Debit(ctx, tx, u.ID, in.Amount)
						}
					} else {
						// Real supplies are consumed from a personal station warehouse, never remotely.
						var s fleet.Ship
						s, err = fleet.EnsureShip(ctx, tx, u.ID)
						if err != nil {
							break
						}
						var star int
						err = tx.QueryRow(ctx, `SELECT star FROM planet_surfaces WHERE id=$1`, p.Surface).Scan(&star)
						if err != nil {
							break
						}
						if !fleet.Docked(s) || s.Star != star {
							err = errors.New("Стыкуйтесь со станцией в системе планеты для передачи материалов")
							break
						}
						var cargo map[string]int
						sourceStar, sourceStation = s.Star, s.ObjectID
						cargo, err = logistics.StorageAccess(ctx, tx, u.ID, logistics.CargoLocation{Kind: "personal", Star: sourceStar, Station: sourceStation}, true)
						if err != nil {
							break
						}
						if int64(cargo[in.Resource]) < in.Amount {
							err = errors.New("Недостаточно ресурсов на личном складе")
							break
						}
						cargo[in.Resource] -= int(in.Amount)
						err = inventory.PutWarehouse(ctx, tx, u.ID, sourceStar, sourceStation, cargo)
					}
					if err != nil {
						break
					}
					_, err = tx.Exec(ctx, `INSERT INTO planet_contributions(project,actor,resource,amount,source_star,source_station) VALUES($1,$2,$3,$4,$5,$6)`, p.ID, u.ID, in.Resource, in.Amount, sourceStar, sourceStation)
					if err != nil {
						break
					}
					p.Received[in.Resource] += in.Amount
					err = planetEvent(ctx, tx, p.ID, u.ID, "contribute", in)
					if err == nil {
						err = planetReceived(ctx, tx, &p)
					}
				case "withdraw":
					if p.Status != "funding" {
						err = errors.New("Возврат доступен только до строительства")
						break
					}
					var actor, amount int64
					var resource string
					err = tx.QueryRow(ctx, `SELECT actor,amount,resource FROM planet_contributions WHERE id=$1 AND project=$2 AND NOT returned AND NOT admin_grant`, in.Contribution, p.ID).Scan(&actor, &amount, &resource)
					if err != nil {
						break
					}
					if actor != u.ID {
						err = errors.New("Можно вернуть только собственный вклад")
						break
					}
					err = refundContribution(ctx, tx, p.ID, in.Contribution, u.ID, 100)
					if err == nil {
						p.Received[resource] -= amount
						err = planetReceived(ctx, tx, &p)
					}
				case "cancel":
					if !manage {
						err = errors.New("Недостаточно корпоративных полномочий")
						break
					}
					err = cancelColony(ctx, tx, &p, u.ID, "cancelled")
				case "admin_fill":
					if role != "admin" {
						err = errors.New("Требуется роль администратора")
						break
					}
					if p.Status != "funding" {
						err = errors.New("Финансирование проекта закрыто")
						break
					}
					added := map[string]int64{}
					for id, required := range p.Required {
						amount := required - p.Received[id]
						if amount <= 0 {
							continue
						}
						added[id] = amount
						_, err = tx.Exec(ctx, `INSERT INTO planet_contributions(project,actor,admin_grant,resource,amount) VALUES($1,$2,true,$3,$4)`, p.ID, u.ID, id, amount)
						if err != nil {
							break
						}
						p.Received[id] = required
					}
					if err != nil {
						break
					}
					err = planetEvent(ctx, tx, p.ID, u.ID, "admin_fill", added)
					if err == nil {
						raw, _ := json.Marshal(map[string]any{"project": p.ID, "added": added})
						_, err = tx.Exec(ctx, `INSERT INTO admin_log(actor,action,data) VALUES($1,'colony_fill',$2)`, u.ID, raw)
					}
					if err == nil {
						err = planetReceived(ctx, tx, &p)
					}
				case "land", "leave":
					err = errors.New("Используйте посадочную площадку и новый протокол высадки")
				default:
					err = errors.New("Неизвестное действие")
				}
			}
		}
	}
	if err != nil {
		var databaseError *pgconn.PgError
		if errors.As(err, &databaseError) {
			httpkit.Internal(w, err)
			return
		}
		if errors.Is(err, pgx.ErrNoRows) {
			httpkit.Fail(w, 404, "Проект или вклад не найден")
		} else {
			httpkit.Fail(w, 409, err.Error())
		}
		return
	}
	if in.Action == "withdraw" || in.Action == "cancel" {
		var pending int
		if err = tx.QueryRow(ctx, `SELECT count(*) FROM planet_refunds WHERE user_id=$1 AND NOT claimed`, u.ID).Scan(&pending); err != nil {
			httpkit.Internal(w, err)
			return
		}
		if resultMap, ok := response.(map[string]any); ok {
			resultMap["pending_refunds"] = pending
		}
	}
	result, _ = json.Marshal(response)
	if _, err = tx.Exec(ctx, `INSERT INTO planet_requests(user_id,request,digest,result) VALUES($1,$2,$3,$4)`, u.ID, in.Request, digest, result); err == nil {
		err = tx.Commit(ctx)
	}
	if err != nil {
		httpkit.Internal(w, err)
		return
	}
	httpkit.Reply(w, 200, response)
}

func (a *Service) ColonyList(w http.ResponseWriter, r *http.Request) {
	u, ok := a.Auth.User(w, r)
	if !ok {
		return
	}
	rows, err := postgres.QueryMaps(r.Context(), a.DB, `SELECT p.id,p.surface,p.sector,p.status,p.progress,p.build_ticks,p.expires_at,s.star,s.planet,s.name,pc.id AS colony_id,cn.current_name AS colony_name,COALESCE(cn.revision,0) AS name_revision,COALESCE(v.username,c.name) AS owner FROM planet_projects p JOIN planet_surfaces s ON s.id=p.surface LEFT JOIN planet_colonies pc ON pc.project=p.id LEFT JOIN aj_colony_names cn ON cn.colony_id=pc.id LEFT JOIN users v ON v.id=p.owner_user LEFT JOIN companies c ON c.id=p.owner_company WHERE $2 OR p.owner_user=$1 OR p.owner_company=(SELECT company_id FROM users WHERE id=$1) OR EXISTS(SELECT 1 FROM planet_contributions x WHERE x.project=p.id AND x.actor=$1 AND NOT x.admin_grant) ORDER BY p.id DESC LIMIT 100`, u.ID, u.Role == "admin")
	if err != nil {
		httpkit.Internal(w, err)
		return
	}
	refunds, err := postgres.QueryMaps(r.Context(), a.DB, `SELECT r.*,c.project FROM planet_refunds r JOIN planet_contributions c ON c.id=r.contribution WHERE r.user_id=$1 AND NOT r.claimed ORDER BY r.id`, u.ID)
	if err != nil {
		httpkit.Internal(w, err)
		return
	}
	httpkit.Reply(w, 200, map[string]any{"projects": rows, "refunds": refunds})
}

func (a *Service) ColonyView(w http.ResponseWriter, r *http.Request) {
	u, ok := a.Auth.User(w, r)
	if !ok {
		return
	}
	ctx := r.Context()
	id, _ := strconv.ParseInt(r.URL.Query().Get("id"), 10, 64)
	tx, err := a.DB.Begin(ctx)
	if err != nil {
		httpkit.Internal(w, err)
		return
	}
	defer tx.Rollback(context.Background())
	p, err := readColonyProject(ctx, tx, id)
	if err != nil {
		httpkit.Fail(w, 404, "Проект не найден")
		return
	}
	member, manage, err := projectAccess(ctx, tx, u.ID, p)
	if err != nil {
		httpkit.Internal(w, err)
		return
	}
	var own bool
	if err = tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM planet_contributions WHERE project=$1 AND actor=$2 AND NOT admin_grant)`, id, u.ID).Scan(&own); err != nil {
		httpkit.Internal(w, err)
		return
	}
	full := member || u.Role == "admin"
	if !full && !own {
		httpkit.Fail(w, 403, "Нет доступа к проекту")
		return
	}
	contributionsBefore, _ := strconv.ParseInt(r.URL.Query().Get("contributions_before"), 10, 64)
	contributions, err := postgres.QueryMaps(ctx, tx, `SELECT c.*,u.username FROM planet_contributions c JOIN users u ON u.id=c.actor WHERE c.project=$1 AND ($3 OR c.actor=$2) AND ($4::bigint=0 OR c.id<$4) ORDER BY c.id DESC LIMIT 50`, id, u.ID, full, contributionsBefore)
	if err != nil {
		httpkit.Internal(w, err)
		return
	}
	before, _ := strconv.ParseInt(r.URL.Query().Get("before"), 10, 64)
	events, err := postgres.QueryMaps(ctx, tx, `SELECT e.*,u.username FROM planet_events e LEFT JOIN users u ON u.id=e.actor WHERE project=$1 AND ($2 OR actor=$3) AND ($4::bigint=0 OR e.id<$4) ORDER BY e.id DESC LIMIT 30`, id, full, u.ID, before)
	if err != nil {
		httpkit.Internal(w, err)
		return
	}
	var name, owner, initiator string
	var star, planet int
	if err = tx.QueryRow(ctx, `SELECT s.name,s.star,s.planet,COALESCE(u.username,c.name),i.username FROM planet_surfaces s LEFT JOIN users u ON u.id=$2 LEFT JOIN companies c ON c.id=$3 JOIN users i ON i.id=$4 WHERE s.id=$1`, p.Surface, p.User, p.Company, p.Initiator).Scan(&name, &star, &planet, &owner, &initiator); err != nil {
		httpkit.Internal(w, err)
		return
	}
	var landReason string
	s, e := fleet.EnsureShip(ctx, tx, u.ID)
	if e != nil {
		httpkit.Internal(w, e)
		return
	}
	visitAllowed, accessErr := projectVisitAccess(ctx, tx, u.ID, p)
	if accessErr != nil {
		httpkit.Internal(w, accessErr)
		return
	}
	if !visitAllowed || p.Status != "complete" {
		landReason = "Нет доступа к завершённой колонии"
	} else if s.Star != star || !fleet.NearObject(s, "planet-"+strconv.Itoa(planet)) {
		landReason = "Для высадки необходимо выполнить манёвр к планете"
	}
	if !full {
		p.Received = nil
	}
	colonyNames, err := postgres.QueryMaps(ctx, tx, `SELECT c.id,COALESCE(n.current_name,'') AS name,COALESCE(n.revision,0) AS revision FROM planet_colonies c LEFT JOIN aj_colony_names n ON n.colony_id=c.id WHERE c.project=$1`, p.ID)
	if err != nil {
		httpkit.Internal(w, err)
		return
	}
	httpkit.Reply(w, 200, map[string]any{"project": p, "colony_names": colonyNames, "name": name, "star": star, "planet": planet, "owner": owner, "initiator_name": initiator, "contributions": contributions, "events": events, "can_manage": manage, "can_contribute": member, "land_reason": landReason, "server_time": time.Now()})
}

func (a *Service) PlanetSettings(w http.ResponseWriter, r *http.Request) {
	u, ok := a.Auth.User(w, r)
	if !ok {
		return
	}
	if u.Role != "admin" {
		httpkit.Fail(w, 403, "Требуется роль администратора")
		return
	}
	ctx := r.Context()
	if r.Method == "POST" {
		var config PlanetConfig
		if !httpkit.Decode(w, r, &config) {
			return
		}
		if !config.valid() {
			httpkit.Fail(w, 400, "Некорректные настройки колонизации")
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
		var role string
		if err = tx.QueryRow(ctx, `SELECT role FROM users WHERE id=$1 FOR UPDATE`, u.ID).Scan(&role); err != nil || role != "admin" {
			httpkit.Fail(w, 403, "Требуется роль администратора")
			return
		}
		raw, _ := json.Marshal(config)
		_, err = tx.Exec(ctx, `UPDATE planet_settings SET config=$1 WHERE id=1`, raw)
		if err == nil {
			_, err = tx.Exec(ctx, `INSERT INTO admin_log(actor,action,data) VALUES($1,'colony_settings',$2)`, u.ID, raw)
		}
		if err == nil {
			err = tx.Commit(ctx)
		}
		if err != nil {
			httpkit.Internal(w, err)
			return
		}
	}
	config, err := ReadConfig(ctx, a.DB)
	if err != nil {
		httpkit.Internal(w, err)
		return
	}
	httpkit.Reply(w, 200, config)
}
