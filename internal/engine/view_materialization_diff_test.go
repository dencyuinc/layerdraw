// SPDX-License-Identifier: LicenseRef-LayerDraw-1.0

package engine

import (
	"context"
	"reflect"
	"strings"
	"testing"

	"github.com/dencyuinc/layerdraw/internal/engine/internal/compiler/materialize"
	"github.com/dencyuinc/layerdraw/internal/engine/internal/compiler/view"
)

func TestMaterializeDiffDerivesTypedChangesAndSideSourcesDeterministically(t *testing.T) {
	t.Parallel()
	before := compileViewFixture(t, structuralDiffViewSource(false, "entity, entity_row"))
	afterSource := strings.Replace(structuralDiffViewSource(false, "entity, entity_row"), `beta "Beta"`, "beta \"Beta updated\"\n  delta \"Delta\"", 1)
	afterSource = strings.Replace(afterSource, "alpha primary: prod, 75", "alpha primary: prod, 80", 1)
	after := compileViewFixture(t, afterSource)

	first := materializeDiffView(t, after, before, after, nil, nil)
	second := materializeDiffView(t, after, before, after, nil, nil)
	if !reflect.DeepEqual(first, second) {
		t.Fatal("repeated Diff materialization is not deterministic")
	}
	if first.Diff == nil || len(first.Diff.Changes) != 3 {
		t.Fatalf("changes = %+v", first.Diff)
	}
	beta := diffChangeByAddress(t, first.Diff.Changes, "ldl:project:p:entity:beta")
	if beta.Kind != DiffChangeUpdated || !reflect.DeepEqual(diffFieldPaths(beta.Fields), [][]string{{"display_name"}}) || beta.BeforeSource == nil || beta.AfterSource == nil {
		t.Fatalf("updated Entity = %+v", beta)
	}
	delta := diffChangeByAddress(t, first.Diff.Changes, "ldl:project:p:entity:delta")
	if delta.Kind != DiffChangeAdded || delta.BeforeAddress != nil || delta.BeforeSource != nil || delta.AfterAddress == nil || delta.AfterSource == nil || len(delta.Fields) == 0 {
		t.Fatalf("added Entity = %+v", delta)
	}
	row := diffChangeByAddress(t, first.Diff.Changes, "ldl:project:p:entity:alpha:row:primary")
	capacity := "ldl:project:p:entity-type:service:column:capacity"
	if row.Kind != DiffChangeUpdated || !reflect.DeepEqual(diffFieldPaths(row.Fields), [][]string{{"values", capacity}}) {
		t.Fatalf("updated row = %+v", row)
	}
	if row.BeforeSource == nil || row.AfterSource == nil || !reflect.DeepEqual(row.Source.CellRefs, []ViewDataCellRef{
		{RowAddress: rowResultAddress(row), ColumnAddress: capacity},
		{RowAddress: rowResultAddress(row), ColumnAddress: "ldl:project:p:entity-type:service:column:environment"},
	}) {
		t.Fatalf("row source = %+v", row.Source)
	}
	for _, change := range first.Diff.Changes {
		if !strings.HasPrefix(change.Key, "vdi:diff-change:") {
			t.Fatalf("change key = %q", change.Key)
		}
		for _, field := range change.Fields {
			if !strings.HasPrefix(field.Key, "vdi:diff-field:") {
				t.Fatalf("field key = %q", field.Key)
			}
		}
	}
	base, ok := first.Base()
	if !ok || base.QueryAddress != nil || base.Revision.Diff == nil || base.StateInput.Kind != "none" || len(base.Source.EntityAddresses) != 3 || base.Source.State.Reads == nil {
		t.Fatalf("Diff base = %+v ok=%v", base, ok)
	}
}

func TestMaterializeDiffUsesOnlyExplicitMoveAuthority(t *testing.T) {
	t.Parallel()
	before := compileViewFixture(t, moveDiffViewSource("alpha", "Alpha", false))
	withoutMove := compileViewFixture(t, moveDiffViewSource("omega", "Alpha", false))
	plain := materializeDiffView(t, withoutMove, before, withoutMove, nil, nil)
	if plain.Diff == nil || len(plain.Diff.Changes) != 2 || plain.Diff.Changes[0].Kind != DiffChangeRemoved || plain.Diff.Changes[1].Kind != DiffChangeAdded {
		t.Fatalf("heuristic identity pairing occurred: %+v", plain.Diff)
	}

	after := compileViewFixture(t, moveDiffViewSource("omega", "Alpha", true))
	moved := materializeDiffView(t, after, before, after, nil, nil)
	if moved.Diff == nil || len(moved.Diff.Changes) != 1 {
		t.Fatalf("moved Diff = %+v", moved.Diff)
	}
	change := moved.Diff.Changes[0]
	if change.Kind != DiffChangeMoved || change.BeforeAddress == nil || *change.BeforeAddress != "ldl:project:p:entity:alpha" || change.AfterAddress == nil || *change.AfterAddress != "ldl:project:p:entity:omega" || len(change.Fields) != 0 {
		t.Fatalf("explicit move = %+v", change)
	}

	updatedSource := strings.Replace(moveDiffViewSource("omega", "Alpha", true), `omega "Alpha"`, `omega "Omega"`, 1)
	updated := compileViewFixture(t, updatedSource)
	movedUpdated := materializeDiffView(t, updated, before, updated, nil, nil).Diff.Changes[0]
	if movedUpdated.Kind != DiffChangeMovedUpdated || !reflect.DeepEqual(diffFieldPaths(movedUpdated.Fields), [][]string{{"display_name"}}) {
		t.Fatalf("moved and updated = %+v", movedUpdated)
	}
}

func TestMaterializeDiffAcceptsExplicitProjectMigrationAndRejectsHistoryRegression(t *testing.T) {
	t.Parallel()
	before := compileViewFixture(t, projectMoveDiffViewSource("p", false))
	after := compileViewFixture(t, projectMoveDiffViewSource("q", true))
	result := materializeDiffView(t, after, before, after, nil, nil)
	if result.Diff == nil || len(result.Diff.Changes) != 2 {
		t.Fatalf("Project migration Diff = %+v", result.Diff)
	}
	for _, change := range result.Diff.Changes {
		if change.Kind != DiffChangeMoved || change.BeforeAddress == nil || change.AfterAddress == nil {
			t.Fatalf("Project migration change = %+v", change)
		}
	}
	base, _ := result.Base()
	if base.ProjectAddress != "ldl:project:q" {
		t.Fatalf("Project migration base = %+v", base)
	}

	regressed := compileViewFixture(t, projectMoveDiffViewSource("q", false))
	response := New(BuildInfo{}).MaterializeView(context.Background(), diffViewInput(regressed, after, regressed, nil, nil))
	if response.Status != "rejected" || len(response.Diagnostics) == 0 || !strings.Contains(response.Diagnostics[0].Message, "extends before identity history") {
		t.Fatalf("identity history regression = %+v", response)
	}
}

func TestMaterializeDiffDoesNotCascadeMoveIntoStableReferencers(t *testing.T) {
	t.Parallel()
	before := compileViewFixture(t, relationMoveDiffViewSource("alpha", false))
	after := compileViewFixture(t, relationMoveDiffViewSource("omega", true))
	result := materializeDiffView(t, after, before, after, nil, nil)
	if result.Diff == nil || len(result.Diff.Changes) != 1 || result.Diff.Changes[0].SubjectKind != "entity" || result.Diff.Changes[0].Kind != DiffChangeMoved {
		t.Fatalf("move referencer cascade = %+v", result.Diff)
	}
}

func TestMaterializeDiffExtendsTransitiveMoveClosure(t *testing.T) {
	t.Parallel()
	before := compileViewFixture(t, chainedMoveDiffViewSource("beta", "entity alpha -> beta"))
	after := compileViewFixture(t, chainedMoveDiffViewSource("gamma", "entity alpha -> beta\n  entity beta -> gamma"))
	result := materializeDiffView(t, after, before, after, nil, nil)
	if result.Diff == nil || len(result.Diff.Changes) != 1 || result.Diff.Changes[0].Kind != DiffChangeMoved || result.Diff.Changes[0].BeforeAddress == nil || *result.Diff.Changes[0].BeforeAddress != "ldl:project:p:entity:beta" || result.Diff.Changes[0].AfterAddress == nil || *result.Diff.Changes[0].AfterAddress != "ldl:project:p:entity:gamma" {
		t.Fatalf("transitive move closure = %+v", result.Diff)
	}
}

func TestApplyDiffMoveClosureRejectsCycle(t *testing.T) {
	t.Parallel()
	_, _, err := applyDiffMoveClosure("ldl:project:p:entity:alpha", map[string]string{
		"ldl:project:p:entity:alpha": "ldl:project:p:entity:beta",
		"ldl:project:p:entity:beta":  "ldl:project:p:entity:alpha",
	})
	if err == nil || !strings.Contains(err.Error(), "cycle") {
		t.Fatalf("move closure cycle error = %v", err)
	}
}

func TestMaterializeDiffSupportsQueryScopedComparison(t *testing.T) {
	t.Parallel()
	before, beforeResult := compileAndExecuteViewFixture(t, queryDiffViewSource())
	afterSource := strings.Replace(queryDiffViewSource(), `beta "Beta"`, `beta "Beta updated"`, 1)
	after, afterResult := compileAndExecuteViewFixture(t, afterSource)
	result := materializeDiffView(t, after, before, after, &beforeResult, &afterResult)
	if result.Diff == nil || len(result.Diff.Changes) != 1 {
		t.Fatalf("query-scoped Diff = %+v", result.Diff)
	}
	change := result.Diff.Changes[0]
	if change.SubjectKind != "entity" || change.Kind != DiffChangeUpdated || diffResultAddress(change) != "ldl:project:p:entity:beta" {
		t.Fatalf("query-scoped change = %+v", change)
	}
	base, _ := result.Base()
	if base.QueryAddress == nil || *base.QueryAddress != "ldl:project:p:query:prod_scope" || len(base.Source.RelationAddresses) != 0 {
		t.Fatalf("query-scoped base = %+v", base)
	}
}

func TestMaterializeDiffPairsExplicitMoveWhenOnlyAfterQuerySelectsSubject(t *testing.T) {
	t.Parallel()
	before := compileViewFixture(t, queryMoveDiffViewSource("alpha", false, false))
	after := compileViewFixture(t, queryMoveDiffViewSource("omega", true, true))
	beforeResult := executeDiffQuery(t, before)
	afterResult := executeDiffQuery(t, after)
	result := materializeDiffView(t, after, before, after, &beforeResult, &afterResult)
	if result.Diff == nil || len(result.Diff.Changes) != 1 || result.Diff.Changes[0].Kind != DiffChangeMoved {
		t.Fatalf("one-sided Query move = %+v", result.Diff)
	}
}

func TestDiffFieldLeavesRetainEmptyMapPresence(t *testing.T) {
	t.Parallel()
	fields := []FieldDiff{}
	diffFieldMapLeaves(nil, nil, map[string]any{"annotations": map[string]any{}}, true, &fields)
	if len(fields) != 1 || !reflect.DeepEqual(fields[0].Path, []string{"annotations"}) || fields[0].BeforePresent || !fields[0].AfterPresent || fields[0].After == nil || fields[0].After.Kind != SemanticValueMap {
		t.Fatalf("empty map presence = %+v", fields)
	}
}

func TestMaterializeDiffRetainsDeclaredColumnOrderAsOwnerSemantics(t *testing.T) {
	t.Parallel()
	before := compileViewFixture(t, orderedColumnDiffViewSource("first", "second"))
	after := compileViewFixture(t, orderedColumnDiffViewSource("second", "first"))
	result := materializeDiffView(t, after, before, after, nil, nil)
	if result.Diff == nil || len(result.Diff.Changes) != 1 {
		t.Fatalf("column-order Diff = %+v", result.Diff)
	}
	change := result.Diff.Changes[0]
	if change.SubjectKind != "entity_type" || change.Kind != DiffChangeUpdated || !reflect.DeepEqual(diffFieldPaths(change.Fields), [][]string{{"column_order"}}) {
		t.Fatalf("column-order change = %+v", change)
	}
}

func TestMaterializeDiffKeepsTableColumnContentOutOfViewOwnerFields(t *testing.T) {
	t.Parallel()
	before := compileQueryExecutionFixture(t, tableColumnChildDiffViewSource("Environment"))
	after := compileQueryExecutionFixture(t, tableColumnChildDiffViewSource("Deployment environment"))
	result := materializeDiffView(t, after, before, after, nil, nil)
	if result.Diff == nil || len(result.Diff.Changes) != 1 {
		t.Fatalf("table-column child Diff = %+v", result.Diff)
	}
	change := result.Diff.Changes[0]
	if change.SubjectKind != "view_table_column" || change.Kind != DiffChangeUpdated || !reflect.DeepEqual(diffFieldPaths(change.Fields), [][]string{{"label"}}) {
		t.Fatalf("table-column child change = %+v", change)
	}
}

func TestMaterializeDiffRejectsInvalidBaselinesProjectsIncludesAndMoves(t *testing.T) {
	t.Parallel()
	validBefore := compileViewFixture(t, moveDiffViewSource("alpha", "Alpha", false))
	validAfter := compileViewFixture(t, moveDiffViewSource("omega", "Alpha", true))

	cases := []struct {
		name   string
		mutate func(*ViewMaterializationInput)
	}{
		{
			name: "definition hash mismatch",
			mutate: func(input *ViewMaterializationInput) {
				input.Diff.BeforeSnapshot.DefinitionHash = "sha256:" + strings.Repeat("f", 64)
			},
		},
		{
			name: "unsupported subject kind",
			mutate: func(input *ViewMaterializationInput) {
				input.Diff.BeforeSnapshot.SemanticIndex.Subjects[0].Kind = "unsupported"
			},
		},
		{
			name: "non-canonical include",
			mutate: func(input *ViewMaterializationInput) {
				input.Recipe.Shape.Diff.Include = []view.DiffSubjectKind{view.DiffEntity, view.DiffEntity}
				input.Diff.RecipeSnapshot.TypedAST.Views[0] = input.Recipe
			},
		},
		{
			name: "malformed move closure",
			mutate: func(input *ViewMaterializationInput) {
				document := input.Diff.AfterSnapshot.NormalizedDocument
				document.Identity.MoveClosure = append(document.Identity.MoveClosure, materialize.MoveResolution{
					Kind: materialize.SubjectEntity, SourceAddress: "ldl:project:p:entity:bad", TerminalAddress: "ldl:project:p",
				})
				input.Diff.AfterSnapshot.CanonicalJSON, _ = materialize.Canonicalize(*document)
				input.Diff.AfterSnapshot.DefinitionHash, _ = diffDefinitionHash(*document)
			},
		},
	}
	for _, test := range cases {
		test := test
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			input := diffViewInput(validAfter, validBefore, validAfter, nil, nil)
			test.mutate(&input)
			first := New(BuildInfo{}).MaterializeView(context.Background(), input)
			second := New(BuildInfo{}).MaterializeView(context.Background(), input)
			if first.Status != "rejected" || len(first.Diagnostics) == 0 || first.Diagnostics[0].Code != "LDL1801" || !reflect.DeepEqual(first, second) {
				t.Fatalf("rejection = %+v second=%+v", first, second)
			}
		})
	}

	foreign := compileViewFixture(t, strings.ReplaceAll(moveDiffViewSource("alpha", "Alpha", false), "project p", "project q"))
	response := New(BuildInfo{}).MaterializeView(context.Background(), diffViewInput(validAfter, validBefore, foreign, nil, nil))
	if response.Status != "rejected" || len(response.Diagnostics) == 0 || !strings.Contains(response.Diagnostics[0].Message, "incompatible Projects") {
		t.Fatalf("foreign Project = %+v", response)
	}
}

func TestMaterializeDiffRejectsIncompatibleQueryParameters(t *testing.T) {
	t.Parallel()
	before, beforeResult := compileAndExecuteViewFixture(t, queryDiffViewSource())
	afterSource := strings.Replace(queryDiffViewSource(), "environment enum [prod, stg] required default prod", "environment enum [prod, stg, dev] required default prod", 1)
	after, afterResult := compileAndExecuteViewFixture(t, afterSource)
	response := New(BuildInfo{}).MaterializeView(context.Background(), diffViewInput(before, before, after, &beforeResult, &afterResult))
	if response.Status != "rejected" || len(response.Diagnostics) == 0 || !strings.Contains(response.Diagnostics[0].Message, "parameter definitions are incompatible") {
		t.Fatalf("incompatible Query = %+v", response)
	}
}

func materializeDiffView(t *testing.T, recipe, before, after Snapshot, beforeResult, afterResult *QueryResult) ViewData {
	t.Helper()
	response := New(BuildInfo{}).MaterializeView(context.Background(), diffViewInput(recipe, before, after, beforeResult, afterResult))
	if response.Status != "ok" || response.Result == nil {
		t.Fatalf("MaterializeView(Diff) = %+v", response)
	}
	return *response.Result
}

func diffViewInput(recipe, before, after Snapshot, beforeResult, afterResult *QueryResult) ViewMaterializationInput {
	recipe = deepClone(recipe)
	before = deepClone(before)
	after = deepClone(after)
	beforeResult = deepClone(beforeResult)
	afterResult = deepClone(afterResult)
	return ViewMaterializationInput{
		Recipe: recipe.TypedAST.Views[0],
		Diff: &DiffViewMaterializationInput{
			RecipeRevisionID: "recipe-1", RecipeSnapshot: recipe,
			BeforeRevisionID: "before-1", BeforeSnapshot: before,
			AfterRevisionID: "after-1", AfterSnapshot: after,
			BeforeQueryResult: beforeResult, AfterQueryResult: afterResult,
		},
	}
}

func structuralDiffViewSource(detectMoves bool, include string) string {
	detect := ""
	if detectMoves {
		detect = "\n    detect_moves"
	}
	return structuralQuerySource() + `
view changes "Changes" diff {
  source diff "before" -> "after" {}
  diff {
    include [` + include + `]` + detect + `
  }
}
`
}

func moveDiffViewSource(entityID, displayName string, withMove bool) string {
	moves := ""
	if withMove {
		moves = `
moves {
  entity alpha -> omega
}
`
	}
	return `
project p "Project" {}
layers {
  app "Application" @10
}
entity_type service "Service" {
  representation shape rect
}
entities service @app {
  ` + entityID + ` "` + displayName + `"
}
` + moves + `
view changes "Changes" diff {
  source diff "before" -> "after" {}
  diff {
    include [entity]
    detect_moves
  }
}
`
}

func queryDiffViewSource() string {
	return structuralQuerySource() + `
view changes "Changes" diff {
  source diff "before" -> "after" {
    query prod_scope
    arguments { environment: prod }
  }
  diff {
    include [entity]
  }
}
`
}

func projectMoveDiffViewSource(projectID string, withMove bool) string {
	moves := ""
	if withMove {
		moves = `
moves {
  project p -> q
}
`
	}
	return `
project ` + projectID + ` "Project" {}
layers {
  app "Application" @10
}
entity_type service "Service" {
  representation shape rect
}
entities service @app {
  alpha "Alpha"
}
` + moves + `
view changes "Changes" diff {
  source diff "before" -> "after" {}
  diff {
    include [project, entity]
    detect_moves
  }
}
`
}

func relationMoveDiffViewSource(entityID string, withMove bool) string {
	moves := ""
	if withMove {
		moves = `
moves {
  entity alpha -> omega
}
`
	}
	return `
project p "Project" {}
layers {
  app "Application" @10
}
entity_type service "Service" {
  representation shape rect
}
relation_type links "Links" data_flow {
  from source types [service] layers [app]
  to target types [service] layers [app]
  label "links"
}
entities service @app {
  ` + entityID + ` "Alpha"
  beta "Beta"
}
relations links {
  link: ` + entityID + ` -> beta
}
` + moves + `
view changes "Changes" diff {
  source diff "before" -> "after" {}
  diff {
    include [entity, relation]
    detect_moves
  }
}
`
}

func chainedMoveDiffViewSource(activeID, moveLines string) string {
	return `
project p "Project" {}
layers {
  app "Application" @10
}
entity_type service "Service" {
  representation shape rect
}
entities service @app {
  ` + activeID + ` "Service"
}
moves {
  ` + moveLines + `
}
view changes "Changes" diff {
  source diff "before" -> "after" {}
  diff {
    include [entity]
    detect_moves
  }
}
`
}

func queryMoveDiffViewSource(entityID string, selectEntity, withMove bool) string {
	root := ""
	if selectEntity {
		root = entityID
	}
	moves := ""
	if withMove {
		moves = `
moves {
  entity alpha -> omega
}
`
	}
	return `
project p "Project" {}
layers {
  app "Application" @10
}
entity_type service "Service" {
  representation shape rect
}
entities service @app {
  ` + entityID + ` "Alpha"
}
` + moves + `
query scope "Scope" {
  select {
    roots [` + root + `]
  }
}
view changes "Changes" diff {
  source diff "before" -> "after" {
    query scope
    arguments {}
  }
  diff {
    include [entity]
    detect_moves
  }
}
`
}

func orderedColumnDiffViewSource(first, second string) string {
	return `
project p "Project" {}
entity_type service "Service" {
  representation shape rect
  columns {
    ` + first + ` "` + strings.ToUpper(first[:1]) + first[1:] + `" string
    ` + second + ` "` + strings.ToUpper(second[:1]) + second[1:] + `" string
  }
}
view changes "Changes" diff {
  source diff "before" -> "after" {}
  diff {
    include [entity_type]
  }
}
`
}

func tableColumnChildDiffViewSource(label string) string {
	return structuralQuerySource() + `
view changes "Changes" diff {
  source diff "before" -> "after" {}
  diff {
    include [view, view_table_column]
  }
}
view inventory "Inventory" inventory {
  source query prod_scope { environment: prod }
  table {
    rows entity_rows
    column environment {
      source attribute environment entity_types [service]
      label "` + label + `"
    }
  }
}
`
}

func executeDiffQuery(t *testing.T, snapshot Snapshot) QueryResult {
	t.Helper()
	response, err := New(BuildInfo{}).ExecuteQuery(context.Background(), QueryExecutionInput{
		Recipe: snapshot.TypedAST.Queries[0], Graph: *snapshot.TypedAST.Graph, Definition: snapshot.QueryDefinitionIdentity(), Arguments: map[string]TypedScalar{},
	})
	if err != nil || response.Status != "ok" || response.Result == nil {
		t.Fatalf("ExecuteQuery() = %+v err=%v", response, err)
	}
	return *response.Result
}

func diffChangeByAddress(t *testing.T, changes []DiffChange, address string) DiffChange {
	t.Helper()
	for _, change := range changes {
		if diffResultAddress(change) == address {
			return change
		}
	}
	t.Fatalf("Diff change %s not found in %+v", address, changes)
	return DiffChange{}
}

func diffFieldPaths(fields []FieldDiff) [][]string {
	out := make([][]string, len(fields))
	for index, field := range fields {
		out[index] = append([]string{}, field.Path...)
	}
	return out
}

func rowResultAddress(change DiffChange) string {
	if change.AfterAddress != nil {
		return *change.AfterAddress
	}
	return *change.BeforeAddress
}
