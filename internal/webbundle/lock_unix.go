//go:build !windows

package webbundle

import (
	"os"
	"syscall"
)

func lockStorage(f *os.File) error {
	return syscall.Flock(int(f.Fd()), syscall.LOCK_EX|syscall.LOCK_NB)
}
