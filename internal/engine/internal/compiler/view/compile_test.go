// SPDX-License-Identifier: LicenseRef-LayerDraw-1.0

package view

import (
	"reflect"
	"testing"

	"github.com/dencyuinc/layerdraw/internal/engine/internal/compiler/query"
	"github.com/dencyuinc/layerdraw/internal/engine/internal/compiler/resolve"
	"github.com/dencyuinc/layerdraw/internal/engine/internal/compiler/semantic/definition"
	"github.com/dencyuinc/layerdraw/internal/engine/internal/compiler/semantic/graph"
	"github.com/dencyuinc/layerdraw/internal/engine/internal/compiler/syntax"
)

func TestCompileEveryShapeAndExportFamily(t *testing.T) {
	input := compileInput(t, validAllRecipes)
	got := Compile(input)
	if got.HasErrors {
		t.Fatalf("Compile() diagnostics=%+v\nbindings=%+v", got.Diagnostics, input.Resolve.Bindings)
	}
	if len(got.Recipes) != 7 || len(got.ExportRecipes.Recipes) != 15 {
		t.Fatalf("recipes=%d exports=%d", len(got.Recipes), len(got.ExportRecipes.Recipes))
	}
	if !got.MatchesResolve(input.Resolve) || !got.ExportRecipes.MatchesResolve(input.Resolve) {
		t.Fatal("successful stages lost their Resolve generation")
	}

	diagram := recipeByID(t, got, "diagram_view")
	if diagram.StateRequirement != query.StateNone || diagram.Shape.Diagram == nil || !diagram.Shape.Diagram.Composed || len(diagram.Shape.Diagram.Placements) != 1 {
		t.Fatalf("diagram=%+v", diagram)
	}
	if len(diagram.RelationProjections) != 1 || diagram.RelationProjections[0].Projections.Diagram.EdgeLabel != definition.ProjectionLabelDisplayName || diagram.RelationProjections[0].Render.Edge.Line != definition.RenderLineDashed {
		t.Fatalf("effective projection=%+v", diagram.RelationProjections)
	}
	if len(diagram.Source.Query.Arguments) != 2 || !diagram.Source.Query.Arguments[1].Defaulted {
		t.Fatalf("complete arguments=%+v", diagram.Source.Query.Arguments)
	}
	for _, argument := range diagram.Source.Query.Arguments {
		found := false
		for _, address := range diagram.Dependencies.ParameterAddresses {
			found = found || address == argument.ParameterAddress
		}
		if !found {
			t.Fatalf("argument parameter %q is absent from dependencies: %+v", argument.ParameterAddress, diagram.Dependencies)
		}
	}
	if !reflect.DeepEqual(diagram.ReservedTableColumnIDs, []string{"legacy_column"}) || !reflect.DeepEqual(diagram.ReservedExportIDs, []string{"legacy_export"}) {
		t.Fatalf("reservations=%v/%v", diagram.ReservedTableColumnIDs, diagram.ReservedExportIDs)
	}

	table := recipeByID(t, got, "table_view")
	if table.Shape.Table == nil || table.Shape.Table.RowSource != RowsEntityRows || len(table.Shape.Table.Columns) != 2 || table.Shape.Table.Columns[0].ValueType.ScalarType != definition.ScalarEnum || table.Shape.Table.Columns[1].Source.StateFieldPath != query.StateSystemUpdatedAt {
		t.Fatalf("table=%+v", table.Shape.Table)
	}
	if table.StateInput != query.StateOptional || table.StateRequirement != query.StateOptional || len(table.Dependencies.StateReads) != 1 {
		t.Fatalf("table state=%s/%s deps=%+v", table.StateInput, table.StateRequirement, table.Dependencies.StateReads)
	}

	matrix := recipeByID(t, got, "matrix_view")
	if matrix.Shape.Matrix == nil || matrix.Shape.Matrix.Cell.Display != MatrixAttributeSummary || matrix.Shape.Matrix.Cell.AttributeColumnAddresses == nil || len(*matrix.Shape.Matrix.Cell.AttributeColumnAddresses) != 1 {
		t.Fatalf("matrix=%+v", matrix.Shape.Matrix)
	}
	tree := recipeByID(t, got, "tree_view")
	if tree.Shape.Tree == nil || tree.Shape.Tree.CyclePolicy != TreeCycleTruncate || tree.Shape.Tree.SharedChildPolicy != SharedChildLink {
		t.Fatalf("tree=%+v", tree.Shape.Tree)
	}
	flow := recipeByID(t, got, "flow_view")
	if flow.Shape.Flow == nil || flow.Shape.Flow.LaneBy != LaneAttribute || flow.Shape.Flow.LaneColumnAddresses == nil || flow.Shape.Flow.CyclePolicy != FlowCycleError || !flow.Shape.Flow.PreserveParallel {
		t.Fatalf("flow=%+v", flow.Shape.Flow)
	}
	context := recipeByID(t, got, "context_view")
	if context.Shape.Context == nil || context.Shape.Context.GroupBy != ContextGroupEntityType || !context.Shape.Context.IncludeEntityRows || !context.Shape.Context.IncludeRelationRows || !context.Shape.Context.Incoming || !context.Shape.Context.Outgoing {
		t.Fatalf("context=%+v", context.Shape.Context)
	}
	diff := recipeByID(t, got, "diff_view")
	if diff.Source.Diff == nil || diff.Source.Diff.Before != "revision:before" || diff.Source.Diff.After != "revision:after" || diff.Shape.Diff == nil || !diff.Shape.Diff.DetectMoves || !reflect.DeepEqual(diff.Shape.Diff.Include, []DiffSubjectKind{DiffEntity, DiffRelation, DiffViewExport}) {
		t.Fatalf("diff=%+v shape=%+v", diff.Source.Diff, diff.Shape.Diff)
	}

	formats := map[string]bool{}
	for _, recipe := range got.ExportRecipes.Recipes {
		formats[string(recipe.Format)] = true
		if recipe.ExporterProfile.RegistryDigest == "" || recipe.ExporterProfile.SpecificationDigest == "" || recipe.Extension == "" {
			t.Fatalf("open Export recipe=%+v", recipe)
		}
	}
	for _, format := range []string{"json", "yaml", "svg", "png", "pdf", "html", "csv", "tsv", "xlsx", "markdown", "pptx", "docx", "mermaid", "bpmn", "drawio"} {
		if !formats[format] {
			t.Fatalf("missing format %s", format)
		}
	}
}

func compileInput(t *testing.T, source string) Input {
	t.Helper()
	parsed := resolve.SourceFromParse(syntax.Parse([]byte(source)))
	resolved := resolve.Resolve(resolve.Input{Mode: resolve.CompileProject, EntryPath: "document.ldl", Project: resolve.ProjectInput{Files: map[string]resolve.SourceFile{"document.ldl": parsed}}})
	defined := definition.Compile(definition.Input{Resolve: resolved})
	graphed := graph.Compile(graph.Input{Resolve: resolved, Definition: defined})
	queried := query.Compile(query.Input{Resolve: resolved, Definition: defined, Graph: graphed})
	return Input{Resolve: resolved, Definition: defined, Graph: graphed, Query: queried}
}

func recipeByID(t *testing.T, result Result, id string) Recipe {
	t.Helper()
	for _, recipe := range result.Recipes {
		if recipe.ID == id {
			return recipe
		}
	}
	t.Fatalf("missing View %s", id)
	return Recipe{}
}

const validAllRecipes = `project p "Project" {}

layers {
  app "Application" @10
}

entity_type service "Service" {
  representation shape rect
  columns {
    environment "Environment" enum [prod, stg] required default prod
  }
}

relation_type link "Link" dependency {
  duplicate_policy allow
  from source types [service] layers [app]
  to target types [service] layers [app]
  label "links"
  reverse "is linked by"
  columns {
    weight "Weight" number
    branch "Branch" string
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
  projection flow {
    source_endpoint from
    target_endpoint to
    connector_kind data
    branch_value_column branch
  }
}

entities service @app {
  alpha "Alpha"
  beta "Beta"
}

rows service [environment] {
  alpha primary: prod
  beta primary: stg
}

relations link {
  alpha_beta: alpha -> beta
}

relation_rows link [weight, branch] {
  alpha_beta primary: 1, "yes"
}

query scope "Scope" {
  parameters {
    environment enum [prod, stg] required
    limit integer default 10 min 0
  }
  select {
    entity_types [service]
    relation_types [link]
    roots [alpha]
  }
  traverse outgoing 0..2 visit_once relations [link]
  result [seed_entities, traversed_entities, path_relations]
}

view diagram_view "Diagram" topology {
  source query scope { environment: prod }
  relation_projection link {
    composed {
      priority 5
      conflict prefer_first
      keep_edge true
    }
    diagram {
      edge_label display_name
      include_relation_type true
    }
    table {
      row_mode relation_rows
      include_from false
      include_to true
      include_relation_type false
    }
    matrix {
      row_endpoint from
      column_endpoint to
      include_relation_rows true
    }
    tree {
      parent_endpoint from
      child_endpoint to
    }
    flow {
      source_endpoint from
      target_endpoint to
      connector_kind message
      branch_value_column branch
    }
    context {
      fact_template "{from.display_name} links {to.display_name}"
      reverse_fact_template "{to.display_name} receives {relation.type}"
      include_attribute_rows true
    }
    render edge {
      arrow both
      line dashed
      color "#aabbcc"
      label display_name
    }
    render nested {
      frame_label display_name
      frame_style strong
    }
    render overlay {
      kind "shield"
      position bottom_left
      max_items 8
    }
    render badge {
      icon "check"
      label display_name
      position top_left
    }
  }
  diagram {
    layout manual
    direction right_to_left
    abstraction detail
    composed
    place alpha 0 0 100 50
  }
  export json_out json "diagram.json" {
    fidelity lossless
    source_refs
    diagnostics
  }
  export yaml_out yaml "diagram.yaml" {
    fidelity lossless
    source_refs
    state_summary
  }
  export svg_out svg "diagram.svg" {
    fidelity visual_only
    width 800
    height 600
    scale 2
    background "#abcdef"
  }
  export png_out png "diagram.png" {
    fidelity visual_only
    exporter_profile "layerdraw/png@1"
  }
  export pdf_out pdf "diagram.pdf" {
    fidelity visual_only
    page_size letter
    orientation landscape
    fit width
    legend
  }
  export html_out html "diagram.html" {
    fidelity traceable_summary
    source_refs
    interactive
    embed_assets
  }
  export csv_out csv "diagram.csv" {
    fidelity traceable_summary
    source_refs
    bundle
    header
    source_manifest
  }
  export xlsx_out xlsx "diagram.xlsx" {
    fidelity lossless
    source_refs
    profile composed_diagram_workbook
    lookup_sheets
    hidden_ids
    formulas
    view_data_json
  }
  export pptx_out pptx "diagram.pptx" {
    fidelity visual_only
  }
  export docx_out docx "diagram.docx" {
    fidelity visual_only
  }
  export mermaid_out mermaid "diagram.mmd" {
    fidelity lossy
  }
  export drawio_out drawio "diagram.drawio" {
    fidelity visual_only
    source_manifest
  }
  reserve {
    table_columns [legacy_column]
    exports [legacy_export]
  }
}

view table_view "Table" inventory {
  state_input optional
  source query scope { environment: prod }
  table {
    rows entity_rows
    entity_types [service]
    entity_id
    type
    layer
    column environment {
      source attribute environment entity_types [service]
      label "Environment"
      aggregate none
    }
    column updated {
      source state system.updated_at
      aggregate max
    }
    sort updated descending nulls last
  }
  export markdown_out markdown "table.md" {
    fidelity lossy
    source_manifest
  }
  export tsv_out tsv "table.tsv" {
    fidelity traceable_summary
    source_refs
    bundle
    header
    source_manifest
  }
}

view matrix_view "Matrix" dependency {
  source query scope { environment: prod }
  matrix {
    row_axis {
      entity_types [service]
      label display_name
    }
    column_axis {
      entity_types [service]
      label id
    }
    cell {
      relation_types [link]
      direction both
      semantic relation_refs
      display attribute_summary
      attributes [weight]
    }
  }
}

view tree_view "Tree" hierarchy {
  source query scope { environment: prod }
  tree {
    relation_types [link]
    cycle_policy truncate
    shared_child_policy link
  }
}

view flow_view "Flow" flow {
  source query scope { environment: prod }
  flow {
    relation_types [link]
    lane_by attribute.environment
    cycle_policy error
    preserve_parallel
  }
  export bpmn_out bpmn "flow.bpmn" {
    fidelity lossy
    source_manifest
  }
}

view context_view "Context" context {
  source query scope { environment: prod }
  context {
    group_by entity_type
    entity_rows
    relation_rows
    incoming
    outgoing
  }
}

view diff_view "Diff" diff {
  source diff "revision:before" -> "revision:after" {
    query scope
    arguments { environment: prod }
  }
  diff {
    include [entity, relation, view_export]
    detect_moves
  }
}
`
