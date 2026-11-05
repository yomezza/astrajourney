package chat

import (
	"context"
	"encoding/json"
	"github.com/jackc/pgx/v5"
	"github.com/yomezza/cosmosimulator/internal/platform/identifiers"
	"strconv"
	"strings"
)

func (s *Service) mutate(ctx context.Context, tx pgx.Tx, u Identity, cmd Command, in Input) (any, error) {
	if strings.HasPrefix(cmd.Type, "private.") {
		return s.private(ctx, tx, u, cmd, in)
	}
	switch cmd.Type {
	case "message.send":
		return s.send(ctx, tx, u, cmd, in)
	case "message.delete", "favorite.copy", "report.create":
		return s.sourceAction(ctx, tx, u, cmd, in)
	case "read":
		c, e := s.channel(ctx, tx, u, in.Target, false)
		if e != nil {
			return nil, e
		}
		if len(in.IDs) > 100 {
			return nil, failure("invalid_argument")
		}
		changed := false
		if c.Kind == "private" {
			for _, id := range in.IDs {
				if !identifiers.ValidUUID(id) {
					return nil, failure("invalid_argument")
				}
				tag, err := tx.Exec(ctx, `INSERT INTO chat_read_receipts(user_id,message_id) SELECT $1,id FROM chat_message_locator WHERE id=$2 AND channel_id=$3 AND expires_at>clock_timestamp() ON CONFLICT DO NOTHING`, u.ID, id, c.ID)
				if err != nil {
					return nil, err
				}
				if tag.RowsAffected() > 0 {
					changed = true
				}
			}
			if e = s.unread(ctx, tx, *c.Conversation); e != nil {
				return nil, e
			}
		} else {
			if in.Seen < 0 || in.Seen > c.Next {
				return nil, failure("invalid_argument")
			}
			if _, e = tx.Exec(ctx, `INSERT INTO chat_read_marks(user_id,channel_id,last_seen_seq) VALUES($1,$2,$3) ON CONFLICT(user_id,channel_id) DO UPDATE SET last_seen_seq=GREATEST(chat_read_marks.last_seen_seq,EXCLUDED.last_seen_seq)`, u.ID, c.ID, in.Seen); e != nil {
				return nil, e
			}
		}
		if changed {
			if e = event(ctx, tx, "read.changed", c.ID, nil, ""); e != nil {
				return nil, e
			}
		}
		return map[string]any{"status": "read"}, nil
	case "favorite.note", "favorite.edit", "favorite.move", "favorite.delete":
		return s.favorite(ctx, tx, u, cmd, in)
	case "folder.create", "folder.rename", "folder.delete":
		if cmd.Type == "folder.delete" {
			var folderID string
			if e := tx.QueryRow(ctx, `SELECT id::text FROM chat_folders WHERE id=$1 AND owner_id=$2 FOR UPDATE`, in.ID, u.ID).Scan(&folderID); e != nil {
				return nil, ErrDenied
			}
			if _, e := tx.Exec(ctx, `UPDATE chat_saved_items SET folder_id=NULL WHERE owner_id=$1 AND folder_id=$2`, u.ID, in.ID); e != nil {
				return nil, e
			}
			if _, e := tx.Exec(ctx, `DELETE FROM chat_folders WHERE id=$1 AND owner_id=$2`, in.ID, u.ID); e != nil {
				return nil, e
			}
			return map[string]any{"status": "deleted"}, nil
		}
		name, e := Body(strings.TrimSpace(in.Name), 100)
		if e != nil {
			return nil, e
		}
		id := in.ID
		if cmd.Type == "folder.create" {
			id = UUID()
			_, e = tx.Exec(ctx, `INSERT INTO chat_folders(id,owner_id,name) VALUES($1,$2,$3)`, id, u.ID, name)
		} else {
			tag, err := tx.Exec(ctx, `UPDATE chat_folders SET name=$3 WHERE id=$1 AND owner_id=$2`, id, u.ID, name)
			e = err
			if e == nil && tag.RowsAffected() != 1 {
				return nil, ErrDenied
			}
		}
		return map[string]any{"id": id}, e
	case "settings":
		var v map[string]any
		if len(in.Settings) > 4096 || json.Unmarshal(in.Settings, &v) != nil || v == nil {
			return nil, failure("invalid_argument")
		}
		for k, val := range v {
			switch k {
			case "opacity":
				n, ok := val.(float64)
				if !ok || n < 40 || n > 100 || float64(int(n)) != n || int(n)%5 != 0 {
					return nil, failure("invalid_argument")
				}
			case "docked", "sounds":
				if _, ok := val.(bool); !ok {
					return nil, failure("invalid_argument")
				}
			case "tab":
				tab, ok := val.(string)
				if !ok || (tab != "favorites" && tab != "global" && tab != "planet" && tab != "sector" && tab != "corporation" && tab != "private") {
					return nil, failure("invalid_argument")
				}
			default:
				return nil, failure("invalid_argument")
			}
		}
		_, e := tx.Exec(ctx, `INSERT INTO chat_user_settings(user_id,settings) VALUES($1,$2) ON CONFLICT(user_id) DO UPDATE SET settings=EXCLUDED.settings,updated_at=clock_timestamp()`, u.ID, in.Settings)
		return map[string]any{"status": "saved"}, e
	case "mute", "unmute":
		return s.moderate(ctx, tx, u, cmd, in)
	case "report.resolve":
		if u.Role != "admin" && !u.GameModerator {
			return nil, ErrDenied
		}
		if in.Decision != "resolved" && in.Decision != "rejected" {
			return nil, failure("invalid_argument")
		}
		_, e := tx.Exec(ctx, `UPDATE chat_reports SET state=$2,reviewer_id=$3,resolved_at=clock_timestamp() WHERE id=$1 AND state IN('open','in_review')`, in.ID, in.Decision, u.ID)
		if e != nil {
			return nil, e
		}
		return map[string]any{"status": in.Decision}, audit(ctx, tx, u.ID, 0, nil, "report.resolve", in.ID, in.Reason)
	case "notification.read":
		_, e := tx.Exec(ctx, `UPDATE chat_notifications SET seen_at=clock_timestamp() WHERE user_id=$1 AND id=ANY($2::uuid[])`, u.ID, in.IDs)
		return map[string]any{"status": "read"}, e
	default:
		return nil, failure("unsupported_command")
	}
}
func (s *Service) favorite(ctx context.Context, tx pgx.Tx, u Identity, cmd Command, in Input) (any, error) {
	var folder *string
	if in.Folder != "" {
		if !identifiers.ValidUUID(in.Folder) {
			return nil, failure("invalid_argument")
		}
		var own string
		if e := tx.QueryRow(ctx, `SELECT id::text FROM chat_folders WHERE id=$1 AND owner_id=$2 FOR SHARE`, in.Folder, u.ID).Scan(&own); e != nil {
			return nil, ErrDenied
		}
		folder = &in.Folder
	}
	id := in.ID
	var e error
	if cmd.Type == "favorite.note" {
		body, e := Body(in.Body, 2500)
		if e != nil {
			return nil, e
		}
		id = UUID()
		_, e = tx.Exec(ctx, `INSERT INTO chat_saved_items(id,owner_id,folder_id,kind,body) VALUES($1,$2,$3,'note',$4)`, id, u.ID, folder, body)
		return map[string]any{"id": id}, e
	}
	var tag pgconnTag
	switch cmd.Type {
	case "favorite.edit":
		body, e := Body(in.Body, 2500)
		if e != nil {
			return nil, e
		}
		t, err := tx.Exec(ctx, `UPDATE chat_saved_items SET body=$3,updated_at=clock_timestamp() WHERE id=$1 AND owner_id=$2 AND kind='note'`, id, u.ID, body)
		tag = pgconnTag(t.RowsAffected())
		e = err
	case "favorite.move":
		t, err := tx.Exec(ctx, `UPDATE chat_saved_items SET folder_id=$3,updated_at=clock_timestamp() WHERE id=$1 AND owner_id=$2`, id, u.ID, folder)
		tag = pgconnTag(t.RowsAffected())
		e = err
	case "favorite.delete":
		t, err := tx.Exec(ctx, `DELETE FROM chat_saved_items WHERE id=$1 AND owner_id=$2`, id, u.ID)
		tag = pgconnTag(t.RowsAffected())
		e = err
	}
	if e == nil && tag != 1 {
		return nil, ErrDenied
	}
	return map[string]any{"id": id}, e
}

type pgconnTag int64

func (s *Service) sourceAction(ctx context.Context, tx pgx.Tx, u Identity, cmd Command, in Input) (any, error) {
	if !identifiers.ValidUUID(in.ID) {
		return nil, failure("invalid_argument")
	}
	var kind, channel string
	if e := tx.QueryRow(ctx, `SELECT kind,channel_id::text FROM chat_message_locator WHERE id=$1 AND expires_at>clock_timestamp()`, in.ID).Scan(&kind, &channel); e != nil {
		return nil, failure("message_missing")
	}
	var target string
	if e := tx.QueryRow(ctx, `SELECT CASE kind WHEN 'private' THEN conversation_id::text WHEN 'corporation' THEN company_id::text WHEN 'planet' THEN surface_id::text WHEN 'sector' THEN surface_id::text||'/'||sector_number::text ELSE '' END FROM chat_channels WHERE id=$1`, channel).Scan(&target); e != nil {
		return nil, e
	}
	if (kind == "planet" || kind == "sector") && !s.spatialAvailable() {
		return nil, failure("planet_unavailable")
	}
	c, e := s.channel(ctx, tx, u, Target{kind, target}, false)
	if e != nil {
		// A game moderator may address ONLY the message in an active complaint.
		if cmd.Type != "message.delete" || (!u.GameModerator && u.Role != "admin") {
			return nil, e
		}
		var reported bool
		if kind == "private" {
			if err := tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM chat_reports WHERE message_id=$1 AND state IN('open','in_review'))`, in.ID).Scan(&reported); err != nil || !reported {
				return nil, ErrDenied
			}
		} else {
			if err := tx.QueryRow(ctx, `SELECT chat_lock_service_scope($1,$2)`, u.ID, channel).Scan(&reported); err != nil || !reported {
				return nil, ErrDenied
			}
		}
		c = Channel{ID: channel, Kind: kind}
		var conv *string
		if err := tx.QueryRow(ctx, `SELECT conversation_id::text,company_id FROM chat_channels WHERE id=$1`, channel).Scan(&conv, &c.Company); err != nil {
			return nil, err
		}
		c.Conversation = conv
		if conv != nil {
			if _, err := s.conversation(ctx, tx, u, *conv, true); err != nil {
				return nil, err
			}
		}
	}
	var author int64
	if e = tx.QueryRow(ctx, `SELECT author_id FROM chat_message_locator WHERE id=$1 AND channel_id=$2 AND expires_at>clock_timestamp() FOR UPDATE`, in.ID, channel).Scan(&author); e != nil {
		return nil, failure("message_missing")
	}
	switch cmd.Type {
	case "favorite.copy":
		id := UUID()
		tag, err := tx.Exec(ctx, `INSERT INTO chat_saved_items(id,owner_id,kind,body,author_id,author_name_snapshot,original_at,source_kind,source_message_id,source_channel_id,surface_id,sector_number) SELECT $1,$2,'copy',m.body,m.author_id,u.username,m.sent_at,m.kind,m.id,m.channel_id,$4,$5 FROM chat_messages m LEFT JOIN users u ON u.id=m.author_id WHERE m.id=$3 AND m.channel_id=$6 AND m.expires_at>clock_timestamp() AND m.message_type='user'`, id, u.ID, in.ID, c.Surface, c.Sector, channel)
		if err != nil {
			return nil, err
		}
		if tag.RowsAffected() != 1 {
			return nil, failure("message_missing")
		}
		return map[string]any{"id": id}, nil
	case "report.create":
		reason, e := Body(in.Reason, 200)
		if e != nil {
			return nil, e
		}
		if e = s.rate(ctx, tx, u.ID, "report", cmd.Request); e != nil {
			return nil, e
		}
		id := UUID()
		_, e = tx.Exec(ctx, `INSERT INTO chat_reports(id,reporter_id,message_id,channel_id,kind,reason) VALUES($1,$2,$3,$4,$5,$6)`, id, u.ID, in.ID, channel, kind, reason)
		return map[string]any{"id": id}, e
	case "message.delete":
		mod := author != u.ID
		if mod {
			allowed := u.Role == "admin" || u.GameModerator
			if c.Company != nil && !allowed {
				allowed, e = s.moderator(ctx, tx, u, *c.Company, author)
				if e != nil {
					return nil, e
				}
			}
			if !allowed {
				return nil, ErrDenied
			}
			reason, e := Body(in.Reason, 200)
			if e != nil {
				return nil, failure("reason_required")
			}
			// Never copy the original body into the reason, audit or operation result.
			var original string
			if e = tx.QueryRow(ctx, `SELECT body FROM chat_messages WHERE id=$1 AND channel_id=$2`, in.ID, channel).Scan(&original); e != nil {
				return nil, e
			}
			if strings.Contains(reason, original) {
				return nil, failure("reason_must_not_quote_message")
			}
			if e = s.Journal.Record(in.ID); e != nil {
				return nil, e
			}
			if e = s.Journal.Watermark(ctx, tx); e != nil {
				return nil, e
			}
			if _, e = tx.Exec(ctx, `DELETE FROM chat_saved_items WHERE source_message_id=$1`, in.ID); e != nil {
				return nil, e
			}
			if e = audit(ctx, tx, u.ID, author, c.Company, "message.moderated", in.ID, reason); e != nil {
				return nil, e
			}
		}
		if _, e = tx.Exec(ctx, `DELETE FROM chat_messages WHERE id=$1 AND channel_id=$2`, in.ID, channel); e != nil {
			return nil, e
		}
		if _, e = tx.Exec(ctx, `DELETE FROM chat_message_locator WHERE id=$1`, in.ID); e != nil {
			return nil, e
		}
		if c.Conversation != nil {
			if e = s.unread(ctx, tx, *c.Conversation); e != nil {
				return nil, e
			}
		}
		if e = event(ctx, tx, "channel.changed", channel, nil, in.ID); e != nil {
			return nil, e
		}
		return map[string]any{"message_id": in.ID, "status": "deleted"}, nil
	}
	return nil, failure("unsupported_command")
}
func (s *Service) unread(ctx context.Context, tx pgx.Tx, conversation string) error {
	_, e := tx.Exec(ctx, `UPDATE chat_members member SET unread_count=(SELECT count(*) FROM chat_channels c JOIN chat_messages m ON m.channel_id=c.id WHERE c.conversation_id=member.conversation_id AND m.message_type='user' AND m.author_id<>member.user_id AND m.seq>member.join_floor_seq AND m.expires_at>clock_timestamp() AND NOT EXISTS(SELECT 1 FROM chat_read_receipts r WHERE r.user_id=member.user_id AND r.message_id=m.id)) WHERE member.conversation_id=$1 AND member.status='active'`, conversation)
	return e
}
func audit(ctx context.Context, tx pgx.Tx, actor, subject int64, company *int64, action, id, reason string) error {
	var ref *string
	if id != "" {
		ref = &id
	}
	_, e := tx.Exec(ctx, `INSERT INTO chat_audit(actor_id,subject_id,company_id,action,reason,reference_id) VALUES($1,$2,$3,$4,$5,$6)`, actor, subject, company, action, reason, ref)
	return e
}
func (s *Service) moderate(ctx context.Context, tx pgx.Tx, u Identity, cmd Command, in Input) (any, error) {
	var company, target int64
	var e error
	company, e = strconv.ParseInt(in.Target.Target, 10, 64)
	if e != nil {
		return nil, ErrDenied
	}
	target, e = strconv.ParseInt(in.User, 10, 64)
	if e != nil {
		return nil, ErrDenied
	}
	if cmd.Type == "mute" {
		ok, e := s.moderator(ctx, tx, u, company, target)
		if e != nil {
			return nil, e
		}
		if !ok {
			return nil, ErrDenied
		}
	} else if u.Role != "admin" && !u.GameModerator {
		var owner, rev int64
		var raw []byte
		if e = tx.QueryRow(ctx, `SELECT owner_id,permissions,revision FROM chat_lock_company_access($1,$2)`, u.ID, company).Scan(&owner, &raw, &rev); e != nil {
			return nil, ErrDenied
		}
		if owner != u.ID && !strings.Contains(string(raw), `"chat.moderate"`) {
			return nil, ErrDenied
		}
	}
	reason, e := Body(in.Reason, 200)
	if e != nil {
		return nil, failure("reason_required")
	}
	var member bool
	if e = tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM users WHERE id=$1 AND company_id=$2)`, target, company).Scan(&member); e != nil || !member {
		return nil, ErrDenied
	}
	if _, e = tx.Exec(ctx, `INSERT INTO chat_corporation_mute_state(company_id,user_id) VALUES($1,$2) ON CONFLICT DO NOTHING`, company, target); e != nil {
		return nil, e
	}
	if _, e = tx.Exec(ctx, `SELECT user_id FROM chat_corporation_mute_state WHERE company_id=$1 AND user_id=$2 FOR UPDATE`, company, target); e != nil {
		return nil, e
	}
	if cmd.Type == "unmute" {
		if _, e = tx.Exec(ctx, `UPDATE chat_corporation_mutes SET lifted_at=clock_timestamp() WHERE id=(SELECT current_mute_id FROM chat_corporation_mute_state WHERE company_id=$1 AND user_id=$2)`, company, target); e != nil {
			return nil, e
		}
		_, e = tx.Exec(ctx, `UPDATE chat_corporation_mute_state SET current_mute_id=NULL,expires_at=clock_timestamp(),changed_at=clock_timestamp() WHERE company_id=$1 AND user_id=$2`, company, target)
	} else {
		valid := false
		for _, n := range []int{300, 1800, 3600, 86400, 604800} {
			if in.Duration == n {
				valid = true
			}
		}
		if !valid {
			return nil, failure("invalid_duration")
		}
		id := UUID()
		if _, e = tx.Exec(ctx, `UPDATE chat_corporation_mutes SET lifted_at=clock_timestamp() WHERE id=(SELECT current_mute_id FROM chat_corporation_mute_state WHERE company_id=$1 AND user_id=$2) AND lifted_at IS NULL`, company, target); e != nil {
			return nil, e
		}
		if _, e = tx.Exec(ctx, `INSERT INTO chat_corporation_mutes(id,company_id,user_id,actor_id,duration_sec,reason,expires_at) VALUES($1,$2,$3,$4,$5,$6,clock_timestamp()+make_interval(secs=>$5::integer))`, id, company, target, u.ID, in.Duration, reason); e != nil {
			return nil, e
		}
		_, e = tx.Exec(ctx, `UPDATE chat_corporation_mute_state SET current_mute_id=$3,expires_at=clock_timestamp()+make_interval(secs=>$4::integer),changed_at=clock_timestamp() WHERE company_id=$1 AND user_id=$2`, company, target, id, in.Duration)
	}
	if e != nil {
		return nil, e
	}
	if e = audit(ctx, tx, u.ID, target, &company, cmd.Type, "", reason); e != nil {
		return nil, e
	}
	if e = event(ctx, tx, "moderation.changed", "", target, ""); e != nil {
		return nil, e
	}
	return map[string]any{"status": "ok"}, nil
}
