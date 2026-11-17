// Package cdnserver delivers external graphics and private media without SQL.
package cdnserver

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"log"
	"net"
	"net/http"
	"net/http/httputil"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/yomezza/cosmosimulator/internal/assetpack"
	"github.com/yomezza/cosmosimulator/internal/platform/configuration"
	"github.com/yomezza/cosmosimulator/internal/platform/fileatom"
	"github.com/yomezza/cosmosimulator/internal/platform/httpkit"
	"github.com/yomezza/cosmosimulator/internal/platform/mediastore"
	"github.com/yomezza/cosmosimulator/internal/platform/natscore"
	"github.com/yomezza/cosmosimulator/internal/platform/telemetry"
)

const Version = "0.29.0"

type Config struct {
	Address, StorageRoot, CosmicURL, TokenFile, Repository string
	SyncEnabled                                            bool
	SyncInterval                                           time.Duration
}

func LoadConfig() (Config, error) {
	c := Config{Address: configuration.Getenv("CDN_HTTP_ADDR"), StorageRoot: configuration.Getenv("CDN_STORAGE_ROOT"), CosmicURL: configuration.Getenv("CDN_COSMIC_INTERNAL_URL"), TokenFile: configuration.Getenv("CDN_TOKEN_FILE"), Repository: configuration.Getenv("CDN_REPOSITORY"), SyncEnabled: configuration.Getenv("CDN_SYNC_ENABLED") != "false", SyncInterval: 30 * time.Minute}
	if c.Address == "" {
		c.Address = "127.0.0.1:8083"
	}
	if c.StorageRoot == "" {
		c.StorageRoot = filepath.Join("data", "cdn")
	}
	if c.CosmicURL == "" {
		c.CosmicURL = "http://127.0.0.1:8080"
	}
	if c.Repository == "" {
		c.Repository = "yomezza/astrajourney"
	}
	if v := configuration.Getenv("CDN_SYNC_ENABLED"); v != "" {
		enabled, e := strconv.ParseBool(v)
		if e != nil {
			return c, errors.New("invalid CDN sync_enabled boolean")
		}
		c.SyncEnabled = enabled
	}
	if c.TokenFile == "" {
		c.TokenFile = filepath.Join(c.StorageRoot, "state", "service.token")
	}
	if v := configuration.Getenv("CDN_SYNC_INTERVAL"); v != "" {
		d, e := time.ParseDuration(v)
		if e != nil || d < time.Minute || d > 24*time.Hour {
			return c, errors.New("CDN sync_interval must be 1m-24h")
		}
		c.SyncInterval = d
	}
	u, e := url.Parse(c.CosmicURL)
	if e != nil || u.Host == "" || (u.Scheme != "http" && u.Scheme != "https") || u.User != nil || u.Path != "" || u.RawQuery != "" || u.Fragment != "" {
		return c, errors.New("CDN cosmic_internal_url must be an HTTP origin")
	}
	if _, _, e = net.SplitHostPort(c.Address); e != nil {
		return c, errors.New("invalid CDN http_addr")
	}
	return c, nil
}

type Service struct {
	count               counters
	owner               *os.File
	config              Config
	store               *assetpack.Store
	media               *mediaFiles
	token               string
	internal            *http.Client
	proxy               *httputil.ReverseProxy
	nats                *natscore.SignalClient
	started             time.Time
	syncMu              sync.Mutex
	lastSync, lastError string
}

func New(c Config) (*Service, error) {
	store, err := assetpack.Open(c.StorageRoot)
	if err != nil {
		return nil, err
	}
	owner, err := os.OpenFile(filepath.Join(store.Directory, "state", "owner.lock"), os.O_CREATE|os.O_RDWR, 0600)
	if err != nil {
		store.Close()
		return nil, err
	}
	if err = lockStorage(owner); err != nil {
		owner.Close()
		store.Close()
		return nil, errors.New("CDN storage is already owned by another process")
	}
	opened := false
	defer func() {
		if !opened {
			owner.Close()
		}
	}()

	if err = clearInterruptedStaging(store.Directory); err != nil {
		store.Close()
		return nil, err
	}

	if c.TokenFile == "" {
		c.TokenFile = filepath.Join(store.Directory, "state", "service.token")
	}
	if _, err = os.Stat(c.TokenFile); os.IsNotExist(err) {
		if err = os.MkdirAll(filepath.Dir(c.TokenFile), 0700); err != nil {
			store.Close()
			return nil, err
		}
		b := make([]byte, 32)
		if _, err = rand.Read(b); err == nil {
			err = fileatom.Write(c.TokenFile, []byte(hex.EncodeToString(b)), 0600)
		}
		if err != nil {
			store.Close()
			return nil, err
		}
	}
	token, err := mediastore.Token(c.TokenFile)
	if err != nil {
		store.Close()
		return nil, err
	}
	root, err := os.OpenRoot(store.Directory)
	if err != nil {
		store.Close()
		return nil, err
	}
	target, err := url.Parse(c.CosmicURL)
	if err != nil || target.Host == "" {
		root.Close()
		store.Close()
		return nil, errors.New("invalid Cosmic URL")
	}
	proxy := &httputil.ReverseProxy{Rewrite: func(p *httputil.ProxyRequest) {
		p.SetURL(target)
		p.Out.Host = p.In.Host
		p.SetXForwarded() // Rebuild forwarding headers; never trust browser-supplied XFF.
		p.Out.Header.Del(mediastore.Header)
	}}
	proxy.Transport = httpkit.InternalTransport()
	proxy.ErrorHandler = func(w http.ResponseWriter, r *http.Request, e error) { httpkit.Fail(w, 503, "Cosmic unavailable") }
	opened = true
	return &Service{owner: owner, config: c, store: store, media: &mediaFiles{root: root, directory: store.Directory, slots: make(chan struct{}, 2)}, token: token, proxy: proxy, started: time.Now(), internal: &http.Client{Transport: httpkit.InternalTransport(), Timeout: 3 * time.Second, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}}, nil
}
func (s *Service) Close() error {
	s.media.root.Close()
	err := s.store.Close()
	s.owner.Close()
	return err
}
func (s *Service) Install(path string) error { _, e := s.store.Install(path); return e }
func (s *Service) Handler() http.Handler {
	m := http.NewServeMux()
	m.HandleFunc("GET /cdn/api/v1/health/live", func(w http.ResponseWriter, r *http.Request) {
		httpkit.Reply(w, 200, map[string]any{"service": "astra-journey-cdn", "version": Version, "status": "alive"})
	})
	m.HandleFunc("GET /cdn/api/v1/health/ready", s.health)
	m.HandleFunc("GET /cdn/api/v1/metrics", s.metrics)
	m.HandleFunc("GET /cdn/api/v1/asset-map", s.store.ServeMap)
	m.HandleFunc("GET /cdn/api/v1/catalog", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Cache-Control", "no-cache")
		revisions := s.store.Revisions()
		httpkit.Reply(w, 200, map[string]any{"schema_version": assetpack.Schema, "client_contract": assetpack.Contract, "revisions": revisions})
	})
	m.HandleFunc("/cdn/internal/v1/media/{id}", s.internalMedia)
	m.HandleFunc("GET /api/guide/media/{id}", func(w http.ResponseWriter, r *http.Request) {
		count := &responseCount{ResponseWriter: w}
		s.privateMedia(count, r)
		s.record(count)
	})
	m.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		if strings.HasPrefix(r.URL.Path, "/internal/") {
			http.NotFound(w, r)
			return
		}
		counted := &responseCount{ResponseWriter: w}
		if s.store.ServePublic(counted, r) {
			s.record(counted)
			return
		}
		// A missing graphics URL never falls through to Cosmic or HTML.
		ext := strings.ToLower(filepath.Ext(r.URL.Path))
		mime := assetpack.MIME(ext)
		if strings.HasPrefix(r.URL.Path, "/assets/") || strings.HasPrefix(r.URL.Path, "/planet-pixelart-v1/") || strings.HasPrefix(r.URL.Path, "/cdn/") || (mime != "" && mime != "application/json") {
			s.count.Missing.Add(1)
			http.NotFound(w, r)
			return
		}
		w.Header().Del("Cache-Control")
		s.proxy.ServeHTTP(w, r)
	})
	return telemetry.TraceHTTP("cdn", http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("X-Content-Type-Options", "nosniff")
		w.Header().Set("Cache-Control", "no-store")
		m.ServeHTTP(w, r)
	}))
}
func (s *Service) health(w http.ResponseWriter, r *http.Request) {
	revisions := s.store.Revisions()
	ready := len(revisions) > 0
	status, code := "ready", 200
	if !ready {
		status = "assets_missing"
		code = 503
	}
	s.syncMu.Lock()
	last, problem := s.lastSync, s.lastError
	s.syncMu.Unlock()
	httpkit.Reply(w, code, map[string]any{"service": "astra-journey-cdn", "version": Version, "status": status, "ready": ready, "revisions": revisions, "nats": natscore.StatusFromDiagnostics(s.nats.Diagnostics()), "sync_last_checked": last, "sync_error": problem, "uptime_seconds": int(time.Since(s.started).Seconds())})
}
func (s *Service) Synchronize(ctx context.Context) {
	syncer := assetpack.Sync{Store: s.store, Repository: s.config.Repository, RequiredTag: "v" + Version}
	// Bootstrap the matching release first, then discover compatible newer packs.
	for _, latest := range []bool{false, true} {
		changed, err := syncer.Check(ctx, latest)
		s.syncMu.Lock()
		s.lastSync = time.Now().UTC().Format(time.RFC3339)
		s.lastError = ""
		if err != nil {
			s.lastError = err.Error()
		}
		s.syncMu.Unlock()
		if err != nil {
			log.Printf("CDN graphics sync: %v", err)
			if !latest && len(s.store.Revisions()) == 0 {
				return
			}
		}
		if changed {
			s.publishAssets()
			s.publishChanged()
		}
	}
}
func Run(ctx context.Context) error {
	if err := configuration.Initialize(configuration.CDN); err != nil {
		return err
	}
	closeLogs, err := telemetry.Start("cdn", configuration.Getenv("DIAGNOSTICS_DIRECTORY"), configuration.Getenv("DIAGNOSTICS_ENABLED") != "false")
	if err != nil {
		return err
	}
	defer closeLogs()
	config, err := LoadConfig()
	if err != nil {
		return err
	}
	s, err := New(config)
	if err != nil {
		return err
	}
	defer s.Close()
	natsConfig, err := natscore.LoadConfig(configuration.Getenv)
	if err != nil {
		return err
	}
	if err = s.ConfigureNATS(natsConfig); err != nil {
		return err
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
	if config.SyncEnabled {
		workers.Add(1)
		go func() {
			defer workers.Done()
			for {
				s.Synchronize(run)
				delay := config.SyncInterval
				if len(s.store.Revisions()) == 0 && delay > 10*time.Second {
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
		}()
	}
	server := &http.Server{Addr: config.Address, Handler: s.Handler(), ReadHeaderTimeout: 5 * time.Second, ReadTimeout: 30 * time.Second, WriteTimeout: 45 * time.Second, IdleTimeout: 60 * time.Second, MaxHeaderBytes: 32768}
	shutdownDone := make(chan struct{})
	go func() {
		defer close(shutdownDone)
		<-run.Done()
		c, stop := context.WithTimeout(context.Background(), 10*time.Second)
		defer stop()
		if server.Shutdown(c) != nil {
			_ = server.Close()
		}
	}()
	log.Printf("Astra Journey CDN listening on %s; storage %s", config.Address, s.store.Directory)
	err = server.ListenAndServe()
	cancel()
	workers.Wait()
	<-shutdownDone
	if errors.Is(err, http.ErrServerClosed) {
		return nil
	}
	return err
}
