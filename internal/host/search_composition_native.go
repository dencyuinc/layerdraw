// SPDX-License-Identifier: LicenseRef-LayerDraw-1.0
//go:build ladybug_native

package host

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"

	"github.com/dencyuinc/layerdraw/gen/go/protocolcommon"
	enginesearch "github.com/dencyuinc/layerdraw/internal/adapter/enginesearch"
	searchadapter "github.com/dencyuinc/layerdraw/internal/adapter/search"
	engineendpoint "github.com/dencyuinc/layerdraw/internal/engine/endpoint"
	"github.com/dencyuinc/layerdraw/internal/localdocument"
	layerruntime "github.com/dencyuinc/layerdraw/internal/runtime"
	"github.com/dencyuinc/layerdraw/internal/runtime/port"
)

// NativeDesktopSearchComposition owns the production Ladybug session used by
// the one shared Wails/MCP Desktop search surface.
type NativeDesktopSearchComposition struct {
	DesktopSearchComposition
	ladybug      *searchadapter.GoLadybugSession
	engineSearch *enginesearch.Adapter
	projection   *enginesearch.LocalAccessProjection
	localHost    *localdocument.Host
	profile      port.EmbeddingProfile
}

type DesktopNativeConfig struct {
	LocalConfig
	DatabasePath, FTSExtensionPath         string
	VectorExtensionPath, AlgoExtensionPath string
	PlanKey, SearchDocumentKey             []byte
	EmbeddingProfile                       port.EmbeddingProfile
	LocalModelSeed                         []byte
	MaxRows, MaxBytes                      int
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
	projection := enginesearch.NewSessionAccessProjection()
	engineSearch := enginesearch.New(engineFacade.Compiler(), projection)
	search, err := openDesktopNativeSearchComposition(DesktopSearchConfig{Root: config.Root, Engine: engineSearch, DocumentProducer: engineSearch, PlanKey: config.PlanKey, SearchDocumentKey: config.SearchDocumentKey, EmbeddingProfile: config.EmbeddingProfile, LocalModelSeed: config.LocalModelSeed, PlanProtocolVersion: "v1", MaxRows: config.MaxRows, MaxBytes: config.MaxBytes, Primitives: append([]port.SearchPrimitive(nil), port.RequiredSearchPrimitives...)}, config.DatabasePath, []string{config.FTSExtensionPath, config.VectorExtensionPath, config.AlgoExtensionPath}, true)
	if err != nil {
		_ = localHost.Shutdown(context.Background())
		return nil, nil, nil, err
	}
	search.engineSearch = engineSearch
	search.projection, search.localHost, search.profile = projection, localHost, config.EmbeddingProfile
	endpoint, err := New(Config{LocalHost: localHost, Engine: engineFacade, Search: search.Surface, SearchLifecycle: search})
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

func (c *NativeDesktopSearchComposition) RefreshSearchIndex(ctx context.Context, session *localdocument.Session) error {
	if c == nil || c.localHost == nil || c.engineSearch == nil || c.projection == nil {
		return fmt.Errorf("native Search lifecycle unavailable")
	}
	encodedInput, snapshot, accessDigest, err := c.localHost.SearchBinding(session)
	if err != nil {
		return err
	}
	input, err := engineendpoint.SearchOpenInputFromEncoded(encodedInput)
	if err != nil {
		return err
	}
	if err := c.projection.BindSession(snapshot, accessDigest); err != nil {
		return err
	}
	corpus, err := c.engineSearch.OpenLocalCorpus(ctx, input, snapshot, accessDigest)
	if err != nil {
		return err
	}
	profile := port.SearchProfile{ProfileID: "layerdraw.desktop.default", LexicalCandidateLimit: 256, SemanticCandidateLimit: 256, MaxHits: 100, RRFK: 60, LexicalWeight: 1, SemanticWeight: 1, SnippetMaxBytes: 256}
	encodedProfile, _ := json.Marshal(profile)
	digest := sha256.Sum256(encodedProfile)
	profile.SpecificationDigest = "sha256:" + hex.EncodeToString(digest[:])
	identity := port.SearchIndexIdentity{DocumentSnapshotRef: snapshot, SearchProfileID: profile.ProfileID, SearchProfileDigest: profile.SpecificationDigest, EmbeddingProfileID: c.profile.ProfileID, EmbeddingProfileDigest: c.profile.ModelDigest, AccessProjectionDigest: accessDigest, LadybugBackendVersion: searchadapter.GoLadybugBackendVersion, IndexSchemaVersion: "1"}
	_, err = c.RebuildIndex(ctx, layerruntime.SearchIndexBuildRequest{Snapshot: snapshot, AccessProjectionDigest: accessDigest, SearchProfile: profile, EmbeddingProfile: &c.profile, IndexIdentity: identity, EngineRequest: []byte(`{"kind":"build_search_index"}`)}, port.SearchDocumentBatchRequest{Snapshot: snapshot, AccessProjectionDigest: accessDigest, EmbeddingProfileDigest: c.profile.ModelDigest, Corpus: corpus})
	return err
}

func (c *NativeDesktopSearchComposition) CorpusEngine() *enginesearch.Adapter {
	if c == nil {
		return nil
	}
	return c.engineSearch
}

func (c *NativeDesktopSearchComposition) BindSearchAuthority(snapshot port.DocumentSnapshotRef, digest string) error {
	if c == nil || c.projection == nil {
		return fmt.Errorf("native Search projection unavailable")
	}
	return c.projection.BindSession(snapshot, digest)
}

// OpenDesktopNativeSearchComposition wires the actual go-ladybug v0.17 native
// binding. Callers cannot substitute a non-production Ladybug session or claim
// a backend version that was not read from the opened database.
func OpenDesktopNativeSearchComposition(config DesktopSearchConfig, databasePath, ftsExtensionPath string) (*NativeDesktopSearchComposition, error) {
	return openDesktopNativeSearchComposition(config, databasePath, []string{ftsExtensionPath}, false)
}

func openDesktopNativeSearchComposition(config DesktopSearchConfig, databasePath string, extensionPaths []string, requireOfficialProfile bool) (*NativeDesktopSearchComposition, error) {
	if config.Ladybug != nil || config.BackendVersion != "" {
		return nil, fmt.Errorf("native Desktop composition owns Ladybug configuration")
	}
	for _, extensionPath := range extensionPaths {
		if extensionPath == "" {
			return nil, fmt.Errorf("native Desktop composition requires bundled extensions")
		}
	}
	if requireOfficialProfile && len(extensionPaths) != 3 {
		return nil, fmt.Errorf("native Desktop composition requires FTS, VECTOR, and ALGO extensions")
	}
	ladybug, err := searchadapter.OpenGoLadybugSessionWithExtensions(databasePath, extensionPaths)
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
