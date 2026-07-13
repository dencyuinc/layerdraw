// SPDX-License-Identifier: LicenseRef-LayerDraw-1.0

package definition

import (
	"reflect"
	"strings"
	"testing"

	"github.com/dencyuinc/layerdraw/internal/engine/internal/compiler/resolve"
)

func TestAnnotationSafetyMatchesQualifiedConceptsWithoutRejectingBenignTokens(t *testing.T) {
	t.Parallel()

	source := `project p "Project" {
  annotations {
    "database.password.hash": "redacted",
    "database:password": "redacted",
    "service_account.private_key.pem": "redacted",
    "generated.view_data.cache": "redacted",
    "runtime.shell.command.args": "redacted",
    "generated_state.payload": "redacted",
    "javascript_source.example": "redacted",
    "password.value": "redacted",
    "callback.url": "https://example.com/hook",
    "api.token": "redacted",
    key_envelope: "-----BEGIN PGP PRIVATE KEY BLOCK-----\nredacted",
    code_sample: "const add = (...) => 1;",
    lambda_example: "() => doWork()",
    "design.token": "violet",
    password_policy: "rotate quarterly",
    "database.password_policy": "rotate monthly",
    arrow_notation: "A => B",
    authentication_note: "Bearer authentication",
  }
}
`
	got := compileProject(t, map[string]string{"document.ldl": source})
	forbidden := diagnosticsByCode(got.Diagnostics, "LDL1901")
	if len(forbidden) != 13 {
		t.Fatalf("forbidden diagnostics = %+v, want 13", forbidden)
	}
	for _, diagnostic := range forbidden {
		if diagnostic.Range == nil || diagnostic.Range.StartByte < 0 || diagnostic.Range.EndByte > len(source) || diagnostic.Range.StartByte >= diagnostic.Range.EndByte {
			t.Fatalf("invalid forbidden diagnostic range: %+v", diagnostic)
		}
	}
	want := map[string]string{
		"design.token":             "violet",
		"password_policy":          "rotate quarterly",
		"database.password_policy": "rotate monthly",
		"arrow_notation":           "A => B",
		"authentication_note":      "Bearer authentication",
	}
	if !reflect.DeepEqual(got.Project.Annotations, want) {
		t.Fatalf("published annotations = %+v, want %+v", got.Project.Annotations, want)
	}
}

func TestAnnotationSafetyRejectsCommonEmbeddedSourceForms(t *testing.T) {
	t.Parallel()

	source := `project p "Project" {
  annotations {
    key_envelope: "Bag Attributes\n-----BEGIN PRIVATE KEY-----\nredacted",
    handler_example: "const f = function () { return 1; };",
    async_handler: "async x => await x",
    arrow_handler: "x => x",
    named_handler: "function named() { return 1; }",
    implementation_note: "func main() {}",
    module_example: "(module (func))",
    empty_module_example: "(module)",
    class_extends_example: "class Widget extends Base {}",
    class_expression_example: "const Widget = class {};",
    side_effect_import_example: "import \"polyfill\";",
    export_list_example: "export { widget };",
    generic_function_example: "func Generic[T any](value T) {}",
    arrow_notation: "A => B",
    constellation_note: "constellation map",
    functional_note: "functional design",
    class_note: "class ownership",
  }
}
`
	got := compileProject(t, map[string]string{"document.ldl": source})
	forbidden := diagnosticsByCode(got.Diagnostics, "LDL1901")
	if len(forbidden) != 13 {
		t.Fatalf("forbidden diagnostics = %+v, want 13; annotations=%+v", forbidden, got.Project.Annotations)
	}
	for _, diagnostic := range forbidden {
		if diagnostic.Range == nil || diagnostic.Range.StartByte < 0 || diagnostic.Range.EndByte > len(source) || diagnostic.Range.StartByte >= diagnostic.Range.EndByte {
			t.Fatalf("invalid forbidden diagnostic range: %+v", diagnostic)
		}
	}
	want := map[string]string{
		"arrow_notation":     "A => B",
		"constellation_note": "constellation map",
		"functional_note":    "functional design",
		"class_note":         "class ownership",
	}
	if !reflect.DeepEqual(got.Project.Annotations, want) {
		t.Fatalf("published annotations = %+v, want %+v", got.Project.Annotations, want)
	}
}

func TestRequiredDefinitionMembersDistinguishMissingFromWrongShape(t *testing.T) {
	t.Parallel()

	source := `project p "Project" {}
entity_type endpoint "Endpoint" {
  representation {}
}
relation_type rel "Rel" dependency {
  from {}
  to target types [endpoint]
  label {}
  projection matrix {
    row_endpoint {}
    column_endpoint to
    include_relation_rows {}
  }
  projection tree {
    parent_endpoint {}
    child_endpoint to
  }
  projection flow {
    source_endpoint from
    target_endpoint to
    connector_kind {}
  }
}
`
	got := compileProject(t, map[string]string{"document.ldl": source})
	invalidShape := diagnosticsByMessage(got.Diagnostics, "unknown or invalid schema member")
	if len(invalidShape) != 7 {
		t.Fatalf("invalid-shape diagnostics = %+v, want 7", got.Diagnostics)
	}
	for _, message := range []string{"missing representation", "missing endpoint", "missing required string", "missing required boolean", "missing connector kind"} {
		if diagnostics := diagnosticsByMessage(got.Diagnostics, message); len(diagnostics) != 0 {
			t.Fatalf("wrong-shaped member cascaded into %q: %+v", message, diagnostics)
		}
	}
	if got.EntityTypes[0].Representation != (Representation{}) || !reflect.DeepEqual(got.RelationTypes[0].From, EndpointRule{}) || got.RelationTypes[0].ForwardLabel != "" {
		t.Fatalf("wrong-shaped members entered typed output: entity=%+v relation=%+v", got.EntityTypes[0], got.RelationTypes[0])
	}
}

func TestDefinitionIdentityIsSemanticAndSourceOrderIndependent(t *testing.T) {
	t.Parallel()

	first := identityFixture(`[legacy_z, legacy_a]`, `[removed_z, removed_a]`, `
  entity_type old_a -> old_b
  entity_type old_b -> current
  entity_type_column current old_column -> current_column`)
	second := identityFixture(`[legacy_a, legacy_z]`, `[removed_a, removed_z]`, `
  entity_type_column current old_column -> current_column

  entity_type old_b -> current
  entity_type old_a -> old_b`)
	a := compileProject(t, map[string]string{"document.ldl": first})
	b := compileProject(t, map[string]string{"document.ldl": second})
	if a.HasErrors || b.HasErrors {
		t.Fatalf("identity fixtures failed: first=%+v second=%+v", a.Diagnostics, b.Diagnostics)
	}
	if !reflect.DeepEqual(a.Identity, b.Identity) {
		t.Fatalf("semantic identity changed with source order:\nfirst=%+v\nsecond=%+v", a.Identity, b.Identity)
	}
	root := a.Identity.RootReservations["ldl:project:p"]
	if !reflect.DeepEqual(root[resolve.KindEntityType], []string{"legacy_a", "legacy_z"}) || len(root) != 8 {
		t.Fatalf("root reservations = %+v", root)
	}
	if _, leaked := root[resolve.KindColumn]; leaked {
		t.Fatalf("owner-scoped reservation leaked into root identity: %+v", root)
	}
	if !reflect.DeepEqual(a.EntityTypes[0].ReservedColumnIDs, []string{"removed_a", "removed_z"}) {
		t.Fatalf("owner reservations = %+v", a.EntityTypes[0].ReservedColumnIDs)
	}
	if len(a.Identity.Moves) != 3 || a.Identity.Moves[0].OwnerAddress != nil || a.Identity.Moves[2].OwnerAddress == nil || *a.Identity.Moves[2].OwnerAddress != "ldl:project:p:entity-type:current" {
		t.Fatalf("semantic moves = %+v", a.Identity.Moves)
	}
	foundChildClosure := false
	for _, move := range a.Identity.MoveClosure {
		if move.Kind == resolve.KindColumn && move.TerminalAddress == "ldl:project:p:entity-type:current:column:current_column" {
			foundChildClosure = move.OwnerAddress != nil && *move.OwnerAddress == "ldl:project:p:entity-type:current"
		}
	}
	if !foundChildClosure {
		t.Fatalf("owner-scoped move closure = %+v", a.Identity.MoveClosure)
	}
	assertNoSourceIdentityFields(t, reflect.TypeOf(a.Identity), map[reflect.Type]bool{})
}

func identityFixture(rootReservations, ownerReservations, moves string) string {
	return `project p "Project" {}
reserved {
  entity_types ` + rootReservations + `
}
entity_type current "Current" {
  representation shape rect
  columns {
    current_column "Current Column" string
  }
  reserve {
    columns ` + ownerReservations + `
  }
}
moves {` + moves + `
}
`
}

func assertNoSourceIdentityFields(t *testing.T, typ reflect.Type, seen map[reflect.Type]bool) {
	t.Helper()
	for typ.Kind() == reflect.Pointer || typ.Kind() == reflect.Slice || typ.Kind() == reflect.Array || typ.Kind() == reflect.Map {
		typ = typ.Elem()
	}
	if typ.Kind() != reflect.Struct || seen[typ] {
		return
	}
	seen[typ] = true
	for i := 0; i < typ.NumField(); i++ {
		field := typ.Field(i)
		name := strings.ToLower(field.Name)
		if strings.Contains(name, "range") || strings.Contains(name, "span") || strings.Contains(name, "source") && field.Name != "SourceAddress" {
			t.Fatalf("identity type %s leaks source metadata field %s", typ, field.Name)
		}
		assertNoSourceIdentityFields(t, field.Type, seen)
	}
}
