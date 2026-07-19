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
	"regexp"
	"slices"
	"strconv"
	"strings"
	"sync"
	"unicode/utf8"

	"github.com/dencyuinc/layerdraw/internal/engine"
	"github.com/dencyuinc/layerdraw/internal/runtime/port"
)

var ErrInvalidNativeSearchRequest = errors.New("Engine Search adapter: invalid request")
var stableScopeAddress = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9:._/-]*$`)

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

type nativeSearchExecutionRequest struct {
	Kind              string   `json:"kind"`
	Mode              string   `json:"mode,omitempty"`
	QueryText         string   `json:"query_text,omitempty"`
	TargetKind        string   `json:"target_kind,omitempty"`
	RootAddresses     []string `json:"root_addresses,omitempty"`
	Algorithm         string   `json:"algorithm,omitempty"`
	EntityAddresses   []string `json:"entity_addresses,omitempty"`
	RelationAddresses []string `json:"relation_addresses,omitempty"`
}

type nativeSearchIndexRequest struct {
	Kind string `json:"kind"`
}

type nativeLadybugPlan struct {
	Statements       []nativeLadybugStatement `json:"statements"`
	PhysicalIndex    *port.PhysicalIndexRef   `json:"physical_index,omitempty"`
	PhysicalEvidence []nativeIndexEvidence    `json:"physical_evidence,omitempty"`
}

type nativeLadybugStatement struct {
	Query      string                   `json:"query"`
	Parameters map[string]port.RawValue `json:"parameters"`
}

type nativeIndexEvidence struct {
	TableName       string   `json:"table_name"`
	IndexName       string   `json:"index_name"`
	IndexType       string   `json:"index_type"`
	PropertyNames   []string `json:"property_names"`
	ContentColumns  []string `json:"content_columns"`
	PrimaryKey      string   `json:"primary_key"`
	AllowNonPrimary bool     `json:"allow_non_primary,omitempty"`
	Relation        bool     `json:"relation,omitempty"`
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
		documents[index] = port.SearchDocumentInput{SubjectAddress: document.SubjectAddress, SubjectKind: document.SubjectKind, OwnerAddress: document.OwnerAddress, GraphEntryAddresses: append([]string(nil), document.GraphEntryAddresses...), TypeAddresses: append([]string(nil), document.TypeAddresses...), LayerAddresses: append([]string(nil), document.LayerAddresses...), ContentHash: document.ContentHash, LexicalText: document.LexicalText, Text: document.Text}
	}
	return port.SearchDocumentBatch{Snapshot: request.Snapshot, AccessProjectionDigest: request.AccessProjectionDigest, EmbeddingProfileDigest: request.EmbeddingProfileDigest, Documents: documents}, nil
}

func (e *Adapter) PrepareSearchIndex(_ context.Context, input port.SearchIndexPreparationInput) (port.ExecutionPlan, error) {
	var request nativeSearchIndexRequest
	if !e.available() {
		return port.ExecutionPlan{}, ErrInvalidNativeSearchRequest
	}
	if err := decodeClosedSearchJSON(input.Request, &request); err != nil || request.Kind != "build_search_index" || input.IndexIdentity.LadybugBackendVersion == "" || len(input.Batch.Documents) == 0 || input.EmbeddingProfile == nil || len(input.Embeddings) != len(input.Batch.Documents) {
		return port.ExecutionPlan{}, ErrInvalidNativeSearchRequest
	}
	identityJSON, err := json.Marshal(input.IndexIdentity)
	if err != nil {
		return port.ExecutionPlan{}, ErrInvalidNativeSearchRequest
	}
	identityDigest := sha256.Sum256(identityJSON)
	physicalIndex := port.PhysicalIndexRef{IdentityDigest: hex.EncodeToString(identityDigest[:]), BackendVersion: input.IndexIdentity.LadybugBackendVersion}
	embeddings := make(map[string]port.EmbeddingVector, len(input.Embeddings))
	for _, embedding := range input.Embeddings {
		if embedding.SubjectAddress == "" || len(embedding.Values) != input.EmbeddingProfile.Dimensions {
			return port.ExecutionPlan{}, ErrInvalidNativeSearchRequest
		}
		embeddings[embedding.SubjectAddress] = embedding
	}
	statements := []nativeLadybugStatement{
		{Query: fmt.Sprintf("CREATE NODE TABLE IF NOT EXISTS SearchDoc (id STRING, kind STRING, owner STRING, graph_entries STRING, type_addresses STRING, layer_addresses STRING, content_hash STRING, body STRING, embedding FLOAT[%d], PRIMARY KEY(id))", input.EmbeddingProfile.Dimensions), Parameters: map[string]port.RawValue{}},
		{Query: "CREATE NODE TABLE IF NOT EXISTS SearchNode (id STRING, PRIMARY KEY(id))", Parameters: map[string]port.RawValue{}},
		{Query: "CREATE REL TABLE IF NOT EXISTS SearchEdge (FROM SearchNode TO SearchNode, id STRING, from_id STRING, to_id STRING)", Parameters: map[string]port.RawValue{}},
		{Query: "MATCH ()-[r:SearchEdge]->() DELETE r", Parameters: map[string]port.RawValue{}},
		{Query: "MATCH (n:SearchNode) DELETE n", Parameters: map[string]port.RawValue{}},
		{Query: "MATCH (n:SearchDoc) DELETE n", Parameters: map[string]port.RawValue{}},
	}
	graphNodes := map[string]bool{}
	for _, document := range input.Batch.Documents {
		embedding, ok := embeddings[document.SubjectAddress]
		if !ok || embedding.ContentHash != document.ContentHash {
			return port.ExecutionPlan{}, ErrInvalidNativeSearchRequest
		}
		encoded, _ := json.Marshal(embedding.Values)
		graphEntries, _ := json.Marshal(document.GraphEntryAddresses)
		typeAddresses, _ := json.Marshal(document.TypeAddresses)
		layerAddresses, _ := json.Marshal(document.LayerAddresses)
		statements = append(statements, nativeLadybugStatement{Query: "CREATE (n:SearchDoc {id: $id, kind: $kind, owner: $owner, graph_entries: $graph_entries, type_addresses: $type_addresses, layer_addresses: $layer_addresses, content_hash: $content_hash, body: $body, embedding: $embedding})", Parameters: map[string]port.RawValue{"id": {Kind: "string", Value: document.SubjectAddress}, "kind": {Kind: "string", Value: document.SubjectKind}, "owner": {Kind: "string", Value: document.OwnerAddress}, "graph_entries": {Kind: "string", Value: string(graphEntries)}, "type_addresses": {Kind: "string", Value: string(typeAddresses)}, "layer_addresses": {Kind: "string", Value: string(layerAddresses)}, "content_hash": {Kind: "string", Value: document.ContentHash}, "body": {Kind: "string", Value: document.LexicalText}, "embedding": {Kind: "float32_array", Value: string(encoded)}}})
		for _, address := range document.GraphEntryAddresses {
			if !graphNodes[address] {
				graphNodes[address] = true
				statements = append(statements, nativeLadybugStatement{Query: "CREATE (n:SearchNode {id: $id})", Parameters: map[string]port.RawValue{"id": {Kind: "string", Value: address}}})
			}
		}
		if document.SubjectKind == "relation" && len(document.GraphEntryAddresses) == 2 {
			statements = append(statements, nativeLadybugStatement{Query: "MATCH (a:SearchNode {id: $from}), (b:SearchNode {id: $to}) CREATE (a)-[:SearchEdge {id: $id, from_id: $from, to_id: $to}]->(b)", Parameters: map[string]port.RawValue{"from": {Kind: "string", Value: document.GraphEntryAddresses[0]}, "to": {Kind: "string", Value: document.GraphEntryAddresses[1]}, "id": {Kind: "string", Value: document.SubjectAddress}}})
		}
	}
	statements = append(statements, nativeLadybugStatement{Query: "CALL CREATE_FTS_INDEX('SearchDoc', 'search_doc_fts', ['body'], stemmer := 'none')", Parameters: map[string]port.RawValue{}})
	statements = append(statements, nativeLadybugStatement{Query: "CALL CREATE_VECTOR_INDEX('SearchDoc', 'search_doc_vector', 'embedding', metric := 'cosine')", Parameters: map[string]port.RawValue{}})
	evidence := []nativeIndexEvidence{
		{TableName: "SearchDoc", IndexName: "search_doc_fts", IndexType: "FTS", PropertyNames: []string{"body"}, ContentColumns: []string{"id", "kind", "owner", "graph_entries", "type_addresses", "layer_addresses", "content_hash", "body", "embedding"}, PrimaryKey: "id"},
		{TableName: "SearchDoc", IndexName: "search_doc_vector", IndexType: "HNSW", PropertyNames: []string{"embedding"}, ContentColumns: []string{"id", "kind", "owner", "graph_entries", "type_addresses", "layer_addresses", "content_hash", "body", "embedding"}, PrimaryKey: "id"},
		{TableName: "SearchNode", ContentColumns: []string{"id"}, PrimaryKey: "id"},
		{TableName: "SearchEdge", ContentColumns: []string{"id", "from_id", "to_id"}, PrimaryKey: "id", AllowNonPrimary: true, Relation: true},
	}
	payload, err := json.Marshal(nativeLadybugPlan{Statements: statements, PhysicalIndex: &physicalIndex, PhysicalEvidence: evidence})
	if err != nil {
		return port.ExecutionPlan{}, err
	}
	return nativePlan(port.PlanSearchIndex, payload, 1, 4096), nil
}

func (e *Adapter) PrepareSearch(_ context.Context, input port.SearchPreparationInput) (port.PreparedSearch, error) {
	request, err := decodeExecutionRequest(input.Request, "search_documents")
	if !e.available() || err != nil || (request.Mode != "lexical" && request.Mode != "semantic" && request.Mode != "hybrid") || request.QueryText == "" || input.MaxOutputBytes <= 0 || ((request.Mode == "semantic" || request.Mode == "hybrid") && len(input.QueryEmbedding) == 0) || (request.Mode == "lexical" && len(input.QueryEmbedding) != 0) {
		return port.PreparedSearch{}, ErrInvalidNativeSearchRequest
	}
	statements := []nativeLadybugStatement{}
	if request.Mode == "lexical" || request.Mode == "hybrid" {
		query := "CALL QUERY_FTS_INDEX('SearchDoc', 'search_doc_fts', $query, TOP := $limit) "
		parameters := map[string]port.RawValue{"query": {Kind: "string", Value: request.QueryText}, "limit": {Kind: "int64", Value: fmt.Sprint(input.SearchProfile.LexicalCandidateLimit)}}
		if request.TargetKind != "" {
			query += "WHERE node.kind = $target_kind "
			parameters["target_kind"] = port.RawValue{Kind: "string", Value: request.TargetKind}
		}
		query += "RETURN 'lexical' AS signal, node.id AS address, node.kind AS kind, node.owner AS owner, node.graph_entries AS graph_entries, node.type_addresses AS type_addresses, node.layer_addresses AS layer_addresses, node.content_hash AS content_hash, score AS score ORDER BY score DESC, address"
		statements = append(statements, nativeLadybugStatement{Query: query, Parameters: parameters})
	}
	if request.Mode == "semantic" || request.Mode == "hybrid" {
		encoded, _ := json.Marshal(input.QueryEmbedding)
		query := "CALL QUERY_VECTOR_INDEX('SearchDoc', 'search_doc_vector', $embedding, $limit) "
		parameters := map[string]port.RawValue{"embedding": {Kind: "float32_array", Value: string(encoded)}, "limit": {Kind: "int64", Value: fmt.Sprint(input.SearchProfile.SemanticCandidateLimit)}}
		if request.TargetKind != "" {
			query += "WHERE node.kind = $target_kind "
			parameters["target_kind"] = port.RawValue{Kind: "string", Value: request.TargetKind}
		}
		query += "RETURN 'semantic' AS signal, node.id AS address, node.kind AS kind, node.owner AS owner, node.graph_entries AS graph_entries, node.type_addresses AS type_addresses, node.layer_addresses AS layer_addresses, node.content_hash AS content_hash, distance AS score ORDER BY score, address"
		statements = append(statements, nativeLadybugStatement{Query: query, Parameters: parameters})
	}
	payload, _ := json.Marshal(nativeLadybugPlan{Statements: statements})
	maxRows := input.SearchProfile.LexicalCandidateLimit
	if request.Mode == "semantic" {
		maxRows = input.SearchProfile.SemanticCandidateLimit
	}
	if request.Mode == "hybrid" {
		maxRows += input.SearchProfile.SemanticCandidateLimit
	}
	plan := nativePlan(port.PlanSearch, payload, maxRows, input.MaxOutputBytes)
	digest := sha256.Sum256(input.Request)
	rrfK := input.SearchProfile.RRFK
	if rrfK <= 0 {
		rrfK = 60
	}
	lexicalWeight, semanticWeight := input.SearchProfile.LexicalWeight, input.SearchProfile.SemanticWeight
	if lexicalWeight == 0 && semanticWeight == 0 {
		lexicalWeight, semanticWeight = 1, 1
	}
	return port.PreparedSearch{Plan: plan, QueryDigest: "sha256:" + hex.EncodeToString(digest[:]), Mode: request.Mode, MaxHits: input.SearchProfile.MaxHits, RRFK: rrfK, LexicalWeight: lexicalWeight, SemanticWeight: semanticWeight}, nil
}

func (e *Adapter) PrepareQuery(_ context.Context, input port.BoundExecutionRequest) (port.ExecutionPlan, error) {
	request, err := decodeExecutionRequest(input.Request, "structural_query")
	if !e.available() || err != nil || input.MaxOutputBytes <= 0 || len(request.RootAddresses) == 0 || len(request.RootAddresses) > 256 {
		return port.ExecutionPlan{}, ErrInvalidNativeSearchRequest
	}
	conditions := make([]string, len(request.RootAddresses))
	parameters := make(map[string]port.RawValue, len(request.RootAddresses))
	seen := map[string]bool{}
	for i, address := range request.RootAddresses {
		if address == "" || seen[address] {
			return port.ExecutionPlan{}, ErrInvalidNativeSearchRequest
		}
		seen[address] = true
		name := fmt.Sprintf("root_%d", i)
		conditions[i] = "n.id = $" + name
		parameters[name] = port.RawValue{Kind: "string", Value: address}
	}
	payload, _ := json.Marshal(nativeLadybugPlan{Statements: []nativeLadybugStatement{{Query: "MATCH (n:SearchDoc) WHERE " + strings.Join(conditions, " OR ") + " RETURN n.id AS address, n.kind AS kind, n.owner AS owner ORDER BY address", Parameters: parameters}}})
	return nativePlan(port.PlanQuery, payload, len(request.RootAddresses), input.MaxOutputBytes), nil
}

func (e *Adapter) PrepareAnalysis(_ context.Context, input port.BoundExecutionRequest) (port.ExecutionPlan, error) {
	request, err := decodeExecutionRequest(input.Request, "analyze_graph")
	if !e.available() || err != nil || input.MaxOutputBytes <= 0 || len(request.EntityAddresses) == 0 || len(request.EntityAddresses) > 256 || len(request.RelationAddresses) == 0 || len(request.RelationAddresses) > 512 {
		return port.ExecutionPlan{}, ErrInvalidNativeSearchRequest
	}
	addresses := append([]string(nil), request.EntityAddresses...)
	slices.Sort(addresses)
	for i, address := range addresses {
		if len(address) > 1024 || !stableScopeAddress.MatchString(address) || (i > 0 && address == addresses[i-1]) {
			return port.ExecutionPlan{}, ErrInvalidNativeSearchRequest
		}
	}
	encodedAddresses := make([]string, len(addresses))
	for i, address := range addresses {
		encodedAddresses[i] = strconv.Quote(address)
	}
	digest := sha256.Sum256(input.Request)
	graphName := "ld_scope_" + hex.EncodeToString(digest[:8])
	predicate := "n.id IN [" + strings.Join(encodedAddresses, ",") + "]"
	relations := append([]string(nil), request.RelationAddresses...)
	slices.Sort(relations)
	encodedRelations := make([]string, len(relations))
	for i, address := range relations {
		if len(address) > 1024 || !stableScopeAddress.MatchString(address) || (i > 0 && address == relations[i-1]) {
			return port.ExecutionPlan{}, ErrInvalidNativeSearchRequest
		}
		encodedRelations[i] = strconv.Quote(address)
	}
	relationPredicate := "r.id IN [" + strings.Join(encodedRelations, ",") + "]"
	project := nativeLadybugStatement{Query: "CALL PROJECT_GRAPH('" + graphName + "', {'SearchNode': '" + predicate + "'}, {'SearchEdge': '" + relationPredicate + "'})", Parameters: map[string]port.RawValue{}}
	algorithmQueries := map[string]string{
		"page_rank": "CALL page_rank('" + graphName + "') RETURN node.id AS address, 'importance' AS metric_name, rank AS metric_value ORDER BY address",
		"k_core":    "CALL k_core_decomposition('" + graphName + "') RETURN node.id AS address, 'core_number' AS metric_name, k_degree AS metric_value ORDER BY address",
		"louvain":   "CALL louvain('" + graphName + "') RETURN node.id AS address, 'community_id' AS metric_name, louvain_id AS metric_value ORDER BY address",
		"scc":       "CALL strongly_connected_components('" + graphName + "') RETURN node.id AS address, 'component_id' AS metric_name, group_id AS metric_value ORDER BY address",
		"wcc":       "CALL weakly_connected_components('" + graphName + "') RETURN node.id AS address, 'component_id' AS metric_name, group_id AS metric_value ORDER BY address",
	}
	query, ok := algorithmQueries[request.Algorithm]
	if !ok {
		return port.ExecutionPlan{}, ErrInvalidNativeSearchRequest
	}
	statements := []nativeLadybugStatement{project, {Query: query, Parameters: map[string]port.RawValue{}}, {Query: "CALL DROP_PROJECTED_GRAPH('" + graphName + "')", Parameters: map[string]port.RawValue{}}}
	payload, _ := json.Marshal(nativeLadybugPlan{Statements: statements})
	return nativePlan(port.PlanAnalysis, payload, len(addresses), input.MaxOutputBytes), nil
}

func (e *Adapter) CompleteSearch(_ context.Context, input port.CompleteSearchInput) ([]byte, error) {
	if !input.Rows.Complete || input.Rows.Truncated {
		return nil, ErrInvalidNativeSearchRequest
	}
	rows := make([]engine.SearchCandidateRow, len(input.Rows.Rows))
	for index, row := range input.Rows.Rows {
		rows[index] = engine.SearchCandidateRow{Signal: row["signal"].Value, Address: row["address"].Value, Kind: row["kind"].Value, Owner: row["owner"].Value, GraphEntries: row["graph_entries"].Value, TypeAddresses: row["type_addresses"].Value, LayerAddresses: row["layer_addresses"].Value, ContentHash: row["content_hash"].Value, Score: row["score"].Value}
	}
	result, err := engine.CompleteSearchResult(engine.SearchCompletion{Mode: input.Prepared.Mode, QueryDigest: input.Prepared.QueryDigest, MaxHits: input.Prepared.MaxHits, RRFK: input.Prepared.RRFK, LexicalWeight: input.Prepared.LexicalWeight, SemanticWeight: input.Prepared.SemanticWeight, Rows: rows})
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
