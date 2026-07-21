// SPDX-License-Identifier: LicenseRef-LayerDraw-1.0

package endpoint

import (
	"context"
	"errors"

	"github.com/dencyuinc/layerdraw/gen/go/engineprotocol"
	"github.com/dencyuinc/layerdraw/gen/go/protocolcommon"
	"github.com/dencyuinc/layerdraw/gen/go/semantic"
	"github.com/dencyuinc/layerdraw/internal/engine"
)

// CompileProjectEditPreconditions compiles a closed local project and projects
// its authoritative semantic snapshot onto the generated edit protocol. This
// keeps the handwritten Engine/protocol mapping inside the endpoint boundary.
func CompileProjectEditPreconditions(ctx context.Context, input LocalProjectInput, generation engineprotocol.DocumentGeneration) (engineprotocol.EngineEditPreconditions, error) {
	return compileEditPreconditions(ctx, engine.CompileInput{
		Mode:                 engine.CompileProject,
		EntryPath:            input.EntryPath,
		ProjectSourceTree:    input.ProjectSourceTree,
		InstalledPackTree:    input.InstalledPackTree,
		ResolvedDependencies: input.ResolvedDependencies,
		ReferencedAssets:     input.ReferencedAssets,
		ResourceLimits:       input.ResourceLimits,
	}, generation)
}

// SourceEditPreconditions projects an already-validated LocalSource onto the
// generated edit-precondition protocol without exposing its CompileInput.
func SourceEditPreconditions(ctx context.Context, source LocalSource, generation engineprotocol.DocumentGeneration) (engineprotocol.EngineEditPreconditions, error) {
	return compileEditPreconditions(ctx, cloneCompileInput(source.input), generation)
}

func compileEditPreconditions(ctx context.Context, input engine.CompileInput, generation engineprotocol.DocumentGeneration) (engineprotocol.EngineEditPreconditions, error) {
	compiled, err := engine.New(engine.BuildInfo{}).Compile(ctx, input)
	if err != nil {
		return engineprotocol.EngineEditPreconditions{}, err
	}
	snapshot := compiled.Snapshot()
	if len(snapshot.Diagnostics) != 0 || snapshot.NormalizedDocument == nil {
		return engineprotocol.EngineEditPreconditions{}, errors.New("project is not valid for edit preconditions")
	}
	result := engineprotocol.EngineEditPreconditions{
		DocumentGeneration:    generation,
		ExpectedSubjectHashes: []engineprotocol.ExpectedHash{},
		ExpectedSubtreeHashes: []engineprotocol.ExpectedHash{},
		ExpectedChildSets:     []engineprotocol.ExpectedChildSet{},
	}
	for _, item := range snapshot.SubjectSemanticHashes {
		result.ExpectedSubjectHashes = append(result.ExpectedSubjectHashes, engineprotocol.ExpectedHash{Address: semantic.StableAddress(item.Address), Hash: protocolcommon.Digest(item.Hash)})
	}
	for _, item := range snapshot.SubtreeHashes {
		result.ExpectedSubtreeHashes = append(result.ExpectedSubtreeHashes, engineprotocol.ExpectedHash{Address: semantic.StableAddress(item.OwnerAddress), Hash: protocolcommon.Digest(item.Hash)})
	}
	for _, item := range snapshot.ChildSetHashes {
		result.ExpectedChildSets = append(result.ExpectedChildSets, engineprotocol.ExpectedChildSet{OwnerAddress: semantic.StableAddress(item.OwnerAddress), ChildKind: semantic.SubjectKind(item.ChildKind), Hash: protocolcommon.Digest(item.Hash)})
	}
	sources := make([]engineprotocol.ExpectedSourceDigest, 0, len(snapshot.SourceMap.Files))
	for _, file := range snapshot.SourceMap.Files {
		origin := semantic.SourceOrigin{Kind: semantic.OriginKind(file.Origin.Kind)}
		if file.Origin.PackAddress != "" {
			address := semantic.PackRootAddress(file.Origin.PackAddress)
			origin.PackAddress = &address
		}
		sources = append(sources, engineprotocol.ExpectedSourceDigest{Module: semantic.ModuleRef{Origin: origin, ModulePath: file.ModulePath}, Digest: protocolcommon.Digest(file.Digest)})
	}
	result.ExpectedSourceDigests = &sources
	return result, nil
}
