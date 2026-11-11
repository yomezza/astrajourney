package configuration_test

import (
	"github.com/yomezza/cosmosimulator/internal/testkit/natsfixture"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/yomezza/cosmosimulator/internal/chatserver"
	"github.com/yomezza/cosmosimulator/internal/planetserver"
	"github.com/yomezza/cosmosimulator/internal/platform/configuration"
	"github.com/yomezza/cosmosimulator/internal/platform/natscore"
)

func file(t *testing.T, text string) string {
	t.Helper()
	p := filepath.Join(t.TempDir(), "config.toml")
	if err := os.WriteFile(p, []byte(text), 0600); err != nil {
		t.Fatal(err)
	}
	return p
}

func lookup(values map[string]string) func(string) string {
	return func(name string) string { return values[name] }
}

func TestSourceSelection(t *testing.T) {
	env := map[string]string{"DATABASE_URL": "environment-db", "CHAT_ENABLED": "true"}
	source, err := configuration.Load(lookup(env), configuration.Cosmos)
	if err != nil || source.Getenv("DATABASE_URL") != "environment-db" {
		t.Fatal("environment mode", err)
	}
	env[configuration.FileVariable] = file(t, "\ufeff"+`[database]
url = 'file-db'
[cosmos]
chat_enabled = false
`)
	source, err = configuration.Load(lookup(env), configuration.Cosmos)
	if err != nil {
		t.Fatal(err)
	}
	if source.Getenv("DATABASE_URL") != "file-db" || source.Getenv("CHAT_ENABLED") != "false" || source.Getenv("HTTP_ADDR") != "" {
		t.Fatal("file must be the exclusive source")
	}
	// An omitted secret must not be inherited from the service account environment.
	env["ADMIN_PASSWORD"] = "stale-secret"
	if source.Getenv("ADMIN_PASSWORD") != "" {
		t.Fatal("environment secret leaked into file mode")
	}
	if err := os.WriteFile(env[configuration.FileVariable], []byte("not valid TOML"), 0600); err != nil {
		t.Fatal(err)
	}
	if source.Getenv("DATABASE_URL") != "file-db" {
		t.Fatal("configuration changed without restart")
	}
}

func TestInvalidFilesNeverFallBackOrLeakSecrets(t *testing.T) {
	for _, contents := range []string{
		"[database\nurl = 'secret-password'",
		"[database]\nurl = 123 # secret-password",
		"[database]\nurl = 'secret-password'\n[cosmos]\nchat_enabld = true",
		"[cosmos]\nchat_enabled = 'secret-password'",
		"[planet]\nallowed_origins = ['http://one,http://two']",
	} {
		source, err := configuration.Load(lookup(map[string]string{configuration.FileVariable: file(t, contents), "DATABASE_URL": "valid-environment-db"}), configuration.Cosmos)
		if err == nil || source != nil {
			t.Fatal("bad config silently accepted")
		}
		if strings.Contains(err.Error(), "secret-password") {
			t.Fatal("configuration error leaks source contents")
		}
	}
	if _, err := configuration.Load(lookup(map[string]string{configuration.FileVariable: filepath.Join(t.TempDir(), "missing.toml"), "DATABASE_URL": "valid-environment-db"}), configuration.Chat); err == nil {
		t.Fatal("missing file selected environment")
	}
}

func TestServiceSectionsAndExistingValidators(t *testing.T) {
	p := file(t, `[database]
url = 'postgres://test:password@127.0.0.1/game'
schema = 'cosmosimulator'
[cosmos]
planet_internal_url = 'http://cosmic-to-planet:8081'
trusted_proxies = ['127.0.0.1/32', '::1/128']
[planet]
allowed_origins = ['http://game:8080', 'http://other:8080']
move_speed = 5.5
path_budget = 12000
db_timeout = '1s'
log_file = 'logs/planet.log'
[chat]
database_url = 'postgres://chat:password@127.0.0.1/game'
cosmic_internal_url = 'http://game:8080'
planet_internal_url = 'http://chat-to-planet:8081'
allowed_origins = ['http://game:8080']
erasure_journal = 'data/erasure.log'
`)
	env := lookup(map[string]string{configuration.FileVariable: p})
	cosmic, err := configuration.Load(env, configuration.Cosmos)
	if err != nil {
		t.Fatal(err)
	}
	if cosmic.Getenv("PLANET_INTERNAL_URL") != "http://cosmic-to-planet:8081" || cosmic.Getenv("TRUSTED_PROXIES") != "127.0.0.1/32,::1/128" {
		t.Fatal("cosmic section")
	}
	planet, err := configuration.Load(env, configuration.Planet)
	if err != nil {
		t.Fatal(err)
	}
	pc, err := planetserver.LoadConfig(planet.Getenv)
	if err != nil {
		t.Fatal(err)
	}
	if pc.MoveSpeed != 5.5 || pc.PathBudget != 12000 || len(pc.AllowedOrigins) != 2 || pc.LogFile != filepath.Join(filepath.Dir(p), "logs", "planet.log") {
		t.Fatal("planet config or relative path")
	}
	chat, err := configuration.Load(env, configuration.Chat)
	if err != nil {
		t.Fatal(err)
	}
	cc, err := chatserver.LoadConfig(chat.Getenv)
	if err != nil {
		t.Fatal(err)
	}
	if cc.DatabaseURL != "postgres://chat:password@127.0.0.1/game" || cc.PlanetURL != "http://chat-to-planet:8081" || cc.Journal != filepath.Join(filepath.Dir(p), "data", "erasure.log") {
		t.Fatal("chat section or relative path")
	}
	zero, err := configuration.Load(lookup(map[string]string{configuration.FileVariable: file(t, "[database]\nurl = 'postgres://test/db'\n[planet]\nscene_capacity = 0")}), configuration.Planet)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = planetserver.LoadConfig(zero.Getenv); err == nil {
		t.Fatal("explicit invalid zero replaced by a default")
	}
}

func TestExamples(t *testing.T) {
	for _, service := range []configuration.Service{configuration.Cosmos, configuration.Planet, configuration.Chat} {
		source, err := configuration.Load(lookup(map[string]string{configuration.FileVariable: filepath.Join("..", "..", "..", "configs", string(service)+".example.toml")}), service)
		if err != nil {
			t.Fatal(service, err)
		}
		if source.Getenv("DB_SCHEMA") != "cosmosimulator" {
			t.Fatal("example missing shared schema")
		}
		switch service {
		case configuration.Planet:
			if _, err = planetserver.LoadConfig(source.Getenv); err != nil {
				t.Fatal(err)
			}
		case configuration.Chat:
			if _, err = chatserver.LoadConfig(source.Getenv); err != nil {
				t.Fatal(err)
			}
		}
	}
}

func TestChatDispatcherTOMLRemainsExclusive(t *testing.T) {
	p := file(t, `[database]
url='postgres://test:password@127.0.0.1/game'
[chat]
erasure_journal='erasure.log'
dispatcher_mode='a2'
fallback_poll_interval='10s'
degraded_poll_interval='2s'
outbox_overlap='60s'
`)
	source, e := configuration.Load(lookup(map[string]string{configuration.FileVariable: p, "CHAT_DISPATCHER_MODE": "legacy", "CHAT_OUTBOX_OVERLAP": "bad"}), configuration.Chat)
	if e != nil {
		t.Fatal(e)
	}
	c, e := chatserver.LoadConfig(source.Getenv)
	if e != nil || c.Dispatcher.Mode != "a2" {
		t.Fatal("dispatcher mapping or environment merge", e)
	}
	if _, e = configuration.Load(lookup(map[string]string{configuration.FileVariable: file(t, "[nats]\nenabled=false\nunknown_key=true")}), configuration.Chat); e == nil {
		t.Fatal("N3 unexpectedly accepts unknown NATS settings")
	}
}

func TestNATSServiceIsolationAndRelativePaths(t *testing.T) {
	p := file(t, `[database]
url='postgres://test:password@127.0.0.1/game'
[chat]
erasure_journal='erasure.log'
dispatcher_mode='a1'
[nats]
enabled=false
service_identity='chat'
servers=['tls://127.0.0.1:14222']
username='chat'
password_file='secrets/chat.password'
ca_file='certs/ca.pem'
connect_timeout='2s'
reconnect_wait='1s'
`)
	source, e := configuration.Load(lookup(map[string]string{configuration.FileVariable: p, "NATS_ENABLED": "true", "NATS_PASSWORD_FILE": "environment-secret"}), configuration.Chat)
	if e != nil {
		t.Fatal(e)
	}
	if source.Getenv("NATS_ENABLED") != "false" || source.Getenv("NATS_PASSWORD_FILE") != filepath.Join(filepath.Dir(p), "secrets", "chat.password") {
		t.Fatal("NATS paths/source isolation")
	}
	if _, e = chatserver.LoadConfig(source.Getenv); e != nil {
		t.Fatal("disabled NATS read secrets", e)
	}
	if _, e = configuration.Load(lookup(map[string]string{configuration.FileVariable: p}), configuration.Cosmos); e != nil {
		t.Fatal("disabled N6 section", e)
	}
	if _, e = configuration.Load(lookup(map[string]string{configuration.FileVariable: file(t, "[nats]\nenabled=false\nservers=['tls://one:4222,tls://two:4222']")}), configuration.Chat); e == nil {
		t.Fatal("ambiguous NATS array accepted")
	}
}

func TestPlanetNATSExclusiveConfig(t *testing.T) {
	m := natsfixture.PKI(t, false)
	p := file(t, `[database]
url='postgres://test:password@127.0.0.1/game'
[planet]
http_addr='127.0.0.1:18081'
[nats]
enabled=true
service_identity='planet'
username='planet'
servers=['tls://127.0.0.1:14222']
password_file=`+strconv.Quote(m.PasswordFile)+`
ca_file=`+strconv.Quote(m.CAFile)+`
`)
	src, e := configuration.Load(lookup(map[string]string{configuration.FileVariable: p, "NATS_USERNAME": "chat", "NATS_ENABLED": "false"}), configuration.Planet)
	if e != nil {
		t.Fatal(e)
	}
	cfg, e := planetserver.LoadConfig(src.Getenv)
	if e != nil || cfg.NATS.Username != "planet" || !cfg.NATS.Enabled {
		t.Fatal("Planet NATS mapping/source isolation", e)
	}
	if _, e = chatserver.LoadConfig(src.Getenv); e == nil {
		t.Fatal("Chat accepted Planet credentials")
	}
	cosmic, e := configuration.Load(lookup(map[string]string{configuration.FileVariable: p}), configuration.Cosmos)
	if e != nil {
		t.Fatal(e)
	}
	nc, e := natscore.LoadConfig(cosmic.Getenv)
	if e != nil || nc.Validate("cosmos", false) == nil {
		t.Fatal("Cosmic accepted Planet identity")
	}

}

func TestCosmicNATSExclusiveConfig(t *testing.T) {
	m := natsfixture.PKI(t, false)
	p := file(t, `[nats]
enabled=true
service_identity='cosmos'
username='cosmos'
servers=['tls://127.0.0.1:14222']
password_file=`+strconv.Quote(m.PasswordFile)+`
ca_file=`+strconv.Quote(m.CAFile)+`
`)
	source, e := configuration.Load(lookup(map[string]string{configuration.FileVariable: p, "NATS_USERNAME": "chat", "NATS_ENABLED": "false"}), configuration.Cosmos)
	if e != nil {
		t.Fatal(e)
	}
	c, e := natscore.LoadConfig(source.Getenv)
	if e != nil || c.Validate("cosmos", false) != nil || !c.Enabled || c.Username != "cosmos" {
		t.Fatal("Cosmic exclusive mapping", e)
	}
	if c.Validate("planet", false) == nil || c.Validate("chat", false) == nil {
		t.Fatal("cross-service identity")
	}
	if _, _, e = c.Credentials(); e != nil {
		t.Fatal(e)
	}
}

func TestSharedNATSPlainProfiles(t *testing.T) {
	text := `[nats]
enabled=true
servers=['nats://127.0.0.1:14222']
connect_timeout='1s'
[nats.cosmos]
username='cosmos'
password='cosmos-test-only'
[nats.planet]
username='planet'
password='planet-test-only'
[nats.chat]
username='chat'
password='chat-test-only'
`
	p := file(t, text)
	for _, service := range []configuration.Service{configuration.Cosmos, configuration.Planet, configuration.Chat} {
		source, e := configuration.Load(lookup(map[string]string{configuration.FileVariable: p, "NATS_PASSWORD": "environment-secret", "NATS_SERVERS": "tls://wrong:4222"}), service)
		if e != nil {
			t.Fatal(e)
		}
		c, e := natscore.LoadConfig(source.Getenv)
		if e != nil || c.Validate(string(service), false) != nil {
			t.Fatal("selected profile", service, e)
		}
		if c.Password != string(service)+"-test-only" || c.ServiceIdentity != string(service) || c.UsesTLS() {
			t.Fatal("profile/ENV contamination")
		}
		tls, _, e := c.Credentials()
		if e != nil || tls != nil {
			t.Fatal("plain required files", e)
		}
	}
	for _, bad := range []string{
		text + "\nmisspelled_password='do-not-print-this'",
		"[nats]\nenabled=true\nusername='cosmos'\npassword='do-not-print-this'\n[nats.chat]\nusername='chat'",
		"[nats]\nenabled=true\n[nats.chat]\nusername='chat'",
	} {
		_, e := configuration.Load(lookup(map[string]string{configuration.FileVariable: file(t, bad)}), configuration.Cosmos)
		if e == nil || strings.Contains(e.Error(), "do-not-print-this") {
			t.Fatal("invalid common config accepted or leaked")
		}
	}
}
