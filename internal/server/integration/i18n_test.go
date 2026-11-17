package integration

import (
	"encoding/json"
	"io/fs"
	"testing"
)

func TestTranslationCatalogs(t *testing.T) {
	catalogs := map[string]map[string]string{}
	for _, lang := range []string{"ru", "en"} {
		data, err := fs.ReadFile(browserAssets, lang+".json")
		if err != nil {
			t.Fatal(err)
		}
		var catalog map[string]string
		if err = json.Unmarshal(data, &catalog); err != nil {
			t.Fatal(err)
		}
		catalogs[lang] = catalog
	}
	if len(catalogs["ru"]) != len(catalogs["en"]) {
		t.Fatal("translation catalogs differ")
	}
	for key, ru := range catalogs["ru"] {
		if ru == "" || catalogs["en"][key] == "" {
			t.Errorf("missing translation: %s", key)
		}
	}
}
