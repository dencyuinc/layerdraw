//go:build !darwin && !linux && !freebsd && !openbsd && !netbsd && !windows

// SPDX-License-Identifier: LicenseRef-LayerDraw-1.0
package local

import (
	"errors"
	"os"
)

func openLockFile(path string) (*os.File, error) {
	return os.OpenFile(path, os.O_CREATE|os.O_RDWR, fileMode)
}

func lockFile(*os.File) (func(), error) {
	return nil, errors.New("local adapter process locking is unsupported on this platform")
}
