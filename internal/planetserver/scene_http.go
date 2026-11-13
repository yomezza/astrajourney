package planetserver

import (
	"context"
	"errors"
	"github.com/yomezza/cosmosimulator/internal/planet/access"
	"github.com/yomezza/cosmosimulator/internal/planet/scene"
	"github.com/yomezza/cosmosimulator/internal/planet/scenestore"
	"github.com/yomezza/cosmosimulator/internal/planet/visits"
	"github.com/yomezza/cosmosimulator/internal/platform/httpkit"
	"net/http"
	"strconv"
)

func (a *App) sceneView(w http.ResponseWriter, r *http.Request) {
	if a.scenes == nil || a.stopping.Load() {
		httpkit.Fail(w, 503, "runtime_unavailable")
		return
	}
	ctx, done := context.WithTimeout(r.Context(), a.config.SceneLoadTimeout)
	defer done()
	id, err := access.Authenticate(ctx, a.db, r)
	if err != nil {
		httpkit.Fail(w, 401, publicError(err))
		return
	}
	v := r.URL.Query().Get("visit")
	epoch, err := strconv.ParseInt(r.URL.Query().Get("epoch"), 10, 64)
	if err != nil || !access.ValidUUID(v) || epoch < 1 {
		httpkit.Fail(w, 400, "invalid_visit")
		return
	}
	in, err := a.visits.Resolve(ctx, id, "", v)
	if err != nil {
		httpkit.Fail(w, 403, publicError(err))
		return
	}
	f, err := a.sceneFence()
	if err != nil {
		httpkit.Fail(w, 503, "runtime_unavailable")
		return
	}
	welcome := visits.Welcome{Visit: v, Epoch: epoch}
	if err = a.visits.Check(ctx, id, welcome, f); err != nil {
		httpkit.Fail(w, 403, publicError(err))
		return
	}
	pin, err := a.scenes.Acquire(ctx, in.Key)
	if err != nil {
		_ = a.visits.DiagnoseSceneFailure(ctx, id, welcome, f, in.Key, err)
		httpkit.Fail(w, 503, "scene_unavailable")
		return
	}
	defer pin.Release()
	if err = a.visits.Check(ctx, id, welcome, f); err != nil {
		httpkit.Fail(w, 403, publicError(err))
		return
	}
	// No ticket, seed or private geology in this DTO or URL.
	_ = a.visits.ClearSceneDiagnosis(ctx, id, welcome)
	public := pin.Scene().Public()
	var hasSite bool
	if err = a.db.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM planet_landing_sites WHERE surface=$1 AND sector=$2 AND enabled)`, in.Key.Surface, in.Key.Sector).Scan(&hasSite); err != nil {
		httpkit.Fail(w, 503, "runtime_unavailable")
		return
	}
	if !hasSite {
		public.Landing = nil
	}
	exits := []map[string]any{}
	for _, portal := range pin.Scene().Portals() {
		key := in.Key
		key.Sector = portal.Neighbor
		territory, e := access.Sector(ctx, a.db, id.User, key)
		if errors.Is(e, access.ErrDenied) {
			continue
		}
		if e != nil {
			httpkit.Fail(w, 503, "runtime_unavailable")
			return
		}
		exits = append(exits, map[string]any{"neighbor": portal.Neighbor, "owner": territory.Owner, "x_q": portal.Ingress.X, "y_q": portal.Ingress.Y})
	}
	if err = a.visits.Check(ctx, id, welcome, f); err != nil {
		httpkit.Fail(w, 403, publicError(err))
		return
	}
	materials, err := a.store.Materials(ctx, pin.Scene())
	if err != nil {
		httpkit.Fail(w, 503, "scene_unavailable")
		return
	}
	if err = a.visits.Check(ctx, id, welcome, f); err != nil {
		httpkit.Fail(w, 403, publicError(err))
		return
	}
	httpkit.Reply(w, 200, struct {
		scene.Public
		Exits     []map[string]any           `json:"exits"`
		Materials []scenestore.MaterialPoint `json:"materials"`
	}{public, exits, materials})
}
