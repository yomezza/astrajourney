package planner

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/yomezza/cosmosimulator/internal/fleet"
	"github.com/yomezza/cosmosimulator/internal/logistics"
	extraction "github.com/yomezza/cosmosimulator/internal/mining"
	"github.com/yomezza/cosmosimulator/internal/platform/httpkit"
	"github.com/yomezza/cosmosimulator/internal/platform/postgres"
	resourcecatalog "github.com/yomezza/cosmosimulator/internal/resources"
	"github.com/yomezza/cosmosimulator/internal/universe"
)

type AutomationTask struct {
	Priority     int    `json:"priority"`
	SourceKind   string `json:"source_kind"`
	DestKind     string `json:"dest_kind"`
	Company      int64  `json:"company"`
	ID           int64  `json:"id"`
	User         int64  `json:"user"`
	Ship         int64  `json:"ship"`
	Kind         string `json:"kind"`
	SourceStar   int    `json:"source_star"`
	SourceObject string `json:"source_object"`
	DestStar     int    `json:"dest_star"`
	DestStation  string `json:"dest_station"`
	Resource     string `json:"resource"`
	Quantity     int    `json:"quantity"`
	Progress     int    `json:"progress"`
	Carried      int    `json:"carried"`
	Pending      int    `json:"pending"`
	Phase        string `json:"phase"`
	Status       string `json:"status"`
	Error        string `json:"error"`
}

func (a *Service) Planner(w http.ResponseWriter, r *http.Request) {
	u, ok := a.Auth.User(w, r)
	if !ok {
		return
	}
	ctx := r.Context()
	if r.Method == "GET" {
		view := r.URL.Query().Get("view")
		if view != "" && view != "active" && view != "history" {
			httpkit.Fail(w, 400, "Некорректный раздел задач")
			return
		}
		history := view == "history"
		list, err := postgres.QueryMaps(ctx, a.DB, `SELECT t.*,s.name AS ship_name FROM automation_tasks t JOIN ships s ON s.id=t.ship_id WHERE t.user_id=$1 AND (($2 AND t.status IN('completed','cancelled')) OR (NOT $2 AND t.status IN('queued','running','paused'))) ORDER BY CASE WHEN NOT $2 AND (t.carried>0 OR t.pending>0) THEN 0 ELSE 1 END,CASE WHEN NOT $2 THEN -t.priority ELSE 0 END,CASE WHEN $2 THEN -t.id ELSE t.id END LIMIT 50`, u.ID, history)
		if err != nil {
			httpkit.Internal(w, err)
			return
		}
		httpkit.Reply(w, 200, list)
		return
	}
	var in struct {
		AutomationTask
		Action string `json:"action"`
	}
	if !httpkit.Decode(w, r, &in) {
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
	if _, err = tx.Exec(ctx, `SELECT id FROM users WHERE id=$1 FOR UPDATE`, u.ID); err != nil {
		httpkit.Internal(w, err)
		return
	}
	if in.Action == "raise" || in.Action == "lower" || in.Action == "activate" {
		var shipID int64
		if err = tx.QueryRow(ctx, `SELECT ship_id FROM automation_tasks WHERE id=$1 AND user_id=$2 AND status IN('queued','running','paused')`, in.ID, u.ID).Scan(&shipID); errors.Is(err, pgx.ErrNoRows) {
			httpkit.Fail(w, 404, "Задача не найдена")
			return
		} else if err != nil {
			httpkit.Internal(w, err)
			return
		}
		rows, e := tx.Query(ctx, `SELECT id FROM automation_tasks WHERE ship_id=$1 AND user_id=$2 AND status IN('queued','running','paused') ORDER BY priority DESC,id`, shipID, u.ID)
		if e != nil {
			httpkit.Internal(w, e)
			return
		}
		ids := []int64{}
		index := -1
		for rows.Next() {
			var id int64
			if e = rows.Scan(&id); e != nil {
				break
			}
			if id == in.ID {
				index = len(ids)
			}
			ids = append(ids, id)
		}
		rows.Close()
		if e == nil {
			e = rows.Err()
		}
		if e != nil {
			httpkit.Internal(w, e)
			return
		}
		if index < 0 {
			httpkit.Fail(w, 404, "Задача не найдена")
			return
		}
		if in.Action == "raise" && index > 0 {
			ids[index], ids[index-1] = ids[index-1], ids[index]
		}
		if in.Action == "lower" && index < len(ids)-1 {
			ids[index], ids[index+1] = ids[index+1], ids[index]
		}
		if in.Action == "activate" {
			var blocked bool
			if e = tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM automation_tasks WHERE ship_id=$1 AND id<>$2 AND status='paused' AND (carried>0 OR pending>0))`, shipID, in.ID).Scan(&blocked); e != nil {
				httpkit.Internal(w, e)
				return
			}
			if blocked {
				httpkit.Fail(w, 409, "Завершите или отмените приостановленную задачу с грузом")
				return
			}
			ids = append([]int64{in.ID}, append(ids[:index], ids[index+1:]...)...)
			if _, e = tx.Exec(ctx, `UPDATE automation_tasks SET status='queued',error='',updated_at=now() WHERE id=$1`, in.ID); e != nil {
				httpkit.Internal(w, e)
				return
			}
		}
		for i, id := range ids {
			if _, e = tx.Exec(ctx, `UPDATE automation_tasks SET priority=$2,updated_at=now() WHERE id=$1`, id, len(ids)-i); e != nil {
				httpkit.Internal(w, e)
				return
			}
		}
	} else if in.Action != "create" {
		status := map[string]string{"pause": "paused", "resume": "queued", "cancel": "cancelled"}[in.Action]
		if status == "" {
			httpkit.Fail(w, 400, "Неизвестное действие")
			return
		}
		tag, e := tx.Exec(ctx, `UPDATE automation_tasks SET status=$3,error='',pending=CASE WHEN $3='queued' AND NOT EXISTS(SELECT 1 FROM mining_jobs j WHERE j.ship_id=automation_tasks.ship_id) THEN 0 ELSE pending END,updated_at=now() WHERE id=$1 AND user_id=$2 AND status IN('queued','running','paused')`, in.ID, u.ID, status)
		err = e
		if err == nil && tag.RowsAffected() == 0 {
			httpkit.Fail(w, 404, "Задача не найдена")
			return
		}
	} else {
		var owner int64
		if err = tx.QueryRow(ctx, `SELECT user_id FROM ships WHERE id=$1`, in.Ship).Scan(&owner); err != nil || owner != u.ID {
			httpkit.Fail(w, 404, "Корабль не найден")
			return
		}
		g, e := a.World.Load(ctx)
		if e != nil {
			httpkit.Internal(w, e)
			return
		}
		if _, ok := resourcecatalog.Lookup(in.Resource); !ok || in.Quantity < 1 || in.Quantity > 100000 || in.SourceStar < 1 || in.SourceStar > len(g.Stars) || in.DestStar < 1 || in.DestStar > len(g.Stars) || (in.Kind != "mine" && in.Kind != "haul") {
			httpkit.Fail(w, 400, "Некорректная задача")
			return
		}
		if g.Stars[in.SourceStar-1].Deleted || g.Stars[in.DestStar-1].Deleted {
			httpkit.Fail(w, 400, "Система удалена")
			return
		}
		var count int
		if err = tx.QueryRow(ctx, `SELECT count(*) FROM automation_tasks WHERE user_id=$1 AND status IN('queued','running','paused')`, u.ID).Scan(&count); err != nil {
			httpkit.Internal(w, err)
			return
		}
		if count >= 20 {
			httpkit.Fail(w, 409, "В очереди уже 20 задач")
			return
		}
		if in.Kind == "mine" {
			valid := false
			for _, site := range universe.SystemSites(g.Stars[in.SourceStar-1]) {
				if site.ID == in.SourceObject {
					for _, res := range site.Resources {
						if res == in.Resource {
							valid = true
						}
					}
				}
			}
			var known bool
			if err = tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM surveys WHERE user_id=$1 AND star=$2)`, u.ID, in.SourceStar).Scan(&known); err != nil {
				httpkit.Internal(w, err)
				return
			}
			if !valid || !known {
				httpkit.Fail(w, 400, "Исследуйте объект добычи и выберите ресурс")
				return
			}
			if in.DestStation != "" && in.DestStar != in.SourceStar {
				httpkit.Fail(w, 400, "Для добычи выберите склад в той же системе")
				return
			}
		} else if in.SourceStar == in.DestStar && in.SourceObject == in.DestStation {
			httpkit.Fail(w, 400, "Склады должны различаться")
			return
		}
		for _, port := range []struct {
			star     int
			id       string
			required bool
		}{{in.SourceStar, in.SourceObject, in.Kind == "haul"}, {in.DestStar, in.DestStation, in.Kind == "haul" || in.DestStation != ""}} {
			if !port.required {
				continue
			}
			var exists bool
			if err = tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM stations WHERE star=$1 AND object_id=$2 AND active)`, port.star, port.id).Scan(&exists); err != nil {
				httpkit.Internal(w, err)
				return
			}
			if !exists {
				httpkit.Fail(w, 400, "Станция не найдена")
				return
			}
		}
		if in.Kind == "haul" && len(g.Path(in.SourceStar, in.DestStar)) == 0 {
			httpkit.Fail(w, 400, "Нет допустимого маршрута")
			return
		}
		_, err = tx.Exec(ctx, `INSERT INTO automation_tasks(user_id,ship_id,kind,source_star,source_object,dest_star,dest_station,resource,quantity) VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9)`, u.ID, in.Ship, in.Kind, in.SourceStar, in.SourceObject, in.DestStar, in.DestStation, in.Resource, in.Quantity)
	}
	if err != nil {
		httpkit.Internal(w, err)
		return
	}
	if err = tx.Commit(ctx); err != nil {
		httpkit.Internal(w, err)
		return
	}
	httpkit.Reply(w, 200, map[string]bool{"ok": true})
}

func taskFuel(s *fleet.Ship, needed int, protected ...int) error {
	reserve := 0
	if len(protected) > 0 {
		reserve = protected[0]
	}
	if needed > s.Hull().Fuel {
		return errors.New("Маршрут длиннее запаса топлива")
	}
	if s.Fuel+max(0, s.Cargo["agni"]-reserve)*5 < needed {
		return errors.New("Недостаточно топлива: добавьте водород в трюм")
	}
	for s.Fuel < needed {
		s.Cargo["agni"]--
		s.Fuel = min(s.Hull().Fuel, s.Fuel+5)
	}
	return nil
}

func taskNavigate(ctx context.Context, tx pgx.Tx, uid int64, s *fleet.Ship, g *universe.Galaxy, star int, object string, dock bool, protected ...int) (bool, error) {
	if s.Destination != nil || s.LocalTarget != nil || len(s.Waypoints) > 0 {
		return false, nil
	}
	if s.Star != star {
		path := g.Path(s.Star, star)
		if len(path) < 2 {
			return false, errors.New("Нет допустимого маршрута")
		}
		cost := (len(path) - 1) * s.Hull().JumpFuel
		if err := taskFuel(s, cost, protected...); err != nil {
			return false, err
		}
		s.Fuel -= cost
		now := time.Now()
		at := now.Add(time.Duration((len(path)-1)*s.Hull().JumpSeconds) * time.Second)
		s.Destination = &star
		s.Departure = &now
		s.Arrival = &at
		s.Path = path
		return false, fleet.SaveShip(ctx, tx, uid, *s)
	}
	if dock {
		var exists bool
		if err := tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM stations WHERE star=$1 AND object_id=$2 AND active)`, star, object).Scan(&exists); err != nil {
			return false, err
		}
		if !exists {
			return false, errors.New("Станция не найдена")
		}
	}
	mode := "approach"
	if dock {
		mode = "docked"
	}
	if s.ObjectID == object && s.PositionMode == mode {
		return true, nil
	}
	if err := taskFuel(s, 1, protected...); err != nil {
		return false, err
	}
	s.Fuel--
	now := time.Now()
	at := now.Add(5 * time.Second)
	s.LocalTarget = &object
	s.LocalMode = &mode
	s.LocalStarted = &now
	s.LocalArrival = &at
	return false, fleet.SaveShip(ctx, tx, uid, *s)
}

func taskStorage(ctx context.Context, tx pgx.Tx, t *AutomationTask, s *fleet.Ship, withdraw bool, amount int) error {
	kind := t.DestKind
	if withdraw {
		kind = t.SourceKind
	}
	if kind == "" {
		kind = "personal"
	}
	loc := logistics.CargoLocation{Kind: kind, Star: s.Star, Station: s.ObjectID, Company: t.Company}
	m, err := logistics.StorageAccess(ctx, tx, t.User, loc, withdraw)
	if err != nil {
		return err
	}
	if withdraw {
		if m[t.Resource] < amount {
			return errors.New("Недостаточно товара на складе")
		}
		m[t.Resource] -= amount
		s.Cargo[t.Resource] += amount
	} else {
		if s.Cargo[t.Resource] < amount {
			return errors.New("Недостаточно груза")
		}
		if m[t.Resource] > 1000000000-amount {
			return errors.New("Склад заполнен")
		}
		m[t.Resource] += amount
		s.Cargo[t.Resource] -= amount
	}
	if err = logistics.WriteStorage(ctx, tx, t.User, loc, m); err != nil {
		return err
	}
	return fleet.SaveShip(ctx, tx, t.User, *s)
}

func stepAutomation(ctx context.Context, tx pgx.Tx, t *AutomationTask, s *fleet.Ship, g *universe.Galaxy) error {
	navigate := func(star int, object string, dock bool) (bool, error) {
		reserve := 0
		if t.Resource == "agni" {
			reserve = t.Carried
			if t.Kind == "unload" {
				reserve = t.Quantity
			}
		}
		return taskNavigate(ctx, tx, t.User, s, g, star, object, dock, reserve)
	}
	if s.Destination != nil || s.LocalTarget != nil || len(s.Waypoints) > 0 {
		return nil
	}
	var mining bool
	if err := tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM mining_jobs WHERE ship_id=$1)`, s.ID).Scan(&mining); err != nil {
		return err
	}
	if mining {
		return nil
	}
	if t.Pending > 0 {
		return errors.New("Добыча была прервана. Возобновите задачу")
	}
	if t.Kind == "load" {
		arrived, e := navigate(t.SourceStar, t.SourceObject, true)
		if e != nil || !arrived {
			return e
		}
		if s.CargoUsed()+t.Quantity > s.Capacity() {
			return errors.New("Трюм заполнен")
		}
		if e = taskStorage(ctx, tx, t, s, true, t.Quantity); e != nil {
			return e
		}
		t.Progress = t.Quantity
		t.Status = "completed"
		return nil
	}
	if t.Kind == "unload" {
		if s.Cargo[t.Resource] < t.Quantity {
			return errors.New("Недостаточно груза")
		}
		arrived, e := navigate(t.DestStar, t.DestStation, true)
		if e != nil || !arrived {
			return e
		}
		if e = taskStorage(ctx, tx, t, s, false, t.Quantity); e != nil {
			return e
		}
		t.Progress = t.Quantity
		t.Status = "completed"
		return nil
	}
	if t.Kind == "mine" {
		remaining := t.Quantity - t.Progress
		if t.Phase == "dest" || remaining == 0 && t.DestStation != "" && t.Carried > 0 {
			arrived, err := navigate(t.DestStar, t.DestStation, true)
			if err != nil || !arrived {
				return err
			}
			if err = taskStorage(ctx, tx, t, s, false, t.Carried); err != nil {
				return err
			}
			t.Carried = 0
			t.Phase = "source"
		}
		if remaining == 0 {
			t.Status = "completed"
			return nil
		}
		planet := 0
		for _, site := range universe.SystemSites(g.Stars[t.SourceStar-1]) {
			if site.ID == t.SourceObject {
				planet = site.Planet
			}
		}
		count := min(remaining, s.ExtractionYieldFor(t.SourceStar, planet, t.Resource))
		if s.Capacity()-s.CargoUsed() < count {
			if t.DestStation == "" || t.Carried == 0 {
				return errors.New("Трюм заполнен")
			}
			t.Phase = "dest"
			return nil
		}
		arrived, err := navigate(t.SourceStar, t.SourceObject, false)
		if err != nil || !arrived {
			return err
		}
		job, err := extraction.BeginExtraction(ctx, tx, t.User, *s, g.Stars[t.SourceStar-1], planet, t.Resource, remaining)
		if err != nil {
			return err
		}
		t.Pending = job.Quantity
		if _, err = tx.Exec(ctx, `UPDATE mining_jobs SET automation_id=$2 WHERE ship_id=$1`, s.ID, t.ID); err != nil {
			return err
		}
	} else {
		if t.Phase == "source" {
			arrived, err := navigate(t.SourceStar, t.SourceObject, true)
			if err != nil || !arrived {
				return err
			}
			amount := min(t.Quantity-t.Progress, s.Capacity()-s.CargoUsed())
			if amount < 1 {
				return errors.New("Трюм заполнен")
			}
			if err = taskStorage(ctx, tx, t, s, true, amount); err != nil {
				return err
			}
			t.Carried = amount
			t.Phase = "dest"
		} else {
			arrived, err := navigate(t.DestStar, t.DestStation, true)
			if err != nil || !arrived {
				return err
			}
			if err = taskStorage(ctx, tx, t, s, false, t.Carried); err != nil {
				return err
			}
			t.Progress += t.Carried
			t.Carried = 0
			t.Phase = "source"
			if t.Progress == t.Quantity {
				t.Status = "completed"
			}
		}
	}
	return nil
}

func (a *Service) AdvanceAutomation(ctx context.Context) error {
	rows, err := a.DB.Query(ctx, `SELECT t.id FROM automation_tasks t WHERE t.status IN('queued','running') AND t.id=(SELECT h.id FROM automation_tasks h WHERE h.ship_id=t.ship_id AND h.status IN('queued','running','paused') ORDER BY (h.carried>0 OR h.pending>0) DESC,h.priority DESC,h.id LIMIT 1) ORDER BY t.updated_at,t.id LIMIT 100`)
	if err != nil {
		return err
	}
	var ids []int64
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
		if err = a.advanceTask(ctx, id); err != nil {
			return err
		}
	}
	return nil
}

func (a *Service) advanceTask(ctx context.Context, id int64) error {
	tx, err := a.DB.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(context.Background())
	if _, err = tx.Exec(ctx, `SELECT pg_advisory_xact_lock(784218)`); err != nil {
		return err
	}
	var t AutomationTask
	err = tx.QueryRow(ctx, `SELECT id,user_id,ship_id,kind,source_star,source_object,dest_star,dest_station,resource,quantity,progress,carried,pending,phase,status,error,source_kind,dest_kind,COALESCE(company_id,0) FROM automation_tasks WHERE id=$1 FOR UPDATE`, id).Scan(&t.ID, &t.User, &t.Ship, &t.Kind, &t.SourceStar, &t.SourceObject, &t.DestStar, &t.DestStation, &t.Resource, &t.Quantity, &t.Progress, &t.Carried, &t.Pending, &t.Phase, &t.Status, &t.Error, &t.SourceKind, &t.DestKind, &t.Company)
	if err != nil {
		return err
	}
	if t.Status != "queued" && t.Status != "running" {
		return nil
	}
	var previous bool
	if err = tx.QueryRow(ctx, `SELECT COALESCE((SELECT id<>$2 FROM automation_tasks WHERE ship_id=$1 AND status IN('queued','running','paused') ORDER BY (carried>0 OR pending>0) DESC,priority DESC,id LIMIT 1),false)`, t.Ship, t.ID).Scan(&previous); err != nil {
		return err
	}
	if previous {
		return nil
	}
	if _, err = tx.Exec(ctx, `SELECT id FROM users WHERE id=$1 FOR UPDATE`, t.User); err != nil {
		return err
	}
	g, err := a.World.Load(ctx)
	if err != nil {
		return err
	}
	s, err := fleet.ReadShip(ctx, tx, t.Ship)
	if err != nil {
		return err
	}
	var busy bool
	if err = tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM research_jobs WHERE ship_id=$1 AND NOT paused) OR EXISTS(SELECT 1 FROM deliveries WHERE ship_id=$1 AND status='carrying')`, s.ID).Scan(&busy); err != nil {
		return err
	}
	if busy {
		if _, err = tx.Exec(ctx, `UPDATE automation_tasks SET updated_at=now() WHERE id=$1`, id); err != nil {
			return err
		}
		return tx.Commit(ctx)
	}
	if _, err = tx.Exec(ctx, `UPDATE automation_tasks SET status='queued' WHERE ship_id=$1 AND id<>$2 AND status='running' AND carried=0 AND pending=0`, t.Ship, t.ID); err != nil {
		return err
	}
	if reserved, e := fleet.Reserved(ctx, tx, s.ID); e != nil {
		return e
	} else if reserved {
		return tx.Commit(ctx)
	}
	original := t
	t.Status = "running"
	nested, err := tx.Begin(ctx)
	if err != nil {
		return err
	}
	err = stepAutomation(ctx, nested, &t, &s, g)
	if err != nil {
		if e := nested.Rollback(ctx); e != nil {
			return e
		}
		t = original
		t.Status = "paused"
		t.Error = err.Error()
	} else if err = nested.Commit(ctx); err != nil {
		return err
	}
	if _, err = tx.Exec(ctx, `UPDATE automation_tasks SET progress=$2,carried=$3,pending=$4,phase=$5,status=$6,error=$7,updated_at=now() WHERE id=$1`, t.ID, t.Progress, t.Carried, t.Pending, t.Phase, t.Status, t.Error); err != nil {
		return err
	}
	if t.Status == "completed" || t.Status == "paused" {
		level := "success"
		message := "Задача выполнена"
		if t.Status == "paused" {
			level = "warning"
			message = "Задача приостановлена"
		}
		raw, _ := json.Marshal(map[string]any{"task": t.ID, "ship_name": s.Name, "reason": t.Error})
		if _, err = tx.Exec(ctx, `INSERT INTO notifications(user_id,kind,level,message,data) VALUES($1,'planner',$2,$3,$4)`, t.User, level, message, raw); err != nil {
			return err
		}
	}
	return tx.Commit(ctx)
}
