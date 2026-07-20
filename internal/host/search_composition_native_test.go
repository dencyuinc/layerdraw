// SPDX-License-Identifier: LicenseRef-LayerDraw-1.0
//go:build ladybug_native

package host

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/dencyuinc/layerdraw/internal/localdocument"
	"github.com/dencyuinc/layerdraw/internal/runtime/port"
)

func TestOpenDesktopNativeEndpointWiresProductionSearch(t *testing.T) {
	ftsExtensionPath := os.Getenv("LAYERDRAW_LADYBUG_FTS_EXTENSION")
	if !filepath.IsAbs(ftsExtensionPath) {
		t.Fatal("LAYERDRAW_LADYBUG_FTS_EXTENSION must be an absolute verified path")
	}
	root := t.TempDir()
	databasePath := filepath.Join(root, "desktop-search.lbug")
	endpoint, search, shutdown, err := OpenDesktopNativeEndpoint(DesktopNativeConfig{
		LocalConfig: LocalConfig{
			Root: root, ReleaseVersion: "0.0.0", SourceRevision: "unknown",
			ReleaseManifestDigest: "sha256:" + strings.Repeat("0", 64),
			EndpointInstanceID:    "desktop-native-test", TransportID: "in_process",
		},
		DatabasePath: databasePath, FTSExtensionPath: ftsExtensionPath,
		VectorExtensionPath: filepath.Join(filepath.Dir(ftsExtensionPath), "libvector.lbug_extension"), AlgoExtensionPath: filepath.Join(filepath.Dir(ftsExtensionPath), "libalgo.lbug_extension"),
		PlanKey: []byte("01234567890123456789012345678901"), SearchDocumentKey: []byte("abcdefghijklmnopqrstuvwxyzABCDEF"),
		LocalModelSeed:   []byte("0123456789012345"),
		EmbeddingProfile: port.EmbeddingProfile{ProfileID: "local", ModelID: "projection", ModelVersion: "1", ModelDigest: "sha256:model", Dimensions: 16, Normalization: "unit", MaxInputBytes: 1024},
		MaxRows:          100, MaxBytes: 4096,
	})
	if err != nil {
		t.Fatal(err)
	}
	defer shutdown(context.Background())
	if !endpoint.Supports(OperationSearch) || !endpoint.Supports(OperationExecuteQuery) || !endpoint.Supports(OperationAnalyzeGraph) {
		t.Fatal("production endpoint does not expose the official native primitive profile")
	}
	project := t.TempDir()
	if err := os.WriteFile(filepath.Join(project, "document.ldl"), []byte(`project p "P" {}
layers {
  app "App" @1
}
entity_type service "Service" {
  representation shape rect
}
entities service @app {
  api "API"
}
`), 0o600); err != nil {
		t.Fatal(err)
	}
	opened, err := endpoint.host.OpenProject(context.Background(), localdocument.OpenProjectInput{Root: project, EntryPath: "document.ldl"})
	if err != nil {
		t.Fatal(err)
	}
	if err := search.RefreshSearchIndex(context.Background(), opened.Session); err != nil {
		t.Fatalf("native lifecycle refresh: %v", err)
	}
	active, err := filepath.Glob(filepath.Join(root, "search-index", "*.active.json"))
	if err != nil || len(active) != 1 || endpoint.searchLifecycle != search {
		t.Fatalf("active=%v lifecycle_wired=%t err=%v", active, endpoint.searchLifecycle == search, err)
	}
}

func TestDesktopNativeSearchCompositionUsesProductionLadybugBinding(t *testing.T) {
	ftsExtensionPath := os.Getenv("LAYERDRAW_LADYBUG_FTS_EXTENSION")
	if !filepath.IsAbs(ftsExtensionPath) {
		t.Fatal("LAYERDRAW_LADYBUG_FTS_EXTENSION must be an absolute verified path")
	}
	profile := port.EmbeddingProfile{ProfileID: "local", ModelID: "projection", ModelVersion: "1", ModelDigest: "sha256:model", Dimensions: 16, Normalization: "unit", MaxInputBytes: 1024}
	composition, err := OpenDesktopNativeSearchComposition(DesktopSearchConfig{
		Root:                t.TempDir(),
		Engine:              compositionEngine{},
		DocumentProducer:    compositionEngine{},
		PlanKey:             []byte("01234567890123456789012345678901"),
		SearchDocumentKey:   []byte("abcdefghijklmnopqrstuvwxyzABCDEF"),
		EmbeddingProfile:    profile,
		LocalModelSeed:      []byte("0123456789012345"),
		PlanProtocolVersion: "v1",
		MaxRows:             100,
		MaxBytes:            4096,
		Primitives:          append([]port.SearchPrimitive(nil), port.RequiredSearchPrimitives...),
	}, filepath.Join(t.TempDir(), "desktop-search.lbug"), ftsExtensionPath)
	if err != nil {
		t.Fatal(err)
	}
	defer composition.Close()
	manifest, err := composition.Surface.Capabilities(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if !manifest.SearchAvailable || !manifest.QueryAvailable || !manifest.AnalysisAvailable {
		t.Fatalf("manifest=%#v", manifest)
	}
}

func TestDesktopNativeSearchCompositionRejectsMissingFTSExtension(t *testing.T) {
	_, err := OpenDesktopNativeSearchComposition(DesktopSearchConfig{}, filepath.Join(t.TempDir(), "desktop-search.lbug"), "")
	if err == nil {
		t.Fatal("expected missing bundled FTS extension to fail closed")
	}
}

func TestOpenDesktopNativeEndpointSupportsLexicalOnlyProfile(t *testing.T) {
	ftsExtensionPath := os.Getenv("LAYERDRAW_LADYBUG_FTS_EXTENSION")
	if !filepath.IsAbs(ftsExtensionPath) {
		t.Fatal("LAYERDRAW_LADYBUG_FTS_EXTENSION must be an absolute verified path")
	}
	root := t.TempDir()
	endpoint, search, shutdown, err := OpenDesktopNativeEndpoint(DesktopNativeConfig{
		LocalConfig:  LocalConfig{Root: root, ReleaseVersion: "0.0.0", SourceRevision: "unknown", ReleaseManifestDigest: "sha256:" + strings.Repeat("0", 64), EndpointInstanceID: "desktop-lexical-test", TransportID: "in_process"},
		DatabasePath: filepath.Join(root, "desktop-search.lbug"), FTSExtensionPath: ftsExtensionPath,
		VectorExtensionPath: filepath.Join(filepath.Dir(ftsExtensionPath), "libvector.lbug_extension"), AlgoExtensionPath: filepath.Join(filepath.Dir(ftsExtensionPath), "libalgo.lbug_extension"),
		PlanKey: []byte("01234567890123456789012345678901"), SearchDocumentKey: []byte("abcdefghijklmnopqrstuvwxyzABCDEF"), MaxRows: 100, MaxBytes: 4096,
	})
	if err != nil {
		t.Fatal(err)
	}
	defer shutdown(context.Background())
	manifest, err := search.Surface.Capabilities(context.Background())
	if err != nil || !manifest.SearchAvailable || manifest.EmbeddingAvailable || manifest.EmbeddingReason == "" {
		t.Fatalf("manifest=%#v err=%v", manifest, err)
	}
	project := t.TempDir()
	if err := os.WriteFile(filepath.Join(project, "document.ldl"), []byte("project p \"P\" {}\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	opened, err := endpoint.host.OpenProject(context.Background(), localdocument.OpenProjectInput{Root: project, EntryPath: "document.ldl"})
	if err != nil {
		t.Fatal(err)
	}
	if err := search.RefreshSearchIndex(context.Background(), opened.Session); err != nil {
		t.Fatalf("lexical-only lifecycle refresh: %v", err)
	}
}
