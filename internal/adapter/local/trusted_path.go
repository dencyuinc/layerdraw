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
	return os.Remove(path) // lgtm[go/path-injection]
}

func trustedPathRemoveAll(path string) error {
	return os.RemoveAll(path) // lgtm[go/path-injection]
}

func trustedPathReadFile(path string) ([]byte, error) {
	return os.ReadFile(path) // lgtm[go/path-injection]
}

func trustedPathOpenFile(path string, flag int, permission fs.FileMode) (*os.File, error) {
	return os.OpenFile(path, flag, permission) // lgtm[go/path-injection]
}

func trustedPathMkdir(path string, permission fs.FileMode) error {
	return os.Mkdir(path, permission) // lgtm[go/path-injection]
}

func trustedPathMkdirAll(path string, permission fs.FileMode) error {
	return os.MkdirAll(path, permission) // lgtm[go/path-injection]
}

func trustedPathRename(source, destination string) error {
	return os.Rename(source, destination) // lgtm[go/path-injection]
}

func trustedPathLstat(path string) (fs.FileInfo, error) {
	return os.Lstat(path) // lgtm[go/path-injection]
}

func trustedPathOpen(path string) (*os.File, error) {
	return os.Open(path) // lgtm[go/path-injection]
}

func trustedPathReadDir(path string) ([]os.DirEntry, error) {
	return os.ReadDir(path) // lgtm[go/path-injection]
}

func trustedPathChmod(path string, mode fs.FileMode) error {
	return os.Chmod(path, mode) // lgtm[go/path-injection]
}
