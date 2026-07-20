// SPDX-License-Identifier: LicenseRef-LayerDraw-1.0

//go:build !windows

package privatefs

import "io/fs"

func permissionsMatch(mode, expected fs.FileMode) bool { return mode.Perm() == expected.Perm() }
