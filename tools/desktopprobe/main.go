// SPDX-License-Identifier: LicenseRef-LayerDraw-1.0

package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"os"

	"github.com/dencyuinc/layerdraw/internal/desktopcontract"
	"github.com/dencyuinc/layerdraw/internal/desktopwails"
)

func main() {
	input := flag.String("input", "", "path to packaged Desktop probe JSON")
	requireDOM := flag.Bool("require-dom", false, "require a completed Wails DOM accessibility round trip")
	flag.Parse()
	if flag.NArg() != 1 || flag.Arg(0) != "verify" || *input == "" {
		fail("usage: desktopprobe -input <path> verify")
	}
	file, err := os.Open(*input)
	if err != nil {
		fail("Desktop probe output unavailable")
	}
	defer file.Close()
	var value desktopwails.PackagedProbeResult
	decoder := json.NewDecoder(file)
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&value); err != nil || value.SchemaVersion != 1 || !value.Platform.Validate() ||
		!value.WailsRuntimeBridge || !value.SettingsRoundTrip || !value.ProjectRoundTrip || value.AssociationHandoff != desktopcontract.FileAssociationLDL {
		fail("Desktop probe output invalid")
	}
	if *requireDOM && (!value.DOMRoundTrip || value.Accessibility == nil || !value.Accessibility.LabelsComplete ||
		!value.Accessibility.FocusOrderValid || !value.Accessibility.KeyboardWorkflowValid ||
		!value.Accessibility.ScreenReaderSemantics || value.Accessibility.MinimumContrast < 4.5 || !value.Accessibility.ZoomLayoutValid || !validUIMatrix(value)) {
		fail("Desktop DOM probe output invalid")
	}
}

func validUIMatrix(value desktopwails.PackagedProbeResult) bool {
	required := map[string]bool{
		"standard-light-2d": false, "minimum-light-2.5d": false,
		"standard-light-zoom200-2d": false, "large-dark-2.5d": false,
	}
	if len(value.UIMatrix) != len(required) {
		return false
	}
	for _, entry := range value.UIMatrix {
		if _, ok := required[entry.ID]; !ok || required[entry.ID] || entry.Profile.ProbeID != entry.ID ||
			entry.Profile.Platform != value.Platform || entry.Profile.WindowWidth != uint16(entry.Window.Bounds.Width) ||
			entry.Profile.WindowHeight != uint16(entry.Window.Bounds.Height) || entry.Profile.ZoomPercent != entry.Settings.ZoomPercent ||
			!entry.Window.Validate() || !entry.Settings.Validate() {
			return false
		}
		report := entry.Accessibility
		if !report.LabelsComplete || !report.ScreenReaderSemantics || !report.FocusOrderValid || !report.KeyboardWorkflowValid ||
			(entry.Profile.ReducedMotion && !report.ReducedMotionHonored) || report.MinimumContrast < 4.5 || report.MinimumContrast > 21 ||
			!report.ZoomLayoutValid || report.ViewportWidth == 0 || report.ViewportHeight == 0 || report.ViewerMode != entry.Profile.ViewerMode ||
			report.ViewerItemCount == 0 || report.ViewerRelationCount == 0 || !report.ViewerKeyboardSelect ||
			(entry.Profile.ViewerMode == "2d" && report.RendererBackend != "svg") ||
			(entry.Profile.ViewerMode == "2.5d" && (report.RendererBackend != "three.js" || !report.WebGLVerified || report.ViewerCrossLayerCount == 0)) {
			return false
		}
		required[entry.ID] = true
	}
	for _, found := range required {
		if !found {
			return false
		}
	}
	return true
}

func fail(message string) {
	_, _ = fmt.Fprintln(os.Stderr, message)
	os.Exit(1)
}
