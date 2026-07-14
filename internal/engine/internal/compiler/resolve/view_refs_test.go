// SPDX-License-Identifier: LicenseRef-LayerDraw-1.0

package resolve

import "testing"

func TestQuerySelectorHelpersAreClosedToOwnerKinds(t *testing.T) {
	if querySelectorVia(KindEntityType) != "query:select.entity_types" || querySelectorVia(KindRelationType) != "query:select.relation_types" {
		t.Fatal("selector binding routes changed")
	}
	if querySelectorVia(KindEntity) != "" || querySelectorAuthored(&moduleState{}, "missing", KindEntity) || querySelectorAuthored(&moduleState{}, "missing", KindEntityType) {
		t.Fatal("unsupported or absent Query selector was treated as authored")
	}
}

func TestViewReferencesBindEveryTypedRecipeFamily(t *testing.T) {
	t.Parallel()
	got := Resolve(Input{EntryPath: "document.ldl", Project: ProjectInput{Files: map[string]SourceFile{"document.ldl": parse(`project p "Project" {}

layers {
  app "Application" @1
}

entity_type service "Service" {
  columns {
    environment "Environment" string
  }
}
entity_type workload "Workload" {
  columns {
    environment "Environment" string
  }
}

relation_type link "Link" dependency {
  columns {
    weight "Weight" number
    branch "Branch" string
  }
}

entities service @app {
  alpha "Alpha"
}

query scope "Scope" {
  parameters {
    environment string required
  }
  select {
    entity_types [service, workload]
    relation_types [link]
    roots [alpha]
  }
}

view diagram_view "Diagram" topology {
  source query scope { environment: "prod" }
  relation_projection link {
    flow {
      source_endpoint from
      target_endpoint to
      connector_kind data
      branch_value_column branch
    }
  }
  diagram {
    place alpha 0 0 20 10
  }
}

view table_view "Table" inventory {
  source query scope { environment: "prod" }
  table {
    rows entity_rows
    entity_types [service, workload]
    column environment {
      source attribute environment
    }
    column count {
      source derived_count outgoing relations [link]
    }
  }
}

view matrix_view "Matrix" dependency {
  source query scope { environment: "prod" }
  matrix {
    row_axis {
      entity_types [service]
    }
    column_axis {
      entity_types [workload]
    }
    cell {
      relation_types [link]
      attributes [weight]
    }
  }
}

view tree_view "Tree" hierarchy {
  source query scope { environment: "prod" }
  tree {
    relation_types [link]
  }
}

view flow_view "Flow" flow {
  source diff "before" -> "after" {
    arguments { environment: "prod" }
    query scope
  }
  flow {
    relation_types [link]
    lane_by attribute.environment
  }
}
`)}}})
	if got.HasErrors {
		t.Fatalf("Resolve() diagnostics=%+v", got.Diagnostics)
	}

	wantVia := map[string]int{
		"view:source.query": 4, "view:source.diff.query": 1, "view:source.argument": 5,
		"view:relation_projection": 1, "view:projection.flow.branch_value_column": 1, "view:diagram.place": 1,
		"view:table.entity_types": 2, "view:table.column.attribute": 2, "view:table.column.derived_count": 1,
		"view:matrix.axis.entity_types": 2, "view:matrix.cell.relation_types": 1, "view:matrix.cell.attributes": 1,
		"view:tree.relation_types": 1, "view:flow.relation_types": 1, "view:flow.lane_by": 2,
	}
	counts := map[string]int{}
	for _, binding := range got.Bindings {
		counts[binding.Via]++
		if binding.Via == "view:table.column.attribute" && binding.SourceAddress != "ldl:project:p:view:table_view:table-column:environment" {
			t.Fatalf("Table Column binding has unstable owner-scoped source: %+v", binding)
		}
	}
	for via, want := range wantVia {
		if counts[via] != want {
			t.Fatalf("bindings via %s=%d, want %d\nall=%+v", via, counts[via], want, got.Bindings)
		}
	}
}

func TestImportedSelectedViewClosesOverPrivateDependenciesAndIgnoresPrivateView(t *testing.T) {
	t.Parallel()
	got := Resolve(Input{EntryPath: "document.ldl", Project: ProjectInput{Files: map[string]SourceFile{
		"document.ldl": parse(`import { imported_view } from "./views.ldl"
project p "Project" {}
`),
		"views.ldl": parse(`entity_type private_service "Private Service" {
  columns {
    environment "Environment" string
  }
}

query private_scope "Private Scope" {
  parameters {
    environment string required
  }
  select {
    entity_types [private_service]
  }
}

view imported_view "Imported View" inventory {
  source query private_scope { environment: "prod" }
  table {
    rows entity_rows
    entity_types [private_service]
    column environment {
      source attribute environment
    }
  }
  export data csv "data.csv" {
    fidelity lossy
  }
}

view private_broken "Private Broken" topology {
  source query missing_query {}
  diagram {
    place missing_entity 0 0 10 10
  }
}

export { imported_view }
`),
	}}})
	if got.HasErrors {
		t.Fatalf("private unselected View diagnosed selected effective document: %+v", got.Diagnostics)
	}
	for _, address := range []string{
		"ldl:project:p:view:imported_view",
		"ldl:project:p:view:imported_view:table-column:environment",
		"ldl:project:p:view:imported_view:export:data",
		"ldl:project:p:query:private_scope",
		"ldl:project:p:query:private_scope:parameter:environment",
		"ldl:project:p:entity-type:private_service",
		"ldl:project:p:entity-type:private_service:column:environment",
	} {
		if !hasAddress(got, address) {
			t.Fatalf("selected View closure omitted %s; declarations=%s", address, addresses(got))
		}
	}
	if hasAddress(got, "ldl:project:p:view:private_broken") {
		t.Fatalf("private View leaked into the effective document: %s", addresses(got))
	}
	if !hasCandidate(got, "ldl:project:p:view:private_broken") {
		t.Fatal("private View should remain an available candidate")
	}

	columnSource := "ldl:project:p:view:imported_view:table-column:environment"
	found := false
	for _, binding := range got.Bindings {
		if binding.SourceAddress == columnSource && binding.TargetAddress == "ldl:project:p:entity-type:private_service:column:environment" {
			found = true
		}
		if binding.SourceAddress == "ldl:project:p:view:private_broken" {
			t.Fatalf("private View binding leaked: %+v", binding)
		}
	}
	if !found {
		t.Fatalf("owner-scoped imported Column binding missing: %+v", got.Bindings)
	}
}

func TestSelectedViewReportsExactUnknownReferenceRanges(t *testing.T) {
	t.Parallel()
	source := `project p "Project" {}
view broken "Broken" topology {
  source query missing_query {}
  diagram {
    place missing_entity 0 0 10 10
  }
}
`
	got := Resolve(Input{EntryPath: "document.ldl", Project: ProjectInput{Files: map[string]SourceFile{"document.ldl": parse(source)}}})
	if !got.HasErrors || len(got.Diagnostics) != 2 {
		t.Fatalf("Resolve() diagnostics=%+v", got.Diagnostics)
	}
	seen := map[string]bool{}
	for _, diagnostic := range got.Diagnostics {
		if diagnostic.Range == nil {
			t.Fatalf("diagnostic lacks source range: %+v", diagnostic)
		}
		text := source[diagnostic.Range.StartByte:diagnostic.Range.EndByte]
		seen[text] = true
	}
	if !seen["missing_query"] || !seen["missing_entity"] {
		t.Fatalf("minimum ranges=%v diagnostics=%+v", seen, got.Diagnostics)
	}
}

func TestViewAttributeOwnerInferenceCoversEveryRowSource(t *testing.T) {
	t.Parallel()
	got := Resolve(Input{EntryPath: "document.ldl", Project: ProjectInput{Files: map[string]SourceFile{"document.ldl": parse(`project p "Project" {}
entity_type service "Service" {
  columns {
    environment "Environment" string
  }
}
entity_type workload "Workload" {
  columns {
    environment "Environment" string
  }
}
relation_type link "Link" dependency {
  columns {
    environment "Environment" string
  }
}
query scope "Scope" {
  select {
    entity_types [service]
    relation_types [link]
  }
}
view entity_default "Entity Default" inventory {
  source query scope {}
  table {
    rows entity
    column environment {
      source attribute environment
    }
  }
}
view relation_default "Relation Default" inventory {
  source query scope {}
  table {
    rows relation_rows
    column environment {
      source attribute environment
    }
  }
}
view automatic_default "Automatic Default" inventory {
  source query scope {}
  table {
    rows automatic_relations
    column environment {
      source attribute environment
    }
  }
}
view explicit_entity "Explicit Entity" inventory {
  source query scope {}
  table {
    rows entity_rows
    column environment {
      source attribute environment entity_types [service]
    }
  }
}
view explicit_relation "Explicit Relation" inventory {
  source query scope {}
  table {
    rows relation_rows
    column environment {
      source attribute environment relation_types [link]
    }
  }
}
view source_after_shape "Source After Shape" inventory {
  table {
    rows entity_rows
    column environment {
      source attribute environment
    }
  }
  source query scope {}
}
view skipped_non_reference_forms "Skipped Forms" topology {
  source query scope {}
  relation_projection link {
    flow {
      branch_value_column
    }
  }
  diagram {
    place
  }
  table {
    entity_types service
    column ignored {
      source derived_count outgoing
    }
  }
  matrix {
    cell {
      attributes weight
    }
  }
  flow {
    lane_by layer
  }
}
`)}}})
	if got.HasErrors {
		t.Fatalf("Resolve() diagnostics=%+v", got.Diagnostics)
	}
	wants := map[string]string{
		"ldl:project:p:view:entity_default:table-column:environment":     "ldl:project:p:entity-type:service:column:environment",
		"ldl:project:p:view:relation_default:table-column:environment":   "ldl:project:p:relation-type:link:column:environment",
		"ldl:project:p:view:automatic_default:table-column:environment":  "ldl:project:p:relation-type:link:column:environment",
		"ldl:project:p:view:explicit_entity:table-column:environment":    "ldl:project:p:entity-type:service:column:environment",
		"ldl:project:p:view:explicit_relation:table-column:environment":  "ldl:project:p:relation-type:link:column:environment",
		"ldl:project:p:view:source_after_shape:table-column:environment": "ldl:project:p:entity-type:service:column:environment",
	}
	found := map[string]bool{}
	for _, binding := range got.Bindings {
		if want, ok := wants[binding.SourceAddress]; ok && binding.Via == "view:table.column.attribute" {
			if binding.TargetAddress != want {
				t.Fatalf("source-order-dependent owner inference: got %+v, want %s", binding, want)
			}
			if found[binding.SourceAddress] {
				t.Fatalf("duplicate inferred owner binding: %+v", binding)
			}
			found[binding.SourceAddress] = true
		}
	}
	for sourceAddress, want := range wants {
		if !found[sourceAddress] {
			t.Fatalf("inferred owner binding missing for %s -> %s\nall=%+v", sourceAddress, want, got.Bindings)
		}
	}
}

func TestViewOwnerInferenceDoesNotDependOnQueryDeclarationOrder(t *testing.T) {
	t.Parallel()
	got := Resolve(Input{EntryPath: "document.ldl", Project: ProjectInput{Files: map[string]SourceFile{"document.ldl": parse(`project p "Project" {}
entity_type selected_type "Selected" {
  columns {
    environment "Environment" string
  }
}
entity_type unrelated_type "Unrelated" {
  columns {
    environment "Environment" integer
  }
}
view inventory_view "Inventory" inventory {
  source query selected_scope {}
  table {
    rows entity_rows
    column environment {
      source attribute environment
    }
  }
}
query selected_scope "Selected Scope" {
  select {
    entity_types [selected_type]
  }
}
`)}}})
	if got.HasErrors {
		t.Fatalf("Resolve() diagnostics=%+v", got.Diagnostics)
	}

	const columnAddress = "ldl:project:p:view:inventory_view:table-column:environment"
	var targets []string
	for _, binding := range got.Bindings {
		if binding.SourceAddress == columnAddress && binding.Via == "view:table.column.attribute" {
			targets = append(targets, binding.TargetAddress)
		}
	}
	if len(targets) != 1 || targets[0] != "ldl:project:p:entity-type:selected_type:column:environment" {
		t.Fatalf("View owner inference used declarations outside the later Query selector: %v", targets)
	}
}

func TestViewOwnerInferenceUsesOnlyQuerySelectors(t *testing.T) {
	t.Parallel()
	got := Resolve(Input{EntryPath: "document.ldl", Project: ProjectInput{Files: map[string]SourceFile{"document.ldl": parse(`project p "Project" {}
entity_type selected_type "Selected" {
  columns {
    environment "Environment" string
  }
}
entity_type predicate_type "Predicate Only" {
  columns {
    environment "Environment" integer
  }
}
query selected_scope "Selected Scope" {
  select {
    entity_types [selected_type]
  }
  where all {
    field type != predicate_type
  }
}
view inventory_view "Inventory" inventory {
  source query selected_scope {}
  table {
    rows entity_rows
    column environment {
      source attribute environment
    }
  }
}
`)}}})
	if got.HasErrors {
		t.Fatalf("Resolve() diagnostics=%+v", got.Diagnostics)
	}

	const columnAddress = "ldl:project:p:view:inventory_view:table-column:environment"
	var targets []string
	for _, binding := range got.Bindings {
		if binding.SourceAddress == columnAddress && binding.Via == "view:table.column.attribute" {
			targets = append(targets, binding.TargetAddress)
		}
	}
	if len(targets) != 1 || targets[0] != "ldl:project:p:entity-type:selected_type:column:environment" {
		t.Fatalf("predicate bindings widened View owner inference: %v", targets)
	}
}

func TestViewOwnerInferencePreservesExplicitlyEmptyQuerySelector(t *testing.T) {
	t.Parallel()
	source := `project p "Project" {}
entity_type visible_type "Visible" {
  columns {
    environment "Environment" string
  }
}
query empty_scope "Empty Scope" {
  select {
    entity_types []
  }
}
view inventory_view "Inventory" inventory {
  source query empty_scope {}
  table {
    rows entity_rows
    column environment {
      source attribute environment
    }
  }
}
`
	got := Resolve(Input{EntryPath: "document.ldl", Project: ProjectInput{Files: map[string]SourceFile{"document.ldl": parse(source)}}})
	if !got.HasErrors || len(got.Diagnostics) != 1 || got.Diagnostics[0].Message != "View Column is unknown for its owner types" {
		t.Fatalf("explicitly empty selector fell back to visible owners: %+v", got.Diagnostics)
	}
	for _, binding := range got.Bindings {
		if binding.SourceAddress == "ldl:project:p:view:inventory_view:table-column:environment" && binding.Via == "view:table.column.attribute" {
			t.Fatalf("explicitly empty selector produced an owner binding: %+v", binding)
		}
	}
}

func TestViewArgumentAndColumnUnknownsDiagnoseOnlySelectedReferences(t *testing.T) {
	t.Parallel()
	source := `project p "Project" {}
entity_type service "Service" {
  columns {
    environment "Environment" string
  }
}
query scope "Scope" {
  parameters {
    environment string required
  }
  select {
    entity_types [service]
  }
}
view broken "Broken" inventory {
  source query scope { unknown_parameter: "prod" }
  table {
    rows entity_rows
    entity_types [service]
    column missing {
      source attribute unknown_column
    }
  }
}
`
	got := Resolve(Input{EntryPath: "document.ldl", Project: ProjectInput{Files: map[string]SourceFile{"document.ldl": parse(source)}}})
	if !got.HasErrors || len(got.Diagnostics) != 2 {
		t.Fatalf("Resolve() diagnostics=%+v", got.Diagnostics)
	}
	seen := map[string]bool{}
	for _, diagnostic := range got.Diagnostics {
		if diagnostic.Range == nil {
			t.Fatalf("missing range: %+v", diagnostic)
		}
		seen[source[diagnostic.Range.StartByte:diagnostic.Range.EndByte]] = true
	}
	if !seen["unknown_parameter"] || !seen["unknown_column"] {
		t.Fatalf("diagnostic ranges=%v", seen)
	}
}
