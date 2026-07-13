// SPDX-License-Identifier: LicenseRef-LayerDraw-1.0

package graph

import (
	"reflect"
	"sync"
	"testing"

	"github.com/dencyuinc/layerdraw/internal/engine/internal/compiler/resolve"
)

func TestUpstreamAndGraphErrorsNeverPublishPartialGraph(t *testing.T) {
	upstream := compileFiles(t, map[string]string{"document.ldl": `project p "P" {
`})
	if !upstream.HasErrors || upstream.Graph != nil || len(upstream.Diagnostics) == 0 {
		t.Fatalf("upstream transactional result = %+v", upstream)
	}

	input := inputFiles(t, map[string]string{"document.ldl": deterministicDocument(false)})
	input.Definition.Root.Address = "ldl:project:other"
	mismatch := Compile(input)
	requireFailureCode(t, mismatch, "LDL1301")
}

func TestGraphOrderingAndDiagnosticsAreDeterministic(t *testing.T) {
	a := compileFiles(t, map[string]string{"document.ldl": deterministicDocument(false)})
	b := compileFiles(t, map[string]string{"document.ldl": deterministicDocument(true)})
	if a.HasErrors || b.HasErrors || !reflect.DeepEqual(a.Graph, b.Graph) {
		t.Fatalf("permuted graph differs\nA=%+v diagnostics=%+v\nB=%+v diagnostics=%+v", a.Graph, a.Diagnostics, b.Graph, b.Diagnostics)
	}

	invalidSource := duplicateDocument("allow_self false\nduplicate_policy deny_same_type_between_same_endpoints", "one: a -> b\ntwo: a -> b")
	first := compileFiles(t, map[string]string{"document.ldl": invalidSource})
	second := compileFiles(t, map[string]string{"document.ldl": invalidSource})
	if !reflect.DeepEqual(first.Diagnostics, second.Diagnostics) {
		t.Fatalf("diagnostics are not stable\nfirst=%+v\nsecond=%+v", first.Diagnostics, second.Diagnostics)
	}
	for i := 1; i < len(first.Diagnostics); i++ {
		ordered := append([]resolve.Diagnostic{}, first.Diagnostics[i], first.Diagnostics[i-1])
		resolve.SortDiagnostics(ordered)
		if !reflect.DeepEqual(ordered[0], first.Diagnostics[i-1]) {
			t.Fatalf("diagnostics not sorted: %+v", first.Diagnostics)
		}
	}
}

func TestCompileIsRaceSafeForConcurrentCallers(t *testing.T) {
	input := inputFiles(t, map[string]string{"document.ldl": deterministicDocument(false)})
	want := Compile(input)
	if want.HasErrors {
		t.Fatalf("fixture diagnostics = %+v", want.Diagnostics)
	}
	var wg sync.WaitGroup
	errs := make(chan Result, 24)
	for range 24 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			got := Compile(input)
			if !reflect.DeepEqual(got, want) {
				errs <- got
			}
		}()
	}
	wg.Wait()
	close(errs)
	for got := range errs {
		t.Fatalf("concurrent Compile() = %+v, want %+v", got, want)
	}
}

func deterministicDocument(reverse bool) string {
	entities := `
  beta "Beta"
  alpha "Alpha"`
	relations := `
  zed: beta -> alpha
  first: alpha -> beta`
	rows := `
  beta main: "b"
  alpha main: "a"`
	if reverse {
		entities = `
  alpha "Alpha"
  beta "Beta"`
		relations = `
  first: alpha -> beta
  zed: beta -> alpha`
		rows = `
  alpha main: "a"
  beta main: "b"`
	}
	return `
project p "P" {}
layers {
  app "App" @0
}
entity_type node "Node" {
  representation table
  columns {
    value "Value" string
  }
}
relation_type edge "Edge" reference {
  duplicate_policy allow
  from source types [node]
  to target types [node]
  label "edge"
}
entities node @app {` + entities + `
}
rows node [value] {` + rows + `
}
relations edge {` + relations + `
}
`
}
