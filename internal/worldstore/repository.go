package worldstore

import (
	"context"
	"encoding/json"
	"net/http"
	"strconv"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/yomezza/cosmosimulator/internal/platform/httpkit"
	"github.com/yomezza/cosmosimulator/internal/platform/postgres"
	"github.com/yomezza/cosmosimulator/internal/universe"
)

func (a *Service) Galaxy(w http.ResponseWriter, r *http.Request) {
	if _, ok := a.Auth.User(w, r); !ok {
		return
	}
	g, err := a.Load(r.Context())
	if err != nil {
		httpkit.Internal(w, err)
		return
	}
	if r.URL.Path == "/api/galaxy" {
		public := *g
		public.Stars = append([]universe.Star(nil), g.Stars...)
		for i := range public.Stars {
			if public.Stars[i].Deleted {
				public.Stars[i] = universe.Star{ID: public.Stars[i].ID, Galaxy: "deleted", Deleted: true}
				continue
			}
			public.Stars[i] = universe.PublicStar(public.Stars[i])
			public.Stars[i].Planets = nil
		}
		founders, e := postgres.QueryMaps(r.Context(), a.DB, `SELECT id,name,tag,color,flag,founder_key FROM companies WHERE founder ORDER BY founder_key`)
		if e != nil {
			httpkit.Internal(w, e)
			return
		}
		httpkit.Reply(w, 200, map[string]any{"name": public.Name, "version": public.Version, "revision": g.Revision, "galaxies": universe.PublicGalaxies(g), "founders": founders, "stars": public.Stars, "regions": public.Regions, "lanes": public.Lanes, "black_holes": universe.BlackHoles(g), "black_hole": map[string]any{"x": 0, "y": 0, "systems": universe.CoreSystems(g)}})
		return
	}
	from, e1 := strconv.Atoi(r.URL.Query().Get("from"))
	to, e2 := strconv.Atoi(r.URL.Query().Get("to"))
	if e1 != nil || e2 != nil || from < 1 || to < 1 || from > len(g.Stars) || to > len(g.Stars) {
		httpkit.Fail(w, 400, "Некорректная система")
		return
	}
	httpkit.Reply(w, 200, map[string]any{"path": g.Path(from, to)})
}

func (a *Service) Load(ctx context.Context) (*universe.Galaxy, error) {
	return a.LoadFrom(ctx, a.DB)
}

// A caller holding a transaction must be able to load a cold world cache using
// that connection, rather than waiting for another slot in a saturated pool.
func (a *Service) LoadFrom(ctx context.Context, q postgres.Querier) (*universe.Galaxy, error) {
	// Transactional readers use their own committed-world snapshot directly. They
	// must not wait for a cache mutex held by an ordinary pool reader that itself
	// might be waiting for one of these transactions to release a connection.
	if _, transactional := q.(pgx.Tx); transactional {
		var raw []byte
		if err := q.QueryRow(ctx, `SELECT data FROM galaxies WHERE name='Edem'`).Scan(&raw); err != nil {
			return nil, err
		}
		var g universe.Galaxy
		if err := json.Unmarshal(raw, &g); err != nil {
			return nil, err
		}
		return &g, nil
	}

	for {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		a.universeMu.Lock()
		cached, epoch, checked := a.universe, a.universeEpoch, a.universeChecked
		a.universeMu.Unlock()
		if cached != nil && time.Since(checked) < 2*time.Second {
			return cached, nil
		}
		// Never wait for a pool connection while holding the cache mutex: a writer
		// may own the last connection and need that mutex to commit/invalidate.
		if cached != nil {
			var revision int64
			if err := q.QueryRow(ctx, `SELECT COALESCE((data->>'revision')::bigint,0) FROM galaxies WHERE name='Edem'`).Scan(&revision); err != nil {
				return nil, err
			}
			a.universeMu.Lock()
			if epoch != a.universeEpoch || cached != a.universe {
				a.universeMu.Unlock()
				continue
			}
			if revision == cached.Revision {
				a.universeChecked = time.Now()
				a.universeMu.Unlock()
				return cached, nil
			}
			a.universeMu.Unlock()
		}
		var raw []byte
		if err := q.QueryRow(ctx, `SELECT data FROM galaxies WHERE name='Edem'`).Scan(&raw); err != nil {
			return nil, err
		}
		var g universe.Galaxy
		if err := json.Unmarshal(raw, &g); err != nil {
			return nil, err
		}
		a.universeMu.Lock()
		if epoch != a.universeEpoch || cached != a.universe {
			a.universeMu.Unlock()
			continue
		}
		a.universe = &g
		a.universeChecked = time.Now()
		a.universeMu.Unlock()
		return &g, nil
	}
}
