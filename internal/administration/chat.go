package administration

import (
	"context"
	"encoding/json"
	"github.com/yomezza/cosmosimulator/internal/platform/httpkit"
	"io"
	"net/http"
	"os"
	"time"
)

func (a *Service) ChatModerator(w http.ResponseWriter, r *http.Request) {
	u, ok := a.Auth.User(w, r)
	if !ok {
		return
	}
	if u.Role != "admin" {
		httpkit.Fail(w, 403, "Требуется роль администратора")
		return
	}
	var in struct {
		User    int64 `json:"user,string"`
		Granted bool  `json:"granted"`
	}
	if !httpkit.Decode(w, r, &in) {
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 4*time.Second)
	defer cancel()
	tx, e := a.DB.Begin(ctx)
	if e != nil {
		httpkit.Internal(w, e)
		return
	}
	defer tx.Rollback(context.Background())
	var real bool
	if e = tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM users WHERE id=$1 AND NOT is_bot)`, in.User).Scan(&real); e != nil || !real {
		httpkit.Fail(w, 400, "Пилот не найден")
		return
	}
	_, e = tx.Exec(ctx, `INSERT INTO chat_game_moderator_grants(user_id,granted_by,revoked_at) VALUES($1,$2,CASE WHEN $3 THEN NULL ELSE clock_timestamp() END) ON CONFLICT(user_id) DO UPDATE SET granted_by=$2,granted_at=clock_timestamp(),revoked_at=CASE WHEN $3 THEN NULL ELSE clock_timestamp() END`, in.User, u.ID, in.Granted)
	if e == nil {
		_, e = tx.Exec(ctx, `INSERT INTO admin_log(actor,action,data) VALUES($1,'chat_moderator',jsonb_build_object('user',$2::bigint::text,'granted',$3::boolean))`, u.ID, in.User, in.Granted)
	}
	if e == nil {
		e = tx.Commit(ctx)
	}
	if e != nil {
		httpkit.Internal(w, e)
		return
	}
	httpkit.Reply(w, 200, map[string]bool{"ok": true})
}
func (a *Service) chatCard(ctx context.Context) ServiceCard {
	card := ServiceCard{ID: "chat", Name: "Служба чата", Status: "disabled", Info: []ServiceMetric{}}
	if os.Getenv("CHAT_ENABLED") != "true" {
		return card
	}
	raw := os.Getenv("CHAT_INTERNAL_URL")
	if raw == "" {
		raw = "http://127.0.0.1:8082"
	}
	request, e := http.NewRequestWithContext(ctx, "GET", raw+"/chat/api/v1/health", nil)
	if e != nil {
		card.Status = "configuration_error"
		return card
	}
	client := http.Client{Timeout: time.Second, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	response, e := client.Do(request)
	if e != nil {
		card.Status = "unreachable"
		return card
	}
	defer response.Body.Close()
	var h struct {
		Version string `json:"version"`
		Ready   bool   `json:"ready"`
		Schema  int    `json:"schema_version"`
		Cosmic  bool   `json:"cosmic"`
		Planet  bool   `json:"planet"`
	}
	if json.NewDecoder(io.LimitReader(response.Body, 4096)).Decode(&h) != nil {
		card.Status = "not_ready"
		return card
	}
	card.Status = "not_ready"
	if h.Ready {
		card.Status = "ready"
	}
	state := func(b bool) string {
		if b {
			return "online"
		}
		return "unavailable"
	}
	card.Info = []ServiceMetric{{"Версия", h.Version}, {"Cosmic", state(h.Cosmic)}, {"Planet", state(h.Planet)}}
	return card
}
