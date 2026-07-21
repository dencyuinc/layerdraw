// SPDX-License-Identifier: LicenseRef-LayerDraw-1.0

package desktopwails

import (
	"os"
	"slices"
	"testing"
)

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
