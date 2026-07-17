// SPDX-License-Identifier: LicenseRef-LayerDraw-1.0

package definition

import (
	"math"
	"unicode/utf8"

	"github.com/dencyuinc/layerdraw/internal/engine/internal/compiler/resolve"
	"github.com/dencyuinc/layerdraw/internal/engine/internal/compiler/syntax"
)

// NormalizeText applies the Language 1 string normalization used by typed
// definitions, graph display names, and row scalar values.
func NormalizeText(raw string) string {
	return normalizeString(raw)
}

// ScalarWorkObserver admits one deterministic unit of scalar normalization
// work. Returning false aborts normalization before the associated work is
// performed. A nil observer leaves normalization unmetered.
type ScalarWorkObserver func(int64) bool

// NormalizeScalarValue canonicalizes and validates an already typed scalar
// against one Column schema. Query execution uses this entry point so runtime
// values and authored scalar literals share the same normalization primitives.
func NormalizeScalarValue(value Scalar, column Column, observe ScalarWorkObserver) (Scalar, bool) {
	if value.Type != column.ValueType {
		return Scalar{}, false
	}

	switch value.Type {
	case ScalarString:
		normalized, ok := normalizeObservedText(value.String, observe)
		if !ok {
			return Scalar{}, false
		}
		if column.Format != nil {
			if !observeScalarWork(observe, int64(len(normalized))) {
				return Scalar{}, false
			}
			normalized, ok = normalizeStringFormat(string(*column.Format), normalized)
			if !ok {
				return Scalar{}, false
			}
		}
		length, ok := observedRuneCount(normalized, observe)
		if !ok || column.MinLength != nil && length < *column.MinLength || column.MaxLength != nil && length > *column.MaxLength {
			return Scalar{}, false
		}
		return Scalar{Type: ScalarString, String: normalized}, true
	case ScalarEnum:
		normalized, ok := normalizeObservedText(value.String, observe)
		if !ok {
			return Scalar{}, false
		}
		active, complete := observedContains(column.EnumValues, normalized, observe)
		if !complete || !active {
			return Scalar{}, false
		}
		reserved, complete := observedContains(column.ReservedEnumValues, normalized, observe)
		if !complete || reserved {
			return Scalar{}, false
		}
		return Scalar{Type: ScalarEnum, String: normalized}, true
	case ScalarDate:
		normalized, ok := normalizeObservedText(value.String, observe)
		if !ok || !observeScalarWork(observe, int64(len(normalized))) {
			return Scalar{}, false
		}
		normalized, ok = normalizeDate(normalized)
		if !ok {
			return Scalar{}, false
		}
		return Scalar{Type: ScalarDate, String: normalized}, true
	case ScalarDatetime:
		normalized, ok := normalizeObservedText(value.String, observe)
		if !ok || !observeScalarWork(observe, int64(len(normalized))) {
			return Scalar{}, false
		}
		normalized, ok = normalizeDatetime(normalized)
		if !ok {
			return Scalar{}, false
		}
		return Scalar{Type: ScalarDatetime, String: normalized}, true
	case ScalarInteger:
		if !observeScalarWork(observe, 1) || !jsonSafeInteger(value.Int) {
			return Scalar{}, false
		}
		numeric := float64(value.Int)
		if column.Min != nil && numeric < *column.Min || column.Max != nil && numeric > *column.Max {
			return Scalar{}, false
		}
		return Scalar{Type: ScalarInteger, Int: value.Int}, true
	case ScalarNumber:
		if !observeScalarWork(observe, 1) || math.IsNaN(value.Float) || math.IsInf(value.Float, 0) {
			return Scalar{}, false
		}
		numeric := value.Float
		if numeric == 0 {
			numeric = 0
		}
		if column.Min != nil && numeric < *column.Min || column.Max != nil && numeric > *column.Max {
			return Scalar{}, false
		}
		return Scalar{Type: ScalarNumber, Float: numeric}, true
	case ScalarBoolean:
		if !observeScalarWork(observe, 1) {
			return Scalar{}, false
		}
		return Scalar{Type: ScalarBoolean, Bool: value.Bool}, true
	default:
		return Scalar{}, false
	}
}

func normalizeObservedText(raw string, observe ScalarWorkObserver) (string, bool) {
	for offset := 0; offset < len(raw); {
		character, size := utf8.DecodeRuneInString(raw[offset:])
		if character == utf8.RuneError && size == 1 {
			return "", false
		}
		if !observeScalarWork(observe, int64(size)) {
			return "", false
		}
		offset += size
	}
	if !observeScalarWork(observe, int64(len(raw))) {
		return "", false
	}
	return normalizeString(raw), true
}

func observedRuneCount(value string, observe ScalarWorkObserver) (int64, bool) {
	var count int64
	for range value {
		if !observeScalarWork(observe, 1) {
			return 0, false
		}
		count++
	}
	return count, true
}

func observedContains(values []string, target string, observe ScalarWorkObserver) (bool, bool) {
	for _, candidate := range values {
		equal, complete := observedStringEqual(candidate, target, observe)
		if !complete || equal {
			return equal, complete
		}
	}
	return false, true
}

func observedStringEqual(left, right string, observe ScalarWorkObserver) (bool, bool) {
	if !observeScalarWork(observe, 1) {
		return false, false
	}
	if len(left) != len(right) {
		return false, true
	}
	for index := 0; index < len(left); index++ {
		if !observeScalarWork(observe, 1) {
			return false, false
		}
		if left[index] != right[index] {
			return false, true
		}
	}
	return true, true
}

func observeScalarWork(observe ScalarWorkObserver, amount int64) bool {
	return amount >= 0 && (observe == nil || observe(amount))
}

// NormalizeScalarLiteral parses, normalizes, and validates a row scalar using
// the same implementation used for Column defaults.
func NormalizeScalarLiteral(raw string, kind syntax.TokenKind, column Column) (Scalar, bool) {
	c := compiler{}
	scalar := c.scalar(value{raw: raw, kind: kind}, &column, resolve.DeclarationSource{}, "")
	if scalar == nil {
		return Scalar{}, false
	}
	return NormalizeScalarValue(*scalar, column, nil)
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
