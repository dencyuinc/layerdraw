// SPDX-License-Identifier: LicenseRef-LayerDraw-1.0

package desktopapp

import (
	"context"
	"math"
	"sync"
	"time"

	"github.com/dencyuinc/layerdraw/gen/go/protocolcommon"
	"github.com/dencyuinc/layerdraw/internal/desktopcontract"
)

type NativeShellConfig struct {
	Platform      desktopcontract.DesktopPlatform
	Settings      desktopcontract.SettingsStore
	Window        desktopcontract.NativeWindowPort
	Commands      desktopcontract.CommandRouter
	External      desktopcontract.ExternalOpenPort
	Associations  desktopcontract.FileAssociationPort
	CrashRecovery desktopcontract.CrashRecoveryPort
	Errors        desktopcontract.ErrorSurfacePort
	Accessibility desktopcontract.AccessibilityProbe
	Logs          desktopcontract.StructuredLogPort
	Now           func() time.Time
}

// NativeShell owns only native window, settings, menu/shortcut, safe OS handoff,
// accessibility-probe and failure-presentation mechanics. Project lifecycle and
// UI commands are injected owner adapters so #123 and #124 remain authoritative.
type NativeShell struct {
	config NativeShellConfig

	persist  sync.Mutex
	mu       sync.Mutex
	restored bool
	state    desktopcontract.PersistedShellState
}

func NewNativeShell(config NativeShellConfig) (*NativeShell, error) {
	if !config.Platform.Validate() || config.Settings == nil || config.Window == nil ||
		config.Commands == nil || config.External == nil || config.Associations == nil ||
		config.CrashRecovery == nil || config.Errors == nil || config.Accessibility == nil ||
		config.Logs == nil {
		return nil, errIncompleteNativeShell
	}
	if config.Now == nil {
		config.Now = time.Now
	}
	return &NativeShell{config: config}, nil
}

var errIncompleteNativeShell = &nativeShellConfigError{}

type nativeShellConfigError struct{}

func (*nativeShellConfigError) Error() string {
	return "desktop native shell composition is incomplete"
}

func (s *NativeShell) Restore(ctx context.Context) (result desktopcontract.Result[desktopcontract.PersistedShellState]) {
	defer finishShellResult(s, ctx, &result)
	defer containShellPanic(s, ctx, &result)
	s.persist.Lock()
	defer s.persist.Unlock()
	displays, err := s.config.Window.Displays(ctx)
	if err != nil {
		return shellFailed[desktopcontract.PersistedShellState](desktopcontract.FailureWindowState, true, desktopcontract.RecoveryRetry)
	}
	primary, validDisplays := usableDisplays(displays)
	if primary == nil {
		return shellFailed[desktopcontract.PersistedShellState](desktopcontract.FailureWindowState, false, desktopcontract.RecoveryExit)
	}
	previousWindow, previousSettings, err := s.config.Window.Snapshot(ctx)
	if err != nil || !previousWindow.Validate() || !previousSettings.Validate() {
		return shellFailed[desktopcontract.PersistedShellState](desktopcontract.FailureWindowState, true, desktopcontract.RecoveryRetry)
	}
	loaded, loadErr := s.config.Settings.Load(ctx)
	recovered := loadErr != nil || !loaded.Validate()
	if recovered {
		loaded = defaultShellState(*primary)
	}
	originalWindow := loaded.Window
	loaded.Window = normalizeWindow(loaded.Window, validDisplays, *primary)
	if err := s.config.Window.ApplyWindow(ctx, loaded.Window); err != nil {
		if !s.rollbackNativeState(ctx, previousWindow, previousSettings) {
			return compensationFailure[desktopcontract.PersistedShellState](s, ctx)
		}
		return shellFailed[desktopcontract.PersistedShellState](desktopcontract.FailureWindowState, true, desktopcontract.RecoveryRetry)
	}
	if err := s.config.Window.ApplySettings(ctx, loaded.Settings); err != nil {
		if !s.rollbackNativeState(ctx, previousWindow, previousSettings) {
			return compensationFailure[desktopcontract.PersistedShellState](s, ctx)
		}
		return shellFailed[desktopcontract.PersistedShellState](desktopcontract.FailureSettings, true, desktopcontract.RecoveryRetry)
	}
	if recovered || loaded.Window != originalWindow {
		if err := s.config.Settings.Save(ctx, loaded); err != nil {
			if !s.rollbackNativeState(ctx, previousWindow, previousSettings) {
				return compensationFailure[desktopcontract.PersistedShellState](s, ctx)
			}
			return shellFailed[desktopcontract.PersistedShellState](desktopcontract.FailureSettings, true, desktopcontract.RecoveryRetry)
		}
	}
	s.mu.Lock()
	s.state, s.restored = loaded, true
	s.mu.Unlock()
	event := desktopcontract.EventSettingsRestored
	level := desktopcontract.LogInfo
	if recovered {
		event, level = desktopcontract.EventSettingsRecovered, desktopcontract.LogWarn
	}
	s.log(ctx, level, event, nil, nil)
	return desktopcontract.Result[desktopcontract.PersistedShellState]{Outcome: protocolcommon.OutcomeSuccess, Value: loaded}
}

func (s *NativeShell) UpdateSettings(ctx context.Context, settings desktopcontract.DesktopSettings) (result desktopcontract.Result[desktopcontract.DesktopSettings]) {
	defer finishShellResult(s, ctx, &result)
	defer containShellPanic(s, ctx, &result)
	s.persist.Lock()
	defer s.persist.Unlock()
	if !settings.Validate() {
		return shellFailed[desktopcontract.DesktopSettings](desktopcontract.FailureSettings, false, desktopcontract.RecoveryRetry)
	}
	s.mu.Lock()
	if !s.restored {
		s.mu.Unlock()
		return shellFailed[desktopcontract.DesktopSettings](desktopcontract.FailureSettings, true, desktopcontract.RecoveryRetry)
	}
	next := s.state
	s.mu.Unlock()
	previous := next.Settings
	next.Settings = settings
	if err := s.config.Window.ApplySettings(ctx, settings); err != nil {
		return shellFailed[desktopcontract.DesktopSettings](desktopcontract.FailureSettings, true, desktopcontract.RecoveryRetry)
	}
	if err := s.config.Settings.Save(ctx, next); err != nil {
		rollbackCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 2*time.Second)
		rollbackErr := safeApplySettings(rollbackCtx, s.config.Window, previous)
		cancel()
		if rollbackErr != nil {
			return compensationFailure[desktopcontract.DesktopSettings](s, ctx)
		}
		return shellFailed[desktopcontract.DesktopSettings](desktopcontract.FailureSettings, true, desktopcontract.RecoveryRetry)
	}
	s.mu.Lock()
	s.state = next
	s.mu.Unlock()
	s.log(ctx, desktopcontract.LogInfo, desktopcontract.EventSettingsSaved, nil, nil)
	return desktopcontract.Result[desktopcontract.DesktopSettings]{Outcome: protocolcommon.OutcomeSuccess, Value: settings}
}

func (s *NativeShell) SaveWindow(ctx context.Context, state desktopcontract.WindowState) (result desktopcontract.Result[desktopcontract.WindowState]) {
	defer finishShellResult(s, ctx, &result)
	defer containShellPanic(s, ctx, &result)
	s.persist.Lock()
	defer s.persist.Unlock()
	displays, err := s.config.Window.Displays(ctx)
	if err != nil {
		return shellFailed[desktopcontract.WindowState](desktopcontract.FailureWindowState, true, desktopcontract.RecoveryRetry)
	}
	primary, validDisplays := usableDisplays(displays)
	if primary == nil {
		return shellFailed[desktopcontract.WindowState](desktopcontract.FailureWindowState, false, desktopcontract.RecoveryExit)
	}
	normalized := normalizeWindow(state, validDisplays, *primary)
	s.mu.Lock()
	if !s.restored {
		s.mu.Unlock()
		return shellFailed[desktopcontract.WindowState](desktopcontract.FailureWindowState, true, desktopcontract.RecoveryRetry)
	}
	next := s.state
	s.mu.Unlock()
	next.Window = normalized
	if err := s.config.Settings.Save(ctx, next); err != nil {
		return shellFailed[desktopcontract.WindowState](desktopcontract.FailureWindowState, true, desktopcontract.RecoveryRetry)
	}
	s.mu.Lock()
	s.state = next
	s.mu.Unlock()
	return desktopcontract.Result[desktopcontract.WindowState]{Outcome: protocolcommon.OutcomeSuccess, Value: normalized}
}

func (s *NativeShell) CommandStatus(ctx context.Context) (result desktopcontract.Result[[]desktopcontract.CommandStatus]) {
	defer finishShellResult(s, ctx, &result)
	defer containShellPanic(s, ctx, &result)
	values, err := s.config.Commands.Status(ctx)
	if err != nil || !validCommandStatuses(values) {
		return shellFailed[[]desktopcontract.CommandStatus](desktopcontract.FailureCommandUnavailable, true, desktopcontract.RecoveryRetry)
	}
	copyOfValues := append([]desktopcontract.CommandStatus(nil), values...)
	return desktopcontract.Result[[]desktopcontract.CommandStatus]{Outcome: protocolcommon.OutcomeSuccess, Value: copyOfValues}
}

func (s *NativeShell) InvokeCommand(ctx context.Context, invocation desktopcontract.CommandInvocation) (result desktopcontract.Result[desktopcontract.CommandStatus]) {
	defer finishShellResult(s, ctx, &result)
	defer containShellPanic(s, ctx, &result)
	if !invocation.Validate() {
		return shellFailed[desktopcontract.CommandStatus](desktopcontract.FailureCommandUnavailable, false, desktopcontract.RecoveryRetry)
	}
	status, err := s.config.Commands.Route(ctx, invocation)
	if err != nil || status.ID != invocation.ID || status.Generation != invocation.StatusGeneration || !status.Validate() {
		return shellFailed[desktopcontract.CommandStatus](desktopcontract.FailureCommandUnavailable, true, desktopcontract.RecoveryRetry)
	}
	if status.State != desktopcontract.CommandAvailable {
		s.log(ctx, desktopcontract.LogWarn, desktopcontract.EventCommandRejected, nil, &invocation.ID)
		return desktopcontract.Result[desktopcontract.CommandStatus]{Outcome: protocolcommon.OutcomeRejected, Value: status}
	}
	s.log(ctx, desktopcontract.LogInfo, desktopcontract.EventCommandInvoked, nil, &invocation.ID)
	return desktopcontract.Result[desktopcontract.CommandStatus]{Outcome: protocolcommon.OutcomeSuccess, Value: status}
}

func (s *NativeShell) OpenExternal(ctx context.Context, target desktopcontract.ExternalTarget) (result desktopcontract.Result[struct{}]) {
	defer finishShellResult(s, ctx, &result)
	defer containShellPanic(s, ctx, &result)
	if !target.Validate() {
		code := desktopcontract.FailureExternalTarget
		s.log(ctx, desktopcontract.LogWarn, desktopcontract.EventExternalDenied, &code, nil)
		return shellFailed[struct{}](code, false, desktopcontract.RecoveryRetry)
	}
	if err := s.config.External.OpenExternal(ctx, target); err != nil {
		return shellFailed[struct{}](desktopcontract.FailureAdapterUnavailable, true, desktopcontract.RecoveryRetry)
	}
	s.log(ctx, desktopcontract.LogInfo, desktopcontract.EventExternalOpened, nil, nil)
	return desktopcontract.Result[struct{}]{Outcome: protocolcommon.OutcomeSuccess}
}

func (s *NativeShell) NextFileAssociation(ctx context.Context) (result desktopcontract.Result[desktopcontract.FileAssociationHandoff]) {
	defer finishShellResult(s, ctx, &result)
	defer containShellPanic(s, ctx, &result)
	value, err := s.config.Associations.Next(ctx)
	if err != nil || !validOpaqueReference(value.Token) ||
		(value.Kind != desktopcontract.FileAssociationLDL && value.Kind != desktopcontract.FileAssociationLayerDraw) {
		return shellFailed[desktopcontract.FileAssociationHandoff](desktopcontract.FailureAdapterUnavailable, true, desktopcontract.RecoveryRetry)
	}
	return desktopcontract.Result[desktopcontract.FileAssociationHandoff]{Outcome: protocolcommon.OutcomeSuccess, Value: value}
}

func (s *NativeShell) PresentUnexpectedFailure(ctx context.Context, origin desktopcontract.UnexpectedFailureOrigin, lifecycle desktopcontract.LifecycleState) (result desktopcontract.Result[desktopcontract.ErrorSurface]) {
	defer finishShellResult(s, ctx, &result)
	defer containShellPanic(s, ctx, &result)
	if (origin != desktopcontract.FailureOriginBackend && origin != desktopcontract.FailureOriginFrontend) || !lifecycle.Validate() {
		return shellFailed[desktopcontract.ErrorSurface](desktopcontract.FailureCrashRecovery, false, desktopcontract.RecoveryExit)
	}
	surface, presented := s.recoverAndPresent(ctx, origin, lifecycle)
	if !presented {
		return shellFailed[desktopcontract.ErrorSurface](desktopcontract.FailureCrashRecovery, true, desktopcontract.RecoveryRetry)
	}
	code := surface.Failure
	s.log(ctx, desktopcontract.LogError, desktopcontract.EventFailurePresented, &code, nil)
	return desktopcontract.Result[desktopcontract.ErrorSurface]{Outcome: protocolcommon.OutcomeSuccess, Value: surface}
}

func (s *NativeShell) VerifyAccessibility(ctx context.Context, profile desktopcontract.AccessibilityProfile) (result desktopcontract.Result[desktopcontract.AccessibilityReport]) {
	defer finishShellResult(s, ctx, &result)
	defer containShellPanic(s, ctx, &result)
	if profile.Platform != s.config.Platform || profile.ZoomPercent < 50 || profile.ZoomPercent > 300 ||
		(profile.ViewerMode != "" && (profile.ProbeID == "" || (profile.ViewerMode != "2d" && profile.ViewerMode != "2.5d") || profile.WindowWidth < 960 || profile.WindowHeight < 640)) {
		return shellFailed[desktopcontract.AccessibilityReport](desktopcontract.FailureAccessibility, false, desktopcontract.RecoveryRetry)
	}
	report, err := s.config.Accessibility.VerifyPackaged(ctx, profile)
	if err != nil || !report.LabelsComplete || !report.FocusOrderValid || !report.KeyboardWorkflowValid ||
		(profile.ScreenReader && !report.ScreenReaderSemantics) ||
		(profile.ReducedMotion && !report.ReducedMotionHonored) || math.IsNaN(report.MinimumContrast) || math.IsInf(report.MinimumContrast, 0) ||
		report.MinimumContrast < 4.5 || report.MinimumContrast > 21 || !report.ZoomLayoutValid ||
		(profile.ViewerMode != "" && (report.ViewerMode != profile.ViewerMode || report.ViewportWidth == 0 || report.ViewportHeight == 0 ||
			report.ViewerItemCount == 0 || report.ViewerRelationCount == 0 || !report.ViewerKeyboardSelect ||
			(profile.ViewerMode == "2d" && report.RendererBackend != "svg") ||
			(profile.ViewerMode == "2.5d" && (report.RendererBackend != "three.js" || !report.WebGLVerified || report.ViewerCrossLayerCount == 0)))) {
		return shellFailed[desktopcontract.AccessibilityReport](desktopcontract.FailureAccessibility, false, desktopcontract.RecoveryRetry)
	}
	return desktopcontract.Result[desktopcontract.AccessibilityReport]{Outcome: protocolcommon.OutcomeSuccess, Value: report}
}

func containShellPanic[T any](s *NativeShell, ctx context.Context, result *desktopcontract.Result[T]) {
	if recover() != nil {
		_, _ = s.recoverAndPresent(ctx, desktopcontract.FailureOriginBackend, desktopcontract.LifecycleRecovery)
		*result = shellFailed[T](desktopcontract.FailureBackendPanic, false, desktopcontract.RecoveryExit)
	}
}

func finishShellResult[T any](s *NativeShell, ctx context.Context, result *desktopcontract.Result[T]) {
	if result.Outcome == protocolcommon.OutcomeFailed && result.Failure != nil {
		code := result.Failure.Code
		s.log(ctx, desktopcontract.LogError, desktopcontract.EventOperationFailed, &code, nil)
	} else if result.Outcome == protocolcommon.OutcomeRejected {
		code := desktopcontract.FailureCommandUnavailable
		s.log(ctx, desktopcontract.LogWarn, desktopcontract.EventOperationRejected, &code, nil)
	}
}

func (s *NativeShell) recoverAndPresent(ctx context.Context, origin desktopcontract.UnexpectedFailureOrigin, lifecycle desktopcontract.LifecycleState) (desktopcontract.ErrorSurface, bool) {
	failureCode := desktopcontract.FailureBackendPanic
	if origin == desktopcontract.FailureOriginFrontend {
		failureCode = desktopcontract.FailureFrontendCrash
	}
	surface := desktopcontract.ErrorSurface{Failure: failureCode, Recovery: desktopcontract.RecoveryRetry}
	preserveCtx, preserveCancel := context.WithTimeout(context.WithoutCancel(ctx), 2*time.Second)
	ref, err := safePreserveRecovery(preserveCtx, s.config.CrashRecovery, desktopcontract.CrashContext{Origin: origin, Lifecycle: lifecycle, At: s.config.Now().UTC()})
	preserveCancel()
	if err == nil && validOpaqueReference(ref.ID) {
		surface.Recovery = desktopcontract.RecoveryOpenRecovery
		surface.Ref = &ref
	}
	presentCtx, presentCancel := context.WithTimeout(context.WithoutCancel(ctx), 2*time.Second)
	presentErr := safePresentError(presentCtx, s.config.Errors, surface)
	presentCancel()
	return surface, presentErr == nil
}

func safePreserveRecovery(ctx context.Context, port desktopcontract.CrashRecoveryPort, input desktopcontract.CrashContext) (result desktopcontract.RecoveryRef, err error) {
	defer func() {
		if recover() != nil {
			result, err = desktopcontract.RecoveryRef{}, errInjectedPanic
		}
	}()
	return port.Preserve(ctx, input)
}

func safePresentError(ctx context.Context, port desktopcontract.ErrorSurfacePort, input desktopcontract.ErrorSurface) (err error) {
	defer func() {
		if recover() != nil {
			err = errInjectedPanic
		}
	}()
	return port.Present(ctx, input)
}

func (s *NativeShell) rollbackNativeState(ctx context.Context, window desktopcontract.WindowState, settings desktopcontract.DesktopSettings) bool {
	bounded, cancel := context.WithTimeout(context.WithoutCancel(ctx), 2*time.Second)
	defer cancel()
	windowErr := safeApplyWindow(bounded, s.config.Window, window)
	settingsErr := safeApplySettings(bounded, s.config.Window, settings)
	return windowErr == nil && settingsErr == nil
}

func safeApplyWindow(ctx context.Context, port desktopcontract.NativeWindowPort, value desktopcontract.WindowState) (err error) {
	defer func() {
		if recover() != nil {
			err = errInjectedPanic
		}
	}()
	return port.ApplyWindow(ctx, value)
}

func safeApplySettings(ctx context.Context, port desktopcontract.NativeWindowPort, value desktopcontract.DesktopSettings) (err error) {
	defer func() {
		if recover() != nil {
			err = errInjectedPanic
		}
	}()
	return port.ApplySettings(ctx, value)
}

func compensationFailure[T any](s *NativeShell, ctx context.Context) desktopcontract.Result[T] {
	_, _ = s.recoverAndPresent(ctx, desktopcontract.FailureOriginBackend, desktopcontract.LifecycleRecovery)
	return shellFailed[T](desktopcontract.FailureCrashRecovery, false, desktopcontract.RecoveryOpenRecovery)
}

func (s *NativeShell) log(ctx context.Context, level desktopcontract.LogLevel, event desktopcontract.ShellEvent, failure *desktopcontract.FailureCode, command *desktopcontract.CommandID) {
	defer func() { _ = recover() }()
	_ = s.config.Logs.Write(context.WithoutCancel(ctx), desktopcontract.StructuredLogRecord{
		At: s.config.Now().UTC(), Level: level, Event: event, Platform: s.config.Platform, Failure: failure, Command: command,
	})
}

func shellFailed[T any](code desktopcontract.FailureCode, retryable bool, recovery desktopcontract.RecoveryAction) desktopcontract.Result[T] {
	value := desktopcontract.Failure{Code: code, Retryable: retryable, Component: desktopcontract.ComponentBindingShell, Recovery: recovery}
	return desktopcontract.Result[T]{Outcome: protocolcommon.OutcomeFailed, Failure: &value}
}

func usableDisplays(displays []desktopcontract.Display) (*desktopcontract.Display, []desktopcontract.Display) {
	valid := make([]desktopcontract.Display, 0, len(displays))
	for _, display := range displays {
		if display.Validate() {
			valid = append(valid, display)
		}
	}
	if len(valid) == 0 {
		return nil, nil
	}
	for i := range valid {
		if valid[i].Primary {
			return &valid[i], valid
		}
	}
	return &valid[0], valid
}

func defaultShellState(primary desktopcontract.Display) desktopcontract.PersistedShellState {
	width, height := min(1280, primary.Work.Width), min(800, primary.Work.Height)
	return desktopcontract.PersistedShellState{
		Settings: desktopcontract.DesktopSettings{SchemaVersion: desktopcontract.SettingsSchemaVersion, Theme: desktopcontract.ThemeSystem, ZoomPercent: 100},
		Window: desktopcontract.WindowState{SchemaVersion: desktopcontract.SettingsSchemaVersion, Bounds: desktopcontract.Rectangle{
			X: primary.Work.X + (primary.Work.Width-width)/2, Y: primary.Work.Y + (primary.Work.Height-height)/2, Width: width, Height: height,
		}},
	}
}

func normalizeWindow(state desktopcontract.WindowState, displays []desktopcontract.Display, primary desktopcontract.Display) desktopcontract.WindowState {
	bounds := state.Bounds
	if state.SchemaVersion != desktopcontract.SettingsSchemaVersion || bounds.Width < 640 || bounds.Height < 480 {
		return defaultShellState(primary).Window
	}
	var selected *desktopcontract.Display
	bestArea := 0
	for _, display := range displays {
		area := intersectionArea(bounds, display.Work)
		if area >= 64*64 && area > bestArea {
			candidate := display
			selected, bestArea = &candidate, area
		}
	}
	if selected == nil {
		width, height := min(bounds.Width, primary.Work.Width), min(bounds.Height, primary.Work.Height)
		bounds = desktopcontract.Rectangle{X: primary.Work.X + (primary.Work.Width-width)/2, Y: primary.Work.Y + (primary.Work.Height-height)/2, Width: width, Height: height}
	} else {
		bounds.Width = min(bounds.Width, selected.Work.Width)
		bounds.Height = min(bounds.Height, selected.Work.Height)
		bounds.X = min(max(bounds.X, selected.Work.X), selected.Work.X+selected.Work.Width-bounds.Width)
		bounds.Y = min(max(bounds.Y, selected.Work.Y), selected.Work.Y+selected.Work.Height-bounds.Height)
	}
	return desktopcontract.WindowState{SchemaVersion: desktopcontract.SettingsSchemaVersion, Bounds: bounds, Maximized: state.Maximized}
}

func intersectionArea(a, b desktopcontract.Rectangle) int {
	left, top := max(a.X, b.X), max(a.Y, b.Y)
	right, bottom := min(a.X+a.Width, b.X+b.Width), min(a.Y+a.Height, b.Y+b.Height)
	if right <= left || bottom <= top {
		return 0
	}
	return (right - left) * (bottom - top)
}

func validCommandStatuses(values []desktopcontract.CommandStatus) bool {
	seen := make(map[desktopcontract.CommandID]bool, len(values))
	for _, value := range values {
		if !value.Validate() || seen[value.ID] {
			return false
		}
		seen[value.ID] = true
	}
	return true
}

func validOpaqueReference(value string) bool {
	if len(value) == 0 || len(value) > 128 {
		return false
	}
	for _, character := range value {
		if (character >= 'a' && character <= 'z') || (character >= 'A' && character <= 'Z') ||
			(character >= '0' && character <= '9') || character == '-' || character == '_' {
			continue
		}
		return false
	}
	return true
}
