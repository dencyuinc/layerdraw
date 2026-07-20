// SPDX-License-Identifier: LicenseRef-LayerDraw-1.0

//go:build windows

package registry

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"time"

	"golang.org/x/sys/windows"
)

func (s *DiskTransactionStore) lock(ctx context.Context) (func(), error) {
	s.mu.Lock()
	select {
	case <-ctx.Done():
		s.mu.Unlock()
		return nil, ctx.Err()
	default:
	}
	lockPath := filepath.Join(s.root, "transactions.lock")
	file, err := openWindowsTransactionLock(lockPath)
	if err != nil {
		s.mu.Unlock()
		return nil, err
	}
	handle := windows.Handle(file.Fd())
	overlapped := new(windows.Overlapped)
	for {
		err := windows.LockFileEx(handle, windows.LOCKFILE_EXCLUSIVE_LOCK|windows.LOCKFILE_FAIL_IMMEDIATELY, 0, 1, 0, overlapped)
		if err == nil {
			writeTransactionLockMetadata(file)
			return func() {
				_ = windows.UnlockFileEx(handle, 0, 1, 0, overlapped)
				_ = file.Close()
				s.mu.Unlock()
			}, nil
		}
		if !errors.Is(err, windows.ERROR_LOCK_VIOLATION) {
			_ = file.Close()
			s.mu.Unlock()
			return nil, err
		}
		select {
		case <-ctx.Done():
			_ = file.Close()
			s.mu.Unlock()
			return nil, ctx.Err()
		case <-time.After(2 * time.Millisecond):
		}
	}
}

func openWindowsTransactionLock(path string) (*os.File, error) {
	if linked, err := os.Lstat(path); err == nil {
		if !linked.Mode().IsRegular() || linked.Mode()&os.ModeSymlink != 0 {
			return nil, errors.New("registry transaction lock target is unsafe")
		}
	} else if !errors.Is(err, os.ErrNotExist) {
		return nil, err
	}
	file, err := os.OpenFile(path, os.O_CREATE|os.O_RDWR, 0o600)
	if err != nil {
		return nil, err
	}
	opened, openErr := file.Stat()
	linked, linkErr := os.Lstat(path)
	if openErr != nil || linkErr != nil || !opened.Mode().IsRegular() || !linked.Mode().IsRegular() || linked.Mode()&os.ModeSymlink != 0 || !os.SameFile(opened, linked) {
		_ = file.Close()
		return nil, errors.New("registry transaction lock target is unsafe")
	}
	return file, nil
}
