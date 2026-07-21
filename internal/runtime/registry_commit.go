// SPDX-License-Identifier: LicenseRef-LayerDraw-1.0

package runtime

import (
	"context"
	"errors"
	"reflect"
	"time"

	"github.com/dencyuinc/layerdraw/gen/go/accessprotocol"
	"github.com/dencyuinc/layerdraw/gen/go/protocolcommon"
	"github.com/dencyuinc/layerdraw/gen/go/runtimeprotocol"
	"github.com/dencyuinc/layerdraw/gen/go/semantic"
	"github.com/dencyuinc/layerdraw/internal/runtime/port"
)

// RegistryCommitInput is the closed, owner-only handoff from Registry to
// Runtime. It carries verified staged-object identities and Access evidence;
// it cannot carry an Engine operation batch or caller-provided source bytes.
type RegistryCommitInput struct {
	Session                    runtimeprotocol.RuntimeSessionRef
	OperationID                runtimeprotocol.OperationID
	IdempotencyKey             runtimeprotocol.IdempotencyKey
	BaseRevision               runtimeprotocol.CommittedRevisionRef
	RegistryTransactionID      string
	PlanDigest                 protocolcommon.Digest
	MutationDigest             protocolcommon.Digest
	ExpectedResolvedLockDigest protocolcommon.Digest
	StagedObjects              []port.RegistryStagedObjectRef
	AuthoringImpact            semantic.AuthoringImpact
	HostOperationImpacts       []accessprotocol.HostOperationImpact
	ExpectedDecision           accessprotocol.AuthoringDecision
	ProjectMutation            port.RegistryProjectMutation
	LeaseToken                 *runtimeprotocol.LeaseToken
	CancellationToken          *runtimeprotocol.CancellationToken
}

func (c *Coordinator) CommitRegistryPlan(ctx context.Context, input RegistryCommitInput) (runtimeprotocol.RuntimeCommitResult, *ContractError) {
	if rejection := validateRegistryCommitInput(input); rejection != nil {
		return runtimeprotocol.RuntimeCommitResult{}, rejection
	}
	result, rejection := c.commitRegistryPlan(ctx, input)
	if rejection != nil {
		return runtimeprotocol.RuntimeCommitResult{}, rejection
	}
	defer c.finishOperation(input.Session, input.OperationID)
	if _, err := runtimeprotocol.EncodeRuntimeCommitResult(result); err != nil {
		return runtimeprotocol.RuntimeCommitResult{}, contractError(runtimeprotocol.RuntimeFailureCodeRuntimeCapabilityUnavailable, "registry commit result violates the Runtime contract")
	}
	return result, nil
}

func validateRegistryCommitInput(in RegistryCommitInput) *ContractError {
	if _, err := runtimeprotocol.EncodeRuntimeSessionRef(in.Session); err != nil {
		return contractError(runtimeprotocol.RuntimeFailureCodeRuntimeMalformedHandle, "registry commit session is malformed")
	}
	if _, err := runtimeprotocol.EncodeOperationID(in.OperationID); err != nil {
		return contractError(runtimeprotocol.RuntimeFailureCodeRuntimeMalformedHandle, "registry operation id is malformed")
	}
	if _, err := runtimeprotocol.EncodeIdempotencyKey(in.IdempotencyKey); err != nil {
		return contractError(runtimeprotocol.RuntimeFailureCodeRuntimeMalformedHandle, "registry idempotency key is malformed")
	}
	if _, err := runtimeprotocol.EncodeCommittedRevisionRef(in.BaseRevision); err != nil || in.BaseRevision.DocumentID != in.Session.Scope.DocumentID {
		return contractError(runtimeprotocol.RuntimeFailureCodeRuntimeStaleRevision, "registry base revision is malformed")
	}
	if len(in.RegistryTransactionID) < 16 || len(in.RegistryTransactionID) > 256 || len(in.StagedObjects) == 0 {
		return contractError(runtimeprotocol.RuntimeFailureCodeRuntimeMalformedHandle, "registry transaction binding is malformed")
	}
	for _, digest := range []protocolcommon.Digest{in.PlanDigest, in.MutationDigest, in.ExpectedResolvedLockDigest} {
		if _, err := protocolcommon.EncodeDigest(digest); err != nil {
			return contractError(runtimeprotocol.RuntimeFailureCodeRuntimeMalformedHandle, "registry digest binding is malformed")
		}
	}
	if _, err := semantic.EncodeAuthoringImpact(in.AuthoringImpact); err != nil || in.AuthoringImpact.BaseDefinitionHash != in.BaseRevision.DefinitionHash {
		return contractError(runtimeprotocol.RuntimeFailureCodeRuntimeAuthorizationProofInvalid, "registry AuthoringImpact is malformed")
	}
	if _, err := accessprotocol.EncodeAuthoringDecision(in.ExpectedDecision); err != nil || in.ExpectedDecision.Outcome != accessprotocol.AuthoringDecisionOutcomeAllow {
		return contractError(runtimeprotocol.RuntimeFailureCodeRuntimeAuthorizationProofInvalid, "registry Access decision is malformed")
	}
	seen := map[string]struct{}{}
	for _, object := range in.StagedObjects {
		if object.ObjectID == "" || object.MediaType == "" {
			return contractError(runtimeprotocol.RuntimeFailureCodeRuntimeMalformedHandle, "registry staged object is malformed")
		}
		if _, exists := seen[object.ObjectID]; exists {
			return contractError(runtimeprotocol.RuntimeFailureCodeRuntimeMalformedHandle, "registry staged object is duplicated")
		}
		seen[object.ObjectID] = struct{}{}
		if _, err := protocolcommon.EncodeDigest(object.Digest); err != nil {
			return contractError(runtimeprotocol.RuntimeFailureCodeRuntimeMalformedHandle, "registry staged object digest is malformed")
		}
		if _, err := protocolcommon.EncodeCanonicalUint64(object.Size); err != nil {
			return contractError(runtimeprotocol.RuntimeFailureCodeRuntimeMalformedHandle, "registry staged object size is malformed")
		}
	}
	for _, impact := range in.HostOperationImpacts {
		if _, err := accessprotocol.EncodeHostOperationImpact(impact); err != nil {
			return contractError(runtimeprotocol.RuntimeFailureCodeRuntimeAuthorizationProofInvalid, "registry HostOperationImpact is malformed")
		}
	}
	if in.ProjectMutation.SnapshotHandle == "" {
		return contractError(runtimeprotocol.RuntimeFailureCodeRuntimeMalformedHandle, "registry Engine snapshot handle is missing")
	}
	if _, err := protocolcommon.EncodeDigest(in.ProjectMutation.SourceClosureDigest); err != nil {
		return contractError(runtimeprotocol.RuntimeFailureCodeRuntimeMalformedHandle, "registry source closure digest is malformed")
	}
	objectIDs := make(map[string]bool, len(in.StagedObjects))
	for _, object := range in.StagedObjects {
		objectIDs[object.ObjectID] = true
	}
	for _, artifact := range in.ProjectMutation.Artifacts {
		if !objectIDs[artifact.Object.ObjectID] || artifact.RegistrySource == "" {
			return contractError(runtimeprotocol.RuntimeFailureCodeRuntimeMalformedHandle, "registry project artifact binding is malformed")
		}
	}
	if in.LeaseToken != nil {
		if _, err := runtimeprotocol.EncodeLeaseToken(*in.LeaseToken); err != nil {
			return contractError(runtimeprotocol.RuntimeFailureCodeRuntimeMalformedHandle, "registry lease is malformed")
		}
	}
	if in.CancellationToken != nil {
		if _, err := runtimeprotocol.EncodeCancellationToken(*in.CancellationToken); err != nil {
			return contractError(runtimeprotocol.RuntimeFailureCodeRuntimeMalformedHandle, "registry cancellation token is malformed")
		}
	}
	return nil
}

func (c *Coordinator) commitRegistryPlan(ctx context.Context, in RegistryCommitInput) (runtimeprotocol.RuntimeCommitResult, *ContractError) {
	p := c.runtime.config.Ports
	if p.Registry == nil {
		return runtimeprotocol.RuntimeCommitResult{}, contractError(runtimeprotocol.RuntimeFailureCodeRuntimeCapabilityUnavailable, "registry revision preparer is unavailable")
	}
	s, rejection := c.validatedSession(in.Session)
	if rejection != nil {
		return runtimeprotocol.RuntimeCommitResult{}, rejection
	}
	in.Session = s.binding.Session
	payload := RegistryRetryRequestDigest(in)
	journalCtx := context.WithoutCancel(ctx)
	if existing, err := p.Recovery.Get(journalCtx, port.GetRecoveryRecordInput{Scope: in.Session.Scope, OperationID: &in.OperationID}); err == nil {
		return registryRetryResult(existing, in, payload)
	} else if !errors.Is(err, port.ErrNotFound) {
		return runtimeprotocol.RuntimeCommitResult{}, portFailure("read registry operation journal", err)
	}
	if existing, err := p.Recovery.Get(journalCtx, port.GetRecoveryRecordInput{Scope: in.Session.Scope, IdempotencyKey: &in.IdempotencyKey}); err == nil {
		return registryRetryResult(existing, in, payload)
	} else if !errors.Is(err, port.ErrNotFound) {
		return runtimeprotocol.RuntimeCommitResult{}, portFailure("read registry idempotency journal", err)
	}
	record, err := p.Recovery.CreatePending(journalCtx, port.CreatePendingRecordInput{Scope: in.Session.Scope, OperationID: in.OperationID, IdempotencyKey: in.IdempotencyKey, PayloadDigest: payload, BaseRevision: in.BaseRevision})
	if err != nil {
		if errors.Is(err, port.ErrConflict) {
			return c.resolveRegistryPendingConflict(journalCtx, in, payload)
		}
		return runtimeprotocol.RuntimeCommitResult{}, portFailure("create registry pending operation", err)
	}
	if !validRegistryRecoveryRecord(record, in, payload, runtimeprotocol.RecoveryPhasePending, nil) {
		return runtimeprotocol.RuntimeCommitResult{}, contractError(runtimeprotocol.RuntimeFailureCodeRuntimeCapabilityUnavailable, "registry journal returned an invalid pending reservation")
	}
	if rejection := c.checkRegistryCancellation(ctx, in); rejection != nil {
		return c.finalRegistryRejected(ctx, in, rejection, nil)
	}
	head, err := p.Documents.GetHead(ctx, port.GetDocumentHeadInput{Scope: in.Session.Scope})
	if err != nil || !validDocumentHead(head) {
		return c.abandonRegistryPending(ctx, in, portFailure("read current registry target head", err))
	}
	if !reflect.DeepEqual(head.Revision, in.BaseRevision) {
		return c.finalRegistryConflict(ctx, in, head.Revision)
	}
	if rejection, failure := c.validateRegistryLease(ctx, in, head); failure != nil {
		return c.abandonRegistryPending(ctx, in, failure)
	} else if rejection != nil {
		return c.finalRegistryRejected(ctx, in, rejection, nil)
	}
	prepared, err := p.Registry.PrepareRegistryRevision(ctx, port.PrepareRegistryRevisionInput{Scope: in.Session.Scope, BaseRevision: in.BaseRevision, RegistryTransactionID: in.RegistryTransactionID, PlanDigest: in.PlanDigest, MutationDigest: in.MutationDigest, ExpectedResolvedLockDigest: in.ExpectedResolvedLockDigest, StagedObjects: append([]port.RegistryStagedObjectRef(nil), in.StagedObjects...), ProjectMutation: in.ProjectMutation})
	if err != nil || !validPreparedRevision(prepared, in.BaseRevision) || !reflect.DeepEqual(prepared.AuthoringImpact, in.AuthoringImpact) {
		return c.abandonRegistryPending(ctx, in, contractError(runtimeprotocol.RuntimeFailureCodeRuntimeCapabilityUnavailable, "Engine rejected the registry staged closure"))
	}
	grant, _, err := p.Grants.ResolveGrant(ctx, in.Session.Scope)
	if err != nil {
		return c.abandonRegistryPending(ctx, in, contractError(runtimeprotocol.RuntimeFailureCodeRuntimeAuthorizationStale, "current registry authoring grant could not be resolved"))
	}
	hostImpacts := append(make([]accessprotocol.HostOperationImpact, 0, len(in.HostOperationImpacts)), in.HostOperationImpacts...)
	evaluation := accessprotocol.EvaluateAuthoringInput{AuthoringImpact: &prepared.AuthoringImpact, GrantSnapshot: grant, HostOperationImpacts: hostImpacts, RequestIntent: "apply"}
	decision, rejection := c.runtime.Authorize(ctx, AuthorizationRequest{Scope: in.Session.Scope, CurrentRevision: head.Revision, Evaluation: evaluation})
	if rejection != nil {
		return c.abandonRegistryPending(ctx, in, rejection)
	}
	if decision.DecisionDigest != in.ExpectedDecision.DecisionDigest || decision.EvaluationDigest != in.ExpectedDecision.EvaluationDigest || !reflect.DeepEqual(decision.RequiredCapabilities, in.ExpectedDecision.RequiredCapabilities) {
		return c.abandonRegistryPending(ctx, in, contractError(runtimeprotocol.RuntimeFailureCodeRuntimeAuthorizationStale, "registry Access decision changed before Runtime apply"))
	}
	if rejection := c.checkRegistryCancellation(ctx, in); rejection != nil {
		return c.finalRegistryRejected(ctx, in, rejection, &registryEvaluation{decision, prepared.AuthoringImpact})
	}
	preview := runtimeprotocol.PreviewEvaluation{AuthoringImpact: prepared.AuthoringImpact, AuthoringDecision: decision}
	staged, err := p.Documents.StageRevision(ctx, port.StageRevisionInput{Scope: in.Session.Scope, OperationID: in.OperationID, IdempotencyKey: in.IdempotencyKey, BaseRevision: head.Revision, DefinitionHash: prepared.DefinitionHash, GraphHash: prepared.GraphHash, SourceBlobs: prepared.Sources, Manifest: prepared.Manifest, DecisionDigest: decision.DecisionDigest, EvaluationDigest: decision.EvaluationDigest, Actor: grant.ActorRef, Trigger: runtimeprotocol.CommitTriggerRegistryInstall, CancellationToken: in.CancellationToken, PreviewEvaluation: &preview})
	if err != nil || !validStagedRevision(staged, in.Session.Scope, prepared) {
		if err == nil && staged.StageID != "" {
			_ = p.Documents.AbortStagedRevision(context.WithoutCancel(ctx), port.AbortStagedRevisionInput{Scope: in.Session.Scope, StageID: staged.StageID})
		}
		return c.abandonRegistryPending(ctx, in, contractError(runtimeprotocol.RuntimeFailureCodeRuntimeCapabilityUnavailable, "document store rejected the registry revision stage"))
	}
	cleanup := prePublicationCleanup{ports: p, scope: in.Session.Scope, documentStage: staged.StageID}
	defer cleanup.run()
	if _, rejection := c.advanceRegistry(ctx, in, runtimeprotocol.RecoveryPhasePending, runtimeprotocol.RecoveryPhaseStaged, nil, &decision, &preview); rejection != nil {
		return runtimeprotocol.RuntimeCommitResult{}, rejection
	}
	if rejection, failure := c.validateRegistryLease(ctx, in, head); failure != nil {
		return runtimeprotocol.RuntimeCommitResult{}, failure
	} else if rejection != nil {
		return c.finalRegistryRejected(ctx, in, rejection, &registryEvaluation{decision, prepared.AuthoringImpact})
	}
	release, err := p.Grants.AcquireAuthoringPublication(ctx, in.Session.Scope)
	if err != nil || release == nil {
		return c.finalRegistryRejected(ctx, in, contractError(runtimeprotocol.RuntimeFailureCodeRuntimeAuthorizationStale, "registry publication fence could not be acquired"), &registryEvaluation{decision, prepared.AuthoringImpact})
	}
	defer release()
	currentGrant, _, err := p.Grants.ResolveGrant(ctx, in.Session.Scope)
	if err != nil {
		return c.finalRegistryRejected(ctx, in, contractError(runtimeprotocol.RuntimeFailureCodeRuntimeAuthorizationStale, "registry grant changed before publication"), &registryEvaluation{decision, prepared.AuthoringImpact})
	}
	publicationEvaluation := accessprotocol.EvaluateAuthoringInput{AuthoringImpact: &prepared.AuthoringImpact, GrantSnapshot: currentGrant, HostOperationImpacts: hostImpacts, RequestIntent: "publish"}
	publicationDecision, rejection := c.runtime.Authorize(ctx, AuthorizationRequest{Scope: in.Session.Scope, CurrentRevision: head.Revision, Evaluation: publicationEvaluation})
	if rejection != nil || publicationDecision.AccessFingerprint != decision.AccessFingerprint || !reflect.DeepEqual(publicationDecision.RequiredCapabilities, decision.RequiredCapabilities) {
		if rejection == nil {
			rejection = contractError(runtimeprotocol.RuntimeFailureCodeRuntimeAuthorizationStale, "registry authoring decision changed before publication")
		}
		return c.finalRegistryRejected(ctx, in, rejection, &registryEvaluation{decision, prepared.AuthoringImpact})
	}
	if _, rejection := c.advanceRegistry(ctx, in, runtimeprotocol.RecoveryPhaseStaged, runtimeprotocol.RecoveryPhasePublicationPending, nil, &decision, nil); rejection != nil {
		return runtimeprotocol.RuntimeCommitResult{}, rejection
	}
	if rejection := c.beginRegistryPublication(ctx, in); rejection != nil {
		return c.finalRegistryRejected(ctx, in, rejection, &registryEvaluation{decision, prepared.AuthoringImpact})
	}
	published, publishErr := p.Documents.PublishHead(ctx, port.PublishDocumentHeadInput{Scope: in.Session.Scope, StageID: staged.StageID, ExpectedRevision: head.Revision.RevisionID, ExpectedDefinitionHash: head.Revision.DefinitionHash, ExpectedProviderVersion: head.ProviderVersion, FencingToken: head.FencingToken})
	if publishErr != nil || !published.Published || !samePublishedRevision(published.Revision, staged.Revision) {
		observed, observeErr := p.Documents.GetHead(context.WithoutCancel(ctx), port.GetDocumentHeadInput{Scope: in.Session.Scope})
		if observeErr == nil && validDocumentHead(observed) && samePublishedRevision(observed.Revision, staged.Revision) {
			published = port.PublishHeadResult{Published: true, Revision: observed.Revision, ProviderVersion: observed.ProviderVersion}
		} else if observeErr == nil && validDocumentHead(observed) && (errors.Is(publishErr, port.ErrConflict) || !reflect.DeepEqual(observed.Revision, head.Revision)) {
			return c.finalRegistryConflictWithEvaluation(ctx, in, observed.Revision, decision, prepared.AuthoringImpact)
		} else {
			cleanup.retired = true
			_, _ = c.advanceRegistry(context.WithoutCancel(ctx), in, runtimeprotocol.RecoveryPhasePublicationPending, runtimeprotocol.RecoveryPhaseRecovering, nil, &decision, nil)
			return c.finalRegistryNeedsReview(ctx, in, decision, prepared.AuthoringImpact)
		}
	}
	cleanup.retired = true
	return c.afterRegistryPublication(ctx, in, s, staged.StageID, prepared, decision, published.Revision)
}

type registryEvaluation struct {
	decision accessprotocol.AuthoringDecision
	impact   semantic.AuthoringImpact
}

// RegistryRetryRequestDigest is the portable idempotency identity. It excludes
// process-local session generation while binding every plan, staged object,
// impact, and publication precondition.
func RegistryRetryRequestDigest(in RegistryCommitInput) protocolcommon.Digest {
	projection := struct {
		DocumentID                 runtimeprotocol.DocumentID           `json:"document_id"`
		OperationID                runtimeprotocol.OperationID          `json:"operation_id"`
		BaseRevision               runtimeprotocol.CommittedRevisionRef `json:"base_revision"`
		RegistryTransactionID      string                               `json:"registry_transaction_id"`
		PlanDigest                 protocolcommon.Digest                `json:"plan_digest"`
		MutationDigest             protocolcommon.Digest                `json:"mutation_digest"`
		ExpectedResolvedLockDigest protocolcommon.Digest                `json:"expected_resolved_lock_digest"`
		StagedObjects              []port.RegistryStagedObjectRef       `json:"staged_objects"`
		AuthoringImpact            semantic.AuthoringImpact             `json:"authoring_impact"`
		HostOperationImpacts       []accessprotocol.HostOperationImpact `json:"host_operation_impacts"`
		DecisionDigest             protocolcommon.Digest                `json:"decision_digest"`
		EvaluationDigest           protocolcommon.Digest                `json:"evaluation_digest"`
		ProjectMutation            port.RegistryProjectMutation         `json:"project_mutation"`
	}{in.Session.Scope.DocumentID, in.OperationID, in.BaseRevision, in.RegistryTransactionID, in.PlanDigest, in.MutationDigest, in.ExpectedResolvedLockDigest, in.StagedObjects, in.AuthoringImpact, in.HostOperationImpacts, in.ExpectedDecision.DecisionDigest, in.ExpectedDecision.EvaluationDigest, in.ProjectMutation}
	return digestValue(projection)
}

func registryRetryResult(record port.RecoveryRecord, in RegistryCommitInput, payload protocolcommon.Digest) (runtimeprotocol.RuntimeCommitResult, *ContractError) {
	if !validRecoveryRecord(record) || !validRegistryRecoveryIdentity(record, in, payload) {
		return runtimeprotocol.RuntimeCommitResult{}, contractError(runtimeprotocol.RuntimeFailureCodeRuntimeIdempotencyMismatch, "registry operation identity was reused")
	}
	if record.Status.OperationResult == nil || record.CommitResult == nil {
		return runtimeprotocol.RuntimeCommitResult{}, contractError(runtimeprotocol.RuntimeFailureCodeRuntimeCapabilityUnavailable, "registry operation is still pending; query its status")
	}
	return *record.CommitResult, nil
}

func validRegistryRecoveryIdentity(record port.RecoveryRecord, in RegistryCommitInput, payload protocolcommon.Digest) bool {
	return reflect.DeepEqual(record.Scope, in.Session.Scope) && record.Status.OperationID == in.OperationID && record.Status.IdempotencyKey == in.IdempotencyKey && record.PayloadDigest == payload && reflect.DeepEqual(record.BaseRevision, in.BaseRevision)
}

func validRegistryRecoveryRecord(record port.RecoveryRecord, in RegistryCommitInput, payload protocolcommon.Digest, phase runtimeprotocol.RecoveryPhase, decision *accessprotocol.AuthoringDecision) bool {
	if !validRecoveryRecord(record) || !validRegistryRecoveryIdentity(record, in, payload) || record.Status.Phase != phase {
		return false
	}
	if decision == nil {
		return record.DecisionDigest == nil && record.EvaluationDigest == nil
	}
	return record.DecisionDigest != nil && record.EvaluationDigest != nil && *record.DecisionDigest == decision.DecisionDigest && *record.EvaluationDigest == decision.EvaluationDigest
}

func (c *Coordinator) resolveRegistryPendingConflict(ctx context.Context, in RegistryCommitInput, payload protocolcommon.Digest) (runtimeprotocol.RuntimeCommitResult, *ContractError) {
	for _, lookup := range []port.GetRecoveryRecordInput{{Scope: in.Session.Scope, OperationID: &in.OperationID}, {Scope: in.Session.Scope, IdempotencyKey: &in.IdempotencyKey}} {
		record, err := c.runtime.config.Ports.Recovery.Get(ctx, lookup)
		if err == nil {
			return registryRetryResult(record, in, payload)
		}
		if !errors.Is(err, port.ErrNotFound) {
			return runtimeprotocol.RuntimeCommitResult{}, portFailure("resolve registry journal conflict", err)
		}
	}
	return runtimeprotocol.RuntimeCommitResult{}, contractError(runtimeprotocol.RuntimeFailureCodeRuntimeCapabilityUnavailable, "registry journal conflict has no authoritative record")
}

func (c *Coordinator) advanceRegistry(ctx context.Context, in RegistryCommitInput, from, to runtimeprotocol.RecoveryPhase, revision *runtimeprotocol.CommittedRevisionRef, decision *accessprotocol.AuthoringDecision, preview *runtimeprotocol.PreviewEvaluation) (port.RecoveryRecord, *ContractError) {
	if rejection := ValidateRecoveryTransition(from, to); rejection != nil {
		return port.RecoveryRecord{}, rejection
	}
	request := port.AdvanceRecoveryRecordInput{Scope: in.Session.Scope, OperationID: in.OperationID, ExpectedPhase: from, NextPhase: to, PublishedRevision: revision, PreviewEvaluation: preview}
	if decision != nil && preview != nil {
		request.DecisionDigest = &decision.DecisionDigest
		request.EvaluationDigest = &decision.EvaluationDigest
	}
	record, err := c.runtime.config.Ports.Recovery.Advance(context.WithoutCancel(ctx), request)
	if err != nil {
		return port.RecoveryRecord{}, portFailure("advance registry operation journal", err)
	}
	if !validRegistryRecoveryRecord(record, in, RegistryRetryRequestDigest(in), to, decision) {
		return port.RecoveryRecord{}, contractError(runtimeprotocol.RuntimeFailureCodeRuntimeCapabilityUnavailable, "registry journal returned an invalid transition")
	}
	return record, nil
}

func (c *Coordinator) checkRegistryCancellation(ctx context.Context, in RegistryCommitInput) *ContractError {
	shim := runtimeprotocol.RuntimeCommitInput{Session: in.Session, OperationID: in.OperationID, CancellationToken: in.CancellationToken}
	return c.checkCancellation(ctx, shim)
}

func (c *Coordinator) beginRegistryPublication(ctx context.Context, in RegistryCommitInput) *ContractError {
	shim := runtimeprotocol.RuntimeCommitInput{Session: in.Session, OperationID: in.OperationID, CancellationToken: in.CancellationToken}
	return c.beginPublication(ctx, shim)
}

func (c *Coordinator) validateRegistryLease(ctx context.Context, in RegistryCommitInput, head port.DocumentHead) (*ContractError, *ContractError) {
	shim := runtimeprotocol.RuntimeCommitInput{Session: in.Session, LeaseToken: in.LeaseToken}
	return c.validateLease(ctx, shim, head)
}

func registryOperationResult(in RegistryCommitInput, status runtimeprotocol.OperationResultStatus, revision *runtimeprotocol.CommittedRevisionRef, state *protocolcommon.CanonicalNonNegativeInt64, failure runtimeprotocol.RuntimeFailureCode) runtimeprotocol.OperationResult {
	shim := runtimeprotocol.RuntimeCommitInput{OperationID: in.OperationID, IdempotencyKey: in.IdempotencyKey}
	return operationResult(shim, status, revision, state, failure)
}

func (c *Coordinator) finalizeRegistry(ctx context.Context, in RegistryCommitInput, result runtimeprotocol.RuntimeCommitResult, phase runtimeprotocol.RecoveryPhase) *ContractError {
	record, err := c.runtime.config.Ports.Recovery.Finalize(context.WithoutCancel(ctx), port.FinalizeRecoveryRecordInput{Scope: in.Session.Scope, CommitResult: result, TerminalPhase: phase})
	if err != nil {
		return portFailure("finalize registry operation", err)
	}
	var decision *accessprotocol.AuthoringDecision
	if result.PreviewEvaluation != nil {
		decision = &result.PreviewEvaluation.AuthoringDecision
	}
	if !validRegistryRecoveryRecord(record, in, RegistryRetryRequestDigest(in), phase, decision) || record.CommitResult == nil || !reflect.DeepEqual(*record.CommitResult, result) {
		return contractError(runtimeprotocol.RuntimeFailureCodeRuntimeCapabilityUnavailable, "registry journal returned an invalid terminal record")
	}
	return nil
}

func (c *Coordinator) finalRegistryRejected(ctx context.Context, in RegistryCommitInput, rejection *ContractError, evaluation *registryEvaluation) (runtimeprotocol.RuntimeCommitResult, *ContractError) {
	result := runtimeprotocol.RuntimeCommitResult{OperationResult: registryOperationResult(in, runtimeprotocol.OperationResultStatusRejected, nil, nil, rejection.Code)}
	if evaluation != nil {
		result.PreviewEvaluation = &runtimeprotocol.PreviewEvaluation{AuthoringImpact: evaluation.impact, AuthoringDecision: evaluation.decision}
	}
	if final := c.finalizeRegistry(ctx, in, result, runtimeprotocol.RecoveryPhaseFinal); final != nil {
		return runtimeprotocol.RuntimeCommitResult{}, final
	}
	return result, nil
}

func (c *Coordinator) finalRegistryConflict(ctx context.Context, in RegistryCommitInput, current runtimeprotocol.CommittedRevisionRef) (runtimeprotocol.RuntimeCommitResult, *ContractError) {
	result := runtimeprotocol.RuntimeCommitResult{OperationResult: registryOperationResult(in, runtimeprotocol.OperationResultStatusRejected, nil, nil, runtimeprotocol.RuntimeFailureCodeRuntimeStaleRevision)}
	result.OperationResult.ConflictEvidence = &runtimeprotocol.ConflictEvidence{CurrentHead: current}
	result.OperationResult.ResultDigest = digestOperationResult(result.OperationResult)
	if final := c.finalizeRegistry(ctx, in, result, runtimeprotocol.RecoveryPhaseFinal); final != nil {
		return runtimeprotocol.RuntimeCommitResult{}, final
	}
	return result, nil
}

func (c *Coordinator) finalRegistryConflictWithEvaluation(ctx context.Context, in RegistryCommitInput, current runtimeprotocol.CommittedRevisionRef, decision accessprotocol.AuthoringDecision, impact semantic.AuthoringImpact) (runtimeprotocol.RuntimeCommitResult, *ContractError) {
	result := runtimeprotocol.RuntimeCommitResult{OperationResult: registryOperationResult(in, runtimeprotocol.OperationResultStatusRejected, nil, nil, runtimeprotocol.RuntimeFailureCodeRuntimeStaleRevision), PreviewEvaluation: &runtimeprotocol.PreviewEvaluation{AuthoringImpact: impact, AuthoringDecision: decision}}
	result.OperationResult.ConflictEvidence = &runtimeprotocol.ConflictEvidence{CurrentHead: current}
	result.OperationResult.ResultDigest = digestOperationResult(result.OperationResult)
	if final := c.finalizeRegistry(ctx, in, result, runtimeprotocol.RecoveryPhaseFinal); final != nil {
		return runtimeprotocol.RuntimeCommitResult{}, final
	}
	return result, nil
}

func (c *Coordinator) finalRegistryNeedsReview(ctx context.Context, in RegistryCommitInput, decision accessprotocol.AuthoringDecision, impact semantic.AuthoringImpact) (runtimeprotocol.RuntimeCommitResult, *ContractError) {
	result := runtimeprotocol.RuntimeCommitResult{OperationResult: registryOperationResult(in, runtimeprotocol.OperationResultStatusNeedsReview, nil, nil, ""), PreviewEvaluation: &runtimeprotocol.PreviewEvaluation{AuthoringImpact: impact, AuthoringDecision: decision}}
	if final := c.finalizeRegistry(ctx, in, result, runtimeprotocol.RecoveryPhaseNeedsReview); final != nil {
		return runtimeprotocol.RuntimeCommitResult{}, final
	}
	return result, nil
}

func (c *Coordinator) abandonRegistryPending(ctx context.Context, in RegistryCommitInput, rejection *ContractError) (runtimeprotocol.RuntimeCommitResult, *ContractError) {
	err := c.runtime.config.Ports.Recovery.AbandonPending(context.WithoutCancel(ctx), port.AbandonPendingRecordInput{Scope: in.Session.Scope, OperationID: in.OperationID, IdempotencyKey: in.IdempotencyKey, PayloadDigest: RegistryRetryRequestDigest(in)})
	if err != nil {
		return runtimeprotocol.RuntimeCommitResult{}, portFailure("abandon registry pending operation", err)
	}
	c.finishOperation(in.Session, in.OperationID)
	return runtimeprotocol.RuntimeCommitResult{}, rejection
}

func (c *Coordinator) afterRegistryPublication(ctx context.Context, in RegistryCommitInput, s sessionState, stageID string, prepared port.PreparedRevision, decision accessprotocol.AuthoringDecision, revision runtimeprotocol.CommittedRevisionRef) (runtimeprotocol.RuntimeCommitResult, *ContractError) {
	p := c.runtime.config.Ports
	for _, transition := range [][2]runtimeprotocol.RecoveryPhase{{runtimeprotocol.RecoveryPhasePublicationPending, runtimeprotocol.RecoveryPhasePublished}, {runtimeprotocol.RecoveryPhasePublished, runtimeprotocol.RecoveryPhaseStatePending}, {runtimeprotocol.RecoveryPhaseStatePending, runtimeprotocol.RecoveryPhaseAuditPending}} {
		if _, rejection := c.advanceRegistry(context.WithoutCancel(ctx), in, transition[0], transition[1], &revision, &decision, nil); rejection != nil {
			stateVersion := s.state.StateVersion
			result := runtimeprotocol.RuntimeCommitResult{OperationResult: registryOperationResult(in, runtimeprotocol.OperationResultStatusCommittedStateStale, &revision, &stateVersion, ""), PreviewEvaluation: &runtimeprotocol.PreviewEvaluation{AuthoringImpact: prepared.AuthoringImpact, AuthoringDecision: decision}}
			return result, nil
		}
	}
	metadata := runtimeprotocol.RevisionMetadata{Revision: revision, ParentRevisionID: &in.BaseRevision.RevisionID, OperationID: in.OperationID, Trigger: runtimeprotocol.CommitTriggerRegistryInstall, AuthoringDecisionDigest: decision.DecisionDigest, CommittedAt: protocolcommon.Rfc3339Time(p.Clock.Now().UTC().Format(time.RFC3339Nano))}
	status := runtimeprotocol.OperationResultStatusCommitted
	if _, err := p.History.AppendRevision(context.WithoutCancel(ctx), port.AppendRevisionInput{Scope: in.Session.Scope, Metadata: metadata}); err != nil {
		status = runtimeprotocol.OperationResultStatusCommittedStateStale
	}
	if _, rejection := c.advanceRegistry(context.WithoutCancel(ctx), in, runtimeprotocol.RecoveryPhaseAuditPending, runtimeprotocol.RecoveryPhaseOutboxReady, &revision, &decision, nil); rejection != nil {
		status = runtimeprotocol.OperationResultStatusCommittedStateStale
	}
	stateVersion := s.state.StateVersion
	result := runtimeprotocol.RuntimeCommitResult{OperationResult: registryOperationResult(in, status, &revision, &stateVersion, ""), PreviewEvaluation: &runtimeprotocol.PreviewEvaluation{AuthoringImpact: prepared.AuthoringImpact, AuthoringDecision: decision}}
	if final := c.finalizeRegistry(ctx, in, result, runtimeprotocol.RecoveryPhaseFinal); final != nil {
		return runtimeprotocol.RuntimeCommitResult{}, final
	}
	_ = p.Documents.AbortStagedRevision(context.WithoutCancel(ctx), port.AbortStagedRevisionInput{Scope: in.Session.Scope, StageID: stageID})
	shim := runtimeprotocol.RuntimeCommitInput{Session: in.Session, OperationID: in.OperationID, IdempotencyKey: in.IdempotencyKey}
	c.checkpointPublished(shim, s, prepared, revision, s.state)
	return result, nil
}

var _ RegistryCommitOperation = (*Coordinator)(nil)
