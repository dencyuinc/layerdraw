// SPDX-License-Identifier: LicenseRef-LayerDraw-1.0

package engine

import (
	"bytes"
	"context"
	"strings"
	"testing"

	"github.com/dencyuinc/layerdraw/internal/engine/internal/compiler/materialize"
)

func TestPlanSemanticEditsRequiresExactSourceGenerationAuthority(t *testing.T) {
	t.Parallel()
	planner := New(BuildInfo{})
	input, ancestor := semanticEditCompiledFixture(t)
	value := SemanticValue{Kind: SemanticValueString, String: "Updated"}
	operation := SemanticOperation{Kind: OperationUpdateSubjectField, TargetAddress: "ldl:project:p:entity:a", Path: []string{"display_name"}, Action: "set", Value: &value}

	t.Run("omitted source digest is rejected", func(t *testing.T) {
		preconditions := allSemanticPreconditions(ancestor)
		preconditions.ExpectedSourceDigests = nil
		plan, err := planner.PlanSemanticEdits(context.Background(), SemanticEditPlanInput{BaseInput: input, BaseSnapshot: ancestor, Batch: SemanticOperationBatch{Operations: []SemanticOperation{operation}}, Preconditions: preconditions})
		if err != nil || plan.Status != "invalid" || !hasSourceModuleConflict(plan.Conflicts, "document.ldl") {
			t.Fatalf("missing source read authority was accepted: plan=%+v err=%v", plan, err)
		}
	})

	t.Run("source-only generation change requires rebase and conflicts on the read module", func(t *testing.T) {
		headInput := cloneSemanticCompileInput(input)
		headInput.ProjectSourceTree["document.ldl"] = bytes.Replace(headInput.ProjectSourceTree["document.ldl"], []byte("// this comment"), []byte("// concurrent comment"), 1)
		head, compileErr := planner.Compile(context.Background(), headInput)
		if compileErr != nil || len(head.Diagnostics) != 0 || head.DefinitionHash != ancestor.DefinitionHash {
			t.Fatalf("compile source-only head: err=%v diagnostics=%+v hashes=%s/%s", compileErr, head.Diagnostics, ancestor.DefinitionHash, head.DefinitionHash)
		}
		withoutAuthority, err := planner.PlanSemanticEdits(context.Background(), SemanticEditPlanInput{BaseInput: headInput, BaseSnapshot: ancestor, Batch: SemanticOperationBatch{Operations: []SemanticOperation{operation}}, Preconditions: allSemanticPreconditions(ancestor)})
		if err != nil || withoutAuthority.Status != "invalid" || len(withoutAuthority.Conflicts) != 1 || withoutAuthority.Conflicts[0].Kind != ConflictStaleRevision {
			t.Fatalf("source-only generation bypassed rebase authority: plan=%+v err=%v", withoutAuthority, err)
		}
		generationOne := SemanticDocumentGeneration{EndpointInstanceID: "test-endpoint", DocumentHandle: "document_abcdefghijklmnop", Value: "1"}
		generationTwo := SemanticDocumentGeneration{EndpointInstanceID: "test-endpoint", DocumentHandle: "document_abcdefghijklmnop", Value: "2"}
		authority := &SemanticRebaseAuthority{AncestorGeneration: generationOne, CurrentGeneration: generationTwo, AncestorDefinitionHash: ancestor.DefinitionHash, CurrentDefinitionHash: head.DefinitionHash}
		withAuthority, err := planner.PlanSemanticEdits(context.Background(), SemanticEditPlanInput{BaseInput: headInput, BaseSnapshot: ancestor, Generation: generationTwo, RebaseAuthority: authority, Batch: SemanticOperationBatch{Operations: []SemanticOperation{operation}}, Preconditions: allSemanticPreconditions(ancestor)})
		if err != nil || withAuthority.Status != "invalid" || !hasSourceModuleConflict(withAuthority.Conflicts, "document.ldl") {
			t.Fatalf("stale relevant source module was accepted: plan=%+v err=%v", withAuthority, err)
		}
	})

	t.Run("unrelated module digest is not part of the operation read set", func(t *testing.T) {
		multiInput := projectTreeCompileInput(map[string][]byte{
			"document.ldl": []byte(`import { service } from "./schema.ldl"
project p "Project" {}
layers {
  app "Application" @10
}
entities service @app {
  a "A"
}
`),
			"schema.ldl": []byte(`// schema comment
entity_type service "Service" {
  representation shape rect
}
export { service }
`),
		})
		base, compileErr := planner.Compile(context.Background(), multiInput)
		if compileErr != nil || len(base.Diagnostics) != 0 {
			t.Fatalf("compile multi-module base: err=%v diagnostics=%+v", compileErr, base.Diagnostics)
		}
		multiAncestor := base.Snapshot()
		headInput := cloneSemanticCompileInput(multiInput)
		headInput.ProjectSourceTree["schema.ldl"] = bytes.Replace(headInput.ProjectSourceTree["schema.ldl"], []byte("// schema comment"), []byte("// concurrent schema comment"), 1)
		head, headErr := planner.Compile(context.Background(), headInput)
		if headErr != nil || len(head.Diagnostics) != 0 || head.DefinitionHash != multiAncestor.DefinitionHash {
			t.Fatalf("compile unrelated source head: err=%v diagnostics=%+v", headErr, head.Diagnostics)
		}
		generationOne := SemanticDocumentGeneration{EndpointInstanceID: "test-endpoint", DocumentHandle: "document_abcdefghijklmnop", Value: "1"}
		generationTwo := SemanticDocumentGeneration{EndpointInstanceID: "test-endpoint", DocumentHandle: "document_abcdefghijklmnop", Value: "2"}
		authority := &SemanticRebaseAuthority{AncestorGeneration: generationOne, CurrentGeneration: generationTwo, AncestorDefinitionHash: multiAncestor.DefinitionHash, CurrentDefinitionHash: head.DefinitionHash}
		preconditions := allSemanticPreconditions(multiAncestor)
		kept := preconditions.ExpectedSourceDigests[:0]
		for _, expected := range preconditions.ExpectedSourceDigests {
			if expected.Module.ModulePath == "document.ldl" {
				kept = append(kept, expected)
			}
		}
		preconditions.ExpectedSourceDigests = kept
		plan, planErr := planner.PlanSemanticEdits(context.Background(), SemanticEditPlanInput{BaseInput: headInput, BaseSnapshot: multiAncestor, Generation: generationTwo, RebaseAuthority: authority, Batch: SemanticOperationBatch{Operations: []SemanticOperation{operation}}, Preconditions: preconditions})
		if planErr != nil || plan.Status != "valid" || !bytes.Contains(plan.SourceTree["document.ldl"], []byte(`a "Updated"`)) {
			t.Fatalf("unrelated module was forced into the source read set: status=%s err=%v conflicts=%+v", plan.Status, planErr, plan.Conflicts)
		}
	})
}

func hasSourceModuleConflict(conflicts []SemanticConflict, module string) bool {
	for _, conflict := range conflicts {
		if conflict.Kind == ConflictSubjectChanged && len(conflict.Path) == 3 && conflict.Path[2] == module {
			return true
		}
	}
	return false
}

func TestPlanSemanticEditsUsesProvenPackReferenceSpelling(t *testing.T) {
	t.Parallel()
	planner := New(BuildInfo{})
	input := installedPackProjectInput()
	input.ProjectSourceTree["document.ldl"] = []byte(`import { service as remote_service } from "schema"
project p "Project" {}
layers {
  app "Application" @10
}
entity_type service "Local Service" {
  representation shape rect
}
`)
	compiled, err := planner.Compile(context.Background(), input)
	if err != nil || len(compiled.Diagnostics) != 0 {
		t.Fatalf("compile pack collision fixture: err=%v diagnostics=%+v", err, compiled.Diagnostics)
	}
	snapshot := compiled.Snapshot()
	packType := ""
	for _, subject := range snapshot.SemanticIndex.Subjects {
		if subject.Kind == materialize.SubjectEntityType && strings.HasPrefix(subject.Address, "ldl:pack:") {
			packType = subject.Address
		}
	}
	if packType == "" {
		t.Fatal("pack entity type address missing")
	}
	operation := SemanticOperation{Kind: OperationCreateSubject, ParentAddress: "ldl:project:p", SubjectKind: materialize.SubjectEntity, ID: "remote", Fields: []SemanticMapEntry{
		{Key: "display_name", Value: SemanticValue{Kind: SemanticValueString, String: "Remote"}},
		{Key: "type_address", Value: SemanticValue{Kind: SemanticValueAddress, Address: packType}},
		{Key: "layer_address", Value: SemanticValue{Kind: SemanticValueAddress, Address: "ldl:project:p:layer:app"}},
	}}
	plan, planErr := planner.PlanSemanticEdits(context.Background(), SemanticEditPlanInput{BaseInput: input, BaseSnapshot: snapshot, Batch: SemanticOperationBatch{Operations: []SemanticOperation{operation}}, Preconditions: allSemanticPreconditions(snapshot)})
	if planErr != nil || plan.Status != "valid" {
		t.Fatalf("aliased pack target failed: status=%s err=%v conflicts=%+v diagnostics=%+v", plan.Status, planErr, plan.Conflicts, plan.Diagnostics)
	}
	got := string(plan.SourceTree["document.ldl"])
	if !strings.Contains(got, "entities remote_service @app") || strings.Contains(got, "entities service @app") {
		t.Fatalf("pack target was rendered as the colliding local terminal ID:\n%s", got)
	}
	object := normalizedSubject(*plan.Result, "ldl:project:p:entity:remote")
	if object["type_address"] != packType {
		t.Fatalf("operation fact resolved to %v, want exact pack StableAddress %s", object["type_address"], packType)
	}

	headInput := cloneSemanticCompileInput(input)
	headInput.ProjectSourceTree["document.ldl"] = bytes.Replace(headInput.ProjectSourceTree["document.ldl"], []byte(`import { service as remote_service } from "schema"`+"\n"), nil, 1)
	head, headErr := planner.Compile(context.Background(), headInput)
	if headErr != nil || len(head.Diagnostics) != 0 {
		t.Fatalf("compile collision-only head: err=%v diagnostics=%+v", headErr, head.Diagnostics)
	}
	generationOne := SemanticDocumentGeneration{EndpointInstanceID: "test-endpoint", DocumentHandle: "document_abcdefghijklmnop", Value: "1"}
	generationTwo := SemanticDocumentGeneration{EndpointInstanceID: "test-endpoint", DocumentHandle: "document_abcdefghijklmnop", Value: "2"}
	authority := &SemanticRebaseAuthority{AncestorGeneration: generationOne, CurrentGeneration: generationTwo, AncestorDefinitionHash: snapshot.DefinitionHash, CurrentDefinitionHash: head.DefinitionHash}
	rejected, rejectErr := planner.PlanSemanticEdits(context.Background(), SemanticEditPlanInput{BaseInput: headInput, BaseSnapshot: snapshot, Generation: generationTwo, RebaseAuthority: authority, Batch: SemanticOperationBatch{Operations: []SemanticOperation{operation}}, Preconditions: allSemanticPreconditions(snapshot)})
	if rejectErr != nil || rejected.Status != "invalid" || len(rejected.SourceTree) != 0 {
		t.Fatalf("unprovable pack reference fell back to the colliding local declaration: plan=%+v err=%v", rejected, rejectErr)
	}
	foundExactTarget := false
	for _, conflict := range rejected.Conflicts {
		foundExactTarget = foundExactTarget || (conflict.Kind == ConflictReferenceBroken || conflict.Kind == ConflictDeleteVsUpdate) && conflict.TargetAddress == packType
	}
	if !foundExactTarget {
		t.Fatalf("wrong-source write did not fail against the exact requested StableAddress: %+v", rejected.Conflicts)
	}
}

func TestPlanSemanticEditsVerifiesConstraintFactsAtTheOperationSource(t *testing.T) {
	t.Parallel()
	planner := New(BuildInfo{})
	fixture := strings.Replace(semanticEditFixture, `entity_type service "Service" {
  representation shape rect
  columns {
    note "Note" string
  }
}
`, `entity_type service "Service" {
  representation shape rect
  columns {
    note "Note" string
  }
}

entity_type alternate "Alternate" {
  representation shape rect
  columns {
    note "Note" string
  }
  unique existing_note [note]
}
`, 1)
	input := projectCompileInput(fixture)
	compiled, err := planner.Compile(context.Background(), input)
	if err != nil || len(compiled.Diagnostics) != 0 {
		t.Fatalf("compile owner-collision fixture: err=%v diagnostics=%+v", err, compiled.Diagnostics)
	}
	snapshot := compiled.Snapshot()
	constraintAddress := "ldl:project:p:entity-type:service:constraint:note_unique"
	serviceColumn := "ldl:project:p:entity-type:service:column:note"
	alternateColumn := "ldl:project:p:entity-type:alternate:column:note"

	tests := []struct {
		name       string
		column     string
		wantStatus string
	}{
		{name: "owner-local column is verified by the normalized constraint", column: serviceColumn, wantStatus: "valid"},
		{name: "same terminal ID from another owner is rejected", column: alternateColumn, wantStatus: "invalid"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			operation := SemanticOperation{Kind: OperationCreateSubject, ParentAddress: "ldl:project:p:entity-type:service", SubjectKind: materialize.SubjectEntityTypeConstraint, ID: "note_unique", Fields: []SemanticMapEntry{{
				Key:   "column_addresses",
				Value: SemanticValue{Kind: SemanticValueArray, Array: []SemanticValue{{Kind: SemanticValueAddress, Address: test.column}}},
			}}}
			plan, planErr := planner.PlanSemanticEdits(context.Background(), SemanticEditPlanInput{BaseInput: input, BaseSnapshot: snapshot, Batch: SemanticOperationBatch{Operations: []SemanticOperation{operation}}, Preconditions: allSemanticPreconditions(snapshot)})
			if planErr != nil || plan.Status != test.wantStatus {
				t.Fatalf("status=%s, want %s: err=%v conflicts=%+v diagnostics=%+v", plan.Status, test.wantStatus, planErr, plan.Conflicts, plan.Diagnostics)
			}
			if test.wantStatus == "invalid" {
				if len(plan.SourceTree) != 0 || len(plan.Conflicts) != 1 || plan.Conflicts[0].Kind != ConflictReferenceBroken || plan.Conflicts[0].OwnerAddress != constraintAddress || plan.Conflicts[0].TargetAddress != alternateColumn {
					t.Fatalf("wrong-source constraint write did not fail atomically and precisely: %+v", plan)
				}
				return
			}
			for _, reference := range plan.Result.SemanticIndex.References {
				if reference.SourceAddress == constraintAddress && reference.TargetAddress == serviceColumn {
					t.Fatalf("constraint unexpectedly emitted a SemanticIndex reference; normalized column_addresses must remain its result authority: %+v", reference)
				}
			}
			object := normalizedSubject(*plan.Result, constraintAddress)
			columns, ok := object["column_addresses"].([]any)
			if !ok || len(columns) != 1 || columns[0] != serviceColumn {
				t.Fatalf("normalized constraint columns=%v, want [%s]", object["column_addresses"], serviceColumn)
			}
		})
	}
}

func TestPlanSemanticEditsTypedOverlayPreservesAuthoredKinds(t *testing.T) {
	t.Parallel()
	planner := New(BuildInfo{})
	input, snapshot := semanticEditCompiledFixture(t)
	required := SemanticOperation{Kind: OperationCreateSubject, ParentAddress: "ldl:project:p:entity-type:service", SubjectKind: materialize.SubjectEntityTypeColumn, ID: "required_count", Fields: []SemanticMapEntry{
		{Key: "display_name", Value: SemanticValue{Kind: SemanticValueString, String: "Required Count"}},
		{Key: "value_type", Value: SemanticValue{Kind: SemanticValueToken, String: "integer"}},
		{Key: "required", Value: SemanticValue{Kind: SemanticValueBoolean, Boolean: true}},
	}}
	annotationArray := SemanticValue{Kind: SemanticValueArray, Array: []SemanticValue{{Kind: SemanticValueMap, Map: []SemanticMapEntry{
		{Key: "key", Value: SemanticValue{Kind: SemanticValueString, String: "team"}},
		{Key: "value", Value: SemanticValue{Kind: SemanticValueString, String: "platform"}},
	}}}}
	owner := SemanticValue{Kind: SemanticValueString, String: "architecture"}
	stableLookingString := SemanticValue{Kind: SemanticValueString, String: "ldl:project:p:entity-type:service"}
	operations := []SemanticOperation{
		required,
		{Kind: OperationCreateSubject, ParentAddress: "ldl:project:p", SubjectKind: materialize.SubjectEntity, ID: "c", Fields: []SemanticMapEntry{
			{Key: "display_name", Value: SemanticValue{Kind: SemanticValueString, String: "C"}},
			{Key: "type_address", Value: SemanticValue{Kind: SemanticValueAddress, Address: "ldl:project:p:entity-type:service"}},
			{Key: "layer_address", Value: SemanticValue{Kind: SemanticValueAddress, Address: "ldl:project:p:layer:app"}},
			{Key: "annotations", Value: annotationArray},
		}},
		{Kind: OperationUpdateSubjectField, TargetAddress: "ldl:project:p:entity:c", Path: []string{"annotations", "owner"}, Action: "set", Value: &owner},
		{Kind: OperationUpdateSubjectField, TargetAddress: "ldl:project:p:entity:c", Path: []string{"annotations", "literal"}, Action: "set", Value: &stableLookingString},
		{Kind: OperationRenameSubject, TargetAddress: "ldl:project:p:entity-type:service", NewID: "backend"},
		{Kind: OperationUpsertRow, OwnerAddress: "ldl:project:p:entity:c", ID: "primary", Values: []SemanticRowCell{
			{ColumnAddress: "ldl:project:p:entity-type:backend:column:note", Value: SemanticValue{Kind: SemanticValueString, String: "new"}},
			{ColumnAddress: "ldl:project:p:entity-type:backend:column:required_count", Value: SemanticValue{Kind: SemanticValueInteger, Integer: 2}},
		}},
		{Kind: OperationUpsertRow, OwnerAddress: "ldl:project:p:entity:a", ID: "primary", Values: []SemanticRowCell{
			{ColumnAddress: "ldl:project:p:entity-type:backend:column:note", Value: SemanticValue{Kind: SemanticValueString, String: "old"}},
			{ColumnAddress: "ldl:project:p:entity-type:backend:column:required_count", Value: SemanticValue{Kind: SemanticValueInteger, Integer: 1}},
		}},
	}
	plan, err := planner.PlanSemanticEdits(context.Background(), SemanticEditPlanInput{BaseInput: input, BaseSnapshot: snapshot, Batch: SemanticOperationBatch{Operations: operations}, Preconditions: allSemanticPreconditions(snapshot)})
	if err != nil || plan.Status != "valid" {
		t.Fatalf("typed overlay batch failed: status=%s err=%v conflicts=%+v diagnostics=%+v", plan.Status, err, plan.Conflicts, plan.Diagnostics)
	}
	got := string(plan.SourceTree["document.ldl"])
	if !strings.Contains(got, `literal: "ldl:project:p:entity-type:service"`) || !strings.Contains(got, `owner: "architecture"`) || !strings.Contains(got, `team: "platform"`) {
		t.Fatalf("typed annotations or StableAddress-looking string were corrupted:\n%s", got)
	}
}

func TestPlanSemanticEditsSharesProjectAndRenameLifecycle(t *testing.T) {
	t.Parallel()
	planner := New(BuildInfo{})

	t.Run("project migration enables top-level create", func(t *testing.T) {
		input, snapshot := semanticEditCompiledFixture(t)
		operations := []SemanticOperation{
			{Kind: OperationMigrateProjectIdentity, ProjectAddress: "ldl:project:p", NewProjectID: "q"},
			{Kind: OperationCreateSubject, ParentAddress: "ldl:project:q", SubjectKind: materialize.SubjectLayer, ID: "extra", Fields: []SemanticMapEntry{
				{Key: "display_name", Value: SemanticValue{Kind: SemanticValueString, String: "Extra"}},
				{Key: "order", Value: SemanticValue{Kind: SemanticValueInteger, Integer: 30}},
			}},
		}
		plan, err := planner.PlanSemanticEdits(context.Background(), SemanticEditPlanInput{BaseInput: input, BaseSnapshot: snapshot, Batch: SemanticOperationBatch{Operations: operations}, Preconditions: allSemanticPreconditions(snapshot)})
		if err != nil || plan.Status != "valid" || !strings.Contains(string(plan.SourceTree["document.ldl"]), `extra "Extra" @30`) {
			t.Fatalf("migrated root was unavailable: status=%s err=%v conflicts=%+v diagnostics=%+v", plan.Status, err, plan.Conflicts, plan.Diagnostics)
		}
	})

	t.Run("migrated root remains authoritative through invalid intermediate", func(t *testing.T) {
		input, snapshot := semanticEditCompiledFixture(t)
		operations := []SemanticOperation{
			{Kind: OperationCreateSubject, ParentAddress: "ldl:project:p:entity-type:service", SubjectKind: materialize.SubjectEntityTypeColumn, ID: "required_count", Fields: []SemanticMapEntry{
				{Key: "display_name", Value: SemanticValue{Kind: SemanticValueString, String: "Required Count"}},
				{Key: "value_type", Value: SemanticValue{Kind: SemanticValueToken, String: "integer"}},
				{Key: "required", Value: SemanticValue{Kind: SemanticValueBoolean, Boolean: true}},
			}},
			{Kind: OperationMigrateProjectIdentity, ProjectAddress: "ldl:project:p", NewProjectID: "q"},
			{Kind: OperationCreateRelation, ParentAddress: "ldl:project:q", ID: "second", TypeAddress: "ldl:project:q:relation-type:calls", FromAddress: "ldl:project:q:entity:b", ToAddress: "ldl:project:q:entity:a", Fields: []SemanticMapEntry{{Key: "display_name", Value: SemanticValue{Kind: SemanticValueString, String: "Second"}}}},
			{Kind: OperationUpsertRow, OwnerAddress: "ldl:project:q:entity:a", ID: "primary", Values: []SemanticRowCell{
				{ColumnAddress: "ldl:project:q:entity-type:service:column:note", Value: SemanticValue{Kind: SemanticValueString, String: "old"}},
				{ColumnAddress: "ldl:project:q:entity-type:service:column:required_count", Value: SemanticValue{Kind: SemanticValueInteger, Integer: 1}},
			}},
		}
		plan, err := planner.PlanSemanticEdits(context.Background(), SemanticEditPlanInput{BaseInput: input, BaseSnapshot: snapshot, Batch: SemanticOperationBatch{Operations: operations}, Preconditions: allSemanticPreconditions(snapshot)})
		if err != nil || plan.Status != "valid" || !strings.Contains(string(plan.SourceTree["document.ldl"]), "second: b -> a") {
			t.Fatalf("invalid-intermediate project lifecycle failed: status=%s err=%v conflicts=%+v diagnostics=%+v", plan.Status, err, plan.Conflicts, plan.Diagnostics)
		}
	})

	t.Run("rename chain exposes descendant destination during rebase", func(t *testing.T) {
		ancestorInput, ancestor := semanticEditCompiledFixture(t)
		headInput := cloneSemanticCompileInput(ancestorInput)
		headInput.ProjectSourceTree["document.ldl"] = bytes.Replace(headInput.ProjectSourceTree["document.ldl"], []byte(`query q "Query"`), []byte(`query q "Concurrent Query"`), 1)
		head, compileErr := planner.Compile(context.Background(), headInput)
		if compileErr != nil || len(head.Diagnostics) != 0 {
			t.Fatalf("compile rebase head: err=%v diagnostics=%+v", compileErr, head.Diagnostics)
		}
		generationOne := SemanticDocumentGeneration{EndpointInstanceID: "test-endpoint", DocumentHandle: "document_abcdefghijklmnop", Value: "1"}
		generationTwo := SemanticDocumentGeneration{EndpointInstanceID: "test-endpoint", DocumentHandle: "document_abcdefghijklmnop", Value: "2"}
		authority := &SemanticRebaseAuthority{AncestorGeneration: generationOne, CurrentGeneration: generationTwo, AncestorDefinitionHash: ancestor.DefinitionHash, CurrentDefinitionHash: head.DefinitionHash}
		name := SemanticValue{Kind: SemanticValueString, String: "Renamed Note"}
		operations := []SemanticOperation{
			{Kind: OperationRenameSubject, TargetAddress: "ldl:project:p:entity-type:service", NewID: "backend"},
			{Kind: OperationRenameSubject, TargetAddress: "ldl:project:p:entity-type:backend", NewID: "platform"},
			{Kind: OperationUpdateSubjectField, TargetAddress: "ldl:project:p:entity-type:platform:column:note", Path: []string{"display_name"}, Action: "set", Value: &name},
		}
		plan, err := planner.PlanSemanticEdits(context.Background(), SemanticEditPlanInput{BaseInput: headInput, BaseSnapshot: ancestor, Generation: generationTwo, RebaseAuthority: authority, Batch: SemanticOperationBatch{Operations: operations}, Preconditions: allSemanticPreconditions(ancestor)})
		if err != nil || plan.Status != "valid" || !strings.Contains(string(plan.SourceTree["document.ldl"]), `note "Renamed Note" string`) {
			t.Fatalf("rename-chain descendant lifecycle diverged: status=%s err=%v conflicts=%+v diagnostics=%+v", plan.Status, err, plan.Conflicts, plan.Diagnostics)
		}
	})
}

func TestPlanSemanticEditsCleansDeterministicRenameChains(t *testing.T) {
	t.Parallel()
	planner := New(BuildInfo{})
	input, snapshot := semanticEditCompiledFixture(t)

	t.Run("top-level chain delete reserves every historical ID", func(t *testing.T) {
		operations := []SemanticOperation{
			{Kind: OperationRenameSubject, TargetAddress: "ldl:project:p:reference:guide", NewID: "middle"},
			{Kind: OperationRenameSubject, TargetAddress: "ldl:project:p:reference:middle", NewID: "final"},
			{Kind: OperationDeleteSubject, TargetAddress: "ldl:project:p:reference:final"},
		}
		plan, err := planner.PlanSemanticEdits(context.Background(), SemanticEditPlanInput{BaseInput: input, BaseSnapshot: snapshot, Batch: SemanticOperationBatch{Operations: operations}, Preconditions: allSemanticPreconditions(snapshot)})
		if err != nil || plan.Status != "valid" {
			t.Fatalf("rename-chain delete failed: status=%s err=%v conflicts=%+v diagnostics=%+v", plan.Status, err, plan.Conflicts, plan.Diagnostics)
		}
		got := string(plan.SourceTree["document.ldl"])
		if strings.Contains(got, "moves {") || !strings.Contains(got, "references [final, guide, middle]") {
			t.Fatalf("rename-chain cleanup was incomplete or nondeterministic:\n%s", got)
		}
	})

	t.Run("chain destination supports another rename and update", func(t *testing.T) {
		name := SemanticValue{Kind: SemanticValueString, String: "Final Entity"}
		operations := []SemanticOperation{
			{Kind: OperationRenameSubject, TargetAddress: "ldl:project:p:entity:a", NewID: "alpha"},
			{Kind: OperationRenameSubject, TargetAddress: "ldl:project:p:entity:alpha", NewID: "omega"},
			{Kind: OperationRenameSubject, TargetAddress: "ldl:project:p:entity:omega", NewID: "final"},
			{Kind: OperationUpdateSubjectField, TargetAddress: "ldl:project:p:entity:final", Path: []string{"display_name"}, Action: "set", Value: &name},
		}
		plan, err := planner.PlanSemanticEdits(context.Background(), SemanticEditPlanInput{BaseInput: input, BaseSnapshot: snapshot, Batch: SemanticOperationBatch{Operations: operations}, Preconditions: allSemanticPreconditions(snapshot)})
		if err != nil || plan.Status != "valid" {
			t.Fatalf("rename-chain continuation failed: status=%s err=%v conflicts=%+v diagnostics=%+v", plan.Status, err, plan.Conflicts, plan.Diagnostics)
		}
		got := string(plan.SourceTree["document.ldl"])
		for _, move := range []string{"entity a -> alpha", "entity alpha -> omega", "entity omega -> final"} {
			if !strings.Contains(got, move) {
				t.Fatalf("rename chain omitted %q:\n%s", move, got)
			}
		}
		if !strings.Contains(got, `final "Final Entity"`) {
			t.Fatalf("final renamed destination was not writable:\n%s", got)
		}
	})

	t.Run("row rename then delete uses exact move item", func(t *testing.T) {
		operations := []SemanticOperation{
			{Kind: OperationRenameSubject, TargetAddress: "ldl:project:p:entity:a:row:primary", NewID: "archived"},
			{Kind: OperationDeleteRow, RowAddress: "ldl:project:p:entity:a:row:archived"},
		}
		plan, err := planner.PlanSemanticEdits(context.Background(), SemanticEditPlanInput{BaseInput: input, BaseSnapshot: snapshot, Batch: SemanticOperationBatch{Operations: operations}, Preconditions: allSemanticPreconditions(snapshot)})
		if err != nil || plan.Status != "valid" {
			t.Fatalf("row rename-delete failed: status=%s err=%v conflicts=%+v diagnostics=%+v", plan.Status, err, plan.Conflicts, plan.Diagnostics)
		}
		got := string(plan.SourceTree["document.ldl"])
		if strings.Contains(got, "entity_row a primary -> archived") || strings.Contains(got, "a archived:") || !strings.Contains(got, "reserve_rows [archived, primary]") {
			t.Fatalf("row lineage cleanup or reservations were wrong:\n%s", got)
		}
	})
}

func TestPlanSemanticEditsRejectsMissingAddressInsideCollection(t *testing.T) {
	t.Parallel()
	planner := New(BuildInfo{})
	input, snapshot := semanticEditCompiledFixture(t)
	missing := "ldl:project:p:layer:missing"
	value := SemanticValue{Kind: SemanticValueMap, Map: []SemanticMapEntry{{Key: "layer_addresses", Value: SemanticValue{Kind: SemanticValueArray, Array: []SemanticValue{
		{Kind: SemanticValueAddress, Address: "ldl:project:p:layer:app"},
		{Kind: SemanticValueAddress, Address: missing},
	}}}}}
	operation := SemanticOperation{Kind: OperationUpdateSubjectField, TargetAddress: "ldl:project:p:query:q", Path: []string{"select"}, Action: "set", Value: &value}
	original := bytes.Clone(input.ProjectSourceTree["document.ldl"])
	plan, err := planner.PlanSemanticEdits(context.Background(), SemanticEditPlanInput{BaseInput: input, BaseSnapshot: snapshot, Batch: SemanticOperationBatch{Operations: []SemanticOperation{operation}}, Preconditions: allSemanticPreconditions(snapshot)})
	if err != nil || plan.Status != "invalid" {
		t.Fatalf("missing collection address was accepted: plan=%+v err=%v", plan, err)
	}
	found := false
	for _, conflict := range plan.Conflicts {
		found = found || conflict.Kind == ConflictReferenceBroken && conflict.TargetAddress == missing
	}
	if !found || !bytes.Equal(original, input.ProjectSourceTree["document.ldl"]) || len(plan.SourceTree) != 0 {
		t.Fatalf("missing collection address did not fail closed atomically: %+v", plan)
	}
}
