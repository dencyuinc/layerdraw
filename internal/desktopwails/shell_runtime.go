// SPDX-License-Identifier: LicenseRef-LayerDraw-1.0

package desktopwails

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"time"

	"github.com/dencyuinc/layerdraw/internal/desktopcontract"
	wailsruntime "github.com/wailsapp/wails/v2/pkg/runtime"
)

const accessibilityRequestEvent = "layerdraw:accessibility-probe"

type accessibilitySubmission struct {
	ch chan desktopcontract.AccessibilityReport
}

type wailsShellRuntime struct {
	screens         func(context.Context) ([]wailsruntime.Screen, error)
	windowPosition  func(context.Context) (int, int)
	windowSize      func(context.Context) (int, int)
	windowMaximized func(context.Context) bool
	unmaximize      func(context.Context)
	setSize         func(context.Context, int, int)
	setPosition     func(context.Context, int, int)
	maximize        func(context.Context)
	setSystemTheme  func(context.Context)
	setLightTheme   func(context.Context)
	setDarkTheme    func(context.Context)
	execJS          func(context.Context, string)
	emit            func(context.Context, string, ...any)
}

func productionWailsShellRuntime() wailsShellRuntime {
	return wailsShellRuntime{
		screens: wailsruntime.ScreenGetAll, windowPosition: wailsruntime.WindowGetPosition,
		windowSize: wailsruntime.WindowGetSize, windowMaximized: wailsruntime.WindowIsMaximised,
		unmaximize: wailsruntime.WindowUnmaximise, setSize: wailsruntime.WindowSetSize,
		setPosition: wailsruntime.WindowSetPosition, maximize: wailsruntime.WindowMaximise,
		setSystemTheme: wailsruntime.WindowSetSystemDefaultTheme, setLightTheme: wailsruntime.WindowSetLightTheme,
		setDarkTheme: wailsruntime.WindowSetDarkTheme, execJS: wailsruntime.WindowExecJS, emit: wailsruntime.EventsEmit,
	}
}

// WailsShellBridge is the production implementation of the #125 native
// window/settings boundary. The same object is bound into Wails so the
// packaged frontend can return its DOM accessibility audit by opaque ID.
type WailsShellBridge struct {
	mu       sync.Mutex
	settings desktopcontract.DesktopSettings
	probes   map[string]*accessibilitySubmission
	nextID   uint64
	ctx      context.Context
	runtime  wailsShellRuntime
}

func (b *WailsShellBridge) setContext(ctx context.Context) {
	b.mu.Lock()
	b.ctx = ctx
	b.mu.Unlock()
}

func (b *WailsShellBridge) context() context.Context {
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.ctx == nil {
		return context.Background()
	}
	return b.ctx
}

func (b *WailsShellBridge) contextReady() bool {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.ctx != nil
}

func NewWailsShellBridge() *WailsShellBridge {
	return &WailsShellBridge{
		settings: desktopcontract.DesktopSettings{SchemaVersion: desktopcontract.SettingsSchemaVersion, Theme: desktopcontract.ThemeSystem, ZoomPercent: 100},
		probes:   make(map[string]*accessibilitySubmission),
		runtime:  productionWailsShellRuntime(),
	}
}

func newWailsShellBridge(runtime wailsShellRuntime) *WailsShellBridge {
	bridge := NewWailsShellBridge()
	bridge.runtime = runtime
	return bridge
}

func (b *WailsShellBridge) Displays(ctx context.Context) ([]desktopcontract.Display, error) {
	screens, err := b.runtime.screens(ctx)
	if err != nil || len(screens) == 0 {
		return nil, errors.New("Wails display inventory unavailable")
	}
	result := make([]desktopcontract.Display, 0, len(screens))
	for index, screen := range screens {
		if !screen.IsPrimary {
			continue
		}
		width, height := screen.Size.Width, screen.Size.Height
		if width == 0 || height == 0 {
			width, height = screen.Width, screen.Height
		}
		display := desktopcontract.Display{ID: fmt.Sprintf("wails-display-%d", index), Primary: screen.IsPrimary, Work: desktopcontract.Rectangle{Width: width, Height: height}}
		if display.Validate() {
			result = append(result, display)
		}
	}
	if len(result) == 0 {
		return nil, errors.New("Wails display inventory invalid")
	}
	return result, nil
}

func (b *WailsShellBridge) Snapshot(ctx context.Context) (desktopcontract.WindowState, desktopcontract.DesktopSettings, error) {
	x, y := b.runtime.windowPosition(ctx)
	width, height := b.runtime.windowSize(ctx)
	b.mu.Lock()
	settings := b.settings
	b.mu.Unlock()
	window := desktopcontract.WindowState{SchemaVersion: desktopcontract.SettingsSchemaVersion, Bounds: desktopcontract.Rectangle{X: x, Y: y, Width: width, Height: height}, Maximized: b.runtime.windowMaximized(ctx)}
	if !window.Validate() || !settings.Validate() {
		return desktopcontract.WindowState{}, desktopcontract.DesktopSettings{}, errors.New("Wails window snapshot invalid")
	}
	return window, settings, nil
}

func (b *WailsShellBridge) ApplyWindow(ctx context.Context, value desktopcontract.WindowState) error {
	if !value.Validate() {
		return errors.New("Wails window state invalid")
	}
	if b.runtime.windowMaximized(ctx) {
		b.runtime.unmaximize(ctx)
	}
	b.runtime.setSize(ctx, value.Bounds.Width, value.Bounds.Height)
	b.runtime.setPosition(ctx, value.Bounds.X, value.Bounds.Y)
	if value.Maximized {
		b.runtime.maximize(ctx)
	}
	return nil
}

func (b *WailsShellBridge) ApplySettings(ctx context.Context, value desktopcontract.DesktopSettings) error {
	if !value.Validate() {
		return errors.New("Wails settings invalid")
	}
	colorScheme := string(value.Theme)
	switch value.Theme {
	case desktopcontract.ThemeSystem:
		b.runtime.setSystemTheme(ctx)
		colorScheme = "light dark"
	case desktopcontract.ThemeLight:
		b.runtime.setLightTheme(ctx)
	case desktopcontract.ThemeDark:
		b.runtime.setDarkTheme(ctx)
	}
	b.runtime.execJS(ctx, fmt.Sprintf("document.documentElement.dataset.theme=%q;document.documentElement.style.colorScheme=%q;document.documentElement.style.zoom=%q", value.Theme, colorScheme, fmt.Sprintf("%d%%", value.ZoomPercent)))
	b.mu.Lock()
	b.settings = value
	b.mu.Unlock()
	return nil
}

func (b *WailsShellBridge) VerifyPackagedAccessibility(ctx context.Context, profile desktopcontract.AccessibilityProfile) (desktopcontract.AccessibilityReport, error) {
	if !profile.Platform.Validate() || profile.Platform != CurrentPlatform() || profile.ZoomPercent < 50 || profile.ZoomPercent > 300 {
		return desktopcontract.AccessibilityReport{}, errors.New("packaged accessibility profile invalid")
	}
	b.mu.Lock()
	b.nextID++
	id := fmt.Sprintf("probe-%d", b.nextID)
	submission := &accessibilitySubmission{ch: make(chan desktopcontract.AccessibilityReport, 1)}
	b.probes[id] = submission
	b.mu.Unlock()
	defer func() {
		b.mu.Lock()
		delete(b.probes, id)
		b.mu.Unlock()
	}()
	b.runtime.emit(ctx, accessibilityRequestEvent, id, profile)
	probeCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	select {
	case report := <-submission.ch:
		return report, nil
	case <-probeCtx.Done():
		return desktopcontract.AccessibilityReport{}, errors.New("packaged accessibility probe timed out")
	}
}

// SubmitAccessibilityReport is called only by the embedded packaged frontend.
// Unknown, expired, duplicate, or malformed submissions are rejected.
func (b *WailsShellBridge) SubmitAccessibilityReport(id string, report desktopcontract.AccessibilityReport) error {
	if id == "" || report.MinimumContrast < 0 || report.MinimumContrast > 21 {
		return errors.New("packaged accessibility report invalid")
	}
	b.mu.Lock()
	submission := b.probes[id]
	if submission == nil {
		b.mu.Unlock()
		return errors.New("packaged accessibility request unavailable")
	}
	delete(b.probes, id)
	b.mu.Unlock()
	select {
	case submission.ch <- report:
		return nil
	default:
		return errors.New("packaged accessibility report duplicate")
	}
}
