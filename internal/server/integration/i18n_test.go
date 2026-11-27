package integration

import (
	"encoding/json"
	"io/fs"
	"testing"
)

func TestRussianTranslationCatalog(t *testing.T) {
	data, err := fs.ReadFile(browserAssets, "ru.json")
	if err != nil {
		t.Fatal(err)
	}
	var catalog map[string]string
	if err := json.Unmarshal(data, &catalog); err != nil {
		t.Fatal(err)
	}
	for key, value := range catalog {
		if value == "" {
			t.Errorf("empty Russian translation: %s", key)
		}
	}
	for _, key := range []string{"Идеи сообщества", "Мои идеи", "Идей: {total}"} {
		if catalog[key] == "" {
			t.Errorf("missing Russian translation: %s", key)
		}
	}
}
