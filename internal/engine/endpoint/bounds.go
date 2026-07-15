// SPDX-License-Identifier: LicenseRef-LayerDraw-1.0

package endpoint

import (
	"unicode/utf8"

	"github.com/dencyuinc/layerdraw/gen/go/engineprotocol"
	"github.com/dencyuinc/layerdraw/gen/go/protocolcommon"
)

// These Engine Protocol 1.0 policy bounds mirror the generated schema
// authority. They are deliberately small relative to the shared 8 MiB wire
// ceiling: a maximum request can be inspected without cloning its collections,
// and every response materialized from the bounded fields remains below that
// ceiling.
const (
	maxRequestIDLength            = 128
	maxReleaseVersionLength       = 128
	maxCapabilityIDLength         = 128
	maxProtocolNameLength         = 64
	maxProtocolVersionLength      = 21
	maxProtocolRangeLength        = 44
	maxSchemaDigestLength         = 71
	maxPositiveInt64Length        = 19
	maxRfc3339TimeLength          = 30
	maxHandshakeProtocols         = 8
	maxProtocolVersionsPerOffer   = 16
	maxRequiredCapabilities       = 64
	maxOptionalCapabilities       = 64
	maxEndpointTransports         = 16
	maxEndpointTransportIDLength  = 64
	maximumHandshakeResponseBytes = 128 * 1024
)

func trustworthyRequestID(requestID string) bool {
	if requestID == "" || !utf8.ValidString(requestID) {
		return false
	}
	return utf8.RuneCountInString(requestID) <= maxRequestIDLength
}

// exceedsHandshakePolicy performs only bounded, allocation-free checks. In
// particular, it rejects oversized typed slices before generated validation
// can marshal them or capability normalization can clone and sort them.
func exceedsHandshakePolicy(request engineprotocol.HandshakeRequestEnvelope) bool {
	payload := request.Payload
	if len(payload.Protocols) > maxHandshakeProtocols ||
		len(payload.RequiredCapabilities) > maxRequiredCapabilities ||
		len(payload.OptionalCapabilities) > maxOptionalCapabilities ||
		len(payload.ClientRelease) > maxReleaseVersionLength {
		return true
	}
	if request.DeadlineAt != nil && len(*request.DeadlineAt) > maxRfc3339TimeLength {
		return true
	}
	for _, offer := range payload.Protocols {
		if len(offer.Name) > maxProtocolNameLength || len(offer.SupportedRange) > maxProtocolRangeLength || len(offer.Versions) > maxProtocolVersionsPerOffer {
			return true
		}
		for _, binding := range offer.Versions {
			if len(binding.Version) > maxProtocolVersionLength || len(binding.SchemaDigest) > maxSchemaDigestLength {
				return true
			}
		}
	}
	return capabilityIDsExceedPolicy(payload.RequiredCapabilities) || capabilityIDsExceedPolicy(payload.OptionalCapabilities) || clientLimitsExceedPolicy(payload.ClientLimits)
}

func capabilityIDsExceedPolicy(capabilities []protocolcommon.CapabilityID) bool {
	for _, capability := range capabilities {
		// CapabilityID's generated pattern is ASCII-only, so bytes and schema
		// Unicode code points have the same maximum-length boundary.
		if len(capability) > maxCapabilityIDLength {
			return true
		}
	}
	return false
}

func clientLimitsExceedPolicy(limits *protocolcommon.CompileResourceLimitConstraints) bool {
	if limits == nil {
		return false
	}
	values := []*protocolcommon.CanonicalPositiveInt64{
		limits.MaxAssetBytes,
		limits.MaxAssets,
		limits.MaxDeclarations,
		limits.MaxPackBytes,
		limits.MaxPackFiles,
		limits.MaxProjectSourceBytes,
		limits.MaxProjectSourceFiles,
		limits.MaxRasterDimension,
		limits.MaxRasterPixels,
	}
	for _, value := range values {
		if value != nil && len(*value) > maxPositiveInt64Length {
			return true
		}
	}
	return false
}
