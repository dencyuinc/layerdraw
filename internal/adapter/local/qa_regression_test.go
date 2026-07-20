// SPDX-License-Identifier: LicenseRef-LayerDraw-1.0
package local

import (
	"bytes"
	"context"
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/dencyuinc/layerdraw/gen/go/protocolcommon"
	"github.com/dencyuinc/layerdraw/gen/go/runtimeprotocol"
	"github.com/dencyuinc/layerdraw/gen/go/semantic"
	"github.com/dencyuinc/layerdraw/internal/runtime/port"
)

func TestEveryAdapterRejectsIntermediateNamespaceSymlink(t *testing.T) {
	ctx := context.Background()
	scope := testScope()
	tests := []struct {
		name, namespace string
		call            func(*Store) error
	}{{"document", "documents", func(s *Store) error {
		_, e := (&Document{s}).GetHead(ctx, port.GetDocumentHeadInput{Scope: scope})
		return e
	}}, {"state", "state", func(s *Store) error { _, e := (&State{s}).GetHead(ctx, port.GetStateHeadInput{Scope: scope}); return e }}, {"assets", "assets", func(s *Store) error {
		_, e := (&Assets{s}).Stat(ctx, port.AssetRef{Scope: scope, Digest: testDigest('a')})
		return e
	}}, {"history", "history", func(s *Store) error {
		_, e := (&History{s}).ListRevisions(ctx, port.ListRevisionsInput{Scope: scope, MaxItems: "1", MaxOutputBytes: "1024"})
		return e
	}}, {"recovery", "recovery", func(s *Store) error {
		op := runtimeprotocol.OperationID("op")
		_, e := (&Recovery{s}).Get(ctx, port.GetRecoveryRecordInput{Scope: scope, OperationID: &op})
		return e
	}}}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			s, e := New(t.TempDir(), Options{})
			if e != nil {
				t.Fatal(e)
			}
			scopeDir, e := s.scopeDir(scope)
			if e != nil {
				t.Fatal(e)
			}
			outside := t.TempDir()
			if e = os.Symlink(outside, filepath.Join(scopeDir, tc.namespace)); e != nil {
				t.Fatal(e)
			}
			if e = tc.call(s); !errors.Is(e, port.ErrConflict) {
				t.Fatalf("symlink error=%v", e)
			}
		})
	}
}

func TestEveryAdapterRejectsFinalRecordSymlink(t *testing.T) {
	ctx := context.Background()
	scope := testScope()
	tests := []struct {
		name     string
		relative func(*Store) string
		call     func(*Store) error
	}{{"document", func(*Store) string { return filepath.Join("documents", "head.json") }, func(s *Store) error {
		_, e := (&Document{s}).GetHead(ctx, port.GetDocumentHeadInput{Scope: scope})
		return e
	}}, {"state", func(*Store) string { return filepath.Join("state", "current.json") }, func(s *Store) error { _, e := (&State{s}).GetHead(ctx, port.GetStateHeadInput{Scope: scope}); return e }}, {"assets", func(s *Store) string {
		id, _ := safeID(string(testDigest('a')))
		return filepath.Join("assets", id+".json")
	}, func(s *Store) error {
		_, e := (&Assets{s}).Stat(ctx, port.AssetRef{Scope: scope, Digest: testDigest('a')})
		return e
	}}, {"history", func(*Store) string { return filepath.Join("history", "revisions.json") }, func(s *Store) error {
		_, e := (&History{s}).ListRevisions(ctx, port.ListRevisionsInput{Scope: scope, MaxItems: "1", MaxOutputBytes: "1024"})
		return e
	}}, {"recovery", func(*Store) string { return filepath.Join("recovery", "journal.json") }, func(s *Store) error {
		op := runtimeprotocol.OperationID("op")
		_, e := (&Recovery{s}).Get(ctx, port.GetRecoveryRecordInput{Scope: scope, OperationID: &op})
		return e
	}}}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			s, e := New(t.TempDir(), Options{})
			if e != nil {
				t.Fatal(e)
			}
			scopeDir, _ := s.scopeDir(scope)
			record := filepath.Join(scopeDir, tc.relative(s))
			if e = s.ensureDir(filepath.Dir(record)); e != nil {
				t.Fatal(e)
			}
			outside := filepath.Join(t.TempDir(), "record")
			if e = os.WriteFile(outside, []byte("{}"), fileMode); e != nil {
				t.Fatal(e)
			}
			if e = os.Symlink(outside, record); e != nil {
				t.Fatal(e)
			}
			if e = tc.call(s); !errors.Is(e, port.ErrConflict) {
				t.Fatalf("final symlink=%v", e)
			}
		})
	}
}

func TestFinalFileSymlinkAndReopenedPermissionsRejected(t *testing.T) {
	s, e := New(t.TempDir(), Options{})
	if e != nil {
		t.Fatal(e)
	}
	dir := filepath.Join(s.root, "records")
	if e = s.ensureDir(dir); e != nil {
		t.Fatal(e)
	}
	outside := filepath.Join(t.TempDir(), "record")
	if e = os.WriteFile(outside, []byte("{}"), fileMode); e != nil {
		t.Fatal(e)
	}
	link := filepath.Join(dir, "record.json")
	if e = os.Symlink(outside, link); e != nil {
		t.Fatal(e)
	}
	var out map[string]any
	if e = s.readJSON(link, &out); !errors.Is(e, port.ErrConflict) {
		t.Fatalf("file symlink=%v", e)
	}
	if e = os.Remove(link); e != nil {
		t.Fatal(e)
	}
	if runtime.GOOS == "windows" {
		return
	}
	if e = os.WriteFile(link, []byte("{}"), 0o666); e != nil {
		t.Fatal(e)
	}
	if e = s.readJSON(link, &out); !errors.Is(e, port.ErrConflict) {
		t.Fatalf("file mode=%v", e)
	}
	if e = os.Chmod(dir, 0o777); e != nil {
		t.Fatal(e)
	}
	if e = s.readJSON(link, &out); !errors.Is(e, port.ErrConflict) {
		t.Fatalf("directory mode=%v", e)
	}
	if e = os.Chmod(s.root, 0o777); e != nil {
		t.Fatal(e)
	}
	if _, e = New(s.root, Options{}); !errors.Is(e, port.ErrConflict) {
		t.Fatalf("reopened root mode=%v", e)
	}
}

func TestAtomicWriteVisibilityBoundary(t *testing.T) {
	for _, op := range []string{"open", "write", "sync", "rename", "diropen", "dirsync"} {
		t.Run(op, func(t *testing.T) {
			root := t.TempDir()
			boom := errors.New("boom")
			s, e := New(root, Options{Fault: func(got, _ string) error {
				if got == op {
					return boom
				}
				return nil
			}})
			if e != nil {
				t.Fatal(e)
			}
			path := filepath.Join(root, "value")
			e = s.atomicWrite(path, bytes.NewReader([]byte("x")), 1)
			if !errors.Is(e, boom) {
				t.Fatalf("cause lost: %v", e)
			}
			post := op == "diropen" || op == "dirsync"
			if errors.Is(e, port.ErrIndeterminate) != post {
				t.Fatalf("operation=%s error=%v", op, e)
			}
			_, statErr := os.Stat(path)
			if (statErr == nil) != post {
				t.Fatalf("operation=%s visible=%v", op, statErr == nil)
			}
		})
	}
}

func TestAssetPairValidationNilAndPostRenameSync(t *testing.T) {
	ctx := context.Background()
	scope := testScope()
	armed := false
	store, e := NewAssetStore(t.TempDir(), Options{Fault: func(op, _ string) error {
		if armed && op == "dirsync" {
			return errors.New("disk sync")
		}
		return nil
	}})
	if e != nil {
		t.Fatal(e)
	}
	data := []byte("abc")
	in := port.PutAssetInput{Scope: scope, ExpectedDigest: digestBytes(data), MediaType: "text/plain", Size: "3"}
	if _, e = store.PutIfAbsent(ctx, in); !errors.Is(e, port.ErrConflict) {
		t.Fatalf("nil reader=%v", e)
	}
	in.Contents = bytes.NewReader(data)
	armed = true
	if _, e = store.PutIfAbsent(ctx, in); !errors.Is(e, port.ErrIndeterminate) {
		t.Fatalf("post rename=%v", e)
	}
	armed = false
	in.Contents = bytes.NewReader(data)
	if _, e = store.PutIfAbsent(ctx, in); e != nil {
		t.Fatal(e)
	}
	r, e := store.Get(ctx, port.AssetRef{Scope: scope, Digest: in.ExpectedDigest})
	if e != nil {
		t.Fatal(e)
	}
	buf := make([]byte, 3)
	n, e := r.Read(buf)
	if e != nil || n != 3 || !bytes.Equal(buf, data) {
		t.Fatalf("exact read n=%d err=%v data=%q", n, e, buf)
	}
	_ = r.Close()
	dataPath, _, _ := store.assetPaths(port.AssetRef{Scope: scope, Digest: in.ExpectedDigest})
	if e = os.Remove(dataPath); e != nil {
		t.Fatal(e)
	}
	in.Contents = bytes.NewReader(data)
	if _, e = store.PutIfAbsent(ctx, in); !errors.Is(e, port.ErrIndeterminate) {
		t.Fatalf("missing pair=%v", e)
	}
}

func TestAssetDeletionSyncFailureIsIndeterminate(t *testing.T) {
	ctx := context.Background()
	scope := testScope()
	armed := false
	a, e := NewAssetStore(t.TempDir(), Options{Fault: func(op, _ string) error {
		if armed && op == "dirsync" {
			return errors.New("sync")
		}
		return nil
	}})
	if e != nil {
		t.Fatal(e)
	}
	data := []byte("abc")
	in := port.PutAssetInput{Scope: scope, ExpectedDigest: digestBytes(data), MediaType: "text/plain", Size: "3", Contents: bytes.NewReader(data)}
	if _, e = a.PutIfAbsent(ctx, in); e != nil {
		t.Fatal(e)
	}
	armed = true
	e = a.DeleteIfUnreferenced(ctx, port.DeleteAssetInput{AssetRef: port.AssetRef{Scope: scope, Digest: in.ExpectedDigest}, ExpectedUnreferenced: true})
	if !errors.Is(e, port.ErrIndeterminate) {
		t.Fatalf("delete sync=%v", e)
	}
}

func stateFixture(now time.Time) (port.StateSnapshot, runtimeprotocol.StateMutation) {
	scope := testScope()
	head := port.StateHead{StateVersion: "0", BackendVersion: "1", DefinitionHash: testDigest('b'), GraphHash: testDigest('c'), CapturedAt: protocolcommon.Rfc3339Time(now.Format(time.RFC3339)), SubjectHashes: map[semantic.StableAddress]protocolcommon.Digest{}}
	snap := port.StateSnapshot{Head: head, Contents: protocolcommon.BlobRef{BlobID: "state_blob_123456", Digest: testDigest('d'), Lifetime: protocolcommon.BlobLifetimeSession, MediaType: "application/json", Size: "1"}, Records: []port.StateRecord{}}
	mutation := runtimeprotocol.StateMutation{AffectedSubjects: []semantic.StableAddress{"ldl:project:fixture:entity:item"}, ExpectedStateVersion: "0", MutationDigest: testDigest('e'), MutationBlob: runtimeprotocol.RuntimeBlobRef{Blob: protocolcommon.BlobRef{BlobID: "mutation_blob_1234", Digest: testDigest('f'), Lifetime: protocolcommon.BlobLifetimeSession, MediaType: "application/octet-stream", Size: "1"}, Scope: scope, SessionGeneration: "1"}}
	return snap, mutation
}

func TestStateOrphanSnapshotIsNotExportedAndRetryCommits(t *testing.T) {
	ctx := context.Background()
	scope := testScope()
	now := time.Date(2026, 7, 19, 0, 0, 0, 0, time.UTC)
	armed := false
	s, e := NewStateBackend(t.TempDir(), Options{Now: func() time.Time { return now }, Fault: func(op, path string) error {
		if armed && op == "write" && strings.HasSuffix(path, "current.json") {
			return errors.New("full")
		}
		return nil
	}})
	if e != nil {
		t.Fatal(e)
	}
	snap, mutation := stateFixture(now)
	if e = s.InitializeState(ctx, scope, snap); e != nil {
		t.Fatal(e)
	}
	lease, e := s.AcquireLease(ctx, port.AcquireLeaseInput{Scope: scope, OwnerID: "owner", TTL: time.Hour})
	if e != nil {
		t.Fatal(e)
	}
	input := port.WriteStateInput{Scope: scope, OperationID: "state_op", IdempotencyKey: "state_idempotency_1", ExpectedStateVersion: "0", ExpectedBackendVersion: "1", ExpectedDefinitionHash: snap.Head.DefinitionHash, ExpectedSubjectHashes: snap.Head.SubjectHashes, LeaseToken: lease.LeaseToken, Mutation: mutation}
	armed = true
	if _, e = s.WriteState(ctx, input); e == nil || errors.Is(e, port.ErrIndeterminate) {
		t.Fatalf("prepublication failure=%v", e)
	}
	if _, e = s.ExportSnapshot(ctx, port.ExportStateSnapshotInput{Scope: scope, StateVersion: "1"}); !errors.Is(e, port.ErrNotFound) {
		t.Fatalf("orphan export=%v", e)
	}
	armed = false
	if _, e = s.WriteState(ctx, input); e != nil {
		t.Fatal(e)
	}
	if _, e = s.ExportSnapshot(ctx, port.ExportStateSnapshotInput{Scope: scope, StateVersion: "1"}); e != nil {
		t.Fatal(e)
	}
}

func TestStateCurrentPostRenameSyncIsIndeterminateButCommitted(t *testing.T) {
	ctx := context.Background()
	scope := testScope()
	now := time.Date(2026, 7, 19, 0, 0, 0, 0, time.UTC)
	armed := false
	syncs := 0
	s, e := NewStateBackend(t.TempDir(), Options{Now: func() time.Time { return now }, Fault: func(op, _ string) error {
		if armed && op == "dirsync" {
			syncs++
			if syncs == 2 {
				return errors.New("sync")
			}
		}
		return nil
	}})
	if e != nil {
		t.Fatal(e)
	}
	snap, mutation := stateFixture(now)
	if e = s.InitializeState(ctx, scope, snap); e != nil {
		t.Fatal(e)
	}
	lease, e := s.AcquireLease(ctx, port.AcquireLeaseInput{Scope: scope, OwnerID: "owner", TTL: time.Hour})
	if e != nil {
		t.Fatal(e)
	}
	armed = true
	_, e = s.WriteState(ctx, port.WriteStateInput{Scope: scope, OperationID: "state_op", IdempotencyKey: "state_idempotency_1", ExpectedStateVersion: "0", ExpectedBackendVersion: "1", ExpectedDefinitionHash: snap.Head.DefinitionHash, ExpectedSubjectHashes: snap.Head.SubjectHashes, LeaseToken: lease.LeaseToken, Mutation: mutation})
	if !errors.Is(e, port.ErrIndeterminate) {
		t.Fatalf("post rename=%v", e)
	}
	armed = false
	if exported, e := s.ExportSnapshot(ctx, port.ExportStateSnapshotInput{Scope: scope, StateVersion: "1"}); e != nil || exported.Head.StateVersion != "1" {
		t.Fatalf("committed snapshot=%+v err=%v", exported, e)
	}
}

func TestRecoveryReverseIndexesAndPublicationEvidence(t *testing.T) {
	ctx := context.Background()
	scope := testScope()
	newJournal := func(t *testing.T) (*Recovery, port.CreatePendingRecordInput) {
		j, e := NewRecoveryJournal(t.TempDir(), Options{})
		if e != nil {
			t.Fatal(e)
		}
		in := port.CreatePendingRecordInput{Scope: scope, OperationID: "op", IdempotencyKey: "idempotency_key_1", PayloadDigest: testDigest('d'), BaseRevision: runtimeprotocol.CommittedRevisionRef{DocumentID: scope.DocumentID, RevisionID: "base", DefinitionHash: testDigest('b'), GraphHash: testDigest('c')}}
		if _, e = j.CreatePending(ctx, in); e != nil {
			t.Fatal(e)
		}
		return j, in
	}
	for _, kind := range []string{"missing_operation", "missing_idempotency", "orphan_record", "scope_mismatch"} {
		t.Run(kind, func(t *testing.T) {
			j, in := newJournal(t)
			d, e := j.loadRecovery(scope)
			if e != nil {
				t.Fatal(e)
			}
			rid := d.Operations[string(in.OperationID)]
			switch kind {
			case "missing_operation":
				delete(d.Operations, string(in.OperationID))
			case "missing_idempotency":
				delete(d.Idempotency, string(in.IdempotencyKey))
			case "orphan_record":
				d.Records["orphan"] = d.Records[rid]
			case "scope_mismatch":
				entry := d.Records[rid]
				entry.Record.Scope.LocalScopeID = "other"
				d.Records[rid] = entry
			}
			if e = j.saveRecovery(scope, d); e != nil {
				t.Fatal(e)
			}
			if _, e = j.Get(ctx, port.GetRecoveryRecordInput{Scope: scope, OperationID: &in.OperationID}); !errors.Is(e, port.ErrIndeterminate) {
				t.Fatalf("corruption=%v", e)
			}
		})
	}
	j, in := newJournal(t)
	eval, decision := testDigest('e'), testDigest('f')
	published := runtimeprotocol.CommittedRevisionRef{DocumentID: scope.DocumentID, RevisionID: "published", DefinitionHash: testDigest('1'), GraphHash: testDigest('2')}
	if _, e := j.Advance(ctx, port.AdvanceRecoveryRecordInput{Scope: scope, OperationID: in.OperationID, ExpectedPhase: runtimeprotocol.RecoveryPhasePending, NextPhase: runtimeprotocol.RecoveryPhaseStaged, EvaluationDigest: &eval, DecisionDigest: &decision, PublishedRevision: &published}); !errors.Is(e, port.ErrConflict) {
		t.Fatalf("premature evidence=%v", e)
	}
	if _, e := j.Advance(ctx, port.AdvanceRecoveryRecordInput{Scope: scope, OperationID: in.OperationID, ExpectedPhase: runtimeprotocol.RecoveryPhasePending, NextPhase: runtimeprotocol.RecoveryPhaseStaged, EvaluationDigest: &eval, DecisionDigest: &decision}); e != nil {
		t.Fatal(e)
	}
	if _, e := j.Advance(ctx, port.AdvanceRecoveryRecordInput{Scope: scope, OperationID: in.OperationID, ExpectedPhase: runtimeprotocol.RecoveryPhaseStaged, NextPhase: runtimeprotocol.RecoveryPhasePublicationPending}); e != nil {
		t.Fatal(e)
	}
	if _, e := j.Advance(ctx, port.AdvanceRecoveryRecordInput{Scope: scope, OperationID: in.OperationID, ExpectedPhase: runtimeprotocol.RecoveryPhasePublicationPending, NextPhase: runtimeprotocol.RecoveryPhasePublished}); !errors.Is(e, port.ErrConflict) {
		t.Fatalf("missing evidence=%v", e)
	}
	if _, e := j.Advance(ctx, port.AdvanceRecoveryRecordInput{Scope: scope, OperationID: in.OperationID, ExpectedPhase: runtimeprotocol.RecoveryPhasePublicationPending, NextPhase: runtimeprotocol.RecoveryPhasePublished, PublishedRevision: &published}); e != nil {
		t.Fatal(e)
	}
	other := published
	other.RevisionID = "other"
	if _, e := j.Advance(ctx, port.AdvanceRecoveryRecordInput{Scope: scope, OperationID: in.OperationID, ExpectedPhase: runtimeprotocol.RecoveryPhasePublished, NextPhase: runtimeprotocol.RecoveryPhaseStatePending, PublishedRevision: &other}); !errors.Is(e, port.ErrConflict) {
		t.Fatalf("mismatched evidence=%v", e)
	}
}

func historyMetadata(id, op, when string) runtimeprotocol.RevisionMetadata {
	provider := runtimeprotocol.ProviderVersionToken(id)
	return runtimeprotocol.RevisionMetadata{Revision: runtimeprotocol.CommittedRevisionRef{DocumentID: testScope().DocumentID, RevisionID: runtimeprotocol.RevisionID(id), DefinitionHash: testDigest('b'), GraphHash: testDigest('c'), ProviderVersion: &provider}, OperationID: runtimeprotocol.OperationID(op), CommittedAt: protocolcommon.Rfc3339Time(when), Trigger: runtimeprotocol.CommitTriggerExplicitSave, AuthoringDecisionDigest: testDigest('d')}
}

func TestHistoryByteLimitAndCursorStableAcrossConcurrentInsert(t *testing.T) {
	ctx := context.Background()
	scope := testScope()
	h, e := NewHistoryStore(t.TempDir(), Options{})
	if e != nil {
		t.Fatal(e)
	}
	for _, m := range []runtimeprotocol.RevisionMetadata{historyMetadata("r2", "op2", "2026-07-19T00:00:02Z"), historyMetadata("r1", "op1", "2026-07-19T00:00:01Z")} {
		if _, e = h.AppendRevision(ctx, port.AppendRevisionInput{Scope: scope, Metadata: m}); e != nil {
			t.Fatal(e)
		}
	}
	if _, e = h.ListRevisions(ctx, port.ListRevisionsInput{Scope: scope, MaxItems: "1", MaxOutputBytes: "1"}); !errors.Is(e, port.ErrConflict) {
		t.Fatalf("too-small page=%v", e)
	}
	first, e := h.ListRevisions(ctx, port.ListRevisionsInput{Scope: scope, MaxItems: "1", MaxOutputBytes: "4096"})
	if e != nil || first.Page.NextCursor == nil {
		t.Fatalf("first=%+v err=%v", first, e)
	}
	cursor := runtimeprotocol.RuntimeCursor(*first.Page.NextCursor)
	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		_, _ = h.AppendRevision(ctx, port.AppendRevisionInput{Scope: scope, Metadata: historyMetadata("r3", "op3", "2026-07-19T00:00:03Z")})
	}()
	wg.Wait()
	second, e := h.ListRevisions(ctx, port.ListRevisionsInput{Scope: scope, Cursor: &cursor, MaxItems: "2", MaxOutputBytes: "4096"})
	if e != nil || len(second.Items) != 1 || second.Items[0].Revision.RevisionID != "r1" {
		t.Fatalf("stable page=%+v err=%v", second, e)
	}
}

func TestBootstrapRejectsMismatchedDocumentAndState(t *testing.T) {
	ctx := context.Background()
	scope := testScope()
	provider := runtimeprotocol.ProviderVersionToken("1")
	blob := source("source", []byte("x"))
	revision := runtimeprotocol.CommittedRevisionRef{DocumentID: scope.DocumentID, RevisionID: "base", DefinitionHash: testDigest('b'), GraphHash: testDigest('c'), ProviderVersion: &provider}
	snapshot := port.RevisionSnapshot{Revision: revision, SourceBlobs: []protocolcommon.BlobRef{blob.Ref}, Manifest: protocolcommon.BlobRef{BlobID: "manifest_blob_123", Digest: testDigest('d'), Lifetime: protocolcommon.BlobLifetimeSession, MediaType: "application/json", Size: "1"}}
	d, _ := NewDocumentStore(t.TempDir(), Options{})
	extra := source("extra", []byte("y"))
	if e := d.InitializeDocument(ctx, scope, snapshot, provider, "1", []port.SourceBlob{blob, extra}); !errors.Is(e, port.ErrConflict) {
		t.Fatalf("extra blob=%v", e)
	}
	state, _ := NewStateBackend(t.TempDir(), Options{})
	bad, _ := stateFixture(time.Now().UTC())
	bad.Contents.Digest = "sha256:bad"
	if e := state.InitializeState(ctx, scope, bad); !errors.Is(e, port.ErrConflict) {
		t.Fatalf("invalid state=%v", e)
	}
	good, _ := stateFixture(time.Now().UTC())
	state2, _ := NewStateBackend(t.TempDir(), Options{})
	if e := state2.InitializeState(ctx, scope, good); e != nil {
		t.Fatal(e)
	}
	good.Head.SubjectHashes["ldl:project:fixture:entity:item"] = testDigest('e')
	head, e := state2.GetHead(ctx, port.GetStateHeadInput{Scope: scope})
	if e != nil {
		t.Fatal(e)
	}
	if len(head.SubjectHashes) != 0 {
		t.Fatal("bootstrap retained caller-owned map")
	}
}

func TestSemanticallyCorruptPersistedJSONFailsClosed(t *testing.T) {
	ctx := context.Background()
	scope := testScope()
	t.Run("document", func(t *testing.T) {
		provider := runtimeprotocol.ProviderVersionToken("1")
		blob := source("source", []byte("x"))
		snap := port.RevisionSnapshot{Revision: runtimeprotocol.CommittedRevisionRef{DocumentID: scope.DocumentID, RevisionID: "base", DefinitionHash: testDigest('b'), GraphHash: testDigest('c'), ProviderVersion: &provider}, SourceBlobs: []protocolcommon.BlobRef{blob.Ref}, Manifest: protocolcommon.BlobRef{BlobID: "manifest_blob_123", Digest: testDigest('d'), Lifetime: protocolcommon.BlobLifetimeSession, MediaType: "application/json", Size: "1"}}
		d, _ := NewDocumentStore(t.TempDir(), Options{})
		if e := d.InitializeDocument(ctx, scope, snap, provider, "1", []port.SourceBlob{blob}); e != nil {
			t.Fatal(e)
		}
		dir, _ := d.scopeDir(scope)
		p := filepath.Join(dir, "documents", "head.json")
		var disk documentHeadDisk
		if e := d.readJSON(p, &disk); e != nil {
			t.Fatal(e)
		}
		disk.Head.Revision.DocumentID = "other"
		if e := d.writeJSON(p, disk); e != nil {
			t.Fatal(e)
		}
		if _, e := d.GetHead(ctx, port.GetDocumentHeadInput{Scope: scope}); !errors.Is(e, port.ErrIndeterminate) {
			t.Fatalf("semantic head=%v", e)
		}
	})
	t.Run("state", func(t *testing.T) {
		now := time.Now().UTC()
		s, _ := NewStateBackend(t.TempDir(), Options{})
		snap, _ := stateFixture(now)
		if e := s.InitializeState(ctx, scope, snap); e != nil {
			t.Fatal(e)
		}
		p, _ := s.statePath(scope)
		var disk stateDisk
		if e := s.readJSON(p, &disk); e != nil {
			t.Fatal(e)
		}
		disk.Head.StateVersion = "01"
		disk.Snapshot.Head.StateVersion = "01"
		if e := s.writeJSON(p, disk); e != nil {
			t.Fatal(e)
		}
		if _, e := s.GetHead(ctx, port.GetStateHeadInput{Scope: scope}); !errors.Is(e, port.ErrIndeterminate) {
			t.Fatalf("semantic state=%v", e)
		}
	})
	t.Run("history", func(t *testing.T) {
		h, _ := NewHistoryStore(t.TempDir(), Options{})
		if _, e := h.AppendRevision(ctx, port.AppendRevisionInput{Scope: scope, Metadata: historyMetadata("r1", "op1", "2026-07-19T00:00:00Z")}); e != nil {
			t.Fatal(e)
		}
		p, _ := h.historyPath(scope)
		var disk historyDisk
		if e := h.readJSON(p, &disk); e != nil {
			t.Fatal(e)
		}
		disk.Items[0].Revision.DocumentID = "other"
		if e := h.writeJSON(p, disk); e != nil {
			t.Fatal(e)
		}
		if _, e := h.ListRevisions(ctx, port.ListRevisionsInput{Scope: scope, MaxItems: "1", MaxOutputBytes: "4096"}); !errors.Is(e, port.ErrIndeterminate) {
			t.Fatalf("semantic history=%v", e)
		}
	})
}

func TestFinalizeRejectsMalformedTerminalResult(t *testing.T) {
	ctx := context.Background()
	scope := testScope()
	j, e := NewRecoveryJournal(t.TempDir(), Options{})
	if e != nil {
		t.Fatal(e)
	}
	in := port.CreatePendingRecordInput{Scope: scope, OperationID: "op", IdempotencyKey: "idempotency_key_1", PayloadDigest: testDigest('d'), BaseRevision: runtimeprotocol.CommittedRevisionRef{DocumentID: scope.DocumentID, RevisionID: "base", DefinitionHash: testDigest('b'), GraphHash: testDigest('c')}}
	if _, e = j.CreatePending(ctx, in); e != nil {
		t.Fatal(e)
	}
	if _, e = j.Finalize(ctx, port.FinalizeRecoveryRecordInput{Scope: scope, TerminalPhase: runtimeprotocol.RecoveryPhaseFinal, CommitResult: runtimeprotocol.RuntimeCommitResult{}}); !errors.Is(e, port.ErrConflict) {
		t.Fatalf("malformed finalize=%v", e)
	}
}
