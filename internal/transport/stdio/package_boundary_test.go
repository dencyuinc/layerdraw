// SPDX-License-Identifier: LicenseRef-LayerDraw-1.0

package stdio

import (
	"go/parser"
	"go/token"
	"io/fs"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

func TestFramingPackageBoundary(t *testing.T) {
	t.Parallel()
	allowed := map[string]bool{
		"github.com/dencyuinc/layerdraw/gen/go/engineprotocol":    true,
		"github.com/dencyuinc/layerdraw/internal/engine/endpoint": true,
	}
	err := filepath.WalkDir(".", func(path string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if entry.IsDir() || !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
			return nil
		}
		file, parseErr := parser.ParseFile(token.NewFileSet(), path, nil, parser.ImportsOnly)
		if parseErr != nil {
			return parseErr
		}
		for _, importSpec := range file.Imports {
			importPath, unquoteErr := strconv.Unquote(importSpec.Path.Value)
			if unquoteErr != nil {
				return unquoteErr
			}
			if allowed[importPath] {
				continue
			}
			firstSegment := strings.Split(importPath, "/")[0]
			if strings.Contains(firstSegment, ".") {
				t.Errorf("framing package has non-standard dependency in %s: %s", path, importPath)
			}
			if strings.HasPrefix(importPath, "github.com/dencyuinc/layerdraw/internal/") {
				t.Errorf("framing package crosses an internal component boundary in %s: %s", path, importPath)
			}
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
}
