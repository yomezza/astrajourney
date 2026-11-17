package configuration_test

import (
	"github.com/yomezza/cosmosimulator/internal/platform/configuration"
	"path/filepath"
	"testing"
)

func TestCDNNeedsNoDatabaseAndSelectsOwnNATSProfile(t *testing.T) {
	path := file(t, `[cdn]
storage_root='data/cdn'
sync_enabled=true
[nats]
enabled=true
servers=['nats://127.0.0.1:4222']
[nats.cosmos]
username='cosmos'
password='cosmos-private'
[nats.cdn]
username='cdn'
password='cdn-private'
`)
	source, err := configuration.Load(lookup(map[string]string{configuration.FileVariable: path}), configuration.CDN)
	if err != nil {
		t.Fatal(err)
	}
	if source.Getenv("DATABASE_URL") != "" || source.Getenv("NATS_USERNAME") != "cdn" || source.Getenv("NATS_PASSWORD") != "cdn-private" || source.Getenv("CDN_STORAGE_ROOT") != filepath.Join(filepath.Dir(path), "data", "cdn") {
		t.Fatal("wrong CDN configuration/profile")
	}
}
