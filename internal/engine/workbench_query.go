// SPDX-License-Identifier: LicenseRef-LayerDraw-1.0

package engine

import (
	"context"
	"errors"
)

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
	response, err := e.ExecuteQuery(ctx, QueryExecutionInput{
		Recipe: recipe, Graph: *snapshot.compiled.TypedAST.Graph, Arguments: input.Arguments,
		Limits: QueryExecutionLimits{MaxItems: input.Limits.MaxItems}, StateSnapshot: input.StateSnapshot,
		Definition: snapshot.compiled.QueryDefinitionIdentity(),
	})
	if err != nil {
		return ExecuteDocumentQueryResult{}, mapQueryExecutionWorkbenchError(err)
	}
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
	returnedBytes, err := MeasureDocumentQueryLogicalBytes(ctx, result, input.Limits.MaxOutputBytes)
	if err != nil {
		return ExecuteDocumentQueryResult{}, err
	}
	result.ReturnedBytes = returnedBytes
	return result, nil
}

func mapQueryExecutionWorkbenchError(err error) error {
	var executionError *QueryExecutionError
	if !errors.As(err, &executionError) {
		return &WorkbenchError{Code: "engine.workbench.query_execution_invariant", Category: WorkbenchErrorInvariant, cause: err}
	}
	switch executionError.Category {
	case QueryExecutionErrorCancelled:
		return &WorkbenchError{Code: "engine.workbench.cancelled", Category: WorkbenchErrorCancelled, cause: err}
	case QueryExecutionErrorResource:
		return &WorkbenchError{Code: "engine.workbench.limit_exceeded", Category: WorkbenchErrorLimitExceeded, Resource: executionError.Resource, Limit: executionError.Limit, Observed: executionError.Observed, cause: err}
	default:
		return &WorkbenchError{Code: "engine.workbench.query_execution_invariant", Category: WorkbenchErrorInvariant, cause: err}
	}
}

type QueryExecutionRejection struct {
	Diagnostics []Diagnostic
}

func (e *QueryExecutionRejection) Error() string {
	return "engine.workbench.query_rejected"
}

func queryResultItemCount(result QueryResult) int64 {
	return int64(len(result.SeedEntityAddresses) + len(result.ReachedEntityAddresses) + len(result.TraversedEntityAddresses) +
		len(result.PathRelationAddresses) + len(result.InducedRelationAddresses) + len(result.PrimaryEntityAddresses) +
		len(result.SelectedRelationAddresses) + len(result.SupportEntityAddresses) + len(result.Paths) + len(result.CycleRefs) +
		len(result.StateReads) + len(result.Diagnostics))
}
