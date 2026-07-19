// SPDX-License-Identifier: LicenseRef-LayerDraw-1.0

package registry

import "testing"

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

func TestDecodeWireResponseBindsExactOperationAndShape(t *testing.T) {
	valid := []byte(`{"wire_version":"1.0","operation":"registry.list_sources","request_id":"request","ok":true,"value":{"items":["source"]}}`)
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
