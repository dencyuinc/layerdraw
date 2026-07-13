// SPDX-License-Identifier: LicenseRef-LayerDraw-1.0

package resolve

import (
	"testing"

	"github.com/dencyuinc/layerdraw/internal/engine/internal/compiler/syntax"
)

func TestQueryBodyBindingsArePreciseAndOwnerAware(t *testing.T) {
	t.Parallel()
	source := `project p "P" {}
layers {
  app "App" @0
}
entity_type a "A" {
  representation shape rect
  columns {
    environment "Environment" enum [prod, stg]
  }
}
entity_type b "B" {
  representation shape rect
  columns {
    environment "Environment" enum [prod, stg]
  }
}
relation_type r "R" reference {
  from source types [a, b] layers [app]
  to target types [a, b] layers [app]
  label "r"
  columns {
    environment "Environment" enum [prod, stg]
  }
}
entities a @app {
  root "Root"
}
relations r {
  edge: root -> root
}
query q "Q" {
  parameters {
    env enum [prod, stg]
  }
  select {
    layers [app]
    entity_types [a, b]
    relation_types [r]
    roots [root]
  }
  where all {
    field address == root
    field type == a
    field layer == app
    field display_name == $env
    any {
      not {
        field type in [a]
      }
    }
    rows any types [a, b] {
      cell environment == $env
    }
  }
  relation_where all {
    field address == edge
    field type == r
    field from == root
    field to in [root]
    rows all types [r] {
      not {
        cell environment missing
      }
    }
  }
  traverse outgoing 0..1 visit_once relations [r]
}
`
	got := Resolve(Input{Mode: CompileProject, EntryPath: "document.ldl", Project: ProjectInput{Files: map[string]SourceFile{"document.ldl": parse(source)}}})
	if got.HasErrors {
		t.Fatalf("diagnostics=%+v", got.Diagnostics)
	}
	queryAddress := "ldl:project:p:query:q"
	columnOwners := map[string]bool{}
	seenVia := map[string]bool{}
	parameterFound := false
	for _, binding := range got.Bindings {
		if binding.SourceAddress != queryAddress {
			continue
		}
		if binding.Range.Start < 0 || binding.Range.End > len(source) || binding.Range.Start >= binding.Range.End {
			t.Fatalf("invalid precise range: %+v", binding)
		}
		seenVia[binding.Via] = true
		raw := source[binding.Range.Start:binding.Range.End]
		switch binding.ExpectedKind {
		case KindColumn:
			if raw != "environment" || binding.TargetOwnerAddress == "" {
				t.Fatalf("column binding=%+v raw=%q", binding, raw)
			}
			columnOwners[binding.TargetOwnerAddress] = true
		case KindParameter:
			if raw != "$env" || binding.TargetOwnerAddress != queryAddress {
				t.Fatalf("parameter binding=%+v raw=%q", binding, raw)
			}
			parameterFound = true
		}
	}
	if len(columnOwners) != 3 || !columnOwners["ldl:project:p:entity-type:a"] || !columnOwners["ldl:project:p:entity-type:b"] || !columnOwners["ldl:project:p:relation-type:r"] || !parameterFound {
		t.Fatalf("owner-aware bindings columns=%+v parameter=%v bindings=%+v", columnOwners, parameterFound, got.Bindings)
	}
	for _, via := range []string{"query:select.layers", "query:select.entity_types", "query:select.relation_types", "query:select.roots", "query:predicate.field.address", "query:predicate.field.type", "query:predicate.field.layer", "query:predicate.field.from", "query:predicate.field.to", "query:rows.types", "query:row.cell", "query:parameter", "query:traverse.relation_types"} {
		if !seenVia[via] {
			t.Errorf("missing %s binding; seen=%+v", via, seenVia)
		}
	}
}

func TestQueryUnknownBodyReferencesUseExactRanges(t *testing.T) {
	t.Parallel()
	source := `project p "P" {}
query q "Q" {
  select {
    roots [missing]
  }
  where all {
    field display_name == $unknown
  }
}
`
	got := Resolve(Input{Mode: CompileProject, EntryPath: "document.ldl", Project: ProjectInput{Files: map[string]SourceFile{"document.ldl": parse(source)}}})
	if !got.HasErrors {
		t.Fatal("expected unresolved Query body diagnostics")
	}
	ranges := map[string]bool{}
	for _, diagnostic := range got.Diagnostics {
		if diagnostic.Code != "LDL1301" || diagnostic.Range == nil {
			continue
		}
		ranges[source[diagnostic.Range.StartByte:diagnostic.Range.EndByte]] = true
	}
	if !ranges["missing"] || !ranges["$unknown"] {
		t.Fatalf("diagnostic ranges=%+v diagnostics=%+v", ranges, got.Diagnostics)
	}
}

func TestQueryReferencesFollowEffectiveDocumentSelection(t *testing.T) {
	t.Run("private query is ignored", func(t *testing.T) {
		got := Resolve(Input{Mode: CompileProject, EntryPath: "document.ldl", Project: ProjectInput{Files: map[string]SourceFile{
			"document.ldl": parse(`import { record } from "./schema.ldl"
project p "P" {}`),
			"schema.ldl": parse(`entity_type record "Record" {
  representation shape rect
}
query private_query "Private" {
  select {
    roots [missing]
  }
}
export { record }`),
		}}})
		if got.HasErrors || len(got.Diagnostics) != 0 {
			t.Fatalf("private query references rejected the document: %+v", got.Diagnostics)
		}
		if hasAddress(got, "ldl:project:p:query:private_query") {
			t.Fatalf("private query entered selected declarations: %v", addresses(got))
		}
	})

	t.Run("selected query validates and closes over bindings", func(t *testing.T) {
		got := Resolve(Input{Mode: CompileProject, EntryPath: "document.ldl", Project: ProjectInput{Files: map[string]SourceFile{
			"document.ldl": parse(`import { selected_query } from "./schema.ldl"
project p "P" {}`),
			"schema.ldl": parse(`entity_type record "Record" {
  representation shape rect
}
query selected_query "Selected" {
  select {
    entity_types [record]
  }
}
export { selected_query }`),
		}}})
		if got.HasErrors {
			t.Fatalf("diagnostics=%+v", got.Diagnostics)
		}
		if !hasAddress(got, "ldl:project:p:query:selected_query") || !hasAddress(got, "ldl:project:p:entity-type:record") {
			t.Fatalf("query binding closure is incomplete: %v", addresses(got))
		}
	})
}

func TestQueryReferenceHelpersPreserveMalformedAbsence(t *testing.T) {
	t.Parallel()
	source := `project p "P" {}
query q "Q" {
  select {
    future [missing]
    layers app
  }
  where all {
    field address
  }
}
`
	got := Resolve(Input{Mode: CompileProject, EntryPath: "document.ldl", Project: ProjectInput{Files: map[string]SourceFile{"document.ldl": parse(source)}}})
	if got.HasErrors {
		t.Fatalf("resolver diagnosed Query shapes owned by semantic compilation: %+v", got.Diagnostics)
	}
	if queryHead(&syntax.Node{}) != "" || queryPredicateLiteralValues(nil) != nil || queryListValues(&syntax.Node{}) != nil {
		t.Fatal("malformed Query CST helpers invented values")
	}
	list := &syntax.Node{Kind: syntax.NodeList, Children: []syntax.Element{&syntax.Node{Kind: syntax.NodeStatement}}}
	value := &syntax.Node{Kind: syntax.NodeValue, Children: []syntax.Element{list}}
	if values := queryListValues(value); len(values) != 0 {
		t.Fatalf("non-value list children became references: %+v", values)
	}
}
