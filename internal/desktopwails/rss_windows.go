// SPDX-License-Identifier: LicenseRef-LayerDraw-1.0

//go:build windows

package desktopwails

import (
	"unsafe"

	"golang.org/x/sys/windows"
)

type processMemoryCounters struct {
	CB                         uint32
	PageFaultCount             uint32
	PeakWorkingSetSize         uintptr
	WorkingSetSize             uintptr
	QuotaPeakPagedPoolUsage    uintptr
	QuotaPagedPoolUsage        uintptr
	QuotaPeakNonPagedPoolUsage uintptr
	QuotaNonPagedPoolUsage     uintptr
	PagefileUsage              uintptr
	PeakPagefileUsage          uintptr
}

var getProcessMemoryInfo = windows.NewLazySystemDLL("psapi.dll").NewProc("GetProcessMemoryInfo")

func isolatedWorkerPeakRSSMebibytes() (int64, error) {
	var counters processMemoryCounters
	counters.CB = uint32(unsafe.Sizeof(counters))
	result, _, callErr := getProcessMemoryInfo.Call(uintptr(windows.CurrentProcess()), uintptr(unsafe.Pointer(&counters)), uintptr(counters.CB))
	if result == 0 {
		return 0, callErr
	}
	value := (int64(counters.PeakWorkingSetSize) + (1 << 20) - 1) / (1 << 20)
	if value < 1 {
		value = 1
	}
	return value, nil
}
