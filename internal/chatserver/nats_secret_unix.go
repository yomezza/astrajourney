//go:build !windows

package chatserver

import (
	"errors"
	"os"
)

func checkNATSSecretPermissions(_ string, info os.FileInfo) error {
	if info.Mode().Perm()&0077 != 0 {
		return errors.New("NATS password_file must not grant group/other access (use 0600 or 0400)")
	}
	return nil
}
