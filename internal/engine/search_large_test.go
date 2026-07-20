// SPDX-License-Identifier: LicenseRef-LayerDraw-1.0

package engine

import (
	"fmt"
	"testing"
)

func TestNativeLexicalIndexPlanAcceptsLargeProjectWithinExplicitBounds(t *testing.T) {
	const documentCount = 10_000
	documents := make([]NativeIndexDocument, documentCount)
	for index := range documents {
		address := fmt.Sprintf("ldl:project:large:entity:item_%05d", index)
		documents[index] = NativeIndexDocument{
			SubjectAddress: address,
			SubjectKind:    "entity",
			OwnerAddress:   "ldl:project:large",
			ContentHash:    fmt.Sprintf("sha256:content-%05d", index),
			LexicalText:    fmt.Sprintf("searchable item %05d", index),
			FieldsJSON:     "[]",
		}
	}
	plan, err := BuildNativeSearchIndexPlan(NativeIndexPlanInput{
		Request: []byte(`{"kind":"build_search_index"}`), Identity: []byte(`{"large":true}`),
		BackendVersion: "0.17.0", EmbeddingDimensions: 0, Documents: documents, FullRebuild: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(plan.Statements) < documentCount || len(plan.PhysicalEvidence) != 3 || plan.PhysicalEvidence[0].IndexType != "FTS" {
		t.Fatalf("statements=%d evidence=%+v", len(plan.Statements), plan.PhysicalEvidence)
	}
	for _, evidence := range plan.PhysicalEvidence {
		if evidence.IndexType == "HNSW" {
			t.Fatal("lexical-only large project unexpectedly requires vector index")
		}
	}
}
