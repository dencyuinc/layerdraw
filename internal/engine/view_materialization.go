// SPDX-License-Identifier: LicenseRef-LayerDraw-1.0

package engine

import (
	"context"
	"sort"

	"github.com/dencyuinc/layerdraw/internal/engine/internal/compiler/query"
	"github.com/dencyuinc/layerdraw/internal/engine/internal/compiler/semantic/definition"
	"github.com/dencyuinc/layerdraw/internal/engine/internal/compiler/semantic/graph"
	"github.com/dencyuinc/layerdraw/internal/engine/internal/compiler/view"
)

type ViewMaterializationInput struct {
	Recipe      CompiledViewRecipe
	Graph       TypedMasterGraph
	QueryResult QueryResult
}

type ViewMaterializationResponse struct {
	Status      string
	Result      *ViewData
	Diagnostics []Diagnostic
}

type ViewData struct {
	ViewAddress string
	Category    string
	Shape       string
	StatePolicy string
	StateInput  QueryStateInputRef
	Source      ViewDataSourceRefs
	Diagnostics []Diagnostic
	Diagram     *DiagramViewData
	Table       *TableViewData
	Context     *ContextViewData
}

type ViewDataSourceRefs struct {
	QueryAddress      string
	EntityAddresses   []string
	RelationAddresses []string
	LayerAddresses    []string
	RowAddresses      []string
	StateReads        []StateReadRef
}

type DiagramViewData struct {
	Nodes      []DiagramNode
	Edges      []DiagramEdge
	Placements []DiagramPlacement
}

type DiagramNode struct {
	Key            string
	EntityAddress  string
	DisplayName    string
	EntityType     string
	LayerAddress   string
	SourceEntities []string
}

type DiagramEdge struct {
	Key             string
	RelationAddress string
	FromAddress     string
	ToAddress       string
	RelationType    string
	DisplayName     *string
	SourceRelations []string
}

type DiagramPlacement struct {
	EntityAddress string
	X             float64
	Y             float64
	Width         float64
	Height        float64
}

type TableViewData struct {
	Columns []TableViewColumn
	Rows    []TableViewRow
	Sorts   []TableViewSort
}

type TableViewColumn struct {
	ID      string
	Address string
	Label   string
	Source  string
}

type TableViewRow struct {
	Key             string
	SubjectAddress  string
	OwnerAddress    string
	SourceRows      []string
	SourceEntities  []string
	SourceRelations []string
	Cells           []TableViewCell
}

type TableViewCell struct {
	ColumnID        string
	Value           ViewDataValue
	SourceRows      []string
	SourceCells     []ViewDataCellRef
	SourceEntities  []string
	SourceRelations []string
	StateReads      []StateReadRef
}

type ViewDataValue struct {
	Kind      string
	Scalar    *TypedScalar
	Address   *string
	StringSet []string
	Null      bool
}

type ViewDataCellRef struct {
	RowAddress    string
	ColumnAddress string
}

type TableViewSort struct {
	ColumnID  string
	Direction string
	Absent    string
}

type ContextViewData struct {
	Groups []ContextGroup
	Facts  []ContextFact
}

type ContextGroup struct {
	Key       string
	Label     string
	Addresses []string
}

type ContextFact struct {
	Key             string
	SubjectAddress  string
	Kind            string
	Text            string
	SourceEntities  []string
	SourceRelations []string
	SourceRows      []string
}

// MaterializeView converts one compiled View recipe and one QueryResult into
// semantic ViewData. It performs no layout, rendering, export planning, state
// backend access, storage access, clock reads, or network I/O.
func (e Engine) MaterializeView(ctx context.Context, input ViewMaterializationInput) ViewMaterializationResponse {
	if ctx == nil {
		return rejectedView(diagnostic("LDL1801", "stale_revision_or_semantic_hash", "ViewData materialization requires a context", input.Recipe.Address, ""))
	}
	if err := pollContext(ctx, "view"); err != nil {
		return rejectedView(diagnostic("LDL1801", "stale_revision_or_semantic_hash", err.Error(), input.Recipe.Address, ""))
	}
	m := newViewMaterializer(input)
	if !m.validate() {
		return rejectedView(m.diagnostics...)
	}
	result := m.base()
	switch input.Recipe.Shape.Kind {
	case view.ShapeDiagram:
		result.Diagram = m.diagram()
	case view.ShapeTable:
		result.Table = m.table()
	case view.ShapeContext:
		result.Context = m.context()
	default:
		m.addDiag("LDL1701", "unsupported_view_shape_or_export", "View shape materialization is not implemented", input.Recipe.Address, "")
		return rejectedView(m.diagnostics...)
	}
	result.Diagnostics = sortedDiagnostics(append(result.Diagnostics, m.diagnostics...))
	result.Source = m.sourceRefs()
	if err := pollContext(ctx, "view"); err != nil {
		return rejectedView(diagnostic("LDL1801", "stale_revision_or_semantic_hash", err.Error(), input.Recipe.Address, ""))
	}
	return ViewMaterializationResponse{Status: "ok", Result: &result}
}

type viewMaterializer struct {
	input       ViewMaterializationInput
	entities    map[string]graph.Entity
	relations   map[string]graph.Relation
	outgoing    map[string][]string
	incoming    map[string][]string
	diagnostics []Diagnostic
	stateReads  map[StateReadRef]bool
}

func newViewMaterializer(input ViewMaterializationInput) *viewMaterializer {
	m := &viewMaterializer{
		input: input, entities: map[string]graph.Entity{}, relations: map[string]graph.Relation{},
		outgoing: map[string][]string{}, incoming: map[string][]string{}, stateReads: map[StateReadRef]bool{},
	}
	for _, entity := range input.Graph.Entities {
		m.entities[entity.Address] = entity
	}
	for _, relation := range input.Graph.Relations {
		m.relations[relation.Address] = relation
	}
	for _, adjacency := range input.Graph.Outgoing {
		m.outgoing[adjacency.EntityAddress] = append([]string{}, adjacency.RelationAddresses...)
	}
	for _, adjacency := range input.Graph.Incoming {
		m.incoming[adjacency.EntityAddress] = append([]string{}, adjacency.RelationAddresses...)
	}
	return m
}

func (m *viewMaterializer) validate() bool {
	if m.input.Recipe.Address == "" {
		m.addDiag("LDL1701", "unsupported_view_shape_or_export", "View recipe address is required", "", "")
	}
	if m.input.Recipe.Source.Kind != view.SourceQuery || m.input.Recipe.Source.Query == nil {
		m.addDiag("LDL1701", "unsupported_view_shape_or_export", "only Query-backed ViewData materialization is supported", m.input.Recipe.Address, "")
	} else if m.input.Recipe.Source.Query.QueryAddress != m.input.QueryResult.QueryAddress {
		m.addDiag("LDL1601", "invalid_query_or_arguments", "View source Query does not match QueryResult", m.input.QueryResult.QueryAddress, m.input.Recipe.Address)
	}
	if m.input.Recipe.StateRequirement == query.StateRequired {
		m.addDiag("LDL1604", "required_state_snapshot_missing", "required StateQuerySnapshot is absent", m.input.Recipe.Address, "")
	}
	for _, address := range m.allEntityAddresses() {
		if _, ok := m.entities[address]; !ok {
			m.addDiag("LDL1601", "invalid_query_or_arguments", "QueryResult references an unknown entity", address, m.input.Recipe.Address)
		}
	}
	for _, address := range m.allRelationAddresses() {
		if _, ok := m.relations[address]; !ok {
			m.addDiag("LDL1601", "invalid_query_or_arguments", "QueryResult references an unknown relation", address, m.input.Recipe.Address)
		}
	}
	return len(m.diagnostics) == 0
}

func (m *viewMaterializer) base() ViewData {
	return ViewData{
		ViewAddress: m.input.Recipe.Address,
		Category:    string(m.input.Recipe.Category),
		Shape:       string(m.input.Recipe.Shape.Kind),
		StatePolicy: string(m.input.Recipe.StateRequirement),
		StateInput:  m.input.QueryResult.StateInput,
		Diagnostics: sortedDiagnostics(m.input.QueryResult.Diagnostics),
	}
}

func (m *viewMaterializer) diagram() *DiagramViewData {
	entityAddresses := m.visibleEntityAddresses()
	relationAddresses := m.visibleRelationAddresses()
	nodes := make([]DiagramNode, 0, len(entityAddresses))
	for _, address := range entityAddresses {
		entity := m.entities[address]
		nodes = append(nodes, DiagramNode{Key: "entity:" + address, EntityAddress: address, DisplayName: entity.DisplayName, EntityType: entity.TypeAddress, LayerAddress: entity.LayerAddress, SourceEntities: []string{address}})
	}
	edges := make([]DiagramEdge, 0, len(relationAddresses))
	for _, address := range relationAddresses {
		relation := m.relations[address]
		edges = append(edges, DiagramEdge{
			Key: "relation:" + address, RelationAddress: address, FromAddress: relation.FromAddress, ToAddress: relation.ToAddress,
			RelationType: relation.TypeAddress, DisplayName: cloneViewStringPointer(relation.DisplayName), SourceRelations: []string{address},
		})
	}
	placements := []DiagramPlacement{}
	if shape := m.input.Recipe.Shape.Diagram; shape != nil {
		visible := viewStringSet(entityAddresses)
		for _, placement := range shape.Placements {
			if visible[placement.EntityAddress] {
				placements = append(placements, DiagramPlacement{EntityAddress: placement.EntityAddress, X: placement.X, Y: placement.Y, Width: placement.Width, Height: placement.Height})
			}
		}
		sort.Slice(placements, func(i, j int) bool { return placements[i].EntityAddress < placements[j].EntityAddress })
	}
	return &DiagramViewData{Nodes: nodes, Edges: edges, Placements: placements}
}

func (m *viewMaterializer) table() *TableViewData {
	shape := m.input.Recipe.Shape.Table
	if shape == nil {
		m.addDiag("LDL1701", "unsupported_view_shape_or_export", "Table View shape is missing", m.input.Recipe.Address, "")
		return &TableViewData{Columns: []TableViewColumn{}, Rows: []TableViewRow{}, Sorts: []TableViewSort{}}
	}
	columns := m.tableColumns(shape)
	rows := m.tableRows(shape, columns)
	sorts := make([]TableViewSort, len(shape.Sorts))
	for i, sortValue := range shape.Sorts {
		sorts[i] = TableViewSort{ColumnID: sortValue.ColumnID, Direction: string(sortValue.Direction), Absent: string(sortValue.Absent)}
	}
	return &TableViewData{Columns: columns, Rows: rows, Sorts: sorts}
}

func (m *viewMaterializer) tableColumns(shape *view.TableShape) []TableViewColumn {
	columns := []TableViewColumn{}
	if shape.IncludeEntityID {
		columns = append(columns, TableViewColumn{ID: "entity_id", Label: "Entity ID", Source: "field:id"})
	}
	if shape.IncludeType {
		columns = append(columns, TableViewColumn{ID: "type", Label: "Type", Source: "field:type"})
	}
	if shape.IncludeLayer {
		columns = append(columns, TableViewColumn{ID: "layer", Label: "Layer", Source: "field:layer"})
	}
	for _, column := range shape.Columns {
		label := column.ID
		if column.Label != nil {
			label = *column.Label
		}
		columns = append(columns, TableViewColumn{ID: column.ID, Address: column.Address, Label: label, Source: string(column.Source.Kind)})
	}
	return columns
}

func (m *viewMaterializer) tableRows(shape *view.TableShape, columns []TableViewColumn) []TableViewRow {
	switch shape.RowSource {
	case view.RowsEntity:
		return m.entityTableRows(shape, columns, false)
	case view.RowsEntityRows:
		return m.entityTableRows(shape, columns, true)
	case view.RowsRelation:
		return m.relationTableRows(shape, columns, false)
	case view.RowsRelationRows, view.RowsAutomaticRelations:
		return m.relationTableRows(shape, columns, true)
	default:
		m.addDiag("LDL1701", "unsupported_view_shape_or_export", "unsupported Table row source", m.input.Recipe.Address, "")
		return []TableViewRow{}
	}
}

func (m *viewMaterializer) entityTableRows(shape *view.TableShape, columns []TableViewColumn, rowGrain bool) []TableViewRow {
	addresses := m.visibleEntityAddresses()
	if shape.EntityTypeAddresses != nil {
		allowed := viewStringSet(*shape.EntityTypeAddresses)
		addresses = filterStrings(addresses, func(address string) bool { return allowed[m.entities[address].TypeAddress] })
	}
	rows := []TableViewRow{}
	for _, address := range addresses {
		entity := m.entities[address]
		if !rowGrain {
			rows = append(rows, m.entityTableRow(shape, columns, entity, nil))
			continue
		}
		for _, row := range entity.Rows {
			rowCopy := row
			rows = append(rows, m.entityTableRow(shape, columns, entity, &rowCopy))
		}
	}
	return rows
}

func (m *viewMaterializer) entityTableRow(shape *view.TableShape, columns []TableViewColumn, entity graph.Entity, row *graph.AttributeRow) TableViewRow {
	key := "entity:" + entity.Address
	sourceRows := []string{}
	if row != nil {
		key = "entity-row:" + row.Address
		sourceRows = []string{row.Address}
	}
	out := TableViewRow{Key: key, SubjectAddress: entity.Address, OwnerAddress: entity.Address, SourceRows: sourceRows, SourceEntities: []string{entity.Address}, Cells: []TableViewCell{}}
	for _, column := range columns {
		out.Cells = append(out.Cells, m.entityCell(shape, column, entity, row))
	}
	return out
}

func (m *viewMaterializer) entityCell(shape *view.TableShape, column TableViewColumn, entity graph.Entity, row *graph.AttributeRow) TableViewCell {
	cell := TableViewCell{ColumnID: column.ID, SourceEntities: []string{entity.Address}}
	switch column.ID {
	case "entity_id":
		cell.Value = addressViewValue(entity.Address)
		return cell
	case "type":
		cell.Value = addressViewValue(entity.TypeAddress)
		return cell
	case "layer":
		cell.Value = addressViewValue(entity.LayerAddress)
		return cell
	}
	source := findTableColumn(shape.Columns, column.ID)
	if source == nil {
		cell.Value = nullViewValue()
		return cell
	}
	switch source.Source.Kind {
	case view.ColumnField:
		cell.Value = optionalViewValue(entityFieldValue(entity, source.Source.Field))
	case view.ColumnAttribute:
		if row != nil {
			cell.Value, cell.SourceCells = rowCellViewValue(*row, source.Source.ColumnAddresses)
			cell.SourceRows = []string{row.Address}
		} else {
			cell.Value, cell.SourceCells, cell.SourceRows = firstRowCellViewValue(entity.Rows, source.Source.ColumnAddresses)
		}
	case view.ColumnDerivedCount:
		cell.Value = scalarViewValue(definition.Scalar{Type: definition.ScalarInteger, Int: int64(m.derivedCount(entity.Address, source.Source.Direction, source.Source.RelationTypeAddresses))})
	case view.ColumnState:
		read := StateReadRef{SubjectAddress: entity.Address, FieldPath: string(source.Source.StateFieldPath)}
		m.stateReads[read] = true
		cell.StateReads = []StateReadRef{read}
		cell.Value = nullViewValue()
	default:
		cell.Value = nullViewValue()
	}
	return cell
}

func (m *viewMaterializer) relationTableRows(shape *view.TableShape, columns []TableViewColumn, rowGrain bool) []TableViewRow {
	addresses := m.visibleRelationAddresses()
	rows := []TableViewRow{}
	for _, address := range addresses {
		relation := m.relations[address]
		if !rowGrain {
			rows = append(rows, m.relationTableRow(shape, columns, relation, nil))
			continue
		}
		for _, row := range relation.Rows {
			rowCopy := row
			rows = append(rows, m.relationTableRow(shape, columns, relation, &rowCopy))
		}
	}
	return rows
}

func (m *viewMaterializer) relationTableRow(shape *view.TableShape, columns []TableViewColumn, relation graph.Relation, row *graph.AttributeRow) TableViewRow {
	key := "relation:" + relation.Address
	sourceRows := []string{}
	if row != nil {
		key = "relation-row:" + row.Address
		sourceRows = []string{row.Address}
	}
	out := TableViewRow{Key: key, SubjectAddress: relation.Address, OwnerAddress: relation.Address, SourceRows: sourceRows, SourceRelations: []string{relation.Address}, Cells: []TableViewCell{}}
	for _, column := range columns {
		out.Cells = append(out.Cells, m.relationCell(shape, column, relation, row))
	}
	return out
}

func (m *viewMaterializer) relationCell(shape *view.TableShape, column TableViewColumn, relation graph.Relation, row *graph.AttributeRow) TableViewCell {
	cell := TableViewCell{ColumnID: column.ID, SourceRelations: []string{relation.Address}}
	source := findTableColumn(shape.Columns, column.ID)
	if source == nil {
		cell.Value = nullViewValue()
		return cell
	}
	switch source.Source.Kind {
	case view.ColumnField:
		cell.Value = optionalViewValue(relationFieldValue(relation, source.Source.Field))
	case view.ColumnRelationEndpoint:
		if source.Source.Endpoint == definition.ProjectionEndpointFrom {
			cell.Value = addressViewValue(relation.FromAddress)
		} else {
			cell.Value = addressViewValue(relation.ToAddress)
		}
	case view.ColumnAttribute:
		if row != nil {
			cell.Value, cell.SourceCells = rowCellViewValue(*row, source.Source.ColumnAddresses)
			cell.SourceRows = []string{row.Address}
		} else {
			cell.Value, cell.SourceCells, cell.SourceRows = firstRowCellViewValue(relation.Rows, source.Source.ColumnAddresses)
		}
	case view.ColumnState:
		read := StateReadRef{SubjectAddress: relation.Address, FieldPath: string(source.Source.StateFieldPath)}
		m.stateReads[read] = true
		cell.StateReads = []StateReadRef{read}
		cell.Value = nullViewValue()
	default:
		cell.Value = nullViewValue()
	}
	return cell
}

func (m *viewMaterializer) context() *ContextViewData {
	shape := m.input.Recipe.Shape.Context
	if shape == nil {
		m.addDiag("LDL1701", "unsupported_view_shape_or_export", "Context View shape is missing", m.input.Recipe.Address, "")
		return &ContextViewData{Groups: []ContextGroup{}, Facts: []ContextFact{}}
	}
	groups := map[string]ContextGroup{}
	facts := []ContextFact{}
	for _, address := range m.visibleEntityAddresses() {
		entity := m.entities[address]
		group := contextGroupForEntity(entity, shape.GroupBy)
		if existing, ok := groups[group.Key]; ok {
			existing.Addresses = append(existing.Addresses, group.Addresses...)
			groups[group.Key] = existing
		} else {
			groups[group.Key] = group
		}
		facts = append(facts, ContextFact{Key: "entity:" + address, SubjectAddress: address, Kind: "entity", Text: entity.DisplayName, SourceEntities: []string{address}})
		if shape.IncludeEntityRows {
			for _, row := range entity.Rows {
				facts = append(facts, ContextFact{Key: "entity-row:" + row.Address, SubjectAddress: row.Address, Kind: "entity_row", Text: row.ID, SourceEntities: []string{address}, SourceRows: []string{row.Address}})
			}
		}
	}
	for _, address := range m.visibleRelationAddresses() {
		relation := m.relations[address]
		text := relation.ID
		if relation.DisplayName != nil {
			text = *relation.DisplayName
		}
		facts = append(facts, ContextFact{Key: "relation:" + address, SubjectAddress: address, Kind: "relation", Text: text, SourceRelations: []string{address}})
		if shape.IncludeRelationRows {
			for _, row := range relation.Rows {
				facts = append(facts, ContextFact{Key: "relation-row:" + row.Address, SubjectAddress: row.Address, Kind: "relation_row", Text: row.ID, SourceRelations: []string{address}, SourceRows: []string{row.Address}})
			}
		}
	}
	groupList := make([]ContextGroup, 0, len(groups))
	for _, group := range groups {
		group.Addresses = sortedUniqueStrings(group.Addresses)
		groupList = append(groupList, group)
	}
	sort.Slice(groupList, func(i, j int) bool { return groupList[i].Key < groupList[j].Key })
	return &ContextViewData{Groups: groupList, Facts: facts}
}

func (m *viewMaterializer) sourceRefs() ViewDataSourceRefs {
	entitySet := map[string]bool{}
	layerSet := map[string]bool{}
	for _, address := range m.allEntityAddresses() {
		if entity, ok := m.entities[address]; ok {
			entitySet[address] = true
			layerSet[entity.LayerAddress] = true
		}
	}
	relationSet := map[string]bool{}
	for _, address := range m.allRelationAddresses() {
		if relation, ok := m.relations[address]; ok {
			relationSet[address] = true
			entitySet[relation.FromAddress] = true
			entitySet[relation.ToAddress] = true
		}
	}
	rowSet := map[string]bool{}
	for _, address := range sortedSet(entitySet) {
		for _, row := range m.entities[address].Rows {
			rowSet[row.Address] = true
		}
	}
	for _, address := range sortedSet(relationSet) {
		for _, row := range m.relations[address].Rows {
			rowSet[row.Address] = true
		}
	}
	return ViewDataSourceRefs{
		QueryAddress:      m.input.QueryResult.QueryAddress,
		EntityAddresses:   sortedSet(entitySet),
		RelationAddresses: sortedSet(relationSet),
		LayerAddresses:    sortedSet(layerSet),
		RowAddresses:      sortedSet(rowSet),
		StateReads:        mergeStateReads(m.input.QueryResult.StateReads, m.sortedStateReads()),
	}
}

func (m *viewMaterializer) visibleEntityAddresses() []string {
	addresses := sortedUniqueStrings(append(append([]string{}, m.input.QueryResult.PrimaryEntityAddresses...), m.input.QueryResult.SupportEntityAddresses...))
	if len(addresses) != 0 {
		return addresses
	}
	return sortedUniqueStrings(append(append(append([]string{}, m.input.QueryResult.SeedEntityAddresses...), m.input.QueryResult.ReachedEntityAddresses...), m.input.QueryResult.TraversedEntityAddresses...))
}

func (m *viewMaterializer) visibleRelationAddresses() []string {
	return sortedUniqueStrings(append(append([]string{}, m.input.QueryResult.SelectedRelationAddresses...), append(m.input.QueryResult.PathRelationAddresses, m.input.QueryResult.InducedRelationAddresses...)...))
}

func (m *viewMaterializer) allEntityAddresses() []string {
	return sortedUniqueStrings(append(append(append(append([]string{}, m.input.QueryResult.SeedEntityAddresses...), m.input.QueryResult.ReachedEntityAddresses...), m.input.QueryResult.TraversedEntityAddresses...), append(m.input.QueryResult.PrimaryEntityAddresses, m.input.QueryResult.SupportEntityAddresses...)...))
}

func (m *viewMaterializer) allRelationAddresses() []string {
	return sortedUniqueStrings(append(append(append([]string{}, m.input.QueryResult.PathRelationAddresses...), m.input.QueryResult.InducedRelationAddresses...), m.input.QueryResult.SelectedRelationAddresses...))
}

func (m *viewMaterializer) derivedCount(entityAddress string, direction definition.TraversalDirection, relationTypes *[]string) int {
	allowed := map[string]bool{}
	if relationTypes != nil {
		allowed = viewStringSet(*relationTypes)
	}
	count := 0
	visit := func(addresses []string) {
		for _, relationAddress := range addresses {
			relation := m.relations[relationAddress]
			if len(allowed) != 0 && !allowed[relation.TypeAddress] {
				continue
			}
			count++
		}
	}
	if direction == definition.TraversalOutgoing || direction == definition.TraversalBoth {
		visit(m.outgoing[entityAddress])
	}
	if direction == definition.TraversalIncoming || direction == definition.TraversalBoth {
		visit(m.incoming[entityAddress])
	}
	return count
}

func (m *viewMaterializer) sortedStateReads() []StateReadRef {
	reads := make([]StateReadRef, 0, len(m.stateReads))
	for read := range m.stateReads {
		reads = append(reads, read)
	}
	sort.Slice(reads, func(i, j int) bool {
		if reads[i].SubjectAddress != reads[j].SubjectAddress {
			return reads[i].SubjectAddress < reads[j].SubjectAddress
		}
		return reads[i].FieldPath < reads[j].FieldPath
	})
	return reads
}

func (m *viewMaterializer) addDiag(code, key, message, subject, owner string) {
	m.diagnostics = append(m.diagnostics, diagnostic(code, key, message, subject, owner))
}

func rejectedView(diagnostics ...Diagnostic) ViewMaterializationResponse {
	return ViewMaterializationResponse{Status: "rejected", Diagnostics: sortedDiagnostics(diagnostics)}
}

func findTableColumn(columns []view.TableColumn, id string) *view.TableColumn {
	for i := range columns {
		if columns[i].ID == id {
			return &columns[i]
		}
	}
	return nil
}

func optionalViewValue(value optionalScalar) ViewDataValue {
	if !value.present {
		return nullViewValue()
	}
	if value.address != nil {
		return addressViewValue(*value.address)
	}
	if len(value.strings) != 0 {
		return ViewDataValue{Kind: "string_set", StringSet: sortedUniqueStrings(value.strings)}
	}
	return scalarViewValue(value.value)
}

func scalarViewValue(value definition.Scalar) ViewDataValue {
	copied := value
	return ViewDataValue{Kind: "scalar", Scalar: &copied}
}

func addressViewValue(value string) ViewDataValue {
	copied := value
	return ViewDataValue{Kind: "stable_address", Address: &copied}
}

func nullViewValue() ViewDataValue {
	return ViewDataValue{Kind: "null", Null: true}
}

func rowCellViewValue(row graph.AttributeRow, columnAddresses []string) (ViewDataValue, []ViewDataCellRef) {
	allowed := viewStringSet(columnAddresses)
	for _, cell := range row.Values {
		if allowed[cell.ColumnAddress] {
			return scalarViewValue(cell.Value), []ViewDataCellRef{{RowAddress: row.Address, ColumnAddress: cell.ColumnAddress}}
		}
	}
	return nullViewValue(), nil
}

func firstRowCellViewValue(rows []graph.AttributeRow, columnAddresses []string) (ViewDataValue, []ViewDataCellRef, []string) {
	for _, row := range rows {
		value, cells := rowCellViewValue(row, columnAddresses)
		if !value.Null {
			return value, cells, []string{row.Address}
		}
	}
	return nullViewValue(), nil, nil
}

func contextGroupForEntity(entity graph.Entity, groupBy view.ContextGroupBy) ContextGroup {
	switch groupBy {
	case view.ContextGroupLayer:
		return ContextGroup{Key: "layer:" + entity.LayerAddress, Label: entity.LayerAddress, Addresses: []string{entity.Address}}
	case view.ContextGroupEntityType:
		return ContextGroup{Key: "entity_type:" + entity.TypeAddress, Label: entity.TypeAddress, Addresses: []string{entity.Address}}
	default:
		return ContextGroup{Key: "all", Label: "All", Addresses: []string{entity.Address}}
	}
}

func mergeStateReads(left, right []StateReadRef) []StateReadRef {
	seen := map[StateReadRef]bool{}
	for _, read := range left {
		seen[read] = true
	}
	for _, read := range right {
		seen[read] = true
	}
	reads := make([]StateReadRef, 0, len(seen))
	for read := range seen {
		reads = append(reads, read)
	}
	sort.Slice(reads, func(i, j int) bool {
		if reads[i].SubjectAddress != reads[j].SubjectAddress {
			return reads[i].SubjectAddress < reads[j].SubjectAddress
		}
		return reads[i].FieldPath < reads[j].FieldPath
	})
	return reads
}

func sortedUniqueStrings(values []string) []string {
	seen := map[string]bool{}
	for _, value := range values {
		if value != "" {
			seen[value] = true
		}
	}
	return sortedSet(seen)
}

func viewStringSet(values []string) map[string]bool {
	out := map[string]bool{}
	for _, value := range values {
		out[value] = true
	}
	return out
}

func sortedSet(values map[string]bool) []string {
	out := make([]string, 0, len(values))
	for value := range values {
		out = append(out, value)
	}
	sort.Strings(out)
	return out
}

func filterStrings(values []string, keep func(string) bool) []string {
	out := []string{}
	for _, value := range values {
		if keep(value) {
			out = append(out, value)
		}
	}
	return out
}

func cloneViewStringPointer(value *string) *string {
	if value == nil {
		return nil
	}
	copied := *value
	return &copied
}
