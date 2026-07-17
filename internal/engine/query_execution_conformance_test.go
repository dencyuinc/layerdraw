// SPDX-License-Identifier: LicenseRef-LayerDraw-1.0

package engine

import (
	"context"
	"errors"
	"fmt"
	"math"
	"reflect"
	"strings"
	"testing"

	"github.com/dencyuinc/layerdraw/internal/engine/internal/compiler/query"
	"github.com/dencyuinc/layerdraw/internal/engine/internal/compiler/semantic/definition"
	"github.com/dencyuinc/layerdraw/internal/engine/internal/compiler/semantic/graph"
)

func TestExecuteQuerySeparatesOperationalFailures(t *testing.T) {
	t.Parallel()
	engine := New(BuildInfo{})
	cancelled, cancel := context.WithCancel(context.Background())
	cancel()
	response, err := engine.ExecuteQuery(cancelled, QueryExecutionInput{})
	if !reflect.DeepEqual(response, QueryExecutionResponse{}) || !IsQueryExecutionError(err, QueryExecutionErrorCancelled) || !errors.Is(err, context.Canceled) {
		t.Fatalf("cancelled ExecuteQuery() = (%+v, %v)", response, err)
	}

	graphValue := graph.MasterGraph{Entities: []graph.Entity{{Address: "entity:a"}}}
	response, err = engine.ExecuteQuery(context.Background(), QueryExecutionInput{Graph: graphValue, Limits: QueryExecutionLimits{MaxWork: 1, MaxItems: 100}})
	if !reflect.DeepEqual(response, QueryExecutionResponse{}) || !IsQueryExecutionError(err, QueryExecutionErrorResource) {
		t.Fatalf("work-limited ExecuteQuery() = (%+v, %v)", response, err)
	}

	root := []string{"entity:a"}
	recipe := emptyQueryRecipe("query:limited")
	recipe.Select.RootAddresses = &root
	response, err = engine.ExecuteQuery(context.Background(), QueryExecutionInput{Recipe: recipe, Graph: graphValue, Limits: QueryExecutionLimits{MaxItems: 1}})
	if !reflect.DeepEqual(response, QueryExecutionResponse{}) || !IsQueryExecutionError(err, QueryExecutionErrorResource) {
		t.Fatalf("item-limited ExecuteQuery() = (%+v, %v)", response, err)
	}
}

func TestQueryArgumentsUseCompilerScalarNormalization(t *testing.T) {
	t.Parallel()
	minimum, maximum := int64(1), int64(8)
	parameter := query.Parameter{Address: "query:q:parameter:name", ValueType: definition.ScalarString, MinLength: &minimum, MaxLength: &maximum}
	normalized, ok := normalizeQueryArgument(definition.Scalar{Type: definition.ScalarString, String: "e\u0301", Int: 99}, parameter)
	if !ok || normalized != (definition.Scalar{Type: definition.ScalarString, String: "é"}) {
		t.Fatalf("normalized string = %+v, %v", normalized, ok)
	}

	format := definition.StringFormatHostname
	parameter.Format = &format
	if _, ok := normalizeQueryArgument(definition.Scalar{Type: definition.ScalarString, String: "bad host"}, parameter); ok {
		t.Fatal("invalid formatted string was accepted")
	}
	dateParameter := query.Parameter{ValueType: definition.ScalarDate}
	if _, ok := normalizeQueryArgument(definition.Scalar{Type: definition.ScalarDate, String: "2025-02-29"}, dateParameter); ok {
		t.Fatal("invalid date was accepted")
	}
	datetimeParameter := query.Parameter{ValueType: definition.ScalarDatetime}
	datetime, ok := normalizeQueryArgument(definition.Scalar{Type: definition.ScalarDatetime, String: "2026-07-17T12:00:00+09:00"}, datetimeParameter)
	if !ok || datetime.String != "2026-07-17T03:00:00Z" {
		t.Fatalf("normalized datetime = %+v, %v", datetime, ok)
	}
	numberParameter := query.Parameter{ValueType: definition.ScalarNumber}
	if _, ok := normalizeQueryArgument(definition.Scalar{Type: definition.ScalarNumber, Float: math.NaN()}, numberParameter); ok {
		t.Fatal("NaN was accepted")
	}
	negativeZero, ok := normalizeQueryArgument(definition.Scalar{Type: definition.ScalarNumber, Float: math.Copysign(0, -1)}, numberParameter)
	if !ok || negativeZero.Float != 0 || math.Signbit(negativeZero.Float) {
		t.Fatalf("negative zero was not canonicalized: %+v, %v", negativeZero, ok)
	}
	enumParameter := query.Parameter{ValueType: definition.ScalarEnum, EnumValues: []string{"é"}}
	if normalized, ok := normalizeQueryArgument(definition.Scalar{Type: definition.ScalarEnum, String: "e\u0301"}, enumParameter); !ok || normalized.String != "é" {
		t.Fatalf("normalized enum = %+v, %v", normalized, ok)
	}
	if _, ok := normalizeQueryArgument(definition.Scalar{Type: definition.ScalarEnum, String: "ê"}, enumParameter); ok {
		t.Fatal("different multibyte enum value was accepted")
	}

	recipe := emptyQueryRecipe("query:q")
	recipe.Parameters = []query.Parameter{{Address: "query:q:parameter:name", ValueType: definition.ScalarString, Required: true}}
	response, err := New(BuildInfo{}).ExecuteQuery(context.Background(), QueryExecutionInput{
		Recipe: recipe,
		Arguments: map[string]TypedScalar{
			"query:q:parameter:name": {Type: definition.ScalarString, String: "e\u0301", Int: 99},
		},
	})
	if err != nil || response.Status != "ok" || response.Result.Arguments["query:q:parameter:name"] != (definition.Scalar{Type: definition.ScalarString, String: "é"}) {
		t.Fatalf("ExecuteQuery() arguments = (%+v, %v)", response, err)
	}
}

func TestQueryArgumentNormalizationHonorsWorkAndCancellation(t *testing.T) {
	t.Parallel()
	enumValues := make([]string, 1_000)
	for index := range enumValues {
		enumValues[index] = fmt.Sprintf("value_%04d", index)
	}
	parameter := query.Parameter{
		Address:    "query:q:parameter:environment",
		ValueType:  definition.ScalarEnum,
		EnumValues: enumValues,
		Required:   true,
	}
	recipe := emptyQueryRecipe("query:q")
	recipe.Parameters = []query.Parameter{parameter}
	_, err := New(BuildInfo{}).ExecuteQuery(context.Background(), QueryExecutionInput{
		Recipe: recipe,
		Arguments: map[string]TypedScalar{
			parameter.Address: {Type: definition.ScalarEnum, String: "missing"},
		},
		Limits: QueryExecutionLimits{MaxItems: 100, MaxWork: 30},
	})
	if !IsQueryExecutionError(err, QueryExecutionErrorResource) {
		t.Fatalf("enum work limit error = %v", err)
	}

	cancelled := &queryExecutor{
		ctx:    &stagedCancelContext{remaining: 10},
		limits: QueryExecutionLimits{MaxItems: 100, MaxWork: 10_000},
	}
	if _, valid := cancelled.normalizeQueryArgument(definition.Scalar{Type: definition.ScalarEnum, String: "missing"}, parameter); valid ||
		!IsQueryExecutionError(cancelled.err, QueryExecutionErrorCancelled) || !errors.Is(cancelled.err, context.Canceled) {
		t.Fatalf("cancelled enum normalization = (valid=%v, err=%v)", valid, cancelled.err)
	}
}

func TestScalarCompareOrdersDatetimesChronologically(t *testing.T) {
	t.Parallel()
	wholeSecond := definition.Scalar{Type: definition.ScalarDatetime, String: "2026-07-17T12:00:00Z"}
	fractionalSecond := definition.Scalar{Type: definition.ScalarDatetime, String: "2026-07-17T12:00:00.1Z"}
	if comparison, ok := scalarCompare(wholeSecond, fractionalSecond); !ok || comparison >= 0 {
		t.Fatalf("whole second comparison = (%d, %v), want chronological less-than", comparison, ok)
	}
	if comparison, ok := scalarCompare(fractionalSecond, wholeSecond); !ok || comparison <= 0 {
		t.Fatalf("fractional second comparison = (%d, %v), want chronological greater-than", comparison, ok)
	}
}

func TestQueryStableSortAccountsForWorkAndCancellation(t *testing.T) {
	t.Parallel()
	executor := &queryExecutor{ctx: context.Background(), limits: QueryExecutionLimits{MaxWork: 100}}
	if got := queryStableSort(executor, []string{"c", "a", "b"}, strings.Compare); !reflect.DeepEqual(got, []string{"a", "b", "c"}) || executor.err != nil || executor.work == 0 {
		t.Fatalf("queryStableSort() = (%v, work=%d, err=%v)", got, executor.work, executor.err)
	}

	limited := &queryExecutor{ctx: context.Background(), limits: QueryExecutionLimits{MaxWork: 2}}
	if got := queryStableSort(limited, []string{"c", "a", "b"}, strings.Compare); got != nil || !IsQueryExecutionError(limited.err, QueryExecutionErrorResource) {
		t.Fatalf("limited queryStableSort() = (%v, %v)", got, limited.err)
	}

	cancelledContext, cancel := context.WithCancel(context.Background())
	cancel()
	cancelled := &queryExecutor{ctx: cancelledContext, limits: QueryExecutionLimits{MaxWork: 100}}
	if got := queryStableSort(cancelled, []string{"b", "a"}, strings.Compare); got != nil || !IsQueryExecutionError(cancelled.err, QueryExecutionErrorCancelled) || !errors.Is(cancelled.err, context.Canceled) {
		t.Fatalf("cancelled queryStableSort() = (%v, %v)", got, cancelled.err)
	}
}

func TestDeepTraversalChargesRetainedPathWork(t *testing.T) {
	t.Parallel()
	const entityCount = 96
	entities := make([]graph.Entity, entityCount)
	relations := make([]graph.Relation, 0, entityCount-1)
	outgoing := make([]graph.Adjacency, 0, entityCount-1)
	for index := range entities {
		entities[index].Address = fmt.Sprintf("entity:%03d", index)
		if index == 0 {
			continue
		}
		relationAddress := fmt.Sprintf("relation:%03d", index)
		relations = append(relations, graph.Relation{Address: relationAddress, FromAddress: entities[index-1].Address, ToAddress: entities[index].Address})
		outgoing = append(outgoing, graph.Adjacency{EntityAddress: entities[index-1].Address, RelationAddresses: []string{relationAddress}})
	}
	root := []string{entities[0].Address}
	recipe := emptyQueryRecipe("query:deep")
	recipe.Select.RootAddresses = &root
	recipe.Traversal = &query.Traversal{Direction: definition.TraversalOutgoing, MinDepth: 1, MaxDepth: entityCount - 1, CyclePolicy: query.CycleVisitOnce}

	response, err := New(BuildInfo{}).ExecuteQuery(context.Background(), QueryExecutionInput{
		Recipe: recipe,
		Graph:  graph.MasterGraph{Entities: entities, Relations: relations, Outgoing: outgoing},
		Limits: QueryExecutionLimits{MaxItems: 1_000, MaxWork: 10_000},
	})
	if !reflect.DeepEqual(response, QueryExecutionResponse{}) || !IsQueryExecutionError(err, QueryExecutionErrorResource) {
		t.Fatalf("deep traversal = (%+v, %v), want bounded query_work failure", response, err)
	}
}

func TestQueryExecutionErrorAndWorkbenchMappingContracts(t *testing.T) {
	t.Parallel()
	var nilExecutionError *QueryExecutionError
	if nilExecutionError.Error() != "<nil>" || nilExecutionError.Unwrap() != nil {
		t.Fatalf("nil query execution error = (%q, %v)", nilExecutionError.Error(), nilExecutionError.Unwrap())
	}
	cause := errors.New("cause")
	resource := &QueryExecutionError{Code: "engine.query.limit_exceeded", Category: QueryExecutionErrorResource, Resource: "query_work", Limit: 2, Observed: 3, cause: cause}
	if resource.Error() != "engine.query.limit_exceeded: query_work observed 3 exceeds limit 2" || !errors.Is(resource, cause) {
		t.Fatalf("resource error = %q, unwrap=%v", resource.Error(), resource.Unwrap())
	}
	plain := &QueryExecutionError{Code: "engine.query.invariant", Category: QueryExecutionErrorInvariant}
	if plain.Error() != plain.Code {
		t.Fatalf("plain error = %q", plain.Error())
	}

	for _, test := range []struct {
		name     string
		err      error
		category WorkbenchErrorCategory
	}{
		{"cancelled", &QueryExecutionError{Category: QueryExecutionErrorCancelled}, WorkbenchErrorCancelled},
		{"resource", resource, WorkbenchErrorLimitExceeded},
		{"invariant", plain, WorkbenchErrorInvariant},
		{"foreign", cause, WorkbenchErrorInvariant},
	} {
		t.Run(test.name, func(t *testing.T) {
			mapped := mapQueryExecutionWorkbenchError(test.err)
			if !IsWorkbenchError(mapped, test.category) {
				t.Fatalf("mapped error = %v, want %s", mapped, test.category)
			}
		})
	}
	if (&QueryExecutionRejection{}).Error() != "engine.workbench.query_rejected" {
		t.Fatal("query rejection error code changed")
	}
}

func TestQueryComparisonAndOrderingHelpers(t *testing.T) {
	t.Parallel()
	executor := &queryExecutor{ctx: context.Background(), limits: QueryExecutionLimits{MaxWork: 10_000}}
	address := "entity:a"
	otherAddress := "entity:b"
	if !executor.compareAddress(address, query.OperatorNotEqual, &query.PredicateValue{Address: &otherAddress}) ||
		!executor.compareAddress(address, query.OperatorNotIn, &query.PredicateValue{Addresses: []string{otherAddress}}) ||
		executor.compareAddress(address, query.OperatorEqual, &query.PredicateValue{Address: &otherAddress}) ||
		executor.compareAddress(address, query.OperatorContains, &query.PredicateValue{Address: &address}) {
		t.Fatal("address comparison branches disagree")
	}

	stringScalar := func(value string) definition.Scalar {
		return definition.Scalar{Type: definition.ScalarString, String: value}
	}
	if !executor.compareStringSet([]string{"b", "a"}, query.OperatorNotEqual, &query.PredicateValue{Scalars: []definition.Scalar{stringScalar("a")}}) ||
		executor.compareStringSet([]string{"a"}, query.OperatorContains, &query.PredicateValue{}) ||
		executor.compareStringSet([]string{"a"}, query.OperatorIn, &query.PredicateValue{}) {
		t.Fatal("string-set comparison branches disagree")
	}

	leftString := stringScalar("alphabet")
	for _, test := range []struct {
		operator query.Operator
		right    definition.Scalar
		want     bool
	}{
		{query.OperatorEqual, stringScalar("alphabet"), true},
		{query.OperatorNotEqual, stringScalar("other"), true},
		{query.OperatorContains, stringScalar("pha"), true},
		{query.OperatorEndsWith, stringScalar("bet"), true},
		{query.OperatorStartsWith, stringScalar("bet"), false},
	} {
		right := test.right
		if got := executor.compareScalar(leftString, test.operator, &query.PredicateValue{Scalar: &right}); got != test.want {
			t.Errorf("compareScalar(%s) = %v, want %v", test.operator, got, test.want)
		}
	}
	if executor.compareScalar(leftString, query.OperatorLess, &query.PredicateValue{}) || executor.compareScalar(leftString, query.OperatorExists, &query.PredicateValue{}) {
		t.Fatal("invalid scalar comparison was accepted")
	}

	integerOne := definition.Scalar{Type: definition.ScalarInteger, Int: 1}
	integerTwo := definition.Scalar{Type: definition.ScalarInteger, Int: 2}
	numberOne := definition.Scalar{Type: definition.ScalarNumber, Float: 1.5}
	numberTwo := definition.Scalar{Type: definition.ScalarNumber, Float: 2.5}
	dateOne := definition.Scalar{Type: definition.ScalarDate, String: "2026-01-01"}
	dateTwo := definition.Scalar{Type: definition.ScalarDate, String: "2026-01-02"}
	for _, pair := range [][2]definition.Scalar{{integerOne, integerTwo}, {numberOne, numberTwo}, {dateOne, dateTwo}} {
		if comparison, ok := scalarCompare(pair[0], pair[1]); !ok || comparison >= 0 {
			t.Errorf("scalarCompare(%+v, %+v) = (%d, %v)", pair[0], pair[1], comparison, ok)
		}
	}
	if _, ok := scalarCompare(integerOne, numberOne); ok {
		t.Fatal("cross-type scalar comparison was accepted")
	}
	if _, ok := scalarCompare(definition.Scalar{Type: definition.ScalarNumber, Float: math.Inf(1)}, numberOne); ok {
		t.Fatal("infinite number comparison was accepted")
	}
	if _, ok := scalarCompare(definition.Scalar{Type: definition.ScalarDatetime, String: "invalid"}, definition.Scalar{Type: definition.ScalarDatetime, String: "also-invalid"}); ok {
		t.Fatal("invalid datetime comparison was accepted")
	}

	for _, scalar := range []definition.Scalar{
		stringScalar("value"),
		{Type: definition.ScalarEnum, String: "value"},
		{Type: definition.ScalarDate, String: "2026-01-01"},
		{Type: definition.ScalarDatetime, String: "2026-01-01T00:00:00Z"},
		{Type: definition.ScalarInteger, Int: 1},
		{Type: definition.ScalarNumber, Float: 1},
		{Type: definition.ScalarBoolean, Bool: true},
	} {
		if !scalarsEqual(scalar, scalar) {
			t.Errorf("equal scalar rejected: %+v", scalar)
		}
	}
	if scalarsEqual(integerOne, numberOne) || scalarsEqual(definition.Scalar{Type: definition.ScalarNumber, Float: math.NaN()}, definition.Scalar{Type: definition.ScalarNumber, Float: math.NaN()}) || scalarsEqual(definition.Scalar{}, definition.Scalar{}) {
		t.Fatal("invalid scalar equality was accepted")
	}

	leftPath := QueryPath{EntityAddresses: []string{"entity:a"}, RelationAddresses: []string{}}
	rightPath := QueryPath{EntityAddresses: []string{"entity:a", "entity:b"}, RelationAddresses: []string{"relation:ab"}}
	pathExecutor := &queryExecutor{ctx: context.Background(), limits: QueryExecutionLimits{MaxWork: 100}}
	if orientationRank("outgoing") != 0 || orientationRank("incoming") != 1 || pathExecutor.compareQueryPaths(leftPath, rightPath) >= 0 || pathExecutor.compareQueryPaths(rightPath, leftPath) <= 0 || pathExecutor.compareQueryPaths(leftPath, leftPath) != 0 {
		t.Fatal("path or orientation ordering changed")
	}
}

func TestQueryPredicateHelpersCoverEntityRelationAndRows(t *testing.T) {
	t.Parallel()
	description, displayName := "description", "relation"
	entity := graph.Entity{
		ID: "service", Address: "entity:service", DisplayName: "Service", TypeAddress: "entity-type:service", LayerAddress: "layer:app",
		Common: definition.Common{Description: &description, Tags: []string{"prod", "api"}},
		Rows:   []graph.AttributeRow{{Address: "entity:service:row:one", Values: []graph.Cell{{ColumnAddress: "column:name", Value: definition.Scalar{Type: definition.ScalarString, String: "value"}}}}},
	}
	relation := graph.Relation{
		ID: "calls", Address: "relation:calls", DisplayName: &displayName, TypeAddress: "relation-type:calls", FromAddress: "entity:service", ToAddress: "entity:db",
		Common: definition.Common{Description: &description, Tags: []string{"sync"}}, Rows: entity.Rows,
	}
	for _, field := range []string{"id", "display_name", "description", "address", "type", "layer", "tags"} {
		if !entityFieldValue(entity, field).present {
			t.Errorf("entity field %q was absent", field)
		}
	}
	if entityFieldValue(graph.Entity{}, "description").present || entityFieldValue(entity, "unknown").present {
		t.Fatal("absent entity field was present")
	}
	for _, field := range []string{"id", "display_name", "description", "address", "type", "from", "to", "tags"} {
		if !relationFieldValue(relation, field).present {
			t.Errorf("relation field %q was absent", field)
		}
	}
	if relationFieldValue(graph.Relation{}, "display_name").present || relationFieldValue(graph.Relation{}, "description").present || relationFieldValue(relation, "unknown").present {
		t.Fatal("absent relation field was present")
	}

	executor := &queryExecutor{
		ctx: context.Background(), limits: QueryExecutionLimits{MaxWork: 10_000, MaxItems: 100},
		arguments: map[string]definition.Scalar{"parameter:value": {Type: definition.ScalarString, String: "value"}}, stateReads: map[StateReadRef]bool{},
	}
	scalarValue := definition.Scalar{Type: definition.ScalarString, String: "value"}
	literal := &query.PredicateValue{Kind: query.ValueLiteral, Scalar: &scalarValue}
	parameter := &query.PredicateValue{Kind: query.ValueParameter, ParameterAddress: "parameter:value"}
	field := query.Predicate{Kind: query.PredicateField, Field: "id", Operator: query.OperatorEqual, Value: &query.PredicateValue{Scalar: queryTestPointer(definition.Scalar{Type: definition.ScalarString, String: "service"})}}
	missing := query.Predicate{Kind: query.PredicateField, Field: "unknown", Operator: query.OperatorMissing}
	rowCell := query.RowPredicate{Kind: query.PredicateCell, ColumnAddresses: []string{"column:name"}, Operator: query.OperatorEqual, Value: literal}
	rowsAny := query.Predicate{Kind: query.PredicateRows, TypeAddresses: []string{entity.TypeAddress}, Quantifier: query.RowsAny, Row: &rowCell}
	stateMissing := query.Predicate{Kind: query.PredicateState, FieldPath: query.StateSystemUpdatedAt, Operator: query.OperatorMissing}
	for name, predicate := range map[string]query.Predicate{
		"all":     {Kind: query.PredicateAll, Children: []query.Predicate{field, missing}},
		"any":     {Kind: query.PredicateAny, Children: []query.Predicate{{Kind: "unknown"}, field}},
		"not":     {Kind: query.PredicateNot, Child: &missing},
		"field":   field,
		"rows":    rowsAny,
		"state":   stateMissing,
		"unknown": {Kind: "unknown"},
	} {
		got := executor.evalEntityPredicate(predicate, entity)
		want := name != "not" && name != "unknown"
		if got != want {
			t.Errorf("entity predicate %s = %v, want %v", name, got, want)
		}
	}
	relationField := query.Predicate{Kind: query.PredicateField, Field: "id", Operator: query.OperatorEqual, Value: &query.PredicateValue{Scalar: queryTestPointer(definition.Scalar{Type: definition.ScalarString, String: "calls"})}}
	for name, predicate := range map[string]query.Predicate{
		"all":     {Kind: query.PredicateAll, Children: []query.Predicate{relationField, missing}},
		"any":     {Kind: query.PredicateAny, Children: []query.Predicate{{Kind: "unknown"}, relationField}},
		"not":     {Kind: query.PredicateNot, Child: &missing},
		"field":   relationField,
		"rows":    {Kind: query.PredicateRows, TypeAddresses: []string{relation.TypeAddress}, Quantifier: query.RowsAny, Row: &rowCell},
		"state":   stateMissing,
		"unknown": {Kind: "unknown"},
	} {
		got := executor.evalRelationPredicate(predicate, relation)
		want := name != "not" && name != "unknown"
		if got != want {
			t.Errorf("relation predicate %s = %v, want %v", name, got, want)
		}
	}

	row := entity.Rows[0]
	rowNot := query.RowPredicate{Kind: query.PredicateNot, Child: &rowCell}
	for name, predicate := range map[string]query.RowPredicate{
		"all":     {Kind: query.PredicateAll, Children: []query.RowPredicate{rowCell, {Kind: query.PredicateState, FieldPath: query.StateSystemCreatedAt, Operator: query.OperatorMissing}}},
		"any":     {Kind: query.PredicateAny, Children: []query.RowPredicate{{Kind: "unknown"}, rowCell}},
		"not":     rowNot,
		"cell":    rowCell,
		"state":   {Kind: query.PredicateState, FieldPath: query.StateSystemUpdatedAt, Operator: query.OperatorMissing},
		"unknown": {Kind: "unknown"},
	} {
		got := executor.evalRowPredicate(predicate, row)
		want := name != "not" && name != "unknown"
		if got != want {
			t.Errorf("row predicate %s = %v, want %v", name, got, want)
		}
	}
	for _, test := range []struct {
		quantifier query.RowQuantifier
		rows       []graph.AttributeRow
		want       bool
	}{
		{query.RowsAny, entity.Rows, true},
		{query.RowsAll, entity.Rows, true},
		{query.RowsAll, nil, false},
		{query.RowsNone, nil, true},
		{query.RowsNone, entity.Rows, false},
		{"unknown", entity.Rows, false},
	} {
		predicate := query.Predicate{Kind: query.PredicateRows, TypeAddresses: []string{entity.TypeAddress}, Quantifier: test.quantifier, Row: &rowCell}
		if got := executor.evalRowsPredicate(predicate, entity.TypeAddress, test.rows); got != test.want {
			t.Errorf("rows %s = %v, want %v", test.quantifier, got, test.want)
		}
	}
	if !executor.evalRowsPredicate(query.Predicate{Quantifier: query.RowsNone}, entity.TypeAddress, entity.Rows) {
		t.Fatal("missing row contract did not satisfy none")
	}
	if executor.evalRowsPredicate(rowsAny, "entity-type:other", entity.Rows) {
		t.Fatal("foreign row type matched")
	}
	if !executor.evalPredicateValue(optionalScalar{}, query.OperatorMissing, nil) || executor.evalPredicateValue(optionalScalar{}, query.OperatorExists, nil) {
		t.Fatal("missing scalar semantics disagree")
	}
	if !executor.evalPredicateValue(stringValue("value"), query.OperatorExists, nil) || executor.evalPredicateValue(stringValue("value"), query.OperatorMissing, nil) || executor.evalPredicateValue(stringValue("value"), query.OperatorEqual, nil) {
		t.Fatal("present scalar semantics disagree")
	}
	if !executor.evalPredicateValue(stringValue("value"), query.OperatorEqual, parameter) {
		t.Fatal("parameter value was not resolved")
	}
	if executor.resolveParameter(&query.PredicateValue{Kind: query.ValueParameter, ParameterAddress: "missing"}).Scalar != nil {
		t.Fatal("missing parameter resolved to a scalar")
	}
}

func TestQueryAccountingDiagnosticsAndOrderingBranches(t *testing.T) {
	t.Parallel()
	for _, test := range []struct {
		cmp      int
		operator query.Operator
		want     bool
	}{
		{-1, query.OperatorLess, true},
		{0, query.OperatorLessEqual, true},
		{1, query.OperatorGreater, true},
		{0, query.OperatorGreaterEq, true},
		{0, query.OperatorEqual, false},
	} {
		if got := operatorCompare(test.cmp, test.operator); got != test.want {
			t.Errorf("operatorCompare(%d, %s) = %v, want %v", test.cmp, test.operator, got, test.want)
		}
	}

	executor := &queryExecutor{ctx: context.Background(), limits: QueryExecutionLimits{MaxWork: 10_000, MaxItems: 2}, stateReads: map[StateReadRef]bool{}}
	executor.addInfo("LDL1605", "info", "message", "entity:b", "query:q")
	executor.addWarning("LDL1605", "warning", "message", "entity:a", "query:q")
	executor.addDiag("LDL1601", "error", "message", "entity:c", "query:q")
	if !IsQueryExecutionError(executor.err, QueryExecutionErrorResource) || len(executor.diagnostics) != 2 {
		t.Fatalf("diagnostic item limit = (%d, %v)", len(executor.diagnostics), executor.err)
	}

	rich := &queryExecutor{ctx: context.Background(), limits: QueryExecutionLimits{MaxWork: 100, MaxItems: 10}}
	if got := rich.sortedDiagnostics([]Diagnostic{{Code: "LDL1601", Arguments: map[string]string{"x": "y"}}}); got != nil || !IsQueryExecutionError(rich.err, QueryExecutionErrorInvariant) {
		t.Fatalf("rich query diagnostic = (%v, %v)", got, rich.err)
	}
	limited := &queryExecutor{ctx: context.Background(), limits: QueryExecutionLimits{MaxWork: 1, MaxItems: 10}}
	if got := limited.sortedDiagnostics([]Diagnostic{{Code: "LDL1601"}, {Code: "LDL1602"}}); got != nil || !IsQueryExecutionError(limited.err, QueryExecutionErrorResource) {
		t.Fatalf("limited diagnostic sort = (%v, %v)", got, limited.err)
	}

	accounting := &queryExecutor{ctx: context.Background(), limits: QueryExecutionLimits{MaxWork: math.MaxInt64, MaxItems: 10}}
	accounting.work = math.MaxInt64
	if accounting.charge(1) || !IsQueryExecutionError(accounting.err, QueryExecutionErrorResource) {
		t.Fatalf("overflowing charge = %v", accounting.err)
	}
	invalidCharge := &queryExecutor{ctx: context.Background(), limits: QueryExecutionLimits{MaxWork: 10, MaxItems: 10}}
	if invalidCharge.charge(-1) || !IsQueryExecutionError(invalidCharge.err, QueryExecutionErrorInvariant) {
		t.Fatalf("negative charge = %v", invalidCharge.err)
	}

	ordering := &queryExecutor{ctx: context.Background(), limits: QueryExecutionLimits{MaxWork: 10_000, MaxItems: 100}}
	short := QueryPath{EntityAddresses: []string{"entity:a"}}
	long := QueryPath{EntityAddresses: []string{"entity:a", "entity:b"}, RelationAddresses: []string{"relation:a"}}
	otherRelation := QueryPath{EntityAddresses: []string{"entity:a", "entity:b"}, RelationAddresses: []string{"relation:b"}}
	if ordering.compareQueryPaths(short, long) >= 0 || ordering.compareQueryPaths(long, short) <= 0 || ordering.compareQueryPaths(long, otherRelation) >= 0 {
		t.Fatal("path ordering branches disagree")
	}
	refs := []QueryCycleRef{
		{Kind: "z", FromEntityAddress: "entity:b", ToEntityAddress: "entity:a", RelationAddress: "relation:b", Orientation: "incoming", RetainedPath: otherRelation},
		{Kind: "a", FromEntityAddress: "entity:a", ToEntityAddress: "entity:b", RelationAddress: "relation:a", Orientation: "outgoing", RetainedPath: long},
	}
	if got := ordering.sortCycleRefs(refs); len(got) != 2 || got[0].Kind != "a" {
		t.Fatalf("sorted cycle refs = %+v", got)
	}
}

func queryTestPointer[T any](value T) *T {
	return &value
}

func TestQueryStateReadsDoNotDependOnBooleanShortCircuit(t *testing.T) {
	t.Parallel()
	entity := graph.Entity{ID: "a", Address: "entity:a", DisplayName: "A", TypeAddress: "type:service", LayerAddress: "layer:app"}
	root := []string{entity.Address}
	recipe := emptyQueryRecipe("query:q")
	recipe.StateInput = query.StateOptional
	recipe.Select.RootAddresses = &root
	recipe.Where = query.Predicate{Kind: query.PredicateAll, Children: []query.Predicate{
		{Kind: query.PredicateField, Field: "display_name", Operator: query.OperatorEqual, Value: scalarPredicateValue("not-a")},
		{Kind: query.PredicateState, FieldPath: query.StateSystemUpdatedAt, Operator: query.OperatorExists},
	}}
	response, err := New(BuildInfo{}).ExecuteQuery(context.Background(), QueryExecutionInput{Recipe: recipe, Graph: graph.MasterGraph{Entities: []graph.Entity{entity}}})
	if err != nil || response.Status != "ok" || !reflect.DeepEqual(response.Result.StateReads, []StateReadRef{{SubjectAddress: entity.Address, FieldPath: string(query.StateSystemUpdatedAt)}}) {
		t.Fatalf("all predicate state reads = (%+v, %v)", response, err)
	}

	recipe.Where = query.Predicate{Kind: query.PredicateAny, Children: []query.Predicate{
		{Kind: query.PredicateField, Field: "display_name", Operator: query.OperatorEqual, Value: scalarPredicateValue("A")},
		{Kind: query.PredicateState, FieldPath: query.StateProvenanceSourceKind, Operator: query.OperatorExists},
	}}
	response, err = New(BuildInfo{}).ExecuteQuery(context.Background(), QueryExecutionInput{Recipe: recipe, Graph: graph.MasterGraph{Entities: []graph.Entity{entity}}})
	if err != nil || response.Status != "ok" || !reflect.DeepEqual(response.Result.StateReads, []StateReadRef{{SubjectAddress: entity.Address, FieldPath: string(query.StateProvenanceSourceKind)}}) {
		t.Fatalf("any predicate state reads = (%+v, %v)", response, err)
	}
}

func TestQueryRecordsSyntacticallyApplicableRelationAndRowStateReads(t *testing.T) {
	t.Parallel()
	entityA := graph.Entity{ID: "a", Address: "entity:a", DisplayName: "A", TypeAddress: "type:service", LayerAddress: "layer:app"}
	entityB := graph.Entity{ID: "b", Address: "entity:b", DisplayName: "B", TypeAddress: "type:service", LayerAddress: "layer:app"}
	row := graph.AttributeRow{Address: "entity:a:row:primary"}
	entityA.Rows = []graph.AttributeRow{row}
	relation := graph.Relation{ID: "ab", Address: "relation:ab", TypeAddress: "type:calls", FromAddress: entityA.Address, ToAddress: entityB.Address}
	relationTypes := []string{relation.TypeAddress}
	recipe := emptyQueryRecipe("query:q")
	recipe.StateInput = query.StateOptional
	recipe.Select.RelationTypeAddresses = &relationTypes
	recipe.Where = query.Predicate{Kind: query.PredicateAll, Children: []query.Predicate{
		{Kind: query.PredicateField, Field: "display_name", Operator: query.OperatorEqual, Value: scalarPredicateValue("A")},
		{Kind: query.PredicateRows, Quantifier: query.RowsAny, TypeAddresses: []string{entityA.TypeAddress}, Row: &query.RowPredicate{Kind: query.PredicateAll, Children: []query.RowPredicate{
			{Kind: query.PredicateCell, ColumnAddresses: []string{"column:missing"}, Operator: query.OperatorExists},
			{Kind: query.PredicateState, FieldPath: query.StateSystemCreatedAt, Operator: query.OperatorExists},
		}}},
	}}
	recipe.RelationWhere = query.Predicate{Kind: query.PredicateState, FieldPath: query.StateSystemUpdatedAt, Operator: query.OperatorExists}
	graphValue := graph.MasterGraph{Entities: []graph.Entity{entityA, entityB}, Relations: []graph.Relation{relation}}
	response, err := New(BuildInfo{}).ExecuteQuery(context.Background(), QueryExecutionInput{Recipe: recipe, Graph: graphValue})
	want := []StateReadRef{
		{SubjectAddress: row.Address, FieldPath: string(query.StateSystemCreatedAt)},
		{SubjectAddress: relation.Address, FieldPath: string(query.StateSystemUpdatedAt)},
	}
	if err != nil || response.Status != "ok" || !reflect.DeepEqual(response.Result.StateReads, want) {
		t.Fatalf("relation/row state reads = (%+v, %v), want %+v", response, err, want)
	}
}

func TestQueryTraversalCanonicalCycleAndRelationRules(t *testing.T) {
	t.Parallel()
	entities := []graph.Entity{
		{Address: "entity:a", DisplayName: "A"},
		{Address: "entity:b", DisplayName: "B"},
		{Address: "entity:c", DisplayName: "C"},
	}
	relations := []graph.Relation{
		{Address: "relation:ab", TypeAddress: "type:edge", FromAddress: "entity:a", ToAddress: "entity:b"},
		{Address: "relation:ac", TypeAddress: "type:edge", FromAddress: "entity:a", ToAddress: "entity:c"},
		{Address: "relation:bc", TypeAddress: "type:edge", FromAddress: "entity:b", ToAddress: "entity:c"},
	}
	root := []string{"entity:a"}
	relationTypes := []string{"type:edge"}
	recipe := emptyQueryRecipe("query:q")
	recipe.Select.RootAddresses = &root
	recipe.Select.RelationTypeAddresses = &relationTypes
	recipe.Traversal = &query.Traversal{Direction: definition.TraversalOutgoing, MinDepth: 1, MaxDepth: 2, CyclePolicy: query.CycleIncludeCycleRef, RelationTypeAddresses: &relationTypes}
	graphValue := graph.MasterGraph{
		Entities: entities, Relations: relations,
		Outgoing: []graph.Adjacency{{EntityAddress: "entity:a", RelationAddresses: []string{"relation:ac", "relation:ab"}}, {EntityAddress: "entity:b", RelationAddresses: []string{"relation:bc"}}},
	}
	response, err := New(BuildInfo{}).ExecuteQuery(context.Background(), QueryExecutionInput{Recipe: recipe, Graph: graphValue})
	if err != nil || response.Status != "ok" || len(response.Result.CycleRefs) != 1 {
		t.Fatalf("merge traversal = (%+v, %v)", response, err)
	}
	wantRetained := QueryPath{EntityAddresses: []string{"entity:a", "entity:b"}, RelationAddresses: []string{"relation:ab"}}
	if response.Result.CycleRefs[0].Kind != "merge" || !reflect.DeepEqual(response.Result.CycleRefs[0].RetainedPath, wantRetained) {
		t.Fatalf("merge retained path = %+v, want %+v", response.Result.CycleRefs[0], wantRetained)
	}

	self := graph.Relation{Address: "relation:self", TypeAddress: "type:edge", FromAddress: "entity:a", ToAddress: "entity:a"}
	recipe.Traversal.Direction = definition.TraversalBoth
	graphValue = graph.MasterGraph{
		Entities: []graph.Entity{entities[0]}, Relations: []graph.Relation{self},
		Outgoing: []graph.Adjacency{{EntityAddress: "entity:a", RelationAddresses: []string{self.Address}}},
		Incoming: []graph.Adjacency{{EntityAddress: "entity:a", RelationAddresses: []string{self.Address}}},
	}
	response, err = New(BuildInfo{}).ExecuteQuery(context.Background(), QueryExecutionInput{Recipe: recipe, Graph: graphValue})
	if err != nil || len(response.Result.CycleRefs) != 1 || response.Result.CycleRefs[0].Orientation != "outgoing" {
		t.Fatalf("self relation traversal = (%+v, %v)", response, err)
	}
}

func TestQueryTraversalOrdersRelationTypesByStableAddress(t *testing.T) {
	t.Parallel()
	projectType := "ldl:project:z:relation-type:calls"
	packType := "ldl:pack:a:one:relation-type:calls"
	projectRelation := "ldl:project:z:relation:project_edge"
	packRelation := "ldl:project:z:relation:pack_edge"
	root := []string{"ldl:project:z:entity:a"}
	types := []string{projectType, packType}
	recipe := emptyQueryRecipe("ldl:project:z:query:q")
	recipe.Select.RootAddresses = &root
	recipe.Select.RelationTypeAddresses = &types
	recipe.Traversal = &query.Traversal{Direction: definition.TraversalOutgoing, MinDepth: 1, MaxDepth: 1, CyclePolicy: query.CycleIncludeCycleRef, RelationTypeAddresses: &types}
	recipe.Result = []query.ResultMember{query.ResultPathRelations}
	graphValue := graph.MasterGraph{
		Entities: []graph.Entity{{Address: root[0]}, {Address: "ldl:project:z:entity:b"}},
		Relations: []graph.Relation{
			{Address: packRelation, TypeAddress: packType, FromAddress: root[0], ToAddress: "ldl:project:z:entity:b"},
			{Address: projectRelation, TypeAddress: projectType, FromAddress: root[0], ToAddress: "ldl:project:z:entity:b"},
		},
		Outgoing: []graph.Adjacency{{EntityAddress: root[0], RelationAddresses: []string{packRelation, projectRelation}}},
	}
	response, err := New(BuildInfo{}).ExecuteQuery(context.Background(), QueryExecutionInput{Recipe: recipe, Graph: graphValue})
	if err != nil || response.Status != "ok" || !reflect.DeepEqual(response.Result.PathRelationAddresses, []string{projectRelation}) {
		t.Fatalf("relation type order = (%+v, %v)", response, err)
	}
}

func emptyQueryRecipe(address string) CompiledQueryRecipe {
	return CompiledQueryRecipe{
		Address: address, StateInput: query.StateNone,
		Where: query.Predicate{Kind: query.PredicateAll}, RelationWhere: query.Predicate{Kind: query.PredicateAll},
	}
}

func scalarPredicateValue(value string) *query.PredicateValue {
	scalar := definition.Scalar{Type: definition.ScalarString, String: value}
	return &query.PredicateValue{Kind: query.ValueLiteral, Scalar: &scalar}
}
