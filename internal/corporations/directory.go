package corporations

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"regexp"
	"strings"
	"unicode"

	"github.com/jackc/pgx/v5"

	"github.com/yomezza/cosmosimulator/internal/gameplay"
	"github.com/yomezza/cosmosimulator/internal/platform/httpkit"
	"github.com/yomezza/cosmosimulator/internal/wallet"
)

type Company struct {
	ID         int64           `json:"id"`
	Name       string          `json:"name"`
	Tag        string          `json:"tag"`
	Color      string          `json:"color"`
	Flag       string          `json:"flag"`
	Owner      *int64          `json:"owner"`
	Starter    bool            `json:"starter"`
	Members    int             `json:"members"`
	Founder    bool            `json:"founder"`
	Credits    int64           `json:"-"`
	FlagDesign json.RawMessage `json:"flag_design"`
}

func ValidCompanyTag(s string) bool {
	runes := []rune(s)
	if len(runes) < 3 || len(runes) > 4 {
		return false
	}
	for _, r := range runes {
		if !unicode.IsLetter(r) && !unicode.IsDigit(r) {
			return false
		}
	}
	return true
}

var colorPattern = regexp.MustCompile(`^#[0-9a-fA-F]{6}$`)

func (a *Service) CompanyList(w http.ResponseWriter, r *http.Request) {
	u, ok := a.Auth.User(w, r)
	if !ok {
		return
	}
	scope := r.URL.Query().Get("scope")
	var star int
	err := a.DB.QueryRow(r.Context(), `SELECT COALESCE((SELECT CASE WHEN arrival<=now() THEN destination ELSE star END FROM ships WHERE user_id=$1 AND active),1)`, u.ID).Scan(&star)
	if err != nil {
		httpkit.Internal(w, err)
		return
	}
	g, err := a.World.Load(r.Context())
	if err != nil {
		httpkit.Internal(w, err)
		return
	}
	rows, err := a.DB.Query(r.Context(), `SELECT c.id,c.name,c.tag,c.color,c.flag,c.owner_id,c.starter,count(u.id)::int,c.founder,c.credits,c.flag_design FROM companies c LEFT JOIN users u ON u.company_id=c.id GROUP BY c.id ORDER BY c.founder DESC,c.starter DESC,c.id LIMIT 500`)
	if err != nil {
		httpkit.Internal(w, err)
		return
	}
	list := []Company{}
	for rows.Next() {
		var c Company
		if err = rows.Scan(&c.ID, &c.Name, &c.Tag, &c.Color, &c.Flag, &c.Owner, &c.Starter, &c.Members, &c.Founder, &c.Credits, &c.FlagDesign); err != nil {
			rows.Close()
			httpkit.Internal(w, err)
			return
		}
		list = append(list, c)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		httpkit.Internal(w, err)
		return
	}
	if scope == "system" || scope == "region" {
		filtered := []Company{}
		for _, c := range list {
			var exists bool
			stars := []int{star}
			if scope == "region" {
				stars = nil
				for _, s := range g.Stars {
					if s.Region == g.Stars[star-1].Region {
						stars = append(stars, s.ID)
					}
				}
			}
			if err = a.DB.QueryRow(r.Context(), `SELECT EXISTS(SELECT 1 FROM users u LEFT JOIN ships s ON s.user_id=u.id WHERE u.company_id=$1 AND COALESCE(CASE WHEN s.arrival<=now() THEN s.destination ELSE s.star END,1)=ANY($2::int[]) AND (s.destination IS NULL OR s.arrival<=now()))`, c.ID, stars).Scan(&exists); err != nil {
				httpkit.Internal(w, err)
				return
			}
			if c.Founder && !exists {
				if err = a.DB.QueryRow(r.Context(), `SELECT EXISTS(SELECT 1 FROM stations WHERE founder=$1 AND active AND star=ANY($2::int[]))`, c.ID, stars).Scan(&exists); err != nil {
					httpkit.Internal(w, err)
					return
				}
			}
			if exists {
				filtered = append(filtered, c)
			}
		}
		list = filtered
	}
	httpkit.Reply(w, 200, list)
}

func (a *Service) CompanyAction(w http.ResponseWriter, r *http.Request) {
	u, ok := a.Auth.User(w, r)
	if !ok {
		return
	}
	var in struct {
		Action string `json:"action"`
		ID     int64  `json:"id"`
		Name   string `json:"name"`
		Tag    string `json:"tag"`
		Color  string `json:"color"`
		Flag   string `json:"flag"`
	}
	if !httpkit.Decode(w, r, &in) {
		return
	}
	tx, err := a.DB.Begin(r.Context())
	if err != nil {
		httpkit.Internal(w, err)
		return
	}
	defer tx.Rollback(context.Background())
	if r.Method == "POST" {
		if _, err = tx.Exec(r.Context(), `SELECT pg_advisory_xact_lock(784218)`); err != nil {
			httpkit.Internal(w, err)
			return
		}
	}
	var current *int64
	if err = tx.QueryRow(r.Context(), `SELECT company_id FROM users WHERE id=$1 FOR UPDATE`, u.ID).Scan(&current); err != nil {
		httpkit.Internal(w, err)
		return
	}
	if err = wallet.Reason(r.Context(), tx, "company_"+in.Action); err != nil {
		httpkit.Internal(w, err)
		return
	}
	problem := ""
	switch in.Action {
	case "leave":
		if current != nil {
			var owner *int64
			if err = tx.QueryRow(r.Context(), `SELECT owner_id FROM companies WHERE id=$1 FOR UPDATE`, *current).Scan(&owner); err != nil {
				break
			}
			if owner != nil && *owner == u.ID {
				problem = "Владелец должен оставаться в компании"
				break
			}
		}
		_, err = tx.Exec(r.Context(), `UPDATE users SET company_id=NULL,company_role=NULL WHERE id=$1`, u.ID)
	case "join":
		if current != nil {
			problem = "Сначала выйдите из текущей компании"
			break
		}
		var id int64
		var starter, founder bool
		err = tx.QueryRow(r.Context(), `SELECT id,starter,founder FROM companies WHERE id=$1 FOR KEY SHARE`, in.ID).Scan(&id, &starter, &founder)
		if err == nil && !starter {
			problem = "Подайте заявку на вступление"
			break
		}
		if err == nil && founder {
			problem = "Эта корпорация не принимает участников"
			break
		}
		if errors.Is(err, pgx.ErrNoRows) {
			err = nil
			problem = "Компания не найдена"
			break
		}
		if err == nil {
			_, err = tx.Exec(r.Context(), `UPDATE users SET company_id=$2 WHERE id=$1`, u.ID, id)
		}
	case "create":
		settings, e := gameplay.ReadGameplay(r.Context(), tx)
		if e != nil {
			err = e
			break
		}
		if e = wallet.Debit(r.Context(), tx, u.ID, settings.CompanyCost); e != nil {
			problem = e.Error()
			break
		}
		if current != nil {
			problem = "Сначала выйдите из текущей компании"
			break
		}
		in.Name = strings.TrimSpace(in.Name)
		in.Tag = strings.ToUpper(in.Tag)
		if len([]rune(in.Name)) < 3 || len([]rune(in.Name)) > 60 || !ValidCompanyTag(in.Tag) || !colorPattern.MatchString(in.Color) || !strings.Contains("◈◆★☀☾⚑⚙✦", in.Flag) || in.Flag == "" || len([]rune(in.Flag)) != 1 {
			problem = "Некорректные параметры компании"
			break
		}
		var id int64
		err = tx.QueryRow(r.Context(), `INSERT INTO companies(name,tag,color,flag,owner_id) VALUES($1,$2,$3,$4,$5) ON CONFLICT DO NOTHING RETURNING id`, in.Name, in.Tag, in.Color, in.Flag, u.ID).Scan(&id)
		if errors.Is(err, pgx.ErrNoRows) {
			err = nil
			problem = "Название или сокращение уже занято"
			break
		}
		if err == nil {
			_, err = tx.Exec(r.Context(), `UPDATE users SET company_id=$2 WHERE id=$1`, u.ID, id)
		}
	default:
		problem = "Неизвестное действие"
	}
	if err != nil {
		httpkit.Internal(w, err)
		return
	}
	if problem != "" {
		httpkit.Fail(w, 409, problem)
		return
	}
	if err = tx.Commit(r.Context()); err != nil {
		httpkit.Internal(w, err)
		return
	}
	a.MembershipCommitted(u.ID)
	httpkit.Reply(w, 200, map[string]bool{"ok": true})
}
