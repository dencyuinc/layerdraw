// SPDX-License-Identifier: LicenseRef-LayerDraw-1.0

package query

import (
	"fmt"
	"reflect"
	"strings"
	"sync"
	"testing"

	"github.com/dencyuinc/layerdraw/internal/engine/internal/compiler/semantic/definition"
)

func TestAbsentAndExplicitlyEmptySelectorsAndDefaults(t *testing.T) {
	got := compileProject(t, map[string]string{"document.ldl": minimalSchema + `
query absent "Absent" {
  select {}
}
query empty "Empty" {
  select {
    layers []
    entity_types []
    relation_types []
    roots []
  }
  where all {}
  relation_where any {}
  traverse outgoing 0..0 visit_once relations []
  result []
}
`})
	if got.HasErrors || len(got.Recipes) != 2 {
		t.Fatalf("recipes=%+v diagnostics=%+v", got.Recipes, got.Diagnostics)
	}
	absent, empty := got.Recipes[0], got.Recipes[1]
	if absent.Select.LayerAddresses != nil || absent.Select.EntityTypeAddresses != nil || absent.Select.RelationTypeAddresses != nil || absent.Select.RootAddresses != nil {
		t.Fatalf("absent selectors were materialized: %+v", absent.Select)
	}
	if absent.Traversal != nil || absent.StateInput != StateNone || !reflect.DeepEqual(absent.Result, []ResultMember{ResultSeedEntities, ResultTraversedEntities, ResultPathRelations}) || absent.Where.Kind != PredicateAll || len(absent.Where.Children) != 0 || absent.RelationWhere.Kind != PredicateAll {
		t.Fatalf("defaults = %+v", absent)
	}
	if empty.Select.LayerAddresses == nil || len(*empty.Select.LayerAddresses) != 0 || empty.Select.RootAddresses == nil || len(*empty.Select.RootAddresses) != 0 || empty.Traversal == nil || empty.Traversal.RelationTypeAddresses == nil || len(*empty.Traversal.RelationTypeAddresses) != 0 || len(empty.Result) != 0 {
		t.Fatalf("explicit empties were lost: %+v", empty)
	}
	if empty.RelationWhere.Kind != PredicateAny || len(empty.RelationWhere.Children) != 0 {
		t.Fatalf("authored empty any predicate = %+v", empty.RelationWhere)
	}
}

func TestEveryPredicateAndOperatorFamilyCompiles(t *testing.T) {
	source := `
project p "Project" {}
layers {
  app "App" @0
}
entity_type item "Item" {
  representation shape rect
  columns {
    count "Count" integer
    ratio "Ratio" number
    born "Born" date
    seen "Seen" datetime
    enabled "Enabled" boolean
    status "Status" enum [active, paused]
    text "Text" string
  }
}
relation_type links "Links" reference {
  from source types [item] layers [app]
  to target types [item] layers [app]
  label "links"
}
entities item @app {
  root "Root"
}
query operators "Operators" {
  state_input optional
  parameters {
    exact string required
  }
  select {
    roots [root]
  }
  where all {
    field id == "root"
    field display_name != $exact
    field display_name contains "oo"
    field display_name starts_with "R"
    field display_name ends_with "t"
    field description missing
    field address == root
    field type in [item]
    field layer not_in [app]
    field tags == [critical]
    field tags != []
    field tags contains critical
    field tags contains "release"
    state system.created_at exists
    state provenance.confidence >= 0.5
    any {
      field display_name exists
      not {
        field tags contains ignored
      }
    }
    rows none types [item] {
      all {
        cell count < 10
        cell ratio <= 1.5
        cell born > "2020-01-01"
        cell seen >= "2020-01-01T00:00:00Z"
        cell enabled in [true, false]
        cell status not_in [paused]
        cell count in [1, 2]
        cell text starts_with "a"
        not {
          cell enabled == true
        }
        state provenance.verified_at missing
      }
    }
  }
  relation_where all {
    field display_name missing
    field type == links
    field from == root
    field to != root
    state system.updated_by.id exists
    rows all types [links] {
      state provenance.source.label exists
    }
  }
}
`
	got := compileProject(t, map[string]string{"document.ldl": source})
	if got.HasErrors || len(got.Recipes) != 1 {
		t.Fatalf("recipes=%+v diagnostics=%+v", got.Recipes, got.Diagnostics)
	}
	recipe := got.Recipes[0]
	seen := map[Operator]bool{}
	collectPredicateOperators(recipe.Where, seen)
	collectPredicateOperators(recipe.RelationWhere, seen)
	for _, operator := range []Operator{OperatorEqual, OperatorNotEqual, OperatorLess, OperatorLessEqual, OperatorGreater, OperatorGreaterEq, OperatorIn, OperatorNotIn, OperatorContains, OperatorStartsWith, OperatorEndsWith, OperatorExists, OperatorMissing} {
		if !seen[operator] {
			t.Errorf("operator %s was not compiled; seen=%+v", operator, seen)
		}
	}
	if len(recipe.Dependencies.StateReads) != 5 {
		t.Fatalf("state reads = %+v", recipe.Dependencies.StateReads)
	}
	if !containsRowKind(recipe.Where, PredicateCell) {
		t.Fatalf("row cells did not retain their normalized kind: %+v", recipe.Where)
	}
}

func TestTraversalDirectionsAndCyclePoliciesCompile(t *testing.T) {
	got := compileProject(t, map[string]string{"document.ldl": minimalSchema + `
query outgoing "Outgoing" {
  select {}
  traverse outgoing 0..1 error
}
query incoming "Incoming" {
  select {}
  traverse incoming 1..2 visit_once
}
query both "Both" {
  select {
    relation_types [r]
  }
  traverse both 0..3 include_cycle_ref relations [r]
}
`})
	if got.HasErrors || len(got.Recipes) != 3 {
		t.Fatalf("traversal recipes=%+v diagnostics=%+v", got.Recipes, got.Diagnostics)
	}
	want := map[string]struct {
		direction definition.TraversalDirection
		cycle     CyclePolicy
		minimum   int64
		maximum   int64
	}{
		"outgoing": {definition.TraversalOutgoing, CycleError, 0, 1},
		"incoming": {definition.TraversalIncoming, CycleVisitOnce, 1, 2},
		"both":     {definition.TraversalBoth, CycleIncludeCycleRef, 0, 3},
	}
	for _, recipe := range got.Recipes {
		expected := want[recipe.ID]
		if recipe.Traversal == nil || recipe.Traversal.Direction != expected.direction || recipe.Traversal.CyclePolicy != expected.cycle || recipe.Traversal.MinDepth != expected.minimum || recipe.Traversal.MaxDepth != expected.maximum {
			t.Errorf("%s traversal = %+v", recipe.ID, recipe.Traversal)
		}
	}
}

func TestInvalidAcceptanceMatrixAndTransactionalFailure(t *testing.T) {
	tests := []struct {
		name  string
		query string
		code  string
	}{
		{"unknown operator", "query bad \"Bad\" {\n  select {}\n  where all {\n    field display_name matches \"x\"\n  }\n}", "LDL1601"},
		{"incompatible operator", "query bad \"Bad\" {\n  select {}\n  where all {\n    field display_name < \"x\"\n  }\n}", "LDL1601"},
		{"address string operator", "query bad \"Bad\" {\n  select {}\n  where all {\n    field address contains root\n  }\n}", "LDL1601"},
		{"parameter type mismatch", "query bad \"Bad\" {\n  parameters {\n    n number\n  }\n  select {}\n  where all {\n    field display_name == $n\n  }\n}", "LDL1601"},
		{"parameter invalid default", "query bad \"Bad\" {\n  parameters {\n    n number default 11 max 10\n  }\n  select {}\n}", "LDL1401"},
		{"state policy missing", "query bad \"Bad\" {\n  select {}\n  where all {\n    state system.updated_at exists\n  }\n}", "LDL1601"},
		{"state policy unused", "query bad \"Bad\" {\n  state_input required\n  select {}\n}", "LDL1601"},
		{"unknown state path", "query bad \"Bad\" {\n  state_input optional\n  select {}\n  where all {\n    state audit.event exists\n  }\n}", "LDL1601"},
		{"ambiguous row column", "query bad \"Bad\" {\n  select {}\n  where all {\n    rows any types [a, b] {\n      cell shared == 1\n    }\n  }\n}", "LDL1601"},
		{"invalid traversal range", "query bad \"Bad\" {\n  select {}\n  traverse outgoing 3..1 error\n}", "LDL1601"},
		{"traversal arity", "query bad \"Bad\" {\n  select {}\n  traverse outgoing 0..1\n}", "LDL1601"},
		{"traversal widens selection", "query bad \"Bad\" {\n  select {\n    relation_types [r]\n  }\n  traverse outgoing 0..1 visit_once relations [s]\n}", "LDL1601"},
		{"duplicate selector", "query bad \"Bad\" {\n  select {\n    layers [app]\n    layers [app]\n  }\n}\n", "LDL1601"},
		{"duplicate result", "query bad \"Bad\" {\n  select {}\n  result [seed_entities, seed_entities]\n}", "LDL1601"},
		{"unknown query member", "query bad \"Bad\" {\n  select {}\n  cypher \"MATCH (n)\"\n}", "LDL1102"},
		{"missing select", "query bad \"Bad\" {\n  result []\n}", "LDL1601"},
		{"duplicate query member", "query bad \"Bad\" {\n  select {}\n  result []\n  result []\n}", "LDL1102"},
		{"row predicate arity", "query bad \"Bad\" {\n  select {}\n  where all {\n    rows any types [a] {}\n  }\n}", "LDL1601"},
		{"unknown root", "query bad \"Bad\" {\n  select {\n    roots [missing]\n  }\n}", "LDL1301"},
		{"invalid state policy", "query bad \"Bad\" {\n  state_input sometimes\n  select {}\n}", "LDL1601"},
		{"state policy block", "query bad \"Bad\" {\n  state_input {}\n  select {}\n}", "LDL1601"},
		{"selector scalar", "query bad \"Bad\" {\n  select {\n    layers app\n  }\n}", "LDL1601"},
		{"selector unknown member", "query bad \"Bad\" {\n  select {\n    entities [root]\n  }\n}", "LDL1601"},
		{"selector duplicate value", "query bad \"Bad\" {\n  select {\n    layers [app, app]\n  }\n}", "LDL1601"},
		{"invalid traversal direction", "query bad \"Bad\" {\n  select {}\n  traverse sideways 0..1 visit_once\n}", "LDL1601"},
		{"unbounded traversal", "query bad \"Bad\" {\n  select {}\n  traverse outgoing 0..* visit_once\n}", "LDL1601"},
		{"invalid cycle policy", "query bad \"Bad\" {\n  select {}\n  traverse outgoing 0..1 repeat\n}", "LDL1601"},
		{"invalid traversal restriction", "query bad \"Bad\" {\n  select {}\n  traverse outgoing 0..1 visit_once types [r]\n}", "LDL1601"},
		{"result scalar", "query bad \"Bad\" {\n  select {}\n  result seed_entities\n}", "LDL1601"},
		{"invalid result member", "query bad \"Bad\" {\n  select {}\n  result [seed_entities, backend_rows]\n}", "LDL1601"},
		{"predicate root missing group", "query bad \"Bad\" {\n  select {}\n  where {}\n}", "LDL1601"},
		{"predicate root invalid group", "query bad \"Bad\" {\n  select {}\n  where field {}\n}", "LDL1601"},
		{"boolean group argument", "query bad \"Bad\" {\n  select {}\n  where all {\n    all extra {}\n  }\n}", "LDL1601"},
		{"not arity", "query bad \"Bad\" {\n  select {}\n  where all {\n    not {\n      field id exists\n      field id missing\n    }\n  }\n}", "LDL1601"},
		{"field missing operator", "query bad \"Bad\" {\n  select {}\n  where all {\n    field id\n  }\n}", "LDL1601"},
		{"field wrong context", "query bad \"Bad\" {\n  select {}\n  relation_where all {\n    field layer exists\n  }\n}", "LDL1601"},
		{"exists with value", "query bad \"Bad\" {\n  select {}\n  where all {\n    field id exists \"x\"\n  }\n}", "LDL1601"},
		{"missing predicate value", "query bad \"Bad\" {\n  select {}\n  where all {\n    field id ==\n  }\n}", "LDL1601"},
		{"extra predicate value", "query bad \"Bad\" {\n  select {}\n  where all {\n    field id == \"x\" \"y\"\n  }\n}", "LDL1601"},
		{"parameter membership", "query bad \"Bad\" {\n  parameters {\n    name string\n  }\n  select {}\n  where all {\n    field id in $name\n  }\n}", "LDL1601"},
		{"address scalar list", "query bad \"Bad\" {\n  select {}\n  where all {\n    field address == [root]\n  }\n}", "LDL1601"},
		{"address membership scalar", "query bad \"Bad\" {\n  select {}\n  where all {\n    field address in root\n  }\n}", "LDL1601"},
		{"tag equality scalar", "query bad \"Bad\" {\n  select {}\n  where all {\n    field tags == critical\n  }\n}", "LDL1601"},
		{"tag contains list", "query bad \"Bad\" {\n  select {}\n  where all {\n    field tags contains [critical]\n  }\n}", "LDL1601"},
		{"tag contains invalid", "query bad \"Bad\" {\n  select {}\n  where all {\n    field tags contains 1\n  }\n}", "LDL1601"},
		{"duplicate tag literal", "query bad \"Bad\" {\n  select {}\n  where all {\n    field tags == [critical, critical]\n  }\n}", "LDL1601"},
		{"scalar membership scalar", "query bad \"Bad\" {\n  select {}\n  where all {\n    field id in \"root\"\n  }\n}", "LDL1601"},
		{"scalar equality list", "query bad \"Bad\" {\n  select {}\n  where all {\n    field id == [\"root\"]\n  }\n}", "LDL1601"},
		{"scalar invalid and duplicate list", "query bad \"Bad\" {\n  select {}\n  where all {\n    rows any types [a] {\n      cell shared in [1, \"bad\", 1]\n    }\n  }\n}", "LDL1601"},
		{"state string ordering", "query bad \"Bad\" {\n  state_input optional\n  select {}\n  where all {\n    state system.created_revision < \"r2\"\n  }\n}", "LDL1601"},
		{"row quantifier", "query bad \"Bad\" {\n  select {}\n  where all {\n    rows some types [a] {\n      cell shared exists\n    }\n  }\n}", "LDL1601"},
		{"row header", "query bad \"Bad\" {\n  select {}\n  where all {\n    rows any owners [a] {\n      cell shared exists\n    }\n  }\n}", "LDL1301"},
		{"row multiple children", "query bad \"Bad\" {\n  select {}\n  where all {\n    rows any types [a] {\n      cell shared exists\n      cell shared missing\n    }\n  }\n}", "LDL1601"},
		{"row boolean argument", "query bad \"Bad\" {\n  select {}\n  where all {\n    rows any types [a] {\n      all extra {}\n    }\n  }\n}", "LDL1601"},
		{"row not arity", "query bad \"Bad\" {\n  select {}\n  where all {\n    rows any types [a] {\n      not {}\n    }\n  }\n}", "LDL1601"},
		{"row unknown predicate", "query bad \"Bad\" {\n  select {}\n  where all {\n    rows any types [a] {\n      field id exists\n    }\n  }\n}", "LDL1601"},
		{"cell missing operator", "query bad \"Bad\" {\n  select {}\n  where all {\n    rows any types [a] {\n      cell shared\n    }\n  }\n}", "LDL1601"},
		{"cell incompatible operator", "query bad \"Bad\" {\n  select {}\n  where all {\n    rows any types [a] {\n      cell shared contains 1\n    }\n  }\n}", "LDL1601"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got := compileProject(t, map[string]string{"document.ldl": invalidSchema + "\n" + test.query + "\n"})
			if !got.HasErrors || got.Recipes != nil {
				t.Fatalf("expected transactional failure, got recipes=%+v diagnostics=%+v", got.Recipes, got.Diagnostics)
			}
			if !diagnosticCode(got, test.code) {
				t.Fatalf("missing %s in %+v", test.code, got.Diagnostics)
			}
		})
	}

	got := compileProject(t, map[string]string{"document.ldl": invalidSchema + `
query good "Good" {
  select {}
}
query bad "Bad" {
  select {}
  where all {
    field display_name < "x"
  }
}
`})
	if !got.HasErrors || got.Recipes != nil {
		t.Fatalf("one invalid Query leaked partial output: %+v", got)
	}
}

func TestExplicitRootMustExistInSuppliedCompiledGraph(t *testing.T) {
	input := projectInput(t, map[string]string{"document.ldl": invalidSchema + `
query rooted "Rooted" {
  select {
    roots [root]
  }
}
`})
	copyGraph := *input.Graph.Graph
	copyGraph.Entities = nil
	input.Graph.Graph = &copyGraph
	got := Compile(input)
	if !got.HasErrors || got.Recipes != nil || !diagnosticCode(got, "LDL1601") {
		t.Fatalf("unresolved compiled root result = %+v", got)
	}

	input = projectInput(t, map[string]string{"document.ldl": minimalSchema + "query q \"Q\" {\n  select {}\n}\n"})
	input.Graph.Graph = nil
	got = Compile(input)
	if !got.HasErrors || got.Recipes != nil {
		t.Fatalf("missing typed graph was accepted: %+v", got)
	}
}

func TestQueryDiagnosticsUseExactAuthoredRanges(t *testing.T) {
	source := invalidSchema + `
query bad "Bad" {
  select {}
  where all {
    field display_name matches "x"
  }
  result [seed_entities, seed_entities]
}
`
	got := compileProject(t, map[string]string{"document.ldl": source})
	if !got.HasErrors || got.Recipes != nil {
		t.Fatalf("invalid Query was not rejected transactionally: %+v", got)
	}
	sawOperator, sawDuplicate := false, false
	for _, diagnostic := range got.Diagnostics {
		switch diagnostic.Message {
		case "operator is incompatible with field type":
			sawOperator = true
			if diagnostic.Range == nil || source[diagnostic.Range.StartByte:diagnostic.Range.EndByte] != "matches" {
				t.Fatalf("operator range = %+v", diagnostic.Range)
			}
		case "duplicate query result member":
			sawDuplicate = true
			if diagnostic.Range == nil || source[diagnostic.Range.StartByte:diagnostic.Range.EndByte] != "seed_entities" || len(diagnostic.Related) != 1 || diagnostic.Related[0].Range == nil {
				t.Fatalf("duplicate result diagnostic = %+v", diagnostic)
			}
			first := strings.Index(source, "seed_entities")
			if diagnostic.Related[0].Range.StartByte != first || diagnostic.Range.StartByte == first {
				t.Fatalf("duplicate/previous ranges = %+v", diagnostic)
			}
		}
	}
	if !sawOperator || !sawDuplicate {
		t.Fatalf("missing range diagnostics: %+v", got.Diagnostics)
	}
}

func TestDeterministicAcrossDeclarationAndSelectorPermutations(t *testing.T) {
	prefixA := `
project p "Project" {}
layers {
  z "Z" @1
  a "A" @0
}
entity_type z_type "Z" {
  representation shape rect
}
entity_type a_type "A" {
  representation shape rect
}
`
	prefixB := `
project p "Project" {}
entity_type a_type "A" {
  representation shape rect
}
entity_type z_type "Z" {
  representation shape rect
}
layers {
  a "A" @0
  z "Z" @1
}
`
	queryA := "query q \"Q\" {\n  parameters {\n    z string\n    a string\n  }\n  select {\n    layers [z, a]\n    entity_types [z_type, a_type]\n  }\n  where all {\n    field id == $a\n    field display_name == $z\n  }\n}\n"
	queryB := "query q \"Q\" {\n  parameters {\n    a string\n    z string\n  }\n  select {\n    layers [a, z]\n    entity_types [a_type, z_type]\n  }\n  where all {\n    field id == $a\n    field display_name == $z\n  }\n}\n"
	a := compileProject(t, map[string]string{"document.ldl": prefixA + queryA})
	b := compileProject(t, map[string]string{"document.ldl": prefixB + queryB})
	if a.HasErrors || b.HasErrors || !reflect.DeepEqual(a.Recipes, b.Recipes) {
		t.Fatalf("permutation mismatch\na=%+v diag=%+v\nb=%+v diag=%+v", a.Recipes, a.Diagnostics, b.Recipes, b.Diagnostics)
	}
}

func TestCompileConcurrentRaceSafety(t *testing.T) {
	source := minimalSchema + "query q \"Q\" {\n  select {}\n}\n"
	input := projectInput(t, map[string]string{"document.ldl": source})
	want := Compile(input)
	if want.HasErrors {
		t.Fatalf("baseline diagnostics=%+v", want.Diagnostics)
	}
	var wait sync.WaitGroup
	errors := make(chan string, 32)
	for i := 0; i < 32; i++ {
		wait.Add(1)
		go func() {
			defer wait.Done()
			for iteration := 0; iteration < 25; iteration++ {
				got := Compile(input)
				if !reflect.DeepEqual(got, want) {
					errors <- fmt.Sprintf("got=%+v want=%+v", got, want)
					return
				}
			}
		}()
	}
	wait.Wait()
	close(errors)
	for err := range errors {
		t.Error(err)
	}
}

func TestSameSourceSeparatelyCompiledHybridIsRejected(t *testing.T) {
	source := minimalSchema + "query q \"Q\" {\n  select {}\n}\n"
	first := projectInput(t, map[string]string{"document.ldl": source})
	second := projectInput(t, map[string]string{"document.ldl": source})

	definitionHybrid := Input{Resolve: first.Resolve, Definition: second.Definition, Graph: first.Graph}
	got := Compile(definitionHybrid)
	if !got.HasErrors || got.Recipes != nil || !diagnosticCode(got, "LDL1801") || got.MatchesResolve(first.Resolve) {
		t.Fatalf("same-source Definition hybrid was accepted: %+v", got)
	}
	graphHybrid := Input{Resolve: first.Resolve, Definition: first.Definition, Graph: second.Graph}
	got = Compile(graphHybrid)
	if !got.HasErrors || got.Recipes != nil || !diagnosticCode(got, "LDL1801") || got.MatchesResolve(first.Resolve) {
		t.Fatalf("same-source Graph hybrid was accepted: %+v", got)
	}
}

func collectPredicateOperators(predicate Predicate, seen map[Operator]bool) {
	if predicate.Operator != "" {
		seen[predicate.Operator] = true
	}
	for _, child := range predicate.Children {
		collectPredicateOperators(child, seen)
	}
	if predicate.Child != nil {
		collectPredicateOperators(*predicate.Child, seen)
	}
	if predicate.Row != nil {
		collectRowOperators(*predicate.Row, seen)
	}
}

func collectRowOperators(predicate RowPredicate, seen map[Operator]bool) {
	if predicate.Operator != "" {
		seen[predicate.Operator] = true
	}
	for _, child := range predicate.Children {
		collectRowOperators(child, seen)
	}
	if predicate.Child != nil {
		collectRowOperators(*predicate.Child, seen)
	}
}

func containsRowKind(predicate Predicate, kind PredicateKind) bool {
	if predicate.Row != nil && containsNestedRowKind(*predicate.Row, kind) {
		return true
	}
	for _, child := range predicate.Children {
		if containsRowKind(child, kind) {
			return true
		}
	}
	if predicate.Child != nil {
		return containsRowKind(*predicate.Child, kind)
	}
	return false
}

func containsNestedRowKind(predicate RowPredicate, kind PredicateKind) bool {
	if predicate.Kind == kind {
		return true
	}
	for _, child := range predicate.Children {
		if containsNestedRowKind(child, kind) {
			return true
		}
	}
	return predicate.Child != nil && containsNestedRowKind(*predicate.Child, kind)
}

func diagnosticCode(result Result, code string) bool {
	for _, diagnostic := range result.Diagnostics {
		if diagnostic.Code == code {
			return true
		}
	}
	return false
}

const minimalSchema = `
project p "Project" {}
layers {
  app "App" @0
}
entity_type a "A" {
  representation shape rect
}
relation_type r "R" reference {
  from source types [a] layers [app]
  to target types [a] layers [app]
  label "r"
}
`

const invalidSchema = `
project p "Project" {}
layers {
  app "App" @0
}
entity_type a "A" {
  representation shape rect
  columns {
    shared "Shared" integer
  }
}
entity_type b "B" {
  representation shape rect
  columns {
    shared "Shared" string
  }
}
relation_type r "R" reference {
  from source types [a, b] layers [app]
  to target types [a, b] layers [app]
  label "r"
}
relation_type s "S" reference {
  from source types [a, b] layers [app]
  to target types [a, b] layers [app]
  label "s"
}
entities a @app {
  root "Root"
}
`
