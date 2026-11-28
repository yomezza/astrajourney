package workspaces

import (
	"context"
	"errors"
	"net/http"
	"reflect"
	"strings"
	"unicode"

	"github.com/jackc/pgx/v5"

	"github.com/yomezza/cosmosimulator/internal/corporations"
	"github.com/yomezza/cosmosimulator/internal/platform/httpkit"
)

type Binding struct {
	Mode     string `json:"mode"`
	Relation string `json:"relation,omitempty"`
	Ship     int64  `json:"ship_id,omitempty"`
	Star     int    `json:"star,omitempty"`
	Station  string `json:"station,omitempty"`
	Company  int64  `json:"company_id,omitempty"`
}

type Settings struct {
	Geometry   *Geometry `json:"geometry,omitempty"`
	Wide       bool      `json:"wide,omitempty"`
	Section    string    `json:"section,omitempty"`
	Display    string    `json:"display,omitempty"`
	Sort       string    `json:"sort,omitempty"`
	Category   string    `json:"category,omitempty"`
	LegacyView string    `json:"legacy_view,omitempty"`
}

type Instance struct {
	ID       string   `json:"id"`
	Type     string   `json:"type"`
	Purpose  string   `json:"purpose"`
	Binding  Binding  `json:"binding"`
	Settings Settings `json:"settings"`
}

type workspaceV2 struct {
	ID         string     `json:"id"`
	Name       string     `json:"name"`
	Customized bool       `json:"customized,omitempty"`
	Ship       int64      `json:"ship_id"`
	Modules    []Instance `json:"modules"`
}

type layoutV2 struct {
	Version    int           `json:"version"`
	Active     string        `json:"active"`
	Default    string        `json:"default"`
	Workspaces []workspaceV2 `json:"workspaces"`
}

type preferencesV2 struct {
	Revision int64    `json:"revision"`
	Layout   layoutV2 `json:"layout"`
}

type bindingTarget uint8

const (
	targetGlobal bindingTarget = iota
	targetShip
	targetPersonal
	targetCorporate
)

type variant struct {
	ID         string   `json:"id"`
	Variant    string   `json:"variant"`
	Purpose    string   `json:"purpose"`
	Bindings   []string `json:"bindings"`
	Placements []string `json:"placements"`
	target     bindingTarget
}

type family struct {
	Type     string    `json:"type"`
	ID       string    `json:"id"`
	Variants []variant `json:"variants"`
	settings func(Settings) bool
}

func descriptor(id, name, purpose string, target bindingTarget, modes ...string) variant {
	return variant{id, name + "." + purpose, purpose, modes, []string{"tile", "window"}, target}
}

var families = []family{
	{"ship", "M001", []variant{descriptor("M001.01", "ship", "owned", targetShip, "workspace", "fixed")}, shipSettings},
	{"inventory", "M002", []variant{
		descriptor("M002.01", "inventory", "cargo", targetShip, "workspace", "fixed"),
		descriptor("M002.02", "inventory", "personal", targetPersonal, "derived", "fixed"),
		descriptor("M002.03", "inventory", "corporate", targetCorporate, "derived", "fixed"),
	}, inventorySettings},
	{"navigation", "M003", []variant{
		descriptor("M003.01", "navigation", "interstellar", targetShip, "workspace", "fixed"),
		descriptor("M003.02", "navigation", "local", targetShip, "workspace", "fixed"),
	}, commonSettings},
}

func commonSettings(s Settings) bool {
	return s.Section == "" && s.Display == "" && s.Sort == "" && s.Category == ""
}

func shipSettings(s Settings) bool {
	return oneOf(s.Section, "", "overview", "specs", "equipment") && s.Display == "" && s.Sort == "" && s.Category == ""
}

func inventorySettings(s Settings) bool {
	return s.Section == "" && oneOf(s.Display, "", "table", "grid") &&
		oneOf(s.Sort, "", "name", "quantity") && oneOf(s.Category, "", "resources", "equipment")
}

func (s *Service) Catalog(w http.ResponseWriter, r *http.Request) {
	if _, ok := s.Auth.User(w, r); ok {
		httpkit.Reply(w, 200, map[string]any{"families": families})
	}
}

func migrateLayout(old Layout) layoutV2 {
	layout := layoutV2{2, old.Active, old.Default, make([]workspaceV2, 0, len(old.Workspaces))}
	for _, space := range old.Workspaces {
		next := workspaceV2{space.ID, space.Name, space.Customized, space.ShipID, make([]Instance, 0, len(space.Modules))}
		for _, module := range space.Modules {
			instance := Instance{ID: module.ID, Type: "legacy", Purpose: module.Type,
				Binding: Binding{Mode: "workspace"}, Settings: Settings{Geometry: module.Geometry, Wide: module.Wide, Sort: module.Filter, LegacyView: module.View}}
			if module.Binding == "fixed" {
				instance.Binding = Binding{Mode: "fixed", Ship: module.ShipID}
			}
			switch module.Type {
			case "fleet", "overview":
				instance.Type, instance.Purpose = "ship", "owned"
			case "cargo":
				instance.Type, instance.Purpose = "inventory", "cargo"
			case "map":
				instance.Type, instance.Purpose = "navigation", "interstellar"
			}
			next.Modules = append(next.Modules, instance)
		}
		layout.Workspaces = append(layout.Workspaces, next)
	}
	return layout
}

func (m Instance) valid() bool {
	b, s := m.Binding, m.Settings
	if !validGeometry(s.Geometry) || !oneOf(s.LegacyView, "", "card", "window") {
		return false
	}
	if m.Type == "legacy" {
		if !commonSettings(s) {
			return false
		}
		switch m.Purpose {
		case "operations":
			return b.valid(targetGlobal) || b == (Binding{Mode: "workspace"})
		case "market":
			return b == (Binding{Mode: "workspace"})
		case "mining":
			return b.valid(targetShip)
		}
		return false
	}
	for _, f := range families {
		if f.Type != m.Type {
			continue
		}
		for _, v := range f.Variants {
			if v.Purpose == m.Purpose {
				return oneOf(b.Mode, v.Bindings...) && oneOf("tile", v.Placements...) && b.valid(v.target) && f.settings(s)
			}
		}
	}
	return false
}

func (b Binding) valid(target bindingTarget) bool {
	switch target {
	case targetGlobal:
		return b == (Binding{Mode: "global"})
	case targetShip:
		return b == (Binding{Mode: "workspace"}) || b.Ship > 0 && b == (Binding{Mode: "fixed", Ship: b.Ship})
	case targetPersonal, targetCorporate:
		if (target == targetPersonal && b.Company != 0) || b.Company < 0 {
			return false
		}
		if b.Mode == "derived" {
			return b == (Binding{Mode: "derived", Relation: "docked_station", Company: b.Company})
		}
		return b.Star > 0 && len(b.Station) > 0 && len(b.Station) <= 80 && !strings.ContainsFunc(b.Station, unicode.IsControl) &&
			(target != targetCorporate || b.Company > 0) && b == (Binding{Mode: "fixed", Star: b.Star, Station: b.Station, Company: b.Company})
	default:
		return false
	}
}

func oneOf(value string, choices ...string) bool {
	for _, choice := range choices {
		if value == choice {
			return true
		}
	}
	return false
}

func (l layoutV2) ids() []string {
	ids := []string{}
	for _, w := range l.Workspaces {
		ids = append(ids, w.ID)
		for _, m := range w.Modules {
			ids = append(ids, m.ID)
		}
	}
	return ids
}

func (l *layoutV2) valid() bool {
	if l.Version != 2 || len(l.Workspaces) < 1 || len(l.Workspaces) > 10 {
		return false
	}
	ids := map[string]bool{}
	active, start := false, false
	for i := range l.Workspaces {
		w := &l.Workspaces[i]
		w.Name = normalizedName(w.Name)
		if !uuid.MatchString(w.ID) || ids[w.ID] || !validName(w.Name) || w.Ship < 0 || w.Modules == nil || len(w.Modules) > 16 {
			return false
		}
		ids[w.ID] = true
		active = active || w.ID == l.Active
		start = start || w.ID == l.Default
		for _, m := range w.Modules {
			if !uuid.MatchString(m.ID) || ids[m.ID] || !m.valid() {
				return false
			}
			ids[m.ID] = true
		}
	}
	return active && start
}

type reference struct {
	instance string
	binding  Binding
}

func (l layoutV2) references() map[reference]bool {
	refs := map[reference]bool{}
	for _, w := range l.Workspaces {
		if w.Ship != 0 {
			refs[reference{w.ID, Binding{Mode: "fixed", Ship: w.Ship}}] = true
		}
		for _, m := range w.Modules {
			b := m.Binding
			if b.Ship != 0 || b.Star != 0 || b.Company != 0 {
				refs[reference{m.ID, b}] = true
			}
		}
	}
	return refs
}

func accessible(ctx context.Context, tx pgx.Tx, uid int64, b Binding) (bool, error) {
	var ok bool
	if b.Ship != 0 {
		err := tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM ships WHERE id=$1 AND user_id=$2)`, b.Ship, uid).Scan(&ok)
		return ok, err
	}
	if b.Company != 0 {
		cid, _, err := corporations.CompanyAccess(ctx, tx, uid, "member")
		if err != nil || cid != b.Company {
			return false, err
		}
	}
	if b.Star == 0 {
		return true, nil
	}
	err := tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM stations WHERE star=$1 AND object_id=$2 AND active)
		AND ($3::bigint=0 OR EXISTS(SELECT 1 FROM company_projects WHERE company_id=$3 AND star=$1 AND station=$2 AND kind='warehouse' AND status='complete'))`, b.Star, b.Station, b.Company).Scan(&ok)
	return ok, err
}

func (s *Service) PreferencesV2(w http.ResponseWriter, r *http.Request) {
	user, ok := s.Auth.User(w, r)
	if !ok {
		return
	}
	ctx := r.Context()
	var next preferencesV2
	if r.Method == http.MethodPut {
		r.Body = http.MaxBytesReader(w, r.Body, 64<<10)
		if !httpkit.Decode(w, r, &next) {
			return
		}
		if next.Revision < 1 || !next.Layout.valid() {
			httpkit.Fail(w, 400, "Некорректная раскладка")
			return
		}
	}
	if err := s.ensureV2Account(ctx, user.ID); err != nil {
		httpkit.Internal(w, err)
		return
	}
	if r.Method == http.MethodGet {
		var current preferencesV2
		var old Layout
		var saved *layoutV2
		err := s.DB.QueryRow(ctx, `SELECT revision,layout,layout_v2 FROM pilot_workspace_preferences WHERE user_id=$1`, user.ID).Scan(&current.Revision, &old, &saved)
		if err != nil {
			httpkit.Internal(w, err)
			return
		}
		current.Layout = migrateLayout(old)
		if saved != nil {
			current.Layout = *saved
		}
		httpkit.Reply(w, 200, current)
		return
	}
	result, status, err := s.saveV2(ctx, user.ID, next)
	if err != nil {
		httpkit.Internal(w, err)
		return
	}
	switch status {
	case 404:
		httpkit.Fail(w, status, "Объект недоступен")
	case 409:
		httpkit.Fail(w, status, "Рабочие пространства изменились в другой вкладке")
	case 422:
		httpkit.Fail(w, status, "Некорректная раскладка")
	default:
		httpkit.Reply(w, status, result)
	}
}

func (s *Service) ensureV2Account(ctx context.Context, uid int64) error {
	var exists bool
	if err := s.DB.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM pilot_workspace_preferences WHERE user_id=$1)`, uid).Scan(&exists); err != nil || exists {
		return err
	}
	old := initial()
	layout := migrateLayout(old)
	space := &layout.Workspaces[0]
	// A single owned ship is an unambiguous initial context. Accounts with
	// several ships choose a context explicitly.
	if err := s.DB.QueryRow(ctx, `SELECT CASE WHEN count(*)=1 THEN min(id) ELSE 0 END FROM ships WHERE user_id=$1`, uid).Scan(&space.Ship); err != nil {
		return err
	}
	space.Modules = []Instance{
		{ID: newID(), Type: "ship", Purpose: "owned", Binding: Binding{Mode: "workspace"}},
		{ID: newID(), Type: "navigation", Purpose: "interstellar", Binding: Binding{Mode: "workspace"}},
		{ID: newID(), Type: "inventory", Purpose: "cargo", Binding: Binding{Mode: "workspace"}},
		{ID: newID(), Type: "legacy", Purpose: "operations", Binding: Binding{Mode: "global"}},
	}
	tx, err := s.DB.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(context.Background())
	tag, err := tx.Exec(ctx, `INSERT INTO pilot_workspace_preferences(user_id,layout,layout_v2) VALUES($1,$2,$3) ON CONFLICT DO NOTHING`, uid, old, layout)
	if err != nil {
		return err
	}
	if tag.RowsAffected() != 0 {
		ok, err := claimIDs(ctx, tx, uid, old.ids(), layout.ids())
		if err != nil {
			return err
		}
		if !ok {
			return errors.New("workspace UUID collision on initialization")
		}
	}
	return tx.Commit(ctx)
}

func (s *Service) saveV2(ctx context.Context, uid int64, next preferencesV2) (preferencesV2, int, error) {
	tx, err := s.DB.Begin(ctx)
	if err != nil {
		return next, 0, err
	}
	defer tx.Rollback(context.Background())
	var old Layout
	var saved *layoutV2
	var revision int64
	err = tx.QueryRow(ctx, `SELECT revision,layout,layout_v2 FROM pilot_workspace_preferences WHERE user_id=$1 FOR UPDATE`, uid).Scan(&revision, &old, &saved)
	if err != nil {
		return next, 0, err
	}
	current := migrateLayout(old)
	if saved != nil {
		current = *saved
		if (next.Revision == revision || next.Revision == revision-1) && reflect.DeepEqual(next.Layout, current) {
			return preferencesV2{revision, current}, 200, nil
		}
	}
	if next.Revision != revision {
		return next, 409, nil
	}
	previous := current.references()
	for ref := range next.Layout.references() {
		if previous[ref] {
			continue
		}
		ok, err := accessible(ctx, tx, uid, ref.binding)
		if err != nil {
			return next, 0, err
		}
		if !ok {
			return next, 404, nil
		}
	}
	err = tx.QueryRow(ctx, `UPDATE pilot_workspace_preferences SET layout_v2=$2,revision=revision+1 WHERE user_id=$1 RETURNING revision`, uid, next.Layout).Scan(&next.Revision)
	if err != nil {
		return next, 0, err
	}
	if ok, err := claimIDs(ctx, tx, uid, old.ids(), next.Layout.ids()); err != nil || !ok {
		return next, 422, err
	}
	return next, 200, tx.Commit(ctx)
}
