package server

import (
	"context"
	"embed"
	"io/fs"
	"net/http"
	"strings"
	"time"

	"github.com/yomezza/cosmosimulator/internal/administration"
	planetruntime "github.com/yomezza/cosmosimulator/internal/planet/runtime"
	"github.com/yomezza/cosmosimulator/internal/platform/httpkit"
)

//go:embed *.sql web/*
var Assets embed.FS

func (a *App) Handler() http.Handler {
	m := http.NewServeMux()
	m.HandleFunc("GET /api/admin/metrics", a.metricsView)
	m.HandleFunc("POST /api/admin/chat/moderator", a.Administration.ChatModerator)
	target, _ := planetEndpoint()
	m.HandleFunc("GET /api/admin/services", a.Administration.Services(administration.ServicesOptions{Version: Version, Started: a.startedAt, PlanetURL: target, PlanetEnabled: func() bool { return a.Transfer.Enabled }, ExpectedSchema: planetruntime.SchemaVersion}))
	m.HandleFunc("GET /api/health", func(w http.ResponseWriter, r *http.Request) {
		c, cancel := context.WithTimeout(r.Context(), 2*time.Second)
		defer cancel()
		if a.DB.Ping(c) != nil {
			httpkit.Fail(w, 503, "База недоступна")
			return
		}
		httpkit.Reply(w, 200, map[string]string{"status": "ok"})
	})
	m.HandleFunc("GET /api/server-status", a.Administration.ServerStatus)
	m.HandleFunc("POST /api/register", a.Auth.Register)
	m.HandleFunc("POST /api/login", a.Auth.Login)
	m.HandleFunc("POST /api/logout", a.Auth.Logout)
	m.HandleFunc("GET /api/state", a.Terminal.State)
	m.HandleFunc("GET /api/fleet", a.Terminal.Fleet)
	a.registerTerminalRoutes(m)
	m.HandleFunc("GET /api/gameplay", a.Administration.Gameplay)
	m.HandleFunc("GET /api/admin/gameplay", a.Administration.Gameplay)
	m.HandleFunc("POST /api/admin/gameplay", a.Administration.Gameplay)
	m.HandleFunc("GET /api/events", a.Administration.Events)
	m.HandleFunc("GET /api/admin/log", a.Administration.Events)
	m.HandleFunc("GET /api/admin/changes", a.Administration.AdminChanges)
	m.HandleFunc("GET /api/admin/galaxies", a.World.AdminGalaxies)
	m.HandleFunc("POST /api/admin/galaxies/preview", a.World.AdminGalaxies)
	m.HandleFunc("POST /api/admin/galaxies/apply", a.World.AdminGalaxies)
	m.HandleFunc("GET /api/admin/planet/surface", a.Colonies.AdminPlanetSurface)
	m.HandleFunc("POST /api/admin/planet/surface", a.Colonies.AdminPlanetSurface)
	m.Handle("GET /planet/", a.planetProxy())
	m.Handle("GET /chat/", a.chatProxy())
	m.HandleFunc("POST /api/planet/handoff", a.Transfer.Handoff)
	m.HandleFunc("GET /api/planet/visit", a.Transfer.State)
	m.HandleFunc("POST /api/planet/return", a.Transfer.Return)
	m.HandleFunc("GET /api/planet/surface", a.Colonies.PlanetSurface)
	m.HandleFunc("GET /api/planet/state", a.Colonies.PlanetState)
	m.HandleFunc("GET /api/planet/projects", a.Colonies.ColonyList)
	m.HandleFunc("GET /api/planet/project", a.Colonies.ColonyView)
	m.HandleFunc("POST /api/planet/action", a.Colonies.ColonyAction)
	m.HandleFunc("GET /api/admin/planet/settings", a.Colonies.PlanetSettings)
	m.HandleFunc("POST /api/admin/planet/settings", a.Colonies.PlanetSettings)
	m.HandleFunc("GET /api/system", a.Terminal.SystemView)
	m.HandleFunc("GET /api/profile", a.Auth.Profile)
	m.HandleFunc("POST /api/profile/upgrade", a.Auth.Upgrade)
	m.HandleFunc("GET /api/companies", a.Corporations.CompanyList)
	m.HandleFunc("POST /api/companies", a.Corporations.CompanyAction)
	m.HandleFunc("GET /api/market/global", a.Commerce.GlobalMarket)
	m.HandleFunc("POST /api/market/global", a.Commerce.GlobalMarket)
	m.HandleFunc("POST /api/admin/tick", a.Administration.AdminTick)
	m.HandleFunc("POST /api/admin/teleport", a.Navigation.AdminTeleport)
	m.HandleFunc("GET /api/admin/resources", a.Administration.AdminResources)
	m.HandleFunc("POST /api/admin/resources", a.Administration.AdminResources)
	m.HandleFunc("POST /api/admin/galaxies/delete-preview", a.World.DeleteGalaxy)
	m.HandleFunc("POST /api/admin/galaxies/delete", a.World.DeleteGalaxy)
	m.HandleFunc("GET /api/logistics", a.Logistics.Logistics)
	m.HandleFunc("POST /api/logistics", a.Logistics.Logistics)
	m.HandleFunc("GET /api/ideas", a.Ideas.Ideas)
	m.HandleFunc("POST /api/ideas", a.Ideas.Ideas)
	m.HandleFunc("POST /api/admin/ideas", a.Ideas.Review)
	m.HandleFunc("GET /api/planner", a.Planner.Planner)
	m.HandleFunc("POST /api/planner", a.Planner.Planner)
	m.HandleFunc("POST /api/admin/user", a.Administration.EditUser)
	m.HandleFunc("GET /api/galaxy", a.World.Galaxy)
	m.HandleFunc("GET /api/route", a.World.Galaxy)
	m.HandleFunc("POST /api/sell", a.Commerce.Sell)
	m.HandleFunc("GET /api/expedition", a.Terminal.Expedition)
	m.HandleFunc("POST /api/expedition", a.Terminal.Expedition)
	m.HandleFunc("GET /api/admin/users", a.Administration.UserPages)
	web, _ := fs.Sub(Assets, "web")
	m.Handle("GET /", http.FileServer(http.FS(web)))
	return httpkit.GzipAPI(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("X-Content-Type-Options", "nosniff")
		w.Header().Set("X-Frame-Options", "DENY")
		w.Header().Set("Content-Security-Policy", "default-src 'self'; style-src 'self'; script-src 'self'; frame-ancestors 'none'; base-uri 'none'; form-action 'self'")
		if strings.HasPrefix(r.URL.Path, "/api/") {
			w.Header().Set("Cache-Control", "no-store")
		}
		if r.Method == "POST" {
			origin := r.Header.Get("Origin")
			expected := a.origin
			if expected == "" {
				scheme := "http"
				if r.TLS != nil {
					scheme = "https"
				}
				expected = scheme + "://" + r.Host
			}
			if origin != "" && origin != expected {
				httpkit.Fail(w, 403, "Недопустимый источник запроса")
				return
			}
			if r.Header.Get("Sec-Fetch-Site") == "cross-site" {
				httpkit.Fail(w, 403, "Недопустимый источник запроса")
				return
			}
			if r.Header.Get("Content-Type") != "application/json" {
				httpkit.Fail(w, 415, "Ожидается application/json")
				return
			}
			if !httpkit.BufferJSON(w, r) {
				return
			}
		}
		if strings.HasPrefix(r.URL.Path, "/api/") {
			bounded, cancel := httpkit.APIDeadline(r)
			defer cancel()
			r = bounded
		}
		m.ServeHTTP(w, r)
	}))
}

func (a *App) registerTerminalRoutes(m *http.ServeMux) {
	m.HandleFunc("GET /api/estimate", a.Terminal.Estimate)
	m.HandleFunc("GET /api/workbench", a.Terminal.Workbench)
	m.HandleFunc("POST /api/workbench", a.Terminal.Command)
	m.HandleFunc("GET /api/notifications", a.Notifications.Notifications)
	m.HandleFunc("POST /api/notifications", a.Notifications.Notifications)
	m.HandleFunc("GET /api/admin/benchmark", a.Benchmark.Benchmark)
	m.HandleFunc("POST /api/admin/benchmark", a.Benchmark.Benchmark)
}
