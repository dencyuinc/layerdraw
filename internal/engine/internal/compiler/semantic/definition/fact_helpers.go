// SPDX-License-Identifier: LicenseRef-LayerDraw-1.0

package definition

import (
	"github.com/dencyuinc/layerdraw/internal/engine/internal/compiler/resolve"
	"github.com/dencyuinc/layerdraw/internal/engine/internal/compiler/syntax"
)

// NormalizeText applies the Language 1 string normalization used by typed
// definitions, graph display names, and row scalar values.
func NormalizeText(raw string) string {
	return normalizeString(raw)
}

// NormalizeScalarLiteral parses, normalizes, and validates a row scalar using
// the same implementation used for Column defaults.
func NormalizeScalarLiteral(raw string, kind syntax.TokenKind, column Column) (Scalar, bool) {
	c := compiler{}
	scalar := c.scalar(value{raw: raw, kind: kind}, &column, resolve.DeclarationSource{}, "")
	if scalar == nil {
		return Scalar{}, false
	}
	column.Default = scalar
	if !c.validateDefault(&column, resolve.DeclarationSource{}, "", syntax.Span{}) {
		return Scalar{}, false
	}
	return *column.Default, true
}

// CompileScalarSchema compiles an owner-scoped Query parameter using the same
// scalar type, modifier, default normalization, and constraint implementation
// as an EntityType or RelationType Column. The source node must be the
// parameter statement itself: <id> <scalar-type> [modifiers...].
func CompileScalarSchema(decl resolve.DeclarationSymbol, src resolve.DeclarationSource) (Column, []resolve.Diagnostic) {
	owner := ""
	if decl.Owner != nil {
		owner = resolve.StableAddress(*decl.Owner)
	}
	column := Column{ID: decl.ID, Address: decl.Address, ReservedEnumValues: []string{}}
	parsed := values(src.Node)
	c := compiler{}
	if len(parsed) < 2 || parsed[0].raw != decl.ID || parsed[1].kind != syntax.TokenIdentifier {
		c.diag("LDL1601", "invalid_query_or_arguments", src, src.Range, "invalid query parameter scalar schema", decl.Address, owner)
		resolve.SortDiagnostics(c.diagnostics)
		return column, c.diagnostics
	}
	if !set("string", "integer", "number", "boolean", "enum", "date", "datetime")[parsed[1].raw] {
		c.diag("LDL1401", "scalar_or_column_type_mismatch", src, parsed[1].span, "invalid scalar type", decl.Address, owner)
		resolve.SortDiagnostics(c.diagnostics)
		return column, c.diagnostics
	}
	column.ValueType = ScalarType(parsed[1].raw)
	c.columnModifiers(&column, parsed[1], parsed[2:], src, owner)
	resolve.SortDiagnostics(c.diagnostics)
	return column, c.diagnostics
}

// CompileCommonFields normalizes Common fields without imposing an owner
// declaration's closed schema. The caller remains responsible for validating
// its non-common members.
func CompileCommonFields(src resolve.DeclarationSource) (Common, []resolve.Diagnostic) {
	c := compiler{}
	common := c.common(c.body(src), src, src.Address, "")
	resolve.SortDiagnostics(c.diagnostics)
	return common, c.diagnostics
}

// CompileFactCommon compiles the common fields permitted on Entity and
// Relation fact items. reserve_rows is validated by resolve and is not part of
// the normalized Common payload.
func CompileFactCommon(src resolve.DeclarationSource) (Common, []resolve.Diagnostic) {
	c := compiler{}
	b := c.body(src)
	spec := commonSpec()
	spec["reserve_rows"] = fieldSpec{card: singleton}
	c.rejectUnknown(b, src, spec)
	common := c.common(b, src, src.Address, "")
	resolve.SortDiagnostics(c.diagnostics)
	return common, c.diagnostics
}
