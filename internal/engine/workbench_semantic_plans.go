// SPDX-License-Identifier: LicenseRef-LayerDraw-1.0

package engine

import (
	"context"
	"strconv"
)

// WorkbenchSemanticPlan binds a pure semantic edit plan to the authoritative
// Working Document input from which it was derived. Candidate is populated
// only for a valid plan and remains private until ApplyToHandle publishes it.
type WorkbenchSemanticPlan struct {
	Plan       SemanticEditPlan
	Candidate  CompileInput
	Generation DocumentGeneration
}

// PlanWorkbenchSemanticEdits evaluates semantic operations against one exact
// retained generation. Caller-supplied BaseInput and BaseSnapshot are ignored:
// the Working Document is the sole authority for source and semantic state.
func (e Engine) PlanWorkbenchSemanticEdits(ctx context.Context, input SemanticEditPlanInput) (WorkbenchSemanticPlan, error) {
	generation := DocumentGeneration{
		DocumentHandle: DocumentHandle{
			EndpointInstanceID: input.Generation.EndpointInstanceID,
			Value:              input.Generation.DocumentHandle,
		},
	}
	value, err := strconv.ParseUint(input.Generation.Value, 10, 64)
	if err != nil {
		return WorkbenchSemanticPlan{}, &WorkbenchError{Code: "engine.workbench.invalid_generation", Category: WorkbenchErrorInputInvalid, cause: err}
	}
	generation.Value = value
	document, snapshot, err := e.acquireSnapshot(ctx, generation)
	if err != nil {
		return WorkbenchSemanticPlan{}, err
	}
	if !snapshot.capabilities.PreviewOperations {
		return WorkbenchSemanticPlan{}, operationDisabled("preview_operations")
	}
	input.BaseInput = cloneWorkbenchCompileInput(snapshot.input)
	input.BaseSnapshot = snapshot.compiled
	input.Generation = SemanticDocumentGeneration{
		EndpointInstanceID: document.handle.EndpointInstanceID,
		DocumentHandle:     document.handle.Value,
		Value:              input.Generation.Value,
	}
	plan, err := e.PlanSemanticEdits(ctx, input)
	if err != nil {
		return WorkbenchSemanticPlan{}, err
	}
	result := WorkbenchSemanticPlan{Plan: plan, Generation: generation}
	if plan.Status == "valid" {
		result.Candidate = cloneWorkbenchCompileInput(snapshot.input)
		result.Candidate.ProjectSourceTree = cloneSourceTree(plan.SourceTree)
	}
	return result, nil
}

// RetainWorkbenchSemanticPreview installs the generated-contract projection of
// a semantic plan as the sole apply token for its authoritative generation.
func (e Engine) RetainWorkbenchSemanticPreview(ctx context.Context, generation DocumentGeneration, plan SourcePlannerPlan) error {
	document, _, err := e.acquireSnapshot(ctx, generation)
	if err != nil {
		return err
	}
	return e.retainPreview(ctx, document, plan)
}
