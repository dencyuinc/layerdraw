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

type viewDataAuthority struct {
	Path string `json:"path"`
	Test string `json:"test"`
}

type viewDataMatrix struct {
	SchemaVersion int      `json:"schema_version"`
	Corpus        string   `json:"corpus"`
	RequiredPaths []string `json:"required_paths"`
	Assertions    []string `json:"assertions"`
	Paths         []struct {
		ID        string            `json:"id"`
		CaseScope string            `json:"case_scope"`
		Authority viewDataAuthority `json:"authority"`
	} `json:"paths"`
	FailureClasses []struct {
		ID          string              `json:"id"`
		Authorities []viewDataAuthority `json:"authorities"`
	} `json:"failure_classes"`
}

func TestViewDataConformanceMatrixNamesExecutableAuthorities(t *testing.T) {
	root := portableCompileRepositoryRoot(t)
	data, err := os.ReadFile(filepath.Join(root, "tests", "conformance", "testdata", "viewdata_conformance_matrix_v1.json"))
	if err != nil {
		t.Fatal(err)
	}
	var matrix viewDataMatrix
	decoder := json.NewDecoder(strings.NewReader(string(data)))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&matrix); err != nil {
		t.Fatal(err)
	}
	if matrix.SchemaVersion != 1 || matrix.Corpus != "tests/conformance/testdata/viewdata_conformance_v1.json" {
		t.Fatalf("ViewData matrix identity drifted: %+v", matrix)
	}
	if _, err := os.Stat(filepath.Join(root, filepath.FromSlash(matrix.Corpus))); err != nil {
		t.Fatal(err)
	}
	wantPaths := []string{"browser_wasm_worker", "engine_client_browser_wasm", "engine_client_stdio", "in_process_go", "node_wasm_worker", "packaged_stdio_raw"}
	wantFailures := []string{"cancelled", "invalid_input", "limit_exceeded", "malformed_wire"}
	wantAssertions := []string{"canonical_view_data", "diagnostics", "failure_category", "item_identities", "no_partial_publication", "outcome", "source_refs", "state_refs", "transport_determinism"}

	paths := make([]string, 0, len(matrix.Paths))
	for _, path := range matrix.Paths {
		paths = append(paths, path.ID)
		if path.CaseScope != "all" && path.CaseScope != "materialize_and_malformed" && path.CaseScope != "materialize_malformed_and_hard_cancel" {
			t.Fatalf("%s has unknown case scope %q", path.ID, path.CaseScope)
		}
		viewDataRequireAuthority(t, root, path.Authority)
	}
	failures := make([]string, 0, len(matrix.FailureClasses))
	for _, failure := range matrix.FailureClasses {
		failures = append(failures, failure.ID)
		if len(failure.Authorities) == 0 {
			t.Fatalf("%s has no executable authority", failure.ID)
		}
		for _, authority := range failure.Authorities {
			viewDataRequireAuthority(t, root, authority)
		}
	}
	slices.Sort(paths)
	slices.Sort(failures)
	slices.Sort(matrix.RequiredPaths)
	slices.Sort(matrix.Assertions)
	if !slices.Equal(paths, wantPaths) || !slices.Equal(matrix.RequiredPaths, wantPaths) {
		t.Fatalf("ViewData path closure drifted: paths=%v required=%v", paths, matrix.RequiredPaths)
	}
	if !slices.Equal(failures, wantFailures) || !slices.Equal(matrix.Assertions, wantAssertions) {
		t.Fatalf("ViewData closure drifted: failures=%v assertions=%v", failures, matrix.Assertions)
	}
}

func viewDataRequireAuthority(t *testing.T, root string, authority viewDataAuthority) {
	t.Helper()
	if authority.Path == "" || authority.Test == "" || filepath.IsAbs(authority.Path) || strings.Contains(filepath.ToSlash(authority.Path), "../") {
		t.Fatalf("invalid ViewData authority: %+v", authority)
	}
	data, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(authority.Path)))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(data), authority.Test) {
		t.Fatalf("%s does not contain authority %q", authority.Path, authority.Test)
	}
}
