// SPDX-License-Identifier: LicenseRef-LayerDraw-1.0

package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestSignedAttestationBindsInstalledResultsAndArtifacts(t *testing.T) {
	root := t.TempDir()
	revision := strings.Repeat("a", 40)
	closurePath := filepath.Join(root, "desktop-conformance.json")
	budgets := map[string]budget{"memory": {MaxMebibytes: 512, MinIterations: 5, Percentile: 95}}
	for _, id := range scenarioIDs {
		budgets[id] = budget{MaxMilliseconds: 100, MinIterations: 5, Percentile: 95}
	}
	writeJSON(t, closurePath, closure{SchemaVersion: 1, Delivery: "desktop", PerformanceBudgets: budgets})
	result := scenarioResult{SchemaVersion: 1, SourceRevision: revision, Platform: "linux", ArtifactKind: "installed_desktop", Iterations: 5, Scenarios: map[string]samples{}, Evidence: map[string]string{}, PeakRSSMiB: []int64{100, 101, 102, 103, 104}}
	for _, id := range scenarioIDs {
		result.Scenarios[id] = samples{Milliseconds: []int64{10, 11, 12, 13, 14}}
		result.Evidence[id] = expectedEvidence[id]
	}
	resultPath := filepath.Join(root, "scenario.json")
	writeJSON(t, resultPath, result)
	installer := filepath.Join(root, "LayerDraw.deb")
	if err := os.WriteFile(installer, []byte("installer"), 0o600); err != nil {
		t.Fatal(err)
	}
	attestationPath := filepath.Join(root, "attestation.json")
	if err := run([]string{"create", "-installer", installer, "-closure", closurePath, "-scenario-result", resultPath, "-output", attestationPath, "-source-revision", revision, "-platform", "linux", "-test-signing"}); err != nil {
		t.Fatal(err)
	}
	if err := run([]string{"verify", "-attestation", attestationPath, "-root", root, "-allow-test-signing"}); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(installer, []byte("tampered"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := run([]string{"verify", "-attestation", attestationPath, "-root", root, "-allow-test-signing"}); err == nil || !strings.Contains(err.Error(), "digest mismatch") {
		t.Fatalf("tampered installer accepted: %v", err)
	}
}

func TestResultRejectsShallowOrSelfAssertedMeasurements(t *testing.T) {
	values := []int64{1, 2, 3, 4, 101}
	if observed, err := percentile95(values); err != nil || observed != 101 {
		t.Fatalf("p95=%d err=%v", observed, err)
	}
	if _, err := percentile95([]int64{1, 0, 2}); err == nil {
		t.Fatal("zero measurement accepted")
	}
}

func writeJSON(t *testing.T, path string, value any) {
	t.Helper()
	data, err := json.Marshal(value)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, data, 0o600); err != nil {
		t.Fatal(err)
	}
}
