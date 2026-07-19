// SPDX-License-Identifier: LicenseRef-LayerDraw-1.0

package registry

import (
	"encoding/json"
	"testing"
)

func TestDecodeWireRequestBindsExactOperation(t *testing.T) {
	valid := []byte(`{"wire_version":"1.0","operation":"registry.list_sources","request_id":"request","input":{}}`)
	request, err := DecodeWireRequest(valid, WireListSources)
	if err != nil || request.Operation != WireListSources {
		t.Fatalf("valid request: %+v %v", request, err)
	}
	for _, input := range []struct {
		wire     []byte
		expected WireOperation
	}{
		{nil, WireListSources},
		{valid, WireSearch},
		{[]byte(`{"wire_version":"2.0","operation":"registry.list_sources","request_id":"request","input":{}}`), WireListSources},
		{[]byte(`{"wire_version":"1.0","operation":"registry.list_sources","request_id":"","input":{}}`), WireListSources},
		{[]byte(`{"wire_version":"1.0","operation":"registry.unknown","request_id":"request","input":{}}`), WireOperation("registry.unknown")},
	} {
		if _, err := DecodeWireRequest(input.wire, input.expected); err == nil {
			t.Fatalf("accepted %s", input.wire)
		}
	}
}

func TestDecodeWireRequestAndResponseCoverEveryTypedOperation(t *testing.T) {
	cases := []struct {
		operation WireOperation
		input     any
		value     any
	}{
		{WireListSources, struct{}{}, []RegistrySource{}},
		{WireConfigureSource, ConfigureSourceInput{}, RegistrySource{}},
		{WireConnectSource, RegistryConnectionInput{}, RegistrySource{}},
		{WireDisconnectSource, SourceIDInput{}, RegistrySource{}},
		{WireSearch, SearchInput{}, []ArtifactRelease{}},
		{WirePlan, PlanRequest{}, InstallPlan{}},
		{WireCommit, WireCommitInput{}, Transaction{}},
		{WireGetTransaction, TransactionIDInput{}, Transaction{}},
		{WireRecoverTransaction, TransactionIDInput{}, Transaction{}},
		{WireAuthorArtifact, AuthorArtifactRequest{}, ArtifactRelease{}},
	}
	for _, test := range cases {
		input, err := json.Marshal(test.input)
		if err != nil {
			t.Fatal(err)
		}
		request, err := json.Marshal(WireRequest{WireVersion: RegistryWireVersion, Operation: test.operation, RequestID: "request", Input: input})
		if err != nil {
			t.Fatal(err)
		}
		if _, err := DecodeWireRequest(request, test.operation); err != nil {
			t.Fatalf("%s request: %v", test.operation, err)
		}
		value, err := json.Marshal(test.value)
		if err != nil {
			t.Fatal(err)
		}
		response, err := json.Marshal(WireResponse{WireVersion: RegistryWireVersion, Operation: test.operation, RequestID: "request", OK: true, Value: value})
		if err != nil {
			t.Fatal(err)
		}
		if _, err := DecodeWireResponse(response, test.operation); err != nil {
			t.Fatalf("%s response: %v", test.operation, err)
		}
	}
	unknownRequest := []byte(`{"wire_version":"1.0","operation":"registry.list_sources","request_id":"request","input":{"unexpected":true}}`)
	if _, err := DecodeWireRequest(unknownRequest, WireListSources); err == nil {
		t.Fatal("operation-specific Registry input accepted an unknown field")
	}
	crossValue := []byte(`{"wire_version":"1.0","operation":"registry.list_sources","request_id":"request","ok":true,"value":{"source_id":"source"}}`)
	if _, err := DecodeWireResponse(crossValue, WireListSources); err == nil {
		t.Fatal("cross-operation Registry result accepted")
	}
}

func TestDecodeWireResponseBindsExactOperationAndShape(t *testing.T) {
	valid := []byte(`{"wire_version":"1.0","operation":"registry.list_sources","request_id":"request","ok":true,"value":[]}`)
	response, err := DecodeWireResponse(valid, WireListSources)
	if err != nil || response.Operation != WireListSources || !response.OK {
		t.Fatalf("valid response: %+v %v", response, err)
	}
	validFailure := []byte(`{"wire_version":"1.0","operation":"registry.list_sources","request_id":"request","ok":false,"failure":{"code":"registry.unavailable","subject":"registry","actionable":true}}`)
	if _, err := DecodeWireResponse(validFailure, WireListSources); err != nil {
		t.Fatalf("valid failure response: %v", err)
	}
	for _, wire := range [][]byte{
		nil,
		[]byte(`{"wire_version":"2.0","operation":"registry.list_sources","request_id":"request","ok":true,"value":{}}`),
		[]byte(`{"wire_version":"1.0","operation":"registry.unknown","request_id":"request","ok":true,"value":{}}`),
		[]byte(`{"wire_version":"1.0","operation":"registry.list_sources","request_id":"","ok":true,"value":{}}`),
		[]byte(`{"wire_version":"1.0","operation":"registry.list_sources","request_id":"request","ok":true}`),
		[]byte(`{"wire_version":"1.0","operation":"registry.list_sources","request_id":"request","ok":true,"value":null}`),
		[]byte(`{"wire_version":"1.0","operation":"registry.list_sources","request_id":"request","ok":true,"value":{},"failure":{"code":"x","subject":"x","actionable":false}}`),
		[]byte(`{"wire_version":"1.0","operation":"registry.list_sources","request_id":"request","ok":false}`),
		[]byte(`{"wire_version":"1.0","operation":"registry.list_sources","request_id":"request","ok":false,"value":{},"failure":{"code":"x","subject":"x","actionable":false}}`),
	} {
		if _, err := DecodeWireResponse(wire, WireListSources); err == nil {
			t.Fatalf("accepted %s", wire)
		}
	}
	if _, err := DecodeWireResponse(valid, WireSearch); err == nil {
		t.Fatal("cross-operation Registry response accepted")
	}
}
