//go:build unix

// SPDX-License-Identifier: LicenseRef-LayerDraw-1.0

package main

import (
	"os"
	"syscall"
)

func processSignals() []os.Signal { return []os.Signal{os.Interrupt, syscall.SIGTERM} }

func signalExitCode(signal os.Signal) int {
	if signal == syscall.SIGTERM {
		return 143
	}
	return 130
}
