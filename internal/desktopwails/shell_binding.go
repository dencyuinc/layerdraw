// SPDX-License-Identifier: LicenseRef-LayerDraw-1.0

package desktopwails

import (
	"github.com/dencyuinc/layerdraw/internal/desktopapp"
	"github.com/dencyuinc/layerdraw/internal/desktopcontract"
)

// ShellBinding is the context-free Wails binding facade. Wails v2 treats a
// context.Context parameter as a JavaScript argument, so frontend bindings
// must acquire the lifecycle context from the production bridge instead of
// exposing it on the generated method surface.
type ShellBinding struct {
	shell         *desktopapp.NativeShell
	bridge        *WailsShellBridge
	packagedProbe bool
}

func newShellBinding(shell *desktopapp.NativeShell, bridge *WailsShellBridge, packagedProbe ...bool) *ShellBinding {
	return &ShellBinding{shell: shell, bridge: bridge, packagedProbe: len(packagedProbe) != 0 && packagedProbe[0]}
}

func (b *ShellBinding) PackagedProbeMode() bool { return b.packagedProbe }

func (b *ShellBinding) CommandStatus() desktopcontract.Result[[]desktopcontract.CommandStatus] {
	return b.shell.CommandStatus(b.bridge.context())
}

func (b *ShellBinding) InvokeCommand(input desktopcontract.CommandInvocation) desktopcontract.Result[desktopcontract.CommandStatus] {
	return b.shell.InvokeCommand(b.bridge.context(), input)
}

func (b *ShellBinding) Settings() desktopcontract.Result[desktopcontract.DesktopSettings] {
	return b.shell.CurrentSettings(b.bridge.context())
}

func (b *ShellBinding) UpdateSettings(settings desktopcontract.DesktopSettings) desktopcontract.Result[desktopcontract.DesktopSettings] {
	return b.shell.UpdateSettings(b.bridge.context(), settings)
}

// AccessibilityProbeReady forms the frontend/backend readiness handshake so
// packaged probes cannot emit before the DOM listener is installed.
func (b *ShellBinding) AccessibilityProbeReady() {
	b.bridge.markAccessibilityProbeReady()
}

func (b *ShellBinding) SubmitAccessibilityReport(id string, report desktopcontract.AccessibilityReport) error {
	return b.bridge.SubmitAccessibilityReport(id, report)
}
