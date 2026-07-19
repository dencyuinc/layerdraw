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
