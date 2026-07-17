// SPDX-License-Identifier: LicenseRef-LayerDraw-1.0

package definition

import (
	"math"
	"reflect"
	"testing"

	"github.com/dencyuinc/layerdraw/internal/engine/internal/compiler/resolve"
	"github.com/dencyuinc/layerdraw/internal/engine/internal/compiler/syntax"
)

func TestFactHelpersReuseDefinitionNormalization(t *testing.T) {
	if got := NormalizeText("e\u0301\r\n"); got != "é\n" {
		t.Fatalf("NormalizeText() = %q", got)
	}
	hostname := StringFormatHostname
	column := Column{ValueType: ScalarString, Format: &hostname, MinLength: int64Pointer(3), MaxLength: int64Pointer(32)}
	got, ok := NormalizeScalarLiteral(`"API.EXAMPLE.COM."`, syntax.TokenString, column)
	if !ok || got.Type != ScalarString || got.String != "api.example.com" {
		t.Fatalf("NormalizeScalarLiteral() = %+v, %v", got, ok)
	}
	if _, ok := NormalizeScalarLiteral(`"x"`, syntax.TokenString, column); ok {
		t.Fatal("NormalizeScalarLiteral accepted an invalid constrained scalar")
	}
	if _, ok := NormalizeScalarLiteral(`"one"`, syntax.TokenString, Column{ValueType: ScalarInteger}); ok {
		t.Fatal("NormalizeScalarLiteral accepted a type mismatch")
	}
}

func TestNormalizeScalarValueCoversTypedSchemas(t *testing.T) {
	t.Parallel()
	hostname := StringFormatHostname
	minimumLength, maximumLength := int64(3), int64(32)
	minimum, maximum := 1.0, 3.0
	tests := []struct {
		name   string
		value  Scalar
		column Column
		want   Scalar
	}{
		{
			name: "formatted string", value: Scalar{Type: ScalarString, String: "API.EXAMPLE.COM."},
			column: Column{ValueType: ScalarString, Format: &hostname, MinLength: &minimumLength, MaxLength: &maximumLength},
			want:   Scalar{Type: ScalarString, String: "api.example.com"},
		},
		{
			name: "normalized enum", value: Scalar{Type: ScalarEnum, String: "e\u0301"},
			column: Column{ValueType: ScalarEnum, EnumValues: []string{"é"}, ReservedEnumValues: []string{"legacy"}},
			want:   Scalar{Type: ScalarEnum, String: "é"},
		},
		{name: "date", value: Scalar{Type: ScalarDate, String: "2026-07-17"}, column: Column{ValueType: ScalarDate}, want: Scalar{Type: ScalarDate, String: "2026-07-17"}},
		{name: "datetime", value: Scalar{Type: ScalarDatetime, String: "2026-07-17T12:00:00+09:00"}, column: Column{ValueType: ScalarDatetime}, want: Scalar{Type: ScalarDatetime, String: "2026-07-17T03:00:00Z"}},
		{name: "integer", value: Scalar{Type: ScalarInteger, Int: 2}, column: Column{ValueType: ScalarInteger, Min: &minimum, Max: &maximum}, want: Scalar{Type: ScalarInteger, Int: 2}},
		{name: "number", value: Scalar{Type: ScalarNumber, Float: math.Copysign(0, -1)}, column: Column{ValueType: ScalarNumber, Min: float64Pointer(-1), Max: float64Pointer(1)}, want: Scalar{Type: ScalarNumber, Float: 0}},
		{name: "boolean", value: Scalar{Type: ScalarBoolean, Bool: true}, column: Column{ValueType: ScalarBoolean}, want: Scalar{Type: ScalarBoolean, Bool: true}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			var work int64
			got, ok := NormalizeScalarValue(test.value, test.column, func(amount int64) bool {
				work += amount
				return true
			})
			if !ok || got != test.want || work <= 0 {
				t.Fatalf("NormalizeScalarValue() = %+v, %v, work %d; want %+v", got, ok, work, test.want)
			}
		})
	}
}

func TestNormalizeScalarValueRejectsInvalidValuesAndConstraints(t *testing.T) {
	t.Parallel()
	unknownFormat := StringFormat("unknown")
	minimumLength, maximumLength := int64(2), int64(3)
	minimum, maximum := 1.0, 3.0
	malformed := string([]byte{0xff})
	tests := []struct {
		name   string
		value  Scalar
		column Column
	}{
		{name: "type mismatch", value: Scalar{Type: ScalarString}, column: Column{ValueType: ScalarBoolean}},
		{name: "malformed string", value: Scalar{Type: ScalarString, String: malformed}, column: Column{ValueType: ScalarString}},
		{name: "unknown string format", value: Scalar{Type: ScalarString, String: "value"}, column: Column{ValueType: ScalarString, Format: &unknownFormat}},
		{name: "short string", value: Scalar{Type: ScalarString, String: "a"}, column: Column{ValueType: ScalarString, MinLength: &minimumLength}},
		{name: "long string", value: Scalar{Type: ScalarString, String: "abcd"}, column: Column{ValueType: ScalarString, MaxLength: &maximumLength}},
		{name: "unknown enum", value: Scalar{Type: ScalarEnum, String: "staging"}, column: Column{ValueType: ScalarEnum, EnumValues: []string{"prod"}}},
		{name: "reserved enum", value: Scalar{Type: ScalarEnum, String: "legacy"}, column: Column{ValueType: ScalarEnum, EnumValues: []string{"legacy"}, ReservedEnumValues: []string{"legacy"}}},
		{name: "invalid date", value: Scalar{Type: ScalarDate, String: "2025-02-29"}, column: Column{ValueType: ScalarDate}},
		{name: "invalid datetime", value: Scalar{Type: ScalarDatetime, String: "not-a-datetime"}, column: Column{ValueType: ScalarDatetime}},
		{name: "unsafe integer", value: Scalar{Type: ScalarInteger, Int: maxJSONSafeInteger + 1}, column: Column{ValueType: ScalarInteger}},
		{name: "integer below minimum", value: Scalar{Type: ScalarInteger, Int: 0}, column: Column{ValueType: ScalarInteger, Min: &minimum}},
		{name: "integer above maximum", value: Scalar{Type: ScalarInteger, Int: 4}, column: Column{ValueType: ScalarInteger, Max: &maximum}},
		{name: "NaN", value: Scalar{Type: ScalarNumber, Float: math.NaN()}, column: Column{ValueType: ScalarNumber}},
		{name: "infinity", value: Scalar{Type: ScalarNumber, Float: math.Inf(1)}, column: Column{ValueType: ScalarNumber}},
		{name: "number below minimum", value: Scalar{Type: ScalarNumber, Float: 0}, column: Column{ValueType: ScalarNumber, Min: &minimum}},
		{name: "number above maximum", value: Scalar{Type: ScalarNumber, Float: 4}, column: Column{ValueType: ScalarNumber, Max: &maximum}},
		{name: "unknown scalar type", value: Scalar{Type: "unknown"}, column: Column{ValueType: "unknown"}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if got, ok := NormalizeScalarValue(test.value, test.column, nil); ok {
				t.Fatalf("NormalizeScalarValue() accepted %+v as %+v", test.value, got)
			}
		})
	}
	if got, ok := NormalizeScalarValue(Scalar{Type: ScalarBoolean, Bool: true}, Column{ValueType: ScalarBoolean}, func(int64) bool { return false }); ok {
		t.Fatalf("NormalizeScalarValue() ignored observer rejection: %+v", got)
	}
}

func TestScalarNormalizationObserverHelpers(t *testing.T) {
	t.Parallel()
	if _, ok := normalizeObservedText(string([]byte{0xff}), nil); ok {
		t.Fatal("normalizeObservedText accepted malformed UTF-8")
	}
	if _, ok := normalizeObservedText("value", func(int64) bool { return false }); ok {
		t.Fatal("normalizeObservedText ignored observer rejection")
	}
	remaining := 1
	if _, ok := normalizeObservedText("a", func(int64) bool {
		remaining--
		return remaining >= 0
	}); ok {
		t.Fatal("normalizeObservedText ignored normalization admission rejection")
	}
	if count, ok := observedRuneCount("éa", nil); !ok || count != 2 {
		t.Fatalf("observedRuneCount() = %d, %v", count, ok)
	}
	if _, ok := observedRuneCount("ab", func(int64) bool { return false }); ok {
		t.Fatal("observedRuneCount ignored observer rejection")
	}
	if found, complete := observedContains([]string{"different-length", "é"}, "é", nil); !found || !complete {
		t.Fatalf("observedContains match = %v, %v", found, complete)
	}
	if found, complete := observedContains([]string{"ê"}, "é", nil); found || !complete {
		t.Fatalf("observedContains mismatch = %v, %v", found, complete)
	}
	if found, complete := observedContains([]string{"value"}, "value", func(int64) bool { return false }); found || complete {
		t.Fatalf("observedContains cancellation = %v, %v", found, complete)
	}
	if observeScalarWork(nil, -1) {
		t.Fatal("observeScalarWork accepted negative work")
	}
}

func TestCompileFactCommonAllowsReservationsAndRejectsUnknownFields(t *testing.T) {
	resolved := resolve.Resolve(resolve.Input{Mode: resolve.CompileProject, EntryPath: "document.ldl", Project: resolve.ProjectInput{Files: map[string]resolve.SourceFile{
		"document.ldl": parse(`
project p "P" {}
layers {
  app "App" @0
}
entity_type node "Node" {
  representation shape rect
}
entities node @app {
  valid "Valid" {
    description "Description"
    tags [zeta, alpha]
    annotations { owner: "platform" }
    reserve_rows [old]
  }
  invalid "Invalid" {
    unknown "value"
  }
}
`),
	}}})
	if resolved.HasErrors {
		t.Fatalf("resolve diagnostics = %+v", resolved.Diagnostics)
	}
	var valid, invalid resolve.DeclarationSource
	for _, src := range resolved.DeclarationSources {
		switch src.Address {
		case "ldl:project:p:entity:valid":
			valid = src
		case "ldl:project:p:entity:invalid":
			invalid = src
		}
	}
	common, diagnostics := CompileFactCommon(valid)
	if len(diagnostics) != 0 || common.Description == nil || *common.Description != "Description" || !reflect.DeepEqual(common.Tags, []string{"alpha", "zeta"}) || common.Annotations["owner"] != "platform" {
		t.Fatalf("CompileFactCommon(valid) = %+v, diagnostics %+v", common, diagnostics)
	}
	_, diagnostics = CompileFactCommon(invalid)
	if len(diagnostics) != 1 || diagnostics[0].Code != "LDL1102" {
		t.Fatalf("CompileFactCommon(invalid) diagnostics = %+v", diagnostics)
	}
}

func TestCompileQueryScalarSchemaAndCommonFields(t *testing.T) {
	resolved := resolve.Resolve(resolve.Input{Mode: resolve.CompileProject, EntryPath: "document.ldl", Project: resolve.ProjectInput{Files: map[string]resolve.SourceFile{
		"document.ldl": parse(`
project p "P" {}
query q "Q" {
  description "Saved selection"
  tags [saved]
  parameters {
    threshold number required default 1.5 min 0 max 2
  }
  select {}
}
`),
	}}})
	if resolved.HasErrors {
		t.Fatalf("resolve diagnostics = %+v", resolved.Diagnostics)
	}
	var parameter resolve.DeclarationSymbol
	var parameterSource, querySource resolve.DeclarationSource
	for _, declaration := range resolved.Declarations {
		if declaration.Kind == resolve.KindParameter {
			parameter = declaration
		}
	}
	for _, source := range resolved.DeclarationSources {
		switch source.Kind {
		case resolve.KindParameter:
			parameterSource = source
		case resolve.KindQuery:
			querySource = source
		}
	}
	column, diagnostics := CompileScalarSchema(parameter, parameterSource)
	if len(diagnostics) != 0 || column.ID != "threshold" || column.ValueType != ScalarNumber || !column.Required || column.Default == nil || column.Default.Float != 1.5 || column.Min == nil || *column.Min != 0 || column.Max == nil || *column.Max != 2 {
		t.Fatalf("CompileScalarSchema() = %+v, diagnostics %+v", column, diagnostics)
	}
	common, diagnostics := CompileCommonFields(querySource)
	if len(diagnostics) != 0 || common.Description == nil || *common.Description != "Saved selection" || !reflect.DeepEqual(common.Tags, []string{"saved"}) {
		t.Fatalf("CompileCommonFields() = %+v, diagnostics %+v", common, diagnostics)
	}

	parameter.ID = "other"
	_, diagnostics = CompileScalarSchema(parameter, parameterSource)
	if len(diagnostics) != 1 || diagnostics[0].Code != "LDL1601" {
		t.Fatalf("mismatched scalar source diagnostics = %+v", diagnostics)
	}
}

func int64Pointer(value int64) *int64 {
	return &value
}

func float64Pointer(value float64) *float64 {
	return &value
}
