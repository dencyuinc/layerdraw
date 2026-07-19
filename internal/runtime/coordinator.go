// SPDX-License-Identifier: LicenseRef-LayerDraw-1.0

package runtime

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"path"
	"reflect"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"
	"unicode/utf16"

	"github.com/dencyuinc/layerdraw/gen/go/accessprotocol"
	"github.com/dencyuinc/layerdraw/gen/go/engineprotocol"
	"github.com/dencyuinc/layerdraw/gen/go/protocolcommon"
	"github.com/dencyuinc/layerdraw/gen/go/runtimeprotocol"
	"github.com/dencyuinc/layerdraw/gen/go/semantic"
	"github.com/dencyuinc/layerdraw/internal/runtime/port"
)

// Coordinator is the host-neutral Runtime state machine. Its only durable
// definition publication is DocumentStore.PublishHead.
type Coordinator struct {
	runtime  *Runtime
	mu       sync.RWMutex
	sessions map[runtimeprotocol.RuntimeSessionID]*sessionState
	cancels  map[operationKey]cancelState
}

type sessionState struct {
	binding SessionBinding
	working port.WorkingDocument
	state   port.StateHead
	grant   accessprotocol.AuthoringGrantSummary
}

func (c *Coordinator) CloseDocument(ctx context.Context, session runtimeprotocol.RuntimeSessionRef) *ContractError {
	c.mu.RLock()
	state, ok := c.sessions[session.RuntimeSessionID]
	if ok {
		if rejection := ValidateSessionUse(session, state.binding, state.binding.Session.Scope, c.runtime.config.Ports.Clock.Now()); rejection != nil {
			c.mu.RUnlock()
			return rejection
		}
	}
	c.mu.RUnlock()
	if !ok {
		return nil
	}
	if closer, ok := c.runtime.config.Ports.Workbench.(port.WorkingDocumentCloser); ok {
		if err := closer.Close(ctx, state.working); err != nil {
			return portFailure("close working document", err)
		}
	}
	c.mu.Lock()
	if c.sessions[session.RuntimeSessionID] == state {
		delete(c.sessions, session.RuntimeSessionID)
		for key := range c.cancels {
			if key.sessionID == session.RuntimeSessionID {
				delete(c.cancels, key)
			}
		}
	}
	c.mu.Unlock()
	return nil
}

type cancelState struct {
	token              runtimeprotocol.CancellationToken
	cancelled          bool
	publicationStarted bool
}

type prePublicationCleanup struct {
	ports         Ports
	scope         runtimeprotocol.RuntimeScope
	documentStage string
	externalStage *port.ExternalFileStage
	retired       bool
}

func (cleanup *prePublicationCleanup) run() {
	if cleanup.retired {
		return
	}
	ctx := context.Background()
	_ = cleanup.ports.Documents.AbortStagedRevision(ctx, port.AbortStagedRevisionInput{Scope: cleanup.scope, StageID: cleanup.documentStage})
	if cleanup.externalStage != nil && cleanup.ports.External != nil {
		_ = cleanup.ports.External.Abort(ctx, port.AbortExternalFileInput{Scope: cleanup.scope, StageID: cleanup.externalStage.StageID})
	}
}

type operationKey struct {
	documentID runtimeprotocol.DocumentID
	sessionID  runtimeprotocol.RuntimeSessionID
	operation  runtimeprotocol.OperationID
}

func newCoordinator(r *Runtime) *Coordinator {
	return &Coordinator{runtime: r, sessions: map[runtimeprotocol.RuntimeSessionID]*sessionState{}, cancels: map[operationKey]cancelState{}}
}

func (c *Coordinator) OpenDocument(ctx context.Context, input runtimeprotocol.OpenRuntimeDocumentInput) (runtimeprotocol.OpenRuntimeDocumentResult, *ContractError) {
	if _, err := runtimeprotocol.EncodeOpenRuntimeDocumentInput(input); err != nil {
		return runtimeprotocol.OpenRuntimeDocumentResult{}, contractError(runtimeprotocol.RuntimeFailureCodeRuntimeMalformedHandle, "open document input is malformed")
	}
	result, rejection := c.openDocument(ctx, input)
	if rejection != nil {
		return runtimeprotocol.OpenRuntimeDocumentResult{}, rejection
	}
	if _, err := runtimeprotocol.EncodeOpenRuntimeDocumentResult(result); err != nil {
		return runtimeprotocol.OpenRuntimeDocumentResult{}, contractError(runtimeprotocol.RuntimeFailureCodeRuntimeCapabilityUnavailable, "open document result violates the Runtime contract")
	}
	return result, nil
}

func (c *Coordinator) openDocument(ctx context.Context, input runtimeprotocol.OpenRuntimeDocumentInput) (runtimeprotocol.OpenRuntimeDocumentResult, *ContractError) {
	p := c.runtime.config.Ports
	scope, err := p.Scopes.ResolveScope(ctx, input.DocumentID)
	if err != nil || scope.DocumentID != input.DocumentID {
		return runtimeprotocol.OpenRuntimeDocumentResult{}, contractError(runtimeprotocol.RuntimeFailureCodeRuntimeAuthorizationStale, "document scope could not be resolved")
	}
	grant, summary, err := p.Grants.ResolveGrant(ctx, scope)
	if err != nil || grant.AccessFingerprint != scope.AccessFingerprint || summary.AccessFingerprint != scope.AccessFingerprint {
		return runtimeprotocol.OpenRuntimeDocumentResult{}, contractError(runtimeprotocol.RuntimeFailureCodeRuntimeAuthorizationStale, "authoring grant could not be resolved")
	}
	head, err := p.Documents.GetHead(ctx, port.GetDocumentHeadInput{Scope: scope})
	if err != nil || !validDocumentHead(head) {
		return runtimeprotocol.OpenRuntimeDocumentResult{}, portFailure("read document head", err)
	}
	revisionID := head.Revision.RevisionID
	if input.RequestedRevisionID != nil {
		revisionID = *input.RequestedRevisionID
	}
	snapshot, err := p.Documents.ReadRevision(ctx, port.ReadRevisionInput{Scope: scope, RevisionID: revisionID})
	if err != nil || !validRevisionSnapshot(snapshot, scope, revisionID) {
		return runtimeprotocol.OpenRuntimeDocumentResult{}, portFailure("read revision", err)
	}
	sources, err := p.Documents.ReadSourceBlobs(ctx, port.ReadSourceBlobsInput{Scope: scope, Revision: snapshot.Revision, Blobs: snapshot.SourceBlobs})
	if err != nil || !validSourceClosure(sources, snapshot.Revision, snapshot.SourceBlobs) {
		return runtimeprotocol.OpenRuntimeDocumentResult{}, portFailure("read source closure", err)
	}
	working, err := p.Workbench.Open(ctx, port.OpenWorkingDocumentInput{Scope: scope, Revision: snapshot, Sources: sources, Limits: c.runtime.config.Limits})
	if err != nil || !validWorkingDocument(working, snapshot.Revision) {
		return runtimeprotocol.OpenRuntimeDocumentResult{}, contractError(runtimeprotocol.RuntimeFailureCodeRuntimeCapabilityUnavailable, "Engine Workbench could not open the revision")
	}
	state, err := p.State.GetHead(ctx, port.GetStateHeadInput{Scope: scope})
	if err != nil || !validStateHead(state) {
		return runtimeprotocol.OpenRuntimeDocumentResult{}, portFailure("read state head", err)
	}
	state.SubjectHashes = cloneSubjectHashes(state.SubjectHashes)
	id, err := p.Identities.NewID(ctx, port.IdentityRuntimeSession)
	if err != nil {
		return runtimeprotocol.OpenRuntimeDocumentResult{}, portFailure("create session identity", err)
	}
	session := runtimeprotocol.RuntimeSessionRef{RuntimeSessionID: runtimeprotocol.RuntimeSessionID(id), Scope: scope, SessionGeneration: "1"}
	binding := SessionBinding{Session: session, CurrentRevision: snapshot.Revision}
	result := runtimeprotocol.OpenRuntimeDocumentResult{
		Session: session, CommittedRevision: snapshot.Revision,
		WorkingDocument: runtimeprotocol.WorkingDocumentRef{Session: session, BaseRevision: snapshot.Revision, WorkingGeneration: runtimeprotocol.WorkingGeneration(working.Generation)},
		// Snapshot construction is a separate boundary. The trusted state head is
		// retained in sessionState for commit preconditions, while the wire result
		// truthfully reports that no StateQuerySnapshot was constructed.
		StateInput:    runtimeprotocol.StateInput{Kind: "none"},
		AccessSummary: summary, CapabilityManifest: c.manifest(summary),
	}
	if _, err := runtimeprotocol.EncodeOpenRuntimeDocumentResult(result); err != nil {
		return runtimeprotocol.OpenRuntimeDocumentResult{}, contractError(runtimeprotocol.RuntimeFailureCodeRuntimeCapabilityUnavailable, "host ports produced an invalid open document binding")
	}
	c.mu.Lock()
	c.sessions[session.RuntimeSessionID] = &sessionState{binding: binding, working: working, state: state, grant: summary}
	c.mu.Unlock()
	return result, nil
}

func (c *Coordinator) CommitOperations(ctx context.Context, input runtimeprotocol.RuntimeCommitInput) (runtimeprotocol.RuntimeCommitResult, *ContractError) {
	if _, err := runtimeprotocol.EncodeRuntimeCommitInput(input); err != nil {
		return runtimeprotocol.RuntimeCommitResult{}, contractError(runtimeprotocol.RuntimeFailureCodeRuntimeStaleRevision, "commit input is malformed")
	}
	result, rejection := c.commitOperations(ctx, input)
	if rejection != nil {
		return runtimeprotocol.RuntimeCommitResult{}, rejection
	}
	// Only a typed terminal result owns cleanup. In particular, an overlapping
	// retry that observes a pending journal record must not remove the active
	// commit's cancellation/publication state.
	defer c.finishOperation(input.Session, input.OperationID)
	if _, err := runtimeprotocol.EncodeRuntimeCommitResult(result); err != nil {
		return runtimeprotocol.RuntimeCommitResult{}, contractError(runtimeprotocol.RuntimeFailureCodeRuntimeCapabilityUnavailable, "commit result violates the Runtime contract")
	}
	return result, nil
}

func (c *Coordinator) commitOperations(ctx context.Context, input runtimeprotocol.RuntimeCommitInput) (runtimeprotocol.RuntimeCommitResult, *ContractError) {
	p := c.runtime.config.Ports
	s, rejection := c.validatedSession(input.Session)
	if rejection != nil {
		return runtimeprotocol.RuntimeCommitResult{}, rejection
	}
	// Discard every caller-supplied session field after validation. All port
	// calls below use the immutable trusted binding.
	input.Session = s.binding.Session
	payloadDigest := logicalCommitDigest(input)
	journalCtx := context.WithoutCancel(ctx)
	if existing, err := p.Recovery.Get(journalCtx, port.GetRecoveryRecordInput{Scope: input.Session.Scope, OperationID: &input.OperationID}); err == nil {
		return c.retryResult(existing, input, payloadDigest)
	} else if !errors.Is(err, port.ErrNotFound) {
		return runtimeprotocol.RuntimeCommitResult{}, portFailure("read operation journal", err)
	}
	if existing, err := p.Recovery.Get(journalCtx, port.GetRecoveryRecordInput{Scope: input.Session.Scope, IdempotencyKey: &input.IdempotencyKey}); err == nil {
		return c.retryResult(existing, input, payloadDigest)
	} else if !errors.Is(err, port.ErrNotFound) {
		return runtimeprotocol.RuntimeCommitResult{}, portFailure("read idempotency journal", err)
	}
	record, err := p.Recovery.CreatePending(journalCtx, port.CreatePendingRecordInput{Scope: input.Session.Scope, OperationID: input.OperationID, IdempotencyKey: input.IdempotencyKey, PayloadDigest: payloadDigest, BaseRevision: input.OperationBatch.BaseRevision})
	if err != nil {
		if errors.Is(err, port.ErrConflict) {
			return c.resolvePendingConflict(journalCtx, input, payloadDigest)
		}
		return runtimeprotocol.RuntimeCommitResult{}, portFailure("create pending operation", err)
	}
	if !validRecoveryRecordForCommit(record, input, payloadDigest, runtimeprotocol.RecoveryPhasePending, nil) {
		return runtimeprotocol.RuntimeCommitResult{}, contractError(runtimeprotocol.RuntimeFailureCodeRuntimeCapabilityUnavailable, "operation journal returned an invalid pending reservation")
	}
	if rejection := c.validateCommitLimits(input); rejection != nil {
		return c.finalRejectedWithoutPreview(ctx, input, rejection)
	}
	if rejection := c.validateStateMutationBinding(input, s); rejection != nil {
		return c.finalRejectedWithoutPreview(ctx, input, rejection)
	}
	if rejection := c.checkCancellation(ctx, input); rejection != nil {
		return c.finalRejectedWithoutPreview(ctx, input, rejection)
	}
	head, err := p.Documents.GetHead(ctx, port.GetDocumentHeadInput{Scope: input.Session.Scope})
	if err != nil || !validDocumentHead(head) {
		return c.abandonPending(ctx, input, portFailure("read current head", err))
	}
	if input.OperationBatch.DocumentID != input.Session.Scope.DocumentID || !reflect.DeepEqual(head.Revision, input.OperationBatch.BaseRevision) || head.Revision.DefinitionHash != input.OperationBatch.ExpectedDefinitionHash {
		return c.finalConflict(ctx, input, head.Revision)
	}
	if rejection, failure := c.validateLease(ctx, input, head); failure != nil {
		return c.abandonPending(ctx, input, failure)
	} else if rejection != nil {
		return c.finalRejectedWithoutPreview(ctx, input, rejection)
	}
	prepared, err := p.Workbench.Preview(ctx, port.PreviewWorkingDocumentInput{Document: s.working, Batch: input.OperationBatch.Operations, Preconditions: input.OperationBatch.Preconditions, MaxOperations: c.runtime.config.Limits.MaxCommitOperations.HardMaximum})
	if err != nil {
		return c.finalRejectedWithoutPreview(ctx, input, contractError(runtimeprotocol.RuntimeFailureCodeRuntimeStaleRevision, "Engine rejected the complete operation batch"))
	}
	if !validPreparedRevision(prepared, input.OperationBatch.BaseRevision) {
		return c.abandonPending(ctx, input, contractError(runtimeprotocol.RuntimeFailureCodeRuntimeCapabilityUnavailable, "Engine returned an invalid prepared revision"))
	}
	requiresExternal := p.External != nil && (input.Trigger == runtimeprotocol.CommitTriggerExplicitSave || input.Trigger == runtimeprotocol.CommitTriggerAutosave)
	if requiresExternal && (prepared.External == nil || !validExternalMaterialization(*prepared.External)) {
		return c.abandonPending(ctx, input, contractError(runtimeprotocol.RuntimeFailureCodeRuntimeCapabilityUnavailable, "Engine did not return a valid external materialization"))
	}
	grant, _, err := p.Grants.ResolveGrant(ctx, input.Session.Scope)
	if err != nil {
		return c.abandonPending(ctx, input, contractError(runtimeprotocol.RuntimeFailureCodeRuntimeAuthorizationStale, "current authoring grant could not be resolved"))
	}
	// The caller proof is intentionally a proposal proof. Validate that exact
	// revision/impact/grant evidence first, then independently require current
	// apply authority. This lets a proposal-only agent preview safely without
	// turning the preview token into an apply permit.
	proposalEvaluation := accessprotocol.EvaluateAuthoringInput{AuthoringImpact: &prepared.AuthoringImpact, GrantSnapshot: grant, HostOperationImpacts: []accessprotocol.HostOperationImpact{}, RequestIntent: "propose"}
	proposalDecision, rejection := c.runtime.Authorize(ctx, AuthorizationRequest{Scope: input.Session.Scope, CurrentRevision: head.Revision, Evaluation: proposalEvaluation, Proof: &input.AuthoringProof})
	if rejection != nil {
		return c.abandonPending(ctx, input, rejection)
	}
	evaluation := accessprotocol.EvaluateAuthoringInput{AuthoringImpact: &prepared.AuthoringImpact, GrantSnapshot: grant, HostOperationImpacts: []accessprotocol.HostOperationImpact{}, RequestIntent: "apply"}
	decision, rejection := c.runtime.Authorize(ctx, AuthorizationRequest{Scope: input.Session.Scope, CurrentRevision: head.Revision, Evaluation: evaluation})
	if rejection != nil || decision.AccessFingerprint != proposalDecision.AccessFingerprint || !reflect.DeepEqual(decision.RequiredCapabilities, proposalDecision.RequiredCapabilities) {
		if rejection != nil {
			return c.abandonPending(ctx, input, rejection)
		}
		return c.abandonPending(ctx, input, contractError(runtimeprotocol.RuntimeFailureCodeRuntimeAuthorizationStale, "authoring decision changed between proposal and apply"))
	}
	if rejection := c.checkCancellation(ctx, input); rejection != nil {
		return c.finalRejected(ctx, input, rejection, decision, prepared.AuthoringImpact)
	}
	previewEvaluation := runtimeprotocol.PreviewEvaluation{AuthoringImpact: prepared.AuthoringImpact, AuthoringDecision: decision}
	staged, err := p.Documents.StageRevision(ctx, port.StageRevisionInput{Scope: input.Session.Scope, OperationID: input.OperationID, IdempotencyKey: input.IdempotencyKey, BaseRevision: head.Revision, DefinitionHash: prepared.DefinitionHash, GraphHash: prepared.GraphHash, SourceBlobs: prepared.Sources, Manifest: prepared.Manifest, DecisionDigest: decision.DecisionDigest, EvaluationDigest: decision.EvaluationDigest, Actor: grant.ActorRef, Trigger: input.Trigger, CancellationToken: input.CancellationToken, PreviewEvaluation: &previewEvaluation})
	if err != nil {
		return c.abandonPending(ctx, input, portFailure("stage revision", err))
	}
	if !validStagedRevision(staged, input.Session.Scope, prepared) {
		if staged.StageID != "" {
			_ = p.Documents.AbortStagedRevision(ctx, port.AbortStagedRevisionInput{Scope: input.Session.Scope, StageID: staged.StageID})
		}
		return c.abandonPending(ctx, input, contractError(runtimeprotocol.RuntimeFailureCodeRuntimeCapabilityUnavailable, "document store returned an invalid staged revision"))
	}
	cleanup := prePublicationCleanup{ports: p, scope: input.Session.Scope, documentStage: staged.StageID}
	defer cleanup.run()
	if _, rejection = c.advanceEvaluated(ctx, input, decision, prepared.AuthoringImpact); rejection != nil {
		return runtimeprotocol.RuntimeCommitResult{}, rejection
	}
	if rejection := c.checkCancellation(ctx, input); rejection != nil {
		return c.finalRejectedFrom(ctx, input, rejection, decision, prepared.AuthoringImpact)
	}
	var externalStage *port.ExternalFileStage
	var expectedExternal *runtimeprotocol.ProviderVersionToken
	if requiresExternal {
		externalHead, externalErr := p.External.GetExternalHead(ctx, port.GetExternalFileHeadInput{Scope: input.Session.Scope})
		if externalErr != nil {
			return c.finalRejectedFrom(ctx, input, contractError(runtimeprotocol.RuntimeFailureCodeRuntimeCapabilityUnavailable, "external file head is unavailable"), decision, prepared.AuthoringImpact)
		}
		candidate, externalErr := p.External.Prepare(ctx, port.PrepareExternalFileInput{Scope: input.Session.Scope, OperationID: input.OperationID, IdempotencyKey: input.IdempotencyKey, RevisionID: staged.Revision.RevisionID, ExpectedProviderVersion: externalHead.ProviderVersion, Materialization: *prepared.External})
		if externalErr != nil {
			code := runtimeprotocol.RuntimeFailureCodeRuntimeCapabilityUnavailable
			if errors.Is(externalErr, port.ErrConflict) {
				code = runtimeprotocol.RuntimeFailureCodeRuntimeStaleRevision
			}
			return c.finalRejectedFrom(ctx, input, contractError(code, "external file preparation failed"), decision, prepared.AuthoringImpact)
		}
		if !validExternalStage(candidate) {
			if candidate.StageID != "" {
				_ = p.External.Abort(context.WithoutCancel(ctx), port.AbortExternalFileInput{Scope: input.Session.Scope, StageID: candidate.StageID})
			}
			return c.finalRejectedFrom(ctx, input, contractError(runtimeprotocol.RuntimeFailureCodeRuntimeCapabilityUnavailable, "external file store returned an invalid stage"), decision, prepared.AuthoringImpact)
		}
		externalStage, expectedExternal = &candidate, &externalHead.ProviderVersion
		cleanup.externalStage = externalStage
	}
	// Preview, authorization, and staging may take long enough for a lease to
	// expire or be fenced. Revalidate at the publication boundary.
	if rejection, failure := c.validateLease(ctx, input, head); failure != nil {
		return runtimeprotocol.RuntimeCommitResult{}, failure
	} else if rejection != nil {
		return c.finalRejectedFrom(ctx, input, rejection, decision, prepared.AuthoringImpact)
	}
	// Authorization is a publication precondition, not a staging permit. Resolve
	// the grant and evaluate the complete impact again after every potentially
	// slow stage/adapter call so policy, delegation expiry/revocation, and agent
	// scope changes reject before the head or external provider is published.
	releasePublication, fenceErr := p.Grants.AcquireAuthoringPublication(ctx, input.Session.Scope)
	if fenceErr != nil || releasePublication == nil {
		return c.finalRejectedFrom(ctx, input, contractError(runtimeprotocol.RuntimeFailureCodeRuntimeAuthorizationStale, "authoring publication fence could not be acquired"), decision, prepared.AuthoringImpact)
	}
	defer releasePublication()
	currentGrant, _, grantErr := p.Grants.ResolveGrant(ctx, input.Session.Scope)
	if grantErr != nil {
		return c.finalRejectedFrom(ctx, input, contractError(runtimeprotocol.RuntimeFailureCodeRuntimeAuthorizationStale, "authoring grant changed before publication"), decision, prepared.AuthoringImpact)
	}
	publicationEvaluation := accessprotocol.EvaluateAuthoringInput{AuthoringImpact: &prepared.AuthoringImpact, GrantSnapshot: currentGrant, HostOperationImpacts: []accessprotocol.HostOperationImpact{}, RequestIntent: "publish"}
	publicationDecision, publicationRejection := c.runtime.Authorize(ctx, AuthorizationRequest{Scope: input.Session.Scope, CurrentRevision: head.Revision, Evaluation: publicationEvaluation})
	if publicationRejection != nil || publicationDecision.AccessFingerprint != decision.AccessFingerprint || publicationDecision.RequiredCapabilities == nil || !reflect.DeepEqual(publicationDecision.RequiredCapabilities, decision.RequiredCapabilities) {
		if publicationRejection != nil {
			return c.finalRejectedFrom(ctx, input, publicationRejection, decision, prepared.AuthoringImpact)
		}
		return c.finalRejectedFrom(ctx, input, contractError(runtimeprotocol.RuntimeFailureCodeRuntimeAuthorizationStale, "authoring decision changed before publication"), decision, prepared.AuthoringImpact)
	}
	if _, rejection = c.advancePublicationPending(ctx, input, decision, externalStage, expectedExternal); rejection != nil {
		return runtimeprotocol.RuntimeCommitResult{}, rejection
	}
	if rejection := c.beginPublication(ctx, input); rejection != nil {
		if rejection.Code == runtimeprotocol.RuntimeFailureCodeRuntimeIdempotencyMismatch {
			return runtimeprotocol.RuntimeCommitResult{}, rejection
		}
		return c.finalRejectedFrom(ctx, input, rejection, decision, prepared.AuthoringImpact)
	}
	published, err := p.Documents.PublishHead(ctx, port.PublishDocumentHeadInput{Scope: input.Session.Scope, StageID: staged.StageID, ExpectedRevision: head.Revision.RevisionID, ExpectedDefinitionHash: head.Revision.DefinitionHash, ExpectedProviderVersion: head.ProviderVersion, FencingToken: head.FencingToken})
	if err != nil || !published.Published {
		return c.resolvePublication(ctx, input, staged, prepared, decision, err, externalStage, expectedExternal, &cleanup)
	}
	if _, encodeErr := runtimeprotocol.EncodeCommittedRevisionRef(published.Revision); encodeErr != nil || !samePublishedRevision(published.Revision, staged.Revision) {
		return c.resolvePublication(ctx, input, staged, prepared, decision, port.ErrIndeterminate, externalStage, expectedExternal, &cleanup)
	}
	cleanup.retired = true
	return c.afterPublication(ctx, input, s, staged.StageID, prepared, decision, published.Revision, externalStage, expectedExternal)
}

func (c *Coordinator) resolvePendingConflict(ctx context.Context, in runtimeprotocol.RuntimeCommitInput, payload protocolcommon.Digest) (runtimeprotocol.RuntimeCommitResult, *ContractError) {
	recovery := c.runtime.config.Ports.Recovery
	byOperation, operationErr := recovery.Get(ctx, port.GetRecoveryRecordInput{Scope: in.Session.Scope, OperationID: &in.OperationID})
	byKey, keyErr := recovery.Get(ctx, port.GetRecoveryRecordInput{Scope: in.Session.Scope, IdempotencyKey: &in.IdempotencyKey})
	operationFound := operationErr == nil
	keyFound := keyErr == nil
	if (!operationFound && !errors.Is(operationErr, port.ErrNotFound)) || (!keyFound && !errors.Is(keyErr, port.ErrNotFound)) {
		return runtimeprotocol.RuntimeCommitResult{}, contractError(runtimeprotocol.RuntimeFailureCodeRuntimeCapabilityUnavailable, "operation journal uniqueness lookup failed")
	}
	if operationFound && (!validRecoveryRecord(byOperation) || !reflect.DeepEqual(byOperation.Scope, in.Session.Scope) || byOperation.Status.OperationID != in.OperationID) {
		return runtimeprotocol.RuntimeCommitResult{}, contractError(runtimeprotocol.RuntimeFailureCodeRuntimeCapabilityUnavailable, "operation journal returned an invalid operation uniqueness record")
	}
	if keyFound && (!validRecoveryRecord(byKey) || !reflect.DeepEqual(byKey.Scope, in.Session.Scope) || byKey.Status.IdempotencyKey != in.IdempotencyKey) {
		return runtimeprotocol.RuntimeCommitResult{}, contractError(runtimeprotocol.RuntimeFailureCodeRuntimeCapabilityUnavailable, "operation journal returned an invalid idempotency uniqueness record")
	}
	if operationFound && keyFound {
		if !reflect.DeepEqual(byOperation, byKey) {
			return runtimeprotocol.RuntimeCommitResult{}, contractError(runtimeprotocol.RuntimeFailureCodeRuntimeCapabilityUnavailable, "operation journal uniqueness indexes disagree")
		}
		return c.retryResult(byOperation, in, payload)
	}
	if operationFound {
		if byOperation.Status.IdempotencyKey != in.IdempotencyKey {
			return runtimeprotocol.RuntimeCommitResult{}, contractError(runtimeprotocol.RuntimeFailureCodeRuntimeIdempotencyMismatch, "operation id was reused with a different idempotency key")
		}
		return runtimeprotocol.RuntimeCommitResult{}, contractError(runtimeprotocol.RuntimeFailureCodeRuntimeCapabilityUnavailable, "operation journal idempotency index is missing")
	}
	if keyFound {
		if byKey.Status.OperationID != in.OperationID {
			return runtimeprotocol.RuntimeCommitResult{}, contractError(runtimeprotocol.RuntimeFailureCodeRuntimeIdempotencyMismatch, "idempotency key was reused with a different operation id")
		}
		return runtimeprotocol.RuntimeCommitResult{}, contractError(runtimeprotocol.RuntimeFailureCodeRuntimeCapabilityUnavailable, "operation journal operation index is missing")
	}
	return runtimeprotocol.RuntimeCommitResult{}, contractError(runtimeprotocol.RuntimeFailureCodeRuntimeCapabilityUnavailable, "operation journal reported a conflict without an authoritative record")
}

func (c *Coordinator) CancelOperation(_ context.Context, input runtimeprotocol.CancelOperationInput) (runtimeprotocol.CancelOperationResult, *ContractError) {
	if _, err := runtimeprotocol.EncodeCancelOperationInput(input); err != nil {
		return runtimeprotocol.CancelOperationResult{}, contractError(runtimeprotocol.RuntimeFailureCodeRuntimeMalformedHandle, "cancel operation input is malformed")
	}
	result, rejection := c.cancelOperation(input)
	if rejection != nil {
		return runtimeprotocol.CancelOperationResult{}, rejection
	}
	if _, err := runtimeprotocol.EncodeCancelOperationResult(result); err != nil {
		return runtimeprotocol.CancelOperationResult{}, contractError(runtimeprotocol.RuntimeFailureCodeRuntimeCapabilityUnavailable, "cancel operation result violates the Runtime contract")
	}
	return result, nil
}

func (c *Coordinator) cancelOperation(input runtimeprotocol.CancelOperationInput) (runtimeprotocol.CancelOperationResult, *ContractError) {
	s, rejection := c.validatedSession(input.Session)
	if rejection != nil {
		return runtimeprotocol.CancelOperationResult{}, rejection
	}
	key := cancellationKey(s.binding.Session, input.OperationID)
	c.mu.Lock()
	state, ok := c.cancels[key]
	status := "not_pending"
	phase := runtimeprotocol.RecoveryPhaseFinal
	if ok && state.token == input.CancellationToken && state.publicationStarted {
		status, phase = "too_late", runtimeprotocol.RecoveryPhasePublicationPending
	} else if ok && state.token == input.CancellationToken {
		state.cancelled = true
		c.cancels[key] = state
		status, phase = "cancel_requested", runtimeprotocol.RecoveryPhasePending
	}
	c.mu.Unlock()
	return runtimeprotocol.CancelOperationResult{OperationID: input.OperationID, Phase: phase, Status: status}, nil
}

func (c *Coordinator) GetOperationResult(ctx context.Context, input runtimeprotocol.GetOperationResultInput) (runtimeprotocol.RuntimeOperationStatus, *ContractError) {
	if _, err := runtimeprotocol.EncodeGetOperationResultInput(input); err != nil {
		return runtimeprotocol.RuntimeOperationStatus{}, contractError(runtimeprotocol.RuntimeFailureCodeRuntimeMalformedHandle, "operation result input is malformed")
	}
	s, rejection := c.validatedSession(input.Session)
	if rejection != nil {
		return runtimeprotocol.RuntimeOperationStatus{}, rejection
	}
	record, err := c.runtime.config.Ports.Recovery.Get(ctx, port.GetRecoveryRecordInput{Scope: s.binding.Session.Scope, OperationID: input.OperationID, IdempotencyKey: input.IdempotencyKey})
	if err != nil {
		return runtimeprotocol.RuntimeOperationStatus{}, portFailure("read operation result", err)
	}
	if !validRecoveryRecord(record) || !reflect.DeepEqual(record.Scope, s.binding.Session.Scope) || (input.OperationID != nil && record.Status.OperationID != *input.OperationID) || (input.IdempotencyKey != nil && record.Status.IdempotencyKey != *input.IdempotencyKey) {
		return runtimeprotocol.RuntimeOperationStatus{}, contractError(runtimeprotocol.RuntimeFailureCodeRuntimeCapabilityUnavailable, "operation journal returned an invalid status")
	}
	return record.Status, nil
}

func (c *Coordinator) ListRevisions(ctx context.Context, input runtimeprotocol.ListRevisionsInput) (runtimeprotocol.RevisionPage, *ContractError) {
	if _, err := runtimeprotocol.EncodeListRevisionsInput(input); err != nil {
		return runtimeprotocol.RevisionPage{}, contractError(runtimeprotocol.RuntimeFailureCodeRuntimeMalformedHandle, "list revisions input is malformed")
	}
	s, rejection := c.validatedSession(input.Session)
	if rejection != nil {
		return runtimeprotocol.RevisionPage{}, rejection
	}
	page, err := c.runtime.config.Ports.History.ListRevisions(ctx, port.ListRevisionsInput{Scope: s.binding.Session.Scope, Cursor: input.Cursor, MaxItems: input.MaxItems, MaxOutputBytes: input.MaxOutputBytes})
	if err != nil {
		return runtimeprotocol.RevisionPage{}, portFailure("list revisions", err)
	}
	if _, err := runtimeprotocol.EncodeRevisionPage(page); err != nil {
		return runtimeprotocol.RevisionPage{}, contractError(runtimeprotocol.RuntimeFailureCodeRuntimeCapabilityUnavailable, "history returned an invalid revision page")
	}
	return page, nil
}

func (c *Coordinator) afterPublication(ctx context.Context, in runtimeprotocol.RuntimeCommitInput, s sessionState, stageID string, prepared port.PreparedRevision, decision accessprotocol.AuthoringDecision, revision runtimeprotocol.CommittedRevisionRef, externalStage *port.ExternalFileStage, expectedExternal *runtimeprotocol.ProviderVersionToken) (runtimeprotocol.RuntimeCommitResult, *ContractError) {
	p := c.runtime.config.Ports
	if _, publicationAdvanceErr := c.advance(context.WithoutCancel(ctx), in, runtimeprotocol.RecoveryPhasePublicationPending, runtimeprotocol.RecoveryPhasePublished, &revision, &decision); publicationAdvanceErr != nil {
		c.checkpointPublished(in, s, prepared, revision, s.state)
		if externalStage != nil {
			return c.externalPendingResult(in, revision, *externalStage, decision, prepared.AuthoringImpact), nil
		}
		stateVersion := s.state.StateVersion
		result := operationResult(in, runtimeprotocol.OperationResultStatusCommittedStateStale, &revision, &stateVersion, "")
		evaluation := runtimeprotocol.PreviewEvaluation{AuthoringImpact: prepared.AuthoringImpact, AuthoringDecision: decision}
		return runtimeprotocol.RuntimeCommitResult{OperationResult: result, PreviewEvaluation: &evaluation}, nil
	}
	status := runtimeprotocol.OperationResultStatusCommitted
	var externalStatus *runtimeprotocol.ExternalMaterializationStatus
	phase := runtimeprotocol.RecoveryPhasePublished
	if externalStage != nil && expectedExternal != nil {
		if _, rejection := c.advance(ctx, in, runtimeprotocol.RecoveryPhasePublished, runtimeprotocol.RecoveryPhaseExternalPending, &revision, &decision); rejection != nil {
			c.checkpointPublished(in, s, prepared, revision, s.state)
			return c.externalPendingResult(in, revision, *externalStage, decision, prepared.AuthoringImpact), nil
		}
		phase = runtimeprotocol.RecoveryPhaseExternalPending
		receipt, externalErr := p.External.Publish(context.WithoutCancel(ctx), port.PublishExternalFileInput{Scope: in.Session.Scope, OperationID: in.OperationID, IdempotencyKey: in.IdempotencyKey, StageID: externalStage.StageID, ExpectedProviderVersion: *expectedExternal})
		if externalErr != nil {
			c.checkpointPublished(in, s, prepared, revision, s.state)
			if !errors.Is(externalErr, port.ErrConflict) {
				return c.externalPendingResult(in, revision, *externalStage, decision, prepared.AuthoringImpact), nil
			}
			failure := runtimeprotocol.ExternalMaterializationFailureConflict
			if _, rejection := c.advanceExternal(ctx, in, runtimeprotocol.RecoveryPhaseExternalPending, runtimeprotocol.RecoveryPhaseExternalFailed, &revision, &decision, nil, &failure); rejection != nil {
				return runtimeprotocol.RuntimeCommitResult{}, rejection
			}
			phase = runtimeprotocol.RecoveryPhaseExternalFailed
			externalStatus = externalFailedResultStatus(*externalStage, failure)
			status = runtimeprotocol.OperationResultStatusCommittedExternalFailed
		} else {
			if _, rejection := c.advanceExternal(ctx, in, runtimeprotocol.RecoveryPhaseExternalPending, runtimeprotocol.RecoveryPhaseExternalPublished, &revision, &decision, &receipt, nil); rejection != nil {
				c.checkpointPublished(in, s, prepared, revision, s.state)
				return c.externalPendingResult(in, revision, *externalStage, decision, prepared.AuthoringImpact), nil
			}
			phase = runtimeprotocol.RecoveryPhaseExternalPublished
			externalStatus = externalPublishedResultStatus(receipt)
		}
	}
	stateHead := s.state
	stateVersion := stateHead.StateVersion
	if _, err := c.advance(context.WithoutCancel(ctx), in, phase, runtimeprotocol.RecoveryPhaseStatePending, &revision, &decision); err != nil {
		status = committedStateStaleStatus()
	}
	if in.StateMutation != nil {
		lease := runtimeprotocol.LeaseToken("")
		if in.LeaseToken != nil {
			lease = *in.LeaseToken
		}
		result, err := p.State.WriteState(context.WithoutCancel(ctx), port.WriteStateInput{Scope: in.Session.Scope, OperationID: in.OperationID, IdempotencyKey: in.IdempotencyKey, ExpectedStateVersion: in.StateMutation.ExpectedStateVersion, ExpectedBackendVersion: s.state.BackendVersion, ExpectedDefinitionHash: s.state.DefinitionHash, ExpectedSubjectHashes: s.state.SubjectHashes, LeaseToken: lease, Mutation: *in.StateMutation})
		if err != nil || !validStateHead(result.Head) || result.Head.DefinitionHash != revision.DefinitionHash || result.Head.GraphHash != revision.GraphHash {
			status = committedStateStaleStatus()
		} else {
			stateHead = result.Head
			stateHead.SubjectHashes = cloneSubjectHashes(result.Head.SubjectHashes)
			stateVersion = result.Head.StateVersion
		}
	}
	if _, rejection := c.advance(context.WithoutCancel(ctx), in, runtimeprotocol.RecoveryPhaseStatePending, runtimeprotocol.RecoveryPhaseAuditPending, &revision, &decision); rejection != nil {
		status = committedStateStaleStatus()
	}
	metadata := runtimeprotocol.RevisionMetadata{Revision: revision, ParentRevisionID: &in.OperationBatch.BaseRevision.RevisionID, OperationID: in.OperationID, Trigger: in.Trigger, AuthoringDecisionDigest: decision.DecisionDigest, CommittedAt: protocolcommon.Rfc3339Time(p.Clock.Now().UTC().Format(time.RFC3339Nano)), ExternalMaterialization: externalStatus}
	if _, err := p.History.AppendRevision(context.WithoutCancel(ctx), port.AppendRevisionInput{Scope: in.Session.Scope, Metadata: metadata}); err != nil {
		status = committedStateStaleStatus()
	}
	if _, rejection := c.advance(context.WithoutCancel(ctx), in, runtimeprotocol.RecoveryPhaseAuditPending, runtimeprotocol.RecoveryPhaseOutboxReady, &revision, &decision); rejection != nil {
		status = committedStateStaleStatus()
	}
	resultStateVersion := &stateVersion
	if status == runtimeprotocol.OperationResultStatusCommittedExternalFailed {
		resultStateVersion = nil
	}
	result := operationResult(in, status, &revision, resultStateVersion, "")
	result.ExternalMaterialization = externalStatus
	result.ResultDigest = digestOperationResult(result)
	evaluation := runtimeprotocol.PreviewEvaluation{AuthoringImpact: prepared.AuthoringImpact, AuthoringDecision: decision}
	commitResult := runtimeprotocol.RuntimeCommitResult{OperationResult: result, PreviewEvaluation: &evaluation}
	if rejection := c.finalizeRecovery(ctx, in, commitResult, runtimeprotocol.RecoveryPhaseFinal, "finalize published operation"); rejection != nil {
		return runtimeprotocol.RuntimeCommitResult{}, rejection
	}
	if externalStatus != nil && externalStatus.State == runtimeprotocol.ExternalMaterializationStateFailed && externalStage != nil {
		_ = p.External.Abort(context.WithoutCancel(ctx), port.AbortExternalFileInput{Scope: in.Session.Scope, StageID: externalStage.StageID})
	}
	// Publication and its terminal result are durable before the disposable
	// candidate is removed. A crash between these steps leaves an orphan that
	// the local recovery driver can safely garbage-collect.
	_ = p.Documents.AbortStagedRevision(context.WithoutCancel(ctx), port.AbortStagedRevisionInput{Scope: in.Session.Scope, StageID: stageID})
	c.checkpointPublished(in, s, prepared, revision, stateHead)
	commitResult.OperationResult = result
	return commitResult, nil
}

func (c *Coordinator) resolvePublication(ctx context.Context, in runtimeprotocol.RuntimeCommitInput, staged port.StagedRevision, prepared port.PreparedRevision, decision accessprotocol.AuthoringDecision, publishErr error, externalStage *port.ExternalFileStage, expectedExternal *runtimeprotocol.ProviderVersionToken, cleanup *prePublicationCleanup) (runtimeprotocol.RuntimeCommitResult, *ContractError) {
	head, headErr := c.runtime.config.Ports.Documents.GetHead(context.WithoutCancel(ctx), port.GetDocumentHeadInput{Scope: in.Session.Scope})
	if headErr == nil && validDocumentHead(head) && samePublishedRevision(head.Revision, staged.Revision) {
		cleanup.retired = true
		s, _ := c.session(in.Session)
		return c.afterPublication(context.WithoutCancel(ctx), in, s, staged.StageID, prepared, decision, head.Revision, externalStage, expectedExternal)
	}
	if headErr == nil && validDocumentHead(head) && (errors.Is(publishErr, port.ErrConflict) || (publishErr == nil && !reflect.DeepEqual(head.Revision, staged.Revision))) {
		result := c.conflict(in, head.Revision)
		evaluation := runtimeprotocol.PreviewEvaluation{AuthoringImpact: prepared.AuthoringImpact, AuthoringDecision: decision}
		result.PreviewEvaluation = &evaluation
		if rejection := c.finalizeRecovery(ctx, in, result, runtimeprotocol.RecoveryPhaseFinal, "finalize publication conflict"); rejection != nil {
			return runtimeprotocol.RuntimeCommitResult{}, rejection
		}
		return result, nil
	}
	// Publication cannot be proven either way. Preserve both candidates for the
	// durable recovery driver; aborting either could destroy the only evidence.
	cleanup.retired = true
	_, _ = c.advance(context.WithoutCancel(ctx), in, runtimeprotocol.RecoveryPhasePublicationPending, runtimeprotocol.RecoveryPhaseRecovering, nil, &decision)
	result := operationResult(in, runtimeprotocol.OperationResultStatusNeedsReview, nil, nil, "")
	evaluation := runtimeprotocol.PreviewEvaluation{AuthoringImpact: prepared.AuthoringImpact, AuthoringDecision: decision}
	commitResult := runtimeprotocol.RuntimeCommitResult{OperationResult: result, PreviewEvaluation: &evaluation}
	if rejection := c.finalizeRecovery(ctx, in, commitResult, runtimeprotocol.RecoveryPhaseNeedsReview, "finalize indeterminate publication"); rejection != nil {
		return runtimeprotocol.RuntimeCommitResult{}, rejection
	}
	return commitResult, nil
}

func samePublishedRevision(published, staged runtimeprotocol.CommittedRevisionRef) bool {
	staged.ProviderVersion = published.ProviderVersion
	return reflect.DeepEqual(published, staged)
}

func (c *Coordinator) session(ref runtimeprotocol.RuntimeSessionRef) (sessionState, *ContractError) {
	c.mu.RLock()
	state := c.sessions[ref.RuntimeSessionID]
	if state != nil {
		copy := *state
		copy.state.SubjectHashes = cloneSubjectHashes(state.state.SubjectHashes)
		state = &copy
	}
	c.mu.RUnlock()
	if state == nil {
		return sessionState{}, contractError(runtimeprotocol.RuntimeFailureCodeRuntimeMalformedHandle, "runtime session is unknown")
	}
	return *state, nil
}

func cloneSubjectHashes(values map[semantic.StableAddress]protocolcommon.Digest) map[semantic.StableAddress]protocolcommon.Digest {
	if values == nil {
		return nil
	}
	result := make(map[semantic.StableAddress]protocolcommon.Digest, len(values))
	for address, digest := range values {
		result[address] = digest
	}
	return result
}

func validStateHead(head port.StateHead) bool {
	if _, err := protocolcommon.EncodeCanonicalNonNegativeInt64(head.StateVersion); err != nil {
		return false
	}
	if _, err := runtimeprotocol.EncodeProviderVersionToken(head.BackendVersion); err != nil {
		return false
	}
	if _, err := protocolcommon.EncodeDigest(head.DefinitionHash); err != nil {
		return false
	}
	if _, err := protocolcommon.EncodeDigest(head.GraphHash); err != nil {
		return false
	}
	if _, err := protocolcommon.EncodeRfc3339Time(head.CapturedAt); err != nil {
		return false
	}
	for address, digest := range head.SubjectHashes {
		if _, err := semantic.EncodeStableAddress(address); err != nil {
			return false
		}
		if _, err := protocolcommon.EncodeDigest(digest); err != nil {
			return false
		}
	}
	return true
}

func validDocumentHead(head port.DocumentHead) bool {
	if _, err := runtimeprotocol.EncodeCommittedRevisionRef(head.Revision); err != nil {
		return false
	}
	if _, err := runtimeprotocol.EncodeProviderVersionToken(head.ProviderVersion); err != nil {
		return false
	}
	if _, err := protocolcommon.EncodeCanonicalUint64(head.FencingToken); err != nil {
		return false
	}
	return true
}

func validRevisionSnapshot(snapshot port.RevisionSnapshot, scope runtimeprotocol.RuntimeScope, revisionID runtimeprotocol.RevisionID) bool {
	if snapshot.Revision.DocumentID != scope.DocumentID || snapshot.Revision.RevisionID != revisionID {
		return false
	}
	if _, err := runtimeprotocol.EncodeCommittedRevisionRef(snapshot.Revision); err != nil {
		return false
	}
	if _, err := protocolcommon.EncodeBlobRef(snapshot.Manifest); err != nil {
		return false
	}
	for _, blob := range snapshot.SourceBlobs {
		if _, err := protocolcommon.EncodeBlobRef(blob); err != nil {
			return false
		}
	}
	return true
}

func validWorkingDocument(working port.WorkingDocument, revision runtimeprotocol.CommittedRevisionRef) bool {
	if working.Handle == "" || !reflect.DeepEqual(working.BaseRevision, revision) || working.DefinitionHash != revision.DefinitionHash || working.GraphHash != revision.GraphHash {
		return false
	}
	_, err := protocolcommon.EncodeCanonicalNonNegativeInt64(working.Generation)
	return err == nil
}

func validPreparedRevision(prepared port.PreparedRevision, base runtimeprotocol.CommittedRevisionRef) bool {
	if prepared.AuthoringImpact.BaseDefinitionHash != base.DefinitionHash || prepared.DefinitionHash != prepared.AuthoringImpact.ResultingDefinitionHash || !validSourceBlobSet(prepared.Sources, base) {
		return false
	}
	if _, err := semantic.EncodeAuthoringImpact(prepared.AuthoringImpact); err != nil {
		return false
	}
	if _, err := protocolcommon.EncodeDigest(prepared.DefinitionHash); err != nil {
		return false
	}
	if _, err := protocolcommon.EncodeDigest(prepared.GraphHash); err != nil {
		return false
	}
	if _, err := protocolcommon.EncodeBlobRef(prepared.Manifest); err != nil {
		return false
	}
	return true
}

func validExternalMaterialization(materialization port.ExternalMaterialization) bool {
	switch materialization.Kind {
	case port.ExternalFileKindContainer:
		return len(materialization.Container) > 0 && len(materialization.ProjectFiles) == 0
	case port.ExternalFileKindProject:
		if len(materialization.Container) != 0 || len(materialization.ProjectFiles) == 0 {
			return false
		}
		seen := make(map[string]struct{}, len(materialization.ProjectFiles))
		for _, file := range materialization.ProjectFiles {
			clean := path.Clean(file.Path)
			if clean != file.Path || clean == "." || strings.HasPrefix(clean, "/") || strings.HasPrefix(clean, "../") || path.Ext(clean) != ".ldl" {
				return false
			}
			if _, exists := seen[clean]; exists {
				return false
			}
			seen[clean] = struct{}{}
		}
		return true
	default:
		return false
	}
}

func validExternalStage(stage port.ExternalFileStage) bool {
	if stage.StageID == "" || stage.CandidateProviderVersion == "" {
		return false
	}
	_, err := protocolcommon.EncodeDigest(stage.MaterializationDigest)
	return err == nil
}

func validSourceClosure(sources port.SourceBlobSet, revision runtimeprotocol.CommittedRevisionRef, expected []protocolcommon.BlobRef) bool {
	if !validSourceBlobSet(sources, revision) || len(sources.Blobs) != len(expected) {
		return false
	}
	expectedByID := make(map[string]protocolcommon.BlobRef, len(expected))
	for _, ref := range expected {
		if _, err := protocolcommon.EncodeBlobRef(ref); err != nil {
			return false
		}
		if _, exists := expectedByID[ref.BlobID]; exists {
			return false
		}
		expectedByID[ref.BlobID] = ref
	}
	for _, blob := range sources.Blobs {
		ref, exists := expectedByID[blob.Ref.BlobID]
		if !exists || !reflect.DeepEqual(ref, blob.Ref) {
			return false
		}
	}
	return true
}

func validSourceBlobSet(sources port.SourceBlobSet, revision runtimeprotocol.CommittedRevisionRef) bool {
	if !reflect.DeepEqual(sources.Revision, revision) {
		return false
	}
	seen := make(map[string]struct{}, len(sources.Blobs))
	for _, blob := range sources.Blobs {
		if _, err := protocolcommon.EncodeBlobRef(blob.Ref); err != nil {
			return false
		}
		if _, exists := seen[blob.Ref.BlobID]; exists {
			return false
		}
		seen[blob.Ref.BlobID] = struct{}{}
		size, err := strconv.ParseUint(string(blob.Ref.Size), 10, 64)
		if err != nil || size != uint64(len(blob.Contents)) {
			return false
		}
		sum := sha256.Sum256(blob.Contents)
		if blob.Ref.Digest != protocolcommon.Digest("sha256:"+hex.EncodeToString(sum[:])) {
			return false
		}
	}
	return true
}

func validStagedRevision(staged port.StagedRevision, scope runtimeprotocol.RuntimeScope, prepared port.PreparedRevision) bool {
	if staged.StageID == "" || staged.Revision.DocumentID != scope.DocumentID || staged.Revision.RevisionID == prepared.Sources.Revision.RevisionID || staged.Revision.DefinitionHash != prepared.DefinitionHash || staged.Revision.GraphHash != prepared.GraphHash {
		return false
	}
	if _, err := runtimeprotocol.EncodeCommittedRevisionRef(staged.Revision); err != nil {
		return false
	}
	if _, err := protocolcommon.EncodeDigest(staged.StagedDigest); err != nil {
		return false
	}
	return true
}

func (c *Coordinator) validatedSession(ref runtimeprotocol.RuntimeSessionRef) (sessionState, *ContractError) {
	state, rejection := c.session(ref)
	if rejection != nil {
		return sessionState{}, rejection
	}
	if rejection = ValidateSessionUse(ref, state.binding, state.binding.Session.Scope, c.runtime.config.Ports.Clock.Now()); rejection != nil {
		return sessionState{}, rejection
	}
	return state, nil
}

func (c *Coordinator) validateCommitLimits(input runtimeprotocol.RuntimeCommitInput) *ContractError {
	maxOps, _ := strconv.ParseInt(string(c.runtime.config.Limits.MaxCommitOperations.HardMaximum), 10, 64)
	if len(input.OperationBatch.Operations.Operations) == 0 || int64(len(input.OperationBatch.Operations.Operations)) > maxOps {
		return contractError(runtimeprotocol.RuntimeFailureCodeRuntimeCapabilityUnavailable, "operation count exceeds Runtime limits")
	}
	if input.StateMutation != nil {
		maxState, _ := strconv.ParseInt(string(c.runtime.config.Limits.MaxStateMutations.HardMaximum), 10, 64)
		if maxState < 1 {
			return contractError(runtimeprotocol.RuntimeFailureCodeRuntimeCapabilityUnavailable, "state mutation exceeds Runtime limits")
		}
	}
	batch, err := engineprotocol.EncodeSemanticOperationBatch(input.OperationBatch.Operations)
	if err != nil || len(batch) > engineprotocol.MaxWireJSONBytes {
		return contractError(runtimeprotocol.RuntimeFailureCodeRuntimeCapabilityUnavailable, "canonical operation batch exceeds wire limits")
	}
	var value any
	if json.Unmarshal(batch, &value) != nil {
		return contractError(runtimeprotocol.RuntimeFailureCodeRuntimeCapabilityUnavailable, "canonical operation batch is invalid")
	}
	maxBlob, _ := strconv.ParseUint(string(c.runtime.config.Limits.MaxBlobBytes.HardMaximum), 10, 64)
	maxTotal, _ := strconv.ParseUint(string(c.runtime.config.Limits.MaxBlobTotalBytes.HardMaximum), 10, 64)
	var total uint64
	if !accumulateBlobSizes(value, maxBlob, maxTotal, &total) {
		return contractError(runtimeprotocol.RuntimeFailureCodeRuntimeCapabilityUnavailable, "operation blob references exceed Runtime limits")
	}
	if input.StateMutation != nil {
		size, err := strconv.ParseUint(string(input.StateMutation.MutationBlob.Blob.Size), 10, 64)
		if err != nil || size > maxBlob || size > maxTotal || total > maxTotal-size {
			return contractError(runtimeprotocol.RuntimeFailureCodeRuntimeCapabilityUnavailable, "state mutation blob exceeds Runtime limits")
		}
	}
	return nil
}

func (c *Coordinator) validateStateMutationBinding(input runtimeprotocol.RuntimeCommitInput, session sessionState) *ContractError {
	if input.StateMutation == nil {
		return nil
	}
	if input.StateMutation.ExpectedStateVersion != session.state.StateVersion {
		return contractError(runtimeprotocol.RuntimeFailureCodeRuntimeStaleRevision, "state mutation expected version is stale")
	}
	blob := input.StateMutation.MutationBlob
	if !sameScope(blob.Scope, input.Session.Scope) || blob.SessionGeneration != input.Session.SessionGeneration {
		return contractError(runtimeprotocol.RuntimeFailureCodeRuntimeBlobScopeMismatch, "state mutation blob is outside the trusted session binding")
	}
	if blob.ExpiresAt != nil && expired(*blob.ExpiresAt, c.runtime.config.Ports.Clock.Now()) {
		return contractError(runtimeprotocol.RuntimeFailureCodeRuntimeBlobExpired, "state mutation blob expired")
	}
	return nil
}

func accumulateBlobSizes(value any, maxBlob, maxTotal uint64, total *uint64) bool {
	switch typed := value.(type) {
	case []any:
		for _, item := range typed {
			if !accumulateBlobSizes(item, maxBlob, maxTotal, total) {
				return false
			}
		}
	case map[string]any:
		if rawSize, ok := typed["size"].(string); ok {
			if _, hasDigest := typed["digest"]; hasDigest {
				size, err := strconv.ParseUint(rawSize, 10, 64)
				if err != nil || size > maxBlob || size > maxTotal || *total > maxTotal-size {
					return false
				}
				*total += size
			}
		}
		for _, item := range typed {
			if !accumulateBlobSizes(item, maxBlob, maxTotal, total) {
				return false
			}
		}
	}
	return true
}

func (c *Coordinator) retryResult(record port.RecoveryRecord, in runtimeprotocol.RuntimeCommitInput, payload protocolcommon.Digest) (runtimeprotocol.RuntimeCommitResult, *ContractError) {
	if !validRecoveryRecord(record) {
		return runtimeprotocol.RuntimeCommitResult{}, contractError(runtimeprotocol.RuntimeFailureCodeRuntimeCapabilityUnavailable, "operation journal returned an invalid recovery record")
	}
	if rejection := ValidateIdempotencyRetry(record.Status.IdempotencyKey, record.PayloadDigest, in.IdempotencyKey, payload); rejection != nil {
		return runtimeprotocol.RuntimeCommitResult{}, rejection
	}
	if !validRecoveryIdentity(record, in, payload) {
		return runtimeprotocol.RuntimeCommitResult{}, contractError(runtimeprotocol.RuntimeFailureCodeRuntimeIdempotencyMismatch, "operation id or idempotency key was reused")
	}
	if record.Status.OperationResult == nil {
		return runtimeprotocol.RuntimeCommitResult{}, contractError(runtimeprotocol.RuntimeFailureCodeRuntimeCapabilityUnavailable, "operation is still pending; query its status")
	}
	return *record.CommitResult, nil
}

func validRecoveryEvaluation(record port.RecoveryRecord) bool {
	if (record.EvaluationDigest == nil) != (record.DecisionDigest == nil) {
		return false
	}
	if record.CommitResult == nil {
		return true
	}
	if record.CommitResult.PreviewEvaluation == nil {
		return record.EvaluationDigest == nil && record.DecisionDigest == nil
	}
	decision := record.CommitResult.PreviewEvaluation.AuthoringDecision
	return record.EvaluationDigest != nil && record.DecisionDigest != nil && *record.EvaluationDigest == decision.EvaluationDigest && *record.DecisionDigest == decision.DecisionDigest
}

func validRecoveryRecord(record port.RecoveryRecord) bool {
	if _, err := runtimeprotocol.EncodeRuntimeScope(record.Scope); err != nil {
		return false
	}
	if _, err := runtimeprotocol.EncodeRuntimeOperationStatus(record.Status); err != nil {
		return false
	}
	if _, err := runtimeprotocol.EncodeCommittedRevisionRef(record.BaseRevision); err != nil {
		return false
	}
	if _, err := protocolcommon.EncodeDigest(record.PayloadDigest); err != nil {
		return false
	}
	if !validRecoveryEvaluation(record) {
		return false
	}
	terminal := record.Status.Phase == runtimeprotocol.RecoveryPhaseFinal || record.Status.Phase == runtimeprotocol.RecoveryPhaseNeedsReview
	if terminal {
		if record.Status.OperationResult == nil || record.CommitResult == nil || !reflect.DeepEqual(*record.Status.OperationResult, record.CommitResult.OperationResult) {
			return false
		}
		result := record.Status.OperationResult
		if result.OperationID != record.Status.OperationID || result.IdempotencyKey != record.Status.IdempotencyKey || result.ResultDigest != digestOperationResult(*result) {
			return false
		}
		if _, err := runtimeprotocol.EncodeRuntimeCommitResult(*record.CommitResult); err != nil {
			return false
		}
		if record.Status.Phase == runtimeprotocol.RecoveryPhaseNeedsReview && record.Status.OperationResult.Status != runtimeprotocol.OperationResultStatusNeedsReview {
			return false
		}
		if record.Status.Phase == runtimeprotocol.RecoveryPhaseFinal && record.Status.OperationResult.Status == runtimeprotocol.OperationResultStatusNeedsReview {
			return false
		}
		return true
	}
	if record.CommitResult != nil {
		return false
	}
	if record.Status.Phase == runtimeprotocol.RecoveryPhasePending {
		return record.EvaluationDigest == nil && record.DecisionDigest == nil
	}
	return record.EvaluationDigest != nil && record.DecisionDigest != nil
}

func validRecoveryIdentity(record port.RecoveryRecord, in runtimeprotocol.RuntimeCommitInput, payload protocolcommon.Digest) bool {
	return reflect.DeepEqual(record.Scope, in.Session.Scope) &&
		record.Status.OperationID == in.OperationID &&
		record.Status.IdempotencyKey == in.IdempotencyKey &&
		record.PayloadDigest == payload &&
		reflect.DeepEqual(record.BaseRevision, in.OperationBatch.BaseRevision)
}

func validRecoveryRecordForCommit(record port.RecoveryRecord, in runtimeprotocol.RuntimeCommitInput, payload protocolcommon.Digest, phase runtimeprotocol.RecoveryPhase, decision *accessprotocol.AuthoringDecision) bool {
	if !validRecoveryRecord(record) || !validRecoveryIdentity(record, in, payload) || record.Status.Phase != phase {
		return false
	}
	if decision == nil {
		return record.EvaluationDigest == nil && record.DecisionDigest == nil
	}
	return record.EvaluationDigest != nil && record.DecisionDigest != nil && *record.EvaluationDigest == decision.EvaluationDigest && *record.DecisionDigest == decision.DecisionDigest
}

func (c *Coordinator) checkCancellation(ctx context.Context, in runtimeprotocol.RuntimeCommitInput) *ContractError {
	if err := ctx.Err(); err != nil {
		return contractError(runtimeprotocol.RuntimeFailureCodeRuntimeCancelled, "commit was cancelled before publication")
	}
	if in.CancellationToken == nil {
		return nil
	}
	key := cancellationKey(in.Session, in.OperationID)
	c.mu.Lock()
	state, exists := c.cancels[key]
	if !exists {
		state = cancelState{token: *in.CancellationToken}
		c.cancels[key] = state
	}
	c.mu.Unlock()
	if state.token != *in.CancellationToken {
		return contractError(runtimeprotocol.RuntimeFailureCodeRuntimeIdempotencyMismatch, "operation cancellation token changed")
	}
	if state.cancelled {
		return contractError(runtimeprotocol.RuntimeFailureCodeRuntimeCancelled, "commit was cancelled before publication")
	}
	return nil
}

func (c *Coordinator) beginPublication(ctx context.Context, in runtimeprotocol.RuntimeCommitInput) *ContractError {
	if in.CancellationToken == nil {
		if ctx.Err() != nil {
			return contractError(runtimeprotocol.RuntimeFailureCodeRuntimeCancelled, "commit was cancelled before publication")
		}
		return nil
	}
	key := cancellationKey(in.Session, in.OperationID)
	c.mu.Lock()
	defer c.mu.Unlock()
	state, ok := c.cancels[key]
	if !ok || state.token != *in.CancellationToken {
		return contractError(runtimeprotocol.RuntimeFailureCodeRuntimeIdempotencyMismatch, "operation cancellation binding is missing")
	}
	if state.cancelled || ctx.Err() != nil {
		return contractError(runtimeprotocol.RuntimeFailureCodeRuntimeCancelled, "commit was cancelled before publication")
	}
	state.publicationStarted = true
	c.cancels[key] = state
	return nil
}

func cancellationKey(session runtimeprotocol.RuntimeSessionRef, operation runtimeprotocol.OperationID) operationKey {
	return operationKey{documentID: session.Scope.DocumentID, sessionID: session.RuntimeSessionID, operation: operation}
}

func (c *Coordinator) finishOperation(session runtimeprotocol.RuntimeSessionRef, operation runtimeprotocol.OperationID) {
	c.mu.Lock()
	delete(c.cancels, cancellationKey(session, operation))
	c.mu.Unlock()
}

func (c *Coordinator) validateLease(ctx context.Context, in runtimeprotocol.RuntimeCommitInput, head port.DocumentHead) (*ContractError, *ContractError) {
	if in.LeaseToken == nil {
		return nil, nil
	}
	lease, err := c.runtime.config.Ports.State.ValidateLease(ctx, port.ValidateLeaseInput{Scope: in.Session.Scope, LeaseToken: *in.LeaseToken})
	if err != nil {
		if errors.Is(err, port.ErrConflict) {
			return contractError(runtimeprotocol.RuntimeFailureCodeRuntimeStaleRevision, "lease is invalid or stale"), nil
		}
		return nil, portFailure("validate lease", err)
	}
	if lease.FencingToken != head.FencingToken || !c.runtime.config.Ports.Clock.Now().Before(lease.ExpiresAt) {
		return contractError(runtimeprotocol.RuntimeFailureCodeRuntimeStaleRevision, "lease is invalid or stale"), nil
	}
	return nil, nil
}

func (c *Coordinator) advance(ctx context.Context, in runtimeprotocol.RuntimeCommitInput, from, to runtimeprotocol.RecoveryPhase, revision *runtimeprotocol.CommittedRevisionRef, decision *accessprotocol.AuthoringDecision) (port.RecoveryRecord, *ContractError) {
	if rejection := ValidateRecoveryTransition(from, to); rejection != nil {
		return port.RecoveryRecord{}, rejection
	}
	record, err := c.runtime.config.Ports.Recovery.Advance(ctx, port.AdvanceRecoveryRecordInput{Scope: in.Session.Scope, OperationID: in.OperationID, ExpectedPhase: from, NextPhase: to, PublishedRevision: revision})
	if err != nil {
		return port.RecoveryRecord{}, portFailure("advance operation journal", err)
	}
	if !validRecoveryRecordForCommit(record, in, logicalCommitDigest(in), to, decision) {
		return port.RecoveryRecord{}, contractError(runtimeprotocol.RuntimeFailureCodeRuntimeCapabilityUnavailable, "operation journal returned an invalid transition record")
	}
	return record, nil
}

func (c *Coordinator) advancePublicationPending(ctx context.Context, in runtimeprotocol.RuntimeCommitInput, decision accessprotocol.AuthoringDecision, externalStage *port.ExternalFileStage, expectedExternal *runtimeprotocol.ProviderVersionToken) (port.RecoveryRecord, *ContractError) {
	if (externalStage == nil) != (expectedExternal == nil) {
		return port.RecoveryRecord{}, contractError(runtimeprotocol.RuntimeFailureCodeRuntimeCapabilityUnavailable, "external publication reservation is incomplete")
	}
	record, err := c.runtime.config.Ports.Recovery.Advance(context.WithoutCancel(ctx), port.AdvanceRecoveryRecordInput{
		Scope:                           in.Session.Scope,
		OperationID:                     in.OperationID,
		ExpectedPhase:                   runtimeprotocol.RecoveryPhaseStaged,
		NextPhase:                       runtimeprotocol.RecoveryPhasePublicationPending,
		ExternalStage:                   externalStage,
		ExpectedExternalProviderVersion: expectedExternal,
	})
	if err != nil {
		return port.RecoveryRecord{}, portFailure("advance operation journal", err)
	}
	if !validRecoveryRecordForCommit(record, in, logicalCommitDigest(in), runtimeprotocol.RecoveryPhasePublicationPending, &decision) {
		return port.RecoveryRecord{}, contractError(runtimeprotocol.RuntimeFailureCodeRuntimeCapabilityUnavailable, "operation journal returned an invalid publication reservation")
	}
	return record, nil
}

func (c *Coordinator) advanceExternal(ctx context.Context, in runtimeprotocol.RuntimeCommitInput, from, to runtimeprotocol.RecoveryPhase, revision *runtimeprotocol.CommittedRevisionRef, decision *accessprotocol.AuthoringDecision, receipt *port.ExternalFileReceipt, failure *runtimeprotocol.ExternalMaterializationFailure) (port.RecoveryRecord, *ContractError) {
	record, err := c.runtime.config.Ports.Recovery.Advance(context.WithoutCancel(ctx), port.AdvanceRecoveryRecordInput{
		Scope:             in.Session.Scope,
		OperationID:       in.OperationID,
		ExpectedPhase:     from,
		NextPhase:         to,
		PublishedRevision: revision,
		ExternalReceipt:   receipt,
		ExternalFailure:   failure,
	})
	if err != nil {
		return port.RecoveryRecord{}, portFailure("advance external publication journal", err)
	}
	if !validRecoveryRecordForCommit(record, in, logicalCommitDigest(in), to, decision) {
		return port.RecoveryRecord{}, contractError(runtimeprotocol.RuntimeFailureCodeRuntimeCapabilityUnavailable, "operation journal returned an invalid external publication transition")
	}
	return record, nil
}

func externalPendingResultStatus(stage port.ExternalFileStage) *runtimeprotocol.ExternalMaterializationStatus {
	return &runtimeprotocol.ExternalMaterializationStatus{State: runtimeprotocol.ExternalMaterializationStatePending, CandidateProviderVersion: stage.CandidateProviderVersion}
}

func externalPublishedResultStatus(receipt port.ExternalFileReceipt) *runtimeprotocol.ExternalMaterializationStatus {
	provider, digest := receipt.ProviderVersion, receipt.ReceiptDigest
	return &runtimeprotocol.ExternalMaterializationStatus{State: runtimeprotocol.ExternalMaterializationStatePublished, CandidateProviderVersion: receipt.ProviderVersion, ProviderVersion: &provider, ReceiptDigest: &digest}
}

func externalFailedResultStatus(stage port.ExternalFileStage, failure runtimeprotocol.ExternalMaterializationFailure) *runtimeprotocol.ExternalMaterializationStatus {
	return &runtimeprotocol.ExternalMaterializationStatus{State: runtimeprotocol.ExternalMaterializationStateFailed, CandidateProviderVersion: stage.CandidateProviderVersion, Failure: &failure}
}

func (c *Coordinator) externalPendingResult(in runtimeprotocol.RuntimeCommitInput, revision runtimeprotocol.CommittedRevisionRef, stage port.ExternalFileStage, decision accessprotocol.AuthoringDecision, impact semantic.AuthoringImpact) runtimeprotocol.RuntimeCommitResult {
	result := operationResult(in, runtimeprotocol.OperationResultStatusCommittedExternalPending, &revision, nil, "")
	result.ExternalMaterialization = externalPendingResultStatus(stage)
	result.ResultDigest = digestOperationResult(result)
	evaluation := runtimeprotocol.PreviewEvaluation{AuthoringImpact: impact, AuthoringDecision: decision}
	return runtimeprotocol.RuntimeCommitResult{OperationResult: result, PreviewEvaluation: &evaluation}
}

func (c *Coordinator) checkpointPublished(in runtimeprotocol.RuntimeCommitInput, previous sessionState, prepared port.PreparedRevision, revision runtimeprotocol.CommittedRevisionRef, state port.StateHead) {
	working, err := c.runtime.config.Ports.Workbench.Checkpoint(context.Background(), port.CheckpointWorkingDocumentInput{Document: previous.working, Prepared: prepared, Revision: revision})
	if err != nil {
		return
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	current := c.sessions[in.Session.RuntimeSessionID]
	if current == nil || current.binding.CurrentRevision.RevisionID != previous.binding.CurrentRevision.RevisionID {
		return
	}
	current.binding.CurrentRevision = revision
	current.working = working
	current.state = state
	current.state.SubjectHashes = cloneSubjectHashes(state.SubjectHashes)
}

func committedStateStaleStatus() runtimeprotocol.OperationResultStatus {
	return runtimeprotocol.OperationResultStatusCommittedStateStale
}

func (c *Coordinator) advanceEvaluated(ctx context.Context, in runtimeprotocol.RuntimeCommitInput, decision accessprotocol.AuthoringDecision, impact semantic.AuthoringImpact) (port.RecoveryRecord, *ContractError) {
	preview := runtimeprotocol.PreviewEvaluation{AuthoringImpact: impact, AuthoringDecision: decision}
	record, err := c.runtime.config.Ports.Recovery.Advance(ctx, port.AdvanceRecoveryRecordInput{Scope: in.Session.Scope, OperationID: in.OperationID, ExpectedPhase: runtimeprotocol.RecoveryPhasePending, NextPhase: runtimeprotocol.RecoveryPhaseStaged, EvaluationDigest: &decision.EvaluationDigest, DecisionDigest: &decision.DecisionDigest, PreviewEvaluation: &preview})
	if err != nil {
		return port.RecoveryRecord{}, portFailure("bind operation evaluation", err)
	}
	if !validRecoveryRecordForCommit(record, in, logicalCommitDigest(in), runtimeprotocol.RecoveryPhaseStaged, &decision) {
		return port.RecoveryRecord{}, contractError(runtimeprotocol.RuntimeFailureCodeRuntimeCapabilityUnavailable, "operation journal did not bind evaluation evidence")
	}
	return record, nil
}

func (c *Coordinator) finalRejected(ctx context.Context, in runtimeprotocol.RuntimeCommitInput, rejection *ContractError, decision accessprotocol.AuthoringDecision, impact semantic.AuthoringImpact) (runtimeprotocol.RuntimeCommitResult, *ContractError) {
	return c.finalRejectedFrom(ctx, in, rejection, decision, impact)
}

func (c *Coordinator) finalRejectedWithoutPreview(ctx context.Context, in runtimeprotocol.RuntimeCommitInput, rejection *ContractError) (runtimeprotocol.RuntimeCommitResult, *ContractError) {
	commitResult := c.rejected(in, rejection.Code, rejection.Message)
	if finalizeRejection := c.finalizeRecovery(ctx, in, commitResult, runtimeprotocol.RecoveryPhaseFinal, "finalize rejected operation"); finalizeRejection != nil {
		return runtimeprotocol.RuntimeCommitResult{}, finalizeRejection
	}
	return commitResult, nil
}

func (c *Coordinator) abandonPending(ctx context.Context, in runtimeprotocol.RuntimeCommitInput, rejection *ContractError) (runtimeprotocol.RuntimeCommitResult, *ContractError) {
	err := c.runtime.config.Ports.Recovery.AbandonPending(context.WithoutCancel(ctx), port.AbandonPendingRecordInput{Scope: in.Session.Scope, OperationID: in.OperationID, IdempotencyKey: in.IdempotencyKey, PayloadDigest: logicalCommitDigest(in)})
	if err != nil {
		return runtimeprotocol.RuntimeCommitResult{}, portFailure("abandon pending operation", err)
	}
	c.finishOperation(in.Session, in.OperationID)
	return runtimeprotocol.RuntimeCommitResult{}, rejection
}

func (c *Coordinator) finalConflict(ctx context.Context, in runtimeprotocol.RuntimeCommitInput, current runtimeprotocol.CommittedRevisionRef) (runtimeprotocol.RuntimeCommitResult, *ContractError) {
	commitResult := c.conflict(in, current)
	if rejection := c.finalizeRecovery(ctx, in, commitResult, runtimeprotocol.RecoveryPhaseFinal, "finalize stale-base conflict"); rejection != nil {
		return runtimeprotocol.RuntimeCommitResult{}, rejection
	}
	return commitResult, nil
}

func (c *Coordinator) finalRejectedFrom(ctx context.Context, in runtimeprotocol.RuntimeCommitInput, rejection *ContractError, decision accessprotocol.AuthoringDecision, impact semantic.AuthoringImpact) (runtimeprotocol.RuntimeCommitResult, *ContractError) {
	result := operationResult(in, runtimeprotocol.OperationResultStatusRejected, nil, nil, rejection.Code)
	evaluation := runtimeprotocol.PreviewEvaluation{AuthoringImpact: impact, AuthoringDecision: decision}
	commitResult := runtimeprotocol.RuntimeCommitResult{OperationResult: result, PreviewEvaluation: &evaluation}
	if finalizeRejection := c.finalizeRecovery(ctx, in, commitResult, runtimeprotocol.RecoveryPhaseFinal, "finalize rejected operation"); finalizeRejection != nil {
		return runtimeprotocol.RuntimeCommitResult{}, finalizeRejection
	}
	return commitResult, nil
}

func (c *Coordinator) finalizeRecovery(ctx context.Context, in runtimeprotocol.RuntimeCommitInput, commitResult runtimeprotocol.RuntimeCommitResult, phase runtimeprotocol.RecoveryPhase, action string) *ContractError {
	record, err := c.runtime.config.Ports.Recovery.Finalize(context.WithoutCancel(ctx), port.FinalizeRecoveryRecordInput{Scope: in.Session.Scope, CommitResult: commitResult, TerminalPhase: phase})
	if err != nil {
		return portFailure(action, err)
	}
	var decision *accessprotocol.AuthoringDecision
	if commitResult.PreviewEvaluation != nil {
		decision = &commitResult.PreviewEvaluation.AuthoringDecision
	}
	if !validRecoveryRecordForCommit(record, in, logicalCommitDigest(in), phase, decision) || record.CommitResult == nil || !reflect.DeepEqual(*record.CommitResult, commitResult) || record.Status.OperationResult == nil || !reflect.DeepEqual(*record.Status.OperationResult, commitResult.OperationResult) {
		return contractError(runtimeprotocol.RuntimeFailureCodeRuntimeCapabilityUnavailable, "operation journal returned an invalid terminal record")
	}
	return nil
}

func (c *Coordinator) rejected(in runtimeprotocol.RuntimeCommitInput, code runtimeprotocol.RuntimeFailureCode, _ string) runtimeprotocol.RuntimeCommitResult {
	return runtimeprotocol.RuntimeCommitResult{OperationResult: operationResult(in, runtimeprotocol.OperationResultStatusRejected, nil, nil, code)}
}

func (c *Coordinator) conflict(in runtimeprotocol.RuntimeCommitInput, current runtimeprotocol.CommittedRevisionRef) runtimeprotocol.RuntimeCommitResult {
	result := operationResult(in, runtimeprotocol.OperationResultStatusRejected, nil, nil, runtimeprotocol.RuntimeFailureCodeRuntimeStaleRevision)
	result.ConflictEvidence = &runtimeprotocol.ConflictEvidence{CurrentHead: current}
	result.ResultDigest = digestOperationResult(result)
	return runtimeprotocol.RuntimeCommitResult{OperationResult: result}
}

func operationResult(in runtimeprotocol.RuntimeCommitInput, status runtimeprotocol.OperationResultStatus, revision *runtimeprotocol.CommittedRevisionRef, state *protocolcommon.CanonicalNonNegativeInt64, failure runtimeprotocol.RuntimeFailureCode) runtimeprotocol.OperationResult {
	result := runtimeprotocol.OperationResult{OperationID: in.OperationID, IdempotencyKey: in.IdempotencyKey, Status: status, CommittedRevision: revision, StateVersion: state, Diagnostics: []semantic.Diagnostic{}}
	if status == runtimeprotocol.OperationResultStatusCommittedStateStale {
		result.Diagnostics = terminalDiagnostic("LDL1902", "operation_state_stale")
	}
	if status == runtimeprotocol.OperationResultStatusCommittedExternalPending {
		result.Diagnostics = terminalDiagnostic("LDL1904", "operation_external_materialization_pending")
	}
	if status == runtimeprotocol.OperationResultStatusCommittedExternalFailed {
		result.Diagnostics = terminalDiagnostic("LDL1905", "operation_external_materialization_failed")
	}
	if status == runtimeprotocol.OperationResultStatusNeedsReview {
		result.Diagnostics = terminalDiagnostic("LDL1903", "operation_recovery_needs_review")
	}
	if status == runtimeprotocol.OperationResultStatusRejected {
		result.Diagnostics = terminalDiagnostic("LDL1901", "operation_rejected")
	}
	if failure != "" {
		result.FailureCode = &failure
	}
	result.ResultDigest = digestOperationResult(result)
	return result
}

// RecoveryCommitResult constructs the canonical terminal result used by a
// host recovery driver after it has reconciled trusted journal and head
// evidence. It does not decide an outcome or mutate storage.
func RecoveryCommitResult(operationID runtimeprotocol.OperationID, idempotencyKey runtimeprotocol.IdempotencyKey, status runtimeprotocol.OperationResultStatus, revision *runtimeprotocol.CommittedRevisionRef, stateVersion *protocolcommon.CanonicalNonNegativeInt64, failure runtimeprotocol.RuntimeFailureCode) runtimeprotocol.RuntimeCommitResult {
	input := runtimeprotocol.RuntimeCommitInput{OperationID: operationID, IdempotencyKey: idempotencyKey}
	return runtimeprotocol.RuntimeCommitResult{OperationResult: operationResult(input, status, revision, stateVersion, failure)}
}

// RecoveryCommitResultWithExternal constructs the same canonical result while
// retaining durable external-publication evidence reconciled by a host driver.
func RecoveryCommitResultWithExternal(operationID runtimeprotocol.OperationID, idempotencyKey runtimeprotocol.IdempotencyKey, status runtimeprotocol.OperationResultStatus, revision *runtimeprotocol.CommittedRevisionRef, stateVersion *protocolcommon.CanonicalNonNegativeInt64, failure runtimeprotocol.RuntimeFailureCode, external *runtimeprotocol.ExternalMaterializationStatus) runtimeprotocol.RuntimeCommitResult {
	result := RecoveryCommitResult(operationID, idempotencyKey, status, revision, stateVersion, failure)
	result.OperationResult.ExternalMaterialization = external
	result.OperationResult.ResultDigest = digestOperationResult(result.OperationResult)
	return result
}

func terminalDiagnostic(code, messageKey string) []semantic.Diagnostic {
	return []semantic.Diagnostic{{Code: code, Severity: semantic.DiagnosticSeverityError, MessageKey: messageKey, ProtocolVersion: 1, Arguments: map[string]semantic.DiagnosticArgumentValue{}, Related: []semantic.DiagnosticRelated{}}}
}

func digestOperationResult(result runtimeprotocol.OperationResult) protocolcommon.Digest {
	projection := struct {
		CommittedRevision *runtimeprotocol.CommittedRevisionRef          `json:"committed_revision,omitempty"`
		ConflictEvidence  *runtimeprotocol.ConflictEvidence              `json:"conflict_evidence,omitempty"`
		Diagnostics       []semantic.Diagnostic                          `json:"diagnostics"`
		External          *runtimeprotocol.ExternalMaterializationStatus `json:"external_materialization,omitempty"`
		FailureCode       *runtimeprotocol.RuntimeFailureCode            `json:"failure_code,omitempty"`
		IdempotencyKey    runtimeprotocol.IdempotencyKey                 `json:"idempotency_key"`
		OperationID       runtimeprotocol.OperationID                    `json:"operation_id"`
		StateVersion      *protocolcommon.CanonicalNonNegativeInt64      `json:"state_version,omitempty"`
		Status            runtimeprotocol.OperationResultStatus          `json:"status"`
	}{result.CommittedRevision, result.ConflictEvidence, result.Diagnostics, result.ExternalMaterialization, result.FailureCode, result.IdempotencyKey, result.OperationID, result.StateVersion, result.Status}
	return digestValue(projection)
}
func digestValue(value any) protocolcommon.Digest {
	data, err := canonicalRuntimeJSON(value)
	if err != nil {
		return ""
	}
	sum := sha256.Sum256(data)
	return protocolcommon.Digest("sha256:" + hex.EncodeToString(sum[:]))
}

func canonicalRuntimeJSON(value any) ([]byte, error) {
	encoded, err := json.Marshal(value)
	if err != nil {
		return nil, err
	}
	decoder := json.NewDecoder(bytes.NewReader(encoded))
	decoder.UseNumber()
	var raw any
	if err := decoder.Decode(&raw); err != nil {
		return nil, err
	}
	return appendCanonicalRuntimeJSON(nil, raw)
}

func appendCanonicalRuntimeJSON(destination []byte, value any) ([]byte, error) {
	switch typed := value.(type) {
	case nil:
		return append(destination, "null"...), nil
	case bool:
		return strconv.AppendBool(destination, typed), nil
	case string:
		var buffer bytes.Buffer
		encoder := json.NewEncoder(&buffer)
		encoder.SetEscapeHTML(false)
		if err := encoder.Encode(typed); err != nil {
			return nil, err
		}
		return append(destination, bytes.TrimSuffix(buffer.Bytes(), []byte{'\n'})...), nil
	case json.Number:
		text := typed.String()
		integer, err := strconv.ParseInt(text, 10, 64)
		if err != nil || strconv.FormatInt(integer, 10) != text {
			return nil, errors.New("Runtime canonical JSON permits only canonical integer numbers")
		}
		return append(destination, text...), nil
	case []any:
		destination = append(destination, '[')
		for index, item := range typed {
			if index != 0 {
				destination = append(destination, ',')
			}
			var err error
			destination, err = appendCanonicalRuntimeJSON(destination, item)
			if err != nil {
				return nil, err
			}
		}
		return append(destination, ']'), nil
	case map[string]any:
		keys := make([]string, 0, len(typed))
		for key := range typed {
			keys = append(keys, key)
		}
		sort.Slice(keys, func(leftIndex, rightIndex int) bool {
			left := utf16.Encode([]rune(keys[leftIndex]))
			right := utf16.Encode([]rune(keys[rightIndex]))
			for index := 0; index < len(left) && index < len(right); index++ {
				if left[index] != right[index] {
					return left[index] < right[index]
				}
			}
			return len(left) < len(right)
		})
		destination = append(destination, '{')
		for index, key := range keys {
			if index != 0 {
				destination = append(destination, ',')
			}
			var err error
			destination, err = appendCanonicalRuntimeJSON(destination, key)
			if err != nil {
				return nil, err
			}
			destination = append(destination, ':')
			destination, err = appendCanonicalRuntimeJSON(destination, typed[key])
			if err != nil {
				return nil, err
			}
		}
		return append(destination, '}'), nil
	default:
		return nil, fmt.Errorf("unsupported Runtime canonical JSON value %T", value)
	}
}

func logicalCommitDigest(input runtimeprotocol.RuntimeCommitInput) protocolcommon.Digest {
	return RetryRequestDigest(input)
}

// RetryRequestDigest is the durable idempotency identity shared by Runtime and
// framework-neutral hosts. It excludes only the process-local Workbench handle
// and generation; every portable request fact remains hash-bound.
func RetryRequestDigest(input runtimeprotocol.RuntimeCommitInput) protocolcommon.Digest {
	type logicalBlob struct {
		Digest    protocolcommon.Digest          `json:"digest"`
		MediaType string                         `json:"media_type"`
		Size      protocolcommon.CanonicalUint64 `json:"size"`
	}
	type logicalStateMutation struct {
		AffectedSubjects     []semantic.StableAddress                 `json:"affected_subjects"`
		ExpectedStateVersion protocolcommon.CanonicalNonNegativeInt64 `json:"expected_state_version"`
		MutationBlob         logicalBlob                              `json:"mutation_blob"`
		MutationDigest       protocolcommon.Digest                    `json:"mutation_digest"`
	}
	var stateMutation *logicalStateMutation
	if input.StateMutation != nil {
		blob := input.StateMutation.MutationBlob.Blob
		stateMutation = &logicalStateMutation{
			AffectedSubjects:     input.StateMutation.AffectedSubjects,
			ExpectedStateVersion: input.StateMutation.ExpectedStateVersion,
			MutationBlob:         logicalBlob{Digest: blob.Digest, MediaType: blob.MediaType, Size: blob.Size},
			MutationDigest:       input.StateMutation.MutationDigest,
		}
	}
	batch := input.OperationBatch
	batch.Preconditions.DocumentGeneration = engineprotocol.DocumentGeneration{}
	projection := struct {
		DocumentID     runtimeprotocol.DocumentID            `json:"document_id"`
		OperationBatch runtimeprotocol.RuntimeOperationBatch `json:"operation_batch"`
		OperationID    runtimeprotocol.OperationID           `json:"operation_id"`
		StateMutation  *logicalStateMutation                 `json:"state_mutation,omitempty"`
		Trigger        runtimeprotocol.CommitTrigger         `json:"trigger"`
	}{input.Session.Scope.DocumentID, batch, input.OperationID, stateMutation, input.Trigger}
	return digestValue(projection)
}

func portFailure(action string, err error) *ContractError {
	code := runtimeprotocol.RuntimeFailureCodeRuntimeCapabilityUnavailable
	if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
		code = runtimeprotocol.RuntimeFailureCodeRuntimeCancelled
	}
	return contractError(code, action+" failed")
}

func (c *Coordinator) manifest(summary accessprotocol.AuthoringGrantSummary) runtimeprotocol.RuntimeCapabilityManifest {
	manifest := runtimeprotocol.RuntimeCapabilityManifest{AuthoringGrantSummary: &summary, Limits: c.runtime.config.Limits, Operations: map[string]protocolcommon.OperationCapability{}, StorageCapabilities: c.runtime.storageCapabilities()}
	for _, operation := range c.runtime.enabledOperations() {
		manifest.Operations[string(operation)] = protocolcommon.OperationCapability{Enabled: true, ProtocolVersion: ProtocolVersion}
	}
	manifest.ManifestEtag = manifestETag(manifest)
	return manifest
}

var _ OpenDocumentOperation = (*Coordinator)(nil)
var _ CommitOperationsOperation = (*Coordinator)(nil)
var _ CancelOperationOperation = (*Coordinator)(nil)
var _ GetOperationResultOperation = (*Coordinator)(nil)
var _ ListRevisionsOperation = (*Coordinator)(nil)
