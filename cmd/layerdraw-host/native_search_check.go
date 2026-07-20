// SPDX-License-Identifier: LicenseRef-LayerDraw-1.0
//go:build !ladybug_native

package main

import (
	"context"
	"io"

	hostendpoint "github.com/dencyuinc/layerdraw/internal/host"
)

func runNativeSearchCheck([]string, io.Writer, io.Writer) (bool, int) { return false, 0 }

func openLocalEndpoint(config hostendpoint.LocalConfig) (*hostendpoint.Endpoint, func(context.Context) error, error) {
	return hostendpoint.NewLocal(config)
}
