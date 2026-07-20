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
	"strings"
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
		browserSemantic, desktopSemantic := normalizedJSONWithoutRequestID(t, browser), normalizedJSONWithoutRequestID(t, desktop)
		if !reflect.DeepEqual(browserSemantic, desktopSemantic) {
			t.Fatalf("%s changed semantic request across adapters", vector.GeneratedMethod)
		}
	}
}

func normalizedJSONWithoutRequestID(t *testing.T, wire []byte) map[string]any {
	t.Helper()
	var value map[string]any
	decoder := json.NewDecoder(bytes.NewReader(wire))
	decoder.UseNumber()
	if err := decoder.Decode(&value); err != nil {
		t.Fatal(err)
	}
	delete(value, "request_id")
	return value
}

func TestEveryApprovedBindingHasExactDecoderAndRejectsEmptyEnvelope(t *testing.T) {
	table := GeneratedBindingTable()
	if len(table) < 67 {
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

func protocolRequestOperations(t *testing.T, schemaPath string) map[string]bool {
	t.Helper()
	var document struct {
		Definitions map[string]struct {
			Properties map[string]struct {
				Constant any `json:"const"`
			} `json:"properties"`
		} `json:"$defs"`
	}
	if err := json.Unmarshal(fixture(t, schemaPath), &document); err != nil {
		t.Fatal(err)
	}
	operations := map[string]bool{}
	for name, definition := range document.Definitions {
		if !strings.HasSuffix(name, "RequestEnvelope") {
			continue
		}
		operation, _ := definition.Properties["operation"].Constant.(string)
		if operation == "" || operations[operation] {
			t.Fatalf("invalid schema request envelope %s=%q", name, operation)
		}
		operations[operation] = true
	}
	return operations
}

func TestBindingClosureIsDerivedFromGeneratedProtocolSchemasAndClientPorts(t *testing.T) {
	wantByTarget := map[BindingTarget]map[string]bool{
		TargetEngine:  protocolRequestOperations(t, "schemas/engine-protocol/v1.schema.json"),
		TargetRuntime: protocolRequestOperations(t, "schemas/runtime-protocol/v1.schema.json"),
	}
	gotByTarget := map[BindingTarget]map[string]bool{}
	methodByTarget := map[BindingTarget]map[string]bool{}
	for _, binding := range GeneratedBindingTable() {
		if gotByTarget[binding.Target] == nil {
			gotByTarget[binding.Target] = map[string]bool{}
			methodByTarget[binding.Target] = map[string]bool{}
		}
		gotByTarget[binding.Target][binding.Operation] = true
		methodByTarget[binding.Target][binding.ClientMethod] = true
	}
	for target, want := range wantByTarget {
		if !reflect.DeepEqual(gotByTarget[target], want) {
			t.Fatalf("%s binding/schema closure drift: got=%v want=%v", target, gotByTarget[target], want)
		}
	}
	clientPorts := map[BindingTarget]any{
		TargetEngine: EngineClient{}, TargetRuntime: RuntimeClient{}, TargetRegistry: RegistryClient{}, TargetReview: ReviewClient{}, TargetHost: HostClient{}, TargetAccess: AccessClient{}, TargetNativeQuery: NativeQueryClient{}, TargetSearchIndex: SearchIndexClient{}, TargetEmbedding: EmbeddingClient{}, TargetMCP: MCPClient{},
	}
	for target, port := range clientPorts {
		wantMethods := map[string]bool{}
		typeOf := reflect.TypeOf(port)
		for index := 0; index < typeOf.NumField(); index++ {
			field := typeOf.Field(index)
			if field.Name != "Decoder" {
				wantMethods[field.Name] = true
			}
		}
		if !reflect.DeepEqual(methodByTarget[target], wantMethods) {
			t.Fatalf("%s binding/client port closure drift: got=%v want=%v", target, methodByTarget[target], wantMethods)
		}
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

func completeClientSet(t *testing.T) ClientSet {
	t.Helper()
	method := ClientMethod(func(_ context.Context, exchange Exchange) (ExchangeResult, error) {
		requestID, err := controlRequestID(exchange.Control)
		if err != nil {
			return ExchangeResult{}, err
		}
		var binding BindingMethod
		for _, candidate := range GeneratedBindingTable() {
			if candidate.Operation == exchange.Operation {
				binding = candidate
				break
			}
		}
		if binding.Operation == "" {
			return ExchangeResult{}, errors.New("unknown test binding")
		}
		return ExchangeResult{Operation: binding.Operation, Control: operationResponseFixture(t, binding, requestID)}, nil
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

func (strictOwnerDecoder) DecodeRequest(expected string, control []byte) (OwnerEnvelopeIdentity, error) {
	var envelope struct {
		Operation string          `json:"operation"`
		RequestID string          `json:"request_id"`
		Payload   json.RawMessage `json:"payload"`
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
	if envelope.Operation != expected || envelope.RequestID == "" || decodeOwnerOperationPayload(expected, envelope.Payload, false) != nil {
		return OwnerEnvelopeIdentity{}, errors.New("invalid owner envelope")
	}
	return OwnerEnvelopeIdentity{Operation: envelope.Operation, RequestID: envelope.RequestID}, nil
}

func (strictOwnerDecoder) DecodeResponse(expected string, control []byte) (OwnerResponseIdentity, error) {
	var envelope struct {
		Operation string          `json:"operation"`
		RequestID string          `json:"request_id"`
		Payload   json.RawMessage `json:"payload"`
	}
	if err := decodeStrictParity(control, &envelope); err != nil || envelope.Operation != expected || envelope.RequestID == "" || decodeOwnerOperationPayload(expected, envelope.Payload, true) != nil {
		return OwnerResponseIdentity{}, errors.New("invalid owner response envelope")
	}
	return OwnerResponseIdentity{Operation: envelope.Operation, RequestID: envelope.RequestID, Outcome: string(protocolcommon.OutcomeSuccess)}, nil
}

type bindingParityFixture struct {
	Version            int                                        `json:"version"`
	Bindings           [][4]string                                `json:"bindings"`
	Capabilities       []protocolcommon.CapabilityID              `json:"capabilities"`
	CapabilityStatuses []protocolcommon.RequestedCapabilityStatus `json:"capability_statuses"`
}

func validateBindingParityFixture(value bindingParityFixture) error {
	if value.Version != 3 {
		return errors.New("invalid binding parity fixture version")
	}
	table := GeneratedBindingTable()
	if len(value.Bindings) != len(table) {
		return errors.New("binding parity closure mismatch")
	}
	seen := map[string]bool{}
	for index, row := range value.Bindings {
		binding := table[index]
		want := [4]string{binding.GeneratedMethod, string(binding.Target), binding.ClientMethod, binding.Operation}
		if row != want || seen[row[0]] {
			return errors.New("binding parity row mismatch")
		}
		seen[row[0]] = true
	}
	manifest := DefaultManifest()
	wantCapabilities := append(append([]protocolcommon.CapabilityID(nil), manifest.RequiredCapabilities...), manifest.OptionalCapabilities...)
	if !reflect.DeepEqual(value.Capabilities, wantCapabilities) {
		return errors.New("capability parity closure mismatch")
	}
	seenCapability := map[protocolcommon.CapabilityID]bool{}
	for _, capability := range value.Capabilities {
		if seenCapability[capability] {
			return errors.New("duplicate capability parity row")
		}
		seenCapability[capability] = true
	}
	wantStatuses := make([]protocolcommon.RequestedCapabilityStatus, 0, len(wantCapabilities))
	for _, capability := range manifest.RequiredCapabilities {
		wantStatuses = append(wantStatuses, protocolcommon.RequestedCapabilityStatus{CapabilityID: capability, Enabled: true, ProtocolVersion: DesktopProtocolVersion})
	}
	unavailable := protocolcommon.UnavailableReasonNotConfigured
	for _, capability := range manifest.OptionalCapabilities {
		wantStatuses = append(wantStatuses, protocolcommon.RequestedCapabilityStatus{CapabilityID: capability, Enabled: false, ProtocolVersion: DesktopProtocolVersion, UnavailableReason: &unavailable})
	}
	if !reflect.DeepEqual(value.CapabilityStatuses, wantStatuses) {
		return errors.New("capability status parity closure mismatch")
	}
	return nil
}

func cloneBindingParityFixture(t *testing.T, value bindingParityFixture) bindingParityFixture {
	t.Helper()
	wire, err := json.Marshal(value)
	if err != nil {
		t.Fatal(err)
	}
	var clone bindingParityFixture
	if err := json.Unmarshal(wire, &clone); err != nil {
		t.Fatal(err)
	}
	return clone
}

func decodeStrictParity(wire []byte, destination any) error {
	decoder := json.NewDecoder(bytes.NewReader(wire))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(destination); err != nil {
		return err
	}
	var trailing any
	if err := decoder.Decode(&trailing); !errors.Is(err, io.EOF) {
		return errors.New("trailing parity envelope")
	}
	return nil
}

func TestBindingAndCapabilityParityFixtureIsExactClosedAuthority(t *testing.T) {
	var fixtureValue bindingParityFixture
	if err := json.Unmarshal(fixture(t, "schemas/fixtures/desktop/owner-binding-parity-v1.json"), &fixtureValue); err != nil {
		t.Fatal(err)
	}
	if err := validateBindingParityFixture(fixtureValue); err != nil {
		t.Fatal(err)
	}
	mutations := []func(*bindingParityFixture){
		func(value *bindingParityFixture) { value.Bindings = value.Bindings[1:] },
		func(value *bindingParityFixture) { value.Bindings = append(value.Bindings, value.Bindings[0]) },
		func(value *bindingParityFixture) { value.Bindings[0][0] = "Unknown" },
		func(value *bindingParityFixture) { value.Bindings[1] = value.Bindings[0] },
		func(value *bindingParityFixture) { value.Bindings[0][3] = "engine.replaced" },
		func(value *bindingParityFixture) { value.Capabilities = value.Capabilities[1:] },
		func(value *bindingParityFixture) { value.Capabilities = append(value.Capabilities, "desktop.extra") },
		func(value *bindingParityFixture) { value.Capabilities[0] = "desktop.unknown" },
		func(value *bindingParityFixture) { value.Capabilities[1] = value.Capabilities[0] },
		func(value *bindingParityFixture) { value.CapabilityStatuses = value.CapabilityStatuses[1:] },
		func(value *bindingParityFixture) {
			value.CapabilityStatuses = append(value.CapabilityStatuses, value.CapabilityStatuses[0])
		},
		func(value *bindingParityFixture) { value.CapabilityStatuses[0].CapabilityID = "desktop.unknown" },
		func(value *bindingParityFixture) { value.CapabilityStatuses[1] = value.CapabilityStatuses[0] },
		func(value *bindingParityFixture) { value.CapabilityStatuses[0].ProtocolVersion = "2.0" },
	}
	for index, mutate := range mutations {
		candidate := cloneBindingParityFixture(t, fixtureValue)
		mutate(&candidate)
		if err := validateBindingParityFixture(candidate); err == nil {
			t.Fatalf("parity closure mutation %d accepted", index)
		}
	}
}

func TestClientSetInvokesOnlyExactValidatedMethods(t *testing.T) {
	clients := completeClientSet(t)
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
		if err != nil || result.Operation != input.operation {
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
	forgedParent := grant
	forgedParent.AccessFingerprint = "sha256:bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"
	requestDelegation = delegation
	requestDelegation.Generation = 0
	if ValidateDelegationRequest(forgedParent, requestDelegation, now) {
		t.Fatal("delegation request accepted a forged active parent fingerprint")
	}
	if ValidateDelegationFence(fence, delegation, forgedParent, now) {
		t.Fatal("delegation fence accepted a forged active parent fingerprint")
	}
}

func TestRuntimeFixtureReallyUsesGeneratedEnvelope(t *testing.T) {
	if _, err := runtimeprotocol.DecodeRuntimeHandshakeRequestEnvelope(fixture(t, "schemas/fixtures/runtime/handshake-request.json")); err != nil {
		t.Fatal(err)
	}
}
