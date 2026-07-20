// SPDX-License-Identifier: LicenseRef-LayerDraw-1.0

package engine

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"math"
	"slices"
	"strconv"
	"strings"
	"unicode/utf8"
)

var ErrSearchResultInvalid = errors.New("engine.search_result_invalid")

type SearchCandidateRow struct {
	Signal, Address, Kind, Owner, GraphEntries, TypeAddresses, LayerAddresses, ContentHash, Fields, Score string
}

type SearchCompletion struct {
	Mode, QueryDigest, QueryText  string
	DocumentSnapshotRef           SearchResultSnapshotRef
	IndexIdentityDigest           string
	MaxHits, RRFK                 int
	LexicalWeight, SemanticWeight float64
	Offset                        int
	NextCursor                    string
	SnippetMaxBytes               int
	Rows                          []SearchCandidateRow
}

type SearchResultSnapshotRef struct {
	Kind               string `json:"kind"`
	HostDocumentID     string `json:"host_document_id,omitempty"`
	CommittedRevision  string `json:"committed_revision,omitempty"`
	SourceTreeDigest   string `json:"source_tree_digest,omitempty"`
	DocumentGeneration uint64 `json:"document_generation,omitempty"`
	DefinitionHash     string `json:"definition_hash"`
}

type ResultDiagnostic struct {
	Code     string `json:"code"`
	Severity string `json:"severity"`
}

// CompleteSearchResult owns deduplication, reciprocal-rank fusion, stable
// ordering, and the canonical domain result shape. Physical adapters supply
// candidates only and never interpret ranking semantics.
func CompleteSearchResult(input SearchCompletion) ([]byte, error) {
	if input.MaxHits <= 0 || input.RRFK <= 0 || input.Offset < 0 || input.QueryDigest == "" || input.IndexIdentityDigest == "" || !validResultSnapshot(input.DocumentSnapshotRef) || (input.Mode != "lexical" && input.Mode != "semantic" && input.Mode != "hybrid") {
		return nil, ErrSearchResultInvalid
	}
	type candidate struct {
		address, kind, owner, graphEntries, typeAddresses, layerAddresses, contentHash, fields, lexicalScore, semanticDistance string
		lexicalRank, semanticRank                                                                                              int
		fused                                                                                                                  float64
	}
	byAddress := map[string]*candidate{}
	lexicalRank, semanticRank := 0, 0
	for _, row := range input.Rows {
		if row.Address == "" || row.Kind == "" || row.Score == "" {
			return nil, ErrSearchResultInvalid
		}
		number, err := strconv.ParseFloat(row.Score, 64)
		if err != nil || math.IsNaN(number) || math.IsInf(number, 0) {
			return nil, ErrSearchResultInvalid
		}
		row.Score = strconv.FormatFloat(number, 'g', -1, 64)
		value := byAddress[row.Address]
		if value == nil {
			value = &candidate{address: row.Address, kind: row.Kind, owner: row.Owner, graphEntries: row.GraphEntries, typeAddresses: row.TypeAddresses, layerAddresses: row.LayerAddresses, contentHash: row.ContentHash, fields: row.Fields}
			byAddress[row.Address] = value
		} else if value.kind != row.Kind || value.owner != row.Owner || value.graphEntries != row.GraphEntries || value.typeAddresses != row.TypeAddresses || value.layerAddresses != row.LayerAddresses || value.contentHash != row.ContentHash || value.fields != row.Fields {
			return nil, ErrSearchResultInvalid
		}
		switch row.Signal {
		case "lexical":
			if value.lexicalRank != 0 {
				return nil, ErrSearchResultInvalid
			}
			lexicalRank++
			value.lexicalRank, value.lexicalScore = lexicalRank, row.Score
		case "semantic":
			if value.semanticRank != 0 {
				return nil, ErrSearchResultInvalid
			}
			semanticRank++
			value.semanticRank, value.semanticDistance = semanticRank, row.Score
		default:
			return nil, ErrSearchResultInvalid
		}
	}
	values := make([]*candidate, 0, len(byAddress))
	for _, value := range byAddress {
		if value.lexicalRank > 0 {
			value.fused += input.LexicalWeight / float64(input.RRFK+value.lexicalRank)
		}
		if value.semanticRank > 0 {
			value.fused += input.SemanticWeight / float64(input.RRFK+value.semanticRank)
		}
		values = append(values, value)
	}
	slices.SortFunc(values, func(a, b *candidate) int {
		if a.fused > b.fused {
			return -1
		}
		if a.fused < b.fused {
			return 1
		}
		return strings.Compare(a.address, b.address)
	})
	if input.Offset > len(values) || (input.Offset == len(values) && input.Offset != 0) {
		return nil, ErrSearchResultInvalid
	}
	if input.Offset > 0 {
		values = values[input.Offset:]
	}
	truncated := len(values) > input.MaxHits
	if truncated {
		values = values[:input.MaxHits]
	}
	hits := make([]map[string]any, len(values))
	for index, value := range values {
		var graphEntries, typeAddresses, layerAddresses []string
		if json.Unmarshal([]byte(value.graphEntries), &graphEntries) != nil || json.Unmarshal([]byte(value.typeAddresses), &typeAddresses) != nil || json.Unmarshal([]byte(value.layerAddresses), &layerAddresses) != nil || value.contentHash == "" {
			return nil, ErrSearchResultInvalid
		}
		_ = typeAddresses
		_ = layerAddresses
		matchedRefs, snippets, err := searchExplainability(value.fields, input.QueryText, input.SnippetMaxBytes)
		if err != nil {
			return nil, ErrSearchResultInvalid
		}
		signals := map[string]any{"fused_score": value.fused}
		if value.lexicalRank != 0 {
			signals["lexical_rank"], signals["lexical_score"] = value.lexicalRank, value.lexicalScore
		}
		if value.semanticRank != 0 {
			signals["semantic_rank"], signals["semantic_distance"] = value.semanticRank, value.semanticDistance
		}
		hit := map[string]any{"rank": input.Offset + index + 1, "subject_address": value.address, "subject_kind": value.kind, "graph_entry_addresses": graphEntries, "score": value.fused, "score_signals": signals, "content_hash": value.contentHash, "matched_source_refs": matchedRefs, "bounded_snippets": snippets}
		if value.owner != "" {
			hit["owner_address"] = value.owner
		}
		hits[index] = hit
	}
	result := map[string]any{"document_snapshot_ref": input.DocumentSnapshotRef, "index_identity_digest": input.IndexIdentityDigest, "mode": input.Mode, "query_digest": input.QueryDigest, "hits": hits, "result_truncated": truncated, "diagnostics": []ResultDiagnostic{}}
	if truncated {
		if input.NextCursor == "" {
			return nil, ErrSearchResultInvalid
		}
		result["next_cursor"] = input.NextCursor
	}
	result["search_result_hash"] = canonicalDigest(result)
	return json.Marshal(result)
}

func validResultSnapshot(value SearchResultSnapshotRef) bool {
	if value.DefinitionHash == "" {
		return false
	}
	return (value.Kind == "host_revision" && value.HostDocumentID != "" && value.CommittedRevision != "" && value.SourceTreeDigest == "" && value.DocumentGeneration == 0) ||
		(value.Kind == "portable_generation" && value.HostDocumentID == "" && value.CommittedRevision == "" && value.SourceTreeDigest != "")
}

func canonicalDigest(value any) string {
	encoded, _ := json.Marshal(value)
	digest := sha256.Sum256(encoded)
	return "sha256:" + hex.EncodeToString(digest[:])
}

type searchResultField struct {
	FieldPath     string
	SourceRef     string
	Text          string
	LexicalWeight int
}

func searchExplainability(encodedFields, query string, maxBytes int) ([]any, []map[string]any, error) {
	if maxBytes <= 0 {
		maxBytes = 256
	}
	if encodedFields == "" {
		return []any{}, []map[string]any{}, nil
	}
	var fields []searchResultField
	if json.Unmarshal([]byte(encodedFields), &fields) != nil {
		return nil, nil, ErrSearchResultInvalid
	}
	terms := strings.Fields(strings.ToLower(query))
	refs := []any{}
	snippets := []map[string]any{}
	seenRefs := map[string]bool{}
	for _, field := range fields {
		matched := len(terms) == 0
		folded := strings.ToLower(field.Text)
		for _, term := range terms {
			if strings.Contains(folded, term) {
				matched = true
				break
			}
		}
		if !matched {
			continue
		}
		if field.SourceRef != "" && !seenRefs[field.SourceRef] {
			var ref any
			if json.Unmarshal([]byte(field.SourceRef), &ref) != nil {
				return nil, nil, ErrSearchResultInvalid
			}
			seenRefs[field.SourceRef] = true
			refs = append(refs, ref)
		}
		snippets = append(snippets, map[string]any{"field_path": field.FieldPath, "text": boundUTF8(field.Text, maxBytes), "lexical_weight": field.LexicalWeight})
	}
	return refs, snippets, nil
}

func boundUTF8(value string, maxBytes int) string {
	if len(value) <= maxBytes {
		return value
	}
	value = value[:maxBytes]
	for !utf8.ValidString(value) {
		value = value[:len(value)-1]
	}
	return value
}

type AnalysisValue struct{ Address, MetricName, TypedValue string }

type QueryValue struct {
	Kind  string `json:"kind"`
	Value string `json:"value"`
}
type QueryRow map[string]QueryValue

type QueryCompletion struct {
	DocumentSnapshotRef SearchResultSnapshotRef
	QueryAddress        string
	Arguments           map[string]QueryValue
	StatePolicy         string
	StateInput          QueryResultStateInput
}

type QueryResultStateInput struct {
	Kind           string `json:"kind"`
	SnapshotHash   string `json:"snapshot_hash,omitempty"`
	StateVersion   string `json:"state_version,omitempty"`
	CapturedAt     string `json:"captured_at,omitempty"`
	DefinitionHash string `json:"definition_hash,omitempty"`
}

type canonicalQueryResult struct {
	DocumentSnapshotRef       SearchResultSnapshotRef `json:"document_snapshot_ref"`
	QueryAddress              string                  `json:"query_address"`
	Arguments                 map[string]QueryValue   `json:"arguments"`
	StatePolicy               string                  `json:"state_policy"`
	StateInput                QueryResultStateInput   `json:"state_input"`
	StateReads                []any                   `json:"state_reads"`
	SeedEntityAddresses       []string                `json:"seed_entity_addresses"`
	ReachedEntityAddresses    []string                `json:"reached_entity_addresses"`
	TraversedEntityAddresses  []string                `json:"traversed_entity_addresses"`
	PathRelationAddresses     []string                `json:"path_relation_addresses"`
	InducedRelationAddresses  []string                `json:"induced_relation_addresses"`
	PrimaryEntityAddresses    []string                `json:"primary_entity_addresses"`
	SelectedRelationAddresses []string                `json:"selected_relation_addresses"`
	SupportEntityAddresses    []string                `json:"support_entity_addresses"`
	Paths                     []any                   `json:"paths"`
	CycleRefs                 []any                   `json:"cycle_refs"`
	Diagnostics               []ResultDiagnostic      `json:"diagnostics"`
}

func CompleteQueryResult(rows []QueryRow, input QueryCompletion) ([]byte, error) {
	if !validResultSnapshot(input.DocumentSnapshotRef) || input.QueryAddress == "" || !stableSearchScopeAddress.MatchString(input.QueryAddress) || (input.StatePolicy != "none" && input.StatePolicy != "optional" && input.StatePolicy != "required") || input.StateInput.Kind == "" {
		return nil, ErrSearchResultInvalid
	}
	noneInput := input.StateInput.Kind == "none" && input.StateInput.SnapshotHash == "" && input.StateInput.StateVersion == "" && input.StateInput.CapturedAt == "" && input.StateInput.DefinitionHash == ""
	snapshotInput := input.StateInput.Kind == "snapshot" && input.StateInput.SnapshotHash != "" && input.StateInput.StateVersion != "" && input.StateInput.CapturedAt != "" && input.StateInput.DefinitionHash != ""
	if (input.StatePolicy == "none" && !noneInput) || (input.StatePolicy == "optional" && !noneInput && !snapshotInput) || (input.StatePolicy == "required" && !snapshotInput) {
		return nil, ErrSearchResultInvalid
	}
	if input.Arguments == nil {
		input.Arguments = map[string]QueryValue{}
	}
	for key, value := range input.Arguments {
		if key == "" || value.Kind == "" || !utf8.ValidString(key) || !utf8.ValidString(value.Kind) || !utf8.ValidString(value.Value) {
			return nil, ErrSearchResultInvalid
		}
	}
	entities, relations := []string{}, []string{}
	seen := map[string]bool{}
	for _, row := range rows {
		if len(row) != 3 || row["address"].Value == "" || row["kind"].Value == "" || !stableSearchScopeAddress.MatchString(row["address"].Value) || seen[row["address"].Value] {
			return nil, ErrSearchResultInvalid
		}
		for _, key := range []string{"address", "kind", "owner"} {
			if _, ok := row[key]; !ok {
				return nil, ErrSearchResultInvalid
			}
		}
		seen[row["address"].Value] = true
		switch row["kind"].Value {
		case "entity":
			entities = append(entities, row["address"].Value)
		case "relation":
			relations = append(relations, row["address"].Value)
		default:
			return nil, ErrSearchResultInvalid
		}
	}
	slices.Sort(entities)
	slices.Sort(relations)
	result := canonicalQueryResult{
		DocumentSnapshotRef: input.DocumentSnapshotRef, QueryAddress: input.QueryAddress, Arguments: input.Arguments,
		StatePolicy: input.StatePolicy, StateInput: input.StateInput, StateReads: []any{}, SeedEntityAddresses: append([]string(nil), entities...),
		ReachedEntityAddresses: []string{}, TraversedEntityAddresses: []string{}, PathRelationAddresses: []string{}, InducedRelationAddresses: []string{},
		PrimaryEntityAddresses: entities, SelectedRelationAddresses: relations, SupportEntityAddresses: []string{}, Paths: []any{}, CycleRefs: []any{}, Diagnostics: []ResultDiagnostic{},
	}
	response := struct {
		canonicalQueryResult
		QueryResultHash string `json:"query_result_hash"`
	}{canonicalQueryResult: result, QueryResultHash: canonicalDigest(result)}
	return json.Marshal(response)
}

type AnalysisCompletion struct {
	DocumentSnapshotRef SearchResultSnapshotRef
	EntityAddresses     []string
	RelationAddresses   []string
	QueryResultHash     string
	Algorithm           string
	AlgorithmProfileID  string
	BackendVersion      string
}

type analysisSummary struct {
	MetricName string             `json:"metric_name"`
	Count      int                `json:"count"`
	Minimum    *map[string]string `json:"minimum,omitempty"`
	Maximum    *map[string]string `json:"maximum,omitempty"`
}

func CompleteAnalysisResult(values []AnalysisValue, input AnalysisCompletion) ([]byte, error) {
	if len(values) == 0 || !validResultSnapshot(input.DocumentSnapshotRef) || input.Algorithm == "" || input.AlgorithmProfileID == "" || input.BackendVersion == "" {
		return nil, ErrSearchResultInvalid
	}
	inputSubgraphHash, err := canonicalInputSubgraphHash(input)
	if err != nil {
		return nil, ErrSearchResultInvalid
	}
	seen := map[string]bool{}
	groups := map[string][]string{}
	for _, value := range values {
		if value.Address == "" || value.MetricName == "" || value.TypedValue == "" || !stableSearchScopeAddress.MatchString(value.Address) || seen[value.MetricName+"\x00"+value.Address] {
			return nil, ErrSearchResultInvalid
		}
		seen[value.MetricName+"\x00"+value.Address] = true
		if value.MetricName == "community_id" || value.MetricName == "component_id" {
			if _, err := strconv.ParseInt(value.TypedValue, 10, 64); err != nil {
				return nil, ErrSearchResultInvalid
			}
			groups[value.MetricName+"\x00"+value.TypedValue] = append(groups[value.MetricName+"\x00"+value.TypedValue], value.Address)
		}
	}
	type group struct{ key, first string }
	groupValues := make([]group, 0, len(groups))
	for key, addresses := range groups {
		slices.Sort(addresses)
		groupValues = append(groupValues, group{key: key, first: addresses[0]})
	}
	slices.SortFunc(groupValues, func(a, b group) int {
		if metric := strings.Compare(strings.SplitN(a.key, "\x00", 2)[0], strings.SplitN(b.key, "\x00", 2)[0]); metric != 0 {
			return metric
		}
		return strings.Compare(a.first, b.first)
	})
	labels := map[string]string{}
	metricLabel := map[string]int{}
	for _, value := range groupValues {
		metric := strings.SplitN(value.key, "\x00", 2)[0]
		metricLabel[metric]++
		labels[value.key] = strconv.Itoa(metricLabel[metric])
	}
	result := make([]map[string]any, len(values))
	for index, value := range values {
		var normalized string
		kind := "int64"
		switch value.MetricName {
		case "importance":
			number, err := strconv.ParseFloat(value.TypedValue, 64)
			if err != nil || math.IsNaN(number) || math.IsInf(number, 0) {
				return nil, ErrSearchResultInvalid
			}
			kind, normalized = "float64", strconv.FormatFloat(number, 'g', -1, 64)
		case "core_number":
			number, err := strconv.ParseInt(value.TypedValue, 10, 64)
			if err != nil || number < 0 {
				return nil, ErrSearchResultInvalid
			}
			normalized = strconv.FormatInt(number, 10)
		case "community_id", "component_id":
			normalized = labels[value.MetricName+"\x00"+value.TypedValue]
		default:
			return nil, ErrSearchResultInvalid
		}
		result[index] = map[string]any{"subject_address": value.Address, "metric_name": value.MetricName, "typed_value": map[string]string{"kind": kind, "value": normalized}}
	}
	slices.SortFunc(result, func(a, b map[string]any) int {
		if metric := strings.Compare(a["metric_name"].(string), b["metric_name"].(string)); metric != 0 {
			return metric
		}
		return strings.Compare(a["subject_address"].(string), b["subject_address"].(string))
	})
	summaries := summarizeAnalysisValues(result)
	canonical := struct {
		DocumentSnapshotRef SearchResultSnapshotRef `json:"document_snapshot_ref"`
		InputSubgraphHash   string                  `json:"input_subgraph_hash"`
		Algorithm           string                  `json:"algorithm"`
		AlgorithmProfileID  string                  `json:"algorithm_profile_id"`
		BackendVersion      string                  `json:"backend_version"`
		Values              []map[string]any        `json:"values"`
		Summaries           []analysisSummary       `json:"summaries"`
		Diagnostics         []ResultDiagnostic      `json:"diagnostics"`
	}{input.DocumentSnapshotRef, inputSubgraphHash, input.Algorithm, input.AlgorithmProfileID, input.BackendVersion, result, summaries, []ResultDiagnostic{}}
	response := struct {
		DocumentSnapshotRef SearchResultSnapshotRef `json:"document_snapshot_ref"`
		InputSubgraphHash   string                  `json:"input_subgraph_hash"`
		Algorithm           string                  `json:"algorithm"`
		AlgorithmProfileID  string                  `json:"algorithm_profile_id"`
		BackendVersion      string                  `json:"backend_version"`
		Values              []map[string]any        `json:"values"`
		Summaries           []analysisSummary       `json:"summaries"`
		Diagnostics         []ResultDiagnostic      `json:"diagnostics"`
		ResultHash          string                  `json:"result_hash"`
	}{canonical.DocumentSnapshotRef, canonical.InputSubgraphHash, canonical.Algorithm, canonical.AlgorithmProfileID, canonical.BackendVersion, canonical.Values, canonical.Summaries, canonical.Diagnostics, canonicalDigest(canonical)}
	return json.Marshal(response)
}

func canonicalInputSubgraphHash(input AnalysisCompletion) (string, error) {
	if (input.QueryResultHash == "") == (len(input.EntityAddresses) == 0 && len(input.RelationAddresses) == 0) {
		return "", ErrSearchResultInvalid
	}
	entities := append([]string(nil), input.EntityAddresses...)
	relations := append([]string(nil), input.RelationAddresses...)
	slices.Sort(entities)
	slices.Sort(relations)
	for _, values := range [][]string{entities, relations} {
		for index, address := range values {
			if address == "" || !stableSearchScopeAddress.MatchString(address) || (index > 0 && address == values[index-1]) {
				return "", ErrSearchResultInvalid
			}
		}
	}
	return canonicalDigest(struct {
		Snapshot          SearchResultSnapshotRef `json:"document_snapshot_ref"`
		QueryResultHash   string                  `json:"query_result_hash,omitempty"`
		EntityAddresses   []string                `json:"entity_addresses"`
		RelationAddresses []string                `json:"relation_addresses"`
	}{input.DocumentSnapshotRef, input.QueryResultHash, entities, relations}), nil
}

func summarizeAnalysisValues(values []map[string]any) []analysisSummary {
	byMetric := map[string][]map[string]string{}
	for _, value := range values {
		typed := value["typed_value"].(map[string]string)
		byMetric[value["metric_name"].(string)] = append(byMetric[value["metric_name"].(string)], typed)
	}
	metrics := make([]string, 0, len(byMetric))
	for metric := range byMetric {
		metrics = append(metrics, metric)
	}
	slices.Sort(metrics)
	result := make([]analysisSummary, 0, len(metrics))
	for _, metric := range metrics {
		values := byMetric[metric]
		summary := analysisSummary{MetricName: metric, Count: len(values)}
		minimum, maximum := values[0], values[0]
		for _, value := range values[1:] {
			left, _ := strconv.ParseFloat(value["value"], 64)
			min, _ := strconv.ParseFloat(minimum["value"], 64)
			max, _ := strconv.ParseFloat(maximum["value"], 64)
			if left < min {
				minimum = value
			}
			if left > max {
				maximum = value
			}
		}
		summary.Minimum, summary.Maximum = &minimum, &maximum
		result = append(result, summary)
	}
	return result
}
