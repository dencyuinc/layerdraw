// SPDX-License-Identifier: LicenseRef-LayerDraw-1.0

package desktopwails

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

type processRSS struct {
	parentPID int
	rssKiB    uint64
	measured  bool
}

var packagedUIProbeCommand = func(ctx context.Context, executable, output string) *exec.Cmd {
	return exec.CommandContext(ctx, executable, "--packaged-ui-probe", output)
}

func conformancePackagedUIProcessTree(ctx context.Context) (int64, error) {
	executable, err := os.Executable()
	if err != nil {
		return 0, errors.New("installed Desktop executable is unavailable")
	}
	root, err := os.MkdirTemp("", "layerdraw-packaged-ui-process-tree-*")
	if err != nil {
		return 0, err
	}
	defer os.RemoveAll(root)
	output := filepath.Join(root, "ui-probe.json")
	command := packagedUIProbeCommand(ctx, executable, output)
	command.Stdout, command.Stderr = io.Discard, io.Discard
	// The UI process owns one transient state root. Clear caller-provided probe
	// state and action together: PowerShell installer smoke variables persist in
	// the process environment, unlike the command-scoped Unix assignments.
	command.Env = packagedUIProbeEnvironment(os.Environ())
	if err := command.Start(); err != nil {
		return 0, errors.New("packaged Desktop UI process failed to start")
	}
	peak, waitErr := measureProcessTreeUntilExit(ctx, command.Process.Pid, command)
	if waitErr != nil {
		return 0, errors.New("packaged Desktop UI process failed")
	}
	data, err := os.ReadFile(output)
	if err != nil {
		return 0, errors.New("packaged Desktop UI probe result is unavailable")
	}
	var result PackagedProbeResult
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	if decoder.Decode(&result) != nil || decoder.Decode(new(any)) != io.EOF || !result.DOMRoundTrip || result.Failure != nil || len(result.UIMatrix) == 0 || peak <= 0 {
		return 0, errors.New("packaged Desktop UI process-tree evidence is invalid")
	}
	return peak, nil
}

func packagedUIProbeEnvironment(base []string) []string {
	const state = "LAYERDRAW_DESKTOP_PROBE_STATE_KEY"
	const action = "LAYERDRAW_DESKTOP_PROBE_ACTION"
	result := make([]string, 0, len(base)+2)
	for _, entry := range base {
		name, _, found := strings.Cut(entry, "=")
		if found && (strings.EqualFold(name, state) || strings.EqualFold(name, action)) {
			continue
		}
		result = append(result, entry)
	}
	return append(result, state+"=", action+"=")
}

func measureProcessTreeUntilExit(ctx context.Context, rootPID int, command *exec.Cmd) (int64, error) {
	done := make(chan error, 1)
	go func() { done <- command.Wait() }()
	ticker := time.NewTicker(10 * time.Millisecond)
	defer ticker.Stop()
	var peakKiB uint64
	for {
		if table, err := snapshotProcessRSS(); err == nil {
			if current, complete := processTreeRSSKiB(table, rootPID); complete && current > peakKiB {
				peakKiB = current
			}
		}
		select {
		case err := <-done:
			if peakKiB == 0 {
				return 0, errors.New("process-tree RSS was unavailable")
			}
			return int64((peakKiB + 1023) / 1024), err
		case <-ctx.Done():
			_ = command.Process.Kill()
			<-done
			return 0, ctx.Err()
		case <-ticker.C:
		}
	}
}

func processTreeRSSKiB(table map[int]processRSS, rootPID int) (uint64, bool) {
	if _, ok := table[rootPID]; !ok {
		return 0, false
	}
	included := map[int]bool{rootPID: true}
	for changed := true; changed; {
		changed = false
		for pid, process := range table {
			if !included[pid] && included[process.parentPID] {
				included[pid] = true
				changed = true
			}
		}
	}
	var total uint64
	for pid := range included {
		process := table[pid]
		if !process.measured || process.rssKiB == 0 {
			return 0, false
		}
		total += process.rssKiB
	}
	return total, true
}
