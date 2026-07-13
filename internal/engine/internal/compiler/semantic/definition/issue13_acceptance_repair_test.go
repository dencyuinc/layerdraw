// SPDX-License-Identifier: LicenseRef-LayerDraw-1.0

package definition

import (
	"strings"
	"testing"

	"github.com/dencyuinc/layerdraw/internal/engine/internal/compiler/syntax"
)

func TestAcceptanceForbiddenAnnotationsAreRejectedAtCommonBoundary(t *testing.T) {
	t.Parallel()
	source := `project p "Project" {
  annotations { owner: "platform", external_id: "project-1", created_at: "2026-01-01" }
}
layers {
  app "App" @0 {
    annotations { owner: "platform", external_id: "layer-1", backend_binding: "prod" }
  }
}
entity_type endpoint "Endpoint" {
  representation shape rect
  annotations { owner: "platform", external_id: "type-1", credential: "secret" }
}
relation_type link "Link" dependency {
  from source types [endpoint]
  to target types [endpoint]
  label "links"
  annotations { owner: "platform", external_id: "relation-1", handler: "javascript:alert(1)" }
}
`
	got := compileProject(t, map[string]string{"document.ldl": source})
	forbidden := diagnosticsByCode(got.Diagnostics, "LDL1901")
	if len(forbidden) != 4 {
		t.Fatalf("forbidden diagnostics = %+v, want 4", forbidden)
	}
	wantTokens := []string{"created_at", "backend_binding", "credential", `"javascript:alert(1)"`}
	for i, diagnostic := range forbidden {
		if diagnostic.MessageKey != "forbidden_state_credential_or_executable_content" || diagnostic.Range == nil {
			t.Fatalf("forbidden diagnostic = %+v", diagnostic)
		}
		start := strings.Index(source, wantTokens[i])
		if diagnostic.Range.StartByte != start || diagnostic.Range.EndByte != start+len(wantTokens[i]) {
			t.Fatalf("range for %q = %+v, want [%d,%d)", wantTokens[i], diagnostic.Range, start, start+len(wantTokens[i]))
		}
	}
	commons := []Common{got.Project.Common, got.Layers[0].Common, got.EntityTypes[0].Common, got.RelationTypes[0].Common}
	for _, common := range commons {
		if common.Annotations["owner"] != "platform" || common.Annotations["external_id"] == "" || len(common.Annotations) != 2 {
			t.Fatalf("committed annotations = %+v", common.Annotations)
		}
	}
}

func TestAcceptanceAuthoredAssetValidationRunsBeforeNormalization(t *testing.T) {
	t.Parallel()
	valid := compileProject(t, map[string]string{
		"document.ldl": `import { endpoint } from "./types/entity.ldl"
project p "Project" {}
export { endpoint }
`,
		"types/entity.ldl": `entity_type endpoint "Endpoint" {
  image "../assets/./image.png"
  representation shape rect
}
export { endpoint }
`,
	})
	if valid.HasErrors || valid.EntityTypes[0].Image == nil || valid.EntityTypes[0].Image.Locator != "assets/image.png" {
		t.Fatalf("valid asset = %+v diagnostics=%+v", valid.EntityTypes, valid.Diagnostics)
	}
	for _, raw := range []string{"assets//image.png", "assets/image.png/", "assets/e\u0301.png", "assets/a\u0085b.png"} {
		t.Run(raw, func(t *testing.T) {
			source := "project p \"Project\" {}\nentity_type endpoint \"Endpoint\" {\n  image \"" + raw + "\"\n  representation shape rect\n}\n"
			got := compileProject(t, map[string]string{"document.ldl": source})
			diagnostics := diagnosticsByCode(got.Diagnostics, "LDL1201")
			if len(diagnostics) != 1 || got.EntityTypes[0].Image != nil {
				t.Fatalf("asset %q result=%+v diagnostics=%+v", raw, got.EntityTypes[0].Image, got.Diagnostics)
			}
			start := strings.Index(source, `"`+raw+`"`)
			if diagnostics[0].Range.StartByte != start || diagnostics[0].Range.EndByte != start+len(raw)+2 {
				t.Fatalf("asset range = %+v, want [%d,%d)", diagnostics[0].Range, start, start+len(raw)+2)
			}
		})
	}
}

func TestAcceptanceDiagramInvalidEndpointDoesNotCascade(t *testing.T) {
	t.Parallel()
	for _, projection := range []string{
		"source_endpoint sideways\n    target_endpoint from",
		"source_endpoint to\n    target_endpoint sideways",
	} {
		t.Run(strings.ReplaceAll(projection, "\n", "_"), func(t *testing.T) {
			source := relationFixture(`
  projection diagram {
    ` + projection + `
  }`)
			got := compileProject(t, map[string]string{"document.ldl": source})
			diagnostics := diagnosticsByCode(got.Diagnostics, "LDL1504")
			if len(diagnostics) != 1 || diagnostics[0].Message != "invalid enum" {
				t.Fatalf("diagram diagnostics = %+v, want one invalid enum", diagnostics)
			}
			start := strings.Index(source, "sideways")
			if diagnostics[0].Range.StartByte != start || diagnostics[0].Range.EndByte != start+len("sideways") {
				t.Fatalf("enum range = %+v, want [%d,%d)", diagnostics[0].Range, start, start+len("sideways"))
			}
		})
	}
}

func TestAcceptanceInvalidTypedCandidatesAreNotPublished(t *testing.T) {
	t.Parallel()
	source := `project p "Project" {}
entity_type endpoint "Endpoint" {
  representation shape rect
  columns {
    unknown "Unknown" mystery
    state "State" enum [active, 1] default active
    uri "URI" string default "not a uri" format uri
    ranged "Ranged" integer default 5 min 6
    sized "Sized" string default "abc" max_length 2
  }
}
relation_type link "Link" sideways {
  from source types [endpoint]
  to target types [endpoint]
  label "links"
  projection matrix {
    row_endpoint sideways
    column_endpoint to
    include_relation_rows true
  }
  projection tree {
    parent_endpoint from
  }
  projection flow {
    source_endpoint from
    target_endpoint to
    connector_kind sideways
  }
}
`
	got := compileProject(t, map[string]string{"document.ldl": source})
	entity := got.EntityTypes[0]
	if entity.Columns[0].ValueType != "" {
		t.Fatalf("invalid value type published: %+v", entity.Columns[0])
	}
	if entity.Columns[1].EnumValues != nil || entity.Columns[1].Default != nil {
		t.Fatalf("partial enum state published: %+v", entity.Columns[1])
	}
	for i := 2; i <= 4; i++ {
		if entity.Columns[i].Default != nil {
			t.Fatalf("invalid default published: %+v", entity.Columns[i])
		}
	}
	relation := got.RelationTypes[0]
	if relation.SemanticKind != "" || relation.Projections.Matrix != nil || relation.Projections.Tree != nil || relation.Projections.Flow != nil {
		t.Fatalf("invalid relation state published: %+v", relation)
	}
	wantMessages := map[string]int{
		"invalid scalar type":     1,
		"invalid enum value":      1,
		"default format mismatch": 1,
		"default range mismatch":  1,
		"default length mismatch": 1,
		"invalid semantic kind":   1,
		"invalid enum":            2,
		"missing endpoint":        1,
	}
	for message, count := range wantMessages {
		if actual := len(diagnosticsByMessage(got.Diagnostics, message)); actual != count {
			t.Fatalf("diagnostic %q count = %d, want %d; all=%+v", message, actual, count, got.Diagnostics)
		}
	}
}

func TestAcceptanceScalarDiagnosticsUseOperandRanges(t *testing.T) {
	t.Parallel()
	source := `project p "Project" {}
entity_type endpoint "Endpoint" {
  representation shape rect
  columns {
    bad_format "Bad Format" string format unknown
    bad_default "Bad Default" string default "relative" format uri
  }
}
relation_type link "Link" dependency {
  allow_self maybe
  duplicate_policy sideways
  from source types [endpoint]
  to target types [endpoint]
  label "links"
  projection composed {
    priority nope
  }
}
`
	got := compileProject(t, map[string]string{"document.ldl": source})
	cases := []struct {
		message string
		token   string
	}{
		{"invalid format", "unknown"},
		{"default format mismatch", `"relative"`},
		{"expected boolean", "maybe"},
		{"invalid enum", "sideways"},
		{"expected integer", "nope"},
	}
	for _, tt := range cases {
		diagnostics := diagnosticsByMessage(got.Diagnostics, tt.message)
		if len(diagnostics) != 1 || diagnostics[0].Range == nil {
			t.Fatalf("diagnostic %q = %+v", tt.message, diagnostics)
		}
		start := strings.Index(source, tt.token)
		if diagnostics[0].Range.StartByte != start || diagnostics[0].Range.EndByte != start+len(tt.token) {
			t.Fatalf("range for %q = %+v, want [%d,%d)", tt.message, diagnostics[0].Range, start, start+len(tt.token))
		}
	}
}

func TestAcceptanceCardinalityUsesStableDiagnostic(t *testing.T) {
	t.Parallel()
	source := relationFixture(`
  cardinality {
    to_per_from 2..*
  }`)
	got := compileProject(t, map[string]string{"document.ldl": source})
	diagnostics := diagnosticsByMessage(got.Diagnostics, "invalid cardinality")
	if len(diagnostics) != 1 {
		t.Fatalf("cardinality diagnostics = %+v", got.Diagnostics)
	}
	diagnostic := diagnostics[0]
	start := strings.Index(source, "2..*")
	if diagnostic.Code != "LDL1503" || diagnostic.MessageKey != "relation_cardinality_violation" || diagnostic.Range == nil || diagnostic.Range.StartByte != start || diagnostic.Range.EndByte != start+1 {
		t.Fatalf("cardinality diagnostic = %+v, token start=%d", diagnostic, start)
	}
}

func TestForbiddenAnnotationRuleAllowsOwnerAndExternalID(t *testing.T) {
	t.Parallel()
	if forbiddenAnnotationKey("owner") || forbiddenAnnotationKey("external_id") || forbiddenAnnotationValue("platform-team") || forbiddenAnnotationValue("svc-42") || forbiddenAnnotationValue("basic design") {
		t.Fatal("valid annotations were classified as forbidden")
	}
	for _, key := range []string{"updated_at", "createdAt", "client-secret", "apiKey", "backend.binding", "backendBinding", "shell_command", "binary_payload"} {
		if !forbiddenAnnotationKey(key) {
			t.Errorf("forbidden key %q was accepted", key)
		}
	}
	for _, value := range []string{"javascript:alert(1)", "Bearer abc.def_123", "Basic dXNlcjpwYXNz", "os.Getenv(\"TOKEN\")", "${API_KEY}"} {
		if !forbiddenAnnotationValue(value) {
			t.Errorf("forbidden value %q was accepted", value)
		}
	}
}

func TestAcceptanceTransactionalHelperBranches(t *testing.T) {
	t.Parallel()
	src := testSource()
	c := &compiler{}
	operandSpan := syntax.Span{Start: 2, End: 7}
	invalidAsset := body{items: []item{{name: "image", args: []value{{raw: "asset", kind: syntax.TokenIdentifier, span: operandSpan}}, span: src.Range}}}
	if got := c.optionalAsset(invalidAsset, "image", src, "subject"); got != nil {
		t.Fatalf("non-string asset published: %+v", got)
	}
	if len(c.diagnostics) != 1 || c.diagnostics[0].Message != "expected asset path string" || c.diagnostics[0].Range == nil || c.diagnostics[0].Range.StartByte != operandSpan.Start || c.diagnostics[0].Range.EndByte != operandSpan.End {
		t.Fatalf("asset diagnostics = %+v", c.diagnostics)
	}
	if _, ok := (value{kind: syntax.TokenIdentifier}).authoredString(); ok {
		t.Fatal("non-string authored value accepted")
	}
	if _, ok := (value{kind: syntax.TokenString, raw: `"\x"`}).authoredString(); ok {
		t.Fatal("malformed quoted authored value accepted")
	}
	if got := invalidOperandSpan(nil); !got.Empty() {
		t.Fatalf("nil operand span = %+v", got)
	}
	if got := invalidOperandSpan(&item{args: []value{{span: src.Range}, {span: src.Range}}}); got != src.Range {
		t.Fatalf("extra operand span = %+v", got)
	}
	if got := itemValueSpan(nil); !got.Empty() {
		t.Fatalf("nil item value span = %+v", got)
	}
	if got := itemValueSpan(&item{span: src.Range}); got != src.Range {
		t.Fatalf("operand-less item span = %+v", got)
	}

	result := compileProject(t, map[string]string{"document.ldl": `project p "Project" {}
entity_type e "E" {
  representation shape rect
  columns {
    invalid "Invalid" mystery
  }
  unique invalid_constraint [invalid]
}
`})
	if len(result.EntityTypes[0].UniqueConstraints) != 0 || len(diagnosticsByMessage(result.Diagnostics, "unknown column")) != 0 || len(diagnosticsByMessage(result.Diagnostics, "empty unique")) != 0 {
		t.Fatalf("invalid-column derivative state/diagnostics = constraints=%+v diagnostics=%+v", result.EntityTypes[0].UniqueConstraints, result.Diagnostics)
	}
}
