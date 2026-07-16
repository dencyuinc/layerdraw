// SPDX-License-Identifier: LicenseRef-LayerDraw-1.0

package endpoint

import (
	"encoding/json"
	"os"
	"testing"

	"github.com/dencyuinc/layerdraw/gen/go/engineprotocol"
	"github.com/dencyuinc/layerdraw/internal/engine"
)

func TestMapSemanticEditPlanInputConsumesCompleteGeneratedCreateUnion(t *testing.T) {
	t.Parallel()
	data, err := os.ReadFile("../../../schemas/fixtures/engine/workbench-create-subject-all-kinds.json")
	if err != nil {
		t.Fatal(err)
	}
	batch, err := engineprotocol.DecodeSemanticOperationBatch(data)
	if err != nil {
		t.Fatal(err)
	}
	mapped, err := MapSemanticEditPlanInput(engine.CompileInput{}, engine.Snapshot{}, engineprotocol.EngineEditPreconditions{}, batch)
	if err != nil {
		t.Fatal(err)
	}
	if len(mapped.Batch.Operations) != 14 {
		t.Fatalf("mapped operations=%d", len(mapped.Batch.Operations))
	}
	seen := map[engine.SemanticSubjectKind]bool{}
	for _, operation := range mapped.Batch.Operations {
		if operation.Kind != engine.OperationCreateSubject || operation.ParentAddress == "" || operation.ID == "" || len(operation.Fields) == 0 {
			t.Fatalf("incomplete mapped operation: %+v", operation)
		}
		seen[operation.SubjectKind] = true
	}
	for _, kind := range []engine.SemanticSubjectKind{"entity_type", "relation_type", "layer", "entity", "query", "view", "reference", "entity_type_column", "entity_type_constraint", "relation_type_column", "relation_type_constraint", "query_parameter", "view_table_column", "view_export"} {
		if !seen[kind] {
			t.Fatalf("missing generated create kind %q", kind)
		}
	}
}

func TestMapPlainSemanticValuesPreservesScalarKinds(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name string
		wire any
		kind engine.SemanticValueKind
	}{
		{name: "absent", wire: nil, kind: engine.SemanticValueAbsent},
		{name: "boolean", wire: true, kind: engine.SemanticValueBoolean},
		{name: "string", wire: "plain", kind: engine.SemanticValueString},
		{name: "stable address", wire: "ldl:project:p", kind: engine.SemanticValueAddress},
		{name: "integer", wire: json.Number("2"), kind: engine.SemanticValueInteger},
		{name: "decimal", wire: json.Number("1.5"), kind: engine.SemanticValueDecimal},
		{name: "array", wire: []any{"x"}, kind: engine.SemanticValueArray},
		{name: "map", wire: map[string]any{"x": true}, kind: engine.SemanticValueMap},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if got := mapPlainValue(test.wire); got.Kind != test.kind {
				t.Fatalf("mapped kind=%q want=%q", got.Kind, test.kind)
			}
		})
	}
}

func TestSemanticIntegerMappingAcceptsCanonicalWireForms(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name  string
		wire  any
		value int64
	}{
		{name: "JSON number", wire: json.Number("4"), value: 4},
		{name: "string", wire: "5", value: 5},
		{name: "decoded float", wire: float64(6), value: 6},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got, err := numberInt64(test.wire)
			if err != nil || got != test.value {
				t.Fatalf("mapped integer=%d err=%v want=%d", got, err, test.value)
			}
		})
	}
	if _, err := numberInt64(true); err == nil {
		t.Fatal("boolean integer was accepted")
	}
}

func TestTaggedSemanticValueMappingRejectsMalformedRecursiveValues(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name string
		wire any
	}{
		{name: "non-object", wire: "not-an-object"},
		{name: "unknown kind", wire: map[string]any{"kind": "unknown"}},
		{name: "non-integer payload", wire: map[string]any{"kind": "integer", "integer": true}},
		{name: "invalid array child", wire: map[string]any{"kind": "array", "array": []any{"bad"}}},
		{name: "invalid map entry", wire: map[string]any{"kind": "map", "map": []any{"bad"}}},
		{name: "invalid map value", wire: map[string]any{"kind": "map", "map": []any{map[string]any{"key": "x", "value": "bad"}}}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if _, err := mapTaggedSemanticValue(test.wire); err == nil {
				t.Fatalf("malformed value was accepted: %+v", test.wire)
			}
		})
	}
}

func TestSemanticOperationMappingRejectsMalformedNestedPayloads(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name string
		wire map[string]any
	}{
		{name: "untagged update value", wire: map[string]any{"operation": "update_subject_field", "value": "bad"}},
		{name: "non-object row cell", wire: map[string]any{"operation": "upsert_row", "values": []any{"bad"}}},
		{name: "untagged row value", wire: map[string]any{"operation": "upsert_row", "values": []any{map[string]any{"column_address": "x", "value": "bad"}}}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if _, err := mapSemanticOperation(test.wire); err == nil {
				t.Fatalf("malformed operation was accepted: %+v", test.wire)
			}
		})
	}
}

func TestMapSemanticEditPlanInputRetainsClosedRecursiveValues(t *testing.T) {
	t.Parallel()
	data, err := os.ReadFile("../../../schemas/fixtures/engine/workbench-preview-operations-request.json")
	if err != nil {
		t.Fatal(err)
	}
	request, err := engineprotocol.DecodePreviewOperationsRequestEnvelope(data)
	if err != nil {
		t.Fatal(err)
	}
	mapped, err := MapSemanticEditPlanInput(engine.CompileInput{}, engine.Snapshot{}, request.Payload.Preconditions, request.Payload.Batch)
	if err != nil {
		t.Fatal(err)
	}
	operation := mapped.Batch.Operations[0]
	if operation.Kind != engine.OperationUpdateSubjectField || operation.Value == nil || operation.Value.Kind != engine.SemanticValueMap || len(operation.Value.Map) != 1 || operation.Value.Map[0].Value.Kind != engine.SemanticValueArray {
		t.Fatalf("recursive value mapping=%+v", operation)
	}
}
