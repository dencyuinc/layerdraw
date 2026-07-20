// SPDX-License-Identifier: LicenseRef-LayerDraw-1.0

// Package review owns durable review sessions, proposal lifecycle, comments,
// annotations, and approve/apply orchestration. It consumes Engine, Access, and
// Runtime results without reimplementing their semantic or policy decisions.
package review

import (
	"context"
	"errors"
	"time"

	"github.com/dencyuinc/layerdraw/gen/go/accessprotocol"
	"github.com/dencyuinc/layerdraw/gen/go/engineprotocol"
	"github.com/dencyuinc/layerdraw/gen/go/protocolcommon"
	"github.com/dencyuinc/layerdraw/gen/go/runtimeprotocol"
	"github.com/dencyuinc/layerdraw/gen/go/semantic"
	accesscore "github.com/dencyuinc/layerdraw/internal/access"
)

var (
	ErrInvalid      = errors.New("review: invalid input")
	ErrNotFound     = errors.New("review: not found")
	ErrConflict     = errors.New("review: conflict")
	ErrDenied       = errors.New("review: denied")
	ErrStale        = errors.New("review: stale")
	ErrCancelled    = errors.New("review: cancelled")
	ErrTerminal     = errors.New("review: terminal state")
	ErrSuperseded   = errors.New("review: superseded")
	ErrSelfApproval = errors.New("review: agent cannot self approve")
)

type Status string

const (
	StatusProposed    Status = "proposed"
	StatusStale       Status = "stale"
	StatusConflicting Status = "conflicting"
	StatusSuperseded  Status = "superseded"
	StatusDenied      Status = "denied"
	StatusWithdrawn   Status = "withdrawn"
	StatusApproved    Status = "approved"
	StatusApplied     Status = "applied"
	StatusNeedsReview Status = "needs_review"
)

type TargetKind string

const (
	TargetProject     TargetKind = "project"
	TargetView        TargetKind = "view"
	TargetEntity      TargetKind = "entity"
	TargetRelation    TargetKind = "relation"
	TargetSourceScope TargetKind = "source_scope"
	TargetDiffEntry   TargetKind = "diff_entry"
)

type Target struct {
	Kind          TargetKind              `json:"kind"`
	StableAddress *semantic.StableAddress `json:"stable_address,omitempty"`
	SourceRange   *semantic.SourceRange   `json:"source_range,omitempty"`
	DiffKey       string                  `json:"diff_key,omitempty"`
}

type Comment struct {
	ID           string                     `json:"id"`
	Author       accessprotocol.ActorRef    `json:"author"`
	Body         string                     `json:"body"`
	Target       Target                     `json:"target"`
	CreatedAt    time.Time                  `json:"created_at"`
	UpdatedAt    time.Time                  `json:"updated_at"`
	Stale        bool                       `json:"stale"`
	BaseRevision runtimeprotocol.RevisionID `json:"base_revision"`
}

type ArtifactPreview struct {
	Kind      string                `json:"kind"`
	Label     string                `json:"label"`
	Digest    protocolcommon.Digest `json:"digest"`
	MediaType string                `json:"media_type"`
}

// Evidence is Engine-owned inspection output. Review stores and presents it;
// it never derives semantic change or Required Capability classifications.
type Evidence struct {
	SemanticDiff      semantic.SemanticDiff     `json:"semantic_diff"`
	SourceDiff        engineprotocol.SourceDiff `json:"source_diff"`
	AuthoringImpact   semantic.AuthoringImpact  `json:"authoring_impact"`
	Diagnostics       []semantic.Diagnostic     `json:"diagnostics"`
	AffectedUsages    []semantic.StableAddress  `json:"affected_usages"`
	AffectedRows      []semantic.StableAddress  `json:"affected_rows"`
	AffectedViews     []semantic.StableAddress  `json:"affected_views"`
	DefinitionPreview *ArtifactPreview          `json:"definition_preview,omitempty"`
	RenderPreviews    []ArtifactPreview         `json:"render_previews"`
}

type Proposal struct {
	ID                     string                                `json:"id"`
	Generation             uint64                                `json:"generation"`
	Status                 Status                                `json:"status"`
	CurrentRevision        runtimeprotocol.CommittedRevisionRef  `json:"current_revision"`
	ProposedDefinitionHash protocolcommon.Digest                 `json:"proposed_definition_hash"`
	ProposedGraphHash      protocolcommon.Digest                 `json:"proposed_graph_hash"`
	OperationBatch         runtimeprotocol.RuntimeOperationBatch `json:"operation_batch"`
	AuthoringProof         runtimeprotocol.AuthoringProof        `json:"authoring_proof"`
	Evidence               Evidence                              `json:"evidence"`
	Proposer               accessprotocol.ActorRef               `json:"proposer"`
	AgentDelegationDigest  *protocolcommon.Digest                `json:"agent_delegation_digest,omitempty"`
	AccessEvaluationDigest protocolcommon.Digest                 `json:"access_evaluation_digest"`
	AccessDecisionDigest   protocolcommon.Digest                 `json:"access_decision_digest"`
	RequiredCapabilities   []semantic.AuthoringCapability        `json:"required_capabilities"`
	Comments               []Comment                             `json:"comments"`
	CreatedAt              time.Time                             `json:"created_at"`
	UpdatedAt              time.Time                             `json:"updated_at"`
	ApprovedBy             *accessprotocol.ActorRef              `json:"approved_by,omitempty"`
	CommittedRevision      *runtimeprotocol.CommittedRevisionRef `json:"committed_revision,omitempty"`
	LastFailure            string                                `json:"last_failure,omitempty"`
}

type Snapshot struct {
	Version   int        `json:"version"`
	Proposals []Proposal `json:"proposals"`
}

type CreateInput struct {
	ProposalID       string
	Preview          RepreviewResult
	Proposer         accessprotocol.ActorRef
	DelegationDigest *protocolcommon.Digest
	Permissions      accesscore.AgentPermissions
	ProposeDecision  accessprotocol.AuthoringDecision
	Now              time.Time
}

type ApprovalInput struct {
	ProposalID     string
	Generation     uint64
	Session        runtimeprotocol.RuntimeSessionRef
	Approver       accessprotocol.ActorRef
	OperationID    runtimeprotocol.OperationID
	IdempotencyKey runtimeprotocol.IdempotencyKey
	Trigger        runtimeprotocol.CommitTrigger
	Cancellation   *runtimeprotocol.CancellationToken
}

type CommentInput struct {
	ProposalID string
	Generation uint64
	CommentID  string
	Author     accessprotocol.ActorRef
	Body       string
	Target     Target
}

type RepreviewInput struct {
	Session runtimeprotocol.RuntimeSessionRef
	Batch   runtimeprotocol.RuntimeOperationBatch
}

type RepreviewResult struct {
	CurrentRevision runtimeprotocol.CommittedRevisionRef
	DefinitionHash  protocolcommon.Digest
	GraphHash       protocolcommon.Digest
	OperationBatch  runtimeprotocol.RuntimeOperationBatch
	AuthoringProof  runtimeprotocol.AuthoringProof
	PreviewDecision accessprotocol.AuthoringDecision
	Evidence        Evidence
}

type ApprovalRequest struct {
	Approver accessprotocol.ActorRef
	Revision runtimeprotocol.CommittedRevisionRef
	Impact   semantic.AuthoringImpact
	Decision accessprotocol.AuthoringDecision
}

type RuntimePort interface {
	Repreview(context.Context, RepreviewInput) (RepreviewResult, error)
	Commit(context.Context, runtimeprotocol.RuntimeCommitInput) (runtimeprotocol.RuntimeCommitResult, error)
}

type AccessPort interface {
	AuthorizeApprover(context.Context, ApprovalRequest) (accessprotocol.AuthoringDecision, error)
}

type Store interface {
	Load(context.Context) (Snapshot, error)
	Save(context.Context, Snapshot) error
}
