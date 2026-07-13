// SPDX-License-Identifier: LicenseRef-LayerDraw-1.0

package main

import (
	"reflect"
	"strings"
	"testing"
)

func TestParseProfile(t *testing.T) {
	t.Parallel()

	profile := `mode: atomic
github.com/dencyuinc/layerdraw/internal/engine/engine.go:10.1,12.2 2 1
github.com/dencyuinc/layerdraw/cmd/layerdraw-engine/main.go:20.1,22.2 3 0
`

	blocks, err := parseProfile(strings.NewReader(profile), "github.com/dencyuinc/layerdraw")
	if err != nil {
		t.Fatalf("parseProfile: %v", err)
	}
	if len(blocks) != 2 {
		t.Fatalf("len(blocks) = %d, want 2", len(blocks))
	}
	if blocks[0].file != "internal/engine/engine.go" || blocks[0].packageID != "github.com/dencyuinc/layerdraw/internal/engine" {
		t.Fatalf("first block = %+v", blocks[0])
	}
}

func TestMergeUnifiedDiff(t *testing.T) {
	t.Parallel()

	diff := `diff --git a/internal/engine/engine.go b/internal/engine/engine.go
--- a/internal/engine/engine.go
+++ b/internal/engine/engine.go
@@ -10,0 +11,3 @@
+one
+two
+three
`

	got := make(map[string]map[int]struct{})
	if err := mergeUnifiedDiff(got, strings.NewReader(diff)); err != nil {
		t.Fatalf("mergeUnifiedDiff: %v", err)
	}
	want := map[string]map[int]struct{}{
		"internal/engine/engine.go": {11: {}, 12: {}, 13: {}},
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("changed lines = %v, want %v", got, want)
	}
}

func TestPackageMinimumUsesMostSpecificRule(t *testing.T) {
	t.Parallel()

	value := policy{
		DefaultPackageMinimum: 85,
		PackageRules: []packageRule{
			{Prefix: "example/internal", Minimum: 90},
			{Prefix: "example/internal/engine", Minimum: 95},
		},
	}

	if got := packageMinimum(value, "example/internal/engine/compiler"); got != 95 {
		t.Fatalf("packageMinimum = %.1f, want 95", got)
	}
}

func TestEvaluateRejectsPackageBelowThreshold(t *testing.T) {
	t.Parallel()

	value := policy{
		OverallMinimum:        50,
		DefaultPackageMinimum: 80,
		PatchMinimum:          90,
	}
	blocks := []coverageBlock{
		{file: "internal/example/example.go", packageID: "example/internal/example", startLine: 1, endLine: 2, statements: 7, count: 1},
		{file: "internal/example/example.go", packageID: "example/internal/example", startLine: 3, endLine: 4, statements: 3, count: 0},
	}

	var output strings.Builder
	err := evaluate(value, blocks, nil, &output)
	if err == nil {
		t.Fatal("evaluate succeeded, want package threshold failure")
	}
	if !strings.Contains(output.String(), "package coverage: 70.0%") {
		t.Fatalf("output = %q", output.String())
	}
}

func TestEvaluateRejectsPatchBelowThreshold(t *testing.T) {
	t.Parallel()

	value := policy{
		OverallMinimum:        0,
		DefaultPackageMinimum: 0,
		PatchMinimum:          90,
	}
	blocks := []coverageBlock{
		{file: "internal/example/example.go", packageID: "example/internal/example", startLine: 10, endLine: 10, statements: 1, count: 1},
		{file: "internal/example/example.go", packageID: "example/internal/example", startLine: 11, endLine: 11, statements: 9, count: 0},
	}
	changedLines := map[string]map[int]struct{}{
		"internal/example/example.go": {10: {}, 11: {}},
	}

	var output strings.Builder
	err := evaluate(value, blocks, changedLines, &output)
	if err == nil {
		t.Fatal("evaluate succeeded, want patch threshold failure")
	}
	if !strings.Contains(output.String(), "patch coverage: 10.0%") {
		t.Fatalf("output = %q", output.String())
	}
}
