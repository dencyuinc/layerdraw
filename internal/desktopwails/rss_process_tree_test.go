// SPDX-License-Identifier: LicenseRef-LayerDraw-1.0

package desktopwails

import (
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"slices"
	"testing"
	"time"
)

func TestPackagedUIProcessTreeMeasuresIsolatedUI(t *testing.T) {
	original := packagedUIProbeCommand
	t.Cleanup(func() { packagedUIProbeCommand = original })
	t.Setenv("LAYERDRAW_TEST_UI_PROCESS_HELPER", "1")
	packagedUIProbeCommand = func(ctx context.Context, _ string, output string) *exec.Cmd {
		t.Setenv("LAYERDRAW_TEST_UI_PROCESS_OUTPUT", output)
		return exec.CommandContext(ctx, os.Args[0], "-test.run=^TestPackagedUIProcessTreeHelper$")
	}
	peak, err := conformancePackagedUIProcessTree(context.Background())
	if err != nil || peak <= 0 {
		t.Fatalf("packaged UI process-tree peak=%dMiB err=%v", peak, err)
	}
}

func TestPackagedUIProcessTreeRejectsInvalidEvidenceAndStartFailure(t *testing.T) {
	original := packagedUIProbeCommand
	t.Cleanup(func() { packagedUIProbeCommand = original })
	t.Setenv("LAYERDRAW_TEST_UI_PROCESS_HELPER", "invalid")
	packagedUIProbeCommand = func(ctx context.Context, _ string, output string) *exec.Cmd {
		t.Setenv("LAYERDRAW_TEST_UI_PROCESS_OUTPUT", output)
		return exec.CommandContext(ctx, os.Args[0], "-test.run=^TestPackagedUIProcessTreeHelper$")
	}
	if peak, err := conformancePackagedUIProcessTree(context.Background()); err == nil || peak != 0 {
		t.Fatalf("invalid UI evidence peak=%dMiB err=%v", peak, err)
	}
	t.Setenv("LAYERDRAW_TEST_UI_PROCESS_HELPER", "fail")
	if peak, err := conformancePackagedUIProcessTree(context.Background()); err == nil || peak != 0 {
		t.Fatalf("failed UI process peak=%dMiB err=%v", peak, err)
	}
	t.Setenv("LAYERDRAW_TEST_UI_PROCESS_HELPER", "nooutput")
	if peak, err := conformancePackagedUIProcessTree(context.Background()); err == nil || peak != 0 {
		t.Fatalf("missing UI evidence peak=%dMiB err=%v", peak, err)
	}
	packagedUIProbeCommand = func(ctx context.Context, _, _ string) *exec.Cmd {
		return exec.CommandContext(ctx, "missing-layerdraw-packaged-ui-probe")
	}
	if peak, err := conformancePackagedUIProcessTree(context.Background()); err == nil || peak != 0 {
		t.Fatalf("missing UI process peak=%dMiB err=%v", peak, err)
	}
}

func TestProcessTreeMeasurementStopsOnCancellation(t *testing.T) {
	t.Setenv("LAYERDRAW_TEST_UI_PROCESS_HELPER", "sleep")
	command := exec.Command(os.Args[0], "-test.run=^TestPackagedUIProcessTreeHelper$")
	command.Env = os.Environ()
	if err := command.Start(); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if peak, err := measureProcessTreeUntilExit(ctx, command.Process.Pid, command); err == nil || peak != 0 {
		t.Fatalf("cancelled process-tree peak=%dMiB err=%v", peak, err)
	}
}

func TestPackagedUIProcessTreeHelper(t *testing.T) {
	mode := os.Getenv("LAYERDRAW_TEST_UI_PROCESS_HELPER")
	if mode == "sleep" {
		time.Sleep(time.Second)
		return
	}
	if mode == "fail" {
		time.Sleep(100 * time.Millisecond)
		os.Exit(2)
	}
	if mode == "nooutput" {
		time.Sleep(100 * time.Millisecond)
		return
	}
	if mode != "1" && mode != "invalid" {
		return
	}
	output := os.Getenv("LAYERDRAW_TEST_UI_PROCESS_OUTPUT")
	if mode == "invalid" {
		if err := os.WriteFile(output, []byte("{}\n"), 0o600); err != nil {
			t.Fatal("failed to publish invalid helper UI evidence")
		}
		time.Sleep(100 * time.Millisecond)
		return
	}
	encoded, err := json.Marshal(PackagedProbeResult{
		SchemaVersion: 1, Platform: CurrentPlatform(), DOMRoundTrip: true,
		UIMatrix: []PackagedUIProbeResult{{ID: "process-tree-helper"}},
	})
	if err != nil || os.WriteFile(output, append(encoded, '\n'), 0o600) != nil {
		t.Fatal("failed to publish helper UI evidence")
	}
	// Keep the process alive long enough for at least one native RSS snapshot.
	time.Sleep(100 * time.Millisecond)
}

func TestPackagedUIProbeEnvironmentClearsPersistentInstallerProbeState(t *testing.T) {
	environment := packagedUIProbeEnvironment([]string{
		"PATH=/bin", "LAYERDRAW_DESKTOP_PROBE_STATE_KEY=persistent",
		"layerdraw_desktop_probe_action=verify", "OTHER=value",
	})
	if !slices.Contains(environment, "PATH=/bin") || !slices.Contains(environment, "OTHER=value") ||
		!slices.Contains(environment, "LAYERDRAW_DESKTOP_PROBE_STATE_KEY=") ||
		!slices.Contains(environment, "LAYERDRAW_DESKTOP_PROBE_ACTION=") {
		t.Fatalf("sanitized environment=%v", environment)
	}
	for _, entry := range environment {
		if entry == "LAYERDRAW_DESKTOP_PROBE_STATE_KEY=persistent" || entry == "layerdraw_desktop_probe_action=verify" {
			t.Fatalf("persistent installer probe state survived: %q", entry)
		}
	}
}

func TestProcessTreeRSSIncludesOnlyRootAndTransitiveDescendants(t *testing.T) {
	table := map[int]processRSS{
		10: {parentPID: 1, rssKiB: 100, measured: true},
		11: {parentPID: 10, rssKiB: 40, measured: true},
		12: {parentPID: 11, rssKiB: 20, measured: true},
		13: {parentPID: 10, rssKiB: 10, measured: true},
		20: {parentPID: 1, rssKiB: 999, measured: true},
		21: {parentPID: 20, rssKiB: 999, measured: true},
	}
	if got, complete := processTreeRSSKiB(table, 10); got != 170 || !complete {
		t.Fatalf("process-tree RSS=%dKiB, want 170KiB", got)
	}
	if got, complete := processTreeRSSKiB(table, 99); got != 0 || complete {
		t.Fatalf("missing root RSS=%dKiB", got)
	}
	table[12] = processRSS{parentPID: 11}
	if got, complete := processTreeRSSKiB(table, 10); got != 0 || complete {
		t.Fatalf("partial process-tree measurement=%dKiB complete=%v", got, complete)
	}
}

func TestProcessSnapshotMeasuresCurrentProcess(t *testing.T) {
	table, err := snapshotProcessRSS()
	if err != nil {
		t.Fatal(err)
	}
	current, ok := table[os.Getpid()]
	if !ok || current.rssKiB == 0 || !current.measured {
		t.Fatalf("current process measurement=%+v present=%v", current, ok)
	}
}
