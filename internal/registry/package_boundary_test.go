// SPDX-License-Identifier: LicenseRef-LayerDraw-1.0

package registry

import (
	"go/parser"
	"go/token"
	"strconv"
	"strings"
	"testing"
)

func TestRegistryPackageDoesNotImportRuntimeUIOrAdapters(t *testing.T) {
	packages, err := parser.ParseDir(token.NewFileSet(), ".", nil, parser.ImportsOnly)
	if err != nil {
		t.Fatal(err)
	}
	for _, pkg := range packages {
		for name, file := range pkg.Files {
			if strings.HasSuffix(name, "_test.go") {
				continue
			}
			for _, spec := range file.Imports {
				path, err := strconv.Unquote(spec.Path.Value)
				if err != nil {
					t.Fatal(err)
				}
				for _, forbidden := range []string{"/internal/runtime", "/internal/adapter", "/packages/", "react"} {
					if strings.Contains(path, forbidden) {
						t.Fatalf("registry semantic package imports forbidden boundary %q", path)
					}
				}
			}
		}
	}
}
