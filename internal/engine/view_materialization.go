// SPDX-License-Identifier: LicenseRef-LayerDraw-1.0

package engine

import (
	"context"
	"fmt"
	"reflect"
	"sort"

	"github.com/dencyuinc/layerdraw/internal/engine/internal/compiler/query"
	"github.com/dencyuinc/layerdraw/internal/engine/internal/compiler/semantic/definition"
	"github.com/dencyuinc/layerdraw/internal/engine/internal/compiler/semantic/graph"
	"github.com/dencyuinc/layerdraw/internal/engine/internal/compiler/view"
)

// MaterializeView converts one immutable Query or Diff source into semantic
// ViewData. It performs no layout, rendering, export planning, storage access,
// clock reads, or network I/O.
func (e Engine) MaterializeView(ctx context.Context, input ViewMaterializationInput) ViewMaterializationResponse {
	if ctx == nil {
		return rejectedView(diagnostic("LDL1801", "stale_revision_or_semantic_hash", "ViewData materialization requires a context", input.Recipe.Address, ""))
	}
	if err := pollContext(ctx, "view"); err != nil {
		return rejectedView(diagnostic("LDL1801", "stale_revision_or_semantic_hash", err.Error(), input.Recipe.Address, ""))
	}
	m := newViewMaterializer(ctx, input)
	if !m.validate() {
		return rejectedView(m.diagnostics...)
	}
	if input.Diff != nil {
		m.addDiag("LDL1701", "unsupported_view_shape_or_export", "Diff View materialization is not implemented", input.Recipe.Address, "")
		return rejectedView(m.diagnostics...)
	}

	base := m.base()
	var result ViewData
	switch input.Recipe.Shape.Kind {
	case view.ShapeDiagram:
		result.Diagram = m.diagram(base)
	case view.ShapeTable:
		result.Table = m.table(base)
	case view.ShapeMatrix:
		result.Matrix = m.matrix(base)
	case view.ShapeContext:
		result.Context = m.contextView(base)
	case view.ShapeTree, view.ShapeFlow, view.ShapeDiff:
		m.addDiag("LDL1701", "unsupported_view_shape_or_export", "View shape materialization is not implemented", input.Recipe.Address, "")
		return rejectedView(m.diagnostics...)
	default:
		m.addDiag("LDL1701", "unsupported_view_shape_or_export", "View shape is invalid", input.Recipe.Address, "")
		return rejectedView(m.diagnostics...)
	}
	if hasViewErrorDiagnostics(m.diagnostics) {
		return rejectedView(m.diagnostics...)
	}
	base, ok := result.Base()
	if !ok || base.Kind != ViewDataKind(input.Recipe.Shape.Kind) {
		return rejectedView(diagnostic("LDL1801", "stale_revision_or_semantic_hash", "materialized ViewData union is invalid", input.Recipe.Address, ""))
	}
	base.Source = m.sourceRefs()
	base.Diagnostics = sortedDiagnostics(append(base.Diagnostics, m.diagnostics...))
	setViewDataBase(&result, base)
	if err := pollContext(ctx, "view"); err != nil {
		return rejectedView(diagnostic("LDL1801", "stale_revision_or_semantic_hash", err.Error(), input.Recipe.Address, ""))
	}
	return ViewMaterializationResponse{Status: "ok", Result: &result}
}

type viewMaterializer struct {
	ctx              context.Context
	input            ViewMaterializationInput
	snapshot         Snapshot
	graph            graph.MasterGraph
	queryResult      QueryResult
	project          string
	revision         ViewRevision
	stateInput       QueryStateInputRef
	entities         map[string]graph.Entity
	relations        map[string]graph.Relation
	entityTypes      map[string]definition.EntityType
	relationTypes    map[string]definition.RelationType
	layers           map[string]definition.Layer
	outgoing         map[string][]string
	incoming         map[string][]string
	diagnostics      []Diagnostic
	directStateReads map[StateReadRef]bool
	validatedState   *validatedStateQuerySnapshot
	staleState       map[string]bool
	deniedStateReads map[StateReadRef]bool
	missingStateWarn bool
}

func newViewMaterializer(ctx context.Context, input ViewMaterializationInput) *viewMaterializer {
	return &viewMaterializer{
		ctx: ctx, input: input, entities: map[string]graph.Entity{}, relations: map[string]graph.Relation{},
		entityTypes: map[string]definition.EntityType{}, relationTypes: map[string]definition.RelationType{},
		layers: map[string]definition.Layer{}, outgoing: map[string][]string{}, incoming: map[string][]string{},
		directStateReads: map[StateReadRef]bool{}, staleState: map[string]bool{}, deniedStateReads: map[StateReadRef]bool{},
	}
}

func (m *viewMaterializer) validate() bool {
	if m.input.Recipe.Address == "" {
		m.addDiag("LDL1701", "unsupported_view_shape_or_export", "View recipe address is required", "", "")
	}
	if (m.input.Query == nil) == (m.input.Diff == nil) {
		m.addDiag("LDL1801", "stale_revision_or_semantic_hash", "exactly one View materialization source is required", m.input.Recipe.Address, "")
		return false
	}
	if m.input.Query != nil {
		m.validateQueryInput(*m.input.Query)
	} else {
		m.validateDiffInput(*m.input.Diff)
	}
	if len(m.diagnostics) != 0 {
		return false
	}
	m.indexSnapshot()
	if m.input.Query != nil {
		m.validateQueryResultSubjects()
	}
	return len(m.diagnostics) == 0
}

func (m *viewMaterializer) validateQueryInput(input QueryViewMaterializationInput) {
	if m.input.Recipe.Source.Kind != view.SourceQuery || m.input.Recipe.Source.Query == nil {
		m.addDiag("LDL1801", "stale_revision_or_semantic_hash", "Query input does not match the View source", m.input.Recipe.Address, "")
		return
	}
	if !validRevisionID(input.RevisionID) || !validQueryViewSnapshot(input.Snapshot) {
		m.addDiag("LDL1801", "stale_revision_or_semantic_hash", "Query View revision binding is incomplete", m.input.Recipe.Address, "")
		return
	}
	compiled, ok := viewRecipeInSnapshot(input.Snapshot, m.input.Recipe.Address)
	if !ok || !reflect.DeepEqual(compiled, m.input.Recipe) {
		m.addDiag("LDL1801", "stale_revision_or_semantic_hash", "View recipe does not belong to the supplied revision", m.input.Recipe.Address, "")
		return
	}
	queryRecipe, ok := queryRecipeInSnapshot(input.Snapshot, m.input.Recipe.Source.Query.QueryAddress)
	if !ok || input.QueryResult.QueryAddress != queryRecipe.Address {
		m.addDiag("LDL1601", "invalid_query_or_arguments", "View source Query does not match QueryResult", input.QueryResult.QueryAddress, m.input.Recipe.Address)
		return
	}
	if input.QueryResult.StatePolicy != string(queryRecipe.StateInput) || !viewArgumentsMatch(m.input.Recipe.Source.Query.Arguments, input.QueryResult.Arguments) {
		m.addDiag("LDL1801", "stale_revision_or_semantic_hash", "QueryResult does not match the compiled View source", input.QueryResult.QueryAddress, m.input.Recipe.Address)
		return
	}
	m.snapshot = input.Snapshot
	m.graph = *input.Snapshot.TypedAST.Graph
	m.queryResult = deepClone(input.QueryResult)
	m.project = input.Snapshot.TypedAST.Project.Address
	m.revision = ViewRevision{Single: &SingleRevision{Kind: "single", RevisionID: input.RevisionID, DefinitionHash: input.Snapshot.DefinitionHash}}
	m.validateQueryState(input, queryRecipe.StateInput)
}

func (m *viewMaterializer) validateQueryState(input QueryViewMaterializationInput, queryPolicy query.StatePolicy) {
	effective := m.input.Recipe.StateRequirement
	if effective != query.StateNone && effective != query.StateOptional && effective != query.StateRequired {
		m.addDiag("LDL1801", "stale_revision_or_semantic_hash", "View state policy is invalid", m.input.Recipe.Address, "")
		return
	}
	if queryPolicy == query.StateNone && (len(input.QueryResult.StateReads) != 0 || input.QueryResult.StateInput.Kind != "none") {
		m.addDiag("LDL1801", "stale_revision_or_semantic_hash", "state-independent QueryResult contains state data", input.QueryResult.QueryAddress, m.input.Recipe.Address)
		return
	}
	if effective == query.StateNone {
		if input.StateSnapshot != nil || input.QueryResult.StateInput.Kind != "none" {
			m.addDiag("LDL1801", "stale_revision_or_semantic_hash", "state-independent View must use StateInputRef.none", m.input.Recipe.Address, "")
			return
		}
		m.stateInput = QueryStateInputRef{Kind: "none"}
		return
	}
	if input.StateSnapshot == nil {
		if effective == query.StateRequired {
			m.addDiag("LDL1604", "required_state_snapshot_missing", "required StateQuerySnapshot is absent", m.input.Recipe.Address, "")
			return
		}
		if input.QueryResult.StateInput.Kind != "none" {
			m.addDiag("LDL1801", "stale_revision_or_semantic_hash", "QueryResult references an absent StateQuerySnapshot", m.input.Recipe.Address, "")
			return
		}
		m.stateInput = QueryStateInputRef{Kind: "none"}
		return
	}
	validated, diagnostics, err := validateStateQuerySnapshotForDefinition(m.ctx, input.Snapshot.QueryDefinitionIdentity(), *input.Snapshot.TypedAST.Graph, *input.StateSnapshot)
	if err != nil {
		m.addDiag("LDL1801", "stale_revision_or_semantic_hash", err.Error(), m.input.Recipe.Address, "")
		return
	}
	if len(diagnostics) != 0 {
		m.diagnostics = append(m.diagnostics, diagnostics...)
		return
	}
	if queryPolicy != query.StateNone && input.QueryResult.StateInput != validated.input {
		m.addDiag("LDL1801", "stale_revision_or_semantic_hash", "QueryResult and View do not reference the same StateQuerySnapshot", m.input.Recipe.Address, "")
		return
	}
	m.stateInput = validated.input
	m.validatedState = &validated
}

func (m *viewMaterializer) validateDiffInput(input DiffViewMaterializationInput) {
	if m.input.Recipe.Source.Kind != view.SourceDiff || m.input.Recipe.Source.Diff == nil || m.input.Recipe.Shape.Kind != view.ShapeDiff {
		m.addDiag("LDL1801", "stale_revision_or_semantic_hash", "Diff input does not match the View source", m.input.Recipe.Address, "")
		return
	}
	if m.input.Recipe.StateRequirement != query.StateNone || m.input.Recipe.StateInput != query.StateNone {
		m.addDiag("LDL1801", "stale_revision_or_semantic_hash", "Diff View must be state-independent", m.input.Recipe.Address, "")
		return
	}
	if !validRevisionID(input.RecipeRevisionID) || !validRevisionID(input.BeforeRevisionID) || !validRevisionID(input.AfterRevisionID) ||
		!validDefinitionSnapshot(input.RecipeSnapshot) || !validDefinitionSnapshot(input.BeforeSnapshot) || !validDefinitionSnapshot(input.AfterSnapshot) {
		m.addDiag("LDL1801", "stale_revision_or_semantic_hash", "Diff revision binding is incomplete", m.input.Recipe.Address, "")
		return
	}
	compiled, ok := viewRecipeInSnapshot(input.RecipeSnapshot, m.input.Recipe.Address)
	if !ok || !reflect.DeepEqual(compiled, m.input.Recipe) {
		m.addDiag("LDL1801", "stale_revision_or_semantic_hash", "Diff recipe does not belong to the recipe revision", m.input.Recipe.Address, "")
		return
	}
	if input.BeforeSnapshot.TypedAST.Project.Address != input.AfterSnapshot.TypedAST.Project.Address {
		m.addDiag("LDL1801", "stale_revision_or_semantic_hash", "Diff revisions belong to different Projects", m.input.Recipe.Address, "")
		return
	}
	hasQuery := m.input.Recipe.Source.Diff.QueryAddress != nil
	if hasQuery != (input.BeforeQueryResult != nil && input.AfterQueryResult != nil) {
		m.addDiag("LDL1801", "stale_revision_or_semantic_hash", "Diff Query results do not match the View source", m.input.Recipe.Address, "")
		return
	}
	if !hasQuery && (input.BeforeQueryResult != nil || input.AfterQueryResult != nil) {
		m.addDiag("LDL1801", "stale_revision_or_semantic_hash", "Query-free Diff input contains Query results", m.input.Recipe.Address, "")
		return
	}
	m.snapshot = input.AfterSnapshot
	m.graph = graph.MasterGraph{}
	if input.AfterSnapshot.TypedAST.Graph != nil {
		m.graph = *input.AfterSnapshot.TypedAST.Graph
	}
	m.project = input.AfterSnapshot.TypedAST.Project.Address
	m.revision = ViewRevision{Diff: &DiffRevision{
		Kind: "diff", RecipeRevisionID: input.RecipeRevisionID, RecipeDefinitionHash: input.RecipeSnapshot.DefinitionHash,
		BeforeRevisionID: input.BeforeRevisionID, BeforeDefinitionHash: input.BeforeSnapshot.DefinitionHash,
		AfterRevisionID: input.AfterRevisionID, AfterDefinitionHash: input.AfterSnapshot.DefinitionHash,
	}}
	m.stateInput = QueryStateInputRef{Kind: "none"}
}

func (m *viewMaterializer) indexSnapshot() {
	for _, entity := range m.graph.Entities {
		m.entities[entity.Address] = entity
	}
	for _, relation := range m.graph.Relations {
		m.relations[relation.Address] = relation
	}
	for _, value := range m.snapshot.TypedAST.EntityTypes {
		m.entityTypes[value.Address] = value
	}
	for _, value := range m.snapshot.TypedAST.RelationTypes {
		m.relationTypes[value.Address] = value
	}
	for _, value := range m.snapshot.TypedAST.Layers {
		m.layers[value.Address] = value
	}
	for _, adjacency := range m.graph.Outgoing {
		m.outgoing[adjacency.EntityAddress] = append([]string{}, adjacency.RelationAddresses...)
	}
	for _, adjacency := range m.graph.Incoming {
		m.incoming[adjacency.EntityAddress] = append([]string{}, adjacency.RelationAddresses...)
	}
}

func (m *viewMaterializer) validateQueryResultSubjects() {
	for _, values := range [][]string{
		m.queryResult.SeedEntityAddresses, m.queryResult.ReachedEntityAddresses, m.queryResult.TraversedEntityAddresses,
		m.queryResult.PrimaryEntityAddresses, m.queryResult.SupportEntityAddresses,
	} {
		if !canonicalStableAddressSlice(values) {
			m.addDiag("LDL1801", "stale_revision_or_semantic_hash", "QueryResult Entity collection is not canonical", m.queryResult.QueryAddress, m.input.Recipe.Address)
			return
		}
		for _, address := range values {
			if _, ok := m.entities[address]; !ok {
				m.addDiag("LDL1601", "invalid_query_or_arguments", "QueryResult references an unknown Entity", address, m.input.Recipe.Address)
			}
		}
	}
	for _, values := range [][]string{m.queryResult.PathRelationAddresses, m.queryResult.InducedRelationAddresses, m.queryResult.SelectedRelationAddresses} {
		known := true
		for _, address := range values {
			if _, ok := m.relations[address]; !ok {
				m.addDiag("LDL1601", "invalid_query_or_arguments", "QueryResult references an unknown Relation", address, m.input.Recipe.Address)
				known = false
			}
		}
		if known && !m.canonicalQueryRelationSlice(values) {
			m.addDiag("LDL1801", "stale_revision_or_semantic_hash", "QueryResult Relation collection is not canonical", m.queryResult.QueryAddress, m.input.Recipe.Address)
			return
		}
	}
	visible := viewStringSet(m.materializationEntityAddresses())
	for _, address := range m.relationAddresses() {
		relation := m.relations[address]
		if !visible[relation.FromAddress] || !visible[relation.ToAddress] {
			m.addDiag("LDL1801", "stale_revision_or_semantic_hash", "QueryResult does not close selected Relation endpoints", address, m.input.Recipe.Address)
		}
	}
}

func (m *viewMaterializer) base() ViewDataBase {
	queryAddress := m.queryResult.QueryAddress
	return ViewDataBase{
		Kind: ViewDataKind(m.input.Recipe.Shape.Kind), Category: string(m.input.Recipe.Category), Shape: deepClone(m.input.Recipe.Shape),
		ProjectAddress: m.project, ViewAddress: m.input.Recipe.Address, QueryAddress: &queryAddress,
		Revision: deepClone(m.revision), StatePolicy: string(m.input.Recipe.StateRequirement), StateInput: m.stateInput,
		Source: emptyViewDataSourceRefs(), Diagnostics: sortedDiagnostics(m.queryResult.Diagnostics),
	}
}

func (m *viewMaterializer) sourceRefs() ViewDataSourceRefs {
	refs := emptyViewDataSourceRefs()
	refs.SubjectAddresses = append(refs.SubjectAddresses, m.project, m.input.Recipe.Address)
	if m.queryResult.QueryAddress != "" {
		refs.SubjectAddresses = append(refs.SubjectAddresses, m.queryResult.QueryAddress)
	}
	refs.SubjectAddresses = append(refs.SubjectAddresses, m.input.Recipe.Dependencies.EntityTypeAddresses...)
	refs.SubjectAddresses = append(refs.SubjectAddresses, m.input.Recipe.Dependencies.RelationTypeAddresses...)
	refs.SubjectAddresses = append(refs.SubjectAddresses, m.input.Recipe.Dependencies.ColumnAddresses...)
	refs.EntityAddresses = append(refs.EntityAddresses, m.materializationEntityAddresses()...)
	refs.RelationAddresses = append(refs.RelationAddresses, m.relationAddresses()...)
	refs.State.Reads = append(refs.State.Reads, m.queryResult.StateReads...)
	refs.State.Reads = append(refs.State.Reads, m.sortedDirectStateReads()...)
	for _, address := range refs.EntityAddresses {
		entity := m.entities[address]
		refs.SubjectAddresses = append(refs.SubjectAddresses, entity.TypeAddress)
		refs.LayerAddresses = append(refs.LayerAddresses, entity.LayerAddress)
		for _, row := range entity.Rows {
			refs.RowAddresses = append(refs.RowAddresses, row.Address)
			for _, cell := range row.Values {
				refs.CellRefs = append(refs.CellRefs, ViewDataCellRef{RowAddress: row.Address, ColumnAddress: cell.ColumnAddress})
			}
		}
	}
	for _, address := range refs.RelationAddresses {
		relation := m.relations[address]
		refs.SubjectAddresses = append(refs.SubjectAddresses, relation.TypeAddress)
		for _, row := range relation.Rows {
			refs.RowAddresses = append(refs.RowAddresses, row.Address)
			for _, cell := range row.Values {
				refs.CellRefs = append(refs.CellRefs, ViewDataCellRef{RowAddress: row.Address, ColumnAddress: cell.ColumnAddress})
			}
		}
	}
	if m.input.Recipe.StateRequirement == query.StateNone {
		refs.State.Reads = []StateReadRef{}
	}
	return canonicalViewDataSourceRefs(refs)
}

func (m *viewMaterializer) entitySource(entity graph.Entity) ViewDataSourceRefs {
	refs := emptyViewDataSourceRefs()
	refs.SubjectAddresses = []string{entity.TypeAddress}
	refs.EntityAddresses = []string{entity.Address}
	refs.LayerAddresses = []string{entity.LayerAddress}
	refs.State.Reads = m.stateReadsForSubjects(entity.Address)
	return canonicalViewDataSourceRefs(refs)
}

func (m *viewMaterializer) relationSource(relation graph.Relation) ViewDataSourceRefs {
	refs := emptyViewDataSourceRefs()
	refs.SubjectAddresses = []string{relation.TypeAddress}
	refs.RelationAddresses = []string{relation.Address}
	refs.State.Reads = m.stateReadsForSubjects(relation.Address)
	return canonicalViewDataSourceRefs(refs)
}

func (m *viewMaterializer) rowSource(ownerAddress string, entity bool, row graph.AttributeRow) ViewDataSourceRefs {
	var refs ViewDataSourceRefs
	if entity {
		refs = m.entitySource(m.entities[ownerAddress])
	} else {
		refs = m.relationSource(m.relations[ownerAddress])
	}
	refs.RowAddresses = []string{row.Address}
	for _, cell := range row.Values {
		refs.CellRefs = append(refs.CellRefs, ViewDataCellRef{RowAddress: row.Address, ColumnAddress: cell.ColumnAddress})
	}
	refs.State.Reads = append(refs.State.Reads, m.stateReadsForSubjects(row.Address)...)
	return canonicalViewDataSourceRefs(refs)
}

func (m *viewMaterializer) stateReadsForSubjects(addresses ...string) []StateReadRef {
	allowed := viewStringSet(addresses)
	reads := append([]StateReadRef{}, m.queryResult.StateReads...)
	reads = append(reads, m.sortedDirectStateReads()...)
	return filterStateReads(canonicalStateReads(reads), func(read StateReadRef) bool { return allowed[read.SubjectAddress] })
}

func (m *viewMaterializer) materializationEntityAddresses() []string {
	values := append(append([]string{}, m.queryResult.PrimaryEntityAddresses...), m.queryResult.SupportEntityAddresses...)
	if len(values) == 0 {
		values = append(values, m.queryResult.SeedEntityAddresses...)
		values = append(values, m.queryResult.TraversedEntityAddresses...)
	}
	return sortedUniqueStableAddresses(values)
}

func (m *viewMaterializer) primaryEntityAddresses() []string {
	values := append([]string{}, m.queryResult.PrimaryEntityAddresses...)
	if len(values) == 0 {
		values = append(values, m.queryResult.SeedEntityAddresses...)
	}
	return sortedUniqueStableAddresses(values)
}

func (m *viewMaterializer) relationAddresses() []string {
	values := append([]string{}, m.queryResult.SelectedRelationAddresses...)
	if len(values) == 0 {
		values = append(values, m.queryResult.PathRelationAddresses...)
		values = append(values, m.queryResult.InducedRelationAddresses...)
	}
	return sortedUniqueStableAddresses(values)
}

func (m *viewMaterializer) sortedDirectStateReads() []StateReadRef {
	values := make([]StateReadRef, 0, len(m.directStateReads))
	for value := range m.directStateReads {
		values = append(values, value)
	}
	return canonicalStateReads(values)
}

func (m *viewMaterializer) addDiag(code, key, message, subject, owner string) {
	m.diagnostics = append(m.diagnostics, diagnostic(code, key, message, subject, owner))
}

func (m *viewMaterializer) addWarning(code, key, message, subject, owner string) {
	value := diagnostic(code, key, message, subject, owner)
	value.Severity = "warning"
	m.diagnostics = append(m.diagnostics, value)
}

func hasViewErrorDiagnostics(values []Diagnostic) bool {
	for _, value := range values {
		if value.Severity != "warning" && value.Severity != "info" {
			return true
		}
	}
	return false
}

func rejectedView(diagnostics ...Diagnostic) ViewMaterializationResponse {
	return ViewMaterializationResponse{Status: "rejected", Diagnostics: sortedDiagnostics(diagnostics)}
}

func validQueryViewSnapshot(snapshot Snapshot) bool {
	return validDefinitionSnapshot(snapshot) && snapshot.TypedAST.Graph != nil
}

func validDefinitionSnapshot(snapshot Snapshot) bool {
	return snapshot.TypedAST.Project != nil && snapshot.TypedAST.Project.Address != "" && validSemanticHash(snapshot.DefinitionHash)
}

func validRevisionID(value string) bool {
	return value != "" && definition.NormalizeText(value) == value
}

func viewRecipeInSnapshot(snapshot Snapshot, address string) (CompiledViewRecipe, bool) {
	for _, recipe := range snapshot.TypedAST.Views {
		if recipe.Address == address {
			return recipe, true
		}
	}
	return CompiledViewRecipe{}, false
}

func queryRecipeInSnapshot(snapshot Snapshot, address string) (CompiledQueryRecipe, bool) {
	for _, recipe := range snapshot.TypedAST.Queries {
		if recipe.Address == address {
			return recipe, true
		}
	}
	return CompiledQueryRecipe{}, false
}

func viewArgumentsMatch(expected []view.Argument, actual map[string]TypedScalar) bool {
	if len(expected) != len(actual) {
		return false
	}
	for _, argument := range expected {
		if value, ok := actual[argument.ParameterAddress]; !ok || value != argument.Value {
			return false
		}
	}
	return true
}

func canonicalStableAddressSlice(values []string) bool {
	for index := 1; index < len(values); index++ {
		if compareStableAddressText(values[index-1], values[index]) >= 0 {
			return false
		}
	}
	return values != nil
}

func (m *viewMaterializer) canonicalQueryRelationSlice(values []string) bool {
	if values == nil {
		return false
	}
	for index := 1; index < len(values); index++ {
		if compareRelationTuple(m.relations[values[index-1]], m.relations[values[index]]) >= 0 {
			return false
		}
	}
	return true
}

func setViewDataBase(value *ViewData, base ViewDataBase) {
	switch {
	case value.Diagram != nil:
		value.Diagram.ViewDataBase = base
	case value.Table != nil:
		value.Table.ViewDataBase = base
	case value.Matrix != nil:
		value.Matrix.ViewDataBase = base
	case value.Tree != nil:
		value.Tree.ViewDataBase = base
	case value.Flow != nil:
		value.Flow.ViewDataBase = base
	case value.Context != nil:
		value.Context.ViewDataBase = base
	case value.Diff != nil:
		value.Diff.ViewDataBase = base
	}
}

func viewStringSet(values []string) map[string]bool {
	out := make(map[string]bool, len(values))
	for _, value := range values {
		out[value] = true
	}
	return out
}

func filterStateReads(values []StateReadRef, keep func(StateReadRef) bool) []StateReadRef {
	out := make([]StateReadRef, 0, len(values))
	for _, value := range values {
		if keep(value) {
			out = append(out, value)
		}
	}
	return out
}

func viewItemKey(m *viewMaterializer, kind string, tuple any) string {
	key, err := newViewDataItemKey(kind, tuple)
	if err != nil {
		m.addDiag("LDL1801", "stale_revision_or_semantic_hash", fmt.Sprintf("cannot derive ViewData item key: %v", err), m.input.Recipe.Address, "")
	}
	return key
}

func sortRelationsByAddress(values []graph.Relation) {
	sort.Slice(values, func(i, j int) bool { return compareStableAddressText(values[i].Address, values[j].Address) < 0 })
}
