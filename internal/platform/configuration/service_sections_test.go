package configuration_test

import (
	"strings"
	"testing"

	"github.com/yomezza/cosmosimulator/internal/platform/configuration"
)

// Shared CONFIG_FILE is valid across staggered service releases. A newly
// introduced field of one service must not prevent a different service from
// starting; unknown fields in the ACTIVE section and in shared sections must
// still be rejected rather than silently ignored.
func TestSharedTOMLVersionSkewIsolation(t *testing.T) {
	cases := []struct {
		name      string
		toml      string
		accepted  []configuration.Service
		rejected  []configuration.Service
	}{
		{
			name:     "future Cosmic setting",
			toml:     "[cosmos]\nguide_media_dir='/var/lib/astrajourney/media'\nfuture_cosmic_setting='new'\n",
			accepted: []configuration.Service{configuration.Planet, configuration.Chat},
			rejected: []configuration.Service{configuration.Cosmos},
		},
		{
			name:     "future Planet setting",
			toml:     "[planet]\nfuture_planet_setting='new'\n",
			accepted: []configuration.Service{configuration.Cosmos, configuration.Chat},
			rejected: []configuration.Service{configuration.Planet},
		},
		{
			name:     "future Chat setting",
			toml:     "[chat]\nfuture_chat_setting='new'\n",
			accepted: []configuration.Service{configuration.Cosmos, configuration.Planet},
			rejected: []configuration.Service{configuration.Chat},
		},
		{
			name:     "future admin setting",
			toml:     "[admin]\nfuture_admin_setting='new'\n",
			accepted: []configuration.Service{configuration.Planet, configuration.Chat},
			rejected: []configuration.Service{configuration.Cosmos},
		},
		{
			name:     "future NATS Cosmic profile setting",
			toml:     "[nats.cosmos]\nfuture_cosmos_auth='new'\n",
			accepted: []configuration.Service{configuration.Planet, configuration.Chat},
			rejected: []configuration.Service{configuration.Cosmos},
		},
		{
			name:     "future NATS Planet profile setting",
			toml:     "[nats.planet]\nfuture_planet_auth='new'\n",
			accepted: []configuration.Service{configuration.Cosmos, configuration.Chat},
			rejected: []configuration.Service{configuration.Planet},
		},
		{
			name:     "future NATS Chat profile setting",
			toml:     "[nats.chat]\nfuture_chat_auth='new'\n",
			accepted: []configuration.Service{configuration.Cosmos, configuration.Planet},
			rejected: []configuration.Service{configuration.Chat},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			env := lookup(map[string]string{configuration.FileVariable: file(t, "[database]\nurl='postgres://example/db'\n"+tc.toml)})
			for _, service := range tc.accepted {
				src, err := configuration.Load(env, service)
				if err != nil || src == nil {
					t.Fatalf("unrelated service %s rejected the shared config: %v", service, err)
				}
				if got := src.Getenv("DATABASE_URL"); got != "postgres://example/db" {
					t.Fatalf("shared database settings lost for %s: %q", service, got)
				}
			}
			for _, service := range tc.rejected {
				src, err := configuration.Load(env, service)
				if src != nil || err == nil || !strings.Contains(err.Error(), "unknown TOML configuration field") {
					t.Fatalf("active service %s accepted its unknown setting: %v", service, err)
				}
			}
		})
	}
}

func TestSharedTOMLUnknownGlobalSettingsRemainStrict(t *testing.T) {
	for _, extra := range []string{
		"[database]\nunknown_shared_key='secret-value'\n",
		"[nats]\nunknown_shared_key='secret-value'\n",
		"[made_up_section]\nkey='secret-value'\n",
	} {
		for _, service := range []configuration.Service{configuration.Cosmos, configuration.Planet, configuration.Chat} {
			src, err := configuration.Load(lookup(map[string]string{configuration.FileVariable: file(t, extra)}), service)
			if src != nil || err == nil || !strings.Contains(err.Error(), "unknown TOML configuration field") {
				t.Fatalf("unknown shared settings accepted by %s: %v", service, err)
			}
			if strings.Contains(err.Error(), "secret-value") {
				t.Fatal("unknown field error disclosed a TOML value")
			}
		}
	}
}
