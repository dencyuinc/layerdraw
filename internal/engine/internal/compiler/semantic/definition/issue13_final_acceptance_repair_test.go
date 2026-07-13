// SPDX-License-Identifier: LicenseRef-LayerDraw-1.0

package definition

import (
	"reflect"
	"strings"
	"testing"

	"github.com/dencyuinc/layerdraw/internal/engine/internal/compiler/resolve"
)

func TestFinalAcceptanceExplicitForbiddenAnnotationsAndPositiveControls(t *testing.T) {
	t.Parallel()

	source := `project p "Project" {
  annotations {
    "database.password": "hunter2",
    "service_account.private_key": "not-shown",
    state_version: "42",
    credential_note: "Authorization details",
    generated_view_data: "generated payload",
    executable_note: "runtime hook",
    source_note: "package main\nfunc main() {}",
    authorization_note: "Authorization: Bearer abc.def",
    key_envelope: "-----BEGIN OPENSSH PRIVATE KEY-----",
    owner: "platform",
    external_id: "project-1",
    password_policy: "rotate quarterly",
    design_note: "basic design",
    authentication_note: "Bearer authentication",
  }
}
`
	got := compileProject(t, map[string]string{"document.ldl": source})
	forbidden := diagnosticsByCode(got.Diagnostics, "LDL1901")
	if len(forbidden) != 9 {
		t.Fatalf("forbidden diagnostics = %+v, want 9", forbidden)
	}
	want := map[string]string{
		"owner": "platform", "external_id": "project-1", "password_policy": "rotate quarterly",
		"design_note": "basic design", "authentication_note": "Bearer authentication",
	}
	if !reflect.DeepEqual(got.Project.Annotations, want) {
		t.Fatalf("published annotations = %+v, want %+v", got.Project.Annotations, want)
	}
	for _, key := range []string{"tenant.database.password", "namespace.service_account.private_key", "state_version", "tenant.state_version", "credential_note", "generated_view_data", "executable_note"} {
		if !forbiddenAnnotationKey(key) {
			t.Errorf("forbidden key %q was accepted", key)
		}
	}
	for _, key := range []string{"owner", "external_id", "password_policy", "database.password_policy"} {
		if forbiddenAnnotationKey(key) {
			t.Errorf("benign key %q was rejected", key)
		}
	}
	for _, value := range []string{"-----BEGIN PRIVATE KEY-----", "-----BEGIN EC PRIVATE KEY-----", "-----BEGIN OPENSSH PRIVATE KEY-----", "Authorization: Bearer abc.def", "package main\nfunc main() {}"} {
		if !forbiddenAnnotationValue(value) {
			t.Errorf("forbidden value %q was accepted", value)
		}
	}
	for _, value := range []string{"basic design", "Bearer authentication", "package delivery notes"} {
		if forbiddenAnnotationValue(value) {
			t.Errorf("benign value %q was rejected", value)
		}
	}
}

func TestFinalAcceptanceProjectionAndRenderPrimitivesAreTransactional(t *testing.T) {
	t.Parallel()

	t.Run("unknown nested members retain fallback", func(t *testing.T) {
		source := relationFixture(`
  projection table {
    row_mode relation
    unexpected true
  }
  render edge {
    arrow backward
    unexpected true
  }
  render nested {
    frame_style strong
    unexpected true
  }
  render overlay {
    kind shield
    unexpected true
  }
  render badge {
    icon "shield"
    unexpected true
  }`)
		got := compileProject(t, map[string]string{"document.ldl": source})
		relation := got.RelationTypes[0]
		if relation.Projections.Table != defaultProjections().Table || !reflect.DeepEqual(relation.Render, defaultRender()) {
			t.Fatalf("invalid primitive siblings entered typed state: table=%+v render=%+v", relation.Projections.Table, relation.Render)
		}
		if count := len(diagnosticsByMessage(got.Diagnostics, "unknown or invalid schema member")); count != 5 {
			t.Fatalf("unknown-member diagnostics = %+v, want 5", got.Diagnostics)
		}
	})

	t.Run("duplicate keyed primitives retain fallback", func(t *testing.T) {
		source := relationFixture(`
  projection diagram {
    mode hide
  }
  projection diagram {
    mode edge
  }
  render badge {
    icon "first"
  }
  render badge {
    label type
  }`)
		got := compileProject(t, map[string]string{"document.ldl": source})
		relation := got.RelationTypes[0]
		if relation.Projections.Diagram != defaultProjections().Diagram || !reflect.DeepEqual(relation.Render.Badge, defaultRender().Badge) {
			t.Fatalf("duplicate primitive selected a candidate: diagram=%+v badge=%+v", relation.Projections.Diagram, relation.Render.Badge)
		}
		if count := len(diagnosticsByMessage(got.Diagnostics, "duplicate primitive")); count != 2 {
			t.Fatalf("duplicate primitive diagnostics = %+v, want 2", got.Diagnostics)
		}
	})
}

func TestFinalAcceptanceDuplicateMembersPreserveFirstTypedValue(t *testing.T) {
	t.Parallel()

	source := `project p "Project" {}
entity_type a "A" {
  representation shape rect
}
entity_type b "B" {
  representation shape rect
}
entity_type values "Values" {
  representation shape rect
  columns {
    value "Value" string default "first" default "second"
  }
}
relation_type rel "Rel" dependency {
  from source types [a] types [b]
  to target types [a]
  cardinality {
    to_per_from 0..1
    to_per_from 1..1
  }
  label "relates"
}
`
	got := compileProject(t, map[string]string{"document.ldl": source})
	column := got.EntityTypes[2].Columns[0]
	if column.Default == nil || column.Default.String != "first" {
		t.Fatalf("duplicate default overwrote first value: %+v", column)
	}
	relation := got.RelationTypes[0]
	if !reflect.DeepEqual(relation.From.EntityTypeAddresses, []string{"ldl:project:p:entity-type:a"}) {
		t.Fatalf("duplicate endpoint selector merged values: %+v", relation.From)
	}
	if relation.Cardinality.ToPerFrom != (CardinalityBound{Min: 0, Max: CardinalityMaximumOne}) {
		t.Fatalf("duplicate cardinality overwrote first value: %+v", relation.Cardinality)
	}
	for message, count := range map[string]int{"duplicate column modifier": 1, "duplicate endpoint selector": 1, "duplicate schema member": 1} {
		if actual := len(diagnosticsByMessage(got.Diagnostics, message)); actual != count {
			t.Fatalf("diagnostic %q count = %d, want %d; all=%+v", message, actual, count, got.Diagnostics)
		}
	}
}

func TestFinalAcceptanceContextDefaultIsValidatedOnce(t *testing.T) {
	t.Parallel()

	source := relationFixture(`
  reverse "{not_allowed}"
  projection context {
    include_attribute_rows true
  }`)
	got := compileProject(t, map[string]string{"document.ldl": source})
	diagnostics := diagnosticsByMessage(got.Diagnostics, "unknown context placeholder")
	if len(diagnostics) != 1 {
		t.Fatalf("context diagnostics = %+v, want exactly one", diagnostics)
	}
	start := strings.Index(source, `"{not_allowed}"`)
	if diagnostics[0].Range == nil || diagnostics[0].Range.StartByte != start || diagnostics[0].Range.EndByte != start+len(`"{not_allowed}"`) {
		t.Fatalf("context range = %+v", diagnostics[0].Range)
	}
}

func TestFinalAcceptanceLayerTieUsesStructuredStableSymbolOrder(t *testing.T) {
	t.Parallel()

	projectSymbol := resolve.StableSymbol{Origin: resolve.Origin{Kind: resolve.OriginProject, ProjectID: "z"}, Path: []resolve.SymbolSegment{{Kind: resolve.KindLayer, ID: "project_layer"}}}
	packSymbol := resolve.StableSymbol{Origin: resolve.Origin{Kind: resolve.OriginPack, Publisher: "acme", PackName: "shared"}, Path: []resolve.SymbolSegment{{Kind: resolve.KindLayer, ID: "pack_layer"}}}
	layers := LayersByDisplayOrder([]Layer{
		{ID: "pack", Address: "ldl:pack:acme:shared:layer:pack_layer", Order: 0, symbol: packSymbol},
		{ID: "project", Address: "ldl:project:z:layer:project_layer", Order: 0, symbol: projectSymbol},
	})
	if layers[0].ID != "project" || layers[1].ID != "pack" {
		t.Fatalf("layer tie order = %+v", layers)
	}

	compiled := compileProject(t, map[string]string{"document.ldl": `project p "P" {}
layers {
  z_layer "Z" @0
  a_layer "A" @0
}
`})
	for _, layer := range compiled.Layers {
		if layer.symbol.Origin.Kind == "" {
			t.Fatalf("compiled layer lost structured symbol: %+v", layer)
		}
	}
}

func TestFinalAcceptanceEmailDoesNotApplySMTPMailboxLimits(t *testing.T) {
	t.Parallel()

	for _, localLength := range []int{65, 245} {
		email := strings.Repeat("a", localLength) + "@example.com"
		if normalized, ok := normalizeStringFormat("email", email); !ok || normalized != email {
			t.Fatalf("email length %d = %q,%v", localLength, normalized, ok)
		}
	}
}
