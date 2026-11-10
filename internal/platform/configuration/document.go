package configuration

import (
	"strconv"
	"strings"
)

type document struct {
	Database struct {
		URL    string `toml:"url"`
		Schema string `toml:"schema"`
	} `toml:"database"`
	Cosmos struct {
		HTTPAddr          string   `toml:"http_addr"`
		PublicOrigin      string   `toml:"public_origin"`
		CookieSecure      *bool    `toml:"cookie_secure"`
		TrustedProxies    []string `toml:"trusted_proxies"`
		PlanetEnabled     *bool    `toml:"planet_enabled"`
		PlanetInternalURL string   `toml:"planet_internal_url"`
		ChatEnabled       *bool    `toml:"chat_enabled"`
		ChatInternalURL   string   `toml:"chat_internal_url"`
	} `toml:"cosmos"`
	Admin struct {
		Username string `toml:"username"`
		Password string `toml:"password"`
	} `toml:"admin"`
	Planet struct {
		HTTPAddr         string   `toml:"http_addr"`
		AllowedOrigins   []string `toml:"allowed_origins"`
		LogFile          string   `toml:"log_file"`
		MoveSpeed        *float64 `toml:"move_speed"`
		PathBudget       *int     `toml:"path_budget"`
		DBTimeout        string   `toml:"db_timeout"`
		LeaseTTL         string   `toml:"lease_ttl"`
		LeaseRenew       string   `toml:"lease_renew"`
		ShutdownTimeout  string   `toml:"shutdown_timeout"`
		SceneCapacity    *int     `toml:"scene_capacity"`
		SceneIdleTTL     string   `toml:"scene_idle_ttl"`
		SceneLoadTimeout string   `toml:"scene_load_timeout"`
	} `toml:"planet"`
	Chat struct {
		DatabaseURL            string   `toml:"database_url"`
		HTTPAddr               string   `toml:"http_addr"`
		CosmicInternalURL      string   `toml:"cosmic_internal_url"`
		PlanetInternalURL      string   `toml:"planet_internal_url"`
		AllowedOrigins         []string `toml:"allowed_origins"`
		ErasureJournal         string   `toml:"erasure_journal"`
		MaintenanceDatabaseURL string   `toml:"maintenance_database_url"`
		RuntimeRole            string   `toml:"runtime_role"`
		DefinerRole            string   `toml:"definer_role"`
		DispatcherMode         string   `toml:"dispatcher_mode"`
		FallbackPollInterval   string   `toml:"fallback_poll_interval"`
		DegradedPollInterval   string   `toml:"degraded_poll_interval"`
		OutboxOverlap          string   `toml:"outbox_overlap"`
	} `toml:"chat"`
	NATS *struct {
		Enabled         *bool    `toml:"enabled"`
		ServiceIdentity string   `toml:"service_identity"`
		Servers         []string `toml:"servers"`
		Username        string   `toml:"username"`
		PasswordFile    string   `toml:"password_file"`
		CAFile          string   `toml:"ca_file"`
		ConnectTimeout  string   `toml:"connect_timeout"`
		ReconnectWait   string   `toml:"reconnect_wait"`
	} `toml:"nats"`
}

func value[T bool | int | float64](v *T) string {
	if v == nil {
		return ""
	}
	switch n := any(*v).(type) {
	case bool:
		return strconv.FormatBool(n)
	case int:
		return strconv.Itoa(n)
	case float64:
		return strconv.FormatFloat(n, 'g', -1, 64)
	}
	panic("unsupported configuration value")
}

func (d document) values(service Service) map[string]string {
	values := map[string]string{
		"DATABASE_URL":                  d.Database.URL,
		"DB_SCHEMA":                     d.Database.Schema,
		"HTTP_ADDR":                     d.Cosmos.HTTPAddr,
		"PUBLIC_ORIGIN":                 d.Cosmos.PublicOrigin,
		"COOKIE_SECURE":                 value(d.Cosmos.CookieSecure),
		"TRUSTED_PROXIES":               strings.Join(d.Cosmos.TrustedProxies, ","),
		"PLANET_ENABLED":                value(d.Cosmos.PlanetEnabled),
		"PLANET_INTERNAL_URL":           d.Cosmos.PlanetInternalURL,
		"CHAT_ENABLED":                  value(d.Cosmos.ChatEnabled),
		"CHAT_INTERNAL_URL":             d.Cosmos.ChatInternalURL,
		"ADMIN_USERNAME":                d.Admin.Username,
		"ADMIN_PASSWORD":                d.Admin.Password,
		"PLANET_HTTP_ADDR":              d.Planet.HTTPAddr,
		"PLANET_ALLOWED_ORIGINS":        strings.Join(d.Planet.AllowedOrigins, ","),
		"PLANET_LOG_FILE":               d.Planet.LogFile,
		"PLANET_MOVE_SPEED":             value(d.Planet.MoveSpeed),
		"PLANET_PATH_BUDGET":            value(d.Planet.PathBudget),
		"PLANET_DB_TIMEOUT":             d.Planet.DBTimeout,
		"PLANET_LEASE_TTL":              d.Planet.LeaseTTL,
		"PLANET_LEASE_RENEW":            d.Planet.LeaseRenew,
		"PLANET_SHUTDOWN_TIMEOUT":       d.Planet.ShutdownTimeout,
		"PLANET_SCENE_CAPACITY":         value(d.Planet.SceneCapacity),
		"PLANET_SCENE_IDLE_TTL":         d.Planet.SceneIdleTTL,
		"PLANET_SCENE_LOAD_TIMEOUT":     d.Planet.SceneLoadTimeout,
		"CHAT_DATABASE_URL":             d.Chat.DatabaseURL,
		"CHAT_HTTP_ADDR":                d.Chat.HTTPAddr,
		"COSMIC_INTERNAL_URL":           d.Chat.CosmicInternalURL,
		"CHAT_ALLOWED_ORIGINS":          strings.Join(d.Chat.AllowedOrigins, ","),
		"CHAT_ERASURE_JOURNAL":          d.Chat.ErasureJournal,
		"CHAT_MAINTENANCE_DATABASE_URL": d.Chat.MaintenanceDatabaseURL,
		"CHAT_RUNTIME_ROLE":             d.Chat.RuntimeRole,
		"CHAT_DEFINER_ROLE":             d.Chat.DefinerRole,
		"CHAT_DISPATCHER_MODE":          d.Chat.DispatcherMode,
		"CHAT_FALLBACK_POLL_INTERVAL":   d.Chat.FallbackPollInterval,
		"CHAT_DEGRADED_POLL_INTERVAL":   d.Chat.DegradedPollInterval,
		"CHAT_OUTBOX_OVERLAP":           d.Chat.OutboxOverlap,
	}
	if service == Chat {
		values["PLANET_INTERNAL_URL"] = d.Chat.PlanetInternalURL
		if n := d.NATS; n != nil {
			values["NATS_ENABLED"] = value(n.Enabled)
			values["NATS_SERVICE_IDENTITY"] = n.ServiceIdentity
			values["NATS_SERVERS"] = strings.Join(n.Servers, ",")
			values["NATS_USERNAME"] = n.Username
			values["NATS_PASSWORD_FILE"] = n.PasswordFile
			values["NATS_CA_FILE"] = n.CAFile
			values["NATS_CONNECT_TIMEOUT"] = n.ConnectTimeout
			values["NATS_RECONNECT_WAIT"] = n.ReconnectWait
		}
	}
	return values
}
