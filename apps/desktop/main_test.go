// SPDX-License-Identifier: LicenseRef-LayerDraw-1.0

package main

import (
	"errors"
	"io/fs"
	"path/filepath"
	"testing"

	"github.com/dencyuinc/layerdraw/internal/desktopapp"
)

func TestRunBuildsPackagedDesktopComposition(t *testing.T) {
	root := t.TempDir()
	originalDir, originalStart := userConfigDir, startDesktop
	t.Cleanup(func() { userConfigDir, startDesktop = originalDir, originalStart })
	userConfigDir = func() (string, error) { return root, nil }
	called := false
	startDesktop = func(config desktopapp.Config, assets fs.FS) error {
		called = true
		if config.Root != filepath.Join(root, "LayerDraw") || assets == nil {
			t.Fatalf("composition: root=%q assets=%v", config.Root, assets)
		}
		return nil
	}
	if err := run(); err != nil || !called {
		t.Fatalf("run: called=%v err=%v", called, err)
	}
}

func TestRunStopsBeforeCompositionWhenConfigRootFails(t *testing.T) {
	original := userConfigDir
	t.Cleanup(func() { userConfigDir = original })
	userConfigDir = func() (string, error) { return "", errors.New("unavailable") }
	if err := run(); err == nil {
		t.Fatal("config-root failure was ignored")
	}
}
