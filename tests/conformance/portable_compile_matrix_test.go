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

type portableCompileAuthority struct {
	Path string `json:"path"`
	Test string `json:"test"`
}

type portableCompileMatrix struct {
	SchemaVersion      int      `json:"schema_version"`
	Corpus             string   `json:"corpus"`
	RequiredTransports []string `json:"required_transports"`
	Assertions         []string `json:"assertions"`
	Transports         []struct {
		ID        string                   `json:"id"`
		CaseScope string                   `json:"case_scope"`
		Authority portableCompileAuthority `json:"authority"`
	} `json:"transports"`
	FailureClasses []struct {
		ID          string                     `json:"id"`
		Authorities []portableCompileAuthority `json:"authorities"`
	} `json:"failure_classes"`
}

func TestPortableCompileMatrixNamesExecutableAuthorities(t *testing.T) {
	root := portableCompileRepositoryRoot(t)
	data, err := os.ReadFile(filepath.Join(root, "tests", "conformance", "testdata", "portable_compile_matrix_v1.json"))
	if err != nil {
		t.Fatal(err)
	}
	var matrix portableCompileMatrix
	decoder := json.NewDecoder(strings.NewReader(string(data)))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&matrix); err != nil {
		t.Fatal(err)
	}
	if matrix.SchemaVersion != 1 || matrix.Corpus != "tests/conformance/testdata/engine_compile_parity_v1.json" {
		t.Fatalf("portable matrix identity drifted: %+v", matrix)
	}
	if _, err := os.Stat(filepath.Join(root, filepath.FromSlash(matrix.Corpus))); err != nil {
		t.Fatal(err)
	}
	wantTransports := []string{
		"browser_wasm_worker",
		"engine_client_browser_wasm",
		"engine_client_stdio",
		"in_process_go",
		"node_wasm_worker",
		"packaged_stdio_raw",
	}
	wantFailures := []string{"corrupt", "mismatched", "oversized", "stale", "truncated", "unsupported"}
	wantAssertions := []string{
		"canonical_output_bytes",
		"classifications",
		"definition_hash",
		"diagnostics",
		"normalized_response",
		"outcome",
		"output_blob_metadata",
		"recipes",
		"stable_addresses",
		"subject_semantic_hashes",
	}

	transportIDs := make([]string, 0, len(matrix.Transports))
	for _, transport := range matrix.Transports {
		transportIDs = append(transportIDs, transport.ID)
		if transport.CaseScope != "all" && transport.CaseScope != "compile" && transport.CaseScope != "compile_and_hard_cancel" {
			t.Fatalf("%s has unknown case scope %q", transport.ID, transport.CaseScope)
		}
		portableCompileRequireAuthority(t, root, transport.Authority)
	}
	failureIDs := make([]string, 0, len(matrix.FailureClasses))
	for _, failure := range matrix.FailureClasses {
		failureIDs = append(failureIDs, failure.ID)
		if len(failure.Authorities) == 0 {
			t.Fatalf("%s has no executable authority", failure.ID)
		}
		for _, authority := range failure.Authorities {
			portableCompileRequireAuthority(t, root, authority)
		}
	}
	slices.Sort(transportIDs)
	slices.Sort(failureIDs)
	slices.Sort(matrix.RequiredTransports)
	slices.Sort(matrix.Assertions)
	if !slices.Equal(transportIDs, wantTransports) || !slices.Equal(matrix.RequiredTransports, wantTransports) {
		t.Fatalf("portable transport closure drifted: transports=%v required=%v", transportIDs, matrix.RequiredTransports)
	}
	if !slices.Equal(failureIDs, wantFailures) {
		t.Fatalf("portable failure closure drifted: %v", failureIDs)
	}
	if !slices.Equal(matrix.Assertions, wantAssertions) {
		t.Fatalf("portable assertion closure drifted: %v", matrix.Assertions)
	}
}

func portableCompileRequireAuthority(t *testing.T, root string, authority portableCompileAuthority) {
	t.Helper()
	if authority.Path == "" || authority.Test == "" || filepath.IsAbs(authority.Path) || strings.Contains(filepath.ToSlash(authority.Path), "../") {
		t.Fatalf("invalid portable authority: %+v", authority)
	}
	data, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(authority.Path)))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(data), authority.Test) {
		t.Fatalf("%s does not contain authority %q", authority.Path, authority.Test)
	}
}

func portableCompileRepositoryRoot(t *testing.T) string {
	t.Helper()
	current, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	for {
		if _, err := os.Stat(filepath.Join(current, "go.mod")); err == nil {
			return current
		}
		parent := filepath.Dir(current)
		if parent == current {
			t.Fatal("repository root not found")
		}
		current = parent
	}
}
