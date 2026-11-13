package colonies

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"strconv"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/yomezza/cosmosimulator/internal/corporations"
	"github.com/yomezza/cosmosimulator/internal/geography"
	"github.com/yomezza/cosmosimulator/internal/inventory"
	"github.com/yomezza/cosmosimulator/internal/platform/httpkit"
	"github.com/yomezza/cosmosimulator/internal/platform/postgres"
	"github.com/yomezza/cosmosimulator/internal/research"
	resourcecatalog "github.com/yomezza/cosmosimulator/internal/resources"
	"github.com/yomezza/cosmosimulator/internal/universe"
	"github.com/yomezza/cosmosimulator/internal/wallet"
)

type PlanetConfig struct {
	MinLevel      int              `json:"min_level"`
	PersonalLimit int              `json:"personal_limit"`
	MinLand       int              `json:"min_land"`
	Counts        []int            `json:"counts"`
	FundingDays   int              `json:"funding_days"`
	BuildTicks    int              `json:"build_ticks"`
	RefundPercent int              `json:"refund_percent"`
	Cost          map[string]int64 `json:"cost"`
}

func ReadConfig(ctx context.Context, q postgres.Querier) (PlanetConfig, error) {
	var c PlanetConfig
	var raw []byte
	err := q.QueryRow(ctx, `SELECT config FROM planet_settings WHERE id=1`).Scan(&raw)
	if err == nil {
		err = json.Unmarshal(raw, &c)
	}
	return c, err
}

func (c PlanetConfig) valid() bool {
	if c.MinLevel < 1 || c.MinLevel > 1000000 || c.PersonalLimit < 1 || c.PersonalLimit > 10000 || c.MinLand < 1 || c.MinLand > 100 || c.FundingDays < 1 || c.FundingDays > 3650 || c.BuildTicks < 1 || c.BuildTicks > 1000000 || c.RefundPercent < 0 || c.RefundPercent > 100 || len(c.Counts) != 4 || len(c.Cost) < 3 || c.Cost["colony_core"] != 1 || c.Cost["credits"] < 1 {
		return false
	}
	for _, v := range c.Counts {
		if v < 10 || v > 200 || v%10 != 0 {
			return false
		}
	}
	for id, v := range c.Cost {
		if v < 1 || v > 1000000000 {
			return false
		}
		if id != "credits" {
			if _, ok := resourcecatalog.Lookup(id); !ok {
				return false
			}
		}
	}
	return true
}

func (a *Service) PlanetAt(ctx context.Context, q postgres.Querier, uid int64, star, planet int) (universe.Planet, error) {
	g, err := a.World.LoadFrom(ctx, q)
	if err != nil {
		return universe.Planet{}, err
	}
	if star < 1 || star > len(g.Stars) || g.Stars[star-1].Deleted {
		return universe.Planet{}, errors.New("Планета недоступна")
	}
	var known bool
	if err = q.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM surveys WHERE user_id=$1 AND star=$2)`, uid, star).Scan(&known); err != nil {
		return universe.Planet{}, err
	}
	sites, err := research.RevealedSites(ctx, q, uid, star, universe.SystemSites(g.Stars[star-1]), known)
	if err != nil {
		return universe.Planet{}, err
	}
	for _, s := range sites {
		if s.Details != nil && s.Details.ID == planet {
			return *s.Details, nil
		}
	}
	return universe.Planet{}, errors.New("Планета не исследована")
}

func (a *Service) PlanetSurface(w http.ResponseWriter, r *http.Request) {
	u, ok := a.Auth.User(w, r)
	if !ok {
		return
	}
	ctx := r.Context()
	star, _ := strconv.Atoi(r.URL.Query().Get("star"))
	planet, _ := strconv.Atoi(r.URL.Query().Get("planet"))
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
	p, err := a.PlanetAt(ctx, tx, u.ID, star, planet)
	if err != nil {
		httpkit.Fail(w, 403, "Планета недоступна или не исследована")
		return
	}
	if p.Kind == "Газовый гигант" {
		httpkit.Fail(w, 409, "У газового гиганта нет твёрдой поверхности")
		return
	}
	var deleted bool
	var revision int64
	if err = tx.QueryRow(ctx, `SELECT COALESCE((SELECT deleted FROM planet_surface_controls WHERE star=$1 AND planet=$2),false),COALESCE((SELECT revision FROM planet_surface_controls WHERE star=$1 AND planet=$2),0)`, star, planet).Scan(&deleted, &revision); err != nil {
		httpkit.Internal(w, err)
		return
	}
	w.Header().Set("Cache-Control", "private, no-store")
	if deleted {
		httpkit.Reply(w, 200, map[string]any{"absent": true, "star": star, "planet": planet, "name": p.Name, "kind": p.Kind, "revision": revision})
		return
	}
	var id int64
	var raw []byte
	err = tx.QueryRow(ctx, `SELECT id,data FROM planet_surfaces WHERE star=$1 AND planet=$2`, star, planet).Scan(&id, &raw)
	if errors.Is(err, pgx.ErrNoRows) {
		c, e := ReadConfig(ctx, tx)
		if e != nil {
			httpkit.Internal(w, e)
			return
		}
		g, e := a.World.LoadFrom(ctx, tx)
		if e != nil {
			httpkit.Internal(w, e)
			return
		}
		seed := geography.SurfaceSeed(g, g.Stars[star-1], p)
		surface := geography.Generate(p, seed, c.Counts[geography.PlanetSize(p)])
		id, err = storeSurface(ctx, tx, star, p, seed, surface)
		raw, _ = json.Marshal(surface)
	}
	if err == nil {
		err = tx.Commit(ctx)
	}
	if err != nil {
		httpkit.Internal(w, err)
		return
	}
	httpkit.Reply(w, 200, map[string]any{"id": id, "star": star, "planet": planet, "name": p.Name, "kind": p.Kind, "version": 1, "revision": revision, "geography": json.RawMessage(raw)})
}

func (a *Service) checkSurface(ctx context.Context, q postgres.Querier, uid, surface int64) (universe.Planet, int, error) {
	var star, planet int
	if err := q.QueryRow(ctx, `SELECT star,planet FROM planet_surfaces WHERE id=$1`, surface).Scan(&star, &planet); err != nil {
		return universe.Planet{}, 0, err
	}
	p, err := a.PlanetAt(ctx, q, uid, star, planet)
	return p, star, err
}

func (a *Service) PlanetState(w http.ResponseWriter, r *http.Request) {
	u, ok := a.Auth.User(w, r)
	if !ok {
		return
	}
	ctx := r.Context()
	tx, err := a.DB.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.RepeatableRead, AccessMode: pgx.ReadOnly})
	if err != nil {
		httpkit.Internal(w, err)
		return
	}
	defer tx.Rollback(context.Background())
	id, _ := strconv.ParseInt(r.URL.Query().Get("surface"), 10, 64)
	p, _, err := a.checkSurface(ctx, tx, u.ID, id)
	if errors.Is(err, pgx.ErrNoRows) {
		httpkit.Fail(w, 404, "Генерация поверхности изменилась")
		return
	}
	if err != nil {
		httpkit.Fail(w, 403, err.Error())
		return
	}
	c, err := ReadConfig(ctx, tx)
	if err != nil {
		httpkit.Internal(w, err)
		return
	}
	var level, count int
	var cid *int64
	if err = tx.QueryRow(ctx, `SELECT level,company_id,(SELECT count(*) FROM planet_projects WHERE owner_user=u.id AND status IN('funding','building','complete')) FROM users u WHERE id=$1`, u.ID).Scan(&level, &cid, &count); err != nil {
		httpkit.Internal(w, err)
		return
	}
	claims, err := postgres.QueryMaps(ctx, tx, `SELECT p.id,p.sector,p.status,p.owner_user,p.owner_company,pc.id AS colony_id,cn.current_name AS colony_name,COALESCE(cn.revision,0) AS name_revision,COALESCE(u.username,c.name) AS owner FROM planet_projects p LEFT JOIN planet_colonies pc ON pc.project=p.id LEFT JOIN aj_colony_names cn ON cn.colony_id=pc.id LEFT JOIN users u ON u.id=p.owner_user LEFT JOIN companies c ON c.id=p.owner_company WHERE p.surface=$1 AND p.status IN('funding','building','complete')`, id)
	if err != nil {
		httpkit.Internal(w, err)
		return
	}
	_, corpAllowed, err := corporations.CompanyAccess(ctx, tx, u.ID, "colonize")
	if err != nil {
		httpkit.Internal(w, err)
		return
	}
	landing, err := a.landingState(ctx, tx, u.ID, id)
	if err != nil {
		httpkit.Internal(w, err)
		return
	}
	if err = tx.Commit(ctx); err != nil {
		httpkit.Internal(w, err)
		return
	}
	httpkit.Reply(w, 200, map[string]any{"landing": landing, "claims": claims, "settings": c, "level": level, "personal_count": count, "company": cid, "can_corporate": corpAllowed, "habitable": p.Kind == "Садовый мир", "server_time": time.Now()})
}

type ColonyProject struct {
	ID            int64            `json:"id"`
	Surface       int64            `json:"surface"`
	Sector        int              `json:"sector"`
	Initiator     int64            `json:"initiator"`
	User          *int64           `json:"owner_user"`
	Company       *int64           `json:"owner_company"`
	Status        string           `json:"status"`
	Required      map[string]int64 `json:"required"`
	Received      map[string]int64 `json:"received"`
	BuildTicks    int              `json:"build_ticks"`
	Progress      int              `json:"progress"`
	LastTick      int64            `json:"last_tick"`
	RefundPercent int              `json:"refund_percent"`
	Expires       time.Time        `json:"expires_at"`
}

func readColonyProject(ctx context.Context, tx pgx.Tx, id int64) (ColonyProject, error) {
	var p ColonyProject
	var required, received []byte
	err := tx.QueryRow(ctx, `SELECT id,surface,sector,initiator,owner_user,owner_company,status,required,received,build_ticks,progress,last_tick,refund_percent,expires_at FROM planet_projects WHERE id=$1 FOR UPDATE`, id).Scan(&p.ID, &p.Surface, &p.Sector, &p.Initiator, &p.User, &p.Company, &p.Status, &required, &received, &p.BuildTicks, &p.Progress, &p.LastTick, &p.RefundPercent, &p.Expires)
	if err == nil {
		err = json.Unmarshal(required, &p.Required)
	}
	if err == nil {
		err = json.Unmarshal(received, &p.Received)
	}
	return p, err
}

func projectAccess(ctx context.Context, q postgres.Querier, uid int64, p ColonyProject) (member, manage bool, err error) {
	if p.User != nil {
		return *p.User == uid, *p.User == uid, nil
	}
	cid, allowed, err := corporations.CompanyAccess(ctx, q, uid, "colonize")
	return p.Company != nil && cid == *p.Company, p.Company != nil && cid == *p.Company && allowed, err
}

func projectVisitAccess(ctx context.Context, q postgres.Querier, uid int64, p ColonyProject) (bool, error) {
	if p.User != nil {
		return *p.User == uid, nil
	}
	cid, allowed, err := corporations.CompanyAccess(ctx, q, uid, "planetary.enter")
	return p.Company != nil && cid == *p.Company && allowed, err
}

func planetEvent(ctx context.Context, tx pgx.Tx, pid, actor int64, kind string, data any) error {
	raw, _ := json.Marshal(data)
	_, err := tx.Exec(ctx, `INSERT INTO planet_events(project,actor,kind,data) VALUES($1,NULLIF($2,0),$3,$4)`, pid, actor, kind, raw)
	return err
}

func planetReceived(ctx context.Context, tx pgx.Tx, p *ColonyProject) error {
	raw, _ := json.Marshal(p.Received)
	tag, err := tx.Exec(ctx, `UPDATE planet_projects SET received=$2 WHERE id=$1 AND status='funding'`, p.ID, raw)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return errors.New("Финансирование проекта закрыто")
	}
	for id, required := range p.Required {
		if p.Received[id] < required {
			return nil
		}
	}
	if _, err = tx.Exec(ctx, `UPDATE planet_projects SET status='building',last_tick=(SELECT tick FROM world WHERE id=1) WHERE id=$1`, p.ID); err != nil {
		return err
	}
	p.Status = "building"
	return planetEvent(ctx, tx, p.ID, 0, "building", nil)
}

// Returns are escrowed independently of membership and source availability.
// Original personal warehouses are restored even if a station was deactivated;
// if its stock cap or a wallet range prevents delivery, the claim remains pending.
func claimPlanetRefund(ctx context.Context, tx pgx.Tx, id, uid int64) error {
	var resource, station string
	var amount int64
	var star int
	var claimed bool
	if err := tx.QueryRow(ctx, `SELECT resource,amount,star,station,claimed FROM planet_refunds WHERE id=$1 AND user_id=$2 FOR UPDATE`, id, uid).Scan(&resource, &amount, &star, &station, &claimed); err != nil {
		return err
	}
	if claimed {
		return nil
	}
	if resource == "credits" {
		if err := wallet.Reason(ctx, tx, "Возврат колониального взноса"); err != nil {
			return err
		}
		if err := wallet.Credit(ctx, tx, uid, amount); err != nil {
			return err
		}
	} else {
		cargo, err := inventory.Warehouse(ctx, tx, uid, star, station)
		if err != nil {
			return err
		}
		if int64(cargo[resource])+amount > 1000000000 {
			return errors.New("Склад заполнен")
		}
		cargo[resource] += int(amount)
		if err = inventory.PutWarehouse(ctx, tx, uid, star, station, cargo); err != nil {
			return err
		}
	}
	_, err := tx.Exec(ctx, `UPDATE planet_refunds SET claimed=true WHERE id=$1`, id)
	return err
}

func refundContribution(ctx context.Context, tx pgx.Tx, pid, contribution, actor int64, percent int) error {
	var uid, amount int64
	var resource, station string
	var star int
	var returned, admin bool
	if err := tx.QueryRow(ctx, `SELECT actor,resource,amount,source_star,source_station,returned,admin_grant FROM planet_contributions WHERE id=$1 AND project=$2 FOR UPDATE`, contribution, pid).Scan(&uid, &resource, &amount, &star, &station, &returned, &admin); err != nil {
		return err
	}
	if returned || admin {
		return errors.New("Вклад уже возвращён или выдан администратором")
	}
	actual := amount * int64(percent) / 100
	if resource == "colony_core" {
		actual = amount
	}
	if _, err := tx.Exec(ctx, `UPDATE planet_contributions SET returned=true WHERE id=$1`, contribution); err != nil {
		return err
	}
	var refund int64
	if err := tx.QueryRow(ctx, `INSERT INTO planet_refunds(contribution,user_id,resource,amount,star,station) VALUES($1,$2,$3,$4,$5,$6) RETURNING id`, contribution, uid, resource, actual, star, station).Scan(&refund); err != nil {
		return err
	}
	// Savepoint keeps a full wallet/warehouse from blocking cancellation/expiry.
	sp, err := tx.Begin(ctx)
	if err != nil {
		return err
	}
	if err = claimPlanetRefund(ctx, sp, refund, uid); err != nil {
		if e := sp.Rollback(ctx); e != nil {
			return e
		}
	} else if err = sp.Commit(ctx); err != nil {
		return err
	}
	return planetEvent(ctx, tx, pid, actor, "refund", map[string]any{"contribution": contribution, "recipient": uid, "resource": resource, "amount": actual, "request": amount})
}

func cancelColony(ctx context.Context, tx pgx.Tx, p *ColonyProject, actor int64, status string) error {
	if p.Status != "funding" {
		return errors.New("Отмена доступна только до строительства")
	}
	rows, err := tx.Query(ctx, `SELECT id FROM planet_contributions WHERE project=$1 AND NOT returned AND NOT admin_grant ORDER BY actor,id`, p.ID)
	if err != nil {
		return err
	}
	ids := []int64{}
	for rows.Next() {
		var id int64
		if err = rows.Scan(&id); err != nil {
			rows.Close()
			return err
		}
		ids = append(ids, id)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return err
	}
	for _, id := range ids {
		if err = refundContribution(ctx, tx, p.ID, id, actor, p.RefundPercent); err != nil {
			return err
		}
	}
	if _, err = tx.Exec(ctx, `UPDATE planet_projects SET status=$2,finished_at=now() WHERE id=$1`, p.ID, status); err != nil {
		return err
	}
	p.Status = status
	return planetEvent(ctx, tx, p.ID, actor, status, nil)
}

func AdvanceColonies(ctx context.Context, tx pgx.Tx, tick int64) error {
	rows, err := tx.Query(ctx, `SELECT id FROM planet_projects WHERE status='building' OR (status='funding' AND expires_at<=now()) ORDER BY id FOR UPDATE`)
	if err != nil {
		return err
	}
	ids := []int64{}
	for rows.Next() {
		var id int64
		if err = rows.Scan(&id); err != nil {
			rows.Close()
			return err
		}
		ids = append(ids, id)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return err
	}
	for _, id := range ids {
		p, e := readColonyProject(ctx, tx, id)
		if e != nil {
			return e
		}
		if p.Status == "funding" {
			if e = cancelColony(ctx, tx, &p, 0, "expired"); e != nil {
				return e
			}
			continue
		}
		if p.LastTick >= tick {
			continue
		}
		p.Progress = min(p.BuildTicks, p.Progress+int(tick-p.LastTick))
		if _, e = tx.Exec(ctx, `UPDATE planet_projects SET progress=$2,last_tick=$3 WHERE id=$1`, id, p.Progress, tick); e != nil {
			return e
		}
		if p.Progress < p.BuildTicks {
			continue
		}
		if _, e = tx.Exec(ctx, `INSERT INTO planet_colonies(project,surface,sector,owner_user,owner_company) VALUES($1,$2,$3,$4,$5)`, id, p.Surface, p.Sector, p.User, p.Company); e != nil {
			return e
		}
		if _, e = tx.Exec(ctx, `UPDATE planet_projects SET status='complete',finished_at=now() WHERE id=$1`, id); e != nil {
			return e
		}
		if e = planetEvent(ctx, tx, id, 0, "complete", nil); e != nil {
			return e
		}
		var name string
		if e = tx.QueryRow(ctx, `SELECT name FROM planet_surfaces WHERE id=$1`, p.Surface).Scan(&name); e != nil {
			return e
		}
		data, _ := json.Marshal(map[string]any{"surface": p.Surface, "project": id, "sector": p.Sector, "planet": name})
		_, e = tx.Exec(ctx, `INSERT INTO notifications(user_id,kind,level,message,data) SELECT id,'colony','success','Колонизация завершена',$1 FROM users WHERE id=$2 OR id=$3 OR ($4::bigint IS NOT NULL AND company_id=$4)`, data, p.User, p.Initiator, p.Company)
		if e != nil {
			return e
		}
	}
	return nil
}
