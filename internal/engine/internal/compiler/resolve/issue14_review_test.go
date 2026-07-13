// SPDX-License-Identifier: LicenseRef-LayerDraw-1.0

package resolve

import (
	"strings"
	"testing"
)

func TestIssue14EmptyFactGroupHeadersResolveOrFailAtHeader(t *testing.T) {
	tests := []struct {
		name   string
		source string
		marker string
	}{
		{name: "entities", source: `project p "P" {}
layers {
  app "App" @0
}
entities missing_type @app {}
`, marker: "missing_type"},
		{name: "relations", source: `project p "P" {}
relations missing_type {}
`, marker: "missing_type"},
		{name: "rows", source: `project p "P" {}
rows missing_type [value] {}
`, marker: "missing_type"},
		{name: "relation rows", source: `project p "P" {}
relation_rows missing_type [value] {}
`, marker: "missing_type"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := Resolve(Input{EntryPath: "document.ldl", Project: ProjectInput{Files: map[string]SourceFile{"document.ldl": parse(tt.source)}}})
			if len(got.Diagnostics) != 1 {
				t.Fatalf("diagnostics = %+v, want one resolver diagnostic", got.Diagnostics)
			}
			diagnostic := got.Diagnostics[0]
			start := strings.Index(tt.source, tt.marker)
			if diagnostic.Code != "LDL1301" || diagnostic.MessageKey != "unknown_or_ambiguous_symbol" || len(diagnostic.Arguments) != 0 ||
				diagnostic.Range == nil || diagnostic.Range.StartByte != start || diagnostic.Range.EndByte != start+len(tt.marker) {
				t.Fatalf("diagnostic = %+v, want exact header [%d,%d)", diagnostic, start, start+len(tt.marker))
			}
		})
	}
}

func TestIssue14UnknownRowTypeProducesOneResolverDiagnosticForGroup(t *testing.T) {
	source := `project p "P" {}
layers {
  app "App" @0
}
entity_type record "Record" {}
entities record @app {
  first "First"
  second "Second"
}
rows missing_type [value] {
  first one: "x"
  second two: "y"
}
`
	got := Resolve(Input{EntryPath: "document.ldl", Project: ProjectInput{Files: map[string]SourceFile{"document.ldl": parse(source)}}})
	if len(got.Diagnostics) != 1 {
		t.Fatalf("diagnostics = %+v, want one group-header diagnostic", got.Diagnostics)
	}
	diagnostic := got.Diagnostics[0]
	start := strings.Index(source, "missing_type")
	if diagnostic.Code != "LDL1301" || diagnostic.Range == nil || diagnostic.Range.StartByte != start || diagnostic.Range.EndByte != start+len("missing_type") {
		t.Fatalf("diagnostic = %+v", diagnostic)
	}
}

func TestIssue14AmbiguousRowTypeProducesOneStableGroupDiagnostic(t *testing.T) {
	source := `rows duplicate [value] {
  item one: "x"
  item two: "y"
}
`
	file := parse(source)
	key := ModuleKey{Origin: Origin{Kind: OriginProject, ProjectID: "p"}, Path: "document.ldl"}
	local := topDecl(key, KindEntityType, "duplicate", zeroSpan())
	imported := local
	imported.Symbol.Path[0].ID = "other"
	imported.Address = addressOf(imported.Symbol)
	state := &moduleState{
		key:      key,
		ast:      extractModule(file),
		localTop: map[SubjectKind]map[string]DeclarationSymbol{KindEntityType: {"duplicate": local}},
		imported: map[SubjectKind]map[string]DeclarationSymbol{KindEntityType: {"duplicate": imported}},
	}
	resolver := &resolver{}
	resolver.resolveFactGroupRefs(state)
	if len(resolver.diagnostics) != 1 {
		t.Fatalf("diagnostics = %+v", resolver.diagnostics)
	}
	start := strings.Index(source, "duplicate")
	diagnostic := resolver.diagnostics[0]
	if diagnostic.Code != "LDL1301" || diagnostic.Range == nil || diagnostic.Range.StartByte != start || diagnostic.Range.EndByte != start+len("duplicate") {
		t.Fatalf("diagnostic = %+v", diagnostic)
	}
}
