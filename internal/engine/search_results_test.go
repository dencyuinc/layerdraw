// SPDX-License-Identifier: LicenseRef-LayerDraw-1.0

package engine

import (
	"encoding/json"
	"strings"
	"testing"
)

func resultTestSnapshot() SearchResultSnapshotRef {
	return SearchResultSnapshotRef{Kind: "host_revision", HostDocumentID: "doc", CommittedRevision: "r1", DefinitionHash: "sha256:def"}
}

func completeTestSearch(input SearchCompletion) ([]byte, error) {
	input.DocumentSnapshotRef = resultTestSnapshot()
	input.IndexIdentityDigest = "sha256:index"
	return CompleteSearchResult(input)
}

func analysisTestCompletion() AnalysisCompletion {
	return AnalysisCompletion{DocumentSnapshotRef: resultTestSnapshot(), EntityAddresses: []string{"a", "b", "z"}, RelationAddresses: []string{"r"}, Algorithm: "page_rank", AlgorithmProfileID: "page-rank-v1", BackendVersion: "ladybug-1"}
}

func TestCompleteSearchResultOwnsRRFAndStableOrdering(t *testing.T) {
	result, err := completeTestSearch(SearchCompletion{Mode: "hybrid", QueryDigest: "sha256:q", MaxHits: 2, RRFK: 60, LexicalWeight: 1, SemanticWeight: 1, Rows: []SearchCandidateRow{
		{Signal: "lexical", Address: "b", Kind: "entity", GraphEntries: `[]`, TypeAddresses: `[]`, LayerAddresses: `[]`, ContentHash: "hb", Score: "2"},
		{Signal: "lexical", Address: "a", Kind: "entity", GraphEntries: `[]`, TypeAddresses: `[]`, LayerAddresses: `[]`, ContentHash: "ha", Score: "1"},
		{Signal: "semantic", Address: "a", Kind: "entity", GraphEntries: `[]`, TypeAddresses: `[]`, LayerAddresses: `[]`, ContentHash: "ha", Score: "0.1"},
		{Signal: "semantic", Address: "b", Kind: "entity", GraphEntries: `[]`, TypeAddresses: `[]`, LayerAddresses: `[]`, ContentHash: "hb", Score: "0.2"},
	}})
	if err != nil || !strings.Contains(string(result), `"rank":1`) || !strings.Contains(string(result), `"subject_address":"a"`) || !strings.Contains(string(result), `"semantic_distance":"0.1"`) || !strings.Contains(string(result), `"document_snapshot_ref":`) || !strings.Contains(string(result), `"index_identity_digest":"sha256:index"`) || !strings.Contains(string(result), `"diagnostics":[]`) || !strings.Contains(string(result), `"search_result_hash":"sha256:`) || !strings.Contains(string(result), `"score_signals":`) {
		t.Fatalf("result=%s err=%v", result, err)
	}
}

func TestCompleteSearchResultOwnsMatchedSourceRefsAndBoundedSnippets(t *testing.T) {
	fields, _ := json.Marshal([]searchResultField{{FieldPath: "name", SourceRef: `{"path":"doc.ldl","line":4}`, Text: "Needle 日本語 suffix", LexicalWeight: 100}, {FieldPath: "description", SourceRef: `{"path":"doc.ldl","line":5}`, Text: "not matched", LexicalWeight: 20}})
	result, err := completeTestSearch(SearchCompletion{Mode: "lexical", QueryDigest: "q", QueryText: "needle", MaxHits: 1, RRFK: 60, LexicalWeight: 1, SnippetMaxBytes: 10, Rows: []SearchCandidateRow{{Signal: "lexical", Address: "a", Kind: "entity", GraphEntries: `[]`, TypeAddresses: `[]`, LayerAddresses: `[]`, ContentHash: "h", Fields: string(fields), Score: "1"}}})
	if err != nil || !strings.Contains(string(result), `"matched_source_refs":[{"line":4,"path":"doc.ldl"}]`) || !strings.Contains(string(result), `"text":"Needle 日"`) || strings.Contains(string(result), "suffix") || strings.Contains(string(result), "not matched") {
		t.Fatalf("result=%s err=%v", result, err)
	}
}

func TestCompleteSearchResultRejectsDuplicatePhysicalSignal(t *testing.T) {
	_, err := completeTestSearch(SearchCompletion{Mode: "lexical", QueryDigest: "q", MaxHits: 1, RRFK: 1, LexicalWeight: 1, Rows: []SearchCandidateRow{{Signal: "lexical", Address: "a", Kind: "entity", Score: "1"}, {Signal: "lexical", Address: "a", Kind: "entity", Score: "1"}}})
	if err == nil {
		t.Fatal("duplicate physical candidate was accepted")
	}
}

func TestCompleteQueryAndAnalysisResults(t *testing.T) {
	query, err := CompleteQueryResult([]QueryRow{{"address": {Kind: "string", Value: "a"}, "kind": {Kind: "string", Value: "entity"}, "owner": {Kind: "string"}}}, QueryCompletion{DocumentSnapshotRef: resultTestSnapshot(), QueryAddress: "ldl:project:p:query:q", StatePolicy: "none", StateInput: QueryResultStateInput{Kind: "none"}})
	if err != nil || !strings.Contains(string(query), `"primary_entity_addresses":["a"]`) || !strings.Contains(string(query), `"query_result_hash":"sha256:`) {
		t.Fatalf("query=%s err=%v", query, err)
	}
	analysis, err := CompleteAnalysisResult([]AnalysisValue{{Address: "a", MetricName: "importance", TypedValue: "0.5"}}, analysisTestCompletion())
	if err != nil || !strings.Contains(string(analysis), `"importance"`) {
		t.Fatalf("analysis=%s err=%v", analysis, err)
	}
	if _, err := CompleteAnalysisResult([]AnalysisValue{{}}, analysisTestCompletion()); err == nil {
		t.Fatal("invalid analysis accepted")
	}
}

func TestAnalysisNormalizesNumbersAndStableCommunityLabels(t *testing.T) {
	result, err := CompleteAnalysisResult([]AnalysisValue{
		{Address: "z", MetricName: "community_id", TypedValue: "99"},
		{Address: "a", MetricName: "community_id", TypedValue: "42"},
		{Address: "b", MetricName: "community_id", TypedValue: "42"},
		{Address: "a", MetricName: "importance", TypedValue: "5e-1"},
	}, analysisTestCompletion())
	if err != nil {
		t.Fatal(err)
	}
	encoded := string(result)
	if !strings.Contains(encoded, `"kind":"float64","value":"0.5"`) || !strings.Contains(encoded, `"result_hash":"sha256:`) || !strings.Contains(encoded, `"input_subgraph_hash":"sha256:`) || !strings.Contains(encoded, `"summaries":[`) {
		t.Fatalf("result=%s", result)
	}
	first := strings.Index(encoded, `"subject_address":"a"`)
	second := strings.Index(encoded, `"subject_address":"z"`)
	if first < 0 || second < first || !strings.Contains(encoded, `"kind":"int64","value":"1"`) || !strings.Contains(encoded, `"kind":"int64","value":"2"`) {
		t.Fatalf("unstable labels/order: %s", result)
	}
	if _, err := CompleteAnalysisResult([]AnalysisValue{{Address: "a", MetricName: "importance", TypedValue: "NaN"}}, analysisTestCompletion()); err == nil {
		t.Fatal("non-finite backend metric accepted")
	}
}

func TestAnalysisHashesCanonicalInputSubgraphAndSemanticResult(t *testing.T) {
	values := []AnalysisValue{{Address: "b", MetricName: "core_number", TypedValue: "2"}, {Address: "a", MetricName: "core_number", TypedValue: "1"}}
	completion := analysisTestCompletion()
	first, err := CompleteAnalysisResult(values, completion)
	if err != nil {
		t.Fatal(err)
	}
	completion.EntityAddresses = []string{"z", "a", "b"}
	second, err := CompleteAnalysisResult([]AnalysisValue{values[1], values[0]}, completion)
	if err != nil || string(first) != string(second) {
		t.Fatalf("canonical permutation diverged:\n%s\n%s err=%v", first, second, err)
	}
	completion.EntityAddresses = []string{"a", "b"}
	changed, err := CompleteAnalysisResult(values, completion)
	if err != nil || string(first) == string(changed) {
		t.Fatalf("changed scope retained identity: first=%s changed=%s err=%v", first, changed, err)
	}
}

func TestAnalysisAcceptsQueryResultHashWithCompleteSelectedSubgraph(t *testing.T) {
	completion := analysisTestCompletion()
	completion.QueryResultHash = "sha256:query-result"
	result, err := CompleteAnalysisResult([]AnalysisValue{{Address: "a", MetricName: "importance", TypedValue: "0.5"}}, completion)
	if err != nil || !strings.Contains(string(result), `"input_subgraph_hash":"sha256:`) {
		t.Fatalf("query-result scope result=%s err=%v", result, err)
	}
}

func TestQueryResultRejectsGenericRawRows(t *testing.T) {
	completion := QueryCompletion{DocumentSnapshotRef: resultTestSnapshot(), QueryAddress: "ldl:project:p:query:q", StatePolicy: "none", StateInput: QueryResultStateInput{Kind: "none"}}
	if _, err := CompleteQueryResult([]QueryRow{{"arbitrary": {Kind: "string", Value: "backend-owned"}}}, completion); err == nil {
		t.Fatal("generic backend row escaped as QueryResult")
	}
}

func TestCompleteSearchResultUsesRuntimeSignedBoundedCursor(t *testing.T) {
	rows := []SearchCandidateRow{
		{Signal: "lexical", Address: "a", Kind: "entity", GraphEntries: `[]`, TypeAddresses: `[]`, LayerAddresses: `[]`, ContentHash: "ha", Score: "3"},
		{Signal: "lexical", Address: "b", Kind: "entity", GraphEntries: `[]`, TypeAddresses: `[]`, LayerAddresses: `[]`, ContentHash: "hb", Score: "2"},
		{Signal: "lexical", Address: "c", Kind: "entity", GraphEntries: `[]`, TypeAddresses: `[]`, LayerAddresses: `[]`, ContentHash: "hc", Score: "1"},
	}
	first, err := completeTestSearch(SearchCompletion{Mode: "lexical", QueryDigest: "q", MaxHits: 2, RRFK: 60, LexicalWeight: 1, NextCursor: "signed", Rows: rows})
	if err != nil || !strings.Contains(string(first), `"next_cursor":"signed"`) || !strings.Contains(string(first), `"result_truncated":true`) {
		t.Fatalf("first=%s err=%v", first, err)
	}
	second, err := completeTestSearch(SearchCompletion{Mode: "lexical", QueryDigest: "q", MaxHits: 2, RRFK: 60, LexicalWeight: 1, Offset: 2, NextCursor: "unused", Rows: rows})
	if err != nil || !strings.Contains(string(second), `"subject_address":"c"`) || strings.Contains(string(second), `next_cursor`) {
		t.Fatalf("second=%s err=%v", second, err)
	}
}
