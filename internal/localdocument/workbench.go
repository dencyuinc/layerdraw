// SPDX-License-Identifier: LicenseRef-LayerDraw-1.0

package localdocument

import (
	"context"
	"errors"
	"strconv"

	"github.com/dencyuinc/layerdraw/gen/go/protocolcommon"
	"github.com/dencyuinc/layerdraw/gen/go/runtimeprotocol"
	engineendpoint "github.com/dencyuinc/layerdraw/internal/engine/endpoint"
	"github.com/dencyuinc/layerdraw/internal/runtime/port"
)

type runtimeWorkbench struct {
	bridge *engineendpoint.RuntimeEngineBridge
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
	return port.PreparedRevision{AuthoringImpact: prepared.AuthoringImpact, DefinitionHash: prepared.DefinitionHash, GraphHash: prepared.GraphHash, Sources: sources, Manifest: ref}, nil
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
