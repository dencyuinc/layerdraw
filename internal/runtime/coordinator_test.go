// SPDX-License-Identifier: LicenseRef-LayerDraw-1.0

package runtime

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"strconv"
	"sync"
	"testing"
	"time"

	"github.com/dencyuinc/layerdraw/gen/go/accessprotocol"
	"github.com/dencyuinc/layerdraw/gen/go/engineprotocol"
	"github.com/dencyuinc/layerdraw/gen/go/protocolcommon"
	"github.com/dencyuinc/layerdraw/gen/go/runtimeprotocol"
	"github.com/dencyuinc/layerdraw/gen/go/semantic"
	"github.com/dencyuinc/layerdraw/internal/runtime/port"
)

func TestCoordinatorOpenCommitRetryAndHistoryLinkage(t *testing.T) {
	host, rt := newCoordinatorFixture(t)
	opened := openCoordinatorFixture(t, rt)
	if opened.WorkingDocument.BaseRevision != host.base || opened.WorkingDocument.WorkingGeneration != "0" || opened.StateInput.Kind != "none" || opened.StateInput.ExpectedStateVersion != nil {
		t.Fatalf("open did not bind the complete working base: %+v", opened)
	}
	if _, err := runtimeprotocol.EncodeOpenRuntimeDocumentResult(opened); err != nil {
		t.Fatalf("open result is wire-invalid: %v", err)
	}
	input := commitFixture(opened.Session, host)
	result, rejection := rt.CommitOperations(context.Background(), input)
	if rejection != nil || result.OperationResult.Status != runtimeprotocol.OperationResultStatusCommitted {
		t.Fatalf("commit result=%+v rejection=%v", result, rejection)
	}
	assertCommitResultEncodes(t, result)
	retry, rejection := rt.CommitOperations(context.Background(), input)
	if rejection != nil || !reflect.DeepEqual(retry.OperationResult, result.OperationResult) {
		t.Fatalf("retry did not return the original typed result: %+v / %v", retry, rejection)
	}
	assertCommitResultEncodes(t, retry)
	host.mu.Lock()
	defer host.mu.Unlock()
	if host.publishCalls != 1 || host.stageCalls != 1 || len(host.history) != 1 {
		t.Fatalf("stage=%d publish=%d history=%d", host.stageCalls, host.publishCalls, len(host.history))
	}
	if host.stagedInput.Actor.ActorID != "local-owner" || host.stagedInput.Trigger != input.Trigger || host.history[0].ParentRevisionID == nil || *host.history[0].ParentRevisionID != host.base.RevisionID || host.history[0].OperationID != input.OperationID {
		t.Fatalf("revision provenance/linkage was lost: stage=%+v history=%+v", host.stagedInput, host.history[0])
	}
	if host.stagedInput.GraphHash != digest('c') {
		t.Fatalf("graph hash was not staged: %s", host.stagedInput.GraphHash)
	}
	record := host.records[input.OperationID]
	if record.EvaluationDigest == nil || record.DecisionDigest == nil || *record.EvaluationDigest != result.PreviewEvaluation.AuthoringDecision.EvaluationDigest || *record.DecisionDigest != result.PreviewEvaluation.AuthoringDecision.DecisionDigest {
		t.Fatalf("durable evaluation binding was lost: %+v", record)
	}
	if !reflect.DeepEqual(host.previewInput.Preconditions, input.OperationBatch.Preconditions) {
		t.Fatal("Engine preconditions were not forwarded unchanged")
	}
}

func TestCoordinatorRaceHasExactlyOnePublicationPoint(t *testing.T) {
	host, rt := newCoordinatorFixture(t)
	opened := openCoordinatorFixture(t, rt)
	const contenders = 12
	var wg sync.WaitGroup
	results := make(chan runtimeprotocol.OperationResultStatus, contenders)
	for index := 0; index < contenders; index++ {
		wg.Add(1)
		go func(index int) {
			defer wg.Done()
			input := commitFixture(opened.Session, host)
			input.OperationID = runtimeprotocol.OperationID(fmt.Sprintf("operation_%d", index+10))
			input.IdempotencyKey = runtimeprotocol.IdempotencyKey(fmt.Sprintf("idem_commit_%06d", index+10))
			result, rejection := rt.CommitOperations(context.Background(), input)
			if rejection == nil {
				results <- result.OperationResult.Status
			}
		}(index)
	}
	wg.Wait()
	close(results)
	committed := 0
	for status := range results {
		if status == runtimeprotocol.OperationResultStatusCommitted {
			committed++
		}
	}
	host.mu.Lock()
	publishes := host.successfulPublishes
	calls := host.publishCalls
	host.mu.Unlock()
	if committed != 1 || publishes != 1 || calls < 1 {
		t.Fatalf("committed=%d successful publications=%d calls=%d", committed, publishes, calls)
	}
}

func TestCoordinatorCancellationConflictAndPortFailuresNeverPublishPartially(t *testing.T) {
	tests := runtimePersistenceFaultCases(t, "in_memory")
	for _, tc := range tests {
		t.Run(tc.ID, func(t *testing.T) {
			host, rt := newCoordinatorFixture(t)
			cancel := configurePersistenceFault(t, host, tc.Injection)
			opened := openCoordinatorFixture(t, rt)
			input := commitFixture(opened.Session, host)
			ctx := context.Background()
			if cancel {
				cancelled, cancel := context.WithCancel(ctx)
				cancel()
				ctx = cancelled
			}
			result, rejection := rt.CommitOperations(ctx, input)
			if rejection != nil || result.OperationResult.Status != runtimeprotocol.OperationResultStatus(tc.ExpectedStatus) {
				t.Fatalf("result=%+v rejection=%v", result, rejection)
			}
			assertCommitResultEncodes(t, result)
			if (result.PreviewEvaluation != nil) != tc.ExpectedPreview {
				t.Fatalf("preview evaluation presence=%t want=%t", result.PreviewEvaluation != nil, tc.ExpectedPreview)
			}
			host.mu.Lock()
			publications := host.successfulPublishes
			previews := host.previewCalls
			host.mu.Unlock()
			if publications != tc.ExpectedPublications {
				t.Fatalf("publications=%d want=%d", publications, tc.ExpectedPublications)
			}
			if (tc.Injection == "cancel_before_publication" || tc.Injection == "invalid_lease") && previews != 0 {
				t.Fatalf("pre-preview rejection reached Engine preview: %d", previews)
			}
			if tc.RetryStable {
				retry, retryRejection := rt.CommitOperations(context.Background(), input)
				if retryRejection != nil || !reflect.DeepEqual(retry, result) {
					t.Fatalf("post-pending retry changed result: retry=%+v rejection=%v", retry, retryRejection)
				}
				assertCommitResultEncodes(t, retry)
			}
			if tc.ExpectedRecoveryPhase != "" {
				status, statusRejection := rt.GetOperationResult(context.Background(), runtimeprotocol.GetOperationResultInput{Session: opened.Session, LookupBy: "operation_id", OperationID: &input.OperationID})
				if statusRejection != nil || status.Phase != runtimeprotocol.RecoveryPhase(tc.ExpectedRecoveryPhase) {
					t.Fatalf("needs_review journal phase=%s rejection=%v", status.Phase, statusRejection)
				}
			}
		})
	}
}

type runtimePersistenceFaultCase struct {
	ID                    string `json:"id"`
	Surface               string `json:"surface"`
	Injection             string `json:"injection"`
	ExpectedStatus        string `json:"expected_status"`
	ExpectedPublications  int    `json:"expected_publications"`
	ExpectedPreview       bool   `json:"expected_preview"`
	ExpectedRecoveryPhase string `json:"expected_recovery_phase"`
	RetryStable           bool   `json:"retry_stable"`
}

func runtimePersistenceFaultCases(t *testing.T, surface string) []runtimePersistenceFaultCase {
	t.Helper()
	data, err := os.ReadFile(filepath.Join("..", "..", "tests", "conformance", "testdata", "local_runtime_persistence_v1.json"))
	if err != nil {
		t.Fatal(err)
	}
	var corpus struct {
		SchemaVersion int                           `json:"schema_version"`
		FaultMatrix   []runtimePersistenceFaultCase `json:"fault_matrix"`
	}
	if err := json.Unmarshal(data, &corpus); err != nil || corpus.SchemaVersion != 1 {
		t.Fatalf("invalid persistence fault corpus: version=%d err=%v", corpus.SchemaVersion, err)
	}
	result := make([]runtimePersistenceFaultCase, 0, len(corpus.FaultMatrix))
	for _, fault := range corpus.FaultMatrix {
		if fault.Surface == surface {
			result = append(result, fault)
		}
	}
	if len(result) == 0 {
		t.Fatalf("persistence fault corpus has no %s cases", surface)
	}
	return result
}

func configurePersistenceFault(t *testing.T, host *coordinatorHost, injection string) bool {
	t.Helper()
	switch injection {
	case "cancel_before_publication":
		return true
	case "preview_failure":
		host.previewErr = errors.New("injected")
	case "conditional_conflict":
		host.publishErr = port.ErrConflict
	case "conflict_without_trusted_head":
		host.publishErr, host.failHeadOnCall = port.ErrConflict, 3
	case "invalid_lease":
		host.leaseErr, host.includeLease = port.ErrConflict, true
	case "lease_fenced_before_publication":
		host.failLeaseOnCall, host.includeLease = 2, true
	case "indeterminate_publication":
		host.publishErr, host.publishDespiteError = port.ErrIndeterminate, true
	case "state_failure_after_publication":
		host.stateWriteErr, host.includeStateMutation = errors.New("injected"), true
	case "history_failure_after_publication":
		host.historyErr = errors.New("injected")
	default:
		t.Fatalf("unknown in-memory persistence fault %q", injection)
	}
	return false
}

func TestCoordinatorRecoveryAdvanceFailuresRemainRecoverableTransportFailures(t *testing.T) {
	for _, phase := range []runtimeprotocol.RecoveryPhase{runtimeprotocol.RecoveryPhasePending, runtimeprotocol.RecoveryPhaseStaged} {
		t.Run(string(phase), func(t *testing.T) {
			host, rt := newCoordinatorFixture(t)
			host.advanceErrFrom = phase
			opened := openCoordinatorFixture(t, rt)
			input := commitFixture(opened.Session, host)
			result, rejection := rt.CommitOperations(context.Background(), input)
			if rejection == nil || !reflect.DeepEqual(result, runtimeprotocol.RuntimeCommitResult{}) {
				t.Fatalf("result=%+v rejection=%v", result, rejection)
			}
			retry, retryRejection := rt.CommitOperations(context.Background(), input)
			if retryRejection == nil || !reflect.DeepEqual(retry, runtimeprotocol.RuntimeCommitResult{}) {
				t.Fatalf("retry=%+v rejection=%v", retry, retryRejection)
			}
			host.mu.Lock()
			defer host.mu.Unlock()
			record := host.records[input.OperationID]
			if host.publishCalls != 0 || host.abortCalls != 1 || record.Status.Phase != phase || record.Status.OperationResult != nil {
				t.Fatalf("advance failure state: publish=%d abort=%d record=%+v", host.publishCalls, host.abortCalls, record)
			}
		})
	}
}

func TestCoordinatorCreatePendingConflictResolvesBothUniquenessIndexes(t *testing.T) {
	for _, tc := range []struct {
		name   string
		mutate func(*runtimeprotocol.RuntimeCommitInput)
	}{
		{"same operation different key", func(input *runtimeprotocol.RuntimeCommitInput) { input.IdempotencyKey = "idem_commit_000002" }},
		{"same key different operation", func(input *runtimeprotocol.RuntimeCommitInput) { input.OperationID = "operation_2" }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			host, rt := newCoordinatorFixture(t)
			opened := openCoordinatorFixture(t, rt)
			first := commitFixture(opened.Session, host)
			second := first
			tc.mutate(&second)
			arrived := make(chan struct{}, 2)
			release := make(chan struct{})
			host.createPendingArrived = arrived
			host.createPendingRelease = release
			type outcome struct {
				result    runtimeprotocol.RuntimeCommitResult
				rejection *ContractError
			}
			outcomes := make(chan outcome, 2)
			for _, input := range []runtimeprotocol.RuntimeCommitInput{first, second} {
				go func(input runtimeprotocol.RuntimeCommitInput) {
					result, rejection := rt.CommitOperations(context.Background(), input)
					outcomes <- outcome{result: result, rejection: rejection}
				}(input)
			}
			<-arrived
			<-arrived
			close(release)

			successes, mismatches := 0, 0
			for range 2 {
				outcome := <-outcomes
				switch {
				case outcome.rejection == nil && outcome.result.OperationResult.Status == runtimeprotocol.OperationResultStatusCommitted:
					successes++
				case outcome.rejection != nil && outcome.rejection.Code == runtimeprotocol.RuntimeFailureCodeRuntimeIdempotencyMismatch && reflect.DeepEqual(outcome.result, runtimeprotocol.RuntimeCommitResult{}):
					mismatches++
				default:
					t.Fatalf("unexpected conflict-race outcome: result=%+v rejection=%v", outcome.result, outcome.rejection)
				}
			}
			if successes != 1 || mismatches != 1 {
				t.Fatalf("conflict race successes=%d mismatches=%d", successes, mismatches)
			}
		})
	}

	for _, tc := range []struct {
		name      string
		configure func(*coordinatorHost)
	}{
		{"disagreeing indexes", func(host *coordinatorHost) { host.createPendingDivergentRecords = true }},
		{"malformed record", func(host *coordinatorHost) {
			host.createPendingConflictRecord = true
			host.mutateGetRecord = func(_ port.GetRecoveryRecordInput, record *port.RecoveryRecord) { record.Scope.DocumentID = "" }
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			host, rt := newCoordinatorFixture(t)
			opened := openCoordinatorFixture(t, rt)
			tc.configure(host)
			result, rejection := rt.CommitOperations(context.Background(), commitFixture(opened.Session, host))
			if rejection == nil || rejection.Code != runtimeprotocol.RuntimeFailureCodeRuntimeCapabilityUnavailable || !reflect.DeepEqual(result, runtimeprotocol.RuntimeCommitResult{}) {
				t.Fatalf("inconsistent conflict state escaped: result=%+v rejection=%v", result, rejection)
			}
			host.mu.Lock()
			defer host.mu.Unlock()
			if host.recoveryGetCalls != 4 || host.publishCalls != 0 {
				t.Fatalf("conflict lookups=%d publish=%d", host.recoveryGetCalls, host.publishCalls)
			}
		})
	}
}

func TestCoordinatorPendingConflictFailsClosedOnIncompleteIndexes(t *testing.T) {
	for _, tc := range []struct {
		name      string
		configure func(*coordinatorHost, runtimeprotocol.RuntimeCommitInput, port.RecoveryRecord)
	}{
		{"same pending record", func(host *coordinatorHost, input runtimeprotocol.RuntimeCommitInput, record port.RecoveryRecord) {
			host.records[input.OperationID] = record
			host.keys[input.IdempotencyKey] = input.OperationID
		}},
		{"malformed idempotency index", func(host *coordinatorHost, input runtimeprotocol.RuntimeCommitInput, record port.RecoveryRecord) {
			host.records[input.OperationID] = record
			host.keys[input.IdempotencyKey] = input.OperationID
			host.mutateGetRecord = func(query port.GetRecoveryRecordInput, returned *port.RecoveryRecord) {
				if query.IdempotencyKey != nil {
					returned.Status.IdempotencyKey = "idem_commit_other01"
				}
			}
		}},
		{"missing idempotency index", func(host *coordinatorHost, input runtimeprotocol.RuntimeCommitInput, record port.RecoveryRecord) {
			host.records[input.OperationID] = record
		}},
		{"missing operation index", func(host *coordinatorHost, input runtimeprotocol.RuntimeCommitInput, record port.RecoveryRecord) {
			storageID := runtimeprotocol.OperationID("operation_storage")
			host.records[storageID] = record
			host.keys[input.IdempotencyKey] = storageID
		}},
		{"missing both indexes", func(*coordinatorHost, runtimeprotocol.RuntimeCommitInput, port.RecoveryRecord) {}},
		{"lookup failure", func(host *coordinatorHost, _ runtimeprotocol.RuntimeCommitInput, _ port.RecoveryRecord) {
			host.recoveryGetErrOnCall = 1
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			host, rt := newCoordinatorFixture(t)
			opened := openCoordinatorFixture(t, rt)
			input := commitFixture(opened.Session, host)
			payload := logicalCommitDigest(input)
			record := port.RecoveryRecord{Scope: input.Session.Scope, Status: runtimeprotocol.RuntimeOperationStatus{Phase: runtimeprotocol.RecoveryPhasePending, OperationID: input.OperationID, IdempotencyKey: input.IdempotencyKey}, PayloadDigest: payload, BaseRevision: input.OperationBatch.BaseRevision}
			tc.configure(host, input, record)
			coordinator := rt.config.Operations.CommitOperations.(*Coordinator)
			result, rejection := coordinator.resolvePendingConflict(context.Background(), input, payload)
			if rejection == nil || rejection.Code != runtimeprotocol.RuntimeFailureCodeRuntimeCapabilityUnavailable || !reflect.DeepEqual(result, runtimeprotocol.RuntimeCommitResult{}) {
				t.Fatalf("incomplete indexes escaped: result=%+v rejection=%v", result, rejection)
			}
			host.mu.Lock()
			defer host.mu.Unlock()
			if host.recoveryGetCalls != 2 {
				t.Fatalf("resolved %d uniqueness indexes, want 2", host.recoveryGetCalls)
			}
		})
	}
}

func TestCoordinatorFailsClosedOnMalformedRecoveryOutputs(t *testing.T) {
	createMutations := []struct {
		name   string
		mutate func(*port.RecoveryRecord)
	}{
		{"scope", func(record *port.RecoveryRecord) { record.Scope.DocumentID = "doc_other" }},
		{"operation", func(record *port.RecoveryRecord) { record.Status.OperationID = "operation_other" }},
		{"idempotency", func(record *port.RecoveryRecord) { record.Status.IdempotencyKey = "idem_commit_other01" }},
		{"payload", func(record *port.RecoveryRecord) { record.PayloadDigest = digest('0') }},
		{"base", func(record *port.RecoveryRecord) { record.BaseRevision.RevisionID = "rev_other" }},
		{"phase", func(record *port.RecoveryRecord) { record.Status.Phase = runtimeprotocol.RecoveryPhaseStaged }},
	}
	for _, tc := range createMutations {
		t.Run("create_"+tc.name, func(t *testing.T) {
			host, rt := newCoordinatorFixture(t)
			host.mutateCreateRecord = tc.mutate
			opened := openCoordinatorFixture(t, rt)
			input := commitFixture(opened.Session, host)
			result, rejection := rt.CommitOperations(context.Background(), input)
			if rejection == nil || !reflect.DeepEqual(result, runtimeprotocol.RuntimeCommitResult{}) {
				t.Fatalf("malformed create output escaped: result=%+v rejection=%v", result, rejection)
			}
			host.mu.Lock()
			defer host.mu.Unlock()
			if host.publishCalls != 0 || host.records[input.OperationID].Status.Phase != runtimeprotocol.RecoveryPhasePending {
				t.Fatalf("malformed create output published or lost reservation: %+v", host.records[input.OperationID])
			}
		})
	}

	advanceMutations := []struct {
		name   string
		mutate func(*port.RecoveryRecord)
	}{
		{"scope", func(record *port.RecoveryRecord) { record.Scope.DocumentID = "doc_other" }},
		{"operation", func(record *port.RecoveryRecord) { record.Status.OperationID = "operation_other" }},
		{"payload", func(record *port.RecoveryRecord) { record.PayloadDigest = digest('0') }},
		{"base", func(record *port.RecoveryRecord) { record.BaseRevision.RevisionID = "rev_other" }},
		{"phase", func(record *port.RecoveryRecord) { record.Status.Phase = runtimeprotocol.RecoveryPhasePending }},
		{"evidence", func(record *port.RecoveryRecord) { record.DecisionDigest = ptr(digest('0')) }},
	}
	for _, tc := range advanceMutations {
		t.Run("advance_"+tc.name, func(t *testing.T) {
			host, rt := newCoordinatorFixture(t)
			host.mutateAdvanceRecord = tc.mutate
			opened := openCoordinatorFixture(t, rt)
			input := commitFixture(opened.Session, host)
			result, rejection := rt.CommitOperations(context.Background(), input)
			if rejection == nil || !reflect.DeepEqual(result, runtimeprotocol.RuntimeCommitResult{}) {
				t.Fatalf("malformed advance output escaped: result=%+v rejection=%v", result, rejection)
			}
			host.mu.Lock()
			defer host.mu.Unlock()
			if host.publishCalls != 0 || host.abortCalls != 1 || host.records[input.OperationID].Status.Phase != runtimeprotocol.RecoveryPhaseStaged {
				t.Fatalf("malformed advance output state: publish=%d abort=%d record=%+v", host.publishCalls, host.abortCalls, host.records[input.OperationID])
			}
		})
	}

	finalizeMutations := []struct {
		name   string
		mutate func(*port.RecoveryRecord)
	}{
		{"scope", func(record *port.RecoveryRecord) { record.Scope.DocumentID = "doc_other" }},
		{"operation", func(record *port.RecoveryRecord) { record.Status.OperationID = "operation_other" }},
		{"payload", func(record *port.RecoveryRecord) { record.PayloadDigest = digest('0') }},
		{"base", func(record *port.RecoveryRecord) { record.BaseRevision.RevisionID = "rev_other" }},
		{"phase", func(record *port.RecoveryRecord) { record.Status.Phase = runtimeprotocol.RecoveryPhasePending }},
		{"result", func(record *port.RecoveryRecord) {
			record.CommitResult.OperationResult.ResultDigest = digest('0')
		}},
		{"forged result digest", func(record *port.RecoveryRecord) {
			statusResult := *record.Status.OperationResult
			statusResult.ResultDigest = digest('0')
			record.Status.OperationResult = &statusResult
			commit := *record.CommitResult
			commit.OperationResult = statusResult
			record.CommitResult = &commit
		}},
	}
	for _, tc := range finalizeMutations {
		t.Run("finalize_"+tc.name, func(t *testing.T) {
			host, rt := newCoordinatorFixture(t)
			host.mutateFinalizeRecord = tc.mutate
			opened := openCoordinatorFixture(t, rt)
			host.mu.Lock()
			host.head.Revision = runtimeprotocol.CommittedRevisionRef{DocumentID: host.base.DocumentID, RevisionID: "rev_current", DefinitionHash: digest('d'), GraphHash: digest('e')}
			host.mu.Unlock()
			input := commitFixture(opened.Session, host)
			result, rejection := rt.CommitOperations(context.Background(), input)
			if rejection == nil || !reflect.DeepEqual(result, runtimeprotocol.RuntimeCommitResult{}) {
				t.Fatalf("malformed finalize output escaped: result=%+v rejection=%v", result, rejection)
			}
			host.mu.Lock()
			host.mutateFinalizeRecord = nil
			publications := host.publishCalls
			host.mu.Unlock()
			retry, retryRejection := rt.CommitOperations(context.Background(), input)
			if retryRejection != nil || retry.OperationResult.Status != runtimeprotocol.OperationResultStatusRejected || publications != 0 {
				t.Fatalf("durable finalize retry=%+v rejection=%v publications=%d", retry, retryRejection, publications)
			}
			assertCommitResultEncodes(t, retry)
		})
	}
}

func TestValidRecoveryRecordRejectsMalformedJournalShapes(t *testing.T) {
	host, rt := newCoordinatorFixture(t)
	opened := openCoordinatorFixture(t, rt)
	input := commitFixture(opened.Session, host)
	if _, rejection := rt.CommitOperations(context.Background(), input); rejection != nil {
		t.Fatal(rejection)
	}
	host.mu.Lock()
	terminal := cloneRecoveryRecordForTest(host.records[input.OperationID])
	host.mu.Unlock()
	if !validRecoveryRecord(terminal) {
		t.Fatal("valid terminal recovery record was rejected")
	}

	pending := port.RecoveryRecord{
		Scope: terminal.Scope,
		Status: runtimeprotocol.RuntimeOperationStatus{
			Phase:          runtimeprotocol.RecoveryPhasePending,
			OperationID:    terminal.Status.OperationID,
			IdempotencyKey: terminal.Status.IdempotencyKey,
		},
		PayloadDigest: terminal.PayloadDigest,
		BaseRevision:  terminal.BaseRevision,
	}
	staged := pending
	staged.Status.Phase = runtimeprotocol.RecoveryPhaseStaged
	staged.EvaluationDigest = ptr(input.AuthoringProof.EvaluationDigest)
	staged.DecisionDigest = ptr(input.AuthoringProof.DecisionDigest)
	if !validRecoveryRecord(pending) || !validRecoveryRecord(staged) {
		t.Fatal("valid prepublication recovery record was rejected")
	}
	needsReviewPhaseMismatch := cloneRecoveryRecordForTest(terminal)
	needsReviewPhaseMismatch.Status.Phase = runtimeprotocol.RecoveryPhaseNeedsReview
	needsReviewResult := operationResult(input, runtimeprotocol.OperationResultStatusNeedsReview, nil, nil, "")
	needsReviewCommit := runtimeprotocol.RuntimeCommitResult{OperationResult: needsReviewResult, PreviewEvaluation: terminal.CommitResult.PreviewEvaluation}
	finalNeedsReviewMismatch := cloneRecoveryRecordForTest(terminal)
	finalNeedsReviewMismatch.Status.OperationResult = &needsReviewResult
	finalNeedsReviewMismatch.CommitResult = &needsReviewCommit

	for _, tc := range []struct {
		name   string
		base   port.RecoveryRecord
		mutate func(*port.RecoveryRecord)
	}{
		{"scope codec", pending, func(record *port.RecoveryRecord) { record.Scope.DocumentID = "" }},
		{"status codec", pending, func(record *port.RecoveryRecord) { record.Status.Phase = "invalid" }},
		{"base codec", pending, func(record *port.RecoveryRecord) { record.BaseRevision.RevisionID = "" }},
		{"payload codec", pending, func(record *port.RecoveryRecord) { record.PayloadDigest = "" }},
		{"one-sided evidence", staged, func(record *port.RecoveryRecord) { record.DecisionDigest = nil }},
		{"pending evidence", pending, func(record *port.RecoveryRecord) {
			record.EvaluationDigest = ptr(digest('1'))
			record.DecisionDigest = ptr(digest('2'))
		}},
		{"staged evidence missing", staged, func(record *port.RecoveryRecord) { record.EvaluationDigest = nil; record.DecisionDigest = nil }},
		{"nonterminal commit result", pending, func(record *port.RecoveryRecord) { record.CommitResult = terminal.CommitResult }},
		{"terminal result mismatch", terminal, func(record *port.RecoveryRecord) {
			changed := *record.Status.OperationResult
			changed.ResultDigest = digest('0')
			record.Status.OperationResult = &changed
		}},
		{"terminal forged identity", terminal, func(record *port.RecoveryRecord) {
			changed := *record.Status.OperationResult
			changed.OperationID = "operation_other"
			changed.ResultDigest = digestOperationResult(changed)
			record.Status.OperationResult = &changed
			commit := *record.CommitResult
			commit.OperationResult = changed
			record.CommitResult = &commit
		}},
		{"terminal forged digest", terminal, func(record *port.RecoveryRecord) {
			changed := *record.Status.OperationResult
			changed.ResultDigest = digest('0')
			record.Status.OperationResult = &changed
			commit := *record.CommitResult
			commit.OperationResult = changed
			record.CommitResult = &commit
		}},
		{"terminal commit codec", terminal, func(record *port.RecoveryRecord) {
			changed := *record.CommitResult
			evaluation := *changed.PreviewEvaluation
			evaluation.AuthoringImpact.ImpactDigest = ""
			changed.PreviewEvaluation = &evaluation
			record.CommitResult = &changed
		}},
		{"needs review phase mismatch", needsReviewPhaseMismatch, func(*port.RecoveryRecord) {}},
		{"final needs review mismatch", finalNeedsReviewMismatch, func(*port.RecoveryRecord) {}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			record := cloneRecoveryRecordForTest(tc.base)
			tc.mutate(&record)
			if validRecoveryRecord(record) {
				t.Fatalf("malformed recovery record escaped: %+v", record)
			}
		})
	}

	coordinator := rt.config.Operations.CommitOperations.(*Coordinator)
	if _, rejection := coordinator.advance(context.Background(), input, runtimeprotocol.RecoveryPhasePending, runtimeprotocol.RecoveryPhasePublicationPending, nil, &host.decision); rejection == nil || rejection.Code != runtimeprotocol.RuntimeFailureCodeRuntimeInvalidRecoveryTransition {
		t.Fatalf("invalid local recovery transition escaped: %v", rejection)
	}
}

func TestCoordinatorRetryAndGetRejectForgedTerminalResultDigest(t *testing.T) {
	host, rt := newCoordinatorFixture(t)
	opened := openCoordinatorFixture(t, rt)
	input := commitFixture(opened.Session, host)
	if _, rejection := rt.CommitOperations(context.Background(), input); rejection != nil {
		t.Fatal(rejection)
	}
	host.mu.Lock()
	record := cloneRecoveryRecordForTest(host.records[input.OperationID])
	forged := *record.Status.OperationResult
	forged.ResultDigest = digest('0')
	record.Status.OperationResult = &forged
	commit := *record.CommitResult
	commit.OperationResult = forged
	record.CommitResult = &commit
	host.records[input.OperationID] = record
	host.mu.Unlock()

	if result, rejection := rt.CommitOperations(context.Background(), input); rejection == nil || !reflect.DeepEqual(result, runtimeprotocol.RuntimeCommitResult{}) {
		t.Fatalf("forged retry escaped: result=%+v rejection=%v", result, rejection)
	}
	operationID := input.OperationID
	if status, rejection := rt.GetOperationResult(context.Background(), runtimeprotocol.GetOperationResultInput{Session: opened.Session, LookupBy: "operation_id", OperationID: &operationID}); rejection == nil || !reflect.DeepEqual(status, runtimeprotocol.RuntimeOperationStatus{}) {
		t.Fatalf("forged GetOperationResult escaped: status=%+v rejection=%v", status, rejection)
	}
}

func TestCoordinatorStaleBaseReturnsTrustedEvidenceWithoutPreview(t *testing.T) {
	host, rt := newCoordinatorFixture(t)
	opened := openCoordinatorFixture(t, rt)
	host.mu.Lock()
	host.head.Revision = runtimeprotocol.CommittedRevisionRef{DocumentID: host.base.DocumentID, RevisionID: "rev_current", DefinitionHash: digest('d'), GraphHash: digest('e')}
	want := host.head.Revision
	host.mu.Unlock()

	result, rejection := rt.CommitOperations(context.Background(), commitFixture(opened.Session, host))
	if rejection != nil || result.OperationResult.Status != runtimeprotocol.OperationResultStatusRejected {
		t.Fatalf("result=%+v rejection=%v", result, rejection)
	}
	assertCommitResultEncodes(t, result)
	if result.PreviewEvaluation != nil || result.OperationResult.ConflictEvidence == nil || result.OperationResult.ConflictEvidence.CurrentHead != want {
		t.Fatalf("stale-base evidence or preview was incorrect: %+v", result)
	}
	firstBytes, err := runtimeprotocol.EncodeRuntimeCommitResult(result)
	if err != nil {
		t.Fatal(err)
	}
	host.mu.Lock()
	host.head.Revision = runtimeprotocol.CommittedRevisionRef{DocumentID: host.base.DocumentID, RevisionID: "rev_later", DefinitionHash: digest('f'), GraphHash: digest('0')}
	host.mu.Unlock()
	retry, retryRejection := rt.CommitOperations(context.Background(), commitFixture(opened.Session, host))
	if retryRejection != nil || !reflect.DeepEqual(retry, result) {
		t.Fatalf("stale-base retry changed after head movement: retry=%+v rejection=%v", retry, retryRejection)
	}
	retryBytes, err := runtimeprotocol.EncodeRuntimeCommitResult(retry)
	if err != nil || !reflect.DeepEqual(retryBytes, firstBytes) {
		t.Fatalf("stale-base retry bytes changed: %v", err)
	}
	host.mu.Lock()
	defer host.mu.Unlock()
	if host.previewCalls != 0 || host.publishCalls != 0 {
		t.Fatalf("stale base reached preview/publication: preview=%d publish=%d", host.previewCalls, host.publishCalls)
	}
}

func TestCoordinatorCancellationAfterEvaluationFinalizesWithoutPublication(t *testing.T) {
	host, rt := newCoordinatorFixture(t)
	opened := openCoordinatorFixture(t, rt)
	ctx, cancel := context.WithCancel(context.Background())
	host.afterPreview = cancel

	result, rejection := rt.CommitOperations(ctx, commitFixture(opened.Session, host))
	if rejection != nil || result.OperationResult.Status != runtimeprotocol.OperationResultStatusRejected || result.PreviewEvaluation == nil {
		t.Fatalf("result=%+v rejection=%v", result, rejection)
	}
	assertCommitResultEncodes(t, result)
	host.mu.Lock()
	defer host.mu.Unlock()
	if host.publishCalls != 0 {
		t.Fatalf("post-evaluation cancellation published: %d", host.publishCalls)
	}
}

func TestCoordinatorCancellationLinearizesAroundPublicationAndCleansState(t *testing.T) {
	t.Run("before publication", func(t *testing.T) {
		host, rt := newCoordinatorFixture(t)
		opened := openCoordinatorFixture(t, rt)
		input := commitFixture(opened.Session, host)
		input.CancellationToken = ptr(runtimeprotocol.CancellationToken("cancellation_token_0001"))
		var cancellation runtimeprotocol.CancelOperationResult
		host.afterPreview = func() {
			var rejection *ContractError
			cancellation, rejection = rt.CancelOperation(context.Background(), runtimeprotocol.CancelOperationInput{Session: opened.Session, OperationID: input.OperationID, CancellationToken: *input.CancellationToken})
			if rejection != nil {
				t.Errorf("cancel rejection: %v", rejection)
			}
		}
		result, rejection := rt.CommitOperations(context.Background(), input)
		if rejection != nil || result.OperationResult.Status != runtimeprotocol.OperationResultStatusRejected || cancellation.Status != "cancel_requested" {
			t.Fatalf("result=%+v rejection=%v cancellation=%+v", result, rejection, cancellation)
		}
		assertCommitResultEncodes(t, result)
		post, postRejection := rt.CancelOperation(context.Background(), runtimeprotocol.CancelOperationInput{Session: opened.Session, OperationID: input.OperationID, CancellationToken: *input.CancellationToken})
		if postRejection != nil || post.Status != "not_pending" {
			t.Fatalf("terminal cancellation state was not cleaned: %+v %v", post, postRejection)
		}
	})

	t.Run("publication started", func(t *testing.T) {
		host, rt := newCoordinatorFixture(t)
		opened := openCoordinatorFixture(t, rt)
		input := commitFixture(opened.Session, host)
		input.CancellationToken = ptr(runtimeprotocol.CancellationToken("cancellation_token_0001"))
		var cancellation runtimeprotocol.CancelOperationResult
		host.onPublish = func() {
			var rejection *ContractError
			cancellation, rejection = rt.CancelOperation(context.Background(), runtimeprotocol.CancelOperationInput{Session: opened.Session, OperationID: input.OperationID, CancellationToken: *input.CancellationToken})
			if rejection != nil {
				t.Errorf("cancel rejection: %v", rejection)
			}
		}
		result, rejection := rt.CommitOperations(context.Background(), input)
		if rejection != nil || result.OperationResult.Status != runtimeprotocol.OperationResultStatusCommitted || cancellation.Status != "too_late" || cancellation.Phase != runtimeprotocol.RecoveryPhasePublicationPending {
			t.Fatalf("result=%+v rejection=%v cancellation=%+v", result, rejection, cancellation)
		}
		assertCommitResultEncodes(t, result)
	})
}

func TestCoordinatorCancellationIsScopedToTrustedSession(t *testing.T) {
	host, rt := newCoordinatorFixture(t)
	opened := openCoordinatorFixture(t, rt)
	coordinator := rt.config.Operations.CommitOperations.(*Coordinator)
	state, rejection := coordinator.session(opened.Session)
	if rejection != nil {
		t.Fatal(rejection)
	}
	other := opened.Session
	other.RuntimeSessionID = "runtime_session_other_document"
	other.Scope.DocumentID = "doc_other"
	state.binding.Session = other
	state.binding.CurrentRevision.DocumentID = other.Scope.DocumentID
	coordinator.mu.Lock()
	coordinator.sessions[other.RuntimeSessionID] = &state
	coordinator.mu.Unlock()

	input := commitFixture(opened.Session, host)
	input.CancellationToken = ptr(runtimeprotocol.CancellationToken("cancellation_token_0001"))
	if cancellationRejection := coordinator.checkCancellation(context.Background(), input); cancellationRejection != nil {
		t.Fatal(cancellationRejection)
	}
	result, cancelRejection := rt.CancelOperation(context.Background(), runtimeprotocol.CancelOperationInput{Session: other, OperationID: input.OperationID, CancellationToken: *input.CancellationToken})
	if cancelRejection != nil || result.Status != "not_pending" {
		t.Fatalf("cross-document cancel=%+v rejection=%v", result, cancelRejection)
	}
	if cancellationRejection := coordinator.checkCancellation(context.Background(), input); cancellationRejection != nil {
		t.Fatalf("cross-document cancellation leaked: %v", cancellationRejection)
	}
	coordinator.finishOperation(opened.Session, input.OperationID)
}

func TestCoordinatorPendingRetryDoesNotCleanActiveCancellationState(t *testing.T) {
	host, rt := newCoordinatorFixture(t)
	opened := openCoordinatorFixture(t, rt)
	input := commitFixture(opened.Session, host)
	input.CancellationToken = ptr(runtimeprotocol.CancellationToken("cancellation_token_0001"))
	coordinator := rt.config.Operations.CommitOperations.(*Coordinator)
	if rejection := coordinator.checkCancellation(context.Background(), input); rejection != nil {
		t.Fatal(rejection)
	}
	payload := logicalCommitDigest(input)
	if _, err := host.CreatePending(context.Background(), port.CreatePendingRecordInput{Scope: opened.Session.Scope, OperationID: input.OperationID, IdempotencyKey: input.IdempotencyKey, PayloadDigest: payload, BaseRevision: host.base}); err != nil {
		t.Fatal(err)
	}
	if _, rejection := rt.CommitOperations(context.Background(), input); rejection == nil {
		t.Fatal("pending retry unexpectedly returned a terminal result")
	}
	coordinator.mu.RLock()
	_, exists := coordinator.cancels[cancellationKey(opened.Session, input.OperationID)]
	coordinator.mu.RUnlock()
	if !exists {
		t.Fatal("pending retry cleaned another commit's cancellation state")
	}
	coordinator.finishOperation(opened.Session, input.OperationID)
}

func TestCoordinatorAbandonedTransportAndAuthorizationFailuresCleanCancellationState(t *testing.T) {
	for _, tc := range []struct {
		name      string
		configure func(*coordinatorHost, *runtimeprotocol.RuntimeCommitInput)
	}{
		{"stage transport", func(host *coordinatorHost, _ *runtimeprotocol.RuntimeCommitInput) {
			host.stageErr = errors.New("injected stage failure")
		}},
		{"grant transport", func(host *coordinatorHost, _ *runtimeprotocol.RuntimeCommitInput) {
			host.grantErr = errors.New("injected grant failure")
		}},
		{"authorization proof", func(_ *coordinatorHost, input *runtimeprotocol.RuntimeCommitInput) {
			input.AuthoringProof.DecisionDigest = digest('e')
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			host, rt := newCoordinatorFixture(t)
			opened := openCoordinatorFixture(t, rt)
			input := commitFixture(opened.Session, host)
			input.CancellationToken = ptr(runtimeprotocol.CancellationToken("cancellation_token_0001"))
			tc.configure(host, &input)
			coordinator := rt.config.Operations.CommitOperations.(*Coordinator)

			for _, token := range []runtimeprotocol.CancellationToken{"cancellation_token_0001", "cancellation_token_0002"} {
				input.CancellationToken = &token
				result, rejection := rt.CommitOperations(context.Background(), input)
				if rejection == nil || rejection.Code == runtimeprotocol.RuntimeFailureCodeRuntimeIdempotencyMismatch || !reflect.DeepEqual(result, runtimeprotocol.RuntimeCommitResult{}) {
					t.Fatalf("abandoned failure result=%+v rejection=%v", result, rejection)
				}
				coordinator.mu.RLock()
				_, exists := coordinator.cancels[cancellationKey(opened.Session, input.OperationID)]
				coordinator.mu.RUnlock()
				if exists {
					t.Fatal("successful conditional abandon leaked cancellation state")
				}
			}
		})
	}
}

func TestCoordinatorLogicalIdempotencyAndResultDigest(t *testing.T) {
	host, rt := newCoordinatorFixture(t)
	opened := openCoordinatorFixture(t, rt)
	input := commitFixture(opened.Session, host)
	input.StateMutation = stateMutationFixture(opened.Session, "mutation_blob_initial", "2")
	first, rejection := rt.CommitOperations(context.Background(), input)
	if rejection != nil {
		t.Fatal(rejection)
	}
	reopened := openCoordinatorFixture(t, rt)
	retryInput := input
	retryInput.Session = reopened.Session
	retryInput.CancellationToken = ptr(runtimeprotocol.CancellationToken("cancellation_token_0001"))
	retryInput.LeaseToken = ptr(runtimeprotocol.LeaseToken("lease_token_0001"))
	retryInput.AuthoringProof.DecisionDigest = digest('e')
	retryInput.StateMutation.MutationBlob.Blob.BlobID = "mutation_blob_reconnected"
	retryInput.StateMutation.MutationBlob.Blob.Lifetime = protocolcommon.BlobLifetimeRequest
	retry, rejection := rt.CommitOperations(context.Background(), retryInput)
	if rejection != nil || !reflect.DeepEqual(first, retry) {
		t.Fatalf("logical retry changed across reconnect: %+v %v", retry, rejection)
	}
	different := retryInput
	different.Trigger = runtimeprotocol.CommitTriggerAutosave
	if _, rejection := rt.CommitOperations(context.Background(), different); rejection == nil || rejection.Code != runtimeprotocol.RuntimeFailureCodeRuntimeIdempotencyMismatch {
		t.Fatalf("different intent reused key: %v", rejection)
	}
	if got := digestOperationResult(first.OperationResult); got != first.OperationResult.ResultDigest {
		t.Fatalf("result digest=%s recomputed=%s", first.OperationResult.ResultDigest, got)
	}
	tampered := first.OperationResult
	tampered.Status = runtimeprotocol.OperationResultStatusCommittedStateStale
	if digestOperationResult(tampered) == first.OperationResult.ResultDigest {
		t.Fatal("result digest did not detect tampering")
	}
	host.mu.Lock()
	stateWrite := host.stateWriteInput
	host.mu.Unlock()
	if stateWrite.ExpectedBackendVersion != "state-4" || stateWrite.ExpectedDefinitionHash != host.base.DefinitionHash {
		t.Fatalf("state write did not use the trusted open-time state head: %+v", stateWrite)
	}
}

func TestRuntimeDigestUsesCrossLanguageCanonicalJSON(t *testing.T) {
	data, err := os.ReadFile("../../schemas/fixtures/runtime/digest-canonicalization.json")
	if err != nil {
		t.Fatal(err)
	}
	var fixture struct {
		Canonical string                `json:"canonical"`
		SHA256    protocolcommon.Digest `json:"sha256"`
		Value     map[string]any        `json:"value"`
	}
	if err := json.Unmarshal(data, &fixture); err != nil {
		t.Fatal(err)
	}
	canonical, err := canonicalRuntimeJSON(fixture.Value)
	if err != nil || string(canonical) != fixture.Canonical {
		t.Fatalf("canonical=%q want=%q err=%v", canonical, fixture.Canonical, err)
	}
	if got := digestValue(fixture.Value); got != fixture.SHA256 {
		t.Fatalf("digest=%s want=%s", got, fixture.SHA256)
	}
}

func TestRuntimeCanonicalJSONRejectsUnsupportedPreimages(t *testing.T) {
	value := map[string]any{
		"a":  []any{nil, true, json.Number("1"), "<&"},
		"aa": map[string]any{"x": false},
	}
	if _, err := canonicalRuntimeJSON(value); err != nil {
		t.Fatalf("canonical supported value: %v", err)
	}
	if _, err := canonicalRuntimeJSON(func() {}); err == nil {
		t.Fatal("marshal-hostile preimage was accepted")
	}
	if got := digestValue(func() {}); got != "" {
		t.Fatalf("marshal-hostile digest=%q", got)
	}
	for _, value := range []any{
		json.Number("1.0"),
		[]any{float64(1)},
		map[string]any{"x": float64(1)},
		make(chan int),
	} {
		if _, err := appendCanonicalRuntimeJSON(nil, value); err == nil {
			t.Fatalf("unsupported canonical value %T was accepted", value)
		}
	}
}

func TestCoordinatorInjectedPortValuesFailClosed(t *testing.T) {
	host, _ := newCoordinatorFixture(t)
	scope := runtimeprotocol.RuntimeScope{DocumentID: host.base.DocumentID, LocalScopeID: "local_fixture", AccessFingerprint: host.grant.AccessFingerprint}

	for _, mutate := range []func(*port.StateHead){
		func(v *port.StateHead) { v.StateVersion = "" },
		func(v *port.StateHead) { v.BackendVersion = "" },
		func(v *port.StateHead) { v.DefinitionHash = "" },
		func(v *port.StateHead) { v.GraphHash = "" },
		func(v *port.StateHead) { v.CapturedAt = "" },
		func(v *port.StateHead) {
			v.SubjectHashes = map[semantic.StableAddress]protocolcommon.Digest{"invalid": digest('1')}
		},
		func(v *port.StateHead) {
			v.SubjectHashes = map[semantic.StableAddress]protocolcommon.Digest{"ldl:project:fixture:entity:item": "invalid"}
		},
	} {
		value := host.stateHead
		mutate(&value)
		if validStateHead(value) {
			t.Fatalf("invalid state head escaped: %+v", value)
		}
	}

	for _, mutate := range []func(*port.DocumentHead){
		func(v *port.DocumentHead) { v.Revision.RevisionID = "" },
		func(v *port.DocumentHead) { v.ProviderVersion = "" },
		func(v *port.DocumentHead) { v.FencingToken = "" },
	} {
		value := host.head
		mutate(&value)
		if validDocumentHead(value) {
			t.Fatalf("invalid document head escaped: %+v", value)
		}
	}

	validBlob := protocolcommon.BlobRef{BlobID: "manifest", Digest: digest('d'), Lifetime: protocolcommon.BlobLifetimePersistent, MediaType: "application/json", Size: "2"}
	validSource := sourceBlobForContents("source-valid", protocolcommon.BlobLifetimePersistent, "text/ldl", []byte("entity valid {}\n"))
	validSnapshot := port.RevisionSnapshot{Revision: host.base, Manifest: validBlob, SourceBlobs: []protocolcommon.BlobRef{validSource.Ref}}
	for _, mutate := range []func(*port.RevisionSnapshot){
		func(v *port.RevisionSnapshot) { v.Revision.DocumentID = "doc_other" },
		func(v *port.RevisionSnapshot) { v.Revision.RevisionID = "other" },
		func(v *port.RevisionSnapshot) { v.Revision.GraphHash = "" },
		func(v *port.RevisionSnapshot) { v.Manifest.Digest = "" },
		func(v *port.RevisionSnapshot) { v.SourceBlobs[0].Digest = "" },
	} {
		value := validSnapshot
		value.SourceBlobs = append([]protocolcommon.BlobRef(nil), validSnapshot.SourceBlobs...)
		mutate(&value)
		if validRevisionSnapshot(value, scope, host.base.RevisionID) {
			t.Fatalf("invalid revision snapshot escaped: %+v", value)
		}
	}

	prepared := port.PreparedRevision{AuthoringImpact: host.impact, DefinitionHash: host.impact.ResultingDefinitionHash, GraphHash: digest('c'), Sources: port.SourceBlobSet{Revision: host.base, Blobs: []port.SourceBlob{validSource}}, Manifest: validBlob}
	for _, mutate := range []func(*port.PreparedRevision){
		func(v *port.PreparedRevision) { v.DefinitionHash = digest('0') },
		func(v *port.PreparedRevision) { v.Sources.Revision.RevisionID = "other" },
		func(v *port.PreparedRevision) { v.AuthoringImpact.BaseDefinitionHash = digest('0') },
		func(v *port.PreparedRevision) { v.AuthoringImpact.ImpactDigest = "" },
		func(v *port.PreparedRevision) { v.DefinitionHash = ""; v.AuthoringImpact.ResultingDefinitionHash = "" },
		func(v *port.PreparedRevision) { v.GraphHash = "" },
		func(v *port.PreparedRevision) { v.Manifest.Digest = "" },
		func(v *port.PreparedRevision) { v.Sources.Blobs[0].Ref.Digest = "" },
	} {
		value := prepared
		value.Sources.Blobs = append([]port.SourceBlob(nil), prepared.Sources.Blobs...)
		mutate(&value)
		if validPreparedRevision(value, host.base) {
			t.Fatalf("invalid prepared revision escaped: %+v", value)
		}
	}

	validStaged := port.StagedRevision{StageID: "stage", Revision: runtimeprotocol.CommittedRevisionRef{DocumentID: host.base.DocumentID, RevisionID: "rev_staged", DefinitionHash: prepared.DefinitionHash, GraphHash: prepared.GraphHash}, StagedDigest: digest('e')}
	for _, mutate := range []func(*port.StagedRevision){
		func(v *port.StagedRevision) { v.StageID = "" },
		func(v *port.StagedRevision) { v.Revision.RevisionID = "" },
		func(v *port.StagedRevision) { v.Revision.RevisionID = prepared.Sources.Revision.RevisionID },
		func(v *port.StagedRevision) { v.StagedDigest = "" },
	} {
		value := validStaged
		mutate(&value)
		if validStagedRevision(value, scope, prepared) {
			t.Fatalf("invalid staged revision escaped: %+v", value)
		}
	}

	if cloneSubjectHashes(nil) != nil {
		t.Fatal("nil subject hashes were not preserved")
	}
	for _, value := range []any{
		[]any{map[string]any{"digest": string(digest('1')), "size": "3"}},
		map[string]any{"digest": string(digest('1')), "size": "invalid"},
		map[string]any{"digest": string(digest('1')), "size": "11"},
		[]any{map[string]any{"digest": string(digest('1')), "size": "6"}, map[string]any{"digest": string(digest('2')), "size": "6"}},
	} {
		total := uint64(0)
		got := accumulateBlobSizes(value, 10, 10, &total)
		want := reflect.DeepEqual(value, []any{map[string]any{"digest": string(digest('1')), "size": "3"}})
		if got != want {
			t.Fatalf("blob accumulation=%t want=%t for %#v", got, want, value)
		}
	}
	if portFailure("cancelled port", context.Canceled).Code != runtimeprotocol.RuntimeFailureCodeRuntimeCancelled {
		t.Fatal("cancelled port failure lost cancellation classification")
	}
	if portFailure("deadline port", context.DeadlineExceeded).Code != runtimeprotocol.RuntimeFailureCodeRuntimeCancelled {
		t.Fatal("deadline port failure lost cancellation classification")
	}
}

func TestGeneratedRuntimeResultRejectsEmptyRequiredTerminalDiagnostics(t *testing.T) {
	host, rt := newCoordinatorFixture(t)
	opened := openCoordinatorFixture(t, rt)
	input := commitFixture(opened.Session, host)
	stateVersion := protocolcommon.CanonicalNonNegativeInt64("4")
	for _, result := range []runtimeprotocol.OperationResult{
		operationResult(input, runtimeprotocol.OperationResultStatusCommittedStateStale, &host.base, &stateVersion, ""),
		operationResult(input, runtimeprotocol.OperationResultStatusNeedsReview, nil, nil, ""),
	} {
		result.Diagnostics = []semantic.Diagnostic{}
		result.ResultDigest = digestOperationResult(result)
		if _, err := runtimeprotocol.EncodeRuntimeCommitResult(runtimeprotocol.RuntimeCommitResult{OperationResult: result}); err == nil {
			t.Fatalf("status %s encoded without its required diagnostic", result.Status)
		}
	}
}

func TestCoordinatorSequentialStateCommitsRefreshCompleteTrustedHead(t *testing.T) {
	host, rt := newCoordinatorFixture(t)
	opened := openCoordinatorFixture(t, rt)
	firstInput := commitFixture(opened.Session, host)
	firstInput.StateMutation = stateMutationFixture(opened.Session, "mutation_blob_first", "2")
	first, rejection := rt.CommitOperations(context.Background(), firstInput)
	if rejection != nil || first.OperationResult.Status != runtimeprotocol.OperationResultStatusCommitted {
		t.Fatalf("first=%+v rejection=%v", first, rejection)
	}
	assertCommitResultEncodes(t, first)

	secondInput := commitAtCurrentHeadFixture(opened.Session, host, "operation_2", "idem_commit_000002")
	secondInput.StateMutation = stateMutationFixture(opened.Session, "mutation_blob_second", "2")
	secondInput.StateMutation.ExpectedStateVersion = "5"
	second, rejection := rt.CommitOperations(context.Background(), secondInput)
	if rejection != nil || second.OperationResult.Status != runtimeprotocol.OperationResultStatusCommitted || second.OperationResult.StateVersion == nil || *second.OperationResult.StateVersion != "6" {
		t.Fatalf("second=%+v rejection=%v", second, rejection)
	}
	assertCommitResultEncodes(t, second)
	host.mu.Lock()
	defer host.mu.Unlock()
	if len(host.stateWrites) != 2 || host.stateWrites[1].ExpectedBackendVersion != "state-5" || host.stateWrites[1].ExpectedDefinitionHash != first.OperationResult.CommittedRevision.DefinitionHash || !reflect.DeepEqual(host.stateWrites[1].ExpectedSubjectHashes, map[semantic.StableAddress]protocolcommon.Digest{"ldl:project:fixture:entity:obsolete": digest('6')}) {
		t.Fatalf("second write did not use refreshed state head: %+v", host.stateWrites)
	}
}

func TestCoordinatorRejectsTamperedSessionsAndLimitsBeforePorts(t *testing.T) {
	host, rt := newCoordinatorFixture(t)
	opened := openCoordinatorFixture(t, rt)
	for _, tampered := range []runtimeprotocol.RuntimeSessionRef{
		func() runtimeprotocol.RuntimeSessionRef {
			value := opened.Session
			value.Scope.LocalScopeID = "attacker"
			return value
		}(),
		func() runtimeprotocol.RuntimeSessionRef {
			value := opened.Session
			value.SessionGeneration = "2"
			return value
		}(),
		func() runtimeprotocol.RuntimeSessionRef {
			value := opened.Session
			expires := protocolcommon.Rfc3339Time(host.now.Add(time.Hour).Format(time.RFC3339))
			value.ExpiresAt = &expires
			return value
		}(),
	} {
		if _, rejection := rt.CancelOperation(context.Background(), runtimeprotocol.CancelOperationInput{Session: tampered, OperationID: "operation_1", CancellationToken: "cancellation_token_0001"}); rejection == nil {
			t.Fatal("cancel accepted a tampered session")
		}
		if _, rejection := rt.GetOperationResult(context.Background(), runtimeprotocol.GetOperationResultInput{Session: tampered, LookupBy: "operation_id", OperationID: ptr(runtimeprotocol.OperationID("operation_1"))}); rejection == nil {
			t.Fatal("result lookup accepted a tampered session")
		}
		if _, rejection := rt.ListRevisions(context.Background(), runtimeprotocol.ListRevisionsInput{Session: tampered, MaxItems: "1", MaxOutputBytes: "100"}); rejection == nil {
			t.Fatal("history accepted a tampered session")
		}
		if _, rejection := rt.CommitOperations(context.Background(), commitFixture(tampered, host)); rejection == nil {
			t.Fatal("commit accepted a tampered session")
		}
	}
	input := commitFixture(opened.Session, host)
	operation := input.OperationBatch.Operations.Operations[0]
	for len(input.OperationBatch.Operations.Operations) <= 100 {
		input.OperationBatch.Operations.Operations = append(input.OperationBatch.Operations.Operations, operation)
	}
	result, rejection := rt.CommitOperations(context.Background(), input)
	if rejection != nil || result.OperationResult.Status != runtimeprotocol.OperationResultStatusRejected {
		t.Fatalf("limit result=%+v rejection=%v", result, rejection)
	}
	retry, retryRejection := rt.CommitOperations(context.Background(), input)
	if retryRejection != nil || !reflect.DeepEqual(retry, result) {
		t.Fatalf("limit retry changed durable rejection: retry=%+v rejection=%v", retry, retryRejection)
	}
	host.mu.Lock()
	publications := host.publishCalls
	host.mu.Unlock()
	if publications != 0 {
		t.Fatalf("limit rejection reached publication: %d", publications)
	}
	input = commitFixture(opened.Session, host)
	input.OperationID = "operation_2"
	input.IdempotencyKey = "idem_commit_000002"
	input.StateMutation = stateMutationFixture(opened.Session, "mutation_blob_oversize", "101")
	result, rejection = rt.CommitOperations(context.Background(), input)
	if rejection != nil || result.OperationResult.Status != runtimeprotocol.OperationResultStatusRejected || result.PreviewEvaluation != nil {
		t.Fatalf("blob limit result=%+v rejection=%v", result, rejection)
	}
	assertCommitResultEncodes(t, result)
	host.mu.Lock()
	if host.previewCalls != 0 || host.publishCalls != 0 {
		t.Fatalf("blob limit reached preview/publication: preview=%d publish=%d", host.previewCalls, host.publishCalls)
	}
	host.mu.Unlock()
}

func TestCoordinatorRejectsUntrustedStateMutationBindingsBeforePreview(t *testing.T) {
	tests := []struct {
		name string
		edit func(*runtimeprotocol.StateMutation, time.Time)
		code runtimeprotocol.RuntimeFailureCode
	}{
		{"state version", func(value *runtimeprotocol.StateMutation, _ time.Time) { value.ExpectedStateVersion = "3" }, runtimeprotocol.RuntimeFailureCodeRuntimeStaleRevision},
		{"scope", func(value *runtimeprotocol.StateMutation, _ time.Time) {
			value.MutationBlob.Scope.LocalScopeID = "attacker"
		}, runtimeprotocol.RuntimeFailureCodeRuntimeBlobScopeMismatch},
		{"generation", func(value *runtimeprotocol.StateMutation, _ time.Time) { value.MutationBlob.SessionGeneration = "2" }, runtimeprotocol.RuntimeFailureCodeRuntimeBlobScopeMismatch},
		{"expiry", func(value *runtimeprotocol.StateMutation, now time.Time) {
			expires := protocolcommon.Rfc3339Time(now.Add(-time.Second).Format(time.RFC3339))
			value.MutationBlob.ExpiresAt = &expires
		}, runtimeprotocol.RuntimeFailureCodeRuntimeBlobExpired},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			host, rt := newCoordinatorFixture(t)
			opened := openCoordinatorFixture(t, rt)
			input := commitFixture(opened.Session, host)
			input.StateMutation = stateMutationFixture(opened.Session, "mutation_blob_fixture", "2")
			tc.edit(input.StateMutation, host.now)
			result, rejection := rt.CommitOperations(context.Background(), input)
			if rejection != nil || result.OperationResult.Status != runtimeprotocol.OperationResultStatusRejected || result.OperationResult.FailureCode == nil || *result.OperationResult.FailureCode != tc.code || result.PreviewEvaluation != nil {
				t.Fatalf("result=%+v rejection=%v", result, rejection)
			}
			assertCommitResultEncodes(t, result)
			host.mu.Lock()
			defer host.mu.Unlock()
			if host.previewCalls != 0 || host.publishCalls != 0 {
				t.Fatalf("invalid state binding reached preview/publication: preview=%d publish=%d", host.previewCalls, host.publishCalls)
			}
		})
	}
}

func TestCoordinatorPublicBoundariesRejectMalformedInputsAndPortOutputs(t *testing.T) {
	t.Run("inputs", func(t *testing.T) {
		_, rt := newCoordinatorFixture(t)
		if _, rejection := rt.OpenDocument(context.Background(), runtimeprotocol.OpenRuntimeDocumentInput{}); rejection == nil {
			t.Fatal("malformed open input escaped")
		}
		if _, rejection := rt.CommitOperations(context.Background(), runtimeprotocol.RuntimeCommitInput{}); rejection == nil {
			t.Fatal("malformed commit input escaped")
		}
		if _, rejection := rt.CancelOperation(context.Background(), runtimeprotocol.CancelOperationInput{}); rejection == nil {
			t.Fatal("malformed cancel input escaped")
		}
		if _, rejection := rt.GetOperationResult(context.Background(), runtimeprotocol.GetOperationResultInput{}); rejection == nil {
			t.Fatal("malformed result input escaped")
		}
		if _, rejection := rt.ListRevisions(context.Background(), runtimeprotocol.ListRevisionsInput{}); rejection == nil {
			t.Fatal("malformed history input escaped")
		}
	})

	t.Run("identity", func(t *testing.T) {
		host, rt := newCoordinatorFixture(t)
		host.identityValue = "x"
		if _, rejection := rt.OpenDocument(context.Background(), runtimeprotocol.OpenRuntimeDocumentInput{DocumentID: "doc_fixture"}); rejection == nil {
			t.Fatal("malformed identity escaped")
		}
		coordinator := rt.config.Operations.OpenDocument.(*Coordinator)
		coordinator.mu.RLock()
		defer coordinator.mu.RUnlock()
		if len(coordinator.sessions) != 0 {
			t.Fatal("invalid open result installed a session")
		}
	})

	for _, tc := range []struct {
		name      string
		configure func(*coordinatorHost)
	}{
		{"revision snapshot", func(h *coordinatorHost) { h.invalidSnapshot = true }},
		{"working document", func(h *coordinatorHost) { h.invalidWorking = true }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			host, rt := newCoordinatorFixture(t)
			tc.configure(host)
			if _, rejection := rt.OpenDocument(context.Background(), runtimeprotocol.OpenRuntimeDocumentInput{DocumentID: "doc_fixture"}); rejection == nil {
				t.Fatal("invalid open port binding escaped")
			}
		})
	}

	t.Run("history and journal", func(t *testing.T) {
		host, rt := newCoordinatorFixture(t)
		opened := openCoordinatorFixture(t, rt)
		host.invalidHistory = true
		if _, rejection := rt.ListRevisions(context.Background(), runtimeprotocol.ListRevisionsInput{Session: opened.Session, MaxItems: "1", MaxOutputBytes: "100"}); rejection == nil {
			t.Fatal("malformed history page escaped")
		}
		host.invalidHistory = false
		input := commitFixture(opened.Session, host)
		if _, rejection := rt.CommitOperations(context.Background(), input); rejection != nil {
			t.Fatal(rejection)
		}
		host.mu.Lock()
		record := host.records[input.OperationID]
		record.Status.Phase = runtimeprotocol.RecoveryPhase("invalid")
		host.records[input.OperationID] = record
		host.mu.Unlock()
		if _, rejection := rt.GetOperationResult(context.Background(), runtimeprotocol.GetOperationResultInput{Session: opened.Session, LookupBy: "operation_id", OperationID: &input.OperationID}); rejection == nil {
			t.Fatal("malformed recovery status escaped")
		}
	})

	t.Run("staged revision", func(t *testing.T) {
		host, rt := newCoordinatorFixture(t)
		host.invalidStage = true
		opened := openCoordinatorFixture(t, rt)
		input := commitFixture(opened.Session, host)
		result, rejection := rt.CommitOperations(context.Background(), input)
		if rejection == nil || !reflect.DeepEqual(result, runtimeprotocol.RuntimeCommitResult{}) {
			t.Fatalf("malformed staged port output escaped: result=%+v rejection=%v", result, rejection)
		}
		retry, retryRejection := rt.CommitOperations(context.Background(), input)
		if retryRejection == nil || !reflect.DeepEqual(retry, runtimeprotocol.RuntimeCommitResult{}) {
			t.Fatalf("malformed staged output retry=%+v rejection=%v", retry, retryRejection)
		}
		host.mu.Lock()
		defer host.mu.Unlock()
		if host.publishCalls != 0 {
			t.Fatalf("invalid staged revision published: %d", host.publishCalls)
		}
	})

	t.Run("prepared revision", func(t *testing.T) {
		host, rt := newCoordinatorFixture(t)
		host.invalidPrepared = true
		opened := openCoordinatorFixture(t, rt)
		result, rejection := rt.CommitOperations(context.Background(), commitFixture(opened.Session, host))
		if rejection == nil || !reflect.DeepEqual(result, runtimeprotocol.RuntimeCommitResult{}) {
			t.Fatalf("invalid prepared revision escaped: result=%+v rejection=%v", result, rejection)
		}
		host.mu.Lock()
		defer host.mu.Unlock()
		if host.stageCalls != 0 || host.publishCalls != 0 {
			t.Fatalf("invalid prepared revision reached durable ports: stage=%d publish=%d", host.stageCalls, host.publishCalls)
		}
	})
}

func TestCoordinatorOpenPortFailuresAndRequestedRevisionFailClosed(t *testing.T) {
	t.Run("requested revision", func(t *testing.T) {
		host, rt := newCoordinatorFixture(t)
		requested := host.base.RevisionID
		if _, rejection := rt.OpenDocument(context.Background(), runtimeprotocol.OpenRuntimeDocumentInput{DocumentID: host.base.DocumentID, RequestedRevisionID: &requested}); rejection != nil {
			t.Fatal(rejection)
		}
	})

	for _, tc := range []struct {
		name      string
		configure func(*coordinatorHost)
	}{
		{"scope", func(h *coordinatorHost) { h.scopeErr = errors.New("injected") }},
		{"grant", func(h *coordinatorHost) { h.grantErr = errors.New("injected") }},
		{"head", func(h *coordinatorHost) { h.failHeadOnCall = 1 }},
		{"revision", func(h *coordinatorHost) { h.readRevisionErr = errors.New("injected") }},
		{"sources", func(h *coordinatorHost) { h.readSourcesErr = errors.New("injected") }},
		{"source binding", func(h *coordinatorHost) { h.invalidSources = true }},
		{"workbench", func(h *coordinatorHost) { h.openErr = errors.New("injected") }},
		{"state", func(h *coordinatorHost) { h.stateHeadErr = errors.New("injected") }},
		{"identity", func(h *coordinatorHost) { h.identityErr = errors.New("injected") }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			host, rt := newCoordinatorFixture(t)
			tc.configure(host)
			if _, rejection := rt.OpenDocument(context.Background(), runtimeprotocol.OpenRuntimeDocumentInput{DocumentID: host.base.DocumentID}); rejection == nil {
				t.Fatal("injected open port failure escaped")
			}
			coordinator := rt.config.Operations.OpenDocument.(*Coordinator)
			coordinator.mu.RLock()
			defer coordinator.mu.RUnlock()
			if len(coordinator.sessions) != 0 {
				t.Fatal("failed open installed a session")
			}
		})
	}
}

func TestCoordinatorValidatesCanonicalSourceClosure(t *testing.T) {
	extra := sourceBlobForContents("source-extra", protocolcommon.BlobLifetimePersistent, "text/ldl", []byte("entity extra {}\n"))
	for _, tc := range []struct {
		name   string
		mutate func(*port.SourceBlobSet)
	}{
		{"missing", func(value *port.SourceBlobSet) { value.Blobs = nil }},
		{"extra", func(value *port.SourceBlobSet) { value.Blobs = append(value.Blobs, extra) }},
		{"duplicate", func(value *port.SourceBlobSet) { value.Blobs = append(value.Blobs, value.Blobs[0]) }},
		{"ref mismatch", func(value *port.SourceBlobSet) { value.Blobs[0].Ref.MediaType = "text/plain" }},
		{"size mismatch", func(value *port.SourceBlobSet) { value.Blobs[0].Contents = append(value.Blobs[0].Contents, '!') }},
		{"digest mismatch", func(value *port.SourceBlobSet) { value.Blobs[0].Contents[0] ^= 1 }},
	} {
		t.Run("read_"+tc.name, func(t *testing.T) {
			host, rt := newCoordinatorFixture(t)
			host.mutateReadSources = tc.mutate
			if result, rejection := rt.OpenDocument(context.Background(), runtimeprotocol.OpenRuntimeDocumentInput{DocumentID: host.base.DocumentID}); rejection == nil || !reflect.DeepEqual(result, runtimeprotocol.OpenRuntimeDocumentResult{}) {
				t.Fatalf("malformed source read escaped: result=%+v rejection=%v", result, rejection)
			}
		})
	}

	for _, tc := range []struct {
		name   string
		mutate func(*port.SourceBlobSet)
	}{
		{"duplicate", func(value *port.SourceBlobSet) { value.Blobs = append(value.Blobs, value.Blobs[0]) }},
		{"ref codec", func(value *port.SourceBlobSet) { value.Blobs[0].Ref.Digest = "" }},
		{"size mismatch", func(value *port.SourceBlobSet) { value.Blobs[0].Contents = append(value.Blobs[0].Contents, '!') }},
		{"digest mismatch", func(value *port.SourceBlobSet) { value.Blobs[0].Contents[0] ^= 1 }},
	} {
		t.Run("prepared_"+tc.name, func(t *testing.T) {
			host, rt := newCoordinatorFixture(t)
			opened := openCoordinatorFixture(t, rt)
			host.mutatePreparedSources = tc.mutate
			result, rejection := rt.CommitOperations(context.Background(), commitFixture(opened.Session, host))
			if rejection == nil || !reflect.DeepEqual(result, runtimeprotocol.RuntimeCommitResult{}) {
				t.Fatalf("malformed prepared sources escaped: result=%+v rejection=%v", result, rejection)
			}
			host.mu.Lock()
			defer host.mu.Unlock()
			if host.stageCalls != 0 || host.publishCalls != 0 {
				t.Fatalf("malformed prepared sources reached publication: stage=%d publish=%d", host.stageCalls, host.publishCalls)
			}
		})
	}
}

func TestCoordinatorCommitJournalAndPostPublicationFailures(t *testing.T) {
	for _, tc := range []struct {
		name      string
		configure func(*coordinatorHost)
	}{
		{"operation journal read", func(h *coordinatorHost) { h.recoveryGetErrOnCall = 1 }},
		{"idempotency journal read", func(h *coordinatorHost) { h.recoveryGetErrOnCall = 2 }},
		{"current head read", func(h *coordinatorHost) { h.failHeadOnCall = 2 }},
		{"current grant read", func(h *coordinatorHost) { h.failGrantOnCall = 2 }},
		{"pending create", func(h *coordinatorHost) { h.createPendingErr = errors.New("injected") }},
		{"pending uniqueness race", func(h *coordinatorHost) { h.createPendingConflictRecord = true }},
		{"lease validation", func(h *coordinatorHost) { h.includeLease = true; h.leaseErr = errors.New("injected") }},
		{"stage write", func(h *coordinatorHost) { h.stageErr = errors.New("injected") }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			host, rt := newCoordinatorFixture(t)
			opened := openCoordinatorFixture(t, rt)
			tc.configure(host)
			if result, rejection := rt.CommitOperations(context.Background(), commitFixture(opened.Session, host)); rejection == nil || !reflect.DeepEqual(result, runtimeprotocol.RuntimeCommitResult{}) {
				t.Fatalf("transport failure became typed success: result=%+v rejection=%v", result, rejection)
			}
			host.mu.Lock()
			defer host.mu.Unlock()
			if host.publishCalls != 0 {
				t.Fatalf("prepublication failure published: %d", host.publishCalls)
			}
		})
	}

	for _, phase := range []runtimeprotocol.RecoveryPhase{
		runtimeprotocol.RecoveryPhasePublicationPending,
		runtimeprotocol.RecoveryPhasePublished,
		runtimeprotocol.RecoveryPhaseStatePending,
		runtimeprotocol.RecoveryPhaseAuditPending,
	} {
		t.Run("postpublication "+string(phase), func(t *testing.T) {
			host, rt := newCoordinatorFixture(t)
			host.advanceErrFrom = phase
			opened := openCoordinatorFixture(t, rt)
			result, rejection := rt.CommitOperations(context.Background(), commitFixture(opened.Session, host))
			if rejection != nil || result.OperationResult.Status != runtimeprotocol.OperationResultStatusCommittedStateStale {
				t.Fatalf("result=%+v rejection=%v", result, rejection)
			}
			assertCommitResultEncodes(t, result)
			host.mu.Lock()
			defer host.mu.Unlock()
			if host.successfulPublishes != 1 {
				t.Fatalf("postpublication recovery failure changed publication count: %d", host.successfulPublishes)
			}
		})
	}
}

func TestCoordinatorHoldsDelegationFenceFromFinalAuthorizationThroughPublication(t *testing.T) {
	t.Run("acquisition failure", func(t *testing.T) {
		host, rt := newCoordinatorFixture(t)
		opened := openCoordinatorFixture(t, rt)
		host.publicationFenceErr = errors.New("revoked delegation")
		result, rejection := rt.CommitOperations(context.Background(), commitFixture(opened.Session, host))
		if rejection != nil || result.OperationResult.Status != runtimeprotocol.OperationResultStatusRejected || host.publishCalls != 0 {
			t.Fatalf("result=%+v rejection=%v publications=%d", result, rejection, host.publishCalls)
		}
	})

	t.Run("revocation waits", func(t *testing.T) {
		host, rt := newCoordinatorFixture(t)
		opened := openCoordinatorFixture(t, rt)
		revokeAcquired := make(chan struct{})
		host.onPublish = func() {
			go func() {
				host.publicationMu.Lock()
				close(revokeAcquired)
				host.publicationMu.Unlock()
			}()
			select {
			case <-revokeAcquired:
				t.Fatal("revocation crossed the final authorization/publication fence")
			case <-time.After(30 * time.Millisecond):
			}
		}
		result, rejection := rt.CommitOperations(context.Background(), commitFixture(opened.Session, host))
		if rejection != nil || result.OperationResult.Status != runtimeprotocol.OperationResultStatusCommitted {
			t.Fatalf("result=%+v rejection=%v", result, rejection)
		}
		select {
		case <-revokeAcquired:
		case <-time.After(time.Second):
			t.Fatal("revocation did not resume after publication fence release")
		}
	})
}

func TestCoordinatorAuthorizationFailureAbandonsReservation(t *testing.T) {
	host, rt := newCoordinatorFixture(t)
	opened := openCoordinatorFixture(t, rt)
	input := commitFixture(opened.Session, host)
	input.AuthoringProof.DecisionDigest = digest('e')
	result, rejection := rt.CommitOperations(context.Background(), input)
	if rejection == nil || rejection.Code != runtimeprotocol.RuntimeFailureCodeRuntimeAuthorizationProofInvalid || !reflect.DeepEqual(result, runtimeprotocol.RuntimeCommitResult{}) {
		t.Fatalf("authorization failure result=%+v rejection=%v", result, rejection)
	}
	host.mu.Lock()
	defer host.mu.Unlock()
	if len(host.records) != 0 || len(host.keys) != 0 || host.publishCalls != 0 {
		t.Fatalf("authorization failure left reservation/publication: records=%d keys=%d publish=%d", len(host.records), len(host.keys), host.publishCalls)
	}

	host, rt = newCoordinatorFixture(t)
	opened = openCoordinatorFixture(t, rt)
	input = commitFixture(opened.Session, host)
	input.AuthoringProof.DecisionDigest = digest('e')
	host.abandonErr = errors.New("injected abandon failure")
	if result, rejection = rt.CommitOperations(context.Background(), input); rejection == nil || rejection.Code != runtimeprotocol.RuntimeFailureCodeRuntimeCapabilityUnavailable || !reflect.DeepEqual(result, runtimeprotocol.RuntimeCommitResult{}) {
		t.Fatalf("failed conditional abandon result=%+v rejection=%v", result, rejection)
	}
}

func TestCoordinatorSeparatesProposalProofFromApplyAuthority(t *testing.T) {
	for _, test := range []struct {
		name      string
		configure func(*coordinatorHost)
	}{
		{name: "apply denied", configure: func(host *coordinatorHost) { host.denyApply = true }},
		{name: "apply impact changed", configure: func(host *coordinatorHost) { host.mismatchApply = true }},
	} {
		t.Run(test.name, func(t *testing.T) {
			host, rt := newCoordinatorFixture(t)
			test.configure(host)
			opened := openCoordinatorFixture(t, rt)
			result, rejection := rt.CommitOperations(context.Background(), commitFixture(opened.Session, host))
			if rejection == nil || !reflect.DeepEqual(result, runtimeprotocol.RuntimeCommitResult{}) {
				t.Fatalf("result=%+v rejection=%v", result, rejection)
			}
			if host.publishCalls != 0 {
				t.Fatalf("apply rejection published %d times", host.publishCalls)
			}
		})
	}
}

func TestCoordinatorRetryRejectsUnboundEvaluationEvidence(t *testing.T) {
	host, rt := newCoordinatorFixture(t)
	opened := openCoordinatorFixture(t, rt)
	input := commitFixture(opened.Session, host)
	if _, rejection := rt.CommitOperations(context.Background(), input); rejection != nil {
		t.Fatal(rejection)
	}
	host.mu.Lock()
	record := host.records[input.OperationID]
	record.DecisionDigest = ptr(digest('0'))
	host.records[input.OperationID] = record
	host.mu.Unlock()
	if result, rejection := rt.CommitOperations(context.Background(), input); rejection == nil || !reflect.DeepEqual(result, runtimeprotocol.RuntimeCommitResult{}) {
		t.Fatalf("unbound journal evidence escaped: result=%+v rejection=%v", result, rejection)
	}
	oneSided := record
	oneSided.DecisionDigest = nil
	if validRecoveryEvaluation(oneSided) {
		t.Fatal("one-sided journal evaluation binding was accepted")
	}
	withoutPreview := record
	withoutPreview.CommitResult = &runtimeprotocol.RuntimeCommitResult{OperationResult: *record.Status.OperationResult}
	if validRecoveryEvaluation(withoutPreview) {
		t.Fatal("journal digests without preview evidence were accepted")
	}

	host, rt = newCoordinatorFixture(t)
	opened = openCoordinatorFixture(t, rt)
	host.invalidAdvanceEvaluation = true
	result, rejection := rt.CommitOperations(context.Background(), commitFixture(opened.Session, host))
	if rejection == nil || !reflect.DeepEqual(result, runtimeprotocol.RuntimeCommitResult{}) {
		t.Fatalf("malformed transition binding result=%+v rejection=%v", result, rejection)
	}
	host.mu.Lock()
	defer host.mu.Unlock()
	if host.publishCalls != 0 || host.abortCalls != 1 {
		t.Fatalf("malformed evaluation binding publish=%d abort=%d", host.publishCalls, host.abortCalls)
	}
}

func TestCoordinatorPublishedFinalizeFailureReturnsNoTypedResultUntilRecovered(t *testing.T) {
	host, rt := newCoordinatorFixture(t)
	opened := openCoordinatorFixture(t, rt)
	input := commitFixture(opened.Session, host)
	host.finalizeErr = errors.New("injected finalize failure")

	result, rejection := rt.CommitOperations(context.Background(), input)
	if rejection == nil || !reflect.DeepEqual(result, runtimeprotocol.RuntimeCommitResult{}) {
		t.Fatalf("undurable terminal escaped: result=%+v rejection=%v", result, rejection)
	}
	retry, retryRejection := rt.CommitOperations(context.Background(), input)
	if retryRejection == nil || !reflect.DeepEqual(retry, runtimeprotocol.RuntimeCommitResult{}) {
		t.Fatalf("pending retry claimed terminal result: result=%+v rejection=%v", retry, retryRejection)
	}

	host.mu.Lock()
	recovery := host.lastFinalizeInput
	host.finalizeErr = nil
	host.mu.Unlock()
	if _, err := host.Finalize(context.Background(), recovery); err != nil {
		t.Fatal(err)
	}
	recovered, recoveredRejection := rt.CommitOperations(context.Background(), input)
	if recoveredRejection != nil || !reflect.DeepEqual(recovered, recovery.CommitResult) {
		t.Fatalf("durable recovery result=%+v rejection=%v want=%+v", recovered, recoveredRejection, recovery.CommitResult)
	}
	assertCommitResultEncodes(t, recovered)
}

func TestCoordinatorLookupHistoryAndTerminalFinalizeFailures(t *testing.T) {
	t.Run("lookup and history port errors", func(t *testing.T) {
		host, rt := newCoordinatorFixture(t)
		opened := openCoordinatorFixture(t, rt)
		host.recoveryGetErrOnCall = 1
		if _, rejection := rt.GetOperationResult(context.Background(), runtimeprotocol.GetOperationResultInput{Session: opened.Session, LookupBy: "operation_id", OperationID: ptr(runtimeprotocol.OperationID("operation_1"))}); rejection == nil {
			t.Fatal("operation lookup port error escaped")
		}
		host.recoveryGetErrOnCall = 0
		host.recoveryGetCalls = 0
		host.listHistoryErr = errors.New("injected")
		if _, rejection := rt.ListRevisions(context.Background(), runtimeprotocol.ListRevisionsInput{Session: opened.Session, MaxItems: "1", MaxOutputBytes: "100"}); rejection == nil {
			t.Fatal("history port error escaped")
		}
		host.listHistoryErr = nil
		if _, rejection := rt.ListRevisions(context.Background(), runtimeprotocol.ListRevisionsInput{Session: opened.Session, MaxItems: "1", MaxOutputBytes: "100"}); rejection != nil {
			t.Fatal(rejection)
		}
	})

	for _, tc := range []struct {
		name      string
		configure func(*coordinatorHost)
	}{
		{"rejected", func(h *coordinatorHost) { h.stageErr = errors.New("injected"); h.finalizeErr = errors.New("injected") }},
		{"conflict", func(h *coordinatorHost) { h.publishErr = port.ErrConflict; h.finalizeErr = errors.New("injected") }},
		{"needs review", func(h *coordinatorHost) { h.publishErr = port.ErrIndeterminate; h.finalizeErr = errors.New("injected") }},
	} {
		t.Run("finalize "+tc.name, func(t *testing.T) {
			host, rt := newCoordinatorFixture(t)
			opened := openCoordinatorFixture(t, rt)
			tc.configure(host)
			if _, rejection := rt.CommitOperations(context.Background(), commitFixture(opened.Session, host)); rejection == nil {
				t.Fatal("terminal finalize failure escaped")
			}
		})
	}
}

type coordinatorHost struct {
	mu                                                                                      sync.Mutex
	publicationMu                                                                           sync.RWMutex
	now                                                                                     time.Time
	base                                                                                    runtimeprotocol.CommittedRevisionRef
	head                                                                                    port.DocumentHead
	working                                                                                 port.WorkingDocument
	source                                                                                  port.SourceBlob
	impact                                                                                  semantic.AuthoringImpact
	decision                                                                                accessprotocol.AuthoringDecision
	grant                                                                                   accessprotocol.AuthoringGrantSnapshot
	summary                                                                                 accessprotocol.AuthoringGrantSummary
	records                                                                                 map[runtimeprotocol.OperationID]port.RecoveryRecord
	keys                                                                                    map[runtimeprotocol.IdempotencyKey]runtimeprotocol.OperationID
	staged                                                                                  map[string]runtimeprotocol.CommittedRevisionRef
	history                                                                                 []runtimeprotocol.RevisionMetadata
	stageCalls, abortCalls, publishCalls, successfulPublishes                               int
	stagedInput                                                                             port.StageRevisionInput
	previewInput                                                                            port.PreviewWorkingDocumentInput
	previewErr, stageErr, publishErr, historyErr                                            error
	finalizeErr                                                                             error
	advanceErrFrom                                                                          runtimeprotocol.RecoveryPhase
	stateWriteErr                                                                           error
	leaseErr                                                                                error
	includeLease, includeStateMutation                                                      bool
	leaseCalls, failLeaseOnCall                                                             int
	publishDespiteError                                                                     bool
	getHeadCalls, failHeadOnCall                                                            int
	previewCalls                                                                            int
	stateWriteInput                                                                         port.WriteStateInput
	stateWrites                                                                             []port.WriteStateInput
	stateHead                                                                               port.StateHead
	afterPreview                                                                            func()
	onPublish                                                                               func()
	idCalls                                                                                 int
	identityValue                                                                           string
	invalidHistory, invalidStage                                                            bool
	invalidSnapshot, invalidWorking, invalidPrepared                                        bool
	scopeErr, grantErr, readRevisionErr, readSourcesErr, openErr, stateHeadErr, identityErr error
	invalidSources                                                                          bool
	grantCalls, failGrantOnCall, recoveryGetCalls, recoveryGetErrOnCall                     int
	createPendingErr, listHistoryErr                                                        error
	abandonErr                                                                              error
	createPendingConflictRecord                                                             bool
	createPendingDivergentRecords                                                           bool
	invalidAdvanceEvaluation                                                                bool
	lastFinalizeInput                                                                       port.FinalizeRecoveryRecordInput
	mutateCreateRecord, mutateAdvanceRecord, mutateFinalizeRecord                           func(*port.RecoveryRecord)
	mutateGetRecord                                                                         func(port.GetRecoveryRecordInput, *port.RecoveryRecord)
	mutateReadSources, mutatePreparedSources                                                func(*port.SourceBlobSet)
	createPendingArrived                                                                    chan struct{}
	createPendingRelease                                                                    <-chan struct{}
	closeCalls                                                                              int
	closeErr                                                                                error
	externalEnabled                                                                         bool
	externalMalformed                                                                       bool
	externalPrepareCalls, externalPublishCalls, externalAbortCalls                          int
	externalPublishErr                                                                      error
	afterExternalPrepare                                                                    func()
	externalStage                                                                           port.ExternalFileStage
	externalReceipt                                                                         port.ExternalFileReceipt
	advancePhases                                                                           []runtimeprotocol.RecoveryPhase
	denyApply, mismatchApply                                                                bool
	publicationFenceErr                                                                     error
}

func TestCoordinatorExternalMaterializationSuccessPendingAndFailure(t *testing.T) {
	for _, test := range []struct {
		name       string
		publishErr error
		status     runtimeprotocol.OperationResultStatus
		phase      runtimeprotocol.RecoveryPhase
		extState   runtimeprotocol.ExternalMaterializationState
	}{
		{name: "published", status: runtimeprotocol.OperationResultStatusCommitted, phase: runtimeprotocol.RecoveryPhaseFinal, extState: runtimeprotocol.ExternalMaterializationStatePublished},
		{name: "pending io", publishErr: errors.New("external unavailable"), status: runtimeprotocol.OperationResultStatusCommittedExternalPending, phase: runtimeprotocol.RecoveryPhaseExternalPending, extState: runtimeprotocol.ExternalMaterializationStatePending},
		{name: "failed conflict", publishErr: port.ErrConflict, status: runtimeprotocol.OperationResultStatusCommittedExternalFailed, phase: runtimeprotocol.RecoveryPhaseFinal, extState: runtimeprotocol.ExternalMaterializationStateFailed},
	} {
		t.Run(test.name, func(t *testing.T) {
			host, rt := newExternalCoordinatorFixture(t)
			host.externalPublishErr = test.publishErr
			opened := openCoordinatorFixture(t, rt)
			input := commitFixture(opened.Session, host)
			result, rejection := rt.CommitOperations(context.Background(), input)
			if rejection != nil {
				t.Fatal(rejection)
			}
			if result.OperationResult.Status != test.status || result.OperationResult.ExternalMaterialization == nil || result.OperationResult.ExternalMaterialization.State != test.extState {
				t.Fatalf("result=%+v", result)
			}
			record := host.records[input.OperationID]
			if record.Status.Phase != test.phase || host.externalPrepareCalls != 1 || host.externalPublishCalls != 1 {
				t.Fatalf("phase=%s prepare=%d publish=%d", record.Status.Phase, host.externalPrepareCalls, host.externalPublishCalls)
			}
			if test.phase == runtimeprotocol.RecoveryPhaseFinal {
				if record.CommitResult == nil || record.Status.OperationResult == nil {
					t.Fatalf("terminal record=%+v", record)
				}
				if len(host.history) != 1 || !slices.Contains(host.advancePhases, runtimeprotocol.RecoveryPhaseStatePending) || !slices.Contains(host.advancePhases, runtimeprotocol.RecoveryPhaseAuditPending) || !slices.Contains(host.advancePhases, runtimeprotocol.RecoveryPhaseOutboxReady) {
					t.Fatalf("post-publication duties history=%d phases=%v", len(host.history), host.advancePhases)
				}
			} else if record.CommitResult != nil || record.Status.ExternalMaterialization == nil {
				t.Fatalf("pending record=%+v", record)
			}
		})
	}
}

func TestCoordinatorExternalStageCleanupBeforeDocumentPublication(t *testing.T) {
	for _, test := range []struct {
		name      string
		configure func(*coordinatorHost, *Runtime, runtimeprotocol.RuntimeCommitInput)
	}{
		{name: "lease", configure: func(host *coordinatorHost, _ *Runtime, _ runtimeprotocol.RuntimeCommitInput) {
			host.includeLease = true
			host.failLeaseOnCall = 2
		}},
		{name: "cancel", configure: func(host *coordinatorHost, rt *Runtime, input runtimeprotocol.RuntimeCommitInput) {
			host.afterExternalPrepare = func() {
				_, _ = rt.CancelOperation(context.Background(), runtimeprotocol.CancelOperationInput{Session: input.Session, OperationID: input.OperationID, CancellationToken: *input.CancellationToken})
			}
		}},
		{name: "document publication conflict", configure: func(host *coordinatorHost, _ *Runtime, _ runtimeprotocol.RuntimeCommitInput) {
			host.publishErr = port.ErrConflict
		}},
	} {
		t.Run(test.name, func(t *testing.T) {
			host, rt := newExternalCoordinatorFixture(t)
			opened := openCoordinatorFixture(t, rt)
			input := commitFixture(opened.Session, host)
			if test.name == "cancel" {
				token := runtimeprotocol.CancellationToken("cancel_external_cleanup")
				input.CancellationToken = &token
			}
			test.configure(host, rt, input)
			if host.includeLease {
				input.LeaseToken = ptr(runtimeprotocol.LeaseToken("lease_token_0001"))
			}
			_, _ = rt.CommitOperations(context.Background(), input)
			if host.externalPrepareCalls != 1 || host.externalAbortCalls != 1 || host.abortCalls != 1 || host.successfulPublishes != 0 {
				t.Fatalf("prepare=%d external_abort=%d document_abort=%d published=%d", host.externalPrepareCalls, host.externalAbortCalls, host.abortCalls, host.successfulPublishes)
			}
		})
	}
}

func TestCoordinatorExternalProjectionFailsClosed(t *testing.T) {
	for _, test := range []struct {
		name      string
		configure func(*coordinatorHost)
	}{
		{name: "missing", configure: func(host *coordinatorHost) { host.externalEnabled = false }},
		{name: "malformed", configure: func(host *coordinatorHost) { host.externalMalformed = true }},
	} {
		t.Run(test.name, func(t *testing.T) {
			host, rt := newExternalCoordinatorFixture(t)
			test.configure(host)
			opened := openCoordinatorFixture(t, rt)
			result, rejection := rt.CommitOperations(context.Background(), commitFixture(opened.Session, host))
			if rejection == nil || rejection.Code != runtimeprotocol.RuntimeFailureCodeRuntimeCapabilityUnavailable || !reflect.DeepEqual(result, runtimeprotocol.RuntimeCommitResult{}) || host.stageCalls != 0 || host.externalPrepareCalls != 0 {
				t.Fatalf("result=%+v rejection=%v stage=%d external_prepare=%d", result, rejection, host.stageCalls, host.externalPrepareCalls)
			}
		})
	}
}

func TestCoordinatorExternalFailureWithHistoryFailureIsStateStale(t *testing.T) {
	host, rt := newExternalCoordinatorFixture(t)
	host.externalPublishErr = port.ErrConflict
	host.historyErr = errors.New("history unavailable")
	opened := openCoordinatorFixture(t, rt)
	result, rejection := rt.CommitOperations(context.Background(), commitFixture(opened.Session, host))
	if rejection != nil || result.OperationResult.Status != runtimeprotocol.OperationResultStatusCommittedStateStale || result.OperationResult.ExternalMaterialization == nil || result.OperationResult.ExternalMaterialization.State != runtimeprotocol.ExternalMaterializationStateFailed {
		t.Fatalf("result=%+v rejection=%v", result, rejection)
	}
}

func TestCoordinatorPostPublicationAdvanceFailureReturnsTruthfulPendingResult(t *testing.T) {
	for _, test := range []struct {
		name         string
		failFrom     runtimeprotocol.RecoveryPhase
		durablePhase runtimeprotocol.RecoveryPhase
	}{
		{name: "publication pending to published", failFrom: runtimeprotocol.RecoveryPhasePublicationPending, durablePhase: runtimeprotocol.RecoveryPhasePublicationPending},
		{name: "published to external pending", failFrom: runtimeprotocol.RecoveryPhasePublished, durablePhase: runtimeprotocol.RecoveryPhasePublished},
	} {
		t.Run(test.name, func(t *testing.T) {
			host, rt := newExternalCoordinatorFixture(t)
			host.advanceErrFrom = test.failFrom
			opened := openCoordinatorFixture(t, rt)
			input := commitFixture(opened.Session, host)
			result, rejection := rt.CommitOperations(context.Background(), input)
			record := host.records[input.OperationID]
			if rejection != nil || result.OperationResult.Status != runtimeprotocol.OperationResultStatusCommittedExternalPending || result.OperationResult.ExternalMaterialization == nil || result.OperationResult.ExternalMaterialization.State != runtimeprotocol.ExternalMaterializationStatePending || record.Status.Phase != test.durablePhase || record.Status.ExternalMaterialization != nil || record.ExternalStage == nil || host.externalPublishCalls != 0 {
				t.Fatalf("result=%+v rejection=%v record=%+v publish=%d", result, rejection, record, host.externalPublishCalls)
			}
		})
	}
}

func newExternalCoordinatorFixture(t *testing.T) (*coordinatorHost, *Runtime) {
	t.Helper()
	host, _ := newCoordinatorFixture(t)
	host.externalEnabled = true
	host.externalStage = port.ExternalFileStage{StageID: "external-stage", CandidateProviderVersion: "external-v2", MaterializationDigest: digest('e')}
	host.externalReceipt = port.ExternalFileReceipt{OperationID: "operation_1", IdempotencyKey: "idem_commit_000001", RevisionID: "rev_2", ProviderVersion: "external-v2", MaterializationDigest: digest('e'), ReceiptDigest: digest('d')}
	rt, err := New(Config{ReleaseVersion: "0.0.0-dev", EndpointInstanceID: "runtime-coordinator", ReleaseManifestDigest: digest('f'), Limits: testLimits("100"), Ports: Ports{Workbench: host, Grants: host, Scopes: host, Documents: host, State: coordinatorState{host}, External: coordinatorExternalHost{host}, History: host, Recovery: host, Authoring: host, Clock: host, Identities: host}})
	if err != nil {
		t.Fatal(err)
	}
	return host, rt
}

func TestCoordinatorCloseAndRecoveryResult(t *testing.T) {
	host, rt := newCoordinatorFixture(t)
	opened := openCoordinatorFixture(t, rt)
	coordinator := rt.config.Operations.OpenDocument.(*Coordinator)
	key := operationKey{documentID: opened.Session.Scope.DocumentID, sessionID: opened.Session.RuntimeSessionID, operation: "operation_closed_with_session"}
	coordinator.mu.Lock()
	coordinator.cancels[key] = cancelState{token: "cancel_closed_with_session"}
	coordinator.mu.Unlock()
	invalid := opened.Session
	invalid.SessionGeneration = "2"
	if rejection := rt.CloseDocument(context.Background(), invalid); rejection == nil {
		t.Fatal("invalid session close was accepted")
	}
	if rejection := rt.CloseDocument(context.Background(), opened.Session); rejection != nil || host.closeCalls != 1 {
		t.Fatalf("close rejection=%v calls=%d", rejection, host.closeCalls)
	}
	coordinator.mu.RLock()
	_, cancelRetained := coordinator.cancels[key]
	coordinator.mu.RUnlock()
	if cancelRetained {
		t.Fatal("close retained session cancellation state")
	}
	if rejection := rt.CloseDocument(context.Background(), opened.Session); rejection != nil || host.closeCalls != 1 {
		t.Fatalf("repeated close rejection=%v calls=%d", rejection, host.closeCalls)
	}

	host, rt = newCoordinatorFixture(t)
	opened = openCoordinatorFixture(t, rt)
	host.closeErr = errors.New("close unavailable")
	if rejection := rt.CloseDocument(context.Background(), opened.Session); rejection == nil {
		t.Fatal("workbench close failure was hidden")
	}
	host.closeErr = nil
	if rejection := rt.CloseDocument(context.Background(), opened.Session); rejection != nil || host.closeCalls != 2 {
		t.Fatalf("close retry rejection=%v calls=%d", rejection, host.closeCalls)
	}

	revision := host.base
	stateVersion := protocolcommon.CanonicalNonNegativeInt64("5")
	result := RecoveryCommitResult("operation_recovered", "idempotency_recovered", runtimeprotocol.OperationResultStatusCommittedStateStale, &revision, &stateVersion, runtimeprotocol.RuntimeFailureCodeRuntimeCapabilityUnavailable)
	if result.OperationResult.ResultDigest == "" || result.OperationResult.CommittedRevision == nil || len(result.OperationResult.Diagnostics) != 1 {
		t.Fatalf("recovery result=%+v", result)
	}
	external := externalPublishedResultStatus(port.ExternalFileReceipt{OperationID: "operation_recovered", IdempotencyKey: "idempotency_recovered", RevisionID: revision.RevisionID, ProviderVersion: "external-v2", MaterializationDigest: digest('e'), ReceiptDigest: digest('f')})
	withExternal := RecoveryCommitResultWithExternal("operation_recovered", "idempotency_recovered", runtimeprotocol.OperationResultStatusCommittedStateStale, &revision, &stateVersion, runtimeprotocol.RuntimeFailureCodeRuntimeCapabilityUnavailable, external)
	if withExternal.OperationResult.ExternalMaterialization == nil || withExternal.OperationResult.ExternalMaterialization.State != runtimeprotocol.ExternalMaterializationStatePublished || withExternal.OperationResult.ResultDigest == result.OperationResult.ResultDigest {
		t.Fatalf("external recovery result=%+v", withExternal)
	}
}

func TestRetryRequestDigestIgnoresAccessAndWorkingContext(t *testing.T) {
	host, rt := newCoordinatorFixture(t)
	opened := openCoordinatorFixture(t, rt)
	input := commitFixture(opened.Session, host)
	want := RetryRequestDigest(input)
	input.Session.Scope.AccessFingerprint = digest('0')
	input.Session.Scope.LocalScopeID = "renewed-local-scope"
	input.OperationBatch.Preconditions.DocumentGeneration.DocumentHandle.Value = "document_restarted_123456"
	input.OperationBatch.Preconditions.DocumentGeneration.Value = "99"
	if got := RetryRequestDigest(input); got != want {
		t.Fatalf("access or working context changed retry digest: got=%s want=%s", got, want)
	}
	input.OperationBatch.Operations = engineprotocol.SemanticOperationBatch{Operations: []engineprotocol.SemanticOperation{}}
	if got := RetryRequestDigest(input); got == want {
		t.Fatal("portable operation payload did not change retry digest")
	}
}

func newCoordinatorFixture(t *testing.T) (*coordinatorHost, *Runtime) {
	t.Helper()
	now := time.Date(2026, 7, 18, 12, 0, 0, 0, time.UTC)
	base := runtimeprotocol.CommittedRevisionRef{DocumentID: "doc_fixture", RevisionID: "rev_1", DefinitionHash: digest('2'), GraphHash: digest('7')}
	impact := semantic.AuthoringImpact{BaseDefinitionHash: base.DefinitionHash, Entries: []semantic.AuthoringImpactEntry{}, ImpactDigest: digest('8'), RequiredCapabilities: []semantic.AuthoringCapability{}, ResultingDefinitionHash: digest('9'), SemanticDiffHash: digest('a'), SourceDiffHash: digest('b')}
	grant := testGrant(now)
	grant.HostDocumentID = "doc_fixture"
	grant.LocalScopeID = "local_fixture"
	grant.MembershipVersion = "7"
	summary := accessprotocol.AuthoringGrantSummary{AccessFingerprint: grant.AccessFingerprint, GrantedCapabilities: grant.GrantedCapabilities, ConstrainedCapabilities: []semantic.AuthoringCapability{}, PolicyEtag: digest('6')}
	decision := accessprotocol.AuthoringDecision{AccessFingerprint: grant.AccessFingerprint, ApprovalRuleRefs: []string{}, AuthoringImpactDigest: &impact.ImpactDigest, ConstraintViolations: []accessprotocol.ConstraintViolation{}, DecisionDigest: digest('3'), Diagnostics: []protocolcommon.ProtocolDiagnostic{}, EvaluationDigest: digest('4'), HostOperationImpactDigests: []protocolcommon.Digest{}, MissingCapabilities: []semantic.AuthoringCapability{}, Outcome: accessprotocol.AuthoringDecisionOutcomeAllow, RequiredCapabilities: []semantic.AuthoringCapability{}}
	stateHead := port.StateHead{StateVersion: "4", BackendVersion: "state-4", DefinitionHash: base.DefinitionHash, GraphHash: base.GraphHash, CapturedAt: protocolcommon.Rfc3339Time(now.Format(time.RFC3339)), SubjectHashes: map[semantic.StableAddress]protocolcommon.Digest{}}
	source := sourceBlobForContents("source-main", protocolcommon.BlobLifetimePersistent, "text/ldl", []byte("entity fixture {}\n"))
	h := &coordinatorHost{now: now, base: base, head: port.DocumentHead{Revision: base, ProviderVersion: "provider-1", FencingToken: "1"}, stateHead: stateHead, working: port.WorkingDocument{Handle: "engine-handle", Generation: "0", BaseRevision: base, DefinitionHash: base.DefinitionHash, GraphHash: digest('7')}, source: source, impact: impact, grant: grant, summary: summary, decision: decision, records: map[runtimeprotocol.OperationID]port.RecoveryRecord{}, keys: map[runtimeprotocol.IdempotencyKey]runtimeprotocol.OperationID{}, staged: map[string]runtimeprotocol.CommittedRevisionRef{}}
	rt, err := New(Config{ReleaseVersion: "0.0.0-dev", EndpointInstanceID: "runtime-coordinator", ReleaseManifestDigest: digest('f'), Limits: testLimits("100"), Ports: Ports{Workbench: h, Registry: h, Grants: h, Scopes: h, Documents: h, State: coordinatorState{h}, History: h, Recovery: h, Authoring: h, Clock: h, Identities: h}})
	if err != nil {
		t.Fatal(err)
	}
	return h, rt
}

func openCoordinatorFixture(t *testing.T, rt *Runtime) runtimeprotocol.OpenRuntimeDocumentResult {
	t.Helper()
	result, rejection := rt.OpenDocument(context.Background(), runtimeprotocol.OpenRuntimeDocumentInput{DocumentID: "doc_fixture"})
	if rejection != nil {
		t.Fatal(rejection)
	}
	return result
}

func commitFixture(session runtimeprotocol.RuntimeSessionRef, h *coordinatorHost) runtimeprotocol.RuntimeCommitInput {
	operations := engineprotocol.SemanticOperationBatch{Operations: []engineprotocol.SemanticOperation{{NonCreateSemanticOperation: &engineprotocol.NonCreateSemanticOperation{Operation: engineprotocol.NonCreateSemanticOperationKindDeleteSubject, TargetAddress: ptr(semantic.StableAddress("ldl:project:fixture:entity:obsolete"))}}}}
	preconditions := engineprotocol.EngineEditPreconditions{DocumentGeneration: engineprotocol.DocumentGeneration{DocumentHandle: engineprotocol.DocumentHandle{EndpointInstanceID: "runtime-coordinator", Value: "document_fixture_12345678"}, Value: "0"}, ExpectedSubjectHashes: []engineprotocol.ExpectedHash{}, ExpectedSubtreeHashes: []engineprotocol.ExpectedHash{}, ExpectedChildSets: []engineprotocol.ExpectedChildSet{}, ExpectedSourceDigests: &[]engineprotocol.ExpectedSourceDigest{}}
	input := runtimeprotocol.RuntimeCommitInput{Session: session, OperationID: "operation_1", IdempotencyKey: "idem_commit_000001", OperationBatch: runtimeprotocol.RuntimeOperationBatch{DocumentID: h.base.DocumentID, BaseRevision: h.base, ExpectedDefinitionHash: h.base.DefinitionHash, Operations: operations, Preconditions: preconditions}, AuthoringProof: runtimeprotocol.AuthoringProof{AccessFingerprint: h.grant.AccessFingerprint, BaseRevision: h.base, DecisionDigest: h.decision.DecisionDigest, EvaluationDigest: h.decision.EvaluationDigest, MembershipVersion: h.grant.MembershipVersion, PolicyRefs: []accessprotocol.PolicyRef{}}, Trigger: runtimeprotocol.CommitTriggerExplicitSave}
	if h.includeLease {
		input.LeaseToken = ptr(runtimeprotocol.LeaseToken("lease_token_0001"))
	}
	if h.includeStateMutation {
		input.StateMutation = stateMutationFixture(session, "mutation_blob_fixture", "2")
	}
	return input
}

func commitAtCurrentHeadFixture(session runtimeprotocol.RuntimeSessionRef, h *coordinatorHost, operationID runtimeprotocol.OperationID, idempotencyKey runtimeprotocol.IdempotencyKey) runtimeprotocol.RuntimeCommitInput {
	input := commitFixture(session, h)
	h.mu.Lock()
	head := h.head.Revision
	h.mu.Unlock()
	input.OperationID = operationID
	input.IdempotencyKey = idempotencyKey
	input.OperationBatch.DocumentID = head.DocumentID
	input.OperationBatch.BaseRevision = head
	input.OperationBatch.ExpectedDefinitionHash = head.DefinitionHash
	input.AuthoringProof.BaseRevision = head
	return input
}

func stateMutationFixture(session runtimeprotocol.RuntimeSessionRef, blobID string, size protocolcommon.CanonicalUint64) *runtimeprotocol.StateMutation {
	return &runtimeprotocol.StateMutation{
		AffectedSubjects:     []semantic.StableAddress{"ldl:project:fixture:entity:obsolete"},
		ExpectedStateVersion: "4",
		MutationBlob: runtimeprotocol.RuntimeBlobRef{
			Blob:  protocolcommon.BlobRef{BlobID: blobID, Digest: digest('5'), Lifetime: protocolcommon.BlobLifetimeSession, MediaType: "application/octet-stream", Size: size},
			Scope: session.Scope, SessionGeneration: session.SessionGeneration,
		},
		MutationDigest: digest('6'),
	}
}

func ptr[T any](value T) *T { return &value }

func sourceBlobForContents(id string, lifetime protocolcommon.BlobLifetime, mediaType string, contents []byte) port.SourceBlob {
	sum := sha256.Sum256(contents)
	return port.SourceBlob{
		Ref: protocolcommon.BlobRef{
			BlobID: id, Digest: protocolcommon.Digest("sha256:" + hex.EncodeToString(sum[:])), Lifetime: lifetime,
			MediaType: mediaType, Size: protocolcommon.CanonicalUint64(strconv.Itoa(len(contents))),
		},
		Contents: append([]byte(nil), contents...),
	}
}

func cloneSourceBlobSetForTest(value port.SourceBlobSet) port.SourceBlobSet {
	clone := value
	clone.Blobs = append([]port.SourceBlob(nil), value.Blobs...)
	for index := range clone.Blobs {
		clone.Blobs[index].Contents = append([]byte(nil), value.Blobs[index].Contents...)
	}
	return clone
}

func cloneRecoveryRecordForTest(record port.RecoveryRecord) port.RecoveryRecord {
	clone := record
	if record.Status.ExternalMaterialization != nil {
		value := *record.Status.ExternalMaterialization
		clone.Status.ExternalMaterialization = &value
	}
	if record.Status.OperationResult != nil {
		operationResult := *record.Status.OperationResult
		clone.Status.OperationResult = &operationResult
	}
	if record.CommitResult != nil {
		commitResult := *record.CommitResult
		clone.CommitResult = &commitResult
	}
	if record.EvaluationDigest != nil {
		clone.EvaluationDigest = ptr(*record.EvaluationDigest)
	}
	if record.DecisionDigest != nil {
		clone.DecisionDigest = ptr(*record.DecisionDigest)
	}
	if record.ExternalStage != nil {
		value := *record.ExternalStage
		clone.ExternalStage = &value
	}
	if record.ExpectedExternalProviderVersion != nil {
		clone.ExpectedExternalProviderVersion = ptr(*record.ExpectedExternalProviderVersion)
	}
	if record.ExternalReceipt != nil {
		value := *record.ExternalReceipt
		clone.ExternalReceipt = &value
	}
	if record.ExternalFailure != nil {
		clone.ExternalFailure = ptr(*record.ExternalFailure)
	}
	return clone
}

func (h *coordinatorHost) ResolveScope(_ context.Context, document runtimeprotocol.DocumentID) (runtimeprotocol.RuntimeScope, error) {
	if h.scopeErr != nil {
		return runtimeprotocol.RuntimeScope{}, h.scopeErr
	}
	return runtimeprotocol.RuntimeScope{DocumentID: document, LocalScopeID: "local_fixture", AccessFingerprint: h.grant.AccessFingerprint}, nil
}
func (h *coordinatorHost) ResolveGrant(context.Context, runtimeprotocol.RuntimeScope) (accessprotocol.AuthoringGrantSnapshot, accessprotocol.AuthoringGrantSummary, error) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.grantCalls++
	if h.grantErr != nil || h.failGrantOnCall == h.grantCalls {
		if h.grantErr != nil {
			return accessprotocol.AuthoringGrantSnapshot{}, accessprotocol.AuthoringGrantSummary{}, h.grantErr
		}
		return accessprotocol.AuthoringGrantSnapshot{}, accessprotocol.AuthoringGrantSummary{}, errors.New("injected grant failure")
	}
	return h.grant, h.summary, nil
}

func (h *coordinatorHost) AcquireAuthoringPublication(context.Context, runtimeprotocol.RuntimeScope) (func(), error) {
	if h.publicationFenceErr != nil {
		return nil, h.publicationFenceErr
	}
	h.publicationMu.RLock()
	return h.publicationMu.RUnlock, nil
}

func (h *coordinatorHost) Now() time.Time { return h.now }
func (h *coordinatorHost) NewID(context.Context, port.IdentityKind) (string, error) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.idCalls++
	if h.identityErr != nil {
		return "", h.identityErr
	}
	if h.identityValue != "" {
		return h.identityValue, nil
	}
	return fmt.Sprintf("runtime_session_fixture_%d", h.idCalls), nil
}
func (h *coordinatorHost) Evaluate(_ context.Context, input accessprotocol.EvaluateAuthoringInput) (accessprotocol.AuthoringDecision, error) {
	decision := h.decision
	decision.AuthoringImpactDigest = &input.AuthoringImpact.ImpactDigest
	if input.RequestIntent == "apply" && h.denyApply {
		decision.Outcome = accessprotocol.AuthoringDecisionOutcomeDeny
		decision.Diagnostics = []protocolcommon.ProtocolDiagnostic{{Code: "authoring.agent_scope_denied", Message: "apply denied", Related: []protocolcommon.ProtocolDiagnosticRelated{}, Severity: protocolcommon.ProtocolDiagnosticSeverityError}}
	}
	if input.RequestIntent == "apply" && h.mismatchApply {
		decision.RequiredCapabilities = []semantic.AuthoringCapability{semantic.AuthoringCapabilityGraphWrite}
	}
	return decision, nil
}

func (h *coordinatorHost) Open(_ context.Context, input port.OpenWorkingDocumentInput) (port.WorkingDocument, error) {
	if h.openErr != nil {
		return port.WorkingDocument{}, h.openErr
	}
	result := h.working
	result.BaseRevision = input.Revision.Revision
	result.DefinitionHash = input.Revision.Revision.DefinitionHash
	result.GraphHash = input.Revision.Revision.GraphHash
	if h.invalidWorking {
		result.DefinitionHash = digest('0')
	}
	return result, nil
}

func (h *coordinatorHost) Close(context.Context, port.WorkingDocument) error {
	h.closeCalls++
	return h.closeErr
}
func (h *coordinatorHost) Preview(_ context.Context, input port.PreviewWorkingDocumentInput) (port.PreparedRevision, error) {
	h.mu.Lock()
	h.previewCalls++
	h.previewInput = input
	previewErr := h.previewErr
	afterPreview := h.afterPreview
	h.mu.Unlock()
	if afterPreview != nil {
		afterPreview()
	}
	if previewErr != nil {
		return port.PreparedRevision{}, previewErr
	}
	impact := h.impact
	impact.BaseDefinitionHash = input.Document.BaseRevision.DefinitionHash
	definitionHash := impact.ResultingDefinitionHash
	if h.invalidPrepared {
		definitionHash = "invalid"
	}
	sources := cloneSourceBlobSetForTest(port.SourceBlobSet{Revision: input.Document.BaseRevision, Blobs: []port.SourceBlob{h.source}})
	if h.mutatePreparedSources != nil {
		h.mutatePreparedSources(&sources)
	}
	result := port.PreparedRevision{AuthoringImpact: impact, DefinitionHash: definitionHash, GraphHash: digest('c'), Sources: sources, Manifest: protocolcommon.BlobRef{BlobID: "manifest", Digest: digest('d'), Lifetime: protocolcommon.BlobLifetimeRequest, MediaType: "application/json", Size: "2"}}
	if h.externalEnabled {
		result.External = &port.ExternalMaterialization{Kind: port.ExternalFileKindProject, ProjectFiles: []port.ExternalProjectFile{{Path: "document.ldl", Contents: []byte("project fixture {}\n")}}}
		if h.externalMalformed {
			result.External.ProjectFiles[0].Path = "../escaped.ldl"
		}
	}
	return result, nil
}

func (h *coordinatorHost) PrepareRegistryRevision(_ context.Context, input port.PrepareRegistryRevisionInput) (port.PreparedRevision, error) {
	if len(input.StagedObjects) == 0 {
		return port.PreparedRevision{}, errors.New("missing staged Registry objects")
	}
	impact := h.impact
	impact.BaseDefinitionHash = input.BaseRevision.DefinitionHash
	return port.PreparedRevision{
		AuthoringImpact: impact,
		DefinitionHash:  impact.ResultingDefinitionHash,
		GraphHash:       digest('c'),
		Sources:         cloneSourceBlobSetForTest(port.SourceBlobSet{Revision: input.BaseRevision, Blobs: []port.SourceBlob{h.source}}),
		Manifest:        protocolcommon.BlobRef{BlobID: "registry-manifest", Digest: digest('d'), Lifetime: protocolcommon.BlobLifetimeRequest, MediaType: "application/json", Size: "2"},
	}, nil
}

type coordinatorExternalHost struct{ host *coordinatorHost }

func (h coordinatorExternalHost) GetExternalHead(context.Context, port.GetExternalFileHeadInput) (port.ExternalFileHead, error) {
	return port.ExternalFileHead{ProviderVersion: "external-v1"}, nil
}
func (h coordinatorExternalHost) Prepare(_ context.Context, input port.PrepareExternalFileInput) (port.ExternalFileStage, error) {
	h.host.externalPrepareCalls++
	if h.host.afterExternalPrepare != nil {
		h.host.afterExternalPrepare()
	}
	stage := h.host.externalStage
	stage.CandidateProviderVersion = "external-v2"
	return stage, nil
}
func (h coordinatorExternalHost) Publish(_ context.Context, input port.PublishExternalFileInput) (port.ExternalFileReceipt, error) {
	h.host.externalPublishCalls++
	if h.host.externalPublishErr != nil {
		return port.ExternalFileReceipt{}, h.host.externalPublishErr
	}
	receipt := h.host.externalReceipt
	receipt.OperationID, receipt.IdempotencyKey = input.OperationID, input.IdempotencyKey
	return receipt, nil
}
func (h coordinatorExternalHost) Inspect(context.Context, port.InspectExternalFileInput) (port.ExternalFileInspection, error) {
	if h.host.externalReceipt.ReceiptDigest != "" {
		value := h.host.externalReceipt
		return port.ExternalFileInspection{Receipt: &value}, nil
	}
	value := h.host.externalStage
	return port.ExternalFileInspection{Stage: &value}, nil
}
func (h coordinatorExternalHost) Abort(context.Context, port.AbortExternalFileInput) error {
	h.host.externalAbortCalls++
	return nil
}
func (h *coordinatorHost) Checkpoint(_ context.Context, input port.CheckpointWorkingDocumentInput) (port.WorkingDocument, error) {
	result := input.Document
	result.BaseRevision = input.Revision
	result.DefinitionHash = input.Prepared.DefinitionHash
	result.GraphHash = input.Prepared.GraphHash
	result.Generation = "1"
	return result, nil
}

func (h *coordinatorHost) GetHead(context.Context, port.GetDocumentHeadInput) (port.DocumentHead, error) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.getHeadCalls++
	if h.failHeadOnCall == h.getHeadCalls {
		return port.DocumentHead{}, errors.New("injected head read")
	}
	return h.head, nil
}
func (h *coordinatorHost) ReadRevision(_ context.Context, input port.ReadRevisionInput) (port.RevisionSnapshot, error) {
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.readRevisionErr != nil {
		return port.RevisionSnapshot{}, h.readRevisionErr
	}
	if input.RevisionID != h.head.Revision.RevisionID {
		return port.RevisionSnapshot{}, port.ErrNotFound
	}
	revision := h.head.Revision
	if h.invalidSnapshot {
		revision.DocumentID = "doc_wrong"
	}
	return port.RevisionSnapshot{Revision: revision, SourceBlobs: []protocolcommon.BlobRef{h.source.Ref}, Manifest: protocolcommon.BlobRef{BlobID: "manifest", Digest: digest('d'), Lifetime: protocolcommon.BlobLifetimePersistent, MediaType: "application/json", Size: "2"}}, nil
}
func (h *coordinatorHost) ReadSourceBlobs(_ context.Context, input port.ReadSourceBlobsInput) (port.SourceBlobSet, error) {
	if h.readSourcesErr != nil {
		return port.SourceBlobSet{}, h.readSourcesErr
	}
	if h.invalidSources {
		input.Revision.RevisionID = "rev_wrong"
	}
	result := cloneSourceBlobSetForTest(port.SourceBlobSet{Revision: input.Revision, Blobs: []port.SourceBlob{h.source}})
	if h.mutateReadSources != nil {
		h.mutateReadSources(&result)
	}
	return result, nil
}
func (h *coordinatorHost) StageRevision(_ context.Context, input port.StageRevisionInput) (port.StagedRevision, error) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.stageCalls++
	h.stagedInput = input
	if h.stageErr != nil {
		return port.StagedRevision{}, h.stageErr
	}
	revision := runtimeprotocol.CommittedRevisionRef{DocumentID: input.Scope.DocumentID, RevisionID: runtimeprotocol.RevisionID("rev_" + string(input.OperationID)), DefinitionHash: input.DefinitionHash, GraphHash: input.GraphHash}
	if h.invalidStage {
		revision.GraphHash = ""
	}
	stageID := "stage-" + string(input.OperationID)
	h.staged[stageID] = revision
	return port.StagedRevision{StageID: stageID, Revision: revision, StagedDigest: digest('e')}, nil
}
func (h *coordinatorHost) PublishHead(_ context.Context, input port.PublishDocumentHeadInput) (port.PublishHeadResult, error) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.publishCalls++
	if h.onPublish != nil {
		h.onPublish()
	}
	if h.head.Revision.RevisionID != input.ExpectedRevision || h.head.Revision.DefinitionHash != input.ExpectedDefinitionHash {
		return port.PublishHeadResult{}, port.ErrConflict
	}
	if h.publishErr != nil && !h.publishDespiteError {
		return port.PublishHeadResult{}, h.publishErr
	}
	revision := h.staged[input.StageID]
	h.head.Revision = revision
	h.head.ProviderVersion = "provider-2"
	h.successfulPublishes++
	return port.PublishHeadResult{Published: true, Revision: revision, ProviderVersion: h.head.ProviderVersion}, h.publishErr
}

func (h *coordinatorHost) AbortStagedRevision(context.Context, port.AbortStagedRevisionInput) error {
	h.mu.Lock()
	h.abortCalls++
	h.mu.Unlock()
	return nil
}

func (h *coordinatorHost) CreatePending(_ context.Context, input port.CreatePendingRecordInput) (port.RecoveryRecord, error) {
	if h.createPendingArrived != nil {
		h.createPendingArrived <- struct{}{}
		<-h.createPendingRelease
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.createPendingErr != nil {
		return port.RecoveryRecord{}, h.createPendingErr
	}
	if h.createPendingConflictRecord {
		record := port.RecoveryRecord{Scope: input.Scope, Status: runtimeprotocol.RuntimeOperationStatus{Phase: runtimeprotocol.RecoveryPhasePending, OperationID: input.OperationID, IdempotencyKey: input.IdempotencyKey}, PayloadDigest: input.PayloadDigest, BaseRevision: input.BaseRevision}
		h.records[input.OperationID] = record
		h.keys[input.IdempotencyKey] = input.OperationID
		return port.RecoveryRecord{}, port.ErrConflict
	}
	if h.createPendingDivergentRecords {
		otherKey := runtimeprotocol.IdempotencyKey("idem_commit_other01")
		otherOperation := runtimeprotocol.OperationID("operation_other")
		byOperation := port.RecoveryRecord{Scope: input.Scope, Status: runtimeprotocol.RuntimeOperationStatus{Phase: runtimeprotocol.RecoveryPhasePending, OperationID: input.OperationID, IdempotencyKey: otherKey}, PayloadDigest: input.PayloadDigest, BaseRevision: input.BaseRevision}
		byKey := port.RecoveryRecord{Scope: input.Scope, Status: runtimeprotocol.RuntimeOperationStatus{Phase: runtimeprotocol.RecoveryPhasePending, OperationID: otherOperation, IdempotencyKey: input.IdempotencyKey}, PayloadDigest: input.PayloadDigest, BaseRevision: input.BaseRevision}
		h.records[input.OperationID] = byOperation
		h.records[otherOperation] = byKey
		h.keys[otherKey] = input.OperationID
		h.keys[input.IdempotencyKey] = otherOperation
		return port.RecoveryRecord{}, port.ErrConflict
	}
	if _, ok := h.records[input.OperationID]; ok {
		return port.RecoveryRecord{}, port.ErrConflict
	}
	if _, ok := h.keys[input.IdempotencyKey]; ok {
		return port.RecoveryRecord{}, port.ErrConflict
	}
	record := port.RecoveryRecord{Scope: input.Scope, Status: runtimeprotocol.RuntimeOperationStatus{Phase: runtimeprotocol.RecoveryPhasePending, OperationID: input.OperationID, IdempotencyKey: input.IdempotencyKey}, PayloadDigest: input.PayloadDigest, BaseRevision: input.BaseRevision}
	h.records[input.OperationID] = record
	h.keys[input.IdempotencyKey] = input.OperationID
	returned := record
	if h.mutateCreateRecord != nil {
		h.mutateCreateRecord(&returned)
	}
	return returned, nil
}
func (h *coordinatorHost) AbandonPending(_ context.Context, input port.AbandonPendingRecordInput) error {
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.abandonErr != nil {
		return h.abandonErr
	}
	record, ok := h.records[input.OperationID]
	if !ok || record.Status.Phase != runtimeprotocol.RecoveryPhasePending || record.Status.IdempotencyKey != input.IdempotencyKey || record.PayloadDigest != input.PayloadDigest {
		return port.ErrConflict
	}
	delete(h.records, input.OperationID)
	delete(h.keys, input.IdempotencyKey)
	return nil
}
func (h *coordinatorHost) Get(_ context.Context, input port.GetRecoveryRecordInput) (port.RecoveryRecord, error) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.recoveryGetCalls++
	if h.recoveryGetErrOnCall == h.recoveryGetCalls {
		return port.RecoveryRecord{}, errors.New("injected recovery read failure")
	}
	id := runtimeprotocol.OperationID("")
	if input.OperationID != nil {
		id = *input.OperationID
	} else if input.IdempotencyKey != nil {
		id = h.keys[*input.IdempotencyKey]
	}
	record, ok := h.records[id]
	if !ok {
		return port.RecoveryRecord{}, port.ErrNotFound
	}
	returned := cloneRecoveryRecordForTest(record)
	if h.mutateGetRecord != nil {
		h.mutateGetRecord(input, &returned)
	}
	return returned, nil
}
func (h *coordinatorHost) Advance(_ context.Context, input port.AdvanceRecoveryRecordInput) (port.RecoveryRecord, error) {
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.advanceErrFrom == input.ExpectedPhase {
		return port.RecoveryRecord{}, errors.New("injected advance failure")
	}
	record, ok := h.records[input.OperationID]
	if !ok {
		return port.RecoveryRecord{}, port.ErrNotFound
	}
	if record.Status.Phase != input.ExpectedPhase {
		return port.RecoveryRecord{}, port.ErrConflict
	}
	if input.ExpectedPhase == runtimeprotocol.RecoveryPhasePending && input.NextPhase == runtimeprotocol.RecoveryPhaseStaged {
		if input.EvaluationDigest == nil || input.DecisionDigest == nil {
			return port.RecoveryRecord{}, port.ErrConflict
		}
		record.EvaluationDigest = ptr(*input.EvaluationDigest)
		record.DecisionDigest = ptr(*input.DecisionDigest)
		if h.invalidAdvanceEvaluation {
			record.DecisionDigest = ptr(digest('0'))
		}
	}
	if input.ExternalStage != nil || input.ExpectedExternalProviderVersion != nil {
		if input.ExternalStage == nil || input.ExpectedExternalProviderVersion == nil {
			return port.RecoveryRecord{}, port.ErrConflict
		}
		stage, expected := *input.ExternalStage, *input.ExpectedExternalProviderVersion
		record.ExternalStage = &stage
		record.ExpectedExternalProviderVersion = &expected
	}
	if input.NextPhase == runtimeprotocol.RecoveryPhaseExternalPending {
		if record.ExternalStage == nil {
			return port.RecoveryRecord{}, port.ErrConflict
		}
		record.Status.ExternalMaterialization = externalPendingResultStatus(*record.ExternalStage)
	}
	if input.ExternalReceipt != nil {
		receipt := *input.ExternalReceipt
		record.ExternalReceipt = &receipt
		record.Status.ExternalMaterialization = externalPublishedResultStatus(receipt)
	}
	if input.ExternalFailure != nil {
		failure := *input.ExternalFailure
		record.ExternalFailure = &failure
		record.Status.ExternalMaterialization = externalFailedResultStatus(*record.ExternalStage, failure)
	}
	record.Status.Phase = input.NextPhase
	h.advancePhases = append(h.advancePhases, input.NextPhase)
	if input.NextPhase == runtimeprotocol.RecoveryPhaseRecovering {
		started := protocolcommon.Rfc3339Time(h.now.Format(time.RFC3339))
		record.Status.RecoveryStartedAt = &started
	}
	h.records[input.OperationID] = record
	returned := record
	if h.mutateAdvanceRecord != nil {
		h.mutateAdvanceRecord(&returned)
	}
	return returned, nil
}
func (h *coordinatorHost) Finalize(_ context.Context, input port.FinalizeRecoveryRecordInput) (port.RecoveryRecord, error) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.lastFinalizeInput = input
	if h.finalizeErr != nil {
		return port.RecoveryRecord{}, h.finalizeErr
	}
	record, ok := h.records[input.CommitResult.OperationResult.OperationID]
	if !ok {
		return port.RecoveryRecord{}, port.ErrNotFound
	}
	result := input.CommitResult.OperationResult
	if input.CommitResult.PreviewEvaluation != nil {
		record.EvaluationDigest = ptr(input.CommitResult.PreviewEvaluation.AuthoringDecision.EvaluationDigest)
		record.DecisionDigest = ptr(input.CommitResult.PreviewEvaluation.AuthoringDecision.DecisionDigest)
	}
	record.Status.OperationResult = &result
	record.CommitResult = &input.CommitResult
	record.Status.Phase = input.TerminalPhase
	record.Status.RecoveryStartedAt = nil
	h.records[result.OperationID] = record
	returned := record
	returnedResult := *record.CommitResult
	returned.CommitResult = &returnedResult
	returnedOperationResult := *record.Status.OperationResult
	returned.Status.OperationResult = &returnedOperationResult
	if h.mutateFinalizeRecord != nil {
		h.mutateFinalizeRecord(&returned)
	}
	return returned, nil
}

type coordinatorState struct{ *coordinatorHost }

func (h coordinatorState) GetHead(context.Context, port.GetStateHeadInput) (port.StateHead, error) {
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.stateHeadErr != nil {
		return port.StateHead{}, h.stateHeadErr
	}
	result := h.stateHead
	result.SubjectHashes = cloneSubjectHashes(result.SubjectHashes)
	return result, nil
}
func (h *coordinatorHost) ReadState(context.Context, port.ReadStateInput) (port.StateSnapshot, error) {
	return port.StateSnapshot{}, nil
}
func (h *coordinatorHost) WriteState(_ context.Context, input port.WriteStateInput) (port.StateWriteResult, error) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.stateWriteInput = input
	h.stateWrites = append(h.stateWrites, input)
	if h.stateWriteErr != nil {
		return port.StateWriteResult{}, h.stateWriteErr
	}
	if input.ExpectedStateVersion != h.stateHead.StateVersion || input.ExpectedBackendVersion != h.stateHead.BackendVersion || input.ExpectedDefinitionHash != h.stateHead.DefinitionHash || !reflect.DeepEqual(input.ExpectedSubjectHashes, h.stateHead.SubjectHashes) {
		return port.StateWriteResult{}, port.ErrConflict
	}
	version, _ := strconv.Atoi(string(h.stateHead.StateVersion))
	h.stateHead.StateVersion = protocolcommon.CanonicalNonNegativeInt64(strconv.Itoa(version + 1))
	h.stateHead.BackendVersion = runtimeprotocol.ProviderVersionToken("state-" + strconv.Itoa(version+1))
	h.stateHead.DefinitionHash = h.head.Revision.DefinitionHash
	h.stateHead.GraphHash = h.head.Revision.GraphHash
	h.stateHead.CapturedAt = protocolcommon.Rfc3339Time(h.now.Format(time.RFC3339))
	h.stateHead.SubjectHashes = map[semantic.StableAddress]protocolcommon.Digest{input.Mutation.AffectedSubjects[0]: input.Mutation.MutationDigest}
	result := h.stateHead
	result.SubjectHashes = cloneSubjectHashes(result.SubjectHashes)
	return port.StateWriteResult{Head: result}, nil
}
func (h *coordinatorHost) AcquireLease(context.Context, port.AcquireLeaseInput) (port.StateLease, error) {
	return port.StateLease{}, nil
}
func (h *coordinatorHost) RenewLease(context.Context, port.RenewLeaseInput) (port.StateLease, error) {
	return port.StateLease{}, nil
}
func (h *coordinatorHost) ReleaseLease(context.Context, port.ReleaseLeaseInput) error { return nil }
func (h *coordinatorHost) ValidateLease(_ context.Context, input port.ValidateLeaseInput) (port.StateLease, error) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.leaseCalls++
	if h.failLeaseOnCall == h.leaseCalls {
		return port.StateLease{}, port.ErrConflict
	}
	if h.leaseErr != nil {
		return port.StateLease{}, h.leaseErr
	}
	return port.StateLease{LeaseToken: input.LeaseToken, FencingToken: h.head.FencingToken, ExpiresAt: h.now.Add(time.Hour)}, nil
}
func (h *coordinatorHost) AppendAuditEvent(context.Context, port.AppendAuditEventInput) (port.AuditEventRef, error) {
	return port.AuditEventRef{}, nil
}
func (h *coordinatorHost) ListAuditEvents(context.Context, port.ListAuditEventsInput) (port.AuditEventPage, error) {
	return port.AuditEventPage{}, nil
}
func (h *coordinatorHost) ExportSnapshot(context.Context, port.ExportStateSnapshotInput) (port.StateSnapshot, error) {
	return port.StateSnapshot{}, nil
}

func (h *coordinatorHost) AppendRevision(_ context.Context, input port.AppendRevisionInput) (runtimeprotocol.RevisionMetadata, error) {
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.historyErr != nil {
		return runtimeprotocol.RevisionMetadata{}, h.historyErr
	}
	h.history = append(h.history, input.Metadata)
	return input.Metadata, nil
}
func (h *coordinatorHost) GetRevision(context.Context, port.GetRevisionMetadataInput) (runtimeprotocol.RevisionMetadata, error) {
	return runtimeprotocol.RevisionMetadata{}, port.ErrNotFound
}
func (h *coordinatorHost) ListRevisions(context.Context, port.ListRevisionsInput) (runtimeprotocol.RevisionPage, error) {
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.listHistoryErr != nil {
		return runtimeprotocol.RevisionPage{}, h.listHistoryErr
	}
	if h.invalidHistory {
		return runtimeprotocol.RevisionPage{Items: []runtimeprotocol.RevisionMetadata{{Revision: runtimeprotocol.CommittedRevisionRef{DocumentID: "doc_fixture", RevisionID: "rev_invalid"}}}, Page: protocolcommon.PageInfo{}}, nil
	}
	items := append([]runtimeprotocol.RevisionMetadata{}, h.history...)
	return runtimeprotocol.RevisionPage{Items: items, Page: protocolcommon.PageInfo{ReturnedBytes: "0", ReturnedItems: protocolcommon.CanonicalUint64(strconv.Itoa(len(items)))}}, nil
}
func (h *coordinatorHost) ResolveProviderVersion(context.Context, port.ResolveProviderVersionInput) (port.ProviderRevisionRef, error) {
	return port.ProviderRevisionRef{}, port.ErrNotFound
}

var _ port.Workbench = (*coordinatorHost)(nil)
var _ port.DocumentStore = (*coordinatorHost)(nil)
var _ port.RecoveryJournal = (*coordinatorHost)(nil)
var _ port.StateBackend = coordinatorState{}
var _ port.HistoryStore = (*coordinatorHost)(nil)

func assertCommitResultEncodes(t *testing.T, result runtimeprotocol.RuntimeCommitResult) {
	t.Helper()
	encoded, err := runtimeprotocol.EncodeRuntimeCommitResult(result)
	if err != nil {
		t.Fatalf("terminal result is wire-invalid: %v (%+v)", err, result)
	}
	decoded, err := runtimeprotocol.DecodeRuntimeCommitResult(encoded)
	if err != nil || !reflect.DeepEqual(decoded, result) {
		t.Fatalf("terminal result did not round trip: %v", err)
	}
	switch result.OperationResult.Status {
	case runtimeprotocol.OperationResultStatusCommittedStateStale:
		if !hasDiagnosticCode(result.OperationResult.Diagnostics, "LDL1902") {
			t.Fatal("committed_state_stale omitted LDL1902")
		}
	case runtimeprotocol.OperationResultStatusNeedsReview:
		if !hasDiagnosticCode(result.OperationResult.Diagnostics, "LDL1903") {
			t.Fatal("needs_review omitted LDL1903")
		}
	case runtimeprotocol.OperationResultStatusRejected:
		if len(result.OperationResult.Diagnostics) == 0 && result.OperationResult.ConflictEvidence == nil {
			t.Fatal("rejected omitted diagnostics and conflict evidence")
		}
	}
}

func hasDiagnosticCode(diagnostics []semantic.Diagnostic, code string) bool {
	for _, diagnostic := range diagnostics {
		if diagnostic.Code == code {
			return true
		}
	}
	return false
}
