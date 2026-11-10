// Package gamesignal defines metadata-only hints. No event grants authority.
package gamesignal

import (
	"bytes"
	"encoding/json"
	"github.com/yomezza/cosmosimulator/internal/platform/identifiers"
	"io"
	"strconv"
	"time"
)

const PlanetSectorSubject = "cosmos.v1.signal.planet.sector.changed"
const MaxPayload = 2048

type SectorChange struct {
	UserID       string `json:"user_id"`
	VisitID      string `json:"visit_id"`
	Generation   string `json:"generation"`
	Epoch        string `json:"connection_epoch"`
	RuntimeFence string `json:"runtime_fence"`
	SurfaceID    string `json:"surface_id"`
	From         int    `json:"from_sector"`
	To           int    `json:"to_sector"`
	Revision     string `json:"geography_revision"`
	Operation    string `json:"transition_operation_id"`
}

func (d SectorChange) User() int64 { v, _ := strconv.ParseInt(d.UserID, 10, 64); return v }

type PlanetSignal struct {
	Version  int          `json:"schema_version"`
	SignalID string       `json:"signal_id"`
	Source   string       `json:"source"`
	Kind     string       `json:"kind"`
	At       time.Time    `json:"occurred_at"`
	Data     SectorChange `json:"data"`
}

func positive(s string) bool {
	v, e := strconv.ParseInt(s, 10, 64)
	return e == nil && v > 0 && strconv.FormatInt(v, 10) == s
}
func (s PlanetSignal) valid() bool {
	_, offset := s.At.Zone()
	d := s.Data
	if s.Version != 1 || s.Source != "planet" || s.Kind != "planet.sector.changed" || s.At.IsZero() || offset != 0 || !identifiers.ValidUUID(s.SignalID) || s.SignalID != d.Operation || !identifiers.ValidUUID(d.VisitID) || d.From <= 0 || d.To <= 0 || d.From == d.To || d.From > 2147483647 || d.To > 2147483647 {
		return false
	}
	for _, v := range []string{d.UserID, d.Generation, d.Epoch, d.RuntimeFence, d.SurfaceID} {
		if !positive(v) {
			return false
		}
	}
	version, e := strconv.ParseInt(d.Revision, 10, 64)
	return e == nil && version >= 0 && strconv.FormatInt(version, 10) == d.Revision
}
func EncodePlanet(d SectorChange) ([]byte, bool) {
	s := PlanetSignal{1, d.Operation, "planet", "planet.sector.changed", time.Now().UTC(), d}
	if !s.valid() {
		return nil, false
	}
	b, e := json.Marshal(s)
	return b, e == nil && len(b) <= MaxPayload
}
func DecodePlanet(subject string, b []byte) (PlanetSignal, bool) {
	var s PlanetSignal
	if subject != PlanetSectorSubject || len(b) == 0 || len(b) > MaxPayload {
		return s, false
	}
	d := json.NewDecoder(bytes.NewReader(b))
	d.DisallowUnknownFields()
	if d.Decode(&s) != nil || d.Decode(new(any)) != io.EOF {
		return s, false
	}
	return s, s.valid()
}
