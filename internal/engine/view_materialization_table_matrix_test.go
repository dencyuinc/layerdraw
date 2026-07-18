// SPDX-License-Identifier: LicenseRef-LayerDraw-1.0

package engine

import (
	"context"
	"fmt"
	"reflect"
	"strings"
	"testing"

	"github.com/dencyuinc/layerdraw/internal/engine/internal/compiler/semantic/definition"
	"github.com/dencyuinc/layerdraw/internal/engine/internal/compiler/view"
)

func TestMaterializeTableSupportsEntityRelationAndAutomaticGrains(t *testing.T) {
	t.Parallel()

	t.Run("entity owner", func(t *testing.T) {
		t.Parallel()
		snapshot, queryResult := compileAndExecuteViewFixture(t, entityOwnerTableViewSource())
		table := materializeQueryView(t, snapshot, queryResult, nil).Table
		if table == nil || len(table.Rows) != 2 || !reflect.DeepEqual(tableColumnIDs(table.Columns), []string{"entity_id", "tags", "related"}) {
			t.Fatalf("table = %+v", table)
		}
		if got := tableCellByID(t, *table, table.Rows[0], "entity_id"); !got.Present || got.Value == nil || got.Value.Scalar == nil || got.Value.Scalar.String != "alpha" {
			t.Fatalf("entity id = %+v", got)
		}
		if got := tableCellByID(t, *table, table.Rows[0], "related"); !got.Present || got.Value == nil || got.Value.Scalar == nil || got.Value.Scalar.Int != 1 || len(got.Source.RelationAddresses) != 1 {
			t.Fatalf("derived count = %+v", got)
		}
		if got := tableCellByID(t, *table, table.Rows[0], "tags"); !got.Present || got.Value == nil || !reflect.DeepEqual(got.Value.StringSet, []string{"critical"}) {
			t.Fatalf("tags = %+v", got)
		}
	})

	t.Run("relation owner", func(t *testing.T) {
		t.Parallel()
		snapshot, queryResult := compileAndExecuteViewFixture(t, relationOwnerTableViewSource())
		table := materializeQueryView(t, snapshot, queryResult, nil).Table
		if table == nil || len(table.Rows) != 1 || !reflect.DeepEqual(tableColumnIDs(table.Columns), []string{"relation_id", "relation_name", "from", "to"}) {
			t.Fatalf("table = %+v", table)
		}
		if got := tableCellByID(t, *table, table.Rows[0], "from"); !got.Present || got.Value == nil || got.Value.Address == nil || *got.Value.Address != "ldl:project:p:entity:alpha" || !reflect.DeepEqual(got.Source.EntityAddresses, []string{"ldl:project:p:entity:alpha"}) {
			t.Fatalf("from endpoint = %+v", got)
		}
		if got := tableCellByID(t, *table, table.Rows[0], "relation_name"); got.Present || got.Value != nil {
			t.Fatalf("missing relation display name = %+v", got)
		}
	})

	t.Run("automatic relation row", func(t *testing.T) {
		t.Parallel()
		snapshot, queryResult := compileAndExecuteViewFixture(t, automaticRelationTableViewSource())
		table := materializeQueryView(t, snapshot, queryResult, nil).Table
		protocol := "ldl:project:p:relation-type:calls:column:protocol"
		if table == nil || len(table.Rows) != 1 || !reflect.DeepEqual(tableColumnIDs(table.Columns), []string{"from", "relation_type", "calls_protocol"}) {
			t.Fatalf("table = %+v", table)
		}
		row := table.Rows[0]
		if got := tableCellByID(t, *table, row, "from"); !got.Present || got.Value == nil || got.Value.Address == nil || *got.Value.Address != "ldl:project:p:entity:alpha" {
			t.Fatalf("from = %+v", got)
		}
		if got := tableCellByID(t, *table, row, "calls_protocol"); !got.Present || got.Value == nil || got.Value.Scalar == nil || got.Value.Scalar.String != "http" || !reflect.DeepEqual(got.Source.CellRefs, []ViewDataCellRef{{RowAddress: "ldl:project:p:relation:alpha_beta:row:primary", ColumnAddress: protocol}}) {
			t.Fatalf("dynamic protocol = %+v", got)
		}
	})

	t.Run("automatic relation owner", func(t *testing.T) {
		t.Parallel()
		source := strings.Replace(automaticRelationTableViewSource(), "row_mode automatic", "row_mode relation", 1)
		snapshot, queryResult := compileAndExecuteViewFixture(t, source)
		table := materializeQueryView(t, snapshot, queryResult, nil).Table
		if table == nil || len(table.Rows) != 1 || !reflect.DeepEqual(tableColumnIDs(table.Columns), []string{"from", "relation_type"}) || len(table.Rows[0].Source.RowAddresses) != 0 {
			t.Fatalf("relation-grain automatic Table = %+v", table)
		}
	})

	t.Run("automatic explicit relation rows", func(t *testing.T) {
		t.Parallel()
		source := strings.Replace(automaticRelationTableViewSource(), "row_mode automatic", "row_mode relation_rows", 1)
		snapshot, queryResult := compileAndExecuteViewFixture(t, source)
		table := materializeQueryView(t, snapshot, queryResult, nil).Table
		if table == nil || len(table.Rows) != 1 || !reflect.DeepEqual(table.Rows[0].Source.RowAddresses, []string{"ldl:project:p:relation:alpha_beta:row:primary"}) {
			t.Fatalf("relation-row automatic Table = %+v", table)
		}
	})
}

func TestDynamicTableColumnIDIsPortableAndCollisionStable(t *testing.T) {
	t.Parallel()
	column := definition.Column{Address: "ldl:project:p:relation-type:calls:column:protocol", ID: "protocol"}
	if got := dynamicTableColumnID(column, map[string]bool{}); got != "calls_protocol" {
		t.Fatalf("dynamic ID = %q", got)
	}
	fallback := dynamicTableColumnID(column, map[string]bool{"calls_protocol": true})
	if !strings.HasPrefix(fallback, "dynamic_") || len(fallback) != len("dynamic_")+64 {
		t.Fatalf("collision fallback = %q", fallback)
	}
	if got := dynamicTableColumnID(column, map[string]bool{"calls_protocol": true, fallback: true}); got != "" {
		t.Fatalf("exhausted collision ID = %q", got)
	}
}

func TestMaterializeTableAggregatesSortsAndPreservesContributors(t *testing.T) {
	t.Parallel()
	snapshot, queryResult := compileAndExecuteViewFixture(t, aggregateTableViewSource())
	first := materializeQueryView(t, snapshot, queryResult, nil)
	second := materializeQueryView(t, snapshot, queryResult, nil)
	if !reflect.DeepEqual(first, second) {
		t.Fatal("aggregate Table materialization is not deterministic")
	}
	table := first.Table
	if table == nil || len(table.Rows) != 2 || !reflect.DeepEqual(tableColumnIDs(table.Columns), []string{"environment", "capacity_max", "entity_count"}) {
		t.Fatalf("table = %+v", table)
	}
	prod := table.Rows[0]
	if got := tableCellByID(t, *table, prod, "environment"); !got.Present || got.Value == nil || got.Value.Scalar == nil || got.Value.Scalar.String != "prod" {
		t.Fatalf("sorted group = %+v", got)
	}
	if got := tableCellByID(t, *table, prod, "capacity_max"); !got.Present || got.Value == nil || got.Value.Scalar == nil || got.Value.Scalar.Float != 75 || len(got.Source.CellRefs) != 2 {
		t.Fatalf("max = %+v", got)
	}
	if got := tableCellByID(t, *table, prod, "entity_count"); !got.Present || got.Value == nil || got.Value.Scalar == nil || got.Value.Scalar.Int != 2 || len(got.Source.EntityAddresses) != 2 {
		t.Fatalf("count = %+v", got)
	}
	if got := tableCellByID(t, *table, table.Rows[1], "capacity_max"); got.Value == nil || got.Value.Scalar == nil || got.Value.Scalar.Float != 50 {
		t.Fatalf("second group = %+v", got)
	}
}

func TestMaterializeTableCoversEveryAggregateFamily(t *testing.T) {
	t.Parallel()
	snapshot, queryResult := compileAndExecuteViewFixture(t, aggregateFamilyTableViewSource())
	table := materializeQueryView(t, snapshot, queryResult, nil).Table
	if table == nil || len(table.Rows) != 2 {
		t.Fatalf("table = %+v", table)
	}
	prod := table.Rows[0]
	assertScalarTableCell(t, tableCellByID(t, *table, prod, "capacity_min"), definition.ScalarNumber, "", 25, 0)
	assertScalarTableCell(t, tableCellByID(t, *table, prod, "capacity_max"), definition.ScalarNumber, "", 75, 0)
	assertScalarTableCell(t, tableCellByID(t, *table, prod, "entity_count_distinct"), definition.ScalarInteger, "", 0, 2)
	assertScalarTableCell(t, tableCellByID(t, *table, prod, "names"), definition.ScalarString, "Alpha, Beta", 0, 0)
}

func TestMaterializeTableCreatesTheClosedEmptyAggregateGroup(t *testing.T) {
	t.Parallel()
	snapshot, queryResult := compileAndExecuteViewFixture(t, emptyAggregateTableViewSource())
	queryResult.SeedEntityAddresses = []string{}
	queryResult.ReachedEntityAddresses = []string{}
	queryResult.TraversedEntityAddresses = []string{}
	queryResult.PrimaryEntityAddresses = []string{}
	queryResult.SupportEntityAddresses = []string{}
	queryResult.PathRelationAddresses = []string{}
	queryResult.InducedRelationAddresses = []string{}
	queryResult.SelectedRelationAddresses = []string{}
	queryResult.Paths = []QueryPath{}
	table := materializeQueryView(t, snapshot, queryResult, nil).Table
	if table == nil || len(table.Rows) != 1 {
		t.Fatalf("empty aggregate Table = %+v", table)
	}
	assertScalarTableCell(t, tableCellByID(t, *table, table.Rows[0], "entity_count"), definition.ScalarInteger, "", 0, 0)
	assertScalarTableCell(t, tableCellByID(t, *table, table.Rows[0], "distinct_count"), definition.ScalarInteger, "", 0, 0)
	if names := tableCellByID(t, *table, table.Rows[0], "names"); names.Present || names.Value != nil {
		t.Fatalf("empty join_unique = %+v", names)
	}
	if maximum := tableCellByID(t, *table, table.Rows[0], "capacity_max"); maximum.Present || maximum.Value != nil {
		t.Fatalf("empty max = %+v", maximum)
	}
}

func TestTableCellComparisonCoversClosedTypedOrdering(t *testing.T) {
	t.Parallel()
	addressA, addressB := "ldl:project:p:entity:alpha", "ldl:project:p:entity:beta"
	cases := []struct {
		name   string
		left   ViewDataValue
		right  ViewDataValue
		column TableColumn
	}{
		{name: "stable address", left: addressViewValue(addressA), right: addressViewValue(addressB)},
		{name: "string set", left: ViewDataValue{Kind: "string_set", StringSet: []string{"a"}}, right: ViewDataValue{Kind: "string_set", StringSet: []string{"a", "b"}}},
		{name: "string", left: scalarViewValue(definition.Scalar{Type: definition.ScalarString, String: "a"}), right: scalarViewValue(definition.Scalar{Type: definition.ScalarString, String: "b"})},
		{name: "enum", left: scalarViewValue(definition.Scalar{Type: definition.ScalarEnum, String: "prod"}), right: scalarViewValue(definition.Scalar{Type: definition.ScalarEnum, String: "stg"}), column: TableColumn{EnumValues: []string{"prod", "stg"}}},
		{name: "boolean", left: scalarViewValue(definition.Scalar{Type: definition.ScalarBoolean}), right: scalarViewValue(definition.Scalar{Type: definition.ScalarBoolean, Bool: true})},
		{name: "number", left: scalarViewValue(definition.Scalar{Type: definition.ScalarNumber, Float: 1}), right: scalarViewValue(definition.Scalar{Type: definition.ScalarNumber, Float: 2})},
	}
	for _, test := range cases {
		test := test
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			compared, ok := compareTableValues(test.left, test.right, test.column)
			if !ok || compared >= 0 {
				t.Fatalf("comparison = %d, %v", compared, ok)
			}
		})
	}
	absent, present := absentTableCell(emptyViewDataSourceRefs()), presentTableCell(scalarViewValue(definition.Scalar{Type: definition.ScalarInteger, Int: 1}), emptyViewDataSourceRefs())
	if compared, ok := compareTableCells(absent, present, TableColumn{}, view.AbsentFirst); !ok || compared >= 0 {
		t.Fatalf("absent-first comparison = %d, %v", compared, ok)
	}
	if compared, ok := compareTableCells(absent, present, TableColumn{}, view.AbsentLast); !ok || compared <= 0 {
		t.Fatalf("absent-last comparison = %d, %v", compared, ok)
	}
	if compared, ok := compareTableCells(absent, absent, TableColumn{}, view.AbsentLast); !ok || compared != 0 {
		t.Fatalf("absent tie = %d, %v", compared, ok)
	}
	if _, ok := compareTableValues(addressViewValue(addressA), scalarViewValue(definition.Scalar{Type: definition.ScalarString, String: "a"}), TableColumn{}); ok {
		t.Fatal("incompatible Table value kinds were comparable")
	}
	if _, ok := compareEnumValues("unknown", "prod", []string{"prod"}); ok {
		t.Fatal("unknown enum option was comparable")
	}
}

func TestMaterializeTableReadsImmutableStateWithMissingStaleAndRedactedSemantics(t *testing.T) {
	t.Parallel()
	snapshot, queryResult := compileAndExecuteViewFixture(t, stateTableViewSource())
	alphaRow := "ldl:project:p:entity:alpha:row:primary"
	state := validStateQuerySnapshot(t, snapshot, []StateQuerySubject{
		stateSubject(alphaRow, stateFields("system.updated_at", datetimeScalar("2026-01-04T00:00:00Z"))),
	})

	t.Run("present and absent", func(t *testing.T) {
		result := materializeQueryView(t, snapshot, queryResult, &state)
		table := result.Table
		if table == nil || len(table.Rows) != 2 {
			t.Fatalf("table = %+v", table)
		}
		present := tableCellByID(t, *table, table.Rows[0], "updated_at")
		wantRead := StateReadRef{SubjectAddress: alphaRow, FieldPath: "system.updated_at"}
		if !present.Present || present.Value == nil || present.Value.Scalar == nil || present.Value.Scalar.String != "2026-01-04T00:00:00Z" || !reflect.DeepEqual(present.Source.State.Reads, []StateReadRef{wantRead}) {
			t.Fatalf("present state = %+v", present)
		}
		if absent := tableCellByID(t, *table, table.Rows[1], "updated_at"); absent.Present || absent.Value != nil {
			t.Fatalf("absent state = %+v", absent)
		}
		base, _ := result.Base()
		if base.StateInput.Kind != "snapshot" || base.StateInput.SnapshotHash == "" {
			t.Fatalf("state input = %+v", base.StateInput)
		}
	})

	t.Run("optional stale", func(t *testing.T) {
		stale := cloneStateSnapshot(state)
		stale.Subjects[0].OwnSubjectHash = semanticHash('f')
		result := materializeQueryView(t, snapshot, queryResult, &stale)
		base, _ := result.Base()
		if !hasDiagnosticCode(base.Diagnostics, "LDL1605") || tableCellByID(t, *result.Table, result.Table.Rows[0], "updated_at").Present {
			t.Fatalf("stale state result = %+v", result)
		}
	})

	t.Run("redacted", func(t *testing.T) {
		redacted := cloneStateSnapshot(state)
		redacted.Subjects[0].Fields = map[string]TypedScalar{}
		redacted.Subjects[0].RedactedFieldPaths = []string{"system.updated_at"}
		response := New(BuildInfo{}).MaterializeView(context.Background(), queryViewInput(snapshot, queryResult, &redacted))
		if response.Status != "rejected" || response.Result != nil || !hasDiagnosticCode(response.Diagnostics, "LDL1904") {
			t.Fatalf("redacted state = %+v", response)
		}
	})

	t.Run("inaccessible", func(t *testing.T) {
		inaccessible := cloneStateSnapshot(state)
		inaccessible.InaccessibleFieldPaths = []string{"system.updated_at"}
		inaccessible.Subjects = []StateQuerySubject{}
		response := New(BuildInfo{}).MaterializeView(context.Background(), queryViewInput(snapshot, queryResult, &inaccessible))
		if response.Status != "rejected" || !hasDiagnosticCode(response.Diagnostics, "LDL1904") {
			t.Fatalf("inaccessible state = %+v", response)
		}
	})

	t.Run("required stale", func(t *testing.T) {
		requiredSnapshot, requiredResult := compileAndExecuteViewFixture(t, strings.Replace(stateTableViewSource(), "state_input optional", "state_input required", 1))
		requiredState := validStateQuerySnapshot(t, requiredSnapshot, []StateQuerySubject{
			stateSubject(alphaRow, stateFields("system.updated_at", datetimeScalar("2026-01-04T00:00:00Z"))),
		})
		requiredState.Subjects[0].OwnSubjectHash = semanticHash('f')
		response := New(BuildInfo{}).MaterializeView(context.Background(), queryViewInput(requiredSnapshot, requiredResult, &requiredState))
		if response.Status != "rejected" || !hasDiagnosticCode(response.Diagnostics, "LDL1604") {
			t.Fatalf("required stale state = %+v", response)
		}
	})
}

func TestMaterializeMatrixResolvesDirectionsProjectionAndCompleteSources(t *testing.T) {
	t.Parallel()

	t.Run("outgoing attribute summary", func(t *testing.T) {
		t.Parallel()
		snapshot, queryResult := compileAndExecuteViewFixture(t, matrixViewSource("outgoing", "relation_refs", "attribute_summary", false))
		result := materializeQueryView(t, snapshot, queryResult, nil)
		if repeated := materializeQueryView(t, snapshot, queryResult, nil); !reflect.DeepEqual(result, repeated) {
			t.Fatal("Matrix materialization is not deterministic")
		}
		matrix := result.Matrix
		if matrix == nil || len(matrix.RowAxis) != 2 || len(matrix.ColumnAxis) != 2 || len(matrix.Cells) != 4 {
			t.Fatalf("matrix = %+v", matrix)
		}
		if got := matrixAxisAddresses(matrix.RowAxis); !reflect.DeepEqual(got, []string{"ldl:project:p:entity:alpha", "ldl:project:p:entity:beta"}) || matrix.RowAxis[0].Label != "alpha" || matrix.ColumnAxis[0].Label != "Alpha" {
			t.Fatalf("axes = %+v / %+v", matrix.RowAxis, matrix.ColumnAxis)
		}
		cell := matrixCellByEntities(t, *matrix, "ldl:project:p:entity:alpha", "ldl:project:p:entity:beta")
		protocol := "ldl:project:p:relation-type:calls:column:protocol"
		if len(cell.SemanticRefs) != 1 || cell.SemanticRefs[0].RelationAddress == nil || *cell.SemanticRefs[0].RelationAddress != "ldl:project:p:relation:alpha_beta" || len(cell.DisplayValue.Attributes) != 1 {
			t.Fatalf("semantic cell = %+v", cell)
		}
		attribute := cell.DisplayValue.Attributes[0]
		if attribute.RowAddress != "ldl:project:p:relation:alpha_beta:row:primary" || attribute.ColumnAddress != protocol || attribute.Value.Type != definition.ScalarEnum || attribute.Value.String != "http" {
			t.Fatalf("attribute = %+v", attribute)
		}
		if !reflect.DeepEqual(cell.Source.EntityAddresses, []string{"ldl:project:p:entity:alpha", "ldl:project:p:entity:beta"}) || !reflect.DeepEqual(cell.Source.RelationAddresses, []string{"ldl:project:p:relation:alpha_beta"}) || !reflect.DeepEqual(cell.Source.CellRefs, []ViewDataCellRef{{RowAddress: attribute.RowAddress, ColumnAddress: protocol}}) {
			t.Fatalf("cell source = %+v", cell.Source)
		}
		empty := matrixCellByEntities(t, *matrix, "ldl:project:p:entity:beta", "ldl:project:p:entity:alpha")
		if len(empty.SemanticRefs) != 0 || empty.DisplayValue.Kind != "attributes" || len(empty.Source.SubjectAddresses) != 0 {
			t.Fatalf("empty cell = %+v", empty)
		}
	})

	t.Run("support entity exclusion", func(t *testing.T) {
		t.Parallel()
		snapshot, queryResult := compileAndExecuteViewFixture(t, matrixViewSource("outgoing", "relation_refs", "exists", false))
		queryResult.SupportEntityAddresses = []string{"ldl:project:p:entity:gamma"}
		matrix := materializeQueryView(t, snapshot, queryResult, nil).Matrix
		if got := matrixAxisAddresses(matrix.RowAxis); !reflect.DeepEqual(got, []string{"ldl:project:p:entity:alpha", "ldl:project:p:entity:beta"}) {
			t.Fatalf("support Entity leaked into Matrix axis: %v", got)
		}
		if present := matrixCellByEntities(t, *matrix, "ldl:project:p:entity:alpha", "ldl:project:p:entity:beta"); present.DisplayValue.Kind != "boolean" || !present.DisplayValue.Boolean {
			t.Fatalf("exists cell = %+v", present)
		}
	})

	t.Run("relation type labels", func(t *testing.T) {
		t.Parallel()
		snapshot, queryResult := compileAndExecuteViewFixture(t, matrixViewSource("outgoing", "relation_refs", "relation_types", false))
		matrix := materializeQueryView(t, snapshot, queryResult, nil).Matrix
		cell := matrixCellByEntities(t, *matrix, "ldl:project:p:entity:alpha", "ldl:project:p:entity:beta")
		if cell.DisplayValue.Kind != "string_set" || !reflect.DeepEqual(cell.DisplayValue.StringSet, []string{"Calls"}) {
			t.Fatalf("relation type labels = %+v", cell.DisplayValue)
		}
	})

	t.Run("omitted relation selector and alternate axis labels", func(t *testing.T) {
		t.Parallel()
		source := matrixViewSource("outgoing", "relation_refs", "exists", false)
		source = strings.Replace(source, "      relation_types [calls]\n", "", 1)
		source = strings.Replace(source, "      label id", "      label type", 1)
		source = strings.Replace(source, "      label display_name", "      label layer", 1)
		snapshot, queryResult := compileAndExecuteViewFixture(t, source)
		matrix := materializeQueryView(t, snapshot, queryResult, nil).Matrix
		if matrix.RowAxis[0].Label != "ldl:project:p:entity-type:service" || matrix.ColumnAxis[0].Label != "ldl:project:p:layer:app" {
			t.Fatalf("axis labels = %+v / %+v", matrix.RowAxis, matrix.ColumnAxis)
		}
	})

	for _, test := range []struct {
		name      string
		direction string
		reverse   bool
		row       string
		column    string
	}{
		{name: "incoming", direction: "incoming", row: "beta", column: "alpha"},
		{name: "reversed projection", direction: "outgoing", reverse: true, row: "beta", column: "alpha"},
	} {
		test := test
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			snapshot, queryResult := compileAndExecuteViewFixture(t, matrixViewSource(test.direction, "relation_refs", "count", test.reverse))
			matrix := materializeQueryView(t, snapshot, queryResult, nil).Matrix
			cell := matrixCellByEntities(t, *matrix, "ldl:project:p:entity:"+test.row, "ldl:project:p:entity:"+test.column)
			if len(cell.SemanticRefs) != 1 || cell.DisplayValue.Integer != 1 {
				t.Fatalf("cell = %+v", cell)
			}
		})
	}

	t.Run("both", func(t *testing.T) {
		t.Parallel()
		snapshot, queryResult := compileAndExecuteViewFixture(t, matrixViewSource("both", "relation_refs", "count", false))
		matrix := materializeQueryView(t, snapshot, queryResult, nil).Matrix
		for _, pair := range [][2]string{{"alpha", "beta"}, {"beta", "alpha"}} {
			cell := matrixCellByEntities(t, *matrix, "ldl:project:p:entity:"+pair[0], "ldl:project:p:entity:"+pair[1])
			if len(cell.SemanticRefs) != 1 || cell.DisplayValue.Integer != 1 {
				t.Fatalf("%s -> %s = %+v", pair[0], pair[1], cell)
			}
		}
	})
}

func TestMaterializeMatrixUsesOnlyRetainedOrderedPathsAndRejectsMalformedPaths(t *testing.T) {
	t.Parallel()
	snapshot, queryResult := compileAndExecuteViewFixture(t, matrixViewSource("incoming", "path_refs", "count", false))
	result := materializeQueryView(t, snapshot, queryResult, nil)
	cell := matrixCellByEntities(t, *result.Matrix, "ldl:project:p:entity:beta", "ldl:project:p:entity:alpha")
	if len(cell.SemanticRefs) != 1 || cell.SemanticRefs[0].Path == nil || cell.DisplayValue.Integer != 1 || !reflect.DeepEqual(cell.Source.RelationAddresses, []string{"ldl:project:p:relation:alpha_beta"}) {
		t.Fatalf("path cell = %+v", cell)
	}
	if got := matrixCellByEntities(t, *result.Matrix, "ldl:project:p:entity:alpha", "ldl:project:p:entity:alpha"); len(got.SemanticRefs) != 1 || got.DisplayValue.Integer != 1 {
		t.Fatalf("retained zero-length path = %+v", got)
	}
	typeSnapshot, typeResult := compileAndExecuteViewFixture(t, matrixViewSource("incoming", "path_refs", "relation_types", false))
	typeCell := matrixCellByEntities(t, *materializeQueryView(t, typeSnapshot, typeResult, nil).Matrix, "ldl:project:p:entity:beta", "ldl:project:p:entity:alpha")
	if !reflect.DeepEqual(typeCell.DisplayValue.StringSet, []string{"Calls"}) {
		t.Fatalf("path relation types = %+v", typeCell.DisplayValue)
	}

	for _, test := range []struct {
		name   string
		mutate func(*QueryResult)
	}{
		{
			name: "invalid sequence",
			mutate: func(result *QueryResult) {
				result.Paths = []QueryPath{{EntityAddresses: []string{"ldl:project:p:entity:alpha", "ldl:project:p:entity:beta"}, RelationAddresses: []string{}}}
			},
		},
		{
			name: "unknown entity",
			mutate: func(result *QueryResult) {
				result.Paths = []QueryPath{{EntityAddresses: []string{"ldl:project:p:entity:alpha", "ldl:project:p:entity:missing"}, RelationAddresses: []string{"ldl:project:p:relation:alpha_beta"}}}
			},
		},
		{
			name: "relation outside source set",
			mutate: func(result *QueryResult) {
				result.Paths = []QueryPath{{EntityAddresses: []string{"ldl:project:p:entity:beta", "ldl:project:p:entity:gamma"}, RelationAddresses: []string{"ldl:project:p:relation:beta_gamma"}}}
			},
		},
		{
			name: "non-adjacent relation",
			mutate: func(result *QueryResult) {
				result.Paths = []QueryPath{{EntityAddresses: []string{"ldl:project:p:entity:alpha", "ldl:project:p:entity:gamma"}, RelationAddresses: []string{"ldl:project:p:relation:alpha_beta"}}}
			},
		},
		{
			name: "unknown relation",
			mutate: func(result *QueryResult) {
				result.Paths = []QueryPath{{EntityAddresses: []string{"ldl:project:p:entity:alpha", "ldl:project:p:entity:beta"}, RelationAddresses: []string{"ldl:project:p:relation:missing"}}}
			},
		},
		{
			name: "duplicate path",
			mutate: func(result *QueryResult) {
				path := QueryPath{EntityAddresses: []string{"ldl:project:p:entity:alpha", "ldl:project:p:entity:beta"}, RelationAddresses: []string{"ldl:project:p:relation:alpha_beta"}}
				result.Paths = []QueryPath{path, deepClone(path)}
			},
		},
	} {
		test := test
		t.Run(test.name, func(t *testing.T) {
			mutated := deepClone(queryResult)
			test.mutate(&mutated)
			first := New(BuildInfo{}).MaterializeView(context.Background(), queryViewInput(snapshot, mutated, nil))
			second := New(BuildInfo{}).MaterializeView(context.Background(), queryViewInput(snapshot, mutated, nil))
			if first.Status != "rejected" || first.Result != nil || !hasDiagnosticCode(first.Diagnostics, "LDL1702") || !reflect.DeepEqual(first, second) {
				t.Fatalf("malformed path response = %+v", first)
			}
		})
	}
}

func entityOwnerTableViewSource() string {
	return structuralQuerySource() + `
view inventory "Inventory" inventory {
  source query prod_scope { environment: prod }
  table {
    rows entity
    column entity_id {
      source field id
    }
    column tags {
      source field tags
    }
    column related {
      source derived_count both relations [calls]
    }
  }
}
`
}

func relationOwnerTableViewSource() string {
	return structuralQuerySource() + `
view dependencies "Dependencies" dependency {
  source query prod_scope { environment: prod }
  table {
    rows relation
    column relation_id {
      source field id
    }
    column relation_name {
      source field display_name
    }
    column from {
      source relation_endpoint from address
    }
    column to {
      source relation_endpoint to display_name
    }
  }
}
`
}

func automaticRelationTableViewSource() string {
	return structuralQuerySource() + `
view dependencies "Dependencies" dependency {
  source query prod_scope { environment: prod }
  relation_projection calls {
    table {
      row_mode automatic
      include_from true
      include_to false
      include_relation_type true
    }
  }
  table {
    rows automatic_relations
  }
}
`
}

func aggregateTableViewSource() string {
	source := strings.Replace(structuralQuerySource(), "beta_gamma primary: grpc", "beta_gamma primary: http", 1)
	source = strings.Replace(source, `  where all {
    rows any types [service] {
      cell environment == $environment
    }
  }
`, "", 1)
	return source + `
view capacity "Capacity" inventory {
  source query prod_scope { environment: prod }
  table {
    rows entity_rows
    column environment {
      source attribute environment entity_types [service]
    }
    column capacity_max {
      source attribute capacity entity_types [service]
      aggregate max
    }
    column entity_count {
      source field id
      aggregate count
    }
    sort capacity_max descending nulls last
  }
}
`
}

func aggregateFamilyTableViewSource() string {
	source := strings.Replace(structuralQuerySource(), "beta_gamma primary: grpc", "beta_gamma primary: http", 1)
	source = strings.Replace(source, `  where all {
    rows any types [service] {
      cell environment == $environment
    }
  }
`, "", 1)
	return source + `
view capacity "Capacity" inventory {
  source query prod_scope { environment: prod }
  table {
    rows entity_rows
    column environment {
      source attribute environment entity_types [service]
    }
    column capacity_min {
      source attribute capacity entity_types [service]
      aggregate min
    }
    column capacity_max {
      source attribute capacity entity_types [service]
      aggregate max
    }
    column entity_count_distinct {
      source field id
      aggregate count_distinct
    }
    column names {
      source field display_name
      aggregate join_unique
    }
    sort environment ascending nulls last
  }
}
`
}

func emptyAggregateTableViewSource() string {
	return structuralQuerySource() + `
view counts "Counts" inventory {
  source query prod_scope { environment: prod }
  table {
    rows entity_rows
    column entity_count {
      source field id
      aggregate count
    }
    column distinct_count {
      source field id
      aggregate count_distinct
    }
    column names {
      source field display_name
      aggregate join_unique
    }
    column capacity_max {
      source attribute capacity entity_types [service]
      aggregate max
    }
  }
}
`
}

func stateTableViewSource() string {
	return structuralQuerySource() + `
view inventory "Inventory" inventory {
  state_input optional
  source query prod_scope { environment: prod }
  table {
    rows entity_rows
    column entity_id {
      source field id
    }
    column updated_at {
      source state system.updated_at
    }
    sort updated_at descending nulls last
  }
}
`
}

func matrixViewSource(direction, semantic, display string, reverse bool) string {
	rowEndpoint, columnEndpoint := "from", "to"
	if reverse {
		rowEndpoint, columnEndpoint = "to", "from"
	}
	attributes := ""
	if display == "attribute_summary" {
		attributes = "\n      attributes [protocol]"
	}
	return structuralQuerySource() + fmt.Sprintf(`
view dependencies "Dependencies" dependency {
  source query prod_scope { environment: prod }
  relation_projection calls {
    matrix {
      row_endpoint %s
      column_endpoint %s
      include_relation_rows true
    }
  }
  matrix {
    row_axis {
      entity_types [service]
      label id
    }
    column_axis {
      entity_types [service]
      label display_name
    }
    cell {
      relation_types [calls]
      direction %s
      semantic %s
      display %s%s
    }
  }
}
`, rowEndpoint, columnEndpoint, direction, semantic, display, attributes)
}

func matrixCellByEntities(t *testing.T, matrix MatrixViewData, rowAddress, columnAddress string) MatrixCell {
	t.Helper()
	rowKey, columnKey := "", ""
	for _, item := range matrix.RowAxis {
		if item.EntityAddress == rowAddress {
			rowKey = item.Key
			break
		}
	}
	for _, item := range matrix.ColumnAxis {
		if item.EntityAddress == columnAddress {
			columnKey = item.Key
			break
		}
	}
	for _, cell := range matrix.Cells {
		if cell.RowKey == rowKey && cell.ColumnKey == columnKey {
			return cell
		}
	}
	t.Fatalf("matrix cell %s -> %s not found", rowAddress, columnAddress)
	return MatrixCell{}
}

func matrixAxisAddresses(values []MatrixAxisItem) []string {
	addresses := make([]string, len(values))
	for index, value := range values {
		addresses[index] = value.EntityAddress
	}
	return addresses
}

func assertScalarTableCell(t *testing.T, cell TableCell, valueType definition.ScalarType, text string, number float64, integer int64) {
	t.Helper()
	if !cell.Present || cell.Value == nil || cell.Value.Scalar == nil || cell.Value.Scalar.Type != valueType || cell.Value.Scalar.String != text || cell.Value.Scalar.Float != number || cell.Value.Scalar.Int != integer {
		t.Fatalf("cell = %+v", cell)
	}
}
