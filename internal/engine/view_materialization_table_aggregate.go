// SPDX-License-Identifier: LicenseRef-LayerDraw-1.0

package engine

import (
	"sort"
	"strings"

	"github.com/dencyuinc/layerdraw/internal/engine/internal/compiler/semantic/definition"
	"github.com/dencyuinc/layerdraw/internal/engine/internal/compiler/view"
)

func (m *viewMaterializer) finalizeTableRows(shape *view.TableShape, columns []TableColumn, raw []materializedTableRow) []materializedTableRow {
	hasAggregate := false
	for _, column := range shape.Columns {
		if column.Aggregate != view.AggregateNone {
			hasAggregate = true
			break
		}
	}
	var rows []materializedTableRow
	if hasAggregate {
		rows = m.aggregateTableRows(shape, columns, raw)
	} else {
		rows = raw
		for index := range rows {
			rows[index].groupKey = tableGroupKey(columns, rows[index].value.Cells)
			rows[index].value.Key = m.tableRowKey(shape.RowSource, rows[index].identities, rows[index].groupKey)
		}
	}
	m.sortTableRows(shape, columns, rows)
	return rows
}

func (m *viewMaterializer) aggregateTableRows(shape *view.TableShape, columns []TableColumn, raw []materializedTableRow) []materializedTableRow {
	nonAggregate := tableNonAggregateColumns(shape, columns)
	type group struct {
		key  []any
		rows []materializedTableRow
	}
	groups := []group{}
	index := map[string]int{}
	for _, row := range raw {
		key := tableGroupKey(nonAggregate, row.value.Cells)
		lookup, err := newViewDataItemKey("table-group", key)
		if err != nil {
			m.addDiag("LDL1801", "stale_revision_or_semantic_hash", "cannot derive Table group identity", m.input.Recipe.Address, "")
			continue
		}
		groupIndex, ok := index[lookup]
		if !ok {
			groupIndex = len(groups)
			index[lookup] = groupIndex
			groups = append(groups, group{key: key})
		}
		groups[groupIndex].rows = append(groups[groupIndex].rows, row)
	}
	if len(raw) == 0 && len(nonAggregate) == 0 {
		groups = append(groups, group{key: []any{}})
	}
	result := make([]materializedTableRow, 0, len(groups))
	for _, current := range groups {
		row := materializedTableRow{
			value:      TableRow{Cells: map[string]TableCell{}, Source: emptyViewDataSourceRefs()},
			identities: [][]string{},
			groupKey:   current.key,
		}
		for _, source := range current.rows {
			row.identities = append(row.identities, deepClone(source.identities)...)
			row.value.Source = mergeViewDataSourceRefs(row.value.Source, source.value.Source)
		}
		for _, column := range columns {
			aggregate := tableColumnAggregate(shape, column.ID)
			cell := m.aggregateTableColumn(column, aggregate, current.rows)
			row.value.Cells[column.Key] = cell
			row.value.Source = mergeViewDataSourceRefs(row.value.Source, cell.Source)
		}
		row.value.Key = m.tableRowKey(shape.RowSource, row.identities, row.groupKey)
		result = append(result, row)
	}
	return result
}

func (m *viewMaterializer) aggregateTableColumn(column TableColumn, aggregate view.Aggregate, rows []materializedTableRow) TableCell {
	if aggregate == view.AggregateNone {
		var result TableCell
		for index, row := range rows {
			cell := row.value.Cells[column.Key]
			if index == 0 {
				result = deepClone(cell)
			} else {
				result.Source = mergeViewDataSourceRefs(result.Source, cell.Source)
			}
		}
		if len(rows) == 0 {
			return absentTableCell(emptyViewDataSourceRefs())
		}
		return result
	}
	present := []TableCell{}
	for _, row := range rows {
		cell := row.value.Cells[column.Key]
		if cell.Present && cell.Value != nil {
			present = append(present, cell)
		}
	}
	switch aggregate {
	case view.AggregateCount:
		source := emptyViewDataSourceRefs()
		for _, row := range rows {
			source = mergeViewDataSourceRefs(source, row.value.Source)
		}
		return presentTableCell(scalarViewValue(definition.Scalar{Type: definition.ScalarInteger, Int: int64(len(rows))}), source)
	case view.AggregateCountDistinct:
		distinct := map[string]bool{}
		source := emptyViewDataSourceRefs()
		for _, cell := range present {
			identity, err := newViewDataItemKey("table-value", *cell.Value)
			if err != nil {
				m.addDiag("LDL1801", "stale_revision_or_semantic_hash", "cannot derive Table aggregate value identity", m.input.Recipe.Address, "")
				continue
			}
			distinct[identity] = true
			source = mergeViewDataSourceRefs(source, cell.Source)
		}
		return presentTableCell(scalarViewValue(definition.Scalar{Type: definition.ScalarInteger, Int: int64(len(distinct))}), source)
	case view.AggregateMin, view.AggregateMax:
		if len(present) == 0 {
			return absentTableCell(emptyViewDataSourceRefs())
		}
		selected := present[0]
		for _, candidate := range present[1:] {
			compared, ok := compareTableValues(*candidate.Value, *selected.Value, column)
			if !ok {
				m.addDiag("LDL1701", "invalid_view_source_category_or_shape", "Table min/max values are not comparable", column.ID, m.input.Recipe.Address)
				return absentTableCell(emptyViewDataSourceRefs())
			}
			if aggregate == view.AggregateMin && compared < 0 || aggregate == view.AggregateMax && compared > 0 {
				selected = candidate
			}
		}
		source := emptyViewDataSourceRefs()
		for _, candidate := range present {
			source = mergeViewDataSourceRefs(source, candidate.Source)
		}
		return presentTableCell(*selected.Value, source)
	case view.AggregateJoinUnique:
		if len(present) == 0 {
			return absentTableCell(emptyViewDataSourceRefs())
		}
		values := map[string]bool{}
		source := emptyViewDataSourceRefs()
		for _, cell := range present {
			if cell.Value.Kind != "scalar" || cell.Value.Scalar == nil || cell.Value.Scalar.Type != definition.ScalarString && cell.Value.Scalar.Type != definition.ScalarEnum {
				m.addDiag("LDL1701", "invalid_view_source_category_or_shape", "Table join_unique value is not string-compatible", column.ID, m.input.Recipe.Address)
				return absentTableCell(emptyViewDataSourceRefs())
			}
			values[cell.Value.Scalar.String] = true
			source = mergeViewDataSourceRefs(source, cell.Source)
		}
		ordered := make([]string, 0, len(values))
		for value := range values {
			ordered = append(ordered, value)
		}
		sort.Strings(ordered)
		return presentTableCell(scalarViewValue(definition.Scalar{Type: definition.ScalarString, String: strings.Join(ordered, ", ")}), source)
	default:
		m.addDiag("LDL1701", "invalid_view_source_category_or_shape", "Table aggregate is invalid", column.ID, m.input.Recipe.Address)
		return absentTableCell(emptyViewDataSourceRefs())
	}
}

func tableNonAggregateColumns(shape *view.TableShape, columns []TableColumn) []TableColumn {
	result := []TableColumn{}
	for _, column := range columns {
		if tableColumnAggregate(shape, column.ID) == view.AggregateNone {
			result = append(result, column)
		}
	}
	return result
}

func tableColumnAggregate(shape *view.TableShape, id string) view.Aggregate {
	if column := findTableColumn(shape.Columns, id); column != nil {
		return column.Aggregate
	}
	return view.AggregateNone
}

func tableGroupKey(columns []TableColumn, cells map[string]TableCell) []any {
	result := make([]any, 0, len(columns))
	for _, column := range columns {
		cell := cells[column.Key]
		if !cell.Present || cell.Value == nil {
			result = append(result, []any{false})
			continue
		}
		result = append(result, []any{true, *cell.Value})
	}
	return result
}

func (m *viewMaterializer) tableRowKey(rowSource view.TableRowSource, identities [][]string, groupKey []any) string {
	return viewItemKey(m, "table-row", []any{m.input.Recipe.Address, string(rowSource), identities, groupKey})
}

func (m *viewMaterializer) sortTableRows(shape *view.TableShape, columns []TableColumn, rows []materializedTableRow) {
	byID := map[string]TableColumn{}
	for _, column := range columns {
		byID[column.ID] = column
	}
	for _, authored := range shape.Sorts {
		if _, ok := byID[authored.ColumnID]; !ok {
			m.addDiag("LDL1701", "invalid_view_source_category_or_shape", "Table sort references an unknown Column", authored.ColumnID, m.input.Recipe.Address)
		}
	}
	sort.SliceStable(rows, func(i, j int) bool {
		for _, authored := range shape.Sorts {
			column, ok := byID[authored.ColumnID]
			if !ok {
				continue
			}
			left, right := rows[i].value.Cells[column.Key], rows[j].value.Cells[column.Key]
			compared, comparable := compareTableCells(left, right, column, authored.Absent)
			if !comparable || compared == 0 {
				continue
			}
			if authored.Direction == view.SortDescending && left.Present == right.Present {
				compared = -compared
			}
			return compared < 0
		}
		return false
	})
}

func compareTableCells(left, right TableCell, column TableColumn, absent view.AbsentOrder) (int, bool) {
	if left.Present != right.Present {
		if absent == view.AbsentFirst {
			if left.Present {
				return 1, true
			}
			return -1, true
		}
		if left.Present {
			return -1, true
		}
		return 1, true
	}
	if !left.Present {
		return 0, true
	}
	if left.Value == nil || right.Value == nil {
		return 0, false
	}
	return compareTableValues(*left.Value, *right.Value, column)
}

func compareTableValues(left, right ViewDataValue, column TableColumn) (int, bool) {
	if left.Kind != right.Kind {
		return 0, false
	}
	switch left.Kind {
	case "stable_address":
		if left.Address == nil || right.Address == nil {
			return 0, false
		}
		return compareStableAddressText(*left.Address, *right.Address), true
	case "string_set":
		for index := 0; index < len(left.StringSet) && index < len(right.StringSet); index++ {
			if compared := strings.Compare(left.StringSet[index], right.StringSet[index]); compared != 0 {
				return compared, true
			}
		}
		return compareInt(int64(len(left.StringSet)), int64(len(right.StringSet))), true
	case "scalar":
		if left.Scalar == nil || right.Scalar == nil || left.Scalar.Type != right.Scalar.Type {
			return 0, false
		}
		switch left.Scalar.Type {
		case definition.ScalarString:
			return strings.Compare(left.Scalar.String, right.Scalar.String), true
		case definition.ScalarEnum:
			return compareEnumValues(left.Scalar.String, right.Scalar.String, column.EnumValues)
		case definition.ScalarBoolean:
			return compareInt(boolRank(left.Scalar.Bool), boolRank(right.Scalar.Bool)), true
		default:
			return scalarCompare(*left.Scalar, *right.Scalar)
		}
	default:
		return 0, false
	}
}

func compareEnumValues(left, right string, options []string) (int, bool) {
	rank := map[string]int{}
	for index, option := range options {
		rank[option] = index
	}
	leftRank, leftOK := rank[left]
	rightRank, rightOK := rank[right]
	if !leftOK || !rightOK {
		return 0, false
	}
	return compareInt(int64(leftRank), int64(rightRank)), true
}

func boolRank(value bool) int64 {
	if value {
		return 1
	}
	return 0
}
