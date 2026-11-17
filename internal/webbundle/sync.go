package webbundle

import (
	"archive/zip"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/yomezza/cosmosimulator/internal/assetpack"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"time"
)

var repoPattern = regexp.MustCompile(`^[A-Za-z0-9_.-]+/[A-Za-z0-9_.-]+$`)
var tagPattern = regexp.MustCompile(`^v[0-9]+\.[0-9]+\.[0-9]+$`)

func hash(s string) bool { return assetpack.ValidHash(s) }
func archiveManifest(name string) (Manifest, error) {
	z, e := zip.OpenReader(name)
	if e != nil {
		return Manifest{}, e
	}
	defer z.Close()
	f, e := z.Open("manifest.json")
	if e != nil {
		return Manifest{}, e
	}
	defer f.Close()
	b, e := io.ReadAll(io.LimitReader(f, 1<<20+1))
	if e != nil {
		return Manifest{}, e
	}
	return Decode(b)
}

// Sync is pinned to one published release, so a newer frontend cannot silently
// introduce requests that the installed Cosmos API does not implement.
type Sync struct {
	Store                    *Store
	Repository, Tag, BaseURL string
	Client                   *http.Client
}

func (s *Sync) Check(ctx context.Context) (bool, error) {
	if !repoPattern.MatchString(s.Repository) || !tagPattern.MatchString(s.Tag) {
		return false, errors.New("invalid Web repository/release")
	}
	base := s.BaseURL
	if base == "" {
		base = "https://github.com"
	}
	prefix := base + "/" + s.Repository + "/releases/download/" + s.Tag + "/"
	client := s.Client
	if client == nil {
		client = &http.Client{Timeout: 90 * time.Second}
	}
	req, e := http.NewRequestWithContext(ctx, "GET", prefix+"astra-journey-web.json", nil)
	if e != nil {
		return false, e
	}
	req.Header.Set("User-Agent", "AstraJourney-Web/1")
	res, e := client.Do(req)
	if e != nil {
		return false, errors.New("Web release index unavailable")
	}
	defer res.Body.Close()
	if res.StatusCode != 200 {
		return false, fmt.Errorf("Web release index HTTP %d", res.StatusCode)
	}
	b, e := io.ReadAll(io.LimitReader(res.Body, 32769))
	var release Release
	if e != nil || len(b) > 32768 || json.Unmarshal(b, &release) != nil || release.Schema != Schema || release.Contract != Contract || release.API != APIContract || release.Tag != s.Tag || !hash(release.Revision) || !hash(release.SHA256) || release.Size < 1 || release.Size > MaxBytes || release.Archive != "astra-journey-web-"+release.Revision+".zip" {
		return false, errors.New("invalid or incompatible Web release index")
	}
	if s.Store.Revision() == release.Revision {
		return false, nil
	}
	req, e = http.NewRequestWithContext(ctx, "GET", prefix+release.Archive, nil)
	if e != nil {
		return false, e
	}
	res, e = client.Do(req)
	if e != nil {
		return false, errors.New("Web archive unavailable")
	}
	defer res.Body.Close()
	if res.StatusCode != 200 {
		return false, fmt.Errorf("Web archive HTTP %d", res.StatusCode)
	}
	f, e := os.CreateTemp(filepath.Join(s.Store.Directory, "staging"), "download-*.zip")
	if e != nil {
		return false, e
	}
	defer os.Remove(f.Name())
	digest := sha256.New()
	n, e := io.Copy(io.MultiWriter(f, digest), io.LimitReader(res.Body, release.Size+1))
	closed := f.Close()
	if e != nil || closed != nil || n != release.Size || hex.EncodeToString(digest.Sum(nil)) != release.SHA256 {
		return false, errors.New("Web archive checksum/size mismatch")
	}
	// Check the manifest revision before Install can change the active pointer.
	manifest, e := archiveManifest(f.Name())
	if e != nil || manifest.Revision != release.Revision {
		return false, errors.New("Web archive revision mismatch")
	}
	_, e = s.Store.Install(f.Name())
	return e == nil, e
}
