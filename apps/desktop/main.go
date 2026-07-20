// SPDX-License-Identifier: LicenseRef-LayerDraw-1.0

package main

import (
	"embed"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"

	"github.com/dencyuinc/layerdraw/internal/desktopapp"
	"github.com/dencyuinc/layerdraw/internal/desktopwails"
)

//go:embed frontend/dist
var frontend embed.FS

var userConfigDir = os.UserConfigDir
var runPackagedProbe = func(output io.Writer) error { return desktopwails.RunPackagedProbe(output) }
var runPackagedConformance = desktopwails.RunPackagedConformance
var startDesktop = func(config desktopapp.Config, assets fs.FS) error {
	return desktopwails.Run(config, assets, nil)
}

func main() {
	if err := run(); err != nil {
		_, _ = fmt.Fprintln(os.Stderr, "LayerDraw Desktop failed to start")
		os.Exit(1)
	}
}

func run() error {
	if len(os.Args) == 2 && os.Args[1] == "--packaged-probe" {
		return runPackagedProbe(os.Stdout)
	}
	if len(os.Args) == 3 && os.Args[1] == "--packaged-conformance" {
		output := os.Args[2]
		if !filepath.IsAbs(output) || filepath.Clean(output) != output {
			return errors.New("packaged Desktop conformance output is invalid")
		}
		return runPackagedConformance(output)
	}
	if len(os.Args) == 3 && os.Args[1] == "--packaged-ui-probe" {
		output, err := filepath.Abs(os.Args[2])
		if err != nil || filepath.Clean(output) != output {
			return errors.New("packaged Desktop UI probe output is invalid")
		}
		if err := os.Setenv("LAYERDRAW_DESKTOP_UI_PROBE_OUTPUT", output); err != nil {
			return err
		}
	}
	configRoot, err := userConfigDir()
	if err != nil {
		return err
	}
	dataRoot := filepath.Join(configRoot, "LayerDraw")
	if err := os.MkdirAll(dataRoot, 0o700); err != nil {
		return err
	}
	assets, err := fs.Sub(frontend, "frontend/dist")
	if err != nil {
		return err
	}
	shared, err := desktopwails.NewSharedConfig(dataRoot)
	if err != nil {
		return err
	}
	return startDesktop(shared, assets)
}
