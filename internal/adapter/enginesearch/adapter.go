// SPDX-License-Identifier: LicenseRef-LayerDraw-1.0

package enginesearch

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"slices"
	"strconv"
	"strings"
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

type LocalAccessProjection struct {
	mu           sync.RWMutex
	legacyDigest string
	bindings     map[port.DocumentSnapshotRef]string
}

func NewLocalAccessProjection(digest string) (*LocalAccessProjection, error) {
	if digest == "" {
		return nil, ErrInvalidNativeSearchRequest
	}
	return &LocalAccessProjection{legacyDigest: digest, bindings: map[port.DocumentSnapshotRef]string{}}, nil
}
func NewSessionAccessProjection() *LocalAccessProjection {
	return &LocalAccessProjection{bindings: map[port.DocumentSnapshotRef]string{}}
}
func (p *LocalAccessProjection) BindSession(snapshot port.DocumentSnapshotRef, digest string) error {
	if p == nil || !validSearchSnapshot(snapshot) || digest == "" {
		return ErrInvalidNativeSearchRequest
	}
	p.mu.Lock()
	p.bindings[snapshot] = digest
	p.mu.Unlock()
	return nil
}
func (p *LocalAccessProjection) AuthorizesSearchProjection(snapshot port.DocumentSnapshotRef, digest string) bool {
	if p == nil || digest == "" {
		return false
	}
	p.mu.RLock()
	bound := p.bindings[snapshot]
	legacy := p.legacyDigest
	p.mu.RUnlock()
	return digest == bound || (bound == "" && digest == legacy)
}
func (*LocalAccessProjection) AllowSearchDocument(document engine.SearchDocument) bool {
	return document.SubjectAddress != "" && document.ContentHash != ""
}
func (*LocalAccessProjection) AllowSearchField(document engine.SearchDocument, field engine.SearchField) bool {
	return document.SubjectAddress != "" && field.FieldPath != "" && field.Text != ""
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
	if !e.available() || e.projection == nil || ctx == nil || ctx.Err() != nil || !validSearchSnapshot(request.Snapshot) || request.Corpus.EndpointInstanceID == "" || request.Corpus.DocumentHandle == "" || request.Corpus.Generation == 0 || !e.projection.AuthorizesSearchProjection(request.Snapshot, request.AccessProjectionDigest) {
		return port.SearchDocumentBatch{}, ErrInvalidNativeSearchRequest
	}
	e.mu.RLock()
	binding, bound := e.bindings[request.Corpus]
	e.mu.RUnlock()
	if !bound || binding.snapshot != request.Snapshot || binding.digest != request.AccessProjectionDigest {
		return port.SearchDocumentBatch{}, ErrInvalidNativeSearchRequest
	}
	corpus, err := e.engine.ReadSearchCorpus(ctx, engine.DocumentGeneration{DocumentHandle: engine.DocumentHandle{EndpointInstanceID: request.Corpus.EndpointInstanceID, Value: request.Corpus.DocumentHandle}, Value: request.Corpus.Generation}, e.projection)
	if err != nil {
		return port.SearchDocumentBatch{}, ErrInvalidNativeSearchRequest
	}
	documents := make([]port.SearchDocumentInput, len(corpus))
	seen := make(map[string]bool, len(documents))
	for index, document := range corpus {
		if document.SubjectAddress == "" || document.ContentHash == "" || document.LexicalText == "" || !utf8.ValidString(document.LexicalText) || !utf8.ValidString(document.Text) || seen[document.SubjectAddress] {
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
	if !e.available() || (input.EmbeddingProfile == nil && len(input.Embeddings) != 0) || (input.EmbeddingProfile != nil && len(input.Embeddings) != len(input.Batch.Documents)) {
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
		var vector port.EmbeddingVector
		if input.EmbeddingProfile != nil {
			var ok bool
			vector, ok = embeddings[document.SubjectAddress]
			if !ok || vector.ContentHash != document.ContentHash {
				return port.ExecutionPlan{}, ErrInvalidNativeSearchRequest
			}
		}
		fieldsJSON, marshalErr := json.Marshal(document.Fields)
		if marshalErr != nil {
			return port.ExecutionPlan{}, ErrInvalidNativeSearchRequest
		}
		physicalDigest := port.SearchDocumentPhysicalDigest(document, vector.Values)
		documents[index] = engine.NativeIndexDocument{SubjectAddress: document.SubjectAddress, SubjectKind: document.SubjectKind, OwnerAddress: document.OwnerAddress, ContentHash: document.ContentHash, PhysicalDigest: physicalDigest, LexicalText: document.LexicalText, GraphEntryAddresses: document.GraphEntryAddresses, TypeAddresses: document.TypeAddresses, LayerAddresses: document.LayerAddresses, Embedding: vector.Values, FieldsJSON: string(fieldsJSON)}
	}
	embeddingDimensions := 0
	if input.EmbeddingProfile != nil {
		embeddingDimensions = input.EmbeddingProfile.Dimensions
	}
	physical, err := engine.BuildNativeSearchIndexPlan(engine.NativeIndexPlanInput{Request: input.Request, Identity: identity, BackendVersion: input.IndexIdentity.LadybugBackendVersion, EmbeddingDimensions: embeddingDimensions, Documents: documents, PreviousContentHashes: input.PreviousContentHashes, FullRebuild: input.FullRebuild})
	if err != nil {
		return port.ExecutionPlan{}, ErrInvalidNativeSearchRequest
	}
	payload, err := json.Marshal(physical)
	if err != nil {
		return port.ExecutionPlan{}, err
	}
	return nativePlan(port.PlanSearchIndex, payload, 1, 4096, indexAuthority(input)), nil
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
	plan := nativePlan(port.PlanSearch, payload, prepared.MaxRows, input.MaxOutputBytes, searchAuthority(input))
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
	return nativePlan(port.PlanQuery, payload, maxRows, input.MaxOutputBytes, boundAuthority(input)), nil
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
	return nativePlan(port.PlanAnalysis, payload, maxRows, input.MaxOutputBytes, boundAuthority(input)), nil
}

func (e *Adapter) CompleteSearch(_ context.Context, input port.CompleteSearchInput) ([]byte, error) {
	if !input.Rows.Complete || input.Rows.Truncated {
		return nil, ErrInvalidNativeSearchRequest
	}
	rows := make([]engine.SearchCandidateRow, len(input.Rows.Rows))
	for index, row := range input.Rows.Rows {
		for _, key := range []string{"signal", "address", "kind", "owner", "graph_entries", "type_addresses", "layer_addresses", "content_hash"} {
			if row[key].Kind != "string" {
				return nil, ErrInvalidNativeSearchRequest
			}
		}
		if fields, present := row["fields"]; present && fields.Kind != "string" {
			return nil, ErrInvalidNativeSearchRequest
		}
		score := row["score"]
		parsedScore, parseErr := strconv.ParseFloat(score.Value, 64)
		if score.Kind != "float64" || parseErr != nil || math.IsNaN(parsedScore) || math.IsInf(parsedScore, 0) {
			return nil, ErrInvalidNativeSearchRequest
		}
		rows[index] = engine.SearchCandidateRow{Signal: row["signal"].Value, Address: row["address"].Value, Kind: row["kind"].Value, Owner: row["owner"].Value, GraphEntries: row["graph_entries"].Value, TypeAddresses: row["type_addresses"].Value, LayerAddresses: row["layer_addresses"].Value, ContentHash: row["content_hash"].Value, Fields: row["fields"].Value, Score: row["score"].Value}
	}
	result, err := engine.CompleteSearchResult(engine.SearchCompletion{DocumentSnapshotRef: resultSnapshot(input.IndexIdentity.DocumentSnapshotRef), IndexIdentityDigest: identityDigest(input.IndexIdentity), Mode: input.Prepared.Mode, QueryDigest: input.Prepared.QueryDigest, QueryText: input.Prepared.QueryText, MaxHits: input.Prepared.MaxHits, RRFK: input.Prepared.RRFK, LexicalWeight: input.Prepared.LexicalWeight, SemanticWeight: input.Prepared.SemanticWeight, Offset: input.Prepared.Offset, NextCursor: input.Prepared.NextCursor, SnippetMaxBytes: input.Prepared.SnippetMaxBytes, Rows: rows})
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
		if len(row) != 3 || row["address"].Kind != "string" || row["kind"].Kind != "string" || row["owner"].Kind != "string" {
			return nil, ErrInvalidNativeSearchRequest
		}
		rows[index] = make(engine.QueryRow, len(row))
		for key, value := range row {
			rows[index][key] = engine.QueryValue{Kind: value.Kind, Value: value.Value}
		}
	}
	var request struct {
		QueryAddress string                       `json:"query_address"`
		Arguments    map[string]engine.QueryValue `json:"arguments"`
	}
	if json.Unmarshal(input.Request, &request) != nil {
		return nil, ErrInvalidNativeSearchRequest
	}
	if request.QueryAddress == "" {
		request.QueryAddress = "ldl:runtime:query:" + strings.TrimPrefix(input.Plan.Authority.RequestDigest, "sha256:")
	}
	return engine.CompleteQueryResult(rows, engine.QueryCompletion{DocumentSnapshotRef: resultSnapshot(input.Plan.Authority.Snapshot), QueryAddress: request.QueryAddress, Arguments: request.Arguments, StatePolicy: "none", StateInput: engine.QueryResultStateInput{Kind: "none"}})
}
func (e *Adapter) CompleteAnalysis(_ context.Context, input port.CompleteExecutionInput) ([]byte, error) {
	if !input.Rows.Complete || input.Rows.Truncated {
		return nil, ErrInvalidNativeSearchRequest
	}
	values := make([]engine.AnalysisValue, 0, len(input.Rows.Rows))
	for _, row := range input.Rows.Rows {
		if row["metric_name"].Value == "scope_violation" {
			return nil, port.ErrInvalidScope
		}
		if row["address"].Value == "" || row["metric_name"].Value == "" || row["metric_value"].Value == "" {
			return nil, ErrInvalidNativeSearchRequest
		}
		values = append(values, engine.AnalysisValue{Address: row["address"].Value, MetricName: row["metric_name"].Value, TypedValue: row["metric_value"].Value})
	}
	var request struct {
		Algorithm          string   `json:"algorithm"`
		AlgorithmProfileID string   `json:"algorithm_profile_id"`
		QueryResultHash    string   `json:"query_result_hash"`
		EntityAddresses    []string `json:"entity_addresses"`
		RelationAddresses  []string `json:"relation_addresses"`
	}
	if json.Unmarshal(input.Request, &request) != nil {
		return nil, ErrInvalidNativeSearchRequest
	}
	if request.AlgorithmProfileID == "" {
		request.AlgorithmProfileID = "layerdraw.analysis." + request.Algorithm + ".v1"
	}
	return engine.CompleteAnalysisResult(values, engine.AnalysisCompletion{DocumentSnapshotRef: resultSnapshot(input.Plan.Authority.Snapshot), EntityAddresses: request.EntityAddresses, RelationAddresses: request.RelationAddresses, QueryResultHash: request.QueryResultHash, Algorithm: request.Algorithm, AlgorithmProfileID: request.AlgorithmProfileID, BackendVersion: input.BackendVersion})
}

func resultSnapshot(value port.DocumentSnapshotRef) engine.SearchResultSnapshotRef {
	return engine.SearchResultSnapshotRef{Kind: string(value.Kind), HostDocumentID: value.HostDocumentID, CommittedRevision: value.CommittedRevision, SourceTreeDigest: value.SourceTreeDigest, DocumentGeneration: value.DocumentGeneration, DefinitionHash: value.DefinitionHash}
}

func nativePlan(kind port.PlanKind, payload []byte, maxRows, maxBytes int, authority port.PlanAuthorityBinding) port.ExecutionPlan {
	bound, _ := json.Marshal(authority)
	hashInput := append([]byte(kind+"\x00"), bound...)
	hashInput = append(hashInput, 0)
	hashInput = append(hashInput, payload...)
	digest := sha256.Sum256(hashInput)
	return port.ExecutionPlan{Kind: kind, PlanID: "plan-" + hex.EncodeToString(digest[:16]), ProtocolVersion: "v1", Payload: payload, MaxRows: maxRows, MaxBytes: maxBytes, Authority: authority}
}

func digestBytes(value []byte) string {
	digest := sha256.Sum256(value)
	return "sha256:" + hex.EncodeToString(digest[:])
}

func identityDigest(identity port.SearchIndexIdentity) string {
	encoded, _ := json.Marshal(identity)
	return digestBytes(encoded)
}

func boundAuthority(input port.BoundExecutionRequest) port.PlanAuthorityBinding {
	return port.PlanAuthorityBinding{Snapshot: input.Snapshot, AccessProjectionDigest: input.AccessProjectionDigest, RequestDigest: digestBytes(input.Request)}
}

func searchAuthority(input port.SearchPreparationInput) port.PlanAuthorityBinding {
	authority := boundAuthority(input.BoundExecutionRequest)
	authority.SearchProfileID = input.SearchProfile.ProfileID
	authority.SearchProfileDigest = input.SearchProfile.SpecificationDigest
	authority.IndexIdentityDigest = identityDigest(input.IndexIdentity)
	if input.EmbeddingProfile != nil {
		authority.EmbeddingProfileID = input.EmbeddingProfile.ProfileID
		authority.EmbeddingProfileDigest = input.EmbeddingProfile.ModelDigest
	}
	return authority
}

func indexAuthority(input port.SearchIndexPreparationInput) port.PlanAuthorityBinding {
	authority := port.PlanAuthorityBinding{Snapshot: input.Snapshot, AccessProjectionDigest: input.AccessProjectionDigest, SearchProfileID: input.SearchProfile.ProfileID, SearchProfileDigest: input.SearchProfile.SpecificationDigest, IndexIdentityDigest: identityDigest(input.IndexIdentity), RequestDigest: digestBytes(input.Request)}
	if input.EmbeddingProfile != nil {
		authority.EmbeddingProfileID = input.EmbeddingProfile.ProfileID
		authority.EmbeddingProfileDigest = input.EmbeddingProfile.ModelDigest
	}
	return authority
}

func validSearchSnapshot(snapshot port.DocumentSnapshotRef) bool {
	host := snapshot.Kind == port.SnapshotHostRevision && snapshot.HostDocumentID != "" && snapshot.CommittedRevision != "" && snapshot.SourceTreeDigest == "" && snapshot.DocumentGeneration == 0
	portable := snapshot.Kind == port.SnapshotPortableGeneration && snapshot.HostDocumentID == "" && snapshot.CommittedRevision == "" && snapshot.SourceTreeDigest != ""
	return (host || portable) && snapshot.DefinitionHash != ""
}
