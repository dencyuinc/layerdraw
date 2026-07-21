// SPDX-License-Identifier: LicenseRef-LayerDraw-1.0

package engine

import (
	"reflect"
	"testing"

	"github.com/dencyuinc/layerdraw/internal/engine/internal/compiler/index"
)

func TestReferenceSourcesFiltersDeduplicatesAndUsesStableAddressOrder(t *testing.T) {
	target := "ldl:project:p:entity:target"
	snapshot := Snapshot{CompileOutput: CompileOutput{SemanticIndex: SemanticIndex{References: []index.SemanticReference{
		{SourceAddress: "ldl:project:p:relation:z", TargetAddress: target},
		{SourceAddress: "ldl:project:p:entity:a", TargetAddress: "ldl:project:p:entity:other"},
		{SourceAddress: "ldl:project:p:entity:b", TargetAddress: target},
		{SourceAddress: "ldl:project:p:entity:b", TargetAddress: target},
		{SourceAddress: "ldl:project:p:entity:a", TargetAddress: target},
	}}}}

	want := []string{
		"ldl:project:p:entity:a",
		"ldl:project:p:entity:b",
		"ldl:project:p:relation:z",
	}
	if got := referenceSources(snapshot, target); !reflect.DeepEqual(got, want) {
		t.Fatalf("reference sources=%v, want %v", got, want)
	}
	if got := referenceSources(snapshot, "ldl:project:p:entity:missing"); len(got) != 0 {
		t.Fatalf("missing target sources=%v", got)
	}
}
