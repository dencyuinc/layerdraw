// SPDX-License-Identifier: LicenseRef-LayerDraw-1.0

package endpoint

import (
	"go/parser"
	"go/token"
	"io/fs"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

func TestEndpointPackageBoundary(t *testing.T) {
	t.Parallel()
	forbiddenPrefixes := []string{
		"github.com/dencyuinc/layerdraw/internal/access",
		"github.com/dencyuinc/layerdraw/internal/adapter",
		"github.com/dencyuinc/layerdraw/internal/application",
		"github.com/dencyuinc/layerdraw/internal/registry",
		"github.com/dencyuinc/layerdraw/internal/runtime",
		"github.com/dencyuinc/layerdraw/internal/transport",
		"github.com/labstack/echo",
		"github.com/wailsapp",
		"modelcontextprotocol",
	}
	forbiddenExact := map[string]bool{
		"database/sql":  true,
		"io/fs":         true,
		"net":           true,
		"net/http":      true,
		"os":            true,
		"path/filepath": true,
	}
	err := filepath.WalkDir(".", func(path string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if entry.IsDir() || !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
			return nil
		}
		file, parseErr := parser.ParseFile(token.NewFileSet(), path, nil, 0)
		if parseErr != nil {
			return parseErr
		}
		for _, importSpec := range file.Imports {
			importPath, unquoteErr := strconv.Unquote(importSpec.Path.Value)
			if unquoteErr != nil {
				return unquoteErr
			}
			for _, prefix := range forbiddenPrefixes {
				if strings.HasPrefix(importPath, prefix) {
					t.Errorf("forbidden endpoint dependency in %s: %s", path, importPath)
				}
			}
			if forbiddenExact[importPath] {
				t.Errorf("filesystem, network, state, or persistence dependency in %s: %s", path, importPath)
			}
			if strings.HasSuffix(importPath, ".ts") || strings.Contains(importPath, "typescript") {
				t.Errorf("TypeScript policy dependency in %s: %s", path, importPath)
			}
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
}
