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
	"github.com/dencyuinc/layerdraw/internal/registry"
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

func (strictOwnerDecoder) DecodeRequest(expected string, control []byte) (OwnerEnvelopeIdentity, error) {
	var envelope struct {
		Operation string        `json:"operation"`
		RequestID string        `json:"request_id"`
		Payload   parityRequest `json:"payload"`
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
	if envelope.Operation != expected || envelope.RequestID == "" || envelope.Payload.Subject == "" || envelope.Payload.Arguments.Query == "" || envelope.Payload.Arguments.Limit == "" {
		return OwnerEnvelopeIdentity{}, errors.New("invalid owner envelope")
	}
	return OwnerEnvelopeIdentity{Operation: envelope.Operation, RequestID: envelope.RequestID}, nil
}

func (strictOwnerDecoder) DecodeResponse(expected string, control []byte) (OwnerResponseIdentity, error) {
	var envelope struct {
		Operation string       `json:"operation"`
		RequestID string       `json:"request_id"`
		Payload   parityResult `json:"payload"`
	}
	if err := decodeStrictParity(control, &envelope); err != nil || envelope.Operation != expected || envelope.RequestID == "" || envelope.Payload.Outcome != protocolcommon.OutcomeSuccess || len(envelope.Payload.Value.Items) == 0 || envelope.Payload.Value.Total == "" {
		return OwnerResponseIdentity{}, errors.New("invalid owner response envelope")
	}
	return OwnerResponseIdentity{Operation: envelope.Operation, RequestID: envelope.RequestID, Outcome: string(envelope.Payload.Outcome)}, nil
}

type parityRequest struct {
	Subject   string `json:"subject"`
	Arguments struct {
		Query string `json:"query"`
		Limit string `json:"limit"`
	} `json:"arguments"`
}

type parityResult struct {
	Outcome protocolcommon.Outcome `json:"outcome"`
	Value   struct {
		Items []string `json:"items"`
		Total string   `json:"total"`
	} `json:"value"`
}

type bindingParityFixture struct {
	Version            int                                        `json:"version"`
	SemanticRequest    parityRequest                              `json:"semantic_request"`
	SemanticResult     parityResult                               `json:"semantic_result"`
	Bindings           [][4]string                                `json:"bindings"`
	Capabilities       []protocolcommon.CapabilityID              `json:"capabilities"`
	CapabilityStatuses []protocolcommon.RequestedCapabilityStatus `json:"capability_statuses"`
}

type normalizedParity struct {
	Method    string
	Target    BindingTarget
	Operation string
	Request   parityRequest
	Result    parityResult
}

func validateBindingParityFixture(value bindingParityFixture) error {
	if value.Version != 2 || value.SemanticRequest.Subject == "" || value.SemanticRequest.Arguments.Query == "" || value.SemanticRequest.Arguments.Limit == "" || value.SemanticResult.Outcome != protocolcommon.OutcomeSuccess || len(value.SemanticResult.Value.Items) == 0 || value.SemanticResult.Value.Total == "" {
		return errors.New("invalid non-empty semantic parity fixture")
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

func bindingParityCodec(target BindingTarget) string {
	switch target {
	case TargetEngine, TargetRuntime:
		return "generated"
	case TargetRegistry:
		return "registry-owner"
	default:
		return "owner-generated"
	}
}

func decodeBrowserParity(wire []byte, binding BindingMethod) (normalizedParity, error) {
	var envelope struct {
		Adapter string `json:"adapter"`
		Codec   string `json:"codec"`
		Route   string `json:"route"`
		Request struct {
			Operation string        `json:"operation"`
			Payload   parityRequest `json:"payload"`
		} `json:"request"`
		Response struct {
			Operation string       `json:"operation"`
			Payload   parityResult `json:"payload"`
		} `json:"response"`
	}
	if err := decodeStrictParity(wire, &envelope); err != nil || envelope.Adapter != "browser-worker" || envelope.Codec != bindingParityCodec(binding.Target) || envelope.Route != binding.Operation || envelope.Request.Operation != binding.Operation || envelope.Response.Operation != binding.Operation {
		return normalizedParity{}, errors.New("invalid Browser parity adapter envelope")
	}
	return normalizedParity{Method: binding.GeneratedMethod, Target: binding.Target, Operation: binding.Operation, Request: envelope.Request.Payload, Result: envelope.Response.Payload}, nil
}

func decodeDesktopParity(wire []byte, binding BindingMethod) (normalizedParity, error) {
	var envelope struct {
		Adapter  string `json:"adapter"`
		Codec    string `json:"codec"`
		Method   string `json:"method"`
		Exchange struct {
			Operation string `json:"operation"`
			Control   struct {
				Operation string        `json:"operation"`
				Payload   parityRequest `json:"payload"`
			} `json:"control"`
		} `json:"exchange"`
		ExchangeResult struct {
			Operation string `json:"operation"`
			Control   struct {
				Operation string       `json:"operation"`
				Payload   parityResult `json:"payload"`
			} `json:"control"`
		} `json:"exchange_result"`
	}
	if err := decodeStrictParity(wire, &envelope); err != nil || envelope.Adapter != "desktop-wails" || envelope.Codec != bindingParityCodec(binding.Target) || envelope.Method != binding.GeneratedMethod || envelope.Exchange.Operation != binding.Operation || envelope.Exchange.Control.Operation != binding.Operation || envelope.ExchangeResult.Operation != binding.Operation || envelope.ExchangeResult.Control.Operation != binding.Operation {
		return normalizedParity{}, errors.New("invalid Desktop parity adapter envelope")
	}
	return normalizedParity{Method: binding.GeneratedMethod, Target: binding.Target, Operation: binding.Operation, Request: envelope.Exchange.Control.Payload, Result: envelope.ExchangeResult.Control.Payload}, nil
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

func TestAllBindingsHaveDistinctBrowserDesktopAdaptersWithSemanticParity(t *testing.T) {
	var fixtureValue bindingParityFixture
	if err := json.Unmarshal(fixture(t, "schemas/fixtures/desktop/owner-binding-parity-v1.json"), &fixtureValue); err != nil {
		t.Fatal(err)
	}
	if err := validateBindingParityFixture(fixtureValue); err != nil {
		t.Fatal(err)
	}
	for _, vector := range fixtureValue.Bindings {
		method, target, clientMethod, operation := vector[0], BindingTarget(vector[1]), vector[2], vector[3]
		binding, err := findBinding(method, operation)
		if err != nil || binding.Target != target || binding.ClientMethod != clientMethod {
			t.Fatalf("binding %s: %+v %v", method, binding, err)
		}
		browser, err := json.Marshal(struct {
			Adapter  string `json:"adapter"`
			Codec    string `json:"codec"`
			Route    string `json:"route"`
			Request  any    `json:"request"`
			Response any    `json:"response"`
		}{"browser-worker", bindingParityCodec(target), operation, struct {
			Operation string        `json:"operation"`
			Payload   parityRequest `json:"payload"`
		}{operation, fixtureValue.SemanticRequest}, struct {
			Operation string       `json:"operation"`
			Payload   parityResult `json:"payload"`
		}{operation, fixtureValue.SemanticResult}})
		if err != nil {
			t.Fatal(err)
		}
		desktop, err := json.Marshal(struct {
			Adapter        string `json:"adapter"`
			Codec          string `json:"codec"`
			Method         string `json:"method"`
			Exchange       any    `json:"exchange"`
			ExchangeResult any    `json:"exchange_result"`
		}{"desktop-wails", bindingParityCodec(target), method, struct {
			Operation string `json:"operation"`
			Control   any    `json:"control"`
		}{operation, struct {
			Operation string        `json:"operation"`
			Payload   parityRequest `json:"payload"`
		}{operation, fixtureValue.SemanticRequest}}, struct {
			Operation string `json:"operation"`
			Control   any    `json:"control"`
		}{operation, struct {
			Operation string       `json:"operation"`
			Payload   parityResult `json:"payload"`
		}{operation, fixtureValue.SemanticResult}}})
		if err != nil {
			t.Fatal(err)
		}
		if bytes.Equal(browser, desktop) {
			t.Fatal("surface adapter bytes are identical")
		}
		browserNormalized, browserErr := decodeBrowserParity(browser, binding)
		desktopNormalized, desktopErr := decodeDesktopParity(desktop, binding)
		if browserErr != nil || desktopErr != nil || !reflect.DeepEqual(browserNormalized, desktopNormalized) {
			t.Fatalf("%s semantic parity: browser=%+v desktop=%+v errors=%v/%v", method, browserNormalized, desktopNormalized, browserErr, desktopErr)
		}
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

func TestEveryOwnerBindingUsesStrictRequestAndResponseDecoders(t *testing.T) {
	var value bindingParityFixture
	if err := json.Unmarshal(fixture(t, "schemas/fixtures/desktop/owner-binding-parity-v1.json"), &value); err != nil {
		t.Fatal(err)
	}
	clients := completeClientSet()
	decoder := strictOwnerDecoder{}
	ownerRows := 0
	for _, binding := range GeneratedBindingTable() {
		if binding.Target == TargetEngine || binding.Target == TargetRuntime || binding.Target == TargetRegistry {
			continue
		}
		ownerRows++
		requests := make([][]byte, 0, 2)
		responses := make([][]byte, 0, 2)
		for _, surface := range []string{"browser", "desktop"} {
			request, err := json.Marshal(struct {
				Operation string        `json:"operation"`
				RequestID string        `json:"request_id"`
				Payload   parityRequest `json:"payload"`
			}{binding.Operation, surface + "-request-" + binding.GeneratedMethod, value.SemanticRequest})
			if err != nil {
				t.Fatal(err)
			}
			response, err := json.Marshal(struct {
				Operation string       `json:"operation"`
				RequestID string       `json:"request_id"`
				Payload   parityResult `json:"payload"`
			}{binding.Operation, surface + "-request-" + binding.GeneratedMethod, value.SemanticResult})
			if err != nil {
				t.Fatal(err)
			}
			if _, err := clients.Invoke(context.Background(), binding.GeneratedMethod, Exchange{Operation: binding.Operation, Control: request}); err != nil {
				t.Fatalf("%s %s request: %v", surface, binding.GeneratedMethod, err)
			}
			if identity, err := decoder.DecodeResponse(binding.Operation, response); err != nil || identity.Operation != binding.Operation || identity.Outcome != string(protocolcommon.OutcomeSuccess) {
				t.Fatalf("%s %s response: %+v %v", surface, binding.GeneratedMethod, identity, err)
			}
			requests = append(requests, request)
			responses = append(responses, response)
		}
		if bytes.Equal(requests[0], requests[1]) || bytes.Equal(responses[0], responses[1]) || !reflect.DeepEqual(normalizedJSONWithoutRequestID(t, requests[0]), normalizedJSONWithoutRequestID(t, requests[1])) || !reflect.DeepEqual(normalizedJSONWithoutRequestID(t, responses[0]), normalizedJSONWithoutRequestID(t, responses[1])) {
			t.Fatalf("%s does not preserve owner semantic parity through distinct adapters", binding.GeneratedMethod)
		}
	}
	if ownerRows != 16 {
		t.Fatalf("owner response decoder closure = %d", ownerRows)
	}
	bad := []byte(`{"operation":"review.other","request_id":"desktop-bad","payload":{"subject":"fixture-document","arguments":{"query":"status:open","limit":"25"}}}`)
	if _, err := clients.Invoke(context.Background(), "ReviewSubmit", Exchange{Operation: "review.other", Control: bad}); err == nil {
		t.Fatal("replaced owner operation accepted")
	}
}

func generatedResponseTemplate(t *testing.T, binding BindingMethod, requestID string, result parityResult) []byte {
	t.Helper()
	if binding.Target == TargetRegistry {
		wire, err := json.Marshal(registry.WireResponse{WireVersion: registry.RegistryWireVersion, Operation: registry.WireOperation(binding.Operation), RequestID: requestID, OK: true, Value: mustRawJSON(t, result)})
		if err != nil {
			t.Fatal(err)
		}
		return wire
	}
	var template []byte
	if binding.Target == TargetRuntime {
		template = fixture(t, "schemas/fixtures/runtime/handshake-failed.json")
	} else {
		switch binding.Operation {
		case string(engineprotocol.CompileRequestEnvelopeOperationValue), string(engineprotocol.HandshakeRequestEnvelopeOperationValue):
			template = fixture(t, "schemas/fixtures/engine/handshake-failed-response.json")
		default:
			template = fixture(t, "schemas/fixtures/engine/workbench-failed-execution-response.json")
		}
	}
	var envelope map[string]any
	decoder := json.NewDecoder(bytes.NewReader(template))
	decoder.UseNumber()
	if err := decoder.Decode(&envelope); err != nil {
		t.Fatal(err)
	}
	envelope["request_id"] = requestID
	wire, err := json.Marshal(envelope)
	if err != nil {
		t.Fatal(err)
	}
	return wire
}

func mustRawJSON(t *testing.T, value any) json.RawMessage {
	t.Helper()
	wire, err := json.Marshal(value)
	if err != nil {
		t.Fatal(err)
	}
	return wire
}

func TestEveryGeneratedAndRegistryBindingHasTypedResponseParity(t *testing.T) {
	var value bindingParityFixture
	if err := json.Unmarshal(fixture(t, "schemas/fixtures/desktop/owner-binding-parity-v1.json"), &value); err != nil {
		t.Fatal(err)
	}
	decoded := 0
	for _, binding := range GeneratedBindingTable() {
		if binding.Target != TargetEngine && binding.Target != TargetRuntime && binding.Target != TargetRegistry {
			continue
		}
		browser := generatedResponseTemplate(t, binding, "browser-"+binding.GeneratedMethod, value.SemanticResult)
		desktop := generatedResponseTemplate(t, binding, "desktop-"+binding.GeneratedMethod, value.SemanticResult)
		if bytes.Equal(browser, desktop) {
			t.Fatalf("%s response adapters are byte-identical", binding.GeneratedMethod)
		}
		if err := decodeExactResponse(binding, browser); err != nil {
			t.Fatalf("%s Browser typed response decoder: %v", binding.GeneratedMethod, err)
		}
		if err := decodeExactResponse(binding, desktop); err != nil {
			t.Fatalf("%s Desktop typed response decoder: %v", binding.GeneratedMethod, err)
		}
		if !reflect.DeepEqual(normalizedJSONWithoutRequestID(t, browser), normalizedJSONWithoutRequestID(t, desktop)) {
			t.Fatalf("%s response semantic parity drift", binding.GeneratedMethod)
		}
		decoded++
	}
	if decoded != 50 {
		t.Fatalf("generated/Registry response decoder closure = %d", decoded)
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
