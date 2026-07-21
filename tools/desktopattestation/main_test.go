// SPDX-License-Identifier: LicenseRef-LayerDraw-1.0

package main

import (
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestSignedAttestationBindsInstalledResultsAndArtifacts(t *testing.T) {
	root := t.TempDir()
	revision := strings.Repeat("a", 40)
	closurePath := filepath.Join(root, "desktop-conformance.json")
	budgets := map[string]budget{"memory": {MaxMebibytes: 512, MinIterations: 5, Percentile: 95}, "ui_process_tree_memory": {MaxMebibytes: 1024, MinIterations: 5, Percentile: 95}}
	for _, id := range scenarioIDs {
		budgets[id] = budget{MaxMilliseconds: 100, MinIterations: 5, Percentile: 95}
	}
	writeJSON(t, closurePath, closure{SchemaVersion: 1, Delivery: "desktop", PerformanceBudgets: budgets})
	result := scenarioResult{SchemaVersion: 1, SourceRevision: revision, Platform: "linux", ArtifactKind: "installed_desktop", Iterations: 5, Scenarios: map[string]samples{}, Evidence: map[string]string{}, WorkerPeakRSS: []int64{100, 101, 102, 103, 104}, UIProcessTreePeakRSS: []int64{300, 301, 302, 303, 304}}
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
	verifyArgs := []string{"verify", "-attestation", attestationPath, "-root", root, "-source-revision", revision, "-platform", "linux", "-allow-test-signing"}
	if err := run(verifyArgs); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(installer, []byte("tampered"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := run(verifyArgs); err == nil || !strings.Contains(err.Error(), "digest mismatch") {
		t.Fatalf("tampered installer accepted: %v", err)
	}
}

func TestSignedAttestationSucceedsAndRecordsPerformanceBudgetOverrun(t *testing.T) {
	root := t.TempDir()
	revision := strings.Repeat("a", 40)
	closurePath := filepath.Join(root, "desktop-conformance.json")
	budgets := map[string]budget{"memory": {MaxMebibytes: 512, MinIterations: 5, Percentile: 95}, "ui_process_tree_memory": {MaxMebibytes: 1024, MinIterations: 5, Percentile: 95}}
	for _, id := range scenarioIDs {
		budgets[id] = budget{MaxMilliseconds: 100, MinIterations: 5, Percentile: 95}
	}
	writeJSON(t, closurePath, closure{SchemaVersion: 1, Delivery: "desktop", PerformanceBudgets: budgets})
	result := scenarioResult{SchemaVersion: 1, SourceRevision: revision, Platform: "linux", ArtifactKind: "installed_desktop", Iterations: 5, Scenarios: map[string]samples{}, Evidence: map[string]string{}, WorkerPeakRSS: []int64{500, 510, 520, 530, 540}, UIProcessTreePeakRSS: []int64{300, 301, 302, 303, 304}}
	for _, id := range scenarioIDs {
		result.Scenarios[id] = samples{Milliseconds: []int64{10, 11, 12, 13, 14}}
		result.Evidence[id] = expectedEvidence[id]
	}
	result.Scenarios["cold_start"] = samples{Milliseconds: []int64{90, 95, 101, 110, 150}}
	resultPath := filepath.Join(root, "scenario.json")
	writeJSON(t, resultPath, result)
	installer := filepath.Join(root, "LayerDraw.deb")
	if err := os.WriteFile(installer, []byte("installer"), 0o600); err != nil {
		t.Fatal(err)
	}
	attestationPath := filepath.Join(root, "attestation.json")
	var createErr, verifyErr error
	stderr := captureStderr(t, func() {
		createErr = run([]string{"create", "-installer", installer, "-closure", closurePath, "-scenario-result", resultPath, "-output", attestationPath, "-source-revision", revision, "-platform", "linux", "-test-signing"})
	})
	if createErr != nil {
		t.Fatalf("performance budget overrun must not fail attestation creation: %v", createErr)
	}
	if !strings.Contains(stderr, "warning: scenario cold_start p95 150ms exceeds informational budget 100ms") ||
		!strings.Contains(stderr, "warning: isolated worker p95 RSS 540MiB exceeds informational budget 512MiB") {
		t.Fatalf("expected performance overrun warnings during create, got %q", stderr)
	}
	verifyArgs := []string{"verify", "-attestation", attestationPath, "-root", root, "-source-revision", revision, "-platform", "linux", "-allow-test-signing"}
	stderr = captureStderr(t, func() {
		verifyErr = run(verifyArgs)
	})
	if verifyErr != nil {
		t.Fatalf("performance budget overrun must not fail attestation verification: %v", verifyErr)
	}
	if !strings.Contains(stderr, "warning: scenario cold_start p95 150ms exceeds informational budget 100ms") {
		t.Fatalf("expected performance overrun warning during verify, got %q", stderr)
	}
	recorded, err := os.ReadFile(resultPath)
	if err != nil {
		t.Fatal(err)
	}
	var recordedResult scenarioResult
	if err := json.Unmarshal(recorded, &recordedResult); err != nil {
		t.Fatal(err)
	}
	if recordedResult.Scenarios["cold_start"].Milliseconds[4] != 150 || recordedResult.WorkerPeakRSS[4] != 540 {
		t.Fatalf("attested scenario result did not retain observed measurements: %+v", recordedResult)
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

func TestMemoryBudgetOverrunIsWarnedNotFailed(t *testing.T) {
	root := t.TempDir()
	store, err := openArtifactStore(root)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	budgets := map[string]budget{"memory": {MaxMebibytes: 512, MinIterations: 5, Percentile: 95}, "ui_process_tree_memory": {MaxMebibytes: 1024, MinIterations: 5, Percentile: 95}}
	for _, id := range scenarioIDs {
		budgets[id] = budget{MaxMilliseconds: 100, MinIterations: 5, Percentile: 95}
	}
	writeJSON(t, filepath.Join(root, "closure.json"), closure{SchemaVersion: 1, Delivery: "desktop", PerformanceBudgets: budgets})
	revision := strings.Repeat("a", 40)
	result := scenarioResult{SchemaVersion: 1, SourceRevision: revision, Platform: "linux", ArtifactKind: "installed_desktop", Iterations: 5, Scenarios: map[string]samples{}, Evidence: map[string]string{}, WorkerPeakRSS: []int64{500, 510, 520, 530, 540}, UIProcessTreePeakRSS: []int64{300, 301, 302, 303, 304}}
	for _, id := range scenarioIDs {
		result.Scenarios[id] = samples{Milliseconds: []int64{1, 2, 3, 4, 5}}
		result.Evidence[id] = expectedEvidence[id]
	}
	writeJSON(t, filepath.Join(root, "result.json"), result)
	stderr := captureStderr(t, func() {
		err = validateResult(store, "closure.json", "result.json", revision, "linux")
	})
	if err != nil {
		t.Fatalf("worker RSS overrun must not fail validation: %v", err)
	}
	if !strings.Contains(stderr, "warning: isolated worker p95 RSS 540MiB exceeds informational budget 512MiB") ||
		!strings.Contains(stderr, "[500 510 520 530 540]") {
		t.Fatalf("expected worker RSS warning with observed samples, got %q", stderr)
	}

	result.WorkerPeakRSS = []int64{100, 101, 102, 103, 104}
	result.UIProcessTreePeakRSS = []int64{900, 950, 1000, 1020, 1030}
	writeJSON(t, filepath.Join(root, "ui-result.json"), result)
	stderr = captureStderr(t, func() {
		err = validateResult(store, "closure.json", "ui-result.json", revision, "linux")
	})
	if err != nil {
		t.Fatalf("UI process-tree RSS overrun must not fail validation: %v", err)
	}
	if !strings.Contains(stderr, "warning: packaged UI process-tree p95 RSS 1030MiB exceeds informational budget 1024MiB") ||
		!strings.Contains(stderr, "[900 950 1000 1020 1030]") {
		t.Fatalf("expected UI process-tree RSS warning with observed samples, got %q", stderr)
	}
}

func TestScenarioBudgetOverrunIsWarnedNotFailed(t *testing.T) {
	root := t.TempDir()
	store, err := openArtifactStore(root)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	budgets := map[string]budget{"memory": {MaxMebibytes: 512, MinIterations: 5, Percentile: 95}, "ui_process_tree_memory": {MaxMebibytes: 1024, MinIterations: 5, Percentile: 95}}
	for _, id := range scenarioIDs {
		budgets[id] = budget{MaxMilliseconds: 100, MinIterations: 5, Percentile: 95}
	}
	writeJSON(t, filepath.Join(root, "closure.json"), closure{SchemaVersion: 1, Delivery: "desktop", PerformanceBudgets: budgets})
	revision := strings.Repeat("a", 40)
	result := scenarioResult{SchemaVersion: 1, SourceRevision: revision, Platform: "windows", ArtifactKind: "installed_desktop", Iterations: 5, Scenarios: map[string]samples{}, Evidence: map[string]string{}, WorkerPeakRSS: []int64{100, 101, 102, 103, 104}, UIProcessTreePeakRSS: []int64{300, 301, 302, 303, 304}}
	for _, id := range scenarioIDs {
		result.Scenarios[id] = samples{Milliseconds: []int64{10, 11, 12, 13, 14}}
		result.Evidence[id] = expectedEvidence[id]
	}
	result.Scenarios["cold_start"] = samples{Milliseconds: []int64{90, 95, 101, 110, 150}}
	writeJSON(t, filepath.Join(root, "result.json"), result)
	var stderr string
	stderr = captureStderr(t, func() {
		err = validateResult(store, "closure.json", "result.json", revision, "windows")
	})
	if err != nil {
		t.Fatalf("scenario p95 overrun must not fail validation: %v", err)
	}
	wantWarning := "warning: scenario cold_start p95 150ms exceeds informational budget 100ms (samples: [90 95 101 110 150])"
	if !strings.Contains(stderr, wantWarning) {
		t.Fatalf("stderr=%q, want to contain %q", stderr, wantWarning)
	}
}

func TestScenarioMissingCoverageStillFails(t *testing.T) {
	root := t.TempDir()
	store, err := openArtifactStore(root)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	budgets := map[string]budget{"memory": {MaxMebibytes: 512, MinIterations: 5, Percentile: 95}, "ui_process_tree_memory": {MaxMebibytes: 1024, MinIterations: 5, Percentile: 95}}
	for _, id := range scenarioIDs {
		budgets[id] = budget{MaxMilliseconds: 100, MinIterations: 5, Percentile: 95}
	}
	writeJSON(t, filepath.Join(root, "closure.json"), closure{SchemaVersion: 1, Delivery: "desktop", PerformanceBudgets: budgets})
	revision := strings.Repeat("a", 40)
	result := scenarioResult{SchemaVersion: 1, SourceRevision: revision, Platform: "windows", ArtifactKind: "installed_desktop", Iterations: 5, Scenarios: map[string]samples{}, Evidence: map[string]string{}, WorkerPeakRSS: []int64{100, 101, 102, 103, 104}, UIProcessTreePeakRSS: []int64{300, 301, 302, 303, 304}}
	for _, id := range scenarioIDs {
		result.Scenarios[id] = samples{Milliseconds: []int64{10, 11, 12, 13, 14}}
		result.Evidence[id] = expectedEvidence[id]
	}
	delete(result.Scenarios, "cold_start")
	delete(result.Evidence, "cold_start")
	writeJSON(t, filepath.Join(root, "result.json"), result)
	if err := validateResult(store, "closure.json", "result.json", revision, "windows"); err == nil {
		t.Fatal("missing required scenario coverage was accepted")
	}
}

func TestScenarioInsufficientIterationsStillFails(t *testing.T) {
	root := t.TempDir()
	store, err := openArtifactStore(root)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	budgets := map[string]budget{"memory": {MaxMebibytes: 512, MinIterations: 5, Percentile: 95}, "ui_process_tree_memory": {MaxMebibytes: 1024, MinIterations: 5, Percentile: 95}}
	for _, id := range scenarioIDs {
		budgets[id] = budget{MaxMilliseconds: 100, MinIterations: 5, Percentile: 95}
	}
	writeJSON(t, filepath.Join(root, "closure.json"), closure{SchemaVersion: 1, Delivery: "desktop", PerformanceBudgets: budgets})
	revision := strings.Repeat("a", 40)
	result := scenarioResult{SchemaVersion: 1, SourceRevision: revision, Platform: "windows", ArtifactKind: "installed_desktop", Iterations: 4, Scenarios: map[string]samples{}, Evidence: map[string]string{}, WorkerPeakRSS: []int64{100, 101, 102, 103}, UIProcessTreePeakRSS: []int64{300, 301, 302, 303}}
	for _, id := range scenarioIDs {
		result.Scenarios[id] = samples{Milliseconds: []int64{10, 11, 12, 13}}
		result.Evidence[id] = expectedEvidence[id]
	}
	writeJSON(t, filepath.Join(root, "result.json"), result)
	if err := validateResult(store, "closure.json", "result.json", revision, "windows"); err == nil {
		t.Fatal("insufficient iteration count was accepted")
	}
}

func TestReleaseClosureDecodesStrictly(t *testing.T) {
	store, err := openArtifactStore(filepath.Join("..", "..", "deploy"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	var value closure
	if err := decodeStrict(store, "desktop-conformance.json", &value); err != nil {
		t.Fatal(err)
	}
	if value.Delivery != "desktop" || len(value.PerformanceBudgets) != 12 {
		t.Fatalf("closure=%+v", value)
	}
}

func TestArtifactStoreRejectsTraversalAndSymlinkInputs(t *testing.T) {
	root := t.TempDir()
	store, err := openArtifactStore(root)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	if _, err := store.directName(filepath.Join(root, "..", "outside.json")); err == nil {
		t.Fatal("parent traversal was accepted")
	}
	outside := filepath.Join(t.TempDir(), "outside.json")
	if err := os.WriteFile(outside, []byte(`{}`), 0o600); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(root, "linked.json")
	if err := os.Symlink(outside, link); err != nil {
		t.Skipf("symbolic links unavailable: %v", err)
	}
	if _, err := describeFile(store, "linked.json"); err == nil {
		t.Fatal("symbolic-link artifact was accepted")
	}
	if err := decodeStrict(store, "linked.json", new(map[string]any)); err == nil {
		t.Fatal("symbolic-link JSON input was accepted")
	}
}

func captureStderr(t *testing.T, fn func()) string {
	t.Helper()
	original := os.Stderr
	reader, writer, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	os.Stderr = writer
	fn()
	os.Stderr = original
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}
	captured, err := io.ReadAll(reader)
	if err != nil {
		t.Fatal(err)
	}
	return string(captured)
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
