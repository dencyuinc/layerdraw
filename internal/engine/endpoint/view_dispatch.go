// SPDX-License-Identifier: LicenseRef-LayerDraw-1.0

package endpoint

import (
	"context"
	"errors"
	"fmt"
	"math"
	"reflect"
	"strconv"

	"github.com/dencyuinc/layerdraw/gen/go/engineprotocol"
	"github.com/dencyuinc/layerdraw/gen/go/protocolcommon"
	"github.com/dencyuinc/layerdraw/gen/go/semantic"
	"github.com/dencyuinc/layerdraw/internal/engine"
	"github.com/dencyuinc/layerdraw/internal/engine/internal/compiler/materialize"
)

func runMaterializeView(payload engineprotocol.MaterializeViewInput) func(context.Context, workbenchDriver, map[string][]byte) (any, []OutputBlob, error) {
	return func(ctx context.Context, driver workbenchDriver, _ map[string][]byte) (any, []OutputBlob, error) {
		input, err := mapMaterializeViewInput(payload)
		if err != nil {
			return nil, nil, materializeViewMappingError(err)
		}
		result, err := driver.MaterializeDocumentView(ctx, input)
		if err != nil {
			return nil, nil, err
		}
		mapped, err := mapMaterializeViewResult(ctx, result, input.Limits)
		if err != nil {
			return nil, nil, materializeViewMappingError(err)
		}
		return mapped, nil, nil
	}
}

func materializeViewMappingError(err error) error {
	if err == nil || errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
		return err
	}
	var workbenchError *engine.WorkbenchError
	if errors.As(err, &workbenchError) {
		return err
	}
	return &engine.WorkbenchError{Code: "engine.workbench.view_data_invariant", Category: engine.WorkbenchErrorInvariant}
}

func mapMaterializeViewInput(input engineprotocol.MaterializeViewInput) (engine.MaterializeDocumentViewInput, error) {
	result := engine.MaterializeDocumentViewInput{ViewAddress: string(input.ViewAddress)}
	if err := convertStruct(input.DocumentGeneration, &result.DocumentGeneration); err != nil {
		return engine.MaterializeDocumentViewInput{}, err
	}
	if err := convertStruct(input.Limits, &result.Limits); err != nil {
		return engine.MaterializeDocumentViewInput{}, err
	}
	queryResult, err := queryResultFromProtocol(input.QueryResult)
	if err != nil {
		return engine.MaterializeDocumentViewInput{}, &engine.WorkbenchError{
			Code: "engine.workbench.invalid_query_result", Category: engine.WorkbenchErrorInputInvalid,
		}
	}
	result.QueryResult = queryResult
	return result, nil
}

func queryResultFromProtocol(input engineprotocol.QueryExecutionResultData) (engine.QueryResult, error) {
	result := engine.QueryResult{
		Arguments:                 make(map[string]engine.TypedScalar, len(input.Arguments)),
		QueryAddress:              string(input.QueryAddress),
		StatePolicy:               input.StatePolicy,
		StateInput:                engine.QueryStateInputRef{Kind: input.StateInput.Kind},
		InducedRelationAddresses:  protocolStrings(input.InducedRelationAddresses),
		PathRelationAddresses:     protocolStrings(input.PathRelationAddresses),
		PrimaryEntityAddresses:    protocolStrings(input.PrimaryEntityAddresses),
		ReachedEntityAddresses:    protocolStrings(input.ReachedEntityAddresses),
		SeedEntityAddresses:       protocolStrings(input.SeedEntityAddresses),
		SelectedRelationAddresses: protocolStrings(input.SelectedRelationAddresses),
		SupportEntityAddresses:    protocolStrings(input.SupportEntityAddresses),
		TraversedEntityAddresses:  protocolStrings(input.TraversedEntityAddresses),
		StateReads:                make([]engine.StateReadRef, len(input.StateReads)),
		Paths:                     make([]engine.QueryPath, len(input.Paths)),
		CycleRefs:                 make([]engine.QueryCycleRef, len(input.CycleRefs)),
		Diagnostics:               make([]engine.Diagnostic, len(input.Diagnostics)),
	}
	for address, value := range input.Arguments {
		mapped, err := engineScalarFromRecipeScalar(value)
		if err != nil {
			return engine.QueryResult{}, err
		}
		result.Arguments[address] = mapped
	}
	for index, value := range input.StateReads {
		result.StateReads[index] = engine.StateReadRef{SubjectAddress: string(value.SubjectAddress), FieldPath: string(value.FieldPath)}
	}
	for index, value := range input.Paths {
		result.Paths[index] = engine.QueryPath{EntityAddresses: protocolStrings(value.EntityAddresses), RelationAddresses: protocolStrings(value.RelationAddresses)}
	}
	for index, value := range input.CycleRefs {
		result.CycleRefs[index] = engine.QueryCycleRef{
			Kind: value.Kind, FromEntityAddress: string(value.FromEntityAddress), ToEntityAddress: string(value.ToEntityAddress),
			RelationAddress: string(value.RelationAddress), Orientation: value.Orientation,
			RetainedPath: engine.QueryPath{EntityAddresses: protocolStrings(value.RetainedPath.EntityAddresses), RelationAddresses: protocolStrings(value.RetainedPath.RelationAddresses)},
		}
	}
	for index, value := range input.Diagnostics {
		mapped, err := diagnosticFromProtocol(value)
		if err != nil {
			return engine.QueryResult{}, err
		}
		result.Diagnostics[index] = mapped
	}
	return result, nil
}

func protocolStrings[T ~string](input []T) []string {
	result := make([]string, len(input))
	for index, value := range input {
		result[index] = string(value)
	}
	return result
}

func diagnosticFromProtocol(input semantic.Diagnostic) (engine.Diagnostic, error) {
	result := engine.Diagnostic{
		Code: input.Code, Severity: string(input.Severity), MessageKey: input.MessageKey,
		Arguments: make(map[string]string, len(input.Arguments)), Related: make([]engine.DiagnosticRelated, len(input.Related)),
	}
	if input.Message != nil {
		result.Message = *input.Message
	}
	if input.SubjectAddress != nil {
		result.SubjectAddress = string(*input.SubjectAddress)
	}
	if input.OwnerAddress != nil {
		result.OwnerAddress = string(*input.OwnerAddress)
	}
	for key, value := range input.Arguments {
		if value.Kind != semantic.DiagnosticArgumentKindString || value.StringValue == nil {
			return engine.Diagnostic{}, fmt.Errorf("diagnostic argument %q is not a string", key)
		}
		result.Arguments[key] = *value.StringValue
	}
	if input.Range != nil {
		mapped, err := sourceRangeFromProtocol(*input.Range)
		if err != nil {
			return engine.Diagnostic{}, err
		}
		result.Range = &mapped
	}
	for index, value := range input.Related {
		mapped := engine.DiagnosticRelated{Relation: string(value.Relation)}
		if value.Message != nil {
			mapped.Message = *value.Message
		}
		if value.SubjectAddress != nil {
			mapped.SubjectAddress = string(*value.SubjectAddress)
		}
		if value.OwnerAddress != nil {
			mapped.OwnerAddress = string(*value.OwnerAddress)
		}
		if value.Range != nil {
			rangeValue, err := sourceRangeFromProtocol(*value.Range)
			if err != nil {
				return engine.Diagnostic{}, err
			}
			mapped.Range = &rangeValue
		}
		result.Related[index] = mapped
	}
	return result, nil
}

func sourceRangeFromProtocol(input semantic.SourceRange) (engine.SourceRange, error) {
	start, err := protocolByteOffset(input.StartByte)
	if err != nil {
		return engine.SourceRange{}, err
	}
	end, err := protocolByteOffset(input.EndByte)
	if err != nil {
		return engine.SourceRange{}, err
	}
	packAddress := ""
	if input.Origin.PackAddress != nil {
		packAddress = string(*input.Origin.PackAddress)
	}
	// The facade aliases the compiler's source range. A JSON-shaped bridge keeps
	// its internal OriginKind type out of the transport boundary.
	bridge := struct {
		Origin struct {
			Kind        string `json:"Kind"`
			PackAddress string `json:"PackAddress"`
		} `json:"Origin"`
		ModulePath string `json:"ModulePath"`
		StartByte  int    `json:"StartByte"`
		EndByte    int    `json:"EndByte"`
	}{ModulePath: input.ModulePath, StartByte: start, EndByte: end}
	bridge.Origin.Kind = string(input.Origin.Kind)
	bridge.Origin.PackAddress = packAddress
	var result engine.SourceRange
	if err := convertStruct(bridge, &result); err != nil {
		return engine.SourceRange{}, err
	}
	return result, nil
}

func protocolByteOffset(value protocolcommon.CanonicalUint64) (int, error) {
	parsed, err := strconv.ParseUint(string(value), 10, 64)
	// Use one portable bound so native and wasm32 endpoints reject the same input.
	if err != nil || parsed > math.MaxInt32 {
		return 0, fmt.Errorf("source byte offset is not representable")
	}
	return int(parsed), nil
}

func mapMaterializeViewResult(ctx context.Context, input engine.MaterializeDocumentViewResult, limits engine.WorkbenchLimits) (engineprotocol.MaterializeViewResult, error) {
	viewData, err := mapViewData(ctx, input.ViewData)
	if err != nil {
		return engineprotocol.MaterializeViewResult{}, err
	}
	items, err := countProtocolArrayItems(ctx, viewData, limits.MaxItems)
	if err != nil {
		return engineprotocol.MaterializeViewResult{}, err
	}
	var generation engineprotocol.DocumentGeneration
	if err := convertStruct(input.DocumentGeneration, &generation); err != nil {
		return engineprotocol.MaterializeViewResult{}, err
	}
	result := engineprotocol.MaterializeViewResult{
		DocumentGeneration: generation,
		ReturnedBytes:      engineprotocol.LogicalResponseByteCount("0"),
		ReturnedItems:      protocolcommon.CanonicalUint64(strconv.FormatInt(items, 10)),
		ViewData:           viewData,
	}
	returnedBytes, err := measureCanonicalJSON(ctx, result, limits.MaxOutputBytes)
	if err != nil {
		var limitError *canonicalJSONLimitError
		if errors.As(err, &limitError) {
			return engineprotocol.MaterializeViewResult{}, &engine.WorkbenchError{
				Code: "engine.workbench.limit_exceeded", Category: engine.WorkbenchErrorLimitExceeded,
				Resource: "view_data_bytes", Limit: limits.MaxOutputBytes, Observed: limitError.Observed,
			}
		}
		return engineprotocol.MaterializeViewResult{}, err
	}
	result.ReturnedBytes = engineprotocol.LogicalResponseByteCount(strconv.FormatInt(returnedBytes, 10))
	if _, err := engineprotocol.EncodeMaterializeViewResult(result); err != nil {
		return engineprotocol.MaterializeViewResult{}, fmt.Errorf("validate materialized ViewData result: %w", err)
	}
	return result, nil
}

func mapViewData(ctx context.Context, input engine.ViewData) (semantic.ViewData, error) {
	diagnostics := make([]semantic.Diagnostic, len(input.Diagnostics))
	for index, value := range input.Diagnostics {
		if err := queryMappingContext(ctx); err != nil {
			return semantic.ViewData{}, err
		}
		mapped, err := mapDiagnostic(value)
		if err != nil {
			return semantic.ViewData{}, err
		}
		diagnostics[index] = mapped
	}
	stateReads := make([]semantic.ViewDataStateReadRef, len(input.Source.StateReads))
	for index, value := range input.Source.StateReads {
		stateReads[index] = semantic.ViewDataStateReadRef{SubjectAddress: semantic.StableAddress(value.SubjectAddress), FieldPath: semantic.StateFieldPath(value.FieldPath)}
	}
	result := semantic.ViewData{
		ViewAddress: semantic.ViewAddress(input.ViewAddress), Category: input.Category, Shape: input.Shape,
		StatePolicy: input.StatePolicy, StateInput: semantic.ViewDataStateInputRef{Kind: input.StateInput.Kind}, Diagnostics: diagnostics,
		Source: semantic.ViewDataSourceRefs{
			QueryAddress:      semantic.QueryAddress(input.Source.QueryAddress),
			EntityAddresses:   protocolSlice[semantic.EntityAddress](input.Source.EntityAddresses),
			RelationAddresses: protocolSlice[semantic.RelationAddress](input.Source.RelationAddresses),
			LayerAddresses:    protocolSlice[semantic.LayerAddress](input.Source.LayerAddresses),
			RowAddresses:      protocolSlice[semantic.StableAddress](input.Source.RowAddresses), StateReads: stateReads,
		},
	}
	if input.Diagram != nil {
		mapped, err := mapDiagramViewData(ctx, *input.Diagram)
		if err != nil {
			return semantic.ViewData{}, err
		}
		result.Diagram = &mapped
	}
	if input.Table != nil {
		mapped, err := mapTableViewData(ctx, *input.Table)
		if err != nil {
			return semantic.ViewData{}, err
		}
		result.Table = &mapped
	}
	if input.Context != nil {
		mapped, err := mapContextViewData(ctx, *input.Context)
		if err != nil {
			return semantic.ViewData{}, err
		}
		result.Context = &mapped
	}
	return result, nil
}

func protocolSlice[T ~string](input []string) []T {
	result := make([]T, len(input))
	for index, value := range input {
		result[index] = T(value)
	}
	return result
}

func mapDiagramViewData(ctx context.Context, input engine.DiagramViewData) (semantic.DiagramViewData, error) {
	result := semantic.DiagramViewData{
		Nodes: make([]semantic.DiagramViewNode, len(input.Nodes)), Edges: make([]semantic.DiagramViewEdge, len(input.Edges)),
		Placements: make([]semantic.DiagramViewPlacement, len(input.Placements)),
	}
	for index, value := range input.Nodes {
		if err := queryMappingContext(ctx); err != nil {
			return semantic.DiagramViewData{}, err
		}
		result.Nodes[index] = semantic.DiagramViewNode{
			Key: value.Key, EntityAddress: semantic.EntityAddress(value.EntityAddress), DisplayName: value.DisplayName,
			EntityTypeAddress: semantic.EntityTypeAddress(value.EntityType), LayerAddress: semantic.LayerAddress(value.LayerAddress),
			SourceEntities: protocolSlice[semantic.EntityAddress](value.SourceEntities),
		}
	}
	for index, value := range input.Edges {
		if err := queryMappingContext(ctx); err != nil {
			return semantic.DiagramViewData{}, err
		}
		result.Edges[index] = semantic.DiagramViewEdge{
			Key: value.Key, RelationAddress: semantic.RelationAddress(value.RelationAddress), FromAddress: semantic.EntityAddress(value.FromAddress),
			ToAddress: semantic.EntityAddress(value.ToAddress), RelationTypeAddress: semantic.RelationTypeAddress(value.RelationType),
			DisplayName: value.DisplayName, SourceRelations: protocolSlice[semantic.RelationAddress](value.SourceRelations),
		}
	}
	for index, value := range input.Placements {
		x, err := finiteDecimal(value.X, false)
		if err != nil {
			return semantic.DiagramViewData{}, err
		}
		y, err := finiteDecimal(value.Y, false)
		if err != nil {
			return semantic.DiagramViewData{}, err
		}
		width, err := finiteDecimal(value.Width, true)
		if err != nil {
			return semantic.DiagramViewData{}, err
		}
		height, err := finiteDecimal(value.Height, true)
		if err != nil {
			return semantic.DiagramViewData{}, err
		}
		result.Placements[index] = semantic.DiagramViewPlacement{
			EntityAddress: semantic.EntityAddress(value.EntityAddress), X: x, Y: y,
			Width: semantic.CanonicalPositiveFiniteDecimal(width), Height: semantic.CanonicalPositiveFiniteDecimal(height),
		}
	}
	return result, nil
}

func mapTableViewData(ctx context.Context, input engine.TableViewData) (semantic.TableViewData, error) {
	result := semantic.TableViewData{
		Columns: make([]semantic.TableViewColumnData, len(input.Columns)), Rows: make([]semantic.TableViewRowData, len(input.Rows)),
		Sorts: make([]semantic.ViewTableSort, len(input.Sorts)),
	}
	for index, value := range input.Columns {
		column := semantic.TableViewColumnData{ID: semantic.LocalIdentifier(value.ID), Label: value.Label, Source: value.Source}
		if value.Address != "" {
			address := semantic.TableColumnAddress(value.Address)
			column.Address = &address
		}
		result.Columns[index] = column
	}
	for index, value := range input.Sorts {
		result.Sorts[index] = semantic.ViewTableSort{ColumnID: value.ColumnID, Direction: value.Direction, Absent: value.Absent}
	}
	for index, value := range input.Rows {
		if err := queryMappingContext(ctx); err != nil {
			return semantic.TableViewData{}, err
		}
		row := semantic.TableViewRowData{
			Key: value.Key, SubjectAddress: semantic.StableAddress(value.SubjectAddress), OwnerAddress: semantic.StableAddress(value.OwnerAddress),
			SourceRows: protocolSlice[semantic.StableAddress](value.SourceRows), SourceEntities: protocolSlice[semantic.EntityAddress](value.SourceEntities),
			SourceRelations: protocolSlice[semantic.RelationAddress](value.SourceRelations), Cells: make([]semantic.TableViewCellData, len(value.Cells)),
		}
		for cellIndex, cellValue := range value.Cells {
			mappedValue, err := mapViewDataValue(cellValue.Value)
			if err != nil {
				return semantic.TableViewData{}, err
			}
			stateReads := make([]semantic.ViewDataStateReadRef, len(cellValue.StateReads))
			for readIndex, read := range cellValue.StateReads {
				stateReads[readIndex] = semantic.ViewDataStateReadRef{SubjectAddress: semantic.StableAddress(read.SubjectAddress), FieldPath: semantic.StateFieldPath(read.FieldPath)}
			}
			cellRefs := make([]semantic.ViewDataCellRef, len(cellValue.SourceCells))
			for refIndex, ref := range cellValue.SourceCells {
				cellRefs[refIndex] = semantic.ViewDataCellRef{RowAddress: semantic.StableAddress(ref.RowAddress), ColumnAddress: semantic.ColumnAddress(ref.ColumnAddress)}
			}
			row.Cells[cellIndex] = semantic.TableViewCellData{
				ColumnID: semantic.LocalIdentifier(cellValue.ColumnID), Value: mappedValue,
				SourceRows: protocolSlice[semantic.StableAddress](cellValue.SourceRows), SourceCells: cellRefs,
				SourceEntities: protocolSlice[semantic.EntityAddress](cellValue.SourceEntities), SourceRelations: protocolSlice[semantic.RelationAddress](cellValue.SourceRelations),
				StateReads: stateReads,
			}
		}
		result.Rows[index] = row
	}
	return result, nil
}

func mapViewDataValue(input engine.ViewDataValue) (semantic.ViewDataValue, error) {
	result := semantic.ViewDataValue{Kind: input.Kind}
	switch input.Kind {
	case "null":
		if !input.Null {
			return semantic.ViewDataValue{}, fmt.Errorf("null ViewData value is not marked null")
		}
		value := true
		result.Null = &value
	case "scalar":
		if input.Scalar == nil {
			return semantic.ViewDataValue{}, fmt.Errorf("scalar ViewData value is absent")
		}
		value, err := mapRecipeScalar(materialize.Scalar{Type: input.Scalar.Type, String: input.Scalar.String, Int: input.Scalar.Int, Float: input.Scalar.Float, Bool: input.Scalar.Bool})
		if err != nil {
			return semantic.ViewDataValue{}, err
		}
		result.Scalar = &value
	case "stable_address":
		if input.Address == nil {
			return semantic.ViewDataValue{}, fmt.Errorf("stable-address ViewData value is absent")
		}
		value := semantic.StableAddress(*input.Address)
		result.StableAddress = &value
	case "string_set":
		value := append([]string(nil), input.StringSet...)
		if value == nil {
			value = []string{}
		}
		result.StringSet = &value
	default:
		return semantic.ViewDataValue{}, fmt.Errorf("unsupported ViewData value kind %q", input.Kind)
	}
	return result, nil
}

func mapContextViewData(ctx context.Context, input engine.ContextViewData) (semantic.ContextViewData, error) {
	result := semantic.ContextViewData{Groups: make([]semantic.ContextViewGroup, len(input.Groups)), Facts: make([]semantic.ContextViewFact, len(input.Facts))}
	for index, value := range input.Groups {
		if err := queryMappingContext(ctx); err != nil {
			return semantic.ContextViewData{}, err
		}
		result.Groups[index] = semantic.ContextViewGroup{Key: value.Key, Label: value.Label, Addresses: protocolSlice[semantic.StableAddress](value.Addresses)}
	}
	for index, value := range input.Facts {
		if err := queryMappingContext(ctx); err != nil {
			return semantic.ContextViewData{}, err
		}
		result.Facts[index] = semantic.ContextViewFact{
			Key: value.Key, SubjectAddress: semantic.StableAddress(value.SubjectAddress), Kind: value.Kind, Text: value.Text,
			SourceEntities: protocolSlice[semantic.EntityAddress](value.SourceEntities), SourceRelations: protocolSlice[semantic.RelationAddress](value.SourceRelations),
			SourceRows: protocolSlice[semantic.StableAddress](value.SourceRows),
		}
	}
	return result, nil
}

func countProtocolArrayItems(ctx context.Context, value any, limit int64) (int64, error) {
	if ctx == nil {
		return 0, &engine.WorkbenchError{Code: "engine.workbench.nil_context", Category: engine.WorkbenchErrorInvariant}
	}
	count := int64(0)
	var walk func(reflect.Value, int) error
	walk = func(current reflect.Value, depth int) error {
		if err := ctx.Err(); err != nil {
			return err
		}
		if depth > 128 {
			return fmt.Errorf("ViewData exceeds maximum protocol depth")
		}
		if !current.IsValid() {
			return nil
		}
		if current.Kind() == reflect.Interface || current.Kind() == reflect.Pointer {
			if current.IsNil() {
				return nil
			}
			return walk(current.Elem(), depth+1)
		}
		switch current.Kind() {
		case reflect.Slice, reflect.Array:
			length := int64(current.Len())
			if length > math.MaxInt64-count {
				return fmt.Errorf("ViewData item count overflows int64")
			}
			count += length
			if count > limit {
				return &engine.WorkbenchError{Code: "engine.workbench.limit_exceeded", Category: engine.WorkbenchErrorLimitExceeded, Resource: "view_data_items", Limit: limit, Observed: count}
			}
			for index := 0; index < current.Len(); index++ {
				if err := walk(current.Index(index), depth+1); err != nil {
					return err
				}
			}
		case reflect.Struct:
			for index := 0; index < current.NumField(); index++ {
				if current.Type().Field(index).PkgPath == "" {
					if err := walk(current.Field(index), depth+1); err != nil {
						return err
					}
				}
			}
		case reflect.Map:
			iterator := current.MapRange()
			for iterator.Next() {
				if err := walk(iterator.Value(), depth+1); err != nil {
					return err
				}
			}
		}
		return nil
	}
	if err := walk(reflect.ValueOf(value), 0); err != nil {
		return 0, err
	}
	return count, nil
}
