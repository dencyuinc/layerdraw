// SPDX-License-Identifier: LicenseRef-LayerDraw-1.0

package endpoint

import (
	"context"
	"os"
	"reflect"
	"testing"

	"github.com/dencyuinc/layerdraw/gen/go/engineprotocol"
	"github.com/dencyuinc/layerdraw/gen/go/protocolcommon"
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
	nestedUnionMapped := false
	for _, operation := range mapped.Batch.Operations {
		if operation.Kind != engine.OperationCreateSubject || operation.ParentAddress == "" || operation.ID == "" || len(operation.Fields) == 0 {
			t.Fatalf("incomplete mapped operation: %+v", operation)
		}
		seen[operation.SubjectKind] = true
		if operation.SubjectKind == "entity_type" {
			for _, field := range operation.Fields {
				if field.Key == "representation" && field.Value.Kind == engine.SemanticValueMap && len(field.Value.Map) != 0 {
					nestedUnionMapped = true
				}
			}
		}
	}
	for _, kind := range []engine.SemanticSubjectKind{"entity_type", "relation_type", "layer", "entity", "query", "view", "reference", "entity_type_column", "entity_type_constraint", "relation_type_column", "relation_type_constraint", "query_parameter", "view_table_column", "view_export"} {
		if !seen[kind] {
			t.Fatalf("missing generated create kind %q", kind)
		}
	}
	if !nestedUnionMapped {
		t.Fatal("nested generated representation union was not mapped structurally")
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

func TestMapPreviewOperationsPlanInputPreservesGeneratedDiscriminatorsAndBounds(t *testing.T) {
	t.Parallel()
	data, err := os.ReadFile("../../../schemas/fixtures/engine/workbench-preview-operations-request.json")
	if err != nil {
		t.Fatal(err)
	}
	request, err := engineprotocol.DecodePreviewOperationsRequestEnvelope(data)
	if err != nil {
		t.Fatal(err)
	}
	mapped, err := MapPreviewOperationsPlanInput(engine.CompileInput{}, engine.Snapshot{}, request.Payload)
	if err != nil {
		t.Fatal(err)
	}
	if mapped.Generation.EndpointInstanceID != "fixture-endpoint" || mapped.Generation.DocumentHandle != "document_abcdefghijklmnop" || mapped.Generation.Value != "7" {
		t.Fatalf("document generation was not preserved: %+v", mapped.Generation)
	}
	if mapped.Limits.MaxItems != 128 || mapped.Limits.MaxOutputBytes != 65536 {
		t.Fatalf("workbench limits were not preserved: %+v", mapped.Limits)
	}
}

func TestMapGeneratedOperationsKeepsRowIDAndTypedCreateValues(t *testing.T) {
	t.Parallel()
	t.Run("row_id is the upsert identity", func(t *testing.T) {
		batch, err := engineprotocol.DecodeSemanticOperationBatch([]byte(`{
  "operations": [{
    "operation": "upsert_row",
    "owner_address": "ldl:project:p:entity:a",
    "row_id": "primary",
    "values": [{"column_address": "ldl:project:p:entity-type:t:column:value", "value": {"kind": "string", "string": "ldl:authored-text"}}],
    "explicit_absent_column_addresses": []
  }]
}`))
		if err != nil {
			t.Fatal(err)
		}
		mapped, err := MapSemanticEditPlanInput(engine.CompileInput{}, engine.Snapshot{}, engineprotocol.EngineEditPreconditions{}, batch)
		if err != nil {
			t.Fatal(err)
		}
		operation := mapped.Batch.Operations[0]
		if operation.ID != "primary" {
			t.Fatalf("row_id was dropped: %+v", operation)
		}
		if operation.Values[0].Value.Kind != engine.SemanticValueString || operation.Values[0].Value.String != "ldl:authored-text" {
			t.Fatalf("tagged authored text was reclassified: %+v", operation.Values[0].Value)
		}
	})

	t.Run("create fields retain generated scalar types", func(t *testing.T) {
		batch, err := engineprotocol.DecodeSemanticOperationBatch([]byte(`{
  "operations": [{
    "operation": "create_subject",
    "subject_kind": "layer",
    "parent_address": "ldl:project:p",
    "id": "extra",
    "fields": {"display_name": "ldl:authored-text", "order": "12"}
  }]
}`))
		if err != nil {
			t.Fatal(err)
		}
		mapped, err := MapSemanticEditPlanInput(engine.CompileInput{}, engine.Snapshot{}, engineprotocol.EngineEditPreconditions{}, batch)
		if err != nil {
			t.Fatal(err)
		}
		fields := map[string]engine.SemanticValue{}
		for _, field := range mapped.Batch.Operations[0].Fields {
			fields[field.Key] = field.Value
		}
		if fields["display_name"].Kind != engine.SemanticValueString || fields["display_name"].String != "ldl:authored-text" {
			t.Fatalf("authored string was guessed as an address: %+v", fields["display_name"])
		}
		if fields["order"].Kind != engine.SemanticValueInteger || fields["order"].Integer != 12 {
			t.Fatalf("canonical integer string was not mapped structurally: %+v", fields["order"])
		}
	})
}

func TestMapSemanticEditPlanResultRoundTripsGeneratedContract(t *testing.T) {
	t.Parallel()
	input := engine.CompileInput{
		Mode:              engine.CompileProject,
		EntryPath:         "document.ldl",
		ProjectSourceTree: map[string][]byte{"document.ldl": []byte("project p \"P\" {}\nlayers {\n  app \"Application\" @10\n}\n")},
		ResolvedDependencies: engine.ResolvedDependencies{
			Format:        "layerdraw-resolved",
			FormatVersion: 1,
			Language:      1,
		},
	}
	planner := engine.New(engine.BuildInfo{})
	compiled, err := planner.Compile(context.Background(), input)
	if err != nil || len(compiled.Diagnostics) != 0 {
		t.Fatalf("compile fixture: err=%v diagnostics=%+v", err, compiled.Diagnostics)
	}
	snapshot := compiled.Snapshot()
	operation := engine.SemanticOperation{Kind: engine.OperationCreateSubject, ParentAddress: "ldl:project:p", SubjectKind: engine.SemanticSubjectKind("layer"), ID: "extra", Fields: []engine.SemanticMapEntry{{Key: "display_name", Value: engine.SemanticValue{Kind: engine.SemanticValueString, String: "Extra"}}, {Key: "order", Value: engine.SemanticValue{Kind: engine.SemanticValueInteger, Integer: 20}}}}
	plan, err := planner.PlanSemanticEdits(context.Background(), engine.SemanticEditPlanInput{BaseInput: input, BaseSnapshot: snapshot, Batch: engine.SemanticOperationBatch{Operations: []engine.SemanticOperation{operation}}, Preconditions: endpointSemanticPreconditions(snapshot)})
	if err != nil || plan.Status != "valid" {
		t.Fatalf("plan fixture: status=%s err=%v diagnostics=%+v", plan.Status, err, plan.Diagnostics)
	}
	base := engineprotocol.DocumentGeneration{DocumentHandle: engineprotocol.DocumentHandle{EndpointInstanceID: protocolcommon.EndpointInstanceID("fixture-endpoint"), Value: "document_abcdefghijklmnop"}, Value: protocolcommon.CanonicalUint64("7")}
	proposed := base
	proposed.Value = protocolcommon.CanonicalUint64("8")
	identity := SemanticPreviewIdentity{BaseGeneration: base, ProposedGeneration: proposed, PreviewID: engineprotocol.PreviewID{EndpointInstanceID: protocolcommon.EndpointInstanceID("fixture-endpoint"), Value: "preview_abcdefghijklmnop"}}
	generated, blobs, err := MapSemanticEditPlanResult(plan, identity, engine.SemanticPlanLimits{MaxItems: 10_000, MaxOutputBytes: 1 << 20})
	if err != nil {
		t.Fatal(err)
	}
	encoded, err := engineprotocol.EncodeWorkbenchPreviewResult(generated)
	if err != nil {
		t.Fatal(err)
	}
	decoded, err := engineprotocol.DecodeWorkbenchPreviewResult(encoded)
	if err != nil {
		t.Fatal(err)
	}
	if decoded.AuthoringImpact == nil || decoded.ResultingHashes == nil || len(decoded.SourceDiff.Edits) == 0 || len(decoded.SemanticDiff.Entries) == 0 {
		t.Fatalf("generated preview result is incomplete: %+v", decoded)
	}
	if len(blobs) == 0 || decoded.SourceDiff.Edits[0].ReplacementBlob == nil {
		t.Fatalf("replacement attachment was not mapped: edits=%+v blobs=%+v", decoded.SourceDiff.Edits, blobs)
	}
	if !reflect.DeepEqual(blobs[0].Bytes, plan.SourceDiff.Edits[0].ReplacementBlob.Bytes) {
		t.Fatal("generated result mapper changed replacement bytes")
	}
}

func endpointSemanticPreconditions(snapshot engine.Snapshot) engine.SemanticEditPreconditions {
	preconditions := engine.SemanticEditPreconditions{}
	for _, subject := range snapshot.SubjectSemanticHashes {
		preconditions.ExpectedSubjectHashes = append(preconditions.ExpectedSubjectHashes, engine.ExpectedSemanticHash{Address: subject.Address, Hash: subject.Hash})
	}
	for _, subtree := range snapshot.SubtreeHashes {
		preconditions.ExpectedSubtreeHashes = append(preconditions.ExpectedSubtreeHashes, engine.ExpectedSemanticHash{Address: subtree.OwnerAddress, Hash: subtree.Hash})
	}
	for _, childSet := range snapshot.ChildSetHashes {
		preconditions.ExpectedChildSets = append(preconditions.ExpectedChildSets, engine.ExpectedSemanticChildSet{OwnerAddress: childSet.OwnerAddress, ChildKind: childSet.ChildKind, Hash: childSet.Hash})
	}
	for _, file := range snapshot.SourceMap.Files {
		if file.Origin.Kind == "project" {
			preconditions.ExpectedSourceDigests = append(preconditions.ExpectedSourceDigests, engine.ExpectedSemanticSourceDigest{ModulePath: file.ModulePath, Digest: file.Digest})
		}
	}
	return preconditions
}
