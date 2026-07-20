// SPDX-License-Identifier: LicenseRef-LayerDraw-1.0

// Package privatefs provides OS-correct checks for private application files.
package privatefs

import "io/fs"

// PermissionsMatch checks Unix permission bits where they are authoritative.
// Windows access control is represented by ACLs rather than fs.FileMode bits.
func PermissionsMatch(info fs.FileInfo, expected fs.FileMode) bool {
	return info != nil && permissionsMatch(info.Mode(), expected)
}
