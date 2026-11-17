package discovery

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"strings"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/yomezza/cosmosimulator/internal/account"
	"github.com/yomezza/cosmosimulator/internal/platform/httpkit"
	"github.com/yomezza/cosmosimulator/internal/platform/idempotency"
	"github.com/yomezza/cosmosimulator/internal/platform/identifiers"
	"github.com/yomezza/cosmosimulator/internal/platform/naming"
	"github.com/yomezza/cosmosimulator/internal/universestore"
)

type World interface {
	Commit(context.Context, pgx.Tx) error
}
type Service struct {
	DB    *pgxpool.Pool
	Auth  *account.Service
	World World
}
type Object struct {
	UUID   string `json:"uuid,omitempty"`
	Kind   string `json:"kind"`
	Star   int    `json:"star_id"`
	Planet int    `json:"planet_id"`
}
type renameRequest struct {
	Operation string `json:"operation"`
	Object    Object `json:"object"`
	Name      string `json:"name"`
	Reason    string `json:"reason,omitempty"`
}

func (a *Service) Rename(w http.ResponseWriter, r *http.Request) {
	u, ok := a.Auth.User(w, r)
	if !ok {
		return
	}
	if !a.Auth.Allow(r) {
		httpkit.Fail(w, 429, "Слишком много запросов")
		return
	}
	var in renameRequest
	if !httpkit.Decode(w, r, &in) {
		return
	}
	admin := strings.HasPrefix(r.URL.Path, "/api/admin/")
	if !identifiers.ValidUUID(in.Operation) || (in.Object.UUID != "" && !identifiers.ValidUUID(in.Object.UUID)) || !((in.Object.Kind == "system" && in.Object.Planet == 0) || (in.Object.Kind == "planet" && (in.Object.Planet > 0 || in.Object.UUID != ""))) || (in.Object.Star < 1 && in.Object.UUID == "") {
		httpkit.Fail(w, 400, "Некорректный объект или идентификатор операции")
		return
	}
	name, key, err := naming.Normalize(in.Name, 40)
	if err != nil {
		httpkit.Fail(w, 422, err.Error())
		return
	}
	if admin && (strings.TrimSpace(in.Reason) == "" || len([]rune(in.Reason)) > 400) {
		httpkit.Fail(w, 422, "Укажите причину исправления")
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
	var bot bool
	if err = tx.QueryRow(ctx, `SELECT role,is_bot FROM users WHERE id=$1 FOR UPDATE`, u.ID).Scan(&role, &bot); err != nil {
		httpkit.Internal(w, err)
		return
	}
	if bot || (admin && role != "admin") {
		httpkit.Fail(w, 403, "Нет права переименования")
		return
	}
	digest := idempotency.Digest(in)
	var previous string
	var actor *int64
	var result []byte
	err = tx.QueryRow(ctx, `SELECT request_hash,actor_id,result FROM aj_discovery_name_history WHERE operation_id=$1`, in.Operation).Scan(&previous, &actor, &result)
	if err == nil {
		if previous != digest || actor == nil || *actor != u.ID {
			httpkit.Fail(w, 409, "Идентификатор операции уже использован")
		} else {
			httpkit.Reply(w, 200, json.RawMessage(result))
		}
		return
	}
	if !errors.Is(err, pgx.ErrNoRows) {
		httpkit.Internal(w, err)
		return
	}
	identity, err := universestore.Resolve(ctx, tx, in.Object.Kind, in.Object.UUID, in.Object.Star, in.Object.Planet)
	if errors.Is(err, pgx.ErrNoRows) {
		httpkit.Fail(w, 404, "Объект не найден")
		return
	}
	if err != nil {
		httpkit.Internal(w, err)
		return
	}
	if (in.Object.Star > 0 && in.Object.Star != identity.Star) || (in.Object.Planet > 0 && in.Object.Planet != identity.Planet) {
		httpkit.Fail(w, 400, "UUID не соответствует указанному объекту")
		return
	}
	in.Object.Star = identity.Star
	in.Object.Planet = identity.Planet
	var owner *int64
	var used bool
	var state, old, code, protected string
	var revision int64
	err = tx.QueryRow(ctx, `SELECT discoverer_id,rename_used_at IS NOT NULL,claim_state,current_name,catalog_code,COALESCE(protected_founder_key,''),name_revision FROM aj_discovery_objects WHERE kind=$1 AND star_id=$2 AND planet_id=$3 FOR UPDATE`, in.Object.Kind, in.Object.Star, in.Object.Planet).Scan(&owner, &used, &state, &old, &code, &protected, &revision)
	if errors.Is(err, pgx.ErrNoRows) {
		httpkit.Fail(w, 404, "Объект не найден")
		return
	}
	if err != nil {
		httpkit.Internal(w, err)
		return
	}
	objectState := state
	// Ownership of the SYSTEM grants separate one-time rights to its planets.
	// Do not rewrite a planet's historical first-scan attribution.
	if !admin {
		var systemProtected string
		if err = tx.QueryRow(ctx, `SELECT discoverer_id,claim_state,COALESCE(protected_founder_key,'') FROM aj_discovery_objects WHERE kind='system' AND star_id=$1 AND planet_id=0 FOR UPDATE`, in.Object.Star).Scan(&owner, &state, &systemProtected); err != nil {
			httpkit.Internal(w, err)
			return
		}
		if systemProtected != "" {
			protected = systemProtected
		}
	}
	if !admin && (state != "claimed" || owner == nil || *owner != u.ID || protected != "" || identity.Founder != "") {
		httpkit.Fail(w, 403, "Нет права переименования")
		return
	}
	if objectState == "retired" || (!admin && used) {
		httpkit.Fail(w, 409, "Право переименования уже использовано")
		return
	}
	_, err = tx.Exec(ctx, `UPDATE aj_discovery_objects SET current_name=$4,name_key=$5,name_revision=name_revision+1,rename_used_at=CASE WHEN $6 THEN rename_used_at ELSE clock_timestamp() END,updated_at=clock_timestamp() WHERE kind=$1 AND star_id=$2 AND planet_id=$3`, in.Object.Kind, in.Object.Star, in.Object.Planet, name, key, admin)
	if err != nil {
		conflict(w, err)
		return
	}
	worldRevision, err := universestore.Rename(ctx, tx, identity, name)
	if err != nil {
		httpkit.Internal(w, err)
		return
	}
	in.Object.UUID = identity.UUID
	if in.Object.Kind == "planet" {
		if _, err = tx.Exec(ctx, `UPDATE planet_surfaces SET name=$3 WHERE star=$1 AND planet=$2`, in.Object.Star, in.Object.Planet, name); err != nil {
			httpkit.Internal(w, err)
			return
		}
	}
	response := map[string]any{"operation": in.Operation, "object": in.Object, "name": name, "catalog_code": code, "name_revision": revision + 1, "world_revision": worldRevision, "rename_available": false}
	result, _ = json.Marshal(response)
	source := "discoverer"
	if admin {
		source = "admin"
	}
	_, err = tx.Exec(ctx, `INSERT INTO aj_discovery_name_history(operation_id,kind,star_id,planet_id,actor_id,actor_source,old_name,new_name,request_hash,reason,result) VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11)`, in.Operation, in.Object.Kind, in.Object.Star, in.Object.Planet, u.ID, source, old, name, digest, in.Reason, result)
	if err == nil && admin {
		audit, _ := json.Marshal(map[string]any{"object": in.Object, "operation": in.Operation, "name": name, "reason": in.Reason})
		_, err = tx.Exec(ctx, `INSERT INTO admin_log(actor,action,data) VALUES($1,'discovery_name_correction',$2)`, u.ID, audit)
	}
	if err == nil {
		err = a.World.Commit(ctx, tx)
	}
	if err != nil {
		conflict(w, err)
		return
	}
	httpkit.Reply(w, 200, response)
}
func conflict(w http.ResponseWriter, err error) {
	var e *pgconn.PgError
	if errors.As(err, &e) && (e.Code == "23505" || e.Code == "23514") {
		httpkit.Fail(w, 409, "Название уже используется или объект изменился")
	} else {
		httpkit.Internal(w, err)
	}
}
