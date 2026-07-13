// SPDX-License-Identifier: LicenseRef-LayerDraw-1.0

package definition

import (
	"reflect"
	"strings"
	"testing"

	"github.com/dencyuinc/layerdraw/internal/engine/internal/compiler/resolve"
	"github.com/dencyuinc/layerdraw/internal/engine/internal/compiler/syntax"
)

func TestCommonFieldClosedSchema(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		body    string
		message string
	}{
		{name: "empty annotations", body: `annotations {}`},
		{name: "tags must be list", body: `tags invalid`, message: "tags requires one list"},
		{name: "tag type", body: `tags [1]`, message: "invalid tag"},
		{name: "empty tag", body: `tags [""]`, message: "invalid tag"},
		{name: "annotation must be object", body: `annotations "bad"`, message: "annotations requires an object"},
		{name: "annotation value type", body: `annotations { owner: true }`, message: "invalid annotation"},
		{name: "noncanonical annotation block", body: "annotations {\n    owner \"team\"\n  }", message: "annotations requires an object"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := compileProject(t, map[string]string{"document.ldl": "project p \"Project\" {\n  " + tt.body + "\n}\n"})
			if tt.message == "" {
				if got.HasErrors || got.Project == nil || len(got.Project.Annotations) != 0 {
					t.Fatalf("result = %+v diagnostics = %+v", got.Project, got.Diagnostics)
				}
				return
			}
			if !hasDiagnosticMessage(got.Diagnostics, tt.message) {
				t.Fatalf("Diagnostics = %+v, want message %q", got.Diagnostics, tt.message)
			}
		})
	}

	duplicate := compileProject(t, map[string]string{"document.ldl": `project p "Project" {
  annotations { "e\u0301": "one", "é": "two" }
}
`})
	if !hasDiagnosticMessage(duplicate.Diagnostics, "duplicate annotation") {
		t.Fatalf("Diagnostics = %+v", duplicate.Diagnostics)
	}
}

func TestEntityClosedSchemaAndReservations(t *testing.T) {
	t.Parallel()

	valid := compileProject(t, map[string]string{"document.ldl": `project p "Project" {}
entity_type container "Container" {
  representation container
  reserve {}
}
entity_type table "Table" {
  representation table
  reserve {
    columns []
    constraints []
  }
}
`})
	if valid.HasErrors || valid.EntityTypes[0].Representation.Kind != "container" || valid.EntityTypes[1].Representation.Kind != "table" {
		t.Fatalf("entities = %+v diagnostics = %+v", valid.EntityTypes, valid.Diagnostics)
	}

	invalid := compileProject(t, map[string]string{"document.ldl": `project p "Project" {}
entity_type invalid "Invalid" {
  representation container extra
  reserve {
    columns invalid
    unknown []
  }
}
`})
	for _, message := range []string{"invalid representation", "reservation requires one list", "unknown or invalid schema member"} {
		if !hasDiagnosticMessage(invalid.Diagnostics, message) {
			t.Fatalf("Diagnostics = %+v, want %q", invalid.Diagnostics, message)
		}
	}
}

func TestEndpointClosedSchema(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		from    string
		message string
	}{
		{name: "missing role", from: `from`, message: "missing endpoint"},
		{name: "selector order", from: `from source layers [app] types [endpoint]`, message: "endpoint selectors out of order"},
		{name: "duplicate selector", from: `from source types [endpoint] types [endpoint]`, message: "duplicate endpoint selector"},
		{name: "selector needs list", from: `from source types "invalid"`, message: "endpoint selector requires a list"},
		{name: "reference type", from: `from source types ["endpoint"]`, message: "invalid endpoint reference"},
		{name: "duplicate reference", from: `from source types [endpoint, endpoint]`, message: "duplicate endpoint reference"},
		{name: "invalid selector", from: `from source invalid [endpoint]`, message: "invalid endpoint selector"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			source := `project p "Project" {}
layers {
  app "Application" @0
}
entity_type endpoint "Endpoint" {
  representation shape rect
}
relation_type relation "Relation" dependency {
  ` + tt.from + `
  to target types [endpoint]
  label "relates"
}
`
			got := compileProject(t, map[string]string{"document.ldl": source})
			if !hasDiagnosticMessage(got.Diagnostics, tt.message) {
				t.Fatalf("Diagnostics = %+v, want %q", got.Diagnostics, tt.message)
			}
		})
	}
}

func TestProjectionRequiredAndModeSpecificFields(t *testing.T) {
	t.Parallel()

	valid := compileProject(t, map[string]string{"document.ldl": relationFixture(`
  projection table {
    row_mode automatic
    include_from false
    include_to true
    include_relation_type false
  }
  render overlay {
    kind "shield"
    position center
    max_items 1
  }
`)})
	if valid.HasErrors || valid.RelationTypes[0].Projections.Table.IncludeFrom || valid.RelationTypes[0].Render.Overlay.Kind != "shield" {
		t.Fatalf("relation = %+v diagnostics = %+v", valid.RelationTypes, valid.Diagnostics)
	}

	tests := []struct {
		name     string
		fragment string
		message  string
	}{
		{name: "flow connector required", fragment: `projection flow {
    source_endpoint from
    target_endpoint to
  }`, message: "missing connector kind"},
		{name: "branch reference shape", fragment: `projection flow {
    source_endpoint from
    target_endpoint to
    connector_kind data
    branch_value_column "column"
  }`, message: "invalid branch column reference"},
		{name: "overlay kind type", fragment: `render overlay {
    kind 1
  }`, message: "expected atom"},
		{name: "nest forbids overlay endpoint", fragment: `projection composed {
    mode nest
    parent_endpoint from
    child_endpoint to
    overlay_endpoint from
  }`, message: "endpoint fields forbidden for nest"},
		{name: "badge forbids overlay endpoint", fragment: `projection composed {
    mode badge
    badge_endpoint from
    target_endpoint to
    overlay_endpoint from
  }`, message: "endpoint fields forbidden for badge"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := compileProject(t, map[string]string{"document.ldl": relationFixture("\n  " + tt.fragment + "\n")})
			if !hasDiagnosticMessage(got.Diagnostics, tt.message) {
				t.Fatalf("Diagnostics = %+v, want %q", got.Diagnostics, tt.message)
			}
		})
	}
}

func TestPureSemanticDecoders(t *testing.T) {
	t.Parallel()

	formats := []struct {
		format string
		value  string
		want   string
		ok     bool
	}{
		{format: "uri", value: "http://[", ok: false},
		{format: "email", value: "User@EXAMPLE.COM", want: "User@EXAMPLE.COM", ok: true},
		{format: "email", value: "bad..dots@example.com", ok: false},
		{format: "email", value: strings.Repeat("a", 65) + "@example.com", ok: false},
		{format: "email", value: strings.Repeat("a", 245) + "@example.com", ok: false},
		{format: "hostname", value: "bad-.example", ok: false},
		{format: "hostname", value: "a..example", ok: false},
		{format: "hostname", value: strings.Repeat("a", 254), ok: false},
		{format: "ipv6", value: "fe80::1%eth0", ok: false},
		{format: "unknown", value: "value", ok: false},
	}
	for _, tt := range formats {
		got, ok := normalizeStringFormat(tt.format, tt.value)
		if ok != tt.ok || got != tt.want {
			t.Fatalf("normalizeStringFormat(%q, %q) = %q, %v; want %q, %v", tt.format, tt.value, got, ok, tt.want, tt.ok)
		}
	}

	if got := heredocText("<<-TEXT\r\n\t  first\r\n\t  second\r\nTEXT"); got != "first\nsecond\n" {
		t.Fatalf("heredoc = %q", got)
	}
	if got := heredocText("not-a-heredoc"); got != "not-a-heredoc" {
		t.Fatalf("short heredoc = %q", got)
	}
	if got := (&compiler{decls: map[string]resolve.DeclarationSymbol{}}).canonicalAddressSet([]string{"b", "a", "a"}); !reflect.DeepEqual(got, []string{"a", "b"}) {
		t.Fatalf("canonical set = %+v", got)
	}
	if _, ok := resolveAssetLocator("types/entity.ldl", "assets/\nimage.png"); ok {
		t.Fatal("asset locator with a control character was accepted")
	}
	layers := LayersByDisplayOrder([]Layer{{ID: "late", Address: "z", Order: 2}, {ID: "early", Address: "b", Order: 1}, {ID: "tie", Address: "a", Order: 1}})
	if !reflect.DeepEqual([]string{layers[0].ID, layers[1].ID, layers[2].ID}, []string{"tie", "early", "late"}) {
		t.Fatalf("layers = %+v", layers)
	}
}

func TestMalformedCSTHelpersRemainTotal(t *testing.T) {
	t.Parallel()

	if got := readBlock(&syntax.Node{Kind: syntax.NodeBlock, Children: []syntax.Element{
		&syntax.Node{Kind: syntax.NodeStatement},
		&syntax.Node{Kind: syntax.NodeNestedBlock},
	}}); len(got.items) != 0 {
		t.Fatalf("malformed block = %+v", got)
	}
	if directTokens(nil) != nil || listValues(nil) != nil || objectValues(nil) != nil {
		t.Fatal("nil CST helpers returned values")
	}
	if got := (value{raw: `"\x"`, kind: syntax.TokenString}).string(); got != `"\x"` {
		t.Fatalf("malformed string fallback = %q", got)
	}
}

func TestLayerOrderRangeDiagnostic(t *testing.T) {
	t.Parallel()

	got := compileProject(t, map[string]string{"document.ldl": `project p "Project" {}
layers {
  overflow "Overflow" @999999999999999999999999999
}
`})
	if !hasDiagnosticMessage(got.Diagnostics, "layer order out of range") {
		t.Fatalf("Diagnostics = %+v", got.Diagnostics)
	}
}

func hasDiagnosticMessage(diagnostics []resolve.Diagnostic, message string) bool {
	return slicesContainFunc(diagnostics, func(d resolve.Diagnostic) bool { return d.Message == message })
}

func slicesContainFunc[T any](values []T, predicate func(T) bool) bool {
	for _, value := range values {
		if predicate(value) {
			return true
		}
	}
	return false
}

func TestDiagnosticOrderingUsesNumericByteOffsets(t *testing.T) {
	t.Parallel()

	diagnostics := []resolve.Diagnostic{
		{Code: "LDL1102", MessageKey: "unknown_or_duplicate_schema_member", Range: &resolve.SourceRange{ModulePath: "document.ldl", StartByte: 10, EndByte: 11}},
		{Code: "LDL1102", MessageKey: "unknown_or_duplicate_schema_member", Range: &resolve.SourceRange{ModulePath: "document.ldl", StartByte: 2, EndByte: 3}},
	}
	resolve.SortDiagnostics(diagnostics)
	if diagnostics[0].Range.StartByte != 2 {
		t.Fatalf("diagnostics = %+v", diagnostics)
	}
	if strings.TrimSpace(diagnostics[0].MessageKey) == "" {
		t.Fatal("stable message key missing")
	}
}
