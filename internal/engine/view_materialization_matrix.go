// SPDX-License-Identifier: LicenseRef-LayerDraw-1.0

package engine

import (
	"sort"

	"github.com/dencyuinc/layerdraw/internal/engine/internal/compiler/semantic/definition"
	"github.com/dencyuinc/layerdraw/internal/engine/internal/compiler/semantic/graph"
	"github.com/dencyuinc/layerdraw/internal/engine/internal/compiler/view"
)

func (m *viewMaterializer) matrix(base ViewDataBase) *MatrixViewData {
	shape := m.input.Recipe.Shape.Matrix
	if shape == nil {
		m.addDiag("LDL1701", "invalid_view_source_category_or_shape", "Matrix View shape is missing", m.input.Recipe.Address, "")
		return &MatrixViewData{ViewDataBase: base, RowAxis: []MatrixAxisItem{}, ColumnAxis: []MatrixAxisItem{}, Cells: []MatrixCell{}}
	}
	allowedTypes := m.matrixRelationTypes(shape.Cell.RelationTypeAddresses)
	rowAxis := m.matrixAxis("row", shape.RowAxis)
	columnAxis := m.matrixAxis("column", shape.ColumnAxis)
	paths := []QueryPath{}
	if shape.Cell.Semantic == view.MatrixPathRefs {
		paths = m.matrixPaths(allowedTypes)
	}
	cells := make([]MatrixCell, 0, len(rowAxis)*len(columnAxis))
	for _, row := range rowAxis {
		for _, column := range columnAxis {
			cell := MatrixCell{
				Key:    viewItemKey(m, "matrix-cell", []any{m.input.Recipe.Address, row.EntityAddress, column.EntityAddress}),
				RowKey: row.Key, ColumnKey: column.Key, SemanticRefs: []MatrixSemanticRef{}, Source: emptyViewDataSourceRefs(),
			}
			switch shape.Cell.Semantic {
			case view.MatrixRelationRefs:
				cell.SemanticRefs, cell.Source = m.matrixRelationRefs(row.EntityAddress, column.EntityAddress, shape.Cell.Direction, allowedTypes)
			case view.MatrixPathRefs:
				cell.SemanticRefs, cell.Source = m.matrixPathRefs(row.EntityAddress, column.EntityAddress, shape.Cell.Direction, paths)
			default:
				m.addDiag("LDL1701", "invalid_view_source_category_or_shape", "Matrix semantic mode is invalid", m.input.Recipe.Address, "")
			}
			cell.DisplayValue, cell.Source = m.matrixDisplayValue(shape, cell.SemanticRefs, cell.Source, allowedTypes)
			cells = append(cells, cell)
		}
	}
	return &MatrixViewData{ViewDataBase: base, RowAxis: rowAxis, ColumnAxis: columnAxis, Cells: cells}
}

func (m *viewMaterializer) matrixAxis(kind string, axis view.MatrixAxis) []MatrixAxisItem {
	allowed := map[string]bool{}
	if axis.EntityTypeAddresses != nil {
		allowed = viewStringSet(*axis.EntityTypeAddresses)
		for address := range allowed {
			if _, ok := m.entityTypes[address]; !ok {
				m.addDiag("LDL1701", "invalid_view_source_category_or_shape", "Matrix axis references an unknown EntityType", address, m.input.Recipe.Address)
			}
		}
	}
	items := []MatrixAxisItem{}
	seen := map[string]bool{}
	for _, address := range m.primaryEntityAddresses() {
		entity, ok := m.entities[address]
		if !ok {
			m.addDiag("LDL1702", "view_materialization_conflict", "Matrix axis Entity is absent from the immutable graph", address, m.input.Recipe.Address)
			continue
		}
		if axis.EntityTypeAddresses != nil && !allowed[entity.TypeAddress] {
			continue
		}
		if seen[address] {
			m.addDiag("LDL1702", "view_materialization_conflict", "Matrix axis contains duplicate Entity membership", address, m.input.Recipe.Address)
			continue
		}
		seen[address] = true
		label, valid := matrixAxisLabel(entity, axis.LabelField)
		if !valid {
			m.addDiag("LDL1701", "invalid_view_source_category_or_shape", "Matrix axis label field is invalid", address, m.input.Recipe.Address)
			continue
		}
		items = append(items, MatrixAxisItem{
			Key:           viewItemKey(m, "matrix-axis", []any{m.input.Recipe.Address, kind, entity.Address}),
			EntityAddress: entity.Address, Label: label, Source: m.entitySource(entity),
		})
	}
	return items
}

func matrixAxisLabel(entity graph.Entity, field view.AxisLabelField) (string, bool) {
	switch field {
	case view.AxisLabelID:
		return entity.ID, true
	case view.AxisLabelDisplayName:
		return entity.DisplayName, true
	case view.AxisLabelType:
		return entity.TypeAddress, true
	case view.AxisLabelLayer:
		return entity.LayerAddress, true
	default:
		return "", false
	}
}

func (m *viewMaterializer) matrixRelationTypes(configured *[]string) map[string]bool {
	allowed := map[string]bool{}
	if configured != nil {
		for _, address := range *configured {
			allowed[address] = true
		}
	} else {
		for _, address := range m.relationAddresses() {
			if relation, ok := m.relations[address]; ok {
				allowed[relation.TypeAddress] = true
			}
		}
	}
	for address := range allowed {
		m.effectiveMatrixProjection(address)
	}
	return allowed
}

func (m *viewMaterializer) matrixRelationRefs(rowAddress, columnAddress string, direction definition.TraversalDirection, allowedTypes map[string]bool) ([]MatrixSemanticRef, ViewDataSourceRefs) {
	relations := []graph.Relation{}
	for _, address := range m.relationAddresses() {
		relation, ok := m.relations[address]
		if !ok || !allowedTypes[relation.TypeAddress] {
			continue
		}
		projection, valid := m.effectiveMatrixProjection(relation.TypeAddress)
		if !valid {
			continue
		}
		projectedRow, projectedColumn := matrixProjectedEndpoints(relation, projection)
		outgoing := projectedRow == rowAddress && projectedColumn == columnAddress
		incoming := projectedRow == columnAddress && projectedColumn == rowAddress
		if direction == definition.TraversalOutgoing && outgoing || direction == definition.TraversalIncoming && incoming || direction == definition.TraversalBoth && (outgoing || incoming) {
			relations = append(relations, relation)
		}
	}
	if direction != definition.TraversalOutgoing && direction != definition.TraversalIncoming && direction != definition.TraversalBoth {
		m.addDiag("LDL1701", "invalid_view_source_category_or_shape", "Matrix relation direction is invalid", m.input.Recipe.Address, "")
	}
	sort.Slice(relations, func(i, j int) bool { return compareRelationTuple(relations[i], relations[j]) < 0 })
	refs := make([]MatrixSemanticRef, 0, len(relations))
	source := emptyViewDataSourceRefs()
	for _, relation := range relations {
		address := relation.Address
		refs = append(refs, MatrixSemanticRef{RelationAddress: &address})
		source = mergeViewDataSourceRefs(source, m.matrixRelationSource(relation))
	}
	return refs, source
}

func (m *viewMaterializer) matrixPaths(allowedTypes map[string]bool) []QueryPath {
	selectedRelations := viewStringSet(m.relationAddresses())
	paths := []QueryPath{}
	for _, path := range m.queryResult.Paths {
		if len(path.EntityAddresses) == 0 || len(path.RelationAddresses)+1 != len(path.EntityAddresses) {
			m.addDiag("LDL1702", "view_materialization_conflict", "Matrix path has an invalid Entity/Relation sequence", m.input.Recipe.Address, "")
			continue
		}
		valid := true
		for index, entityAddress := range path.EntityAddresses {
			if _, ok := m.entities[entityAddress]; !ok {
				m.addDiag("LDL1702", "view_materialization_conflict", "Matrix path references an unknown Entity", entityAddress, m.input.Recipe.Address)
				valid = false
			}
			if index >= len(path.RelationAddresses) {
				continue
			}
			relationAddress := path.RelationAddresses[index]
			relation, ok := m.relations[relationAddress]
			if !ok {
				m.addDiag("LDL1702", "view_materialization_conflict", "Matrix path references an unknown Relation", relationAddress, m.input.Recipe.Address)
				valid = false
				continue
			}
			if !selectedRelations[relationAddress] {
				m.addDiag("LDL1702", "view_materialization_conflict", "Matrix path references a Relation outside the selected source set", relationAddress, m.input.Recipe.Address)
				valid = false
				continue
			}
			if !allowedTypes[relation.TypeAddress] {
				valid = false
				continue
			}
			next := path.EntityAddresses[index+1]
			if !(relation.FromAddress == entityAddress && relation.ToAddress == next || relation.ToAddress == entityAddress && relation.FromAddress == next) {
				m.addDiag("LDL1702", "view_materialization_conflict", "Matrix path Relation does not connect adjacent Entities", relationAddress, m.input.Recipe.Address)
				valid = false
			}
		}
		if valid {
			paths = append(paths, deepClone(path))
		}
	}
	sort.Slice(paths, func(i, j int) bool { return compareMatrixPaths(paths[i], paths[j]) < 0 })
	for index := 1; index < len(paths); index++ {
		if compareMatrixPaths(paths[index-1], paths[index]) == 0 {
			m.addDiag("LDL1702", "view_materialization_conflict", "Matrix path collection contains a duplicate path", m.input.Recipe.Address, "")
		}
	}
	return paths
}

func (m *viewMaterializer) matrixPathRefs(rowAddress, columnAddress string, direction definition.TraversalDirection, paths []QueryPath) ([]MatrixSemanticRef, ViewDataSourceRefs) {
	refs := []MatrixSemanticRef{}
	source := emptyViewDataSourceRefs()
	for _, path := range paths {
		first := path.EntityAddresses[0]
		last := path.EntityAddresses[len(path.EntityAddresses)-1]
		outgoing := first == rowAddress && last == columnAddress
		incoming := first == columnAddress && last == rowAddress
		if !(direction == definition.TraversalOutgoing && outgoing || direction == definition.TraversalIncoming && incoming || direction == definition.TraversalBoth && (outgoing || incoming)) {
			continue
		}
		pathCopy := deepClone(path)
		refs = append(refs, MatrixSemanticRef{Path: &pathCopy})
		source = mergeViewDataSourceRefs(source, m.matrixPathSource(path))
	}
	if direction != definition.TraversalOutgoing && direction != definition.TraversalIncoming && direction != definition.TraversalBoth {
		m.addDiag("LDL1701", "invalid_view_source_category_or_shape", "Matrix path direction is invalid", m.input.Recipe.Address, "")
	}
	return refs, source
}

func (m *viewMaterializer) matrixDisplayValue(shape *view.MatrixShape, refs []MatrixSemanticRef, source ViewDataSourceRefs, allowedTypes map[string]bool) (MatrixDisplayValue, ViewDataSourceRefs) {
	switch shape.Cell.Display {
	case view.MatrixExists:
		return MatrixDisplayValue{Kind: "boolean", Boolean: len(refs) != 0, StringSet: []string{}, Attributes: []MatrixAttributeItem{}}, source
	case view.MatrixCount:
		return MatrixDisplayValue{Kind: "integer", Integer: int64(len(refs)), StringSet: []string{}, Attributes: []MatrixAttributeItem{}}, source
	case view.MatrixRelationTypes:
		addresses := map[string]bool{}
		for _, ref := range refs {
			if ref.RelationAddress != nil {
				if relation, ok := m.relations[*ref.RelationAddress]; ok {
					addresses[relation.TypeAddress] = true
				}
			}
			if ref.Path != nil {
				for _, relationAddress := range ref.Path.RelationAddresses {
					if relation, ok := m.relations[relationAddress]; ok {
						addresses[relation.TypeAddress] = true
					}
				}
			}
		}
		ordered := make([]string, 0, len(addresses))
		for address := range addresses {
			ordered = append(ordered, address)
		}
		sort.Slice(ordered, func(i, j int) bool { return compareStableAddressText(ordered[i], ordered[j]) < 0 })
		labels := make([]string, 0, len(ordered))
		for _, address := range ordered {
			relationType, ok := m.relationTypes[address]
			if !ok {
				m.addDiag("LDL1702", "view_materialization_conflict", "Matrix display references an unknown RelationType", address, m.input.Recipe.Address)
				continue
			}
			labels = append(labels, relationType.DisplayName)
		}
		return MatrixDisplayValue{Kind: "string_set", StringSet: labels, Attributes: []MatrixAttributeItem{}}, source
	case view.MatrixAttributeSummary:
		if shape.Cell.Semantic != view.MatrixRelationRefs || shape.Cell.AttributeColumnAddresses == nil {
			m.addDiag("LDL1701", "invalid_view_source_category_or_shape", "Matrix attribute summary requires relation refs and attribute Columns", m.input.Recipe.Address, "")
			return MatrixDisplayValue{Kind: "attributes", StringSet: []string{}, Attributes: []MatrixAttributeItem{}}, source
		}
		attributes, attributeSource := m.matrixAttributes(refs, *shape.Cell.AttributeColumnAddresses, allowedTypes)
		return MatrixDisplayValue{Kind: "attributes", StringSet: []string{}, Attributes: attributes}, mergeViewDataSourceRefs(source, attributeSource)
	default:
		m.addDiag("LDL1701", "invalid_view_source_category_or_shape", "Matrix display mode is invalid", m.input.Recipe.Address, "")
		return MatrixDisplayValue{Kind: "", StringSet: []string{}, Attributes: []MatrixAttributeItem{}}, source
	}
}

func (m *viewMaterializer) matrixAttributes(refs []MatrixSemanticRef, columnAddresses []string, allowedTypes map[string]bool) ([]MatrixAttributeItem, ViewDataSourceRefs) {
	allowedColumns := viewStringSet(columnAddresses)
	relations := []graph.Relation{}
	for _, ref := range refs {
		if ref.RelationAddress == nil {
			continue
		}
		relation, ok := m.relations[*ref.RelationAddress]
		if !ok || !allowedTypes[relation.TypeAddress] {
			continue
		}
		projection, valid := m.effectiveMatrixProjection(relation.TypeAddress)
		if !valid || !projection.IncludeRelationRows {
			m.addDiag("LDL1504", "invalid_projection_contract", "Matrix attribute summary requires Relation row inclusion", relation.TypeAddress, m.input.Recipe.Address)
			continue
		}
		relations = append(relations, relation)
	}
	sort.Slice(relations, func(i, j int) bool { return compareRelationTuple(relations[i], relations[j]) < 0 })
	items := []MatrixAttributeItem{}
	source := emptyViewDataSourceRefs()
	for _, relation := range relations {
		for _, row := range relation.Rows {
			for _, cell := range row.Values {
				if !allowedColumns[cell.ColumnAddress] {
					continue
				}
				items = append(items, MatrixAttributeItem{RelationAddress: relation.Address, RowAddress: row.Address, ColumnAddress: cell.ColumnAddress, Value: cell.Value})
				refs := m.rowSource(relation.Address, false, row)
				refs.CellRefs = []ViewDataCellRef{{RowAddress: row.Address, ColumnAddress: cell.ColumnAddress}}
				source = mergeViewDataSourceRefs(source, canonicalViewDataSourceRefs(refs))
			}
		}
	}
	return items, source
}

func (m *viewMaterializer) effectiveMatrixProjection(relationTypeAddress string) (definition.MatrixProjection, bool) {
	relationType, ok := m.relationTypes[relationTypeAddress]
	if !ok {
		m.addDiag("LDL1702", "view_materialization_conflict", "Matrix RelationType is absent from the immutable definition", relationTypeAddress, m.input.Recipe.Address)
		return definition.MatrixProjection{}, false
	}
	projection := relationType.Projections.Matrix
	found := false
	for _, override := range m.input.Recipe.RelationProjections {
		if override.RelationTypeAddress != relationTypeAddress {
			continue
		}
		if found {
			m.addDiag("LDL1504", "invalid_projection_contract", "View contains duplicate effective RelationType projection overrides", relationTypeAddress, m.input.Recipe.Address)
			return definition.MatrixProjection{}, false
		}
		found = true
		projection = override.Projections.Matrix
	}
	if projection == nil || projection.RowEndpoint == projection.ColumnEndpoint ||
		(projection.RowEndpoint != definition.ProjectionEndpointFrom && projection.RowEndpoint != definition.ProjectionEndpointTo) ||
		(projection.ColumnEndpoint != definition.ProjectionEndpointFrom && projection.ColumnEndpoint != definition.ProjectionEndpointTo) {
		m.addDiag("LDL1504", "invalid_projection_contract", "effective Matrix projection is invalid", relationTypeAddress, m.input.Recipe.Address)
		return definition.MatrixProjection{}, false
	}
	return *projection, true
}

func matrixProjectedEndpoints(relation graph.Relation, projection definition.MatrixProjection) (string, string) {
	row, column := relation.ToAddress, relation.FromAddress
	if projection.RowEndpoint == definition.ProjectionEndpointFrom {
		row = relation.FromAddress
	}
	if projection.ColumnEndpoint == definition.ProjectionEndpointTo {
		column = relation.ToAddress
	}
	return row, column
}

func (m *viewMaterializer) matrixRelationSource(relation graph.Relation) ViewDataSourceRefs {
	source := m.relationSource(relation)
	if entity, ok := m.entities[relation.FromAddress]; ok {
		source = mergeViewDataSourceRefs(source, m.entitySource(entity))
	}
	if entity, ok := m.entities[relation.ToAddress]; ok {
		source = mergeViewDataSourceRefs(source, m.entitySource(entity))
	}
	return source
}

func (m *viewMaterializer) matrixPathSource(path QueryPath) ViewDataSourceRefs {
	source := emptyViewDataSourceRefs()
	for _, address := range path.EntityAddresses {
		if entity, ok := m.entities[address]; ok {
			source = mergeViewDataSourceRefs(source, m.entitySource(entity))
		}
	}
	for _, address := range path.RelationAddresses {
		if relation, ok := m.relations[address]; ok {
			source = mergeViewDataSourceRefs(source, m.relationSource(relation))
		}
	}
	return source
}

func compareMatrixPaths(left, right QueryPath) int {
	maximum := max(len(left.EntityAddresses), len(right.EntityAddresses))
	for index := 0; index < maximum; index++ {
		if index >= len(left.EntityAddresses) {
			return -1
		}
		if index >= len(right.EntityAddresses) {
			return 1
		}
		if compared := compareStableAddressText(left.EntityAddresses[index], right.EntityAddresses[index]); compared != 0 {
			return compared
		}
		if index < len(left.RelationAddresses) && index < len(right.RelationAddresses) {
			if compared := compareStableAddressText(left.RelationAddresses[index], right.RelationAddresses[index]); compared != 0 {
				return compared
			}
		}
	}
	return compareInt(int64(len(left.RelationAddresses)), int64(len(right.RelationAddresses)))
}
