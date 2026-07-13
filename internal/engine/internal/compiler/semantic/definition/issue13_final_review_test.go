// SPDX-License-Identifier: LicenseRef-LayerDraw-1.0

package definition

import (
	"fmt"
	"strings"
	"testing"

	"github.com/dencyuinc/layerdraw/internal/engine/internal/compiler/resolve"
)

func TestFinalReviewReserveMembersAreCanonicalIdentifiers(t *testing.T) {
	t.Parallel()

	got := compileProject(t, map[string]string{"document.ldl": `project p "Project" {}
entity_type e "E" {
  representation shape rect
  reserve {
    columns ["quoted", 1, namespace.qualified, valid_column]
    constraints ["quoted_constraint", 2, namespace.qualified_constraint, valid_constraint]
  }
}
`})
	diagnostics := diagnosticsByMessage(got.Diagnostics, "invalid reservation identifier")
	if len(diagnostics) != 6 {
		t.Fatalf("reservation diagnostics = %+v, want exactly 6", diagnostics)
	}
	entity := got.EntityTypes[0]
	if len(entity.ReservedColumnIDs) != 1 || entity.ReservedColumnIDs[0] != "valid_column" || len(entity.ReservedConstraintIDs) != 1 || entity.ReservedConstraintIDs[0] != "valid_constraint" {
		t.Fatalf("effective reservations = columns=%+v constraints=%+v", entity.ReservedColumnIDs, entity.ReservedConstraintIDs)
	}
}

func TestFinalReviewInvalidComposedValuesDoNotCascade(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name       string
		projection string
	}{
		{name: "invalid mode", projection: `projection composed {
    mode sideways
    parent_endpoint from
  }`},
		{name: "invalid nest endpoint", projection: `projection composed {
    mode nest
    parent_endpoint sideways
    child_endpoint to
  }`},
		{name: "invalid overlay endpoint", projection: `projection composed {
    mode overlay
    overlay_endpoint from
    target_endpoint sideways
  }`},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := compileProject(t, map[string]string{"document.ldl": relationFixture("\n  " + tt.projection)})
			diagnostics := diagnosticsByCode(got.Diagnostics, "LDL1504")
			if len(diagnostics) != 1 || diagnostics[0].Message != "invalid enum" {
				t.Fatalf("projection diagnostics = %+v, want one primary invalid enum", diagnostics)
			}
		})
	}
}

func TestFinalReviewInvalidColumnModifiersDoNotEnterTypedState(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name       string
		column     string
		message    string
		assertType func(*testing.T, Column)
	}{
		{
			name:    "format",
			column:  `value "Value" string default "x" format unknown`,
			message: "invalid format",
			assertType: func(t *testing.T, col Column) {
				if col.Format != nil {
					t.Fatalf("invalid format entered typed state: %+v", col)
				}
			},
		},
		{
			name:    "numeric bound",
			column:  `value "Value" integer default -1 min nope`,
			message: "invalid numeric bound",
			assertType: func(t *testing.T, col Column) {
				if col.Min != nil {
					t.Fatalf("invalid minimum entered typed state: %+v", col)
				}
			},
		},
		{
			name:    "length bound",
			column:  `value "Value" string default "abc" max_length -1`,
			message: "invalid length bound",
			assertType: func(t *testing.T, col Column) {
				if col.MaxLength != nil {
					t.Fatalf("invalid maximum length entered typed state: %+v", col)
				}
			},
		},
		{
			name:    "inverted bounds",
			column:  `value "Value" integer default 5 min 10 max 1`,
			message: "invalid bounds",
			assertType: func(t *testing.T, col Column) {
				if col.Min != nil || col.Max != nil {
					t.Fatalf("inverted bounds entered typed state: %+v", col)
				}
			},
		},
		{
			name:    "invalid reserved value",
			column:  `value "Value" enum [active] reserve_values [active, 1] default active`,
			message: "invalid enum value",
			assertType: func(t *testing.T, col Column) {
				if len(col.ReservedEnumValues) != 0 {
					t.Fatalf("invalid reserved values entered typed state: %+v", col)
				}
			},
		},
		{
			name:    "overlapping reserved value",
			column:  `value "Value" enum [active] reserve_values [active] default active`,
			message: "active and reserved enum values overlap",
			assertType: func(t *testing.T, col Column) {
				if len(col.ReservedEnumValues) != 0 {
					t.Fatalf("overlapping reserved values entered typed state: %+v", col)
				}
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := compileProject(t, map[string]string{"document.ldl": fmt.Sprintf(`project p "Project" {}
entity_type e "E" {
  representation shape rect
  columns {
    %s
  }
}
`, tt.column)})
			if len(got.Diagnostics) != 1 || got.Diagnostics[0].Message != tt.message {
				t.Fatalf("diagnostics = %+v, want exactly %q", got.Diagnostics, tt.message)
			}
			tt.assertType(t, got.EntityTypes[0].Columns[0])
		})
	}
}

func TestFinalReviewStrictRFC3986AbsoluteURI(t *testing.T) {
	t.Parallel()

	valid := []string{
		"https://example.com/a%20b?x=%2F#fragment",
		"https://user:pass@reg_name.example:8080/path",
		"http://reg_name.example/path",
		"http://[2001:db8::1]/path",
		"http://[2001:db8::1]:443/path",
		"http://[v1.a:b]/path",
		"mailto:user@example.com",
		"urn:isbn:0451450523",
		"file:///tmp/file",
		"custom:opaque/path",
		"custom:path?first?second",
		"foo:",
	}
	for _, uri := range valid {
		if got, ok := normalizeStringFormat("uri", uri); !ok || got != uri {
			t.Errorf("valid URI %q normalized to %q,%v", uri, got, ok)
		}
	}

	invalid := []string{
		"/relative",
		"https://example.com/a b",
		"http:foo\tbar",
		"http:foo\x00bar",
		"https://example.com/é",
		`https:\\example.com\path`,
		"https://example.com/%",
		"https://example.com/%2",
		"https://example.com/%GG",
		"https://example.com/{bad}",
		"urn:[raw-brackets]",
		"urn:value#[raw-brackets]",
		"urn:value#one#two",
		"1scheme:value",
		"bad_scheme:value",
		"http://one@two@example.com/path",
		"http://us[er@example.com/path",
		"http://example.com]/path",
		"http://example.com:not_a_port/path",
		"http://[2001:db8::1]suffix/path",
		"http://[x]/path",
		"http://[v.foo]/path",
		"http://[vg.foo]/path",
		"http://[v1.]/path",
		"http://[2001:db8::1%25zone]/path",
	}
	for _, uri := range invalid {
		if got, ok := normalizeStringFormat("uri", uri); ok {
			t.Errorf("invalid URI %q accepted as %q", uri, got)
		}
	}
	if validURIScheme("") {
		t.Fatal("empty URI scheme was accepted")
	}
	if got := itemHeaderSpan(nil); !got.Empty() {
		t.Fatalf("nil item header span = %+v", got)
	}
}

func TestFinalReviewAssetLocatorPercentDecodeSafety(t *testing.T) {
	t.Parallel()

	valid := map[string]string{
		"../assets/image.png":        "assets/image.png",
		"assets/file%20name.png":     "types/assets/file%20name.png",
		"assets/version%2E1.png":     "types/assets/version%2E1.png",
		"./assets/../icons/icon.png": "types/icons/icon.png",
	}
	for raw, want := range valid {
		if got, ok := resolve.ResolveAuthoredAssetLocator("types/entity.ldl", raw); !ok || got != want {
			t.Errorf("valid asset locator %q = %q,%v, want %q", raw, got, ok, want)
		}
	}

	invalid := []string{
		"assets/%2e%2e/secret.png",
		"assets/%2E./secret.png",
		"assets/.%2e/secret.png",
		"assets%2f..%2fsecret.png",
		"assets/foo%2Fbar.png",
		"assets/foo%5cbar.png",
		"assets/foo%00bar.png",
		"assets/foo%1fbar.png",
		"assets/foo%7Fbar.png",
		"assets/bad%escape.png",
	}
	for _, raw := range invalid {
		if got, ok := resolve.ResolveAuthoredAssetLocator("types/entity.ldl", raw); ok {
			t.Errorf("unsafe asset locator %q accepted as %q", raw, got)
		}
	}
}

func TestFinalReviewMissingRequiredDiagnosticsUseNearestHeader(t *testing.T) {
	t.Parallel()

	declarations := `project p "Project" {}
entity_type endpoint "Endpoint" {
}
relation_type rel "Rel" dependency {
}
`
	got := compileProject(t, map[string]string{"document.ldl": declarations})
	assertDiagnosticHeader(t, diagnosticsByMessage(got.Diagnostics, "missing representation"), declarations, `entity_type endpoint "Endpoint"`, 1)
	assertDiagnosticHeader(t, diagnosticsByMessage(got.Diagnostics, "missing endpoint"), declarations, `relation_type rel "Rel" dependency`, 2)
	assertDiagnosticHeader(t, diagnosticsByMessage(got.Diagnostics, "missing required string"), declarations, `relation_type rel "Rel" dependency`, 1)

	projections := `project p "Project" {}
entity_type endpoint "Endpoint" {
  representation shape rect
}
relation_type rel "Rel" dependency {
  from source types [endpoint]
  to target types [endpoint]
  label "relates"
  projection tree {}
  projection flow {
    source_endpoint from
    target_endpoint to
  }
}
`
	projectionResult := compileProject(t, map[string]string{"document.ldl": projections})
	assertDiagnosticHeader(t, diagnosticsByMessage(projectionResult.Diagnostics, "missing endpoint"), projections, "projection tree", 2)
	assertDiagnosticHeader(t, diagnosticsByMessage(projectionResult.Diagnostics, "missing connector kind"), projections, "projection flow", 1)
}

func diagnosticsByCode(diagnostics []resolve.Diagnostic, code string) []resolve.Diagnostic {
	var out []resolve.Diagnostic
	for _, diagnostic := range diagnostics {
		if diagnostic.Code == code {
			out = append(out, diagnostic)
		}
	}
	return out
}

func diagnosticsByMessage(diagnostics []resolve.Diagnostic, message string) []resolve.Diagnostic {
	var out []resolve.Diagnostic
	for _, diagnostic := range diagnostics {
		if diagnostic.Message == message {
			out = append(out, diagnostic)
		}
	}
	return out
}

func assertDiagnosticHeader(t *testing.T, diagnostics []resolve.Diagnostic, source, header string, count int) {
	t.Helper()
	if len(diagnostics) != count {
		t.Fatalf("diagnostics for %q = %+v, want %d", header, diagnostics, count)
	}
	start := strings.Index(source, header)
	for _, diagnostic := range diagnostics {
		if diagnostic.Range == nil || diagnostic.Range.StartByte != start || diagnostic.Range.EndByte != start+len(header) {
			t.Fatalf("diagnostic range for %q = %+v, want [%d,%d)", header, diagnostic.Range, start, start+len(header))
		}
	}
}
