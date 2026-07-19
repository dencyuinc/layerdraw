// SPDX-License-Identifier: LicenseRef-LayerDraw-1.0
package local

import (
	"bytes"
	"context"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/dencyuinc/layerdraw/gen/go/accessprotocol"
	"github.com/dencyuinc/layerdraw/gen/go/protocolcommon"
	"github.com/dencyuinc/layerdraw/gen/go/runtimeprotocol"
	"github.com/dencyuinc/layerdraw/gen/go/semantic"
	"github.com/dencyuinc/layerdraw/internal/runtime/port"
)

func stagedDocumentFixture(t *testing.T, options Options) (*Document, runtimeprotocol.RuntimeScope, port.StageRevisionInput, port.StagedRevision) {
	t.Helper()
	ctx := context.Background()
	scope := testScope()
	store, err := NewDocumentStore(t.TempDir(), options)
	if err != nil {
		t.Fatal(err)
	}
	provider := runtimeprotocol.ProviderVersionToken("1")
	base := runtimeprotocol.CommittedRevisionRef{
		DocumentID:      scope.DocumentID,
		RevisionID:      "abort_base",
		DefinitionHash:  testDigest('b'),
		GraphHash:       testDigest('c'),
		ProviderVersion: &provider,
	}
	baseBlob := source("abort_base_blob", []byte("base"))
	manifest := protocolcommon.BlobRef{
		BlobID:    "abort_manifest",
		Digest:    testDigest('d'),
		Lifetime:  protocolcommon.BlobLifetimeSession,
		MediaType: "application/json",
		Size:      "1",
	}
	if err := store.InitializeDocument(ctx, scope, port.RevisionSnapshot{
		Revision:    base,
		SourceBlobs: []protocolcommon.BlobRef{baseBlob.Ref},
		Manifest:    manifest,
	}, provider, "1", []port.SourceBlob{baseBlob}); err != nil {
		t.Fatal(err)
	}
	next := source("abort_next_blob", []byte("next"))
	input := port.StageRevisionInput{
		Scope:            scope,
		OperationID:      "abort_stage_op",
		IdempotencyKey:   "abort_stage_idempotency",
		BaseRevision:     base,
		DefinitionHash:   testDigest('e'),
		GraphHash:        testDigest('f'),
		SourceBlobs:      port.SourceBlobSet{Revision: base, Blobs: []port.SourceBlob{next}},
		Manifest:         manifest,
		DecisionDigest:   testDigest('1'),
		EvaluationDigest: testDigest('2'),
	}
	staged, err := store.StageRevision(ctx, input)
	if err != nil {
		t.Fatal(err)
	}
	return store, scope, input, staged
}

func TestAbortStagedRevisionRemovalIdempotenceAndSafety(t *testing.T) {
	ctx := context.Background()
	store, scope, _, staged := stagedDocumentFixture(t, Options{})
	if err := store.AbortStagedRevision(ctx, port.AbortStagedRevisionInput{Scope: scope, StageID: "/unsafe"}); !errors.Is(err, port.ErrConflict) {
		t.Fatalf("unsafe stage id=%v", err)
	}
	if err := store.AbortStagedRevision(ctx, port.AbortStagedRevisionInput{Scope: scope, StageID: staged.StageID}); err != nil {
		t.Fatal(err)
	}
	if err := store.AbortStagedRevision(ctx, port.AbortStagedRevisionInput{Scope: scope, StageID: staged.StageID}); err != nil {
		t.Fatalf("missing abort=%v", err)
	}
	reopened, err := NewDocumentStore(store.root, Options{})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := reopened.PublishHead(ctx, port.PublishDocumentHeadInput{Scope: scope, StageID: staged.StageID}); !errors.Is(err, port.ErrNotFound) {
		t.Fatalf("aborted stage publish=%v", err)
	}

	stageID := "symlink_stage"
	id, _ := safeID(stageID)
	dir, _ := store.scopeDir(scope)
	stageParent := filepath.Join(dir, "documents", "staged")
	if err := store.ensureDir(stageParent); err != nil {
		t.Fatal(err)
	}
	outside := t.TempDir()
	if err := os.Symlink(outside, filepath.Join(stageParent, id)); err != nil {
		t.Fatal(err)
	}
	if err := store.AbortStagedRevision(ctx, port.AbortStagedRevisionInput{Scope: scope, StageID: stageID}); !errors.Is(err, port.ErrConflict) {
		t.Fatalf("symlink stage=%v", err)
	}
}

func TestAbortStagedRevisionPostRemovalSyncIsIndeterminate(t *testing.T) {
	armed := false
	store, scope, _, staged := stagedDocumentFixture(t, Options{Fault: func(operation, path string) error {
		if armed && operation == "dirsync" && strings.HasSuffix(path, filepath.Join("documents", "staged")) {
			return errors.New("sync failed")
		}
		return nil
	}})
	armed = true
	err := store.AbortStagedRevision(context.Background(), port.AbortStagedRevisionInput{Scope: scope, StageID: staged.StageID})
	if !errors.Is(err, port.ErrIndeterminate) {
		t.Fatalf("post-removal sync=%v", err)
	}
	armed = false
	if err := store.AbortStagedRevision(context.Background(), port.AbortStagedRevisionInput{Scope: scope, StageID: staged.StageID}); err != nil {
		t.Fatalf("visible removal=%v", err)
	}
}

func TestAbandonPendingChecksIdentityPhaseAndPersistsRemoval(t *testing.T) {
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
	wrongKey := input
	wrongKey.IdempotencyKey = "different_idempotency_key"
	if err := journal.AbandonPending(ctx, port.AbandonPendingRecordInput{
		Scope: scope, OperationID: input.OperationID, IdempotencyKey: wrongKey.IdempotencyKey, PayloadDigest: input.PayloadDigest,
	}); !errors.Is(err, port.ErrConflict) {
		t.Fatalf("wrong key=%v", err)
	}
	if err := journal.AbandonPending(ctx, port.AbandonPendingRecordInput{
		Scope: scope, OperationID: input.OperationID, IdempotencyKey: input.IdempotencyKey, PayloadDigest: testDigest('9'),
	}); !errors.Is(err, port.ErrConflict) {
		t.Fatalf("wrong digest=%v", err)
	}
	if err := journal.AbandonPending(ctx, port.AbandonPendingRecordInput{
		Scope: scope, OperationID: "missing_op", IdempotencyKey: input.IdempotencyKey, PayloadDigest: input.PayloadDigest,
	}); !errors.Is(err, port.ErrNotFound) {
		t.Fatalf("missing=%v", err)
	}
	if err := journal.AbandonPending(ctx, port.AbandonPendingRecordInput{
		Scope: scope, OperationID: input.OperationID, IdempotencyKey: input.IdempotencyKey, PayloadDigest: input.PayloadDigest,
	}); err != nil {
		t.Fatal(err)
	}
	reopened, err := NewRecoveryJournal(journal.root, Options{})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := reopened.Get(ctx, port.GetRecoveryRecordInput{Scope: scope, OperationID: &input.OperationID}); !errors.Is(err, port.ErrNotFound) {
		t.Fatalf("removed record=%v", err)
	}

	stagedInput := pendingRecordInput(scope)
	stagedInput.OperationID = "staged_abandon_op"
	stagedInput.IdempotencyKey = "staged_abandon_idempotency"
	if _, err := reopened.CreatePending(ctx, stagedInput); err != nil {
		t.Fatal(err)
	}
	evaluation, decision := testDigest('e'), testDigest('f')
	if _, err := reopened.Advance(ctx, port.AdvanceRecoveryRecordInput{
		Scope: scope, OperationID: stagedInput.OperationID, ExpectedPhase: runtimeprotocol.RecoveryPhasePending,
		NextPhase: runtimeprotocol.RecoveryPhaseStaged, EvaluationDigest: &evaluation, DecisionDigest: &decision,
	}); err != nil {
		t.Fatal(err)
	}
	if err := reopened.AbandonPending(ctx, port.AbandonPendingRecordInput{
		Scope: scope, OperationID: stagedInput.OperationID, IdempotencyKey: stagedInput.IdempotencyKey, PayloadDigest: stagedInput.PayloadDigest,
	}); !errors.Is(err, port.ErrConflict) {
		t.Fatalf("staged abandon=%v", err)
	}
	record, err := reopened.Get(ctx, port.GetRecoveryRecordInput{Scope: scope, OperationID: &stagedInput.OperationID})
	if err != nil || record.Status.Phase != runtimeprotocol.RecoveryPhaseStaged {
		t.Fatalf("record=%+v err=%v", record, err)
	}
}

func TestCreatePendingIdempotenceAndIdentityCollisionsPreserveOriginal(t *testing.T) {
	ctx := context.Background()
	scope := testScope()
	journal, err := NewRecoveryJournal(t.TempDir(), Options{})
	if err != nil {
		t.Fatal(err)
	}
	input := pendingRecordInput(scope)
	first, err := journal.CreatePending(ctx, input)
	if err != nil {
		t.Fatal(err)
	}
	again, err := journal.CreatePending(ctx, input)
	if err != nil || again.Status.OperationID != first.Status.OperationID {
		t.Fatalf("idempotent=%+v err=%v", again, err)
	}
	payloadConflict := input
	payloadConflict.PayloadDigest = testDigest('9')
	if _, err := journal.CreatePending(ctx, payloadConflict); !errors.Is(err, port.ErrConflict) {
		t.Fatalf("payload collision=%v", err)
	}
	keyConflict := input
	keyConflict.OperationID = "different_operation"
	if _, err := journal.CreatePending(ctx, keyConflict); !errors.Is(err, port.ErrConflict) {
		t.Fatalf("idempotency collision=%v", err)
	}
	reopened, err := NewRecoveryJournal(journal.root, Options{})
	if err != nil {
		t.Fatal(err)
	}
	loaded, err := reopened.Get(ctx, port.GetRecoveryRecordInput{Scope: scope, OperationID: &input.OperationID})
	if err != nil || loaded.PayloadDigest != input.PayloadDigest {
		t.Fatalf("loaded=%+v err=%v", loaded, err)
	}
}

func TestReadStateVersionRestartDefensiveCloneAndCorruption(t *testing.T) {
	ctx := context.Background()
	scope := testScope()
	now := time.Date(2026, 7, 19, 0, 0, 0, 0, time.UTC)
	snapshot, _ := stateFixture(now)
	address := semantic.StableAddress("ldl:project:fixture:entity:item")
	hash := testDigest('8')
	snapshot.Head.SubjectHashes[address] = hash
	snapshot.Records = []port.StateRecord{stateRecordFixture(address, hash)}
	state, err := NewStateBackend(t.TempDir(), Options{Now: func() time.Time { return now }})
	if err != nil {
		t.Fatal(err)
	}
	if err := state.InitializeState(ctx, scope, snapshot); err != nil {
		t.Fatal(err)
	}
	expected := protocolcommon.CanonicalNonNegativeInt64("0")
	got, err := state.ReadState(ctx, port.ReadStateInput{Scope: scope, ExpectedStateVersion: &expected})
	if err != nil {
		t.Fatal(err)
	}
	got.Head.SubjectHashes[address] = testDigest('7')
	got.Records[0].Fields["semantic.state.name"] = semantic.RecipeScalar{Kind: "string", StringValue: ptr("changed")}
	reopened, err := NewStateBackend(state.root, Options{Now: func() time.Time { return now }})
	if err != nil {
		t.Fatal(err)
	}
	again, err := reopened.ReadState(ctx, port.ReadStateInput{Scope: scope})
	if err != nil || again.Head.SubjectHashes[address] != hash || len(again.Records[0].Fields) != 0 {
		t.Fatalf("snapshot=%+v err=%v", again, err)
	}
	wrong := protocolcommon.CanonicalNonNegativeInt64("1")
	if _, err := reopened.ReadState(ctx, port.ReadStateInput{Scope: scope, ExpectedStateVersion: &wrong}); !errors.Is(err, port.ErrConflict) {
		t.Fatalf("version mismatch=%v", err)
	}
	cancelled, cancel := context.WithCancel(ctx)
	cancel()
	if _, err := reopened.ReadState(cancelled, port.ReadStateInput{Scope: scope}); !errors.Is(err, context.Canceled) {
		t.Fatalf("cancel=%v", err)
	}

	path, _ := reopened.statePath(scope)
	var disk stateDisk
	if err := reopened.readJSON(path, &disk); err != nil {
		t.Fatal(err)
	}
	disk.Snapshot.Head.StateVersion = "01"
	if err := reopened.writeJSON(path, disk); err != nil {
		t.Fatal(err)
	}
	if _, err := reopened.ReadState(ctx, port.ReadStateInput{Scope: scope}); !errors.Is(err, port.ErrIndeterminate) {
		t.Fatalf("corruption=%v", err)
	}
}

func ptr[T any](value T) *T { return &value }

func TestRenewAndReleaseLeaseLifecyclePersistsFencing(t *testing.T) {
	ctx := context.Background()
	scope := testScope()
	now := time.Date(2026, 7, 19, 0, 0, 0, 0, time.UTC)
	clock := func() time.Time { return now }
	state, err := NewStateBackend(t.TempDir(), Options{Now: clock})
	if err != nil {
		t.Fatal(err)
	}
	snapshot, _ := stateFixture(now)
	if err := state.InitializeState(ctx, scope, snapshot); err != nil {
		t.Fatal(err)
	}
	lease, err := state.AcquireLease(ctx, port.AcquireLeaseInput{Scope: scope, OwnerID: "owner", TTL: time.Minute})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := state.RenewLease(ctx, port.RenewLeaseInput{Scope: scope, LeaseToken: lease.LeaseToken, TTL: 0}); !errors.Is(err, port.ErrConflict) {
		t.Fatalf("zero ttl=%v", err)
	}
	if _, err := state.RenewLease(ctx, port.RenewLeaseInput{Scope: scope, LeaseToken: "lease_wrong_token_1234", TTL: time.Hour}); !errors.Is(err, port.ErrConflict) {
		t.Fatalf("wrong token=%v", err)
	}
	renewed, err := state.RenewLease(ctx, port.RenewLeaseInput{Scope: scope, LeaseToken: lease.LeaseToken, TTL: 2 * time.Hour})
	if err != nil || renewed.FencingToken != lease.FencingToken || renewed.ExpiresAt != now.Add(2*time.Hour) {
		t.Fatalf("renewed=%+v err=%v", renewed, err)
	}
	reopened, err := NewStateBackend(state.root, Options{Now: clock})
	if err != nil {
		t.Fatal(err)
	}
	if got, err := reopened.ValidateLease(ctx, port.ValidateLeaseInput{Scope: scope, LeaseToken: lease.LeaseToken}); err != nil || got.ExpiresAt != renewed.ExpiresAt {
		t.Fatalf("restart lease=%+v err=%v", got, err)
	}
	if err := reopened.ReleaseLease(ctx, port.ReleaseLeaseInput{Scope: scope, LeaseToken: "lease_wrong_token_1234"}); !errors.Is(err, port.ErrConflict) {
		t.Fatalf("wrong release=%v", err)
	}
	if err := reopened.ReleaseLease(ctx, port.ReleaseLeaseInput{Scope: scope, LeaseToken: lease.LeaseToken}); err != nil {
		t.Fatal(err)
	}
	final, err := NewStateBackend(state.root, Options{Now: clock})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := final.ValidateLease(ctx, port.ValidateLeaseInput{Scope: scope, LeaseToken: lease.LeaseToken}); !errors.Is(err, port.ErrConflict) {
		t.Fatalf("released lease=%v", err)
	}
	next, err := final.AcquireLease(ctx, port.AcquireLeaseInput{Scope: scope, OwnerID: "next", TTL: time.Minute})
	if err != nil || next.FencingToken == lease.FencingToken {
		t.Fatalf("next=%+v err=%v", next, err)
	}
	now = next.ExpiresAt
	if _, err := final.RenewLease(ctx, port.RenewLeaseInput{Scope: scope, LeaseToken: next.LeaseToken, TTL: time.Minute}); !errors.Is(err, port.ErrConflict) {
		t.Fatalf("expired renew=%v", err)
	}
}

func TestAssetLifecycleRejectsInvalidInputsAndPersistsDeletion(t *testing.T) {
	ctx := context.Background()
	scope := testScope()
	store, err := NewAssetStore(t.TempDir(), Options{MaxAssetBytes: 8})
	if err != nil {
		t.Fatal(err)
	}
	data := []byte("asset")
	digest := digestBytes(data)
	valid := port.PutAssetInput{Scope: scope, ExpectedDigest: digest, MediaType: "text/plain", Size: "5", Contents: bytes.NewReader(data)}
	for _, tc := range []struct {
		name string
		edit func(*port.PutAssetInput)
	}{
		{name: "digest", edit: func(in *port.PutAssetInput) { in.ExpectedDigest = "invalid" }},
		{name: "media type", edit: func(in *port.PutAssetInput) { in.MediaType = "" }},
		{name: "nil contents", edit: func(in *port.PutAssetInput) { in.Contents = nil }},
		{name: "noncanonical size", edit: func(in *port.PutAssetInput) { in.Size = "05" }},
		{name: "oversize", edit: func(in *port.PutAssetInput) { in.Size = "9" }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			input := valid
			input.Contents = bytes.NewReader(data)
			tc.edit(&input)
			if _, err := store.PutIfAbsent(ctx, input); !errors.Is(err, port.ErrConflict) {
				t.Fatalf("put=%v", err)
			}
		})
	}
	cancelled, cancel := context.WithCancel(ctx)
	cancel()
	if _, err := store.PutIfAbsent(cancelled, valid); !errors.Is(err, context.Canceled) {
		t.Fatalf("cancel=%v", err)
	}
	metadata, err := store.PutIfAbsent(ctx, valid)
	if err != nil || metadata.Digest != digest {
		t.Fatalf("metadata=%+v err=%v", metadata, err)
	}
	valid.Contents = bytes.NewReader(data)
	if _, err := store.PutIfAbsent(ctx, valid); err != nil {
		t.Fatalf("idempotent put=%v", err)
	}
	reader, err := store.Get(ctx, port.AssetRef{Scope: scope, Digest: digest})
	if err != nil {
		t.Fatal(err)
	}
	got, err := io.ReadAll(reader)
	_ = reader.Close()
	if err != nil || !bytes.Equal(got, data) {
		t.Fatalf("asset=%q err=%v", got, err)
	}
	ref := port.AssetRef{Scope: scope, Digest: digest}
	if err := store.DeleteIfUnreferenced(ctx, port.DeleteAssetInput{AssetRef: ref}); !errors.Is(err, port.ErrConflict) {
		t.Fatalf("referenced delete=%v", err)
	}
	if err := store.DeleteIfUnreferenced(ctx, port.DeleteAssetInput{AssetRef: port.AssetRef{Scope: scope, Digest: "invalid"}, ExpectedUnreferenced: true}); !errors.Is(err, port.ErrConflict) {
		t.Fatalf("invalid ref=%v", err)
	}
	if err := store.DeleteIfUnreferenced(ctx, port.DeleteAssetInput{AssetRef: ref, ExpectedUnreferenced: true}); err != nil {
		t.Fatal(err)
	}
	reopened, err := NewAssetStore(store.root, Options{})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := reopened.Stat(ctx, ref); !errors.Is(err, port.ErrNotFound) {
		t.Fatalf("deleted stat=%v", err)
	}
	if err := reopened.DeleteIfUnreferenced(ctx, port.DeleteAssetInput{AssetRef: ref, ExpectedUnreferenced: true}); !errors.Is(err, port.ErrNotFound) {
		t.Fatalf("missing delete=%v", err)
	}
}

func TestDocumentStagePublishBootstrapBranchesRemainConditional(t *testing.T) {
	ctx := context.Background()
	store, scope, input, staged := stagedDocumentFixture(t, Options{})
	again, err := store.StageRevision(ctx, input)
	if err != nil || again != staged {
		t.Fatalf("idempotent stage=%+v err=%v", again, err)
	}
	badInput := input
	badInput.OperationID = ""
	if _, err := store.StageRevision(ctx, badInput); !errors.Is(err, port.ErrConflict) {
		t.Fatalf("invalid operation=%v", err)
	}
	cancelled, cancel := context.WithCancel(ctx)
	cancel()
	if _, err := store.StageRevision(cancelled, input); !errors.Is(err, context.Canceled) {
		t.Fatalf("cancel stage=%v", err)
	}
	provider := runtimeprotocol.ProviderVersionToken("1")
	if _, err := store.PublishHead(ctx, port.PublishDocumentHeadInput{
		Scope: scope, StageID: staged.StageID, ExpectedRevision: input.BaseRevision.RevisionID,
		ExpectedDefinitionHash: input.BaseRevision.DefinitionHash, ExpectedProviderVersion: provider, FencingToken: "2",
	}); !errors.Is(err, port.ErrConflict) {
		t.Fatalf("wrong fencing=%v", err)
	}
	published, err := store.PublishHead(ctx, port.PublishDocumentHeadInput{
		Scope: scope, StageID: staged.StageID, ExpectedRevision: input.BaseRevision.RevisionID,
		ExpectedDefinitionHash: input.BaseRevision.DefinitionHash, ExpectedProviderVersion: provider, FencingToken: "1",
	})
	if err != nil || !published.Published || published.ProviderVersion != "2" {
		t.Fatalf("published=%+v err=%v", published, err)
	}
	if _, err := store.PublishHead(ctx, port.PublishDocumentHeadInput{
		Scope: scope, StageID: staged.StageID, ExpectedRevision: input.BaseRevision.RevisionID,
		ExpectedDefinitionHash: input.BaseRevision.DefinitionHash, ExpectedProviderVersion: provider, FencingToken: "1",
	}); !errors.Is(err, port.ErrConflict) {
		t.Fatalf("stale republish=%v", err)
	}
	head, err := store.GetHead(ctx, port.GetDocumentHeadInput{Scope: scope})
	if err != nil || head.Revision.RevisionID != published.Revision.RevisionID {
		t.Fatalf("head=%+v err=%v", head, err)
	}
	revision, err := store.ReadRevision(ctx, port.ReadRevisionInput{Scope: scope, RevisionID: published.Revision.RevisionID})
	if err != nil || revision.Revision.ProviderVersion == nil || *revision.Revision.ProviderVersion != "2" {
		t.Fatalf("revision=%+v err=%v", revision, err)
	}
	if err := store.InitializeDocument(ctx, scope, revision, "2", "1", nil); !errors.Is(err, port.ErrConflict) {
		t.Fatalf("second bootstrap=%v", err)
	}
}

func TestDocumentPublishRejectsCorruptPreexistingBlobAndBootstrapRetriesOrphans(t *testing.T) {
	ctx := context.Background()
	store, scope, input, staged := stagedDocumentFixture(t, Options{})
	dir, _ := store.scopeDir(scope)
	blob := input.SourceBlobs.Blobs[0]
	name, _ := safeID(string(blob.Ref.Digest))
	destination := filepath.Join(dir, "documents", "blobs", name)
	if err := store.atomicWrite(destination, bytes.NewReader([]byte("nope")), 4); err != nil {
		t.Fatal(err)
	}
	if _, err := store.PublishHead(ctx, port.PublishDocumentHeadInput{
		Scope: scope, StageID: staged.StageID, ExpectedRevision: input.BaseRevision.RevisionID,
		ExpectedDefinitionHash: input.BaseRevision.DefinitionHash, ExpectedProviderVersion: "1", FencingToken: "1",
	}); !errors.Is(err, port.ErrIndeterminate) {
		t.Fatalf("corrupt preexisting blob=%v", err)
	}
	if err := store.atomicWrite(destination, bytes.NewReader(blob.Contents), int64(len(blob.Contents))); err != nil {
		t.Fatal(err)
	}
	if _, err := store.PublishHead(ctx, port.PublishDocumentHeadInput{
		Scope: scope, StageID: staged.StageID, ExpectedRevision: input.BaseRevision.RevisionID,
		ExpectedDefinitionHash: input.BaseRevision.DefinitionHash, ExpectedProviderVersion: "1", FencingToken: "1",
	}); err != nil {
		t.Fatal(err)
	}

	bootstrap, err := NewDocumentStore(t.TempDir(), Options{})
	if err != nil {
		t.Fatal(err)
	}
	snapshot := port.RevisionSnapshot{Revision: input.BaseRevision, SourceBlobs: []protocolcommon.BlobRef{blob.Ref}, Manifest: input.Manifest}
	if err := bootstrap.InitializeDocument(ctx, scope, snapshot, "1", "1", nil); !errors.Is(err, port.ErrConflict) {
		t.Fatalf("missing blob=%v", err)
	}
	if _, err := bootstrap.GetHead(ctx, port.GetDocumentHeadInput{Scope: scope}); !errors.Is(err, port.ErrNotFound) {
		t.Fatalf("failed bootstrap visible=%v", err)
	}
	if err := bootstrap.InitializeDocument(ctx, scope, snapshot, "1", "1", []port.SourceBlob{blob, blob}); !errors.Is(err, port.ErrConflict) {
		t.Fatalf("duplicate blob=%v", err)
	}
	if err := bootstrap.InitializeDocument(ctx, scope, snapshot, "1", "1", []port.SourceBlob{blob}); err != nil {
		t.Fatalf("retry bootstrap=%v", err)
	}
}

func TestStateExportAndInitializeRejectUncommittedOrCorruptSnapshots(t *testing.T) {
	ctx := context.Background()
	scope := testScope()
	now := time.Date(2026, 7, 19, 0, 0, 0, 0, time.UTC)
	snapshot, _ := stateFixture(now)
	state, err := NewStateBackend(t.TempDir(), Options{Now: func() time.Time { return now }})
	if err != nil {
		t.Fatal(err)
	}
	if err := state.InitializeState(ctx, scope, snapshot); err != nil {
		t.Fatal(err)
	}
	if err := state.InitializeState(ctx, scope, snapshot); !errors.Is(err, port.ErrConflict) {
		t.Fatalf("second initialize=%v", err)
	}
	if _, err := state.ExportSnapshot(ctx, port.ExportStateSnapshotInput{Scope: scope, StateVersion: "01"}); !errors.Is(err, port.ErrConflict) {
		t.Fatalf("invalid version=%v", err)
	}
	if _, err := state.ExportSnapshot(ctx, port.ExportStateSnapshotInput{Scope: scope, StateVersion: "1"}); !errors.Is(err, port.ErrNotFound) {
		t.Fatalf("future version=%v", err)
	}
	exported, err := state.ExportSnapshot(ctx, port.ExportStateSnapshotInput{Scope: scope, StateVersion: "0"})
	if err != nil || exported.Head.StateVersion != "0" {
		t.Fatalf("exported=%+v err=%v", exported, err)
	}
	dir, _ := state.scopeDir(scope)
	id, _ := safeID("0")
	path := filepath.Join(dir, "state", "snapshots", id+".json")
	var corrupt port.StateSnapshot
	if err := state.readJSON(path, &corrupt); err != nil {
		t.Fatal(err)
	}
	corrupt.Head.StateVersion = "1"
	if err := state.writeJSON(path, corrupt); err != nil {
		t.Fatal(err)
	}
	if _, err := state.ExportSnapshot(ctx, port.ExportStateSnapshotInput{Scope: scope, StateVersion: "0"}); !errors.Is(err, port.ErrIndeterminate) {
		t.Fatalf("corrupt export=%v", err)
	}
	cancelled, cancel := context.WithCancel(ctx)
	cancel()
	if _, err := state.ExportSnapshot(cancelled, port.ExportStateSnapshotInput{Scope: scope, StateVersion: "0"}); !errors.Is(err, context.Canceled) {
		t.Fatalf("cancel export=%v", err)
	}
	other, err := NewStateBackend(t.TempDir(), Options{})
	if err != nil {
		t.Fatal(err)
	}
	bad := snapshot
	bad.Head.StateVersion = "01"
	bad.Records = append([]port.StateRecord(nil), snapshot.Records...)
	if err := other.InitializeState(ctx, scope, bad); !errors.Is(err, port.ErrConflict) {
		t.Fatalf("invalid initialize=%v", err)
	}
	if err := other.InitializeState(cancelled, scope, snapshot); !errors.Is(err, context.Canceled) {
		t.Fatalf("cancel initialize=%v", err)
	}
}

func TestReadStateRejectsSemanticallyCorruptPersistedVariants(t *testing.T) {
	ctx := context.Background()
	scope := testScope()
	now := time.Date(2026, 7, 19, 0, 0, 0, 0, time.UTC)
	address := semantic.StableAddress("ldl:project:fixture:entity:item")
	hash := testDigest('8')
	base, _ := stateFixture(now)
	base.Head.SubjectHashes[address] = hash
	base.Records = []port.StateRecord{stateRecordFixture(address, hash)}
	validPath := semantic.StateFieldPathSystemUpdatedByID
	validValue := semantic.RecipeScalar{Kind: "string", StringValue: ptr("actor")}
	tests := []struct {
		name string
		edit func(*stateDisk)
	}{
		{name: "backend version", edit: func(d *stateDisk) {
			d.Head.BackendVersion = ""
			d.Snapshot.Head.BackendVersion = ""
		}},
		{name: "captured time", edit: func(d *stateDisk) {
			d.Head.CapturedAt = "not-time"
			d.Snapshot.Head.CapturedAt = "not-time"
		}},
		{name: "subject digest", edit: func(d *stateDisk) {
			d.Head.SubjectHashes[address] = "bad"
			d.Snapshot.Head.SubjectHashes[address] = "bad"
		}},
		{name: "contents", edit: func(d *stateDisk) { d.Snapshot.Contents.Digest = "bad" }},
		{name: "duplicate inaccessible path", edit: func(d *stateDisk) {
			d.Snapshot.InaccessibleFieldPaths = []semantic.StateFieldPath{validPath, validPath}
		}},
		{name: "duplicate record", edit: func(d *stateDisk) {
			d.Snapshot.Records = append(d.Snapshot.Records, d.Snapshot.Records[0])
		}},
		{name: "record kind", edit: func(d *stateDisk) { d.Snapshot.Records[0].SubjectKind = "invalid" }},
		{name: "record hash", edit: func(d *stateDisk) { d.Snapshot.Records[0].OwnSubjectHash = "bad" }},
		{name: "record fields nil", edit: func(d *stateDisk) { d.Snapshot.Records[0].Fields = nil }},
		{name: "record absent from head", edit: func(d *stateDisk) {
			delete(d.Head.SubjectHashes, address)
			delete(d.Snapshot.Head.SubjectHashes, address)
		}},
		{name: "field path", edit: func(d *stateDisk) {
			d.Snapshot.Records[0].Fields["invalid"] = validValue
		}},
		{name: "field value", edit: func(d *stateDisk) {
			d.Snapshot.Records[0].Fields[string(validPath)] = semantic.RecipeScalar{Kind: "invalid"}
		}},
		{name: "duplicate redacted path", edit: func(d *stateDisk) {
			d.Snapshot.Records[0].RedactedFieldPaths = []semantic.StateFieldPath{validPath, validPath}
		}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			state, err := NewStateBackend(t.TempDir(), Options{Now: func() time.Time { return now }})
			if err != nil {
				t.Fatal(err)
			}
			if err := state.InitializeState(ctx, scope, base); err != nil {
				t.Fatal(err)
			}
			path, _ := state.statePath(scope)
			var disk stateDisk
			if err := state.readJSON(path, &disk); err != nil {
				t.Fatal(err)
			}
			tc.edit(&disk)
			if err := state.writeJSON(path, disk); err != nil {
				t.Fatal(err)
			}
			if _, err := state.ReadState(ctx, port.ReadStateInput{Scope: scope}); !errors.Is(err, port.ErrIndeterminate) {
				t.Fatalf("read=%v", err)
			}
		})
	}
}

func previewEvaluationFixture(scope runtimeprotocol.RuntimeScope) runtimeprotocol.PreviewEvaluation {
	impact := semantic.AuthoringImpact{
		BaseDefinitionHash:      testDigest('1'),
		Entries:                 []semantic.AuthoringImpactEntry{},
		ImpactDigest:            testDigest('2'),
		RequiredCapabilities:    []semantic.AuthoringCapability{},
		ResultingDefinitionHash: testDigest('3'),
		SemanticDiffHash:        testDigest('4'),
		SourceDiffHash:          testDigest('5'),
	}
	return runtimeprotocol.PreviewEvaluation{
		AuthoringImpact: impact,
		AuthoringDecision: accessprotocol.AuthoringDecision{
			AccessFingerprint:          scope.AccessFingerprint,
			ApprovalRuleRefs:           []string{},
			AuthoringImpactDigest:      &impact.ImpactDigest,
			ConstraintViolations:       []accessprotocol.ConstraintViolation{},
			DecisionDigest:             testDigest('6'),
			Diagnostics:                []protocolcommon.ProtocolDiagnostic{},
			EvaluationDigest:           testDigest('7'),
			HostOperationImpactDigests: []protocolcommon.Digest{},
			MissingCapabilities:        []semantic.AuthoringCapability{},
			Outcome:                    accessprotocol.AuthoringDecisionOutcomeAllow,
			RequiredCapabilities:       []semantic.AuthoringCapability{},
		},
	}
}

func terminalDiagnosticFixture() []semantic.Diagnostic {
	return []semantic.Diagnostic{{
		Code: "LDL1903", Severity: semantic.DiagnosticSeverityError, MessageKey: "operation_recovery_needs_review",
		ProtocolVersion: 1, Arguments: map[string]semantic.DiagnosticArgumentValue{}, Related: []semantic.DiagnosticRelated{},
	}}
}

func TestRecoveryFinalizeCommittedAndNeedsReviewFlowsReload(t *testing.T) {
	ctx := context.Background()
	scope := testScope()
	preview := previewEvaluationFixture(scope)
	advanceToPublication := func(t *testing.T, journal *Recovery, input port.CreatePendingRecordInput) {
		t.Helper()
		if _, err := journal.CreatePending(ctx, input); err != nil {
			t.Fatal(err)
		}
		evaluation := preview.AuthoringDecision.EvaluationDigest
		decision := preview.AuthoringDecision.DecisionDigest
		if _, err := journal.Advance(ctx, port.AdvanceRecoveryRecordInput{
			Scope: scope, OperationID: input.OperationID, ExpectedPhase: runtimeprotocol.RecoveryPhasePending,
			NextPhase: runtimeprotocol.RecoveryPhaseStaged, EvaluationDigest: &evaluation, DecisionDigest: &decision,
		}); err != nil {
			t.Fatal(err)
		}
		if _, err := journal.Advance(ctx, port.AdvanceRecoveryRecordInput{
			Scope: scope, OperationID: input.OperationID, ExpectedPhase: runtimeprotocol.RecoveryPhaseStaged,
			NextPhase: runtimeprotocol.RecoveryPhasePublicationPending,
		}); err != nil {
			t.Fatal(err)
		}
	}

	t.Run("recovering needs review", func(t *testing.T) {
		journal, err := NewRecoveryJournal(t.TempDir(), Options{})
		if err != nil {
			t.Fatal(err)
		}
		input := pendingRecordInput(scope)
		input.OperationID = "needs_review_op"
		input.IdempotencyKey = "needs_review_idempotency"
		advanceToPublication(t, journal, input)
		if _, err := journal.Advance(ctx, port.AdvanceRecoveryRecordInput{
			Scope: scope, OperationID: input.OperationID, ExpectedPhase: runtimeprotocol.RecoveryPhasePublicationPending,
			NextPhase: runtimeprotocol.RecoveryPhaseRecovering,
		}); err != nil {
			t.Fatal(err)
		}
		result := runtimeprotocol.OperationResult{
			OperationID: input.OperationID, IdempotencyKey: input.IdempotencyKey,
			Status: runtimeprotocol.OperationResultStatusNeedsReview, Diagnostics: terminalDiagnosticFixture(),
		}
		result.ResultDigest = recoveryResultDigest(result)
		final, err := journal.Finalize(ctx, port.FinalizeRecoveryRecordInput{
			Scope: scope, TerminalPhase: runtimeprotocol.RecoveryPhaseNeedsReview,
			CommitResult: runtimeprotocol.RuntimeCommitResult{OperationResult: result, PreviewEvaluation: &preview},
		})
		if err != nil || final.Status.Phase != runtimeprotocol.RecoveryPhaseNeedsReview {
			t.Fatalf("final=%+v err=%v", final, err)
		}
		reopened, err := NewRecoveryJournal(journal.root, Options{})
		if err != nil {
			t.Fatal(err)
		}
		loaded, err := reopened.Get(ctx, port.GetRecoveryRecordInput{Scope: scope, OperationID: &input.OperationID})
		if err != nil || loaded.CommitResult == nil || loaded.CommitResult.OperationResult.Status != runtimeprotocol.OperationResultStatusNeedsReview {
			t.Fatalf("loaded=%+v err=%v", loaded, err)
		}
	})

	t.Run("outbox committed", func(t *testing.T) {
		journal, err := NewRecoveryJournal(t.TempDir(), Options{})
		if err != nil {
			t.Fatal(err)
		}
		input := pendingRecordInput(scope)
		input.OperationID = "committed_finalize_op"
		input.IdempotencyKey = "committed_finalize_idempotency"
		advanceToPublication(t, journal, input)
		published := runtimeprotocol.CommittedRevisionRef{
			DocumentID: scope.DocumentID, RevisionID: "committed_revision",
			DefinitionHash: testDigest('8'), GraphHash: testDigest('9'),
		}
		transitions := []struct {
			from runtimeprotocol.RecoveryPhase
			to   runtimeprotocol.RecoveryPhase
			pub  *runtimeprotocol.CommittedRevisionRef
		}{
			{runtimeprotocol.RecoveryPhasePublicationPending, runtimeprotocol.RecoveryPhasePublished, &published},
			{runtimeprotocol.RecoveryPhasePublished, runtimeprotocol.RecoveryPhaseStatePending, nil},
			{runtimeprotocol.RecoveryPhaseStatePending, runtimeprotocol.RecoveryPhaseAuditPending, nil},
			{runtimeprotocol.RecoveryPhaseAuditPending, runtimeprotocol.RecoveryPhaseOutboxReady, nil},
		}
		for _, transition := range transitions {
			if _, err := journal.Advance(ctx, port.AdvanceRecoveryRecordInput{
				Scope: scope, OperationID: input.OperationID, ExpectedPhase: transition.from,
				NextPhase: transition.to, PublishedRevision: transition.pub,
			}); err != nil {
				t.Fatalf("%s -> %s: %v", transition.from, transition.to, err)
			}
		}
		result := runtimeprotocol.OperationResult{
			OperationID: input.OperationID, IdempotencyKey: input.IdempotencyKey,
			Status: runtimeprotocol.OperationResultStatusCommitted, CommittedRevision: &published,
			Diagnostics: []semantic.Diagnostic{},
		}
		result.ResultDigest = recoveryResultDigest(result)
		final, err := journal.Finalize(ctx, port.FinalizeRecoveryRecordInput{
			Scope: scope, TerminalPhase: runtimeprotocol.RecoveryPhaseFinal,
			CommitResult: runtimeprotocol.RuntimeCommitResult{OperationResult: result, PreviewEvaluation: &preview},
		})
		if err != nil || final.Status.Phase != runtimeprotocol.RecoveryPhaseFinal {
			t.Fatalf("final=%+v err=%v", final, err)
		}
		reopened, err := NewRecoveryJournal(journal.root, Options{})
		if err != nil {
			t.Fatal(err)
		}
		loaded, err := reopened.Get(ctx, port.GetRecoveryRecordInput{Scope: scope, IdempotencyKey: &input.IdempotencyKey})
		if err != nil || loaded.CommitResult == nil || loaded.CommitResult.OperationResult.CommittedRevision == nil {
			t.Fatalf("loaded=%+v err=%v", loaded, err)
		}
	})
}

func TestPublicAdapterOperationsHonorPreCanceledContexts(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	root := t.TempDir()
	document, _ := NewDocumentStore(root, Options{})
	assets, _ := NewAssetStore(root, Options{})
	state, _ := NewStateBackend(root, Options{})
	history, _ := NewHistoryStore(root, Options{})
	recovery, _ := NewRecoveryJournal(root, Options{})
	scope := testScope()
	operation := runtimeprotocol.OperationID("cancelled_operation")
	revision := runtimeprotocol.RevisionID("cancelled_revision")
	assetRef := port.AssetRef{Scope: scope, Digest: testDigest('1')}
	tests := []struct {
		name string
		call func() error
	}{
		{name: "document head", call: func() error { _, err := document.GetHead(ctx, port.GetDocumentHeadInput{Scope: scope}); return err }},
		{name: "document revision", call: func() error {
			_, err := document.ReadRevision(ctx, port.ReadRevisionInput{Scope: scope, RevisionID: revision})
			return err
		}},
		{name: "document blobs", call: func() error {
			_, err := document.ReadSourceBlobs(ctx, port.ReadSourceBlobsInput{Scope: scope})
			return err
		}},
		{name: "document publish", call: func() error {
			_, err := document.PublishHead(ctx, port.PublishDocumentHeadInput{Scope: scope})
			return err
		}},
		{name: "document abort", call: func() error { return document.AbortStagedRevision(ctx, port.AbortStagedRevisionInput{Scope: scope}) }},
		{name: "document initialize", call: func() error { return document.InitializeDocument(ctx, scope, port.RevisionSnapshot{}, "", "", nil) }},
		{name: "asset stat", call: func() error { _, err := assets.Stat(ctx, assetRef); return err }},
		{name: "asset get", call: func() error { _, err := assets.Get(ctx, assetRef); return err }},
		{name: "asset delete", call: func() error { return assets.DeleteIfUnreferenced(ctx, port.DeleteAssetInput{AssetRef: assetRef}) }},
		{name: "recovery create", call: func() error {
			_, err := recovery.CreatePending(ctx, port.CreatePendingRecordInput{Scope: scope})
			return err
		}},
		{name: "recovery abandon", call: func() error { return recovery.AbandonPending(ctx, port.AbandonPendingRecordInput{Scope: scope}) }},
		{name: "recovery get", call: func() error {
			_, err := recovery.Get(ctx, port.GetRecoveryRecordInput{Scope: scope, OperationID: &operation})
			return err
		}},
		{name: "recovery advance", call: func() error {
			_, err := recovery.Advance(ctx, port.AdvanceRecoveryRecordInput{Scope: scope})
			return err
		}},
		{name: "recovery finalize", call: func() error {
			_, err := recovery.Finalize(ctx, port.FinalizeRecoveryRecordInput{Scope: scope})
			return err
		}},
		{name: "state head", call: func() error { _, err := state.GetHead(ctx, port.GetStateHeadInput{Scope: scope}); return err }},
		{name: "state acquire", call: func() error { _, err := state.AcquireLease(ctx, port.AcquireLeaseInput{Scope: scope}); return err }},
		{name: "state renew", call: func() error { _, err := state.RenewLease(ctx, port.RenewLeaseInput{Scope: scope}); return err }},
		{name: "state release", call: func() error { return state.ReleaseLease(ctx, port.ReleaseLeaseInput{Scope: scope}) }},
		{name: "state validate", call: func() error { _, err := state.ValidateLease(ctx, port.ValidateLeaseInput{Scope: scope}); return err }},
		{name: "state write", call: func() error { _, err := state.WriteState(ctx, port.WriteStateInput{Scope: scope}); return err }},
		{name: "state audit append", call: func() error {
			_, err := state.AppendAuditEvent(ctx, port.AppendAuditEventInput{Scope: scope})
			return err
		}},
		{name: "state audit list", call: func() error {
			_, err := state.ListAuditEvents(ctx, port.ListAuditEventsInput{Scope: scope})
			return err
		}},
		{name: "state initialize", call: func() error { return state.InitializeState(ctx, scope, port.StateSnapshot{}) }},
		{name: "history append", call: func() error {
			_, err := history.AppendRevision(ctx, port.AppendRevisionInput{Scope: scope})
			return err
		}},
		{name: "history get", call: func() error {
			_, err := history.GetRevision(ctx, port.GetRevisionMetadataInput{Scope: scope})
			return err
		}},
		{name: "history list", call: func() error { _, err := history.ListRevisions(ctx, port.ListRevisionsInput{Scope: scope}); return err }},
		{name: "history resolve", call: func() error {
			_, err := history.ResolveProviderVersion(ctx, port.ResolveProviderVersionInput{Scope: scope})
			return err
		}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if err := tc.call(); !errors.Is(err, context.Canceled) {
				t.Fatalf("error=%v", err)
			}
		})
	}
}

func TestAssetReadAndWriteFailuresPreserveCauseAndRejectMetadataDrift(t *testing.T) {
	ctx := context.Background()
	scope := testScope()
	data := []byte("asset")
	digest := digestBytes(data)
	armed := false
	store, err := NewAssetStore(t.TempDir(), Options{Fault: func(operation, path string) error {
		if armed && operation == "open" && strings.HasSuffix(path, ".data") {
			return syscall.EACCES
		}
		return nil
	}})
	if err != nil {
		t.Fatal(err)
	}
	input := port.PutAssetInput{Scope: scope, ExpectedDigest: digest, MediaType: "text/plain", Size: "5", Contents: bytes.NewReader(data)}
	if _, err := store.PutIfAbsent(ctx, input); err != nil {
		t.Fatal(err)
	}
	armed = true
	if _, err := store.Get(ctx, port.AssetRef{Scope: scope, Digest: digest}); !errors.Is(err, syscall.EACCES) {
		t.Fatalf("read cause=%v", err)
	}
	armed = false
	_, metadataPath, _ := store.assetPaths(port.AssetRef{Scope: scope, Digest: digest})
	var disk assetDisk
	if err := store.readJSON(metadataPath, &disk); err != nil {
		t.Fatal(err)
	}
	disk.Metadata.MediaType = ""
	if err := store.writeJSON(metadataPath, disk); err != nil {
		t.Fatal(err)
	}
	if _, err := store.Stat(ctx, port.AssetRef{Scope: scope, Digest: digest}); !errors.Is(err, port.ErrIndeterminate) {
		t.Fatalf("invalid metadata=%v", err)
	}

	failing, err := NewAssetStore(t.TempDir(), Options{Fault: func(operation, path string) error {
		if operation == "open" && strings.HasSuffix(path, ".data") {
			return syscall.ENOSPC
		}
		return nil
	}})
	if err != nil {
		t.Fatal(err)
	}
	input.Contents = bytes.NewReader(data)
	if _, err := failing.PutIfAbsent(ctx, input); !errors.Is(err, syscall.ENOSPC) {
		t.Fatalf("write cause=%v", err)
	}
	if _, err := failing.Stat(ctx, port.AssetRef{Scope: scope, Digest: digest}); !errors.Is(err, port.ErrNotFound) {
		t.Fatalf("failed put visibility=%v", err)
	}
}
