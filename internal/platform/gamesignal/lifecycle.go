package gamesignal

import (
	"bytes"
	"encoding/json"
	"io"
	"time"

	"github.com/yomezza/cosmosimulator/internal/platform/identifiers"
)

const (
	MembershipSubject        = "cosmos.v1.signal.cosmic.company.membership.changed"
	RoleSubject              = "cosmos.v1.signal.cosmic.company.role.changed"
	PermissionsSubject       = "cosmos.v1.signal.cosmic.company.permissions.changed"
	ModeratorSubject         = "cosmos.v1.signal.cosmic.moderator.changed"
	DeletionSubject          = "cosmos.v1.signal.cosmic.company.deletion.requested"
	CleanupSubject           = "cosmos.v1.signal.chat.company.cleanup.completed"
	VisitStartedSubject      = "cosmos.v1.signal.planet.visit.started"
	VisitResumedSubject      = "cosmos.v1.signal.planet.visit.resumed"
	VisitDisconnectedSubject = "cosmos.v1.signal.planet.visit.disconnected"
	VisitEndedSubject        = "cosmos.v1.signal.planet.visit.ended"
)

var CosmicSubjects = []string{MembershipSubject, RoleSubject, PermissionsSubject, ModeratorSubject, DeletionSubject}
var VisitSubjects = []string{VisitStartedSubject, VisitResumedSubject, VisitDisconnectedSubject, VisitEndedSubject}

type Message struct {
	Subject string
	Data    []byte
}

// Emit must only enqueue bounded metadata; it is called AFTER a successful COMMIT.
type Emit func(Message)

type CompanyChange struct {
	UserID    string `json:"user_id,omitempty"`
	CompanyID string `json:"company_id,omitempty"`
	Fence     string `json:"company_fence_revision,omitempty"`
	Operation string `json:"operation_id,omitempty"`
}
type VisitChange struct {
	UserID       string `json:"user_id"`
	VisitID      string `json:"visit_id"`
	Generation   string `json:"generation"`
	Epoch        string `json:"connection_epoch"`
	RuntimeFence string `json:"runtime_fence"`
	SurfaceID    string `json:"surface_id"`
	Sector       int    `json:"sector_number"`
}
type Envelope[T any] struct {
	Version  int       `json:"schema_version"`
	SignalID string    `json:"signal_id"`
	Source   string    `json:"source"`
	Kind     string    `json:"kind"`
	At       time.Time `json:"occurred_at"`
	Data     T         `json:"data"`
}

func strict(b []byte, v any) bool {
	if len(b) == 0 || len(b) > MaxPayload {
		return false
	}
	d := json.NewDecoder(bytes.NewReader(b))
	d.DisallowUnknownFields()
	return d.Decode(v) == nil && d.Decode(new(any)) == io.EOF
}
func header[T any](s Envelope[T], source, kind string) bool {
	_, offset := s.At.Zone()
	return s.Version == 1 && identifiers.ValidUUID(s.SignalID) && s.Source == source && s.Kind == kind && !s.At.IsZero() && offset == 0
}
func companyKind(subject string) (string, string) {
	switch subject {
	case MembershipSubject:
		return "cosmic", "cosmic.company.membership.changed"
	case RoleSubject:
		return "cosmic", "cosmic.company.role.changed"
	case PermissionsSubject:
		return "cosmic", "cosmic.company.permissions.changed"
	case ModeratorSubject:
		return "cosmic", "cosmic.moderator.changed"
	case DeletionSubject:
		return "cosmic", "cosmic.company.deletion.requested"
	case CleanupSubject:
		return "chat", "chat.company.cleanup.completed"
	}
	return "", ""
}
func validCompany(subject string, d CompanyChange) bool {
	switch subject {
	case MembershipSubject, ModeratorSubject:
		return positive(d.UserID) && d.CompanyID == "" && d.Fence == "" && d.Operation == ""
	case RoleSubject:
		return positive(d.UserID) && positive(d.CompanyID) && positive(d.Fence) && d.Operation == ""
	case PermissionsSubject:
		return d.UserID == "" && positive(d.CompanyID) && positive(d.Fence) && d.Operation == ""
	case DeletionSubject, CleanupSubject:
		return d.UserID == "" && positive(d.CompanyID) && d.Fence == "" && identifiers.ValidUUID(d.Operation)
	}
	return false
}
func EncodeCompany(subject string, d CompanyChange) (Message, bool) {
	source, kind := companyKind(subject)
	id, e := identifiers.UUID()
	if e != nil || !validCompany(subject, d) {
		return Message{}, false
	}
	b, e := json.Marshal(Envelope[CompanyChange]{1, id, source, kind, time.Now().UTC(), d})
	return Message{subject, b}, e == nil && len(b) <= MaxPayload
}
func DecodeCompany(subject string, b []byte) (Envelope[CompanyChange], bool) {
	var s Envelope[CompanyChange]
	source, kind := companyKind(subject)
	ok := decodeCompanyInto(subject, b, source, kind, &s)
	return s, ok
}
func decodeCompanyInto(subject string, b []byte, source, kind string, s *Envelope[CompanyChange]) bool {
	return source != "" && strict(b, s) && header(*s, source, kind) && validCompany(subject, s.Data)
}
func visitKind(subject string) string {
	switch subject {
	case VisitStartedSubject:
		return "planet.visit.started"
	case VisitResumedSubject:
		return "planet.visit.resumed"
	case VisitDisconnectedSubject:
		return "planet.visit.disconnected"
	case VisitEndedSubject:
		return "planet.visit.ended"
	}
	return ""
}
func validVisit(d VisitChange) bool {
	return positive(d.UserID) && identifiers.ValidUUID(d.VisitID) && positive(d.Generation) && positive(d.Epoch) && positive(d.RuntimeFence) && positive(d.SurfaceID) && d.Sector > 0 && d.Sector <= 2147483647
}
func EncodeVisit(subject string, d VisitChange) (Message, bool) {
	kind := visitKind(subject)
	id, e := identifiers.UUID()
	if kind == "" || e != nil || !validVisit(d) {
		return Message{}, false
	}
	b, e := json.Marshal(Envelope[VisitChange]{1, id, "planet", kind, time.Now().UTC(), d})
	return Message{subject, b}, e == nil && len(b) <= MaxPayload
}
func DecodeVisit(subject string, b []byte) (Envelope[VisitChange], bool) {
	var s Envelope[VisitChange]
	kind := visitKind(subject)
	ok := kind != "" && strict(b, &s) && header(s, "planet", kind) && validVisit(s.Data)
	return s, ok
}
