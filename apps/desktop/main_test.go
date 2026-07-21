// SPDX-License-Identifier: LicenseRef-LayerDraw-1.0

package main

import (
	"errors"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"testing"

	"github.com/dencyuinc/layerdraw/internal/desktopapp"
)

func TestPackagedProbeRunsFromDesktopExecutable(t *testing.T) {
	originalArgs := os.Args
	originalProbe := runPackagedProbe
	t.Cleanup(func() { os.Args, runPackagedProbe = originalArgs, originalProbe })
	os.Args = []string{"LayerDraw", "--packaged-probe"}
	called := false
	runPackagedProbe = func(io.Writer) error { called = true; return nil }
	if err := run(); err != nil || !called {
		t.Fatalf("probe dispatch called=%v err=%v", called, err)
	}
}

func TestPackagedConformanceRunsFromDesktopExecutable(t *testing.T) {
	originalArgs := os.Args
	originalConformance := runPackagedConformance
	t.Cleanup(func() { os.Args, runPackagedConformance = originalArgs, originalConformance })
	output := filepath.Join(t.TempDir(), "conformance.json")
	os.Args = []string{"LayerDraw", "--packaged-conformance", output}
	called := false
	runPackagedConformance = func(got string) error {
		called = true
		if got != output {
			t.Fatalf("output = %q, want %q", got, output)
		}
		return nil
	}
	if err := run(); err != nil || !called {
		t.Fatalf("conformance dispatch called=%v err=%v", called, err)
	}
}

func TestPackagedConformanceRejectsRelativeOutput(t *testing.T) {
	originalArgs := os.Args
	originalConformance := runPackagedConformance
	t.Cleanup(func() { os.Args, runPackagedConformance = originalArgs, originalConformance })
	os.Args = []string{"LayerDraw", "--packaged-conformance", "relative.json"}
	runPackagedConformance = func(string) error {
		t.Fatal("invalid output reached conformance runner")
		return nil
	}
	if err := run(); err == nil {
		t.Fatal("relative conformance output was accepted")
	}
}

func TestPackagedConformanceScenarioRunsInIsolatedDesktopProcess(t *testing.T) {
	originalArgs := os.Args
	originalScenario := runPackagedConformanceScenario
	t.Cleanup(func() { os.Args, runPackagedConformanceScenario = originalArgs, originalScenario })
	os.Args = []string{"LayerDraw", "--packaged-conformance-scenario", "cold_start"}
	called := false
	runPackagedConformanceScenario = func(name string, output io.Writer) error {
		called = name == "cold_start" && output != nil
		return nil
	}
	if err := run(); err != nil || !called {
		t.Fatalf("isolated scenario dispatch called=%v err=%v", called, err)
	}
}

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
