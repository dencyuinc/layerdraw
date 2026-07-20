// SPDX-License-Identifier: LicenseRef-LayerDraw-1.0
//go:build !ladybug_native

package desktopwails

import (
	"context"

	"github.com/dencyuinc/layerdraw/internal/mcphost"
)

func conformanceNativeMCP(context.Context, *conformanceInstance, []mcphost.Tool) error { return nil }
