// SPDX-License-Identifier: LicenseRef-LayerDraw-1.0

package engine

import (
	"context"
	"reflect"
	"strings"
	"testing"

	"github.com/dencyuinc/layerdraw/internal/engine/internal/compiler/semantic/definition"
	"github.com/dencyuinc/layerdraw/internal/engine/internal/compiler/semantic/graph"
)

func TestMaterializeComposedDiagramProjectsEveryModeWithCompleteSources(t *testing.T) {
	t.Parallel()
	snapshot, queryResult := compileAndExecuteDiagramFixture(t, composedDiagramSource())
	first := materializeQueryView(t, snapshot, queryResult, nil)
	second := materializeQueryView(t, snapshot, queryResult, nil)
	if !reflect.DeepEqual(first, second) {
		t.Fatal("composed Diagram materialization is not deterministic")
	}
	diagram := first.Diagram
	if diagram == nil || len(diagram.Occurrences) != 5 || len(diagram.Edges) != 2 || len(diagram.Containers) != 1 || len(diagram.Overlays) != 1 || len(diagram.Badges) != 1 || len(diagram.SupportItems) != 1 {
		t.Fatalf("diagram = %+v", diagram)
	}

	root := diagramOccurrenceByEntity(t, *diagram, "ldl:project:p:entity:root")
	child := diagramOccurrenceByEntity(t, *diagram, "ldl:project:p:entity:child")
	if root.Role != DiagramRoleContainer || child.ParentKey == nil || *child.ParentKey != root.Key || child.ViaRelationAddress == nil || *child.ViaRelationAddress != "ldl:project:p:relation:root_child" {
		t.Fatalf("root/child occurrences = root:%+v child:%+v", root, child)
	}
	assertCompleteDiagramItemSource(t, root.Source, false)
	assertCompleteDiagramItemSource(t, child.Source, true)

	container := diagram.Containers[0]
	if container.OccurrenceKey != root.Key || !reflect.DeepEqual(container.ChildKeys, []string{child.Key}) {
		t.Fatalf("container = %+v", container)
	}
	assertCompleteDiagramItemSource(t, container.Source, true)

	nestEdge := diagramEdgeByRelation(t, *diagram, "ldl:project:p:relation:root_child")
	if nestEdge.FromOccurrenceKey != root.Key || nestEdge.ToOccurrenceKey != child.Key {
		t.Fatalf("nest keep-edge projection = %+v", nestEdge)
	}
	if diagram.Edges[0].RelationAddress != nestEdge.RelationAddress {
		t.Fatalf("candidate priority order = %+v", diagram.Edges)
	}
	peer := diagramOccurrenceByEntity(t, *diagram, "ldl:project:p:entity:peer")
	reversed := diagramEdgeByRelation(t, *diagram, "ldl:project:p:relation:child_peer")
	if reversed.FromOccurrenceKey != peer.Key || reversed.ToOccurrenceKey != child.Key {
		t.Fatalf("Diagram endpoint override = %+v", reversed)
	}
	assertCompleteDiagramItemSource(t, reversed.Source, true)

	overlay := diagram.Overlays[0]
	if overlay.TargetOccurrenceKey != child.Key || overlay.OverlayEntityAddress != "ldl:project:p:entity:shield" || overlay.RelationAddress != "ldl:project:p:relation:shield_child" {
		t.Fatalf("overlay = %+v", overlay)
	}
	assertCompleteDiagramItemSource(t, overlay.Source, true)
	badge := diagram.Badges[0]
	if badge.TargetOccurrenceKey != child.Key || badge.RelationAddress != "ldl:project:p:relation:badge_child" || badge.Label == nil || *badge.Label != "Badge" {
		t.Fatalf("badge = %+v", badge)
	}
	assertCompleteDiagramItemSource(t, badge.Source, true)

	support := diagram.SupportItems[0]
	if support.SupportKind != DiagramSupportHiddenRelation || support.RelationAddress == nil || *support.RelationAddress != "ldl:project:p:relation:peer_root" {
		t.Fatalf("support = %+v", support)
	}
	assertCompleteDiagramItemSource(t, support.Source, true)

	base, ok := first.Base()
	if !ok || base.Shape.Diagram == nil || len(base.Shape.Diagram.Placements) != 1 || base.Shape.Diagram.Placements[0].EntityAddress != root.EntityAddress || base.Shape.Diagram.Placements[0].X != 10 {
		t.Fatalf("semantic placement intent = %+v", base.Shape)
	}
	override := snapshot.TypedAST.Views[0].RelationProjections[0].Projections.Composed
	if override.Priority != 20 || !override.KeepEdge || override.ParentEndpoint == nil || *override.ParentEndpoint != definition.ProjectionEndpointFrom || override.ChildEndpoint == nil || *override.ChildEndpoint != definition.ProjectionEndpointTo {
		t.Fatalf("effective View override did not retain RelationType endpoints: %+v", override)
	}
}

func TestMaterializeComposedDiagramResolvesParentConflictsByClosedPolicy(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name        string
		conflict    string
		wantParent  string
		wantEdges   int
		wantSupport int
		wantWarning bool
	}{
		{name: "diagnostic", conflict: "diagnostic", wantSupport: 2, wantWarning: true},
		{name: "prefer first", conflict: "prefer_first", wantParent: "ldl:project:p:entity:p1", wantSupport: 1},
		{name: "keep edge", conflict: "keep_edge", wantEdges: 2},
	}
	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			snapshot, queryResult := compileAndExecuteDiagramFixture(t, conflictingDiagramSource(tc.conflict))
			result := materializeQueryView(t, snapshot, queryResult, nil)
			diagram := result.Diagram
			if diagram == nil || len(diagram.Edges) != tc.wantEdges || len(diagram.SupportItems) != tc.wantSupport {
				t.Fatalf("diagram = %+v", diagram)
			}
			child := diagramOccurrenceByEntity(t, *diagram, "ldl:project:p:entity:child")
			if tc.wantParent == "" {
				if child.ParentKey != nil {
					t.Fatalf("unexpected parent = %+v", child)
				}
			} else {
				parent := diagramOccurrenceByEntity(t, *diagram, tc.wantParent)
				if child.ParentKey == nil || *child.ParentKey != parent.Key {
					t.Fatalf("selected parent = %+v want %s", child, parent.Key)
				}
			}
			base, _ := result.Base()
			warnings := 0
			for _, value := range base.Diagnostics {
				if value.Code == "LDL1704" && value.Severity == "warning" && value.MessageKey == "composed_parent_ambiguity_retained" {
					warnings++
				}
			}
			if (warnings == 1) != tc.wantWarning {
				t.Fatalf("diagnostics = %+v", base.Diagnostics)
			}
		})
	}
}

func TestMaterializeComposedDiagramFailsClosedForInvalidDerivedStructure(t *testing.T) {
	t.Parallel()

	t.Run("nesting cycle", func(t *testing.T) {
		snapshot, queryResult := compileAndExecuteDiagramFixture(t, cyclicDiagramSource())
		response := New(BuildInfo{}).MaterializeView(context.Background(), queryViewInput(snapshot, queryResult, nil))
		assertRejectedDiagram(t, response, "LDL1702")
	})

	snapshot, queryResult := compileAndExecuteDiagramFixture(t, conflictingDiagramSource("prefer_first"))
	t.Run("duplicate occurrence input", func(t *testing.T) {
		duplicate := deepClone(queryResult)
		duplicate.PrimaryEntityAddresses = append(duplicate.PrimaryEntityAddresses, duplicate.PrimaryEntityAddresses[len(duplicate.PrimaryEntityAddresses)-1])
		response := New(BuildInfo{}).MaterializeView(context.Background(), queryViewInput(snapshot, duplicate, nil))
		assertRejectedDiagram(t, response, "LDL1801")
	})

	t.Run("missing endpoint closure", func(t *testing.T) {
		open := deepClone(queryResult)
		open.PrimaryEntityAddresses = []string{"ldl:project:p:entity:child", "ldl:project:p:entity:p1"}
		response := New(BuildInfo{}).MaterializeView(context.Background(), queryViewInput(snapshot, open, nil))
		assertRejectedDiagram(t, response, "LDL1801")
	})

	t.Run("invalid effective projection", func(t *testing.T) {
		invalid := deepClone(snapshot)
		value := definition.ProjectionEndpointFrom
		invalid.TypedAST.RelationTypes[0].Projections.Composed.ChildEndpoint = &value
		response := New(BuildInfo{}).MaterializeView(context.Background(), queryViewInput(invalid, queryResult, nil))
		assertRejectedDiagram(t, response, "LDL1504")
	})

	t.Run("placement targets nested occurrence", func(t *testing.T) {
		placedSource := strings.Replace(composedDiagramSource(), "place root 10 20 300 200", "place child 10 20 300 200", 1)
		placedSnapshot, placedResult := compileAndExecuteDiagramFixture(t, placedSource)
		response := New(BuildInfo{}).MaterializeView(context.Background(), queryViewInput(placedSnapshot, placedResult, nil))
		assertRejectedDiagram(t, response, "LDL1702")
	})
}

func TestDiagramBadgeLabelUsesClosedSemanticValues(t *testing.T) {
	t.Parallel()
	m := &viewMaterializer{
		input:       ViewMaterializationInput{Recipe: CompiledViewRecipe{Address: "ldl:project:p:view:v"}},
		entities:    map[string]graph.Entity{"badge": {Address: "badge", DisplayName: "Badge", TypeAddress: "node"}},
		entityTypes: map[string]definition.EntityType{"node": {Address: "node", DisplayName: "Node"}},
	}
	candidate := diagramProjectionCandidate{relation: graph.Relation{TypeAddress: "marks"}}
	typeLabel := "Node"
	displayNameLabel := "Badge"
	countLabel := "1"
	cases := []struct {
		label definition.RenderBadgeLabel
		want  *string
	}{
		{label: definition.RenderBadgeLabelType, want: &typeLabel},
		{label: definition.RenderBadgeLabelDisplayName, want: &displayNameLabel},
		{label: definition.RenderBadgeLabelCount, want: &countLabel},
		{label: definition.RenderBadgeLabelNone},
	}
	for _, tc := range cases {
		candidate.render.Badge.Label = tc.label
		if got := m.diagramBadgeLabel(candidate, "badge"); !reflect.DeepEqual(got, tc.want) {
			t.Fatalf("label %s = %v want %v", tc.label, got, tc.want)
		}
	}
}

func TestDiagramProjectionEndpointPairPreservesSelfRelations(t *testing.T) {
	t.Parallel()
	m := &viewMaterializer{input: ViewMaterializationInput{Recipe: CompiledViewRecipe{Address: "ldl:project:p:view:v"}}}
	relation := graph.Relation{Address: "self", TypeAddress: "links", FromAddress: "node", ToAddress: "node"}
	from, to, ok := m.diagramProjectionEndpointPair(relation, definition.ProjectionEndpointFrom, definition.ProjectionEndpointTo)
	if !ok || from != "node" || to != "node" || len(m.diagnostics) != 0 {
		t.Fatalf("self Relation endpoints = (%q, %q, %t), diagnostics = %+v", from, to, ok, m.diagnostics)
	}
}

func compileAndExecuteDiagramFixture(t *testing.T, source string) (Snapshot, QueryResult) {
	t.Helper()
	snapshot := compileViewFixture(t, source)
	response, err := New(BuildInfo{}).ExecuteQuery(context.Background(), QueryExecutionInput{
		Recipe: snapshot.TypedAST.Queries[0], Graph: *snapshot.TypedAST.Graph,
	})
	if err != nil {
		t.Fatal(err)
	}
	if response.Status != "ok" || response.Result == nil {
		t.Fatalf("ExecuteQuery() = %+v", response)
	}
	return snapshot, *response.Result
}

func assertRejectedDiagram(t *testing.T, response ViewMaterializationResponse, code string) {
	t.Helper()
	if response.Status != "rejected" || response.Result != nil || len(response.Diagnostics) == 0 {
		t.Fatalf("response = %+v", response)
	}
	found := false
	for _, value := range response.Diagnostics {
		if value.Code == code {
			found = true
		}
	}
	if !found {
		t.Fatalf("diagnostics = %+v want code %s", response.Diagnostics, code)
	}
}

func assertCompleteDiagramItemSource(t *testing.T, source ViewDataSourceRefs, relation bool) {
	t.Helper()
	if len(source.EntityAddresses) == 0 || len(source.RowAddresses) == 0 || len(source.CellRefs) == 0 {
		t.Fatalf("incomplete Entity/row/cell source = %+v", source)
	}
	if relation && len(source.RelationAddresses) == 0 {
		t.Fatalf("incomplete Relation source = %+v", source)
	}
}

func diagramOccurrenceByEntity(t *testing.T, diagram DiagramViewData, address string) DiagramOccurrence {
	t.Helper()
	for _, value := range diagram.Occurrences {
		if value.EntityAddress == address {
			return value
		}
	}
	t.Fatalf("occurrence %s is absent", address)
	return DiagramOccurrence{}
}

func diagramEdgeByRelation(t *testing.T, diagram DiagramViewData, address string) DiagramEdge {
	t.Helper()
	for _, value := range diagram.Edges {
		if value.RelationAddress == address {
			return value
		}
	}
	t.Fatalf("edge %s is absent", address)
	return DiagramEdge{}
}

func composedDiagramSource() string {
	return `
project p "Project" {}
layers {
  infra "Infrastructure" @10
  app "Application" @20
  security "Security" @30
}
entity_type group "Group" {
  representation container
  columns {
    note "Note" string
  }
}
entity_type node "Node" {
  representation shape rect
  columns {
    note "Note" string
  }
}
relation_type contains "Contains" containment {
  duplicate_policy allow
  from parent types [group] layers [infra]
  to child types [node] layers [app]
  label "contains"
  columns {
    note "Note" string
  }
  projection composed {
    mode nest
    parent_endpoint from
    child_endpoint to
    priority 1
    conflict diagnostic
    keep_edge false
  }
}
relation_type protects "Protects" security {
  duplicate_policy allow
  from control types [node] layers [security]
  to target types [node] layers [app]
  label "protects"
  columns {
    note "Note" string
  }
  projection composed {
    mode overlay
    overlay_endpoint from
    target_endpoint to
  }
}
relation_type marks "Marks" reference {
  duplicate_policy allow
  from badge types [node] layers [security]
  to target types [node] layers [app]
  label "marks"
  columns {
    note "Note" string
  }
  projection composed {
    mode badge
    badge_endpoint from
    target_endpoint to
  }
  render badge {
    label display_name
  }
}
relation_type links "Links" dependency {
  duplicate_policy allow
  from source types [node] layers [app]
  to target types [node] layers [app]
  label "links"
  columns {
    note "Note" string
  }
  projection composed {
    mode edge
  }
  projection diagram {
    mode edge
    source_endpoint to
    target_endpoint from
  }
}
relation_type conceals "Conceals" governance {
  duplicate_policy allow
  from source types [node] layers [app]
  to target types [group] layers [infra]
  label "conceals"
  columns {
    note "Note" string
  }
  projection composed {
    mode hide
  }
}
entities group @infra {
  root "Root"
}
entities node @app {
  child "Child"
  peer "Peer"
}
entities node @security {
  badge "Badge"
  shield "Shield"
}
rows group [note] {
  root primary: "root row"
}
rows node [note] {
  child primary: "child row"
  peer primary: "peer row"
  badge primary: "badge row"
  shield primary: "shield row"
}
relations contains {
  root_child: root -> child
}
relations protects {
  shield_child: shield -> child
}
relations marks {
  badge_child: badge -> child
}
relations links {
  child_peer: child -> peer
}
relations conceals {
  peer_root: peer -> root
}
relation_rows contains [note] {
  root_child primary: "contains row"
}
relation_rows protects [note] {
  shield_child primary: "protects row"
}
relation_rows marks [note] {
  badge_child primary: "marks row"
}
relation_rows links [note] {
  child_peer primary: "links row"
}
relation_rows conceals [note] {
  peer_root primary: "conceals row"
}
query scope "Scope" {
  select {
    layers [infra, app, security]
    entity_types [group, node]
    relation_types [contains, protects, marks, links, conceals]
    roots [root, child, peer, badge, shield]
  }
  result [seed_entities, induced_relations]
}
view composed "Composed" topology {
  source query scope {}
  relation_projection contains {
    composed {
      priority 20
      keep_edge true
    }
  }
  diagram {
    layout layered
    direction left_to_right
    abstraction normal
    composed
    place root 10 20 300 200
  }
}
`
}

func conflictingDiagramSource(conflict string) string {
	source := `
project p "Project" {}
layers {
  app "Application" @10
}
entity_type group "Group" {
  representation container
  columns {
    note "Note" string
  }
}
entity_type node "Node" {
  representation shape rect
  columns {
    note "Note" string
  }
}
relation_type contains "Contains" containment {
  duplicate_policy allow
  from parent types [group] layers [app]
  to child types [node] layers [app]
  label "contains"
  columns {
    note "Note" string
  }
  projection composed {
    mode nest
    parent_endpoint from
    child_endpoint to
    conflict CONFLICT
    keep_edge false
  }
}
entities group @app {
  p1 "Parent 1"
  p2 "Parent 2"
}
entities node @app {
  child "Child"
}
rows group [note] {
  p1 primary: "p1"
  p2 primary: "p2"
}
rows node [note] {
  child primary: "child"
}
relations contains {
  p1_child: p1 -> child
  p2_child: p2 -> child
}
relation_rows contains [note] {
  p1_child primary: "one"
  p2_child primary: "two"
}
query scope "Scope" {
  select {
    roots [p1, p2, child]
    entity_types [group, node]
    relation_types [contains]
  }
  result [seed_entities, induced_relations]
}
view composed "Composed" topology {
  source query scope {}
  diagram {
    layout layered
    direction left_to_right
    abstraction normal
    composed
  }
}
`
	return strings.Replace(source, "CONFLICT", conflict, 1)
}

func cyclicDiagramSource() string {
	return `
project p "Project" {}
layers {
  app "Application" @10
}
entity_type group "Group" {
  representation container
  columns {
    note "Note" string
  }
}
relation_type contains "Contains" containment {
  duplicate_policy allow
  from parent types [group] layers [app]
  to child types [group] layers [app]
  label "contains"
  columns {
    note "Note" string
  }
  projection composed {
    mode nest
    parent_endpoint from
    child_endpoint to
    conflict prefer_first
    keep_edge false
  }
}
entities group @app {
  a "A"
  b "B"
}
rows group [note] {
  a primary: "a"
  b primary: "b"
}
relations contains {
  a_b: a -> b
  b_a: b -> a
}
relation_rows contains [note] {
  a_b primary: "one"
  b_a primary: "two"
}
query scope "Scope" {
  select {
    roots [a, b]
    entity_types [group]
    relation_types [contains]
  }
  result [seed_entities, induced_relations]
}
view composed "Composed" topology {
  source query scope {}
  diagram {
    layout layered
    direction left_to_right
    abstraction normal
    composed
  }
}
`
}
