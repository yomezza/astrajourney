package terminal

import (
	"context"
	"net/http"
	"strconv"

	"github.com/yomezza/cosmosimulator/internal/discovery"
	"github.com/yomezza/cosmosimulator/internal/mining"
	"github.com/yomezza/cosmosimulator/internal/platform/httpkit"
	"github.com/yomezza/cosmosimulator/internal/research"
	"github.com/yomezza/cosmosimulator/internal/universe"
	"github.com/yomezza/cosmosimulator/internal/worldstore"
)

func (a *Service) SystemView(w http.ResponseWriter, r *http.Request) {
	u, ok := a.Auth.User(w, r)
	if !ok {
		return
	}
	id, err := strconv.Atoi(r.URL.Query().Get("id"))
	g, loadErr := a.World.Load(r.Context())
	if loadErr != nil {
		httpkit.Internal(w, loadErr)
		return
	}
	if err != nil || id < 1 || id > len(g.Stars) {
		httpkit.Fail(w, 400, "Некорректная система")
		return
	}
	s := g.Stars[id-1]
	if s.Deleted {
		httpkit.Fail(w, 404, "Система удалена")
		return
	}
	var scanned bool
	if err = a.DB.QueryRow(r.Context(), `SELECT EXISTS(SELECT 1 FROM surveys WHERE user_id=$1 AND star=$2)`, u.ID, id).Scan(&scanned); err != nil {
		httpkit.Internal(w, err)
		return
	}
	sites, siteErr := worldstore.AddCustomSites(r.Context(), a.DB, s, universe.SystemSites(s))
	if siteErr != nil {
		httpkit.Internal(w, siteErr)
		return
	}
	sites, err = research.RevealedSites(r.Context(), a.DB, u.ID, id, sites, scanned)
	if err != nil {
		httpkit.Internal(w, err)
		return
	}
	if scanned {
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
		if _, err = tx.Exec(r.Context(), `SELECT pg_advisory_xact_lock(784219,$1)`, id); err != nil {
			httpkit.Internal(w, err)
			return
		}
		sites, err = mining.QuotaView(r.Context(), tx, s, sites)
		if err != nil {
			httpkit.Internal(w, err)
			return
		}
		if err = tx.Commit(r.Context()); err != nil {
			httpkit.Internal(w, err)
			return
		}
	}
	planets := []int{}
	for _, site := range sites {
		if site.Details != nil {
			planets = append(planets, site.Planet)
		}
	}
	discoveries, err := discovery.Metadata(r.Context(), a.DB, u.ID, id, planets)
	if err != nil {
		httpkit.Internal(w, err)
		return
	}
	s = universe.PublicStar(s)
	s.Planets = nil
	httpkit.Reply(w, 200, map[string]any{"star": s, "region": g.RegionName(s.Region), "surveyed": scanned, "sites": sites, "discoveries": discoveries})
}
