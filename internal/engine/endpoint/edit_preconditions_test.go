// SPDX-License-Identifier: LicenseRef-LayerDraw-1.0

package endpoint

import (
	"context"
	"reflect"
	"testing"

	"github.com/dencyuinc/layerdraw/gen/go/engineprotocol"
)

func TestCompileProjectEditPreconditionsProjectsAuthoritativeSnapshot(t *testing.T) {
	generation := engineprotocol.DocumentGeneration{DocumentHandle: engineprotocol.DocumentHandle{EndpointInstanceID: "endpoint-test", Value: "document_test_123456"}, Value: "1"}
	input := LocalProjectInput{
		EntryPath:         "document.ldl",
		ProjectSourceTree: map[string][]byte{"document.ldl": []byte("project p \"P\" {}\n")},
		ResolvedDependencies: LocalResolvedDependencies{
			Format: "layerdraw-resolved", FormatVersion: 1, Language: 1,
		},
	}
	result, err := CompileProjectEditPreconditions(context.Background(), input, generation)
	if err != nil {
		t.Fatal(err)
	}
	if result.DocumentGeneration != generation || len(result.ExpectedSubjectHashes) == 0 || len(result.ExpectedSubtreeHashes) == 0 || len(result.ExpectedChildSets) == 0 || result.ExpectedSourceDigests == nil || len(*result.ExpectedSourceDigests) != 1 {
		t.Fatalf("preconditions=%+v", result)
	}
	if _, err := CompileProjectEditPreconditions(context.Background(), LocalProjectInput{EntryPath: "missing.ldl"}, generation); err == nil {
		t.Fatal("invalid project was accepted")
	}
}

func TestSourceEditPreconditionsMatchCompiledProjectInput(t *testing.T) {
	generation := engineprotocol.DocumentGeneration{DocumentHandle: engineprotocol.DocumentHandle{EndpointInstanceID: "endpoint-test", Value: "document_test_123456"}, Value: "1"}
	input := LocalProjectInput{
		EntryPath:         "document.ldl",
		ProjectSourceTree: map[string][]byte{"document.ldl": []byte("project p \"P\" {}\n")},
		ResolvedDependencies: LocalResolvedDependencies{
			Format: "layerdraw-resolved", FormatVersion: 1, Language: 1,
		},
	}
	source, err := NewLocalDocumentEngine().CompileProject(context.Background(), input)
	if err != nil {
		t.Fatal(err)
	}
	if source.DisplayName != "P" {
		t.Fatalf("source display name=%q", source.DisplayName)
	}
	fromSource, err := SourceEditPreconditions(context.Background(), source, generation)
	if err != nil {
		t.Fatal(err)
	}
	fromInput, err := CompileProjectEditPreconditions(context.Background(), input, generation)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(fromSource, fromInput) {
		t.Fatalf("preconditions diverged:\n%+v\n%+v", fromSource, fromInput)
	}
	if _, err := SourceEditPreconditions(context.Background(), LocalSource{}, generation); err == nil {
		t.Fatal("empty source was accepted")
	}
}
