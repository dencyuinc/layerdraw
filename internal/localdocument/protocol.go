// SPDX-License-Identifier: LicenseRef-LayerDraw-1.0

package localdocument

import (
	"bytes"
	"context"
	"errors"
	"strconv"

	"github.com/dencyuinc/layerdraw/gen/go/accessprotocol"
	"github.com/dencyuinc/layerdraw/gen/go/engineprotocol"
	"github.com/dencyuinc/layerdraw/gen/go/protocolcommon"
	"github.com/dencyuinc/layerdraw/gen/go/runtimeprotocol"
	"github.com/dencyuinc/layerdraw/gen/go/semantic"
	accesscore "github.com/dencyuinc/layerdraw/internal/access"
	runtimehost "github.com/dencyuinc/layerdraw/internal/runtime"
	"github.com/dencyuinc/layerdraw/internal/runtime/port"
)

type localStateBinding struct{ backend port.StateBackend }

func (b localStateBinding) ResolveStateBackend(_ context.Context, input port.ResolveStateBackendInput) (port.StateBackend, error) {
	if input.Binding.Kind != port.BackendBindingLocal || input.Binding.BindingID != "local" || b.backend == nil {
		return nil, port.ErrNotFound
	}
	return b.backend, nil
}

func (h *Host) Negotiate(request runtimeprotocol.RuntimeHandshakeRequest) (runtimeprotocol.RuntimeHandshakeResult, error) {
	result, rejection := h.runtime.Negotiate(request)
	if rejection != nil {
		return runtimeprotocol.RuntimeHandshakeResult{}, rejection
	}
	return result, nil
}

// SessionFor resolves an exact issued Runtime session without trusting a
// caller-provided handle as local process state.
func (h *Host) SessionFor(ref runtimeprotocol.RuntimeSessionRef) (*Session, error) {
	h.mu.Lock()
	defer h.mu.Unlock()
	session := h.sessions[ref.RuntimeSessionID]
	if session == nil || session.closed || session.Open.Session != ref {
		return nil, errors.New("runtime session is closed or unknown")
	}
	return session, nil
}

func (h *Host) Inspect(ref runtimeprotocol.RuntimeSessionRef) (runtimeprotocol.RuntimeInspectionResult, error) {
	session, err := h.SessionFor(ref)
	if err != nil {
		return runtimeprotocol.RuntimeInspectionResult{}, err
	}
	ctx := h.accessContext(context.Background(), session)
	if err := h.authority.AuthorizeRead(ctx, ref.Scope, accesscore.SurfaceReview); err != nil {
		return runtimeprotocol.RuntimeInspectionResult{}, err
	}
	return runtimeprotocol.RuntimeInspectionResult{
		Session: session.Open.Session, CommittedRevision: session.Open.CommittedRevision,
		WorkingDocument: session.Open.WorkingDocument, StateInput: session.Open.StateInput,
		CapabilityManifest: session.Open.CapabilityManifest,
	}, nil
}

// Preview maps the host preview directly to the existing Engine Workbench and
// local authorization authority. It creates no recovery record and publishes
// no definition, state, asset, history, or filesystem bytes.
func (h *Host) Preview(ctx context.Context, input runtimeprotocol.PreviewOperationsInput) (runtimeprotocol.PreviewOperationsResult, error) {
	session, err := h.SessionFor(input.Session)
	if err != nil {
		return runtimeprotocol.PreviewOperationsResult{}, err
	}
	ctx = h.accessContext(ctx, session)
	current := session.Open.CommittedRevision
	if input.OperationBatch.DocumentID != current.DocumentID || !sameCommittedRevision(input.OperationBatch.BaseRevision, current) || input.OperationBatch.ExpectedDefinitionHash != current.DefinitionHash {
		return runtimeprotocol.PreviewOperationsResult{}, port.ErrConflict
	}
	preconditions := input.OperationBatch.Preconditions
	preconditions.DocumentGeneration = h.documentGeneration(session)
	prepared, err := h.workbench.Preview(ctx, port.PreviewWorkingDocumentInput{Document: session.working, Batch: input.OperationBatch.Operations, Preconditions: preconditions, MaxOperations: "4096"})
	if err != nil {
		return runtimeprotocol.PreviewOperationsResult{}, err
	}
	grant, _, err := h.authority.ResolveGrant(ctx, input.Session.Scope)
	if err != nil {
		return runtimeprotocol.PreviewOperationsResult{}, err
	}
	evaluation := accessprotocol.EvaluateAuthoringInput{AuthoringImpact: &prepared.AuthoringImpact, GrantSnapshot: grant, HostOperationImpacts: []accessprotocol.HostOperationImpact{}, RequestIntent: "propose"}
	decision, rejection := h.runtime.Authorize(ctx, runtimehost.AuthorizationRequest{Scope: input.Session.Scope, CurrentRevision: current, Evaluation: evaluation})
	if rejection != nil {
		return runtimeprotocol.PreviewOperationsResult{}, rejection
	}
	proof := runtimeprotocol.AuthoringProof{AccessFingerprint: grant.AccessFingerprint, BaseRevision: current, DecisionDigest: decision.DecisionDigest, EvaluationDigest: decision.EvaluationDigest, MembershipVersion: grant.MembershipVersion, PolicyRefs: grant.PolicyRefs}
	return runtimeprotocol.PreviewOperationsResult{PreviewEvaluation: runtimeprotocol.PreviewEvaluation{AuthoringImpact: prepared.AuthoringImpact, AuthoringDecision: decision}, AuthoringProof: proof, DefinitionHash: prepared.DefinitionHash, GraphHash: prepared.GraphHash}, nil
}

func sameCommittedRevision(left, right runtimeprotocol.CommittedRevisionRef) bool {
	if left.DefinitionHash != right.DefinitionHash || left.DocumentID != right.DocumentID || left.GraphHash != right.GraphHash || left.RevisionID != right.RevisionID {
		return false
	}
	if left.ProviderVersion == nil || right.ProviderVersion == nil {
		return left.ProviderVersion == nil && right.ProviderVersion == nil
	}
	return *left.ProviderVersion == *right.ProviderVersion
}

func (h *Host) documentGeneration(session *Session) engineprotocol.DocumentGeneration {
	return engineprotocol.DocumentGeneration{DocumentHandle: engineprotocol.DocumentHandle{EndpointInstanceID: h.config.EndpointInstanceID, Value: session.working.Handle}, Value: protocolcommon.CanonicalUint64(session.working.Generation)}
}

// Commit delegates an already previewed generated Runtime input to Runtime.
func (h *Host) Commit(ctx context.Context, input runtimeprotocol.RuntimeCommitInput) (runtimeprotocol.RuntimeCommitResult, error) {
	session, err := h.SessionFor(input.Session)
	if err != nil {
		return runtimeprotocol.RuntimeCommitResult{}, err
	}
	ctx = h.accessContext(ctx, session)
	if change, detectErr := h.detectExternalChange(ctx, session); detectErr != nil {
		return runtimeprotocol.RuntimeCommitResult{}, detectErr
	} else if change != nil {
		return runtimeprotocol.RuntimeCommitResult{}, port.ErrConflict
	}
	input.OperationBatch.Preconditions.DocumentGeneration = h.documentGeneration(session)
	result, rejection := h.runtime.CommitOperations(ctx, input)
	if rejection != nil {
		return runtimeprotocol.RuntimeCommitResult{}, rejection
	}
	if err := h.applyCommit(session, result); err != nil {
		return result, err
	}
	return result, nil
}

func saveInput(session *Session, input runtimeprotocol.RuntimeCommitInput, trigger runtimeprotocol.CommitTrigger) SaveInput {
	return SaveInput{Session: session, Operations: input.OperationBatch.Operations, Preconditions: input.OperationBatch.Preconditions, OperationID: input.OperationID, IdempotencyKey: input.IdempotencyKey, Trigger: trigger, Cancellation: input.CancellationToken}
}

func (h *Host) SaveRuntime(ctx context.Context, input runtimeprotocol.RuntimeCommitInput) (runtimeprotocol.RuntimeCommitResult, error) {
	session, err := h.SessionFor(input.Session)
	if err != nil {
		return runtimeprotocol.RuntimeCommitResult{}, err
	}
	current := session.Open.CommittedRevision
	if input.OperationBatch.DocumentID != current.DocumentID || !sameCommittedRevision(input.OperationBatch.BaseRevision, current) || input.OperationBatch.ExpectedDefinitionHash != current.DefinitionHash {
		return runtimeprotocol.RuntimeCommitResult{}, port.ErrConflict
	}
	return h.Save(ctx, saveInput(session, input, runtimeprotocol.CommitTriggerExplicitSave))
}

func (h *Host) applyCommit(session *Session, result runtimeprotocol.RuntimeCommitResult) error {
	if result.OperationResult.CommittedRevision != nil {
		revision := *result.OperationResult.CommittedRevision
		session.Open.CommittedRevision = revision
		session.Open.WorkingDocument.BaseRevision = revision
		if working, ok := h.workbench.Working(session.working.Handle, revision); ok {
			session.working = working
			session.Open.WorkingDocument.WorkingGeneration = runtimeprotocol.WorkingGeneration(working.Generation)
		}
	}
	if external := result.OperationResult.ExternalMaterialization; external != nil && external.State == runtimeprotocol.ExternalMaterializationStatePublished {
		digest, ok := h.workbench.SourceDigest(session.working.Handle)
		if !ok {
			return errors.New("published external source baseline is unavailable")
		}
		// The journal result and external receipt are already durable. Baseline
		// metadata is a conservative change-detection cache: a write failure may
		// cause a later reopen to require review, but must never change the
		// published terminal result.
		_ = h.acceptSessionSourceBaseline(session, digest)
	}
	return nil
}

func (h *Host) ControlAutosave(ctx context.Context, input runtimeprotocol.AutosaveControlInput) (runtimeprotocol.AutosaveControlResult, error) {
	return h.ControlAutosaveWithResult(ctx, input, nil)
}

// ControlAutosaveWithResult exposes the terminal autosave result to trusted
// Desktop composition while preserving the generated protocol response shape.
func (h *Host) ControlAutosaveWithResult(ctx context.Context, input runtimeprotocol.AutosaveControlInput, result chan<- AutosaveResult) (runtimeprotocol.AutosaveControlResult, error) {
	if input.Action == runtimeprotocol.AutosaveActionCancel {
		terminal, found, err := h.cancelAutosave(input.Session)
		if err != nil {
			return runtimeprotocol.AutosaveControlResult{}, err
		}
		if result != nil && found {
			select {
			case result <- terminal:
			default:
			}
		}
		return runtimeprotocol.AutosaveControlResult{Action: input.Action, Scheduled: false}, nil
	}
	if input.Commit == nil {
		return runtimeprotocol.AutosaveControlResult{}, errors.New("autosave commit is required")
	}
	if input.Commit.Session != input.Session {
		return runtimeprotocol.AutosaveControlResult{}, errors.New("autosave session mismatch")
	}
	session, err := h.SessionFor(input.Session)
	if err != nil {
		return runtimeprotocol.AutosaveControlResult{}, err
	}
	if err := h.scheduleAutosave(ctx, saveInput(session, *input.Commit, runtimeprotocol.CommitTriggerAutosave), result, result != nil); err != nil {
		return runtimeprotocol.AutosaveControlResult{}, err
	}
	return runtimeprotocol.AutosaveControlResult{Action: input.Action, Scheduled: true}, nil
}

func (h *Host) StateSnapshot(ctx context.Context, ref runtimeprotocol.RuntimeSessionRef) (runtimeprotocol.StateSnapshotResult, error) {
	session, err := h.SessionFor(ref)
	if err != nil {
		return runtimeprotocol.StateSnapshotResult{}, err
	}
	ctx = h.accessContext(ctx, session)
	if err := h.authority.AuthorizeRead(ctx, ref.Scope, accesscore.SurfaceQuery); err != nil {
		return runtimeprotocol.StateSnapshotResult{}, err
	}
	revision, err := h.documents.ReadRevision(ctx, port.ReadRevisionInput{Scope: ref.Scope, RevisionID: session.Open.CommittedRevision.RevisionID})
	if err != nil {
		return runtimeprotocol.StateSnapshotResult{}, err
	}
	blobs, err := h.documents.ReadSourceBlobs(ctx, port.ReadSourceBlobsInput{Scope: ref.Scope, Revision: revision.Revision, Blobs: revision.SourceBlobs})
	if err != nil {
		return runtimeprotocol.StateSnapshotResult{}, err
	}
	var encoded []byte
	for _, blob := range blobs.Blobs {
		if blob.Ref == revision.Manifest {
			encoded = blob.Contents
			break
		}
	}
	if encoded == nil {
		return runtimeprotocol.StateSnapshotResult{}, errors.New("current source manifest is unavailable")
	}
	source, err := h.engine.ReadEncodedInput(ctx, encoded)
	if err != nil {
		return runtimeprotocol.StateSnapshotResult{}, err
	}
	built, err := h.runtime.BuildStateQueryInput(ctx, runtimehost.BuildStateQueryInput{
		Scope: ref.Scope, Binding: port.BackendBinding{Kind: port.BackendBindingLocal, BindingID: "local"}, Policy: runtimehost.StateInputPolicyRequired,
		Definition: runtimehost.StateQueryDefinition{ProjectAddress: semantic.ProjectRootAddress(source.PortableID), DefinitionHash: source.DefinitionHash, GraphHash: source.GraphHash, SubjectHashes: source.SubjectHashes(), AddressMoves: []runtimehost.StateAddressMove{}},
	})
	if err != nil {
		return runtimeprotocol.StateSnapshotResult{}, err
	}
	return runtimeprotocol.StateSnapshotResult{StateInput: built.StateInput}, nil
}

func (h *Host) PreviewRestore(ctx context.Context, input runtimeprotocol.RestorePreviewInput) (runtimeprotocol.RestorePreviewResult, error) {
	session, err := h.SessionFor(input.Session)
	if err != nil {
		return runtimeprotocol.RestorePreviewResult{}, err
	}
	ctx = h.accessContext(ctx, session)
	if err := h.authority.AuthorizeRead(ctx, input.Session.Scope, accesscore.SurfaceReview); err != nil {
		return runtimeprotocol.RestorePreviewResult{}, err
	}
	revision, err := h.history.GetRevision(ctx, port.GetRevisionMetadataInput{Scope: session.Open.Session.Scope, RevisionID: input.RevisionID})
	if err != nil {
		return runtimeprotocol.RestorePreviewResult{}, err
	}
	return runtimeprotocol.RestorePreviewResult{Revision: revision, RequiresCommit: true}, nil
}

func (h *Host) StageAsset(ctx context.Context, input runtimeprotocol.StageAssetInput, contents []byte) (runtimeprotocol.StageAssetResult, error) {
	session, err := h.SessionFor(input.Session)
	if err != nil {
		return runtimeprotocol.StageAssetResult{}, err
	}
	ctx = h.accessContext(ctx, session)
	ref := input.ContentBlob
	if ref.Lifetime != protocolcommon.BlobLifetimeRequest || ref.Size != protocolcommon.CanonicalUint64(strconv.Itoa(len(contents))) {
		return runtimeprotocol.StageAssetResult{}, port.ErrConflict
	}
	releasePublication, err := h.authority.AcquireAuthoringPublication(ctx, session.Open.Session.Scope)
	if err != nil || releasePublication == nil {
		return runtimeprotocol.StageAssetResult{}, accesscore.ErrGrantStale
	}
	defer releasePublication()
	grant, _, err := h.authority.ResolveGrant(ctx, session.Open.Session.Scope)
	if err != nil {
		return runtimeprotocol.StageAssetResult{}, err
	}
	impact, err := accesscore.HostOperationImpact(accessprotocol.HostOperationKindAssetStage, "stage", accessprotocol.HostResourceScope{DocumentID: string(session.Open.Session.Scope.DocumentID), LocalScopeID: session.Open.Session.Scope.LocalScopeID, OrganizationScopeID: session.Open.Session.Scope.OrganizationScopeID}, []string{ref.BlobID})
	if err != nil {
		return runtimeprotocol.StageAssetResult{}, err
	}
	decision, rejection := h.runtime.Authorize(ctx, runtimehost.AuthorizationRequest{Scope: session.Open.Session.Scope, CurrentRevision: session.Open.CommittedRevision, Evaluation: accessprotocol.EvaluateAuthoringInput{GrantSnapshot: grant, HostOperationImpacts: []accessprotocol.HostOperationImpact{impact}, RequestIntent: "publish"}})
	if rejection != nil || decision.Outcome != accessprotocol.AuthoringDecisionOutcomeAllow {
		if rejection != nil {
			return runtimeprotocol.StageAssetResult{}, rejection
		}
		return runtimeprotocol.StageAssetResult{}, port.ErrConflict
	}
	metadata, err := h.assets.PutIfAbsent(ctx, port.PutAssetInput{Scope: session.Open.Session.Scope, ExpectedDigest: ref.Digest, MediaType: ref.MediaType, Size: protocolcommon.CanonicalUint64(ref.Size), Contents: bytes.NewReader(contents)})
	if err != nil {
		return runtimeprotocol.StageAssetResult{}, err
	}
	persistent := protocolcommon.BlobRef{BlobID: ref.BlobID, Digest: metadata.Digest, Lifetime: protocolcommon.BlobLifetimePersistent, MediaType: metadata.MediaType, Size: metadata.Size}
	return runtimeprotocol.StageAssetResult{Asset: runtimeprotocol.RuntimeBlobRef{Blob: persistent, Scope: session.Open.Session.Scope, SessionGeneration: session.Open.Session.SessionGeneration}}, nil
}

// AuthorizeHostOperation re-evaluates one current host-owned publication
// immediately before its side effect. The adapter never receives the grant or
// decision and cannot classify its own Access impact.
func (h *Host) AuthorizeHostOperation(ctx context.Context, sessionRef runtimeprotocol.RuntimeSessionRef, revision runtimeprotocol.CommittedRevisionRef, kind accessprotocol.HostOperationKind, action string, resourceRefs []string) error {
	session, err := h.SessionFor(sessionRef)
	if err != nil || session.Open.CommittedRevision != revision {
		return port.ErrConflict
	}
	ctx = h.accessContext(ctx, session)
	releasePublication, err := h.authority.AcquireAuthoringPublication(ctx, session.Open.Session.Scope)
	if err != nil || releasePublication == nil {
		return accesscore.ErrGrantStale
	}
	defer releasePublication()
	grant, _, err := h.authority.ResolveGrant(ctx, session.Open.Session.Scope)
	if err != nil {
		return err
	}
	impact, err := accesscore.HostOperationImpact(kind, action, accessprotocol.HostResourceScope{
		DocumentID: string(sessionRef.Scope.DocumentID), LocalScopeID: sessionRef.Scope.LocalScopeID, OrganizationScopeID: sessionRef.Scope.OrganizationScopeID,
	}, resourceRefs)
	if err != nil {
		return err
	}
	decision, rejection := h.runtime.Authorize(ctx, runtimehost.AuthorizationRequest{Scope: sessionRef.Scope, CurrentRevision: revision, Evaluation: accessprotocol.EvaluateAuthoringInput{
		GrantSnapshot: grant, HostOperationImpacts: []accessprotocol.HostOperationImpact{impact}, RequestIntent: "publish",
	}})
	if rejection != nil {
		return rejection
	}
	if decision.Outcome != accessprotocol.AuthoringDecisionOutcomeAllow {
		return port.ErrConflict
	}
	return nil
}

func (h *Host) RecoverOperations(ctx context.Context, documentID runtimeprotocol.DocumentID) (runtimeprotocol.RecoverOperationsResult, error) {
	// Runtime protocol v1 supplies only a document ID, so it cannot bind this
	// read or mutation to an issued owner/delegated session. Both recovery and
	// operation details fail closed until a session-bound v2 shape. Trusted
	// startup recovery calls Host.Recover directly inside the local host.
	_ = ctx
	_ = documentID
	return runtimeprotocol.RecoverOperationsResult{Operations: []runtimeprotocol.RuntimeOperationStatus{}}, nil
}

func (h *Host) ListRevisions(ctx context.Context, input runtimeprotocol.ListRevisionsInput) (runtimeprotocol.RevisionPage, error) {
	session, err := h.SessionFor(input.Session)
	if err != nil {
		return runtimeprotocol.RevisionPage{}, err
	}
	ctx = h.accessContext(ctx, session)
	if err := h.authority.AuthorizeRead(ctx, input.Session.Scope, accesscore.SurfaceReview); err != nil {
		return runtimeprotocol.RevisionPage{}, err
	}
	result, rejection := h.runtime.ListRevisions(ctx, input)
	if rejection != nil {
		return runtimeprotocol.RevisionPage{}, rejection
	}
	return result, nil
}

func (h *Host) OperationResult(ctx context.Context, input runtimeprotocol.GetOperationResultInput) (runtimeprotocol.RuntimeOperationStatus, error) {
	session, err := h.SessionFor(input.Session)
	if err != nil {
		return runtimeprotocol.RuntimeOperationStatus{}, err
	}
	ctx = h.accessContext(ctx, session)
	if err := h.authority.AuthorizeRead(ctx, input.Session.Scope, accesscore.SurfaceReview); err != nil {
		return runtimeprotocol.RuntimeOperationStatus{}, err
	}
	result, rejection := h.runtime.GetOperationResult(ctx, input)
	if rejection != nil {
		return runtimeprotocol.RuntimeOperationStatus{}, rejection
	}
	return result, nil
}

// AuthorizeReadSurface is the mandatory host composition point for future
// Search, Query, Review, export, and MCP adapters. The session, not caller
// input, selects the delegated actor context.
func (h *Host) AuthorizeReadSurface(ctx context.Context, ref runtimeprotocol.RuntimeSessionRef, surface accesscore.ReadSurface) error {
	session, err := h.SessionFor(ref)
	if err != nil {
		return err
	}
	return h.authority.AuthorizeRead(h.accessContext(ctx, session), ref.Scope, surface)
}

func (h *Host) Cancel(ctx context.Context, input runtimeprotocol.CancelOperationInput) (runtimeprotocol.CancelOperationResult, error) {
	if _, err := h.SessionFor(input.Session); err != nil {
		return runtimeprotocol.CancelOperationResult{}, err
	}
	result, rejection := h.runtime.CancelOperation(ctx, input)
	if rejection != nil {
		return runtimeprotocol.CancelOperationResult{}, rejection
	}
	return result, nil
}
