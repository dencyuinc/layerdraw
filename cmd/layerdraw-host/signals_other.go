// SPDX-License-Identifier: LicenseRef-LayerDraw-1.0

//go:build !unix

package main

import "os"

func processSignals() []os.Signal  { return []os.Signal{os.Interrupt} }
func signalExitCode(os.Signal) int { return 130 }
