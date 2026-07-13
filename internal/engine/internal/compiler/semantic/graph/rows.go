// SPDX-License-Identifier: LicenseRef-LayerDraw-1.0

package graph

import (
	"math"
	"sort"
	"strconv"
	"strings"

	"github.com/dencyuinc/layerdraw/internal/engine/internal/compiler/resolve"
	"github.com/dencyuinc/layerdraw/internal/engine/internal/compiler/semantic/definition"
)

func (c *compiler) compileRows() {
	c.validateFactGroups()
	for _, decl := range c.declarations {
		if decl.Kind != resolve.KindRow {
			continue
		}
		src, ok := c.sources[decl.Address]
		if !ok || src.Node == nil || decl.Owner == nil || len(decl.Owner.Path) == 0 {
			c.diag("LDL1402", "invalid_or_duplicate_row", src, decl.Range, "row is missing its source or owner", decl.Address, "")
			continue
		}
		group, ok := c.rowGroups[sourceKey{module: src.Module, span: src.Range}]
		if !ok {
			c.diag("LDL1402", "invalid_or_duplicate_row", src, src.Range, "row is missing its declared header", decl.Address, resolve.StableAddress(*decl.Owner))
			continue
		}
		toks := directTokens(src.Node)
		if len(toks) < 3 || toks[1].Raw != decl.ID {
			c.diag("LDL1402", "invalid_or_duplicate_row", src, src.Range, "row identity does not match its resolved symbol", decl.Address, resolve.StableAddress(*decl.Owner))
			continue
		}
		ownerAddress := resolve.StableAddress(*decl.Owner)
		ownerKind := decl.Owner.Path[len(decl.Owner.Path)-1].Kind
		switch ownerKind {
		case resolve.KindEntity:
			index, exists := c.entityIndex[ownerAddress]
			if !exists {
				c.diag("LDL1402", "invalid_or_duplicate_row", src, src.Range, "row owner is not a compiled entity", decl.Address, ownerAddress)
				continue
			}
			ownerSource := c.sources[ownerAddress]
			if ownerSource.Module != src.Module {
				c.diag("LDL1402", "invalid_or_duplicate_row", src, src.Range, "entity row owner must be declared in the same module", decl.Address, ownerAddress)
			}
			entity := &c.entities[index]
			if group.typeAddress != "" && group.typeAddress != entity.TypeAddress {
				c.diag("LDL1402", "invalid_or_duplicate_row", src, src.Range, "entity row group type does not match its owner", decl.Address, ownerAddress)
			}
			typeDefinition, exists := c.entityTypes[entity.TypeAddress]
			if !exists {
				continue
			}
			row := c.compileRow(decl, src, group.header, typeDefinition.Columns, ownerAddress)
			entity.Rows = append(entity.Rows, row)
		case resolve.KindRelation:
			index, exists := c.relationIndex[ownerAddress]
			if !exists {
				c.diag("LDL1402", "invalid_or_duplicate_row", src, src.Range, "row owner is not a compiled relation", decl.Address, ownerAddress)
				continue
			}
			ownerSource := c.sources[ownerAddress]
			if ownerSource.Module != src.Module {
				c.diag("LDL1402", "invalid_or_duplicate_row", src, src.Range, "relation row owner must be declared in the same module", decl.Address, ownerAddress)
			}
			relation := &c.relations[index]
			if group.typeAddress != "" && group.typeAddress != relation.TypeAddress {
				c.diag("LDL1402", "invalid_or_duplicate_row", src, src.Range, "relation row group type does not match its owner", decl.Address, ownerAddress)
			}
			typeDefinition, exists := c.relationTypes[relation.TypeAddress]
			if !exists {
				continue
			}
			row := c.compileRow(decl, src, group.header, typeDefinition.Columns, ownerAddress)
			relation.Rows = append(relation.Rows, row)
		default:
			c.diag("LDL1402", "invalid_or_duplicate_row", src, src.Range, "row owner must be an entity or relation", decl.Address, ownerAddress)
		}
	}
	for i := range c.entities {
		if schema, ok := c.entityTypes[c.entities[i].TypeAddress]; ok {
			c.validateUnique(c.entities[i].Rows, schema.UniqueConstraints, c.entities[i].Address)
		}
	}
	for i := range c.relations {
		if schema, ok := c.relationTypes[c.relations[i].TypeAddress]; ok {
			c.validateUnique(c.relations[i].Rows, schema.UniqueConstraints, c.relations[i].Address)
		}
	}
}

func (c *compiler) validateFactGroups() {
	if c.groupsChecked {
		return
	}
	c.groupsChecked = true
	for _, group := range c.factGroups {
		src := resolve.DeclarationSource{Module: group.module, Range: group.span}
		switch group.kind {
		case "entities":
			typeAddress, typeOK := c.validateGroupRef(group, 0, resolve.KindEntityType, src)
			if typeOK {
				group.typeAddress = typeAddress
				if _, exists := c.entityTypes[typeAddress]; !exists {
					c.diag("LDL1301", "unknown_or_ambiguous_symbol", src, group.refs[0].span, "entity group type is not in the typed definition", typeAddress, "")
				}
			}
			layerAddress, layerOK := c.validateGroupRef(group, 1, resolve.KindLayer, src)
			if layerOK {
				if _, exists := c.layers[layerAddress]; !exists {
					c.diag("LDL1301", "unknown_or_ambiguous_symbol", src, group.refs[1].span, "entity group layer is not in the typed definition", layerAddress, "")
				}
			}
		case "relations":
			typeAddress, typeOK := c.validateGroupRef(group, 0, resolve.KindRelationType, src)
			if typeOK {
				group.typeAddress = typeAddress
				if _, exists := c.relationTypes[typeAddress]; !exists {
					c.diag("LDL1301", "unknown_or_ambiguous_symbol", src, group.refs[0].span, "relation group type is not in the typed definition", typeAddress, "")
				}
			}
		case "rows":
			typeAddress, typeOK := c.validateGroupRef(group, 0, resolve.KindEntityType, src)
			if typeOK {
				group.typeAddress = typeAddress
				if schema, exists := c.entityTypes[typeAddress]; exists {
					c.validateGroupHeader(group, schema.Columns, src)
				} else {
					c.diag("LDL1301", "unknown_or_ambiguous_symbol", src, group.refs[0].span, "row group type is not in the typed definition", typeAddress, "")
				}
			}
		case "relation_rows":
			typeAddress, typeOK := c.validateGroupRef(group, 0, resolve.KindRelationType, src)
			if typeOK {
				group.typeAddress = typeAddress
				if schema, exists := c.relationTypes[typeAddress]; exists {
					c.validateGroupHeader(group, schema.Columns, src)
				} else {
					c.diag("LDL1301", "unknown_or_ambiguous_symbol", src, group.refs[0].span, "relation row group type is not in the typed definition", typeAddress, "")
				}
			}
		}
	}
}

func (c *compiler) validateGroupRef(group *factGroup, index int, kind resolve.SubjectKind, src resolve.DeclarationSource) (string, bool) {
	if index >= len(group.refs) || group.refs[index].kind != kind {
		c.diag("LDL1301", "unknown_or_ambiguous_symbol", src, group.span, "fact group is missing a required header reference", "", "")
		return "", false
	}
	span := group.refs[index].span
	targets := map[string]bool{}
	for _, binding := range c.input.Resolve.Bindings {
		if binding.Module == group.module && binding.ExpectedKind == kind && binding.Range == span {
			targets[binding.TargetAddress] = true
		}
	}
	if len(targets) != 1 {
		c.diag("LDL1301", "unknown_or_ambiguous_symbol", src, span, "fact group header must have exactly one resolved binding", "", "")
		return "", false
	}
	for address := range targets {
		return address, true
	}
	return "", false
}

func (c *compiler) validateGroupHeader(group *factGroup, columns []definition.Column, src resolve.DeclarationSource) {
	columnByID := map[string]bool{}
	for _, column := range columns {
		columnByID[column.ID] = true
	}
	seen := map[string]bool{}
	for _, header := range group.header {
		if seen[header.id] {
			c.diag("LDL1402", "invalid_or_duplicate_row", src, header.span, "duplicate row header column", group.typeAddress, "")
			continue
		}
		seen[header.id] = true
		if !columnByID[header.id] {
			c.diag("LDL1402", "invalid_or_duplicate_row", src, header.span, "unknown row header column", group.typeAddress, "")
		}
	}
}

func (c *compiler) compileRow(decl resolve.DeclarationSymbol, src resolve.DeclarationSource, header []headerColumn, columns []definition.Column, ownerAddress string) AttributeRow {
	row := AttributeRow{ID: decl.ID, Address: decl.Address, Values: []Cell{}}
	cells := rowCells(src.Node)
	if len(cells) != len(header) {
		c.diag("LDL1402", "invalid_or_duplicate_row", src, src.Range, "row cell count does not match its declared header", decl.Address, ownerAddress)
	}
	positions := map[string]int{}
	for position, column := range header {
		if _, duplicate := positions[column.id]; duplicate {
			continue
		}
		positions[column.id] = position
	}
	for _, column := range columns {
		position, specified := positions[column.ID]
		if !specified {
			if column.Default != nil {
				row.Values = append(row.Values, Cell{ColumnAddress: column.Address, Value: *column.Default})
			} else if column.Required {
				c.diag("LDL1402", "invalid_or_duplicate_row", src, src.Range, "required row column is absent", decl.Address, ownerAddress)
			}
			continue
		}
		if position >= len(cells) {
			continue
		}
		cell := cells[position]
		if cell.absent {
			if column.Required {
				c.diag("LDL1402", "invalid_or_duplicate_row", src, cell.span, "required row column cannot be explicitly absent", decl.Address, ownerAddress)
			}
			continue
		}
		scalar, valid := definition.NormalizeScalarLiteral(cell.raw, cell.kind, column)
		if !valid {
			c.diag("LDL1401", "scalar_or_column_type_mismatch", src, cell.span, "row scalar does not satisfy its column", decl.Address, ownerAddress)
			continue
		}
		row.Values = append(row.Values, Cell{ColumnAddress: column.Address, Value: scalar})
	}
	sort.SliceStable(row.Values, func(i, j int) bool { return row.Values[i].ColumnAddress < row.Values[j].ColumnAddress })
	return row
}

func (c *compiler) validateUnique(rows []AttributeRow, constraints []definition.UniqueConstraint, ownerAddress string) {
	for _, constraint := range constraints {
		seen := map[string]AttributeRow{}
		for _, row := range rows {
			values := map[string]definition.Scalar{}
			for _, cell := range row.Values {
				values[cell.ColumnAddress] = cell.Value
			}
			var key strings.Builder
			complete := true
			for _, columnAddress := range constraint.ColumnAddresses {
				scalar, present := values[columnAddress]
				if !present {
					complete = false
					break
				}
				part := scalarIdentity(scalar)
				key.WriteString(strconv.Itoa(len(part)))
				key.WriteByte(':')
				key.WriteString(part)
			}
			if !complete {
				continue
			}
			identity := key.String()
			if previous, duplicate := seen[identity]; duplicate {
				c.diagRelated("LDL1403", "unique_constraint_violation", c.sources[row.Address], c.sources[row.Address].Range, "owner-local unique constraint violation", row.Address, ownerAddress, c.sources[previous.Address])
				continue
			}
			seen[identity] = row
		}
	}
}

func scalarIdentity(scalar definition.Scalar) string {
	switch scalar.Type {
	case definition.ScalarInteger:
		return string(scalar.Type) + ":" + strconv.FormatInt(scalar.Int, 10)
	case definition.ScalarNumber:
		return string(scalar.Type) + ":" + strconv.FormatUint(math.Float64bits(scalar.Float), 16)
	case definition.ScalarBoolean:
		return string(scalar.Type) + ":" + strconv.FormatBool(scalar.Bool)
	default:
		return string(scalar.Type) + ":" + scalar.String
	}
}
