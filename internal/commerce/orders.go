package commerce

import (
	"context"
	"errors"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/yomezza/cosmosimulator/internal/account"
	"github.com/yomezza/cosmosimulator/internal/commands"
	"github.com/yomezza/cosmosimulator/internal/events"
	"github.com/yomezza/cosmosimulator/internal/fleet"
	"github.com/yomezza/cosmosimulator/internal/inventory"
	resourcecatalog "github.com/yomezza/cosmosimulator/internal/resources"
	"github.com/yomezza/cosmosimulator/internal/universe"
	"github.com/yomezza/cosmosimulator/internal/wallet"
)

func (a *Service) EconomyCommand(ctx context.Context, tx pgx.Tx, u account.User, s *fleet.Ship, g *universe.Galaxy, in commands.Command) (bool, error) {
	if in.Quantity < 0 || in.Quantity > 100000 || in.Price < 0 || in.Price > 1000000 || in.Amount < 0 || in.Amount > wallet.MaxCredits {
		return true, errors.New("Некорректное количество или цена")
	}
	switch in.Action {
	case "store", "withdraw":
		if !fleet.Docked(*s) {
			return true, errors.New("Стыкуйтесь со станцией")
		}
		if _, ok := resourcecatalog.Lookup(in.Resource); !ok || in.Quantity < 1 {
			return true, errors.New("Некорректный ресурс")
		}
		m, err := inventory.Warehouse(ctx, tx, u.ID, s.Star, s.ObjectID)
		if err != nil {
			return true, err
		}
		if in.Action == "store" {
			if s.Cargo[in.Resource] < in.Quantity {
				return true, errors.New("Недостаточно груза")
			}
			s.Cargo[in.Resource] -= in.Quantity
			m[in.Resource] += in.Quantity
		} else {
			var reserved int
			if err = tx.QueryRow(ctx, `SELECT COALESCE(sum(quantity),0) FROM mining_jobs WHERE ship_id=$1`, s.ID).Scan(&reserved); err != nil {
				return true, err
			}
			if m[in.Resource] < in.Quantity || s.CargoUsed()+reserved+in.Quantity > s.Capacity() {
				return true, errors.New("Недостаточно товара или места в трюме")
			}
			m[in.Resource] -= in.Quantity
			s.Cargo[in.Resource] += in.Quantity
		}
		if err = inventory.PutWarehouse(ctx, tx, u.ID, s.Star, s.ObjectID, m); err != nil {
			return true, err
		}
		return true, fleet.SaveShip(ctx, tx, u.ID, *s)
	case "data_sell":
		if !fleet.Docked(*s) || in.Price < 1 || in.Price > 1000000 {
			return true, errors.New("Стыкуйтесь со станцией и укажите цену")
		}
		ids := []int{}
		rows, err := tx.Query(ctx, `SELECT star FROM surveys WHERE user_id=$1 AND source<>'purchased' ORDER BY star`, u.ID)
		if err != nil {
			return true, err
		}
		for rows.Next() {
			var id int
			if err = rows.Scan(&id); err != nil {
				rows.Close()
				return true, err
			}
			if id >= 1 && id <= len(g.Stars) && !g.Stars[id-1].Deleted && g.Stars[id-1].Region == in.Region {
				ids = append(ids, id)
			}
		}
		err = rows.Err()
		rows.Close()
		if err != nil {
			return true, err
		}
		if len(ids) == 0 {
			return true, errors.New("Нет исследованных систем в регионе")
		}
		_, err = tx.Exec(ctx, `INSERT INTO survey_offers(seller,star,station,region,systems,price) VALUES($1,$2,$3,$4,$5,$6)`, u.ID, s.Star, s.ObjectID, in.Region, ids, in.Price)
		return true, err
	case "data_remove":
		tag, err := tx.Exec(ctx, `DELETE FROM survey_offers WHERE id=$1 AND seller=$2`, in.ID, u.ID)
		if err == nil && tag.RowsAffected() == 0 {
			return true, errors.New("Предложение не найдено")
		}
		return true, err
	case "data_buy":
		var seller, price int64
		var star int
		var station string
		var systems []int
		if err := tx.QueryRow(ctx, `SELECT seller,price,star,station,systems FROM survey_offers WHERE id=$1 FOR SHARE`, in.ID).Scan(&seller, &price, &star, &station, &systems); err != nil {
			return true, err
		}
		if seller == u.ID {
			return true, errors.New("Это ваши данные")
		}
		if !fleet.Docked(*s) || s.Star != star || s.ObjectID != station {
			return true, errors.New("Прибудьте на станцию продажи данных")
		}
		var fresh int
		if err := tx.QueryRow(ctx, `SELECT count(*) FROM unnest($2::int[]) v WHERE NOT EXISTS(SELECT 1 FROM surveys WHERE user_id=$1 AND star=v)`, u.ID, systems).Scan(&fresh); err != nil {
			return true, err
		}
		if fresh == 0 {
			return true, errors.New("Все сведения уже известны")
		}
		if err := wallet.Debit(ctx, tx, u.ID, price); err != nil {
			return true, err
		}
		if err := wallet.Credit(ctx, tx, seller, price); err != nil {
			return true, err
		}
		_, err := tx.Exec(ctx, `INSERT INTO surveys(user_id,star,source) SELECT $1,unnest($2::int[]),'purchased' ON CONFLICT DO NOTHING`, u.ID, systems)
		return true, err
	case "order_sell":
		if !fleet.Docked(*s) || in.Price < 1 || in.Quantity < 1 {
			return true, errors.New("Стыкуйтесь со станцией и укажите количество и цену")
		}
		if _, ok := resourcecatalog.Lookup(in.Resource); !ok {
			return true, errors.New("Неизвестный ресурс")
		}
		m, err := inventory.Warehouse(ctx, tx, u.ID, s.Star, s.ObjectID)
		if err != nil {
			return true, err
		}
		if m[in.Resource] < in.Quantity {
			return true, errors.New("Сначала поместите товар на склад")
		}
		m[in.Resource] -= in.Quantity
		if err = inventory.PutWarehouse(ctx, tx, u.ID, s.Star, s.ObjectID, m); err != nil {
			return true, err
		}
		_, err = tx.Exec(ctx, `INSERT INTO sell_orders(seller,star,station,resource,quantity,price) VALUES($1,$2,$3,$4,$5,$6)`, u.ID, s.Star, s.ObjectID, in.Resource, in.Quantity, in.Price)
		return true, err
	case "order_buy", "order_cancel":
		var seller, price int64
		var star, q int
		var station, res string
		if err := tx.QueryRow(ctx, `SELECT seller,star,station,resource,quantity,price FROM sell_orders WHERE id=$1 FOR UPDATE`, in.ID).Scan(&seller, &star, &station, &res, &q, &price); err != nil {
			return true, err
		}
		if in.Action == "order_cancel" {
			if seller != u.ID {
				return true, errors.New("Нет полномочий")
			}
			m, err := inventory.Warehouse(ctx, tx, u.ID, star, station)
			if err != nil {
				return true, err
			}
			m[res] += q
			if err = inventory.PutWarehouse(ctx, tx, u.ID, star, station, m); err != nil {
				return true, err
			}
			_, err = tx.Exec(ctx, `UPDATE sell_orders SET quantity=0 WHERE id=$1`, in.ID)
			return true, err
		}
		if seller == u.ID || in.Quantity < 1 || in.Quantity > q {
			return true, errors.New("Недопустимая покупка")
		}
		cost := price * int64(in.Quantity)
		if err := wallet.Debit(ctx, tx, u.ID, cost); err != nil {
			return true, err
		}
		if err := wallet.Credit(ctx, tx, seller, cost); err != nil {
			return true, err
		}
		m, err := inventory.Warehouse(ctx, tx, u.ID, star, station)
		if err != nil {
			return true, err
		}
		m[res] += in.Quantity
		if err = inventory.PutWarehouse(ctx, tx, u.ID, star, station, m); err != nil {
			return true, err
		}
		_, err = tx.Exec(ctx, `UPDATE sell_orders SET quantity=quantity-$2 WHERE id=$1`, in.ID, in.Quantity)
		return true, err
	case "delivery_create":
		if !fleet.Docked(*s) || in.Quantity < 1 || in.Quantity > s.Capacity() || in.Amount < 1 || in.Hours < 1 || in.Hours > 168 || in.To < 1 || in.To > len(g.Stars) {
			return true, errors.New("Некорректный заказ доставки")
		}
		res, ok := resourcecatalog.Lookup(in.Resource)
		if !ok || in.Collateral < int64(in.Quantity*res.Price) || in.Collateral > 100000000 {
			return true, errors.New("Обеспечение должно покрывать стоимость груза")
		}
		if in.Amount > wallet.MaxCredits-in.Collateral {
			return true, errors.New("Сумма превышает технический диапазон")
		}
		var dest bool
		if err := tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM stations WHERE star=$1 AND object_id=$2 AND active)`, in.To, in.Station).Scan(&dest); err != nil {
			return true, err
		}
		if !dest || len(g.Path(s.Star, in.To)) == 0 || s.Star == in.To && s.ObjectID == in.Station {
			return true, errors.New("Нет маршрута доставки")
		}
		m, err := inventory.Warehouse(ctx, tx, u.ID, s.Star, s.ObjectID)
		if err != nil {
			return true, err
		}
		if m[in.Resource] < in.Quantity {
			return true, errors.New("Недостаточно товара на складе")
		}
		if err = wallet.Debit(ctx, tx, u.ID, in.Amount); err != nil {
			return true, err
		}
		m[in.Resource] -= in.Quantity
		if err = inventory.PutWarehouse(ctx, tx, u.ID, s.Star, s.ObjectID, m); err != nil {
			return true, err
		}
		_, err = tx.Exec(ctx, `INSERT INTO deliveries(owner,from_star,from_station,to_star,to_station,resource,quantity,reward,collateral,deadline) VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9,now()+make_interval(hours=>$10))`, u.ID, s.Star, s.ObjectID, in.To, in.Station, in.Resource, in.Quantity, in.Amount, in.Collateral, in.Hours)
		return true, err
	case "delivery_accept", "delivery_bot", "delivery_finish", "delivery_cancel":
		return true, a.deliveryCommand(ctx, tx, u, s, g, in)
	}
	return false, nil
}

func (a *Service) deliveryCommand(ctx context.Context, tx pgx.Tx, u account.User, s *fleet.Ship, g *universe.Galaxy, in commands.Command) error {
	var owner, reward, collateral int64
	var carrier *int64
	var sid *int64
	var from, to, q int
	var fs, ts, res, status string
	var deadline time.Time
	err := tx.QueryRow(ctx, `SELECT owner,carrier,ship_id,from_star,from_station,to_star,to_station,resource,quantity,reward,collateral,status,deadline FROM deliveries WHERE id=$1 FOR UPDATE`, in.ID).Scan(&owner, &carrier, &sid, &from, &fs, &to, &ts, &res, &q, &reward, &collateral, &status, &deadline)
	if err != nil {
		return err
	}
	switch in.Action {
	case "delivery_accept":
		if status != "open" || owner == u.ID || !time.Now().Before(deadline) {
			return errors.New("Заказ недоступен")
		}
		if !fleet.Docked(*s) || s.Star != from || s.ObjectID != fs || s.CargoUsed()+q > s.Capacity() {
			return errors.New("Прибудьте к отправителю и освободите трюм")
		}
		var reserved int
		if err = tx.QueryRow(ctx, `SELECT COALESCE(sum(quantity),0) FROM mining_jobs WHERE ship_id=$1`, s.ID).Scan(&reserved); err != nil {
			return err
		}
		if s.CargoUsed()+reserved+q > s.Capacity() {
			return errors.New("Трюм заполнен")
		}
		if err = wallet.Debit(ctx, tx, u.ID, collateral); err != nil {
			return err
		}
		s.Cargo[res] += q
		if err = fleet.SaveShip(ctx, tx, u.ID, *s); err != nil {
			return err
		}
		_, err = tx.Exec(ctx, `UPDATE deliveries SET status='carrying',carrier=$2,ship_id=$3 WHERE id=$1`, in.ID, u.ID, s.ID)
	case "delivery_bot":
		if owner != u.ID || status != "open" {
			return errors.New("Заказ недоступен")
		}
		path := g.Path(from, to)
		if len(path) == 0 {
			return errors.New("Нет маршрута")
		}
		seconds := 30 + 5*len(path)
		if time.Now().Add(time.Duration(seconds) * time.Second).After(deadline) {
			return errors.New("Перевозчик не успеет в срок")
		}
		_, err = tx.Exec(ctx, `UPDATE deliveries SET status='bot',carrier=(SELECT id FROM users WHERE is_bot AND virtual_run IS NULL ORDER BY id LIMIT 1),arrival=now()+make_interval(secs=>$2) WHERE id=$1`, in.ID, seconds)
	case "delivery_finish":
		if status != "carrying" || carrier == nil || *carrier != u.ID || sid == nil || *sid != s.ID || !fleet.Docked(*s) || s.Star != to || s.ObjectID != ts || s.Cargo[res] < q || time.Now().After(deadline) {
			return errors.New("Доставка пока не выполнена")
		}
		s.Cargo[res] -= q
		if err = fleet.SaveShip(ctx, tx, u.ID, *s); err != nil {
			return err
		}
		if err = completeDelivery(ctx, tx, in.ID, owner, u.ID, to, ts, res, q, reward+collateral); err != nil {
			return err
		}
	case "delivery_cancel":
		if owner != u.ID || status != "open" {
			return errors.New("Нельзя отменить принятый заказ")
		}
		m, e := inventory.Warehouse(ctx, tx, owner, from, fs)
		if e != nil {
			return e
		}
		m[res] += q
		if err = inventory.PutWarehouse(ctx, tx, owner, from, fs, m); err != nil {
			return err
		}
		if err = wallet.Credit(ctx, tx, owner, reward); err != nil {
			return err
		}
		_, err = tx.Exec(ctx, `UPDATE deliveries SET status='cancelled' WHERE id=$1`, in.ID)
	}
	return err
}

func completeDelivery(ctx context.Context, tx pgx.Tx, id, owner, carrier int64, to int, station, res string, q int, pay int64) error {
	m, err := inventory.Warehouse(ctx, tx, owner, to, station)
	if err != nil {
		return err
	}
	m[res] += q
	if err = inventory.PutWarehouse(ctx, tx, owner, to, station, m); err != nil {
		return err
	}
	if err = wallet.Credit(ctx, tx, carrier, pay); err != nil {
		return err
	}
	if _, err = tx.Exec(ctx, `UPDATE deliveries SET status='delivered' WHERE id=$1`, id); err != nil {
		return err
	}
	if _, err = tx.Exec(ctx, `INSERT INTO reputation(user_id,company_id,points) SELECT $1,founder,1 FROM stations WHERE star=$2 AND object_id=$3 AND founder IS NOT NULL ON CONFLICT(user_id,company_id) DO UPDATE SET points=reputation.points+1`, carrier, to, station); err != nil {
		return err
	}
	for _, uid := range []int64{owner, carrier} {
		if err = events.RecordEvent(ctx, tx, uid, "delivery_complete", map[string]any{"delivery": id, "star": to, "resource": res, "quantity": q}); err != nil {
			return err
		}
	}
	return nil
}

func (a *Service) AdvanceLogistics(ctx context.Context) error {
	tx, err := a.DB.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(context.Background())
	if _, err = tx.Exec(ctx, `SELECT pg_advisory_xact_lock(784218)`); err != nil {
		return err
	}
	if err = wallet.Reason(ctx, tx, "Доставка / истечение бронирования"); err != nil {
		return err
	}
	rows, err := tx.Query(ctx, `SELECT id,owner,COALESCE(carrier,0),to_star,to_station,resource,quantity,reward FROM deliveries WHERE status='bot' AND arrival<=now() ORDER BY id FOR UPDATE`)
	if err != nil {
		return err
	}
	type done struct {
		id, owner, carrier int64
		star, q            int
		station, res       string
		reward             int64
	}
	list := []done{}
	for rows.Next() {
		var d done
		if err = rows.Scan(&d.id, &d.owner, &d.carrier, &d.star, &d.station, &d.res, &d.q, &d.reward); err != nil {
			rows.Close()
			return err
		}
		list = append(list, d)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return err
	}
	for _, d := range list {
		if ok, e := wallet.Room(ctx, tx, d.carrier, d.reward); e != nil {
			return e
		} else if !ok {
			continue
		}
		if err = completeDelivery(ctx, tx, d.id, d.owner, d.carrier, d.star, d.station, d.res, d.q, d.reward); err != nil {
			return err
		}
	}
	rows, err = tx.Query(ctx, `SELECT id,owner,COALESCE(carrier,0),from_star,from_station,resource,quantity,reward,collateral,status FROM deliveries WHERE status IN('open','carrying') AND deadline<=now() ORDER BY id FOR UPDATE`)
	if err != nil {
		return err
	}
	type expired struct {
		done
		collateral int64
		status     string
	}
	expiredList := []expired{}
	for rows.Next() {
		var d expired
		if err = rows.Scan(&d.id, &d.owner, &d.carrier, &d.star, &d.station, &d.res, &d.q, &d.reward, &d.collateral, &d.status); err != nil {
			rows.Close()
			return err
		}
		expiredList = append(expiredList, d)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return err
	}
	for _, d := range expiredList {
		refund := d.reward
		if d.status != "open" {
			refund += d.collateral
		}
		if ok, e := wallet.Room(ctx, tx, d.owner, refund); e != nil {
			return e
		} else if !ok {
			continue
		}
		if d.status == "open" {
			m, e := inventory.Warehouse(ctx, tx, d.owner, d.star, d.station)
			if e != nil {
				return e
			}
			m[d.res] += d.q
			if err = inventory.PutWarehouse(ctx, tx, d.owner, d.star, d.station, m); err != nil {
				return err
			}
		}
		if err = wallet.Credit(ctx, tx, d.owner, refund); err != nil {
			return err
		}
		if _, err = tx.Exec(ctx, `UPDATE deliveries SET status='expired' WHERE id=$1`, d.id); err != nil {
			return err
		}
	}
	// Reservations have full refunds and no storage fee until their visible expiry.
	rows, err = tx.Query(ctx, `SELECT id,user_id,star,station,resource,quantity,cost FROM reservations WHERE status='reserved' AND expires_at<=now() ORDER BY id FOR UPDATE`)
	if err != nil {
		return err
	}
	list = nil
	for rows.Next() {
		var d done
		if err = rows.Scan(&d.id, &d.owner, &d.star, &d.station, &d.res, &d.q, &d.reward); err != nil {
			rows.Close()
			return err
		}
		list = append(list, d)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return err
	}
	for _, d := range list {
		if ok, e := wallet.Room(ctx, tx, d.owner, d.reward); e != nil {
			return e
		} else if !ok {
			continue
		}
		if _, err = tx.Exec(ctx, `UPDATE local_markets SET stock=jsonb_set(stock,ARRAY[$2::text],to_jsonb(COALESCE((stock->>$2)::int,0)+$3)) WHERE star=$1 AND station=$4`, d.star, d.res, d.q, d.station); err != nil {
			return err
		}
		if err = wallet.Credit(ctx, tx, d.owner, d.reward); err != nil {
			return err
		}
		if _, err = tx.Exec(ctx, `UPDATE reservations SET status='cancelled' WHERE id=$1`, d.id); err != nil {
			return err
		}
	}
	return tx.Commit(ctx)
}
