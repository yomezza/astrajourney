package ideas

import (
	"context"
	"net/http"
	"net/url"
	"strings"
	"unicode/utf8"

	"github.com/jackc/pgx/v5"

	"github.com/yomezza/cosmosimulator/internal/platform/httpkit"
	"github.com/yomezza/cosmosimulator/internal/platform/postgres"
	textutil "github.com/yomezza/cosmosimulator/internal/platform/text"
)

func validCategory(value string) bool {
	switch value {
	case "gameplay", "interface", "economy", "other":
		return true
	}
	return false
}

func (a *Service) list(w http.ResponseWriter, ctx context.Context, owner int64, scope string, query url.Values, page int) {
	category := query.Get("category")
	if category != "" && !validCategory(category) {
		httpkit.Fail(w, 400, "Некорректная категория идеи")
		return
	}
	search := strings.TrimSpace(query.Get("search"))
	if utf8.RuneCountInString(search) > 120 || !textutil.Valid(search, false) {
		httpkit.Fail(w, 400, "Некорректный поисковый запрос")
		return
	}
	sort := query.Get("sort")
	const oldest, newest = "oldest", "newest"
	if sort != "" && sort != oldest && sort != newest {
		httpkit.Fail(w, 400, "Некорректная сортировка")
		return
	}

	projection := ""
	order := "i.id DESC" // Preserve the legacy order for mine and all without an explicit sort.
	if scope == "community" {
		projection = ",i.published_at"
		order = "COALESCE(i.published_at,i.created_at) DESC,i.id DESC"
		if sort == oldest {
			order = "COALESCE(i.published_at,i.created_at) ASC,i.id ASC"
		}
	} else if sort == oldest {
		order = "i.created_at ASC,i.id ASC"
	} else if sort == newest {
		order = "i.created_at DESC,i.id DESC"
	}

	const where = ` WHERE
		(CASE $1::text WHEN 'community' THEN i.status='approved' WHEN 'all' THEN true ELSE i.user_id=$2::bigint END)
		AND ($3::text='' OR i.category=$3)
		AND ($4::text='' OR
			strpos(lower(i.title COLLATE pg_catalog.pg_c_utf8),lower($4 COLLATE pg_catalog.pg_c_utf8))>0 OR
			strpos(lower(i.description COLLATE pg_catalog.pg_c_utf8),lower($4 COLLATE pg_catalog.pg_c_utf8))>0)`

	// Read the page and its count from the same snapshot during moderation.
	tx, err := a.DB.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.RepeatableRead, AccessMode: pgx.ReadOnly})
	if err != nil {
		httpkit.Internal(w, err)
		return
	}
	defer tx.Rollback(context.Background())
	var total int
	if err := tx.QueryRow(ctx, `SELECT count(*) FROM player_ideas i`+where, scope, owner, category, search).Scan(&total); err != nil {
		httpkit.Internal(w, err)
		return
	}
	const pageSize = 10
	pages := max(1, (total+pageSize-1)/pageSize)
	page = min(page, pages)
	items, err := postgres.QueryMaps(ctx, tx, `SELECT i.id,i.title,i.description,i.category,i.status,i.created_at,
		COALESCE(u.username,i.author_name) AS author`+projection+
		` FROM player_ideas i LEFT JOIN users u ON u.id=i.user_id`+where+
		` ORDER BY `+order+` LIMIT $5 OFFSET $6`, scope, owner, category, search, pageSize, (page-1)*pageSize)
	if err == nil {
		err = tx.Commit(ctx)
	}
	if err != nil {
		httpkit.Internal(w, err)
		return
	}
	if items == nil {
		items = []map[string]any{}
	}
	httpkit.Reply(w, 200, map[string]any{"items": items, "total": total, "page": page, "pages": pages})
}
