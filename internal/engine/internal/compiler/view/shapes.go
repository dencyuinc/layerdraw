// SPDX-License-Identifier: LicenseRef-LayerDraw-1.0

package view

import (
	"math"
	"sort"
	"strconv"
	"strings"

	"github.com/dencyuinc/layerdraw/internal/engine/internal/compiler/query"
	"github.com/dencyuinc/layerdraw/internal/engine/internal/compiler/resolve"
	"github.com/dencyuinc/layerdraw/internal/engine/internal/compiler/semantic/definition"
	"github.com/dencyuinc/layerdraw/internal/engine/internal/compiler/syntax"
)

func (c *compiler) compileShape(source resolve.DeclarationSource, declaration resolve.DeclarationSymbol, members []authoredMember) Shape {
	for _, member := range members {
		switch member.head {
		case "diagram":
			shape := c.compileDiagram(source, declaration, member)
			return Shape{Kind: ShapeDiagram, Diagram: &shape}
		case "table":
			shape := c.compileTable(source, declaration, member)
			return Shape{Kind: ShapeTable, Table: &shape}
		case "matrix":
			shape := c.compileMatrix(source, declaration, member)
			return Shape{Kind: ShapeMatrix, Matrix: &shape}
		case "tree":
			shape := c.compileTree(source, declaration, member)
			return Shape{Kind: ShapeTree, Tree: &shape}
		case "flow":
			shape := c.compileFlow(source, declaration, member)
			return Shape{Kind: ShapeFlow, Flow: &shape}
		case "context":
			shape := c.compileContext(source, declaration, member)
			return Shape{Kind: ShapeContext, Context: &shape}
		case "diff":
			shape := c.compileDiff(source, declaration, member)
			return Shape{Kind: ShapeDiff, Diff: &shape}
		}
	}
	return Shape{}
}

func (c *compiler) compileDiagram(source resolve.DeclarationSource, declaration resolve.DeclarationSymbol, member authoredMember) DiagramShape {
	shape := DiagramShape{Layout: LayoutLayered, Direction: DirectionLeftToRight, Abstraction: AbstractionNormal, Placements: []Placement{}}
	if member.block == nil || len(member.args) != 0 {
		c.diag("LDL1701", "unsupported_view_shape_or_export", source, member.span, "diagram requires a body and no arguments", declaration.Address, "")
		return shape
	}
	members := readMembers(member.block)
	rules := map[string]memberRule{"layout": {}, "direction": {}, "abstraction": {}, "composed": {flag: true}, "place": {}}
	c.validateClosedMembers(source, declaration.Address, "diagram", members, rules, false)
	seenSingleton := map[string]authoredMember{}
	seenPlacement := map[string]syntax.Span{}
	for _, item := range members {
		if item.head != "place" {
			if previous, duplicate := seenSingleton[item.head]; duplicate {
				c.diagRelated("LDL1102", "unknown_or_duplicate_schema_member", source, item.headSpan, "duplicate diagram member", declaration.Address, "", previous.headSpan)
				continue
			}
			seenSingleton[item.head] = item
		}
		switch item.head {
		case "layout":
			shape.Layout = DiagramLayout(c.enumMember(source, declaration.Address, item, string(shape.Layout), set("layered", "force", "grid", "radial", "manual")))
		case "direction":
			shape.Direction = DiagramDirection(c.enumMember(source, declaration.Address, item, string(shape.Direction), set("left_to_right", "right_to_left", "top_to_bottom", "bottom_to_top")))
		case "abstraction":
			shape.Abstraction = DiagramAbstraction(c.enumMember(source, declaration.Address, item, string(shape.Abstraction), set("summary", "normal", "detail")))
		case "composed":
			shape.Composed = c.flagMember(source, declaration.Address, item)
		case "place":
			placement, entitySpan, ok := c.compilePlacement(source, declaration, item)
			if !ok {
				continue
			}
			if previous, duplicate := seenPlacement[placement.EntityAddress]; duplicate {
				c.diagRelated("LDL1701", "unsupported_view_shape_or_export", source, entitySpan, "duplicate Entity placement", declaration.Address, "", previous)
				continue
			}
			seenPlacement[placement.EntityAddress] = entitySpan
			shape.Placements = append(shape.Placements, placement)
		}
	}
	sort.SliceStable(shape.Placements, func(i, j int) bool {
		return c.compareAddresses(shape.Placements[i].EntityAddress, shape.Placements[j].EntityAddress) < 0
	})
	return shape
}

func (c *compiler) compilePlacement(source resolve.DeclarationSource, declaration resolve.DeclarationSymbol, member authoredMember) (Placement, syntax.Span, bool) {
	if member.block != nil || len(member.args) != 5 {
		c.diag("LDL1701", "unsupported_view_shape_or_export", source, member.span, "place requires Entity, x, y, width, and height", declaration.Address, "")
		return Placement{}, member.headSpan, false
	}
	address, bound := c.singleBindingAt(declaration.Address, resolve.KindEntity, member.args[0].span, source)
	if bound && !c.graphEntities[address] {
		c.diag("LDL1801", "stale_revision_or_semantic_hash", source, member.args[0].span, "placed Entity is absent from the typed graph", declaration.Address, "")
		bound = false
	}
	values := make([]float64, 4)
	valid := bound
	for index := range values {
		value, ok := finiteNumber(member.args[index+1])
		if !ok || index >= 2 && value <= 0 {
			c.diag("LDL1701", "unsupported_view_shape_or_export", source, member.args[index+1].span, "placement coordinates must be finite and sizes positive", declaration.Address, "")
			valid = false
		}
		values[index] = value
	}
	return Placement{EntityAddress: address, X: values[0], Y: values[1], Width: values[2], Height: values[3]}, member.args[0].span, valid
}

func (c *compiler) compileTable(source resolve.DeclarationSource, declaration resolve.DeclarationSymbol, member authoredMember) TableShape {
	shape := TableShape{RowSource: RowsEntity, Columns: []TableColumn{}, Sorts: []TableSort{}}
	if member.block == nil || len(member.args) != 0 {
		c.diag("LDL1701", "unsupported_view_shape_or_export", source, member.span, "table requires a body and no arguments", declaration.Address, "")
		return shape
	}
	members := readMembers(member.block)
	rules := map[string]memberRule{"rows": {}, "entity_types": {}, "entity_id": {flag: true}, "type": {flag: true}, "layer": {flag: true}, "column": {block: true}, "sort": {}}
	c.validateClosedMembers(source, declaration.Address, "table", members, rules, false)
	seenSingleton := map[string]authoredMember{}
	for _, item := range members {
		if item.head == "column" || item.head == "sort" {
			continue
		}
		if previous, duplicate := seenSingleton[item.head]; duplicate {
			c.diagRelated("LDL1102", "unknown_or_duplicate_schema_member", source, item.headSpan, "duplicate table member", declaration.Address, "", previous.headSpan)
			continue
		}
		seenSingleton[item.head] = item
		switch item.head {
		case "rows":
			shape.RowSource = TableRowSource(c.enumMember(source, declaration.Address, item, string(shape.RowSource), set("entity", "entity_rows", "relation", "relation_rows", "automatic_relations")))
		case "entity_types":
			addresses := c.boundList(source, declaration.Address, resolve.KindEntityType, item)
			shape.EntityTypeAddresses = &addresses
		case "entity_id":
			shape.IncludeEntityID = c.flagMember(source, declaration.Address, item)
		case "type":
			shape.IncludeType = c.flagMember(source, declaration.Address, item)
		case "layer":
			shape.IncludeLayer = c.flagMember(source, declaration.Address, item)
		}
	}
	entityRows := shape.RowSource == RowsEntity || shape.RowSource == RowsEntityRows
	if !entityRows && (shape.EntityTypeAddresses != nil || shape.IncludeEntityID || shape.IncludeType || shape.IncludeLayer) {
		c.diag("LDL1701", "unsupported_view_shape_or_export", source, member.headSpan, "entity type selectors and fixed Entity columns are forbidden for Relation rows", declaration.Address, "")
	}
	columns := map[string]bool{}
	if shape.IncludeEntityID {
		columns["entity_id"] = true
	}
	if shape.IncludeType {
		columns["entity_type"] = true
	}
	if shape.IncludeLayer {
		columns["entity_layer"] = true
	}
	if shape.RowSource == RowsAutomaticRelations {
		for id := range c.automaticRelationFixedColumns(declaration.Address) {
			columns[id] = true
		}
	}
	for _, item := range members {
		if item.head != "column" {
			continue
		}
		column := c.compileTableColumn(source, declaration, shape.RowSource, item)
		if columns[column.ID] {
			span := item.headSpan
			if len(item.args) != 0 {
				span = item.args[0].span
			}
			c.diag("LDL1701", "unsupported_view_shape_or_export", source, span, "named Table Column conflicts with a fixed Column", declaration.Address, "")
		}
		columns[column.ID] = true
		shape.Columns = append(shape.Columns, column)
	}
	for _, item := range members {
		if item.head != "sort" {
			continue
		}
		shape.Sorts = append(shape.Sorts, c.compileTableSort(source, declaration, item, columns))
	}
	return shape
}

func (c *compiler) compileTableColumn(viewSource resolve.DeclarationSource, viewDecl resolve.DeclarationSymbol, rowSource TableRowSource, member authoredMember) TableColumn {
	column := TableColumn{Aggregate: AggregateNone}
	if member.block == nil || len(member.args) != 1 || member.args[0].kind != syntax.TokenIdentifier {
		c.diag("LDL1701", "unsupported_view_shape_or_export", viewSource, member.span, "column requires an ID and body", viewDecl.Address, "")
		return column
	}
	column.ID = member.args[0].raw
	column.Address = childAddress(c.declarations, viewDecl.Address, resolve.KindTableColumn, column.ID)
	source := c.sources[column.Address]
	if source.Node == nil {
		c.diag("LDL1101", "invalid_structure_syntax", viewSource, member.span, "missing Table Column declaration source", column.Address, viewDecl.Address)
		source = viewSource
	}
	members := readMembers(member.block)
	rules := map[string]memberRule{"source": {}, "label": {}, "aggregate": {}}
	c.validateClosedMembers(source, column.Address, "Table Column", members, rules, true)
	if label := oneMember(members, "label"); label != nil {
		column.Label = c.optionalString(source, resolve.DeclarationSymbol{Address: column.Address, Owner: &viewDecl.Symbol}, *label)
	}
	sourceMember := oneMember(members, "source")
	if sourceMember == nil {
		c.diag("LDL1701", "unsupported_view_shape_or_export", source, member.headSpan, "Table Column requires exactly one source", column.Address, viewDecl.Address)
	} else {
		column.Source, column.ValueType = c.compileTableColumnSource(source, viewDecl.Address, column.Address, rowSource, *sourceMember)
	}
	if aggregate := oneMember(members, "aggregate"); aggregate != nil {
		column.Aggregate = Aggregate(c.enumMember(source, column.Address, *aggregate, string(AggregateNone), set("none", "count", "count_distinct", "min", "max", "join_unique")))
	}
	c.validateAggregate(source, viewDecl.Address, &column, member.headSpan)
	return column
}

func (c *compiler) compileTableColumnSource(source resolve.DeclarationSource, viewAddress, columnAddress string, rowSource TableRowSource, member authoredMember) (TableColumnSource, TableValueType) {
	result := TableColumnSource{}
	if member.block != nil || len(member.args) < 2 {
		c.diag("LDL1701", "unsupported_view_shape_or_export", source, member.span, "invalid Table Column source", columnAddress, viewAddress)
		return result, TableValueType{}
	}
	switch member.args[0].raw {
	case "field":
		result.Kind = ColumnField
		if len(member.args) != 2 {
			c.diag("LDL1701", "unsupported_view_shape_or_export", source, member.span, "field source requires one field", columnAddress, viewAddress)
			return result, TableValueType{}
		}
		result.Field = member.args[1].raw
		valueType, ok := fieldTableType(result.Field)
		if !ok {
			c.diag("LDL1701", "unsupported_view_shape_or_export", source, member.args[1].span, "invalid Table field", columnAddress, viewAddress)
		}
		return result, valueType
	case "attribute":
		result.Kind = ColumnAttribute
		if rowSource != RowsEntityRows && rowSource != RowsRelationRows {
			c.diag("LDL1701", "unsupported_view_shape_or_export", source, member.args[0].span, "attribute source requires entity_rows or relation_rows", columnAddress, viewAddress)
		}
		seenClauses := map[string]syntax.Span{}
		for index := 2; index < len(member.args); index += 2 {
			if index+1 >= len(member.args) || !set("entity_types", "relation_types")[member.args[index].raw] || !member.args[index+1].list {
				c.diag("LDL1701", "unsupported_view_shape_or_export", source, member.span, "attribute source restrictions must be entity_types or relation_types lists", columnAddress, viewAddress)
				break
			}
			if previous, duplicate := seenClauses[member.args[index].raw]; duplicate {
				c.diagRelated("LDL1102", "unknown_or_duplicate_schema_member", source, member.args[index].span, "duplicate attribute source restriction", columnAddress, viewAddress, previous)
			}
			seenClauses[member.args[index].raw] = member.args[index].span
			if rowSource == RowsEntityRows && member.args[index].raw != "entity_types" || rowSource == RowsRelationRows && member.args[index].raw != "relation_types" {
				c.diag("LDL1701", "unsupported_view_shape_or_export", source, member.args[index].span, "attribute source restriction is incompatible with Table row source", columnAddress, viewAddress)
			}
		}
		result.ColumnAddresses = bindingAddresses(c, columnAddress, resolve.KindColumn, member.args[1].span)
		if len(result.ColumnAddresses) == 0 {
			c.diag("LDL1301", "unknown_or_ambiguous_symbol", source, member.args[1].span, "attribute source lacks resolver-owned Column bindings", columnAddress, viewAddress)
		}
		expectedOwner := resolve.KindEntityType
		if rowSource == RowsRelationRows {
			expectedOwner = resolve.KindRelationType
		}
		for _, address := range result.ColumnAddresses {
			if c.columnOwnerKind(address) != expectedOwner {
				c.diag("LDL1701", "unsupported_view_shape_or_export", source, member.args[1].span, "attribute Column owner is incompatible with Table row source", columnAddress, viewAddress)
			}
		}
		return result, c.compatibleColumnType(source, columnAddress, viewAddress, member.args[1].span, result.ColumnAddresses)
	case "relation_endpoint":
		result.Kind = ColumnRelationEndpoint
		if rowSource != RowsRelation && rowSource != RowsRelationRows {
			c.diag("LDL1701", "unsupported_view_shape_or_export", source, member.args[0].span, "relation_endpoint requires Relation rows", columnAddress, viewAddress)
		}
		if len(member.args) != 3 || !set("from", "to")[member.args[1].raw] || !set("id", "address", "display_name", "type", "layer")[member.args[2].raw] {
			c.diag("LDL1701", "unsupported_view_shape_or_export", source, member.span, "invalid relation_endpoint source", columnAddress, viewAddress)
			return result, TableValueType{}
		}
		result.Endpoint = definition.ProjectionEndpoint(member.args[1].raw)
		result.Field = member.args[2].raw
		if result.Field == "id" || result.Field == "display_name" {
			return result, scalarTableType(definition.ScalarString)
		}
		return result, TableValueType{Kind: TableValueStableAddress}
	case "derived_count":
		result.Kind = ColumnDerivedCount
		if rowSource != RowsEntity && rowSource != RowsEntityRows {
			c.diag("LDL1701", "unsupported_view_shape_or_export", source, member.args[0].span, "derived_count requires Entity rows", columnAddress, viewAddress)
		}
		if len(member.args) != 3 && len(member.args) != 4 || len(member.args) >= 3 && member.args[2].raw != "relations" || !set("outgoing", "incoming", "both")[member.args[1].raw] {
			c.diag("LDL1701", "unsupported_view_shape_or_export", source, member.span, "invalid derived_count source", columnAddress, viewAddress)
			return result, scalarTableType(definition.ScalarInteger)
		}
		result.Direction = definition.TraversalDirection(member.args[1].raw)
		if len(member.args) == 4 {
			if !member.args[3].list {
				c.diag("LDL1701", "unsupported_view_shape_or_export", source, member.args[3].span, "derived_count relation restriction requires a list", columnAddress, viewAddress)
			} else {
				addresses := c.boundValueList(source, columnAddress, resolve.KindRelationType, member.args[3])
				result.RelationTypeAddresses = &addresses
			}
		}
		return result, scalarTableType(definition.ScalarInteger)
	case "state":
		result.Kind = ColumnState
		if len(member.args) != 2 {
			c.diag("LDL1701", "unsupported_view_shape_or_export", source, member.span, "state source requires one field path", columnAddress, viewAddress)
			return result, TableValueType{}
		}
		result.StateFieldPath = query.StateFieldPath(member.args[1].raw)
		schema, ok := query.LookupStateFieldSchema(result.StateFieldPath)
		if !ok {
			c.diag("LDL1601", "invalid_query_or_arguments", source, member.args[1].span, "unknown state field path", columnAddress, viewAddress)
			return result, TableValueType{}
		}
		return result, TableValueType{
			Kind:       TableValueScalar,
			ScalarType: schema.ValueType,
			EnumValues: append([]string{}, schema.EnumValues...),
			Format:     cloneStringFormat(schema.Format),
		}
	default:
		c.diag("LDL1701", "unsupported_view_shape_or_export", source, member.args[0].span, "unknown Table Column source kind", columnAddress, viewAddress)
		return result, TableValueType{}
	}
}

func (c *compiler) columnOwnerKind(address string) resolve.SubjectKind {
	symbol, ok := c.symbols[address]
	if !ok || len(symbol.Path) < 2 || symbol.Path[len(symbol.Path)-1].Kind != resolve.KindColumn {
		return ""
	}
	return symbol.Path[len(symbol.Path)-2].Kind
}

func (c *compiler) automaticRelationFixedColumns(viewAddress string) map[string]bool {
	fixed := map[string]bool{}
	for _, address := range c.selectedQueryRelationTypes(viewAddress) {
		projection := c.effectiveRelationType(address).Projections.Table
		if projection.IncludeFrom {
			fixed["from"] = true
		}
		if projection.IncludeTo {
			fixed["to"] = true
		}
		if projection.IncludeRelationType {
			fixed["relation_type"] = true
		}
	}
	return fixed
}

func (c *compiler) selectedQueryRelationTypes(viewAddress string) []string {
	queryAddress := sourceQueryAddressFromBindings(c.bindings[viewAddress])
	recipe, ok := c.queryRecipes[queryAddress]
	if !ok {
		return []string{}
	}
	types := map[string]bool{}
	add := func(values *[]string) {
		if values == nil {
			for address := range c.relationTypes {
				types[address] = true
			}
			return
		}
		for _, address := range *values {
			types[address] = true
		}
	}
	if containsResult(recipe.Result, query.ResultInducedRelations) {
		add(recipe.Select.RelationTypeAddresses)
	}
	if containsResult(recipe.Result, query.ResultPathRelations) && recipe.Traversal != nil {
		if recipe.Traversal.RelationTypeAddresses != nil {
			add(recipe.Traversal.RelationTypeAddresses)
		} else {
			add(recipe.Select.RelationTypeAddresses)
		}
	}
	addresses := make([]string, 0, len(types))
	for address := range types {
		addresses = append(addresses, address)
	}
	c.sortAddresses(addresses)
	return addresses
}

func (c *compiler) validateAggregate(source resolve.DeclarationSource, viewAddress string, column *TableColumn, span syntax.Span) {
	input := column.ValueType
	switch column.Aggregate {
	case AggregateCount, AggregateCountDistinct:
		column.ValueType = scalarTableType(definition.ScalarInteger)
	case AggregateMin, AggregateMax:
		if input.Kind != TableValueScalar || !set("integer", "number", "date", "datetime", "enum")[string(input.ScalarType)] {
			c.diag("LDL1701", "unsupported_view_shape_or_export", source, span, "min/max require numeric, date, datetime, or compatible enum input", column.Address, viewAddress)
		}
	case AggregateJoinUnique:
		if input.Kind != TableValueScalar || input.ScalarType != definition.ScalarString && input.ScalarType != definition.ScalarEnum {
			c.diag("LDL1701", "unsupported_view_shape_or_export", source, span, "join_unique requires string or enum input", column.Address, viewAddress)
		}
		column.ValueType = scalarTableType(definition.ScalarString)
	}
}

func (c *compiler) compileTableSort(source resolve.DeclarationSource, declaration resolve.DeclarationSymbol, member authoredMember, columns map[string]bool) TableSort {
	sortRecipe := TableSort{}
	if member.block != nil || len(member.args) != 4 || member.args[2].raw != "nulls" {
		c.diag("LDL1701", "unsupported_view_shape_or_export", source, member.span, "sort requires Column, direction, nulls, and absent order", declaration.Address, "")
		return sortRecipe
	}
	sortRecipe.ColumnID = member.args[0].raw
	if !columns[sortRecipe.ColumnID] {
		c.diag("LDL1701", "unsupported_view_shape_or_export", source, member.args[0].span, "sort references an unknown output Column", declaration.Address, "")
	}
	sortRecipe.Direction = SortDirection(c.enumValue(source, declaration.Address, member.args[1], "", set("ascending", "descending"), "sort direction"))
	sortRecipe.Absent = AbsentOrder(c.enumValue(source, declaration.Address, member.args[3], "", set("first", "last"), "sort null order"))
	return sortRecipe
}

func (c *compiler) compileMatrix(source resolve.DeclarationSource, declaration resolve.DeclarationSymbol, member authoredMember) MatrixShape {
	shape := MatrixShape{RowAxis: MatrixAxis{LabelField: AxisLabelDisplayName}, ColumnAxis: MatrixAxis{LabelField: AxisLabelDisplayName}, Cell: MatrixCell{Direction: definition.TraversalOutgoing, Semantic: MatrixRelationRefs, Display: MatrixExists}}
	if member.block == nil || len(member.args) != 0 {
		c.diag("LDL1701", "unsupported_view_shape_or_export", source, member.span, "matrix requires a body and no arguments", declaration.Address, "")
		return shape
	}
	members := readMembers(member.block)
	rules := map[string]memberRule{"row_axis": {block: true}, "column_axis": {block: true}, "cell": {block: true}}
	c.validateClosedMembers(source, declaration.Address, "matrix", members, rules, true)
	row, column, cell := oneMember(members, "row_axis"), oneMember(members, "column_axis"), oneMember(members, "cell")
	if row == nil || column == nil || cell == nil {
		c.diag("LDL1701", "unsupported_view_shape_or_export", source, member.headSpan, "matrix requires row_axis, column_axis, and cell", declaration.Address, "")
		return shape
	}
	shape.RowAxis = c.compileMatrixAxis(source, declaration, *row)
	shape.ColumnAxis = c.compileMatrixAxis(source, declaration, *column)
	shape.Cell = c.compileMatrixCell(source, declaration, *cell)
	return shape
}

func (c *compiler) compileMatrixAxis(source resolve.DeclarationSource, declaration resolve.DeclarationSymbol, member authoredMember) MatrixAxis {
	axis := MatrixAxis{LabelField: AxisLabelDisplayName}
	members := readMembers(member.block)
	c.validateClosedMembers(source, declaration.Address, "matrix axis", members, map[string]memberRule{"entity_types": {}, "label": {}}, true)
	if types := oneMember(members, "entity_types"); types != nil {
		addresses := c.boundList(source, declaration.Address, resolve.KindEntityType, *types)
		axis.EntityTypeAddresses = &addresses
	}
	if label := oneMember(members, "label"); label != nil {
		axis.LabelField = AxisLabelField(c.enumMember(source, declaration.Address, *label, string(axis.LabelField), set("id", "display_name", "type", "layer")))
	}
	return axis
}

func (c *compiler) compileMatrixCell(source resolve.DeclarationSource, declaration resolve.DeclarationSymbol, member authoredMember) MatrixCell {
	cell := MatrixCell{Direction: definition.TraversalOutgoing, Semantic: MatrixRelationRefs, Display: MatrixExists}
	members := readMembers(member.block)
	c.validateClosedMembers(source, declaration.Address, "matrix cell", members, map[string]memberRule{"relation_types": {}, "direction": {}, "semantic": {}, "display": {}, "attributes": {}}, true)
	relationTypeAddresses := c.selectedQueryRelationTypes(declaration.Address)
	if types := oneMember(members, "relation_types"); types != nil {
		addresses := c.boundList(source, declaration.Address, resolve.KindRelationType, *types)
		cell.RelationTypeAddresses = &addresses
		relationTypeAddresses = addresses
	}
	if direction := oneMember(members, "direction"); direction != nil {
		cell.Direction = definition.TraversalDirection(c.enumMember(source, declaration.Address, *direction, string(cell.Direction), set("outgoing", "incoming", "both")))
	}
	if semantic := oneMember(members, "semantic"); semantic != nil {
		cell.Semantic = MatrixSemantic(c.enumMember(source, declaration.Address, *semantic, string(cell.Semantic), set("relation_refs", "path_refs")))
	}
	if display := oneMember(members, "display"); display != nil {
		cell.Display = MatrixDisplay(c.enumMember(source, declaration.Address, *display, string(cell.Display), set("exists", "count", "relation_types", "attribute_summary")))
	}
	if attributes := oneMember(members, "attributes"); attributes != nil {
		addresses := c.boundList(source, declaration.Address, resolve.KindColumn, *attributes)
		cell.AttributeColumnAddresses = &addresses
		c.compatibleColumnType(source, declaration.Address, "", attributes.span, addresses)
	}
	if cell.Display == MatrixAttributeSummary {
		if cell.AttributeColumnAddresses == nil || len(*cell.AttributeColumnAddresses) == 0 {
			c.diag("LDL1701", "unsupported_view_shape_or_export", source, member.headSpan, "attribute_summary requires a non-empty attributes list", declaration.Address, "")
		}
		if cell.Semantic != MatrixRelationRefs {
			c.diag("LDL1701", "unsupported_view_shape_or_export", source, member.headSpan, "attribute_summary requires relation_refs semantics", declaration.Address, "")
		}
	} else if cell.AttributeColumnAddresses != nil {
		c.diag("LDL1701", "unsupported_view_shape_or_export", source, oneMember(members, "attributes").headSpan, "attributes are forbidden for this matrix display", declaration.Address, "")
	}
	for _, address := range relationTypeAddresses {
		projection := c.effectiveRelationType(address).Projections.Matrix
		if projection == nil {
			c.diag("LDL1701", "unsupported_view_shape_or_export", source, member.headSpan, "Matrix RelationType does not participate in matrix projection", declaration.Address, "")
		}
		if cell.Display == MatrixAttributeSummary && (projection == nil || !projection.IncludeRelationRows) {
			c.diag("LDL1701", "unsupported_view_shape_or_export", source, member.headSpan, "attribute_summary requires RelationType matrix row inclusion", declaration.Address, "")
		}
	}
	if cell.Semantic == MatrixPathRefs {
		queryAddress := sourceQueryAddressFromBindings(c.bindings[declaration.Address])
		queryRecipe := c.queryRecipes[queryAddress]
		if queryRecipe.Traversal == nil || !containsResult(queryRecipe.Result, query.ResultPathRelations) {
			c.diag("LDL1701", "unsupported_view_shape_or_export", source, member.headSpan, "path_refs requires a traversing Query that retains paths", declaration.Address, "")
		}
	}
	return cell
}

func (c *compiler) compileTree(source resolve.DeclarationSource, declaration resolve.DeclarationSymbol, member authoredMember) TreeShape {
	shape := TreeShape{RelationTypeAddresses: []string{}, CyclePolicy: TreeCycleError, SharedChildPolicy: SharedChildError}
	if member.block == nil || len(member.args) != 0 {
		c.diag("LDL1701", "unsupported_view_shape_or_export", source, member.span, "tree requires a body and no arguments", declaration.Address, "")
		return shape
	}
	members := readMembers(member.block)
	c.validateClosedMembers(source, declaration.Address, "tree", members, map[string]memberRule{"relation_types": {}, "cycle_policy": {}, "shared_child_policy": {}}, true)
	if types := oneMember(members, "relation_types"); types != nil {
		shape.RelationTypeAddresses = c.boundList(source, declaration.Address, resolve.KindRelationType, *types)
	}
	if len(shape.RelationTypeAddresses) == 0 {
		c.diag("LDL1701", "unsupported_view_shape_or_export", source, member.headSpan, "tree requires non-empty relation_types", declaration.Address, "")
	}
	for _, address := range shape.RelationTypeAddresses {
		if c.effectiveRelationType(address).Projections.Tree == nil {
			c.diag("LDL1701", "unsupported_view_shape_or_export", source, member.headSpan, "Tree RelationType has no tree projection", declaration.Address, "")
		}
	}
	if policy := oneMember(members, "cycle_policy"); policy != nil {
		shape.CyclePolicy = TreeCyclePolicy(c.enumMember(source, declaration.Address, *policy, string(shape.CyclePolicy), set("error", "truncate", "duplicate_occurrence")))
	}
	if policy := oneMember(members, "shared_child_policy"); policy != nil {
		shape.SharedChildPolicy = SharedChildPolicy(c.enumMember(source, declaration.Address, *policy, string(shape.SharedChildPolicy), set("error", "duplicate_occurrence", "link")))
	}
	return shape
}

func (c *compiler) compileFlow(source resolve.DeclarationSource, declaration resolve.DeclarationSymbol, member authoredMember) FlowShape {
	shape := FlowShape{RelationTypeAddresses: []string{}, LaneBy: LaneNone, CyclePolicy: FlowCycleIncludeCycleRef}
	if member.block == nil || len(member.args) != 0 {
		c.diag("LDL1701", "unsupported_view_shape_or_export", source, member.span, "flow requires a body and no arguments", declaration.Address, "")
		return shape
	}
	members := readMembers(member.block)
	c.validateClosedMembers(source, declaration.Address, "flow", members, map[string]memberRule{"relation_types": {}, "lane_by": {}, "cycle_policy": {}, "preserve_parallel": {flag: true}}, true)
	if types := oneMember(members, "relation_types"); types != nil {
		shape.RelationTypeAddresses = c.boundList(source, declaration.Address, resolve.KindRelationType, *types)
	}
	if len(shape.RelationTypeAddresses) == 0 {
		c.diag("LDL1701", "unsupported_view_shape_or_export", source, member.headSpan, "flow requires non-empty relation_types", declaration.Address, "")
	}
	var branchType *TableValueType
	for _, address := range shape.RelationTypeAddresses {
		projection := c.effectiveRelationType(address).Projections.Flow
		if projection == nil {
			c.diag("LDL1701", "unsupported_view_shape_or_export", source, member.headSpan, "Flow RelationType has no flow projection", declaration.Address, "")
			continue
		}
		if projection.BranchValueColumnAddress != nil {
			valueType := c.compatibleColumnType(source, declaration.Address, "", member.headSpan, []string{*projection.BranchValueColumnAddress})
			if branchType != nil && !sameTableType(*branchType, valueType) {
				c.diag("LDL1701", "unsupported_view_shape_or_export", source, member.headSpan, "Flow branch Columns have incompatible scalar schemas", declaration.Address, "")
			}
			branchType = &valueType
		}
	}
	if lane := oneMember(members, "lane_by"); lane != nil {
		if len(lane.args) != 1 {
			c.diag("LDL1701", "unsupported_view_shape_or_export", source, lane.span, "lane_by requires one mode", declaration.Address, "")
		} else if strings.HasPrefix(lane.args[0].raw, "attribute.") {
			shape.LaneBy = LaneAttribute
			addresses := bindingAddresses(c, declaration.Address, resolve.KindColumn, lane.args[0].span)
			shape.LaneColumnAddresses = &addresses
			if len(addresses) == 0 {
				c.diag("LDL1301", "unknown_or_ambiguous_symbol", source, lane.args[0].span, "attribute lane lacks resolver-owned Column bindings", declaration.Address, "")
			} else {
				c.compatibleColumnType(source, declaration.Address, "", lane.args[0].span, addresses)
			}
		} else {
			shape.LaneBy = LaneBy(c.enumValue(source, declaration.Address, lane.args[0], string(shape.LaneBy), set("none", "layer", "entity_type"), "lane_by"))
		}
	}
	if policy := oneMember(members, "cycle_policy"); policy != nil {
		shape.CyclePolicy = FlowCyclePolicy(c.enumMember(source, declaration.Address, *policy, string(shape.CyclePolicy), set("error", "truncate", "include_cycle_ref")))
	}
	if flag := oneMember(members, "preserve_parallel"); flag != nil {
		shape.PreserveParallel = c.flagMember(source, declaration.Address, *flag)
	}
	return shape
}

func (c *compiler) compileContext(source resolve.DeclarationSource, declaration resolve.DeclarationSymbol, member authoredMember) ContextShape {
	shape := ContextShape{GroupBy: ContextGroupLayer, Incoming: true, Outgoing: true}
	if member.block == nil || len(member.args) != 0 {
		c.diag("LDL1701", "unsupported_view_shape_or_export", source, member.span, "context requires a body and no arguments", declaration.Address, "")
		return shape
	}
	members := readMembers(member.block)
	c.validateClosedMembers(source, declaration.Address, "context", members, map[string]memberRule{"group_by": {}, "entity_rows": {flag: true}, "relation_rows": {flag: true}, "incoming": {flag: true}, "outgoing": {flag: true}}, true)
	if group := oneMember(members, "group_by"); group != nil {
		shape.GroupBy = ContextGroupBy(c.enumMember(source, declaration.Address, *group, string(shape.GroupBy), set("none", "layer", "entity_type")))
	}
	if flag := oneMember(members, "entity_rows"); flag != nil {
		shape.IncludeEntityRows = c.flagMember(source, declaration.Address, *flag)
	}
	if flag := oneMember(members, "relation_rows"); flag != nil {
		shape.IncludeRelationRows = c.flagMember(source, declaration.Address, *flag)
	}
	// Incoming and outgoing default true. Their presence is an affirmative flag,
	// retained explicitly so the normalized recipe is closed.
	if flag := oneMember(members, "incoming"); flag != nil {
		shape.Incoming = c.flagMember(source, declaration.Address, *flag)
	}
	if flag := oneMember(members, "outgoing"); flag != nil {
		shape.Outgoing = c.flagMember(source, declaration.Address, *flag)
	}
	return shape
}

func (c *compiler) compileDiff(source resolve.DeclarationSource, declaration resolve.DeclarationSymbol, member authoredMember) DiffShape {
	shape := DiffShape{Include: append([]DiffSubjectKind{}, allDiffKinds...)}
	if member.block == nil || len(member.args) != 0 {
		c.diag("LDL1701", "unsupported_view_shape_or_export", source, member.span, "diff requires a body and no arguments", declaration.Address, "")
		return shape
	}
	members := readMembers(member.block)
	c.validateClosedMembers(source, declaration.Address, "diff", members, map[string]memberRule{"include": {}, "detect_moves": {flag: true}}, true)
	if include := oneMember(members, "include"); include != nil {
		shape.Include = c.diffInclude(source, declaration, *include)
	}
	if flag := oneMember(members, "detect_moves"); flag != nil {
		shape.DetectMoves = c.flagMember(source, declaration.Address, *flag)
	}
	return shape
}

var allDiffKinds = []DiffSubjectKind{
	DiffProject, DiffPack, DiffEntityType, DiffRelationType, DiffLayer, DiffEntity, DiffRelation, DiffQuery, DiffView, DiffReference,
	DiffEntityTypeColumn, DiffEntityTypeConstraint, DiffRelationTypeColumn, DiffRelationTypeConstraint, DiffEntityRow, DiffRelationRow,
	DiffQueryParameter, DiffViewTableColumn, DiffViewExport,
}

func (c *compiler) diffInclude(source resolve.DeclarationSource, declaration resolve.DeclarationSymbol, member authoredMember) []DiffSubjectKind {
	if member.block != nil || len(member.args) != 1 || !member.args[0].list {
		c.diag("LDL1701", "unsupported_view_shape_or_export", source, member.span, "diff include requires one list", declaration.Address, "")
		return []DiffSubjectKind{}
	}
	rank := map[DiffSubjectKind]int{}
	for index, kind := range allDiffKinds {
		rank[kind] = index
	}
	seen := map[DiffSubjectKind]syntax.Span{}
	var out []DiffSubjectKind
	for _, item := range listItems(member.args[0]) {
		kind := DiffSubjectKind(item.raw)
		if _, ok := rank[kind]; !ok {
			c.diag("LDL1701", "unsupported_view_shape_or_export", source, item.span, "unknown Diff subject kind", declaration.Address, "")
			continue
		}
		if previous, duplicate := seen[kind]; duplicate {
			c.diagRelated("LDL1701", "unsupported_view_shape_or_export", source, item.span, "duplicate Diff subject kind", declaration.Address, "", previous)
			continue
		}
		seen[kind] = item.span
		out = append(out, kind)
	}
	sort.SliceStable(out, func(i, j int) bool { return rank[out[i]] < rank[out[j]] })
	return out
}

func (c *compiler) boundList(source resolve.DeclarationSource, sourceAddress string, kind resolve.SubjectKind, member authoredMember) []string {
	if member.block != nil || len(member.args) != 1 || !member.args[0].list {
		c.diag("LDL1701", "unsupported_view_shape_or_export", source, member.span, member.head+" requires one list", sourceAddress, ownerForSource(c.declarations, sourceAddress))
		return []string{}
	}
	return c.boundValueList(source, sourceAddress, kind, member.args[0])
}

func (c *compiler) boundValueList(source resolve.DeclarationSource, sourceAddress string, kind resolve.SubjectKind, value authoredValue) []string {
	seen := map[string]syntax.Span{}
	var out []string
	for _, item := range listItems(value) {
		address, ok := c.singleBindingAt(sourceAddress, kind, item.span, source)
		if !ok {
			continue
		}
		if previous, duplicate := seen[address]; duplicate {
			c.diagRelated("LDL1701", "unsupported_view_shape_or_export", source, item.span, "duplicate typed selector", sourceAddress, ownerForSource(c.declarations, sourceAddress), previous)
			continue
		}
		seen[address] = item.span
		out = append(out, address)
	}
	c.sortAddresses(out)
	return out
}

func bindingAddresses(c *compiler, sourceAddress string, kind resolve.SubjectKind, span syntax.Span) []string {
	bindings := c.bindingsAt(sourceAddress, kind, span)
	out := make([]string, 0, len(bindings))
	seen := map[string]bool{}
	for _, binding := range bindings {
		if !seen[binding.TargetAddress] {
			seen[binding.TargetAddress] = true
			out = append(out, binding.TargetAddress)
		}
	}
	return out
}

func (c *compiler) compatibleColumnType(source resolve.DeclarationSource, subject, owner string, span syntax.Span, addresses []string) TableValueType {
	var result TableValueType
	for index, address := range addresses {
		column, ok := c.columns[address]
		if !ok {
			c.diag("LDL1801", "stale_revision_or_semantic_hash", source, span, "Column binding is absent from typed definitions", subject, owner)
			continue
		}
		candidate := TableValueType{Kind: TableValueScalar, ScalarType: column.ValueType, EnumValues: append([]string{}, column.EnumValues...), Format: cloneStringFormat(column.Format)}
		if index == 0 || result.Kind == "" {
			result = candidate
		} else if !sameTableType(result, candidate) {
			c.diag("LDL1701", "unsupported_view_shape_or_export", source, span, "Columns have incompatible scalar schemas", subject, owner)
		}
	}
	return result
}

func cloneStringFormat(value *definition.StringFormat) *definition.StringFormat {
	if value == nil {
		return nil
	}
	copy := *value
	return &copy
}

func sameTableType(a, b TableValueType) bool {
	if a.Kind != b.Kind || a.ScalarType != b.ScalarType || !equalStrings(a.EnumValues, b.EnumValues) {
		return false
	}
	if a.Format == nil || b.Format == nil {
		return a.Format == nil && b.Format == nil
	}
	return *a.Format == *b.Format
}

func fieldTableType(field string) (TableValueType, bool) {
	switch field {
	case "id", "display_name", "description":
		return scalarTableType(definition.ScalarString), true
	case "address", "type", "layer":
		return TableValueType{Kind: TableValueStableAddress}, true
	case "tags":
		return TableValueType{Kind: TableValueStringSet}, true
	default:
		return TableValueType{}, false
	}
}

func scalarTableType(valueType definition.ScalarType) TableValueType {
	return TableValueType{Kind: TableValueScalar, ScalarType: valueType, EnumValues: []string{}}
}

func (c *compiler) enumMember(source resolve.DeclarationSource, subject string, member authoredMember, fallback string, allowed map[string]bool) string {
	if member.block != nil || len(member.args) != 1 {
		c.diag("LDL1701", "unsupported_view_shape_or_export", source, member.span, member.head+" requires one value", subject, ownerForSource(c.declarations, subject))
		return fallback
	}
	return c.enumValue(source, subject, member.args[0], fallback, allowed, member.head)
}

func (c *compiler) enumValue(source resolve.DeclarationSource, subject string, value authoredValue, fallback string, allowed map[string]bool, label string) string {
	if value.kind != syntax.TokenIdentifier || !allowed[value.raw] {
		c.diag("LDL1701", "unsupported_view_shape_or_export", source, value.span, "invalid "+label, subject, ownerForSource(c.declarations, subject))
		return fallback
	}
	return value.raw
}

func (c *compiler) flagMember(source resolve.DeclarationSource, subject string, member authoredMember) bool {
	if member.block != nil || len(member.args) != 0 {
		c.diag("LDL1701", "unsupported_view_shape_or_export", source, member.span, member.head+" is a flag", subject, ownerForSource(c.declarations, subject))
		return false
	}
	return true
}

func finiteNumber(value authoredValue) (float64, bool) {
	if value.kind != syntax.TokenInteger && value.kind != syntax.TokenNumber {
		return 0, false
	}
	number, err := strconv.ParseFloat(value.raw, 64)
	if number == 0 {
		number = 0
	}
	return number, err == nil && !math.IsInf(number, 0) && !math.IsNaN(number)
}

func childAddress(declarations []resolve.DeclarationSymbol, owner string, kind resolve.SubjectKind, id string) string {
	for _, declaration := range declarations {
		if declaration.Kind == kind && declaration.ID == id && declaration.Owner != nil && resolve.StableAddress(*declaration.Owner) == owner {
			return declaration.Address
		}
	}
	return ""
}

func sourceQueryAddressFromBindings(bindings []resolve.SourceBinding) string {
	for _, binding := range bindings {
		if binding.ExpectedKind == resolve.KindQuery && (binding.Via == "view:source.query" || binding.Via == "view:source.diff.query") {
			return binding.TargetAddress
		}
	}
	return ""
}

func containsResult(values []query.ResultMember, target query.ResultMember) bool {
	for _, value := range values {
		if value == target {
			return true
		}
	}
	return false
}

func shapeStateReads(shape Shape) []query.StateReadDependency {
	if shape.Table == nil {
		return []query.StateReadDependency{}
	}
	var subject query.StateSubjectKind
	switch shape.Table.RowSource {
	case RowsEntity:
		subject = query.StateSubjectEntity
	case RowsEntityRows:
		subject = query.StateSubjectEntityRow
	case RowsRelation:
		subject = query.StateSubjectRelation
	case RowsRelationRows:
		subject = query.StateSubjectRelationRow
	case RowsAutomaticRelations:
		// Automatic relation rows may materialize either grain. The dependency
		// remains closed by recording both possible runtime subjects.
		var out []query.StateReadDependency
		for _, column := range shape.Table.Columns {
			if column.Source.Kind == ColumnState {
				out = append(out,
					query.StateReadDependency{SubjectKind: query.StateSubjectRelation, FieldPath: column.Source.StateFieldPath, ValueType: column.ValueType.ScalarType},
					query.StateReadDependency{SubjectKind: query.StateSubjectRelationRow, FieldPath: column.Source.StateFieldPath, ValueType: column.ValueType.ScalarType},
				)
			}
		}
		return canonicalStateReads(out)
	}
	var out []query.StateReadDependency
	for _, column := range shape.Table.Columns {
		if column.Source.Kind == ColumnState {
			out = append(out, query.StateReadDependency{SubjectKind: subject, FieldPath: column.Source.StateFieldPath, ValueType: column.ValueType.ScalarType})
		}
	}
	return canonicalStateReads(out)
}

func canonicalStateReads(values []query.StateReadDependency) []query.StateReadDependency {
	return query.CanonicalStateReads(values)
}

func equalStrings(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for index := range a {
		if a[index] != b[index] {
			return false
		}
	}
	return true
}
