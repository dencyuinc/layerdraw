// SPDX-License-Identifier: LicenseRef-LayerDraw-1.0

package endpoint

import (
	"context"
	"fmt"
	"os"
	"strings"
	"testing"

	"github.com/dencyuinc/layerdraw/internal/engine"
)

// Opt in with LAYERDRAW_BENCHMARK_CONTAINER=/path/to/project.layerdraw.
// Keep user documents out of the repository and read them without rewriting them.
func BenchmarkLocalDocumentReadContainer(b *testing.B) {
	path := os.Getenv("LAYERDRAW_BENCHMARK_CONTAINER")
	if path == "" {
		b.Skip("set LAYERDRAW_BENCHMARK_CONTAINER to benchmark a real container")
	}
	data, err := os.ReadFile(path)
	if err != nil {
		b.Fatal(err)
	}
	compiler := NewLocalDocumentEngine()
	b.ReportAllocs()
	b.ResetTimer()
	for b.Loop() {
		source, err := compiler.ReadContainer(context.Background(), data)
		if err != nil {
			b.Fatal(err)
		}
		if source.PortableID == "" || source.GraphHash == "" {
			b.Fatal("container did not produce a validated project")
		}
	}
}

func BenchmarkLocalDocumentCompileGraph(b *testing.B) {
	for _, size := range []int{100, 1000, 5000} {
		b.Run(fmt.Sprintf("nodes_%d_edges_%d", size, size-1), func(b *testing.B) {
			var source strings.Builder
			source.WriteString("project p \"Benchmark\" {}\nentity_type node \"Node\" {\nrepresentation shape rect\n}\nrelation_type link \"Link\" dependency {\nfrom source\nto target\nlabel \"links\"\n}\nlayers {\nmain \"Main\" @0\n}\nentities node @main {\n")
			for i := range size {
				fmt.Fprintf(&source, "n_%06d \"Node %d\"\n", i, i)
			}
			source.WriteString("}\nrelations link {\n")
			for i := 1; i < size; i++ {
				fmt.Fprintf(&source, "e_%06d: n_%06d -> n_%06d\n", i, i-1, i)
			}
			source.WriteString("}\n")
			input := LocalProjectInput{EntryPath: "document.ldl", ProjectSourceTree: map[string][]byte{"document.ldl": []byte(source.String())}, ResolvedDependencies: LocalResolvedDependencies{Format: "layerdraw-resolved", FormatVersion: 1, Language: 1}}
			compiler := NewLocalDocumentEngine()
			b.ReportAllocs()
			b.ResetTimer()
			for b.Loop() {
				compiled, err := compiler.CompileProject(context.Background(), input)
				if err != nil {
					diagnostics, compileErr := compiler.engine.Compile(context.Background(), engine.CompileInput{Mode: engine.CompileProject, EntryPath: input.EntryPath, ProjectSourceTree: input.ProjectSourceTree, ResolvedDependencies: input.ResolvedDependencies})
					b.Fatalf("%v: compile=%v diagnostics=%+v", err, compileErr, diagnostics.Snapshot().Diagnostics)
				}
				if len(compiled.SubjectHashes()) != size*2+3 {
					b.Fatalf("subject count=%d", len(compiled.SubjectHashes()))
				}
			}
		})
	}
}
