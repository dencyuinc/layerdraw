// SPDX-License-Identifier: LicenseRef-LayerDraw-1.0

package endpoint

import (
	"context"
	"testing"

	"github.com/dencyuinc/layerdraw/gen/go/engineprotocol"
	"github.com/dencyuinc/layerdraw/gen/go/protocolcommon"
	"github.com/dencyuinc/layerdraw/internal/engine"
)

func TestOpenDocumentResultWithRecipesIsWireRepresentable(t *testing.T) {
	const source = `project p "Project" {}

entity_type service "Service" {
  representation shape rect
}

entities service {
  a "Service A"
}

query all "All" {
  select {}
}

view v "V" inventory {
  source query all {}
  table {}
}
`
	compiler := engine.New(engine.BuildInfo{Workbench: engine.WorkbenchConfig{EndpointInstanceID: "layerdraw-desktop"}})
	opened, err := compiler.OpenDocument(context.Background(), engine.OpenDocumentInput{
		CompileInput: engine.CompileInput{
			Mode: engine.CompileProject, EntryPath: "fixture.ldl",
			ProjectSourceTree:    map[string][]byte{"fixture.ldl": []byte(source)},
			ResolvedDependencies: engine.ResolvedDependencies{Format: "layerdraw-resolved", FormatVersion: 1, Language: 1},
		},
		RequestedLimits: engine.WorkbenchLimits{MaxItems: 64, MaxOutputBytes: 65536},
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := encodeWorkbenchTerminal(OperationOpenDocument, opened, nil, engineprotocol.WorkbenchFailure{}, protocolcommon.OutcomeSuccess, "0.0.0-dev", "conformance-open"); err != nil {
		t.Fatalf("open result is not wire representable: %v", err)
	}
}
