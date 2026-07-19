// SPDX-License-Identifier: LicenseRef-LayerDraw-1.0
package local

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"sync"
	"syscall"
	"testing"
	"time"

	"github.com/dencyuinc/layerdraw/gen/go/protocolcommon"
	"github.com/dencyuinc/layerdraw/gen/go/runtimeprotocol"
	"github.com/dencyuinc/layerdraw/gen/go/semantic"
	"github.com/dencyuinc/layerdraw/internal/runtime/port"
)

func testDigest(b byte) protocolcommon.Digest {
	return protocolcommon.Digest("sha256:" + string(bytes.Repeat([]byte{b}, 64)))
}
func testScope() runtimeprotocol.RuntimeScope {
	return runtimeprotocol.RuntimeScope{DocumentID: "doc_test", LocalScopeID: "local", AccessFingerprint: testDigest('a')}
}
func source(id string, data []byte) port.SourceBlob {
	return port.SourceBlob{Ref: protocolcommon.BlobRef{BlobID: id, Digest: digestBytes(data), Lifetime: protocolcommon.BlobLifetimeSession, MediaType: "text/plain", Size: protocolcommon.CanonicalUint64(string(rune('0' + len(data))))}, Contents: data}
}

func TestDocumentStorePublishesConditionallyAndRestarts(t *testing.T) {
	ctx := context.Background()
	root := t.TempDir()
	scope := testScope()
	ds, err := NewDocumentStore(root, Options{})
	if err != nil {
		t.Fatal(err)
	}
	if inspected, err := ds.ListStaged(ctx, scope, 1); err != nil || len(inspected) != 0 {
		t.Fatalf("empty staged inspection=%+v err=%v", inspected, err)
	}
	cancelled, cancel := context.WithCancel(ctx)
	cancel()
	if _, err := ds.ListStaged(cancelled, scope, 1); !errors.Is(err, context.Canceled) {
		t.Fatalf("cancelled staged inspection=%v", err)
	}
	provider := runtimeprotocol.ProviderVersionToken("1")
	base := runtimeprotocol.CommittedRevisionRef{DocumentID: scope.DocumentID, RevisionID: "rev_base", DefinitionHash: testDigest('b'), GraphHash: testDigest('c'), ProviderVersion: &provider}
	blob := source("source", []byte("x"))
	snapshot := port.RevisionSnapshot{Revision: base, SourceBlobs: []protocolcommon.BlobRef{blob.Ref}, Manifest: protocolcommon.BlobRef{BlobID: "manifest", Digest: testDigest('d'), Lifetime: protocolcommon.BlobLifetimeSession, MediaType: "application/json", Size: "1"}}
	if err := ds.InitializeDocument(ctx, scope, snapshot, provider, "3", []port.SourceBlob{blob}); err != nil {
		t.Fatal(err)
	}
	next := source("next", []byte("y"))
	in := port.StageRevisionInput{Scope: scope, OperationID: "op", IdempotencyKey: "idempotency_key_1", BaseRevision: base, DefinitionHash: testDigest('e'), GraphHash: testDigest('f'), SourceBlobs: port.SourceBlobSet{Revision: base, Blobs: []port.SourceBlob{next}}, Manifest: snapshot.Manifest, DecisionDigest: testDigest('1'), EvaluationDigest: testDigest('2')}
	staged, err := ds.StageRevision(ctx, in)
	if err != nil {
		t.Fatal(err)
	}
	inspected, err := ds.ListStaged(ctx, scope, 1)
	if err != nil || len(inspected) != 1 || inspected[0].Stage.StageID != staged.StageID {
		t.Fatalf("staged inspection=%+v err=%v", inspected, err)
	}
	if _, err := ds.ListStaged(ctx, scope, 0); !errors.Is(err, port.ErrConflict) {
		t.Fatalf("unbounded staged inspection=%v", err)
	}
	if _, err := ds.PublishHead(ctx, port.PublishDocumentHeadInput{Scope: scope, StageID: staged.StageID, ExpectedRevision: base.RevisionID, ExpectedDefinitionHash: base.DefinitionHash, ExpectedProviderVersion: provider, FencingToken: "2"}); !errors.Is(err, port.ErrConflict) {
		t.Fatalf("stale fencing error=%v", err)
	}
	pub, err := ds.PublishHead(ctx, port.PublishDocumentHeadInput{Scope: scope, StageID: staged.StageID, ExpectedRevision: base.RevisionID, ExpectedDefinitionHash: base.DefinitionHash, ExpectedProviderVersion: provider, FencingToken: "3"})
	if err != nil || !pub.Published {
		t.Fatalf("publish=%+v err=%v", pub, err)
	}
	reopened, _ := NewDocumentStore(root, Options{})
	got, err := reopened.ReadSourceBlobs(ctx, port.ReadSourceBlobsInput{Scope: scope, Revision: pub.Revision, Blobs: []protocolcommon.BlobRef{next.Ref}})
	if err != nil || string(got.Blobs[0].Contents) != "y" {
		t.Fatalf("restart read=%+v err=%v", got, err)
	}
	got.Blobs[0].Contents[0] = 'z'
	again, _ := reopened.ReadSourceBlobs(ctx, port.ReadSourceBlobsInput{Scope: scope, Revision: pub.Revision, Blobs: []protocolcommon.BlobRef{next.Ref}})
	if string(again.Blobs[0].Contents) != "y" {
		t.Fatal("read was not defensive")
	}
}

func TestAssetsBoundStreamsDigestConcurrencyAndSymlinks(t *testing.T) {
	ctx := context.Background()
	root := t.TempDir()
	scope := testScope()
	a, _ := NewAssetStore(root, Options{MaxAssetBytes: 4})
	data := []byte("abc")
	digest := digestBytes(data)
	input := func(r io.Reader) port.PutAssetInput {
		return port.PutAssetInput{Scope: scope, ExpectedDigest: digest, MediaType: "text/plain", Size: "3", Contents: r}
	}
	if _, err := a.PutIfAbsent(ctx, input(bytes.NewReader([]byte("ab")))); !errors.Is(err, port.ErrConflict) {
		t.Fatalf("short=%v", err)
	}
	if _, err := a.PutIfAbsent(ctx, input(bytes.NewReader([]byte("abcd")))); !errors.Is(err, port.ErrConflict) {
		t.Fatalf("overlong=%v", err)
	}
	wrong := input(bytes.NewReader(data))
	wrong.ExpectedDigest = testDigest('0')
	if _, err := a.PutIfAbsent(ctx, wrong); !errors.Is(err, port.ErrConflict) {
		t.Fatalf("digest=%v", err)
	}
	var wg sync.WaitGroup
	errs := make(chan error, 2)
	for range 2 {
		wg.Add(1)
		go func() { defer wg.Done(); _, e := a.PutIfAbsent(ctx, input(bytes.NewReader(data))); errs <- e }()
	}
	wg.Wait()
	close(errs)
	for e := range errs {
		if e != nil {
			t.Fatal(e)
		}
	}
	reopened, _ := NewAssetStore(root, Options{})
	r, err := reopened.Get(ctx, port.AssetRef{Scope: scope, Digest: digest})
	if err != nil {
		t.Fatal(err)
	}
	got, _ := io.ReadAll(r)
	r.Close()
	if !bytes.Equal(got, data) {
		t.Fatal("restart asset mismatch")
	}
	dataPath, _, _ := reopened.assetPaths(port.AssetRef{Scope: scope, Digest: digest})
	if err := os.WriteFile(dataPath, []byte("abd"), fileMode); err != nil {
		t.Fatal(err)
	}
	if _, err := reopened.Get(ctx, port.AssetRef{Scope: scope, Digest: digest}); !errors.Is(err, port.ErrIndeterminate) {
		t.Fatalf("corrupt digest=%v", err)
	}
	if err := os.Remove(dataPath); err != nil {
		t.Fatal(err)
	}
	outside := filepath.Join(t.TempDir(), "outside")
	if err := os.WriteFile(outside, []byte("secret"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(outside, dataPath); err != nil {
		t.Fatal(err)
	}
	if _, err := reopened.Get(ctx, port.AssetRef{Scope: scope, Digest: digest}); !errors.Is(err, port.ErrConflict) {
		t.Fatalf("symlink=%v", err)
	}
}

func TestStateLeaseFencingSnapshotsAuditAndRestart(t *testing.T) {
	ctx := context.Background()
	root := t.TempDir()
	scope := testScope()
	now := time.Date(2026, 7, 19, 0, 0, 0, 0, time.UTC)
	clock := func() time.Time { return now }
	s, _ := NewStateBackend(root, Options{Now: clock})
	head := port.StateHead{StateVersion: "0", BackendVersion: "1", DefinitionHash: testDigest('b'), GraphHash: testDigest('c'), CapturedAt: protocolcommon.Rfc3339Time(now.Format(time.RFC3339)), SubjectHashes: map[semantic.StableAddress]protocolcommon.Digest{}}
	snap := port.StateSnapshot{Head: head, Contents: protocolcommon.BlobRef{BlobID: "state", Digest: testDigest('d'), Lifetime: protocolcommon.BlobLifetimeSession, MediaType: "application/json", Size: "1"}, Records: []port.StateRecord{}}
	if err := s.InitializeState(ctx, scope, snap); err != nil {
		t.Fatal(err)
	}
	lease, err := s.AcquireLease(ctx, port.AcquireLeaseInput{Scope: scope, OwnerID: "owner", TTL: time.Minute})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.AcquireLease(ctx, port.AcquireLeaseInput{Scope: scope, OwnerID: "other", TTL: time.Minute}); !errors.Is(err, port.ErrConflict) {
		t.Fatalf("concurrent lease=%v", err)
	}
	mutation := runtimeprotocol.StateMutation{AffectedSubjects: []semantic.StableAddress{"ldl:project:fixture:entity:item"}, ExpectedStateVersion: "0", MutationDigest: testDigest('e'), MutationBlob: runtimeprotocol.RuntimeBlobRef{Blob: protocolcommon.BlobRef{BlobID: "mutation_blob_1234", Digest: testDigest('f'), Lifetime: protocolcommon.BlobLifetimeSession, MediaType: "application/octet-stream", Size: "1"}, Scope: scope, SessionGeneration: "1"}}
	write, err := s.WriteState(ctx, port.WriteStateInput{Scope: scope, OperationID: "state_op", IdempotencyKey: "state_idempotency_1", ExpectedStateVersion: "0", ExpectedBackendVersion: "1", ExpectedDefinitionHash: head.DefinitionHash, ExpectedSubjectHashes: head.SubjectHashes, LeaseToken: lease.LeaseToken, Mutation: mutation})
	if err != nil || write.Head.StateVersion != "1" {
		t.Fatalf("write=%+v err=%v", write, err)
	}
	exported, err := s.ExportSnapshot(ctx, port.ExportStateSnapshotInput{Scope: scope, StateVersion: "1"})
	if err != nil || exported.Head.StateVersion != "1" {
		t.Fatalf("snapshot=%+v err=%v", exported, err)
	}
	event := protocolcommon.BlobRef{BlobID: "audit", Digest: testDigest('9'), Lifetime: protocolcommon.BlobLifetimeSession, MediaType: "application/json", Size: "1"}
	if _, err := s.AppendAuditEvent(ctx, port.AppendAuditEventInput{Scope: scope, OperationID: "op", ExpectedStateVersion: "1", EventDigest: event.Digest, Event: event}); err != nil {
		t.Fatal(err)
	}
	page, err := s.ListAuditEvents(ctx, port.ListAuditEventsInput{Scope: scope, MaxItems: "1"})
	if err != nil || len(page.Items) != 1 {
		t.Fatalf("audit=%+v err=%v", page, err)
	}
	now = now.Add(2 * time.Minute)
	if _, err := s.ValidateLease(ctx, port.ValidateLeaseInput{Scope: scope, LeaseToken: lease.LeaseToken}); !errors.Is(err, port.ErrConflict) {
		t.Fatalf("expired=%v", err)
	}
	lease2, err := s.AcquireLease(ctx, port.AcquireLeaseInput{Scope: scope, OwnerID: "next", TTL: time.Minute})
	if err != nil || lease2.FencingToken == lease.FencingToken {
		t.Fatalf("fencing=%+v err=%v", lease2, err)
	}
	reopened, _ := NewStateBackend(root, Options{Now: clock})
	if h, err := reopened.GetHead(ctx, port.GetStateHeadInput{Scope: scope}); err != nil || h.StateVersion != "1" {
		t.Fatalf("restart head=%+v err=%v", h, err)
	}
}

func TestHistoryBoundsIdempotenceAndRestart(t *testing.T) {
	ctx := context.Background()
	root := t.TempDir()
	scope := testScope()
	h, _ := NewHistoryStore(root, Options{})
	provider := runtimeprotocol.ProviderVersionToken("7")
	m := runtimeprotocol.RevisionMetadata{Revision: runtimeprotocol.CommittedRevisionRef{DocumentID: scope.DocumentID, RevisionID: "r1", DefinitionHash: testDigest('b'), GraphHash: testDigest('c'), ProviderVersion: &provider}, OperationID: "op1", CommittedAt: "2026-07-19T00:00:00Z", Trigger: runtimeprotocol.CommitTriggerExplicitSave, AuthoringDecisionDigest: testDigest('d')}
	if _, err := h.AppendRevision(ctx, port.AppendRevisionInput{Scope: scope, Metadata: m}); err != nil {
		t.Fatal(err)
	}
	if _, err := h.AppendRevision(ctx, port.AppendRevisionInput{Scope: scope, Metadata: m}); err != nil {
		t.Fatal(err)
	}
	conflict := m
	conflict.Trigger = runtimeprotocol.CommitTriggerAutosave
	if _, err := h.AppendRevision(ctx, port.AppendRevisionInput{Scope: scope, Metadata: conflict}); !errors.Is(err, port.ErrConflict) {
		t.Fatalf("conflict=%v", err)
	}
	reopened, _ := NewHistoryStore(root, Options{})
	page, err := reopened.ListRevisions(ctx, port.ListRevisionsInput{Scope: scope, MaxItems: "1", MaxOutputBytes: "4096"})
	if err != nil || len(page.Items) != 1 {
		t.Fatalf("page=%+v err=%v", page, err)
	}
	resolved, err := reopened.ResolveProviderVersion(ctx, port.ResolveProviderVersionInput{Scope: scope, ProviderVersion: provider})
	if err != nil || resolved.Revision.RevisionID != "r1" {
		t.Fatalf("resolve=%+v err=%v", resolved, err)
	}
}

func TestRecoveryJournalIndexesTransitionsRestartAndCorruption(t *testing.T) {
	ctx := context.Background()
	root := t.TempDir()
	scope := testScope()
	j, _ := NewRecoveryJournal(root, Options{})
	cancelled, cancel := context.WithCancel(ctx)
	cancel()
	if _, err := j.List(cancelled, scope, 1); !errors.Is(err, context.Canceled) {
		t.Fatalf("cancelled recovery inspection=%v", err)
	}
	base := runtimeprotocol.CommittedRevisionRef{DocumentID: scope.DocumentID, RevisionID: "base", DefinitionHash: testDigest('b'), GraphHash: testDigest('c')}
	in := port.CreatePendingRecordInput{Scope: scope, OperationID: "op", IdempotencyKey: "idempotency_key_1", PayloadDigest: testDigest('d'), BaseRevision: base}
	if _, err := j.CreatePending(ctx, in); err != nil {
		t.Fatal(err)
	}
	inspected, err := j.List(ctx, scope, 1)
	if err != nil || len(inspected) != 1 || inspected[0].Record.Status.OperationID != in.OperationID {
		t.Fatalf("recovery inspection=%+v err=%v", inspected, err)
	}
	if _, err := j.List(ctx, scope, 0); !errors.Is(err, port.ErrConflict) {
		t.Fatalf("unbounded recovery inspection=%v", err)
	}
	other := in
	other.OperationID = "other"
	if _, err := j.CreatePending(ctx, other); !errors.Is(err, port.ErrConflict) {
		t.Fatalf("key conflict=%v", err)
	}
	eval, decision := testDigest('e'), testDigest('f')
	if _, err := j.Advance(ctx, port.AdvanceRecoveryRecordInput{Scope: scope, OperationID: in.OperationID, ExpectedPhase: runtimeprotocol.RecoveryPhasePending, NextPhase: runtimeprotocol.RecoveryPhasePublished}); !errors.Is(err, port.ErrConflict) {
		t.Fatalf("illegal transition=%v", err)
	}
	if _, err := j.Advance(ctx, port.AdvanceRecoveryRecordInput{Scope: scope, OperationID: in.OperationID, ExpectedPhase: runtimeprotocol.RecoveryPhasePending, NextPhase: runtimeprotocol.RecoveryPhaseStaged, EvaluationDigest: &eval, DecisionDigest: &decision}); err != nil {
		t.Fatal(err)
	}
	reopened, _ := NewRecoveryJournal(root, Options{})
	record, err := reopened.Get(ctx, port.GetRecoveryRecordInput{Scope: scope, IdempotencyKey: &in.IdempotencyKey})
	if err != nil || record.Status.Phase != runtimeprotocol.RecoveryPhaseStaged || record.EvaluationDigest == nil {
		t.Fatalf("restart record=%+v err=%v", record, err)
	}
	p, _ := reopened.recoveryPath(scope)
	if err := os.WriteFile(p, []byte("{"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := reopened.Get(ctx, port.GetRecoveryRecordInput{Scope: scope, OperationID: &in.OperationID}); !errors.Is(err, port.ErrIndeterminate) {
		t.Fatalf("corruption=%v", err)
	}
}

func TestRejectsSymlinkRoot(t *testing.T) {
	outside := t.TempDir()
	link := filepath.Join(t.TempDir(), "link")
	if err := os.Symlink(outside, link); err != nil {
		t.Fatal(err)
	}
	if _, err := NewAssetStore(filepath.Join(link, "child"), Options{}); !errors.Is(err, port.ErrConflict) {
		t.Fatalf("root symlink=%v", err)
	}
}

func TestRejectsTraversalIdentityAndPreservesOSFailureClass(t *testing.T) {
	scope := testScope()
	d, _ := NewDocumentStore(t.TempDir(), Options{})
	if _, err := d.ReadRevision(context.Background(), port.ReadRevisionInput{Scope: scope, RevisionID: "../escape"}); !errors.Is(err, port.ErrConflict) {
		t.Fatalf("traversal=%v", err)
	}
	data, err := os.ReadFile(filepath.Join("..", "..", "..", "tests", "conformance", "testdata", "local_runtime_persistence_v1.json"))
	if err != nil {
		t.Fatal(err)
	}
	var corpus struct {
		SchemaVersion int `json:"schema_version"`
		FaultMatrix   []struct {
			ID            string `json:"id"`
			Surface       string `json:"surface"`
			Injection     string `json:"injection"`
			ExpectedError string `json:"expected_error"`
		} `json:"fault_matrix"`
	}
	if err := json.Unmarshal(data, &corpus); err != nil || corpus.SchemaVersion != 1 {
		t.Fatalf("invalid persistence fault corpus: version=%d err=%v", corpus.SchemaVersion, err)
	}
	cases := 0
	for _, fault := range corpus.FaultMatrix {
		if fault.Surface != "filesystem_error" {
			continue
		}
		cases++
		t.Run(fault.ID, func(t *testing.T) {
			var injected error
			switch fault.Injection {
			case "ENOSPC":
				injected = syscall.ENOSPC
			case "EACCES":
				injected = syscall.EACCES
			default:
				t.Fatalf("unknown filesystem error %q", fault.Injection)
			}
			classified := classify(injected)
			if fault.ExpectedError == "preserved" && !errors.Is(classified, injected) {
				t.Fatalf("OS failure class was lost: %v", classified)
			}
			if fault.ExpectedError == "permission" && !errors.Is(classified, fs.ErrPermission) {
				t.Fatalf("permission class was lost: %v", classified)
			}
		})
	}
	if cases == 0 {
		t.Fatal("persistence fault corpus has no filesystem_error cases")
	}
}

func TestInjectedAtomicFilesystemFailures(t *testing.T) {
	data, err := os.ReadFile(filepath.Join("..", "..", "..", "tests", "conformance", "testdata", "local_runtime_persistence_v1.json"))
	if err != nil {
		t.Fatal(err)
	}
	var corpus struct {
		SchemaVersion int `json:"schema_version"`
		FaultMatrix   []struct {
			ID                 string `json:"id"`
			Surface            string `json:"surface"`
			Injection          string `json:"injection"`
			ExpectedVisibility string `json:"expected_visibility"`
			ExpectedError      string `json:"expected_error"`
		} `json:"fault_matrix"`
	}
	if err := json.Unmarshal(data, &corpus); err != nil || corpus.SchemaVersion != 1 {
		t.Fatalf("invalid persistence fault corpus: version=%d err=%v", corpus.SchemaVersion, err)
	}
	cases := 0
	for _, fault := range corpus.FaultMatrix {
		if fault.Surface != "filesystem_atomic" {
			continue
		}
		cases++
		t.Run(fault.ID, func(t *testing.T) {
			root := t.TempDir()
			injected := errors.New("injected " + fault.Injection)
			s, err := New(root, Options{Fault: func(got, _ string) error {
				if got == fault.Injection {
					return injected
				}
				return nil
			}})
			if err != nil {
				t.Fatal(err)
			}
			err = s.atomicWrite(filepath.Join(root, "record"), bytes.NewReader([]byte("x")), 1)
			if !errors.Is(err, injected) || (fault.ExpectedError == "indeterminate") != errors.Is(err, port.ErrIndeterminate) {
				t.Fatalf("error=%v", err)
			}
			_, statErr := os.Lstat(filepath.Join(root, "record"))
			visible := statErr == nil
			wantVisible := fault.ExpectedVisibility == "published_indeterminate"
			if visible != wantVisible {
				t.Fatalf("visibility=%t want=%t", visible, wantVisible)
			}
		})
	}
	if cases == 0 {
		t.Fatal("persistence fault corpus has no filesystem_atomic cases")
	}
}
