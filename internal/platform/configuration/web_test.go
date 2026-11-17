package configuration_test

import (
	"github.com/yomezza/cosmosimulator/internal/platform/configuration"
	"path/filepath"
	"testing"
)

func TestWebHasNoDatabaseAndUsesOwnNATSProfile(t *testing.T) {
	p := file(t, `[web]
storage_root='data/web'
cdn_internal_url='http://127.0.0.1:8084'
[nats]
enabled=true
servers=['nats://127.0.0.1:4222']
[nats.web]
username='web'
password='web-private'
[nats.cdn]
username='cdn'
password='cdn-private'
`)
	s, e := configuration.Load(lookup(map[string]string{configuration.FileVariable: p}), configuration.Web)
	if e != nil {
		t.Fatal(e)
	}
	if s.Getenv("DATABASE_URL") != "" || s.Getenv("WEB_STORAGE_ROOT") != filepath.Join(filepath.Dir(p), "data", "web") || s.Getenv("NATS_USERNAME") != "web" || s.Getenv("NATS_PASSWORD") != "web-private" || s.Getenv("NATS_SERVICE_IDENTITY") != "web" {
		t.Fatal("invalid Web configuration/profile")
	}
}
