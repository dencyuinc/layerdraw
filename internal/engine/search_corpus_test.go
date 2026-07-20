// SPDX-License-Identifier: LicenseRef-LayerDraw-1.0

package engine

import (
	"context"
	"strings"
	"testing"
)

type corpusProjection struct{}

func (corpusProjection) AllowSearchDocument(document SearchDocument) bool {
	return !strings.Contains(document.SubjectAddress, ":beta")
}
func (corpusProjection) AllowSearchField(_ SearchDocument, field SearchField) bool {
	return field.FieldPath != "description"
}

func TestReadSearchCorpusAppliesTrustedDocumentAndFieldProjection(t *testing.T) {
	instance := New(BuildInfo{Workbench: WorkbenchConfig{EndpointInstanceID: "corpus-test"}})
	opened, err := instance.OpenDocument(context.Background(), OpenDocumentInput{CompileInput: CompileInput{Mode: CompileProject, EntryPath: "document.ldl", ProjectSourceTree: map[string][]byte{"document.ldl": []byte(`project p "Project" {
  description "redacted"
}
layers {
  app "Application" @1
}
entity_type service "Service" {
  representation shape rect
}
entities service @app {
  alpha "Alpha"
  beta "Beta"
}
`)}, ResolvedDependencies: ResolvedDependencies{Format: "layerdraw-resolved", FormatVersion: 1, Language: 1}}, RequestedLimits: WorkbenchLimits{MaxItems: 100, MaxOutputBytes: 1 << 20}})
	if err != nil {
		t.Fatal(err)
	}
	documents, err := instance.ReadSearchCorpus(context.Background(), opened.DocumentGeneration, corpusProjection{})
	if err != nil || len(documents) == 0 {
		t.Fatalf("documents=%+v err=%v", documents, err)
	}
	for _, document := range documents {
		if strings.Contains(document.SubjectAddress, ":beta") || strings.Contains(document.LexicalText, "redacted") || strings.Contains(document.Text, "redacted") {
			t.Fatalf("projection leaked: %+v", document)
		}
	}
	if _, err := instance.ReadSearchCorpus(context.Background(), opened.DocumentGeneration, nil); err == nil {
		t.Fatal("nil projection accepted")
	}
}
