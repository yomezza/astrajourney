package cdnserver

import (
	"os"
	"path/filepath"
	"strings"
)

// Called only after obtaining the exclusive storage owner lock. Interrupted
// temporary transfers have no published metadata and can safely be retried.
func clearInterruptedStaging(directory string) error {
	for _, sub := range []string{"staging/packs", "staging/uploads"} {
		root := filepath.Join(directory, filepath.FromSlash(sub))
		entries, err := os.ReadDir(root)
		if err != nil {
			return err
		}
		for _, entry := range entries {
			n := entry.Name()
			if strings.HasPrefix(n, "install-") || strings.HasPrefix(n, "download-") || strings.HasPrefix(n, ".pending-") {
				if err = os.RemoveAll(filepath.Join(root, n)); err != nil {
					return err
				}
			}
		}
	}
	return nil
}
