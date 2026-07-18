// SPDX-License-Identifier: LicenseRef-LayerDraw-1.0

package port

import (
	"context"
	"io"
	"time"

	"github.com/dencyuinc/layerdraw/gen/go/accessprotocol"
	"github.com/dencyuinc/layerdraw/gen/go/protocolcommon"
	"github.com/dencyuinc/layerdraw/gen/go/runtimeprotocol"
	"github.com/dencyuinc/layerdraw/gen/go/semantic"
)

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
// provider locator or SDK stream handle.
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
	SourceBlobs       SourceBlobSet
	Manifest          protocolcommon.BlobRef
	DecisionDigest    protocolcommon.Digest
	EvaluationDigest  protocolcommon.Digest
	CancellationToken *runtimeprotocol.CancellationToken
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
	AppendAuditEvent(context.Context, AppendAuditEventInput) (AuditEventRef, error)
	ListAuditEvents(context.Context, ListAuditEventsInput) (AuditEventPage, error)
	ExportSnapshot(context.Context, ExportStateSnapshotInput) (StateSnapshot, error)
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
	Head     StateHead
	Contents protocolcommon.BlobRef
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
	StateVersion   protocolcommon.CanonicalNonNegativeInt64
	BackendVersion runtimeprotocol.ProviderVersionToken
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
	Get(context.Context, GetRecoveryRecordInput) (RecoveryRecord, error)
	Advance(context.Context, AdvanceRecoveryRecordInput) (RecoveryRecord, error)
	Finalize(context.Context, FinalizeRecoveryRecordInput) (RecoveryRecord, error)
}

type CreatePendingRecordInput struct {
	Scope            runtimeprotocol.RuntimeScope
	OperationID      runtimeprotocol.OperationID
	IdempotencyKey   runtimeprotocol.IdempotencyKey
	PayloadDigest    protocolcommon.Digest
	BaseRevision     runtimeprotocol.CommittedRevisionRef
	EvaluationDigest protocolcommon.Digest
	DecisionDigest   protocolcommon.Digest
}

type GetRecoveryRecordInput struct {
	Scope          runtimeprotocol.RuntimeScope
	OperationID    *runtimeprotocol.OperationID
	IdempotencyKey *runtimeprotocol.IdempotencyKey
}

type AdvanceRecoveryRecordInput struct {
	Scope             runtimeprotocol.RuntimeScope
	OperationID       runtimeprotocol.OperationID
	ExpectedPhase     runtimeprotocol.RecoveryPhase
	NextPhase         runtimeprotocol.RecoveryPhase
	PublishedRevision *runtimeprotocol.CommittedRevisionRef
}

type FinalizeRecoveryRecordInput struct {
	Scope           runtimeprotocol.RuntimeScope
	OperationResult runtimeprotocol.OperationResult
}

type RecoveryRecord struct {
	Status        runtimeprotocol.RuntimeOperationStatus
	PayloadDigest protocolcommon.Digest
	BaseRevision  runtimeprotocol.CommittedRevisionRef
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
