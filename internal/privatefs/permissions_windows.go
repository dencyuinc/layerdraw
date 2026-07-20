// SPDX-License-Identifier: LicenseRef-LayerDraw-1.0

//go:build windows

package privatefs

import "io/fs"

func permissionsMatch(_, _ fs.FileMode) bool {
	// FileMode permission bits do not represent Windows ACLs. Callers still
	// enforce private app-data roots, file type, identity, and symlink bounds.
	return true
}
