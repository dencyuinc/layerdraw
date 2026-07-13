// SPDX-License-Identifier: LicenseRef-LayerDraw-1.0

package syntax

import "sort"

// Diagnostic is the stable syntax diagnostic identity used by this package.
type Diagnostic struct {
	Code       string
	Severity   string
	MessageKey string
	Message    string
	Span       Span
}

func sortDiagnostics(diagnostics []Diagnostic) {
	sort.SliceStable(diagnostics, func(i, j int) bool {
		a := diagnostics[i]
		b := diagnostics[j]
		if a.Span.Start != b.Span.Start {
			return a.Span.Start < b.Span.Start
		}
		if a.Span.End != b.Span.End {
			return a.Span.End < b.Span.End
		}
		if severityRank(a.Severity) != severityRank(b.Severity) {
			return severityRank(a.Severity) < severityRank(b.Severity)
		}
		if a.Code != b.Code {
			return a.Code < b.Code
		}
		return a.MessageKey < b.MessageKey
	})
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

func invalidUTF8(span Span) Diagnostic {
	return Diagnostic{
		Code:       "LDL1001",
		Severity:   "error",
		MessageKey: "invalid_utf8",
		Message:    "invalid UTF-8",
		Span:       span,
	}
}

func invalidStructure(span Span, message string) Diagnostic {
	return Diagnostic{
		Code:       "LDL1101",
		Severity:   "error",
		MessageKey: "invalid_structure_syntax",
		Message:    message,
		Span:       span,
	}
}
