//go:build !darwin && !linux && !freebsd && !openbsd && !netbsd

// SPDX-License-Identifier: LicenseRef-LayerDraw-1.0
package local

import (
	"errors"
	"os"
)

func openLockFile(path string) (*os.File, error) {
	return os.OpenFile(path, os.O_CREATE|os.O_RDWR, fileMode)
}

func lockFile(*os.File) error {
	return errors.New("local adapter process locking is unsupported on this platform")
}
func unlockFile(*os.File) {}
