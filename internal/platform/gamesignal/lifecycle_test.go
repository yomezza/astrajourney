package gamesignal

import (
	"bytes"
	"encoding/json"
	"testing"
)

func TestN6StrictContracts(t *testing.T) {
	for _, subject := range append(append([]string{}, CosmicSubjects...), CleanupSubject) {
		d := CompanyChange{}
		switch subject {
		case MembershipSubject, ModeratorSubject:
			d.UserID = "12"
		case RoleSubject:
			d = CompanyChange{UserID: "12", CompanyID: "9", Fence: "3"}
		case PermissionsSubject:
			d = CompanyChange{CompanyID: "9", Fence: "3"}
		default:
			d = CompanyChange{CompanyID: "9", Operation: "12345678-1234-4234-8234-123456789abc"}
		}
		m, ok := EncodeCompany(subject, d)
		if !ok {
			t.Fatal(subject)
		}
		ev, ok := DecodeCompany(subject, m.Data)
		if !ok || ev.Data != d {
			t.Fatal("roundtrip", subject)
		}
		for _, b := range [][]byte{bytes.Replace(m.Data, []byte(`"schema_version":1`), []byte(`"schema_version":2`), 1), bytes.Replace(m.Data, []byte(`"source":"`+ev.Source+`"`), []byte(`"source":"wrong"`), 1), append(append([]byte{}, m.Data...), []byte(`{}`)...), bytes.Repeat([]byte("x"), MaxPayload+1)} {
			if _, ok = DecodeCompany(subject, b); ok {
				t.Fatal("invalid accepted", subject)
			}
		}
		if _, ok = DecodeCompany(PlanetSectorSubject, m.Data); ok {
			t.Fatal("subject mismatch")
		}
		var raw map[string]any
		json.Unmarshal(m.Data, &raw)
		raw["data"].(map[string]any)["granted"] = true
		b, _ := json.Marshal(raw)
		if _, ok = DecodeCompany(subject, b); ok {
			t.Fatal("authority field accepted")
		}
	}
	d := VisitChange{UserID: "12", VisitID: "12345678-1234-4234-8234-123456789abc", Generation: "1", Epoch: "7", RuntimeFence: "3", SurfaceID: "9", Sector: 2}
	for _, subject := range VisitSubjects {
		m, ok := EncodeVisit(subject, d)
		if !ok {
			t.Fatal(subject)
		}
		e, ok := DecodeVisit(subject, m.Data)
		if !ok || e.Data != d {
			t.Fatal("visit roundtrip")
		}
		if _, ok = DecodeVisit(VisitEndedSubject+".other", m.Data); ok {
			t.Fatal("unknown subject")
		}
		if _, ok = DecodeCompany(subject, m.Data); ok {
			t.Fatal("wrong schema")
		}
	}
	d.UserID = "012"
	if _, ok := EncodeVisit(VisitStartedSubject, d); ok {
		t.Fatal("noncanonical bigint")
	}
}
