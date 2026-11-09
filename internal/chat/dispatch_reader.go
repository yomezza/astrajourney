package chat

import (
	"context"
	"time"
)

// DispatchEvent contains metadata only. Message bodies never enter the dispatcher.
type DispatchEvent struct {
	ID                 int64
	Kind               string
	Channel, Reference *string
	Target             *int64
	Created            time.Time
}
type DispatchWindow struct {
	Max    int64
	Cutoff time.Time
}
type DispatchConnection struct {
	Identity Identity
	Lease    Lease
}

// The cutoff uses the database clock, and remains fixed across all pages.
func (s *Service) OutboxWindow(ctx context.Context, overlap time.Duration) (DispatchWindow, error) {
	var w DispatchWindow
	err := s.DB.QueryRow(ctx, `SELECT COALESCE(max(id),0),clock_timestamp()-make_interval(secs=>$1) FROM chat_outbox`, overlap.Seconds()).Scan(&w.Max, &w.Cutoff)
	return w, err
}
func (s *Service) OutboxPage(ctx context.Context, window DispatchWindow, highwater, after int64, limit int) ([]DispatchEvent, error) {
	rows, err := s.DB.Query(ctx, `SELECT id,event_kind,channel_id::text,target_user_id,reference_id::text,created_at FROM chat_outbox WHERE id>$1 AND id<=$2 AND (id>$3 OR created_at>=$4) ORDER BY id LIMIT $5`, after, window.Max, highwater, window.Cutoff, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []DispatchEvent{}
	for rows.Next() {
		var e DispatchEvent
		if err = rows.Scan(&e.ID, &e.Kind, &e.Channel, &e.Target, &e.Reference, &e.Created); err != nil {
			return nil, err
		}
		out = append(out, e)
	}
	return out, rows.Err()
}

// ScopeStamp is separate from outbox polling: role/presence changes need not
// insert an outbox row. It also rejects a replaced/expired WS generation.
func (s *Service) ScopeStamp(ctx context.Context, l Lease) (string, error) {
	s.scopeChecks.Add(1)
	var stamp string
	err := s.DB.QueryRow(ctx, `SELECT chat_scope_stamp($1) FROM chat_ws_leases WHERE user_id=$1 AND connection_id=$2 AND generation=$3 AND expires_at>clock_timestamp()`, l.User, l.Connection, l.Generation).Scan(&stamp)
	return stamp, err
}
