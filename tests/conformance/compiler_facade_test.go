// SPDX-License-Identifier: Apache-2.0

package conformance_test

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"reflect"
	"slices"
	"strings"
	"testing"

	"github.com/dencyuinc/layerdraw/internal/engine"
)

func TestCompilerFacadeProjectPackAndClosureConformance(t *testing.T) {
	t.Parallel()
	packProject := conformancePackProject()
	rootPack := conformancePackProject()
	rootPack.Mode = engine.CompilePack
	rootPack.EntryPath = "pack.ldl"
	rootPack.RootPackID = "pub/schema"
	rootPack.ProjectSourceTree = map[string][]byte{}

	for _, test := range []struct {
		name       string
		input      engine.CompileInput
		wantFiles  int
		wantPack   bool
		wantTypes  int
		wantSearch bool
	}{
		{
			name: "single module Project",
			input: engine.CompileInput{Mode: engine.CompileProject, EntryPath: "document.ldl", ProjectSourceTree: map[string][]byte{
				"document.ldl": []byte(`project p "Project" {}`),
			}, ResolvedDependencies: conformanceEmptyResolved()},
			wantFiles: 1,
		},
		{
			name: "multi module Project",
			input: engine.CompileInput{Mode: engine.CompileProject, EntryPath: "document.ldl", ProjectSourceTree: map[string][]byte{
				"document.ldl": []byte("import { service } from \"./schema.ldl\"\nproject p \"Project\" {}\n"),
				"schema.ldl":   []byte("entity_type service \"Service\" {\n  representation shape rect\n}\nexport { service }\n"),
			}, ResolvedDependencies: conformanceEmptyResolved()},
			wantFiles: 2, wantTypes: 1, wantSearch: true,
		},
		{name: "installed Pack Project", input: packProject, wantFiles: 2, wantTypes: 1, wantSearch: true},
		{name: "root Pack", input: rootPack, wantFiles: 1, wantPack: true, wantTypes: 1},
	} {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			result, err := engine.New(engine.BuildInfo{}).Compile(context.Background(), test.input)
			if err != nil || len(result.Diagnostics) != 0 {
				t.Fatalf("Compile() err=%v diagnostics=%+v", err, result.Diagnostics)
			}
			snapshot := result.Snapshot()
			if len(snapshot.LosslessSyntaxTree.Files) != test.wantFiles || len(snapshot.TypedAST.EntityTypes) != test.wantTypes {
				t.Fatalf("closure files=%d types=%d", len(snapshot.LosslessSyntaxTree.Files), len(snapshot.TypedAST.EntityTypes))
			}
			if (test.wantPack && (snapshot.NormalizedPackArtifact == nil || snapshot.NormalizedDocument != nil)) ||
				(!test.wantPack && (snapshot.NormalizedDocument == nil || snapshot.NormalizedPackArtifact != nil)) {
				t.Fatalf("wrong normalized union: document=%v pack=%v", snapshot.NormalizedDocument != nil, snapshot.NormalizedPackArtifact != nil)
			}
			if test.wantSearch && len(snapshot.SearchDocuments) == 0 {
				t.Fatal("Project compile produced no SearchDocuments")
			}
		})
	}
}

func conformanceEmptyResolved() engine.ResolvedDependencies {
	return engine.ResolvedDependencies{Format: "layerdraw-resolved", FormatVersion: 1, Language: 1}
}

func TestCompilerFacadeDeterminismAndAtomicRejectionConformance(t *testing.T) {
	t.Parallel()
	input := conformancePackProject()
	first, err := engine.New(engine.BuildInfo{}).Compile(context.Background(), input)
	if err != nil {
		t.Fatal(err)
	}
	permuted := conformancePackProject()
	slices.Reverse(permuted.ResolvedDependencies.Installs[0].Files)
	second, err := engine.New(engine.BuildInfo{}).Compile(context.Background(), permuted)
	if err != nil || !reflect.DeepEqual(first.Snapshot(), second.Snapshot()) {
		t.Fatalf("byte-identical input permutation changed output: %v", err)
	}

	invalid := conformancePackProject()
	invalid.InstalledPackTree["pack/schema/pack.ldl"] = []byte("corrupt")
	rejected, err := engine.New(engine.BuildInfo{}).Compile(context.Background(), invalid)
	if err != nil || len(rejected.Diagnostics) == 0 {
		t.Fatalf("invalid closure err=%v diagnostics=%+v", err, rejected.Diagnostics)
	}
	if rejected.NormalizedDocument != nil || rejected.NormalizedPackArtifact != nil || rejected.DefinitionHash != "" || len(rejected.SemanticIndex.Subjects) != 0 || len(rejected.SearchDocuments) != 0 {
		t.Fatalf("semantic rejection published partial output: %+v", rejected.CompileOutput)
	}
}

func conformancePackProject() engine.CompileInput {
	packSource := []byte("entity_type service \"Service\" {\n  representation shape rect\n}\nexport { service }\n")
	manifest, err := json.Marshal(map[string]any{
		"format": "layerdraw-pack", "format_version": 1, "id": "pub/schema", "name": "schema",
		"version": "1.0.0", "language": 1, "entry": "pack.ldl", "dependencies": map[string]any{},
	})
	if err != nil {
		panic(err)
	}
	return engine.CompileInput{
		Mode: engine.CompileProject, EntryPath: "document.ldl",
		ProjectSourceTree: map[string][]byte{"document.ldl": []byte("import { service } from \"schema\"\nproject p \"Project\" {}\n")},
		InstalledPackTree: map[string][]byte{"pack/schema/pack.ldl": packSource},
		ResolvedDependencies: engine.ResolvedDependencies{Format: "layerdraw-resolved", FormatVersion: 1, Language: 1, Installs: []engine.ResolvedPack{{
			InstallName: "schema", CanonicalID: "pub/schema", Version: "1.0.0", Digest: "sha256:" + strings.Repeat("a", 64),
			Path: "pack/schema", Entry: "pack.ldl", ManifestPath: "manifest.json", Manifest: manifest,
			Files: []engine.ResolvedPackFile{{Path: "pack.ldl", Digest: conformanceDigest(packSource)}},
		}}},
	}
}

func conformanceDigest(value []byte) string {
	digest := sha256.Sum256(value)
	return "sha256:" + hex.EncodeToString(digest[:])
}
