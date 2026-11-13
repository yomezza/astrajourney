package knowledge

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"regexp"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/yomezza/cosmosimulator/internal/platform/httpkit"
	"github.com/yomezza/cosmosimulator/internal/platform/idempotency"
	"github.com/yomezza/cosmosimulator/internal/platform/identifiers"
	"github.com/yomezza/cosmosimulator/internal/platform/postgres"
)

type Translation struct {
	Title       string `json:"title"`
	Description string `json:"description"`
}
type Illustration struct {
	Builtin string `json:"builtin"`
	Media   string `json:"media"`
	Role    string `json:"role"`
	Caption string `json:"caption"`
}
type Edit struct {
	Operation string         `json:"operation"`
	Action    string         `json:"action"`
	ID        string         `json:"id"`
	Slug      string         `json:"slug"`
	Parent    *string        `json:"parent"`
	Category  string         `json:"category"`
	Order     int            `json:"order"`
	Status    string         `json:"status"`
	Expected  int64          `json:"expected_revision"`
	RU        Translation    `json:"ru"`
	EN        Translation    `json:"en"`
	Locale    string         `json:"locale"`
	Title     string         `json:"title"`
	Summary   string         `json:"summary"`
	Markdown  string         `json:"markdown"`
	Revision  int            `json:"revision"`
	Related   []string       `json:"related"`
	Media     []Illustration `json:"media"`
}

var slugPattern = regexp.MustCompile(`^[a-z0-9][a-z0-9-]{0,79}$`)

func stale() error {
	return &inputError{409, "Редакция уже изменена. Обновите данные перед сохранением"}
}

func (a *Service) mutate(w http.ResponseWriter, r *http.Request, run func(context.Context, pgx.Tx, int64, Edit) (any, error)) {
	u, ok := a.admin(w, r)
	if !ok {
		return
	}
	var in Edit
	if !httpkit.Decode(w, r, &in) {
		return
	}
	if in.Action == "" {
		in.Action = "save"
	}
	if !identifiers.ValidUUID(in.Operation) {
		httpkit.Fail(w, 400, "Нужен UUID операции")
		return
	}
	if in.ID != "" && !identifiers.ValidUUID(in.ID) {
		httpkit.Fail(w, 400, "Некорректный UUID материала")
		return
	}
	ctx := r.Context()
	tx, err := a.DB.Begin(ctx)
	if err != nil {
		failure(w, err)
		return
	}
	defer tx.Rollback(context.Background())
	if _, err = tx.Exec(ctx, `SELECT pg_advisory_xact_lock(792503)`); err != nil {
		failure(w, err)
		return
	}
	var role string
	if err = tx.QueryRow(ctx, `SELECT role FROM users WHERE id=$1 FOR SHARE`, u.ID).Scan(&role); err != nil {
		failure(w, err)
		return
	}
	if role != "admin" {
		httpkit.Fail(w, 403, "Требуется роль администратора")
		return
	}
	digest := idempotency.Digest(struct {
		Path string
		Body Edit
	}{r.URL.Path, in})
	var old string
	var actor *int64
	var receipt []byte
	err = tx.QueryRow(ctx, `SELECT actor,digest,result FROM aj_guide_operations WHERE operation=$1`, in.Operation).Scan(&actor, &old, &receipt)
	if err == nil {
		if actor == nil || *actor != u.ID || old != digest {
			httpkit.Fail(w, 409, "Идентификатор операции уже использован")
		} else {
			httpkit.Reply(w, 200, json.RawMessage(receipt))
		}
		return
	}
	if !errors.Is(err, pgx.ErrNoRows) {
		failure(w, err)
		return
	}
	result, err := run(ctx, tx, u.ID, in)
	if err != nil {
		// End the failed/aborted transaction before taking another pool connection.
		_ = tx.Rollback(context.Background())
		a.auditRejected(r, u.ID, in, err)
		failure(w, err)
		return
	}
	receipt, _ = json.Marshal(result)
	audit, _ := json.Marshal(map[string]any{"operation": in.Operation, "target": in.ID, "action": in.Action, "expected_revision": in.Expected})
	if _, err = tx.Exec(ctx, `INSERT INTO admin_log(actor,action,data) VALUES($1,'knowledge_edit',$2)`, u.ID, audit); err == nil {
		_, err = tx.Exec(ctx, `UPDATE aj_guide_state SET revision=revision+1 WHERE id=1`)
	}
	if err == nil {
		_, err = tx.Exec(ctx, `INSERT INTO aj_guide_operations(operation,actor,digest,result) VALUES($1,$2,$3,$4)`, in.Operation, u.ID, digest, receipt)
	}
	if err == nil {
		err = tx.Commit(ctx)
	}
	if err != nil {
		failure(w, err)
		return
	}
	httpkit.Reply(w, 200, result)
}

func (a *Service) auditRejected(r *http.Request, actor int64, in Edit, cause error) {
	code := "database_error"
	var input *inputError
	if errors.As(cause, &input) {
		code = "invalid_edit"
		if input.status == 409 {
			code = "revision_conflict"
		}
	}
	// Never record article bodies, uploaded bytes, SQL errors or query strings.
	data, _ := json.Marshal(map[string]any{"operation": in.Operation, "target": in.ID, "action": in.Action, "path": r.URL.Path, "result": code})
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	_, _ = a.DB.Exec(ctx, `INSERT INTO admin_log(actor,action,data) VALUES($1,'knowledge_edit_rejected',$2)`, actor, data)
}

func (a *Service) Categories(w http.ResponseWriter, r *http.Request) {
	if r.Method == "GET" {
		if _, ok := a.admin(w, r); !ok {
			return
		}
		rows, err := postgres.QueryMaps(r.Context(), a.DB, `SELECT c.id::text,c.parent_id::text,c.slug,c.sort_order,c.status,c.version,c.created_at,c.updated_at,COALESCE(t.title,c.slug) AS title,COALESCE(t.description,'') AS description,COALESCE(e.title,'') AS en_title,COALESCE(e.description,'') AS en_description FROM aj_guide_categories c LEFT JOIN aj_guide_category_i18n t ON t.category_id=c.id AND t.locale='ru' LEFT JOIN aj_guide_category_i18n e ON e.category_id=c.id AND e.locale='en' ORDER BY c.sort_order,c.slug`)
		if err != nil {
			failure(w, err)
			return
		}
		httpkit.Reply(w, 200, map[string]any{"items": rows})
		return
	}
	a.mutate(w, r, func(ctx context.Context, tx pgx.Tx, actor int64, in Edit) (any, error) {
		if in.Action != "save" && in.Action != "delete" {
			return nil, invalid("Неизвестное действие с категорией")
		}
		if in.Parent != nil && (*in.Parent == "" || !identifiers.ValidUUID(*in.Parent)) {
			return nil, invalid("Некорректная родительская категория")
		}
		id := in.ID
		version := int64(0)
		if id == "" {
			var err error
			id, err = identifiers.UUID()
			if err != nil {
				return nil, err
			}
		} else {
			if err := tx.QueryRow(ctx, `SELECT version FROM aj_guide_categories WHERE id=$1 FOR UPDATE`, id).Scan(&version); err != nil {
				return nil, err
			}
			if version != in.Expected {
				return nil, stale()
			}
		}
		if in.Action == "delete" {
			_, err := tx.Exec(ctx, `DELETE FROM aj_guide_categories WHERE id=$1`, id)
			return map[string]any{"id": id, "deleted": true}, err
		}
		if !slugPattern.MatchString(in.Slug) || len(strings.TrimSpace(in.RU.Title)) == 0 || len([]rune(in.RU.Title)) > 100 || len([]rune(in.EN.Title)) > 100 {
			return nil, invalid("Укажите код и название категории на русском")
		}
		if in.Status == "" {
			in.Status = "visible"
		}
		if in.Status != "visible" && in.Status != "hidden" && in.Status != "archived" {
			return nil, invalid("Некорректный статус категории")
		}
		_, err := tx.Exec(ctx, `INSERT INTO aj_guide_categories(id,parent_id,slug,sort_order,status,version) VALUES($1,$2,$3,$4,$5,1) ON CONFLICT(id) DO UPDATE SET parent_id=EXCLUDED.parent_id,slug=EXCLUDED.slug,sort_order=EXCLUDED.sort_order,status=EXCLUDED.status,version=aj_guide_categories.version+1,updated_at=clock_timestamp()`, id, in.Parent, in.Slug, in.Order, in.Status)
		if err != nil {
			return nil, err
		}
		var invalidTree bool
		if err = tx.QueryRow(ctx, `WITH RECURSIVE tree(id,depth) AS(SELECT id,1 FROM aj_guide_categories WHERE parent_id IS NULL UNION ALL SELECT c.id,t.depth+1 FROM aj_guide_categories c JOIN tree t ON c.parent_id=t.id WHERE t.depth<4) SELECT EXISTS(SELECT 1 FROM tree WHERE depth>3) OR (SELECT count(*) FROM tree)<>(SELECT count(*) FROM aj_guide_categories)`).Scan(&invalidTree); err != nil {
			return nil, err
		}
		if invalidTree {
			return nil, invalid("Не более трёх уровней категорий, циклы запрещены")
		}
		for lang, text := range map[string]Translation{"ru": in.RU, "en": in.EN} {
			if strings.TrimSpace(text.Title) == "" {
				_, err = tx.Exec(ctx, `DELETE FROM aj_guide_category_i18n WHERE category_id=$1 AND locale=$2`, id, lang)
			} else {
				_, err = tx.Exec(ctx, `INSERT INTO aj_guide_category_i18n(category_id,locale,title,description) VALUES($1,$2,$3,$4) ON CONFLICT(category_id,locale) DO UPDATE SET title=EXCLUDED.title,description=EXCLUDED.description`, id, lang, text.Title, text.Description)
			}
			if err != nil {
				return nil, err
			}
		}
		return map[string]any{"id": id, "version": version + 1}, nil
	})
}

func (a *Service) Articles(w http.ResponseWriter, r *http.Request) {
	if r.Method == "GET" {
		if _, ok := a.admin(w, r); !ok {
			return
		}
		rows, err := postgres.QueryMaps(r.Context(), a.DB, `SELECT a.id::text,a.category_id::text,a.slug,a.sort_order,a.status,a.version,a.created_at,a.updated_at,COALESCE(t.title,a.slug) AS title FROM aj_guide_articles a LEFT JOIN LATERAL(SELECT title FROM aj_guide_article_revisions WHERE article_id=a.id ORDER BY revision DESC LIMIT 1)t ON true ORDER BY a.sort_order,a.slug`)
		if err != nil {
			failure(w, err)
			return
		}
		httpkit.Reply(w, 200, map[string]any{"items": rows})
		return
	}
	a.mutate(w, r, a.editArticle)
}
func (a *Service) editArticle(ctx context.Context, tx pgx.Tx, actor int64, in Edit) (any, error) {
	switch in.Action {
	case "save", "archive", "restore", "hide", "unhide", "move", "restore_revision":
	default:
		return nil, invalid("Неизвестное действие со статьёй")
	}
	if in.ID == "" && in.Action != "save" {
		return nil, invalid("Сначала создайте статью")
	}
	id := in.ID
	version := int64(0)
	if id == "" {
		var err error
		id, err = identifiers.UUID()
		if err != nil {
			return nil, err
		}
		if !slugPattern.MatchString(in.Slug) || !identifiers.ValidUUID(in.Category) {
			return nil, invalid("Укажите категорию и код статьи")
		}
		if _, err = tx.Exec(ctx, `INSERT INTO aj_guide_articles(id,category_id,slug,sort_order,created_by) VALUES($1,$2,$3,$4,$5)`, id, in.Category, in.Slug, in.Order, actor); err != nil {
			return nil, err
		}
	} else {
		if err := tx.QueryRow(ctx, `SELECT version FROM aj_guide_articles WHERE id=$1 FOR UPDATE`, id).Scan(&version); err != nil {
			return nil, err
		}
		if version != in.Expected {
			return nil, stale()
		}
	}
	if in.Action == "archive" || in.Action == "restore" {
		status := "archived"
		if in.Action == "restore" {
			status = "active"
		}
		if _, err := tx.Exec(ctx, `UPDATE aj_guide_articles SET status=$2,version=version+1,updated_at=clock_timestamp() WHERE id=$1`, id, status); err != nil {
			return nil, err
		}
		_, err := tx.Exec(ctx, `INSERT INTO aj_guide_publication_history(article_id,locale,operation,actor_id) VALUES($1,'ru',$2,$3)`, id, in.Action, actor)
		return map[string]any{"id": id, "version": version + 1}, err
	}
	if in.Action == "hide" || in.Action == "unhide" {
		status := "hidden"
		if in.Action == "unhide" {
			status = "active"
		}
		tag, err := tx.Exec(ctx, `UPDATE aj_guide_articles SET status=$2,version=version+1,updated_at=clock_timestamp() WHERE id=$1 AND status<>'archived'`, id, status)
		if err == nil && tag.RowsAffected() == 0 {
			return nil, &inputError{409, "Сначала восстановите статью из архива"}
		}
		return map[string]any{"id": id, "version": version + 1}, err
	}
	if in.Action == "move" {
		if !identifiers.ValidUUID(in.Category) {
			return nil, invalid("Укажите категорию")
		}
		_, err := tx.Exec(ctx, `UPDATE aj_guide_articles SET category_id=$2,sort_order=$3,version=version+1,updated_at=clock_timestamp() WHERE id=$1`, id, in.Category, in.Order)
		return map[string]any{"id": id, "version": version + 1}, err
	}
	if in.Locale != "ru" && in.Locale != "en" {
		return nil, invalid("Выберите ru или en")
	}
	if in.Action == "restore_revision" {
		if err := tx.QueryRow(ctx, `SELECT title,summary,markdown_source FROM aj_guide_article_revisions WHERE article_id=$1 AND revision=$2 AND locale=$3`, id, in.Revision, in.Locale).Scan(&in.Title, &in.Summary, &in.Markdown); err != nil {
			return nil, err
		}
	}
	if strings.TrimSpace(in.Title) == "" || len([]rune(in.Title)) > 180 || len([]rune(in.Summary)) > 1200 {
		return nil, invalid("Укажите допустимый заголовок и аннотацию")
	}
	blocks, err := Parse(in.Markdown)
	if err != nil {
		return nil, invalid(err.Error())
	}
	if err = a.checkReferences(ctx, tx, blocks); err != nil {
		return nil, err
	}
	if len(in.Media) > 12 || len(in.Related) > 30 {
		return nil, invalid("Слишком много связей или иллюстраций")
	}
	if in.Slug != "" || in.Category != "" {
		if !slugPattern.MatchString(in.Slug) || !identifiers.ValidUUID(in.Category) {
			return nil, invalid("Укажите категорию и код статьи")
		}
		if _, err = tx.Exec(ctx, `UPDATE aj_guide_articles SET slug=$2,category_id=$3,sort_order=$4 WHERE id=$1`, id, in.Slug, in.Category, in.Order); err != nil {
			return nil, err
		}
	}
	revision := int(version + 1)
	_, err = tx.Exec(ctx, `INSERT INTO aj_guide_article_revisions(article_id,revision,locale,title,summary,markdown_source,editor_id) VALUES($1,$2,$3,$4,$5,$6,$7)`, id, revision, in.Locale, in.Title, in.Summary, in.Markdown, actor)
	if err != nil {
		return nil, err
	}
	if in.Action == "restore_revision" {
		_, err = tx.Exec(ctx, `INSERT INTO aj_guide_revision_media SELECT article_id,$2,locale,position,role,uploaded_media_id,builtin_asset_key,caption FROM aj_guide_revision_media WHERE article_id=$1 AND revision=$3 AND locale=$4`, id, revision, in.Revision, in.Locale)
		if err != nil {
			return nil, err
		}
	} else {
		// Inline images also become immutable revision references, which prevent GC.
		illustrations := append([]Illustration(nil), in.Media...)
		for _, v := range references(blocks) {
			if v.Type == "image" {
				item := Illustration{Role: "illustration", Caption: v.Text}
				if strings.HasPrefix(v.Target, "media:") {
					item.Media = strings.TrimPrefix(v.Target, "media:")
				} else {
					item.Builtin = strings.TrimPrefix(v.Target, "builtin:")
				}
				illustrations = append(illustrations, item)
			}
		}
		if len(illustrations) > 12 {
			return nil, invalid("Не более 12 иллюстраций")
		}
		for i, v := range illustrations {
			if err = a.attachMedia(ctx, tx, id, revision, in.Locale, i, v); err != nil {
				return nil, err
			}
		}
	}
	if in.Related != nil {
		if _, err = tx.Exec(ctx, `DELETE FROM aj_guide_related WHERE article_id=$1`, id); err != nil {
			return nil, err
		}
		for i, target := range in.Related {
			if !identifiers.ValidUUID(target) || target == id {
				return nil, invalid("Некорректная связанная статья")
			}
			if _, err = tx.Exec(ctx, `INSERT INTO aj_guide_related(article_id,related_id,position) VALUES($1,$2,$3)`, id, target, i); err != nil {
				return nil, err
			}
		}
	}
	_, err = tx.Exec(ctx, `UPDATE aj_guide_articles SET version=version+1,updated_at=clock_timestamp() WHERE id=$1`, id)
	return map[string]any{"id": id, "version": version + 1, "revision": revision, "locale": in.Locale, "blocks": blocks}, err
}
func (a *Service) attachMedia(ctx context.Context, tx pgx.Tx, id string, revision int, lang string, index int, v Illustration) error {
	if (v.Builtin == "") == (v.Media == "") {
		return invalid("Выберите один источник изображения")
	}
	if v.Role == "" {
		v.Role = "illustration"
	}
	if v.Role != "cover" && v.Role != "hologram" && v.Role != "illustration" {
		return invalid("Некорректная роль изображения")
	}
	var builtin, uploaded *string
	if v.Builtin != "" {
		if _, ok := a.Assets[v.Builtin]; !ok {
			return invalid("Встроенный ресурс недоступен")
		}
		builtin = &v.Builtin
	} else {
		if !identifiers.ValidUUID(v.Media) || v.Role == "hologram" {
			return invalid("Для голограмм доступны только встроенные ресурсы")
		}
		var ready bool
		if err := tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM aj_guide_media WHERE id=$1 AND state='ready')`, v.Media).Scan(&ready); err != nil {
			return err
		}
		if !ready {
			return invalid("Изображение недоступно")
		}
		uploaded = &v.Media
	}
	_, err := tx.Exec(ctx, `INSERT INTO aj_guide_revision_media(article_id,revision,locale,position,role,uploaded_media_id,builtin_asset_key,caption) VALUES($1,$2,$3,$4,$5,$6,$7,$8)`, id, revision, lang, index, v.Role, uploaded, builtin, v.Caption)
	return err
}

func (a *Service) Publish(w http.ResponseWriter, r *http.Request) {
	a.mutate(w, r, func(ctx context.Context, tx pgx.Tx, actor int64, in Edit) (any, error) {
		if in.Locale != "ru" && in.Locale != "en" {
			return nil, invalid("Выберите ru или en")
		}
		var version int64
		if err := tx.QueryRow(ctx, `SELECT version FROM aj_guide_articles WHERE id=$1 FOR UPDATE`, in.ID).Scan(&version); err != nil {
			return nil, err
		}
		if version != in.Expected {
			return nil, stale()
		}
		if in.Action == "unpublish" {
			if _, err := tx.Exec(ctx, `DELETE FROM aj_guide_publications WHERE article_id=$1 AND locale=$2`, in.ID, in.Locale); err != nil {
				return nil, err
			}
		} else if in.Action == "publish" {
			var source string
			if err := tx.QueryRow(ctx, `SELECT markdown_source FROM aj_guide_article_revisions WHERE article_id=$1 AND revision=$2 AND locale=$3`, in.ID, in.Revision, in.Locale).Scan(&source); err != nil {
				return nil, err
			}
			blocks, err := Parse(source)
			if err != nil {
				return nil, invalid(err.Error())
			}
			if err = a.checkReferences(ctx, tx, blocks); err != nil {
				return nil, err
			}
			if _, err = tx.Exec(ctx, `INSERT INTO aj_guide_publications(article_id,locale,revision,published_by) VALUES($1,$2,$3,$4) ON CONFLICT(article_id,locale) DO UPDATE SET revision=EXCLUDED.revision,published_by=EXCLUDED.published_by,published_at=clock_timestamp()`, in.ID, in.Locale, in.Revision, actor); err != nil {
				return nil, err
			}
		} else {
			return nil, invalid("Выберите публикацию или снятие публикации")
		}
		_, err := tx.Exec(ctx, `INSERT INTO aj_guide_publication_history(article_id,locale,revision,operation,actor_id) VALUES($1,$2,$3,$4,$5)`, in.ID, in.Locale, in.Revision, in.Action, actor)
		if err == nil {
			_, err = tx.Exec(ctx, `UPDATE aj_guide_articles SET version=version+1 WHERE id=$1`, in.ID)
		}
		return map[string]any{"id": in.ID, "version": version + 1}, err
	})
}
func (a *Service) Revisions(w http.ResponseWriter, r *http.Request) {
	if _, ok := a.admin(w, r); !ok {
		return
	}
	id := r.URL.Query().Get("article_id")
	if !identifiers.ValidUUID(id) {
		httpkit.Fail(w, 400, "Некорректный UUID")
		return
	}
	rows, err := postgres.QueryMaps(r.Context(), a.DB, `SELECT * FROM aj_guide_article_revisions WHERE article_id=$1 ORDER BY revision DESC,locale LIMIT 100`, id)
	if err != nil {
		failure(w, err)
		return
	}
	for _, v := range rows {
		blocks, e := Parse(fmtValue(v["markdown_source"]))
		if e != nil {
			failure(w, e)
			return
		}
		v["blocks"] = blocks
		media, e := a.revisionMedia(r.Context(), a.DB, id, v["revision"], fmtValue(v["locale"]))
		if e != nil {
			failure(w, e)
			return
		}
		v["media"] = media
	}
	pubs, err := postgres.QueryMaps(r.Context(), a.DB, `SELECT * FROM aj_guide_publications WHERE article_id=$1 ORDER BY locale`, id)
	if err != nil {
		failure(w, err)
		return
	}
	history, err := postgres.QueryMaps(r.Context(), a.DB, `SELECT * FROM aj_guide_publication_history WHERE article_id=$1 ORDER BY id DESC LIMIT 100`, id)
	if err != nil {
		failure(w, err)
		return
	}
	related, err := postgres.QueryMaps(r.Context(), a.DB, `SELECT related_id::text,position FROM aj_guide_related WHERE article_id=$1 ORDER BY position`, id)
	if err != nil {
		failure(w, err)
		return
	}
	httpkit.Reply(w, 200, map[string]any{"items": rows, "publications": pubs, "history": history, "related": related})
}
func (a *Service) Preview(w http.ResponseWriter, r *http.Request) {
	if _, ok := a.admin(w, r); !ok {
		return
	}
	var in Edit
	if !httpkit.Decode(w, r, &in) {
		return
	}
	blocks, err := Parse(in.Markdown)
	if err == nil {
		err = a.checkReferences(r.Context(), a.DB, blocks)
	}
	if err != nil {
		failure(w, invalid(err.Error()))
		return
	}
	httpkit.Reply(w, 200, map[string]any{"blocks": blocks})
}
