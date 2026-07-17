// SPDX-License-Identifier: LicenseRef-LayerDraw-1.0

package engine

import "context"

// ExecuteDocumentQuery evaluates one compiled Query against a retained Working
// Document generation. The Working Document supplies the authoritative compiled
// recipe and typed graph; callers supply only the query address and arguments.
func (e Engine) ExecuteDocumentQuery(ctx context.Context, input ExecuteDocumentQueryInput) (ExecuteDocumentQueryResult, error) {
	document, snapshot, err := e.acquireSnapshot(ctx, input.DocumentGeneration)
	if err != nil {
		return ExecuteDocumentQueryResult{}, err
	}
	if err := validateReadLimits(input.Limits, document.limits); err != nil {
		return ExecuteDocumentQueryResult{}, err
	}
	if input.QueryAddress == "" {
		return ExecuteDocumentQueryResult{}, &WorkbenchError{Code: "engine.workbench.invalid_query_address", Category: WorkbenchErrorInputInvalid}
	}
	if snapshot.compiled.TypedAST.Graph == nil {
		return ExecuteDocumentQueryResult{}, &WorkbenchError{Code: "engine.workbench.graph_unavailable", Category: WorkbenchErrorOperationDisabled}
	}
	var recipe CompiledQueryRecipe
	found := false
	for _, candidate := range snapshot.compiled.TypedAST.Queries {
		if candidate.Address == input.QueryAddress {
			recipe = candidate
			found = true
			break
		}
	}
	if !found {
		return ExecuteDocumentQueryResult{}, &WorkbenchError{Code: "engine.workbench.query_not_found", Category: WorkbenchErrorNotFound}
	}
	response := e.ExecuteQuery(ctx, QueryExecutionInput{Recipe: recipe, Graph: *snapshot.compiled.TypedAST.Graph, Arguments: input.Arguments})
	if response.Status == "rejected" {
		return ExecuteDocumentQueryResult{}, &QueryExecutionRejection{Diagnostics: response.Diagnostics}
	}
	if response.Result == nil {
		return ExecuteDocumentQueryResult{}, &WorkbenchError{Code: "engine.workbench.query_execution_invariant", Category: WorkbenchErrorInvariant}
	}
	result := ExecuteDocumentQueryResult{DocumentGeneration: input.DocumentGeneration, Result: *response.Result}
	result.ReturnedItems = queryResultItemCount(result.Result)
	if result.ReturnedItems > input.Limits.MaxItems {
		return ExecuteDocumentQueryResult{}, workbenchLimit("query_result_items", input.Limits.MaxItems, result.ReturnedItems)
	}
	returnedBytes, err := measureExecuteDocumentQueryResult(result)
	if err != nil {
		return ExecuteDocumentQueryResult{}, err
	}
	result.ReturnedBytes = returnedBytes
	if returnedBytes > input.Limits.MaxOutputBytes {
		return ExecuteDocumentQueryResult{}, workbenchLimit("max_output_bytes", input.Limits.MaxOutputBytes, returnedBytes)
	}
	return result, nil
}

type QueryExecutionRejection struct {
	Diagnostics []Diagnostic
}

func (e *QueryExecutionRejection) Error() string {
	return "engine.workbench.query_rejected"
}

func measureExecuteDocumentQueryResult(result ExecuteDocumentQueryResult) (int64, error) {
	for range 4 {
		measured, err := measureLogicalResult(result, 0)
		if err != nil {
			return 0, err
		}
		if measured == result.ReturnedBytes {
			return measured, nil
		}
		result.ReturnedBytes = measured
	}
	return measureLogicalResult(result, 0)
}

func queryResultItemCount(result QueryResult) int64 {
	return int64(len(result.SeedEntityAddresses) + len(result.ReachedEntityAddresses) + len(result.TraversedEntityAddresses) +
		len(result.PathRelationAddresses) + len(result.InducedRelationAddresses) + len(result.PrimaryEntityAddresses) +
		len(result.SelectedRelationAddresses) + len(result.SupportEntityAddresses) + len(result.Paths) + len(result.CycleRefs) +
		len(result.StateReads) + len(result.Diagnostics))
}
