package fleet

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/jackc/pgx/v5"

	"github.com/yomezza/cosmosimulator/internal/account"
	"github.com/yomezza/cosmosimulator/internal/commands"
	resourcecatalog "github.com/yomezza/cosmosimulator/internal/resources"
	"github.com/yomezza/cosmosimulator/internal/universe"
	"github.com/yomezza/cosmosimulator/internal/wallet"
)

type ShipClass struct {
	ID            string `json:"id"`
	Name          string `json:"name"`
	Role          string `json:"role"`
	Description   string `json:"description"`
	Cargo         int    `json:"cargo"`
	Fuel          int    `json:"fuel"`
	JumpSeconds   int    `json:"jump_seconds"`
	JumpFuel      int    `json:"jump_fuel"`
	MiningBonus   int    `json:"mining_bonus"`
	Mining        int    `json:"mining"`
	Price         int64  `json:"price"`
	Model         string `json:"model"`
	Length        int    `json:"length"`
	RequiredLevel int    `json:"required_level"`
}

var Catalogue = priceShipClasses([]ShipClass{
	{ID: "pilgrim", Name: "Пилигрим", Role: "Универсальный", Description: "Начальный корабль для первых экспедиций, торговли и добычи.", Cargo: 60, Fuel: 40, JumpSeconds: 5, JumpFuel: 2, Model: "pilgrim.svg", Length: 22},
	{ID: "wanderer", Name: "Скиталец", Role: "Разведчик", Description: "Быстрый разведчик с большим баком и компактным трюмом.", Cargo: 35, Fuel: 80, JumpSeconds: 3, JumpFuel: 2, Model: "wanderer.svg", Length: 24},
	{ID: "forge", Name: "Горн", Role: "Добывающий", Description: "Промышленный корпус с усиленной добывающей установкой.", Cargo: 120, Fuel: 60, JumpSeconds: 6, JumpFuel: 3, MiningBonus: 2, Model: "forge.svg", Length: 40},
	{ID: "ark", Name: "Ковчег", Role: "Грузовой", Description: "Вместительный транспорт для крупных партий. Медленнее и расходует больше топлива.", Cargo: 240, Fuel: 80, JumpSeconds: 7, JumpFuel: 4, Model: "ark.svg", Length: 60},
	{ID: "hermes", Name: "Гермес", Role: "Курьер", Description: "Дальнобойный курьер для срочных грузов: большой бак и умеренный трюм.", Cargo: 90, Fuel: 100, JumpSeconds: 4, JumpFuel: 2, Model: "hermes.svg", Length: 32},
	{ID: "vesper", Name: "Веспер", Role: "Разведчик", Description: "Разведчик с раздвоенным носом и парой сенсорных штанг. Больше топлива и места для находок, чем у Скитальца.", Cargo: 45, Fuel: 100, JumpSeconds: 3, JumpFuel: 2, Model: "vesper.svg", Length: 28},
	{ID: "astra", Name: "Астра", Role: "Разведчик", Description: "Дальняя разведка: стреловидный корпус, боковые сканеры и ускоренный гиперпереход.", Cargo: 55, Fuel: 120, JumpSeconds: 2, JumpFuel: 2, Model: "astra.svg", Length: 34},
	{ID: "argus", Name: "Аргус", Role: "Разведчик", Description: "Флагман разведывательной серии с кольцевым сенсорным блоком, большим баком и расширенным трюмом.", Cargo: 70, Fuel: 150, JumpSeconds: 2, JumpFuel: 2, Model: "argus.svg", Length: 42},
	{ID: "crucible", Name: "Тигель", Role: "Добывающий", Description: "Добывающий корабль с парными буровыми захватами и защищёнными рудными отсеками.", Cargo: 150, Fuel: 80, JumpSeconds: 6, JumpFuel: 3, MiningBonus: 3, Model: "crucible.svg", Length: 48},
	{ID: "titan", Name: "Титан", Role: "Добывающий", Description: "Крупный промышленный корабль с четырьмя добывающими узлами и увеличенным рудным трюмом.", Cargo: 190, Fuel: 105, JumpSeconds: 5, JumpFuel: 3, MiningBonus: 5, Model: "titan.svg", Length: 60},
	{ID: "atlas", Name: "Атлас", Role: "Грузовой", Description: "Контейнеровоз с двумя рядами грузовых модулей. Перевозит больше Ковчега при том же расходе на переход.", Cargo: 320, Fuel: 100, JumpSeconds: 7, JumpFuel: 4, Model: "atlas.svg", Length: 72},
	{ID: "leviathan", Name: "Левиафан", Role: "Грузовой", Description: "Тяжёлый транспорт с трёхрядной грузовой палубой и усиленной кормой. Самый вместительный корабль серии.", Cargo: 420, Fuel: 125, JumpSeconds: 6, JumpFuel: 4, Model: "leviathan.svg", Length: 92},
	{ID: "iris", Name: "Ирида", Role: "Курьер", Description: "Курьер с разнесёнными двигателями и защищённым центральным грузовым отсеком. Следующая ступень после Гермеса.", Cargo: 110, Fuel: 120, JumpSeconds: 4, JumpFuel: 2, Model: "iris.svg", Length: 40},
	{ID: "zephyr", Name: "Зефир", Role: "Курьер", Description: "Скоростной курьер с серповидными крыльями. Быстрее Ириды и берёт больше груза и топлива.", Cargo: 135, Fuel: 145, JumpSeconds: 3, JumpFuel: 2, Model: "zephyr.svg", Length: 48},
	{ID: "lightning", Name: "Молния", Role: "Курьер", Description: "Дальний экспресс с тройным маршевым блоком. Двухсекундный гиперпереход и вместительный курьерский отсек.", Cargo: 160, Fuel: 170, JumpSeconds: 2, JumpFuel: 2, Model: "lightning.svg", Length: 58},
})

// Relative to Pilgrim: hold, tank, speed, efficiency and extraction all have a
// price. Round up to 5,000 CR. Yard markups never undercut the 100,000 CR floor.
func priceShipClasses(classes []ShipClass) []ShipClass {
	for i := range classes {
		c := &classes[i]
		c.Mining = resourcecatalog.ExtractorYield("universal", 0, 0) + c.MiningBonus
		c.RequiredLevel = map[string]int{"pilgrim": 6, "wanderer": 6, "forge": 10, "ark": 10, "hermes": 10, "vesper": 10, "astra": 15, "argus": 20, "crucible": 15, "titan": 20, "atlas": 15, "leviathan": 20, "iris": 10, "zephyr": 15, "lightning": 20}[c.ID]
		value := int64(100000 + (c.Cargo-60)*1000 + (c.Fuel-40)*2000 + 600000/c.JumpSeconds - 120000 + 120000/c.JumpFuel - 60000 + c.MiningBonus*90000)
		c.Price = (max(int64(100000), value) + 4999) / 5000 * 5000
	}
	return classes
}

const HullCatalogueVersion = 2

// Keep PostgreSQL's fuel/class invariant in sync with the authoritative Go
// catalogue, including upgrades of databases containing only the original hulls.
func EnsureShipHullConstraint(ctx context.Context, tx pgx.Tx) error {
	marker := fmt.Sprintf("astra-journey-hulls-%d", HullCatalogueVersion)
	// Accept persisted markers from before the rename without rebuilding the constraint.
	legacyMarker := fmt.Sprintf("cosmosimulator-hulls-%d", HullCatalogueVersion)
	var current string
	if err := tx.QueryRow(ctx, `SELECT COALESCE(obj_description(oid,'pg_constraint'),'') FROM pg_constraint WHERE conrelid='ships'::regclass AND conname='ships_class_fuel'`).Scan(&current); err != nil {
		return err
	}
	if current == marker || current == legacyMarker {
		return nil
	}
	var cases strings.Builder
	for _, c := range Catalogue {
		fmt.Fprintf(&cases, " WHEN '%s' THEN %d", strings.ReplaceAll(c.ID, "'", "''"), c.Fuel)
	}
	_, err := tx.Exec(ctx, `ALTER TABLE ships DROP CONSTRAINT ships_class_fuel; ALTER TABLE ships ADD CONSTRAINT ships_class_fuel CHECK(fuel>=0 AND fuel<=CASE class`+cases.String()+` ELSE -1 END); COMMENT ON CONSTRAINT ships_class_fuel ON ships IS '`+marker+`'`)
	return err
}

func Hull(id string) (ShipClass, bool) {
	for _, c := range Catalogue {
		if c.ID == id {
			return c, true
		}
	}
	return Catalogue[0], false
}

func (s Ship) Hull() ShipClass { c, _ := Hull(s.Class); return c }

func (s Ship) Capacity() int { return s.Hull().Cargo }

type MiningStats struct {
	Min     int  `json:"min"`
	Max     int  `json:"max"`
	Current *int `json:"current"`
}

func (s Ship) ExtractionYield(star, planet int) int {
	return resourcecatalog.ExtractorYield(s.Extractor, star, planet) + s.Hull().MiningBonus
}

// Only revealed, compatible deposits can contribute a location-specific figure.
// Cargo space and shared quotas limit the job, not the ship's mining capability.
func (s Ship) MiningStats(sites []universe.Site) MiningStats {
	lo, hi := resourcecatalog.ExtractorRange(s.Extractor)
	bonus := s.Hull().MiningBonus
	stats := MiningStats{Min: lo + bonus, Max: hi + bonus}
	if lo == hi {
		value := stats.Min
		stats.Current = &value
		return stats
	}
	for _, site := range sites {
		if !NearObject(s, site.ID) {
			continue
		}
		for _, resource := range site.Resources {
			if resourcecatalog.ExtractorAccepts(s.Extractor, resource) {
				value := s.ExtractionYield(s.Star, site.Planet)
				stats.Current = &value
				return stats
			}
		}
	}
	return stats
}

func BuyShip(ctx context.Context, tx pgx.Tx, u account.User, s Ship, in commands.Command) error {
	if !Docked(s) {
		return errors.New("Стыкуйтесь с верфью")
	}
	if in.Star > 0 && (in.Star != s.Star || in.Station != s.ObjectID) {
		return errors.New("Прибудьте на выбранную верфь")
	}
	id := in.Target
	if id == "" {
		id = "pilgrim"
	}
	c, ok := Hull(id)
	if !ok {
		return errors.New("Неизвестная модель корабля")
	}
	var level int
	if err := tx.QueryRow(ctx, `SELECT level FROM users WHERE id=$1 FOR UPDATE`, u.ID).Scan(&level); err != nil {
		return err
	}
	var count int
	if err := tx.QueryRow(ctx, `SELECT count(*) FROM ships WHERE user_id=$1`, u.ID).Scan(&count); err != nil {
		return err
	}
	if count >= FleetLimit(level) {
		return errors.New("Достигнут лимит кораблей по стажу")
	}
	if level < c.RequiredLevel {
		return fmt.Errorf("Для покупки нужен стаж %d", c.RequiredLevel)
	}
	var price int64
	var quantity int
	err := tx.QueryRow(ctx, `SELECT y.price,y.quantity FROM shipyard_stock y JOIN stations p ON p.star=y.star AND p.object_id=y.station WHERE y.star=$1 AND y.station=$2 AND y.ship_class=$3 AND p.shipyard AND p.active FOR UPDATE OF y`, s.Star, s.ObjectID, id).Scan(&price, &quantity)
	if errors.Is(err, pgx.ErrNoRows) || err == nil && quantity < 1 {
		return errors.New("На верфи нет доступных кораблей")
	}
	if err != nil {
		return err
	}
	if in.Price != 0 && in.Price != price {
		return errors.New("Цена изменилась. Проверьте предложение ещё раз")
	}
	if err = wallet.Debit(ctx, tx, u.ID, price); err != nil {
		return err
	}
	if _, err = tx.Exec(ctx, `UPDATE shipyard_stock SET quantity=quantity-1 WHERE star=$1 AND station=$2 AND ship_class=$3`, s.Star, s.ObjectID, id); err != nil {
		return err
	}
	if id == "pilgrim" {
		if _, err = tx.Exec(ctx, `UPDATE stations SET ship_stock=GREATEST(0,ship_stock-1) WHERE star=$1 AND object_id=$2`, s.Star, s.ObjectID); err != nil {
			return err
		}
	}
	_, err = tx.Exec(ctx, `INSERT INTO ships(user_id,star,object_id,active,class,name,fuel) VALUES($1,$2,$3,FALSE,$4,$5,$6)`, u.ID, s.Star, s.ObjectID, c.ID, c.Name, c.Fuel)
	return err
}

func RenameShip(ctx context.Context, tx pgx.Tx, s Ship, name string) error {
	name = strings.TrimSpace(name)
	if !utf8.ValidString(name) || utf8.RuneCountInString(name) < 1 || utf8.RuneCountInString(name) > 40 || strings.ContainsFunc(name, unicode.IsControl) {
		return errors.New("Название корабля: от 1 до 40 символов")
	}
	_, err := tx.Exec(ctx, `UPDATE ships SET name=$2 WHERE id=$1`, s.ID, name)
	return err
}

// Above 35 retain the last specified allowance until further tiers are defined.
func FleetLimit(level int) int {
	switch {
	case level <= 5:
		return 2
	case level <= 10:
		return 3
	case level <= 16:
		return 4
	case level <= 25:
		return 6
	default:
		return 7
	}
}
