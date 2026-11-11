// Package chatserver composes the independently deployable Chat Service.
package chatserver

import (
	"context"
	"encoding/json"
	"errors"
	"github.com/yomezza/cosmosimulator/internal/platform/natscore"
	"io"
	"log/slog"
	"net"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/coder/websocket"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/yomezza/cosmosimulator/internal/account"
	"github.com/yomezza/cosmosimulator/internal/chat"
	"github.com/yomezza/cosmosimulator/internal/chatintegration"
	"github.com/yomezza/cosmosimulator/internal/platform/configuration"
	"github.com/yomezza/cosmosimulator/internal/platform/httpkit"
	"github.com/yomezza/cosmosimulator/internal/platform/postgres"
)

const Version = "0.24.14"

type Config struct {
	DatabaseURL, Schema, Addr, CosmicURL, PlanetURL, Journal string
	Origins                                                  []string
	MaintenanceURL                                           string
	Dispatcher                                               DispatcherConfig
	NATS                                                     NATSConfig
}

func LoadConfig(getenv func(string) string) (Config, error) {
	c := Config{DatabaseURL: getenv("CHAT_DATABASE_URL"), MaintenanceURL: getenv("CHAT_MAINTENANCE_DATABASE_URL"), Schema: getenv("DB_SCHEMA"), Addr: getenv("CHAT_HTTP_ADDR"), CosmicURL: getenv("COSMIC_INTERNAL_URL"), PlanetURL: getenv("PLANET_INTERNAL_URL"), Journal: getenv("CHAT_ERASURE_JOURNAL")}
	var err error
	c.Dispatcher, err = dispatcherConfig(getenv)
	if err != nil {
		return c, err
	}
	c.NATS, err = loadNATSConfig(getenv)
	if err != nil {
		return c, err
	}
	if c.Dispatcher.Mode == "b1" {
		if err = c.NATS.Validate("b1"); err != nil {
			return c, err
		}
		if _, _, err = c.NATS.credentials(); err != nil {
			return c, err
		}
	}
	if c.DatabaseURL == "" {
		c.DatabaseURL = getenv("DATABASE_URL")
	}
	if c.Addr == "" {
		c.Addr = "127.0.0.1:8082"
	}
	if c.CosmicURL == "" {
		c.CosmicURL = "http://127.0.0.1:8080"
	}
	if c.PlanetURL == "" {
		c.PlanetURL = "http://127.0.0.1:8081"
	}
	origin := getenv("CHAT_ALLOWED_ORIGINS")
	if origin == "" {
		origin = getenv("PUBLIC_ORIGIN")
	}
	if origin == "" {
		origin = "http://127.0.0.1:8080,http://localhost:8080"
	}
	for _, v := range strings.Split(origin, ",") {
		c.Origins = append(c.Origins, strings.TrimRight(strings.TrimSpace(v), "/"))
	}
	return c, c.Validate()
}
func (c Config) Validate() error {
	if err := c.Dispatcher.Validate(); err != nil {
		return err
	}
	if err := c.NATS.Validate(c.Dispatcher.Mode); err != nil {
		return err
	}
	if c.DatabaseURL == "" || c.Journal == "" {
		return errors.New("configure database.url (or chat.database_url) and chat.erasure_journal in TOML, or CHAT_DATABASE_URL/DATABASE_URL and CHAT_ERASURE_JOURNAL in environment")
	}
	if _, _, e := net.SplitHostPort(c.Addr); e != nil {
		return errors.New("CHAT_HTTP_ADDR must be host:port")
	}
	for _, v := range append(append([]string{}, c.Origins...), c.CosmicURL, c.PlanetURL) {
		u, e := url.Parse(v)
		if e != nil || u.Host == "" || u.User != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Path != "" || u.RawQuery != "" || u.Fragment != "" || strings.Contains(u.Host, "*") {
			return errors.New("Chat endpoints/origins must be explicit http(s) origins")
		}
	}
	return nil
}

type Health struct {
	NATS        natscore.Status `json:"nats"`
	Cosmic      bool            `json:"cosmic"`
	Planet      bool            `json:"planet"`
	Ready       bool            `json:"ready"`
	Version     string          `json:"version"`
	Service     string          `json:"service"`
	Schema      int             `json:"schema_version"`
	Status      string          `json:"status"`
	Maintenance string          `json:"maintenance"`
}
type App struct {
	Service                        *chat.Service
	config                         Config
	instance                       string
	mu                             sync.Mutex
	health                         Health
	connections                    map[*websocket.Conn]bool
	stopping                       bool
	checkedAt                      time.Time
	cosmicFailures, planetFailures int
	maintenanceState               string
	registry                       *WSRegistry
	dispatcher                     *EventDispatcher
	source                         WakeSource
	socketWG                       sync.WaitGroup
	syncWrites                     atomic.Uint64
	serving                        atomic.Bool
}

func New(s *chat.Service, c Config) (*App, error) {
	// Existing Go embeddings omit dispatcher settings, just like old TOML/ENV.
	defaults := DefaultDispatcherConfig()
	if c.Dispatcher.Mode == "" {
		c.Dispatcher.Mode = defaults.Mode
	}
	if c.Dispatcher.PollInterval == 0 {
		c.Dispatcher.PollInterval = defaults.PollInterval
	}
	if c.Dispatcher.DegradedInterval == 0 {
		c.Dispatcher.DegradedInterval = defaults.DegradedInterval
	}
	if c.Dispatcher.Overlap == 0 {
		c.Dispatcher.Overlap = defaults.Overlap
	}
	if e := c.Validate(); e != nil {
		return nil, e
	}
	a := &App{Service: s, config: c, instance: chat.UUID(), connections: map[*websocket.Conn]bool{}}
	a.registry = NewWSRegistry()
	if c.Dispatcher.Mode != "legacy" {
		if c.Dispatcher.Mode == "b1" {
			var err error
			a.source, err = NewNATSWakeSource(c.NATS)
			if err != nil {
				return nil, err
			}
		} else if c.Dispatcher.Mode == "a2" {
			a.source = NewPostgresWakeSource(s.DB)
		} else {
			a.source = NewLocalWakeSource()
		}
		a.dispatcher = NewEventDispatcher(s, a.registry, a.source, c.Dispatcher)
	}
	s.PlanetAvailable = func() bool { h := a.Health(); return h.Ready && h.Planet }
	return a, nil
}
func (a *App) Health() Health {
	a.mu.Lock()
	defer a.mu.Unlock()
	h := a.health
	h.NATS = a.NATSStatus()
	h.Maintenance = a.maintenanceState
	if time.Since(a.checkedAt) > 3*time.Second {
		h.Ready = false
		h.Cosmic = false
		h.Planet = false
		h.Status = "stale"
	}
	return h
}
func (a *App) probe(ctx context.Context) {
	client := http.Client{Transport: httpkit.InternalTransport(), Timeout: 900 * time.Millisecond, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	probe := func(endpoint, path string) bool {
		req, e := http.NewRequestWithContext(ctx, "GET", endpoint+path, nil)
		if e != nil {
			return false
		}
		r, e := client.Do(req)
		if e != nil {
			return false
		}
		defer r.Body.Close()
		if r.StatusCode != 200 {
			return false
		}
		var v map[string]any
		if json.NewDecoder(io.LimitReader(r.Body, 4096)).Decode(&v) != nil {
			return false
		}
		return v["status"] == "ok" || v["status"] == "ready"
	}
	cosmic, planet := probe(a.config.CosmicURL, "/api/health"), probe(a.config.PlanetURL, "/planet/api/v1/health/ready")
	ping, cancel := context.WithTimeout(ctx, time.Second)
	defer cancel()
	database := a.Service.DB.Ping(ping) == nil
	a.mu.Lock()
	if cosmic {
		a.cosmicFailures = 0
	} else {
		a.cosmicFailures++
		cosmic = a.health.Cosmic && a.cosmicFailures < 2
	}
	if planet {
		a.planetFailures = 0
	} else {
		a.planetFailures++
		planet = a.health.Planet && a.planetFailures < 2
	}
	a.checkedAt = time.Now()
	a.health = Health{Cosmic: cosmic, Planet: planet, Ready: cosmic && database, Version: Version, Service: "chat", Schema: chat.SchemaVersion, Status: "unavailable"}
	if a.health.Ready {
		a.health.Status = "ready"
	}
	a.mu.Unlock()
}
func (a *App) Handler() http.Handler {
	m := http.NewServeMux()
	m.HandleFunc("GET /chat/api/v1/health", func(w http.ResponseWriter, r *http.Request) {
		h := a.Health()
		status := 200
		if !h.Ready {
			status = 503
		}
		httpkit.Reply(w, status, h)
	})
	m.HandleFunc("GET /chat/ws/v1", a.socket)
	m.HandleFunc("GET /chat/api/v1/{route}", a.query)
	return bounded(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Cache-Control", "no-store")
		w.Header().Set("X-Content-Type-Options", "nosniff")
		m.ServeHTTP(w, r)
	}))
}
func (a *App) identity(ctx context.Context, r *http.Request) (chat.Identity, error) {
	c, e := r.Cookie("session")
	if e != nil || len(c.Value) != 64 {
		return chat.Identity{}, errors.New("auth_required")
	}
	return a.Service.Authenticate(ctx, account.TokenHash(c.Value))
}
func code(e error) string {
	switch e.Error() {
	case "access_denied", "auth_required", "invalid_argument", "duplicate_chat_connection", "stale_connection", "operation_unknown", "operation_pending", "idempotency_conflict", "rate_limited", "muted", "scope_changed", "presence_unverifiable", "conversation_deleting", "conversation_pending", "message_missing", "unsupported_command", "invalid_recipient", "invitation_expired", "invitation_pending", "invitation_cooldown", "already_member", "group_capacity", "owner_required", "confirmation_required", "revision_conflict", "reason_required", "reason_must_not_quote_message", "search_too_short", "invalid_duration", "retry_later":
		return e.Error()
	}
	return "request_failed"
}
func (a *App) query(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), 6*time.Second)
	defer cancel()
	u, e := a.identity(ctx, r)
	if e != nil {
		httpkit.Fail(w, 401, "auth_required")
		return
	}
	h := a.Health()
	if !h.Ready {
		httpkit.Fail(w, 503, "cosmic_unavailable")
		return
	}
	q := r.URL.Query()
	t := chat.Target{Kind: q.Get("kind"), Target: q.Get("target")}
	if (t.Kind == "planet" || t.Kind == "sector") && !h.Planet {
		httpkit.Fail(w, 503, "planet_unavailable")
		return
	}
	var v any
	if r.PathValue("route") == "history" {
		v, e = a.Service.History(ctx, u, t, chat.ParseBefore(q.Get("before")), q.Get("search"))
	} else {
		v, e = a.Service.Query(ctx, u, r.PathValue("route"), t, q.Get("id"), q.Get("search"), q.Get("before"))
	}
	if e != nil {
		status := 400
		if code(e) == "access_denied" {
			status = 403
		}
		httpkit.Fail(w, status, code(e))
		return
	}
	httpkit.Reply(w, 200, v)
}
func (a *App) socket(w http.ResponseWriter, r *http.Request) {
	origin := r.Header.Get("Origin")
	valid := false
	for _, o := range a.config.Origins {
		if origin == o {
			valid = true
		}
	}
	if !valid {
		httpkit.Fail(w, 403, "origin_denied")
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 5*time.Second)
	u, e := a.identity(ctx, r)
	cancel()
	if e != nil {
		httpkit.Fail(w, 401, "auth_required")
		return
	}
	a.mu.Lock()
	full := len(a.connections) >= 256
	a.mu.Unlock()
	if full {
		httpkit.Fail(w, 429, "server_busy")
		return
	}
	if !a.Health().Ready {
		httpkit.Fail(w, 503, "cosmic_unavailable")
		return
	}
	ctx, cancel = context.WithTimeout(r.Context(), 3*time.Second)
	lease, e := a.Service.Claim(ctx, u, a.instance)
	cancel()
	if e != nil {
		httpkit.Fail(w, 409, code(e))
		return
	}
	trackedSocket := false
	defer func() {
		c, done := context.WithTimeout(context.Background(), time.Second)
		defer done()
		a.Service.Release(c, lease)
		if trackedSocket {
			a.socketWG.Done()
		}
	}()
	a.mu.Lock()
	if a.stopping {
		a.mu.Unlock()
		httpkit.Fail(w, 503, "chat_unavailable")
		return
	}
	a.socketWG.Add(1)
	trackedSocket = true
	a.mu.Unlock()
	conn, e := websocket.Accept(w, r, &websocket.AcceptOptions{InsecureSkipVerify: true, CompressionMode: websocket.CompressionDisabled})
	if e != nil {
		return
	}
	conn.SetReadLimit(24 << 10)
	a.mu.Lock()
	if a.stopping {
		a.mu.Unlock()
		conn.CloseNow()
		return
	}
	a.connections[conn] = true
	a.mu.Unlock()
	defer func() { a.mu.Lock(); delete(a.connections, conn); a.mu.Unlock(); conn.CloseNow() }()
	run, stop := context.WithCancel(r.Context())
	defer stop()
	handle, ok := a.registry.Register(u, lease, stop)
	if !ok {
		return
	}
	defer a.registry.Unregister(handle)
	var pendingSync <-chan struct{}
	if a.dispatcher != nil {
		pendingSync = handle.Sync
	}
	commands := make(chan chat.Command, 16)
	readDone := make(chan struct{})
	go func() {
		defer close(readDone)
		tokens, last := 20.0, time.Now()
		for {
			_, body, e := conn.Read(run)
			if e != nil {
				return
			}
			elapsed := time.Since(last).Seconds()
			last = time.Now()
			tokens = min(20, tokens+elapsed*10)
			if tokens < 1 {
				return
			}
			tokens--
			var cmd chat.Command
			dec := json.NewDecoder(strings.NewReader(string(body)))
			dec.DisallowUnknownFields()
			if dec.Decode(&cmd) != nil || dec.Decode(new(any)) != io.EOF {
				return
			}
			select {
			case commands <- cmd:
			default:
				return
			}
		}
	}()
	defer func() { stop(); conn.CloseNow(); <-readDone }()
	write := func(v any) bool {
		ctx, cancel := context.WithTimeout(run, 3*time.Second)
		defer cancel()
		b, e := json.Marshal(v)
		return e == nil && conn.Write(ctx, websocket.MessageText, b) == nil
	}
	var scopeStamp string
	if a.dispatcher != nil {
		check, cancel := context.WithTimeout(run, 3*time.Second)
		scopeStamp, e = a.Service.ScopeStamp(check, lease)
		cancel()
		if e != nil {
			return
		}
	}
	if !write(map[string]any{"type": "hello", "user": u, "health": a.Health()}) {
		return
	}
	writeSync := func() bool {
		if !a.registry.Current(handle) {
			return false
		}
		if !write(map[string]any{"type": "sync"}) {
			return false
		}
		a.syncWrites.Add(1)
		return true
	}
	if a.dispatcher != nil && !writeSync() {
		return
	}
	ticker := time.NewTicker(2 * time.Second)
	defer ticker.Stop()
	lastRenew := time.Now()
	var cursor int64
	for {
		select {
		case <-run.Done():
			return
		case <-readDone:
			return
		case <-pendingSync:
			if !writeSync() {
				return
			}
		case cmd := <-commands:
			h := a.Health()
			var in chat.Input
			_ = json.Unmarshal(cmd.Payload, &in)
			var result json.RawMessage
			var err error
			if !h.Ready {
				err = errors.New("cosmic_unavailable")
			} else if (in.Kind == "planet" || in.Kind == "sector") && !h.Planet {
				err = errors.New("planet_unavailable")
			} else {
				c, done := context.WithTimeout(run, 8*time.Second)
				result, err = a.Service.Execute(c, u, lease, cmd)
				done()
			}
			v := map[string]any{"type": "ack", "request_id": cmd.Request, "result": result}
			if err != nil {
				v["error"] = code(err)
				if err.Error() == "cosmic_unavailable" || err.Error() == "planet_unavailable" {
					v["error"] = err.Error()
				}
			}
			if !write(v) {
				return
			}
		case <-ticker.C:
			c, done := context.WithTimeout(run, 3*time.Second)
			fresh, e := a.Service.Authenticate(c, u.Session)
			if e == nil && time.Since(lastRenew) > 10*time.Second {
				e = a.Service.Renew(c, lease)
				lastRenew = time.Now()
			}
			var next int64
			var changed bool
			var stamp string
			if e == nil {
				if a.dispatcher == nil {
					next, changed, stamp, e = a.Service.Changes(c, u, cursor)
				} else {
					stamp, e = a.Service.ScopeStamp(c, lease)
				}
			}
			done()
			if e != nil {
				return
			}
			u = fresh
			if !write(map[string]any{"type": "health", "health": a.Health()}) {
				return
			}
			if changed || stamp != scopeStamp {
				scopeStamp = stamp
				cursor = next
				if !writeSync() {
					return
				}
			}
			cursor = next
		}
	}
}
func (a *App) Serve(ctx context.Context, listener net.Listener, started func()) error {
	if !a.serving.CompareAndSwap(false, true) {
		return errors.New("Chat App already served")
	}
	a.probe(ctx)
	run, cancel := context.WithCancel(ctx)
	defer cancel()
	workers := sync.WaitGroup{}
	if a.dispatcher != nil {
		unsubscribe := a.Service.ObserveCommits(func(string) { a.source.AfterCommit() })
		defer unsubscribe()
		workers.Add(1)
		go func() { defer workers.Done(); a.dispatcher.Run(run) }()
	}
	var maintenanceWake <-chan struct{}
	if source, ok := a.source.(*NATSWakeSource); ok {
		maintenanceWake = source.MaintenanceWakeups()
		unsubscribe := a.Service.ObserveCompanyCleanup(source.CompanyCleanup)
		defer unsubscribe()
	}
	workers.Add(2)
	go func() {
		defer workers.Done()
		tick := time.NewTicker(2 * time.Second)
		defer tick.Stop()
		count := 0
		for {
			select {
			case <-run.Done():
				return
			case <-tick.C:
				a.probe(run)
				count++
				if count%15 == 0 {
					slog.Info("chat dispatcher diagnostics", "metrics", a.Diagnostics())
				}
			}
		}
	}()
	go func() {
		defer workers.Done()
		tick := time.NewTicker(10 * time.Second)
		defer tick.Stop()
		for {
			select {
			case <-run.Done():
				return
			case <-maintenanceWake:
			case <-tick.C:
			}
			if run.Err() != nil {
				return
			}
			c, done := context.WithTimeout(run, 9*time.Second)
			e := a.Service.Maintain(c)
			a.mu.Lock()
			if e != nil {
				a.maintenanceState = "failed"
			} else {
				a.maintenanceState = "ready"
			}
			a.mu.Unlock()
			if e != nil {
				slog.Warn("chat maintenance failed", "code", code(e))
			}
			done()
		}
	}()
	defer workers.Wait()
	server := &http.Server{Handler: a.Handler(), ReadHeaderTimeout: 5 * time.Second, IdleTimeout: 60 * time.Second, MaxHeaderBytes: 16 << 10}
	errc := make(chan error, 1)
	go func() { errc <- server.Serve(listener) }()
	if started != nil {
		started()
	}
	var err error
	select {
	case <-ctx.Done():
	case err = <-errc:
	}
	cancel()
	a.mu.Lock()
	a.stopping = true
	for conn := range a.connections {
		conn.CloseNow()
	}
	a.mu.Unlock()
	a.registry.Close()
	stop, done := context.WithTimeout(context.Background(), 8*time.Second)
	defer done()
	server.Shutdown(stop)
	a.socketWG.Wait()
	if errors.Is(err, http.ErrServerClosed) {
		err = nil
	}
	return err
}
func RunStarted(ctx context.Context, started func()) error {
	if err := configuration.Initialize(configuration.Chat); err != nil {
		return err
	}
	c, e := LoadConfig(configuration.Getenv)
	if e != nil {
		return e
	}
	check, done := context.WithTimeout(ctx, 2*time.Minute)
	defer done()
	db, _, e := postgres.Open(check, c.DatabaseURL, c.Schema)
	if e != nil {
		return errors.New("Chat PostgreSQL unavailable; check configuration")
	}
	defer db.Close()
	lock, e := db.Acquire(check)
	if e != nil {
		return e
	}
	defer lock.Release()
	var exclusive bool
	if e = lock.QueryRow(check, `SELECT pg_try_advisory_lock(hashtextextended(current_schema()||':chat-runtime',784242))`).Scan(&exclusive); e != nil || !exclusive {
		return errors.New("another Chat process already owns recovery; stop it before starting this service")
	}
	defer lock.Exec(context.Background(), `SELECT pg_advisory_unlock(hashtextextended(current_schema()||':chat-runtime',784242))`)
	var maintenance *pgxpool.Pool
	if c.MaintenanceURL != "" {
		maintenance, _, e = postgres.Open(check, c.MaintenanceURL, c.Schema)
		if e != nil {
			return errors.New("Chat maintenance connection unavailable")
		}
		defer maintenance.Close()
		if e = chatintegration.MaintainPartitions(check, maintenance, time.Now()); e != nil {
			return errors.New("Chat partition maintenance failed")
		}
	}

	var version int
	if e = db.QueryRow(check, `SELECT version FROM chat_schema_state WHERE id=1`).Scan(&version); e != nil || version != chat.SchemaVersion {
		return errors.New("Chat schema incompatible; start upgraded Cosmic with CHAT_ENABLED=true first")
	}
	s := &chat.Service{DB: db}
	journal, e := chat.OpenJournal(check, s, c.Journal)
	if e != nil {
		return e
	}
	defer journal.Close()
	s.Journal = journal
	a, e := New(s, c)
	if e != nil {
		return e
	}
	listener, e := net.Listen("tcp", c.Addr)
	if e != nil {
		return e
	}
	defer listener.Close()
	run, cancel := context.WithCancel(ctx)
	defer cancel()
	var workers sync.WaitGroup
	if maintenance != nil {
		workers.Add(1)
		go func() {
			defer workers.Done()
			ticker := time.NewTicker(time.Hour)
			defer ticker.Stop()
			for {
				select {
				case <-run.Done():
					return
				case <-ticker.C:
					check, done := context.WithTimeout(run, 30*time.Second)
					_ = chatintegration.MaintainPartitions(check, maintenance, time.Now())
					done()
				}
			}
		}()
	}
	result := a.Serve(run, listener, started)
	cancel()
	workers.Wait()
	return result
}

func Run(ctx context.Context) error { return RunStarted(ctx, nil) }
