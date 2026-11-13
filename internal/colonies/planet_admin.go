package colonies

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"github.com/yomezza/cosmosimulator/internal/chatintegration"
	"net/http"
	"strconv"

	"github.com/jackc/pgx/v5"

	"github.com/yomezza/cosmosimulator/internal/geography"
	"github.com/yomezza/cosmosimulator/internal/platform/httpkit"
	"github.com/yomezza/cosmosimulator/internal/platform/idempotency"
	"github.com/yomezza/cosmosimulator/internal/universe"
)

// No cascade: these dependencies are deliberately inspected and removed in FK
// order. World lock 784218 serializes against funding, refunds, landing and ticks.
type SurfaceResetInfo struct {
	Star           int              `json:"star"`
	Planet         int              `json:"planet"`
	Name           string           `json:"name"`
	Surface        int64            `json:"surface"`
	Revision       int64            `json:"revision"`
	Sectors        int              `json:"sectors"`
	Projects       int              `json:"projects"`
	Active         int              `json:"active"`
	Building       int              `json:"building"`
	Colonies       int              `json:"colonies"`
	Corporate      int              `json:"corporate"`
	Visits         int              `json:"visits"`
	PendingRefunds int              `json:"pending_refunds"`
	Contributions  int              `json:"contributions"`
	LocalLocations int              `json:"local_locations"`
	RuntimeState   string           `json:"runtime_state"`
	Invested       map[string]int64 `json:"invested"`
	State          string           `json:"-"`
	Token          string           `json:"token"`
	Blocked        string           `json:"blocked"`
}

func resetInfo(ctx context.Context, q pgx.Tx, star, planet int, name string) (SurfaceResetInfo, error) {
	v := SurfaceResetInfo{Star: star, Planet: planet, Name: name, Invested: map[string]int64{}}
	err := q.QueryRow(ctx, `SELECT COALESCE((SELECT id FROM planet_surfaces WHERE star=$1 AND planet=$2),0),COALESCE((SELECT revision FROM planet_surface_controls WHERE star=$1 AND planet=$2),0)`, star, planet).Scan(&v.Surface, &v.Revision)
	if err != nil {
		return v, err
	}
	if err = q.QueryRow(ctx, `SELECT (SELECT count(*) FROM planet_local_locations WHERE surface=$1),COALESCE((SELECT state FROM planet_runtime_controls WHERE surface=$1),'')`, v.Surface).Scan(&v.LocalLocations, &v.RuntimeState); err != nil {
		return v, err
	}
	err = q.QueryRow(ctx, `SELECT
 (SELECT count(*) FROM planet_sectors WHERE surface=$1),
 (SELECT count(*) FROM planet_projects WHERE surface=$1),
 (SELECT count(*) FROM planet_projects WHERE surface=$1 AND status IN('funding','building')),
 (SELECT count(*) FROM planet_projects WHERE surface=$1 AND status='building'),
 (SELECT count(*) FROM planet_colonies WHERE surface=$1),
 (SELECT count(*) FROM planet_projects WHERE surface=$1 AND owner_company IS NOT NULL),
 (SELECT count(*) FROM planet_visits v JOIN planet_colonies c ON c.id=v.colony WHERE c.surface=$1 AND v.state<>'ended'),
 (SELECT count(*) FROM planet_refunds r JOIN planet_contributions c ON c.id=r.contribution JOIN planet_projects p ON p.id=c.project WHERE p.surface=$1 AND NOT r.claimed),
 (SELECT count(*) FROM planet_contributions c JOIN planet_projects p ON p.id=c.project WHERE p.surface=$1),
 md5(COALESCE((SELECT jsonb_agg((to_jsonb(p)-'progress'-'last_tick') ORDER BY p.id)::text FROM planet_projects p WHERE p.surface=$1),'')||COALESCE((SELECT jsonb_agg(to_jsonb(c) ORDER BY c.id)::text FROM planet_contributions c JOIN planet_projects p ON p.id=c.project WHERE p.surface=$1),''))`, v.Surface).Scan(&v.Sectors, &v.Projects, &v.Active, &v.Building, &v.Colonies, &v.Corporate, &v.Visits, &v.PendingRefunds, &v.Contributions, &v.State)
	if err != nil {
		return v, err
	}
	rows, err := q.Query(ctx, `SELECT c.resource,sum(c.amount)::bigint FROM planet_contributions c JOIN planet_projects p ON p.id=c.project WHERE p.surface=$1 AND NOT c.returned AND NOT c.admin_grant GROUP BY c.resource`, v.Surface)
	if err != nil {
		return v, err
	}
	for rows.Next() {
		var id string
		var amount int64
		if err = rows.Scan(&id, &amount); err != nil {
			break
		}
		v.Invested[id] = amount
	}
	if err == nil {
		err = rows.Err()
	}
	rows.Close()
	if err != nil {
		return v, err
	}
	if v.Visits > 0 {
		v.Blocked = "На планете зарегистрированы высадки. Сначала вернитесь в космос."
	} else if v.PendingRefunds > 0 {
		v.Blocked = "Есть неполученные возвраты. Сначала получите их в разделе Колонии."
	}
	var named bool
	if err = q.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM aj_colony_names WHERE surface_id=$1)`, v.Surface).Scan(&named); err != nil {
		return v, err
	}
	if named {
		v.Blocked = "На поверхности есть именованные колонии. Удаление запрещено, чтобы сохранить историю названий и владения."
	}
	// State has json:"-": include it explicitly in the optimistic confirmation token.
	v.Token = idempotency.Digest(struct {
		Info  SurfaceResetInfo
		State string
	}{v, v.State})
	return v, nil
}

func storeSurface(ctx context.Context, tx pgx.Tx, star int, p universe.Planet, seed string, surface geography.PlanetSurface) (int64, error) {
	raw, err := json.Marshal(surface)
	if err != nil {
		return 0, err
	}
	var id int64
	err = tx.QueryRow(ctx, `INSERT INTO planet_surfaces(star,planet,version,seed,name,kind,data) VALUES($1,$2,1,$3,$4,$5,$6) RETURNING id`, star, p.ID, seed, p.Name, p.Kind, raw).Scan(&id)
	if err != nil {
		return 0, err
	}
	batch := &pgx.Batch{}
	for _, sector := range surface.Sectors {
		v, e := json.Marshal(sector)
		if e != nil {
			return 0, e
		}
		batch.Queue(`INSERT INTO planet_sectors(surface,number,data) VALUES($1,$2,$3)`, id, sector.Number, v)
	}
	err = tx.SendBatch(ctx, batch).Close()
	return id, err
}

func eraseSurface(ctx context.Context, tx pgx.Tx, id int64) error {
	// Keep financial_ledger and operation receipts. Retire create/land receipts
	// that otherwise return now-invalid project/surface links on retry.
	queries := []string{
		`UPDATE planet_runtime_controls SET state='retiring' WHERE surface=$1`,
		`UPDATE planet_transfer_controls SET generation=generation+1,reserved_ship=NULL,active_operation=NULL,revoke_requested_at=NULL WHERE EXISTS(SELECT 1 FROM planet_handoffs h WHERE h.surface=$1 AND h.user_id=planet_transfer_controls.user_id AND h.operation=planet_transfer_controls.active_operation)`,
		`DELETE FROM planet_handoffs WHERE surface=$1`,
		`DELETE FROM planet_landing_sites WHERE surface=$1`,
		`UPDATE planet_requests SET result=result||'{"surface_reset":true}'::jsonb WHERE result->>'surface'=$1::bigint::text OR result->>'project' IN (SELECT id::text FROM planet_projects WHERE surface=$1)`,
		`DELETE FROM notifications WHERE kind IN('colony','landing_ready') AND data->>'surface'=$1::bigint::text`,
		`DELETE FROM planet_colonies WHERE surface=$1`,
		`DELETE FROM planet_events WHERE project IN (SELECT id FROM planet_projects WHERE surface=$1)`,
		`DELETE FROM planet_refunds WHERE contribution IN (SELECT c.id FROM planet_contributions c JOIN planet_projects p ON p.id=c.project WHERE p.surface=$1)`,
		`DELETE FROM planet_contributions WHERE project IN (SELECT id FROM planet_projects WHERE surface=$1)`,
		`DELETE FROM planet_projects WHERE surface=$1`,
		`DELETE FROM planet_sectors WHERE surface=$1`,
		`DELETE FROM planet_runtime_controls WHERE surface=$1`,
		`DELETE FROM planet_surfaces WHERE id=$1`,
	}
	for _, sql := range queries {
		if _, err := tx.Exec(ctx, sql, id); err != nil {
			return err
		}
	}
	return nil
}

func (a *Service) AdminPlanetSurface(w http.ResponseWriter, r *http.Request) {
	u, ok := a.Auth.User(w, r)
	if !ok {
		return
	}
	if u.Role != "admin" {
		httpkit.Fail(w, 403, "Требуется роль администратора")
		return
	}
	var in struct {
		Action  string `json:"action"`
		Star    int    `json:"star"`
		Planet  int    `json:"planet"`
		Token   string `json:"token"`
		Confirm string `json:"confirm"`
		Forfeit bool   `json:"forfeit"`
		Request string `json:"request"`
	}
	if r.Method == "GET" {
		in.Star, _ = strconv.Atoi(r.URL.Query().Get("star"))
		in.Planet, _ = strconv.Atoi(r.URL.Query().Get("planet"))
	} else {
		if !httpkit.Decode(w, r, &in) {
			return
		}
		required := "УДАЛИТЬ"
		if in.Action == "regenerate" {
			required = "ПЕРЕГЕНЕРИРОВАТЬ"
		}
		if (in.Action != "delete" && in.Action != "regenerate") || in.Confirm != required || len(in.Request) < 8 || len(in.Request) > 100 || len(in.Token) != 64 {
			httpkit.Fail(w, 400, "Неверное подтверждение операции")
			return
		}
	}
	ctx := r.Context()
	tx, err := a.DB.Begin(ctx)
	if err != nil {
		httpkit.Internal(w, err)
		return
	}
	defer tx.Rollback(context.Background())
	// Read-only preview waits for a consistent snapshot. Mutation refuses contention
	// rather than queuing two destructive requests behind one another.
	if r.Method == "GET" {
		_, err = tx.Exec(ctx, `SELECT pg_advisory_xact_lock_shared(784218)`)
	} else {
		var locked bool
		err = tx.QueryRow(ctx, `SELECT pg_try_advisory_xact_lock(784218)`).Scan(&locked)
		if err == nil && !locked {
			httpkit.Fail(w, 409, "Мир занят другой операцией. Повторите попытку.")
			return
		}
	}
	if err != nil {
		httpkit.Internal(w, err)
		return
	}
	var role string
	if err = tx.QueryRow(ctx, `SELECT role FROM users WHERE id=$1`, u.ID).Scan(&role); err != nil {
		httpkit.Internal(w, err)
		return
	}
	if role != "admin" {
		httpkit.Fail(w, 403, "Требуется роль администратора")
		return
	}
	digest := idempotency.Digest(in)
	if r.Method == "POST" {
		// Role changes use the same world lock; also lock the actor for this mutation.
		if err = tx.QueryRow(ctx, `SELECT role FROM users WHERE id=$1 FOR UPDATE`, u.ID).Scan(&role); err != nil {
			httpkit.Internal(w, err)
			return
		}
		if role != "admin" {
			httpkit.Fail(w, 403, "Требуется роль администратора")
			return
		}
		var previous string
		var raw []byte
		err = tx.QueryRow(ctx, `SELECT digest,result FROM planet_requests WHERE user_id=$1 AND request=$2`, u.ID, in.Request).Scan(&previous, &raw)
		if err == nil {
			if previous != digest {
				httpkit.Fail(w, 409, "Идентификатор операции уже использован")
			} else {
				httpkit.Reply(w, 200, json.RawMessage(raw))
			}
			return
		}
		if !errors.Is(err, pgx.ErrNoRows) {
			httpkit.Internal(w, err)
			return
		}
	}
	p, err := a.PlanetAt(ctx, tx, u.ID, in.Star, in.Planet)
	if err != nil {
		httpkit.Fail(w, 403, "Планета недоступна или не исследована")
		return
	}
	if p.Kind == "Газовый гигант" {
		httpkit.Fail(w, 409, "У газового гиганта нет твёрдой поверхности")
		return
	}
	info, err := resetInfo(ctx, tx, in.Star, in.Planet, p.Name)
	if err != nil {
		httpkit.Internal(w, err)
		return
	}
	if r.Method == "GET" {
		httpkit.Reply(w, 200, info)
		return
	}
	// Failed/blocked attempts are audited separately after rolling back; SQL
	// details are only sent to the server log by internal().
	reject := func(message string) {
		_ = tx.Rollback(context.Background())
		raw, _ := json.Marshal(map[string]any{"star": in.Star, "planet": in.Planet, "operation": in.Action, "result": "blocked", "reason": message})
		if _, e := a.DB.Exec(ctx, `INSERT INTO admin_log(actor,action,data) VALUES($1,'planet_surface_reset',$2)`, u.ID, raw); e != nil {
			httpkit.Internal(w, e)
			return
		}
		httpkit.Fail(w, 409, message)
	}
	if info.Token != in.Token {
		reject("Данные планеты изменились. Откройте предупреждение заново.")
		return
	}
	if info.Blocked != "" {
		reject(info.Blocked)
		return
	}
	if len(info.Invested) > 0 && !in.Forfeit {
		reject("Подтвердите безвозвратное списание вкладов в проекты.")
		return
	}
	if in.Action == "delete" && info.Surface == 0 {
		reject("Генерация поверхности отсутствует")
		return
	}
	if info.Surface > 0 {
		ready, e := chatintegration.PrepareSurface(ctx, tx, info.Surface)
		if e != nil {
			httpkit.Internal(w, e)
			return
		}
		if !ready {
			if e = tx.Commit(ctx); e != nil {
				httpkit.Internal(w, e)
				return
			}
			httpkit.Fail(w, 409, "Подготовка поверхности: служба чата очищает пространственные каналы. Повторите операцию после очистки.")
			return
		}
	}
	if info.LocalLocations > 0 {
		// Do not delete planetary-owned data from cosmic. Fence new loads first;
		// the live planetary registry drains pins and its writer clears snapshots.
		_, err = tx.Exec(ctx, `UPDATE planet_runtime_controls SET state='retiring' WHERE surface=$1`, info.Surface)
		if err == nil {
			_, err = tx.Exec(ctx, `INSERT INTO admin_log(actor,action,data) VALUES($1,'planet_surface_retire',jsonb_build_object('surface',$2::bigint,'operation',$3::text))`, u.ID, info.Surface, in.Action)
		}
		if err == nil {
			err = tx.Commit(ctx)
		}
		if err != nil {
			httpkit.Internal(w, err)
			return
		}
		httpkit.Fail(w, 409, "Подготовка поверхности к удалению: планетарная служба освобождает локальные локации. После очистки откройте предупреждение заново и повторите операцию. Планетарная служба должна быть запущена.")
		return
	}
	var next geography.PlanetSurface
	var seed string
	if in.Action == "regenerate" {
		// Administrative regeneration uses the established four size categories.
		entropy := make([]byte, 32)
		if _, err = rand.Read(entropy); err != nil {
			httpkit.Internal(w, err)
			return
		}
		seed = hex.EncodeToString(entropy)
		next = geography.Generate(p, seed, []int{50, 100, 160, 200}[geography.PlanetSize(p)])
	}
	if info.Surface != 0 {
		err = eraseSurface(ctx, tx, info.Surface)
	}
	var id int64
	if err == nil && in.Action == "regenerate" {
		id, err = storeSurface(ctx, tx, in.Star, p, seed, next)
	}
	if err == nil {
		_, err = tx.Exec(ctx, `INSERT INTO planet_surface_controls(star,planet,revision,deleted) VALUES($1,$2,1,$3) ON CONFLICT(star,planet) DO UPDATE SET revision=planet_surface_controls.revision+1,deleted=EXCLUDED.deleted`, in.Star, in.Planet, in.Action == "delete")
	}
	result := map[string]any{"ok": true, "surface": id, "old_surface": info.Surface, "revision": info.Revision + 1, "star": in.Star, "planet": in.Planet}
	raw, _ := json.Marshal(result)
	if err == nil {
		_, err = tx.Exec(ctx, `INSERT INTO planet_requests(user_id,request,digest,result) VALUES($1,$2,$3,$4)`, u.ID, in.Request, digest, raw)
	}
	if err == nil {
		audit, _ := json.Marshal(map[string]any{"operation": in.Action, "result": "success", "before": info, "new_surface": id, "forfeit": in.Forfeit, "seed": seed})
		_, err = tx.Exec(ctx, `INSERT INTO admin_log(actor,action,data) VALUES($1,'planet_surface_reset',$2)`, u.ID, audit)
	}
	if err == nil {
		err = tx.Commit(ctx)
	}
	if err != nil {
		_ = tx.Rollback(context.Background())
		audit, _ := json.Marshal(map[string]any{"star": in.Star, "planet": in.Planet, "operation": in.Action, "result": "error"})
		_, _ = a.DB.Exec(ctx, `INSERT INTO admin_log(actor,action,data) VALUES($1,'planet_surface_reset',$2)`, u.ID, audit)
		httpkit.Internal(w, err)
		return
	}
	httpkit.Reply(w, 200, result)
}
