// SPDX-License-Identifier: LicenseRef-LayerDraw-1.0

package endpoint

import (
	"context"
	"fmt"
	"slices"

	"github.com/dencyuinc/layerdraw/gen/go/engineprotocol"
	"github.com/dencyuinc/layerdraw/gen/go/protocolcommon"
)

// Negotiate validates one generated Engine handshake request and returns one
// generated response plus an immutable per-client context on success. The
// error result is reserved for caller misuse that cannot be represented by a
// valid response (nil context, nil descriptor, or an untrustworthy request ID)
// and for an impossible response-construction invariant.
func (d *Descriptor) Negotiate(ctx context.Context, request engineprotocol.HandshakeRequestEnvelope) (engineprotocol.HandshakeResponseEnvelope, *NegotiatedContext, error) {
	if d == nil {
		return engineprotocol.HandshakeResponseEnvelope{}, nil, fmt.Errorf("nil endpoint descriptor")
	}
	if ctx == nil {
		return engineprotocol.HandshakeResponseEnvelope{}, nil, fmt.Errorf("nil handshake context")
	}
	if !trustworthyRequestID(request.RequestID) {
		return engineprotocol.HandshakeResponseEnvelope{}, nil, fmt.Errorf("handshake request ID must contain 1..%d valid UTF-8 code points", maxRequestIDLength)
	}
	if ctx.Err() != nil {
		return d.cancelled(request.RequestID)
	}
	if request.Operation != engineprotocol.HandshakeRequestEnvelopeOperationValue || request.Protocol != bootstrapProtocolRef() {
		return d.reject(request.RequestID, invalidHandshakeDiagnostic(DiagnosticReasonInvalidEnvelope))
	}
	if exceedsHandshakePolicy(request) {
		return d.reject(request.RequestID, invalidHandshakeDiagnostic(DiagnosticReasonPolicyLimitExceeded))
	}
	if _, err := engineprotocol.EncodeHandshakeRequestEnvelope(request); err != nil {
		return d.reject(request.RequestID, invalidHandshakeDiagnostic(DiagnosticReasonInvalidEnvelope))
	}
	if ctx.Err() != nil {
		return d.cancelled(request.RequestID)
	}

	offer, found := engineOffer(request.Payload.Protocols)
	if !found {
		return d.reject(request.RequestID, invalidHandshakeDiagnostic(DiagnosticReasonMissingEngineOffer))
	}
	selected, selection := selectProtocol(d.protocols, offer)
	if selection != selectionCompatible {
		diagnostic, err := selectionDiagnostic(selection, offer, d.protocols)
		if err != nil {
			return d.invariantFailure(request.RequestID, err)
		}
		return d.reject(request.RequestID, diagnostic)
	}
	if ctx.Err() != nil {
		return d.cancelled(request.RequestID)
	}

	required, optional, detail, valid := normalizeCapabilityRequests(request.Payload.RequiredCapabilities, request.Payload.OptionalCapabilities)
	if !valid {
		return d.reject(request.RequestID, invalidHandshakeDiagnostic(detail))
	}
	missing := missingRequiredCapabilities(d.operations, required)
	if len(missing) != 0 {
		return d.reject(request.RequestID, missingCapabilitiesDiagnostic(missing))
	}
	statuses := requestedCapabilityStatuses(d.operations, required, optional, selected.wireVersion)
	if ctx.Err() != nil {
		return d.cancelled(request.RequestID)
	}

	defaultLimits, maximumLimits, err := effectiveLimits(d.limits, request.Payload.ClientLimits)
	if err != nil {
		return d.invariantFailure(request.RequestID, err)
	}
	manifest, err := d.capabilityManifest(request.Payload.ClientLimits != nil, selected, maximumLimits)
	if err != nil {
		return d.invariantFailure(request.RequestID, err)
	}
	if ctx.Err() != nil {
		return d.cancelled(request.RequestID)
	}

	result := protocolcommon.HandshakeResult{
		CapabilityManifest: manifest,
		CapabilityStatuses: statuses,
		EndpointInstanceID: d.endpointInstanceID,
		HostRelease:        d.engineRelease,
		NegotiatedProtocols: []protocolcommon.NegotiatedProtocol{{
			Name:         ProtocolName,
			SchemaDigest: selected.schemaDigest,
			Version:      selected.wireVersion,
		}},
		ReleaseManifestDigest: d.releaseManifestDigest,
	}
	response, err := d.successResponse(request.RequestID, result)
	if err != nil {
		return d.invariantFailure(request.RequestID, err)
	}
	negotiated := &NegotiatedContext{
		endpointInstanceID:    d.endpointInstanceID,
		manifestETag:          manifest.ManifestEtag,
		engineRelease:         d.engineRelease,
		releaseManifestDigest: d.releaseManifestDigest,
		protocolName:          ProtocolName,
		protocolVersion:       selected.wireVersion,
		protocolSchemaDigest:  selected.schemaDigest,
		operations:            slices.Clone(d.operations),
		defaultLimits:         defaultLimits,
		effectiveMaximums:     maximumLimits,
	}
	return response, negotiated, nil
}

func (d *Descriptor) reject(requestID string, diagnostic protocolcommon.ProtocolDiagnostic) (engineprotocol.HandshakeResponseEnvelope, *NegotiatedContext, error) {
	response, err := d.rejectedResponse(requestID, []protocolcommon.ProtocolDiagnostic{diagnostic})
	if err != nil {
		return d.invariantFailure(requestID, err)
	}
	return response, nil, nil
}

func (d *Descriptor) cancelled(requestID string) (engineprotocol.HandshakeResponseEnvelope, *NegotiatedContext, error) {
	response, err := d.cancelledResponse(requestID)
	if err != nil {
		return engineprotocol.HandshakeResponseEnvelope{}, nil, err
	}
	return response, nil, nil
}

func (d *Descriptor) invariantFailure(requestID string, cause error) (engineprotocol.HandshakeResponseEnvelope, *NegotiatedContext, error) {
	response, err := d.failedResponse(requestID)
	if err != nil {
		return engineprotocol.HandshakeResponseEnvelope{}, nil, fmt.Errorf("handshake invariant (%v): %w", cause, err)
	}
	return response, nil, nil
}

func engineOffer(offers []protocolcommon.ProtocolOffer) (protocolcommon.ProtocolOffer, bool) {
	for _, offer := range offers {
		if offer.Name == ProtocolName {
			return offer, true
		}
	}
	return protocolcommon.ProtocolOffer{}, false
}

func normalizeCapabilityRequests(requiredInput, optionalInput []protocolcommon.CapabilityID) ([]protocolcommon.CapabilityID, []protocolcommon.CapabilityID, string, bool) {
	required, duplicateRequired := uniqueSortedCapabilities(requiredInput)
	optional, duplicateOptional := uniqueSortedCapabilities(optionalInput)
	if duplicateRequired || duplicateOptional {
		return nil, nil, DiagnosticReasonDuplicateCapability, false
	}
	for _, capability := range required {
		if _, found := slices.BinarySearch(optional, capability); found {
			return nil, nil, DiagnosticReasonCapabilityOverlap, false
		}
	}
	if required == nil || optional == nil {
		return nil, nil, DiagnosticReasonInvalidCapabilitySets, false
	}
	return required, optional, "", true
}

func uniqueSortedCapabilities(input []protocolcommon.CapabilityID) ([]protocolcommon.CapabilityID, bool) {
	if input == nil {
		return nil, false
	}
	result := slices.Clone(input)
	slices.Sort(result)
	for index := 1; index < len(result); index++ {
		if result[index-1] == result[index] {
			return result, true
		}
	}
	return result, false
}

func missingRequiredCapabilities(operations []string, required []protocolcommon.CapabilityID) []string {
	missing := make([]string, 0)
	for _, capability := range required {
		if _, found := slices.BinarySearch(operations, string(capability)); !found {
			missing = append(missing, string(capability))
		}
	}
	return missing
}

func requestedCapabilityStatuses(operations []string, required, optional []protocolcommon.CapabilityID, version protocolcommon.ProtocolVersion) []protocolcommon.RequestedCapabilityStatus {
	all := make([]protocolcommon.CapabilityID, 0, len(required)+len(optional))
	all = append(all, required...)
	all = append(all, optional...)
	slices.Sort(all)
	statuses := make([]protocolcommon.RequestedCapabilityStatus, 0, len(all))
	for _, capability := range all {
		_, enabled := slices.BinarySearch(operations, string(capability))
		status := protocolcommon.RequestedCapabilityStatus{
			CapabilityID:    capability,
			Enabled:         enabled,
			ProtocolVersion: version,
		}
		if !enabled {
			reason := protocolcommon.UnavailableReasonUnsupported
			status.UnavailableReason = &reason
		}
		statuses = append(statuses, status)
	}
	return statuses
}

func invalidHandshakeDiagnostic(detail string) protocolcommon.ProtocolDiagnostic {
	return protocolDiagnostic(
		DiagnosticInvalidHandshake,
		"The handshake request is invalid.",
		"Send an unambiguous generated Engine handshake request.",
		protocolcommon.JsonObject{DiagnosticDataReason: stringJSON(detail)},
	)
}

func selectionDiagnostic(selection selectionFailure, offer protocolcommon.ProtocolOffer, catalog []protocolBinding) (protocolcommon.ProtocolDiagnostic, error) {
	switch selection {
	case selectionMajorMismatch:
		data, err := upgradeData(protocolcommon.ProtocolVersionOrRange(offer.SupportedRange))
		if err != nil {
			return protocolcommon.ProtocolDiagnostic{}, err
		}
		return protocolDiagnostic(
			DiagnosticMajorVersionMismatch,
			"The client and Engine do not support a common protocol major version.",
			"Use client and Engine artifacts that support the same Engine Protocol major version.",
			data,
		), nil
	case selectionRangeMismatch:
		data, err := upgradeData(protocolcommon.ProtocolVersionOrRange(offer.SupportedRange))
		if err != nil {
			return protocolcommon.ProtocolDiagnostic{}, err
		}
		return protocolDiagnostic(
			DiagnosticVersionRangeMismatch,
			"The client and Engine protocol version ranges do not overlap.",
			"Use client and Engine artifacts with overlapping Engine Protocol versions.",
			data,
		), nil
	case selectionSchemaDigestMismatch:
		offered := make([]string, 0)
		for _, binding := range offer.Versions {
			for _, host := range catalog {
				if binding.Version == host.wireVersion {
					offered = append(offered, string(binding.SchemaDigest))
				}
			}
		}
		slices.Sort(offered)
		offered = slices.Compact(offered)
		return protocolDiagnostic(
			DiagnosticSchemaDigestMismatch,
			"The offered Engine schema digest does not match the generated Engine schema.",
			"Install client and Engine artifacts from a compatible release set.",
			protocolcommon.JsonObject{
				DiagnosticDataOfferedSchemaDigests: stringsJSON(offered),
				"protocol_name":                    stringJSON(ProtocolName),
				DiagnosticDataRequiredSchemaDigest: stringJSON(string(catalog[0].schemaDigest)),
				"version":                          stringJSON(string(catalog[0].wireVersion)),
			},
		), nil
	default:
		return protocolcommon.ProtocolDiagnostic{}, fmt.Errorf("unknown protocol selection failure %d", selection)
	}
}

func upgradeData(required protocolcommon.ProtocolVersionOrRange) (protocolcommon.JsonObject, error) {
	data := protocolcommon.UpgradeDiagnosticData{
		AffectedArtifacts:      []string{ProtocolName},
		CurrentVersion:         protocolcommon.ProtocolVersion(ProtocolVersion),
		MigrationAvailable:     false,
		ReadonlyPossible:       false,
		RequiredVersionOrRange: required,
	}
	encoded, err := protocolcommon.EncodeUpgradeDiagnosticData(data)
	if err != nil {
		return nil, fmt.Errorf("encode upgrade diagnostic data: %w", err)
	}
	result, err := protocolcommon.DecodeJsonObject(encoded)
	if err != nil {
		return nil, fmt.Errorf("convert upgrade diagnostic data: %w", err)
	}
	return result, nil
}

func missingCapabilitiesDiagnostic(missing []string) protocolcommon.ProtocolDiagnostic {
	return protocolDiagnostic(
		DiagnosticRequiredCapabilityMissing,
		"One or more required capabilities are unavailable.",
		"Use an endpoint that provides the requested capabilities or make them optional.",
		protocolcommon.JsonObject{DiagnosticDataMissingCapabilities: stringsJSON(slices.Clone(missing))},
	)
}
