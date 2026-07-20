// SPDX-License-Identifier: LicenseRef-LayerDraw-1.0

package engine

import (
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
)

var ErrSearchPlanInvalid = errors.New("engine.search_plan_invalid")
var stableSearchScopeAddress = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9:._/-]*$`)

type SearchPlanValue struct{ Kind, Value string }
type SearchPlanStatement struct {
	Query      string                     `json:"query"`
	Parameters map[string]SearchPlanValue `json:"parameters"`
}
type SearchPlanPhysicalRef struct {
	IdentityDigest string `json:"identity_digest"`
	ContentDigest  string `json:"content_digest"`
	BackendVersion string `json:"backend_version"`
}
type SearchPlanEvidence struct {
	TableName                 string   `json:"table_name"`
	IndexName                 string   `json:"index_name"`
	IndexType                 string   `json:"index_type"`
	PropertyNames             []string `json:"property_names"`
	ContentColumns            []string `json:"content_columns"`
	PrimaryKey                string   `json:"primary_key"`
	AllowNonPrimary           bool     `json:"allow_non_primary,omitempty"`
	Relation                  bool     `json:"relation,omitempty"`
	ExpectedDocumentSetDigest string   `json:"expected_document_set_digest,omitempty"`
}
type NativeSearchPlan struct {
	Statements       []SearchPlanStatement  `json:"statements"`
	PhysicalIndex    *SearchPlanPhysicalRef `json:"physical_index,omitempty"`
	PhysicalEvidence []SearchPlanEvidence   `json:"physical_evidence,omitempty"`
}

type NativeIndexDocument struct {
	SubjectAddress, SubjectKind, OwnerAddress, ContentHash, PhysicalDigest, LexicalText string
	GraphEntryAddresses, TypeAddresses, LayerAddresses                                  []string
	Embedding                                                                           []float32
	FieldsJSON                                                                          string
}
type NativeIndexPlanInput struct {
	Request               []byte
	Identity              []byte
	BackendVersion        string
	EmbeddingDimensions   int
	Documents             []NativeIndexDocument
	PreviousContentHashes map[string]string
	FullRebuild           bool
}

type nativeExecutionRequest struct {
	Kind              string   `json:"kind"`
	Mode              string   `json:"mode,omitempty"`
	QueryText         string   `json:"query_text,omitempty"`
	TargetKind        string   `json:"target_kind,omitempty"`
	RootAddresses     []string `json:"root_addresses,omitempty"`
	Algorithm         string   `json:"algorithm,omitempty"`
	EntityAddresses   []string `json:"entity_addresses,omitempty"`
	RelationAddresses []string `json:"relation_addresses,omitempty"`
}

func BuildNativeSearchIndexPlan(input NativeIndexPlanInput) (NativeSearchPlan, error) {
	var request struct {
		Kind string `json:"kind"`
	}
	if decodeClosedSearchRequest(input.Request, &request) != nil || request.Kind != "build_search_index" || input.BackendVersion == "" || input.EmbeddingDimensions < 0 || len(input.Identity) == 0 {
		return NativeSearchPlan{}, ErrSearchPlanInvalid
	}
	identityDigest := sha256.Sum256(input.Identity)
	physical := &SearchPlanPhysicalRef{IdentityDigest: hex.EncodeToString(identityDigest[:]), BackendVersion: input.BackendVersion}
	fullRebuild := input.FullRebuild || input.PreviousContentHashes == nil
	documentSchema := "CREATE NODE TABLE IF NOT EXISTS SearchDoc (id STRING, kind STRING, owner STRING, graph_entries STRING, type_addresses STRING, layer_addresses STRING, content_hash STRING, physical_digest STRING, fields STRING, body STRING"
	if input.EmbeddingDimensions > 0 {
		documentSchema += fmt.Sprintf(", embedding FLOAT[%d]", input.EmbeddingDimensions)
	}
	documentSchema += ", PRIMARY KEY(id))"
	statements := []SearchPlanStatement{
		{Query: documentSchema, Parameters: map[string]SearchPlanValue{}},
		{Query: "CREATE NODE TABLE IF NOT EXISTS SearchNode (id STRING, PRIMARY KEY(id))", Parameters: map[string]SearchPlanValue{}},
		{Query: "CREATE REL TABLE IF NOT EXISTS SearchEdge (FROM SearchNode TO SearchNode, id STRING, from_id STRING, to_id STRING)", Parameters: map[string]SearchPlanValue{}},
		{Query: "MATCH ()-[r:SearchEdge]->() DELETE r", Parameters: map[string]SearchPlanValue{}},
		{Query: "MATCH (n:SearchNode) DELETE n", Parameters: map[string]SearchPlanValue{}},
	}
	if fullRebuild {
		statements = append(statements, SearchPlanStatement{Query: "MATCH (n:SearchDoc) DELETE n", Parameters: map[string]SearchPlanValue{}})
	}
	graphNodes := map[string]bool{}
	seen := map[string]bool{}
	currentHashes := make(map[string]string, len(input.Documents))
	for _, document := range input.Documents {
		physicalDigest, digestErr := nativeIndexDocumentPhysicalDigest(document)
		if digestErr != nil || document.SubjectAddress == "" || document.ContentHash == "" || (document.PhysicalDigest != "" && document.PhysicalDigest != physicalDigest) || len(document.Embedding) != input.EmbeddingDimensions || seen[document.SubjectAddress] {
			return NativeSearchPlan{}, ErrSearchPlanInvalid
		}
		document.PhysicalDigest = physicalDigest
		seen[document.SubjectAddress] = true
		currentHashes[document.SubjectAddress] = document.PhysicalDigest
		if !json.Valid([]byte(document.FieldsJSON)) {
			return NativeSearchPlan{}, ErrSearchPlanInvalid
		}
		for _, address := range document.GraphEntryAddresses {
			if !graphNodes[address] {
				graphNodes[address] = true
				statements = append(statements, SearchPlanStatement{Query: "CREATE (n:SearchNode {id: $id})", Parameters: map[string]SearchPlanValue{"id": {Kind: "string", Value: address}}})
			}
		}
		if document.SubjectKind == "relation" && len(document.GraphEntryAddresses) == 2 {
			statements = append(statements, SearchPlanStatement{Query: "MATCH (a:SearchNode {id: $from}), (b:SearchNode {id: $to}) CREATE (a)-[:SearchEdge {id: $id, from_id: $from, to_id: $to}]->(b)", Parameters: map[string]SearchPlanValue{"from": {Kind: "string", Value: document.GraphEntryAddresses[0]}, "to": {Kind: "string", Value: document.GraphEntryAddresses[1]}, "id": {Kind: "string", Value: document.SubjectAddress}}})
		}
		if !fullRebuild && input.PreviousContentHashes[document.SubjectAddress] == document.PhysicalDigest {
			continue
		}
		if !fullRebuild && input.PreviousContentHashes[document.SubjectAddress] != "" {
			statements = append(statements, SearchPlanStatement{Query: "MATCH (n:SearchDoc {id: $id}) DELETE n", Parameters: map[string]SearchPlanValue{"id": {Kind: "string", Value: document.SubjectAddress}}})
		}
		graphEntries, _ := json.Marshal(document.GraphEntryAddresses)
		types, _ := json.Marshal(document.TypeAddresses)
		layers, _ := json.Marshal(document.LayerAddresses)
		parameters := map[string]SearchPlanValue{"id": {Kind: "string", Value: document.SubjectAddress}, "kind": {Kind: "string", Value: document.SubjectKind}, "owner": {Kind: "string", Value: document.OwnerAddress}, "graph_entries": {Kind: "string", Value: string(graphEntries)}, "type_addresses": {Kind: "string", Value: string(types)}, "layer_addresses": {Kind: "string", Value: string(layers)}, "content_hash": {Kind: "string", Value: document.ContentHash}, "physical_digest": {Kind: "string", Value: document.PhysicalDigest}, "fields": {Kind: "string", Value: document.FieldsJSON}, "body": {Kind: "string", Value: document.LexicalText}}
		properties := "id: $id, kind: $kind, owner: $owner, graph_entries: $graph_entries, type_addresses: $type_addresses, layer_addresses: $layer_addresses, content_hash: $content_hash, physical_digest: $physical_digest, fields: $fields, body: $body"
		if input.EmbeddingDimensions > 0 {
			embedding, _ := json.Marshal(document.Embedding)
			parameters["embedding"] = SearchPlanValue{Kind: "float32_array", Value: string(embedding)}
			properties += ", embedding: $embedding"
		}
		statements = append(statements, SearchPlanStatement{Query: "CREATE (n:SearchDoc {" + properties + "})", Parameters: parameters})
	}
	if !fullRebuild {
		removed := make([]string, 0)
		for address := range input.PreviousContentHashes {
			if currentHashes[address] == "" {
				removed = append(removed, address)
			}
		}
		slices.Sort(removed)
		for _, address := range removed {
			statements = append(statements, SearchPlanStatement{Query: "MATCH (n:SearchDoc {id: $id}) DELETE n", Parameters: map[string]SearchPlanValue{"id": {Kind: "string", Value: address}}})
		}
	}
	statements = append(statements, SearchPlanStatement{Query: "CALL CREATE_FTS_INDEX('SearchDoc', 'search_doc_fts', ['body'], stemmer := 'none')", Parameters: map[string]SearchPlanValue{}})
	if input.EmbeddingDimensions > 0 {
		statements = append(statements, SearchPlanStatement{Query: "CALL CREATE_VECTOR_INDEX('SearchDoc', 'search_doc_vector', 'embedding', metric := 'cosine')", Parameters: map[string]SearchPlanValue{}})
	}
	expectedDocumentSetDigest := searchDocumentSetDigest(currentHashes)
	columns := []string{"id", "kind", "owner", "graph_entries", "type_addresses", "layer_addresses", "content_hash", "physical_digest", "fields", "body"}
	if input.EmbeddingDimensions > 0 {
		columns = append(columns, "embedding")
	}
	evidence := []SearchPlanEvidence{
		{TableName: "SearchDoc", IndexName: "search_doc_fts", IndexType: "FTS", PropertyNames: []string{"body"}, ContentColumns: columns, PrimaryKey: "id", ExpectedDocumentSetDigest: expectedDocumentSetDigest},
	}
	if input.EmbeddingDimensions > 0 {
		evidence = append(evidence, SearchPlanEvidence{TableName: "SearchDoc", IndexName: "search_doc_vector", IndexType: "HNSW", PropertyNames: []string{"embedding"}, ContentColumns: columns, PrimaryKey: "id", ExpectedDocumentSetDigest: expectedDocumentSetDigest})
	}
	evidence = append(evidence, SearchPlanEvidence{TableName: "SearchNode", ContentColumns: []string{"id"}, PrimaryKey: "id"}, SearchPlanEvidence{TableName: "SearchEdge", ContentColumns: []string{"id", "from_id", "to_id"}, PrimaryKey: "id", AllowNonPrimary: true, Relation: true})
	return NativeSearchPlan{Statements: statements, PhysicalIndex: physical, PhysicalEvidence: evidence}, nil
}

func nativeIndexDocumentPhysicalDigest(document NativeIndexDocument) (string, error) {
	data, err := json.Marshal(struct {
		SubjectAddress, SubjectKind, OwnerAddress, ContentHash, LexicalText string
		GraphEntryAddresses, TypeAddresses, LayerAddresses                  []string
		Embedding                                                           []float32
		FieldsJSON                                                          string
	}{document.SubjectAddress, document.SubjectKind, document.OwnerAddress, document.ContentHash, document.LexicalText, document.GraphEntryAddresses, document.TypeAddresses, document.LayerAddresses, document.Embedding, document.FieldsJSON})
	if err != nil {
		return "", err
	}
	digest := sha256.Sum256(data)
	return "sha256:" + hex.EncodeToString(digest[:]), nil
}

func searchDocumentSetDigest(hashes map[string]string) string {
	addresses := make([]string, 0, len(hashes))
	for address := range hashes {
		addresses = append(addresses, address)
	}
	slices.Sort(addresses)
	hash := sha256.New()
	for _, address := range addresses {
		_, _ = io.WriteString(hash, address+"\x00"+hashes[address]+"\n")
	}
	return "sha256:" + hex.EncodeToString(hash.Sum(nil))
}

type NativeSearchPlanInput struct {
	Request                                     []byte
	QueryEmbedding                              []float32
	LexicalLimit, SemanticLimit, MaxOutputBytes int
}
type NativeSearchPreparation struct {
	Plan                         NativeSearchPlan
	Mode, QueryDigest, QueryText string
	MaxRows                      int
}

func BuildNativeSearchPlan(input NativeSearchPlanInput) (NativeSearchPreparation, error) {
	request, err := decodeNativeExecutionRequest(input.Request, "search_documents")
	if err != nil || (request.Mode != "lexical" && request.Mode != "semantic" && request.Mode != "hybrid") || request.QueryText == "" || input.MaxOutputBytes <= 0 || input.LexicalLimit <= 0 || input.SemanticLimit <= 0 || ((request.Mode == "semantic" || request.Mode == "hybrid") && len(input.QueryEmbedding) == 0) || (request.Mode == "lexical" && len(input.QueryEmbedding) != 0) {
		return NativeSearchPreparation{}, ErrSearchPlanInvalid
	}
	statements := []SearchPlanStatement{}
	if request.Mode == "lexical" || request.Mode == "hybrid" {
		query := "CALL QUERY_FTS_INDEX('SearchDoc', 'search_doc_fts', $query, TOP := $limit) "
		parameters := map[string]SearchPlanValue{"query": {Kind: "string", Value: request.QueryText}, "limit": {Kind: "int64", Value: fmt.Sprint(input.LexicalLimit)}}
		if request.TargetKind != "" {
			query += "WHERE node.kind = $target_kind "
			parameters["target_kind"] = SearchPlanValue{Kind: "string", Value: request.TargetKind}
		}
		query += "RETURN 'lexical' AS signal, node.id AS address, node.kind AS kind, node.owner AS owner, node.graph_entries AS graph_entries, node.type_addresses AS type_addresses, node.layer_addresses AS layer_addresses, node.content_hash AS content_hash, node.fields AS fields, score AS score ORDER BY score DESC, address"
		statements = append(statements, SearchPlanStatement{Query: query, Parameters: parameters})
	}
	if request.Mode == "semantic" || request.Mode == "hybrid" {
		embedding, _ := json.Marshal(input.QueryEmbedding)
		query := "CALL QUERY_VECTOR_INDEX('SearchDoc', 'search_doc_vector', $embedding, $limit) "
		parameters := map[string]SearchPlanValue{"embedding": {Kind: "float32_array", Value: string(embedding)}, "limit": {Kind: "int64", Value: fmt.Sprint(input.SemanticLimit)}}
		if request.TargetKind != "" {
			query += "WHERE node.kind = $target_kind "
			parameters["target_kind"] = SearchPlanValue{Kind: "string", Value: request.TargetKind}
		}
		query += "RETURN 'semantic' AS signal, node.id AS address, node.kind AS kind, node.owner AS owner, node.graph_entries AS graph_entries, node.type_addresses AS type_addresses, node.layer_addresses AS layer_addresses, node.content_hash AS content_hash, node.fields AS fields, distance AS score ORDER BY score, address"
		statements = append(statements, SearchPlanStatement{Query: query, Parameters: parameters})
	}
	maxRows := input.LexicalLimit
	if request.Mode == "semantic" {
		maxRows = input.SemanticLimit
	} else if request.Mode == "hybrid" {
		maxRows += input.SemanticLimit
	}
	digest := sha256.Sum256(input.Request)
	return NativeSearchPreparation{Plan: NativeSearchPlan{Statements: statements}, Mode: request.Mode, QueryDigest: "sha256:" + hex.EncodeToString(digest[:]), QueryText: request.QueryText, MaxRows: maxRows}, nil
}

func BuildNativeQueryPlan(requestBytes []byte) (NativeSearchPlan, int, error) {
	request, err := decodeNativeExecutionRequest(requestBytes, "structural_query")
	if err != nil || len(request.RootAddresses) == 0 || len(request.RootAddresses) > 256 {
		return NativeSearchPlan{}, 0, ErrSearchPlanInvalid
	}
	conditions := make([]string, len(request.RootAddresses))
	parameters := map[string]SearchPlanValue{}
	seen := map[string]bool{}
	for index, address := range request.RootAddresses {
		if address == "" || seen[address] {
			return NativeSearchPlan{}, 0, ErrSearchPlanInvalid
		}
		seen[address] = true
		name := fmt.Sprintf("root_%d", index)
		conditions[index] = "n.id = $" + name
		parameters[name] = SearchPlanValue{Kind: "string", Value: address}
	}
	return NativeSearchPlan{Statements: []SearchPlanStatement{{Query: "MATCH (n:SearchDoc) WHERE " + strings.Join(conditions, " OR ") + " RETURN n.id AS address, n.kind AS kind, n.owner AS owner ORDER BY address", Parameters: parameters}}}, len(request.RootAddresses), nil
}

func BuildNativeAnalysisPlan(requestBytes []byte) (NativeSearchPlan, int, error) {
	request, err := decodeNativeExecutionRequest(requestBytes, "analyze_graph")
	if err != nil || len(request.EntityAddresses) == 0 || len(request.EntityAddresses) > 256 || len(request.RelationAddresses) == 0 || len(request.RelationAddresses) > 512 {
		return NativeSearchPlan{}, 0, ErrSearchPlanInvalid
	}
	encodeScope := func(values []string) ([]string, error) {
		values = append([]string(nil), values...)
		slices.Sort(values)
		encoded := make([]string, len(values))
		for index, address := range values {
			if len(address) > 1024 || !stableSearchScopeAddress.MatchString(address) || (index > 0 && address == values[index-1]) {
				return nil, ErrSearchPlanInvalid
			}
			encoded[index] = strconv.Quote(address)
		}
		return encoded, nil
	}
	entities, err := encodeScope(request.EntityAddresses)
	if err != nil {
		return NativeSearchPlan{}, 0, err
	}
	relations, err := encodeScope(request.RelationAddresses)
	if err != nil {
		return NativeSearchPlan{}, 0, err
	}
	digest := sha256.Sum256(requestBytes)
	graph := "ld_scope_" + hex.EncodeToString(digest[:8])
	validate := SearchPlanStatement{Query: "MATCH (a:SearchNode)-[r:SearchEdge]->(b:SearchNode) WHERE r.id IN [" + strings.Join(relations, ",") + "] AND (NOT a.id IN [" + strings.Join(entities, ",") + "] OR NOT b.id IN [" + strings.Join(entities, ",") + "]) RETURN r.id AS address, 'scope_violation' AS metric_name, '1' AS metric_value", Parameters: map[string]SearchPlanValue{}}
	project := SearchPlanStatement{Query: "CALL PROJECT_GRAPH('" + graph + "', {'SearchNode': 'n.id IN [" + strings.Join(entities, ",") + "]'}, {'SearchEdge': 'r.id IN [" + strings.Join(relations, ",") + "]'})", Parameters: map[string]SearchPlanValue{}}
	queries := map[string]string{
		"page_rank": "CALL page_rank('" + graph + "') RETURN node.id AS address, 'importance' AS metric_name, rank AS metric_value ORDER BY address",
		"k_core":    "CALL k_core_decomposition('" + graph + "') RETURN node.id AS address, 'core_number' AS metric_name, k_degree AS metric_value ORDER BY address",
		"louvain":   "CALL louvain('" + graph + "') RETURN node.id AS address, 'community_id' AS metric_name, louvain_id AS metric_value ORDER BY address",
		"scc":       "CALL strongly_connected_components('" + graph + "') RETURN node.id AS address, 'component_id' AS metric_name, group_id AS metric_value ORDER BY address",
		"wcc":       "CALL weakly_connected_components('" + graph + "') RETURN node.id AS address, 'component_id' AS metric_name, group_id AS metric_value ORDER BY address",
	}
	query, ok := queries[request.Algorithm]
	if !ok {
		return NativeSearchPlan{}, 0, ErrSearchPlanInvalid
	}
	return NativeSearchPlan{Statements: []SearchPlanStatement{validate, project, {Query: query, Parameters: map[string]SearchPlanValue{}}, {Query: "CALL DROP_PROJECTED_GRAPH('" + graph + "')", Parameters: map[string]SearchPlanValue{}}}}, len(request.EntityAddresses) + 1, nil
}

func decodeNativeExecutionRequest(data []byte, kind string) (nativeExecutionRequest, error) {
	var request nativeExecutionRequest
	if decodeClosedSearchRequest(data, &request) != nil || request.Kind != kind {
		return request, ErrSearchPlanInvalid
	}
	return request, nil
}
func decodeClosedSearchRequest(data []byte, destination any) error {
	decoder := json.NewDecoder(strings.NewReader(string(data)))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(destination); err != nil {
		return err
	}
	if err := decoder.Decode(&struct{}{}); err != io.EOF {
		return ErrSearchPlanInvalid
	}
	return nil
}
