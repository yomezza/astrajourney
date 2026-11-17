// Package assetpack defines the versioned, external public graphics format.
// It contains no embedded image bytes and has no database dependency.
package assetpack

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io/fs"
	"path"
	"regexp"
	"strings"
)

const Schema = 1
const Contract = "astra-assets-v1"
const MaxPackBytes int64 = 256 << 20
const MaxFiles = 4096

var digestPattern = regexp.MustCompile(`^[a-f0-9]{64}$`)

type Entry struct {
	Path       string   `json:"path"`
	SHA256     string   `json:"sha256"`
	Size       int64    `json:"size"`
	MIME       string   `json:"mime"`
	Aliases    []string `json:"aliases"`
	GzipSHA256 string   `json:"gzip_sha256,omitempty"`
	GzipSize   int64    `json:"gzip_size,omitempty"`
}

func (e Entry) URL() string { return "/assets/sha256/" + e.SHA256 + "/" + e.Path }

type Manifest struct {
	Schema   int     `json:"schema_version"`
	Contract string  `json:"client_contract"`
	Revision string  `json:"revision"`
	Entries  []Entry `json:"entries"`
}
type Release struct {
	Schema   int    `json:"schema_version"`
	Contract string `json:"client_contract"`
	Tag      string `json:"tag"`
	Revision string `json:"revision"`
	Archive  string `json:"archive"`
	SHA256   string `json:"sha256"`
	Size     int64  `json:"size"`
}

func Hash(b []byte) string    { h := sha256.Sum256(b); return hex.EncodeToString(h[:]) }
func ValidHash(s string) bool { return digestPattern.MatchString(s) }
func MIME(extension string) string {
	return map[string]string{".svg": "image/svg+xml", ".png": "image/png", ".jpg": "image/jpeg", ".jpeg": "image/jpeg", ".webp": "image/webp", ".gif": "image/gif", ".avif": "image/avif", ".ico": "image/vnd.microsoft.icon", ".ktx2": "image/ktx2", ".woff2": "font/woff2", ".json": "application/json"}[extension]
}
func ValidPath(p string) bool {
	if !fs.ValidPath(p) || strings.ContainsAny(p, "\\:\x00?#%") || len(p) > 240 {
		return false
	}
	for _, c := range strings.Split(p, "/") {
		if strings.HasPrefix(c, ".") || strings.HasSuffix(c, ".") || strings.HasSuffix(c, " ") {
			return false
		}
		stem := strings.ToUpper(strings.Split(c, ".")[0])
		if stem == "CON" || stem == "PRN" || stem == "AUX" || stem == "NUL" || regexp.MustCompile(`^(COM|LPT)[0-9]$`).MatchString(stem) {
			return false
		}
	}
	return true
}
func (m Manifest) Digest() string { m.Revision = ""; raw, _ := json.Marshal(m); return Hash(raw) }
func Decode(raw []byte) (Manifest, error) {
	var m Manifest
	if len(raw) > 4<<20 || json.Unmarshal(raw, &m) != nil {
		return m, errors.New("invalid asset manifest")
	}
	if m.Schema != Schema || m.Contract != Contract || !ValidHash(m.Revision) || m.Digest() != m.Revision || len(m.Entries) == 0 || len(m.Entries) > MaxFiles {
		return m, errors.New("incompatible or corrupt asset manifest")
	}
	paths, aliases := map[string]bool{}, map[string]bool{}
	var total int64
	for _, e := range m.Entries {
		ext := path.Ext(e.Path)
		mime := MIME(ext)
		if !ValidPath(e.Path) || paths[strings.ToLower(e.Path)] || !ValidHash(e.SHA256) || e.Size < 1 || e.Size > 32<<20 || mime == "" || e.MIME != mime {
			return m, errors.New("invalid asset entry")
		}
		paths[strings.ToLower(e.Path)] = true
		total += e.Size + e.GzipSize
		if e.GzipSHA256 != "" && (ext != ".svg" || !ValidHash(e.GzipSHA256) || e.GzipSize < 1 || e.GzipSize > e.Size+1024) {
			return m, errors.New("invalid encoded representation")
		}
		if e.GzipSHA256 == "" && e.GzipSize != 0 {
			return m, errors.New("unexpected encoded size")
		}
		for _, a := range e.Aliases {
			parts := strings.SplitN(strings.TrimPrefix(a, "/"), "?", 2)
			prefix := strings.SplitN(parts[0], "/", 2)[0]
			if prefix == "api" || prefix == "cdn" || prefix == "assets" || prefix == "internal" || prefix == "private" || prefix == "staging" || prefix == "state" || prefix == "quarantine" {
				return m, errors.New("reserved asset alias")
			}
			if !strings.HasPrefix(a, "/") || !ValidPath(parts[0]) || aliases[a] || (len(parts) == 2 && !regexp.MustCompile(`^v=[a-zA-Z0-9_-]{1,64}$`).MatchString(parts[1])) {
				return m, errors.New("invalid asset alias")
			}
			aliases[a] = true
		}
	}
	if total > MaxPackBytes {
		return m, errors.New("asset pack too large")
	}
	return m, nil
}
