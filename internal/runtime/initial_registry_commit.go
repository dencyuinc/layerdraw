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

// InitialRegistryCommitInput publishes a template into a reserved Document
// with no existing head. BaselineRevision is an Engine-owned semantic baseline
// used only for impact and journal identity; it is never passed as a store head.
type InitialRegistryCommitInput struct {
	DocumentID                 runtimeprotocol.DocumentID
	OperationID                runtimeprotocol.OperationID
	IdempotencyKey             runtimeprotocol.IdempotencyKey
	BaselineRevision           runtimeprotocol.CommittedRevisionRef
	RegistryTransactionID      string
	PlanDigest                 protocolcommon.Digest
	MutationDigest             protocolcommon.Digest
	ExpectedResolvedLockDigest protocolcommon.Digest
	StagedObjects              []port.RegistryStagedObjectRef
	AuthoringImpact            semantic.AuthoringImpact
	HostOperationImpacts       []accessprotocol.HostOperationImpact
	ExpectedDecision           accessprotocol.AuthoringDecision
	ProjectMutation            port.RegistryProjectMutation
}

func (c *Coordinator) CommitInitialRegistryTemplate(ctx context.Context, input InitialRegistryCommitInput) (runtimeprotocol.RuntimeCommitResult, *ContractError) {
	p := c.runtime.config.Ports
	if p.Registry == nil || p.InitialRegistry == nil {
		return runtimeprotocol.RuntimeCommitResult{}, contractError(runtimeprotocol.RuntimeFailureCodeRuntimeCapabilityUnavailable, "initial Registry publication ports are unavailable")
	}
	scope, err := p.Scopes.ResolveScope(ctx, input.DocumentID)
	if err != nil || scope.DocumentID != input.DocumentID {
		return runtimeprotocol.RuntimeCommitResult{}, contractError(runtimeprotocol.RuntimeFailureCodeRuntimeAuthorizationStale, "initial Registry document scope could not be resolved")
	}
	shim := initialRegistryShim(input, scope)
	if rejection := validateInitialRegistryCommitInput(input, shim); rejection != nil {
		return runtimeprotocol.RuntimeCommitResult{}, rejection
	}
	payload := RegistryRetryRequestDigest(shim)
	journalCtx := context.WithoutCancel(ctx)
	if existing, getErr := p.Recovery.Get(journalCtx, port.GetRecoveryRecordInput{Scope: scope, OperationID: &input.OperationID}); getErr == nil {
		return registryRetryResult(existing, shim, payload)
	} else if !errors.Is(getErr, port.ErrNotFound) {
		return runtimeprotocol.RuntimeCommitResult{}, portFailure("read initial Registry journal", getErr)
	}
	if existing, getErr := p.Recovery.Get(journalCtx, port.GetRecoveryRecordInput{Scope: scope, IdempotencyKey: &input.IdempotencyKey}); getErr == nil {
		return registryRetryResult(existing, shim, payload)
	} else if !errors.Is(getErr, port.ErrNotFound) {
		return runtimeprotocol.RuntimeCommitResult{}, portFailure("read initial Registry idempotency journal", getErr)
	}
	record, err := p.Recovery.CreatePending(journalCtx, port.CreatePendingRecordInput{Scope: scope, OperationID: input.OperationID, IdempotencyKey: input.IdempotencyKey, PayloadDigest: payload, BaseRevision: input.BaselineRevision})
	if err != nil {
		if errors.Is(err, port.ErrConflict) {
			return c.resolveRegistryPendingConflict(journalCtx, shim, payload)
		}
		return runtimeprotocol.RuntimeCommitResult{}, portFailure("create initial Registry reservation", err)
	}
	if !validRegistryRecoveryRecord(record, shim, payload, runtimeprotocol.RecoveryPhasePending, nil) {
		return runtimeprotocol.RuntimeCommitResult{}, contractError(runtimeprotocol.RuntimeFailureCodeRuntimeCapabilityUnavailable, "initial Registry journal reservation is invalid")
	}
	if head, headErr := p.Documents.GetHead(ctx, port.GetDocumentHeadInput{Scope: scope}); headErr == nil && validDocumentHead(head) {
		return c.finalRegistryConflict(ctx, shim, head.Revision)
	} else if headErr != nil && !errors.Is(headErr, port.ErrNotFound) {
		return c.abandonRegistryPending(ctx, shim, portFailure("inspect initial Registry head", headErr))
	}
	prepared, err := p.Registry.PrepareInitialRegistryRevision(ctx, port.PrepareInitialRegistryRevisionInput{Scope: scope, BaselineRevision: input.BaselineRevision, RegistryTransactionID: input.RegistryTransactionID, PlanDigest: input.PlanDigest, MutationDigest: input.MutationDigest, ExpectedResolvedLockDigest: input.ExpectedResolvedLockDigest, StagedObjects: append([]port.RegistryStagedObjectRef(nil), input.StagedObjects...), ProjectMutation: input.ProjectMutation})
	if err != nil || !validPreparedRevision(prepared, input.BaselineRevision) || !reflect.DeepEqual(prepared.AuthoringImpact, input.AuthoringImpact) {
		return c.abandonRegistryPending(ctx, shim, contractError(runtimeprotocol.RuntimeFailureCodeRuntimeCapabilityUnavailable, "Engine rejected the initial Registry closure"))
	}
	grant, _, err := p.Grants.ResolveGrant(ctx, scope)
	if err != nil {
		return c.abandonRegistryPending(ctx, shim, contractError(runtimeprotocol.RuntimeFailureCodeRuntimeAuthorizationStale, "initial Registry grant could not be resolved"))
	}
	hostImpacts := append(make([]accessprotocol.HostOperationImpact, 0, len(input.HostOperationImpacts)), input.HostOperationImpacts...)
	evaluation := accessprotocol.EvaluateAuthoringInput{AuthoringImpact: &prepared.AuthoringImpact, GrantSnapshot: grant, HostOperationImpacts: hostImpacts, RequestIntent: "apply"}
	decision, rejection := c.runtime.Authorize(ctx, AuthorizationRequest{Scope: scope, CurrentRevision: input.BaselineRevision, Evaluation: evaluation})
	if rejection != nil || decision.DecisionDigest != input.ExpectedDecision.DecisionDigest || decision.EvaluationDigest != input.ExpectedDecision.EvaluationDigest {
		if rejection == nil {
			rejection = contractError(runtimeprotocol.RuntimeFailureCodeRuntimeAuthorizationStale, "initial Registry decision changed before apply")
		}
		return c.abandonRegistryPending(ctx, shim, rejection)
	}
	preview := runtimeprotocol.PreviewEvaluation{AuthoringImpact: prepared.AuthoringImpact, AuthoringDecision: decision}
	if _, rejection := c.advanceRegistry(ctx, shim, runtimeprotocol.RecoveryPhasePending, runtimeprotocol.RecoveryPhaseStaged, nil, &decision, &preview); rejection != nil {
		return runtimeprotocol.RuntimeCommitResult{}, rejection
	}
	release, err := p.Grants.AcquireAuthoringPublication(ctx, scope)
	if err != nil || release == nil {
		return c.finalRegistryRejected(ctx, shim, contractError(runtimeprotocol.RuntimeFailureCodeRuntimeAuthorizationStale, "initial Registry publication fence is unavailable"), &registryEvaluation{decision, prepared.AuthoringImpact})
	}
	defer release()
	currentGrant, _, err := p.Grants.ResolveGrant(ctx, scope)
	if err != nil {
		return c.finalRegistryRejected(ctx, shim, contractError(runtimeprotocol.RuntimeFailureCodeRuntimeAuthorizationStale, "initial Registry grant changed before publication"), &registryEvaluation{decision, prepared.AuthoringImpact})
	}
	publishDecision, rejection := c.runtime.Authorize(ctx, AuthorizationRequest{Scope: scope, CurrentRevision: input.BaselineRevision, Evaluation: accessprotocol.EvaluateAuthoringInput{AuthoringImpact: &prepared.AuthoringImpact, GrantSnapshot: currentGrant, HostOperationImpacts: hostImpacts, RequestIntent: "publish"}})
	if rejection != nil || publishDecision.AccessFingerprint != decision.AccessFingerprint || !reflect.DeepEqual(publishDecision.RequiredCapabilities, decision.RequiredCapabilities) {
		if rejection == nil {
			rejection = contractError(runtimeprotocol.RuntimeFailureCodeRuntimeAuthorizationStale, "initial Registry decision changed before publication")
		}
		return c.finalRegistryRejected(ctx, shim, rejection, &registryEvaluation{decision, prepared.AuthoringImpact})
	}
	if _, rejection := c.advanceRegistry(ctx, shim, runtimeprotocol.RecoveryPhaseStaged, runtimeprotocol.RecoveryPhasePublicationPending, nil, &decision, nil); rejection != nil {
		return runtimeprotocol.RuntimeCommitResult{}, rejection
	}
	revision, publishErr := p.InitialRegistry.PublishInitialRegistryRevision(ctx, port.PublishInitialRegistryRevisionInput{Scope: scope, OperationID: input.OperationID, IdempotencyKey: input.IdempotencyKey, Prepared: prepared, DecisionDigest: decision.DecisionDigest, EvaluationDigest: decision.EvaluationDigest, Actor: grant.ActorRef, Trigger: runtimeprotocol.CommitTriggerRegistryInstall, PreviewEvaluation: preview})
	if publishErr != nil || !validInitialRegistryRevision(revision, scope, prepared) {
		head, headErr := p.Documents.GetHead(context.WithoutCancel(ctx), port.GetDocumentHeadInput{Scope: scope})
		if headErr != nil || !validInitialRegistryRevision(head.Revision, scope, prepared) {
			_, _ = c.advanceRegistry(context.WithoutCancel(ctx), shim, runtimeprotocol.RecoveryPhasePublicationPending, runtimeprotocol.RecoveryPhaseRecovering, nil, &decision, nil)
			return c.finalRegistryNeedsReview(ctx, shim, decision, prepared.AuthoringImpact)
		}
		revision = head.Revision
	}
	for _, transition := range [][2]runtimeprotocol.RecoveryPhase{{runtimeprotocol.RecoveryPhasePublicationPending, runtimeprotocol.RecoveryPhasePublished}, {runtimeprotocol.RecoveryPhasePublished, runtimeprotocol.RecoveryPhaseStatePending}, {runtimeprotocol.RecoveryPhaseStatePending, runtimeprotocol.RecoveryPhaseAuditPending}} {
		if _, rejection := c.advanceRegistry(context.WithoutCancel(ctx), shim, transition[0], transition[1], &revision, &decision, nil); rejection != nil {
			return runtimeprotocol.RuntimeCommitResult{}, rejection
		}
	}
	metadata := runtimeprotocol.RevisionMetadata{Revision: revision, OperationID: input.OperationID, Trigger: runtimeprotocol.CommitTriggerRegistryInstall, AuthoringDecisionDigest: decision.DecisionDigest, CommittedAt: protocolcommon.Rfc3339Time(p.Clock.Now().UTC().Format(time.RFC3339Nano))}
	status := runtimeprotocol.OperationResultStatusCommitted
	if _, err := p.History.AppendRevision(context.WithoutCancel(ctx), port.AppendRevisionInput{Scope: scope, Metadata: metadata}); err != nil {
		status = runtimeprotocol.OperationResultStatusCommittedStateStale
	}
	if _, rejection := c.advanceRegistry(context.WithoutCancel(ctx), shim, runtimeprotocol.RecoveryPhaseAuditPending, runtimeprotocol.RecoveryPhaseOutboxReady, &revision, &decision, nil); rejection != nil {
		status = runtimeprotocol.OperationResultStatusCommittedStateStale
	}
	var stateVersion *protocolcommon.CanonicalNonNegativeInt64
	if state, stateErr := p.State.GetHead(context.WithoutCancel(ctx), port.GetStateHeadInput{Scope: scope}); stateErr == nil && validStateHead(state) && state.DefinitionHash == revision.DefinitionHash && state.GraphHash == revision.GraphHash {
		stateVersion = &state.StateVersion
	} else {
		status = runtimeprotocol.OperationResultStatusCommittedStateStale
	}
	result := runtimeprotocol.RuntimeCommitResult{OperationResult: registryOperationResult(shim, status, &revision, stateVersion, ""), PreviewEvaluation: &preview}
	if final := c.finalizeRegistry(ctx, shim, result, runtimeprotocol.RecoveryPhaseFinal); final != nil {
		return runtimeprotocol.RuntimeCommitResult{}, final
	}
	return result, nil
}

func initialRegistryShim(in InitialRegistryCommitInput, scope runtimeprotocol.RuntimeScope) RegistryCommitInput {
	return RegistryCommitInput{Session: runtimeprotocol.RuntimeSessionRef{RuntimeSessionID: runtimeprotocol.RuntimeSessionID("registry_initial_" + string(in.OperationID)), SessionGeneration: "1", Scope: scope}, OperationID: in.OperationID, IdempotencyKey: in.IdempotencyKey, BaseRevision: in.BaselineRevision, RegistryTransactionID: in.RegistryTransactionID, PlanDigest: in.PlanDigest, MutationDigest: in.MutationDigest, ExpectedResolvedLockDigest: in.ExpectedResolvedLockDigest, StagedObjects: in.StagedObjects, AuthoringImpact: in.AuthoringImpact, HostOperationImpacts: in.HostOperationImpacts, ExpectedDecision: in.ExpectedDecision, ProjectMutation: in.ProjectMutation}
}

func validateInitialRegistryCommitInput(in InitialRegistryCommitInput, shim RegistryCommitInput) *ContractError {
	if in.BaselineRevision.DocumentID != in.DocumentID {
		return contractError(runtimeprotocol.RuntimeFailureCodeRuntimeStaleRevision, "initial Registry baseline document is malformed")
	}
	return validateRegistryCommitInput(shim)
}

func validInitialRegistryRevision(revision runtimeprotocol.CommittedRevisionRef, scope runtimeprotocol.RuntimeScope, prepared port.PreparedRevision) bool {
	return revision.DocumentID == scope.DocumentID && revision.DefinitionHash == prepared.DefinitionHash && revision.GraphHash == prepared.GraphHash && revision.RevisionID != ""
}

var _ InitialRegistryCommitOperation = (*Coordinator)(nil)
