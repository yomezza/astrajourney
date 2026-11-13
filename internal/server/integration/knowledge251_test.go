package integration

import (
	"bytes"
	"encoding/json"
	"image"
	"image/png"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func json251(t *testing.T, w *httptest.ResponseRecorder) map[string]any {
	t.Helper()
	var m map[string]any
	if e := json.Unmarshal(w.Body.Bytes(), &m); e != nil {
		t.Fatal(e)
	}
	return m
}
func TestKnowledgePublishing251(t *testing.T) {
	f := newFixture033(t)
	admin, aid := f.signup("editor251")
	player, _ := f.signup("reader251")
	f.exec(`UPDATE users SET role='admin' WHERE id=$1`, aid)
	post := func(path string, body map[string]any, status int) map[string]any {
		body["operation"] = uuid250(t)
		return json251(t, f.req(admin, "POST", "/api/admin/guide/"+path, body, status))
	}
	category := post("categories", map[string]any{"slug": "flight", "ru": map[string]any{"title": "Полёты"}, "en": map[string]any{"title": "Flight"}}, 200)["id"].(string)
	item := post("articles", map[string]any{"slug": "first-flight", "category": category, "locale": "ru", "title": "Первый полёт", "summary": "Русская аннотация", "markdown": "## Старт\nВнутритекстовыйуникальныйпоиск и **маршрут**."}, 200)
	id := item["id"].(string)
	version := item["version"].(float64)
	revision := item["revision"]
	f.req(player, "GET", "/api/guide/articles/"+id, nil, 404)
	f.req(player, "POST", "/api/admin/guide/articles", map[string]any{"operation": uuid250(t)}, 403)
	f.req(player, "GET", "/api/admin/guide/assets", nil, 403)
	pub := post("publish", map[string]any{"id": id, "action": "publish", "locale": "ru", "revision": revision, "expected_revision": version}, 200)
	version = pub["version"].(float64)
	fallback := json251(t, f.req(player, "GET", "/api/guide/articles/"+id+"?lang=en", nil, 200))
	if fallback["actual_locale"] != "ru" || fallback["fallback"] != true || fallback["title"] != "Первый полёт" {
		t.Fatal("whole-locale fallback failed", fallback)
	}
	search := json251(t, f.req(player, "GET", "/api/guide/catalog?q=Внутритекстовыйуникальныйпоиск", nil, 200))
	if search["count"] != float64(1) {
		t.Fatal("published body search", search)
	}
	draft := post("articles", map[string]any{"id": id, "expected_revision": version, "locale": "en", "title": "First flight", "summary": "English summary", "markdown": "## Start\nPlot a route."}, 200)
	version = draft["version"].(float64)
	post("publish", map[string]any{"id": id, "action": "publish", "locale": "en", "revision": draft["revision"], "expected_revision": version - 1}, 409)
	pub = post("publish", map[string]any{"id": id, "action": "publish", "locale": "en", "revision": draft["revision"], "expected_revision": version}, 200)
	version = pub["version"].(float64)
	translated := json251(t, f.req(player, "GET", "/api/guide/articles/"+id+"?lang=en", nil, 200))
	if translated["actual_locale"] != "en" || translated["fallback"] != false || translated["title"] != "First flight" {
		t.Fatal(translated)
	}
	draft = post("articles", map[string]any{"id": id, "expected_revision": version, "locale": "ru", "title": "Неопубликованный черновик", "markdown": "Секретный черновик"}, 200)
	version = draft["version"].(float64)
	published := json251(t, f.req(player, "GET", "/api/guide/articles/"+id+"?lang=ru", nil, 200))
	if published["title"] != "Первый полёт" {
		t.Fatal("draft leaked")
	}
	post("articles", map[string]any{"id": id, "expected_revision": version - 1, "locale": "ru", "title": "Lost update", "markdown": "Wrong"}, 409)
	if _, e := f.db.Exec(f.ctx, `UPDATE aj_guide_article_revisions SET title='tampered' WHERE article_id=$1`, id); e == nil {
		t.Fatal("revision was mutable")
	}
	post("categories", map[string]any{"id": category, "action": "delete", "expected_revision": 1}, 409)
	for _, source := range []string{"<img src=x onerror=alert(1)>", "[x](javascript:alert)", "![x](data:evil)", "![x](builtin:../../secret)"} {
		post("preview", map[string]any{"markdown": source}, 422)
	}
	restored := post("articles", map[string]any{"id": id, "action": "restore_revision", "expected_revision": version, "locale": "ru", "revision": 1}, 200)
	version = restored["version"].(float64)
	if f.integer(`SELECT count(*) FROM aj_guide_article_revisions WHERE article_id=$1`, id) != 4 {
		t.Fatal("restore overwrote history")
	}
	archived := post("articles", map[string]any{"id": id, "action": "archive", "expected_revision": version}, 200)
	version = archived["version"].(float64)
	f.req(player, "GET", "/api/guide/articles/"+id, nil, 404)
	version = post("articles", map[string]any{"id": id, "action": "restore", "expected_revision": version}, 200)["version"].(float64)
	f.req(player, "GET", "/api/guide/articles/"+id+"?lang=en", nil, 200)
	version = post("articles", map[string]any{"id": id, "action": "hide", "expected_revision": version}, 200)["version"].(float64)
	f.req(player, "GET", "/api/guide/articles/"+id, nil, 404)
	post("articles", map[string]any{"id": id, "action": "unhide", "expected_revision": version}, 200)
	f.req(player, "GET", "/api/guide/articles/"+id, nil, 200)
	// A category cannot move below its descendant; descendants also stay hidden
	// when an ancestor is archived, without deleting articles or publications.
	child := post("categories", map[string]any{"slug": "child", "parent": category, "ru": map[string]any{"title": "Подкатегория"}}, 200)["id"]
	post("categories", map[string]any{"id": category, "slug": "flight", "parent": child, "expected_revision": 1, "ru": map[string]any{"title": "Полёты"}}, 409)
	post("categories", map[string]any{"id": category, "slug": "flight", "status": "archived", "expected_revision": 1, "ru": map[string]any{"title": "Полёты"}}, 200)
	f.req(player, "GET", "/api/guide/articles/"+id+"?lang=en", nil, 404)
	if f.integer(`SELECT count(*) FROM aj_guide_articles WHERE id=$1`, id) != 1 || f.integer(`SELECT count(*) FROM aj_guide_publications WHERE article_id=$1`, id) != 2 {
		t.Fatal("category archive lost content")
	}
	// Independently valid edits of two roots must serialize: the second attempt
	// to make them each other's parent must not commit a cycle.
	a := post("categories", map[string]any{"slug": "race-a", "ru": map[string]any{"title": "Ветка А"}}, 200)["id"]
	b := post("categories", map[string]any{"slug": "race-b", "ru": map[string]any{"title": "Ветка Б"}}, 200)["id"]
	statuses := make(chan int, 2)
	for i, ids := range [][2]any{{a, b}, {b, a}} {
		raw, _ := json.Marshal(map[string]any{"operation": uuid250(t), "id": ids[0], "parent": ids[1], "slug": []string{"race-a", "race-b"}[i], "expected_revision": 1, "ru": map[string]any{"title": "Ветка"}})
		go func() {
			r := httptest.NewRequest("POST", "/api/admin/guide/categories", bytes.NewReader(raw))
			r.AddCookie(admin)
			r.Header.Set("Content-Type", "application/json")
			w := httptest.NewRecorder()
			f.handler.ServeHTTP(w, r)
			statuses <- w.Code
		}()
	}
	x, y := <-statuses, <-statuses
	if !((x == 200 && y == 409) || (x == 409 && y == 200)) {
		t.Fatal("concurrent category cycle", x, y)
	}
}

func TestKnowledgeMedia251(t *testing.T) {
	f := newFixture033(t)
	admin, uid := f.signup("media251")
	player, _ := f.signup("mediareader251")
	f.exec(`UPDATE users SET role='admin' WHERE id=$1`, uid)
	f.app.Knowledge.MediaDir = t.TempDir()
	upload := func(c *http.Cookie, op string, body []byte, status int) map[string]any {
		t.Helper()
		var content bytes.Buffer
		form := multipart.NewWriter(&content)
		form.WriteField("operation", op)
		form.WriteField("alt_ru", "Тестовое изображение")
		file, _ := form.CreateFormFile("file", "../../untrusted.svg")
		file.Write(body)
		form.Close()
		r := httptest.NewRequest("POST", "/api/admin/guide/media", &content)
		r.Header.Set("Content-Type", form.FormDataContentType())
		r.AddCookie(c)
		w := httptest.NewRecorder()
		f.handler.ServeHTTP(w, r)
		if w.Code != status {
			t.Fatalf("upload %d != %d: %s", w.Code, status, w.Body.String())
		}
		return json251(t, w)
	}
	var pngFile bytes.Buffer
	png.Encode(&pngFile, image.NewRGBA(image.Rect(0, 0, 16, 16)))
	operation := uuid250(t)
	result := upload(admin, operation, pngFile.Bytes(), 200)
	upload(admin, operation, pngFile.Bytes(), 200)
	upload(player, uuid250(t), pngFile.Bytes(), 403)
	upload(admin, uuid250(t), []byte(`<svg onload="alert(1)"></svg>`), 415)
	upload(admin, operation, []byte("<script>bad</script>"), 415)
	id := result["id"].(string)
	f.req(player, "GET", "/api/guide/media/"+id, nil, 404)
	response := f.req(admin, "GET", "/api/guide/media/"+id, nil, 200)
	if !strings.HasPrefix(response.Header().Get("Content-Type"), "image/png") {
		t.Fatal("raw input served")
	}
	if _, _, e := image.DecodeConfig(bytes.NewReader(response.Body.Bytes())); e != nil {
		t.Fatal(e)
	}
	f.exec(`UPDATE aj_guide_media SET state='pending' WHERE id=$1`, id)
	if err := f.app.Knowledge.Maintain(f.ctx); err != nil {
		t.Fatal(err)
	}
	if f.integer(`SELECT count(*) FROM aj_guide_media WHERE id=$1 AND state='ready'`, id) != 1 {
		t.Fatal("pending file was not repaired")
	}
	if err := os.Remove(filepath.Join(f.app.Knowledge.MediaDir, id+".png")); err != nil {
		t.Fatal(err)
	}
	if err := f.app.Knowledge.Maintain(f.ctx); err != nil {
		t.Fatal(err)
	}
	if f.integer(`SELECT count(*) FROM aj_guide_media WHERE id=$1 AND state='quarantined'`, id) != 1 {
		t.Fatal("missing ready file was not quarantined")
	}
	f.req(admin, "GET", "/api/guide/media/"+id, nil, 404)
}
