// SPDX-License-Identifier: LicenseRef-LayerDraw-1.0
//go:build ladybug_native

package host

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

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
		PlanKey: []byte("01234567890123456789012345678901"), SearchDocumentKey: []byte("abcdefghijklmnopqrstuvwxyzABCDEF"),
		LocalModelSeed:   []byte("0123456789012345"),
		EmbeddingProfile: port.EmbeddingProfile{ProfileID: "local", ModelID: "projection", ModelVersion: "1", ModelDigest: "sha256:model", Dimensions: 16, Normalization: "unit", MaxInputBytes: 1024},
		MaxRows:          100, MaxBytes: 4096,
	})
	if err != nil {
		t.Fatal(err)
	}
	defer shutdown(context.Background())
	for _, operation := range []string{OperationSearch, OperationExecuteQuery, OperationAnalyzeGraph} {
		if !endpoint.Supports(operation) {
			t.Fatalf("production endpoint does not support %s", operation)
		}
	}
	result, err := search.Surface.ExecuteAnalysis(context.Background(), port.BoundExecutionRequest{
		Snapshot:               port.DocumentSnapshotRef{Kind: port.SnapshotHostRevision, HostDocumentID: "check", CommittedRevision: "r1", DefinitionHash: "sha256:def"},
		AccessProjectionDigest: "sha256:access", Request: []byte(`{"kind":"count_search_documents"}`), MaxOutputBytes: 4096,
	})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(result), "document_count") {
		t.Fatalf("result=%s", result)
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
