// SPDX-License-Identifier: LicenseRef-LayerDraw-1.0

package port

import (
	"context"
	"errors"
	"io"
	"time"

	"github.com/dencyuinc/layerdraw/gen/go/accessprotocol"
	"github.com/dencyuinc/layerdraw/gen/go/engineprotocol"
	"github.com/dencyuinc/layerdraw/gen/go/protocolcommon"
	"github.com/dencyuinc/layerdraw/gen/go/runtimeprotocol"
	"github.com/dencyuinc/layerdraw/gen/go/semantic"
)

// Stable adapter outcomes. Providers map their private errors to these values;
// Runtime never inspects provider error strings.
var (
	ErrNotFound      = errors.New("runtime port: not found")
	ErrConflict      = errors.New("runtime port: conditional conflict")
	ErrInvalidScope  = errors.New("runtime port: invalid result scope")
	ErrIndeterminate = errors.New("runtime port: publication indeterminate")
)

// Workbench is the Runtime-facing view of the Go Engine Workbench. It owns all
// LDL parsing, semantic application, canonical source rewriting, hashes, and
// AuthoringImpact classification. Runtime only coordinates its result.
type Workbench interface {
	Open(context.Context, OpenWorkingDocumentInput) (WorkingDocument, error)
	Preview(context.Context, PreviewWorkingDocumentInput) (PreparedRevision, error)
	Checkpoint(context.Context, CheckpointWorkingDocumentInput) (WorkingDocument, error)
}

type RegistryStagedObjectRef struct {
	ObjectID  string
	Digest    protocolcommon.Digest
	Size      protocolcommon.CanonicalUint64
	MediaType string
}

type PrepareRegistryRevisionInput struct {
	Scope                      runtimeprotocol.RuntimeScope
	BaseRevision               runtimeprotocol.CommittedRevisionRef
	RegistryTransactionID      string
	PlanDigest                 protocolcommon.Digest
	MutationDigest             protocolcommon.Digest
	ExpectedResolvedLockDigest protocolcommon.Digest
	StagedObjects              []RegistryStagedObjectRef
}

// RegistryRevisionPreparer is the Engine-owned handoff from verified staged
// package bytes to a complete Runtime PreparedRevision. Runtime never parses
// LDL, expands archives, or fabricates source blobs from plan metadata.
type RegistryRevisionPreparer interface {
	PrepareRegistryRevision(context.Context, PrepareRegistryRevisionInput) (PreparedRevision, error)
}

// WorkingDocumentCloser is optional because some remote Engine facades expire
// handles server-side. Local hosts implement it to release retained bytes on
// bounded close and repeated open/close cycles.
type WorkingDocumentCloser interface {
	Close(context.Context, WorkingDocument) error
}

type OpenWorkingDocumentInput struct {
	Scope    runtimeprotocol.RuntimeScope
	Revision RevisionSnapshot
	Sources  SourceBlobSet
	Limits   runtimeprotocol.RuntimeLimits
}

type WorkingDocument struct {
	Handle         string
	Generation     protocolcommon.CanonicalNonNegativeInt64
	BaseRevision   runtimeprotocol.CommittedRevisionRef
	DefinitionHash protocolcommon.Digest
	GraphHash      protocolcommon.Digest
}

type PreviewWorkingDocumentInput struct {
	Document      WorkingDocument
	Batch         engineprotocol.SemanticOperationBatch
	Preconditions engineprotocol.EngineEditPreconditions
	MaxOperations protocolcommon.CanonicalPositiveInt64
}

type PreparedRevision struct {
	AuthoringImpact semantic.AuthoringImpact
	DefinitionHash  protocolcommon.Digest
	GraphHash       protocolcommon.Digest
	// Sources is the complete canonical source closure produced by Workbench.
	// Every ref identity is unique and Contents must match its declared size and
	// sha256 digest before Runtime may pass the set to StageRevision.
	Sources  SourceBlobSet
	Manifest protocolcommon.BlobRef
	// External is an optional, already-materialized file-backed projection.
	// Server-backed Workbench implementations leave it nil.
	External *ExternalMaterialization
}

type CheckpointWorkingDocumentInput struct {
	Document WorkingDocument
	Prepared PreparedRevision
	Revision runtimeprotocol.CommittedRevisionRef
}

// GrantSource resolves a fresh trusted snapshot for every open and commit.
// A preview proof is an equality precondition and is never treated as a grant.
type GrantSource interface {
	ResolveGrant(context.Context, runtimeprotocol.RuntimeScope) (accessprotocol.AuthoringGrantSnapshot, accessprotocol.AuthoringGrantSummary, error)
	// AcquireAuthoringPublication returns a fence held through authoritative
	// publication. Delegation revoke/expiry mutation must be linearized against
	// this fence; returning no fence is not a supported alternate route.
	AcquireAuthoringPublication(context.Context, runtimeprotocol.RuntimeScope) (release func(), err error)
}

// ScopeSource resolves trusted host scope; OpenRuntimeDocumentInput deliberately
// carries only a document id and cannot self-assert organization or access.
type ScopeSource interface {
	ResolveScope(context.Context, runtimeprotocol.DocumentID) (runtimeprotocol.RuntimeScope, error)
}

// DocumentStore owns canonical revision bytes and the conditional head
// publication point. A provider version is an opaque comparison token, not a
// provider SDK object.
type DocumentStore interface {
	GetHead(context.Context, GetDocumentHeadInput) (DocumentHead, error)
	ReadRevision(context.Context, ReadRevisionInput) (RevisionSnapshot, error)
	ReadSourceBlobs(context.Context, ReadSourceBlobsInput) (SourceBlobSet, error)
	StageRevision(context.Context, StageRevisionInput) (StagedRevision, error)
	PublishHead(context.Context, PublishDocumentHeadInput) (PublishHeadResult, error)
	AbortStagedRevision(context.Context, AbortStagedRevisionInput) error
}

type GetDocumentHeadInput struct{ Scope runtimeprotocol.RuntimeScope }

type DocumentHead struct {
	Revision        runtimeprotocol.CommittedRevisionRef
	ProviderVersion runtimeprotocol.ProviderVersionToken
	FencingToken    protocolcommon.CanonicalUint64
}

type ReadRevisionInput struct {
	Scope      runtimeprotocol.RuntimeScope
	RevisionID runtimeprotocol.RevisionID
}

type RevisionSnapshot struct {
	Revision    runtimeprotocol.CommittedRevisionRef
	SourceBlobs []protocolcommon.BlobRef
	Manifest    protocolcommon.BlobRef
}

type ReadSourceBlobsInput struct {
	Scope    runtimeprotocol.RuntimeScope
	Revision runtimeprotocol.CommittedRevisionRef
	Blobs    []protocolcommon.BlobRef
}

// SourceBlob carries the exact source bytes behind a revision BlobRef. The
// provider-neutral port owns byte acquisition; Runtime never interprets a
// provider locator or SDK stream handle. ReadSourceBlobs returns exactly one
// SourceBlob for every requested BlobRef, with no missing, extra, or duplicate
// BlobID and with the complete Ref and Contents verified byte-for-byte.
type SourceBlob struct {
	Ref      protocolcommon.BlobRef
	Contents []byte
}

type SourceBlobSet struct {
	Revision runtimeprotocol.CommittedRevisionRef
	Blobs    []SourceBlob
}

type StageRevisionInput struct {
	Scope             runtimeprotocol.RuntimeScope
	OperationID       runtimeprotocol.OperationID
	IdempotencyKey    runtimeprotocol.IdempotencyKey
	BaseRevision      runtimeprotocol.CommittedRevisionRef
	DefinitionHash    protocolcommon.Digest
	GraphHash         protocolcommon.Digest
	SourceBlobs       SourceBlobSet
	Manifest          protocolcommon.BlobRef
	DecisionDigest    protocolcommon.Digest
	EvaluationDigest  protocolcommon.Digest
	Actor             accessprotocol.ActorRef
	Trigger           runtimeprotocol.CommitTrigger
	CancellationToken *runtimeprotocol.CancellationToken
	// PreviewEvaluation is durable recovery evidence. Local adapters persist it
	// with the staged candidate so a restarted host never has to recreate or
	// weaken the authorization decision that preceded publication.
	PreviewEvaluation *runtimeprotocol.PreviewEvaluation
}

type StagedRevision struct {
	StageID      string
	Revision     runtimeprotocol.CommittedRevisionRef
	StagedDigest protocolcommon.Digest
}

type PublishDocumentHeadInput struct {
	Scope                   runtimeprotocol.RuntimeScope
	StageID                 string
	ExpectedRevision        runtimeprotocol.RevisionID
	ExpectedDefinitionHash  protocolcommon.Digest
	ExpectedProviderVersion runtimeprotocol.ProviderVersionToken
	FencingToken            protocolcommon.CanonicalUint64
}

type PublishHeadResult struct {
	Published       bool
	Revision        runtimeprotocol.CommittedRevisionRef
	ProviderVersion runtimeprotocol.ProviderVersionToken
}

type AbortStagedRevisionInput struct {
	Scope   runtimeprotocol.RuntimeScope
	StageID string
}

// StateBackend stores durable host state separately from authored definition
// bytes. Runtime alone performs Access projection into StateQuerySnapshot.
type StateBackend interface {
	GetHead(context.Context, GetStateHeadInput) (StateHead, error)
	ReadState(context.Context, ReadStateInput) (StateSnapshot, error)
	WriteState(context.Context, WriteStateInput) (StateWriteResult, error)
	AcquireLease(context.Context, AcquireLeaseInput) (StateLease, error)
	RenewLease(context.Context, RenewLeaseInput) (StateLease, error)
	ReleaseLease(context.Context, ReleaseLeaseInput) error
	ValidateLease(context.Context, ValidateLeaseInput) (StateLease, error)
	AppendAuditEvent(context.Context, AppendAuditEventInput) (AuditEventRef, error)
	ListAuditEvents(context.Context, ListAuditEventsInput) (AuditEventPage, error)
	ExportSnapshot(context.Context, ExportStateSnapshotInput) (StateSnapshot, error)
}

type ValidateLeaseInput struct {
	Scope      runtimeprotocol.RuntimeScope
	LeaseToken runtimeprotocol.LeaseToken
}

type GetStateHeadInput struct{ Scope runtimeprotocol.RuntimeScope }

type StateHead struct {
	StateVersion   protocolcommon.CanonicalNonNegativeInt64
	BackendVersion runtimeprotocol.ProviderVersionToken
	DefinitionHash protocolcommon.Digest
	GraphHash      protocolcommon.Digest
	CapturedAt     protocolcommon.Rfc3339Time
	SubjectHashes  map[semantic.StableAddress]protocolcommon.Digest
}

type ReadStateInput struct {
	Scope                runtimeprotocol.RuntimeScope
	ExpectedStateVersion *protocolcommon.CanonicalNonNegativeInt64
}

type StateSnapshot struct {
	Head                   StateHead
	Contents               protocolcommon.BlobRef
	InaccessibleFieldPaths []semantic.StateFieldPath
	Records                []StateRecord
}

// StateRecord is the provider-neutral durable record projected by Runtime.
// Fields outside the closed Language 1 StateFieldPath registry may be retained
// by an adapter under ProviderFields, but Runtime never exposes them to Engine.
type StateRecord struct {
	SubjectAddress     semantic.StableAddress
	SubjectKind        semantic.StateSubjectKind
	OwnSubjectHash     protocolcommon.Digest
	Fields             map[string]semantic.RecipeScalar
	ProviderFields     map[string]any
	RedactedFieldPaths []semantic.StateFieldPath
	Tombstoned         bool
}

type BackendBindingKind string

const (
	BackendBindingNone     BackendBindingKind = "none"
	BackendBindingLocal    BackendBindingKind = "local"
	BackendBindingPackaged BackendBindingKind = "packaged_state"
)

// BackendBinding is resolved exclusively from trusted host input. BindingID
// is an opaque adapter-owned identifier, never an LDL field or package member.
type BackendBinding struct {
	Kind      BackendBindingKind
	BindingID string
}

type ResolveStateBackendInput struct {
	Scope   runtimeprotocol.RuntimeScope
	Binding BackendBinding
}

// StateBackendBindingResolver selects an already configured local or packaged
// backend. It does not parse LDL or portable container content for config.
type StateBackendBindingResolver interface {
	ResolveStateBackend(context.Context, ResolveStateBackendInput) (StateBackend, error)
}

type StateQueryAuthorizationSubject struct {
	SubjectAddress semantic.StableAddress
	SubjectKind    semantic.StateSubjectKind
}

type StateQueryAuthorizationInput struct {
	Scope                    runtimeprotocol.RuntimeScope
	DefinitionProjectAddress semantic.ProjectRootAddress
	DefinitionHash           protocolcommon.Digest
	GraphHash                protocolcommon.Digest
	FieldPaths               []semantic.StateFieldPath
	Subjects                 []StateQueryAuthorizationSubject
}

// StateQueryAuthorizationDecision is fail-closed: inaccessible paths apply
// across the actor scope, while redacted paths apply to one subject. All other
// closed-registry paths are allowed. Raw values are never decision inputs.
type StateQueryAuthorizationDecision struct {
	AccessFingerprint      protocolcommon.Digest
	DecisionDigest         protocolcommon.Digest
	InaccessibleFieldPaths []semantic.StateFieldPath
	RedactedFieldPaths     map[semantic.StableAddress][]semantic.StateFieldPath
}

type StateQueryAuthorization interface {
	EvaluateStateQuery(context.Context, StateQueryAuthorizationInput) (StateQueryAuthorizationDecision, error)
}

type WriteStateInput struct {
	Scope                  runtimeprotocol.RuntimeScope
	OperationID            runtimeprotocol.OperationID
	IdempotencyKey         runtimeprotocol.IdempotencyKey
	ExpectedStateVersion   protocolcommon.CanonicalNonNegativeInt64
	ExpectedBackendVersion runtimeprotocol.ProviderVersionToken
	ExpectedDefinitionHash protocolcommon.Digest
	ExpectedSubjectHashes  map[semantic.StableAddress]protocolcommon.Digest
	LeaseToken             runtimeprotocol.LeaseToken
	Mutation               runtimeprotocol.StateMutation
}

type StateWriteResult struct {
	// Head is the complete trusted post-write binding. Returning only a version
	// would leave the next mutation using stale backend and subject metadata.
	Head StateHead
}

type AcquireLeaseInput struct {
	Scope   runtimeprotocol.RuntimeScope
	OwnerID string
	TTL     time.Duration
}

type RenewLeaseInput struct {
	Scope      runtimeprotocol.RuntimeScope
	LeaseToken runtimeprotocol.LeaseToken
	TTL        time.Duration
}

type ReleaseLeaseInput struct {
	Scope      runtimeprotocol.RuntimeScope
	LeaseToken runtimeprotocol.LeaseToken
}

type StateLease struct {
	LeaseToken   runtimeprotocol.LeaseToken
	FencingToken protocolcommon.CanonicalUint64
	ExpiresAt    time.Time
}

type AppendAuditEventInput struct {
	Scope                runtimeprotocol.RuntimeScope
	OperationID          runtimeprotocol.OperationID
	ExpectedStateVersion protocolcommon.CanonicalNonNegativeInt64
	EventDigest          protocolcommon.Digest
	Event                protocolcommon.BlobRef
}

type AuditEventRef struct {
	EventID      string
	EventDigest  protocolcommon.Digest
	StateVersion protocolcommon.CanonicalNonNegativeInt64
}

type ListAuditEventsInput struct {
	Scope    runtimeprotocol.RuntimeScope
	Cursor   *runtimeprotocol.RuntimeCursor
	MaxItems protocolcommon.CanonicalPositiveSafeInteger
}

type AuditEventPage struct {
	Items []AuditEventRef
	Page  protocolcommon.PageInfo
}

type ExportStateSnapshotInput struct {
	Scope        runtimeprotocol.RuntimeScope
	StateVersion protocolcommon.CanonicalNonNegativeInt64
}

// AssetStore is content-addressed. Logical authored paths remain in the
// document manifest and are never interpreted by an adapter.
type AssetStore interface {
	Stat(context.Context, AssetRef) (AssetMetadata, error)
	Get(context.Context, AssetRef) (io.ReadCloser, error)
	PutIfAbsent(context.Context, PutAssetInput) (AssetMetadata, error)
	DeleteIfUnreferenced(context.Context, DeleteAssetInput) error
}

type AssetRef struct {
	Scope  runtimeprotocol.RuntimeScope
	Digest protocolcommon.Digest
}

type AssetMetadata struct {
	Digest    protocolcommon.Digest
	MediaType string
	Size      protocolcommon.CanonicalUint64
}

type PutAssetInput struct {
	Scope          runtimeprotocol.RuntimeScope
	ExpectedDigest protocolcommon.Digest
	MediaType      string
	Size           protocolcommon.CanonicalUint64
	Contents       io.Reader
}

type DeleteAssetInput struct {
	AssetRef
	ExpectedUnreferenced bool
}

type ExternalFileKind string

const (
	ExternalFileKindContainer ExternalFileKind = "container"
	ExternalFileKindProject   ExternalFileKind = "project"
)

type ExternalProjectFile struct {
	Path     string
	Contents []byte
}

// ExternalMaterialization is the complete bounded file-backed projection
// produced by the host's Engine boundary. Runtime and storage adapters never
// parse LDL or reconstruct a source tree from semantic output.
type ExternalMaterialization struct {
	Kind         ExternalFileKind
	ProjectFiles []ExternalProjectFile
	Container    []byte
}

type ExternalFileHead struct {
	ProviderVersion runtimeprotocol.ProviderVersionToken
}

type GetExternalFileHeadInput struct{ Scope runtimeprotocol.RuntimeScope }

type PrepareExternalFileInput struct {
	Scope                   runtimeprotocol.RuntimeScope
	OperationID             runtimeprotocol.OperationID
	IdempotencyKey          runtimeprotocol.IdempotencyKey
	RevisionID              runtimeprotocol.RevisionID
	ExpectedProviderVersion runtimeprotocol.ProviderVersionToken
	Materialization         ExternalMaterialization
}

type ExternalFileStage struct {
	StageID                  string
	CandidateProviderVersion runtimeprotocol.ProviderVersionToken
	MaterializationDigest    protocolcommon.Digest
}

type PublishExternalFileInput struct {
	Scope                   runtimeprotocol.RuntimeScope
	OperationID             runtimeprotocol.OperationID
	IdempotencyKey          runtimeprotocol.IdempotencyKey
	StageID                 string
	ExpectedProviderVersion runtimeprotocol.ProviderVersionToken
}

type ExternalFileReceipt struct {
	OperationID           runtimeprotocol.OperationID
	IdempotencyKey        runtimeprotocol.IdempotencyKey
	RevisionID            runtimeprotocol.RevisionID
	ProviderVersion       runtimeprotocol.ProviderVersionToken
	ReceiptDigest         protocolcommon.Digest
	MaterializationDigest protocolcommon.Digest
}

type InspectExternalFileInput struct {
	Scope          runtimeprotocol.RuntimeScope
	OperationID    runtimeprotocol.OperationID
	IdempotencyKey runtimeprotocol.IdempotencyKey
}

type ExternalFileInspection struct {
	Stage   *ExternalFileStage
	Receipt *ExternalFileReceipt
}

type AbortExternalFileInput struct {
	Scope   runtimeprotocol.RuntimeScope
	StageID string
}

// ExternalFileStore conditionally publishes a complete file-backed source
// projection after DocumentStore publication. Prepare is non-visible and
// Publish is idempotent by operation and idempotency identity.
type ExternalFileStore interface {
	GetExternalHead(context.Context, GetExternalFileHeadInput) (ExternalFileHead, error)
	Prepare(context.Context, PrepareExternalFileInput) (ExternalFileStage, error)
	Publish(context.Context, PublishExternalFileInput) (ExternalFileReceipt, error)
	Inspect(context.Context, InspectExternalFileInput) (ExternalFileInspection, error)
	Abort(context.Context, AbortExternalFileInput) error
}

// HistoryStore indexes immutable DocumentStore revisions; it is not the
// source of canonical document bytes.
type HistoryStore interface {
	AppendRevision(context.Context, AppendRevisionInput) (runtimeprotocol.RevisionMetadata, error)
	GetRevision(context.Context, GetRevisionMetadataInput) (runtimeprotocol.RevisionMetadata, error)
	ListRevisions(context.Context, ListRevisionsInput) (runtimeprotocol.RevisionPage, error)
	ResolveProviderVersion(context.Context, ResolveProviderVersionInput) (ProviderRevisionRef, error)
}

type AppendRevisionInput struct {
	Scope    runtimeprotocol.RuntimeScope
	Metadata runtimeprotocol.RevisionMetadata
}

type GetRevisionMetadataInput struct {
	Scope      runtimeprotocol.RuntimeScope
	RevisionID runtimeprotocol.RevisionID
}

type ListRevisionsInput struct {
	Scope          runtimeprotocol.RuntimeScope
	Cursor         *runtimeprotocol.RuntimeCursor
	MaxItems       protocolcommon.CanonicalPositiveSafeInteger
	MaxOutputBytes protocolcommon.CanonicalPositiveInt64
}

type ResolveProviderVersionInput struct {
	Scope           runtimeprotocol.RuntimeScope
	ProviderVersion runtimeprotocol.ProviderVersionToken
}

type ProviderRevisionRef struct {
	Revision        runtimeprotocol.CommittedRevisionRef
	ProviderVersion runtimeprotocol.ProviderVersionToken
}

// RecoveryJournal is the durable write-ahead authority used to prove whether
// publication occurred after a timeout or partial failure.
type RecoveryJournal interface {
	CreatePending(context.Context, CreatePendingRecordInput) (RecoveryRecord, error)
	AbandonPending(context.Context, AbandonPendingRecordInput) error
	Get(context.Context, GetRecoveryRecordInput) (RecoveryRecord, error)
	Advance(context.Context, AdvanceRecoveryRecordInput) (RecoveryRecord, error)
	Finalize(context.Context, FinalizeRecoveryRecordInput) (RecoveryRecord, error)
}

type AbandonPendingRecordInput struct {
	Scope          runtimeprotocol.RuntimeScope
	OperationID    runtimeprotocol.OperationID
	IdempotencyKey runtimeprotocol.IdempotencyKey
	PayloadDigest  protocolcommon.Digest
}

type CreatePendingRecordInput struct {
	Scope          runtimeprotocol.RuntimeScope
	OperationID    runtimeprotocol.OperationID
	IdempotencyKey runtimeprotocol.IdempotencyKey
	PayloadDigest  protocolcommon.Digest
	// BaseRevision is the caller's validated wire base. Reservation happens
	// before current-head, lease, preview, and Access evaluation so every typed
	// rejection can be durably replayed without rerunning those checks.
	BaseRevision runtimeprotocol.CommittedRevisionRef
}

type GetRecoveryRecordInput struct {
	Scope          runtimeprotocol.RuntimeScope
	OperationID    *runtimeprotocol.OperationID
	IdempotencyKey *runtimeprotocol.IdempotencyKey
}

type AdvanceRecoveryRecordInput struct {
	Scope                           runtimeprotocol.RuntimeScope
	OperationID                     runtimeprotocol.OperationID
	ExpectedPhase                   runtimeprotocol.RecoveryPhase
	NextPhase                       runtimeprotocol.RecoveryPhase
	PublishedRevision               *runtimeprotocol.CommittedRevisionRef
	EvaluationDigest                *protocolcommon.Digest
	DecisionDigest                  *protocolcommon.Digest
	PreviewEvaluation               *runtimeprotocol.PreviewEvaluation
	ExternalStage                   *ExternalFileStage
	ExpectedExternalProviderVersion *runtimeprotocol.ProviderVersionToken
	ExternalReceipt                 *ExternalFileReceipt
	ExternalFailure                 *runtimeprotocol.ExternalMaterializationFailure
}

type FinalizeRecoveryRecordInput struct {
	Scope        runtimeprotocol.RuntimeScope
	CommitResult runtimeprotocol.RuntimeCommitResult
	// TerminalPhase is final for proven outcomes and needs_review only when
	// publication cannot be proven from the trusted head.
	TerminalPhase runtimeprotocol.RecoveryPhase
}

type RecoveryRecord struct {
	Scope                           runtimeprotocol.RuntimeScope
	Status                          runtimeprotocol.RuntimeOperationStatus
	CommitResult                    *runtimeprotocol.RuntimeCommitResult
	PayloadDigest                   protocolcommon.Digest
	BaseRevision                    runtimeprotocol.CommittedRevisionRef
	EvaluationDigest                *protocolcommon.Digest
	DecisionDigest                  *protocolcommon.Digest
	PreviewEvaluation               *runtimeprotocol.PreviewEvaluation
	ExternalStage                   *ExternalFileStage
	ExpectedExternalProviderVersion *runtimeprotocol.ProviderVersionToken
	ExternalReceipt                 *ExternalFileReceipt
	ExternalFailure                 *runtimeprotocol.ExternalMaterializationFailure
}

// AuthoringDecision is injected explicitly. Local full-authoring hosts use
// this exact interface with a full grant; Runtime has no authorization bypass.
type AuthoringDecision interface {
	Evaluate(context.Context, accessprotocol.EvaluateAuthoringInput) (accessprotocol.AuthoringDecision, error)
}

// Clock and IdentityGenerator make expiry, recovery, and opaque identity
// behavior deterministic and testable without package-level mutable state.
type Clock interface{ Now() time.Time }

type IdentityKind string

const (
	IdentityRuntimeSession IdentityKind = "runtime_session"
	IdentityOperation      IdentityKind = "operation"
	IdentityRevision       IdentityKind = "revision"
	IdentityCancellation   IdentityKind = "cancellation"
)

type IdentityGenerator interface {
	NewID(context.Context, IdentityKind) (string, error)
}
