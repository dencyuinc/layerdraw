// SPDX-License-Identifier: LicenseRef-LayerDraw-1.0

// Package endpoint implements the transport-neutral Engine Protocol endpoint.
// It owns handshake compatibility policy, immutable negotiated contexts, and
// the sole handwritten mapping boundary between generated protocol values and
// the canonical Engine facade. It does not own a transport.
package endpoint

import (
	"fmt"
	"regexp"
	"slices"

	"github.com/dencyuinc/layerdraw/gen/go/engineprotocol"
	"github.com/dencyuinc/layerdraw/gen/go/protocolcommon"
	"github.com/dencyuinc/layerdraw/internal/engine"
)

const (
	// ProtocolName is the only protocol negotiated by this endpoint.
	ProtocolName = "engine"
	// ProtocolVersion is the exact Engine Protocol version implemented here.
	ProtocolVersion = "1.0"

	// OperationHandshake is the Engine Protocol bootstrap operation.
	OperationHandshake = "engine.handshake"
	// OperationCompile is the closed-input compiler operation wired by the Go Engine.
	OperationCompile            = "engine.compile"
	OperationApplyToHandle      = "engine.apply_to_handle"
	OperationCloseDocument      = "engine.close_document"
	OperationExecuteQuery       = "engine.execute_query"
	OperationFindSymbols        = "engine.find_symbols"
	OperationFindUsages         = "engine.find_usages"
	OperationFormatScope        = "engine.format_scope"
	OperationGetNeighbors       = "engine.get_neighbors"
	OperationInspectSubgraph    = "engine.inspect_subgraph"
	OperationListModules        = "engine.list_modules"
	OperationListReferences     = "engine.list_references"
	OperationMaterializeView    = "engine.materialize_view"
	OperationPlanExport         = "engine.plan_export"
	OperationOpenDocument       = "engine.open_document"
	OperationOrganizeWorkspace  = "engine.organize_workspace"
	OperationPreviewFragment    = "engine.preview_fragment"
	OperationPreviewOperations  = "engine.preview_operations"
	OperationPreviewSourcePatch = "engine.preview_source_patch"
	OperationReadDeclarations   = "engine.read_declarations"
	OperationReadModules        = "engine.read_modules"
	OperationReadReferences     = "engine.read_references"
	OperationReadRows           = "engine.read_rows"
	OperationReadScope          = "engine.read_scope"
	OperationReplaceSourceTree  = "engine.replace_source_tree"

	// TransportInProcess is the stable identifier for typed in-process calls.
	TransportInProcess = "in_process"
)

// LimitPolicy separates compiler defaults from endpoint-enforced hard maxima.
// Every field must be positive and a default must not exceed its corresponding
// hard maximum.
type LimitPolicy struct {
	Defaults     engine.ResourceLimits
	HardMaximums engine.ResourceLimits
}

// DefaultLimitPolicy returns the compiler facade defaults as both defaults and
// hard maxima. Composition roots may explicitly provide larger hard maxima.
func DefaultLimitPolicy() LimitPolicy {
	defaults := engine.DefaultResourceLimits()
	return LimitPolicy{Defaults: defaults, HardMaximums: defaults}
}

// DescriptorConfig contains only injected, non-secret endpoint identity and
// release metadata. SourceRevision is internal provenance and is never emitted
// by the handshake.
type DescriptorConfig struct {
	EngineRelease         protocolcommon.ReleaseVersion
	SourceRevision        string
	ReleaseManifestDigest protocolcommon.Digest
	EndpointInstanceID    protocolcommon.EndpointInstanceID
	Transports            []string
	Limits                LimitPolicy
}

// Descriptor is an immutable validated Engine endpoint description. Its
// operation catalog is deliberately fixed rather than inferred from linked
// packages or methods.
type Descriptor struct {
	engineRelease         protocolcommon.ReleaseVersion
	sourceRevision        string
	releaseManifestDigest protocolcommon.Digest
	endpointInstanceID    protocolcommon.EndpointInstanceID
	transports            []string
	operations            []string
	limits                LimitPolicy
	protocols             []protocolBinding
}

// NewDescriptor validates and defensively copies one portable compiler
// endpoint configuration.
func NewDescriptor(config DescriptorConfig) (*Descriptor, error) {
	if _, err := protocolcommon.EncodeReleaseVersion(config.EngineRelease); err != nil {
		return nil, fmt.Errorf("engine release: %w", err)
	}
	if config.SourceRevision != engine.UnknownSourceRevision {
		matched, err := regexp.MatchString(`^[0-9a-f]{7,64}$`, config.SourceRevision)
		if err != nil || !matched {
			return nil, fmt.Errorf("source revision must be %q or 7-64 lowercase hexadecimal characters", engine.UnknownSourceRevision)
		}
	}
	if _, err := protocolcommon.EncodeDigest(config.ReleaseManifestDigest); err != nil {
		return nil, fmt.Errorf("release manifest digest: %w", err)
	}
	if _, err := protocolcommon.EncodeEndpointInstanceID(config.EndpointInstanceID); err != nil {
		return nil, fmt.Errorf("endpoint instance ID: %w", err)
	}
	transports, err := validateTransports(config.Transports)
	if err != nil {
		return nil, err
	}
	if err := validateLimitPolicy(config.Limits); err != nil {
		return nil, err
	}
	if _, err := protocolcommon.EncodeDigest(protocolcommon.Digest(engineprotocol.SchemaDigest)); err != nil {
		return nil, fmt.Errorf("generated Engine schema digest: %w", err)
	}

	return &Descriptor{
		engineRelease:         config.EngineRelease,
		sourceRevision:        config.SourceRevision,
		releaseManifestDigest: config.ReleaseManifestDigest,
		endpointInstanceID:    config.EndpointInstanceID,
		transports:            transports,
		operations: []string{
			OperationApplyToHandle,
			OperationCloseDocument,
			OperationCompile,
			OperationExecuteQuery,
			OperationFindSymbols,
			OperationFindUsages,
			OperationFormatScope,
			OperationGetNeighbors,
			OperationHandshake,
			OperationInspectSubgraph,
			OperationListModules,
			OperationListReferences,
			OperationMaterializeView,
			OperationOpenDocument,
			OperationOrganizeWorkspace,
			OperationPlanExport,
			OperationPreviewFragment,
			OperationPreviewSourcePatch,
			OperationReadDeclarations,
			OperationReadModules,
			OperationReadReferences,
			OperationReadRows,
			OperationReadScope,
			OperationReplaceSourceTree,
		},
		limits: config.Limits,
		protocols: []protocolBinding{{
			version:      protocolNumber{major: 1, minor: 0},
			wireVersion:  protocolcommon.ProtocolVersion(ProtocolVersion),
			schemaDigest: protocolcommon.Digest(engineprotocol.SchemaDigest),
		}},
	}, nil
}

// NewCompilerDescriptor is the composition-root constructor for a canonical
// Engine facade. It keeps generated wire scalar conversions inside the
// endpoint boundary while preserving the fully explicit DescriptorConfig API
// for endpoint-native callers.
func NewCompilerDescriptor(compiler engine.Engine, releaseManifestDigest, endpointInstanceID string, transports []string, limits LimitPolicy) (*Descriptor, error) {
	described := compiler.Describe()
	return NewDescriptor(DescriptorConfig{
		EngineRelease:         protocolcommon.ReleaseVersion(described.ReleaseVersion),
		SourceRevision:        described.SourceRevision,
		ReleaseManifestDigest: protocolcommon.Digest(releaseManifestDigest),
		EndpointInstanceID:    protocolcommon.EndpointInstanceID(endpointInstanceID),
		Transports:            transports,
		Limits:                limits,
	})
}

// EngineRelease returns the endpoint release copied into every response.
func (d *Descriptor) EngineRelease() protocolcommon.ReleaseVersion {
	if d == nil {
		return ""
	}
	return d.engineRelease
}

// DispatchRelease returns the release that should be stamped on operation
// terminal responses. Negotiated context wins after handshake; descriptor
// release is the only trustworthy value before negotiation.
func DispatchRelease(negotiated *NegotiatedContext, descriptor *Descriptor) protocolcommon.ReleaseVersion {
	if negotiated != nil {
		return negotiated.EngineRelease()
	}
	if descriptor != nil {
		return descriptor.EngineRelease()
	}
	return ""
}

// SourceRevision returns internal build provenance. It is not handshake wire
// metadata and must not be exposed as a capability or diagnostic detail.
func (d *Descriptor) SourceRevision() string {
	if d == nil {
		return ""
	}
	return d.sourceRevision
}

// ReleaseManifestDigest returns the exact release-set identity.
func (d *Descriptor) ReleaseManifestDigest() protocolcommon.Digest {
	if d == nil {
		return ""
	}
	return d.releaseManifestDigest
}

// EndpointInstanceID returns the injected non-secret instance identity.
func (d *Descriptor) EndpointInstanceID() protocolcommon.EndpointInstanceID {
	if d == nil {
		return ""
	}
	return d.endpointInstanceID
}

// Transports returns a sorted defensive copy of configured transport IDs.
func (d *Descriptor) Transports() []string {
	if d == nil {
		return nil
	}
	return slices.Clone(d.transports)
}

// Operations returns the exact sorted enabled operation catalog.
func (d *Descriptor) Operations() []string {
	if d == nil {
		return nil
	}
	return slices.Clone(d.operations)
}

// Limits returns the immutable endpoint default and hard-limit policy by value.
func (d *Descriptor) Limits() LimitPolicy {
	if d == nil {
		return LimitPolicy{}
	}
	return d.limits
}

func validateTransports(input []string) ([]string, error) {
	if len(input) == 0 {
		return nil, fmt.Errorf("at least one endpoint transport is required")
	}
	if len(input) > maxEndpointTransports {
		return nil, fmt.Errorf("endpoint transport count exceeds %d", maxEndpointTransports)
	}
	result := slices.Clone(input)
	slices.Sort(result)
	for index, transport := range result {
		matched, err := regexp.MatchString(`^[a-z][a-z0-9_]*(?:[.-][a-z0-9_]+)*$`, transport)
		if err != nil || !matched || len(transport) > maxEndpointTransportIDLength {
			return nil, fmt.Errorf("transport %q is not a safe stable identifier", transport)
		}
		if index > 0 && result[index-1] == transport {
			return nil, fmt.Errorf("duplicate endpoint transport %q", transport)
		}
	}
	return result, nil
}
