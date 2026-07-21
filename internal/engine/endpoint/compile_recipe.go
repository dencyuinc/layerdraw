// SPDX-License-Identifier: LicenseRef-LayerDraw-1.0

package endpoint

import (
	"crypto/sha256"
	"fmt"
	"math"
	"slices"
	"strconv"
	"strings"

	"github.com/dencyuinc/layerdraw/gen/go/engineprotocol"
	"github.com/dencyuinc/layerdraw/gen/go/protocolcommon"
	"github.com/dencyuinc/layerdraw/gen/go/semantic"
	"github.com/dencyuinc/layerdraw/internal/engine"
	"github.com/dencyuinc/layerdraw/internal/engine/internal/compiler/exportrecipe"
	"github.com/dencyuinc/layerdraw/internal/engine/internal/compiler/materialize"
	"github.com/dencyuinc/layerdraw/internal/engine/internal/compiler/query"
	viewcompiler "github.com/dencyuinc/layerdraw/internal/engine/internal/compiler/view"
)

const maxSafeInteger = int64(9_007_199_254_740_991)

func mapCompiledRecipes(snapshot engine.Snapshot) (engineprotocol.CompiledRecipes, []OutputBlob, error) {
	return mapCompiledRecipesWithBudget(snapshot, newCompileMappingBudget(math.MaxInt64))
}

func mapCompiledRecipesWithBudget(snapshot engine.Snapshot, budget *compileMappingBudget) (engineprotocol.CompiledRecipes, []OutputBlob, error) {
	queries, views, err := normalizedRecipeSets(snapshot)
	if err != nil {
		return engineprotocol.CompiledRecipes{}, nil, err
	}
	normalizedExportCount := 0
	for _, value := range views {
		normalizedExportCount += len(value.Exports)
	}
	if len(queries) != len(snapshot.CompiledQueryRecipes) || len(views) != len(snapshot.CompiledViewRecipes) || normalizedExportCount != len(snapshot.CompiledExportRecipes) {
		return engineprotocol.CompiledRecipes{}, nil, fmt.Errorf("compiled recipe sets do not match normalized counterparts")
	}
	for _, compiled := range snapshot.CompiledQueryRecipes {
		minimum := engineprotocol.CompiledQueryRecipeArtifact{Address: semantic.QueryAddress(compiled.Address)}
		if err := budget.claim(minimum); err != nil {
			return engineprotocol.CompiledRecipes{}, nil, err
		}
	}
	for _, compiled := range snapshot.CompiledViewRecipes {
		minimum := engineprotocol.CompiledViewRecipeArtifact{Address: semantic.ViewAddress(compiled.Address)}
		if err := budget.claim(minimum); err != nil {
			return engineprotocol.CompiledRecipes{}, nil, err
		}
	}
	for _, compiled := range snapshot.CompiledExportRecipes {
		minimum := engineprotocol.CompiledExportRecipeArtifact{Address: semantic.ViewExportAddress(compiled.Address)}
		if err := budget.claim(minimum); err != nil {
			return engineprotocol.CompiledRecipes{}, nil, err
		}
	}
	if err := uniqueRecipeAddresses(queries, views, snapshot.CompiledQueryRecipes, snapshot.CompiledViewRecipes, snapshot.CompiledExportRecipes); err != nil {
		return engineprotocol.CompiledRecipes{}, nil, err
	}

	queryByAddress := make(map[string]materialize.Query, len(queries))
	for _, value := range queries {
		queryByAddress[value.Address] = value
	}
	viewByAddress := make(map[string]materialize.View, len(views))
	exportByAddress := make(map[string]materialize.ExportRecipe)
	for _, value := range views {
		viewByAddress[value.Address] = value
		for _, recipe := range value.Exports {
			exportByAddress[recipe.Address] = recipe
		}
	}
	compiledExportByAddress := make(map[string]exportrecipe.Recipe, len(snapshot.CompiledExportRecipes))
	for _, value := range snapshot.CompiledExportRecipes {
		compiledExportByAddress[value.Address] = value
	}

	result := engineprotocol.CompiledRecipes{
		Queries: make([]engineprotocol.CompiledQueryRecipeArtifact, 0, min(len(snapshot.CompiledQueryRecipes), 256)),
		Views:   make([]engineprotocol.CompiledViewRecipeArtifact, 0, min(len(snapshot.CompiledViewRecipes), 256)),
		Exports: make([]engineprotocol.CompiledExportRecipeArtifact, 0, min(len(snapshot.CompiledExportRecipes), 256)),
	}
	blobs := make([]OutputBlob, 0, min(len(snapshot.CompiledQueryRecipes)+len(snapshot.CompiledViewRecipes)+len(snapshot.CompiledExportRecipes), 256))
	for _, compiled := range snapshot.CompiledQueryRecipes {
		normalized, found := queryByAddress[compiled.Address]
		if !found {
			return engineprotocol.CompiledRecipes{}, nil, fmt.Errorf("compiled Query lacks normalized counterpart")
		}
		recipe, err := mapQueryRecipe(normalized, compiled)
		if err != nil {
			return engineprotocol.CompiledRecipes{}, nil, err
		}
		document := semantic.CompiledQueryRecipeDocument{Format: semantic.CompiledQueryRecipeDocumentFormatValue, Recipe: recipe, SchemaVersion: 1}
		encoded, err := semantic.EncodeCompiledQueryRecipeDocument(document)
		if err != nil {
			return engineprotocol.CompiledRecipes{}, nil, fmt.Errorf("encode generated Query recipe: %w", err)
		}
		blob := newOutputBlob(recipeBlobID("query", compiled.Address), encoded)
		blob.Ref.MediaType = string(engineprotocol.QueryRecipeBlobRefMediaTypeValue)
		mapped := engineprotocol.CompiledQueryRecipeArtifact{Address: semantic.QueryAddress(compiled.Address), CanonicalJSON: queryRecipeRef(blob.Ref)}
		result.Queries = append(result.Queries, mapped)
		blobs = append(blobs, blob)
	}
	for _, compiled := range snapshot.CompiledViewRecipes {
		normalized, found := viewByAddress[compiled.Address]
		if !found {
			return engineprotocol.CompiledRecipes{}, nil, fmt.Errorf("compiled View lacks normalized counterpart")
		}
		recipe, err := mapViewRecipe(normalized, compiled, compiledExportByAddress)
		if err != nil {
			return engineprotocol.CompiledRecipes{}, nil, err
		}
		document := semantic.CompiledViewRecipeDocument{Format: semantic.CompiledViewRecipeDocumentFormatValue, Recipe: recipe, SchemaVersion: 1}
		encoded, err := semantic.EncodeCompiledViewRecipeDocument(document)
		if err != nil {
			return engineprotocol.CompiledRecipes{}, nil, fmt.Errorf("encode generated View recipe: %w", err)
		}
		blob := newOutputBlob(recipeBlobID("view", compiled.Address), encoded)
		blob.Ref.MediaType = string(engineprotocol.ViewRecipeBlobRefMediaTypeValue)
		mapped := engineprotocol.CompiledViewRecipeArtifact{Address: semantic.ViewAddress(compiled.Address), CanonicalJSON: viewRecipeRef(blob.Ref)}
		result.Views = append(result.Views, mapped)
		blobs = append(blobs, blob)
	}
	for _, compiled := range snapshot.CompiledExportRecipes {
		normalized, found := exportByAddress[compiled.Address]
		if !found {
			return engineprotocol.CompiledRecipes{}, nil, fmt.Errorf("compiled Export lacks normalized counterpart")
		}
		recipe, err := mapExportRecipe(normalized, compiled)
		if err != nil {
			return engineprotocol.CompiledRecipes{}, nil, err
		}
		document := semantic.CompiledExportRecipeDocument{Format: semantic.CompiledExportRecipeDocumentFormatValue, Recipe: recipe, SchemaVersion: 1}
		encoded, err := semantic.EncodeCompiledExportRecipeDocument(document)
		if err != nil {
			return engineprotocol.CompiledRecipes{}, nil, fmt.Errorf("encode generated Export recipe: %w", err)
		}
		blob := newOutputBlob(recipeBlobID("export", compiled.Address), encoded)
		blob.Ref.MediaType = string(engineprotocol.ExportRecipeBlobRefMediaTypeValue)
		mapped := engineprotocol.CompiledExportRecipeArtifact{Address: semantic.ViewExportAddress(compiled.Address), CanonicalJSON: exportRecipeRef(blob.Ref)}
		result.Exports = append(result.Exports, mapped)
		blobs = append(blobs, blob)
	}
	return result, blobs, nil
}

func normalizedRecipeSets(snapshot engine.Snapshot) ([]materialize.Query, []materialize.View, error) {
	switch snapshot.Mode {
	case engine.CompileProject:
		if snapshot.NormalizedDocument == nil {
			return nil, nil, fmt.Errorf("missing normalized Project")
		}
		return snapshot.NormalizedDocument.Queries, snapshot.NormalizedDocument.Views, nil
	case engine.CompilePack:
		if snapshot.NormalizedPackArtifact == nil {
			return nil, nil, fmt.Errorf("missing normalized Pack")
		}
		return snapshot.NormalizedPackArtifact.Queries, snapshot.NormalizedPackArtifact.Views, nil
	default:
		return nil, nil, fmt.Errorf("invalid recipe mode")
	}
}

func uniqueRecipeAddresses(queries []materialize.Query, views []materialize.View, compiledQueries []engine.CompiledQueryRecipe, compiledViews []engine.CompiledViewRecipe, compiledExports []engine.CompiledExportRecipe) error {
	sets := [][]string{
		make([]string, len(queries)), make([]string, len(views)), make([]string, len(compiledQueries)), make([]string, len(compiledViews)), make([]string, len(compiledExports)),
	}
	for i, value := range queries {
		sets[0][i] = value.Address
	}
	for i, value := range views {
		sets[1][i] = value.Address
	}
	for i, value := range compiledQueries {
		sets[2][i] = value.Address
	}
	for i, value := range compiledViews {
		sets[3][i] = value.Address
	}
	for i, value := range compiledExports {
		sets[4][i] = value.Address
	}
	normalizedExports := make([]string, 0)
	for _, value := range views {
		for _, recipe := range value.Exports {
			normalizedExports = append(normalizedExports, recipe.Address)
		}
	}
	sets = append(sets, normalizedExports)
	for _, values := range sets {
		ordered := slices.Clone(values)
		slices.Sort(ordered)
		for i := 1; i < len(ordered); i++ {
			if ordered[i] == ordered[i-1] {
				return fmt.Errorf("duplicate recipe address")
			}
		}
	}
	return nil
}

func mapQueryRecipe(input materialize.Query, compiled engine.CompiledQueryRecipe) (semantic.QueryRecipe, error) {
	if input.Address != compiled.Address {
		return semantic.QueryRecipe{}, fmt.Errorf("Query recipe generation mismatch")
	}
	result := semantic.QueryRecipe{
		ID: semantic.LocalIdentifier(input.ID), Address: semantic.QueryAddress(input.Address), DisplayName: input.DisplayName,
		Tags: cloneStrings(input.Tags), Annotations: cloneStringMap(input.Annotations), StateInput: string(input.StateInput),
		Parameters: make([]semantic.QueryRecipeParameter, len(input.Parameters)), Select: mapQuerySelect(input.Select),
		ReservedParameterIDs: typedStrings[semantic.LocalIdentifier](input.ReservedParameterIDs), Result: make([]string, len(input.Result)),
		Dependencies: semantic.QueryRecipeDependencies{
			LayerAddresses: typedStrings[semantic.LayerAddress](compiled.Dependencies.LayerAddresses), EntityTypeAddresses: typedStrings[semantic.EntityTypeAddress](compiled.Dependencies.EntityTypeAddresses), RelationTypeAddresses: typedStrings[semantic.RelationTypeAddress](compiled.Dependencies.RelationTypeAddresses),
			EntityAddresses: typedStrings[semantic.EntityAddress](compiled.Dependencies.EntityAddresses), RelationAddresses: typedStrings[semantic.RelationAddress](compiled.Dependencies.RelationAddresses), ColumnAddresses: typedStrings[semantic.ColumnAddress](compiled.Dependencies.ColumnAddresses), ParameterAddresses: typedStrings[semantic.ParameterAddress](compiled.Dependencies.ParameterAddresses), StateReads: mapStateReads(compiled.Dependencies.StateReads),
		},
	}
	result.Description = cloneStringPointer(input.Description)
	var err error
	result.Where, err = mapRecipePredicate(compiled.Where)
	if err != nil {
		return semantic.QueryRecipe{}, err
	}
	result.RelationWhere, err = mapRecipePredicate(compiled.RelationWhere)
	if err != nil {
		return semantic.QueryRecipe{}, err
	}
	for i, value := range input.Result {
		result.Result[i] = string(value)
	}
	for i, parameter := range input.Parameters {
		mapped, err := mapQueryParameter(parameter)
		if err != nil {
			return semantic.QueryRecipe{}, err
		}
		result.Parameters[i] = mapped
	}
	if compiled.Traversal != nil {
		minDepth, err := nonNegativeSafe(compiled.Traversal.MinDepth)
		if err != nil {
			return semantic.QueryRecipe{}, err
		}
		maxDepth, err := nonNegativeSafe(compiled.Traversal.MaxDepth)
		if err != nil {
			return semantic.QueryRecipe{}, err
		}
		result.Traverse = &semantic.QueryRecipeTraversal{Direction: string(compiled.Traversal.Direction), MinDepth: minDepth, MaxDepth: maxDepth, CyclePolicy: string(compiled.Traversal.CyclePolicy), RelationTypeAddresses: typedStringSlicePointer[semantic.RelationTypeAddress](compiled.Traversal.RelationTypeAddresses)}
	}
	return result, nil
}

func mapQueryParameter(input materialize.QueryParameter) (semantic.QueryRecipeParameter, error) {
	result := semantic.QueryRecipeParameter{ID: semantic.LocalIdentifier(input.ID), Address: semantic.ParameterAddress(input.Address), ValueType: semantic.ValueType(input.ValueType), Required: input.Required, ReservedEnumValues: sortedStrings(input.ReservedEnumValues)}
	if input.EnumValues != nil {
		values := sortedStrings(input.EnumValues)
		result.EnumValues = &values
	}
	if input.Default != nil {
		value, err := mapRecipeScalar(*input.Default)
		if err != nil {
			return semantic.QueryRecipeParameter{}, err
		}
		result.Default = &value
	}
	if input.Format != nil {
		value := semantic.StringFormat(*input.Format)
		result.Format = &value
	}
	if input.Min != nil {
		value, err := finiteDecimal(*input.Min, false)
		if err != nil {
			return semantic.QueryRecipeParameter{}, err
		}
		result.Min = &value
	}
	if input.Max != nil {
		value, err := finiteDecimal(*input.Max, false)
		if err != nil {
			return semantic.QueryRecipeParameter{}, err
		}
		result.Max = &value
	}
	if input.MinLength != nil {
		value, err := nonNegativeSafe(*input.MinLength)
		if err != nil {
			return semantic.QueryRecipeParameter{}, err
		}
		result.MinLength = &value
	}
	if input.MaxLength != nil {
		value, err := nonNegativeSafe(*input.MaxLength)
		if err != nil {
			return semantic.QueryRecipeParameter{}, err
		}
		result.MaxLength = &value
	}
	return result, nil
}

func mapQuerySelect(input materialize.QuerySelect) semantic.QueryRecipeSelect {
	return semantic.QueryRecipeSelect{LayerAddresses: typedStringSlicePointer[semantic.LayerAddress](input.LayerAddresses), EntityTypeAddresses: typedStringSlicePointer[semantic.EntityTypeAddress](input.EntityTypeAddresses), RelationTypeAddresses: typedStringSlicePointer[semantic.RelationTypeAddress](input.RelationTypeAddresses), RootAddresses: typedStringSlicePointer[semantic.EntityAddress](input.RootAddresses)}
}

func mapRecipePredicate(input query.Predicate) (semantic.RecipePredicate, error) {
	result := semantic.RecipePredicate{Kind: string(input.Kind)}
	switch input.Kind {
	case query.PredicateAll, query.PredicateAny:
		children := make([]semantic.RecipePredicate, len(input.Children))
		for i, child := range input.Children {
			mapped, err := mapRecipePredicate(child)
			if err != nil {
				return semantic.RecipePredicate{}, err
			}
			children[i] = mapped
		}
		result.Children = &children
	case query.PredicateNot:
		if input.Child == nil {
			return semantic.RecipePredicate{}, fmt.Errorf("not predicate lacks child")
		}
		child, err := mapRecipePredicate(*input.Child)
		if err != nil {
			return semantic.RecipePredicate{}, err
		}
		result.Child = &child
	case query.PredicateField:
		result.Field = stringPointer(input.Field)
		result.Operator = stringPointer(string(input.Operator))
		operand, err := mapRecipeOperandType(input.OperandType)
		if err != nil {
			return semantic.RecipePredicate{}, err
		}
		result.OperandType = &operand
		value, err := mapOptionalPredicateValue(input.Value)
		if err != nil {
			return semantic.RecipePredicate{}, err
		}
		result.Value = value
	case query.PredicateState:
		result.FieldPath = typedStringPointer[semantic.StateFieldPath](string(input.FieldPath))
		result.Operator = stringPointer(string(input.Operator))
		operand, err := mapRecipeOperandType(input.OperandType)
		if err != nil {
			return semantic.RecipePredicate{}, err
		}
		result.OperandType = &operand
		value, err := mapOptionalPredicateValue(input.Value)
		if err != nil {
			return semantic.RecipePredicate{}, err
		}
		result.Value = value
	case query.PredicateRows:
		if input.Row == nil {
			return semantic.RecipePredicate{}, fmt.Errorf("rows predicate lacks row predicate")
		}
		predicate, err := mapRowPredicate(*input.Row)
		if err != nil {
			return semantic.RecipePredicate{}, err
		}
		addresses := typedStrings[semantic.EntityOrRelationTypeAddress](input.TypeAddresses)
		result.Predicate = &predicate
		result.Quantifier = stringPointer(string(input.Quantifier))
		result.TypeAddresses = &addresses
	default:
		return semantic.RecipePredicate{}, fmt.Errorf("unsupported Query predicate kind")
	}
	return result, nil
}

func mapRowPredicate(input query.RowPredicate) (semantic.RecipeRowPredicate, error) {
	result := semantic.RecipeRowPredicate{Kind: string(input.Kind)}
	switch input.Kind {
	case query.PredicateAll, query.PredicateAny:
		children := make([]semantic.RecipeRowPredicate, len(input.Children))
		for i, child := range input.Children {
			mapped, err := mapRowPredicate(child)
			if err != nil {
				return semantic.RecipeRowPredicate{}, err
			}
			children[i] = mapped
		}
		result.Children = &children
	case query.PredicateNot:
		if input.Child == nil {
			return semantic.RecipeRowPredicate{}, fmt.Errorf("row not predicate lacks child")
		}
		child, err := mapRowPredicate(*input.Child)
		if err != nil {
			return semantic.RecipeRowPredicate{}, err
		}
		result.Child = &child
	case query.PredicateCell:
		addresses := typedStrings[semantic.ColumnAddress](input.ColumnAddresses)
		result.ColumnAddresses = &addresses
		result.Operator = stringPointer(string(input.Operator))
		operand, err := mapRecipeOperandType(input.OperandType)
		if err != nil {
			return semantic.RecipeRowPredicate{}, err
		}
		result.OperandType = &operand
		value, err := mapOptionalPredicateValue(input.Value)
		if err != nil {
			return semantic.RecipeRowPredicate{}, err
		}
		result.Value = value
	case query.PredicateState:
		result.FieldPath = typedStringPointer[semantic.StateFieldPath](string(input.FieldPath))
		result.Operator = stringPointer(string(input.Operator))
		operand, err := mapRecipeOperandType(input.OperandType)
		if err != nil {
			return semantic.RecipeRowPredicate{}, err
		}
		result.OperandType = &operand
		value, err := mapOptionalPredicateValue(input.Value)
		if err != nil {
			return semantic.RecipeRowPredicate{}, err
		}
		result.Value = value
	default:
		return semantic.RecipeRowPredicate{}, fmt.Errorf("unsupported row predicate kind")
	}
	return result, nil
}

func mapRecipeOperandType(input query.OperandType) (semantic.RecipeOperandType, error) {
	result := semantic.RecipeOperandType{Kind: string(input.Kind)}
	switch input.Kind {
	case query.OperandAddress:
		result.AddressKind = typedStringPointer[semantic.SubjectKind](strings.ReplaceAll(string(input.AddressKind), "-", "_"))
	case query.OperandScalar:
		result.ScalarType = typedStringPointer[semantic.ValueType](string(input.ScalarType))
	case query.OperandStringSet:
	default:
		return semantic.RecipeOperandType{}, fmt.Errorf("unsupported Query operand type")
	}
	return result, nil
}

func mapOptionalPredicateValue(input *query.PredicateValue) (*semantic.RecipePredicateValue, error) {
	if input == nil {
		return nil, nil
	}
	result := semantic.RecipePredicateValue{}
	if input.Kind == query.ValueParameter {
		result.Kind = "parameter"
		result.ParameterAddress = nonEmptyTypedStringPointer[semantic.ParameterAddress](input.ParameterAddress)
		return &result, nil
	}
	if input.Kind != query.ValueLiteral {
		return nil, fmt.Errorf("unsupported predicate value kind")
	}
	switch {
	case input.Scalar != nil:
		value, err := mapRecipeScalar(materialize.Scalar{Type: input.Scalar.Type, String: input.Scalar.String, Int: input.Scalar.Int, Float: input.Scalar.Float, Bool: input.Scalar.Bool})
		if err != nil {
			return nil, err
		}
		result.Kind = "scalar"
		result.ScalarValue = &value
	case input.Address != nil:
		result.Kind = "address"
		result.AddressValue = stableAddressPointer(*input.Address)
	case input.Scalars != nil:
		values := make([]semantic.RecipeScalar, len(input.Scalars))
		for i, item := range input.Scalars {
			value, err := mapRecipeScalar(materialize.Scalar{Type: item.Type, String: item.String, Int: item.Int, Float: item.Float, Bool: item.Bool})
			if err != nil {
				return nil, err
			}
			values[i] = value
		}
		result.Kind = "scalar_set"
		result.ScalarValues = &values
	case input.Addresses != nil:
		values := stableAddresses(input.Addresses)
		result.Kind = "address_set"
		result.AddressValues = &values
	default:
		return nil, fmt.Errorf("literal predicate lacks value")
	}
	return &result, nil
}

func mapRecipeScalar(input materialize.Scalar) (semantic.RecipeScalar, error) {
	result := semantic.RecipeScalar{Kind: string(input.Type)}
	switch string(input.Type) {
	case "string", "enum", "date", "datetime":
		result.StringValue = stringPointer(input.String)
	case "integer":
		if input.Int < -maxSafeInteger || input.Int > maxSafeInteger {
			return semantic.RecipeScalar{}, fmt.Errorf("recipe integer exceeds safe range")
		}
		value := protocolcommon.CanonicalSafeInteger(strconv.FormatInt(input.Int, 10))
		result.IntegerValue = &value
	case "number":
		value, err := finiteDecimal(input.Float, false)
		if err != nil {
			return semantic.RecipeScalar{}, err
		}
		result.NumberValue = &value
	case "boolean":
		value := input.Bool
		result.BooleanValue = &value
	default:
		return semantic.RecipeScalar{}, fmt.Errorf("unsupported recipe scalar")
	}
	return result, nil
}

func mapViewRecipe(input materialize.View, compiled engine.CompiledViewRecipe, exports map[string]exportrecipe.Recipe) (semantic.ViewRecipe, error) {
	if input.Address != compiled.Address {
		return semantic.ViewRecipe{}, fmt.Errorf("View recipe generation mismatch")
	}
	result := semantic.ViewRecipe{
		ID: semantic.LocalIdentifier(input.ID), Address: semantic.ViewAddress(input.Address), DisplayName: input.DisplayName, Tags: cloneStrings(input.Tags), Annotations: cloneStringMap(input.Annotations), Category: string(input.Category), Intent: cloneStringPointer(input.Intent),
		StateInput: string(input.StateInput), StateRequirement: string(compiled.StateRequirement), RelationProjectionOverrides: make(map[string]semantic.ViewProjectionOverride, len(input.RelationProjections)),
		ReservedTableColumnIDs: typedStrings[semantic.LocalIdentifier](input.ReservedTableColumnIDs), ReservedExportIDs: typedStrings[semantic.LocalIdentifier](input.ReservedExportIDs), Exports: make([]semantic.ExportRecipe, len(input.Exports)),
		Dependencies: semantic.ViewRecipeDependencies{QueryAddresses: typedStrings[semantic.QueryAddress](compiled.Dependencies.QueryAddresses), ParameterAddresses: typedStrings[semantic.ParameterAddress](compiled.Dependencies.ParameterAddresses), LayerAddresses: typedStrings[semantic.LayerAddress](compiled.Dependencies.LayerAddresses), EntityTypeAddresses: typedStrings[semantic.EntityTypeAddress](compiled.Dependencies.EntityTypeAddresses), RelationTypeAddresses: typedStrings[semantic.RelationTypeAddress](compiled.Dependencies.RelationTypeAddresses), EntityAddresses: typedStrings[semantic.EntityAddress](compiled.Dependencies.EntityAddresses), RelationAddresses: typedStrings[semantic.RelationAddress](compiled.Dependencies.RelationAddresses), ColumnAddresses: typedStrings[semantic.ColumnAddress](compiled.Dependencies.ColumnAddresses), ExportAddresses: typedStrings[semantic.ViewExportAddress](compiled.Dependencies.ExportAddresses), StateReads: mapStateReads(compiled.Dependencies.StateReads)},
	}
	result.Description = cloneStringPointer(input.Description)
	var err error
	result.Source, err = mapViewSource(input.Source)
	if err != nil {
		return semantic.ViewRecipe{}, err
	}
	for address, projection := range input.RelationProjections {
		mapped, err := mapProjectionOverride(projection)
		if err != nil {
			return semantic.ViewRecipe{}, err
		}
		result.RelationProjectionOverrides[address] = mapped
	}
	shape, err := mapViewShape(input.Shape)
	if err != nil {
		return semantic.ViewRecipe{}, err
	}
	if err := applyCompiledViewShape(&shape, compiled.Shape); err != nil {
		return semantic.ViewRecipe{}, err
	}
	result.Shape = shape
	for i, item := range input.Exports {
		compiledExport, found := exports[item.Address]
		if !found {
			return semantic.ViewRecipe{}, fmt.Errorf("nested Export lacks compiled counterpart")
		}
		mapped, err := mapExportRecipe(item, compiledExport)
		if err != nil {
			return semantic.ViewRecipe{}, err
		}
		result.Exports[i] = mapped
	}
	return result, nil
}

func mapViewSource(input materialize.ViewSource) (semantic.ViewRecipeSource, error) {
	result := semantic.ViewRecipeSource{Kind: string(input.Kind), Arguments: make(map[string]semantic.RecipeScalar, len(input.Arguments)), QueryAddress: optionalTypedStringPointer[semantic.QueryAddress](input.QueryAddress), Before: cloneStringPointer(input.Before), After: cloneStringPointer(input.After)}
	for key, value := range input.Arguments {
		mapped, err := mapRecipeScalar(value)
		if err != nil {
			return semantic.ViewRecipeSource{}, err
		}
		result.Arguments[key] = mapped
	}
	return result, nil
}

func mapProjectionOverride(input materialize.ProjectionOverride) (semantic.ViewProjectionOverride, error) {
	result := semantic.ViewProjectionOverride{}
	if value := input.Composed; value != nil {
		priority, err := safeInteger(value.Priority)
		if err != nil {
			return result, err
		}
		result.Composed = &semantic.ViewComposedProjection{Mode: string(value.Mode), Priority: priority, Conflict: string(value.Conflict), KeepEdge: value.KeepEdge, ParentEndpoint: stringifyPointer(value.ParentEndpoint), ChildEndpoint: stringifyPointer(value.ChildEndpoint), OverlayEndpoint: stringifyPointer(value.OverlayEndpoint), TargetEndpoint: stringifyPointer(value.TargetEndpoint), BadgeEndpoint: stringifyPointer(value.BadgeEndpoint)}
	}
	if value := input.Diagram; value != nil {
		result.Diagram = &semantic.ViewDiagramProjection{Mode: string(value.Mode), SourceEndpoint: string(value.SourceEndpoint), TargetEndpoint: string(value.TargetEndpoint), EdgeLabel: string(value.EdgeLabel), IncludeRelationType: value.IncludeRelationType}
	}
	if value := input.Table; value != nil {
		result.Table = &semantic.ViewTableProjection{RowMode: string(value.RowMode), IncludeFrom: value.IncludeFrom, IncludeTo: value.IncludeTo, IncludeRelationType: value.IncludeRelationType}
	}
	if value := input.Matrix; value != nil {
		result.Matrix = &semantic.ViewMatrixProjection{RowEndpoint: string(value.RowEndpoint), ColumnEndpoint: string(value.ColumnEndpoint), IncludeRelationRows: value.IncludeRelationRows}
	}
	if value := input.Tree; value != nil {
		result.Tree = &semantic.ViewTreeProjection{ParentEndpoint: string(value.ParentEndpoint), ChildEndpoint: string(value.ChildEndpoint)}
	}
	if value := input.Flow; value != nil {
		result.Flow = &semantic.ViewFlowProjection{SourceEndpoint: string(value.SourceEndpoint), TargetEndpoint: string(value.TargetEndpoint), ConnectorKind: string(value.ConnectorKind), BranchValueColumnAddress: optionalTypedStringPointer[semantic.ColumnAddress](value.BranchValueColumnAddress)}
	}
	if value := input.Context; value != nil {
		result.Context = &semantic.ViewContextProjection{FactTemplate: value.FactTemplate, ReverseFactTemplate: cloneStringPointer(value.ReverseFactTemplate), IncludeAttributeRows: value.IncludeAttributeRows}
	}
	if value := input.Render; value != nil {
		maxItems, err := positiveSafe(value.Overlay.MaxItems)
		if err != nil {
			return result, err
		}
		result.Render = &semantic.ViewRenderSet{EdgeArrow: string(value.Edge.Arrow), EdgeLine: string(value.Edge.Line), EdgeColor: optionalTypedStringPointer[semantic.Color](value.Edge.Color), EdgeLabel: string(value.Edge.Label), NestedFrameLabel: string(value.Nested.FrameLabel), NestedFrameStyle: string(value.Nested.FrameStyle), OverlayKind: value.Overlay.Kind, OverlayPosition: string(value.Overlay.Position), OverlayMaxItems: maxItems, BadgeIcon: cloneStringPointer(value.Badge.Icon), BadgeLabel: string(value.Badge.Label), BadgePosition: string(value.Badge.Position)}
	}
	return result, nil
}

func mapViewShape(input materialize.ViewShape) (semantic.ViewRecipeShape, error) {
	result := semantic.ViewRecipeShape{Kind: string(input.Kind)}
	if value := input.Diagram; value != nil {
		placements := make([]semantic.ViewPlacement, len(value.Placements))
		for i, item := range value.Placements {
			x, err := finiteDecimal(item.X, false)
			if err != nil {
				return result, err
			}
			y, err := finiteDecimal(item.Y, false)
			if err != nil {
				return result, err
			}
			width, err := finiteDecimal(item.Width, true)
			if err != nil {
				return result, err
			}
			height, err := finiteDecimal(item.Height, true)
			if err != nil {
				return result, err
			}
			placements[i] = semantic.ViewPlacement{EntityAddress: semantic.EntityAddress(item.EntityAddress), X: x, Y: y, Width: semantic.CanonicalPositiveFiniteDecimal(width), Height: semantic.CanonicalPositiveFiniteDecimal(height)}
		}
		result.Diagram = &semantic.ViewDiagramShape{Layout: string(value.Layout), Direction: string(value.Direction), Abstraction: string(value.Abstraction), Composed: value.Composed, Placements: placements}
	}
	if value := input.Table; value != nil {
		columns := make([]semantic.ViewTableColumn, len(value.Columns))
		for i, item := range value.Columns {
			columns[i] = semantic.ViewTableColumn{ID: semantic.LocalIdentifier(item.ID), Address: semantic.TableColumnAddress(item.Address), Label: cloneStringPointer(item.Label), Source: mapTableColumnSource(item.Source), Aggregate: string(item.Aggregate)}
		}
		sorts := make([]semantic.ViewTableSort, len(value.Sorts))
		for i, item := range value.Sorts {
			sorts[i] = semantic.ViewTableSort{ColumnID: item.ColumnID, Direction: string(item.Direction), Absent: string(item.Absent)}
		}
		result.Table = &semantic.ViewTableShape{RowSource: string(value.RowSource), AutomaticRelationColumns: sortedStrings(value.AutomaticRelationColumns), EntityTypeAddresses: typedStringSlicePointer[semantic.EntityTypeAddress](value.EntityTypeAddresses), IncludeEntityID: value.IncludeEntityID, IncludeType: value.IncludeType, IncludeLayer: value.IncludeLayer, Columns: columns, Sorts: sorts}
	}
	if value := input.Matrix; value != nil {
		result.Matrix = &semantic.ViewMatrixShape{RowAxis: mapMatrixAxis(value.RowAxis), ColumnAxis: mapMatrixAxis(value.ColumnAxis), Cell: semantic.ViewMatrixCell{RelationTypeAddresses: typedStringSlicePointer[semantic.RelationTypeAddress](value.Cell.RelationTypeAddresses), Direction: string(value.Cell.Direction), Semantic: string(value.Cell.Semantic), Display: string(value.Cell.Display), AttributeColumnAddresses: typedStringSlicePointer[semantic.RelationTypeColumnAddress](value.Cell.AttributeColumnAddresses)}}
	}
	if value := input.Tree; value != nil {
		result.Tree = &semantic.ViewTreeShape{RelationTypeAddresses: typedStrings[semantic.RelationTypeAddress](value.RelationTypeAddresses), CyclePolicy: string(value.CyclePolicy), SharedChildPolicy: string(value.SharedChildPolicy)}
	}
	if value := input.Flow; value != nil {
		result.Flow = &semantic.ViewFlowShape{RelationTypeAddresses: typedStrings[semantic.RelationTypeAddress](value.RelationTypeAddresses), LaneBy: string(value.LaneBy), LaneColumnAddresses: typedStringSlicePointer[semantic.EntityTypeColumnAddress](value.LaneColumnAddresses), CyclePolicy: string(value.CyclePolicy), PreserveParallel: value.PreserveParallel}
	}
	if value := input.Context; value != nil {
		result.Context = &semantic.ViewContextShape{GroupBy: string(value.GroupBy), IncludeEntityRows: value.IncludeEntityRows, IncludeRelationRows: value.IncludeRelationRows, Incoming: value.Incoming, Outgoing: value.Outgoing}
	}
	if value := input.Diff; value != nil {
		kinds := make([]semantic.SubjectKind, len(value.Include))
		for i, item := range value.Include {
			kinds[i] = semantic.SubjectKind(item)
		}
		result.Diff = &semantic.ViewDiffShape{Include: kinds, DetectMoves: value.DetectMoves}
	}
	return result, nil
}

// mapCompiledViewShape maps the canonical compiler shape already embedded in
// ViewData. It performs representation conversion only; shape semantics remain
// owned by the compiler and materializer.
func mapCompiledViewShape(input viewcompiler.Shape) (semantic.ViewRecipeShape, error) {
	result := semantic.ViewRecipeShape{Kind: string(input.Kind)}
	if value := input.Diagram; value != nil {
		placements := make([]semantic.ViewPlacement, len(value.Placements))
		for index, placement := range value.Placements {
			x, err := finiteDecimal(placement.X, false)
			if err != nil {
				return result, err
			}
			y, err := finiteDecimal(placement.Y, false)
			if err != nil {
				return result, err
			}
			width, err := finiteDecimal(placement.Width, true)
			if err != nil {
				return result, err
			}
			height, err := finiteDecimal(placement.Height, true)
			if err != nil {
				return result, err
			}
			placements[index] = semantic.ViewPlacement{
				EntityAddress: semantic.EntityAddress(placement.EntityAddress),
				X:             x,
				Y:             y,
				Width:         semantic.CanonicalPositiveFiniteDecimal(width),
				Height:        semantic.CanonicalPositiveFiniteDecimal(height),
			}
		}
		result.Diagram = &semantic.ViewDiagramShape{
			Layout:      string(value.Layout),
			Direction:   string(value.Direction),
			Abstraction: string(value.Abstraction),
			Composed:    value.Composed,
			Placements:  placements,
		}
	}
	if value := input.Table; value != nil {
		columns := make([]semantic.ViewTableColumn, len(value.Columns))
		for index, column := range value.Columns {
			valueType, err := mapTableValueType(column.ValueType)
			if err != nil {
				return result, err
			}
			columns[index] = semantic.ViewTableColumn{
				Address:   semantic.TableColumnAddress(column.Address),
				Aggregate: string(column.Aggregate),
				ID:        semantic.LocalIdentifier(column.ID),
				Label:     cloneStringPointer(column.Label),
				Source:    mapCompiledTableColumnSource(column.Source),
				ValueType: valueType,
			}
		}
		sorts := make([]semantic.ViewTableSort, len(value.Sorts))
		for index, item := range value.Sorts {
			sorts[index] = semantic.ViewTableSort{ColumnID: item.ColumnID, Direction: string(item.Direction), Absent: string(item.Absent)}
		}
		result.Table = &semantic.ViewTableShape{
			RowSource:                string(value.RowSource),
			AutomaticRelationColumns: cloneStrings(value.AutomaticRelationColumns),
			EntityTypeAddresses:      typedStringSlicePointer[semantic.EntityTypeAddress](value.EntityTypeAddresses),
			IncludeEntityID:          value.IncludeEntityID,
			IncludeType:              value.IncludeType,
			IncludeLayer:             value.IncludeLayer,
			Columns:                  columns,
			Sorts:                    sorts,
		}
	}
	if value := input.Matrix; value != nil {
		result.Matrix = &semantic.ViewMatrixShape{
			RowAxis:    mapCompiledMatrixAxis(value.RowAxis),
			ColumnAxis: mapCompiledMatrixAxis(value.ColumnAxis),
			Cell: semantic.ViewMatrixCell{
				RelationTypeAddresses:    typedStringSlicePointer[semantic.RelationTypeAddress](value.Cell.RelationTypeAddresses),
				Direction:                string(value.Cell.Direction),
				Semantic:                 string(value.Cell.Semantic),
				Display:                  string(value.Cell.Display),
				AttributeColumnAddresses: typedStringSlicePointer[semantic.RelationTypeColumnAddress](value.Cell.AttributeColumnAddresses),
			},
		}
	}
	if value := input.Tree; value != nil {
		result.Tree = &semantic.ViewTreeShape{
			RelationTypeAddresses: typedStrings[semantic.RelationTypeAddress](value.RelationTypeAddresses),
			CyclePolicy:           string(value.CyclePolicy),
			SharedChildPolicy:     string(value.SharedChildPolicy),
		}
	}
	if value := input.Flow; value != nil {
		result.Flow = &semantic.ViewFlowShape{
			RelationTypeAddresses: typedStrings[semantic.RelationTypeAddress](value.RelationTypeAddresses),
			LaneBy:                string(value.LaneBy),
			LaneColumnAddresses:   typedStringSlicePointer[semantic.EntityTypeColumnAddress](value.LaneColumnAddresses),
			CyclePolicy:           string(value.CyclePolicy),
			PreserveParallel:      value.PreserveParallel,
		}
	}
	if value := input.Context; value != nil {
		result.Context = &semantic.ViewContextShape{
			GroupBy:             string(value.GroupBy),
			IncludeEntityRows:   value.IncludeEntityRows,
			IncludeRelationRows: value.IncludeRelationRows,
			Incoming:            value.Incoming,
			Outgoing:            value.Outgoing,
		}
	}
	if value := input.Diff; value != nil {
		include := make([]semantic.SubjectKind, len(value.Include))
		for index, kind := range value.Include {
			include[index] = semantic.SubjectKind(kind)
		}
		result.Diff = &semantic.ViewDiffShape{Include: include, DetectMoves: value.DetectMoves}
	}
	return result, nil
}

func mapCompiledTableColumnSource(input viewcompiler.TableColumnSource) semantic.ViewTableColumnSource {
	result := semantic.ViewTableColumnSource{
		Kind:                  string(input.Kind),
		RelationTypeAddresses: typedStringSlicePointer[semantic.RelationTypeAddress](input.RelationTypeAddresses),
	}
	if input.Field != "" {
		result.Field = stringPointer(input.Field)
	}
	if len(input.ColumnAddresses) != 0 {
		values := typedStrings[semantic.ColumnAddress](input.ColumnAddresses)
		result.ColumnAddresses = &values
	}
	if input.Endpoint != "" {
		result.Endpoint = stringPointer(string(input.Endpoint))
	}
	if input.Direction != "" {
		result.Direction = stringPointer(string(input.Direction))
	}
	if input.StateFieldPath != "" {
		result.FieldPath = typedStringPointer[semantic.StateFieldPath](string(input.StateFieldPath))
	}
	return result
}

func mapCompiledMatrixAxis(input viewcompiler.MatrixAxis) semantic.ViewMatrixAxis {
	return semantic.ViewMatrixAxis{
		EntityTypeAddresses: typedStringSlicePointer[semantic.EntityTypeAddress](input.EntityTypeAddresses),
		LabelField:          string(input.LabelField),
	}
}

func applyCompiledViewShape(result *semantic.ViewRecipeShape, compiled viewcompiler.Shape) error {
	if result.Kind != string(compiled.Kind) {
		return fmt.Errorf("compiled View shape kind does not match normalized shape")
	}
	if compiled.Table == nil {
		return nil
	}
	if result.Table == nil || len(result.Table.Columns) != len(compiled.Table.Columns) {
		return fmt.Errorf("compiled View table columns do not match normalized shape")
	}
	for i, column := range compiled.Table.Columns {
		if string(result.Table.Columns[i].Address) != column.Address || string(result.Table.Columns[i].ID) != column.ID {
			return fmt.Errorf("compiled View table column identity does not match normalized shape")
		}
		mapped, err := mapTableValueType(column.ValueType)
		if err != nil {
			return err
		}
		result.Table.Columns[i].ValueType = mapped
	}
	return nil
}

func mapTableValueType(input viewcompiler.TableValueType) (semantic.ViewTableValueType, error) {
	result := semantic.ViewTableValueType{Kind: string(input.Kind)}
	switch input.Kind {
	case viewcompiler.TableValueScalar:
		if input.ScalarType == "" {
			return semantic.ViewTableValueType{}, fmt.Errorf("compiled scalar table value lacks scalar type")
		}
		scalarType := semantic.ValueType(input.ScalarType)
		result.ScalarType = &scalarType
		if len(input.EnumValues) != 0 {
			values := sortedStrings(input.EnumValues)
			result.EnumValues = &values
		}
		if input.Format != nil {
			format := semantic.StringFormat(*input.Format)
			result.Format = &format
		}
	case viewcompiler.TableValueStableAddress, viewcompiler.TableValueStringSet:
		if input.ScalarType != "" || input.EnumValues != nil || input.Format != nil {
			return semantic.ViewTableValueType{}, fmt.Errorf("compiled non-scalar table value has scalar metadata")
		}
	default:
		return semantic.ViewTableValueType{}, fmt.Errorf("compiled table value has unknown kind")
	}
	return result, nil
}

func mapTableColumnSource(input materialize.TableColumnSource) semantic.ViewTableColumnSource {
	result := semantic.ViewTableColumnSource{Kind: string(input.Kind), RelationTypeAddresses: typedStringSlicePointer[semantic.RelationTypeAddress](input.RelationTypeAddresses)}
	if input.Field != "" {
		result.Field = stringPointer(input.Field)
	}
	if len(input.ColumnAddresses) != 0 {
		values := typedStrings[semantic.ColumnAddress](input.ColumnAddresses)
		result.ColumnAddresses = &values
	}
	if input.Endpoint != "" {
		result.Endpoint = stringPointer(string(input.Endpoint))
	}
	if input.Direction != "" {
		result.Direction = stringPointer(string(input.Direction))
	}
	if input.StateFieldPath != "" {
		result.FieldPath = typedStringPointer[semantic.StateFieldPath](string(input.StateFieldPath))
	}
	return result
}

func mapMatrixAxis(input materialize.MatrixAxis) semantic.ViewMatrixAxis {
	return semantic.ViewMatrixAxis{EntityTypeAddresses: typedStringSlicePointer[semantic.EntityTypeAddress](input.EntityTypeAddresses), LabelField: string(input.LabelField)}
}

func mapExportRecipe(input materialize.ExportRecipe, compiled exportrecipe.Recipe) (semantic.ExportRecipe, error) {
	if input.Address != compiled.Address {
		return semantic.ExportRecipe{}, fmt.Errorf("Export recipe generation mismatch")
	}
	options, err := mapExportOptions(input.Options)
	if err != nil {
		return semantic.ExportRecipe{}, err
	}
	return semantic.ExportRecipe{ID: semantic.LocalIdentifier(input.ID), Address: semantic.ViewExportAddress(input.Address), ViewAddress: semantic.ViewAddress(compiled.ViewAddress), Format: semantic.ExportFormat(input.Format), Extension: compiled.Extension, Filename: input.Filename, Fidelity: semantic.ExportFidelity(input.Fidelity), SourceRefs: input.SourceRefs, ExporterProfile: semantic.ExporterProfileRef{ID: input.ExporterProfile.ID, Format: semantic.ExportFormat(input.ExporterProfile.Format), RegistrySchemaVersion: int64(input.ExporterProfile.RegistrySchemaVersion), RegistryDigest: protocolcommon.Digest(input.ExporterProfile.RegistryDigest), SpecificationDigest: protocolcommon.Digest(input.ExporterProfile.SpecificationDigest)}, Options: options, NativeMaximumFidelity: semantic.ExportFidelity(compiled.NativeMaximumFidelity), EffectiveMaximumFidelity: semantic.ExportFidelity(compiled.EffectiveMaximumFidelity), FidelityBasis: string(compiled.FidelityBasis), RequiresSourceManifest: compiled.RequiresSourceManifest}, nil
}

func mapExportOptions(input materialize.ExportOptions) (semantic.ExportOptions, error) {
	result := semantic.ExportOptions{Kind: semantic.ExportFormat(input.Kind), Background: optionalTypedStringPointer[semantic.RasterBackground](input.Background), Bundle: cloneBoolPointer(input.Bundle), Diagnostics: cloneBoolPointer(input.Diagnostics), EmbedAssets: cloneBoolPointer(input.EmbedAssets), Fit: stringifyPointer(input.Fit), Formulas: cloneBoolPointer(input.Formulas), Header: cloneBoolPointer(input.Header), HiddenIDs: cloneBoolPointer(input.HiddenIDs), Interactive: cloneBoolPointer(input.Interactive), Legend: cloneBoolPointer(input.Legend), LookupSheets: cloneBoolPointer(input.LookupSheets), Orientation: stringifyPointer(input.Orientation), PageSize: stringifyPointer(input.PageSize), Profile: stringifyPointer(input.Profile), SourceManifest: cloneBoolPointer(input.SourceManifest), StateSummary: cloneBoolPointer(input.StateSummary), ViewDataJSON: cloneBoolPointer(input.ViewDataJSON)}
	if input.Width != nil {
		value, err := mapDimension(*input.Width)
		if err != nil {
			return semantic.ExportOptions{}, err
		}
		result.Width = &value
	}
	if input.Height != nil {
		value, err := mapDimension(*input.Height)
		if err != nil {
			return semantic.ExportOptions{}, err
		}
		result.Height = &value
	}
	if input.Scale != nil {
		value, err := finiteDecimal(*input.Scale, true)
		if err != nil {
			return semantic.ExportOptions{}, err
		}
		positive := semantic.CanonicalPositiveFiniteDecimal(value)
		result.Scale = &positive
	}
	return result, nil
}

func mapDimension(input materialize.Dimension) (semantic.ExportDimension, error) {
	if input.Auto {
		return semantic.ExportDimension{Kind: "auto"}, nil
	}
	if input.Value <= 0 {
		return semantic.ExportDimension{}, fmt.Errorf("non-positive export dimension")
	}
	value := protocolcommon.CanonicalPositiveInt64(strconv.FormatInt(input.Value, 10))
	return semantic.ExportDimension{Kind: "value", Value: &value}, nil
}

func recipeBlobID(kind, address string) string {
	digest := sha256Bytes([]byte(address))
	return "engine.compile/output/recipe/" + kind + "/" + digest
}

func sha256Bytes(value []byte) string {
	digest := sha256.Sum256(value)
	return fmt.Sprintf("%x", digest[:])
}

func queryRecipeRef(ref protocolcommon.BlobRef) engineprotocol.QueryRecipeBlobRef {
	return engineprotocol.QueryRecipeBlobRef{BlobID: ref.BlobID, Digest: ref.Digest, Lifetime: engineprotocol.QueryRecipeBlobRefLifetime(ref.Lifetime), MediaType: engineprotocol.QueryRecipeBlobRefMediaType(ref.MediaType), Size: ref.Size}
}
func viewRecipeRef(ref protocolcommon.BlobRef) engineprotocol.ViewRecipeBlobRef {
	return engineprotocol.ViewRecipeBlobRef{BlobID: ref.BlobID, Digest: ref.Digest, Lifetime: engineprotocol.ViewRecipeBlobRefLifetime(ref.Lifetime), MediaType: engineprotocol.ViewRecipeBlobRefMediaType(ref.MediaType), Size: ref.Size}
}
func exportRecipeRef(ref protocolcommon.BlobRef) engineprotocol.ExportRecipeBlobRef {
	return engineprotocol.ExportRecipeBlobRef{BlobID: ref.BlobID, Digest: ref.Digest, Lifetime: engineprotocol.ExportRecipeBlobRefLifetime(ref.Lifetime), MediaType: engineprotocol.ExportRecipeBlobRefMediaType(ref.MediaType), Size: ref.Size}
}

func typedStringSlicePointer[T ~string](input *[]string) *[]T {
	if input == nil {
		return nil
	}
	value := typedStrings[T](*input)
	return &value
}

func typedStrings[T ~string](input []string) []T {
	result := make([]T, len(input))
	for index, value := range input {
		result[index] = T(value)
	}
	return result
}

func typedStringPointer[T ~string](input string) *T {
	value := T(input)
	return &value
}

func nonEmptyTypedStringPointer[T ~string](input string) *T {
	if input == "" {
		return nil
	}
	return typedStringPointer[T](input)
}

func optionalTypedStringPointer[T ~string](input *string) *T {
	if input == nil {
		return nil
	}
	return typedStringPointer[T](*input)
}

func cloneStrings(input []string) []string { return append([]string{}, input...) }

func sortedStrings(input []string) []string {
	result := cloneStrings(input)
	slices.Sort(result)
	return result
}
func cloneStringMap(input map[string]string) map[string]string {
	result := make(map[string]string, len(input))
	for key, value := range input {
		result[key] = value
	}
	return result
}
func cloneStringPointer(input *string) *string {
	if input == nil {
		return nil
	}
	value := *input
	return &value
}
func cloneBoolPointer(input *bool) *bool {
	if input == nil {
		return nil
	}
	value := *input
	return &value
}
func stringifyPointer[T ~string](input *T) *string {
	if input == nil {
		return nil
	}
	value := string(*input)
	return &value
}

func safeInteger(value int64) (protocolcommon.CanonicalSafeInteger, error) {
	if value < -maxSafeInteger || value > maxSafeInteger {
		return "", fmt.Errorf("integer exceeds safe range")
	}
	return protocolcommon.CanonicalSafeInteger(strconv.FormatInt(value, 10)), nil
}
func nonNegativeSafe(value int64) (protocolcommon.CanonicalNonNegativeSafeInteger, error) {
	if value < 0 || value > maxSafeInteger {
		return "", fmt.Errorf("integer exceeds non-negative safe range")
	}
	return protocolcommon.CanonicalNonNegativeSafeInteger(strconv.FormatInt(value, 10)), nil
}
func positiveSafe(value int64) (protocolcommon.CanonicalPositiveSafeInteger, error) {
	if value <= 0 || value > maxSafeInteger {
		return "", fmt.Errorf("integer exceeds positive safe range")
	}
	return protocolcommon.CanonicalPositiveSafeInteger(strconv.FormatInt(value, 10)), nil
}

func finiteDecimal(value float64, positive bool) (semantic.CanonicalFiniteDecimal, error) {
	if math.IsNaN(value) || math.IsInf(value, 0) || math.Signbit(value) && value == 0 || positive && value <= 0 {
		return "", fmt.Errorf("invalid finite decimal")
	}
	return semantic.CanonicalFiniteDecimal(canonicalBinary64(value)), nil
}

func canonicalBinary64(value float64) string {
	if value == 0 {
		return "0"
	}
	negative := math.Signbit(value)
	if negative {
		value = -value
	}
	scientific := strconv.FormatFloat(value, 'e', -1, 64)
	parts := strings.SplitN(scientific, "e", 2)
	digits := strings.ReplaceAll(parts[0], ".", "")
	exponent, _ := strconv.Atoi(parts[1])
	decimalPosition := exponent + 1
	var result string
	switch {
	case decimalPosition > 0 && decimalPosition <= 21:
		if decimalPosition >= len(digits) {
			result = digits + strings.Repeat("0", decimalPosition-len(digits))
		} else {
			result = digits[:decimalPosition] + "." + digits[decimalPosition:]
		}
	case decimalPosition <= 0 && decimalPosition > -6:
		result = "0." + strings.Repeat("0", -decimalPosition) + digits
	default:
		result = digits[:1]
		if len(digits) > 1 {
			result += "." + digits[1:]
		}
		if exponent >= 0 {
			result += "e+" + strconv.Itoa(exponent)
		} else {
			result += "e" + strconv.Itoa(exponent)
		}
	}
	if negative {
		return "-" + result
	}
	return result
}
