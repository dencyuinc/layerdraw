// SPDX-License-Identifier: LicenseRef-LayerDraw-1.0
//go:build ladybug_native

package main

import (
	"context"
	"crypto/rand"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	searchadapter "github.com/dencyuinc/layerdraw/internal/adapter/search"
	hostendpoint "github.com/dencyuinc/layerdraw/internal/host"
	"github.com/dencyuinc/layerdraw/internal/runtime/port"
)

func runNativeSearchCheck(args []string, stdout, stderr io.Writer) (bool, int) {
	if len(args) == 0 || args[0] != "native-search-check" {
		return false, 0
	}
	if len(args) != 5 || args[1] != "--database" || args[3] != "--fts-extension" || !filepath.IsAbs(args[2]) || !filepath.IsAbs(args[4]) {
		fmt.Fprintln(stderr, "usage: layerdraw-host native-search-check --database ABSOLUTE_PATH --fts-extension ABSOLUTE_PATH")
		return true, 2
	}
	root := filepath.Dir(args[2])
	endpoint, search, shutdown, err := hostendpoint.OpenDesktopNativeEndpoint(hostendpoint.DesktopNativeConfig{
		LocalConfig:  hostendpoint.LocalConfig{Root: root, ReleaseVersion: releaseVersion, SourceRevision: sourceRevision, ReleaseManifestDigest: "sha256:" + strings.Repeat("0", 64), EndpointInstanceID: "native-search-check", TransportID: "in_process"},
		DatabasePath: args[2], FTSExtensionPath: args[4], PlanKey: []byte("native-search-check-plan-key-0001"), SearchDocumentKey: []byte("native-search-check-document-key01"), LocalModelSeed: []byte("native-search-check-model-seed-01"),
		EmbeddingProfile: port.EmbeddingProfile{ProfileID: "check", ModelID: "projection", ModelVersion: "1", ModelDigest: "sha256:model", Dimensions: 16, Normalization: "unit", MaxInputBytes: 1024}, MaxRows: 16, MaxBytes: 4096,
	})
	if err != nil {
		fmt.Fprintln(stderr, "layerdraw-host: native_search_composition_unavailable")
		return true, 1
	}
	defer shutdown(context.Background())
	if !endpoint.Supports(hostendpoint.OperationSearch) || !endpoint.Supports(hostendpoint.OperationExecuteQuery) || !endpoint.Supports(hostendpoint.OperationAnalyzeGraph) {
		fmt.Fprintln(stderr, "layerdraw-host: native_search_operations_unavailable")
		return true, 1
	}
	result, err := search.Surface.ExecuteAnalysis(context.Background(), port.BoundExecutionRequest{Snapshot: port.DocumentSnapshotRef{Kind: port.SnapshotHostRevision, HostDocumentID: "check", CommittedRevision: "r1", DefinitionHash: "sha256:def"}, AccessProjectionDigest: "sha256:access", Request: []byte(`{"kind":"count_search_documents"}`), MaxOutputBytes: 4096})
	if err != nil || !strings.Contains(string(result), "document_count") {
		fmt.Fprintln(stderr, "layerdraw-host: native_search_dispatch_failed")
		return true, 1
	}
	fmt.Fprintf(stdout, "layerdraw-host native-search ladybug %s fts loaded\n", searchadapter.GoLadybugBackendVersion)
	return true, 0
}

func openLocalEndpoint(config hostendpoint.LocalConfig) (*hostendpoint.Endpoint, func(context.Context) error, error) {
	stateRoot := filepath.Join(config.Root, ".layerdraw-native")
	if err := os.MkdirAll(stateRoot, 0o700); err != nil {
		return nil, nil, err
	}
	planKey, err := loadOrCreateSecret(filepath.Join(stateRoot, "search-plan.key"), 32)
	if err != nil {
		return nil, nil, err
	}
	documentKey, err := loadOrCreateSecret(filepath.Join(stateRoot, "search-document.key"), 32)
	if err != nil {
		return nil, nil, err
	}
	modelSeed, err := loadOrCreateSecret(filepath.Join(stateRoot, "embedding-model.seed"), 32)
	if err != nil {
		return nil, nil, err
	}
	executable, err := os.Executable()
	if err != nil {
		return nil, nil, err
	}
	endpoint, _, shutdown, err := hostendpoint.OpenDesktopNativeEndpoint(hostendpoint.DesktopNativeConfig{
		LocalConfig: config, DatabasePath: filepath.Join(stateRoot, "search.lbug"), FTSExtensionPath: filepath.Join(filepath.Dir(executable), "libfts.lbug_extension"),
		PlanKey: planKey, SearchDocumentKey: documentKey, LocalModelSeed: modelSeed,
		EmbeddingProfile: port.EmbeddingProfile{ProfileID: "layerdraw.local.projection", ModelID: "projection", ModelVersion: "1", ModelDigest: "sha256:83b1de40264e440055688d27480462c767f97c2f57c8208a360840988a902b40", Dimensions: 128, Normalization: "unit", MaxInputBytes: 65536},
		MaxRows:          10_000, MaxBytes: 16 << 20,
	})
	return endpoint, shutdown, err
}

func loadOrCreateSecret(path string, size int) ([]byte, error) {
	if data, err := os.ReadFile(path); err == nil {
		if len(data) != size {
			return nil, fmt.Errorf("invalid persisted native authority")
		}
		return data, nil
	} else if !os.IsNotExist(err) {
		return nil, err
	}
	data := make([]byte, size)
	if _, err := rand.Read(data); err != nil {
		return nil, err
	}
	file, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if err != nil {
		if os.IsExist(err) {
			return loadOrCreateSecret(path, size)
		}
		return nil, err
	}
	if _, err = file.Write(data); err != nil {
		_ = file.Close()
		return nil, err
	}
	if err := file.Sync(); err != nil {
		_ = file.Close()
		return nil, err
	}
	if err := file.Close(); err != nil {
		return nil, err
	}
	return data, nil
}
