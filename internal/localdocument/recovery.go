// SPDX-License-Identifier: LicenseRef-LayerDraw-1.0

package localdocument

import (
	"context"
	"errors"
	"reflect"

	"github.com/dencyuinc/layerdraw/gen/go/accessprotocol"
	"github.com/dencyuinc/layerdraw/gen/go/protocolcommon"
	"github.com/dencyuinc/layerdraw/gen/go/runtimeprotocol"
	accesscore "github.com/dencyuinc/layerdraw/internal/access"
	"github.com/dencyuinc/layerdraw/internal/adapter/local"
	runtimehost "github.com/dencyuinc/layerdraw/internal/runtime"
	"github.com/dencyuinc/layerdraw/internal/runtime/port"
)

type RecoveryResult struct {
	OperationID runtimeprotocol.OperationID
	Status      runtimeprotocol.RuntimeOperationStatus
	Converged   bool
}

// Recover reconciles bounded durable evidence and finalizes every operation it
// can prove. Ambiguous publication is never guessed; it converges to
// needs_review. The method is restart-safe and idempotent.
func (h *Host) Recover(ctx context.Context, documentID runtimeprotocol.DocumentID) ([]RecoveryResult, error) {
	scope := h.authority.add(documentID)
	records, err := h.recovery.List(ctx, scope, h.config.MaxRecoveryItems)
	if err != nil {
		return nil, err
	}
	stages, err := h.documents.ListStaged(ctx, scope, h.config.MaxRecoveryItems)
	if err != nil {
		return nil, err
	}
	stageByOperation := map[runtimeprotocol.OperationID]local.StagedInspection{}
	for _, stage := range stages {
		stageByOperation[stage.Input.OperationID] = stage
	}
	results := make([]RecoveryResult, 0, len(records))
	active := map[runtimeprotocol.OperationID]bool{}
	for _, inspection := range records {
		record := inspection.Record
		phase := record.Status.Phase
		if phase == runtimeprotocol.RecoveryPhaseFinal {
			results = append(results, RecoveryResult{OperationID: record.Status.OperationID, Status: record.Status, Converged: true})
			continue
		}
		active[record.Status.OperationID] = true
		if phase == runtimeprotocol.RecoveryPhaseNeedsReview {
			results = append(results, RecoveryResult{OperationID: record.Status.OperationID, Status: record.Status, Converged: true})
			continue
		}
		stage, hasStage := stageByOperation[record.Status.OperationID]
		var status runtimeprotocol.RuntimeOperationStatus
		switch phase {
		case runtimeprotocol.RecoveryPhasePending:
			status, err = h.finalizeRecovered(ctx, scope, record, nil, runtimeprotocol.OperationResultStatusRejected, nil, runtimeprotocol.RuntimeFailureCodeRuntimeCancelled, runtimeprotocol.RecoveryPhaseFinal)
		case runtimeprotocol.RecoveryPhaseStaged:
			if hasStage {
				_ = h.documents.AbortStagedRevision(ctx, port.AbortStagedRevisionInput{Scope: scope, StageID: stage.Stage.StageID})
			}
			status, err = h.finalizeRecovered(ctx, scope, record, previewFor(record, stage, hasStage), runtimeprotocol.OperationResultStatusRejected, nil, runtimeprotocol.RuntimeFailureCodeRuntimeCancelled, runtimeprotocol.RecoveryPhaseFinal)
		case runtimeprotocol.RecoveryPhasePublicationPending:
			status, err = h.recoverPublicationPending(ctx, scope, record, stage, hasStage)
		case runtimeprotocol.RecoveryPhasePublished, runtimeprotocol.RecoveryPhaseExternalPending, runtimeprotocol.RecoveryPhaseExternalFailed, runtimeprotocol.RecoveryPhaseExternalPublished, runtimeprotocol.RecoveryPhaseStatePending, runtimeprotocol.RecoveryPhaseAuditPending, runtimeprotocol.RecoveryPhaseOutboxReady:
			status, err = h.recoverPublished(ctx, scope, record, inspection.PublishedRevision, stage, hasStage)
		case runtimeprotocol.RecoveryPhaseRecovering:
			status, err = h.finalizeRecovered(ctx, scope, record, previewFor(record, stage, hasStage), runtimeprotocol.OperationResultStatusNeedsReview, nil, "", runtimeprotocol.RecoveryPhaseNeedsReview)
		default:
			err = port.ErrIndeterminate
		}
		if err != nil {
			return results, err
		}
		results = append(results, RecoveryResult{OperationID: record.Status.OperationID, Status: status, Converged: true})
	}
	// Stages not referenced by a live journal record can never be publication
	// authority. Remove them with the same bounded validated stage IDs.
	for _, stage := range stages {
		if !active[stage.Input.OperationID] {
			if err := h.documents.AbortStagedRevision(ctx, port.AbortStagedRevisionInput{Scope: scope, StageID: stage.Stage.StageID}); err != nil {
				return results, err
			}
		}
	}
	return results, nil
}

func previewOf(stage local.StagedInspection, ok bool) *runtimeprotocol.PreviewEvaluation {
	if !ok || stage.Input.PreviewEvaluation == nil {
		return nil
	}
	value := *stage.Input.PreviewEvaluation
	return &value
}

func previewFor(record port.RecoveryRecord, stage local.StagedInspection, ok bool) *runtimeprotocol.PreviewEvaluation {
	if value := previewOf(stage, ok); value != nil {
		return value
	}
	if record.PreviewEvaluation == nil {
		return nil
	}
	value := *record.PreviewEvaluation
	return &value
}

func (h *Host) recoverPublicationPending(ctx context.Context, scope runtimeprotocol.RuntimeScope, record port.RecoveryRecord, stage local.StagedInspection, hasStage bool) (runtimeprotocol.RuntimeOperationStatus, error) {
	head, headErr := h.documents.GetHead(ctx, port.GetDocumentHeadInput{Scope: scope})
	if headErr != nil {
		if _, err := h.advanceRecovery(ctx, scope, record.Status.OperationID, runtimeprotocol.RecoveryPhasePublicationPending, runtimeprotocol.RecoveryPhaseRecovering, nil); err != nil {
			return runtimeprotocol.RuntimeOperationStatus{}, err
		}
		return h.finalizeRecovered(ctx, scope, record, previewFor(record, stage, hasStage), runtimeprotocol.OperationResultStatusNeedsReview, nil, "", runtimeprotocol.RecoveryPhaseNeedsReview)
	}
	if !hasStage {
		if reflect.DeepEqual(head.Revision, record.BaseRevision) {
			if _, err := h.advanceRecovery(ctx, scope, record.Status.OperationID, runtimeprotocol.RecoveryPhasePublicationPending, runtimeprotocol.RecoveryPhaseRecovering, nil); err != nil {
				return runtimeprotocol.RuntimeOperationStatus{}, err
			}
			return h.finalizeRecovered(ctx, scope, record, previewFor(record, stage, false), runtimeprotocol.OperationResultStatusRejected, nil, runtimeprotocol.RuntimeFailureCodeRuntimeStaleRevision, runtimeprotocol.RecoveryPhaseFinal)
		}
		if _, err := h.advanceRecovery(ctx, scope, record.Status.OperationID, runtimeprotocol.RecoveryPhasePublicationPending, runtimeprotocol.RecoveryPhaseRecovering, nil); err != nil {
			return runtimeprotocol.RuntimeOperationStatus{}, err
		}
		return h.finalizeRecovered(ctx, scope, record, previewFor(record, stage, false), runtimeprotocol.OperationResultStatusNeedsReview, nil, "", runtimeprotocol.RecoveryPhaseNeedsReview)
	}
	if sameStagedHead(head.Revision, stage.Stage.Revision) {
		if _, err := h.advanceRecovery(ctx, scope, record.Status.OperationID, runtimeprotocol.RecoveryPhasePublicationPending, runtimeprotocol.RecoveryPhasePublished, &head.Revision); err != nil {
			return runtimeprotocol.RuntimeOperationStatus{}, err
		}
		return h.finishPublished(ctx, scope, record, runtimeprotocol.RecoveryPhasePublished, head.Revision, stage)
	}
	if reflect.DeepEqual(head.Revision, record.BaseRevision) {
		_ = h.documents.AbortStagedRevision(ctx, port.AbortStagedRevisionInput{Scope: scope, StageID: stage.Stage.StageID})
		if _, err := h.advanceRecovery(ctx, scope, record.Status.OperationID, runtimeprotocol.RecoveryPhasePublicationPending, runtimeprotocol.RecoveryPhaseRecovering, nil); err != nil {
			return runtimeprotocol.RuntimeOperationStatus{}, err
		}
		return h.finalizeRecovered(ctx, scope, record, previewFor(record, stage, true), runtimeprotocol.OperationResultStatusRejected, nil, runtimeprotocol.RuntimeFailureCodeRuntimeStaleRevision, runtimeprotocol.RecoveryPhaseFinal)
	}
	// A different current head cannot prove this candidate was never
	// published: it may have been published and subsequently superseded.
	if _, err := h.advanceRecovery(ctx, scope, record.Status.OperationID, runtimeprotocol.RecoveryPhasePublicationPending, runtimeprotocol.RecoveryPhaseRecovering, nil); err != nil {
		return runtimeprotocol.RuntimeOperationStatus{}, err
	}
	return h.finalizeRecovered(ctx, scope, record, previewFor(record, stage, true), runtimeprotocol.OperationResultStatusNeedsReview, nil, "", runtimeprotocol.RecoveryPhaseNeedsReview)
}

func sameStagedHead(head, staged runtimeprotocol.CommittedRevisionRef) bool {
	staged.ProviderVersion = head.ProviderVersion
	return reflect.DeepEqual(head, staged)
}

func (h *Host) recoverPublished(ctx context.Context, scope runtimeprotocol.RuntimeScope, record port.RecoveryRecord, published *runtimeprotocol.CommittedRevisionRef, stage local.StagedInspection, hasStage bool) (runtimeprotocol.RuntimeOperationStatus, error) {
	if published == nil {
		if record.Status.Phase == runtimeprotocol.RecoveryPhaseOutboxReady {
			return runtimeprotocol.RuntimeOperationStatus{}, port.ErrIndeterminate
		}
		if _, err := h.advanceRecovery(ctx, scope, record.Status.OperationID, record.Status.Phase, runtimeprotocol.RecoveryPhaseRecovering, nil); err != nil {
			return runtimeprotocol.RuntimeOperationStatus{}, err
		}
		return h.finalizeRecovered(ctx, scope, record, previewFor(record, stage, hasStage), runtimeprotocol.OperationResultStatusNeedsReview, nil, "", runtimeprotocol.RecoveryPhaseNeedsReview)
	}
	return h.finishPublished(ctx, scope, record, record.Status.Phase, *published, stage)
}

func (h *Host) finishPublished(ctx context.Context, scope runtimeprotocol.RuntimeScope, record port.RecoveryRecord, phase runtimeprotocol.RecoveryPhase, revision runtimeprotocol.CommittedRevisionRef, stage local.StagedInspection) (runtimeprotocol.RuntimeOperationStatus, error) {
	var externalStatus *runtimeprotocol.ExternalMaterializationStatus
	if record.ExternalStage != nil {
		if phase == runtimeprotocol.RecoveryPhasePublished {
			updated, err := h.recovery.Advance(ctx, port.AdvanceRecoveryRecordInput{Scope: scope, OperationID: record.Status.OperationID, ExpectedPhase: phase, NextPhase: runtimeprotocol.RecoveryPhaseExternalPending, PublishedRevision: &revision})
			if err != nil {
				return runtimeprotocol.RuntimeOperationStatus{}, err
			}
			record, phase = updated, runtimeprotocol.RecoveryPhaseExternalPending
		}
		if phase == runtimeprotocol.RecoveryPhaseExternalPending {
			inspection, err := h.external.Inspect(ctx, port.InspectExternalFileInput{Scope: scope, OperationID: record.Status.OperationID, IdempotencyKey: record.Status.IdempotencyKey})
			if err != nil {
				return runtimeprotocol.RuntimeOperationStatus{}, err
			}
			var receipt port.ExternalFileReceipt
			if inspection.Receipt != nil {
				receipt = *inspection.Receipt
			} else if inspection.Stage != nil && record.ExpectedExternalProviderVersion != nil {
				publicationCtx, release, authErr := h.authorizeRecoveredExternalPublication(ctx, scope, record, stage)
				if authErr != nil {
					return runtimeprotocol.RuntimeOperationStatus{}, authErr
				}
				receipt, err = h.external.Publish(publicationCtx, port.PublishExternalFileInput{Scope: scope, OperationID: record.Status.OperationID, IdempotencyKey: record.Status.IdempotencyKey, StageID: inspection.Stage.StageID, ExpectedProviderVersion: *record.ExpectedExternalProviderVersion})
				release()
			} else {
				err = port.ErrIndeterminate
			}
			if err != nil {
				if !errors.Is(err, port.ErrConflict) {
					return runtimeprotocol.RuntimeOperationStatus{}, err
				}
				failure := runtimeprotocol.ExternalMaterializationFailureConflict
				updated, advanceErr := h.recovery.Advance(ctx, port.AdvanceRecoveryRecordInput{Scope: scope, OperationID: record.Status.OperationID, ExpectedPhase: phase, NextPhase: runtimeprotocol.RecoveryPhaseExternalFailed, PublishedRevision: &revision, ExternalFailure: &failure})
				if advanceErr != nil {
					return runtimeprotocol.RuntimeOperationStatus{}, advanceErr
				}
				record, phase = updated, runtimeprotocol.RecoveryPhaseExternalFailed
			} else {
				updated, advanceErr := h.recovery.Advance(ctx, port.AdvanceRecoveryRecordInput{Scope: scope, OperationID: record.Status.OperationID, ExpectedPhase: phase, NextPhase: runtimeprotocol.RecoveryPhaseExternalPublished, PublishedRevision: &revision, ExternalReceipt: &receipt})
				if advanceErr != nil {
					return runtimeprotocol.RuntimeOperationStatus{}, advanceErr
				}
				record, phase = updated, runtimeprotocol.RecoveryPhaseExternalPublished
			}
		}
		externalStatus = record.Status.ExternalMaterialization
	}
	for phase != runtimeprotocol.RecoveryPhaseOutboxReady {
		next := map[runtimeprotocol.RecoveryPhase]runtimeprotocol.RecoveryPhase{runtimeprotocol.RecoveryPhasePublished: runtimeprotocol.RecoveryPhaseStatePending, runtimeprotocol.RecoveryPhaseExternalFailed: runtimeprotocol.RecoveryPhaseStatePending, runtimeprotocol.RecoveryPhaseExternalPublished: runtimeprotocol.RecoveryPhaseStatePending, runtimeprotocol.RecoveryPhaseStatePending: runtimeprotocol.RecoveryPhaseAuditPending, runtimeprotocol.RecoveryPhaseAuditPending: runtimeprotocol.RecoveryPhaseOutboxReady}[phase]
		if next == "" {
			return runtimeprotocol.RuntimeOperationStatus{}, port.ErrIndeterminate
		}
		if _, err := h.advanceRecovery(ctx, scope, record.Status.OperationID, phase, next, &revision); err != nil {
			return runtimeprotocol.RuntimeOperationStatus{}, err
		}
		phase = next
	}
	decisionDigest := record.DecisionDigest
	if decisionDigest == nil {
		return runtimeprotocol.RuntimeOperationStatus{}, port.ErrIndeterminate
	}
	parent := record.BaseRevision.RevisionID
	trigger := stage.Input.Trigger
	if trigger == "" {
		trigger = runtimeprotocol.CommitTriggerRestore
	}
	_, err := h.history.AppendRevision(ctx, port.AppendRevisionInput{Scope: scope, Metadata: runtimeprotocol.RevisionMetadata{Revision: revision, ParentRevisionID: &parent, OperationID: record.Status.OperationID, Trigger: trigger, AuthoringDecisionDigest: *decisionDigest, CommittedAt: protocolcommon.Rfc3339Time(h.config.Clock.Now().UTC().Format("2006-01-02T15:04:05.999999999Z07:00")), ExternalMaterialization: externalStatus}})
	stateVersion := protocolcommon.CanonicalNonNegativeInt64("0")
	status := runtimeprotocol.OperationResultStatusCommittedStateStale
	if stateHead, stateErr := h.state.GetHead(ctx, port.GetStateHeadInput{Scope: scope}); stateErr == nil {
		stateVersion = stateHead.StateVersion
		if stateHead.DefinitionHash == revision.DefinitionHash && stateHead.GraphHash == revision.GraphHash {
			status = runtimeprotocol.OperationResultStatusCommitted
		}
	}
	if err != nil {
		status = runtimeprotocol.OperationResultStatusCommittedStateStale
	} else if status == runtimeprotocol.OperationResultStatusCommitted && externalStatus != nil && externalStatus.State == runtimeprotocol.ExternalMaterializationStateFailed {
		status = runtimeprotocol.OperationResultStatusCommittedExternalFailed
	}
	resultState := &stateVersion
	if status == runtimeprotocol.OperationResultStatusCommittedExternalFailed {
		resultState = nil
	}
	if externalStatus != nil && externalStatus.State == runtimeprotocol.ExternalMaterializationStatePublished {
		digest := stage.Input.Manifest.Digest
		if stage.Input.OperationID != record.Status.OperationID || digest == "" {
			return runtimeprotocol.RuntimeOperationStatus{}, port.ErrIndeterminate
		}
		// The external receipt is authoritative. Failure to refresh the
		// conservative metadata cache must not rewrite a committed operation as
		// a recovery failure; a later reopen will safely surface review instead.
		_ = h.acceptDocumentSourceBaseline(scope.DocumentID, digest)
	}
	final, err := h.finalizeRecoveredWithExternal(ctx, scope, record, previewFor(record, stage, true), status, &revision, resultState, "", runtimeprotocol.RecoveryPhaseFinal, externalStatus)
	if err != nil {
		return runtimeprotocol.RuntimeOperationStatus{}, err
	}
	if externalStatus != nil && externalStatus.State == runtimeprotocol.ExternalMaterializationStateFailed && record.ExternalStage != nil {
		_ = h.external.Abort(context.WithoutCancel(ctx), port.AbortExternalFileInput{Scope: scope, StageID: record.ExternalStage.StageID})
	}
	return final, nil
}

func (h *Host) authorizeRecoveredExternalPublication(ctx context.Context, scope runtimeprotocol.RuntimeScope, record port.RecoveryRecord, stage local.StagedInspection) (context.Context, func(), error) {
	preview := previewFor(record, stage, true)
	if preview == nil {
		return nil, nil, port.ErrIndeterminate
	}
	actor := stage.Input.Actor
	ctx, err := h.authority.recoveredGrantContext(ctx, scope, actor, preview.AuthoringDecision.AccessFingerprint)
	if err != nil {
		return nil, nil, err
	}
	release, err := h.authority.AcquireAuthoringPublication(ctx, scope)
	if err != nil || release == nil {
		return nil, nil, accesscore.ErrGrantStale
	}
	grant, _, err := h.authority.ResolveGrant(ctx, scope)
	if err != nil {
		release()
		return nil, nil, accesscore.ErrGrantStale
	}
	decision, rejection := h.runtime.Authorize(ctx, runtimehost.AuthorizationRequest{Scope: scope, CurrentRevision: record.BaseRevision, Evaluation: accessprotocol.EvaluateAuthoringInput{AuthoringImpact: &preview.AuthoringImpact, GrantSnapshot: grant, HostOperationImpacts: []accessprotocol.HostOperationImpact{}, RequestIntent: "publish"}})
	if rejection != nil || decision.Outcome != accessprotocol.AuthoringDecisionOutcomeAllow || decision.AccessFingerprint != preview.AuthoringDecision.AccessFingerprint || !reflect.DeepEqual(decision.RequiredCapabilities, preview.AuthoringDecision.RequiredCapabilities) {
		release()
		if rejection != nil {
			return nil, nil, rejection
		}
		return nil, nil, accesscore.ErrGrantStale
	}
	return ctx, release, nil
}

func (h *Host) advanceRecovery(ctx context.Context, scope runtimeprotocol.RuntimeScope, operation runtimeprotocol.OperationID, from, to runtimeprotocol.RecoveryPhase, revision *runtimeprotocol.CommittedRevisionRef) (port.RecoveryRecord, error) {
	return h.recovery.Advance(ctx, port.AdvanceRecoveryRecordInput{Scope: scope, OperationID: operation, ExpectedPhase: from, NextPhase: to, PublishedRevision: revision})
}

func (h *Host) finalizeRecovered(ctx context.Context, scope runtimeprotocol.RuntimeScope, record port.RecoveryRecord, preview *runtimeprotocol.PreviewEvaluation, status runtimeprotocol.OperationResultStatus, revision *runtimeprotocol.CommittedRevisionRef, failure runtimeprotocol.RuntimeFailureCode, terminal runtimeprotocol.RecoveryPhase) (runtimeprotocol.RuntimeOperationStatus, error) {
	return h.finalizeRecoveredWithState(ctx, scope, record, preview, status, revision, nil, failure, terminal)
}

func (h *Host) finalizeRecoveredWithState(ctx context.Context, scope runtimeprotocol.RuntimeScope, record port.RecoveryRecord, preview *runtimeprotocol.PreviewEvaluation, status runtimeprotocol.OperationResultStatus, revision *runtimeprotocol.CommittedRevisionRef, stateVersion *protocolcommon.CanonicalNonNegativeInt64, failure runtimeprotocol.RuntimeFailureCode, terminal runtimeprotocol.RecoveryPhase) (runtimeprotocol.RuntimeOperationStatus, error) {
	return h.finalizeRecoveredWithExternal(ctx, scope, record, preview, status, revision, stateVersion, failure, terminal, nil)
}

func (h *Host) finalizeRecoveredWithExternal(ctx context.Context, scope runtimeprotocol.RuntimeScope, record port.RecoveryRecord, preview *runtimeprotocol.PreviewEvaluation, status runtimeprotocol.OperationResultStatus, revision *runtimeprotocol.CommittedRevisionRef, stateVersion *protocolcommon.CanonicalNonNegativeInt64, failure runtimeprotocol.RuntimeFailureCode, terminal runtimeprotocol.RecoveryPhase, external *runtimeprotocol.ExternalMaterializationStatus) (runtimeprotocol.RuntimeOperationStatus, error) {
	result := runtimehost.RecoveryCommitResultWithExternal(record.Status.OperationID, record.Status.IdempotencyKey, status, revision, stateVersion, failure, external)
	result.PreviewEvaluation = preview
	final, err := h.recovery.Finalize(ctx, port.FinalizeRecoveryRecordInput{Scope: scope, CommitResult: result, TerminalPhase: terminal})
	if err != nil {
		return runtimeprotocol.RuntimeOperationStatus{}, err
	}
	return final.Status, nil
}

var _ = errors.Is
