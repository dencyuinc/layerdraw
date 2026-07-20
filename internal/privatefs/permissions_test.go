// SPDX-License-Identifier: LicenseRef-LayerDraw-1.0

package privatefs

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

func TestPermissionsMatchUsesNativeSecurityModel(t *testing.T) {
	path := filepath.Join(t.TempDir(), "private")
	if err := os.WriteFile(path, []byte("private"), 0o600); err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(path)
	if err != nil || !PermissionsMatch(info, 0o600) {
		t.Fatalf("private file rejected: info=%v err=%v", info, err)
	}
	if runtime.GOOS != "windows" && PermissionsMatch(info, 0o700) {
		t.Fatal("mismatched Unix permissions accepted")
	}
}

func TestSyncDirectoryUsesNativeDurabilityBoundary(t *testing.T) {
	directory, err := os.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer directory.Close()
	if err := SyncDirectory(directory); err != nil {
		t.Fatalf("sync directory: %v", err)
	}

	filePath := filepath.Join(t.TempDir(), "file")
	if err := os.WriteFile(filePath, []byte("data"), 0o600); err != nil {
		t.Fatal(err)
	}
	file, err := os.Open(filePath)
	if err != nil {
		t.Fatal(err)
	}
	defer file.Close()
	if runtime.GOOS == "windows" && SyncDirectory(file) == nil {
		t.Fatal("Windows accepted a non-directory handle")
	}
}
