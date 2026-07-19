// SPDX-License-Identifier: LicenseRef-LayerDraw-1.0
package local

import (
	"context"
	"encoding/json"
	"errors"
	"sort"
	"strconv"
	"sync"
	"testing"
	"time"

	"github.com/dencyuinc/layerdraw/gen/go/protocolcommon"
	"github.com/dencyuinc/layerdraw/gen/go/runtimeprotocol"
	"github.com/dencyuinc/layerdraw/gen/go/semantic"
	"github.com/dencyuinc/layerdraw/internal/runtime/port"
)

func rejectedCommitResult(op runtimeprotocol.OperationID, key runtimeprotocol.IdempotencyKey) runtimeprotocol.RuntimeCommitResult {
	code := runtimeprotocol.RuntimeFailureCodeRuntimeStaleRevision
	result := runtimeprotocol.OperationResult{
		OperationID:    op,
		IdempotencyKey: key,
		Status:         runtimeprotocol.OperationResultStatusRejected,
		FailureCode:    &code,
		Diagnostics:    []semantic.Diagnostic{},
	}
	result.ResultDigest = recoveryResultDigest(result)
	return runtimeprotocol.RuntimeCommitResult{OperationResult: result}
}

func TestFinalizeProspectiveRecordIsAlwaysReloadable(t *testing.T) {
	ctx := context.Background()
	scope := testScope()
	create := func(t *testing.T) (*Recovery, port.CreatePendingRecordInput) {
		j, err := NewRecoveryJournal(t.TempDir(), Options{})
		if err != nil {
			t.Fatal(err)
		}
		in := port.CreatePendingRecordInput{
			Scope:          scope,
			OperationID:    "finalize_op",
			IdempotencyKey: "finalize_idempotency_1",
			PayloadDigest:  testDigest('d'),
			BaseRevision: runtimeprotocol.CommittedRevisionRef{
				DocumentID:     scope.DocumentID,
				RevisionID:     "base",
				DefinitionHash: testDigest('b'),
				GraphHash:      testDigest('c'),
			},
		}
		if _, err := j.CreatePending(ctx, in); err != nil {
			t.Fatal(err)
		}
		return j, in
	}

	t.Run("evidence requires preview", func(t *testing.T) {
		j, in := create(t)
		evaluation, decision := testDigest('e'), testDigest('f')
		if _, err := j.Advance(ctx, port.AdvanceRecoveryRecordInput{
			Scope:            scope,
			OperationID:      in.OperationID,
			ExpectedPhase:    runtimeprotocol.RecoveryPhasePending,
			NextPhase:        runtimeprotocol.RecoveryPhaseStaged,
			EvaluationDigest: &evaluation,
			DecisionDigest:   &decision,
		}); err != nil {
			t.Fatal(err)
		}
		_, err := j.Finalize(ctx, port.FinalizeRecoveryRecordInput{
			Scope:         scope,
			TerminalPhase: runtimeprotocol.RecoveryPhaseFinal,
			CommitResult:  rejectedCommitResult(in.OperationID, in.IdempotencyKey),
		})
		if !errors.Is(err, port.ErrConflict) {
			t.Fatalf("finalize=%v", err)
		}
		record, err := j.Get(ctx, port.GetRecoveryRecordInput{Scope: scope, OperationID: &in.OperationID})
		if err != nil || record.Status.Phase != runtimeprotocol.RecoveryPhaseStaged {
			t.Fatalf("record=%+v err=%v", record, err)
		}
	})

	t.Run("pending rejection reloads", func(t *testing.T) {
		j, in := create(t)
		record, err := j.Finalize(ctx, port.FinalizeRecoveryRecordInput{
			Scope:         scope,
			TerminalPhase: runtimeprotocol.RecoveryPhaseFinal,
			CommitResult:  rejectedCommitResult(in.OperationID, in.IdempotencyKey),
		})
		if err != nil {
			t.Fatal(err)
		}
		if record.Status.Phase != runtimeprotocol.RecoveryPhaseFinal {
			t.Fatalf("record=%+v", record)
		}
		reopened, err := NewRecoveryJournal(j.root, Options{})
		if err != nil {
			t.Fatal(err)
		}
		loaded, err := reopened.Get(ctx, port.GetRecoveryRecordInput{Scope: scope, IdempotencyKey: &in.IdempotencyKey})
		if err != nil || loaded.CommitResult == nil {
			t.Fatalf("loaded=%+v err=%v", loaded, err)
		}
	})

	t.Run("committed result requires revision", func(t *testing.T) {
		j, in := create(t)
		result := runtimeprotocol.OperationResult{
			OperationID:    in.OperationID,
			IdempotencyKey: in.IdempotencyKey,
			Status:         runtimeprotocol.OperationResultStatusCommitted,
			Diagnostics:    []semantic.Diagnostic{},
		}
		result.ResultDigest = recoveryResultDigest(result)
		_, err := j.Finalize(ctx, port.FinalizeRecoveryRecordInput{
			Scope:         scope,
			TerminalPhase: runtimeprotocol.RecoveryPhaseFinal,
			CommitResult:  runtimeprotocol.RuntimeCommitResult{OperationResult: result},
		})
		if !errors.Is(err, port.ErrConflict) {
			t.Fatalf("finalize=%v", err)
		}
		record, err := j.Get(ctx, port.GetRecoveryRecordInput{Scope: scope, OperationID: &in.OperationID})
		if err != nil || record.Status.Phase != runtimeprotocol.RecoveryPhasePending {
			t.Fatalf("record=%+v err=%v", record, err)
		}
	})
}

func pendingRecordInput(scope runtimeprotocol.RuntimeScope) port.CreatePendingRecordInput {
	return port.CreatePendingRecordInput{
		Scope:          scope,
		OperationID:    "reloadable_op",
		IdempotencyKey: "reloadable_idempotency_1",
		PayloadDigest:  testDigest('d'),
		BaseRevision: runtimeprotocol.CommittedRevisionRef{
			DocumentID:     scope.DocumentID,
			RevisionID:     "base",
			DefinitionHash: testDigest('b'),
			GraphHash:      testDigest('c'),
		},
	}
}

func TestCreatePendingRejectsProtocolInvalidIDsAndReloadsSuccess(t *testing.T) {
	ctx := context.Background()
	scope := testScope()
	for _, tc := range []struct {
		name string
		edit func(*port.CreatePendingRecordInput)
	}{
		{name: "operation", edit: func(in *port.CreatePendingRecordInput) { in.OperationID = "-" }},
		{name: "short idempotency", edit: func(in *port.CreatePendingRecordInput) { in.IdempotencyKey = "short" }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			journal, err := NewRecoveryJournal(t.TempDir(), Options{})
			if err != nil {
				t.Fatal(err)
			}
			input := pendingRecordInput(scope)
			tc.edit(&input)
			if _, err := journal.CreatePending(ctx, input); !errors.Is(err, port.ErrConflict) {
				t.Fatalf("create=%v", err)
			}
			disk, err := journal.loadRecovery(scope)
			if err != nil || len(disk.Records) != 0 || len(disk.Operations) != 0 || len(disk.Idempotency) != 0 {
				t.Fatalf("disk=%+v err=%v", disk, err)
			}
		})
	}

	journal, err := NewRecoveryJournal(t.TempDir(), Options{})
	if err != nil {
		t.Fatal(err)
	}
	input := pendingRecordInput(scope)
	if _, err := journal.CreatePending(ctx, input); err != nil {
		t.Fatal(err)
	}
	reopened, err := NewRecoveryJournal(journal.root, Options{})
	if err != nil {
		t.Fatal(err)
	}
	got, err := reopened.Get(ctx, port.GetRecoveryRecordInput{Scope: scope, OperationID: &input.OperationID})
	if err != nil || got.Status.Phase != runtimeprotocol.RecoveryPhasePending {
		t.Fatalf("record=%+v err=%v", got, err)
	}
}

func TestAdvanceValidatesProspectivePublicationEvidence(t *testing.T) {
	ctx := context.Background()
	scope := testScope()
	journal, err := NewRecoveryJournal(t.TempDir(), Options{})
	if err != nil {
		t.Fatal(err)
	}
	input := pendingRecordInput(scope)
	if _, err := journal.CreatePending(ctx, input); err != nil {
		t.Fatal(err)
	}
	evaluation, decision := testDigest('e'), testDigest('f')
	if _, err := journal.Advance(ctx, port.AdvanceRecoveryRecordInput{
		Scope:            scope,
		OperationID:      input.OperationID,
		ExpectedPhase:    runtimeprotocol.RecoveryPhasePending,
		NextPhase:        runtimeprotocol.RecoveryPhaseStaged,
		EvaluationDigest: &evaluation,
		DecisionDigest:   &decision,
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := journal.Advance(ctx, port.AdvanceRecoveryRecordInput{
		Scope:         scope,
		OperationID:   input.OperationID,
		ExpectedPhase: runtimeprotocol.RecoveryPhaseStaged,
		NextPhase:     runtimeprotocol.RecoveryPhasePublicationPending,
	}); err != nil {
		t.Fatal(err)
	}
	valid := runtimeprotocol.CommittedRevisionRef{
		DocumentID:     scope.DocumentID,
		RevisionID:     "published",
		DefinitionHash: testDigest('1'),
		GraphHash:      testDigest('2'),
	}
	invalid := valid
	invalid.GraphHash = "not-a-digest"
	if _, err := journal.Advance(ctx, port.AdvanceRecoveryRecordInput{
		Scope:             scope,
		OperationID:       input.OperationID,
		ExpectedPhase:     runtimeprotocol.RecoveryPhasePublicationPending,
		NextPhase:         runtimeprotocol.RecoveryPhasePublished,
		PublishedRevision: &invalid,
	}); !errors.Is(err, port.ErrConflict) {
		t.Fatalf("invalid evidence=%v", err)
	}
	reopened, err := NewRecoveryJournal(journal.root, Options{})
	if err != nil {
		t.Fatal(err)
	}
	unchanged, err := reopened.Get(ctx, port.GetRecoveryRecordInput{Scope: scope, OperationID: &input.OperationID})
	if err != nil || unchanged.Status.Phase != runtimeprotocol.RecoveryPhasePublicationPending {
		t.Fatalf("record=%+v err=%v", unchanged, err)
	}
	advanced, err := reopened.Advance(ctx, port.AdvanceRecoveryRecordInput{
		Scope:             scope,
		OperationID:       input.OperationID,
		ExpectedPhase:     runtimeprotocol.RecoveryPhasePublicationPending,
		NextPhase:         runtimeprotocol.RecoveryPhasePublished,
		PublishedRevision: &valid,
	})
	if err != nil || advanced.Status.Phase != runtimeprotocol.RecoveryPhasePublished {
		t.Fatalf("record=%+v err=%v", advanced, err)
	}
	finalReopen, err := NewRecoveryJournal(journal.root, Options{})
	if err != nil {
		t.Fatal(err)
	}
	loaded, err := finalReopen.Get(ctx, port.GetRecoveryRecordInput{Scope: scope, OperationID: &input.OperationID})
	if err != nil || loaded.Status.Phase != runtimeprotocol.RecoveryPhasePublished {
		t.Fatalf("record=%+v err=%v", loaded, err)
	}
}

func stateRecordFixture(address semantic.StableAddress, hash protocolcommon.Digest) port.StateRecord {
	return port.StateRecord{
		SubjectAddress:     address,
		SubjectKind:        semantic.StateSubjectKindEntity,
		OwnSubjectHash:     hash,
		Fields:             map[string]semantic.RecipeScalar{},
		ProviderFields:     map[string]any{},
		RedactedFieldPaths: []semantic.StateFieldPath{},
	}
}

func TestStateRecordPersistenceMatchesRuntimeConsumption(t *testing.T) {
	ctx := context.Background()
	scope := testScope()
	address := semantic.StableAddress("ldl:project:fixture:entity:item")
	hash := testDigest('a')
	base, _ := stateFixture(time.Date(2026, 7, 19, 0, 0, 0, 0, time.UTC))
	tests := []struct {
		name string
		edit func(*port.StateSnapshot)
	}{
		{name: "nil fields", edit: func(s *port.StateSnapshot) {
			s.Head.SubjectHashes[address] = hash
			r := stateRecordFixture(address, hash)
			r.Fields = nil
			s.Records = []port.StateRecord{r}
		}},
		{name: "missing head subject", edit: func(s *port.StateSnapshot) {
			s.Records = []port.StateRecord{stateRecordFixture(address, hash)}
		}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			snapshot, _ := clone(base)
			tc.edit(&snapshot)
			state, err := NewStateBackend(t.TempDir(), Options{})
			if err != nil {
				t.Fatal(err)
			}
			if err := state.InitializeState(ctx, scope, snapshot); !errors.Is(err, port.ErrConflict) {
				t.Fatalf("bootstrap=%v", err)
			}
		})
	}

	valid, _ := clone(base)
	valid.Head.SubjectHashes[address] = hash
	valid.Records = []port.StateRecord{stateRecordFixture(address, hash)}
	state, err := NewStateBackend(t.TempDir(), Options{})
	if err != nil {
		t.Fatal(err)
	}
	if err := state.InitializeState(ctx, scope, valid); err != nil {
		t.Fatal(err)
	}
	path, _ := state.statePath(scope)
	var disk stateDisk
	if err := state.readJSON(path, &disk); err != nil {
		t.Fatal(err)
	}
	disk.Snapshot.Records[0].Fields = nil
	if err := state.writeJSON(path, disk); err != nil {
		t.Fatal(err)
	}
	if _, err := state.GetHead(ctx, port.GetStateHeadInput{Scope: scope}); !errors.Is(err, port.ErrIndeterminate) {
		t.Fatalf("corrupt load=%v", err)
	}
}

func TestWriteStateDirectIdentityAndPostWriteHashes(t *testing.T) {
	ctx := context.Background()
	scope := testScope()
	affected := semantic.StableAddress("ldl:project:fixture:entity:a")
	unaffected := semantic.StableAddress("ldl:project:fixture:entity:z")
	oldA, oldZ := testDigest('a'), testDigest('b')
	snapshot, mutation := stateFixture(time.Date(2026, 7, 19, 0, 0, 0, 0, time.UTC))
	snapshot.Head.SubjectHashes = map[semantic.StableAddress]protocolcommon.Digest{affected: oldA, unaffected: oldZ}
	snapshot.Records = []port.StateRecord{stateRecordFixture(affected, oldA), stateRecordFixture(unaffected, oldZ)}
	mutation.AffectedSubjects = []semantic.StableAddress{affected}
	newHash := mutation.MutationDigest
	state, err := NewStateBackend(t.TempDir(), Options{})
	if err != nil {
		t.Fatal(err)
	}
	if err := state.InitializeState(ctx, scope, snapshot); err != nil {
		t.Fatal(err)
	}
	lease, err := state.AcquireLease(ctx, port.AcquireLeaseInput{Scope: scope, OwnerID: "owner", TTL: time.Hour})
	if err != nil {
		t.Fatal(err)
	}
	valid := port.WriteStateInput{
		Scope:                  scope,
		OperationID:            "state_write",
		IdempotencyKey:         "state_write_key_1",
		ExpectedStateVersion:   "0",
		ExpectedBackendVersion: "1",
		ExpectedDefinitionHash: snapshot.Head.DefinitionHash,
		ExpectedSubjectHashes:  snapshot.Head.SubjectHashes,
		LeaseToken:             lease.LeaseToken,
		Mutation:               mutation,
	}
	tests := []struct {
		name string
		edit func(*port.WriteStateInput)
	}{
		{name: "operation", edit: func(v *port.WriteStateInput) { v.OperationID = "bad/op" }},
		{name: "idempotency", edit: func(v *port.WriteStateInput) { v.IdempotencyKey = "short" }},
		{name: "affected identity", edit: func(v *port.WriteStateInput) {
			v.Mutation.AffectedSubjects = []semantic.StableAddress{"bad"}
		}},
		{name: "affected duplicate", edit: func(v *port.WriteStateInput) {
			v.Mutation.AffectedSubjects = []semantic.StableAddress{affected, affected}
		}},
		{name: "affected order", edit: func(v *port.WriteStateInput) {
			v.Mutation.AffectedSubjects = []semantic.StableAddress{unaffected, affected}
		}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			candidate, _ := clone(valid)
			tc.edit(&candidate)
			if _, err := state.WriteState(ctx, candidate); !errors.Is(err, port.ErrConflict) {
				t.Fatalf("write=%v", err)
			}
		})
	}
	result, err := state.WriteState(ctx, valid)
	if err != nil {
		t.Fatal(err)
	}
	if result.Head.SubjectHashes[affected] != newHash || result.Head.SubjectHashes[unaffected] != oldZ {
		t.Fatalf("hashes=%v", result.Head.SubjectHashes)
	}
	exported, err := state.ExportSnapshot(ctx, port.ExportStateSnapshotInput{Scope: scope, StateVersion: "1"})
	if err != nil {
		t.Fatal(err)
	}
	if exported.Records[0].OwnSubjectHash != newHash || exported.Records[1].OwnSubjectHash != oldZ {
		t.Fatalf("records=%+v", exported.Records)
	}
}

func TestHistoryRejectsProviderPoisonAndCountsExactItemJSON(t *testing.T) {
	ctx := context.Background()
	scope := testScope()
	history, err := NewHistoryStore(t.TempDir(), Options{})
	if err != nil {
		t.Fatal(err)
	}
	first := historyMetadata("r1", "history_op_1", "2026-07-19T00:00:01Z")
	if _, err := history.AppendRevision(ctx, port.AppendRevisionInput{Scope: scope, Metadata: first}); err != nil {
		t.Fatal(err)
	}
	duplicate := historyMetadata("r2", "history_op_2", "2026-07-19T00:00:02Z")
	duplicate.Revision.ProviderVersion = first.Revision.ProviderVersion
	if _, err := history.AppendRevision(ctx, port.AppendRevisionInput{Scope: scope, Metadata: duplicate}); !errors.Is(err, port.ErrConflict) {
		t.Fatalf("provider duplicate=%v", err)
	}
	got, err := history.GetRevision(ctx, port.GetRevisionMetadataInput{Scope: scope, RevisionID: first.Revision.RevisionID})
	if err != nil || got.Revision.RevisionID != first.Revision.RevisionID {
		t.Fatalf("reload=%+v err=%v", got, err)
	}
	encoded, _ := json.Marshal([]runtimeprotocol.RevisionMetadata{first})
	exact := protocolcommon.CanonicalPositiveInt64(strconv.Itoa(len(encoded)))
	page, err := history.ListRevisions(ctx, port.ListRevisionsInput{Scope: scope, MaxItems: "1", MaxOutputBytes: exact})
	if err != nil {
		t.Fatal(err)
	}
	itemsJSON, _ := json.Marshal(page.Items)
	if len(itemsJSON) != len(encoded) || string(page.Page.ReturnedBytes) != strconv.Itoa(len(encoded)) {
		t.Fatalf("bytes items=%d returned=%s", len(itemsJSON), page.Page.ReturnedBytes)
	}
	tooSmall := protocolcommon.CanonicalPositiveInt64(strconv.Itoa(len(encoded) - 1))
	if _, err := history.ListRevisions(ctx, port.ListRevisionsInput{Scope: scope, MaxItems: "1", MaxOutputBytes: tooSmall}); !errors.Is(err, port.ErrConflict) {
		t.Fatalf("boundary=%v", err)
	}
}

func auditEventID(op runtimeprotocol.OperationID, digest protocolcommon.Digest) string {
	id, _ := safeID(strconv.Itoa(len(op)) + ":" + string(op) + ":" + string(digest))
	return "audit_" + id[:32]
}

func TestAuditCursorStableAcrossSameVersionConcurrentInsert(t *testing.T) {
	ctx := context.Background()
	scope := testScope()
	state, err := NewStateBackend(t.TempDir(), Options{})
	if err != nil {
		t.Fatal(err)
	}
	snapshot, _ := stateFixture(time.Date(2026, 7, 19, 0, 0, 0, 0, time.UTC))
	if err := state.InitializeState(ctx, scope, snapshot); err != nil {
		t.Fatal(err)
	}
	event := protocolcommon.BlobRef{
		BlobID:    "audit_blob_12345",
		Digest:    testDigest('9'),
		Lifetime:  protocolcommon.BlobLifetimeSession,
		MediaType: "application/json",
		Size:      "1",
	}
	ops := []runtimeprotocol.OperationID{"audit_op_000", "audit_op_001", "audit_op_002", "audit_op_003", "audit_op_004"}
	sort.Slice(ops, func(i, j int) bool {
		return auditEventID(ops[i], event.Digest) < auditEventID(ops[j], event.Digest)
	})
	appendEvent := func(op runtimeprotocol.OperationID) error {
		_, err := state.AppendAuditEvent(ctx, port.AppendAuditEventInput{
			Scope:                scope,
			OperationID:          op,
			ExpectedStateVersion: "0",
			EventDigest:          event.Digest,
			Event:                event,
		})
		return err
	}
	if err := appendEvent(ops[1]); err != nil {
		t.Fatal(err)
	}
	if err := appendEvent(ops[4]); err != nil {
		t.Fatal(err)
	}
	first, err := state.ListAuditEvents(ctx, port.ListAuditEventsInput{Scope: scope, MaxItems: "1"})
	if err != nil || first.Page.NextCursor == nil {
		t.Fatalf("first=%+v err=%v", first, err)
	}
	cursor := runtimeprotocol.RuntimeCursor(*first.Page.NextCursor)
	errCh := make(chan error, 1)
	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		errCh <- appendEvent(ops[0])
	}()
	wg.Wait()
	if err := <-errCh; err != nil {
		t.Fatal(err)
	}
	second, err := state.ListAuditEvents(ctx, port.ListAuditEventsInput{Scope: scope, Cursor: &cursor, MaxItems: "2"})
	if err != nil || len(second.Items) != 1 || second.Items[0].EventID != auditEventID(ops[4], event.Digest) {
		t.Fatalf("second=%+v err=%v", second, err)
	}
	if _, err := state.ListAuditEvents(ctx, port.ListAuditEventsInput{Scope: scope, MaxItems: "01"}); !errors.Is(err, port.ErrConflict) {
		t.Fatalf("noncanonical max=%v", err)
	}
}

func TestAppendAuditEventRejectsProtocolInvalidOperationWithoutMutation(t *testing.T) {
	ctx := context.Background()
	scope := testScope()
	state, err := NewStateBackend(t.TempDir(), Options{})
	if err != nil {
		t.Fatal(err)
	}
	snapshot, _ := stateFixture(time.Date(2026, 7, 19, 0, 0, 0, 0, time.UTC))
	if err := state.InitializeState(ctx, scope, snapshot); err != nil {
		t.Fatal(err)
	}
	event := protocolcommon.BlobRef{
		BlobID:    "audit_blob_12345",
		Digest:    testDigest('9'),
		Lifetime:  protocolcommon.BlobLifetimeSession,
		MediaType: "application/json",
		Size:      "1",
	}
	if _, err := state.AppendAuditEvent(ctx, port.AppendAuditEventInput{
		Scope:                scope,
		OperationID:          "-",
		ExpectedStateVersion: "0",
		EventDigest:          event.Digest,
		Event:                event,
	}); !errors.Is(err, port.ErrConflict) {
		t.Fatalf("append=%v", err)
	}
	page, err := state.ListAuditEvents(ctx, port.ListAuditEventsInput{Scope: scope, MaxItems: "10"})
	if err != nil || len(page.Items) != 0 {
		t.Fatalf("page=%+v err=%v", page, err)
	}
	if _, err := state.AppendAuditEvent(ctx, port.AppendAuditEventInput{
		Scope:                scope,
		OperationID:          "audit_valid",
		ExpectedStateVersion: "0",
		EventDigest:          event.Digest,
		Event:                event,
	}); err != nil {
		t.Fatal(err)
	}
	reopened, err := NewStateBackend(state.root, Options{})
	if err != nil {
		t.Fatal(err)
	}
	page, err = reopened.ListAuditEvents(ctx, port.ListAuditEventsInput{Scope: scope, MaxItems: "10"})
	if err != nil || len(page.Items) != 1 {
		t.Fatalf("page=%+v err=%v", page, err)
	}
}
