// SPDX-License-Identifier: LicenseRef-LayerDraw-1.0
//go:build ladybug_native

package host

import (
	"fmt"

	searchadapter "github.com/dencyuinc/layerdraw/internal/adapter/search"
)

// NativeDesktopSearchComposition owns the production Ladybug session used by
// the one shared Wails/MCP Desktop search surface.
type NativeDesktopSearchComposition struct {
	DesktopSearchComposition
	ladybug *searchadapter.GoLadybugSession
}

// OpenDesktopNativeSearchComposition wires the actual go-ladybug v0.17 native
// binding. Callers cannot substitute a non-production Ladybug session or claim
// a backend version that was not read from the opened database.
func OpenDesktopNativeSearchComposition(config DesktopSearchConfig, databasePath string) (*NativeDesktopSearchComposition, error) {
	if config.Ladybug != nil || config.BackendVersion != "" {
		return nil, fmt.Errorf("native Desktop composition owns Ladybug configuration")
	}
	ladybug, err := searchadapter.OpenGoLadybugSession(databasePath)
	if err != nil {
		return nil, err
	}
	backendVersion, err := ladybug.BackendVersion()
	if err != nil {
		ladybug.Close()
		return nil, err
	}
	if backendVersion != searchadapter.GoLadybugBackendVersion {
		ladybug.Close()
		return nil, fmt.Errorf("unsupported Ladybug backend version %q", backendVersion)
	}
	config.Ladybug = ladybug
	config.BackendVersion = backendVersion
	composition, err := NewDesktopSearchComposition(config)
	if err != nil {
		ladybug.Close()
		return nil, err
	}
	return &NativeDesktopSearchComposition{DesktopSearchComposition: composition, ladybug: ladybug}, nil
}

func (c *NativeDesktopSearchComposition) Close() {
	if c != nil && c.ladybug != nil {
		c.ladybug.Close()
		c.ladybug = nil
	}
}
