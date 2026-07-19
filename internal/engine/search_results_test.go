// SPDX-License-Identifier: LicenseRef-LayerDraw-1.0

package engine

import (
	"strings"
	"testing"
)

func TestCompleteSearchResultOwnsRRFAndStableOrdering(t *testing.T) {
	result, err := CompleteSearchResult(SearchCompletion{Mode: "hybrid", QueryDigest: "sha256:q", MaxHits: 2, RRFK: 60, LexicalWeight: 1, SemanticWeight: 1, Rows: []SearchCandidateRow{
		{Signal: "lexical", Address: "b", Kind: "entity", GraphEntries: `[]`, TypeAddresses: `[]`, LayerAddresses: `[]`, ContentHash: "hb", Score: "2"},
		{Signal: "lexical", Address: "a", Kind: "entity", GraphEntries: `[]`, TypeAddresses: `[]`, LayerAddresses: `[]`, ContentHash: "ha", Score: "1"},
		{Signal: "semantic", Address: "a", Kind: "entity", GraphEntries: `[]`, TypeAddresses: `[]`, LayerAddresses: `[]`, ContentHash: "ha", Score: "0.1"},
		{Signal: "semantic", Address: "b", Kind: "entity", GraphEntries: `[]`, TypeAddresses: `[]`, LayerAddresses: `[]`, ContentHash: "hb", Score: "0.2"},
	}})
	if err != nil || !strings.Contains(string(result), `"rank":1`) || !strings.Contains(string(result), `"subject_address":"a"`) || !strings.Contains(string(result), `"semantic_distance":"0.1"`) {
		t.Fatalf("result=%s err=%v", result, err)
	}
}

func TestCompleteSearchResultRejectsDuplicatePhysicalSignal(t *testing.T) {
	_, err := CompleteSearchResult(SearchCompletion{Mode: "lexical", QueryDigest: "q", MaxHits: 1, RRFK: 1, LexicalWeight: 1, Rows: []SearchCandidateRow{{Signal: "lexical", Address: "a", Kind: "entity", Score: "1"}, {Signal: "lexical", Address: "a", Kind: "entity", Score: "1"}}})
	if err == nil {
		t.Fatal("duplicate physical candidate was accepted")
	}
}
