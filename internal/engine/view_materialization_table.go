// SPDX-License-Identifier: LicenseRef-LayerDraw-1.0

package engine

import (
	"github.com/dencyuinc/layerdraw/internal/engine/internal/compiler/semantic/definition"
	"github.com/dencyuinc/layerdraw/internal/engine/internal/compiler/semantic/graph"
	"github.com/dencyuinc/layerdraw/internal/engine/internal/compiler/view"
)

func (m *viewMaterializer) table(base ViewDataBase) *TableViewData {
	shape := m.input.Recipe.Shape.Table
	if shape == nil {
		m.addDiag("LDL1701", "unsupported_view_shape_or_export", "Table View shape is missing", m.input.Recipe.Address, "")
		return &TableViewData{ViewDataBase: base, Columns: []TableColumn{}, Rows: []TableRow{}}
	}
	columns := m.tableColumns(shape)
	rows := m.tableRows(shape, columns)
	return &TableViewData{ViewDataBase: base, Columns: columns, Rows: rows}
}

func (m *viewMaterializer) tableColumns(shape *view.TableShape) []TableColumn {
	columns := []TableColumn{}
	appendFixed := func(id, label string) {
		columns = append(columns, TableColumn{
			Key: viewItemKey(m, "table-column", []any{m.input.Recipe.Address, id}),
			ID:  id, Label: label, ValueType: string(view.TableValueStableAddress), SourceColumnAddresses: []string{},
		})
	}
	if shape.IncludeEntityID {
		appendFixed("entity_id", "Entity ID")
	}
	if shape.IncludeType {
		appendFixed("type", "Type")
	}
	if shape.IncludeLayer {
		appendFixed("layer", "Layer")
	}
	for _, source := range shape.Columns {
		label := source.ID
		if source.Label != nil {
			label = *source.Label
		}
		address := source.Address
		column := TableColumn{
			Key: viewItemKey(m, "table-column", []any{m.input.Recipe.Address, source.Address}),
			ID:  source.ID, Address: &address, Label: label, ValueType: tableValueTypeName(source.ValueType),
			SourceColumnAddresses: []string{},
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

func (m *viewMaterializer) tableRows(shape *view.TableShape, columns []TableColumn) []TableRow {
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
		m.addDiag("LDL1701", "unsupported_view_shape_or_export", "Table row source is invalid", m.input.Recipe.Address, "")
		return []TableRow{}
	}
}

func (m *viewMaterializer) entityTableRows(shape *view.TableShape, columns []TableColumn, rowGrain bool) []TableRow {
	addresses := m.materializationEntityAddresses()
	if shape.EntityTypeAddresses != nil {
		allowed := viewStringSet(*shape.EntityTypeAddresses)
		filtered := make([]string, 0, len(addresses))
		for _, address := range addresses {
			if allowed[m.entities[address].TypeAddress] {
				filtered = append(filtered, address)
			}
		}
		addresses = filtered
	}
	rows := []TableRow{}
	for _, address := range addresses {
		entity := m.entities[address]
		if !rowGrain {
			rows = append(rows, m.entityTableRow(shape, columns, entity, nil))
			continue
		}
		for index := range entity.Rows {
			rows = append(rows, m.entityTableRow(shape, columns, entity, &entity.Rows[index]))
		}
	}
	return rows
}

func (m *viewMaterializer) entityTableRow(shape *view.TableShape, columns []TableColumn, entity graph.Entity, row *graph.AttributeRow) TableRow {
	identity := [][]string{{entity.Address}}
	baseSource := m.entitySource(entity)
	if row != nil {
		identity = [][]string{{entity.Address, row.Address}}
		baseSource = m.rowSource(entity.Address, true, *row)
	}
	result := TableRow{
		Key:   viewItemKey(m, "table-row", []any{m.input.Recipe.Address, string(shape.RowSource), identity, []any{}}),
		Cells: make(map[string]TableCell, len(columns)), Source: baseSource,
	}
	for _, column := range columns {
		cell := m.entityTableCell(shape, column, entity, row)
		result.Cells[column.Key] = cell
		result.Source = mergeViewDataSourceRefs(result.Source, cell.Source)
	}
	return result
}

func (m *viewMaterializer) entityTableCell(shape *view.TableShape, column TableColumn, entity graph.Entity, row *graph.AttributeRow) TableCell {
	source := m.entitySource(entity)
	switch column.ID {
	case "entity_id":
		return presentTableCell(addressViewValue(entity.Address), source)
	case "type":
		return presentTableCell(addressViewValue(entity.TypeAddress), source)
	case "layer":
		return presentTableCell(addressViewValue(entity.LayerAddress), source)
	}
	definitionColumn := findTableColumn(shape.Columns, column.ID)
	if definitionColumn == nil {
		return absentTableCell(source)
	}
	switch definitionColumn.Source.Kind {
	case view.ColumnField:
		return optionalTableCell(entityFieldValue(entity, definitionColumn.Source.Field), source)
	case view.ColumnAttribute:
		return m.attributeTableCell(entity.Address, true, entity.Rows, row, definitionColumn.Source.ColumnAddresses)
	case view.ColumnDerivedCount:
		count, contributing := m.derivedCount(entity.Address, definitionColumn.Source.Direction, definitionColumn.Source.RelationTypeAddresses)
		for _, relation := range contributing {
			source = mergeViewDataSourceRefs(source, m.relationSource(relation))
		}
		return presentTableCell(scalarViewValue(definition.Scalar{Type: definition.ScalarInteger, Int: int64(count)}), source)
	case view.ColumnState:
		read := StateReadRef{SubjectAddress: entity.Address, FieldPath: string(definitionColumn.Source.StateFieldPath)}
		m.directStateReads[read] = true
		source.State.Reads = canonicalStateReads(append(source.State.Reads, read))
		return absentTableCell(canonicalViewDataSourceRefs(source))
	default:
		return absentTableCell(source)
	}
}

func (m *viewMaterializer) relationTableRows(shape *view.TableShape, columns []TableColumn, rowGrain bool) []TableRow {
	rows := []TableRow{}
	for _, address := range m.relationAddresses() {
		relation := m.relations[address]
		if !rowGrain {
			rows = append(rows, m.relationTableRow(shape, columns, relation, nil))
			continue
		}
		for index := range relation.Rows {
			rows = append(rows, m.relationTableRow(shape, columns, relation, &relation.Rows[index]))
		}
	}
	return rows
}

func (m *viewMaterializer) relationTableRow(shape *view.TableShape, columns []TableColumn, relation graph.Relation, row *graph.AttributeRow) TableRow {
	identity := [][]string{{relation.Address}}
	baseSource := m.relationSource(relation)
	if row != nil {
		identity = [][]string{{relation.Address, row.Address}}
		baseSource = m.rowSource(relation.Address, false, *row)
	}
	result := TableRow{
		Key:   viewItemKey(m, "table-row", []any{m.input.Recipe.Address, string(shape.RowSource), identity, []any{}}),
		Cells: make(map[string]TableCell, len(columns)), Source: baseSource,
	}
	for _, column := range columns {
		cell := m.relationTableCell(shape, column, relation, row)
		result.Cells[column.Key] = cell
		result.Source = mergeViewDataSourceRefs(result.Source, cell.Source)
	}
	return result
}

func (m *viewMaterializer) relationTableCell(shape *view.TableShape, column TableColumn, relation graph.Relation, row *graph.AttributeRow) TableCell {
	source := m.relationSource(relation)
	definitionColumn := findTableColumn(shape.Columns, column.ID)
	if definitionColumn == nil {
		return absentTableCell(source)
	}
	switch definitionColumn.Source.Kind {
	case view.ColumnField:
		return optionalTableCell(relationFieldValue(relation, definitionColumn.Source.Field), source)
	case view.ColumnRelationEndpoint:
		if definitionColumn.Source.Endpoint == definition.ProjectionEndpointFrom {
			return presentTableCell(addressViewValue(relation.FromAddress), source)
		}
		return presentTableCell(addressViewValue(relation.ToAddress), source)
	case view.ColumnAttribute:
		return m.attributeTableCell(relation.Address, false, relation.Rows, row, definitionColumn.Source.ColumnAddresses)
	case view.ColumnState:
		read := StateReadRef{SubjectAddress: relation.Address, FieldPath: string(definitionColumn.Source.StateFieldPath)}
		m.directStateReads[read] = true
		source.State.Reads = canonicalStateReads(append(source.State.Reads, read))
		return absentTableCell(canonicalViewDataSourceRefs(source))
	default:
		return absentTableCell(source)
	}
}

func (m *viewMaterializer) attributeTableCell(owner string, entity bool, rows []graph.AttributeRow, selected *graph.AttributeRow, columns []string) TableCell {
	if selected != nil {
		if value, cell, ok := rowCellValue(*selected, columns); ok {
			source := m.rowSource(owner, entity, *selected)
			source.CellRefs = []ViewDataCellRef{cell}
			return presentTableCell(value, canonicalViewDataSourceRefs(source))
		}
		return absentTableCell(m.rowSource(owner, entity, *selected))
	}
	for _, row := range rows {
		if value, cell, ok := rowCellValue(row, columns); ok {
			source := m.rowSource(owner, entity, row)
			source.CellRefs = []ViewDataCellRef{cell}
			return presentTableCell(value, canonicalViewDataSourceRefs(source))
		}
	}
	if entity {
		return absentTableCell(m.entitySource(m.entities[owner]))
	}
	return absentTableCell(m.relationSource(m.relations[owner]))
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

func (m *viewMaterializer) derivedCount(entityAddress string, direction definition.TraversalDirection, relationTypes *[]string) (int, []graph.Relation) {
	allowed := map[string]bool{}
	if relationTypes != nil {
		allowed = viewStringSet(*relationTypes)
	}
	seen := map[string]bool{}
	values := []graph.Relation{}
	visit := func(addresses []string) {
		for _, relationAddress := range addresses {
			relation := m.relations[relationAddress]
			if seen[relationAddress] || (len(allowed) != 0 && !allowed[relation.TypeAddress]) {
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
	sortRelationsByAddress(values)
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
