// SPDX-License-Identifier: LicenseRef-LayerDraw-1.0

//go:build linux

package desktopwails

import "github.com/dencyuinc/layerdraw/internal/desktopcontract"

func CurrentPlatform() desktopcontract.DesktopPlatform { return desktopcontract.PlatformLinux }
