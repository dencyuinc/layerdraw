// SPDX-License-Identifier: LicenseRef-LayerDraw-1.0

package runtime

import (
	"context"
	"errors"
	"testing"

	"github.com/dencyuinc/layerdraw/gen/go/accessprotocol"
	"github.com/dencyuinc/layerdraw/gen/go/protocolcommon"
	"github.com/dencyuinc/layerdraw/gen/go/runtimeprotocol"
	"github.com/dencyuinc/layerdraw/internal/runtime/port"
)

func registryCommitFixture(opened runtimeprotocol.OpenRuntimeDocumentResult, host *coordinatorHost) RegistryCommitInput {
	return RegistryCommitInput{
		Session:                    opened.Session,
		OperationID:                "registry_operation_1",
		IdempotencyKey:             "registry_idempotency_000001",
		BaseRevision:               host.base,
		RegistryTransactionID:      "registry_transaction_000001",
		PlanDigest:                 digest('1'),
		MutationDigest:             digest('2'),
		ExpectedResolvedLockDigest: digest('3'),
		StagedObjects:              []port.RegistryStagedObjectRef{{ObjectID: "registry-object-1", Digest: digest('4'), Size: protocolcommon.CanonicalUint64("1"), MediaType: "application/vnd.layerdraw.pack"}},
		AuthoringImpact:            host.impact,
		HostOperationImpacts:       []accessprotocol.HostOperationImpact{},
		ExpectedDecision:           host.decision,
		ProjectMutation:            port.RegistryProjectMutation{SnapshotHandle: host.working.Handle, SourceClosureDigest: digest('5'), Artifacts: []port.RegistryProjectArtifactRef{{Object: port.RegistryStagedObjectRef{ObjectID: "registry-object-1", Digest: digest('4'), Size: "1", MediaType: "application/vnd.layerdraw.pack"}, RegistrySource: "official"}}, RemoveCanonicalIDs: []string{}},
	}
}

func TestCoordinatorCommitRegistryPlanPublishesWithoutOperationBatch(t *testing.T) {
	host, rt := newCoordinatorFixture(t)
	opened := openCoordinatorFixture(t, rt)
	input := registryCommitFixture(opened, host)

	result, rejection := rt.CommitRegistryPlan(context.Background(), input)
	if rejection != nil {
		t.Fatal(rejection)
	}
	if result.OperationResult.Status != runtimeprotocol.OperationResultStatusCommitted || result.OperationResult.CommittedRevision == nil {
		t.Fatalf("result=%+v", result)
	}
	if host.stageCalls != 1 || host.publishCalls != 1 || host.stagedInput.Trigger != runtimeprotocol.CommitTriggerRegistryInstall {
		t.Fatalf("stage=%d publish=%d trigger=%s", host.stageCalls, host.publishCalls, host.stagedInput.Trigger)
	}
	if got := host.records[input.OperationID].Status.Phase; got != runtimeprotocol.RecoveryPhaseFinal {
		t.Fatalf("journal phase=%s", got)
	}
	if len(host.history) != 1 || host.history[0].Trigger != runtimeprotocol.CommitTriggerRegistryInstall {
		t.Fatalf("history=%+v", host.history)
	}

	retry, retryRejection := rt.CommitRegistryPlan(context.Background(), input)
	if retryRejection != nil || retry.OperationResult.ResultDigest != result.OperationResult.ResultDigest || host.publishCalls != 1 {
		t.Fatalf("retry=%+v rejection=%v publishes=%d", retry, retryRejection, host.publishCalls)
	}
}

func TestCoordinatorCommitRegistryPlanRejectsChangedStagedIdentity(t *testing.T) {
	host, rt := newCoordinatorFixture(t)
	opened := openCoordinatorFixture(t, rt)
	input := registryCommitFixture(opened, host)
	if _, rejection := rt.CommitRegistryPlan(context.Background(), input); rejection != nil {
		t.Fatal(rejection)
	}
	input.StagedObjects[0].Digest = digest('9')
	if _, rejection := rt.CommitRegistryPlan(context.Background(), input); rejection == nil || rejection.Code != runtimeprotocol.RuntimeFailureCodeRuntimeIdempotencyMismatch {
		t.Fatalf("rejection=%v", rejection)
	}
}

func TestCoordinatorCommitRegistryPlanRechecksPublicationAccess(t *testing.T) {
	host, rt := newCoordinatorFixture(t)
	opened := openCoordinatorFixture(t, rt)
	input := registryCommitFixture(opened, host)
	host.failGrantOnCall = 3 // open, apply, then publication re-check

	result, rejection := rt.CommitRegistryPlan(context.Background(), input)
	if rejection != nil || result.OperationResult.Status != runtimeprotocol.OperationResultStatusRejected || host.successfulPublishes != 0 {
		t.Fatalf("result=%+v rejection=%v publishes=%d", result, rejection, host.successfulPublishes)
	}
}

func TestRegistryCommitFailureMatrixPreservesDurableTerminalMeaning(t *testing.T) {
	tests := []struct {
		name      string
		configure func(*coordinatorHost)
		status    runtimeprotocol.OperationResultStatus
		rejected  bool
	}{
		{name: "prepare transport", configure: func(h *coordinatorHost) { h.registryPrepareErr = errors.New("Engine unavailable") }, rejected: true},
		{name: "invalid prepared closure", configure: func(h *coordinatorHost) { h.invalidRegistryPrepared = true }, rejected: true},
		{name: "prepare grant unavailable", configure: func(h *coordinatorHost) { h.failGrantOnCall = 2 }, rejected: true},
		{name: "document stage failure", configure: func(h *coordinatorHost) { h.stageErr = errors.New("stage unavailable") }, rejected: true},
		{name: "invalid document stage", configure: func(h *coordinatorHost) { h.invalidStage = true }, rejected: true},
		{name: "rejected final journal", configure: func(h *coordinatorHost) {
			h.publicationFenceErr, h.finalizeErr = errors.New("fence unavailable"), errors.New("finalize unavailable")
		}, rejected: true},
		{name: "stale final journal", configure: func(h *coordinatorHost) {
			h.head.Revision.RevisionID, h.finalizeErr = "other_revision", errors.New("finalize unavailable")
		}, rejected: true},
		{name: "publication conflict", configure: func(h *coordinatorHost) { h.publishErr = port.ErrConflict }, status: runtimeprotocol.OperationResultStatusRejected},
		{name: "conflict final journal", configure: func(h *coordinatorHost) {
			h.publishErr, h.finalizeErr = port.ErrConflict, errors.New("finalize unavailable")
		}, rejected: true},
		{name: "indeterminate publication", configure: func(h *coordinatorHost) { h.publishErr, h.failHeadOnCall = port.ErrIndeterminate, 3 }, status: runtimeprotocol.OperationResultStatusNeedsReview},
		{name: "review final journal", configure: func(h *coordinatorHost) {
			h.publishErr, h.failHeadOnCall, h.finalizeErr = port.ErrIndeterminate, 3, errors.New("finalize unavailable")
		}, rejected: true},
		{name: "history stale", configure: func(h *coordinatorHost) { h.historyErr = errors.New("history unavailable") }, status: runtimeprotocol.OperationResultStatusCommittedStateStale},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			host, rt := newCoordinatorFixture(t)
			opened := openCoordinatorFixture(t, rt)
			test.configure(host)
			result, rejection := rt.CommitRegistryPlan(context.Background(), registryCommitFixture(opened, host))
			if test.rejected {
				if rejection == nil || result.OperationResult.Status != "" || host.successfulPublishes != 0 {
					t.Fatalf("result=%+v rejection=%v publishes=%d", result, rejection, host.successfulPublishes)
				}
				return
			}
			if rejection != nil || result.OperationResult.Status != test.status {
				t.Fatalf("result=%+v rejection=%v", result, rejection)
			}
		})
	}
}

func TestRegistryCommitInputValidationRejectsEveryUnboundAuthority(t *testing.T) {
	fixture := func(t *testing.T) RegistryCommitInput {
		t.Helper()
		host, rt := newCoordinatorFixture(t)
		opened := openCoordinatorFixture(t, rt)
		return registryCommitFixture(opened, host)
	}
	tests := []struct {
		name   string
		mutate func(*RegistryCommitInput)
	}{
		{name: "session", mutate: func(in *RegistryCommitInput) { in.Session.RuntimeSessionID = "" }},
		{name: "operation", mutate: func(in *RegistryCommitInput) { in.OperationID = "" }},
		{name: "idempotency", mutate: func(in *RegistryCommitInput) { in.IdempotencyKey = "" }},
		{name: "base document", mutate: func(in *RegistryCommitInput) { in.BaseRevision.DocumentID = "other" }},
		{name: "transaction", mutate: func(in *RegistryCommitInput) { in.RegistryTransactionID = "short" }},
		{name: "staged closure", mutate: func(in *RegistryCommitInput) { in.StagedObjects = nil }},
		{name: "plan digest", mutate: func(in *RegistryCommitInput) { in.PlanDigest = "invalid" }},
		{name: "impact", mutate: func(in *RegistryCommitInput) { in.AuthoringImpact.BaseDefinitionHash = digest('9') }},
		{name: "decision", mutate: func(in *RegistryCommitInput) {
			in.ExpectedDecision.Outcome = accessprotocol.AuthoringDecisionOutcomeDeny
		}},
		{name: "object id", mutate: func(in *RegistryCommitInput) { in.StagedObjects[0].ObjectID = "" }},
		{name: "duplicate object", mutate: func(in *RegistryCommitInput) { in.StagedObjects = append(in.StagedObjects, in.StagedObjects[0]) }},
		{name: "object digest", mutate: func(in *RegistryCommitInput) { in.StagedObjects[0].Digest = "invalid" }},
		{name: "object size", mutate: func(in *RegistryCommitInput) { in.StagedObjects[0].Size = "-1" }},
		{name: "snapshot", mutate: func(in *RegistryCommitInput) { in.ProjectMutation.SnapshotHandle = "" }},
		{name: "source closure", mutate: func(in *RegistryCommitInput) { in.ProjectMutation.SourceClosureDigest = "invalid" }},
		{name: "artifact object", mutate: func(in *RegistryCommitInput) { in.ProjectMutation.Artifacts[0].Object.ObjectID = "other" }},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			input := fixture(t)
			test.mutate(&input)
			if rejection := validateRegistryCommitInput(input); rejection == nil {
				t.Fatal("invalid Registry authority was accepted")
			}
		})
	}
}

func TestRegistryJournalAndAuthorizationFaultsNeverReachPublication(t *testing.T) {
	tests := []struct {
		name      string
		configure func(*coordinatorHost)
	}{
		{name: "conflicting pending identity", configure: func(h *coordinatorHost) { h.createPendingConflictRecord = true }},
		{name: "divergent uniqueness indexes", configure: func(h *coordinatorHost) { h.createPendingDivergentRecords = true }},
		{name: "conflict without authoritative record", configure: func(h *coordinatorHost) { h.createPendingErr = port.ErrConflict }},
		{name: "conflict lookup transport", configure: func(h *coordinatorHost) { h.createPendingErr, h.recoveryGetErrOnCall = port.ErrConflict, 3 }},
		{name: "reservation transport", configure: func(h *coordinatorHost) { h.createPendingErr = errors.New("journal unavailable") }},
		{name: "operation journal read", configure: func(h *coordinatorHost) { h.recoveryGetErrOnCall = 1 }},
		{name: "idempotency journal read", configure: func(h *coordinatorHost) { h.recoveryGetErrOnCall = 2 }},
		{name: "invalid reservation", configure: func(h *coordinatorHost) {
			h.mutateCreateRecord = func(record *port.RecoveryRecord) { record.Status.Phase = runtimeprotocol.RecoveryPhaseStaged }
		}},
		{name: "apply denied", configure: func(h *coordinatorHost) { h.denyApply = true }},
		{name: "apply decision changed", configure: func(h *coordinatorHost) { h.mismatchApply = true }},
		{name: "publication denied", configure: func(h *coordinatorHost) { h.denyPublish = true }},
		{name: "publication decision changed", configure: func(h *coordinatorHost) { h.mismatchPublish = true }},
		{name: "publication fence", configure: func(h *coordinatorHost) { h.publicationFenceErr = errors.New("fence unavailable") }},
		{name: "pending transition", configure: func(h *coordinatorHost) { h.advanceErrFrom = runtimeprotocol.RecoveryPhasePending }},
		{name: "staged transition", configure: func(h *coordinatorHost) { h.advanceErrFrom = runtimeprotocol.RecoveryPhaseStaged }},
		{name: "abandon transport", configure: func(h *coordinatorHost) {
			h.registryPrepareErr, h.abandonErr = errors.New("Engine unavailable"), errors.New("journal abandon unavailable")
		}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			host, rt := newCoordinatorFixture(t)
			opened := openCoordinatorFixture(t, rt)
			test.configure(host)
			result, rejection := rt.CommitRegistryPlan(context.Background(), registryCommitFixture(opened, host))
			if rejection == nil && result.OperationResult.Status == "" {
				t.Fatalf("fault lost terminal meaning: result=%+v rejection=%v", result, rejection)
			}
			if host.successfulPublishes != 0 {
				t.Fatalf("fault published %d revisions", host.successfulPublishes)
			}
		})
	}
}

func TestRegistryPostPublicationFaultsPreservePublishedRevision(t *testing.T) {
	for _, phase := range []runtimeprotocol.RecoveryPhase{
		runtimeprotocol.RecoveryPhasePublicationPending,
		runtimeprotocol.RecoveryPhasePublished,
		runtimeprotocol.RecoveryPhaseStatePending,
		runtimeprotocol.RecoveryPhaseAuditPending,
	} {
		t.Run(string(phase), func(t *testing.T) {
			host, rt := newCoordinatorFixture(t)
			opened := openCoordinatorFixture(t, rt)
			host.advanceErrFrom = phase
			result, rejection := rt.CommitRegistryPlan(context.Background(), registryCommitFixture(opened, host))
			if rejection != nil || result.OperationResult.Status != runtimeprotocol.OperationResultStatusCommittedStateStale || result.OperationResult.CommittedRevision == nil || host.successfulPublishes != 1 {
				t.Fatalf("result=%+v rejection=%v publishes=%d", result, rejection, host.successfulPublishes)
			}
		})
	}
	t.Run("terminal journal", func(t *testing.T) {
		host, rt := newCoordinatorFixture(t)
		opened := openCoordinatorFixture(t, rt)
		host.finalizeErr = errors.New("final journal unavailable")
		result, rejection := rt.CommitRegistryPlan(context.Background(), registryCommitFixture(opened, host))
		if rejection == nil || result.OperationResult.Status != "" || host.successfulPublishes != 1 {
			t.Fatalf("result=%+v rejection=%v publishes=%d", result, rejection, host.successfulPublishes)
		}
	})
}

func TestRegistryLeaseAndCancellationAreRecheckedAtPublicationBoundary(t *testing.T) {
	for _, test := range []struct {
		name       string
		leaseError error
		failCall   int
	}{
		{name: "lease transport", leaseError: errors.New("lease unavailable")},
		{name: "lease rejected before prepare", failCall: 1},
		{name: "lease fenced before publication", failCall: 2},
	} {
		t.Run(test.name, func(t *testing.T) {
			host, rt := newCoordinatorFixture(t)
			opened := openCoordinatorFixture(t, rt)
			host.includeLease, host.leaseErr, host.failLeaseOnCall = true, test.leaseError, test.failCall
			input := registryCommitFixture(opened, host)
			input.LeaseToken = ptr(runtimeprotocol.LeaseToken("lease_token_0001"))
			result, rejection := rt.CommitRegistryPlan(context.Background(), input)
			if rejection == nil && result.OperationResult.Status == "" {
				t.Fatalf("result=%+v rejection=%v", result, rejection)
			}
			if host.successfulPublishes != 0 {
				t.Fatalf("lease fault published %d revisions", host.successfulPublishes)
			}
		})
	}
	t.Run("cancelled context", func(t *testing.T) {
		host, rt := newCoordinatorFixture(t)
		opened := openCoordinatorFixture(t, rt)
		input := registryCommitFixture(opened, host)
		input.CancellationToken = ptr(runtimeprotocol.CancellationToken("cancel_registry_0001"))
		ctx, cancel := context.WithCancel(context.Background())
		cancel()
		result, rejection := rt.CommitRegistryPlan(ctx, input)
		if rejection != nil || result.OperationResult.Status != runtimeprotocol.OperationResultStatusRejected || host.successfulPublishes != 0 {
			t.Fatalf("result=%+v rejection=%v publishes=%d", result, rejection, host.successfulPublishes)
		}
	})
}

func TestRegistryJournalRejectsImpossiblePhaseTransition(t *testing.T) {
	host, runtime := newCoordinatorFixture(t)
	opened := openCoordinatorFixture(t, runtime)
	input := registryCommitFixture(opened, host)
	coordinator := runtime.config.Operations.RegistryCommit.(*Coordinator)
	if _, rejection := coordinator.advanceRegistry(context.Background(), input, runtimeprotocol.RecoveryPhaseFinal, runtimeprotocol.RecoveryPhasePending, nil, nil, nil); rejection == nil {
		t.Fatal("impossible Registry recovery transition was accepted")
	}
}
