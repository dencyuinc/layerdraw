// SPDX-License-Identifier: LicenseRef-LayerDraw-1.0

package registryengine

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"

	"github.com/dencyuinc/layerdraw/internal/engine"
	engineendpoint "github.com/dencyuinc/layerdraw/internal/engine/endpoint"
	"github.com/dencyuinc/layerdraw/internal/registry"
)

type ProjectSnapshotReader interface {
	ReadRegistryProjectSnapshot(context.Context, registry.RegistryProjectSnapshot) ([]byte, error)
}

type Adapter struct {
	engine    engine.Engine
	local     *engineendpoint.LocalDocumentEngine
	objects   registry.StagedObjectStore
	snapshots ProjectSnapshotReader
	limits    engine.LayerdrawLimits
}

func New(objects registry.StagedObjectStore, snapshots ProjectSnapshotReader) (*Adapter, error) {
	if objects == nil || snapshots == nil {
		return nil, errors.New("Registry Engine adapter requires staged objects and project snapshots")
	}
	return &Adapter{engine: engine.New(engine.BuildInfo{}), local: engineendpoint.NewLocalDocumentEngine(), objects: objects, snapshots: snapshots}, nil
}

func (a *Adapter) ValidateRegistryArtifact(ctx context.Context, release registry.ArtifactRelease, body []byte) (registry.ValidatedArtifact, error) {
	if release.Identity.Kind != registry.ArtifactPack {
		return registry.ValidatedArtifact{}, errors.New("template validation requires the initial project facade")
	}
	artifact, err := a.engine.ReadRegistryPack(ctx, body, a.limits)
	if err != nil || artifact.Manifest.ID != release.Identity.CanonicalID || artifact.Manifest.Version != release.Identity.Version {
		return registry.ValidatedArtifact{}, errors.New("Registry Pack identity does not match its archive")
	}
	ref, err := a.objects.PutRegistryObject(ctx, "application/vnd.layerdraw.pack", bodyReader(body), int64(len(body)))
	if err != nil {
		return registry.ValidatedArtifact{}, err
	}
	manifest := digestValue(struct {
		Manifest engine.RegistryPackManifest `json:"manifest"`
		Digests  map[string]string           `json:"digests"`
	}{artifact.Manifest, artifact.Digests})
	return registry.ValidatedArtifact{Identity: release.Identity, CanonicalDigest: release.Digest, StagedTreeManifest: manifest, ResolvedLockDigest: release.DependencyMetadataDigest, MutationDigest: digestValue(struct {
		Identity registry.ArtifactIdentity `json:"identity"`
		Object   registry.StagedObjectRef  `json:"object"`
	}{release.Identity, ref}), AuthoringImpactDigest: manifest, Diagnostics: []string{}, StagedObjects: []registry.StagedObjectRef{ref}}, nil
}

func (a *Adapter) BuildRegistryMutationPlan(ctx context.Context, input registry.RegistryMutationBuildInput) (registry.ProjectMutationPlan, error) {
	if input.Action == registry.ActionCreateFromTemplate {
		return registry.ProjectMutationPlan{}, errors.New("template mutation is not a Pack dependency mutation")
	}
	base, err := a.snapshots.ReadRegistryProjectSnapshot(ctx, input.Project.EngineSnapshot)
	if err != nil {
		return registry.ProjectMutationPlan{}, err
	}
	artifacts := make([]engineendpoint.RegistryProjectArtifactInput, 0, len(input.Artifacts))
	staged := make([]registry.StagedObjectRef, 0, len(input.Artifacts))
	for _, planned := range input.Artifacts {
		if planned.Release.Identity.Kind != registry.ArtifactPack || len(planned.Validation.StagedObjects) != 1 {
			return registry.ProjectMutationPlan{}, errors.New("Registry mutation artifact closure is invalid")
		}
		ref := planned.Validation.StagedObjects[0]
		reader, err := a.objects.OpenRegistryObject(ctx, ref)
		if err != nil {
			return registry.ProjectMutationPlan{}, err
		}
		const maxRegistryPackBytes int64 = 64 << 20
		body, readErr := io.ReadAll(io.LimitReader(reader, maxRegistryPackBytes+1))
		closeErr := reader.Close()
		if readErr != nil || closeErr != nil || int64(len(body)) > maxRegistryPackBytes {
			return registry.ProjectMutationPlan{}, errors.New("Registry staged Pack is unavailable")
		}
		artifacts = append(artifacts, engineendpoint.RegistryProjectArtifactInput{Bytes: body, RegistrySource: planned.Release.SourceID})
		staged = append(staged, ref)
	}
	removed := make([]string, len(input.ResolvedLockDelta.Removed))
	for index, artifact := range input.ResolvedLockDelta.Removed {
		removed[index] = artifact.Identity.CanonicalID
	}
	prepared, err := a.local.PrepareRegistryProject(ctx, engineendpoint.RegistryProjectMutationInput{BaseEncoded: base, Artifacts: artifacts, RemoveCanonicalIDs: removed})
	if err != nil {
		return registry.ProjectMutationPlan{}, err
	}
	mutation := digestValue(struct {
		Action     registry.Action            `json:"action"`
		Base       string                     `json:"base"`
		Definition string                     `json:"definition"`
		Delta      registry.ResolvedLockDelta `json:"delta"`
		Objects    []registry.StagedObjectRef `json:"objects"`
	}{input.Action, input.Project.Revision, string(prepared.DefinitionHash), input.ResolvedLockDelta, staged})
	return registry.ProjectMutationPlan{BaseProjectRevision: input.Project.Revision, ExpectedDefinitionHash: input.Project.DefinitionHash, ExpectedResolvedLockDigest: input.Project.DependencySnapshot.ResolvedLockDigest, StagedTreeManifest: digestValue(staged), ResolvedLockDelta: input.ResolvedLockDelta, SourceEdits: []registry.SourceEdit{}, TrustPolicyDigest: digestValue(input.Artifacts), MutationDigest: mutation, AuthoringImpact: prepared.AuthoringImpact, AuthoringImpactDigest: string(prepared.AuthoringImpact.ImpactDigest), StagedObjects: staged}, nil
}

type byteSliceReader struct{ data []byte }

func bodyReader(data []byte) io.Reader { return &byteSliceReader{data: append([]byte(nil), data...)} }
func (r *byteSliceReader) Read(p []byte) (int, error) {
	if len(r.data) == 0 {
		return 0, io.EOF
	}
	n := copy(p, r.data)
	r.data = r.data[n:]
	return n, nil
}

func digestValue(value any) string {
	data, _ := json.Marshal(value)
	sum := sha256.Sum256(data)
	return "sha256:" + hex.EncodeToString(sum[:])
}

var _ registry.PackageValidator = (*Adapter)(nil)
