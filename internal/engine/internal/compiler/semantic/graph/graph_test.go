// SPDX-License-Identifier: LicenseRef-LayerDraw-1.0

package graph

import (
	"os"
	"path/filepath"
	"reflect"
	"testing"

	"github.com/dencyuinc/layerdraw/internal/engine/internal/compiler/resolve"
	"github.com/dencyuinc/layerdraw/internal/engine/internal/compiler/semantic/definition"
	"github.com/dencyuinc/layerdraw/internal/engine/internal/compiler/syntax"
)

func TestCompileValidRowsCyclesCrossLayerAndAdjacency(t *testing.T) {
	source, err := os.ReadFile(filepath.Join("testdata", "valid_graph.ldl"))
	if err != nil {
		t.Fatal(err)
	}
	got := compileFiles(t, map[string]string{"document.ldl": string(source)})
	if got.HasErrors || got.Graph == nil {
		t.Fatalf("Compile() diagnostics = %+v", got.Diagnostics)
	}
	graph := got.Graph
	if len(graph.Entities) != 3 || len(graph.Relations) != 3 {
		t.Fatalf("graph sizes = entities %d, relations %d", len(graph.Entities), len(graph.Relations))
	}
	alpha := graph.Entities[0]
	if alpha.ID != "alpha" || alpha.DisplayName != "Alpha" || alpha.TypeAddress != "ldl:project:p:entity-type:service" || alpha.LayerAddress != "ldl:project:p:layer:app" {
		t.Fatalf("alpha = %+v", alpha)
	}
	if !reflect.DeepEqual(alpha.Tags, []string{"api", "critical"}) || !reflect.DeepEqual(alpha.ReservedRowIDs, []string{"legacy"}) {
		t.Fatalf("alpha common/reservations = %+v", alpha)
	}
	if len(alpha.Rows) != 1 || len(alpha.Rows[0].Values) != 2 {
		t.Fatalf("alpha rows = %+v", alpha.Rows)
	}
	values := cellsByAddress(alpha.Rows[0])
	if values["ldl:project:p:entity-type:service:column:environment"].String != "prod" || values["ldl:project:p:entity-type:service:column:hostname"].String != "api.example.com" {
		t.Fatalf("normalized/default values = %+v", values)
	}
	if _, present := values["ldl:project:p:entity-type:service:column:owner"]; present {
		t.Fatalf("explicit absence materialized a default/value: %+v", values)
	}

	if graph.Relations[0].ID != "alpha_beta" || graph.Relations[0].FromAddress != alpha.Address || graph.Relations[0].ToAddress != graph.Entities[1].Address || graph.Relations[0].CrossLayer {
		t.Fatalf("first directed relation = %+v", graph.Relations[0])
	}
	if graph.Relations[0].DisplayName == nil || *graph.Relations[0].DisplayName != "Alpha to Beta" || len(graph.Relations[0].Rows) != 2 {
		t.Fatalf("relation display/rows = %+v", graph.Relations[0])
	}
	if graph.Relations[0].Rows[0].ID != "grpc" || graph.Relations[0].Rows[1].ID != "http" {
		t.Fatalf("row order is not structured StableSymbol order: %+v", graph.Relations[0].Rows)
	}
	if !graph.Relations[1].CrossLayer || !graph.Relations[2].CrossLayer {
		t.Fatalf("cross-layer flags = %+v", graph.Relations)
	}
	wantOutgoing := [][]string{{graph.Relations[0].Address}, {graph.Relations[1].Address}, {graph.Relations[2].Address}}
	wantIncoming := [][]string{{graph.Relations[2].Address}, {graph.Relations[0].Address}, {graph.Relations[1].Address}}
	for i := range graph.Entities {
		if graph.Outgoing[i].EntityAddress != graph.Entities[i].Address || graph.Incoming[i].EntityAddress != graph.Entities[i].Address ||
			!reflect.DeepEqual(graph.Outgoing[i].RelationAddresses, wantOutgoing[i]) || !reflect.DeepEqual(graph.Incoming[i].RelationAddresses, wantIncoming[i]) {
			t.Fatalf("adjacency[%d] outgoing=%+v incoming=%+v", i, graph.Outgoing[i], graph.Incoming[i])
		}
	}
}

func compileFiles(t *testing.T, files map[string]string) Result {
	t.Helper()
	input := inputFiles(t, files)
	return Compile(input)
}

func inputFiles(t *testing.T, files map[string]string) Input {
	t.Helper()
	parsed := map[string]resolve.SourceFile{}
	for path, source := range files {
		parsed[path] = resolve.SourceFromParse(syntax.Parse([]byte(source)))
	}
	resolved := resolve.Resolve(resolve.Input{Mode: resolve.CompileProject, EntryPath: "document.ldl", Project: resolve.ProjectInput{Files: parsed}})
	defined := definition.Compile(definition.Input{Resolve: resolved})
	return Input{Resolve: resolved, Definition: defined}
}

func cellsByAddress(row AttributeRow) map[string]definition.Scalar {
	out := map[string]definition.Scalar{}
	for _, cell := range row.Values {
		out[cell.ColumnAddress] = cell.Value
	}
	return out
}
