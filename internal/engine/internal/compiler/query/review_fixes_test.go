// SPDX-License-Identifier: LicenseRef-LayerDraw-1.0

package query

import (
	"reflect"
	"strings"
	"sync"
	"testing"

	"github.com/dencyuinc/layerdraw/internal/engine/internal/compiler/resolve"
)

func TestAddressPredicateResolvesLiteralMatchingFieldName(t *testing.T) {
	got := compileProject(t, map[string]string{"document.ldl": minimalSchema + `
entities a @app {
  address "Address"
}
query same_name "Same name" {
  select {}
  where all {
    field address == address
  }
}
`})
	if got.HasErrors || len(got.Recipes) != 1 || len(got.Recipes[0].Where.Children) != 1 {
		t.Fatalf("result=%+v diagnostics=%+v", got.Recipes, got.Diagnostics)
	}
	value := got.Recipes[0].Where.Children[0].Value
	want := "ldl:project:p:entity:address"
	if value == nil || value.Address == nil || *value.Address != want {
		t.Fatalf("address predicate value=%+v, want %q", value, want)
	}
}

func TestTraversalDepthUsesJSONSafeIntegerDomain(t *testing.T) {
	accepted := compileProject(t, map[string]string{"document.ldl": minimalSchema + `
query boundary "Boundary" {
  select {}
  traverse outgoing 0..9007199254740991 visit_once
}
`})
	if accepted.HasErrors || accepted.Recipes[0].Traversal.MaxDepth != 9007199254740991 {
		t.Fatalf("safe boundary rejected: recipes=%+v diagnostics=%+v", accepted.Recipes, accepted.Diagnostics)
	}

	rejected := compileProject(t, map[string]string{"document.ldl": minimalSchema + `
query overflow "Overflow" {
  select {}
  traverse outgoing 0..9007199254740992 visit_once
}
`})
	if !rejected.HasErrors || !hasDiagnosticMessage(rejected, "invalid traversal depth range") {
		t.Fatalf("unsafe traversal depth accepted: %+v", rejected)
	}
}

func TestNestedParameterDeclarationIsRejected(t *testing.T) {
	got := compileProject(t, map[string]string{"document.ldl": minimalSchema + `
query nested "Nested" {
  parameters {
    ignored string {}
  }
  select {}
}
`})
	if !got.HasErrors || got.Recipes != nil || !diagnosticCode(got, "LDL1102") || !hasDiagnosticMessage(got, "query parameter declarations must be scalar statements") {
		t.Fatalf("nested parameter declaration was ignored: %+v", got)
	}
}

func TestQueryBlockHeadersRejectArguments(t *testing.T) {
	for _, block := range []string{"reserve", "parameters", "select"} {
		t.Run(block, func(t *testing.T) {
			got := compileProject(t, map[string]string{"document.ldl": minimalSchema + `
query invalid_header "Invalid header" {
  ` + block + ` ignored {}
  select {}
}
`})
			if !got.HasErrors || got.Recipes != nil || !diagnosticCode(got, "LDL1102") {
				t.Fatalf("%s block argument was ignored: %+v", block, got)
			}
		})
	}
}

func TestTagContainsAcceptsStringParameter(t *testing.T) {
	got := compileProject(t, map[string]string{"document.ldl": minimalSchema + `
query parameterized_tag "Parameterized tag" {
  parameters {
    tag string
  }
  select {}
  where all {
    field tags contains $tag
  }
}
`})
	if got.HasErrors || len(got.Recipes) != 1 || len(got.Recipes[0].Where.Children) != 1 {
		t.Fatalf("parameterized tag predicate failed: recipes=%+v diagnostics=%+v", got.Recipes, got.Diagnostics)
	}
	value := got.Recipes[0].Where.Children[0].Value
	if value == nil || value.Kind != ValueParameter || value.ParameterAddress != "ldl:project:p:query:parameterized_tag:parameter:tag" {
		t.Fatalf("tag predicate value = %+v", value)
	}
}

func TestInvalidStatePredicateDoesNotReportUnusedStatePolicy(t *testing.T) {
	got := compileProject(t, map[string]string{"document.ldl": minimalSchema + `
query invalid_state "Invalid state" {
  state_input optional
  select {}
  where all {
    state system.created_at contains "x"
  }
}
`})
	if !got.HasErrors || !hasDiagnosticMessage(got, "operator is incompatible with state field type") {
		t.Fatalf("invalid state operator was accepted: %+v", got)
	}
	if hasDiagnosticMessage(got, "state_input is forbidden without a state predicate") {
		t.Fatalf("authored state predicate was treated as absent: %+v", got.Diagnostics)
	}
}

func TestMalformedPredicateAncestorStillCountsAuthoredStatePredicate(t *testing.T) {
	got := compileProject(t, map[string]string{"document.ldl": minimalSchema + `
query invalid_group "Invalid group" {
  state_input optional
  select {}
  where all {
    all extra {
      state system.created_at exists
    }
  }
}
`})
	if !got.HasErrors || !hasDiagnosticMessage(got, "boolean predicate group requires only a block") {
		t.Fatalf("malformed group was accepted: %+v", got)
	}
	if hasDiagnosticMessage(got, "state_input is forbidden without a state predicate") {
		t.Fatalf("nested authored state predicate was treated as absent: %+v", got.Diagnostics)
	}
}

func TestQueryDiagnosticsUseMinimumAuthoredRange(t *testing.T) {
	source := minimalSchema + `
query ranges "Ranges" {
  state_input optional
  select {}
}
`
	got := compileProject(t, map[string]string{"document.ldl": source})
	diagnostic := diagnosticWithMessage(got, "state_input is forbidden without a state predicate")
	wantStart := strings.Index(source, "state_input optional")
	wantEnd := wantStart + len("state_input optional")
	if diagnostic == nil || diagnostic.Range == nil || diagnostic.Range.StartByte != wantStart || diagnostic.Range.EndByte != wantEnd {
		t.Fatalf("state policy range = %+v, want [%d,%d)", diagnostic, wantStart, wantEnd)
	}

	source = minimalSchema + `
query ranges "Ranges" {
  select {}
  where all {
    field id ==
  }
}
`
	got = compileProject(t, map[string]string{"document.ldl": source})
	diagnostic = diagnosticWithMessage(got, "predicate operator requires exactly one value")
	wantStart = strings.Index(source, "field id ==")
	wantEnd = wantStart + len("field id ==")
	if diagnostic == nil || diagnostic.Range == nil || diagnostic.Range.StartByte != wantStart || diagnostic.Range.EndByte != wantEnd {
		t.Fatalf("missing value range = %+v, want [%d,%d)", diagnostic, wantStart, wantEnd)
	}
}

func TestGenerationMismatchIsCheckedBeforeUpstreamFailure(t *testing.T) {
	source := minimalSchema + "query q \"Q\" {\n  select {}\n}\n"
	first := projectInput(t, map[string]string{"document.ldl": source})
	second := projectInput(t, map[string]string{"document.ldl": source})
	second.Graph.HasErrors = true

	got := Compile(Input{Resolve: first.Resolve, Definition: second.Definition, Graph: second.Graph})
	if !got.HasErrors || !diagnosticCode(got, "LDL1801") || got.MatchesResolve(first.Resolve) || got.MatchesResolve(second.Resolve) {
		t.Fatalf("mixed failed parents were not rejected transactionally: %+v", got)
	}
}

func TestResultGenerationCoversSuccessAndSemanticFailure(t *testing.T) {
	validInput := projectInput(t, map[string]string{"document.ldl": minimalSchema + "query q \"Q\" {\n  select {}\n}\n"})
	valid := Compile(validInput)
	if valid.HasErrors || !valid.MatchesResolve(validInput.Resolve) || !valid.Generation().Matches(validInput.Resolve.Generation()) {
		t.Fatalf("successful result lost generation: %+v", valid)
	}

	invalidInput := projectInput(t, map[string]string{"document.ldl": minimalSchema + "query q \"Q\" {\n  result [unknown]\n  select {}\n}\n"})
	invalid := Compile(invalidInput)
	if !invalid.HasErrors || !invalid.MatchesResolve(invalidInput.Resolve) {
		t.Fatalf("semantic failure lost coherent generation: %+v", invalid)
	}
}

func TestCompileDoesNotMutateSharedUpstreamDiagnostics(t *testing.T) {
	input := projectInput(t, map[string]string{"document.ldl": minimalSchema + "query q \"Q\" {\n  select {}\n}\n"})
	input.Graph.HasErrors = true
	input.Graph.Diagnostics = []resolve.Diagnostic{{
		Code: "LDL9999", Severity: "error", MessageKey: "test", Arguments: map[string]string{"key": "value"},
		Range: &resolve.SourceRange{ModulePath: "document.ldl", StartByte: 1, EndByte: 2},
		Related: []resolve.DiagnosticRelated{
			{Relation: "z", Range: &resolve.SourceRange{ModulePath: "document.ldl", StartByte: 4, EndByte: 5}},
			{Relation: "a", Range: &resolve.SourceRange{ModulePath: "document.ldl", StartByte: 2, EndByte: 3}},
		},
	}}
	want := resolve.CloneDiagnostics(input.Graph.Diagnostics)

	var wait sync.WaitGroup
	for i := 0; i < 32; i++ {
		wait.Add(1)
		go func() {
			defer wait.Done()
			for iteration := 0; iteration < 25; iteration++ {
				_ = Compile(input)
			}
		}()
	}
	wait.Wait()
	if !reflect.DeepEqual(input.Graph.Diagnostics, want) {
		t.Fatalf("upstream diagnostics mutated: got=%+v want=%+v", input.Graph.Diagnostics, want)
	}
}

func hasDiagnosticMessage(result Result, message string) bool {
	for _, diagnostic := range result.Diagnostics {
		if diagnostic.Message == message {
			return true
		}
	}
	return false
}

func diagnosticWithMessage(result Result, message string) *resolve.Diagnostic {
	for index := range result.Diagnostics {
		if result.Diagnostics[index].Message == message {
			return &result.Diagnostics[index]
		}
	}
	return nil
}
