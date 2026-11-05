package chat

import (
	"context"
	"encoding/json"
	"errors"
	"github.com/jackc/pgx/v5"
	"github.com/yomezza/cosmosimulator/internal/platform/identifiers"
	"strconv"
)

func (s *Service) channel(ctx context.Context, tx pgx.Tx, u Identity, t Target, write bool) (Channel, error) {
	c := Channel{Kind: t.Kind}
	var e error
	switch t.Kind {
	case "global":
	case "corporation":
		var cid int64
		if e = tx.QueryRow(ctx, `SELECT company_id FROM users WHERE id=$1`, u.ID).Scan(&cid); e != nil {
			return c, ErrDenied
		}
		if t.Target != "" && t.Target != strconv.FormatInt(cid, 10) {
			return c, ErrDenied
		}
		var owner, rev int64
		var permissions []byte
		if e = tx.QueryRow(ctx, `SELECT owner_id,permissions,revision FROM chat_lock_company_access($1,$2)`, u.ID, cid).Scan(&owner, &permissions, &rev); e != nil {
			return c, ErrDenied
		}
		c.Company = &cid
		if write {
			var muted bool
			e = tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM chat_corporation_mute_state WHERE company_id=$1 AND user_id=$2 AND expires_at>clock_timestamp())`, cid, u.ID).Scan(&muted)
			if e != nil {
				return c, e
			}
			if muted {
				return c, failure("muted")
			}
		}
	case "planet", "sector":
		if !s.spatialAvailable() {
			return c, failure("planet_unavailable")
		}
		var surface, epoch int64
		var sector int
		if e = tx.QueryRow(ctx, `SELECT surface,sector,epoch FROM chat_lock_planet_presence($1)`, u.ID).Scan(&surface, &sector, &epoch); e != nil {
			return c, failure("presence_unverifiable")
		}
		c.Surface = &surface
		expected := strconv.FormatInt(surface, 10)
		if t.Kind == "sector" {
			c.Sector = &sector
			expected += "/" + strconv.Itoa(sector)
		}
		if t.Target != "" && t.Target != expected {
			return c, failure("scope_changed")
		}
	case "private":
		if !identifiers.ValidUUID(t.Target) {
			return c, ErrDenied
		}
		state, e := s.conversation(ctx, tx, u, t.Target, false)
		if e != nil {
			return c, e
		}
		if state.State == "deleting" {
			return c, failure("conversation_deleting")
		}
		if write && state.State != "active" {
			return c, failure("conversation_pending")
		}
		c.Conversation = &t.Target
	default:
		return c, failure("invalid_argument")
	}
	_, e = tx.Exec(ctx, `INSERT INTO chat_channels(id,kind,surface_id,sector_number,company_id,conversation_id) VALUES($1,$2,$3,$4,$5,$6) ON CONFLICT DO NOTHING`, UUID(), c.Kind, c.Surface, c.Sector, c.Company, c.Conversation)
	if e != nil {
		return c, e
	}
	sql := `SELECT id::text,next_seq FROM chat_channels WHERE kind=$1 AND surface_id IS NOT DISTINCT FROM $2 AND sector_number IS NOT DISTINCT FROM $3 AND company_id IS NOT DISTINCT FROM $4 AND conversation_id IS NOT DISTINCT FROM $5`
	if write {
		sql += " FOR UPDATE"
	}
	e = tx.QueryRow(ctx, sql, c.Kind, c.Surface, c.Sector, c.Company, c.Conversation).Scan(&c.ID, &c.Next)
	return c, e
}

type Conversation struct {
	ID       string `json:"id"`
	Type     string `json:"type"`
	State    string `json:"state"`
	Owner    int64  `json:"owner,string"`
	Title    string `json:"title"`
	Revision int64  `json:"revision,string"`
}

func (s *Service) conversation(ctx context.Context, tx pgx.Tx, u Identity, id string, invited bool) (Conversation, error) {
	c := Conversation{ID: id}
	if !identifiers.ValidUUID(id) {
		return c, ErrDenied
	}
	var low, high int64
	e := tx.QueryRow(ctx, `SELECT low_user_id,high_user_id FROM chat_dm_pairs WHERE conversation_id=$1`, id).Scan(&low, &high)
	if e == nil {
		if _, e = tx.Exec(ctx, `SELECT low_user_id FROM chat_dm_pair_locks WHERE low_user_id=$1 AND high_user_id=$2 FOR UPDATE`, low, high); e != nil {
			return c, e
		}
	} else if !errors.Is(e, pgx.ErrNoRows) {
		return c, e
	}
	e = tx.QueryRow(ctx, `SELECT type,state,owner_id,title,revision FROM chat_conversations WHERE id=$1 FOR UPDATE`, id).Scan(&c.Type, &c.State, &c.Owner, &c.Title, &c.Revision)
	if e != nil {
		return c, ErrDenied
	}
	var member bool
	e = tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM chat_members WHERE conversation_id=$1 AND user_id=$2 AND status='active')`, id, u.ID).Scan(&member)
	if e != nil {
		return c, e
	}
	if !member && !invited {
		return c, ErrDenied
	}
	if c.State == "deleting" {
		return c, failure("conversation_deleting")
	}
	return c, nil
}
func (s *Service) moderator(ctx context.Context, tx pgx.Tx, u Identity, company, target int64) (bool, error) {
	if u.Role == "admin" || u.GameModerator {
		return true, nil
	}
	if u.ID == target {
		return false, nil
	}
	var owner int64
	var permissions []byte
	var rev int64
	e := tx.QueryRow(ctx, `SELECT owner_id,permissions,revision FROM chat_lock_company_access($1,$2)`, u.ID, company).Scan(&owner, &permissions, &rev)
	if e != nil {
		return false, ErrDenied
	}
	if u.ID == owner {
		return true, nil
	}
	var ps []string
	_ = json.Unmarshal(permissions, &ps)
	allowed := false
	for _, p := range ps {
		if p == "chat.moderate" {
			allowed = true
		}
	}
	var protected bool
	e = tx.QueryRow(ctx, `SELECT u.id=$3 OR COALESCE(r.permissions,'[]'::jsonb)?'chat.moderate' FROM users u LEFT JOIN company_roles r ON r.id=u.company_role AND r.company_id=u.company_id WHERE u.id=$1 AND u.company_id=$2`, target, company, owner).Scan(&protected)
	return allowed && !protected, e
}
