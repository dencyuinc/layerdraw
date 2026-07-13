// SPDX-License-Identifier: LicenseRef-LayerDraw-1.0

package graph

import (
	"fmt"
	"reflect"
	"strings"
	"sync"
	"testing"

	"github.com/dencyuinc/layerdraw/internal/engine/internal/compiler/resolve"
)

func TestIssue14UnselectedPrivateFactGroupsDoNotReachGraphValidation(t *testing.T) {
	got := compileFiles(t, map[string]string{
		"document.ldl": `import { record } from "./schema.ldl"
project p "P" {}`,
		"schema.ldl": `layers {
  app "App" @0
}
entity_type record "Record" {
  representation table
  columns {
    value "Value" string
  }
}
entities record @app {
  hidden "Hidden"
}
rows record [missing, missing] {
  hidden private: "one", "two"
}
rows record [also_missing] {}
export { record }`,
	})
	if got.HasErrors || got.Graph == nil || len(got.Graph.Entities) != 0 || len(got.Graph.Relations) != 0 {
		t.Fatalf("unselected private groups reached graph validation: %+v", got)
	}
}

func TestIssue14EntryEmptyFactGroupIsValidatedAtExactHeaderRanges(t *testing.T) {
	source := `project p "P" {}
entity_type record "Record" {
  representation table
  columns {
    value "Value" string
  }
}
rows record [missing, missing] {}`
	got := compileFiles(t, map[string]string{"document.ldl": source})
	if !got.HasErrors || got.Graph != nil || countCode(got, "LDL1402") != 2 {
		t.Fatalf("entry empty group diagnostics = %+v", got.Diagnostics)
	}
	wantStarts := []int{strings.Index(source, "missing"), strings.LastIndex(source, "missing")}
	gotStarts := make([]int, 0, 2)
	for _, diagnostic := range got.Diagnostics {
		if diagnostic.Code != "LDL1402" {
			continue
		}
		if diagnostic.Range == nil || diagnostic.Range.ModulePath != "document.ldl" || diagnostic.Range.EndByte != diagnostic.Range.StartByte+len("missing") ||
			diagnostic.SubjectAddress != "ldl:project:p:entity-type:record" {
			t.Fatalf("entry empty group diagnostic = %+v", diagnostic)
		}
		gotStarts = append(gotStarts, diagnostic.Range.StartByte)
	}
	if !reflect.DeepEqual(gotStarts, wantStarts) {
		t.Fatalf("entry empty group ranges = %v, want %v", gotStarts, wantStarts)
	}
}

func TestIssue14RejectedMixedGenerationStillReportsMismatch(t *testing.T) {
	old := inputFiles(t, map[string]string{"document.ldl": deterministicDocument(false)})
	rejected := inputFiles(t, map[string]string{"document.ldl": `project p "P" {}
rows missing_type [value] {}`})
	if !rejected.Resolve.HasErrors || rejected.Definition.MatchesResolve(old.Resolve) {
		t.Fatalf("invalid mixed-generation fixture: resolve=%+v definition=%+v", rejected.Resolve, rejected.Definition)
	}

	got := Compile(Input{Resolve: rejected.Resolve, Definition: old.Definition})
	mismatchCount := 0
	for _, diagnostic := range got.Diagnostics {
		if diagnostic.Message == "definition result does not match resolve result" {
			mismatchCount++
		}
	}
	if !got.HasErrors || got.Graph != nil || mismatchCount != 1 || got.MatchesResolve(rejected.Resolve) || !got.MatchesResolve(old.Resolve) {
		t.Fatalf("rejected mixed-generation result = %+v", got)
	}
}

func TestIssue14RejectedUpstreamDiagnosticsAreDeepClonedForConcurrentCallers(t *testing.T) {
	input := inputFiles(t, map[string]string{"document.ldl": deterministicDocument(false)})
	related := []resolve.DiagnosticRelated{
		{Relation: "cause", Message: "third", Range: &resolve.SourceRange{Origin: resolve.SourceOrigin{Kind: resolve.OriginProject}, ModulePath: "b.ldl", StartByte: 20, EndByte: 21}},
		{Relation: "cause", Message: "second", Range: &resolve.SourceRange{Origin: resolve.SourceOrigin{Kind: resolve.OriginProject}, ModulePath: "a.ldl", StartByte: 10, EndByte: 11}},
		{Relation: "cause", Message: "first", Range: &resolve.SourceRange{Origin: resolve.SourceOrigin{Kind: resolve.OriginProject}, ModulePath: "a.ldl", StartByte: 5, EndByte: 6}},
	}
	original := append([]resolve.DiagnosticRelated{}, related...)
	want := []resolve.DiagnosticRelated{related[2], related[1], related[0]}
	input.Definition.Diagnostics = []resolve.Diagnostic{{
		Code:       "LDL1102",
		Severity:   "error",
		MessageKey: "concurrent_rejection",
		Arguments:  map[string]string{},
		Message:    "concurrent rejection",
		Related:    related,
	}}
	input.Definition.HasErrors = true

	const callers = 32
	start := make(chan struct{})
	errs := make(chan string, callers)
	var wg sync.WaitGroup
	for range callers {
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-start
			got := Compile(input)
			if !got.HasErrors || got.Graph != nil || len(got.Diagnostics) != 1 || !reflect.DeepEqual(got.Diagnostics[0].Related, want) {
				errs <- fmt.Sprintf("result=%+v", got)
			}
		}()
	}
	close(start)
	wg.Wait()
	close(errs)
	for err := range errs {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(input.Definition.Diagnostics[0].Related, original) {
		t.Fatalf("Compile mutated upstream related diagnostics: got=%+v want=%+v", input.Definition.Diagnostics[0].Related, original)
	}
}

func TestIssue14ForbiddenSelfRelationUsesRepeatedEndpointTokenRange(t *testing.T) {
	source := duplicateDocument("allow_self false\nduplicate_policy allow", "self: a -> a")
	got := compileFiles(t, map[string]string{"document.ldl": source})
	if countCode(got, "LDL1501") != 1 {
		t.Fatalf("self relation diagnostics = %+v", got.Diagnostics)
	}
	start := strings.Index(source, "self: a -> a") + len("self: a -> ")
	for _, diagnostic := range got.Diagnostics {
		if diagnostic.Code != "LDL1501" {
			continue
		}
		if diagnostic.Range == nil || diagnostic.Range.StartByte != start || diagnostic.Range.EndByte != start+1 ||
			diagnostic.SubjectAddress != "ldl:project:p:relation:self" || diagnostic.OwnerAddress != "" {
			t.Fatalf("self relation diagnostic = %+v, want repeated endpoint [%d,%d)", diagnostic, start, start+1)
		}
	}
}
