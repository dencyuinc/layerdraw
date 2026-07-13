// SPDX-License-Identifier: LicenseRef-LayerDraw-1.0

package resolve

import (
	"reflect"
	"testing"
)

func TestCloneDiagnosticsOwnsAllMutableStorage(t *testing.T) {
	original := []Diagnostic{{
		Code: "LDL9999", Arguments: map[string]string{"key": "value"},
		Range: &SourceRange{ModulePath: "document.ldl", StartByte: 1, EndByte: 2},
		Related: []DiagnosticRelated{{
			Relation: "previous",
			Range:    &SourceRange{ModulePath: "document.ldl", StartByte: 3, EndByte: 4},
		}},
	}, {Code: "LDL0000"}}
	want := []Diagnostic{{
		Code: "LDL9999", Arguments: map[string]string{"key": "value"},
		Range: &SourceRange{ModulePath: "document.ldl", StartByte: 1, EndByte: 2},
		Related: []DiagnosticRelated{{
			Relation: "previous",
			Range:    &SourceRange{ModulePath: "document.ldl", StartByte: 3, EndByte: 4},
		}},
	}, {Code: "LDL0000"}}

	cloned := CloneDiagnostics(original)
	cloned[0].Arguments["key"] = "changed"
	cloned[0].Range.StartByte = 9
	cloned[0].Related[0].Relation = "changed"
	cloned[0].Related[0].Range.StartByte = 10
	if !reflect.DeepEqual(original, want) {
		t.Fatalf("clone shares mutable storage: got=%+v want=%+v", original, want)
	}
}

func TestStageGenerationUsesInvocationIdentity(t *testing.T) {
	first := newStageGeneration()
	second := newStageGeneration()
	if !first.Matches(first) || first.Matches(second) || first.Matches(StageGeneration{}) || (StageGeneration{}).Matches(first) {
		t.Fatalf("generation identity mismatch: first=%+v second=%+v", first, second)
	}
	result := Result{stageGeneration: first}
	if !result.Generation().Matches(first) {
		t.Fatal("result did not expose its generation")
	}
}
