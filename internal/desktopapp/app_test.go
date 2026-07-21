// SPDX-License-Identifier: LicenseRef-LayerDraw-1.0

package desktopapp

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"sync"
	"testing"
	"time"

	"github.com/dencyuinc/layerdraw/gen/go/accessprotocol"
	"github.com/dencyuinc/layerdraw/gen/go/engineprotocol"
	"github.com/dencyuinc/layerdraw/gen/go/protocolcommon"
	"github.com/dencyuinc/layerdraw/gen/go/runtimeprotocol"
	"github.com/dencyuinc/layerdraw/gen/go/semantic"
	accesscore "github.com/dencyuinc/layerdraw/internal/access"
	"github.com/dencyuinc/layerdraw/internal/desktopcontract"
	"github.com/dencyuinc/layerdraw/internal/engine"
	"github.com/dencyuinc/layerdraw/internal/localdocument"
)

var desktopTestNow = time.Date(2026, 7, 20, 12, 0, 0, 0, time.UTC)

type lifecycleRecorder struct {
	mu      sync.Mutex
	states  []desktopcontract.LifecycleState
	fail    desktopcontract.LifecycleState
	ready   chan struct{}
	release <-chan struct{}
}

type windowStub struct{}

func (windowStub) Show(context.Context) error         { return nil }
func (windowStub) RequestClose(context.Context) error { return nil }

type dialogStub struct{}

func (dialogStub) Select(_ context.Context, _ desktopcontract.DialogRequest) desktopcontract.Result[desktopcontract.DialogSelection] {
	return desktopcontract.Result[desktopcontract.DialogSelection]{Outcome: protocolcommon.OutcomeSuccess, Value: desktopcontract.DialogSelection{Token: "opaque"}}
}

type panicLifecycle struct{}

func (panicLifecycle) Publish(context.Context, desktopcontract.LifecycleEvent) error {
	panic("private")
}

func (r *lifecycleRecorder) Publish(_ context.Context, event desktopcontract.LifecycleEvent) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.states = append(r.states, event.State)
	if event.State == r.fail {
		return errors.New("lifecycle unavailable")
	}
	if event.State == desktopcontract.LifecycleReady && r.ready != nil {
		close(r.ready)
		<-r.release
	}
	return nil
}

type adapterStub struct {
	id       desktopcontract.ComponentID
	mu       *sync.Mutex
	events   *[]string
	startErr error
	stopErr  error
	started  chan struct{}
	release  <-chan struct{}
}

type nativeSearchLifecycleStub struct {
	err   error
	calls int
}

func (stub *nativeSearchLifecycleStub) RefreshSearchIndex(context.Context, *localdocument.Session) error {
	stub.calls++
	return stub.err
}

func (a *adapterStub) Start(context.Context) error {
	if a.mu != nil {
		a.mu.Lock()
		*a.events = append(*a.events, "start:"+string(a.id))
		a.mu.Unlock()
	}
	if a.started != nil {
		close(a.started)
	}
	if a.release != nil {
		<-a.release
	}
	return a.startErr
}

func (a *adapterStub) Shutdown(context.Context) error {
	if a.mu != nil {
		a.mu.Lock()
		*a.events = append(*a.events, "stop:"+string(a.id))
		a.mu.Unlock()
	}
	return a.stopErr
}

type storageStub struct{ project ProjectLocation }

func (s storageStub) Create(context.Context, string) (ProjectLocation, error) { return s.project, nil }
func (s storageStub) Open(context.Context, string) (ProjectLocation, error)   { return s.project, nil }

type panicStorage struct{}

func (panicStorage) Create(context.Context, string) (ProjectLocation, error) { panic("private") }
func (panicStorage) Open(context.Context, string) (ProjectLocation, error)   { panic("private") }

type recoveryRecorder struct{ values []desktopcontract.Failure }

func (r *recoveryRecorder) Report(_ context.Context, value desktopcontract.Failure) {
	r.values = append(r.values, value)
}

type panicReporter struct{}

func (panicReporter) Report(context.Context, desktopcontract.Failure) { panic("private") }

type negotiatorStub struct {
	value protocolcommon.HandshakeResult
	err   error
}

type panicNegotiator struct{}

func (panicNegotiator) Negotiate(context.Context, desktopcontract.Manifest) (protocolcommon.HandshakeResult, error) {
	panic("private")
}

type actorFailure struct{}

func (actorFailure) ResolveLocalActor(context.Context) desktopcontract.Result[accessprotocol.ActorRef] {
	return failed[accessprotocol.ActorRef](desktopcontract.FailureLocalActor, desktopcontract.ComponentAccess, true, desktopcontract.RecoveryRetry)
}

type localActorPortStub struct{ actor accessprotocol.ActorRef }

func (s localActorPortStub) ResolveLocalActor(context.Context) desktopcontract.Result[accessprotocol.ActorRef] {
	return desktopcontract.Result[accessprotocol.ActorRef]{Outcome: protocolcommon.OutcomeSuccess, Value: s.actor}
}

type panicActorPort struct{}

func (panicActorPort) ResolveLocalActor(context.Context) desktopcontract.Result[accessprotocol.ActorRef] {
	panic("private")
}

type credentialPortStub struct{}

func (credentialPortStub) Resolve(context.Context, desktopcontract.CredentialRef) desktopcontract.Result[[]byte] {
	return desktopcontract.Result[[]byte]{Outcome: protocolcommon.OutcomeSuccess, Value: []byte("ephemeral")}
}

type credentialFailure struct{ panic bool }

func (c credentialFailure) Resolve(context.Context, desktopcontract.CredentialRef) desktopcontract.Result[[]byte] {
	if c.panic {
		panic("credential secret")
	}
	return failed[[]byte](desktopcontract.FailureCredential, desktopcontract.ComponentAccess, true, desktopcontract.RecoveryRetry)
}

type localOwnerPortStub struct{}

func (localOwnerPortStub) IssueLocalOwnerGrant(_ context.Context, request desktopcontract.LocalOwnerGrantRequest) desktopcontract.Result[accessprotocol.AuthoringGrantSnapshot] {
	grant := accessprotocol.AuthoringGrantSnapshot{
		ActorRef: request.Actor, GrantedCapabilities: accesscore.FullAuthoringCapabilities(),
		HostDocumentID: request.Scope.DocumentID, IssuedAt: request.IssuedAt,
		LocalScopeID: request.Scope.LocalScopeID, MembershipVersion: "1",
		PolicyRefs: []accessprotocol.PolicyRef{},
	}
	grant.AccessFingerprint = accesscore.Fingerprint(grant)
	return desktopcontract.Result[accessprotocol.AuthoringGrantSnapshot]{Outcome: protocolcommon.OutcomeSuccess, Value: grant}
}

type localOwnerFailure struct{ panic bool }

func (p localOwnerFailure) IssueLocalOwnerGrant(context.Context, desktopcontract.LocalOwnerGrantRequest) desktopcontract.Result[accessprotocol.AuthoringGrantSnapshot] {
	if p.panic {
		panic("private")
	}
	return desktopcontract.Result[accessprotocol.AuthoringGrantSnapshot]{Outcome: protocolcommon.OutcomeSuccess}
}

type delegationPortStub struct{}

type delegationValueStub struct {
	delegation accesscore.Delegation
}

func (delegationPortStub) Delegate(context.Context, accessprotocol.AuthoringGrantSnapshot, accesscore.Delegation) desktopcontract.Result[accesscore.Delegation] {
	return desktopcontract.Result[accesscore.Delegation]{Outcome: protocolcommon.OutcomeSuccess}
}

type delegationFailure struct{ panic bool }

func (delegationFailure) Delegate(context.Context, accessprotocol.AuthoringGrantSnapshot, accesscore.Delegation) desktopcontract.Result[accesscore.Delegation] {
	return failed[accesscore.Delegation](desktopcontract.FailureAgentDelegation, desktopcontract.ComponentAccess, false, desktopcontract.RecoveryOpenRecovery)
}
func (d delegationFailure) Resolve(context.Context, desktopcontract.DelegationFence) desktopcontract.Result[accesscore.Delegation] {
	if d.panic {
		panic("private")
	}
	return failed[accesscore.Delegation](desktopcontract.FailureAgentDelegation, desktopcontract.ComponentAccess, false, desktopcontract.RecoveryOpenRecovery)
}
func (delegationFailure) Revoke(context.Context, desktopcontract.DelegationFence) desktopcontract.Result[accesscore.DelegationSnapshot] {
	return failed[accesscore.DelegationSnapshot](desktopcontract.FailureAgentDelegation, desktopcontract.ComponentAccess, false, desktopcontract.RecoveryOpenRecovery)
}
func (delegationPortStub) Resolve(_ context.Context, fence desktopcontract.DelegationFence) desktopcontract.Result[accesscore.Delegation] {
	return desktopcontract.Result[accesscore.Delegation]{Outcome: protocolcommon.OutcomeSuccess, Value: accesscore.Delegation{
		ID: fence.DelegationID, ParentActor: accessprotocol.ActorRef{ActorID: "desktop-test-owner", Kind: "user"},
		Agent:      accessprotocol.ActorRef{ActorID: "desktop-test-agent", Kind: "agent"},
		DocumentID: fence.DocumentID, LocalScopeID: fence.LocalScopeID,
		AuthoringCapabilities: accesscore.FullAuthoringCapabilities(),
		Permissions:           accesscore.AgentPermissions{Read: true}, IssuedAt: desktopTestNow.Add(-time.Minute),
		ExpiresAt: desktopTestNow.Add(time.Hour), Generation: 1,
	}}
}
func (delegationPortStub) Revoke(context.Context, desktopcontract.DelegationFence) desktopcontract.Result[accesscore.DelegationSnapshot] {
	return desktopcontract.Result[accesscore.DelegationSnapshot]{Outcome: protocolcommon.OutcomeSuccess}
}

func (d delegationValueStub) Delegate(context.Context, accessprotocol.AuthoringGrantSnapshot, accesscore.Delegation) desktopcontract.Result[accesscore.Delegation] {
	return desktopcontract.Result[accesscore.Delegation]{Outcome: protocolcommon.OutcomeSuccess, Value: d.delegation}
}

func (d delegationValueStub) Resolve(context.Context, desktopcontract.DelegationFence) desktopcontract.Result[accesscore.Delegation] {
	return desktopcontract.Result[accesscore.Delegation]{Outcome: protocolcommon.OutcomeSuccess, Value: d.delegation}
}

func (delegationValueStub) Revoke(context.Context, desktopcontract.DelegationFence) desktopcontract.Result[accesscore.DelegationSnapshot] {
	return desktopcontract.Result[accesscore.DelegationSnapshot]{Outcome: protocolcommon.OutcomeSuccess}
}

type mcpPortStub struct {
	mu         *sync.Mutex
	events     *[]string
	startPanic bool
	stopPanic  bool
	startFail  bool
	stopFail   bool
}

func (m mcpPortStub) Start(context.Context) desktopcontract.Result[struct{}] {
	if m.startPanic {
		panic("secret")
	}
	if m.startFail {
		return failed[struct{}](desktopcontract.FailureMCPTransport, desktopcontract.ComponentMCPHost, true, desktopcontract.RecoveryReconnect)
	}
	if m.mu != nil {
		m.mu.Lock()
		*m.events = append(*m.events, "start:"+string(desktopcontract.ComponentMCPHost))
		m.mu.Unlock()
	}
	return desktopcontract.Result[struct{}]{Outcome: protocolcommon.OutcomeSuccess}
}
func (m mcpPortStub) Shutdown(context.Context) desktopcontract.Result[struct{}] {
	if m.stopPanic {
		panic("secret")
	}
	if m.stopFail {
		return failed[struct{}](desktopcontract.FailureMCPTransport, desktopcontract.ComponentMCPHost, true, desktopcontract.RecoveryReconnect)
	}
	if m.mu != nil {
		m.mu.Lock()
		*m.events = append(*m.events, "stop:"+string(desktopcontract.ComponentMCPHost))
		m.mu.Unlock()
	}
	return desktopcontract.Result[struct{}]{Outcome: protocolcommon.OutcomeSuccess}
}

func (n negotiatorStub) Negotiate(context.Context, desktopcontract.Manifest) (protocolcommon.HandshakeResult, error) {
	return n.value, n.err
}

type ownerDecoderStub struct{}

func (ownerDecoderStub) DecodeRequest(expected string, control []byte) (desktopcontract.OwnerEnvelopeIdentity, error) {
	var wire struct {
		Operation string `json:"operation"`
		RequestID string `json:"request_id"`
	}
	err := json.Unmarshal(control, &wire)
	if err != nil || wire.Operation != expected || wire.RequestID == "" {
		return desktopcontract.OwnerEnvelopeIdentity{}, errors.New("invalid request")
	}
	return desktopcontract.OwnerEnvelopeIdentity{Operation: wire.Operation, RequestID: wire.RequestID}, nil
}

func (ownerDecoderStub) DecodeResponse(expected string, control []byte) (desktopcontract.OwnerResponseIdentity, error) {
	var wire struct {
		Operation string `json:"operation"`
		RequestID string `json:"request_id"`
		Outcome   string `json:"outcome"`
	}
	err := json.Unmarshal(control, &wire)
	if err != nil || wire.Operation != expected || wire.RequestID == "" {
		return desktopcontract.OwnerResponseIdentity{}, errors.New("invalid response")
	}
	return desktopcontract.OwnerResponseIdentity{Operation: wire.Operation, RequestID: wire.RequestID, Outcome: wire.Outcome}, nil
}

func completeClients(t *testing.T) desktopcontract.ClientSet {
	t.Helper()
	clients := desktopcontract.ClientSet{}
	root := reflect.ValueOf(&clients).Elem()
	methodType := reflect.TypeOf(desktopcontract.ClientMethod(nil))
	method := reflect.MakeFunc(methodType, func(values []reflect.Value) []reflect.Value {
		exchange := values[1].Interface().(desktopcontract.Exchange)
		control, _ := json.Marshal(map[string]string{"operation": exchange.Operation, "request_id": "request", "outcome": "success"})
		return []reflect.Value{reflect.ValueOf(desktopcontract.ExchangeResult{Operation: exchange.Operation, Control: control}), reflect.Zero(reflect.TypeOf((*error)(nil)).Elem())}
	})
	decoder := reflect.ValueOf(ownerDecoderStub{})
	for index := 0; index < root.NumField(); index++ {
		owner := root.Field(index)
		for fieldIndex := 0; fieldIndex < owner.NumField(); fieldIndex++ {
			field := owner.Field(fieldIndex)
			if field.Kind() == reflect.Interface {
				field.Set(decoder)
			} else {
				field.Set(method)
			}
		}
	}
	if err := clients.Validate(); err != nil {
		t.Fatal(err)
	}
	return clients
}

func validHandshake(t *testing.T) protocolcommon.HandshakeResult {
	t.Helper()
	data, err := os.ReadFile(filepath.Join("..", "..", "schemas", "fixtures", "engine", "handshake-success.json"))
	if err != nil {
		t.Fatal(err)
	}
	response, err := engineprotocol.DecodeHandshakeResponseEnvelope(data)
	if err != nil || response.Payload == nil {
		t.Fatalf("handshake fixture: %v", err)
	}
	value := *response.Payload
	manifest := desktopcontract.DefaultManifest()
	ids := append(append([]protocolcommon.CapabilityID(nil), manifest.RequiredCapabilities...), manifest.OptionalCapabilities...)
	value.CapabilityStatuses = make([]protocolcommon.RequestedCapabilityStatus, 0, len(ids))
	for _, id := range ids {
		status := protocolcommon.RequestedCapabilityStatus{CapabilityID: id, Enabled: true, ProtocolVersion: desktopcontract.DesktopProtocolVersion}
		if id == desktopcontract.CapabilityExternalStorage {
			reason := protocolcommon.UnavailableReasonNotConfigured
			status.Enabled = false
			status.UnavailableReason = &reason
		}
		value.CapabilityStatuses = append(value.CapabilityStatuses, status)
	}
	return value
}

func enableExternal(handshake protocolcommon.HandshakeResult) protocolcommon.HandshakeResult {
	for index := range handshake.CapabilityStatuses {
		if handshake.CapabilityStatuses[index].CapabilityID == desktopcontract.CapabilityExternalStorage {
			handshake.CapabilityStatuses[index].Enabled = true
			handshake.CapabilityStatuses[index].UnavailableReason = nil
		}
	}
	return handshake
}

func testConfig(t *testing.T, root, project string) Config {
	t.Helper()
	adapters := make(map[desktopcontract.ComponentID]Adapter)
	for _, id := range injectedComponents {
		adapters[id] = &adapterStub{id: id}
	}
	return Config{
		Root: filepath.Join(root, "desktop-data"), ReleaseVersion: "1.0.0", EndpointInstanceID: "desktop-test",
		ReleaseManifestDigest: "sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
		HostPorts: desktopcontract.HostPorts{
			Credentials: credentialPortStub{},
			LocalActor:  localActorPortStub{actor: accessprotocol.ActorRef{ActorID: "desktop-test-owner", Kind: "user"}},
			LocalOwner:  localOwnerPortStub{}, Delegations: delegationPortStub{}, MCP: mcpPortStub{},
		},
		Lifecycle: &lifecycleRecorder{}, Window: windowStub{}, Dialogs: dialogStub{}, ProjectStorage: storageStub{ProjectLocation{Root: project, EntryPath: "document.ldl"}},
		Capabilities: negotiatorStub{value: validHandshake(t)}, Bindings: completeClients(t), Adapters: adapters,
		Now: func() time.Time { return desktopTestNow },
	}
}

func writeProject(t *testing.T, root string) string {
	t.Helper()
	project := filepath.Join(root, "project")
	if err := os.MkdirAll(project, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(project, "document.ldl"), []byte("project p \"P\" {}\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	return project
}

func TestStartupOrdersAdaptersNegotiatesAndShutdownReverses(t *testing.T) {
	root := t.TempDir()
	config := testConfig(t, root, writeProject(t, root))
	var mu sync.Mutex
	var events []string
	for _, id := range injectedComponents {
		config.Adapters[id] = &adapterStub{id: id, mu: &mu, events: &events}
	}
	config.HostPorts.MCP = mcpPortStub{mu: &mu, events: &events}
	external := &adapterStub{id: desktopcontract.ComponentExternalStorage, mu: &mu, events: &events}
	config.Adapters[desktopcontract.ComponentExternalStorage] = external
	config.ExternalLifecycle = &externalLifecycleHarness{reconcile: true}
	config.Capabilities = negotiatorStub{value: enableExternal(validHandshake(t))}
	app, err := New(config)
	if err != nil {
		t.Fatal(err)
	}
	started := app.Start(context.Background())
	if started.Outcome != protocolcommon.OutcomeSuccess || app.State() != desktopcontract.LifecycleReady {
		t.Fatalf("start=%+v state=%s", started, app.State())
	}
	if _, ok := app.Handshake(); !ok {
		t.Fatal("ready handshake unavailable")
	}
	stopped := app.Shutdown(context.Background())
	if stopped.Outcome != protocolcommon.OutcomeSuccess || app.State() != desktopcontract.LifecycleStopped {
		t.Fatalf("shutdown=%+v state=%s", stopped, app.State())
	}
	wantStarts := []string{}
	for _, id := range startupOrder {
		if !isCore(id) {
			wantStarts = append(wantStarts, "start:"+string(id))
		}
	}
	if !reflect.DeepEqual(events[:len(wantStarts)], wantStarts) {
		t.Fatalf("startup order=%v want=%v", events, wantStarts)
	}
	gotStops := events[len(wantStarts):]
	wantStops := make([]string, len(wantStarts))
	for i, start := range wantStarts {
		wantStops[len(wantStarts)-1-i] = "stop:" + start[len("start:"):]
	}
	if !reflect.DeepEqual(gotStops, wantStops) {
		t.Fatalf("shutdown order=%v want=%v", gotStops, wantStops)
	}
}

func TestOptionalExternalMayBeAbsentAndRequiredAdapterFailsClosed(t *testing.T) {
	root := t.TempDir()
	config := testConfig(t, root, writeProject(t, root))
	app, err := New(config)
	if err != nil || app.Start(context.Background()).Outcome != protocolcommon.OutcomeSuccess {
		t.Fatalf("optional external rejected: app=%v err=%v", app, err)
	}
	_ = app.Shutdown(context.Background())
	delete(config.Adapters, desktopcontract.ComponentReview)
	if _, err := New(config); err == nil {
		t.Fatal("missing required adapter accepted")
	}
}

func TestConfigurationAndPreReadyCallsFailClosed(t *testing.T) {
	root := t.TempDir()
	project := writeProject(t, root)
	valid := testConfig(t, root, project)
	mutations := []func(*Config){
		func(c *Config) { c.Root = "relative" },
		func(c *Config) { c.Lifecycle = nil },
		func(c *Config) { c.ProjectStorage = nil },
		func(c *Config) { c.Capabilities = nil },
		func(c *Config) { c.HostPorts.LocalActor = nil },
		func(c *Config) { c.HostPorts.Credentials = nil },
		func(c *Config) { c.HostPorts.LocalOwner = nil },
		func(c *Config) { c.HostPorts.Delegations = nil },
		func(c *Config) { c.HostPorts.MCP = nil },
		func(c *Config) { c.Bindings.Engine.Compile = nil },
		func(c *Config) { c.Adapters = nil },
	}
	for index, mutate := range mutations {
		candidate := valid
		mutate(&candidate)
		if _, err := New(candidate); err == nil {
			t.Fatalf("invalid configuration %d accepted", index)
		}
	}
	app, err := New(valid)
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := app.Handshake(); ok {
		t.Fatal("handshake published before ready")
	}
	if result := app.OpenProject(context.Background(), "opaque"); result.Failure == nil || result.Failure.Code != desktopcontract.FailureReconnect {
		t.Fatalf("pre-ready call=%+v", result)
	}
	if result := app.Shutdown(context.Background()); result.Outcome != protocolcommon.OutcomeSuccess {
		t.Fatalf("idempotent stopped shutdown=%+v", result)
	}
}

func TestLifecycleCapabilityAndAdapterPanicFailures(t *testing.T) {
	root := t.TempDir()
	project := writeProject(t, root)
	config := testConfig(t, root, project)
	reporter := &recoveryRecorder{}
	config.Recovery = reporter
	config.Lifecycle = &lifecycleRecorder{fail: desktopcontract.LifecycleStarting}
	app, _ := New(config)
	result := app.Start(context.Background())
	if result.Failure == nil || result.Failure.Component != desktopcontract.ComponentBindingShell || len(reporter.values) != 1 {
		t.Fatalf("lifecycle failure=%+v reports=%v", result, reporter.values)
	}

	config = testConfig(t, filepath.Join(root, "actor"), writeProject(t, filepath.Join(root, "actor")))
	config.HostPorts.LocalActor = actorFailure{}
	app, _ = New(config)
	result = app.Start(context.Background())
	if result.Failure == nil || result.Failure.Code != desktopcontract.FailureLocalActor || result.Failure.Component != desktopcontract.ComponentAccess {
		t.Fatalf("actor failure=%+v", result)
	}

	config = testConfig(t, root, project)
	config.Capabilities = negotiatorStub{err: errors.New("private capability error")}
	app, _ = New(config)
	result = app.Start(context.Background())
	if result.Failure == nil || result.Failure.Code != desktopcontract.FailureProtocolIncompatible {
		t.Fatalf("capability failure=%+v", result)
	}

	config = testConfig(t, root, project)
	config.HostPorts.MCP = mcpPortStub{startPanic: true}
	app, _ = New(config)
	result = app.Start(context.Background())
	if result.Failure == nil || result.Failure.Code != desktopcontract.FailureBackendPanic {
		t.Fatalf("adapter panic=%+v", result)
	}
}

func TestCredentialAndDelegationStartupFailuresAreTyped(t *testing.T) {
	root := t.TempDir()
	project := writeProject(t, root)
	config := testConfig(t, root, project)
	config.CredentialRefs = []desktopcontract.CredentialRef{{ID: "registry"}}
	config.HostPorts.Credentials = credentialFailure{}
	app, _ := New(config)
	result := app.Start(context.Background())
	if result.Failure == nil || result.Failure.Code != desktopcontract.FailureCredential {
		t.Fatalf("credential failure=%+v", result)
	}

	config = testConfig(t, filepath.Join(root, "panic"), writeProject(t, filepath.Join(root, "panic")))
	config.CredentialRefs = []desktopcontract.CredentialRef{{ID: "registry"}}
	config.HostPorts.Credentials = credentialFailure{panic: true}
	app, _ = New(config)
	result = app.Start(context.Background())
	if result.Failure == nil || result.Failure.Code != desktopcontract.FailureBackendPanic {
		t.Fatalf("credential panic=%+v", result)
	}

	config = testConfig(t, filepath.Join(root, "delegation"), writeProject(t, filepath.Join(root, "delegation")))
	config.DelegationFences = []desktopcontract.DelegationFence{{DelegationID: "delegation", DocumentID: "document", LocalScopeID: "local", Generation: "1"}}
	config.HostPorts.Delegations = delegationFailure{}
	app, _ = New(config)
	result = app.Start(context.Background())
	if result.Failure == nil || result.Failure.Code != desktopcontract.FailureAgentDelegation {
		t.Fatalf("delegation failure=%+v", result)
	}
}

func TestCredentialAndDelegationStartupSuccess(t *testing.T) {
	root := t.TempDir()
	config := testConfig(t, root, writeProject(t, root))
	config.Now = func() time.Time { return desktopTestNow.In(time.FixedZone("JST", 9*60*60)) }
	config.CredentialRefs = []desktopcontract.CredentialRef{{ID: "registry"}}
	config.DelegationFences = []desktopcontract.DelegationFence{{DelegationID: "delegation", DocumentID: "document", LocalScopeID: "local", Generation: "1"}}
	app, _ := New(config)
	if result := app.Start(context.Background()); result.Outcome != protocolcommon.OutcomeSuccess {
		t.Fatalf("typed startup=%+v", result)
	}
	_ = app.Shutdown(context.Background())
}

func TestDelegationStartupValidatesOwnerGrantAndFence(t *testing.T) {
	newConfig := func(name string) Config {
		root := filepath.Join(t.TempDir(), name)
		config := testConfig(t, root, writeProject(t, root))
		config.DelegationFences = []desktopcontract.DelegationFence{{DelegationID: "delegation", DocumentID: "document", LocalScopeID: "local", Generation: "1"}}
		return config
	}
	for _, owner := range []desktopcontract.LocalOwnerGrantPort{localOwnerFailure{}, localOwnerFailure{panic: true}} {
		config := newConfig("owner")
		config.HostPorts.LocalOwner = owner
		app, _ := New(config)
		result := app.Start(context.Background())
		if result.Failure == nil || (result.Failure.Code != desktopcontract.FailureAgentDelegation && result.Failure.Code != desktopcontract.FailureBackendPanic) {
			t.Fatalf("invalid owner grant accepted: %+v", result)
		}
	}

	valid := accesscore.Delegation{
		ID: "delegation", ParentActor: accessprotocol.ActorRef{ActorID: "desktop-test-owner", Kind: "user"},
		Agent:      accessprotocol.ActorRef{ActorID: "desktop-test-agent", Kind: "agent"},
		DocumentID: "document", LocalScopeID: "local", AuthoringCapabilities: accesscore.FullAuthoringCapabilities(),
		Permissions: accesscore.AgentPermissions{Read: true}, IssuedAt: desktopTestNow.Add(-time.Minute),
		ExpiresAt: desktopTestNow.Add(time.Hour), Generation: 1,
	}
	invalid := []accesscore.Delegation{valid, valid, valid}
	invalid[0].DocumentID = "other"
	invalid[1].Generation = 2
	invalid[2].ExpiresAt = desktopTestNow
	for index, delegation := range invalid {
		config := newConfig("fence")
		config.HostPorts.Delegations = delegationValueStub{delegation: delegation}
		app, _ := New(config)
		result := app.Start(context.Background())
		if result.Failure == nil || result.Failure.Code != desktopcontract.FailureAgentDelegation {
			t.Fatalf("invalid delegation %d accepted: %+v", index, result)
		}
	}
}

func TestStartupRollbackFailureRetainsInventoryForShutdownRetry(t *testing.T) {
	root := t.TempDir()
	config := testConfig(t, root, writeProject(t, root))
	failingStop := &adapterStub{id: desktopcontract.ComponentReview, stopErr: errors.New("private shutdown failure")}
	config.Adapters[desktopcontract.ComponentReview] = failingStop
	config.Adapters[desktopcontract.ComponentBindingShell] = &adapterStub{id: desktopcontract.ComponentBindingShell, startErr: errors.New("private startup failure")}
	app, err := New(config)
	if err != nil {
		t.Fatal(err)
	}
	started := app.Start(context.Background())
	if started.Failure == nil || started.Failure.Code != desktopcontract.FailureShutdown || started.Failure.Component != desktopcontract.ComponentReview || app.State() != desktopcontract.LifecycleDraining {
		t.Fatalf("rollback failure lost: result=%+v state=%s", started, app.State())
	}
	failingStop.stopErr = nil
	if stopped := app.Shutdown(context.Background()); stopped.Outcome != protocolcommon.OutcomeSuccess || app.State() != desktopcontract.LifecycleStopped {
		t.Fatalf("rollback retry failed: result=%+v state=%s", stopped, app.State())
	}
}

func TestAdditionalStartupPanicsAndCapabilityMismatchFailClosed(t *testing.T) {
	root := t.TempDir()
	project := writeProject(t, root)
	config := testConfig(t, root, project)
	config.HostPorts.LocalActor = panicActorPort{}
	app, _ := New(config)
	if result := app.Start(context.Background()); result.Failure == nil || result.Failure.Code != desktopcontract.FailureBackendPanic {
		t.Fatalf("actor panic=%+v", result)
	}

	delegationRoot := filepath.Join(root, "delegation-panic")
	config = testConfig(t, delegationRoot, writeProject(t, delegationRoot))
	config.DelegationFences = []desktopcontract.DelegationFence{{DelegationID: "delegation", DocumentID: "document", LocalScopeID: "local", Generation: "1"}}
	config.HostPorts.Delegations = delegationFailure{panic: true}
	app, _ = New(config)
	if result := app.Start(context.Background()); result.Failure == nil || result.Failure.Code != desktopcontract.FailureBackendPanic {
		t.Fatalf("delegation panic=%+v", result)
	}

	adapterRoot := filepath.Join(root, "adapter-panic")
	config = testConfig(t, adapterRoot, writeProject(t, adapterRoot))
	config.Adapters[desktopcontract.ComponentReview] = panicAdapter{}
	app, _ = New(config)
	if result := app.Start(context.Background()); result.Failure == nil || result.Failure.Code != desktopcontract.FailureBackendPanic {
		t.Fatalf("adapter panic=%+v", result)
	}

	externalRoot := filepath.Join(root, "external-mismatch")
	config = testConfig(t, externalRoot, writeProject(t, externalRoot))
	config.Adapters[desktopcontract.ComponentExternalStorage] = &adapterStub{id: desktopcontract.ComponentExternalStorage}
	app, _ = New(config)
	if result := app.Start(context.Background()); result.Failure == nil || result.Failure.Component != desktopcontract.ComponentExternalStorage {
		t.Fatalf("external mismatch=%+v", result)
	}

	mcpRoot := filepath.Join(root, "mcp-failure")
	config = testConfig(t, mcpRoot, writeProject(t, mcpRoot))
	config.HostPorts.MCP = mcpPortStub{startFail: true}
	app, _ = New(config)
	if result := app.Start(context.Background()); result.Failure == nil || result.Failure.Code != desktopcontract.FailureMCPTransport {
		t.Fatalf("MCP failure=%+v", result)
	}
}

func TestReadyPublicationPrecedesAdmission(t *testing.T) {
	root := t.TempDir()
	config := testConfig(t, root, writeProject(t, root))
	ready := make(chan struct{})
	release := make(chan struct{})
	config.Lifecycle = &lifecycleRecorder{ready: ready, release: release}
	app, err := New(config)
	if err != nil {
		t.Fatal(err)
	}
	started := make(chan desktopcontract.Result[protocolcommon.HandshakeResult], 1)
	go func() { started <- app.Start(context.Background()) }()
	<-ready
	if app.State() != desktopcontract.LifecycleStarting {
		t.Fatalf("state before ready publication=%s", app.State())
	}
	if opened := app.OpenProject(context.Background(), "opaque"); opened.Failure == nil || opened.Failure.Code != desktopcontract.FailureReconnect {
		t.Fatalf("request admitted before ready publication: %+v", opened)
	}
	close(release)
	if result := <-started; result.Outcome != protocolcommon.OutcomeSuccess || app.State() != desktopcontract.LifecycleReady {
		t.Fatalf("start=%+v state=%s", result, app.State())
	}
	_ = app.Shutdown(context.Background())
}

func TestReadyFailureRollsBackWithoutAdmission(t *testing.T) {
	root := t.TempDir()
	config := testConfig(t, root, writeProject(t, root))
	config.Lifecycle = &lifecycleRecorder{fail: desktopcontract.LifecycleReady}
	app, _ := New(config)
	result := app.Start(context.Background())
	if result.Failure == nil || result.Failure.Code != desktopcontract.FailureStartup || app.State() != desktopcontract.LifecycleStopped {
		t.Fatalf("ready failure=%+v state=%s", result, app.State())
	}
	if opened := app.OpenProject(context.Background(), "opaque"); opened.Failure == nil {
		t.Fatalf("request admitted after failed ready publication: %+v", opened)
	}
}

func TestInjectedPortPanicsNeverEscape(t *testing.T) {
	root := t.TempDir()
	project := writeProject(t, root)
	config := testConfig(t, root, project)
	config.Lifecycle = panicLifecycle{}
	app, _ := New(config)
	if result := app.Start(context.Background()); result.Failure == nil || result.Failure.Code != desktopcontract.FailureBackendPanic {
		t.Fatalf("lifecycle panic=%+v", result)
	}

	other := filepath.Join(root, "negotiator")
	config = testConfig(t, other, writeProject(t, other))
	config.Capabilities = panicNegotiator{}
	app, _ = New(config)
	if result := app.Start(context.Background()); result.Failure == nil || result.Failure.Code != desktopcontract.FailureBackendPanic {
		t.Fatalf("negotiator panic=%+v", result)
	}

	reporterRoot := filepath.Join(root, "reporter")
	config = testConfig(t, reporterRoot, writeProject(t, reporterRoot))
	config.CredentialRefs = []desktopcontract.CredentialRef{{ID: "registry"}}
	config.HostPorts.Credentials = credentialFailure{}
	config.Recovery = panicReporter{}
	app, _ = New(config)
	if result := app.Start(context.Background()); result.Failure == nil || result.Failure.Code != desktopcontract.FailureCredential {
		t.Fatalf("reporter panic changed result=%+v", result)
	}
}

type panicAdapter struct{}

func (panicAdapter) Start(context.Context) error    { panic("secret") }
func (panicAdapter) Shutdown(context.Context) error { panic("secret") }

type stopPanicAdapter struct{}

func (stopPanicAdapter) Start(context.Context) error    { return nil }
func (stopPanicAdapter) Shutdown(context.Context) error { panic("secret") }

type nonComparableAdapter struct{ values []string }

func (nonComparableAdapter) Start(context.Context) error    { return nil }
func (nonComparableAdapter) Shutdown(context.Context) error { return nil }

func TestAdapterAlreadyStartedUsesExactComparableIdentity(t *testing.T) {
	started := &adapterStub{id: desktopcontract.ComponentReview}
	app := &Application{
		config:  Config{Adapters: map[desktopcontract.ComponentID]Adapter{desktopcontract.ComponentReview: started}},
		started: []desktopcontract.ComponentID{desktopcontract.ComponentReview},
	}
	if !app.adapterAlreadyStarted(started) {
		t.Fatal("started adapter identity was not detected")
	}
	if app.adapterAlreadyStarted(&adapterStub{id: desktopcontract.ComponentReview}) || app.adapterAlreadyStarted(panicAdapter{}) || app.adapterAlreadyStarted(nonComparableAdapter{values: []string{"value"}}) || app.adapterAlreadyStarted(nil) {
		t.Fatal("distinct, mismatched, non-comparable, or nil adapter reused a started identity")
	}
}

func TestShutdownContinuesAfterAdapterFailures(t *testing.T) {
	root := t.TempDir()
	config := testConfig(t, root, writeProject(t, root))
	config.Adapters[desktopcontract.ComponentReview] = &adapterStub{id: desktopcontract.ComponentReview, stopErr: errors.New("stop failed")}
	app, _ := New(config)
	if app.Start(context.Background()).Outcome != protocolcommon.OutcomeSuccess {
		t.Fatal("start failed")
	}
	result := app.Shutdown(context.Background())
	if result.Failure == nil || result.Failure.Component != desktopcontract.ComponentReview || app.State() != desktopcontract.LifecycleDraining {
		t.Fatalf("shutdown=%+v state=%s", result, app.State())
	}
	config.Adapters[desktopcontract.ComponentReview].(*adapterStub).stopErr = nil
	if retried := app.Shutdown(context.Background()); retried.Outcome != protocolcommon.OutcomeSuccess || app.State() != desktopcontract.LifecycleStopped {
		t.Fatalf("retried shutdown=%+v state=%s", retried, app.State())
	}
}

func TestShutdownSanitizesAdapterPanic(t *testing.T) {
	root := t.TempDir()
	config := testConfig(t, root, writeProject(t, root))
	config.Adapters[desktopcontract.ComponentReview] = stopPanicAdapter{}
	app, _ := New(config)
	if app.Start(context.Background()).Outcome != protocolcommon.OutcomeSuccess {
		t.Fatal("start failed")
	}
	result := app.Shutdown(context.Background())
	if result.Failure == nil || result.Failure.Component != desktopcontract.ComponentReview {
		t.Fatalf("shutdown panic=%+v", result)
	}
	config.Adapters[desktopcontract.ComponentReview] = &adapterStub{id: desktopcontract.ComponentReview}
	if retried := app.Shutdown(context.Background()); retried.Outcome != protocolcommon.OutcomeSuccess {
		t.Fatalf("retried panic shutdown=%+v", retried)
	}
}

func TestShutdownLifecycleAndMCPFailuresRemainResumable(t *testing.T) {
	root := t.TempDir()
	config := testConfig(t, root, writeProject(t, root))
	lifecycle := &lifecycleRecorder{}
	config.Lifecycle = lifecycle
	app, _ := New(config)
	if app.Start(context.Background()).Outcome != protocolcommon.OutcomeSuccess {
		t.Fatal("start failed")
	}
	lifecycle.fail = desktopcontract.LifecycleDraining
	if result := app.Shutdown(context.Background()); result.Failure == nil || app.State() != desktopcontract.LifecycleDraining {
		t.Fatalf("lifecycle shutdown=%+v state=%s", result, app.State())
	}
	lifecycle.fail = ""
	app.config.HostPorts.MCP = mcpPortStub{stopFail: true}
	if result := app.Shutdown(context.Background()); result.Failure == nil || result.Failure.Component != desktopcontract.ComponentMCPHost || app.State() != desktopcontract.LifecycleDraining {
		t.Fatalf("MCP shutdown=%+v state=%s", result, app.State())
	}
	app.config.HostPorts.MCP = mcpPortStub{}
	if result := app.Shutdown(context.Background()); result.Outcome != protocolcommon.OutcomeSuccess {
		t.Fatalf("resumed shutdown=%+v", result)
	}
}

func TestStartupFailureRollsBackAndSanitizes(t *testing.T) {
	root := t.TempDir()
	config := testConfig(t, root, writeProject(t, root))
	config.Adapters[desktopcontract.ComponentRegistryClient] = &adapterStub{id: desktopcontract.ComponentRegistryClient, startErr: errors.New("secret /native/path credential=abc")}
	app, err := New(config)
	if err != nil {
		t.Fatal(err)
	}
	result := app.Start(context.Background())
	if result.Failure == nil || result.Failure.Component != desktopcontract.ComponentRegistryClient || result.Failure.Code != desktopcontract.FailureStartup || app.State() != desktopcontract.LifecycleStopped {
		t.Fatalf("result=%+v state=%s", result, app.State())
	}
	wire, _ := json.Marshal(result)
	if string(wire) == "" || containsAny(string(wire), "secret", "/native/path", "credential") {
		t.Fatalf("private adapter error leaked: %s", wire)
	}
}

func TestCorruptStateEntersRecoveryWithoutReset(t *testing.T) {
	root := t.TempDir()
	project := writeProject(t, root)
	corrupt := []byte(`{"version":99,"bindings":{}}`)
	dataRoot := filepath.Join(root, "desktop-data")
	if err := os.MkdirAll(dataRoot, 0o700); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(dataRoot, "local-document-bindings.json")
	if err := os.WriteFile(path, corrupt, 0o600); err != nil {
		t.Fatal(err)
	}
	app, err := New(testConfig(t, root, project))
	if err != nil {
		t.Fatal(err)
	}
	result := app.Start(context.Background())
	if result.Failure == nil || result.Failure.Recovery != desktopcontract.RecoveryOpenRecovery || app.State() != desktopcontract.LifecycleRecovery {
		t.Fatalf("result=%+v state=%s", result, app.State())
	}
	got, err := os.ReadFile(path)
	if err != nil || !reflect.DeepEqual(got, corrupt) {
		t.Fatalf("corrupt state was reset: %q err=%v", got, err)
	}
}

func TestProjectCreateOpenReloadCloseAndRestartDurability(t *testing.T) {
	root := t.TempDir()
	project := writeProject(t, root)
	app, err := New(testConfig(t, root, project))
	if err != nil || app.Start(context.Background()).Outcome != protocolcommon.OutcomeSuccess {
		t.Fatalf("start err=%v", err)
	}
	created := app.CreateProject(context.Background(), "opaque-create-token")
	if created.Outcome != protocolcommon.OutcomeSuccess {
		t.Fatalf("create=%+v", created)
	}
	documentID := created.Value.Open.CommittedRevision.DocumentID
	cancelled := app.OpenProject(context.Background(), "")
	if cancelled.Outcome != protocolcommon.OutcomeCancelled || !cancelled.Validate() {
		t.Fatalf("dialog cancellation=%+v", cancelled)
	}
	if closed := app.CloseProject(context.Background(), created.Value.Open.Session); closed.Outcome != protocolcommon.OutcomeSuccess {
		t.Fatalf("close=%+v", closed)
	}
	reloaded := app.ReloadProject(context.Background(), documentID)
	if reloaded.Outcome != protocolcommon.OutcomeSuccess || reloaded.Value.Open.CommittedRevision.DocumentID != documentID {
		t.Fatalf("reload=%+v", reloaded)
	}
	_ = app.CloseProject(context.Background(), reloaded.Value.Open.Session)
	if app.Shutdown(context.Background()).Outcome != protocolcommon.OutcomeSuccess {
		t.Fatal("first shutdown failed")
	}

	restarted, err := New(testConfig(t, root, project))
	if err != nil || restarted.Start(context.Background()).Outcome != protocolcommon.OutcomeSuccess {
		t.Fatalf("restart err=%v", err)
	}
	afterRestart := restarted.ReloadProject(context.Background(), documentID)
	if afterRestart.Outcome != protocolcommon.OutcomeSuccess || afterRestart.Value.Open.CommittedRevision.RevisionID != created.Value.Open.CommittedRevision.RevisionID {
		t.Fatalf("durable revision unavailable: %+v", afterRestart)
	}
	_ = restarted.Shutdown(context.Background())
}

func TestProjectOpenAndReloadFailClosedWhenNativeSearchRefreshFails(t *testing.T) {
	root := t.TempDir()
	project := writeProject(t, root)
	config := testConfig(t, root, project)
	failing := &nativeSearchLifecycleStub{err: errors.New("native search unavailable")}
	config.NativeSearchLifecycle = failing
	app, err := New(config)
	if err != nil || app.Start(context.Background()).Outcome != protocolcommon.OutcomeSuccess {
		t.Fatalf("start err=%v", err)
	}
	opened := app.OpenProject(context.Background(), "opaque")
	if opened.Failure == nil || opened.Failure.Component != desktopcontract.ComponentNativeQuery || failing.calls != 1 {
		t.Fatalf("open=%+v refresh calls=%d", opened, failing.calls)
	}
	_ = app.Shutdown(context.Background())

	reloadConfig := testConfig(t, root, project)
	restarted, err := New(reloadConfig)
	if err != nil || restarted.Start(context.Background()).Outcome != protocolcommon.OutcomeSuccess {
		t.Fatalf("restart err=%v", err)
	}
	initial := restarted.OpenProject(context.Background(), "opaque")
	if initial.Outcome != protocolcommon.OutcomeSuccess {
		t.Fatalf("initial open=%+v", initial)
	}
	documentID := initial.Value.ProjectID
	if closed := restarted.CloseProject(context.Background(), initial.Value.Open.Session); closed.Outcome != protocolcommon.OutcomeSuccess {
		t.Fatalf("close=%+v", closed)
	}
	restarted.config.NativeSearchLifecycle = failing
	reloaded := restarted.ReloadProject(context.Background(), documentID)
	if reloaded.Failure == nil || reloaded.Failure.Component != desktopcontract.ComponentNativeQuery || failing.calls != 2 {
		t.Fatalf("reload=%+v refresh calls=%d", reloaded, failing.calls)
	}
	_ = restarted.Shutdown(context.Background())
}

func TestShutdownRejectsNewWorkAndDoesNotReleaseDuringInflight(t *testing.T) {
	root := t.TempDir()
	config := testConfig(t, root, writeProject(t, root))
	app, err := New(config)
	if err != nil || app.Start(context.Background()).Outcome != protocolcommon.OutcomeSuccess {
		t.Fatal(err)
	}
	done, failure := app.begin(desktopcontract.ComponentRuntime)
	if failure != nil {
		t.Fatal(failure)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Millisecond)
	defer cancel()
	shutdown := app.Shutdown(ctx)
	if shutdown.Failure == nil || app.State() != desktopcontract.LifecycleDraining {
		t.Fatalf("interrupted shutdown=%+v state=%s", shutdown, app.State())
	}
	if opened := app.OpenProject(context.Background(), "opaque"); opened.Failure == nil || opened.Failure.Code != desktopcontract.FailureShutdown {
		t.Fatalf("new request accepted while draining: %+v", opened)
	}
	done()
	if result := app.Shutdown(context.Background()); result.Outcome != protocolcommon.OutcomeSuccess {
		t.Fatalf("resumed shutdown=%+v", result)
	}
}

func TestConcurrentProjectRequestsAreTrackedAndClosable(t *testing.T) {
	root := t.TempDir()
	app, err := New(testConfig(t, root, writeProject(t, root)))
	if err != nil || app.Start(context.Background()).Outcome != protocolcommon.OutcomeSuccess {
		t.Fatal(err)
	}
	const count = 8
	results := make(chan desktopcontract.Result[ProjectOpenResult], count)
	var group sync.WaitGroup
	for index := 0; index < count; index++ {
		group.Add(1)
		go func() {
			defer group.Done()
			results <- app.OpenProject(context.Background(), "opaque")
		}()
	}
	group.Wait()
	close(results)
	var session runtimeprotocol.RuntimeSessionRef
	openedCount, focusedCount := 0, 0
	for result := range results {
		if result.Outcome != protocolcommon.OutcomeSuccess {
			t.Fatalf("concurrent open=%+v", result)
		}
		if session.RuntimeSessionID == "" {
			session = result.Value.Open.Session
		} else if result.Value.Open.Session != session {
			t.Fatalf("concurrent open did not focus stable session: %+v", result)
		}
		switch result.Value.Disposition {
		case ProjectOpened:
			openedCount++
		case ProjectFocused:
			focusedCount++
		}
	}
	if openedCount != 1 || focusedCount != count-1 {
		t.Fatalf("dispositions opened=%d focused=%d", openedCount, focusedCount)
	}
	if closed := app.CloseProject(context.Background(), session); closed.Outcome != protocolcommon.OutcomeSuccess {
		t.Fatalf("concurrent close=%+v", closed)
	}
	if stopped := app.Shutdown(context.Background()); stopped.Outcome != protocolcommon.OutcomeSuccess {
		t.Fatalf("shutdown=%+v", stopped)
	}
}

func TestBindingPanicAndUnknownMethodAreClosedFailures(t *testing.T) {
	root := t.TempDir()
	config := testConfig(t, root, writeProject(t, root))
	config.Bindings.Review.Submit = func(context.Context, desktopcontract.Exchange) (desktopcontract.ExchangeResult, error) {
		panic("document secret")
	}
	app, err := New(config)
	if err != nil || app.Start(context.Background()).Outcome != protocolcommon.OutcomeSuccess {
		t.Fatal(err)
	}
	control := []byte(`{"operation":"review.submit","request_id":"request"}`)
	panicked := app.Invoke(context.Background(), "ReviewSubmit", desktopcontract.Exchange{Operation: "review.submit", Control: control})
	if panicked.Failure == nil || panicked.Failure.Code != desktopcontract.FailureBackendPanic {
		t.Fatalf("panic result=%+v failure=%+v", panicked, panicked.Failure)
	}
	unknown := app.Invoke(context.Background(), "InternalMethod", desktopcontract.Exchange{Operation: "internal.read", Control: control})
	if unknown.Failure == nil || unknown.Failure.Code != desktopcontract.FailureProtocolIncompatible {
		t.Fatalf("unknown=%+v", unknown)
	}
	_ = app.Shutdown(context.Background())
}

func TestBindingSuccessAndProjectFailuresAreNormalized(t *testing.T) {
	root := t.TempDir()
	config := testConfig(t, root, writeProject(t, root))
	app, _ := New(config)
	if app.Start(context.Background()).Outcome != protocolcommon.OutcomeSuccess {
		t.Fatal("start failed")
	}
	control := []byte(`{"operation":"review.submit","request_id":"request"}`)
	invoked := app.Invoke(context.Background(), "ReviewSubmit", desktopcontract.Exchange{Operation: "review.submit", Control: control})
	if invoked.Outcome != protocolcommon.OutcomeSuccess || invoked.Value.Operation != "review.submit" || !invoked.Validate() {
		t.Fatalf("invoke=%+v", invoked)
	}
	if result := app.ReloadProject(context.Background(), "missing_document"); result.Failure == nil {
		t.Fatalf("missing reload=%+v", result)
	}
	invalidSession := runtimeprotocol.RuntimeSessionRef{}
	if result := app.Preview(context.Background(), runtimeprotocol.PreviewOperationsInput{}); result.Failure == nil {
		t.Fatalf("invalid preview=%+v", result)
	}
	if result := app.Commit(context.Background(), runtimeprotocol.RuntimeCommitInput{}); result.Failure == nil {
		t.Fatalf("invalid commit=%+v", result)
	}
	if result := app.CloseProject(context.Background(), invalidSession); result.Failure == nil {
		t.Fatalf("invalid close=%+v", result)
	}
	_ = app.Shutdown(context.Background())

	config = testConfig(t, filepath.Join(root, "other"), writeProject(t, filepath.Join(root, "other")))
	config.ProjectStorage = storageStub{ProjectLocation{Root: "relative", EntryPath: "document.ldl"}}
	app, _ = New(config)
	_ = app.Start(context.Background())
	if result := app.OpenProject(context.Background(), "opaque"); result.Failure == nil || result.Failure.Component != desktopcontract.ComponentLocalStorage {
		t.Fatalf("invalid location=%+v", result)
	}
	_ = app.Shutdown(context.Background())

	third := filepath.Join(root, "third")
	config = testConfig(t, third, writeProject(t, third))
	config.ProjectStorage = panicStorage{}
	app, _ = New(config)
	_ = app.Start(context.Background())
	if result := app.OpenProject(context.Background(), "opaque"); result.Failure == nil || result.Failure.Code != desktopcontract.FailureBackendPanic {
		t.Fatalf("storage panic=%+v", result)
	}
	_ = app.Shutdown(context.Background())
}

func TestBindingPreservesRejectedFailedAndCancelledOutcomes(t *testing.T) {
	for _, outcome := range []protocolcommon.Outcome{protocolcommon.OutcomeRejected, protocolcommon.OutcomeFailed, protocolcommon.OutcomeCancelled} {
		root := t.TempDir()
		config := testConfig(t, root, writeProject(t, root))
		config.Bindings.Review.Submit = func(_ context.Context, exchange desktopcontract.Exchange) (desktopcontract.ExchangeResult, error) {
			control, _ := json.Marshal(map[string]string{"operation": exchange.Operation, "request_id": "request", "outcome": string(outcome)})
			return desktopcontract.ExchangeResult{Operation: exchange.Operation, Control: control}, nil
		}
		app, _ := New(config)
		if app.Start(context.Background()).Outcome != protocolcommon.OutcomeSuccess {
			t.Fatal("start failed")
		}
		control := []byte(`{"operation":"review.submit","request_id":"request"}`)
		result := app.Invoke(context.Background(), "ReviewSubmit", desktopcontract.Exchange{Operation: "review.submit", Control: control})
		if result.Outcome != outcome || result.Failure != nil || !result.Validate() {
			t.Fatalf("outcome %s was not preserved: %+v", outcome, result)
		}
		_ = app.Shutdown(context.Background())
	}
}

func TestBindingRejectsInvalidOutcomeAndValidationShapes(t *testing.T) {
	root := t.TempDir()
	config := testConfig(t, root, writeProject(t, root))
	config.Bindings.Review.Submit = func(_ context.Context, exchange desktopcontract.Exchange) (desktopcontract.ExchangeResult, error) {
		return desktopcontract.ExchangeResult{Operation: exchange.Operation, Control: []byte(`{"operation":"review.submit","request_id":"request","outcome":"other"}`)}, nil
	}
	app, _ := New(config)
	_ = app.Start(context.Background())
	control := []byte(`{"operation":"review.submit","request_id":"request"}`)
	result := app.Invoke(context.Background(), "ReviewSubmit", desktopcontract.Exchange{Operation: "review.submit", Control: control})
	if result.Failure == nil || result.Failure.Code != desktopcontract.FailureProtocolIncompatible || !result.Validate() {
		t.Fatalf("invalid outcome=%+v", result)
	}
	if (BindingResult{Outcome: "other"}).Validate() || (BindingResult{Outcome: protocolcommon.OutcomeSuccess}).Validate() {
		t.Fatal("invalid binding result shape accepted")
	}
	_ = app.Shutdown(context.Background())
}

func TestCommitSurvivesRestart(t *testing.T) {
	root := t.TempDir()
	project := writeProject(t, root)
	app, err := New(testConfig(t, root, project))
	if err != nil || app.Start(context.Background()).Outcome != protocolcommon.OutcomeSuccess {
		t.Fatal(err)
	}
	opened := app.OpenProject(context.Background(), "opaque")
	if opened.Outcome != protocolcommon.OutcomeSuccess {
		t.Fatal(opened.Failure)
	}
	batch, err := engineprotocol.DecodeSemanticOperationBatch([]byte(`{"operations":[{"operation":"create_subject","subject_kind":"layer","parent_address":"ldl:project:p","id":"desktop_layer","fields":{"display_name":"Desktop","order":"1"}}]}`))
	if err != nil {
		t.Fatal(err)
	}
	preconditions := preconditionsFor(t, "project p \"P\" {}\n")
	input := runtimeprotocol.RuntimeCommitInput{Session: opened.Value.Open.Session, OperationID: "desktop_commit", IdempotencyKey: "desktop_commit_idempotency", OperationBatch: runtimeprotocol.RuntimeOperationBatch{DocumentID: opened.Value.Open.CommittedRevision.DocumentID, BaseRevision: opened.Value.Open.CommittedRevision, ExpectedDefinitionHash: opened.Value.Open.CommittedRevision.DefinitionHash, Operations: batch, Preconditions: preconditions}}
	preview := app.Preview(context.Background(), runtimeprotocol.PreviewOperationsInput{Session: input.Session, OperationBatch: input.OperationBatch})
	if preview.Outcome != protocolcommon.OutcomeSuccess || preview.Value.DefinitionHash == opened.Value.Open.CommittedRevision.DefinitionHash {
		t.Fatalf("preview=%+v", preview)
	}
	committed := app.Commit(context.Background(), input)
	if committed.Outcome != protocolcommon.OutcomeSuccess || committed.Value.OperationResult.CommittedRevision == nil {
		t.Fatalf("commit=%+v", committed)
	}
	revision := *committed.Value.OperationResult.CommittedRevision
	_ = app.Shutdown(context.Background())
	restarted, err := New(testConfig(t, root, project))
	if err != nil || restarted.Start(context.Background()).Outcome != protocolcommon.OutcomeSuccess {
		t.Fatal(err)
	}
	reloaded := restarted.ReloadProject(context.Background(), revision.DocumentID)
	if reloaded.Outcome != protocolcommon.OutcomeSuccess || reloaded.Value.Open.CommittedRevision.RevisionID != revision.RevisionID {
		t.Fatalf("committed revision lost: %+v", reloaded)
	}
	_ = restarted.Shutdown(context.Background())
}

func preconditionsFor(t *testing.T, source string) engineprotocol.EngineEditPreconditions {
	t.Helper()
	result, err := engine.New(engine.BuildInfo{}).Compile(context.Background(), engine.CompileInput{Mode: engine.CompileProject, EntryPath: "document.ldl", ProjectSourceTree: map[string][]byte{"document.ldl": []byte(source)}, ResolvedDependencies: engine.ResolvedDependencies{Format: "layerdraw-resolved", FormatVersion: 1, Language: 1}})
	if err != nil {
		t.Fatal(err)
	}
	snapshot := result.Snapshot()
	preconditions := engineprotocol.EngineEditPreconditions{DocumentGeneration: engineprotocol.DocumentGeneration{DocumentHandle: engineprotocol.DocumentHandle{EndpointInstanceID: "placeholder", Value: "document_placeholder_123456"}, Value: "1"}, ExpectedSubjectHashes: []engineprotocol.ExpectedHash{}, ExpectedSubtreeHashes: []engineprotocol.ExpectedHash{}, ExpectedChildSets: []engineprotocol.ExpectedChildSet{}}
	for _, item := range snapshot.SubjectSemanticHashes {
		preconditions.ExpectedSubjectHashes = append(preconditions.ExpectedSubjectHashes, engineprotocol.ExpectedHash{Address: semantic.StableAddress(item.Address), Hash: protocolcommon.Digest(item.Hash)})
	}
	for _, item := range snapshot.SubtreeHashes {
		preconditions.ExpectedSubtreeHashes = append(preconditions.ExpectedSubtreeHashes, engineprotocol.ExpectedHash{Address: semantic.StableAddress(item.OwnerAddress), Hash: protocolcommon.Digest(item.Hash)})
	}
	for _, item := range snapshot.ChildSetHashes {
		preconditions.ExpectedChildSets = append(preconditions.ExpectedChildSets, engineprotocol.ExpectedChildSet{OwnerAddress: semantic.StableAddress(item.OwnerAddress), ChildKind: semantic.SubjectKind(item.ChildKind), Hash: protocolcommon.Digest(item.Hash)})
	}
	sources := []engineprotocol.ExpectedSourceDigest{}
	for _, file := range snapshot.SourceMap.Files {
		origin := semantic.SourceOrigin{Kind: semantic.OriginKind(file.Origin.Kind)}
		if file.Origin.PackAddress != "" {
			value := semantic.PackRootAddress(file.Origin.PackAddress)
			origin.PackAddress = &value
		}
		sources = append(sources, engineprotocol.ExpectedSourceDigest{Module: semantic.ModuleRef{Origin: origin, ModulePath: file.ModulePath}, Digest: protocolcommon.Digest(file.Digest)})
	}
	preconditions.ExpectedSourceDigests = &sources
	return preconditions
}

func containsAny(value string, needles ...string) bool {
	for _, needle := range needles {
		if len(needle) > 0 && reflect.ValueOf(value).String() != "" && stringContains(value, needle) {
			return true
		}
	}
	return false
}

func stringContains(value, needle string) bool {
	for index := 0; index+len(needle) <= len(value); index++ {
		if value[index:index+len(needle)] == needle {
			return true
		}
	}
	return false
}
