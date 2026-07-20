// SPDX-License-Identifier: LicenseRef-LayerDraw-1.0

package host

import (
	"context"
	"fmt"
	"path/filepath"

	searchadapter "github.com/dencyuinc/layerdraw/internal/adapter/search"
	layerruntime "github.com/dencyuinc/layerdraw/internal/runtime"
	"github.com/dencyuinc/layerdraw/internal/runtime/port"
)

type DesktopSearchConfig struct {
	Root                                string
	Engine                              port.SearchEngine
	DocumentProducer                    port.SearchDocumentBatchProducer
	Ladybug                             searchadapter.LadybugSession
	PlanKey, SearchDocumentKey          []byte
	EmbeddingProfile                    port.EmbeddingProfile
	LocalModelSeed                      []byte
	BackendVersion, PlanProtocolVersion string
	MaxRows, MaxBytes                   int
	Primitives                          []port.SearchPrimitive
}
type DesktopSearchComposition struct {
	Surface   ConsumerSearchSurface
	service   *layerruntime.SearchService
	documents port.SearchDocumentBatchProducer
}

// NewDesktopSearchComposition constructs the exact shared instance injected
// into Wails and MCP Host. It has no framework dependency and no alternate
// consumer-specific capability path.
func NewDesktopSearchComposition(config DesktopSearchConfig) (DesktopSearchComposition, error) {
	if config.Root == "" || !filepath.IsAbs(config.Root) || config.Engine == nil || config.DocumentProducer == nil || config.Ladybug == nil {
		return DesktopSearchComposition{}, fmt.Errorf("incomplete Desktop Search composition")
	}
	authorizedEngine, planVerifier, err := searchadapter.BindEnginePlanAuthority(config.Engine, config.PlanKey)
	if err != nil {
		return DesktopSearchComposition{}, err
	}
	documentProducer, documentVerifier, err := searchadapter.BindEngineSearchDocumentAuthority(config.DocumentProducer, config.SearchDocumentKey)
	if err != nil {
		return DesktopSearchComposition{}, err
	}
	driver, err := searchadapter.NewLadybugNativeDriver(config.Ladybug)
	if err != nil {
		return DesktopSearchComposition{}, err
	}
	capability := port.QueryAdapterCapability{AdapterID: "layerdraw.ladybug.native", Backend: "ladybug_native", BackendVersion: config.BackendVersion, PlanProtocolVersion: config.PlanProtocolVersion, Primitives: append([]port.SearchPrimitive(nil), config.Primitives...), MaxRows: config.MaxRows, MaxBytes: config.MaxBytes}
	executor, err := searchadapter.NewNativeExecutor(capability, driver, planVerifier)
	if err != nil {
		return DesktopSearchComposition{}, err
	}
	indexes, err := searchadapter.NewDurableIndexStore(filepath.Join(config.Root, "search-index"), executor, nil)
	if err != nil {
		return DesktopSearchComposition{}, err
	}
	var embedding port.EmbeddingProvider
	if config.EmbeddingProfile.ProfileID != "" {
		model, modelErr := searchadapter.NewLocalProjectionModel(config.EmbeddingProfile.Dimensions, config.LocalModelSeed)
		if modelErr != nil {
			return DesktopSearchComposition{}, modelErr
		}
		embedding, err = searchadapter.NewEmbeddingProvider(port.EmbeddingCapability{ProviderID: "layerdraw.local.projection", Available: true, Profiles: []port.EmbeddingProfile{config.EmbeddingProfile}}, map[string]searchadapter.VectorModel{config.EmbeddingProfile.ProfileID: model}, false, documentVerifier)
		if err != nil {
			return DesktopSearchComposition{}, err
		}
	} else if config.EmbeddingProfile != (port.EmbeddingProfile{}) || len(config.LocalModelSeed) != 0 {
		return DesktopSearchComposition{}, fmt.Errorf("incomplete Desktop embedding configuration")
	}
	service := layerruntime.NewVerifiedSearchServiceWithCursorAuthority(authorizedEngine, executor, indexes, embedding, documentVerifier, config.PlanKey)
	return DesktopSearchComposition{Surface: service, service: service, documents: documentProducer}, nil
}

// RebuildIndex produces the signed SearchDocumentBatch through the bound
// Engine/Access authority before Runtime sees it. No consumer can submit or
// sign an arbitrary batch directly.
func (c DesktopSearchComposition) RebuildIndex(ctx context.Context, input layerruntime.SearchIndexBuildRequest, documents port.SearchDocumentBatchRequest) (port.SearchIndexStatus, error) {
	if c.service == nil || c.documents == nil || documents.Snapshot != input.Snapshot || documents.AccessProjectionDigest != input.AccessProjectionDigest || (input.EmbeddingProfile == nil && documents.EmbeddingProfileDigest != "") || (input.EmbeddingProfile != nil && documents.EmbeddingProfileDigest != input.EmbeddingProfile.ModelDigest) {
		return port.SearchIndexStatus{}, fmt.Errorf("invalid Desktop Search document authority input")
	}
	batch, err := c.documents.ProduceSearchDocumentBatch(ctx, documents)
	if err != nil {
		return port.SearchIndexStatus{}, err
	}
	input.Batch = batch
	return c.service.RebuildIndex(ctx, input)
}
