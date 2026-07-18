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
	if err := convertStruct(input.Limits, &result.Limits); err != nil {
		return engine.MaterializeDocumentViewInput{}, err
	}
	switch input.Kind {
	case "query":
		if input.Query == nil || input.Diff != nil {
			return engine.MaterializeDocumentViewInput{}, invalidMaterializeViewInput("engine.workbench.invalid_view_source")
		}
		mapped, err := mapMaterializeQueryViewInput(*input.Query)
		if err != nil {
			return engine.MaterializeDocumentViewInput{}, err
		}
		result.Query = &mapped
	case "diff":
		if input.Diff == nil || input.Query != nil {
			return engine.MaterializeDocumentViewInput{}, invalidMaterializeViewInput("engine.workbench.invalid_view_source")
		}
		mapped, err := mapMaterializeDiffViewInput(*input.Diff)
		if err != nil {
			return engine.MaterializeDocumentViewInput{}, err
		}
		result.Diff = &mapped
	default:
		return engine.MaterializeDocumentViewInput{}, invalidMaterializeViewInput("engine.workbench.invalid_view_source")
	}
	return result, nil
}

func mapMaterializeQueryViewInput(input engineprotocol.MaterializeQueryViewInput) (engine.MaterializeDocumentQueryViewInput, error) {
	var result engine.MaterializeDocumentQueryViewInput
	if err := convertStruct(input.DocumentGeneration, &result.DocumentGeneration); err != nil {
		return result, err
	}
	queryResult, err := queryResultFromProtocol(input.QueryResult)
	if err != nil {
		return result, invalidMaterializeViewInput("engine.workbench.invalid_query_result")
	}
	result.QueryResult = queryResult
	if input.StateSnapshot != nil {
		snapshot, err := stateQuerySnapshotFromProtocol(*input.StateSnapshot)
		if err != nil {
			return result, invalidMaterializeViewInput("engine.workbench.invalid_state_snapshot")
		}
		result.StateSnapshot = &snapshot
	}
	return result, nil
}

func mapMaterializeDiffViewInput(input engineprotocol.MaterializeDiffViewInput) (engine.MaterializeDocumentDiffViewInput, error) {
	var result engine.MaterializeDocumentDiffViewInput
	if err := convertStruct(input.RecipeGeneration, &result.RecipeGeneration); err != nil {
		return result, err
	}
	if err := convertStruct(input.BeforeGeneration, &result.BeforeGeneration); err != nil {
		return result, err
	}
	if err := convertStruct(input.AfterGeneration, &result.AfterGeneration); err != nil {
		return result, err
	}
	if input.BeforeQueryResult != nil {
		mapped, err := queryResultFromProtocol(*input.BeforeQueryResult)
		if err != nil {
			return result, invalidMaterializeViewInput("engine.workbench.invalid_query_result")
		}
		result.BeforeQueryResult = &mapped
	}
	if input.AfterQueryResult != nil {
		mapped, err := queryResultFromProtocol(*input.AfterQueryResult)
		if err != nil {
			return result, invalidMaterializeViewInput("engine.workbench.invalid_query_result")
		}
		result.AfterQueryResult = &mapped
	}
	return result, nil
}

func invalidMaterializeViewInput(code string) error {
	return &engine.WorkbenchError{Code: code, Category: engine.WorkbenchErrorInputInvalid}
}

func queryResultFromProtocol(input engineprotocol.QueryExecutionResultData) (engine.QueryResult, error) {
	result := engine.QueryResult{
		Arguments:                 make(map[string]engine.TypedScalar, len(input.Arguments)),
		QueryAddress:              string(input.QueryAddress),
		StatePolicy:               input.StatePolicy,
		StateInput:                queryStateInputRefFromProtocol(input.StateInput),
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

func queryStateInputRefFromProtocol(input engineprotocol.QueryStateInputRef) engine.QueryStateInputRef {
	result := engine.QueryStateInputRef{Kind: input.Kind}
	if input.SnapshotHash != nil {
		result.SnapshotHash = string(*input.SnapshotHash)
	}
	if input.StateVersion != nil {
		result.StateVersion = *input.StateVersion
	}
	if input.CapturedAt != nil {
		result.CapturedAt = string(*input.CapturedAt)
	}
	if input.DefinitionHash != nil {
		result.DefinitionHash = string(*input.DefinitionHash)
	}
	return result
}

func stateQuerySnapshotFromProtocol(input semantic.StateQuerySnapshot) (engine.StateQuerySnapshot, error) {
	result := engine.StateQuerySnapshot{
		Format:                 string(input.Format),
		SchemaVersion:          int(input.SchemaVersion),
		DefinitionProject:      string(input.DefinitionProjectAddress),
		DefinitionHash:         string(input.DefinitionHash),
		GraphHash:              string(input.GraphHash),
		StateVersion:           input.StateVersion,
		CapturedAt:             string(input.CapturedAt),
		InaccessibleFieldPaths: protocolStrings(input.InaccessibleFieldPaths),
		Subjects:               make([]engine.StateQuerySubject, len(input.Subjects)),
	}
	for index, subject := range input.Subjects {
		mapped := engine.StateQuerySubject{
			SubjectAddress:     string(subject.SubjectAddress),
			OwnSubjectHash:     string(subject.OwnSubjectHash),
			Fields:             make(map[string]engine.TypedScalar, len(subject.Fields)),
			RedactedFieldPaths: protocolStrings(subject.RedactedFieldPaths),
		}
		for path, value := range subject.Fields {
			scalar, err := engineScalarFromRecipeScalar(value)
			if err != nil {
				return engine.StateQuerySnapshot{}, err
			}
			mapped.Fields[path] = scalar
		}
		result.Subjects[index] = mapped
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
	blobBytes, err := measureViewDataBlobBytes(ctx, input.ViewData)
	if err != nil {
		return engineprotocol.MaterializeViewResult{}, err
	}
	if uint64(returnedBytes) > math.MaxUint64-blobBytes {
		return engineprotocol.MaterializeViewResult{}, fmt.Errorf("ViewData logical response byte count overflows uint64")
	}
	logicalBytes := uint64(returnedBytes) + blobBytes
	if limits.MaxOutputBytes > 0 && logicalBytes > uint64(limits.MaxOutputBytes) {
		observed := limits.MaxOutputBytes
		if logicalBytes <= math.MaxInt64 {
			observed = int64(logicalBytes)
		} else if observed < math.MaxInt64 {
			observed++
		}
		return engineprotocol.MaterializeViewResult{}, &engine.WorkbenchError{
			Code: "engine.workbench.limit_exceeded", Category: engine.WorkbenchErrorLimitExceeded,
			Resource: "view_data_bytes", Limit: limits.MaxOutputBytes, Observed: observed,
		}
	}
	result.ReturnedBytes = engineprotocol.LogicalResponseByteCount(strconv.FormatUint(logicalBytes, 10))
	if _, err := engineprotocol.EncodeMaterializeViewResult(result); err != nil {
		return engineprotocol.MaterializeViewResult{}, fmt.Errorf("validate materialized ViewData result: %w", err)
	}
	return result, nil
}

func measureViewDataBlobBytes(ctx context.Context, input engine.ViewData) (uint64, error) {
	if err := queryMappingContext(ctx); err != nil {
		return 0, err
	}
	if input.Diff == nil {
		return 0, nil
	}
	var total uint64
	var walk func(engine.SemanticValue, int) error
	walk = func(value engine.SemanticValue, depth int) error {
		if err := ctx.Err(); err != nil {
			return err
		}
		if depth > protocolcommon.MaxWireJSONDepth {
			return fmt.Errorf("Diff semantic value exceeds maximum protocol depth")
		}
		switch value.Kind {
		case engine.SemanticValueBlob:
			if value.BlobRef == nil {
				return fmt.Errorf("Diff blob value lacks BlobRef")
			}
			if math.MaxUint64-total < value.BlobRef.Size {
				return fmt.Errorf("ViewData BlobRef byte count overflows uint64")
			}
			total += value.BlobRef.Size
		case engine.SemanticValueArray:
			for _, item := range value.Array {
				if err := walk(item, depth+1); err != nil {
					return err
				}
			}
		case engine.SemanticValueMap:
			for _, item := range value.Map {
				if err := walk(item.Value, depth+1); err != nil {
					return err
				}
			}
		}
		return nil
	}
	for _, change := range input.Diff.Changes {
		for _, field := range change.Fields {
			if field.Before != nil {
				if err := walk(*field.Before, 0); err != nil {
					return 0, err
				}
			}
			if field.After != nil {
				if err := walk(*field.After, 0); err != nil {
					return 0, err
				}
			}
		}
	}
	return total, nil
}

func mapViewData(ctx context.Context, input engine.ViewData) (semantic.ViewData, error) {
	base, ok := input.Base()
	if !ok {
		return semantic.ViewData{}, fmt.Errorf("invalid ViewData union")
	}
	shape, err := mapCompiledViewShape(base.Shape)
	if err != nil {
		return semantic.ViewData{}, err
	}
	revision, err := mapViewRevision(base.Revision)
	if err != nil {
		return semantic.ViewData{}, err
	}
	source, err := mapViewDataSourceRefs(ctx, base.Source)
	if err != nil {
		return semantic.ViewData{}, err
	}
	diagnostics := make([]semantic.Diagnostic, len(base.Diagnostics))
	for index, value := range base.Diagnostics {
		if err := queryMappingContext(ctx); err != nil {
			return semantic.ViewData{}, err
		}
		mapped, err := mapDiagnostic(value)
		if err != nil {
			return semantic.ViewData{}, err
		}
		diagnostics[index] = mapped
	}
	result := semantic.ViewData{
		Kind:           string(base.Kind),
		Category:       base.Category,
		Shape:          shape,
		ProjectAddress: semantic.ProjectRootAddress(base.ProjectAddress),
		ViewAddress:    semantic.ViewAddress(base.ViewAddress),
		Revision:       revision,
		StatePolicy:    base.StatePolicy,
		StateInput:     mapViewDataStateInput(base.StateInput),
		Source:         source,
		Diagnostics:    diagnostics,
	}
	if base.QueryAddress != nil {
		value := semantic.QueryAddress(*base.QueryAddress)
		result.QueryAddress = &value
	}
	switch base.Kind {
	case engine.ViewDataDiagram:
		mapped, err := mapDiagramViewData(ctx, *input.Diagram)
		if err != nil {
			return semantic.ViewData{}, err
		}
		result.Diagram = &mapped
	case engine.ViewDataTable:
		mapped, err := mapTableViewData(ctx, *input.Table)
		if err != nil {
			return semantic.ViewData{}, err
		}
		result.Table = &mapped
	case engine.ViewDataMatrix:
		mapped, err := mapMatrixViewData(ctx, *input.Matrix)
		if err != nil {
			return semantic.ViewData{}, err
		}
		result.Matrix = &mapped
	case engine.ViewDataTree:
		mapped, err := mapTreeViewData(ctx, *input.Tree)
		if err != nil {
			return semantic.ViewData{}, err
		}
		result.Tree = &mapped
	case engine.ViewDataFlow:
		mapped, err := mapFlowViewData(ctx, *input.Flow)
		if err != nil {
			return semantic.ViewData{}, err
		}
		result.Flow = &mapped
	case engine.ViewDataContext:
		mapped, err := mapContextViewData(ctx, *input.Context)
		if err != nil {
			return semantic.ViewData{}, err
		}
		result.Context = &mapped
	case engine.ViewDataDiff:
		mapped, err := mapDiffViewData(ctx, *input.Diff)
		if err != nil {
			return semantic.ViewData{}, err
		}
		result.Diff = &mapped
	default:
		return semantic.ViewData{}, fmt.Errorf("unsupported ViewData kind %q", base.Kind)
	}
	return result, nil
}

func mapViewRevision(input engine.ViewRevision) (semantic.ViewRevision, error) {
	if (input.Single == nil) == (input.Diff == nil) {
		return semantic.ViewRevision{}, fmt.Errorf("invalid View revision union")
	}
	if input.Single != nil {
		if input.Single.Kind != "single" {
			return semantic.ViewRevision{}, fmt.Errorf("invalid single View revision kind")
		}
		revisionID := input.Single.RevisionID
		definitionHash := protocolcommon.Digest(input.Single.DefinitionHash)
		return semantic.ViewRevision{Kind: "single", RevisionID: &revisionID, DefinitionHash: &definitionHash}, nil
	}
	if input.Diff.Kind != "diff" {
		return semantic.ViewRevision{}, fmt.Errorf("invalid diff View revision kind")
	}
	recipeRevisionID := input.Diff.RecipeRevisionID
	recipeDefinitionHash := protocolcommon.Digest(input.Diff.RecipeDefinitionHash)
	beforeRevisionID := input.Diff.BeforeRevisionID
	beforeDefinitionHash := protocolcommon.Digest(input.Diff.BeforeDefinitionHash)
	afterRevisionID := input.Diff.AfterRevisionID
	afterDefinitionHash := protocolcommon.Digest(input.Diff.AfterDefinitionHash)
	return semantic.ViewRevision{
		Kind:                 "diff",
		RecipeRevisionID:     &recipeRevisionID,
		RecipeDefinitionHash: &recipeDefinitionHash,
		BeforeRevisionID:     &beforeRevisionID,
		BeforeDefinitionHash: &beforeDefinitionHash,
		AfterRevisionID:      &afterRevisionID,
		AfterDefinitionHash:  &afterDefinitionHash,
	}, nil
}

func mapViewDataStateInput(input engine.QueryStateInputRef) semantic.ViewDataStateInputRef {
	mapped := mapQueryStateInputRef(input)
	return semantic.ViewDataStateInputRef{
		Kind:           mapped.Kind,
		SnapshotHash:   mapped.SnapshotHash,
		StateVersion:   mapped.StateVersion,
		CapturedAt:     mapped.CapturedAt,
		DefinitionHash: mapped.DefinitionHash,
	}
}

func mapViewDataSourceRefs(ctx context.Context, input engine.ViewDataSourceRefs) (semantic.ViewDataSourceRefs, error) {
	if err := queryMappingContext(ctx); err != nil {
		return semantic.ViewDataSourceRefs{}, err
	}
	cellRefs := make([]semantic.ViewDataCellRef, len(input.CellRefs))
	for index, value := range input.CellRefs {
		cellRefs[index] = semantic.ViewDataCellRef{RowAddress: semantic.StableAddress(value.RowAddress), ColumnAddress: semantic.ColumnAddress(value.ColumnAddress)}
	}
	stateReads := make([]semantic.ViewDataStateReadRef, len(input.State.Reads))
	for index, value := range input.State.Reads {
		stateReads[index] = semantic.ViewDataStateReadRef{SubjectAddress: semantic.StableAddress(value.SubjectAddress), FieldPath: semantic.StateFieldPath(value.FieldPath)}
	}
	return semantic.ViewDataSourceRefs{
		SubjectAddresses:  protocolSlice[semantic.StableAddress](input.SubjectAddresses),
		EntityAddresses:   protocolSlice[semantic.EntityAddress](input.EntityAddresses),
		RelationAddresses: protocolSlice[semantic.RelationAddress](input.RelationAddresses),
		LayerAddresses:    protocolSlice[semantic.LayerAddress](input.LayerAddresses),
		RowAddresses:      protocolSlice[semantic.StableAddress](input.RowAddresses),
		CellRefs:          cellRefs,
		AssetDigests:      protocolSlice[protocolcommon.Digest](input.AssetDigests),
		State:             semantic.ViewDataStateRefs{Reads: stateReads},
	}, nil
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
		Occurrences:  make([]semantic.DiagramOccurrence, len(input.Occurrences)),
		Edges:        make([]semantic.DiagramEdge, len(input.Edges)),
		Containers:   make([]semantic.DiagramContainer, len(input.Containers)),
		Overlays:     make([]semantic.DiagramOverlay, len(input.Overlays)),
		Badges:       make([]semantic.DiagramBadge, len(input.Badges)),
		SupportItems: make([]semantic.DiagramSupportItem, len(input.SupportItems)),
	}
	for index, value := range input.Occurrences {
		source, err := mapViewDataSourceRefs(ctx, value.Source)
		if err != nil {
			return semantic.DiagramViewData{}, err
		}
		result.Occurrences[index] = semantic.DiagramOccurrence{
			Key:                semantic.ViewDataItemKey(value.Key),
			EntityAddress:      semantic.EntityAddress(value.EntityAddress),
			LayerAddress:       semantic.LayerAddress(value.LayerAddress),
			ParentKey:          optionalTypedStringPointer[semantic.ViewDataItemKey](value.ParentKey),
			ViaRelationAddress: optionalTypedStringPointer[semantic.RelationAddress](value.ViaRelationAddress),
			Role:               string(value.Role),
			Source:             source,
		}
	}
	for index, value := range input.Edges {
		source, err := mapViewDataSourceRefs(ctx, value.Source)
		if err != nil {
			return semantic.DiagramViewData{}, err
		}
		result.Edges[index] = semantic.DiagramEdge{
			Key:                 semantic.ViewDataItemKey(value.Key),
			FromOccurrenceKey:   semantic.ViewDataItemKey(value.FromOccurrenceKey),
			ToOccurrenceKey:     semantic.ViewDataItemKey(value.ToOccurrenceKey),
			RelationAddress:     semantic.RelationAddress(value.RelationAddress),
			RelationTypeAddress: semantic.RelationTypeAddress(value.RelationTypeAddress),
			Source:              source,
		}
	}
	for index, value := range input.Containers {
		source, err := mapViewDataSourceRefs(ctx, value.Source)
		if err != nil {
			return semantic.DiagramViewData{}, err
		}
		result.Containers[index] = semantic.DiagramContainer{
			Key:           semantic.ViewDataItemKey(value.Key),
			OccurrenceKey: semantic.ViewDataItemKey(value.OccurrenceKey),
			ChildKeys:     protocolSlice[semantic.ViewDataItemKey](value.ChildKeys),
			Source:        source,
		}
	}
	for index, value := range input.Overlays {
		source, err := mapViewDataSourceRefs(ctx, value.Source)
		if err != nil {
			return semantic.DiagramViewData{}, err
		}
		result.Overlays[index] = semantic.DiagramOverlay{
			Key:                  semantic.ViewDataItemKey(value.Key),
			TargetOccurrenceKey:  semantic.ViewDataItemKey(value.TargetOccurrenceKey),
			OverlayEntityAddress: semantic.EntityAddress(value.OverlayEntityAddress),
			RelationAddress:      semantic.RelationAddress(value.RelationAddress),
			RelationTypeAddress:  semantic.RelationTypeAddress(value.RelationTypeAddress),
			Source:               source,
		}
	}
	for index, value := range input.Badges {
		source, err := mapViewDataSourceRefs(ctx, value.Source)
		if err != nil {
			return semantic.DiagramViewData{}, err
		}
		result.Badges[index] = semantic.DiagramBadge{
			Key:                 semantic.ViewDataItemKey(value.Key),
			TargetOccurrenceKey: semantic.ViewDataItemKey(value.TargetOccurrenceKey),
			RelationAddress:     semantic.RelationAddress(value.RelationAddress),
			RelationTypeAddress: semantic.RelationTypeAddress(value.RelationTypeAddress),
			Label:               cloneStringPointer(value.Label),
			Source:              source,
		}
	}
	for index, value := range input.SupportItems {
		source, err := mapViewDataSourceRefs(ctx, value.Source)
		if err != nil {
			return semantic.DiagramViewData{}, err
		}
		result.SupportItems[index] = semantic.DiagramSupportItem{
			Key:             semantic.ViewDataItemKey(value.Key),
			SupportKind:     string(value.SupportKind),
			EntityAddress:   optionalTypedStringPointer[semantic.EntityAddress](value.EntityAddress),
			RelationAddress: optionalTypedStringPointer[semantic.RelationAddress](value.RelationAddress),
			Source:          source,
		}
	}
	return result, nil
}

func mapTableViewData(ctx context.Context, input engine.TableViewData) (semantic.TableViewData, error) {
	result := semantic.TableViewData{
		Columns: make([]semantic.TableColumn, len(input.Columns)),
		Rows:    make([]semantic.TableRow, len(input.Rows)),
	}
	for index, value := range input.Columns {
		column := semantic.TableColumn{
			Key:                   semantic.ViewDataItemKey(value.Key),
			ID:                    semantic.LocalIdentifier(value.ID),
			Label:                 value.Label,
			ValueType:             semantic.TableViewValueType(value.ValueType),
			SourceColumnAddresses: protocolSlice[semantic.ColumnAddress](value.SourceColumnAddresses),
		}
		if value.Address != nil {
			address := semantic.TableColumnAddress(*value.Address)
			column.Address = &address
		}
		if value.EnumValues != nil {
			values := cloneStrings(value.EnumValues)
			column.EnumValues = &values
		}
		if value.StateFieldPath != nil {
			path := semantic.StateFieldPath(*value.StateFieldPath)
			column.StateFieldPath = &path
		}
		result.Columns[index] = column
	}
	for index, value := range input.Rows {
		source, err := mapViewDataSourceRefs(ctx, value.Source)
		if err != nil {
			return semantic.TableViewData{}, err
		}
		row := semantic.TableRow{
			Key:    semantic.ViewDataItemKey(value.Key),
			Cells:  make(map[string]semantic.TableCell, len(value.Cells)),
			Source: source,
		}
		for key, value := range value.Cells {
			cellSource, err := mapViewDataSourceRefs(ctx, value.Source)
			if err != nil {
				return semantic.TableViewData{}, err
			}
			cell := semantic.TableCell{Present: value.Present, Source: cellSource}
			if value.Value != nil {
				mapped, err := mapViewDataValue(*value.Value)
				if err != nil {
					return semantic.TableViewData{}, err
				}
				cell.Value = &mapped
			}
			row.Cells[key] = cell
		}
		result.Rows[index] = row
	}
	return result, nil
}

func mapViewDataValue(input engine.ViewDataValue) (semantic.ViewDataValue, error) {
	result := semantic.ViewDataValue{Kind: input.Kind}
	switch input.Kind {
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
		value := append([]string{}, input.StringSet...)
		result.StringSet = &value
	default:
		return semantic.ViewDataValue{}, fmt.Errorf("unsupported ViewData value kind %q", input.Kind)
	}
	return result, nil
}

func mapMatrixViewData(ctx context.Context, input engine.MatrixViewData) (semantic.MatrixViewData, error) {
	rowAxis, err := mapMatrixAxisItems(ctx, input.RowAxis)
	if err != nil {
		return semantic.MatrixViewData{}, err
	}
	columnAxis, err := mapMatrixAxisItems(ctx, input.ColumnAxis)
	if err != nil {
		return semantic.MatrixViewData{}, err
	}
	result := semantic.MatrixViewData{
		RowAxis:    rowAxis,
		ColumnAxis: columnAxis,
		Cells:      make([]semantic.MatrixCell, len(input.Cells)),
	}
	for index, value := range input.Cells {
		source, err := mapViewDataSourceRefs(ctx, value.Source)
		if err != nil {
			return semantic.MatrixViewData{}, err
		}
		semanticRefs := make([]semantic.MatrixSemanticRef, len(value.SemanticRefs))
		for refIndex, ref := range value.SemanticRefs {
			mapped, err := mapMatrixSemanticRef(ref)
			if err != nil {
				return semantic.MatrixViewData{}, err
			}
			semanticRefs[refIndex] = mapped
		}
		displayValue, err := mapMatrixDisplayValue(value.DisplayValue)
		if err != nil {
			return semantic.MatrixViewData{}, err
		}
		result.Cells[index] = semantic.MatrixCell{
			Key:          semantic.ViewDataItemKey(value.Key),
			RowKey:       semantic.ViewDataItemKey(value.RowKey),
			ColumnKey:    semantic.ViewDataItemKey(value.ColumnKey),
			SemanticRefs: semanticRefs,
			DisplayValue: displayValue,
			Source:       source,
		}
	}
	return result, nil
}

func mapMatrixAxisItems(ctx context.Context, input []engine.MatrixAxisItem) ([]semantic.MatrixAxisItem, error) {
	result := make([]semantic.MatrixAxisItem, len(input))
	for index, value := range input {
		source, err := mapViewDataSourceRefs(ctx, value.Source)
		if err != nil {
			return nil, err
		}
		result[index] = semantic.MatrixAxisItem{
			Key:           semantic.ViewDataItemKey(value.Key),
			EntityAddress: semantic.EntityAddress(value.EntityAddress),
			Label:         value.Label,
			Source:        source,
		}
	}
	return result, nil
}

func mapMatrixSemanticRef(input engine.MatrixSemanticRef) (semantic.MatrixSemanticRef, error) {
	if (input.RelationAddress == nil) == (input.Path == nil) {
		return semantic.MatrixSemanticRef{}, fmt.Errorf("invalid Matrix semantic-ref union")
	}
	if input.RelationAddress != nil {
		address := semantic.RelationAddress(*input.RelationAddress)
		return semantic.MatrixSemanticRef{Kind: "relation", RelationAddress: &address}, nil
	}
	path := semantic.ViewDataQueryPath{
		EntityAddresses:   protocolSlice[semantic.EntityAddress](input.Path.EntityAddresses),
		RelationAddresses: protocolSlice[semantic.RelationAddress](input.Path.RelationAddresses),
	}
	return semantic.MatrixSemanticRef{Kind: "path", Path: &path}, nil
}

func mapMatrixDisplayValue(input engine.MatrixDisplayValue) (semantic.MatrixDisplayValue, error) {
	result := semantic.MatrixDisplayValue{Kind: input.Kind}
	switch input.Kind {
	case "boolean":
		value := input.Boolean
		result.Boolean = &value
	case "integer":
		value := protocolcommon.CanonicalInt64(strconv.FormatInt(input.Integer, 10))
		result.Integer = &value
	case "string_set":
		value := append([]string{}, input.StringSet...)
		result.StringSet = &value
	case "attributes":
		values := make([]semantic.MatrixAttributeItem, len(input.Attributes))
		for index, item := range input.Attributes {
			scalar, err := mapEngineScalar(item.Value)
			if err != nil {
				return semantic.MatrixDisplayValue{}, err
			}
			values[index] = semantic.MatrixAttributeItem{
				RelationAddress: semantic.RelationAddress(item.RelationAddress),
				RowAddress:      semantic.StableAddress(item.RowAddress),
				ColumnAddress:   semantic.ColumnAddress(item.ColumnAddress),
				Value:           scalar,
			}
		}
		result.Attributes = &values
	default:
		return semantic.MatrixDisplayValue{}, fmt.Errorf("unsupported Matrix display kind %q", input.Kind)
	}
	return result, nil
}

func mapTreeViewData(ctx context.Context, input engine.TreeViewData) (semantic.TreeViewData, error) {
	roots, err := mapTreeOccurrences(ctx, input.Roots)
	if err != nil {
		return semantic.TreeViewData{}, err
	}
	cycleRefs, err := mapTreeRefs(ctx, input.CycleRefs)
	if err != nil {
		return semantic.TreeViewData{}, err
	}
	linkRefs, err := mapTreeRefs(ctx, input.LinkRefs)
	if err != nil {
		return semantic.TreeViewData{}, err
	}
	return semantic.TreeViewData{Roots: roots, CycleRefs: cycleRefs, LinkRefs: linkRefs}, nil
}

func mapTreeOccurrences(ctx context.Context, input []engine.TreeOccurrence) ([]semantic.TreeOccurrence, error) {
	result := make([]semantic.TreeOccurrence, len(input))
	type frame struct {
		input  *engine.TreeOccurrence
		output *semantic.TreeOccurrence
		depth  int
	}
	stack := make([]frame, 0, len(input))
	for index := range input {
		stack = append(stack, frame{input: &input[index], output: &result[index], depth: 1})
	}
	for len(stack) != 0 {
		last := len(stack) - 1
		current := stack[last]
		stack = stack[:last]
		if current.depth > protocolcommon.MaxWireJSONDepth {
			return nil, fmt.Errorf("Tree ViewData exceeds maximum protocol depth")
		}
		source, err := mapViewDataSourceRefs(ctx, current.input.Source)
		if err != nil {
			return nil, err
		}
		*current.output = semantic.TreeOccurrence{
			Key:                semantic.ViewDataItemKey(current.input.Key),
			EntityAddress:      semantic.EntityAddress(current.input.EntityAddress),
			ViaRelationAddress: optionalTypedStringPointer[semantic.RelationAddress](current.input.ViaRelationAddress),
			Children:           make([]semantic.TreeOccurrence, len(current.input.Children)),
			Source:             source,
		}
		for index := len(current.input.Children) - 1; index >= 0; index-- {
			stack = append(stack, frame{
				input:  &current.input.Children[index],
				output: &current.output.Children[index],
				depth:  current.depth + 1,
			})
		}
	}
	return result, nil
}

func mapTreeRefs(ctx context.Context, input []engine.TreeRef) ([]semantic.TreeRef, error) {
	result := make([]semantic.TreeRef, len(input))
	for index, value := range input {
		source, err := mapViewDataSourceRefs(ctx, value.Source)
		if err != nil {
			return nil, err
		}
		result[index] = semantic.TreeRef{
			Key:               semantic.ViewDataItemKey(value.Key),
			FromOccurrenceKey: semantic.ViewDataItemKey(value.FromOccurrenceKey),
			ToEntityAddress:   semantic.EntityAddress(value.ToEntityAddress),
			RelationAddress:   semantic.RelationAddress(value.RelationAddress),
			Source:            source,
		}
	}
	return result, nil
}

func mapFlowViewData(ctx context.Context, input engine.FlowViewData) (semantic.FlowViewData, error) {
	result := semantic.FlowViewData{
		Lanes:      make([]semantic.FlowLane, len(input.Lanes)),
		Steps:      make([]semantic.FlowStep, len(input.Steps)),
		Connectors: make([]semantic.FlowConnector, len(input.Connectors)),
		CycleRefs:  make([]semantic.FlowCycleRef, len(input.CycleRefs)),
	}
	for index, value := range input.Lanes {
		source, err := mapViewDataSourceRefs(ctx, value.Source)
		if err != nil {
			return semantic.FlowViewData{}, err
		}
		result.Lanes[index] = semantic.FlowLane{
			Key: semantic.ViewDataItemKey(value.Key), Label: value.Label,
			StepKeys: protocolSlice[semantic.ViewDataItemKey](value.StepKeys), Source: source,
		}
	}
	for index, value := range input.Steps {
		source, err := mapViewDataSourceRefs(ctx, value.Source)
		if err != nil {
			return semantic.FlowViewData{}, err
		}
		result.Steps[index] = semantic.FlowStep{
			Key: semantic.ViewDataItemKey(value.Key), EntityAddress: semantic.EntityAddress(value.EntityAddress),
			LaneKey: semantic.ViewDataItemKey(value.LaneKey), Branch: value.Branch, Join: value.Join, Source: source,
		}
	}
	for index, value := range input.Connectors {
		mapped, err := mapFlowConnector(ctx, value)
		if err != nil {
			return semantic.FlowViewData{}, err
		}
		result.Connectors[index] = mapped
	}
	for index, value := range input.CycleRefs {
		mapped, err := mapFlowCycleRef(ctx, value)
		if err != nil {
			return semantic.FlowViewData{}, err
		}
		result.CycleRefs[index] = mapped
	}
	return result, nil
}

func mapFlowConnector(ctx context.Context, input engine.FlowConnector) (semantic.FlowConnector, error) {
	source, err := mapViewDataSourceRefs(ctx, input.Source)
	if err != nil {
		return semantic.FlowConnector{}, err
	}
	result := semantic.FlowConnector{
		Key: semantic.ViewDataItemKey(input.Key), FromStepKey: semantic.ViewDataItemKey(input.FromStepKey),
		ToStepKey: semantic.ViewDataItemKey(input.ToStepKey), Kind: semantic.FlowConnectorKind(input.Kind),
		BranchRowAddresses: protocolSlice[semantic.StableAddress](input.BranchRowAddresses),
		RelationAddresses:  protocolSlice[semantic.RelationAddress](input.RelationAddresses), Source: source,
	}
	if input.BranchValue != nil {
		value, err := mapEngineScalar(*input.BranchValue)
		if err != nil {
			return semantic.FlowConnector{}, err
		}
		result.BranchValue = &value
	}
	return result, nil
}

func mapFlowCycleRef(ctx context.Context, input engine.FlowCycleRef) (semantic.FlowCycleRef, error) {
	source, err := mapViewDataSourceRefs(ctx, input.Source)
	if err != nil {
		return semantic.FlowCycleRef{}, err
	}
	result := semantic.FlowCycleRef{
		Key: semantic.ViewDataItemKey(input.Key), ConnectorKey: semantic.ViewDataItemKey(input.ConnectorKey),
		FromStepKey: semantic.ViewDataItemKey(input.FromStepKey), ToStepKey: semantic.ViewDataItemKey(input.ToStepKey),
		Kind: semantic.FlowConnectorKind(input.Kind), BranchRowAddresses: protocolSlice[semantic.StableAddress](input.BranchRowAddresses),
		RelationAddresses: protocolSlice[semantic.RelationAddress](input.RelationAddresses), Source: source,
	}
	if input.BranchValue != nil {
		value, err := mapEngineScalar(*input.BranchValue)
		if err != nil {
			return semantic.FlowCycleRef{}, err
		}
		result.BranchValue = &value
	}
	return result, nil
}

func mapContextViewData(ctx context.Context, input engine.ContextViewData) (semantic.ContextViewData, error) {
	result := semantic.ContextViewData{Groups: make([]semantic.ContextGroup, len(input.Groups))}
	for index, value := range input.Groups {
		source, err := mapViewDataSourceRefs(ctx, value.Source)
		if err != nil {
			return semantic.ContextViewData{}, err
		}
		group := semantic.ContextGroup{
			Key:        semantic.ViewDataItemKey(value.Key),
			Label:      value.Label,
			Facts:      make([]semantic.ContextFact, len(value.Facts)),
			Attributes: make([]semantic.ContextAttribute, len(value.Attributes)),
			Source:     source,
		}
		for factIndex, fact := range value.Facts {
			factSource, err := mapViewDataSourceRefs(ctx, fact.Source)
			if err != nil {
				return semantic.ContextViewData{}, err
			}
			group.Facts[factIndex] = semantic.ContextFact{
				Key:             semantic.ViewDataItemKey(fact.Key),
				Direction:       string(fact.Direction),
				Text:            fact.Text,
				EntityAddress:   semantic.EntityAddress(fact.EntityAddress),
				RelationAddress: semantic.RelationAddress(fact.RelationAddress),
				RowAddresses:    protocolSlice[semantic.StableAddress](fact.RowAddresses),
				Source:          factSource,
			}
		}
		for attributeIndex, attribute := range value.Attributes {
			attributeSource, err := mapViewDataSourceRefs(ctx, attribute.Source)
			if err != nil {
				return semantic.ContextViewData{}, err
			}
			values := make(map[string]semantic.RecipeScalar, len(attribute.Values))
			for address, scalar := range attribute.Values {
				mapped, err := mapEngineScalar(scalar)
				if err != nil {
					return semantic.ContextViewData{}, err
				}
				values[address] = mapped
			}
			group.Attributes[attributeIndex] = semantic.ContextAttribute{
				Key:          semantic.ViewDataItemKey(attribute.Key),
				GroupKey:     semantic.ViewDataItemKey(attribute.GroupKey),
				OwnerAddress: semantic.StableAddress(attribute.OwnerAddress),
				RowAddress:   semantic.StableAddress(attribute.RowAddress),
				Values:       values,
				Source:       attributeSource,
			}
		}
		result.Groups[index] = group
	}
	return result, nil
}

func mapEngineScalar(input engine.TypedScalar) (semantic.RecipeScalar, error) {
	return mapRecipeScalar(materialize.Scalar{
		Type: input.Type, String: input.String, Int: input.Int, Float: input.Float, Bool: input.Bool,
	})
}

func mapDiffViewData(ctx context.Context, input engine.DiffViewData) (semantic.DiffViewData, error) {
	result := semantic.DiffViewData{Changes: make([]semantic.DiffChange, len(input.Changes))}
	for index, value := range input.Changes {
		source, err := mapViewDataSourceRefs(ctx, value.Source)
		if err != nil {
			return semantic.DiffViewData{}, err
		}
		change := semantic.DiffChange{
			Key:         semantic.ViewDataItemKey(value.Key),
			Kind:        string(value.Kind),
			SubjectKind: semantic.SubjectKind(value.SubjectKind),
			Fields:      make([]semantic.FieldDiff, len(value.Fields)),
			Source:      source,
		}
		change.BeforeAddress = optionalTypedStringPointer[semantic.StableAddress](value.BeforeAddress)
		change.AfterAddress = optionalTypedStringPointer[semantic.StableAddress](value.AfterAddress)
		if value.BeforeSource != nil {
			mapped, err := mapViewDataSourceRefs(ctx, *value.BeforeSource)
			if err != nil {
				return semantic.DiffViewData{}, err
			}
			change.BeforeSource = &mapped
		}
		if value.AfterSource != nil {
			mapped, err := mapViewDataSourceRefs(ctx, *value.AfterSource)
			if err != nil {
				return semantic.DiffViewData{}, err
			}
			change.AfterSource = &mapped
		}
		for fieldIndex, field := range value.Fields {
			mapped := semantic.FieldDiff{
				Key:           semantic.ViewDataItemKey(field.Key),
				Path:          append([]string{}, field.Path...),
				BeforePresent: field.BeforePresent,
				AfterPresent:  field.AfterPresent,
			}
			if field.Before != nil {
				before, err := mapViewDataSemanticValue(*field.Before, 0)
				if err != nil {
					return semantic.DiffViewData{}, err
				}
				mapped.Before = &before
			}
			if field.After != nil {
				after, err := mapViewDataSemanticValue(*field.After, 0)
				if err != nil {
					return semantic.DiffViewData{}, err
				}
				mapped.After = &after
			}
			change.Fields[fieldIndex] = mapped
		}
		result.Changes[index] = change
	}
	return result, nil
}

func mapViewDataSemanticValue(input engine.SemanticValue, depth int) (semantic.ViewDataSemanticValue, error) {
	if depth > protocolcommon.MaxWireJSONDepth {
		return semantic.ViewDataSemanticValue{}, fmt.Errorf("Diff semantic value exceeds maximum protocol depth")
	}
	result := semantic.ViewDataSemanticValue{Kind: string(input.Kind)}
	switch input.Kind {
	case engine.SemanticValueAbsent:
	case engine.SemanticValueAddress:
		value := semantic.StableAddress(input.Address)
		result.Address = &value
	case engine.SemanticValueArray:
		values := make([]semantic.ViewDataSemanticValue, len(input.Array))
		for index, item := range input.Array {
			mapped, err := mapViewDataSemanticValue(item, depth+1)
			if err != nil {
				return semantic.ViewDataSemanticValue{}, err
			}
			values[index] = mapped
		}
		result.Array = &values
	case engine.SemanticValueBlob:
		if input.BlobRef == nil {
			return semantic.ViewDataSemanticValue{}, fmt.Errorf("Diff blob value lacks BlobRef")
		}
		value := protocolcommon.BlobRef{
			BlobID: input.BlobRef.BlobID, Digest: protocolcommon.Digest(input.BlobRef.Digest),
			Lifetime: protocolcommon.BlobLifetime(input.BlobRef.Lifetime), MediaType: input.BlobRef.MediaType,
			Size: protocolcommon.CanonicalUint64(strconv.FormatUint(input.BlobRef.Size, 10)),
		}
		result.Blob = &value
	case engine.SemanticValueBoolean:
		value := input.Boolean
		result.Boolean = &value
	case engine.SemanticValueDecimal:
		value := semantic.CanonicalFiniteDecimal(input.Decimal)
		result.Decimal = &value
	case engine.SemanticValueInteger:
		value := protocolcommon.CanonicalInt64(strconv.FormatInt(input.Integer, 10))
		result.Integer = &value
	case engine.SemanticValueMap:
		values := make([]semantic.ViewDataSemanticMapEntry, len(input.Map))
		for index, item := range input.Map {
			mapped, err := mapViewDataSemanticValue(item.Value, depth+1)
			if err != nil {
				return semantic.ViewDataSemanticValue{}, err
			}
			values[index] = semantic.ViewDataSemanticMapEntry{Key: item.Key, Value: mapped}
		}
		result.Map = &values
	case engine.SemanticValueString:
		value := input.String
		result.String = &value
	case engine.SemanticValueToken:
		value := input.String
		result.Token = &value
	default:
		return semantic.ViewDataSemanticValue{}, fmt.Errorf("unsupported Diff semantic value kind %q", input.Kind)
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
