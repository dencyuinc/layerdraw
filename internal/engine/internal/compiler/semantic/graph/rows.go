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
			groupType, groupOK := c.singleBinding(decl.Address, resolve.KindEntityType, src)
			entity := &c.entities[index]
			if groupOK && groupType != entity.TypeAddress {
				c.diag("LDL1402", "invalid_or_duplicate_row", src, src.Range, "entity row group type does not match its owner", decl.Address, ownerAddress)
			}
			typeDefinition, exists := c.entityTypes[entity.TypeAddress]
			if !exists {
				continue
			}
			row := c.compileRow(decl, src, group, typeDefinition.Columns, ownerAddress)
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
			groupType, groupOK := c.singleBinding(decl.Address, resolve.KindRelationType, src)
			relation := &c.relations[index]
			if groupOK && groupType != relation.TypeAddress {
				c.diag("LDL1402", "invalid_or_duplicate_row", src, src.Range, "relation row group type does not match its owner", decl.Address, ownerAddress)
			}
			typeDefinition, exists := c.relationTypes[relation.TypeAddress]
			if !exists {
				continue
			}
			row := c.compileRow(decl, src, group, typeDefinition.Columns, ownerAddress)
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

func (c *compiler) compileRow(decl resolve.DeclarationSymbol, src resolve.DeclarationSource, group rowGroup, columns []definition.Column, ownerAddress string) AttributeRow {
	row := AttributeRow{ID: decl.ID, Address: decl.Address, Values: []Cell{}}
	cells := rowCells(src.Node)
	if len(cells) != len(group.header) {
		c.diag("LDL1402", "invalid_or_duplicate_row", src, src.Range, "row cell count does not match its declared header", decl.Address, ownerAddress)
	}
	columnByID := map[string]definition.Column{}
	for _, column := range columns {
		columnByID[column.ID] = column
	}
	positions := map[string]int{}
	for position, header := range group.header {
		if previous, duplicate := positions[header.id]; duplicate {
			c.diag("LDL1402", "invalid_or_duplicate_row", src, header.span, "duplicate row header column", decl.Address, ownerAddress)
			_ = previous
			continue
		}
		positions[header.id] = position
		if _, exists := columnByID[header.id]; !exists {
			c.diag("LDL1402", "invalid_or_duplicate_row", src, header.span, "unknown row header column", decl.Address, ownerAddress)
		}
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
