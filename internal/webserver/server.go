// Package webserver owns the browser client and the same-origin HTTP gateway.
// All game authority and authentication remain in the downstream services.
package webserver

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"log"
	"net"
	"net/http"
	"net/http/httputil"
	"net/url"
	"path"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/nats-io/nats.go"
	"github.com/yomezza/cosmosimulator/internal/assetpack"
	"github.com/yomezza/cosmosimulator/internal/platform/configuration"
	"github.com/yomezza/cosmosimulator/internal/platform/gamesignal"
	"github.com/yomezza/cosmosimulator/internal/platform/httpkit"
	"github.com/yomezza/cosmosimulator/internal/platform/mediastore"
	"github.com/yomezza/cosmosimulator/internal/platform/natscore"
	"github.com/yomezza/cosmosimulator/internal/platform/telemetry"
	"github.com/yomezza/cosmosimulator/internal/webbundle"
)

const Version = "0.31.1"

type Config struct {
	Address, StorageRoot, CosmicURL, CDNURL, PlanetURL, ChatURL, Repository, ReleaseTag string
	SyncEnabled                                                                         bool
	SyncInterval                                                                        time.Duration
	DisablePlanet, DisableChat                                                          bool
}

func origin(raw string) (*url.URL, error) {
	u, e := url.Parse(raw)
	if e != nil || u.Host == "" || (u.Scheme != "http" && u.Scheme != "https") || u.User != nil || u.Path != "" || u.RawQuery != "" || u.ForceQuery || u.Fragment != "" {
		return nil, errors.New("Web upstream must be an HTTP origin")
	}
	return u, nil
}
func LoadConfig() (Config, error) {
	get := configuration.Getenv
	c := Config{Address: get("WEB_HTTP_ADDR"), StorageRoot: get("WEB_STORAGE_ROOT"), CosmicURL: get("WEB_COSMIC_INTERNAL_URL"), CDNURL: get("WEB_CDN_INTERNAL_URL"), PlanetURL: get("WEB_PLANET_INTERNAL_URL"), ChatURL: get("WEB_CHAT_INTERNAL_URL"), Repository: get("WEB_REPOSITORY"), ReleaseTag: get("WEB_RELEASE_TAG"), SyncEnabled: true, SyncInterval: 30 * time.Minute}
	for _, v := range []struct {
		p        *string
		fallback string
	}{{&c.Address, "127.0.0.1:8083"}, {&c.StorageRoot, "data/web"}, {&c.CosmicURL, "http://127.0.0.1:8080"}, {&c.CDNURL, "http://127.0.0.1:8084"}, {&c.PlanetURL, "http://127.0.0.1:8081"}, {&c.ChatURL, "http://127.0.0.1:8082"}, {&c.Repository, "yomezza/astrajourney"}, {&c.ReleaseTag, "v" + Version}} {
		if *v.p == "" {
			*v.p = v.fallback
		}
	}
	var e error
	if v := get("WEB_SYNC_ENABLED"); v != "" {
		c.SyncEnabled, e = strconv.ParseBool(v)
		if e != nil {
			return c, errors.New("invalid Web sync_enabled")
		}
	}
	if v := get("WEB_SYNC_INTERVAL"); v != "" {
		c.SyncInterval, e = time.ParseDuration(v)
		if e != nil || c.SyncInterval < time.Minute || c.SyncInterval > 24*time.Hour {
			return c, errors.New("Web sync_interval must be 1m-24h")
		}
	}
	for _, option := range []struct {
		name, fallback string
		disabled       *bool
	}{{"WEB_PLANET_ENABLED", "PLANET_ENABLED", &c.DisablePlanet}, {"WEB_CHAT_ENABLED", "CHAT_ENABLED", &c.DisableChat}} {
		v := get(option.name)
		if v == "" {
			v = get(option.fallback)
		}
		if v != "" {
			enabled, err := strconv.ParseBool(v)
			if err != nil {
				return c, errors.New("invalid Web service enabled setting")
			}
			*option.disabled = !enabled
		}
	}
	if _, _, e = net.SplitHostPort(c.Address); e != nil {
		return c, errors.New("invalid Web http_addr")
	}
	for _, raw := range []string{c.CosmicURL, c.CDNURL, c.PlanetURL, c.ChatURL} {
		if _, e = origin(raw); e != nil {
			return c, e
		}
	}
	return c, nil
}

type mapCache struct {
	sync.Mutex
	content    *httpkit.StaticContent
	loading    *mapFetch
	until      time.Time
	generation uint64
}
type Service struct {
	config                    Config
	bundle                    *webbundle.Store
	cosmic, cdn, planet, chat *httputil.ReverseProxy
	client                    *http.Client
	nats                      *natscore.SignalClient
	started                   time.Time
	generation                atomic.Uint64
	cache                     mapCache
	syncMu                    sync.Mutex
	lastSync, lastError       string
}

func New(c Config) (*Service, error) {
	proxies := make([]*httputil.ReverseProxy, 4)
	for i, raw := range []string{c.CosmicURL, c.CDNURL, c.PlanetURL, c.ChatURL} {
		target, e := origin(raw)
		if e != nil {
			return nil, e
		}
		proxies[i] = &httputil.ReverseProxy{Rewrite: func(p *httputil.ProxyRequest) {
			p.SetURL(target)
			p.Out.Host = p.In.Host
			p.SetXForwarded()
			p.Out.Header.Del(mediastore.Header)
		}, Transport: httpkit.InternalTransport(), ErrorHandler: func(w http.ResponseWriter, r *http.Request, e error) {
			w.Header().Set("Cache-Control", "no-store")
			httpkit.Fail(w, 503, "Служба временно недоступна")
		}}
	}
	bundle, e := webbundle.Open(c.StorageRoot)
	if e != nil {
		return nil, e
	}
	return &Service{config: c, bundle: bundle, cosmic: proxies[0], cdn: proxies[1], planet: proxies[2], chat: proxies[3], started: time.Now(), client: &http.Client{Transport: httpkit.InternalTransport(), Timeout: 3 * time.Second, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}}, nil
}
func (s *Service) Close() error                   { return s.bundle.Close() }
func (s *Service) Install(file string) error      { _, e := s.bundle.Install(file); return e }
func (s *Service) Activate(revision string) error { return s.bundle.Activate(revision) }
func (s *Service) ConfigureNATS(c natscore.Config) error {
	client, e := natscore.NewSignalClient(c, "web", nil, []string{gamesignal.AssetsReadySubject, gamesignal.AssetsChangedSubject}, func(m *nats.Msg) {
		if m.Reply == "" && gamesignal.DecodeAssets(m.Subject, m.Data) {
			s.generation.Add(1)
		}
	}, func() { s.generation.Add(1) })
	if e == nil {
		s.nats = client
	}
	return e
}
func (s *Service) health(w http.ResponseWriter, r *http.Request) {
	revision := s.bundle.Revision()
	ready := revision != ""
	status := "ready"
	if !ready {
		status = "bundle_missing"
	}
	s.syncMu.Lock()
	checked, problem := s.lastSync, s.lastError
	s.syncMu.Unlock()

	if ready {
		request, e := http.NewRequestWithContext(r.Context(), "GET", s.config.CosmicURL+"/api/client-contract", nil)
		compatible := false
		if e == nil {
			response, e := s.client.Do(request)
			if e == nil {
				var contract struct {
					API string `json:"api_contract"`
				}
				compatible = response.StatusCode == 200 && json.NewDecoder(io.LimitReader(response.Body, 32768)).Decode(&contract) == nil && contract.API == webbundle.APIContract
				response.Body.Close()
			}
		}
		if !compatible {
			ready = false
			status = "api_incompatible_or_unavailable"
		}
	}
	code := 200
	if !ready {
		code = 503
	}
	httpkit.Reply(w, code, map[string]any{"service": "astra-journey-web", "version": Version, "status": status, "ready": ready, "revision": revision, "api_contract": webbundle.APIContract, "nats": natscore.StatusFromDiagnostics(s.nats.Diagnostics()), "sync_last_checked": checked, "sync_error": problem, "uptime_seconds": int(time.Since(s.started).Seconds())})
}
func (s *Service) Handler() http.Handler {
	mux := http.NewServeMux()
	// ReverseProxy appends upstream headers. Remove our fallback cache policy
	// before forwarding, otherwise "no-store" would override immutable CDN files.
	forward := func(proxy *httputil.ReverseProxy) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Del("Cache-Control")
			proxy.ServeHTTP(w, r)
		})
	}
	planet, chat := forward(s.planet), forward(s.chat)
	disabled := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		httpkit.Fail(w, 503, "Служба отключена в настройках")
	})
	if s.config.DisablePlanet {
		planet = disabled
	}
	if s.config.DisableChat {
		chat = disabled
	}
	mux.HandleFunc("GET /web/api/v1/health/live", func(w http.ResponseWriter, r *http.Request) {
		httpkit.Reply(w, 200, map[string]any{"service": "astra-journey-web", "version": Version, "status": "alive"})
	})
	mux.HandleFunc("GET /web/api/v1/health/ready", s.health)
	mux.HandleFunc("GET /cdn/api/v1/asset-map", s.assetMap)
	mux.Handle("/api/guide/media/", forward(s.cdn))
	mux.Handle("/api/", forward(s.cosmic))
	mux.Handle("GET /planet/ws/v1", planet)
	mux.Handle("GET /planet/api/v1/scene", planet)
	mux.Handle("GET /planet/api/v1/health/live", planet)
	mux.Handle("GET /planet/api/v1/health/ready", planet)
	mux.Handle("GET /chat/ws/v1", chat)
	mux.Handle("GET /chat/api/v1/{route}", chat)
	mux.Handle("/assets/", forward(s.cdn))
	mux.Handle("/planet-pixelart-v1/", forward(s.cdn))
	mux.Handle("GET /cdn/api/v1/health/live", forward(s.cdn))
	mux.Handle("GET /cdn/api/v1/health/ready", forward(s.cdn))
	mux.Handle("GET /cdn/api/v1/catalog", forward(s.cdn))
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		p := r.URL.Path
		if strings.HasPrefix(p, "/internal/") || strings.HasPrefix(p, "/cdn/") || strings.HasPrefix(p, "/planet/") || strings.HasPrefix(p, "/chat/") {
			http.NotFound(w, r)
			return
		}
		if assetpack.MIME(strings.ToLower(path.Ext(p))) != "" && path.Ext(p) != ".json" {
			forward(s.cdn).ServeHTTP(w, r)
			return
		}
		s.bundle.ServeHTTP(w, r)
	})
	return telemetry.TraceHTTP("web", http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("X-Content-Type-Options", "nosniff")
		w.Header().Set("X-Frame-Options", "DENY")
		w.Header().Set("Referrer-Policy", "same-origin")
		w.Header().Set("Cache-Control", "no-store")
		w.Header().Set("Content-Security-Policy", "default-src 'self'; style-src 'self'; script-src 'self'; frame-ancestors 'none'; base-uri 'none'; form-action 'self'")
		// Per-request deadlines leave upgraded sockets alive, and preserve long world/scene operations.
		if !strings.EqualFold(r.Header.Get("Upgrade"), "websocket") {
			timeout := 20 * time.Second
			if strings.HasPrefix(r.URL.Path, "/api/admin/") {
				timeout = 2 * time.Minute
			}
			if r.URL.Path == "/planet/api/v1/scene" {
				timeout = 90 * time.Second
			}
			c := http.NewResponseController(w)
			_ = c.SetReadDeadline(time.Now().Add(timeout))
			_ = c.SetWriteDeadline(time.Now().Add(timeout))
			defer func() { _ = c.SetReadDeadline(time.Time{}); _ = c.SetWriteDeadline(time.Time{}) }()
		}
		mux.ServeHTTP(w, r)
	}))
}
func (s *Service) Synchronize(ctx context.Context) {
	syncer := webbundle.Sync{Store: s.bundle, Repository: s.config.Repository, Tag: s.config.ReleaseTag}
	_, e := syncer.Check(ctx)
	s.syncMu.Lock()
	s.lastSync = time.Now().UTC().Format(time.RFC3339)
	s.lastError = ""
	if e != nil {
		s.lastError = e.Error()
	}
	s.syncMu.Unlock()
	if e != nil {
		log.Printf("Web bundle sync: %v", e)
	}
}
func Run(ctx context.Context) error {
	if e := configuration.Initialize(configuration.Web); e != nil {
		return e
	}
	closeLogs, e := telemetry.Start("web", configuration.Getenv("DIAGNOSTICS_DIRECTORY"), configuration.Getenv("DIAGNOSTICS_ENABLED") != "false")
	if e != nil {
		return e
	}
	defer closeLogs()
	c, e := LoadConfig()
	if e != nil {
		return e
	}
	s, e := New(c)
	if e != nil {
		return e
	}
	defer s.Close()
	nc, e := natscore.LoadConfig(configuration.Getenv)
	if e != nil {
		return e
	}
	if e = s.ConfigureNATS(nc); e != nil {
		return e
	}
	run, cancel := context.WithCancel(ctx)
	defer cancel()
	var workers sync.WaitGroup
	if s.nats != nil {
		s.nats.Start(run)
		defer func() {
			c, stop := context.WithTimeout(context.Background(), time.Second)
			defer stop()
			_ = s.nats.Close(c)
		}()
	}
	if c.SyncEnabled {
		workers.Go(func() {
			for {
				s.Synchronize(run)
				delay := c.SyncInterval
				if s.bundle.Revision() == "" {
					delay = 10 * time.Second
				}
				timer := time.NewTimer(delay)
				select {
				case <-run.Done():
					timer.Stop()
					return
				case <-timer.C:
				}
			}
		})
	}
	server := &http.Server{Addr: c.Address, Handler: s.Handler(), ReadHeaderTimeout: 5 * time.Second, IdleTimeout: 60 * time.Second, MaxHeaderBytes: 32768}
	done := make(chan struct{})
	go func() {
		defer close(done)
		<-run.Done()
		c, stop := context.WithTimeout(context.Background(), 10*time.Second)
		defer stop()
		if server.Shutdown(c) != nil {
			_ = server.Close()
		}
	}()
	log.Printf("Astra Journey Web listening on %s; storage %s", c.Address, s.bundle.Directory)
	e = server.ListenAndServe()
	cancel()
	workers.Wait()
	<-done
	if errors.Is(e, http.ErrServerClosed) {
		return nil
	}
	return e
}
