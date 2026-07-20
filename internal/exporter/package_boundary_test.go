// SPDX-License-Identifier: LicenseRef-LayerDraw-1.0

package exporter

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestExporterDoesNotOwnRuntimeStorageOrCompilerSemantics(t *testing.T) {
	entries, err := os.ReadDir(".")
	if err != nil {
		t.Fatal(err)
	}
	for _, entry := range entries {
		if entry.IsDir() || filepath.Ext(entry.Name()) != ".go" || strings.HasSuffix(entry.Name(), "_test.go") {
			continue
		}
		value, err := os.ReadFile(entry.Name())
		if err != nil {
			t.Fatal(err)
		}
		for _, forbidden := range []string{"internal/runtime", "internal/localdocument", "internal/engine/internal", "github.com/wailsapp"} {
			if strings.Contains(string(value), forbidden) {
				t.Fatalf("%s imports forbidden owner %q", entry.Name(), forbidden)
			}
		}
	}
}
