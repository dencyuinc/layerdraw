// SPDX-License-Identifier: LicenseRef-LayerDraw-1.0
package local

import (
	"context"
	"errors"
	"path/filepath"
	"reflect"
	"strconv"
	"time"

	"github.com/dencyuinc/layerdraw/gen/go/protocolcommon"
	"github.com/dencyuinc/layerdraw/gen/go/runtimeprotocol"
	"github.com/dencyuinc/layerdraw/internal/runtime/port"
)

type recoveryEntry struct {
	Record            port.RecoveryRecord                   `json:"record"`
	PublishedRevision *runtimeprotocol.CommittedRevisionRef `json:"published_revision,omitempty"`
}
type recoveryDisk struct {
	Records     map[string]recoveryEntry `json:"records"`
	Operations  map[string]string        `json:"operations"`
	Idempotency map[string]string        `json:"idempotency"`
}

func externalPendingStatus(stage port.ExternalFileStage) *runtimeprotocol.ExternalMaterializationStatus {
	return &runtimeprotocol.ExternalMaterializationStatus{State: runtimeprotocol.ExternalMaterializationStatePending, CandidateProviderVersion: stage.CandidateProviderVersion}
}

func externalPublishedStatus(receipt port.ExternalFileReceipt) *runtimeprotocol.ExternalMaterializationStatus {
	provider, digest := receipt.ProviderVersion, receipt.ReceiptDigest
	return &runtimeprotocol.ExternalMaterializationStatus{State: runtimeprotocol.ExternalMaterializationStatePublished, CandidateProviderVersion: receipt.ProviderVersion, ProviderVersion: &provider, ReceiptDigest: &digest}
}

func externalFailedStatus(stage port.ExternalFileStage, failure runtimeprotocol.ExternalMaterializationFailure) *runtimeprotocol.ExternalMaterializationStatus {
	return &runtimeprotocol.ExternalMaterializationStatus{State: runtimeprotocol.ExternalMaterializationStateFailed, CandidateProviderVersion: stage.CandidateProviderVersion, Failure: &failure}
}

func recoveryRecordID(op, key string) (string, error) {
	return safeID(strconv.Itoa(len(op)) + ":" + op + ":" + key)
}

func validRecoveryEntry(scope runtimeprotocol.RuntimeScope, entry recoveryEntry) bool {
	if !validRecoveryRecordShape(scope, entry.Record) {
		return false
	}
	phase := entry.Record.Status.Phase
	publishedPhase := phase == runtimeprotocol.RecoveryPhasePublished || phase == runtimeprotocol.RecoveryPhaseExternalPending || phase == runtimeprotocol.RecoveryPhaseExternalPublished || phase == runtimeprotocol.RecoveryPhaseExternalFailed || phase == runtimeprotocol.RecoveryPhaseStatePending || phase == runtimeprotocol.RecoveryPhaseAuditPending || phase == runtimeprotocol.RecoveryPhaseOutboxReady
	if publishedPhase && entry.PublishedRevision == nil {
		return false
	}
	if entry.Record.CommitResult != nil && entry.Record.CommitResult.OperationResult.CommittedRevision != nil && (entry.PublishedRevision == nil || !reflect.DeepEqual(entry.PublishedRevision, entry.Record.CommitResult.OperationResult.CommittedRevision)) {
		return false
	}
	if entry.PublishedRevision != nil {
		if !validRevision(scope, *entry.PublishedRevision) {
			return false
		}
		if phase == runtimeprotocol.RecoveryPhasePending || phase == runtimeprotocol.RecoveryPhaseStaged || phase == runtimeprotocol.RecoveryPhasePublicationPending {
			return false
		}
	}
	return true
}

func (s *Recovery) recoveryPath(scope runtimeprotocol.RuntimeScope) (string, error) {
	d, e := s.scopeDir(scope)
	return filepath.Join(d, "recovery", "journal.json"), e
}
func (s *Recovery) loadRecovery(scope runtimeprotocol.RuntimeScope) (recoveryDisk, error) {
	p, e := s.recoveryPath(scope)
	if e != nil {
		return recoveryDisk{}, e
	}
	var d recoveryDisk
	if e = s.readJSON(p, &d); e != nil {
		if errors.Is(e, port.ErrNotFound) {
			return recoveryDisk{Records: map[string]recoveryEntry{}, Operations: map[string]string{}, Idempotency: map[string]string{}}, nil
		}
		return d, e
	}
	if d.Records == nil || d.Operations == nil || d.Idempotency == nil {
		return d, port.ErrIndeterminate
	}
	if len(d.Records) != len(d.Operations) || len(d.Records) != len(d.Idempotency) {
		return d, invalidPersisted("recovery indexes")
	}
	for op, k := range d.Operations {
		entry, ok := d.Records[k]
		if !ok || string(entry.Record.Status.OperationID) != op {
			return d, port.ErrIndeterminate
		}
	}
	for key, k := range d.Idempotency {
		entry, ok := d.Records[k]
		if !ok || string(entry.Record.Status.IdempotencyKey) != key {
			return d, port.ErrIndeterminate
		}
	}
	for rid, entry := range d.Records {
		op, key := string(entry.Record.Status.OperationID), string(entry.Record.Status.IdempotencyKey)
		expected, e := recoveryRecordID(op, key)
		if e != nil || rid != expected || d.Operations[op] != rid || d.Idempotency[key] != rid || !validRecoveryEntry(scope, entry) {
			return d, invalidPersisted("recovery record")
		}
	}
	return d, nil
}
func (s *Recovery) saveRecovery(scope runtimeprotocol.RuntimeScope, d recoveryDisk) error {
	p, e := s.recoveryPath(scope)
	if e != nil {
		return e
	}
	return s.writeJSON(p, d)
}

func (s *Recovery) CreatePending(ctx context.Context, in port.CreatePendingRecordInput) (port.RecoveryRecord, error) {
	if err := ctx.Err(); err != nil {
		return port.RecoveryRecord{}, err
	}
	if _, err := runtimeprotocol.EncodeRuntimeScope(in.Scope); err != nil {
		return port.RecoveryRecord{}, port.ErrConflict
	}
	if !validDigest(in.PayloadDigest) || !validRevision(in.Scope, in.BaseRevision) {
		return port.RecoveryRecord{}, port.ErrConflict
	}
	if _, e := runtimeprotocol.EncodeOperationID(in.OperationID); e != nil {
		return port.RecoveryRecord{}, port.ErrConflict
	}
	if _, e := runtimeprotocol.EncodeIdempotencyKey(in.IdempotencyKey); e != nil {
		return port.RecoveryRecord{}, port.ErrConflict
	}
	var out port.RecoveryRecord
	err := s.withLock(in.Scope, func(_ string) error {
		d, e := s.loadRecovery(in.Scope)
		if e != nil {
			return e
		}
		op := string(in.OperationID)
		key := string(in.IdempotencyKey)
		if existing, ok := d.Operations[op]; ok {
			entry := d.Records[existing]
			if entry.Record.Status.IdempotencyKey == in.IdempotencyKey && entry.Record.PayloadDigest == in.PayloadDigest && reflect.DeepEqual(entry.Record.BaseRevision, in.BaseRevision) {
				out = entry.Record
				return nil
			}
			return port.ErrConflict
		}
		if _, ok := d.Idempotency[key]; ok {
			return port.ErrConflict
		}
		rid, e := recoveryRecordID(op, key)
		if e != nil {
			return e
		}
		out = port.RecoveryRecord{Scope: in.Scope, Status: runtimeprotocol.RuntimeOperationStatus{Phase: runtimeprotocol.RecoveryPhasePending, OperationID: in.OperationID, IdempotencyKey: in.IdempotencyKey}, PayloadDigest: in.PayloadDigest, BaseRevision: in.BaseRevision}
		entry := recoveryEntry{Record: out}
		if !validRecoveryEntry(in.Scope, entry) {
			return port.ErrConflict
		}
		d.Records[rid] = entry
		d.Operations[op] = rid
		d.Idempotency[key] = rid
		return s.saveRecovery(in.Scope, d)
	})
	return cloneResult(out, err)
}

func (s *Recovery) AbandonPending(ctx context.Context, in port.AbandonPendingRecordInput) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	return s.withLock(in.Scope, func(_ string) error {
		d, e := s.loadRecovery(in.Scope)
		if e != nil {
			return e
		}
		rid, ok := d.Operations[string(in.OperationID)]
		if !ok {
			return port.ErrNotFound
		}
		entry := d.Records[rid]
		if entry.Record.Status.Phase != runtimeprotocol.RecoveryPhasePending || entry.Record.Status.IdempotencyKey != in.IdempotencyKey || entry.Record.PayloadDigest != in.PayloadDigest {
			return port.ErrConflict
		}
		delete(d.Records, rid)
		delete(d.Operations, string(in.OperationID))
		delete(d.Idempotency, string(in.IdempotencyKey))
		return s.saveRecovery(in.Scope, d)
	})
}

func (s *Recovery) Get(ctx context.Context, in port.GetRecoveryRecordInput) (port.RecoveryRecord, error) {
	if err := ctx.Err(); err != nil {
		return port.RecoveryRecord{}, err
	}
	if (in.OperationID == nil) == (in.IdempotencyKey == nil) {
		return port.RecoveryRecord{}, port.ErrConflict
	}
	d, e := s.loadRecovery(in.Scope)
	if e != nil {
		return port.RecoveryRecord{}, e
	}
	var rid string
	var ok bool
	if in.OperationID != nil {
		rid, ok = d.Operations[string(*in.OperationID)]
	} else {
		rid, ok = d.Idempotency[string(*in.IdempotencyKey)]
	}
	if !ok {
		return port.RecoveryRecord{}, port.ErrNotFound
	}
	return clone(d.Records[rid].Record)
}

var legalTransitions = map[runtimeprotocol.RecoveryPhase]map[runtimeprotocol.RecoveryPhase]bool{
	runtimeprotocol.RecoveryPhasePending: {runtimeprotocol.RecoveryPhaseStaged: true}, runtimeprotocol.RecoveryPhaseStaged: {runtimeprotocol.RecoveryPhasePublicationPending: true}, runtimeprotocol.RecoveryPhasePublicationPending: {runtimeprotocol.RecoveryPhasePublished: true, runtimeprotocol.RecoveryPhaseRecovering: true}, runtimeprotocol.RecoveryPhasePublished: {runtimeprotocol.RecoveryPhaseExternalPending: true, runtimeprotocol.RecoveryPhaseStatePending: true, runtimeprotocol.RecoveryPhaseRecovering: true}, runtimeprotocol.RecoveryPhaseExternalPending: {runtimeprotocol.RecoveryPhaseExternalFailed: true, runtimeprotocol.RecoveryPhaseExternalPublished: true, runtimeprotocol.RecoveryPhaseRecovering: true}, runtimeprotocol.RecoveryPhaseExternalFailed: {runtimeprotocol.RecoveryPhaseStatePending: true}, runtimeprotocol.RecoveryPhaseExternalPublished: {runtimeprotocol.RecoveryPhaseStatePending: true, runtimeprotocol.RecoveryPhaseRecovering: true}, runtimeprotocol.RecoveryPhaseStatePending: {runtimeprotocol.RecoveryPhaseAuditPending: true, runtimeprotocol.RecoveryPhaseRecovering: true}, runtimeprotocol.RecoveryPhaseAuditPending: {runtimeprotocol.RecoveryPhaseOutboxReady: true, runtimeprotocol.RecoveryPhaseRecovering: true}, runtimeprotocol.RecoveryPhaseOutboxReady: {runtimeprotocol.RecoveryPhaseFinal: true}, runtimeprotocol.RecoveryPhaseRecovering: {runtimeprotocol.RecoveryPhaseFinal: true, runtimeprotocol.RecoveryPhaseNeedsReview: true},
}

func (s *Recovery) Advance(ctx context.Context, in port.AdvanceRecoveryRecordInput) (port.RecoveryRecord, error) {
	if err := ctx.Err(); err != nil {
		return port.RecoveryRecord{}, err
	}
	if !legalTransitions[in.ExpectedPhase][in.NextPhase] {
		return port.RecoveryRecord{}, port.ErrConflict
	}
	var out port.RecoveryRecord
	err := s.withLock(in.Scope, func(_ string) error {
		d, e := s.loadRecovery(in.Scope)
		if e != nil {
			return e
		}
		rid, ok := d.Operations[string(in.OperationID)]
		if !ok {
			return port.ErrNotFound
		}
		entry := d.Records[rid]
		if entry.Record.Status.Phase != in.ExpectedPhase {
			return port.ErrConflict
		}
		if in.ExpectedPhase == runtimeprotocol.RecoveryPhasePending && in.NextPhase == runtimeprotocol.RecoveryPhaseStaged {
			if in.EvaluationDigest == nil || in.DecisionDigest == nil || !validDigest(*in.EvaluationDigest) || !validDigest(*in.DecisionDigest) {
				return port.ErrConflict
			}
			entry.Record.EvaluationDigest = in.EvaluationDigest
			entry.Record.DecisionDigest = in.DecisionDigest
			if in.PreviewEvaluation != nil {
				if _, encodeErr := runtimeprotocol.EncodePreviewEvaluation(*in.PreviewEvaluation); encodeErr != nil || in.PreviewEvaluation.AuthoringDecision.EvaluationDigest != *in.EvaluationDigest || in.PreviewEvaluation.AuthoringDecision.DecisionDigest != *in.DecisionDigest {
					return port.ErrConflict
				}
				value := *in.PreviewEvaluation
				entry.Record.PreviewEvaluation = &value
			}
		} else if in.EvaluationDigest != nil || in.DecisionDigest != nil {
			return port.ErrConflict
		}
		if in.PublishedRevision != nil {
			if in.NextPhase != runtimeprotocol.RecoveryPhasePublished && (in.ExpectedPhase == runtimeprotocol.RecoveryPhasePending || in.ExpectedPhase == runtimeprotocol.RecoveryPhaseStaged || in.ExpectedPhase == runtimeprotocol.RecoveryPhasePublicationPending) {
				return port.ErrConflict
			}
			if in.PublishedRevision.DocumentID != in.Scope.DocumentID {
				return port.ErrConflict
			}
			if entry.PublishedRevision != nil && !reflect.DeepEqual(entry.PublishedRevision, in.PublishedRevision) {
				return port.ErrConflict
			}
			v := *in.PublishedRevision
			entry.PublishedRevision = &v
		}
		if in.NextPhase == runtimeprotocol.RecoveryPhasePublished && (in.PublishedRevision == nil || entry.PublishedRevision == nil) {
			return port.ErrConflict
		}
		if in.ExternalStage != nil || in.ExpectedExternalProviderVersion != nil {
			if in.ExpectedPhase != runtimeprotocol.RecoveryPhaseStaged || in.NextPhase != runtimeprotocol.RecoveryPhasePublicationPending || in.ExternalStage == nil || in.ExpectedExternalProviderVersion == nil || entry.Record.ExternalStage != nil {
				return port.ErrConflict
			}
			stage := *in.ExternalStage
			expected := *in.ExpectedExternalProviderVersion
			entry.Record.ExternalStage = &stage
			entry.Record.ExpectedExternalProviderVersion = &expected
		}
		if in.ExternalReceipt != nil {
			if in.ExpectedPhase != runtimeprotocol.RecoveryPhaseExternalPending || in.NextPhase != runtimeprotocol.RecoveryPhaseExternalPublished || entry.Record.ExternalStage == nil || in.ExternalReceipt.ProviderVersion != entry.Record.ExternalStage.CandidateProviderVersion || in.ExternalReceipt.MaterializationDigest != entry.Record.ExternalStage.MaterializationDigest {
				return port.ErrConflict
			}
			receipt := *in.ExternalReceipt
			entry.Record.ExternalReceipt = &receipt
			entry.Record.Status.ExternalMaterialization = externalPublishedStatus(receipt)
		} else if in.NextPhase == runtimeprotocol.RecoveryPhaseExternalPending {
			if entry.Record.ExternalStage == nil || entry.Record.ExpectedExternalProviderVersion == nil {
				return port.ErrConflict
			}
			entry.Record.Status.ExternalMaterialization = externalPendingStatus(*entry.Record.ExternalStage)
		} else if in.ExternalFailure != nil {
			if in.ExpectedPhase != runtimeprotocol.RecoveryPhaseExternalPending || in.NextPhase != runtimeprotocol.RecoveryPhaseExternalFailed || entry.Record.ExternalStage == nil {
				return port.ErrConflict
			}
			failure := *in.ExternalFailure
			entry.Record.ExternalFailure = &failure
			entry.Record.Status.ExternalMaterialization = externalFailedStatus(*entry.Record.ExternalStage, failure)
		}
		entry.Record.Status.Phase = in.NextPhase
		if in.NextPhase == runtimeprotocol.RecoveryPhaseRecovering {
			started := protocolcommon.Rfc3339Time(s.now().UTC().Format(time.RFC3339Nano))
			entry.Record.Status.RecoveryStartedAt = &started
		}
		if !validRecoveryEntry(in.Scope, entry) {
			return port.ErrConflict
		}
		d.Records[rid] = entry
		out = entry.Record
		return s.saveRecovery(in.Scope, d)
	})
	return cloneResult(out, err)
}

func (s *Recovery) Finalize(ctx context.Context, in port.FinalizeRecoveryRecordInput) (port.RecoveryRecord, error) {
	if err := ctx.Err(); err != nil {
		return port.RecoveryRecord{}, err
	}
	if in.TerminalPhase != runtimeprotocol.RecoveryPhaseFinal && in.TerminalPhase != runtimeprotocol.RecoveryPhaseNeedsReview {
		return port.RecoveryRecord{}, port.ErrConflict
	}
	if _, e := runtimeprotocol.EncodeRuntimeCommitResult(in.CommitResult); e != nil {
		return port.RecoveryRecord{}, port.ErrConflict
	}
	result := in.CommitResult.OperationResult
	if result.OperationID == "" || result.IdempotencyKey == "" {
		return port.RecoveryRecord{}, port.ErrConflict
	}
	if in.TerminalPhase == runtimeprotocol.RecoveryPhaseNeedsReview && result.Status != runtimeprotocol.OperationResultStatusNeedsReview {
		return port.RecoveryRecord{}, port.ErrConflict
	}
	if in.TerminalPhase == runtimeprotocol.RecoveryPhaseFinal && result.Status == runtimeprotocol.OperationResultStatusNeedsReview {
		return port.RecoveryRecord{}, port.ErrConflict
	}
	if recoveryResultDigest(result) != result.ResultDigest {
		return port.RecoveryRecord{}, port.ErrConflict
	}
	op := in.CommitResult.OperationResult.OperationID
	var out port.RecoveryRecord
	err := s.withLock(in.Scope, func(_ string) error {
		d, e := s.loadRecovery(in.Scope)
		if e != nil {
			return e
		}
		rid, ok := d.Operations[string(op)]
		if !ok {
			return port.ErrNotFound
		}
		entry := d.Records[rid]
		phase := entry.Record.Status.Phase
		if entry.Record.Status.IdempotencyKey != in.CommitResult.OperationResult.IdempotencyKey {
			return port.ErrConflict
		}
		if in.CommitResult.PreviewEvaluation != nil {
			decision := in.CommitResult.PreviewEvaluation.AuthoringDecision
			if entry.Record.EvaluationDigest == nil || entry.Record.DecisionDigest == nil || *entry.Record.EvaluationDigest != decision.EvaluationDigest || *entry.Record.DecisionDigest != decision.DecisionDigest {
				return port.ErrConflict
			}
		} else if entry.Record.EvaluationDigest != nil || entry.Record.DecisionDigest != nil {
			if phase == runtimeprotocol.RecoveryPhasePending {
				return port.ErrConflict
			}
		}
		if result.CommittedRevision != nil {
			if entry.PublishedRevision == nil || !reflect.DeepEqual(entry.PublishedRevision, result.CommittedRevision) {
				return port.ErrConflict
			}
		}
		if phase == runtimeprotocol.RecoveryPhasePending || phase == runtimeprotocol.RecoveryPhaseStaged {
			if in.TerminalPhase != runtimeprotocol.RecoveryPhaseFinal {
				return port.ErrConflict
			}
		} else if phase == runtimeprotocol.RecoveryPhaseOutboxReady || phase == runtimeprotocol.RecoveryPhaseRecovering || phase == runtimeprotocol.RecoveryPhaseExternalFailed {
			if !legalTransitions[phase][in.TerminalPhase] {
				return port.ErrConflict
			}
		} else {
			return port.ErrConflict
		}
		result := in.CommitResult
		entry.Record.Status.Phase = in.TerminalPhase
		entry.Record.Status.OperationResult = &result.OperationResult
		entry.Record.Status.RecoveryStartedAt = nil
		entry.Record.Status.RetryAfterMs = nil
		entry.Record.Status.ExternalMaterialization = nil
		entry.Record.CommitResult = &result
		if !validRecoveryEntry(in.Scope, entry) {
			return port.ErrConflict
		}
		d.Records[rid] = entry
		out = entry.Record
		return s.saveRecovery(in.Scope, d)
	})
	return cloneResult(out, err)
}

func cloneResult[T any](v T, err error) (T, error) {
	if err != nil {
		var z T
		return z, err
	}
	return clone(v)
}
