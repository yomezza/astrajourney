package integration

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"testing"
	"time"
)

func TestCommunityIdeas(t *testing.T) {
	f := newFixture033(t)
	author, aid := f.signup("community_author")
	reader, _ := f.signup("community_reader")
	admin, mid := f.signup("community_moderator")
	f.exec(`UPDATE users SET role='admin' WHERE id=$1`, mid)

	type page struct {
		Page, Pages, Total int
		Items              []struct {
			ID          int64
			Title       string
			Description string
			Author      string
			Status      string
			PublishedAt *time.Time `json:"published_at"`
		}
	}
	get := func(cookie *http.Cookie, query string) page {
		t.Helper()
		w := f.req(cookie, "GET", "/api/ideas?"+query, nil, 200)
		for _, private := range []string{`"request_id"`, `"user_id"`, `"author_name"`} {
			if strings.Contains(w.Body.String(), private) {
				t.Fatalf("response exposed %s: %s", private, w.Body)
			}
		}
		var p page
		if err := json.Unmarshal(w.Body.Bytes(), &p); err != nil {
			t.Fatal(err)
		}
		return p
	}
	submit := func(title, description, request string) int64 {
		t.Helper()
		w := f.req(author, "POST", "/api/ideas", map[string]string{
			"title": title, "description": description, "category": "interface", "request": request,
		}, 201)
		var created struct{ ID int64 }
		if err := json.Unmarshal(w.Body.Bytes(), &created); err != nil || created.ID == 0 {
			t.Fatalf("invalid submission response: %s", w.Body)
		}
		return created.ID
	}
	const community = "scope=community"
	description := "Ёж управляет модулем.\nБуквальные символы %_\\ и <script>."
	id := submit("Тихий терминал", description, strings.Repeat("a", 32))
	f.req(nil, "GET", "/api/ideas?"+community, nil, 401)
	f.req(reader, "GET", "/api/ideas?scope=all", nil, 403)
	f.req(reader, "POST", "/api/admin/ideas", map[string]any{"id": id, "status": "approved"}, 403)
	for _, cookie := range []*http.Cookie{author, reader, admin} {
		if result := get(cookie, community); result.Total != 1 || len(result.Items) != 1 ||
			result.Items[0].ID != id || result.Items[0].Status != "pending" || result.Items[0].PublishedAt != nil {
			t.Fatalf("pending idea missing from community: %+v", result)
		}
	}
	if mine := get(author, "scope=mine"); mine.Total != 1 || mine.Items[0].Status != "pending" {
		t.Fatalf("pending idea missing from author history: %+v", mine)
	}
	if mine := get(reader, "scope=mine"); mine.Total != 0 {
		t.Fatalf("another account's history leaked: %+v", mine)
	}

	f.req(admin, "POST", "/api/admin/ideas", map[string]any{"id": id, "status": "approved"}, 200)
	for _, term := range []string{"ТЕРМИНАЛ", "ёж УПРАВЛЯЕТ", "%_\\", "<script>"} {
		for _, cookie := range []*http.Cookie{reader, author} {
			result := get(cookie, community+"&category=interface&search="+url.QueryEscape(term))
			if result.Total != 1 {
				t.Fatalf("expected one published idea for %q: %+v", term, result)
			}
			item := result.Items[0]
			if item.ID != id || item.Author != "community_author" || item.Description != description || item.Status != "approved" || item.PublishedAt == nil {
				t.Fatalf("bad public idea: %+v", item)
			}
		}
	}
	published := get(reader, community).Items[0].PublishedAt
	f.req(admin, "POST", "/api/admin/ideas", map[string]any{"id": id, "status": "approved"}, 200)
	if again := get(reader, community).Items[0].PublishedAt; !published.Equal(*again) {
		t.Fatal("an unchanged review moved the original publication date")
	}
	f.req(admin, "POST", "/api/admin/ideas", map[string]any{"id": id, "status": "pending"}, 200)
	if result := get(reader, community); result.Total != 1 || result.Items[0].Status != "pending" ||
		result.Items[0].PublishedAt != nil {
		t.Fatalf("returning an idea to review exposed an old publication date: %+v", result)
	}
	f.req(admin, "POST", "/api/admin/ideas", map[string]any{"id": id, "status": "approved"}, 200)
	published = get(reader, community).Items[0].PublishedAt
	for _, q := range []string{"search=" + url.QueryEscape("' OR 1=1 --"), "category=economy"} {
		if result := get(reader, community+"&"+q); result.Total != 0 {
			t.Fatalf("unexpected match for %q: %+v", q, result)
		}
	}

	rejected := submit("Отклонённое предложение", "Подробности второй идеи", strings.Repeat("b", 32))
	f.req(admin, "POST", "/api/admin/ideas", map[string]any{"id": rejected, "status": "rejected"}, 200)
	if result := get(author, "scope=mine&category=interface&sort=newest"); result.Total != 2 || result.Items[0].Status != "rejected" {
		t.Fatalf("rejected idea missing from author history: %+v", result)
	}
	if result := get(admin, "scope=all"); result.Total != 2 || result.Items[0].ID != rejected {
		t.Fatalf("moderation lost legacy order or access: %+v", result)
	}
	if result := get(reader, community+"&search="+url.QueryEscape("Отклонённое")); result.Total != 0 {
		t.Fatal("rejected idea appeared in public search")
	}
	f.req(admin, "POST", "/api/admin/ideas", map[string]any{"id": id, "status": "rejected"}, 200)
	if result := get(reader, community); result.Total != 0 {
		t.Fatal("unpublished idea remained visible")
	}
	f.req(admin, "POST", "/api/admin/ideas", map[string]any{"id": id, "status": "approved"}, 200)
	if again := get(reader, community).Items[0].PublishedAt; !again.After(*published) {
		t.Fatal("republication did not record the new publication date")
	}

	// Simulate an upgrade with older approved rows that predate publication tracking.
	for i := range 12 {
		f.exec(`INSERT INTO player_ideas(user_id,author_name,request_id,category,title,description,status,created_at)
			VALUES($1,'community_author',$2,'gameplay',$3,'Текст из старой версии','approved','2025-01-01T00:00:00Z')`,
			aid, fmt.Sprintf("%032x", i), fmt.Sprintf("Архивная идея %02d", i))
	}
	for i, date := range []string{"2024-01-01T00:00:00Z", "2026-01-01T00:00:00Z"} {
		f.exec(`INSERT INTO player_ideas(user_id,author_name,request_id,category,title,description,status,created_at)
			VALUES($1,'community_author',$2,'gameplay',$3,'Текст идеи на рассмотрении','pending',$4::timestamptz)`,
			aid, fmt.Sprintf("%032x", 20+i), fmt.Sprintf("Идея на рассмотрении %d", i), date)
	}
	f.exec(`ALTER TABLE player_ideas DROP COLUMN published_at`)
	f.exec(`UPDATE game_release SET version='0.31.3',migration=313 WHERE id=1`)
	var schema string
	if err := f.db.QueryRow(f.ctx, `SELECT current_schema()`).Scan(&schema); err != nil {
		t.Fatal(err)
	}
	for range 2 {
		db, err := openDatabase(f.ctx, f.db.Config().ConnString(), schema)
		if err != nil {
			t.Fatal(err)
		}
		db.Close()
		status := f.req(nil, "GET", "/api/server-status", nil, 200)
		var release struct{ Version string }
		if err := json.Unmarshal(status.Body.Bytes(), &release); err != nil || release.Version != "0.31.10" {
			t.Fatalf("unexpected version after migration: %s", status.Body)
		}
	}
	first := get(reader, community+"&category=gameplay&sort=oldest")
	last := get(reader, community+"&category=gameplay&sort=oldest&page=2")
	reverse := get(reader, community+"&category=gameplay&sort=newest")
	reverseLast := get(reader, community+"&category=gameplay&sort=newest&page=2")
	if first.Total != 14 || first.Pages != 2 || len(first.Items) != 10 ||
		last.Page != 2 || len(last.Items) != 4 || first.Items[0].Title != "Архивная идея 00" ||
		last.Items[1].Title != "Архивная идея 11" || reverse.Items[0].ID != last.Items[1].ID ||
		last.Items[2].Title != "Идея на рассмотрении 0" || last.Items[3].Title != "Идея на рассмотрении 1" ||
		len(reverseLast.Items) != 4 || reverseLast.Items[1].Status != "approved" ||
		reverseLast.Items[2].ID != last.Items[3].ID || reverseLast.Items[3].ID != last.Items[2].ID ||
		first.Items[0].PublishedAt != nil {
		t.Fatalf("bad sort, pagination or migrated data: %+v / %+v", first, last)
	}
	if hit := get(reader, community+"&search=11&page=2&sort=oldest"); hit.Total != 1 || hit.Page != 1 || hit.Items[0].Title != "Архивная идея 11" {
		t.Fatalf("search did not cover all pages: %+v", hit)
	}
	if hit := get(reader, community+"&category=gameplay&search="+url.QueryEscape("на рассмотрении")); hit.Total != 2 ||
		hit.Items[0].Status != "pending" || hit.Items[1].Status != "pending" {
		t.Fatalf("community search missed ideas under review: %+v", hit)
	}
	for _, q := range []string{"scope=invalid", community + "&category=invalid", community + "&sort=invalid",
		community + "&search=%", community + "&search=%00", community + "&search=" + strings.Repeat("x", 121)} {
		f.req(reader, "GET", "/api/ideas?"+q, nil, 400)
	}
}

func TestIdeaCategoryAdministration(t *testing.T) {
	f := newFixture033(t)
	player, _ := f.signup("category_pilot")
	admin, id := f.signup("category_admin")
	f.exec("UPDATE users SET role='admin' WHERE id=$1", id)
	const read = "/api/ideas/categories"
	const write = "/api/admin/ideas/categories"
	f.req(nil, "GET", read, nil, 401)
	if result := f.req(player, "GET", read, nil, 200); !strings.Contains(result.Body.String(), "Игровой процесс") {
		t.Fatal(result.Body.String())
	}
	data := map[string]any{"name": "Корабли", "active": true}
	f.req(player, "POST", write, data, 403)
	created := f.req(admin, "POST", write, data, 200)
	var category struct{ ID string }
	if err := json.Unmarshal(created.Body.Bytes(), &category); err != nil || category.ID == "" {
		t.Fatal(created.Body.String())
	}
	f.req(admin, "POST", write, map[string]any{"name": "КОРАБЛИ", "active": true}, 409)
	idea := map[string]string{"title": "Новая идея", "description": "Проверка новой категории", "category": category.ID, "request": strings.Repeat("e", 32)}
	f.req(player, "POST", "/api/ideas", idea, 201)
	f.req(admin, "POST", write, map[string]any{"id": category.ID, "name": "Флот", "active": false}, 200)
	f.req(player, "POST", "/api/ideas", idea, 200)
	idea["request"] = strings.Repeat("f", 32)
	f.req(player, "POST", "/api/ideas", idea, 400)
	f.req(admin, "POST", write, map[string]any{"id": "gameplay", "name": "Разведка", "active": true}, 200)
	var schema string
	if err := f.db.QueryRow(f.ctx, "SELECT current_schema()").Scan(&schema); err != nil {
		t.Fatal(err)
	}
	db, err := openDatabase(f.ctx, f.db.Config().ConnString(), schema)
	if err != nil {
		t.Fatal(err)
	}
	db.Close()
	if result := f.req(player, "GET", read, nil, 200); !strings.Contains(result.Body.String(), "Разведка") || !strings.Contains(result.Body.String(), "Флот") {
		t.Fatal(result.Body.String())
	}
	result := f.req(player, "GET", "/api/ideas?scope=community&category="+category.ID, nil, 200)
	if !strings.Contains(result.Body.String(), "Новая идея") {
		t.Fatal(result.Body.String())
	}
}
