// SPDX-License-Identifier: LicenseRef-LayerDraw-1.0

package syntax

// Diagnostic is the stable syntax diagnostic identity used by this package.
type Diagnostic struct {
	Code       string
	Severity   string
	MessageKey string
	Message    string
	Span       Span
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
