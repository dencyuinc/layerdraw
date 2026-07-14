// SPDX-License-Identifier: LicenseRef-LayerDraw-1.0

package materialize

import (
	"fmt"
	"sort"

	"github.com/dencyuinc/layerdraw/internal/engine/internal/compiler/exportrecipe"
	"github.com/dencyuinc/layerdraw/internal/engine/internal/compiler/query"
	"github.com/dencyuinc/layerdraw/internal/engine/internal/compiler/resolve"
	"github.com/dencyuinc/layerdraw/internal/engine/internal/compiler/semantic/definition"
	"github.com/dencyuinc/layerdraw/internal/engine/internal/compiler/semantic/graph"
	"github.com/dencyuinc/layerdraw/internal/engine/internal/compiler/view"
)

type normalizer struct {
	input   Input
	assets  map[assetKey]AssetBlobSummary
	symbols map[string]resolve.StableSymbol
	kinds   map[string]resolve.SubjectKind
}

type assetKey struct {
	Origin  resolve.SourceOrigin
	Locator string
}

func newNormalizer(input Input, assets map[assetKey]AssetBlobSummary) normalizer {
	symbols := make(map[string]resolve.StableSymbol, len(input.Resolve.Candidates)+len(input.Resolve.Declarations))
	kinds := make(map[string]resolve.SubjectKind, len(input.Resolve.Candidates)+len(input.Resolve.Declarations))
	for _, declaration := range input.Resolve.Candidates {
		symbols[declaration.Address] = declaration.Symbol
		kinds[declaration.Address] = declaration.Kind
	}
	for _, declaration := range input.Resolve.Declarations {
		symbols[declaration.Address] = declaration.Symbol
		kinds[declaration.Address] = declaration.Kind
	}
	return normalizer{input: input, assets: assets, symbols: symbols, kinds: kinds}
}

func (n normalizer) document() (NormalizedDocument, error) {
	d := n.input.Definition
	g := n.input.Graph.Graph
	if d.Project == nil || d.Pack != nil || g == nil {
		return NormalizedDocument{}, fmt.Errorf("Project stages do not contain the Project root and graph")
	}
	entityTypes, err := n.entityTypes(d.EntityTypes)
	if err != nil {
		return NormalizedDocument{}, err
	}
	identity, err := n.identity(d.Identity)
	if err != nil {
		return NormalizedDocument{}, err
	}
	document := NormalizedDocument{
		Format:        NormalizedFormat,
		SchemaVersion: SchemaVersion,
		Language:      LanguageVersion,
		Project:       project(*d.Project),
		Dependencies:  n.dependencies(n.input.Resolved.SelectedClosure, ""),
		EntityTypes:   entityTypes,
		RelationTypes: relationTypes(d.RelationTypes),
		Layers:        layers(d.Layers),
		Entities:      entities(g.Entities),
		Relations:     relations(g.Relations),
		Queries:       queries(n.input.Query.Recipes),
		Views:         views(n.input.View.Recipes),
		References:    references(d.References),
		Identity:      identity,
	}
	document.Assets = n.referencedAssets(document.EntityTypes)
	n.sortDocument(&document)
	return document, nil
}

func (n normalizer) pack() (NormalizedPackArtifact, error) {
	d := n.input.Definition
	if d.Pack == nil || d.Project != nil || n.input.Graph.Graph == nil || len(n.input.Graph.Graph.Entities) != 0 || len(n.input.Graph.Graph.Relations) != 0 || len(d.Layers) != 0 {
		return NormalizedPackArtifact{}, fmt.Errorf("Pack stages contain a Project-only root or graph subject")
	}
	entityTypes, err := n.entityTypes(d.EntityTypes)
	if err != nil {
		return NormalizedPackArtifact{}, err
	}
	identity, err := n.identity(d.Identity)
	if err != nil {
		return NormalizedPackArtifact{}, err
	}
	pack := NormalizedPackArtifact{
		Format:        NormalizedPackFormat,
		SchemaVersion: SchemaVersion,
		Language:      LanguageVersion,
		Pack:          PackRoot{Address: d.Pack.Address, CanonicalID: d.Pack.CanonicalID},
		Dependencies:  n.dependencies(n.input.Resolved.SelectedClosure, d.Pack.Address),
		EntityTypes:   entityTypes,
		RelationTypes: relationTypes(d.RelationTypes),
		Queries:       queries(n.input.Query.Recipes),
		Views:         views(n.input.View.Recipes),
		References:    references(d.References),
		Identity:      identity,
	}
	pack.Assets = n.referencedAssets(pack.EntityTypes)
	n.sortPack(&pack)
	return pack, nil
}

func common(value definition.Common) Common {
	return Common{Description: cloneNormalizedString(value.Description), Tags: normalizedStrings(value.Tags), Annotations: normalizedMap(value.Annotations)}
}

func project(value definition.Project) Project {
	return Project{Common: common(value.Common), ID: normalizeString(value.ID), Address: value.Address, DisplayName: normalizeString(value.DisplayName)}
}

func (n normalizer) dependencies(values []ResolvedPackClosure, excludeAddress string) []ResolvedPackSummary {
	out := make([]ResolvedPackSummary, 0, len(values))
	for _, value := range values {
		if value.Address == excludeAddress {
			continue
		}
		value.CanonicalID = normalizeString(value.CanonicalID)
		value.Version = normalizeString(value.Version)
		out = append(out, value.ResolvedPackSummary)
	}
	sort.Slice(out, func(i, j int) bool { return n.less(out[i].Address, out[j].Address) })
	return out
}

func layers(values []definition.Layer) []Layer {
	out := make([]Layer, len(values))
	for i, value := range values {
		out[i] = Layer{Common: common(value.Common), ID: normalizeString(value.ID), Address: value.Address, DisplayName: normalizeString(value.DisplayName), Order: value.Order}
	}
	return out
}

func (n normalizer) entityTypes(values []definition.EntityType) ([]EntityType, error) {
	out := make([]EntityType, len(values))
	for i, value := range values {
		var image *AssetRef
		if value.Image != nil {
			origin, ok := sourceOrigin(n.input, value.Image.Origin)
			if !ok {
				return nil, fmt.Errorf("asset locator %q has no selected origin", value.Image.Locator)
			}
			asset, exists := n.assets[assetKey{Origin: origin, Locator: value.Image.Locator}]
			if !exists {
				return nil, fmt.Errorf("asset locator %q has no closed resolved bytes", value.Image.Locator)
			}
			image = &AssetRef{Digest: asset.Digest, MediaType: asset.MediaType}
		}
		var shape *definition.RepresentationShape
		if value.Representation.Kind == definition.RepresentationShapeKind {
			copy := value.Representation.Shape
			shape = &copy
		}
		out[i] = EntityType{
			Common: common(value.Common), ID: normalizeString(value.ID), Address: value.Address,
			DisplayName: normalizeString(value.DisplayName), Icon: cloneNormalizedString(value.Icon), Image: image,
			Color: cloneNormalizedString(value.Color), Representation: Representation{Kind: value.Representation.Kind, Shape: shape},
			Columns: columns(value.Columns), UniqueConstraints: constraints(value.UniqueConstraints),
			ReservedColumnIDs: normalizedStrings(value.ReservedColumnIDs), ReservedConstraintIDs: normalizedStrings(value.ReservedConstraintIDs),
		}
	}
	return out, nil
}

func relationTypes(values []definition.RelationType) []RelationType {
	out := make([]RelationType, len(values))
	for i, value := range values {
		out[i] = RelationType{
			Common: common(value.Common), ID: normalizeString(value.ID), Address: value.Address, DisplayName: normalizeString(value.DisplayName),
			SemanticKind: value.SemanticKind, AllowSelf: value.AllowSelf, DuplicatePolicy: value.DuplicatePolicy,
			From: endpoint(value.From), To: endpoint(value.To), Cardinality: cardinality(value.Cardinality),
			ForwardLabel: normalizeString(value.ForwardLabel), ReverseLabel: cloneNormalizedString(value.ReverseLabel),
			Columns: columns(value.Columns), UniqueConstraints: constraints(value.UniqueConstraints), Traversal: traversalPolicy(value.Traversal),
			Projections: projectionSet(value.Projections), Render: renderSet(value.Render),
			Export:            RelationExport{IncludeEndpoints: value.Export.IncludeEndpoints, IncludeRelationRows: value.Export.IncludeRelationRows, SheetName: cloneNormalizedString(value.Export.SheetName)},
			ReservedColumnIDs: normalizedStrings(value.ReservedColumnIDs), ReservedConstraintIDs: normalizedStrings(value.ReservedConstraintIDs),
		}
	}
	return out
}

func columns(values []definition.Column) []Column {
	out := make([]Column, len(values))
	for i, value := range values {
		out[i] = Column{ID: normalizeString(value.ID), Address: value.Address, DisplayName: normalizeString(value.DisplayName), ValueType: value.ValueType,
			EnumValues: normalizedStrings(value.EnumValues), ReservedEnumValues: normalizedStrings(value.ReservedEnumValues), Required: value.Required,
			Default: scalarPointer(value.Default), Format: clonePointer(value.Format), Min: clonePointer(value.Min), Max: clonePointer(value.Max),
			MinLength: clonePointer(value.MinLength), MaxLength: clonePointer(value.MaxLength)}
	}
	return out
}

func constraints(values []definition.UniqueConstraint) []UniqueConstraint {
	out := make([]UniqueConstraint, len(values))
	for i, value := range values {
		out[i] = UniqueConstraint{ID: normalizeString(value.ID), Address: value.Address, ColumnAddresses: append([]string{}, value.ColumnAddresses...)}
	}
	return out
}

func endpoint(value definition.EndpointRule) EndpointRule {
	return EndpointRule{Role: normalizeString(value.Role), EntityTypeAddresses: append([]string{}, value.EntityTypeAddresses...), LayerAddresses: append([]string{}, value.LayerAddresses...)}
}

func cardinality(value definition.Cardinality) Cardinality {
	return Cardinality{ToPerFrom: cardinalityBound(value.ToPerFrom), FromPerTo: cardinalityBound(value.FromPerTo)}
}

func cardinalityBound(value definition.CardinalityBound) CardinalityBound {
	return CardinalityBound{Min: value.Min, Max: CardinalityMaximum{Many: value.Max == definition.CardinalityMaximumMany, Value: 1}}
}

func traversalPolicy(value definition.TraversalPolicy) TraversalPolicy {
	return TraversalPolicy{DefaultDirection: value.DefaultDirection, ParticipatesInImpact: value.ParticipatesInImpact,
		ParticipatesInFlow: value.ParticipatesInFlow, ParticipatesInHierarchy: value.ParticipatesInHierarchy,
		ParticipatesInDependencyMatrix: value.ParticipatesInDependencyMatrix}
}

func projectionSet(value definition.ProjectionSet) ProjectionSet {
	return ProjectionSet{Composed: composedProjection(value.Composed), Diagram: diagramProjection(value.Diagram), Table: tableProjection(value.Table),
		Matrix: matrixProjection(value.Matrix), Tree: treeProjection(value.Tree), Flow: flowProjection(value.Flow), Context: contextProjection(value.Context)}
}

func composedProjection(value definition.ComposedProjection) ComposedProjection {
	return ComposedProjection{Mode: value.Mode, Priority: value.Priority, Conflict: value.Conflict, KeepEdge: value.KeepEdge,
		ParentEndpoint: clonePointer(value.ParentEndpoint), ChildEndpoint: clonePointer(value.ChildEndpoint), OverlayEndpoint: clonePointer(value.OverlayEndpoint),
		TargetEndpoint: clonePointer(value.TargetEndpoint), BadgeEndpoint: clonePointer(value.BadgeEndpoint)}
}

func diagramProjection(value definition.DiagramProjection) DiagramProjection {
	return DiagramProjection{Mode: value.Mode, SourceEndpoint: value.SourceEndpoint, TargetEndpoint: value.TargetEndpoint, EdgeLabel: value.EdgeLabel, IncludeRelationType: value.IncludeRelationType}
}

func tableProjection(value definition.TableProjection) TableProjection {
	return TableProjection{RowMode: value.RowMode, IncludeFrom: value.IncludeFrom, IncludeTo: value.IncludeTo, IncludeRelationType: value.IncludeRelationType}
}

func matrixProjection(value *definition.MatrixProjection) *MatrixProjection {
	if value == nil {
		return nil
	}
	return &MatrixProjection{RowEndpoint: value.RowEndpoint, ColumnEndpoint: value.ColumnEndpoint, IncludeRelationRows: value.IncludeRelationRows}
}

func treeProjection(value *definition.TreeProjection) *TreeProjection {
	if value == nil {
		return nil
	}
	return &TreeProjection{ParentEndpoint: value.ParentEndpoint, ChildEndpoint: value.ChildEndpoint}
}

func flowProjection(value *definition.FlowProjection) *FlowProjection {
	if value == nil {
		return nil
	}
	return &FlowProjection{SourceEndpoint: value.SourceEndpoint, TargetEndpoint: value.TargetEndpoint, ConnectorKind: value.ConnectorKind, BranchValueColumnAddress: clonePointer(value.BranchValueColumnAddress)}
}

func contextProjection(value definition.ContextProjection) ContextProjection {
	return ContextProjection{FactTemplate: normalizeString(value.FactTemplate), ReverseFactTemplate: cloneNormalizedString(value.ReverseFactTemplate), IncludeAttributeRows: value.IncludeAttributeRows}
}

func renderSet(value definition.RenderSet) RenderSet {
	return RenderSet{
		Edge:    EdgeRender{Arrow: value.Edge.Arrow, Line: value.Edge.Line, Color: cloneNormalizedString(value.Edge.Color), Label: value.Edge.Label},
		Nested:  NestedRender{FrameLabel: value.Nested.FrameLabel, FrameStyle: value.Nested.FrameStyle},
		Overlay: OverlayRender{Kind: normalizeString(value.Overlay.Kind), Position: value.Overlay.Position, MaxItems: value.Overlay.MaxItems},
		Badge:   BadgeRender{Icon: cloneNormalizedString(value.Badge.Icon), Label: value.Badge.Label, Position: value.Badge.Position},
	}
}

func entities(values []graph.Entity) []Entity {
	out := make([]Entity, len(values))
	for i, value := range values {
		out[i] = Entity{Common: common(value.Common), ID: normalizeString(value.ID), Address: value.Address, DisplayName: normalizeString(value.DisplayName),
			TypeAddress: value.TypeAddress, LayerAddress: value.LayerAddress, Rows: rows(value.Rows), ReservedRowIDs: normalizedStrings(value.ReservedRowIDs)}
	}
	return out
}

func relations(values []graph.Relation) []Relation {
	out := make([]Relation, len(values))
	for i, value := range values {
		out[i] = Relation{Common: common(value.Common), ID: normalizeString(value.ID), Address: value.Address, DisplayName: cloneNormalizedString(value.DisplayName),
			TypeAddress: value.TypeAddress, FromAddress: value.FromAddress, ToAddress: value.ToAddress, Rows: rows(value.Rows), ReservedRowIDs: normalizedStrings(value.ReservedRowIDs)}
	}
	return out
}

func rows(values []graph.AttributeRow) []AttributeRow {
	out := make([]AttributeRow, len(values))
	for i, value := range values {
		cells := make(map[string]Scalar, len(value.Values))
		for _, cell := range value.Values {
			cells[cell.ColumnAddress] = scalar(cell.Value)
		}
		out[i] = AttributeRow{ID: normalizeString(value.ID), Address: value.Address, Values: cells}
	}
	return out
}

func queries(values []query.Recipe) []Query {
	out := make([]Query, len(values))
	for i, value := range values {
		parameters := make([]QueryParameter, len(value.Parameters))
		for j, parameter := range value.Parameters {
			parameters[j] = QueryParameter{ID: normalizeString(parameter.ID), Address: parameter.Address, ValueType: parameter.ValueType,
				EnumValues: normalizedStrings(parameter.EnumValues), ReservedEnumValues: normalizedStrings(parameter.ReservedEnumValues), Required: parameter.Required,
				Default: scalarPointer(parameter.Default), Format: clonePointer(parameter.Format), Min: clonePointer(parameter.Min), Max: clonePointer(parameter.Max),
				MinLength: clonePointer(parameter.MinLength), MaxLength: clonePointer(parameter.MaxLength)}
		}
		out[i] = Query{Common: common(value.Common), ID: normalizeString(value.ID), Address: value.Address, DisplayName: normalizeString(value.DisplayName),
			StateInput: value.StateInput, Parameters: parameters, Select: querySelect(value.Select), Where: predicate(value.Where),
			RelationWhere: predicate(value.RelationWhere), Traverse: queryTraversal(value.Traversal), Result: append([]query.ResultMember{}, value.Result...),
			ReservedParameterIDs: normalizedStrings(value.ReservedParameterIDs)}
	}
	return out
}

func querySelect(value query.Select) QuerySelect {
	return QuerySelect{LayerAddresses: cloneStringSlicePointer(value.LayerAddresses), EntityTypeAddresses: cloneStringSlicePointer(value.EntityTypeAddresses),
		RelationTypeAddresses: cloneStringSlicePointer(value.RelationTypeAddresses), RootAddresses: cloneStringSlicePointer(value.RootAddresses)}
}

func predicate(value query.Predicate) Predicate {
	out := Predicate{Kind: value.Kind, Field: value.Field, FieldPath: value.FieldPath, Operator: value.Operator, Quantifier: value.Quantifier,
		TypeAddresses: append([]string{}, value.TypeAddresses...), Value: predicateValue(value.Value)}
	for _, child := range value.Children {
		out.Children = append(out.Children, predicate(child))
	}
	if value.Child != nil {
		child := predicate(*value.Child)
		out.Child = &child
	}
	if value.Row != nil {
		row := rowPredicate(*value.Row)
		out.RowPredicate = &row
	}
	return out
}

func rowPredicate(value query.RowPredicate) RowPredicate {
	out := RowPredicate{Kind: value.Kind, ColumnAddresses: append([]string{}, value.ColumnAddresses...), FieldPath: value.FieldPath, Operator: value.Operator, Value: predicateValue(value.Value)}
	for _, child := range value.Children {
		out.Children = append(out.Children, rowPredicate(child))
	}
	if value.Child != nil {
		child := rowPredicate(*value.Child)
		out.Child = &child
	}
	return out
}

func predicateValue(value *query.PredicateValue) *PredicateValue {
	if value == nil {
		return nil
	}
	out := &PredicateValue{Kind: value.Kind, ParameterAddress: value.ParameterAddress}
	switch {
	case value.Scalar != nil:
		scalarValue := scalar(*value.Scalar)
		out.Scalar = &scalarValue
	case value.Address != nil:
		out.Address = cloneNormalizedString(value.Address)
	case value.Scalars != nil:
		values := make([]Scalar, len(value.Scalars))
		for i := range value.Scalars {
			values[i] = scalar(value.Scalars[i])
		}
		out.Scalars = values
	case value.Addresses != nil:
		out.Addresses = append([]string{}, value.Addresses...)
	}
	return out
}

func queryTraversal(value *query.Traversal) *QueryTraversal {
	if value == nil {
		return nil
	}
	return &QueryTraversal{Direction: value.Direction, MinDepth: value.MinDepth, MaxDepth: value.MaxDepth, CyclePolicy: value.CyclePolicy, RelationTypeAddresses: cloneStringSlicePointer(value.RelationTypeAddresses)}
}

func views(values []view.Recipe) []View {
	out := make([]View, len(values))
	for i, value := range values {
		projections := make(map[string]ProjectionOverride, len(value.RelationProjections))
		for _, item := range value.RelationProjections {
			set, render := projectionSet(item.Projections), renderSet(item.Render)
			projections[item.RelationTypeAddress] = ProjectionOverride{Composed: &set.Composed, Diagram: &set.Diagram, Table: &set.Table,
				Matrix: set.Matrix, Tree: set.Tree, Flow: set.Flow, Context: &set.Context, Render: &render}
		}
		out[i] = View{Common: common(value.Common), ID: normalizeString(value.ID), Address: value.Address, DisplayName: normalizeString(value.DisplayName), StateInput: value.StateInput,
			Category: value.Category, Intent: cloneNormalizedString(value.Intent), Source: viewSource(value.Source), RelationProjections: projections,
			Shape: viewShape(value.Shape), Exports: exports(value.Exports), ReservedTableColumnIDs: normalizedStrings(value.ReservedTableColumnIDs), ReservedExportIDs: normalizedStrings(value.ReservedExportIDs)}
	}
	return out
}

func viewSource(value view.Source) ViewSource {
	out := ViewSource{Kind: value.Kind, Arguments: map[string]Scalar{}}
	if value.Query != nil {
		address := value.Query.QueryAddress
		out.QueryAddress = &address
		for _, argument := range value.Query.Arguments {
			out.Arguments[argument.ParameterAddress] = scalar(argument.Value)
		}
	}
	if value.Diff != nil {
		before, after := normalizeString(value.Diff.Before), normalizeString(value.Diff.After)
		out.Before, out.After = &before, &after
		out.QueryAddress = clonePointer(value.Diff.QueryAddress)
		for _, argument := range value.Diff.Arguments {
			out.Arguments[argument.ParameterAddress] = scalar(argument.Value)
		}
	}
	return out
}

func viewShape(value view.Shape) ViewShape {
	out := ViewShape{Kind: value.Kind}
	if value.Diagram != nil {
		placements := make([]Placement, len(value.Diagram.Placements))
		for i, p := range value.Diagram.Placements {
			placements[i] = Placement{p.EntityAddress, p.X, p.Y, p.Width, p.Height}
		}
		out.Diagram = &DiagramShape{Layout: value.Diagram.Layout, Direction: value.Diagram.Direction, Abstraction: value.Diagram.Abstraction, Composed: value.Diagram.Composed, Placements: placements}
	}
	if value.Table != nil {
		columns := make([]TableColumn, len(value.Table.Columns))
		for i, column := range value.Table.Columns {
			columns[i] = TableColumn{ID: normalizeString(column.ID), Address: column.Address, Label: cloneNormalizedString(column.Label), Source: tableColumnSource(column.Source), Aggregate: column.Aggregate}
		}
		sorts := make([]TableSort, len(value.Table.Sorts))
		for i, item := range value.Table.Sorts {
			sorts[i] = TableSort{item.ColumnID, item.Direction, item.Absent}
		}
		out.Table = &TableShape{RowSource: value.Table.RowSource, EntityTypeAddresses: cloneStringSlicePointer(value.Table.EntityTypeAddresses), IncludeEntityID: value.Table.IncludeEntityID, IncludeType: value.Table.IncludeType, IncludeLayer: value.Table.IncludeLayer, Columns: columns, Sorts: sorts}
	}
	if value.Matrix != nil {
		out.Matrix = &MatrixShape{RowAxis: matrixAxis(value.Matrix.RowAxis), ColumnAxis: matrixAxis(value.Matrix.ColumnAxis), Cell: matrixCell(value.Matrix.Cell)}
	}
	if value.Tree != nil {
		out.Tree = &TreeShape{RelationTypeAddresses: append([]string{}, value.Tree.RelationTypeAddresses...), CyclePolicy: value.Tree.CyclePolicy, SharedChildPolicy: value.Tree.SharedChildPolicy}
	}
	if value.Flow != nil {
		out.Flow = &FlowShape{RelationTypeAddresses: append([]string{}, value.Flow.RelationTypeAddresses...), LaneBy: value.Flow.LaneBy, LaneColumnAddresses: cloneStringSlicePointer(value.Flow.LaneColumnAddresses), CyclePolicy: value.Flow.CyclePolicy, PreserveParallel: value.Flow.PreserveParallel}
	}
	if value.Context != nil {
		out.Context = &ContextShape{GroupBy: value.Context.GroupBy, IncludeEntityRows: value.Context.IncludeEntityRows, IncludeRelationRows: value.Context.IncludeRelationRows, Incoming: value.Context.Incoming, Outgoing: value.Context.Outgoing}
	}
	if value.Diff != nil {
		out.Diff = &DiffShape{Include: append([]view.DiffSubjectKind{}, value.Diff.Include...), DetectMoves: value.Diff.DetectMoves}
	}
	return out
}

func tableColumnSource(value view.TableColumnSource) TableColumnSource {
	return TableColumnSource{Kind: value.Kind, Field: value.Field, ColumnAddresses: append([]string{}, value.ColumnAddresses...), Endpoint: value.Endpoint,
		Direction: value.Direction, RelationTypeAddresses: cloneStringSlicePointer(value.RelationTypeAddresses), StateFieldPath: value.StateFieldPath}
}

func matrixAxis(value view.MatrixAxis) MatrixAxis {
	return MatrixAxis{EntityTypeAddresses: cloneStringSlicePointer(value.EntityTypeAddresses), LabelField: value.LabelField}
}
func matrixCell(value view.MatrixCell) MatrixCell {
	return MatrixCell{RelationTypeAddresses: cloneStringSlicePointer(value.RelationTypeAddresses), Direction: value.Direction, Semantic: value.Semantic, Display: value.Display, AttributeColumnAddresses: cloneStringSlicePointer(value.AttributeColumnAddresses)}
}

func exports(values []exportrecipe.Recipe) []ExportRecipe {
	out := make([]ExportRecipe, len(values))
	for i, value := range values {
		out[i] = ExportRecipe{ID: normalizeString(value.ID), Address: value.Address, Format: value.Format, Filename: normalizeString(value.Filename), Fidelity: value.Fidelity, SourceRefs: value.SourceRefs,
			ExporterProfile: ExporterProfileRef{ID: normalizeString(value.ExporterProfile.ID), Format: value.ExporterProfile.Format, RegistrySchemaVersion: value.ExporterProfile.RegistrySchemaVersion,
				RegistryDigest: value.ExporterProfile.RegistryDigest, SpecificationDigest: value.ExporterProfile.SpecificationDigest}, Options: exportOptions(value.Options)}
	}
	return out
}

func exportOptions(value exportrecipe.Options) ExportOptions {
	out := ExportOptions{Kind: value.Kind}
	if v := value.Structured; v != nil {
		out.Diagnostics, out.StateSummary = boolPointer(v.Diagnostics), boolPointer(v.StateSummary)
	}
	if v := value.Image; v != nil {
		out.Width, out.Height = dimension(v.Width), dimension(v.Height)
		out.Scale, out.Background = clonePointer(&v.Scale), cloneNormalizedString(&v.Background)
	}
	if v := value.Page; v != nil {
		out.PageSize, out.Orientation, out.Fit, out.Legend = clonePointer(&v.PageSize), clonePointer(&v.Orientation), clonePointer(&v.Fit), boolPointer(v.Legend)
	}
	if v := value.HTML; v != nil {
		out.Interactive, out.EmbedAssets = boolPointer(v.Interactive), boolPointer(v.EmbedAssets)
	}
	if v := value.Delimited; v != nil {
		out.Bundle, out.Header, out.SourceManifest = boolPointer(v.Bundle), boolPointer(v.Header), boolPointer(v.SourceManifest)
	}
	if v := value.XLSX; v != nil {
		out.Profile, out.LookupSheets, out.HiddenIDs, out.Formulas, out.ViewDataJSON = clonePointer(&v.Profile), boolPointer(v.LookupSheets), boolPointer(v.HiddenIDs), boolPointer(v.Formulas), boolPointer(v.ViewDataJSON)
	}
	if v := value.Manifest; v != nil {
		out.SourceManifest = boolPointer(v.SourceManifest)
	}
	return out
}

func dimension(value exportrecipe.Dimension) *Dimension {
	return &Dimension{Auto: value.Auto, Value: value.Value}
}

func references(values []definition.Reference) []Reference {
	out := make([]Reference, len(values))
	for i, value := range values {
		out[i] = Reference{ID: normalizeString(value.ID), Address: value.Address, Text: normalizeString(value.Text)}
	}
	return out
}

func (n normalizer) identity(value definition.IdentityHistory) (IdentityHistory, error) {
	out := IdentityHistory{RootReservations: map[string]map[SubjectKind][]string{}, Moves: make([]Move, len(value.Moves)), MoveClosure: make([]MoveResolution, len(value.MoveClosure))}
	for root, kinds := range value.RootReservations {
		out.RootReservations[root] = map[SubjectKind][]string{}
		for kind, ids := range kinds {
			generated, ok := GeneratedSubjectKind(kind, "")
			if !ok {
				return IdentityHistory{}, fmt.Errorf("root reservation %q has unsupported generated kind %q", root, kind)
			}
			out.RootReservations[root][generated] = normalizedStrings(ids)
		}
	}
	for i, move := range value.Moves {
		generated, ok := n.generatedKind(move.Kind, move.OwnerAddress)
		if !ok {
			return IdentityHistory{}, fmt.Errorf("move %q has unsupported generated kind %q", move.OldAddress, move.Kind)
		}
		out.Moves[i] = Move{generated, clonePointer(move.OwnerAddress), move.OldAddress, move.NewAddress}
	}
	for i, move := range value.MoveClosure {
		generated, ok := n.generatedKind(move.Kind, move.OwnerAddress)
		if !ok {
			return IdentityHistory{}, fmt.Errorf("move resolution %q has unsupported generated kind %q", move.SourceAddress, move.Kind)
		}
		out.MoveClosure[i] = MoveResolution{generated, clonePointer(move.OwnerAddress), move.SourceAddress, move.TerminalAddress}
	}
	return out, nil
}

func (n normalizer) generatedKind(kind resolve.SubjectKind, ownerAddress *string) (SubjectKind, bool) {
	ownerKind := resolve.SubjectKind("")
	if ownerAddress != nil {
		ownerKind = n.kinds[*ownerAddress]
	}
	return GeneratedSubjectKind(kind, ownerKind)
}

func (n normalizer) referencedAssets(types []EntityType) []AssetBlobSummary {
	byDigest := map[string]AssetBlobSummary{}
	for _, item := range types {
		if item.Image != nil {
			for _, asset := range n.assets {
				if asset.Digest == item.Image.Digest {
					byDigest[asset.Digest] = asset
					break
				}
			}
		}
	}
	out := make([]AssetBlobSummary, 0, len(byDigest))
	for _, value := range byDigest {
		out = append(out, value)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Digest < out[j].Digest })
	return out
}

func (n normalizer) sortDocument(value *NormalizedDocument) {
	n.sortEntityTypes(value.EntityTypes)
	n.sortRelationTypes(value.RelationTypes)
	sort.SliceStable(value.Layers, func(i, j int) bool { return n.less(value.Layers[i].Address, value.Layers[j].Address) })
	sort.SliceStable(value.Entities, func(i, j int) bool { return n.less(value.Entities[i].Address, value.Entities[j].Address) })
	sort.SliceStable(value.Relations, func(i, j int) bool { return n.less(value.Relations[i].Address, value.Relations[j].Address) })
	n.sortQueries(value.Queries)
	n.sortViews(value.Views)
	sort.SliceStable(value.References, func(i, j int) bool { return n.less(value.References[i].Address, value.References[j].Address) })
	for i := range value.Entities {
		sort.SliceStable(value.Entities[i].Rows, func(a, b int) bool {
			return n.less(value.Entities[i].Rows[a].Address, value.Entities[i].Rows[b].Address)
		})
	}
	for i := range value.Relations {
		sort.SliceStable(value.Relations[i].Rows, func(a, b int) bool {
			return n.less(value.Relations[i].Rows[a].Address, value.Relations[i].Rows[b].Address)
		})
	}
}

func (n normalizer) sortPack(value *NormalizedPackArtifact) {
	n.sortEntityTypes(value.EntityTypes)
	n.sortRelationTypes(value.RelationTypes)
	n.sortQueries(value.Queries)
	n.sortViews(value.Views)
	sort.SliceStable(value.References, func(i, j int) bool { return n.less(value.References[i].Address, value.References[j].Address) })
}

func (n normalizer) sortEntityTypes(values []EntityType) {
	sort.SliceStable(values, func(i, j int) bool { return n.less(values[i].Address, values[j].Address) })
	for i := range values {
		sort.SliceStable(values[i].UniqueConstraints, func(a, b int) bool {
			return n.less(values[i].UniqueConstraints[a].Address, values[i].UniqueConstraints[b].Address)
		})
	}
}
func (n normalizer) sortRelationTypes(values []RelationType) {
	sort.SliceStable(values, func(i, j int) bool { return n.less(values[i].Address, values[j].Address) })
	for i := range values {
		sort.SliceStable(values[i].UniqueConstraints, func(a, b int) bool {
			return n.less(values[i].UniqueConstraints[a].Address, values[i].UniqueConstraints[b].Address)
		})
	}
}
func (n normalizer) sortQueries(values []Query) {
	sort.SliceStable(values, func(i, j int) bool { return n.less(values[i].Address, values[j].Address) })
	for i := range values {
		sort.SliceStable(values[i].Parameters, func(a, b int) bool { return n.less(values[i].Parameters[a].Address, values[i].Parameters[b].Address) })
	}
}
func (n normalizer) sortViews(values []View) {
	sort.SliceStable(values, func(i, j int) bool { return n.less(values[i].Address, values[j].Address) })
	for i := range values {
		sort.SliceStable(values[i].Exports, func(a, b int) bool { return n.less(values[i].Exports[a].Address, values[i].Exports[b].Address) })
	}
}

func (n normalizer) less(left, right string) bool {
	a, aOK := n.symbols[left]
	if !aOK {
		a, aOK = stableSymbolFromAddress(left)
	}
	b, bOK := n.symbols[right]
	if !bOK {
		b, bOK = stableSymbolFromAddress(right)
	}
	if aOK && bOK {
		return resolve.CompareStableSymbols(a, b) < 0
	}
	return left < right
}

func scalar(value definition.Scalar) Scalar {
	return Scalar{Type: value.Type, String: normalizeString(value.String), Int: value.Int, Float: value.Float, Bool: value.Bool}
}
func scalarPointer(value *definition.Scalar) *Scalar {
	if value == nil {
		return nil
	}
	out := scalar(*value)
	return &out
}
func boolPointer(value bool) *bool { return &value }
func clonePointer[T any](value *T) *T {
	if value == nil {
		return nil
	}
	out := *value
	return &out
}
func cloneNormalizedString(value *string) *string {
	if value == nil {
		return nil
	}
	out := normalizeString(*value)
	return &out
}
func normalizedStrings(values []string) []string {
	out := make([]string, len(values))
	for i, value := range values {
		out[i] = normalizeString(value)
	}
	return out
}
func normalizedMap(values map[string]string) map[string]string {
	out := make(map[string]string, len(values))
	for key, value := range values {
		out[normalizeString(key)] = normalizeString(value)
	}
	return out
}
func cloneStringSlicePointer(value *[]string) *[]string {
	if value == nil {
		return nil
	}
	out := append([]string{}, (*value)...)
	return &out
}
