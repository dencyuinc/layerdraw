// SPDX-License-Identifier: LicenseRef-LayerDraw-1.0

package engine

import "context"

// MaterializeDocumentView resolves a compiled View recipe from one retained
// Working Document generation and converts a caller-supplied QueryResult into
// deterministic semantic ViewData.
func (e Engine) MaterializeDocumentView(ctx context.Context, input MaterializeDocumentViewInput) (MaterializeDocumentViewResult, error) {
	document, snapshot, err := e.acquireSnapshot(ctx, input.DocumentGeneration)
	if err != nil {
		return MaterializeDocumentViewResult{}, err
	}
	if err := validateReadLimits(input.Limits, document.limits); err != nil {
		return MaterializeDocumentViewResult{}, err
	}
	if input.ViewAddress == "" {
		return MaterializeDocumentViewResult{}, &WorkbenchError{Code: "engine.workbench.invalid_view_address", Category: WorkbenchErrorInputInvalid}
	}
	if snapshot.compiled.TypedAST.Graph == nil {
		return MaterializeDocumentViewResult{}, &WorkbenchError{Code: "engine.workbench.graph_unavailable", Category: WorkbenchErrorOperationDisabled}
	}
	var recipe CompiledViewRecipe
	found := false
	for _, candidate := range snapshot.compiled.TypedAST.Views {
		if candidate.Address == input.ViewAddress {
			recipe = candidate
			found = true
			break
		}
	}
	if !found {
		return MaterializeDocumentViewResult{}, &WorkbenchError{Code: "engine.workbench.view_not_found", Category: WorkbenchErrorNotFound}
	}
	response := e.MaterializeView(ctx, ViewMaterializationInput{
		Recipe: recipe, Graph: *snapshot.compiled.TypedAST.Graph, QueryResult: input.QueryResult,
	})
	if response.Status == "rejected" {
		return MaterializeDocumentViewResult{}, &ViewMaterializationRejection{Diagnostics: response.Diagnostics}
	}
	if response.Status != "ok" || response.Result == nil {
		return MaterializeDocumentViewResult{}, &WorkbenchError{Code: "engine.workbench.view_materialization_invariant", Category: WorkbenchErrorInvariant}
	}
	return MaterializeDocumentViewResult{
		DocumentGeneration: input.DocumentGeneration,
		ViewData:           *response.Result,
	}, nil
}

type ViewMaterializationRejection struct {
	Diagnostics []Diagnostic
}

func (e *ViewMaterializationRejection) Error() string {
	return "engine.workbench.view_materialization_rejected"
}
