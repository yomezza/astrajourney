package worldstore

import (
	"context"
	"encoding/json"
	"net/http"

	"github.com/jackc/pgx/v5"

	"github.com/yomezza/cosmosimulator/internal/discovery"
	"github.com/yomezza/cosmosimulator/internal/platform/httpkit"
	"github.com/yomezza/cosmosimulator/internal/platform/idempotency"
	"github.com/yomezza/cosmosimulator/internal/universe"
)

func deletionBlockers(ctx context.Context, tx pgx.Tx, ids []int) (map[string]int, error) {
	queries := map[string]string{
		"planet_projects":   `SELECT count(*) FROM planet_projects p JOIN planet_surfaces s ON s.id=p.surface WHERE s.star=ANY($1::int[])`,
		"ships":             `SELECT count(*) FROM ships s WHERE star=ANY($1::int[]) OR destination=ANY($1::int[]) OR EXISTS(SELECT 1 FROM jsonb_array_elements_text(waypoints) v WHERE v::int=ANY($1::int[]))`,
		"warehouses":        `SELECT count(*) FROM warehouses WHERE star=ANY($1::int[]) AND EXISTS(SELECT 1 FROM jsonb_each_text(cargo) e WHERE e.value::bigint>0)`,
		"corporate_storage": `SELECT count(*) FROM company_storage WHERE star=ANY($1::int[]) AND EXISTS(SELECT 1 FROM jsonb_each_text(cargo) e WHERE e.value::bigint>0)`,
		"stations":          `SELECT count(*) FROM stations WHERE star=ANY($1::int[]) AND company_id IS NOT NULL`,
		"projects":          `SELECT count(*) FROM company_projects WHERE star=ANY($1::int[]) AND status IN('active','complete')`,
		"tasks":             `SELECT count(*) FROM automation_tasks WHERE status IN('queued','running','paused') AND (source_star=ANY($1::int[]) OR dest_star=ANY($1::int[]))`,
		"deliveries":        `SELECT count(*) FROM deliveries WHERE status IN('open','carrying','bot') AND (from_star=ANY($1::int[]) OR to_star=ANY($1::int[]))`,
		"reservations":      `SELECT count(*) FROM reservations WHERE star=ANY($1::int[]) AND status='reserved'`,
		"offers":            `SELECT (SELECT count(*) FROM sell_orders WHERE star=ANY($1::int[]) AND quantity>0)+(SELECT count(*) FROM survey_offers WHERE star=ANY($1::int[]))`,
		"research":          `SELECT (SELECT count(*) FROM research_jobs WHERE star=ANY($1::int[]))+(SELECT count(*) FROM mining_jobs WHERE star=ANY($1::int[]))`,
		"player_data":       `SELECT (SELECT count(*) FROM surveys s JOIN users u ON u.id=s.user_id WHERE u.role<>'admin' AND s.star=ANY($1::int[]))+(SELECT count(*) FROM navigation_notes n JOIN users u ON u.id=n.user_id WHERE u.role<>'admin' AND n.star=ANY($1::int[]) AND (n.bookmark OR n.note<>''))`,
	}
	out := map[string]int{}
	for key, sql := range queries {
		var count int
		if err := tx.QueryRow(ctx, sql, ids).Scan(&count); err != nil {
			return nil, err
		}
		out[key] = count
	}
	return out, nil
}

func (a *Service) DeleteGalaxy(w http.ResponseWriter, r *http.Request) {
	u, ok := a.Auth.User(w, r)
	if !ok {
		return
	}
	if u.Role != "admin" {
		httpkit.Fail(w, 403, "Требуется роль администратора")
		return
	}
	var in struct {
		ID      string `json:"id"`
		Preview string `json:"preview"`
	}
	if !httpkit.Decode(w, r, &in) {
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
	if err = tx.QueryRow(ctx, `SELECT role FROM users WHERE id=$1 FOR UPDATE`, u.ID).Scan(&role); err != nil || role != "admin" {
		httpkit.Fail(w, 403, "Требуется роль администратора")
		return
	}
	var raw []byte
	if err = tx.QueryRow(ctx, `SELECT data FROM galaxies WHERE name='Edem' FOR UPDATE`).Scan(&raw); err != nil {
		httpkit.Internal(w, err)
		return
	}
	var g universe.Galaxy
	if err = json.Unmarshal(raw, &g); err != nil {
		httpkit.Internal(w, err)
		return
	}
	d := g.Definition(in.ID)
	if d == nil || d.Deleted || in.ID == "edem" || in.ID == "asterion" {
		httpkit.Fail(w, 400, "Удалять можно только галактики, созданные администратором")
		return
	}
	ids := []int{}
	for _, s := range universe.GalaxyStars(&g, in.ID) {
		ids = append(ids, s.ID)
	}
	blockers, err := deletionBlockers(ctx, tx, ids)
	if err != nil {
		httpkit.Internal(w, err)
		return
	}
	total := 0
	for _, n := range blockers {
		total += n
	}
	digest := idempotency.Digest([]any{g.Revision, in.ID, ids, blockers})
	if r.URL.Path == "/api/admin/galaxies/delete-preview" {
		httpkit.Reply(w, 200, map[string]any{"preview": digest, "can_delete": total == 0, "blockers": blockers, "systems": len(ids), "name": d.Name})
		return
	}
	if in.Preview == "" || in.Preview != digest {
		httpkit.Fail(w, 409, "Данные изменились. Проверьте удаление заново")
		return
	}
	if total > 0 {
		httpkit.Fail(w, 409, "В галактике есть корабли, имущество или данные игроков")
		return
	}
	d.Deleted = true
	d.Opened = false
	d.Territories = nil
	for i := range g.Stars {
		if universe.GalaxyKey(g.Stars[i]) == in.ID {
			g.Stars[i].Deleted = true
		}
	}
	lanes := []universe.Lane{}
	for _, e := range g.Lanes {
		if !g.Stars[e.From-1].Deleted && !g.Stars[e.To-1].Deleted {
			lanes = append(lanes, e)
		}
	}
	g.Lanes = lanes
	g.Revision++
	if _, err = tx.Exec(ctx, `UPDATE stations SET active=FALSE WHERE star=ANY($1::int[])`, ids); err != nil {
		httpkit.Internal(w, err)
		return
	}
	if err = discovery.Sync(ctx, tx, &g); err != nil {
		httpkit.Internal(w, err)
		return
	}
	raw, _ = json.Marshal(&g)
	if _, err = tx.Exec(ctx, `UPDATE galaxies SET data=$1 WHERE name='Edem'`, raw); err != nil {
		httpkit.Internal(w, err)
		return
	}
	audit, _ := json.Marshal(map[string]any{"galaxy": in.ID, "name": d.Name, "systems": len(ids), "revision": g.Revision})
	if _, err = tx.Exec(ctx, `INSERT INTO admin_log(actor,action,data) VALUES($1,'galaxy_delete',$2)`, u.ID, audit); err != nil {
		httpkit.Internal(w, err)
		return
	}
	err = a.Commit(ctx, tx)
	if err != nil {
		httpkit.Internal(w, err)
		return
	}
	httpkit.Reply(w, 200, map[string]bool{"ok": true})
}
