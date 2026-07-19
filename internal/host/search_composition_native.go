// SPDX-License-Identifier: LicenseRef-LayerDraw-1.0
//go:build ladybug_native

package host

import (
	"context"
	"fmt"

	"github.com/dencyuinc/layerdraw/gen/go/protocolcommon"
	enginesearch "github.com/dencyuinc/layerdraw/internal/adapter/enginesearch"
	searchadapter "github.com/dencyuinc/layerdraw/internal/adapter/search"
	engineendpoint "github.com/dencyuinc/layerdraw/internal/engine/endpoint"
	"github.com/dencyuinc/layerdraw/internal/localdocument"
	"github.com/dencyuinc/layerdraw/internal/runtime/port"
)

// NativeDesktopSearchComposition owns the production Ladybug session used by
// the one shared Wails/MCP Desktop search surface.
type NativeDesktopSearchComposition struct {
	DesktopSearchComposition
	ladybug *searchadapter.GoLadybugSession
}

type DesktopNativeConfig struct {
	LocalConfig
	DatabasePath, FTSExtensionPath string
	PlanKey, SearchDocumentKey     []byte
	EmbeddingProfile               port.EmbeddingProfile
	LocalModelSeed                 []byte
	MaxRows, MaxBytes              int
}

// OpenDesktopNativeEndpoint is the production in-process composition root used
// by the Wails Desktop backend and the native host transport. Both consumers
// receive the same Endpoint and Search surface; neither can substitute a stub.
func OpenDesktopNativeEndpoint(config DesktopNativeConfig) (*Endpoint, *NativeDesktopSearchComposition, func(context.Context) error, error) {
	localHost, err := localdocument.New(localdocument.Config{Root: config.Root, ReleaseVersion: protocolcommon.ReleaseVersion(config.ReleaseVersion), EndpointInstanceID: protocolcommon.EndpointInstanceID(config.EndpointInstanceID), ReleaseManifestDigest: protocolcommon.Digest(config.ReleaseManifestDigest)})
	if err != nil {
		return nil, nil, nil, err
	}
	engineFacade, err := engineendpoint.NewHostEngineFacade(config.ReleaseVersion, config.SourceRevision, config.ReleaseManifestDigest, config.EndpointInstanceID, config.TransportID)
	if err != nil {
		_ = localHost.Shutdown(context.Background())
		return nil, nil, nil, err
	}
	engineSearch := enginesearch.New(engineFacade.Compiler())
	search, err := OpenDesktopNativeSearchComposition(DesktopSearchConfig{Root: config.Root, Engine: engineSearch, DocumentProducer: engineSearch, PlanKey: config.PlanKey, SearchDocumentKey: config.SearchDocumentKey, EmbeddingProfile: config.EmbeddingProfile, LocalModelSeed: config.LocalModelSeed, PlanProtocolVersion: "v1", MaxRows: config.MaxRows, MaxBytes: config.MaxBytes}, config.DatabasePath, config.FTSExtensionPath)
	if err != nil {
		_ = localHost.Shutdown(context.Background())
		return nil, nil, nil, err
	}
	endpoint, err := New(Config{LocalHost: localHost, Engine: engineFacade, Search: search.Surface})
	if err != nil {
		search.Close()
		_ = localHost.Shutdown(context.Background())
		return nil, nil, nil, err
	}
	shutdown := func(ctx context.Context) error {
		search.Close()
		return localHost.Shutdown(ctx)
	}
	return endpoint, search, shutdown, nil
}

// OpenDesktopNativeSearchComposition wires the actual go-ladybug v0.17 native
// binding. Callers cannot substitute a non-production Ladybug session or claim
// a backend version that was not read from the opened database.
func OpenDesktopNativeSearchComposition(config DesktopSearchConfig, databasePath, ftsExtensionPath string) (*NativeDesktopSearchComposition, error) {
	if config.Ladybug != nil || config.BackendVersion != "" {
		return nil, fmt.Errorf("native Desktop composition owns Ladybug configuration")
	}
	if ftsExtensionPath == "" {
		return nil, fmt.Errorf("native Desktop composition requires a bundled FTS extension")
	}
	ladybug, err := searchadapter.OpenGoLadybugSessionWithFTS(databasePath, ftsExtensionPath)
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
