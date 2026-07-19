// SPDX-License-Identifier: LicenseRef-LayerDraw-1.0

package localdocument

import (
	"context"
	"errors"
	"reflect"

	"github.com/dencyuinc/layerdraw/gen/go/protocolcommon"
	"github.com/dencyuinc/layerdraw/gen/go/runtimeprotocol"
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
		case runtimeprotocol.RecoveryPhasePublished, runtimeprotocol.RecoveryPhaseStatePending, runtimeprotocol.RecoveryPhaseAuditPending, runtimeprotocol.RecoveryPhaseOutboxReady:
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
	for phase != runtimeprotocol.RecoveryPhaseOutboxReady {
		next := map[runtimeprotocol.RecoveryPhase]runtimeprotocol.RecoveryPhase{runtimeprotocol.RecoveryPhasePublished: runtimeprotocol.RecoveryPhaseStatePending, runtimeprotocol.RecoveryPhaseStatePending: runtimeprotocol.RecoveryPhaseAuditPending, runtimeprotocol.RecoveryPhaseAuditPending: runtimeprotocol.RecoveryPhaseOutboxReady}[phase]
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
	_, err := h.history.AppendRevision(ctx, port.AppendRevisionInput{Scope: scope, Metadata: runtimeprotocol.RevisionMetadata{Revision: revision, ParentRevisionID: &parent, OperationID: record.Status.OperationID, Trigger: trigger, AuthoringDecisionDigest: *decisionDigest, CommittedAt: protocolcommon.Rfc3339Time(h.config.Clock.Now().UTC().Format("2006-01-02T15:04:05.999999999Z07:00"))}})
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
	}
	return h.finalizeRecoveredWithState(ctx, scope, record, previewFor(record, stage, true), status, &revision, &stateVersion, "", runtimeprotocol.RecoveryPhaseFinal)
}

func (h *Host) advanceRecovery(ctx context.Context, scope runtimeprotocol.RuntimeScope, operation runtimeprotocol.OperationID, from, to runtimeprotocol.RecoveryPhase, revision *runtimeprotocol.CommittedRevisionRef) (port.RecoveryRecord, error) {
	return h.recovery.Advance(ctx, port.AdvanceRecoveryRecordInput{Scope: scope, OperationID: operation, ExpectedPhase: from, NextPhase: to, PublishedRevision: revision})
}

func (h *Host) finalizeRecovered(ctx context.Context, scope runtimeprotocol.RuntimeScope, record port.RecoveryRecord, preview *runtimeprotocol.PreviewEvaluation, status runtimeprotocol.OperationResultStatus, revision *runtimeprotocol.CommittedRevisionRef, failure runtimeprotocol.RuntimeFailureCode, terminal runtimeprotocol.RecoveryPhase) (runtimeprotocol.RuntimeOperationStatus, error) {
	return h.finalizeRecoveredWithState(ctx, scope, record, preview, status, revision, nil, failure, terminal)
}

func (h *Host) finalizeRecoveredWithState(ctx context.Context, scope runtimeprotocol.RuntimeScope, record port.RecoveryRecord, preview *runtimeprotocol.PreviewEvaluation, status runtimeprotocol.OperationResultStatus, revision *runtimeprotocol.CommittedRevisionRef, stateVersion *protocolcommon.CanonicalNonNegativeInt64, failure runtimeprotocol.RuntimeFailureCode, terminal runtimeprotocol.RecoveryPhase) (runtimeprotocol.RuntimeOperationStatus, error) {
	result := runtimehost.RecoveryCommitResult(record.Status.OperationID, record.Status.IdempotencyKey, status, revision, stateVersion, failure)
	result.PreviewEvaluation = preview
	final, err := h.recovery.Finalize(ctx, port.FinalizeRecoveryRecordInput{Scope: scope, CommitResult: result, TerminalPhase: terminal})
	if err != nil {
		return runtimeprotocol.RuntimeOperationStatus{}, err
	}
	return final.Status, nil
}

var _ = errors.Is
