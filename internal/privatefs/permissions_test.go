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
