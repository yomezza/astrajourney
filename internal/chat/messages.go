package chat

import (
	"context"
	"encoding/json"
	"github.com/jackc/pgx/v5"
	"strconv"
	"strings"
	"time"
)

func (s *Service) rate(ctx context.Context, tx pgx.Tx, u int64, kind, op string) error {
	_, e := tx.Exec(ctx, `INSERT INTO chat_rate_state(user_id,kind) VALUES($1,$2) ON CONFLICT DO NOTHING`, u, kind)
	if e != nil {
		return e
	}
	var elapsed float64
	e = tx.QueryRow(ctx, `SELECT EXTRACT(epoch FROM clock_timestamp()-last_sent_at)::float8 FROM chat_rate_state WHERE user_id=$1 AND kind=$2 FOR UPDATE`, u, kind).Scan(&elapsed)
	if e != nil {
		return e
	}
	if kind == "private" || kind == "report" {
		window, limit := 10, 10
		if kind == "report" {
			window, limit = 600, 5
		}
		var n int
		e = tx.QueryRow(ctx, `SELECT count(*) FROM chat_rate_events WHERE user_id=$1 AND kind=$2 AND sent_at>clock_timestamp()-make_interval(secs=>$3)`, u, kind, window).Scan(&n)
		if e != nil {
			return e
		}
		if n >= limit {
			return failure("rate_limited")
		}
		_, e = tx.Exec(ctx, `INSERT INTO chat_rate_events(user_id,operation_id,kind) VALUES($1,$2,$3)`, u, op, kind)
		return e
	}
	delay := map[string]time.Duration{"global": 3 * time.Second, "planet": 2 * time.Second, "sector": time.Second, "corporation": time.Second}[kind]
	if elapsed < delay.Seconds() {
		return failure("rate_limited")
	}
	_, e = tx.Exec(ctx, `UPDATE chat_rate_state SET last_sent_at=clock_timestamp() WHERE user_id=$1 AND kind=$2`, u, kind)
	return e
}
func (s *Service) send(ctx context.Context, tx pgx.Tx, u Identity, cmd Command, in Input) (any, error) {
	c, e := s.channel(ctx, tx, u, in.Target, true)
	if e != nil {
		return nil, e
	}
	limit := 256
	if c.Kind == "private" {
		limit = 500
	}
	body, e := Body(in.Body, limit)
	if e != nil {
		return nil, e
	}
	if in.Reply != "" {
		var exists bool
		e = tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM chat_message_locator WHERE id=$1 AND channel_id=$2 AND expires_at>clock_timestamp())`, in.Reply, c.ID).Scan(&exists)
		if e != nil {
			return nil, e
		}
		if !exists {
			return nil, failure("message_missing")
		}
	}
	entities, e := s.entities(ctx, tx, u, c, in.Entities, body)
	if e != nil {
		return nil, e
	}
	if e = s.rate(ctx, tx, u.ID, c.Kind, cmd.Request); e != nil {
		return nil, e
	}
	var seq int64
	var at time.Time
	e = tx.QueryRow(ctx, `UPDATE chat_channels SET next_seq=next_seq+1 WHERE id=$1 RETURNING next_seq,clock_timestamp()`, c.ID).Scan(&seq, &at)
	if e != nil {
		return nil, e
	}
	days := map[string]int{"sector": 7, "global": 30, "planet": 30, "corporation": 90, "private": 180}[c.Kind]
	expiry := at.Add(time.Duration(days) * 24 * time.Hour)
	id := UUID()
	var origin *int
	if c.Kind == "planet" {
		var surface, epoch int64
		var sector int
		if e = tx.QueryRow(ctx, `SELECT surface,sector,epoch FROM chat_lock_planet_presence($1)`, u.ID).Scan(&surface, &sector, &epoch); e != nil {
			return nil, e
		}
		origin = &sector
	}
	var reply *string
	if in.Reply != "" {
		reply = &in.Reply
	}
	_, e = tx.Exec(ctx, `INSERT INTO chat_message_locator(id,kind,sent_at,channel_id,seq,author_id,expires_at) VALUES($1,$2,$3,$4,$5,$6,$7)`, id, c.Kind, at, c.ID, seq, u.ID, expiry)
	if e != nil {
		return nil, e
	}
	_, e = tx.Exec(ctx, `INSERT INTO chat_messages(kind,sent_at,id,expires_at,channel_id,seq,author_id,origin_sector_number,body,reply_to_id,entities) VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11)`, c.Kind, at, id, expiry, c.ID, seq, u.ID, origin, body, reply, entities)
	if e != nil {
		return nil, e
	}
	if c.Kind == "private" {
		hasAll := false
		for _, entity := range in.Entities {
			if entity.Kind == "all" {
				hasAll = true
			}
		}
		if !hasAll {
			if _, e = tx.Exec(ctx, `INSERT INTO chat_notifications(id,user_id,type,ref_id,channel_id) SELECT gen_random_uuid(),user_id,'private_message',$2,$3 FROM chat_members WHERE conversation_id=$1 AND status='active' AND user_id<>$4 ON CONFLICT DO NOTHING`, c.Conversation, id, c.ID, u.ID); e != nil {
				return nil, e
			}
		}
		_, e = tx.Exec(ctx, `UPDATE chat_members SET unread_count=unread_count+1 WHERE conversation_id=$1 AND status='active' AND user_id<>$2`, c.Conversation, u.ID)
		if e != nil {
			return nil, e
		}
	}
	if e = s.mentions(ctx, tx, u, c, id, in.Entities); e != nil {
		return nil, e
	}
	if e = event(ctx, tx, "channel.changed", c.ID, "", id); e != nil {
		return nil, e
	}
	return map[string]any{"message_id": id, "channel_id": c.ID, "seq": strconv.FormatInt(seq, 10), "sent_at": at, "expires_at": expiry}, nil
}
func event(ctx context.Context, tx pgx.Tx, kind, channel string, user any, id string) error {
	var c, r *string
	if channel != "" {
		c = &channel
	}
	if id != "" {
		r = &id
	}
	if user == "" {
		user = nil
	}
	_, e := tx.Exec(ctx, `INSERT INTO chat_outbox(event_kind,channel_id,target_user_id,reference_id) VALUES($1,$2,$3,$4)`, kind, c, user, r)
	return e
}
func (s *Service) entities(ctx context.Context, tx pgx.Tx, u Identity, c Channel, items []Entity, body string) (json.RawMessage, error) {
	if len(items) > 32 {
		return nil, failure("invalid_argument")
	}
	runes := []rune(body)
	last := 0
	for i := range items {
		v := &items[i]
		if v.Start < last || v.End <= v.Start || v.End > len(runes) {
			return nil, failure("invalid_argument")
		}
		last = v.End
		switch v.Kind {
		case "mention":
			id, e := strconv.ParseInt(v.ID, 10, 64)
			if e != nil {
				return nil, failure("invalid_argument")
			}
			var name string
			if e = tx.QueryRow(ctx, `SELECT username FROM users WHERE id=$1 AND NOT is_bot`, id).Scan(&name); e != nil {
				return nil, ErrDenied
			}
			if c.Kind == "private" {
				var ok bool
				e = tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM chat_members WHERE conversation_id=$1 AND user_id=$2 AND status='active')`, c.Conversation, id).Scan(&ok)
				if e != nil || !ok {
					return nil, ErrDenied
				}
			}
			if c.Kind == "corporation" {
				var ok bool
				e = tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM users WHERE id=$1 AND company_id=$2)`, id, c.Company).Scan(&ok)
				if e != nil || !ok {
					return nil, ErrDenied
				}
			}
			if c.Kind == "planet" || c.Kind == "sector" {
				var surface, epoch int64
				var sector int
				e = tx.QueryRow(ctx, `SELECT surface,sector,epoch FROM chat_lock_planet_presence($1)`, id).Scan(&surface, &sector, &epoch)
				if e != nil || surface != *c.Surface || (c.Sector != nil && sector != *c.Sector) {
					return nil, ErrDenied
				}
			}
			v.Label = name
		case "all":
			if c.Kind != "private" && c.Kind != "corporation" {
				return nil, ErrDenied
			}
			v.Label = "all"
		case "company":
			id, e := strconv.ParseInt(v.ID, 10, 64)
			if e != nil {
				return nil, ErrDenied
			}
			if e = tx.QueryRow(ctx, `SELECT name FROM companies WHERE id=$1 AND NOT chat_deleting`, id).Scan(&v.Label); e != nil {
				return nil, ErrDenied
			}
		case "ship":
			id, e := strconv.ParseInt(v.ID, 10, 64)
			if e != nil {
				return nil, ErrDenied
			}
			if e = tx.QueryRow(ctx, `SELECT name FROM ships WHERE id=$1 AND user_id=$2`, id, u.ID).Scan(&v.Label); e != nil {
				return nil, ErrDenied
			}
		case "surface", "sector":
			id, e := strconv.ParseInt(v.ID, 10, 64)
			if e != nil {
				return nil, ErrDenied
			}
			if e = tx.QueryRow(ctx, `SELECT name,star,planet FROM planet_surfaces WHERE id=$1 AND EXISTS(SELECT 1 FROM surveys WHERE user_id=$2 AND star=planet_surfaces.star)`, id, u.ID).Scan(&v.Label, &v.Star, &v.Planet); e != nil {
				return nil, ErrDenied
			}
			if v.Kind == "sector" {
				var exists bool
				e = tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM planet_sectors WHERE surface=$1 AND number=$2)`, id, v.Sector).Scan(&exists)
				if e != nil || !exists {
					return nil, ErrDenied
				}
			}
		default:
			return nil, failure("invalid_argument")
		}
	}
	raw, e := json.Marshal(items)
	if items == nil {
		raw = []byte("[]")
	}
	return raw, e
}
func (s *Service) mentions(ctx context.Context, tx pgx.Tx, u Identity, c Channel, message string, items []Entity) error {
	recipients := map[int64]bool{}
	all := false
	for _, v := range items {
		if v.Kind == "mention" {
			id, _ := strconv.ParseInt(v.ID, 10, 64)
			recipients[id] = true
		}
		if v.Kind == "all" {
			all = true
		}
	}
	if all {
		key := "all:" + c.ID
		_, e := tx.Exec(ctx, `INSERT INTO chat_rate_state(user_id,kind) VALUES($1,$2) ON CONFLICT DO NOTHING`, int64(0), key)
		if e != nil {
			return e
		}
		var allowed bool
		e = tx.QueryRow(ctx, `UPDATE chat_rate_state SET last_sent_at=clock_timestamp() WHERE user_id=$1 AND kind=$2 AND last_sent_at<=clock_timestamp()-interval '60 seconds' RETURNING true`, int64(0), key).Scan(&allowed)
		if e == nil && allowed {
			var rows pgx.Rows
			if c.Kind == "private" {
				rows, e = tx.Query(ctx, `SELECT user_id FROM chat_members WHERE conversation_id=$1 AND status='active'`, c.Conversation)
			} else {
				rows, e = tx.Query(ctx, `SELECT id FROM users WHERE company_id=$1 AND NOT is_bot`, c.Company)
			}
			if e != nil {
				return e
			}
			for rows.Next() {
				var id int64
				if e = rows.Scan(&id); e != nil {
					rows.Close()
					return e
				}
				recipients[id] = true
			}
			e = rows.Err()
			rows.Close()
			if e != nil {
				return e
			}
		}
	}
	for id := range recipients {
		if id == u.ID {
			continue
		}
		_, e := tx.Exec(ctx, `INSERT INTO chat_notifications(id,user_id,type,ref_id,channel_id) VALUES($1,$2,'mention',$3,$4) ON CONFLICT DO NOTHING`, UUID(), id, message, c.ID)
		if e != nil {
			return e
		}
	}
	return nil
}
func (s *Service) History(ctx context.Context, u Identity, t Target, before int64, query string) ([]Message, error) {
	if len([]rune(query)) > 128 {
		return nil, failure("invalid_argument")
	}
	tx, e := s.DB.Begin(ctx)
	if e != nil {
		return nil, e
	}
	defer tx.Rollback(context.Background())
	_, e = tx.Exec(ctx, `SET LOCAL statement_timeout='2s';SET LOCAL lock_timeout='1s'`)
	if e != nil {
		return nil, e
	}
	c, e := s.channel(ctx, tx, u, t, false)
	if e != nil {
		return nil, e
	}
	if before <= 0 {
		before = 9223372036854775807
	}
	pattern := "%" + strings.NewReplacer("\\", "\\\\", "%", "\\%", "_", "\\_").Replace(query) + "%"
	rows, e := tx.Query(ctx, `SELECT m.id::text,m.channel_id::text,m.kind,m.seq,m.author_id,COALESCE(u.username,''),COALESCE(co.tag,''),m.sent_at,m.expires_at,m.body,m.reply_to_id::text,EXISTS(SELECT 1 FROM chat_read_receipts r WHERE r.message_id=m.id AND r.user_id<>m.author_id),m.origin_sector_number,m.entities,m.system_code,parent.body,pu.username FROM chat_messages m JOIN chat_message_locator locator ON locator.id=m.id AND locator.kind=m.kind AND locator.sent_at=m.sent_at LEFT JOIN users u ON u.id=m.author_id LEFT JOIN companies co ON co.id=u.company_id LEFT JOIN chat_messages parent ON parent.id=m.reply_to_id AND parent.kind=m.kind AND parent.channel_id=m.channel_id AND parent.expires_at>clock_timestamp() LEFT JOIN users pu ON pu.id=parent.author_id WHERE m.channel_id=$1 AND m.kind=$2 AND m.seq<$3 AND m.expires_at>clock_timestamp() AND m.sent_at>=transaction_timestamp()-make_interval(days=>$6) AND (length($4)>=3 OR $4='' OR m.seq>$7-1000) AND ($4='' OR lower(m.body COLLATE chat_unicode) LIKE lower($5 COLLATE chat_unicode) ESCAPE '\') ORDER BY m.seq DESC LIMIT 50`, c.ID, c.Kind, before, query, pattern, map[string]int{"global": 30, "planet": 30, "sector": 7, "corporation": 90, "private": 180}[c.Kind], c.Next)
	if e != nil {
		return nil, e
	}
	out := []Message{}
	for rows.Next() {
		var m Message
		if e = rows.Scan(&m.ID, &m.Channel, &m.Kind, &m.Seq, &m.Author, &m.Name, &m.Tag, &m.Time, &m.Expires, &m.Body, &m.Reply, &m.Read, &m.Sector, &m.Entities, &m.System, &m.ReplyBody, &m.ReplyAuthor); e != nil {
			rows.Close()
			return nil, e
		}
		out = append(out, m)
	}
	e = rows.Err()
	rows.Close()
	if e != nil {
		return nil, e
	}
	return out, tx.Commit(ctx)
}
