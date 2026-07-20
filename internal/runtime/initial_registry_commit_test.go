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
