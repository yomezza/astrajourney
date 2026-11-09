package chat

import (
	"context"

	"github.com/jackc/pgx/v5"
)

// ResolveRecipients checks current SQL authority, not connection-cached scopes.
// Each audience has a bounded query; only generic sync is returned to transport.
// Its read-time race with revoke is safe: no private identifiers/bodies are sent,
// and every history/mutation request retains its existing authorization.
func (s *Service) ResolveRecipients(ctx context.Context, events []DispatchEvent, connections []DispatchConnection) (map[int64]bool, error) {
	allowed := map[int64]bool{}
	if len(events) == 0 || len(connections) == 0 {
		return allowed, nil
	}
	if len(events) > 1000 || len(connections) > 256 {
		return nil, failure("invalid_argument")
	}
	tx, err := s.DB.Begin(ctx)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback(context.Background())
	if _, err = tx.Exec(ctx, `SET LOCAL lock_timeout='1s';SET LOCAL statement_timeout='2s'`); err != nil {
		return nil, err
	}
	ids := []int64{}
	sessions := []string{}
	leases := []string{}
	generations := []int64{}
	for _, c := range connections {
		ids = append(ids, c.Identity.ID)
		sessions = append(sessions, c.Identity.Session)
		leases = append(leases, c.Lease.Connection)
		generations = append(generations, c.Lease.Generation)
	}
	valid, err := dispatchIDs(ctx, tx, `SELECT i.user_id FROM unnest($1::bigint[],$2::text[],$3::uuid[],$4::bigint[]) i(user_id,token,connection,generation) JOIN chat_ws_leases l ON l.user_id=i.user_id AND l.connection_id=i.connection AND l.generation=i.generation AND l.expires_at>clock_timestamp() JOIN LATERAL chat_validate_session(i.token) u ON u.id=i.user_id`, ids, sessions, leases, generations)
	if err != nil {
		return nil, err
	}
	validSet := map[int64]bool{}
	for _, id := range valid {
		validSet[id] = true
	}
	channels := []string{}
	for _, e := range events {
		if e.Target != nil {
			if validSet[*e.Target] {
				allowed[*e.Target] = true
			}
			continue
		}
		if e.Channel != nil {
			channels = append(channels, *e.Channel)
		}
	}
	rows, err := tx.Query(ctx, `SELECT id::text,kind FROM chat_channels WHERE id=ANY($1::uuid[])`, channels)
	if err != nil {
		return nil, err
	}
	kinds := map[string]string{}
	groups := map[string][]string{}
	for rows.Next() {
		var id, kind string
		if err = rows.Scan(&id, &kind); err != nil {
			rows.Close()
			return nil, err
		}
		kinds[id] = kind
		groups[kind] = append(groups[kind], id)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return nil, err
	}
	if len(groups["global"]) > 0 {
		for _, id := range valid {
			allowed[id] = true
		}
		return allowed, tx.Commit(ctx)
	}
	add := func(query string, args ...any) error {
		found, e := dispatchIDs(ctx, tx, query, args...)
		for _, id := range found {
			allowed[id] = true
		}
		return e
	}
	if ch := groups["corporation"]; len(ch) > 0 {
		err = add(`SELECT DISTINCT u.id FROM users u JOIN companies co ON co.id=u.company_id AND NOT co.chat_deleting JOIN LATERAL chat_lock_company_access(u.id,co.id) fence ON true JOIN chat_channels c ON c.company_id=co.id AND c.kind='corporation' WHERE u.id=ANY($1::bigint[]) AND c.id=ANY($2::uuid[])`, valid, ch)
		if err != nil {
			return nil, err
		}
	}
	if ch := groups["private"]; len(ch) > 0 {
		err = add(`SELECT DISTINCT m.user_id FROM chat_members m JOIN chat_channels c ON c.conversation_id=m.conversation_id WHERE m.user_id=ANY($1::bigint[]) AND m.status='active' AND c.id=ANY($2::uuid[])`, valid, ch)
		if err != nil {
			return nil, err
		}
	}
	refs := []string{}
	for _, e := range events {
		if e.Target == nil && e.Kind == "private.changed" && e.Reference != nil && (e.Channel == nil || kinds[*e.Channel] == "") {
			refs = append(refs, *e.Reference)
		}
	}
	if len(refs) > 0 {
		err = add(`SELECT DISTINCT user_id FROM (SELECT user_id FROM chat_members WHERE conversation_id=ANY($2::uuid[]) AND status='active' UNION SELECT sender_id FROM chat_invites WHERE conversation_id=ANY($2::uuid[]) AND status='pending' AND expires_at>clock_timestamp() UNION SELECT recipient_id FROM chat_invites WHERE conversation_id=ANY($2::uuid[]) AND status='pending' AND expires_at>clock_timestamp()) candidates WHERE user_id=ANY($1::bigint[])`, valid, refs)
		if err != nil {
			return nil, err
		}
	}
	spatial := append(groups["planet"], groups["sector"]...)
	if len(spatial) > 0 && s.spatialAvailable() {
		err = add(`SELECT DISTINCT actor.id FROM unnest($1::bigint[]) actor(id) JOIN LATERAL chat_lock_planet_presence(actor.id) p ON true JOIN chat_channels c ON c.surface_id=p.surface AND (c.kind='planet' OR c.kind='sector' AND c.sector_number=p.sector) WHERE c.id=ANY($2::uuid[])`, valid, spatial)
		if err != nil {
			return nil, err
		}
	}
	return allowed, tx.Commit(ctx)
}

func dispatchIDs(ctx context.Context, tx pgx.Tx, sql string, args ...any) ([]int64, error) {
	rows, err := tx.Query(ctx, sql, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	ids := []int64{}
	for rows.Next() {
		var id int64
		if err = rows.Scan(&id); err != nil {
			return nil, err
		}
		ids = append(ids, id)
	}
	return ids, rows.Err()
}
