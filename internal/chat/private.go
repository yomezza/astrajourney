package chat

import (
	"context"
	"github.com/jackc/pgx/v5"
	"strconv"
	"strings"
	"time"
)

func (s *Service) private(ctx context.Context, tx pgx.Tx, u Identity, cmd Command, in Input) (any, error) {
	if cmd.Type == "private.create" {
		if in.Kind != "dm" && in.Kind != "group" {
			return nil, failure("invalid_argument")
		}
		var target int64
		if in.Kind == "dm" {
			if e := tx.QueryRow(ctx, `SELECT id FROM users WHERE (username=$1 OR id::text=$1) AND NOT is_bot`, in.User).Scan(&target); e != nil || target == u.ID {
				return nil, failure("invalid_recipient")
			}
			low, high := min(u.ID, target), max(u.ID, target)
			if _, e := tx.Exec(ctx, `INSERT INTO chat_dm_pair_locks VALUES($1,$2) ON CONFLICT DO NOTHING`, low, high); e != nil {
				return nil, e
			}
			if _, e := tx.Exec(ctx, `SELECT low_user_id FROM chat_dm_pair_locks WHERE low_user_id=$1 AND high_user_id=$2 FOR UPDATE`, low, high); e != nil {
				return nil, e
			}
			var cooling bool
			if e := tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM chat_invite_attempts WHERE sender_id=$1 AND recipient_id=$2 AND dm_low_id=$3 AND dm_high_id=$4 AND rejected_at>clock_timestamp()-interval '24 hours')`, u.ID, target, low, high).Scan(&cooling); e != nil {
				return nil, e
			}
			if cooling {
				return nil, failure("invitation_cooldown")
			}
			var existing string
			e := tx.QueryRow(ctx, `SELECT conversation_id::text FROM chat_dm_pairs WHERE low_user_id=$1 AND high_user_id=$2`, low, high).Scan(&existing)
			if e == nil {
				c, e := s.conversation(ctx, tx, u, existing, true)
				if e != nil {
					return nil, e
				}
				if c.State == "pending" && c.Owner != u.ID {
					var invite string
					if e = tx.QueryRow(ctx, `SELECT id::text FROM chat_invites WHERE conversation_id=$1 AND recipient_id=$2 AND status='pending' AND expires_at>clock_timestamp()`, existing, u.ID).Scan(&invite); e != nil {
						return nil, failure("invitation_expired")
					}
					in.ID = invite
					in.Decision = "accept"
					return s.decide(ctx, tx, u, in)
				}
				return map[string]any{"conversation": existing}, nil
			}
			if e != pgx.ErrNoRows {
				return nil, e
			}
		}
		title := strings.TrimSpace(in.Title)
		if len([]rune(title)) > 100 || (in.Kind == "group" && title == "") {
			return nil, failure("invalid_argument")
		}
		id := UUID()
		if _, e := tx.Exec(ctx, `INSERT INTO chat_conversations(id,type,state,owner_id,title) VALUES($1,$2,'pending',$3,$4)`, id, in.Kind, u.ID, title); e != nil {
			return nil, e
		}
		if _, e := tx.Exec(ctx, `INSERT INTO chat_members(conversation_id,user_id,status) VALUES($1,$2,'active')`, id, u.ID); e != nil {
			return nil, e
		}
		if in.Kind == "dm" {
			if _, e := tx.Exec(ctx, `INSERT INTO chat_dm_pairs VALUES($1,$2,$3)`, min(u.ID, target), max(u.ID, target), id); e != nil {
				return nil, e
			}
			if e := s.invite(ctx, tx, u, Conversation{ID: id, Type: "dm", State: "pending", Owner: u.ID}, target); e != nil {
				return nil, e
			}
		}
		return map[string]any{"conversation": id}, nil
	}
	if cmd.Type == "private.decide" {
		return s.decide(ctx, tx, u, in)
	}
	c, e := s.conversation(ctx, tx, u, in.Target.Target, false)
	if e != nil {
		return nil, e
	}
	switch cmd.Type {
	case "private.invite":
		if c.Owner != u.ID {
			return nil, ErrDenied
		}
		var target int64
		if e = tx.QueryRow(ctx, `SELECT id FROM users WHERE (username=$1 OR id::text=$1) AND NOT is_bot`, in.User).Scan(&target); e != nil {
			return nil, failure("invalid_recipient")
		}
		if e = s.invite(ctx, tx, u, c, target); e != nil {
			return nil, e
		}
	case "private.rename":
		if c.Owner != u.ID || c.Type != "group" {
			return nil, ErrDenied
		}
		title, e := Body(in.Title, 100)
		if e != nil {
			return nil, e
		}
		_, e = tx.Exec(ctx, `UPDATE chat_conversations SET title=$2,revision=revision+1 WHERE id=$1`, c.ID, title)
		if e != nil {
			return nil, e
		}
		if e = s.system(ctx, tx, c, "renamed", u.ID); e != nil {
			return nil, e
		}
	case "private.owner":
		if c.Owner != u.ID || c.Type != "group" {
			return nil, ErrDenied
		}
		if in.Revision != c.Revision {
			return nil, failure("revision_conflict")
		}
		target, e := strconv.ParseInt(in.User, 10, 64)
		if e != nil || target == u.ID {
			return nil, ErrDenied
		}
		var member bool
		e = tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM chat_members WHERE conversation_id=$1 AND user_id=$2 AND status='active')`, c.ID, target).Scan(&member)
		if e != nil || !member {
			return nil, ErrDenied
		}
		_, e = tx.Exec(ctx, `UPDATE chat_conversations SET owner_id=$2,revision=revision+1 WHERE id=$1`, c.ID, target)
		if e != nil {
			return nil, e
		}
		if e = s.system(ctx, tx, c, "owner_changed", target); e != nil {
			return nil, e
		}
	case "private.leave", "private.kick", "private.delete":
		target := u.ID
		if cmd.Type != "private.leave" {
			if c.Owner != u.ID {
				return nil, ErrDenied
			}
			if cmd.Type == "private.delete" {
				if !in.Confirm {
					return nil, failure("confirmation_required")
				}
				return s.deleteConversation(ctx, tx, c, cmd.Request)
			}
			target, e = strconv.ParseInt(in.User, 10, 64)
			if e != nil || target == u.ID {
				return nil, ErrDenied
			}
		}
		var count int
		if e = tx.QueryRow(ctx, `SELECT count(*) FROM chat_members WHERE conversation_id=$1 AND status='active'`, c.ID).Scan(&count); e != nil {
			return nil, e
		}
		if c.Type == "dm" || count <= 2 {
			return s.deleteConversation(ctx, tx, c, cmd.Request)
		}
		if target == c.Owner {
			var next int64
			e := tx.QueryRow(ctx, `SELECT id FROM users WHERE (username=$1 OR id::text=$1) AND NOT is_bot`, in.User).Scan(&next)
			if e != nil || next == target {
				return nil, failure("owner_required")
			}
			var active bool
			e = tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM chat_members WHERE conversation_id=$1 AND user_id=$2 AND status='active')`, c.ID, next).Scan(&active)
			if e != nil || !active {
				return nil, ErrDenied
			}
			if _, e = tx.Exec(ctx, `UPDATE chat_conversations SET owner_id=$2,revision=revision+1 WHERE id=$1`, c.ID, next); e != nil {
				return nil, e
			}
		}
		status := "left"
		if cmd.Type == "private.kick" {
			status = "kicked"
		}
		tag, e := tx.Exec(ctx, `UPDATE chat_members SET status=$3,left_at=clock_timestamp(),unread_count=0 WHERE conversation_id=$1 AND user_id=$2 AND status='active'`, c.ID, target, status)
		if e != nil {
			return nil, e
		}
		if tag.RowsAffected() != 1 {
			return nil, ErrDenied
		}
		if e = event(ctx, tx, "membership.changed", "", target, c.ID); e != nil {
			return nil, e
		}
		if e = s.system(ctx, tx, c, status, target); e != nil {
			return nil, e
		}
	default:
		return nil, failure("unsupported_command")
	}
	if e = event(ctx, tx, "private.changed", "", nil, c.ID); e != nil {
		return nil, e
	}
	return map[string]any{"conversation": c.ID}, nil
}
func (s *Service) invite(ctx context.Context, tx pgx.Tx, u Identity, c Conversation, target int64) error {
	if target == u.ID {
		return failure("invalid_recipient")
	}
	if _, e := tx.Exec(ctx, `UPDATE chat_invites SET status='expired',decided_at=clock_timestamp() WHERE conversation_id=$1 AND recipient_id=$2 AND status='pending' AND expires_at<=clock_timestamp()`, c.ID, target); e != nil {
		return e
	}

	var member, pending bool
	var occupied int
	e := tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM chat_members WHERE conversation_id=$1 AND user_id=$2 AND status='active'),EXISTS(SELECT 1 FROM chat_invites WHERE conversation_id=$1 AND recipient_id=$2 AND status='pending' AND expires_at>clock_timestamp()),(SELECT count(*) FROM chat_members WHERE conversation_id=$1 AND status='active')+(SELECT count(*) FROM chat_invites WHERE conversation_id=$1 AND status='pending' AND expires_at>clock_timestamp())`, c.ID, target).Scan(&member, &pending, &occupied)
	if e != nil {
		return e
	}
	if member {
		return failure("already_member")
	}
	if pending {
		return failure("invitation_pending")
	}
	if occupied >= 50 {
		return failure("group_capacity")
	}
	if _, e = tx.Exec(ctx, `INSERT INTO chat_invite_rate_locks VALUES($1,$2) ON CONFLICT DO NOTHING`, u.ID, target); e != nil {
		return e
	}
	if _, e = tx.Exec(ctx, `SELECT sender_id FROM chat_invite_rate_locks WHERE sender_id=$1 AND recipient_id=$2 FOR UPDATE`, u.ID, target); e != nil {
		return e
	}
	var count int
	var cooldown bool
	e = tx.QueryRow(ctx, `SELECT count(*) FILTER(WHERE attempted_at>clock_timestamp()-interval '24 hours'),COALESCE(bool_or(rejected_at>clock_timestamp()-interval '24 hours' AND (conversation_id=$3 OR ($4 AND dm_low_id=$5 AND dm_high_id=$6))),false) FROM chat_invite_attempts WHERE sender_id=$1 AND recipient_id=$2`, u.ID, target, c.ID, c.Type == "dm", min(u.ID, target), max(u.ID, target)).Scan(&count, &cooldown)
	if e != nil {
		return e
	}
	if cooldown {
		return failure("invitation_cooldown")
	}
	if count >= 20 {
		return failure("rate_limited")
	}
	id := UUID()
	if _, e = tx.Exec(ctx, `INSERT INTO chat_invites(id,conversation_id,sender_id,recipient_id,status,expires_at) VALUES($1,$2,$3,$4,'pending',clock_timestamp()+interval '168 hours')`, id, c.ID, u.ID, target); e != nil {
		return e
	}
	var low, high *int64
	if c.Type == "dm" {
		a, b := min(u.ID, target), max(u.ID, target)
		low, high = &a, &b
	}
	if _, e = tx.Exec(ctx, `INSERT INTO chat_invite_attempts(id,sender_id,recipient_id,conversation_id,dm_low_id,dm_high_id,status) VALUES($1,$2,$3,$4,$5,$6,'pending')`, id, u.ID, target, c.ID, low, high); e != nil {
		return e
	}
	if _, e = tx.Exec(ctx, `INSERT INTO chat_notifications(id,user_id,type,ref_id) VALUES($1,$2,'invite',$3)`, UUID(), target, id); e != nil {
		return e
	}
	return event(ctx, tx, "invite.changed", "", target, id)
}
func (s *Service) decide(ctx context.Context, tx pgx.Tx, u Identity, in Input) (any, error) {
	var conv string
	if e := tx.QueryRow(ctx, `SELECT conversation_id::text FROM chat_invites WHERE id=$1 AND recipient_id=$2`, in.ID, u.ID).Scan(&conv); e != nil {
		return nil, ErrDenied
	}
	c, e := s.conversation(ctx, tx, u, conv, true)
	if e != nil {
		return nil, e
	}
	var status string
	var live bool
	var sender int64
	e = tx.QueryRow(ctx, `SELECT status,expires_at>clock_timestamp(),sender_id FROM chat_invites WHERE id=$1 AND recipient_id=$2 FOR UPDATE`, in.ID, u.ID).Scan(&status, &live, &sender)
	if e != nil {
		return nil, ErrDenied
	}
	if status != "pending" || !live {
		return nil, failure("invitation_expired")
	}
	if in.Decision != "accept" && in.Decision != "reject" {
		return nil, failure("invalid_argument")
	}
	status = "rejected"
	if in.Decision == "accept" {
		status = "accepted"
	}
	if _, e = tx.Exec(ctx, `UPDATE chat_invites SET status=$2,decided_at=clock_timestamp() WHERE id=$1`, in.ID, status); e != nil {
		return nil, e
	}
	if _, e = tx.Exec(ctx, `UPDATE chat_invite_attempts SET status=$2,rejected_at=CASE WHEN $2='rejected' THEN clock_timestamp() ELSE NULL END WHERE id=$1`, in.ID, status); e != nil {
		return nil, e
	}
	if status == "accepted" {
		var count int
		if e = tx.QueryRow(ctx, `SELECT count(*) FROM chat_members WHERE conversation_id=$1 AND status='active'`, conv).Scan(&count); e != nil {
			return nil, e
		}
		if count >= 50 {
			return nil, failure("group_capacity")
		}
		if c.Type == "dm" && c.State == "active" {
			if _, e = tx.Exec(ctx, `DELETE FROM chat_dm_pairs WHERE conversation_id=$1`, conv); e != nil {
				return nil, e
			}
			c.Type = "group"
		}
		if _, e = tx.Exec(ctx, `INSERT INTO chat_channels(id,kind,conversation_id) VALUES($1,'private',$2) ON CONFLICT DO NOTHING`, UUID(), conv); e != nil {
			return nil, e
		}
		if _, e = tx.Exec(ctx, `INSERT INTO chat_members(conversation_id,user_id,status,join_floor_seq) VALUES($1,$2,'active',COALESCE((SELECT next_seq FROM chat_channels WHERE conversation_id=$1),0)) ON CONFLICT(conversation_id,user_id) DO UPDATE SET status='active',joined_at=clock_timestamp(),left_at=NULL,join_floor_seq=EXCLUDED.join_floor_seq,unread_count=0`, conv, u.ID); e != nil {
			return nil, e
		}
		if _, e = tx.Exec(ctx, `UPDATE chat_conversations SET state='active',type=$2,activated_at=COALESCE(activated_at,clock_timestamp()),revision=revision+1 WHERE id=$1`, conv, c.Type); e != nil {
			return nil, e
		}
		if e = s.system(ctx, tx, c, "joined", u.ID); e != nil {
			return nil, e
		}
	} else if c.State == "pending" {
		var pending bool
		if e = tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM chat_invites WHERE conversation_id=$1 AND status='pending' AND expires_at>clock_timestamp())`, conv).Scan(&pending); e != nil {
			return nil, e
		}
		if !pending {
			return s.deleteConversation(ctx, tx, c, UUID())
		}
	}
	if _, e = tx.Exec(ctx, `INSERT INTO chat_notifications(id,user_id,type,ref_id) VALUES($1,$2,'invite_decided',$3) ON CONFLICT DO NOTHING`, UUID(), sender, in.ID); e != nil {
		return nil, e
	}
	if e = event(ctx, tx, "private.changed", "", nil, conv); e != nil {
		return nil, e
	}
	return map[string]any{"conversation": conv, "status": status}, nil
}
func (s *Service) deleteConversation(ctx context.Context, tx pgx.Tx, c Conversation, operation string) (any, error) {
	_, e := tx.Exec(ctx, `UPDATE chat_conversations SET state='deleting',delete_started_at=clock_timestamp(),revision=revision+1 WHERE id=$1`, c.ID)
	if e != nil {
		return nil, e
	}
	if _, e = tx.Exec(ctx, `UPDATE chat_invites SET status='cancelled',decided_at=clock_timestamp() WHERE conversation_id=$1 AND status='pending'`, c.ID); e != nil {
		return nil, e
	}
	_, e = tx.Exec(ctx, `INSERT INTO chat_cleanup_jobs(id,kind,scope_id,operation_id) VALUES($1,'group',$2,$3) ON CONFLICT DO NOTHING`, UUID(), c.ID, operation)
	if e != nil {
		return nil, e
	}
	if e = event(ctx, tx, "private.changed", "", nil, c.ID); e != nil {
		return nil, e
	}
	return map[string]any{"conversation": c.ID, "status": "deleting"}, nil
}
func (s *Service) system(ctx context.Context, tx pgx.Tx, c Conversation, code string, subject int64) error {
	var channel string
	var seq int64
	var at time.Time
	e := tx.QueryRow(ctx, `UPDATE chat_channels SET next_seq=next_seq+1 WHERE conversation_id=$1 RETURNING id::text,next_seq,clock_timestamp()`, c.ID).Scan(&channel, &seq, &at)
	if e == pgx.ErrNoRows {
		return nil
	}
	if e != nil {
		return e
	}
	id := UUID()
	end := at.Add(180 * 24 * time.Hour)
	if _, e = tx.Exec(ctx, `INSERT INTO chat_message_locator(id,kind,sent_at,channel_id,seq,author_id,expires_at) VALUES($1,'private',$2,$3,$4,$5,$6)`, id, at, channel, seq, subject, end); e != nil {
		return e
	}
	_, e = tx.Exec(ctx, `INSERT INTO chat_messages(kind,sent_at,id,expires_at,channel_id,seq,author_id,body,message_type,system_code,system_data) VALUES('private',$1,$2,$3,$4,$5,$6,'','system',$7,jsonb_build_object('user',$6::bigint::text))`, at, id, end, channel, seq, subject, code)
	return e
}
