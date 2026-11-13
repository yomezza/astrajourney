package colonies

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/yomezza/cosmosimulator/internal/account"
	"github.com/yomezza/cosmosimulator/internal/corporations"
	"github.com/yomezza/cosmosimulator/internal/platform/httpkit"
	"github.com/yomezza/cosmosimulator/internal/platform/idempotency"
	"github.com/yomezza/cosmosimulator/internal/platform/identifiers"
	"github.com/yomezza/cosmosimulator/internal/platform/naming"
)

func (a *Service) renameColony(w http.ResponseWriter, r *http.Request, u account.User, in planetAction) {
	if !identifiers.ValidUUID(in.Request) || in.ID < 1 {
		httpkit.Fail(w, 400, "Некорректный идентификатор операции")
		return
	}
	in.Reason = strings.TrimSpace(in.Reason)
	if len([]rune(in.Reason)) > 400 {
		httpkit.Fail(w, 422, "Причина исправления не должна превышать 400 символов")
		return
	}
	name, key, err := naming.Normalize(in.Name, 60)
	if err != nil {
		httpkit.Fail(w, 422, err.Error())
		return
	}
	ctx := r.Context()
	tx, err := a.DB.Begin(ctx)
	if err != nil {
		httpkit.Internal(w, err)
		return
	}
	defer tx.Rollback(context.Background())
	if _, err = tx.Exec(ctx, `SELECT pg_advisory_xact_lock(784218)`); err != nil {
		httpkit.Internal(w, err)
		return
	}
	var role string
	if err = tx.QueryRow(ctx, `SELECT role FROM users WHERE id=$1 FOR UPDATE`, u.ID).Scan(&role); err != nil {
		httpkit.Internal(w, err)
		return
	}
	if in.Correction && (role != "admin" || in.Reason == "") {
		httpkit.Fail(w, 403, "Требуется администратор и причина исправления")
		return
	}
	digest := idempotency.Digest(in)
	var prior string
	var actor *int64
	var receipt []byte
	err = tx.QueryRow(ctx, `SELECT request_hash,actor_id,result FROM aj_colony_name_history WHERE operation_id=$1`, in.Request).Scan(&prior, &actor, &receipt)
	if err == nil {
		if prior != digest || actor == nil || *actor != u.ID {
			httpkit.Fail(w, 409, "Идентификатор операции уже использован")
		} else {
			httpkit.Reply(w, 200, json.RawMessage(receipt))
		}
		return
	}
	if !errors.Is(err, pgx.ErrNoRows) {
		httpkit.Internal(w, err)
		return
	}
	var owner, company *int64
	var surface int64
	var sector int
	var old string
	err = tx.QueryRow(ctx, `SELECT c.owner_user,c.owner_company,c.surface,c.sector,COALESCE(n.current_name,'Колония '||c.id::text) FROM planet_colonies c LEFT JOIN aj_colony_names n ON n.colony_id=c.id WHERE c.id=$1 FOR UPDATE OF c`, in.ID).Scan(&owner, &company, &surface, &sector, &old)
	if errors.Is(err, pgx.ErrNoRows) {
		httpkit.Fail(w, 404, "Колония не найдена")
		return
	}
	if err != nil {
		httpkit.Internal(w, err)
		return
	}
	source := "personal_owner"
	allowed := owner != nil && *owner == u.ID
	if company != nil {
		cid, manage, e := corporations.CompanyAccess(ctx, tx, u.ID, "colonize")
		if e != nil {
			httpkit.Internal(w, e)
			return
		}
		allowed = manage && cid == *company
		source = "corporate_manager"
	}
	if in.Correction {
		allowed = true
		source = "admin"
	}
	if !allowed {
		httpkit.Fail(w, 403, "Нет права переименовать колонию")
		return
	}
	var revision int64
	var last *time.Time
	err = tx.QueryRow(ctx, `SELECT current_name,revision,last_player_rename_at FROM aj_colony_names WHERE colony_id=$1 FOR UPDATE`, in.ID).Scan(&old, &revision, &last)
	if err != nil && !errors.Is(err, pgx.ErrNoRows) {
		httpkit.Internal(w, err)
		return
	}
	if revision != in.ExpectedRevision {
		httpkit.Fail(w, 409, "Колония уже изменена")
		return
	}
	if !in.Correction && last != nil && time.Since(*last) < 24*time.Hour {
		httpkit.Fail(w, 409, "Название колонии можно менять раз в 24 часа")
		return
	}
	_, err = tx.Exec(ctx, `INSERT INTO aj_colony_names(colony_id,surface_id,sector_number,current_name,name_key,revision,last_player_rename_at) VALUES($1,$2,$3,$4,$5,1,CASE WHEN $6 THEN NULL ELSE clock_timestamp() END) ON CONFLICT(colony_id) DO UPDATE SET current_name=EXCLUDED.current_name,name_key=EXCLUDED.name_key,revision=aj_colony_names.revision+1,last_player_rename_at=CASE WHEN $6 THEN aj_colony_names.last_player_rename_at ELSE clock_timestamp() END,updated_at=clock_timestamp()`, in.ID, surface, sector, name, key, in.Correction)
	if err != nil {
		colonyNameError(w, err)
		return
	}
	response := map[string]any{"colony": in.ID, "name": name, "revision": revision + 1, "operation": in.Request}
	receipt, _ = json.Marshal(response)
	_, err = tx.Exec(ctx, `INSERT INTO aj_colony_name_history(operation_id,colony_id,actor_id,actor_source,old_name,new_name,request_hash,reason,result) VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9)`, in.Request, in.ID, u.ID, source, old, name, digest, in.Reason, receipt)
	if err == nil && in.Correction {
		audit, _ := json.Marshal(map[string]any{"colony": in.ID, "operation": in.Request, "reason": in.Reason})
		_, err = tx.Exec(ctx, `INSERT INTO admin_log(actor,action,data) VALUES($1,'colony_name_correction',$2)`, u.ID, audit)
	}
	if err == nil {
		err = tx.Commit(ctx)
	}
	if err != nil {
		colonyNameError(w, err)
		return
	}
	httpkit.Reply(w, 200, response)
}
func colonyNameError(w http.ResponseWriter, err error) {
	var e *pgconn.PgError
	if errors.As(err, &e) && e.Code == "23505" {
		httpkit.Fail(w, 409, "Название колонии уже используется")
	} else {
		httpkit.Internal(w, err)
	}
}
