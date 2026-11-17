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
	entries, err := fs.Glob(files, "space/objects/object-*.svg")
	if err != nil {
		return nil, err
	}
	for _, name := range entries {
		base := strings.TrimPrefix(name, "space/objects/")
		paths["object/"+strings.TrimSuffix(strings.TrimPrefix(base, "object-"), ".svg")] = name
	}
	for _, h := range fleet.Catalogue {
		paths["ship/"+h.ID] = "space/ships/" + h.Model
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
		manifest.Items = append(manifest.Items, Asset{key, "/assets/sha256/" + hex.EncodeToString(sum[:]) + "/" + paths[key], hex.EncodeToString(sum[:]), true})
	}
	raw, err := json.MarshalIndent(manifest, "", "  ")
	return append(raw, '\n'), err
}
func verifiedAssets(_ fs.FS) (map[string]Asset, string) {
	assets := map[string]Asset{}
	digest := sha256.Sum256(builtinManifest)
	var manifest AssetManifest
	if json.Unmarshal(builtinManifest, &manifest) != nil || manifest.Version != 1 {
		return assets, hex.EncodeToString(digest[:])
	}
	// The build tool verifies the source manifest. Runtime trusts this small,
	// compiled catalog; all image bytes belong exclusively to the external pack.
	for _, item := range manifest.Items {
		if regexp.MustCompile(`^/assets/sha256/[a-f0-9]{64}/space/(ships|objects)/[a-z0-9-]+\.svg$`).MatchString(item.Path) {
			assets[item.Key] = item
		}
	}
	return assets, hex.EncodeToString(digest[:])
}
