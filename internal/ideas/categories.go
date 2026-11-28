package ideas

import (
	"errors"
	"net/http"
	"strings"
	"unicode/utf8"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/yomezza/cosmosimulator/internal/platform/httpkit"
	"github.com/yomezza/cosmosimulator/internal/platform/postgres"
	textutil "github.com/yomezza/cosmosimulator/internal/platform/text"
)

func (a *Service) Categories(w http.ResponseWriter, r *http.Request) {
	u, ok := a.Auth.User(w, r)
	if !ok {
		return
	}
	ctx := r.Context()
	if r.Method == http.MethodGet {
		rows, err := postgres.QueryMaps(ctx, a.DB, "SELECT id,name,active FROM player_idea_categories ORDER BY name,id")
		if err != nil {
			httpkit.Internal(w, err)
			return
		}
		httpkit.Reply(w, 200, rows)
		return
	}
	if u.Role != "admin" {
		httpkit.Fail(w, 403, "Требуется роль администратора")
		return
	}
	var in struct {
		ID     string `json:"id"`
		Name   string `json:"name"`
		Active *bool  `json:"active"`
	}
	if !httpkit.Decode(w, r, &in) {
		return
	}
	in.Name = strings.TrimSpace(in.Name)
	if in.Name == "" || utf8.RuneCountInString(in.Name) > 80 || !textutil.Valid(in.Name, false) || in.Active == nil {
		httpkit.Fail(w, 400, "Укажите название категории до 80 символов и её доступность")
		return
	}
	var id string
	var err error
	if in.ID == "" {
		err = a.DB.QueryRow(ctx, "INSERT INTO player_idea_categories(name,active) VALUES($1,$2) RETURNING id", in.Name, *in.Active).Scan(&id)
	} else {
		err = a.DB.QueryRow(ctx, "UPDATE player_idea_categories SET name=$2,active=$3 WHERE id=$1 RETURNING id", in.ID, in.Name, *in.Active).Scan(&id)
	}
	var pgErr *pgconn.PgError
	switch {
	case errors.Is(err, pgx.ErrNoRows):
		httpkit.Fail(w, 404, "Категория не найдена")
	case errors.As(err, &pgErr) && pgErr.Code == "23505":
		httpkit.Fail(w, 409, "Категория с таким названием уже существует")
	case err != nil:
		httpkit.Internal(w, err)
	default:
		httpkit.Reply(w, 200, map[string]string{"id": id})
	}
}
