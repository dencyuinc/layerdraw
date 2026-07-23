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
	"sync"
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
	wailsruntime "github.com/wailsapp/wails/v2/pkg/runtime"
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
	registryOwner, _ := base.Adapters[desktopcontract.ComponentBindingShell].(registryDispatcher)
	frontend := NewFrontendBridge(application, registryOwner)
	frontend.attachNativeShell(native.Shell)
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
	localeState := &menuLocaleState{}
	configured.Menu = nativeMenu(application, native.Shell, bridge, localeState)
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
		// Startup Restore applies settings before the DOM exists; reapply so the
		// persisted theme/zoom actually reach the loaded document.
		if current := native.Shell.CurrentSettings(ctx); current.Outcome == protocolcommon.OutcomeSuccess {
			_ = bridge.ApplySettings(ctx, current.Value)
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
	var mcpSocket *mcpSocketServer
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
		if socket, socketErr := startMCPSocket(application, base.Root); socketErr == nil {
			mcpSocket = socket
		}
		if restored := native.Shell.Restore(ctx); restored.Outcome != protocolcommon.OutcomeSuccess && restored.Failure != nil {
			WailsRuntime{}.Emit(ctx, recoveryEvent, *restored.Failure)
		} else {
			localeState.set(restored.Value.Settings.Locale)
			wailsruntime.MenuSetApplicationMenu(ctx, nativeMenu(application, native.Shell, bridge, localeState))
			if restored.Value.Settings.MCPEnabled {
				_ = application.SetMCPEnabled(ctx, true, desktopapp.MCPTransportLocal)
			}
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
		if mcpSocket != nil {
			mcpSocket.close()
		}
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
	type matrixCase struct {
		id      string
		width   uint16
		height  uint16
		theme   desktopcontract.Theme
		zoom    uint16
		mode    string
		reduced bool
	}
	cases := []matrixCase{
		{id: "standard-light-2d", width: 1280, height: 800, theme: desktopcontract.ThemeLight, zoom: 100, mode: "2d"},
		{id: "minimum-light-2.5d", width: 960, height: 640, theme: desktopcontract.ThemeLight, zoom: 100, mode: "2.5d", reduced: true},
		{id: "standard-light-zoom200-2d", width: 1280, height: 800, theme: desktopcontract.ThemeLight, zoom: 200, mode: "2d", reduced: true},
		{id: "large-dark-2.5d", width: 1440, height: 900, theme: desktopcontract.ThemeDark, zoom: 100, mode: "2.5d"},
	}
	for _, current := range cases {
		window := desktopcontract.WindowState{SchemaVersion: 1, Bounds: desktopcontract.Rectangle{Width: int(current.width), Height: int(current.height)}}
		if err := bridge.ApplyWindow(ctx, window); err != nil {
			return
		}
		settings := desktopcontract.DesktopSettings{SchemaVersion: 1, Theme: current.theme, ZoomPercent: current.zoom}
		if result := shell.UpdateSettings(ctx, settings); result.Outcome != protocolcommon.OutcomeSuccess {
			return
		}
		profile := desktopcontract.AccessibilityProfile{
			Platform: CurrentPlatform(), ScreenReader: true, KeyboardOnly: true, ReducedMotion: current.reduced, ZoomPercent: current.zoom,
			ProbeID: current.id, ViewerMode: current.mode, WindowWidth: current.width, WindowHeight: current.height,
		}
		reportResult := shell.VerifyAccessibility(ctx, profile)
		if reportResult.Outcome != protocolcommon.OutcomeSuccess {
			probe.Failure = &PackagedUIProbeFailure{ID: current.id, Accessibility: bridge.lastAccessibilityReport()}
			return
		}
		probe.UIMatrix = append(probe.UIMatrix, PackagedUIProbeResult{ID: current.id, Window: window, Settings: settings, Profile: profile, Accessibility: reportResult.Value})
	}
	probe.DOMRoundTrip = len(probe.UIMatrix) == len(cases)
	if probe.DOMRoundTrip {
		last := probe.UIMatrix[len(probe.UIMatrix)-1].Accessibility
		probe.Accessibility = &last
	}
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

// menuEvent carries View-menu commands to the frontend, which owns the
// presentation state the items control.
const menuEvent = "desktop:menu"

// menuLocaleState tracks the Language radio selection so menu rebuilds render
// exactly one checkmark; the frontend owns the actual catalog switch.
type menuLocaleState struct {
	mu     sync.Mutex
	locale string
}

func (s *menuLocaleState) value() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.locale == "" {
		return "system"
	}
	return s.locale
}

func (s *menuLocaleState) set(locale string) {
	s.mu.Lock()
	s.locale = locale
	s.mu.Unlock()
}

// recentProjectsOf tolerates a nil application so the menu can be built (and
// unit-tested) before the composition root finishes constructing one.
func recentProjectsOf(application *desktopapp.Application) desktopcontract.Result[[]desktopapp.RecentProject] {
	if application == nil {
		return desktopcontract.Result[[]desktopapp.RecentProject]{Outcome: protocolcommon.OutcomeRejected}
	}
	return application.RecentProjects()
}

// nativeMenu builds the OS menu per the approved shell design: app menu with
// Language selection, File with recents and close, Edit, View toggles, Window,
// and Help. Labels follow the approved (English) menu vocabulary; the Language
// submenu switches the in-app catalog locale.
func nativeMenu(application *desktopapp.Application, shell *desktopapp.NativeShell, bridge *WailsShellBridge, localeState *menuLocaleState) *menu.Menu {
	result := menu.NewMenu()
	runtimeShell := WailsRuntime{}

	appMenu := result.AddSubmenu("LayerDraw")
	appMenu.AddText("About LayerDraw", nil, func(*menu.CallbackData) {
		_, _ = wailsruntime.MessageDialog(bridge.context(), wailsruntime.MessageDialogOptions{
			Type:    wailsruntime.InfoDialog,
			Title:   "LayerDraw",
			Message: "LayerDraw Desktop\nTyped-graph modeling for software architecture.",
		})
	})
	appMenu.AddSeparator()
	appMenu.AddText("Settings…", keys.CmdOrCtrl(","), func(*menu.CallbackData) {
		runtimeShell.Emit(bridge.context(), menuEvent, "settings")
	})
	language := appMenu.AddSubmenu("Language")
	for _, locale := range []struct{ label, value string }{{"System", "system"}, {"English", "en"}, {"日本語", "ja"}} {
		value := locale.value
		language.Append(menu.Radio(locale.label, value == localeState.value(), nil, func(*menu.CallbackData) {
			localeState.set(value)
			ctx := bridge.context()
			if current := shell.CurrentSettings(ctx); current.Outcome == protocolcommon.OutcomeSuccess {
				next := current.Value
				next.Locale = value
				_ = shell.UpdateSettings(ctx, next)
			}
			runtimeShell.Emit(ctx, menuEvent, "locale:"+value)
			wailsruntime.MenuSetApplicationMenu(ctx, nativeMenu(application, shell, bridge, localeState))
		}))
	}
	appMenu.AddSeparator()
	appMenu.AddText("Quit LayerDraw", keys.CmdOrCtrl("q"), func(*menu.CallbackData) {
		runtimeShell.Quit(bridge.context())
	})

	file := result.AddSubmenu("File")
	file.AddText("New Project", keys.CmdOrCtrl("n"), func(*menu.CallbackData) {
		ctx := bridge.context()
		if invokeNativeCommand(ctx, shell, desktopcontract.CommandNewProject, desktopcontract.CommandSourceMenu) {
			runtimeShell.Emit(ctx, projectEvent)
		}
	})
	file.AddText("Open Project…", keys.CmdOrCtrl("o"), func(*menu.CallbackData) {
		ctx := bridge.context()
		if invokeNativeCommand(ctx, shell, desktopcontract.CommandOpenProject, desktopcontract.CommandSourceMenu) {
			runtimeShell.Emit(ctx, projectEvent)
		}
	})
	recent := file.AddSubmenu("Open Recent")
	if recents := recentProjectsOf(application); recents.Outcome == protocolcommon.OutcomeSuccess {
		for _, entry := range recents.Value {
			if entry.Availability == desktopapp.ProjectMissing {
				continue
			}
			projectID := entry.ProjectID
			label := entry.DisplayName
			for _, internalPrefix := range []string{"doc_", "revision_", "session_", "project_"} {
				if strings.HasPrefix(label, internalPrefix) {
					label = ""
					break
				}
			}
			if label == "" {
				label = "(Untitled project)"
			}
			recent.AddText(label, nil, func(*menu.CallbackData) {
				ctx := bridge.context()
				if opened := application.OpenRecentProject(ctx, projectID); opened.Outcome == protocolcommon.OutcomeSuccess {
					runtimeShell.Emit(ctx, projectEvent)
				}
			})
		}
	}
	if len(recent.Items) == 0 {
		empty := recent.AddText("No recent projects", nil, nil)
		empty.Disabled = true
	}
	file.AddSeparator()
	file.AddText("Close Project", keys.Combo("w", keys.CmdOrCtrlKey, keys.ShiftKey), func(*menu.CallbackData) {
		ctx := bridge.context()
		if application != nil {
			for _, session := range application.ActiveSessions() {
				_ = application.CloseProject(ctx, session)
			}
		}
		runtimeShell.Emit(ctx, projectEvent)
	})
	file.AddSeparator()
	export := file.AddText("Export…", nil, nil)
	export.Disabled = true

	result.Append(menu.EditMenu())

	view := result.AddSubmenu("View")
	view.AddText("2D Canvas", nil, func(*menu.CallbackData) { runtimeShell.Emit(bridge.context(), menuEvent, "view:2d") })
	view.AddText("3D Layers", nil, func(*menu.CallbackData) { runtimeShell.Emit(bridge.context(), menuEvent, "view:2.5d") })
	view.AddSeparator()
	view.AddText("Show Library", nil, func(*menu.CallbackData) { runtimeShell.Emit(bridge.context(), menuEvent, "panel:library") })
	view.AddText("Show Review", nil, func(*menu.CallbackData) { runtimeShell.Emit(bridge.context(), menuEvent, "panel:review") })
	view.AddText("Show AI Access", nil, func(*menu.CallbackData) { runtimeShell.Emit(bridge.context(), menuEvent, "panel:mcp") })

	window := result.AddSubmenu("Window")
	window.AddText("Minimize", keys.CmdOrCtrl("m"), func(*menu.CallbackData) { wailsruntime.WindowMinimise(bridge.context()) })

	help := result.AddSubmenu("Help")
	help.AddText("LayerDraw Documentation", nil, func(*menu.CallbackData) {
		wailsruntime.BrowserOpenURL(bridge.context(), "https://github.com/dencyuinc/layerdraw")
	})
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
