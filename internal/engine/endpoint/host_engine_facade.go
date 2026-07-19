// SPDX-License-Identifier: LicenseRef-LayerDraw-1.0

package endpoint

import (
	"context"
	"errors"

	"github.com/dencyuinc/layerdraw/gen/go/engineprotocol"
	"github.com/dencyuinc/layerdraw/gen/go/protocolcommon"
	"github.com/dencyuinc/layerdraw/internal/engine"
)

type HostEngineFacade struct {
	descriptor *Descriptor
	dispatcher *CompileDispatcher
	negotiated *NegotiatedContext
	release    string
}

func NewHostEngineFacade(release, sourceRevision, manifestDigest, instanceID, transportID string) (*HostEngineFacade, error) {
	compiler := engine.New(engine.BuildInfo{ReleaseVersion: release, SourceRevision: sourceRevision})
	descriptor, err := NewCompilerDescriptor(compiler, manifestDigest, instanceID, []string{transportID}, DefaultLimitPolicy())
	if err != nil {
		return nil, err
	}
	request := engineprotocol.HandshakeRequestEnvelope{Operation: engineprotocol.HandshakeRequestEnvelopeOperationValue, Protocol: engineprotocol.EngineProtocolRef{Name: engineprotocol.EngineProtocolRefNameValue, Version: engineprotocol.EngineProtocolRefVersionValue}, RequestID: "host-engine-bootstrap", Payload: protocolcommon.HandshakeRequest{ClientRelease: protocolcommon.ReleaseVersion(release), Protocols: []protocolcommon.ProtocolOffer{{Name: ProtocolName, SupportedRange: "1.0..1.0", Versions: []protocolcommon.ProtocolVersionBinding{{Version: ProtocolVersion, SchemaDigest: protocolcommon.Digest(engineprotocol.SchemaDigest)}}}}, RequiredCapabilities: []protocolcommon.CapabilityID{}, OptionalCapabilities: []protocolcommon.CapabilityID{}}}
	response, negotiated, err := descriptor.Negotiate(context.Background(), request)
	if err != nil || negotiated == nil || response.Outcome != protocolcommon.OutcomeSuccess {
		return nil, errors.New("host Engine bootstrap negotiation failed")
	}
	return &HostEngineFacade{descriptor: descriptor, dispatcher: NewCompileDispatcher(compiler), negotiated: negotiated, release: release}, nil
}

func (f *HostEngineFacade) Descriptor() *Descriptor        { return f.descriptor }
func (f *HostEngineFacade) Dispatcher() *CompileDispatcher { return f.dispatcher }
func (f *HostEngineFacade) Negotiated() *NegotiatedContext { return f.negotiated }
func (f *HostEngineFacade) ReleaseVersion() string         { return f.release }
