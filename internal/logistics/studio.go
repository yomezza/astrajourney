package logistics

import (
	"context"
	"errors"
	"net/http"
	"sort"
	"strconv"

	"github.com/jackc/pgx/v5"

	"github.com/yomezza/cosmosimulator/internal/corporations"
	"github.com/yomezza/cosmosimulator/internal/fleet"
	"github.com/yomezza/cosmosimulator/internal/platform/httpkit"
)

type inventoryItem struct {
	Resource string `json:"resource"`
	Quantity int    `json:"quantity"`
	Category string `json:"category"`
}

type inventoryView struct {
	Location    CargoLocation      `json:"location"`
	Items       []inventoryItem    `json:"items"`
	Equipment   []fleet.ShipModule `json:"equipment"`
	Capacity    *int               `json:"capacity"`
	Used        *int               `json:"used"`
	Reserved    *int               `json:"reserved"`
	Free        *int               `json:"free"`
	CanDeposit  bool               `json:"can_deposit"`
	CanWithdraw bool               `json:"can_withdraw"`
}

func validLocation(loc CargoLocation) bool {
	switch loc.Kind {
	case "ship":
		return loc.Ship > 0 && loc.Star == 0 && loc.Station == "" && loc.Company == 0
	case "personal", "company":
		return loc.Ship == 0 && loc.Star > 0 && len(loc.Station) > 0 && len(loc.Station) <= 80 &&
			(loc.Kind == "personal" && loc.Company == 0 || loc.Kind == "company" && loc.Company > 0)
	default:
		return false
	}
}

func storagePermission(ctx context.Context, tx pgx.Tx, uid int64, loc CargoLocation, requireWithdraw bool) (bool, error) {
	withdraw := true
	if loc.Kind == "company" {
		cid, allowed, err := corporations.CompanyAccess(ctx, tx, uid, "warehouse")
		if err != nil {
			return false, err
		}
		if cid == 0 || cid != loc.Company || requireWithdraw && !allowed {
			return false, pgx.ErrNoRows
		}
		withdraw = allowed
	}
	var available bool
	err := tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM stations WHERE star=$1 AND object_id=$2 AND active)
		AND ($3::bigint=0 OR EXISTS(SELECT 1 FROM company_projects WHERE company_id=$3 AND star=$1 AND station=$2 AND kind='warehouse' AND status='complete'))`,
		loc.Star, loc.Station, loc.Company).Scan(&available)
	if err != nil {
		return false, err
	}
	if !available {
		return false, pgx.ErrNoRows
	}
	return withdraw, nil
}

func (a *Service) StudioInventory(w http.ResponseWriter, r *http.Request) {
	u, ok := a.Auth.User(w, r)
	if !ok {
		return
	}
	q := r.URL.Query()
	loc := CargoLocation{Kind: q.Get("kind"), Station: q.Get("station")}
	var err error
	for key, dst := range map[string]*int64{"ship": &loc.Ship, "company": &loc.Company} {
		if q.Has(key) {
			*dst, err = strconv.ParseInt(q.Get(key), 10, 64)
			if err != nil || *dst < 1 {
				httpkit.Fail(w, 400, "Некорректный адрес склада")
				return
			}
		}
	}
	if q.Has("star") {
		loc.Star, err = strconv.Atoi(q.Get("star"))
		if err != nil || loc.Star < 1 {
			httpkit.Fail(w, 400, "Некорректный адрес склада")
			return
		}
	}
	derived := loc.Ship > 0 && loc.Kind != "ship" && loc.Star == 0 && loc.Station == "" &&
		(loc.Kind == "company" || loc.Kind == "personal" && loc.Company == 0)
	if !derived && !validLocation(loc) {
		httpkit.Fail(w, 400, "Некорректный адрес склада")
		return
	}
	ctx := r.Context()
	tx, err := a.DB.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.RepeatableRead, AccessMode: pgx.ReadOnly})
	if err != nil {
		httpkit.Internal(w, err)
		return
	}
	defer tx.Rollback(context.Background())
	out, err := readInventory(ctx, tx, u.ID, loc, derived)
	if errors.Is(err, pgx.ErrNoRows) {
		httpkit.Fail(w, 404, "Объект недоступен")
		return
	}
	if errors.Is(err, errNotDocked) {
		httpkit.Fail(w, 409, err.Error())
		return
	}
	if err != nil {
		httpkit.Internal(w, err)
		return
	}
	httpkit.Reply(w, 200, out)
}

var errNotDocked = errors.New("Корабль не пристыкован к станции")

func readInventory(ctx context.Context, tx pgx.Tx, uid int64, loc CargoLocation, derived bool) (inventoryView, error) {
	out := inventoryView{Items: []inventoryItem{}, Equipment: []fleet.ShipModule{}}
	var cargo map[string]int
	var ship fleet.Ship
	var err error
	if loc.Ship > 0 {
		ship, err = fleet.ReadOwnedShip(ctx, tx, uid, loc.Ship)
		if err != nil {
			return out, err
		}
	}
	if derived {
		if !fleet.Docked(ship) {
			return out, errNotDocked
		}
		loc.Ship, loc.Star, loc.Station = 0, ship.Star, ship.ObjectID
		if loc.Kind == "company" && loc.Company == 0 {
			loc.Company, _, err = corporations.CompanyAccess(ctx, tx, uid, "warehouse")
			if err != nil {
				return out, err
			}
			if loc.Company == 0 {
				return out, pgx.ErrNoRows
			}
		}
	}
	if loc.Kind == "ship" {
		cargo = ship.Cargo
		for _, module := range ship.Modules {
			if !module.Installed {
				out.Equipment = append(out.Equipment, module)
			}
		}
		capacity, used, reserved := ship.Capacity(), ship.CargoUsed(), 0
		err = tx.QueryRow(ctx, `SELECT COALESCE(sum(quantity),0) FROM mining_jobs WHERE ship_id=$1`, ship.ID).Scan(&reserved)
		if err != nil {
			return out, err
		}
		free := max(0, capacity-used-reserved)
		out.Capacity, out.Used, out.Reserved, out.Free = &capacity, &used, &reserved, &free
		out.CanWithdraw, out.CanDeposit = true, true
	} else {
		out.CanWithdraw, err = storagePermission(ctx, tx, uid, loc, false)
		if err != nil {
			return out, err
		}
		out.CanDeposit = true
		if loc.Kind == "company" {
			err = tx.QueryRow(ctx, `SELECT cargo FROM company_storage WHERE company_id=$1 AND star=$2 AND station=$3`, loc.Company, loc.Star, loc.Station).Scan(&cargo)
		} else {
			err = tx.QueryRow(ctx, `SELECT cargo FROM warehouses WHERE user_id=$1 AND star=$2 AND station=$3`, uid, loc.Star, loc.Station).Scan(&cargo)
		}
		if err != nil && !errors.Is(err, pgx.ErrNoRows) {
			return out, err
		}
	}
	out.Location = loc
	for key, quantity := range cargo {
		if quantity > 0 {
			out.Items = append(out.Items, inventoryItem{key, quantity, "resources"})
		}
	}
	sort.Slice(out.Items, func(i, j int) bool { return out.Items[i].Resource < out.Items[j].Resource })
	return out, nil
}
