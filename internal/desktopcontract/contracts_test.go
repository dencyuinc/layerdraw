// SPDX-License-Identifier: LicenseRef-LayerDraw-1.0

package desktopcontract

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"testing"
	"time"

	"github.com/dencyuinc/layerdraw/gen/go/accessprotocol"
	"github.com/dencyuinc/layerdraw/gen/go/engineprotocol"
	"github.com/dencyuinc/layerdraw/gen/go/protocolcommon"
	"github.com/dencyuinc/layerdraw/gen/go/runtimeprotocol"
	accesscore "github.com/dencyuinc/layerdraw/internal/access"
)

var repoRoot = filepath.Join("..", "..")

func fixture(t *testing.T, name string) []byte {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(repoRoot, filepath.FromSlash(name)))
	if err != nil {
		t.Fatal(err)
	}
	return data
}

func TestDefaultManifestFreezesDesktopClosure(t *testing.T) {
	manifest := DefaultManifest()
	if err := manifest.Validate(); err != nil {
		t.Fatal(err)
	}
	cases := []func(*Manifest){
		func(m *Manifest) { m.Version = 2 },
		func(m *Manifest) { m.Components = m.Components[1:] },
		func(m *Manifest) { m.Frontend[0] = "@layerdraw/other" },
		func(m *Manifest) { m.RequiredCapabilities = m.RequiredCapabilities[1:] },
		func(m *Manifest) { m.OptionalCapabilities = nil },
		func(m *Manifest) { m.OptionalCapabilities[0] = m.RequiredCapabilities[0] },
		func(m *Manifest) { m.RequiredCapabilities[0] = "INVALID" },
	}
	for index, mutate := range cases {
		candidate := DefaultManifest()
		mutate(&candidate)
		if err := candidate.Validate(); err == nil {
			t.Fatalf("manifest mutation %d was accepted", index)
		}
	}
	components := RequiredBackendClosure()
	assets := RequiredFrontendClosure()
	components[0], assets[0] = "mutated", "mutated"
	if DefaultManifest().Components[0] == "mutated" || DefaultManifest().Frontend[0] == "mutated" {
		t.Fatal("closure publication aliases package state")
	}
}

func TestGeneratedBindingFixtureUsesExactGeneratedDecoders(t *testing.T) {
	var value struct {
		Version  int      `json:"version"`
		Surfaces []string `json:"surfaces"`
		Bindings []struct {
			GeneratedMethod string        `json:"generated_method"`
			Target          BindingTarget `json:"target"`
			ClientMethod    string        `json:"client_method"`
			Operation       string        `json:"operation"`
			RequestFixture  string        `json:"request_fixture"`
		} `json:"bindings"`
		Outcomes []struct {
			Decoder string                 `json:"decoder"`
			Fixture string                 `json:"fixture"`
			Outcome protocolcommon.Outcome `json:"outcome"`
		} `json:"outcomes"`
		CapabilityStatuses []json.RawMessage `json:"capability_statuses"`
	}
	if err := json.Unmarshal(fixture(t, "schemas/fixtures/desktop/wails-binding-compatibility-v1.json"), &value); err != nil {
		t.Fatal(err)
	}
	if value.Version != 1 || !reflect.DeepEqual(value.Surfaces, []string{"browser", "desktop"}) || len(value.Bindings) == 0 {
		t.Fatal("empty compatibility fixture")
	}
	for _, vector := range value.Bindings {
		binding, err := ValidateExchange(vector.GeneratedMethod, Exchange{Operation: vector.Operation, Control: fixture(t, vector.RequestFixture)})
		if err != nil {
			t.Fatalf("%s: %v", vector.GeneratedMethod, err)
		}
		if binding.Target != vector.Target || binding.ClientMethod != vector.ClientMethod {
			t.Fatalf("binding drift: %+v", binding)
		}
	}
	wantOutcomes := []protocolcommon.Outcome{protocolcommon.OutcomeSuccess, protocolcommon.OutcomeRejected, protocolcommon.OutcomeFailed, protocolcommon.OutcomeCancelled}
	if len(value.Outcomes) != len(wantOutcomes) {
		t.Fatal("outcome fixture is incomplete")
	}
	for index, vector := range value.Outcomes {
		var outcome protocolcommon.Outcome
		switch vector.Decoder {
		case "compile":
			decoded, err := engineprotocol.DecodeCompileResponseEnvelope(fixture(t, vector.Fixture))
			if err != nil {
				t.Fatal(err)
			}
			outcome = decoded.Outcome
		case "handshake":
			decoded, err := engineprotocol.DecodeHandshakeResponseEnvelope(fixture(t, vector.Fixture))
			if err != nil {
				t.Fatal(err)
			}
			outcome = decoded.Outcome
		default:
			t.Fatalf("unknown generated decoder %q", vector.Decoder)
		}
		if outcome != vector.Outcome || outcome != wantOutcomes[index] {
			t.Fatalf("outcome drift: %q", outcome)
		}
	}
	if len(value.CapabilityStatuses) != len(DefaultManifest().RequiredCapabilities)+len(DefaultManifest().OptionalCapabilities) {
		t.Fatal("capability fixture is incomplete")
	}
	seen := map[protocolcommon.CapabilityID]bool{}
	for _, raw := range value.CapabilityStatuses {
		status, err := protocolcommon.DecodeRequestedCapabilityStatus(raw)
		if err != nil {
			t.Fatal(err)
		}
		if status.CapabilityID == "" || seen[status.CapabilityID] || status.ProtocolVersion != DesktopProtocolVersion {
			t.Fatalf("invalid status %+v", status)
		}
		seen[status.CapabilityID] = true
	}
}

func TestEveryApprovedBindingHasExactDecoderAndRejectsEmptyEnvelope(t *testing.T) {
	table := GeneratedBindingTable()
	if len(table) != 50 {
		t.Fatalf("binding closure = %d", len(table))
	}
	seenMethod, seenOperation := map[string]bool{}, map[string]bool{}
	for _, binding := range table {
		if binding.GeneratedMethod == "" || binding.ClientMethod == "" || binding.Operation == "" || seenMethod[binding.GeneratedMethod] || seenOperation[string(binding.Target)+binding.Operation] {
			t.Fatalf("invalid binding %+v", binding)
		}
		seenMethod[binding.GeneratedMethod], seenOperation[string(binding.Target)+binding.Operation] = true, true
		if _, err := ValidateExchange(binding.GeneratedMethod, Exchange{Operation: binding.Operation}); err == nil {
			t.Fatalf("empty %s envelope accepted", binding.GeneratedMethod)
		}
	}
	table[0].GeneratedMethod = "mutated"
	if GeneratedBindingTable()[0].GeneratedMethod == "mutated" {
		t.Fatal("binding table aliases package state")
	}
}

func TestBindingRejectsConfusedDeputyInputs(t *testing.T) {
	compile := fixture(t, "schemas/fixtures/engine/compile-request.json")
	for _, input := range []struct {
		method, operation string
		control           []byte
	}{
		{"EngineCompile", "runtime.handshake", compile},
		{"RuntimeHandshake", "runtime.handshake", compile},
		{"Unknown", "engine.compile", compile},
	} {
		if _, err := ValidateExchange(input.method, Exchange{Operation: input.operation, Control: input.control}); err == nil {
			t.Fatalf("accepted %+v", input)
		}
	}
}

func completeClientSet() ClientSet {
	method := ClientMethod(func(_ context.Context, exchange Exchange) (ExchangeResult, error) {
		return ExchangeResult{Control: append([]byte(nil), exchange.Control...)}, nil
	})
	fill := func(value any) {
		fields := reflect.ValueOf(value).Elem()
		for index := 0; index < fields.NumField(); index++ {
			fields.Field(index).Set(reflect.ValueOf(method))
		}
	}
	clients := ClientSet{}
	fill(&clients.Engine)
	fill(&clients.Runtime)
	fill(&clients.Registry)
	fill(&clients.Review)
	fill(&clients.Host)
	return clients
}

func TestClientSetInvokesOnlyExactValidatedMethods(t *testing.T) {
	clients := completeClientSet()
	if err := clients.Validate(); err != nil {
		t.Fatal(err)
	}
	for _, input := range []struct{ method, operation, fixture string }{
		{"EngineCompile", "engine.compile", "schemas/fixtures/engine/compile-request.json"},
		{"RuntimeHandshake", "runtime.handshake", "schemas/fixtures/runtime/handshake-request.json"},
		{"RegistryListSources", "registry.list_sources", "schemas/fixtures/desktop/registry-list-sources-request-v1.json"},
	} {
		control := fixture(t, input.fixture)
		result, err := clients.Invoke(context.Background(), input.method, Exchange{Operation: input.operation, Control: control})
		if err != nil || string(result.Control) != string(control) {
			t.Fatalf("invoke %s: %v", input.method, err)
		}
	}
	clients.Engine.Compile = nil
	if err := clients.Validate(); err == nil {
		t.Fatal("missing exact client method accepted")
	}
	if _, err := clients.Invoke(context.Background(), "EngineCompile", Exchange{}); err == nil {
		t.Fatal("incomplete client set invoked")
	}
}

func handshakeTemplate(t *testing.T, statuses []protocolcommon.RequestedCapabilityStatus) protocolcommon.HandshakeResult {
	decoded, err := engineprotocol.DecodeHandshakeResponseEnvelope(fixture(t, "schemas/fixtures/engine/handshake-success.json"))
	if err != nil || decoded.Payload == nil {
		t.Fatalf("handshake fixture: %v", err)
	}
	result := *decoded.Payload
	result.CapabilityStatuses = statuses
	return result
}

func desktopStatuses() []protocolcommon.RequestedCapabilityStatus {
	manifest := DefaultManifest()
	ids := append(append([]protocolcommon.CapabilityID(nil), manifest.RequiredCapabilities...), manifest.OptionalCapabilities...)
	result := make([]protocolcommon.RequestedCapabilityStatus, 0, len(ids))
	for _, id := range ids {
		result = append(result, protocolcommon.RequestedCapabilityStatus{CapabilityID: id, Enabled: true, ProtocolVersion: DesktopProtocolVersion})
	}
	return result
}

func TestCapabilityNegotiationUsesGeneratedHandshakeAndDeepCopies(t *testing.T) {
	statuses := desktopStatuses()
	handshake := handshakeTemplate(t, statuses)
	result := NegotiateCapabilities(DefaultManifest(), handshake)
	if !result.Validate() || result.Outcome != protocolcommon.OutcomeSuccess {
		t.Fatalf("negotiation failed: %+v", result)
	}
	handshake.CapabilityStatuses[0].CapabilityID = "desktop.changed"
	handshake.CapabilityManifest.Transports[0] = "changed"
	handshake.CapabilityManifest.Operations["engine.compile"] = protocolcommon.OperationCapability{}
	if result.Value.CapabilityStatuses[0].CapabilityID == "desktop.changed" || result.Value.CapabilityManifest.Transports[0] == "changed" || result.Value.CapabilityManifest.Operations["engine.compile"].ProtocolVersion == "" {
		t.Fatal("capability result aliases mutable handshake input")
	}
}

func TestCapabilityNegotiationFailsClosed(t *testing.T) {
	reason := protocolcommon.UnavailableReasonNotConfigured
	cases := []struct {
		mutate func(*Manifest, *[]protocolcommon.RequestedCapabilityStatus)
		code   FailureCode
	}{
		{func(m *Manifest, _ *[]protocolcommon.RequestedCapabilityStatus) { m.Version = 2 }, FailureProtocolIncompatible},
		{func(_ *Manifest, s *[]protocolcommon.RequestedCapabilityStatus) { *s = (*s)[:len(*s)-1] }, FailureProtocolIncompatible},
		{func(_ *Manifest, s *[]protocolcommon.RequestedCapabilityStatus) {
			(*s)[1].CapabilityID = (*s)[0].CapabilityID
		}, FailureProtocolIncompatible},
		{func(_ *Manifest, s *[]protocolcommon.RequestedCapabilityStatus) { (*s)[0].ProtocolVersion = "2.0" }, FailureProtocolIncompatible},
		{func(_ *Manifest, s *[]protocolcommon.RequestedCapabilityStatus) {
			(*s)[0].Enabled = false
			(*s)[0].UnavailableReason = &reason
		}, FailureAdapterUnavailable},
	}
	for index, test := range cases {
		manifest, statuses := DefaultManifest(), desktopStatuses()
		test.mutate(&manifest, &statuses)
		result := NegotiateCapabilities(manifest, handshakeTemplate(t, statuses))
		if !result.Validate() || result.Failure == nil || result.Failure.Code != test.code {
			t.Fatalf("case %d: %+v", index, result)
		}
	}
	statuses := desktopStatuses()
	handshake := handshakeTemplate(t, statuses)
	handshake.NegotiatedProtocols[0].Version = "2.0"
	result := NegotiateCapabilities(DefaultManifest(), handshake)
	if result.Failure == nil || result.Failure.Code != FailureProtocolIncompatible {
		t.Fatalf("arbitrary negotiated protocol accepted: %+v", result)
	}
}

func TestResultAndFailureVocabulariesAreClosed(t *testing.T) {
	valid := Failure{Code: FailureStartup, Component: ComponentBindingShell, Recovery: RecoveryRetry}
	if !valid.Validate() {
		t.Fatal("valid failure rejected")
	}
	for _, failure := range []Failure{{Code: "desktop.other", Component: ComponentBindingShell, Recovery: RecoveryRetry}, {Code: FailureStartup, Component: "other", Recovery: RecoveryRetry}, {Code: FailureStartup, Component: ComponentBindingShell, Recovery: "other"}} {
		if failure.Validate() {
			t.Fatalf("open failure accepted: %+v", failure)
		}
	}
	for _, result := range []Result[string]{
		{Outcome: protocolcommon.OutcomeSuccess}, {Outcome: protocolcommon.OutcomeRejected},
		{Outcome: protocolcommon.OutcomeCancelled, Failure: &Failure{Code: FailureDialogCancelled, Component: ComponentBindingShell, Recovery: RecoveryRetry}},
		{Outcome: protocolcommon.OutcomeFailed, Failure: &valid},
	} {
		if !result.Validate() {
			t.Fatalf("valid result rejected: %+v", result)
		}
	}
	for _, result := range []Result[string]{{Outcome: "other"}, {Outcome: protocolcommon.OutcomeCancelled, Failure: &valid}, {Outcome: protocolcommon.OutcomeFailed}, {Outcome: protocolcommon.OutcomeFailed, Failure: &Failure{Code: FailureDialogCancelled, Component: ComponentBindingShell, Recovery: RecoveryRetry}}} {
		if result.Validate() {
			t.Fatalf("invalid result accepted: %+v", result)
		}
	}
}

func TestLocalOwnerAndDelegationUseAccessContracts(t *testing.T) {
	now := time.Date(2026, 7, 20, 1, 2, 3, 0, time.UTC)
	request := LocalOwnerGrantRequest{Actor: accessprotocol.ActorRef{ActorID: "local-user", Kind: "user"}, Scope: accessprotocol.HostResourceScope{DocumentID: "doc", LocalScopeID: "local"}, IssuedAt: protocolcommon.Rfc3339Time(now.Format(time.RFC3339Nano))}
	grant := accessprotocol.AuthoringGrantSnapshot{AccessFingerprint: "sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa", ActorRef: request.Actor, GrantedCapabilities: accesscore.FullAuthoringCapabilities(), HostDocumentID: "doc", IssuedAt: request.IssuedAt, LocalScopeID: "local", MembershipVersion: "1", PolicyRefs: []accessprotocol.PolicyRef{}}
	if !ValidateLocalOwnerGrant(request, grant) {
		t.Fatal("valid full-authoring local owner rejected")
	}
	grant.GrantedCapabilities = grant.GrantedCapabilities[1:]
	if ValidateLocalOwnerGrant(request, grant) {
		t.Fatal("partial local owner grant accepted")
	}
	grant.GrantedCapabilities = accesscore.FullAuthoringCapabilities()
	delegation := accesscore.Delegation{ID: "delegation", ParentActor: request.Actor, Agent: accessprotocol.ActorRef{ActorID: "agent", Kind: "agent"}, DocumentID: "doc", LocalScopeID: "local", AuthoringCapabilities: accesscore.FullAuthoringCapabilities(), Permissions: accesscore.AgentPermissions{Read: true, Export: true, Propose: true, Apply: true}, IssuedAt: now, ExpiresAt: now.Add(time.Hour), Generation: 3}
	requestDelegation := delegation
	requestDelegation.Generation = 0
	if !ValidateDelegationRequest(grant, requestDelegation) {
		t.Fatal("typed delegation request rejected")
	}
	requestDelegation.Permissions = accesscore.AgentPermissions{}
	if ValidateDelegationRequest(grant, requestDelegation) {
		t.Fatal("permissionless delegation accepted")
	}
	requestDelegation = delegation
	requestDelegation.Generation = 0
	requestDelegation.DocumentID = "other"
	if ValidateDelegationRequest(grant, requestDelegation) {
		t.Fatal("cross-document delegation accepted")
	}
	fence := DelegationFence{DelegationID: "delegation", DocumentID: "doc", LocalScopeID: "local", Generation: "3"}
	if !ValidateDelegationFence(fence, delegation) {
		t.Fatal("valid generation fence rejected")
	}
	fence.Generation = "2"
	if ValidateDelegationFence(fence, delegation) {
		t.Fatal("stale generation fence accepted")
	}
}

func TestRuntimeFixtureReallyUsesGeneratedEnvelope(t *testing.T) {
	if _, err := runtimeprotocol.DecodeRuntimeHandshakeRequestEnvelope(fixture(t, "schemas/fixtures/runtime/handshake-request.json")); err != nil {
		t.Fatal(err)
	}
}
