// Package building owns placed sector objects. Saved geography remains immutable.
package building

import (
	"errors"
	"math"

	"github.com/yomezza/cosmosimulator/internal/planet/pathing"
	"github.com/yomezza/cosmosimulator/internal/planet/scene"
)

const GridSize = 4
const Limit = 256
const Reach = 32

var ErrPlacement = errors.New("build_invalid_place")
var ErrDenied = errors.New("build_access_denied")
var ErrLimit = errors.New("build_limit")

type Definition struct {
	ID     string `json:"id"`
	Title  string `json:"title"`
	Group  string `json:"group"`
	Asset  string `json:"asset"`
	Solid  bool   `json:"solid"`
	Ground bool   `json:"ground"`
}

// Only packaged originals are accepted. Asset paths and collision never come from clients.
var Catalog = []Definition{
	{"foundation", "Фундамент", "Основания и дорожки", "construction/farm/foundation.png", false, true},
	{"floor", "Металлический пол", "Основания и дорожки", "construction/floors/floor_tile.png", false, true},
	{"deck", "Технический настил", "Основания и дорожки", "construction/foundations/support_frame.png", false, true},
	{"path", "Каменная дорожка", "Основания и дорожки", "construction/farm/stone_path.png", false, true},
	{"dirt_path", "Грунтовая дорожка", "Основания и дорожки", "construction/floors/technical_deck.png", false, true},
	{"wall", "Стена", "Стены и ограды", "construction/walls/wall_corner.png", true, false},
	{"corner", "Угловая стена", "Стены и ограды", "construction/walls/door.png", true, false},
	{"door", "Дверной модуль", "Стены и ограды", "construction/walls/window.png", true, false},
	{"window", "Оконный модуль", "Стены и ограды", "construction/paths/farm_path.png", true, false},
	{"fence", "Деревянная ограда", "Стены и ограды", "construction/farm/fence_straight.png", true, false},
	{"fence_corner", "Угол ограды", "Стены и ограды", "construction/farm/fence_corner.png", true, false},
	{"fence_gate", "Сетчатая ограда", "Стены и ограды", "construction/fences/fence_gate.png", true, false},
	{"gate_open", "Открытая калитка", "Стены и ограды", "construction/farm/gate_open.png", false, false},
	{"post", "Столб ограды", "Стены и ограды", "construction/farm/fence_post.png", true, false},
	{"bed", "Пустая грядка", "Ферма", "construction/farm/bed_empty.png", false, false},
	{"seedlings", "Грядка с рассадой", "Ферма", "construction/farm/bed_seedlings.png", false, false},
	{"tomato", "Томаты", "Ферма", "construction/farm/tomato_mature.png", false, false},
	{"corn", "Кукуруза", "Ферма", "construction/farm/corn_mature.png", false, false},
	{"soil", "Обработанная почва", "Ферма", "construction/farm/soil_bed.png", false, true},
	{"sprout", "Ростки", "Ферма", "construction/farm/crop_a_sprout.png", false, false},
	{"crop", "Подросшие растения", "Ферма", "construction/farm/crop_b_growth.png", false, false},
	{"tank", "Бак с водой", "Оборудование", "construction/farm/water_tank.png", true, false},
	{"solar", "Солнечная панель", "Оборудование", "construction/farm/solar_unit.png", true, false},
	{"workbench", "Верстак", "Оборудование", "construction/farm/workstation.png", true, false},
	{"terminal", "Терминал фермы", "Оборудование", "construction/farm/farm_terminal.png", true, false},
	{"container", "Контейнер", "Оборудование", "construction/farm/container.png", true, false},
	{"tools", "Ящик с инструментами", "Оборудование", "construction/farm/tool_crate.png", true, false},
	{"pipe", "Труба", "Оборудование", "construction/floors/pipe.png", false, true},
	{"cable", "Оросительная труба", "Оборудование", "construction/farm/irrigation_junction.png", false, true},
	{"sign", "Указатель", "Декор", "construction/fences/signpost.png", true, false},
	{"basket", "Корзина урожая", "Декор", "construction/farm/harvest_basket.png", false, false},
	{"log", "Бревно", "Декор", "nature/decorations/log.png", true, false},
	{"stump", "Пень", "Декор", "nature/decorations/stump.png", true, false},
	{"leaves", "Опавшие листья", "Декор", "nature/decorations/fallen_leaves.png", false, true},
}

func Find(id string) (Definition, bool) {
	for _, d := range Catalog {
		if d.ID == id {
			return d, true
		}
	}
	return Definition{}, false
}

type Object struct {
	ID       string `json:"id"`
	Kind     string `json:"kind"`
	X        int    `json:"x"` // grid cell, not character coordinates
	Y        int    `json:"y"`
	Rotation int    `json:"rotation"` // quarter turns, 0..3
	Owner    int64  `json:"owner,omitempty"`
	Mine     bool   `json:"mine,omitempty"`
}
type Snapshot struct {
	Version int64    `json:"version"`
	Objects []Object `json:"objects"`
}

func (s Snapshot) Public(uid int64) Snapshot {
	p := Snapshot{Version: s.Version, Objects: make([]Object, len(s.Objects))}
	for i, o := range s.Objects {
		o.Mine = o.Owner == uid
		o.Owner = 0
		p.Objects[i] = o
	}
	return p
}
func Position(o Object) pathing.Point {
	return pathing.Point{X: float64(o.X*GridSize) + GridSize/2, Y: float64(o.Y*GridSize) + GridSize/2}
}

// One extra world cell is the character-body safety margin, matching the base mask.
func Covers(o Object, x, y int) bool {
	return x >= o.X*GridSize-1 && x < (o.X+1)*GridSize+1 && y >= o.Y*GridSize-1 && y < (o.Y+1)*GridSize+1
}

type Grid struct {
	Base    *scene.Scene
	blocked []bool
}

func NewGrid(base *scene.Scene, objects []Object) Grid {
	g := Grid{Base: base, blocked: make([]bool, 256*256)}
	for _, o := range objects {
		d, ok := Find(o.Kind)
		if !ok || !d.Solid {
			continue
		}
		for y := max(0, o.Y*GridSize-1); y < min(256, (o.Y+1)*GridSize+1); y++ {
			for x := max(0, o.X*GridSize-1); x < min(256, (o.X+1)*GridSize+1); x++ {
				g.blocked[y*256+x] = true
			}
		}
	}
	return g
}
func (g Grid) Walkable(x, y int) bool {
	return x >= 0 && y >= 0 && x < 256 && y < 256 && g.Base.Walkable(x, y) && !g.blocked[y*256+x]
}
func Validate(s *scene.Scene, objects []Object, o Object, people []pathing.Point, builder pathing.Point) error {
	d, ok := Find(o.Kind)
	if !ok || o.Rotation < 0 || o.Rotation > 3 || o.X < 0 || o.Y < 0 || o.X >= 64 || o.Y >= 64 {
		return ErrPlacement
	}
	p := Position(o)
	if math.Hypot(p.X-builder.X, p.Y-builder.Y) > Reach {
		return errors.New("build_too_far")
	}
	for y := o.Y*GridSize - 1; y < (o.Y+1)*GridSize+1; y++ {
		for x := o.X*GridSize - 1; x < (o.X+1)*GridSize+1; x++ {
			if !s.Walkable(x, y) || s.Water(x, y) || s.Reserved(x, y) {
				return ErrPlacement
			}
		}
	}
	for _, other := range objects {
		od, ok := Find(other.Kind)
		if !ok {
			return ErrPlacement
		}
		if other.X == o.X && other.Y == o.Y && od.Ground == d.Ground {
			return errors.New("build_occupied")
		}
	}
	if d.Solid {
		for _, p := range people {
			if Covers(o, int(p.X), int(p.Y)) {
				return errors.New("build_player_here")
			}
		}
	}
	return nil
}
