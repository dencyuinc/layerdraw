// SPDX-License-Identifier: LicenseRef-LayerDraw-1.0

package endpoint

import (
	"fmt"

	"github.com/dencyuinc/layerdraw/gen/go/protocolcommon"
	"github.com/dencyuinc/layerdraw/internal/engine"
)

// CompilerEndpointConfig is the transport-neutral composition input for the
// negotiated endpoint and canonical compile dispatcher. Strings are accepted
// here so a composition root never needs to import generated wire packages
// alongside the Engine implementation.
type CompilerEndpointConfig struct {
	EngineRelease         string
	SourceRevision        string
	ReleaseManifestDigest string
	EndpointInstanceID    string
	Transports            []string
	Limits                LimitPolicy
}

// CompilerEndpoint contains the two immutable authorities a byte transport
// needs. Descriptor owns generated handshake policy; Dispatcher is the only
// generated-protocol-to-Engine mapping boundary.
type CompilerEndpoint struct {
	Descriptor *Descriptor
	Dispatcher *CompileDispatcher
}

// NewCompilerEndpoint composes one canonical in-memory Engine with its
// descriptor and dispatcher. It does not create transport or session state.
func NewCompilerEndpoint(config CompilerEndpointConfig) (*CompilerEndpoint, error) {
	compiler := engine.New(engine.BuildInfo{
		ReleaseVersion: config.EngineRelease,
		SourceRevision: config.SourceRevision,
	})
	descriptor, err := NewDescriptor(DescriptorConfig{
		EngineRelease:         protocolcommon.ReleaseVersion(config.EngineRelease),
		SourceRevision:        config.SourceRevision,
		ReleaseManifestDigest: protocolcommon.Digest(config.ReleaseManifestDigest),
		EndpointInstanceID:    protocolcommon.EndpointInstanceID(config.EndpointInstanceID),
		Transports:            config.Transports,
		Limits:                config.Limits,
	})
	if err != nil {
		return nil, fmt.Errorf("compose compiler endpoint: %w", err)
	}
	return &CompilerEndpoint{
		Descriptor: descriptor,
		Dispatcher: NewCompileDispatcher(compiler),
	}, nil
}
