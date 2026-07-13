// SPDX-License-Identifier: LicenseRef-LayerDraw-1.0

package definition

import (
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
