package chat

import (
	"context"
	"encoding/json"
	"github.com/jackc/pgx/v5"
	"github.com/yomezza/cosmosimulator/internal/platform/identifiers"
	"strconv"
	"strings"
)

func list(ctx context.Context, tx pgx.Tx, query string, args ...any) ([]map[string]any, error) {
	rows, e := tx.Query(ctx, query, args...)
	if e != nil {
		return nil, e
	}
	defer rows.Close()
	out := []map[string]any{}
	for rows.Next() {
		values, e := rows.Values()
		if e != nil {
			return nil, e
		}
		v := map[string]any{}
		for i, f := range rows.FieldDescriptions() {
			v[f.Name] = values[i]
		}
		out = append(out, v)
	}
	return out, rows.Err()
}
func (s *Service) Query(ctx context.Context, u Identity, route string, t Target, id, search, before string) (any, error) {
	tx, e := s.DB.Begin(ctx)
	if e != nil {
		return nil, e
	}
	defer tx.Rollback(context.Background())
	if _, e = tx.Exec(ctx, `SET LOCAL statement_timeout='4s'; SET LOCAL lock_timeout='2s'`); e != nil {
		return nil, e
	}
	if e = tx.QueryRow(ctx, `SELECT id,name,role,game_moderator FROM chat_validate_session($1)`, u.Session).Scan(&u.ID, &u.Name, &u.Role, &u.GameModerator); e != nil {
		return nil, failure("auth_required")
	}
	var result any
	switch route {
	case "metrics":
		if u.Role != "admin" {
			return nil, ErrDenied
		}
		result, e = list(ctx, tx, `SELECT kind,count(*) AS messages,min(expires_at) AS oldest_expiry FROM chat_message_locator GROUP BY kind ORDER BY kind`)
	case "bootstrap":
		v := map[string]any{"user": u, "limits": map[string]int{"favorites": 2500, "global": 256, "planet": 256, "sector": 256, "corporation": 256, "private": 500}, "retention_days": map[string]int{"global": 30, "planet": 30, "sector": 7, "corporation": 90, "private": 180}}
		v["conversations"], e = list(ctx, tx, `SELECT c.id::text,c.type,c.state,c.owner_id::text AS owner,c.title,c.revision::text,m.unread_count,COALESCE((SELECT string_agg(u.username,', ' ORDER BY u.id) FROM chat_members cm JOIN users u ON u.id=cm.user_id WHERE cm.conversation_id=c.id AND cm.status='active' AND cm.user_id<>$1),'') AS participants FROM chat_conversations c JOIN chat_members m ON m.conversation_id=c.id WHERE m.user_id=$1 AND m.status='active' ORDER BY c.created_at DESC LIMIT 200`, u.ID)
		if e != nil {
			return nil, e
		}
		v["invitations"], e = list(ctx, tx, `SELECT i.id::text,i.conversation_id::text,i.sender_id::text,u.username AS sender,c.title,c.type,i.expires_at FROM chat_invites i JOIN chat_conversations c ON c.id=i.conversation_id JOIN users u ON u.id=i.sender_id WHERE i.recipient_id=$1 AND i.status='pending' AND i.expires_at>clock_timestamp() AND c.state<>'deleting' ORDER BY i.created_at DESC LIMIT 100`, u.ID)
		if e != nil {
			return nil, e
		}
		v["folders"], e = list(ctx, tx, `SELECT id::text,name,position FROM chat_folders WHERE owner_id=$1 ORDER BY position,created_at LIMIT 200`, u.ID)
		if e != nil {
			return nil, e
		}
		var settings json.RawMessage
		e = tx.QueryRow(ctx, `SELECT settings FROM chat_user_settings WHERE user_id=$1`, u.ID).Scan(&settings)
		if e == pgx.ErrNoRows {
			settings = json.RawMessage(`{}`)
			e = nil
		}
		if e != nil {
			return nil, e
		}
		v["settings"] = settings
		v["corporation"], e = list(ctx, tx, `SELECT c.id::text,c.name,c.tag,c.owner_id::text AS owner,COALESCE(r.permissions,'[]') AS permissions FROM users u JOIN companies c ON c.id=u.company_id LEFT JOIN company_roles r ON r.id=u.company_role AND r.company_id=c.id WHERE u.id=$1 AND NOT c.chat_deleting`, u.ID)
		if e != nil {
			return nil, e
		}
		// Missing/transitioning presence is an unavailable spatial tab, not a fabricated scope.
		if s.spatialAvailable() {
			v["presence"], e = list(ctx, tx, `SELECT surface::text,sector,epoch::text FROM chat_lock_planet_presence($1)`, u.ID)
		} else {
			v["presence"] = []any{}
		}
		if e != nil {
			return nil, e
		}
		v["restrictions"], e = list(ctx, tx, `SELECT m.reason,m.expires_at FROM chat_corporation_mute_state state JOIN chat_corporation_mutes m ON m.id=state.current_mute_id WHERE state.user_id=$1 AND state.company_id=(SELECT company_id FROM users WHERE id=$1) AND state.expires_at>clock_timestamp()`, u.ID)
		if e != nil {
			return nil, e
		}
		v["public_new"], e = list(ctx, tx, `SELECT c.kind,c.id::text,c.next_seq>COALESCE(r.last_seen_seq,0) AS fresh FROM chat_channels c LEFT JOIN chat_read_marks r ON r.channel_id=c.id AND r.user_id=$1 WHERE c.kind='global' OR (c.kind='corporation' AND c.company_id=(SELECT company_id FROM users WHERE id=$1)) OR ($2 AND c.kind IN('planet','sector') AND EXISTS(SELECT 1 FROM chat_lock_planet_presence($1) p WHERE p.surface=c.surface_id AND (c.kind='planet' OR p.sector=c.sector_number)))`, u.ID, s.spatialAvailable())
		if e != nil {
			return nil, e
		}
		result = v
	case "favorites":
		if len([]rune(search)) > 100 {
			return nil, failure("invalid_argument")
		}
		pattern := literal(search)
		result, e = list(ctx, tx, `SELECT id::text,folder_id::text,kind,body,author_id::text,author_name_snapshot,original_at,source_kind,source_message_id::text,source_channel_id::text,surface_id::text,sector_number,saved_at,updated_at FROM chat_saved_items WHERE owner_id=$1 AND ($2='' OR folder_id::text=$2) AND ($3='' OR lower(body COLLATE chat_unicode) LIKE lower($3 COLLATE chat_unicode) ESCAPE '\') AND ($4='' OR (saved_at,id)<(SELECT saved_at,id FROM chat_saved_items WHERE id::text=$4 AND owner_id=$1)) ORDER BY saved_at DESC,id DESC LIMIT 50`, u.ID, id, pattern, before)
	case "operation":
		if !identifiers.ValidUUID(id) {
			return nil, failure("invalid_argument")
		}
		var state string
		var raw json.RawMessage
		e = tx.QueryRow(ctx, `SELECT state,result_json FROM chat_operations WHERE user_id=$1 AND request_id=$2 AND expires_at>clock_timestamp()`, u.ID, id).Scan(&state, &raw)
		if e == pgx.ErrNoRows {
			e = nil
			state = "not_found"
		}
		result = map[string]any{"state": state, "result": raw}
	case "notifications":
		// Revalidate recipients now: old company memberships and departed spatial
		// scopes must never reveal a mention which is no longer accessible.
		result, e = list(ctx, tx, `SELECT n.id::text,n.type,n.ref_id::text,n.channel_id::text,n.created_at,n.seen_at FROM chat_notifications n LEFT JOIN chat_channels c ON c.id=n.channel_id WHERE n.user_id=$1 AND n.expires_at>clock_timestamp() AND (c.id IS NULL AND n.type LIKE 'invite%' OR c.kind='global' OR c.kind='corporation' AND c.company_id=(SELECT company_id FROM users WHERE id=$1) AND EXISTS(SELECT 1 FROM companies co WHERE co.id=c.company_id AND NOT co.chat_deleting) OR c.kind='private' AND EXISTS(SELECT 1 FROM chat_members m JOIN chat_conversations cv ON cv.id=m.conversation_id WHERE m.conversation_id=c.conversation_id AND m.user_id=$1 AND m.status='active' AND cv.state='active') OR $2 AND c.kind IN('planet','sector') AND EXISTS(SELECT 1 FROM chat_lock_planet_presence($1) p WHERE p.surface=c.surface_id AND (c.kind='planet' OR p.sector=c.sector_number))) ORDER BY n.created_at DESC LIMIT 100`, u.ID, s.spatialAvailable())
	case "roster":
		c, err := s.channel(ctx, tx, u, t, false)
		if err != nil {
			return nil, err
		}
		switch c.Kind {
		case "private":
			result, e = list(ctx, tx, `SELECT u.id::text,u.username AS name,m.joined_at,c.owner_id=u.id AS owner FROM chat_members m JOIN users u ON u.id=m.user_id JOIN chat_conversations c ON c.id=m.conversation_id WHERE m.conversation_id=$1 AND m.status='active' ORDER BY u.username LIMIT 50`, c.Conversation)
		case "corporation":
			result, e = list(ctx, tx, `SELECT u.id::text,u.username AS name,c.owner_id=u.id AS owner,COALESCE(r.permissions,'[]') AS permissions,ms.expires_at AS muted_until FROM users u JOIN companies c ON c.id=u.company_id LEFT JOIN company_roles r ON r.id=u.company_role AND r.company_id=c.id LEFT JOIN chat_corporation_mute_state ms ON ms.user_id=u.id AND ms.company_id=c.id AND ms.expires_at>clock_timestamp() WHERE u.company_id=$1 AND NOT u.is_bot ORDER BY u.username LIMIT 200`, c.Company)
		case "planet", "sector":
			result, e = list(ctx, tx, `SELECT u.id::text,u.username AS name FROM planet_visits v JOIN users u ON u.id=v.user_id JOIN planet_runtime_leases l ON l.id=1 AND l.fence=v.runtime_fence WHERE v.state='active' AND v.current_surface=$1 AND ($2::integer IS NULL OR v.current_sector=$2) AND v.checked_at>clock_timestamp()-interval '2 seconds' AND l.expires_at>clock_timestamp() ORDER BY u.username LIMIT 200`, c.Surface, c.Sector)
		default:
			result, e = list(ctx, tx, `SELECT u.id::text,u.username AS name FROM chat_ws_leases l JOIN users u ON u.id=l.user_id WHERE l.expires_at>clock_timestamp() ORDER BY u.username LIMIT 200`)
		}
	case "private.search":
		if len([]rune(search)) < 3 || len([]rune(search)) > 100 {
			return nil, failure("search_too_short")
		}
		result, e = list(ctx, tx, `SELECT m.id::text,m.channel_id::text,c.conversation_id::text,m.body,m.sent_at,u.username AS author FROM chat_messages m JOIN chat_channels c ON c.id=m.channel_id JOIN chat_members member ON member.conversation_id=c.conversation_id AND member.user_id=$1 AND member.status='active' JOIN chat_conversations cv ON cv.id=c.conversation_id AND cv.state='active' LEFT JOIN users u ON u.id=m.author_id WHERE m.kind='private' AND m.sent_at>=transaction_timestamp()-interval '180 days' AND m.expires_at>clock_timestamp() AND lower(m.body COLLATE chat_unicode) LIKE lower($2 COLLATE chat_unicode) ESCAPE '\' ORDER BY m.sent_at DESC LIMIT 50`, u.ID, literal(search))
	case "my.moderation":
		result, e = list(ctx, tx, `SELECT action,reason,created_at,company_id::text,reference_id::text FROM chat_audit WHERE subject_id=$1 AND action IN('mute','unmute','message.moderated') AND created_at>clock_timestamp()-interval '180 days' ORDER BY id DESC LIMIT 100`, u.ID)
	case "corporate.moderation":
		var company int64
		if e = tx.QueryRow(ctx, `SELECT company_id FROM users WHERE id=$1`, u.ID).Scan(&company); e != nil {
			return nil, ErrDenied
		}
		var owner, revision int64
		var permissions []byte
		if e = tx.QueryRow(ctx, `SELECT owner_id,permissions,revision FROM chat_lock_company_access($1,$2)`, u.ID, company).Scan(&owner, &permissions, &revision); e != nil {
			return nil, ErrDenied
		}
		var ps []string
		json.Unmarshal(permissions, &ps)
		allowed := u.ID == owner || u.Role == "admin" || u.GameModerator
		for _, p := range ps {
			if p == "chat.moderate" {
				allowed = true
			}
		}
		if !allowed {
			return nil, ErrDenied
		}
		mutes, err := list(ctx, tx, `SELECT state.user_id::text,u.username,m.reason,m.actor_id::text,state.expires_at FROM chat_corporation_mute_state state JOIN chat_corporation_mutes m ON m.id=state.current_mute_id LEFT JOIN users u ON u.id=state.user_id WHERE state.company_id=$1 AND state.expires_at>clock_timestamp() ORDER BY state.expires_at DESC LIMIT 200`, company)
		if err != nil {
			return nil, err
		}
		events, err := list(ctx, tx, `SELECT action,actor_id::text,subject_id::text,reason,created_at FROM chat_audit WHERE company_id=$1 AND action IN('mute','unmute','message.moderated') AND created_at>clock_timestamp()-interval '180 days' ORDER BY id DESC LIMIT 100`, company)
		if err != nil {
			return nil, err
		}
		result = map[string]any{"company": strconv.FormatInt(company, 10), "mutes": mutes, "events": events}
	case "service.channels", "service.history":
		if u.Role != "admin" && !u.GameModerator {
			return nil, ErrDenied
		}
		if route == "service.channels" {
			result, e = list(ctx, tx, `SELECT c.id::text,c.kind,co.name AS company,p.name AS planet,c.sector_number FROM chat_channels c LEFT JOIN companies co ON co.id=c.company_id LEFT JOIN planet_surfaces p ON p.id=c.surface_id WHERE c.kind<>'private' AND (c.kind<>'corporation' OR co.id IS NOT NULL AND NOT co.chat_deleting) AND (c.kind NOT IN('planet','sector') OR EXISTS(SELECT 1 FROM planet_runtime_controls rc WHERE rc.surface=c.surface_id AND rc.state='enabled')) ORDER BY c.kind,c.created_at DESC LIMIT 500`)
		} else {
			if !identifiers.ValidUUID(id) {
				return nil, ErrDenied
			}
			var scopeKind string
			if e = tx.QueryRow(ctx, `SELECT kind FROM chat_channels WHERE id=$1`, id).Scan(&scopeKind); e != nil {
				return nil, ErrDenied
			}
			if (scopeKind == "planet" || scopeKind == "sector") && !s.spatialAvailable() {
				return nil, failure("planet_unavailable")
			}
			var allowed bool
			if e = tx.QueryRow(ctx, `SELECT chat_lock_service_scope($1,$2)`, u.ID, id).Scan(&allowed); e != nil || !allowed {
				return nil, ErrDenied
			}
			result, e = list(ctx, tx, `SELECT m.id::text,m.kind,m.author_id::text,u.username AS author,m.sent_at,m.body,m.seq::text FROM chat_messages m JOIN chat_message_locator locator ON locator.id=m.id LEFT JOIN users u ON u.id=m.author_id WHERE m.channel_id=$1 AND m.expires_at>clock_timestamp() ORDER BY m.seq DESC LIMIT 50`, id)
			if e == nil {
				e = audit(ctx, tx, u.ID, 0, nil, "service.view", id, "")
			}
		}
	case "reports", "report", "audit":
		if u.Role != "admin" && !u.GameModerator {
			return nil, ErrDenied
		}
		if route == "reports" {
			result, e = list(ctx, tx, `SELECT r.id::text,r.reporter_id::text,u.username AS reporter,r.message_id::text,r.channel_id::text,r.kind,r.reason,r.state,r.created_at FROM chat_reports r LEFT JOIN users u ON u.id=r.reporter_id WHERE ($1='' OR r.state=$1) ORDER BY r.created_at DESC LIMIT 100`, id)
		} else if route == "audit" {
			result, e = list(ctx, tx, `SELECT id::text,actor_id::text,subject_id::text,company_id::text,action,reason,reference_id::text,created_at FROM chat_audit WHERE created_at>clock_timestamp()-interval '180 days' ORDER BY id DESC LIMIT 100`)
		} else {
			if !identifiers.ValidUUID(id) {
				return nil, ErrDenied
			}
			result, e = list(ctx, tx, `SELECT r.id::text,r.message_id::text,r.reason,r.state,m.body,m.sent_at,m.author_id::text,u.username AS author FROM chat_reports r LEFT JOIN chat_messages m ON m.id=r.message_id AND m.channel_id=r.channel_id AND m.expires_at>clock_timestamp() LEFT JOIN users u ON u.id=m.author_id WHERE r.id=$1 AND r.state IN('open','in_review')`, id)
			if e == nil {
				e = audit(ctx, tx, u.ID, 0, nil, "report.inspect", id, "")
			}
		}
	case "link.options":
		if len([]rune(search)) > 100 {
			return nil, failure("invalid_argument")
		}
		result, e = list(ctx, tx, `SELECT * FROM(SELECT 'ship'::text AS kind,id::text,name AS label FROM ships WHERE user_id=$1 UNION ALL SELECT 'company',id::text,name FROM companies WHERE NOT chat_deleting UNION ALL SELECT 'surface',id::text,name FROM planet_surfaces WHERE EXISTS(SELECT 1 FROM surveys WHERE user_id=$1 AND star=planet_surfaces.star)) objects WHERE ($2='' OR lower(label COLLATE chat_unicode) LIKE lower($2 COLLATE chat_unicode) ESCAPE '\') ORDER BY kind,label LIMIT 100`, u.ID, literal(search))
	case "link":
		var entity Entity
		if json.Unmarshal([]byte(id), &entity) != nil {
			return nil, ErrDenied
		}
		entity.Start = 0
		entity.End = 1
		raw, err := s.entities(ctx, tx, u, Channel{Kind: "global"}, []Entity{entity}, "x")
		if err != nil {
			return nil, err
		}
		result = json.RawMessage(raw)
	default:
		return nil, failure("invalid_argument")
	}
	if e != nil {
		return nil, e
	}
	if e = tx.Commit(ctx); e != nil {
		return nil, e
	}
	return result, nil
}
func literal(s string) string {
	if s == "" {
		return ""
	}
	return "%" + strings.NewReplacer(`\`, `\\`, "%", `\%`, "_", `\_`).Replace(s) + "%"
}
func ParseBefore(v string) int64 { n, _ := strconv.ParseInt(v, 10, 64); return n }
