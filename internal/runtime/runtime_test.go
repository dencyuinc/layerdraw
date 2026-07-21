// SPDX-License-Identifier: LicenseRef-LayerDraw-1.0

package runtime

import (
	"context"
	"errors"
	"io"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/dencyuinc/layerdraw/gen/go/accessprotocol"
	"github.com/dencyuinc/layerdraw/gen/go/protocolcommon"
	"github.com/dencyuinc/layerdraw/gen/go/runtimeprotocol"
	"github.com/dencyuinc/layerdraw/gen/go/semantic"
	"github.com/dencyuinc/layerdraw/internal/runtime/port"
)

func TestNegotiationAdvertisesOnlyWiredCapabilities(t *testing.T) {
	implementations := Operations{CancelOperation: fakeRuntimeOperations{}, GetOperationResult: fakeRuntimeOperations{}}
	runtime := newTestRuntimeWithOperations(t, Ports{Recovery: fakeRecovery{}}, implementations)
	descriptor := runtime.Describe()
	want := []protocolcommon.CapabilityID{OperationCancelOperation, OperationGetOperationResult, OperationHandshake}
	if !equalCapabilities(descriptor.Operations, want) {
		t.Fatalf("operations=%v want=%v", descriptor.Operations, want)
	}

	request := handshakeRequest()
	request.RequiredCapabilities = []protocolcommon.CapabilityID{OperationGetOperationResult}
	request.OptionalCapabilities = []protocolcommon.CapabilityID{"runtime.future"}
	result, rejection := runtime.Negotiate(request)
	if rejection != nil {
		t.Fatal(rejection)
	}
	if len(result.CapabilityManifest.Operations) != 3 || !result.CapabilityManifest.Operations[OperationHandshake].Enabled || !result.CapabilityManifest.Operations[OperationGetOperationResult].Enabled {
		t.Fatalf("unexpected manifest: %+v", result.CapabilityManifest)
	}
	if len(result.CapabilityStatuses) != 2 || result.CapabilityStatuses[1].UnavailableReason == nil || *result.CapabilityStatuses[1].UnavailableReason != protocolcommon.UnavailableReasonUnsupported {
		t.Fatalf("unknown optional capability was not explicit: %+v", result.CapabilityStatuses)
	}
}

func TestRegistryOwnerOperationsAreUnavailableWithoutExplicitComposition(t *testing.T) {
	runtime := newTestRuntimeWithOperations(t, Ports{}, Operations{})
	if _, rejection := runtime.CommitRegistryPlan(context.Background(), RegistryCommitInput{}); rejection == nil || rejection.Code != runtimeprotocol.RuntimeFailureCodeRuntimeCapabilityUnavailable {
		t.Fatalf("Registry commit rejection=%v", rejection)
	}
	if _, rejection := runtime.CommitInitialRegistryTemplate(context.Background(), InitialRegistryCommitInput{}); rejection == nil || rejection.Code != runtimeprotocol.RuntimeFailureCodeRuntimeCapabilityUnavailable {
		t.Fatalf("initial Registry commit rejection=%v", rejection)
	}
	if rejection := runtime.CloseDocument(context.Background(), runtimeprotocol.RuntimeSessionRef{}); rejection == nil || rejection.Code != runtimeprotocol.RuntimeFailureCodeRuntimeCapabilityUnavailable {
		t.Fatalf("close rejection=%v", rejection)
	}
}

func TestHostOperationActionsAreClosedByOperationKind(t *testing.T) {
	tests := []struct {
		kind   accessprotocol.HostOperationKind
		action string
		valid  bool
	}{
		{accessprotocol.HostOperationKindAssetDelete, "delete", true},
		{accessprotocol.HostOperationKindAssetPersist, "create", true},
		{accessprotocol.HostOperationKindAssetPersist, "update", true},
		{accessprotocol.HostOperationKindAssetStage, "stage", true},
		{accessprotocol.HostOperationKindPackageTransaction, "delete", true},
		{accessprotocol.HostOperationKindBackendConfigure, "update", true},
		{accessprotocol.HostOperationKindProjectConfigure, "update", true},
		{accessprotocol.HostOperationKindAssetDelete, "update", false},
		{accessprotocol.HostOperationKind("future"), "update", false},
	}
	for _, test := range tests {
		if got := validHostOperationAction(test.kind, test.action); got != test.valid {
			t.Fatalf("kind=%q action=%q got=%v want=%v", test.kind, test.action, got, test.valid)
		}
	}
}

func TestNegotiationRejectsDigestAndMissingRequiredCapability(t *testing.T) {
	runtime := newTestRuntime(t, Ports{})
	tests := []struct {
		name string
		edit func(*runtimeprotocol.RuntimeHandshakeRequest)
	}{
		{"schema digest", func(request *runtimeprotocol.RuntimeHandshakeRequest) {
			request.Protocols[0].Versions[0].SchemaDigest = digest('f')
		}},
		{"required capability", func(request *runtimeprotocol.RuntimeHandshakeRequest) {
			request.RequiredCapabilities = []protocolcommon.CapabilityID{OperationCommitOperations}
		}},
		{"client byte limit unit", func(request *runtimeprotocol.RuntimeHandshakeRequest) {
			limits := testLimits("5")
			limits.MaxBlobBytes.Unit = runtimeprotocol.RuntimeByteLimitValueUnit("items")
			request.ClientLimits = &limits
		}},
		{"client item limit unit", func(request *runtimeprotocol.RuntimeHandshakeRequest) {
			limits := testLimits("5")
			limits.MaxCommitOperations.Unit = runtimeprotocol.RuntimeItemLimitValueUnit("bytes")
			request.ClientLimits = &limits
		}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			request := handshakeRequest()
			test.edit(&request)
			if _, rejection := runtime.Negotiate(request); rejection == nil || rejection.Code != runtimeprotocol.RuntimeFailureCodeRuntimeCapabilityUnavailable {
				t.Fatalf("rejection=%v", rejection)
			}
		})
	}
}

func TestNegotiationIntersectsClientLimits(t *testing.T) {
	runtime := newTestRuntime(t, Ports{})
	request := handshakeRequest()
	client := testLimits("5")
	request.ClientLimits = &client
	result, rejection := runtime.Negotiate(request)
	if rejection != nil {
		t.Fatal(rejection)
	}
	if result.CapabilityManifest.Limits.MaxBlobBytes.HardMaximum != "5" {
		t.Fatalf("limits were not intersected: %+v", result.CapabilityManifest.Limits)
	}
}

func TestRuntimeConfigurationRejectsEveryInvalidAuthority(t *testing.T) {
	valid := Config{ReleaseVersion: "0.0.0-dev", EndpointInstanceID: "runtime-test", ReleaseManifestDigest: digest('9'), Limits: testLimits("100")}
	tests := []struct {
		name string
		edit func(*Config)
	}{
		{"release", func(config *Config) { config.ReleaseVersion = "invalid" }},
		{"endpoint", func(config *Config) { config.EndpointInstanceID = "" }},
		{"manifest", func(config *Config) { config.ReleaseManifestDigest = "invalid" }},
		{"limits", func(config *Config) { config.Limits.MaxBlobBytes.HardMaximum = "0" }},
		{"byte limit unit", func(config *Config) {
			config.Limits.MaxBlobBytes.Unit = runtimeprotocol.RuntimeByteLimitValueUnit("items")
		}},
		{"item limit unit", func(config *Config) {
			config.Limits.MaxCommitOperations.Unit = runtimeprotocol.RuntimeItemLimitValueUnit("bytes")
		}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			config := valid
			test.edit(&config)
			if _, err := New(config); err == nil {
				t.Fatal("invalid configuration was accepted")
			}
		})
	}
}

func TestNegotiationCoversInvalidInputEnabledOptionalAndAllWiredPorts(t *testing.T) {
	fullyWired := Ports{
		Documents: fakeDocumentStore{}, State: fakeStateBackend{}, Assets: fakeAssetStore{}, History: fakeHistoryStore{},
		Recovery: fakeRecovery{}, Authoring: &fakeDecision{}, Clock: fixedClock{}, Identities: fakeIdentityGenerator{},
	}
	implementations := Operations{
		OpenDocument: fakeRuntimeOperations{}, CommitOperations: fakeRuntimeOperations{}, CancelOperation: fakeRuntimeOperations{},
		GetOperationResult: fakeRuntimeOperations{}, ListRevisions: fakeRuntimeOperations{},
	}
	runtime := newTestRuntimeWithOperations(t, fullyWired, implementations)
	want := []protocolcommon.CapabilityID{
		OperationCancelOperation, OperationCommitOperations, OperationGetOperationResult,
		OperationHandshake, OperationListRevisions, OperationOpenDocument,
	}
	if got := runtime.Describe().Operations; !equalCapabilities(got, want) {
		t.Fatalf("operations=%v want=%v", got, want)
	}
	if got := runtime.Describe().StorageCapabilities; strings.Join(got, ",") != "assets,conditional_document_head,history,recovery_journal,state" {
		t.Fatalf("storage capabilities=%v", got)
	}
	request := handshakeRequest()
	request.OptionalCapabilities = []protocolcommon.CapabilityID{OperationCommitOperations}
	result, rejection := runtime.Negotiate(request)
	if rejection != nil || len(result.CapabilityStatuses) != 2 || !result.CapabilityStatuses[1].Enabled {
		t.Fatalf("enabled optional capability result=%+v rejection=%v", result, rejection)
	}
	if _, rejection := runtime.Negotiate(runtimeprotocol.RuntimeHandshakeRequest{}); rejection == nil {
		t.Fatal("malformed handshake was accepted")
	}
	if got := minItemLimit(
		runtimeprotocol.RuntimeItemLimitValue{HardMaximum: "10", Unit: runtimeprotocol.RuntimeItemLimitValueUnitValue},
		runtimeprotocol.RuntimeItemLimitValue{HardMaximum: "20", Unit: runtimeprotocol.RuntimeItemLimitValueUnitValue},
	); got.HardMaximum != "10" {
		t.Fatalf("larger client limit changed the host maximum: %+v", got)
	}
	if got := minByteLimit(
		runtimeprotocol.RuntimeByteLimitValue{HardMaximum: "10", Unit: runtimeprotocol.RuntimeByteLimitValueUnitValue},
		runtimeprotocol.RuntimeByteLimitValue{HardMaximum: "1", Unit: runtimeprotocol.RuntimeByteLimitValueUnitValue},
	); got.HardMaximum != "1" {
		t.Fatalf("smaller client byte limit was not selected: %+v", got)
	}
	if got := minByteLimit(
		runtimeprotocol.RuntimeByteLimitValue{HardMaximum: "10", Unit: runtimeprotocol.RuntimeByteLimitValueUnitValue},
		runtimeprotocol.RuntimeByteLimitValue{HardMaximum: "20", Unit: runtimeprotocol.RuntimeByteLimitValueUnitValue},
	); got.HardMaximum != "10" {
		t.Fatalf("larger client byte limit changed the host maximum: %+v", got)
	}
	if got := minItemLimit(
		runtimeprotocol.RuntimeItemLimitValue{HardMaximum: "10", Unit: runtimeprotocol.RuntimeItemLimitValueUnitValue},
		runtimeprotocol.RuntimeItemLimitValue{HardMaximum: "1", Unit: runtimeprotocol.RuntimeItemLimitValueUnitValue},
	); got.HardMaximum != "1" {
		t.Fatalf("smaller client item limit was not selected: %+v", got)
	}
	if !decimalLess("9", "10") || decimalLess("20", "10") {
		t.Fatal("canonical decimal comparison is incorrect")
	}
	if _, rejection := runtime.OpenDocument(context.Background(), runtimeprotocol.OpenRuntimeDocumentInput{}); rejection != nil {
		t.Fatal(rejection)
	}
	if _, rejection := runtime.CommitOperations(context.Background(), runtimeprotocol.RuntimeCommitInput{}); rejection != nil {
		t.Fatal(rejection)
	}
	if _, rejection := runtime.CancelOperation(context.Background(), runtimeprotocol.CancelOperationInput{}); rejection != nil {
		t.Fatal(rejection)
	}
	if _, rejection := runtime.GetOperationResult(context.Background(), runtimeprotocol.GetOperationResultInput{}); rejection != nil {
		t.Fatal(rejection)
	}
	if _, rejection := runtime.ListRevisions(context.Background(), runtimeprotocol.ListRevisionsInput{}); rejection != nil {
		t.Fatal(rejection)
	}
	unimplemented := newTestRuntime(t, Ports{})
	if _, rejection := unimplemented.OpenDocument(context.Background(), runtimeprotocol.OpenRuntimeDocumentInput{}); rejection == nil {
		t.Fatal("unimplemented operation was callable")
	}
	if _, rejection := unimplemented.CommitOperations(context.Background(), runtimeprotocol.RuntimeCommitInput{}); rejection == nil {
		t.Fatal("unimplemented commit was callable")
	}
	if _, rejection := unimplemented.CancelOperation(context.Background(), runtimeprotocol.CancelOperationInput{}); rejection == nil {
		t.Fatal("unimplemented cancellation was callable")
	}
	if _, rejection := unimplemented.GetOperationResult(context.Background(), runtimeprotocol.GetOperationResultInput{}); rejection == nil {
		t.Fatal("unimplemented result lookup was callable")
	}
	if _, rejection := unimplemented.ListRevisions(context.Background(), runtimeprotocol.ListRevisionsInput{}); rejection == nil {
		t.Fatal("unimplemented history was callable")
	}
}

func TestScopeContractsRejectCrossDocumentStaleExpiredAndMalformedUse(t *testing.T) {
	now := time.Date(2026, 7, 18, 10, 0, 0, 0, time.UTC)
	binding := testBinding(now.Add(time.Hour))
	tests := []struct {
		name string
		edit func(*runtimeprotocol.RuntimeSessionRef)
		code runtimeprotocol.RuntimeFailureCode
	}{
		{"cross document", func(value *runtimeprotocol.RuntimeSessionRef) { value.Scope.DocumentID = "doc_other" }, runtimeprotocol.RuntimeFailureCodeRuntimeCrossDocumentHandle},
		{"stale generation", func(value *runtimeprotocol.RuntimeSessionRef) { value.SessionGeneration = "2" }, runtimeprotocol.RuntimeFailureCodeRuntimeStaleSessionGeneration},
		{"stripped expiry", func(value *runtimeprotocol.RuntimeSessionRef) { value.ExpiresAt = nil }, runtimeprotocol.RuntimeFailureCodeRuntimeMalformedHandle},
		{"extended expiry", func(value *runtimeprotocol.RuntimeSessionRef) {
			expires := protocolcommon.Rfc3339Time(now.Add(2 * time.Hour).Format(time.RFC3339))
			value.ExpiresAt = &expires
		}, runtimeprotocol.RuntimeFailureCodeRuntimeMalformedHandle},
		{"malformed", func(value *runtimeprotocol.RuntimeSessionRef) { value.RuntimeSessionID = "short" }, runtimeprotocol.RuntimeFailureCodeRuntimeMalformedHandle},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			candidate := binding.Session
			test.edit(&candidate)
			err := ValidateSessionUse(candidate, binding, binding.Session.Scope, now)
			if err == nil || err.Code != test.code {
				t.Fatalf("error=%v want=%s", err, test.code)
			}
		})
	}
	if err := ValidateSessionUse(binding.Session, binding, binding.Session.Scope, now); err != nil {
		t.Fatalf("valid session rejected: %v", err)
	}
	expiredBinding := testBinding(now.Add(-time.Second))
	if err := ValidateSessionUse(expiredBinding.Session, expiredBinding, expiredBinding.Session.Scope, now); err == nil || err.Code != runtimeprotocol.RuntimeFailureCodeRuntimeSessionExpired {
		t.Fatalf("trusted expiry error=%v", err)
	}
	unknown := binding.Session
	unknown.RuntimeSessionID = "runtime_session_unknown"
	if err := ValidateSessionUse(unknown, binding, binding.Session.Scope, now); err == nil || err.Code != runtimeprotocol.RuntimeFailureCodeRuntimeMalformedHandle {
		t.Fatalf("unknown session error=%v", err)
	}
	changedScope := binding.Session.Scope
	changedScope.AccessFingerprint = digest('f')
	if err := ValidateSessionUse(binding.Session, binding, changedScope, now); err == nil || err.Code != runtimeprotocol.RuntimeFailureCodeRuntimeAuthorizationStale {
		t.Fatalf("changed access scope error=%v", err)
	}
}

func TestRevisionBlobCursorAndIdempotencyContracts(t *testing.T) {
	now := time.Date(2026, 7, 18, 10, 0, 0, 0, time.UTC)
	binding := testBinding(now.Add(time.Hour))
	revision := binding.CurrentRevision
	if err := ValidateRevisionUse(revision, binding); err != nil {
		t.Fatalf("valid revision rejected: %v", err)
	}
	revision.DocumentID = "doc_other"
	if err := ValidateRevisionUse(revision, binding); err == nil || err.Code != runtimeprotocol.RuntimeFailureCodeRuntimeRevisionScopeMismatch {
		t.Fatalf("revision error=%v", err)
	}
	revision = binding.CurrentRevision
	revision.RevisionID = "rev_stale"
	if err := ValidateRevisionUse(revision, binding); err == nil || err.Code != runtimeprotocol.RuntimeFailureCodeRuntimeStaleRevision {
		t.Fatalf("stale revision error=%v", err)
	}
	revision = binding.CurrentRevision
	revision.RevisionID = ""
	if err := ValidateRevisionUse(revision, binding); err == nil || err.Code != runtimeprotocol.RuntimeFailureCodeRuntimeStaleRevision {
		t.Fatalf("malformed revision error=%v", err)
	}
	revision = binding.CurrentRevision
	alteredProvider := runtimeprotocol.ProviderVersionToken("provider-v2")
	revision.ProviderVersion = &alteredProvider
	if err := ValidateRevisionUse(revision, binding); err == nil || err.Code != runtimeprotocol.RuntimeFailureCodeRuntimeStaleRevision {
		t.Fatalf("altered provider version error=%v", err)
	}
	revision = binding.CurrentRevision
	revision.ProviderVersion = nil
	if err := ValidateRevisionUse(revision, binding); err == nil || err.Code != runtimeprotocol.RuntimeFailureCodeRuntimeStaleRevision {
		t.Fatalf("stripped provider version error=%v", err)
	}

	blob := runtimeprotocol.RuntimeBlobRef{
		Blob:  protocolcommon.BlobRef{BlobID: "blob", Digest: digest('b'), Lifetime: protocolcommon.BlobLifetimeSession, MediaType: "application/octet-stream", Size: "1"},
		Scope: binding.Session.Scope, SessionGeneration: binding.Session.SessionGeneration,
	}
	blobExpiresAt := protocolcommon.Rfc3339Time(now.Add(time.Hour).Format(time.RFC3339))
	blob.ExpiresAt = &blobExpiresAt
	issued := BlobBinding{Blob: blob}
	if err := ValidateBlobUse(blob, issued, binding, now); err != nil {
		t.Fatalf("valid blob rejected: %v", err)
	}
	alteredIdentity := blob
	alteredIdentity.Blob.BlobID = "other_blob"
	if err := ValidateBlobUse(alteredIdentity, issued, binding, now); err == nil || err.Code != runtimeprotocol.RuntimeFailureCodeRuntimeBlobScopeMismatch {
		t.Fatalf("altered blob identity error=%v", err)
	}
	strippedExpiry := blob
	strippedExpiry.ExpiresAt = nil
	if err := ValidateBlobUse(strippedExpiry, issued, binding, now); err == nil || err.Code != runtimeprotocol.RuntimeFailureCodeRuntimeBlobScopeMismatch {
		t.Fatalf("stripped blob expiry error=%v", err)
	}
	extendedExpiry := blob
	extended := protocolcommon.Rfc3339Time(now.Add(2 * time.Hour).Format(time.RFC3339))
	extendedExpiry.ExpiresAt = &extended
	if err := ValidateBlobUse(extendedExpiry, issued, binding, now); err == nil || err.Code != runtimeprotocol.RuntimeFailureCodeRuntimeBlobScopeMismatch {
		t.Fatalf("extended blob expiry error=%v", err)
	}
	blob.Scope.AccessFingerprint = digest('c')
	if err := ValidateBlobUse(blob, issued, binding, now); err == nil || err.Code != runtimeprotocol.RuntimeFailureCodeRuntimeBlobScopeMismatch {
		t.Fatalf("blob error=%v", err)
	}
	blob.Scope = binding.Session.Scope
	expiredAt := protocolcommon.Rfc3339Time(now.Add(-time.Second).Format(time.RFC3339))
	issued.Blob.ExpiresAt = &expiredAt
	blob.ExpiresAt = &expiredAt
	if err := ValidateBlobUse(blob, issued, binding, now); err == nil || err.Code != runtimeprotocol.RuntimeFailureCodeRuntimeBlobExpired {
		t.Fatalf("expired blob error=%v", err)
	}
	issued.Blob.ExpiresAt = &blobExpiresAt
	blob.ExpiresAt = &blobExpiresAt
	blob.Blob.BlobID = ""
	if err := ValidateBlobUse(blob, issued, binding, now); err == nil || err.Code != runtimeprotocol.RuntimeFailureCodeRuntimeBlobScopeMismatch {
		t.Fatalf("malformed blob error=%v", err)
	}

	cursor := runtimeprotocol.RuntimeCursorBinding{
		Cursor: runtimeprotocol.RuntimeCursor(strings.Repeat("c", 32)), ExpiresAt: protocolcommon.Rfc3339Time(now.Add(time.Hour).Format(time.RFC3339)),
		NormalizedRequestDigest: digest('d'), Operation: OperationListRevisions, Revision: binding.CurrentRevision,
		SchemaVersion: 1, Scope: binding.Session.Scope,
	}
	issuedCursor := CursorBinding{Cursor: cursor}
	if err := ValidateCursorUse(cursor, issuedCursor, binding, OperationListRevisions, digest('d'), now); err != nil {
		t.Fatalf("valid cursor rejected: %v", err)
	}
	if err := ValidateCursorUse(cursor, issuedCursor, binding, OperationListRevisions, digest('e'), now); err == nil || err.Code != runtimeprotocol.RuntimeFailureCodeRuntimeCursorScopeMismatch {
		t.Fatalf("cursor error=%v", err)
	}
	for _, test := range []struct {
		name string
		edit func(*runtimeprotocol.RuntimeCursorBinding)
	}{
		{"altered token", func(value *runtimeprotocol.RuntimeCursorBinding) {
			value.Cursor = runtimeprotocol.RuntimeCursor(strings.Repeat("d", 32))
		}},
		{"extended expiry", func(value *runtimeprotocol.RuntimeCursorBinding) {
			value.ExpiresAt = protocolcommon.Rfc3339Time(now.Add(2 * time.Hour).Format(time.RFC3339))
		}},
		{"altered schema version", func(value *runtimeprotocol.RuntimeCursorBinding) { value.SchemaVersion = 2 }},
		{"altered revision provider version", func(value *runtimeprotocol.RuntimeCursorBinding) {
			providerVersion := runtimeprotocol.ProviderVersionToken("provider-v2")
			value.Revision.ProviderVersion = &providerVersion
		}},
	} {
		t.Run("cursor "+test.name, func(t *testing.T) {
			candidate := cursor
			test.edit(&candidate)
			if err := ValidateCursorUse(candidate, issuedCursor, binding, OperationListRevisions, digest('d'), now); err == nil || err.Code != runtimeprotocol.RuntimeFailureCodeRuntimeInvalidCursor {
				t.Fatalf("error=%v", err)
			}
		})
	}
	expiredCursor := cursor
	expiredCursor.ExpiresAt = protocolcommon.Rfc3339Time(now.Add(-time.Second).Format(time.RFC3339))
	if err := ValidateCursorUse(expiredCursor, CursorBinding{Cursor: expiredCursor}, binding, OperationListRevisions, digest('d'), now); err == nil || err.Code != runtimeprotocol.RuntimeFailureCodeRuntimeInvalidCursor {
		t.Fatalf("expired cursor error=%v", err)
	}
	malformedCursor := cursor
	malformedCursor.Cursor = "short"
	if err := ValidateCursorUse(malformedCursor, issuedCursor, binding, OperationListRevisions, digest('d'), now); err == nil || err.Code != runtimeprotocol.RuntimeFailureCodeRuntimeInvalidCursor {
		t.Fatalf("malformed cursor error=%v", err)
	}
	if err := ValidateIdempotencyRetry("idem_key_12345678", digest('a'), "idem_key_12345678", digest('b')); err == nil || err.Code != runtimeprotocol.RuntimeFailureCodeRuntimeIdempotencyMismatch {
		t.Fatalf("idempotency error=%v", err)
	}
	if err := ValidateIdempotencyRetry("idem_key_12345678", digest('a'), "idem_key_87654321", digest('b')); err != nil {
		t.Fatalf("different idempotency key was rejected: %v", err)
	}
}

func TestRecoveryStateMachineAcceptsOnlyNormativeTransitions(t *testing.T) {
	allowed := [][2]runtimeprotocol.RecoveryPhase{
		{runtimeprotocol.RecoveryPhasePending, runtimeprotocol.RecoveryPhaseStaged},
		{runtimeprotocol.RecoveryPhaseStaged, runtimeprotocol.RecoveryPhasePublicationPending},
		{runtimeprotocol.RecoveryPhasePublicationPending, runtimeprotocol.RecoveryPhasePublished},
		{runtimeprotocol.RecoveryPhasePublicationPending, runtimeprotocol.RecoveryPhaseRecovering},
		{runtimeprotocol.RecoveryPhasePublished, runtimeprotocol.RecoveryPhaseStatePending},
		{runtimeprotocol.RecoveryPhasePublished, runtimeprotocol.RecoveryPhaseRecovering},
		{runtimeprotocol.RecoveryPhaseStatePending, runtimeprotocol.RecoveryPhaseAuditPending},
		{runtimeprotocol.RecoveryPhaseStatePending, runtimeprotocol.RecoveryPhaseRecovering},
		{runtimeprotocol.RecoveryPhaseAuditPending, runtimeprotocol.RecoveryPhaseOutboxReady},
		{runtimeprotocol.RecoveryPhaseAuditPending, runtimeprotocol.RecoveryPhaseRecovering},
		{runtimeprotocol.RecoveryPhaseOutboxReady, runtimeprotocol.RecoveryPhaseFinal},
		{runtimeprotocol.RecoveryPhaseRecovering, runtimeprotocol.RecoveryPhaseFinal},
		{runtimeprotocol.RecoveryPhaseRecovering, runtimeprotocol.RecoveryPhaseNeedsReview},
	}
	for _, transition := range allowed {
		if rejection := ValidateRecoveryTransition(transition[0], transition[1]); rejection != nil {
			t.Fatalf("valid transition %s -> %s rejected: %v", transition[0], transition[1], rejection)
		}
	}
	for _, transition := range [][2]runtimeprotocol.RecoveryPhase{
		{runtimeprotocol.RecoveryPhasePending, runtimeprotocol.RecoveryPhasePublished},
		{runtimeprotocol.RecoveryPhaseFinal, runtimeprotocol.RecoveryPhasePending},
		{runtimeprotocol.RecoveryPhaseNeedsReview, runtimeprotocol.RecoveryPhaseFinal},
	} {
		if rejection := ValidateRecoveryTransition(transition[0], transition[1]); rejection == nil || rejection.Code != runtimeprotocol.RuntimeFailureCodeRuntimeInvalidRecoveryTransition {
			t.Fatalf("invalid transition %s -> %s rejection=%v", transition[0], transition[1], rejection)
		}
	}
}

func TestAuthorizeBindsCurrentRevisionAndProofToRequestScope(t *testing.T) {
	now := time.Date(2026, 7, 18, 10, 0, 0, 0, time.UTC)
	for _, test := range []struct {
		name string
		edit func(*AuthorizationRequest)
		want runtimeprotocol.RuntimeFailureCode
	}{
		{"cross-document current revision and proof", func(request *AuthorizationRequest) {
			request.CurrentRevision.DocumentID = "doc_other"
			request.Proof.BaseRevision.DocumentID = "doc_other"
		}, runtimeprotocol.RuntimeFailureCodeRuntimeRevisionScopeMismatch},
		{"malformed current revision", func(request *AuthorizationRequest) {
			request.CurrentRevision.RevisionID = ""
		}, runtimeprotocol.RuntimeFailureCodeRuntimeStaleRevision},
	} {
		t.Run(test.name, func(t *testing.T) {
			request, decision := authorizationFixture(now)
			test.edit(&request)
			decider := &fakeDecision{decision: decision}
			runtime := newTestRuntime(t, Ports{Authoring: decider, Clock: fixedClock{now}})
			if _, rejection := runtime.Authorize(context.Background(), request); rejection == nil || rejection.Code != test.want {
				t.Fatalf("rejection=%v want=%s", rejection, test.want)
			}
			if decider.calls != 0 {
				t.Fatalf("Access called before current revision validation: calls=%d", decider.calls)
			}
		})
	}

	for _, test := range []struct {
		name string
		edit func(*runtimeprotocol.AuthoringProof)
	}{
		{"cross-document proof", func(proof *runtimeprotocol.AuthoringProof) { proof.BaseRevision.DocumentID = "doc_other" }},
		{"malformed proof revision", func(proof *runtimeprotocol.AuthoringProof) { proof.BaseRevision.RevisionID = "" }},
	} {
		t.Run(test.name, func(t *testing.T) {
			request, decision := authorizationFixture(now)
			test.edit(request.Proof)
			decider := &fakeDecision{decision: decision}
			runtime := newTestRuntime(t, Ports{Authoring: decider, Clock: fixedClock{now}})
			if _, rejection := runtime.Authorize(context.Background(), request); rejection == nil || rejection.Code != runtimeprotocol.RuntimeFailureCodeRuntimeAuthorizationProofInvalid {
				t.Fatalf("rejection=%v", rejection)
			}
			if decider.calls != 1 {
				t.Fatalf("valid current revision did not reach Access exactly once: calls=%d", decider.calls)
			}
		})
	}
}

func TestAuthorizeRejectsEveryStaleOrMalformedBoundary(t *testing.T) {
	now := time.Date(2026, 7, 18, 10, 0, 0, 0, time.UTC)
	request, decision := authorizationFixture(now)

	withoutPort := newTestRuntime(t, Ports{})
	if _, rejection := withoutPort.Authorize(context.Background(), request); rejection == nil || rejection.Code != runtimeprotocol.RuntimeFailureCodeRuntimeCapabilityUnavailable {
		t.Fatalf("missing port rejection=%v", rejection)
	}

	tests := []struct {
		name      string
		edit      func(*AuthorizationRequest, *accessprotocol.AuthoringDecision)
		portError error
		want      runtimeprotocol.RuntimeFailureCode
	}{
		{"malformed input", func(request *AuthorizationRequest, _ *accessprotocol.AuthoringDecision) {
			request.Evaluation = accessprotocol.EvaluateAuthoringInput{}
		}, nil, runtimeprotocol.RuntimeFailureCodeRuntimeAuthorizationProofInvalid},
		{"revision digest mismatch", func(request *AuthorizationRequest, _ *accessprotocol.AuthoringDecision) {
			request.Evaluation.BaseRevisionDigest = digest('f')
		}, nil, runtimeprotocol.RuntimeFailureCodeRuntimeAuthorizationProofInvalid},
		{"grant scope", func(request *AuthorizationRequest, _ *accessprotocol.AuthoringDecision) {
			request.Evaluation.GrantSnapshot.HostDocumentID = "doc_other"
		}, nil, runtimeprotocol.RuntimeFailureCodeRuntimeAuthorizationStale},
		{"host impact descriptor", func(request *AuthorizationRequest, _ *accessprotocol.AuthoringDecision) {
			request.Evaluation.HostOperationImpacts[0].RequiredAuthoringCapabilities = []semantic.AuthoringCapability{semantic.AuthoringCapabilityGraphWrite}
		}, nil, runtimeprotocol.RuntimeFailureCodeRuntimeAuthorizationProofInvalid},
		{"expired grant", func(request *AuthorizationRequest, _ *accessprotocol.AuthoringDecision) {
			value := protocolcommon.Rfc3339Time(now.Add(-time.Second).Format(time.RFC3339))
			request.Evaluation.GrantSnapshot.ExpiresAt = &value
		}, nil, runtimeprotocol.RuntimeFailureCodeRuntimeAuthorizationStale},
		{"decision provider", func(*AuthorizationRequest, *accessprotocol.AuthoringDecision) {}, errors.New("provider failed"), runtimeprotocol.RuntimeFailureCodeRuntimeAuthorizationStale},
		{"malformed decision", func(_ *AuthorizationRequest, decision *accessprotocol.AuthoringDecision) {
			*decision = accessprotocol.AuthoringDecision{}
		}, nil, runtimeprotocol.RuntimeFailureCodeRuntimeAuthorizationProofInvalid},
		{"changed access", func(_ *AuthorizationRequest, decision *accessprotocol.AuthoringDecision) {
			decision.AccessFingerprint = digest('f')
		}, nil, runtimeprotocol.RuntimeFailureCodeRuntimeAuthorizationStale},
		{"invalid proof", func(request *AuthorizationRequest, _ *accessprotocol.AuthoringDecision) {
			request.Proof.DecisionDigest = digest('f')
		}, nil, runtimeprotocol.RuntimeFailureCodeRuntimeAuthorizationProofInvalid},
		{"expired proof", func(request *AuthorizationRequest, _ *accessprotocol.AuthoringDecision) {
			value := protocolcommon.Rfc3339Time(now.Add(-time.Second).Format(time.RFC3339))
			request.Proof.ExpiresAt = &value
		}, nil, runtimeprotocol.RuntimeFailureCodeRuntimeAuthorizationStale},
		{"approval required", func(_ *AuthorizationRequest, decision *accessprotocol.AuthoringDecision) {
			decision.Outcome = accessprotocol.AuthoringDecisionOutcomeApprovalRequired
			decision.ApprovalRuleRefs = []string{"rule"}
		}, nil, runtimeprotocol.RuntimeFailureCodeRuntimeAuthorizationStale},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			candidateRequest := request
			candidateRequest.Evaluation.HostOperationImpacts = append([]accessprotocol.HostOperationImpact(nil), request.Evaluation.HostOperationImpacts...)
			candidateRequest.Evaluation.GrantSnapshot = request.Evaluation.GrantSnapshot
			proof := *request.Proof
			candidateRequest.Proof = &proof
			candidateDecision := decision
			test.edit(&candidateRequest, &candidateDecision)
			decider := &fakeDecision{decision: candidateDecision, err: test.portError}
			runtime := newTestRuntime(t, Ports{Authoring: decider, Clock: fixedClock{now}})
			if _, rejection := runtime.Authorize(context.Background(), candidateRequest); rejection == nil || rejection.Code != test.want {
				t.Fatalf("rejection=%v want=%s", rejection, test.want)
			}
		})
	}

	for _, kind := range []accessprotocol.HostOperationKind{
		accessprotocol.HostOperationKindAssetDelete,
		accessprotocol.HostOperationKindAssetPersist,
		accessprotocol.HostOperationKindPackageTransaction,
		accessprotocol.HostOperationKindBackendConfigure,
		accessprotocol.HostOperationKindProjectConfigure,
	} {
		capability := semanticCapabilityForHostOperation(kind)
		if capability == "" {
			t.Fatalf("operation %s has no capability", kind)
		}
	}
	if capability := semanticCapabilityForHostOperation(accessprotocol.HostOperationKind("unknown")); capability != "" {
		t.Fatalf("unknown operation capability=%s", capability)
	}
	if message := contractError(runtimeprotocol.RuntimeFailureCodeRuntimeCancelled, "cancelled").Error(); message != "runtime.cancelled: cancelled" {
		t.Fatalf("contract error=%q", message)
	}
}

func TestAuthorizeUsesInjectedDecisionForFullLocalGrantAndBindsAllImpacts(t *testing.T) {
	now := time.Date(2026, 7, 18, 10, 0, 0, 0, time.UTC)
	grant := testGrant(now)
	hostImpact := accessprotocol.HostOperationImpact{
		Action: "stage", OperationKind: accessprotocol.HostOperationKindAssetStage,
		RequiredAuthoringCapabilities: []semantic.AuthoringCapability{semantic.AuthoringCapabilityAssetWrite}, ResourceRefs: []string{"asset"},
		ResourceScope: accessprotocol.HostResourceScope{DocumentID: grant.HostDocumentID, LocalScopeID: grant.LocalScopeID},
	}
	hostImpact = sealHostImpact(hostImpact)
	engineImpact := semantic.AuthoringImpact{
		BaseDefinitionHash: digest('7'), Entries: []semantic.AuthoringImpactEntry{}, ImpactDigest: digest('8'),
		RequiredCapabilities: []semantic.AuthoringCapability{}, ResultingDefinitionHash: digest('9'),
		SemanticDiffHash: digest('a'), SourceDiffHash: digest('b'),
	}
	decision := accessprotocol.AuthoringDecision{
		AuthoringImpactDigest: &engineImpact.ImpactDigest,
		AccessFingerprint:     grant.AccessFingerprint, ApprovalRuleRefs: []string{}, ConstraintViolations: []accessprotocol.ConstraintViolation{},
		DecisionDigest: digest('4'), Diagnostics: []protocolcommon.ProtocolDiagnostic{}, EvaluationDigest: digest('5'),
		HostOperationImpactDigests: []protocolcommon.Digest{hostImpact.ImpactDigest}, MissingCapabilities: []semantic.AuthoringCapability{},
		Outcome: accessprotocol.AuthoringDecisionOutcomeAllow, RequiredCapabilities: []semantic.AuthoringCapability{semantic.AuthoringCapabilityAssetWrite},
	}
	decider := &fakeDecision{decision: decision}
	runtime := newTestRuntime(t, Ports{Authoring: decider, Clock: fixedClock{now}})
	input := accessprotocol.EvaluateAuthoringInput{RequestIntent: "apply", AuthoringImpact: &engineImpact, HostOperationImpacts: []accessprotocol.HostOperationImpact{hostImpact}, GrantSnapshot: grant}
	proof := runtimeprotocol.AuthoringProof{
		AccessFingerprint: grant.AccessFingerprint, BaseRevision: testRevision(), DecisionDigest: decision.DecisionDigest,
		EvaluationDigest: decision.EvaluationDigest, MembershipVersion: grant.MembershipVersion, PolicyRefs: grant.PolicyRefs,
	}
	request := AuthorizationRequest{Scope: runtimeprotocol.RuntimeScope{AccessFingerprint: grant.AccessFingerprint, DocumentID: "doc_one", LocalScopeID: "local"}, CurrentRevision: testRevision(), Evaluation: input, Proof: &proof}
	if _, rejection := runtime.Authorize(context.Background(), request); rejection != nil {
		t.Fatal(rejection)
	}
	if decider.calls != 1 {
		t.Fatalf("full local grant bypassed injected decision port: calls=%d", decider.calls)
	}
	if decider.input.BaseRevisionDigest != digestValue(testRevision()) {
		t.Fatalf("Access input revision digest=%s want=%s", decider.input.BaseRevisionDigest, digestValue(testRevision()))
	}
	decider.decision.HostOperationImpactDigests = []protocolcommon.Digest{digest('6')}
	if _, rejection := runtime.Authorize(context.Background(), request); rejection == nil || rejection.Code != runtimeprotocol.RuntimeFailureCodeRuntimeAuthorizationProofInvalid {
		t.Fatalf("rejection=%v", rejection)
	}
	decider.decision.HostOperationImpactDigests = []protocolcommon.Digest{hostImpact.ImpactDigest}
	wrongEngineImpact := digest('c')
	decider.decision.AuthoringImpactDigest = &wrongEngineImpact
	if _, rejection := runtime.Authorize(context.Background(), request); rejection == nil || rejection.Code != runtimeprotocol.RuntimeFailureCodeRuntimeAuthorizationProofInvalid {
		t.Fatalf("Engine impact rejection=%v", rejection)
	}
}

func TestHostOperationImpactValidationFailsClosedAcrossScopeAndMapping(t *testing.T) {
	organization := "org"
	scope := runtimeprotocol.RuntimeScope{DocumentID: "doc", LocalScopeID: "local", OrganizationScopeID: &organization}
	base := sealHostImpact(accessprotocol.HostOperationImpact{Action: "stage", OperationKind: accessprotocol.HostOperationKindAssetStage, RequiredAuthoringCapabilities: []semantic.AuthoringCapability{semantic.AuthoringCapabilityAssetWrite}, ResourceRefs: []string{"asset"}, ResourceScope: accessprotocol.HostResourceScope{DocumentID: "doc", LocalScopeID: "local", OrganizationScopeID: &organization}})
	if !validHostOperationImpact(base, scope) {
		t.Fatal("valid host impact rejected")
	}
	for _, mutate := range []func(*accessprotocol.HostOperationImpact){
		func(impact *accessprotocol.HostOperationImpact) { impact.ResourceScope.DocumentID = "other" },
		func(impact *accessprotocol.HostOperationImpact) { impact.ResourceScope.LocalScopeID = "other" },
		func(impact *accessprotocol.HostOperationImpact) { impact.ResourceScope.OrganizationScopeID = nil },
		func(impact *accessprotocol.HostOperationImpact) { impact.RequiredAuthoringCapabilities = nil },
		func(impact *accessprotocol.HostOperationImpact) {
			impact.RequiredAuthoringCapabilities = append(impact.RequiredAuthoringCapabilities, semantic.AuthoringCapabilityGraphWrite)
		},
		func(impact *accessprotocol.HostOperationImpact) {
			impact.RequiredAuthoringCapabilities[0] = semantic.AuthoringCapabilityGraphWrite
		},
		func(impact *accessprotocol.HostOperationImpact) {
			impact.OperationKind = accessprotocol.HostOperationKind("unknown")
		},
		func(impact *accessprotocol.HostOperationImpact) { impact.Action = "update" },
		func(impact *accessprotocol.HostOperationImpact) { impact.ResourceRefs = []string{"z", "a"} },
		func(impact *accessprotocol.HostOperationImpact) { impact.ImpactDigest = digest('f') },
	} {
		candidate := base
		candidate.RequiredAuthoringCapabilities = append([]semantic.AuthoringCapability(nil), base.RequiredAuthoringCapabilities...)
		mutate(&candidate)
		if validHostOperationImpact(candidate, scope) {
			t.Fatalf("invalid host impact accepted: %+v", candidate)
		}
	}
}

func TestExternalMaterializationValidationRejectsAmbiguousAndUnsafeTrees(t *testing.T) {
	if !validExternalMaterialization(port.ExternalMaterialization{Kind: port.ExternalFileKindContainer, Container: []byte("container")}) {
		t.Fatal("valid container rejected")
	}
	if validExternalMaterialization(port.ExternalMaterialization{Kind: port.ExternalFileKindContainer, Container: []byte("container"), ProjectFiles: []port.ExternalProjectFile{{Path: "document.ldl"}}}) {
		t.Fatal("mixed container/project accepted")
	}
	validProject := port.ExternalMaterialization{Kind: port.ExternalFileKindProject, ProjectFiles: []port.ExternalProjectFile{{Path: "document.ldl"}, {Path: "model/types.ldl"}}}
	if !validExternalMaterialization(validProject) {
		t.Fatal("valid project tree rejected")
	}
	for _, files := range [][]port.ExternalProjectFile{
		nil,
		{{Path: "/absolute.ldl"}},
		{{Path: "../escape.ldl"}},
		{{Path: "model/../document.ldl"}},
		{{Path: "document.txt"}},
		{{Path: "document.ldl"}, {Path: "document.ldl"}},
	} {
		if validExternalMaterialization(port.ExternalMaterialization{Kind: port.ExternalFileKindProject, ProjectFiles: files}) {
			t.Fatalf("unsafe project tree accepted: %+v", files)
		}
	}
	if validExternalMaterialization(port.ExternalMaterialization{Kind: port.ExternalFileKind("unknown")}) {
		t.Fatal("unknown external materialization accepted")
	}
}

func TestAuthorizeCanonicalizesEquivalentHostImpactSetsBeforeDecision(t *testing.T) {
	now := time.Date(2026, 7, 18, 10, 0, 0, 0, time.UTC)
	request, decision := authorizationFixture(now)
	second := request.Evaluation.HostOperationImpacts[0]
	second.OperationKind = accessprotocol.HostOperationKindAssetPersist
	second.Action = "create"
	second.ResourceRefs = []string{"asset-a", "asset-b"}
	second = sealHostImpact(second)
	request.Evaluation.HostOperationImpacts = []accessprotocol.HostOperationImpact{request.Evaluation.HostOperationImpacts[0], second}
	expected := canonicalizeAuthoringInput(request.Evaluation).HostOperationImpacts
	decision.HostOperationImpactDigests = []protocolcommon.Digest{expected[0].ImpactDigest, expected[1].ImpactDigest}
	decider := &fakeDecision{decision: decision}
	runtime := newTestRuntime(t, Ports{Authoring: decider, Clock: fixedClock{now}})
	if _, rejection := runtime.Authorize(context.Background(), request); rejection != nil {
		t.Fatal(rejection)
	}
	if !reflect.DeepEqual(decider.input.HostOperationImpacts, expected) {
		t.Fatalf("decision input was not canonicalized: %+v", decider.input.HostOperationImpacts)
	}
}

func newTestRuntime(t *testing.T, ports Ports) *Runtime {
	return newTestRuntimeWithOperations(t, ports, Operations{})
}

func newTestRuntimeWithOperations(t *testing.T, ports Ports, operations Operations) *Runtime {
	t.Helper()
	value, err := New(Config{ReleaseVersion: "0.0.0-dev", EndpointInstanceID: "runtime-test", ReleaseManifestDigest: digest('9'), Limits: testLimits("100"), Ports: ports, Operations: operations})
	if err != nil {
		t.Fatal(err)
	}
	return value
}

func handshakeRequest() runtimeprotocol.RuntimeHandshakeRequest {
	return runtimeprotocol.RuntimeHandshakeRequest{
		ClientRelease: "1.0.0", Protocols: []protocolcommon.ProtocolOffer{{Name: ProtocolName, SupportedRange: "1.0..1.0", Versions: []protocolcommon.ProtocolVersionBinding{{Version: ProtocolVersion, SchemaDigest: protocolcommon.Digest(runtimeprotocol.SchemaDigest)}}}},
		RequiredCapabilities: []protocolcommon.CapabilityID{OperationHandshake}, OptionalCapabilities: []protocolcommon.CapabilityID{},
	}
}

func testLimits(max string) runtimeprotocol.RuntimeLimits {
	items := runtimeprotocol.RuntimeItemLimitValue{HardMaximum: protocolcommon.CanonicalPositiveInt64(max), Unit: runtimeprotocol.RuntimeItemLimitValueUnitValue}
	bytes := runtimeprotocol.RuntimeByteLimitValue{HardMaximum: protocolcommon.CanonicalPositiveInt64(max), Unit: runtimeprotocol.RuntimeByteLimitValueUnitValue}
	return runtimeprotocol.RuntimeLimits{MaxBlobBytes: bytes, MaxBlobTotalBytes: bytes, MaxCommitOperations: items, MaxHistoryItems: items, MaxOutputBytes: bytes, MaxStateMutations: items}
}

func testBinding(expires time.Time) SessionBinding {
	expiresAt := protocolcommon.Rfc3339Time(expires.Format(time.RFC3339))
	scope := runtimeprotocol.RuntimeScope{AccessFingerprint: digest('1'), DocumentID: "doc_one", LocalScopeID: "local"}
	return SessionBinding{Session: runtimeprotocol.RuntimeSessionRef{RuntimeSessionID: "runtime_session_123456", SessionGeneration: "1", Scope: scope, ExpiresAt: &expiresAt}, CurrentRevision: testRevision()}
}

func testRevision() runtimeprotocol.CommittedRevisionRef {
	providerVersion := runtimeprotocol.ProviderVersionToken("provider-v1")
	return runtimeprotocol.CommittedRevisionRef{DocumentID: "doc_one", RevisionID: "rev_one", DefinitionHash: digest('2'), GraphHash: digest('7'), ProviderVersion: &providerVersion}
}
func digest(char byte) protocolcommon.Digest {
	return protocolcommon.Digest("sha256:" + strings.Repeat(string(char), 64))
}
func equalCapabilities(left, right []protocolcommon.CapabilityID) bool {
	if len(left) != len(right) {
		return false
	}
	for i := range left {
		if left[i] != right[i] {
			return false
		}
	}
	return true
}

func testGrant(now time.Time) accessprotocol.AuthoringGrantSnapshot {
	return accessprotocol.AuthoringGrantSnapshot{
		AccessFingerprint: digest('1'), ActorRef: accessprotocol.ActorRef{ActorID: "local-owner", Kind: "user"},
		GrantedCapabilities: []semantic.AuthoringCapability{semantic.AuthoringCapabilityAssetWrite, semantic.AuthoringCapabilityGraphWrite, semantic.AuthoringCapabilityPackageManage, semantic.AuthoringCapabilityProjectConfigure, semantic.AuthoringCapabilityQueryWrite, semantic.AuthoringCapabilityReferenceWrite, semantic.AuthoringCapabilitySchemaWrite, semantic.AuthoringCapabilitySourceMaintain, semantic.AuthoringCapabilityViewWrite},
		HostDocumentID:      "doc_one", IssuedAt: protocolcommon.Rfc3339Time(now.Format(time.RFC3339)), LocalScopeID: "local", MembershipVersion: "1", PolicyRefs: []accessprotocol.PolicyRef{},
	}
}

func authorizationFixture(now time.Time) (AuthorizationRequest, accessprotocol.AuthoringDecision) {
	grant := testGrant(now)
	hostImpact := accessprotocol.HostOperationImpact{
		Action: "stage", OperationKind: accessprotocol.HostOperationKindAssetStage,
		RequiredAuthoringCapabilities: []semantic.AuthoringCapability{semantic.AuthoringCapabilityAssetWrite}, ResourceRefs: []string{"asset"},
		ResourceScope: accessprotocol.HostResourceScope{DocumentID: grant.HostDocumentID, LocalScopeID: grant.LocalScopeID},
	}
	hostImpact = sealHostImpact(hostImpact)
	engineImpact := semantic.AuthoringImpact{
		BaseDefinitionHash: digest('7'), Entries: []semantic.AuthoringImpactEntry{}, ImpactDigest: digest('8'),
		RequiredCapabilities: []semantic.AuthoringCapability{}, ResultingDefinitionHash: digest('9'),
		SemanticDiffHash: digest('a'), SourceDiffHash: digest('b'),
	}
	decision := accessprotocol.AuthoringDecision{
		AccessFingerprint: grant.AccessFingerprint, ApprovalRuleRefs: []string{}, AuthoringImpactDigest: &engineImpact.ImpactDigest,
		ConstraintViolations: []accessprotocol.ConstraintViolation{}, DecisionDigest: digest('4'), Diagnostics: []protocolcommon.ProtocolDiagnostic{},
		EvaluationDigest: digest('5'), HostOperationImpactDigests: []protocolcommon.Digest{hostImpact.ImpactDigest}, MissingCapabilities: []semantic.AuthoringCapability{},
		Outcome: accessprotocol.AuthoringDecisionOutcomeAllow, RequiredCapabilities: []semantic.AuthoringCapability{semantic.AuthoringCapabilityAssetWrite},
	}
	proof := runtimeprotocol.AuthoringProof{
		AccessFingerprint: grant.AccessFingerprint, BaseRevision: testRevision(), DecisionDigest: decision.DecisionDigest,
		EvaluationDigest: decision.EvaluationDigest, MembershipVersion: grant.MembershipVersion, PolicyRefs: grant.PolicyRefs,
	}
	request := AuthorizationRequest{
		Scope:           runtimeprotocol.RuntimeScope{AccessFingerprint: grant.AccessFingerprint, DocumentID: "doc_one", LocalScopeID: "local"},
		CurrentRevision: testRevision(),
		Evaluation:      accessprotocol.EvaluateAuthoringInput{RequestIntent: "apply", AuthoringImpact: &engineImpact, HostOperationImpacts: []accessprotocol.HostOperationImpact{hostImpact}, GrantSnapshot: grant},
		Proof:           &proof,
	}
	return request, decision
}

func sealHostImpact(impact accessprotocol.HostOperationImpact) accessprotocol.HostOperationImpact {
	impact.ImpactDigest = ""
	impact.ImpactDigest = digestValue(impact)
	return impact
}

type fixedClock struct{ now time.Time }

func (c fixedClock) Now() time.Time { return c.now }

type fakeDecision struct {
	decision accessprotocol.AuthoringDecision
	calls    int
	err      error
	input    accessprotocol.EvaluateAuthoringInput
}

func (f *fakeDecision) Evaluate(_ context.Context, input accessprotocol.EvaluateAuthoringInput) (accessprotocol.AuthoringDecision, error) {
	f.calls++
	f.input = input
	return f.decision, f.err
}

type fakeRecovery struct{}

func (fakeRecovery) CreatePending(context.Context, port.CreatePendingRecordInput) (port.RecoveryRecord, error) {
	return port.RecoveryRecord{}, nil
}
func (fakeRecovery) AbandonPending(context.Context, port.AbandonPendingRecordInput) error { return nil }
func (fakeRecovery) Get(context.Context, port.GetRecoveryRecordInput) (port.RecoveryRecord, error) {
	return port.RecoveryRecord{}, nil
}
func (fakeRecovery) Advance(context.Context, port.AdvanceRecoveryRecordInput) (port.RecoveryRecord, error) {
	return port.RecoveryRecord{}, nil
}
func (fakeRecovery) Finalize(context.Context, port.FinalizeRecoveryRecordInput) (port.RecoveryRecord, error) {
	return port.RecoveryRecord{}, nil
}

type fakeDocumentStore struct{}

var (
	_ port.DocumentStore        = fakeDocumentStore{}
	_ port.StateBackend         = fakeStateBackend{}
	_ port.AssetStore           = fakeAssetStore{}
	_ port.HistoryStore         = fakeHistoryStore{}
	_ port.RecoveryJournal      = fakeRecovery{}
	_ port.IdentityGenerator    = fakeIdentityGenerator{}
	_ OpenDocumentOperation     = fakeRuntimeOperations{}
	_ CommitOperationsOperation = fakeRuntimeOperations{}
)

func (fakeDocumentStore) GetHead(context.Context, port.GetDocumentHeadInput) (port.DocumentHead, error) {
	return port.DocumentHead{}, nil
}
func (fakeDocumentStore) ReadRevision(context.Context, port.ReadRevisionInput) (port.RevisionSnapshot, error) {
	return port.RevisionSnapshot{}, nil
}
func (fakeDocumentStore) ReadSourceBlobs(context.Context, port.ReadSourceBlobsInput) (port.SourceBlobSet, error) {
	return port.SourceBlobSet{}, nil
}
func (fakeDocumentStore) StageRevision(context.Context, port.StageRevisionInput) (port.StagedRevision, error) {
	return port.StagedRevision{}, nil
}
func (fakeDocumentStore) PublishHead(context.Context, port.PublishDocumentHeadInput) (port.PublishHeadResult, error) {
	return port.PublishHeadResult{}, nil
}
func (fakeDocumentStore) AbortStagedRevision(context.Context, port.AbortStagedRevisionInput) error {
	return nil
}

type fakeStateBackend struct{}

func (fakeStateBackend) GetHead(context.Context, port.GetStateHeadInput) (port.StateHead, error) {
	return port.StateHead{}, nil
}
func (fakeStateBackend) ReadState(context.Context, port.ReadStateInput) (port.StateSnapshot, error) {
	return port.StateSnapshot{}, nil
}
func (fakeStateBackend) WriteState(context.Context, port.WriteStateInput) (port.StateWriteResult, error) {
	return port.StateWriteResult{}, nil
}
func (fakeStateBackend) AcquireLease(context.Context, port.AcquireLeaseInput) (port.StateLease, error) {
	return port.StateLease{}, nil
}
func (fakeStateBackend) RenewLease(context.Context, port.RenewLeaseInput) (port.StateLease, error) {
	return port.StateLease{}, nil
}
func (fakeStateBackend) ReleaseLease(context.Context, port.ReleaseLeaseInput) error { return nil }
func (fakeStateBackend) ValidateLease(context.Context, port.ValidateLeaseInput) (port.StateLease, error) {
	return port.StateLease{}, nil
}
func (fakeStateBackend) AppendAuditEvent(context.Context, port.AppendAuditEventInput) (port.AuditEventRef, error) {
	return port.AuditEventRef{}, nil
}
func (fakeStateBackend) ListAuditEvents(context.Context, port.ListAuditEventsInput) (port.AuditEventPage, error) {
	return port.AuditEventPage{}, nil
}
func (fakeStateBackend) ExportSnapshot(context.Context, port.ExportStateSnapshotInput) (port.StateSnapshot, error) {
	return port.StateSnapshot{}, nil
}

type fakeAssetStore struct{}

func (fakeAssetStore) Stat(context.Context, port.AssetRef) (port.AssetMetadata, error) {
	return port.AssetMetadata{}, nil
}
func (fakeAssetStore) Get(context.Context, port.AssetRef) (io.ReadCloser, error) { return nil, nil }
func (fakeAssetStore) PutIfAbsent(context.Context, port.PutAssetInput) (port.AssetMetadata, error) {
	return port.AssetMetadata{}, nil
}
func (fakeAssetStore) DeleteIfUnreferenced(context.Context, port.DeleteAssetInput) error { return nil }

type fakeHistoryStore struct{}

func (fakeHistoryStore) AppendRevision(context.Context, port.AppendRevisionInput) (runtimeprotocol.RevisionMetadata, error) {
	return runtimeprotocol.RevisionMetadata{}, nil
}
func (fakeHistoryStore) GetRevision(context.Context, port.GetRevisionMetadataInput) (runtimeprotocol.RevisionMetadata, error) {
	return runtimeprotocol.RevisionMetadata{}, nil
}
func (fakeHistoryStore) ListRevisions(context.Context, port.ListRevisionsInput) (runtimeprotocol.RevisionPage, error) {
	return runtimeprotocol.RevisionPage{}, nil
}
func (fakeHistoryStore) ResolveProviderVersion(context.Context, port.ResolveProviderVersionInput) (port.ProviderRevisionRef, error) {
	return port.ProviderRevisionRef{}, nil
}

type fakeIdentityGenerator struct{}

func (fakeIdentityGenerator) NewID(context.Context, port.IdentityKind) (string, error) {
	return "identity", nil
}

type fakeRuntimeOperations struct{}

func (fakeRuntimeOperations) OpenDocument(context.Context, runtimeprotocol.OpenRuntimeDocumentInput) (runtimeprotocol.OpenRuntimeDocumentResult, *ContractError) {
	return runtimeprotocol.OpenRuntimeDocumentResult{}, nil
}
func (fakeRuntimeOperations) CommitOperations(context.Context, runtimeprotocol.RuntimeCommitInput) (runtimeprotocol.RuntimeCommitResult, *ContractError) {
	return runtimeprotocol.RuntimeCommitResult{}, nil
}
func (fakeRuntimeOperations) CancelOperation(context.Context, runtimeprotocol.CancelOperationInput) (runtimeprotocol.CancelOperationResult, *ContractError) {
	return runtimeprotocol.CancelOperationResult{}, nil
}
func (fakeRuntimeOperations) GetOperationResult(context.Context, runtimeprotocol.GetOperationResultInput) (runtimeprotocol.RuntimeOperationStatus, *ContractError) {
	return runtimeprotocol.RuntimeOperationStatus{}, nil
}
func (fakeRuntimeOperations) ListRevisions(context.Context, runtimeprotocol.ListRevisionsInput) (runtimeprotocol.RevisionPage, *ContractError) {
	return runtimeprotocol.RevisionPage{}, nil
}
