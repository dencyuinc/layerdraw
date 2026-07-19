//go:build darwin || linux || freebsd || openbsd || netbsd

// SPDX-License-Identifier: LicenseRef-LayerDraw-1.0
package local

import (
	"os"

	"golang.org/x/sys/unix"
)

func openLockFile(path string) (*os.File, error) {
	fd, err := unix.Open(path, unix.O_CREAT|unix.O_RDWR|unix.O_NOFOLLOW, fileMode)
	if err != nil {
		return nil, err
	}
	return os.NewFile(uintptr(fd), path), nil
}

func lockFile(f *os.File) error { return unix.Flock(int(f.Fd()), unix.LOCK_EX) }
func unlockFile(f *os.File)     { _ = unix.Flock(int(f.Fd()), unix.LOCK_UN) }
