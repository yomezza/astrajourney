package workspaces

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"reflect"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/yomezza/cosmosimulator/internal/account"
	"github.com/yomezza/cosmosimulator/internal/platform/httpkit"
)

type Service struct {
	DB   *pgxpool.Pool
	Auth *account.Service
}

type preferences struct {
	Revision int64  `json:"revision"`
	Layout   Layout `json:"layout"`
}

func (s *Service) Preferences(w http.ResponseWriter, r *http.Request) {
	user, ok := s.Auth.User(w, r)
	if !ok {
		return
	}
	ctx := r.Context()
	current, err := s.load(ctx, user.ID)
	if err != nil {
		httpkit.Internal(w, err)
		return
	}
	if r.Method == http.MethodGet {
		httpkit.Reply(w, http.StatusOK, current)
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, 64<<10)
	var next preferences
	if !httpkit.Decode(w, r, &next) {
		return
	}
	if next.Revision < 1 || !next.Layout.valid() {
		httpkit.Fail(w, http.StatusBadRequest, "Некорректная раскладка")
		return
	}
	result, status, err := s.save(ctx, user.ID, current, next)
	if err != nil {
		httpkit.Internal(w, err)
		return
	}
	switch status {
	case http.StatusConflict:
		httpkit.Fail(w, status, "Рабочие пространства изменились в другой вкладке")
	case http.StatusBadRequest:
		httpkit.Fail(w, status, "Корабль недоступен")
	case http.StatusUnprocessableEntity:
		httpkit.Fail(w, status, "Некорректная раскладка")
	default:
		httpkit.Reply(w, status, result)
	}
}

func (s *Service) load(ctx context.Context, uid int64) (preferences, error) {
	var p preferences
	read := func() error {
		return s.DB.QueryRow(ctx, `SELECT revision,layout FROM pilot_workspace_preferences WHERE user_id=$1`, uid).Scan(&p.Revision, &p.Layout)
	}
	if err := read(); !errors.Is(err, pgx.ErrNoRows) {
		return p, err
	}
	start := initial()
	_, err := s.DB.Exec(ctx, `WITH created AS (
		INSERT INTO pilot_workspace_preferences(user_id,layout) VALUES($1,$2) ON CONFLICT DO NOTHING
		RETURNING user_id
	)
	INSERT INTO pilot_workspace_ids(id,user_id) SELECT $3::text::uuid,user_id FROM created`, uid, start, start.Active)
	if err != nil {
		return p, err
	}
	return p, read()
}

func (s *Service) save(ctx context.Context, uid int64, current, next preferences) (preferences, int, error) {
	if next.Revision == current.Revision || next.Revision == current.Revision-1 {
		if reflect.DeepEqual(next.Layout, current.Layout) {
			return current, http.StatusOK, nil
		}
	}
	if next.Revision != current.Revision {
		return current, http.StatusConflict, nil
	}

	previous := current.Layout.shipBindings()
	owned := make([]int64, 0)
	for binding := range next.Layout.shipBindings() {
		// Keep stale references for display, but only owners can introduce new ones.
		if !previous[binding] {
			owned = append(owned, binding.ship)
		}
	}
	raw, err := json.Marshal(next.Layout)
	if err != nil {
		return current, 0, err
	}
	tx, err := s.DB.Begin(ctx)
	if err != nil {
		return current, 0, err
	}
	defer tx.Rollback(context.Background())
	err = tx.QueryRow(ctx, `UPDATE pilot_workspace_preferences
		SET layout=$3, revision=revision+1
		WHERE user_id=$1 AND revision=$2
		AND NOT EXISTS (SELECT 1 FROM unnest($4::bigint[]) AS desired(id)
			WHERE NOT EXISTS (SELECT 1 FROM ships WHERE ships.id=desired.id AND user_id=$1))
		RETURNING revision`, uid, next.Revision, raw, owned).Scan(&next.Revision)
	if errors.Is(err, pgx.ErrNoRows) {
		_ = tx.Rollback(ctx)
		latest, e := s.load(ctx, uid)
		if e != nil {
			return current, 0, e
		}
		if latest.Revision == current.Revision {
			return current, http.StatusBadRequest, nil
		}
		if latest.Revision == current.Revision+1 && reflect.DeepEqual(next.Layout, latest.Layout) {
			return latest, http.StatusOK, nil
		}
		return latest, http.StatusConflict, nil
	}
	if err != nil {
		return current, 0, err
	}
	ids := next.Layout.ids()
	// Claim workspace and module UUIDs in a deterministic order. A different
	// account can neither reuse a live ID nor partially commit this document.
	if _, err = tx.Exec(ctx, `INSERT INTO pilot_workspace_ids(id,user_id)
		SELECT id::uuid,$1 FROM unnest($2::text[]) AS incoming(id) ORDER BY id
		ON CONFLICT DO NOTHING`, uid, ids); err != nil {
		return current, 0, err
	}
	var count int
	if err = tx.QueryRow(ctx, `SELECT count(*) FROM pilot_workspace_ids
		WHERE user_id=$1 AND id=ANY($2::text[]::uuid[])`, uid, ids).Scan(&count); err != nil {
		return current, 0, err
	}
	if count != len(ids) {
		return current, http.StatusUnprocessableEntity, nil
	}
	if _, err = tx.Exec(ctx, `DELETE FROM pilot_workspace_ids
		WHERE user_id=$1 AND NOT (id=ANY($2::text[]::uuid[]))`, uid, ids); err != nil {
		return current, 0, err
	}
	return next, http.StatusOK, tx.Commit(ctx)
}
