// SPDX-License-Identifier: LicenseRef-LayerDraw-1.0

package runtime

import (
	"context"
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
