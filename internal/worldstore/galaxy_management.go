package worldstore

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/jackc/pgx/v5"

	"github.com/yomezza/cosmosimulator/internal/fleet"
	"github.com/yomezza/cosmosimulator/internal/platform/httpkit"
	resourcecatalog "github.com/yomezza/cosmosimulator/internal/resources"
	"github.com/yomezza/cosmosimulator/internal/universe"
)

func protectedTravelLanes(ctx context.Context, tx pgx.Tx, g *universe.Galaxy) ([]universe.Lane, error) {
	rows, err := tx.Query(ctx, `SELECT star,COALESCE(destination,star),travel_path,waypoints FROM ships ORDER BY id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []universe.Lane{}
	for rows.Next() {
		var star, to int
		var raw, queued []byte
		var path, waypoints []int
		if err = rows.Scan(&star, &to, &raw, &queued); err != nil {
			return nil, err
		}
		if err = json.Unmarshal(raw, &path); err != nil {
			return nil, err
		}
		if err = json.Unmarshal(queued, &waypoints); err != nil {
			return nil, err
		}
		for i := 1; i < len(path); i++ {
			out = append(out, universe.Lane{From: path[i-1], To: path[i]})
		}
		from := to
		for _, next := range waypoints {
			p := g.Path(from, next)
			for i := 1; i < len(p); i++ {
				out = append(out, universe.Lane{From: p[i-1], To: p[i]})
			}
			from = next
		}
	}
	return out, rows.Err()
}

// New shortcuts may reduce the fuel needed by queued legs. Release the surplus
// reservation now so the ship does not finish its route with fuel locked forever.
func repriceQueuedRoutes(ctx context.Context, tx pgx.Tx, g *universe.Galaxy) error {
	rows, err := tx.Query(ctx, `SELECT id,COALESCE(destination,star),waypoints,class,fuel FROM ships WHERE reserved_fuel<>0 OR jsonb_array_length(waypoints)>0 ORDER BY id`)
	if err != nil {
		return err
	}
	type reservation struct {
		id   int64
		cost int
	}
	list := []reservation{}
	for rows.Next() {
		var sid int64
		var from, fuel int
		var raw []byte
		var class string
		if err = rows.Scan(&sid, &from, &raw, &class, &fuel); err != nil {
			rows.Close()
			return err
		}
		var points []int
		if err = json.Unmarshal(raw, &points); err != nil {
			rows.Close()
			return err
		}
		cost := 0
		for _, to := range points {
			path := g.Path(from, to)
			if len(path) == 0 {
				rows.Close()
				return errors.New("Сохранённый маршрут недоступен")
			}
			cost += (len(path) - 1) * (fleet.Ship{Class: class}).Hull().JumpFuel
			from = to
		}
		if cost > fuel {
			rows.Close()
			return errors.New("Недостаточно топлива для сохранённого маршрута")
		}
		list = append(list, reservation{sid, cost})
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return err
	}
	for _, r := range list {
		if _, err = tx.Exec(ctx, `UPDATE ships SET reserved_fuel=$2 WHERE id=$1`, r.id, r.cost); err != nil {
			return err
		}
	}
	return nil
}

// Ports with player property or live obligations are exempt from redistribution.
func protectedPorts(ctx context.Context, tx pgx.Tx) (map[string]bool, error) {
	rows, err := tx.Query(ctx, `SELECT star,object_id FROM stations s WHERE company_id IS NOT NULL
 OR EXISTS(SELECT 1 FROM ships p WHERE p.star=s.star AND (p.object_id=s.object_id OR p.local_target=s.object_id))
 OR EXISTS(SELECT 1 FROM warehouses w WHERE w.star=s.star AND w.station=s.object_id AND w.cargo<>'{}')
 OR EXISTS(SELECT 1 FROM company_storage w WHERE w.star=s.star AND w.station=s.object_id AND w.cargo<>'{}')
 OR EXISTS(SELECT 1 FROM reservations r WHERE r.star=s.star AND r.station=s.object_id AND r.status='reserved')
 OR EXISTS(SELECT 1 FROM sell_orders o WHERE o.star=s.star AND o.station=s.object_id AND o.quantity>0)
 OR EXISTS(SELECT 1 FROM survey_offers o WHERE o.star=s.star AND o.station=s.object_id)
 OR EXISTS(SELECT 1 FROM automation_tasks t WHERE t.status IN('queued','running','paused') AND ((t.kind IN('haul','load') AND t.source_star=s.star AND t.source_object=s.object_id) OR (t.dest_star=s.star AND t.dest_station=s.object_id)))
 OR EXISTS(SELECT 1 FROM deliveries d WHERE d.status IN('open','carrying','bot') AND ((d.from_star=s.star AND d.from_station=s.object_id) OR (d.to_star=s.star AND d.to_station=s.object_id)))`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[string]bool{}
	for rows.Next() {
		var star int
		var object string
		if err = rows.Scan(&star, &object); err != nil {
			return nil, err
		}
		out[fmt.Sprintf("%d/%s", star, object)] = true
	}
	return out, rows.Err()
}

func plannedPorts(g *universe.Galaxy, protected map[string]bool) map[string]bool {
	out := map[string]bool{}
	for key := range protected {
		var id int
		fmt.Sscanf(key, "%d/", &id)
		if id > 0 && id <= len(g.Stars) && g.Stars[id-1].Deleted {
			continue
		}
		out[key] = true
	}
	for _, s := range g.Stars {
		if s.Deleted {
			continue
		}
		for _, site := range universe.SystemSites(s) {
			if site.Kind == "Орбитальная станция" {
				out[fmt.Sprintf("%d/%s", s.ID, site.ID)] = true
			}
		}
	}
	return out
}

func ReconcileStations(ctx context.Context, tx pgx.Tx, g *universe.Galaxy) error {
	protected, err := protectedPorts(ctx, tx)
	if err != nil {
		return err
	}
	if err = EnsureStations(ctx, tx, g); err != nil {
		return err
	}
	// Protected old ports keep their inventories and obligations. A public port
	// outside today's colonies becomes neutral rather than claiming the frontier.
	owners := make([]map[string]any, 0, len(g.Stars))
	for _, s := range g.Stars {
		owners = append(owners, map[string]any{"star": s.ID, "founder_key": s.Founder})
	}
	raw, err := json.Marshal(owners)
	if err != nil {
		return err
	}
	if _, err = tx.Exec(ctx, `UPDATE stations s SET founder=c.id FROM jsonb_to_recordset($1) AS x(star int,founder_key text) LEFT JOIN companies c ON c.founder_key=x.founder_key WHERE s.star=x.star AND s.company_id IS NULL`, raw); err != nil {
		return err
	}
	keys := []string{}
	for key := range plannedPorts(g, protected) {
		keys = append(keys, key)
	}
	_, err = tx.Exec(ctx, `UPDATE stations SET active=(star::text||'/'||object_id)=ANY($1::text[])`, keys)
	return err
}

func UpgradeManagedGalaxies(ctx context.Context, tx pgx.Tx, g *universe.Galaxy) (bool, error) {
	if len(g.Catalog) > 0 {
		return false, nil
	}
	universe.InitializeGalaxyCatalog(g)
	protected, err := protectedTravelLanes(ctx, tx, g)
	if err != nil {
		return false, err
	}
	if err = universe.AppendGalaxySystems(g, "edem", 3000); err != nil {
		return false, err
	}
	for _, d := range g.Catalog {
		if d.Opened {
			universe.AssignGalaxyContent(g, d.ID)
			universe.RebuildGalaxyLanes(g, d.ID, protected)
		}
	}
	g.Revision = 1
	g.SettlementVersion = 1
	if err = repriceQueuedRoutes(ctx, tx, g); err != nil {
		return false, err
	}
	return true, ReconcileStations(ctx, tx, g)
}

func validateGalaxyEdit(in universe.GalaxyEdit, g *universe.Galaxy) error {
	d := g.Definition(in.ID)
	if in.Create {
		if d != nil || len(universe.VisibleGalaxies(g)) >= universe.MaxGalaxies || len(in.ID) < 3 || len(in.ID) > 40 || strings.ContainsFunc(in.ID, func(r rune) bool { return !(r >= 'a' && r <= 'z' || r >= '0' && r <= '9' || r == '-') }) {
			return errors.New("Некорректная новая галактика")
		}
		if in.Systems < universe.MinGalaxySystems || in.Systems > universe.MaxGalaxySystems {
			return errors.New("В галактике должно быть от 5 до 50000 систем")
		}
		if in.Shape != "spiral" && in.Shape != "rings" && in.Shape != "elliptical" && in.Shape != "irregular" {
			return errors.New("Неизвестная форма галактики")
		}
	} else if d == nil || !d.Opened || d.Deleted {
		return errors.New("Галактика не открыта")
	}
	if strings.TrimSpace(in.Name) == "" || utf8.RuneCountInString(in.Name) > 48 {
		return errors.New("Название должно содержать от 1 до 48 символов")
	}
	for _, other := range g.Catalog {
		if !other.Deleted && other.ID != in.ID && strings.EqualFold(other.Name, strings.TrimSpace(in.Name)) {
			return errors.New("Название галактики уже используется")
		}
	}
	s := in.Settings
	if s.ExtraLanes < 0 || s.ExtraLanes > 100 || s.CrossLinks < 0 || s.CrossLinks > 40 || s.MaxLaneLength < 50 || s.MaxLaneLength > 10000 || s.StationPercent < 0 || s.StationPercent > 100 || s.MaxPorts < 1 || s.MaxPorts > 5 {
		return errors.New("Некорректные параметры галактики")
	}
	if len(s.ResourceWeights) != len(resourcecatalog.All) {
		return errors.New("Укажите распространённость каждого ресурса")
	}
	for _, r := range resourcecatalog.All {
		v, ok := s.ResourceWeights[r.ID]
		if !ok || v < 0 || v > 300 {
			return errors.New("Распространённость ресурса должна быть от 0 до 300%")
		}
	}
	if len(g.Stars)+in.Systems-len(universe.GalaxyStars(g, in.ID)) > universe.MaxWorldSystems {
		return errors.New("Достигнут общий предел систем вселенной")
	}
	if in.Systems < universe.MinGalaxySystems || in.Systems < len(universe.GalaxyStars(g, in.ID)) || in.Systems > universe.MaxGalaxySystems {
		return errors.New("Количество систем можно только увеличивать, максимум 50000")
	}
	return nil
}

func buildGalaxyEdit(ctx context.Context, tx pgx.Tx, g *universe.Galaxy, in universe.GalaxyEdit) (*universe.Galaxy, map[string]bool, error) {
	if err := validateGalaxyEdit(in, g); err != nil {
		return nil, nil, err
	}
	raw, _ := json.Marshal(g)
	var next universe.Galaxy
	if err := json.Unmarshal(raw, &next); err != nil {
		return nil, nil, err
	}
	if in.Create {
		base := 8
		for _, r := range next.Regions {
			if r.ID >= base {
				base = r.ID + 1
			}
		}
		radius := 3600.0
		if in.Shape == "rings" {
			radius = 1250
		}
		next.Catalog = append(next.Catalog, universe.GalaxyDefinition{ID: in.ID, Name: strings.TrimSpace(in.Name), Shape: in.Shape, Seed: in.Seed, Opened: true, Radius: radius, RegionBase: base, Settings: in.Settings})
		for i, title := range []string{"Тихая дуга", "Светлый предел", "Дальний рубеж", "Звёздный поток"} {
			next.Regions = append(next.Regions, universe.Region{ID: base + i, Name: strings.TrimSpace(in.Name) + " · " + title, Specialty: []string{"iron", "orichalcum", "azoth", "vajra"}[i]})
		}
	}
	d := next.Definition(in.ID)
	d.Name = strings.TrimSpace(in.Name)
	d.Settings = in.Settings
	protected, err := protectedTravelLanes(ctx, tx, g)
	if err != nil {
		return nil, nil, err
	}
	if err = universe.AppendGalaxySystems(&next, in.ID, in.Systems); err != nil {
		return nil, nil, err
	}
	universe.AssignGalaxyContent(&next, in.ID)
	universe.RebuildGalaxyLanes(&next, in.ID, protected)
	next.Revision = g.Revision + 1
	ports, err := protectedPorts(ctx, tx)
	return &next, ports, err
}

func previewDigest(g *universe.Galaxy, ports map[string]bool) string {
	raw, _ := json.Marshal([]any{g, ports})
	sum := sha256.Sum256(raw)
	return hex.EncodeToString(sum[:])
}

func galaxyStatistics(g *universe.Galaxy, id string, ports map[string]bool) map[string]any {
	stars := universe.GalaxyStars(g, id)
	claimed := 0
	for _, s := range stars {
		if s.Founder != "" {
			claimed++
		}
	}
	lanes, cross, stationSystems, stationCount := 0, 0, map[int]bool{}, 0
	for _, e := range g.Lanes {
		if universe.GalaxyKey(g.Stars[e.From-1]) == id {
			lanes++
			if g.Stars[e.From-1].Region != g.Stars[e.To-1].Region {
				cross++
			}
		}
	}
	for key := range plannedPorts(g, ports) {
		var star int
		fmt.Sscanf(key, "%d/", &star)
		if star > 0 && universe.GalaxyKey(g.Stars[star-1]) == id {
			stationCount++
			stationSystems[star] = true
		}
	}
	resourceSystems := map[string]int{}
	for _, s := range stars {
		seen := map[string]bool{}
		for _, site := range universe.SystemSites(s) {
			for _, r := range site.Resources {
				seen[r] = true
			}
		}
		for r := range seen {
			resourceSystems[r]++
		}
	}
	return map[string]any{"systems": len(stars), "lanes": lanes, "cross_links": cross, "station_systems": len(stationSystems), "stations": stationCount, "resources": resourceSystems, "founder_systems": claimed, "neutral_systems": len(stars) - claimed}
}

func (a *Service) AdminGalaxies(w http.ResponseWriter, r *http.Request) {
	// Keep authentication bounded even when the generation budget is longer.
	authContext, cancelAuth := context.WithTimeout(r.Context(), 12*time.Second)
	u, ok := a.Auth.User(w, r.WithContext(authContext))
	cancelAuth()
	if !ok {
		return
	}
	if u.Role != "admin" {
		httpkit.Fail(w, 403, "Требуется роль администратора")
		return
	}
	ctx := r.Context()
	tx, err := a.DB.Begin(ctx)
	if err != nil {
		httpkit.Internal(w, err)
		return
	}
	defer tx.Rollback(context.Background())
	apply := r.URL.Path == "/api/admin/galaxies/apply"
	if apply {
		if _, err = tx.Exec(ctx, `SELECT pg_advisory_xact_lock(784218)`); err != nil {
			httpkit.Internal(w, err)
			return
		}
	}
	var raw []byte
	sql := `SELECT data FROM galaxies WHERE name='Edem'`
	if apply {
		sql += " FOR UPDATE"
	}
	if err = tx.QueryRow(ctx, sql).Scan(&raw); err != nil {
		httpkit.Internal(w, err)
		return
	}
	var g universe.Galaxy
	if err = json.Unmarshal(raw, &g); err != nil {
		httpkit.Internal(w, err)
		return
	}
	if r.Method == "GET" {
		ports, e := protectedPorts(ctx, tx)
		if e != nil {
			httpkit.Internal(w, e)
			return
		}
		list := []map[string]any{}
		for _, d := range g.Catalog {
			if d.Deleted {
				continue
			}
			list = append(list, map[string]any{"definition": d, "stats": galaxyStatistics(&g, d.ID, ports)})
		}
		httpkit.Reply(w, 200, map[string]any{"galaxies": list, "revision": g.Revision})
		return
	}
	var in universe.GalaxyEdit
	if !httpkit.Decode(w, r, &in) {
		return
	}
	next, ports, err := buildGalaxyEdit(ctx, tx, &g, in)
	if err != nil {
		httpkit.Fail(w, 400, err.Error())
		return
	}
	digest := previewDigest(next, ports)
	if !apply {
		points := universe.GalaxyStars(next, in.ID)
		for i := range points {
			points[i].Planets = nil
			points[i].ResourceWeights = nil
		}
		lanes := []universe.Lane{}
		for _, e := range next.Lanes {
			if universe.GalaxyKey(next.Stars[e.From-1]) == in.ID {
				lanes = append(lanes, e)
			}
		}
		httpkit.Reply(w, 200, map[string]any{"preview": digest, "before": galaxyStatistics(&g, in.ID, ports), "after": galaxyStatistics(next, in.ID, ports), "stars": points, "lanes": lanes, "protected_ports": len(ports), "radius": next.Definition(in.ID).Radius, "territories": next.Definition(in.ID).Territories})
		return
	}
	if in.Preview == "" || in.Preview != digest {
		httpkit.Fail(w, 409, "Карта или задания изменились. Сформируйте предпросмотр заново")
		return
	}
	var role string
	if err = tx.QueryRow(ctx, `SELECT role FROM users WHERE id=$1 FOR UPDATE`, u.ID).Scan(&role); err != nil {
		httpkit.Internal(w, err)
		return
	}
	if role != "admin" {
		httpkit.Fail(w, 403, "Требуется роль администратора")
		return
	}
	if err = ReconcileStations(ctx, tx, next); err != nil {
		httpkit.Internal(w, err)
		return
	}
	if err = repriceQueuedRoutes(ctx, tx, next); err != nil {
		httpkit.Internal(w, err)
		return
	}
	raw, _ = json.Marshal(next)
	if _, err = tx.Exec(ctx, `UPDATE galaxies SET data=$1 WHERE name='Edem'`, raw); err != nil {
		httpkit.Internal(w, err)
		return
	}
	audit, _ := json.Marshal(map[string]any{"galaxy": in.ID, "name": in.Name, "before": galaxyStatistics(&g, in.ID, ports), "after": galaxyStatistics(next, in.ID, ports), "settings": in.Settings, "revision": next.Revision})
	if _, err = tx.Exec(ctx, `INSERT INTO admin_log(actor,action,data) VALUES($1,'galaxy_update',$2)`, u.ID, audit); err != nil {
		httpkit.Internal(w, err)
		return
	}
	a.universeMu.Lock()
	err = tx.Commit(ctx)
	if err == nil {
		a.universe = next
	}
	a.universeMu.Unlock()
	if err != nil {
		httpkit.Internal(w, err)
		return
	}
	httpkit.Reply(w, 200, map[string]any{"ok": true, "revision": next.Revision})
}
