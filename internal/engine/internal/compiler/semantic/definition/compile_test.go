// SPDX-License-Identifier: LicenseRef-LayerDraw-1.0

package definition

import (
	"reflect"
	"testing"

	"github.com/dencyuinc/layerdraw/internal/engine/internal/compiler/resolve"
	"github.com/dencyuinc/layerdraw/internal/engine/internal/compiler/syntax"
)

func TestCompileProjectDefinitionsAndDefaults(t *testing.T) {
	t.Parallel()

	got := compileProject(t, map[string]string{"document.ldl": `
project p "Project" {
  tags [beta, alpha]
  annotations { owner: "platform" }
}

layers {
  app "Application" @10
  net "Network" @10
}

entity_type server "Server" {
  representation shape rect
  color "#526d82"
  image "./assets/server.png"
  columns {
    name "Name" string required default "api" min_length 1 max_length 32
    launched "Launched" date default "2026-07-14"
    status "Status" enum [active, paused] reserve_values [deleted] default active
  }
  unique server_name [name]
}

relation_type depends_on "Depends on" dependency {
  from source types [server] layers [app]
  to target types [server] layers [net]
  label "depends on"
  reverse "supports"
  columns {
    status "Status" enum [active, paused] default active
  }
  projection diagram {
    edge_label reverse_label
  }
  projection flow {
    source_endpoint from
    target_endpoint to
    connector_kind data
    branch_value_column status
  }
  render edge {
    label reverse_label
  }
}

reference runbook <<-TEXT
Use the primary runbook.
TEXT
`})
	if got.HasErrors {
		t.Fatalf("Diagnostics = %+v, want none", got.Diagnostics)
	}
	if got.Project == nil || got.Project.DisplayName != "Project" || !reflect.DeepEqual(got.Project.Tags, []string{"alpha", "beta"}) {
		t.Fatalf("Project = %+v", got.Project)
	}
	if len(got.Layers) != 2 {
		t.Fatalf("Layers = %+v", got.Layers)
	}
	display := LayersByDisplayOrder(got.Layers)
	if display[0].ID != "app" || display[1].ID != "net" {
		t.Fatalf("display order = %+v", display)
	}
	et := got.EntityTypes[0]
	if et.Color == nil || *et.Color != "#526D82" || et.Image == nil || et.Image.Locator != "assets/server.png" {
		t.Fatalf("entity visual fields = %+v", et)
	}
	if et.Columns[1].Default == nil || et.Columns[1].Default.Type != ScalarDate || et.Columns[1].Default.String != "2026-07-14" {
		t.Fatalf("date scalar was not tagged: %+v", et.Columns[1].Default)
	}
	rt := got.RelationTypes[0]
	if rt.Projections.Table.RowMode != "automatic" || rt.Projections.Matrix != nil || rt.Render.Edge.Label != "reverse_label" {
		t.Fatalf("relation defaults/overlays = %+v", rt)
	}
	if rt.Projections.Flow == nil || rt.Projections.Flow.BranchValueColumnAddress == nil || *rt.Projections.Flow.BranchValueColumnAddress != rt.Columns[0].Address {
		t.Fatalf("flow branch column = %+v", rt.Projections.Flow)
	}
	if len(got.References) != 1 || got.References[0].Text != "Use the primary runbook.\n" {
		t.Fatalf("Reference = %+v", got.References)
	}
}

func TestCompileDiagnosticsUseRegisteredCodes(t *testing.T) {
	t.Parallel()

	got := compileProject(t, map[string]string{"document.ldl": `
project p "Project" {}
entity_type server "Server" {
  unknown "x"
  representation shape triangle
  image "../escape.png"
  columns {
    age "Age" integer default "old"
  }
  unique bad [missing]
}
relation_type r "R" dependency {
  from source types []
  to target types [server]
  label "r"
  projection diagram {
    source_endpoint from
    target_endpoint from
    edge_label reverse_label
  }
}
`})
	codes := map[string]bool{}
	for _, d := range got.Diagnostics {
		codes[d.Code+"/"+d.MessageKey] = true
		if d.Arguments == nil || len(d.Arguments) != 0 {
			t.Fatalf("diagnostic arguments must be empty: %+v", d)
		}
		if d.Range == nil || d.Range.ModulePath != "document.ldl" {
			t.Fatalf("diagnostic range = %+v", d)
		}
	}
	for _, want := range []string{
		"LDL1102/unknown_or_duplicate_schema_member",
		"LDL1201/module_pack_or_asset_resolution_failed",
		"LDL1301/unknown_or_ambiguous_symbol",
		"LDL1401/scalar_or_column_type_mismatch",
		"LDL1501/invalid_relation_endpoint_or_self_rule",
		"LDL1504/invalid_projection_contract",
	} {
		if !codes[want] {
			t.Fatalf("missing diagnostic %s in %+v", want, got.Diagnostics)
		}
	}
}

func TestCompileEndpointRefsUseResolverImports(t *testing.T) {
	t.Parallel()

	got := compileProject(t, map[string]string{
		"document.ldl": `
import { external_type as imported_type } from "./schema.ldl"
import ns from "./schema.ldl"
project p "Project" {}
layers {
  app "Application" @0
}
relation_type rel "Rel" dependency {
  from source types [imported_type] layers [app]
  to target types [ns.external_type] layers [app]
  label "rel"
}
`,
		"schema.ldl": `
entity_type external_type "External" {
  representation shape rect
}
export { external_type }
`,
	})
	if got.HasErrors {
		t.Fatalf("Diagnostics = %+v", got.Diagnostics)
	}
	rt := got.RelationTypes[0]
	if len(rt.From.EntityTypeAddresses) != 1 || len(rt.To.EntityTypeAddresses) != 1 || rt.From.EntityTypeAddresses[0] != rt.To.EntityTypeAddresses[0] {
		t.Fatalf("endpoint addresses = from %+v to %+v", rt.From, rt.To)
	}
}

func TestCompileRelationProjectionRenderFamilies(t *testing.T) {
	t.Parallel()

	got := compileProject(t, map[string]string{"document.ldl": `
project p "Project" {}
layers {
  app "Application" @0
}
entity_type server "Server" {
  representation container
}
relation_type rel "Rel" data_flow {
  allow_self true
  duplicate_policy allow
  from parent types [server] layers [app]
  to child types [server] layers [app]
  cardinality {
    to_per_from 0..1
    from_per_to 1..*
  }
  label "relates"
  reverse "related by"
  columns {
    branch "Branch" string default "ok"
    count "Count" integer default 2 min 0 max 10
    score "Score" number default 1.5 min 0.5 max 2.5
    ok "OK" boolean default true
    seen "Seen" datetime default "2026-07-14T10:00:00Z"
  }
  traversal {
    default_direction both
    participates_in_impact true
    participates_in_flow true
    participates_in_hierarchy true
    participates_in_dependency_matrix true
  }
  projection composed {
    mode nest
    parent_endpoint from
    child_endpoint to
    priority 4
    conflict keep_edge
    keep_edge false
  }
  projection matrix {
    row_endpoint from
    column_endpoint to
    include_relation_rows true
  }
  projection tree {
    parent_endpoint from
    child_endpoint to
  }
  projection context {
    fact_template "{from.id} relates to {to.display_name}"
    reverse_fact_template "{to.id} is related by {from.display_name}"
    include_attribute_rows true
  }
  render edge {
    arrow both
    line dotted
    color "#11223344"
    label display_name
  }
  render nested {
    frame_label type
    frame_style strong
  }
  render overlay {
    kind shield
    position center
    max_items 8
  }
  render badge {
    icon "shield-check"
    label type
    position bottom_left
  }
  export {
    include_endpoints false
    include_relation_rows false
    sheet_name "Relations"
  }
}
`})
	if got.HasErrors {
		t.Fatalf("Diagnostics = %+v", got.Diagnostics)
	}
	rt := got.RelationTypes[0]
	if !rt.AllowSelf || rt.Cardinality.ToPerFrom.Max != "1" || rt.Cardinality.FromPerTo.Min != 1 {
		t.Fatalf("relation policy/cardinality = %+v", rt)
	}
	if rt.Traversal.DefaultDirection != "both" || !rt.Traversal.ParticipatesInDependencyMatrix {
		t.Fatalf("traversal = %+v", rt.Traversal)
	}
	if rt.Projections.Composed.Mode != "nest" || rt.Projections.Matrix == nil || rt.Projections.Tree == nil || !rt.Projections.Context.IncludeAttributeRows {
		t.Fatalf("projections = %+v", rt.Projections)
	}
	if rt.Render.Edge.Color == nil || *rt.Render.Edge.Color != "#11223344" || rt.Render.Overlay.MaxItems != 8 || rt.Render.Badge.Icon == nil {
		t.Fatalf("render = %+v", rt.Render)
	}
	if rt.Export.SheetName == nil || *rt.Export.SheetName != "Relations" || rt.Export.IncludeEndpoints || rt.Export.IncludeRelationRows {
		t.Fatalf("export = %+v", rt.Export)
	}
	for _, col := range rt.Columns {
		if col.Default == nil {
			t.Fatalf("missing default for %+v", col)
		}
	}
}

func TestCompileDeterministicAcrossInputMapOrder(t *testing.T) {
	t.Parallel()

	filesA := map[string]string{
		"document.ldl": `import { server } from "./b.ldl"
project p "Project" {}
layers { app "Application" @0 }
relation_type r "R" dependency {
  from source types [server]
  to target types [server]
  label "r"
}
`,
		"b.ldl": `entity_type server "Server" { representation shape rect }
export { server }
`,
	}
	filesB := map[string]string{"b.ldl": filesA["b.ldl"], "document.ldl": filesA["document.ldl"]}
	a := compileProject(t, filesA)
	b := compileProject(t, filesB)
	if !reflect.DeepEqual(a.EntityTypes, b.EntityTypes) || !reflect.DeepEqual(a.RelationTypes, b.RelationTypes) || !reflect.DeepEqual(a.Diagnostics, b.Diagnostics) {
		t.Fatalf("definition output changed with map order\nA=%+v\nB=%+v", a, b)
	}
}

func FuzzCompileDefinitionNoPanic(f *testing.F) {
	f.Add("project p \"P\" {}\n")
	f.Add("project p \"P\" {}\nentity_type e \"E\" { representation shape rect }\n")
	f.Fuzz(func(t *testing.T, src string) {
		if len(src) > 4096 {
			t.Skip()
		}
		in := resolve.Input{Mode: resolve.CompileProject, EntryPath: "document.ldl", Project: resolve.ProjectInput{Files: map[string]resolve.SourceFile{"document.ldl": parse(src)}}}
		_ = Compile(Input{Resolve: resolve.Resolve(in)})
	})
}

func compileProject(t *testing.T, files map[string]string) Result {
	t.Helper()
	input := resolve.Input{Mode: resolve.CompileProject, EntryPath: "document.ldl", Project: resolve.ProjectInput{Files: map[string]resolve.SourceFile{}}}
	for path, src := range files {
		input.Project.Files[path] = parse(src)
	}
	return Compile(Input{Resolve: resolve.Resolve(input)})
}

func parse(src string) resolve.SourceFile {
	return resolve.SourceFromParse(syntax.Parse([]byte(src)))
}
