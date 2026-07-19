// SPDX-License-Identifier: LicenseRef-LayerDraw-1.0

package engine

import (
	"encoding/json"
	"errors"
	"slices"
	"strings"
	"unicode/utf8"
)

var ErrSearchResultInvalid = errors.New("engine.search_result_invalid")

type SearchCandidateRow struct {
	Signal, Address, Kind, Owner, GraphEntries, TypeAddresses, LayerAddresses, ContentHash, Fields, Score string
}

type SearchCompletion struct {
	Mode, QueryDigest, QueryText  string
	MaxHits, RRFK                 int
	LexicalWeight, SemanticWeight float64
	Offset                        int
	NextCursor                    string
	SnippetMaxBytes               int
	Rows                          []SearchCandidateRow
}

// CompleteSearchResult owns deduplication, reciprocal-rank fusion, stable
// ordering, and the canonical domain result shape. Physical adapters supply
// candidates only and never interpret ranking semantics.
func CompleteSearchResult(input SearchCompletion) ([]byte, error) {
	if input.MaxHits <= 0 || input.RRFK <= 0 || input.Offset < 0 || input.QueryDigest == "" || (input.Mode != "lexical" && input.Mode != "semantic" && input.Mode != "hybrid") {
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
		matchedRefs, snippets, err := searchExplainability(value.fields, input.QueryText, input.SnippetMaxBytes)
		if err != nil {
			return nil, ErrSearchResultInvalid
		}
		hits[index] = map[string]any{"rank": input.Offset + index + 1, "subject_address": value.address, "subject_kind": value.kind, "owner_address": value.owner, "graph_entry_addresses": graphEntries, "type_addresses": typeAddresses, "layer_addresses": layerAddresses, "content_hash": value.contentHash, "lexical_rank": value.lexicalRank, "lexical_score": value.lexicalScore, "semantic_rank": value.semanticRank, "semantic_distance": value.semanticDistance, "fused_score": value.fused, "matched_source_refs": matchedRefs, "bounded_snippets": snippets}
	}
	result := map[string]any{"mode": input.Mode, "query_digest": input.QueryDigest, "hits": hits, "result_truncated": truncated}
	if truncated {
		if input.NextCursor == "" {
			return nil, ErrSearchResultInvalid
		}
		result["next_cursor"] = input.NextCursor
	}
	return json.Marshal(result)
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

type QueryValue struct{ Kind, Value string }
type QueryRow map[string]QueryValue

func CompleteQueryResult(rows []QueryRow) ([]byte, error) {
	result := make([]map[string]QueryValue, len(rows))
	for index, row := range rows {
		keys := make([]string, 0, len(row))
		for key := range row {
			keys = append(keys, key)
		}
		slices.Sort(keys)
		result[index] = make(map[string]QueryValue, len(row))
		for _, key := range keys {
			result[index][key] = row[key]
		}
	}
	return json.Marshal(map[string]any{"rows": result})
}

func CompleteAnalysisResult(values []AnalysisValue) ([]byte, error) {
	result := make([]map[string]string, len(values))
	for index, value := range values {
		if value.Address == "" || value.MetricName == "" || value.TypedValue == "" {
			return nil, ErrSearchResultInvalid
		}
		result[index] = map[string]string{"subject_address": value.Address, "metric_name": value.MetricName, "typed_value": value.TypedValue}
	}
	return json.Marshal(map[string]any{"values": result, "result_truncated": false})
}
