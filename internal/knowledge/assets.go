package knowledge

import (
	"crypto/sha256"
	_ "embed"
	"encoding/hex"
	"encoding/json"
	"io/fs"
	"regexp"
	"sort"
	"strings"

	"github.com/yomezza/cosmosimulator/internal/fleet"
)

//go:embed builtin-assets.json
var builtinManifest []byte

type AssetManifest struct {
	Version int     `json:"version"`
	Items   []Asset `json:"items"`
}

// CompileAssets is a build utility over source-controlled files, not uploads.
// The checked-in manifest is verified before Cosmic release compilation.
func CompileAssets(files fs.FS) ([]byte, error) {
	paths := map[string]string{}
	entries, err := fs.ReadDir(files, ".")
	if err != nil {
		return nil, err
	}
	for _, e := range entries {
		if regexp.MustCompile(`^object-[a-z-]+\.svg$`).MatchString(e.Name()) {
			paths["object/"+strings.TrimSuffix(strings.TrimPrefix(e.Name(), "object-"), ".svg")] = e.Name()
		}
	}
	for _, h := range fleet.Catalogue {
		paths["ship/"+h.ID] = h.Model
	}
	keys := []string{}
	for key := range paths {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	manifest := AssetManifest{Version: 1, Items: []Asset{}}
	for _, key := range keys {
		body, e := fs.ReadFile(files, paths[key])
		if e != nil {
			return nil, e
		}
		sum := sha256.Sum256(body)
		manifest.Items = append(manifest.Items, Asset{key, "/" + paths[key], hex.EncodeToString(sum[:]), true})
	}
	raw, err := json.MarshalIndent(manifest, "", "  ")
	return append(raw, '\n'), err
}
func verifiedAssets(files fs.FS) (map[string]Asset, string) {
	assets := map[string]Asset{}
	digest := sha256.Sum256(builtinManifest)
	var manifest AssetManifest
	if json.Unmarshal(builtinManifest, &manifest) != nil || manifest.Version != 1 {
		return assets, hex.EncodeToString(digest[:])
	}
	for _, item := range manifest.Items {
		path := strings.TrimPrefix(item.Path, "/")
		if !regexp.MustCompile(`^[a-z][a-z0-9-]*\.svg$`).MatchString(path) {
			continue
		}
		raw, err := fs.ReadFile(files, path)
		if err != nil {
			continue
		}
		sum := sha256.Sum256(raw)
		if hex.EncodeToString(sum[:]) == item.SHA {
			assets[item.Key] = item
		}
	}
	return assets, hex.EncodeToString(digest[:])
}
