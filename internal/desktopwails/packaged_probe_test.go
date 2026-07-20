// SPDX-License-Identifier: LicenseRef-LayerDraw-1.0

package desktopwails

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/dencyuinc/layerdraw/gen/go/protocolcommon"
	"github.com/dencyuinc/layerdraw/internal/desktopapp"
	"github.com/dencyuinc/layerdraw/internal/desktopcontract"
	wailsruntime "github.com/wailsapp/wails/v2/pkg/runtime"
)

type fakeWailsShellRuntime struct {
	screens             []wailsruntime.Screen
	screenErr           error
	x, y, width, height int
	maximized           bool
	unmaximized         bool
	maximizedApplied    bool
	theme               desktopcontract.Theme
	script              string
	emitted             chan []any
}

func (f *fakeWailsShellRuntime) Screens(context.Context) ([]wailsruntime.Screen, error) {
	return f.screens, f.screenErr
}
func (f *fakeWailsShellRuntime) WindowPosition(context.Context) (int, int) { return f.x, f.y }
func (f *fakeWailsShellRuntime) WindowSize(context.Context) (int, int)     { return f.width, f.height }
func (f *fakeWailsShellRuntime) WindowMaximized(context.Context) bool      { return f.maximized }
func (f *fakeWailsShellRuntime) Unmaximize(context.Context)                { f.unmaximized = true }
func (f *fakeWailsShellRuntime) SetSize(_ context.Context, width, height int) {
	f.width, f.height = width, height
}
func (f *fakeWailsShellRuntime) SetPosition(_ context.Context, x, y int) { f.x, f.y = x, y }
func (f *fakeWailsShellRuntime) Maximize(context.Context)                { f.maximizedApplied = true }
func (f *fakeWailsShellRuntime) SetSystemTheme(context.Context) {
	f.theme = desktopcontract.ThemeSystem
}
func (f *fakeWailsShellRuntime) SetLightTheme(context.Context)           { f.theme = desktopcontract.ThemeLight }
func (f *fakeWailsShellRuntime) SetDarkTheme(context.Context)            { f.theme = desktopcontract.ThemeDark }
func (f *fakeWailsShellRuntime) ExecJS(_ context.Context, script string) { f.script = script }
func (f *fakeWailsShellRuntime) Emit(_ context.Context, _ string, data ...any) {
	if f.emitted != nil {
		f.emitted <- data
	}
}

func (f *fakeWailsShellRuntime) calls() wailsShellRuntime {
	return wailsShellRuntime{
		screens: f.Screens, windowPosition: f.WindowPosition, windowSize: f.WindowSize,
		windowMaximized: f.WindowMaximized, unmaximize: f.Unmaximize, setSize: f.SetSize,
		setPosition: f.SetPosition, maximize: f.Maximize, setSystemTheme: f.SetSystemTheme,
		setLightTheme: f.SetLightTheme, setDarkTheme: f.SetDarkTheme, execJS: f.ExecJS, emit: f.Emit,
	}
}

func TestPackagedProbeExercisesCurrentOSAdapters(t *testing.T) {
	var output bytes.Buffer
	if err := RunPackagedProbe(&output); err != nil {
		t.Fatal(err)
	}
	var result PackagedProbeResult
	if err := json.Unmarshal(output.Bytes(), &result); err != nil {
		t.Fatal(err)
	}
	if result.Platform != CurrentPlatform() || !result.WailsRuntimeBridge || !result.SettingsRoundTrip || result.AssociationHandoff != desktopcontract.FileAssociationLDL {
		t.Fatalf("probe=%+v", result)
	}
	if err := RunPackagedProbe(nil); err == nil {
		t.Fatal("nil packaged probe output accepted")
	}
}

func TestAccessibilityReportsAreOpaqueSingleUseSubmissions(t *testing.T) {
	bridge := NewWailsShellBridge()
	if err := bridge.SubmitAccessibilityReport("missing", desktopcontract.AccessibilityReport{}); err == nil {
		t.Fatal("unknown accessibility request accepted")
	}
	request := &accessibilitySubmission{ch: make(chan desktopcontract.AccessibilityReport, 1)}
	bridge.probes["probe"] = request
	report := desktopcontract.AccessibilityReport{LabelsComplete: true, MinimumContrast: 7}
	if err := bridge.SubmitAccessibilityReport("probe", report); err != nil {
		t.Fatal(err)
	}
	if received := <-request.ch; received != report {
		t.Fatalf("report=%+v", received)
	}
	if err := bridge.SubmitAccessibilityReport("probe", report); err == nil {
		t.Fatal("accessibility report replayed")
	}
	if err := bridge.SubmitAccessibilityReport("bad", desktopcontract.AccessibilityReport{MinimumContrast: 22}); err == nil {
		t.Fatal("invalid accessibility report accepted")
	}
}

func TestWailsShellBridgeAppliesWindowSettingsAndDisplays(t *testing.T) {
	runtime := &fakeWailsShellRuntime{
		screens: []wailsruntime.Screen{{IsPrimary: true, Width: 1920, Height: 1080}},
		x:       10, y: 20, width: 1280, height: 800, maximized: true,
	}
	bridge := newWailsShellBridge(runtime.calls())
	displays, err := bridge.Displays(context.Background())
	if err != nil || len(displays) != 1 || !displays[0].Validate() || !displays[0].Primary {
		t.Fatalf("displays=%+v err=%v", displays, err)
	}
	window, settings, err := bridge.Snapshot(context.Background())
	if err != nil || !window.Maximized || settings.ZoomPercent != 100 {
		t.Fatalf("snapshot=%+v %+v err=%v", window, settings, err)
	}
	next := desktopcontract.WindowState{SchemaVersion: 1, Bounds: desktopcontract.Rectangle{X: 30, Y: 40, Width: 1400, Height: 900}, Maximized: true}
	if err := bridge.ApplyWindow(context.Background(), next); err != nil || !runtime.unmaximized || !runtime.maximizedApplied || runtime.x != 30 || runtime.width != 1400 {
		t.Fatalf("window runtime=%+v err=%v", runtime, err)
	}
	if err := bridge.ApplyWindow(context.Background(), desktopcontract.WindowState{}); err == nil {
		t.Fatal("invalid window accepted")
	}
	for _, theme := range []desktopcontract.Theme{desktopcontract.ThemeSystem, desktopcontract.ThemeLight, desktopcontract.ThemeDark} {
		settings := desktopcontract.DesktopSettings{SchemaVersion: 1, Theme: theme, ZoomPercent: 175}
		if err := bridge.ApplySettings(context.Background(), settings); err != nil || runtime.theme != theme || !strings.Contains(runtime.script, "175%") {
			t.Fatalf("theme=%s runtime=%+v err=%v", theme, runtime, err)
		}
	}
	if err := bridge.ApplySettings(context.Background(), desktopcontract.DesktopSettings{}); err == nil {
		t.Fatal("invalid settings accepted")
	}
	runtime.screenErr = errors.New("unavailable")
	if _, err := bridge.Displays(context.Background()); err == nil {
		t.Fatal("display failure ignored")
	}
	runtime.screenErr, runtime.screens = nil, nil
	if _, err := bridge.Displays(context.Background()); err == nil {
		t.Fatal("empty display inventory accepted")
	}
	runtime.width = 1
	if _, _, err := bridge.Snapshot(context.Background()); err == nil {
		t.Fatal("invalid snapshot accepted")
	}
}

func TestWailsAccessibilityRoundTripAndCancellation(t *testing.T) {
	runtime := &fakeWailsShellRuntime{emitted: make(chan []any, 1)}
	bridge := newWailsShellBridge(runtime.calls())
	profile := desktopcontract.AccessibilityProfile{Platform: CurrentPlatform(), ScreenReader: true, KeyboardOnly: true, ZoomPercent: 200}
	result := make(chan error, 1)
	go func() {
		report, err := bridge.VerifyPackagedAccessibility(context.Background(), profile)
		if err == nil && (!report.LabelsComplete || report.MinimumContrast != 7) {
			err = errors.New("report mismatch")
		}
		result <- err
	}()
	event := <-runtime.emitted
	id, ok := event[0].(string)
	if !ok || id == "" {
		t.Fatalf("event=%+v", event)
	}
	if err := bridge.SubmitAccessibilityReport(id, desktopcontract.AccessibilityReport{LabelsComplete: true, MinimumContrast: 7}); err != nil {
		t.Fatal(err)
	}
	if err := <-result; err != nil {
		t.Fatal(err)
	}
	if _, err := bridge.VerifyPackagedAccessibility(context.Background(), desktopcontract.AccessibilityProfile{}); err == nil {
		t.Fatal("invalid profile accepted")
	}
	cancelled, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := bridge.VerifyPackagedAccessibility(cancelled, profile); err == nil {
		t.Fatal("cancelled accessibility probe succeeded")
	}
}

func TestWailsShellHelpersAndCommandRouter(t *testing.T) {
	runtime := &fakeWailsShellRuntime{width: 1280, height: 800}
	bridge := newWailsShellBridge(runtime.calls())
	if bridge.contextReady() {
		t.Fatal("new bridge unexpectedly has Wails context")
	}
	if bridge.context() == nil {
		t.Fatal("bridge did not return a fallback context")
	}
	if window, err := safeWindowSnapshot(context.Background(), bridge); err != nil || window.Bounds.Width != 1280 {
		t.Fatalf("safe snapshot=%+v err=%v", window, err)
	}
	panicCalls := runtime.calls()
	panicCalls.windowPosition = func(context.Context) (int, int) { panic("private") }
	if _, err := safeWindowSnapshot(context.Background(), newWailsShellBridge(panicCalls)); err == nil || strings.Contains(err.Error(), "private") {
		t.Fatalf("Wails snapshot panic was not safely redacted: %v", err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	bridge.setContext(ctx)
	if !bridge.contextReady() || bridge.context() != ctx {
		t.Fatal("Wails context was not retained")
	}
	router := newApplicationCommandRouter(nil)
	statuses, err := router.Status(context.Background())
	if err != nil || len(statuses) != 8 {
		t.Fatalf("statuses=%+v err=%v", statuses, err)
	}
	settings := desktopcontract.CommandInvocation{ID: desktopcontract.CommandSettings, Source: desktopcontract.CommandSourceControl, StatusGeneration: protocolcommon.CanonicalUint64("1")}
	if status, err := router.Route(context.Background(), settings); err != nil || status.State != desktopcontract.CommandAvailable {
		t.Fatalf("settings=%+v err=%v", status, err)
	}
	settings.StatusGeneration = "2"
	if _, err := router.Route(context.Background(), settings); err == nil {
		t.Fatal("stale command generation accepted")
	}
	undo := desktopcontract.CommandInvocation{ID: desktopcontract.CommandUndo, Source: desktopcontract.CommandSourceMenu, StatusGeneration: "1"}
	if _, err := router.Route(context.Background(), undo); err == nil {
		t.Fatal("unavailable command accepted")
	}
	native := &nativeStub{}
	if err := (wailsErrorSurface{runtime: native}).Present(context.Background(), desktopcontract.ErrorSurface{Failure: desktopcontract.FailureBackendPanic, Recovery: desktopcontract.RecoveryRetry}); err != nil || len(native.events) != 1 {
		t.Fatalf("error surface events=%v err=%v", native.events, err)
	}
	accepted := []string{}
	working := t.TempDir()
	absolute := filepath.Join(working, "two.ldl")
	acceptAssociationArguments(associationAcceptor(func(path string) error { accepted = append(accepted, path); return nil }), []string{"one.ldl", absolute}, working)
	if len(accepted) != 2 || accepted[0] != filepath.Join(working, "one.ldl") || accepted[1] != absolute {
		t.Fatalf("association paths=%v", accepted)
	}
}

type associationAcceptor func(string) error

func (accept associationAcceptor) AcceptOSPath(path string) error { return accept(path) }

type probeCrashRecovery struct{}

func (probeCrashRecovery) Preserve(context.Context, desktopcontract.CrashContext) (desktopcontract.RecoveryRef, error) {
	return desktopcontract.RecoveryRef{ID: "probe-recovery"}, nil
}

func newBindingFixture(t *testing.T, application *desktopapp.Application) (*ShellBinding, *WailsShellBridge, *desktopapp.PlatformNativeShell, *fakeWailsShellRuntime) {
	t.Helper()
	runtime := &fakeWailsShellRuntime{
		screens: []wailsruntime.Screen{{IsPrimary: true, Width: 1920, Height: 1080}},
		width:   1280, height: 800, emitted: make(chan []any, 1),
	}
	bridge := newWailsShellBridge(runtime.calls())
	bridge.setContext(context.Background())
	nativeRuntime := &nativeStub{}
	native, err := desktopapp.NewPlatformNativeShell(desktopapp.PlatformNativeShellConfig{
		Platform: CurrentPlatform(), StateRoot: t.TempDir(), Runtime: bridge,
		Commands: newApplicationCommandRouter(application), CrashRecovery: probeCrashRecovery{},
		Errors: wailsErrorSurface{runtime: nativeRuntime},
	})
	if err != nil {
		t.Fatal(err)
	}
	if restored := native.Shell.Restore(context.Background()); restored.Outcome != protocolcommon.OutcomeSuccess {
		t.Fatalf("restore=%+v", restored)
	}
	return newShellBinding(native.Shell, bridge), bridge, native, runtime
}

func TestContextFreeShellBindingRoutesWailsCalls(t *testing.T) {
	binding, bridge, _, _ := newBindingFixture(t, nil)
	status := binding.CommandStatus()
	if status.Outcome != protocolcommon.OutcomeSuccess || len(status.Value) != 8 {
		t.Fatalf("status=%+v", status)
	}
	invoked := binding.InvokeCommand(desktopcontract.CommandInvocation{ID: desktopcontract.CommandSettings, Source: desktopcontract.CommandSourceControl, StatusGeneration: "1"})
	if invoked.Outcome != protocolcommon.OutcomeSuccess {
		t.Fatalf("invoke=%+v", invoked)
	}
	updated := binding.UpdateSettings(desktopcontract.DesktopSettings{SchemaVersion: 1, Theme: desktopcontract.ThemeDark, ZoomPercent: 150})
	if updated.Outcome != protocolcommon.OutcomeSuccess || updated.Value.ZoomPercent != 150 {
		t.Fatalf("settings=%+v", updated)
	}
	bridge.probes["binding-probe"] = &accessibilitySubmission{ch: make(chan desktopcontract.AccessibilityReport, 1)}
	if err := binding.SubmitAccessibilityReport("binding-probe", desktopcontract.AccessibilityReport{MinimumContrast: 7}); err != nil {
		t.Fatal(err)
	}
}

func TestShellBindingPublishesPackagedProbeMode(t *testing.T) {
	_, bridge, native, _ := newBindingFixture(t, nil)
	if !newShellBinding(native.Shell, bridge, true).PackagedProbeMode() {
		t.Fatal("packaged probe mode was not published to the frontend")
	}
	if newShellBinding(native.Shell, bridge).PackagedProbeMode() {
		t.Fatal("normal Desktop falsely published packaged probe mode")
	}
}

func TestNativeCommandInvocationUsesCurrentBackendGeneration(t *testing.T) {
	binding, _, _, _ := newBindingFixture(t, nil)
	invokeNativeCommand(context.Background(), binding.shell, desktopcontract.CommandSettings, desktopcontract.CommandSourceMenu)
	invokeNativeCommand(context.Background(), binding.shell, desktopcontract.CommandSaveProject, desktopcontract.CommandSourceMenu)
}

func TestAssociatedPathIsConsumedInsideTrustedProjectLifecycle(t *testing.T) {
	root := t.TempDir()
	base, err := NewSharedConfig(filepath.Join(root, "state"))
	if err != nil {
		t.Fatal(err)
	}
	runtime := &nativeStub{}
	application, vault, err := compose(base, runtime, nil)
	if err != nil {
		t.Fatal(err)
	}
	_, _, native, _ := newBindingFixture(t, application)
	project := filepath.Join(root, "associated.ldl")
	if err := os.WriteFile(project, []byte("project associated \"Associated\" {}\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := native.Associations.AcceptOSPath(project); err != nil {
		t.Fatal(err)
	}
	if started := application.Start(context.Background()); started.Outcome != protocolcommon.OutcomeSuccess {
		t.Fatalf("start=%+v", started)
	}
	router := newApplicationCommandRouter(application)
	for _, id := range []desktopcontract.CommandID{desktopcontract.CommandNewProject, desktopcontract.CommandOpenProject} {
		status, err := router.Route(context.Background(), desktopcontract.CommandInvocation{
			ID: id, Source: desktopcontract.CommandSourceMenu, StatusGeneration: "1",
		})
		if err != nil || status.State != desktopcontract.CommandAvailable {
			t.Fatalf("native command %s status=%+v err=%v", id, status, err)
		}
	}
	if _, err := (projectCrashRecovery{application: application}).Preserve(context.Background(), desktopcontract.CrashContext{}); err == nil {
		t.Fatal("empty project lifecycle produced a recovery reference")
	}
	openAssociatedProjects(context.Background(), native, vault, application)
	if recent := application.RecentProjects(); recent.Outcome != protocolcommon.OutcomeSuccess || len(recent.Value) != 1 {
		t.Fatalf("recent=%+v", recent)
	}
	if stopped := application.Shutdown(context.Background()); stopped.Outcome != protocolcommon.OutcomeSuccess {
		t.Fatalf("shutdown=%+v", stopped)
	}
}

func TestPackagedUIProbeWritesRealAccessibilityRoundTrip(t *testing.T) {
	binding, bridge, _, bridgeRuntime := newBindingFixture(t, nil)
	output := filepath.Join(t.TempDir(), "ui-probe.json")
	runtime := &nativeStub{}
	done := make(chan struct{})
	go func() {
		runPackagedUIProbe(context.Background(), output, binding.shell, bridge, runtime)
		close(done)
	}()
	select {
	case event := <-bridgeRuntime.emitted:
		t.Fatalf("probe emitted before frontend readiness: %v", event)
	case <-time.After(25 * time.Millisecond):
	}
	binding.AccessibilityProbeReady()
	var event []any
	select {
	case event = <-bridgeRuntime.emitted:
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for packaged UI probe event")
	}
	id := event[0].(string)
	report := desktopcontract.AccessibilityReport{LabelsComplete: true, FocusOrderValid: true, KeyboardWorkflowValid: true, ReducedMotionHonored: true, MinimumContrast: 7, ZoomLayoutValid: true}
	if err := binding.SubmitAccessibilityReport(id, report); err != nil {
		t.Fatal(err)
	}
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for packaged UI probe completion")
	}
	encoded, err := os.ReadFile(output)
	if err != nil {
		t.Fatal(err)
	}
	var result PackagedProbeResult
	if err := json.Unmarshal(encoded, &result); err != nil || !result.DOMRoundTrip || result.Accessibility == nil || !result.Accessibility.KeyboardWorkflowValid || !runtime.quit {
		t.Fatalf("probe=%+v runtime=%+v err=%v", result, runtime, err)
	}
}

func TestPackagedUIProbeRejectsRelativeOutput(t *testing.T) {
	binding, bridge, _, _ := newBindingFixture(t, nil)
	runtime := &nativeStub{}
	runPackagedUIProbe(context.Background(), "relative.json", binding.shell, bridge, runtime)
	if !runtime.quit {
		t.Fatal("packaged UI probe did not quit after rejecting relative output")
	}
}

func TestPackagedUIProbeDoesNotOverwriteExistingOutput(t *testing.T) {
	output := filepath.Join(t.TempDir(), "ui-probe.json")
	if err := os.WriteFile(output, []byte("sentinel"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := writeExclusivePackagedProbe(output, []byte("replacement")); err == nil {
		t.Fatal("existing packaged probe output was overwritten")
	}
	content, err := os.ReadFile(output)
	if err != nil {
		t.Fatal(err)
	}
	if string(content) != "sentinel" {
		t.Fatalf("existing output changed: %q", content)
	}
}
