// SPDX-License-Identifier: LicenseRef-LayerDraw-1.0

package endpoint

import (
	"context"

	"github.com/dencyuinc/layerdraw/gen/go/protocolcommon"
	"github.com/dencyuinc/layerdraw/gen/go/semantic"
	"github.com/dencyuinc/layerdraw/internal/engine"
)

type RegistryProjectArtifactInput struct {
	Bytes          []byte
	RegistrySource string
}

type RegistryProjectMutationInput struct {
	BaseEncoded        []byte
	Artifacts          []RegistryProjectArtifactInput
	RemoveCanonicalIDs []string
	ResourceLimits     LocalResourceLimits
}

type RegistryProjectPreparation struct {
	EncodedInput    []byte
	AuthoringImpact semantic.AuthoringImpact
	DefinitionHash  protocolcommon.Digest
	GraphHash       protocolcommon.Digest
}

// PrepareRegistryTemplate compares a validated no-head baseline with the exact
// staged .layerdraw container selected by Registry. The resulting closed input
// is published by Runtime as the first revision of the reserved Document.
func (e *LocalDocumentEngine) PrepareRegistryTemplate(ctx context.Context, baseEncoded, container []byte) (RegistryProjectPreparation, error) {
	base, err := DecodeLocalCompileInput(baseEncoded)
	if err != nil {
		return RegistryProjectPreparation{}, err
	}
	template, err := e.ReadContainer(ctx, container)
	if err != nil {
		return RegistryProjectPreparation{}, err
	}
	beforeResult, err := e.engine.Compile(ctx, base)
	if err != nil {
		return RegistryProjectPreparation{}, err
	}
	afterResult, err := e.engine.Compile(ctx, template.input)
	if err != nil {
		return RegistryProjectPreparation{}, err
	}
	_, _, planned, err := engine.BuildCanonicalAuthoringPlan(ctx, beforeResult.Snapshot(), afterResult.Snapshot(), base.ProjectSourceTree, template.input.ProjectSourceTree, engine.SemanticPlanLimits{MaxItems: 1 << 20, MaxOutputBytes: 64 << 20})
	if err != nil {
		return RegistryProjectPreparation{}, err
	}
	impact, err := generatedAuthoringImpact(planned)
	if err != nil {
		return RegistryProjectPreparation{}, err
	}
	encoded, err := EncodeLocalCompileInput(template.input)
	if err != nil {
		return RegistryProjectPreparation{}, err
	}
	return RegistryProjectPreparation{EncodedInput: encoded, AuthoringImpact: impact, DefinitionHash: template.DefinitionHash, GraphHash: template.GraphHash}, nil
}

// PrepareRegistryProject is the sole mapping boundary from an opaque local
// Engine snapshot plus verified Registry archives to a Runtime-ready source
// closure. Callers never receive or mutate CompileInput directly.
func (e *LocalDocumentEngine) PrepareRegistryProject(ctx context.Context, input RegistryProjectMutationInput) (RegistryProjectPreparation, error) {
	base, err := DecodeLocalCompileInput(input.BaseEncoded)
	if err != nil {
		return RegistryProjectPreparation{}, err
	}
	artifacts := make([]engine.RegistryProjectArtifactInput, len(input.Artifacts))
	for index, artifact := range input.Artifacts {
		artifacts[index] = engine.RegistryProjectArtifactInput{Bytes: append([]byte(nil), artifact.Bytes...), RegistrySource: artifact.RegistrySource}
	}
	prepared, err := e.engine.BuildRegistryProjectMutation(ctx, engine.RegistryProjectMutationInput{Base: base, Artifacts: artifacts, RemoveCanonicalIDs: append([]string(nil), input.RemoveCanonicalIDs...), ResourceLimits: input.ResourceLimits})
	if err != nil {
		return RegistryProjectPreparation{}, err
	}
	impact, err := generatedAuthoringImpact(prepared.AuthoringImpact)
	if err != nil {
		return RegistryProjectPreparation{}, err
	}
	encoded, err := EncodeLocalCompileInput(prepared.Input)
	if err != nil {
		return RegistryProjectPreparation{}, err
	}
	return RegistryProjectPreparation{EncodedInput: encoded, AuthoringImpact: impact, DefinitionHash: protocolcommon.Digest(prepared.Snapshot.DefinitionHash), GraphHash: protocolcommon.Digest(*prepared.Snapshot.GraphHash)}, nil
}
