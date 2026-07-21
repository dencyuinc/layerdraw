// SPDX-License-Identifier: LicenseRef-LayerDraw-1.0

package desktopwails

import (
	"archive/zip"
	"bytes"
	"context"
	"crypto/ed25519"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"github.com/dencyuinc/layerdraw/gen/go/accessprotocol"
	"github.com/dencyuinc/layerdraw/gen/go/engineprotocol"
	"github.com/dencyuinc/layerdraw/gen/go/protocolcommon"
	"github.com/dencyuinc/layerdraw/gen/go/runtimeprotocol"
	"github.com/dencyuinc/layerdraw/gen/go/semantic"
	accesscore "github.com/dencyuinc/layerdraw/internal/access"
	"github.com/dencyuinc/layerdraw/internal/adapter/registryengine"
	"github.com/dencyuinc/layerdraw/internal/adapter/registrysource"
	reviewapp "github.com/dencyuinc/layerdraw/internal/application/review"
	"github.com/dencyuinc/layerdraw/internal/desktopapp"
	"github.com/dencyuinc/layerdraw/internal/desktopcontract"
	enginecore "github.com/dencyuinc/layerdraw/internal/engine"
	"github.com/dencyuinc/layerdraw/internal/engine/endpoint"
	"github.com/dencyuinc/layerdraw/internal/mcphost"
	"github.com/dencyuinc/layerdraw/internal/registry"
)

const packagedConformanceIterations = 5

var sourceRevisionPattern = regexp.MustCompile(`^[0-9a-f]{40}$`)

type ConformanceSamples struct {
	SamplesMilliseconds []int64 `json:"samples_milliseconds"`
}

type PackagedConformanceReport struct {
	SchemaVersion            uint32                        `json:"schema_version"`
	SourceRevision           string                        `json:"source_revision"`
	Platform                 string                        `json:"platform"`
	ArtifactKind             string                        `json:"artifact_kind"`
	Iterations               int                           `json:"iterations"`
	Scenarios                map[string]ConformanceSamples `json:"scenarios"`
	IsolatedWorkerPeakRSSMiB []int64                       `json:"isolated_worker_peak_rss_mebibytes"`
	ScenarioEvidence         map[string]string             `json:"scenario_evidence"`
}

type packagedConformanceScenarioReport struct {
	SchemaVersion uint32 `json:"schema_version"`
	Scenario      string `json:"scenario"`
	WorkerPeakRSS int64  `json:"isolated_worker_peak_rss_mebibytes"`
}

type packagedConformanceError struct {
	code string
	err  error
}

type conformanceStageError struct {
	stage string
	err   error
}

func (failure *conformanceStageError) Error() string { return failure.stage }
func (failure *conformanceStageError) Unwrap() error { return failure.err }

func (failure *packagedConformanceError) Error() string { return failure.code }
func (failure *packagedConformanceError) Unwrap() error { return failure.err }

// PackagedConformanceFailureCode returns a closed, non-sensitive diagnostic
// code suitable for installer CI. Raw native paths and provider errors remain
// inside the process boundary.
func PackagedConformanceFailureCode(err error) string {
	var failure *packagedConformanceError
	if errors.As(err, &failure) {
		return failure.code
	}
	return ""
}

func conformanceFailure(code string, err error) error {
	return &packagedConformanceError{code: code, err: err}
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
		return conformanceFailure("invocation.output", errors.New("packaged conformance output must be absolute"))
	}
	revision := os.Getenv("LAYERDRAW_CONFORMANCE_SOURCE_REVISION")
	if !sourceRevisionPattern.MatchString(revision) {
		return conformanceFailure("invocation.revision", errors.New("packaged conformance source revision is invalid"))
	}
	platform, err := conformancePlatform(CurrentPlatform())
	if err != nil {
		return conformanceFailure("invocation.platform", err)
	}
	report := PackagedConformanceReport{
		SchemaVersion: 1, SourceRevision: revision, Platform: platform,
		ArtifactKind: "installed_desktop", Iterations: packagedConformanceIterations,
		Scenarios: map[string]ConformanceSamples{}, ScenarioEvidence: cloneEvidence(),
	}
	for iteration := 0; iteration < packagedConformanceIterations; iteration++ {
		var iterationPeak int64
		for _, name := range conformanceScenarioOrder() {
			started := time.Now()
			ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
			rss, err := runConformanceScenarioProcess(ctx, name)
			cancel()
			if err != nil {
				if PackagedConformanceFailureCode(err) != "" {
					return err
				}
				return conformanceFailure("scenario."+name, fmt.Errorf("iteration %d: %w", iteration+1, err))
			}
			elapsed := time.Since(started).Milliseconds()
			if elapsed < 1 {
				elapsed = 1
			}
			samples := report.Scenarios[name]
			samples.SamplesMilliseconds = append(samples.SamplesMilliseconds, elapsed)
			report.Scenarios[name] = samples
			if rss > iterationPeak {
				iterationPeak = rss
			}
		}
		if iterationPeak <= 0 {
			return conformanceFailure("measurement.memory", errors.New("packaged conformance isolated worker RSS is unavailable"))
		}
		report.IsolatedWorkerPeakRSSMiB = append(report.IsolatedWorkerPeakRSSMiB, iterationPeak)
	}
	encoded, err := json.Marshal(report)
	if err != nil {
		return conformanceFailure("result.encode", err)
	}
	if err := writeExclusivePackagedProbe(output, append(encoded, '\n')); err != nil {
		return conformanceFailure("result.write", err)
	}
	return nil
}

func conformanceRunners() map[string]func(context.Context) error {
	return map[string]func(context.Context) error{
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
}

var executeConformanceScenario = func(ctx context.Context, executable, name string) ([]byte, error) {
	return exec.CommandContext(ctx, executable, "--packaged-conformance-scenario", name).Output()
}

var runConformanceScenarioProcess = func(ctx context.Context, name string) (int64, error) {
	executable, err := os.Executable()
	if err != nil {
		return 0, errors.New("installed Desktop executable is unavailable")
	}
	encoded, err := executeConformanceScenario(ctx, executable, name)
	if err != nil {
		var exit *exec.ExitError
		if errors.As(err, &exit) {
			if code := parseConformanceChildFailure(exit.Stderr, name); code != "" {
				return 0, conformanceFailure(code, errors.New("isolated installed Desktop scenario failed"))
			}
		}
		return 0, errors.New("isolated installed Desktop scenario failed")
	}
	var report packagedConformanceScenarioReport
	decoder := json.NewDecoder(strings.NewReader(string(encoded)))
	decoder.DisallowUnknownFields()
	if decoder.Decode(&report) != nil || decoder.Decode(new(any)) != io.EOF || report.SchemaVersion != 1 || report.Scenario != name || report.WorkerPeakRSS <= 0 {
		return 0, errors.New("isolated installed Desktop scenario result is invalid")
	}
	return report.WorkerPeakRSS, nil
}

func parseConformanceChildFailure(stderr []byte, scenario string) string {
	const prefix = "LayerDraw Desktop conformance failed ["
	line := strings.TrimSpace(string(stderr))
	if !strings.HasPrefix(line, prefix) || !strings.HasSuffix(line, "]") {
		return ""
	}
	code := strings.TrimSuffix(strings.TrimPrefix(line, prefix), "]")
	if !strings.HasPrefix(code, "scenario."+scenario) && code != "measurement.memory" {
		return ""
	}
	for _, character := range code {
		if (character < 'a' || character > 'z') && (character < '0' || character > '9') && character != '_' && character != '.' {
			return ""
		}
	}
	return code
}

// RunPackagedConformanceScenario executes one isolated workflow for the
// installed conformance parent process. It is intentionally not a user-facing
// Desktop mode and emits only a strict measurement envelope.
func RunPackagedConformanceScenario(name string, output io.Writer) error {
	runner := conformanceRunners()[name]
	if runner == nil || output == nil {
		return conformanceFailure("scenario.invalid", errors.New("packaged conformance scenario is invalid"))
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	if err := runner(ctx); err != nil {
		var stage *conformanceStageError
		if errors.As(err, &stage) {
			return conformanceFailure("scenario."+name+"."+stage.stage, err)
		}
		return conformanceFailure("scenario."+name, err)
	}
	rss, err := isolatedWorkerPeakRSSMebibytes()
	if err != nil || rss <= 0 {
		return conformanceFailure("measurement.memory", errors.New("scenario process RSS is unavailable"))
	}
	return json.NewEncoder(output).Encode(packagedConformanceScenarioReport{SchemaVersion: 1, Scenario: name, WorkerPeakRSS: rss})
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
	owner *sharedOwner
}

func newConformanceInstance(ctx context.Context, external bool) (*conformanceInstance, error) {
	root, err := os.MkdirTemp("", "layerdraw-packaged-conformance-*")
	if err != nil {
		return nil, &conformanceStageError{stage: "root", err: err}
	}
	base, err := NewSharedConfig(root)
	if err != nil {
		os.RemoveAll(root)
		return nil, &conformanceStageError{stage: "config", err: err}
	}
	owner, ok := base.Adapters[desktopcontract.ComponentBindingShell].(*sharedOwner)
	if !ok || owner == nil {
		os.RemoveAll(root)
		return nil, &conformanceStageError{stage: "config", err: errors.New("canonical Desktop shared owner is unavailable")}
	}
	providers := map[string]ExternalProvider(nil)
	if external {
		base.Adapters[desktopcontract.ComponentExternalStorage] = disabledComponent{}
		providers = map[string]ExternalProvider{"conformance": conformanceProvider{}}
	}
	app, vault, err := compose(base, conformanceRuntime{}, providers)
	if err != nil {
		os.RemoveAll(root)
		return nil, &conformanceStageError{stage: "compose", err: err}
	}
	if started := app.Start(ctx); started.Outcome != protocolcommon.OutcomeSuccess {
		os.RemoveAll(root)
		stage := "start"
		if started.Failure != nil && started.Failure.Validate() {
			stage += "_" + string(started.Failure.Component)
		}
		return nil, &conformanceStageError{stage: stage, err: errors.New("canonical Desktop application did not start")}
	}
	return &conformanceInstance{root: root, app: app, vault: vault, owner: owner}, nil
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
		stage := "project_open"
		if opened.Failure != nil && opened.Failure.Validate() {
			stage += "_" + string(opened.Failure.Component)
		}
		return desktopapp.ProjectOpenResult{}, &conformanceStageError{stage: stage, err: errors.New("project did not cross the Wails storage boundary")}
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
	preconditions, err := conformancePreconditions(ctx, conformanceAuthoringSource)
	if err != nil {
		instance.close(context.Background())
		return nil, desktopapp.ProjectOpenResult{}, runtimeprotocol.RuntimeCommitInput{}, err
	}
	input := runtimeprotocol.RuntimeCommitInput{
		Session: opened.Open.Session, OperationID: runtimeprotocol.OperationID("conformance_" + suffix),
		IdempotencyKey: runtimeprotocol.IdempotencyKey("conformance_" + suffix + "_idempotency"),
		OperationBatch: runtimeprotocol.RuntimeOperationBatch{DocumentID: opened.ProjectID, BaseRevision: opened.Open.CommittedRevision, ExpectedDefinitionHash: opened.Open.CommittedRevision.DefinitionHash, Operations: batch, Preconditions: preconditions},
		Trigger:        runtimeprotocol.CommitTriggerExplicitSave,
	}
	return instance, opened, input, nil
}

func conformancePreconditions(ctx context.Context, source string) (engineprotocol.EngineEditPreconditions, error) {
	generation := engineprotocol.DocumentGeneration{DocumentHandle: engineprotocol.DocumentHandle{EndpointInstanceID: "placeholder", Value: "document_placeholder_123456"}, Value: "1"}
	return endpoint.CompileProjectEditPreconditions(ctx, endpoint.LocalProjectInput{
		EntryPath:         "document.ldl",
		ProjectSourceTree: map[string][]byte{"document.ldl": []byte(source)},
		ResolvedDependencies: endpoint.LocalResolvedDependencies{
			Format: "layerdraw-resolved", FormatVersion: 1, Language: 1,
		},
	}, generation)
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
	inspect := engineprotocol.InspectSubgraphRequestEnvelope{Operation: engineprotocol.InspectSubgraphRequestEnvelopeOperationValue, Protocol: conformanceEngineProtocol(), RequestID: "conformance-analysis", Payload: engineprotocol.InspectSubgraphInput{Depth: 2, DocumentGeneration: workbench.generation, Limits: conformanceLimits(), RootAddresses: []semantic.EntityAddress{"ldl:project:p:entity:alpha"}}}
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
	workbench, err := newConformanceWorkbench(ctx)
	if err != nil {
		return err
	}
	defer workbench.close(context.Background())
	enabled := workbench.instance.app.SetMCPEnabled(ctx, true, desktopapp.MCPTransportLocal)
	if enabled.Outcome != protocolcommon.OutcomeSuccess || !enabled.Value.Enabled {
		return fmt.Errorf("bundled MCP Host enable failed: outcome=%s failure=%+v", enabled.Outcome, enabled.Failure)
	}
	tools, failure := workbench.instance.app.MCPListTools(ctx)
	if failure != nil {
		return fmt.Errorf("bundled MCP Host discovery failed: %+v", failure)
	}
	required := map[string]bool{
		"layerdraw.list_modules": false, "layerdraw.find_symbols": false,
		"layerdraw.preview_operations": false, "layerdraw.apply_operations": false,
		"layerdraw.materialize_view": false, "layerdraw.plan_export": false,
		"layerdraw.list_revisions": false, "layerdraw.restore_revision": false,
	}
	for _, tool := range tools {
		if _, ok := required[tool.Name]; ok {
			required[tool.Name] = true
		}
	}
	for name, found := range required {
		if !found {
			names := make([]string, 0, len(tools))
			for _, tool := range tools {
				names = append(names, tool.Name)
			}
			return fmt.Errorf("bundled MCP tool %s is absent from %v", name, names)
		}
	}
	opened, err := workbench.instance.openProject(ctx, conformanceAuthoringSource)
	if err != nil {
		return err
	}
	commitInput, err := conformanceCommitInput(ctx, opened, "mcp")
	if err != nil {
		return err
	}
	binding := &mcphost.Binding{DocumentID: opened.ProjectID, RevisionDigest: opened.Open.CommittedRevision.DefinitionHash, AccessFingerprint: opened.Open.Session.Scope.AccessFingerprint}
	if err := conformanceMCPRegistry(ctx, workbench.instance); err != nil {
		return err
	}
	if err := conformanceMCPReview(ctx, workbench.instance, opened, commitInput, binding); err != nil {
		return err
	}
	publication, err := workbench.instance.app.ProjectPublication(ctx)
	if err != nil || publication.Project == nil {
		return errors.New("bundled MCP post-Review project publication failed")
	}
	restoreTarget := publication.Project.AuthoritativeRevision
	currentSource, err := os.ReadFile(filepath.Join(workbench.instance.root, "document.ldl"))
	if err != nil {
		return err
	}
	if !bytes.Contains(currentSource, []byte("mcp_layer")) {
		return errors.New("bundled MCP Review approval was not durably saved")
	}
	restorePreconditions, err := conformancePreconditions(ctx, string(currentSource))
	if err != nil {
		return err
	}
	awayOperations, err := engineprotocol.DecodeSemanticOperationBatch([]byte(`{"operations":[{"operation":"update_subject_field","target_address":"ldl:project:p:layer:mcp_layer","path":["display_name"],"action":"set","value":{"kind":"string","string":"Restore setup"}}]}`))
	if err != nil {
		return err
	}
	commitInput.OperationID = "conformance_restore_away"
	commitInput.IdempotencyKey = "conformance_restore_away_idempotency"
	commitInput.OperationBatch.BaseRevision = restoreTarget
	commitInput.OperationBatch.ExpectedDefinitionHash = restoreTarget.DefinitionHash
	commitInput.OperationBatch.Operations = awayOperations
	commitInput.OperationBatch.Preconditions = restorePreconditions
	awayPreview := workbench.instance.app.Preview(ctx, runtimeprotocol.PreviewOperationsInput{Session: opened.Open.Session, OperationBatch: commitInput.OperationBatch})
	if awayPreview.Outcome != protocolcommon.OutcomeSuccess {
		return fmt.Errorf("bundled MCP restore setup preview failed: %+v", awayPreview.Failure)
	}
	commitInput.AuthoringProof = awayPreview.Value.AuthoringProof
	awayCommit := workbench.instance.app.Commit(ctx, commitInput)
	if awayCommit.Outcome != protocolcommon.OutcomeSuccess || awayCommit.Value.OperationResult.CommittedRevision == nil {
		return fmt.Errorf("bundled MCP restore setup save failed: %+v", awayCommit.Failure)
	}
	publication, err = workbench.instance.app.ProjectPublication(ctx)
	if err != nil || publication.Project == nil || publication.Project.AuthoritativeRevision.RevisionID == restoreTarget.RevisionID {
		return errors.New("bundled MCP restore setup publication failed")
	}
	currentSource, err = os.ReadFile(filepath.Join(workbench.instance.root, "document.ldl"))
	if err != nil {
		return err
	}
	restorePreconditions, err = conformancePreconditions(ctx, string(currentSource))
	if err != nil {
		return err
	}
	restoreOperations, err := engineprotocol.DecodeSemanticOperationBatch([]byte(`{"operations":[{"operation":"update_subject_field","target_address":"ldl:project:p:layer:mcp_layer","path":["display_name"],"action":"set","value":{"kind":"string","string":"Conformance"}}]}`))
	if err != nil {
		return err
	}
	commitInput.OperationID = "conformance_restore_commit"
	commitInput.IdempotencyKey = "conformance_restore_commit_idempotency"
	commitInput.OperationBatch.BaseRevision = publication.Project.AuthoritativeRevision
	commitInput.OperationBatch.ExpectedDefinitionHash = publication.Project.AuthoritativeRevision.DefinitionHash
	commitInput.OperationBatch.Operations = restoreOperations
	commitInput.OperationBatch.Preconditions = restorePreconditions
	commitInput.AuthoringProof = runtimeprotocol.AuthoringProof{}
	binding = &mcphost.Binding{DocumentID: opened.ProjectID, RevisionDigest: publication.Project.AuthoritativeRevision.DefinitionHash, AccessFingerprint: opened.Open.Session.Scope.AccessFingerprint}
	if err := conformanceMCPHistoryRestore(ctx, workbench.instance.app, opened, restoreTarget, commitInput, binding); err != nil {
		return err
	}
	restoredSource, err := os.ReadFile(filepath.Join(workbench.instance.root, "document.ldl"))
	if err != nil {
		return err
	}
	restoredProject, err := endpoint.NewLocalDocumentEngine().CompileProject(ctx, endpoint.LocalProjectInput{
		EntryPath:         "document.ldl",
		ProjectSourceTree: map[string][]byte{"document.ldl": restoredSource},
		ResolvedDependencies: endpoint.LocalResolvedDependencies{
			Format: "layerdraw-resolved", FormatVersion: 1, Language: 1,
		},
	})
	if err != nil || restoredProject.DefinitionHash != restoreTarget.DefinitionHash || restoredProject.GraphHash != restoreTarget.GraphHash {
		return fmt.Errorf("bundled MCP restore durable read-back failed: %w", err)
	}
	if err := conformanceMCPAgentScope(ctx, workbench.instance.app, opened); err != nil {
		return err
	}
	capabilities := workbench.instance.app.MCPCallTool(ctx, mcphost.CallToolRequest{Name: "layerdraw.get_capabilities", RequestID: "conformance-capabilities", Arguments: json.RawMessage(`{}`)})
	if capabilities.Failure != nil || len(capabilities.Content) == 0 {
		return errors.New("bundled MCP capability read failed")
	}
	list := engineprotocol.ListModulesRequestEnvelope{
		Operation: engineprotocol.ListModulesRequestEnvelopeOperationValue,
		Payload: engineprotocol.ListModulesInput{
			DocumentGeneration: workbench.generation,
			Limits:             conformanceLimits(),
		},
		Protocol: conformanceEngineProtocol(), RequestID: "conformance-mcp-list",
	}
	arguments, err := engineprotocol.EncodeListModulesRequestEnvelope(list)
	if err != nil {
		return err
	}
	digest := protocolcommon.Digest(desktopDigest)
	listed := workbench.instance.app.MCPCallTool(ctx, mcphost.CallToolRequest{
		Name: "layerdraw.list_modules", RequestID: "conformance-mcp-list", Arguments: arguments,
		Binding: &mcphost.Binding{DocumentID: "conformance-document", RevisionDigest: digest, AccessFingerprint: digest},
	})
	listResponse, decodeErr := engineprotocol.DecodeListModulesResponseEnvelope(listed.Content)
	if listed.Failure != nil || decodeErr != nil || listResponse.Outcome != protocolcommon.OutcomeSuccess || listResponse.Payload == nil || len(listResponse.Payload.Items) == 0 {
		return fmt.Errorf("bundled MCP bounded read failed: failure=%+v decode=%v", listed.Failure, decodeErr)
	}
	resources, resourceFailure := workbench.instance.app.MCPListResources(ctx)
	if resourceFailure != nil || len(resources) == 0 {
		return errors.New("bundled MCP resource discovery failed")
	}
	read := workbench.instance.app.MCPReadResource(ctx, mcphost.ReadResourceRequest{URI: resources[0].URI})
	if read.Failure != nil || len(read.Content) == 0 || read.MimeType == "" {
		return errors.New("bundled MCP resource read failed")
	}
	if closed := workbench.instance.app.CloseProject(ctx, opened.Open.Session); closed.Outcome != protocolcommon.OutcomeSuccess {
		return fmt.Errorf("bundled MCP authoring project close failed: %+v", closed.Failure)
	}
	native, err := newConformanceInstance(ctx, false)
	if err != nil {
		return err
	}
	defer native.close(context.Background())
	enabled = native.app.SetMCPEnabled(ctx, true, desktopapp.MCPTransportLocal)
	if enabled.Outcome != protocolcommon.OutcomeSuccess || !enabled.Value.Enabled {
		return fmt.Errorf("bundled native MCP Host enable failed: outcome=%s failure=%+v", enabled.Outcome, enabled.Failure)
	}
	nativeTools, failure := native.app.MCPListTools(ctx)
	if failure != nil {
		return fmt.Errorf("bundled native MCP Host discovery failed: %+v", failure)
	}
	return conformanceNativeMCP(ctx, native, nativeTools)
}

func conformanceCommitInput(ctx context.Context, opened desktopapp.ProjectOpenResult, suffix string) (runtimeprotocol.RuntimeCommitInput, error) {
	batch, err := engineprotocol.DecodeSemanticOperationBatch([]byte(fmt.Sprintf(`{"operations":[{"operation":"create_subject","subject_kind":"layer","parent_address":"ldl:project:p","id":"%s_layer","fields":{"display_name":"Conformance","order":"1"}}]}`, suffix)))
	if err != nil {
		return runtimeprotocol.RuntimeCommitInput{}, err
	}
	preconditions, err := conformancePreconditions(ctx, conformanceAuthoringSource)
	if err != nil {
		return runtimeprotocol.RuntimeCommitInput{}, err
	}
	return runtimeprotocol.RuntimeCommitInput{
		Session: opened.Open.Session, OperationID: runtimeprotocol.OperationID("conformance_" + suffix),
		IdempotencyKey: runtimeprotocol.IdempotencyKey("conformance_" + suffix + "_idempotency"),
		OperationBatch: runtimeprotocol.RuntimeOperationBatch{DocumentID: opened.ProjectID, BaseRevision: opened.Open.CommittedRevision, ExpectedDefinitionHash: opened.Open.CommittedRevision.DefinitionHash, Operations: batch, Preconditions: preconditions},
		Trigger:        runtimeprotocol.CommitTriggerAgentApply,
	}, nil
}

func conformanceMCPRegistry(ctx context.Context, instance *conformanceInstance) error {
	const canonicalID = "layerdraw/conformance"
	root := filepath.Join(instance.root, "conformance-registry")
	if err := os.MkdirAll(filepath.Join(root, filepath.Dir(registrysource.CatalogPath)), 0o700); err != nil {
		return err
	}
	manifest := []byte("{\"dependencies\":{},\"entry\":\"pack.ldl\",\"format\":\"layerdraw-pack\",\"format_version\":1,\"id\":\"layerdraw/conformance\",\"language\":1,\"name\":\"conformance\",\"version\":\"1.0.0\"}\n")
	source := []byte("entity_type conformance \"Conformance\" {\n  representation shape rect\n}\nexport { conformance }\n")
	var archive bytes.Buffer
	writer := zip.NewWriter(&archive)
	for _, file := range []struct {
		name string
		data []byte
	}{{"manifest.json", manifest}, {"pack.ldl", source}} {
		entry, err := writer.CreateHeader(&zip.FileHeader{Name: file.name, Method: zip.Store})
		if err != nil {
			return err
		}
		if _, err := entry.Write(file.data); err != nil {
			return err
		}
	}
	if err := writer.Close(); err != nil {
		return err
	}
	artifact := archive.Bytes()
	if _, err := enginecore.New(enginecore.BuildInfo{}).ReadRegistryPack(ctx, artifact, enginecore.LayerdrawLimits{}); err != nil {
		return fmt.Errorf("installed Registry pack fixture is invalid: %w", err)
	}
	digest := func(value []byte) string { sum := sha256.Sum256(value); return "sha256:" + hex.EncodeToString(sum[:]) }
	release := registry.ArtifactRelease{Identity: registry.ArtifactIdentity{Kind: registry.ArtifactPack, CanonicalID: canonicalID, Version: "1.0.0"}, SourceID: "installed-conformance", PublisherID: "layerdraw", Digest: digest(artifact), ManifestDigest: digest(manifest), DependencyMetadataDigest: digest([]byte("[]")), Size: int64(len(artifact)), Dependencies: []registry.Dependency{}, Compatibility: []registry.CompatibilityDecision{}, License: "LicenseRef-LayerDraw-1.0", ProvenanceDigest: digest([]byte("installed-conformance"))}
	catalog, err := json.Marshal(registrysource.Catalog{SchemaVersion: registrysource.CatalogVersion, Artifacts: []registrysource.CatalogEntry{{Release: release, ArtifactPath: "conformance.ldpack"}}})
	if err != nil {
		return err
	}
	if err := os.WriteFile(filepath.Join(root, "conformance.ldpack"), artifact, 0o600); err != nil {
		return err
	}
	if err := os.WriteFile(filepath.Join(root, registrysource.CatalogPath), catalog, 0o600); err != nil {
		return err
	}
	const policyID = "installed-conformance-local"
	if err := instance.owner.registry.PutTrustPolicy(registry.TrustPolicy{PolicyID: policyID, AllowUnsignedLocal: true, TrustedPublishers: nil, PublicKeys: map[string]ed25519.PublicKey{}, RevokedKeys: map[string]bool{}}); err != nil {
		return err
	}
	configured := registry.RegistrySource{SourceID: release.SourceID, Kind: registry.SourceLocalDirectory, EndpointRef: root, TrustPolicyID: policyID, CachePolicy: "verified", Priority: 100, Connected: true}
	found, err := (registrysource.LocalDirectory{}).Search(ctx, configured, registry.SearchInput{Query: canonicalID})
	if err != nil || len(found) != 1 {
		return fmt.Errorf("installed Registry source fixture failed: %w", err)
	}
	validator, err := registryengine.New(instance.owner.objects, instance.owner.local)
	if err != nil {
		return err
	}
	if _, err := validator.ValidateRegistryArtifact(ctx, release, artifact); err != nil {
		return fmt.Errorf("installed Registry artifact fixture failed: %w", err)
	}
	if err := instance.owner.registry.ConfigureSource(configured); err != nil {
		return err
	}
	if err := instance.owner.registry.ConnectSource(ctx, configured.SourceID, "installed-conformance-local"); err != nil {
		return err
	}
	input, _ := json.Marshal(registry.SearchInput{Query: canonicalID})
	request, _ := json.Marshal(registry.WireRequest{WireVersion: registry.RegistryWireVersion, Operation: registry.WireSearch, RequestID: "conformance-registry", Input: input})
	result := instance.app.MCPCallTool(ctx, mcphost.CallToolRequest{Name: "layerdraw.registry_search", RequestID: "conformance-registry", Arguments: request})
	response, err := registry.DecodeWireResponse(result.Content, registry.WireSearch)
	var releases []registry.ArtifactRelease
	if result.Failure != nil || err != nil || !response.OK || json.Unmarshal(response.Value, &releases) != nil || len(releases) != 1 || releases[0].Identity.CanonicalID != canonicalID {
		return fmt.Errorf("bundled MCP Registry search failed: failure=%+v response=%+v decode=%v", result.Failure, response.Failure, err)
	}
	return nil
}

func conformanceMCPHistoryRestore(ctx context.Context, app *desktopapp.Application, opened desktopapp.ProjectOpenResult, selected runtimeprotocol.CommittedRevisionRef, commit runtimeprotocol.RuntimeCommitInput, binding *mcphost.Binding) error {
	protocol := runtimeprotocol.RuntimeProtocolRef{Name: runtimeprotocol.RuntimeProtocolRefNameValue, Version: "1.0"}
	list, err := runtimeprotocol.EncodeListRevisionsRequestEnvelope(runtimeprotocol.ListRevisionsRequestEnvelope{Operation: runtimeprotocol.ListRevisionsRequestEnvelopeOperationValue, Protocol: protocol, RequestID: "conformance-history", Payload: runtimeprotocol.ListRevisionsInput{Session: opened.Open.Session, MaxItems: "20", MaxOutputBytes: "1048576"}})
	if err != nil {
		return err
	}
	listed := app.MCPCallTool(ctx, mcphost.CallToolRequest{Name: "layerdraw.list_revisions", RequestID: "conformance-history", Arguments: list, Binding: binding})
	history, err := runtimeprotocol.DecodeListRevisionsResponseEnvelope(listed.Content)
	if listed.Failure != nil || err != nil || history.Payload == nil || len(history.Payload.Items) == 0 {
		return fmt.Errorf("bundled MCP history failed: failure=%+v decode=%v", listed.Failure, err)
	}
	foundTarget := false
	for _, item := range history.Payload.Items {
		if item.Revision.RevisionID == selected.RevisionID && item.Revision.DefinitionHash == selected.DefinitionHash && item.Revision.GraphHash == selected.GraphHash {
			foundTarget = true
			break
		}
	}
	if !foundTarget {
		return errors.New("bundled MCP history omitted the selected restore revision")
	}
	preview, err := runtimeprotocol.EncodeRestorePreviewRequestEnvelope(runtimeprotocol.RestorePreviewRequestEnvelope{Operation: runtimeprotocol.RestorePreviewRequestEnvelopeOperationValue, Protocol: protocol, RequestID: "conformance-restore", Payload: runtimeprotocol.RestorePreviewInput{Session: opened.Open.Session, RevisionID: selected.RevisionID}})
	if err != nil {
		return err
	}
	validated := app.Preview(ctx, runtimeprotocol.PreviewOperationsInput{Session: opened.Open.Session, OperationBatch: commit.OperationBatch})
	if validated.Outcome != protocolcommon.OutcomeSuccess {
		return fmt.Errorf("bundled MCP restore commit preview failed: %+v base=%+v", validated.Failure, commit.OperationBatch.BaseRevision)
	}
	commit.AuthoringProof = validated.Value.AuthoringProof
	commit.Trigger = runtimeprotocol.CommitTriggerAgentApply
	commitWire, err := runtimeprotocol.EncodeCommitOperationsRequestEnvelope(runtimeprotocol.CommitOperationsRequestEnvelope{Operation: runtimeprotocol.CommitOperationsRequestEnvelopeOperationValue, Protocol: protocol, RequestID: "conformance-restore", Payload: commit})
	if err != nil {
		return err
	}
	arguments, _ := json.Marshal(map[string]json.RawMessage{"preview": preview, "commit": commitWire})
	restored := app.MCPCallTool(ctx, mcphost.CallToolRequest{Name: "layerdraw.restore_revision", RequestID: "conformance-restore", Arguments: arguments, Binding: binding})
	response, decodeErr := runtimeprotocol.DecodeCommitOperationsResponseEnvelope(restored.Content)
	if restored.Failure != nil || decodeErr != nil || response.Outcome != protocolcommon.OutcomeSuccess || response.Payload == nil || response.Payload.OperationResult.CommittedRevision == nil {
		return fmt.Errorf("bundled MCP restore failed: %+v", restored.Failure)
	}
	committed := *response.Payload.OperationResult.CommittedRevision
	if committed.RevisionID == selected.RevisionID || committed.DefinitionHash != selected.DefinitionHash || committed.GraphHash != selected.GraphHash {
		return fmt.Errorf("bundled MCP restore did not publish the selected revision state: selected=%s/%s/%s committed=%s/%s/%s", selected.RevisionID, selected.DefinitionHash, selected.GraphHash, committed.RevisionID, committed.DefinitionHash, committed.GraphHash)
	}
	readbackWire, err := runtimeprotocol.EncodeListRevisionsRequestEnvelope(runtimeprotocol.ListRevisionsRequestEnvelope{Operation: runtimeprotocol.ListRevisionsRequestEnvelopeOperationValue, Protocol: protocol, RequestID: "conformance-history-readback", Payload: runtimeprotocol.ListRevisionsInput{Session: opened.Open.Session, MaxItems: "1", MaxOutputBytes: "1048576"}})
	if err != nil {
		return err
	}
	readback := app.MCPCallTool(ctx, mcphost.CallToolRequest{Name: "layerdraw.list_revisions", RequestID: "conformance-history-readback", Arguments: readbackWire, Binding: &mcphost.Binding{DocumentID: opened.ProjectID, RevisionDigest: committed.DefinitionHash, AccessFingerprint: opened.Open.Session.Scope.AccessFingerprint}})
	latest, err := runtimeprotocol.DecodeListRevisionsResponseEnvelope(readback.Content)
	if readback.Failure != nil || err != nil || latest.Payload == nil || len(latest.Payload.Items) != 1 || latest.Payload.Items[0].Revision.RevisionID != committed.RevisionID {
		return errors.New("bundled MCP restore history read-back failed")
	}
	publication, err := app.ProjectPublication(ctx)
	if err != nil || publication.Project == nil || publication.Project.AuthoritativeRevision.RevisionID != committed.RevisionID || publication.Project.AuthoritativeRevision.DefinitionHash != selected.DefinitionHash || publication.Project.AuthoritativeRevision.GraphHash != selected.GraphHash {
		return errors.New("bundled MCP restore project read-back failed")
	}
	return nil
}

func conformanceMCPReview(ctx context.Context, instance *conformanceInstance, opened desktopapp.ProjectOpenResult, commit runtimeprotocol.RuntimeCommitInput, binding *mcphost.Binding) error {
	app := instance.app
	preview := app.Preview(ctx, runtimeprotocol.PreviewOperationsInput{Session: opened.Open.Session, OperationBatch: commit.OperationBatch})
	if preview.Outcome != protocolcommon.OutcomeSuccess {
		return fmt.Errorf("bundled MCP Review preview failed: failure=%+v base=%s/%s", preview.Failure, commit.OperationBatch.BaseRevision.RevisionID, commit.OperationBatch.BaseRevision.DefinitionHash)
	}
	impact := preview.Value.PreviewEvaluation.AuthoringImpact
	create := reviewapp.CreateInput{
		ProposalID: "conformance-review", Proposer: accessprotocol.ActorRef{ActorID: "conformance-reviewer", Kind: "user"},
		ProposeDecision: preview.Value.PreviewEvaluation.AuthoringDecision,
		Preview:         reviewapp.RepreviewResult{CurrentRevision: opened.Open.CommittedRevision, DefinitionHash: preview.Value.DefinitionHash, GraphHash: preview.Value.GraphHash, OperationBatch: commit.OperationBatch, AuthoringProof: preview.Value.AuthoringProof, PreviewDecision: preview.Value.PreviewEvaluation.AuthoringDecision, Evidence: reviewapp.Evidence{SemanticDiff: semantic.SemanticDiff{Digest: impact.SemanticDiffHash, Entries: []semantic.SemanticDiffEntry{}}, SourceDiff: engineprotocol.SourceDiff{Digest: impact.SourceDiffHash, Edits: []engineprotocol.SourceEdit{}}, AuthoringImpact: impact, Diagnostics: []semantic.Diagnostic{}, AffectedUsages: []semantic.StableAddress{}, AffectedRows: []semantic.StableAddress{}, AffectedViews: []semantic.StableAddress{}, RenderPreviews: []reviewapp.ArtifactPreview{}}},
	}
	if create.Preview.CurrentRevision.DocumentID == "" || create.Preview.OperationBatch.DocumentID != create.Preview.CurrentRevision.DocumentID || create.Preview.OperationBatch.BaseRevision != create.Preview.CurrentRevision || create.Preview.Evidence.AuthoringImpact.ImpactDigest == "" || create.ProposeDecision.AuthoringImpactDigest == nil || create.Preview.Evidence.AuthoringImpact.ImpactDigest != *create.ProposeDecision.AuthoringImpactDigest {
		return errors.New("bundled MCP Review evidence is inconsistent")
	}
	if _, err := accessprotocol.EncodeAuthoringDecision(create.ProposeDecision); err != nil {
		return fmt.Errorf("bundled MCP Review decision is invalid: %w", err)
	}
	encoded, _ := json.Marshal(create)
	created := app.MCPCallTool(ctx, mcphost.CallToolRequest{Name: "layerdraw.review_create_proposal", RequestID: "conformance-review-create", Arguments: encoded, Binding: binding})
	var proposal reviewapp.Proposal
	if created.Failure != nil || json.Unmarshal(created.Content, &proposal) != nil || proposal.Status != reviewapp.StatusProposed {
		return fmt.Errorf("bundled MCP Review proposal failed: %+v", created.Failure)
	}
	approval, _ := json.Marshal(map[string]any{"proposal_id": proposal.ID, "generation": proposal.Generation})
	applied := app.MCPCallTool(ctx, mcphost.CallToolRequest{Name: "layerdraw.review_approve_apply", RequestID: "conformance-review-approve", Arguments: approval, Binding: binding})
	if applied.Failure != nil || json.Unmarshal(applied.Content, &proposal) != nil || proposal.Status != reviewapp.StatusApplied {
		return fmt.Errorf("bundled MCP Review approval failed: %+v", applied.Failure)
	}
	return nil
}

func conformanceMCPAgentScope(ctx context.Context, app *desktopapp.Application, opened desktopapp.ProjectOpenResult) error {
	request := desktopapp.MCPConnectRequest{ClientID: "installed-conformance", ProtocolVersion: desktopapp.MCPConnectionProtocolVersion, DocumentID: opened.ProjectID, AgentID: "installed-conformance-agent", Capabilities: []semantic.AuthoringCapability{semantic.AuthoringCapabilityGraphWrite}, Permissions: accesscore.AgentPermissions{Read: true, Propose: true}, ExpiresAt: protocolcommon.Rfc3339Time(time.Now().Add(time.Hour).UTC().Format(time.RFC3339Nano))}
	created := app.CreateMCPConnection(ctx, request)
	if created.Outcome != protocolcommon.OutcomeSuccess || created.Value.Status != desktopapp.MCPConnectionConnected {
		return fmt.Errorf("bundled MCP agent scope failed: %+v", created.Failure)
	}
	called := app.MCPCallConnectionTool(ctx, created.Value.ConnectionID, mcphost.CallToolRequest{Name: "layerdraw.get_capabilities", RequestID: "conformance-agent-scope", Arguments: json.RawMessage(`{}`)})
	if called.Failure != nil || len(called.Content) == 0 {
		return fmt.Errorf("bundled MCP scoped connection failed: %+v", called.Failure)
	}
	revoked := app.RevokeMCPConnection(ctx, created.Value.ConnectionID)
	if revoked.Outcome != protocolcommon.OutcomeSuccess || revoked.Value.Status != desktopapp.MCPConnectionRevoked {
		return fmt.Errorf("bundled MCP revocation failed: %+v", revoked.Failure)
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
	instance, err := newConformanceInstance(ctx, true)
	if err != nil {
		return err
	}
	defer instance.close(context.Background())
	opened, err := instance.openProject(ctx, conformanceAuthoringSource)
	if err != nil {
		return err
	}
	connected := instance.app.ConnectExternal(ctx, desktopapp.ExternalConnectionRequest{ProviderID: "conformance", AccountLabel: "fixture", ScopeLabel: "isolated"})
	if connected.Outcome != protocolcommon.OutcomeSuccess {
		return fmt.Errorf("external provider connection failed: %+v", connected.Failure)
	}
	sync := instance.app.SyncExternal(ctx, opened.Open.Session, desktopapp.ExternalSyncRequest{ConnectionID: connected.Value.ConnectionID, DocumentID: opened.Open.Session.Scope.DocumentID, Revision: opened.Open.CommittedRevision})
	if sync.Outcome != protocolcommon.OutcomeSuccess || !sync.Value.ReconcileNeeded {
		return fmt.Errorf("external sync did not require reconcile: %+v", sync.Failure)
	}
	reconciled := instance.app.ReconcileExternal(ctx, opened.Open.Session, desktopapp.ExternalReconcileRequest{ConnectionID: connected.Value.ConnectionID, DocumentID: opened.Open.Session.Scope.DocumentID, Resolution: "accept_remote"})
	if reconciled.Outcome != protocolcommon.OutcomeSuccess || !reconciled.Value.Converged {
		return fmt.Errorf("external reconcile did not converge: %+v", reconciled.Failure)
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

relation_type calls "Calls" data_flow {
  allow_self false
  duplicate_policy allow
  from caller types [service] layers [app]
  to callee types [service] layers [app]
  cardinality {
    to_per_from 0..*
    from_per_to 0..*
  }
  label "calls"
}

entities service @app {
  alpha "Service A"
  beta "Service B"
}

relations calls {
  alpha_beta: alpha -> beta
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
