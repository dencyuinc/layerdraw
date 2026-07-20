// SPDX-License-Identifier: LicenseRef-LayerDraw-1.0

// Package privatefs provides OS-correct checks for private application files.
package privatefs

import (
	"io/fs"
	"os"
)

// PermissionsMatch checks Unix permission bits where they are authoritative.
// Windows access control is represented by ACLs rather than fs.FileMode bits.
func PermissionsMatch(info fs.FileInfo, expected fs.FileMode) bool {
	return info != nil && permissionsMatch(info.Mode(), expected)
}

// SyncDirectory flushes directory metadata on platforms that expose that
// operation. Windows does not support os.File.Sync on directory handles, so
// the platform implementation validates the handle and treats the preceding
// file sync plus atomic rename as the strongest portable durability boundary.
func SyncDirectory(directory *os.File) error { return syncDirectory(directory) }
