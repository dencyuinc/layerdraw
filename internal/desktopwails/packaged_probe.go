// SPDX-License-Identifier: LicenseRef-LayerDraw-1.0

package desktopwails

import (
	"context"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"os"
	"path/filepath"

	"github.com/dencyuinc/layerdraw/gen/go/protocolcommon"
	desktopadapter "github.com/dencyuinc/layerdraw/internal/adapter/desktop"
	"github.com/dencyuinc/layerdraw/internal/desktopcontract"
)

type PackagedProbeResult struct {
	SchemaVersion      uint32                                     `json:"schema_version"`
	Platform           desktopcontract.DesktopPlatform            `json:"platform"`
	CapabilityManifest desktopcontract.Manifest                   `json:"capability_manifest"`
	CapabilityStatuses []protocolcommon.RequestedCapabilityStatus `json:"capability_statuses"`
	WailsRuntimeBridge bool                                       `json:"wails_runtime_bridge"`
	SettingsRoundTrip  bool                                       `json:"settings_round_trip"`
	ProjectRoundTrip   bool                                       `json:"project_round_trip"`
	AssociationHandoff desktopcontract.FileAssociationKind        `json:"association_handoff"`
	DOMRoundTrip       bool                                       `json:"dom_round_trip,omitempty"`
	Accessibility      *desktopcontract.AccessibilityReport       `json:"accessibility,omitempty"`
	UIMatrix           []PackagedUIProbeResult                    `json:"ui_matrix,omitempty"`
	Failure            *PackagedUIProbeFailure                    `json:"failure,omitempty"`
}

type PackagedUIProbeFailure struct {
	ID            string                               `json:"id"`
	Accessibility *desktopcontract.AccessibilityReport `json:"accessibility,omitempty"`
}

type PackagedUIProbeResult struct {
	ID            string                               `json:"id"`
	Window        desktopcontract.WindowState          `json:"window"`
	Settings      desktopcontract.DesktopSettings      `json:"settings"`
	Profile       desktopcontract.AccessibilityProfile `json:"profile"`
	Accessibility desktopcontract.AccessibilityReport  `json:"accessibility"`
}

// RunPackagedProbe is executed from the packaged desktop binary on each OS CI
// runner. It verifies the build-tag-selected platform plus the concrete Wails,
// private settings, external opener, and association adapter linkage without
// opening a GUI or invoking an external application.
func RunPackagedProbe(output io.Writer) error {
	if output == nil || !CurrentPlatform().Validate() {
		return errors.New("packaged Desktop probe unavailable")
	}
	result, err := executePackagedProbe()
	if err != nil {
		return err
	}
	return json.NewEncoder(output).Encode(result)
}

func executePackagedProbe() (PackagedProbeResult, error) {
	manifest := desktopcontract.DefaultManifest()
	handshake, err := (nativeCapabilities{}).Negotiate(context.Background(), manifest)
	if err != nil {
		return PackagedProbeResult{}, err
	}
	negotiated := desktopcontract.NegotiateCapabilitiesFor(manifest, handshake, packagedRequiredCapabilities())
	if negotiated.Outcome != protocolcommon.OutcomeSuccess {
		return PackagedProbeResult{}, errors.New("packaged Desktop capability probe failed")
	}
	root, persistent, err := packagedProbeStateRoot()
	if err != nil {
		return PackagedProbeResult{}, err
	}
	if !persistent {
		defer os.RemoveAll(root)
	}
	if err := ensurePackagedProbeRoot(root); err != nil {
		return PackagedProbeResult{}, err
	}
	action := os.Getenv("LAYERDRAW_DESKTOP_PROBE_ACTION")
	if action == "" {
		action = "initialize"
	}
	if action != "initialize" && action != "verify" {
		return PackagedProbeResult{}, errors.New("packaged Desktop probe action is invalid")
	}
	store, err := desktopadapter.NewAtomicSettingsStore(filepath.Join(root, "settings-v1.json"))
	if err != nil {
		return PackagedProbeResult{}, err
	}
	state := desktopcontract.PersistedShellState{
		Settings: desktopcontract.DesktopSettings{SchemaVersion: 1, Theme: desktopcontract.ThemeSystem, ZoomPercent: 100},
		Window:   desktopcontract.WindowState{SchemaVersion: 1, Bounds: desktopcontract.Rectangle{Width: 1280, Height: 800}},
	}
	if action == "initialize" {
		if err := store.Save(context.Background(), state); err != nil {
			return PackagedProbeResult{}, err
		}
	}
	loaded, err := store.Load(context.Background())
	if err != nil || loaded != state {
		return PackagedProbeResult{}, errors.New("packaged Desktop settings probe failed")
	}
	if _, err := desktopadapter.NewSystemExternalOpener(CurrentPlatform()); err != nil {
		return PackagedProbeResult{}, err
	}
	projectRoot := filepath.Join(root, "projects", "upgrade-probe")
	associated := filepath.Join(projectRoot, "document.ldl")
	projectSource := []byte("project upgrade_probe \"Upgrade Probe\" {}\n")
	if action == "initialize" {
		if err := os.MkdirAll(projectRoot, 0o700); err != nil {
			return PackagedProbeResult{}, err
		}
		if err := os.WriteFile(associated, projectSource, 0o600); err != nil {
			return PackagedProbeResult{}, err
		}
	}
	actualProject, err := os.ReadFile(associated)
	if err != nil || string(actualProject) != string(projectSource) {
		return PackagedProbeResult{}, errors.New("packaged Desktop project probe failed")
	}
	broker := desktopadapter.NewAssociationBroker()
	if err := broker.AcceptOSPath(associated); err != nil {
		return PackagedProbeResult{}, err
	}
	handoff, err := broker.Next(context.Background())
	if err != nil {
		return PackagedProbeResult{}, err
	}
	bridge := NewWailsShellBridge()
	var _ desktopadapter.WailsRuntimeBridge = bridge
	return PackagedProbeResult{
		SchemaVersion: 1, Platform: CurrentPlatform(), CapabilityManifest: manifest,
		CapabilityStatuses: negotiated.Value.CapabilityStatuses, WailsRuntimeBridge: true,
		SettingsRoundTrip: true, ProjectRoundTrip: true, AssociationHandoff: handoff.Kind,
	}, nil
}

func ensurePackagedProbeRoot(root string) error {
	info, err := os.Lstat(root)
	if errors.Is(err, os.ErrNotExist) {
		return os.Mkdir(root, 0o700)
	}
	if err != nil {
		return err
	}
	if !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
		return errors.New("packaged Desktop probe state root is unsafe")
	}
	return nil
}

func packagedProbeStateRoot() (string, bool, error) {
	key := os.Getenv("LAYERDRAW_DESKTOP_PROBE_STATE_KEY")
	if key != "" {
		decoded, err := hex.DecodeString(key)
		if err != nil || len(decoded) != 16 {
			return "", false, errors.New("packaged Desktop probe state key is invalid")
		}
		// Re-encode fixed-size decoded bytes so the filesystem component cannot
		// retain caller-controlled separators or alternate representations.
		safeKey := hex.EncodeToString(decoded)
		return filepath.Join(os.TempDir(), "layerdraw-desktop-probe-state-"+safeKey), true, nil
	}
	root, err := os.MkdirTemp("", "layerdraw-desktop-probe-*")
	return root, false, err
}
