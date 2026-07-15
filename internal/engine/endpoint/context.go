// SPDX-License-Identifier: LicenseRef-LayerDraw-1.0

package endpoint

import (
	"slices"

	"github.com/dencyuinc/layerdraw/gen/go/protocolcommon"
	"github.com/dencyuinc/layerdraw/internal/engine"
)

// NegotiatedContext is the immutable per-handshake authority consumed by
// later operation dispatch. It is connection/request-session state, never a
// process-global "last handshake" value.
type NegotiatedContext struct {
	endpointInstanceID    protocolcommon.EndpointInstanceID
	manifestETag          protocolcommon.ManifestETag
	engineRelease         protocolcommon.ReleaseVersion
	releaseManifestDigest protocolcommon.Digest
	protocolName          string
	protocolVersion       protocolcommon.ProtocolVersion
	protocolSchemaDigest  protocolcommon.Digest
	operations            []string
	defaultLimits         engine.ResourceLimits
	effectiveMaximums     engine.ResourceLimits
}

// EndpointInstanceID returns the endpoint instance bound by the handshake.
func (context *NegotiatedContext) EndpointInstanceID() protocolcommon.EndpointInstanceID {
	if context == nil {
		return ""
	}
	return context.endpointInstanceID
}

// ManifestETag returns the effective capability-manifest identity.
func (context *NegotiatedContext) ManifestETag() protocolcommon.ManifestETag {
	if context == nil {
		return ""
	}
	return context.manifestETag
}

// EngineRelease returns the endpoint release selected for response metadata.
func (context *NegotiatedContext) EngineRelease() protocolcommon.ReleaseVersion {
	if context == nil {
		return ""
	}
	return context.engineRelease
}

// ReleaseManifestDigest returns the fixed release-set identity.
func (context *NegotiatedContext) ReleaseManifestDigest() protocolcommon.Digest {
	if context == nil {
		return ""
	}
	return context.releaseManifestDigest
}

// ProtocolName returns the selected protocol name.
func (context *NegotiatedContext) ProtocolName() string {
	if context == nil {
		return ""
	}
	return context.protocolName
}

// ProtocolVersion returns the exact selected protocol version.
func (context *NegotiatedContext) ProtocolVersion() protocolcommon.ProtocolVersion {
	if context == nil {
		return ""
	}
	return context.protocolVersion
}

// ProtocolSchemaDigest returns the generated schema-closure digest proved by
// the handshake.
func (context *NegotiatedContext) ProtocolSchemaDigest() protocolcommon.Digest {
	if context == nil {
		return ""
	}
	return context.protocolSchemaDigest
}

// Operations returns a sorted defensive copy of enabled operations.
func (context *NegotiatedContext) Operations() []string {
	if context == nil {
		return nil
	}
	return slices.Clone(context.operations)
}

// SupportsOperation reports exact set membership without prefix, package, or
// method inference.
func (context *NegotiatedContext) SupportsOperation(operation string) bool {
	if context == nil {
		return false
	}
	_, found := slices.BinarySearch(context.operations, operation)
	return found
}

// DefaultCompileLimits returns the defaults capped by the negotiated maxima.
// A later compile request's zero or omitted override selects these values.
func (context *NegotiatedContext) DefaultCompileLimits() engine.ResourceLimits {
	if context == nil {
		return engine.ResourceLimits{}
	}
	return context.defaultLimits
}

// EffectiveMaximumCompileLimits returns the client-scoped ceilings a later
// compile request must not exceed.
func (context *NegotiatedContext) EffectiveMaximumCompileLimits() engine.ResourceLimits {
	if context == nil {
		return engine.ResourceLimits{}
	}
	return context.effectiveMaximums
}
