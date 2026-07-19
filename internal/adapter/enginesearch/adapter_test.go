// SPDX-License-Identifier: LicenseRef-LayerDraw-1.0

package enginesearch

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/dencyuinc/layerdraw/internal/engine"
	"github.com/dencyuinc/layerdraw/internal/runtime/port"
)

func TestProductionEngineAdapterProducesDocumentsAndClosedPlans(t *testing.T) {
	adapter := New(engine.New(engine.BuildInfo{}))
	snapshot := port.DocumentSnapshotRef{Kind: port.SnapshotHostRevision, HostDocumentID: "doc", CommittedRevision: "r1", DefinitionHash: "sha256:def"}
	batch, err := adapter.ProduceSearchDocumentBatch(context.Background(), port.SearchDocumentBatchRequest{Snapshot: snapshot, AccessProjectionDigest: "sha256:access", EmbeddingProfileDigest: "sha256:model", Documents: []port.SearchDocumentInput{{SubjectAddress: "ldl:project:p:entity:e", ContentHash: "sha256:content", Text: "allowed text"}}})
	if err != nil || len(batch.Documents) != 1 || batch.Token != "" {
		t.Fatalf("batch=%+v err=%v", batch, err)
	}
	bound := port.BoundExecutionRequest{Snapshot: snapshot, AccessProjectionDigest: "sha256:access", Request: []byte(`{"kind":"list_search_documents"}`), MaxOutputBytes: 4096}
	query, err := adapter.PrepareQuery(context.Background(), bound)
	if err != nil || query.Kind != port.PlanQuery || query.Token != "" || !json.Valid(query.Payload) {
		t.Fatalf("query=%+v err=%v", query, err)
	}
	bound.Request = []byte(`{"kind":"count_search_documents"}`)
	analysis, err := adapter.PrepareAnalysis(context.Background(), bound)
	if err != nil || analysis.Kind != port.PlanAnalysis || !json.Valid(analysis.Payload) {
		t.Fatalf("analysis=%+v err=%v", analysis, err)
	}
	if _, err := adapter.PrepareQuery(context.Background(), port.BoundExecutionRequest{Request: []byte(`{"kind":"list_search_documents","raw":"MATCH secret"}`), MaxOutputBytes: 1}); err == nil {
		t.Fatal("raw query field crossed the Engine adapter boundary")
	}
	result, err := adapter.CompleteQuery(context.Background(), port.CompleteExecutionInput{Rows: port.ExecutionResult{Complete: true, Rows: []port.RawRow{{"address": {Kind: "string", Value: "a"}}}, Bytes: 1}})
	if err != nil || string(result) != `{"rows":[{"address":{"Kind":"string","Value":"a"}}]}` {
		t.Fatalf("result=%s err=%v", result, err)
	}
}

func TestProductionEngineAdapterRejectsUnboundDocumentGeneration(t *testing.T) {
	adapter := New(engine.New(engine.BuildInfo{}))
	request := port.SearchDocumentBatchRequest{AccessProjectionDigest: "sha256:access", EmbeddingProfileDigest: "sha256:model", Documents: []port.SearchDocumentInput{{SubjectAddress: "a", ContentHash: "h", Text: "text"}}}
	if _, err := adapter.ProduceSearchDocumentBatch(context.Background(), request); err == nil {
		t.Fatal("unbound Search documents were issued")
	}
	request.Snapshot = port.DocumentSnapshotRef{Kind: port.SnapshotHostRevision, HostDocumentID: "doc", CommittedRevision: "r1", DefinitionHash: "sha256:def"}
	request.Documents = append(request.Documents, request.Documents[0])
	if _, err := adapter.ProduceSearchDocumentBatch(context.Background(), request); err == nil {
		t.Fatal("duplicate Search documents were issued")
	}
}

func TestProductionEngineAdapterBuildsIndexSearchAndCompletionPlans(t *testing.T) {
	adapter := New(engine.New(engine.BuildInfo{}))
	snapshot := port.DocumentSnapshotRef{Kind: port.SnapshotHostRevision, HostDocumentID: "doc", CommittedRevision: "r1", DefinitionHash: "sha256:def"}
	document := port.SearchDocumentInput{SubjectAddress: "ldl:project:p:entity:e", ContentHash: "sha256:content", Text: "layer draw"}
	physical := port.PhysicalIndexRef{IdentityDigest: "sha256:identity", ContentDigest: "sha256:content", BackendVersion: "0.17.0"}
	request, err := json.Marshal(map[string]any{"physical_index": physical})
	if err != nil {
		t.Fatal(err)
	}
	plan, err := adapter.PrepareSearchIndex(context.Background(), port.SearchIndexPreparationInput{Batch: port.SearchDocumentBatch{Documents: []port.SearchDocumentInput{document}}, Request: request})
	if err != nil || plan.Kind != port.PlanSearchIndex || plan.MaxRows != 1 || !json.Valid(plan.Payload) {
		t.Fatalf("plan=%+v err=%v", plan, err)
	}
	prepared, err := adapter.PrepareSearch(context.Background(), port.SearchPreparationInput{
		BoundExecutionRequest: port.BoundExecutionRequest{Snapshot: snapshot, Request: []byte(`{"kind":"search_documents","query_text":"layer"}`), MaxOutputBytes: 4096},
		SearchProfile:         port.SearchProfile{MaxHits: 10},
	})
	if err != nil || prepared.Plan.Kind != port.PlanSearch || prepared.QueryDigest == "" {
		t.Fatalf("prepared=%+v err=%v", prepared, err)
	}
	rows := port.ExecutionResult{Complete: true, Rows: []port.RawRow{{"address": {Kind: "string", Value: "a"}}}, Bytes: 1}
	searchResult, err := adapter.CompleteSearch(context.Background(), port.CompleteSearchInput{Rows: rows})
	if err != nil || string(searchResult) != `{"hits":[{"address":{"Kind":"string","Value":"a"}}]}` {
		t.Fatalf("search result=%s err=%v", searchResult, err)
	}
	analysisResult, err := adapter.CompleteAnalysis(context.Background(), port.CompleteExecutionInput{Rows: rows})
	if err != nil || string(analysisResult) != `{"analysis":[{"address":{"Kind":"string","Value":"a"}}]}` {
		t.Fatalf("analysis result=%s err=%v", analysisResult, err)
	}
}

func TestProductionEngineAdapterRejectsInvalidPlansAndRows(t *testing.T) {
	adapter := New(engine.New(engine.BuildInfo{}))
	if _, err := adapter.PrepareSearchIndex(context.Background(), port.SearchIndexPreparationInput{Request: []byte(`{"physical_index":{}}`)}); err == nil {
		t.Fatal("invalid physical index was accepted")
	}
	if _, err := adapter.PrepareSearch(context.Background(), port.SearchPreparationInput{BoundExecutionRequest: port.BoundExecutionRequest{Request: []byte(`{"kind":"search_documents"}`), MaxOutputBytes: 1}}); err == nil {
		t.Fatal("empty search text was accepted")
	}
	if _, err := adapter.CompleteAnalysis(context.Background(), port.CompleteExecutionInput{Rows: port.ExecutionResult{Truncated: true}}); err == nil {
		t.Fatal("truncated analysis was accepted")
	}
}
