// SPDX-License-Identifier: LicenseRef-LayerDraw-1.0

//go:build windows

package registry

import (
	"context"
	"errors"
	"testing"
	"time"
)

func TestWindowsTransactionLockSerializesStores(t *testing.T) {
	root := t.TempDir()
	holder, err := NewDiskTransactionStore(root)
	if err != nil {
		t.Fatal(err)
	}
	contender, err := NewDiskTransactionStore(root)
	if err != nil {
		t.Fatal(err)
	}
	release, err := holder.lock(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	blocked, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()
	if _, err := contender.lock(blocked); !errors.Is(err, context.DeadlineExceeded) {
		release()
		t.Fatalf("contending Windows lock did not block: %v", err)
	}
	release()
	releaseNext, err := contender.lock(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	releaseNext()
}
