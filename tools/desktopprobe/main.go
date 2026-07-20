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
		!value.Accessibility.ReducedMotionHonored || value.Accessibility.MinimumContrast < 4.5 || !value.Accessibility.ZoomLayoutValid) {
		fail("Desktop DOM probe output invalid")
	}
}

func fail(message string) {
	_, _ = fmt.Fprintln(os.Stderr, message)
	os.Exit(1)
}
