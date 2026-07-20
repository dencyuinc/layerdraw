// SPDX-License-Identifier: LicenseRef-LayerDraw-1.0

//go:build !windows

package privatefs

import (
	"io/fs"
	"os"
)

func permissionsMatch(mode, expected fs.FileMode) bool { return mode.Perm() == expected.Perm() }

func syncDirectory(directory *os.File) error { return directory.Sync() }
