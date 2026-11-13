package knowledge

import (
	"context"
	"errors"
	"io/fs"
	"net/http"
	"path/filepath"
	"sort"
	"strconv"
	"strings"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/yomezza/cosmosimulator/internal/account"
	"github.com/yomezza/cosmosimulator/internal/fleet"
	"github.com/yomezza/cosmosimulator/internal/platform/configuration"
	"github.com/yomezza/cosmosimulator/internal/platform/httpkit"
	"github.com/yomezza/cosmosimulator/internal/platform/postgres"
	resourcecatalog "github.com/yomezza/cosmosimulator/internal/resources"
)

type Asset struct {
	Key      string `json:"key"`
	Path     string `json:"path"`
	SHA      string `json:"sha256"`
	Hologram bool   `json:"hologram_allowed"`
}
type Service struct {
	uploads       chan struct{}
	DB            *pgxpool.Pool
	Auth          *account.Service
	MediaDir      string
	Assets        map[string]Asset
	AssetRevision string
}

func New(db *pgxpool.Pool, auth *account.Service, files fs.FS) *Service {
	dir := configuration.Getenv("GUIDE_MEDIA_DIR")
	if dir == "" {
		dir = filepath.Join("data", "guide-media")
	}
	a := &Service{uploads: make(chan struct{}, 2), DB: db, Auth: auth, MediaDir: dir, Assets: map[string]Asset{}}
	a.Assets, a.AssetRevision = verifiedAssets(files)
	return a
}
func locale(r *http.Request) string {
	if r.URL.Query().Get("lang") == "en" {
		return "en"
	}
	return "ru"
}
func (a *Service) admin(w http.ResponseWriter, r *http.Request) (account.User, bool) {
	u, ok := a.Auth.User(w, r)
	if ok && u.Role != "admin" {
		httpkit.Fail(w, 403, "Требуется роль администратора")
		ok = false
	}
	return u, ok
}
func failure(w http.ResponseWriter, err error) {
	var e *pgconn.PgError
	switch {
	case errors.Is(err, pgx.ErrNoRows):
		httpkit.Fail(w, 404, "Материал не найден")
	case errors.As(err, &e) && (e.Code == "23505" || e.Code == "23503" || e.Code == "23514" || e.Code == "P0001"):
		httpkit.Fail(w, 409, "Конфликт данных: переместите содержимое или обновите редакцию")
	default:
		var v *inputError
		if errors.As(err, &v) {
			httpkit.Fail(w, v.status, v.message)
		} else {
			httpkit.Internal(w, err)
		}
	}
}

type inputError struct {
	status  int
	message string
}

func (e *inputError) Error() string { return e.message }
func invalid(s string) error        { return &inputError{422, s} }

const visibleCategories = `WITH RECURSIVE visible AS(SELECT id,parent_id FROM aj_guide_categories WHERE parent_id IS NULL AND status='visible' UNION ALL SELECT c.id,c.parent_id FROM aj_guide_categories c JOIN visible p ON c.parent_id=p.id WHERE c.status='visible') `
const publishedSelect = `SELECT a.id::text,a.slug,a.category_id::text,a.sort_order,a.version,v.title,v.summary,v.markdown_source,v.revision,v.locale AS actual_locale,(v.locale<>$1) AS fallback FROM aj_guide_articles a JOIN visible c ON c.id=a.category_id JOIN LATERAL(SELECT r.* FROM aj_guide_publications p JOIN aj_guide_article_revisions r ON (r.article_id,r.revision,r.locale)=(p.article_id,p.revision,p.locale) WHERE p.article_id=a.id AND (p.locale=$1 OR p.locale='ru') ORDER BY (p.locale=$1) DESC LIMIT 1)v ON true WHERE a.status='active'`

func (a *Service) AssetsView(w http.ResponseWriter, r *http.Request) {
	if strings.HasPrefix(r.URL.Path, "/api/admin/") {
		if _, ok := a.admin(w, r); !ok {
			return
		}
	} else {
		if _, ok := a.Auth.User(w, r); !ok {
			return
		}
	}
	items := []Asset{}
	for _, v := range a.Assets {
		items = append(items, v)
	}
	sort.Slice(items, func(i, j int) bool { return items[i].Key < items[j].Key })
	httpkit.Reply(w, 200, map[string]any{"revision": a.AssetRevision, "items": items})
}
func gameCatalog() []map[string]any {
	out := []map[string]any{}
	for _, v := range resourcecatalog.Catalog() {
		out = append(out, map[string]any{"id": "game:resource:" + fmtValue(v["id"]), "title": v["name"], "category_id": "game:resources", "summary": v["description"], "source": "game", "type": "resource", "ref": v["id"]})
	}
	for _, v := range fleet.Catalogue {
		out = append(out, map[string]any{"id": "game:ship:" + v.ID, "title": v.Name, "category_id": "game:ships", "summary": v.Name, "source": "game", "type": "ship", "ref": v.ID})
	}
	for _, v := range [][2]string{{"star", "Звезда"}, {"garden", "Садовый мир"}, {"rocky", "Каменистая"}, {"icy", "Ледяная"}, {"gas", "Газовый гигант"}, {"lava", "Вулканическая"}, {"asteroids", "Пояс астероидов"}, {"ice-field", "Ледяное поле"}, {"station", "Орбитальная станция"}, {"black-hole", "Чёрная дыра"}} {
		out = append(out, map[string]any{"id": "game:object:" + v[0], "title": v[1], "category_id": "game:objects", "source": "game", "type": "object", "ref": v[0]})
	}
	out = append(out, map[string]any{"id": "game:technical:experience", "title": "Стаж и размер флота", "category_id": "game:technical", "source": "game", "type": "technical", "ref": "experience"})
	return out
}
func (a *Service) Catalog(w http.ResponseWriter, r *http.Request) {
	if _, ok := a.Auth.User(w, r); !ok {
		return
	}
	ctx := r.Context()
	lang := locale(r)
	tx, err := a.DB.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.RepeatableRead, AccessMode: pgx.ReadOnly})
	if err != nil {
		failure(w, err)
		return
	}
	defer tx.Rollback(context.Background())
	var revision int64
	if err = tx.QueryRow(ctx, `SELECT revision FROM aj_guide_state WHERE id=1`).Scan(&revision); err != nil {
		failure(w, err)
		return
	}
	tag := `"guide-` + strconv.FormatInt(revision, 10) + "-" + lang + `"`
	w.Header().Set("ETag", tag)
	// Conditional requests are private and still pass authentication.
	if r.Header.Get("If-None-Match") == tag {
		w.WriteHeader(304)
		return
	}
	categories, err := postgres.QueryMaps(ctx, tx, visibleCategories+`SELECT c.id::text,c.parent_id::text,c.sort_order,COALESCE(t.title,ru.title,c.slug) AS title,CASE WHEN t.title IS NULL AND $1<>'ru' THEN true ELSE false END AS fallback FROM aj_guide_categories c JOIN visible x ON x.id=c.id LEFT JOIN aj_guide_category_i18n t ON t.category_id=c.id AND t.locale=$1 LEFT JOIN aj_guide_category_i18n ru ON ru.category_id=c.id AND ru.locale='ru' ORDER BY c.sort_order,c.slug`, lang)
	if err != nil {
		failure(w, err)
		return
	}
	articles, err := postgres.QueryMaps(ctx, tx, visibleCategories+publishedSelect+` ORDER BY a.sort_order,a.slug`, lang)
	if err != nil {
		failure(w, err)
		return
	}
	searchText := map[string]string{}
	items := gameCatalog()
	for _, v := range articles {
		v["source"] = "article"
		searchText[fmtValue(v["id"])] = fmtValue(v["markdown_source"])
		delete(v, "markdown_source")
		items = append(items, v)
	}
	q := strings.ToLower(strings.TrimSpace(r.URL.Query().Get("q")))
	category := r.URL.Query().Get("category")
	filtered := []map[string]any{}
	for _, v := range items {
		hay := strings.ToLower(fmtValue(v["title"]) + " " + fmtValue(v["summary"]) + " " + searchText[fmtValue(v["id"])])
		if (category == "" || fmtValue(v["category_id"]) == category) && (q == "" || strings.Contains(hay, q)) {
			filtered = append(filtered, v)
		}
	}
	offset, _ := strconv.Atoi(r.URL.Query().Get("cursor"))
	offset = max(0, min(offset, len(filtered)))
	end := min(offset+40, len(filtered))
	next := ""
	if end < len(filtered) {
		next = strconv.Itoa(end)
	}
	if err = tx.Commit(ctx); err != nil {
		failure(w, err)
		return
	}
	httpkit.Reply(w, 200, map[string]any{"items": filtered[offset:end], "categories": categories, "count": len(filtered), "next_cursor": next, "revision": revision, "requested_locale": lang})
}
func fmtValue(v any) string {
	if s, ok := v.(string); ok {
		return s
	}
	return ""
}
func (a *Service) Article(w http.ResponseWriter, r *http.Request) {
	if _, ok := a.Auth.User(w, r); !ok {
		return
	}
	ctx := r.Context()
	lang := locale(r)
	id := r.PathValue("id")
	tx, err := a.DB.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.RepeatableRead, AccessMode: pgx.ReadOnly})
	if err != nil {
		failure(w, err)
		return
	}
	defer tx.Rollback(context.Background())
	rows, err := postgres.QueryMaps(ctx, tx, visibleCategories+publishedSelect+` AND a.id::text=$2`, lang, id)
	if err != nil {
		failure(w, err)
		return
	}
	if len(rows) == 0 {
		failure(w, pgx.ErrNoRows)
		return
	}
	article := rows[0]
	blocks, err := Parse(fmtValue(article["markdown_source"]))
	if err != nil {
		failure(w, err)
		return
	}
	// References were validated at save/publication. Removed assets remain safe
	// typed references and render an unavailable placeholder, not a broken article.
	delete(article, "markdown_source")
	article["blocks"] = blocks
	article["requested_locale"] = lang
	related, err := postgres.QueryMaps(ctx, tx, visibleCategories+publishedSelect+` AND a.id IN(SELECT related_id FROM aj_guide_related WHERE article_id=$2::uuid) ORDER BY (SELECT position FROM aj_guide_related WHERE article_id=$2::uuid AND related_id=a.id),a.slug`, lang, id)
	if err != nil {
		failure(w, err)
		return
	}
	for _, v := range related {
		delete(v, "markdown_source")
	}
	article["related"] = related
	media, err := a.revisionMedia(ctx, tx, id, article["revision"], fmtValue(article["actual_locale"]))
	if err != nil {
		failure(w, err)
		return
	}
	article["media"] = media
	if err = tx.Commit(ctx); err != nil {
		failure(w, err)
		return
	}
	httpkit.Reply(w, 200, article)
}

func (a *Service) checkReferences(ctx context.Context, q postgres.Querier, blocks []Block) error {
	games := map[string]bool{}
	for _, v := range gameCatalog() {
		games[v["id"].(string)] = true
	}
	for _, ref := range references(blocks) {
		if strings.HasPrefix(ref.Target, "builtin:") {
			if _, ok := a.Assets[strings.TrimPrefix(ref.Target, "builtin:")]; !ok {
				return invalid("Встроенный ресурс недоступен")
			}
		}
		if strings.HasPrefix(ref.Target, "game:") && !games[ref.Target] {
			return invalid("Игровой материал недоступен")
		}
		if strings.HasPrefix(ref.Target, "media:") {
			var ready bool
			if err := q.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM aj_guide_media WHERE id::text=$1 AND state='ready')`, strings.TrimPrefix(ref.Target, "media:")).Scan(&ready); err != nil {
				return err
			}
			if !ready {
				return invalid("Изображение недоступно")
			}
		}
	}
	return nil
}

func (a *Service) revisionMedia(ctx context.Context, q interface {
	Query(context.Context, string, ...any) (pgx.Rows, error)
}, id string, revision any, lang string) ([]map[string]any, error) {
	items, err := postgres.QueryMaps(ctx, q, `SELECT m.position,m.role,m.caption,m.builtin_asset_key,m.uploaded_media_id::text,g.state,COALESCE(g.alt_ru,'') AS alt_ru,COALESCE(g.alt_en,'') AS alt_en FROM aj_guide_revision_media m LEFT JOIN aj_guide_media g ON g.id=m.uploaded_media_id WHERE m.article_id=$1::uuid AND m.revision=$2 AND m.locale=$3 ORDER BY m.position`, id, revision, lang)
	for _, v := range items {
		if key := fmtValue(v["builtin_asset_key"]); key != "" {
			if asset, ok := a.Assets[key]; ok {
				v["asset"] = asset
			} else {
				v["unavailable"] = true
			}
		} else if v["state"] != "ready" {
			v["unavailable"] = true
		}
	}
	return items, err
}
