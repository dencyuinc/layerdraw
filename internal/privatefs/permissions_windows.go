// SPDX-License-Identifier: LicenseRef-LayerDraw-1.0

//go:build windows

package privatefs

import (
	"fmt"
	"io/fs"
	"os"
)

func permissionsMatch(_, _ fs.FileMode) bool {
	// FileMode permission bits do not represent Windows ACLs. Callers still
	// enforce private app-data roots, file type, identity, and symlink bounds.
	return true
}

func syncDirectory(directory *os.File) error {
	if directory == nil {
		return fmt.Errorf("nil directory handle")
	}
	info, err := directory.Stat()
	if err != nil {
		return err
	}
	if !info.IsDir() {
		return fmt.Errorf("directory sync target is not a directory")
	}
	// os.File.Sync maps to FlushFileBuffers, which rejects directory handles
	// on Windows with ERROR_ACCESS_DENIED. The containing directory has already
	// been opened and validated; file contents were flushed before the rename.
	return nil
}
