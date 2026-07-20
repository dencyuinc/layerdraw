// SPDX-License-Identifier: LicenseRef-LayerDraw-1.0

//go:build !windows

package registry

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"time"

	"golang.org/x/sys/unix"
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
	fd, err := unix.Open(lockPath, unix.O_CREAT|unix.O_RDWR|unix.O_CLOEXEC|unix.O_NOFOLLOW, 0o600)
	if err != nil {
		s.mu.Unlock()
		return nil, err
	}
	file := os.NewFile(uintptr(fd), lockPath)
	for {
		err := unix.Flock(fd, unix.LOCK_EX|unix.LOCK_NB)
		if err == nil {
			writeTransactionLockMetadata(file)
			return func() {
				_ = unix.Flock(fd, unix.LOCK_UN)
				_ = file.Close()
				s.mu.Unlock()
			}, nil
		}
		if !errors.Is(err, unix.EWOULDBLOCK) && !errors.Is(err, unix.EAGAIN) {
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
