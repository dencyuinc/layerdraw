// SPDX-License-Identifier: LicenseRef-LayerDraw-1.0

package desktopwails

import (
	"os"
	"testing"
)

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
