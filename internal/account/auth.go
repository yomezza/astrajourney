package account

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"net/http"
	"time"
	"unicode/utf8"

	"github.com/jackc/pgx/v5"
	"golang.org/x/crypto/bcrypt"

	"github.com/yomezza/cosmosimulator/internal/platform/configuration"
	"github.com/yomezza/cosmosimulator/internal/platform/httpkit"
)

type attempt struct {
	count int
	until time.Time
}

type User struct {
	ID       int64  `json:"id"`
	Username string `json:"username"`
	Role     string `json:"role"`
	Bot      bool   `json:"bot"`
	Credits  int64  `json:"credits"`
	Ore      int64  `json:"ore"`
}

func (a *Service) Bootstrap(ctx context.Context) error {
	name, pass := configuration.Getenv("ADMIN_USERNAME"), configuration.Getenv("ADMIN_PASSWORD")
	if name == "" && pass == "" {
		return nil
	}
	if !ValidName(name) || len(pass) < 1 || len(pass) > 72 {
		return errors.New("admin username must be 3–24 ASCII letters/digits/_/-; password 1–72 bytes")
	}
	hash, err := bcrypt.GenerateFromPassword([]byte(pass), bcrypt.DefaultCost)
	if err != nil {
		return err
	}
	var role string
	err = a.DB.QueryRow(ctx, `INSERT INTO users(username,password_hash,role) VALUES($1,$2,'admin') ON CONFLICT(username) DO UPDATE SET username=EXCLUDED.username RETURNING role`, name, string(hash)).Scan(&role)
	if err == nil && role != "admin" {
		return errors.New("ADMIN_USERNAME belongs to a player; choose another name")
	}
	return err
}

func ValidName(s string) bool {
	if len(s) < 3 || len(s) > 24 {
		return false
	}
	for _, c := range s {
		if !(c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c >= '0' && c <= '9' || c == '_' || c == '-') {
			return false
		}
	}
	return true
}

func (a *Service) Allow(r *http.Request) bool {
	a.mu.Lock()
	defer a.mu.Unlock()
	now := time.Now()
	for k, v := range a.attempts {
		if now.After(v.until) {
			delete(a.attempts, k)
		}
	}
	// Forwarded addresses are accepted only from explicitly trusted proxies.
	host := r.URL.Path + "/" + a.ClientAddress(r)
	if a.attempts == nil {
		a.attempts = map[string]attempt{}
	}
	v := a.attempts[host]
	if v.count >= 20 {
		return false
	}
	if v.count == 0 {
		v.until = now.Add(5 * time.Minute)
	}
	v.count++
	a.attempts[host] = v
	return true
}

func (a *Service) Register(w http.ResponseWriter, r *http.Request) {
	if !a.Allow(r) {
		httpkit.Fail(w, 429, "Слишком много попыток. Повторите через 5 минут.")
		return
	}
	var in struct {
		Username string `json:"username"`
		Password string `json:"password"`
	}
	if !httpkit.Decode(w, r, &in) {
		return
	}
	if !ValidName(in.Username) || !utf8.ValidString(in.Password) || len(in.Password) < 1 || len(in.Password) > 72 {
		httpkit.Fail(w, 400, "Имя: 3–24 латинских символа, цифры, _ или -. Пароль: 1–72 байта.")
		return
	}
	hash, err := bcrypt.GenerateFromPassword([]byte(in.Password), bcrypt.DefaultCost)
	if err != nil {
		httpkit.Internal(w, err)
		return
	}
	var id int64
	err = a.DB.QueryRow(r.Context(), `INSERT INTO users(username,password_hash) VALUES($1,$2) ON CONFLICT DO NOTHING RETURNING id`, in.Username, string(hash)).Scan(&id)
	if errors.Is(err, pgx.ErrNoRows) {
		httpkit.Fail(w, 409, "Имя уже занято")
		return
	}
	if err != nil {
		httpkit.Internal(w, err)
		return
	}
	a.newSession(w, r, id)
}

func (a *Service) Login(w http.ResponseWriter, r *http.Request) {
	if !a.Allow(r) {
		httpkit.Fail(w, 429, "Слишком много попыток. Повторите через 5 минут.")
		return
	}
	var in struct {
		Username string `json:"username"`
		Password string `json:"password"`
	}
	if !httpkit.Decode(w, r, &in) {
		return
	}
	var id int64
	var hash string
	err := a.DB.QueryRow(r.Context(), `SELECT id,password_hash FROM users WHERE username=$1 AND NOT is_bot`, in.Username).Scan(&id, &hash)
	if err != nil && !errors.Is(err, pgx.ErrNoRows) {
		httpkit.Internal(w, err)
		return
	}
	if errors.Is(err, pgx.ErrNoRows) {
		hash = "$2a$10$N9qo8uLOickgx2ZMRZoMyeIjZAgcfl7p92ldGxad68LJZdL17lhWy"
	}
	bad := bcrypt.CompareHashAndPassword([]byte(hash), []byte(in.Password))
	if err != nil || bad != nil {
		httpkit.Fail(w, 401, "Неверное имя или пароль")
		return
	}
	a.newSession(w, r, id)
}

func TokenHash(t string) string { h := sha256.Sum256([]byte(t)); return hex.EncodeToString(h[:]) }

func (a *Service) newSession(w http.ResponseWriter, r *http.Request, id int64) {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		httpkit.Internal(w, err)
		return
	}
	token := hex.EncodeToString(b)
	_, err := a.DB.Exec(r.Context(), `INSERT INTO sessions(token_hash,user_id,expires_at) VALUES($1,$2,now()+interval '7 days')`, TokenHash(token), id)
	if err != nil {
		httpkit.Internal(w, err)
		return
	}
	http.SetCookie(w, &http.Cookie{Name: "session", Value: token, Path: "/", HttpOnly: true, Secure: a.Secure, SameSite: http.SameSiteLaxMode, MaxAge: 7 * 24 * 3600})
	httpkit.Reply(w, 200, map[string]bool{"ok": true})
}

func (a *Service) User(w http.ResponseWriter, r *http.Request) (User, bool) {
	var u User
	c, err := r.Cookie("session")
	if err != nil || len(c.Value) != 64 {
		httpkit.Fail(w, 401, "Войдите в аккаунт")
		return u, false
	}
	err = a.DB.QueryRow(r.Context(), `SELECT u.id,u.username,u.role,u.is_bot,u.credits,u.ore FROM users u JOIN sessions s ON s.user_id=u.id WHERE s.token_hash=$1 AND s.expires_at>now()`, TokenHash(c.Value)).Scan(&u.ID, &u.Username, &u.Role, &u.Bot, &u.Credits, &u.Ore)
	if errors.Is(err, pgx.ErrNoRows) {
		httpkit.Fail(w, 401, "Сессия истекла")
		return u, false
	}
	if err != nil {
		httpkit.Internal(w, err)
		return u, false
	}
	return u, true
}

func (a *Service) Logout(w http.ResponseWriter, r *http.Request) {
	if c, err := r.Cookie("session"); err == nil {
		if _, err = a.DB.Exec(r.Context(), `DELETE FROM sessions WHERE token_hash=$1`, TokenHash(c.Value)); err != nil {
			httpkit.Internal(w, err)
			return
		}
	}
	http.SetCookie(w, &http.Cookie{Name: "session", Path: "/", HttpOnly: true, Secure: a.Secure, SameSite: http.SameSiteLaxMode, MaxAge: -1})
	httpkit.Reply(w, 200, map[string]bool{"ok": true})
}
