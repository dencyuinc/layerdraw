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
