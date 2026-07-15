// SPDX-License-Identifier: LicenseRef-LayerDraw-1.0

package main

import (
	"bytes"
	"encoding/json"
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
		Schema: schemaDialectID,
		ID:     "test", Title: "test", Package: "test", Module: "test", MaxJSONBytes: 1024, MaxJSONDepth: 1,
		ScalarUnicode: true,
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

func TestStableAddressRoleJSONOmitsInactiveSelectors(t *testing.T) {
	t.Parallel()
	data, err := json.Marshal(stableAddressRoleRule{
		Kind:        "child_kind",
		Addresses:   "child_addresses",
		Owner:       "owner_address",
		OwnerPolicy: "children",
	})
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(data), `"address":""`) {
		t.Fatalf("inactive singular selector leaked into generated authority: %s", data)
	}
}

func TestRejectDuplicateJSONObjectKeys(t *testing.T) {
	t.Parallel()
	for _, input := range []string{`null`, `true`, `"text"`, `123`, `[{"a":1},[false]]`} {
		if err := rejectDuplicateJSONObjectKeys([]byte(input)); err != nil {
			t.Errorf("unambiguous JSON %s was rejected: %v", input, err)
		}
	}
	for _, test := range []struct {
		input, want string
	}{
		{`{"a":1,"a":2}`, "duplicate JSON object key"},
		{`{"a":1,"\u0061":2}`, "duplicate JSON object key"},
		{`{"a":`, "EOF"},
		{`[1`, "EOF"},
		{`[{"a":`, "EOF"},
	} {
		if err := rejectDuplicateJSONObjectKeys([]byte(test.input)); err == nil || !strings.Contains(err.Error(), test.want) {
			t.Errorf("ambiguous or malformed JSON %s returned %v, want %q", test.input, err, test.want)
		}
	}
}

func TestSchemaDocumentValidationFailures(t *testing.T) {
	t.Parallel()
	valid := func() *schemaDocument {
		return &schemaDocument{
			Schema: schemaDialectID, ID: "test", Title: "test",
			Package: "test", Module: "test", MaxJSONBytes: 1024, MaxJSONDepth: 1,
			ScalarUnicode: true,
			Definitions:   map[string]*schemaType{"Record": {Type: "string"}},
		}
	}
	tests := []struct {
		name, want string
		mutate     func(*schemaDocument)
	}{
		{"schema dialect", "$schema", func(document *schemaDocument) { document.Schema = "draft-07" }},
		{"identity", "$id", func(document *schemaDocument) { document.ID = "" }},
		{"byte limit", "protocol limits", func(document *schemaDocument) { document.MaxJSONBytes = 100 }},
		{"depth limit", "protocol limits", func(document *schemaDocument) { document.MaxJSONDepth = 0 }},
		{"scalar Unicode assertion", "DIALECT_SCHEMA_SCALAR_UNICODE", func(document *schemaDocument) { document.ScalarUnicode = false }},
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
		{"unsupported oneOf", "unsupported oneOf", func() *schemaType { return &schemaType{OneOf: []*schemaType{{Type: "string"}, {Type: "boolean"}}} }},
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
		{"non-string property names", "propertyNames must validate strings", func() *schemaType {
			return &schemaType{Type: "object", PropertyNames: &schemaType{Type: "boolean"}, AdditionalProperties: &schemaType{Type: "string"}}
		}},
		{"array without items", "array requires items", func() *schemaType { return &schemaType{Type: "array"} }},
		{"stable address order on non-strings", "selector must resolve to strings", func() *schemaType {
			return &schemaType{Type: "array", Items: &schemaType{Type: "boolean"}, StableAddressOrder: "$item"}
		}},
		{"stable address order missing property", "does not name an item property", func() *schemaType {
			return &schemaType{Type: "array", Items: validObject(), StableAddressOrder: "address"}
		}},
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
		{"contradictory tagged property", "contradictory rules", func() *schemaType {
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
		{"empty tagged rule on non-array", "empty/non_empty rule requires array", func() *schemaType {
			value := validObject()
			value.TaggedUnion = &taggedUnion{Property: "kind", Variants: map[string]taggedVariant{
				"a": {Empty: []string{"payload"}}, "b": {},
			}}
			return value
		}},
		{"non-empty tagged rule on non-array", "empty/non_empty rule requires array", func() *schemaType {
			value := validObject()
			value.TaggedUnion = &taggedUnion{Property: "kind", Variants: map[string]taggedVariant{
				"a": {NonEmpty: []string{"payload"}}, "b": {},
			}}
			return value
		}},
		{"tagged allowed values on non-enum", "allowed_values requires string-enum", func() *schemaType {
			value := validObject()
			value.TaggedUnion = &taggedUnion{Property: "kind", Variants: map[string]taggedVariant{
				"a": {AllowedValues: map[string][]string{"payload": {"x"}}}, "b": {},
			}}
			return value
		}},
		{"tagged unknown allowed value", "invalid allowed value", func() *schemaType {
			value := validObject()
			value.Properties["payload"].Enum = []string{"x", "y"}
			value.TaggedUnion = &taggedUnion{Property: "kind", Variants: map[string]taggedVariant{
				"a": {AllowedValues: map[string][]string{"payload": {"z"}}}, "b": {},
			}}
			return value
		}},
		{"tagged allowed forbidden property", "invalid allowed_values", func() *schemaType {
			value := validObject()
			value.Properties["payload"].Enum = []string{"x", "y"}
			value.TaggedUnion = &taggedUnion{Property: "kind", Variants: map[string]taggedVariant{
				"a": {Forbidden: []string{"payload"}, AllowedValues: map[string][]string{"payload": {"x"}}}, "b": {},
			}}
			return value
		}},
		{"diff source missing shape", "diff source assertion requires before", func() *schemaType {
			value := validObject()
			value.DiffSource = true
			return value
		}},
		{"diff source invalid shape", "invalid diff source assertion shape", func() *schemaType {
			one := 1
			return &schemaType{
				Type: "object", Properties: map[string]*schemaType{
					"kind": {Type: "string", Enum: []string{"diff", "query"}}, "before": {Type: "string"}, "after": {Type: "string", MinLength: &one},
					"query_address": {Type: "string"}, "arguments": {Type: "object", AdditionalProperties: &schemaType{Type: "string"}},
				}, Required: []string{"kind", "arguments"}, AdditionalProperties: false, DiffSource: true,
			}
		}},
		{"operator rule missing properties", "invalid operator/value", func() *schemaType {
			value := validObject()
			value.OperatorValue = &operatorValueRule{Operator: "operator", Value: "value", Valueless: []string{"missing"}}
			return value
		}},
		{"protocol offer metadata missing fields", "protocol offer requires", func() *schemaType {
			value := validObject()
			value.ProtocolOffer = true
			return value
		}},
		{"limit metadata missing fields", "limit capability requires", func() *schemaType {
			value := validObject()
			value.LimitCapability = true
			return value
		}},
		{"unique key metadata missing array", "invalid unique array key", func() *schemaType {
			value := validObject()
			value.UniqueArrayKeys = []uniqueArrayKey{{Array: "missing", Property: "id"}}
			return value
		}},
		{"incomplete ordered range", "ordered range requires", func() *schemaType {
			value := validObject()
			value.OrderedRange = true
			return value
		}},
		{"invalid disjoint array", "invalid disjoint array", func() *schemaType {
			value := validObject()
			value.DisjointArrays = []disjointArrayPair{{Left: "kind", Right: "kind"}}
			return value
		}},
		{"disjoint rule missing array", "names non-array property", func() *schemaType {
			value := validObject()
			value.DisjointArrays = []disjointArrayPair{{Left: "missing", Right: "payload"}}
			return value
		}},
		{"disjoint rule non-string array", "requires string-array property", func() *schemaType {
			value := validObject()
			value.Properties["left"] = &schemaType{Type: "array", Items: &schemaType{Type: "boolean"}}
			value.Properties["right"] = &schemaType{Type: "array", Items: &schemaType{Type: "string"}}
			value.DisjointArrays = []disjointArrayPair{{Left: "left", Right: "right"}}
			return value
		}},
		{"canonical identifier order without unique strings", "canonical identifier order requires string items and uniqueItems", func() *schemaType {
			return &schemaType{Type: "array", Items: &schemaType{Type: "string"}, CanonicalIDOrder: true}
		}},
		{"canonical enum order without unique enum strings", "canonical enum order requires string-enum items and uniqueItems", func() *schemaType {
			return &schemaType{Type: "array", Items: &schemaType{Type: "string", Enum: []string{"a", "b"}}, CanonicalEnumOrder: true}
		}},
		{"Unicode scalar order without unique strings", "Unicode scalar order requires string items and uniqueItems", func() *schemaType {
			return &schemaType{Type: "array", Items: &schemaType{Type: "string"}, UnicodeScalarOrder: true}
		}},
		{"ordered pair missing property", "invalid ordered-pair rule", func() *schemaType {
			value := validObject()
			value.OrderedPairs = []orderedPairRule{{Lower: "missing", Upper: "payload", Comparison: "unsigned_decimal"}}
			return value
		}},
		{"ordered pair wrong unsigned formats", "requires canonical unsigned-decimal formats", func() *schemaType {
			value := validObject()
			value.OrderedPairs = []orderedPairRule{{Lower: "kind", Upper: "payload", Comparison: "unsigned_decimal"}}
			return value
		}},
		{"ordered pair unknown comparison", "unknown comparison", func() *schemaType {
			value := validObject()
			value.OrderedPairs = []orderedPairRule{{Lower: "kind", Upper: "payload", Comparison: "other"}}
			return value
		}},
		{"disjoint array-key missing arrays", "invalid disjoint array-key rule", func() *schemaType {
			value := validObject()
			value.DisjointArrayKeys = []disjointArrayKey{{Array: "missing", Property: "id", Strings: "also_missing"}}
			return value
		}},
		{"address terminal ID missing property", "invalid address terminal-ID rule", func() *schemaType {
			value := validObject()
			value.AddressTerminalID = &addressTerminalIDRule{Address: "missing", ID: "kind"}
			return value
		}},
		{"export recipe assertion missing fields", "export recipe assertion requires exporter_profile", func() *schemaType {
			value := validObject()
			value.ExportRecipe = true
			return value
		}},
		{"view recipe assertion missing fields", "view recipe assertion requires address", func() *schemaType {
			value := validObject()
			value.ViewRecipe = true
			return value
		}},
		{"address owner missing property", "invalid address-owner rule", func() *schemaType {
			value := validObject()
			value.AddressOwners = []addressOwnerRule{{Owner: "missing", Children: "payload", Selector: "$value"}}
			return value
		}},
		{"address owner is not a string", "must be a string property", func() *schemaType {
			value := validObject()
			value.Properties["owner"] = &schemaType{Type: "boolean"}
			value.AddressOwners = []addressOwnerRule{{Owner: "owner", Children: "payload", Selector: "$value"}}
			return value
		}},
		{"address owner value child is not a string", "$value rule requires string children", func() *schemaType {
			value := validObject()
			value.Properties["child"] = &schemaType{Type: "boolean"}
			value.AddressOwners = []addressOwnerRule{{Owner: "payload", Children: "child", Selector: "$value"}}
			return value
		}},
		{"address owner property names missing", "$propertyNames rule requires an object with propertyNames", func() *schemaType {
			value := validObject()
			value.Properties["children"] = &schemaType{Type: "object", AdditionalProperties: &schemaType{Type: "string"}}
			value.AddressOwners = []addressOwnerRule{{Owner: "payload", Children: "children", Selector: "$propertyNames"}}
			return value
		}},
		{"address owner selector missing", "requires selector", func() *schemaType {
			value := validObject()
			value.AddressOwners = []addressOwnerRule{{Owner: "payload", Children: "kind"}}
			return value
		}},
		{"address owner selector missing from array item", "must name a string item property", func() *schemaType {
			value := validObject()
			value.Properties["children"] = &schemaType{Type: "array", Items: validObject()}
			value.AddressOwners = []addressOwnerRule{{Owner: "payload", Children: "children", Selector: "address"}}
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
	booleanUnion := &schemaType{
		Type: "object", Properties: map[string]*schemaType{"enabled": {Type: "boolean"}, "reason": {Type: "string"}},
		Required: []string{"enabled"}, AdditionalProperties: false,
		TaggedUnion: &taggedUnion{Property: "enabled", Variants: map[string]taggedVariant{"false": {Required: []string{"reason"}}, "true": {Forbidden: []string{"reason"}}}},
	}
	if err := validateType(set, document, "BooleanUnion", booleanUnion, map[*schemaType]bool{}); err != nil {
		t.Fatalf("valid boolean tagged union rejected: %v", err)
	}
	document.Definitions["PayloadKind"] = &schemaType{Type: "string", Enum: []string{"x", "y"}}
	allowedRefUnion := validObject()
	allowedRefUnion.Properties["payload"] = &schemaType{Ref: "test#/$defs/PayloadKind"}
	allowedRefUnion.TaggedUnion = &taggedUnion{Property: "kind", Variants: map[string]taggedVariant{
		"a": {Required: []string{"payload"}, AllowedValues: map[string][]string{"payload": {"x"}}}, "b": {Forbidden: []string{"payload"}},
	}}
	if err := validateType(set, document, "AllowedRefUnion", allowedRefUnion, map[*schemaType]bool{}); err != nil {
		t.Fatalf("valid tagged allowed_values reference rejected: %v", err)
	}
	one := 1
	document.Definitions["DiffKind"] = &schemaType{Type: "string", Enum: []string{"diff", "query"}}
	diffSource := &schemaType{
		Type: "object", Properties: map[string]*schemaType{
			"kind": {Ref: "test#/$defs/DiffKind"}, "before": {Type: "string", MinLength: &one}, "after": {Type: "string", MinLength: &one},
			"query_address": {Type: "string"}, "arguments": {Type: "object", AdditionalProperties: &schemaType{Type: "string"}},
		}, Required: []string{"kind", "arguments"}, AdditionalProperties: false, DiffSource: true,
	}
	if err := validateType(set, document, "DiffSource", diffSource, map[*schemaType]bool{}); err != nil {
		t.Fatalf("valid diff source assertion rejected: %v", err)
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
	t.Run("missing schema", func(t *testing.T) {
		temporary := copyProtocolSchemas(t, root, nil)
		if err := os.Remove(filepath.Join(temporary, "schemas", "engine-protocol", "v1.schema.json")); err != nil {
			t.Fatal(err)
		}
		if _, err := loadSchemas(temporary); err == nil || !strings.Contains(err.Error(), "exactly v1.schema.json") {
			t.Fatalf("missing schema was accepted: %v", err)
		}
	})
	t.Run("malformed schema", func(t *testing.T) {
		temporary := copyProtocolSchemas(t, root, func(group string, data []byte) []byte {
			if group == "protocol-common" {
				return []byte("{")
			}
			return data
		})
		if _, err := loadSchemas(temporary); err == nil || !strings.Contains(err.Error(), "decode") {
			t.Fatalf("malformed schema was accepted: %v", err)
		}
	})
	t.Run("unexpected schema identity", func(t *testing.T) {
		temporary := copyProtocolSchemas(t, root, func(group string, data []byte) []byte {
			if group == "protocol-common" {
				return bytes.Replace(data, []byte(`https://schemas.layerdraw.dev/protocol-common/v1`), []byte(`https://schemas.layerdraw.dev/protocol-common/v2`), 1)
			}
			return data
		})
		if _, err := loadSchemas(temporary); err == nil || !strings.Contains(err.Error(), "unexpected group identity") {
			t.Fatalf("unexpected schema identity was accepted: %v", err)
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
	for _, test := range []struct {
		name, group      string
		old, replacement []byte
	}{
		{"duplicate top metadata", "protocol-common", []byte(`"$id":`), []byte(`"$id":"shadowed","\u0024id":`)},
		{"duplicate definitions", "semantic", []byte(`"$defs": {`), []byte(`"$defs": {}, "\u0024defs": {`)},
		{"duplicate nested properties", "engine-protocol", []byte(`"properties": {`), []byte(`"properties": {}, "propert\u0069es": {`)},
	} {
		test := test
		t.Run(test.name, func(t *testing.T) {
			temporary := copyProtocolSchemas(t, root, func(group string, data []byte) []byte {
				if group == test.group {
					return bytes.Replace(data, test.old, test.replacement, 1)
				}
				return data
			})
			if _, err := loadSchemas(temporary); err == nil || !strings.Contains(err.Error(), "duplicate JSON object key") {
				t.Fatalf("duplicate or escaped-equivalent schema key was accepted: %v", err)
			}
		})
	}
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
	t.Run("extra dialect", func(t *testing.T) {
		temporary := copyProtocolSchemas(t, root, nil)
		if err := os.WriteFile(filepath.Join(temporary, "schemas", "meta", "extra.json"), []byte("{}\n"), 0o644); err != nil {
			t.Fatal(err)
		}
		if _, err := loadSchemas(temporary); err == nil || !strings.Contains(err.Error(), "exactly layerdraw-protocol") {
			t.Fatalf("extra dialect schema was accepted: %v", err)
		}
	})
	t.Run("dialect trailing JSON", func(t *testing.T) {
		temporary := copyProtocolSchemas(t, root, nil)
		path := filepath.Join(temporary, filepath.FromSlash(schemaDialectPath))
		data, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, append(data, []byte("{}\n")...), 0o644); err != nil {
			t.Fatal(err)
		}
		if _, err := loadSchemas(temporary); err == nil || !strings.Contains(err.Error(), "trailing JSON value") {
			t.Fatalf("trailing dialect JSON was accepted: %v", err)
		}
	})
	t.Run("malformed dialect", func(t *testing.T) {
		temporary := copyProtocolSchemas(t, root, nil)
		path := filepath.Join(temporary, filepath.FromSlash(schemaDialectPath))
		if err := os.WriteFile(path, []byte("{"), 0o644); err != nil {
			t.Fatal(err)
		}
		if _, err := loadSchemas(temporary); err == nil || !strings.Contains(err.Error(), "decode") {
			t.Fatalf("malformed dialect was accepted: %v", err)
		}
	})
	t.Run("dialect identity", func(t *testing.T) {
		temporary := copyProtocolSchemas(t, root, nil)
		path := filepath.Join(temporary, filepath.FromSlash(schemaDialectPath))
		data, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		data = bytes.Replace(data, []byte(schemaDialectID), []byte("https://schemas.layerdraw.dev/meta/protocol/v2"), 1)
		if err := os.WriteFile(path, data, 0o644); err != nil {
			t.Fatal(err)
		}
		if _, err := loadSchemas(temporary); err == nil || !strings.HasPrefix(err.Error(), dialectIdentityCode+":") {
			t.Fatalf("unexpected dialect identity was accepted: %v", err)
		}
	})
	t.Run("dialect vocabulary", func(t *testing.T) {
		temporary := copyProtocolSchemas(t, root, nil)
		path := filepath.Join(temporary, filepath.FromSlash(schemaDialectPath))
		data, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		data = bytes.Replace(data, []byte(`"https://schemas.layerdraw.dev/vocab/protocol/v1": true`), []byte(`"https://schemas.layerdraw.dev/vocab/protocol/v1": false`), 1)
		if err := os.WriteFile(path, data, 0o644); err != nil {
			t.Fatal(err)
		}
		if _, err := loadSchemas(temporary); err == nil || !strings.HasPrefix(err.Error(), dialectVocabularyValueCode+":") {
			t.Fatalf("optional dialect vocabulary was accepted: %v", err)
		}
	})
	t.Run("dialect format assertion vocabulary", func(t *testing.T) {
		temporary := copyProtocolSchemas(t, root, nil)
		path := filepath.Join(temporary, filepath.FromSlash(schemaDialectPath))
		data, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		data = bytes.Replace(data, []byte(`"https://json-schema.org/draft/2020-12/vocab/format-assertion": true`), []byte(`"https://json-schema.org/draft/2020-12/vocab/format-assertion": false`), 1)
		if err := os.WriteFile(path, data, 0o644); err != nil {
			t.Fatal(err)
		}
		if _, err := loadSchemas(temporary); err == nil || !strings.Contains(err.Error(), formatAssertionVocabulary) {
			t.Fatalf("optional format assertion vocabulary was accepted: %v", err)
		}
	})
	t.Run("duplicate dialect vocabulary entry", func(t *testing.T) {
		temporary := copyProtocolSchemas(t, root, nil)
		path := filepath.Join(temporary, filepath.FromSlash(schemaDialectPath))
		data, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		key := []byte(`"https://schemas.layerdraw.dev/vocab/protocol/v1": true`)
		duplicate := []byte(`"https://schemas.layerdraw.dev/vocab/protocol/v1": true, "https://schemas.layerdraw.dev/\u0076ocab/protocol/v1": true`)
		data = bytes.Replace(data, key, duplicate, 1)
		if err := os.WriteFile(path, data, 0o644); err != nil {
			t.Fatal(err)
		}
		if _, err := loadSchemas(temporary); err == nil || !strings.Contains(err.Error(), "duplicate JSON object key") {
			t.Fatalf("duplicate escaped-equivalent vocabulary entry was accepted: %v", err)
		}
	})
}

func TestSchemaDialectFailsClosedOverEveryRequiredVocabulary(t *testing.T) {
	t.Parallel()
	root := testRepositoryRoot(t)
	for _, vocabulary := range sortedKeys(requiredDialectVocabularies) {
		vocabulary := vocabulary
		mutations := []struct {
			name, code string
			mutate     func(map[string]any)
		}{
			{"remove", dialectVocabularyCode, func(values map[string]any) { delete(values, vocabulary) }},
			{"rename", dialectVocabularyCode, func(values map[string]any) {
				values[vocabulary+"-renamed"] = values[vocabulary]
				delete(values, vocabulary)
			}},
			{"weaken", dialectVocabularyValueCode, func(values map[string]any) { values[vocabulary] = false }},
			{"wrong_type", dialectVocabularyValueCode, func(values map[string]any) { values[vocabulary] = "true" }},
			{"disable", dialectVocabularyValueCode, func(values map[string]any) { values[vocabulary] = nil }},
			{"contradictory", dialectVocabularyCode, func(values map[string]any) { values[vocabulary+"#contradictory"] = false }},
		}
		for _, mutation := range mutations {
			mutation := mutation
			t.Run(vocabulary+"/"+mutation.name, func(t *testing.T) {
				temporary := mutateSchemaDialect(t, root, func(document map[string]any) {
					mutation.mutate(document["$vocabulary"].(map[string]any))
				})
				assertDialectDiagnostic(t, temporary, mutation.code)
			})
		}
	}
}

func TestSchemaDialectFailsClosedOverRootAndInventoryContainers(t *testing.T) {
	t.Parallel()
	root := testRepositoryRoot(t)
	mutations := []struct {
		name, code string
		mutate     func(map[string]any)
	}{
		{"dynamic_anchor", dialectRootShapeCode, func(document map[string]any) { document["$dynamicAnchor"] = "other" }},
		{"draft_applicator", dialectRootShapeCode, func(document map[string]any) { document["allOf"] = []any{} }},
		{"vocabulary_container", dialectVocabularyCode, func(document map[string]any) { document["$vocabulary"] = []any{} }},
		{"keyword_container", dialectKeywordCode, func(document map[string]any) { document["properties"] = []any{} }},
		{"definition_container", dialectDefinitionCode, func(document map[string]any) { document["$defs"] = []any{} }},
	}
	for _, mutation := range mutations {
		mutation := mutation
		t.Run(mutation.name, func(t *testing.T) {
			temporary := mutateSchemaDialect(t, root, mutation.mutate)
			assertDialectDiagnostic(t, temporary, mutation.code)
		})
	}
}

func TestSchemaDialectFailsClosedOverEveryRequiredKeyword(t *testing.T) {
	t.Parallel()
	root := testRepositoryRoot(t)
	for _, keyword := range sortedKeys(requiredDialectKeywordSchemas) {
		keyword := keyword
		mutations := []struct {
			name, code string
			mutate     func(map[string]any)
		}{
			{"remove", dialectKeywordCode, func(values map[string]any) { delete(values, keyword) }},
			{"rename", dialectKeywordCode, func(values map[string]any) { values[keyword+"-renamed"] = values[keyword]; delete(values, keyword) }},
			{"weaken", dialectKeywordShapeCode, func(values map[string]any) { values[keyword] = map[string]any{} }},
			{"wrong_type", dialectKeywordShapeCode, func(values map[string]any) { values[keyword] = map[string]any{"type": "number"} }},
			{"disable", dialectKeywordShapeCode, func(values map[string]any) { values[keyword] = false }},
			{"contradictory", dialectKeywordShapeCode, func(values map[string]any) {
				shape := cloneDialectObject(t, requiredDialectKeywordSchemas[keyword])
				shape["not"] = map[string]any{}
				values[keyword] = shape
			}},
		}
		for _, mutation := range mutations {
			mutation := mutation
			t.Run(keyword+"/"+mutation.name, func(t *testing.T) {
				temporary := mutateSchemaDialect(t, root, func(document map[string]any) {
					mutation.mutate(document["properties"].(map[string]any))
				})
				assertDialectDiagnostic(t, temporary, mutation.code)
			})
		}
	}
}

func TestSchemaDialectFailsClosedOverEveryRequiredKeywordDefinition(t *testing.T) {
	t.Parallel()
	root := testRepositoryRoot(t)
	for _, definition := range sortedKeys(requiredDialectDefinitions) {
		definition := definition
		mutations := []struct {
			name, code string
			mutate     func(map[string]any)
		}{
			{"remove", dialectDefinitionCode, func(values map[string]any) { delete(values, definition) }},
			{"rename", dialectDefinitionCode, func(values map[string]any) {
				values[definition+"Renamed"] = values[definition]
				delete(values, definition)
			}},
			{"weaken", dialectDefinitionShapeCode, func(values map[string]any) { values[definition] = map[string]any{} }},
			{"wrong_type", dialectDefinitionShapeCode, func(values map[string]any) { values[definition] = map[string]any{"type": "string"} }},
			{"disable", dialectDefinitionShapeCode, func(values map[string]any) { values[definition] = false }},
			{"contradictory", dialectDefinitionShapeCode, func(values map[string]any) {
				shape := cloneDialectObject(t, requiredDialectDefinitions[definition])
				shape["not"] = map[string]any{}
				values[definition] = shape
			}},
		}
		for _, mutation := range mutations {
			mutation := mutation
			t.Run(definition+"/"+mutation.name, func(t *testing.T) {
				temporary := mutateSchemaDialect(t, root, func(document map[string]any) {
					mutation.mutate(document["$defs"].(map[string]any))
				})
				assertDialectDiagnostic(t, temporary, mutation.code)
			})
		}
	}
}

func TestEveryProtocolSchemaMustAdvertiseScalarUnicodeAssertion(t *testing.T) {
	t.Parallel()
	root := testRepositoryRoot(t)
	for _, group := range []string{"protocol-common", "semantic", "engine-protocol"} {
		group := group
		for _, replacement := range []struct {
			name string
			line []byte
		}{
			{"removed", nil},
			{"disabled", []byte("  \"x-layerdraw-scalar-unicode\": false,\n")},
		} {
			replacement := replacement
			t.Run(group+"/"+replacement.name, func(t *testing.T) {
				temporary := copyProtocolSchemas(t, root, func(candidate string, data []byte) []byte {
					if candidate != group {
						return data
					}
					return bytes.Replace(data, []byte("  \"x-layerdraw-scalar-unicode\": true,\n"), replacement.line, 1)
				})
				if _, err := loadSchemas(temporary); err == nil || !strings.Contains(err.Error(), "DIALECT_SCHEMA_SCALAR_UNICODE") {
					t.Fatalf("protocol schema without scalar Unicode authority was accepted: %v", err)
				}
			})
		}
	}
}

func TestGeneratedSurfacesPreserveConstAndWireGuards(t *testing.T) {
	t.Parallel()
	set, err := loadSchemas(testRepositoryRoot(t))
	if err != nil {
		t.Fatal(err)
	}
	files, err := generate(set)
	if err != nil {
		t.Fatal(err)
	}
	byPath := map[string]string{}
	for _, file := range files {
		byPath[file.path] = string(file.data)
	}
	goTypes := byPath["gen/go/engineprotocol/types.gen.go"]
	for _, expected := range []string{
		`type CompileRequestEnvelopeOperation string`,
		`const CompileRequestEnvelopeOperationValue CompileRequestEnvelopeOperation = "engine.compile"`,
		`type QueryRecipeBlobRefMediaType string`,
		`const QueryRecipeBlobRefLifetimeValue QueryRecipeBlobRefLifetime = "request"`,
		`func CollectCompileInputBlobRefs(value CompileInput) ([]protocolcommon.BlobRef, error)`,
		`if _, err := EncodeCompileInput(value); err != nil`,
		`for _, blobItem3 := range value.ResolvedDependencies.Installs`,
		`func CollectCompileResultBlobRefs(value CompileResult) ([]protocolcommon.BlobRef, error)`,
	} {
		if !strings.Contains(goTypes, expected) {
			t.Errorf("generated Go const surface missing %q", expected)
		}
	}
	tsTypes := byPath["packages/protocol/src/engine.gen.ts"]
	for _, expected := range []string{
		`operation: "engine.compile";`,
		`media_type: "application/vnd.layerdraw.query-recipe.v1+json";`,
		`function scanUniqueJSONValue`,
		`function matchesCanonicalBinary64`,
		`lifetime: "request";`,
		`function hasDisjointArrays`,
		`export function collectCompileInputBlobRefs(value: CompileInput): ReadonlyArray<BlobRef>`,
		`for (const blobItem3 of value["resolved_dependencies"]["installs"])`,
		`export function collectCompileResultBlobRefs(value: CompileResult): ReadonlyArray<BlobRef>`,
	} {
		if !strings.Contains(tsTypes, expected) {
			t.Errorf("generated TypeScript surface missing %q", expected)
		}
	}
}

func TestEveryGeneratedCodecAndRecursivePredicateUsesBoundedPreflight(t *testing.T) {
	t.Parallel()
	set, err := loadSchemas(testRepositoryRoot(t))
	if err != nil {
		t.Fatal(err)
	}
	files, err := generate(set)
	if err != nil {
		t.Fatal(err)
	}
	byPath := map[string]string{}
	for _, file := range files {
		byPath[file.path] = string(file.data)
	}

	type definitionID struct {
		documentID string
		name       string
	}
	adjacency := map[definitionID]map[definitionID]bool{}
	for _, document := range set.documents {
		for name, definition := range document.Definitions {
			from := definitionID{documentID: document.ID, name: name}
			adjacency[from] = map[definitionID]bool{}
			var collectReferences func(*schemaType)
			collectReferences = func(value *schemaType) {
				if value == nil {
					return
				}
				if value.Ref != "" {
					target, targetName, resolveErr := resolveRef(set, document, value.Ref)
					if resolveErr != nil {
						t.Fatal(resolveErr)
					}
					adjacency[from][definitionID{documentID: target.ID, name: targetName}] = true
				}
				for _, property := range value.Properties {
					collectReferences(property)
				}
				collectReferences(value.Items)
				if additional, ok := value.AdditionalProperties.(*schemaType); ok {
					collectReferences(additional)
				}
				for _, branch := range value.OneOf {
					collectReferences(branch)
				}
			}
			collectReferences(definition)
		}
	}

	recursive := map[definitionID]bool{}
	for start := range adjacency {
		var reachesStart func(definitionID, map[definitionID]bool) bool
		reachesStart = func(current definitionID, visited map[definitionID]bool) bool {
			for next := range adjacency[current] {
				if next == start {
					return true
				}
				if !visited[next] {
					visited[next] = true
					if reachesStart(next, visited) {
						return true
					}
				}
			}
			return false
		}
		if reachesStart(start, map[definitionID]bool{start: true}) {
			recursive[start] = true
		}
	}
	if len(recursive) == 0 {
		t.Fatal("schema recursion audit unexpectedly found no recursive definitions")
	}
	recursiveNames := map[string]bool{}
	for id := range recursive {
		recursiveNames[set.byID[id.documentID].Module+"."+id.name] = true
	}
	for _, name := range []string{
		"common.JsonValue",
		"semantic.DiagnosticArgumentValue",
		"semantic.RecipePredicate",
		"semantic.RecipeRowPredicate",
	} {
		if !recursiveNames[name] {
			t.Errorf("schema recursion audit did not find %s", name)
		}
	}
	if len(recursiveNames) != 4 {
		t.Errorf("schema recursion audit found %d recursive definitions, want 4: %v", len(recursiveNames), recursiveNames)
	}

	reachesRecursive := map[definitionID]bool{}
	for start := range adjacency {
		var reaches func(definitionID, map[definitionID]bool) bool
		reaches = func(current definitionID, visited map[definitionID]bool) bool {
			if recursive[current] {
				return true
			}
			for next := range adjacency[current] {
				if !visited[next] {
					visited[next] = true
					if reaches(next, visited) {
						return true
					}
				}
			}
			return false
		}
		reachesRecursive[start] = reaches(start, map[definitionID]bool{start: true})
	}

	reachingCount := 0
	for _, document := range set.documents {
		typeScript := byPath["packages/protocol/src/"+document.Module+".gen.ts"]
		goCodec := byPath["gen/go/"+document.Package+"/codec.gen.go"]
		for name := range document.Definitions {
			predicatePrefix := "export function is" + name + "(value: unknown): value is " + name + " {\n  return isProgrammaticWireValue(value, () => "
			if !strings.Contains(typeScript, predicatePrefix) {
				t.Errorf("generated TypeScript predicate %s.%s lacks the total bounded preflight", document.Module, name)
			}
			if !strings.Contains(typeScript, "export function encode"+name+"(value: "+name+"): string {\n  validateProgrammaticWireValue(value);") {
				t.Errorf("generated TypeScript encoder %s.%s lacks the bounded preflight", document.Module, name)
			}
			if !strings.Contains(goCodec, "func Encode"+name+"(value "+name+") ([]byte, error) {\n\tif err := validateGoWireValue(reflect.ValueOf(value), map[visit]bool{}, 0); err != nil {") {
				t.Errorf("generated Go encoder %s.%s lacks the bounded preflight", document.Package, name)
			}
			id := definitionID{documentID: document.ID, name: name}
			if reachesRecursive[id] {
				reachingCount++
				t.Logf("audited bounded predicate for %s.%s, which reaches schema recursion", document.Module, name)
			}
		}
	}
	if reachingCount <= len(recursive) {
		t.Fatalf("schema graph audit found no non-recursive definitions reaching recursion: recursive=%d reaching=%d", len(recursive), reachingCount)
	}
	commonTypeScript := byPath["packages/protocol/src/common.gen.ts"]
	if !strings.Contains(commonTypeScript, "export function isJsonValue(value: unknown): value is JsonValue {\n  return isProgrammaticWireValue(value, () => isJSONCompatible(value));") {
		t.Error("generated JsonValue predicate no longer composes its specialized validator with the bounded preflight")
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
	copySchemaDialect(t, root, copyRoot)
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
	copySchemaDialect(t, root, copyRoot)
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
	copySchemaDialect(t, root, temporary)
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
	copySchemaDialect(t, root, temporary)
	return temporary
}

func copySchemaDialect(t *testing.T, root, targetRoot string) {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(schemaDialectPath)))
	if err != nil {
		t.Fatal(err)
	}
	target := filepath.Join(targetRoot, filepath.FromSlash(schemaDialectPath))
	if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(target, data, 0o644); err != nil {
		t.Fatal(err)
	}
}

func mutateSchemaDialect(t *testing.T, root string, mutate func(map[string]any)) string {
	t.Helper()
	temporary := copyProtocolSchemas(t, root, nil)
	path := filepath.Join(temporary, filepath.FromSlash(schemaDialectPath))
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var document map[string]any
	if err := json.Unmarshal(data, &document); err != nil {
		t.Fatal(err)
	}
	mutate(document)
	data, err = json.MarshalIndent(document, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	data = append(data, '\n')
	if err := os.WriteFile(path, data, 0o644); err != nil {
		t.Fatal(err)
	}
	return temporary
}

func cloneDialectObject(t *testing.T, value any) map[string]any {
	t.Helper()
	data, err := json.Marshal(value)
	if err != nil {
		t.Fatal(err)
	}
	var result map[string]any
	if err := json.Unmarshal(data, &result); err != nil {
		t.Fatal(err)
	}
	return result
}

func assertDialectDiagnostic(t *testing.T, root, code string) {
	t.Helper()
	_, err := loadSchemas(root)
	if err == nil || !strings.HasPrefix(err.Error(), code+": LayerDraw schema dialect ") {
		t.Fatalf("dialect mutation did not return stable %s diagnostic: %v", code, err)
	}
}
