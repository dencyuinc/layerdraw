// SPDX-License-Identifier: LicenseRef-LayerDraw-1.0

package packaged_test

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/dencyuinc/layerdraw/gen/go/protocolcommon"
	"github.com/dencyuinc/layerdraw/internal/desktopapp"
	"github.com/dencyuinc/layerdraw/internal/desktopcontract"
)

type packagedDesktopRuntime struct {
	window   desktopcontract.WindowState
	settings desktopcontract.DesktopSettings
}

func (r *packagedDesktopRuntime) Displays(context.Context) ([]desktopcontract.Display, error) {
	return []desktopcontract.Display{{ID: "packaged-primary", Primary: true, Work: desktopcontract.Rectangle{Width: 1920, Height: 1080}}}, nil
}
func (r *packagedDesktopRuntime) Snapshot(context.Context) (desktopcontract.WindowState, desktopcontract.DesktopSettings, error) {
	return r.window, r.settings, nil
}
func (r *packagedDesktopRuntime) ApplyWindow(_ context.Context, value desktopcontract.WindowState) error {
	r.window = value
	return nil
}
func (r *packagedDesktopRuntime) ApplySettings(_ context.Context, value desktopcontract.DesktopSettings) error {
	r.settings = value
	return nil
}
func (r *packagedDesktopRuntime) VerifyPackagedAccessibility(_ context.Context, profile desktopcontract.AccessibilityProfile) (desktopcontract.AccessibilityReport, error) {
	return desktopcontract.AccessibilityReport{
		LabelsComplete: true, ScreenReaderSemantics: profile.ScreenReader, FocusOrderValid: true, KeyboardWorkflowValid: profile.KeyboardOnly,
		ReducedMotionHonored: profile.ReducedMotion, MinimumContrast: 7, ZoomLayoutValid: profile.ZoomPercent == 200,
	}, nil
}

type packagedCommandRouter struct {
	invocations []desktopcontract.CommandInvocation
}

func (*packagedCommandRouter) Status(context.Context) ([]desktopcontract.CommandStatus, error) {
	return []desktopcontract.CommandStatus{{ID: desktopcontract.CommandOpenProject, State: desktopcontract.CommandAvailable, Generation: "1"}}, nil
}
func (r *packagedCommandRouter) Route(_ context.Context, input desktopcontract.CommandInvocation) (desktopcontract.CommandStatus, error) {
	r.invocations = append(r.invocations, input)
	return desktopcontract.CommandStatus{ID: input.ID, State: desktopcontract.CommandAvailable, Generation: input.StatusGeneration}, nil
}

type packagedCrashRecovery struct{}

func (packagedCrashRecovery) Preserve(context.Context, desktopcontract.CrashContext) (desktopcontract.RecoveryRef, error) {
	return desktopcontract.RecoveryRef{ID: "packaged-recovery"}, nil
}

type packagedErrorSurface struct{}

func (packagedErrorSurface) Present(context.Context, desktopcontract.ErrorSurface) error { return nil }

func TestPackagedDesktopNativeShellMatrix(t *testing.T) {
	for _, platform := range []desktopcontract.DesktopPlatform{desktopcontract.PlatformMacOS, desktopcontract.PlatformWindows, desktopcontract.PlatformLinux} {
		t.Run(string(platform), func(t *testing.T) {
			root := t.TempDir()
			runtime := &packagedDesktopRuntime{
				window:   desktopcontract.WindowState{SchemaVersion: 1, Bounds: desktopcontract.Rectangle{X: 20, Y: 20, Width: 1200, Height: 800}},
				settings: desktopcontract.DesktopSettings{SchemaVersion: 1, Theme: desktopcontract.ThemeSystem, ZoomPercent: 100},
			}
			router := &packagedCommandRouter{}
			composition, err := desktopapp.NewPlatformNativeShell(desktopapp.PlatformNativeShellConfig{
				Platform: platform, StateRoot: root, Runtime: runtime, Commands: router,
				CrashRecovery: packagedCrashRecovery{}, Errors: packagedErrorSurface{},
				Now: func() time.Time { return time.Date(2026, 7, 20, 0, 0, 0, 0, time.UTC) },
			})
			if err != nil {
				t.Fatal(err)
			}
			if result := composition.Shell.Restore(context.Background()); !result.Validate() || result.Outcome != protocolcommon.OutcomeSuccess {
				t.Fatalf("restore=%+v", result)
			}
			profile := desktopcontract.AccessibilityProfile{Platform: platform, ScreenReader: true, KeyboardOnly: true, ReducedMotion: true, ZoomPercent: 200}
			if result := composition.Shell.VerifyAccessibility(context.Background(), profile); result.Outcome != protocolcommon.OutcomeSuccess {
				t.Fatalf("accessibility=%+v", result)
			}
			invocation := desktopcontract.CommandInvocation{ID: desktopcontract.CommandOpenProject, Source: desktopcontract.CommandSourceShortcut, StatusGeneration: "1"}
			if result := composition.Shell.InvokeCommand(context.Background(), invocation); result.Outcome != protocolcommon.OutcomeSuccess || len(router.invocations) != 1 {
				t.Fatalf("command=%+v invocations=%+v", result, router.invocations)
			}
			associated := filepath.Join(root, "associated.layerdraw")
			if err := os.WriteFile(associated, []byte("packaged"), 0o600); err != nil {
				t.Fatal(err)
			}
			if err := composition.Associations.AcceptOSPath(associated); err != nil {
				t.Fatal(err)
			}
			handoff := composition.Shell.NextFileAssociation(context.Background())
			if handoff.Outcome != protocolcommon.OutcomeSuccess || handoff.Value.Token == "" {
				t.Fatalf("association=%+v", handoff)
			}
			if resolved, err := composition.Associations.Resolve(handoff.Value.Token); err != nil || resolved != associated {
				t.Fatalf("resolved=%q err=%v", resolved, err)
			}
			if info, err := os.Stat(filepath.Join(root, "settings-v1.json")); err != nil || info.Mode().Perm()&0o077 != 0 {
				t.Fatalf("private settings info=%v err=%v", info, err)
			}
		})
	}
}
