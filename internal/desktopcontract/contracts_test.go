// SPDX-License-Identifier: LicenseRef-LayerDraw-1.0

package desktopcontract

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
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
			GeneratedMethod       string        `json:"generated_method"`
			Target                BindingTarget `json:"target"`
			ClientMethod          string        `json:"client_method"`
			Operation             string        `json:"operation"`
			BrowserRequestFixture string        `json:"browser_request_fixture"`
			DesktopRequestFixture string        `json:"desktop_request_fixture"`
		} `json:"bindings"`
		Outcomes []struct {
			BrowserDecoder string                 `json:"browser_decoder"`
			DesktopDecoder string                 `json:"desktop_decoder"`
			BrowserFixture string                 `json:"browser_fixture"`
			DesktopFixture string                 `json:"desktop_fixture"`
			Outcome        protocolcommon.Outcome `json:"outcome"`
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
		browser, desktop := fixture(t, vector.BrowserRequestFixture), fixture(t, vector.DesktopRequestFixture)
		if bytes.Equal(browser, desktop) {
			t.Fatalf("%s uses identical Browser/Desktop bytes", vector.GeneratedMethod)
		}
		for _, control := range [][]byte{browser, desktop} {
			binding, err := ValidateExchange(vector.GeneratedMethod, Exchange{Operation: vector.Operation, Control: control})
			if err != nil {
				t.Fatalf("%s: %v", vector.GeneratedMethod, err)
			}
			if binding.Target != vector.Target || binding.ClientMethod != vector.ClientMethod {
				t.Fatalf("binding drift: %+v", binding)
			}
		}
	}
	wantOutcomes := []protocolcommon.Outcome{protocolcommon.OutcomeSuccess, protocolcommon.OutcomeRejected, protocolcommon.OutcomeFailed, protocolcommon.OutcomeCancelled}
	if len(value.Outcomes) != len(wantOutcomes) {
		t.Fatal("outcome fixture is incomplete")
	}
	for index, vector := range value.Outcomes {
		browserBytes, desktopBytes := fixture(t, vector.BrowserFixture), fixture(t, vector.DesktopFixture)
		if bytes.Equal(browserBytes, desktopBytes) {
			t.Fatalf("outcome %s uses identical bytes", vector.Outcome)
		}
		for surfaceIndex, fixtureName := range []string{vector.BrowserFixture, vector.DesktopFixture} {
			decoder := []string{vector.BrowserDecoder, vector.DesktopDecoder}[surfaceIndex]
			var outcome protocolcommon.Outcome
			switch decoder {
			case "compile":
				decoded, err := engineprotocol.DecodeCompileResponseEnvelope(fixture(t, fixtureName))
				if err != nil {
					t.Fatal(err)
				}
				outcome = decoded.Outcome
			case "handshake":
				decoded, err := engineprotocol.DecodeHandshakeResponseEnvelope(fixture(t, fixtureName))
				if err != nil {
					t.Fatal(err)
				}
				outcome = decoded.Outcome
			case "execute_query":
				decoded, err := engineprotocol.DecodeExecuteQueryResponseEnvelope(fixture(t, fixtureName))
				if err != nil {
					t.Fatal(err)
				}
				outcome = decoded.Outcome
			case "open_document":
				decoded, err := engineprotocol.DecodeOpenDocumentResponseEnvelope(fixture(t, fixtureName))
				if err != nil {
					t.Fatal(err)
				}
				outcome = decoded.Outcome
			default:
				t.Fatalf("unknown generated decoder %q", decoder)
			}
			if outcome != vector.Outcome || outcome != wantOutcomes[index] {
				t.Fatalf("outcome drift: %q", outcome)
			}
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
	if len(table) != 66 {
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
			if fields.Field(index).Kind() == reflect.Interface {
				fields.Field(index).Set(reflect.ValueOf(OwnerDecoder(strictOwnerDecoder{})))
				continue
			}
			fields.Field(index).Set(reflect.ValueOf(method))
		}
	}
	clients := ClientSet{}
	fill(&clients.Engine)
	fill(&clients.Runtime)
	fill(&clients.Registry)
	fill(&clients.Review)
	fill(&clients.Host)
	fill(&clients.Access)
	fill(&clients.NativeQuery)
	fill(&clients.SearchIndex)
	fill(&clients.Embedding)
	fill(&clients.MCP)
	return clients
}

type strictOwnerDecoder struct{}

func (strictOwnerDecoder) Decode(expected string, control []byte) (OwnerEnvelopeIdentity, error) {
	var envelope struct {
		Operation string         `json:"operation"`
		RequestID string         `json:"request_id"`
		Payload   map[string]any `json:"payload"`
	}
	decoder := json.NewDecoder(bytes.NewReader(control))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&envelope); err != nil {
		return OwnerEnvelopeIdentity{}, err
	}
	var trailing any
	if err := decoder.Decode(&trailing); !errors.Is(err, io.EOF) {
		return OwnerEnvelopeIdentity{}, errors.New("trailing owner envelope")
	}
	if envelope.Operation != expected || envelope.RequestID == "" || envelope.Payload == nil {
		return OwnerEnvelopeIdentity{}, errors.New("invalid owner envelope")
	}
	return OwnerEnvelopeIdentity{Operation: envelope.Operation, RequestID: envelope.RequestID}, nil
}

func TestOwnerBindingsAreCompleteUsableAndDistinctAcrossSurfaces(t *testing.T) {
	var fixtureValue struct {
		Version  int         `json:"version"`
		Bindings [][4]string `json:"bindings"`
	}
	if err := json.Unmarshal(fixture(t, "schemas/fixtures/desktop/owner-binding-parity-v1.json"), &fixtureValue); err != nil {
		t.Fatal(err)
	}
	if fixtureValue.Version != 1 || len(fixtureValue.Bindings) != 16 {
		t.Fatal("owner binding closure incomplete")
	}
	clients := completeClientSet()
	seen := map[string]bool{}
	for _, vector := range fixtureValue.Bindings {
		method, target, clientMethod, operation := vector[0], BindingTarget(vector[1]), vector[2], vector[3]
		if seen[method] {
			t.Fatalf("duplicate owner binding %s", method)
		}
		seen[method] = true
		binding, err := findBinding(method, operation)
		if err != nil || binding.Target != target || binding.ClientMethod != clientMethod {
			t.Fatalf("binding %s: %+v %v", method, binding, err)
		}
		browser := []byte(`{"operation":"` + operation + `","request_id":"browser-` + method + `","payload":{}}`)
		desktop := []byte(`{"operation":"` + operation + `","request_id":"desktop-` + method + `","payload":{}}`)
		if bytes.Equal(browser, desktop) {
			t.Fatal("owner surface bytes are identical")
		}
		for _, control := range [][]byte{browser, desktop} {
			if _, err := clients.Invoke(context.Background(), method, Exchange{Operation: operation, Control: control}); err != nil {
				t.Fatalf("%s: %v", method, err)
			}
		}
	}
	if _, err := clients.Invoke(context.Background(), "ReviewSubmit", Exchange{Operation: "review.other", Control: []byte(`{"operation":"review.other","request_id":"x","payload":{}}`)}); err == nil {
		t.Fatal("replaced owner operation accepted")
	}
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
		{func(_ *Manifest, s *[]protocolcommon.RequestedCapabilityStatus) {
			(*s)[0].CapabilityID = "desktop.unknown"
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
	grant.AccessFingerprint = accesscore.Fingerprint(grant)
	if !ValidateLocalOwnerGrant(request, grant, now) {
		t.Fatal("valid full-authoring local owner rejected")
	}
	grant.GrantedCapabilities = grant.GrantedCapabilities[1:]
	if ValidateLocalOwnerGrant(request, grant, now) {
		t.Fatal("partial local owner grant accepted")
	}
	grant.GrantedCapabilities = accesscore.FullAuthoringCapabilities()
	grant.AccessFingerprint = accesscore.Fingerprint(grant)
	forged := grant
	forged.AccessFingerprint = "sha256:bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"
	if ValidateLocalOwnerGrant(request, forged, now) {
		t.Fatal("forged fingerprint accepted")
	}
	expiredAt := protocolcommon.Rfc3339Time(now.Add(-time.Second).Format(time.RFC3339Nano))
	expired := grant
	expired.ExpiresAt = &expiredAt
	expired.AccessFingerprint = accesscore.Fingerprint(expired)
	if ValidateLocalOwnerGrant(request, expired, now) {
		t.Fatal("expired local owner accepted")
	}
	delegation := accesscore.Delegation{ID: "delegation", ParentActor: request.Actor, Agent: accessprotocol.ActorRef{ActorID: "agent", Kind: "agent"}, DocumentID: "doc", LocalScopeID: "local", AuthoringCapabilities: accesscore.FullAuthoringCapabilities(), Permissions: accesscore.AgentPermissions{Read: true, Export: true, Propose: true, Apply: true}, IssuedAt: now, ExpiresAt: now.Add(time.Hour), Generation: 3}
	requestDelegation := delegation
	requestDelegation.Generation = 0
	if !ValidateDelegationRequest(grant, requestDelegation, now) {
		t.Fatal("typed delegation request rejected")
	}
	requestDelegation.Permissions = accesscore.AgentPermissions{}
	if ValidateDelegationRequest(grant, requestDelegation, now) {
		t.Fatal("permissionless delegation accepted")
	}
	requestDelegation = delegation
	requestDelegation.Generation = 0
	requestDelegation.DocumentID = "other"
	if ValidateDelegationRequest(grant, requestDelegation, now) {
		t.Fatal("cross-document delegation accepted")
	}
	fence := DelegationFence{DelegationID: "delegation", DocumentID: "doc", LocalScopeID: "local", Generation: "3"}
	if !ValidateDelegationFence(fence, delegation, grant, now) {
		t.Fatal("valid generation fence rejected")
	}
	fence.Generation = "2"
	if ValidateDelegationFence(fence, delegation, grant, now) {
		t.Fatal("stale generation fence accepted")
	}
	fence.Generation = "3"
	if ValidateDelegationFence(fence, delegation, grant, now.Add(2*time.Hour)) {
		t.Fatal("expired delegation fence accepted")
	}
}

func TestRuntimeFixtureReallyUsesGeneratedEnvelope(t *testing.T) {
	if _, err := runtimeprotocol.DecodeRuntimeHandshakeRequestEnvelope(fixture(t, "schemas/fixtures/runtime/handshake-request.json")); err != nil {
		t.Fatal(err)
	}
}
