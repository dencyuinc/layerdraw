// SPDX-License-Identifier: LicenseRef-LayerDraw-1.0

package syntax

import "testing"

func TestSortDiagnosticsNormativeOrder(t *testing.T) {
	t.Parallel()

	diagnostics := []Diagnostic{
		{Code: "LDL1101", Severity: "warning", MessageKey: "z", Span: Span{Start: 1, End: 2}},
		{Code: "LDL1101", Severity: "info", MessageKey: "a", Span: Span{Start: 1, End: 2}},
		{Code: "LDL1001", Severity: "error", MessageKey: "b", Span: Span{Start: 1, End: 2}},
		{Code: "LDL1101", Severity: "error", MessageKey: "a", Span: Span{Start: 0, End: 9}},
		{Code: "LDL1101", Severity: "error", MessageKey: "a", Span: Span{Start: 0, End: 1}},
	}

	sortDiagnostics(diagnostics)

	want := []Diagnostic{
		{Code: "LDL1101", Severity: "error", MessageKey: "a", Span: Span{Start: 0, End: 1}},
		{Code: "LDL1101", Severity: "error", MessageKey: "a", Span: Span{Start: 0, End: 9}},
		{Code: "LDL1001", Severity: "error", MessageKey: "b", Span: Span{Start: 1, End: 2}},
		{Code: "LDL1101", Severity: "warning", MessageKey: "z", Span: Span{Start: 1, End: 2}},
		{Code: "LDL1101", Severity: "info", MessageKey: "a", Span: Span{Start: 1, End: 2}},
	}
	for i := range want {
		if diagnostics[i] != want[i] {
			t.Fatalf("diagnostic %d = %+v, want %+v; all=%+v", i, diagnostics[i], want[i], diagnostics)
		}
	}
	if severityRank("unknown") != 3 {
		t.Fatalf("unknown severity rank = %d, want 3", severityRank("unknown"))
	}
}
