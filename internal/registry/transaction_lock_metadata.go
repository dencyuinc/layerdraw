// SPDX-License-Identifier: LicenseRef-LayerDraw-1.0

package registry

import (
	"encoding/json"
	"io"
	"os"
	"time"
)

func writeTransactionLockMetadata(file *os.File) {
	metadata, _ := json.Marshal(struct {
		PID        int       `json:"pid"`
		AcquiredAt time.Time `json:"acquired_at"`
	}{os.Getpid(), time.Now().UTC()})
	// Metadata is diagnostic only. The OS lock is authoritative.
	_ = file.Truncate(0)
	_, _ = file.Seek(0, io.SeekStart)
	_, _ = file.Write(metadata)
	_ = file.Sync()
}
