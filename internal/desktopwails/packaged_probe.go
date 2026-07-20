// SPDX-License-Identifier: LicenseRef-LayerDraw-1.0

package desktopwails

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"os"
	"path/filepath"

	desktopadapter "github.com/dencyuinc/layerdraw/internal/adapter/desktop"
	"github.com/dencyuinc/layerdraw/internal/desktopcontract"
)

type PackagedProbeResult struct {
	SchemaVersion      uint32                               `json:"schema_version"`
	Platform           desktopcontract.DesktopPlatform      `json:"platform"`
	WailsRuntimeBridge bool                                 `json:"wails_runtime_bridge"`
	SettingsRoundTrip  bool                                 `json:"settings_round_trip"`
	ProjectRoundTrip   bool                                 `json:"project_round_trip"`
	AssociationHandoff desktopcontract.FileAssociationKind  `json:"association_handoff"`
	DOMRoundTrip       bool                                 `json:"dom_round_trip,omitempty"`
	Accessibility      *desktopcontract.AccessibilityReport `json:"accessibility,omitempty"`
}

// RunPackagedProbe is executed from the packaged desktop binary on each OS CI
// runner. It verifies the build-tag-selected platform plus the concrete Wails,
// private settings, external opener, and association adapter linkage without
// opening a GUI or invoking an external application.
func RunPackagedProbe(output io.Writer) error {
	if output == nil || !CurrentPlatform().Validate() {
		return errors.New("packaged Desktop probe unavailable")
	}
	root := os.Getenv("LAYERDRAW_DESKTOP_PROBE_STATE_ROOT")
	cleanup := false
	if root == "" {
		var err error
		root, err = os.MkdirTemp("", "layerdraw-desktop-probe-*")
		if err != nil {
			return err
		}
		cleanup = true
	} else if !filepath.IsAbs(root) || filepath.Clean(root) != root {
		return errors.New("packaged Desktop probe state root is invalid")
	}
	if cleanup {
		defer os.RemoveAll(root)
	}
	if err := os.MkdirAll(root, 0o700); err != nil {
		return err
	}
	action := os.Getenv("LAYERDRAW_DESKTOP_PROBE_ACTION")
	if action == "" {
		action = "initialize"
	}
	if action != "initialize" && action != "verify" {
		return errors.New("packaged Desktop probe action is invalid")
	}
	store, err := desktopadapter.NewAtomicSettingsStore(filepath.Join(root, "settings-v1.json"))
	if err != nil {
		return err
	}
	state := desktopcontract.PersistedShellState{
		Settings: desktopcontract.DesktopSettings{SchemaVersion: 1, Theme: desktopcontract.ThemeSystem, ZoomPercent: 100},
		Window:   desktopcontract.WindowState{SchemaVersion: 1, Bounds: desktopcontract.Rectangle{Width: 1280, Height: 800}},
	}
	if action == "initialize" {
		if err := store.Save(context.Background(), state); err != nil {
			return err
		}
	}
	loaded, err := store.Load(context.Background())
	if err != nil || loaded != state {
		return errors.New("packaged Desktop settings probe failed")
	}
	if _, err := desktopadapter.NewSystemExternalOpener(CurrentPlatform()); err != nil {
		return err
	}
	projectRoot := filepath.Join(root, "projects", "upgrade-probe")
	associated := filepath.Join(projectRoot, "document.ldl")
	projectSource := []byte("project upgrade_probe \"Upgrade Probe\" {}\n")
	if action == "initialize" {
		if err := os.MkdirAll(projectRoot, 0o700); err != nil {
			return err
		}
		if err := os.WriteFile(associated, projectSource, 0o600); err != nil {
			return err
		}
	}
	actualProject, err := os.ReadFile(associated)
	if err != nil || string(actualProject) != string(projectSource) {
		return errors.New("packaged Desktop project probe failed")
	}
	broker := desktopadapter.NewAssociationBroker()
	if err := broker.AcceptOSPath(associated); err != nil {
		return err
	}
	handoff, err := broker.Next(context.Background())
	if err != nil {
		return err
	}
	bridge := NewWailsShellBridge()
	var _ desktopadapter.WailsRuntimeBridge = bridge
	return json.NewEncoder(output).Encode(PackagedProbeResult{
		SchemaVersion: 1, Platform: CurrentPlatform(), WailsRuntimeBridge: true,
		SettingsRoundTrip: true, ProjectRoundTrip: true, AssociationHandoff: handoff.Kind,
	})
}
