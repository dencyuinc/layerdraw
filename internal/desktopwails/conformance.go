// SPDX-License-Identifier: LicenseRef-LayerDraw-1.0

package desktopwails

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"time"

	"github.com/dencyuinc/layerdraw/gen/go/engineprotocol"
	"github.com/dencyuinc/layerdraw/gen/go/protocolcommon"
	"github.com/dencyuinc/layerdraw/gen/go/runtimeprotocol"
	"github.com/dencyuinc/layerdraw/gen/go/semantic"
	"github.com/dencyuinc/layerdraw/internal/desktopapp"
	"github.com/dencyuinc/layerdraw/internal/desktopcontract"
	"github.com/dencyuinc/layerdraw/internal/engine"
	"github.com/dencyuinc/layerdraw/internal/mcphost"
)

const packagedConformanceIterations = 5

var sourceRevisionPattern = regexp.MustCompile(`^[0-9a-f]{40}$`)

type ConformanceSamples struct {
	SamplesMilliseconds []int64 `json:"samples_milliseconds"`
}

type PackagedConformanceReport struct {
	SchemaVersion               uint32                        `json:"schema_version"`
	SourceRevision              string                        `json:"source_revision"`
	Platform                    string                        `json:"platform"`
	ArtifactKind                string                        `json:"artifact_kind"`
	Iterations                  int                           `json:"iterations"`
	Scenarios                   map[string]ConformanceSamples `json:"scenarios"`
	ProcessTreePeakRSSMebibytes []int64                       `json:"process_tree_peak_rss_mebibytes"`
	ScenarioEvidence            map[string]string             `json:"scenario_evidence"`
}

var conformanceEvidence = map[string]string{
	"cold_start":             "desktop.lifecycle.cold_start",
	"project_open":           "desktop.project.open_save_restart",
	"search_analysis":        "desktop.search.query_analysis",
	"preview":                "desktop.preview",
	"commit":                 "desktop.commit_durable",
	"viewer_interaction":     "desktop.viewer.2d_3d_interaction",
	"mcp_bounded_operations": "desktop.mcp.bounded_operations",
	"external_reconcile":     "desktop.external.reconcile",
	"shutdown":               "desktop.lifecycle.shutdown",
}

func RunPackagedConformance(output string) error {
	if !filepath.IsAbs(output) || filepath.Clean(output) != output {
		return errors.New("packaged conformance output must be absolute")
	}
	revision := os.Getenv("LAYERDRAW_CONFORMANCE_SOURCE_REVISION")
	if !sourceRevisionPattern.MatchString(revision) {
		return errors.New("packaged conformance source revision is invalid")
	}
	platform, err := conformancePlatform(CurrentPlatform())
	if err != nil {
		return err
	}
	report := PackagedConformanceReport{
		SchemaVersion: 1, SourceRevision: revision, Platform: platform,
		ArtifactKind: "installed_desktop", Iterations: packagedConformanceIterations,
		Scenarios: map[string]ConformanceSamples{}, ScenarioEvidence: cloneEvidence(),
	}
	runners := map[string]func(context.Context) error{
		"cold_start":             conformanceColdStart,
		"project_open":           conformanceProjectOpen,
		"search_analysis":        conformanceSearchAnalysis,
		"preview":                conformancePreview,
		"commit":                 conformanceCommitDurable,
		"viewer_interaction":     conformanceViewer,
		"mcp_bounded_operations": conformanceMCP,
		"external_reconcile":     conformanceExternal,
		"shutdown":               conformanceShutdown,
	}
	for iteration := 0; iteration < packagedConformanceIterations; iteration++ {
		for _, name := range conformanceScenarioOrder() {
			started := time.Now()
			ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
			err := runners[name](ctx)
			cancel()
			if err != nil {
				return fmt.Errorf("packaged conformance scenario %s iteration %d: %w", name, iteration+1, err)
			}
			elapsed := time.Since(started).Milliseconds()
			if elapsed < 1 {
				elapsed = 1
			}
			samples := report.Scenarios[name]
			samples.SamplesMilliseconds = append(samples.SamplesMilliseconds, elapsed)
			report.Scenarios[name] = samples
		}
		rss, err := processTreePeakRSSMebibytes()
		if err != nil || rss <= 0 {
			return errors.New("packaged conformance process-tree RSS is unavailable")
		}
		report.ProcessTreePeakRSSMebibytes = append(report.ProcessTreePeakRSSMebibytes, rss)
	}
	encoded, err := json.Marshal(report)
	if err != nil {
		return err
	}
	return writeExclusivePackagedProbe(output, append(encoded, '\n'))
}

func cloneEvidence() map[string]string {
	result := make(map[string]string, len(conformanceEvidence))
	for key, value := range conformanceEvidence {
		result[key] = value
	}
	return result
}

func conformanceScenarioOrder() []string {
	return []string{"cold_start", "project_open", "search_analysis", "preview", "commit", "viewer_interaction", "mcp_bounded_operations", "external_reconcile", "shutdown"}
}

func conformancePlatform(platform desktopcontract.DesktopPlatform) (string, error) {
	switch platform {
	case desktopcontract.PlatformMacOS:
		return "darwin", nil
	case desktopcontract.PlatformWindows:
		return "windows", nil
	case desktopcontract.PlatformLinux:
		return "linux", nil
	default:
		return "", errors.New("packaged conformance platform is invalid")
	}
}

type conformanceRuntime struct{}

func (conformanceRuntime) OpenDirectory(context.Context, string) (string, error) { return "", nil }
func (conformanceRuntime) OpenFile(context.Context, string, []string) (string, error) {
	return "", nil
}
func (conformanceRuntime) SaveFile(context.Context, string, []string) (string, error) {
	return "", nil
}
func (conformanceRuntime) ShowWindow(context.Context)           {}
func (conformanceRuntime) Quit(context.Context)                 {}
func (conformanceRuntime) Emit(context.Context, string, ...any) {}

type conformanceInstance struct {
	root  string
	app   *desktopapp.Application
	vault *selectionVault
}

func newConformanceInstance(ctx context.Context, external bool) (*conformanceInstance, error) {
	root, err := os.MkdirTemp("", "layerdraw-packaged-conformance-*")
	if err != nil {
		return nil, err
	}
	base, err := NewSharedConfig(root)
	if err != nil {
		os.RemoveAll(root)
		return nil, err
	}
	providers := map[string]ExternalProvider(nil)
	if external {
		base.Adapters[desktopcontract.ComponentExternalStorage] = disabledComponent{}
		providers = map[string]ExternalProvider{"conformance": conformanceProvider{}}
	}
	app, vault, err := compose(base, conformanceRuntime{}, providers)
	if err != nil {
		os.RemoveAll(root)
		return nil, err
	}
	if started := app.Start(ctx); started.Outcome != protocolcommon.OutcomeSuccess {
		os.RemoveAll(root)
		return nil, errors.New("canonical Desktop application did not start")
	}
	return &conformanceInstance{root: root, app: app, vault: vault}, nil
}

func (instance *conformanceInstance) close(ctx context.Context) error {
	err := instance.shutdown(ctx)
	removeErr := os.RemoveAll(instance.root)
	if err != nil {
		return err
	}
	return removeErr
}

func (instance *conformanceInstance) shutdown(ctx context.Context) error {
	result := instance.app.Shutdown(ctx)
	if result.Outcome != protocolcommon.OutcomeSuccess {
		return errors.New("canonical Desktop application did not stop")
	}
	return nil
}

func (instance *conformanceInstance) openProject(ctx context.Context, source string) (desktopapp.ProjectOpenResult, error) {
	path := filepath.Join(instance.root, "document.ldl")
	if err := os.WriteFile(path, []byte(source), 0o600); err != nil {
		return desktopapp.ProjectOpenResult{}, err
	}
	token, err := instance.vault.issue(path)
	if err != nil {
		return desktopapp.ProjectOpenResult{}, err
	}
	opened := instance.app.OpenProject(ctx, token)
	if opened.Outcome != protocolcommon.OutcomeSuccess || opened.Value.Open.Session.RuntimeSessionID == "" {
		return desktopapp.ProjectOpenResult{}, errors.New("project did not cross the Wails storage boundary")
	}
	return opened.Value, nil
}

func conformanceColdStart(ctx context.Context) error {
	instance, err := newConformanceInstance(ctx, false)
	if err != nil {
		return err
	}
	return instance.close(ctx)
}

func conformanceProjectOpen(ctx context.Context) error {
	instance, err := newConformanceInstance(ctx, false)
	if err != nil {
		return err
	}
	defer os.RemoveAll(instance.root)
	opened, err := instance.openProject(ctx, conformanceAuthoringSource)
	if err != nil {
		return err
	}
	if closed := instance.app.CloseProject(ctx, opened.Open.Session); closed.Outcome != protocolcommon.OutcomeSuccess {
		return errors.New("project close failed")
	}
	return instance.close(ctx)
}

func conformancePreview(ctx context.Context) error {
	instance, opened, input, err := conformanceAuthoringInput(ctx, "preview")
	if err != nil {
		return err
	}
	defer instance.close(context.Background())
	preview := instance.app.Preview(ctx, runtimeprotocol.PreviewOperationsInput{Session: opened.Open.Session, OperationBatch: input.OperationBatch})
	if preview.Outcome != protocolcommon.OutcomeSuccess || preview.Value.DefinitionHash == opened.Open.CommittedRevision.DefinitionHash {
		return fmt.Errorf("semantic preview did not produce an ephemeral revision: outcome=%s failure=%+v", preview.Outcome, preview.Failure)
	}
	return nil
}

func conformanceCommitDurable(ctx context.Context) error {
	instance, opened, input, err := conformanceAuthoringInput(ctx, "commit")
	if err != nil {
		return err
	}
	preview := instance.app.Preview(ctx, runtimeprotocol.PreviewOperationsInput{Session: opened.Open.Session, OperationBatch: input.OperationBatch})
	if preview.Outcome != protocolcommon.OutcomeSuccess {
		return fmt.Errorf("commit preview failed: outcome=%s failure=%+v", preview.Outcome, preview.Failure)
	}
	committed := instance.app.Commit(ctx, input)
	if committed.Outcome != protocolcommon.OutcomeSuccess || committed.Value.OperationResult.CommittedRevision == nil {
		return errors.New("durable commit failed")
	}
	revision := *committed.Value.OperationResult.CommittedRevision
	if err := instance.shutdown(ctx); err != nil {
		return err
	}
	defer os.RemoveAll(instance.root)
	base, err := NewSharedConfig(instance.root)
	if err != nil {
		return err
	}
	restarted, _, err := compose(base, conformanceRuntime{}, nil)
	if err != nil || restarted.Start(ctx).Outcome != protocolcommon.OutcomeSuccess {
		return errors.New("Desktop restart failed")
	}
	defer restarted.Shutdown(context.Background())
	reloaded := restarted.ReloadProject(ctx, revision.DocumentID)
	if reloaded.Outcome != protocolcommon.OutcomeSuccess || reloaded.Value.Open.CommittedRevision.RevisionID != revision.RevisionID || len(reloaded.Value.History.Items) < 2 {
		return errors.New("committed revision or history did not survive restart")
	}
	return nil
}

func conformanceAuthoringInput(ctx context.Context, suffix string) (*conformanceInstance, desktopapp.ProjectOpenResult, runtimeprotocol.RuntimeCommitInput, error) {
	instance, err := newConformanceInstance(ctx, false)
	if err != nil {
		return nil, desktopapp.ProjectOpenResult{}, runtimeprotocol.RuntimeCommitInput{}, err
	}
	opened, err := instance.openProject(ctx, conformanceAuthoringSource)
	if err != nil {
		instance.close(context.Background())
		return nil, desktopapp.ProjectOpenResult{}, runtimeprotocol.RuntimeCommitInput{}, err
	}
	batch, err := engineprotocol.DecodeSemanticOperationBatch([]byte(fmt.Sprintf(`{"operations":[{"operation":"create_subject","subject_kind":"layer","parent_address":"ldl:project:p","id":"%s_layer","fields":{"display_name":"Conformance","order":"1"}}]}`, suffix)))
	if err != nil {
		instance.close(context.Background())
		return nil, desktopapp.ProjectOpenResult{}, runtimeprotocol.RuntimeCommitInput{}, err
	}
	input := runtimeprotocol.RuntimeCommitInput{
		Session: opened.Open.Session, OperationID: runtimeprotocol.OperationID("conformance_" + suffix),
		IdempotencyKey: runtimeprotocol.IdempotencyKey("conformance_" + suffix + "_idempotency"),
		OperationBatch: runtimeprotocol.RuntimeOperationBatch{DocumentID: opened.ProjectID, BaseRevision: opened.Open.CommittedRevision, ExpectedDefinitionHash: opened.Open.CommittedRevision.DefinitionHash, Operations: batch, Preconditions: conformancePreconditions(conformanceAuthoringSource)},
		Trigger:        runtimeprotocol.CommitTriggerExplicitSave,
	}
	return instance, opened, input, nil
}

func conformancePreconditions(source string) engineprotocol.EngineEditPreconditions {
	compiled, err := engine.New(engine.BuildInfo{}).Compile(context.Background(), engine.CompileInput{
		Mode: engine.CompileProject, EntryPath: "document.ldl", ProjectSourceTree: map[string][]byte{"document.ldl": []byte(source)},
		ResolvedDependencies: engine.ResolvedDependencies{Format: "layerdraw-resolved", FormatVersion: 1, Language: 1},
	})
	if err != nil {
		return engineprotocol.EngineEditPreconditions{}
	}
	snapshot := compiled.Snapshot()
	result := engineprotocol.EngineEditPreconditions{DocumentGeneration: engineprotocol.DocumentGeneration{DocumentHandle: engineprotocol.DocumentHandle{EndpointInstanceID: "placeholder", Value: "document_placeholder_123456"}, Value: "1"}, ExpectedSubjectHashes: []engineprotocol.ExpectedHash{}, ExpectedSubtreeHashes: []engineprotocol.ExpectedHash{}, ExpectedChildSets: []engineprotocol.ExpectedChildSet{}}
	for _, item := range snapshot.SubjectSemanticHashes {
		result.ExpectedSubjectHashes = append(result.ExpectedSubjectHashes, engineprotocol.ExpectedHash{Address: semantic.StableAddress(item.Address), Hash: protocolcommon.Digest(item.Hash)})
	}
	for _, item := range snapshot.SubtreeHashes {
		result.ExpectedSubtreeHashes = append(result.ExpectedSubtreeHashes, engineprotocol.ExpectedHash{Address: semantic.StableAddress(item.OwnerAddress), Hash: protocolcommon.Digest(item.Hash)})
	}
	for _, item := range snapshot.ChildSetHashes {
		result.ExpectedChildSets = append(result.ExpectedChildSets, engineprotocol.ExpectedChildSet{OwnerAddress: semantic.StableAddress(item.OwnerAddress), ChildKind: semantic.SubjectKind(item.ChildKind), Hash: protocolcommon.Digest(item.Hash)})
	}
	sources := make([]engineprotocol.ExpectedSourceDigest, 0, len(snapshot.SourceMap.Files))
	for _, file := range snapshot.SourceMap.Files {
		origin := semantic.SourceOrigin{Kind: semantic.OriginKind(file.Origin.Kind)}
		if file.Origin.PackAddress != "" {
			address := semantic.PackRootAddress(file.Origin.PackAddress)
			origin.PackAddress = &address
		}
		sources = append(sources, engineprotocol.ExpectedSourceDigest{
			Module: semantic.ModuleRef{Origin: origin, ModulePath: file.ModulePath},
			Digest: protocolcommon.Digest(file.Digest),
		})
	}
	result.ExpectedSourceDigests = &sources
	return result
}

type conformanceWorkbench struct {
	instance   *conformanceInstance
	generation engineprotocol.DocumentGeneration
	handle     engineprotocol.DocumentHandle
	query      engineprotocol.QueryExecutionResultData
}

func newConformanceWorkbench(ctx context.Context) (*conformanceWorkbench, error) {
	instance, err := newConformanceInstance(ctx, false)
	if err != nil {
		return nil, err
	}
	source := []byte(conformanceProjectSource)
	ref := conformanceBlobRef("conformance-source", "text/plain; charset=utf-8", source)
	request := engineprotocol.OpenDocumentRequestEnvelope{Operation: engineprotocol.OpenDocumentRequestEnvelopeOperationValue, Protocol: conformanceEngineProtocol(), RequestID: "conformance-open", Payload: engineprotocol.OpenDocumentInput{CompileInput: engineprotocol.CompileInput{Mode: engineprotocol.CompileModeProject, EntryPath: "fixture.ldl", ProjectSourceTree: []engineprotocol.SourceFileInput{{Path: "fixture.ldl", Blob: ref}}, InstalledPackTree: []engineprotocol.SourceFileInput{}, ReferencedAssets: []engineprotocol.AssetInput{}, ResolvedDependencies: engineprotocol.ResolvedDependencies{Format: engineprotocol.ResolvedDependenciesFormatValue, FormatVersion: 1, Language: 1, Installs: []engineprotocol.ResolvedPack{}}, ResourceLimits: engineprotocol.ResourceLimits{}}, RequestedLimits: conformanceLimits()}}
	control, err := engineprotocol.EncodeOpenDocumentRequestEnvelope(request)
	if err != nil {
		return nil, err
	}
	result := instance.app.Invoke(ctx, "EngineOpenDocument", desktopcontract.Exchange{Operation: string(request.Operation), Control: control, Blobs: []desktopcontract.Blob{{ID: ref.BlobID, Bytes: source}}})
	response, err := engineprotocol.DecodeOpenDocumentResponseEnvelope(result.Value.Control)
	if result.Outcome != protocolcommon.OutcomeSuccess || err != nil || response.Outcome != protocolcommon.OutcomeSuccess || response.Payload == nil {
		return nil, fmt.Errorf("Wails Engine open failed: desktop=%s owner=%s failure=%+v diagnostics=%+v decode=%v", result.Outcome, response.Outcome, response.Failure, response.Diagnostics, err)
	}
	if response.Payload.State.SemanticState != "available" {
		return nil, fmt.Errorf("Wails Engine semantic state unavailable: state=%+v", response.Payload.State)
	}
	return &conformanceWorkbench{instance: instance, generation: response.Payload.DocumentGeneration, handle: response.Payload.DocumentHandle}, nil
}

func (workbench *conformanceWorkbench) close(ctx context.Context) error {
	request := engineprotocol.CloseDocumentRequestEnvelope{Operation: engineprotocol.CloseDocumentRequestEnvelopeOperationValue, Protocol: conformanceEngineProtocol(), RequestID: "conformance-close", Payload: engineprotocol.CloseDocumentInput{DocumentGeneration: workbench.generation, DocumentHandle: workbench.handle}}
	control, _ := engineprotocol.EncodeCloseDocumentRequestEnvelope(request)
	result := workbench.instance.app.Invoke(ctx, "EngineCloseDocument", desktopcontract.Exchange{Operation: string(request.Operation), Control: control})
	if result.Outcome != protocolcommon.OutcomeSuccess {
		return errors.New("Wails Engine close failed")
	}
	return workbench.instance.close(ctx)
}

func conformanceSearchAnalysis(ctx context.Context) error {
	workbench, err := newConformanceWorkbench(ctx)
	if err != nil {
		return err
	}
	defer workbench.close(context.Background())
	find := engineprotocol.FindSymbolsRequestEnvelope{Operation: engineprotocol.FindSymbolsRequestEnvelopeOperationValue, Protocol: conformanceEngineProtocol(), RequestID: "conformance-find", Payload: engineprotocol.FindSymbolsInput{CaseMode: "unicode_simple_fold", MatchMode: "substring", Query: "service", DocumentGeneration: workbench.generation, Limits: conformanceLimits()}}
	control, _ := engineprotocol.EncodeFindSymbolsRequestEnvelope(find)
	result := workbench.instance.app.Invoke(ctx, "EngineFindSymbols", desktopcontract.Exchange{Operation: string(find.Operation), Control: control})
	response, err := engineprotocol.DecodeFindSymbolsResponseEnvelope(result.Value.Control)
	if result.Outcome != protocolcommon.OutcomeSuccess || err != nil || response.Outcome != protocolcommon.OutcomeSuccess || response.Payload == nil || len(response.Payload.Items) == 0 {
		return fmt.Errorf("Wails search produced no canonical symbols: desktop=%s owner=%s payload=%+v failure=%+v diagnostics=%+v decode=%v", result.Outcome, response.Outcome, response.Payload, response.Failure, response.Diagnostics, err)
	}
	query := engineprotocol.ExecuteQueryRequestEnvelope{Operation: engineprotocol.ExecuteQueryRequestEnvelopeOperationValue, Protocol: conformanceEngineProtocol(), RequestID: "conformance-query", Payload: engineprotocol.ExecuteQueryInput{Arguments: map[string]semantic.RecipeScalar{}, DocumentGeneration: workbench.generation, Limits: conformanceLimits(), QueryAddress: "ldl:project:p:query:all"}}
	control, _ = engineprotocol.EncodeExecuteQueryRequestEnvelope(query)
	result = workbench.instance.app.Invoke(ctx, "EngineExecuteQuery", desktopcontract.Exchange{Operation: string(query.Operation), Control: control})
	queryResponse, err := engineprotocol.DecodeExecuteQueryResponseEnvelope(result.Value.Control)
	if result.Outcome != protocolcommon.OutcomeSuccess || err != nil || queryResponse.Outcome != protocolcommon.OutcomeSuccess || queryResponse.Payload == nil {
		return fmt.Errorf("Wails query failed: desktop=%s owner=%s failure=%+v diagnostics=%+v decode=%v", result.Outcome, queryResponse.Outcome, queryResponse.Failure, queryResponse.Diagnostics, err)
	}
	workbench.query = queryResponse.Payload.Result
	inspect := engineprotocol.InspectSubgraphRequestEnvelope{Operation: engineprotocol.InspectSubgraphRequestEnvelopeOperationValue, Protocol: conformanceEngineProtocol(), RequestID: "conformance-analysis", Payload: engineprotocol.InspectSubgraphInput{Depth: 2, DocumentGeneration: workbench.generation, Limits: conformanceLimits(), RootAddresses: []semantic.EntityAddress{"ldl:project:p:entity:a"}}}
	control, _ = engineprotocol.EncodeInspectSubgraphRequestEnvelope(inspect)
	result = workbench.instance.app.Invoke(ctx, "EngineInspectSubgraph", desktopcontract.Exchange{Operation: string(inspect.Operation), Control: control})
	analysis, err := engineprotocol.DecodeInspectSubgraphResponseEnvelope(result.Value.Control)
	if result.Outcome != protocolcommon.OutcomeSuccess || err != nil || analysis.Outcome != protocolcommon.OutcomeSuccess || analysis.Payload == nil {
		return errors.New("Wails graph analysis failed")
	}
	return nil
}

func conformanceViewer(ctx context.Context) error {
	workbench, err := newConformanceWorkbench(ctx)
	if err != nil {
		return err
	}
	defer workbench.close(context.Background())
	query := engineprotocol.ExecuteQueryRequestEnvelope{Operation: engineprotocol.ExecuteQueryRequestEnvelopeOperationValue, Protocol: conformanceEngineProtocol(), RequestID: "conformance-view-query", Payload: engineprotocol.ExecuteQueryInput{Arguments: map[string]semantic.RecipeScalar{}, DocumentGeneration: workbench.generation, Limits: conformanceLimits(), QueryAddress: "ldl:project:p:query:all"}}
	control, _ := engineprotocol.EncodeExecuteQueryRequestEnvelope(query)
	result := workbench.instance.app.Invoke(ctx, "EngineExecuteQuery", desktopcontract.Exchange{Operation: string(query.Operation), Control: control})
	queryResponse, err := engineprotocol.DecodeExecuteQueryResponseEnvelope(result.Value.Control)
	if result.Outcome != protocolcommon.OutcomeSuccess || err != nil || queryResponse.Outcome != protocolcommon.OutcomeSuccess || queryResponse.Payload == nil {
		return fmt.Errorf("Viewer query failed: desktop=%s owner=%s failure=%+v diagnostics=%+v decode=%v", result.Outcome, queryResponse.Outcome, queryResponse.Failure, queryResponse.Diagnostics, err)
	}
	view := engineprotocol.MaterializeViewRequestEnvelope{Operation: engineprotocol.MaterializeViewRequestEnvelopeOperationValue, Protocol: conformanceEngineProtocol(), RequestID: "conformance-view", Payload: engineprotocol.MaterializeViewInput{Kind: "query", Limits: conformanceLimits(), ViewAddress: "ldl:project:p:view:v", Query: &engineprotocol.MaterializeQueryViewInput{DocumentGeneration: workbench.generation, QueryResult: queryResponse.Payload.Result}}}
	control, _ = engineprotocol.EncodeMaterializeViewRequestEnvelope(view)
	result = workbench.instance.app.Invoke(ctx, "EngineMaterializeView", desktopcontract.Exchange{Operation: string(view.Operation), Control: control})
	viewResponse, err := engineprotocol.DecodeMaterializeViewResponseEnvelope(result.Value.Control)
	if result.Outcome != protocolcommon.OutcomeSuccess || err != nil || viewResponse.Outcome != protocolcommon.OutcomeSuccess || viewResponse.Payload == nil {
		return errors.New("Viewer materialization failed")
	}
	return nil
}

func conformanceMCP(ctx context.Context) error {
	instance, err := newConformanceInstance(ctx, false)
	if err != nil {
		return err
	}
	defer instance.close(context.Background())
	tools, failure := instance.app.MCPListTools(ctx)
	if failure != nil {
		return errors.New("bundled MCP Host discovery failed")
	}
	required := map[string]bool{"layerdraw.list_modules": false, "layerdraw.search": false, "layerdraw.preview_operations": false, "layerdraw.apply_operations": false, "layerdraw.run_query": false, "layerdraw.materialize_view": false, "layerdraw.plan_export": false, "layerdraw.list_revisions": false, "layerdraw.restore_revision": false, "layerdraw.registry_search": false}
	for _, tool := range tools {
		if _, ok := required[tool.Name]; ok {
			required[tool.Name] = true
		}
	}
	for name, found := range required {
		if !found {
			return fmt.Errorf("bundled MCP tool %s is absent", name)
		}
	}
	capabilities := instance.app.MCPCallTool(ctx, mcphost.CallToolRequest{Name: "layerdraw.get_capabilities", RequestID: "conformance-capabilities", Arguments: json.RawMessage(`{}`)})
	if capabilities.Failure != nil || len(capabilities.Content) == 0 {
		return errors.New("bundled MCP capability read failed")
	}
	if resources, resourceFailure := instance.app.MCPListResources(ctx); resourceFailure != nil || len(resources) == 0 {
		return errors.New("bundled MCP resource discovery failed")
	}
	return nil
}

type conformanceProvider struct{}

func (conformanceProvider) Connect(_ context.Context, request desktopapp.ExternalConnectionRequest) (desktopapp.ExternalConnection, error) {
	return desktopapp.ExternalConnection{ConnectionID: "conformance-connection", ProviderID: request.ProviderID, AccountLabel: request.AccountLabel, ScopeLabel: request.ScopeLabel, Status: desktopapp.ExternalConnectionConnected, Capabilities: desktopapp.ExternalProviderCapability{Open: true, ConditionalWrite: true, Lease: true, MoveDetection: true, ResumableUpload: true}}, nil
}
func (conformanceProvider) Sync(context.Context, desktopapp.ExternalSyncRequest) (desktopapp.ExternalSyncResult, error) {
	return desktopapp.ExternalSyncResult{ProviderVersion: "conformance-v1", ReconcileNeeded: true}, nil
}
func (conformanceProvider) Reconcile(context.Context, desktopapp.ExternalReconcileRequest) (desktopapp.ExternalReconcileResult, error) {
	return desktopapp.ExternalReconcileResult{ProviderVersion: "conformance-v2", Converged: true}, nil
}

func conformanceExternal(ctx context.Context) error {
	adapter := NewExternalAdapter(map[string]ExternalProvider{"conformance": conformanceProvider{}})
	connected := adapter.Connect(ctx, desktopapp.ExternalConnectionRequest{ProviderID: "conformance", AccountLabel: "fixture", ScopeLabel: "isolated"})
	if connected.Outcome != protocolcommon.OutcomeSuccess {
		return errors.New("external provider connection failed")
	}
	sync := adapter.Sync(ctx, desktopapp.ExternalSyncRequest{ConnectionID: connected.Value.ConnectionID, DocumentID: "conformance-document", Revision: runtimeprotocol.CommittedRevisionRef{DocumentID: "conformance-document"}})
	if sync.Outcome != protocolcommon.OutcomeSuccess || !sync.Value.ReconcileNeeded {
		return errors.New("external sync did not require reconcile")
	}
	reconciled := adapter.Reconcile(ctx, desktopapp.ExternalReconcileRequest{ConnectionID: connected.Value.ConnectionID, DocumentID: "conformance-document", Resolution: "accept_remote"})
	if reconciled.Outcome != protocolcommon.OutcomeSuccess || !reconciled.Value.Converged {
		return errors.New("external reconcile did not converge")
	}
	return nil
}

func conformanceShutdown(ctx context.Context) error {
	instance, err := newConformanceInstance(ctx, false)
	if err != nil {
		return err
	}
	return instance.close(ctx)
}

func conformanceBlobRef(id, mediaType string, content []byte) protocolcommon.BlobRef {
	digest := sha256.Sum256(content)
	return protocolcommon.BlobRef{BlobID: id, Digest: protocolcommon.Digest("sha256:" + hex.EncodeToString(digest[:])), Lifetime: protocolcommon.BlobLifetimeRequest, MediaType: mediaType, Size: protocolcommon.CanonicalUint64(fmt.Sprintf("%d", len(content)))}
}

func conformanceLimits() engineprotocol.WorkbenchLimits {
	return engineprotocol.WorkbenchLimits{MaxItems: "64", MaxOutputBytes: "65536"}
}

func conformanceEngineProtocol() engineprotocol.EngineProtocolRef {
	return engineprotocol.EngineProtocolRef{Name: engineprotocol.EngineProtocolRefNameValue, Version: "1.0"}
}

const conformanceProjectSource = `project p "Project" {}

layers {
  app "Application" @10
}

entity_type service "Service" {
  representation shape rect
}

entities service @app {
  a "Service A"
}

query all "All" {
  select {}
}

view v "V" inventory {
  source query all {}
  table {}
}
`

const conformanceAuthoringSource = "project p \"Project\" {}\n"
