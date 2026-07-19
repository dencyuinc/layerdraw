// SPDX-License-Identifier: LicenseRef-LayerDraw-1.0
//go:build ladybug_native

package main

import (
	"context"
	"crypto/rand"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	searchadapter "github.com/dencyuinc/layerdraw/internal/adapter/search"
	"github.com/dencyuinc/layerdraw/internal/engine"
	hostendpoint "github.com/dencyuinc/layerdraw/internal/host"
	layerruntime "github.com/dencyuinc/layerdraw/internal/runtime"
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
		DatabasePath: args[2], FTSExtensionPath: args[4], VectorExtensionPath: filepath.Join(filepath.Dir(args[4]), "libvector.lbug_extension"), AlgoExtensionPath: filepath.Join(filepath.Dir(args[4]), "libalgo.lbug_extension"), PlanKey: []byte("native-search-check-plan-key-0001"), SearchDocumentKey: []byte("native-search-check-document-key01"), LocalModelSeed: []byte("native-search-check-model-seed-01"), LocalAccessProjectionDigest: "sha256:access",
		EmbeddingProfile: port.EmbeddingProfile{ProfileID: "check", ModelID: "projection", ModelVersion: "1", ModelDigest: "sha256:model", Dimensions: 16, Normalization: "unit", MaxInputBytes: 1024}, MaxRows: 16, MaxBytes: 65536,
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
	const fixture = `project p "Project" {}
layers {
  app "Application" @1
}
entity_type service "Service" {
  representation shape rect
}
relation_type calls "Calls" data_flow {
  duplicate_policy allow
  from caller types [service] layers [app]
  to callee types [service] layers [app]
  label "calls"
}
entities service @app {
  alpha "Alpha Searchable"
  beta "Beta Target"
}
relations calls {
  alpha_beta: alpha -> beta
}
`
	snapshot := port.DocumentSnapshotRef{Kind: port.SnapshotHostRevision, HostDocumentID: "check", CommittedRevision: "r1", DefinitionHash: "sha256:def"}
	corpus, err := search.CorpusEngine().OpenLocalCorpus(context.Background(), engine.OpenDocumentInput{CompileInput: engine.CompileInput{Mode: engine.CompileProject, EntryPath: "document.ldl", ProjectSourceTree: map[string][]byte{"document.ldl": []byte(fixture)}, ResolvedDependencies: engine.ResolvedDependencies{Format: "layerdraw-resolved", FormatVersion: 1, Language: 1}}, RequestedLimits: engine.WorkbenchLimits{MaxItems: 1024, MaxOutputBytes: 1 << 20}}, snapshot, "sha256:access")
	if err != nil {
		fmt.Fprintln(stderr, "layerdraw-host: native_search_corpus_failed")
		return true, 1
	}
	profile := port.EmbeddingProfile{ProfileID: "check", ModelID: "projection", ModelVersion: "1", ModelDigest: "sha256:model", Dimensions: 16, Normalization: "unit", MaxInputBytes: 1024}
	searchProfile := port.SearchProfile{ProfileID: "default", SpecificationDigest: "sha256:search", MaxHits: 1, LexicalCandidateLimit: 8, SemanticCandidateLimit: 8, RRFK: 60, LexicalWeight: 1, SemanticWeight: 1}
	identity := port.SearchIndexIdentity{DocumentSnapshotRef: snapshot, SearchProfileID: searchProfile.ProfileID, SearchProfileDigest: searchProfile.SpecificationDigest, EmbeddingProfileID: profile.ProfileID, EmbeddingProfileDigest: profile.ModelDigest, AccessProjectionDigest: "sha256:access", LadybugBackendVersion: searchadapter.GoLadybugBackendVersion, IndexSchemaVersion: "1"}
	buildRequest := layerruntime.SearchIndexBuildRequest{Snapshot: snapshot, AccessProjectionDigest: "sha256:access", SearchProfile: searchProfile, EmbeddingProfile: &profile, IndexIdentity: identity, EngineRequest: []byte(`{"kind":"build_search_index"}`)}
	batchRequest := port.SearchDocumentBatchRequest{Snapshot: snapshot, AccessProjectionDigest: "sha256:access", EmbeddingProfileDigest: profile.ModelDigest, Corpus: corpus}
	_, err = search.RebuildIndex(context.Background(), buildRequest, batchRequest)
	if err != nil {
		fmt.Fprintln(stderr, "layerdraw-host: native_search_index_failed")
		return true, 1
	}
	if _, err = search.RebuildIndex(context.Background(), buildRequest, batchRequest); err != nil {
		fmt.Fprintln(stderr, "layerdraw-host: native_search_index_recovery_failed")
		return true, 1
	}
	lexical, err := search.Surface.Search(context.Background(), layerruntime.SearchRequest{Snapshot: snapshot, AccessProjectionDigest: "sha256:access", SearchProfile: searchProfile, EmbeddingProfile: &profile, IndexIdentity: identity, Mode: "lexical", QueryText: "alpha", EngineRequest: []byte(`{"kind":"search_documents","mode":"lexical","query_text":"alpha"}`), MaxOutputBytes: 65536})
	if err != nil || !strings.Contains(string(lexical), "subject_address") {
		fmt.Fprintln(stderr, "layerdraw-host: native_search_dispatch_failed")
		return true, 1
	}
	for _, mode := range []string{"semantic", "hybrid"} {
		searchRequest := layerruntime.SearchRequest{Snapshot: snapshot, AccessProjectionDigest: "sha256:access", SearchProfile: searchProfile, EmbeddingProfile: &profile, IndexIdentity: identity, Mode: mode, QueryText: "alpha", EngineRequest: []byte(fmt.Sprintf(`{"kind":"search_documents","mode":%q,"query_text":"alpha"}`, mode)), MaxOutputBytes: 65536}
		result, searchErr := search.Surface.Search(context.Background(), searchRequest)
		if searchErr != nil || !strings.Contains(string(result), "subject_address") {
			fmt.Fprintln(stderr, "layerdraw-host: native_search_dispatch_failed")
			return true, 1
		}
		if mode == "semantic" {
			var page struct {
				NextCursor      string `json:"next_cursor"`
				ResultTruncated bool   `json:"result_truncated"`
			}
			if json.Unmarshal(result, &page) != nil || !page.ResultTruncated || page.NextCursor == "" {
				fmt.Fprintln(stderr, "layerdraw-host: native_search_cursor_failed")
				return true, 1
			}
			searchRequest.Cursor = page.NextCursor
			next, nextErr := search.Surface.Search(context.Background(), searchRequest)
			if nextErr != nil || !strings.Contains(string(next), "subject_address") || string(next) == string(result) {
				fmt.Fprintln(stderr, "layerdraw-host: native_search_cursor_failed")
				return true, 1
			}
		}
	}
	query, err := search.Surface.ExecuteQuery(context.Background(), port.BoundExecutionRequest{Snapshot: snapshot, AccessProjectionDigest: "sha256:access", Request: []byte(`{"kind":"structural_query","root_addresses":["ldl:project:p:entity:alpha"]}`), MaxOutputBytes: 4096})
	if err != nil || !strings.Contains(string(query), "entity:alpha") {
		fmt.Fprintln(stderr, "layerdraw-host: native_query_dispatch_failed")
		return true, 1
	}
	algorithms := map[string]string{"page_rank": "importance", "k_core": "core_number", "louvain": "community_id", "scc": "component_id", "wcc": "component_id"}
	for algorithm, metric := range algorithms {
		request := []byte(fmt.Sprintf(`{"kind":"analyze_graph","algorithm":%q,"entity_addresses":["ldl:project:p:entity:alpha","ldl:project:p:entity:beta"],"relation_addresses":["ldl:project:p:relation:alpha_beta"]}`, algorithm))
		analysis, analysisErr := search.Surface.ExecuteAnalysis(context.Background(), port.BoundExecutionRequest{Snapshot: snapshot, AccessProjectionDigest: "sha256:access", Request: request, MaxOutputBytes: 4096})
		if analysisErr != nil || !strings.Contains(string(analysis), metric) || !strings.Contains(string(analysis), "entity:alpha") {
			fmt.Fprintln(stderr, "layerdraw-host: native_analysis_dispatch_failed")
			return true, 1
		}
	}
	fmt.Fprintf(stdout, "layerdraw-host native-search ladybug %s search-query-analysis verified\n", searchadapter.GoLadybugBackendVersion)
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
		VectorExtensionPath: filepath.Join(filepath.Dir(executable), "libvector.lbug_extension"), AlgoExtensionPath: filepath.Join(filepath.Dir(executable), "libalgo.lbug_extension"), LocalAccessProjectionDigest: "sha256:desktop-local-full-source",
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
