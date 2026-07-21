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

func initialRegistryCommitFixture(host *coordinatorHost) InitialRegistryCommitInput {
	baseline := host.base
	baseline.RevisionID = "registry-empty-baseline"
	return InitialRegistryCommitInput{
		DocumentID:                 host.base.DocumentID,
		OperationID:                "registry_initial_operation_1",
		IdempotencyKey:             "registry_initial_idempotency_1",
		BaselineRevision:           baseline,
		RegistryTransactionID:      "registry_initial_transaction_1",
		PlanDigest:                 digest('1'),
		MutationDigest:             digest('2'),
		ExpectedResolvedLockDigest: digest('3'),
		StagedObjects:              []port.RegistryStagedObjectRef{{ObjectID: "template-object", Digest: digest('4'), Size: protocolcommon.CanonicalUint64("1"), MediaType: "application/vnd.layerdraw.template"}},
		AuthoringImpact:            host.impact,
		HostOperationImpacts:       []accessprotocol.HostOperationImpact{},
		ExpectedDecision:           host.decision,
		ProjectMutation:            port.RegistryProjectMutation{SnapshotHandle: "engine-template-baseline", SourceClosureDigest: digest('5'), Artifacts: []port.RegistryProjectArtifactRef{{Object: port.RegistryStagedObjectRef{ObjectID: "template-object", Digest: digest('4'), Size: "1", MediaType: "application/vnd.layerdraw.template"}, RegistrySource: "official"}}, RemoveCanonicalIDs: []string{}},
	}
}

func TestCoordinatorCommitInitialRegistryTemplateOwnsFirstHead(t *testing.T) {
	host, rt := newCoordinatorFixture(t)
	host.mu.Lock()
	host.head = port.DocumentHead{}
	host.mu.Unlock()
	input := initialRegistryCommitFixture(host)

	result, rejection := rt.CommitInitialRegistryTemplate(context.Background(), input)
	if rejection != nil {
		t.Fatal(rejection)
	}
	if result.OperationResult.Status != runtimeprotocol.OperationResultStatusCommitted || result.OperationResult.CommittedRevision == nil || host.initialPublishCalls != 1 {
		t.Fatalf("result=%+v publishes=%d", result, host.initialPublishCalls)
	}
	if len(host.history) != 1 || host.history[0].ParentRevisionID != nil || host.history[0].Trigger != runtimeprotocol.CommitTriggerRegistryInstall {
		t.Fatalf("history=%+v", host.history)
	}
	retry, retryRejection := rt.CommitInitialRegistryTemplate(context.Background(), input)
	if retryRejection != nil || retry.OperationResult.ResultDigest != result.OperationResult.ResultDigest || host.initialPublishCalls != 1 {
		t.Fatalf("retry=%+v rejection=%v publishes=%d", retry, retryRejection, host.initialPublishCalls)
	}
}

func TestCoordinatorCommitInitialRegistryTemplateRejectsExistingHead(t *testing.T) {
	host, rt := newCoordinatorFixture(t)
	result, rejection := rt.CommitInitialRegistryTemplate(context.Background(), initialRegistryCommitFixture(host))
	if rejection != nil || result.OperationResult.Status != runtimeprotocol.OperationResultStatusRejected || result.OperationResult.ConflictEvidence == nil || host.initialPublishCalls != 0 {
		t.Fatalf("result=%+v rejection=%v publishes=%d", result, rejection, host.initialPublishCalls)
	}
}

func TestInitialRegistryCommitFaultMatrixIsClosedAndNeverDoublePublishes(t *testing.T) {
	tests := []struct {
		name      string
		configure func(*coordinatorHost, *InitialRegistryCommitInput)
	}{
		{name: "malformed baseline", configure: func(_ *coordinatorHost, in *InitialRegistryCommitInput) { in.BaselineRevision.DocumentID = "other" }},
		{name: "scope unavailable", configure: func(h *coordinatorHost, _ *InitialRegistryCommitInput) { h.scopeErr = errors.New("scope unavailable") }},
		{name: "operation journal read", configure: func(h *coordinatorHost, _ *InitialRegistryCommitInput) { h.recoveryGetErrOnCall = 1 }},
		{name: "idempotency journal read", configure: func(h *coordinatorHost, _ *InitialRegistryCommitInput) { h.recoveryGetErrOnCall = 2 }},
		{name: "reservation transport", configure: func(h *coordinatorHost, _ *InitialRegistryCommitInput) {
			h.createPendingErr = errors.New("journal unavailable")
		}},
		{name: "conflicting reservation", configure: func(h *coordinatorHost, _ *InitialRegistryCommitInput) { h.createPendingConflictRecord = true }},
		{name: "invalid reservation", configure: func(h *coordinatorHost, _ *InitialRegistryCommitInput) {
			h.mutateCreateRecord = func(record *port.RecoveryRecord) { record.Status.Phase = runtimeprotocol.RecoveryPhaseStaged }
		}},
		{name: "prepare transport", configure: func(h *coordinatorHost, _ *InitialRegistryCommitInput) {
			h.registryPrepareErr = errors.New("Engine unavailable")
		}},
		{name: "invalid prepared closure", configure: func(h *coordinatorHost, _ *InitialRegistryCommitInput) { h.invalidRegistryPrepared = true }},
		{name: "head transport", configure: func(h *coordinatorHost, _ *InitialRegistryCommitInput) { h.failHeadOnCall = 1 }},
		{name: "abandon transport", configure: func(h *coordinatorHost, _ *InitialRegistryCommitInput) {
			h.registryPrepareErr, h.abandonErr = errors.New("Engine unavailable"), errors.New("journal abandon unavailable")
		}},
		{name: "grant unavailable", configure: func(h *coordinatorHost, _ *InitialRegistryCommitInput) { h.failGrantOnCall = 1 }},
		{name: "apply denied", configure: func(h *coordinatorHost, _ *InitialRegistryCommitInput) { h.denyApply = true }},
		{name: "pending transition", configure: func(h *coordinatorHost, _ *InitialRegistryCommitInput) {
			h.advanceErrFrom = runtimeprotocol.RecoveryPhasePending
		}},
		{name: "publication fence", configure: func(h *coordinatorHost, _ *InitialRegistryCommitInput) {
			h.publicationFenceErr = errors.New("fence unavailable")
		}},
		{name: "publication grant unavailable", configure: func(h *coordinatorHost, _ *InitialRegistryCommitInput) { h.failGrantOnCall = 2 }},
		{name: "publication denied", configure: func(h *coordinatorHost, _ *InitialRegistryCommitInput) { h.denyPublish = true }},
		{name: "publication decision changed", configure: func(h *coordinatorHost, _ *InitialRegistryCommitInput) { h.mismatchPublish = true }},
		{name: "staged transition", configure: func(h *coordinatorHost, _ *InitialRegistryCommitInput) {
			h.advanceErrFrom = runtimeprotocol.RecoveryPhaseStaged
		}},
		{name: "publication unavailable", configure: func(h *coordinatorHost, _ *InitialRegistryCommitInput) {
			h.initialPublishErr = errors.New("publication unavailable")
		}},
		{name: "invalid publication", configure: func(h *coordinatorHost, _ *InitialRegistryCommitInput) { h.invalidInitialPublish = true }},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			host, rt := newCoordinatorFixture(t)
			host.mu.Lock()
			host.head = port.DocumentHead{}
			host.mu.Unlock()
			input := initialRegistryCommitFixture(host)
			test.configure(host, &input)
			result, rejection := rt.CommitInitialRegistryTemplate(context.Background(), input)
			if rejection == nil && result.OperationResult.Status == "" {
				t.Fatalf("fault lost terminal meaning: result=%+v rejection=%v", result, rejection)
			}
			if host.initialPublishCalls > 1 {
				t.Fatalf("initial publication repeated %d times", host.initialPublishCalls)
			}
		})
	}
}

func TestInitialRegistryPostPublicationFaultsRemainCommittedStateStale(t *testing.T) {
	for _, test := range []struct {
		name         string
		configure    func(*coordinatorHost)
		wantRejected bool
	}{
		{name: "history", configure: func(h *coordinatorHost) { h.historyErr = errors.New("history unavailable") }},
		{name: "state", configure: func(h *coordinatorHost) { h.stateHeadErr = errors.New("state unavailable") }, wantRejected: true},
		{name: "published transition", configure: func(h *coordinatorHost) { h.advanceErrFrom = runtimeprotocol.RecoveryPhasePublished }, wantRejected: true},
		{name: "state transition", configure: func(h *coordinatorHost) { h.advanceErrFrom = runtimeprotocol.RecoveryPhaseStatePending }, wantRejected: true},
		{name: "audit transition", configure: func(h *coordinatorHost) { h.advanceErrFrom = runtimeprotocol.RecoveryPhaseAuditPending }},
	} {
		t.Run(test.name, func(t *testing.T) {
			host, rt := newCoordinatorFixture(t)
			host.mu.Lock()
			host.head = port.DocumentHead{}
			host.mu.Unlock()
			test.configure(host)
			result, rejection := rt.CommitInitialRegistryTemplate(context.Background(), initialRegistryCommitFixture(host))
			if test.wantRejected {
				if rejection == nil || host.initialPublishCalls != 1 {
					t.Fatalf("result=%+v rejection=%v publishes=%d", result, rejection, host.initialPublishCalls)
				}
				return
			}
			if rejection != nil {
				t.Fatal(rejection)
			}
			if result.OperationResult.CommittedRevision == nil || result.OperationResult.Status != runtimeprotocol.OperationResultStatusCommittedStateStale {
				t.Fatalf("result=%+v", result)
			}
		})
	}
}
