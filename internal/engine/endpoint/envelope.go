// SPDX-License-Identifier: LicenseRef-LayerDraw-1.0

package endpoint

import (
	"fmt"

	"github.com/dencyuinc/layerdraw/gen/go/engineprotocol"
	"github.com/dencyuinc/layerdraw/gen/go/protocolcommon"
)

const (
	// DiagnosticInvalidHandshake identifies malformed or ambiguous handshake policy input.
	DiagnosticInvalidHandshake = "protocol.invalid_handshake"
	// DiagnosticMajorVersionMismatch identifies a protocol-major incompatibility.
	DiagnosticMajorVersionMismatch = "protocol.major_version_mismatch"
	// DiagnosticVersionRangeMismatch identifies a same-major range incompatibility.
	DiagnosticVersionRangeMismatch = "protocol.version_range_mismatch"
	// DiagnosticSchemaDigestMismatch identifies corrupt or incompatible exact-version artifacts.
	DiagnosticSchemaDigestMismatch = "protocol.schema_digest_mismatch"
	// DiagnosticRequiredCapabilityMissing identifies unavailable required capabilities.
	DiagnosticRequiredCapabilityMissing = "protocol.required_capability_missing"
	// DiagnosticLimitIncompatible is reserved for an unsatisfied valid limit contract.
	DiagnosticLimitIncompatible = "protocol.limit_incompatible"

	// FailureHandshakeCancelled identifies caller cancellation or deadline expiry.
	FailureHandshakeCancelled = "engine.handshake.cancelled"
	// FailureHandshakeInvariant identifies a safe unexpected endpoint invariant failure.
	FailureHandshakeInvariant = "engine.handshake.invariant_failure"
)

const (
	// DiagnosticDataReason is the safe detail key used by invalid-handshake diagnostics.
	DiagnosticDataReason = "reason"
	// DiagnosticDataMissingCapabilities contains only sorted capability IDs requested by the caller.
	DiagnosticDataMissingCapabilities = "missing_capabilities"
	// DiagnosticDataOfferedSchemaDigests contains only exact-version digests offered by the caller.
	DiagnosticDataOfferedSchemaDigests = "offered_schema_digests"
	// DiagnosticDataRequiredSchemaDigest is the public generated Engine closure digest.
	DiagnosticDataRequiredSchemaDigest = "required_schema_digest"

	// DiagnosticReasonInvalidEnvelope identifies generated-shape or bootstrap identity failure.
	DiagnosticReasonInvalidEnvelope = "invalid_envelope"
	// DiagnosticReasonMissingEngineOffer identifies a request without an Engine offer.
	DiagnosticReasonMissingEngineOffer = "missing_engine_offer"
	// DiagnosticReasonDuplicateCapability identifies a repeated required or optional ID.
	DiagnosticReasonDuplicateCapability = "duplicate_capability"
	// DiagnosticReasonCapabilityOverlap identifies one ID in both required and optional sets.
	DiagnosticReasonCapabilityOverlap = "required_optional_overlap"
	// DiagnosticReasonInvalidCapabilitySets identifies absent typed capability arrays.
	DiagnosticReasonInvalidCapabilitySets = "invalid_capability_sets"
)

func bootstrapProtocolRef() engineprotocol.EngineProtocolRef {
	return engineprotocol.EngineProtocolRef{
		Name:    engineprotocol.EngineProtocolRefNameValue,
		Version: engineprotocol.EngineProtocolRefVersionValue,
	}
}

func (d *Descriptor) successResponse(requestID string, result protocolcommon.HandshakeResult) (engineprotocol.HandshakeResponseEnvelope, error) {
	response := engineprotocol.HandshakeResponseEnvelope{
		Diagnostics:   []protocolcommon.ProtocolDiagnostic{},
		EngineRelease: d.engineRelease,
		Outcome:       protocolcommon.OutcomeSuccess,
		Payload:       &result,
		Protocol:      bootstrapProtocolRef(),
		RequestID:     requestID,
	}
	return validateResponse(response)
}

func (d *Descriptor) rejectedResponse(requestID string, diagnostics []protocolcommon.ProtocolDiagnostic) (engineprotocol.HandshakeResponseEnvelope, error) {
	response := engineprotocol.HandshakeResponseEnvelope{
		Diagnostics:   cloneDiagnostics(diagnostics),
		EngineRelease: d.engineRelease,
		Outcome:       protocolcommon.OutcomeRejected,
		Protocol:      bootstrapProtocolRef(),
		RequestID:     requestID,
	}
	return validateResponse(response)
}

func (d *Descriptor) failedResponse(requestID string) (engineprotocol.HandshakeResponseEnvelope, error) {
	failure := protocolcommon.ProtocolFailure{
		Category:  protocolcommon.ProtocolFailureCategoryInvariant,
		Code:      FailureHandshakeInvariant,
		Message:   "The Engine could not complete the handshake.",
		Retryable: false,
	}
	response := engineprotocol.HandshakeResponseEnvelope{
		Diagnostics:   []protocolcommon.ProtocolDiagnostic{},
		EngineRelease: d.engineRelease,
		Failure:       &failure,
		Outcome:       protocolcommon.OutcomeFailed,
		Protocol:      bootstrapProtocolRef(),
		RequestID:     requestID,
	}
	return validateResponse(response)
}

func (d *Descriptor) cancelledResponse(requestID string) (engineprotocol.HandshakeResponseEnvelope, error) {
	failure := protocolcommon.ProtocolFailure{
		Category:  protocolcommon.ProtocolFailureCategoryCancelled,
		Code:      FailureHandshakeCancelled,
		Message:   "The handshake was cancelled.",
		Retryable: true,
	}
	response := engineprotocol.HandshakeResponseEnvelope{
		Diagnostics:   []protocolcommon.ProtocolDiagnostic{},
		EngineRelease: d.engineRelease,
		Failure:       &failure,
		Outcome:       protocolcommon.OutcomeCancelled,
		Protocol:      bootstrapProtocolRef(),
		RequestID:     requestID,
	}
	return validateResponse(response)
}

func validateResponse(response engineprotocol.HandshakeResponseEnvelope) (engineprotocol.HandshakeResponseEnvelope, error) {
	if _, err := engineprotocol.EncodeHandshakeResponseEnvelope(response); err != nil {
		return engineprotocol.HandshakeResponseEnvelope{}, fmt.Errorf("constructed handshake response is invalid: %w", err)
	}
	return response, nil
}

func cloneDiagnostics(input []protocolcommon.ProtocolDiagnostic) []protocolcommon.ProtocolDiagnostic {
	if input == nil {
		return nil
	}
	result := make([]protocolcommon.ProtocolDiagnostic, len(input))
	for index, diagnostic := range input {
		result[index] = diagnostic
		result[index].Data = cloneJSONMap(diagnostic.Data)
		if diagnostic.Remediation != nil {
			remediation := *diagnostic.Remediation
			result[index].Remediation = &remediation
		}
		if diagnostic.Related != nil {
			result[index].Related = make([]protocolcommon.ProtocolDiagnosticRelated, len(diagnostic.Related))
			copy(result[index].Related, diagnostic.Related)
		}
		for relatedIndex := range result[index].Related {
			result[index].Related[relatedIndex].Source = cloneDiagnosticSource(result[index].Related[relatedIndex].Source)
		}
		result[index].Source = cloneDiagnosticSource(diagnostic.Source)
	}
	return result
}

func cloneDiagnosticSource(input *protocolcommon.ProtocolDiagnosticSource) *protocolcommon.ProtocolDiagnosticSource {
	if input == nil {
		return nil
	}
	result := *input
	if input.StableAddress != nil {
		stableAddress := *input.StableAddress
		result.StableAddress = &stableAddress
	}
	return &result
}

func cloneJSONMap(input *protocolcommon.JsonObject) *protocolcommon.JsonObject {
	if input == nil {
		return nil
	}
	result := make(protocolcommon.JsonObject, len(*input))
	for key, value := range *input {
		result[key] = cloneJSONValue(value)
	}
	return &result
}

func cloneJSONValue(input protocolcommon.JsonValue) protocolcommon.JsonValue {
	result := input
	if input.Array != nil {
		result.Array = make([]protocolcommon.JsonValue, len(input.Array))
		for index, value := range input.Array {
			result.Array[index] = cloneJSONValue(value)
		}
	}
	if input.Object != nil {
		result.Object = make(map[string]protocolcommon.JsonValue, len(input.Object))
		for key, value := range input.Object {
			result.Object[key] = cloneJSONValue(value)
		}
	}
	return result
}

func stringJSON(value string) protocolcommon.JsonValue {
	return protocolcommon.JsonValue{Kind: protocolcommon.JsonValueKindString, String: value}
}

func stringsJSON(values []string) protocolcommon.JsonValue {
	items := make([]protocolcommon.JsonValue, len(values))
	for index, value := range values {
		items[index] = stringJSON(value)
	}
	return protocolcommon.JsonValue{Kind: protocolcommon.JsonValueKindArray, Array: items}
}

func protocolDiagnostic(code, message, remediation string, data protocolcommon.JsonObject) protocolcommon.ProtocolDiagnostic {
	result := protocolcommon.ProtocolDiagnostic{
		Code:     code,
		Message:  message,
		Related:  []protocolcommon.ProtocolDiagnosticRelated{},
		Severity: protocolcommon.ProtocolDiagnosticSeverityError,
	}
	if remediation != "" {
		result.Remediation = &remediation
	}
	if data != nil {
		result.Data = &data
	}
	return result
}
