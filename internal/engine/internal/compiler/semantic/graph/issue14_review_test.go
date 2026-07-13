// SPDX-License-Identifier: LicenseRef-LayerDraw-1.0

package graph

import (
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"strings"
	"sync"
	"testing"
)

func TestIssue14InvalidSourceGolden(t *testing.T) {
	source, err := os.ReadFile(filepath.Join("testdata", "invalid_graph.ldl"))
	if err != nil {
		t.Fatal(err)
	}
	want, err := os.ReadFile(filepath.Join("testdata", "invalid_graph.golden"))
	if err != nil {
		t.Fatal(err)
	}
	got := compileFiles(t, map[string]string{"document.ldl": string(source)})
	if !got.HasErrors || got.Graph != nil {
		t.Fatalf("invalid golden published a graph: %+v", got)
	}
	var actual strings.Builder
	for _, diagnostic := range got.Diagnostics {
		if len(diagnostic.Arguments) != 0 || diagnostic.Range == nil {
			t.Fatalf("invalid primary diagnostic contract: %+v", diagnostic)
		}
		fmt.Fprintf(&actual, "%s|%s|%d:%d|%s|%s|", diagnostic.Code, diagnostic.MessageKey, diagnostic.Range.StartByte, diagnostic.Range.EndByte, diagnostic.SubjectAddress, diagnostic.OwnerAddress)
		for i, related := range diagnostic.Related {
			if i > 0 {
				actual.WriteByte(',')
			}
			if related.Range == nil {
				actual.WriteString("-")
				continue
			}
			fmt.Fprintf(&actual, "%s@%d:%d@%s@%s", related.Relation, related.Range.StartByte, related.Range.EndByte, related.SubjectAddress, related.OwnerAddress)
		}
		actual.WriteByte('\n')
	}
	if actual.String() != string(want) {
		t.Fatalf("invalid diagnostic golden mismatch\nactual:\n%s\nwant:\n%s", actual.String(), string(want))
	}
}

func TestIssue14EmptyFactGroupsAreValidated(t *testing.T) {
	valid := compileFiles(t, map[string]string{"document.ldl": `project p "P" {}
layers {
  app "App" @0
}
entity_type node "Node" {
  representation table
  columns {
    name "Name" string
  }
}
relation_type edge "Edge" reference {
  from source types [node]
  to target types [node]
  label "edge"
  columns {
    weight "Weight" integer
  }
}
entities node @app {}
relations edge {}
rows node [name] {}
relation_rows edge [weight] {}
`})
	if valid.HasErrors || valid.Graph == nil || len(valid.Graph.Entities) != 0 || len(valid.Graph.Relations) != 0 {
		t.Fatalf("valid empty groups = %+v", valid)
	}

	invalidSource := `project p "P" {}
entity_type node "Node" {
  representation table
  columns {
    name "Name" string
  }
}
rows node [missing, missing] {}
`
	invalid := compileFiles(t, map[string]string{"document.ldl": invalidSource})
	if !invalid.HasErrors || invalid.Graph != nil || countCode(invalid, "LDL1402") != 2 {
		t.Fatalf("invalid empty row group = %+v", invalid)
	}
	first := strings.Index(invalidSource, "missing")
	second := strings.LastIndex(invalidSource, "missing")
	starts := []int{invalid.Diagnostics[0].Range.StartByte, invalid.Diagnostics[1].Range.StartByte}
	sort.Ints(starts)
	if !reflect.DeepEqual(starts, []int{first, second}) {
		t.Fatalf("header diagnostic starts = %v, want [%d %d]; diagnostics=%+v", starts, first, second, invalid.Diagnostics)
	}
	for _, diagnostic := range invalid.Diagnostics {
		if diagnostic.MessageKey != "invalid_or_duplicate_row" || len(diagnostic.Arguments) != 0 || diagnostic.SubjectAddress != "ldl:project:p:entity-type:node" {
			t.Fatalf("header diagnostic identity = %+v", diagnostic)
		}
	}
}

func TestIssue14EndpointRestrictionDiagnosticsUseExactEndpointRanges(t *testing.T) {
	source := `project p "P" {}
layers {
  app "App" @0
  data "Data" @1
}
entity_type source "Source" {
  representation shape rect
}
entity_type target "Target" {
  representation shape rect
}
relation_type sends "Sends" data_flow {
  from sender types [source] layers [app]
  to receiver types [target] layers [data]
  label "sends"
}
entities source @app {
  s "S"
}
entities target @app {
  t "T"
}
relations sends {
  wrong_layer: s -> t
  reversed: t -> s
}
`
	got := compileFiles(t, map[string]string{"document.ldl": source})
	if countCode(got, "LDL1501") != 3 {
		t.Fatalf("diagnostics = %+v", got.Diagnostics)
	}
	expected := map[string][]int{
		"ldl:project:p:relation:wrong_layer": {strings.Index(source, "wrong_layer: s -> t") + len("wrong_layer: s -> ")},
		"ldl:project:p:relation:reversed": {
			strings.Index(source, "reversed: t -> s") + len("reversed: "),
			strings.Index(source, "reversed: t -> s") + len("reversed: t -> "),
		},
	}
	actual := map[string][]int{}
	for _, diagnostic := range got.Diagnostics {
		if diagnostic.Code != "LDL1501" {
			continue
		}
		if len(diagnostic.Arguments) != 0 || diagnostic.OwnerAddress != "" || diagnostic.Range == nil || diagnostic.Range.EndByte != diagnostic.Range.StartByte+1 {
			t.Fatalf("endpoint diagnostic identity/range = %+v", diagnostic)
		}
		actual[diagnostic.SubjectAddress] = append(actual[diagnostic.SubjectAddress], diagnostic.Range.StartByte)
	}
	for address := range actual {
		sort.Ints(actual[address])
		sort.Ints(expected[address])
	}
	if !reflect.DeepEqual(actual, expected) {
		t.Fatalf("endpoint ranges = %v, want %v", actual, expected)
	}
}

func TestIssue14CardinalityIdentityAggregatesDirectionsPerEntityAndType(t *testing.T) {
	source := `project p "P" {}
layers {
  app "App" @0
}
entity_type node "Node" {
  representation shape rect
}
relation_type first "First" reference {
  from source types [node]
  to target types [node]
  cardinality {
    to_per_from 1..1
    from_per_to 1..1
  }
  label "first"
}
relation_type second "Second" reference {
  from source types [node]
  to target types [node]
  cardinality {
    to_per_from 1..1
    from_per_to 1..1
  }
  label "second"
}
entities node @app {
  only "Only"
}
relations first {}
relations second {}
`
	got := compileFiles(t, map[string]string{"document.ldl": source})
	if countCode(got, "LDL1503") != 2 {
		t.Fatalf("cardinality diagnostics = %+v", got.Diagnostics)
	}
	owners := map[string]bool{}
	boundRanges := map[string][]int{}
	firstTypeStart := strings.Index(source, `relation_type first`)
	firstToStart := firstTypeStart + strings.Index(source[firstTypeStart:], "1..1")
	firstFromStart := firstToStart + len("1..1") + strings.Index(source[firstToStart+len("1..1"):], "1..1")
	secondTypeStart := strings.Index(source, `relation_type second`)
	secondToStart := secondTypeStart + strings.Index(source[secondTypeStart:], "1..1")
	secondFromStart := secondToStart + len("1..1") + strings.Index(source[secondToStart+len("1..1"):], "1..1")
	wantBoundRanges := map[string][]int{
		"ldl:project:p:relation-type:first":  {firstToStart, firstFromStart},
		"ldl:project:p:relation-type:second": {secondToStart, secondFromStart},
	}
	entityStart := strings.Index(source, `only "Only"`)
	for _, diagnostic := range got.Diagnostics {
		if diagnostic.Code != "LDL1503" {
			continue
		}
		owners[diagnostic.OwnerAddress] = true
		if diagnostic.SubjectAddress != "ldl:project:p:entity:only" || len(diagnostic.Arguments) != 0 || diagnostic.Range == nil ||
			diagnostic.Range.StartByte != entityStart || diagnostic.Range.EndByte != entityStart+len("only") || len(diagnostic.Related) != 2 {
			t.Fatalf("cardinality diagnostic identity = %+v", diagnostic)
		}
		for _, related := range diagnostic.Related {
			if related.Relation != "cause" || related.SubjectAddress != diagnostic.SubjectAddress || related.OwnerAddress != diagnostic.OwnerAddress || related.Range == nil || related.Range.EndByte != related.Range.StartByte+len("1..1") {
				t.Fatalf("cardinality related identity = %+v", related)
			}
			boundRanges[diagnostic.OwnerAddress] = append(boundRanges[diagnostic.OwnerAddress], related.Range.StartByte)
		}
	}
	if !owners["ldl:project:p:relation-type:first"] || !owners["ldl:project:p:relation-type:second"] || len(owners) != 2 {
		t.Fatalf("cardinality owners = %v", owners)
	}
	for owner := range boundRanges {
		sort.Ints(boundRanges[owner])
		sort.Ints(wantBoundRanges[owner])
	}
	if !reflect.DeepEqual(boundRanges, wantBoundRanges) {
		t.Fatalf("cardinality bound ranges = %v, want %v", boundRanges, wantBoundRanges)
	}
}

func TestIssue14CardinalitySourcePropertyLoop(t *testing.T) {
	for min := 0; min <= 1; min++ {
		for _, maximum := range []struct {
			name  string
			token string
		}{{name: "one", token: "1"}, {name: "many", token: "*"}} {
			for count := 0; count <= 3; count++ {
				name := fmt.Sprintf("min_%d_max_%s_count_%d", min, maximum.name, count)
				t.Run(name, func(t *testing.T) {
					var rightItems, relations strings.Builder
					for i := 0; i < count; i++ {
						fmt.Fprintf(&rightItems, "  r%d \"R%d\"\n", i, i)
						fmt.Fprintf(&relations, "  edge%d: left -> r%d\n", i, i)
					}
					source := fmt.Sprintf(`project p "P" {}
layers {
  app "App" @0
}
entity_type left_type "Left" {
  representation shape rect
}
entity_type right_type "Right" {
  representation shape rect
}
relation_type edge "Edge" reference {
  from source types [left_type]
  to target types [right_type]
  cardinality {
    to_per_from %d..%s
    from_per_to 0..*
  }
  label "edge"
}
entities left_type @app {
  left "Left"
}
entities right_type @app {
%s}
relations edge {
%s}
`, min, maximum.token, rightItems.String(), relations.String())
					got := compileFiles(t, map[string]string{"document.ldl": source})
					wantFailure := count < min || maximum.token == "1" && count > 1
					if (countCode(got, "LDL1503") != 0) != wantFailure {
						t.Fatalf("count=%d bound=%d..%s diagnostics=%+v\nsource:\n%s", count, min, maximum.token, got.Diagnostics, source)
					}
				})
			}
		}
	}
}

func TestIssue14RowCellCountSourcePropertyLoop(t *testing.T) {
	columnIDs := []string{"first", "second", "third"}
	for headerCount := 1; headerCount <= len(columnIDs); headerCount++ {
		for cellCount := 1; cellCount <= len(columnIDs); cellCount++ {
			name := fmt.Sprintf("headers_%d_cells_%d", headerCount, cellCount)
			t.Run(name, func(t *testing.T) {
				cells := make([]string, cellCount)
				for i := range cells {
					cells[i] = fmt.Sprintf(`"v%d"`, i)
				}
				separator := ""
				if len(cells) > 0 {
					separator = " "
				}
				source := fmt.Sprintf(`project p "P" {}
layers {
  app "App" @0
}
entity_type record "Record" {
  representation table
  columns {
    first "First" string
    second "Second" string
    third "Third" string
  }
}
entities record @app {
  item "Item"
}
rows record [%s] {
  item row:%s%s
}
`, strings.Join(columnIDs[:headerCount], ", "), separator, strings.Join(cells, ", "))
				got := compileFiles(t, map[string]string{"document.ldl": source})
				wantFailure := headerCount != cellCount
				if (countCode(got, "LDL1402") != 0) != wantFailure {
					t.Fatalf("headers=%d cells=%d diagnostics=%+v", headerCount, cellCount, got.Diagnostics)
				}
			})
		}
	}
}

func TestIssue14SameRootStageGenerationsCannotBeMixed(t *testing.T) {
	first := inputFiles(t, map[string]string{"document.ldl": deterministicDocument(false)})
	second := inputFiles(t, map[string]string{"document.ldl": deterministicDocument(false)})
	if first.Resolve.Generation().Matches(second.Resolve.Generation()) {
		t.Fatal("separate Resolve invocations shared a generation token")
	}
	if got := Compile(first); got.HasErrors || got.Graph == nil || !got.MatchesResolve(first.Resolve) || got.MatchesResolve(second.Resolve) {
		t.Fatalf("same generation failed: %+v", got)
	}
	mixed := Compile(Input{Resolve: second.Resolve, Definition: first.Definition})
	if !mixed.HasErrors || mixed.Graph != nil || countCode(mixed, "LDL1301") != 1 || mixed.MatchesResolve(second.Resolve) || !mixed.MatchesResolve(first.Resolve) {
		t.Fatalf("same-root mixed generation = %+v", mixed)
	}

	rejected := inputFiles(t, map[string]string{"document.ldl": duplicateDocument("allow_self false\nduplicate_policy deny_same_type_between_same_endpoints", "one: a -> b\ntwo: a -> b")})
	rejectedResult := Compile(rejected)
	if !rejectedResult.HasErrors || !rejectedResult.MatchesResolve(rejected.Resolve) || rejectedResult.MatchesResolve(first.Resolve) {
		t.Fatalf("rejected graph generation contract = %+v", rejectedResult)
	}
}

func TestIssue14RejectedInputIsRaceSafeForConcurrentCallers(t *testing.T) {
	input := inputFiles(t, map[string]string{"document.ldl": duplicateDocument("allow_self false\nduplicate_policy deny_same_type_between_same_endpoints", "one: a -> b\ntwo: a -> b")})
	want := Compile(input)
	if !want.HasErrors || want.Graph != nil {
		t.Fatalf("fixture was not rejected: %+v", want)
	}
	var wg sync.WaitGroup
	errResults := make(chan Result, 24)
	for range 24 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if got := Compile(input); !reflect.DeepEqual(got, want) {
				errResults <- got
			}
		}()
	}
	wg.Wait()
	close(errResults)
	for got := range errResults {
		t.Fatalf("concurrent rejected Compile() = %+v, want %+v", got, want)
	}
}
