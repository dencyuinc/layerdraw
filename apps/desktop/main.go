// SPDX-License-Identifier: LicenseRef-LayerDraw-1.0

package main

import (
	"embed"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"

	"github.com/dencyuinc/layerdraw/internal/desktopapp"
	"github.com/dencyuinc/layerdraw/internal/desktopwails"
)

//go:embed frontend/dist
var frontend embed.FS

var userConfigDir = os.UserConfigDir
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
