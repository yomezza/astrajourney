// Package chat owns chat data and transactions, independent of service transports.
package chat

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/yomezza/cosmosimulator/internal/platform/gamesignal"
	"github.com/yomezza/cosmosimulator/internal/platform/identifiers"
	"io"
	"strings"
	"sync/atomic"
	"time"
	"unicode/utf8"
)

const SchemaVersion = 1

var ErrDenied = errors.New("access_denied")

func failure(code string) error { return errors.New(code) }
func UUID() string {
	v, e := identifiers.UUID()
	if e != nil {
		panic(e)
	}
	return v
}

type Service struct {
	DB              *pgxpool.Pool
	Journal         *ErasureJournal
	PlanetAvailable func() bool
	commitWake      atomic.Pointer[func(string)]
	cleanupWake     atomic.Pointer[gamesignal.Emit]
	changesCalls    atomic.Uint64
	scopeChecks     atomic.Uint64
}
type Identity struct {
	ID            int64  `json:"id,string"`
	Name          string `json:"name"`
	Role          string `json:"role"`
	Session       string `json:"-"`
	GameModerator bool   `json:"game_moderator"`
}
type Lease struct {
	User       int64
	Connection string
	Generation int64
}
type Target struct {
	Kind   string `json:"kind"`
	Target string `json:"target,omitempty"`
}
type Command struct {
	Type    string          `json:"type"`
	Request string          `json:"request_id"`
	Payload json.RawMessage `json:"payload"`
}
type Input struct {
	Target
	ID         string          `json:"id,omitempty"`
	Body       string          `json:"body,omitempty"`
	Name       string          `json:"name,omitempty"`
	Title      string          `json:"title,omitempty"`
	User       string          `json:"user,omitempty"`
	Decision   string          `json:"decision,omitempty"`
	Folder     string          `json:"folder,omitempty"`
	Reply      string          `json:"reply_to,omitempty"`
	Reason     string          `json:"reason,omitempty"`
	ReasonCode string          `json:"reason_code,omitempty"`
	Duration   int             `json:"duration,omitempty"`
	Revision   int64           `json:"revision,omitempty"`
	IDs        []string        `json:"ids,omitempty"`
	Seen       int64           `json:"seen_seq,omitempty"`
	Settings   json.RawMessage `json:"settings,omitempty"`
	Entities   []Entity        `json:"entities,omitempty"`
	Confirm    bool            `json:"confirm,omitempty"`
}
type Entity struct {
	Star    int    `json:"star,omitempty"`
	Planet  int    `json:"planet,omitempty"`
	Kind    string `json:"kind"`
	ID      string `json:"id"`
	Surface string `json:"surface_id,omitempty"`
	Sector  int    `json:"sector_number,omitempty"`
	Start   int    `json:"start"`
	End     int    `json:"end"`
	Label   string `json:"label,omitempty"`
}
type Channel struct {
	ID           string  `json:"id"`
	Kind         string  `json:"kind"`
	Surface      *int64  `json:"surface,omitempty"`
	Sector       *int    `json:"sector,omitempty"`
	Company      *int64  `json:"company,omitempty"`
	Conversation *string `json:"conversation,omitempty"`
	Next         int64   `json:"next_seq,string"`
}
type Message struct {
	ReplyBody   *string         `json:"reply_body"`
	ReplyAuthor *string         `json:"reply_author"`
	ID          string          `json:"id"`
	Channel     string          `json:"channel"`
	Kind        string          `json:"kind"`
	Seq         int64           `json:"seq,string"`
	Author      *int64          `json:"author_id,string,omitempty"`
	Name        string          `json:"author"`
	Tag         string          `json:"tag"`
	Time        time.Time       `json:"sent_at"`
	Expires     time.Time       `json:"expires_at"`
	Body        string          `json:"body"`
	Reply       *string         `json:"reply_to"`
	Read        bool            `json:"read"`
	Sector      *int            `json:"sector"`
	Entities    json.RawMessage `json:"entities"`
	System      *string         `json:"system_code"`
}

func Body(text string, max int) (string, error) {
	if !utf8.ValidString(text) || strings.ContainsRune(text, 0) {
		return "", failure("invalid_argument")
	}
	text = strings.ReplaceAll(text, "\r\n", "\n")
	if strings.TrimSpace(text) == "" || utf8.RuneCountInString(text) > max {
		return "", failure("invalid_argument")
	}
	return text, nil
}
func (s *Service) Authenticate(ctx context.Context, hash string) (Identity, error) {
	u := Identity{Session: hash}
	err := s.DB.QueryRow(ctx, `SELECT id,name,role,game_moderator FROM chat_validate_session($1)`, hash).Scan(&u.ID, &u.Name, &u.Role, &u.GameModerator)
	if errors.Is(err, pgx.ErrNoRows) {
		err = failure("auth_required")
	}
	return u, err
}
func (s *Service) Claim(ctx context.Context, u Identity, instance string) (Lease, error) {
	l := Lease{User: u.ID, Connection: UUID()}
	err := s.DB.QueryRow(ctx, `INSERT INTO chat_ws_leases(user_id,connection_id,server_instance,expires_at) VALUES($1,$2,$3,clock_timestamp()+interval '45 seconds') ON CONFLICT(user_id) DO UPDATE SET connection_id=EXCLUDED.connection_id,server_instance=EXCLUDED.server_instance,generation=chat_ws_leases.generation+1,expires_at=EXCLUDED.expires_at,renewed_at=clock_timestamp() WHERE chat_ws_leases.expires_at<=clock_timestamp() RETURNING generation`, u.ID, l.Connection, instance).Scan(&l.Generation)
	if errors.Is(err, pgx.ErrNoRows) {
		err = failure("duplicate_chat_connection")
	}
	return l, err
}
func (s *Service) Renew(ctx context.Context, l Lease) error {
	tag, e := s.DB.Exec(ctx, `UPDATE chat_ws_leases SET expires_at=clock_timestamp()+interval '45 seconds',renewed_at=clock_timestamp() WHERE user_id=$1 AND connection_id=$2 AND generation=$3 AND expires_at>clock_timestamp()`, l.User, l.Connection, l.Generation)
	if e == nil && tag.RowsAffected() != 1 {
		return failure("stale_connection")
	}
	return e
}
func (s *Service) Release(ctx context.Context, l Lease) {
	s.DB.Exec(ctx, `UPDATE chat_ws_leases SET expires_at=clock_timestamp() WHERE user_id=$1 AND connection_id=$2 AND generation=$3`, l.User, l.Connection, l.Generation)
}
func (s *Service) Execute(ctx context.Context, u Identity, l Lease, cmd Command) (json.RawMessage, error) {
	if !identifiers.ValidUUID(cmd.Request) || l.User != u.ID {
		return nil, failure("invalid_argument")
	}
	var in Input
	dec := json.NewDecoder(strings.NewReader(string(cmd.Payload)))
	dec.DisallowUnknownFields()
	if dec.Decode(&in) != nil || dec.Decode(new(any)) != io.EOF {
		return nil, failure("invalid_argument")
	}
	for attempt := 0; attempt < 3; attempt++ {
		result, e := s.execute(ctx, u, l, cmd, in)
		if e == nil {
			return result, nil
		}
		var pe interface{ SQLState() string }
		if !errors.As(e, &pe) || (pe.SQLState() != "40001" && pe.SQLState() != "40P01") {
			return nil, e
		}
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-time.After(time.Duration(attempt+1) * 17 * time.Millisecond):
		}
	}
	return nil, failure("retry_later")
}
func (s *Service) execute(ctx context.Context, u Identity, l Lease, cmd Command, in Input) (json.RawMessage, error) {
	tx, e := s.DB.Begin(ctx)
	if e != nil {
		return nil, e
	}
	defer tx.Rollback(context.Background())
	if _, e = tx.Exec(ctx, `SET LOCAL lock_timeout='2s';SET LOCAL statement_timeout='5s'`); e != nil {
		return nil, e
	}
	var valid bool
	e = tx.QueryRow(ctx, `SELECT true FROM chat_ws_leases WHERE user_id=$1 AND connection_id=$2 AND generation=$3 AND expires_at>clock_timestamp() FOR SHARE`, u.ID, l.Connection, l.Generation).Scan(&valid)
	if e != nil {
		return nil, failure("stale_connection")
	}
	var authenticated int64
	if e = tx.QueryRow(ctx, `SELECT id,name,role,game_moderator FROM chat_validate_session($1)`, u.Session).Scan(&authenticated, &u.Name, &u.Role, &u.GameModerator); e != nil || authenticated != u.ID {
		return nil, failure("auth_required")
	}
	digest := sha256.Sum256(append([]byte(cmd.Type+"\n"), cmd.Payload...))
	tag, e := tx.Exec(ctx, `INSERT INTO chat_operations(user_id,request_id,action,payload_digest,state) VALUES($1,$2,$3,$4,'in_progress') ON CONFLICT DO NOTHING`, u.ID, cmd.Request, cmd.Type, digest[:])
	if e != nil {
		return nil, e
	}
	if tag.RowsAffected() == 0 {
		var old []byte
		var result json.RawMessage
		var state string
		var action string
		e = tx.QueryRow(ctx, `SELECT payload_digest,result_json,state,action FROM chat_operations WHERE user_id=$1 AND request_id=$2 FOR UPDATE`, u.ID, cmd.Request).Scan(&old, &result, &state, &action)
		if e != nil {
			return nil, e
		}
		if string(old) != string(digest[:]) || action != cmd.Type {
			return nil, failure("idempotency_conflict")
		}
		if state != "completed" {
			return nil, failure("operation_pending")
		}
		return result, tx.Commit(ctx)
	}
	result, e := s.mutate(ctx, tx, u, cmd, in)
	if e != nil {
		return nil, e
	}
	raw, e := json.Marshal(result)
	if e != nil {
		return nil, e
	}
	if _, e = tx.Exec(ctx, `UPDATE chat_operations SET state='completed',result_code='ok',result_json=$3 WHERE user_id=$1 AND request_id=$2`, u.ID, cmd.Request, raw); e != nil {
		return nil, e
	}
	if e = tx.Commit(ctx); e != nil {
		return nil, failure("operation_unknown")
	}
	s.committed("mutation")
	return raw, nil
}

func (s *Service) spatialAvailable() bool { return s.PlanetAvailable == nil || s.PlanetAvailable() }
