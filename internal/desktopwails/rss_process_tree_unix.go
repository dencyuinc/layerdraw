// SPDX-License-Identifier: LicenseRef-LayerDraw-1.0

//go:build !windows

package desktopwails

import (
	"bytes"
	"errors"
	"os/exec"
	"strconv"
)

func snapshotProcessRSS() (map[int]processRSS, error) {
	output, err := exec.Command("ps", "-axo", "pid=,ppid=,rss=").Output()
	if err != nil {
		return nil, err
	}
	table := map[int]processRSS{}
	for _, line := range bytes.Split(output, []byte{'\n'}) {
		fields := bytes.Fields(line)
		if len(fields) != 3 {
			continue
		}
		pid, pidErr := strconv.Atoi(string(fields[0]))
		parent, parentErr := strconv.Atoi(string(fields[1]))
		rss, rssErr := strconv.ParseUint(string(fields[2]), 10, 64)
		if pidErr == nil && parentErr == nil && rssErr == nil && pid > 0 {
			table[pid] = processRSS{parentPID: parent, rssKiB: rss, measured: rss > 0}
		}
	}
	if len(table) == 0 {
		return nil, errors.New("process table is empty")
	}
	return table, nil
}
