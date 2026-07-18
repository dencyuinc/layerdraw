// SPDX-License-Identifier: LicenseRef-LayerDraw-1.0

package engine

import (
	"context"
	"reflect"
	"strings"
	"testing"

	"github.com/dencyuinc/layerdraw/internal/engine/internal/compiler/semantic/definition"
	"github.com/dencyuinc/layerdraw/internal/engine/internal/compiler/semantic/graph"
	"github.com/dencyuinc/layerdraw/internal/engine/internal/compiler/view"
)

func TestMaterializeTreeBuildsDeterministicOccurrencesLinksAndCycleRefs(t *testing.T) {
	t.Parallel()
	snapshot, queryResult := compileAndExecuteTreeFlowFixture(t, treeViewSource())
	first := materializeQueryView(t, snapshot, queryResult, nil)
	second := materializeQueryView(t, snapshot, queryResult, nil)
	if !reflect.DeepEqual(first, second) {
		t.Fatal("repeated Tree materialization is not deterministic")
	}
	if first.Tree == nil || len(first.Tree.Roots) != 1 || len(first.Tree.CycleRefs) != 1 || len(first.Tree.LinkRefs) != 1 {
		t.Fatalf("tree = %+v", first.Tree)
	}
	root := first.Tree.Roots[0]
	if root.EntityAddress != entityAddress("root") || len(root.Children) != 2 {
		t.Fatalf("root = %+v", root)
	}
	alpha, beta := root.Children[0], root.Children[1]
	if alpha.EntityAddress != entityAddress("alpha") || beta.EntityAddress != entityAddress("beta") || len(alpha.Children) != 1 || len(beta.Children) != 0 {
		t.Fatalf("root children = %+v", root.Children)
	}
	shared := alpha.Children[0]
	if shared.EntityAddress != entityAddress("shared") || shared.ViaRelationAddress == nil || *shared.ViaRelationAddress != relationAddress("c_alpha_shared") {
		t.Fatalf("shared occurrence = %+v", shared)
	}
	cycleRef := first.Tree.CycleRefs[0]
	if cycleRef.FromOccurrenceKey != shared.Key || cycleRef.ToEntityAddress != entityAddress("root") || cycleRef.RelationAddress != relationAddress("e_shared_root") {
		t.Fatalf("cycle ref = %+v", cycleRef)
	}
	linkRef := first.Tree.LinkRefs[0]
	if linkRef.FromOccurrenceKey != beta.Key || linkRef.ToEntityAddress != entityAddress("shared") || linkRef.RelationAddress != relationAddress("d_beta_shared") {
		t.Fatalf("link ref = %+v", linkRef)
	}
	for _, source := range []ViewDataSourceRefs{root.Source, alpha.Source, shared.Source, cycleRef.Source, linkRef.Source} {
		assertRequiredSourceCollections(t, source)
	}
	if !reflect.DeepEqual(shared.Source.RelationAddresses, []string{relationAddress("c_alpha_shared")}) ||
		!reflect.DeepEqual(cycleRef.Source.RelationAddresses, []string{relationAddress("e_shared_root")}) ||
		!reflect.DeepEqual(linkRef.Source.RelationAddresses, []string{relationAddress("d_beta_shared")}) {
		t.Fatalf("tree source refs shared=%+v cycle=%+v link=%+v", shared.Source, cycleRef.Source, linkRef.Source)
	}
}

func TestMaterializeTreeHonorsSharedChildCycleAndDepthPolicies(t *testing.T) {
	t.Parallel()
	t.Run("duplicate occurrence", func(t *testing.T) {
		source := strings.Replace(treeViewSource(), "shared_child_policy link", "shared_child_policy duplicate_occurrence", 1)
		snapshot, queryResult := compileAndExecuteTreeFlowFixture(t, source)
		result := materializeQueryView(t, snapshot, queryResult, nil)
		if result.Tree == nil || len(result.Tree.LinkRefs) != 0 || len(result.Tree.CycleRefs) != 2 {
			t.Fatalf("duplicate tree = %+v", result.Tree)
		}
		root := result.Tree.Roots[0]
		if len(root.Children[0].Children) != 1 || len(root.Children[1].Children) != 1 || root.Children[0].Children[0].Key == root.Children[1].Children[0].Key {
			t.Fatalf("duplicate occurrences = %+v", root.Children)
		}
	})
	t.Run("shared child error", func(t *testing.T) {
		source := strings.Replace(treeViewSource(), "shared_child_policy link", "shared_child_policy error", 1)
		assertTreeFlowRejected(t, source, ViewMaterializationLimits{}, "forbidden shared child")
	})
	t.Run("cycle error", func(t *testing.T) {
		source := strings.Replace(treeViewSource(), "cycle_policy truncate", "cycle_policy error", 1)
		assertTreeFlowRejected(t, source, ViewMaterializationLimits{}, "forbidden ancestry cycle")
	})
	t.Run("query depth bounds materialization", func(t *testing.T) {
		source := strings.Replace(treeViewSource(), "traverse outgoing 0..8", "traverse outgoing 0..1", 1)
		snapshot, queryResult := compileAndExecuteTreeFlowFixture(t, source)
		result := materializeQueryView(t, snapshot, queryResult, nil)
		root := result.Tree.Roots[0]
		if len(root.Children) != 2 || len(root.Children[0].Children) != 0 || len(root.Children[1].Children) != 0 || len(result.Tree.CycleRefs) != 0 || len(result.Tree.LinkRefs) != 0 {
			t.Fatalf("depth-bounded tree = %+v", result.Tree)
		}
	})
}

func TestMaterializeFlowBuildsDeterministicLanesBranchesJoinsAndMergedConnectors(t *testing.T) {
	t.Parallel()
	snapshot, queryResult := compileAndExecuteTreeFlowFixture(t, flowViewSource())
	first := materializeQueryView(t, snapshot, queryResult, nil)
	second := materializeQueryView(t, snapshot, queryResult, nil)
	if !reflect.DeepEqual(first, second) {
		t.Fatal("repeated Flow materialization is not deterministic")
	}
	flow := first.Flow
	if flow == nil || len(flow.Lanes) != 3 || len(flow.Steps) != 5 || len(flow.Connectors) != 6 || len(flow.CycleRefs) != 1 {
		t.Fatalf("flow = %+v", flow)
	}
	steps := flowStepsByEntity(flow.Steps)
	if !steps[entityAddress("a_start")].Branch || steps[entityAddress("a_start")].Join || !steps[entityAddress("d_join")].Join {
		t.Fatalf("branch/join steps = %+v", flow.Steps)
	}
	merged := flowConnectorByEndpoints(t, *flow, entityAddress("a_start"), entityAddress("b_task"))
	if merged.BranchValue == nil || merged.BranchValue.String != "yes" || len(merged.RelationAddresses) != 2 || len(merged.BranchRowAddresses) != 3 {
		t.Fatalf("merged connector = %+v", merged)
	}
	if len(merged.Source.RelationAddresses) != 2 || len(merged.Source.RowAddresses) != 3 || len(merged.Source.CellRefs) != 3 {
		t.Fatalf("merged connector source = %+v", merged.Source)
	}
	absent := flowConnectorByEndpoints(t, *flow, entityAddress("d_join"), entityAddress("e_end"))
	if absent.BranchValue != nil || len(absent.BranchRowAddresses) != 0 || len(absent.Source.RowAddresses) != 0 || len(absent.Source.CellRefs) != 0 {
		t.Fatalf("rowless branch connector = %+v", absent)
	}
	cycleRef := flow.CycleRefs[0]
	if cycleRef.ConnectorKey == "" || !reflect.DeepEqual(cycleRef.RelationAddresses, []string{relationAddress("g_end_start")}) || cycleRef.FromStepKey != steps[entityAddress("e_end")].Key || cycleRef.ToStepKey != steps[entityAddress("a_start")].Key {
		t.Fatalf("cycle ref = %+v", cycleRef)
	}
	workLane := flowLaneByLabel(t, flow.Lanes, "work")
	if len(workLane.StepKeys) != 3 || len(workLane.Source.RowAddresses) != 4 || len(workLane.Source.CellRefs) != 4 {
		t.Fatalf("work lane = %+v", workLane)
	}
	missingLane := flowLaneByLabel(t, flow.Lanes, "")
	if len(missingLane.StepKeys) != 1 || missingLane.StepKeys[0] != steps[entityAddress("e_end")].Key {
		t.Fatalf("missing lane = %+v", missingLane)
	}
	for _, connector := range flow.Connectors {
		assertRequiredSourceCollections(t, connector.Source)
	}
}

func TestMaterializeFlowHonorsParallelCycleAndLanePolicies(t *testing.T) {
	t.Parallel()
	t.Run("preserve parallel", func(t *testing.T) {
		source := strings.Replace(flowViewSource(), "    cycle_policy include_cycle_ref", "    cycle_policy include_cycle_ref\n    preserve_parallel", 1)
		snapshot, queryResult := compileAndExecuteTreeFlowFixture(t, source)
		result := materializeQueryView(t, snapshot, queryResult, nil)
		if result.Flow == nil || len(result.Flow.Connectors) != 8 || len(result.Flow.CycleRefs) != 1 {
			t.Fatalf("parallel flow = %+v", result.Flow)
		}
		count := 0
		for _, connector := range result.Flow.Connectors {
			steps := flowStepsByKey(result.Flow.Steps)
			if steps[connector.FromStepKey].EntityAddress == entityAddress("a_start") && steps[connector.ToStepKey].EntityAddress == entityAddress("b_task") {
				count++
				if len(connector.RelationAddresses) != 1 || len(connector.BranchRowAddresses) != 1 {
					t.Fatalf("unmerged connector = %+v", connector)
				}
			}
		}
		if count != 3 {
			t.Fatalf("parallel a_start -> b_task connectors = %d", count)
		}
	})
	t.Run("truncate cycle", func(t *testing.T) {
		source := strings.Replace(flowViewSource(), "cycle_policy include_cycle_ref", "cycle_policy truncate", 1)
		snapshot, queryResult := compileAndExecuteTreeFlowFixture(t, source)
		result := materializeQueryView(t, snapshot, queryResult, nil)
		if result.Flow == nil || len(result.Flow.Connectors) != 5 || len(result.Flow.CycleRefs) != 1 {
			t.Fatalf("truncated flow = %+v", result.Flow)
		}
		for _, connector := range result.Flow.Connectors {
			if connector.Key == result.Flow.CycleRefs[0].ConnectorKey {
				t.Fatalf("truncated connector retained = %+v", connector)
			}
		}
	})
	t.Run("cycle error", func(t *testing.T) {
		source := strings.Replace(flowViewSource(), "cycle_policy include_cycle_ref", "cycle_policy error", 1)
		assertTreeFlowRejected(t, source, ViewMaterializationLimits{}, "forbidden directed cycle")
	})
	t.Run("lane modes", func(t *testing.T) {
		for _, mode := range []string{"none", "layer", "entity_type"} {
			t.Run(mode, func(t *testing.T) {
				source := strings.Replace(flowViewSource(), "lane_by attribute.status", "lane_by "+mode, 1)
				snapshot, queryResult := compileAndExecuteTreeFlowFixture(t, source)
				result := materializeQueryView(t, snapshot, queryResult, nil)
				if result.Flow == nil || len(result.Flow.Lanes) != 1 || len(result.Flow.Lanes[0].StepKeys) != 5 {
					t.Fatalf("%s lanes = %+v", mode, result.Flow)
				}
			})
		}
	})
	t.Run("ambiguous attribute lane", func(t *testing.T) {
		source := strings.Replace(flowViewSource(), "  c_task primary: work", "  c_task primary: work\n  c_task secondary: entry", 1)
		assertTreeFlowRejected(t, source, ViewMaterializationLimits{}, "multiple distinct attribute lane values")
	})
}

func TestMaterializeTreeAndFlowFailClosedForLimitsAndMissingSubjects(t *testing.T) {
	t.Parallel()
	for _, fixture := range []struct {
		name   string
		source string
	}{
		{name: "tree", source: treeViewSource()},
		{name: "flow", source: flowViewSource()},
	} {
		fixture := fixture
		t.Run(fixture.name+" item limit", func(t *testing.T) {
			assertTreeFlowRejected(t, fixture.source, ViewMaterializationLimits{MaxItems: 1}, "limit exceeded")
		})
		t.Run(fixture.name+" invalid limit", func(t *testing.T) {
			assertTreeFlowRejected(t, fixture.source, ViewMaterializationLimits{MaxItems: -1}, "limits must be positive")
		})
		t.Run(fixture.name+" work limit", func(t *testing.T) {
			assertTreeFlowRejected(t, fixture.source, ViewMaterializationLimits{MaxWork: 1}, "work limit exceeded")
		})
		t.Run(fixture.name+" missing subject", func(t *testing.T) {
			snapshot, queryResult := compileAndExecuteTreeFlowFixture(t, fixture.source)
			queryResult.PrimaryEntityAddresses = append(queryResult.PrimaryEntityAddresses, entityAddress("zz_missing"))
			response := New(BuildInfo{}).MaterializeView(context.Background(), queryViewInput(snapshot, queryResult, nil))
			if response.Status != "rejected" || response.Result != nil || !diagnosticsContain(response.Diagnostics, "unknown Entity") {
				t.Fatalf("missing subject response = %+v", response)
			}
		})
	}
}

func TestMaterializeTreeAndFlowRejectCorruptCompiledContracts(t *testing.T) {
	t.Parallel()
	t.Run("tree", func(t *testing.T) {
		snapshot, queryResult := compileAndExecuteTreeFlowFixture(t, treeViewSource())
		cases := []struct {
			name   string
			mutate func(*Snapshot)
		}{
			{name: "missing shape", mutate: func(value *Snapshot) { value.TypedAST.Views[0].Shape.Tree = nil }},
			{name: "empty relation types", mutate: func(value *Snapshot) { value.TypedAST.Views[0].Shape.Tree.RelationTypeAddresses = nil }},
			{name: "invalid cycle policy", mutate: func(value *Snapshot) {
				value.TypedAST.Views[0].Shape.Tree.CyclePolicy = view.TreeCyclePolicy("invalid")
			}},
			{name: "invalid shared child policy", mutate: func(value *Snapshot) {
				value.TypedAST.Views[0].Shape.Tree.SharedChildPolicy = view.SharedChildPolicy("invalid")
			}},
			{name: "missing projection", mutate: func(value *Snapshot) { value.TypedAST.RelationTypes[0].Projections.Tree = nil }},
			{name: "invalid projection endpoints", mutate: func(value *Snapshot) {
				value.TypedAST.RelationTypes[0].Projections.Tree.ChildEndpoint = definition.ProjectionEndpointFrom
			}},
		}
		for _, test := range cases {
			test := test
			t.Run(test.name, func(t *testing.T) {
				corrupt := deepClone(snapshot)
				test.mutate(&corrupt)
				assertCorruptTreeFlowRejected(t, corrupt, queryResult)
			})
		}
	})
	t.Run("flow", func(t *testing.T) {
		snapshot, queryResult := compileAndExecuteTreeFlowFixture(t, flowViewSource())
		cases := []struct {
			name   string
			mutate func(*Snapshot)
		}{
			{name: "missing shape", mutate: func(value *Snapshot) { value.TypedAST.Views[0].Shape.Flow = nil }},
			{name: "empty relation types", mutate: func(value *Snapshot) { value.TypedAST.Views[0].Shape.Flow.RelationTypeAddresses = nil }},
			{name: "invalid cycle policy", mutate: func(value *Snapshot) {
				value.TypedAST.Views[0].Shape.Flow.CyclePolicy = view.FlowCyclePolicy("invalid")
			}},
			{name: "invalid lane mode", mutate: func(value *Snapshot) { value.TypedAST.Views[0].Shape.Flow.LaneBy = view.LaneBy("invalid") }},
			{name: "missing projection", mutate: func(value *Snapshot) { value.TypedAST.RelationTypes[0].Projections.Flow = nil }},
			{name: "invalid projection endpoints", mutate: func(value *Snapshot) {
				value.TypedAST.RelationTypes[0].Projections.Flow.TargetEndpoint = definition.ProjectionEndpointFrom
			}},
			{name: "unknown branch column", mutate: func(value *Snapshot) {
				missing := "ldl:project:p:relation-type:link:column:missing"
				value.TypedAST.RelationTypes[0].Projections.Flow.BranchValueColumnAddress = &missing
			}},
		}
		for _, test := range cases {
			test := test
			t.Run(test.name, func(t *testing.T) {
				corrupt := deepClone(snapshot)
				test.mutate(&corrupt)
				assertCorruptTreeFlowRejected(t, corrupt, queryResult)
			})
		}
	})
}

func TestMaterializeFlowSupportsBranchlessAndReversedProjections(t *testing.T) {
	t.Parallel()
	t.Run("branchless", func(t *testing.T) {
		source := strings.Replace(flowViewSource(), "    branch_value_column branch\n", "", 1)
		snapshot, queryResult := compileAndExecuteTreeFlowFixture(t, source)
		result := materializeQueryView(t, snapshot, queryResult, nil)
		if result.Flow == nil || len(result.Flow.Connectors) != 6 {
			t.Fatalf("branchless flow = %+v", result.Flow)
		}
		merged := flowConnectorByEndpoints(t, *result.Flow, entityAddress("a_start"), entityAddress("b_task"))
		if merged.BranchValue != nil || len(merged.BranchRowAddresses) != 0 || len(merged.RelationAddresses) != 2 {
			t.Fatalf("branchless merged connector = %+v", merged)
		}
	})
	t.Run("reversed", func(t *testing.T) {
		source := strings.Replace(flowViewSource(), "    source_endpoint from\n    target_endpoint to", "    source_endpoint to\n    target_endpoint from", 1)
		snapshot, queryResult := compileAndExecuteTreeFlowFixture(t, source)
		result := materializeQueryView(t, snapshot, queryResult, nil)
		connector := flowConnectorByEndpoints(t, *result.Flow, entityAddress("b_task"), entityAddress("a_start"))
		if len(connector.RelationAddresses) != 2 {
			t.Fatalf("reversed connector = %+v", connector)
		}
	})
}

func TestTreeRootAndProjectionHelpersAreCanonical(t *testing.T) {
	t.Parallel()
	m := &viewMaterializer{queryResult: QueryResult{}, input: ViewMaterializationInput{Recipe: CompiledViewRecipe{Address: "ldl:project:p:view:v"}}}
	entities := []string{entityAddress("alpha"), entityAddress("beta"), entityAddress("gamma")}
	candidates := []treeCandidate{{parent: entities[0], child: entities[1]}}
	if got := m.treeRoots(entities, candidates); !reflect.DeepEqual(got, []string{entities[0], entities[2]}) {
		t.Fatalf("parentless roots = %v", got)
	}
	m.queryResult.SeedEntityAddresses = []string{entities[1], entityAddress("missing")}
	if got := m.treeRoots(entities, candidates); !reflect.DeepEqual(got, []string{entities[1]}) {
		t.Fatalf("seed roots = %v", got)
	}
	m.queryResult.SeedEntityAddresses = nil
	cycle := append(candidates, treeCandidate{parent: entities[1], child: entities[0]}, treeCandidate{parent: entities[0], child: entities[2]})
	cycle = append(cycle, treeCandidate{parent: entities[2], child: entities[0]})
	if got := m.treeRoots(entities, cycle); !reflect.DeepEqual(got, []string{entities[0]}) {
		t.Fatalf("cycle fallback root = %v", got)
	}
	if got := m.treeRoots(nil, nil); len(got) != 0 {
		t.Fatalf("empty roots = %v", got)
	}

	relation := graph.Relation{FromAddress: entities[0], ToAddress: entities[1]}
	from, to, ok := projectionEndpointPair(relation, definition.ProjectionEndpointTo, definition.ProjectionEndpointFrom)
	if !ok || from != entities[1] || to != entities[0] {
		t.Fatalf("reversed endpoints = (%q, %q, %t)", from, to, ok)
	}
	if _, _, ok := projectionEndpointPair(relation, definition.ProjectionEndpointFrom, definition.ProjectionEndpointFrom); ok {
		t.Fatal("identical projection endpoints were accepted")
	}

	relationTypeAddress := "ldl:project:p:relation-type:link"
	m.relationTypes = map[string]definition.RelationType{relationTypeAddress: {
		Address: relationTypeAddress,
		Projections: definition.ProjectionSet{Tree: &definition.TreeProjection{
			ParentEndpoint: definition.ProjectionEndpointFrom,
			ChildEndpoint:  definition.ProjectionEndpointTo,
		}},
	}}
	override := definition.ProjectionSet{Tree: &definition.TreeProjection{ParentEndpoint: definition.ProjectionEndpointTo, ChildEndpoint: definition.ProjectionEndpointFrom}}
	m.input.Recipe.RelationProjections = []view.RelationProjection{{RelationTypeAddress: relationTypeAddress, Projections: override}}
	if got, ok := m.effectiveViewProjectionSet(relationTypeAddress); !ok || !reflect.DeepEqual(got, override) {
		t.Fatalf("effective override = %+v ok=%v", got, ok)
	}
	m.input.Recipe.RelationProjections = append(m.input.Recipe.RelationProjections, m.input.Recipe.RelationProjections[0])
	if _, ok := m.effectiveViewProjectionSet(relationTypeAddress); ok {
		t.Fatal("duplicate projection overrides were accepted")
	}
	if _, ok := m.effectiveViewProjectionSet("ldl:project:p:relation-type:missing"); ok {
		t.Fatal("missing RelationType projection was accepted")
	}
}

func TestFlowCanonicalScalarAndOrderingContracts(t *testing.T) {
	t.Parallel()
	values := []struct {
		value TypedScalar
		label string
		rank  int
	}{
		{value: TypedScalar{Type: definition.ScalarString, String: "alpha"}, label: "alpha", rank: 0},
		{value: TypedScalar{Type: definition.ScalarEnum, String: "beta"}, label: "beta", rank: 1},
		{value: TypedScalar{Type: definition.ScalarInteger, Int: -2}, label: "-2", rank: 2},
		{value: TypedScalar{Type: definition.ScalarNumber, Float: 1.25}, label: "1.25", rank: 3},
		{value: TypedScalar{Type: definition.ScalarBoolean, Bool: true}, label: "true", rank: 4},
		{value: TypedScalar{Type: definition.ScalarDate, String: "2026-07-18"}, label: "2026-07-18", rank: 5},
		{value: TypedScalar{Type: definition.ScalarDatetime, String: "2026-07-18T00:00:00Z"}, label: "2026-07-18T00:00:00Z", rank: 6},
	}
	if scalarIdentity(nil).present {
		t.Fatal("absent scalar identity is present")
	}
	for index, test := range values {
		if got := flowScalarLabel(test.value); got != test.label || flowScalarTypeRank(test.value.Type) != test.rank || !scalarIdentity(&test.value).present {
			t.Fatalf("scalar %d label/rank/identity = %q/%d/%+v", index, got, flowScalarTypeRank(test.value.Type), scalarIdentity(&test.value))
		}
		if compareFlowScalars(test.value, test.value) != 0 {
			t.Fatalf("scalar %d is not reflexively equal", index)
		}
		if index > 0 && compareFlowScalars(values[index-1].value, test.value) >= 0 {
			t.Fatalf("scalar order %d -> %d is not increasing", index-1, index)
		}
	}
	if compareFlowScalars(TypedScalar{Type: definition.ScalarInteger, Int: 1}, TypedScalar{Type: definition.ScalarInteger, Int: 2}) >= 0 ||
		compareFlowScalars(TypedScalar{Type: definition.ScalarNumber, Float: 2}, TypedScalar{Type: definition.ScalarNumber, Float: 1}) <= 0 ||
		compareFlowScalars(TypedScalar{Type: definition.ScalarBoolean}, TypedScalar{Type: definition.ScalarBoolean, Bool: true}) >= 0 {
		t.Fatal("same-type scalar ordering is invalid")
	}
	invalid := definition.ScalarType("invalid")
	if flowScalarTypeRank(invalid) != 7 || flowScalarLabel(TypedScalar{Type: invalid}) != "" || compareFlowScalars(TypedScalar{Type: invalid}, TypedScalar{Type: invalid}) != 0 {
		t.Fatal("unknown scalar fallback is invalid")
	}

	kinds := []FlowConnectorKind{FlowConnectorSequence, FlowConnectorControl, FlowConnectorData, FlowConnectorMessage, FlowConnectorError, FlowConnectorKind("invalid")}
	for index, kind := range kinds {
		if flowConnectorKindRank(kind) != index {
			t.Fatalf("connector kind %q rank = %d", kind, flowConnectorKindRank(kind))
		}
	}
	for _, kind := range []definition.FlowConnectorKind{
		definition.FlowConnectorSequence, definition.FlowConnectorControl, definition.FlowConnectorData, definition.FlowConnectorMessage, definition.FlowConnectorError,
	} {
		if !validFlowProjection(&definition.FlowProjection{SourceEndpoint: definition.ProjectionEndpointFrom, TargetEndpoint: definition.ProjectionEndpointTo, ConnectorKind: kind}) {
			t.Fatalf("valid connector kind %q was rejected", kind)
		}
	}
	if validFlowProjection(nil) || validFlowProjection(&definition.FlowProjection{SourceEndpoint: definition.ProjectionEndpointFrom, TargetEndpoint: definition.ProjectionEndpointFrom, ConnectorKind: definition.FlowConnectorData}) ||
		validFlowProjection(&definition.FlowProjection{SourceEndpoint: definition.ProjectionEndpointFrom, TargetEndpoint: definition.ProjectionEndpointTo, ConnectorKind: definition.FlowConnectorKind("invalid")}) {
		t.Fatal("invalid Flow projection was accepted")
	}

	format := definition.StringFormatURI
	baseColumn := definition.Column{ValueType: definition.ScalarString, Format: &format}
	sameColumn := definition.Column{ValueType: definition.ScalarString, Format: &format}
	differentColumn := definition.Column{ValueType: definition.ScalarEnum, EnumValues: []string{"a"}}
	if !sameFlowColumnSchema(baseColumn, sameColumn) || sameFlowColumnSchema(baseColumn, differentColumn) || !equalStringFormat(nil, nil) || equalStringFormat(&format, nil) {
		t.Fatal("Flow column schema compatibility is invalid")
	}
	if _, ok := relationTypeColumn(definition.RelationType{Columns: []definition.Column{{Address: "column"}}}, "missing"); ok {
		t.Fatal("missing RelationType Column was found")
	}
}

func TestFlowComparatorUsesEveryCanonicalTieBreaker(t *testing.T) {
	t.Parallel()
	base := flowConnectorCandidate{
		from: entityAddress("a"), to: entityAddress("b"), kind: FlowConnectorData,
		relationAddresses: []string{relationAddress("a")}, branchRows: []string{relationAddress("a") + ":row:a"},
	}
	cases := []flowConnectorCandidate{
		func() flowConnectorCandidate { value := base; value.from = entityAddress("b"); return value }(),
		func() flowConnectorCandidate { value := base; value.to = entityAddress("c"); return value }(),
		func() flowConnectorCandidate { value := base; value.kind = FlowConnectorMessage; return value }(),
		func() flowConnectorCandidate {
			value := base
			scalar := TypedScalar{Type: definition.ScalarString, String: "a"}
			value.branchValue = &scalar
			return value
		}(),
		func() flowConnectorCandidate {
			value := base
			value.relationAddresses = []string{relationAddress("b")}
			return value
		}(),
		func() flowConnectorCandidate {
			value := base
			value.branchRows = []string{relationAddress("a") + ":row:b"}
			return value
		}(),
	}
	for index, value := range cases {
		if compareFlowConnectorCandidates(base, value) >= 0 || compareFlowConnectorCandidates(value, base) <= 0 {
			t.Fatalf("connector tie-breaker %d is not antisymmetric", index)
		}
	}
	laneBase := flowLaneDescriptor{mode: view.LaneAttribute, address: "a", value: &TypedScalar{Type: definition.ScalarString, String: "a"}}
	laneCases := []flowLaneDescriptor{
		{mode: view.LaneEntityType, address: "a"},
		{mode: view.LaneAttribute, address: "b"},
		{mode: view.LaneAttribute, address: "a", value: &TypedScalar{Type: definition.ScalarString, String: "b"}},
	}
	for index, value := range laneCases {
		if compareFlowLaneDescriptors(laneBase, value) == 0 {
			t.Fatalf("lane tie-breaker %d compared equal", index)
		}
	}
	missing := flowLaneDescriptor{mode: view.LaneAttribute, address: "a", missing: true}
	present := flowLaneDescriptor{mode: view.LaneAttribute, address: "a", value: &TypedScalar{Type: definition.ScalarString, String: "a"}}
	if compareFlowLaneDescriptors(missing, present) >= 0 || compareFlowLaneDescriptors(present, missing) <= 0 {
		t.Fatal("missing lane ordering is invalid")
	}
}

func assertCorruptTreeFlowRejected(t *testing.T, snapshot Snapshot, queryResult QueryResult) {
	t.Helper()
	first := New(BuildInfo{}).MaterializeView(context.Background(), queryViewInput(snapshot, queryResult, nil))
	second := New(BuildInfo{}).MaterializeView(context.Background(), queryViewInput(snapshot, queryResult, nil))
	if first.Status != "rejected" || first.Result != nil || len(first.Diagnostics) == 0 || !reflect.DeepEqual(first, second) {
		t.Fatalf("corrupt compiled contract response = %+v", first)
	}
}

func compileAndExecuteTreeFlowFixture(t *testing.T, source string) (Snapshot, QueryResult) {
	t.Helper()
	snapshot := compileQueryExecutionFixture(t, source)
	if len(snapshot.TypedAST.Views) != 1 {
		t.Fatalf("views = %+v", snapshot.TypedAST.Views)
	}
	response, err := New(BuildInfo{}).ExecuteQuery(context.Background(), QueryExecutionInput{
		Recipe: snapshot.TypedAST.Queries[0], Graph: *snapshot.TypedAST.Graph, Definition: snapshot.QueryDefinitionIdentity(), Arguments: map[string]TypedScalar{},
	})
	if err != nil {
		t.Fatal(err)
	}
	if response.Status != "ok" || response.Result == nil {
		t.Fatalf("ExecuteQuery() = %+v", response)
	}
	return snapshot, *response.Result
}

func assertTreeFlowRejected(t *testing.T, source string, limits ViewMaterializationLimits, diagnosticFragment string) {
	t.Helper()
	snapshot, queryResult := compileAndExecuteTreeFlowFixture(t, source)
	input := queryViewInput(snapshot, queryResult, nil)
	input.Limits = limits
	response := New(BuildInfo{}).MaterializeView(context.Background(), input)
	if response.Status != "rejected" || response.Result != nil || !diagnosticsContain(response.Diagnostics, diagnosticFragment) {
		t.Fatalf("MaterializeView() = %+v, want diagnostic containing %q", response, diagnosticFragment)
	}
}

func diagnosticsContain(values []Diagnostic, fragment string) bool {
	for _, value := range values {
		if strings.Contains(value.Message, fragment) {
			return true
		}
	}
	return false
}

func flowStepsByEntity(values []FlowStep) map[string]FlowStep {
	result := make(map[string]FlowStep, len(values))
	for _, value := range values {
		result[value.EntityAddress] = value
	}
	return result
}

func flowStepsByKey(values []FlowStep) map[string]FlowStep {
	result := make(map[string]FlowStep, len(values))
	for _, value := range values {
		result[value.Key] = value
	}
	return result
}

func flowConnectorByEndpoints(t *testing.T, flow FlowViewData, from, to string) FlowConnector {
	t.Helper()
	steps := flowStepsByKey(flow.Steps)
	for _, connector := range flow.Connectors {
		if steps[connector.FromStepKey].EntityAddress == from && steps[connector.ToStepKey].EntityAddress == to {
			return connector
		}
	}
	t.Fatalf("connector %s -> %s not found", from, to)
	return FlowConnector{}
}

func flowLaneByLabel(t *testing.T, values []FlowLane, label string) FlowLane {
	t.Helper()
	for _, value := range values {
		if value.Label == label {
			return value
		}
	}
	t.Fatalf("lane %q not found", label)
	return FlowLane{}
}

func entityAddress(id string) string   { return "ldl:project:p:entity:" + id }
func relationAddress(id string) string { return "ldl:project:p:relation:" + id }

func treeViewSource() string {
	return `
project p "Project" {}

layers {
  app "Application" @10
}

entity_type node "Node" {
  representation shape rect
}

relation_type link "Link" dependency {
  duplicate_policy allow
  from parent types [node] layers [app]
  to child types [node] layers [app]
  label "links"
  projection tree {
    parent_endpoint from
    child_endpoint to
  }
}

entities node @app {
  root "Root"
  alpha "Alpha"
  beta "Beta"
  shared "Shared"
}

relations link {
  a_root_alpha: root -> alpha
  b_root_beta: root -> beta
  c_alpha_shared: alpha -> shared
  d_beta_shared: beta -> shared
  e_shared_root: shared -> root
}

query scope "Scope" {
  select {
    entity_types [node]
    relation_types [link]
    roots [root]
  }
  traverse outgoing 0..8 visit_once relations [link]
  result [seed_entities, traversed_entities, path_relations, induced_relations]
}

view hierarchy "Hierarchy" hierarchy {
  source query scope {}
  tree {
    relation_types [link]
    cycle_policy truncate
    shared_child_policy link
  }
}
`
}

func flowViewSource() string {
	return `
project p "Project" {}

layers {
  app "Application" @10
}

entity_type node "Node" {
  representation shape rect
  columns {
    status "Status" enum [entry, work]
  }
}

relation_type link "Link" data_flow {
  duplicate_policy allow
  from source types [node] layers [app]
  to target types [node] layers [app]
  label "flows to"
  columns {
    branch "Branch" string
  }
  projection flow {
    source_endpoint from
    target_endpoint to
    connector_kind data
    branch_value_column branch
  }
}

entities node @app {
  a_start "Start"
  b_task "Task B"
  c_task "Task C"
  d_join "Join"
  e_end "End"
}

rows node [status] {
  a_start primary: entry
  b_task primary: work
  b_task secondary: work
  c_task primary: work
  d_join primary: work
}

relations link {
  a_start_task: a_start -> b_task
  b_start_task_parallel: a_start -> b_task
  c_start_other: a_start -> c_task
  d_task_join: b_task -> d_join
  e_other_join: c_task -> d_join
  f_join_end: d_join -> e_end
  g_end_start: e_end -> a_start
}

relation_rows link [branch] {
  a_start_task primary: "yes"
  a_start_task retry: "yes"
  b_start_task_parallel primary: "yes"
  c_start_other primary: "no"
  d_task_join primary: "next"
  e_other_join primary: "next"
  g_end_start primary: "loop"
}

query scope "Scope" {
  select {
    entity_types [node]
    relation_types [link]
    roots [a_start]
  }
  traverse outgoing 0..10 visit_once relations [link]
  result [seed_entities, traversed_entities, path_relations, induced_relations]
}

view process "Process" flow {
  source query scope {}
  flow {
    relation_types [link]
    lane_by attribute.status
    cycle_policy include_cycle_ref
  }
}
`
}
