// SPDX-License-Identifier: LicenseRef-LayerDraw-1.0

package localdocument

import (
	"context"
	"errors"
	"sync"
	"time"

	"github.com/dencyuinc/layerdraw/gen/go/protocolcommon"
	"github.com/dencyuinc/layerdraw/gen/go/runtimeprotocol"
	"github.com/dencyuinc/layerdraw/gen/go/semantic"
	"github.com/dencyuinc/layerdraw/internal/adapter/local"
	"github.com/dencyuinc/layerdraw/internal/runtime/port"
)

type initialRegistryPublisher struct {
	mu        sync.Mutex
	documents *local.Document
	state     *local.State
	clock     Clock
}

func (p *initialRegistryPublisher) PublishInitialRegistryRevision(ctx context.Context, input port.PublishInitialRegistryRevisionInput) (runtimeprotocol.CommittedRevisionRef, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	provider := runtimeprotocol.ProviderVersionToken("registry-initial-" + string(input.OperationID))
	revision := runtimeprotocol.CommittedRevisionRef{DocumentID: input.Scope.DocumentID, RevisionID: runtimeprotocol.RevisionID("registry-" + string(input.OperationID)), DefinitionHash: input.Prepared.DefinitionHash, GraphHash: input.Prepared.GraphHash, ProviderVersion: &provider}
	if head, err := p.documents.GetHead(ctx, port.GetDocumentHeadInput{Scope: input.Scope}); err == nil {
		if !sameCommittedRevision(head.Revision, revision) {
			return runtimeprotocol.CommittedRevisionRef{}, port.ErrConflict
		}
		if err := p.ensureInitialState(ctx, input.Scope, revision); err != nil {
			return runtimeprotocol.CommittedRevisionRef{}, err
		}
		return head.Revision, nil
	} else if !errors.Is(err, port.ErrNotFound) {
		return runtimeprotocol.CommittedRevisionRef{}, err
	}
	refs := make([]protocolcommon.BlobRef, len(input.Prepared.Sources.Blobs))
	for index, blob := range input.Prepared.Sources.Blobs {
		refs[index] = blob.Ref
	}
	snapshot := port.RevisionSnapshot{Revision: revision, SourceBlobs: refs, Manifest: input.Prepared.Manifest}
	if err := p.documents.InitializeDocument(ctx, input.Scope, snapshot, provider, "0", input.Prepared.Sources.Blobs); err != nil {
		return runtimeprotocol.CommittedRevisionRef{}, err
	}
	if err := p.ensureInitialState(ctx, input.Scope, revision); err != nil {
		return runtimeprotocol.CommittedRevisionRef{}, port.ErrIndeterminate
	}
	return revision, nil
}

func (p *initialRegistryPublisher) ensureInitialState(ctx context.Context, scope runtimeprotocol.RuntimeScope, revision runtimeprotocol.CommittedRevisionRef) error {
	if head, err := p.state.GetHead(ctx, port.GetStateHeadInput{Scope: scope}); err == nil {
		if head.DefinitionHash != revision.DefinitionHash || head.GraphHash != revision.GraphHash {
			return port.ErrConflict
		}
		return nil
	} else if !errors.Is(err, port.ErrNotFound) {
		return err
	}
	stateRef := protocolcommon.BlobRef{BlobID: "local-empty-state", Digest: digestJSON(struct{}{}), Lifetime: protocolcommon.BlobLifetimePersistent, MediaType: "application/json", Size: "2"}
	head := port.StateHead{StateVersion: "0", BackendVersion: "1", DefinitionHash: revision.DefinitionHash, GraphHash: revision.GraphHash, CapturedAt: protocolcommon.Rfc3339Time(p.clock.Now().UTC().Format(time.RFC3339Nano)), SubjectHashes: map[semantic.StableAddress]protocolcommon.Digest{}}
	return p.state.InitializeState(ctx, scope, port.StateSnapshot{Head: head, Contents: stateRef, InaccessibleFieldPaths: []semantic.StateFieldPath{}, Records: []port.StateRecord{}})
}

var _ port.InitialRegistryRevisionPublisher = (*initialRegistryPublisher)(nil)
