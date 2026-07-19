// SPDX-License-Identifier: LicenseRef-LayerDraw-1.0

package port

import (
	"context"
	"time"
)

// Search primitives are intentionally physical. Plans and rows are produced
// and consumed by Engine; adapters must not interpret LDL, ranking, graph
// meaning, StableAddress identity, or Access policy.
type SearchPrimitive string

const (
	PrimitiveStructuralMatch SearchPrimitive = "structural_match"
	PrimitiveTypedPredicate  SearchPrimitive = "typed_predicate"
	PrimitiveTraversal       SearchPrimitive = "directed_traversal"
	PrimitiveShortestPath    SearchPrimitive = "shortest_path"
	PrimitiveFTSBM25         SearchPrimitive = "fts_bm25"
	PrimitiveVectorHNSW      SearchPrimitive = "vector_hnsw"
	PrimitiveVectorFiltered  SearchPrimitive = "vector_filtered_search"
	PrimitivePageRank        SearchPrimitive = "algo_page_rank"
	PrimitiveKCore           SearchPrimitive = "algo_k_core"
	PrimitiveLouvain         SearchPrimitive = "algo_louvain"
	PrimitiveSCC             SearchPrimitive = "algo_scc"
	PrimitiveWCC             SearchPrimitive = "algo_wcc"
)

var RequiredSearchPrimitives = []SearchPrimitive{
	PrimitiveStructuralMatch, PrimitiveTypedPredicate, PrimitiveTraversal,
	PrimitiveShortestPath, PrimitiveFTSBM25, PrimitiveVectorHNSW,
	PrimitiveVectorFiltered, PrimitivePageRank, PrimitiveKCore,
	PrimitiveLouvain, PrimitiveSCC, PrimitiveWCC,
}

type QueryAdapterCapability struct {
	AdapterID           string
	Backend             string
	BackendVersion      string
	PlanProtocolVersion string
	Primitives          []SearchPrimitive
	MaxRows             int
	MaxBytes            int
}

type PlanKind string

const (
	PlanQuery       PlanKind = "query"
	PlanSearch      PlanKind = "search"
	PlanAnalysis    PlanKind = "analysis"
	PlanSearchIndex PlanKind = "search_index"
)

// ExecutionPlan is an opaque, Engine-issued parameterized plan. Payload is
// never accepted from a UI/MCP surface without Engine token verification.
type ExecutionPlan struct {
	Kind            PlanKind
	PlanID          string
	ProtocolVersion string
	Token           string
	Payload         []byte
	MaxRows         int
	MaxBytes        int
}

type RawValue struct {
	Kind  string
	Value string
}

type RawRow map[string]RawValue

type ExecutionResult struct {
	Rows          []RawRow
	Truncated     bool
	Complete      bool
	Bytes         int
	PhysicalIndex *PhysicalIndexRef
}

type ExecutionLimits struct{ MaxRows, MaxBytes int }
type RowSink interface{ Push(RawRow) error }
type PhysicalIndexRef struct {
	IdentityDigest string `json:"identity_digest"`
	ContentDigest  string `json:"content_digest"`
	BackendVersion string `json:"backend_version"`
}
type PhysicalIndexInspector interface {
	InspectPhysicalIndex(context.Context, PhysicalIndexRef) error
}

type QueryExecutionPort interface {
	Capabilities(context.Context) (QueryAdapterCapability, error)
	Execute(context.Context, ExecutionPlan) (ExecutionResult, error)
	Cancel(context.Context, string) error
}

type SnapshotKind string

const (
	SnapshotHostRevision       SnapshotKind = "host_revision"
	SnapshotPortableGeneration SnapshotKind = "portable_generation"
)

type DocumentSnapshotRef struct {
	Kind               SnapshotKind `json:"kind"`
	HostDocumentID     string       `json:"host_document_id,omitempty"`
	CommittedRevision  string       `json:"committed_revision,omitempty"`
	SourceTreeDigest   string       `json:"source_tree_digest,omitempty"`
	DocumentGeneration uint64       `json:"document_generation,omitempty"`
	DefinitionHash     string       `json:"definition_hash"`
}

type SearchIndexIdentity struct {
	DocumentSnapshotRef    DocumentSnapshotRef `json:"document_snapshot_ref"`
	SearchProfileID        string              `json:"search_profile_id"`
	SearchProfileDigest    string              `json:"search_profile_digest"`
	EmbeddingProfileID     string              `json:"embedding_profile_id"`
	EmbeddingProfileDigest string              `json:"embedding_profile_digest"`
	AccessProjectionDigest string              `json:"access_projection_digest"`
	LadybugBackendVersion  string              `json:"ladybug_backend_version"`
	IndexSchemaVersion     string              `json:"index_schema_version"`
}

type SearchIndexStatus struct {
	Identity      SearchIndexIdentity `json:"identity"`
	State         string              `json:"state"`
	PlanID        string              `json:"plan_id,omitempty"`
	UpdatedAt     time.Time           `json:"updated_at"`
	PhysicalIndex *PhysicalIndexRef   `json:"physical_index,omitempty"`
}

type SearchIndexApplyResult struct {
	Identity      SearchIndexIdentity
	PlanID        string
	PhysicalIndex PhysicalIndexRef
}

type SearchIndexStore interface {
	Describe(context.Context, SearchIndexIdentity) (SearchIndexStatus, error)
	ApplyPlan(context.Context, SearchIndexIdentity, ExecutionPlan) (SearchIndexApplyResult, error)
	Activate(context.Context, SearchIndexApplyResult) (SearchIndexStatus, error)
	Invalidate(context.Context, SearchIndexIdentity) error
}

// SearchDocumentInput contains only Engine-produced, Access-filtered text.
// Providers never receive LDL, source trees, policy decisions, or corpus APIs.
type SearchDocumentInput struct {
	SubjectAddress      string
	SubjectKind         string
	OwnerAddress        string
	GraphEntryAddresses []string
	TypeAddresses       []string
	LayerAddresses      []string
	ContentHash         string
	LexicalText         string
	Text                string
}

// SearchDocumentBatch is opaque evidence issued after Engine generation and
// Access projection. Runtime verifies Token before any provider sees Text.
type SearchDocumentBatch struct {
	Snapshot               DocumentSnapshotRef
	AccessProjectionDigest string
	EmbeddingProfileDigest string
	Documents              []SearchDocumentInput
	Token                  string
}

type SearchDocumentBatchVerifier interface {
	VerifySearchDocumentBatch(context.Context, SearchDocumentBatch) error
}

// SearchDocumentBatchProducer is an Engine/Access-owned high-level issuer.
// Callers provide generation inputs; they never receive a primitive capable of
// signing an already-constructed SearchDocumentBatch.
type SearchDocumentBatchProducer interface {
	ProduceSearchDocumentBatch(context.Context, SearchDocumentBatchRequest) (SearchDocumentBatch, error)
}

type SearchDocumentBatchRequest struct {
	Snapshot               DocumentSnapshotRef
	AccessProjectionDigest string
	EmbeddingProfileDigest string
	Corpus                 SearchCorpusRef
}

// SearchCorpusRef identifies a retained Engine generation that was bound to a
// snapshot by the trusted document/Access pipeline. It contains no corpus text.
type SearchCorpusRef struct {
	EndpointInstanceID string
	DocumentHandle     string
	Generation         uint64
}

type EmbeddingProfile struct {
	ProfileID     string
	ModelID       string
	ModelVersion  string
	ModelDigest   string
	Dimensions    int
	Normalization string
	MaxInputBytes int
}

type EmbeddingCapability struct {
	ProviderID string
	Available  bool
	Remote     bool
	Profiles   []EmbeddingProfile
}

type EmbeddingVector struct {
	SubjectAddress string
	ContentHash    string
	Values         []float32
}

type EmbeddingProvider interface {
	Describe(context.Context) (EmbeddingCapability, error)
	EmbedDocuments(context.Context, EmbeddingProfile, SearchDocumentBatch) ([]EmbeddingVector, error)
	EmbedQuery(context.Context, EmbeddingProfile, string) ([]float32, error)
}

// SearchEngine is the only port allowed to interpret search/query/analysis
// requests and normalize raw rows into domain result bytes.
type SearchEngine interface {
	PrepareSearchIndex(context.Context, SearchIndexPreparationInput) (ExecutionPlan, error)
	PrepareSearch(context.Context, SearchPreparationInput) (PreparedSearch, error)
	CompleteSearch(context.Context, CompleteSearchInput) ([]byte, error)
	PrepareQuery(context.Context, BoundExecutionRequest) (ExecutionPlan, error)
	CompleteQuery(context.Context, CompleteExecutionInput) ([]byte, error)
	PrepareAnalysis(context.Context, BoundExecutionRequest) (ExecutionPlan, error)
	CompleteAnalysis(context.Context, CompleteExecutionInput) ([]byte, error)
}

type SearchIndexPreparationInput struct {
	Snapshot               DocumentSnapshotRef
	AccessProjectionDigest string
	SearchProfile          SearchProfile
	EmbeddingProfile       *EmbeddingProfile
	IndexIdentity          SearchIndexIdentity
	Batch                  SearchDocumentBatch
	Embeddings             []EmbeddingVector
	Request                []byte
}

type BoundExecutionRequest struct {
	Snapshot               DocumentSnapshotRef
	AccessProjectionDigest string
	Request                []byte
	MaxOutputBytes         int
}

type SearchPreparationInput struct {
	BoundExecutionRequest
	SearchProfile    SearchProfile
	EmbeddingProfile *EmbeddingProfile
	IndexIdentity    SearchIndexIdentity
	QueryEmbedding   []float32
}

type SearchProfile struct {
	ProfileID              string
	SpecificationDigest    string
	LexicalCandidateLimit  int
	SemanticCandidateLimit int
	MaxHits                int
	RRFK                   int
	LexicalWeight          float64
	SemanticWeight         float64
}

type PreparedSearch struct {
	Plan           ExecutionPlan
	QueryDigest    string
	Mode           string
	MaxHits        int
	RRFK           int
	LexicalWeight  float64
	SemanticWeight float64
}

type CompleteSearchInput struct {
	Prepared PreparedSearch
	Rows     ExecutionResult
}

type CompleteExecutionInput struct {
	Plan ExecutionPlan
	Rows ExecutionResult
}
