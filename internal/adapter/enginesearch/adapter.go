// SPDX-License-Identifier: LicenseRef-LayerDraw-1.0

package enginesearch

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"sync"
	"unicode/utf8"

	"github.com/dencyuinc/layerdraw/internal/engine"
	"github.com/dencyuinc/layerdraw/internal/runtime/port"
)

var ErrInvalidNativeSearchRequest = errors.New("Engine Search adapter: invalid request")

type AccessProjection interface {
	engine.SearchCorpusProjection
	AuthorizesSearchProjection(port.DocumentSnapshotRef, string) bool
}

type LocalAccessProjection struct{ digest string }

func NewLocalAccessProjection(digest string) (*LocalAccessProjection, error) {
	if digest == "" {
		return nil, ErrInvalidNativeSearchRequest
	}
	return &LocalAccessProjection{digest: digest}, nil
}
func (p *LocalAccessProjection) AuthorizesSearchProjection(_ port.DocumentSnapshotRef, digest string) bool {
	return p != nil && digest == p.digest
}
func (*LocalAccessProjection) AllowSearchDocument(engine.SearchDocument) bool { return true }
func (*LocalAccessProjection) AllowSearchField(engine.SearchDocument, engine.SearchField) bool {
	return true
}

type Adapter struct {
	engine     engine.Engine
	projection AccessProjection
	mu         sync.RWMutex
	bindings   map[port.SearchCorpusRef]corpusBinding
}

type corpusBinding struct {
	snapshot port.DocumentSnapshotRef
	digest   string
}

func New(value engine.Engine, projection AccessProjection) *Adapter {
	return &Adapter{engine: value, projection: projection, bindings: map[port.SearchCorpusRef]corpusBinding{}}
}

// OpenLocalCorpus compiles trusted local source through Engine and binds the
// resulting immutable generation to exactly one snapshot/Access projection.
func (e *Adapter) OpenLocalCorpus(ctx context.Context, input engine.OpenDocumentInput, snapshot port.DocumentSnapshotRef, accessProjectionDigest string) (port.SearchCorpusRef, error) {
	if e.projection == nil || !validSearchSnapshot(snapshot) || !e.projection.AuthorizesSearchProjection(snapshot, accessProjectionDigest) {
		return port.SearchCorpusRef{}, ErrInvalidNativeSearchRequest
	}
	opened, err := e.engine.OpenDocument(ctx, input)
	if err != nil {
		return port.SearchCorpusRef{}, fmt.Errorf("%w: Engine corpus open failed: %v", ErrInvalidNativeSearchRequest, err)
	}
	if opened.State.SemanticState != "available" {
		return port.SearchCorpusRef{}, fmt.Errorf("%w: Engine corpus semantic state %s", ErrInvalidNativeSearchRequest, opened.State.SemanticState)
	}
	ref := port.SearchCorpusRef{EndpointInstanceID: opened.DocumentHandle.EndpointInstanceID, DocumentHandle: opened.DocumentHandle.Value, Generation: opened.DocumentGeneration.Value}
	e.mu.Lock()
	e.bindings[ref] = corpusBinding{snapshot: snapshot, digest: accessProjectionDigest}
	e.mu.Unlock()
	return ref, nil
}

func (e *Adapter) available() bool {
	descriptor := e.engine.Describe()
	return descriptor.Component == "engine" && slices.Contains(descriptor.Capabilities, engine.CapabilityExecuteQuery)
}

// ProduceSearchDocumentBatch is the production Engine side of the document
// issuer. The adapter decorates only this result with authority; it never
// exposes a signer for caller-constructed batches.
func (e *Adapter) ProduceSearchDocumentBatch(ctx context.Context, request port.SearchDocumentBatchRequest) (port.SearchDocumentBatch, error) {
	if !e.available() || e.projection == nil || ctx == nil || ctx.Err() != nil || !validSearchSnapshot(request.Snapshot) || request.EmbeddingProfileDigest == "" || request.Corpus.EndpointInstanceID == "" || request.Corpus.DocumentHandle == "" || request.Corpus.Generation == 0 || !e.projection.AuthorizesSearchProjection(request.Snapshot, request.AccessProjectionDigest) {
		return port.SearchDocumentBatch{}, ErrInvalidNativeSearchRequest
	}
	e.mu.RLock()
	binding, bound := e.bindings[request.Corpus]
	e.mu.RUnlock()
	if !bound || binding.snapshot != request.Snapshot || binding.digest != request.AccessProjectionDigest {
		return port.SearchDocumentBatch{}, ErrInvalidNativeSearchRequest
	}
	corpus, err := e.engine.ReadSearchCorpus(ctx, engine.DocumentGeneration{DocumentHandle: engine.DocumentHandle{EndpointInstanceID: request.Corpus.EndpointInstanceID, Value: request.Corpus.DocumentHandle}, Value: request.Corpus.Generation}, e.projection)
	if err != nil || len(corpus) == 0 {
		return port.SearchDocumentBatch{}, ErrInvalidNativeSearchRequest
	}
	documents := make([]port.SearchDocumentInput, len(corpus))
	seen := make(map[string]bool, len(documents))
	for index, document := range corpus {
		if document.SubjectAddress == "" || document.ContentHash == "" || document.Text == "" || !utf8.ValidString(document.Text) || seen[document.SubjectAddress] {
			return port.SearchDocumentBatch{}, ErrInvalidNativeSearchRequest
		}
		seen[document.SubjectAddress] = true
		fields := make([]port.SearchDocumentField, len(document.Fields))
		for fieldIndex, field := range document.Fields {
			fields[fieldIndex] = port.SearchDocumentField{FieldPath: field.FieldPath, SourceRef: field.SourceRef, Text: field.Text, LexicalWeight: field.LexicalWeight}
		}
		documents[index] = port.SearchDocumentInput{SubjectAddress: document.SubjectAddress, SubjectKind: document.SubjectKind, OwnerAddress: document.OwnerAddress, GraphEntryAddresses: append([]string(nil), document.GraphEntryAddresses...), TypeAddresses: append([]string(nil), document.TypeAddresses...), LayerAddresses: append([]string(nil), document.LayerAddresses...), ContentHash: document.ContentHash, LexicalText: document.LexicalText, Text: document.Text, Fields: fields}
	}
	return port.SearchDocumentBatch{Snapshot: request.Snapshot, AccessProjectionDigest: request.AccessProjectionDigest, EmbeddingProfileDigest: request.EmbeddingProfileDigest, Documents: documents}, nil
}

func (e *Adapter) PrepareSearchIndex(_ context.Context, input port.SearchIndexPreparationInput) (port.ExecutionPlan, error) {
	if !e.available() || input.EmbeddingProfile == nil || len(input.Embeddings) != len(input.Batch.Documents) {
		return port.ExecutionPlan{}, ErrInvalidNativeSearchRequest
	}
	identity, err := json.Marshal(input.IndexIdentity)
	if err != nil {
		return port.ExecutionPlan{}, ErrInvalidNativeSearchRequest
	}
	embeddings := make(map[string]port.EmbeddingVector, len(input.Embeddings))
	for _, value := range input.Embeddings {
		embeddings[value.SubjectAddress] = value
	}
	documents := make([]engine.NativeIndexDocument, len(input.Batch.Documents))
	for index, document := range input.Batch.Documents {
		vector, ok := embeddings[document.SubjectAddress]
		if !ok || vector.ContentHash != document.ContentHash {
			return port.ExecutionPlan{}, ErrInvalidNativeSearchRequest
		}
		fieldsJSON, marshalErr := json.Marshal(document.Fields)
		if marshalErr != nil {
			return port.ExecutionPlan{}, ErrInvalidNativeSearchRequest
		}
		documents[index] = engine.NativeIndexDocument{SubjectAddress: document.SubjectAddress, SubjectKind: document.SubjectKind, OwnerAddress: document.OwnerAddress, ContentHash: document.ContentHash, LexicalText: document.LexicalText, GraphEntryAddresses: document.GraphEntryAddresses, TypeAddresses: document.TypeAddresses, LayerAddresses: document.LayerAddresses, Embedding: vector.Values, FieldsJSON: string(fieldsJSON)}
	}
	physical, err := engine.BuildNativeSearchIndexPlan(engine.NativeIndexPlanInput{Request: input.Request, Identity: identity, BackendVersion: input.IndexIdentity.LadybugBackendVersion, EmbeddingDimensions: input.EmbeddingProfile.Dimensions, Documents: documents, PreviousContentHashes: input.PreviousContentHashes, FullRebuild: input.FullRebuild})
	if err != nil {
		return port.ExecutionPlan{}, ErrInvalidNativeSearchRequest
	}
	payload, err := json.Marshal(physical)
	if err != nil {
		return port.ExecutionPlan{}, err
	}
	return nativePlan(port.PlanSearchIndex, payload, 1, 4096), nil
}

func (e *Adapter) PrepareSearch(_ context.Context, input port.SearchPreparationInput) (port.PreparedSearch, error) {
	if !e.available() {
		return port.PreparedSearch{}, ErrInvalidNativeSearchRequest
	}
	prepared, err := engine.BuildNativeSearchPlan(engine.NativeSearchPlanInput{Request: input.Request, QueryEmbedding: input.QueryEmbedding, LexicalLimit: input.SearchProfile.LexicalCandidateLimit, SemanticLimit: input.SearchProfile.SemanticCandidateLimit, MaxOutputBytes: input.MaxOutputBytes})
	if err != nil {
		return port.PreparedSearch{}, ErrInvalidNativeSearchRequest
	}
	payload, _ := json.Marshal(prepared.Plan)
	plan := nativePlan(port.PlanSearch, payload, prepared.MaxRows, input.MaxOutputBytes)
	rrfK := input.SearchProfile.RRFK
	if rrfK <= 0 {
		rrfK = 60
	}
	lexicalWeight, semanticWeight := input.SearchProfile.LexicalWeight, input.SearchProfile.SemanticWeight
	if lexicalWeight == 0 && semanticWeight == 0 {
		lexicalWeight, semanticWeight = 1, 1
	}
	snippetMaxBytes := input.SearchProfile.SnippetMaxBytes
	if snippetMaxBytes <= 0 {
		snippetMaxBytes = 256
	}
	return port.PreparedSearch{Plan: plan, QueryDigest: prepared.QueryDigest, Mode: prepared.Mode, MaxHits: input.SearchProfile.MaxHits, RRFK: rrfK, LexicalWeight: lexicalWeight, SemanticWeight: semanticWeight, QueryText: prepared.QueryText, SnippetMaxBytes: snippetMaxBytes}, nil
}

func (e *Adapter) PrepareQuery(_ context.Context, input port.BoundExecutionRequest) (port.ExecutionPlan, error) {
	if !e.available() || input.MaxOutputBytes <= 0 {
		return port.ExecutionPlan{}, ErrInvalidNativeSearchRequest
	}
	physical, maxRows, err := engine.BuildNativeQueryPlan(input.Request)
	if err != nil {
		return port.ExecutionPlan{}, ErrInvalidNativeSearchRequest
	}
	payload, _ := json.Marshal(physical)
	return nativePlan(port.PlanQuery, payload, maxRows, input.MaxOutputBytes), nil
}

func (e *Adapter) PrepareAnalysis(_ context.Context, input port.BoundExecutionRequest) (port.ExecutionPlan, error) {
	if !e.available() || input.MaxOutputBytes <= 0 {
		return port.ExecutionPlan{}, ErrInvalidNativeSearchRequest
	}
	physical, maxRows, err := engine.BuildNativeAnalysisPlan(input.Request)
	if err != nil {
		return port.ExecutionPlan{}, ErrInvalidNativeSearchRequest
	}
	payload, _ := json.Marshal(physical)
	return nativePlan(port.PlanAnalysis, payload, maxRows, input.MaxOutputBytes), nil
}

func (e *Adapter) CompleteSearch(_ context.Context, input port.CompleteSearchInput) ([]byte, error) {
	if !input.Rows.Complete || input.Rows.Truncated {
		return nil, ErrInvalidNativeSearchRequest
	}
	rows := make([]engine.SearchCandidateRow, len(input.Rows.Rows))
	for index, row := range input.Rows.Rows {
		rows[index] = engine.SearchCandidateRow{Signal: row["signal"].Value, Address: row["address"].Value, Kind: row["kind"].Value, Owner: row["owner"].Value, GraphEntries: row["graph_entries"].Value, TypeAddresses: row["type_addresses"].Value, LayerAddresses: row["layer_addresses"].Value, ContentHash: row["content_hash"].Value, Fields: row["fields"].Value, Score: row["score"].Value}
	}
	result, err := engine.CompleteSearchResult(engine.SearchCompletion{Mode: input.Prepared.Mode, QueryDigest: input.Prepared.QueryDigest, QueryText: input.Prepared.QueryText, MaxHits: input.Prepared.MaxHits, RRFK: input.Prepared.RRFK, LexicalWeight: input.Prepared.LexicalWeight, SemanticWeight: input.Prepared.SemanticWeight, Offset: input.Prepared.Offset, NextCursor: input.Prepared.NextCursor, SnippetMaxBytes: input.Prepared.SnippetMaxBytes, Rows: rows})
	if err != nil {
		return nil, ErrInvalidNativeSearchRequest
	}
	return result, nil
}
func (e *Adapter) CompleteQuery(_ context.Context, input port.CompleteExecutionInput) ([]byte, error) {
	if !input.Rows.Complete || input.Rows.Truncated || input.Rows.Bytes < 0 {
		return nil, ErrInvalidNativeSearchRequest
	}
	rows := make([]engine.QueryRow, len(input.Rows.Rows))
	for index, row := range input.Rows.Rows {
		rows[index] = make(engine.QueryRow, len(row))
		for key, value := range row {
			rows[index][key] = engine.QueryValue{Kind: value.Kind, Value: value.Value}
		}
	}
	return engine.CompleteQueryResult(rows)
}
func (e *Adapter) CompleteAnalysis(_ context.Context, input port.CompleteExecutionInput) ([]byte, error) {
	if !input.Rows.Complete || input.Rows.Truncated {
		return nil, ErrInvalidNativeSearchRequest
	}
	values := make([]engine.AnalysisValue, 0, len(input.Rows.Rows))
	for _, row := range input.Rows.Rows {
		if row["address"].Value == "" || row["metric_name"].Value == "" || row["metric_value"].Value == "" {
			return nil, ErrInvalidNativeSearchRequest
		}
		values = append(values, engine.AnalysisValue{Address: row["address"].Value, MetricName: row["metric_name"].Value, TypedValue: row["metric_value"].Value})
	}
	return engine.CompleteAnalysisResult(values)
}

func nativePlan(kind port.PlanKind, payload []byte, maxRows, maxBytes int) port.ExecutionPlan {
	digest := sha256.Sum256(append([]byte(kind+"\x00"), payload...))
	return port.ExecutionPlan{Kind: kind, PlanID: "plan-" + hex.EncodeToString(digest[:16]), ProtocolVersion: "v1", Payload: payload, MaxRows: maxRows, MaxBytes: maxBytes}
}

func validSearchSnapshot(snapshot port.DocumentSnapshotRef) bool {
	host := snapshot.Kind == port.SnapshotHostRevision && snapshot.HostDocumentID != "" && snapshot.CommittedRevision != "" && snapshot.SourceTreeDigest == "" && snapshot.DocumentGeneration == 0
	portable := snapshot.Kind == port.SnapshotPortableGeneration && snapshot.HostDocumentID == "" && snapshot.CommittedRevision == "" && snapshot.SourceTreeDigest != ""
	return (host || portable) && snapshot.DefinitionHash != ""
}
