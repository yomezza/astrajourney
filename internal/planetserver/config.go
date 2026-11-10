// Package planetserver composes the independent planetary service.
package planetserver

import (
	"errors"
	"fmt"
	"github.com/yomezza/cosmosimulator/internal/platform/natscore"
	"math"
	"net"
	"net/url"
	"strconv"
	"strings"
	"time"
)

type Config struct {
	NATS             natscore.Config
	MoveSpeed        float64
	PathBudget       int
	DatabaseURL      string
	Schema           string
	Addr             string
	AllowedOrigins   []string
	DBTimeout        time.Duration
	LeaseTTL         time.Duration
	LeaseRenew       time.Duration
	ShutdownTimeout  time.Duration
	LogFile          string
	SceneCapacity    int
	SceneIdleTTL     time.Duration
	SceneLoadTimeout time.Duration
}

func DefaultConfig() Config {
	return Config{MoveSpeed: 4, PathBudget: 24000, Addr: "127.0.0.1:8081", DBTimeout: 2 * time.Second, LeaseTTL: 15 * time.Second, LeaseRenew: 5 * time.Second, ShutdownTimeout: 10 * time.Second, AllowedOrigins: []string{"http://127.0.0.1:8080", "http://localhost:8080"}, SceneCapacity: 32, SceneIdleTTL: 2 * time.Minute, SceneLoadTimeout: 20 * time.Second}
}

func LoadConfig(getenv func(string) string) (Config, error) {
	c := DefaultConfig()
	var err error
	c.NATS, err = natscore.LoadConfig(getenv)
	if err != nil {
		return c, err
	}
	if err = c.NATS.Validate("planet", false); err != nil {
		return c, err
	}
	if c.NATS.Enabled {
		if _, _, err = c.NATS.Credentials(); err != nil {
			return c, err
		}
	}
	c.DatabaseURL = getenv("DATABASE_URL")
	c.Schema = getenv("DB_SCHEMA")
	c.LogFile = getenv("PLANET_LOG_FILE")
	if v := getenv("PLANET_MOVE_SPEED"); v != "" {
		n, e := strconv.ParseFloat(v, 64)
		if e != nil {
			return c, errors.New("invalid PLANET_MOVE_SPEED")
		}
		c.MoveSpeed = n
	}
	if v := getenv("PLANET_PATH_BUDGET"); v != "" {
		n, e := strconv.Atoi(v)
		if e != nil {
			return c, errors.New("invalid PLANET_PATH_BUDGET")
		}
		c.PathBudget = n
	}
	if c.DatabaseURL == "" {
		return c, errors.New("configure database.url in TOML or DATABASE_URL for the planetary service")
	}
	if v := getenv("PLANET_HTTP_ADDR"); v != "" {
		c.Addr = v
	}
	if v := getenv("PLANET_SCENE_CAPACITY"); v != "" {
		n, err := strconv.Atoi(v)
		if err != nil {
			return c, errors.New("invalid PLANET_SCENE_CAPACITY")
		}
		c.SceneCapacity = n
	}
	for _, p := range []struct {
		name   string
		target *time.Duration
	}{
		{"PLANET_DB_TIMEOUT", &c.DBTimeout}, {"PLANET_LEASE_TTL", &c.LeaseTTL}, {"PLANET_LEASE_RENEW", &c.LeaseRenew}, {"PLANET_SHUTDOWN_TIMEOUT", &c.ShutdownTimeout},
		{"PLANET_SCENE_IDLE_TTL", &c.SceneIdleTTL}, {"PLANET_SCENE_LOAD_TIMEOUT", &c.SceneLoadTimeout},
	} {
		if v := getenv(p.name); v != "" {
			d, err := time.ParseDuration(v)
			if err != nil {
				return c, fmt.Errorf("invalid %s duration", p.name)
			}
			*p.target = d
		}
	}
	origins := getenv("PLANET_ALLOWED_ORIGINS")
	if origins == "" {
		origins = getenv("PUBLIC_ORIGIN")
	}
	if origins != "" {
		c.AllowedOrigins = strings.Split(origins, ",")
	}
	for i, v := range c.AllowedOrigins {
		c.AllowedOrigins[i] = strings.TrimRight(strings.TrimSpace(v), "/")
	}
	return c, c.Validate()
}

func (c Config) Validate() error {
	if e := c.NATS.Validate("planet", false); e != nil {
		return e
	}
	if math.IsNaN(c.MoveSpeed) || math.IsInf(c.MoveSpeed, 0) || c.MoveSpeed < 1 || c.MoveSpeed > 12 || c.PathBudget < 1000 || c.PathBudget > 65536 {
		return errors.New("movement speed must be 1-12 and path budget 1000-65536")
	}
	_, port, err := net.SplitHostPort(c.Addr)
	n, e := strconv.Atoi(port)
	if err != nil || e != nil || n < 1 || n > 65535 {
		return errors.New("PLANET_HTTP_ADDR must be host:port (1-65535)")
	}
	if c.DBTimeout < 100*time.Millisecond || c.DBTimeout > 30*time.Second {
		return errors.New("PLANET_DB_TIMEOUT must be 100ms-30s")
	}
	if c.LeaseTTL < 3*time.Second || c.LeaseTTL > 5*time.Minute || c.LeaseRenew < time.Second || c.LeaseRenew > c.LeaseTTL/3 || c.DBTimeout >= c.LeaseRenew {
		return errors.New("lease requires TTL 3s-5m, renewal >=1s and <=TTL/3, DB timeout < renewal")
	}
	if c.ShutdownTimeout < time.Second || c.ShutdownTimeout > time.Minute {
		return errors.New("PLANET_SHUTDOWN_TIMEOUT must be 1s-1m")
	}
	if c.SceneCapacity < 1 || c.SceneCapacity > 1024 || c.SceneIdleTTL < time.Second || c.SceneIdleTTL > time.Hour || c.SceneLoadTimeout < time.Second || c.SceneLoadTimeout > time.Minute {
		return errors.New("scene capacity must be 1-1024, idle TTL 1s-1h and load timeout 1s-1m")
	}
	if len(c.AllowedOrigins) == 0 {
		return errors.New("at least one planetary Origin is required")
	}
	for _, s := range c.AllowedOrigins {
		u, err := url.Parse(s)
		if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" || u.User != nil || u.Path != "" || u.RawQuery != "" || u.Fragment != "" || strings.Contains(u.Host, "*") {
			return errors.New("planetary Origins must be explicit http(s) origins without paths or wildcards")
		}
	}
	return nil
}
