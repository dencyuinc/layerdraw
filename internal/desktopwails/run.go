// SPDX-License-Identifier: LicenseRef-LayerDraw-1.0

package desktopwails

import (
	"context"
	"encoding/json"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/dencyuinc/layerdraw/gen/go/protocolcommon"
	"github.com/dencyuinc/layerdraw/internal/desktopapp"
	"github.com/dencyuinc/layerdraw/internal/desktopcontract"
	"github.com/wailsapp/wails/v2"
	"github.com/wailsapp/wails/v2/pkg/menu"
	"github.com/wailsapp/wails/v2/pkg/menu/keys"
	"github.com/wailsapp/wails/v2/pkg/options"
	"github.com/wailsapp/wails/v2/pkg/options/assetserver"
	"github.com/wailsapp/wails/v2/pkg/options/mac"
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
	application, selectionVault, err := compose(base, runtime, providers)
	if err != nil {
		return err
	}
	bridge := NewWailsShellBridge()
	native, err := desktopapp.NewPlatformNativeShell(desktopapp.PlatformNativeShellConfig{
		Platform: CurrentPlatform(), StateRoot: base.Root, Runtime: bridge,
		Commands: newApplicationCommandRouter(application), CrashRecovery: projectCrashRecovery{application: application},
		Errors: wailsErrorSurface{runtime: runtime},
	})
	if err != nil {
		return err
	}
	acceptAssociationArguments(native.Associations, os.Args[1:], "")
	frontend := NewFrontendBridge(application)
	configured := &options.App{
		Title: "LayerDraw", Width: 1280, Height: 800, MinWidth: 960, MinHeight: 640,
		AssetServer: &assetserver.Options{Assets: assets}, StartHidden: true,
	}
	for _, extension := range extensions {
		if extension != nil {
			extension(configured)
		}
	}
	probeOutput := os.Getenv("LAYERDRAW_DESKTOP_UI_PROBE_OUTPUT")
	configured.Bind = append([]any{frontend, newShellBinding(native.Shell, bridge, probeOutput != "")}, configured.Bind...)
	configured.Menu = nativeMenu(native.Shell, bridge)
	configured.SingleInstanceLock = &options.SingleInstanceLock{
		UniqueId: "dev.layerdraw.desktop",
		OnSecondInstanceLaunch: func(data options.SecondInstanceData) {
			acceptAssociationArguments(native.Associations, data.Args, data.WorkingDirectory)
			openAssociatedProjects(bridge.context(), native, selectionVault, application)
			runtime.ShowWindow(bridge.context())
		},
	}
	startupReady := make(chan struct{})
	previousDOMReady := configured.OnDomReady
	configured.OnDomReady = func(ctx context.Context) {
		if previousDOMReady != nil {
			previousDOMReady(ctx)
		}
		if probeOutput != "" {
			go func() {
				select {
				case <-startupReady:
					runPackagedUIProbe(ctx, probeOutput, native.Shell, bridge, runtime)
				case <-ctx.Done():
				}
			}()
		}
	}
	if configured.Mac == nil {
		configured.Mac = &mac.Options{}
	}
	previousFileOpen := configured.Mac.OnFileOpen
	configured.Mac.OnFileOpen = func(path string) {
		if previousFileOpen != nil {
			previousFileOpen(path)
		}
		acceptAssociationArguments(native.Associations, []string{path}, "")
		if bridge.contextReady() {
			openAssociatedProjects(bridge.context(), native, selectionVault, application)
		}
	}
	configured.OnStartup = func(ctx context.Context) {
		defer close(startupReady)
		bridge.setContext(ctx)
		frontend.setContext(ctx)
		result := application.Start(ctx)
		if result.Outcome != protocolcommon.OutcomeSuccess && result.Failure != nil {
			WailsRuntime{}.Emit(ctx, recoveryEvent, *result.Failure)
			runtime.ShowWindow(ctx)
			return
		}
		if restored := native.Shell.Restore(ctx); restored.Outcome != protocolcommon.OutcomeSuccess && restored.Failure != nil {
			WailsRuntime{}.Emit(ctx, recoveryEvent, *restored.Failure)
		}
		openAssociatedProjects(ctx, native, selectionVault, application)
		runtime.ShowWindow(ctx)
	}
	configured.OnBeforeClose = func(ctx context.Context) bool {
		if bridge.contextReady() {
			if window, snapshotErr := safeWindowSnapshot(ctx, bridge); snapshotErr == nil {
				_ = native.Shell.SaveWindow(ctx, window)
			}
		}
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

func runPackagedUIProbe(ctx context.Context, output string, shell *desktopapp.NativeShell, bridge *WailsShellBridge, runtime NativeRuntime) {
	defer runtime.Quit(ctx)
	if !filepath.IsAbs(output) || filepath.Clean(output) != output {
		return
	}
	probe := PackagedProbeResult{}
	defer func() {
		encoded, err := json.Marshal(probe)
		if err == nil {
			_ = writeExclusivePackagedProbe(output, append(encoded, '\n'))
		}
	}()
	stateProbe, err := executePackagedProbe()
	if err != nil {
		return
	}
	probe = stateProbe
	readyCtx, cancelReady := context.WithTimeout(ctx, 30*time.Second)
	defer cancelReady()
	if err := bridge.waitAccessibilityProbeReady(readyCtx); err != nil {
		return
	}
	settings := desktopcontract.DesktopSettings{SchemaVersion: 1, Theme: desktopcontract.ThemeLight, ZoomPercent: 200}
	if result := shell.UpdateSettings(ctx, settings); result.Outcome != protocolcommon.OutcomeSuccess {
		return
	}
	profile := desktopcontract.AccessibilityProfile{Platform: CurrentPlatform(), ScreenReader: true, KeyboardOnly: true, ReducedMotion: true, ZoomPercent: 200}
	report, err := bridge.VerifyPackagedAccessibility(ctx, profile)
	if err != nil {
		return
	}
	probe.DOMRoundTrip, probe.Accessibility = true, &report
}

func writeExclusivePackagedProbe(output string, encoded []byte) (err error) {
	volumeRoot := filepath.VolumeName(output) + string(os.PathSeparator)
	if !strings.HasPrefix(output, volumeRoot) {
		return errors.New("packaged Desktop UI probe path escapes its volume")
	}
	// This path is accepted only by the explicit packaged-probe CLI mode and is
	// required to be clean and absolute above. O_EXCL prevents an existing file
	// or symlink from being followed or overwritten.
	file, err := os.OpenFile(output, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if err != nil {
		return err
	}
	defer func() {
		if err != nil {
			_ = file.Close()
		}
	}()
	if _, err = file.Write(encoded); err != nil {
		return err
	}
	if err = file.Sync(); err != nil {
		return err
	}
	return file.Close()
}

func openAssociatedProjects(ctx context.Context, native *desktopapp.PlatformNativeShell, vault *selectionVault, application *desktopapp.Application) {
	for {
		handoff := native.Shell.NextFileAssociation(ctx)
		if handoff.Outcome != protocolcommon.OutcomeSuccess {
			return
		}
		path, identity, err := native.Associations.ResolveIdentity(handoff.Value.Token)
		if err != nil {
			continue
		}
		token, err := vault.issuePinned(path, identity)
		if err != nil {
			continue
		}
		_ = application.OpenProject(ctx, token)
	}
}

func safeWindowSnapshot(ctx context.Context, bridge *WailsShellBridge) (window desktopcontract.WindowState, err error) {
	defer func() {
		if recover() != nil {
			window = desktopcontract.WindowState{}
			err = errors.New("Wails window snapshot unavailable")
		}
	}()
	window, _, err = bridge.Snapshot(ctx)
	return window, err
}

func nativeMenu(shell *desktopapp.NativeShell, bridge *WailsShellBridge) *menu.Menu {
	result := menu.NewMenu()
	file := result.AddSubmenu("File")
	file.AddText("New Project", keys.CmdOrCtrl("n"), func(*menu.CallbackData) {
		invokeNativeCommand(bridge.context(), shell, desktopcontract.CommandNewProject, desktopcontract.CommandSourceMenu)
	})
	file.AddText("Open Project", keys.CmdOrCtrl("o"), func(*menu.CallbackData) {
		invokeNativeCommand(bridge.context(), shell, desktopcontract.CommandOpenProject, desktopcontract.CommandSourceMenu)
	})
	result.Append(menu.EditMenu())
	return result
}

func acceptAssociationArguments(broker interface{ AcceptOSPath(string) error }, arguments []string, workingDirectory string) {
	for _, argument := range arguments {
		path := argument
		if !filepath.IsAbs(path) && workingDirectory != "" {
			path = filepath.Join(workingDirectory, path)
		}
		_ = broker.AcceptOSPath(filepath.Clean(path))
	}
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
