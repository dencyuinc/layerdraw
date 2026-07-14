// SPDX-License-Identifier: LicenseRef-LayerDraw-1.0

package view

import (
	"bytes"
	"encoding/json"
	"os"
	"reflect"
	"strings"
	"testing"

	"github.com/dencyuinc/layerdraw/internal/engine/internal/compiler/query"
	"github.com/dencyuinc/layerdraw/internal/engine/internal/compiler/resolve"
	"github.com/dencyuinc/layerdraw/internal/engine/internal/compiler/semantic/definition"
	"github.com/dencyuinc/layerdraw/internal/engine/internal/compiler/semantic/graph"
	"github.com/dencyuinc/layerdraw/internal/engine/internal/compiler/syntax"
)

func TestViewAndExportRecipeGolden(t *testing.T) {
	input := compileInput(t, `project p "Project" {}

query q "Query" {
  select {}
}

view v "View" topology {
  intent "Golden static recipe"
  source query q {}
  diagram {
    direction top_to_bottom
  }
  export data json "view.json" {
    fidelity lossless
    source_refs
    diagnostics
  }
}
`)
	got := Compile(input)
	if got.HasErrors {
		t.Fatalf("golden fixture diagnostics=%+v", got.Diagnostics)
	}
	payload, err := json.MarshalIndent(map[string]any{"exports": got.ExportRecipes.Recipes, "views": got.Recipes}, "", "  ")
	if err != nil {
		t.Fatalf("marshal golden payload: %v", err)
	}
	payload = append(payload, '\n')
	want, err := os.ReadFile("testdata/view_export_recipe.golden.json")
	if err != nil {
		t.Fatalf("read golden fixture: %v", err)
	}
	if !bytes.Equal(payload, want) {
		t.Fatalf("typed View/Export golden mismatch\n--- want\n%s\n--- got\n%s", want, payload)
	}
}

func TestClosedRecipeHelpers(t *testing.T) {
	formatA := definition.StringFormat("email")
	formatB := definition.StringFormat("uri")
	base := TableValueType{Kind: TableValueScalar, ScalarType: definition.ScalarEnum, EnumValues: []string{"a", "b"}, Format: &formatA}
	if !sameTableType(base, base) || sameTableType(base, TableValueType{Kind: TableValueStableAddress}) || sameTableType(base, TableValueType{Kind: TableValueScalar, ScalarType: definition.ScalarString, EnumValues: []string{"a", "b"}, Format: &formatA}) || sameTableType(base, TableValueType{Kind: TableValueScalar, ScalarType: definition.ScalarEnum, EnumValues: []string{"b", "a"}, Format: &formatA}) || sameTableType(base, TableValueType{Kind: TableValueScalar, ScalarType: definition.ScalarEnum, EnumValues: []string{"a", "b"}}) || sameTableType(base, TableValueType{Kind: TableValueScalar, ScalarType: definition.ScalarEnum, EnumValues: []string{"a", "b"}, Format: &formatB}) {
		t.Fatal("sameTableType does not compare the complete scalar schema")
	}
	for _, tc := range []struct {
		field string
		kind  TableValueKind
		ok    bool
	}{
		{field: "id", kind: TableValueScalar, ok: true}, {field: "display_name", kind: TableValueScalar, ok: true}, {field: "description", kind: TableValueScalar, ok: true},
		{field: "address", kind: TableValueStableAddress, ok: true}, {field: "type", kind: TableValueStableAddress, ok: true}, {field: "layer", kind: TableValueStableAddress, ok: true},
		{field: "tags", kind: TableValueStringSet, ok: true}, {field: "unknown", ok: false},
	} {
		got, ok := fieldTableType(tc.field)
		if ok != tc.ok || got.Kind != tc.kind {
			t.Fatalf("fieldTableType(%q)=(%+v,%v)", tc.field, got, ok)
		}
	}
	if equalStrings([]string{"a"}, []string{"a", "b"}) || equalStrings([]string{"a"}, []string{"b"}) || !equalStrings([]string{"a", "b"}, []string{"a", "b"}) {
		t.Fatal("equalStrings is not exact")
	}
	bindings := []resolve.SourceBinding{{ExpectedKind: resolve.KindEntity, TargetAddress: "ignored", Via: "other"}, {ExpectedKind: resolve.KindQuery, TargetAddress: "query", Via: "view:source.diff.query"}}
	if sourceQueryAddressFromBindings(bindings) != "query" || sourceQueryAddressFromBindings(nil) != "" || !containsResult([]query.ResultMember{query.ResultSeedEntities, query.ResultPathRelations}, query.ResultPathRelations) || containsResult(nil, query.ResultPathRelations) {
		t.Fatal("source-query/result helpers are not closed")
	}
	if composeStatePolicy(query.StateOptional, query.StateNone) != query.StateOptional || composeStatePolicy(query.StateOptional, query.StateRequired) != query.StateRequired || composeStatePolicy(query.StateNone, query.StateNone) != query.StateNone {
		t.Fatal("state policy precedence is not required > optional > none")
	}
}

func TestCompatibleColumnTypeClonesStringFormat(t *testing.T) {
	format := definition.StringFormatEmail
	c := &compiler{columns: map[string]definition.Column{
		"column": {ValueType: definition.ScalarString, Format: &format},
	}}
	valueType := c.compatibleColumnType(resolve.DeclarationSource{}, "view", "", syntax.Span{}, []string{"column"})
	if valueType.Format == nil || *valueType.Format != definition.StringFormatEmail {
		t.Fatalf("compiled format=%v", valueType.Format)
	}
	*valueType.Format = definition.StringFormatURI
	if format != definition.StringFormatEmail {
		t.Fatal("compiled Table value type aliases the Definition Column format")
	}
	if cloneStringFormat(nil) != nil {
		t.Fatal("nil string format clone changed representation")
	}
}

func TestShapeStateDependenciesCoverEveryRowGrainAndDeduplicate(t *testing.T) {
	column := TableColumn{Source: TableColumnSource{Kind: ColumnState, StateFieldPath: query.StateSystemUpdatedAt}, ValueType: scalarTableType(definition.ScalarDatetime)}
	wants := map[TableRowSource][]query.StateSubjectKind{
		RowsEntity: {query.StateSubjectEntity}, RowsEntityRows: {query.StateSubjectEntityRow}, RowsRelation: {query.StateSubjectRelation}, RowsRelationRows: {query.StateSubjectRelationRow},
		RowsAutomaticRelations: {query.StateSubjectRelation, query.StateSubjectRelationRow},
	}
	for rowSource, subjects := range wants {
		shape := Shape{Kind: ShapeTable, Table: &TableShape{RowSource: rowSource, Columns: []TableColumn{column, column}}}
		got := shapeStateReads(shape)
		if len(got) != len(subjects) {
			t.Fatalf("shapeStateReads(%s)=%+v", rowSource, got)
		}
		for index, subject := range subjects {
			if got[index].SubjectKind != subject || got[index].FieldPath != query.StateSystemUpdatedAt {
				t.Fatalf("shapeStateReads(%s)=%+v", rowSource, got)
			}
		}
	}
	if got := shapeStateReads(Shape{Kind: ShapeDiagram}); len(got) != 0 {
		t.Fatalf("non-table state reads=%+v", got)
	}
	ordered := canonicalStateReads([]query.StateReadDependency{
		{SubjectKind: query.StateSubjectEntity, FieldPath: query.StateSystemCreatedByKind, ValueType: definition.ScalarEnum},
		{SubjectKind: query.StateSubjectEntity, FieldPath: query.StateSystemUpdatedAt, ValueType: definition.ScalarDatetime},
	})
	if len(ordered) != 2 || ordered[0].FieldPath != query.StateSystemUpdatedAt || ordered[1].FieldPath != query.StateSystemCreatedByKind {
		t.Fatalf("View state reads ignored registry order: %+v", ordered)
	}
}

func TestStateTableColumnPreservesRegistryEnumSchema(t *testing.T) {
	input := compileInput(t, `project p "Project" {}
query q "Query" {
  select {}
}
view v "View" inventory {
  state_input optional
  source query q {}
  table {
    rows entity
    column actor_kind {
      source state system.created_by.kind
      aggregate min
    }
  }
}
`)
	got := Compile(input)
	if got.HasErrors {
		t.Fatalf("Compile() diagnostics=%+v", got.Diagnostics)
	}
	column := got.Recipes[0].Shape.Table.Columns[0]
	want := []string{"user", "agent", "service_account", "anonymous"}
	if column.ValueType.ScalarType != definition.ScalarEnum || !reflect.DeepEqual(column.ValueType.EnumValues, want) {
		t.Fatalf("state enum schema=%+v, want %v", column.ValueType, want)
	}
	column.ValueType.EnumValues[0] = "mutated"
	schema, ok := query.LookupStateFieldSchema(query.StateSystemCreatedByKind)
	if !ok || !reflect.DeepEqual(schema.EnumValues, want) {
		t.Fatalf("compiled enum values alias the shared state registry: %+v", schema)
	}
}

func TestInternalOrderingRangesAndDiagnosticDeduplication(t *testing.T) {
	c := &compiler{symbols: map[string]resolve.StableSymbol{
		"a": {Origin: resolve.Origin{Kind: resolve.OriginProject, ProjectID: "p"}, Path: []resolve.SymbolSegment{{Kind: resolve.KindEntity, ID: "a"}}},
		"b": {Origin: resolve.Origin{Kind: resolve.OriginProject, ProjectID: "p"}, Path: []resolve.SymbolSegment{{Kind: resolve.KindEntity, ID: "b"}}},
	}}
	if c.compareAddresses("a", "b") >= 0 || c.compareAddresses("z", "y") <= 0 {
		t.Fatal("address ordering did not use symbols then stable string fallback")
	}
	addresses := []string{"b", "a"}
	c.sortAddresses(addresses)
	if !reflect.DeepEqual(addresses, []string{"a", "b"}) {
		t.Fatalf("sorted addresses=%v", addresses)
	}
	c.relationTypes = map[string]definition.RelationType{"relation": {Address: "relation"}}
	if c.effectiveRelationType("relation").Address != "relation" || cloneEndpoint(nil) != nil {
		t.Fatal("effective RelationType fallback or nil clone changed")
	}
	c.projectionOverrides = map[string]RelationProjection{"relation": {RelationTypeAddress: "relation", Projections: definition.ProjectionSet{Diagram: definition.DiagramProjection{Mode: definition.DiagramHide}}}}
	if c.effectiveRelationType("relation").Projections.Diagram.Mode != definition.DiagramHide {
		t.Fatal("View projection override did not supersede RelationType defaults")
	}
	invalid := &compiler{}
	invalid.optionalString(resolve.DeclarationSource{}, resolve.DeclarationSymbol{Address: "view"}, authoredMember{head: "intent"})
	invalid.boundList(resolve.DeclarationSource{}, "view", resolve.KindEntityType, authoredMember{head: "entity_types"})
	if len(invalid.diagnostics) != 2 {
		t.Fatalf("invalid helper diagnostics=%+v", invalid.diagnostics)
	}

	node, _ := syntax.Parse([]byte("view v \"V\" topology {}\n")).Root.Children[0].(*syntax.Node)
	span := declarationHeaderSpan(node)
	if span.End <= span.Start || declarationHeaderSpan(nil) != (syntax.Span{}) {
		t.Fatalf("header spans=%+v nil=%+v", span, declarationHeaderSpan(nil))
	}
	source := resolve.DeclarationSource{Module: resolve.ModuleKey{Origin: resolve.Origin{Kind: resolve.OriginPack, Publisher: "vendor", PackName: "pack"}, Path: "view.ldl"}}
	c.diag("LDL1", "key", source, span, "message", "subject", "owner")
	c.diagRelated("LDL1", "key", source, span, "message", "subject", "owner", span)
	if c.diagnostics[0].Range == nil || c.diagnostics[0].Range.Origin.PackAddress == "" || len(c.diagnostics[1].Related) != 1 {
		t.Fatalf("diagnostic ranges=%+v", c.diagnostics)
	}
	duplicate := append([]resolve.Diagnostic{}, c.diagnostics[0], c.diagnostics[0], c.diagnostics[1])
	got := dedupeDiagnostics(duplicate)
	if len(got) != 1 || diagnosticKey(got[0]) == "" || itoa(0) != "0" || itoa(1203) != "1203" || hasError([]resolve.Diagnostic{{Severity: "warning"}}) || !hasError(got) {
		t.Fatalf("dedupe=%+v", got)
	}
	if sourceRange(resolve.DeclarationSource{}, span) != nil || ownerForSource(nil, "none") != "" {
		t.Fatal("missing sources/owners must remain absent")
	}
}

func TestShapeCompilersRejectNonBlockForms(t *testing.T) {
	c := &compiler{declarations: []resolve.DeclarationSymbol{}, symbols: map[string]resolve.StableSymbol{}, sources: map[string]resolve.DeclarationSource{}, bindings: map[string][]resolve.SourceBinding{}, graphEntities: map[string]bool{}, relationTypes: map[string]definition.RelationType{}, columns: map[string]definition.Column{}, queryRecipes: map[string]query.Recipe{}}
	source := resolve.DeclarationSource{Module: resolve.ModuleKey{Origin: resolve.Origin{Kind: resolve.OriginProject, ProjectID: "p"}, Path: "view.ldl"}}
	declaration := resolve.DeclarationSymbol{Address: "view"}
	member := authoredMember{args: []authoredValue{{raw: "invalid", kind: syntax.TokenIdentifier}}}
	c.compileDiagram(source, declaration, member)
	c.compileTable(source, declaration, member)
	c.compileMatrix(source, declaration, member)
	c.compileTree(source, declaration, member)
	c.compileFlow(source, declaration, member)
	c.compileContext(source, declaration, member)
	c.compileDiff(source, declaration, member)
	c.compileDiffSource(source, declaration, authoredMember{args: []authoredValue{{raw: "diff"}, {raw: `"before"`, kind: syntax.TokenString}, {raw: `"after"`, kind: syntax.TokenString}}})
	c.compileProjectionOverrides(source, declaration, []authoredMember{{head: "relation_projection", args: []authoredValue{{raw: "relation", kind: syntax.TokenIdentifier}}}})
	for _, message := range []string{"diagram requires", "table requires", "matrix requires", "tree requires", "flow requires", "context requires", "diff requires", "Diff source requires before", "requires RelationType and body"} {
		if !viewDiagnosticContains(c.diagnostics, message) {
			t.Fatalf("missing %q in diagnostics=%+v", message, c.diagnostics)
		}
	}
}

func TestCompileDefaultedShapes(t *testing.T) {
	source := validAllRecipes
	replacements := [][2]string{
		{`  diagram {
    layout manual
    direction right_to_left
    abstraction detail
    composed
    place alpha 0 0 100 50
  }`, `  diagram {}`},
		{`  table {
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
  }`, `  table {}`},
		{"  state_input optional\n", ""},
		{`  matrix {
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
  }`, `  matrix {
    row_axis {}
    column_axis {}
    cell {}
  }`},
		{`  tree {
    relation_types [link]
    cycle_policy truncate
    shared_child_policy link
  }`, `  tree {
    relation_types [link]
  }`},
		{`  flow {
    relation_types [link]
    lane_by attribute.environment
    cycle_policy error
    preserve_parallel
  }`, `  flow {
    relation_types [link]
  }`},
		{`  context {
    group_by entity_type
    entity_rows
    relation_rows
    incoming
    outgoing
  }`, `  context {}`},
		{`  diff {
    include [entity, relation, view_export]
    detect_moves
  }`, `  diff {}`},
	}
	for _, replacement := range replacements {
		source = replaceRequired(t, source, replacement[0], replacement[1])
	}
	got := Compile(compileInput(t, source))
	if got.HasErrors {
		t.Fatalf("defaulted recipes diagnostics=%+v", got.Diagnostics)
	}
	diagram := recipeByID(t, got, "diagram_view").Shape.Diagram
	table := recipeByID(t, got, "table_view").Shape.Table
	matrix := recipeByID(t, got, "matrix_view").Shape.Matrix
	tree := recipeByID(t, got, "tree_view").Shape.Tree
	flow := recipeByID(t, got, "flow_view").Shape.Flow
	context := recipeByID(t, got, "context_view").Shape.Context
	diff := recipeByID(t, got, "diff_view").Shape.Diff
	if diagram.Layout != LayoutLayered || diagram.Direction != DirectionLeftToRight || diagram.Abstraction != AbstractionNormal || diagram.Composed || len(diagram.Placements) != 0 || table.RowSource != RowsEntity || table.EntityTypeAddresses != nil || len(table.Columns) != 0 || matrix.RowAxis.LabelField != AxisLabelDisplayName || matrix.Cell.Semantic != MatrixRelationRefs || matrix.Cell.Display != MatrixExists || tree.CyclePolicy != TreeCycleError || tree.SharedChildPolicy != SharedChildError || flow.LaneBy != LaneNone || flow.CyclePolicy != FlowCycleIncludeCycleRef || context.GroupBy != ContextGroupLayer || !context.Incoming || !context.Outgoing || !reflect.DeepEqual(diff.Include, allDiffKinds) || diff.DetectMoves {
		t.Fatalf("defaults are not closed: diagram=%+v table=%+v matrix=%+v tree=%+v flow=%+v context=%+v diff=%+v", diagram, table, matrix, tree, flow, context, diff)
	}
}

func TestCompileTableColumnSourceAndAggregateFamilies(t *testing.T) {
	source := replaceRequired(t, validAllRecipes, `  state_input optional
`, "")
	source = replaceRequired(t, source, `  table {
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
  }`, `  table {
    rows entity_rows
    entity_types [service]
    column name {
      source field display_name
      aggregate join_unique
    }
    column address_count {
      source field address
      aggregate count
    }
    column related {
      source derived_count outgoing relations [link]
      aggregate count_distinct
    }
    column environment {
      source attribute environment entity_types [service]
      aggregate min
    }
    sort name ascending nulls first
  }`)
	got := Compile(compileInput(t, source))
	if got.HasErrors {
		t.Fatalf("entity Table families diagnostics=%+v", got.Diagnostics)
	}
	table := recipeByID(t, got, "table_view").Shape.Table
	if len(table.Columns) != 4 || table.Columns[0].Source.Kind != ColumnField || table.Columns[0].Aggregate != AggregateJoinUnique || table.Columns[0].ValueType.ScalarType != definition.ScalarString || table.Columns[1].Aggregate != AggregateCount || table.Columns[1].ValueType.ScalarType != definition.ScalarInteger || table.Columns[2].Source.Kind != ColumnDerivedCount || table.Columns[2].Aggregate != AggregateCountDistinct || table.Columns[2].Source.RelationTypeAddresses == nil || table.Columns[3].Aggregate != AggregateMin || table.Sorts[0].Direction != SortAscending || table.Sorts[0].Absent != AbsentFirst {
		t.Fatalf("entity Table=%+v", table)
	}

	source = replaceRequired(t, source, `  table {
    rows entity_rows
    entity_types [service]
    column name {
      source field display_name
      aggregate join_unique
    }
    column address_count {
      source field address
      aggregate count
    }
    column related {
      source derived_count outgoing relations [link]
      aggregate count_distinct
    }
    column environment {
      source attribute environment entity_types [service]
      aggregate min
    }
    sort name ascending nulls first
  }`, `  table {
    rows relation_rows
    column from_name {
      source relation_endpoint from display_name
    }
    column to_type {
      source relation_endpoint to type
    }
    column branch {
      source attribute branch relation_types [link]
    }
  }`)
	got = Compile(compileInput(t, source))
	if got.HasErrors {
		t.Fatalf("relation Table families diagnostics=%+v", got.Diagnostics)
	}
	table = recipeByID(t, got, "table_view").Shape.Table
	if table.Columns[0].Source.Kind != ColumnRelationEndpoint || table.Columns[0].ValueType.ScalarType != definition.ScalarString || table.Columns[1].ValueType.Kind != TableValueStableAddress || table.Columns[2].Source.Kind != ColumnAttribute {
		t.Fatalf("relation Table=%+v", table)
	}
}

func TestAutomaticRelationsFixedColumnsUseEffectiveProjectionUnion(t *testing.T) {
	source := replaceRequired(t, validAllRecipes, `  projection matrix {`, `  projection table {
    row_mode automatic
    include_from false
    include_to true
    include_relation_type false
  }
  projection matrix {`)
	source = replaceRequired(t, source, `view table_view "Table" inventory {
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
`, `view table_view "Table" inventory {
  source query scope { environment: prod }
  table {
    rows automatic_relations
    column from {
      source field id
    }
    sort from ascending nulls last
  }
`)

	got := Compile(compileInput(t, source))
	if got.HasErrors {
		t.Fatalf("non-projected fixed Column was rejected: %+v", got.Diagnostics)
	}
	table := recipeByID(t, got, "table_view").Shape.Table
	if len(table.Columns) != 1 || table.Columns[0].ID != "from" || len(table.Sorts) != 1 {
		t.Fatalf("automatic relation Table=%+v", table)
	}

	overridden := replaceRequired(t, source, `  source query scope { environment: prod }
  table {`, `  source query scope { environment: prod }
  relation_projection link {
    table {
      include_from true
    }
  }
  table {`)
	rejected := Compile(compileInput(t, overridden))
	if !rejected.HasErrors || !viewDiagnosticContains(rejected.Diagnostics, "conflicts with a fixed Column") {
		t.Fatalf("effective View projection did not reserve from: %+v", rejected.Diagnostics)
	}
}

func TestMatrixOmittedRelationSelectorValidatesQueryCandidates(t *testing.T) {
	omitted := replaceRequired(t, validAllRecipes, "      relation_types [link]\n      direction both\n", "      direction both\n")
	got := Compile(compileInput(t, omitted))
	if got.HasErrors {
		t.Fatalf("valid omitted Matrix selector diagnostics=%+v", got.Diagnostics)
	}
	if recipeByID(t, got, "matrix_view").Shape.Matrix.Cell.RelationTypeAddresses != nil {
		t.Fatal("omitted Matrix selector was materialized as an explicit selector")
	}

	withoutProjection := replaceRequired(t, omitted, `  projection matrix {
    row_endpoint from
    column_endpoint to
    include_relation_rows true
  }
`, "")
	rejected := Compile(compileInput(t, withoutProjection))
	if !rejected.HasErrors || !viewDiagnosticContains(rejected.Diagnostics, "does not participate in matrix projection") {
		t.Fatalf("omitted selector bypassed Matrix projection validation: %+v", rejected.Diagnostics)
	}

	withoutRows := replaceRequired(t, omitted, "    include_relation_rows true\n", "    include_relation_rows false\n")
	rejected = Compile(compileInput(t, withoutRows))
	if !rejected.HasErrors || !viewDiagnosticContains(rejected.Diagnostics, "requires RelationType matrix row inclusion") {
		t.Fatalf("omitted selector bypassed attribute_summary row validation: %+v", rejected.Diagnostics)
	}
}

func TestStateRequirementComposesQueryAndDirectViewPolicies(t *testing.T) {
	stateful := replaceRequired(t, validAllRecipes, `query scope "Scope" {
  parameters {`, `query scope "Scope" {
  state_input required
  parameters {`)
	stateful = replaceRequired(t, stateful, `  traverse outgoing 0..2 visit_once relations [link]`, `  where all {
    state system.updated_at exists
  }
  traverse outgoing 0..2 visit_once relations [link]`)
	withoutDiffQuery := replaceRequired(t, stateful, `  source diff "revision:before" -> "revision:after" {
    query scope
    arguments { environment: prod }
  }`, `  source diff "revision:before" -> "revision:after" {}`)
	got := Compile(compileInput(t, withoutDiffQuery))
	if got.HasErrors {
		t.Fatalf("state policy composition diagnostics=%+v", got.Diagnostics)
	}
	if recipeByID(t, got, "diagram_view").StateRequirement != query.StateRequired || recipeByID(t, got, "table_view").StateRequirement != query.StateRequired || recipeByID(t, got, "diff_view").StateRequirement != query.StateNone {
		t.Fatalf("state requirements did not compose required > optional > none: diagram=%s table=%s diff=%s", recipeByID(t, got, "diagram_view").StateRequirement, recipeByID(t, got, "table_view").StateRequirement, recipeByID(t, got, "diff_view").StateRequirement)
	}

	rejected := Compile(compileInput(t, stateful))
	if !rejected.HasErrors || !viewDiagnosticContains(rejected.Diagnostics, "forbid state-dependent Query") {
		t.Fatalf("state-dependent Diff Query was accepted: %+v", rejected.Diagnostics)
	}
}

func TestDiffQueryAcceptsCanonicalEmptyArgumentsMap(t *testing.T) {
	got := Compile(compileInput(t, `project p "Project" {}
query q "Query" {
  select {}
}
view changes "Changes" diff {
  source diff "before" -> "after" {
    query q
    arguments {}
  }
  diff {}
}
`))
	if got.HasErrors {
		t.Fatalf("empty Diff Query arguments diagnostics=%+v", got.Diagnostics)
	}
	diff := recipeByID(t, got, "changes").Source.Diff
	if diff == nil || diff.QueryAddress == nil || len(diff.Arguments) != 0 {
		t.Fatalf("Diff source is not closed: %+v", diff)
	}
}

func TestCompileRejectsInvalidViewContractsTransactionally(t *testing.T) {
	cases := []struct {
		name, old, replacement, message string
	}{
		{name: "unknown View member", old: "  diagram {\n    layout manual", replacement: "  mystery value\n  diagram {\n    layout manual", message: "unknown View member"},
		{name: "duplicate View member", old: "  source query scope { environment: prod }\n  relation_projection", replacement: "  source query scope { environment: prod }\n  source query scope { environment: prod }\n  relation_projection", message: "duplicate View member"},
		{name: "missing source", old: "  source query scope { environment: prod }\n  relation_projection", replacement: "  relation_projection", message: "exactly one source"},
		{name: "multiple shapes", old: "  diagram {\n    layout manual", replacement: "  context {}\n  diagram {\n    layout manual", message: "exactly one typed shape"},
		{name: "invalid category", old: `view diagram_view "Diagram" topology`, replacement: `view diagram_view "Diagram" unknown`, message: "invalid View category"},
		{name: "intent type", old: "  relation_projection link {", replacement: "  intent nope\n  relation_projection link {", message: "intent requires one string"},
		{name: "state arity", old: "  state_input optional", replacement: "  state_input", message: "state_input requires one policy"},
		{name: "state enum", old: "  state_input optional", replacement: "  state_input sometimes", message: "invalid View state policy"},
		{name: "empty source", old: "  source query scope { environment: prod }\n  relation_projection", replacement: "  source\n  relation_projection", message: "invalid View source"},
		{name: "source kind", old: "  source query scope { environment: prod }\n  relation_projection", replacement: "  source graph scope {}\n  relation_projection", message: "query or diff"},
		{name: "query source shape", old: "  source query scope { environment: prod }\n  relation_projection", replacement: "  source query scope\n  relation_projection", message: "requires Query reference"},
		{name: "required argument", old: "source query scope { environment: prod }", replacement: "source query scope { limit: 10 }", message: "required Query argument is missing"},
		{name: "argument type", old: "source query scope { environment: prod }", replacement: "source query scope { environment: 7 }", message: "does not satisfy parameter schema"},
		{name: "duplicate argument", old: "source query scope { environment: prod }", replacement: "source query scope { environment: prod, environment: stg }", message: "duplicate Query argument"},
		{name: "argument key", old: "source query scope { environment: prod }", replacement: "source query scope { \"environment\": prod }", message: "key must be a parameter ID"},
		{name: "non-diff category", old: `view diagram_view "Diagram" topology`, replacement: `view diagram_view "Diagram" diff`, message: "must occur together"},
		{name: "state without read", old: "view diagram_view \"Diagram\" topology {", replacement: "view diagram_view \"Diagram\" topology {\n  state_input optional", message: "forbidden without direct Table state reads"},
		{name: "state read without policy", old: "  state_input optional\n  source query scope { environment: prod }\n  table", replacement: "  source query scope { environment: prod }\n  table", message: "require optional or required"},
		{name: "diagram enum", old: "    layout manual", replacement: "    layout impossible", message: "invalid layout"},
		{name: "diagram duplicate", old: "    layout manual", replacement: "    layout manual\n    layout grid", message: "duplicate diagram member"},
		{name: "diagram flag", old: "    abstraction detail\n    composed\n    place", replacement: "    abstraction detail\n    composed true\n    place", message: "unknown or invalid diagram member"},
		{name: "placement arity", old: "    place alpha 0 0 100 50", replacement: "    place alpha 0 0 100", message: "place requires Entity"},
		{name: "placement number", old: "    place alpha 0 0 100 50", replacement: "    place alpha nope 0 100 50", message: "coordinates must be finite"},
		{name: "placement size", old: "    place alpha 0 0 100 50", replacement: "    place alpha 0 0 0 50", message: "sizes positive"},
		{name: "placement duplicate", old: "    place alpha 0 0 100 50", replacement: "    place alpha 0 0 100 50\n    place alpha 1 1 20 20", message: "duplicate Entity placement"},
		{name: "table enum", old: "    rows entity_rows", replacement: "    rows unknown", message: "invalid rows"},
		{name: "table duplicate", old: "    rows entity_rows", replacement: "    rows entity_rows\n    rows entity", message: "duplicate table member"},
		{name: "relation fixed columns", old: "    rows entity_rows", replacement: "    rows relation_rows", message: "forbidden for Relation rows"},
		{name: "column fixed conflict", old: "    column environment {", replacement: "    column entity_id {", message: "conflicts with a fixed Column"},
		{name: "column missing source", old: "      source attribute environment entity_types [service]\n      label \"Environment\"", replacement: "      label \"Environment\"", message: "requires exactly one source"},
		{name: "column source arity", old: "      source attribute environment entity_types [service]", replacement: "      source field", message: "invalid Table Column source"},
		{name: "field arity", old: "      source attribute environment entity_types [service]", replacement: "      source field id extra", message: "field source requires one field"},
		{name: "field invalid", old: "      source attribute environment entity_types [service]", replacement: "      source field unknown", message: "invalid Table field"},
		{name: "attribute grain", old: "    rows entity_rows", replacement: "    rows entity", message: "attribute source requires"},
		{name: "attribute restriction syntax", old: "source attribute environment entity_types [service]", replacement: "source attribute environment owners [service]", message: "restrictions must be"},
		{name: "attribute restriction duplicate", old: "source attribute environment entity_types [service]", replacement: "source attribute environment entity_types [service] entity_types [service]", message: "duplicate attribute source restriction"},
		{name: "attribute owner kind", old: "source attribute environment entity_types [service]", replacement: "source attribute weight relation_types [link]", message: "incompatible with Table row source"},
		{name: "relation endpoint grain", old: "      source attribute environment entity_types [service]", replacement: "      source relation_endpoint from id", message: "requires Relation rows"},
		{name: "relation endpoint invalid", old: "      source attribute environment entity_types [service]", replacement: "      source relation_endpoint side nope", message: "invalid relation_endpoint"},
		{name: "derived count invalid", old: "      source attribute environment entity_types [service]", replacement: "      source derived_count sideways relations", message: "invalid derived_count"},
		{name: "derived count restriction", old: "      source attribute environment entity_types [service]", replacement: "      source derived_count outgoing relations link", message: "requires a list"},
		{name: "state arity", old: "      source state system.updated_at", replacement: "      source state system.updated_at extra", message: "state source requires one field path"},
		{name: "state field", old: "      source state system.updated_at", replacement: "      source state system.unknown", message: "unknown state field path"},
		{name: "column source kind", old: "      source state system.updated_at", replacement: "      source computed system.updated_at", message: "unknown Table Column source kind"},
		{name: "min aggregate type", old: "      source attribute environment entity_types [service]\n      label \"Environment\"\n      aggregate none", replacement: "      source field address\n      label \"Environment\"\n      aggregate min", message: "min/max require"},
		{name: "join aggregate type", old: "      source state system.updated_at\n      aggregate max", replacement: "      source state system.updated_at\n      aggregate join_unique", message: "join_unique requires"},
		{name: "sort arity", old: "    sort updated descending nulls last", replacement: "    sort updated descending last", message: "sort requires Column"},
		{name: "sort unknown", old: "    sort updated descending nulls last", replacement: "    sort missing descending nulls last", message: "unknown output Column"},
		{name: "sort direction", old: "    sort updated descending nulls last", replacement: "    sort updated sideways nulls last", message: "invalid sort direction"},
		{name: "sort nulls", old: "    sort updated descending nulls last", replacement: "    sort updated descending nulls middle", message: "invalid sort null order"},
		{name: "matrix required blocks", old: "    column_axis {\n      entity_types [service]\n      label id\n    }", replacement: "", message: "matrix requires row_axis"},
		{name: "matrix axis label", old: "      label display_name", replacement: "      label nope", message: "invalid label"},
		{name: "matrix summary attributes", old: "      attributes [weight]", replacement: "", message: "requires a non-empty attributes"},
		{name: "matrix summary semantic", old: "      semantic relation_refs", replacement: "      semantic path_refs", message: "requires relation_refs"},
		{name: "matrix attributes forbidden", old: "      display attribute_summary", replacement: "      display exists", message: "attributes are forbidden"},
		{name: "tree types", old: "    relation_types [link]\n    cycle_policy truncate\n    shared_child_policy link", replacement: "    cycle_policy truncate\n    shared_child_policy link", message: "non-empty relation_types"},
		{name: "tree policy", old: "    cycle_policy truncate", replacement: "    cycle_policy loop", message: "invalid cycle_policy"},
		{name: "flow types", old: "    relation_types [link]\n    lane_by attribute.environment", replacement: "    lane_by attribute.environment", message: "non-empty relation_types"},
		{name: "flow lane arity", old: "    lane_by attribute.environment", replacement: "    lane_by", message: "requires one mode"},
		{name: "flow lane", old: "    lane_by attribute.environment", replacement: "    lane_by unknown", message: "invalid lane_by"},
		{name: "context group", old: "    group_by entity_type", replacement: "    group_by unknown", message: "invalid group_by"},
		{name: "context flag", old: "    entity_rows", replacement: "    entity_rows true", message: "unknown or invalid context member"},
		{name: "diff include shape", old: "    include [entity, relation, view_export]", replacement: "    include entity", message: "requires one list"},
		{name: "diff include kind", old: "    include [entity, relation, view_export]", replacement: "    include [entity, unknown]", message: "unknown Diff subject kind"},
		{name: "diff include duplicate", old: "    include [entity, relation, view_export]", replacement: "    include [entity, entity]", message: "duplicate Diff subject kind"},
		{name: "diff flag", old: "    detect_moves", replacement: "    detect_moves true", message: "unknown or invalid diff member"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			source := replaceRequired(t, validAllRecipes, tc.old, tc.replacement)
			got := Compile(compileInput(t, source))
			if !got.HasErrors || len(got.Recipes) != 0 || len(got.ExportRecipes.Recipes) != 0 || !viewDiagnosticContains(got.Diagnostics, tc.message) {
				t.Fatalf("Compile() diagnostics=%+v recipes=%+v exports=%+v, want %q", got.Diagnostics, got.Recipes, got.ExportRecipes.Recipes, tc.message)
			}
		})
	}
}

func TestCompileRejectsInvalidDiffSourceContracts(t *testing.T) {
	cases := []struct {
		name, old, replacement, message string
	}{
		{name: "empty before", old: `source diff "revision:before" -> "revision:after"`, replacement: `source diff "" -> "revision:after"`, message: "before selector must be non-empty"},
		{name: "empty after", old: `source diff "revision:before" -> "revision:after"`, replacement: `source diff "revision:before" -> ""`, message: "after selector must be non-empty"},
		{name: "same selectors", old: `source diff "revision:before" -> "revision:after"`, replacement: `source diff "revision:before" -> "revision:before"`, message: "selectors must differ"},
		{name: "missing arrow", old: `source diff "revision:before" -> "revision:after"`, replacement: `source diff "revision:before" "revision:after"`, message: "requires before, arrow, after"},
		{name: "unknown child", old: "    query scope\n    arguments", replacement: "    unknown value\n    query scope\n    arguments", message: "unknown or invalid Diff source member"},
		{name: "duplicate child", old: "    query scope\n    arguments", replacement: "    query scope\n    query scope\n    arguments", message: "duplicate Diff source member"},
		{name: "query arity", old: "    query scope", replacement: "    query scope extra", message: "Diff Query requires one reference"},
		{name: "missing arguments", old: "    arguments { environment: prod }", replacement: "", message: "requires one arguments object"},
		{name: "arguments shape", old: "    arguments { environment: prod }", replacement: "    arguments environment", message: "requires one arguments object"},
		{name: "arguments without query", old: "    query scope\n", replacement: "", message: "forbidden without Query"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			source := replaceRequired(t, validAllRecipes, tc.old, tc.replacement)
			got := Compile(compileInput(t, source))
			if !got.HasErrors || len(got.Recipes) != 0 || !viewDiagnosticContains(got.Diagnostics, tc.message) {
				t.Fatalf("Compile() diagnostics=%+v, want %q", got.Diagnostics, tc.message)
			}
		})
	}
}

func TestCompileRejectsInvalidProjectionOverrides(t *testing.T) {
	cases := []struct {
		name, old, replacement, message string
	}{
		{name: "duplicate override", old: "  relation_projection link {", replacement: "  relation_projection link {\n    diagram {}\n  }\n  relation_projection link {", message: "duplicate RelationType projection override"},
		{name: "unknown primitive", old: "    composed {", replacement: "    mystery {}\n    composed {", message: "unknown projection override primitive"},
		{name: "primitive without body", old: "    composed {", replacement: "    mystery\n    composed {", message: "requires a body"},
		{name: "duplicate primitive", old: "    composed {", replacement: "    composed {}\n    composed {", message: "duplicate projection primitive override"},
		{name: "composed arguments", old: "    composed {", replacement: "    composed invalid {", message: "takes no arguments"},
		{name: "diagram arguments", old: "    diagram {", replacement: "    diagram invalid {", message: "takes no arguments"},
		{name: "composed boolean", old: "      keep_edge true", replacement: "      keep_edge maybe", message: "requires true or false"},
		{name: "composed integer", old: "      priority 5", replacement: "      priority nope", message: "requires an integer"},
		{name: "composed safe integer", old: "      priority 5", replacement: "      priority 9007199254740992", message: "JSON-safe integer"},
		{name: "edge endpoints", old: "      keep_edge true", replacement: "      keep_edge true\n      parent_endpoint from", message: "forbidden for effective edge"},
		{name: "nest endpoints", old: "      priority 5", replacement: "      mode nest\n      parent_endpoint from\n      child_endpoint from\n      priority 5", message: "invalid effective nest"},
		{name: "overlay endpoints", old: "      priority 5", replacement: "      mode overlay\n      overlay_endpoint from\n      target_endpoint from\n      priority 5", message: "invalid effective overlay"},
		{name: "badge endpoints", old: "      priority 5", replacement: "      mode badge\n      badge_endpoint from\n      target_endpoint from\n      priority 5", message: "invalid effective badge"},
		{name: "diagram endpoints", old: "      edge_label display_name", replacement: "      source_endpoint from\n      target_endpoint from\n      edge_label display_name", message: "Diagram endpoints must differ"},
		{name: "reverse label", old: "      edge_label display_name", replacement: "      edge_label reverse_label", message: "requires an authored RelationType reverse label"},
		{name: "matrix endpoints", old: "      row_endpoint from\n      column_endpoint to", replacement: "      row_endpoint from\n      column_endpoint from", message: "Matrix endpoints"},
		{name: "tree endpoints", old: "      parent_endpoint from\n      child_endpoint to", replacement: "      parent_endpoint from\n      child_endpoint from", message: "Tree endpoints"},
		{name: "flow endpoints", old: "      source_endpoint from\n      target_endpoint to\n      connector_kind message", replacement: "      source_endpoint from\n      target_endpoint from\n      connector_kind message", message: "Flow projection requires"},
		{name: "flow branch arity", old: "      branch_value_column branch", replacement: "      branch_value_column branch extra", message: "requires one Column"},
		{name: "context placeholder", old: `      fact_template "{from.display_name} links {to.display_name}"`, replacement: `      fact_template "{from.unknown} links {to.display_name}"`, message: "unknown Context template placeholder"},
		{name: "context malformed", old: `      fact_template "{from.display_name} links {to.display_name}"`, replacement: `      fact_template "{from.display_name} links {"`, message: "malformed Context template placeholder"},
		{name: "render arity", old: "    render edge {", replacement: "    render {", message: "render override requires one primitive"},
		{name: "render primitive", old: "    render edge {", replacement: "    render unknown {", message: "unknown render override primitive"},
		{name: "render color arity", old: `      color "#aabbcc"`, replacement: "      color", message: "color requires one string"},
		{name: "render color value", old: `      color "#aabbcc"`, replacement: `      color "red"`, message: "invalid canonical color"},
		{name: "overlay kind", old: `      kind "shield"`, replacement: "      kind", message: "overlay kind requires one atom"},
		{name: "overlay max", old: "      max_items 8", replacement: "      max_items 0", message: "requires a positive integer"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			source := replaceRequired(t, validAllRecipes, tc.old, tc.replacement)
			if tc.name == "reverse label" {
				source = replaceRequired(t, source, "  reverse \"is linked by\"\n", "")
			}
			got := Compile(compileInput(t, source))
			if !got.HasErrors || len(got.Recipes) != 0 || !viewDiagnosticContains(got.Diagnostics, tc.message) {
				t.Fatalf("Compile() diagnostics=%+v, want %q", got.Diagnostics, tc.message)
			}
		})
	}
}

func TestCompileParentGenerationTransactionAndSnapshotIsolation(t *testing.T) {
	good := compileInput(t, validAllRecipes)
	otherSource := strings.Replace(validAllRecipes, `project p "Project"`, `project other "Other"`, 1)
	other := compileInput(t, otherSource)
	cases := []struct {
		name   string
		mutate func(*Input)
		want   string
	}{
		{name: "definition", mutate: func(input *Input) { input.Definition = other.Definition }, want: "definition result does not match"},
		{name: "graph", mutate: func(input *Input) { input.Graph = other.Graph }, want: "graph result does not match"},
		{name: "query", mutate: func(input *Input) { input.Query = other.Query }, want: "Query result does not match"},
		{name: "nil graph", mutate: func(input *Input) { input.Graph.Graph = nil }, want: "typed graph result is unavailable"},
		{name: "duplicate query", mutate: func(input *Input) { input.Query.Recipes = append(input.Query.Recipes, input.Query.Recipes[0]) }, want: "duplicate or foreign"},
		{name: "foreign query", mutate: func(input *Input) { input.Query.Recipes[0].Address = "foreign" }, want: "duplicate or foreign"},
		{name: "missing query", mutate: func(input *Input) { input.Query.Recipes = nil }, want: "missing an effective recipe"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			input := good
			input.Query.Recipes = append([]query.Recipe{}, good.Query.Recipes...)
			tc.mutate(&input)
			input.Resolve.HasErrors = true
			input.Resolve.Diagnostics = []resolve.Diagnostic{{Code: "UPSTREAM", Severity: "error", Message: "upstream", Arguments: map[string]string{"owner": "input"}}}
			got := Compile(input)
			if !got.HasErrors || len(got.Recipes) != 0 || !viewDiagnosticContains(got.Diagnostics, "upstream") || !viewDiagnosticContains(got.Diagnostics, tc.want) {
				t.Fatalf("Compile() diagnostics=%+v, want %q", got.Diagnostics, tc.want)
			}
			for index := range got.Diagnostics {
				got.Diagnostics[index].Arguments["owner"] = "result"
			}
			if input.Resolve.Diagnostics[0].Arguments["owner"] != "input" {
				t.Fatal("upstream diagnostic storage was aliased")
			}
		})
	}

	result := Compile(good)
	if result.HasErrors || !result.Generation().Matches(good.Resolve.Generation()) {
		t.Fatalf("successful result=%+v", result.Diagnostics)
	}
	diagram := recipeByID(t, result, "diagram_view")
	if len(diagram.Dependencies.QueryAddresses) == 0 || len(diagram.Exports) == 0 {
		t.Fatalf("diagram_view fixture lacks mutable nested fields: %+v", diagram)
	}
	wantQueryAddress := diagram.Dependencies.QueryAddresses[0]
	wantFilename := diagram.Exports[0].Filename
	diagram.Dependencies.QueryAddresses[0] = "mutated-query"
	diagram.Exports[0].Filename = "mutated-export"
	if diagram.Exports[0].Options.Structured == nil {
		t.Fatal("diagram_view fixture lacks mutable export options")
	}
	diagram.Exports[0].Options.Structured.Diagnostics = !diagram.Exports[0].Options.Structured.Diagnostics
	mutated := recipeByID(t, result, "diagram_view")
	if mutated.Dependencies.QueryAddresses[0] != "mutated-query" || mutated.Exports[0].Filename != "mutated-export" {
		t.Fatal("test did not mutate the returned recipe snapshot")
	}
	for _, export := range result.ExportRecipes.Recipes {
		if export.Address == diagram.Exports[0].Address && export.Options.Structured != nil && export.Options.Structured.Diagnostics == diagram.Exports[0].Options.Structured.Diagnostics {
			t.Fatal("View-local Export options alias the aggregate Export result")
		}
	}
	again := Compile(good)
	if again.HasErrors {
		t.Fatalf("recompile after returned-snapshot mutation failed: %+v", again.Diagnostics)
	}
	fresh := recipeByID(t, again, "diagram_view")
	if len(fresh.Dependencies.QueryAddresses) == 0 || len(fresh.Exports) == 0 {
		t.Fatalf("fresh diagram_view lost nested fields: %+v", fresh)
	}
	if fresh.Dependencies.QueryAddresses[0] != wantQueryAddress || fresh.Exports[0].Filename != wantFilename {
		t.Fatalf("returned snapshot mutation leaked into recompilation: query=%q export=%q", fresh.Dependencies.QueryAddresses[0], fresh.Exports[0].Filename)
	}
	third := Compile(good)
	if !reflect.DeepEqual(again.Recipes, third.Recipes) || !reflect.DeepEqual(again.Diagnostics, third.Diagnostics) {
		t.Fatal("recompilation is not snapshot-stable")
	}
}

func TestCompileIsDeterministicAcrossParentSlicePermutations(t *testing.T) {
	input := compileInput(t, validAllRecipes)
	want := Compile(input)
	if want.HasErrors {
		t.Fatalf("baseline diagnostics=%+v", want.Diagnostics)
	}
	reverseViewSlice(input.Resolve.Declarations)
	reverseViewSlice(input.Resolve.DeclarationSources)
	reverseViewSlice(input.Resolve.Candidates)
	reverseViewSlice(input.Resolve.Bindings)
	reverseViewSlice(input.Definition.EntityTypes)
	reverseViewSlice(input.Definition.RelationTypes)
	reverseViewSlice(input.Graph.Graph.Entities)
	reverseViewSlice(input.Graph.Graph.Relations)
	reverseViewSlice(input.Query.Recipes)
	got := Compile(input)
	if got.HasErrors || !reflect.DeepEqual(got.Recipes, want.Recipes) || !reflect.DeepEqual(got.ExportRecipes.Recipes, want.ExportRecipes.Recipes) || !reflect.DeepEqual(got.Diagnostics, want.Diagnostics) {
		t.Fatalf("parent slice permutation changed static recipes: diagnostics=%+v", got.Diagnostics)
	}
}

func TestUpstreamDiagnosticPriorityAndCloning(t *testing.T) {
	diagnostic := func(message string) []resolve.Diagnostic {
		return []resolve.Diagnostic{{Message: message, Arguments: map[string]string{"owner": message}}}
	}
	input := Input{Resolve: resolve.Result{Diagnostics: diagnostic("resolve")}, Definition: definition.Result{Diagnostics: diagnostic("definition")}, Graph: graph.Result{Diagnostics: diagnostic("graph")}, Query: query.Result{Diagnostics: diagnostic("query")}}
	for _, tc := range []struct {
		name string
		drop func(*Input)
		want string
	}{
		{name: "query", drop: func(*Input) {}, want: "query"},
		{name: "graph", drop: func(in *Input) { in.Query.Diagnostics = nil }, want: "graph"},
		{name: "definition", drop: func(in *Input) { in.Query.Diagnostics = nil; in.Graph.Diagnostics = nil }, want: "definition"},
		{name: "resolve", drop: func(in *Input) {
			in.Query.Diagnostics = nil
			in.Graph.Diagnostics = nil
			in.Definition.Diagnostics = nil
		}, want: "resolve"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			value := input
			tc.drop(&value)
			got := upstreamDiagnostics(value)
			if len(got) != 1 || got[0].Message != tc.want {
				t.Fatalf("diagnostics=%+v", got)
			}
			got[0].Arguments["owner"] = "result"
		})
	}
}

func TestCompileRejectsMissingSourcesAndResolverBindings(t *testing.T) {
	input := compileInput(t, validAllRecipes)
	for index, source := range input.Resolve.DeclarationSources {
		if source.Address == recipeAddress(input.Resolve, resolve.KindView, "diagram_view") {
			input.Resolve.DeclarationSources = append(input.Resolve.DeclarationSources[:index], input.Resolve.DeclarationSources[index+1:]...)
			break
		}
	}
	got := Compile(input)
	if !got.HasErrors || !viewDiagnosticContains(got.Diagnostics, "missing View declaration source") {
		t.Fatalf("missing View source diagnostics=%+v", got.Diagnostics)
	}

	input = compileInput(t, validAllRecipes)
	viewAddress := recipeAddress(input.Resolve, resolve.KindView, "diagram_view")
	filtered := input.Resolve.Bindings[:0]
	removed := false
	for _, binding := range input.Resolve.Bindings {
		if !removed && binding.SourceAddress == viewAddress && binding.ExpectedKind == resolve.KindQuery {
			removed = true
			continue
		}
		filtered = append(filtered, binding)
	}
	input.Resolve.Bindings = filtered
	got = Compile(input)
	if !got.HasErrors || !viewDiagnosticContains(got.Diagnostics, "precise resolver-owned binding") || len(got.Recipes) != 0 {
		t.Fatalf("missing binding diagnostics=%+v recipes=%+v", got.Diagnostics, got.Recipes)
	}
}

func recipeAddress(result resolve.Result, kind resolve.SubjectKind, id string) string {
	for _, declaration := range result.Declarations {
		if declaration.Kind == kind && declaration.ID == id {
			return declaration.Address
		}
	}
	return ""
}

func replaceRequired(t *testing.T, source, old, replacement string) string {
	t.Helper()
	if !strings.Contains(source, old) {
		t.Fatalf("fixture does not contain %q", old)
	}
	return strings.Replace(source, old, replacement, 1)
}

func viewDiagnosticContains(diagnostics []resolve.Diagnostic, text string) bool {
	for _, diagnostic := range diagnostics {
		if strings.Contains(diagnostic.Message, text) {
			return true
		}
	}
	return false
}

func reverseViewSlice[T any](values []T) {
	for left, right := 0, len(values)-1; left < right; left, right = left+1, right-1 {
		values[left], values[right] = values[right], values[left]
	}
}
