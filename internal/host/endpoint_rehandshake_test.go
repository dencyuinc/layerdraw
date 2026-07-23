// SPDX-License-Identifier: LicenseRef-LayerDraw-1.0

package host

import (
	"context"
	"testing"

	"github.com/dencyuinc/layerdraw/gen/go/protocolcommon"
	"github.com/dencyuinc/layerdraw/gen/go/runtimeprotocol"
)

// TestRehandshakeReplaysNegotiatedResult covers the frontend-reload path: a
// valid repeat handshake replays the negotiated manifest with success instead
// of killing the runtime channel.
func TestRehandshakeReplaysNegotiatedResult(t *testing.T) {
	endpoint := newTestEndpoint(t)
	handshakeTestEndpoint(t, endpoint)
	request := runtimeprotocol.RuntimeHandshakeRequestEnvelope{
		Operation: runtimeprotocol.RuntimeHandshakeRequestEnvelopeOperationValue,
		Protocol:  runtimeprotocol.RuntimeProtocolRef{Name: runtimeprotocol.RuntimeProtocolRefNameValue, Version: "1.0"},
		RequestID: "rehandshake_request",
		Payload: runtimeprotocol.RuntimeHandshakeRequest{
			ClientRelease:        "1.0.0",
			Protocols:            []protocolcommon.ProtocolOffer{{Name: "runtime", SupportedRange: "1.0..1.0", Versions: []protocolcommon.ProtocolVersionBinding{{Version: "1.0", SchemaDigest: protocolcommon.Digest(runtimeprotocol.SchemaDigest)}}}},
			RequiredCapabilities: []protocolcommon.CapabilityID{"runtime.commit_operations"},
			OptionalCapabilities: []protocolcommon.CapabilityID{"runtime.unsupported"},
		},
	}
	control, err := runtimeprotocol.EncodeRuntimeHandshakeRequestEnvelope(request)
	if err != nil {
		t.Fatal(err)
	}
	response, accepted, err := endpoint.Handshake(context.Background(), control)
	if err != nil || !accepted || response.Outcome != protocolcommon.OutcomeSuccess {
		t.Fatalf("re-handshake accepted=%v outcome=%s err=%v", accepted, response.Outcome, err)
	}
	decoded, err := runtimeprotocol.DecodeRuntimeHandshakeResponseEnvelope(response.Control)
	if err != nil || decoded.Payload == nil {
		t.Fatalf("re-handshake payload missing: %v", err)
	}
	statuses := decoded.Payload.CapabilityStatuses
	foundRequired, foundOptional := false, false
	for _, status := range statuses {
		if status.CapabilityID == "runtime.commit_operations" && status.Enabled {
			foundRequired = true
		}
		if status.CapabilityID == "runtime.unsupported" && !status.Enabled && status.UnavailableReason != nil {
			foundOptional = true
		}
	}
	if !foundRequired || !foundOptional {
		t.Fatalf("re-handshake statuses incomplete: %+v", statuses)
	}
}
