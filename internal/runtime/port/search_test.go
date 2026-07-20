// SPDX-License-Identifier: LicenseRef-LayerDraw-1.0

package port

import "testing"

func TestSearchDocumentPhysicalDigestBindsPhysicalColumns(t *testing.T) {
	document := SearchDocumentInput{
		SubjectAddress: "ldl:entity:a", SubjectKind: "entity", OwnerAddress: "ldl:project:p",
		ContentHash: "sha256:content", LexicalText: "alpha",
		GraphEntryAddresses: []string{"ldl:entity:b"}, TypeAddresses: []string{"ldl:type:t"}, LayerAddresses: []string{"ldl:layer:l"},
		Fields: []SearchDocumentField{{FieldPath: "name", SourceRef: "document.ldl:1", Text: "Alpha", LexicalWeight: 2}},
	}
	first := SearchDocumentPhysicalDigest(document, []float32{1, 2})
	if first == "" {
		t.Fatalf("digest=%q", first)
	}
	document.LexicalText = "beta"
	second := SearchDocumentPhysicalDigest(document, []float32{1, 2})
	if second == first {
		t.Fatalf("changed digest=%q first=%q", second, first)
	}
	third := SearchDocumentPhysicalDigest(document, []float32{2, 1})
	if third == second {
		t.Fatalf("vector digest=%q second=%q", third, second)
	}
}
