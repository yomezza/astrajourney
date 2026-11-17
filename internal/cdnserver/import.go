package cdnserver

import (
	"bytes"
	"encoding/json"
	"fmt"
	"image/png"
	"log"
	"os"
	"path/filepath"
	"strings"

	"github.com/yomezza/cosmosimulator/internal/assetpack"
	"github.com/yomezza/cosmosimulator/internal/platform/fileatom"
	"github.com/yomezza/cosmosimulator/internal/platform/identifiers"
	"github.com/yomezza/cosmosimulator/internal/platform/mediastore"
)

// ImportGuide is offline and copy-only: UUIDs and original PNG bytes survive.
// It makes no publication/ACL decision and never opens PostgreSQL.
func (s *Service) ImportGuide(directory string) error {
	source, err := os.OpenRoot(directory)
	if err != nil {
		return err
	}
	defer source.Close()
	entries, err := os.ReadDir(directory)
	if err != nil {
		return err
	}
	copied := 0
	for _, entry := range entries {
		if !strings.HasSuffix(entry.Name(), ".png") {
			continue
		}
		id := strings.TrimSuffix(entry.Name(), ".png")
		if !identifiers.ValidUUID(id) {
			return fmt.Errorf("invalid legacy image filename: %s", entry.Name())
		}
		info, err := entry.Info()
		if err != nil || !info.Mode().IsRegular() || info.Size() > mediastore.Limit {
			return fmt.Errorf("invalid legacy image: %s", id)
		}
		raw, err := source.ReadFile(entry.Name())
		if err != nil {
			return err
		}
		cfg, err := png.DecodeConfig(bytes.NewReader(raw))
		if err != nil || cfg.Width > 4096 || cfg.Height > 4096 {
			return fmt.Errorf("invalid legacy PNG: %s", id)
		}
		sum := assetpack.Hash(raw)
		if old, err := s.media.root.ReadFile(mediaObject(id)); err == nil && assetpack.Hash(old) != sum {
			return fmt.Errorf("existing CDN object differs: %s", id)
		}
		if old, err := s.media.root.ReadFile(mediaReceipt(id)); err == nil {
			var receipt mediastore.Receipt
			if json.Unmarshal(old, &receipt) != nil || receipt.Deleted || receipt.SHA256 != sum {
				return fmt.Errorf("existing CDN receipt differs: %s", id)
			}
		}
		for _, p := range []string{mediaObject(id), mediaReceipt(id)} {
			if err = s.media.root.MkdirAll(filepath.ToSlash(filepath.Dir(p)), 0700); err != nil {
				return err
			}
		}
		receipt := mediastore.Receipt{ID: id, SHA256: sum, Size: int64(len(raw)), Width: cfg.Width, Height: cfg.Height, SourceFormat: "png"}
		body, _ := json.Marshal(receipt)
		if err = fileatom.WriteStaged(filepath.Join(s.store.Directory, filepath.FromSlash(mediaObject(id))), raw, 0600, filepath.Join(s.store.Directory, "staging", "uploads")); err != nil {
			return err
		}
		if err = fileatom.Write(filepath.Join(s.store.Directory, filepath.FromSlash(mediaReceipt(id))), body, 0600); err != nil {
			return err
		}
		copied++
	}
	log.Printf("CDN legacy guide import verified: %d images copied; source retained", copied)
	return nil
}
