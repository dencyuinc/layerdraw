// SPDX-License-Identifier: LicenseRef-LayerDraw-1.0

//go:build !darwin

package desktopwails

import (
	"github.com/dencyuinc/layerdraw/internal/desktopcontract"
)

// Native credential brokers for other packaged targets are injected by their
// delivery composition until an OS implementation is linked.
func newPlatformCredentialPort() desktopcontract.CredentialPort { return unavailableCredentials{} }
