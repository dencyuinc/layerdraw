// SPDX-License-Identifier: LicenseRef-LayerDraw-1.0

package review

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/dencyuinc/layerdraw/gen/go/accessprotocol"
	"github.com/dencyuinc/layerdraw/gen/go/engineprotocol"
	"github.com/dencyuinc/layerdraw/gen/go/protocolcommon"
	"github.com/dencyuinc/layerdraw/gen/go/runtimeprotocol"
	"github.com/dencyuinc/layerdraw/gen/go/semantic"
	accesscore "github.com/dencyuinc/layerdraw/internal/access"
)

type storeStub struct {
	snapshot         Snapshot
	loadErr, saveErr error
}

type crashAfterCommitStore struct {
	snapshot Snapshot
	saves    int
}

func (s *crashAfterCommitStore) Load(context.Context) (Snapshot, error) {
	return cloneSnapshot(s.snapshot), nil
}
func (s *crashAfterCommitStore) Save(_ context.Context, state Snapshot) error {
	s.saves++
	if s.saves == 3 {
		return errors.New("simulated crash before Review finalization")
	}
	s.snapshot = cloneSnapshot(state)
	return nil
}

type contextObservingStore struct {
	*MemoryStore
	lastSaveContextErr error
}

func (s *contextObservingStore) Save(ctx context.Context, state Snapshot) error {
	s.lastSaveContextErr = ctx.Err()
	return s.MemoryStore.Save(ctx, state)
}

func (s storeStub) Load(context.Context) (Snapshot, error) { return s.snapshot, s.loadErr }
func (s storeStub) Save(context.Context, Snapshot) error   { return s.saveErr }

type runtimeStub struct {
	preview               RepreviewResult
	commit                runtimeprotocol.RuntimeCommitResult
	previewErr, commitErr error
	commits               []runtimeprotocol.RuntimeCommitInput
}

func (s *runtimeStub) Repreview(context.Context, RepreviewInput) (RepreviewResult, error) {
	return s.preview, s.previewErr
}
func (s *runtimeStub) Commit(_ context.Context, input runtimeprotocol.RuntimeCommitInput) (runtimeprotocol.RuntimeCommitResult, error) {
	s.commits = append(s.commits, input)
	return s.commit, s.commitErr
}

type accessStub struct {
	decision accessprotocol.AuthoringDecision
	err      error
	calls    int
}

func (s *accessStub) AuthorizeApprover(context.Context, ApprovalRequest) (accessprotocol.AuthoringDecision, error) {
	s.calls++
	return s.decision, s.err
}

func testDigest(char byte) protocolcommon.Digest {
	return protocolcommon.Digest("sha256:" + strings.Repeat(string(char), 64))
}

func fixture() (RepreviewResult, accessprotocol.AuthoringDecision) {
	digest := testDigest('a')
	revision := runtimeprotocol.CommittedRevisionRef{DocumentID: "doc", RevisionID: "revision_abcdefghijklmnop", DefinitionHash: digest, GraphHash: digest}
	impact := semantic.AuthoringImpact{BaseDefinitionHash: digest, Entries: []semantic.AuthoringImpactEntry{}, ImpactDigest: digest, RequiredCapabilities: []semantic.AuthoringCapability{}, ResultingDefinitionHash: digest, SemanticDiffHash: digest, SourceDiffHash: digest}
	decision := accessprotocol.AuthoringDecision{AccessFingerprint: digest, ApprovalRuleRefs: []string{}, AuthoringImpactDigest: &impact.ImpactDigest, ConstraintViolations: []accessprotocol.ConstraintViolation{}, DecisionDigest: digest, Diagnostics: []protocolcommon.ProtocolDiagnostic{}, EvaluationDigest: digest, HostOperationImpactDigests: []protocolcommon.Digest{}, MissingCapabilities: []semantic.AuthoringCapability{}, Outcome: accessprotocol.AuthoringDecisionOutcomeAllow, RequiredCapabilities: []semantic.AuthoringCapability{}}
	preview := RepreviewResult{CurrentRevision: revision, DefinitionHash: digest, GraphHash: digest, OperationBatch: runtimeprotocol.RuntimeOperationBatch{DocumentID: revision.DocumentID, BaseRevision: revision, ExpectedDefinitionHash: digest, Operations: engineprotocol.SemanticOperationBatch{}, Preconditions: engineprotocol.EngineEditPreconditions{}}, AuthoringProof: runtimeprotocol.AuthoringProof{AccessFingerprint: digest, BaseRevision: revision, DecisionDigest: digest, EvaluationDigest: digest, PolicyRefs: []accessprotocol.PolicyRef{}}, PreviewDecision: decision, Evidence: Evidence{SemanticDiff: semantic.SemanticDiff{Digest: digest, Entries: []semantic.SemanticDiffEntry{}}, SourceDiff: engineprotocol.SourceDiff{Digest: digest, Edits: []engineprotocol.SourceEdit{}}, AuthoringImpact: impact, Diagnostics: []semantic.Diagnostic{}, AffectedUsages: []semantic.StableAddress{}, AffectedRows: []semantic.StableAddress{}, AffectedViews: []semantic.StableAddress{}, RenderPreviews: []ArtifactPreview{}}}
	return preview, decision
}

func newApplication(t *testing.T, store Store, runtime *runtimeStub, access *accessStub) *Application {
	t.Helper()
	app, err := New(context.Background(), store, runtime, access, func() time.Time { return time.Date(2026, 7, 20, 1, 2, 3, 0, time.UTC) })
	if err != nil {
		t.Fatal(err)
	}
	return app
}

func createProposal(t *testing.T, app *Application, id string, actor accessprotocol.ActorRef, permissions accesscore.AgentPermissions, delegation *protocolcommon.Digest, preview RepreviewResult, decision accessprotocol.AuthoringDecision) Proposal {
	t.Helper()
	proposal, err := app.Create(context.Background(), CreateInput{ProposalID: id, Preview: preview, Proposer: actor, Permissions: permissions, DelegationDigest: delegation, ProposeDecision: decision})
	if err != nil {
		t.Fatal(err)
	}
	return proposal
}

func TestConcurrentHumanAndProposeOnlyAgentProposals(t *testing.T) {
	preview, decision := fixture()
	runtime, access := &runtimeStub{preview: preview}, &accessStub{decision: decision}
	app := newApplication(t, NewMemoryStore(), runtime, access)
	createProposal(t, app, "human", accessprotocol.ActorRef{ActorID: "owner", Kind: "user"}, accesscore.AgentPermissions{}, nil, preview, decision)
	delegation := testDigest('d')
	agent := accessprotocol.ActorRef{ActorID: "agent", Kind: "agent"}
	createProposal(t, app, "agent", agent, accesscore.AgentPermissions{Propose: true}, &delegation, preview, decision)
	if _, err := app.Create(context.Background(), CreateInput{ProposalID: "agent", Preview: preview, Proposer: agent, Permissions: accesscore.AgentPermissions{Propose: true}, DelegationDigest: &delegation, ProposeDecision: decision}); !errors.Is(err, ErrConflict) {
		t.Fatalf("duplicate=%v", err)
	}
	proposal, _ := app.Get("agent")
	if _, err := app.ApproveAndApply(context.Background(), ApprovalInput{ProposalID: proposal.ID, Generation: proposal.Generation, Approver: agent}); !errors.Is(err, ErrSelfApproval) {
		t.Fatalf("self approval=%v", err)
	}
}

func TestApprovalReevaluatesCurrentAccessAndMarksPolicyDenial(t *testing.T) {
	preview, decision := fixture()
	runtime := &runtimeStub{preview: preview}
	access := &accessStub{decision: decision, err: ErrDenied}
	app := newApplication(t, NewMemoryStore(), runtime, access)
	proposal := createProposal(t, app, "policy", accessprotocol.ActorRef{ActorID: "author", Kind: "user"}, accesscore.AgentPermissions{}, nil, preview, decision)
	got, err := app.ApproveAndApply(context.Background(), ApprovalInput{ProposalID: proposal.ID, Generation: proposal.Generation, Approver: accessprotocol.ActorRef{ActorID: "approver", Kind: "user"}})
	if !errors.Is(err, ErrDenied) || got.Status != StatusDenied || got.LastFailure != "approval_denied" || access.calls != 1 {
		t.Fatalf("got=%+v err=%v calls=%d", got, err, access.calls)
	}
}

func TestStaleRevisionPreservesProjectCommentsAndStalesAnchoredComments(t *testing.T) {
	preview, decision := fixture()
	runtime, access := &runtimeStub{preview: preview}, &accessStub{decision: decision}
	app := newApplication(t, NewMemoryStore(), runtime, access)
	proposal := createProposal(t, app, "stale", accessprotocol.ActorRef{ActorID: "author", Kind: "user"}, accesscore.AgentPermissions{}, nil, preview, decision)
	proposal, _ = app.Comment(context.Background(), CommentInput{ProposalID: proposal.ID, Generation: proposal.Generation, CommentID: "project", Author: proposal.Proposer, Body: "overall", Target: Target{Kind: TargetProject}})
	proposal, _ = app.Comment(context.Background(), CommentInput{ProposalID: proposal.ID, Generation: proposal.Generation, CommentID: "diff", Author: proposal.Proposer, Body: "line", Target: Target{Kind: TargetDiffEntry, DiffKey: "entry"}})
	runtime.preview.CurrentRevision.RevisionID = "revision_bcdefghijklmnopq"
	got, err := app.ApproveAndApply(context.Background(), ApprovalInput{ProposalID: proposal.ID, Generation: proposal.Generation, Approver: accessprotocol.ActorRef{ActorID: "approver", Kind: "user"}})
	if !errors.Is(err, ErrStale) || got.Status != StatusStale || got.Comments[0].Stale || !got.Comments[1].Stale {
		t.Fatalf("got=%+v err=%v", got, err)
	}
}

func TestApprovalCancellationIsExplicitAndRecoverable(t *testing.T) {
	preview, decision := fixture()
	runtime, access := &runtimeStub{preview: preview, previewErr: context.Canceled}, &accessStub{decision: decision}
	app := newApplication(t, NewMemoryStore(), runtime, access)
	proposal := createProposal(t, app, "cancel", accessprotocol.ActorRef{ActorID: "author", Kind: "user"}, accesscore.AgentPermissions{}, nil, preview, decision)
	got, err := app.ApproveAndApply(context.Background(), ApprovalInput{ProposalID: proposal.ID, Generation: proposal.Generation, Approver: accessprotocol.ActorRef{ActorID: "approver", Kind: "user"}})
	if !errors.Is(err, ErrCancelled) || got.Status != StatusNeedsReview || got.LastFailure != "approval_cancelled" {
		t.Fatalf("got=%+v err=%v", got, err)
	}
}

func TestApprovalCancellationPersistsWithDetachedBoundedContext(t *testing.T) {
	preview, decision := fixture()
	runtime, access := &runtimeStub{preview: preview, previewErr: context.Canceled}, &accessStub{decision: decision}
	store := &contextObservingStore{MemoryStore: NewMemoryStore()}
	app := newApplication(t, store, runtime, access)
	proposal := createProposal(t, app, "cancel-detached", accessprotocol.ActorRef{ActorID: "author", Kind: "user"}, accesscore.AgentPermissions{}, nil, preview, decision)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	got, err := app.ApproveAndApply(ctx, ApprovalInput{ProposalID: proposal.ID, Generation: proposal.Generation, Approver: accessprotocol.ActorRef{ActorID: "approver", Kind: "user"}})
	if !errors.Is(err, ErrCancelled) || got.LastFailure != "approval_cancelled" || store.lastSaveContextErr != nil {
		t.Fatalf("got=%+v err=%v persisted_context_err=%v", got, err, store.lastSaveContextErr)
	}
}

func TestApprovalReplaysPendingRuntimeCommitAfterFinalSaveCrash(t *testing.T) {
	preview, decision := fixture()
	committed := preview.CurrentRevision
	committed.RevisionID = "revision_cdefghijklmnopqr"
	runtime := &runtimeStub{preview: preview, commit: runtimeprotocol.RuntimeCommitResult{OperationResult: runtimeprotocol.OperationResult{Status: runtimeprotocol.OperationResultStatusCommitted, CommittedRevision: &committed}}}
	access := &accessStub{decision: decision}
	store := &crashAfterCommitStore{}
	app := newApplication(t, store, runtime, access)
	proposal := createProposal(t, app, "replay", accessprotocol.ActorRef{ActorID: "author", Kind: "user"}, accesscore.AgentPermissions{}, nil, preview, decision)
	input := ApprovalInput{ProposalID: proposal.ID, Generation: proposal.Generation, Approver: accessprotocol.ActorRef{ActorID: "approver", Kind: "user"}, OperationID: "operation", IdempotencyKey: "idempotency", Trigger: runtimeprotocol.CommitTriggerExplicitSave}
	if _, err := app.ApproveAndApply(context.Background(), input); err == nil {
		t.Fatal("expected final Review save failure")
	}
	if store.snapshot.Proposals[0].Status != StatusApproved || store.snapshot.Proposals[0].PendingOperationID != input.OperationID {
		t.Fatalf("pending approval was not durable: %+v", store.snapshot.Proposals[0])
	}
	restarted := newApplication(t, store, runtime, access)
	got, err := restarted.ApproveAndApply(context.Background(), input)
	if err != nil || got.Status != StatusApplied || len(runtime.commits) != 2 || runtime.commits[0].OperationID != runtime.commits[1].OperationID || runtime.commits[0].IdempotencyKey != runtime.commits[1].IdempotencyKey {
		t.Fatalf("got=%+v err=%v commits=%+v", got, err, runtime.commits)
	}
}

func TestApprovalRepreviewCommitsAtomicallyAndFileStoreRestarts(t *testing.T) {
	preview, decision := fixture()
	committed := preview.CurrentRevision
	committed.RevisionID = "revision_cdefghijklmnopqr"
	runtime := &runtimeStub{preview: preview, commit: runtimeprotocol.RuntimeCommitResult{OperationResult: runtimeprotocol.OperationResult{Status: runtimeprotocol.OperationResultStatusCommitted, CommittedRevision: &committed}}}
	access := &accessStub{decision: decision}
	store, err := NewFileStore(filepath.Join(t.TempDir(), "review"))
	if err != nil {
		t.Fatal(err)
	}
	app := newApplication(t, store, runtime, access)
	proposal := createProposal(t, app, "apply", accessprotocol.ActorRef{ActorID: "author", Kind: "user"}, accesscore.AgentPermissions{}, nil, preview, decision)
	got, err := app.ApproveAndApply(context.Background(), ApprovalInput{ProposalID: proposal.ID, Generation: proposal.Generation, Approver: accessprotocol.ActorRef{ActorID: "approver", Kind: "user"}, OperationID: "op", IdempotencyKey: "idem", Trigger: runtimeprotocol.CommitTriggerExplicitSave})
	if err != nil || got.Status != StatusApplied || len(runtime.commits) != 1 || runtime.commits[0].AuthoringProof.DecisionDigest != preview.AuthoringProof.DecisionDigest {
		t.Fatalf("got=%+v err=%v commits=%d", got, err, len(runtime.commits))
	}
	restarted := newApplication(t, store, runtime, access)
	loaded, err := restarted.Get("apply")
	if err != nil || loaded.Status != StatusApplied || loaded.CommittedRevision == nil || loaded.CommittedRevision.RevisionID != committed.RevisionID {
		t.Fatalf("loaded=%+v err=%v", loaded, err)
	}
}

func TestProposalTransitionsAndTargetValidation(t *testing.T) {
	preview, decision := fixture()
	runtime, access := &runtimeStub{preview: preview}, &accessStub{decision: decision}
	app := newApplication(t, NewMemoryStore(), runtime, access)
	actor := accessprotocol.ActorRef{ActorID: "author", Kind: "user"}
	proposal := createProposal(t, app, "transition", actor, accesscore.AgentPermissions{}, nil, preview, decision)
	if _, err := app.Comment(context.Background(), CommentInput{ProposalID: proposal.ID, Generation: proposal.Generation, CommentID: "", Body: "bad", Target: Target{Kind: TargetProject}}); !errors.Is(err, ErrConflict) {
		t.Fatalf("invalid comment=%v", err)
	}
	for _, target := range []Target{{Kind: TargetView, StableAddress: pointer(semantic.StableAddress("project:p/view:v"))}, {Kind: TargetEntity, StableAddress: pointer(semantic.StableAddress("project:p/entity:e"))}, {Kind: TargetRelation, StableAddress: pointer(semantic.StableAddress("project:p/relation:r"))}, {Kind: TargetSourceScope, SourceRange: &semantic.SourceRange{}}, {Kind: TargetDiffEntry, DiffKey: "entry"}} {
		proposal, _ = app.Comment(context.Background(), CommentInput{ProposalID: proposal.ID, Generation: proposal.Generation, CommentID: string(target.Kind), Author: actor, Body: "comment", Target: target})
	}
	if _, err := app.Withdraw(context.Background(), proposal.ID, proposal.Generation, accessprotocol.ActorRef{ActorID: "other", Kind: "user"}); !errors.Is(err, ErrDenied) {
		t.Fatalf("foreign withdraw=%v", err)
	}
	withdrawn, err := app.Withdraw(context.Background(), proposal.ID, proposal.Generation, actor)
	if err != nil || withdrawn.Status != StatusWithdrawn {
		t.Fatalf("withdrawn=%+v err=%v", withdrawn, err)
	}
	if _, err := app.Comment(context.Background(), CommentInput{ProposalID: withdrawn.ID, Generation: withdrawn.Generation, CommentID: "late", Body: "late", Target: Target{Kind: TargetProject}}); !errors.Is(err, ErrTerminal) {
		t.Fatalf("late comment=%v", err)
	}
	second := createProposal(t, app, "supersede", actor, accesscore.AgentPermissions{}, nil, preview, decision)
	if _, err := app.Supersede(context.Background(), second.ID, second.Generation+1); !errors.Is(err, ErrConflict) {
		t.Fatalf("generation=%v", err)
	}
	superseded, err := app.Supersede(context.Background(), second.ID, second.Generation)
	if err != nil || superseded.Status != StatusSuperseded {
		t.Fatalf("superseded=%+v err=%v", superseded, err)
	}
	if len(app.Snapshot().Proposals) != 2 {
		t.Fatal("snapshot clone lost proposals")
	}
	if _, err := app.Get("absent"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("missing=%v", err)
	}
}

func pointer[T any](value T) *T { return &value }

func TestApprovalConflictDenialAndRuntimeRecoveryStatuses(t *testing.T) {
	preview, decision := fixture()
	actor, approver := accessprotocol.ActorRef{ActorID: "author", Kind: "user"}, accessprotocol.ActorRef{ActorID: "approver", Kind: "user"}
	tests := []struct {
		name    string
		mutate  func(*runtimeStub, *accessStub)
		want    Status
		wantErr error
	}{
		{name: "semantic conflict", mutate: func(r *runtimeStub, _ *accessStub) { r.preview.Evidence.SemanticDiff.Digest = testDigest('b') }, want: StatusConflicting, wantErr: ErrConflict},
		{name: "insufficient approver", mutate: func(_ *runtimeStub, a *accessStub) {
			a.decision.RequiredCapabilities = []semantic.AuthoringCapability{semantic.AuthoringCapabilityGraphWrite}
		}, want: StatusDenied, wantErr: ErrDenied},
		{name: "commit failure", mutate: func(r *runtimeStub, _ *accessStub) { r.commitErr = errors.New("disk") }, want: StatusNeedsReview, wantErr: errors.New("disk")},
		{name: "runtime needs review", mutate: func(r *runtimeStub, _ *accessStub) {
			r.commit.OperationResult.Status = runtimeprotocol.OperationResultStatusNeedsReview
		}, want: StatusNeedsReview},
		{name: "runtime rejected", mutate: func(r *runtimeStub, _ *accessStub) {
			r.commit.OperationResult.Status = runtimeprotocol.OperationResultStatusRejected
		}, want: StatusDenied},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			runtime, access := &runtimeStub{preview: preview}, &accessStub{decision: decision}
			test.mutate(runtime, access)
			app := newApplication(t, NewMemoryStore(), runtime, access)
			proposal := createProposal(t, app, "proposal", actor, accesscore.AgentPermissions{}, nil, preview, decision)
			got, err := app.ApproveAndApply(context.Background(), ApprovalInput{ProposalID: proposal.ID, Generation: proposal.Generation, Approver: approver, OperationID: "operation", IdempotencyKey: "idempotency"})
			if got.Status != test.want {
				t.Fatalf("status=%s err=%v", got.Status, err)
			}
			if test.wantErr == nil && err != nil {
				t.Fatal(err)
			}
			if test.wantErr != nil && err == nil {
				t.Fatalf("expected error %v", test.wantErr)
			}
		})
	}
}

func TestConstructionAndStoresFailClosed(t *testing.T) {
	preview, decision := fixture()
	runtime, access := &runtimeStub{preview: preview}, &accessStub{decision: decision}
	if _, err := New(context.Background(), nil, runtime, access, nil); !errors.Is(err, ErrInvalid) {
		t.Fatalf("nil store=%v", err)
	}
	if _, err := New(context.Background(), storeStub{loadErr: errors.New("load")}, runtime, access, nil); err == nil {
		t.Fatal("load error accepted")
	}
	if _, err := New(context.Background(), storeStub{snapshot: Snapshot{Version: 2}}, runtime, access, nil); !errors.Is(err, ErrInvalid) {
		t.Fatalf("version=%v", err)
	}
	if _, err := NewFileStore("relative"); !errors.Is(err, ErrInvalid) {
		t.Fatalf("relative=%v", err)
	}
	root := filepath.Join(t.TempDir(), "state")
	store, err := NewFileStore(root)
	if err != nil {
		t.Fatal(err)
	}
	if snapshot, err := store.Load(context.Background()); err != nil || len(snapshot.Proposals) != 0 {
		t.Fatalf("empty=%+v err=%v", snapshot, err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := store.Save(ctx, Snapshot{Version: 1, Proposals: []Proposal{}}); !errors.Is(err, context.Canceled) {
		t.Fatalf("cancel=%v", err)
	}
	path := filepath.Join(root, "review-proposals.json")
	if err := os.WriteFile(path, []byte(`{"version":1,"proposals":[],"unknown":true}`), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := store.Load(context.Background()); !errors.Is(err, ErrInvalid) {
		t.Fatalf("unknown=%v", err)
	}
	if err := os.WriteFile(path, []byte(`{"version":1,"proposals":[]}`), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(path, 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := store.Load(context.Background()); !errors.Is(err, ErrInvalid) {
		t.Fatalf("permissions=%v", err)
	}
	if err := os.Remove(path); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(path, 0o700); err != nil {
		t.Fatal(err)
	}
	if _, err := store.Load(context.Background()); !errors.Is(err, ErrInvalid) {
		t.Fatalf("directory=%v", err)
	}
}

func TestInvalidAndPersistenceFailureEdges(t *testing.T) {
	preview, decision := fixture()
	runtime, access := &runtimeStub{preview: preview}, &accessStub{decision: decision}
	app := newApplication(t, NewMemoryStore(), runtime, access)
	agent := accessprotocol.ActorRef{ActorID: "agent", Kind: "agent"}
	if _, err := app.Create(context.Background(), CreateInput{ProposalID: "agent", Preview: preview, Proposer: agent, ProposeDecision: decision}); !errors.Is(err, ErrDenied) {
		t.Fatalf("undelegated=%v", err)
	}
	if _, err := app.Create(context.Background(), CreateInput{ProposalID: "service", Preview: preview, Proposer: accessprotocol.ActorRef{ActorID: "svc", Kind: "service"}, ProposeDecision: decision}); !errors.Is(err, ErrInvalid) {
		t.Fatalf("actor kind=%v", err)
	}
	invalid := preview
	invalid.OperationBatch.DocumentID = "other"
	if _, err := app.Create(context.Background(), CreateInput{ProposalID: "invalid", Preview: invalid, Proposer: accessprotocol.ActorRef{ActorID: "user", Kind: "user"}, ProposeDecision: decision}); !errors.Is(err, ErrInvalid) {
		t.Fatalf("binding=%v", err)
	}
	if _, err := app.Comment(context.Background(), CommentInput{ProposalID: "absent"}); !errors.Is(err, ErrNotFound) {
		t.Fatalf("comment absent=%v", err)
	}
	if _, err := app.Withdraw(context.Background(), "absent", 1, accessprotocol.ActorRef{}); !errors.Is(err, ErrNotFound) {
		t.Fatalf("withdraw absent=%v", err)
	}
	if _, err := app.ApproveAndApply(context.Background(), ApprovalInput{ProposalID: "absent"}); !errors.Is(err, ErrNotFound) {
		t.Fatalf("approve absent=%v", err)
	}
	failing := newApplication(t, storeStub{snapshot: Snapshot{Version: 1, Proposals: []Proposal{}}, saveErr: errors.New("save")}, runtime, access)
	if _, err := failing.Create(context.Background(), CreateInput{ProposalID: "save", Preview: preview, Proposer: accessprotocol.ActorRef{ActorID: "user", Kind: "user"}, ProposeDecision: decision}); err == nil {
		t.Fatal("save failure accepted")
	}
	root := filepath.Join(t.TempDir(), "state")
	store, _ := NewFileStore(root)
	path := filepath.Join(root, "review-proposals.json")
	if err := os.WriteFile(path, []byte(`{"version":1,"proposals":[]} trailing`), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := store.Load(context.Background()); !errors.Is(err, ErrInvalid) {
		t.Fatalf("trailing=%v", err)
	}
}
