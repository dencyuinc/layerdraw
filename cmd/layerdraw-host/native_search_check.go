// SPDX-License-Identifier: LicenseRef-LayerDraw-1.0
//go:build !ladybug_native

package main

import "io"

func runNativeSearchCheck([]string, io.Writer, io.Writer) (bool, int) { return false, 0 }
