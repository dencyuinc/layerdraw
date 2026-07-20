// SPDX-License-Identifier: LicenseRef-LayerDraw-1.0

//go:build darwin || linux

package desktopwails

import (
	"runtime"
	"syscall"
)

func isolatedWorkerPeakRSSMebibytes() (int64, error) {
	var usage syscall.Rusage
	if err := syscall.Getrusage(syscall.RUSAGE_SELF, &usage); err != nil {
		return 0, err
	}
	bytes := int64(usage.Maxrss) * 1024
	if runtime.GOOS == "darwin" {
		bytes = int64(usage.Maxrss)
	}
	result := (bytes + (1 << 20) - 1) / (1 << 20)
	if result < 1 {
		result = 1
	}
	return result, nil
}
