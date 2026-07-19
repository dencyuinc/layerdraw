// SPDX-License-Identifier: LicenseRef-LayerDraw-1.0

package engine

import (
	"fmt"
	"strings"
	"testing"
)

func nativeDocument(address, contentHash string) NativeIndexDocument {
	return NativeIndexDocument{SubjectAddress: address, SubjectKind: "entity", ContentHash: contentHash, LexicalText: "search text", GraphEntryAddresses: []string{address}, Embedding: []float32{1, 2}, FieldsJSON: `[]`}
}

func TestNativeSearchPlansRemainCandidateBoundedForLargeProject(t *testing.T) {
	documents := make([]NativeIndexDocument, 2048)
	for index := range documents {
		documents[index] = nativeDocument(fmt.Sprintf("entity:%04d", index), fmt.Sprintf("hash:%04d", index))
	}
	plan, err := BuildNativeSearchIndexPlan(NativeIndexPlanInput{Request: []byte(`{"kind":"build_search_index"}`), Identity: []byte("large"), BackendVersion: "0.17.0", EmbeddingDimensions: 2, Documents: documents, FullRebuild: true})
	if err != nil || len(plan.Statements) < len(documents) || plan.PhysicalEvidence[0].ExpectedDocumentSetDigest == "" {
		t.Fatalf("statements=%d evidence=%+v err=%v", len(plan.Statements), plan.PhysicalEvidence, err)
	}
	prepared, err := BuildNativeSearchPlan(NativeSearchPlanInput{Request: []byte(`{"kind":"search_documents","mode":"hybrid","query_text":"needle"}`), QueryEmbedding: []float32{1, 2}, LexicalLimit: 31, SemanticLimit: 37, MaxOutputBytes: 8192})
	if err != nil || prepared.MaxRows != 68 || len(prepared.Plan.Statements) != 2 {
		t.Fatalf("prepared=%+v err=%v", prepared, err)
	}
}

func TestBuildNativeSearchIndexPlanOwnsIncrementalContentHashDiff(t *testing.T) {
	input := NativeIndexPlanInput{Request: []byte(`{"kind":"build_search_index"}`), Identity: []byte(`{"identity":1}`), BackendVersion: "0.17.0", EmbeddingDimensions: 2, Documents: []NativeIndexDocument{nativeDocument("unchanged", "h1"), nativeDocument("changed", "h2"), nativeDocument("added", "h3")}, PreviousContentHashes: map[string]string{"unchanged": "h1", "changed": "old", "removed": "h0"}}
	plan, err := BuildNativeSearchIndexPlan(input)
	if err != nil || plan.PhysicalIndex == nil || len(plan.PhysicalEvidence) != 4 || plan.PhysicalEvidence[0].ExpectedDocumentSetDigest == "" {
		t.Fatalf("plan=%+v err=%v", plan, err)
	}
	created, deleted := map[string]bool{}, map[string]bool{}
	for _, statement := range plan.Statements {
		if strings.HasPrefix(statement.Query, "CREATE (n:SearchDoc") {
			created[statement.Parameters["id"].Value] = true
		}
		if strings.HasPrefix(statement.Query, "MATCH (n:SearchDoc {id:") {
			deleted[statement.Parameters["id"].Value] = true
		}
	}
	if created["unchanged"] || !created["changed"] || !created["added"] || !deleted["changed"] || !deleted["removed"] {
		t.Fatalf("created=%v deleted=%v", created, deleted)
	}
	full := input
	full.FullRebuild = true
	full.PreviousContentHashes = nil
	fullPlan, err := BuildNativeSearchIndexPlan(full)
	if err != nil || !strings.Contains(fullPlan.Statements[5].Query, "MATCH (n:SearchDoc) DELETE") {
		t.Fatalf("full plan=%+v err=%v", fullPlan, err)
	}
}

func TestBuildNativeSearchQueryAndAllAnalysisPlans(t *testing.T) {
	for _, mode := range []string{"lexical", "semantic", "hybrid"} {
		embedding := []float32(nil)
		if mode != "lexical" {
			embedding = []float32{1, 2}
		}
		prepared, err := BuildNativeSearchPlan(NativeSearchPlanInput{Request: []byte(`{"kind":"search_documents","mode":"` + mode + `","query_text":"needle"}`), QueryEmbedding: embedding, LexicalLimit: 5, SemanticLimit: 7, MaxOutputBytes: 4096})
		if err != nil || prepared.QueryText != "needle" || prepared.QueryDigest == "" || len(prepared.Plan.Statements) == 0 {
			t.Fatalf("mode=%s prepared=%+v err=%v", mode, prepared, err)
		}
	}
	if _, _, err := BuildNativeQueryPlan([]byte(`{"kind":"structural_query","root_addresses":["a"],"raw":"MATCH secret"}`)); err == nil {
		t.Fatal("raw query escaped the closed Engine request")
	}
	queryPlan, queryRows, err := BuildNativeQueryPlan([]byte(`{"kind":"structural_query","root_addresses":["b","a"]}`))
	if err != nil || queryRows != 2 || len(queryPlan.Statements) != 1 || queryPlan.Statements[0].Parameters["root_0"].Value != "b" {
		t.Fatalf("query plan=%+v rows=%d err=%v", queryPlan, queryRows, err)
	}
	for _, algorithm := range []string{"page_rank", "k_core", "louvain", "scc", "wcc"} {
		plan, rows, err := BuildNativeAnalysisPlan([]byte(`{"kind":"analyze_graph","algorithm":"` + algorithm + `","entity_addresses":["a","b"],"relation_addresses":["r"]}`))
		if err != nil || rows != 2 || len(plan.Statements) != 3 || !strings.Contains(plan.Statements[1].Query, "ORDER BY address") {
			t.Fatalf("algorithm=%s plan=%+v rows=%d err=%v", algorithm, plan, rows, err)
		}
	}
}
