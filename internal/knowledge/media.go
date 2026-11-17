package knowledge

import (
	"bytes"
	"context"
	"crypto/sha256"
	"errors"
	"github.com/jackc/pgx/v5"
	"github.com/yomezza/cosmosimulator/internal/platform/gamesignal"
	"github.com/yomezza/cosmosimulator/internal/platform/httpkit"
	"github.com/yomezza/cosmosimulator/internal/platform/idempotency"
	"github.com/yomezza/cosmosimulator/internal/platform/identifiers"
	"github.com/yomezza/cosmosimulator/internal/platform/mediastore"
	"github.com/yomezza/cosmosimulator/internal/platform/postgres"
	"io"
	"log"
	"net/http"
	"time"
)

const mediaLimit = mediastore.Limit

func mediaFailure(w http.ResponseWriter, err error) {
	var e *mediastore.Error
	if errors.As(err, &e) {
		switch e.Status {
		case 409, 413, 415, 422, 429:
			httpkit.Fail(w, e.Status, "Изображение не принято CDN")
			return
		}
	}
	httpkit.Fail(w, 503, "Хранилище изображений недоступно")
}
func (a *Service) emitMedia(id, state string) {
	if a.Signals != nil {
		if message, ok := gamesignal.EncodeMedia(id, state); ok {
			a.Signals(message)
		}
	}
}
func (a *Service) Media(w http.ResponseWriter, r *http.Request) {
	u, ok := a.admin(w, r)
	if !ok {
		return
	}
	if r.Method == "GET" {
		items, err := postgres.QueryMaps(r.Context(), a.DB, `SELECT id::text,state,source_format,served_format,sha256,byte_size,pixel_width,pixel_height,alt_ru,alt_en,uploaded_at FROM aj_guide_media ORDER BY uploaded_at DESC LIMIT 100`)
		if err != nil {
			failure(w, err)
			return
		}
		httpkit.Reply(w, 200, map[string]any{"items": items})
		return
	}
	select {
	case a.uploads <- struct{}{}:
		defer func() { <-a.uploads }()
	default:
		httpkit.Fail(w, 429, "Обработка изображений занята")
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, mediaLimit+64*1024)
	if err := r.ParseMultipartForm(mediaLimit); err != nil {
		httpkit.Fail(w, 413, "Изображение слишком большое")
		return
	}
	if r.MultipartForm != nil {
		defer r.MultipartForm.RemoveAll()
	}
	operation := r.FormValue("operation")
	if !identifiers.ValidUUID(operation) {
		httpkit.Fail(w, 400, "Нужен UUID операции")
		return
	}
	altRU, altEN := r.FormValue("alt_ru"), r.FormValue("alt_en")
	if len([]rune(altRU)) > 300 || len([]rune(altEN)) > 300 {
		httpkit.Fail(w, 422, "Слишком длинная подпись")
		return
	}
	input, _, err := r.FormFile("file")
	if err != nil {
		httpkit.Fail(w, 400, "Выберите изображение")
		return
	}
	defer input.Close()
	raw, err := io.ReadAll(io.LimitReader(input, mediaLimit+1))
	if err != nil || int64(len(raw)) > mediaLimit {
		httpkit.Fail(w, 413, "Изображение слишком большое")
		return
	}
	// The CDN owns decoding, normalization, hashes, durable files and receipts.
	receipt, err := a.MediaStore.Do(r.Context(), "PUT", operation, bytes.NewReader(raw))
	if err != nil {
		mediaFailure(w, err)
		return
	}
	digest := idempotency.Digest(struct {
		Input  [32]byte
		RU, EN string
	}{sha256.Sum256(raw), altRU, altEN})
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
	var old, state string
	var actor *int64
	err = tx.QueryRow(ctx, `SELECT request_hash,state,uploaded_by FROM aj_guide_media WHERE id=$1 FOR UPDATE`, operation).Scan(&old, &state, &actor)
	if err == nil {
		if old != digest || actor == nil || *actor != u.ID {
			httpkit.Fail(w, 409, "Идентификатор операции уже использован")
			return
		}
		if state == "ready" {
			httpkit.Reply(w, 200, map[string]any{"id": operation, "state": state})
			return
		}
		if state != "pending" {
			httpkit.Fail(w, 409, "Изображение недоступно")
			return
		}
	} else if errors.Is(err, pgx.ErrNoRows) {
		_, err = tx.Exec(ctx, `INSERT INTO aj_guide_media(id,source_format,served_format,sha256,byte_size,pixel_width,pixel_height,object_key,state,alt_ru,alt_en,uploaded_by,request_hash) VALUES($1,$2,'png',$3,$4,$5,$6,$1,'pending',$7,$8,$9,$10)`, operation, receipt.SourceFormat, receipt.SHA256, receipt.Size, receipt.Width, receipt.Height, altRU, altEN, u.ID, digest)
	}
	if err != nil {
		failure(w, err)
		return
	}
	// Durable CDN receipt first, publication metadata and audit in one transaction.
	_, err = tx.Exec(ctx, `UPDATE aj_guide_media SET state='ready' WHERE id=$1 AND state='pending' AND sha256=$2`, operation, receipt.SHA256)
	if err == nil {
		audit, _ := jsonMediaAudit(operation, receipt.SHA256)
		_, err = tx.Exec(ctx, `INSERT INTO admin_log(actor,action,data) VALUES($1,'knowledge_media_upload',$2)`, u.ID, audit)
	}
	if err == nil {
		err = tx.Commit(ctx)
	}
	if err != nil {
		failure(w, err)
		return
	}
	a.emitMedia(operation, "ready")
	httpkit.Reply(w, 200, map[string]any{"id": operation, "state": "ready", "sha256": receipt.SHA256, "width": receipt.Width, "height": receipt.Height})
}
func jsonMediaAudit(id, hash string) ([]byte, error) {
	return []byte(`{"id":"` + id + `","sha256":"` + hash + `"}`), nil
}

// MediaAuthorize is service-authenticated and returns metadata, never image bytes.
func (a *Service) MediaAuthorize(w http.ResponseWriter, r *http.Request) {
	token, err := mediastore.Token(a.MediaStore.TokenFile)
	if err != nil || !mediastore.Authorized(r, token) {
		httpkit.Fail(w, 403, "service authentication required")
		return
	}
	u, ok := a.Auth.User(w, r)
	if !ok {
		return
	}
	id := r.PathValue("id")
	if !identifiers.ValidUUID(id) {
		http.NotFound(w, r)
		return
	}
	var object, hash string
	var visible bool
	err = a.DB.QueryRow(r.Context(), visibleCategories+`SELECT m.object_key::text,m.sha256,($2 OR EXISTS(SELECT 1 FROM aj_guide_revision_media x JOIN aj_guide_publications p ON (p.article_id,p.revision,p.locale)=(x.article_id,x.revision,x.locale) JOIN aj_guide_articles a ON a.id=p.article_id JOIN visible c ON c.id=a.category_id WHERE x.uploaded_media_id=m.id AND a.status='active')) FROM aj_guide_media m WHERE m.id=$1 AND m.state='ready'`, id, u.Role == "admin").Scan(&object, &hash, &visible)
	if err != nil || !visible || !identifiers.ValidUUID(object) {
		http.NotFound(w, r)
		return
	}
	httpkit.Reply(w, 200, mediastore.Receipt{ID: object, SHA256: hash})
}
func (a *Service) DeleteMedia(w http.ResponseWriter, r *http.Request) {
	a.mutate(w, r, func(ctx context.Context, tx pgx.Tx, actor int64, in Edit) (any, error) {
		var used bool
		if err := tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM aj_guide_revision_media WHERE uploaded_media_id=$1)`, in.ID).Scan(&used); err != nil {
			return nil, err
		}
		if used {
			return nil, &inputError{409, "Изображение используется в истории статьи"}
		}
		var state string
		if err := tx.QueryRow(ctx, `SELECT state FROM aj_guide_media WHERE id=$1 FOR UPDATE`, in.ID).Scan(&state); err != nil {
			return nil, err
		}
		if state == "pending" {
			return nil, &inputError{409, "Обработка изображения ещё не завершена"}
		}
		tag, err := tx.Exec(ctx, `UPDATE aj_guide_media SET state='deleted',deleted_at=COALESCE(deleted_at,clock_timestamp()) WHERE id=$1`, in.ID)
		if err != nil {
			return nil, err
		}
		if tag.RowsAffected() == 0 {
			return nil, pgx.ErrNoRows
		}
		return map[string]any{"id": in.ID, "deleted": true}, nil
	}, func(result any) {
		if item, ok := result.(map[string]any); ok {
			if id, ok := item["id"].(string); ok {
				a.emitMedia(id, "deleted")
			}
		}
	})
}

// Maintain reconciles durable metadata against CDN receipts. An unreachable CDN
// is an operational error, never evidence that images disappeared.
func (a *Service) Maintain(ctx context.Context) error {
	items, err := postgres.QueryMaps(ctx, a.DB, `SELECT id::text,object_key::text,sha256,state,uploaded_at<clock_timestamp()-interval '24 hours' AS stale FROM aj_guide_media WHERE state IN('pending','ready') ORDER BY last_verified_at NULLS FIRST,uploaded_at LIMIT 200`)
	if err != nil {
		return err
	}
	for _, item := range items {
		id := fmtValue(item["id"])
		object := fmtValue(item["object_key"])
		if !identifiers.ValidUUID(object) {
			continue
		}
		receipt, readErr := a.MediaStore.Do(ctx, "GET", object, nil)
		state := "ready"
		if readErr != nil {
			var e *mediastore.Error
			if !errors.As(readErr, &e) || e.Status != 404 && e.Status != 422 {
				return readErr
			}
			state = fmtValue(item["state"])
			if e.Status == 422 || state == "ready" || item["stale"] == true {
				state = "quarantined"
			}
		} else if receipt.SHA256 != item["sha256"] {
			state = "quarantined"
		}
		if _, err = a.DB.Exec(ctx, `UPDATE aj_guide_media SET state=$2,last_verified_at=clock_timestamp() WHERE id=$1 AND state IN('pending','ready')`, id, state); err != nil {
			return err
		}
		if state != fmtValue(item["state"]) {
			a.emitMedia(id, state)
		}
	}
	deleted, err := postgres.QueryMaps(ctx, a.DB, `SELECT object_key::text,sha256 FROM aj_guide_media m WHERE state='deleted' AND deleted_at<clock_timestamp()-interval '30 days' AND NOT EXISTS(SELECT 1 FROM aj_guide_revision_media x WHERE x.uploaded_media_id=m.id) LIMIT 200`)
	if err != nil {
		return err
	}
	for _, item := range deleted {
		_, err = a.MediaStore.Do(ctx, "DELETE", fmtValue(item["object_key"])+"?sha256="+fmtValue(item["sha256"]), nil)
		if err != nil {
			return err
		}
	}
	return nil
}
func (a *Service) Run(ctx context.Context) {
	ticker := time.NewTicker(time.Hour)
	defer ticker.Stop()
	for {
		bounded, cancel := context.WithTimeout(ctx, 30*time.Second)
		err := a.Maintain(bounded)
		cancel()
		if err != nil && ctx.Err() == nil {
			log.Printf("Knowledge media maintenance: %v", err)
		}
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
	}
}
