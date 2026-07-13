// SPDX-License-Identifier: LicenseRef-LayerDraw-1.0

package graph

import "testing"

func TestEndpointRestrictionsAndDirectionAreEnforced(t *testing.T) {
	got := compileFiles(t, map[string]string{"document.ldl": `
project p "P" {}
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
`})
	requireFailureCode(t, got, "LDL1501")
	if countCode(got, "LDL1501") != 3 {
		t.Fatalf("endpoint diagnostics = %+v", got.Diagnostics)
	}
}

func TestSelfPolicyAndOrderedDuplicatePolicies(t *testing.T) {
	tests := []struct {
		name        string
		policy      string
		relations   string
		wantCode    string
		wantCount   int
		wantSuccess bool
	}{
		{name: "self denied", policy: "allow_self false\nduplicate_policy allow", relations: "self: a -> a", wantCode: "LDL1501", wantCount: 1},
		{name: "self allowed", policy: "allow_self true\nduplicate_policy allow", relations: "self: a -> a", wantSuccess: true},
		{name: "same type duplicate denied", policy: "allow_self false\nduplicate_policy deny_same_type_between_same_endpoints", relations: "one: a -> b\ntwo: a -> b", wantCode: "LDL1502", wantCount: 1},
		{name: "reverse is not duplicate", policy: "allow_self false\nduplicate_policy deny_same_type_between_same_endpoints", relations: "one: a -> b\ntwo: b -> a", wantSuccess: true},
		{name: "duplicates allowed", policy: "allow_self false\nduplicate_policy allow", relations: "one: a -> b\ntwo: a -> b", wantSuccess: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := compileFiles(t, map[string]string{"document.ldl": duplicateDocument(tt.policy, tt.relations)})
			if tt.wantSuccess {
				if got.HasErrors || got.Graph == nil {
					t.Fatalf("Compile() diagnostics = %+v", got.Diagnostics)
				}
				return
			}
			requireFailureCode(t, got, tt.wantCode)
			if countCode(got, tt.wantCode) != tt.wantCount {
				t.Fatalf("diagnostics = %+v", got.Diagnostics)
			}
		})
	}
}

func TestDenyAnyDuplicatePolicyAppliesAcrossRelationTypes(t *testing.T) {
	got := compileFiles(t, map[string]string{"document.ldl": `
project p "P" {}
layers {
  app "App" @0
}
entity_type node "Node" {
  representation shape rect
}
relation_type permissive "Permissive" reference {
  allow_self false
  duplicate_policy allow
  from source types [node]
  to target types [node]
  label "references"
}
relation_type exclusive "Exclusive" dependency {
  allow_self false
  duplicate_policy deny_any_between_same_endpoints
  from source types [node]
  to target types [node]
  label "depends"
}
entities node @app {
  a "A"
  b "B"
}
relations exclusive {
  first: a -> b
}
relations permissive {
  second: a -> b
}
`})
	requireFailureCode(t, got, "LDL1502")
}

func TestBothCardinalityDirectionsUseCompleteEligibleGraph(t *testing.T) {
	got := compileFiles(t, map[string]string{"document.ldl": `
project p "P" {}
layers {
  app "App" @0
}
entity_type left "Left" {
  representation shape rect
}
entity_type right "Right" {
  representation shape rect
}
relation_type maps "Maps" reference {
  duplicate_policy allow
  from source types [left]
  to target types [right]
  cardinality {
    to_per_from 1..1
    from_per_to 1..1
  }
  label "maps"
}
entities left @app {
  l1 "L1"
  l2 "L2"
  l3 "L3"
}
entities right @app {
  r1 "R1"
  r2 "R2"
  r3 "R3"
}
relations maps {
  a: l1 -> r1
  duplicate_same_neighbor: l1 -> r1
  b: l1 -> r2
  c: l2 -> r1
}
`})
	requireFailureCode(t, got, "LDL1503")
	// l1 exceeds to_per_from, r1 exceeds from_per_to, and l3/r3 violate
	// the respective eligible-endpoint minima. The duplicate l1->r1 fact
	// counts as one neighboring Entity, not a second target.
	if countCode(got, "LDL1503") != 4 {
		t.Fatalf("cardinality diagnostics = %+v", got.Diagnostics)
	}
}

func duplicateDocument(policy, relations string) string {
	return `
project p "P" {}
layers {
  app "App" @0
}
entity_type node "Node" {
  representation shape rect
}
relation_type edge "Edge" reference {
  ` + policy + `
  from source types [node]
  to target types [node]
  label "edge"
}
entities node @app {
  a "A"
  b "B"
}
relations edge {
  ` + relations + `
}
`
}

func requireFailureCode(t *testing.T, got Result, code string) {
	t.Helper()
	if !got.HasErrors || got.Graph != nil || countCode(got, code) == 0 {
		t.Fatalf("Compile() = graph %+v, errors %v, diagnostics %+v; want transactional %s failure", got.Graph, got.HasErrors, got.Diagnostics, code)
	}
}

func countCode(got Result, code string) int {
	count := 0
	for _, diagnostic := range got.Diagnostics {
		if diagnostic.Code == code {
			count++
		}
	}
	return count
}
