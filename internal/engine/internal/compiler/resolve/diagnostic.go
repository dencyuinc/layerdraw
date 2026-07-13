// SPDX-License-Identifier: LicenseRef-LayerDraw-1.0

package resolve

import (
	"sort"

	"github.com/dencyuinc/layerdraw/internal/engine/internal/compiler/syntax"
)

func (r *resolver) diag(code, key, msg string, mod ModuleKey, span syntax.Span) {
	d := Diagnostic{
		Code:       code,
		Severity:   "error",
		MessageKey: key,
		Arguments:  map[string]string{},
		Message:    msg,
	}
	d.Range = &SourceRange{
		Origin:     sourceOrigin(mod.Origin),
		ModulePath: mod.Path,
		StartByte:  span.Start,
		EndByte:    span.End,
	}
	r.diagnostics = append(r.diagnostics, d)
}

func sourceOrigin(origin Origin) SourceOrigin {
	if origin.Kind == OriginPack {
		return SourceOrigin{Kind: OriginPack, PackAddress: addressOf(StableSymbol{Origin: origin})}
	}
	return SourceOrigin{Kind: OriginProject}
}

func sortDiagnostics(ds []Diagnostic) {
	sort.SliceStable(ds, func(i, j int) bool {
		a, b := ds[i], ds[j]
		if cmp := cmpSourceRange(a.Range, b.Range); cmp != 0 {
			return cmp < 0
		}
		if severityRank(a.Severity) != severityRank(b.Severity) {
			return severityRank(a.Severity) < severityRank(b.Severity)
		}
		if a.Code != b.Code {
			return a.Code < b.Code
		}
		if a.SubjectAddress != b.SubjectAddress {
			return a.SubjectAddress < b.SubjectAddress
		}
		if a.OwnerAddress != b.OwnerAddress {
			return a.OwnerAddress < b.OwnerAddress
		}
		return a.MessageKey < b.MessageKey
	})
}

func cmpSourceRange(a, b *SourceRange) int {
	if a == nil || b == nil {
		switch {
		case a == nil && b == nil:
			return 0
		case a == nil:
			return -1
		default:
			return 1
		}
	}
	if rankOrigin(a.Origin) != rankOrigin(b.Origin) {
		return rankOrigin(a.Origin) - rankOrigin(b.Origin)
	}
	if a.Origin.PackAddress != b.Origin.PackAddress {
		if a.Origin.PackAddress < b.Origin.PackAddress {
			return -1
		}
		return 1
	}
	if a.ModulePath != b.ModulePath {
		if a.ModulePath < b.ModulePath {
			return -1
		}
		return 1
	}
	if a.StartByte != b.StartByte {
		return a.StartByte - b.StartByte
	}
	return a.EndByte - b.EndByte
}

func rankOrigin(origin SourceOrigin) int {
	if origin.Kind == OriginPack {
		return 1
	}
	return 0
}

func severityRank(severity string) int {
	switch severity {
	case "error":
		return 0
	case "warning":
		return 1
	case "info":
		return 2
	default:
		return 3
	}
}
