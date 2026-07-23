// SPDX-License-Identifier: LicenseRef-LayerDraw-1.0

package desktopapp

import (
	"context"
	"encoding/json"
	"errors"
	"math"
	"os"
	"path/filepath"
	"reflect"
	"sync"
	"testing"
	"time"

	"github.com/dencyuinc/layerdraw/gen/go/protocolcommon"
	"github.com/dencyuinc/layerdraw/internal/desktopcontract"
)

var shellTestNow = time.Date(2026, 7, 20, 16, 0, 0, 0, time.UTC)

type shellSettingsStore struct {
	mu      sync.Mutex
	value   desktopcontract.PersistedShellState
	loadErr error
	saveErr error
	saves   []desktopcontract.PersistedShellState
}

func (s *shellSettingsStore) Load(context.Context) (desktopcontract.PersistedShellState, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.value, s.loadErr
}

func (s *shellSettingsStore) Save(_ context.Context, value desktopcontract.PersistedShellState) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.saveErr != nil {
		return s.saveErr
	}
	s.value = value
	s.saves = append(s.saves, value)
	return nil
}

type shellWindowPort struct {
	displays        []desktopcontract.Display
	snapshotWindow  desktopcontract.WindowState
	snapshotSetting desktopcontract.DesktopSettings
	windows         []desktopcontract.WindowState
	settings        []desktopcontract.DesktopSettings
	displayErr      error
	snapshotErr     error
	windowErr       error
	settingsErr     error
	windowErrors    []error
	settingsErrors  []error
	panicOnDisplays bool
}

func (p *shellWindowPort) Displays(context.Context) ([]desktopcontract.Display, error) {
	if p.panicOnDisplays {
		panic("private native path")
	}
	return append([]desktopcontract.Display(nil), p.displays...), p.displayErr
}
func (p *shellWindowPort) Snapshot(context.Context) (desktopcontract.WindowState, desktopcontract.DesktopSettings, error) {
	return p.snapshotWindow, p.snapshotSetting, p.snapshotErr
}
func (p *shellWindowPort) ApplyWindow(_ context.Context, value desktopcontract.WindowState) error {
	p.windows = append(p.windows, value)
	if len(p.windowErrors) != 0 {
		err := p.windowErrors[0]
		p.windowErrors = p.windowErrors[1:]
		return err
	}
	return p.windowErr
}
func (p *shellWindowPort) ApplySettings(_ context.Context, value desktopcontract.DesktopSettings) error {
	p.settings = append(p.settings, value)
	if len(p.settingsErrors) != 0 {
		err := p.settingsErrors[0]
		p.settingsErrors = p.settingsErrors[1:]
		return err
	}
	return p.settingsErr
}
func (p *shellWindowPort) VerifyPackagedAccessibility(context.Context, desktopcontract.AccessibilityProfile) (desktopcontract.AccessibilityReport, error) {
	return desktopcontract.AccessibilityReport{
		LabelsComplete: true, ScreenReaderSemantics: true, FocusOrderValid: true, KeyboardWorkflowValid: true,
		ReducedMotionHonored: true, MinimumContrast: 7, ZoomLayoutValid: true,
	}, nil
}

type shellCommandRouter struct {
	statuses []desktopcontract.CommandStatus
	calls    []desktopcontract.CommandInvocation
	route    *desktopcontract.CommandStatus
	err      error
}

func (r *shellCommandRouter) Status(context.Context) ([]desktopcontract.CommandStatus, error) {
	return append([]desktopcontract.CommandStatus(nil), r.statuses...), r.err
}
func (r *shellCommandRouter) Route(_ context.Context, value desktopcontract.CommandInvocation) (desktopcontract.CommandStatus, error) {
	r.calls = append(r.calls, value)
	if r.err != nil {
		return desktopcontract.CommandStatus{}, r.err
	}
	if r.route != nil {
		return *r.route, nil
	}
	for _, status := range r.statuses {
		if status.ID == value.ID {
			return status, nil
		}
	}
	return desktopcontract.CommandStatus{ID: value.ID, State: desktopcontract.CommandUnavailable, Generation: value.StatusGeneration}, nil
}

type shellExternalPort struct {
	values []desktopcontract.ExternalTarget
	err    error
	panic  bool
}

func (p *shellExternalPort) OpenExternal(_ context.Context, value desktopcontract.ExternalTarget) error {
	if p.panic {
		panic("secret token")
	}
	p.values = append(p.values, value)
	return p.err
}

type shellAssociationPort struct {
	value desktopcontract.FileAssociationHandoff
	err   error
}

func (p shellAssociationPort) Next(context.Context) (desktopcontract.FileAssociationHandoff, error) {
	return p.value, p.err
}

type shellCrashPort struct {
	contexts []desktopcontract.CrashContext
	bounded  []bool
	ref      desktopcontract.RecoveryRef
	err      error
	panic    bool
}

func (p *shellCrashPort) Preserve(ctx context.Context, value desktopcontract.CrashContext) (desktopcontract.RecoveryRef, error) {
	p.contexts = append(p.contexts, value)
	_, hasDeadline := ctx.Deadline()
	p.bounded = append(p.bounded, hasDeadline)
	if p.panic {
		panic("private recovery path")
	}
	return p.ref, p.err
}

type shellErrorPort struct {
	values  []desktopcontract.ErrorSurface
	bounded []bool
	err     error
	panic   bool
}

func (p *shellErrorPort) Present(ctx context.Context, value desktopcontract.ErrorSurface) error {
	p.values = append(p.values, value)
	_, hasDeadline := ctx.Deadline()
	p.bounded = append(p.bounded, hasDeadline)
	if p.panic {
		panic("private presenter failure")
	}
	return p.err
}

type shellAccessibilityPort struct {
	profiles []desktopcontract.AccessibilityProfile
	report   desktopcontract.AccessibilityReport
	err      error
}

func (p *shellAccessibilityPort) VerifyPackaged(_ context.Context, value desktopcontract.AccessibilityProfile) (desktopcontract.AccessibilityReport, error) {
	p.profiles = append(p.profiles, value)
	return p.report, p.err
}

type shellLogPort struct {
	values []desktopcontract.StructuredLogRecord
}

func (p *shellLogPort) Write(_ context.Context, value desktopcontract.StructuredLogRecord) error {
	p.values = append(p.values, value)
	return nil
}

type shellFixture struct {
	store    *shellSettingsStore
	window   *shellWindowPort
	router   *shellCommandRouter
	external *shellExternalPort
	crash    *shellCrashPort
	errors   *shellErrorPort
	access   *shellAccessibilityPort
	logs     *shellLogPort
}

func validShellState() desktopcontract.PersistedShellState {
	return desktopcontract.PersistedShellState{
		Settings: desktopcontract.DesktopSettings{SchemaVersion: 1, Theme: desktopcontract.ThemeDark, ZoomPercent: 125},
		Window:   desktopcontract.WindowState{SchemaVersion: 1, Bounds: desktopcontract.Rectangle{X: 100, Y: 80, Width: 1200, Height: 760}, Maximized: true},
	}
}

func newShellFixture(t *testing.T, platform desktopcontract.DesktopPlatform) (*NativeShell, *shellFixture) {
	t.Helper()
	f := &shellFixture{
		store: &shellSettingsStore{value: validShellState()},
		window: &shellWindowPort{
			displays:       []desktopcontract.Display{{ID: "primary", Primary: true, Work: desktopcontract.Rectangle{Width: 1920, Height: 1080}}},
			snapshotWindow: validShellState().Window, snapshotSetting: validShellState().Settings,
		},
		router: &shellCommandRouter{statuses: []desktopcontract.CommandStatus{
			{ID: desktopcontract.CommandOpenProject, State: desktopcontract.CommandAvailable, Generation: "1"},
			{ID: desktopcontract.CommandSaveProject, State: desktopcontract.CommandPending, Generation: "1"},
			{ID: desktopcontract.CommandUndo, State: desktopcontract.CommandDenied, Generation: "1"},
		}},
		external: &shellExternalPort{},
		crash:    &shellCrashPort{ref: desktopcontract.RecoveryRef{ID: "recovery-opaque-1"}},
		errors:   &shellErrorPort{},
		access: &shellAccessibilityPort{report: desktopcontract.AccessibilityReport{
			LabelsComplete: true, ScreenReaderSemantics: true, FocusOrderValid: true, KeyboardWorkflowValid: true,
			ReducedMotionHonored: true, MinimumContrast: 7, ZoomLayoutValid: true,
		}},
		logs: &shellLogPort{},
	}
	shell, err := NewNativeShell(NativeShellConfig{
		Platform: platform, Settings: f.store, Window: f.window, Commands: f.router,
		External:      f.external,
		Associations:  shellAssociationPort{value: desktopcontract.FileAssociationHandoff{Kind: desktopcontract.FileAssociationLDL, Token: "opaque-file-1"}},
		CrashRecovery: f.crash, Errors: f.errors, Accessibility: f.access, Logs: f.logs,
		Now: func() time.Time { return shellTestNow },
	})
	if err != nil {
		t.Fatal(err)
	}
	return shell, f
}

func TestNativeShellCompositionIsFailClosed(t *testing.T) {
	if _, err := NewNativeShell(NativeShellConfig{}); err == nil {
		t.Fatal("incomplete native shell was accepted")
	}
}

func TestPlatformNativeShellWiresProductionAdapters(t *testing.T) {
	_, f := newShellFixture(t, desktopcontract.PlatformMacOS)
	root := t.TempDir()
	platform, err := NewPlatformNativeShell(PlatformNativeShellConfig{
		Platform: desktopcontract.PlatformMacOS, StateRoot: root, Runtime: f.window,
		Commands: f.router, CrashRecovery: f.crash, Errors: f.errors,
		Now: func() time.Time { return shellTestNow },
	})
	if err != nil {
		t.Fatal(err)
	}
	if result := platform.Shell.Restore(context.Background()); result.Outcome != protocolcommon.OutcomeSuccess {
		t.Fatalf("production restore=%+v", result)
	}
	profile := desktopcontract.AccessibilityProfile{Platform: desktopcontract.PlatformMacOS, ScreenReader: true, KeyboardOnly: true, ReducedMotion: true, ZoomPercent: 200}
	if result := platform.Shell.VerifyAccessibility(context.Background(), profile); result.Outcome != protocolcommon.OutcomeSuccess {
		t.Fatalf("production accessibility=%+v", result)
	}
	path := filepath.Join(root, "associated.ldl")
	if err := os.WriteFile(path, []byte("project x {}"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := platform.Associations.AcceptOSPath(path); err != nil {
		t.Fatal(err)
	}
	handoff := platform.Shell.NextFileAssociation(context.Background())
	if handoff.Outcome != protocolcommon.OutcomeSuccess || handoff.Value.Token == "" {
		t.Fatalf("production handoff=%+v", handoff)
	}
	if resolved, err := platform.Associations.Resolve(handoff.Value.Token); err != nil || resolved != path {
		t.Fatalf("production resolve=%q err=%v", resolved, err)
	}
	if _, err := os.Stat(filepath.Join(root, "settings-v1.json")); err != nil {
		t.Fatalf("production settings not persisted: %v", err)
	}
}

func TestNativeShellRestoresAcrossSupportedPlatforms(t *testing.T) {
	for _, platform := range []desktopcontract.DesktopPlatform{desktopcontract.PlatformMacOS, desktopcontract.PlatformWindows, desktopcontract.PlatformLinux} {
		t.Run(string(platform), func(t *testing.T) {
			shell, f := newShellFixture(t, platform)
			result := shell.Restore(context.Background())
			if !result.Validate() || result.Outcome != protocolcommon.OutcomeSuccess || !reflect.DeepEqual(result.Value, validShellState()) {
				t.Fatalf("restore=%+v", result)
			}
			if !reflect.DeepEqual(f.window.windows, []desktopcontract.WindowState{validShellState().Window}) || !reflect.DeepEqual(f.window.settings, []desktopcontract.DesktopSettings{validShellState().Settings}) {
				t.Fatalf("applied window=%+v settings=%+v", f.window.windows, f.window.settings)
			}
		})
	}
}

func TestNativeShellRecoversCorruptAndOffscreenState(t *testing.T) {
	shell, f := newShellFixture(t, desktopcontract.PlatformMacOS)
	f.store.loadErr = errors.New("corrupt private settings")
	result := shell.Restore(context.Background())
	if result.Outcome != protocolcommon.OutcomeSuccess || result.Value.Settings.Theme != desktopcontract.ThemeSystem || result.Value.Settings.ZoomPercent != 100 || len(f.store.saves) != 1 {
		t.Fatalf("corrupt recovery=%+v saves=%+v", result, f.store.saves)
	}

	shell, f = newShellFixture(t, desktopcontract.PlatformWindows)
	f.store.value.Window.Bounds = desktopcontract.Rectangle{X: 9000, Y: -4000, Width: 4000, Height: 3000}
	result = shell.Restore(context.Background())
	if got := result.Value.Window.Bounds; got != (desktopcontract.Rectangle{X: 0, Y: 0, Width: 1920, Height: 1080}) || !result.Value.Window.Maximized || len(f.store.saves) != 1 {
		t.Fatalf("offscreen recovery=%+v saves=%d", result.Value.Window, len(f.store.saves))
	}
}

func TestNativeShellSettingsAndWindowPersistence(t *testing.T) {
	shell, f := newShellFixture(t, desktopcontract.PlatformLinux)
	if result := shell.UpdateSettings(context.Background(), validShellState().Settings); result.Outcome != protocolcommon.OutcomeFailed {
		t.Fatalf("update before restore=%+v", result)
	}
	if result := shell.Restore(context.Background()); result.Outcome != protocolcommon.OutcomeSuccess {
		t.Fatal(result)
	}
	next := desktopcontract.DesktopSettings{SchemaVersion: 1, Theme: desktopcontract.ThemeLight, ZoomPercent: 200}
	if result := shell.UpdateSettings(context.Background(), next); result.Outcome != protocolcommon.OutcomeSuccess || f.store.value.Settings != next {
		t.Fatalf("settings update=%+v stored=%+v", result, f.store.value)
	}
	window := desktopcontract.WindowState{SchemaVersion: 1, Bounds: desktopcontract.Rectangle{X: -5000, Y: 100, Width: 900, Height: 700}}
	if result := shell.SaveWindow(context.Background(), window); result.Outcome != protocolcommon.OutcomeSuccess || result.Value.Bounds.X < 0 || f.store.value.Window != result.Value {
		t.Fatalf("window update=%+v stored=%+v", result, f.store.value.Window)
	}
	invalid := desktopcontract.DesktopSettings{SchemaVersion: 1, Theme: "transparent", ZoomPercent: 100}
	if result := shell.UpdateSettings(context.Background(), invalid); result.Outcome != protocolcommon.OutcomeFailed || result.Failure.Code != desktopcontract.FailureSettings {
		t.Fatalf("invalid settings=%+v", result)
	}
}

func TestNativeShellSettingsSaveFailureRollsBackAppliedSetting(t *testing.T) {
	shell, f := newShellFixture(t, desktopcontract.PlatformMacOS)
	if result := shell.Restore(context.Background()); result.Outcome != protocolcommon.OutcomeSuccess {
		t.Fatal(result)
	}
	f.store.saveErr = errors.New("disk full")
	next := desktopcontract.DesktopSettings{SchemaVersion: 1, Theme: desktopcontract.ThemeLight, ZoomPercent: 150}
	result := shell.UpdateSettings(context.Background(), next)
	if result.Outcome != protocolcommon.OutcomeFailed || len(f.window.settings) != 3 || f.window.settings[2] != validShellState().Settings {
		t.Fatalf("rollback result=%+v applied=%+v", result, f.window.settings)
	}
}

func TestNativeShellRestoreCompensatesEveryMutationStage(t *testing.T) {
	t.Run("snapshot", func(t *testing.T) {
		shell, f := newShellFixture(t, desktopcontract.PlatformMacOS)
		f.window.snapshotErr = errors.New("snapshot")
		result := shell.Restore(context.Background())
		if result.Outcome != protocolcommon.OutcomeFailed || len(f.window.windows) != 0 || len(f.window.settings) != 0 {
			t.Fatalf("snapshot failure=%+v window=%+v settings=%+v", result, f.window.windows, f.window.settings)
		}
	})

	t.Run("apply window", func(t *testing.T) {
		shell, f := newShellFixture(t, desktopcontract.PlatformMacOS)
		f.window.windowErrors = []error{errors.New("apply"), nil}
		result := shell.Restore(context.Background())
		if result.Outcome != protocolcommon.OutcomeFailed || len(f.window.windows) != 2 || f.window.windows[1] != f.window.snapshotWindow || len(f.window.settings) != 1 || f.window.settings[0] != f.window.snapshotSetting {
			t.Fatalf("window compensation=%+v windows=%+v settings=%+v", result, f.window.windows, f.window.settings)
		}
	})

	t.Run("apply settings", func(t *testing.T) {
		shell, f := newShellFixture(t, desktopcontract.PlatformMacOS)
		f.window.settingsErrors = []error{errors.New("apply"), nil}
		result := shell.Restore(context.Background())
		if result.Outcome != protocolcommon.OutcomeFailed || len(f.window.windows) != 2 || len(f.window.settings) != 2 || f.window.settings[1] != f.window.snapshotSetting {
			t.Fatalf("settings compensation=%+v windows=%+v settings=%+v", result, f.window.windows, f.window.settings)
		}
	})

	t.Run("persist", func(t *testing.T) {
		shell, f := newShellFixture(t, desktopcontract.PlatformMacOS)
		f.store.loadErr = errors.New("corrupt")
		f.store.saveErr = errors.New("persist")
		result := shell.Restore(context.Background())
		if result.Outcome != protocolcommon.OutcomeFailed || len(f.window.windows) != 2 || len(f.window.settings) != 2 || f.window.windows[1] != f.window.snapshotWindow || f.window.settings[1] != f.window.snapshotSetting {
			t.Fatalf("persist compensation=%+v windows=%+v settings=%+v", result, f.window.windows, f.window.settings)
		}
	})

	t.Run("rollback", func(t *testing.T) {
		shell, f := newShellFixture(t, desktopcontract.PlatformMacOS)
		f.window.settingsErrors = []error{errors.New("apply"), errors.New("rollback")}
		result := shell.Restore(context.Background())
		if result.Outcome != protocolcommon.OutcomeFailed || result.Failure.Code != desktopcontract.FailureCrashRecovery || result.Failure.Recovery != desktopcontract.RecoveryOpenRecovery || len(f.crash.contexts) != 1 || len(f.errors.values) != 1 {
			t.Fatalf("rollback recovery=%+v crash=%+v surface=%+v", result, f.crash.contexts, f.errors.values)
		}
	})
}

func TestNativeShellUpdateRollbackFailureOpensRecovery(t *testing.T) {
	shell, f := newShellFixture(t, desktopcontract.PlatformLinux)
	if result := shell.Restore(context.Background()); result.Outcome != protocolcommon.OutcomeSuccess {
		t.Fatal(result)
	}
	f.store.saveErr = errors.New("persist")
	f.window.settingsErrors = []error{nil, errors.New("rollback")}
	result := shell.UpdateSettings(context.Background(), desktopcontract.DesktopSettings{SchemaVersion: 1, Theme: desktopcontract.ThemeLight, ZoomPercent: 150})
	if result.Outcome != protocolcommon.OutcomeFailed || result.Failure.Code != desktopcontract.FailureCrashRecovery || len(f.errors.values) != 1 {
		t.Fatalf("update recovery=%+v surfaces=%+v", result, f.errors.values)
	}
}

func TestNativeShellRoutesMenusShortcutsAndControlsIdentically(t *testing.T) {
	shell, f := newShellFixture(t, desktopcontract.PlatformMacOS)
	for _, source := range []desktopcontract.CommandSource{desktopcontract.CommandSourceMenu, desktopcontract.CommandSourceShortcut, desktopcontract.CommandSourceControl} {
		result := shell.InvokeCommand(context.Background(), desktopcontract.CommandInvocation{ID: desktopcontract.CommandOpenProject, Source: source, StatusGeneration: "1"})
		if result.Outcome != protocolcommon.OutcomeSuccess {
			t.Fatalf("source %s=%+v", source, result)
		}
	}
	if len(f.router.calls) != 3 {
		t.Fatalf("router calls=%+v", f.router.calls)
	}
	for _, id := range []desktopcontract.CommandID{desktopcontract.CommandSaveProject, desktopcontract.CommandUndo, desktopcontract.CommandRedo} {
		result := shell.InvokeCommand(context.Background(), desktopcontract.CommandInvocation{ID: id, Source: desktopcontract.CommandSourceShortcut, StatusGeneration: "1"})
		if result.Outcome != protocolcommon.OutcomeRejected || result.Value.State == desktopcontract.CommandAvailable {
			t.Fatalf("unavailable %s=%+v", id, result)
		}
	}
	if len(f.router.calls) != 6 {
		t.Fatal("every source must use the one atomic owner route")
	}
}

func TestNativeShellAtomicRouteRejectsStaleAvailability(t *testing.T) {
	shell, f := newShellFixture(t, desktopcontract.PlatformWindows)
	status := shell.CommandStatus(context.Background())
	if status.Outcome != protocolcommon.OutcomeSuccess || status.Value[0].State != desktopcontract.CommandAvailable {
		t.Fatal(status)
	}
	stale := desktopcontract.CommandStatus{ID: desktopcontract.CommandOpenProject, State: desktopcontract.CommandDenied, Generation: "2"}
	f.router.route = &stale
	result := shell.InvokeCommand(context.Background(), desktopcontract.CommandInvocation{ID: desktopcontract.CommandOpenProject, Source: desktopcontract.CommandSourceShortcut, StatusGeneration: "1"})
	if result.Outcome != protocolcommon.OutcomeFailed || result.Failure.Code != desktopcontract.FailureCommandUnavailable || len(f.router.calls) != 1 {
		t.Fatalf("stale route=%+v calls=%+v", result, f.router.calls)
	}
}

func TestNativeShellRejectsUnsafeExternalTargetsWithoutLeakingThem(t *testing.T) {
	shell, f := newShellFixture(t, desktopcontract.PlatformWindows)
	unsafe := []desktopcontract.ExternalTarget{
		{Kind: desktopcontract.ExternalWebLink, Value: "file:///Users/private/secret.ldl"},
		{Kind: desktopcontract.ExternalWebLink, Value: "javascript:alert(1)"},
		{Kind: desktopcontract.ExternalWebLink, Value: "https://token@example.com/private"},
		{Kind: desktopcontract.ExternalWebLink, Value: "https://example.com\n--execute"},
		{Kind: desktopcontract.ExternalEmail, Value: "mailto:user@example.com?body=secret"},
	}
	for _, target := range unsafe {
		result := shell.OpenExternal(context.Background(), target)
		if result.Outcome != protocolcommon.OutcomeFailed || result.Failure.Code != desktopcontract.FailureExternalTarget {
			t.Fatalf("unsafe target accepted: %#v => %+v", target, result)
		}
	}
	for _, target := range []desktopcontract.ExternalTarget{
		{Kind: desktopcontract.ExternalWebLink, Value: "https://layerdraw.com/docs"},
		{Kind: desktopcontract.ExternalEmail, Value: "mailto:help@layerdraw.com"},
	} {
		if result := shell.OpenExternal(context.Background(), target); result.Outcome != protocolcommon.OutcomeSuccess {
			t.Fatalf("safe target=%+v", result)
		}
	}
	if len(f.external.values) != 2 {
		t.Fatalf("external calls=%+v", f.external.values)
	}
	encoded, err := json.Marshal(f.logs.values)
	if err != nil {
		t.Fatal(err)
	}
	for _, private := range []string{"Users/private", "secret.ldl", "token@example", "body=secret"} {
		if contains := string(encoded); len(contains) > 0 && shellContains(contains, private) {
			t.Fatalf("structured logs leaked %q: %s", private, encoded)
		}
	}
}

func shellContains(value, fragment string) bool {
	for i := 0; i+len(fragment) <= len(value); i++ {
		if value[i:i+len(fragment)] == fragment {
			return true
		}
	}
	return false
}

func TestNativeShellFileAssociationUsesOpaqueHandoff(t *testing.T) {
	shell, _ := newShellFixture(t, desktopcontract.PlatformMacOS)
	result := shell.NextFileAssociation(context.Background())
	if result.Outcome != protocolcommon.OutcomeSuccess || result.Value.Token != "opaque-file-1" || result.Value.Kind != desktopcontract.FileAssociationLDL {
		t.Fatalf("handoff=%+v", result)
	}

	badShell, _ := newShellFixture(t, desktopcontract.PlatformMacOS)
	config := badShell.config
	config.Associations = shellAssociationPort{value: desktopcontract.FileAssociationHandoff{Kind: desktopcontract.FileAssociationLDL, Token: "/Users/private/document.ldl"}}
	badShell, err := NewNativeShell(config)
	if err != nil {
		t.Fatal(err)
	}
	if result := badShell.NextFileAssociation(context.Background()); result.Outcome != protocolcommon.OutcomeFailed {
		t.Fatalf("native path crossed handoff=%+v", result)
	}
}

func TestNativeShellPackagedAccessibilityMatrix(t *testing.T) {
	for _, platform := range []desktopcontract.DesktopPlatform{desktopcontract.PlatformMacOS, desktopcontract.PlatformWindows, desktopcontract.PlatformLinux} {
		shell, _ := newShellFixture(t, platform)
		profile := desktopcontract.AccessibilityProfile{Platform: platform, ScreenReader: true, KeyboardOnly: true, ReducedMotion: true, ZoomPercent: 200}
		if result := shell.VerifyAccessibility(context.Background(), profile); result.Outcome != protocolcommon.OutcomeSuccess {
			t.Fatalf("%s=%+v", platform, result)
		}
	}
}

func TestNativeShellAccessibilityFailsClosedForEveryRequiredSignal(t *testing.T) {
	edits := []func(*desktopcontract.AccessibilityReport){
		func(r *desktopcontract.AccessibilityReport) { r.LabelsComplete = false },
		func(r *desktopcontract.AccessibilityReport) { r.FocusOrderValid = false },
		func(r *desktopcontract.AccessibilityReport) { r.KeyboardWorkflowValid = false },
		func(r *desktopcontract.AccessibilityReport) { r.ReducedMotionHonored = false },
		func(r *desktopcontract.AccessibilityReport) { r.MinimumContrast = 4.49 },
		func(r *desktopcontract.AccessibilityReport) { r.MinimumContrast = math.NaN() },
		func(r *desktopcontract.AccessibilityReport) { r.MinimumContrast = math.Inf(1) },
		func(r *desktopcontract.AccessibilityReport) { r.MinimumContrast = 21.01 },
		func(r *desktopcontract.AccessibilityReport) { r.ZoomLayoutValid = false },
	}
	for i, edit := range edits {
		shell, f := newShellFixture(t, desktopcontract.PlatformLinux)
		edit(&f.access.report)
		profile := desktopcontract.AccessibilityProfile{Platform: desktopcontract.PlatformLinux, ReducedMotion: true, ZoomPercent: 200}
		if result := shell.VerifyAccessibility(context.Background(), profile); result.Outcome != protocolcommon.OutcomeFailed || result.Failure.Code != desktopcontract.FailureAccessibility {
			t.Fatalf("case %d=%+v", i, result)
		}
	}
}

func TestNativeShellPreservesCrashRecoveryAndPresentsRecoverableSurface(t *testing.T) {
	shell, f := newShellFixture(t, desktopcontract.PlatformMacOS)
	result := shell.PresentUnexpectedFailure(context.Background(), desktopcontract.FailureOriginFrontend, desktopcontract.LifecycleReady)
	if result.Outcome != protocolcommon.OutcomeSuccess || result.Value.Failure != desktopcontract.FailureFrontendCrash || result.Value.Recovery != desktopcontract.RecoveryOpenRecovery || result.Value.Ref == nil || len(f.crash.contexts) != 1 || len(f.errors.values) != 1 {
		t.Fatalf("recoverable crash=%+v contexts=%+v errors=%+v", result, f.crash.contexts, f.errors.values)
	}
	if got := f.crash.contexts[0]; got.Origin != desktopcontract.FailureOriginFrontend || got.Lifecycle != desktopcontract.LifecycleReady || !got.At.Equal(shellTestNow) {
		t.Fatalf("crash context=%+v", got)
	}

	shell, f = newShellFixture(t, desktopcontract.PlatformMacOS)
	f.crash.err = errors.New("recovery unavailable")
	result = shell.PresentUnexpectedFailure(context.Background(), desktopcontract.FailureOriginBackend, desktopcontract.LifecycleStarting)
	if result.Outcome != protocolcommon.OutcomeSuccess || result.Value.Ref != nil || result.Value.Recovery != desktopcontract.RecoveryRetry || len(f.errors.values) != 1 {
		t.Fatalf("fallback crash=%+v", result)
	}

	shell, f = newShellFixture(t, desktopcontract.PlatformMacOS)
	f.crash.ref.ID = "/Users/private/recovery.json"
	result = shell.PresentUnexpectedFailure(context.Background(), desktopcontract.FailureOriginBackend, desktopcontract.LifecycleReady)
	if result.Value.Ref != nil || result.Value.Recovery != desktopcontract.RecoveryRetry {
		t.Fatalf("native recovery path crossed surface=%+v", result)
	}
}

func TestNativeShellContainsAdapterPanicAndNeverReturnsPanicText(t *testing.T) {
	shell, f := newShellFixture(t, desktopcontract.PlatformWindows)
	f.window.panicOnDisplays = true
	result := shell.Restore(context.Background())
	if result.Outcome != protocolcommon.OutcomeFailed || result.Failure.Code != desktopcontract.FailureBackendPanic {
		t.Fatalf("panic result=%+v", result)
	}
	if len(f.crash.contexts) != 1 || len(f.errors.values) != 1 || !f.crash.bounded[0] || !f.errors.bounded[0] {
		t.Fatalf("panic did not preserve/present with bounded context: crash=%+v surface=%+v", f.crash, f.errors)
	}
	encoded, err := json.Marshal(result)
	if err != nil || shellContains(string(encoded), "private native path") {
		t.Fatalf("panic leaked: %s, %v", encoded, err)
	}

	shell, f = newShellFixture(t, desktopcontract.PlatformWindows)
	f.external.panic = true
	resultExternal := shell.OpenExternal(context.Background(), desktopcontract.ExternalTarget{Kind: desktopcontract.ExternalWebLink, Value: "https://layerdraw.com"})
	if resultExternal.Outcome != protocolcommon.OutcomeFailed || resultExternal.Failure.Code != desktopcontract.FailureBackendPanic {
		t.Fatalf("external panic=%+v", resultExternal)
	}

	shell, f = newShellFixture(t, desktopcontract.PlatformWindows)
	f.window.panicOnDisplays = true
	f.crash.panic = true
	result = shell.Restore(context.Background())
	if result.Outcome != protocolcommon.OutcomeFailed || len(f.errors.values) != 1 {
		t.Fatalf("preserve panic prevented error presentation: result=%+v surfaces=%+v", result, f.errors.values)
	}
}

func TestNativeShellEveryFailureProducesClosedRedactedLog(t *testing.T) {
	shell, f := newShellFixture(t, desktopcontract.PlatformMacOS)
	f.window.displayErr = errors.New("/Users/private/document.ldl token=secret")
	result := shell.Restore(context.Background())
	if result.Outcome != protocolcommon.OutcomeFailed || len(f.logs.values) == 0 {
		t.Fatalf("failure log missing: result=%+v logs=%+v", result, f.logs.values)
	}
	last := f.logs.values[len(f.logs.values)-1]
	if last.Event != desktopcontract.EventOperationFailed || last.Failure == nil || *last.Failure != desktopcontract.FailureWindowState {
		t.Fatalf("closed failure log=%+v", last)
	}
	encoded, err := json.Marshal(f.logs.values)
	if err != nil || shellContains(string(encoded), "Users/private") || shellContains(string(encoded), "secret") {
		t.Fatalf("failure log leaked adapter text: %s", encoded)
	}
}

// TestCurrentSettingsFailsBeforeRestoreAndReturnsAfter pins the restore gate:
// zero-value settings are never observable as authoritative.
func TestCurrentSettingsFailsBeforeRestoreAndReturnsAfter(t *testing.T) {
	shell, _ := newShellFixture(t, desktopcontract.PlatformMacOS)
	if result := shell.CurrentSettings(context.Background()); result.Outcome == protocolcommon.OutcomeSuccess {
		t.Fatalf("settings before restore=%+v", result)
	}
	if restored := shell.Restore(context.Background()); restored.Outcome != protocolcommon.OutcomeSuccess {
		t.Fatalf("restore=%+v", restored)
	}
	if result := shell.CurrentSettings(context.Background()); result.Outcome != protocolcommon.OutcomeSuccess {
		t.Fatalf("settings after restore=%+v", result)
	}
}
