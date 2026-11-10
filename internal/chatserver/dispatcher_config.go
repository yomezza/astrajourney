package chatserver

import (
	"errors"
	"fmt"
	"strings"
	"time"
)

type DispatcherConfig struct {
	Mode                                    string
	PollInterval, DegradedInterval, Overlap time.Duration
}

func DefaultDispatcherConfig() DispatcherConfig {
	return DispatcherConfig{"legacy", 10 * time.Second, 2 * time.Second, 60 * time.Second}
}
func dispatcherConfig(getenv func(string) string) (DispatcherConfig, error) {
	c := DefaultDispatcherConfig()
	if mode := getenv("CHAT_DISPATCHER_MODE"); mode != "" {
		c.Mode = strings.ToLower(mode)
	}
	for _, p := range []struct {
		name  string
		value *time.Duration
	}{
		{"CHAT_FALLBACK_POLL_INTERVAL", &c.PollInterval}, {"CHAT_DEGRADED_POLL_INTERVAL", &c.DegradedInterval}, {"CHAT_OUTBOX_OVERLAP", &c.Overlap},
	} {
		if v := getenv(p.name); v != "" {
			d, e := time.ParseDuration(v)
			if e != nil {
				return c, fmt.Errorf("invalid %s duration", p.name)
			}
			*p.value = d
		}
	}
	return c, c.Validate()
}
func (c DispatcherConfig) Validate() error {
	if c.Mode != "legacy" && c.Mode != "a1" && c.Mode != "a2" {
		return errors.New("chat.dispatcher_mode / CHAT_DISPATCHER_MODE must be legacy, a1 or a2; B1 is not implemented")
	}
	if c.PollInterval < time.Second || c.PollInterval > 30*time.Second || c.DegradedInterval < 500*time.Millisecond || c.DegradedInterval > 10*time.Second || c.DegradedInterval > c.PollInterval || c.Overlap < 60*time.Second || c.Overlap > 10*time.Minute {
		return errors.New("invalid Chat dispatcher intervals: poll 1s-30s, degraded 500ms-10s and <=poll, overlap 60s-10m")
	}
	return nil
}
