// SPDX-License-Identifier: LicenseRef-LayerDraw-1.0

package desktopwails

import (
	"context"
	"errors"
	"io/fs"
	"time"

	"github.com/dencyuinc/layerdraw/gen/go/protocolcommon"
	"github.com/dencyuinc/layerdraw/internal/desktopapp"
	"github.com/dencyuinc/layerdraw/internal/desktopcontract"
	"github.com/wailsapp/wails/v2"
	"github.com/wailsapp/wails/v2/pkg/options"
	"github.com/wailsapp/wails/v2/pkg/options/assetserver"
)

// AppOption is the packaged-shell extension seam for native display, settings,
// file-association, accessibility, and additional binding bridges. Lifecycle
// callbacks remain owned by this package and are applied after these options.
type AppOption func(*options.App)

// Run starts the packaged native shell. Shared capability packages supply the
// framework-neutral base config; this boundary supplies Wails-owned ports.
func Run(base desktopapp.Config, assets fs.FS, providers map[string]ExternalProvider, extensions ...AppOption) error {
	if assets == nil {
		return errors.New("desktop frontend assets are unavailable")
	}
	runtime := WailsRuntime{}
	application, err := Compose(base, runtime, providers)
	if err != nil {
		return err
	}
	frontend := NewFrontendBridge(application)
	configured := &options.App{
		Title: "LayerDraw", Width: 1280, Height: 800, MinWidth: 960, MinHeight: 640,
		AssetServer: &assetserver.Options{Assets: assets},
	}
	for _, extension := range extensions {
		if extension != nil {
			extension(configured)
		}
	}
	configured.Bind = append([]any{frontend}, configured.Bind...)
	configured.OnStartup = func(ctx context.Context) {
		frontend.setContext(ctx)
		result := application.Start(ctx)
		if result.Outcome != protocolcommon.OutcomeSuccess && result.Failure != nil {
			WailsRuntime{}.Emit(ctx, recoveryEvent, *result.Failure)
		}
	}
	configured.OnBeforeClose = func(ctx context.Context) bool {
		assessment := application.FenceQuit(ctx)
		return assessment.Outcome != protocolcommon.OutcomeSuccess || !assessment.Value.CanQuit
	}
	configured.OnShutdown = func(ctx context.Context) {
		shutdown, cancel := context.WithTimeout(context.WithoutCancel(ctx), 15*time.Second)
		defer cancel()
		result := application.Shutdown(shutdown)
		if result.Outcome != protocolcommon.OutcomeSuccess && result.Failure != nil && result.Failure.Validate() {
			WailsRuntime{}.Emit(ctx, recoveryEvent, *result.Failure)
		}
	}
	return runWails(configured)
}

var runWails = wails.Run

var _ desktopcontract.WindowPort = WindowAdapter{}
var _ desktopcontract.NativeDialogPort = (*DialogAdapter)(nil)
var _ desktopapp.ProjectStorage = (*ProjectStorageAdapter)(nil)
var _ desktopapp.ProjectImportStorage = (*ProjectStorageAdapter)(nil)
var _ desktopapp.ProjectRelocationStorage = (*ProjectStorageAdapter)(nil)
var _ desktopapp.ExternalLifecycleAdapter = (*ExternalAdapter)(nil)
var _ desktopapp.ExternalStorageAdapter = (*ExternalAdapter)(nil)
var _ desktopapp.RecoveryReporter = RecoveryReporter{}
