// SPDX-License-Identifier: LicenseRef-LayerDraw-1.0

package desktopcontract

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

type stubClient struct{}

func (stubClient) Exchange(context.Context, Exchange) (ExchangeResult, error) {
	return ExchangeResult{}, nil
}

func TestDefaultManifestFreezesDesktopClosure(t *testing.T) {
	manifest := DefaultManifest()
	if err := manifest.Validate(); err != nil {
		t.Fatal(err)
	}
	manifest.Components = append([]ComponentID(nil), manifest.Components[1:]...)
	if err := manifest.Validate(); err == nil {
		t.Fatal("missing Engine component was accepted")
	}
	manifest = DefaultManifest()
	manifest.Frontend[1] = "@layerdraw/engine-client"
	if err := manifest.Validate(); err == nil {
		t.Fatal("transport-neutral root replaced the required Wails entrypoint")
	}
	manifest = DefaultManifest()
	manifest.Capabilities[0].Requirement = Optional
	if err := manifest.Validate(); err == nil {
		t.Fatal("required capability was silently weakened")
	}
	manifest = DefaultManifest()
	manifest.Version = 2
	if err := manifest.Validate(); err == nil {
		t.Fatal("unknown manifest version was accepted")
	}
	manifest = DefaultManifest()
	manifest.Frontend = manifest.Frontend[:len(manifest.Frontend)-1]
	if err := manifest.Validate(); err == nil {
		t.Fatal("incomplete frontend closure was accepted")
	}
	manifest = DefaultManifest()
	manifest.Capabilities = manifest.Capabilities[:len(manifest.Capabilities)-1]
	if err := manifest.Validate(); err == nil {
		t.Fatal("incomplete capability closure was accepted")
	}
	manifest = DefaultManifest()
	manifest.Capabilities[0].Requirement = "sometimes"
	if err := manifest.Validate(); err == nil {
		t.Fatal("unknown capability requirement was accepted")
	}
	manifest = DefaultManifest()
	manifest.Capabilities[1].ID = manifest.Capabilities[0].ID
	if err := manifest.Validate(); err == nil {
		t.Fatal("duplicate capability was accepted")
	}
}

func TestGeneratedBindingCompatibilityFixture(t *testing.T) {
	path := filepath.Join("..", "..", "schemas", "fixtures", "desktop", "wails-binding-compatibility-v1.json")
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var fixture struct {
		Version  int `json:"version"`
		Bindings []struct {
			GeneratedMethod string        `json:"generated_method"`
			Target          BindingTarget `json:"target"`
			Operation       string        `json:"operation"`
			BrowserOutcome  Outcome       `json:"browser_outcome"`
			DesktopOutcome  Outcome       `json:"desktop_outcome"`
		} `json:"bindings"`
		Capabilities []struct {
			CapabilityID   CapabilityID `json:"capability_id"`
			BrowserOutcome Outcome      `json:"browser_outcome"`
			DesktopOutcome Outcome      `json:"desktop_outcome"`
		} `json:"capabilities"`
	}
	if err := json.Unmarshal(data, &fixture); err != nil {
		t.Fatal(err)
	}
	if fixture.Version != 1 || len(fixture.Bindings) != len(generatedBindingTable) {
		t.Fatalf("fixture does not cover the binding table: %+v", fixture)
	}
	for index, vector := range fixture.Bindings {
		binding := generatedBindingTable[index]
		if vector.GeneratedMethod != binding.GeneratedMethod || vector.Target != binding.Target {
			t.Fatalf("fixture binding %d drifted: %+v != %+v", index, vector, binding)
		}
		resolved, err := ResolveBinding(vector.GeneratedMethod, vector.Operation)
		if err != nil || resolved != vector.Target {
			t.Fatalf("binding %d does not resolve: %v", index, err)
		}
		if vector.BrowserOutcome != vector.DesktopOutcome {
			t.Fatalf("binding %d forks browser/Desktop outcome", index)
		}
	}
	if len(fixture.Capabilities) != len(DefaultManifest().Capabilities) {
		t.Fatalf("fixture does not cover every capability family")
	}
	for index, vector := range fixture.Capabilities {
		if vector.CapabilityID != DefaultManifest().Capabilities[index].ID {
			t.Fatalf("capability fixture %d drifted", index)
		}
		if vector.BrowserOutcome != vector.DesktopOutcome {
			t.Fatalf("capability %q forks browser/Desktop outcome", vector.CapabilityID)
		}
	}
}

func TestBindingRejectsCrossComponentAndUnknownMethods(t *testing.T) {
	for _, input := range [][2]string{{"EngineExchange", "runtime.open_document"}, {"Unknown", "engine.compile"}, {"EngineExchange", "engine."}} {
		if _, err := ResolveBinding(input[0], input[1]); err == nil {
			t.Fatalf("accepted binding %q / %q", input[0], input[1])
		}
	}
}

func TestBindingClientSetAndTableAreClosed(t *testing.T) {
	clients := ClientSet{Engine: stubClient{}, Runtime: stubClient{}, Registry: stubClient{}, Review: stubClient{}, Host: stubClient{}}
	if err := clients.Validate(); err != nil {
		t.Fatal(err)
	}
	clients.Host = nil
	if err := clients.Validate(); err == nil {
		t.Fatal("incomplete binding clients were accepted")
	}
	table := GeneratedBindingTable()
	table[0].GeneratedMethod = "Mutated"
	if GeneratedBindingTable()[0].GeneratedMethod != "EngineExchange" {
		t.Fatal("binding table publication was not defensive")
	}
}

func TestTypedResultHasClosedOutcomeShape(t *testing.T) {
	value := "ok"
	cases := []struct {
		result Result[string]
		valid  bool
	}{
		{Result[string]{Outcome: OutcomeSuccess, Value: &value}, true},
		{Result[string]{Outcome: OutcomeCancelled, Failure: &Failure{Code: FailureDialogCancelled}}, true},
		{Result[string]{Outcome: OutcomeFailed, Failure: &Failure{Code: FailureBackendPanic}}, true},
		{Result[string]{Outcome: OutcomeSuccess}, false},
		{Result[string]{Outcome: OutcomeCancelled, Failure: &Failure{Code: FailureShutdown}}, false},
		{Result[string]{Outcome: OutcomeFailed, Failure: &Failure{Code: FailureDialogCancelled}}, false},
		{Result[string]{Outcome: "unknown"}, false},
	}
	for index, test := range cases {
		if test.result.Validate() != test.valid {
			t.Fatalf("case %d validation mismatch", index)
		}
	}
}

func TestCapabilityNegotiationFailsRequiredAndRetainsOptional(t *testing.T) {
	manifest := DefaultManifest()
	statuses := make([]CapabilityStatus, 0, len(manifest.Capabilities))
	for _, requirement := range manifest.Capabilities {
		statuses = append(statuses, CapabilityStatus{ID: requirement.ID, Availability: Available, ProtocolVersion: "1.0"})
	}
	result := NegotiateCapabilities(manifest, statuses)
	if !result.Validate() || result.Value == nil || len(*result.Value) != len(statuses) {
		t.Fatalf("valid capability negotiation failed: %+v", result)
	}
	reason := UnavailableAdapter
	statuses[7].Availability, statuses[7].UnavailableReason = Unavailable, &reason
	result = NegotiateCapabilities(manifest, statuses)
	if !result.Validate() || result.Outcome != OutcomeSuccess || (*result.Value)[7].UnavailableReason == nil {
		t.Fatalf("optional unavailable capability was not retained: %+v", result)
	}
	statuses[0].Availability, statuses[0].UnavailableReason = Unavailable, &reason
	result = NegotiateCapabilities(manifest, statuses)
	if !result.Validate() || result.Failure == nil || result.Failure.Code != FailureAdapterUnavailable {
		t.Fatalf("required unavailable capability did not fail closed: %+v", result)
	}
	statuses = statuses[:len(statuses)-1]
	result = NegotiateCapabilities(manifest, statuses)
	if result.Failure == nil || result.Failure.Code != FailureProtocolIncompatible {
		t.Fatalf("incomplete status closure was accepted: %+v", result)
	}
	manifest.Version = 2
	result = NegotiateCapabilities(manifest, nil)
	if result.Failure == nil || result.Failure.Code != FailureProtocolIncompatible {
		t.Fatalf("invalid manifest was negotiated: %+v", result)
	}
}

func TestCapabilityNegotiationRejectsMalformedStatuses(t *testing.T) {
	manifest := DefaultManifest()
	valid := func() []CapabilityStatus {
		statuses := make([]CapabilityStatus, 0, len(manifest.Capabilities))
		for _, requirement := range manifest.Capabilities {
			statuses = append(statuses, CapabilityStatus{ID: requirement.ID, Availability: Available, ProtocolVersion: "1.0"})
		}
		return statuses
	}
	reason := UnavailableConfiguration
	cases := []func([]CapabilityStatus){
		func(statuses []CapabilityStatus) { statuses[1].ID = statuses[0].ID },
		func(statuses []CapabilityStatus) { statuses[0].ProtocolVersion = "" },
		func(statuses []CapabilityStatus) { statuses[0].UnavailableReason = &reason },
		func(statuses []CapabilityStatus) { statuses[7].Availability = Unavailable },
		func(statuses []CapabilityStatus) { statuses[0].Availability = "maybe" },
		func(statuses []CapabilityStatus) { statuses[0].ID = "desktop.unknown" },
	}
	for index, mutate := range cases {
		statuses := valid()
		mutate(statuses)
		result := NegotiateCapabilities(manifest, statuses)
		if result.Outcome != OutcomeFailed || result.Failure == nil || result.Failure.Code != FailureProtocolIncompatible {
			t.Fatalf("malformed status case %d was accepted: %+v", index, result)
		}
	}
}
