// SPDX-License-Identifier: LicenseRef-LayerDraw-1.0

package enginesearch

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"slices"
	"strings"
	"unicode/utf8"

	"github.com/dencyuinc/layerdraw/internal/engine"
	"github.com/dencyuinc/layerdraw/internal/runtime/port"
)

var ErrInvalidNativeSearchRequest = errors.New("Engine Search adapter: invalid request")

type Adapter struct{ engine engine.Engine }

func New(value engine.Engine) *Adapter {
	return &Adapter{engine: value}
}

func (e *Adapter) available() bool {
	descriptor := e.engine.Describe()
	return descriptor.Component == "engine" && slices.Contains(descriptor.Capabilities, engine.CapabilityExecuteQuery)
}

type nativeSearchExecutionRequest struct {
	Kind      string `json:"kind"`
	QueryText string `json:"query_text,omitempty"`
}

type nativeSearchIndexRequest struct {
	PhysicalIndex port.PhysicalIndexRef `json:"physical_index"`
}

type nativeLadybugPlan struct {
	Statements       []nativeLadybugStatement `json:"statements"`
	PhysicalIndex    *port.PhysicalIndexRef   `json:"physical_index,omitempty"`
	PhysicalEvidence *nativeIndexEvidence     `json:"physical_evidence,omitempty"`
}

type nativeLadybugStatement struct {
	Query      string                   `json:"query"`
	Parameters map[string]port.RawValue `json:"parameters"`
}

type nativeIndexEvidence struct {
	TableName      string   `json:"table_name"`
	IndexName      string   `json:"index_name"`
	IndexType      string   `json:"index_type"`
	PropertyNames  []string `json:"property_names"`
	ContentColumns []string `json:"content_columns"`
	PrimaryKey     string   `json:"primary_key"`
}

// ProduceSearchDocumentBatch is the production Engine side of the document
// issuer. The adapter decorates only this result with authority; it never
// exposes a signer for caller-constructed batches.
func (e *Adapter) ProduceSearchDocumentBatch(ctx context.Context, request port.SearchDocumentBatchRequest) (port.SearchDocumentBatch, error) {
	if !e.available() || ctx == nil || ctx.Err() != nil || !validSearchSnapshot(request.Snapshot) || request.AccessProjectionDigest == "" || request.EmbeddingProfileDigest == "" || len(request.Documents) == 0 {
		return port.SearchDocumentBatch{}, ErrInvalidNativeSearchRequest
	}
	documents := make([]port.SearchDocumentInput, len(request.Documents))
	seen := make(map[string]bool, len(documents))
	for index, document := range request.Documents {
		if document.SubjectAddress == "" || document.ContentHash == "" || document.Text == "" || !utf8.ValidString(document.Text) || seen[document.SubjectAddress] {
			return port.SearchDocumentBatch{}, ErrInvalidNativeSearchRequest
		}
		seen[document.SubjectAddress] = true
		documents[index] = document
	}
	return port.SearchDocumentBatch{Snapshot: request.Snapshot, AccessProjectionDigest: request.AccessProjectionDigest, EmbeddingProfileDigest: request.EmbeddingProfileDigest, Documents: documents}, nil
}

func (e *Adapter) PrepareSearchIndex(_ context.Context, input port.SearchIndexPreparationInput) (port.ExecutionPlan, error) {
	var request nativeSearchIndexRequest
	if !e.available() {
		return port.ExecutionPlan{}, ErrInvalidNativeSearchRequest
	}
	if err := decodeClosedSearchJSON(input.Request, &request); err != nil || request.PhysicalIndex.IdentityDigest == "" || request.PhysicalIndex.ContentDigest == "" || request.PhysicalIndex.BackendVersion == "" || len(input.Batch.Documents) == 0 {
		return port.ExecutionPlan{}, ErrInvalidNativeSearchRequest
	}
	statements := []nativeLadybugStatement{{Query: "CREATE NODE TABLE IF NOT EXISTS SearchDoc (id STRING, body STRING, PRIMARY KEY(id))", Parameters: map[string]port.RawValue{}}}
	for _, document := range input.Batch.Documents {
		statements = append(statements, nativeLadybugStatement{Query: "MERGE (n:SearchDoc {id: $id}) SET n.body = $body", Parameters: map[string]port.RawValue{"id": {Kind: "string", Value: document.SubjectAddress}, "body": {Kind: "string", Value: document.Text}}})
	}
	statements = append(statements, nativeLadybugStatement{Query: "CALL CREATE_FTS_INDEX('SearchDoc', 'search_doc_fts', ['body'], stemmer := 'none')", Parameters: map[string]port.RawValue{}})
	evidence := &nativeIndexEvidence{TableName: "SearchDoc", IndexName: "search_doc_fts", IndexType: "FTS", PropertyNames: []string{"body"}, ContentColumns: []string{"id", "body"}, PrimaryKey: "id"}
	payload, err := json.Marshal(nativeLadybugPlan{Statements: statements, PhysicalIndex: &request.PhysicalIndex, PhysicalEvidence: evidence})
	if err != nil {
		return port.ExecutionPlan{}, err
	}
	return nativePlan(port.PlanSearchIndex, payload, 1, 4096), nil
}

func (e *Adapter) PrepareSearch(_ context.Context, input port.SearchPreparationInput) (port.PreparedSearch, error) {
	request, err := decodeExecutionRequest(input.Request, "search_documents")
	if !e.available() || err != nil || request.QueryText == "" || input.MaxOutputBytes <= 0 {
		return port.PreparedSearch{}, ErrInvalidNativeSearchRequest
	}
	statement := nativeLadybugStatement{Query: "CALL QUERY_FTS_INDEX('SearchDoc', 'search_doc_fts', $query, limit := $limit) RETURN node.id AS address, score ORDER BY score DESC, address", Parameters: map[string]port.RawValue{"query": {Kind: "string", Value: request.QueryText}, "limit": {Kind: "int64", Value: fmt.Sprint(input.SearchProfile.MaxHits)}}}
	payload, _ := json.Marshal(nativeLadybugPlan{Statements: []nativeLadybugStatement{statement}})
	plan := nativePlan(port.PlanSearch, payload, input.SearchProfile.MaxHits, input.MaxOutputBytes)
	digest := sha256.Sum256(input.Request)
	return port.PreparedSearch{Plan: plan, QueryDigest: "sha256:" + hex.EncodeToString(digest[:])}, nil
}

func (e *Adapter) PrepareQuery(_ context.Context, input port.BoundExecutionRequest) (port.ExecutionPlan, error) {
	_, err := decodeExecutionRequest(input.Request, "list_search_documents")
	if !e.available() || err != nil || input.MaxOutputBytes <= 0 {
		return port.ExecutionPlan{}, ErrInvalidNativeSearchRequest
	}
	payload, _ := json.Marshal(nativeLadybugPlan{Statements: []nativeLadybugStatement{{Query: "MATCH (n:SearchDoc) RETURN n.id AS address, n.body AS body ORDER BY address", Parameters: map[string]port.RawValue{}}}})
	return nativePlan(port.PlanQuery, payload, 1000, input.MaxOutputBytes), nil
}

func (e *Adapter) PrepareAnalysis(_ context.Context, input port.BoundExecutionRequest) (port.ExecutionPlan, error) {
	_, err := decodeExecutionRequest(input.Request, "count_search_documents")
	if !e.available() || err != nil || input.MaxOutputBytes <= 0 {
		return port.ExecutionPlan{}, ErrInvalidNativeSearchRequest
	}
	payload, _ := json.Marshal(nativeLadybugPlan{Statements: []nativeLadybugStatement{{Query: "RETURN 0 AS document_count", Parameters: map[string]port.RawValue{}}}})
	return nativePlan(port.PlanAnalysis, payload, 1, input.MaxOutputBytes), nil
}

func (e *Adapter) CompleteSearch(_ context.Context, input port.CompleteSearchInput) ([]byte, error) {
	return completeNativeRows("hits", input.Rows)
}
func (e *Adapter) CompleteQuery(_ context.Context, input port.CompleteExecutionInput) ([]byte, error) {
	return completeNativeRows("rows", input.Rows)
}
func (e *Adapter) CompleteAnalysis(_ context.Context, input port.CompleteExecutionInput) ([]byte, error) {
	return completeNativeRows("analysis", input.Rows)
}

func nativePlan(kind port.PlanKind, payload []byte, maxRows, maxBytes int) port.ExecutionPlan {
	digest := sha256.Sum256(append([]byte(kind+"\x00"), payload...))
	return port.ExecutionPlan{Kind: kind, PlanID: "plan-" + hex.EncodeToString(digest[:16]), ProtocolVersion: "v1", Payload: payload, MaxRows: maxRows, MaxBytes: maxBytes}
}

func completeNativeRows(field string, result port.ExecutionResult) ([]byte, error) {
	if !result.Complete || result.Truncated || result.Bytes < 0 {
		return nil, ErrInvalidNativeSearchRequest
	}
	rows := make([]map[string]port.RawValue, len(result.Rows))
	for index, row := range result.Rows {
		keys := make([]string, 0, len(row))
		for key := range row {
			keys = append(keys, key)
		}
		slices.Sort(keys)
		clone := make(map[string]port.RawValue, len(row))
		for _, key := range keys {
			clone[key] = row[key]
		}
		rows[index] = clone
	}
	return json.Marshal(map[string]any{field: rows})
}

func decodeExecutionRequest(data []byte, kind string) (nativeSearchExecutionRequest, error) {
	var request nativeSearchExecutionRequest
	if err := decodeClosedSearchJSON(data, &request); err != nil || request.Kind != kind {
		return request, ErrInvalidNativeSearchRequest
	}
	return request, nil
}

func decodeClosedSearchJSON(data []byte, destination any) error {
	decoder := json.NewDecoder(strings.NewReader(string(data)))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(destination); err != nil {
		return err
	}
	if err := decoder.Decode(&struct{}{}); err != io.EOF {
		return ErrInvalidNativeSearchRequest
	}
	return nil
}

func validSearchSnapshot(snapshot port.DocumentSnapshotRef) bool {
	host := snapshot.Kind == port.SnapshotHostRevision && snapshot.HostDocumentID != "" && snapshot.CommittedRevision != "" && snapshot.SourceTreeDigest == "" && snapshot.DocumentGeneration == 0
	portable := snapshot.Kind == port.SnapshotPortableGeneration && snapshot.HostDocumentID == "" && snapshot.CommittedRevision == "" && snapshot.SourceTreeDigest != ""
	return (host || portable) && snapshot.DefinitionHash != ""
}
