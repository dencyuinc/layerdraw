// SPDX-License-Identifier: LicenseRef-LayerDraw-1.0
package local

import (
	"bytes"
	"encoding/json"
	"fmt"
	"reflect"
	"strings"

	"github.com/dencyuinc/layerdraw/gen/go/protocolcommon"
	"github.com/dencyuinc/layerdraw/gen/go/runtimeprotocol"
	"github.com/dencyuinc/layerdraw/gen/go/semantic"
	"github.com/dencyuinc/layerdraw/internal/runtime/port"
)

func invalidPersisted(label string) error {
	return fmt.Errorf("invalid persisted %s: %w", label, port.ErrIndeterminate)
}
func validBlobRef(v protocolcommon.BlobRef) bool {
	_, e := protocolcommon.EncodeBlobRef(v)
	return e == nil && validDigest(v.Digest)
}
func validRevision(scope runtimeprotocol.RuntimeScope, v runtimeprotocol.CommittedRevisionRef) bool {
	_, e := runtimeprotocol.EncodeCommittedRevisionRef(v)
	return e == nil && v.DocumentID == scope.DocumentID && validDigest(v.DefinitionHash) && validDigest(v.GraphHash)
}
func validDocumentHeadDisk(scope runtimeprotocol.RuntimeScope, h port.DocumentHead) bool {
	return validRevision(scope, h.Revision) && h.Revision.ProviderVersion != nil && *h.Revision.ProviderVersion == h.ProviderVersion && h.ProviderVersion != "" && func() bool { _, e := parseUint(h.FencingToken); return e == nil }()
}
func validRevisionSnapshotDisk(scope runtimeprotocol.RuntimeScope, s port.RevisionSnapshot) bool {
	if !validRevision(scope, s.Revision) || !validBlobRef(s.Manifest) {
		return false
	}
	seen := map[string]bool{}
	for _, r := range s.SourceBlobs {
		if !validBlobRef(r) || seen[r.BlobID] {
			return false
		}
		seen[r.BlobID] = true
	}
	return true
}

func validStateHead(h port.StateHead) bool {
	if _, e := parseNN(h.StateVersion); e != nil {
		return false
	}
	if h.BackendVersion == "" || len(h.BackendVersion) > 1024 || !validDigest(h.DefinitionHash) || !validDigest(h.GraphHash) {
		return false
	}
	if _, e := protocolcommon.EncodeRfc3339Time(h.CapturedAt); e != nil {
		return false
	}
	for a, d := range h.SubjectHashes {
		if _, e := semantic.EncodeStableAddress(a); e != nil || !validDigest(d) {
			return false
		}
	}
	return true
}
func validStateSnapshot(s port.StateSnapshot) bool {
	if !validStateHead(s.Head) || !validBlobRef(s.Contents) {
		return false
	}
	paths := map[semantic.StateFieldPath]bool{}
	for _, p := range s.InaccessibleFieldPaths {
		if _, e := semantic.EncodeStateFieldPath(p); e != nil || paths[p] {
			return false
		}
		paths[p] = true
	}
	subjects := map[semantic.StableAddress]bool{}
	for _, r := range s.Records {
		if _, e := semantic.EncodeStableAddress(r.SubjectAddress); e != nil || subjects[r.SubjectAddress] {
			return false
		}
		subjects[r.SubjectAddress] = true
		if _, e := semantic.EncodeStateSubjectKind(r.SubjectKind); e != nil {
			return false
		}
		if !validDigest(r.OwnSubjectHash) {
			return false
		}
		if r.Fields == nil {
			return false
		}
		if expected, ok := s.Head.SubjectHashes[r.SubjectAddress]; !ok || expected != r.OwnSubjectHash {
			return false
		}
		for path, v := range r.Fields {
			if _, e := semantic.EncodeStateFieldPath(semantic.StateFieldPath(path)); e != nil {
				return false
			}
			if _, e := semantic.EncodeRecipeScalar(v); e != nil {
				return false
			}
		}
		redacted := map[semantic.StateFieldPath]bool{}
		for _, p := range r.RedactedFieldPaths {
			if _, e := semantic.EncodeStateFieldPath(p); e != nil || redacted[p] {
				return false
			}
			redacted[p] = true
		}
	}
	return true
}
func validAudit(a auditDisk) bool {
	if _, e := runtimeprotocol.EncodeOperationID(a.OperationID); e != nil {
		return false
	}
	if !validDigest(a.Ref.EventDigest) || a.Ref.EventDigest != a.Event.Digest || !validBlobRef(a.Event) || a.Ref.EventID == "" {
		return false
	}
	_, e := parseNN(a.Ref.StateVersion)
	return e == nil
}

func validRecoveryRecordShape(scope runtimeprotocol.RuntimeScope, r port.RecoveryRecord) bool {
	if !reflect.DeepEqual(scope, r.Scope) || !validRevision(scope, r.BaseRevision) || !validDigest(r.PayloadDigest) {
		return false
	}
	if _, e := runtimeprotocol.EncodeRuntimeOperationStatus(r.Status); e != nil {
		return false
	}
	if _, e := safeID(string(r.Status.OperationID)); e != nil {
		return false
	}
	if _, e := safeID(string(r.Status.IdempotencyKey)); e != nil {
		return false
	}
	terminal := r.Status.Phase == runtimeprotocol.RecoveryPhaseFinal || r.Status.Phase == runtimeprotocol.RecoveryPhaseNeedsReview
	if terminal {
		if r.CommitResult == nil || r.Status.OperationResult == nil || !reflect.DeepEqual(*r.Status.OperationResult, r.CommitResult.OperationResult) {
			return false
		}
		if _, e := runtimeprotocol.EncodeRuntimeCommitResult(*r.CommitResult); e != nil {
			return false
		}
		if r.CommitResult.OperationResult.OperationID != r.Status.OperationID || r.CommitResult.OperationResult.IdempotencyKey != r.Status.IdempotencyKey {
			return false
		}
		if recoveryResultDigest(r.CommitResult.OperationResult) != r.CommitResult.OperationResult.ResultDigest {
			return false
		}
		if r.Status.Phase == runtimeprotocol.RecoveryPhaseNeedsReview && r.CommitResult.OperationResult.Status != runtimeprotocol.OperationResultStatusNeedsReview {
			return false
		}
		if r.Status.Phase == runtimeprotocol.RecoveryPhaseFinal && r.CommitResult.OperationResult.Status == runtimeprotocol.OperationResultStatusNeedsReview {
			return false
		}
		status := r.CommitResult.OperationResult.Status
		committed := status == runtimeprotocol.OperationResultStatusCommitted || status == runtimeprotocol.OperationResultStatusCommittedExternalFailed || status == runtimeprotocol.OperationResultStatusCommittedExternalPending || status == runtimeprotocol.OperationResultStatusCommittedStateStale
		if committed != (r.CommitResult.OperationResult.CommittedRevision != nil) {
			return false
		}
		if r.CommitResult.PreviewEvaluation != nil {
			decision := r.CommitResult.PreviewEvaluation.AuthoringDecision
			if r.EvaluationDigest == nil || r.DecisionDigest == nil || *r.EvaluationDigest != decision.EvaluationDigest || *r.DecisionDigest != decision.DecisionDigest {
				return false
			}
		}
	} else if r.CommitResult != nil || r.Status.OperationResult != nil {
		return false
	}
	switch r.Status.Phase {
	case runtimeprotocol.RecoveryPhaseExternalPending:
		if r.ExternalStage == nil || r.ExpectedExternalProviderVersion == nil || r.Status.ExternalMaterialization == nil || r.Status.ExternalMaterialization.State != runtimeprotocol.ExternalMaterializationStatePending {
			return false
		}
	case runtimeprotocol.RecoveryPhaseExternalPublished:
		if r.ExternalStage == nil || r.ExternalReceipt == nil || r.Status.ExternalMaterialization == nil || r.Status.ExternalMaterialization.State != runtimeprotocol.ExternalMaterializationStatePublished {
			return false
		}
	case runtimeprotocol.RecoveryPhaseExternalFailed:
		if r.ExternalStage == nil || r.ExternalFailure == nil || r.Status.ExternalMaterialization == nil || r.Status.ExternalMaterialization.State != runtimeprotocol.ExternalMaterializationStateFailed {
			return false
		}
	}
	if r.Status.Phase == runtimeprotocol.RecoveryPhasePending {
		return r.EvaluationDigest == nil && r.DecisionDigest == nil && r.PreviewEvaluation == nil
	}
	if r.PreviewEvaluation != nil {
		if _, e := runtimeprotocol.EncodePreviewEvaluation(*r.PreviewEvaluation); e != nil || r.EvaluationDigest == nil || r.DecisionDigest == nil || r.PreviewEvaluation.AuthoringDecision.EvaluationDigest != *r.EvaluationDigest || r.PreviewEvaluation.AuthoringDecision.DecisionDigest != *r.DecisionDigest {
			return false
		}
	}
	if terminal && r.CommitResult != nil && r.CommitResult.PreviewEvaluation == nil {
		return r.EvaluationDigest == nil && r.DecisionDigest == nil
	}
	return r.EvaluationDigest != nil && r.DecisionDigest != nil && validDigest(*r.EvaluationDigest) && validDigest(*r.DecisionDigest)
}

func recoveryResultDigest(result runtimeprotocol.OperationResult) protocolcommon.Digest {
	projection := struct {
		CommittedRevision       *runtimeprotocol.CommittedRevisionRef          `json:"committed_revision,omitempty"`
		ConflictEvidence        *runtimeprotocol.ConflictEvidence              `json:"conflict_evidence,omitempty"`
		Diagnostics             []semantic.Diagnostic                          `json:"diagnostics"`
		ExternalMaterialization *runtimeprotocol.ExternalMaterializationStatus `json:"external_materialization,omitempty"`
		FailureCode             *runtimeprotocol.RuntimeFailureCode            `json:"failure_code,omitempty"`
		IdempotencyKey          runtimeprotocol.IdempotencyKey                 `json:"idempotency_key"`
		OperationID             runtimeprotocol.OperationID                    `json:"operation_id"`
		StateVersion            *protocolcommon.CanonicalNonNegativeInt64      `json:"state_version,omitempty"`
		Status                  runtimeprotocol.OperationResultStatus          `json:"status"`
	}{result.CommittedRevision, result.ConflictEvidence, result.Diagnostics, result.ExternalMaterialization, result.FailureCode, result.IdempotencyKey, result.OperationID, result.StateVersion, result.Status}
	var b bytes.Buffer
	e := json.NewEncoder(&b)
	e.SetEscapeHTML(false)
	if e.Encode(projection) != nil {
		return ""
	}
	return digestBytes([]byte(strings.TrimSuffix(b.String(), "\n")))
}
