package assetpack

import (
	"archive/zip"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"time"
)

func readManifestFromArchive(p string) (Manifest, error) {
	z, err := zip.OpenReader(p)
	if err != nil {
		return Manifest{}, err
	}
	defer z.Close()
	f, err := z.Open("manifest.json")
	if err != nil {
		return Manifest{}, err
	}
	defer f.Close()
	raw, err := io.ReadAll(io.LimitReader(f, 4<<20+1))
	if err != nil {
		return Manifest{}, err
	}
	return Decode(raw)
}

var repoPattern = regexp.MustCompile(`^[A-Za-z0-9_.-]+/[A-Za-z0-9_.-]+$`)
var tagPattern = regexp.MustCompile(`^v[0-9]+\.[0-9]+\.[0-9]+$`)

// Sync fetches published releases, never unreviewed main or paths supplied by NATS.
type Sync struct {
	Store                   *Store
	Repository, RequiredTag string
	Client                  *http.Client
	BaseURL                 string
}

func (s *Sync) Check(ctx context.Context, latest bool) (bool, error) {
	if !repoPattern.MatchString(s.Repository) || !tagPattern.MatchString(s.RequiredTag) {
		return false, errors.New("invalid graphics repository/release")
	}
	base := s.BaseURL
	if base == "" {
		base = "https://github.com"
	}
	prefix := base + "/" + s.Repository + "/releases/download/" + s.RequiredTag + "/"
	if latest {
		prefix = base + "/" + s.Repository + "/releases/latest/download/"
	}
	client := s.Client
	if client == nil {
		client = &http.Client{Timeout: 90 * time.Second}
	}
	request, err := http.NewRequestWithContext(ctx, "GET", prefix+"astra-journey-assets.json", nil)
	if err != nil {
		return false, err
	}
	request.Header.Set("User-Agent", "AstraJourney-CDN/1")
	response, err := client.Do(request)
	if err != nil {
		return false, errors.New("graphics release index unavailable")
	}
	defer response.Body.Close()
	if response.StatusCode != 200 {
		return false, fmt.Errorf("graphics release index HTTP %d", response.StatusCode)
	}
	var release Release
	if json.NewDecoder(io.LimitReader(response.Body, 32768)).Decode(&release) != nil || release.Schema != Schema || release.Contract != Contract || !ValidHash(release.Revision) || !ValidHash(release.SHA256) || !tagPattern.MatchString(release.Tag) || release.Size < 1 || release.Size > MaxPackBytes || release.Archive != "astra-journey-assets-"+release.Revision+".zip" {
		return false, errors.New("invalid or incompatible graphics release index")
	}
	if !latest && release.Tag != s.RequiredTag {
		return false, errors.New("graphics release tag mismatch")
	}
	if s.Store.Has(release.Revision) {
		return false, nil
	}
	// The archive comes only from the validated, fixed repository and published tag.
	request, err = http.NewRequestWithContext(ctx, "GET", base+"/"+s.Repository+"/releases/download/"+release.Tag+"/"+release.Archive, nil)
	if err != nil {
		return false, err
	}
	response, err = client.Do(request)
	if err != nil {
		return false, errors.New("graphics archive unavailable")
	}
	defer response.Body.Close()
	if response.StatusCode != 200 {
		return false, fmt.Errorf("graphics archive HTTP %d", response.StatusCode)
	}
	f, err := os.CreateTemp(filepath.Join(s.Store.Directory, "staging", "packs"), "download-*.zip")
	if err != nil {
		return false, err
	}
	defer os.Remove(f.Name())
	digest := sha256.New()
	n, err := io.Copy(io.MultiWriter(f, digest), io.LimitReader(response.Body, release.Size+1))
	closeErr := f.Close()
	if err != nil || closeErr != nil || n != release.Size {
		return false, errors.New("incomplete graphics archive")
	}
	if hex.EncodeToString(digest.Sum(nil)) != release.SHA256 {
		return false, errors.New("graphics archive checksum mismatch")
	}
	z, err := readManifestFromArchive(f.Name())
	if err != nil || z.Revision != release.Revision {
		return false, errors.New("graphics archive revision mismatch")
	}
	_, err = s.Store.Install(f.Name())
	return err == nil, err
}
