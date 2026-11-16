package dblayout

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

func TestNamesAndPaths(t *testing.T) {
	seen := map[string]string{}
	for _, root := range []string{"game", "cosmosimulator", `schema"; DROP TABLE users; --`, strings.Repeat("я", 31), strings.Repeat("x", 63), "Game", "a,b"} {
		prefix := Prefix(root)
		if previous := seen[prefix]; previous != "" && previous != root {
			t.Fatal("prefix collision")
		}
		seen[prefix] = root
		if Prefix(root) != prefix {
			t.Fatal("unstable prefix")
		}
		for _, name := range Names(root, true)[1:] {
			if len(name) > 63 || !regexp.MustCompile(`^[a-z][a-z0-9_]*$`).MatchString(name) {
				t.Fatal("unsafe identifier", name)
			}
		}
		recovered, e := RootFromPath(SearchPath(root))
		if e != nil || recovered != root {
			t.Fatal("root roundtrip", root, e)
		}
		if strings.Contains(SearchPath(root), Schema(root, "archive")) {
			t.Fatal("archive in runtime path")
		}
		if !strings.HasPrefix(FunctionPath(root), "pg_catalog,") || !strings.HasSuffix(FunctionPath(root), ",pg_temp") {
			t.Fatal("unsafe function path")
		}
	}
	if Schema("game", "universe") != "game_universe" {
		t.Fatal("unexpected conventional name")
	}
}

func TestAllMigrationObjectsHaveDomains(t *testing.T) {
	files, e := filepath.Glob("../../server/schema*.sql")
	if e != nil {
		t.Fatal(e)
	}
	files = append(files, "../../universestore/schema.sql")
	total := 0
	for _, file := range files {
		raw, e := os.ReadFile(file)
		if e != nil {
			t.Fatal(e)
		}
		for _, match := range creates.FindAllStringSubmatch(string(raw), -1) {
			total++
			if Domain(match[2]) == "" {
				t.Errorf("unmapped creation %s in %s", match[2], file)
			}
		}
		routed := DDL("game", string(raw))
		for _, match := range creates.FindAllStringSubmatch(routed, -1) {
			if !strings.EqualFold(match[2], "if") {
				t.Errorf("unqualified creation %s in %s", match[2], file)
			}
		}
	}
	if total < 100 {
		t.Fatal("migration inventory incomplete", total)
	}
}

func TestDDLQualifiesDynamicViewAndFunctionPath(t *testing.T) {
	sql := `DO $$ BEGIN EXECUTE 'CREATE VIEW planet_session_lookup AS SELECT 1'; END $$;
CREATE OR REPLACE FUNCTION planet_reserved_ship() RETURNS trigger LANGUAGE plpgsql AS $$ BEGIN RETURN NEW; END $$;`
	routed := DDL(`odd'root`, sql)
	if !strings.Contains(routed, Qualified(`odd'root`, "planet_session_lookup")) || !strings.Contains(routed, "SET search_path="+FunctionPath(`odd'root`)) {
		t.Fatal("incomplete routing", routed)
	}
	if strings.Contains(routed, "odd'root") {
		t.Fatal("unsafe root interpolated in SQL literal")
	}
}
