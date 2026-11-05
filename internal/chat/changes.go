package chat

import (
	"context"
)

// Changes returns metadata-only invalidation for an audience which is checked at
// delivery time. Private traffic never broadcasts conversation/message IDs.
// A 15-second overlap covers the bounded transaction deadline: sequence IDs
// are allocated before commit, so a plain high-water cursor can skip late commits.
func (s *Service) Changes(ctx context.Context, u Identity, after int64) (int64, bool, string, error) {
	tx, e := s.DB.Begin(ctx)
	if e != nil {
		return 0, false, "", e
	}
	defer tx.Rollback(context.Background())
	var latest int64
	var fingerprint string
	var stamp string
	if e = tx.QueryRow(ctx, `SELECT COALESCE(max(id),0) FROM chat_outbox`).Scan(&latest); e != nil {
		return 0, false, "", e
	}
	if e = tx.QueryRow(ctx, `SELECT chat_scope_stamp($1)`, u.ID).Scan(&stamp); e != nil {
		return 0, false, "", e
	}
	{
		e = tx.QueryRow(ctx, `SELECT COALESCE(md5(string_agg(o.id::text,',' ORDER BY o.id)),'') FROM chat_outbox o LEFT JOIN chat_channels c ON c.id=o.channel_id WHERE (o.id>$2 OR o.created_at>clock_timestamp()-interval '15 seconds') AND o.id<=$3 AND (o.target_user_id=$1 OR o.target_user_id IS NULL AND (c.kind='global' OR c.kind='corporation' AND c.company_id=(SELECT company_id FROM users WHERE id=$1) AND EXISTS(SELECT 1 FROM companies co WHERE co.id=c.company_id AND NOT co.chat_deleting) OR c.kind='private' AND EXISTS(SELECT 1 FROM chat_members m WHERE m.conversation_id=c.conversation_id AND m.user_id=$1 AND m.status='active') OR c.kind IN('planet','sector') AND EXISTS(SELECT 1 FROM chat_lock_planet_presence($1) p WHERE p.surface=c.surface_id AND (c.kind='planet' OR p.sector=c.sector_number)) OR c.id IS NULL AND o.event_kind='private.changed' AND (EXISTS(SELECT 1 FROM chat_members m WHERE m.conversation_id=o.reference_id AND m.user_id=$1 AND m.status='active') OR EXISTS(SELECT 1 FROM chat_invites i WHERE i.conversation_id=o.reference_id AND (i.sender_id=$1 OR i.recipient_id=$1) AND i.status='pending' AND i.expires_at>clock_timestamp()))))`, u.ID, after, latest).Scan(&fingerprint)
		if e != nil {
			return 0, false, "", e
		}
	}
	if e = tx.Commit(ctx); e != nil {
		return 0, false, "", e
	}
	return latest, false, stamp + ":" + fingerprint, nil
}
