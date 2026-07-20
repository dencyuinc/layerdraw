// SPDX-License-Identifier: LicenseRef-LayerDraw-1.0

package desktopwails

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"testing/fstest"

	"github.com/dencyuinc/layerdraw/gen/go/accessprotocol"
	"github.com/dencyuinc/layerdraw/gen/go/engineprotocol"
	"github.com/dencyuinc/layerdraw/gen/go/protocolcommon"
	"github.com/dencyuinc/layerdraw/gen/go/runtimeprotocol"
	accesscore "github.com/dencyuinc/layerdraw/internal/access"
	"github.com/dencyuinc/layerdraw/internal/desktopapp"
	"github.com/dencyuinc/layerdraw/internal/desktopcontract"
	engineendpoint "github.com/dencyuinc/layerdraw/internal/engine/endpoint"
	"github.com/dencyuinc/layerdraw/internal/localdocument"
	"github.com/wailsapp/wails/v2/pkg/options"
)

type nativeStub struct {
	open, save string
	err        error
	events     []string
	shown      bool
	quit       bool
}

func (n *nativeStub) OpenDirectory(context.Context, string) (string, error) { return n.open, n.err }
func (n *nativeStub) OpenFile(context.Context, string, []string) (string, error) {
	return n.open, n.err
}
func (n *nativeStub) SaveFile(context.Context, string, []string) (string, error) {
	return n.save, n.err
}
func (n *nativeStub) ShowWindow(context.Context) { n.shown = true }
func (n *nativeStub) Quit(context.Context)       { n.quit = true }
func (n *nativeStub) Emit(_ context.Context, name string, _ ...any) {
	n.events = append(n.events, name)
}

func TestNativeDialogCancelAndSingleUseOpaqueSelection(t *testing.T) {
	t.Parallel()
	native := &nativeStub{}
	vault := newSelectionVault()
	dialogs := NewDialogAdapter(native, vault)
	cancelled := dialogs.Select(context.Background(), desktopcontract.DialogRequest{Kind: desktopcontract.DialogOpenProject, RequestID: "open", Extensions: []string{"ldl"}})
	if cancelled.Outcome != protocolcommon.OutcomeCancelled || cancelled.Failure == nil || cancelled.Failure.Code != desktopcontract.FailureDialogCancelled {
		t.Fatalf("cancel was not preserved: %+v", cancelled)
	}

	root := t.TempDir()
	path := filepath.Join(root, "document.ldl")
	if err := os.WriteFile(path, []byte("project p \"P\" {}\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	native.open = path
	selected := dialogs.Select(context.Background(), desktopcontract.DialogRequest{Kind: desktopcontract.DialogOpenProject, RequestID: "open", Extensions: []string{"ldl"}})
	if selected.Outcome != protocolcommon.OutcomeSuccess || selected.Value.Token == "" || selected.Value.Token == path {
		t.Fatalf("selection was not opaque: %+v", selected)
	}
	storage := NewProjectStorageAdapter(vault)
	location, err := storage.Open(context.Background(), selected.Value.Token)
	if err != nil || location.Root != root || location.EntryPath != "document.ldl" {
		t.Fatalf("resolve: %+v %v", location, err)
	}
	if _, err := storage.Open(context.Background(), selected.Value.Token); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("token was reusable: %v", err)
	}
}

func TestCreateProjectUsesExclusiveFileAndValidSource(t *testing.T) {
	t.Parallel()
	native := &nativeStub{save: filepath.Join(t.TempDir(), "new-project")}
	vault := newSelectionVault()
	selection := NewDialogAdapter(native, vault).Select(context.Background(), desktopcontract.DialogRequest{Kind: desktopcontract.DialogCreateProject, RequestID: "create"})
	location, err := NewProjectStorageAdapter(vault).Create(context.Background(), selection.Value.Token)
	if err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(filepath.Join(location.Root, location.EntryPath))
	if err != nil || string(data) != "project project \"Untitled\" {}\n" {
		t.Fatalf("created source: %q %v", data, err)
	}
	native.save = filepath.Join(location.Root, location.EntryPath)
	second := NewDialogAdapter(native, vault).Select(context.Background(), desktopcontract.DialogRequest{Kind: desktopcontract.DialogCreateProject, RequestID: "create-2"})
	if _, err := NewProjectStorageAdapter(vault).Create(context.Background(), second.Value.Token); err == nil {
		t.Fatal("existing project was overwritten")
	}
}

type providerStub struct{}

func (providerStub) Connect(context.Context, desktopapp.ExternalConnectionRequest) (desktopapp.ExternalConnection, error) {
	return desktopapp.ExternalConnection{ConnectionID: "connection", ProviderID: "provider"}, nil
}
func (providerStub) Sync(context.Context, desktopapp.ExternalSyncRequest) (desktopapp.ExternalSyncResult, error) {
	return desktopapp.ExternalSyncResult{ProviderVersion: "v1"}, nil
}
func (providerStub) Reconcile(context.Context, desktopapp.ExternalReconcileRequest) (desktopapp.ExternalReconcileResult, error) {
	return desktopapp.ExternalReconcileResult{ProviderVersion: "v2", Converged: true}, nil
}

func TestExternalAdapterRoutesOnlyEstablishedConnections(t *testing.T) {
	t.Parallel()
	adapter := NewExternalAdapter(map[string]ExternalProvider{"provider": providerStub{}})
	connected := adapter.Connect(context.Background(), desktopapp.ExternalConnectionRequest{ProviderID: "provider"})
	if connected.Outcome != protocolcommon.OutcomeSuccess {
		t.Fatalf("connect: %+v", connected)
	}
	unknown := adapter.Sync(context.Background(), desktopapp.ExternalSyncRequest{ConnectionID: "forged"})
	if unknown.Failure == nil || unknown.Failure.Recovery != desktopcontract.RecoveryConfigureAdapter {
		t.Fatalf("forged connection: %+v", unknown)
	}
	synced := adapter.Sync(context.Background(), desktopapp.ExternalSyncRequest{ConnectionID: "connection"})
	if synced.Outcome != protocolcommon.OutcomeSuccess || synced.Value.ProviderVersion != "v1" {
		t.Fatalf("sync: %+v", synced)
	}
	reconciled := adapter.Reconcile(context.Background(), desktopapp.ExternalReconcileRequest{ConnectionID: "connection"})
	if reconciled.Outcome != protocolcommon.OutcomeSuccess || !reconciled.Value.Converged {
		t.Fatalf("reconcile: %+v", reconciled)
	}
	if failed := adapter.Connect(context.Background(), desktopapp.ExternalConnectionRequest{ProviderID: "missing"}); failed.Failure == nil || failed.Failure.Retryable {
		t.Fatalf("missing provider: %+v", failed)
	}
}

func TestProductionCompositionCallsDesktopApplicationConstructor(t *testing.T) {
	base, err := NewSharedConfig(filepath.Join(t.TempDir(), "data"))
	if err != nil {
		t.Fatal(err)
	}
	native := &nativeStub{}
	application, err := Compose(base, native, nil)
	if err != nil || application == nil {
		t.Fatalf("compose: %v", err)
	}
	LifecycleAdapter{runtime: native}.Publish(context.Background(), desktopcontract.LifecycleEvent{State: desktopcontract.LifecycleStarting})
	RecoveryReporter{runtime: native}.Report(context.Background(), desktopcontract.Failure{Code: desktopcontract.FailureStartup, Component: desktopcontract.ComponentBindingShell, Recovery: desktopcontract.RecoveryRetry})
	if len(native.events) != 2 || native.events[0] != lifecycleEvent || native.events[1] != recoveryEvent {
		t.Fatalf("native events: %v", native.events)
	}
}

func TestPackagedCompositionReadyCallsOwnersAndStops(t *testing.T) {
	base, err := NewSharedConfig(filepath.Join(t.TempDir(), "data"))
	if err != nil {
		t.Fatal(err)
	}
	projectRoot := t.TempDir()
	projectPath := filepath.Join(projectRoot, "document.ldl")
	if err := os.WriteFile(projectPath, []byte("project p \"P\" {}\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	application, err := Compose(base, &nativeStub{open: projectPath}, nil)
	if err != nil {
		t.Fatal(err)
	}
	started := application.Start(context.Background())
	if started.Outcome != protocolcommon.OutcomeSuccess || application.State() != desktopcontract.LifecycleReady {
		t.Fatalf("packaged owners did not enter ready: %+v state=%s", started, application.State())
	}
	engineControl, err := engineprotocol.EncodeHandshakeRequestEnvelope(engineprotocol.HandshakeRequestEnvelope{
		Operation: engineprotocol.HandshakeRequestEnvelopeOperationValue,
		Protocol:  engineprotocol.EngineProtocolRef{Name: engineprotocol.EngineProtocolRefNameValue, Version: engineprotocol.EngineProtocolRefVersionValue},
		RequestID: "desktop-engine-call",
		Payload:   protocolcommon.HandshakeRequest{ClientRelease: desktopRelease, Protocols: []protocolcommon.ProtocolOffer{{Name: engineendpoint.ProtocolName, SupportedRange: "1.0..1.0", Versions: []protocolcommon.ProtocolVersionBinding{{Version: engineendpoint.ProtocolVersion, SchemaDigest: protocolcommon.Digest(engineprotocol.SchemaDigest)}}}}, RequiredCapabilities: []protocolcommon.CapabilityID{}, OptionalCapabilities: []protocolcommon.CapabilityID{}},
	})
	if err != nil {
		t.Fatal(err)
	}
	engineCall := application.Invoke(context.Background(), "EngineHandshake", desktopcontract.Exchange{Operation: string(engineprotocol.HandshakeRequestEnvelopeOperationValue), Control: engineControl})
	if engineCall.Outcome != protocolcommon.OutcomeSuccess || !engineCall.Validate() {
		t.Fatalf("engine call: %+v", engineCall)
	}
	runtimeControl, err := runtimeprotocol.EncodeRuntimeHandshakeRequestEnvelope(runtimeprotocol.RuntimeHandshakeRequestEnvelope{
		Operation: runtimeprotocol.RuntimeHandshakeRequestEnvelopeOperationValue,
		Protocol:  runtimeprotocol.RuntimeProtocolRef{Name: runtimeprotocol.RuntimeProtocolRefNameValue, Version: "1.0"},
		RequestID: "desktop-runtime-call",
		Payload:   runtimeprotocol.RuntimeHandshakeRequest{ClientRelease: desktopRelease, Protocols: []protocolcommon.ProtocolOffer{{Name: "runtime", SupportedRange: "1.0..1.0", Versions: []protocolcommon.ProtocolVersionBinding{{Version: "1.0", SchemaDigest: protocolcommon.Digest(runtimeprotocol.SchemaDigest)}}}}, RequiredCapabilities: []protocolcommon.CapabilityID{}, OptionalCapabilities: []protocolcommon.CapabilityID{}},
	})
	if err != nil {
		t.Fatal(err)
	}
	runtimeCall := application.Invoke(context.Background(), "RuntimeHandshake", desktopcontract.Exchange{Operation: string(runtimeprotocol.RuntimeHandshakeRequestEnvelopeOperationValue), Control: runtimeControl})
	if runtimeCall.Outcome != protocolcommon.OutcomeSuccess || !runtimeCall.Validate() {
		t.Fatalf("runtime call: %+v", runtimeCall)
	}
	opened := application.OpenProjectDialog(context.Background(), "shared-host-open")
	if opened.Outcome != protocolcommon.OutcomeSuccess {
		t.Fatalf("project open through application host: %+v", opened)
	}
	inspectControl, err := runtimeprotocol.EncodeInspectDocumentRequestEnvelope(runtimeprotocol.InspectDocumentRequestEnvelope{
		Operation: runtimeprotocol.InspectDocumentRequestEnvelopeOperationValue,
		Protocol:  runtimeprotocol.RuntimeProtocolRef{Name: runtimeprotocol.RuntimeProtocolRefNameValue, Version: "1.0"},
		RequestID: "shared-host-inspect",
		Payload:   runtimeprotocol.RuntimeSessionInput{Session: opened.Value.Open.Session},
	})
	if err != nil {
		t.Fatal(err)
	}
	inspectCall := application.Invoke(context.Background(), "RuntimeInspectDocument", desktopcontract.Exchange{
		Operation: string(runtimeprotocol.InspectDocumentRequestEnvelopeOperationValue),
		Control:   inspectControl,
	})
	if inspectCall.Outcome != protocolcommon.OutcomeSuccess || !inspectCall.Validate() {
		t.Fatalf("runtime binding could not inspect application session: %+v", inspectCall)
	}
	inspected, err := runtimeprotocol.DecodeInspectDocumentResponseEnvelope(inspectCall.Value.Control)
	if err != nil || inspected.Outcome != protocolcommon.OutcomeSuccess || inspected.Payload == nil {
		t.Fatalf("inspect application session response: %+v err=%v", inspected, err)
	}
	stopped := application.Shutdown(context.Background())
	if stopped.Outcome != protocolcommon.OutcomeSuccess {
		t.Fatalf("shutdown: %+v", stopped)
	}
}

func TestRunExposesPackagedNativeExtensionSeam(t *testing.T) {
	base, err := NewSharedConfig(filepath.Join(t.TempDir(), "data"))
	if err != nil {
		t.Fatal(err)
	}
	original := runWails
	t.Cleanup(func() { runWails = original })
	called := false
	runWails = func(config *options.App) error {
		called = true
		if config.Title != "Packaged LayerDraw" || len(config.Bind) != 2 || config.OnStartup == nil || config.OnShutdown == nil || config.OnBeforeClose == nil {
			t.Fatalf("incomplete Wails config: %+v", config)
		}
		if config.OnBeforeClose(context.Background()) {
			t.Fatal("clean unopened application blocked close")
		}
		config.OnShutdown(context.Background())
		return nil
	}
	err = Run(base, fstest.MapFS{"index.html": {Data: []byte("desktop")}}, nil, func(config *options.App) {
		config.Title = "Packaged LayerDraw"
		config.Bind = append(config.Bind, struct{}{})
	})
	if err != nil || !called {
		t.Fatalf("run: called=%v err=%v", called, err)
	}
	failedProvider := errorProvider{}
	external := NewExternalAdapter(map[string]ExternalProvider{"broken": failedProvider})
	if result := external.Connect(context.Background(), desktopapp.ExternalConnectionRequest{ProviderID: "broken"}); result.Failure == nil {
		t.Fatal("provider connect failure hidden")
	}
	external.connections["broken-connection"] = "broken"
	if result := external.Sync(context.Background(), desktopapp.ExternalSyncRequest{ConnectionID: "broken-connection"}); result.Failure == nil {
		t.Fatal("provider sync failure hidden")
	}
	if result := external.Reconcile(context.Background(), desktopapp.ExternalReconcileRequest{ConnectionID: "broken-connection"}); result.Failure == nil {
		t.Fatal("provider reconcile failure hidden")
	}
}

type errorProvider struct{}

func (errorProvider) Connect(context.Context, desktopapp.ExternalConnectionRequest) (desktopapp.ExternalConnection, error) {
	return desktopapp.ExternalConnection{}, errors.New("provider unavailable")
}
func (errorProvider) Sync(context.Context, desktopapp.ExternalSyncRequest) (desktopapp.ExternalSyncResult, error) {
	return desktopapp.ExternalSyncResult{}, errors.New("provider unavailable")
}
func (errorProvider) Reconcile(context.Context, desktopapp.ExternalReconcileRequest) (desktopapp.ExternalReconcileResult, error) {
	return desktopapp.ExternalReconcileResult{}, errors.New("provider unavailable")
}

func TestNativeAdapterNegativeAndContainerBoundaries(t *testing.T) {
	t.Parallel()
	if got := patterns([]string{".ldl", " layerdraw ", ""}); got != "*.ldl;*.layerdraw" {
		t.Fatalf("patterns: %q", got)
	}
	native := &nativeStub{err: errors.New("native failure")}
	vault := newSelectionVault()
	for _, request := range []desktopcontract.DialogRequest{
		{Kind: desktopcontract.DialogCreateProject}, {Kind: desktopcontract.DialogOpenProject},
		{Kind: desktopcontract.DialogImport}, {Kind: desktopcontract.DialogExport}, {Kind: "forged"},
	} {
		result := NewDialogAdapter(native, vault).Select(context.Background(), request)
		if result.Failure == nil || result.Failure.Code != desktopcontract.FailureAdapterUnavailable {
			t.Fatalf("dialog failure %q: %+v", request.Kind, result)
		}
	}

	native.err = nil
	container := filepath.Join(t.TempDir(), "portable.layerdraw")
	if err := os.WriteFile(container, []byte("container"), 0o600); err != nil {
		t.Fatal(err)
	}
	native.open = container
	selected := NewDialogAdapter(native, vault).Select(context.Background(), desktopcontract.DialogRequest{Kind: desktopcontract.DialogImport})
	storage := NewProjectStorageAdapter(vault)
	location, err := storage.Import(context.Background(), selected.Value.Token)
	if err != nil || location.Kind != "container" {
		t.Fatalf("import: %+v %v", location, err)
	}
	native.open = container
	selected = NewDialogAdapter(native, vault).Select(context.Background(), desktopcontract.DialogRequest{Kind: desktopcontract.DialogOpenProject})
	relocated, err := storage.Relocate(context.Background(), "document", selected.Value.Token)
	if err != nil || relocated.Kind != "container" {
		t.Fatalf("relocate: %+v %v", relocated, err)
	}

	directory := t.TempDir()
	if _, err := projectLocation(directory); err == nil {
		t.Fatal("directory accepted as project")
	}
	unsupported := filepath.Join(t.TempDir(), "project.txt")
	if err := os.WriteFile(unsupported, []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := projectLocation(unsupported); err == nil {
		t.Fatal("unsupported extension accepted")
	}

	window := WindowAdapter{runtime: native}
	if err := window.Show(context.Background()); err != nil {
		t.Fatal(err)
	}
	if err := window.RequestClose(context.Background()); err != nil {
		t.Fatal(err)
	}
	if !native.shown || !native.quit {
		t.Fatalf("window bridge: %+v", native)
	}
}

func TestCompositionRejectsMissingRuntimeAndMismatchedExternalLifecycle(t *testing.T) {
	t.Parallel()
	base, err := NewSharedConfig(filepath.Join(t.TempDir(), "data"))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := Compose(base, nil, nil); err == nil {
		t.Fatal("nil runtime accepted")
	}
	if _, err := Compose(base, &nativeStub{}, map[string]ExternalProvider{"provider": providerStub{}}); err == nil {
		t.Fatal("external lifecycle accepted without capability lifecycle adapter")
	}
	if err := Run(base, nil, nil); err == nil {
		t.Fatal("nil assets accepted")
	}
}

func TestClosedSharedPortsRemainTyped(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	if result := (disabledMCP{}).Start(ctx); result.Outcome != protocolcommon.OutcomeSuccess {
		t.Fatalf("disabled MCP lifecycle failed: %+v", result)
	}
	handshake, err := (nativeCapabilities{}).Negotiate(ctx, desktopcontract.DefaultManifest())
	if err != nil {
		t.Fatal(err)
	}
	for _, status := range handshake.CapabilityStatuses {
		packaged := status.CapabilityID == desktopcontract.CapabilityAuthoring
		if status.Enabled != packaged || (!packaged && status.UnavailableReason == nil) {
			t.Fatalf("capability availability is not truthful: %+v", status)
		}
	}
	if result := (unavailableCredentials{}).Resolve(ctx, desktopcontract.CredentialRef{}); result.Failure == nil || !result.Failure.Validate() {
		t.Fatalf("credential: %+v", result)
	}
	if result := (unavailableOwner{}).IssueLocalOwnerGrant(ctx, desktopcontract.LocalOwnerGrantRequest{}); result.Failure == nil || !result.Failure.Validate() {
		t.Fatalf("owner: %+v", result)
	}
	delegations := unavailableDelegations{}
	if result := delegations.Delegate(ctx, accessprotocol.AuthoringGrantSnapshot{}, accesscore.Delegation{}); result.Failure == nil {
		t.Fatalf("delegate: %+v", result)
	}
	if result := delegations.Resolve(ctx, desktopcontract.DelegationFence{}); result.Failure == nil {
		t.Fatalf("resolve: %+v", result)
	}
	if result := delegations.Revoke(ctx, desktopcontract.DelegationFence{}); result.Failure == nil {
		t.Fatalf("revoke: %+v", result)
	}
	decoder := closedOwnerDecoder{}
	control := []byte(`{"operation":"review.submit","request_id":"request"}`)
	identity, err := decoder.DecodeRequest("review.submit", control)
	if err != nil || identity.RequestID != "request" {
		t.Fatalf("decode: %+v %v", identity, err)
	}
	if _, err := decoder.DecodeRequest("host.export", control); err == nil {
		t.Fatal("operation mismatch accepted")
	}
	if _, err := decoder.DecodeResponse("review.submit", control); err == nil {
		t.Fatal("unavailable response accepted")
	}
}

func TestSharedOwnerAndBlobBridgesRemainClosedAndOwned(t *testing.T) {
	ctx := context.Background()
	owner := &sharedOwner{}
	if _, err := owner.Invoke(ctx, desktopcontract.Exchange{}); err == nil {
		t.Fatal("unstarted shared owner accepted invocation")
	}
	if err := owner.Start(ctx); err == nil {
		t.Fatal("unbound shared owner started")
	}
	if err := owner.BindLocalHost(nil); err == nil {
		t.Fatal("nil local host accepted")
	}
	localHost, err := localdocument.New(localdocument.Config{Root: filepath.Join(t.TempDir(), "owner")})
	if err != nil {
		t.Fatal(err)
	}
	defer localHost.Shutdown(ctx)
	if err := owner.BindLocalHost(localHost); err != nil {
		t.Fatal(err)
	}
	if err := owner.Start(ctx); err != nil {
		t.Fatal(err)
	}
	if err := owner.BindLocalHost(localHost); err == nil {
		t.Fatal("started owner accepted host replacement")
	}
	if err := owner.Start(ctx); err != nil {
		t.Fatalf("idempotent owner start: %v", err)
	}
	if _, err := owner.Invoke(ctx, desktopcontract.Exchange{Operation: string(engineprotocol.HandshakeRequestEnvelopeOperationValue), Control: []byte(`{}`)}); err == nil {
		t.Fatal("malformed engine handshake accepted")
	}
	if _, err := owner.Invoke(ctx, desktopcontract.Exchange{Operation: "runtime.unknown", Control: []byte(`{}`)}); err == nil {
		t.Fatal("unknown runtime operation accepted")
	}
	sourceBytes := []byte("input")
	definitions, err := (exchangeBlobSource{{ID: "blob-1", Bytes: sourceBytes}}).Definitions(ctx)
	if err != nil || len(definitions) != 1 || definitions[0].Owned == nil || string(definitions[0].Owned.Bytes) != "input" {
		t.Fatalf("definitions=%+v err=%v", definitions, err)
	}
	sourceBytes[0] = 'X'
	if string(definitions[0].Owned.Bytes) != "input" {
		t.Fatal("blob source did not take ownership")
	}
	definitions[0].Owned.Release()
	sink := &exchangeBlobSink{}
	outputBytes := []byte("output")
	if err := sink.Publish(ctx, []engineendpoint.OutputBlob{{Ref: protocolcommon.BlobRef{BlobID: "blob-2"}, Bytes: outputBytes}}); err != nil {
		t.Fatal(err)
	}
	outputBytes[0] = 'X'
	if len(sink.blobs) != 1 || string(sink.blobs[0].Bytes) != "output" {
		t.Fatalf("blob sink did not take ownership: %+v", sink.blobs)
	}
	if err := (disabledComponent{}).Start(ctx); err != nil {
		t.Fatal(err)
	}
	if err := (disabledComponent{}).Shutdown(ctx); err != nil {
		t.Fatal(err)
	}
	if result := (disabledMCP{}).Shutdown(ctx); result.Outcome != protocolcommon.OutcomeSuccess {
		t.Fatalf("disabled MCP shutdown=%+v", result)
	}
	if err := owner.Shutdown(ctx); err != nil {
		t.Fatal(err)
	}
	if err := owner.Shutdown(ctx); err != nil {
		t.Fatalf("idempotent owner shutdown: %v", err)
	}
}
