// SPDX-License-Identifier: LicenseRef-LayerDraw-1.0

//go:build windows

package desktopwails

import (
	"errors"
	"unsafe"

	"golang.org/x/sys/windows"
)

func snapshotProcessRSS() (map[int]processRSS, error) {
	snapshot, err := windows.CreateToolhelp32Snapshot(windows.TH32CS_SNAPPROCESS, 0)
	if err != nil {
		return nil, err
	}
	defer windows.CloseHandle(snapshot)
	table := map[int]processRSS{}
	entry := windows.ProcessEntry32{Size: uint32(unsafe.Sizeof(windows.ProcessEntry32{}))}
	for err = windows.Process32First(snapshot, &entry); err == nil; err = windows.Process32Next(snapshot, &entry) {
		if entry.ProcessID == 0 {
			continue
		}
		process := processRSS{parentPID: int(entry.ParentProcessID)}
		handle, openErr := windows.OpenProcess(windows.PROCESS_QUERY_LIMITED_INFORMATION, false, entry.ProcessID)
		if openErr == nil {
			counters := processMemoryCounters{CB: uint32(unsafe.Sizeof(processMemoryCounters{}))}
			result, _, _ := getProcessMemoryInfo.Call(uintptr(handle), uintptr(unsafe.Pointer(&counters)), uintptr(counters.CB))
			_ = windows.CloseHandle(handle)
			if result != 0 {
				process.rssKiB = uint64(counters.WorkingSetSize) / 1024
				process.measured = process.rssKiB > 0
			}
		}
		table[int(entry.ProcessID)] = process
	}
	if len(table) == 0 {
		return nil, errors.New("process table is empty")
	}
	return table, nil
}
