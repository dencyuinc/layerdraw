// SPDX-License-Identifier: LicenseRef-LayerDraw-1.0

package desktopapp

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestDesktopCompositionOwnsNoServerOrLDLSemantics(t *testing.T) {
	entries, err := os.ReadDir(".")
	if err != nil {
		t.Fatal(err)
	}
	for _, entry := range entries {
		if entry.IsDir() || filepath.Ext(entry.Name()) != ".go" || strings.HasSuffix(entry.Name(), "_test.go") {
			continue
		}
		data, err := os.ReadFile(entry.Name())
		if err != nil {
			t.Fatal(err)
		}
		text := strings.ToLower(string(data))
		for _, forbidden := range []string{
			"github.com/wailsapp", "internal/application", "organization", "workspace membership",
			"realtime collaboration", "layerdraw-host",
		} {
			if strings.Contains(text, forbidden) {
				t.Fatalf("%s contains forbidden composition semantic %q", entry.Name(), forbidden)
			}
		}
	}
}
