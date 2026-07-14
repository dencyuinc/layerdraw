// SPDX-License-Identifier: LicenseRef-LayerDraw-1.0

package endpoint

import (
	"fmt"

	"github.com/dencyuinc/layerdraw/gen/go/engineprotocol"
	"github.com/dencyuinc/layerdraw/gen/go/protocolcommon"
)

const (
	FailureCompileDuplicateRequest = "engine.compile.duplicate_request_id"
	FailureCompileTransportLimit   = "engine.compile.transport_limit"
	DiagnosticHandshakeState       = "protocol.handshake.invalid_connection_state"
)

// CompileTransportFailure is the closed endpoint-owned set of generated
// outcomes a transport may need before a CompilePlan is dispatched.
type CompileTransportFailure uint8

const (
	CompileTransportDuplicateRequest CompileTransportFailure = iota + 1
	CompileTransportResourceLimit
)

// CompileTransportResponse constructs a stable generated failure without
// allowing a transport to invent protocol fields or compiler diagnostics.
func (d *CompileDispatcher) CompileTransportResponse(requestID string, reason CompileTransportFailure) (engineprotocol.CompileResponseEnvelope, error) {
	if d == nil {
		return engineprotocol.CompileResponseEnvelope{}, fmt.Errorf("nil compile dispatcher")
	}
	if err := ValidateRequestID(requestID); err != nil {
		return engineprotocol.CompileResponseEnvelope{}, fmt.Errorf("untrustworthy compile request ID: %w", err)
	}
	release := protocolcommon.ReleaseVersion(d.compiler.Describe().ReleaseVersion)
	switch reason {
	case CompileTransportDuplicateRequest:
		return compileFailedResponse(requestID, release, protocolFailure(
			protocolcommon.ProtocolFailureCategoryTransport,
			FailureCompileDuplicateRequest,
			"The request ID is already active on this connection.",
			false,
			nil,
		))
	case CompileTransportResourceLimit:
		return compileFailedResponse(requestID, release, protocolFailure(
			protocolcommon.ProtocolFailureCategoryResource,
			FailureCompileTransportLimit,
			"The transport cannot admit this request within its fixed resource limits.",
			true,
			nil,
		))
	default:
		return engineprotocol.CompileResponseEnvelope{}, fmt.Errorf("unknown compile transport failure %d", reason)
	}
}

// RejectNegotiatedHandshake returns the endpoint-owned response for a second
// handshake after a connection has already bound its immutable context.
func (d *Descriptor) RejectNegotiatedHandshake(requestID string) (engineprotocol.HandshakeResponseEnvelope, error) {
	if d == nil {
		return engineprotocol.HandshakeResponseEnvelope{}, fmt.Errorf("nil endpoint descriptor")
	}
	if err := ValidateRequestID(requestID); err != nil {
		return engineprotocol.HandshakeResponseEnvelope{}, fmt.Errorf("untrustworthy handshake request ID: %w", err)
	}
	response, err := d.rejectedResponse(requestID, []protocolcommon.ProtocolDiagnostic{protocolDiagnostic(
		DiagnosticHandshakeState,
		"The connection has already completed its handshake.",
		"Open a new connection to negotiate a different context.",
		nil,
	)})
	return response, err
}
