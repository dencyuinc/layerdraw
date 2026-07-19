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
