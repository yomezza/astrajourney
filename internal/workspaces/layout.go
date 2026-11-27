// Package workspaces persists account-owned presentation state.
package workspaces

import (
	"crypto/rand"
	"encoding/hex"
	"regexp"
	"strings"
	"unicode"

	"github.com/rivo/uniseg"
	"golang.org/x/text/unicode/norm"
)

type Geometry struct {
	X         int  `json:"x"`
	Y         int  `json:"y"`
	W         int  `json:"w"`
	H         int  `json:"h"`
	Pinned    bool `json:"pinned"`
	Minimized bool `json:"minimized"`
}

type Module struct {
	ID       string    `json:"id"`
	Type     string    `json:"type"`
	Binding  string    `json:"binding"`
	ShipID   int64     `json:"ship_id"`
	Geometry *Geometry `json:"geometry,omitempty"`
	Filter   string    `json:"filter,omitempty"`
	Wide     bool      `json:"wide,omitempty"`
	View     string    `json:"view,omitempty"`
	Tab      string    `json:"tab,omitempty"`
	SystemID int       `json:"system_id,omitempty"`
}

type Workspace struct {
	ID         string   `json:"id"`
	Name       string   `json:"name"`
	Customized bool     `json:"customized,omitempty"`
	ShipID     int64    `json:"ship_id"`
	Modules    []Module `json:"modules"`
}

type Layout struct {
	Version    int         `json:"version"`
	Active     string      `json:"active"`
	Default    string      `json:"default"`
	Workspaces []Workspace `json:"workspaces"`
}

var uuid = regexp.MustCompile(`^[0-9a-f]{8}-[0-9a-f]{4}-4[0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$`)

func newID() string {
	var b [16]byte
	_, _ = rand.Read(b[:])
	b[6] = b[6]&0x0f | 0x40
	b[8] = b[8]&0x3f | 0x80
	s := hex.EncodeToString(b[:])
	return s[:8] + "-" + s[8:12] + "-" + s[12:16] + "-" + s[16:20] + "-" + s[20:]
}

func initial() Layout {
	id := newID()
	return Layout{Version: 1, Active: id, Default: id, Workspaces: []Workspace{{ID: id, Name: "Астра", Modules: []Module{}}}}
}

func (l *Layout) valid() bool {
	if l.Version != 1 || len(l.Workspaces) < 1 || len(l.Workspaces) > 10 {
		return false
	}
	workspaces := map[string]bool{}
	ids := map[string]bool{}
	for i := range l.Workspaces {
		w := &l.Workspaces[i]
		w.Name = norm.NFC.String(strings.TrimSpace(w.Name))
		if !uuid.MatchString(w.ID) || ids[w.ID] || len(w.Name) > 1024 ||
			w.Name == "" || uniseg.GraphemeClusterCount(w.Name) > 32 ||
			strings.ContainsFunc(w.Name, unicode.IsControl) ||
			w.ShipID < 0 || w.Modules == nil || len(w.Modules) > 16 {
			return false
		}
		workspaces[w.ID] = true
		ids[w.ID] = true
		for _, m := range w.Modules {
			if !uuid.MatchString(m.ID) || ids[m.ID] || !m.valid() {
				return false
			}
			ids[m.ID] = true
		}
	}
	return workspaces[l.Active] && workspaces[l.Default]
}

func (m Module) valid() bool {
	if m.View != "" && m.View != "card" && m.View != "window" {
		return false
	}
	if g := m.Geometry; g != nil && (g.X < 0 || g.X > 8192 || g.Y < 0 || g.Y > 8192 ||
		g.W < 160 || g.W > 8192 || g.H < 160 || g.H > 8192) {
		return false
	}
	if m.SystemID < 0 || m.SystemID > 2147483647 || m.SystemID != 0 && m.Type != "map" {
		return false
	}
	if m.Tab != "" && (m.Type != "market" || m.Tab != "local" && m.Tab != "orders") {
		return false
	}
	switch m.Type {
	case "overview", "map", "operations", "market", "logistics", "studies",
		"colonies", "corporations", "guide", "finance", "profile":
		return m.Filter == "" && m.Binding == "none" && m.ShipID == 0
	case "tasks":
		return (m.Filter == "" || m.Filter == "all" || m.Filter == "running" || m.Filter == "queued" || m.Filter == "paused") && m.Binding == "none" && m.ShipID == 0
	case "fleet", "mining":
		if m.Filter != "" {
			return false
		}
	case "cargo":
		if m.Filter != "" && m.Filter != "name" && m.Filter != "quantity" {
			return false
		}
	default:
		return false
	}
	return ((m.Binding == "none" || m.Binding == "workspace") && m.ShipID == 0) || (m.Binding == "fixed" && m.ShipID > 0)
}

type shipBinding struct {
	id   string
	ship int64
}

func (l Layout) shipBindings() map[shipBinding]bool {
	bindings := map[shipBinding]bool{}
	for _, w := range l.Workspaces {
		if w.ShipID > 0 {
			bindings[shipBinding{w.ID, w.ShipID}] = true
		}
		for _, m := range w.Modules {
			if m.ShipID > 0 {
				bindings[shipBinding{m.ID, m.ShipID}] = true
			}
		}
	}
	return bindings
}

func (l Layout) ids() []string {
	ids := make([]string, 0, len(l.Workspaces))
	for _, w := range l.Workspaces {
		ids = append(ids, w.ID)
		for _, m := range w.Modules {
			ids = append(ids, m.ID)
		}
	}
	return ids
}
