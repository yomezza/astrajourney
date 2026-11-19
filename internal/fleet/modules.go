package fleet

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/yomezza/cosmosimulator/internal/commands"
	"github.com/yomezza/cosmosimulator/internal/wallet"
)

type ModuleEffects struct {
	Extractor string `json:"extractor,omitempty"`
	Cargo     int    `json:"cargo_bonus,omitempty"`
	Yield     int    `json:"yield_bonus,omitempty"`
	Scan      int    `json:"scan_percent,omitempty"`
	Jump      int    `json:"jump_percent,omitempty"`
	Scoop     int    `json:"scoop_rate,omitempty"`
}
type ModuleDefinition struct {
	CatalogID   string        `json:"catalog_id"`
	Name        string        `json:"name"`
	Kind        string        `json:"kind"`
	Rating      string        `json:"rating"`
	Size        int           `json:"size"`
	Price       int64         `json:"price"`
	Description string        `json:"description"`
	Effects     ModuleEffects `json:"effects"`
}
type ShipModule struct {
	ModuleDefinition
	ID        int64 `json:"id"`
	Installed bool  `json:"installed"`
	Paid      int64 `json:"paid"`
}

func (d ModuleDefinition) Validate() error {
	if d.Size < 1 || d.Size > 64 || d.Price < 1 || d.Price > 1000000000 || d.CatalogID == "" || d.Name == "" {
		return errors.New("Некорректное определение модуля")
	}
	switch d.Rating {
	case "S", "A", "B", "C":
	default:
		return errors.New("Неизвестный класс модуля")
	}
	switch d.Kind {
	case "cargo":
		if d.Rating != "C" {
			return errors.New("Грузовые расширители выпускаются только в классе C")
		}
	case "scoop":
		if d.Rating == "B" {
			return errors.New("Неизвестный класс топливозаборника")
		}
	case "extractor", "scanner", "drive":
	default:
		return errors.New("Неизвестный тип модуля")
	}
	e := d.Effects
	if e.Cargo < 0 || e.Cargo > 10000 || e.Yield < 0 || e.Yield > 100 || e.Scan < 0 || e.Scan > 85 || e.Jump < 0 || e.Jump > 70 || e.Scoop < 0 || e.Scoop > 100 {
		return errors.New("Характеристики модуля вне допустимых границ")
	}
	if d.Kind != "cargo" && e.Cargo != 0 || d.Kind != "extractor" && (e.Yield != 0 || e.Extractor != "") || d.Kind != "scanner" && e.Scan != 0 || d.Kind != "drive" && e.Jump != 0 || d.Kind != "scoop" && e.Scoop != 0 {
		return errors.New("Характеристики не соответствуют типу модуля")
	}
	if d.Kind == "cargo" && e.Cargo == 0 || d.Kind == "scanner" && e.Scan == 0 || d.Kind == "scoop" && e.Scoop == 0 || d.Kind == "drive" && e.Jump == 0 {
		return errors.New("Модуль должен иметь рабочую характеристику")
	}
	if d.Kind == "extractor" {
		switch e.Extractor {
		case "universal", "ore", "ice", "gas":
		default:
			return errors.New("Неизвестный добытчик")
		}
	}
	return nil
}
func (s Ship) ModuleUsed() int {
	n := 0
	for _, m := range s.Modules {
		if m.Installed {
			n += m.Size
		}
	}
	return n
}
func (s Ship) ModuleCargo() int {
	n := 0
	for _, m := range s.Modules {
		if !m.Installed {
			n += m.Size
		}
	}
	return n
}
func (s Ship) CargoUsed() int { return CargoSize(s.Cargo) + s.ModuleCargo() }
func (s Ship) ModuleEffect(kind string) ModuleEffects {
	for _, m := range s.Modules {
		if m.Installed && m.Kind == kind {
			return m.Effects
		}
	}
	return ModuleEffects{}
}
func (s Ship) ScanSeconds(base int) int {
	return max(1, (base*(100-s.ModuleEffect("scanner").Scan)+99)/100)
}

func EnsureModuleDefaults(ctx context.Context, tx pgx.Tx) error {
	for _, c := range Catalogue {
		grade := "C"
		if c.Role == "Добывающий" {
			grade = "B"
		}
		if _, err := tx.Exec(ctx, `INSERT INTO ship_module_hulls(class,capacity,mining_rating) VALUES($1,$2,$3) ON CONFLICT DO NOTHING`, c.ID, min(80, 8+int(c.Price/50000)), grade); err != nil {
			return err
		}
	}
	_, err := tx.Exec(ctx, `INSERT INTO ship_modules(ship_id,catalog_id,definition,installed)
 SELECT s.id,c.id,(to_jsonb(c)-'enabled'-'id')||jsonb_build_object('catalog_id',c.id),TRUE
 FROM ships s JOIN ship_module_hulls h ON h.class=s.class JOIN ship_module_catalog c
 ON c.id='cargo-1-C' OR c.id='extractor-'||s.extractor||'-'||h.mining_rating WHERE NOT s.modules_initialized;
 UPDATE ships SET modules_initialized=TRUE WHERE NOT modules_initialized`)
	return err
}

// Caller owns the world and user locks; lock the exact ship before any equipment write.
func ModuleCommand(ctx context.Context, tx pgx.Tx, uid int64, s *Ship, in commands.Command) (bool, error) {
	switch in.Action {
	case "module_buy", "module_install", "module_remove", "module_sell", "module_scoop":
	default:
		return false, nil
	}
	var owned int64
	if err := tx.QueryRow(ctx, `SELECT id FROM ships WHERE id=$1 AND user_id=$2 FOR UPDATE`, s.ID, uid).Scan(&owned); err != nil {
		return true, err
	}
	fresh, err := ReadShip(ctx, tx, s.ID)
	if err != nil {
		return true, err
	}
	*s = fresh
	if err = Idle(ctx, tx, *s); err != nil {
		return true, err
	}
	if len(s.Waypoints) > 0 || s.ReservedFuel > 0 {
		return true, errors.New("Сначала отмените маршрут")
	}
	if in.Action == "module_scoop" {
		rate := s.ModuleEffect("scoop").Scoop
		if rate <= 0 {
			return true, errors.New("Установите топливозаборник")
		}
		if Docked(*s) {
			return true, errors.New("Отстыкуйтесь: топливозаборник работает в открытом космосе")
		}
		if s.Fuel >= s.Hull().Fuel {
			return true, errors.New("Топливный бак заполнен")
		}
		tag, e := tx.Exec(ctx, `UPDATE ships SET fuel=LEAST($2,fuel+$3),scoop_ready_at=clock_timestamp()+interval '10 seconds' WHERE id=$1 AND scoop_ready_at<=clock_timestamp()`, s.ID, s.Hull().Fuel, rate)
		if e == nil && tag.RowsAffected() == 0 {
			e = errors.New("Топливозаборник остывает: цикл раз в 10 секунд")
		}
		return true, e
	}
	if !Docked(*s) {
		return true, errors.New("Для оснащения и торговли модулями стыкуйтесь со станцией")
	}
	var active bool
	if err = tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM stations WHERE star=$1 AND object_id=$2 AND active)`, s.Star, s.ObjectID).Scan(&active); err != nil {
		return true, err
	}
	if !active {
		return true, errors.New("Станция недоступна")
	}
	if in.Action == "module_buy" {
		if in.Star != s.Star || in.Station != s.ObjectID {
			return true, errors.New("Прибудьте на станцию продавца")
		}
		if err = EnsureModuleStock(ctx, tx, s.Star, s.ObjectID); err != nil {
			return true, err
		}
		var raw []byte
		var price int64
		var quantity int
		err = tx.QueryRow(ctx, `SELECT (to_jsonb(c)-'id')||jsonb_build_object('catalog_id',c.id),o.price,o.quantity FROM station_module_stock o JOIN ship_module_catalog c ON c.id=o.catalog_id WHERE o.star=$1 AND o.station=$2 AND o.catalog_id=$3 AND o.enabled AND c.enabled FOR UPDATE OF o`, s.Star, s.ObjectID, in.Target).Scan(&raw, &price, &quantity)
		if errors.Is(err, pgx.ErrNoRows) {
			return true, errors.New("На станции нет такого модуля")
		}
		if err != nil {
			return true, err
		}
		if quantity < 1 {
			return true, errors.New("Модули закончились")
		}
		if in.Price != price {
			return true, errors.New("Цена изменилась. Обновите рынок")
		}
		var d ModuleDefinition
		if err = json.Unmarshal(raw, &d); err != nil {
			return true, err
		}
		if err = d.Validate(); err != nil {
			return true, err
		}
		if s.CargoUsed()+d.Size > s.Capacity() {
			return true, errors.New("В трюме недостаточно места для модуля")
		}
		if err = wallet.Debit(ctx, tx, uid, price); err != nil {
			return true, err
		}
		d.Price = price
		raw, err = json.Marshal(d)
		if err != nil {
			return true, err
		}
		if _, err = tx.Exec(ctx, `UPDATE station_module_stock SET quantity=quantity-1 WHERE star=$1 AND station=$2 AND catalog_id=$3`, s.Star, s.ObjectID, in.Target); err != nil {
			return true, err
		}
		_, err = tx.Exec(ctx, `INSERT INTO ship_modules(ship_id,catalog_id,definition,purchased_price) VALUES($1,$2,$3,$4)`, s.ID, d.CatalogID, raw, price)
		return true, err
	}
	index := -1
	for i, m := range s.Modules {
		if m.ID == in.ID {
			index = i
			break
		}
	}
	if index < 0 {
		return true, errors.New("Модуль не найден в этом корабле")
	}
	m := s.Modules[index]
	switch in.Action {
	case "module_install":
		if m.Installed {
			return true, errors.New("Модуль уже установлен")
		}
		if err = m.ModuleDefinition.Validate(); err != nil {
			return true, err
		}
		if s.ModuleUsed()+m.Size > s.ModuleLimit {
			return true, errors.New("Превышен размер оснащения корабля")
		}
		for _, other := range s.Modules {
			if other.Installed && other.Kind == m.Kind && m.Kind != "cargo" && (m.Kind != "extractor" || other.Effects.Extractor == m.Effects.Extractor) {
				return true, errors.New("Сначала снимите установленный модуль этого типа")
			}
		}
		s.Modules[index].Installed = true
	case "module_remove":
		if !m.Installed {
			return true, errors.New("Модуль уже находится в трюме")
		}
		s.Modules[index].Installed = false
	case "module_sell":
		if m.Installed {
			return true, errors.New("Сначала снимите модуль")
		}
		var paid int64
		if err = tx.QueryRow(ctx, `DELETE FROM ship_modules WHERE id=$1 AND ship_id=$2 AND NOT installed RETURNING purchased_price`, in.ID, s.ID).Scan(&paid); err != nil {
			return true, err
		}
		return true, wallet.Credit(ctx, tx, uid, paid/2)
	}
	if s.CargoUsed() > s.Capacity() {
		return true, errors.New("Снятие модуля переполнит трюм: освободите место")
	}
	_, err = tx.Exec(ctx, `UPDATE ship_modules SET installed=$3 WHERE id=$1 AND ship_id=$2`, m.ID, s.ID, s.Modules[index].Installed)
	return true, err
}

func EnsureModuleStock(ctx context.Context, q interface {
	Exec(context.Context, string, ...any) (pgconn.CommandTag, error)
}, star int, station string) error {
	_, err := q.Exec(ctx, `INSERT INTO station_module_stock(star,station,catalog_id,quantity,price)
 SELECT s.star,s.object_id,c.id,CASE c.rating WHEN 'C' THEN 8 WHEN 'B' THEN 4 WHEN 'A' THEN 2 ELSE 1 END,c.price
 FROM stations s CROSS JOIN ship_module_catalog c WHERE s.star=$1 AND s.object_id=$2 AND s.active AND c.enabled AND
 (c.rating='C' OR (hashtextextended(s.star::text||'/'||s.object_id||'/'||c.id,0)&2147483647)%CASE c.rating WHEN 'B' THEN 2 WHEN 'A' THEN 3 ELSE 7 END=0)
 ON CONFLICT DO NOTHING`, star, station)
	return err
}

func (m ShipModule) Code() string { return fmt.Sprintf("%d%s", m.Size, m.Rating) }
