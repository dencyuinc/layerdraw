// SPDX-License-Identifier: Apache-2.0

package conformance_test

import (
	"encoding/json"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

type browserAuthoringAuthority struct {
	Path string `json:"path"`
	Test string `json:"test"`
}

type browserAuthoringMatrix struct {
	SchemaVersion int      `json:"schema_version"`
	RequiredPaths []string `json:"required_paths"`
	Assertions    []string `json:"assertions"`
	Paths         []struct {
		ID        string                    `json:"id"`
		Authority browserAuthoringAuthority `json:"authority"`
	} `json:"paths"`
	FailureClasses []struct {
		ID        string                    `json:"id"`
		Authority browserAuthoringAuthority `json:"authority"`
	} `json:"failure_classes"`
	PackageBoundaries []browserAuthoringAuthority `json:"package_boundaries"`
}

func TestBrowserAuthoringMatrixNamesExecutableAuthorities(t *testing.T) {
	root := portableCompileRepositoryRoot(t)
	data, err := os.ReadFile(filepath.Join(root, "tests", "conformance", "testdata", "browser_authoring_matrix_v1.json"))
	if err != nil {
		t.Fatal(err)
	}
	var matrix browserAuthoringMatrix
	decoder := json.NewDecoder(strings.NewReader(string(data)))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&matrix); err != nil {
		t.Fatal(err)
	}
	if matrix.SchemaVersion != 1 {
		t.Fatalf("browser authoring matrix identity drifted: %+v", matrix)
	}
	wantPaths := []string{"browser_composed", "engine_local", "engine_runtime_parity", "runtime_host"}
	wantFailures := []string{"approval_cancellation", "conflict", "denial", "disposal", "optional_unavailable", "reconnect", "required_capability", "stale_revision"}
	wantAssertions := []string{"accessibility_and_responsive_states", "authoritative_preview_parity", "capability_fail_closed", "engine_semantics_excluded_from_ui", "framework_neutral_package_graph", "large_document_latency_budgets", "persistence_truth", "typed_recovery_lifecycle"}

	paths := make([]string, 0, len(matrix.Paths))
	for _, path := range matrix.Paths {
		paths = append(paths, path.ID)
		browserAuthoringRequireAuthority(t, root, path.Authority)
	}
	failures := make([]string, 0, len(matrix.FailureClasses))
	for _, failure := range matrix.FailureClasses {
		failures = append(failures, failure.ID)
		browserAuthoringRequireAuthority(t, root, failure.Authority)
	}
	if len(matrix.PackageBoundaries) != 3 {
		t.Fatalf("browser authoring package boundary closure drifted: %d", len(matrix.PackageBoundaries))
	}
	for _, authority := range matrix.PackageBoundaries {
		browserAuthoringRequireAuthority(t, root, authority)
	}
	slices.Sort(paths)
	slices.Sort(failures)
	slices.Sort(matrix.RequiredPaths)
	slices.Sort(matrix.Assertions)
	if !slices.Equal(paths, wantPaths) || !slices.Equal(matrix.RequiredPaths, wantPaths) {
		t.Fatalf("browser authoring path closure drifted: paths=%v required=%v", paths, matrix.RequiredPaths)
	}
	if !slices.Equal(failures, wantFailures) || !slices.Equal(matrix.Assertions, wantAssertions) {
		t.Fatalf("browser authoring closure drifted: failures=%v assertions=%v", failures, matrix.Assertions)
	}
}

func browserAuthoringRequireAuthority(t *testing.T, root string, authority browserAuthoringAuthority) {
	t.Helper()
	if authority.Path == "" || authority.Test == "" || filepath.IsAbs(authority.Path) || strings.Contains(filepath.ToSlash(authority.Path), "../") {
		t.Fatalf("invalid browser authoring authority: %+v", authority)
	}
	data, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(authority.Path)))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(data), authority.Test) {
		t.Fatalf("%s does not contain authority %q", authority.Path, authority.Test)
	}
}
