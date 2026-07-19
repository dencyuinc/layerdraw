// SPDX-License-Identifier: LicenseRef-LayerDraw-1.0

package local

import (
	"io/fs"
	"os"
)

// These wrappers are the explicit local-filesystem authority boundary. Their
// paths come from the caller that launched the private stdio host and are
// canonicalized and type/symlink/ownership checked by the calling adapter.
// CodeQL cannot model those adapter-specific sanitizers, so suppress only the
// path-injection query at the unavoidable OS sinks instead of suppressing it
// throughout the storage implementation.

func trustedPathRemove(path string) error {
	// codeql[go/path-injection]
	return os.Remove(path)
}

func trustedPathRemoveAll(path string) error {
	// codeql[go/path-injection]
	return os.RemoveAll(path)
}

func trustedPathReadFile(path string) ([]byte, error) {
	// codeql[go/path-injection]
	return os.ReadFile(path)
}

func trustedPathOpenFile(path string, flag int, permission fs.FileMode) (*os.File, error) {
	// codeql[go/path-injection]
	return os.OpenFile(path, flag, permission)
}

func trustedPathMkdir(path string, permission fs.FileMode) error {
	// codeql[go/path-injection]
	return os.Mkdir(path, permission)
}

func trustedPathMkdirAll(path string, permission fs.FileMode) error {
	// codeql[go/path-injection]
	return os.MkdirAll(path, permission)
}

func trustedPathRename(source, destination string) error {
	// codeql[go/path-injection]
	return os.Rename(source, destination)
}

func trustedPathLstat(path string) (fs.FileInfo, error) {
	// codeql[go/path-injection]
	return os.Lstat(path)
}

func trustedPathOpen(path string) (*os.File, error) {
	// codeql[go/path-injection]
	return os.Open(path)
}

func trustedPathReadDir(path string) ([]os.DirEntry, error) {
	// codeql[go/path-injection]
	return os.ReadDir(path)
}

func trustedPathChmod(path string, mode fs.FileMode) error {
	// codeql[go/path-injection]
	return os.Chmod(path, mode)
}
