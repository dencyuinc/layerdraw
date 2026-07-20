// SPDX-License-Identifier: LicenseRef-LayerDraw-1.0

package desktopwails

import (
	"context"
	"path/filepath"
	"testing"

	"github.com/dencyuinc/layerdraw/gen/go/engineprotocol"
	"github.com/dencyuinc/layerdraw/gen/go/protocolcommon"
	"github.com/dencyuinc/layerdraw/internal/desktopcontract"
)

func TestFrontendBridgeExposesContextFreeGeneratedInvoke(t *testing.T) {
	base, err := NewSharedConfig(filepath.Join(t.TempDir(), "data"))
	if err != nil {
		t.Fatal(err)
	}
	application, err := Compose(base, &nativeStub{}, nil)
	if err != nil {
		t.Fatal(err)
	}
	bridge := NewFrontendBridge(application)
	bridge.setContext(context.Background())
	if started := application.Start(context.Background()); started.Outcome != protocolcommon.OutcomeSuccess {
		t.Fatalf("start: %+v", started)
	}
	defer application.Shutdown(context.Background())
	control, err := engineprotocol.EncodeHandshakeRequestEnvelope(engineprotocol.HandshakeRequestEnvelope{
		Operation: engineprotocol.HandshakeRequestEnvelopeOperationValue,
		Protocol:  engineprotocol.EngineProtocolRef{Name: engineprotocol.EngineProtocolRefNameValue, Version: engineprotocol.EngineProtocolRefVersionValue},
		RequestID: "frontend-bridge",
		Payload: protocolcommon.HandshakeRequest{ClientRelease: "0.0.0-dev",
			Protocols:            []protocolcommon.ProtocolOffer{{Name: "engine", SupportedRange: "1.0..1.0", Versions: []protocolcommon.ProtocolVersionBinding{{Version: "1.0", SchemaDigest: protocolcommon.Digest(engineprotocol.SchemaDigest)}}}},
			RequiredCapabilities: []protocolcommon.CapabilityID{}, OptionalCapabilities: []protocolcommon.CapabilityID{}},
	})
	if err != nil {
		t.Fatal(err)
	}
	result := bridge.Invoke("EngineHandshake", desktopcontract.Exchange{Operation: "engine.handshake", Control: control, Blobs: []desktopcontract.Blob{}})
	if result.Outcome != protocolcommon.OutcomeSuccess || !result.Validate() || bridge.State() != desktopcontract.LifecycleReady {
		t.Fatalf("invoke=%+v state=%s", result, bridge.State())
	}
}
