// SPDX-License-Identifier: LicenseRef-LayerDraw-1.0

package desktopapp

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/dencyuinc/layerdraw/gen/go/accessprotocol"
	"github.com/dencyuinc/layerdraw/gen/go/protocolcommon"
	"github.com/dencyuinc/layerdraw/gen/go/runtimeprotocol"
	"github.com/dencyuinc/layerdraw/internal/desktopcontract"
)

type recordingCredentialPort struct {
	value []byte
	fail  bool
}

func (port *recordingCredentialPort) Resolve(context.Context, desktopcontract.CredentialRef) desktopcontract.Result[[]byte] {
	if port.fail {
		return failed[[]byte](desktopcontract.FailureCredential, desktopcontract.ComponentAccess, true, desktopcontract.RecoveryReconnect)
	}
	port.value = []byte("provider-secret-value")
	return desktopcontract.Result[[]byte]{Outcome: protocolcommon.OutcomeSuccess, Value: port.value}
}

type publicationGateStub struct {
	calls  int
	panic  bool
	result desktopcontract.Result[struct{}]
}

func (gate *publicationGateStub) RevalidateExternalPublication(context.Context, ExternalPublicationIntent) desktopcontract.Result[struct{}] {
	gate.calls++
	if gate.panic {
		panic("private access evaluator detail")
	}
	return gate.result
}

func TestReferenceExternalStorageLifecycleConditionalWriteAndRestart(t *testing.T) {
	root := t.TempDir()
	credentials := &recordingCredentialPort{}
	now := time.Date(2026, 7, 20, 1, 2, 3, 0, time.UTC)
	adapter, err := NewReferenceExternalStorage(ReferenceExternalStorageConfig{Root: root, Credentials: credentials, Now: func() time.Time { return now }})
	if err != nil {
		t.Fatal(err)
	}
	connected := adapter.Connect(context.Background(), ExternalConnectionRequest{
		ProviderID: "reference", CredentialRef: desktopcontract.CredentialRef{ID: "keychain:item-1"}, AccountLabel: "Design account", ScopeLabel: "Projects",
	})
	if connected.Outcome != protocolcommon.OutcomeSuccess || connected.Value.Status != ExternalConnectionConnected || !connected.Value.Capabilities.ConditionalWrite || !connected.Value.Capabilities.Lease {
		t.Fatalf("connect=%+v", connected)
	}
	for _, value := range credentials.value {
		if value != 0 {
			t.Fatal("credential bytes survived Connect")
		}
	}
	stateBytes, err := os.ReadFile(filepath.Join(root, "external-storage-reference", "state.json"))
	if err != nil {
		t.Fatal(err)
	}
	stateText := string(stateBytes)
	if strings.Contains(stateText, "provider-secret-value") || strings.Contains(stateText, "keychain:item-1") {
		t.Fatalf("secret or credential ref persisted: %s", stateText)
	}
	if refreshed := adapter.Refresh(context.Background(), connected.Value.ConnectionID); refreshed.Outcome != protocolcommon.OutcomeSuccess || refreshed.Value.Status != ExternalConnectionConnected {
		t.Fatalf("refresh=%+v", refreshed)
	}
	credentials.fail = true
	if expired := adapter.Refresh(context.Background(), connected.Value.ConnectionID); expired.Outcome != protocolcommon.OutcomeFailed || expired.Failure.Code != desktopcontract.FailureCredential {
		t.Fatalf("credential expiry=%+v", expired)
	}
	if inspected := adapter.Inspect(context.Background(), connected.Value.ConnectionID); inspected.Value.Status != ExternalConnectionExpired {
		t.Fatalf("expired status=%+v", inspected)
	}
	credentials.fail = false
	connected = adapter.Connect(context.Background(), ExternalConnectionRequest{
		ProviderID: "reference", CredentialRef: desktopcontract.CredentialRef{ID: "keychain:item-1"}, AccountLabel: "Design account", ScopeLabel: "Projects",
	})

	documentID := runtimeprotocol.DocumentID("document-1")
	selected := adapter.SelectRemote(context.Background(), ExternalRemoteSelectionRequest{ConnectionID: connected.Value.ConnectionID, DocumentID: documentID, SelectionToken: "opaque-picker-token"})
	if selected.Outcome != protocolcommon.OutcomeSuccess || selected.Value.DocumentID != documentID || selected.Value.BindingID == "" || selected.Value.RemoteItemID == "opaque-picker-token" {
		t.Fatalf("select=%+v", selected)
	}
	if selectedAgain := adapter.SelectRemote(context.Background(), ExternalRemoteSelectionRequest{ConnectionID: connected.Value.ConnectionID, DocumentID: documentID, SelectionToken: "opaque-picker-token"}); selectedAgain.Value != selected.Value {
		t.Fatalf("stable binding changed: first=%+v second=%+v", selected, selectedAgain)
	}
	if missing := adapter.Inspect(context.Background(), "missing"); missing.Outcome != protocolcommon.OutcomeFailed {
		t.Fatalf("missing inspect=%+v", missing)
	}
	if invalidLease := adapter.AcquireLease(context.Background(), ExternalBackendBinding{BindingID: "missing"}); invalidLease.Outcome != protocolcommon.OutcomeFailed {
		t.Fatalf("invalid lease=%+v", invalidLease)
	}
	revision := revisionRef(documentID, "revision-1")
	lease := adapter.AcquireLease(context.Background(), selected.Value)
	if lease.Outcome != protocolcommon.OutcomeSuccess || lease.Value.Token == "" {
		t.Fatalf("lease=%+v", lease)
	}
	written := adapter.Write(context.Background(), ExternalWriteRequest{
		Binding: selected.Value, Revision: revision, ExpectedProviderVersion: "v0", LeaseToken: lease.Value.Token, Payload: []byte("canonical project bytes"),
	})
	if written.Outcome != protocolcommon.OutcomeSuccess || written.Value.State != ExternalWritePublished || written.Value.ProviderVersion != "v1" {
		t.Fatalf("write=%+v", written)
	}
	staleLease := adapter.AcquireLease(context.Background(), ExternalBackendBinding{
		BindingID: selected.Value.BindingID, ConnectionID: selected.Value.ConnectionID, DocumentID: selected.Value.DocumentID, RemoteItemID: selected.Value.RemoteItemID, ProviderVersion: "v1",
	})
	if staleLease.Outcome != protocolcommon.OutcomeSuccess {
		t.Fatalf("second lease=%+v", staleLease)
	}
	conflict := adapter.Write(context.Background(), ExternalWriteRequest{
		Binding: selected.Value, Revision: revisionRef(documentID, "revision-2"), ExpectedProviderVersion: "v0", LeaseToken: staleLease.Value.Token,
	})
	if conflict.Value.State != ExternalWriteConflict || conflict.Value.ProviderVersion != "v1" {
		t.Fatalf("blind overwrite was not rejected: %+v", conflict)
	}
	cancelled, cancel := context.WithCancel(context.Background())
	cancel()
	if offline := adapter.Write(cancelled, ExternalWriteRequest{}); offline.Value.State != ExternalWriteOffline || !offline.Value.Retryable {
		t.Fatalf("offline write=%+v", offline)
	}
	if moved := adapter.Write(context.Background(), ExternalWriteRequest{Binding: ExternalBackendBinding{BindingID: "moved"}}); moved.Value.State != ExternalWriteMoved {
		t.Fatalf("moved write=%+v", moved)
	}
	if compatibility := adapter.Reconcile(context.Background(), ExternalReconcileRequest{ConnectionID: connected.Value.ConnectionID, DocumentID: documentID, Resolution: "keep_current"}); compatibility.Outcome != protocolcommon.OutcomeSuccess || !compatibility.Value.Converged {
		t.Fatalf("compatibility reconcile=%+v", compatibility)
	}

	plan := adapter.PlanReconcile(context.Background(), ExternalSyncRequest{ConnectionID: connected.Value.ConnectionID, DocumentID: documentID, Revision: revisionRef(documentID, "revision-2")}, true)
	if plan.Outcome != protocolcommon.OutcomeSuccess || plan.Value.Kind != ExternalReconcileQuarantined || !plan.Value.RequiresReview {
		t.Fatalf("restricted plan=%+v", plan)
	}
	if applied := adapter.ApplyReconcile(context.Background(), plan.Value, "accept_provider"); applied.Value.Converged {
		t.Fatalf("restricted change silently applied: %+v", applied)
	}
	stalePlan := plan.Value
	stalePlan.ProviderVersion = "forged"
	if stale := adapter.ApplyReconcile(context.Background(), stalePlan, "accept_provider"); stale.Outcome != protocolcommon.OutcomeFailed {
		t.Fatalf("stale plan=%+v", stale)
	}

	restarted, err := NewReferenceExternalStorage(ReferenceExternalStorageConfig{Root: root, Credentials: credentials, Now: func() time.Time { return now }})
	if err != nil {
		t.Fatal(err)
	}
	if refresh := restarted.Refresh(context.Background(), connected.Value.ConnectionID); refresh.Outcome != protocolcommon.OutcomeFailed || refresh.Failure.Recovery != desktopcontract.RecoveryReconnect {
		t.Fatalf("restart must request a new keychain authorization: %+v", refresh)
	}
	synced := restarted.Sync(context.Background(), ExternalSyncRequest{ConnectionID: connected.Value.ConnectionID, DocumentID: documentID, Revision: revision})
	if synced.Outcome != protocolcommon.OutcomeSuccess || synced.Value.ProviderVersion != "v1" || synced.Value.ReconcileNeeded {
		t.Fatalf("restart sync=%+v", synced)
	}
	disconnected := restarted.Disconnect(context.Background(), connected.Value.ConnectionID)
	if disconnected.Outcome != protocolcommon.OutcomeSuccess || disconnected.Value.Status != ExternalConnectionDisconnected {
		t.Fatalf("disconnect=%+v", disconnected)
	}
	if missing := restarted.Disconnect(context.Background(), "missing"); missing.Outcome != protocolcommon.OutcomeFailed {
		t.Fatalf("missing disconnect=%+v", missing)
	}
	if offline := restarted.Sync(context.Background(), ExternalSyncRequest{ConnectionID: connected.Value.ConnectionID, DocumentID: documentID, Revision: revision}); offline.Outcome != protocolcommon.OutcomeFailed || offline.Failure.Recovery != desktopcontract.RecoveryReconnect {
		t.Fatalf("offline sync=%+v", offline)
	}
}

func TestReferenceExternalStorageFailsClosedWithoutCredentialAndRejectsStalePlan(t *testing.T) {
	adapter, err := NewReferenceExternalStorage(ReferenceExternalStorageConfig{Root: t.TempDir(), Credentials: credentialFailure{}})
	if err != nil {
		t.Fatal(err)
	}
	if result := adapter.Connect(context.Background(), ExternalConnectionRequest{ProviderID: "reference", CredentialRef: desktopcontract.CredentialRef{ID: "missing"}, AccountLabel: "a", ScopeLabel: "s"}); result.Outcome != protocolcommon.OutcomeFailed || result.Failure.Code != desktopcontract.FailureCredential {
		t.Fatalf("credential failure=%+v", result)
	}
	if _, err := NewReferenceExternalStorage(ReferenceExternalStorageConfig{Root: "relative", Credentials: credentialPortStub{}}); err == nil {
		t.Fatal("relative adapter root accepted")
	}
}

func TestReferenceExternalStorageRejectsCorruptOrExposedRestartState(t *testing.T) {
	for _, testCase := range []struct {
		name string
		body string
		mode os.FileMode
	}{
		{name: "unknown field", body: `{"version":1,"connections":{},"bindings":{},"credential":"secret"}`, mode: 0o600},
		{name: "trailing value", body: `{"version":1,"connections":{},"bindings":{}} {}`, mode: 0o600},
		{name: "world readable", body: `{"version":1,"connections":{},"bindings":{}}`, mode: 0o644},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			root := t.TempDir()
			stateRoot := filepath.Join(root, "external-storage-reference")
			if err := os.MkdirAll(filepath.Join(stateRoot, "objects"), 0o700); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(filepath.Join(stateRoot, "state.json"), []byte(testCase.body), testCase.mode); err != nil {
				t.Fatal(err)
			}
			if _, err := NewReferenceExternalStorage(ReferenceExternalStorageConfig{Root: root, Credentials: credentialPortStub{}}); err == nil {
				t.Fatal("unsafe state accepted")
			}
		})
	}
}

func TestReferenceExternalStorageReportsPartialUnknownAndRateLimitRecovery(t *testing.T) {
	root := t.TempDir()
	adapter, err := NewReferenceExternalStorage(ReferenceExternalStorageConfig{Root: root, Credentials: credentialPortStub{}})
	if err != nil {
		t.Fatal(err)
	}
	connected := adapter.Connect(context.Background(), ExternalConnectionRequest{ProviderID: "reference", CredentialRef: desktopcontract.CredentialRef{ID: "keychain:test"}, AccountLabel: "Account", ScopeLabel: "Scope"})
	binding := adapter.SelectRemote(context.Background(), ExternalRemoteSelectionRequest{ConnectionID: connected.Value.ConnectionID, DocumentID: "document", SelectionToken: "item"})
	lease := adapter.AcquireLease(context.Background(), binding.Value)
	objects := filepath.Join(root, "external-storage-reference", "objects")
	if err := os.Remove(objects); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(objects, []byte("blocks uploads"), 0o600); err != nil {
		t.Fatal(err)
	}
	request := ExternalWriteRequest{Binding: binding.Value, Revision: revisionRef("document", "revision-1"), ExpectedProviderVersion: "v0", LeaseToken: lease.Value.Token, Payload: []byte("payload")}
	if partial := adapter.Write(context.Background(), request); partial.Value.State != ExternalWritePartial || !partial.Value.Retryable {
		t.Fatalf("partial upload=%+v", partial)
	}
	if err := os.Remove(objects); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(objects, 0o700); err != nil {
		t.Fatal(err)
	}
	statePath := filepath.Join(root, "external-storage-reference", "state.json")
	if err := os.Remove(statePath); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(statePath, 0o700); err != nil {
		t.Fatal(err)
	}
	if unknown := adapter.Write(context.Background(), request); unknown.Value.State != ExternalWriteUnknown || !unknown.Value.Retryable {
		t.Fatalf("unknown write result=%+v", unknown)
	}
	if err := os.Remove(statePath); err != nil {
		t.Fatal(err)
	}
	adapter.mu.Lock()
	connection := adapter.state.Connections[connected.Value.ConnectionID]
	connection.Status = ExternalConnectionRateLimited
	adapter.state.Connections[connected.Value.ConnectionID] = connection
	if err := adapter.saveLocked(); err != nil {
		adapter.mu.Unlock()
		t.Fatal(err)
	}
	adapter.mu.Unlock()
	if inspected := adapter.Inspect(context.Background(), connected.Value.ConnectionID); inspected.Value.Status != ExternalConnectionRateLimited {
		t.Fatalf("rate limit status=%+v", inspected)
	}
	if synced := adapter.Sync(context.Background(), ExternalSyncRequest{ConnectionID: connected.Value.ConnectionID, DocumentID: "document", Revision: request.Revision}); synced.Outcome != protocolcommon.OutcomeFailed || synced.Failure.Recovery != desktopcontract.RecoveryReconnect {
		t.Fatalf("rate limited sync=%+v", synced)
	}
}

func TestExternalPublicationRequiresCurrentRuntimeAccessGate(t *testing.T) {
	app := &Application{}
	if failure := app.revalidateExternal(context.Background(), ExternalPublicationIntent{}); failure == nil || failure.Code != desktopcontract.FailurePermissionDenied || failure.Component != desktopcontract.ComponentAccess {
		t.Fatalf("missing gate=%+v", failure)
	}
	gate := &publicationGateStub{result: desktopcontract.Result[struct{}]{Outcome: protocolcommon.OutcomeSuccess}}
	app.config.ExternalPublication = gate
	if failure := app.revalidateExternal(context.Background(), ExternalPublicationIntent{Action: "publish_local"}); failure != nil || gate.calls != 1 {
		t.Fatalf("allowed gate failure=%+v calls=%d", failure, gate.calls)
	}
	gate.panic = true
	if failure := app.revalidateExternal(context.Background(), ExternalPublicationIntent{}); failure == nil || failure.Code != desktopcontract.FailureBackendPanic {
		t.Fatalf("panic gate=%+v", failure)
	}
}

func TestApplicationExternalStorageSurfaceRevalidatesAndTracksPublication(t *testing.T) {
	root := t.TempDir()
	projectRoot := writeProject(t, root)
	storage := &lifecycleStorageHarness{locations: map[string]ProjectLocation{"open": {Root: projectRoot, EntryPath: "document.ldl"}}, errs: map[string]error{}}
	app := startLifecycleApp(t, root, projectRoot, &dialogHarness{}, storage)
	opened := app.OpenProject(context.Background(), "open")
	if opened.Outcome != protocolcommon.OutcomeSuccess {
		t.Fatalf("open=%+v", opened)
	}
	done, host, hostFailure := app.beginHost(desktopcontract.ComponentAccess)
	if hostFailure != nil {
		t.Fatal(hostFailure)
	}
	if err := host.AuthorizeHostOperation(context.Background(), opened.Value.Open.Session, opened.Value.Open.CommittedRevision, accessprotocol.HostOperationKindBackendConfigure, "update", []string{"binding"}); err != nil {
		done()
		t.Fatalf("host publication authorization=%v", err)
	}
	staleRevision := opened.Value.Open.CommittedRevision
	staleRevision.RevisionID = "stale"
	if err := host.AuthorizeHostOperation(context.Background(), opened.Value.Open.Session, staleRevision, accessprotocol.HostOperationKindBackendConfigure, "update", []string{"binding"}); err == nil {
		done()
		t.Fatal("stale publication authorized")
	}
	done()
	adapter, err := NewReferenceExternalStorage(ReferenceExternalStorageConfig{Root: root, Credentials: credentialPortStub{}})
	if err != nil {
		t.Fatal(err)
	}
	gate := &publicationGateStub{result: desktopcontract.Result[struct{}]{Outcome: protocolcommon.OutcomeSuccess}}
	app.config.ExternalLifecycle = adapter
	app.config.ExternalPublication = gate
	connected := app.ConnectExternal(context.Background(), ExternalConnectionRequest{ProviderID: "reference", CredentialRef: desktopcontract.CredentialRef{ID: "keychain:test"}, AccountLabel: "Account", ScopeLabel: "Scope"})
	if connected.Outcome != protocolcommon.OutcomeSuccess {
		t.Fatalf("connect=%+v", connected)
	}
	if inspected := app.InspectExternal(context.Background(), connected.Value.ConnectionID); inspected.Outcome != protocolcommon.OutcomeSuccess {
		t.Fatalf("inspect=%+v", inspected)
	}
	if refreshed := app.RefreshExternal(context.Background(), connected.Value.ConnectionID); refreshed.Outcome != protocolcommon.OutcomeSuccess {
		t.Fatalf("refresh=%+v", refreshed)
	}
	binding := app.SelectExternalRemote(context.Background(), ExternalRemoteSelectionRequest{ConnectionID: connected.Value.ConnectionID, DocumentID: opened.Value.ProjectID, SelectionToken: "picker"})
	if binding.Outcome != protocolcommon.OutcomeSuccess {
		t.Fatalf("select=%+v", binding)
	}
	lease := app.AcquireExternalLease(context.Background(), opened.Value.Open.Session, binding.Value)
	if lease.Outcome != protocolcommon.OutcomeSuccess {
		t.Fatalf("lease=%+v", lease)
	}
	written := app.WriteExternal(context.Background(), opened.Value.Open.Session, ExternalWriteRequest{
		Binding: binding.Value, Revision: opened.Value.Open.CommittedRevision, ExpectedProviderVersion: "v0", LeaseToken: lease.Value.Token, Payload: []byte("project"),
	})
	if written.Outcome != protocolcommon.OutcomeSuccess || written.Value.State != ExternalWritePublished || gate.calls != 1 {
		t.Fatalf("write=%+v gate=%d", written, gate.calls)
	}
	plan := app.PlanExternalReconcile(context.Background(), opened.Value.Open.Session, ExternalSyncRequest{ConnectionID: connected.Value.ConnectionID, DocumentID: opened.Value.ProjectID, Revision: opened.Value.Open.CommittedRevision}, false)
	if plan.Outcome != protocolcommon.OutcomeSuccess || plan.Value.Kind != ExternalReconcileUpToDate {
		t.Fatalf("plan=%+v", plan)
	}
	applied := app.ApplyExternalReconcile(context.Background(), opened.Value.Open.Session, plan.Value, "keep_current")
	if applied.Outcome != protocolcommon.OutcomeSuccess || !applied.Value.Converged || gate.calls != 2 {
		t.Fatalf("apply=%+v gate=%d", applied, gate.calls)
	}
	if disconnected := app.DisconnectExternal(context.Background(), connected.Value.ConnectionID); disconnected.Outcome != protocolcommon.OutcomeSuccess {
		t.Fatalf("disconnect=%+v", disconnected)
	}
	if err := adapter.Start(context.Background()); err != nil {
		t.Fatal(err)
	}
	if err := adapter.Shutdown(context.Background()); err != nil {
		t.Fatal(err)
	}
	_ = app.CloseProject(context.Background(), opened.Value.Open.Session)
	_ = app.Shutdown(context.Background())
}

func TestApplicationExternalStorageSurfaceFailsClosedWithoutFullAdapter(t *testing.T) {
	app := &Application{}
	if result := app.InspectExternal(context.Background(), ""); result.Failure == nil || result.Failure.Code != desktopcontract.FailureAdapterUnavailable {
		t.Fatalf("inspect=%+v", result)
	}
	if result := app.RefreshExternal(context.Background(), ""); result.Failure == nil {
		t.Fatalf("refresh=%+v", result)
	}
	if result := app.DisconnectExternal(context.Background(), ""); result.Failure == nil {
		t.Fatalf("disconnect=%+v", result)
	}
	if result := app.SelectExternalRemote(context.Background(), ExternalRemoteSelectionRequest{}); result.Failure == nil {
		t.Fatalf("select=%+v", result)
	}
	if result := app.AcquireExternalLease(context.Background(), runtimeprotocol.RuntimeSessionRef{}, ExternalBackendBinding{}); result.Failure == nil {
		t.Fatalf("lease=%+v", result)
	}
	if result := app.WriteExternal(context.Background(), runtimeprotocol.RuntimeSessionRef{}, ExternalWriteRequest{}); result.Failure == nil {
		t.Fatalf("write=%+v", result)
	}
	if result := app.PlanExternalReconcile(context.Background(), runtimeprotocol.RuntimeSessionRef{}, ExternalSyncRequest{}, false); result.Failure == nil {
		t.Fatalf("plan=%+v", result)
	}
	if result := app.ApplyExternalReconcile(context.Background(), runtimeprotocol.RuntimeSessionRef{}, ExternalReconcilePlan{}, ""); result.Failure == nil {
		t.Fatalf("apply=%+v", result)
	}
	for _, value := range []ExternalWriteResult{
		{State: ExternalWritePublished, ProviderVersion: "v1"},
		{State: ExternalWriteConflict, Retryable: true}, {State: ExternalWritePartial, Retryable: true},
		{State: ExternalWriteUnknown, Retryable: true}, {State: ExternalWriteMoved}, {State: ExternalWriteOffline, Retryable: true},
	} {
		if !validExternalWriteResult(value) {
			t.Fatalf("valid write result rejected: %+v", value)
		}
	}
	if validExternalWriteResult(ExternalWriteResult{}) || validExternalWriteResult(ExternalWriteResult{State: ExternalWritePublished}) {
		t.Fatal("invalid write result accepted")
	}
}

func revisionRef(documentID runtimeprotocol.DocumentID, revision runtimeprotocol.RevisionID) runtimeprotocol.CommittedRevisionRef {
	return runtimeprotocol.CommittedRevisionRef{
		DocumentID: documentID, RevisionID: revision,
		DefinitionHash: protocolcommon.Digest("sha256:" + strings.Repeat("1", 64)),
		GraphHash:      protocolcommon.Digest("sha256:" + strings.Repeat("2", 64)),
	}
}
