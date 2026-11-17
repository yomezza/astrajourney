//go:build !windows

package fileatom

import "os"

func replace(from, to string) error { return os.Rename(from, to) }

func syncDirectory(path string) error {
	d, e := os.Open(path)
	if e != nil {
		return e
	}
	defer d.Close()
	return d.Sync()
}
