// Package fileatom publishes complete small files on both supported platforms.
package fileatom

import (
	"os"
	"path/filepath"
)

func Write(path string, body []byte, mode os.FileMode) error {
	return WriteStaged(path, body, mode, filepath.Dir(path))
}

// WriteStaged requires staging and destination to reside on the same volume.
func WriteStaged(path string, body []byte, mode os.FileMode, staging string) error {
	f, err := os.CreateTemp(staging, ".pending-")
	if err != nil {
		return err
	}
	defer os.Remove(f.Name())
	if err = f.Chmod(mode); err == nil {
		_, err = f.Write(body)
	}
	if err == nil {
		err = f.Sync()
	}
	closeErr := f.Close()
	if err != nil {
		return err
	}
	if closeErr != nil {
		return closeErr
	}
	if err = replace(f.Name(), path); err != nil {
		return err
	}
	return syncDirectory(filepath.Dir(path))
}
