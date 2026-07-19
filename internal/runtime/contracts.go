// SPDX-License-Identifier: LicenseRef-LayerDraw-1.0

package runtime

import (
	"context"
	"fmt"
	"reflect"
	"sort"
	"time"

	"github.com/dencyuinc/layerdraw/gen/go/accessprotocol"
	"github.com/dencyuinc/layerdraw/gen/go/protocolcommon"
	"github.com/dencyuinc/layerdraw/gen/go/runtimeprotocol"
	"github.com/dencyuinc/layerdraw/gen/go/semantic"
)

// ContractError is a stable, safe boundary rejection. It intentionally does
// not wrap provider errors or expose opaque handle contents.
type ContractError struct {
	Code    runtimeprotocol.RuntimeFailureCode
	Message string
}

func (e *ContractError) Error() string { return fmt.Sprintf("%s: %s", e.Code, e.Message) }

func contractError(code runtimeprotocol.RuntimeFailureCode, message string) *ContractError {
	return &ContractError{Code: code, Message: message}
}

// SessionBinding is trusted Runtime state corresponding to an opaque issued
// handle. It is not accepted from a client.
type SessionBinding struct {
	Session         runtimeprotocol.RuntimeSessionRef
	CurrentRevision runtimeprotocol.CommittedRevisionRef
}

func ValidateSessionUse(candidate runtimeprotocol.RuntimeSessionRef, binding SessionBinding, expectedScope runtimeprotocol.RuntimeScope, now time.Time) *ContractError {
	if _, err := runtimeprotocol.EncodeRuntimeSessionRef(candidate); err != nil {
		return contractError(runtimeprotocol.RuntimeFailureCodeRuntimeMalformedHandle, "runtime session handle is malformed")
	}
	if candidate.RuntimeSessionID != binding.Session.RuntimeSessionID {
		return contractError(runtimeprotocol.RuntimeFailureCodeRuntimeMalformedHandle, "runtime session handle is unknown")
	}
	if candidate.Scope.DocumentID != expectedScope.DocumentID || candidate.Scope.DocumentID != binding.Session.Scope.DocumentID {
		return contractError(runtimeprotocol.RuntimeFailureCodeRuntimeCrossDocumentHandle, "runtime session belongs to another document")
	}
	if !sameScope(candidate.Scope, expectedScope) || !sameScope(candidate.Scope, binding.Session.Scope) {
		return contractError(runtimeprotocol.RuntimeFailureCodeRuntimeAuthorizationStale, "runtime session access scope changed")
	}
	if candidate.SessionGeneration != binding.Session.SessionGeneration {
		return contractError(runtimeprotocol.RuntimeFailureCodeRuntimeStaleSessionGeneration, "runtime session generation is stale")
	}
	if !reflect.DeepEqual(candidate.ExpiresAt, binding.Session.ExpiresAt) {
		return contractError(runtimeprotocol.RuntimeFailureCodeRuntimeMalformedHandle, "runtime session expiry binding was altered")
	}
	if binding.Session.ExpiresAt != nil && expired(*binding.Session.ExpiresAt, now) {
		return contractError(runtimeprotocol.RuntimeFailureCodeRuntimeSessionExpired, "runtime session expired")
	}
	return nil
}

func ValidateRevisionUse(candidate runtimeprotocol.CommittedRevisionRef, binding SessionBinding) *ContractError {
	if _, err := runtimeprotocol.EncodeCommittedRevisionRef(candidate); err != nil {
		return contractError(runtimeprotocol.RuntimeFailureCodeRuntimeStaleRevision, "revision reference is malformed")
	}
	if candidate.DocumentID != binding.Session.Scope.DocumentID {
		return contractError(runtimeprotocol.RuntimeFailureCodeRuntimeRevisionScopeMismatch, "revision belongs to another document")
	}
	if !reflect.DeepEqual(candidate, binding.CurrentRevision) {
		return contractError(runtimeprotocol.RuntimeFailureCodeRuntimeStaleRevision, "revision precondition is stale")
	}
	return nil
}

// BlobBinding is trusted Runtime state recorded when a scoped blob reference
// is issued. Blob identity, scope, generation, and expiry are never accepted
// as self-asserted client claims.
type BlobBinding struct {
	Blob runtimeprotocol.RuntimeBlobRef
}

func ValidateBlobUse(candidate runtimeprotocol.RuntimeBlobRef, issued BlobBinding, session SessionBinding, now time.Time) *ContractError {
	if _, err := runtimeprotocol.EncodeRuntimeBlobRef(candidate); err != nil {
		return contractError(runtimeprotocol.RuntimeFailureCodeRuntimeBlobScopeMismatch, "runtime blob reference is malformed")
	}
	if _, err := runtimeprotocol.EncodeRuntimeBlobRef(issued.Blob); err != nil {
		return contractError(runtimeprotocol.RuntimeFailureCodeRuntimeBlobScopeMismatch, "issued runtime blob binding is invalid")
	}
	if !reflect.DeepEqual(candidate, issued.Blob) {
		return contractError(runtimeprotocol.RuntimeFailureCodeRuntimeBlobScopeMismatch, "runtime blob identity or issuance binding was altered")
	}
	if !sameScope(issued.Blob.Scope, session.Session.Scope) || issued.Blob.SessionGeneration != session.Session.SessionGeneration {
		return contractError(runtimeprotocol.RuntimeFailureCodeRuntimeBlobScopeMismatch, "runtime blob reference has a different session scope")
	}
	if issued.Blob.ExpiresAt != nil && expired(*issued.Blob.ExpiresAt, now) {
		return contractError(runtimeprotocol.RuntimeFailureCodeRuntimeBlobExpired, "runtime blob reference expired")
	}
	return nil
}

// CursorBinding is trusted Runtime state recorded when an opaque cursor is
// issued. The wire shape is not proof of authenticity; every client-supplied
// field must match this issued binding exactly before scope checks are applied.
type CursorBinding struct {
	Cursor runtimeprotocol.RuntimeCursorBinding
}

func ValidateCursorUse(candidate runtimeprotocol.RuntimeCursorBinding, issued CursorBinding, binding SessionBinding, operation string, requestDigest protocolcommon.Digest, now time.Time) *ContractError {
	if _, err := runtimeprotocol.EncodeRuntimeCursorBinding(candidate); err != nil {
		return contractError(runtimeprotocol.RuntimeFailureCodeRuntimeInvalidCursor, "runtime cursor is malformed")
	}
	if _, err := runtimeprotocol.EncodeRuntimeCursorBinding(issued.Cursor); err != nil {
		return contractError(runtimeprotocol.RuntimeFailureCodeRuntimeInvalidCursor, "issued runtime cursor binding is invalid")
	}
	if !reflect.DeepEqual(candidate, issued.Cursor) {
		return contractError(runtimeprotocol.RuntimeFailureCodeRuntimeInvalidCursor, "runtime cursor issuance binding was altered")
	}
	if issued.Cursor.Operation != operation || issued.Cursor.NormalizedRequestDigest != requestDigest || !sameScope(issued.Cursor.Scope, binding.Session.Scope) || !reflect.DeepEqual(issued.Cursor.Revision, binding.CurrentRevision) {
		return contractError(runtimeprotocol.RuntimeFailureCodeRuntimeCursorScopeMismatch, "runtime cursor scope does not match the request")
	}
	if expired(issued.Cursor.ExpiresAt, now) {
		return contractError(runtimeprotocol.RuntimeFailureCodeRuntimeInvalidCursor, "runtime cursor expired")
	}
	return nil
}

func ValidateIdempotencyRetry(existingKey runtimeprotocol.IdempotencyKey, existingPayloadDigest protocolcommon.Digest, candidateKey runtimeprotocol.IdempotencyKey, candidatePayloadDigest protocolcommon.Digest) *ContractError {
	if existingKey == candidateKey && existingPayloadDigest != candidatePayloadDigest {
		return contractError(runtimeprotocol.RuntimeFailureCodeRuntimeIdempotencyMismatch, "idempotency key was reused with a different payload")
	}
	return nil
}

func ValidateRecoveryTransition(from, to runtimeprotocol.RecoveryPhase) *ContractError {
	allowed := map[runtimeprotocol.RecoveryPhase][]runtimeprotocol.RecoveryPhase{
		runtimeprotocol.RecoveryPhasePending:            {runtimeprotocol.RecoveryPhaseStaged},
		runtimeprotocol.RecoveryPhaseStaged:             {runtimeprotocol.RecoveryPhasePublicationPending},
		runtimeprotocol.RecoveryPhasePublicationPending: {runtimeprotocol.RecoveryPhasePublished, runtimeprotocol.RecoveryPhaseRecovering},
		runtimeprotocol.RecoveryPhasePublished:          {runtimeprotocol.RecoveryPhaseExternalPending, runtimeprotocol.RecoveryPhaseStatePending, runtimeprotocol.RecoveryPhaseRecovering},
		runtimeprotocol.RecoveryPhaseExternalPending:    {runtimeprotocol.RecoveryPhaseExternalFailed, runtimeprotocol.RecoveryPhaseExternalPublished},
		runtimeprotocol.RecoveryPhaseExternalFailed:     {runtimeprotocol.RecoveryPhaseStatePending},
		runtimeprotocol.RecoveryPhaseExternalPublished:  {runtimeprotocol.RecoveryPhaseStatePending, runtimeprotocol.RecoveryPhaseRecovering},
		runtimeprotocol.RecoveryPhaseStatePending:       {runtimeprotocol.RecoveryPhaseAuditPending, runtimeprotocol.RecoveryPhaseRecovering},
		runtimeprotocol.RecoveryPhaseAuditPending:       {runtimeprotocol.RecoveryPhaseOutboxReady, runtimeprotocol.RecoveryPhaseRecovering},
		runtimeprotocol.RecoveryPhaseOutboxReady:        {runtimeprotocol.RecoveryPhaseFinal},
		runtimeprotocol.RecoveryPhaseRecovering:         {runtimeprotocol.RecoveryPhaseFinal, runtimeprotocol.RecoveryPhaseNeedsReview},
	}
	for _, candidate := range allowed[from] {
		if candidate == to {
			return nil
		}
	}
	return contractError(runtimeprotocol.RuntimeFailureCodeRuntimeInvalidRecoveryTransition, "runtime recovery phase transition is invalid")
}

type AuthorizationRequest struct {
	Scope           runtimeprotocol.RuntimeScope
	CurrentRevision runtimeprotocol.CommittedRevisionRef
	Evaluation      accessprotocol.EvaluateAuthoringInput
	Proof           *runtimeprotocol.AuthoringProof
}

// Authorize always invokes the injected Access decision port. A local host
// expresses full authoring through a full AuthoringGrantSnapshot and the same
// call; there is no local-mode allow branch.
func (r *Runtime) Authorize(ctx context.Context, request AuthorizationRequest) (accessprotocol.AuthoringDecision, *ContractError) {
	if r.config.Ports.Authoring == nil || r.config.Ports.Clock == nil {
		return accessprotocol.AuthoringDecision{}, contractError(runtimeprotocol.RuntimeFailureCodeRuntimeCapabilityUnavailable, "authoring decision port is not configured")
	}
	if rejection := validateRevisionInScope(request.CurrentRevision, request.Scope); rejection != nil {
		return accessprotocol.AuthoringDecision{}, rejection
	}
	input := request.Evaluation
	expectedRevisionDigest := digestValue(request.CurrentRevision)
	if input.BaseRevisionDigest != "" && input.BaseRevisionDigest != expectedRevisionDigest {
		return accessprotocol.AuthoringDecision{}, contractError(runtimeprotocol.RuntimeFailureCodeRuntimeAuthorizationProofInvalid, "authoring evaluation is bound to another revision")
	}
	// Runtime is the trusted owner of the revision binding. Callers cannot
	// self-assert it and older in-process consumers remain source compatible.
	input.BaseRevisionDigest = expectedRevisionDigest
	if _, err := accessprotocol.EncodeEvaluateAuthoringInput(input); err != nil {
		return accessprotocol.AuthoringDecision{}, contractError(runtimeprotocol.RuntimeFailureCodeRuntimeAuthorizationProofInvalid, "authoring decision input is malformed")
	}
	input = canonicalizeAuthoringInput(input)
	grant := input.GrantSnapshot
	accessMismatch := grant.AccessFingerprint != request.Scope.AccessFingerprint && (grant.ActorRef.Kind != "agent" || grant.AgentDelegationDigest == nil)
	if grant.HostDocumentID != string(request.Scope.DocumentID) || grant.LocalScopeID != request.Scope.LocalScopeID || accessMismatch || !sameOptionalString(grant.OrganizationScopeID, request.Scope.OrganizationScopeID) {
		return accessprotocol.AuthoringDecision{}, contractError(runtimeprotocol.RuntimeFailureCodeRuntimeAuthorizationStale, "authoring grant scope does not match the Runtime request")
	}
	for _, impact := range input.HostOperationImpacts {
		if !validHostOperationImpact(impact, request.Scope) {
			return accessprotocol.AuthoringDecision{}, contractError(runtimeprotocol.RuntimeFailureCodeRuntimeAuthorizationProofInvalid, "HostOperationImpact does not match its versioned operation descriptor")
		}
	}
	now := r.config.Ports.Clock.Now()
	if input.GrantSnapshot.ExpiresAt != nil && expired(*input.GrantSnapshot.ExpiresAt, now) {
		return accessprotocol.AuthoringDecision{}, contractError(runtimeprotocol.RuntimeFailureCodeRuntimeAuthorizationStale, "authoring grant expired")
	}
	decision, err := r.config.Ports.Authoring.Evaluate(ctx, input)
	if err != nil {
		return accessprotocol.AuthoringDecision{}, contractError(runtimeprotocol.RuntimeFailureCodeRuntimeAuthorizationStale, "authoring decision could not be obtained")
	}
	if _, err := accessprotocol.EncodeAuthoringDecision(decision); err != nil {
		return accessprotocol.AuthoringDecision{}, contractError(runtimeprotocol.RuntimeFailureCodeRuntimeAuthorizationProofInvalid, "authoring decision is malformed")
	}
	if decision.AccessFingerprint != input.GrantSnapshot.AccessFingerprint {
		return accessprotocol.AuthoringDecision{}, contractError(runtimeprotocol.RuntimeFailureCodeRuntimeAuthorizationStale, "authoring access fingerprint changed")
	}
	if input.AuthoringImpact != nil && (decision.AuthoringImpactDigest == nil || *decision.AuthoringImpactDigest != input.AuthoringImpact.ImpactDigest) {
		return accessprotocol.AuthoringDecision{}, contractError(runtimeprotocol.RuntimeFailureCodeRuntimeAuthorizationProofInvalid, "Engine AuthoringImpact is not bound to the decision")
	}
	wantHostDigests := make([]protocolcommon.Digest, len(input.HostOperationImpacts))
	for index := range input.HostOperationImpacts {
		wantHostDigests[index] = input.HostOperationImpacts[index].ImpactDigest
	}
	if !reflect.DeepEqual(decision.HostOperationImpactDigests, wantHostDigests) {
		return accessprotocol.AuthoringDecision{}, contractError(runtimeprotocol.RuntimeFailureCodeRuntimeAuthorizationProofInvalid, "HostOperationImpact is not bound to the decision")
	}
	if request.Proof != nil {
		proof := request.Proof
		if _, err := runtimeprotocol.EncodeAuthoringProof(*proof); err != nil || !reflect.DeepEqual(proof.BaseRevision, request.CurrentRevision) || proof.AccessFingerprint != decision.AccessFingerprint || proof.DecisionDigest != decision.DecisionDigest || proof.EvaluationDigest != decision.EvaluationDigest || proof.MembershipVersion != input.GrantSnapshot.MembershipVersion || !reflect.DeepEqual(proof.PolicyRefs, input.GrantSnapshot.PolicyRefs) {
			return accessprotocol.AuthoringDecision{}, contractError(runtimeprotocol.RuntimeFailureCodeRuntimeAuthorizationProofInvalid, "authoring proof does not match the current decision")
		}
		if proof.ExpiresAt != nil && expired(*proof.ExpiresAt, now) {
			return accessprotocol.AuthoringDecision{}, contractError(runtimeprotocol.RuntimeFailureCodeRuntimeAuthorizationStale, "authoring proof expired")
		}
	}
	if decision.Outcome != accessprotocol.AuthoringDecisionOutcomeAllow {
		return accessprotocol.AuthoringDecision{}, contractError(runtimeprotocol.RuntimeFailureCodeRuntimeAuthorizationStale, "authoring decision does not allow publication")
	}
	return decision, nil
}

func sameScope(left, right runtimeprotocol.RuntimeScope) bool {
	return left.DocumentID == right.DocumentID && left.LocalScopeID == right.LocalScopeID && left.AccessFingerprint == right.AccessFingerprint && reflect.DeepEqual(left.OrganizationScopeID, right.OrganizationScopeID)
}

func sameOptionalString(left, right *string) bool { return reflect.DeepEqual(left, right) }

func validateRevisionInScope(revision runtimeprotocol.CommittedRevisionRef, scope runtimeprotocol.RuntimeScope) *ContractError {
	if _, err := runtimeprotocol.EncodeCommittedRevisionRef(revision); err != nil {
		return contractError(runtimeprotocol.RuntimeFailureCodeRuntimeStaleRevision, "current revision is malformed")
	}
	if revision.DocumentID != scope.DocumentID {
		return contractError(runtimeprotocol.RuntimeFailureCodeRuntimeRevisionScopeMismatch, "current revision belongs to another document")
	}
	return nil
}

func canonicalizeAuthoringInput(input accessprotocol.EvaluateAuthoringInput) accessprotocol.EvaluateAuthoringInput {
	input.HostOperationImpacts = append([]accessprotocol.HostOperationImpact(nil), input.HostOperationImpacts...)
	for index := range input.HostOperationImpacts {
		impact := &input.HostOperationImpacts[index]
		impact.ResourceRefs = append([]string(nil), impact.ResourceRefs...)
		sort.Strings(impact.ResourceRefs)
	}
	sort.Slice(input.HostOperationImpacts, func(left, right int) bool {
		return input.HostOperationImpacts[left].ImpactDigest < input.HostOperationImpacts[right].ImpactDigest
	})
	return input
}

func validHostOperationImpact(impact accessprotocol.HostOperationImpact, scope runtimeprotocol.RuntimeScope) bool {
	if impact.ResourceScope.DocumentID != string(scope.DocumentID) || impact.ResourceScope.LocalScopeID != scope.LocalScopeID || !sameOptionalString(impact.ResourceScope.OrganizationScopeID, scope.OrganizationScopeID) {
		return false
	}
	want := semanticCapabilityForHostOperation(impact.OperationKind)
	if want == "" || len(impact.RequiredAuthoringCapabilities) != 1 || impact.RequiredAuthoringCapabilities[0] != want || !validHostOperationAction(impact.OperationKind, impact.Action) || len(impact.ResourceRefs) == 0 {
		return false
	}
	for index, ref := range impact.ResourceRefs {
		if ref == "" || (index > 0 && impact.ResourceRefs[index-1] >= ref) {
			return false
		}
	}
	candidate := impact
	candidate.ImpactDigest = ""
	return impact.ImpactDigest == digestValue(candidate)
}

func validHostOperationAction(kind accessprotocol.HostOperationKind, action string) bool {
	switch kind {
	case accessprotocol.HostOperationKindAssetDelete:
		return action == "delete"
	case accessprotocol.HostOperationKindAssetPersist:
		return action == "create" || action == "update"
	case accessprotocol.HostOperationKindAssetStage:
		return action == "stage"
	case accessprotocol.HostOperationKindPackageTransaction:
		return action == "create" || action == "update" || action == "delete"
	case accessprotocol.HostOperationKindBackendConfigure, accessprotocol.HostOperationKindProjectConfigure:
		return action == "update"
	default:
		return false
	}
}

func semanticCapabilityForHostOperation(kind accessprotocol.HostOperationKind) semantic.AuthoringCapability {
	switch kind {
	case accessprotocol.HostOperationKindAssetDelete, accessprotocol.HostOperationKindAssetPersist, accessprotocol.HostOperationKindAssetStage:
		return semantic.AuthoringCapabilityAssetWrite
	case accessprotocol.HostOperationKindPackageTransaction:
		return semantic.AuthoringCapabilityPackageManage
	case accessprotocol.HostOperationKindBackendConfigure, accessprotocol.HostOperationKindProjectConfigure:
		return semantic.AuthoringCapabilityProjectConfigure
	default:
		return ""
	}
}

func expired(value protocolcommon.Rfc3339Time, now time.Time) bool {
	parsed, err := time.Parse(time.RFC3339Nano, string(value))
	return err != nil || !now.Before(parsed)
}
