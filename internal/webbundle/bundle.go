// Package webbundle manages external, immutable HTML/CSS/JS release packages.
// It has no dependency on the game database or on graphic file formats.
package webbundle

import (
	"archive/zip"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"net/http"
	"os"
	"path"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/yomezza/cosmosimulator/internal/assetpack"
	"github.com/yomezza/cosmosimulator/internal/platform/clientcontract"
	"github.com/yomezza/cosmosimulator/internal/platform/fileatom"
	"github.com/yomezza/cosmosimulator/internal/platform/httpkit"
)

const Schema = 1
const Contract = "astra-web-v1"
const APIContract = clientcontract.API
const MaxBytes int64 = 32 << 20
const MaxFiles = 1024

type Entry struct {
	Path   string `json:"path"`
	SHA256 string `json:"sha256"`
	Size   int64  `json:"size"`
}
type Manifest struct {
	Schema   int     `json:"schema_version"`
	Contract string  `json:"client_contract"`
	API      string  `json:"api_contract"`
	Revision string  `json:"revision"`
	Entries  []Entry `json:"entries"`
}
type Release struct {
	Schema   int    `json:"schema_version"`
	Contract string `json:"client_contract"`
	API      string `json:"api_contract"`
	Tag      string `json:"tag"`
	Revision string `json:"revision"`
	Archive  string `json:"archive"`
	SHA256   string `json:"sha256"`
	Size     int64  `json:"size"`
}

func mime(p string) string {
	return map[string]string{".html": "text/html; charset=utf-8", ".css": "text/css; charset=utf-8", ".js": "text/javascript; charset=utf-8", ".json": "application/json; charset=utf-8"}[path.Ext(p)]
}
func (m Manifest) Digest() string { m.Revision = ""; b, _ := json.Marshal(m); return assetpack.Hash(b) }
func Decode(b []byte) (Manifest, error) {
	var m Manifest
	if len(b) > 1<<20 || json.Unmarshal(b, &m) != nil || m.Schema != Schema || m.Contract != Contract || m.API != APIContract || !assetpack.ValidHash(m.Revision) || m.Digest() != m.Revision || len(m.Entries) < 1 || len(m.Entries) > MaxFiles {
		return m, errors.New("incompatible or corrupt Web manifest")
	}
	seen := map[string]bool{}
	total := int64(0)
	for _, e := range m.Entries {
		key := strings.ToLower(e.Path)
		if !assetpack.ValidPath(e.Path) || seen[key] || mime(e.Path) == "" || !assetpack.ValidHash(e.SHA256) || e.Size < 1 || e.Size > 8<<20 {
			return m, errors.New("invalid Web entry")
		}
		seen[key] = true
		total += e.Size
	}
	if total > MaxBytes || !seen["index.html"] || !seen["bootstrap.js"] || !seen["ru.json"] || !seen["en.json"] {
		return m, errors.New("incomplete or oversized Web bundle")
	}
	return m, nil
}

// Build creates a reproducible archive. Graphics remain exclusively in CDN packs.
func Build(directory, output, tag string) (Release, error) {
	if !tagPattern.MatchString(tag) {
		return Release{}, errors.New("invalid Web release tag")
	}
	m := Manifest{Schema: Schema, Contract: Contract, API: APIContract}
	files := map[string][]byte{}
	err := filepath.WalkDir(directory, func(p string, d fs.DirEntry, e error) error {
		if e != nil {
			return e
		}
		if d.IsDir() {
			return nil
		}
		if !d.Type().IsRegular() {
			return errors.New("Web sources must be regular files")
		}
		rel, e := filepath.Rel(directory, p)
		if e != nil {
			return e
		}
		rel = filepath.ToSlash(rel)
		if !assetpack.ValidPath(rel) || mime(rel) == "" {
			return fmt.Errorf("unsupported Web source %s", rel)
		}
		info, e := d.Info()
		if e != nil {
			return e
		}
		if info.Size() > 8<<20 {
			return errors.New("oversized Web source")
		}
		b, e := os.ReadFile(p)
		if e != nil {
			return e
		}
		files[rel] = b
		m.Entries = append(m.Entries, Entry{rel, assetpack.Hash(b), int64(len(b))})
		return nil
	})
	if err != nil {
		return Release{}, err
	}
	sort.Slice(m.Entries, func(i, j int) bool { return m.Entries[i].Path < m.Entries[j].Path })
	m.Revision = m.Digest()
	manifest, _ := json.Marshal(m)
	if _, err = Decode(manifest); err != nil {
		return Release{}, err
	}
	if err = os.MkdirAll(output, 0755); err != nil {
		return Release{}, err
	}
	r := Release{Schema: Schema, Contract: Contract, API: APIContract, Tag: tag, Revision: m.Revision, Archive: "astra-journey-web-" + m.Revision + ".zip"}
	f, err := os.CreateTemp(output, "web-*.zip")
	if err != nil {
		return r, err
	}
	defer os.Remove(f.Name())
	z := zip.NewWriter(f)
	add := func(name string, b []byte) error {
		h := &zip.FileHeader{Name: name, Method: zip.Deflate}
		h.SetMode(0644)
		h.Modified = time.Date(2000, 1, 1, 0, 0, 0, 0, time.UTC)
		w, e := z.CreateHeader(h)
		if e == nil {
			_, e = w.Write(b)
		}
		return e
	}
	if err = add("manifest.json", manifest); err == nil {
		for _, e := range m.Entries {
			if err = add(e.Path, files[e.Path]); err != nil {
				break
			}
		}
	}
	if e := z.Close(); err == nil {
		err = e
	}
	if e := f.Close(); err == nil {
		err = e
	}
	if err != nil {
		return r, err
	}
	b, err := os.ReadFile(f.Name())
	if err != nil {
		return r, err
	}
	r.Size = int64(len(b))
	r.SHA256 = assetpack.Hash(b)
	if err = fileatom.Write(filepath.Join(output, r.Archive), b, 0644); err != nil {
		return r, err
	}
	index, _ := json.MarshalIndent(r, "", "  ")
	err = fileatom.Write(filepath.Join(output, "astra-journey-web.json"), append(index, '\n'), 0644)
	return r, err
}

type activation struct {
	Current  string `json:"current"`
	Previous string `json:"previous,omitempty"`
}
type installed struct {
	manifest Manifest
	entries  map[string]Entry
	root     *os.Root
}
type Store struct {
	Directory string
	owner     *os.File
	root      *os.Root
	mu        sync.RWMutex
	packs     map[string]*installed
	active    activation
	install   sync.Mutex
	content   contentCache
}

func Open(directory string) (*Store, error) {
	directory, e := filepath.Abs(directory)
	if e != nil {
		return nil, e
	}
	for _, p := range []string{"packs", "state", "staging"} {
		if e = os.MkdirAll(filepath.Join(directory, p), 0750); e != nil {
			return nil, e
		}
	}
	owner, e := os.OpenFile(filepath.Join(directory, "state", "owner.lock"), os.O_CREATE|os.O_RDWR, 0600)
	if e != nil {
		return nil, e
	}
	if e = lockStorage(owner); e != nil {
		owner.Close()
		return nil, errors.New("Web storage is already owned by another process")
	}
	root, e := os.OpenRoot(directory)
	if e != nil {
		owner.Close()
		return nil, e
	}
	s := &Store{Directory: directory, owner: owner, root: root, packs: map[string]*installed{}}
	entries, e := os.ReadDir(filepath.Join(directory, "packs"))
	if e != nil {
		s.Close()
		return nil, e
	}
	for _, d := range entries {
		if d.IsDir() && assetpack.ValidHash(d.Name()) {
			if p, e := s.verify(d.Name()); e == nil {
				s.packs[d.Name()] = p
			}
		}
	}
	b, e := root.ReadFile("state/active.json")
	if e == nil {
		if json.Unmarshal(b, &s.active) != nil {
			s.Close()
			return nil, errors.New("invalid Web activation state")
		}
	} else if !os.IsNotExist(e) {
		s.Close()
		return nil, e
	}
	if s.packs[s.active.Current] == nil {
		s.active.Current = s.active.Previous
		s.active.Previous = ""
	}
	if s.packs[s.active.Current] == nil {
		s.active = activation{}
	}
	// Interrupted downloads and extraction directories can never become active.
	staging, e := os.ReadDir(filepath.Join(directory, "staging"))
	if e != nil {
		s.Close()
		return nil, e
	}
	for _, d := range staging {
		if e = root.RemoveAll("staging/" + d.Name()); e != nil {
			s.Close()
			return nil, e
		}
	}
	return s, nil
}
func (s *Store) verify(revision string) (*installed, error) {
	if !assetpack.ValidHash(revision) {
		return nil, errors.New("invalid Web revision")
	}
	root, e := s.root.OpenRoot("packs/" + revision)
	if e != nil {
		return nil, e
	}
	ok := false
	defer func() {
		if !ok {
			root.Close()
		}
	}()
	b, e := root.ReadFile("manifest.json")
	if e != nil {
		return nil, e
	}
	m, e := Decode(b)
	if e != nil || m.Revision != revision {
		return nil, errors.New("invalid installed Web bundle")
	}
	entries := map[string]Entry{}
	for _, file := range m.Entries {
		f, e := root.Open(file.Path)
		if e != nil {
			return nil, e
		}
		b, e := io.ReadAll(io.LimitReader(f, file.Size+1))
		f.Close()
		if e != nil || int64(len(b)) != file.Size || assetpack.Hash(b) != file.SHA256 {
			return nil, errors.New("Web file checksum mismatch")
		}
		entries[file.Path] = file
	}
	ok = true
	return &installed{m, entries, root}, nil
}
func (s *Store) Close() error {
	for _, p := range s.packs {
		p.root.Close()
	}
	s.root.Close()
	return s.owner.Close()
}
func (s *Store) Revision() string { s.mu.RLock(); defer s.mu.RUnlock(); return s.active.Current }
func (s *Store) Activate(revision string) error {
	s.install.Lock()
	defer s.install.Unlock()
	return s.activate(revision)
}
func (s *Store) activate(revision string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.packs[revision] == nil {
		return errors.New("Web bundle is not installed or verified")
	}
	if s.active.Current == revision {
		return nil
	}
	next := activation{revision, s.active.Current}
	b, _ := json.Marshal(next)
	if e := fileatom.Write(filepath.Join(s.Directory, "state", "active.json"), b, 0600); e != nil {
		return e
	}
	s.active = next
	return nil
}
func (s *Store) Install(archive string) (string, error) {
	s.install.Lock()
	defer s.install.Unlock()
	f, e := os.Open(archive)
	if e != nil {
		return "", e
	}
	defer f.Close()
	info, e := f.Stat()
	if e != nil {
		return "", e
	}
	if !info.Mode().IsRegular() || info.Size() > MaxBytes {
		return "", errors.New("oversized Web archive")
	}
	z, e := zip.NewReader(f, info.Size())
	if e != nil {
		return "", e
	}
	if len(z.File) > MaxFiles+1 {
		return "", errors.New("too many Web files")
	}
	records := map[string]*zip.File{}
	for _, entry := range z.File {
		key := strings.ToLower(entry.Name)
		if !assetpack.ValidPath(entry.Name) || !entry.Mode().IsRegular() || records[key] != nil || entry.UncompressedSize64 > uint64(MaxBytes) {
			return "", errors.New("invalid Web archive path")
		}
		records[key] = entry
	}
	mf := records["manifest.json"]
	if mf == nil {
		return "", errors.New("Web manifest missing")
	}
	r, e := mf.Open()
	if e != nil {
		return "", e
	}
	b, e := io.ReadAll(io.LimitReader(r, 1<<20+1))
	r.Close()
	if e != nil {
		return "", e
	}
	m, e := Decode(b)
	if e != nil {
		return "", e
	}
	if len(records) != len(m.Entries)+1 {
		return "", errors.New("unexpected Web archive files")
	}
	stage, e := os.MkdirTemp(filepath.Join(s.Directory, "staging"), "install-")
	if e != nil {
		return "", e
	}
	defer os.RemoveAll(stage)
	for _, entry := range m.Entries {
		source := records[strings.ToLower(entry.Path)]
		if source == nil || source.Name != entry.Path || source.UncompressedSize64 != uint64(entry.Size) {
			return "", errors.New("Web entry missing or wrong size")
		}
		r, e := source.Open()
		if e != nil {
			return "", e
		}
		data, e := io.ReadAll(io.LimitReader(r, entry.Size+1))
		r.Close()
		if e != nil || int64(len(data)) != entry.Size || assetpack.Hash(data) != entry.SHA256 {
			return "", errors.New("Web entry checksum mismatch")
		}
		dst := filepath.Join(stage, filepath.FromSlash(entry.Path))
		if e = os.MkdirAll(filepath.Dir(dst), 0750); e != nil {
			return "", e
		}
		if e = os.WriteFile(dst, data, 0644); e != nil {
			return "", e
		}
	}
	if e = os.WriteFile(filepath.Join(stage, "manifest.json"), b, 0644); e != nil {
		return "", e
	}
	s.mu.RLock()
	known := s.packs[m.Revision] != nil
	s.mu.RUnlock()
	if !known {
		destination := filepath.Join(s.Directory, "packs", m.Revision)
		if _, e = os.Lstat(destination); e == nil {
			return "", errors.New("unverified Web destination already exists")
		}
		if e = os.Rename(stage, destination); e != nil {
			return "", e
		}
		p, e := s.verify(m.Revision)
		if e != nil {
			return "", e
		}
		s.mu.Lock()
		s.packs[m.Revision] = p
		s.mu.Unlock()
	}
	return m.Revision, s.activate(m.Revision)
}
func (s *Store) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if r.Method != "GET" && r.Method != "HEAD" {
		w.Header().Set("Allow", "GET, HEAD")
		http.Error(w, "Method not allowed", 405)
		return
	}
	s.mu.RLock()
	revision := s.active.Current
	name := "index.html"
	versioned := false
	if strings.HasPrefix(r.URL.Path, "/web/") {
		parts := strings.SplitN(strings.TrimPrefix(r.URL.Path, "/web/"), "/", 2)
		if len(parts) != 2 || !assetpack.ValidHash(parts[0]) {
			s.mu.RUnlock()
			http.NotFound(w, r)
			return
		}
		revision, name, versioned = parts[0], parts[1], true
	} else if r.URL.Path != "/" && r.URL.Path != "/index.html" {
		name = strings.TrimPrefix(r.URL.Path, "/")
	}
	p := s.packs[revision]
	if p == nil {
		s.mu.RUnlock()
		w.Header().Set("Retry-After", "10")
		http.Error(w, "Web bundle unavailable; install a matching Web package", 503)
		return
	}
	entry, exists := p.entries[name]
	s.mu.RUnlock()
	if !exists {
		http.NotFound(w, r)
		return
	}
	if !versioned && name != "index.html" {
		w.Header().Set("Cache-Control", "no-cache")
		http.Redirect(w, r, "/web/"+revision+"/"+name, 307)
		return
	}
	content, e := s.content.get(r.Context(), revision+"/"+entry.SHA256+"/"+name, func() (*httpkit.StaticContent, error) {
		f, err := p.root.Open(name)
		if err != nil {
			return nil, err
		}
		defer f.Close()
		body, err := io.ReadAll(io.LimitReader(f, entry.Size+1))
		if err != nil {
			return nil, err
		}
		if int64(len(body)) != entry.Size {
			return nil, errors.New("Web file size changed")
		}
		if name == "index.html" {
			// Relative imports and styles always stay within this revision.
			html := strings.ReplaceAll(string(body), `href="styles/`, `href="/web/`+revision+`/styles/`)
			html = strings.ReplaceAll(html, `src="bootstrap.js"`, `src="/web/`+revision+`/bootstrap.js"`)
			body = []byte(html)
		}
		return httpkit.NewStaticContent(body), nil
	})
	if e != nil {
		http.Error(w, "Web bundle unavailable", 503)
		return
	}
	w.Header().Set("Content-Type", mime(name))
	w.Header().Set("X-Astra-Web-Revision", revision)
	if name == "index.html" {
		w.Header().Set("Cache-Control", "no-cache")
	} else {
		w.Header().Set("Cache-Control", "public, max-age=31536000, immutable")
	}
	content.ServeHTTP(w, r)
}
