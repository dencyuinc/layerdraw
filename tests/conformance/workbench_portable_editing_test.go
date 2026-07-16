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

type portableWorkbenchCorpus struct {
	SchemaVersion      int      `json:"schema_version"`
	Corpus             string   `json:"corpus"`
	RequiredTransports []string `json:"required_transports"`
	Normalization      []string `json:"normalization"`
	Scenarios          []struct {
		Name   string `json:"name"`
		Source string `json:"source"`
		Patch  struct {
			ModulePath string `json:"module_path"`
			Find       string `json:"find"`
			Replace    string `json:"replace"`
		} `json:"patch"`
		Expected map[string]any `json:"expected"`
	} `json:"scenarios"`
}

func TestPortableWorkbenchEditingCorpusIsClosedAndExecutable(t *testing.T) {
	root := portableCompileRepositoryRoot(t)
	data, err := os.ReadFile(filepath.Join(root, "tests", "conformance", "testdata", "workbench_portable_editing_v1.json"))
	if err != nil {
		t.Fatal(err)
	}
	var corpus portableWorkbenchCorpus
	decoder := json.NewDecoder(strings.NewReader(string(data)))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&corpus); err != nil {
		t.Fatal(err)
	}
	if corpus.SchemaVersion != 1 || corpus.Corpus != "portable-workbench-editing" {
		t.Fatalf("workbench corpus identity drifted: %+v", corpus)
	}
	wantTransports := []string{
		"browser_wasm_worker",
		"engine_client_browser_wasm",
		"engine_client_stdio",
		"in_process_go",
		"node_wasm_worker",
		"packaged_stdio_raw",
	}
	gotTransports := slices.Clone(corpus.RequiredTransports)
	slices.Sort(gotTransports)
	if !slices.Equal(gotTransports, wantTransports) {
		t.Fatalf("workbench transport coverage = %v, want %v", gotTransports, wantTransports)
	}
	if len(corpus.Normalization) != 4 || len(corpus.Scenarios) != 1 {
		t.Fatalf("incomplete workbench corpus: normalization=%d scenarios=%d", len(corpus.Normalization), len(corpus.Scenarios))
	}
	for _, scenario := range corpus.Scenarios {
		if scenario.Name == "" || scenario.Source == "" || scenario.Patch.ModulePath == "" || scenario.Patch.Find == "" || scenario.Patch.Replace == "" {
			t.Fatalf("scenario is not executable: %+v", scenario)
		}
		for _, key := range []string{
			"open_state_kind",
			"open_semantic_state",
			"initial_generation",
			"preview_status",
			"changed_source_files",
			"applied_generation",
			"final_contains",
			"stale_apply_outcome",
			"stale_apply_diagnostic_code",
			"close_outcome",
		} {
			if _, ok := scenario.Expected[key]; !ok {
				t.Fatalf("%s missing expected assertion %s", scenario.Name, key)
			}
		}
	}
}
