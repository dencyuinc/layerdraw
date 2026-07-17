// SPDX-License-Identifier: LicenseRef-LayerDraw-1.0

package query

import "testing"

func TestCompareStateFieldPathsUsesRegistryThenLexicalFallback(t *testing.T) {
	t.Parallel()
	first := StateSystemCreatedAt
	last := StateProvenanceConfidence
	if CompareStateFieldPaths(first, last) >= 0 {
		t.Fatal("registry order did not place the first field before the last")
	}
	if CompareStateFieldPaths(last, first) <= 0 {
		t.Fatal("registry order did not place the last field after the first")
	}
	if CompareStateFieldPaths(first, first) != 0 {
		t.Fatal("identical registered fields did not compare equal")
	}
	if CompareStateFieldPaths("future.z", "future.a") <= 0 {
		t.Fatal("unknown fields did not use lexical fallback")
	}
}
