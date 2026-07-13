// SPDX-License-Identifier: LicenseRef-LayerDraw-1.0

package resolve

import "testing"

func TestDefinitionSourcesAndRelationEndpointBindings(t *testing.T) {
	t.Parallel()

	in := Input{Mode: CompileProject, EntryPath: "document.ldl", Project: ProjectInput{Files: map[string]SourceFile{
		"document.ldl": parse(`import ns from "./schema.ldl"
project p "Project" {}
layers {
  app "Application" @0
}
relation_type rel "Rel" dependency {
  from source types [ns.server] layers [app]
  to target types [ns.server] layers [app]
  label "rel"
}
`),
		"schema.ldl": parse(`entity_type server "Server" {
  representation shape rect
}
export { server }
`),
	}}}
	got := Resolve(in)
	if got.HasErrors {
		t.Fatalf("Diagnostics = %+v", got.Diagnostics)
	}
	if got.Mode != CompileProject || got.RootAddress != "ldl:project:p" {
		t.Fatalf("root = %s %s", got.Mode, got.RootAddress)
	}
	if len(got.DeclarationSources) == 0 {
		t.Fatal("DeclarationSources is empty")
	}
	for _, src := range got.DeclarationSources {
		if src.Address == "ldl:project:p:relation-type:rel" && src.Node == nil {
			t.Fatalf("relation source node missing: %+v", src)
		}
	}
	requireBinding(t, got, "ns.server", "ldl:project:p:entity-type:server")
	requireBinding(t, got, "app", "ldl:project:p:layer:app")
}
