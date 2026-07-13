// SPDX-License-Identifier: LicenseRef-LayerDraw-1.0

package resolve

import (
	"sort"
	"strings"
)

func parseCanonicalID(id string) (string, string, bool) {
	parts := strings.Split(id, "/")
	if len(parts) != 2 || !isKebab(parts[0]) || !isKebab(parts[1]) {
		return "", "", false
	}
	return parts[0], parts[1], true
}

func isIdent(s string) bool {
	if s == "" || s[0] < 'a' || s[0] > 'z' {
		return false
	}
	for i := 1; i < len(s); i++ {
		c := s[i]
		if (c >= 'a' && c <= 'z') || (c >= '0' && c <= '9') || c == '_' {
			continue
		}
		return false
	}
	return true
}

func isKebab(s string) bool {
	if s == "" || s[0] < 'a' || s[0] > 'z' || strings.Contains(s, "--") || strings.HasSuffix(s, "-") {
		return false
	}
	for i := 1; i < len(s); i++ {
		c := s[i]
		if (c >= 'a' && c <= 'z') || (c >= '0' && c <= '9') || c == '-' {
			continue
		}
		return false
	}
	return true
}

func addressOf(sym StableSymbol) string {
	var b strings.Builder
	if sym.Origin.Kind == OriginPack {
		b.WriteString("ldl:pack:")
		b.WriteString(sym.Origin.Publisher)
		b.WriteByte(':')
		b.WriteString(sym.Origin.PackName)
	} else {
		b.WriteString("ldl:project:")
		b.WriteString(sym.Origin.ProjectID)
	}
	for _, seg := range sym.Path {
		b.WriteByte(':')
		b.WriteString(string(seg.Kind))
		b.WriteByte(':')
		b.WriteString(seg.ID)
	}
	return b.String()
}

// StableAddress returns the canonical address for a resolved semantic symbol.
func StableAddress(sym StableSymbol) string {
	return addressOf(sym)
}

// MoveClosureKind returns the semantic subject kind represented by a closure.
func MoveClosureKind(move MoveClosure) SubjectKind {
	if len(move.toSymbol.Path) == 0 {
		return KindProject
	}
	return move.toSymbol.Path[len(move.toSymbol.Path)-1].Kind
}

// MoveClosureOwner returns the terminal owner for an owner-scoped move.
func MoveClosureOwner(move MoveClosure) (StableSymbol, bool) {
	if len(move.toSymbol.Path) <= 1 {
		return StableSymbol{}, false
	}
	owner := StableSymbol{Origin: move.toSymbol.Origin, Path: append([]SymbolSegment{}, move.toSymbol.Path[:len(move.toSymbol.Path)-1]...)}
	return owner, true
}

func compareSymbol(a, b StableSymbol) int {
	if originRank(a.Origin) != originRank(b.Origin) {
		return originRank(a.Origin) - originRank(b.Origin)
	}
	if a.Origin.ProjectID != b.Origin.ProjectID {
		return strings.Compare(a.Origin.ProjectID, b.Origin.ProjectID)
	}
	if a.Origin.Publisher != b.Origin.Publisher {
		return strings.Compare(a.Origin.Publisher, b.Origin.Publisher)
	}
	if a.Origin.PackName != b.Origin.PackName {
		return strings.Compare(a.Origin.PackName, b.Origin.PackName)
	}
	if len(a.Path) != len(b.Path) {
		return len(a.Path) - len(b.Path)
	}
	for i := range a.Path {
		if kindRank(a.Path[i].Kind) != kindRank(b.Path[i].Kind) {
			return kindRank(a.Path[i].Kind) - kindRank(b.Path[i].Kind)
		}
		if a.Path[i].ID != b.Path[i].ID {
			return strings.Compare(a.Path[i].ID, b.Path[i].ID)
		}
	}
	return 0
}

func originRank(origin Origin) int {
	if origin.Kind == OriginPack {
		return 1
	}
	return 0
}

func kindRank(kind SubjectKind) int {
	switch kind {
	case KindEntityType:
		return 0
	case KindRelationType:
		return 1
	case KindLayer:
		return 2
	case KindEntity:
		return 3
	case KindRelation:
		return 4
	case KindQuery:
		return 5
	case KindView:
		return 6
	case KindReference:
		return 7
	case KindColumn:
		return 8
	case KindConstraint:
		return 9
	case KindRow:
		return 10
	case KindParameter:
		return 11
	case KindTableColumn:
		return 12
	case KindExport:
		return 13
	default:
		return 99
	}
}

func sortDeclarations(decls []DeclarationSymbol) {
	sort.SliceStable(decls, func(i, j int) bool {
		return compareSymbol(decls[i].Symbol, decls[j].Symbol) < 0
	})
}

// SortDeclarations orders declarations by their structured StableSymbol.
func SortDeclarations(decls []DeclarationSymbol) {
	sortDeclarations(decls)
}

// CompareStableSymbols applies the normative structured StableSymbol order.
func CompareStableSymbols(a, b StableSymbol) int {
	return compareSymbol(a, b)
}
