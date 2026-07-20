// SPDX-License-Identifier: LicenseRef-LayerDraw-1.0

// Package desktopapp composes the trusted in-process Desktop backend. It owns
// lifecycle and framework wiring only; capability semantics remain in their
// owner packages.
package desktopapp

import (
	"context"

	"github.com/dencyuinc/layerdraw/gen/go/protocolcommon"
	"github.com/dencyuinc/layerdraw/internal/desktopcontract"
)

// Adapter is the lifecycle surface implemented by a Desktop capability
// adapter. Start and Shutdown errors are deliberately mapped to closed Desktop
// failures before they can cross the Wails boundary.
type Adapter interface {
	Start(context.Context) error
	Shutdown(context.Context) error
}

// ProjectLocation is a trusted backend-only local project reference. Native
// paths never appear in a Wails request or response.
type ProjectLocation struct {
	Root      string
	EntryPath string
}

// ProjectStorage resolves opaque native-dialog tokens and owns project
// creation. It must not parse or rewrite LDL; it only returns a trusted local
// location that the Engine will compile.
type ProjectStorage interface {
	Create(context.Context, string) (ProjectLocation, error)
	Open(context.Context, string) (ProjectLocation, error)
}

// CapabilityNegotiator returns the generated common handshake produced from
// the actually wired adapters, providers and policy. The composition root
// validates it against the frozen Desktop manifest before publishing ready.
type CapabilityNegotiator interface {
	Negotiate(context.Context, desktopcontract.Manifest) (protocolcommon.HandshakeResult, error)
}

// RecoveryReporter is an optional backend-only diagnostic sink. It receives
// only closed failure values and never underlying errors, paths or content.
type RecoveryReporter interface {
	Report(context.Context, desktopcontract.Failure)
}
