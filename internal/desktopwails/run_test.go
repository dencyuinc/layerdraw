// SPDX-License-Identifier: LicenseRef-LayerDraw-1.0

package desktopwails

import (
	"context"
	"errors"
	"testing"

	"github.com/dencyuinc/layerdraw/internal/desktopapp"
	"github.com/dencyuinc/layerdraw/internal/desktopcontract"
	"github.com/wailsapp/wails/v2/pkg/menu"
	wailsruntime "github.com/wailsapp/wails/v2/pkg/runtime"
)

// availableNewOpenCommandRouter reports New/Open Project as available but
// always fails Route. This lets the "File" menu callbacks built by
// nativeMenu run all the way through invokeNativeCommand without reaching
// the real Wails dialog/event runtime, which requires a live application
// context that unit tests cannot provide.
type availableNewOpenCommandRouter struct{}

func (availableNewOpenCommandRouter) Status(context.Context) ([]desktopcontract.CommandStatus, error) {
	return []desktopcontract.CommandStatus{
		{ID: desktopcontract.CommandNewProject, State: desktopcontract.CommandAvailable, Generation: "1"},
		{ID: desktopcontract.CommandOpenProject, State: desktopcontract.CommandAvailable, Generation: "1"},
	}, nil
}

func (availableNewOpenCommandRouter) Route(context.Context, desktopcontract.CommandInvocation) (desktopcontract.CommandStatus, error) {
	return desktopcontract.CommandStatus{}, errors.New("native command rejected")
}

// TestNativeMenuFileCommandsInvokeNativeCommandWithoutPublishingOnFailure
// exercises the File > New/Open Project menu callbacks wired in nativeMenu.
// The callbacks must call invokeNativeCommand and only publish the
// desktop-project lifecycle event when that call reports success; here the
// backend rejects every command, so the menu handlers must run to
// completion without emitting anything through the (unavailable in tests)
// Wails runtime context.
func TestNativeMenuFileCommandsInvokeNativeCommandWithoutPublishingOnFailure(t *testing.T) {
	runtime := &fakeWailsShellRuntime{
		screens: []wailsruntime.Screen{{IsPrimary: true, Width: 1920, Height: 1080}},
		width:   1280, height: 800, emitted: make(chan []any, 1),
	}
	bridge := newWailsShellBridge(runtime.calls())
	native, err := desktopapp.NewPlatformNativeShell(desktopapp.PlatformNativeShellConfig{
		Platform: CurrentPlatform(), StateRoot: t.TempDir(), Runtime: bridge,
		Commands: availableNewOpenCommandRouter{}, CrashRecovery: probeCrashRecovery{},
		Errors: wailsErrorSurface{runtime: &nativeStub{}},
	})
	if err != nil {
		t.Fatal(err)
	}

	built := nativeMenu(nil, native.Shell, bridge)
	for _, top := range []string{"LayerDraw", "File", "View", "Window", "Help"} {
		findSubmenuItem(t, built, top)
	}
	file := findSubmenuItem(t, built, "File")
	newProject := findMenuAction(t, file.SubMenu, "New Project")
	openProject := findMenuAction(t, file.SubMenu, "Open Project…")

	newProject.Click(&menu.CallbackData{MenuItem: newProject})
	openProject.Click(&menu.CallbackData{MenuItem: openProject})

	select {
	case emitted := <-runtime.emitted:
		t.Fatalf("rejected native command still published an event: %v", emitted)
	default:
	}
}

func findSubmenuItem(t *testing.T, built *menu.Menu, label string) *menu.MenuItem {
	t.Helper()
	for _, item := range built.Items {
		if item.Label == label {
			return item
		}
	}
	t.Fatalf("menu item %q not found", label)
	return nil
}

func findMenuAction(t *testing.T, sub *menu.Menu, label string) *menu.MenuItem {
	t.Helper()
	if sub == nil {
		t.Fatalf("submenu is nil looking for %q", label)
	}
	return findSubmenuItem(t, sub, label)
}
