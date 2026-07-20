// SPDX-License-Identifier: LicenseRef-LayerDraw-1.0

package localdocument

import (
	"context"
	"errors"
	"io"
	"sort"
	"strconv"
	"sync"

	"github.com/dencyuinc/layerdraw/gen/go/protocolcommon"
	"github.com/dencyuinc/layerdraw/gen/go/runtimeprotocol"
	engineendpoint "github.com/dencyuinc/layerdraw/internal/engine/endpoint"
	"github.com/dencyuinc/layerdraw/internal/runtime/port"
)

type runtimeWorkbench struct {
	bridge            *engineendpoint.RuntimeEngineBridge
	engine            *engineendpoint.LocalDocumentEngine
	mu                sync.RWMutex
	kinds             map[runtimeprotocol.DocumentID]port.ExternalFileKind
	registryReader    port.RegistryStagedObjectReader
	registryBaselines map[string][]byte
}

const maxRegistryObjectBytes int64 = 64 << 20

func (w *runtimeWorkbench) BindExternal(documentID runtimeprotocol.DocumentID, kind port.ExternalFileKind) {
	w.mu.Lock()
	w.kinds[documentID] = kind
	w.mu.Unlock()
}

func (w *runtimeWorkbench) RegisterRegistryBaseline(handle string, encoded []byte) {
	w.mu.Lock()
	w.registryBaselines[handle] = append([]byte(nil), encoded...)
	w.mu.Unlock()
}

func (w *runtimeWorkbench) registryBase(handle string) ([]byte, bool) {
	if encoded, ok := w.bridge.SearchEncodedInput(handle); ok {
		return encoded, true
	}
	w.mu.RLock()
	encoded, ok := w.registryBaselines[handle]
	w.mu.RUnlock()
	return append([]byte(nil), encoded...), ok
}

func (w *runtimeWorkbench) PrepareRegistryRevision(ctx context.Context, input port.PrepareRegistryRevisionInput) (port.PreparedRevision, error) {
	return w.prepareRegistryRevision(ctx, input.BaseRevision, input.ProjectMutation, true)
}

func (w *runtimeWorkbench) PrepareInitialRegistryRevision(ctx context.Context, input port.PrepareInitialRegistryRevisionInput) (port.PreparedRevision, error) {
	return w.prepareRegistryRevision(ctx, input.BaselineRevision, input.ProjectMutation, false)
}

func (w *runtimeWorkbench) prepareRegistryRevision(ctx context.Context, base runtimeprotocol.CommittedRevisionRef, mutation port.RegistryProjectMutation, retain bool) (port.PreparedRevision, error) {
	encoded, ok := w.registryBase(mutation.SnapshotHandle)
	if !ok || engineendpoint.LocalCompileInputRef(encoded).Digest != mutation.SourceClosureDigest || w.registryReader == nil {
		return port.PreparedRevision{}, port.ErrConflict
	}
	artifacts := make([]engineendpoint.RegistryProjectArtifactInput, 0, len(mutation.Artifacts))
	artifactBytes := make([][]byte, 0, len(mutation.Artifacts))
	for _, artifact := range mutation.Artifacts {
		reader, err := w.registryReader.OpenRegistryStagedObject(ctx, artifact.Object)
		if err != nil {
			return port.PreparedRevision{}, err
		}
		contents, readErr := io.ReadAll(io.LimitReader(reader, maxRegistryObjectBytes+1))
		closeErr := reader.Close()
		if readErr != nil || closeErr != nil || int64(len(contents)) > maxRegistryObjectBytes {
			return port.PreparedRevision{}, port.ErrConflict
		}
		artifacts = append(artifacts, engineendpoint.RegistryProjectArtifactInput{Bytes: contents, RegistrySource: artifact.RegistrySource})
		artifactBytes = append(artifactBytes, contents)
	}
	var prepared engineendpoint.RegistryProjectPreparation
	var err error
	if !retain && len(mutation.Artifacts) == 1 && mutation.Artifacts[0].Object.MediaType == "application/vnd.layerdraw.project" && len(mutation.RemoveCanonicalIDs) == 0 {
		prepared, err = w.engine.PrepareRegistryTemplate(ctx, encoded, artifactBytes[0])
	} else {
		prepared, err = w.engine.PrepareRegistryProject(ctx, engineendpoint.RegistryProjectMutationInput{BaseEncoded: encoded, Artifacts: artifacts, RemoveCanonicalIDs: append([]string(nil), mutation.RemoveCanonicalIDs...)})
	}
	if err != nil {
		return port.PreparedRevision{}, err
	}
	ref := engineendpoint.LocalCompileInputRef(prepared.EncodedInput)
	result := port.PreparedRevision{AuthoringImpact: prepared.AuthoringImpact, DefinitionHash: prepared.DefinitionHash, GraphHash: prepared.GraphHash, Sources: port.SourceBlobSet{Revision: base, Blobs: []port.SourceBlob{{Ref: ref, Contents: prepared.EncodedInput}}}, Manifest: ref}
	if retain {
		working, ok := w.Working(mutation.SnapshotHandle, base)
		if !ok {
			return port.PreparedRevision{}, port.ErrConflict
		}
		bridgeWorking := engineendpoint.BridgeWorking{Handle: working.Handle, Generation: string(working.Generation), DocumentID: string(base.DocumentID), RevisionID: string(base.RevisionID), DefinitionHash: working.DefinitionHash, GraphHash: working.GraphHash}
		if err := w.bridge.RetainRegistryPrepared(ctx, bridgeWorking, engineendpoint.BridgePrepared{AuthoringImpact: prepared.AuthoringImpact, DefinitionHash: prepared.DefinitionHash, GraphHash: prepared.GraphHash, EncodedInput: prepared.EncodedInput}); err != nil {
			return port.PreparedRevision{}, err
		}
	}
	return result, nil
}

func (w *runtimeWorkbench) Open(ctx context.Context, in port.OpenWorkingDocumentInput) (port.WorkingDocument, error) {
	var encoded []byte
	for _, blob := range in.Sources.Blobs {
		if blob.Ref == in.Revision.Manifest && blob.Ref.BlobID == engineendpoint.LocalCompileInputBlobID {
			encoded = blob.Contents
			break
		}
	}
	if encoded == nil {
		return port.WorkingDocument{}, port.ErrNotFound
	}
	working, err := w.bridge.Open(ctx, string(in.Scope.DocumentID), string(in.Revision.Revision.RevisionID), in.Revision.Revision.DefinitionHash, in.Revision.Revision.GraphHash, encoded)
	if err != nil {
		return port.WorkingDocument{}, err
	}
	return workingFromBridge(working, in.Revision.Revision), nil
}

func (w *runtimeWorkbench) Preview(ctx context.Context, in port.PreviewWorkingDocumentInput) (port.PreparedRevision, error) {
	max, err := strconv.ParseInt(string(in.MaxOperations), 10, 64)
	if err != nil || max <= 0 {
		return port.PreparedRevision{}, port.ErrConflict
	}
	bridgeWorking := engineendpoint.BridgeWorking{Handle: in.Document.Handle, Generation: string(in.Document.Generation), DocumentID: string(in.Document.BaseRevision.DocumentID), RevisionID: string(in.Document.BaseRevision.RevisionID), DefinitionHash: in.Document.DefinitionHash, GraphHash: in.Document.GraphHash}
	prepared, err := w.bridge.Preview(ctx, bridgeWorking, in.Batch, in.Preconditions, max)
	if err != nil {
		return port.PreparedRevision{}, err
	}
	ref := engineendpoint.LocalCompileInputRef(prepared.EncodedInput)
	sources := port.SourceBlobSet{Revision: in.Document.BaseRevision, Blobs: []port.SourceBlob{{Ref: ref, Contents: prepared.EncodedInput}}}
	result := port.PreparedRevision{AuthoringImpact: prepared.AuthoringImpact, DefinitionHash: prepared.DefinitionHash, GraphHash: prepared.GraphHash, Preview: prepared.Preview, Sources: sources, Manifest: ref}
	w.mu.RLock()
	kind, fileBacked := w.kinds[in.Document.BaseRevision.DocumentID]
	w.mu.RUnlock()
	if fileBacked {
		source, err := w.engine.ReadEncodedInput(ctx, prepared.EncodedInput)
		if err != nil {
			return port.PreparedRevision{}, err
		}
		switch kind {
		case port.ExternalFileKindProject:
			tree := source.ProjectSourceTree()
			paths := make([]string, 0, len(tree))
			for path := range tree {
				paths = append(paths, path)
			}
			sort.Strings(paths)
			files := make([]port.ExternalProjectFile, 0, len(paths))
			for _, path := range paths {
				files = append(files, port.ExternalProjectFile{Path: path, Contents: tree[path]})
			}
			result.External = &port.ExternalMaterialization{Kind: kind, ProjectFiles: files}
		case port.ExternalFileKindContainer:
			container, err := w.engine.WriteContainer(ctx, source)
			if err != nil {
				return port.PreparedRevision{}, err
			}
			result.External = &port.ExternalMaterialization{Kind: kind, Container: container}
		default:
			return port.PreparedRevision{}, port.ErrConflict
		}
	}
	return result, nil
}

func (w *runtimeWorkbench) Checkpoint(ctx context.Context, in port.CheckpointWorkingDocumentInput) (port.WorkingDocument, error) {
	bridgeWorking := engineendpoint.BridgeWorking{Handle: in.Document.Handle, Generation: string(in.Document.Generation), DocumentID: string(in.Document.BaseRevision.DocumentID), RevisionID: string(in.Document.BaseRevision.RevisionID), DefinitionHash: in.Document.DefinitionHash, GraphHash: in.Document.GraphHash}
	var encoded []byte
	for _, blob := range in.Prepared.Sources.Blobs {
		if blob.Ref == in.Prepared.Manifest {
			encoded = blob.Contents
			break
		}
	}
	if encoded == nil {
		return port.WorkingDocument{}, errors.New("prepared compile input unavailable")
	}
	prepared := engineendpoint.BridgePrepared{AuthoringImpact: in.Prepared.AuthoringImpact, DefinitionHash: in.Prepared.DefinitionHash, GraphHash: in.Prepared.GraphHash, EncodedInput: encoded}
	working, err := w.bridge.Checkpoint(ctx, bridgeWorking, prepared, string(in.Revision.RevisionID))
	if err != nil {
		return port.WorkingDocument{}, err
	}
	return workingFromBridge(working, in.Revision), nil
}

func (w *runtimeWorkbench) Close(_ context.Context, in port.WorkingDocument) error {
	return w.bridge.Close(engineendpoint.BridgeWorking{Handle: in.Handle, Generation: string(in.Generation), DocumentID: string(in.BaseRevision.DocumentID), RevisionID: string(in.BaseRevision.RevisionID), DefinitionHash: in.DefinitionHash, GraphHash: in.GraphHash})
}

func (w *runtimeWorkbench) Working(handle string, revision runtimeprotocol.CommittedRevisionRef) (port.WorkingDocument, bool) {
	value, ok := w.bridge.Working(handle)
	if !ok {
		return port.WorkingDocument{}, false
	}
	return workingFromBridge(value, revision), true
}

func (w *runtimeWorkbench) SourceDigest(handle string) (protocolcommon.Digest, bool) {
	return w.bridge.SourceDigest(handle)
}

func (w *runtimeWorkbench) SearchEncodedInput(handle string) ([]byte, bool) {
	return w.bridge.SearchEncodedInput(handle)
}

func (w *runtimeWorkbench) Opened(revision runtimeprotocol.CommittedRevisionRef) (port.WorkingDocument, bool) {
	value, ok := w.bridge.Opened(string(revision.DocumentID), string(revision.RevisionID))
	if !ok {
		return port.WorkingDocument{}, false
	}
	return workingFromBridge(value, revision), true
}

func workingFromBridge(value engineendpoint.BridgeWorking, revision runtimeprotocol.CommittedRevisionRef) port.WorkingDocument {
	return port.WorkingDocument{Handle: value.Handle, Generation: protocolcommon.CanonicalNonNegativeInt64(value.Generation), BaseRevision: revision, DefinitionHash: value.DefinitionHash, GraphHash: value.GraphHash}
}
