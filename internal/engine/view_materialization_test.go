// SPDX-License-Identifier: LicenseRef-LayerDraw-1.0

package engine

import (
	"context"
	"reflect"
	"testing"

	"github.com/dencyuinc/layerdraw/internal/engine/internal/compiler/semantic/definition"
	"github.com/dencyuinc/layerdraw/internal/engine/internal/compiler/semantic/graph"
	"github.com/dencyuinc/layerdraw/internal/engine/internal/compiler/view"
)

func TestMaterializeViewBuildsDiagramViewDataFromQueryResult(t *testing.T) {
	t.Parallel()
	snapshot, queryResult := compileAndExecuteViewFixture(t, diagramViewSource())
	response := New(BuildInfo{}).MaterializeView(context.Background(), ViewMaterializationInput{
		Recipe:      snapshot.TypedAST.Views[0],
		Graph:       *snapshot.TypedAST.Graph,
		QueryResult: queryResult,
	})
	if response.Status != "ok" || response.Result == nil {
		t.Fatalf("MaterializeView() = %+v", response)
	}
	result := response.Result
	if result.Shape != "diagram" || result.Diagram == nil {
		t.Fatalf("shape/result = %+v", result)
	}
	if got := nodeAddresses(result.Diagram.Nodes); !reflect.DeepEqual(got, []string{"ldl:project:p:entity:alpha", "ldl:project:p:entity:beta"}) {
		t.Fatalf("nodes = %v", got)
	}
	if got := edgeAddresses(result.Diagram.Edges); !reflect.DeepEqual(got, []string{"ldl:project:p:relation:alpha_beta"}) {
		t.Fatalf("edges = %v", got)
	}
	if len(result.Diagram.Placements) != 1 || result.Diagram.Placements[0].EntityAddress != "ldl:project:p:entity:alpha" {
		t.Fatalf("placements = %+v", result.Diagram.Placements)
	}
	if !reflect.DeepEqual(result.Source.EntityAddresses, []string{"ldl:project:p:entity:alpha", "ldl:project:p:entity:beta"}) {
		t.Fatalf("source entities = %v", result.Source.EntityAddresses)
	}
	if !reflect.DeepEqual(result.Source.RelationAddresses, []string{"ldl:project:p:relation:alpha_beta"}) {
		t.Fatalf("source relations = %v", result.Source.RelationAddresses)
	}
}

func TestMaterializeViewBuildsTableRowsAndSourceCells(t *testing.T) {
	t.Parallel()
	snapshot, queryResult := compileAndExecuteViewFixture(t, tableViewSource())
	response := New(BuildInfo{}).MaterializeView(context.Background(), ViewMaterializationInput{
		Recipe:      snapshot.TypedAST.Views[0],
		Graph:       *snapshot.TypedAST.Graph,
		QueryResult: queryResult,
	})
	if response.Status != "ok" || response.Result == nil || response.Result.Table == nil {
		t.Fatalf("MaterializeView() = %+v", response)
	}
	table := response.Result.Table
	if got := columnIDs(table.Columns); !reflect.DeepEqual(got, []string{"entity_id", "type", "layer", "environment", "outgoing"}) {
		t.Fatalf("columns = %v", got)
	}
	if len(table.Rows) != 2 {
		t.Fatalf("rows = %+v", table.Rows)
	}
	first := table.Rows[0]
	if first.Key != "entity-row:ldl:project:p:entity:alpha:row:primary" || first.SubjectAddress != "ldl:project:p:entity:alpha" {
		t.Fatalf("first row = %+v", first)
	}
	if first.Cells[3].Value.Scalar == nil || first.Cells[3].Value.Scalar.String != "prod" {
		t.Fatalf("environment cell = %+v", first.Cells[3])
	}
	if !reflect.DeepEqual(first.Cells[3].SourceCells, []ViewDataCellRef{{RowAddress: "ldl:project:p:entity:alpha:row:primary", ColumnAddress: "ldl:project:p:entity-type:service:column:environment"}}) {
		t.Fatalf("source cells = %+v", first.Cells[3].SourceCells)
	}
	if first.Cells[4].Value.Scalar == nil || first.Cells[4].Value.Scalar.Int != 1 {
		t.Fatalf("derived count = %+v", first.Cells[4])
	}
	if !reflect.DeepEqual(response.Result.Source.RowAddresses, []string{"ldl:project:p:entity:alpha:row:primary", "ldl:project:p:entity:beta:row:primary", "ldl:project:p:relation:alpha_beta:row:primary"}) {
		t.Fatalf("source rows = %+v", response.Result.Source.RowAddresses)
	}
}

func TestMaterializeViewBuildsEntitySummaryTableAndStringSets(t *testing.T) {
	t.Parallel()
	snapshot, queryResult := compileAndExecuteViewFixture(t, entitySummaryTableViewSource())
	response := New(BuildInfo{}).MaterializeView(context.Background(), ViewMaterializationInput{
		Recipe:      snapshot.TypedAST.Views[0],
		Graph:       *snapshot.TypedAST.Graph,
		QueryResult: queryResult,
	})
	if response.Status != "ok" || response.Result == nil || response.Result.Table == nil {
		t.Fatalf("MaterializeView() = %+v", response)
	}
	if len(response.Result.Table.Rows) != 2 {
		t.Fatalf("rows = %+v", response.Result.Table.Rows)
	}
	first := response.Result.Table.Rows[0]
	if first.Key != "entity:ldl:project:p:entity:alpha" {
		t.Fatalf("first row = %+v", first)
	}
	if first.Cells[0].Value.Scalar == nil || first.Cells[0].Value.Scalar.String != "Alpha" {
		t.Fatalf("display cell = %+v", first.Cells[0])
	}
	if !reflect.DeepEqual(first.Cells[1].Value.StringSet, []string{"critical"}) {
		t.Fatalf("tags cell = %+v", first.Cells[1])
	}
}

func TestMaterializeViewBuildsRelationTableAndStateReads(t *testing.T) {
	t.Parallel()
	snapshot, queryResult := compileAndExecuteViewFixture(t, relationTableViewSource())
	response := New(BuildInfo{}).MaterializeView(context.Background(), ViewMaterializationInput{
		Recipe:      snapshot.TypedAST.Views[0],
		Graph:       *snapshot.TypedAST.Graph,
		QueryResult: queryResult,
	})
	if response.Status != "ok" || response.Result == nil || response.Result.Table == nil {
		t.Fatalf("MaterializeView() = %+v", response)
	}
	table := response.Result.Table
	if got := columnIDs(table.Columns); !reflect.DeepEqual(got, []string{"relation_id", "from", "protocol", "updated_at"}) {
		t.Fatalf("columns = %v", got)
	}
	if len(table.Rows) != 1 {
		t.Fatalf("rows = %+v", table.Rows)
	}
	row := table.Rows[0]
	if row.Key != "relation-row:ldl:project:p:relation:alpha_beta:row:primary" || row.SubjectAddress != "ldl:project:p:relation:alpha_beta" {
		t.Fatalf("row = %+v", row)
	}
	if row.Cells[0].Value.Scalar == nil || row.Cells[0].Value.Scalar.String != "alpha_beta" {
		t.Fatalf("relation id cell = %+v", row.Cells[0])
	}
	if row.Cells[1].Value.Address == nil || *row.Cells[1].Value.Address != "ldl:project:p:entity:alpha" {
		t.Fatalf("from endpoint cell = %+v", row.Cells[1])
	}
	if row.Cells[2].Value.Scalar == nil || row.Cells[2].Value.Scalar.String != "http" {
		t.Fatalf("protocol cell = %+v", row.Cells[2])
	}
	wantRead := StateReadRef{SubjectAddress: "ldl:project:p:relation:alpha_beta", FieldPath: "system.updated_at"}
	if !reflect.DeepEqual(row.Cells[3].StateReads, []StateReadRef{wantRead}) {
		t.Fatalf("cell state reads = %+v", row.Cells[3].StateReads)
	}
	if !reflect.DeepEqual(response.Result.Source.StateReads, []StateReadRef{wantRead}) {
		t.Fatalf("source state reads = %+v", response.Result.Source.StateReads)
	}
}

func TestMaterializeViewBuildsContextFacts(t *testing.T) {
	t.Parallel()
	snapshot, queryResult := compileAndExecuteViewFixture(t, contextViewSource())
	response := New(BuildInfo{}).MaterializeView(context.Background(), ViewMaterializationInput{
		Recipe:      snapshot.TypedAST.Views[0],
		Graph:       *snapshot.TypedAST.Graph,
		QueryResult: queryResult,
	})
	if response.Status != "ok" || response.Result == nil || response.Result.Context == nil {
		t.Fatalf("MaterializeView() = %+v", response)
	}
	if got := contextFactKeys(response.Result.Context.Facts); !reflect.DeepEqual(got, []string{
		"entity:ldl:project:p:entity:alpha",
		"entity-row:ldl:project:p:entity:alpha:row:primary",
		"entity:ldl:project:p:entity:beta",
		"entity-row:ldl:project:p:entity:beta:row:primary",
		"relation:ldl:project:p:relation:alpha_beta",
		"relation-row:ldl:project:p:relation:alpha_beta:row:primary",
	}) {
		t.Fatalf("facts = %v", got)
	}
	if got := len(response.Result.Context.Groups); got != 1 {
		t.Fatalf("groups = %+v", response.Result.Context.Groups)
	}
}

func TestMaterializeViewRejectsMismatchedQueryAndUnsupportedShape(t *testing.T) {
	t.Parallel()
	snapshot, queryResult := compileAndExecuteViewFixture(t, diagramViewSource())
	recipe := snapshot.TypedAST.Views[0]
	recipe.Source.Query.QueryAddress = "ldl:project:p:query:other"
	rejected := New(BuildInfo{}).MaterializeView(context.Background(), ViewMaterializationInput{Recipe: recipe, Graph: *snapshot.TypedAST.Graph, QueryResult: queryResult})
	if rejected.Status != "rejected" || len(rejected.Diagnostics) == 0 || rejected.Diagnostics[0].Code != "LDL1601" {
		t.Fatalf("mismatched query response = %+v", rejected)
	}
	snapshot, queryResult = compileAndExecuteViewFixture(t, diagramViewSource())
	unsupported := snapshot.TypedAST.Views[0]
	unsupported.Shape = view.Shape{Kind: view.ShapeMatrix}
	rejected = New(BuildInfo{}).MaterializeView(context.Background(), ViewMaterializationInput{Recipe: unsupported, Graph: *snapshot.TypedAST.Graph, QueryResult: queryResult})
	if rejected.Status != "rejected" || len(rejected.Diagnostics) == 0 || rejected.Diagnostics[0].Code != "LDL1701" {
		t.Fatalf("unsupported shape response = %+v", rejected)
	}
	required := snapshot.TypedAST.Views[0]
	required.StateRequirement = "required"
	rejected = New(BuildInfo{}).MaterializeView(context.Background(), ViewMaterializationInput{Recipe: required, Graph: *snapshot.TypedAST.Graph, QueryResult: queryResult})
	if rejected.Status != "rejected" || len(rejected.Diagnostics) == 0 || rejected.Diagnostics[0].Code != "LDL1604" {
		t.Fatalf("required state response = %+v", rejected)
	}
}

func TestMaterializeViewRejectsInvalidInputAndFallsBackToSeedSelection(t *testing.T) {
	t.Parallel()
	graphValue := TypedMasterGraph{
		Entities: []graph.Entity{{ID: "alpha", Address: "entity:alpha", DisplayName: "Alpha", TypeAddress: "type:service", LayerAddress: "layer:app"}},
	}
	recipe := CompiledViewRecipe{
		Address: "view:v", Category: "topology",
		Source: CompiledViewRecipe{}.Source,
		Shape:  view.Shape{Kind: view.ShapeDiagram, Diagram: &view.DiagramShape{}},
	}
	recipe.Source.Kind = view.SourceQuery
	recipe.Source.Query = &view.QuerySource{QueryAddress: "query:q"}
	queryResult := QueryResult{QueryAddress: "query:q", SeedEntityAddresses: []string{"entity:alpha"}, StateInput: QueryStateInputRef{Kind: "none"}}
	response := New(BuildInfo{}).MaterializeView(context.Background(), ViewMaterializationInput{Recipe: recipe, Graph: graphValue, QueryResult: queryResult})
	if response.Status != "ok" || response.Result == nil || len(response.Result.Diagram.Nodes) != 1 {
		t.Fatalf("fallback response = %+v", response)
	}
	if response.Result.Diagram.Nodes[0].EntityAddress != "entity:alpha" {
		t.Fatalf("fallback node = %+v", response.Result.Diagram.Nodes)
	}
	if rejected := New(BuildInfo{}).MaterializeView(nil, ViewMaterializationInput{Recipe: recipe, Graph: graphValue, QueryResult: queryResult}); rejected.Status != "rejected" || rejected.Diagnostics[0].Code != "LDL1801" {
		t.Fatalf("nil context response = %+v", rejected)
	}
	invalid := recipe
	invalid.Address = ""
	rejected := New(BuildInfo{}).MaterializeView(context.Background(), ViewMaterializationInput{Recipe: invalid, Graph: graphValue, QueryResult: queryResult})
	if rejected.Status != "rejected" || rejected.Diagnostics[0].Code != "LDL1701" {
		t.Fatalf("empty address response = %+v", rejected)
	}
	invalid = recipe
	invalid.Source.Kind = view.SourceDiff
	invalid.Source.Query = nil
	rejected = New(BuildInfo{}).MaterializeView(context.Background(), ViewMaterializationInput{Recipe: invalid, Graph: graphValue, QueryResult: queryResult})
	if rejected.Status != "rejected" || rejected.Diagnostics[0].Code != "LDL1701" {
		t.Fatalf("diff source response = %+v", rejected)
	}
	invalid = recipe
	queryResult.SeedEntityAddresses = []string{"entity:missing"}
	rejected = New(BuildInfo{}).MaterializeView(context.Background(), ViewMaterializationInput{Recipe: invalid, Graph: graphValue, QueryResult: queryResult})
	if rejected.Status != "rejected" || rejected.Diagnostics[0].Code != "LDL1601" {
		t.Fatalf("missing entity response = %+v", rejected)
	}
	queryResult = QueryResult{QueryAddress: "query:q", SelectedRelationAddresses: []string{"relation:missing"}, StateInput: QueryStateInputRef{Kind: "none"}}
	rejected = New(BuildInfo{}).MaterializeView(context.Background(), ViewMaterializationInput{Recipe: invalid, Graph: graphValue, QueryResult: queryResult})
	if rejected.Status != "rejected" || rejected.Diagnostics[0].Code != "LDL1601" {
		t.Fatalf("missing relation response = %+v", rejected)
	}
}

func compileAndExecuteViewFixture(t *testing.T, source string) (Snapshot, QueryResult) {
	t.Helper()
	snapshot := compileQueryExecutionFixture(t, source)
	if len(snapshot.TypedAST.Views) != 1 {
		t.Fatalf("views = %+v", snapshot.TypedAST.Views)
	}
	queryResponse, err := New(BuildInfo{}).ExecuteQuery(context.Background(), QueryExecutionInput{
		Recipe: snapshot.TypedAST.Queries[0],
		Graph:  *snapshot.TypedAST.Graph,
		Arguments: map[string]TypedScalar{
			"ldl:project:p:query:prod_scope:parameter:environment": {Type: definition.ScalarEnum, String: "prod"},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if queryResponse.Status != "ok" || queryResponse.Result == nil {
		t.Fatalf("ExecuteQuery() = %+v", queryResponse)
	}
	return snapshot, *queryResponse.Result
}

func diagramViewSource() string {
	return structuralQuerySource() + `
view topology "Topology" topology {
  source query prod_scope { environment: prod }
  diagram {
    layout manual
    direction left_to_right
    abstraction normal
    place alpha 10 20 200 100
    place gamma 30 40 200 100
  }
}
`
}

func tableViewSource() string {
	return structuralQuerySource() + `
view inventory "Inventory" inventory {
  source query prod_scope { environment: prod }
  table {
    rows entity_rows
    entity_types [service]
    entity_id
    type
    layer
    column environment {
      source attribute environment entity_types [service]
    }
    column outgoing {
      source derived_count outgoing relations [calls]
    }
  }
}
`
}

func entitySummaryTableViewSource() string {
	return structuralQuerySource() + `
view inventory "Inventory" inventory {
  source query prod_scope { environment: prod }
  table {
    rows entity
    entity_types [service]
    column display_name {
      source field display_name
    }
    column tags {
      source field tags
    }
  }
}
`
}

func relationTableViewSource() string {
	return structuralQuerySource() + `
view dependencies "Dependencies" dependency {
  state_input optional
  source query prod_scope { environment: prod }
  table {
    rows relation_rows
    column relation_id {
      source field id
    }
    column from {
      source relation_endpoint from id
    }
    column protocol {
      source attribute protocol relation_types [calls]
    }
    column updated_at {
      source state system.updated_at
    }
  }
}
`
}

func contextViewSource() string {
	return structuralQuerySource() + `
view context "Context" context {
  source query prod_scope { environment: prod }
  context {
    group_by layer
    entity_rows
    relation_rows
    outgoing
  }
}
`
}

func nodeAddresses(nodes []DiagramNode) []string {
	values := make([]string, len(nodes))
	for i, node := range nodes {
		values[i] = node.EntityAddress
	}
	return values
}

func edgeAddresses(edges []DiagramEdge) []string {
	values := make([]string, len(edges))
	for i, edge := range edges {
		values[i] = edge.RelationAddress
	}
	return values
}

func columnIDs(columns []TableViewColumn) []string {
	values := make([]string, len(columns))
	for i, column := range columns {
		values[i] = column.ID
	}
	return values
}

func contextFactKeys(facts []ContextFact) []string {
	values := make([]string, len(facts))
	for i, fact := range facts {
		values[i] = fact.Key
	}
	return values
}
