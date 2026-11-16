package postgres

import (
	"strings"
	"testing"

	"github.com/yomezza/cosmosimulator/internal/platform/dblayout"
)

func TestDatabaseConfig(t *testing.T) {
	for _, schema := range []string{"", "public", "cosmosimulator", "My Schema", `schema"; DROP TABLE users; --`, "a,b"} {
		config, name, err := Config("postgres://localhost/example?search_path=public", schema)
		if err != nil {
			t.Fatal(err)
		}
		if schema == "" {
			schema = "public"
		}
		if name != schema || config.ConnConfig.RuntimeParams["search_path"] != dblayout.SearchPath(schema) {
			t.Fatalf("schema was not treated as one quoted identifier: %q", schema)
		}
	}
	for _, schema := range []string{strings.Repeat("x", 64), strings.Repeat("я", 32), "a\x00b", "$user", "pg_catalog", "pg_temp", "PG_other", "information_schema", string([]byte{255})} {
		if _, _, err := Config("postgres://localhost/example", schema); err == nil {
			t.Errorf("accepted invalid schema: %q", schema)
		}
	}
}
