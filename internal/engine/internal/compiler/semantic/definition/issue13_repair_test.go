// SPDX-License-Identifier: LicenseRef-LayerDraw-1.0

package definition

import (
	"reflect"
	"strings"
	"testing"

	"github.com/dencyuinc/layerdraw/internal/engine/internal/compiler/resolve"
	"github.com/dencyuinc/layerdraw/internal/engine/internal/compiler/syntax"
)

var (
	_ RepresentationKind     = RepresentationShapeKind
	_ RepresentationShape    = ShapeCylinder
	_ StringFormat           = StringFormatIPv6
	_ RelationSemanticKind   = RelationDataFlow
	_ DuplicatePolicy        = DuplicateDenyAnyBetweenSameEndpoints
	_ CardinalityMaximum     = CardinalityMaximumOne
	_ TraversalDirection     = TraversalBoth
	_ ComposedProjectionMode = ComposedNest
	_ ProjectionConflict     = ProjectionConflictPreferFirst
	_ ProjectionEndpoint     = ProjectionEndpointFrom
	_ DiagramProjectionMode  = DiagramHide
	_ ProjectionLabel        = ProjectionLabelReverseLabel
	_ TableRowMode           = TableRowsRelationRows
	_ FlowConnectorKind      = FlowConnectorMessage
	_ RenderArrow            = RenderArrowBackward
	_ RenderLine             = RenderLineDashed
	_ RenderFrameLabel       = RenderFrameLabelDisplayName
	_ RenderFrameStyle       = RenderFrameStrong
	_ RenderPosition         = RenderPositionBottomLeft
	_ RenderBadgeLabel       = RenderBadgeLabelCount
)

func TestPackRootUsesResolverCanonicalIDAndInstallAlias(t *testing.T) {
	t.Parallel()

	root := issue13Pack("pub/root-pack", "root", `entity_type root_type "Root" {
  representation shape rect
}
export { root_type }
`)
	other := issue13Pack("pub/other-pack", "other", `entity_type other_type "Other" {
  representation shape cloud
}
export { other_type }
`)
	resolved := resolve.Resolve(resolve.Input{
		Mode:       resolve.CompilePack,
		RootPackID: "pub/root-pack",
		EntryPath:  "pack.ldl",
		Packs: resolve.ResolvedDependencies{
			Format:        "layerdraw-resolved",
			FormatVersion: 1,
			Language:      1,
			Installs: map[string]resolve.ResolvedPack{
				"unrelated_alias": other,
				"selected_alias":  root,
			},
		},
	})
	got := Compile(Input{Resolve: resolved})
	if got.HasErrors {
		t.Fatalf("Diagnostics = %+v", got.Diagnostics)
	}
	if got.Pack == nil || got.Pack.Address != "ldl:pack:pub:root-pack" || got.Pack.CanonicalID != "pub/root-pack" || len(got.EntityTypes) != 1 || got.EntityTypes[0].ID != "root_type" {
		t.Fatalf("selected pack result = %+v", got)
	}
}

func TestEndpointAddressSetsUseStructuredStableSymbolOrder(t *testing.T) {
	t.Parallel()

	pack := issue13Pack("pub/schema", "schema", `entity_type external_type "External" {
  representation shape cloud
}
export { external_type }
`)
	resolved := resolve.Resolve(resolve.Input{
		Mode:      resolve.CompileProject,
		EntryPath: "document.ldl",
		Project: resolve.ProjectInput{Files: map[string]resolve.SourceFile{
			"document.ldl": parse(`import { external_type } from "vendor"
project z "Project" {}
layers {
  z_layer "Z" @0
  a_layer "A" @1
}
entity_type local_type "Local" {
  representation shape rect
}
relation_type rel "Rel" dependency {
  from source types [external_type, local_type] layers [z_layer, a_layer]
  to target types [external_type, local_type] layers [z_layer, a_layer]
  label "relates"
}
`),
		}},
		Packs: resolve.ResolvedDependencies{Format: "layerdraw-resolved", FormatVersion: 1, Language: 1, Installs: map[string]resolve.ResolvedPack{"vendor": pack}},
	})
	got := Compile(Input{Resolve: resolved})
	if got.HasErrors {
		t.Fatalf("Diagnostics = %+v", got.Diagnostics)
	}
	from := got.RelationTypes[0].From
	if want := []string{"ldl:project:z:entity-type:local_type", "ldl:pack:pub:schema:entity-type:external_type"}; !reflect.DeepEqual(from.EntityTypeAddresses, want) {
		t.Fatalf("entity type order = %+v, want %+v", from.EntityTypeAddresses, want)
	}
	if want := []string{"ldl:project:z:layer:a_layer", "ldl:project:z:layer:z_layer"}; !reflect.DeepEqual(from.LayerAddresses, want) {
		t.Fatalf("layer order = %+v, want %+v", from.LayerAddresses, want)
	}
}

func TestContextTemplatesValidateFinalEffectiveValues(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name          string
		label         string
		reverse       string
		projection    string
		wantErrors    int
		wantRangeText string
	}{
		{name: "invalid label-derived default", label: `"bad {label.placeholder}"`, wantErrors: 1, wantRangeText: `"bad {label.placeholder}"`},
		{name: "invalid reverse-derived default", label: `"relates"`, reverse: `reverse "bad {reverse.placeholder}"`, wantErrors: 1, wantRangeText: `"bad {reverse.placeholder}"`},
		{name: "valid explicit fact override suppresses invalid derived fact", label: `"bad {unused.placeholder}"`, projection: `projection context {
    fact_template "{from.display_name} valid {to.display_name}"
  }`},
		{name: "valid explicit reverse override suppresses invalid derived reverse", label: `"relates"`, reverse: `reverse "bad {unused.placeholder}"`, projection: `projection context {
    reverse_fact_template "{to.display_name} valid {from.display_name}"
  }`},
		{name: "invalid explicit template is diagnosed once", label: `"relates"`, projection: `projection context {
    fact_template "{bad.one} {bad.two}"
  }`, wantErrors: 1, wantRangeText: `"{bad.one} {bad.two}"`},
		{name: "valid defaults", label: `"relates"`, reverse: `reverse "is related by"`},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			source := `project p "Project" {}
entity_type endpoint "Endpoint" {
  representation shape rect
}
relation_type rel "Rel" dependency {
  from source types [endpoint]
  to target types [endpoint]
  label ` + tt.label + "\n  " + tt.reverse + "\n  " + tt.projection + `
}
`
			got := compileProject(t, map[string]string{"document.ldl": source})
			var contextDiagnostics []resolve.Diagnostic
			for _, diagnostic := range got.Diagnostics {
				if diagnostic.Code == "LDL1504" && (diagnostic.Message == "unknown context placeholder" || diagnostic.Message == "malformed context placeholder") {
					contextDiagnostics = append(contextDiagnostics, diagnostic)
				}
			}
			if len(contextDiagnostics) != tt.wantErrors {
				t.Fatalf("context diagnostics = %+v, want %d", contextDiagnostics, tt.wantErrors)
			}
			if tt.wantRangeText != "" {
				wantStart := strings.Index(source, tt.wantRangeText)
				if contextDiagnostics[0].Range == nil || contextDiagnostics[0].Range.StartByte != wantStart {
					t.Fatalf("diagnostic range = %+v, want start %d for %s", contextDiagnostics[0].Range, wantStart, tt.wantRangeText)
				}
			}
		})
	}
}

func TestClosedDomainTypesPreserveDefaultsAndAuthoredValues(t *testing.T) {
	t.Parallel()

	got := compileProject(t, map[string]string{"document.ldl": `project p "Project" {}
entity_type endpoint "Endpoint" {
  representation shape cylinder
  columns {
    address "Address" string default "2001:db8::1" format ipv6
  }
}
relation_type rel "Rel" data_flow {
  duplicate_policy deny_any_between_same_endpoints
  from source types [endpoint]
  to target types [endpoint]
  cardinality {
    to_per_from 0..1
  }
  label "relates"
  traversal {
    default_direction both
  }
  projection composed {
    mode hide
    conflict prefer_first
  }
  projection diagram {
    mode hide
    edge_label none
  }
  projection table {
    row_mode relation_rows
  }
  projection flow {
    source_endpoint from
    target_endpoint to
    connector_kind message
  }
  render edge {
    arrow backward
    line dashed
    label none
  }
  render nested {
    frame_label display_name
    frame_style strong
  }
  render overlay {
    position center
  }
  render badge {
    label none
    position bottom_left
  }
}
`})
	if got.HasErrors {
		t.Fatalf("Diagnostics = %+v", got.Diagnostics)
	}
	entity := got.EntityTypes[0]
	relation := got.RelationTypes[0]
	if entity.Representation.Kind != RepresentationShapeKind || entity.Representation.Shape != ShapeCylinder || entity.Columns[0].Format == nil || *entity.Columns[0].Format != StringFormatIPv6 {
		t.Fatalf("typed entity fields = %+v", entity)
	}
	if relation.SemanticKind != RelationDataFlow || relation.DuplicatePolicy != DuplicateDenyAnyBetweenSameEndpoints || relation.Cardinality.ToPerFrom.Max != CardinalityMaximumOne || relation.Cardinality.FromPerTo.Max != CardinalityMaximumMany || relation.Traversal.DefaultDirection != TraversalBoth {
		t.Fatalf("typed relation policy = %+v", relation)
	}
	if relation.Projections.Composed.Mode != ComposedHide || relation.Projections.Composed.Conflict != ProjectionConflictPreferFirst || relation.Projections.Diagram.Mode != DiagramHide || relation.Projections.Diagram.EdgeLabel != ProjectionLabelNone || relation.Projections.Table.RowMode != TableRowsRelationRows || relation.Projections.Flow == nil || relation.Projections.Flow.ConnectorKind != FlowConnectorMessage {
		t.Fatalf("typed projections = %+v", relation.Projections)
	}
	if relation.Render.Edge.Arrow != RenderArrowBackward || relation.Render.Edge.Line != RenderLineDashed || relation.Render.Edge.Label != ProjectionLabelNone || relation.Render.Nested.FrameLabel != RenderFrameLabelDisplayName || relation.Render.Nested.FrameStyle != RenderFrameStrong || relation.Render.Overlay.Position != RenderPositionCenter || relation.Render.Badge.Label != RenderBadgeLabelNone || relation.Render.Badge.Position != RenderPositionBottomLeft {
		t.Fatalf("typed render = %+v", relation.Render)
	}
}

func TestInvalidPresentRequiredFieldsDoNotAlsoReportMissing(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name       string
		projection string
	}{
		{name: "endpoint", projection: `projection tree {
    parent_endpoint sideways
    child_endpoint to
  }`},
		{name: "connector", projection: `projection flow {
    source_endpoint from
    target_endpoint to
    connector_kind sideways
  }`},
		{name: "boolean", projection: `projection matrix {
    row_endpoint from
    column_endpoint to
    include_relation_rows sideways
  }`},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := compileProject(t, map[string]string{"document.ldl": relationFixture("\n  " + tt.projection)})
			var projectionDiagnostics []resolve.Diagnostic
			for _, diagnostic := range got.Diagnostics {
				if diagnostic.Code == "LDL1504" {
					projectionDiagnostics = append(projectionDiagnostics, diagnostic)
				}
			}
			if len(projectionDiagnostics) != 1 || projectionDiagnostics[0].Message == "missing endpoint" || projectionDiagnostics[0].Message == "missing connector kind" || projectionDiagnostics[0].Message == "missing required boolean" {
				t.Fatalf("projection diagnostics = %+v", projectionDiagnostics)
			}
		})
	}

	src := testSource()
	c := &compiler{}
	c.requiredString(body{items: []item{{name: "label", args: []value{{raw: "invalid", kind: syntax.TokenIdentifier}}, span: src.Range}}}, "label", src, "subject", "", "LDL1501", "invalid_relation_endpoint_or_self_rule")
	if len(c.diagnostics) != 1 || c.diagnostics[0].Message != "expected string" {
		t.Fatalf("required string diagnostics = %+v", c.diagnostics)
	}
}

func issue13Pack(canonicalID, name, source string) resolve.ResolvedPack {
	return resolve.ResolvedPack{
		CanonicalID: canonicalID,
		Version:     "1.0.0",
		Digest:      "sha256:" + strings.Repeat("a", 64),
		Path:        "pack/" + name,
		Entry:       "pack.ldl",
		Files:       map[string]string{"pack.ldl": "sha256:" + strings.Repeat("b", 64)},
		Manifest:    resolve.PackManifest{Format: "layerdraw-pack", FormatVersion: 1, ID: canonicalID, Name: name, Version: "1.0.0", Language: 1, Entry: "pack.ldl"},
		SourceFiles: map[string]resolve.SourceFile{"pack.ldl": parse(source)},
	}
}
