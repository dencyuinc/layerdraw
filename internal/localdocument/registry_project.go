// SPDX-License-Identifier: LicenseRef-LayerDraw-1.0

package localdocument

import (
	"context"
	"errors"

	"github.com/dencyuinc/layerdraw/gen/go/protocolcommon"
	"github.com/dencyuinc/layerdraw/gen/go/runtimeprotocol"
	"github.com/dencyuinc/layerdraw/internal/engine/endpoint"
	"github.com/dencyuinc/layerdraw/internal/registry"
	"github.com/dencyuinc/layerdraw/internal/runtime/port"
)

type registryProjectMetadata struct {
	DependencySnapshot registry.ProjectDependencySnapshot `json:"dependency_snapshot"`
	PackTreeManifest   string                             `json:"pack_tree_manifest"`
}

func (h *Host) ReadRegistryProjectSnapshot(_ context.Context, snapshot registry.RegistryProjectSnapshot) ([]byte, error) {
	encoded, ok := h.workbench.registryBase(snapshot.Handle)
	if !ok || endpoint.LocalCompileInputRef(encoded).Digest != protocolcommon.Digest(snapshot.SourceClosureDigest) {
		return nil, errors.New("Registry Engine snapshot is stale")
	}
	return encoded, nil
}

func (h *Host) CurrentRegistryProjectState(ctx context.Context, projectID string) (registry.ProjectState, error) {
	h.mu.Lock()
	var session *Session
	for _, candidate := range h.sessions {
		if !candidate.closed && candidate.PortableID == projectID {
			session = candidate
			break
		}
	}
	if session == nil {
		h.mu.Unlock()
		return registry.ProjectState{}, port.ErrNotFound
	}
	open := session.Open
	working := session.working
	metadata := h.metadata.RegistryProjects[string(open.Session.Scope.DocumentID)]
	h.mu.Unlock()
	digest, ok := h.workbench.SourceDigest(working.Handle)
	if !ok {
		return registry.ProjectState{}, errors.New("Registry project source snapshot is unavailable")
	}
	grant, _, err := h.authority.ResolveGrant(ctx, open.Session.Scope)
	if err != nil {
		return registry.ProjectState{}, err
	}
	return registry.ProjectState{ProjectID: projectID, DocumentID: string(open.Session.Scope.DocumentID), LocalScopeID: open.Session.Scope.LocalScopeID, OrganizationScopeID: open.Session.Scope.OrganizationScopeID, Revision: string(open.CommittedRevision.RevisionID), DefinitionHash: string(open.CommittedRevision.DefinitionHash), DependencySnapshot: metadata.DependencySnapshot, PackTreeManifest: metadata.PackTreeManifest, HostCapabilities: []string{}, GrantSnapshot: grant, RuntimeSessionID: string(open.Session.RuntimeSessionID), EngineSnapshot: registry.RegistryProjectSnapshot{Kind: registry.RegistryProjectSnapshotWorking, Handle: working.Handle, DocumentID: string(open.Session.Scope.DocumentID), Revision: string(open.CommittedRevision.RevisionID), DefinitionHash: string(open.CommittedRevision.DefinitionHash), GraphHash: string(open.CommittedRevision.GraphHash), SourceClosureDigest: string(digest)}}, nil
}

func (h *Host) NewRegistryDocumentState(ctx context.Context, identity registry.ArtifactIdentity) (registry.ProjectState, error) {
	reservation := string(digestJSON(identity))
	id := reservation[len("sha256:") : len("sha256:")+32]
	documentID := runtimeprotocol.DocumentID("registry-template-" + id)
	scope := h.authority.add(documentID)
	source, err := h.engine.CompileProject(ctx, endpoint.LocalProjectInput{EntryPath: "document.ldl", ProjectSourceTree: map[string][]byte{"document.ldl": []byte("project template \"Untitled\" {}\n")}, ResolvedDependencies: endpoint.LocalResolvedDependencies{Format: "layerdraw-resolved", FormatVersion: 1, Language: 1}})
	if err != nil {
		return registry.ProjectState{}, err
	}
	encoded, ref, err := source.EncodedInput()
	if err != nil {
		return registry.ProjectState{}, err
	}
	handle := "registry-template-baseline-" + id
	h.workbench.RegisterRegistryBaseline(handle, encoded)
	grant, _, err := h.authority.ResolveGrant(ctx, scope)
	if err != nil {
		return registry.ProjectState{}, err
	}
	projectID := source.PortableID
	return registry.ProjectState{ProjectID: projectID, DocumentID: string(documentID), LocalScopeID: scope.LocalScopeID, Revision: "", DefinitionHash: string(source.DefinitionHash), DependencySnapshot: registry.ProjectDependencySnapshot{ResolvedLockDigest: registryDigestEmptyLock(), Installs: []registry.LockedArtifact{}}, PackTreeManifest: string(ref.Digest), HostCapabilities: []string{}, GrantSnapshot: grant, EngineSnapshot: registry.RegistryProjectSnapshot{Kind: registry.RegistryProjectSnapshotEmptyTemplate, Handle: handle, DocumentID: string(documentID), DefinitionHash: string(source.DefinitionHash), GraphHash: string(source.GraphHash), SourceClosureDigest: string(ref.Digest)}}, nil
}

func (h *Host) ApplyRegistryProjectState(documentID runtimeprotocol.DocumentID, snapshot registry.ProjectDependencySnapshot, manifest string) error {
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.closed {
		return errors.New("local document host is closed")
	}
	h.metadata.RegistryProjects[string(documentID)] = registryProjectMetadata{DependencySnapshot: snapshot, PackTreeManifest: manifest}
	return h.saveMetadataLocked()
}

func registryDigestEmptyLock() string {
	return string(digestJSON(struct {
		Installs []string `json:"installs"`
	}{Installs: []string{}}))
}

var _ registry.ProjectStatePort = (*Host)(nil)
var _ registry.TemplateDocumentPort = (*Host)(nil)
var _ interface {
	ReadRegistryProjectSnapshot(context.Context, registry.RegistryProjectSnapshot) ([]byte, error)
} = (*Host)(nil)
