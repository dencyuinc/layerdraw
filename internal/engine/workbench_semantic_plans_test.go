// SPDX-License-Identifier: LicenseRef-LayerDraw-1.0

package engine

import (
	"bytes"
	"context"
	"testing"
)

func semanticWorkbenchInput(opened OpenDocumentResult, operation SemanticOperation) SemanticEditPlanInput {
	return SemanticEditPlanInput{
		Batch: SemanticOperationBatch{Operations: []SemanticOperation{operation}},
		Generation: SemanticDocumentGeneration{
			EndpointInstanceID: opened.DocumentGeneration.DocumentHandle.EndpointInstanceID,
			DocumentHandle:     opened.DocumentGeneration.DocumentHandle.Value,
			Value:              "1",
		},
		Limits: SemanticPlanLimits{MaxItems: 1_000, MaxOutputBytes: 1 << 20},
	}
}

func TestPlanWorkbenchSemanticEditsUsesRetainedGenerationAndKeepsCandidatePrivate(t *testing.T) {
	instance := New(BuildInfo{Workbench: WorkbenchConfig{EndpointInstanceID: "semantic-workbench"}})
	retained := projectCompileInput(allDeclarationsFixture)
	opened := openWorkbench(t, instance, retained)
	input := semanticWorkbenchInput(opened, SemanticOperation{
		Kind: OperationRenameSubject, TargetAddress: workbenchAlpha, NewID: "alpha_renamed",
	})
	compiled, err := instance.Compile(context.Background(), retained)
	if err != nil {
		t.Fatal(err)
	}
	snapshot := compiled.Snapshot()
	input.Preconditions = allSemanticPreconditions(snapshot)
	// These caller values are deliberately untrusted. The retained Working
	// Document must remain the only planning authority.
	input.BaseInput = projectCompileInput("project forged \"Forged\" {}")
	input.BaseSnapshot = Snapshot{}

	planned, err := instance.PlanWorkbenchSemanticEdits(context.Background(), input)
	if err != nil || planned.Plan.Status != "valid" {
		t.Fatalf("PlanWorkbenchSemanticEdits() = %+v, %v", planned.Plan, err)
	}
	if planned.Generation != opened.DocumentGeneration {
		t.Fatalf("generation = %+v, want %+v", planned.Generation, opened.DocumentGeneration)
	}
	if !bytes.Contains(planned.Candidate.ProjectSourceTree["document.ldl"], []byte("alpha_renamed")) || bytes.Contains(planned.Candidate.ProjectSourceTree["document.ldl"], []byte("Forged")) {
		t.Fatalf("candidate did not come from retained input: %q", planned.Candidate.ProjectSourceTree["document.ldl"])
	}

	conflicted := semanticWorkbenchInput(opened, SemanticOperation{
		Kind: OperationRenameSubject, TargetAddress: "ldl:project:p:entity:missing", NewID: "next",
	})
	conflicted.Preconditions = allSemanticPreconditions(snapshot)
	conflictPlan, err := instance.PlanWorkbenchSemanticEdits(context.Background(), conflicted)
	if err != nil || conflictPlan.Plan.Status == "valid" || len(conflictPlan.Candidate.ProjectSourceTree) != 0 {
		t.Fatalf("conflicting plan leaked candidate: %+v, %v", conflictPlan, err)
	}
}

func TestPlanWorkbenchSemanticEditsRejectsInvalidStaleAndUnavailableGenerations(t *testing.T) {
	instance := New(BuildInfo{Workbench: WorkbenchConfig{EndpointInstanceID: "semantic-workbench-errors"}})
	opened := openWorkbench(t, instance, projectCompileInput(allDeclarationsFixture))
	input := semanticWorkbenchInput(opened, SemanticOperation{Kind: OperationRenameSubject, TargetAddress: workbenchAlpha, NewID: "next"})

	input.Generation.Value = "not-a-generation"
	if _, err := instance.PlanWorkbenchSemanticEdits(context.Background(), input); !IsWorkbenchError(err, WorkbenchErrorInputInvalid) {
		t.Fatalf("invalid generation error = %v", err)
	}
	input.Generation.Value = "2"
	if _, err := instance.PlanWorkbenchSemanticEdits(context.Background(), input); !IsWorkbenchError(err, WorkbenchErrorGenerationStale) {
		t.Fatalf("stale generation error = %v", err)
	}

	unavailable := New(BuildInfo{Workbench: WorkbenchConfig{EndpointInstanceID: "semantic-workbench-unavailable"}})
	bad := openWorkbench(t, unavailable, projectCompileInput("project broken"))
	badInput := semanticWorkbenchInput(bad, SemanticOperation{Kind: OperationRenameSubject, TargetAddress: workbenchAlpha, NewID: "next"})
	badInput.Generation.EndpointInstanceID = bad.DocumentGeneration.DocumentHandle.EndpointInstanceID
	badInput.Generation.DocumentHandle = bad.DocumentGeneration.DocumentHandle.Value
	if _, err := unavailable.PlanWorkbenchSemanticEdits(context.Background(), badInput); !IsWorkbenchError(err, WorkbenchErrorOperationDisabled) {
		t.Fatalf("unavailable preview error = %v", err)
	}
}

func TestRetainWorkbenchSemanticPreviewBindsApplyToExactGeneration(t *testing.T) {
	instance := New(BuildInfo{Workbench: WorkbenchConfig{EndpointInstanceID: "semantic-retain"}})
	source := []byte("project p \"Project\" {}\n// keep\n")
	opened := openWorkbench(t, instance, projectCompileInput(string(source)))
	plan := previewKeepPatchForTest(t, instance, opened.DocumentGeneration, source, "kept")

	if err := instance.RetainWorkbenchSemanticPreview(context.Background(), opened.DocumentGeneration, plan); err != nil {
		t.Fatalf("RetainWorkbenchSemanticPreview() error = %v", err)
	}
	applied, err := instance.ApplyToHandle(context.Background(), ApplyToHandleInput{
		BaseGeneration: opened.DocumentGeneration,
		PreviewDigest:  SourcePlannerDigest(*plan.Preview.PreviewDigest),
		PreviewID:      *plan.Preview.PreviewID,
	})
	if err != nil || applied.DocumentGeneration.Value != 2 {
		t.Fatalf("ApplyToHandle() = %+v, %v", applied, err)
	}
	if err := instance.RetainWorkbenchSemanticPreview(context.Background(), opened.DocumentGeneration, plan); !IsWorkbenchError(err, WorkbenchErrorGenerationStale) {
		t.Fatalf("stale retain error = %v", err)
	}
}
