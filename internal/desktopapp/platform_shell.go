// SPDX-License-Identifier: LicenseRef-LayerDraw-1.0

package desktopapp

import (
	"errors"
	"path/filepath"
	"time"

	desktopadapter "github.com/dencyuinc/layerdraw/internal/adapter/desktop"
	"github.com/dencyuinc/layerdraw/internal/desktopcontract"
)

type PlatformNativeShellConfig struct {
	Platform      desktopcontract.DesktopPlatform
	StateRoot     string
	Runtime       desktopadapter.WailsRuntimeBridge
	Commands      desktopcontract.CommandRouter
	CrashRecovery desktopcontract.CrashRecoveryPort
	Errors        desktopcontract.ErrorSurfacePort
	Now           func() time.Time
}

// PlatformNativeShell is the production composition consumed by the Wails
// application. Its OS adapters are concrete; owner command and project crash
// recovery ports remain injected from #124 and #123 respectively.
type PlatformNativeShell struct {
	Shell        *NativeShell
	Associations *desktopadapter.AssociationBroker
}

func NewPlatformNativeShell(config PlatformNativeShellConfig) (*PlatformNativeShell, error) {
	if config.StateRoot == "" || !filepath.IsAbs(config.StateRoot) || filepath.Clean(config.StateRoot) != config.StateRoot {
		return nil, errors.New("desktop platform state root must be clean and absolute")
	}
	runtimeAdapter, err := desktopadapter.NewWailsRuntimeAdapter(config.Runtime)
	if err != nil {
		return nil, err
	}
	settings, err := desktopadapter.NewAtomicSettingsStore(filepath.Join(config.StateRoot, "settings-v1.json"))
	if err != nil {
		return nil, err
	}
	opener, err := desktopadapter.NewSystemExternalOpener(config.Platform)
	if err != nil {
		return nil, err
	}
	logs, err := desktopadapter.NewJSONLogStore(filepath.Join(config.StateRoot, "logs", "desktop.jsonl"))
	if err != nil {
		return nil, err
	}
	associations := desktopadapter.NewAssociationBroker()
	shell, err := NewNativeShell(NativeShellConfig{
		Platform: config.Platform, Settings: settings, Window: runtimeAdapter,
		Commands: config.Commands, External: opener, Associations: associations,
		CrashRecovery: config.CrashRecovery, Errors: config.Errors,
		Accessibility: runtimeAdapter, Logs: logs, Now: config.Now,
	})
	if err != nil {
		return nil, err
	}
	return &PlatformNativeShell{Shell: shell, Associations: associations}, nil
}
