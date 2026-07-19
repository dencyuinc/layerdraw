// SPDX-License-Identifier: LicenseRef-LayerDraw-1.0

package runtime

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math"
	"strings"

	"github.com/dencyuinc/layerdraw/internal/runtime/port"
)

var (
	ErrSearchInvalidRequest       = errors.New("search.invalid_request")
	ErrSearchIndexNotReady        = errors.New("search.index_not_ready")
	ErrSearchIndexStale           = errors.New("search.index_stale")
	ErrSearchEmbeddingUnavailable = errors.New("search.embedding_unavailable")
	ErrSearchEmbeddingProfile     = errors.New("search.embedding_profile_mismatch")
	ErrSearchCapabilityMissing    = errors.New("search.capability_missing")
	ErrSearchBackendFailed        = errors.New("search.backend_failed")
	ErrSearchCancelled            = errors.New("search.cancelled")
	ErrSearchInvalidCursor        = errors.New("search.cursor_invalid")
	ErrAnalysisInvalidScope       = errors.New("analysis.invalid_scope")
)

type SearchCapabilityManifest struct {
	QueryAvailable     bool
	SearchAvailable    bool
	AnalysisAvailable  bool
	EmbeddingAvailable bool
	EmbeddingReason    string
	Adapter            port.QueryAdapterCapability
}

type SearchService struct {
	engine        port.SearchEngine
	executor      port.QueryExecutionPort
	indexes       port.SearchIndexStore
	embedder      port.EmbeddingProvider
	batchVerifier port.SearchDocumentBatchVerifier
	cursorKey     []byte
}

func NewVerifiedSearchService(engine port.SearchEngine, executor port.QueryExecutionPort, indexes port.SearchIndexStore, embedder port.EmbeddingProvider, verifier port.SearchDocumentBatchVerifier) *SearchService {
	return &SearchService{engine: engine, executor: executor, indexes: indexes, embedder: embedder, batchVerifier: verifier}
}

func NewVerifiedSearchServiceWithCursorAuthority(engine port.SearchEngine, executor port.QueryExecutionPort, indexes port.SearchIndexStore, embedder port.EmbeddingProvider, verifier port.SearchDocumentBatchVerifier, key []byte) *SearchService {
	service := NewVerifiedSearchService(engine, executor, indexes, embedder, verifier)
	if len(key) >= 16 {
		service.cursorKey = append([]byte(nil), key...)
	}
	return service
}

func NewSearchService(engine port.SearchEngine, executor port.QueryExecutionPort, indexes port.SearchIndexStore, embedder port.EmbeddingProvider) *SearchService {
	return &SearchService{engine: engine, executor: executor, indexes: indexes, embedder: embedder}
}

func (s *SearchService) Capabilities(ctx context.Context) (SearchCapabilityManifest, error) {
	if s.engine == nil || s.executor == nil || s.indexes == nil {
		return SearchCapabilityManifest{}, fmt.Errorf("%w: search composition is incomplete", ErrSearchCapabilityMissing)
	}
	capability, err := s.executor.Capabilities(ctx)
	if err != nil {
		return SearchCapabilityManifest{}, fmt.Errorf("%w: adapter capability unavailable", ErrSearchBackendFailed)
	}
	has := func(required ...port.SearchPrimitive) bool {
		for _, value := range required {
			found := false
			for _, primitive := range capability.Primitives {
				found = found || primitive == value
			}
			if !found {
				return false
			}
		}
		return true
	}
	manifest := SearchCapabilityManifest{
		QueryAvailable:    has(port.PrimitiveStructuralMatch, port.PrimitiveTypedPredicate),
		SearchAvailable:   has(port.PrimitiveFTSBM25),
		AnalysisAvailable: has(port.PrimitivePageRank, port.PrimitiveKCore, port.PrimitiveLouvain, port.PrimitiveSCC, port.PrimitiveWCC),
		Adapter:           capability,
	}
	if s.embedder == nil {
		manifest.EmbeddingReason = "embedding provider is not configured"
		return manifest, nil
	}
	embedding, err := s.embedder.Describe(ctx)
	if err != nil || !embedding.Available {
		manifest.EmbeddingReason = "embedding provider is unavailable"
		return manifest, nil
	}
	manifest.EmbeddingAvailable = has(port.PrimitiveVectorHNSW, port.PrimitiveVectorFiltered)
	if !manifest.EmbeddingAvailable {
		manifest.EmbeddingReason = "native adapter does not provide vector primitives"
	}
	return manifest, nil
}

type SearchRequest struct {
	Snapshot               port.DocumentSnapshotRef
	AccessProjectionDigest string
	SearchProfile          port.SearchProfile
	EmbeddingProfile       *port.EmbeddingProfile
	IndexIdentity          port.SearchIndexIdentity
	Mode                   string
	QueryText              string
	EngineRequest          []byte
	MaxOutputBytes         int
	Cursor                 string
}

type searchCursorPayload struct {
	Version, QueryDigest, IndexDigest, EmbeddingDigest, AccessDigest string
	Snapshot                                                         port.DocumentSnapshotRef
	Offset                                                           int
}

type SearchIndexBuildRequest struct {
	Snapshot               port.DocumentSnapshotRef
	AccessProjectionDigest string
	SearchProfile          port.SearchProfile
	EmbeddingProfile       *port.EmbeddingProfile
	IndexIdentity          port.SearchIndexIdentity
	Batch                  port.SearchDocumentBatch
	EngineRequest          []byte
}

// RebuildIndex stages and atomically activates only an Engine-planned index.
// Documents must already be Access-filtered SearchDocuments; the provider sees
// no source tree, policy, or unfiltered corpus. Content hashes let Engine emit
// an incremental physical plan without moving diff semantics into the store.
func (s *SearchService) RebuildIndex(ctx context.Context, input SearchIndexBuildRequest) (port.SearchIndexStatus, error) {
	validation := SearchRequest{Snapshot: input.Snapshot, AccessProjectionDigest: input.AccessProjectionDigest, SearchProfile: input.SearchProfile, EmbeddingProfile: input.EmbeddingProfile, IndexIdentity: input.IndexIdentity, Mode: "lexical", QueryText: "index-build", MaxOutputBytes: 1}
	if err := validateSearchRequest(validation); err != nil {
		return port.SearchIndexStatus{}, err
	}
	if s.engine == nil || s.indexes == nil {
		return port.SearchIndexStatus{}, ErrSearchCapabilityMissing
	}
	var embeddings []port.EmbeddingVector
	var err error
	if input.EmbeddingProfile != nil {
		if s.embedder == nil || s.batchVerifier == nil {
			return port.SearchIndexStatus{}, ErrSearchEmbeddingUnavailable
		}
		if input.Batch.Snapshot != input.Snapshot || input.Batch.AccessProjectionDigest != input.AccessProjectionDigest || input.Batch.EmbeddingProfileDigest != input.EmbeddingProfile.ModelDigest {
			return port.SearchIndexStatus{}, ErrSearchEmbeddingProfile
		}
		if err := s.batchVerifier.VerifySearchDocumentBatch(ctx, input.Batch); err != nil {
			return port.SearchIndexStatus{}, ErrSearchEmbeddingProfile
		}
		embeddings, err = s.embedder.EmbedDocuments(ctx, *input.EmbeddingProfile, input.Batch)
		if err != nil {
			if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
				return port.SearchIndexStatus{}, ErrSearchCancelled
			}
			return port.SearchIndexStatus{}, ErrSearchEmbeddingUnavailable
		}
		if len(embeddings) != len(input.Batch.Documents) {
			return port.SearchIndexStatus{}, ErrSearchEmbeddingProfile
		}
		seen := map[string]bool{}
		for index, vector := range embeddings {
			document := input.Batch.Documents[index]
			if vector.SubjectAddress != document.SubjectAddress || vector.ContentHash != document.ContentHash || seen[vector.SubjectAddress] || len(vector.Values) != input.EmbeddingProfile.Dimensions {
				return port.SearchIndexStatus{}, ErrSearchEmbeddingProfile
			}
			seen[vector.SubjectAddress] = true
			for _, value := range vector.Values {
				if math.IsNaN(float64(value)) || math.IsInf(float64(value), 0) {
					return port.SearchIndexStatus{}, ErrSearchEmbeddingProfile
				}
			}
		}
	}
	previousHashes := map[string]string{}
	fullRebuild := true
	manifestStore, supportsManifest := s.indexes.(port.SearchIndexManifestStore)
	if supportsManifest {
		previousHashes, err = manifestStore.PreviousDocumentHashes(ctx, input.IndexIdentity)
		if err != nil && !errors.Is(err, port.ErrNotFound) {
			return port.SearchIndexStatus{}, fmt.Errorf("%w: index manifest unavailable", ErrSearchBackendFailed)
		}
		fullRebuild = err != nil
	}
	plan, err := s.engine.PrepareSearchIndex(ctx, port.SearchIndexPreparationInput{Snapshot: input.Snapshot, AccessProjectionDigest: input.AccessProjectionDigest, SearchProfile: input.SearchProfile, EmbeddingProfile: input.EmbeddingProfile, IndexIdentity: input.IndexIdentity, Batch: input.Batch, Embeddings: embeddings, Request: append([]byte(nil), input.EngineRequest...), PreviousContentHashes: previousHashes, FullRebuild: fullRebuild})
	if err != nil {
		return port.SearchIndexStatus{}, ErrSearchInvalidRequest
	}
	staged, err := s.indexes.ApplyPlan(ctx, input.IndexIdentity, plan)
	if err != nil {
		return port.SearchIndexStatus{}, fmt.Errorf("%w: index build failed", ErrSearchBackendFailed)
	}
	status, err := s.indexes.Activate(ctx, staged)
	if err != nil {
		return port.SearchIndexStatus{}, fmt.Errorf("%w: index activation failed", ErrSearchBackendFailed)
	}
	if supportsManifest {
		currentHashes := make(map[string]string, len(input.Batch.Documents))
		for _, document := range input.Batch.Documents {
			currentHashes[document.SubjectAddress] = document.ContentHash
		}
		if err := manifestStore.RecordDocumentHashes(ctx, input.IndexIdentity, currentHashes); err != nil {
			_ = s.indexes.Invalidate(ctx, input.IndexIdentity)
			return port.SearchIndexStatus{}, fmt.Errorf("%w: index manifest activation failed", ErrSearchBackendFailed)
		}
	}
	return status, nil
}

func (s *SearchService) Search(ctx context.Context, input SearchRequest) ([]byte, error) {
	if err := validateSearchRequest(input); err != nil {
		return nil, err
	}
	if s.engine == nil || s.executor == nil || s.indexes == nil {
		return nil, fmt.Errorf("%w: search composition is incomplete", ErrSearchCapabilityMissing)
	}
	status, err := s.indexes.Describe(ctx, input.IndexIdentity)
	if err != nil {
		if errors.Is(err, port.ErrNotFound) {
			return nil, ErrSearchIndexNotReady
		}
		return nil, fmt.Errorf("%w: index status unavailable", ErrSearchBackendFailed)
	}
	if status.State != "active" {
		return nil, ErrSearchIndexNotReady
	}
	if status.Identity != input.IndexIdentity {
		return nil, ErrSearchIndexStale
	}
	queryHash := sha256.New()
	_, _ = queryHash.Write(input.EngineRequest)
	_, _ = queryHash.Write([]byte{0})
	_, _ = io.WriteString(queryHash, input.QueryText)
	queryDigest := "sha256:" + hex.EncodeToString(queryHash.Sum(nil))
	identityBytes, err := json.Marshal(input.IndexIdentity)
	if err != nil {
		return nil, ErrSearchInvalidRequest
	}
	identityDigestBytes := sha256.Sum256(identityBytes)
	binding := searchCursorPayload{Version: "v1", QueryDigest: queryDigest, IndexDigest: "sha256:" + hex.EncodeToString(identityDigestBytes[:]), EmbeddingDigest: input.IndexIdentity.EmbeddingProfileDigest, AccessDigest: input.AccessProjectionDigest, Snapshot: input.Snapshot}
	offset := 0
	if input.Cursor != "" {
		if len(s.cursorKey) == 0 {
			return nil, ErrSearchInvalidCursor
		}
		decoded, cursorErr := decodeSearchCursor(s.cursorKey, input.Cursor)
		if cursorErr != nil || decoded.Version != binding.Version || decoded.QueryDigest != binding.QueryDigest || decoded.IndexDigest != binding.IndexDigest || decoded.EmbeddingDigest != binding.EmbeddingDigest || decoded.AccessDigest != binding.AccessDigest || decoded.Snapshot != binding.Snapshot || decoded.Offset <= 0 {
			return nil, ErrSearchInvalidCursor
		}
		offset = decoded.Offset
	}
	var queryEmbedding []float32
	if input.Mode == "semantic" || input.Mode == "hybrid" {
		if s.embedder == nil || input.EmbeddingProfile == nil {
			return nil, ErrSearchEmbeddingUnavailable
		}
		queryEmbedding, err = s.embedder.EmbedQuery(ctx, *input.EmbeddingProfile, input.QueryText)
		if err != nil {
			if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
				return nil, ErrSearchCancelled
			}
			return nil, ErrSearchEmbeddingUnavailable
		}
		if len(queryEmbedding) != input.EmbeddingProfile.Dimensions || !finiteVector(queryEmbedding) {
			return nil, ErrSearchEmbeddingProfile
		}
	}
	prepared, err := s.engine.PrepareSearch(ctx, port.SearchPreparationInput{
		BoundExecutionRequest: port.BoundExecutionRequest{Snapshot: input.Snapshot, AccessProjectionDigest: input.AccessProjectionDigest, Request: input.EngineRequest, MaxOutputBytes: input.MaxOutputBytes},
		SearchProfile:         input.SearchProfile, EmbeddingProfile: input.EmbeddingProfile, IndexIdentity: input.IndexIdentity, QueryEmbedding: queryEmbedding,
	})
	if err != nil {
		return nil, fmt.Errorf("%w: Engine rejected search", ErrSearchInvalidRequest)
	}
	prepared.Offset = offset
	if len(s.cursorKey) != 0 {
		binding.Offset = offset + input.SearchProfile.MaxHits
		prepared.NextCursor, err = encodeSearchCursor(s.cursorKey, binding)
		if err != nil {
			return nil, ErrSearchInvalidCursor
		}
	}
	rows, err := s.executor.Execute(ctx, prepared.Plan)
	if err != nil {
		return nil, normalizeExecutionError(err)
	}
	result, err := s.engine.CompleteSearch(ctx, port.CompleteSearchInput{Prepared: prepared, Rows: rows})
	if err != nil {
		return nil, fmt.Errorf("%w: Engine rejected adapter rows", ErrSearchBackendFailed)
	}
	if len(result) > input.MaxOutputBytes {
		return nil, fmt.Errorf("%w: Engine result exceeded bound", ErrSearchInvalidRequest)
	}
	return result, nil
}

func encodeSearchCursor(key []byte, payload searchCursorPayload) (string, error) {
	data, err := json.Marshal(payload)
	if err != nil {
		return "", err
	}
	mac := hmac.New(sha256.New, key)
	_, _ = mac.Write([]byte("layerdraw-search-cursor-v1\x00"))
	_, _ = mac.Write(data)
	return base64.RawURLEncoding.EncodeToString(data) + "." + base64.RawURLEncoding.EncodeToString(mac.Sum(nil)), nil
}

func decodeSearchCursor(key []byte, token string) (searchCursorPayload, error) {
	var payload searchCursorPayload
	parts := strings.Split(token, ".")
	if len(parts) != 2 {
		return payload, ErrSearchInvalidCursor
	}
	data, err := base64.RawURLEncoding.DecodeString(parts[0])
	if err != nil {
		return payload, ErrSearchInvalidCursor
	}
	signature, err := base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil {
		return payload, ErrSearchInvalidCursor
	}
	mac := hmac.New(sha256.New, key)
	_, _ = mac.Write([]byte("layerdraw-search-cursor-v1\x00"))
	_, _ = mac.Write(data)
	if !hmac.Equal(signature, mac.Sum(nil)) {
		return payload, ErrSearchInvalidCursor
	}
	decoder := json.NewDecoder(strings.NewReader(string(data)))
	decoder.DisallowUnknownFields()
	if decoder.Decode(&payload) != nil || decoder.Decode(&struct{}{}) != io.EOF {
		return payload, ErrSearchInvalidCursor
	}
	return payload, nil
}

func finiteVector(values []float32) bool {
	for _, value := range values {
		if math.IsNaN(float64(value)) || math.IsInf(float64(value), 0) {
			return false
		}
	}
	return true
}

func (s *SearchService) ExecuteQuery(ctx context.Context, input port.BoundExecutionRequest) ([]byte, error) {
	if s.engine == nil || s.executor == nil || input.MaxOutputBytes <= 0 {
		return nil, ErrSearchInvalidRequest
	}
	plan, err := s.engine.PrepareQuery(ctx, input)
	if err != nil {
		return nil, ErrSearchInvalidRequest
	}
	rows, err := s.executor.Execute(ctx, plan)
	if err != nil {
		return nil, normalizeExecutionError(err)
	}
	result, err := s.engine.CompleteQuery(ctx, port.CompleteExecutionInput{Plan: plan, Rows: rows})
	if err == nil && len(result) > input.MaxOutputBytes {
		return nil, ErrSearchInvalidRequest
	}
	return result, err
}

func (s *SearchService) ExecuteAnalysis(ctx context.Context, input port.BoundExecutionRequest) ([]byte, error) {
	if s.engine == nil || s.executor == nil || input.MaxOutputBytes <= 0 {
		return nil, ErrAnalysisInvalidScope
	}
	plan, err := s.engine.PrepareAnalysis(ctx, input)
	if err != nil {
		return nil, ErrAnalysisInvalidScope
	}
	rows, err := s.executor.Execute(ctx, plan)
	if err != nil {
		return nil, normalizeExecutionError(err)
	}
	result, err := s.engine.CompleteAnalysis(ctx, port.CompleteExecutionInput{Plan: plan, Rows: rows})
	if err == nil && len(result) > input.MaxOutputBytes {
		return nil, ErrAnalysisInvalidScope
	}
	return result, err
}

func validateSearchRequest(input SearchRequest) error {
	if input.QueryText == "" || input.MaxOutputBytes <= 0 || input.SearchProfile.ProfileID == "" || input.SearchProfile.SpecificationDigest == "" || input.AccessProjectionDigest == "" || input.IndexIdentity.EmbeddingProfileID == "" || input.IndexIdentity.EmbeddingProfileDigest == "" || input.IndexIdentity.LadybugBackendVersion == "" || input.IndexIdentity.IndexSchemaVersion == "" {
		return ErrSearchInvalidRequest
	}
	snapshot := input.Snapshot
	validHost := snapshot.Kind == port.SnapshotHostRevision && snapshot.HostDocumentID != "" && snapshot.CommittedRevision != "" && snapshot.SourceTreeDigest == "" && snapshot.DocumentGeneration == 0
	validPortable := snapshot.Kind == port.SnapshotPortableGeneration && snapshot.HostDocumentID == "" && snapshot.CommittedRevision == "" && snapshot.SourceTreeDigest != ""
	if (!validHost && !validPortable) || snapshot.DefinitionHash == "" || input.SearchProfile.MaxHits <= 0 || input.SearchProfile.LexicalCandidateLimit < input.SearchProfile.MaxHits || input.SearchProfile.SemanticCandidateLimit < input.SearchProfile.MaxHits {
		return ErrSearchInvalidRequest
	}
	if input.Mode != "lexical" && input.Mode != "semantic" && input.Mode != "hybrid" {
		return ErrSearchInvalidRequest
	}
	if (input.Mode == "semantic" || input.Mode == "hybrid") && input.EmbeddingProfile == nil {
		return ErrSearchInvalidRequest
	}
	if input.IndexIdentity.DocumentSnapshotRef != input.Snapshot || input.IndexIdentity.AccessProjectionDigest != input.AccessProjectionDigest || input.IndexIdentity.SearchProfileID != input.SearchProfile.ProfileID || input.IndexIdentity.SearchProfileDigest != input.SearchProfile.SpecificationDigest {
		return ErrSearchIndexStale
	}
	if input.EmbeddingProfile != nil && (input.IndexIdentity.EmbeddingProfileID != input.EmbeddingProfile.ProfileID || input.IndexIdentity.EmbeddingProfileDigest != input.EmbeddingProfile.ModelDigest) {
		return ErrSearchEmbeddingProfile
	}
	return nil
}

func normalizeExecutionError(err error) error {
	if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
		return ErrSearchCancelled
	}
	return fmt.Errorf("%w: adapter execution failed", ErrSearchBackendFailed)
}
