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
		ID:     "test", Title: "test", Package: "test", Module: "test", MaxJSONBytes: 1024, MaxJSONDepth: 1,
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

func TestNormalizeSchemaBytes(t *testing.T) {
	t.Parallel()
	lf := []byte("{\n}\n")
	normalized, err := normalizeSchemaBytes(lf)
	if err != nil || !bytes.Equal(normalized, lf) {
		t.Fatalf("LF input changed: %q, %v", normalized, err)
	}
	normalized, err = normalizeSchemaBytes([]byte("{\r\n}\r\n"))
	if err != nil || !bytes.Equal(normalized, lf) {
		t.Fatalf("CRLF input was not normalized: %q, %v", normalized, err)
	}
	for _, input := range [][]byte{[]byte("{\r}"), []byte("{}\r")} {
		if _, err := normalizeSchemaBytes(input); err == nil || !strings.Contains(err.Error(), "bare carriage return") {
			t.Fatalf("bare carriage return accepted in %q: %v", input, err)
		}
	}
}

func TestSchemaDocumentValidationFailures(t *testing.T) {
	t.Parallel()
	valid := func() *schemaDocument {
		return &schemaDocument{
			Schema: "https://json-schema.org/draft/2020-12/schema", ID: "test", Title: "test",
			Package: "test", Module: "test", MaxJSONBytes: 1024, MaxJSONDepth: 1,
			Definitions: map[string]*schemaType{"Record": {Type: "string"}},
		}
	}
	tests := []struct {
		name, want string
		mutate     func(*schemaDocument)
	}{
		{"schema draft", "$schema", func(document *schemaDocument) { document.Schema = "draft-07" }},
		{"identity", "$id", func(document *schemaDocument) { document.ID = "" }},
		{"byte limit", "protocol limits", func(document *schemaDocument) { document.MaxJSONBytes = 100 }},
		{"depth limit", "protocol limits", func(document *schemaDocument) { document.MaxJSONDepth = 0 }},
		{"empty definitions", "$defs", func(document *schemaDocument) { document.Definitions = nil }},
		{"definition name", "UpperCamelCase", func(document *schemaDocument) {
			document.Definitions = map[string]*schemaType{"not_upper": {Type: "string"}}
		}},
	}
	for _, test := range tests {
		test := test
		t.Run(test.name, func(t *testing.T) {
			document := valid()
			test.mutate(document)
			if err := validateDocument(document); err == nil || !strings.Contains(err.Error(), test.want) {
				t.Fatalf("invalid document was accepted: %v", err)
			}
		})
	}
}

func TestSchemaTypeValidationFailures(t *testing.T) {
	t.Parallel()
	minimum, maximum := float64(0), float64(10)
	document := &schemaDocument{ID: "test", Definitions: map[string]*schemaType{}}
	set := schemaSet{documents: []*schemaDocument{document}, byID: map[string]*schemaDocument{"test": document}}
	validObject := func() *schemaType {
		return &schemaType{
			Type: "object", Properties: map[string]*schemaType{
				"kind":    {Type: "string", Enum: []string{"a", "b"}},
				"payload": {Type: "string"},
			},
			Required: []string{"kind"}, AdditionalProperties: false,
		}
	}
	tests := []struct {
		name, want string
		value      func() *schemaType
	}{
		{"missing shape", "neither", func() *schemaType { return &schemaType{} }},
		{"non-string enum", "enum must", func() *schemaType { return &schemaType{Type: "boolean", Enum: []string{"x"}} }},
		{"unknown format", "unsupported format", func() *schemaType { return &schemaType{Type: "string", Format: "uuid"} }},
		{"integer without bounds", "explicit minimum", func() *schemaType { return &schemaType{Type: "integer"} }},
		{"fractional integer bound", "portable-safe", func() *schemaType {
			fraction := 0.5
			return &schemaType{Type: "integer", Minimum: &fraction, Maximum: &maximum}
		}},
		{"unsafe integer bound", "portable-safe", func() *schemaType {
			unsafe := float64(9007199254740992)
			return &schemaType{Type: "integer", Minimum: &minimum, Maximum: &unsafe}
		}},
		{"repeated required", "repeats required", func() *schemaType {
			value := validObject()
			value.Required = []string{"kind", "kind"}
			return value
		}},
		{"unknown required", "requires unknown", func() *schemaType {
			value := validObject()
			value.Required = []string{"missing"}
			return value
		}},
		{"open record", "open records", func() *schemaType {
			value := validObject()
			value.AdditionalProperties = true
			return value
		}},
		{"invalid additional properties", "invalid additionalProperties", func() *schemaType {
			value := validObject()
			value.AdditionalProperties = 42
			return value
		}},
		{"array without items", "array requires items", func() *schemaType { return &schemaType{Type: "array"} }},
		{"invalid tagged union", "invalid tagged union", func() *schemaType {
			value := validObject()
			value.TaggedUnion = &taggedUnion{Property: "missing", Variants: map[string]taggedVariant{"a": {}, "b": {}}}
			return value
		}},
		{"missing tagged variant", "exactly match", func() *schemaType {
			value := validObject()
			value.TaggedUnion = &taggedUnion{Property: "kind", Variants: map[string]taggedVariant{"a": {}, "b": {}, "c": {}}}
			return value
		}},
		{"unknown tagged value", "unknown tagged union value", func() *schemaType {
			value := validObject()
			value.Properties["kind"].Enum = []string{"a", "c"}
			value.TaggedUnion = &taggedUnion{Property: "kind", Variants: map[string]taggedVariant{"a": {}, "b": {}}}
			return value
		}},
		{"unknown tagged property", "unknown property", func() *schemaType {
			value := validObject()
			value.TaggedUnion = &taggedUnion{Property: "kind", Variants: map[string]taggedVariant{"a": {Required: []string{"missing"}}, "b": {}}}
			return value
		}},
		{"contradictory tagged property", "requires and forbids", func() *schemaType {
			value := validObject()
			value.TaggedUnion = &taggedUnion{Property: "kind", Variants: map[string]taggedVariant{
				"a": {Required: []string{"payload"}, Forbidden: []string{"payload"}}, "b": {},
			}}
			return value
		}},
		{"incomplete outcome metadata", "outcome envelope requires", func() *schemaType {
			value := validObject()
			value.OutcomeEnvelope = true
			return value
		}},
		{"empty tagged rule on non-array", "empty rule requires array", func() *schemaType {
			value := validObject()
			value.TaggedUnion = &taggedUnion{Property: "kind", Variants: map[string]taggedVariant{
				"a": {Empty: []string{"payload"}}, "b": {},
			}}
			return value
		}},
		{"incomplete ordered range", "ordered range requires", func() *schemaType {
			value := validObject()
			value.OrderedRange = true
			return value
		}},
	}
	for _, test := range tests {
		test := test
		t.Run(test.name, func(t *testing.T) {
			if err := validateType(set, document, "Value", test.value(), map[*schemaType]bool{}); err == nil || !strings.Contains(err.Error(), test.want) {
				t.Fatalf("invalid type was accepted: %v", err)
			}
		})
	}
	if value, err := scalarType([]any{"string", "null"}); err != nil || value != "union" {
		t.Fatalf("type union was not recognized: %q, %v", value, err)
	}
	if _, err := scalarType(42); err == nil || !strings.Contains(err.Error(), "unsupported") {
		t.Fatalf("unsupported type declaration was accepted: %v", err)
	}
}

func TestSchemaLoaderNormalizesCRLFAndRejectsAmbiguousInputs(t *testing.T) {
	t.Parallel()
	root := testRepositoryRoot(t)
	original, err := loadSchemas(root)
	if err != nil {
		t.Fatal(err)
	}
	crlfRoot := copyProtocolSchemas(t, root, func(_ string, data []byte) []byte {
		return bytes.ReplaceAll(data, []byte("\n"), []byte("\r\n"))
	})
	crlf, err := loadSchemas(crlfRoot)
	if err != nil {
		t.Fatal(err)
	}
	if original.digest != crlf.digest {
		t.Fatalf("line ending normalization changed digest: %s != %s", original.digest, crlf.digest)
	}

	t.Run("extra schema", func(t *testing.T) {
		temporary := copyProtocolSchemas(t, root, nil)
		path := filepath.Join(temporary, "schemas", "protocol-common", "extra.schema.json")
		if err := os.WriteFile(path, []byte("{}\n"), 0o644); err != nil {
			t.Fatal(err)
		}
		if _, err := loadSchemas(temporary); err == nil || !strings.Contains(err.Error(), "exactly v1.schema.json") {
			t.Fatalf("extra schema was accepted: %v", err)
		}
	})
	t.Run("trailing JSON", func(t *testing.T) {
		temporary := copyProtocolSchemas(t, root, func(group string, data []byte) []byte {
			if group == "protocol-common" {
				return append(data, []byte("{}\n")...)
			}
			return data
		})
		if _, err := loadSchemas(temporary); err == nil || !strings.Contains(err.Error(), "trailing JSON value") {
			t.Fatalf("trailing schema JSON was accepted: %v", err)
		}
	})
	t.Run("bare carriage return", func(t *testing.T) {
		temporary := copyProtocolSchemas(t, root, func(group string, data []byte) []byte {
			if group == "semantic" {
				return append(data, '\r')
			}
			return data
		})
		if _, err := loadSchemas(temporary); err == nil || !strings.Contains(err.Error(), "bare carriage return") {
			t.Fatalf("bare schema carriage return was accepted: %v", err)
		}
	})
	t.Run("unknown format", func(t *testing.T) {
		temporary := copyProtocolSchemas(t, root, func(group string, data []byte) []byte {
			if group == "protocol-common" {
				return bytes.Replace(data, []byte(`"format": "date-time"`), []byte(`"format": "uuid"`), 1)
			}
			return data
		})
		if _, err := loadSchemas(temporary); err == nil || !strings.Contains(err.Error(), "unsupported format") {
			t.Fatalf("unknown schema format was accepted: %v", err)
		}
	})
	t.Run("mismatched limits", func(t *testing.T) {
		temporary := copyProtocolSchemas(t, root, func(group string, data []byte) []byte {
			if group == "semantic" {
				return bytes.Replace(data, []byte(`"x-layerdraw-max-json-depth": 128`), []byte(`"x-layerdraw-max-json-depth": 127`), 1)
			}
			return data
		})
		if _, err := loadSchemas(temporary); err == nil || !strings.Contains(err.Error(), "identical JSON") {
			t.Fatalf("mismatched schema limits were accepted: %v", err)
		}
	})
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

func copyProtocolSchemas(t *testing.T, root string, transform func(string, []byte) []byte) string {
	t.Helper()
	temporary := t.TempDir()
	for _, group := range []string{"protocol-common", "semantic", "engine-protocol"} {
		data, err := os.ReadFile(filepath.Join(root, "schemas", group, "v1.schema.json"))
		if err != nil {
			t.Fatal(err)
		}
		if transform != nil {
			data = transform(group, data)
		}
		path := filepath.Join(temporary, "schemas", group, "v1.schema.json")
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, data, 0o644); err != nil {
			t.Fatal(err)
		}
	}
	return temporary
}
