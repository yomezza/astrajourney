package chat

import (
	"context"
	"github.com/jackc/pgx/v5"
	"time"
)

func (s *Service) Maintain(ctx context.Context) error {
	tx, e := s.DB.Begin(ctx)
	if e != nil {
		return e
	}
	defer tx.Rollback(context.Background())
	if _, e = tx.Exec(ctx, `SET LOCAL lock_timeout='1s';SET LOCAL statement_timeout='8s'`); e != nil {
		return e
	}
	var locked bool
	if e = tx.QueryRow(ctx, `SELECT pg_try_advisory_xact_lock(hashtextextended(current_schema()||':chat-maintenance',784241))`).Scan(&locked); e != nil || !locked {
		return e
	}
	// Expiration does not remove permanent saved copies.
	started := time.Now()
	for {
		tag, err := tx.Exec(ctx, `WITH batch AS(SELECT id,kind,sent_at FROM chat_message_locator WHERE expires_at<=clock_timestamp() ORDER BY expires_at,id LIMIT 1000 FOR UPDATE SKIP LOCKED),removed AS(DELETE FROM chat_messages m USING batch b WHERE m.kind=b.kind AND m.sent_at=b.sent_at AND m.id=b.id) DELETE FROM chat_message_locator l USING batch b WHERE l.id=b.id`)
		if err != nil {
			return err
		}
		if tag.RowsAffected() < 1000 || time.Since(started) > 2*time.Second {
			break
		}
	}
	if _, e = tx.Exec(ctx, `UPDATE chat_invites SET status='expired',decided_at=clock_timestamp() WHERE status='pending' AND expires_at<=clock_timestamp(); UPDATE chat_invite_attempts a SET status='expired' FROM chat_invites i WHERE i.id=a.id AND i.status='expired' AND a.status='pending'; INSERT INTO chat_cleanup_jobs(id,kind,scope_id,operation_id) SELECT gen_random_uuid(),'group',c.id::text,gen_random_uuid() FROM chat_conversations c WHERE c.state='pending' AND NOT EXISTS(SELECT 1 FROM chat_invites i WHERE i.conversation_id=c.id AND i.status='pending') AND EXISTS(SELECT 1 FROM chat_invites i WHERE i.conversation_id=c.id) ON CONFLICT DO NOTHING; UPDATE chat_conversations c SET state='deleting' WHERE c.state='pending' AND EXISTS(SELECT 1 FROM chat_cleanup_jobs j WHERE j.kind='group' AND j.scope_id=c.id::text AND j.phase<>'completed')`); e != nil {
		return e
	}
	// Cosmic owns requests; Chat writes only its data and acknowledgements.
	if _, e = tx.Exec(ctx, `INSERT INTO chat_cleanup_jobs(id,kind,scope_id,operation_id) SELECT gen_random_uuid(),'company',company_id::text,operation_id FROM chat_company_deletion_requests ON CONFLICT DO NOTHING; INSERT INTO chat_cleanup_jobs(id,kind,scope_id,operation_id) SELECT gen_random_uuid(),'surface',surface_id::text,operation_id FROM chat_surface_cleanup_requests ON CONFLICT DO NOTHING`); e != nil {
		return e
	}
	var job, kind, scope, op string
	e = tx.QueryRow(ctx, `SELECT id::text,kind,scope_id,operation_id::text FROM chat_cleanup_jobs WHERE phase<>'completed' ORDER BY created_at LIMIT 1 FOR UPDATE SKIP LOCKED`).Scan(&job, &kind, &scope, &op)
	if e != nil && e != pgx.ErrNoRows {
		return e
	}
	if e == nil {
		channels, err := list(ctx, tx, `SELECT id::text FROM chat_channels WHERE ($1='group' AND conversation_id::text=$2) OR ($1='company' AND company_id::text=$2) OR ($1='surface' AND surface_id::text=$2) ORDER BY id`, kind, scope)
		if err != nil {
			return err
		}
		ids := []string{}
		for _, c := range channels {
			ids = append(ids, c["id"].(string))
		}
		if kind == "group" {
			if _, e = tx.Exec(ctx, `SELECT id FROM chat_conversations WHERE id::text=$1 FOR UPDATE`, scope); e != nil {
				return e
			}
		}
		messages, err := list(ctx, tx, `SELECT id::text FROM chat_message_locator WHERE channel_id=ANY($1::uuid[]) ORDER BY id LIMIT 500 FOR UPDATE`, ids)
		if err != nil {
			return err
		}
		deleted := []string{}
		for _, m := range messages {
			deleted = append(deleted, m["id"].(string))
		}
		if len(deleted) > 0 {
			if _, e = tx.Exec(ctx, `DELETE FROM chat_messages WHERE id=ANY($1::uuid[])`, deleted); e != nil {
				return e
			}
			if _, e = tx.Exec(ctx, `DELETE FROM chat_message_locator WHERE id=ANY($1::uuid[])`, deleted); e != nil {
				return e
			}
		} else {
			for _, q := range []string{`DELETE FROM chat_notifications WHERE channel_id=ANY($1::uuid[])`, `DELETE FROM chat_read_marks WHERE channel_id=ANY($1::uuid[])`, `DELETE FROM chat_channels WHERE id=ANY($1::uuid[])`} {
				if _, e = tx.Exec(ctx, q, ids); e != nil {
					return e
				}
			}
			switch kind {
			case "group":
				for _, q := range []string{`DELETE FROM chat_notifications WHERE ref_id IN(SELECT id FROM chat_invites WHERE conversation_id::text=$1)`, `DELETE FROM chat_invites WHERE conversation_id::text=$1`, `DELETE FROM chat_members WHERE conversation_id::text=$1`, `DELETE FROM chat_dm_pairs WHERE conversation_id::text=$1`, `DELETE FROM chat_conversations WHERE id::text=$1`} {
					if _, e = tx.Exec(ctx, q, scope); e != nil {
						return e
					}
				}
			case "company":
				if _, e = tx.Exec(ctx, `DELETE FROM chat_corporation_mute_state WHERE company_id::text=$1`, scope); e != nil {
					return e
				}
				if _, e = tx.Exec(ctx, `INSERT INTO chat_company_cleanup_receipts(company_id,operation_id) VALUES($1::text::bigint,$2) ON CONFLICT DO NOTHING`, scope, op); e != nil {
					return e
				}
			case "surface":
				if _, e = tx.Exec(ctx, `INSERT INTO chat_surface_cleanup_receipts(surface_id,operation_id) VALUES($1::text::bigint,$2) ON CONFLICT DO NOTHING`, scope, op); e != nil {
					return e
				}
			}
			if e = event(ctx, tx, "cleanup.completed", "", nil, ""); e != nil {
				return e
			}
			if _, e = tx.Exec(ctx, `UPDATE chat_cleanup_jobs SET phase='completed',updated_at=clock_timestamp() WHERE id=$1`, job); e != nil {
				return e
			}
		}
	}
	// Bound expired metadata retention too. No ordinary message bodies in these rows.
	for _, q := range []string{`DELETE FROM chat_audit WHERE created_at<clock_timestamp()-interval '180 days'`, `DELETE FROM chat_reports WHERE created_at<clock_timestamp()-interval '180 days'`, `DELETE FROM chat_rate_events WHERE sent_at<clock_timestamp()-interval '10 minutes'`, `DELETE FROM chat_invite_attempts WHERE attempted_at<clock_timestamp()-interval '8 days'`, `DELETE FROM chat_notifications WHERE expires_at<=clock_timestamp()`, `DELETE FROM chat_operations WHERE (user_id,request_id) IN(SELECT user_id,request_id FROM chat_operations WHERE expires_at<=clock_timestamp() LIMIT 500)`, `DELETE FROM chat_outbox WHERE created_at<clock_timestamp()-interval '1 day'`} {
		if _, e = tx.Exec(ctx, q); e != nil {
			return e
		}
	}
	if e = s.unreadAll(ctx, tx); e != nil {
		return e
	}
	return tx.Commit(ctx)
}
func (s *Service) unreadAll(ctx context.Context, tx pgx.Tx) error {
	rows, e := tx.Query(ctx, `SELECT id::text FROM chat_conversations WHERE state='active' ORDER BY id LIMIT 1000`)
	if e != nil {
		return e
	}
	ids := []string{}
	for rows.Next() {
		var id string
		if e = rows.Scan(&id); e != nil {
			rows.Close()
			return e
		}
		ids = append(ids, id)
	}
	e = rows.Err()
	rows.Close()
	if e != nil {
		return e
	}
	for _, id := range ids {
		if e = s.unread(ctx, tx, id); e != nil {
			return e
		}
	}
	return nil
}
