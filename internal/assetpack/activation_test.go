package assetpack

import (
	"encoding/json"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
)

func TestNewGraphicsActivateIndependentlyAndRollbackSurvivesRestart(t *testing.T) {
	source, out, storage := t.TempDir(), t.TempDir(), t.TempDir()
	os.WriteFile(filepath.Join(source, "aliases.json"), []byte(`{"one.svg":["/one.svg"],"two.svg":["/two.svg"]}`), 0600)
	os.WriteFile(filepath.Join(source, "one.svg"), []byte(`<svg id="one"/>`), 0600)
	os.WriteFile(filepath.Join(source, "two.svg"), []byte(`<svg id="two"/>`), 0600)
	first, _, err := Build(source, out, "v0.29.0")
	if err != nil {
		t.Fatal(err)
	}
	s, err := Open(storage)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = s.Install(filepath.Join(out, first.Archive)); err != nil {
		t.Fatal(err)
	}
	read := func() map[string]any {
		t.Helper()
		w := httptest.NewRecorder()
		s.ServeMap(w, httptest.NewRequest("GET", "/cdn/api/v1/asset-map", nil))
		if w.Code != 200 {
			t.Fatal(w.Code)
		}
		var data map[string]any
		if err := json.Unmarshal(w.Body.Bytes(), &data); err != nil {
			t.Fatal(err)
		}
		return data
	}
	before := read()
	os.WriteFile(filepath.Join(source, "one.svg"), []byte(`<svg id="updated-one"/>`), 0600)
	second, _, err := Build(source, out, "v0.30.0")
	if err != nil {
		t.Fatal(err)
	}
	if _, err = s.Install(filepath.Join(out, second.Archive)); err != nil {
		t.Fatal(err)
	}
	after := read()
	b, a := before["urls"].(map[string]any), after["urls"].(map[string]any)
	if b["/one.svg"] == a["/one.svg"] || b["/two.svg"] != a["/two.svg"] {
		t.Fatal("cache invalidation spread to unchanged file")
	}
	s.Close()
	s, err = Open(storage)
	if err != nil {
		t.Fatal(err)
	}
	if read()["revision"] != second.Revision {
		t.Fatal("active map changed at restart")
	}
	if _, err = s.Install(filepath.Join(out, first.Archive)); err != nil {
		t.Fatal(err)
	}
	s.Close()
	s, err = Open(storage)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	if read()["revision"] != first.Revision {
		t.Fatal("rollback lost at restart")
	}
	if !s.Has(second.Revision) {
		t.Fatal("rollback removed newer immutable URLs")
	}
}
