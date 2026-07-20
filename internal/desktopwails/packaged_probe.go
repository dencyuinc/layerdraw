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
	root, err := os.MkdirTemp("", "layerdraw-desktop-probe-*")
	if err != nil {
		return err
	}
	defer os.RemoveAll(root)
	store, err := desktopadapter.NewAtomicSettingsStore(filepath.Join(root, "settings-v1.json"))
	if err != nil {
		return err
	}
	state := desktopcontract.PersistedShellState{
		Settings: desktopcontract.DesktopSettings{SchemaVersion: 1, Theme: desktopcontract.ThemeSystem, ZoomPercent: 100},
		Window:   desktopcontract.WindowState{SchemaVersion: 1, Bounds: desktopcontract.Rectangle{Width: 1280, Height: 800}},
	}
	if err := store.Save(context.Background(), state); err != nil {
		return err
	}
	loaded, err := store.Load(context.Background())
	if err != nil || loaded != state {
		return errors.New("packaged Desktop settings probe failed")
	}
	if _, err := desktopadapter.NewSystemExternalOpener(CurrentPlatform()); err != nil {
		return err
	}
	associated := filepath.Join(root, "probe.ldl")
	if err := os.WriteFile(associated, []byte("project probe {}\n"), 0o600); err != nil {
		return err
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
	var runtimeBridge desktopadapter.WailsRuntimeBridge = bridge
	return json.NewEncoder(output).Encode(PackagedProbeResult{
		SchemaVersion: 1, Platform: CurrentPlatform(), WailsRuntimeBridge: runtimeBridge != nil,
		SettingsRoundTrip: true, AssociationHandoff: handoff.Kind,
	})
}
