// SPDX-License-Identifier: LicenseRef-LayerDraw-1.0

package engine

import (
	"sort"

	"github.com/dencyuinc/layerdraw/internal/engine/internal/compiler/query"
	"github.com/dencyuinc/layerdraw/internal/engine/internal/compiler/semantic/definition"
	"github.com/dencyuinc/layerdraw/internal/engine/internal/compiler/semantic/graph"
	"github.com/dencyuinc/layerdraw/internal/engine/internal/compiler/view"
)

type tableSourceRow struct {
	identity   []string
	entity     *graph.Entity
	relation   *graph.Relation
	row        *graph.AttributeRow
	projection *definition.TableProjection
}

type materializedTableRow struct {
	value      TableRow
	identities [][]string
	groupKey   []any
}

func (m *viewMaterializer) table(base ViewDataBase) *TableViewData {
	shape := m.input.Recipe.Shape.Table
	if shape == nil {
		m.addDiag("LDL1701", "invalid_view_source_category_or_shape", "Table View shape is missing", m.input.Recipe.Address, "")
		return &TableViewData{ViewDataBase: base, Columns: []TableColumn{}, Rows: []TableRow{}}
	}
	sources := m.tableSourceRows(shape)
	columns := m.tableColumns(shape, sources)
	rows := m.materializeTableRows(shape, columns, sources)
	return &TableViewData{ViewDataBase: base, Columns: columns, Rows: rows}
}

func (m *viewMaterializer) tableSourceRows(shape *view.TableShape) []tableSourceRow {
	switch shape.RowSource {
	case view.RowsEntity, view.RowsEntityRows:
		return m.entityTableSourceRows(shape)
	case view.RowsRelation, view.RowsRelationRows:
		return m.relationTableSourceRows(shape)
	case view.RowsAutomaticRelations:
		return m.automaticRelationTableSourceRows()
	default:
		m.addDiag("LDL1701", "invalid_view_source_category_or_shape", "Table row source is invalid", m.input.Recipe.Address, "")
		return []tableSourceRow{}
	}
}

func (m *viewMaterializer) entityTableSourceRows(shape *view.TableShape) []tableSourceRow {
	addresses := m.primaryEntityAddresses()
	if shape.EntityTypeAddresses != nil {
		allowed := viewStringSet(*shape.EntityTypeAddresses)
		filtered := make([]string, 0, len(addresses))
		for _, address := range addresses {
			entity, ok := m.entities[address]
			if !ok {
				m.addDiag("LDL1702", "view_materialization_conflict", "Table Entity is absent from the immutable graph", address, m.input.Recipe.Address)
				continue
			}
			if allowed[entity.TypeAddress] {
				filtered = append(filtered, address)
			}
		}
		addresses = filtered
	}
	rows := []tableSourceRow{}
	for _, address := range addresses {
		entity, ok := m.entities[address]
		if !ok {
			m.addDiag("LDL1702", "view_materialization_conflict", "Table Entity is absent from the immutable graph", address, m.input.Recipe.Address)
			continue
		}
		entityCopy := entity
		if shape.RowSource == view.RowsEntity {
			rows = append(rows, tableSourceRow{identity: []string{entity.Address}, entity: &entityCopy})
			continue
		}
		for index := range entity.Rows {
			rowCopy := entity.Rows[index]
			rows = append(rows, tableSourceRow{identity: []string{entity.Address, rowCopy.Address}, entity: &entityCopy, row: &rowCopy})
		}
	}
	return rows
}

func (m *viewMaterializer) relationTableSourceRows(shape *view.TableShape) []tableSourceRow {
	rows := []tableSourceRow{}
	for _, address := range m.relationAddresses() {
		relation, ok := m.relations[address]
		if !ok {
			m.addDiag("LDL1702", "view_materialization_conflict", "Table Relation is absent from the immutable graph", address, m.input.Recipe.Address)
			continue
		}
		relationCopy := relation
		if shape.RowSource == view.RowsRelation {
			rows = append(rows, tableSourceRow{identity: []string{relation.Address}, relation: &relationCopy})
			continue
		}
		for index := range relation.Rows {
			rowCopy := relation.Rows[index]
			rows = append(rows, tableSourceRow{identity: []string{relation.Address, rowCopy.Address}, relation: &relationCopy, row: &rowCopy})
		}
	}
	return rows
}

func (m *viewMaterializer) automaticRelationTableSourceRows() []tableSourceRow {
	rows := []tableSourceRow{}
	for _, address := range m.relationAddresses() {
		relation, ok := m.relations[address]
		if !ok {
			m.addDiag("LDL1702", "view_materialization_conflict", "automatic Table Relation is absent from the immutable graph", address, m.input.Recipe.Address)
			continue
		}
		projection, valid := m.effectiveTableProjection(relation.TypeAddress)
		if !valid {
			continue
		}
		relationCopy := relation
		projectionCopy := projection
		appendRelation := func() {
			rows = append(rows, tableSourceRow{identity: []string{relation.Address}, relation: &relationCopy, projection: &projectionCopy})
		}
		appendRows := func() {
			for index := range relation.Rows {
				rowCopy := relation.Rows[index]
				rows = append(rows, tableSourceRow{identity: []string{relation.Address, rowCopy.Address}, relation: &relationCopy, row: &rowCopy, projection: &projectionCopy})
			}
		}
		switch projection.RowMode {
		case definition.TableRowsRelation:
			appendRelation()
		case definition.TableRowsRelationRows:
			appendRows()
		case definition.TableRowsAutomatic:
			if len(relation.Rows) == 0 {
				appendRelation()
			} else {
				appendRows()
			}
		default:
			m.addDiag("LDL1504", "invalid_projection_contract", "effective Table row mode is invalid", relation.TypeAddress, m.input.Recipe.Address)
		}
	}
	return rows
}

func (m *viewMaterializer) tableColumns(shape *view.TableShape, sources []tableSourceRow) []TableColumn {
	columns := []TableColumn{}
	seen := map[string]bool{}
	appendFixed := func(id, label, valueType string) {
		if seen[id] {
			m.addDiag("LDL1701", "invalid_view_source_category_or_shape", "Table contains a duplicate fixed Column", id, m.input.Recipe.Address)
			return
		}
		seen[id] = true
		columns = append(columns, TableColumn{
			Key: viewItemKey(m, "table-column", []any{m.input.Recipe.Address, id}),
			ID:  id, Label: label, ValueType: valueType, EnumValues: []string{}, SourceColumnAddresses: []string{},
		})
	}
	if shape.IncludeEntityID {
		appendFixed("entity_id", "id", string(definition.ScalarString))
	}
	if shape.IncludeType {
		appendFixed("entity_type", "type", string(view.TableValueStableAddress))
	}
	if shape.IncludeLayer {
		appendFixed("entity_layer", "layer", string(view.TableValueStableAddress))
	}
	if shape.RowSource == view.RowsAutomaticRelations {
		automatic := viewStringSet(shape.AutomaticRelationColumns)
		for _, fixed := range []struct{ id, label string }{{"from", "from"}, {"to", "to"}, {"relation_type", "relation_type"}} {
			if automatic[fixed.id] {
				appendFixed(fixed.id, fixed.label, string(view.TableValueStableAddress))
			}
		}
	}
	for _, source := range shape.Columns {
		if source.ID == "" || source.Address == "" || seen[source.ID] {
			m.addDiag("LDL1701", "invalid_view_source_category_or_shape", "Table contains an invalid or duplicate named Column", source.Address, m.input.Recipe.Address)
			continue
		}
		seen[source.ID] = true
		label := source.ID
		if source.Label != nil {
			label = *source.Label
		}
		address := source.Address
		column := TableColumn{
			Key: viewItemKey(m, "table-column", []any{m.input.Recipe.Address, source.Address}),
			ID:  source.ID, Address: &address, Label: label, ValueType: tableValueTypeName(source.ValueType),
			EnumValues: []string{}, SourceColumnAddresses: []string{},
		}
		if source.ValueType.ScalarType == definition.ScalarEnum {
			column.EnumValues = append([]string{}, source.ValueType.EnumValues...)
		}
		if source.Source.Kind == view.ColumnAttribute {
			column.SourceColumnAddresses = sortedUniqueStableAddresses(source.Source.ColumnAddresses)
		}
		if source.Source.Kind == view.ColumnState {
			path := string(source.Source.StateFieldPath)
			column.StateFieldPath = &path
		}
		columns = append(columns, column)
	}
	if shape.RowSource != view.RowsAutomaticRelations {
		return columns
	}
	dynamic := map[string]definition.Column{}
	for _, source := range sources {
		if source.row == nil {
			continue
		}
		for _, cell := range source.row.Values {
			column, ok := m.relationColumn(cell.ColumnAddress)
			if !ok {
				m.addDiag("LDL1702", "view_materialization_conflict", "automatic Table cell references an unknown RelationType Column", cell.ColumnAddress, m.input.Recipe.Address)
				continue
			}
			dynamic[column.Address] = column
		}
	}
	addresses := make([]string, 0, len(dynamic))
	for address := range dynamic {
		addresses = append(addresses, address)
	}
	sort.Slice(addresses, func(i, j int) bool { return compareStableAddressText(addresses[i], addresses[j]) < 0 })
	for _, address := range addresses {
		if seen[address] {
			m.addDiag("LDL1701", "invalid_view_source_category_or_shape", "dynamic Table Column identity conflicts with another Column", address, m.input.Recipe.Address)
			continue
		}
		seen[address] = true
		definitionColumn := dynamic[address]
		column := TableColumn{
			Key: viewItemKey(m, "table-column", []any{m.input.Recipe.Address, address}),
			ID:  address, Label: definitionColumn.DisplayName, ValueType: string(definitionColumn.ValueType),
			EnumValues: []string{}, SourceColumnAddresses: []string{address},
		}
		if definitionColumn.ValueType == definition.ScalarEnum {
			column.EnumValues = append([]string{}, definitionColumn.EnumValues...)
		}
		columns = append(columns, column)
	}
	return columns
}

func tableValueTypeName(value view.TableValueType) string {
	switch value.Kind {
	case view.TableValueScalar:
		return string(value.ScalarType)
	case view.TableValueStableAddress:
		return "stable_address"
	case view.TableValueStringSet:
		return "string_set"
	default:
		return ""
	}
}

func (m *viewMaterializer) materializeTableRows(shape *view.TableShape, columns []TableColumn, sources []tableSourceRow) []TableRow {
	raw := make([]materializedTableRow, 0, len(sources))
	for _, source := range sources {
		value := TableRow{Cells: make(map[string]TableCell, len(columns)), Source: m.tableSourceRowSource(source)}
		for _, column := range columns {
			cell := m.tableSourceCell(shape, column, source)
			value.Cells[column.Key] = cell
			value.Source = mergeViewDataSourceRefs(value.Source, cell.Source)
		}
		raw = append(raw, materializedTableRow{value: value, identities: [][]string{append([]string{}, source.identity...)}})
	}
	rows := m.finalizeTableRows(shape, columns, raw)
	result := make([]TableRow, len(rows))
	for index := range rows {
		result[index] = rows[index].value
	}
	return result
}

func (m *viewMaterializer) tableSourceCell(shape *view.TableShape, column TableColumn, source tableSourceRow) TableCell {
	baseSource := m.tableSourceIdentitySource(source)
	if source.entity != nil {
		switch column.ID {
		case "entity_id":
			return presentTableCell(scalarViewValue(definition.Scalar{Type: definition.ScalarString, String: source.entity.ID}), baseSource)
		case "entity_type":
			return presentTableCell(addressViewValue(source.entity.TypeAddress), baseSource)
		case "entity_layer":
			return presentTableCell(addressViewValue(source.entity.LayerAddress), baseSource)
		}
	}
	if shape.RowSource == view.RowsAutomaticRelations && source.relation != nil && source.projection != nil {
		switch column.ID {
		case "from":
			if source.projection.IncludeFrom {
				return presentTableCell(addressViewValue(source.relation.FromAddress), baseSource)
			}
			return absentTableCell(baseSource)
		case "to":
			if source.projection.IncludeTo {
				return presentTableCell(addressViewValue(source.relation.ToAddress), baseSource)
			}
			return absentTableCell(baseSource)
		case "relation_type":
			if source.projection.IncludeRelationType {
				return presentTableCell(addressViewValue(source.relation.TypeAddress), baseSource)
			}
			return absentTableCell(baseSource)
		}
		if len(column.SourceColumnAddresses) == 1 && column.ID == column.SourceColumnAddresses[0] {
			return m.dynamicRelationAttributeCell(column.SourceColumnAddresses[0], source)
		}
	}
	definitionColumn := findTableColumn(shape.Columns, column.ID)
	if definitionColumn == nil {
		return absentTableCell(baseSource)
	}
	return m.namedTableCell(*definitionColumn, source, baseSource)
}

func (m *viewMaterializer) namedTableCell(column view.TableColumn, source tableSourceRow, baseSource ViewDataSourceRefs) TableCell {
	switch column.Source.Kind {
	case view.ColumnField:
		if source.entity != nil {
			return optionalTableCell(entityFieldValue(*source.entity, column.Source.Field), baseSource)
		}
		if source.relation != nil {
			return optionalTableCell(relationFieldValue(*source.relation, column.Source.Field), baseSource)
		}
	case view.ColumnAttribute:
		if source.row == nil {
			m.addDiag("LDL1701", "invalid_view_source_category_or_shape", "attribute Table Column requires row-grain input", column.Address, m.input.Recipe.Address)
			return absentTableCell(baseSource)
		}
		return m.attributeTableCell(source, column.Source.ColumnAddresses)
	case view.ColumnRelationEndpoint:
		if source.relation == nil {
			m.addDiag("LDL1701", "invalid_view_source_category_or_shape", "relation endpoint Table Column requires Relation input", column.Address, m.input.Recipe.Address)
			return absentTableCell(baseSource)
		}
		return m.relationEndpointTableCell(*source.relation, column.Source.Endpoint, column.Source.Field, baseSource)
	case view.ColumnDerivedCount:
		if source.entity == nil {
			m.addDiag("LDL1701", "invalid_view_source_category_or_shape", "derived count Table Column requires Entity input", column.Address, m.input.Recipe.Address)
			return absentTableCell(baseSource)
		}
		count, contributing := m.derivedCount(source.entity.Address, column.Source.Direction, column.Source.RelationTypeAddresses)
		for _, relation := range contributing {
			baseSource = mergeViewDataSourceRefs(baseSource, m.relationSource(relation))
		}
		return presentTableCell(scalarViewValue(definition.Scalar{Type: definition.ScalarInteger, Int: int64(count)}), baseSource)
	case view.ColumnState:
		if !tableStateFieldKnown(column.Source.StateFieldPath) {
			m.addDiag("LDL1601", "invalid_query_or_arguments", "Table state Column uses an unknown field path", column.Address, m.input.Recipe.Address)
			return absentTableCell(baseSource)
		}
		subject := source.ownerAddress()
		if source.row != nil {
			subject = source.row.Address
		}
		read := StateReadRef{SubjectAddress: subject, FieldPath: string(column.Source.StateFieldPath)}
		return stateTableCell(m.readViewState(subject, column.Source.StateFieldPath), baseSource, read)
	default:
		m.addDiag("LDL1701", "invalid_view_source_category_or_shape", "Table Column source kind is invalid", column.Address, m.input.Recipe.Address)
	}
	return absentTableCell(baseSource)
}

func (m *viewMaterializer) relationEndpointTableCell(relation graph.Relation, endpoint definition.ProjectionEndpoint, field string, source ViewDataSourceRefs) TableCell {
	address := relation.ToAddress
	if endpoint == definition.ProjectionEndpointFrom {
		address = relation.FromAddress
	} else if endpoint != definition.ProjectionEndpointTo {
		m.addDiag("LDL1701", "invalid_view_source_category_or_shape", "Table relation endpoint is invalid", relation.Address, m.input.Recipe.Address)
		return absentTableCell(source)
	}
	entity, ok := m.entities[address]
	if !ok {
		m.addDiag("LDL1702", "view_materialization_conflict", "Table relation endpoint Entity is absent", address, m.input.Recipe.Address)
		return absentTableCell(source)
	}
	return optionalTableCell(entityFieldValue(entity, field), mergeViewDataSourceRefs(source, m.entitySource(entity)))
}

func (m *viewMaterializer) attributeTableCell(source tableSourceRow, columns []string) TableCell {
	if source.row == nil {
		return absentTableCell(m.tableSourceIdentitySource(source))
	}
	if value, cell, ok := rowCellValue(*source.row, columns); ok {
		refs := m.tableSourceIdentitySource(source)
		refs.CellRefs = []ViewDataCellRef{cell}
		return presentTableCell(value, canonicalViewDataSourceRefs(refs))
	}
	return absentTableCell(m.tableSourceIdentitySource(source))
}

func (m *viewMaterializer) dynamicRelationAttributeCell(columnAddress string, source tableSourceRow) TableCell {
	if source.row == nil {
		return absentTableCell(m.tableSourceIdentitySource(source))
	}
	if value, cell, ok := rowCellValue(*source.row, []string{columnAddress}); ok {
		refs := m.tableSourceIdentitySource(source)
		refs.CellRefs = []ViewDataCellRef{cell}
		return presentTableCell(value, canonicalViewDataSourceRefs(refs))
	}
	return absentTableCell(m.tableSourceIdentitySource(source))
}

func rowCellValue(row graph.AttributeRow, columnAddresses []string) (ViewDataValue, ViewDataCellRef, bool) {
	allowed := viewStringSet(columnAddresses)
	for _, cell := range row.Values {
		if allowed[cell.ColumnAddress] {
			return scalarViewValue(cell.Value), ViewDataCellRef{RowAddress: row.Address, ColumnAddress: cell.ColumnAddress}, true
		}
	}
	return ViewDataValue{}, ViewDataCellRef{}, false
}

func (m *viewMaterializer) tableSourceRowSource(source tableSourceRow) ViewDataSourceRefs {
	if source.row == nil {
		return m.tableSourceIdentitySource(source)
	}
	if source.entity != nil {
		return m.rowSource(source.entity.Address, true, *source.row)
	}
	return m.rowSource(source.relation.Address, false, *source.row)
}

func (m *viewMaterializer) tableSourceIdentitySource(source tableSourceRow) ViewDataSourceRefs {
	var refs ViewDataSourceRefs
	if source.entity != nil {
		refs = m.entitySource(*source.entity)
	} else if source.relation != nil {
		refs = m.relationSource(*source.relation)
	} else {
		return emptyViewDataSourceRefs()
	}
	if source.row != nil {
		refs.RowAddresses = []string{source.row.Address}
		refs.State.Reads = canonicalStateReads(append(refs.State.Reads, m.stateReadsForSubjects(source.row.Address)...))
	}
	return canonicalViewDataSourceRefs(refs)
}

func (source tableSourceRow) ownerAddress() string {
	if source.entity != nil {
		return source.entity.Address
	}
	if source.relation != nil {
		return source.relation.Address
	}
	return ""
}

func (m *viewMaterializer) effectiveTableProjection(relationTypeAddress string) (definition.TableProjection, bool) {
	relationType, ok := m.relationTypes[relationTypeAddress]
	if !ok {
		m.addDiag("LDL1702", "view_materialization_conflict", "Table RelationType is absent from the immutable definition", relationTypeAddress, m.input.Recipe.Address)
		return definition.TableProjection{}, false
	}
	projection := relationType.Projections.Table
	found := false
	for _, override := range m.input.Recipe.RelationProjections {
		if override.RelationTypeAddress != relationTypeAddress {
			continue
		}
		if found {
			m.addDiag("LDL1504", "invalid_projection_contract", "View contains duplicate effective RelationType projection overrides", relationTypeAddress, m.input.Recipe.Address)
			return definition.TableProjection{}, false
		}
		found = true
		projection = override.Projections.Table
	}
	if projection.RowMode != definition.TableRowsRelation && projection.RowMode != definition.TableRowsRelationRows && projection.RowMode != definition.TableRowsAutomatic {
		m.addDiag("LDL1504", "invalid_projection_contract", "effective Table projection is invalid", relationTypeAddress, m.input.Recipe.Address)
		return definition.TableProjection{}, false
	}
	return projection, true
}

func (m *viewMaterializer) relationColumn(address string) (definition.Column, bool) {
	for _, relationType := range m.relationTypes {
		for _, column := range relationType.Columns {
			if column.Address == address {
				return column, true
			}
		}
	}
	return definition.Column{}, false
}

func (m *viewMaterializer) derivedCount(entityAddress string, direction definition.TraversalDirection, relationTypes *[]string) (int, []graph.Relation) {
	allowedTypes := map[string]bool{}
	if relationTypes != nil {
		allowedTypes = viewStringSet(*relationTypes)
	}
	selected := viewStringSet(m.relationAddresses())
	seen := map[string]bool{}
	values := []graph.Relation{}
	visit := func(addresses []string) {
		for _, relationAddress := range addresses {
			relation, ok := m.relations[relationAddress]
			if !ok || !selected[relationAddress] || seen[relationAddress] || len(allowedTypes) != 0 && !allowedTypes[relation.TypeAddress] {
				continue
			}
			seen[relationAddress] = true
			values = append(values, relation)
		}
	}
	if direction == definition.TraversalOutgoing || direction == definition.TraversalBoth {
		visit(m.outgoing[entityAddress])
	}
	if direction == definition.TraversalIncoming || direction == definition.TraversalBoth {
		visit(m.incoming[entityAddress])
	}
	sort.Slice(values, func(i, j int) bool { return compareRelationTuple(values[i], values[j]) < 0 })
	return len(values), values
}

func findTableColumn(columns []view.TableColumn, id string) *view.TableColumn {
	for index := range columns {
		if columns[index].ID == id {
			return &columns[index]
		}
	}
	return nil
}

func optionalTableCell(value optionalScalar, source ViewDataSourceRefs) TableCell {
	if !value.present {
		return absentTableCell(source)
	}
	if value.address != nil {
		return presentTableCell(addressViewValue(*value.address), source)
	}
	if value.strings != nil {
		return presentTableCell(ViewDataValue{Kind: "string_set", StringSet: sortedUniqueStrings(value.strings)}, source)
	}
	return presentTableCell(scalarViewValue(value.value), source)
}

func presentTableCell(value ViewDataValue, source ViewDataSourceRefs) TableCell {
	copied := deepClone(value)
	return TableCell{Present: true, Value: &copied, Source: canonicalViewDataSourceRefs(source)}
}

func absentTableCell(source ViewDataSourceRefs) TableCell {
	return TableCell{Present: false, Source: canonicalViewDataSourceRefs(source)}
}

func scalarViewValue(value definition.Scalar) ViewDataValue {
	copied := value
	return ViewDataValue{Kind: "scalar", Scalar: &copied, StringSet: []string{}}
}

func addressViewValue(value string) ViewDataValue {
	copied := value
	return ViewDataValue{Kind: "stable_address", Address: &copied, StringSet: []string{}}
}

func tableStateFieldKnown(path query.StateFieldPath) bool {
	_, ok := query.LookupStateFieldSchema(path)
	return ok
}
