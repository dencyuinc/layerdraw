// SPDX-License-Identifier: LicenseRef-LayerDraw-1.0

package engine

import (
	"go/ast"
	"go/parser"
	"go/token"
	"io/fs"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

func TestEngineCompilerPackageBoundary(t *testing.T) {
	t.Parallel()
	forbidden := []string{
		"github.com/dencyuinc/layerdraw/internal/runtime",
		"github.com/dencyuinc/layerdraw/internal/access",
		"github.com/dencyuinc/layerdraw/internal/registry",
		"github.com/dencyuinc/layerdraw/internal/adapter",
		"github.com/dencyuinc/layerdraw/internal/application",
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
			for _, prefix := range forbidden {
				if strings.HasPrefix(importPath, prefix) {
					t.Errorf("forbidden Engine/compiler import in %s: %s", path, importPath)
				}
			}
			if forbiddenExact[importPath] {
				t.Errorf("filesystem, network, or state dependency in %s: %s", path, importPath)
			}
			if strings.HasSuffix(importPath, ".ts") || strings.Contains(importPath, "typescript") {
				t.Errorf("TypeScript dependency in %s: %s", path, importPath)
			}
			allowedLocalImport := strings.HasPrefix(importPath, "github.com/dencyuinc/layerdraw/internal/engine/internal/compiler") ||
				strings.HasPrefix(importPath, "github.com/dencyuinc/layerdraw/gen/go/") ||
				(strings.HasPrefix(path, "endpoint/") && importPath == "github.com/dencyuinc/layerdraw/internal/engine")
			if strings.HasPrefix(importPath, "github.com/dencyuinc/layerdraw/") && !allowedLocalImport {
				t.Errorf("Engine/compiler dependency escapes its component boundary in %s: %s", path, importPath)
			}
			firstSegment := strings.Split(importPath, "/")[0]
			if strings.Contains(firstSegment, ".") &&
				!strings.HasPrefix(importPath, "github.com/dencyuinc/layerdraw/") &&
				!strings.HasPrefix(importPath, "golang.org/x/text/") &&
				!strings.HasPrefix(importPath, "golang.org/x/image/") {
				t.Errorf("unapproved framework or provider dependency in %s: %s", path, importPath)
			}
		}
		ast.Inspect(file, func(node ast.Node) bool {
			if _, ok := node.(*ast.GoStmt); ok {
				t.Errorf("Engine/compiler must not create goroutines in %s", path)
			}
			return true
		})
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
}

func TestGeneratedProtocolToEngineMappingHasOneHandwrittenPackageBoundary(t *testing.T) {
	t.Parallel()
	type imports struct {
		generatedProtocol bool
		internalEngine    bool
	}
	byDirectory := map[string]*imports{}
	root := filepath.Clean("../..")
	err := filepath.WalkDir(root, func(path string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if entry.IsDir() {
			switch entry.Name() {
			case ".git", "node_modules", "vendor":
				return filepath.SkipDir
			}
			return nil
		}
		if !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
			return nil
		}
		file, parseErr := parser.ParseFile(token.NewFileSet(), path, nil, parser.ImportsOnly)
		if parseErr != nil {
			return parseErr
		}
		directory := filepath.Dir(path)
		seen := byDirectory[directory]
		if seen == nil {
			seen = &imports{}
			byDirectory[directory] = seen
		}
		for _, importSpec := range file.Imports {
			importPath, unquoteErr := strconv.Unquote(importSpec.Path.Value)
			if unquoteErr != nil {
				return unquoteErr
			}
			if strings.HasPrefix(importPath, "github.com/dencyuinc/layerdraw/gen/go/") {
				seen.generatedProtocol = true
			}
			if importPath == "github.com/dencyuinc/layerdraw/internal/engine" ||
				strings.HasPrefix(importPath, "github.com/dencyuinc/layerdraw/internal/engine/internal/") {
				seen.internalEngine = true
			}
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	allowed := map[string]bool{
		filepath.Join(root, "internal", "engine", "endpoint"): true,
		// The stdio transport decodes generated envelopes but may invoke only
		// the public endpoint/CompilePlan facade, never Engine or compiler
		// domain packages directly. Its own package-boundary test freezes that
		// narrower exception.
		filepath.Join(root, "internal", "transport", "stdio"): true,
	}
	for directory, seen := range byDirectory {
		if seen.generatedProtocol && seen.internalEngine && !allowed[directory] {
			t.Errorf("generated protocol and internal Engine types meet outside endpoint boundary: %s", directory)
		}
	}
}
