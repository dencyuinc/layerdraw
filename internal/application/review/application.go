// SPDX-License-Identifier: LicenseRef-LayerDraw-1.0

package review

import (
	"context"
	"encoding/json"
	"errors"
	"reflect"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/dencyuinc/layerdraw/gen/go/accessprotocol"
	"github.com/dencyuinc/layerdraw/gen/go/protocolcommon"
	"github.com/dencyuinc/layerdraw/gen/go/runtimeprotocol"
	"github.com/dencyuinc/layerdraw/gen/go/semantic"
)

type Application struct {
	mu      sync.Mutex
	store   Store
	runtime RuntimePort
	access  AccessPort
	now     func() time.Time
	state   Snapshot
}

func New(ctx context.Context, store Store, runtime RuntimePort, access AccessPort, now func() time.Time) (*Application, error) {
	if store == nil || runtime == nil || access == nil {
		return nil, ErrInvalid
	}
	if now == nil {
		now = time.Now
	}
	state, err := store.Load(ctx)
	if err != nil {
		return nil, err
	}
	if state.Version == 0 {
		state.Version = 1
	}
	if err := validateSnapshot(state); err != nil {
		return nil, err
	}
	return &Application{store: store, runtime: runtime, access: access, now: now, state: cloneSnapshot(state)}, nil
}

func (a *Application) Snapshot() Snapshot {
	a.mu.Lock()
	defer a.mu.Unlock()
	return cloneSnapshot(a.state)
}

func (a *Application) Get(id string) (Proposal, error) {
	a.mu.Lock()
	defer a.mu.Unlock()
	index := proposalIndex(a.state.Proposals, id)
	if index < 0 {
		return Proposal{}, ErrNotFound
	}
	return cloneProposal(a.state.Proposals[index]), nil
}

func (a *Application) Create(ctx context.Context, input CreateInput) (Proposal, error) {
	a.mu.Lock()
	defer a.mu.Unlock()
	if input.Now.IsZero() {
		input.Now = a.now().UTC()
	}
	if input.ProposalID == "" || input.Preview.CurrentRevision.DocumentID == "" || input.Preview.OperationBatch.DocumentID != input.Preview.CurrentRevision.DocumentID || !sameRevision(input.Preview.OperationBatch.BaseRevision, input.Preview.CurrentRevision) || input.Preview.Evidence.AuthoringImpact.ImpactDigest == "" || input.Preview.Evidence.AuthoringImpact.ImpactDigest != valueDigest(input.ProposeDecision.AuthoringImpactDigest) || !validDecision(input.ProposeDecision) {
		return Proposal{}, ErrInvalid
	}
	if proposalIndex(a.state.Proposals, input.ProposalID) >= 0 {
		return Proposal{}, ErrConflict
	}
	if input.Proposer.Kind == "agent" {
		if !input.Permissions.Propose || input.DelegationDigest == nil {
			return Proposal{}, ErrDenied
		}
	} else if input.Proposer.Kind != "user" {
		return Proposal{}, ErrInvalid
	}
	if input.ProposeDecision.Outcome == accessprotocol.AuthoringDecisionOutcomeDeny && (input.Proposer.Kind != "agent" || !input.Permissions.Propose) {
		return Proposal{}, ErrDenied
	}
	proposal := Proposal{
		ID: input.ProposalID, Generation: 1, Status: StatusProposed,
		CurrentRevision: input.Preview.CurrentRevision, ProposedDefinitionHash: input.Preview.DefinitionHash,
		ProposedGraphHash: input.Preview.GraphHash, OperationBatch: input.Preview.OperationBatch,
		AuthoringProof: input.Preview.AuthoringProof, Evidence: input.Preview.Evidence, Proposer: input.Proposer,
		AgentDelegationDigest: input.DelegationDigest, AccessEvaluationDigest: input.ProposeDecision.EvaluationDigest,
		AccessDecisionDigest: input.ProposeDecision.DecisionDigest,
		RequiredCapabilities: append([]semantic.AuthoringCapability(nil), input.Preview.Evidence.AuthoringImpact.RequiredCapabilities...),
		Comments:             []Comment{}, CreatedAt: input.Now, UpdatedAt: input.Now,
	}
	next := cloneSnapshot(a.state)
	next.Proposals = append(next.Proposals, proposal)
	sortProposals(next.Proposals)
	if err := a.store.Save(ctx, next); err != nil {
		return Proposal{}, err
	}
	a.state = next
	return cloneProposal(proposal), nil
}

func sameRevision(left, right runtimeprotocol.CommittedRevisionRef) bool {
	if left.DocumentID != right.DocumentID || left.RevisionID != right.RevisionID || left.DefinitionHash != right.DefinitionHash || left.GraphHash != right.GraphHash {
		return false
	}
	if left.ProviderVersion == nil || right.ProviderVersion == nil {
		return left.ProviderVersion == nil && right.ProviderVersion == nil
	}
	return *left.ProviderVersion == *right.ProviderVersion
}

func (a *Application) Comment(ctx context.Context, input CommentInput) (Proposal, error) {
	a.mu.Lock()
	defer a.mu.Unlock()
	index := proposalIndex(a.state.Proposals, input.ProposalID)
	if index < 0 {
		return Proposal{}, ErrNotFound
	}
	current := a.state.Proposals[index]
	if input.Generation != current.Generation || input.CommentID == "" || strings.TrimSpace(input.Body) == "" || !validTarget(input.Target) {
		return Proposal{}, ErrConflict
	}
	for _, item := range current.Comments {
		if item.ID == input.CommentID {
			return Proposal{}, ErrConflict
		}
	}
	if terminal(current.Status) {
		return Proposal{}, ErrTerminal
	}
	now := a.now().UTC()
	comment := Comment{ID: input.CommentID, Author: input.Author, Body: input.Body, Target: input.Target, CreatedAt: now, UpdatedAt: now, BaseRevision: current.CurrentRevision.RevisionID}
	next := cloneSnapshot(a.state)
	proposal := &next.Proposals[index]
	proposal.Comments = append(proposal.Comments, comment)
	proposal.Generation++
	proposal.UpdatedAt = now
	if err := a.store.Save(ctx, next); err != nil {
		return Proposal{}, err
	}
	a.state = next
	return cloneProposal(*proposal), nil
}

func (a *Application) Withdraw(ctx context.Context, id string, generation uint64, actor accessprotocol.ActorRef) (Proposal, error) {
	return a.transition(ctx, id, generation, func(proposal *Proposal) error {
		if proposal.Proposer != actor || terminal(proposal.Status) {
			return ErrDenied
		}
		proposal.Status = StatusWithdrawn
		return nil
	})
}

func (a *Application) Supersede(ctx context.Context, id string, generation uint64) (Proposal, error) {
	return a.transition(ctx, id, generation, func(proposal *Proposal) error {
		if terminal(proposal.Status) {
			return ErrTerminal
		}
		proposal.Status = StatusSuperseded
		return nil
	})
}

func (a *Application) ApproveAndApply(ctx context.Context, input ApprovalInput) (Proposal, error) {
	a.mu.Lock()
	defer a.mu.Unlock()
	index := proposalIndex(a.state.Proposals, input.ProposalID)
	if index < 0 {
		return Proposal{}, ErrNotFound
	}
	current := a.state.Proposals[index]
	if input.Generation != current.Generation {
		return Proposal{}, ErrConflict
	}
	if terminal(current.Status) || current.Status == StatusSuperseded {
		return Proposal{}, ErrTerminal
	}
	if current.Proposer.Kind == "agent" && current.Proposer == input.Approver {
		return Proposal{}, ErrSelfApproval
	}
	if current.Status == StatusApproved {
		return a.commitPending(ctx, index, input)
	}
	repreview, err := a.runtime.Repreview(ctx, RepreviewInput{Session: input.Session, Batch: current.OperationBatch})
	if err != nil {
		if errors.Is(err, context.Canceled) || errors.Is(err, ErrCancelled) {
			return a.recordFailure(ctx, index, StatusNeedsReview, "approval_cancelled", ErrCancelled)
		}
		return a.recordFailure(ctx, index, StatusNeedsReview, "repreview_failed", err)
	}
	if !reflect.DeepEqual(repreview.CurrentRevision, current.CurrentRevision) {
		status := StatusStale
		if repreview.CurrentRevision.DocumentID != current.CurrentRevision.DocumentID {
			status = StatusConflicting
		}
		return a.recordFailure(ctx, index, status, "revision_changed", ErrStale)
	}
	// AuthoringImpact is the Engine-owned closure over semantic and source diff
	// hashes. Repreview adapters need not duplicate the full presentation diff
	// merely to prove the same mutation at approval time.
	if repreview.Evidence.SemanticDiff.Digest != current.Evidence.SemanticDiff.Digest || repreview.Evidence.AuthoringImpact.ImpactDigest != current.Evidence.AuthoringImpact.ImpactDigest || repreview.DefinitionHash != current.ProposedDefinitionHash || repreview.GraphHash != current.ProposedGraphHash {
		return a.recordFailure(ctx, index, StatusConflicting, "proposal_changed", ErrConflict)
	}
	decision, err := a.access.AuthorizeApprover(ctx, ApprovalRequest{Approver: input.Approver, Revision: repreview.CurrentRevision, Impact: repreview.Evidence.AuthoringImpact, Decision: repreview.PreviewDecision})
	if err != nil {
		if errors.Is(err, context.Canceled) || errors.Is(err, ErrCancelled) {
			return a.recordFailure(ctx, index, StatusNeedsReview, "approval_cancelled", ErrCancelled)
		}
		return a.recordFailure(ctx, index, StatusDenied, "approval_denied", err)
	}
	if decision.Outcome != accessprotocol.AuthoringDecisionOutcomeAllow || !sameCapabilities(decision.RequiredCapabilities, current.RequiredCapabilities) {
		return a.recordFailure(ctx, index, StatusDenied, "approver_insufficient", ErrDenied)
	}
	next := cloneSnapshot(a.state)
	pending := &next.Proposals[index]
	pending.UpdatedAt = a.now().UTC()
	pending.ApprovedBy = &input.Approver
	pending.AccessEvaluationDigest = decision.EvaluationDigest
	pending.AccessDecisionDigest = decision.DecisionDigest
	pending.Status = StatusApproved
	pending.PendingCommit = &PendingCommit{
		OperationID: input.OperationID, IdempotencyKey: input.IdempotencyKey,
		OperationBatch: repreview.OperationBatch, AuthoringProof: repreview.AuthoringProof,
		Approver: input.Approver, AccessEvaluationDigest: decision.EvaluationDigest,
		AccessDecisionDigest: decision.DecisionDigest, Trigger: input.Trigger,
	}
	if err := a.store.Save(ctx, next); err != nil {
		return Proposal{}, err
	}
	a.state = next
	return a.commitPending(ctx, index, input)
}

func (a *Application) commitPending(ctx context.Context, index int, input ApprovalInput) (Proposal, error) {
	current := a.state.Proposals[index]
	pending := current.PendingCommit
	if pending == nil || pending.OperationID == "" || pending.IdempotencyKey == "" || pending.OperationID != input.OperationID || pending.IdempotencyKey != input.IdempotencyKey || pending.Approver != input.Approver || input.Session.Scope.DocumentID == "" || input.Session.Scope.DocumentID != pending.OperationBatch.DocumentID || pending.OperationBatch.DocumentID != current.CurrentRevision.DocumentID {
		return Proposal{}, ErrConflict
	}
	commit := runtimeprotocol.RuntimeCommitInput{Session: input.Session, OperationBatch: pending.OperationBatch, AuthoringProof: pending.AuthoringProof, OperationID: pending.OperationID, IdempotencyKey: pending.IdempotencyKey, Trigger: pending.Trigger, CancellationToken: input.Cancellation}
	result, err := a.runtime.Commit(ctx, commit)
	if err != nil {
		if errors.Is(err, context.Canceled) || errors.Is(err, ErrCancelled) {
			return a.recordFailure(ctx, index, StatusNeedsReview, "approval_cancelled", ErrCancelled)
		}
		return a.recordFailure(ctx, index, StatusNeedsReview, "commit_failed", err)
	}
	next := cloneSnapshot(a.state)
	proposal := &next.Proposals[index]
	proposal.Generation++
	proposal.UpdatedAt = a.now().UTC()
	proposal.ApprovedBy = &pending.Approver
	proposal.AccessEvaluationDigest = pending.AccessEvaluationDigest
	proposal.AccessDecisionDigest = pending.AccessDecisionDigest
	proposal.Status = StatusApproved
	proposal.PendingCommit = nil
	switch result.OperationResult.Status {
	case runtimeprotocol.OperationResultStatusCommitted, runtimeprotocol.OperationResultStatusCommittedExternalFailed, runtimeprotocol.OperationResultStatusCommittedExternalPending, runtimeprotocol.OperationResultStatusCommittedStateStale:
		if result.OperationResult.CommittedRevision == nil {
			return Proposal{}, ErrInvalid
		}
		proposal.Status = StatusApplied
		proposal.CommittedRevision = result.OperationResult.CommittedRevision
	case runtimeprotocol.OperationResultStatusNeedsReview:
		proposal.Status = StatusNeedsReview
		proposal.LastFailure = "runtime_needs_review"
	case runtimeprotocol.OperationResultStatusRejected:
		proposal.Status = StatusDenied
		proposal.LastFailure = "runtime_rejected"
	default:
		return Proposal{}, ErrInvalid
	}
	if err := a.store.Save(ctx, next); err != nil {
		return Proposal{}, err
	}
	a.state = next
	return cloneProposal(*proposal), nil
}

func (a *Application) transition(ctx context.Context, id string, generation uint64, mutate func(*Proposal) error) (Proposal, error) {
	a.mu.Lock()
	defer a.mu.Unlock()
	index := proposalIndex(a.state.Proposals, id)
	if index < 0 {
		return Proposal{}, ErrNotFound
	}
	next := cloneSnapshot(a.state)
	proposal := &next.Proposals[index]
	if proposal.Generation != generation {
		return Proposal{}, ErrConflict
	}
	if err := mutate(proposal); err != nil {
		return Proposal{}, err
	}
	proposal.Generation++
	proposal.UpdatedAt = a.now().UTC()
	if err := a.store.Save(ctx, next); err != nil {
		return Proposal{}, err
	}
	a.state = next
	return cloneProposal(*proposal), nil
}

func (a *Application) recordFailure(ctx context.Context, index int, status Status, code string, cause error) (Proposal, error) {
	next := cloneSnapshot(a.state)
	proposal := &next.Proposals[index]
	proposal.Status = status
	proposal.LastFailure = code
	proposal.PendingCommit = nil
	proposal.Generation++
	proposal.UpdatedAt = a.now().UTC()
	markStaleComments(proposal)
	persistCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
	defer cancel()
	if err := a.store.Save(persistCtx, next); err != nil {
		return Proposal{}, err
	}
	a.state = next
	return cloneProposal(*proposal), cause
}

func markStaleComments(proposal *Proposal) {
	for index := range proposal.Comments {
		if proposal.Comments[index].Target.Kind == TargetDiffEntry || proposal.Comments[index].Target.Kind == TargetSourceScope {
			proposal.Comments[index].Stale = true
		}
	}
}

func validDecision(value accessprotocol.AuthoringDecision) bool {
	_, err := accessprotocol.EncodeAuthoringDecision(value)
	return err == nil
}
func valueDigest(value *protocolcommon.Digest) protocolcommon.Digest {
	if value == nil {
		return ""
	}
	return *value
}
func proposalIndex(values []Proposal, id string) int {
	for index := range values {
		if values[index].ID == id {
			return index
		}
	}
	return -1
}
func sortProposals(values []Proposal) {
	sort.Slice(values, func(i, j int) bool { return values[i].ID < values[j].ID })
}
func terminal(status Status) bool { return status == StatusWithdrawn || status == StatusApplied }
func sameCapabilities(left, right []semantic.AuthoringCapability) bool {
	if len(left) != len(right) {
		return false
	}
	for i := range left {
		if left[i] != right[i] {
			return false
		}
	}
	return true
}
func validTarget(target Target) bool {
	switch target.Kind {
	case TargetProject:
		return target.StableAddress == nil && target.SourceRange == nil && target.DiffKey == ""
	case TargetView, TargetEntity, TargetRelation:
		return target.StableAddress != nil && target.SourceRange == nil && target.DiffKey == ""
	case TargetSourceScope:
		return target.SourceRange != nil && target.StableAddress == nil && target.DiffKey == ""
	case TargetDiffEntry:
		return target.DiffKey != "" && target.StableAddress == nil && target.SourceRange == nil
	default:
		return false
	}
}
func validateSnapshot(snapshot Snapshot) error {
	if snapshot.Version != 1 {
		return ErrInvalid
	}
	seen := map[string]bool{}
	for _, proposal := range snapshot.Proposals {
		pending := proposal.PendingCommit != nil
		if proposal.ID == "" || proposal.Generation == 0 || seen[proposal.ID] || pending != (proposal.Status == StatusApproved) || (pending && (proposal.PendingCommit.OperationID == "" || proposal.PendingCommit.IdempotencyKey == "" || proposal.ApprovedBy == nil || proposal.PendingCommit.Approver != *proposal.ApprovedBy || proposal.PendingCommit.OperationBatch.DocumentID != proposal.CurrentRevision.DocumentID)) {
			return ErrInvalid
		}
		seen[proposal.ID] = true
	}
	return nil
}
func cloneSnapshot(value Snapshot) Snapshot {
	data, _ := json.Marshal(value)
	var result Snapshot
	_ = json.Unmarshal(data, &result)
	if result.Proposals == nil {
		result.Proposals = []Proposal{}
	}
	return result
}
func cloneProposal(value Proposal) Proposal {
	return cloneSnapshot(Snapshot{Version: 1, Proposals: []Proposal{value}}).Proposals[0]
}
