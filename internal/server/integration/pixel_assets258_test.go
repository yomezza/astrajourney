package integration

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io/fs"
	"testing"

	"github.com/yomezza/cosmosimulator/internal/planet/building"
	"github.com/yomezza/cosmosimulator/internal/server"
)

func TestPixelPackOriginalFilesAndNoLegacyGraphics258(t *testing.T) {
	var manifest struct {
		Files   map[string]string `json:"files"`
		Mapping map[string]string `json:"mapping"`
	}
	raw, err := fs.ReadFile(graphicAssets, "planet/pixelart-v1/manifest.json")
	if err != nil {
		t.Fatal(err)
	}
	if err = json.Unmarshal(raw, &manifest); err != nil {
		t.Fatal(err)
	}
	if len(manifest.Files) != 246 {
		t.Fatalf("expected 246 original archive files, got %d", len(manifest.Files))
	}
	for name, digest := range manifest.Files {
		data, e := fs.ReadFile(graphicAssets, "planet/pixelart-v1/"+name)
		if e != nil {
			t.Fatal(e)
		}
		hash := sha256.Sum256(data)
		if hex.EncodeToString(hash[:]) != digest {
			t.Errorf("original pack file changed: %s", name)
		}
	}
	for _, definition := range building.Catalog {
		if _, ok := manifest.Files[definition.Asset]; !ok {
			t.Errorf("missing original building image: %s", definition.Asset)
		}
	}
	for key, name := range manifest.Mapping {
		if _, ok := manifest.Files[name]; !ok {
			t.Errorf("missing new art for %s: %s", key, name)
		}
	}
	for _, name := range []string{"web/planet-art", "web/planet-landing-core.png"} {
		if _, e := fs.Stat(server.Assets, name); !errors.Is(e, fs.ErrNotExist) {
			t.Errorf("legacy graphics remain embedded: %s", name)
		}
	}
	if old, e := fs.Glob(server.Assets, "web/surface-ground-*.png"); e != nil || len(old) > 0 {
		t.Fatalf("legacy grounds remain: %v %v", old, e)
	}
}
