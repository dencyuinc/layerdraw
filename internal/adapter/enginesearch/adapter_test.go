// SPDX-License-Identifier: LicenseRef-LayerDraw-1.0

package enginesearch

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/dencyuinc/layerdraw/internal/engine"
	"github.com/dencyuinc/layerdraw/internal/runtime/port"
)

const corpusFixture = `project p "Project" {
  description "Project Field Secret"
}
layers {
  app "Application" @1
}
entity_type service "Service" {
  representation shape rect
}
entities service @app {
  alpha "Alpha Searchable"
  beta "Beta Secret"
}
`

type restrictedProjection struct{ digest string }

func (p restrictedProjection) AuthorizesSearchProjection(_ port.DocumentSnapshotRef, digest string) bool {
	return digest == p.digest
}
func (restrictedProjection) AllowSearchDocument(document engine.SearchDocument) bool {
	return !strings.Contains(document.SubjectAddress, ":beta")
}
func (restrictedProjection) AllowSearchField(_ engine.SearchDocument, field engine.SearchField) bool {
	return field.FieldPath != "description"
}

func boundAdapter(t *testing.T, projection AccessProjection) (*Adapter, port.DocumentSnapshotRef, port.SearchCorpusRef) {
	t.Helper()
	instance := engine.New(engine.BuildInfo{Workbench: engine.WorkbenchConfig{EndpointInstanceID: "search-test"}})
	adapter := New(instance, projection)
	snapshot := port.DocumentSnapshotRef{Kind: port.SnapshotHostRevision, HostDocumentID: "doc", CommittedRevision: "r1", DefinitionHash: "sha256:def"}
	ref, err := adapter.OpenLocalCorpus(context.Background(), engine.OpenDocumentInput{CompileInput: engine.CompileInput{Mode: engine.CompileProject, EntryPath: "document.ldl", ProjectSourceTree: map[string][]byte{"document.ldl": []byte(corpusFixture)}, ResolvedDependencies: engine.ResolvedDependencies{Format: "layerdraw-resolved", FormatVersion: 1, Language: 1}}, RequestedLimits: engine.WorkbenchLimits{MaxItems: 1000, MaxOutputBytes: 1 << 20}}, snapshot, "sha256:access")
	if err != nil {
		t.Fatal(err)
	}
	return adapter, snapshot, ref
}

func TestProductionEngineAdapterIssuesOnlyBoundAccessProjectedCorpus(t *testing.T) {
	adapter, snapshot, ref := boundAdapter(t, restrictedProjection{digest: "sha256:access"})
	batch, err := adapter.ProduceSearchDocumentBatch(context.Background(), port.SearchDocumentBatchRequest{Snapshot: snapshot, AccessProjectionDigest: "sha256:access", EmbeddingProfileDigest: "sha256:model", Corpus: ref})
	if err != nil || len(batch.Documents) == 0 || batch.Token != "" {
		t.Fatalf("batch=%+v err=%v", batch, err)
	}
	for _, document := range batch.Documents {
		if strings.Contains(document.SubjectAddress, ":beta") || strings.Contains(document.Text, "Beta Secret") || strings.Contains(document.Text, "Project Field Secret") || strings.Contains(document.LexicalText, "Project Field Secret") {
			t.Fatalf("unauthorized document was issued: %+v", document)
		}
		for _, field := range document.Fields {
			if strings.Contains(field.Text, "Beta Secret") || strings.Contains(field.Text, "Project Field Secret") {
				t.Fatalf("unauthorized field/source projection was issued: %+v", field)
			}
		}
	}
	wrong := snapshot
	wrong.CommittedRevision = "r2"
	if _, err := adapter.ProduceSearchDocumentBatch(context.Background(), port.SearchDocumentBatchRequest{Snapshot: wrong, AccessProjectionDigest: "sha256:access", EmbeddingProfileDigest: "sha256:model", Corpus: ref}); err == nil {
		t.Fatal("corpus binding was reusable across revisions")
	}
}

func TestProductionEngineAdapterBuildsClosedLexicalAndStructuralPlans(t *testing.T) {
	projection, _ := NewLocalAccessProjection("sha256:access")
	adapter, snapshot, _ := boundAdapter(t, projection)
	document := port.SearchDocumentInput{SubjectAddress: "ldl:project:p:entity:alpha", SubjectKind: "entity", ContentHash: "sha256:content", Text: "layer draw"}
	request := []byte(`{"kind":"build_search_index"}`)
	profile := port.EmbeddingProfile{Dimensions: 2}
	identity := port.SearchIndexIdentity{DocumentSnapshotRef: snapshot, SearchProfileID: "default", SearchProfileDigest: "sha256:search", EmbeddingProfileID: "embed", EmbeddingProfileDigest: "sha256:model", AccessProjectionDigest: "sha256:access", LadybugBackendVersion: "0.17.0", IndexSchemaVersion: "1"}
	plan, err := adapter.PrepareSearchIndex(context.Background(), port.SearchIndexPreparationInput{IndexIdentity: identity, Batch: port.SearchDocumentBatch{Documents: []port.SearchDocumentInput{document}}, EmbeddingProfile: &profile, Embeddings: []port.EmbeddingVector{{SubjectAddress: document.SubjectAddress, ContentHash: document.ContentHash, Values: []float32{0.1, 0.2}}}, Request: request})
	if err != nil || plan.Kind != port.PlanSearchIndex || !json.Valid(plan.Payload) {
		t.Fatalf("plan=%+v err=%v", plan, err)
	}
	prepared, err := adapter.PrepareSearch(context.Background(), port.SearchPreparationInput{BoundExecutionRequest: port.BoundExecutionRequest{Snapshot: snapshot, Request: []byte(`{"kind":"search_documents","mode":"lexical","query_text":"layer","target_kind":"entity"}`), MaxOutputBytes: 4096}, SearchProfile: port.SearchProfile{MaxHits: 10, LexicalCandidateLimit: 10, SemanticCandidateLimit: 10}})
	if err != nil || prepared.Plan.Kind != port.PlanSearch || prepared.QueryDigest == "" {
		t.Fatalf("prepared=%+v err=%v", prepared, err)
	}
	query, err := adapter.PrepareQuery(context.Background(), port.BoundExecutionRequest{Request: []byte(`{"kind":"structural_query","root_addresses":["ldl:project:p:entity:alpha"]}`), MaxOutputBytes: 4096})
	if err != nil || query.Kind != port.PlanQuery || !json.Valid(query.Payload) {
		t.Fatalf("query=%+v err=%v", query, err)
	}
	rows := port.ExecutionResult{Complete: true, Rows: []port.RawRow{{"signal": {Kind: "string", Value: "lexical"}, "address": {Kind: "string", Value: "ldl:project:p:entity:alpha"}, "kind": {Kind: "string", Value: "entity"}, "owner": {Kind: "string"}, "graph_entries": {Kind: "string", Value: `[]`}, "type_addresses": {Kind: "string", Value: `[]`}, "layer_addresses": {Kind: "string", Value: `[]`}, "content_hash": {Kind: "string", Value: "sha256:content"}, "score": {Kind: "float64", Value: "1.5"}}}}
	result, err := adapter.CompleteSearch(context.Background(), port.CompleteSearchInput{Prepared: prepared, Rows: rows})
	if err != nil || !strings.Contains(string(result), `"subject_address":"ldl:project:p:entity:alpha"`) || !strings.Contains(string(result), `"rank":1`) {
		t.Fatalf("result=%s err=%v", result, err)
	}
}

func TestProductionEngineAdapterRejectsRawAndPlansBoundedAnalysis(t *testing.T) {
	projection, _ := NewLocalAccessProjection("sha256:access")
	adapter, _, _ := boundAdapter(t, projection)
	if _, err := adapter.PrepareQuery(context.Background(), port.BoundExecutionRequest{Request: []byte(`{"kind":"structural_query","root_addresses":["a"],"raw":"MATCH secret"}`), MaxOutputBytes: 1}); err == nil {
		t.Fatal("raw query field crossed Engine boundary")
	}
	semantic, err := adapter.PrepareSearch(context.Background(), port.SearchPreparationInput{BoundExecutionRequest: port.BoundExecutionRequest{Request: []byte(`{"kind":"search_documents","mode":"semantic","query_text":"x"}`), MaxOutputBytes: 4096}, SearchProfile: port.SearchProfile{MaxHits: 1, LexicalCandidateLimit: 1, SemanticCandidateLimit: 1}, QueryEmbedding: []float32{1}})
	if err != nil || semantic.Mode != "semantic" || semantic.Plan.Kind != port.PlanSearch {
		t.Fatalf("semantic=%+v err=%v", semantic, err)
	}
	analysis, err := adapter.PrepareAnalysis(context.Background(), port.BoundExecutionRequest{Request: []byte(`{"kind":"analyze_graph","algorithm":"page_rank","entity_addresses":["ldl:project:p:entity:alpha"],"relation_addresses":["ldl:project:p:relation:alpha_beta"]}`), MaxOutputBytes: 4096})
	if err != nil || analysis.Kind != port.PlanAnalysis || !json.Valid(analysis.Payload) {
		t.Fatalf("analysis=%+v err=%v", analysis, err)
	}
}

func TestEngineAdapterCompletesQueryAnalysisAndLocalProjection(t *testing.T) {
	projection, err := NewLocalAccessProjection("sha256:access")
	if err != nil || !projection.AllowSearchDocument(engine.SearchDocument{}) || !projection.AllowSearchField(engine.SearchDocument{}, engine.SearchField{}) {
		t.Fatalf("projection=%v err=%v", projection, err)
	}
	adapter, _, _ := boundAdapter(t, projection)
	query, err := adapter.CompleteQuery(context.Background(), port.CompleteExecutionInput{Rows: port.ExecutionResult{Complete: true, Rows: []port.RawRow{{"address": {Kind: "string", Value: "a"}}}}})
	if err != nil || !strings.Contains(string(query), `"address"`) {
		t.Fatalf("query=%s err=%v", query, err)
	}
	analysis, err := adapter.CompleteAnalysis(context.Background(), port.CompleteExecutionInput{Rows: port.ExecutionResult{Complete: true, Rows: []port.RawRow{{"address": {Kind: "string", Value: "a"}, "metric_name": {Kind: "string", Value: "importance"}, "metric_value": {Kind: "float64", Value: "0.5"}}}}})
	if err != nil || !strings.Contains(string(analysis), `"importance"`) {
		t.Fatalf("analysis=%s err=%v", analysis, err)
	}
	if _, err := adapter.CompleteQuery(context.Background(), port.CompleteExecutionInput{Rows: port.ExecutionResult{Truncated: true}}); err == nil {
		t.Fatal("truncated query accepted")
	}
	if _, err := adapter.CompleteAnalysis(context.Background(), port.CompleteExecutionInput{Rows: port.ExecutionResult{Complete: true, Rows: []port.RawRow{{}}}}); err == nil {
		t.Fatal("invalid analysis row accepted")
	}
}
