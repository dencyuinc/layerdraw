// SPDX-License-Identifier: LicenseRef-LayerDraw-1.0

//go:build windows

package local

import (
	"errors"
	"os"

	"golang.org/x/sys/windows"
)

func openLockFile(path string) (*os.File, error) {
	linked, err := os.Lstat(path)
	if err == nil {
		if !linked.Mode().IsRegular() || linked.Mode()&os.ModeSymlink != 0 {
			return nil, errors.New("local adapter lock target is unsafe")
		}
	} else if !errors.Is(err, os.ErrNotExist) {
		return nil, err
	}
	file, err := os.OpenFile(path, os.O_CREATE|os.O_RDWR, fileMode)
	if err != nil {
		return nil, err
	}
	opened, openErr := file.Stat()
	linked, linkErr := os.Lstat(path)
	if openErr != nil || linkErr != nil || !opened.Mode().IsRegular() || !linked.Mode().IsRegular() || linked.Mode()&os.ModeSymlink != 0 || !os.SameFile(opened, linked) {
		_ = file.Close()
		return nil, errors.New("local adapter lock target is unsafe")
	}
	return file, nil
}

func lockFile(file *os.File) error {
	return windows.LockFileEx(windows.Handle(file.Fd()), windows.LOCKFILE_EXCLUSIVE_LOCK, 0, 1, 0, new(windows.Overlapped))
}

func unlockFile(file *os.File) {
	_ = windows.UnlockFileEx(windows.Handle(file.Fd()), 0, 1, 0, new(windows.Overlapped))
}
