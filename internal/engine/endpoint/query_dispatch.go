// SPDX-License-Identifier: LicenseRef-LayerDraw-1.0

package endpoint

import (
	"context"
	"errors"
	"fmt"
	"strconv"

	"github.com/dencyuinc/layerdraw/gen/go/engineprotocol"
	"github.com/dencyuinc/layerdraw/gen/go/protocolcommon"
	"github.com/dencyuinc/layerdraw/gen/go/semantic"
	"github.com/dencyuinc/layerdraw/internal/engine"
	"github.com/dencyuinc/layerdraw/internal/engine/internal/compiler/materialize"
	"github.com/dencyuinc/layerdraw/internal/engine/internal/compiler/semantic/definition"
)

func runExecuteQuery(payload engineprotocol.ExecuteQueryInput) func(context.Context, workbenchDriver, map[string][]byte) (any, []OutputBlob, error) {
	return func(ctx context.Context, driver workbenchDriver, _ map[string][]byte) (any, []OutputBlob, error) {
		input, err := mapExecuteQueryInput(payload)
		if err != nil {
			return nil, nil, err
		}
		result, err := driver.ExecuteDocumentQuery(ctx, input)
		if err != nil {
			return nil, nil, err
		}
		mapped, err := mapExecuteQueryResult(ctx, result, input.Limits.MaxOutputBytes)
		if err != nil {
			return nil, nil, queryResultMappingError(err)
		}
		return mapped, nil, nil
	}
}

func queryResultMappingError(err error) error {
	if err == nil || errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
		return err
	}
	var workbenchError *engine.WorkbenchError
	if errors.As(err, &workbenchError) {
		return err
	}
	return &engine.WorkbenchError{
		Code:     "engine.workbench.query_result_invariant",
		Category: engine.WorkbenchErrorInvariant,
	}
}

func mapExecuteQueryInput(input engineprotocol.ExecuteQueryInput) (engine.ExecuteDocumentQueryInput, error) {
	result := engine.ExecuteDocumentQueryInput{
		Arguments:    make(map[string]engine.TypedScalar, len(input.Arguments)),
		QueryAddress: string(input.QueryAddress),
	}
	if err := convertStruct(input.DocumentGeneration, &result.DocumentGeneration); err != nil {
		return engine.ExecuteDocumentQueryInput{}, err
	}
	if err := convertStruct(input.Limits, &result.Limits); err != nil {
		return engine.ExecuteDocumentQueryInput{}, err
	}
	for address, value := range input.Arguments {
		scalar, err := engineScalarFromRecipeScalar(value)
		if err != nil {
			return engine.ExecuteDocumentQueryInput{}, err
		}
		result.Arguments[address] = scalar
	}
	return result, nil
}

func mapExecuteQueryResult(ctx context.Context, input engine.ExecuteDocumentQueryResult, maxOutputBytes int64) (engineprotocol.ExecuteQueryResult, error) {
	returnedBytes, err := engine.MeasureDocumentQueryLogicalBytes(ctx, input, maxOutputBytes)
	if err != nil {
		return engineprotocol.ExecuteQueryResult{}, err
	}
	if input.ReturnedBytes != returnedBytes {
		return engineprotocol.ExecuteQueryResult{}, &engine.WorkbenchError{Code: "engine.workbench.query_result_byte_invariant", Category: engine.WorkbenchErrorInvariant}
	}
	var generation engineprotocol.DocumentGeneration
	if err := convertStruct(input.DocumentGeneration, &generation); err != nil {
		return engineprotocol.ExecuteQueryResult{}, err
	}
	returnedItems, err := canonicalUint64FromInt64(input.ReturnedItems)
	if err != nil {
		return engineprotocol.ExecuteQueryResult{}, err
	}
	result, err := mapQueryExecutionResultData(ctx, input.Result)
	if err != nil {
		return engineprotocol.ExecuteQueryResult{}, err
	}
	return engineprotocol.ExecuteQueryResult{
		DocumentGeneration: generation,
		Result:             result,
		ReturnedBytes:      engineprotocol.LogicalResponseByteCount(strconv.FormatInt(returnedBytes, 10)),
		ReturnedItems:      protocolcommon.CanonicalUint64(returnedItems),
	}, nil
}

func mapQueryExecutionResultData(ctx context.Context, input engine.QueryResult) (engineprotocol.QueryExecutionResultData, error) {
	if err := queryMappingContext(ctx); err != nil {
		return engineprotocol.QueryExecutionResultData{}, err
	}
	arguments := make(map[string]semantic.RecipeScalar, len(input.Arguments))
	for address, value := range input.Arguments {
		if err := queryMappingContext(ctx); err != nil {
			return engineprotocol.QueryExecutionResultData{}, err
		}
		mapped, err := mapRecipeScalar(materialize.Scalar{Type: value.Type, String: value.String, Int: value.Int, Float: value.Float, Bool: value.Bool})
		if err != nil {
			return engineprotocol.QueryExecutionResultData{}, err
		}
		arguments[address] = mapped
	}
	diagnostics := make([]semantic.Diagnostic, 0, len(input.Diagnostics))
	for _, value := range input.Diagnostics {
		if err := queryMappingContext(ctx); err != nil {
			return engineprotocol.QueryExecutionResultData{}, err
		}
		mapped, err := mapDiagnostic(value)
		if err != nil {
			return engineprotocol.QueryExecutionResultData{}, err
		}
		diagnostics = append(diagnostics, mapped)
	}
	cycleRefs, err := mapQueryCycleRefs(ctx, input.CycleRefs)
	if err != nil {
		return engineprotocol.QueryExecutionResultData{}, err
	}
	induced, err := mapQueryStrings[semantic.RelationAddress](ctx, input.InducedRelationAddresses)
	if err != nil {
		return engineprotocol.QueryExecutionResultData{}, err
	}
	pathRelations, err := mapQueryStrings[semantic.RelationAddress](ctx, input.PathRelationAddresses)
	if err != nil {
		return engineprotocol.QueryExecutionResultData{}, err
	}
	paths, err := mapQueryPaths(ctx, input.Paths)
	if err != nil {
		return engineprotocol.QueryExecutionResultData{}, err
	}
	primary, err := mapQueryStrings[semantic.EntityAddress](ctx, input.PrimaryEntityAddresses)
	if err != nil {
		return engineprotocol.QueryExecutionResultData{}, err
	}
	reached, err := mapQueryStrings[semantic.EntityAddress](ctx, input.ReachedEntityAddresses)
	if err != nil {
		return engineprotocol.QueryExecutionResultData{}, err
	}
	seeds, err := mapQueryStrings[semantic.EntityAddress](ctx, input.SeedEntityAddresses)
	if err != nil {
		return engineprotocol.QueryExecutionResultData{}, err
	}
	selected, err := mapQueryStrings[semantic.RelationAddress](ctx, input.SelectedRelationAddresses)
	if err != nil {
		return engineprotocol.QueryExecutionResultData{}, err
	}
	stateReads, err := mapQueryStateReadRefs(ctx, input.StateReads)
	if err != nil {
		return engineprotocol.QueryExecutionResultData{}, err
	}
	support, err := mapQueryStrings[semantic.EntityAddress](ctx, input.SupportEntityAddresses)
	if err != nil {
		return engineprotocol.QueryExecutionResultData{}, err
	}
	traversed, err := mapQueryStrings[semantic.EntityAddress](ctx, input.TraversedEntityAddresses)
	if err != nil {
		return engineprotocol.QueryExecutionResultData{}, err
	}
	return engineprotocol.QueryExecutionResultData{
		Arguments:                 arguments,
		CycleRefs:                 cycleRefs,
		Diagnostics:               diagnostics,
		InducedRelationAddresses:  induced,
		PathRelationAddresses:     pathRelations,
		Paths:                     paths,
		PrimaryEntityAddresses:    primary,
		QueryAddress:              semantic.QueryAddress(input.QueryAddress),
		ReachedEntityAddresses:    reached,
		SeedEntityAddresses:       seeds,
		SelectedRelationAddresses: selected,
		StateInput:                engineprotocol.QueryStateInputRef{Kind: input.StateInput.Kind},
		StatePolicy:               input.StatePolicy,
		StateReads:                stateReads,
		SupportEntityAddresses:    support,
		TraversedEntityAddresses:  traversed,
	}, nil
}

func engineScalarFromRecipeScalar(input semantic.RecipeScalar) (engine.TypedScalar, error) {
	switch input.Kind {
	case "string", "enum", "date", "datetime":
		if input.StringValue == nil {
			return engine.TypedScalar{}, fmt.Errorf("missing string recipe scalar")
		}
		return engine.TypedScalar{Type: definition.ScalarType(input.Kind), String: *input.StringValue}, nil
	case "integer":
		if input.IntegerValue == nil {
			return engine.TypedScalar{}, fmt.Errorf("missing integer recipe scalar")
		}
		value, err := strconv.ParseInt(string(*input.IntegerValue), 10, 64)
		if err != nil {
			return engine.TypedScalar{}, err
		}
		return engine.TypedScalar{Type: definition.ScalarInteger, Int: value}, nil
	case "number":
		if input.NumberValue == nil {
			return engine.TypedScalar{}, fmt.Errorf("missing number recipe scalar")
		}
		value, err := strconv.ParseFloat(string(*input.NumberValue), 64)
		if err != nil {
			return engine.TypedScalar{}, err
		}
		return engine.TypedScalar{Type: definition.ScalarNumber, Float: value}, nil
	case "boolean":
		if input.BooleanValue == nil {
			return engine.TypedScalar{}, fmt.Errorf("missing boolean recipe scalar")
		}
		return engine.TypedScalar{Type: definition.ScalarBoolean, Bool: *input.BooleanValue}, nil
	default:
		return engine.TypedScalar{}, fmt.Errorf("unsupported recipe scalar kind %q", input.Kind)
	}
}

func mapQueryStateReadRefs(ctx context.Context, input []engine.StateReadRef) ([]engineprotocol.QueryStateReadRef, error) {
	result := make([]engineprotocol.QueryStateReadRef, len(input))
	for index, item := range input {
		if err := queryMappingContext(ctx); err != nil {
			return nil, err
		}
		result[index] = engineprotocol.QueryStateReadRef{
			FieldPath:      semantic.StateFieldPath(item.FieldPath),
			SubjectAddress: semantic.StableAddress(item.SubjectAddress),
		}
	}
	return result, nil
}

func mapQueryPaths(ctx context.Context, input []engine.QueryPath) ([]engineprotocol.QueryPath, error) {
	result := make([]engineprotocol.QueryPath, len(input))
	for index, item := range input {
		mapped, err := mapQueryPath(ctx, item)
		if err != nil {
			return nil, err
		}
		result[index] = mapped
	}
	return result, nil
}

func mapQueryCycleRefs(ctx context.Context, input []engine.QueryCycleRef) ([]engineprotocol.QueryCycleRef, error) {
	result := make([]engineprotocol.QueryCycleRef, len(input))
	for index, item := range input {
		if err := queryMappingContext(ctx); err != nil {
			return nil, err
		}
		path, err := mapQueryPath(ctx, item.RetainedPath)
		if err != nil {
			return nil, err
		}
		result[index] = engineprotocol.QueryCycleRef{
			FromEntityAddress: semantic.EntityAddress(item.FromEntityAddress),
			Kind:              item.Kind,
			Orientation:       item.Orientation,
			RelationAddress:   semantic.RelationAddress(item.RelationAddress),
			RetainedPath:      path,
			ToEntityAddress:   semantic.EntityAddress(item.ToEntityAddress),
		}
	}
	return result, nil
}

func mapQueryPath(ctx context.Context, input engine.QueryPath) (engineprotocol.QueryPath, error) {
	entities, err := mapQueryStrings[semantic.EntityAddress](ctx, input.EntityAddresses)
	if err != nil {
		return engineprotocol.QueryPath{}, err
	}
	relations, err := mapQueryStrings[semantic.RelationAddress](ctx, input.RelationAddresses)
	if err != nil {
		return engineprotocol.QueryPath{}, err
	}
	return engineprotocol.QueryPath{EntityAddresses: entities, RelationAddresses: relations}, nil
}

func mapQueryStrings[T ~string](ctx context.Context, input []string) ([]T, error) {
	result := make([]T, len(input))
	for index, value := range input {
		if err := queryMappingContext(ctx); err != nil {
			return nil, err
		}
		result[index] = T(value)
	}
	return result, nil
}

func queryMappingContext(ctx context.Context) error {
	if ctx == nil {
		return &engine.WorkbenchError{Code: "engine.workbench.nil_context", Category: engine.WorkbenchErrorInvariant}
	}
	return ctx.Err()
}
