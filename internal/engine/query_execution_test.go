// SPDX-License-Identifier: LicenseRef-LayerDraw-1.0

package engine

import (
	"context"
	"reflect"
	"testing"

	"github.com/dencyuinc/layerdraw/internal/engine/internal/compiler/query"
	"github.com/dencyuinc/layerdraw/internal/engine/internal/compiler/semantic/definition"
	"github.com/dencyuinc/layerdraw/internal/engine/internal/compiler/semantic/graph"
)

func TestExecuteQueryEvaluatesStructuralRecipe(t *testing.T) {
	t.Parallel()
	snapshot := compileQueryExecutionFixture(t, structuralQuerySource())
	response, err := New(BuildInfo{}).ExecuteQuery(context.Background(), QueryExecutionInput{
		Recipe: snapshot.TypedAST.Queries[0],
		Graph:  *snapshot.TypedAST.Graph,
	})
	if err != nil {
		t.Fatal(err)
	}
	if response.Status != "ok" || response.Result == nil {
		t.Fatalf("ExecuteQuery() = %+v", response)
	}
	result := response.Result
	if !reflect.DeepEqual(result.SeedEntityAddresses, []string{"ldl:project:p:entity:alpha"}) {
		t.Fatalf("seed entities = %v", result.SeedEntityAddresses)
	}
	if !reflect.DeepEqual(result.ReachedEntityAddresses, []string{"ldl:project:p:entity:beta"}) {
		t.Fatalf("reached entities = %v", result.ReachedEntityAddresses)
	}
	if !reflect.DeepEqual(result.TraversedEntityAddresses, []string{"ldl:project:p:entity:beta"}) {
		t.Fatalf("traversed entities = %v", result.TraversedEntityAddresses)
	}
	if !reflect.DeepEqual(result.PathRelationAddresses, []string{"ldl:project:p:relation:alpha_beta"}) {
		t.Fatalf("path relations = %v", result.PathRelationAddresses)
	}
	if !reflect.DeepEqual(result.SelectedRelationAddresses, []string{"ldl:project:p:relation:alpha_beta"}) {
		t.Fatalf("selected relations = %v", result.SelectedRelationAddresses)
	}
	if !reflect.DeepEqual(result.PrimaryEntityAddresses, []string{"ldl:project:p:entity:alpha", "ldl:project:p:entity:beta"}) {
		t.Fatalf("primary entities = %v", result.PrimaryEntityAddresses)
	}
	if len(result.SupportEntityAddresses) != 0 {
		t.Fatalf("support entities = %v", result.SupportEntityAddresses)
	}
	wantPaths := []QueryPath{
		{EntityAddresses: []string{"ldl:project:p:entity:alpha"}, RelationAddresses: []string{}},
		{EntityAddresses: []string{"ldl:project:p:entity:alpha", "ldl:project:p:entity:beta"}, RelationAddresses: []string{"ldl:project:p:relation:alpha_beta"}},
	}
	if !reflect.DeepEqual(result.Paths, wantPaths) {
		t.Fatalf("paths = %#v", result.Paths)
	}
	if len(result.Diagnostics) != 0 {
		t.Fatalf("diagnostics = %+v", result.Diagnostics)
	}
}

func TestExecuteQueryRejectsRequiredStateWithoutSnapshot(t *testing.T) {
	t.Parallel()
	snapshot := compileQueryExecutionFixture(t, requiredStateQuerySource())
	response, err := New(BuildInfo{}).ExecuteQuery(context.Background(), QueryExecutionInput{
		Recipe: snapshot.TypedAST.Queries[0],
		Graph:  *snapshot.TypedAST.Graph,
	})
	if err != nil {
		t.Fatal(err)
	}
	if response.Status != "rejected" || response.Result != nil {
		t.Fatalf("ExecuteQuery() = %+v", response)
	}
	if len(response.Diagnostics) != 1 || response.Diagnostics[0].Code != "LDL1604" {
		t.Fatalf("diagnostics = %+v", response.Diagnostics)
	}
}

func TestExecuteQueryOptionalStateEvaluatesAsMissingWithReadRefs(t *testing.T) {
	t.Parallel()
	snapshot := compileQueryExecutionFixture(t, optionalStateQuerySource())
	response, err := New(BuildInfo{}).ExecuteQuery(context.Background(), QueryExecutionInput{
		Recipe: snapshot.TypedAST.Queries[0],
		Graph:  *snapshot.TypedAST.Graph,
	})
	if err != nil {
		t.Fatal(err)
	}
	if response.Status != "ok" || response.Result == nil {
		t.Fatalf("ExecuteQuery() = %+v", response)
	}
	if len(response.Result.SeedEntityAddresses) != 0 {
		t.Fatalf("state exists predicate should have no seeds without a snapshot: %v", response.Result.SeedEntityAddresses)
	}
	wantRead := StateReadRef{SubjectAddress: "ldl:project:p:entity:alpha", FieldPath: "system.updated_at"}
	if !reflect.DeepEqual(response.Result.StateReads, []StateReadRef{wantRead}) {
		t.Fatalf("state reads = %+v", response.Result.StateReads)
	}
	if len(response.Result.Diagnostics) != 2 || response.Result.Diagnostics[0].Code != "LDL1605" || response.Result.Diagnostics[1].Code != "LDL1602" {
		t.Fatalf("diagnostics = %+v", response.Result.Diagnostics)
	}
}

func TestExecuteQueryRejectsInvalidArgumentsBeforeGraphEvaluation(t *testing.T) {
	t.Parallel()
	snapshot := compileQueryExecutionFixture(t, structuralQuerySource())
	response, err := New(BuildInfo{}).ExecuteQuery(context.Background(), QueryExecutionInput{
		Recipe: snapshot.TypedAST.Queries[0],
		Graph:  *snapshot.TypedAST.Graph,
		Arguments: map[string]TypedScalar{
			"ldl:project:p:query:prod_scope:parameter:environment": {Type: definition.ScalarString, String: "prod"},
			"ldl:project:p:query:prod_scope:parameter:unknown":     {Type: definition.ScalarEnum, String: "prod"},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if response.Status != "rejected" || len(response.Diagnostics) != 2 {
		t.Fatalf("ExecuteQuery() = %+v", response)
	}
	for _, diagnostic := range response.Diagnostics {
		if diagnostic.Code != "LDL1601" {
			t.Fatalf("diagnostic = %+v", diagnostic)
		}
	}
}

func TestExecuteDocumentQueryUsesRetainedWorkbenchGeneration(t *testing.T) {
	t.Parallel()
	instance := New(BuildInfo{})
	opened, err := instance.OpenDocument(context.Background(), OpenDocumentInput{
		CompileInput:    projectCompileInput(structuralQuerySource()),
		RequestedLimits: generousWorkbenchLimits,
	})
	if err != nil {
		t.Fatal(err)
	}
	result, err := instance.ExecuteDocumentQuery(context.Background(), ExecuteDocumentQueryInput{
		Arguments: map[string]TypedScalar{
			"ldl:project:p:query:prod_scope:parameter:environment": {Type: definition.ScalarEnum, String: "prod"},
		},
		DocumentGeneration: opened.DocumentGeneration,
		Limits:             generousWorkbenchLimits,
		QueryAddress:       "ldl:project:p:query:prod_scope",
	})
	if err != nil {
		t.Fatal(err)
	}
	if result.DocumentGeneration != opened.DocumentGeneration {
		t.Fatalf("generation = %+v want %+v", result.DocumentGeneration, opened.DocumentGeneration)
	}
	if result.ReturnedItems == 0 || result.ReturnedBytes == 0 {
		t.Fatalf("result limits were not measured: %+v", result)
	}
	if !reflect.DeepEqual(result.Result.ReachedEntityAddresses, []string{"ldl:project:p:entity:beta"}) {
		t.Fatalf("reached entities = %v", result.Result.ReachedEntityAddresses)
	}
	if result.Result.Arguments["ldl:project:p:query:prod_scope:parameter:environment"].String != "prod" {
		t.Fatalf("arguments = %+v", result.Result.Arguments)
	}
}

func TestExecuteDocumentQueryRejectsInvalidLookupAndLimits(t *testing.T) {
	t.Parallel()
	instance := New(BuildInfo{})
	opened, err := instance.OpenDocument(context.Background(), OpenDocumentInput{
		CompileInput:    projectCompileInput(structuralQuerySource()),
		RequestedLimits: generousWorkbenchLimits,
	})
	if err != nil {
		t.Fatal(err)
	}
	base := ExecuteDocumentQueryInput{
		DocumentGeneration: opened.DocumentGeneration,
		Limits:             generousWorkbenchLimits,
		QueryAddress:       "ldl:project:p:query:prod_scope",
	}
	if _, err := instance.ExecuteDocumentQuery(context.Background(), ExecuteDocumentQueryInput{DocumentGeneration: opened.DocumentGeneration, Limits: generousWorkbenchLimits}); !IsWorkbenchError(err, WorkbenchErrorInputInvalid) {
		t.Fatalf("empty query address error = %v", err)
	}
	missing := base
	missing.QueryAddress = "ldl:project:p:query:missing"
	if _, err := instance.ExecuteDocumentQuery(context.Background(), missing); !IsWorkbenchError(err, WorkbenchErrorNotFound) {
		t.Fatalf("missing query error = %v", err)
	}
	limited := base
	limited.Limits = WorkbenchLimits{MaxItems: 1, MaxOutputBytes: generousWorkbenchLimits.MaxOutputBytes}
	if _, err := instance.ExecuteDocumentQuery(context.Background(), limited); !IsWorkbenchError(err, WorkbenchErrorLimitExceeded) {
		t.Fatalf("item limit error = %v", err)
	}
	byteLimited := base
	byteLimited.Limits = WorkbenchLimits{MaxItems: generousWorkbenchLimits.MaxItems, MaxOutputBytes: 1}
	if _, err := instance.ExecuteDocumentQuery(context.Background(), byteLimited); !IsWorkbenchError(err, WorkbenchErrorLimitExceeded) {
		t.Fatalf("output byte limit error = %v", err)
	}
	invalidArguments := base
	invalidArguments.Arguments = map[string]TypedScalar{
		"ldl:project:p:query:prod_scope:parameter:unknown": {Type: definition.ScalarEnum, String: "prod"},
	}
	if _, err := instance.ExecuteDocumentQuery(context.Background(), invalidArguments); err == nil {
		t.Fatal("invalid query arguments were accepted")
	} else if rejection, ok := err.(*QueryExecutionRejection); !ok || len(rejection.Diagnostics) == 0 {
		t.Fatalf("invalid query arguments error = %#v", err)
	}
}

func TestExecuteQueryTraversesIncomingAndReportsCycles(t *testing.T) {
	t.Parallel()
	engine := New(BuildInfo{})
	graphValue := graph.MasterGraph{
		Entities: []graph.Entity{
			{ID: "a", Address: "a", DisplayName: "A", TypeAddress: "service", LayerAddress: "app"},
			{ID: "b", Address: "b", DisplayName: "B", TypeAddress: "service", LayerAddress: "app"},
		},
		Relations: []graph.Relation{
			{ID: "ab", Address: "ab", TypeAddress: "calls", FromAddress: "a", ToAddress: "b"},
			{ID: "ba", Address: "ba", TypeAddress: "calls", FromAddress: "b", ToAddress: "a"},
		},
		Outgoing: []graph.Adjacency{{EntityAddress: "a", RelationAddresses: []string{"ab"}}, {EntityAddress: "b", RelationAddresses: []string{"ba"}}},
		Incoming: []graph.Adjacency{{EntityAddress: "a", RelationAddresses: []string{"ba"}}, {EntityAddress: "b", RelationAddresses: []string{"ab"}}},
	}
	root := []string{"a"}
	relationTypes := []string{"calls"}
	recipe := CompiledQueryRecipe{
		Address: "q", StateInput: query.StateNone,
		Select:        query.Select{RootAddresses: &root, RelationTypeAddresses: &relationTypes},
		Where:         query.Predicate{Kind: query.PredicateAll},
		RelationWhere: query.Predicate{Kind: query.PredicateAll},
		Traversal:     &query.Traversal{Direction: definition.TraversalBoth, MinDepth: 1, MaxDepth: 2, CyclePolicy: query.CycleIncludeCycleRef, RelationTypeAddresses: &relationTypes},
		Result:        []query.ResultMember{query.ResultSeedEntities, query.ResultTraversedEntities, query.ResultPathRelations},
	}
	response, err := engine.ExecuteQuery(context.Background(), QueryExecutionInput{Recipe: recipe, Graph: graphValue})
	if err != nil {
		t.Fatal(err)
	}
	if response.Status != "ok" || response.Result == nil {
		t.Fatalf("ExecuteQuery() = %+v", response)
	}
	if !reflect.DeepEqual(response.Result.ReachedEntityAddresses, []string{"b"}) {
		t.Fatalf("reached = %v", response.Result.ReachedEntityAddresses)
	}
	if len(response.Result.CycleRefs) != 3 || response.Result.CycleRefs[0].Kind != "cycle" || response.Result.CycleRefs[2].Kind != "merge" {
		t.Fatalf("cycle refs = %+v", response.Result.CycleRefs)
	}

	recipe.Traversal.CyclePolicy = query.CycleError
	rejected, err := engine.ExecuteQuery(context.Background(), QueryExecutionInput{Recipe: recipe, Graph: graphValue})
	if err != nil {
		t.Fatal(err)
	}
	if rejected.Status != "rejected" || len(rejected.Diagnostics) == 0 || rejected.Diagnostics[0].Code != "LDL1603" {
		t.Fatalf("cycle error response = %+v", rejected)
	}
}

func TestQueryPredicatePrimitiveComparisons(t *testing.T) {
	t.Parallel()
	executor := &queryExecutor{ctx: context.Background(), limits: DefaultQueryExecutionLimits()}
	entity := graph.Entity{
		ID: "alpha", Address: "entity:alpha", DisplayName: "Alpha", TypeAddress: "service", LayerAddress: "app",
		Common: definition.Common{Description: stringPointer("Primary API"), Tags: []string{"critical", "api"}},
	}
	relationName := "Calls"
	relation := graph.Relation{
		ID: "calls", Address: "relation:calls", DisplayName: &relationName, TypeAddress: "calls",
		FromAddress: "entity:alpha", ToAddress: "entity:beta", Common: definition.Common{Tags: []string{"sync"}},
	}
	if !executor.compareAddress("entity:alpha", query.OperatorEqual, &query.PredicateValue{Kind: query.ValueLiteral, Address: stringPointer("entity:alpha")}) {
		t.Fatal("address equality failed")
	}
	if !executor.compareAddress("entity:alpha", query.OperatorIn, &query.PredicateValue{Kind: query.ValueLiteral, Addresses: []string{"entity:alpha", "entity:beta"}}) {
		t.Fatal("address membership failed")
	}
	if !executor.compareStringSet(entity.Tags, query.OperatorContains, &query.PredicateValue{Kind: query.ValueLiteral, Scalar: &definition.Scalar{Type: definition.ScalarString, String: "critical"}}) {
		t.Fatal("tag contains failed")
	}
	if !executor.compareStringSet(entity.Tags, query.OperatorEqual, &query.PredicateValue{Kind: query.ValueLiteral, Scalars: []definition.Scalar{{Type: definition.ScalarString, String: "api"}, {Type: definition.ScalarString, String: "critical"}}}) {
		t.Fatal("tag equality failed")
	}
	if !executor.compareScalar(definition.Scalar{Type: definition.ScalarString, String: "Alpha"}, query.OperatorStartsWith, &query.PredicateValue{Kind: query.ValueLiteral, Scalar: &definition.Scalar{Type: definition.ScalarString, String: "Al"}}) {
		t.Fatal("string prefix failed")
	}
	if !executor.compareScalar(definition.Scalar{Type: definition.ScalarInteger, Int: 7}, query.OperatorGreaterEq, &query.PredicateValue{Kind: query.ValueLiteral, Scalar: &definition.Scalar{Type: definition.ScalarInteger, Int: 7}}) {
		t.Fatal("integer comparison failed")
	}
	if !executor.compareScalar(definition.Scalar{Type: definition.ScalarNumber, Float: 1.5}, query.OperatorLess, &query.PredicateValue{Kind: query.ValueLiteral, Scalar: &definition.Scalar{Type: definition.ScalarNumber, Float: 2.5}}) {
		t.Fatal("number comparison failed")
	}
	if !executor.compareScalar(definition.Scalar{Type: definition.ScalarEnum, String: "prod"}, query.OperatorIn, &query.PredicateValue{Kind: query.ValueLiteral, Scalars: []definition.Scalar{{Type: definition.ScalarEnum, String: "prod"}}}) {
		t.Fatal("scalar membership failed")
	}
	if !entityFieldValue(entity, "description").present || !entityFieldValue(entity, "address").present || !entityFieldValue(entity, "tags").present {
		t.Fatal("entity fields should be present")
	}
	if !relationFieldValue(relation, "display_name").present || !relationFieldValue(relation, "from").present || !relationFieldValue(relation, "tags").present {
		t.Fatal("relation fields should be present")
	}
	if executor.compareAddress("entity:alpha", query.OperatorEqual, &query.PredicateValue{Kind: query.ValueLiteral, Address: stringPointer("entity:beta")}) {
		t.Fatal("address mismatch was accepted")
	}
}

func TestQueryEvaluatorPredicateBranches(t *testing.T) {
	t.Parallel()
	row := graph.AttributeRow{Address: "row:primary", Values: []graph.Cell{{ColumnAddress: "column:env", Value: definition.Scalar{Type: definition.ScalarEnum, String: "prod"}}}}
	entity := graph.Entity{
		ID: "alpha", Address: "entity:alpha", DisplayName: "Alpha", TypeAddress: "service", LayerAddress: "app",
		Rows: []graph.AttributeRow{row}, Common: definition.Common{Tags: []string{"critical"}},
	}
	relation := graph.Relation{ID: "r", Address: "relation:r", TypeAddress: "calls", FromAddress: "entity:alpha", ToAddress: "entity:beta", Rows: []graph.AttributeRow{row}}
	executor := newQueryExecutor(context.Background(), QueryExecutionInput{Recipe: CompiledQueryRecipe{Address: "query:test", StateInput: query.StateNone}, Graph: graph.MasterGraph{Entities: []graph.Entity{entity}, Relations: []graph.Relation{relation}}}, DefaultQueryExecutionLimits())

	fieldTrue := query.Predicate{Kind: query.PredicateField, Field: "display_name", Operator: query.OperatorEqual, Value: &query.PredicateValue{Kind: query.ValueLiteral, Scalar: &definition.Scalar{Type: definition.ScalarString, String: "Alpha"}}}
	fieldFalse := query.Predicate{Kind: query.PredicateField, Field: "tags", Operator: query.OperatorContains, Value: &query.PredicateValue{Kind: query.ValueLiteral, Scalar: &definition.Scalar{Type: definition.ScalarString, String: "missing"}}}
	if !executor.evalEntityPredicate(query.Predicate{Kind: query.PredicateAll, Children: []query.Predicate{fieldTrue}}, entity) {
		t.Fatal("all predicate rejected a true child")
	}
	if executor.evalEntityPredicate(query.Predicate{Kind: query.PredicateAny, Children: []query.Predicate{fieldFalse}}, entity) {
		t.Fatal("any predicate accepted false children")
	}
	if !executor.evalEntityPredicate(query.Predicate{Kind: query.PredicateNot, Child: &fieldFalse}, entity) {
		t.Fatal("not predicate rejected false child")
	}
	rowPredicate := query.Predicate{
		Kind: query.PredicateRows, Quantifier: query.RowsAny, TypeAddresses: []string{"service"},
		Row: &query.RowPredicate{Kind: query.PredicateCell, ColumnAddresses: []string{"column:env"}, Operator: query.OperatorEqual, Value: &query.PredicateValue{Kind: query.ValueLiteral, Scalar: &definition.Scalar{Type: definition.ScalarEnum, String: "prod"}}},
	}
	if !executor.evalEntityPredicate(rowPredicate, entity) {
		t.Fatal("row predicate rejected matching row")
	}
	rowPredicate.Quantifier = query.RowsAll
	if !executor.evalEntityPredicate(rowPredicate, entity) {
		t.Fatal("all rows rejected matching row")
	}
	rowPredicate.Quantifier = query.RowsNone
	if executor.evalEntityPredicate(rowPredicate, entity) {
		t.Fatal("none rows accepted matching row")
	}
	relationPredicate := query.Predicate{Kind: query.PredicateAll, Children: []query.Predicate{
		{Kind: query.PredicateField, Field: "from", Operator: query.OperatorEqual, Value: &query.PredicateValue{Kind: query.ValueLiteral, Address: stringPointer("entity:alpha")}},
	}}
	if !executor.evalRelationPredicate(relationPredicate, relation) {
		t.Fatal("relation predicate rejected matching from endpoint")
	}
	if executor.evalRelationPredicate(query.Predicate{Kind: query.PredicateNot, Child: &relationPredicate}, relation) {
		t.Fatal("relation not predicate accepted true child")
	}
	if executor.evalEntityPredicate(query.Predicate{Kind: "bogus"}, entity) || executor.evalRelationPredicate(query.Predicate{Kind: "bogus"}, relation) || executor.evalRowPredicate(query.RowPredicate{Kind: "bogus"}, row) {
		t.Fatal("unknown predicates must be false")
	}
}

func TestQueryArgumentSchemaBranches(t *testing.T) {
	t.Parallel()
	enumParameter := query.Parameter{ValueType: definition.ScalarEnum, EnumValues: []string{"prod"}}
	if !argumentMatchesParameter(definition.Scalar{Type: definition.ScalarEnum, String: "prod"}, enumParameter) {
		t.Fatal("enum argument rejected")
	}
	if argumentMatchesParameter(definition.Scalar{Type: definition.ScalarEnum, String: "stg"}, enumParameter) {
		t.Fatal("unknown enum argument accepted")
	}
	minimum, maximum := 1.0, 3.0
	numberParameter := query.Parameter{ValueType: definition.ScalarNumber, Min: &minimum, Max: &maximum}
	if !argumentMatchesParameter(definition.Scalar{Type: definition.ScalarNumber, Float: 2}, numberParameter) {
		t.Fatal("bounded number rejected")
	}
	if argumentMatchesParameter(definition.Scalar{Type: definition.ScalarNumber, Float: 4}, numberParameter) {
		t.Fatal("out-of-range number accepted")
	}
	minLength, maxLength := int64(2), int64(4)
	stringParameter := query.Parameter{ValueType: definition.ScalarString, MinLength: &minLength, MaxLength: &maxLength}
	if !argumentMatchesParameter(definition.Scalar{Type: definition.ScalarString, String: "api"}, stringParameter) {
		t.Fatal("bounded string rejected")
	}
	if argumentMatchesParameter(definition.Scalar{Type: definition.ScalarString, String: "a"}, stringParameter) {
		t.Fatal("short string accepted")
	}
	if compareInt(1, 0) <= 0 || compareInt(0, 1) >= 0 || compareFloat(1, 0) <= 0 || compareFloat(1, 1) != 0 {
		t.Fatal("comparison helpers returned invalid ordering")
	}
}

func compileQueryExecutionFixture(t *testing.T, source string) Snapshot {
	t.Helper()
	result, err := New(BuildInfo{}).Compile(context.Background(), CompileInput{
		Mode:      CompileProject,
		EntryPath: "document.ldl",
		ProjectSourceTree: map[string][]byte{
			"document.ldl": []byte(source),
		},
		ResolvedDependencies: ResolvedDependencies{Format: "layerdraw-resolved", FormatVersion: 1, Language: 1},
	})
	if err != nil {
		t.Fatal(err)
	}
	snapshot := result.Snapshot()
	if len(snapshot.Diagnostics) != 0 {
		t.Fatalf("compile diagnostics = %+v", snapshot.Diagnostics)
	}
	if len(snapshot.TypedAST.Queries) != 1 || snapshot.TypedAST.Graph == nil {
		t.Fatalf("compiled snapshot = %+v", snapshot.TypedAST)
	}
	return snapshot
}

func stringPointer(value string) *string {
	return &value
}

func structuralQuerySource() string {
	return `
project p "Project" {}

layers {
  app "Application" @10
  data "Data" @20
}

entity_type service "Service" {
  representation shape rect
  columns {
    environment "Environment" enum [prod, stg] required default prod
    capacity "Capacity" number min 0
  }
}

relation_type calls "Calls" data_flow {
  duplicate_policy allow
  from caller types [service] layers [app, data]
  to callee types [service] layers [app, data]
  label "calls"
  columns {
    protocol "Protocol" enum [http, grpc] required
  }
}

entities service @app {
  alpha "Alpha" {
    tags [critical]
  }
  beta "Beta"
}

entities service @data {
  gamma "Gamma"
}

rows service [environment, capacity] {
  alpha primary: prod, 75
  beta primary: prod, 25
  gamma primary: stg, 50
}

relations calls {
  alpha_beta: alpha -> beta
  beta_gamma: beta -> gamma
}

relation_rows calls [protocol] {
  alpha_beta primary: http
  beta_gamma primary: grpc
}

query prod_scope "Prod Scope" {
  parameters {
    environment enum [prod, stg] required default prod
  }
  select {
    layers [app, data]
    entity_types [service]
    relation_types [calls]
    roots [alpha]
  }
  where all {
    rows any types [service] {
      cell environment == $environment
    }
  }
  relation_where all {
    rows any types [calls] {
      cell protocol == http
    }
  }
  traverse outgoing 1..2 visit_once relations [calls]
  result [seed_entities, traversed_entities, path_relations, induced_relations]
}
`
}

func requiredStateQuerySource() string {
	return `
project p "Project" {}
layers {
  app "Application" @10
}
entity_type service "Service" {
  representation shape rect
}
entities service @app {
  alpha "Alpha"
}
query stale "Stale" {
  state_input required
  select {
    roots [alpha]
  }
  where all {
    state system.updated_at exists
  }
}
`
}

func optionalStateQuerySource() string {
	return `
project p "Project" {}
layers {
  app "Application" @10
}
entity_type service "Service" {
  representation shape rect
}
entities service @app {
  alpha "Alpha"
}
query stale "Stale" {
  state_input optional
  select {
    roots [alpha]
  }
  where all {
    state system.updated_at exists
  }
}
`
}
