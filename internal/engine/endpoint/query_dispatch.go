// SPDX-License-Identifier: LicenseRef-LayerDraw-1.0

package endpoint

import (
	"context"
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
		mapped, err := mapExecuteQueryResult(result)
		return mapped, nil, err
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

func mapExecuteQueryResult(input engine.ExecuteDocumentQueryResult) (engineprotocol.ExecuteQueryResult, error) {
	var generation engineprotocol.DocumentGeneration
	if err := convertStruct(input.DocumentGeneration, &generation); err != nil {
		return engineprotocol.ExecuteQueryResult{}, err
	}
	returnedItems, err := canonicalUint64FromInt64(input.ReturnedItems)
	if err != nil {
		return engineprotocol.ExecuteQueryResult{}, err
	}
	returnedBytes, err := canonicalUint64FromInt64(input.ReturnedBytes)
	if err != nil {
		return engineprotocol.ExecuteQueryResult{}, err
	}
	result, err := mapQueryExecutionResultData(input.Result)
	if err != nil {
		return engineprotocol.ExecuteQueryResult{}, err
	}
	return engineprotocol.ExecuteQueryResult{
		DocumentGeneration: generation,
		Result:             result,
		ReturnedBytes:      engineprotocol.LogicalResponseByteCount(returnedBytes),
		ReturnedItems:      protocolcommon.CanonicalUint64(returnedItems),
	}, nil
}

func mapQueryExecutionResultData(input engine.QueryResult) (engineprotocol.QueryExecutionResultData, error) {
	arguments := make(map[string]semantic.RecipeScalar, len(input.Arguments))
	for address, value := range input.Arguments {
		mapped, err := mapRecipeScalar(materialize.Scalar{Type: value.Type, String: value.String, Int: value.Int, Float: value.Float, Bool: value.Bool})
		if err != nil {
			return engineprotocol.QueryExecutionResultData{}, err
		}
		arguments[address] = mapped
	}
	diagnostics, err := mapDiagnostics(input.Diagnostics)
	if err != nil {
		return engineprotocol.QueryExecutionResultData{}, err
	}
	return engineprotocol.QueryExecutionResultData{
		Arguments:                 arguments,
		CycleRefs:                 mapQueryCycleRefs(input.CycleRefs),
		Diagnostics:               diagnostics,
		InducedRelationAddresses:  typedStrings[semantic.RelationAddress](input.InducedRelationAddresses),
		PathRelationAddresses:     typedStrings[semantic.RelationAddress](input.PathRelationAddresses),
		Paths:                     mapQueryPaths(input.Paths),
		PrimaryEntityAddresses:    typedStrings[semantic.EntityAddress](input.PrimaryEntityAddresses),
		QueryAddress:              semantic.QueryAddress(input.QueryAddress),
		ReachedEntityAddresses:    typedStrings[semantic.EntityAddress](input.ReachedEntityAddresses),
		SeedEntityAddresses:       typedStrings[semantic.EntityAddress](input.SeedEntityAddresses),
		SelectedRelationAddresses: typedStrings[semantic.RelationAddress](input.SelectedRelationAddresses),
		StateInput:                engineprotocol.QueryStateInputRef{Kind: input.StateInput.Kind},
		StatePolicy:               input.StatePolicy,
		StateReads:                mapQueryStateReadRefs(input.StateReads),
		SupportEntityAddresses:    typedStrings[semantic.EntityAddress](input.SupportEntityAddresses),
		TraversedEntityAddresses:  typedStrings[semantic.EntityAddress](input.TraversedEntityAddresses),
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

func mapQueryStateReadRefs(input []engine.StateReadRef) []engineprotocol.QueryStateReadRef {
	result := make([]engineprotocol.QueryStateReadRef, len(input))
	for index, item := range input {
		result[index] = engineprotocol.QueryStateReadRef{
			FieldPath:      semantic.StateFieldPath(item.FieldPath),
			SubjectAddress: semantic.StableAddress(item.SubjectAddress),
		}
	}
	return result
}

func mapQueryPaths(input []engine.QueryPath) []engineprotocol.QueryPath {
	result := make([]engineprotocol.QueryPath, len(input))
	for index, item := range input {
		result[index] = engineprotocol.QueryPath{
			EntityAddresses:   typedStrings[semantic.EntityAddress](item.EntityAddresses),
			RelationAddresses: typedStrings[semantic.RelationAddress](item.RelationAddresses),
		}
	}
	return result
}

func mapQueryCycleRefs(input []engine.QueryCycleRef) []engineprotocol.QueryCycleRef {
	result := make([]engineprotocol.QueryCycleRef, len(input))
	for index, item := range input {
		result[index] = engineprotocol.QueryCycleRef{
			FromEntityAddress: semantic.EntityAddress(item.FromEntityAddress),
			Kind:              item.Kind,
			Orientation:       item.Orientation,
			RelationAddress:   semantic.RelationAddress(item.RelationAddress),
			RetainedPath:      mapQueryPath(item.RetainedPath),
			ToEntityAddress:   semantic.EntityAddress(item.ToEntityAddress),
		}
	}
	return result
}

func mapQueryPath(input engine.QueryPath) engineprotocol.QueryPath {
	return engineprotocol.QueryPath{
		EntityAddresses:   typedStrings[semantic.EntityAddress](input.EntityAddresses),
		RelationAddresses: typedStrings[semantic.RelationAddress](input.RelationAddresses),
	}
}
