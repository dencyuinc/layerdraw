// SPDX-License-Identifier: LicenseRef-LayerDraw-1.0

package wasm

import (
	"go/parser"
	"go/token"
	"io/fs"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

func TestWASMTransportPackageBoundary(t *testing.T) {
	t.Parallel()
	allowedLocal := map[string]bool{
		"github.com/dencyuinc/layerdraw/gen/go/engineprotocol":    true,
		"github.com/dencyuinc/layerdraw/internal/engine/endpoint": true,
	}
	forbiddenExact := map[string]bool{
		"database/sql": true,
		"net":          true,
		"net/http":     true,
		"os":           true,
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
		for _, spec := range file.Imports {
			importPath, unquoteErr := strconv.Unquote(spec.Path.Value)
			if unquoteErr != nil {
				return unquoteErr
			}
			if strings.HasPrefix(importPath, "github.com/dencyuinc/layerdraw/") && !allowedLocal[importPath] {
				t.Errorf("transport dependency escapes generated/endpoint boundary in %s: %s", path, importPath)
			}
			if forbiddenExact[importPath] || strings.HasPrefix(importPath, "github.com/dencyuinc/layerdraw/internal/engine/internal/") {
				t.Errorf("state, network, or compiler-stage dependency in %s: %s", path, importPath)
			}
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
}
