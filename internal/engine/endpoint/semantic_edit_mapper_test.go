// SPDX-License-Identifier: LicenseRef-LayerDraw-1.0

package endpoint

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"os"
	"reflect"
	"strings"
	"testing"

	"github.com/dencyuinc/layerdraw/gen/go/engineprotocol"
	"github.com/dencyuinc/layerdraw/gen/go/protocolcommon"
	semanticwire "github.com/dencyuinc/layerdraw/gen/go/semantic"
	"github.com/dencyuinc/layerdraw/internal/engine"
)

func TestGeneratedCreateUnionPlansAndAppliesThroughPublicResult(t *testing.T) {
	data, err := os.ReadFile("../../../schemas/fixtures/engine/workbench-create-subject-all-kinds.json")
	if err != nil {
		t.Fatal(err)
	}
	batch, err := engineprotocol.DecodeSemanticOperationBatch(data)
	if err != nil {
		t.Fatal(err)
	}
	batch.Operations[6].CreateSubjectOperation.CreateReferenceOperation.Fields.Text += "\n"
	batch.Operations[1].CreateSubjectOperation.CreateRelationTypeOperation.Fields.Cardinality.ToPerFrom.Min = 0
	relationFields := &batch.Operations[1].CreateSubjectOperation.CreateRelationTypeOperation.Fields
	tags := []string{"entity"}
	relationFields.Tags = &tags
	reverseLabel := "contained by"
	relationFields.ReverseLabel = &reverseLabel
	edgeLabel := "reverse_label"
	relationFields.Projections = &semanticwire.AuthoredRelationProjectionSet{Diagram: &semanticwire.AuthoredRelationDiagramProjection{EdgeLabel: &edgeLabel}}
	renderLabel := "reverse_label"
	relationFields.Render = &semanticwire.AuthoredRelationRenderSet{Edge: &semanticwire.AuthoredRelationRenderEdge{Label: &renderLabel}}
	participatesInImpact := true
	relationFields.Traversal = &semanticwire.AuthoredRelationTraversalPolicy{ParticipatesInImpact: &participatesInImpact}
	includeEndpoints := true
	relationFields.Export = &semanticwire.AuthoredRelationExport{IncludeEndpoints: &includeEndpoints}
	emptyPredicates := []semanticwire.RecipePredicate{}
	batch.Operations[4].CreateSubjectOperation.CreateQueryOperation.Fields.Where = &semanticwire.RecipePredicate{Kind: "all", Children: &emptyPredicates}
	assetBytes, err := base64.StdEncoding.DecodeString("iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAQAAAC1HAwCAAAAC0lEQVR42mNk+A8AAQUBAScY42YAAAAASUVORK5CYII=")
	if err != nil {
		t.Fatal(err)
	}
	assetSum := sha256.Sum256(assetBytes)
	assetDigest := "sha256:" + hex.EncodeToString(assetSum[:])
	batch.Operations[0].CreateSubjectOperation.CreateEntityTypeOperation.Fields.Image = &semanticwire.AuthoredAssetRef{Digest: protocolcommon.Digest(assetDigest), MediaType: "image/png"}
	viewFields := &batch.Operations[5].CreateSubjectOperation.CreateViewOperation.Fields
	viewFields.Shape.Kind = "table"
	rowSource := "entity_rows"
	viewFields.Shape.RowSource = &rowSource
	includeEntityID := true
	viewFields.Shape.IncludeEntityID = &includeEntityID
	sourceRefs := true
	batch.Operations[13].CreateSubjectOperation.CreateViewExportOperation.Fields.SourceRefs = &sourceRefs
	diagnostics := true
	stateSummary := false
	batch.Operations[13].CreateSubjectOperation.CreateViewExportOperation.Fields.Options = &semanticwire.ExportOptions{Kind: semanticwire.ExportFormat("json"), Diagnostics: &diagnostics, StateSummary: &stateSummary}

	input := engine.CompileInput{Mode: engine.CompileProject, EntryPath: "document.ldl", ProjectSourceTree: map[string][]byte{"document.ldl": []byte("project p \"P\" {}\n")}, ReferencedAssets: []engine.AssetInput{{Origin: engine.SourceOriginProject, Locator: "assets/generated.png", Bytes: assetBytes, Digest: assetDigest, MediaType: "image/png", ByteLength: int64(len(assetBytes))}}, ResolvedDependencies: engine.ResolvedDependencies{Format: "layerdraw-resolved", FormatVersion: 1, Language: 1}}
	planner := engine.New(engine.BuildInfo{})
	compiled, err := planner.Compile(context.Background(), input)
	if err != nil || len(compiled.Diagnostics) != 0 {
		t.Fatalf("compile base: err=%v diagnostics=%+v", err, compiled.Diagnostics)
	}
	base := compiled.Snapshot()
	mapped, err := MapSemanticEditPlanInput(input, base, emptyGeneratedSemanticPreconditions(), batch)
	if err != nil {
		t.Fatal(err)
	}
	mapped.Preconditions = endpointSemanticPreconditions(base)
	plan, err := planner.PlanSemanticEdits(context.Background(), mapped)
	if err != nil || plan.Status != "valid" {
		t.Fatalf("generated create union plan: status=%s err=%v conflicts=%+v diagnostics=%+v operations=%+v", plan.Status, err, plan.Conflicts, plan.Diagnostics, mapped.Batch.Operations)
	}
	for _, address := range []string{"entity-type:item", "relation-type:contains", "layer:default", "entity:one", "query:all", "view:map", "reference:note", "column:name", "constraint:unique_name", "parameter:needle", "table-column:name", "export:json"} {
		if !strings.Contains(string(plan.Result.CanonicalJSON), address) {
			t.Fatalf("result omitted generated subject %q", address)
		}
	}
	if !strings.Contains(string(plan.SourceTree["document.ldl"]), `image "assets/generated.png"`) {
		t.Fatalf("generated AuthoredAssetRef was not resolved through declared asset authority:\n%s", plan.SourceTree["document.ldl"])
	}
	if !strings.Contains(string(plan.Result.CanonicalJSON), assetDigest) {
		t.Fatalf("complete recompilation lost generated image digest %q", assetDigest)
	}
	for _, authored := range []string{"where all {", "projection diagram {", "render edge {", "traversal {", "export {", "rows entity_rows", "entity_id", "diagnostics", `tags ["entity"]`} {
		if !strings.Contains(string(plan.SourceTree["document.ldl"]), authored) {
			t.Fatalf("generated nested authored field %q was omitted:\n%s", authored, plan.SourceTree["document.ldl"])
		}
	}

	baseGeneration := engineprotocol.DocumentGeneration{DocumentHandle: engineprotocol.DocumentHandle{EndpointInstanceID: "fixture-endpoint", Value: "document_abcdefghijklmnop"}, Value: "1"}
	proposedGeneration := baseGeneration
	proposedGeneration.Value = "2"
	result, blobs, err := MapSemanticEditPlanResult(plan, SemanticPreviewIdentity{BaseGeneration: baseGeneration, ProposedGeneration: proposedGeneration, PreviewID: engineprotocol.PreviewID{EndpointInstanceID: "fixture-endpoint", Value: "preview_abcdefghijklmnop"}}, engine.SemanticPlanLimits{MaxItems: 100_000, MaxOutputBytes: 8 << 20})
	if err != nil {
		t.Fatal(err)
	}
	byID := map[string][]byte{}
	for _, blob := range blobs {
		byID[blob.Ref.BlobID] = blob.Bytes
	}
	publicTree := map[string][]byte{}
	for path, value := range input.ProjectSourceTree {
		publicTree[path] = bytes.Clone(value)
	}
	for _, edit := range result.SourceDiff.Edits {
		switch edit.Kind {
		case engineprotocol.SourceEditKindCreate:
			publicTree[edit.AfterModule.ModulePath] = bytes.Clone(byID[edit.ReplacementBlob.BlobID])
		case engineprotocol.SourceEditKindDelete:
			delete(publicTree, edit.BeforeModule.ModulePath)
		case engineprotocol.SourceEditKindMove:
			publicTree[edit.AfterModule.ModulePath] = publicTree[edit.BeforeModule.ModulePath]
			delete(publicTree, edit.BeforeModule.ModulePath)
		case engineprotocol.SourceEditKindReplace:
			// Frozen SourceEdit attachments are complete reconstructable after
			// modules; source_range identifies the localized authored change.
			publicTree[edit.SourceRange.ModulePath] = bytes.Clone(byID[edit.ReplacementBlob.BlobID])
		}
	}
	if !reflect.DeepEqual(publicTree, plan.SourceTree) {
		t.Fatalf("public SourceEdit application diverged from plan\n got: %q\nwant: %q", publicTree, plan.SourceTree)
	}
}

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
	mapped, err := MapSemanticEditPlanInput(engine.CompileInput{}, engine.Snapshot{}, emptyGeneratedSemanticPreconditions(), batch)
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
		mapped, err := MapSemanticEditPlanInput(engine.CompileInput{}, engine.Snapshot{}, emptyGeneratedSemanticPreconditions(), batch)
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
		mapped, err := MapSemanticEditPlanInput(engine.CompileInput{}, engine.Snapshot{}, emptyGeneratedSemanticPreconditions(), batch)
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

	t.Run("blob values retain the complete generated reference", func(t *testing.T) {
		batch, err := engineprotocol.DecodeSemanticOperationBatch([]byte(`{
  "operations": [{
    "operation": "update_subject_field",
    "target_address": "ldl:project:p:entity-type:item",
    "path": ["image"],
    "action": "set",
    "value": {
      "kind": "blob",
      "blob": {
        "blob_id": "image-asset",
        "digest": "sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
        "lifetime": "request",
        "media_type": "image/png",
        "size": "17"
      }
    }
  }]
}`))
		if err != nil {
			t.Fatal(err)
		}
		mapped, err := MapSemanticEditPlanInput(engine.CompileInput{}, engine.Snapshot{}, emptyGeneratedSemanticPreconditions(), batch)
		if err != nil {
			t.Fatal(err)
		}
		blob := mapped.Batch.Operations[0].Value.BlobRef
		if blob == nil || blob.BlobID != "image-asset" || blob.Digest != "sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa" || blob.Lifetime != "request" || blob.MediaType != "image/png" || blob.Size != 17 {
			t.Fatalf("mapped blob reference is incomplete: %+v", blob)
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

func TestMapSemanticEditPlanResultDeduplicatesModuleAttachmentsAndKeepsLeafImpacts(t *testing.T) {
	t.Parallel()
	const source = `project p "Project" {}
layers {
  app "Application" @10
}
entity_type service "Service" {
  representation shape rect
  columns {
    note "Note" string
  }
}
entities service @app {
  a "A"
}
rows service [note] {
  a primary: "old"
}
`
	input := engine.CompileInput{Mode: engine.CompileProject, EntryPath: "document.ldl", ProjectSourceTree: map[string][]byte{"document.ldl": []byte(source)}, ResolvedDependencies: engine.ResolvedDependencies{Format: "layerdraw-resolved", FormatVersion: 1, Language: 1}}
	planner := engine.New(engine.BuildInfo{})
	compiled, err := planner.Compile(context.Background(), input)
	if err != nil || len(compiled.Diagnostics) != 0 {
		t.Fatalf("compile public result fixture: err=%v diagnostics=%+v", err, compiled.Diagnostics)
	}
	base := compiled.Snapshot()
	annotation := engine.SemanticValue{Kind: engine.SemanticValueString, String: "core"}
	cell := engine.SemanticValue{Kind: engine.SemanticValueString, String: "new"}
	operations := []engine.SemanticOperation{
		{Kind: engine.OperationUpdateSubjectField, TargetAddress: "ldl:project:p:entity:a", Path: []string{"annotations", "team"}, Action: "set", Value: &annotation},
		{Kind: engine.OperationUpdateSubjectField, TargetAddress: "ldl:project:p:entity:a:row:primary", Path: []string{"values", "ldl:project:p:entity-type:service:column:note"}, Action: "set", Value: &cell},
	}
	plan, err := planner.PlanSemanticEdits(context.Background(), engine.SemanticEditPlanInput{BaseInput: input, BaseSnapshot: base, Batch: engine.SemanticOperationBatch{Operations: operations}, Preconditions: endpointSemanticPreconditions(base)})
	if err != nil || plan.Status != "valid" {
		t.Fatalf("plan public result fixture: status=%s err=%v conflicts=%+v diagnostics=%+v", plan.Status, err, plan.Conflicts, plan.Diagnostics)
	}
	identity := semanticPreviewIdentityFixture()
	result, blobs, err := MapSemanticEditPlanResult(plan, identity, engine.SemanticPlanLimits{MaxItems: 10_000, MaxOutputBytes: 1 << 20})
	if err != nil {
		t.Fatal(err)
	}
	replacementCount := 0
	sharedID := ""
	for _, edit := range result.SourceDiff.Edits {
		if edit.Kind != engineprotocol.SourceEditKind("replace") {
			continue
		}
		replacementCount++
		if edit.ReplacementBlob == nil {
			t.Fatal("replace edit omitted its public attachment reference")
		}
		if sharedID == "" {
			sharedID = edit.ReplacementBlob.BlobID
		} else if edit.ReplacementBlob.BlobID != sharedID {
			t.Fatalf("same after-module bytes used distinct blob IDs: %q and %q", sharedID, edit.ReplacementBlob.BlobID)
		}
	}
	if replacementCount < 2 || len(blobs) != 1 {
		t.Fatalf("multi-hunk module did not share one output attachment: replacements=%d blobs=%d edits=%+v", replacementCount, len(blobs), result.SourceDiff.Edits)
	}
	if err := validateUniqueOutputBlobs(blobs); err != nil {
		t.Fatalf("public boundary rejected deduplicated attachments: %v", err)
	}
	if _, err := engineprotocol.EncodeWorkbenchPreviewResult(result); err != nil {
		t.Fatalf("public generated result rejected multi-hunk edit: %v", err)
	}
	wantPaths := map[string]bool{
		"annotations/team": false,
		"values/ldl:project:p:entity-type:service:column:note": false,
	}
	wantImpactPaths := map[string]bool{
		"annotations/team": false,
		"values/ldl:project:p:entity-type:service:column:note": false,
	}
	for _, entry := range result.SemanticDiff.Entries {
		for _, path := range entry.ChangedFieldPaths {
			joined := strings.Join(path.Tokens, "/")
			if joined == "annotations" || joined == "values" {
				t.Fatalf("semantic diff widened an exact registered leaf to %q", joined)
			}
			if _, exists := wantPaths[joined]; exists {
				wantPaths[joined] = true
			}
		}
	}
	for _, entry := range result.AuthoringImpact.Entries {
		for _, path := range entry.ChangedFieldPaths {
			joined := strings.Join(path.Tokens, "/")
			if joined == "annotations" || joined == "values" {
				t.Fatalf("AuthoringImpact widened an exact registered leaf to %q", joined)
			}
			if _, exists := wantImpactPaths[joined]; exists {
				wantImpactPaths[joined] = true
			}
		}
	}
	for path, found := range wantPaths {
		if !found {
			t.Fatalf("public semantic diff omitted exact authored path %q: %+v", path, result.SemanticDiff.Entries)
		}
	}
	for path, found := range wantImpactPaths {
		if !found {
			t.Fatalf("public AuthoringImpact omitted exact authorization path %q: %+v", path, result.AuthoringImpact.Entries)
		}
	}
	publicTree := map[string][]byte{"document.ldl": []byte(source)}
	for _, edit := range result.SourceDiff.Edits {
		if edit.Kind == engineprotocol.SourceEditKind("replace") {
			publicTree[edit.SourceRange.ModulePath] = bytes.Clone(blobs[0].Bytes)
		}
	}
	if !reflect.DeepEqual(publicTree, plan.SourceTree) {
		t.Fatalf("public shared attachment did not reconstruct the planned source tree\n got=%q\nwant=%q", publicTree, plan.SourceTree)
	}
}

func TestSemanticConflictOrderEncodesClassBeforeAddress(t *testing.T) {
	t.Parallel()
	const source = `project p "Project" {}
layers {
  app "Application" @10
}
entity_type alpha "Alpha" {
  representation shape rect
}
entities alpha @app {
  z "Z"
}
`
	input := engine.CompileInput{Mode: engine.CompileProject, EntryPath: "document.ldl", ProjectSourceTree: map[string][]byte{"document.ldl": []byte(source)}, ResolvedDependencies: engine.ResolvedDependencies{Format: "layerdraw-resolved", FormatVersion: 1, Language: 1}}
	planner := engine.New(engine.BuildInfo{})
	compiled, err := planner.Compile(context.Background(), input)
	if err != nil || len(compiled.Diagnostics) != 0 {
		t.Fatalf("compile conflict fixture: err=%v diagnostics=%+v", err, compiled.Diagnostics)
	}
	base := compiled.Snapshot()
	value := engine.SemanticValue{Kind: engine.SemanticValueString, String: "changed"}
	preconditions := endpointSemanticPreconditions(base)
	for index := range preconditions.ExpectedSubjectHashes {
		if preconditions.ExpectedSubjectHashes[index].Address == "ldl:project:p:entity:z" {
			preconditions.ExpectedSubjectHashes[index].Hash = "sha256:" + strings.Repeat("a", 64)
		}
	}
	for index := range preconditions.ExpectedSubtreeHashes {
		if preconditions.ExpectedSubtreeHashes[index].Address == "ldl:project:p:entity-type:alpha" {
			preconditions.ExpectedSubtreeHashes[index].Hash = "sha256:" + strings.Repeat("b", 64)
		}
	}
	operation := engine.SemanticOperation{Kind: engine.OperationUpdateSubjectField, TargetAddress: "ldl:project:p:entity:z", Path: []string{"description"}, Action: "set", Value: &value}
	plan, err := planner.PlanSemanticEdits(context.Background(), engine.SemanticEditPlanInput{BaseInput: input, BaseSnapshot: base, Batch: engine.SemanticOperationBatch{Operations: []engine.SemanticOperation{operation}}, Preconditions: preconditions})
	if err != nil || plan.Status != "invalid" || len(plan.Conflicts) != 2 {
		t.Fatalf("expected two ordered conflicts: plan=%+v err=%v", plan, err)
	}
	if plan.Conflicts[0].Kind != engine.ConflictSubjectChanged || plan.Conflicts[1].Kind != engine.ConflictSubtreeChanged {
		t.Fatalf("planner did not use normative class-first order: %+v", plan.Conflicts)
	}
	result, _, err := MapSemanticEditPlanResult(plan, semanticPreviewIdentityFixture(), engine.SemanticPlanLimits{MaxItems: 100, MaxOutputBytes: 1 << 20})
	if err != nil {
		t.Fatalf("map class-first conflicts: %v", err)
	}
	encoded, err := engineprotocol.EncodeWorkbenchPreviewResult(result)
	if err != nil {
		t.Fatalf("generated comparator contradicted planner order: %v", err)
	}
	decoded, err := engineprotocol.DecodeWorkbenchPreviewResult(encoded)
	if err != nil || decoded.Conflicts[0].Kind != string(engine.ConflictSubjectChanged) || decoded.Conflicts[1].Kind != string(engine.ConflictSubtreeChanged) {
		t.Fatalf("public conflict order changed: decoded=%+v err=%v", decoded.Conflicts, err)
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
		preconditions.ExpectedSourceDigests = append(preconditions.ExpectedSourceDigests, engine.ExpectedSemanticSourceDigest{Module: engine.PlannedModuleRef{OriginKind: engine.SourceOriginKind(file.Origin.Kind), PackAddress: file.Origin.PackAddress, ModulePath: file.ModulePath}, Digest: file.Digest})
	}
	return preconditions
}

func emptyGeneratedSemanticPreconditions() engineprotocol.EngineEditPreconditions {
	return engineprotocol.EngineEditPreconditions{
		DocumentGeneration: engineprotocol.DocumentGeneration{
			DocumentHandle: engineprotocol.DocumentHandle{EndpointInstanceID: protocolcommon.EndpointInstanceID("fixture-endpoint"), Value: "document_abcdefghijklmnop"},
			Value:          protocolcommon.CanonicalUint64("1"),
		},
		ExpectedSubjectHashes: []engineprotocol.ExpectedHash{},
		ExpectedSubtreeHashes: []engineprotocol.ExpectedHash{},
		ExpectedChildSets:     []engineprotocol.ExpectedChildSet{},
	}
}

func semanticPreviewIdentityFixture() SemanticPreviewIdentity {
	base := engineprotocol.DocumentGeneration{DocumentHandle: engineprotocol.DocumentHandle{EndpointInstanceID: protocolcommon.EndpointInstanceID("fixture-endpoint"), Value: "document_abcdefghijklmnop"}, Value: protocolcommon.CanonicalUint64("7")}
	proposed := base
	proposed.Value = protocolcommon.CanonicalUint64("8")
	return SemanticPreviewIdentity{BaseGeneration: base, ProposedGeneration: proposed, PreviewID: engineprotocol.PreviewID{EndpointInstanceID: protocolcommon.EndpointInstanceID("fixture-endpoint"), Value: "preview_abcdefghijklmnop"}}
}

func TestMapSemanticEditPlanInputValidatesPreconditionsOnEveryPublicPath(t *testing.T) {
	t.Parallel()
	batch, err := engineprotocol.DecodeSemanticOperationBatch([]byte(`{"operations":[{"operation":"create_subject","subject_kind":"layer","parent_address":"ldl:project:p","id":"extra","fields":{"display_name":"Extra","order":"10"}}]}`))
	if err != nil {
		t.Fatal(err)
	}
	digestA := protocolcommon.Digest("sha256:" + strings.Repeat("a", 64))
	digestB := protocolcommon.Digest("sha256:" + strings.Repeat("b", 64))
	valid := emptyGeneratedSemanticPreconditions()
	tests := []struct {
		name   string
		mutate func(engineprotocol.EngineEditPreconditions) engineprotocol.EngineEditPreconditions
	}{
		{name: "missing required collection", mutate: func(value engineprotocol.EngineEditPreconditions) engineprotocol.EngineEditPreconditions {
			value.ExpectedSubjectHashes = nil
			return value
		}},
		{name: "duplicate subject", mutate: func(value engineprotocol.EngineEditPreconditions) engineprotocol.EngineEditPreconditions {
			value.ExpectedSubjectHashes = []engineprotocol.ExpectedHash{{Address: "ldl:project:p:entity:a", Hash: digestA}, {Address: "ldl:project:p:entity:a", Hash: digestA}}
			return value
		}},
		{name: "noncanonical subject order", mutate: func(value engineprotocol.EngineEditPreconditions) engineprotocol.EngineEditPreconditions {
			value.ExpectedSubjectHashes = []engineprotocol.ExpectedHash{{Address: "ldl:project:p:entity:z", Hash: digestA}, {Address: "ldl:project:p:entity:a", Hash: digestB}}
			return value
		}},
		{name: "malformed hash", mutate: func(value engineprotocol.EngineEditPreconditions) engineprotocol.EngineEditPreconditions {
			value.ExpectedSubjectHashes = []engineprotocol.ExpectedHash{{Address: "ldl:project:p:entity:a", Hash: "sha256:bad"}}
			return value
		}},
		{name: "noncanonical generation", mutate: func(value engineprotocol.EngineEditPreconditions) engineprotocol.EngineEditPreconditions {
			value.DocumentGeneration.Value = protocolcommon.CanonicalUint64("01")
			return value
		}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if _, err := MapSemanticEditPlanInput(engine.CompileInput{}, engine.Snapshot{}, test.mutate(valid), batch); err == nil {
				t.Fatal("malformed generated preconditions crossed the public mapper")
			}
		})
	}
}

func TestMapSemanticEditPlanInputPreservesCompleteSourceModuleIdentity(t *testing.T) {
	t.Parallel()
	batch, err := engineprotocol.DecodeSemanticOperationBatch([]byte(`{"operations":[{"operation":"create_subject","subject_kind":"layer","parent_address":"ldl:project:p","id":"extra","fields":{"display_name":"Extra","order":"10"}}]}`))
	if err != nil {
		t.Fatal(err)
	}
	packAddress := semanticwire.PackRootAddress("ldl:pack:publisher:shared-pack")
	expected := []engineprotocol.ExpectedSourceDigest{
		{Module: semanticwire.ModuleRef{Origin: semanticwire.SourceOrigin{Kind: "project"}, ModulePath: "same.ldl"}, Digest: protocolcommon.Digest("sha256:" + strings.Repeat("a", 64))},
		{Module: semanticwire.ModuleRef{Origin: semanticwire.SourceOrigin{Kind: "pack", PackAddress: &packAddress}, ModulePath: "same.ldl"}, Digest: protocolcommon.Digest("sha256:" + strings.Repeat("b", 64))},
	}
	preconditions := emptyGeneratedSemanticPreconditions()
	preconditions.ExpectedSourceDigests = &expected
	mapped, err := MapSemanticEditPlanInput(engine.CompileInput{}, engine.Snapshot{}, preconditions, batch)
	if err != nil {
		t.Fatal(err)
	}
	if len(mapped.Preconditions.ExpectedSourceDigests) != 2 {
		t.Fatalf("source preconditions were dropped: %+v", mapped.Preconditions.ExpectedSourceDigests)
	}
	project, pack := mapped.Preconditions.ExpectedSourceDigests[0].Module, mapped.Preconditions.ExpectedSourceDigests[1].Module
	if project.OriginKind != engine.SourceOriginProject || project.PackAddress != "" || project.ModulePath != "same.ldl" {
		t.Fatalf("project ModuleRef changed: %+v", project)
	}
	if pack.OriginKind != engine.SourceOriginPack || pack.PackAddress != string(packAddress) || pack.ModulePath != "same.ldl" {
		t.Fatalf("pack ModuleRef changed: %+v", pack)
	}
}
