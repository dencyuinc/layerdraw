// SPDX-License-Identifier: LicenseRef-LayerDraw-1.0

package resolve

import (
	"sort"
	"strings"

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
	for i := range ds {
		sortRelated(ds[i].Related)
		ds[i].Related = dedupeRelated(ds[i].Related)
	}
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
		if a.MessageKey != b.MessageKey {
			return a.MessageKey < b.MessageKey
		}
		return canonicalArgs(a.Arguments) < canonicalArgs(b.Arguments)
	})
}

// SortDiagnostics applies the compiler-wide deterministic diagnostic order.
func SortDiagnostics(ds []Diagnostic) {
	sortDiagnostics(ds)
}

// CloneDiagnostics returns a fully independent diagnostic snapshot suitable
// for sorting and publication by a downstream compiler stage.
func CloneDiagnostics(diagnostics []Diagnostic) []Diagnostic {
	out := make([]Diagnostic, len(diagnostics))
	for i, diagnostic := range diagnostics {
		out[i] = diagnostic
		out[i].Arguments = cloneStringMap(diagnostic.Arguments)
		out[i].Range = cloneSourceRange(diagnostic.Range)
		out[i].Related = make([]DiagnosticRelated, len(diagnostic.Related))
		for j, related := range diagnostic.Related {
			out[i].Related[j] = related
			out[i].Related[j].Range = cloneSourceRange(related.Range)
		}
	}
	return out
}

func cloneStringMap(values map[string]string) map[string]string {
	if values == nil {
		return nil
	}
	out := make(map[string]string, len(values))
	for key, value := range values {
		out[key] = value
	}
	return out
}

func cloneSourceRange(source *SourceRange) *SourceRange {
	if source == nil {
		return nil
	}
	out := *source
	return &out
}

func canonicalArgs(args map[string]string) string {
	if len(args) == 0 {
		return ""
	}
	keys := make([]string, 0, len(args))
	for key := range args {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	var b strings.Builder
	for _, key := range keys {
		b.WriteString(key)
		b.WriteByte('=')
		b.WriteString(args[key])
		b.WriteByte('\n')
	}
	return b.String()
}

func sortRelated(items []DiagnosticRelated) {
	sort.SliceStable(items, func(i, j int) bool {
		a, b := items[i], items[j]
		if a.Relation != b.Relation {
			return a.Relation < b.Relation
		}
		if cmp := cmpSourceRange(a.Range, b.Range); cmp != 0 {
			return cmp < 0
		}
		if a.SubjectAddress != b.SubjectAddress {
			return a.SubjectAddress < b.SubjectAddress
		}
		if a.OwnerAddress != b.OwnerAddress {
			return a.OwnerAddress < b.OwnerAddress
		}
		return false
	})
}

func dedupeRelated(items []DiagnosticRelated) []DiagnosticRelated {
	if len(items) < 2 {
		return items
	}
	out := items[:0]
	var prev string
	for _, item := range items {
		key := item.Relation + "|" + rangeKey(item.Range) + "|" + item.SubjectAddress + "|" + item.OwnerAddress
		if key == prev {
			continue
		}
		out = append(out, item)
		prev = key
	}
	return out
}

func rangeKey(r *SourceRange) string {
	if r == nil {
		return ""
	}
	return string(r.Origin.Kind) + "|" + r.Origin.PackAddress + "|" + r.ModulePath + "|" + itoa(r.StartByte) + "|" + itoa(r.EndByte)
}

func itoa(v int) string {
	if v == 0 {
		return "0"
	}
	var buf [20]byte
	i := len(buf)
	n := v
	for n > 0 {
		i--
		buf[i] = byte('0' + n%10)
		n /= 10
	}
	return string(buf[i:])
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
