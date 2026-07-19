// SPDX-License-Identifier: LicenseRef-LayerDraw-1.0

package host

import (
	"fmt"
	"path/filepath"

	searchadapter "github.com/dencyuinc/layerdraw/internal/adapter/search"
	layerruntime "github.com/dencyuinc/layerdraw/internal/runtime"
	"github.com/dencyuinc/layerdraw/internal/runtime/port"
)

type DesktopSearchConfig struct {
	Root                                string
	Engine                              port.SearchEngine
	Ladybug                             searchadapter.LadybugSession
	PlanKey, SearchDocumentKey          []byte
	EmbeddingProfile                    port.EmbeddingProfile
	LocalModelSeed                      []byte
	BackendVersion, PlanProtocolVersion string
	MaxRows, MaxBytes                   int
}
type DesktopSearchComposition struct {
	Surface ConsumerSearchSurface
}

// NewDesktopSearchComposition constructs the exact shared instance injected
// into Wails and MCP Host. It has no framework dependency and no alternate
// consumer-specific capability path.
func NewDesktopSearchComposition(config DesktopSearchConfig) (DesktopSearchComposition, error) {
	if config.Root == "" || !filepath.IsAbs(config.Root) || config.Engine == nil || config.Ladybug == nil {
		return DesktopSearchComposition{}, fmt.Errorf("incomplete Desktop Search composition")
	}
	authorizedEngine, planVerifier, err := searchadapter.BindEnginePlanAuthority(config.Engine, config.PlanKey)
	if err != nil {
		return DesktopSearchComposition{}, err
	}
	documentVerifier, err := searchadapter.NewSearchDocumentBatchVerifier(config.SearchDocumentKey)
	if err != nil {
		return DesktopSearchComposition{}, err
	}
	driver, err := searchadapter.NewLadybugNativeDriver(config.Ladybug)
	if err != nil {
		return DesktopSearchComposition{}, err
	}
	capability := port.QueryAdapterCapability{AdapterID: "layerdraw.ladybug.native", Backend: "ladybug_native", BackendVersion: config.BackendVersion, PlanProtocolVersion: config.PlanProtocolVersion, Primitives: append([]port.SearchPrimitive(nil), port.RequiredSearchPrimitives...), MaxRows: config.MaxRows, MaxBytes: config.MaxBytes}
	executor, err := searchadapter.NewNativeExecutor(capability, driver, planVerifier)
	if err != nil {
		return DesktopSearchComposition{}, err
	}
	indexes, err := searchadapter.NewDurableIndexStore(filepath.Join(config.Root, "search-index"), executor, nil)
	if err != nil {
		return DesktopSearchComposition{}, err
	}
	model, err := searchadapter.NewLocalProjectionModel(config.EmbeddingProfile.Dimensions, config.LocalModelSeed)
	if err != nil {
		return DesktopSearchComposition{}, err
	}
	embedding, err := searchadapter.NewEmbeddingProvider(port.EmbeddingCapability{ProviderID: "layerdraw.local.projection", Available: true, Profiles: []port.EmbeddingProfile{config.EmbeddingProfile}}, map[string]searchadapter.VectorModel{config.EmbeddingProfile.ProfileID: model}, false, documentVerifier)
	if err != nil {
		return DesktopSearchComposition{}, err
	}
	service := layerruntime.NewVerifiedSearchService(authorizedEngine, executor, indexes, embedding, documentVerifier)
	return DesktopSearchComposition{Surface: service}, nil
}
