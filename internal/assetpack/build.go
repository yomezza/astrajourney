package assetpack

import (
	"archive/zip"
	"bytes"
	"compress/gzip"
	"encoding/json"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// Build prepares one platform-independent archive and a compact browser URL map.
func Build(root, output, tag string) (Release, map[string]string, error) {
	var release Release
	aliasesRaw, err := os.ReadFile(filepath.Join(root, "aliases.json"))
	if err != nil {
		return release, nil, err
	}
	var aliases map[string][]string
	if err = json.Unmarshal(aliasesRaw, &aliases); err != nil {
		return release, nil, err
	}
	legacy := map[string]string{}
	if raw, e := os.ReadFile(filepath.Join(filepath.Dir(root), "legacy-aliases.json")); e == nil {
		if e = json.Unmarshal(raw, &legacy); e != nil {
			return release, nil, e
		}
	}
	m := Manifest{Schema: Schema, Contract: Contract}
	files := map[string][]byte{}
	err = fs.WalkDir(os.DirFS(root), ".", func(p string, d fs.DirEntry, e error) error {
		if e != nil {
			return e
		}
		if d.IsDir() {
			return nil
		}
		if p == "aliases.json" {
			return nil
		}
		if !d.Type().IsRegular() {
			return fmt.Errorf("non-regular asset %s", p)
		}
		info, e := d.Info()
		if e != nil {
			return e
		}
		if info.Size() > 32<<20 || len(m.Entries) >= MaxFiles {
			return fmt.Errorf("asset limit exceeded: %s", p)
		}
		b, e := os.ReadFile(filepath.Join(root, filepath.FromSlash(p)))
		if e != nil {
			return e
		}
		mime := MIME(filepath.Ext(p))
		ent := Entry{Path: p, SHA256: Hash(b), Size: int64(len(b)), MIME: mime, Aliases: aliases[p]}
		ent.Aliases = nil
		for _, alias := range aliases[p] {
			if expected, ok := legacy[alias]; ok && expected != ent.SHA256 {
				continue
			}
			ent.Aliases = append(ent.Aliases, alias)
		}
		files[p] = b
		if mime == "image/svg+xml" {
			var buf bytes.Buffer
			gz := gzip.NewWriter(&buf)
			_, _ = gz.Write(b)
			if e = gz.Close(); e != nil {
				return e
			}
			files[p+".gz"] = buf.Bytes()
			ent.GzipSHA256 = Hash(buf.Bytes())
			ent.GzipSize = int64(buf.Len())
		}
		m.Entries = append(m.Entries, ent)
		return nil
	})
	if err != nil {
		return release, nil, err
	}
	sort.Slice(m.Entries, func(i, j int) bool { return m.Entries[i].Path < m.Entries[j].Path })
	m.Revision = m.Digest()
	raw, _ := json.MarshalIndent(m, "", "  ")
	raw = append(raw, '\n')
	if _, err = Decode(raw); err != nil {
		return release, nil, err
	}
	files["manifest.json"] = raw
	if err = os.MkdirAll(output, 0755); err != nil {
		return release, nil, err
	}
	archive := "astra-journey-assets-" + m.Revision + ".zip"
	var buf bytes.Buffer
	z := zip.NewWriter(&buf)
	names := make([]string, 0, len(files))
	for p := range files {
		names = append(names, p)
	}
	sort.Strings(names)
	for _, p := range names {
		w, e := z.Create(p)
		if e != nil {
			return release, nil, e
		}
		if _, e = w.Write(files[p]); e != nil {
			return release, nil, e
		}
	}
	if err = z.Close(); err != nil {
		return release, nil, err
	}
	if err = os.WriteFile(filepath.Join(output, archive), buf.Bytes(), 0644); err != nil {
		return release, nil, err
	}
	release = Release{Schema: Schema, Contract: Contract, Tag: tag, Revision: m.Revision, Archive: archive, SHA256: Hash(buf.Bytes()), Size: int64(buf.Len())}
	index, _ := json.MarshalIndent(release, "", "  ")
	if err = os.WriteFile(filepath.Join(output, "astra-journey-assets.json"), append(index, '\n'), 0644); err != nil {
		return release, nil, err
	}
	urls := map[string]string{}
	for _, e := range m.Entries {
		for _, a := range e.Aliases {
			urls[a] = e.URL()
			if !strings.Contains(a, "?") {
				urls[strings.TrimPrefix(a, "/")] = e.URL()
			}
		}
	}
	return release, urls, nil
}
