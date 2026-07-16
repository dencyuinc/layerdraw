// SPDX-License-Identifier: LicenseRef-LayerDraw-1.0

package engine

import (
	"bytes"
	"context"
	"errors"
	"reflect"
	"strings"
	"testing"
	"unicode/utf8"

	"github.com/dencyuinc/layerdraw/internal/engine/internal/compiler/index"
	"github.com/dencyuinc/layerdraw/internal/engine/internal/compiler/materialize"
	"github.com/dencyuinc/layerdraw/internal/engine/internal/compiler/resolve"
	"github.com/dencyuinc/layerdraw/internal/engine/internal/compiler/syntax"
)

const semanticEditFixture = `project p "Project" {}

// this comment and all unrelated spacing must survive
layers {
  app "Application" @10
  data "Data" @20
}

entity_type service "Service" {
  representation shape rect
  columns {
    note "Note" string
  }
}

relation_type calls "Calls" data_flow {
  allow_self true
  from caller types [service] layers [app, data]
  to callee types [service] layers [app, data]
  label "calls"
  columns {
    weight "Weight" number
  }
}

entities service @app {
  a "A"
  b "B"
}

rows service [note] {
  a primary: "old"
}

relations calls {
  r: a -> b
}

relation_rows calls [weight] {
  r primary: 1.5
}

query q "Query" {
  select {}
}

view v "View" inventory {
  source query q {}
  table {}
}

reference guide <<-TEXT
Keep this reference text unchanged.
TEXT
`

func TestPlanSemanticEditsOperationMatrixAndFullRecompile(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name      string
		operation func(Snapshot) SemanticOperation
		contains  string
		missing   string
	}{
		{name: "update field", operation: func(Snapshot) SemanticOperation {
			value := SemanticValue{Kind: SemanticValueString, String: "A Prime"}
			return SemanticOperation{Kind: OperationUpdateSubjectField, TargetAddress: "ldl:project:p:entity:a", Path: []string{"display_name"}, Action: "set", Value: &value}
		}, contains: `a "A Prime"`},
		{name: "update block field", operation: func(Snapshot) SemanticOperation {
			value := SemanticValue{Kind: SemanticValueString, String: "Entity docs"}
			return SemanticOperation{Kind: OperationUpdateSubjectField, TargetAddress: "ldl:project:p:entity:a", Path: []string{"description"}, Action: "set", Value: &value}
		}, contains: `description "Entity docs"`},
		{name: "update layer order", operation: func(Snapshot) SemanticOperation {
			value := SemanticValue{Kind: SemanticValueInteger, Integer: 11}
			return SemanticOperation{Kind: OperationUpdateSubjectField, TargetAddress: "ldl:project:p:layer:app", Path: []string{"order"}, Action: "set", Value: &value}
		}, contains: `app "Application" @11`},
		{name: "update reference text", operation: func(Snapshot) SemanticOperation {
			value := SemanticValue{Kind: SemanticValueString, String: "Changed reference.\n"}
			return SemanticOperation{Kind: OperationUpdateSubjectField, TargetAddress: "ldl:project:p:reference:guide", Path: []string{"text"}, Action: "set", Value: &value}
		}, contains: `Changed reference.`},
		{name: "update annotation", operation: func(Snapshot) SemanticOperation {
			value := SemanticValue{Kind: SemanticValueString, String: "owned"}
			return SemanticOperation{Kind: OperationUpdateSubjectField, TargetAddress: "ldl:project:p:entity:a", Path: []string{"annotations", "team"}, Action: "set", Value: &value}
		}, contains: `annotations { team: "owned" }`},
		{name: "remove absent annotation", operation: func(Snapshot) SemanticOperation {
			return SemanticOperation{Kind: OperationUpdateSubjectField, TargetAddress: "ldl:project:p:entity:a", Path: []string{"annotations", "team"}, Action: "remove"}
		}, missing: `annotations {`},
		{name: "remove block field", operation: func(Snapshot) SemanticOperation {
			return SemanticOperation{Kind: OperationUpdateSubjectField, TargetAddress: "ldl:project:p:entity:a", Path: []string{"description"}, Action: "remove"}
		}, missing: `description `},
		{name: "update relation row cell", operation: func(Snapshot) SemanticOperation {
			value := SemanticValue{Kind: SemanticValueDecimal, Decimal: "2"}
			return SemanticOperation{Kind: OperationUpdateSubjectField, TargetAddress: "ldl:project:p:relation:r:row:primary", Path: []string{"values", "ldl:project:p:relation-type:calls:column:weight"}, Action: "set", Value: &value}
		}, contains: `r primary: 2`},
		{name: "remove row cell", operation: func(Snapshot) SemanticOperation {
			return SemanticOperation{Kind: OperationUpdateSubjectField, TargetAddress: "ldl:project:p:entity:a:row:primary", Path: []string{"values", "ldl:project:p:entity-type:service:column:note"}, Action: "remove"}
		}, contains: `a primary: _`},
		{name: "rename with references", operation: func(Snapshot) SemanticOperation {
			return SemanticOperation{Kind: OperationRenameSubject, TargetAddress: "ldl:project:p:entity:a", NewID: "alpha"}
		}, contains: `r: alpha -> b`, missing: "\n  a primary:"},
		{name: "delete subject", operation: func(Snapshot) SemanticOperation {
			return SemanticOperation{Kind: OperationDeleteSubject, TargetAddress: "ldl:project:p:reference:guide"}
		}, missing: `reference guide`},
		{name: "create relation", operation: func(Snapshot) SemanticOperation {
			return SemanticOperation{Kind: OperationCreateRelation, ParentAddress: "ldl:project:p", ID: "r2", TypeAddress: "ldl:project:p:relation-type:calls", FromAddress: "ldl:project:p:entity:b", ToAddress: "ldl:project:p:entity:a", Fields: []SemanticMapEntry{{Key: "display_name", Value: SemanticValue{Kind: SemanticValueString, String: "Second"}}, {Key: "description", Value: SemanticValue{Kind: SemanticValueString, String: "relation docs"}}}}
		}, contains: `r2: b -> a`},
		{name: "update relation endpoint", operation: func(Snapshot) SemanticOperation {
			return SemanticOperation{Kind: OperationUpdateRelationEndpoint, RelationAddress: "ldl:project:p:relation:r", Endpoint: "to", EntityAddress: "ldl:project:p:entity:a"}
		}, contains: `r: a -> a`},
		{name: "move entity", operation: func(Snapshot) SemanticOperation {
			return SemanticOperation{Kind: OperationMoveEntityToLayer, EntityAddress: "ldl:project:p:entity:b", LayerAddress: "ldl:project:p:layer:data"}
		}, contains: "entities service @data {\n  b \"B\""},
		{name: "upsert row", operation: func(Snapshot) SemanticOperation {
			return SemanticOperation{Kind: OperationUpsertRow, OwnerAddress: "ldl:project:p:entity:b", ID: "secondary", Values: []SemanticRowCell{{ColumnAddress: "ldl:project:p:entity-type:service:column:note", Value: SemanticValue{Kind: SemanticValueString, String: "new"}}}}
		}, contains: `b secondary: "new"`},
		{name: "upsert existing relation row", operation: func(Snapshot) SemanticOperation {
			return SemanticOperation{Kind: OperationUpsertRow, OwnerAddress: "ldl:project:p:relation:r", ID: "primary", Values: []SemanticRowCell{{ColumnAddress: "ldl:project:p:relation-type:calls:column:weight", Value: SemanticValue{Kind: SemanticValueDecimal, Decimal: "3.5"}}}}
		}, contains: `r primary: 3.5`},
		{name: "delete row", operation: func(Snapshot) SemanticOperation {
			return SemanticOperation{Kind: OperationDeleteRow, RowAddress: "ldl:project:p:entity:a:row:primary"}
		}, missing: `a primary: "old"`},
		{name: "create subject", operation: func(Snapshot) SemanticOperation {
			return SemanticOperation{Kind: OperationCreateSubject, ParentAddress: "ldl:project:p", SubjectKind: SemanticSubjectKind("layer"), ID: "extra", Fields: []SemanticMapEntry{{Key: "display_name", Value: SemanticValue{Kind: SemanticValueString, String: "Extra"}}, {Key: "order", Value: SemanticValue{Kind: SemanticValueInteger, Integer: 30}}}}
		}, contains: `extra "Extra" @30`},
		{name: "create entity type", operation: func(Snapshot) SemanticOperation {
			return SemanticOperation{Kind: OperationCreateSubject, ParentAddress: "ldl:project:p", SubjectKind: SemanticSubjectKind("entity_type"), ID: "component", Fields: []SemanticMapEntry{{Key: "display_name", Value: SemanticValue{Kind: SemanticValueString, String: "Component"}}, {Key: "representation", Value: SemanticValue{Kind: SemanticValueMap, Map: []SemanticMapEntry{{Key: "kind", Value: SemanticValue{Kind: SemanticValueString, String: "container"}}}}}}}
		}, contains: `entity_type component "Component"`},
		{name: "create relation type", operation: func(Snapshot) SemanticOperation {
			endpoint := func(role string) SemanticValue {
				return SemanticValue{Kind: SemanticValueMap, Map: []SemanticMapEntry{{Key: "entity_type_addresses", Value: SemanticValue{Kind: SemanticValueArray, Array: []SemanticValue{{Kind: SemanticValueAddress, Address: "ldl:project:p:entity-type:service"}}}}, {Key: "role", Value: SemanticValue{Kind: SemanticValueString, String: role}}}}
			}
			return SemanticOperation{Kind: OperationCreateSubject, ParentAddress: "ldl:project:p", SubjectKind: SemanticSubjectKind("relation_type"), ID: "links", Fields: []SemanticMapEntry{{Key: "display_name", Value: SemanticValue{Kind: SemanticValueString, String: "Links"}}, {Key: "forward_label", Value: SemanticValue{Kind: SemanticValueString, String: "links"}}, {Key: "from", Value: endpoint("source")}, {Key: "semantic_kind", Value: SemanticValue{Kind: SemanticValueString, String: "reference"}}, {Key: "to", Value: endpoint("target")}}}
		}, contains: `relation_type links "Links" reference`},
		{name: "create entity", operation: func(Snapshot) SemanticOperation {
			return SemanticOperation{Kind: OperationCreateSubject, ParentAddress: "ldl:project:p", SubjectKind: SemanticSubjectKind("entity"), ID: "c", Fields: []SemanticMapEntry{{Key: "display_name", Value: SemanticValue{Kind: SemanticValueString, String: "C"}}, {Key: "layer_address", Value: SemanticValue{Kind: SemanticValueAddress, Address: "ldl:project:p:layer:data"}}, {Key: "type_address", Value: SemanticValue{Kind: SemanticValueAddress, Address: "ldl:project:p:entity-type:service"}}}}
		}, contains: `c "C"`},
		{name: "create query", operation: func(Snapshot) SemanticOperation {
			return SemanticOperation{Kind: OperationCreateSubject, ParentAddress: "ldl:project:p", SubjectKind: SemanticSubjectKind("query"), ID: "empty", Fields: []SemanticMapEntry{{Key: "display_name", Value: SemanticValue{Kind: SemanticValueString, String: "Empty"}}, {Key: "select", Value: SemanticValue{Kind: SemanticValueMap}}}}
		}, contains: `query empty "Empty"`},
		{name: "create view", operation: func(Snapshot) SemanticOperation {
			return SemanticOperation{Kind: OperationCreateSubject, ParentAddress: "ldl:project:p", SubjectKind: SemanticSubjectKind("view"), ID: "table2", Fields: []SemanticMapEntry{{Key: "category", Value: SemanticValue{Kind: SemanticValueString, String: "inventory"}}, {Key: "display_name", Value: SemanticValue{Kind: SemanticValueString, String: "Table 2"}}, {Key: "shape", Value: SemanticValue{Kind: SemanticValueMap, Map: []SemanticMapEntry{{Key: "kind", Value: SemanticValue{Kind: SemanticValueString, String: "table"}}}}}, {Key: "source", Value: SemanticValue{Kind: SemanticValueMap, Map: []SemanticMapEntry{{Key: "kind", Value: SemanticValue{Kind: SemanticValueString, String: "query"}}, {Key: "query_address", Value: SemanticValue{Kind: SemanticValueAddress, Address: "ldl:project:p:query:q"}}}}}}}
		}, contains: `view table2 "Table 2" inventory`},
		{name: "create reference", operation: func(Snapshot) SemanticOperation {
			return SemanticOperation{Kind: OperationCreateSubject, ParentAddress: "ldl:project:p", SubjectKind: SemanticSubjectKind("reference"), ID: "notes", Fields: []SemanticMapEntry{{Key: "text", Value: SemanticValue{Kind: SemanticValueString, String: "Notes body.\n"}}}}
		}, contains: "reference notes <<-LDL_REFERENCE"},
		{name: "create entity type column", operation: func(Snapshot) SemanticOperation {
			return SemanticOperation{Kind: OperationCreateSubject, ParentAddress: "ldl:project:p:entity-type:service", SubjectKind: SemanticSubjectKind("entity_type_column"), ID: "count", Fields: []SemanticMapEntry{{Key: "display_name", Value: SemanticValue{Kind: SemanticValueString, String: "Count"}}, {Key: "value_type", Value: SemanticValue{Kind: SemanticValueString, String: "integer"}}}}
		}, contains: `count "Count" integer`},
		{name: "create entity type constraint", operation: func(Snapshot) SemanticOperation {
			return SemanticOperation{Kind: OperationCreateSubject, ParentAddress: "ldl:project:p:entity-type:service", SubjectKind: SemanticSubjectKind("entity_type_constraint"), ID: "note_unique", Fields: []SemanticMapEntry{{Key: "column_addresses", Value: SemanticValue{Kind: SemanticValueArray, Array: []SemanticValue{{Kind: SemanticValueAddress, Address: "ldl:project:p:entity-type:service:column:note"}}}}}}
		}, contains: `unique note_unique [note]`},
		{name: "create relation type column", operation: func(Snapshot) SemanticOperation {
			return SemanticOperation{Kind: OperationCreateSubject, ParentAddress: "ldl:project:p:relation-type:calls", SubjectKind: SemanticSubjectKind("relation_type_column"), ID: "cost", Fields: []SemanticMapEntry{{Key: "display_name", Value: SemanticValue{Kind: SemanticValueString, String: "Cost"}}, {Key: "value_type", Value: SemanticValue{Kind: SemanticValueString, String: "number"}}}}
		}, contains: `cost "Cost" number`},
		{name: "create relation type constraint", operation: func(Snapshot) SemanticOperation {
			return SemanticOperation{Kind: OperationCreateSubject, ParentAddress: "ldl:project:p:relation-type:calls", SubjectKind: SemanticSubjectKind("relation_type_constraint"), ID: "weight_unique", Fields: []SemanticMapEntry{{Key: "column_addresses", Value: SemanticValue{Kind: SemanticValueArray, Array: []SemanticValue{{Kind: SemanticValueAddress, Address: "ldl:project:p:relation-type:calls:column:weight"}}}}}}
		}, contains: `unique weight_unique [weight]`},
		{name: "create query parameter", operation: func(Snapshot) SemanticOperation {
			return SemanticOperation{Kind: OperationCreateSubject, ParentAddress: "ldl:project:p:query:q", SubjectKind: SemanticSubjectKind("query_parameter"), ID: "limit", Fields: []SemanticMapEntry{{Key: "value_type", Value: SemanticValue{Kind: SemanticValueString, String: "integer"}}}}
		}, contains: `limit integer`},
		{name: "create view column", operation: func(Snapshot) SemanticOperation {
			return SemanticOperation{Kind: OperationCreateSubject, ParentAddress: "ldl:project:p:view:v", SubjectKind: SemanticSubjectKind("view_table_column"), ID: "name", Fields: []SemanticMapEntry{{Key: "source", Value: SemanticValue{Kind: SemanticValueMap, Map: []SemanticMapEntry{{Key: "field", Value: SemanticValue{Kind: SemanticValueString, String: "display_name"}}, {Key: "kind", Value: SemanticValue{Kind: SemanticValueString, String: "field"}}}}}}}
		}, contains: `column name`},
		{name: "create view export", operation: func(Snapshot) SemanticOperation {
			return SemanticOperation{Kind: OperationCreateSubject, ParentAddress: "ldl:project:p:view:v", SubjectKind: SemanticSubjectKind("view_export"), ID: "data", Fields: []SemanticMapEntry{{Key: "fidelity", Value: SemanticValue{Kind: SemanticValueString, String: "lossless"}}, {Key: "filename", Value: SemanticValue{Kind: SemanticValueString, String: "data.json"}}, {Key: "format", Value: SemanticValue{Kind: SemanticValueString, String: "json"}}, {Key: "source_refs", Value: SemanticValue{Kind: SemanticValueBoolean, Boolean: true}}}}
		}, contains: `export data json "data.json"`},
		{name: "migrate project identity", operation: func(Snapshot) SemanticOperation {
			return SemanticOperation{Kind: OperationMigrateProjectIdentity, ProjectAddress: "ldl:project:p", NewProjectID: "next"}
		}, contains: `project next "Project"`},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			planner := New(BuildInfo{})
			input := projectCompileInput(semanticEditFixture)
			compiled, err := planner.Compile(context.Background(), input)
			if err != nil || len(compiled.Diagnostics) != 0 {
				t.Fatalf("base compile: %v %+v", err, compiled.Diagnostics)
			}
			base := compiled.Snapshot()
			operation := test.operation(base)
			plan, err := planner.PlanSemanticEdits(context.Background(), SemanticEditPlanInput{BaseInput: input, BaseSnapshot: base, Batch: SemanticOperationBatch{Operations: []SemanticOperation{operation}}, Preconditions: allSemanticPreconditions(base)})
			if err != nil {
				t.Fatal(err)
			}
			if plan.Status != "valid" || plan.Result == nil || len(plan.Diagnostics) != 0 || len(plan.Conflicts) != 0 {
				t.Fatalf("plan=%+v", plan)
			}
			got := string(plan.SourceTree["document.ldl"])
			if test.contains != "" && !strings.Contains(got, test.contains) {
				t.Fatalf("source missing %q:\n%s", test.contains, got)
			}
			if test.missing != "" && strings.Contains(got, test.missing) {
				t.Fatalf("source retained %q:\n%s", test.missing, got)
			}
			if !strings.Contains(got, "// this comment and all unrelated spacing must survive") {
				t.Fatal("unrelated comment churned")
			}
			recompileInput := cloneSemanticCompileInput(input)
			recompileInput.ProjectSourceTree = plan.SourceTree
			recompiled, compileErr := planner.Compile(context.Background(), recompileInput)
			if compileErr != nil || len(recompiled.Diagnostics) != 0 || recompiled.DefinitionHash != plan.Result.DefinitionHash {
				t.Fatalf("full recompilation mismatch err=%v diagnostics=%+v", compileErr, recompiled.Diagnostics)
			}
			if plan.SourceDiff.Digest == "" || plan.SemanticDiff.Digest == "" || plan.AuthoringImpact == nil || plan.AuthoringImpact.ImpactDigest == "" {
				t.Fatal("deterministic derived output incomplete")
			}
		})
	}
}

func TestPlanSemanticEditsStalePreconditionsAtomicityAndCancellation(t *testing.T) {
	t.Parallel()
	planner := New(BuildInfo{})
	input := projectCompileInput(semanticEditFixture)
	result, err := planner.Compile(context.Background(), input)
	if err != nil {
		t.Fatal(err)
	}
	base := result.Snapshot()
	original := bytes.Clone(input.ProjectSourceTree["document.ldl"])
	value := SemanticValue{Kind: SemanticValueString, String: "Changed"}
	operation := SemanticOperation{Kind: OperationUpdateSubjectField, TargetAddress: "ldl:project:p:entity:a", Path: []string{"display_name"}, Action: "set", Value: &value}
	pre := allSemanticPreconditions(base)
	for i := range pre.ExpectedSubjectHashes {
		if pre.ExpectedSubjectHashes[i].Address == operation.TargetAddress {
			pre.ExpectedSubjectHashes[i].Hash = "sha256:" + strings.Repeat("0", 64)
		}
	}
	plan, err := planner.PlanSemanticEdits(context.Background(), SemanticEditPlanInput{BaseInput: input, BaseSnapshot: base, Batch: SemanticOperationBatch{Operations: []SemanticOperation{operation}}, Preconditions: pre})
	if err != nil || plan.Status != "invalid" || len(plan.Conflicts) != 1 || plan.Conflicts[0].Kind != ConflictSubjectChanged {
		t.Fatalf("stale plan=%+v err=%v", plan, err)
	}
	if !bytes.Equal(input.ProjectSourceTree["document.ldl"], original) {
		t.Fatal("rejected plan mutated caller source")
	}

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err = planner.PlanSemanticEdits(ctx, SemanticEditPlanInput{BaseInput: input, BaseSnapshot: base, Batch: SemanticOperationBatch{Operations: []SemanticOperation{operation}}, Preconditions: allSemanticPreconditions(base)})
	if !errors.Is(err, context.Canceled) || !IsCompileError(err, ErrorCategoryCancelled) {
		t.Fatalf("cancellation=%v", err)
	}
}

func TestPlanSemanticEditsDeterministicAndUnchangedBytes(t *testing.T) {
	t.Parallel()
	planner := New(BuildInfo{})
	input := projectCompileInput(semanticEditFixture)
	compiled, _ := planner.Compile(context.Background(), input)
	base := compiled.Snapshot()
	value := SemanticValue{Kind: SemanticValueString, String: "A Prime"}
	op := SemanticOperation{Kind: OperationUpdateSubjectField, TargetAddress: "ldl:project:p:entity:a", Path: []string{"display_name"}, Action: "set", Value: &value}
	request := SemanticEditPlanInput{BaseInput: input, BaseSnapshot: base, Batch: SemanticOperationBatch{Operations: []SemanticOperation{op}}, Preconditions: allSemanticPreconditions(base)}
	first, err := planner.PlanSemanticEdits(context.Background(), request)
	if err != nil {
		t.Fatal(err)
	}
	second, err := planner.PlanSemanticEdits(context.Background(), request)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(first.SourceTree["document.ldl"], second.SourceTree["document.ldl"]) || first.SourceDiff.Digest != second.SourceDiff.Digest || first.SemanticDiff.Digest != second.SemanticDiff.Digest || first.AuthoringImpact.ImpactDigest != second.AuthoringImpact.ImpactDigest {
		t.Fatal("planner output is nondeterministic")
	}
	want := strings.Replace(semanticEditFixture, `a "A"`, `a "A Prime"`, 1)
	if got := string(first.SourceTree["document.ldl"]); got != want {
		t.Fatalf("unchanged bytes churned\n--- want\n%s\n--- got\n%s", want, got)
	}
}

func TestPlanSemanticEditsIndependentOperationOrderAndSchemaImpact(t *testing.T) {
	t.Parallel()
	planner := New(BuildInfo{})
	input := projectCompileInput(semanticEditFixture)
	compiled, _ := planner.Compile(context.Background(), input)
	base := compiled.Snapshot()
	valueA := SemanticValue{Kind: SemanticValueString, String: "A Prime"}
	valueB := SemanticValue{Kind: SemanticValueString, String: "B Prime"}
	a := SemanticOperation{Kind: OperationUpdateSubjectField, TargetAddress: "ldl:project:p:entity:a", Path: []string{"display_name"}, Action: "set", Value: &valueA}
	b := SemanticOperation{Kind: OperationUpdateSubjectField, TargetAddress: "ldl:project:p:entity:b", Path: []string{"display_name"}, Action: "set", Value: &valueB}
	var baseline SemanticEditPlan
	for index, operations := range [][]SemanticOperation{{a, b}, {b, a}} {
		plan, err := planner.PlanSemanticEdits(context.Background(), SemanticEditPlanInput{BaseInput: input, BaseSnapshot: base, Batch: SemanticOperationBatch{Operations: operations}, Preconditions: allSemanticPreconditions(base)})
		if err != nil || plan.Status != "valid" {
			t.Fatalf("permutation %d: plan=%+v err=%v", index, plan, err)
		}
		if index == 0 {
			baseline = plan
		} else if !bytes.Equal(plan.SourceTree["document.ldl"], baseline.SourceTree["document.ldl"]) || plan.SemanticDiff.Digest != baseline.SemanticDiff.Digest || plan.AuthoringImpact.ImpactDigest != baseline.AuthoringImpact.ImpactDigest {
			t.Fatal("independent operation order changed canonical output")
		}
	}

	bound := func(min int64, max SemanticValue) SemanticValue {
		return SemanticValue{Kind: SemanticValueMap, Map: []SemanticMapEntry{{Key: "max", Value: max}, {Key: "min", Value: SemanticValue{Kind: SemanticValueInteger, Integer: min}}}}
	}
	cardinality := SemanticValue{Kind: SemanticValueMap, Map: []SemanticMapEntry{{Key: "from_per_to", Value: bound(0, SemanticValue{Kind: SemanticValueInteger, Integer: 1})}, {Key: "to_per_from", Value: bound(0, SemanticValue{Kind: SemanticValueInteger, Integer: 1})}}}
	op := SemanticOperation{Kind: OperationUpdateSubjectField, TargetAddress: "ldl:project:p:relation-type:calls", Path: []string{"cardinality"}, Action: "set", Value: &cardinality}
	plan, err := planner.PlanSemanticEdits(context.Background(), SemanticEditPlanInput{BaseInput: input, BaseSnapshot: base, Batch: SemanticOperationBatch{Operations: []SemanticOperation{op}}, Preconditions: allSemanticPreconditions(base)})
	if err != nil || plan.Status != "valid" {
		t.Fatalf("cardinality plan=%+v err=%v", plan, err)
	}
	if !strings.Contains(string(plan.SourceTree["document.ldl"]), "to_per_from 0..1") {
		t.Fatal("cardinality was not canonically rendered")
	}
	foundSchema := false
	for _, entry := range plan.AuthoringImpact.Entries {
		if entry.SubjectAddress == "ldl:project:p:relation-type:calls" && entry.Capability == CapabilitySchemaWrite {
			foundSchema = true
		}
	}
	if !foundSchema {
		t.Fatalf("schema impact missing: %+v", plan.AuthoringImpact)
	}
}

func TestPlanSemanticEditsCanonicalizesRenameAndReservationOrder(t *testing.T) {
	t.Parallel()
	planner := New(BuildInfo{})
	input, snapshot := semanticEditCompiledFixture(t)
	rename := SemanticOperation{Kind: OperationRenameSubject, TargetAddress: "ldl:project:p:entity:a", NewID: "alpha"}
	removeReference := SemanticOperation{Kind: OperationDeleteSubject, TargetAddress: "ldl:project:p:reference:guide"}

	var baseline SemanticEditPlan
	for index, operations := range [][]SemanticOperation{{rename, removeReference}, {removeReference, rename}} {
		plan, err := planner.PlanSemanticEdits(context.Background(), SemanticEditPlanInput{BaseInput: input, BaseSnapshot: snapshot, Batch: SemanticOperationBatch{Operations: operations}, Preconditions: allSemanticPreconditions(snapshot)})
		if err != nil || plan.Status != "valid" {
			t.Fatalf("permutation %d failed: status=%s err=%v diagnostics=%+v", index, plan.Status, err, plan.Diagnostics)
		}
		if index == 0 {
			baseline = plan
			continue
		}
		if !bytes.Equal(plan.SourceTree["document.ldl"], baseline.SourceTree["document.ldl"]) || plan.SourceDiff.Digest != baseline.SourceDiff.Digest || plan.SemanticDiff.Digest != baseline.SemanticDiff.Digest || plan.AuthoringImpact.ImpactDigest != baseline.AuthoringImpact.ImpactDigest {
			t.Fatalf("rename/delete order changed canonical output\nfirst:\n%s\nsecond:\n%s", baseline.SourceTree["document.ldl"], plan.SourceTree["document.ldl"])
		}
	}
}

func TestPlanSemanticEditsCanonicalizesMultiCapabilityImpact(t *testing.T) {
	t.Parallel()
	planner := New(BuildInfo{})
	input, snapshot := semanticEditCompiledFixture(t)
	text := func(value string) *SemanticValue {
		return &SemanticValue{Kind: SemanticValueString, String: value}
	}
	operations := []SemanticOperation{
		{Kind: OperationUpdateSubjectField, TargetAddress: "ldl:project:p:view:v", Path: []string{"description"}, Action: "set", Value: text("view docs")},
		{Kind: OperationUpdateSubjectField, TargetAddress: "ldl:project:p:entity:a", Path: []string{"description"}, Action: "set", Value: text("entity docs")},
		{Kind: OperationUpdateSubjectField, TargetAddress: "ldl:project:p:reference:guide", Path: []string{"text"}, Action: "set", Value: text("reference docs\n")},
		{Kind: OperationUpdateSubjectField, TargetAddress: "ldl:project:p:entity-type:service", Path: []string{"description"}, Action: "set", Value: text("schema docs")},
		{Kind: OperationUpdateSubjectField, TargetAddress: "ldl:project:p:query:q", Path: []string{"description"}, Action: "set", Value: text("query docs")},
	}
	plan, err := planner.PlanSemanticEdits(context.Background(), SemanticEditPlanInput{
		BaseInput:     input,
		BaseSnapshot:  snapshot,
		Batch:         SemanticOperationBatch{Operations: operations},
		Preconditions: allSemanticPreconditions(snapshot),
	})
	if err != nil || plan.Status != "valid" {
		t.Fatalf("plan=%+v err=%v", plan, err)
	}
	wantCapabilities := []AuthoringCapability{
		CapabilityGraphWrite,
		CapabilityQueryWrite,
		CapabilityReferenceWrite,
		CapabilitySchemaWrite,
		CapabilityViewWrite,
	}
	if !reflect.DeepEqual(plan.AuthoringImpact.RequiredCapabilities, wantCapabilities) {
		t.Fatalf("capabilities=%v want=%v", plan.AuthoringImpact.RequiredCapabilities, wantCapabilities)
	}
	for i := 1; i < len(plan.AuthoringImpact.Entries); i++ {
		previous := plan.AuthoringImpact.Entries[i-1]
		current := plan.AuthoringImpact.Entries[i]
		if compareStableAddressText(previous.SubjectAddress, current.SubjectAddress) > 0 {
			t.Fatalf("impact entries are not in canonical StableAddress order at %d: %q before %q", i, previous.SubjectAddress, current.SubjectAddress)
		}
	}
}

func TestPlanSemanticEditsFailsClosedOnMissingOwnerAndChildSetAuthority(t *testing.T) {
	t.Parallel()
	planner := New(BuildInfo{})
	input := projectCompileInput(semanticEditFixture)
	compiled, _ := planner.Compile(context.Background(), input)
	base := compiled.Snapshot()
	op := SemanticOperation{Kind: OperationCreateSubject, ParentAddress: "ldl:project:p:entity:a", SubjectKind: SemanticSubjectKind("layer"), ID: "bad", Fields: []SemanticMapEntry{{Key: "display_name", Value: SemanticValue{Kind: SemanticValueString, String: "Bad"}}, {Key: "order", Value: SemanticValue{Kind: SemanticValueInteger, Integer: 99}}}}
	plan, err := planner.PlanSemanticEdits(context.Background(), SemanticEditPlanInput{BaseInput: input, BaseSnapshot: base, Batch: SemanticOperationBatch{Operations: []SemanticOperation{op}}, Preconditions: allSemanticPreconditions(base)})
	if err != nil || plan.Status != "invalid" || len(plan.Conflicts) != 1 || plan.Conflicts[0].Kind != ConflictChildSetChanged {
		t.Fatalf("owner authority plan=%+v err=%v", plan, err)
	}

	valid := SemanticOperation{Kind: OperationCreateSubject, ParentAddress: "ldl:project:p", SubjectKind: SemanticSubjectKind("layer"), ID: "new_layer", Fields: []SemanticMapEntry{{Key: "display_name", Value: SemanticValue{Kind: SemanticValueString, String: "New"}}, {Key: "order", Value: SemanticValue{Kind: SemanticValueInteger, Integer: 99}}}}
	pre := allSemanticPreconditions(base)
	filtered := pre.ExpectedChildSets[:0]
	for _, item := range pre.ExpectedChildSets {
		if !(item.OwnerAddress == "ldl:project:p" && item.ChildKind == materialize.SubjectLayer) {
			filtered = append(filtered, item)
		}
	}
	pre.ExpectedChildSets = filtered
	plan, err = planner.PlanSemanticEdits(context.Background(), SemanticEditPlanInput{BaseInput: input, BaseSnapshot: base, Batch: SemanticOperationBatch{Operations: []SemanticOperation{valid}}, Preconditions: pre})
	if err != nil || plan.Status != "invalid" || len(plan.Conflicts) != 1 || plan.Conflicts[0].Kind != ConflictChildSetChanged {
		t.Fatalf("missing child-set plan=%+v err=%v", plan, err)
	}
}

func TestPlanSemanticEditsAllowsTemporarilyInvalidAtomicDeleteBatch(t *testing.T) {
	t.Parallel()
	planner := New(BuildInfo{})
	input := projectCompileInput(semanticEditFixture)
	compiled, _ := planner.Compile(context.Background(), input)
	base := compiled.Snapshot()
	// Deleting the referenced type first is invalid in isolation. The batch is
	// nevertheless valid after its relation instance is removed.
	operations := []SemanticOperation{{Kind: OperationDeleteSubject, TargetAddress: "ldl:project:p:relation-type:calls"}, {Kind: OperationDeleteRow, RowAddress: "ldl:project:p:relation:r:row:primary"}, {Kind: OperationDeleteSubject, TargetAddress: "ldl:project:p:relation:r"}}
	plan, err := planner.PlanSemanticEdits(context.Background(), SemanticEditPlanInput{BaseInput: input, BaseSnapshot: base, Batch: SemanticOperationBatch{Operations: operations}, Preconditions: allSemanticPreconditions(base)})
	if err != nil || plan.Status != "valid" {
		t.Fatalf("atomic delete plan=%+v err=%v", plan, err)
	}
	got := string(plan.SourceTree["document.ldl"])
	if strings.Contains(got, "relation_type calls") || strings.Contains(got, "r: a -> b") {
		t.Fatalf("atomic delete incomplete:\n%s", got)
	}
}

func TestPlanSemanticEditsPreservesAuthoredIdentityHistory(t *testing.T) {
	t.Parallel()
	planner := New(BuildInfo{})
	input, snapshot := semanticEditCompiledFixture(t)

	t.Run("rename writes moves and uses authored lineage", func(t *testing.T) {
		operation := SemanticOperation{Kind: OperationRenameSubject, TargetAddress: "ldl:project:p:entity:a", NewID: "alpha"}
		plan, err := planner.PlanSemanticEdits(context.Background(), SemanticEditPlanInput{
			BaseInput:     input,
			BaseSnapshot:  snapshot,
			Batch:         SemanticOperationBatch{Operations: []SemanticOperation{operation}},
			Preconditions: allSemanticPreconditions(snapshot),
		})
		if err != nil || plan.Status != "valid" {
			t.Fatalf("rename plan failed: status=%s err=%v conflicts=%+v diagnostics=%+v", plan.Status, err, plan.Conflicts, plan.Diagnostics)
		}
		if !strings.Contains(string(plan.SourceTree["document.ldl"]), "entity a -> alpha") {
			t.Fatalf("rename did not materialize a durable move:\n%s", plan.SourceTree["document.ldl"])
		}
		found := false
		for _, entry := range plan.SemanticDiff.Entries {
			if entry.BeforeAddress == "ldl:project:p:entity:a" && entry.AfterAddress == "ldl:project:p:entity:alpha" && entry.Kind == SemanticRenamed {
				found = true
			}
		}
		if !found {
			t.Fatalf("semantic diff omitted authored rename lineage: %+v", plan.SemanticDiff.Entries)
		}
	})

	t.Run("structurally similar delete and create stay distinct", func(t *testing.T) {
		operations := []SemanticOperation{
			{Kind: OperationDeleteSubject, TargetAddress: "ldl:project:p:reference:guide"},
			{Kind: OperationCreateSubject, ParentAddress: "ldl:project:p", SubjectKind: materialize.SubjectReference, ID: "notes", Fields: []SemanticMapEntry{{Key: "text", Value: SemanticValue{Kind: SemanticValueString, String: "Keep this reference text unchanged.\n"}}}},
		}
		plan, err := planner.PlanSemanticEdits(context.Background(), SemanticEditPlanInput{
			BaseInput:     input,
			BaseSnapshot:  snapshot,
			Batch:         SemanticOperationBatch{Operations: operations},
			Preconditions: allSemanticPreconditions(snapshot),
		})
		if err != nil || plan.Status != "valid" {
			t.Fatalf("delete/create plan failed: status=%s err=%v diagnostics=%+v", plan.Status, err, plan.Diagnostics)
		}
		kinds := map[SemanticChangeKind]int{}
		for _, entry := range plan.SemanticDiff.Entries {
			if entry.SubjectKind == materialize.SubjectReference {
				kinds[entry.Kind]++
			}
		}
		if kinds[SemanticDeleted] != 1 || kinds[SemanticCreated] != 1 || kinds[SemanticRenamed] != 0 {
			t.Fatalf("identity was inferred instead of authored: %+v", plan.SemanticDiff.Entries)
		}
		if !strings.Contains(string(plan.SourceTree["document.ldl"]), "references [guide]") {
			t.Fatalf("delete did not reserve the committed reference identity:\n%s", plan.SourceTree["document.ldl"])
		}
	})
}

func TestPlanSemanticEditsRebasesIndependentFieldsAndRejectsSameFieldChanges(t *testing.T) {
	t.Parallel()
	planner := New(BuildInfo{})
	ancestorInput, ancestor := semanticEditCompiledFixture(t)
	headInput := cloneSemanticCompileInput(ancestorInput)
	headInput.ProjectSourceTree["document.ldl"] = bytes.Replace(headInput.ProjectSourceTree["document.ldl"], []byte(`a "A"`), []byte(`a "Head name"`), 1)
	headResult, err := planner.Compile(context.Background(), headInput)
	if err != nil || len(headResult.Diagnostics) != 0 {
		t.Fatalf("compile current head: err=%v diagnostics=%+v", err, headResult.Diagnostics)
	}
	authority := &SemanticRebaseAuthority{
		AncestorGeneration:     SemanticDocumentGeneration{EndpointInstanceID: "test-endpoint", DocumentHandle: "document_abcdefghijklmnop", Value: "1"},
		CurrentGeneration:      SemanticDocumentGeneration{EndpointInstanceID: "test-endpoint", DocumentHandle: "document_abcdefghijklmnop", Value: "2"},
		AncestorDefinitionHash: ancestor.DefinitionHash,
		CurrentDefinitionHash:  headResult.Snapshot().DefinitionHash,
	}

	t.Run("independent field merges", func(t *testing.T) {
		value := SemanticValue{Kind: SemanticValueString, String: "Merged description"}
		operation := SemanticOperation{Kind: OperationUpdateSubjectField, TargetAddress: "ldl:project:p:entity:a", Path: []string{"description"}, Action: "set", Value: &value}
		plan, planErr := planner.PlanSemanticEdits(context.Background(), SemanticEditPlanInput{BaseInput: headInput, BaseSnapshot: ancestor, Batch: SemanticOperationBatch{Operations: []SemanticOperation{operation}}, Preconditions: allSemanticPreconditions(ancestor), Generation: authority.CurrentGeneration, RebaseAuthority: authority})
		if planErr != nil || plan.Status != "valid" {
			t.Fatalf("independent rebase failed: status=%s err=%v conflicts=%+v", plan.Status, planErr, plan.Conflicts)
		}
		source := string(plan.SourceTree["document.ldl"])
		if !strings.Contains(source, `a "Head name"`) || !strings.Contains(source, `description "Merged description"`) {
			t.Fatalf("rebased source lost either edit:\n%s", source)
		}
	})

	t.Run("same field conflicts", func(t *testing.T) {
		value := SemanticValue{Kind: SemanticValueString, String: "Batch name"}
		operation := SemanticOperation{Kind: OperationUpdateSubjectField, TargetAddress: "ldl:project:p:entity:a", Path: []string{"display_name"}, Action: "set", Value: &value}
		plan, planErr := planner.PlanSemanticEdits(context.Background(), SemanticEditPlanInput{BaseInput: headInput, BaseSnapshot: ancestor, Batch: SemanticOperationBatch{Operations: []SemanticOperation{operation}}, Preconditions: allSemanticPreconditions(ancestor), Generation: authority.CurrentGeneration, RebaseAuthority: authority})
		if planErr != nil {
			t.Fatal(planErr)
		}
		if plan.Status != "invalid" || len(plan.Conflicts) != 1 || plan.Conflicts[0].Kind != ConflictSameFieldChanged {
			t.Fatalf("same-field edit did not fail closed: %+v", plan)
		}
	})

	t.Run("independent incoming referencer change merges", func(t *testing.T) {
		referencerInput := cloneSemanticCompileInput(ancestorInput)
		referencerInput.ProjectSourceTree["document.ldl"] = bytes.Replace(referencerInput.ProjectSourceTree["document.ldl"], []byte("r: a -> b"), []byte("r: a -> b {\n    description \"concurrent\"\n  }"), 1)
		referencerResult, compileErr := planner.Compile(context.Background(), referencerInput)
		if compileErr != nil || len(referencerResult.Diagnostics) != 0 {
			t.Fatalf("compile referencer head: err=%v diagnostics=%+v", compileErr, referencerResult.Diagnostics)
		}
		referencerAuthority := &SemanticRebaseAuthority{AncestorGeneration: authority.AncestorGeneration, CurrentGeneration: authority.CurrentGeneration, AncestorDefinitionHash: ancestor.DefinitionHash, CurrentDefinitionHash: referencerResult.Snapshot().DefinitionHash}
		value := SemanticValue{Kind: SemanticValueString, String: "Merged description"}
		operation := SemanticOperation{Kind: OperationUpdateSubjectField, TargetAddress: "ldl:project:p:entity:a", Path: []string{"description"}, Action: "set", Value: &value}
		plan, planErr := planner.PlanSemanticEdits(context.Background(), SemanticEditPlanInput{BaseInput: referencerInput, BaseSnapshot: ancestor, Batch: SemanticOperationBatch{Operations: []SemanticOperation{operation}}, Preconditions: allSemanticPreconditions(ancestor), Generation: referencerAuthority.CurrentGeneration, RebaseAuthority: referencerAuthority})
		if planErr != nil || plan.Status != "valid" {
			t.Fatalf("independent referencer edit was overvalidated: status=%s err=%v conflicts=%+v", plan.Status, planErr, plan.Conflicts)
		}
		if source := string(plan.SourceTree["document.ldl"]); !strings.Contains(source, `description "concurrent"`) || !strings.Contains(source, `description "Merged description"`) {
			t.Fatalf("three-way merge lost an independent edit:\n%s", source)
		}
	})
}

func TestPlanSemanticEditsRebaseExemptsSameBatchCreatedTargets(t *testing.T) {
	t.Parallel()
	planner := New(BuildInfo{})
	ancestorInput, ancestor := semanticEditCompiledFixture(t)
	headInput := cloneSemanticCompileInput(ancestorInput)
	headInput.ProjectSourceTree["document.ldl"] = bytes.Replace(headInput.ProjectSourceTree["document.ldl"], []byte(`query q "Query"`), []byte(`query q "Concurrent Query"`), 1)
	headResult, err := planner.Compile(context.Background(), headInput)
	if err != nil || len(headResult.Diagnostics) != 0 {
		t.Fatalf("compile current head: err=%v diagnostics=%+v", err, headResult.Diagnostics)
	}
	authority := &SemanticRebaseAuthority{
		AncestorGeneration:     SemanticDocumentGeneration{EndpointInstanceID: "test-endpoint", DocumentHandle: "document_abcdefghijklmnop", Value: "1"},
		CurrentGeneration:      SemanticDocumentGeneration{EndpointInstanceID: "test-endpoint", DocumentHandle: "document_abcdefghijklmnop", Value: "2"},
		AncestorDefinitionHash: ancestor.DefinitionHash,
		CurrentDefinitionHash:  headResult.Snapshot().DefinitionHash,
	}
	create := SemanticOperation{Kind: OperationCreateSubject, SubjectKind: materialize.SubjectLayer, ParentAddress: "ldl:project:p", ID: "temporary", Fields: []SemanticMapEntry{{Key: "display_name", Value: SemanticValue{Kind: SemanticValueString, String: "Temporary"}}, {Key: "order", Value: SemanticValue{Kind: SemanticValueInteger, Integer: 30}}}}
	tests := []struct {
		name      string
		operation SemanticOperation
	}{
		{name: "rename", operation: SemanticOperation{Kind: OperationRenameSubject, TargetAddress: "ldl:project:p:layer:temporary", NewID: "renamed"}},
		{name: "delete", operation: SemanticOperation{Kind: OperationDeleteSubject, TargetAddress: "ldl:project:p:layer:temporary"}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			plan, planErr := planner.PlanSemanticEdits(context.Background(), SemanticEditPlanInput{BaseInput: headInput, BaseSnapshot: ancestor, Batch: SemanticOperationBatch{Operations: []SemanticOperation{create, test.operation}}, Preconditions: allSemanticPreconditions(ancestor), Generation: authority.CurrentGeneration, RebaseAuthority: authority})
			if planErr != nil || plan.Status != "valid" {
				t.Fatalf("same-batch create then %s required nonexistent ancestor authority: status=%s err=%v conflicts=%+v diagnostics=%+v", test.name, plan.Status, planErr, plan.Conflicts, plan.Diagnostics)
			}
		})
	}
}

func TestPlanSemanticEditsRebaseRequiresRetainedAuthorityAndClosedDependencies(t *testing.T) {
	t.Parallel()
	planner := New(BuildInfo{})
	ancestorInput, ancestor := semanticEditCompiledFixture(t)
	generationOne := SemanticDocumentGeneration{EndpointInstanceID: "test-endpoint", DocumentHandle: "document_abcdefghijklmnop", Value: "1"}
	generationTwo := SemanticDocumentGeneration{EndpointInstanceID: "test-endpoint", DocumentHandle: "document_abcdefghijklmnop", Value: "2"}

	compileHead := func(t *testing.T, old, replacement string) (CompileInput, Snapshot, *SemanticRebaseAuthority) {
		t.Helper()
		headInput := cloneSemanticCompileInput(ancestorInput)
		headInput.ProjectSourceTree["document.ldl"] = bytes.Replace(headInput.ProjectSourceTree["document.ldl"], []byte(old), []byte(replacement), 1)
		compiled, err := planner.Compile(context.Background(), headInput)
		if err != nil || len(compiled.Diagnostics) != 0 {
			t.Fatalf("compile concurrent head: err=%v diagnostics=%+v", err, compiled.Diagnostics)
		}
		head := compiled.Snapshot()
		authority := &SemanticRebaseAuthority{AncestorGeneration: generationOne, CurrentGeneration: generationTwo, AncestorDefinitionHash: ancestor.DefinitionHash, CurrentDefinitionHash: head.DefinitionHash}
		return headInput, head, authority
	}

	t.Run("unrelated snapshot without retained generation authority is stale", func(t *testing.T) {
		headInput, _, _ := compileHead(t, `a "A"`, `a "Concurrent"`)
		value := SemanticValue{Kind: SemanticValueString, String: "description"}
		operation := SemanticOperation{Kind: OperationUpdateSubjectField, TargetAddress: "ldl:project:p:entity:a", Path: []string{"description"}, Action: "set", Value: &value}
		plan, err := planner.PlanSemanticEdits(context.Background(), SemanticEditPlanInput{BaseInput: headInput, BaseSnapshot: ancestor, Generation: generationTwo, Batch: SemanticOperationBatch{Operations: []SemanticOperation{operation}}, Preconditions: allSemanticPreconditions(ancestor)})
		if err != nil || plan.Status != "invalid" || len(plan.Conflicts) != 1 || plan.Conflicts[0].Kind != ConflictStaleRevision {
			t.Fatalf("unproven ancestor snapshot was accepted: plan=%+v err=%v", plan, err)
		}
	})

	t.Run("requested generation must be the retained current head", func(t *testing.T) {
		headInput, _, authority := compileHead(t, `a "A"`, `a "Concurrent"`)
		value := SemanticValue{Kind: SemanticValueString, String: "description"}
		operation := SemanticOperation{Kind: OperationUpdateSubjectField, TargetAddress: "ldl:project:p:entity:a", Path: []string{"description"}, Action: "set", Value: &value}
		plan, err := planner.PlanSemanticEdits(context.Background(), SemanticEditPlanInput{BaseInput: headInput, BaseSnapshot: ancestor, Generation: generationOne, RebaseAuthority: authority, Batch: SemanticOperationBatch{Operations: []SemanticOperation{operation}}, Preconditions: allSemanticPreconditions(ancestor)})
		if err != nil || plan.Status != "invalid" || len(plan.Conflicts) != 1 || plan.Conflicts[0].Kind != ConflictStaleRevision {
			t.Fatalf("mismatched current generation was accepted: plan=%+v err=%v", plan, err)
		}
	})

	t.Run("rename detects concurrent reference source changes", func(t *testing.T) {
		headInput, _, authority := compileHead(t, `r: a -> b`, `r: b -> a`)
		operation := SemanticOperation{Kind: OperationRenameSubject, TargetAddress: "ldl:project:p:entity:a", NewID: "alpha"}
		plan, err := planner.PlanSemanticEdits(context.Background(), SemanticEditPlanInput{BaseInput: headInput, BaseSnapshot: ancestor, Generation: generationTwo, RebaseAuthority: authority, Batch: SemanticOperationBatch{Operations: []SemanticOperation{operation}}, Preconditions: allSemanticPreconditions(ancestor)})
		if err != nil || plan.Status != "invalid" {
			t.Fatalf("concurrent reference edit was not rejected: plan=%+v err=%v", plan, err)
		}
		found := false
		for _, conflict := range plan.Conflicts {
			found = found || (conflict.Kind == ConflictSubjectChanged && conflict.TargetAddress == "ldl:project:p:relation:r")
		}
		if !found {
			t.Fatalf("rename conflict omitted changed reference source: %+v", plan.Conflicts)
		}
	})

	t.Run("row update detects concurrent owner schema changes", func(t *testing.T) {
		headInput, _, authority := compileHead(t, `    note "Note" string`, "    note \"Note\" string\n    extra \"Extra\" string")
		value := SemanticValue{Kind: SemanticValueString, String: "batch"}
		operation := SemanticOperation{Kind: OperationUpdateSubjectField, TargetAddress: "ldl:project:p:entity:a:row:primary", Path: []string{"values", "ldl:project:p:entity-type:service:column:note"}, Action: "set", Value: &value}
		plan, err := planner.PlanSemanticEdits(context.Background(), SemanticEditPlanInput{BaseInput: headInput, BaseSnapshot: ancestor, Generation: generationTwo, RebaseAuthority: authority, Batch: SemanticOperationBatch{Operations: []SemanticOperation{operation}}, Preconditions: allSemanticPreconditions(ancestor)})
		if err != nil || plan.Status != "invalid" {
			t.Fatalf("concurrent row schema edit was not rejected: plan=%+v err=%v", plan, err)
		}
		found := false
		for _, conflict := range plan.Conflicts {
			found = found || (conflict.Kind == ConflictSubtreeChanged && conflict.TargetAddress == "ldl:project:p:entity-type:service")
		}
		if !found {
			t.Fatalf("row conflict omitted changed schema subtree: %+v", plan.Conflicts)
		}
	})
}

func TestPlanSemanticEditsAllowsChildOfSameBatchCreatedOwner(t *testing.T) {
	t.Parallel()
	planner := New(BuildInfo{})
	input, snapshot := semanticEditCompiledFixture(t)
	operations := []SemanticOperation{
		{Kind: OperationCreateSubject, ParentAddress: "ldl:project:p", SubjectKind: materialize.SubjectEntityType, ID: "component", Fields: []SemanticMapEntry{{Key: "display_name", Value: SemanticValue{Kind: SemanticValueString, String: "Component"}}, {Key: "representation", Value: SemanticValue{Kind: SemanticValueMap, Map: []SemanticMapEntry{{Key: "kind", Value: SemanticValue{Kind: SemanticValueString, String: "shape"}}, {Key: "shape", Value: SemanticValue{Kind: SemanticValueString, String: "rect"}}}}}}},
		{Kind: OperationCreateSubject, ParentAddress: "ldl:project:p:entity-type:component", SubjectKind: materialize.SubjectEntityTypeColumn, ID: "count", Fields: []SemanticMapEntry{{Key: "display_name", Value: SemanticValue{Kind: SemanticValueString, String: "Count"}}, {Key: "value_type", Value: SemanticValue{Kind: SemanticValueString, String: "integer"}}}},
	}
	plan, err := planner.PlanSemanticEdits(context.Background(), SemanticEditPlanInput{BaseInput: input, BaseSnapshot: snapshot, Batch: SemanticOperationBatch{Operations: operations}, Preconditions: allSemanticPreconditions(snapshot)})
	if err != nil || plan.Status != "valid" {
		t.Fatalf("same-batch owner authority was rejected: status=%s err=%v conflicts=%+v diagnostics=%+v", plan.Status, err, plan.Conflicts, plan.Diagnostics)
	}
	if !strings.Contains(string(plan.SourceTree["document.ldl"]), `count "Count" integer`) {
		t.Fatalf("same-batch child was not materialized:\n%s", plan.SourceTree["document.ldl"])
	}
}

func TestPlanSemanticEditsPreservesSetValuedColumnBindings(t *testing.T) {
	t.Parallel()
	const source = `project p "Project" {}
layers {
  app "Application" @10
}
entity_type alpha "Alpha" {
  representation shape rect
  columns {
    status "Status" string
  }
}
entity_type beta "Beta" {
  representation shape rect
  columns {
    status "Status" string
  }
}
relation_type link "Link" data_flow {
  from source types [alpha, beta] layers [app]
  to target types [alpha, beta] layers [app]
  label "links"
  projection flow {
    source_endpoint from
    target_endpoint to
    connector_kind data
  }
}
`
	input := projectCompileInput(source)
	planner := New(BuildInfo{})
	compiled, err := planner.Compile(context.Background(), input)
	if err != nil || len(compiled.Diagnostics) != 0 {
		t.Fatalf("compile set fixture: err=%v diagnostics=%+v", err, compiled.Diagnostics)
	}
	base := compiled.Snapshot()
	addresses := SemanticValue{Kind: SemanticValueArray, Array: []SemanticValue{
		{Kind: SemanticValueAddress, Address: "ldl:project:p:entity-type:alpha:column:status"},
		{Kind: SemanticValueAddress, Address: "ldl:project:p:entity-type:beta:column:status"},
	}}
	types := SemanticValue{Kind: SemanticValueArray, Array: []SemanticValue{
		{Kind: SemanticValueAddress, Address: "ldl:project:p:entity-type:alpha"},
		{Kind: SemanticValueAddress, Address: "ldl:project:p:entity-type:beta"},
	}}
	operations := []SemanticOperation{
		{Kind: OperationCreateSubject, SubjectKind: materialize.SubjectQuery, ParentAddress: "ldl:project:p", ID: "multi", Fields: []SemanticMapEntry{
			{Key: "display_name", Value: SemanticValue{Kind: SemanticValueString, String: "Multi"}},
			{Key: "select", Value: SemanticValue{Kind: SemanticValueMap}},
			{Key: "where", Value: SemanticValue{Kind: SemanticValueMap, Map: []SemanticMapEntry{
				{Key: "kind", Value: SemanticValue{Kind: SemanticValueToken, String: "all"}},
				{Key: "children", Value: SemanticValue{Kind: SemanticValueArray, Array: []SemanticValue{{Kind: SemanticValueMap, Map: []SemanticMapEntry{
					{Key: "kind", Value: SemanticValue{Kind: SemanticValueToken, String: "rows"}},
					{Key: "quantifier", Value: SemanticValue{Kind: SemanticValueToken, String: "any"}},
					{Key: "type_addresses", Value: types},
					{Key: "predicate", Value: SemanticValue{Kind: SemanticValueMap, Map: []SemanticMapEntry{
						{Key: "kind", Value: SemanticValue{Kind: SemanticValueToken, String: "cell"}},
						{Key: "column_addresses", Value: addresses},
						{Key: "operator", Value: SemanticValue{Kind: SemanticValueToken, String: "exists"}},
					}}},
				}}}}},
			}}},
		}},
		{Kind: OperationCreateSubject, SubjectKind: materialize.SubjectView, ParentAddress: "ldl:project:p", ID: "table_view", Fields: []SemanticMapEntry{
			{Key: "display_name", Value: SemanticValue{Kind: SemanticValueString, String: "Table"}},
			{Key: "category", Value: SemanticValue{Kind: SemanticValueToken, String: "inventory"}},
			{Key: "source", Value: SemanticValue{Kind: SemanticValueMap, Map: []SemanticMapEntry{{Key: "kind", Value: SemanticValue{Kind: SemanticValueToken, String: "query"}}, {Key: "query_address", Value: SemanticValue{Kind: SemanticValueAddress, Address: "ldl:project:p:query:multi"}}, {Key: "arguments", Value: SemanticValue{Kind: SemanticValueMap}}}}},
			{Key: "shape", Value: SemanticValue{Kind: SemanticValueMap, Map: []SemanticMapEntry{{Key: "kind", Value: SemanticValue{Kind: SemanticValueToken, String: "table"}}, {Key: "row_source", Value: SemanticValue{Kind: SemanticValueToken, String: "entity_rows"}}}}},
		}},
		{Kind: OperationCreateSubject, SubjectKind: materialize.SubjectViewTableColumn, ParentAddress: "ldl:project:p:view:table_view", ID: "status", Fields: []SemanticMapEntry{
			{Key: "source", Value: SemanticValue{Kind: SemanticValueMap, Map: []SemanticMapEntry{{Key: "kind", Value: SemanticValue{Kind: SemanticValueToken, String: "attribute"}}, {Key: "column_addresses", Value: addresses}}}},
		}},
		{Kind: OperationCreateSubject, SubjectKind: materialize.SubjectView, ParentAddress: "ldl:project:p", ID: "flow_view", Fields: []SemanticMapEntry{
			{Key: "display_name", Value: SemanticValue{Kind: SemanticValueString, String: "Flow"}},
			{Key: "category", Value: SemanticValue{Kind: SemanticValueToken, String: "flow"}},
			{Key: "source", Value: SemanticValue{Kind: SemanticValueMap, Map: []SemanticMapEntry{{Key: "kind", Value: SemanticValue{Kind: SemanticValueToken, String: "query"}}, {Key: "query_address", Value: SemanticValue{Kind: SemanticValueAddress, Address: "ldl:project:p:query:multi"}}, {Key: "arguments", Value: SemanticValue{Kind: SemanticValueMap}}}}},
			{Key: "shape", Value: SemanticValue{Kind: SemanticValueMap, Map: []SemanticMapEntry{
				{Key: "kind", Value: SemanticValue{Kind: SemanticValueToken, String: "flow"}},
				{Key: "relation_type_addresses", Value: SemanticValue{Kind: SemanticValueArray, Array: []SemanticValue{{Kind: SemanticValueAddress, Address: "ldl:project:p:relation-type:link"}}}},
				{Key: "lane_by", Value: SemanticValue{Kind: SemanticValueToken, String: "attribute"}},
				{Key: "lane_column_addresses", Value: addresses},
				{Key: "cycle_policy", Value: SemanticValue{Kind: SemanticValueToken, String: "error"}},
			}}},
		}},
	}
	queryPlan, err := planner.PlanSemanticEdits(context.Background(), SemanticEditPlanInput{BaseInput: input, BaseSnapshot: base, Batch: SemanticOperationBatch{Operations: operations[:1]}, Preconditions: allSemanticPreconditions(base)})
	if err != nil || queryPlan.Status != "valid" {
		t.Fatalf("set-valued row predicate plan failed: status=%s err=%v conflicts=%+v diagnostics=%+v", queryPlan.Status, err, queryPlan.Conflicts, queryPlan.Diagnostics)
	}
	input.ProjectSourceTree = queryPlan.SourceTree
	base = *queryPlan.Result
	operations = operations[1:]
	plan, err := planner.PlanSemanticEdits(context.Background(), SemanticEditPlanInput{BaseInput: input, BaseSnapshot: base, Batch: SemanticOperationBatch{Operations: operations}, Preconditions: allSemanticPreconditions(base)})
	if err != nil || plan.Status != "valid" {
		t.Fatalf("set-valued plan failed: status=%s err=%v conflicts=%+v diagnostics=%+v\n%s", plan.Status, err, plan.Conflicts, plan.Diagnostics, plan.SourceTree["document.ldl"])
	}
	for _, fragment := range []string{"source attribute status entity_types [alpha, beta]", "cell status exists", "lane_by attribute.status"} {
		if !strings.Contains(string(plan.SourceTree["document.ldl"]), fragment) {
			t.Fatalf("canonical set rendering omitted %q:\n%s", fragment, plan.SourceTree["document.ldl"])
		}
	}
	canonical := string(plan.Result.CanonicalJSON)
	for _, address := range []string{"entity-type:alpha:column:status", "entity-type:beta:column:status"} {
		if strings.Count(canonical, address) < 3 {
			t.Fatalf("compiled result narrowed set member %q:\n%s", address, canonical)
		}
	}
}

func TestPlanSemanticEditsCanonicalStandardPlacementIsPermutationIndependent(t *testing.T) {
	t.Parallel()
	planner := New(BuildInfo{})
	input := projectCompileInput("project p \"Project\" {}\nlayers {\n  middle \"Middle\" @20\n}\n")
	compiled, err := planner.Compile(context.Background(), input)
	if err != nil || len(compiled.Diagnostics) != 0 {
		t.Fatalf("compile placement fixture: err=%v diagnostics=%+v", err, compiled.Diagnostics)
	}
	base := compiled.Snapshot()
	create := func(id, name string, order int64) SemanticOperation {
		return SemanticOperation{Kind: OperationCreateSubject, SubjectKind: materialize.SubjectLayer, ParentAddress: "ldl:project:p", ID: id, Fields: []SemanticMapEntry{{Key: "display_name", Value: SemanticValue{Kind: SemanticValueString, String: name}}, {Key: "order", Value: SemanticValue{Kind: SemanticValueInteger, Integer: order}}}}
	}
	permutations := [][]SemanticOperation{{create("zeta", "Zeta", 30), create("alpha", "Alpha", 10)}, {create("alpha", "Alpha", 10), create("zeta", "Zeta", 30)}}
	var baseline SemanticEditPlan
	for index, operations := range permutations {
		plan, planErr := planner.PlanSemanticEdits(context.Background(), SemanticEditPlanInput{BaseInput: input, BaseSnapshot: base, Batch: SemanticOperationBatch{Operations: operations}, Preconditions: allSemanticPreconditions(base)})
		if planErr != nil || plan.Status != "valid" {
			t.Fatalf("permutation %d failed: status=%s err=%v diagnostics=%+v", index, plan.Status, planErr, plan.Diagnostics)
		}
		if index == 0 {
			baseline = plan
			continue
		}
		if !bytes.Equal(plan.SourceTree["document.ldl"], baseline.SourceTree["document.ldl"]) || plan.SourceDiff.Digest != baseline.SourceDiff.Digest || plan.SemanticDiff.Digest != baseline.SemanticDiff.Digest || plan.AuthoringImpact.ImpactDigest != baseline.AuthoringImpact.ImpactDigest {
			t.Fatalf("independent creates were operation-ordered:\nfirst:\n%s\nsecond:\n%s", baseline.SourceTree["document.ldl"], plan.SourceTree["document.ldl"])
		}
	}
	source := string(baseline.SourceTree["document.ldl"])
	if strings.Count(source, "layers {") != 1 || !(strings.Index(source, "alpha ") < strings.Index(source, "middle ") && strings.Index(source, "middle ") < strings.Index(source, "zeta ")) {
		t.Fatalf("standard placement did not extend the canonical group in address order:\n%s", source)
	}
}

func TestCanonicalAuthoringPlanUsesBeforeAndAfterGraphFacts(t *testing.T) {
	t.Parallel()
	planner := New(BuildInfo{})
	input, snapshot := semanticEditCompiledFixture(t)
	operation := SemanticOperation{Kind: OperationMoveEntityToLayer, EntityAddress: "ldl:project:p:entity:b", LayerAddress: "ldl:project:p:layer:data"}
	plan, err := planner.PlanSemanticEdits(context.Background(), SemanticEditPlanInput{BaseInput: input, BaseSnapshot: snapshot, Batch: SemanticOperationBatch{Operations: []SemanticOperation{operation}}, Preconditions: allSemanticPreconditions(snapshot)})
	if err != nil || plan.Status != "valid" {
		t.Fatalf("move plan failed: status=%s err=%v", plan.Status, err)
	}
	for _, entry := range plan.AuthoringImpact.Entries {
		if entry.SubjectAddress != operation.EntityAddress || entry.GraphFacts == nil {
			continue
		}
		want := []string{"ldl:project:p:layer:app", "ldl:project:p:layer:data"}
		if !reflect.DeepEqual(entry.GraphFacts.LayerAddresses, want) {
			t.Fatalf("move graph facts=%v want before/after union %v", entry.GraphFacts.LayerAddresses, want)
		}
		return
	}
	t.Fatalf("move impact entry missing: %+v", plan.AuthoringImpact.Entries)
}

func TestBuildPlannedSourceDiffRecognizesExactModuleMove(t *testing.T) {
	t.Parallel()
	before := map[string][]byte{"old.ldl": []byte("project p \"P\" {}\n")}
	after := map[string][]byte{"new.ldl": []byte("project p \"P\" {}\n")}
	diff := buildPlannedSourceDiff(before, after)
	if len(diff.Edits) != 1 || diff.Edits[0].Kind != PlannedSourceMove || diff.Edits[0].BeforeModule == nil || diff.Edits[0].AfterModule == nil {
		t.Fatalf("exact module move was not represented by the complete union: %+v", diff.Edits)
	}
}

func TestBuildPlannedSourceDiffDoesNotInventAmbiguousModuleMoves(t *testing.T) {
	t.Parallel()
	source := []byte("project p \"P\" {}\n")
	before := map[string][]byte{"old-a.ldl": source, "old-b.ldl": source}
	after := map[string][]byte{"new-a.ldl": source, "new-b.ldl": source}
	diff := buildPlannedSourceDiff(before, after)

	kinds := map[PlannedSourceEditKind]int{}
	for _, edit := range diff.Edits {
		kinds[edit.Kind]++
	}
	if kinds[PlannedSourceMove] != 0 || kinds[PlannedSourceDelete] != 2 || kinds[PlannedSourceCreate] != 2 {
		t.Fatalf("ambiguous byte-identical modules were assigned lineage: %+v", diff.Edits)
	}
}

func TestPlanSemanticEditsHonorsAuthoredPlacementHints(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name       string
		placement  SemanticPlacementHint
		beforeText string
		afterText  string
	}{
		{name: "before anchor", placement: SemanticPlacementHint{GroupAnchorAddress: "ldl:project:p:layer:data", Position: "before"}, beforeText: "extra", afterText: "data"},
		{name: "after anchor", placement: SemanticPlacementHint{GroupAnchorAddress: "ldl:project:p:layer:app", Position: "after"}, beforeText: "app", afterText: "extra"},
		{name: "end of anchor group", placement: SemanticPlacementHint{GroupAnchorAddress: "ldl:project:p:layer:app", Position: "end"}, beforeText: "data", afterText: "extra"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			planner := New(BuildInfo{})
			input, snapshot := semanticEditCompiledFixture(t)
			operation := SemanticOperation{Kind: OperationCreateSubject, ParentAddress: "ldl:project:p", SubjectKind: materialize.SubjectLayer, ID: "extra", Placement: &test.placement, Fields: []SemanticMapEntry{{Key: "display_name", Value: SemanticValue{Kind: SemanticValueString, String: "Extra"}}, {Key: "order", Value: SemanticValue{Kind: SemanticValueInteger, Integer: 15}}}}
			plan, err := planner.PlanSemanticEdits(context.Background(), SemanticEditPlanInput{BaseInput: input, BaseSnapshot: snapshot, Batch: SemanticOperationBatch{Operations: []SemanticOperation{operation}}, Preconditions: allSemanticPreconditions(snapshot)})
			if err != nil || plan.Status != "valid" {
				t.Fatalf("placement plan failed: status=%s err=%v conflicts=%+v diagnostics=%+v", plan.Status, err, plan.Conflicts, plan.Diagnostics)
			}
			source := string(plan.SourceTree["document.ldl"])
			beforeIndex := strings.Index(source, "  "+test.beforeText+" ")
			afterIndex := strings.Index(source, "  "+test.afterText+" ")
			if beforeIndex < 0 || afterIndex < 0 || beforeIndex >= afterIndex {
				t.Fatalf("placement %q was not honored: before=%d after=%d\n%s", test.placement.Position, beforeIndex, afterIndex, source)
			}
		})
	}
}

func TestPlanSemanticEditsRejectsPlacementOutsideDeclaredKindAndOwner(t *testing.T) {
	t.Parallel()
	planner := New(BuildInfo{})
	input, snapshot := semanticEditCompiledFixture(t)

	t.Run("top-level anchor must have the requested child kind", func(t *testing.T) {
		operation := SemanticOperation{Kind: OperationCreateSubject, ParentAddress: "ldl:project:p", SubjectKind: materialize.SubjectLayer, ID: "extra", Fields: []SemanticMapEntry{{Key: "display_name", Value: SemanticValue{Kind: SemanticValueString, String: "Extra"}}, {Key: "order", Value: SemanticValue{Kind: SemanticValueInteger, Integer: 30}}}, Placement: &SemanticPlacementHint{GroupAnchorAddress: "ldl:project:p:entity:a", Position: "before"}}
		plan, err := planner.PlanSemanticEdits(context.Background(), SemanticEditPlanInput{BaseInput: input, BaseSnapshot: snapshot, Batch: SemanticOperationBatch{Operations: []SemanticOperation{operation}}, Preconditions: allSemanticPreconditions(snapshot)})
		if err != nil || plan.Status != "invalid" || len(plan.Conflicts) != 1 || plan.Conflicts[0].Kind != ConflictPlacementChanged {
			t.Fatalf("different-kind anchor was accepted: plan=%+v err=%v", plan, err)
		}
	})

	t.Run("row anchor must belong to the declared row owner", func(t *testing.T) {
		operation := SemanticOperation{Kind: OperationUpsertRow, OwnerAddress: "ldl:project:p:entity:b", ID: "secondary", Values: []SemanticRowCell{{ColumnAddress: "ldl:project:p:entity-type:service:column:note", Value: SemanticValue{Kind: SemanticValueString, String: "new"}}}, Placement: &SemanticPlacementHint{GroupAnchorAddress: "ldl:project:p:entity:a:row:primary", Position: "after"}}
		plan, err := planner.PlanSemanticEdits(context.Background(), SemanticEditPlanInput{BaseInput: input, BaseSnapshot: snapshot, Batch: SemanticOperationBatch{Operations: []SemanticOperation{operation}}, Preconditions: allSemanticPreconditions(snapshot)})
		if err != nil || plan.Status != "invalid" || len(plan.Conflicts) != 1 || plan.Conflicts[0].Kind != ConflictPlacementChanged {
			t.Fatalf("different-owner row anchor was accepted: plan=%+v err=%v", plan, err)
		}
	})

	t.Run("child before placement preserves owner-local order", func(t *testing.T) {
		operation := SemanticOperation{Kind: OperationCreateSubject, ParentAddress: "ldl:project:p:entity-type:service", SubjectKind: materialize.SubjectEntityTypeColumn, ID: "count", Fields: []SemanticMapEntry{{Key: "display_name", Value: SemanticValue{Kind: SemanticValueString, String: "Count"}}, {Key: "value_type", Value: SemanticValue{Kind: SemanticValueToken, String: "integer"}}}, Placement: &SemanticPlacementHint{GroupAnchorAddress: "ldl:project:p:entity-type:service:column:note", Position: "before"}}
		plan, err := planner.PlanSemanticEdits(context.Background(), SemanticEditPlanInput{BaseInput: input, BaseSnapshot: snapshot, Batch: SemanticOperationBatch{Operations: []SemanticOperation{operation}}, Preconditions: allSemanticPreconditions(snapshot)})
		if err != nil || plan.Status != "valid" {
			t.Fatalf("owner-local child placement failed: status=%s err=%v conflicts=%+v", plan.Status, err, plan.Conflicts)
		}
		source := string(plan.SourceTree["document.ldl"])
		if strings.Index(source, `count "Count" integer`) >= strings.Index(source, `note "Note" string`) {
			t.Fatalf("child was not inserted before its declared anchor:\n%s", source)
		}
	})
}

func TestPlanSemanticEditsPreservesCommentsAndIgnoresBlockTextInTrivia(t *testing.T) {
	t.Parallel()
	planner := New(BuildInfo{})

	t.Run("moving an entity keeps its attached comment", func(t *testing.T) {
		source := strings.Replace(semanticEditFixture, `  b "B"`, "  // attached to b\n  b \"B\"", 1)
		input := projectCompileInput(source)
		compiled, err := planner.Compile(context.Background(), input)
		if err != nil || len(compiled.Diagnostics) != 0 {
			t.Fatalf("compile comment fixture: err=%v diagnostics=%+v", err, compiled.Diagnostics)
		}
		snapshot := compiled.Snapshot()
		operation := SemanticOperation{Kind: OperationMoveEntityToLayer, EntityAddress: "ldl:project:p:entity:b", LayerAddress: "ldl:project:p:layer:data"}
		plan, err := planner.PlanSemanticEdits(context.Background(), SemanticEditPlanInput{BaseInput: input, BaseSnapshot: snapshot, Batch: SemanticOperationBatch{Operations: []SemanticOperation{operation}}, Preconditions: allSemanticPreconditions(snapshot)})
		if err != nil || plan.Status != "valid" {
			t.Fatalf("move with comment failed: status=%s err=%v conflicts=%+v", plan.Status, err, plan.Conflicts)
		}
		got := string(plan.SourceTree["document.ldl"])
		if !strings.Contains(got, "entities service @data {\n  // attached to b\n  b \"B\"") || strings.Count(got, "// attached to b") != 1 {
			t.Fatalf("attached comment was orphaned or duplicated:\n%s", got)
		}
	})

	t.Run("child insertion ignores a block name in comments and strings", func(t *testing.T) {
		source := strings.Replace(semanticEditFixture, `  columns {`, "  description \"columns { decoy }\"\n  // columns { another decoy }\n  columns {", 1)
		input := projectCompileInput(source)
		compiled, err := planner.Compile(context.Background(), input)
		if err != nil || len(compiled.Diagnostics) != 0 {
			t.Fatalf("compile trivia fixture: err=%v diagnostics=%+v", err, compiled.Diagnostics)
		}
		snapshot := compiled.Snapshot()
		operation := SemanticOperation{Kind: OperationCreateSubject, ParentAddress: "ldl:project:p:entity-type:service", SubjectKind: materialize.SubjectEntityTypeColumn, ID: "count", Fields: []SemanticMapEntry{{Key: "display_name", Value: SemanticValue{Kind: SemanticValueString, String: "Count"}}, {Key: "value_type", Value: SemanticValue{Kind: SemanticValueToken, String: "integer"}}}}
		plan, err := planner.PlanSemanticEdits(context.Background(), SemanticEditPlanInput{BaseInput: input, BaseSnapshot: snapshot, Batch: SemanticOperationBatch{Operations: []SemanticOperation{operation}}, Preconditions: allSemanticPreconditions(snapshot)})
		if err != nil || plan.Status != "valid" {
			t.Fatalf("CST-authoritative insertion failed: status=%s err=%v diagnostics=%+v", plan.Status, err, plan.Diagnostics)
		}
		got := string(plan.SourceTree["document.ldl"])
		if !strings.Contains(got, `description "columns { decoy }"`) || !strings.Contains(got, `// columns { another decoy }`) || !strings.Contains(got, `count "Count" integer`) {
			t.Fatalf("trivia changed or child missed the real block:\n%s", got)
		}
	})
}

func TestPlanSemanticEditsReplacesCompleteMultilineCSTField(t *testing.T) {
	t.Parallel()
	planner := New(BuildInfo{})
	input, snapshot := semanticEditCompiledFixture(t)
	bound := func(min int64, max SemanticValue) SemanticValue {
		return SemanticValue{Kind: SemanticValueMap, Map: []SemanticMapEntry{{Key: "min", Value: SemanticValue{Kind: SemanticValueInteger, Integer: min}}, {Key: "max", Value: max}}}
	}
	many := SemanticValue{Kind: SemanticValueString, String: "many"}
	cardinality := SemanticValue{Kind: SemanticValueMap, Map: []SemanticMapEntry{{Key: "from_per_to", Value: bound(0, many)}, {Key: "to_per_from", Value: bound(0, many)}}}
	operation := SemanticOperation{Kind: OperationUpdateSubjectField, TargetAddress: "ldl:project:p:relation-type:calls", Path: []string{"cardinality"}, Action: "set", Value: &cardinality}
	first, err := planner.PlanSemanticEdits(context.Background(), SemanticEditPlanInput{BaseInput: input, BaseSnapshot: snapshot, Batch: SemanticOperationBatch{Operations: []SemanticOperation{operation}}, Preconditions: allSemanticPreconditions(snapshot)})
	if err != nil || first.Status != "valid" {
		t.Fatalf("add multiline cardinality: status=%s err=%v diagnostics=%+v", first.Status, err, first.Diagnostics)
	}
	secondInput := cloneSemanticCompileInput(input)
	secondInput.ProjectSourceTree = first.SourceTree
	one := SemanticValue{Kind: SemanticValueInteger, Integer: 1}
	cardinality = SemanticValue{Kind: SemanticValueMap, Map: []SemanticMapEntry{{Key: "from_per_to", Value: bound(0, one)}, {Key: "to_per_from", Value: bound(0, one)}}}
	operation.Value = &cardinality
	second, err := planner.PlanSemanticEdits(context.Background(), SemanticEditPlanInput{BaseInput: secondInput, BaseSnapshot: *first.Result, Batch: SemanticOperationBatch{Operations: []SemanticOperation{operation}}, Preconditions: allSemanticPreconditions(*first.Result)})
	if err != nil || second.Status != "valid" {
		t.Fatalf("replace multiline cardinality: status=%s err=%v diagnostics=%+v", second.Status, err, second.Diagnostics)
	}
	source := string(second.SourceTree["document.ldl"])
	if strings.Count(source, "cardinality {") != 1 || !strings.Contains(source, "from_per_to 0..1") || !strings.Contains(source, "to_per_from 0..1") {
		t.Fatalf("multiline CST field was only partially rewritten:\n%s", source)
	}
	if !strings.Contains(source, "// this comment and all unrelated spacing must survive") {
		t.Fatal("multiline rewrite changed an unrelated comment")
	}
}

func TestPlanSemanticEditsRejectsInvalidFinalDelete(t *testing.T) {
	t.Parallel()
	planner := New(BuildInfo{})
	input, snapshot := semanticEditCompiledFixture(t)
	before := bytes.Clone(input.ProjectSourceTree["document.ldl"])
	operation := SemanticOperation{Kind: OperationDeleteSubject, TargetAddress: "ldl:project:p:entity:a"}

	plan, err := planner.PlanSemanticEdits(context.Background(), SemanticEditPlanInput{
		BaseInput:     input,
		BaseSnapshot:  snapshot,
		Batch:         SemanticOperationBatch{Operations: []SemanticOperation{operation}},
		Preconditions: allSemanticPreconditions(snapshot),
	})
	if err != nil {
		t.Fatal(err)
	}
	if plan.Status != "invalid" || len(plan.Diagnostics) == 0 {
		t.Fatalf("plan=%+v want compiler diagnostics for the broken relation reference", plan)
	}
	for _, diagnostic := range plan.Diagnostics {
		if diagnostic.Arguments["operation_index"] != "0" {
			t.Fatalf("diagnostic=%+v want operation_index=0", diagnostic)
		}
	}
	if !bytes.Equal(input.ProjectSourceTree["document.ldl"], before) {
		t.Fatal("rejected final candidate mutated caller source")
	}
}

func allSemanticPreconditions(snapshot Snapshot) SemanticEditPreconditions {
	pre := SemanticEditPreconditions{}
	for _, subject := range snapshot.SubjectSemanticHashes {
		pre.ExpectedSubjectHashes = append(pre.ExpectedSubjectHashes, ExpectedSemanticHash{Address: subject.Address, Hash: subject.Hash})
	}
	for _, subtree := range snapshot.SubtreeHashes {
		pre.ExpectedSubtreeHashes = append(pre.ExpectedSubtreeHashes, ExpectedSemanticHash{Address: subtree.OwnerAddress, Hash: subtree.Hash})
	}
	for _, child := range snapshot.ChildSetHashes {
		pre.ExpectedChildSets = append(pre.ExpectedChildSets, ExpectedSemanticChildSet{OwnerAddress: child.OwnerAddress, ChildKind: child.ChildKind, Hash: child.Hash})
	}
	for _, file := range snapshot.SourceMap.Files {
		pre.ExpectedSourceDigests = append(pre.ExpectedSourceDigests, ExpectedSemanticSourceDigest{Module: PlannedModuleRef{OriginKind: SourceOriginKind(file.Origin.Kind), PackAddress: file.Origin.PackAddress, ModulePath: file.ModulePath}, Digest: file.Digest})
	}
	return pre
}

func FuzzPlanSemanticEditDisplayNameRoundTrip(f *testing.F) {
	for _, seed := range []string{"plain", "quoted \"name\"", "日本語", "line\\nbreak", "control\x01character"} {
		f.Add(seed)
	}
	f.Fuzz(func(t *testing.T, name string) {
		if name == "" || !utf8.ValidString(name) || strings.ContainsAny(name, "\r\n") {
			t.Skip()
		}
		planner := New(BuildInfo{})
		input := projectCompileInput(semanticEditFixture)
		compiled, err := planner.Compile(context.Background(), input)
		if err != nil || len(compiled.Diagnostics) != 0 {
			t.Fatal("fixture")
		}
		base := compiled.Snapshot()
		value := SemanticValue{Kind: SemanticValueString, String: name}
		op := SemanticOperation{Kind: OperationUpdateSubjectField, TargetAddress: "ldl:project:p:entity:a", Path: []string{"display_name"}, Action: "set", Value: &value}
		plan, err := planner.PlanSemanticEdits(context.Background(), SemanticEditPlanInput{BaseInput: input, BaseSnapshot: base, Batch: SemanticOperationBatch{Operations: []SemanticOperation{op}}, Preconditions: allSemanticPreconditions(base)})
		if err != nil {
			t.Fatal(err)
		}
		if plan.Status != "valid" {
			t.Fatalf("name %q rejected: %+v", name, plan.Diagnostics)
		}
		object := normalizedSubject(*plan.Result, "ldl:project:p:entity:a")
		if got, _ := object["display_name"].(string); got != name {
			t.Fatalf("round trip=%q want=%q", got, name)
		}
	})
}

func TestSemanticValueRenderingUsesClosedCanonicalVocabulary(t *testing.T) {
	t.Parallel()
	_, snapshot := semanticEditCompiledFixture(t)
	module := "document.ldl"

	valid := []struct {
		name  string
		value SemanticValue
	}{
		{name: "absent", value: SemanticValue{Kind: SemanticValueAbsent}},
		{name: "address", value: SemanticValue{Kind: SemanticValueAddress, Address: "ldl:project:p:entity:a"}},
		{name: "boolean", value: SemanticValue{Kind: SemanticValueBoolean, Boolean: true}},
		{name: "decimal", value: SemanticValue{Kind: SemanticValueDecimal, Decimal: "1.5"}},
		{name: "integer", value: SemanticValue{Kind: SemanticValueInteger, Integer: 2}},
		{name: "string", value: SemanticValue{Kind: SemanticValueString, String: "free text"}},
		{name: "array", value: SemanticValue{Kind: SemanticValueArray, Array: []SemanticValue{{Kind: SemanticValueInteger, Integer: 1}}}},
		{name: "map", value: SemanticValue{Kind: SemanticValueMap, Map: []SemanticMapEntry{{Key: "x", Value: SemanticValue{Kind: SemanticValueBoolean, Boolean: true}}}}},
	}
	for _, test := range valid {
		t.Run(test.name, func(t *testing.T) {
			if rendered, ok := renderSemanticValue(snapshot, module, test.value); !ok || rendered == "" {
				t.Fatalf("canonical render failed: rendered=%q ok=%v", rendered, ok)
			}
		})
	}

	invalid := []struct {
		name  string
		value SemanticValue
	}{
		{name: "blob has no source bytes", value: SemanticValue{Kind: SemanticValueBlob, Blob: "sha256:x"}},
		{name: "unknown kind", value: SemanticValue{Kind: "unknown"}},
		{name: "unresolved address", value: SemanticValue{Kind: SemanticValueAddress, Address: "ldl:project:p:entity:missing"}},
	}
	for _, test := range invalid {
		t.Run(test.name, func(t *testing.T) {
			if rendered, ok := renderSemanticValue(snapshot, module, test.value); ok {
				t.Fatalf("invalid value rendered as %q", rendered)
			}
		})
	}
}

func TestSemanticSpecialFieldsRenderCanonicalLDL(t *testing.T) {
	t.Parallel()
	_, snapshot := semanticEditCompiledFixture(t)
	module := "document.ldl"
	entityTypes := SemanticValue{Kind: SemanticValueArray, Array: []SemanticValue{{Kind: SemanticValueAddress, Address: "ldl:project:p:entity-type:service"}}}
	tests := []struct {
		name     string
		field    string
		value    SemanticValue
		contains string
	}{
		{name: "representation", field: "representation", value: semanticMapValue("kind", "shape", "shape", "rect"), contains: "representation shape rect"},
		{name: "endpoint", field: "from", value: SemanticValue{Kind: SemanticValueMap, Map: []SemanticMapEntry{{Key: "entity_type_addresses", Value: entityTypes}, {Key: "role", Value: SemanticValue{Kind: SemanticValueString, String: "source"}}}}, contains: "from source types [service]"},
		{name: "select", field: "select", value: SemanticValue{Kind: SemanticValueMap, Map: []SemanticMapEntry{{Key: "entity_type_addresses", Value: entityTypes}}}, contains: "entity_types [service]"},
		{name: "shape", field: "shape", value: semanticMapValue("kind", "table"), contains: "table {"},
		{name: "field source", field: "source", value: semanticMapValue("kind", "field", "field", "display_name"), contains: "source field display_name"},
		{name: "query source", field: "source", value: SemanticValue{Kind: SemanticValueMap, Map: []SemanticMapEntry{{Key: "kind", Value: SemanticValue{Kind: SemanticValueString, String: "query"}}, {Key: "query_address", Value: SemanticValue{Kind: SemanticValueAddress, Address: "ldl:project:p:query:q"}}}}, contains: "source query q {}"},
		{name: "attribute source", field: "source", value: SemanticValue{Kind: SemanticValueMap, Map: []SemanticMapEntry{{Key: "kind", Value: SemanticValue{Kind: SemanticValueString, String: "attribute"}}, {Key: "column_addresses", Value: SemanticValue{Kind: SemanticValueArray, Array: []SemanticValue{{Kind: SemanticValueAddress, Address: "ldl:project:p:entity-type:service:column:note"}}}}}}, contains: "source attribute note"},
		{name: "state source", field: "source", value: semanticMapValue("kind", "state", "field_path", "selection.active"), contains: "source state selection.active"},
		{name: "cardinality", field: "cardinality", value: semanticCardinalityValue(), contains: "to_per_from 0..*"},
		{name: "traversal", field: "traverse", value: SemanticValue{Kind: SemanticValueMap, Map: []SemanticMapEntry{{Key: "direction", Value: SemanticValue{Kind: SemanticValueString, String: "outgoing"}}, {Key: "min_depth", Value: SemanticValue{Kind: SemanticValueInteger}}, {Key: "max_depth", Value: SemanticValue{Kind: SemanticValueInteger, Integer: 3}}, {Key: "cycle_policy", Value: SemanticValue{Kind: SemanticValueString, String: "visit_once"}}, {Key: "relation_type_addresses", Value: SemanticValue{Kind: SemanticValueArray, Array: []SemanticValue{{Kind: SemanticValueAddress, Address: "ldl:project:p:relation-type:calls"}}}}}}, contains: "relations [calls]"},
		{name: "annotations", field: "annotations", value: SemanticValue{Kind: SemanticValueArray, Array: []SemanticValue{{Kind: SemanticValueMap, Map: []SemanticMapEntry{{Key: "key", Value: SemanticValue{Kind: SemanticValueString, String: "team"}}, {Key: "value", Value: SemanticValue{Kind: SemanticValueString, String: "core"}}}}}}, contains: `team: "core"`},
		{name: "forward label", field: "forward_label", value: SemanticValue{Kind: SemanticValueString, String: "links"}, contains: `label "links"`},
		{name: "enabled flag", field: "source_refs", value: SemanticValue{Kind: SemanticValueBoolean, Boolean: true}, contains: "source_refs"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			rendered, handled, valid := renderSpecialField(snapshot, module, test.field, test.value, 2)
			if !handled || !valid || !strings.Contains(rendered, test.contains) {
				t.Fatalf("rendered=%q handled=%v valid=%v; want substring %q", rendered, handled, valid, test.contains)
			}
		})
	}
}

func TestSemanticSpecialFieldsRejectIncompleteAuthority(t *testing.T) {
	t.Parallel()
	_, snapshot := semanticEditCompiledFixture(t)
	tests := []struct {
		name  string
		field string
		value SemanticValue
	}{
		{name: "representation without kind", field: "representation", value: SemanticValue{Kind: SemanticValueMap}},
		{name: "representation is not a map", field: "representation", value: SemanticValue{Kind: SemanticValueString, String: "shape"}},
		{name: "endpoint without role", field: "from", value: SemanticValue{Kind: SemanticValueMap}},
		{name: "endpoint is not a map", field: "from", value: SemanticValue{Kind: SemanticValueString, String: "source"}},
		{name: "cardinality without bounds", field: "cardinality", value: SemanticValue{Kind: SemanticValueMap}},
		{name: "cardinality is not a map", field: "cardinality", value: SemanticValue{Kind: SemanticValueString, String: "0..1"}},
		{name: "shape without kind", field: "shape", value: SemanticValue{Kind: SemanticValueMap}},
		{name: "shape is not a map", field: "shape", value: SemanticValue{Kind: SemanticValueString, String: "table"}},
		{name: "source is not a map", field: "source", value: SemanticValue{Kind: SemanticValueString, String: "field"}},
		{name: "attribute source without column", field: "source", value: semanticMapValue("kind", "attribute")},
		{name: "query source without address", field: "source", value: semanticMapValue("kind", "query")},
		{name: "state source without path", field: "source", value: semanticMapValue("kind", "state")},
		{name: "traversal without policy", field: "traverse", value: SemanticValue{Kind: SemanticValueMap}},
		{name: "traversal is not a map", field: "traverse", value: SemanticValue{Kind: SemanticValueString, String: "outgoing"}},
		{name: "traversal with unresolved relation", field: "traverse", value: SemanticValue{Kind: SemanticValueMap, Map: []SemanticMapEntry{{Key: "direction", Value: SemanticValue{Kind: SemanticValueString, String: "outgoing"}}, {Key: "min_depth", Value: SemanticValue{Kind: SemanticValueInteger}}, {Key: "max_depth", Value: SemanticValue{Kind: SemanticValueInteger, Integer: 1}}, {Key: "cycle_policy", Value: SemanticValue{Kind: SemanticValueString, String: "visit_once"}}, {Key: "relation_type_addresses", Value: SemanticValue{Kind: SemanticValueArray, Array: []SemanticValue{{Kind: SemanticValueAddress, Address: "ldl:project:p:relation-type:missing"}}}}}}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			rendered, handled, valid := renderSpecialField(snapshot, "document.ldl", test.field, test.value, 0)
			if !handled || valid {
				t.Fatalf("incomplete field rendered=%q handled=%v valid=%v", rendered, handled, valid)
			}
		})
	}
}

func TestSemanticCSTFieldRewriteRoundTrip(t *testing.T) {
	t.Parallel()
	input, snapshot := semanticEditCompiledFixture(t)
	planner := New(BuildInfo{})
	_, _, ok := subjectRecord(snapshot, "ldl:project:p:entity:a")
	if !ok {
		t.Fatal("fixture entity source record is missing")
	}

	mutable := cloneSemanticCompileInput(input)
	steps := []struct {
		name        string
		rendered    string
		remove      bool
		wantPresent string
	}{
		{name: "insert", rendered: `description "one"`, wantPresent: `description "one"`},
		{name: "replace", rendered: `description "two"`, wantPresent: `description "two"`},
		{name: "remove", remove: true},
	}
	current := snapshot
	for _, step := range steps {
		t.Run(step.name, func(t *testing.T) {
			_, currentSource, present := subjectRecord(current, "ldl:project:p:entity:a")
			if !present {
				t.Fatal("entity source record disappeared")
			}
			if !rewriteBlockField(&mutable, current, currentSource, "description", step.rendered, step.remove) {
				t.Fatal("CST rewrite was rejected")
			}
			compiled, err := planner.Compile(context.Background(), mutable)
			if err != nil || len(compiled.Diagnostics) != 0 {
				t.Fatalf("rewritten source did not compile: err=%v diagnostics=%+v", err, compiled.Diagnostics)
			}
			current = compiled.Snapshot()
			got := string(mutable.ProjectSourceTree["document.ldl"])
			if step.wantPresent != "" && !strings.Contains(got, step.wantPresent) {
				t.Fatalf("rewritten source missing %q", step.wantPresent)
			}
			if step.remove && strings.Contains(got, "description ") {
				t.Fatal("removed field remains in source")
			}
		})
	}
}

func TestSemanticSourceDiffKindsAndLargeFallback(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name   string
		before map[string][]byte
		after  map[string][]byte
		kind   PlannedSourceEditKind
		count  int
	}{
		{name: "create", before: map[string][]byte{}, after: map[string][]byte{"a.ldl": []byte("x")}, kind: PlannedSourceCreate, count: 1},
		{name: "delete", before: map[string][]byte{"a.ldl": []byte("x")}, after: map[string][]byte{}, kind: PlannedSourceDelete, count: 1},
		{name: "unchanged", before: map[string][]byte{"a.ldl": []byte("x")}, after: map[string][]byte{"a.ldl": []byte("x")}, count: 0},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			diff := buildPlannedSourceDiff(test.before, test.after)
			if len(diff.Edits) != test.count {
				t.Fatalf("edit count=%d want=%d", len(diff.Edits), test.count)
			}
			if test.count > 0 && diff.Edits[0].Kind != test.kind {
				t.Fatalf("edit kind=%q want=%q", diff.Edits[0].Kind, test.kind)
			}
		})
	}

	largeBefore := bytes.Repeat([]byte("a\n"), 1001)
	largeAfter := bytes.Repeat([]byte("b\n"), 1001)
	if edits := minimalModuleEdits("large.ldl", largeBefore, largeAfter); len(edits) != 1 {
		t.Fatalf("large source diff produced %d edits; want one bounded fallback", len(edits))
	}
}

func TestSemanticSourceEditsPreserveUTF8Boundaries(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name   string
		before string
		after  string
	}{
		{name: "changed leading code point", before: `name "éa"`, after: `name "êb"`},
		{name: "changed code point with common continuation byte", before: `name "xé"`, after: `name "yĩ"`},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			edits := minimalModuleEdits("document.ldl", []byte(test.before), []byte(test.after))
			got := []byte(test.before)
			for i := len(edits) - 1; i >= 0; i-- {
				edit := edits[i]
				if !utf8.Valid(edit.Replacement) {
					t.Fatalf("replacement splits UTF-8: %x", edit.Replacement)
				}
				got = append(append(append([]byte{}, got[:edit.StartByte]...), edit.Replacement...), got[edit.EndByte:]...)
			}
			if string(got) != test.after {
				t.Fatalf("applied source=%q want=%q", got, test.after)
			}
		})
	}
}

func TestSemanticSourceAppendAndReferenceDelimiterAreLossless(t *testing.T) {
	t.Parallel()
	input := CompileInput{ProjectSourceTree: map[string][]byte{"document.ldl": []byte("project p")}}
	appendText(&input, "document.ldl", "reference notes")
	if got := string(input.ProjectSourceTree["document.ldl"]); got != "project p\nreference notes" {
		t.Fatalf("appended source=%q", got)
	}

	reference := renderReference("notes", "body\nLDL_REFERENCE\n")
	if !strings.Contains(reference, "<<-LDL_REFERENCE_TEXT") || !strings.HasSuffix(reference, "LDL_REFERENCE_TEXT\n") {
		t.Fatalf("colliding reference delimiter was not extended:\n%s", reference)
	}
	firstLineCollision := renderReference("notes", "LDL_REFERENCE\nbody\n")
	if !strings.Contains(firstLineCollision, "<<-LDL_REFERENCE_TEXT") {
		t.Fatalf("first content line collision was not escaped:\n%s", firstLineCollision)
	}
}

func TestPlanSemanticReferenceTextIsExactOrRejected(t *testing.T) {
	t.Parallel()
	planner := New(BuildInfo{})
	input, snapshot := semanticEditCompiledFixture(t)

	t.Run("trailing LF round trips through the normalized result", func(t *testing.T) {
		value := SemanticValue{Kind: SemanticValueString, String: "exact text\n"}
		operation := SemanticOperation{Kind: OperationUpdateSubjectField, TargetAddress: "ldl:project:p:reference:guide", Path: []string{"text"}, Action: "set", Value: &value}
		plan, err := planner.PlanSemanticEdits(context.Background(), SemanticEditPlanInput{BaseInput: input, BaseSnapshot: snapshot, Batch: SemanticOperationBatch{Operations: []SemanticOperation{operation}}, Preconditions: allSemanticPreconditions(snapshot)})
		if err != nil || plan.Status != "valid" {
			t.Fatalf("reference update failed: status=%s err=%v diagnostics=%+v", plan.Status, err, plan.Diagnostics)
		}
		if got, _ := normalizedSubject(*plan.Result, operation.TargetAddress)["text"].(string); got != value.String {
			t.Fatalf("normalized Reference text=%q want exact %q", got, value.String)
		}
	})

	t.Run("nonempty text without LF is rejected", func(t *testing.T) {
		value := SemanticValue{Kind: SemanticValueString, String: "would change meaning"}
		operation := SemanticOperation{Kind: OperationUpdateSubjectField, TargetAddress: "ldl:project:p:reference:guide", Path: []string{"text"}, Action: "set", Value: &value}
		plan, err := planner.PlanSemanticEdits(context.Background(), SemanticEditPlanInput{BaseInput: input, BaseSnapshot: snapshot, Batch: SemanticOperationBatch{Operations: []SemanticOperation{operation}}, Preconditions: allSemanticPreconditions(snapshot)})
		if err != nil || plan.Status != "invalid" || len(plan.Diagnostics) != 1 || plan.Diagnostics[0].MessageKey != "unrepresentable_reference_text" {
			t.Fatalf("non-lossless Reference update was not rejected precisely: plan=%+v err=%v", plan, err)
		}
	})
}

func TestSemanticTemporaryOverlayRebasesAllProjectSourceRoles(t *testing.T) {
	t.Parallel()
	input, snapshot := semanticEditCompiledFixture(t)
	module := "document.ldl"
	projectOrigin := resolve.SourceOrigin{Kind: resolve.OriginProject}
	moduleRef := index.ModuleRef{Origin: projectOrigin, ModulePath: module}
	snapshot.SourceMap.Exports = append(snapshot.SourceMap.Exports, index.ExportBindingRecord{Module: moduleRef, Range: SourceRange{Origin: projectOrigin, ModulePath: module, StartByte: 1, EndByte: 2}})
	snapshot.SourceMap.Assets = append(snapshot.SourceMap.Assets, index.SourceAssetRecord{Origin: projectOrigin, ModulePath: module, Range: SourceRange{Origin: projectOrigin, ModulePath: module, StartByte: 2, EndByte: 3}})
	snapshot.SourceMap.Subjects[0].CommentRanges = []SourceRange{{Origin: projectOrigin, ModulePath: module, StartByte: 0, EndByte: 1}}

	before := cloneSourceTree(input.ProjectSourceTree)
	after := cloneSourceTree(before)
	after[module] = append([]byte("// inserted\n"), after[module]...)
	rebased := rebaseSnapshotSourceRanges(snapshot, before, after)

	if rebased.SourceMap.Files[0].ByteLength != len(after[module]) {
		t.Fatalf("rebased byte length=%d want=%d", rebased.SourceMap.Files[0].ByteLength, len(after[module]))
	}
	if got := rebased.SourceMap.Exports[len(rebased.SourceMap.Exports)-1].Range.StartByte; got <= 1 {
		t.Fatalf("export range was not rebased: start=%d", got)
	}
	if got := rebased.SourceMap.Assets[len(rebased.SourceMap.Assets)-1].Range.StartByte; got <= 2 {
		t.Fatalf("asset range was not rebased: start=%d", got)
	}
}

func TestSemanticTemporaryOverlayLeavesPackSourceAuthorityUntouched(t *testing.T) {
	t.Parallel()
	input, snapshot := semanticEditCompiledFixture(t)
	packOrigin := resolve.SourceOrigin{Kind: resolve.OriginPack}
	snapshot.SourceMap.Files = append(snapshot.SourceMap.Files, index.SourceFileRecord{Origin: packOrigin, ModulePath: "pack.ldl", Digest: "sha256:pack", ByteLength: 4})
	snapshot.SourceMap.Subjects = append(snapshot.SourceMap.Subjects, index.SourceSubjectRecord{Address: "ldl:pack:pub:name:entity-type:type", Module: &index.ModuleRef{Origin: packOrigin, ModulePath: "pack.ldl"}})
	snapshot.LosslessSyntaxTree.Files = append(snapshot.LosslessSyntaxTree.Files, LosslessSyntaxFile{Origin: packOrigin, ModulePath: "pack.ldl", Source: []byte("pack")})

	before := cloneSourceTree(input.ProjectSourceTree)
	after := cloneSourceTree(before)
	after["document.ldl"] = append([]byte("// project edit\n"), after["document.ldl"]...)
	rebased := rebaseSnapshotSourceRanges(snapshot, before, after)

	packFile := rebased.SourceMap.Files[len(rebased.SourceMap.Files)-1]
	if packFile.Digest != "sha256:pack" || packFile.ByteLength != 4 {
		t.Fatalf("pack source authority changed: %+v", packFile)
	}
	packSyntax := rebased.LosslessSyntaxTree.Files[len(rebased.LosslessSyntaxTree.Files)-1]
	if string(packSyntax.Source) != "pack" {
		t.Fatalf("pack syntax source changed to %q", packSyntax.Source)
	}
}

func TestSemanticImpactClassifiesSourceOnlyAndEmptyChanges(t *testing.T) {
	t.Parallel()
	_, snapshot := semanticEditCompiledFixture(t)
	emptySemantic := PlannedSemanticDiff{Entries: []SemanticDiffEntry{}, Digest: digestJSON([]SemanticDiffEntry{})}
	source := buildPlannedSourceDiff(map[string][]byte{"document.ldl": []byte("// before\n")}, map[string][]byte{"document.ldl": []byte("// after\n")})
	impact := buildPlannedAuthoringImpact(snapshot, snapshot, source, emptySemantic)
	if len(impact.Entries) != 1 || impact.Entries[0].Capability != CapabilitySourceMaintain {
		t.Fatalf("source-only impact=%+v", impact)
	}

	empty := buildPlannedAuthoringImpact(snapshot, snapshot, buildPlannedSourceDiff(nil, nil), emptySemantic)
	if len(empty.Entries) != 0 || len(empty.RequiredCapabilities) != 0 {
		t.Fatalf("empty impact=%+v", empty)
	}
	semantic := buildPlannedSemanticDiff(Snapshot{}, Snapshot{})
	if len(semantic.Entries) != 0 || semantic.Digest == "" {
		t.Fatalf("canonical empty semantic diff=%+v", semantic)
	}
}

func TestSemanticPlannerRejectsInvalidBoundaryInputs(t *testing.T) {
	t.Parallel()
	planner := New(BuildInfo{})
	input, snapshot := semanticEditCompiledFixture(t)
	tests := []struct {
		name  string
		ctx   context.Context
		input SemanticEditPlanInput
	}{
		{name: "nil context", input: SemanticEditPlanInput{}},
		{name: "empty batch", ctx: context.Background(), input: SemanticEditPlanInput{BaseInput: input}},
		{name: "invalid source", ctx: context.Background(), input: SemanticEditPlanInput{BaseInput: projectCompileInput("project p \"P\" {\n"), Batch: SemanticOperationBatch{Operations: []SemanticOperation{{Kind: "unknown"}}}}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			plan, err := planner.PlanSemanticEdits(test.ctx, test.input)
			if test.ctx == nil {
				if err == nil {
					t.Fatal("nil context was accepted")
				}
				return
			}
			if err != nil || plan.Status != "invalid" {
				t.Fatalf("plan=%+v err=%v", plan, err)
			}
		})
	}

	stale := snapshot
	stale.DefinitionHash = "sha256:stale"
	plan, err := planner.PlanSemanticEdits(context.Background(), SemanticEditPlanInput{BaseInput: input, BaseSnapshot: stale, Batch: SemanticOperationBatch{Operations: []SemanticOperation{{Kind: "unknown"}}}, Preconditions: allSemanticPreconditions(snapshot)})
	if err != nil || plan.Status != "invalid" || len(plan.Conflicts) != 1 || plan.Conflicts[0].Kind != ConflictStaleRevision {
		t.Fatalf("stale snapshot plan=%+v err=%v", plan, err)
	}
}

func TestPlanSemanticEditsEnforcesCumulativeLogicalResponseLimits(t *testing.T) {
	t.Parallel()
	planner := New(BuildInfo{})
	input, snapshot := semanticEditCompiledFixture(t)
	value := SemanticValue{Kind: SemanticValueString, String: "A response large enough to exercise complete accounting"}
	operation := SemanticOperation{Kind: OperationUpdateSubjectField, TargetAddress: "ldl:project:p:entity:a", Path: []string{"display_name"}, Action: "set", Value: &value}

	tests := []struct {
		name     string
		limits   SemanticPlanLimits
		resource string
	}{
		{name: "nested logical items", limits: SemanticPlanLimits{MaxItems: 10}, resource: "logical_response_items"},
		{name: "wire bytes plus attachments", limits: SemanticPlanLimits{MaxOutputBytes: 64}, resource: "logical_response_bytes"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			_, err := planner.PlanSemanticEdits(context.Background(), SemanticEditPlanInput{BaseInput: input, BaseSnapshot: snapshot, Batch: SemanticOperationBatch{Operations: []SemanticOperation{operation}}, Preconditions: allSemanticPreconditions(snapshot), Limits: test.limits})
			var compileErr *CompileError
			if !errors.As(err, &compileErr) || compileErr.Category != ErrorCategoryResource || compileErr.Resource != test.resource {
				t.Fatalf("limit error=%v want resource %q", err, test.resource)
			}
		})
	}
}

func TestCanonicalDigestsUseRFC8785BytesForUnicodeAndHTMLText(t *testing.T) {
	t.Parallel()
	value := map[string]any{"html": "<entity>", "é": "composed", "𐀀": "supplementary", "\ue000": "private-use"}
	canonical, err := materialize.Canonicalize(value)
	if err != nil {
		t.Fatal(err)
	}
	if got, want := digestJSON(value), semanticDigest(canonical); got != want {
		t.Fatalf("canonical digest=%q want=%q for bytes %s", got, want, canonical)
	}
	if bytes.Contains(canonical, []byte(`\u003c`)) {
		t.Fatalf("canonical digest input used HTML-escaped JSON: %s", canonical)
	}
}

func TestSemanticValueConversionsPreserveAuthoredData(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name  string
		value SemanticValue
		plain any
	}{
		{name: "absent", value: SemanticValue{Kind: SemanticValueAbsent}, plain: nil},
		{name: "address", value: SemanticValue{Kind: SemanticValueAddress, Address: "ldl:project:p:entity:a"}, plain: "ldl:project:p:entity:a"},
		{name: "boolean", value: SemanticValue{Kind: SemanticValueBoolean, Boolean: true}, plain: true},
		{name: "decimal", value: SemanticValue{Kind: SemanticValueDecimal, Decimal: "1.5"}, plain: float64(1.5)},
		{name: "integer", value: SemanticValue{Kind: SemanticValueInteger, Integer: 2}, plain: int64(2)},
		{name: "string", value: SemanticValue{Kind: SemanticValueString, String: "text"}, plain: "text"},
		{name: "array", value: SemanticValue{Kind: SemanticValueArray, Array: []SemanticValue{{Kind: SemanticValueInteger, Integer: 1}}}, plain: []any{int64(1)}},
		{name: "map", value: SemanticValue{Kind: SemanticValueMap, Map: []SemanticMapEntry{{Key: "enabled", Value: SemanticValue{Kind: SemanticValueBoolean, Boolean: true}}}}, plain: map[string]any{"enabled": true}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if got := semanticValuePlain(test.value); !reflect.DeepEqual(got, test.plain) {
				t.Fatalf("plain value=%#v want=%#v", got, test.plain)
			}
		})
	}

	plainTests := []struct {
		name string
		wire any
		kind SemanticValueKind
	}{
		{name: "nil", wire: nil, kind: SemanticValueAbsent},
		{name: "string", wire: "text", kind: SemanticValueString},
		{name: "boolean", wire: true, kind: SemanticValueBoolean},
		{name: "whole number", wire: float64(2), kind: SemanticValueInteger},
		{name: "fraction", wire: float64(1.5), kind: SemanticValueDecimal},
	}
	for _, test := range plainTests {
		t.Run("plain "+test.name, func(t *testing.T) {
			if got := plainSemanticValue(test.wire); got.Kind != test.kind {
				t.Fatalf("semantic kind=%q want=%q", got.Kind, test.kind)
			}
		})
	}
}

func TestCanonicalCreateRenderingIncludesOptionalAuthoredFields(t *testing.T) {
	t.Parallel()
	_, snapshot := semanticEditCompiledFixture(t)
	module := "document.ldl"
	common := map[string]SemanticValue{
		"display_name":  {Kind: SemanticValueString, String: "Created"},
		"description":   {Kind: SemanticValueString, String: "body"},
		"order":         {Kind: SemanticValueInteger, Integer: 30},
		"type_address":  {Kind: SemanticValueAddress, Address: "ldl:project:p:entity-type:service"},
		"layer_address": {Kind: SemanticValueAddress, Address: "ldl:project:p:layer:app"},
	}
	tests := []struct {
		name string
		kind SemanticSubjectKind
		want string
	}{
		{name: "layer body", kind: materialize.SubjectLayer, want: `description "body"`},
		{name: "entity body", kind: materialize.SubjectEntity, want: `description "body"`},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			rendered, _, ok := renderCreatedSubject(snapshot, module, test.kind, "created", common)
			if !ok || !strings.Contains(rendered, test.want) {
				t.Fatalf("rendered=%q ok=%v; want substring %q", rendered, ok, test.want)
			}
		})
	}

	column, valid := renderColumn(snapshot, module, "state", map[string]SemanticValue{
		"display_name": {Kind: SemanticValueString, String: "State"},
		"value_type":   {Kind: SemanticValueString, String: "enum"},
		"enum_values":  {Kind: SemanticValueArray, Array: []SemanticValue{{Kind: SemanticValueString, String: "open"}}},
		"required":     {Kind: SemanticValueBoolean, Boolean: true},
		"default":      {Kind: SemanticValueString, String: "open"},
		"min_length":   {Kind: SemanticValueInteger, Integer: 1},
		"max_length":   {Kind: SemanticValueInteger, Integer: 20},
	}, true)
	if !valid {
		t.Fatal("valid generated column fields were rejected")
	}
	for _, want := range []string{`enum ["open"]`, "required", `default "open"`, "min_length 1", "max_length 20"} {
		if !strings.Contains(column, want) {
			t.Fatalf("column=%q; missing %q", column, want)
		}
	}
}

func TestCanonicalCreateRenderingRejectsMissingRequiredFields(t *testing.T) {
	t.Parallel()
	_, snapshot := semanticEditCompiledFixture(t)
	tests := []struct {
		name   string
		kind   SemanticSubjectKind
		fields map[string]SemanticValue
	}{
		{name: "entity type display name", kind: materialize.SubjectEntityType, fields: map[string]SemanticValue{}},
		{name: "relation type semantic kind", kind: materialize.SubjectRelationType, fields: map[string]SemanticValue{"display_name": {Kind: SemanticValueString, String: "Links"}}},
		{name: "layer order", kind: materialize.SubjectLayer, fields: map[string]SemanticValue{"display_name": {Kind: SemanticValueString, String: "Layer"}}},
		{name: "entity type and layer", kind: materialize.SubjectEntity, fields: map[string]SemanticValue{"display_name": {Kind: SemanticValueString, String: "Entity"}}},
		{name: "query display name", kind: materialize.SubjectQuery, fields: map[string]SemanticValue{}},
		{name: "view category", kind: materialize.SubjectView, fields: map[string]SemanticValue{"display_name": {Kind: SemanticValueString, String: "View"}}},
		{name: "reference text", kind: materialize.SubjectReference, fields: map[string]SemanticValue{"text": {Kind: SemanticValueInteger}}},
		{name: "column display name", kind: materialize.SubjectEntityTypeColumn, fields: map[string]SemanticValue{"value_type": {Kind: SemanticValueString, String: "string"}}},
		{name: "constraint columns", kind: materialize.SubjectEntityTypeConstraint, fields: map[string]SemanticValue{}},
		{name: "query parameter type", kind: materialize.SubjectQueryParameter, fields: map[string]SemanticValue{}},
		{name: "view export filename", kind: materialize.SubjectViewExport, fields: map[string]SemanticValue{"format": {Kind: SemanticValueString, String: "json"}}},
		{name: "unknown subject kind", kind: "unknown", fields: map[string]SemanticValue{}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			rendered, _, ok := renderCreatedSubject(snapshot, "document.ldl", test.kind, "created", test.fields)
			if ok {
				t.Fatalf("incomplete subject rendered as %q", rendered)
			}
		})
	}
}

func TestApplyCreateSubjectRejectsIncompleteGeneratedPayload(t *testing.T) {
	t.Parallel()
	input, snapshot := semanticEditCompiledFixture(t)
	operation := SemanticOperation{
		Kind:          OperationCreateSubject,
		ParentAddress: "ldl:project:p",
		SubjectKind:   materialize.SubjectLayer,
		ID:            "incomplete",
		Fields: []SemanticMapEntry{
			{Key: "display_name", Value: SemanticValue{Kind: SemanticValueString, String: "Incomplete"}},
		},
	}
	conflict, diagnostic := applyCreateSubject(&input, snapshot, operation)
	if conflict != nil || diagnostic == nil || diagnostic.Code != "LDL1801" {
		t.Fatalf("conflict=%+v diagnostic=%+v", conflict, diagnostic)
	}
}

func TestInsertOwnerChildRejectsInvalidSourceAuthority(t *testing.T) {
	t.Parallel()
	input, snapshot := semanticEditCompiledFixture(t)
	_, projectSource, ok := subjectRecord(snapshot, "ldl:project:p")
	if !ok {
		t.Fatal("fixture project source record is missing")
	}
	tests := []struct {
		name   string
		mutate func(*CompileInput, *index.SourceSubjectRecord)
	}{
		{
			name: "range outside source",
			mutate: func(_ *CompileInput, source *index.SourceSubjectRecord) {
				source.DeclarationRange.StartByte = -1
			},
		},
		{
			name: "owner without CST block",
			mutate: func(input *CompileInput, source *index.SourceSubjectRecord) {
				input.ProjectSourceTree["document.ldl"] = []byte("project p")
				source.DeclarationRange.StartByte = 0
				source.DeclarationRange.EndByte = len(input.ProjectSourceTree["document.ldl"])
			},
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			mutable := cloneSemanticCompileInput(input)
			source := projectSource
			test.mutate(&mutable, &source)
			if insertOwnerChild(&mutable, snapshot, source, materialize.SubjectQueryParameter, "limit integer") {
				t.Fatal("child insertion succeeded without valid owner CST authority")
			}
		})
	}
}

func TestSemanticPlacementUsesOnlySourceMapAuthority(t *testing.T) {
	t.Parallel()
	_, snapshot := semanticEditCompiledFixture(t)
	root := rootProjectAddress(snapshot)
	tests := []struct {
		name      string
		placement *SemanticPlacementHint
		owner     string
		fallback  string
		module    string
		conflict  SemanticConflictKind
	}{
		{name: "declared module", placement: &SemanticPlacementHint{ModulePath: "document.ldl"}, owner: root, fallback: "fallback", module: "document.ldl"},
		{name: "declared anchor", placement: &SemanticPlacementHint{GroupAnchorAddress: "ldl:project:p:entity:a"}, owner: root, fallback: "fallback", module: "document.ldl"},
		{name: "owner module", owner: "ldl:project:p:entity:a", fallback: "fallback", module: "document.ldl"},
		{name: "fallback", owner: "ldl:project:p:entity:missing", fallback: "fallback", module: "fallback"},
		{name: "missing module", placement: &SemanticPlacementHint{ModulePath: "missing.ldl"}, owner: root, fallback: "fallback", conflict: ConflictPlacementChanged},
		{name: "missing anchor", placement: &SemanticPlacementHint{GroupAnchorAddress: "ldl:project:p:entity:missing"}, owner: root, fallback: "fallback", conflict: ConflictPlacementChanged},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			module, conflict := placementModule(snapshot, test.placement, test.owner, test.fallback)
			if test.conflict != "" {
				if conflict == nil || conflict.Kind != test.conflict {
					t.Fatalf("conflict=%+v want kind=%q", conflict, test.conflict)
				}
				return
			}
			if conflict != nil || module != test.module {
				t.Fatalf("module=%q conflict=%+v want module=%q", module, conflict, test.module)
			}
		})
	}
}

func TestSemanticConflictDetailsHaveCanonicalOrder(t *testing.T) {
	t.Parallel()
	conflicts := []SemanticConflict{
		{Kind: ConflictPlacementChanged, TargetAddress: "same", OwnerAddress: "z"},
		{Kind: ConflictSubjectChanged, TargetAddress: "b"},
		{Kind: ConflictPlacementChanged, TargetAddress: "same", OwnerAddress: "a", ChildKind: materialize.SubjectRelation},
		{Kind: ConflictPlacementChanged, TargetAddress: "same", OwnerAddress: "a", ChildKind: materialize.SubjectEntity, Path: []string{"z"}},
		{Kind: ConflictPlacementChanged, TargetAddress: "same", OwnerAddress: "a", ChildKind: materialize.SubjectEntity, Path: []string{"a"}},
		{Kind: ConflictSubjectChanged, TargetAddress: "a"},
	}
	sortSemanticConflicts(conflicts)
	want := []string{
		"subject_changed:a:::",
		"subject_changed:b:::",
		"placement_changed:same:a:entity:a",
		"placement_changed:same:a:entity:z",
		"placement_changed:same:a:relation:",
		"placement_changed:same:z::",
	}
	for i, conflict := range conflicts {
		got := string(conflict.Kind) + ":" + conflict.TargetAddress + ":" + conflict.OwnerAddress + ":" + string(conflict.ChildKind) + ":" + strings.Join(conflict.Path, ".")
		if got != want[i] {
			t.Fatalf("conflict[%d]=%q want=%q", i, got, want[i])
		}
	}
}

func TestSemanticPlannerClonesAssetBytes(t *testing.T) {
	t.Parallel()
	input := projectCompileInput(semanticEditFixture)
	input.ReferencedAssets = []AssetInput{{Bytes: []byte("asset")}}
	cloned := cloneSemanticCompileInput(input)
	cloned.ReferencedAssets[0].Bytes[0] = 'X'
	if string(input.ReferencedAssets[0].Bytes) != "asset" {
		t.Fatalf("caller asset bytes mutated to %q", input.ReferencedAssets[0].Bytes)
	}
}

func TestSemanticRebaseClampsRangesInsideDeletedText(t *testing.T) {
	t.Parallel()
	edits := []PlannedSourceEdit{{StartByte: 0, EndByte: 4}}
	rangeValue := rebaseSourceRange(SourceRange{StartByte: 5, EndByte: 2}, edits)
	if rangeValue.StartByte != rangeValue.EndByte {
		t.Fatalf("source range was not clamped: %+v", rangeValue)
	}
	span := rebaseSyntaxSpan(syntax.Span{Start: 5, End: 2}, edits)
	if span.Start != span.End {
		t.Fatalf("syntax span was not clamped: %+v", span)
	}
}

func TestSemanticOperationsFailClosedWithoutCompilerAuthority(t *testing.T) {
	t.Parallel()
	input, snapshot := semanticEditCompiledFixture(t)
	stringValue := SemanticValue{Kind: SemanticValueString, String: "changed"}
	integerValue := SemanticValue{Kind: SemanticValueInteger, Integer: 11}
	tests := []struct {
		name string
		run  func() *SemanticConflict
		kind SemanticConflictKind
	}{
		{
			name: "relation endpoint without binding",
			kind: ConflictReferenceBroken,
			run: func() *SemanticConflict {
				withoutBindings := snapshot
				withoutBindings.SourceMap.Bindings = nil
				mutable := cloneSemanticCompileInput(input)
				conflict, _ := applyRelationEndpoint(&mutable, withoutBindings, SemanticOperation{RelationAddress: "ldl:project:p:relation:r", Endpoint: "to", EntityAddress: "ldl:project:p:entity:a"})
				return conflict
			},
		},
		{
			name: "display name without lossless CST",
			kind: ConflictSameFieldChanged,
			run: func() *SemanticConflict {
				withoutSyntax := snapshot
				withoutSyntax.LosslessSyntaxTree.Files = nil
				mutable := cloneSemanticCompileInput(input)
				conflict, _ := applyUpdateField(&mutable, withoutSyntax, SemanticOperation{TargetAddress: "ldl:project:p:entity:a", Path: []string{"display_name"}, Action: "set", Value: &stringValue})
				return conflict
			},
		},
		{
			name: "layer order without lossless CST",
			kind: ConflictSameFieldChanged,
			run: func() *SemanticConflict {
				withoutSyntax := snapshot
				withoutSyntax.LosslessSyntaxTree.Files = nil
				mutable := cloneSemanticCompileInput(input)
				conflict, _ := applyUpdateField(&mutable, withoutSyntax, SemanticOperation{TargetAddress: "ldl:project:p:layer:app", Path: []string{"order"}, Action: "set", Value: &integerValue})
				return conflict
			},
		},
		{
			name: "row owner absent from semantic index",
			kind: ConflictReferenceBroken,
			run: func() *SemanticConflict {
				mutable := cloneSemanticCompileInput(input)
				conflict, _ := appendRowGroup(&mutable, snapshot, "document.ldl", "ldl:project:p:entity:missing", "row", nil, nil)
				return conflict
			},
		},
		{
			name: "row cell blob has no authored scalar",
			kind: ConflictReferenceBroken,
			run: func() *SemanticConflict {
				mutable := cloneSemanticCompileInput(input)
				conflict, _ := appendRowGroup(&mutable, snapshot, "document.ldl", "ldl:project:p:entity:a", "row", []SemanticRowCell{{ColumnAddress: "ldl:project:p:entity-type:service:column:note", Value: SemanticValue{Kind: SemanticValueBlob}}}, nil)
				return conflict
			},
		},
		{
			name: "delete subject authored by an installed pack",
			kind: ConflictReferenceBroken,
			run: func() *SemanticConflict {
				withoutProjectAuthority := snapshot
				withoutProjectAuthority.SourceMap.Subjects = append([]index.SourceSubjectRecord(nil), snapshot.SourceMap.Subjects...)
				for i := range withoutProjectAuthority.SourceMap.Subjects {
					if withoutProjectAuthority.SourceMap.Subjects[i].Address == "ldl:project:p:entity:a" {
						module := *withoutProjectAuthority.SourceMap.Subjects[i].Module
						module.Origin.Kind = "pack"
						withoutProjectAuthority.SourceMap.Subjects[i].Module = &module
					}
				}
				mutable := cloneSemanticCompileInput(input)
				conflict, _ := applyDelete(&mutable, withoutProjectAuthority, "ldl:project:p:entity:a")
				return conflict
			},
		},
		{
			name: "move entity without declared type authority",
			kind: ConflictReferenceBroken,
			run: func() *SemanticConflict {
				withoutType := snapshot
				withoutType.CanonicalJSON = bytes.ReplaceAll(snapshot.CanonicalJSON, []byte("ldl:project:p:entity-type:service"), []byte("ldl:project:p:entity-type:missing"))
				mutable := cloneSemanticCompileInput(input)
				conflict, _ := applyMoveEntity(&mutable, withoutType, SemanticOperation{EntityAddress: "ldl:project:p:entity:a", LayerAddress: "ldl:project:p:layer:data"})
				return conflict
			},
		},
		{
			name: "row owner without declared type authority",
			kind: ConflictReferenceBroken,
			run: func() *SemanticConflict {
				withoutType := snapshot
				withoutType.CanonicalJSON = bytes.ReplaceAll(snapshot.CanonicalJSON, []byte("ldl:project:p:entity-type:service"), []byte("ldl:project:p:entity-type:missing"))
				mutable := cloneSemanticCompileInput(input)
				conflict, _ := appendRowGroup(&mutable, withoutType, "document.ldl", "ldl:project:p:entity:a", "row", nil, nil)
				return conflict
			},
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			conflict := test.run()
			if conflict == nil || conflict.Kind != test.kind {
				t.Fatalf("conflict=%+v want kind=%q", conflict, test.kind)
			}
		})
	}
}

func TestSemanticLookupRejectsMalformedSnapshotAuthority(t *testing.T) {
	t.Parallel()
	_, snapshot := semanticEditCompiledFixture(t)

	malformedJSON := snapshot
	malformedJSON.CanonicalJSON = []byte("{")
	if subject := normalizedSubject(malformedJSON, "ldl:project:p:entity:a"); subject != nil {
		t.Fatalf("malformed canonical JSON resolved a subject: %+v", subject)
	}

	withoutSourceMap := snapshot
	withoutSourceMap.SourceMap.Subjects = nil
	if _, _, ok := subjectRecord(withoutSourceMap, "ldl:project:p:entity:a"); ok {
		t.Fatal("semantic subject resolved without source-map authority")
	}

	if owner := ownerForSubject(Snapshot{}, index.SemanticSubject{}); owner != "" {
		t.Fatalf("owner=%q without normalized document authority", owner)
	}
}

func semanticEditCompiledFixture(t *testing.T) (CompileInput, Snapshot) {
	t.Helper()
	input := projectCompileInput(semanticEditFixture)
	compiled, err := New(BuildInfo{}).Compile(context.Background(), input)
	if err != nil || len(compiled.Diagnostics) != 0 {
		t.Fatalf("compile fixture: err=%v diagnostics=%+v", err, compiled.Diagnostics)
	}
	return input, compiled.Snapshot()
}

func semanticMapValue(entries ...string) SemanticValue {
	value := SemanticValue{Kind: SemanticValueMap}
	for i := 0; i+1 < len(entries); i += 2 {
		value.Map = append(value.Map, SemanticMapEntry{Key: entries[i], Value: SemanticValue{Kind: SemanticValueString, String: entries[i+1]}})
	}
	return value
}

func semanticCardinalityValue() SemanticValue {
	bound := func(max SemanticValue) SemanticValue {
		return SemanticValue{Kind: SemanticValueMap, Map: []SemanticMapEntry{
			{Key: "min", Value: SemanticValue{Kind: SemanticValueInteger}},
			{Key: "max", Value: max},
		}}
	}
	return SemanticValue{Kind: SemanticValueMap, Map: []SemanticMapEntry{
		{Key: "to_per_from", Value: bound(SemanticValue{Kind: SemanticValueString, String: "many"})},
		{Key: "from_per_to", Value: bound(SemanticValue{Kind: SemanticValueInteger, Integer: 1})},
	}}
}

func TestPlanSemanticEditsRejectsMalformedOperations(t *testing.T) {
	t.Parallel()
	planner := New(BuildInfo{})
	input := projectCompileInput(semanticEditFixture)
	compiled, err := planner.Compile(context.Background(), input)
	if err != nil {
		t.Fatal(err)
	}
	base := compiled.Snapshot()
	stringValue := SemanticValue{Kind: SemanticValueString, String: "value"}
	integerValue := SemanticValue{Kind: SemanticValueInteger, Integer: 1}
	addressValue := SemanticValue{Kind: SemanticValueAddress, Address: "ldl:project:p:entity:missing"}
	tests := []struct {
		name string
		op   SemanticOperation
	}{
		{name: "missing discriminator", op: SemanticOperation{}},
		{name: "unknown discriminator", op: SemanticOperation{Kind: "unknown"}},
		{name: "rename missing", op: SemanticOperation{Kind: OperationRenameSubject, TargetAddress: "ldl:project:p:entity:missing", NewID: "next"}},
		{name: "rename project through subject", op: SemanticOperation{Kind: OperationRenameSubject, TargetAddress: "ldl:project:p", NewID: "next"}},
		{name: "rename empty", op: SemanticOperation{Kind: OperationRenameSubject, TargetAddress: "ldl:project:p:entity:a"}},
		{name: "rename duplicate", op: SemanticOperation{Kind: OperationRenameSubject, TargetAddress: "ldl:project:p:entity:b", NewID: "a"}},
		{name: "delete missing", op: SemanticOperation{Kind: OperationDeleteSubject, TargetAddress: "ldl:project:p:entity:missing"}},
		{name: "endpoint missing relation", op: SemanticOperation{Kind: OperationUpdateRelationEndpoint, RelationAddress: "ldl:project:p:relation:missing", Endpoint: "to", EntityAddress: "ldl:project:p:entity:a"}},
		{name: "endpoint invalid side", op: SemanticOperation{Kind: OperationUpdateRelationEndpoint, RelationAddress: "ldl:project:p:relation:r", Endpoint: "middle", EntityAddress: "ldl:project:p:entity:a"}},
		{name: "endpoint missing entity", op: SemanticOperation{Kind: OperationUpdateRelationEndpoint, RelationAddress: "ldl:project:p:relation:r", Endpoint: "to", EntityAddress: "ldl:project:p:entity:missing"}},
		{name: "move missing entity", op: SemanticOperation{Kind: OperationMoveEntityToLayer, EntityAddress: "ldl:project:p:entity:missing", LayerAddress: "ldl:project:p:layer:data"}},
		{name: "move missing layer", op: SemanticOperation{Kind: OperationMoveEntityToLayer, EntityAddress: "ldl:project:p:entity:a", LayerAddress: "ldl:project:p:layer:missing"}},
		{name: "update missing", op: SemanticOperation{Kind: OperationUpdateSubjectField, TargetAddress: "ldl:project:p:entity:missing", Path: []string{"description"}, Action: "set", Value: &stringValue}},
		{name: "update empty path", op: SemanticOperation{Kind: OperationUpdateSubjectField, TargetAddress: "ldl:project:p:entity:a", Action: "set", Value: &stringValue}},
		{name: "update long path", op: SemanticOperation{Kind: OperationUpdateSubjectField, TargetAddress: "ldl:project:p:entity:a", Path: []string{"a", "b", "c"}, Action: "set", Value: &stringValue}},
		{name: "display wrong value", op: SemanticOperation{Kind: OperationUpdateSubjectField, TargetAddress: "ldl:project:p:entity:a", Path: []string{"display_name"}, Action: "set", Value: &integerValue}},
		{name: "layer order wrong value", op: SemanticOperation{Kind: OperationUpdateSubjectField, TargetAddress: "ldl:project:p:layer:app", Path: []string{"order"}, Action: "set", Value: &stringValue}},
		{name: "reference text wrong value", op: SemanticOperation{Kind: OperationUpdateSubjectField, TargetAddress: "ldl:project:p:reference:guide", Path: []string{"text"}, Action: "set", Value: &integerValue}},
		{name: "row cell missing value", op: SemanticOperation{Kind: OperationUpdateSubjectField, TargetAddress: "ldl:project:p:entity:a:row:primary", Path: []string{"values", "ldl:project:p:entity-type:service:column:note"}, Action: "set"}},
		{name: "annotation wrong value", op: SemanticOperation{Kind: OperationUpdateSubjectField, TargetAddress: "ldl:project:p:entity:a", Path: []string{"annotations", "team"}, Action: "set", Value: &integerValue}},
		{name: "generic missing value", op: SemanticOperation{Kind: OperationUpdateSubjectField, TargetAddress: "ldl:project:p:entity:a", Path: []string{"description"}, Action: "set"}},
		{name: "generic invalid action", op: SemanticOperation{Kind: OperationUpdateSubjectField, TargetAddress: "ldl:project:p:entity:a", Path: []string{"description"}, Action: "merge", Value: &stringValue}},
		{name: "generic missing address", op: SemanticOperation{Kind: OperationUpdateSubjectField, TargetAddress: "ldl:project:p:entity:a", Path: []string{"type_address"}, Action: "set", Value: &addressValue}},
		{name: "create relation wrong parent", op: SemanticOperation{Kind: OperationCreateRelation, ParentAddress: "ldl:project:p:layer:app", ID: "x"}},
		{name: "create relation missing type", op: SemanticOperation{Kind: OperationCreateRelation, ParentAddress: "ldl:project:p", ID: "x", TypeAddress: "ldl:project:p:relation-type:missing"}},
		{name: "create relation missing from", op: SemanticOperation{Kind: OperationCreateRelation, ParentAddress: "ldl:project:p", ID: "x", TypeAddress: "ldl:project:p:relation-type:calls", FromAddress: "ldl:project:p:entity:missing"}},
		{name: "create relation missing to", op: SemanticOperation{Kind: OperationCreateRelation, ParentAddress: "ldl:project:p", ID: "x", TypeAddress: "ldl:project:p:relation-type:calls", FromAddress: "ldl:project:p:entity:a", ToAddress: "ldl:project:p:entity:missing"}},
		{name: "upsert missing owner", op: SemanticOperation{Kind: OperationUpsertRow, OwnerAddress: "ldl:project:p:entity:missing", ID: "x"}},
		{name: "upsert missing column", op: SemanticOperation{Kind: OperationUpsertRow, OwnerAddress: "ldl:project:p:entity:a", ID: "x", Values: []SemanticRowCell{{ColumnAddress: "ldl:project:p:entity-type:service:column:missing", Value: stringValue}}}},
		{name: "create unknown subject", op: SemanticOperation{Kind: OperationCreateSubject, ParentAddress: "ldl:project:p", SubjectKind: "unknown", ID: "x"}},
		{name: "create invalid placement", op: SemanticOperation{Kind: OperationCreateSubject, ParentAddress: "ldl:project:p", SubjectKind: materialize.SubjectLayer, ID: "x", Fields: []SemanticMapEntry{{Key: "display_name", Value: stringValue}, {Key: "order", Value: integerValue}}, Placement: &SemanticPlacementHint{ModulePath: "missing.ldl", Position: "end"}}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			plan, planErr := planner.PlanSemanticEdits(context.Background(), SemanticEditPlanInput{BaseInput: input, BaseSnapshot: base, Batch: SemanticOperationBatch{Operations: []SemanticOperation{test.op}}, Preconditions: allSemanticPreconditions(base)})
			if planErr != nil || plan.Status != "invalid" || (len(plan.Conflicts) == 0 && len(plan.Diagnostics) == 0) {
				t.Fatalf("plan=%+v err=%v", plan, planErr)
			}
		})
	}
}

func TestSemanticEditPreconditionsFailClosedByAuthorityKind(t *testing.T) {
	t.Parallel()
	planner := New(BuildInfo{})
	input := projectCompileInput(semanticEditFixture)
	compiled, err := planner.Compile(context.Background(), input)
	if err != nil {
		t.Fatal(err)
	}
	base := compiled.Snapshot()
	op := SemanticOperation{Kind: OperationRenameSubject, TargetAddress: "ldl:project:p:entity:a", NewID: "alpha"}
	tests := []struct {
		name string
		edit func(*SemanticEditPreconditions)
		kind SemanticConflictKind
	}{
		{name: "subject", kind: ConflictSubjectChanged, edit: func(pre *SemanticEditPreconditions) { pre.ExpectedSubjectHashes[0].Hash = "sha256:stale" }},
		{name: "subtree", kind: ConflictSubtreeChanged, edit: func(pre *SemanticEditPreconditions) { pre.ExpectedSubtreeHashes[0].Hash = "sha256:stale" }},
		{name: "child set", kind: ConflictChildSetChanged, edit: func(pre *SemanticEditPreconditions) { pre.ExpectedChildSets[0].Hash = "sha256:stale" }},
		{name: "source", kind: ConflictSubjectChanged, edit: func(pre *SemanticEditPreconditions) { pre.ExpectedSourceDigests[0].Digest = "sha256:stale" }},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			pre := allSemanticPreconditions(base)
			test.edit(&pre)
			plan, planErr := planner.PlanSemanticEdits(context.Background(), SemanticEditPlanInput{BaseInput: input, BaseSnapshot: base, Batch: SemanticOperationBatch{Operations: []SemanticOperation{op}}, Preconditions: pre})
			if planErr != nil || plan.Status != "invalid" || len(plan.Conflicts) == 0 || plan.Conflicts[0].Kind != test.kind {
				t.Fatalf("plan=%+v err=%v", plan, planErr)
			}
		})
	}
}

func TestSemanticSourceDigestPreconditionsUseCompleteModuleIdentity(t *testing.T) {
	t.Parallel()
	projectDigest := semanticDigest([]byte("project"))
	packDigest := semanticDigest([]byte("pack"))
	snapshot := Snapshot{CompileOutput: CompileOutput{SourceMap: index.SourceMapV1{Files: []index.SourceFileRecord{
		{Origin: resolve.SourceOrigin{Kind: resolve.OriginPack, PackAddress: "ldl:pack:publisher:shared-pack"}, ModulePath: "same.ldl", Digest: packDigest},
		{Origin: resolve.SourceOrigin{Kind: resolve.OriginProject}, ModulePath: "same.ldl", Digest: projectDigest},
	}}}}
	pack := PlannedModuleRef{OriginKind: SourceOriginPack, PackAddress: "ldl:pack:publisher:shared-pack", ModulePath: "same.ldl"}
	project := PlannedModuleRef{OriginKind: SourceOriginProject, ModulePath: "same.ldl"}
	tests := []struct {
		name      string
		expected  []ExpectedSemanticSourceDigest
		conflicts int
	}{
		{name: "pack only exact", expected: []ExpectedSemanticSourceDigest{{Module: pack, Digest: packDigest}}},
		{name: "same path both origins exact", expected: []ExpectedSemanticSourceDigest{{Module: pack, Digest: packDigest}, {Module: project, Digest: projectDigest}}},
		{name: "project digest cannot satisfy pack", expected: []ExpectedSemanticSourceDigest{{Module: pack, Digest: projectDigest}}, conflicts: 1},
		{name: "pack address is part of identity", expected: []ExpectedSemanticSourceDigest{{Module: PlannedModuleRef{OriginKind: SourceOriginPack, PackAddress: "ldl:pack:publisher:other-pack", ModulePath: "same.ldl"}, Digest: packDigest}}, conflicts: 1},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			conflicts := validateSemanticPreconditions(snapshot, SemanticEditPreconditions{ExpectedSourceDigests: test.expected}, SemanticOperationBatch{})
			if len(conflicts) != test.conflicts {
				t.Fatalf("conflicts=%+v want count=%d", conflicts, test.conflicts)
			}
		})
	}
}

func TestSemanticEditPreconditionsRequireRelevantAuthority(t *testing.T) {
	t.Parallel()
	_, snapshot := semanticEditCompiledFixture(t)
	tests := []struct {
		name      string
		operation SemanticOperation
		remove    func(*SemanticEditPreconditions)
		kind      SemanticConflictKind
	}{
		{
			name:      "field update requires subject hash",
			operation: SemanticOperation{Kind: OperationUpdateSubjectField, TargetAddress: "ldl:project:p:entity:a"},
			remove: func(pre *SemanticEditPreconditions) {
				pre.ExpectedSubjectHashes = removeExpectedHash(pre.ExpectedSubjectHashes, "ldl:project:p:entity:a")
			},
			kind: ConflictSubjectChanged,
		},
		{
			name:      "rename requires subtree hash",
			operation: SemanticOperation{Kind: OperationRenameSubject, TargetAddress: "ldl:project:p:entity:a", NewID: "alpha"},
			remove: func(pre *SemanticEditPreconditions) {
				pre.ExpectedSubtreeHashes = removeExpectedHash(pre.ExpectedSubtreeHashes, "ldl:project:p:entity:a")
			},
			kind: ConflictSubtreeChanged,
		},
		{
			name:      "rename requires every reference source hash",
			operation: SemanticOperation{Kind: OperationRenameSubject, TargetAddress: "ldl:project:p:entity:a", NewID: "alpha"},
			remove: func(pre *SemanticEditPreconditions) {
				pre.ExpectedSubjectHashes = removeExpectedHash(pre.ExpectedSubjectHashes, "ldl:project:p:relation:r")
			},
			kind: ConflictSubjectChanged,
		},
		{
			name:      "project migration requires root subtree hash",
			operation: SemanticOperation{Kind: OperationMigrateProjectIdentity, ProjectAddress: "ldl:project:p", NewProjectID: "next"},
			remove: func(pre *SemanticEditPreconditions) {
				pre.ExpectedSubtreeHashes = removeExpectedHash(pre.ExpectedSubtreeHashes, "ldl:project:p")
			},
			kind: ConflictSubtreeChanged,
		},
		{
			name:      "relation creation requires child-set hash",
			operation: SemanticOperation{Kind: OperationCreateRelation, ParentAddress: "ldl:project:p", ID: "r2"},
			remove: func(pre *SemanticEditPreconditions) {
				pre.ExpectedChildSets = removeExpectedChildSet(pre.ExpectedChildSets, "ldl:project:p", materialize.SubjectRelation)
			},
			kind: ConflictChildSetChanged,
		},
		{
			name:      "new row requires owner row child-set hash",
			operation: SemanticOperation{Kind: OperationUpsertRow, OwnerAddress: "ldl:project:p:entity:b", ID: "secondary"},
			remove: func(pre *SemanticEditPreconditions) {
				pre.ExpectedChildSets = removeExpectedChildSet(pre.ExpectedChildSets, "ldl:project:p:entity:b", materialize.SubjectEntityRow)
			},
			kind: ConflictChildSetChanged,
		},
		{
			name:      "existing row requires row subject hash",
			operation: SemanticOperation{Kind: OperationUpsertRow, OwnerAddress: "ldl:project:p:entity:a", ID: "primary"},
			remove: func(pre *SemanticEditPreconditions) {
				pre.ExpectedSubjectHashes = removeExpectedHash(pre.ExpectedSubjectHashes, "ldl:project:p:entity:a:row:primary")
			},
			kind: ConflictSubjectChanged,
		},
		{
			name:      "row cell update requires owner schema subtree",
			operation: SemanticOperation{Kind: OperationUpdateSubjectField, TargetAddress: "ldl:project:p:entity:a:row:primary", Path: []string{"values", "ldl:project:p:entity-type:service:column:note"}},
			remove: func(pre *SemanticEditPreconditions) {
				pre.ExpectedSubtreeHashes = removeExpectedHash(pre.ExpectedSubtreeHashes, "ldl:project:p:entity-type:service")
			},
			kind: ConflictSubtreeChanged,
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			preconditions := allSemanticPreconditions(snapshot)
			test.remove(&preconditions)
			conflicts := validateSemanticPreconditions(snapshot, preconditions, SemanticOperationBatch{Operations: []SemanticOperation{test.operation}})
			if len(conflicts) != 1 || conflicts[0].Kind != test.kind {
				t.Fatalf("conflicts=%+v want one %q conflict", conflicts, test.kind)
			}
		})
	}
}

func removeExpectedHash(values []ExpectedSemanticHash, address string) []ExpectedSemanticHash {
	out := make([]ExpectedSemanticHash, 0, len(values))
	for _, value := range values {
		if value.Address != address {
			out = append(out, value)
		}
	}
	return out
}

func removeExpectedChildSet(values []ExpectedSemanticChildSet, owner string, kind SemanticSubjectKind) []ExpectedSemanticChildSet {
	out := make([]ExpectedSemanticChildSet, 0, len(values))
	for _, value := range values {
		if value.OwnerAddress != owner || value.ChildKind != kind {
			out = append(out, value)
		}
	}
	return out
}
