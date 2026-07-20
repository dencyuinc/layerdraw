// SPDX-License-Identifier: LicenseRef-LayerDraw-1.0

package host

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/dencyuinc/layerdraw/gen/go/engineprotocol"
	"github.com/dencyuinc/layerdraw/gen/go/protocolcommon"
	"github.com/dencyuinc/layerdraw/gen/go/runtimeprotocol"
	engineendpoint "github.com/dencyuinc/layerdraw/internal/engine/endpoint"
	"github.com/dencyuinc/layerdraw/internal/localdocument"
	layerruntime "github.com/dencyuinc/layerdraw/internal/runtime"
	"github.com/dencyuinc/layerdraw/internal/runtime/port"
)

type searchSurfaceStub struct{}

func (searchSurfaceStub) Capabilities(context.Context) (layerruntime.SearchCapabilityManifest, error) {
	return layerruntime.SearchCapabilityManifest{QueryAvailable: true, SearchAvailable: true, AnalysisAvailable: true}, nil
}
func (searchSurfaceStub) Search(context.Context, layerruntime.SearchRequest) ([]byte, error) {
	return []byte(`{"surface":"search"}`), nil
}
func (searchSurfaceStub) ExecuteQuery(context.Context, port.BoundExecutionRequest) ([]byte, error) {
	return []byte(`{"surface":"query"}`), nil
}
func (searchSurfaceStub) ExecuteAnalysis(context.Context, port.BoundExecutionRequest) ([]byte, error) {
	return []byte(`{"surface":"analysis"}`), nil
}

type emptyBlobSource struct {
	definitions []engineendpoint.BlobDefinition
	err         error
}

type captureBlobSink struct{ blobs []engineendpoint.OutputBlob }

func (s *captureBlobSink) Publish(_ context.Context, blobs []engineendpoint.OutputBlob) error {
	s.blobs = append([]engineendpoint.OutputBlob(nil), blobs...)
	return nil
}

func (s emptyBlobSource) Definitions(context.Context) ([]engineendpoint.BlobDefinition, error) {
	return s.definitions, s.err
}

func TestHandshakeAdvertisesOnlyWiredRuntimeAndEngineOperations(t *testing.T) {
	digest := protocolcommon.Digest("sha256:" + strings.Repeat("a", 64))
	local, err := localdocument.New(localdocument.Config{
		Root: t.TempDir(), ReleaseVersion: "1.0.0", EndpointInstanceID: "host-test",
		ReleaseManifestDigest: digest,
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = local.Shutdown(context.Background()) })
	engineFacade, err := engineendpoint.NewHostEngineFacade("1.0.0", "unknown", string(digest), "host-test", "stdio")
	if err != nil {
		t.Fatal(err)
	}
	composite, err := New(Config{
		LocalHost: local, Engine: engineFacade, Search: searchSurfaceStub{},
	})
	if err != nil {
		t.Fatal(err)
	}
	required := make([]protocolcommon.CapabilityID, 0, len(runtimeOperations)+1)
	required = append(required, "runtime.handshake")
	for _, operation := range runtimeOperations {
		required = append(required, protocolcommon.CapabilityID(operation))
	}
	required = append(required, OperationSearch, OperationExecuteQuery, OperationAnalyzeGraph)
	request := runtimeprotocol.RuntimeHandshakeRequestEnvelope{
		Operation: runtimeprotocol.RuntimeHandshakeRequestEnvelopeOperationValue,
		Protocol:  runtimeprotocol.RuntimeProtocolRef{Name: runtimeprotocol.RuntimeProtocolRefNameValue, Version: "1.0"},
		RequestID: "handshake",
		Payload: runtimeprotocol.RuntimeHandshakeRequest{
			ClientRelease:        "1.0.0",
			Protocols:            []protocolcommon.ProtocolOffer{{Name: "runtime", SupportedRange: "1.0..1.0", Versions: []protocolcommon.ProtocolVersionBinding{{Version: "1.0", SchemaDigest: protocolcommon.Digest(runtimeprotocol.SchemaDigest)}}}},
			RequiredCapabilities: required, OptionalCapabilities: []protocolcommon.CapabilityID{},
		},
	}
	control, err := runtimeprotocol.EncodeRuntimeHandshakeRequestEnvelope(request)
	if err != nil {
		t.Fatal(err)
	}
	response, accepted, err := composite.Handshake(context.Background(), control)
	if err != nil || !accepted {
		t.Fatalf("accepted=%v err=%v", accepted, err)
	}
	decoded, err := runtimeprotocol.DecodeRuntimeHandshakeResponseEnvelope(response.Control)
	if err != nil || decoded.Payload == nil {
		t.Fatalf("response=%s err=%v", response.Control, err)
	}
	operations := decoded.Payload.CapabilityManifest.Operations
	wired := append([]string{"runtime.handshake", "engine.compile", "engine.open_document", "engine.execute_query", "engine.materialize_view", "engine.plan_export"}, runtimeOperations...)
	wired = append(wired, OperationSearch, OperationExecuteQuery, OperationAnalyzeGraph)
	for _, operation := range wired {
		if capability, ok := operations[operation]; !ok || !capability.Enabled {
			t.Errorf("wired operation %q missing", operation)
		}
	}
	if composite.SearchSurface() == nil {
		t.Fatal("Desktop/MCP shared search surface not retained")
	}
	for operation := range operations {
		lower := strings.ToLower(operation)
		for _, excluded := range []string{"registry", "realtime", "mcp", "remote_storage", "native_export", "organization", "http", "wails"} {
			if strings.Contains(lower, excluded) {
				t.Errorf("out-of-scope operation advertised: %s", operation)
			}
		}
	}
}

func TestRuntimeTerminalResponsesCoverEveryAdvertisedOperation(t *testing.T) {
	composite := newTestEndpoint(t)
	operations := append([]string{"runtime.handshake"}, runtimeOperations...)
	for _, operation := range operations {
		t.Run(operation, func(t *testing.T) {
			response, err := composite.runtimeResponse(operation, "terminal_request", nil, protocolcommon.OutcomeCancelled, failure("runtime.cancelled", protocolcommon.ProtocolFailureCategoryCancelled))
			if err != nil {
				t.Fatal(err)
			}
			if response.Operation != operation || response.RequestID != "terminal_request" || len(response.Control) == 0 {
				t.Fatalf("unexpected terminal response: %+v", response)
			}
			cancelled, err := composite.CancellationResponse(operation, "cancel_request")
			if err != nil || cancelled.Outcome != protocolcommon.OutcomeCancelled {
				t.Fatalf("cancelled=%+v err=%v", cancelled, err)
			}
			transport, err := composite.TransportResponse(operation, "transport_request")
			if err != nil || transport.Outcome != protocolcommon.OutcomeFailed {
				t.Fatalf("transport=%+v err=%v", transport, err)
			}
		})
	}
	if _, err := composite.runtimeResponse("runtime.unsupported", "request", nil, protocolcommon.OutcomeFailed, nil); err == nil {
		t.Fatal("unsupported Runtime response succeeded")
	}
	if composite.Supports("runtime.unsupported") {
		t.Fatal("unsupported Runtime operation reported as supported")
	}
	if !composite.Supports("engine.compile") {
		t.Fatal("wired Engine operation reported as unsupported")
	}
}

func TestSearchOperationsDispatchThroughTheWiredSurface(t *testing.T) {
	composite := newSearchTestEndpoint(t)
	session, snapshot, accessDigest := openSearchTestSession(t, composite)
	protocol := runtimeprotocol.RuntimeProtocolRef{Name: runtimeprotocol.RuntimeProtocolRefNameValue, Version: "1.0"}
	requests := []struct {
		operation string
		payload   any
		want      string
	}{
		{OperationSearch, layerruntime.SearchRequest{Session: &session, Snapshot: snapshot, AccessProjectionDigest: accessDigest}, `"surface":"search"`},
		{OperationExecuteQuery, port.BoundExecutionRequest{Session: &session, Snapshot: snapshot, AccessProjectionDigest: accessDigest}, `"surface":"query"`},
		{OperationAnalyzeGraph, port.BoundExecutionRequest{Session: &session, Snapshot: snapshot, AccessProjectionDigest: accessDigest}, `"surface":"analysis"`},
	}
	for _, testCase := range requests {
		t.Run(testCase.operation, func(t *testing.T) {
			if !composite.Supports(testCase.operation) {
				t.Fatal("wired Search operation is not supported")
			}
			control, err := json.Marshal(map[string]any{"operation": testCase.operation, "protocol": protocol, "request_id": "search_request", "payload": testCase.payload})
			if err != nil {
				t.Fatal(err)
			}
			response := executeRuntimeControl(t, composite, testCase.operation, control, emptyBlobSource{})
			if response.Outcome != protocolcommon.OutcomeSuccess || !strings.Contains(string(response.Control), testCase.want) {
				t.Fatalf("response=%s", response.Control)
			}
			if cancelled, err := composite.CancellationResponse(testCase.operation, "cancel"); err != nil || cancelled.Outcome != protocolcommon.OutcomeCancelled {
				t.Fatalf("cancelled=%+v err=%v", cancelled, err)
			}
		})
	}
	if _, _, err := composite.Prepare(context.Background(), OperationExecuteQuery, []byte(`{"operation":"runtime.execute_query","protocol":{"name":"runtime","version":"1.0"},"request_id":"x","payload":{},"unknown":true}`)); err == nil {
		t.Fatal("open Search envelope accepted unknown fields")
	}
}

func TestWailsAndMCPConsumersReceiveIdenticalEngineSearchResultBytes(t *testing.T) {
	composite := newSearchTestEndpoint(t)
	session, snapshot, accessDigest := openSearchTestSession(t, composite)
	request := layerruntime.SearchRequest{Session: &session, Snapshot: snapshot, AccessProjectionDigest: accessDigest}
	// Wails links the in-process consumer surface directly.
	wailsResult, err := composite.SearchSurface().Search(context.Background(), request)
	if err != nil {
		t.Fatal(err)
	}
	// MCP maps its tool envelope through the same Endpoint operation.
	control, _ := json.Marshal(searchOperationRequest[layerruntime.SearchRequest]{Operation: OperationSearch, Protocol: runtimeprotocol.RuntimeProtocolRef{Name: runtimeprotocol.RuntimeProtocolRefNameValue, Version: "1.0"}, RequestID: "mcp-search", Payload: request})
	response := executeRuntimeControl(t, composite, OperationSearch, control, emptyBlobSource{})
	var decoded searchOperationResponse
	if err := json.Unmarshal(response.Control, &decoded); err != nil {
		t.Fatal(err)
	}
	if string(decoded.Payload) != string(wailsResult) {
		t.Fatalf("Wails=%s MCP=%s", wailsResult, decoded.Payload)
	}
}

func openSearchTestSession(t *testing.T, endpoint *Endpoint) (runtimeprotocol.RuntimeSessionRef, port.DocumentSnapshotRef, string) {
	t.Helper()
	project := t.TempDir()
	if err := os.WriteFile(filepath.Join(project, "document.ldl"), []byte("project p \"P\" {}\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	opened, err := endpoint.host.OpenProject(context.Background(), localdocument.OpenProjectInput{Root: project, EntryPath: "document.ldl"})
	if err != nil {
		t.Fatal(err)
	}
	revision := opened.Session.Open.CommittedRevision
	snapshot := port.DocumentSnapshotRef{Kind: port.SnapshotHostRevision, HostDocumentID: string(revision.DocumentID), CommittedRevision: string(revision.RevisionID), DefinitionHash: string(revision.DefinitionHash)}
	return opened.Session.Open.Session, snapshot, string(opened.Session.Open.AccessSummary.AccessFingerprint)
}

func TestSearchSessionAuthorityRejectsForgedActorRevisionAndFingerprint(t *testing.T) {
	endpoint := newSearchTestEndpoint(t)
	session, snapshot, digest := openSearchTestSession(t, endpoint)
	if err := endpoint.authorizeSearchSession(context.Background(), &session, snapshot, digest); err != nil {
		t.Fatal(err)
	}
	foreign := session
	foreign.RuntimeSessionID = "foreign-session"
	if err := endpoint.authorizeSearchSession(context.Background(), &foreign, snapshot, digest); !errors.Is(err, layerruntime.ErrSearchIndexStale) {
		t.Fatalf("foreign session err=%v", err)
	}
	changed := snapshot
	changed.CommittedRevision = "foreign-revision"
	if err := endpoint.authorizeSearchSession(context.Background(), &session, changed, digest); !errors.Is(err, layerruntime.ErrSearchIndexStale) {
		t.Fatalf("foreign revision err=%v", err)
	}
	if err := endpoint.authorizeSearchSession(context.Background(), &session, snapshot, "sha256:forged"); !errors.Is(err, layerruntime.ErrSearchIndexStale) {
		t.Fatalf("forged fingerprint err=%v", err)
	}
}

func TestRuntimePlanResourceAccountingAndFailurePaths(t *testing.T) {
	digest := protocolcommon.Digest("sha256:" + strings.Repeat("b", 64))
	ref := protocolcommon.BlobRef{BlobID: "asset/test.bin", Digest: digest, Lifetime: protocolcommon.BlobLifetimeRequest, MediaType: "application/octet-stream", Size: "7"}
	plan := &runtimePlan{requirements: []engineendpoint.BlobRequirement{{Ref: ref, References: 1}}}
	if got := plan.BlobRequirements(); len(got) != 1 || got[0].Ref.BlobID != ref.BlobID {
		t.Fatalf("requirements=%+v", got)
	}
	if budget := plan.AdmissionBudget(); budget.RequiredBlobCount != 1 || budget.RequiredBlobBytes != 7 {
		t.Fatalf("budget=%+v", budget)
	}
	plan.Abort()
	if _, err := plan.Execute(context.Background(), nil, nil); err == nil {
		t.Fatal("Runtime plan compiled through the Engine-only entrypoint")
	}
	if _, err := plan.ExecuteDispatch(context.Background(), emptyBlobSource{err: errors.New("source failed")}, nil); err == nil {
		t.Fatal("blob source failure was ignored")
	}
	plan.run = func(context.Context, map[string][]byte) (any, error) { return nil, nil }
	if _, err := plan.ExecuteDispatch(context.Background(), emptyBlobSource{}, nil); err == nil {
		t.Fatal("missing endpoint context was ignored")
	}
	plan.endpoint = newTestEndpoint(t)
	plan.operation, plan.requestID = OperationRecover, "recover_request"
	plan.run = func(context.Context, map[string][]byte) (any, error) { return nil, errors.New("operation failed") }
	response, err := plan.ExecuteDispatch(context.Background(), emptyBlobSource{}, nil)
	if err != nil || response.Outcome != protocolcommon.OutcomeFailed {
		t.Fatalf("response=%+v err=%v", response, err)
	}
}

func TestConstructorsRejectIncompleteComposition(t *testing.T) {
	if _, err := New(Config{}); err == nil {
		t.Fatal("incomplete composition succeeded")
	}
	if _, _, err := NewLocal(LocalConfig{}); err == nil {
		t.Fatal("invalid local composition succeeded")
	}
	if valueOr(nil, "fallback") != "fallback" {
		t.Fatal("nil optional value did not use fallback")
	}
	value := "configured"
	if valueOr(&value, "fallback") != value {
		t.Fatal("configured optional value was replaced")
	}
	digest := "sha256:" + strings.Repeat("c", 64)
	endpoint, shutdown, err := NewLocal(LocalConfig{Root: t.TempDir(), ReleaseVersion: "1.0.0", SourceRevision: "unknown", ReleaseManifestDigest: digest, EndpointInstanceID: "local-constructor", TransportID: "stdio"})
	if err != nil || endpoint == nil || shutdown == nil {
		t.Fatalf("endpoint=%v shutdown_missing=%v err=%v", endpoint, shutdown == nil, err)
	}
	if err := shutdown(context.Background()); err != nil {
		t.Fatal(err)
	}
}

func TestHandshakeAndPrepareRejectInvalidConnectionInputs(t *testing.T) {
	if _, accepted, err := newTestEndpoint(t).Handshake(context.Background(), []byte("{}")); err == nil || accepted {
		t.Fatalf("malformed handshake accepted=%v err=%v", accepted, err)
	}
	missing := newTestEndpoint(t)
	request := runtimeprotocol.RuntimeHandshakeRequestEnvelope{
		Operation: runtimeprotocol.RuntimeHandshakeRequestEnvelopeOperationValue,
		Protocol:  runtimeprotocol.RuntimeProtocolRef{Name: runtimeprotocol.RuntimeProtocolRefNameValue, Version: "1.0"}, RequestID: "missing_request",
		Payload: runtimeprotocol.RuntimeHandshakeRequest{ClientRelease: "1.0.0", Protocols: []protocolcommon.ProtocolOffer{{Name: "runtime", SupportedRange: "1.0..1.0", Versions: []protocolcommon.ProtocolVersionBinding{{Version: "1.0", SchemaDigest: protocolcommon.Digest(runtimeprotocol.SchemaDigest)}}}}, RequiredCapabilities: []protocolcommon.CapabilityID{"runtime.unsupported"}, OptionalCapabilities: []protocolcommon.CapabilityID{}},
	}
	control, err := runtimeprotocol.EncodeRuntimeHandshakeRequestEnvelope(request)
	if err != nil {
		t.Fatal(err)
	}
	if response, accepted, err := missing.Handshake(context.Background(), control); err != nil || accepted || response.Outcome != protocolcommon.OutcomeRejected {
		t.Fatalf("missing accepted=%v response=%+v err=%v", accepted, response, err)
	}
	connected := newTestEndpoint(t)
	handshakeTestEndpoint(t, connected)
	if response, accepted, err := connected.Handshake(context.Background(), control); err != nil || accepted || response.Outcome != protocolcommon.OutcomeRejected {
		t.Fatalf("repeat accepted=%v response=%+v err=%v", accepted, response, err)
	}
	for _, operation := range runtimeOperations {
		t.Run(operation, func(t *testing.T) {
			if plan, terminal, err := connected.Prepare(context.Background(), operation, []byte("{}")); err == nil || plan != nil || terminal != nil {
				t.Fatalf("plan=%v terminal=%v err=%v", plan, terminal, err)
			}
		})
	}
	if _, _, err := connected.Prepare(context.Background(), "runtime.unsupported", []byte("{}")); err == nil {
		t.Fatal("unsupported Runtime operation prepared")
	}
	if _, _, err := connected.Prepare(context.Background(), "engine.compile", []byte("{}")); err == nil {
		t.Fatal("malformed Engine operation prepared")
	}
	if response, err := connected.CancellationResponse("engine.compile", "engine_cancel"); err != nil || response.Outcome != protocolcommon.OutcomeCancelled {
		t.Fatalf("engine cancellation=%+v err=%v", response, err)
	}
	if response, err := connected.TransportResponse("engine.compile", "engine_transport"); err != nil || response.Outcome != protocolcommon.OutcomeFailed {
		t.Fatalf("engine transport=%+v err=%v", response, err)
	}
}

func TestRuntimeDispatchLifecycle(t *testing.T) {
	composite := newTestEndpoint(t)
	handshakeTestEndpoint(t, composite)
	compileSource := []byte("project p \"P\" {}\n")
	compileDigest := sha256.Sum256(compileSource)
	compileRef := protocolcommon.BlobRef{BlobID: "source", Digest: protocolcommon.Digest("sha256:" + fmtHex(compileDigest[:])), Lifetime: protocolcommon.BlobLifetimeRequest, MediaType: "text/plain; charset=utf-8", Size: protocolcommon.CanonicalUint64(strconv.Itoa(len(compileSource)))}
	compileRequest := engineprotocol.CompileRequestEnvelope{
		Operation: engineprotocol.CompileRequestEnvelopeOperationValue,
		Protocol:  engineprotocol.EngineProtocolRef{Name: engineprotocol.EngineProtocolRefNameValue, Version: engineprotocol.EngineProtocolRefVersionValue},
		RequestID: "composed_compile_request",
		Payload:   engineprotocol.CompileInput{EntryPath: "document.ldl", InstalledPackTree: []engineprotocol.SourceFileInput{}, Mode: engineprotocol.CompileModeProject, ProjectSourceTree: []engineprotocol.SourceFileInput{{Path: "document.ldl", Blob: compileRef}}, ReferencedAssets: []engineprotocol.AssetInput{}, ResolvedDependencies: engineprotocol.ResolvedDependencies{Format: engineprotocol.ResolvedDependenciesFormatValue, FormatVersion: 1, Language: 1, Installs: []engineprotocol.ResolvedPack{}}, ResourceLimits: engineprotocol.ResourceLimits{}},
	}
	compileControl, err := engineprotocol.EncodeCompileRequestEnvelope(compileRequest)
	if err != nil {
		t.Fatal(err)
	}
	compilePlan, terminal, err := composite.Prepare(context.Background(), "engine.compile", compileControl)
	if err != nil || terminal != nil || compilePlan == nil {
		t.Fatalf("compile prepare plan=%v terminal=%+v err=%v", compilePlan, terminal, err)
	}
	compileReleased := false
	compileBlobs := emptyBlobSource{definitions: []engineendpoint.BlobDefinition{{BlobID: compileRef.BlobID, Owned: &engineendpoint.OwnedBlob{Bytes: compileSource, Release: func() { compileReleased = true }}}}}
	compileSink := &captureBlobSink{}
	compileResponse, err := compilePlan.ExecuteDispatch(context.Background(), compileBlobs, compileSink)
	if err != nil || compileResponse.Outcome != protocolcommon.OutcomeSuccess || !compileReleased || len(compileSink.blobs) == 0 {
		t.Fatalf("composed compile response=%+v released=%v output_blobs=%d err=%v", compileResponse, compileReleased, len(compileSink.blobs), err)
	}
	decodedCompile, err := engineprotocol.DecodeCompileResponseEnvelope(compileResponse.Control)
	if err != nil || decodedCompile.Payload == nil {
		t.Fatalf("composed compile control=%s err=%v", compileResponse.Control, err)
	}
	project := t.TempDir()
	if err := os.WriteFile(filepath.Join(project, "document.ldl"), []byte("project p \"P\" {}\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	protocol := runtimeprotocol.RuntimeProtocolRef{Name: runtimeprotocol.RuntimeProtocolRefNameValue, Version: "1.0"}
	openControl, err := runtimeprotocol.EncodeOpenDocumentRequestEnvelope(runtimeprotocol.OpenDocumentRequestEnvelope{
		Operation: runtimeprotocol.OpenDocumentRequestEnvelopeOperationValue, Protocol: protocol, RequestID: "open_request",
		Payload: runtimeprotocol.OpenRuntimeDocumentInput{DocumentID: "bootstrap", LocalSource: &runtimeprotocol.LocalDocumentSource{Kind: "project", Path: project}},
	})
	if err != nil {
		t.Fatal(err)
	}
	openResponse := executeRuntimeControl(t, composite, "runtime.open_document", openControl, emptyBlobSource{})
	opened, err := runtimeprotocol.DecodeOpenDocumentResponseEnvelope(openResponse.Control)
	if err != nil || opened.Payload == nil {
		t.Fatalf("open=%s err=%v", openResponse.Control, err)
	}
	session := opened.Payload.Session

	inspectControl, err := runtimeprotocol.EncodeInspectDocumentRequestEnvelope(runtimeprotocol.InspectDocumentRequestEnvelope{
		Operation: runtimeprotocol.InspectDocumentRequestEnvelopeOperationValue, Protocol: protocol, RequestID: "inspect_request",
		Payload: runtimeprotocol.RuntimeSessionInput{Session: session},
	})
	if err != nil {
		t.Fatal(err)
	}
	if response := executeRuntimeControl(t, composite, OperationInspect, inspectControl, emptyBlobSource{}); response.Outcome != protocolcommon.OutcomeSuccess {
		t.Fatalf("inspect=%s", response.Control)
	}

	stateControl, err := runtimeprotocol.EncodeStateSnapshotRequestEnvelope(runtimeprotocol.StateSnapshotRequestEnvelope{
		Operation: runtimeprotocol.StateSnapshotRequestEnvelopeOperationValue, Protocol: protocol, RequestID: "state_request",
		Payload: runtimeprotocol.RuntimeSessionInput{Session: session},
	})
	if err != nil {
		t.Fatal(err)
	}
	stateResponse := executeRuntimeControl(t, composite, OperationStateSnapshot, stateControl, emptyBlobSource{})
	state, err := runtimeprotocol.DecodeStateSnapshotResponseEnvelope(stateResponse.Control)
	if err != nil || state.Payload == nil || state.Payload.StateInput.Snapshot == nil || state.Payload.StateInput.SnapshotHash == nil {
		t.Fatalf("state=%s err=%v", stateResponse.Control, err)
	}

	historyControl, err := runtimeprotocol.EncodeListRevisionsRequestEnvelope(runtimeprotocol.ListRevisionsRequestEnvelope{
		Operation: runtimeprotocol.ListRevisionsRequestEnvelopeOperationValue, Protocol: protocol, RequestID: "history_request",
		Payload: runtimeprotocol.ListRevisionsInput{Session: session, MaxItems: "20", MaxOutputBytes: "1048576"},
	})
	if err != nil {
		t.Fatal(err)
	}
	historyResponse := executeRuntimeControl(t, composite, "runtime.list_revisions", historyControl, emptyBlobSource{})
	history, err := runtimeprotocol.DecodeListRevisionsResponseEnvelope(historyResponse.Control)
	if err != nil || history.Payload == nil || len(history.Payload.Items) == 0 {
		t.Fatalf("history=%s err=%v", historyResponse.Control, err)
	}

	restoreControl, err := runtimeprotocol.EncodeRestorePreviewRequestEnvelope(runtimeprotocol.RestorePreviewRequestEnvelope{
		Operation: runtimeprotocol.RestorePreviewRequestEnvelopeOperationValue, Protocol: protocol, RequestID: "restore_request",
		Payload: runtimeprotocol.RestorePreviewInput{Session: session, RevisionID: history.Payload.Items[0].Revision.RevisionID},
	})
	if err != nil {
		t.Fatal(err)
	}
	executeRuntimeControl(t, composite, OperationRestore, restoreControl, emptyBlobSource{})

	autosaveControl, err := runtimeprotocol.EncodeAutosaveControlRequestEnvelope(runtimeprotocol.AutosaveControlRequestEnvelope{
		Operation: runtimeprotocol.AutosaveControlRequestEnvelopeOperationValue, Protocol: protocol, RequestID: "autosave_request",
		Payload: runtimeprotocol.AutosaveControlInput{Session: session, Action: runtimeprotocol.AutosaveActionCancel},
	})
	if err != nil {
		t.Fatal(err)
	}
	if response := executeRuntimeControl(t, composite, OperationAutosave, autosaveControl, emptyBlobSource{}); response.Outcome != protocolcommon.OutcomeSuccess {
		t.Fatalf("autosave=%s", response.Control)
	}

	assetBytes := []byte("host asset")
	assetDigest := sha256.Sum256(assetBytes)
	assetRef := protocolcommon.BlobRef{BlobID: "asset/test.bin", Digest: protocolcommon.Digest("sha256:" + fmtHex(assetDigest[:])), Lifetime: protocolcommon.BlobLifetimeRequest, MediaType: "application/octet-stream", Size: protocolcommon.CanonicalUint64("10")}
	assetControl, err := runtimeprotocol.EncodeStageAssetRequestEnvelope(runtimeprotocol.StageAssetRequestEnvelope{
		Operation: runtimeprotocol.StageAssetRequestEnvelopeOperationValue, Protocol: protocol, RequestID: "asset_request",
		Payload: runtimeprotocol.StageAssetInput{Session: session, ContentBlob: assetRef},
	})
	if err != nil {
		t.Fatal(err)
	}
	released := false
	assetSource := emptyBlobSource{definitions: []engineendpoint.BlobDefinition{{BlobID: assetRef.BlobID, Owned: &engineendpoint.OwnedBlob{Bytes: assetBytes, Release: func() { released = true }}}}}
	assetResponse := executeRuntimeControl(t, composite, OperationAsset, assetControl, assetSource)
	if assetResponse.Outcome != protocolcommon.OutcomeSuccess || !released {
		t.Fatalf("asset=%s released=%v", assetResponse.Control, released)
	}

	recoverControl, err := runtimeprotocol.EncodeRecoverOperationsRequestEnvelope(runtimeprotocol.RecoverOperationsRequestEnvelope{
		Operation: runtimeprotocol.RecoverOperationsRequestEnvelopeOperationValue, Protocol: protocol, RequestID: "recover_request",
		Payload: runtimeprotocol.RecoverOperationsInput{DocumentID: opened.Payload.CommittedRevision.DocumentID},
	})
	if err != nil {
		t.Fatal(err)
	}
	executeRuntimeControl(t, composite, OperationRecover, recoverControl, emptyBlobSource{})

	closeControl, err := runtimeprotocol.EncodeCloseRuntimeDocumentRequestEnvelope(runtimeprotocol.CloseRuntimeDocumentRequestEnvelope{
		Operation: runtimeprotocol.CloseRuntimeDocumentRequestEnvelopeOperationValue, Protocol: protocol, RequestID: "close_request",
		Payload: runtimeprotocol.RuntimeSessionInput{Session: session},
	})
	if err != nil {
		t.Fatal(err)
	}
	if response := executeRuntimeControl(t, composite, OperationClose, closeControl, emptyBlobSource{}); response.Outcome != protocolcommon.OutcomeSuccess {
		t.Fatalf("close=%s", response.Control)
	}
}

func executeRuntimeControl(t *testing.T, endpoint *Endpoint, operation string, control []byte, source engineendpoint.BlobSource) engineendpoint.DispatchResponse {
	t.Helper()
	plan, terminal, err := endpoint.Prepare(context.Background(), operation, control)
	if err != nil || terminal != nil || plan == nil {
		t.Fatalf("prepare %s: terminal=%+v err=%v", operation, terminal, err)
	}
	response, err := plan.ExecuteDispatch(context.Background(), source, nil)
	if err != nil {
		t.Fatalf("execute %s: %v", operation, err)
	}
	return response
}

func handshakeTestEndpoint(t *testing.T, endpoint *Endpoint) {
	t.Helper()
	request := runtimeprotocol.RuntimeHandshakeRequestEnvelope{
		Operation: runtimeprotocol.RuntimeHandshakeRequestEnvelopeOperationValue,
		Protocol:  runtimeprotocol.RuntimeProtocolRef{Name: runtimeprotocol.RuntimeProtocolRefNameValue, Version: "1.0"},
		RequestID: "handshake_request",
		Payload:   runtimeprotocol.RuntimeHandshakeRequest{ClientRelease: "1.0.0", Protocols: []protocolcommon.ProtocolOffer{{Name: "runtime", SupportedRange: "1.0..1.0", Versions: []protocolcommon.ProtocolVersionBinding{{Version: "1.0", SchemaDigest: protocolcommon.Digest(runtimeprotocol.SchemaDigest)}}}}, RequiredCapabilities: []protocolcommon.CapabilityID{}, OptionalCapabilities: []protocolcommon.CapabilityID{"runtime.unsupported"}},
	}
	control, err := runtimeprotocol.EncodeRuntimeHandshakeRequestEnvelope(request)
	if err != nil {
		t.Fatal(err)
	}
	if response, accepted, err := endpoint.Handshake(context.Background(), control); err != nil || !accepted || response.Outcome != protocolcommon.OutcomeSuccess {
		t.Fatalf("handshake accepted=%v response=%+v err=%v", accepted, response, err)
	}
}

func fmtHex(value []byte) string {
	const digits = "0123456789abcdef"
	encoded := make([]byte, len(value)*2)
	for index, item := range value {
		encoded[index*2], encoded[index*2+1] = digits[item>>4], digits[item&15]
	}
	return string(encoded)
}

func TestRuntimeSearchFailuresPreserveTypedParityCodes(t *testing.T) {
	for _, test := range []struct {
		err       error
		code      string
		outcome   protocolcommon.Outcome
		retryable bool
	}{
		{layerruntime.ErrSearchEmbeddingUnavailable, "search.embedding_unavailable", protocolcommon.OutcomeFailed, true},
		{layerruntime.ErrSearchEmbeddingProfile, "search.embedding_profile_mismatch", protocolcommon.OutcomeFailed, false},
		{layerruntime.ErrSearchIndexStale, "search.index_stale", protocolcommon.OutcomeFailed, false},
		{layerruntime.ErrSearchInvalidCursor, "search.cursor_invalid", protocolcommon.OutcomeFailed, false},
		{layerruntime.ErrSearchCancelled, "search.cancelled", protocolcommon.OutcomeCancelled, false},
		{layerruntime.ErrSearchIndexNotReady, "search.index_not_ready", protocolcommon.OutcomeFailed, true},
		{layerruntime.ErrSearchCapabilityMissing, "search.capability_missing", protocolcommon.OutcomeFailed, false},
		{layerruntime.ErrSearchInvalidRequest, "search.invalid_request", protocolcommon.OutcomeFailed, false},
		{layerruntime.ErrAnalysisInvalidScope, "analysis.invalid_scope", protocolcommon.OutcomeFailed, false},
		{layerruntime.ErrSearchBackendFailed, "search.backend_failed", protocolcommon.OutcomeFailed, true},
		{errors.New("unknown"), "runtime.operation_failed", protocolcommon.OutcomeFailed, true},
	} {
		outcome, failure := runtimeOperationFailure(test.err)
		if outcome != test.outcome || failure.Code != test.code || failure.Retryable != test.retryable {
			t.Fatalf("err=%v outcome=%s failure=%+v", test.err, outcome, failure)
		}
	}
}

func TestSearchSessionAuthorityRequiresHostAndSession(t *testing.T) {
	endpoint := &Endpoint{}
	if err := endpoint.authorizeSearchSession(context.Background(), nil, port.DocumentSnapshotRef{}, ""); !errors.Is(err, layerruntime.ErrSearchInvalidRequest) {
		t.Fatalf("nil authority err=%v", err)
	}
	endpoint = newSearchTestEndpoint(t)
	if err := endpoint.authorizeSearchSession(context.Background(), nil, port.DocumentSnapshotRef{}, ""); !errors.Is(err, layerruntime.ErrSearchInvalidRequest) {
		t.Fatalf("nil session err=%v", err)
	}
}

type searchLifecycleRecorder struct {
	calls int
	err   error
}

func (r *searchLifecycleRecorder) RefreshSearchIndex(_ context.Context, _ *localdocument.Session) error {
	r.calls++
	return r.err
}

func TestSearchLifecycleRefreshUsesOnlyTrackedSessions(t *testing.T) {
	endpoint := newSearchTestEndpoint(t)
	if err := endpoint.refreshSearchIndex(context.Background(), nil); err != nil {
		t.Fatal(err)
	}
	ref, _, _ := openSearchTestSession(t, endpoint)
	recorder := &searchLifecycleRecorder{}
	endpoint.searchLifecycle = recorder
	if err := endpoint.refreshSearchSession(context.Background(), ref); err != nil || recorder.calls != 1 {
		t.Fatalf("refresh calls=%d err=%v", recorder.calls, err)
	}
	missing := ref
	missing.RuntimeSessionID = "missing"
	if err := endpoint.refreshSearchSession(context.Background(), missing); err == nil {
		t.Fatal("missing lifecycle session was accepted")
	}
	recorder.err = errors.New("index failed")
	if err := endpoint.refreshSearchSession(context.Background(), ref); err == nil || recorder.calls != 2 {
		t.Fatalf("refresh error calls=%d err=%v", recorder.calls, err)
	}
}

func TestOpenFailsClosedWhenSearchLifecycleRefreshFails(t *testing.T) {
	endpoint := newSearchTestEndpoint(t)
	endpoint.searchLifecycle = &searchLifecycleRecorder{err: errors.New("index failed")}
	handshakeTestEndpoint(t, endpoint)
	project := t.TempDir()
	if err := os.WriteFile(filepath.Join(project, "document.ldl"), []byte("project p \"P\" {}\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	control, err := runtimeprotocol.EncodeOpenDocumentRequestEnvelope(runtimeprotocol.OpenDocumentRequestEnvelope{
		Operation: runtimeprotocol.OpenDocumentRequestEnvelopeOperationValue,
		Protocol:  runtimeprotocol.RuntimeProtocolRef{Name: runtimeprotocol.RuntimeProtocolRefNameValue, Version: "1.0"},
		RequestID: "open_search_failure",
		Payload:   runtimeprotocol.OpenRuntimeDocumentInput{DocumentID: "bootstrap", LocalSource: &runtimeprotocol.LocalDocumentSource{Kind: "project", Path: project}},
	})
	if err != nil {
		t.Fatal(err)
	}
	response := executeRuntimeControl(t, endpoint, "runtime.open_document", control, emptyBlobSource{})
	if response.Outcome != protocolcommon.OutcomeFailed || response.Failure == nil {
		t.Fatalf("response=%+v", response)
	}
}

func TestSearchEnvelopeAndResponseFailClosed(t *testing.T) {
	valid := []byte(`{"operation":"runtime.search","protocol":{"name":"runtime","version":"1.0"},"request_id":"r","payload":{}}`)
	if _, err := decodeSearchOperationRequest[layerruntime.SearchRequest](append(valid, []byte(" {}")...), OperationSearch); err == nil {
		t.Fatal("trailing search request was accepted")
	}
	if _, err := decodeSearchOperationRequest[layerruntime.SearchRequest](valid, OperationExecuteQuery); err == nil {
		t.Fatal("mismatched search request was accepted")
	}
	endpoint := newSearchTestEndpoint(t)
	if _, err := endpoint.runtimeResponse(OperationSearch, "r", "not bytes", protocolcommon.OutcomeSuccess, nil); err == nil {
		t.Fatal("non-byte search response was accepted")
	}
	if _, err := endpoint.runtimeResponse("runtime.unknown", "r", nil, protocolcommon.OutcomeSuccess, nil); err == nil {
		t.Fatal("unknown Runtime response was accepted")
	}
}

func newTestEndpoint(t *testing.T) *Endpoint {
	return newEndpointWithSearch(t, nil)
}

func newSearchTestEndpoint(t *testing.T) *Endpoint {
	return newEndpointWithSearch(t, searchSurfaceStub{})
}

func newEndpointWithSearch(t *testing.T, search ConsumerSearchSurface) *Endpoint {
	t.Helper()
	digest := protocolcommon.Digest("sha256:" + strings.Repeat("a", 64))
	local, err := localdocument.New(localdocument.Config{
		Root: t.TempDir(), ReleaseVersion: "1.0.0", EndpointInstanceID: "host-test",
		ReleaseManifestDigest: digest,
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = local.Shutdown(context.Background()) })
	engineFacade, err := engineendpoint.NewHostEngineFacade("1.0.0", "unknown", string(digest), "host-test", "stdio")
	if err != nil {
		t.Fatal(err)
	}
	composite, err := New(Config{LocalHost: local, Engine: engineFacade, Search: search})
	if err != nil {
		t.Fatal(err)
	}
	return composite
}
