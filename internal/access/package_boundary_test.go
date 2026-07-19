// SPDX-License-Identifier: LicenseRef-LayerDraw-1.0

package access

import (
	"go/parser"
	"go/token"
	"io/fs"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

func TestAccessHasNoEngineRuntimeTransportOrFrameworkImports(t *testing.T) {
	forbidden := []string{
		"github.com/dencyuinc/layerdraw/internal/engine",
		"github.com/dencyuinc/layerdraw/internal/runtime",
		"github.com/dencyuinc/layerdraw/internal/transport",
		"github.com/wailsapp",
		"modelcontextprotocol",
	}
	err := filepath.WalkDir(".", func(path string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil || entry.IsDir() || !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
			return walkErr
		}
		file, err := parser.ParseFile(token.NewFileSet(), path, nil, parser.ImportsOnly)
		if err != nil {
			return err
		}
		for _, spec := range file.Imports {
			importPath, err := strconv.Unquote(spec.Path.Value)
			if err != nil {
				return err
			}
			for _, prefix := range forbidden {
				if strings.HasPrefix(importPath, prefix) {
					t.Errorf("forbidden Access import in %s: %s", path, importPath)
				}
			}
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
}
