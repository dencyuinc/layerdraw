// SPDX-License-Identifier: LicenseRef-LayerDraw-1.0

package engine

import (
	"context"
	"reflect"
	"strings"
	"testing"

	"github.com/dencyuinc/layerdraw/internal/engine/internal/compiler/semantic/definition"
)

func TestMaterializeViewBuildsCommonDiagramDomainDeterministically(t *testing.T) {
	t.Parallel()
	snapshot, queryResult := compileAndExecuteViewFixture(t, diagramViewSource())
	first := materializeQueryView(t, snapshot, queryResult, nil)
	second := materializeQueryView(t, snapshot, queryResult, nil)
	if !reflect.DeepEqual(first, second) {
		t.Fatal("repeated View materialization is not deterministic")
	}
	base, ok := first.Base()
	if !ok || base.Kind != ViewDataDiagram || base.Shape.Diagram == nil || base.ProjectAddress != "ldl:project:p" || base.ViewAddress != "ldl:project:p:view:topology" {
		t.Fatalf("base = %+v ok=%v", base, ok)
	}
	if base.Revision.Single == nil || base.Revision.Diff != nil || base.Revision.Single.RevisionID != "revision-1" || base.Revision.Single.DefinitionHash != snapshot.DefinitionHash {
		t.Fatalf("revision = %+v", base.Revision)
	}
	if first.Diagram == nil || len(first.Diagram.Occurrences) != 2 || len(first.Diagram.Edges) != 1 {
		t.Fatalf("diagram = %+v", first.Diagram)
	}
	if got := occurrenceAddresses(first.Diagram.Occurrences); !reflect.DeepEqual(got, []string{"ldl:project:p:entity:alpha", "ldl:project:p:entity:beta"}) {
		t.Fatalf("occurrences = %v", got)
	}
	for _, occurrence := range first.Diagram.Occurrences {
		if !strings.HasPrefix(occurrence.Key, "vdi:diagram-occurrence:") || len(occurrence.Source.SubjectAddresses) == 0 {
			t.Fatalf("occurrence identity/source = %+v", occurrence)
		}
	}
	if len(base.Source.EntityAddresses) != 2 || len(base.Source.RelationAddresses) != 1 || len(base.Source.CellRefs) != 5 {
		t.Fatalf("complete source = %+v", base.Source)
	}
	assertRequiredSourceCollections(t, base.Source)
}

func TestMaterializeViewBuildsTypedTableRowsAndCompleteCellSources(t *testing.T) {
	t.Parallel()
	snapshot, queryResult := compileAndExecuteViewFixture(t, tableViewSource())
	result := materializeQueryView(t, snapshot, queryResult, nil)
	if result.Table == nil || len(result.Table.Rows) != 2 {
		t.Fatalf("table = %+v", result.Table)
	}
	if got := tableColumnIDs(result.Table.Columns); !reflect.DeepEqual(got, []string{"entity_id", "entity_type", "entity_layer", "environment", "outgoing"}) {
		t.Fatalf("columns = %v", got)
	}
	first := result.Table.Rows[0]
	if !strings.HasPrefix(first.Key, "vdi:table-row:") || len(first.Cells) != len(result.Table.Columns) {
		t.Fatalf("first row = %+v", first)
	}
	environment := tableCellByID(t, *result.Table, first, "environment")
	if !environment.Present || environment.Value == nil || environment.Value.Scalar == nil || environment.Value.Scalar.String != "prod" {
		t.Fatalf("environment cell = %+v", environment)
	}
	wantCell := ViewDataCellRef{RowAddress: "ldl:project:p:entity:alpha:row:primary", ColumnAddress: "ldl:project:p:entity-type:service:column:environment"}
	if !reflect.DeepEqual(environment.Source.CellRefs, []ViewDataCellRef{wantCell}) {
		t.Fatalf("environment source = %+v", environment.Source)
	}
	outgoing := tableCellByID(t, *result.Table, first, "outgoing")
	if outgoing.Value == nil || outgoing.Value.Scalar == nil || outgoing.Value.Scalar.Int != 1 || len(outgoing.Source.RelationAddresses) != 1 {
		t.Fatalf("derived cell = %+v", outgoing)
	}
}

func TestMaterializeViewTracksOptionalDirectStateReadsWithoutInventingValues(t *testing.T) {
	t.Parallel()
	snapshot, queryResult := compileAndExecuteViewFixture(t, relationTableViewSource())
	result := materializeQueryView(t, snapshot, queryResult, nil)
	base, _ := result.Base()
	want := StateReadRef{SubjectAddress: "ldl:project:p:relation:alpha_beta:row:primary", FieldPath: "system.updated_at"}
	if base.StatePolicy != "optional" || base.StateInput.Kind != "none" || !reflect.DeepEqual(base.Source.State.Reads, []StateReadRef{want}) {
		t.Fatalf("state base = %+v", base)
	}
	cell := tableCellByID(t, *result.Table, result.Table.Rows[0], "updated_at")
	if cell.Present || cell.Value != nil || !reflect.DeepEqual(cell.Source.State.Reads, []StateReadRef{want}) {
		t.Fatalf("state cell = %+v", cell)
	}
}

func TestMaterializeViewBuildsNestedContextGroups(t *testing.T) {
	t.Parallel()
	snapshot, queryResult := compileAndExecuteViewFixture(t, contextViewSource())
	result := materializeQueryView(t, snapshot, queryResult, nil)
	if result.Context == nil || len(result.Context.Groups) != 1 {
		t.Fatalf("context = %+v", result.Context)
	}
	group := result.Context.Groups[0]
	if group.Label != "Application" || len(group.Facts) != 2 || len(group.Attributes) != 2 {
		t.Fatalf("group = %+v", group)
	}
	fact := group.Facts[0]
	if fact.Direction != ContextFactOutgoing || fact.EntityAddress != "ldl:project:p:entity:alpha" || fact.RelationAddress != "ldl:project:p:relation:alpha_beta" {
		t.Fatalf("fact = %+v", fact)
	}
	if !strings.HasPrefix(fact.Key, "vdi:context-fact:") || len(fact.Source.RelationAddresses) != 1 {
		t.Fatalf("fact identity/source = %+v", fact)
	}
}

func TestViewDataUnionDefinesEveryClosedShape(t *testing.T) {
	t.Parallel()
	base := ViewDataBase{Kind: ViewDataMatrix}
	values := []ViewData{
		{Diagram: &DiagramViewData{ViewDataBase: ViewDataBase{Kind: ViewDataDiagram}}},
		{Table: &TableViewData{ViewDataBase: ViewDataBase{Kind: ViewDataTable}}},
		{Matrix: &MatrixViewData{ViewDataBase: base}},
		{Tree: &TreeViewData{ViewDataBase: ViewDataBase{Kind: ViewDataTree}}},
		{Flow: &FlowViewData{ViewDataBase: ViewDataBase{Kind: ViewDataFlow}}},
		{Context: &ContextViewData{ViewDataBase: ViewDataBase{Kind: ViewDataContext}}},
		{Diff: &DiffViewData{ViewDataBase: ViewDataBase{Kind: ViewDataDiff}}},
	}
	for index, value := range values {
		if _, ok := value.Base(); !ok {
			t.Fatalf("variant %d is not closed", index)
		}
	}
	values[0].Table = &TableViewData{}
	if _, ok := values[0].Base(); ok {
		t.Fatal("multi-variant ViewData was accepted")
	}
	if _, ok := (ViewData{Diagram: &DiagramViewData{ViewDataBase: ViewDataBase{Kind: ViewDataTable}}}).Base(); ok {
		t.Fatal("variant with mismatched embedded kind was accepted")
	}
}

func TestMaterializeViewFailsClosedForSourceRevisionAndStateMismatches(t *testing.T) {
	t.Parallel()
	snapshot, queryResult := compileAndExecuteViewFixture(t, diagramViewSource())
	recipe := snapshot.TypedAST.Views[0]
	engine := New(BuildInfo{})
	cases := []ViewMaterializationInput{
		{Recipe: recipe},
		{Recipe: recipe, Query: &QueryViewMaterializationInput{RevisionID: "", Snapshot: snapshot, QueryResult: queryResult}},
		{Recipe: recipe, Query: &QueryViewMaterializationInput{RevisionID: "revision-1", Snapshot: snapshot, QueryResult: queryResult}, Diff: &DiffViewMaterializationInput{}},
	}
	for index, input := range cases {
		response := engine.MaterializeView(context.Background(), input)
		if response.Status != "rejected" || len(response.Diagnostics) == 0 {
			t.Fatalf("case %d = %+v", index, response)
		}
	}
	foreign := compileViewFixture(t, strings.Replace(diagramViewSource(), `"Topology"`, `"Foreign topology"`, 1))
	response := engine.MaterializeView(context.Background(), ViewMaterializationInput{
		Recipe: recipe, Query: &QueryViewMaterializationInput{RevisionID: "revision-1", Snapshot: foreign, QueryResult: queryResult},
	})
	if response.Status != "rejected" || response.Diagnostics[0].Code != "LDL1801" {
		t.Fatalf("foreign revision = %+v", response)
	}

	requiredSnapshot, requiredResult := compileAndExecuteViewFixture(t, strings.Replace(relationTableViewSource(), "state_input optional", "state_input required", 1))
	state := validStateQuerySnapshot(t, requiredSnapshot, []StateQuerySubject{stateSubject(
		"ldl:project:p:relation:alpha_beta:row:primary", stateFields("system.updated_at", datetimeScalar("2026-01-04T00:00:00Z")),
	)})
	ok := materializeQueryView(t, requiredSnapshot, requiredResult, &state)
	base, _ := ok.Base()
	if base.StateInput.Kind != "snapshot" || base.StateInput.SnapshotHash == "" {
		t.Fatalf("required state = %+v", base.StateInput)
	}
	missing := New(BuildInfo{}).MaterializeView(context.Background(), queryViewInput(requiredSnapshot, requiredResult, nil))
	if missing.Status != "rejected" || missing.Diagnostics[0].Code != "LDL1604" {
		t.Fatalf("missing required state = %+v", missing)
	}
}

func TestMaterializeViewValidatesDiffInputBeforeUnimplementedProjection(t *testing.T) {
	t.Parallel()
	snapshot := compileViewFixture(t, `project p "Project" {}
view changes "Changes" diff {
  source diff "before" -> "after" {}
  diff {}
}
`)
	recipe := snapshot.TypedAST.Views[0]
	valid := ViewMaterializationInput{Recipe: recipe, Diff: &DiffViewMaterializationInput{
		RecipeRevisionID: "recipe-1", RecipeSnapshot: snapshot,
		BeforeRevisionID: "before-1", BeforeSnapshot: snapshot,
		AfterRevisionID: "after-1", AfterSnapshot: snapshot,
	}}
	response := New(BuildInfo{}).MaterializeView(context.Background(), valid)
	if response.Status != "rejected" || len(response.Diagnostics) != 1 || response.Diagnostics[0].Code != "LDL1701" {
		t.Fatalf("valid but unimplemented Diff = %+v", response)
	}
	invalid := valid
	invalid.Diff.AfterRevisionID = ""
	response = New(BuildInfo{}).MaterializeView(context.Background(), invalid)
	if response.Status != "rejected" || response.Diagnostics[0].Code != "LDL1801" {
		t.Fatalf("invalid Diff = %+v", response)
	}
}

func materializeQueryView(t *testing.T, snapshot Snapshot, result QueryResult, state *StateQuerySnapshot) ViewData {
	t.Helper()
	response := New(BuildInfo{}).MaterializeView(context.Background(), queryViewInput(snapshot, result, state))
	if response.Status != "ok" || response.Result == nil {
		t.Fatalf("MaterializeView() = %+v", response)
	}
	return *response.Result
}

func queryViewInput(snapshot Snapshot, result QueryResult, state *StateQuerySnapshot) ViewMaterializationInput {
	return ViewMaterializationInput{
		Recipe: snapshot.TypedAST.Views[0],
		Query:  &QueryViewMaterializationInput{RevisionID: "revision-1", Snapshot: snapshot, QueryResult: result, StateSnapshot: state},
	}
}

func compileAndExecuteViewFixture(t *testing.T, source string) (Snapshot, QueryResult) {
	t.Helper()
	snapshot := compileQueryExecutionFixture(t, source)
	if len(snapshot.TypedAST.Views) != 1 {
		t.Fatalf("views = %+v", snapshot.TypedAST.Views)
	}
	queryResponse, err := New(BuildInfo{}).ExecuteQuery(context.Background(), QueryExecutionInput{
		Recipe: snapshot.TypedAST.Queries[0], Graph: *snapshot.TypedAST.Graph, Definition: snapshot.QueryDefinitionIdentity(),
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

func compileViewFixture(t *testing.T, source string) Snapshot {
	t.Helper()
	result, err := New(BuildInfo{}).Compile(context.Background(), CompileInput{
		Mode: CompileProject, EntryPath: "document.ldl", ProjectSourceTree: map[string][]byte{"document.ldl": []byte(source)},
		ResolvedDependencies: ResolvedDependencies{Format: "layerdraw-resolved", FormatVersion: 1, Language: 1},
	})
	if err != nil {
		t.Fatal(err)
	}
	snapshot := result.Snapshot()
	if len(snapshot.Diagnostics) != 0 || len(snapshot.TypedAST.Views) != 1 {
		t.Fatalf("compile = %+v", snapshot.Diagnostics)
	}
	return snapshot
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
    place beta 250 20 200 100
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

func occurrenceAddresses(values []DiagramOccurrence) []string {
	out := make([]string, len(values))
	for index, value := range values {
		out[index] = value.EntityAddress
	}
	return out
}

func tableColumnIDs(values []TableColumn) []string {
	out := make([]string, len(values))
	for index, value := range values {
		out[index] = value.ID
	}
	return out
}

func tableCellByID(t *testing.T, table TableViewData, row TableRow, id string) TableCell {
	t.Helper()
	for _, column := range table.Columns {
		if column.ID == id {
			cell, ok := row.Cells[column.Key]
			if !ok {
				t.Fatalf("row lacks cell %s", id)
			}
			return cell
		}
	}
	t.Fatalf("column %s not found", id)
	return TableCell{}
}

func assertRequiredSourceCollections(t *testing.T, source ViewDataSourceRefs) {
	t.Helper()
	if source.SubjectAddresses == nil || source.EntityAddresses == nil || source.RelationAddresses == nil || source.LayerAddresses == nil ||
		source.RowAddresses == nil || source.CellRefs == nil || source.AssetDigests == nil || source.State.Reads == nil {
		t.Fatalf("required source collection is nil: %+v", source)
	}
}
