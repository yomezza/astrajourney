package knowledge

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"image"
	_ "image/jpeg"
	"image/png"
	"io"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/yomezza/cosmosimulator/internal/platform/httpkit"
	"github.com/yomezza/cosmosimulator/internal/platform/idempotency"
	"github.com/yomezza/cosmosimulator/internal/platform/identifiers"
	"github.com/yomezza/cosmosimulator/internal/platform/postgres"
	_ "golang.org/x/image/webp"
)

const mediaLimit = 8 * 1024 * 1024

type limitWriter struct {
	w io.Writer
	n int64
}

func (l *limitWriter) Write(p []byte) (int, error) {
	if int64(len(p)) > l.n {
		return 0, errors.New("encoded image too large")
	}
	n, e := l.w.Write(p)
	l.n -= int64(n)
	return n, e
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
	if err != nil {
		failure(w, err)
		return
	}
	if len(raw) > mediaLimit {
		httpkit.Fail(w, 413, "Изображение слишком большое")
		return
	}
	config, format, err := image.DecodeConfig(bytes.NewReader(raw))
	if err != nil || (format != "png" && format != "jpeg" && format != "webp") {
		httpkit.Fail(w, 415, "Принимаются PNG, JPEG и WebP; загрузка SVG пока недоступна")
		return
	}
	if config.Width < 1 || config.Height < 1 || config.Width > 4096 || config.Height > 4096 || bytes.Contains(raw, []byte("acTL")) || bytes.Contains(raw, []byte("ANIM")) {
		httpkit.Fail(w, 422, "Изображение должно быть статичным и не более 4096 × 4096")
		return
	}
	decoded, actual, err := image.Decode(bytes.NewReader(raw))
	if err != nil || actual != format {
		httpkit.Fail(w, 422, "Повреждённое изображение")
		return
	}
	if err = os.MkdirAll(a.MediaDir, 0700); err != nil {
		failure(w, err)
		return
	}
	staging, err := os.CreateTemp(a.MediaDir, ".staging-")
	if err != nil {
		failure(w, err)
		return
	}
	defer os.Remove(staging.Name())
	hash := sha256.New()
	writer := &limitWriter{io.MultiWriter(staging, hash), mediaLimit}
	if err = png.Encode(writer, decoded); err != nil {
		staging.Close()
		httpkit.Fail(w, 413, "Перекодированное изображение слишком большое")
		return
	}
	if err = staging.Sync(); err != nil {
		staging.Close()
		failure(w, err)
		return
	}
	info, err := staging.Stat()
	staging.Close()
	if err != nil {
		failure(w, err)
		return
	}
	sum := hex.EncodeToString(hash.Sum(nil))
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
		_, err = tx.Exec(ctx, `INSERT INTO aj_guide_media(id,source_format,served_format,sha256,byte_size,pixel_width,pixel_height,object_key,state,alt_ru,alt_en,uploaded_by,request_hash) VALUES($1,$2,'png',$3,$4,$5,$6,$1,'pending',$7,$8,$9,$10)`, operation, format, sum, info.Size(), config.Width, config.Height, altRU, altEN, u.ID, digest)
	}
	if err == nil {
		err = tx.Commit(ctx)
	}
	if err != nil {
		failure(w, err)
		return
	}
	target := filepath.Join(a.MediaDir, operation+".png")
	if err = os.Rename(staging.Name(), target); err != nil {
		// Windows refuses to rename over an existing file. A concurrent retry of
		// the same committed intent may already have published this exact PNG.
		file, openErr := os.Open(target)
		if openErr != nil {
			failure(w, err)
			return
		}
		existing := sha256.New()
		n, readErr := io.Copy(existing, io.LimitReader(file, mediaLimit+1))
		file.Close()
		if readErr != nil || n > mediaLimit || hex.EncodeToString(existing.Sum(nil)) != sum {
			failure(w, err)
			return
		}
	}
	// Metadata pending + durable atomic file first; public reads require ready.
	_, err = a.DB.Exec(ctx, `UPDATE aj_guide_media SET state='ready' WHERE id=$1 AND state='pending'`, operation)
	if err != nil {
		failure(w, err)
		return
	}
	audit, _ := jsonMediaAudit(operation, sum)
	_, err = a.DB.Exec(ctx, `INSERT INTO admin_log(actor,action,data) VALUES($1,'knowledge_media_upload',$2)`, u.ID, audit)
	if err != nil {
		failure(w, err)
		return
	}
	httpkit.Reply(w, 200, map[string]any{"id": operation, "state": "ready", "sha256": sum, "width": config.Width, "height": config.Height})
}
func jsonMediaAudit(id, hash string) ([]byte, error) {
	return []byte(`{"id":"` + id + `","sha256":"` + hash + `"}`), nil
}
func (a *Service) MediaView(w http.ResponseWriter, r *http.Request) {
	u, ok := a.Auth.User(w, r)
	if !ok {
		return
	}
	id := r.PathValue("id")
	if !identifiers.ValidUUID(id) {
		httpkit.Fail(w, 404, "Изображение не найдено")
		return
	}
	var object, hash string
	var visible bool
	err := a.DB.QueryRow(r.Context(), visibleCategories+`SELECT m.object_key::text,m.sha256,($2 OR EXISTS(SELECT 1 FROM aj_guide_revision_media x JOIN aj_guide_publications p ON (p.article_id,p.revision,p.locale)=(x.article_id,x.revision,x.locale) JOIN aj_guide_articles a ON a.id=p.article_id JOIN visible c ON c.id=a.category_id WHERE x.uploaded_media_id=m.id AND a.status='active')) FROM aj_guide_media m WHERE m.id=$1 AND m.state='ready'`, id, u.Role == "admin").Scan(&object, &hash, &visible)
	if err != nil || !visible || !identifiers.ValidUUID(object) {
		httpkit.Fail(w, 404, "Изображение не найдено")
		return
	}
	file, err := os.Open(filepath.Join(a.MediaDir, object+".png"))
	if err != nil {
		httpkit.Fail(w, 404, "Изображение недоступно")
		return
	}
	defer file.Close()
	info, err := file.Stat()
	if err != nil {
		failure(w, err)
		return
	}
	w.Header().Set("Content-Type", "image/png")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.Header().Set("Content-Security-Policy", "sandbox; default-src 'none'")
	w.Header().Set("Cache-Control", "private, max-age=300")
	w.Header().Set("ETag", `"`+hash+`"`)
	http.ServeContent(w, r, "image.png", info.ModTime(), file)
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
	})
}

// Maintain removes only explicitly deleted, unreferenced media after 30 days,
// and stale staging/orphan files after a day. Published revision files survive.
func (a *Service) Maintain(ctx context.Context) error {
	// Repair a committed pending intent only when its durable PNG matches the
	// stored digest. A missing ready file is quarantined, never replaced by input.
	items, e := postgres.QueryMaps(ctx, a.DB, `SELECT id::text,object_key::text,sha256,state FROM aj_guide_media WHERE state IN('pending','ready') ORDER BY last_verified_at NULLS FIRST,uploaded_at LIMIT 200`)
	if e != nil {
		return e
	}
	for _, item := range items {
		object := fmtValue(item["object_key"])
		if !identifiers.ValidUUID(object) {
			continue
		}
		file, readErr := os.Open(filepath.Join(a.MediaDir, object+".png"))
		if readErr != nil {
			if !os.IsNotExist(readErr) {
				return readErr
			}
			if _, e = a.DB.Exec(ctx, `UPDATE aj_guide_media SET last_verified_at=clock_timestamp(),state=CASE WHEN state='ready' OR uploaded_at<clock_timestamp()-interval '24 hours' THEN 'quarantined' ELSE state END WHERE id=$1 AND state IN('pending','ready')`, item["id"]); e != nil {
				return e
			}
			continue
		}
		hash := sha256.New()
		length, readErr := io.Copy(hash, io.LimitReader(file, mediaLimit+1))
		file.Close()
		if readErr != nil {
			return readErr
		}
		state := "ready"
		if length > mediaLimit || hex.EncodeToString(hash.Sum(nil)) != item["sha256"] {
			state = "quarantined"
		}
		if _, e = a.DB.Exec(ctx, `UPDATE aj_guide_media SET state=$2,last_verified_at=clock_timestamp() WHERE id=$1 AND state IN('pending','ready')`, item["id"], state); e != nil {
			return e
		}
	}
	entries, err := os.ReadDir(a.MediaDir)
	if os.IsNotExist(err) {
		return nil
	}
	if err != nil {
		return err
	}
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		info, e := entry.Info()
		if e != nil || time.Since(info.ModTime()) < 24*time.Hour {
			continue
		}
		name := entry.Name()
		remove := strings.HasPrefix(name, ".staging-")
		if strings.HasSuffix(name, ".png") && identifiers.ValidUUID(strings.TrimSuffix(name, ".png")) {
			var exists, deleted bool
			e = a.DB.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM aj_guide_media WHERE object_key=$1),EXISTS(SELECT 1 FROM aj_guide_media m WHERE object_key=$1 AND state='deleted' AND deleted_at<clock_timestamp()-interval '30 days' AND NOT EXISTS(SELECT 1 FROM aj_guide_revision_media x WHERE x.uploaded_media_id=m.id))`, strings.TrimSuffix(name, ".png")).Scan(&exists, &deleted)
			if e != nil {
				return e
			}
			remove = !exists || deleted
		}
		if remove {
			if e = os.Remove(filepath.Join(a.MediaDir, name)); e != nil && !os.IsNotExist(e) {
				return e
			}
		}
	}
	return nil
}

func (a *Service) Run(ctx context.Context) {
	timer := time.NewTicker(time.Hour)
	defer timer.Stop()
	for {
		bounded, cancel := context.WithTimeout(ctx, 30*time.Second)
		if err := a.Maintain(bounded); err != nil && ctx.Err() == nil {
			log.Printf("Knowledge media maintenance: %v", err)
		}
		cancel()
		select {
		case <-ctx.Done():
			return
		case <-timer.C:
		}
	}
}
