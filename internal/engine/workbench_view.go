// SPDX-License-Identifier: LicenseRef-LayerDraw-1.0

package engine

import (
	"context"
	"fmt"
)

// MaterializeDocumentView resolves one query-backed or diff-backed input from
// retained Working Document generations and delegates semantic conversion to
// MaterializeView.
func (e Engine) MaterializeDocumentView(ctx context.Context, input MaterializeDocumentViewInput) (MaterializeDocumentViewResult, error) {
	if input.ViewAddress == "" {
		return MaterializeDocumentViewResult{}, &WorkbenchError{Code: "engine.workbench.invalid_view_address", Category: WorkbenchErrorInputInvalid}
	}
	if (input.Query == nil) == (input.Diff == nil) {
		return MaterializeDocumentViewResult{}, &WorkbenchError{Code: "engine.workbench.invalid_view_source", Category: WorkbenchErrorInputInvalid}
	}
	if input.Query != nil {
		return e.materializeDocumentQueryView(ctx, input, *input.Query)
	}
	return e.materializeDocumentDiffView(ctx, input, *input.Diff)
}

func (e Engine) materializeDocumentQueryView(ctx context.Context, input MaterializeDocumentViewInput, queryInput MaterializeDocumentQueryViewInput) (MaterializeDocumentViewResult, error) {
	snapshot, err := e.materializeDocumentSnapshot(ctx, queryInput.DocumentGeneration, input.Limits)
	if err != nil {
		return MaterializeDocumentViewResult{}, err
	}
	recipe, err := materializeDocumentViewRecipe(snapshot, input.ViewAddress)
	if err != nil {
		return MaterializeDocumentViewResult{}, err
	}
	response := e.MaterializeView(ctx, ViewMaterializationInput{
		Recipe: recipe,
		Query: &QueryViewMaterializationInput{
			RevisionID:    workbenchRevisionID(queryInput.DocumentGeneration),
			Snapshot:      snapshot.compiled,
			QueryResult:   queryInput.QueryResult,
			StateSnapshot: queryInput.StateSnapshot,
		},
		Limits: ViewMaterializationLimits{MaxItems: input.Limits.MaxItems},
	})
	return materializeDocumentViewResult(queryInput.DocumentGeneration, response)
}

func (e Engine) materializeDocumentDiffView(ctx context.Context, input MaterializeDocumentViewInput, diffInput MaterializeDocumentDiffViewInput) (MaterializeDocumentViewResult, error) {
	recipeSnapshot, err := e.materializeDocumentSnapshot(ctx, diffInput.RecipeGeneration, input.Limits)
	if err != nil {
		return MaterializeDocumentViewResult{}, err
	}
	beforeSnapshot, err := e.materializeDocumentSnapshot(ctx, diffInput.BeforeGeneration, input.Limits)
	if err != nil {
		return MaterializeDocumentViewResult{}, err
	}
	afterSnapshot, err := e.materializeDocumentSnapshot(ctx, diffInput.AfterGeneration, input.Limits)
	if err != nil {
		return MaterializeDocumentViewResult{}, err
	}
	recipe, err := materializeDocumentViewRecipe(recipeSnapshot, input.ViewAddress)
	if err != nil {
		return MaterializeDocumentViewResult{}, err
	}
	response := e.MaterializeView(ctx, ViewMaterializationInput{
		Recipe: recipe,
		Diff: &DiffViewMaterializationInput{
			RecipeRevisionID:  workbenchRevisionID(diffInput.RecipeGeneration),
			RecipeSnapshot:    recipeSnapshot.compiled,
			BeforeRevisionID:  workbenchRevisionID(diffInput.BeforeGeneration),
			BeforeSnapshot:    beforeSnapshot.compiled,
			AfterRevisionID:   workbenchRevisionID(diffInput.AfterGeneration),
			AfterSnapshot:     afterSnapshot.compiled,
			BeforeQueryResult: diffInput.BeforeQueryResult,
			AfterQueryResult:  diffInput.AfterQueryResult,
		},
		Limits: ViewMaterializationLimits{MaxItems: input.Limits.MaxItems},
	})
	return materializeDocumentViewResult(diffInput.RecipeGeneration, response)
}

func (e Engine) materializeDocumentSnapshot(ctx context.Context, generation DocumentGeneration, limits WorkbenchLimits) (*workingSnapshot, error) {
	document, snapshot, err := e.acquireSnapshot(ctx, generation)
	if err != nil {
		return nil, err
	}
	if err := validateReadLimits(limits, document.limits); err != nil {
		return nil, err
	}
	if snapshot.compiled.TypedAST.Graph == nil {
		return nil, &WorkbenchError{Code: "engine.workbench.graph_unavailable", Category: WorkbenchErrorOperationDisabled}
	}
	return snapshot, nil
}

func materializeDocumentViewRecipe(snapshot *workingSnapshot, viewAddress string) (CompiledViewRecipe, error) {
	for _, candidate := range snapshot.compiled.TypedAST.Views {
		if candidate.Address == viewAddress {
			return candidate, nil
		}
	}
	return CompiledViewRecipe{}, &WorkbenchError{Code: "engine.workbench.view_not_found", Category: WorkbenchErrorNotFound}
}

func workbenchRevisionID(generation DocumentGeneration) string {
	return fmt.Sprintf("workbench:%s:%d", generation.DocumentHandle.Value, generation.Value)
}

func materializeDocumentViewResult(generation DocumentGeneration, response ViewMaterializationResponse) (MaterializeDocumentViewResult, error) {
	if response.Status == "rejected" {
		return MaterializeDocumentViewResult{}, &ViewMaterializationRejection{Diagnostics: response.Diagnostics}
	}
	if response.Status != "ok" || response.Result == nil {
		return MaterializeDocumentViewResult{}, &WorkbenchError{Code: "engine.workbench.view_materialization_invariant", Category: WorkbenchErrorInvariant}
	}
	return MaterializeDocumentViewResult{DocumentGeneration: generation, ViewData: *response.Result}, nil
}

type ViewMaterializationRejection struct {
	Diagnostics []Diagnostic
}

func (e *ViewMaterializationRejection) Error() string {
	return "engine.workbench.view_materialization_rejected"
}
