package gamesignal

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestPlanetContract(t *testing.T) {
	d := SectorChange{"7", "12345678-1234-4234-8234-123456789abc", "1", "3", "2", "10", 1, 2, "4", "12345678-1234-4234-8234-123456789def"}
	d.Revision = "0" // Initial surface revision is valid in the existing schema.
	raw, ok := EncodePlanet(d)
	if !ok {
		t.Fatal("encode")
	}
	s, ok := DecodePlanet(PlanetSectorSubject, raw)
	if !ok || s.SignalID != d.Operation || s.Data.User() != 7 {
		t.Fatal("decode/stable ID")
	}
	for _, change := range []func(*PlanetSignal){func(s *PlanetSignal) { s.Version = 2 }, func(s *PlanetSignal) { s.Source = "chat" }, func(s *PlanetSignal) { s.Data.UserID = "9223372036854775808" }, func(s *PlanetSignal) { s.Data.Epoch = "0" }, func(s *PlanetSignal) { s.Data.RuntimeFence = "" }, func(s *PlanetSignal) { s.Data.Generation = "-1" }, func(s *PlanetSignal) { s.Data.Revision = "01" }, func(s *PlanetSignal) { s.Data.To = s.Data.From }, func(s *PlanetSignal) { s.Data.VisitID = "bad" }, func(s *PlanetSignal) { s.SignalID = d.VisitID }} {
		v := s
		change(&v)
		b, _ := json.Marshal(v)
		if _, ok = DecodePlanet(PlanetSectorSubject, b); ok {
			t.Fatal("invalid contract accepted")
		}
	}
	for _, b := range [][]byte{[]byte(strings.Repeat("x", 2049)), append(raw, []byte("{}")...), []byte(`{"schema_version":1,"token":"do-not-accept"}`)} {
		if _, ok = DecodePlanet(PlanetSectorSubject, b); ok {
			t.Fatal("bad JSON accepted")
		}
	}
	if _, ok = DecodePlanet("cosmos.v1.signal.chat.outbox.changed", raw); ok {
		t.Fatal("wrong subject")
	}
}
