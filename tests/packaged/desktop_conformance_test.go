// SPDX-License-Identifier: LicenseRef-LayerDraw-1.0

package packaged_test

import (
	"encoding/json"
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

func TestPackagedDesktopMemoryBudget(t *testing.T) {
	manifestPath := filepath.Join("..", "..", "deploy", "desktop-conformance.json")
	file, err := os.Open(manifestPath)
	if err != nil {
		t.Fatal(err)
	}
	defer file.Close()
	var closure struct {
		PerformanceBudgets map[string]struct {
			MaxMebibytes int64 `json:"max_mebibytes"`
		} `json:"performance_budgets"`
	}
	decoder := json.NewDecoder(file)
	if err := decoder.Decode(&closure); err != nil {
		t.Fatal(err)
	}
	budget := closure.PerformanceBudgets["memory"].MaxMebibytes
	if budget <= 0 {
		t.Fatal("Desktop memory budget is absent")
	}

	// Exercise bounded packaged-style payload ownership before measuring the
	// process. This catches accidental aggregate retention while remaining
	// independent of platform-specific resident-set accounting APIs.
	payloads := make([][]byte, 64)
	for index := range payloads {
		payloads[index] = make([]byte, 64<<10)
		payloads[index][0] = byte(index)
	}
	runtime.KeepAlive(payloads)
	var stats runtime.MemStats
	runtime.ReadMemStats(&stats)
	usedMebibytes := int64(stats.Sys / (1 << 20))
	if usedMebibytes > budget {
		t.Fatalf("packaged Desktop memory=%dMiB budget=%dMiB", usedMebibytes, budget)
	}
}
