// SPDX-License-Identifier: LicenseRef-LayerDraw-1.0

package main

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestGenerationIsDeterministicAndCommitted(t *testing.T) {
	t.Parallel()
	root := testRepositoryRoot(t)
	firstSet, err := loadSchemas(root)
	if err != nil {
		t.Fatal(err)
	}
	secondSet, err := loadSchemas(root)
	if err != nil {
		t.Fatal(err)
	}
	first, err := generate(firstSet)
	if err != nil {
		t.Fatal(err)
	}
	second, err := generate(secondSet)
	if err != nil {
		t.Fatal(err)
	}
	if len(first) != len(second) {
		t.Fatalf("generation count changed: %d != %d", len(first), len(second))
	}
	for index := range first {
		if first[index].path != second[index].path || !bytes.Equal(first[index].data, second[index].data) {
			t.Fatalf("repeated generation changed %s", first[index].path)
		}
		committed, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(first[index].path)))
		if err != nil {
			t.Fatal(err)
		}
		if !bytes.Equal(first[index].data, committed) {
			t.Fatalf("stale generated file %s; run make generate", first[index].path)
		}
	}
}

func TestSchemaValidationRejectsUnsafeShapes(t *testing.T) {
	t.Parallel()
	document := &schemaDocument{
		Schema: "https://json-schema.org/draft/2020-12/schema",
		ID:     "test", Title: "test", Package: "test", Module: "test",
		Definitions: map[string]*schemaType{"Record": {
			Type: "object",
			Properties: map[string]*schemaType{
				"badField": {Type: "string"},
			},
			Required: []string{"badField"}, AdditionalProperties: false,
		}},
	}
	if err := validateDocument(document); err != nil {
		t.Fatal(err)
	}
	set := schemaSet{documents: []*schemaDocument{document}, byID: map[string]*schemaDocument{"test": document}}
	if err := validateType(set, document, "Record", document.Definitions["Record"], map[*schemaType]bool{}); err == nil || !strings.Contains(err.Error(), "lower_snake_case") {
		t.Fatalf("mixed-case field was not rejected: %v", err)
	}

	document.Definitions["Record"] = &schemaType{
		Type: "object", Properties: map[string]*schemaType{"ok": {Type: "string"}}, Required: []string{"ok"},
	}
	if err := validateType(set, document, "Record", document.Definitions["Record"], map[*schemaType]bool{}); err == nil || !strings.Contains(err.Error(), "additionalProperties explicitly") {
		t.Fatalf("implicit extension behavior was not rejected: %v", err)
	}

	document.Definitions["Record"] = &schemaType{Ref: "missing#/$defs/Nope"}
	if err := validateType(set, document, "Record", document.Definitions["Record"], map[*schemaType]bool{}); err == nil || !strings.Contains(err.Error(), "unknown schema") {
		t.Fatalf("broken reference was not rejected: %v", err)
	}
}

func TestSchemaDigestChangesWithSchemaBytes(t *testing.T) {
	t.Parallel()
	root := testRepositoryRoot(t)
	copyRoot := t.TempDir()
	for _, directory := range []string{"protocol-common", "semantic", "engine-protocol"} {
		source := filepath.Join(root, "schemas", directory, "v1.schema.json")
		data, err := os.ReadFile(source)
		if err != nil {
			t.Fatal(err)
		}
		target := filepath.Join(copyRoot, "schemas", directory, "v1.schema.json")
		if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
			t.Fatal(err)
		}
		if directory == "engine-protocol" {
			data = bytes.Replace(data, []byte("LayerDraw Engine Protocol v1"), []byte("LayerDraw Engine Protocol version one"), 1)
		}
		if err := os.WriteFile(target, data, 0o644); err != nil {
			t.Fatal(err)
		}
	}
	original, err := loadSchemas(root)
	if err != nil {
		t.Fatal(err)
	}
	changed, err := loadSchemas(copyRoot)
	if err != nil {
		t.Fatal(err)
	}
	if original.digest == changed.digest {
		t.Fatal("aggregate digest did not change with schema bytes")
	}
}

func TestImportedSchemaChangeUpdatesDependentGroupDigests(t *testing.T) {
	t.Parallel()
	root := testRepositoryRoot(t)
	copyRoot := t.TempDir()
	for _, directory := range []string{"protocol-common", "semantic", "engine-protocol"} {
		data, err := os.ReadFile(filepath.Join(root, "schemas", directory, "v1.schema.json"))
		if err != nil {
			t.Fatal(err)
		}
		if directory == "protocol-common" {
			data = bytes.Replace(data, []byte("LayerDraw Protocol Common v1"), []byte("LayerDraw Protocol Common version one"), 1)
		}
		target := filepath.Join(copyRoot, "schemas", directory, "v1.schema.json")
		if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(target, data, 0o644); err != nil {
			t.Fatal(err)
		}
	}
	original, err := loadSchemas(root)
	if err != nil {
		t.Fatal(err)
	}
	changed, err := loadSchemas(copyRoot)
	if err != nil {
		t.Fatal(err)
	}
	for index, document := range original.documents {
		if document.digest == changed.documents[index].digest {
			t.Errorf("import-closure digest for %s did not change", document.Module)
		}
	}
}

func TestRunGeneratesEveryOutputAndRemovesOrphans(t *testing.T) {
	t.Parallel()
	root := testRepositoryRoot(t)
	temporary := t.TempDir()
	for _, directory := range []string{"protocol-common", "semantic", "engine-protocol"} {
		data, err := os.ReadFile(filepath.Join(root, "schemas", directory, "v1.schema.json"))
		if err != nil {
			t.Fatal(err)
		}
		target := filepath.Join(temporary, "schemas", directory, "v1.schema.json")
		if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(target, data, 0o644); err != nil {
			t.Fatal(err)
		}
	}
	orphanGo := filepath.Join(temporary, "gen", "go", "orphan", "orphan.gen.go")
	orphanTS := filepath.Join(temporary, "packages", "protocol", "src", "orphan.gen.ts")
	for _, orphan := range []string{orphanGo, orphanTS} {
		if err := os.MkdirAll(filepath.Dir(orphan), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(orphan, []byte("stale"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	if err := run([]string{"-root", temporary, "generate"}); err != nil {
		t.Fatal(err)
	}
	for _, path := range []string{
		"gen/go/protocolcommon/types.gen.go", "gen/go/protocolcommon/codec.gen.go",
		"gen/go/semantic/types.gen.go", "gen/go/semantic/codec.gen.go",
		"gen/go/engineprotocol/types.gen.go", "gen/go/engineprotocol/codec.gen.go",
		"packages/protocol/src/common.gen.ts", "packages/protocol/src/semantic.gen.ts",
		"packages/protocol/src/engine.gen.ts", "gen/schema-digests.json",
	} {
		if _, err := os.Stat(filepath.Join(temporary, filepath.FromSlash(path))); err != nil {
			t.Errorf("missing %s: %v", path, err)
		}
	}
	for _, orphan := range []string{orphanGo, orphanTS} {
		if _, err := os.Stat(orphan); !os.IsNotExist(err) {
			t.Errorf("orphan was not removed: %s", orphan)
		}
	}
	if err := run([]string{"invalid"}); err == nil || !strings.Contains(err.Error(), "usage") {
		t.Fatalf("invalid command accepted: %v", err)
	}
}

func testRepositoryRoot(t *testing.T) string {
	t.Helper()
	root, err := filepath.Abs(filepath.Join("..", ".."))
	if err != nil {
		t.Fatal(err)
	}
	return root
}
