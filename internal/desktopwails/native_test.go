// SPDX-License-Identifier: LicenseRef-LayerDraw-1.0

package desktopwails

import (
	"bytes"
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"testing/fstest"
	"time"

	"github.com/dencyuinc/layerdraw/gen/go/accessprotocol"
	"github.com/dencyuinc/layerdraw/gen/go/engineprotocol"
	"github.com/dencyuinc/layerdraw/gen/go/protocolcommon"
	"github.com/dencyuinc/layerdraw/gen/go/runtimeprotocol"
	"github.com/dencyuinc/layerdraw/gen/go/semantic"
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

type storageProviderStub struct{ providerStub }

func (storageProviderStub) Inspect(_ context.Context, id string) (desktopapp.ExternalConnection, error) {
	return desktopapp.ExternalConnection{ConnectionID: id, ProviderID: "provider", Status: desktopapp.ExternalConnectionConnected}, nil
}
func (storageProviderStub) Refresh(_ context.Context, id string) (desktopapp.ExternalConnection, error) {
	return desktopapp.ExternalConnection{ConnectionID: id, ProviderID: "provider", Status: desktopapp.ExternalConnectionConnected}, nil
}
func (storageProviderStub) Disconnect(_ context.Context, id string) (desktopapp.ExternalConnection, error) {
	return desktopapp.ExternalConnection{ConnectionID: id, ProviderID: "provider", Status: desktopapp.ExternalConnectionDisconnected}, nil
}
func (storageProviderStub) SelectRemote(_ context.Context, request desktopapp.ExternalRemoteSelectionRequest) (desktopapp.ExternalBackendBinding, error) {
	return desktopapp.ExternalBackendBinding{BindingID: "binding", ConnectionID: request.ConnectionID, DocumentID: request.DocumentID, RemoteItemID: "remote", ProviderVersion: "v1"}, nil
}
func (storageProviderStub) AcquireLease(context.Context, desktopapp.ExternalBackendBinding) (desktopapp.ExternalLease, error) {
	return desktopapp.ExternalLease{Token: "lease", ExpiresAt: "2026-07-20T01:00:00Z"}, nil
}
func (storageProviderStub) Write(context.Context, desktopapp.ExternalWriteRequest) (desktopapp.ExternalWriteResult, error) {
	return desktopapp.ExternalWriteResult{State: desktopapp.ExternalWritePublished, ProviderVersion: "v2"}, nil
}
func (storageProviderStub) PlanReconcile(_ context.Context, request desktopapp.ExternalSyncRequest, restricted bool) (desktopapp.ExternalReconcilePlan, error) {
	return desktopapp.ExternalReconcilePlan{PlanID: "plan", Binding: desktopapp.ExternalBackendBinding{BindingID: "binding", ConnectionID: request.ConnectionID, DocumentID: request.DocumentID, RemoteItemID: "remote", ProviderVersion: "v1"}, LocalRevision: request.Revision, ProviderVersion: "v1", Restricted: restricted}, nil
}
func (storageProviderStub) ApplyReconcile(context.Context, desktopapp.ExternalReconcilePlan, string) (desktopapp.ExternalReconcileResult, error) {
	return desktopapp.ExternalReconcileResult{ProviderVersion: "v2", Converged: true}, nil
}

type malformedStorageProvider struct{ storageProviderStub }

func (malformedStorageProvider) Inspect(context.Context, string) (desktopapp.ExternalConnection, error) {
	return desktopapp.ExternalConnection{}, nil
}
func (malformedStorageProvider) Refresh(context.Context, string) (desktopapp.ExternalConnection, error) {
	return desktopapp.ExternalConnection{}, nil
}
func (malformedStorageProvider) Disconnect(context.Context, string) (desktopapp.ExternalConnection, error) {
	return desktopapp.ExternalConnection{}, nil
}
func (malformedStorageProvider) SelectRemote(context.Context, desktopapp.ExternalRemoteSelectionRequest) (desktopapp.ExternalBackendBinding, error) {
	return desktopapp.ExternalBackendBinding{}, nil
}
func (malformedStorageProvider) AcquireLease(context.Context, desktopapp.ExternalBackendBinding) (desktopapp.ExternalLease, error) {
	return desktopapp.ExternalLease{}, nil
}
func (malformedStorageProvider) Write(context.Context, desktopapp.ExternalWriteRequest) (desktopapp.ExternalWriteResult, error) {
	return desktopapp.ExternalWriteResult{}, errors.New("malformed write")
}
func (malformedStorageProvider) PlanReconcile(context.Context, desktopapp.ExternalSyncRequest, bool) (desktopapp.ExternalReconcilePlan, error) {
	return desktopapp.ExternalReconcilePlan{}, nil
}
func (malformedStorageProvider) ApplyReconcile(context.Context, desktopapp.ExternalReconcilePlan, string) (desktopapp.ExternalReconcileResult, error) {
	return desktopapp.ExternalReconcileResult{}, nil
}

type errorStorageProvider struct{ errorProvider }

func (errorStorageProvider) Inspect(context.Context, string) (desktopapp.ExternalConnection, error) {
	return desktopapp.ExternalConnection{}, errors.New("provider unavailable")
}
func (errorStorageProvider) Refresh(context.Context, string) (desktopapp.ExternalConnection, error) {
	return desktopapp.ExternalConnection{}, errors.New("provider unavailable")
}
func (errorStorageProvider) Disconnect(context.Context, string) (desktopapp.ExternalConnection, error) {
	return desktopapp.ExternalConnection{}, errors.New("provider unavailable")
}
func (errorStorageProvider) SelectRemote(context.Context, desktopapp.ExternalRemoteSelectionRequest) (desktopapp.ExternalBackendBinding, error) {
	return desktopapp.ExternalBackendBinding{}, errors.New("provider unavailable")
}
func (errorStorageProvider) AcquireLease(context.Context, desktopapp.ExternalBackendBinding) (desktopapp.ExternalLease, error) {
	return desktopapp.ExternalLease{}, errors.New("provider unavailable")
}
func (errorStorageProvider) Write(context.Context, desktopapp.ExternalWriteRequest) (desktopapp.ExternalWriteResult, error) {
	return desktopapp.ExternalWriteResult{}, errors.New("provider unavailable")
}
func (errorStorageProvider) PlanReconcile(context.Context, desktopapp.ExternalSyncRequest, bool) (desktopapp.ExternalReconcilePlan, error) {
	return desktopapp.ExternalReconcilePlan{}, errors.New("provider unavailable")
}
func (errorStorageProvider) ApplyReconcile(context.Context, desktopapp.ExternalReconcilePlan, string) (desktopapp.ExternalReconcileResult, error) {
	return desktopapp.ExternalReconcileResult{}, errors.New("provider unavailable")
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
	if result := adapter.Inspect(context.Background(), "forged"); result.Failure == nil {
		t.Fatalf("forged inspect: %+v", result)
	}
	if result := adapter.Refresh(context.Background(), "forged"); result.Failure == nil {
		t.Fatalf("forged refresh: %+v", result)
	}
	if result := adapter.Disconnect(context.Background(), "forged"); result.Failure == nil {
		t.Fatalf("forged disconnect: %+v", result)
	}
	forgedBinding := desktopapp.ExternalBackendBinding{ConnectionID: "forged", DocumentID: "document"}
	if result := adapter.SelectRemote(context.Background(), desktopapp.ExternalRemoteSelectionRequest{ConnectionID: "forged"}); result.Failure == nil {
		t.Fatalf("forged selection: %+v", result)
	}
	if result := adapter.AcquireLease(context.Background(), forgedBinding); result.Failure == nil {
		t.Fatalf("forged lease: %+v", result)
	}
	if result := adapter.Write(context.Background(), desktopapp.ExternalWriteRequest{Binding: forgedBinding}); result.Failure == nil {
		t.Fatalf("forged write: %+v", result)
	}
	if result := adapter.PlanReconcile(context.Background(), desktopapp.ExternalSyncRequest{ConnectionID: "forged"}, false); result.Failure == nil {
		t.Fatalf("forged plan: %+v", result)
	}
	if result := adapter.ApplyReconcile(context.Background(), desktopapp.ExternalReconcilePlan{Binding: forgedBinding}, "retry"); result.Failure == nil {
		t.Fatalf("forged apply: %+v", result)
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

func TestExternalAdapterForwardsFullStorageContractOnlyToOwningProvider(t *testing.T) {
	adapter := NewExternalAdapter(map[string]ExternalProvider{"provider": storageProviderStub{}})
	connected := adapter.Connect(context.Background(), desktopapp.ExternalConnectionRequest{ProviderID: "provider"})
	if connected.Outcome != protocolcommon.OutcomeSuccess {
		t.Fatal(connected.Failure)
	}
	connectionID := connected.Value.ConnectionID
	if result := adapter.Inspect(context.Background(), connectionID); result.Outcome != protocolcommon.OutcomeSuccess {
		t.Fatalf("inspect=%+v", result)
	}
	if result := adapter.Refresh(context.Background(), connectionID); result.Outcome != protocolcommon.OutcomeSuccess {
		t.Fatalf("refresh=%+v", result)
	}
	request := desktopapp.ExternalRemoteSelectionRequest{ConnectionID: connectionID, DocumentID: "document", SelectionToken: "opaque"}
	binding := adapter.SelectRemote(context.Background(), request)
	if binding.Outcome != protocolcommon.OutcomeSuccess {
		t.Fatalf("select=%+v", binding)
	}
	if result := adapter.AcquireLease(context.Background(), binding.Value); result.Outcome != protocolcommon.OutcomeSuccess {
		t.Fatalf("lease=%+v", result)
	}
	if result := adapter.Write(context.Background(), desktopapp.ExternalWriteRequest{Binding: binding.Value}); result.Outcome != protocolcommon.OutcomeSuccess {
		t.Fatalf("write=%+v", result)
	}
	syncRequest := desktopapp.ExternalSyncRequest{ConnectionID: connectionID, DocumentID: "document", Revision: runtimeprotocol.CommittedRevisionRef{DocumentID: "document", RevisionID: "revision"}}
	plan := adapter.PlanReconcile(context.Background(), syncRequest, true)
	if plan.Outcome != protocolcommon.OutcomeSuccess || !plan.Value.Restricted {
		t.Fatalf("plan=%+v", plan)
	}
	if result := adapter.ApplyReconcile(context.Background(), plan.Value, "accept_provider"); result.Outcome != protocolcommon.OutcomeSuccess {
		t.Fatalf("apply=%+v", result)
	}
	if result := adapter.Disconnect(context.Background(), connectionID); result.Outcome != protocolcommon.OutcomeSuccess {
		t.Fatalf("disconnect=%+v", result)
	}
	legacy := NewExternalAdapter(map[string]ExternalProvider{"provider": providerStub{}})
	legacy.connections["connection"] = "provider"
	if result := legacy.Inspect(context.Background(), "connection"); result.Failure == nil || result.Failure.Retryable {
		t.Fatalf("legacy provider exposed full storage: %+v", result)
	}
}

func TestExternalAdapterRejectsMalformedStorageProviderResults(t *testing.T) {
	adapter := NewExternalAdapter(map[string]ExternalProvider{"provider": malformedStorageProvider{}})
	connected := adapter.Connect(context.Background(), desktopapp.ExternalConnectionRequest{ProviderID: "provider"})
	if connected.Outcome != protocolcommon.OutcomeSuccess {
		t.Fatal(connected.Failure)
	}
	connectionID := connected.Value.ConnectionID
	assertFailed := func(name string, failure *desktopcontract.Failure) {
		t.Helper()
		if failure == nil {
			t.Fatalf("malformed %s result accepted", name)
		}
	}
	assertFailed("inspect", adapter.Inspect(context.Background(), connectionID).Failure)
	assertFailed("refresh", adapter.Refresh(context.Background(), connectionID).Failure)
	assertFailed("disconnect", adapter.Disconnect(context.Background(), connectionID).Failure)
	binding := desktopapp.ExternalBackendBinding{ConnectionID: connectionID, DocumentID: "document"}
	assertFailed("select", adapter.SelectRemote(context.Background(), desktopapp.ExternalRemoteSelectionRequest{ConnectionID: connectionID, DocumentID: "document"}).Failure)
	assertFailed("lease", adapter.AcquireLease(context.Background(), binding).Failure)
	assertFailed("write", adapter.Write(context.Background(), desktopapp.ExternalWriteRequest{Binding: binding}).Failure)
	assertFailed("plan", adapter.PlanReconcile(context.Background(), desktopapp.ExternalSyncRequest{ConnectionID: connectionID, DocumentID: "document"}, false).Failure)
	assertFailed("apply", adapter.ApplyReconcile(context.Background(), desktopapp.ExternalReconcilePlan{Binding: binding}, "retry").Failure)
}

func TestExternalAdapterClosesEveryStorageProviderFailure(t *testing.T) {
	t.Parallel()
	adapter := NewExternalAdapter(map[string]ExternalProvider{
		"broken": errorStorageProvider{},
		"empty":  nil,
		"":       storageProviderStub{},
	})
	connected := adapter.Connect(context.Background(), desktopapp.ExternalConnectionRequest{ProviderID: "broken"})
	if connected.Failure == nil || !connected.Failure.Retryable {
		t.Fatalf("connect=%+v", connected)
	}
	adapter.connections["connection"] = "broken"
	assertFailed := func(name string, failure *desktopcontract.Failure) {
		t.Helper()
		if failure == nil || !failure.Retryable || failure.Recovery == desktopcontract.RecoveryConfigureAdapter {
			t.Fatalf("%s failure=%+v", name, failure)
		}
	}
	ctx := context.Background()
	assertFailed("inspect", adapter.Inspect(ctx, "connection").Failure)
	assertFailed("refresh", adapter.Refresh(ctx, "connection").Failure)
	assertFailed("disconnect", adapter.Disconnect(ctx, "connection").Failure)
	binding := desktopapp.ExternalBackendBinding{ConnectionID: "connection", DocumentID: "document"}
	assertFailed("select", adapter.SelectRemote(ctx, desktopapp.ExternalRemoteSelectionRequest{ConnectionID: "connection", DocumentID: "document"}).Failure)
	assertFailed("lease", adapter.AcquireLease(ctx, binding).Failure)
	assertFailed("write", adapter.Write(ctx, desktopapp.ExternalWriteRequest{Binding: binding}).Failure)
	request := desktopapp.ExternalSyncRequest{ConnectionID: "connection", DocumentID: "document"}
	assertFailed("plan", adapter.PlanReconcile(ctx, request, false).Failure)
	assertFailed("apply", adapter.ApplyReconcile(ctx, desktopapp.ExternalReconcilePlan{Binding: binding}, "retry").Failure)

	missing := NewExternalAdapter(nil)
	assertMissing := func(name string, failure *desktopcontract.Failure) {
		t.Helper()
		if failure == nil || failure.Retryable || failure.Recovery != desktopcontract.RecoveryConfigureAdapter {
			t.Fatalf("%s missing provider failure=%+v", name, failure)
		}
	}
	assertMissing("refresh", missing.Refresh(ctx, "unknown").Failure)
	assertMissing("disconnect", missing.Disconnect(ctx, "unknown").Failure)
	assertMissing("select", missing.SelectRemote(ctx, desktopapp.ExternalRemoteSelectionRequest{ConnectionID: "unknown"}).Failure)
	assertMissing("lease", missing.AcquireLease(ctx, desktopapp.ExternalBackendBinding{ConnectionID: "unknown"}).Failure)
	assertMissing("write", missing.Write(ctx, desktopapp.ExternalWriteRequest{Binding: desktopapp.ExternalBackendBinding{ConnectionID: "unknown"}}).Failure)
	assertMissing("plan", missing.PlanReconcile(ctx, desktopapp.ExternalSyncRequest{ConnectionID: "unknown"}, false).Failure)
	assertMissing("apply", missing.ApplyReconcile(ctx, desktopapp.ExternalReconcilePlan{Binding: desktopapp.ExternalBackendBinding{ConnectionID: "unknown"}}, "retry").Failure)
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
	publication, err := application.ProjectPublication(context.Background())
	if err != nil || publication.Project == nil || publication.Project.ProjectID != opened.Value.Open.Session.Scope.DocumentID || publication.Project.LibraryProject.ProjectID == "" || publication.Project.LibraryProject.ResolvedLockDigest == "" || publication.Project.LibraryProject.DependencySnapshot.Installs == nil || publication.Project.Views == nil {
		t.Fatalf("project publication=%+v err=%v", publication, err)
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
		if config.Title != "Packaged LayerDraw" || len(config.Bind) != 3 || config.Menu == nil || config.SingleInstanceLock == nil || config.OnStartup == nil || config.OnShutdown == nil || config.OnBeforeClose == nil {
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

func TestProjectStorageRejectsPinnedAssociationReplacement(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	path := filepath.Join(root, "associated.ldl")
	if err := os.WriteFile(path, []byte("project original \"Original\" {}\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	identity, err := os.Lstat(path)
	if err != nil {
		t.Fatal(err)
	}
	vault := newSelectionVault()
	token, err := vault.issuePinned(path, identity)
	if err != nil {
		t.Fatal(err)
	}
	replacement := filepath.Join(root, "replacement.ldl")
	if err := os.WriteFile(replacement, []byte("project replacement \"Replacement\" {}\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Rename(replacement, path); err != nil {
		t.Fatal(err)
	}
	if _, err := NewProjectStorageAdapter(vault).Open(context.Background(), token); err == nil {
		t.Fatal("replaced association target accepted")
	}
}

func TestProjectStorageReturnsHandleBoundAssociationContent(t *testing.T) {
	t.Parallel()
	path := filepath.Join(t.TempDir(), "associated.ldl")
	original := []byte("project original \"Original\" {}\n")
	if err := os.WriteFile(path, original, 0o600); err != nil {
		t.Fatal(err)
	}
	identity, err := os.Lstat(path)
	if err != nil {
		t.Fatal(err)
	}
	vault := newSelectionVault()
	token, err := vault.issuePinned(path, identity)
	if err != nil {
		t.Fatal(err)
	}
	location, err := NewProjectStorageAdapter(vault).Open(context.Background(), token)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte("not valid LDL"), 0o600); err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(location.PinnedContent, original) {
		t.Fatalf("pinned content changed: %q", location.PinnedContent)
	}
	if _, err := pinnedContent(selectionPath{path: filepath.Join(t.TempDir(), "missing.ldl"), identity: identity}); err == nil {
		t.Fatal("missing pinned association content accepted")
	}
	large := filepath.Join(t.TempDir(), "large.ldl")
	if err := os.WriteFile(large, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Truncate(large, maxPinnedAssociationBytes+1); err != nil {
		t.Fatal(err)
	}
	largeIdentity, err := os.Lstat(large)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := pinnedContent(selectionPath{path: large, identity: largeIdentity}); err == nil {
		t.Fatal("oversized pinned association content accepted")
	}
	container := filepath.Join(t.TempDir(), "associated.layerdraw")
	containerBytes := []byte("handle-bound container")
	if err := os.WriteFile(container, containerBytes, 0o600); err != nil {
		t.Fatal(err)
	}
	containerIdentity, err := os.Lstat(container)
	if err != nil {
		t.Fatal(err)
	}
	containerToken, err := vault.issuePinned(container, containerIdentity)
	if err != nil {
		t.Fatal(err)
	}
	imported, err := NewProjectStorageAdapter(vault).Import(context.Background(), containerToken)
	if err != nil || !bytes.Equal(imported.PinnedContent, containerBytes) {
		t.Fatalf("pinned container=%+v err=%v", imported, err)
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
	if _, err := Compose(base, &nativeStub{}, map[string]ExternalProvider{"provider": providerStub{}}); err != nil {
		t.Fatalf("packaged external provider replacement was rejected: %v", err)
	}
	if err := Run(base, nil, nil); err == nil {
		t.Fatal("nil assets accepted")
	}
}

func TestClosedSharedPortsRemainTyped(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	base, err := NewSharedConfig(filepath.Join(t.TempDir(), "data"))
	if err != nil {
		t.Fatal(err)
	}
	if result := base.ExternalPublication.RevalidateExternalPublication(ctx, desktopapp.ExternalPublicationIntent{}); result.Outcome != protocolcommon.OutcomeFailed || result.Failure.Code != desktopcontract.FailurePermissionDenied {
		t.Fatalf("external publication gate=%+v", result)
	}
	if result := (disabledMCP{}).Start(ctx); result.Outcome != protocolcommon.OutcomeSuccess {
		t.Fatalf("disabled MCP lifecycle failed: %+v", result)
	}
	handshake, err := (nativeCapabilities{}).Negotiate(ctx, desktopcontract.DefaultManifest())
	if err != nil {
		t.Fatal(err)
	}
	available := make(map[protocolcommon.CapabilityID]bool)
	for _, id := range packagedCapabilities() {
		available[id] = true
	}
	for _, status := range handshake.CapabilityStatuses {
		packaged := available[status.CapabilityID]
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

func TestPackagedDesktopComposesCanonicalMCPDefaultOff(t *testing.T) {
	root := t.TempDir()
	project := filepath.Join(root, "project", "document.ldl")
	if err := os.MkdirAll(filepath.Dir(project), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(project, []byte("project p \"P\" {}\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	base, err := NewSharedConfig(filepath.Join(root, "data"))
	if err != nil {
		t.Fatal(err)
	}
	native := &nativeStub{open: project}
	app, err := Compose(base, native, nil)
	if err != nil {
		t.Fatal(err)
	}
	if result := app.Start(context.Background()); result.Outcome != protocolcommon.OutcomeSuccess {
		t.Fatalf("start=%+v", result)
	}
	defer app.Shutdown(context.Background())
	if status := app.MCPStatus(); status.Enabled {
		t.Fatalf("MCP silently enabled: %+v", status)
	}
	if result := app.SetMCPEnabled(context.Background(), true, desktopapp.MCPTransportLocal); result.Outcome != protocolcommon.OutcomeSuccess {
		t.Fatalf("enable=%+v", result)
	}
	if tools, failure := app.MCPListTools(context.Background()); failure != nil || len(tools) < 2 {
		t.Fatalf("tools=%d failure=%+v", len(tools), failure)
	}
	opened := app.OpenProjectDialog(context.Background(), "mcp-project")
	if opened.Outcome != protocolcommon.OutcomeSuccess {
		t.Fatalf("open=%+v", opened)
	}
	connect := func(agent string, apply bool) desktopapp.MCPConnection {
		result := app.CreateMCPConnection(context.Background(), desktopapp.MCPConnectRequest{
			ClientID: "reference-client", ProtocolVersion: desktopapp.MCPConnectionProtocolVersion, DocumentID: opened.Value.ProjectID, AgentID: agent,
			Capabilities: []semantic.AuthoringCapability{semantic.AuthoringCapabilityGraphWrite}, Permissions: accesscore.AgentPermissions{Read: true, Propose: true, Apply: apply},
			ExpiresAt: protocolcommon.Rfc3339Time(time.Now().Add(time.Hour).UTC().Format(time.RFC3339Nano)), ConfirmApply: apply,
		})
		if result.Outcome != protocolcommon.OutcomeSuccess {
			t.Fatalf("connect %s=%+v", agent, result)
		}
		return result.Value
	}
	proposal, applicable := connect("proposal-agent", false), connect("apply-agent", true)
	toolNames := func(connection string) map[string]bool {
		tools, failure := app.MCPListConnectionTools(context.Background(), connection)
		if failure != nil {
			t.Fatalf("connection tools=%+v", failure)
		}
		names := map[string]bool{}
		for _, tool := range tools {
			names[tool.Name] = true
		}
		return names
	}
	if names := toolNames(proposal.ConnectionID); !names["layerdraw.preview_operations"] || names["layerdraw.apply_operations"] {
		t.Fatalf("proposal tools=%v", names)
	}
	if names := toolNames(applicable.ConnectionID); !names["layerdraw.apply_operations"] {
		t.Fatalf("apply tools=%v", names)
	}
	if revoked := app.RevokeMCPConnection(context.Background(), proposal.ConnectionID); revoked.Outcome != protocolcommon.OutcomeSuccess {
		t.Fatalf("revoke=%+v", revoked)
	}
	if _, failure := app.MCPListConnectionTools(context.Background(), proposal.ConnectionID); failure == nil {
		t.Fatal("revoked reference client retained tools")
	}
	if restarted := app.RestartMCP(context.Background()); restarted.Outcome != protocolcommon.OutcomeSuccess {
		t.Fatalf("restart=%+v", restarted)
	}
	if _, failure := app.MCPListConnectionTools(context.Background(), applicable.ConnectionID); failure == nil {
		t.Fatal("host restart did not fence live client")
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
