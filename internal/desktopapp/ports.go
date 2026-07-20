// SPDX-License-Identifier: LicenseRef-LayerDraw-1.0

// Package desktopapp composes the trusted in-process Desktop backend. It owns
// lifecycle and framework wiring only; capability semantics remain in their
// owner packages.
package desktopapp

import (
	"context"
	"encoding/json"
	"errors"

	"github.com/dencyuinc/layerdraw/gen/go/accessprotocol"
	"github.com/dencyuinc/layerdraw/gen/go/protocolcommon"
	"github.com/dencyuinc/layerdraw/gen/go/runtimeprotocol"
	accesscore "github.com/dencyuinc/layerdraw/internal/access"
	"github.com/dencyuinc/layerdraw/internal/desktopcontract"
	nativeexport "github.com/dencyuinc/layerdraw/internal/exporter"
)

var errInjectedPanic = errors.New("desktop injected port panic")

// Adapter is the lifecycle surface implemented by a Desktop capability
// adapter. Start and Shutdown errors are deliberately mapped to closed Desktop
// failures before they can cross the Wails boundary. Shutdown must be
// idempotent: an interrupted drain retries the remaining reverse-order close.
type Adapter interface {
	Start(context.Context) error
	Shutdown(context.Context) error
}

// ProjectLocation is a trusted backend-only local project reference. Native
// paths never appear in a Wails request or response.
type ProjectLocation struct {
	Root          string
	EntryPath     string
	Kind          string
	PinnedContent []byte
}

// ProjectStorage resolves opaque native-dialog tokens and owns project
// creation. It must not parse or rewrite LDL; it only returns a trusted local
// location that the Engine will compile.
type ProjectStorage interface {
	Create(context.Context, string) (ProjectLocation, error)
	Open(context.Context, string) (ProjectLocation, error)
}

// ProjectImportStorage is implemented by the local storage adapter when the
// selected object is an importable .layerdraw container. The opaque selection
// token, rather than a native path, crosses the shell boundary.
type ProjectImportStorage interface {
	Import(context.Context, string) (ProjectLocation, error)
}

// ProjectRelocationStorage validates a replacement selection for a previously
// known stable project. Runtime remains responsible for portable identity
// validation before the binding is changed.
type ProjectRelocationStorage interface {
	Relocate(context.Context, runtimeprotocol.DocumentID, string) (ProjectLocation, error)
}

// NativeInterchangePort owns host filesystem selections, staged artifact
// bytes, durable preview storage, and external-format adapters. Engine-owned
// plans and generated operation batches cross this boundary unchanged.
type NativeInterchangePort interface {
	Profiles() []nativeexport.Profile
	Serialize(context.Context, nativeexport.SerializeInput) (NativeSerializeResult, error)
	Publish(context.Context, string, string) error
	Import(context.Context, string, string) (nativeexport.ImportPreview, error)
}

type NativeArtifactRef struct {
	ArtifactID    string                `json:"artifact_id"`
	LogicalPath   string                `json:"logical_path"`
	MediaType     string                `json:"media_type"`
	ContentDigest protocolcommon.Digest `json:"content_digest"`
}

type NativeSerializeResult struct {
	Artifact       NativeArtifactRef   `json:"artifact"`
	SourceManifest nativeexport.Result `json:"-"`
	Manifest       json.RawMessage     `json:"source_manifest"`
}

type ExternalConnectionRequest struct {
	ProviderID    string                        `json:"provider_id"`
	CredentialRef desktopcontract.CredentialRef `json:"credential_ref"`
	AccountLabel  string                        `json:"account_label"`
	ScopeLabel    string                        `json:"scope_label"`
}

type ExternalConnection struct {
	ConnectionID string                     `json:"connection_id"`
	ProviderID   string                     `json:"provider_id"`
	AccountLabel string                     `json:"account_label"`
	ScopeLabel   string                     `json:"scope_label"`
	Status       ExternalConnectionStatus   `json:"status"`
	Capabilities ExternalProviderCapability `json:"capabilities"`
}

type ExternalConnectionStatus string

const (
	ExternalConnectionConnected    ExternalConnectionStatus = "connected"
	ExternalConnectionExpired      ExternalConnectionStatus = "credential_expired"
	ExternalConnectionRateLimited  ExternalConnectionStatus = "rate_limited"
	ExternalConnectionDisconnected ExternalConnectionStatus = "disconnected"
	ExternalConnectionReconnect    ExternalConnectionStatus = "reconnect_required"
)

// ExternalProviderCapability is discovered from the provider. Desktop never
// guesses write safety from a method being present.
type ExternalProviderCapability struct {
	Open             bool `json:"open"`
	ConditionalWrite bool `json:"conditional_write"`
	Lease            bool `json:"lease"`
	MoveDetection    bool `json:"move_detection"`
	ResumableUpload  bool `json:"resumable_upload"`
}

// ExternalBackendBinding is host metadata, deliberately separate from the
// portable project StableAddress and from provider credentials.
type ExternalBackendBinding struct {
	BindingID       string                               `json:"binding_id"`
	ConnectionID    string                               `json:"connection_id"`
	DocumentID      runtimeprotocol.DocumentID           `json:"document_id"`
	RemoteItemID    string                               `json:"remote_item_id"`
	ProviderVersion runtimeprotocol.ProviderVersionToken `json:"provider_version"`
}

type ExternalRemoteSelectionRequest struct {
	ConnectionID   string                     `json:"connection_id"`
	DocumentID     runtimeprotocol.DocumentID `json:"document_id"`
	SelectionToken string                     `json:"selection_token"`
}

type ExternalLease struct {
	Token     string                     `json:"token"`
	ExpiresAt protocolcommon.Rfc3339Time `json:"expires_at"`
}

type ExternalWriteRequest struct {
	Binding                 ExternalBackendBinding               `json:"binding"`
	Revision                runtimeprotocol.CommittedRevisionRef `json:"revision"`
	ExpectedProviderVersion runtimeprotocol.ProviderVersionToken `json:"expected_provider_version"`
	LeaseToken              string                               `json:"lease_token,omitempty"`
	// Payload is trusted-backend-only material and never crosses Wails.
	Payload []byte `json:"-"`
}

type ExternalWriteState string

const (
	ExternalWritePublished ExternalWriteState = "published"
	ExternalWriteConflict  ExternalWriteState = "conflict"
	ExternalWritePartial   ExternalWriteState = "partial_upload"
	ExternalWriteUnknown   ExternalWriteState = "unknown_write_result"
	ExternalWriteMoved     ExternalWriteState = "moved_item"
	ExternalWriteOffline   ExternalWriteState = "offline"
)

type ExternalWriteResult struct {
	State           ExternalWriteState                   `json:"state"`
	ProviderVersion runtimeprotocol.ProviderVersionToken `json:"provider_version,omitempty"`
	Retryable       bool                                 `json:"retryable"`
}

type ExternalReconcileKind string

const (
	ExternalReconcileUpToDate    ExternalReconcileKind = "up_to_date"
	ExternalReconcileFastForward ExternalReconcileKind = "fast_forward"
	ExternalReconcileConflict    ExternalReconcileKind = "conflict"
	ExternalReconcileMergeRebase ExternalReconcileKind = "merge_rebase"
	ExternalReconcileQuarantined ExternalReconcileKind = "quarantined"
)

type ExternalReconcilePlan struct {
	PlanID          string                               `json:"plan_id"`
	Binding         ExternalBackendBinding               `json:"binding"`
	Kind            ExternalReconcileKind                `json:"kind"`
	LocalRevision   runtimeprotocol.CommittedRevisionRef `json:"local_revision"`
	ProviderVersion runtimeprotocol.ProviderVersionToken `json:"provider_version"`
	RequiresReview  bool                                 `json:"requires_review"`
	Restricted      bool                                 `json:"restricted"`
}

type ExternalSyncRequest struct {
	ConnectionID string                               `json:"connection_id"`
	DocumentID   runtimeprotocol.DocumentID           `json:"document_id"`
	Revision     runtimeprotocol.CommittedRevisionRef `json:"revision"`
}

type ExternalSyncResult struct {
	ProviderVersion runtimeprotocol.ProviderVersionToken `json:"provider_version"`
	ReconcileNeeded bool                                 `json:"reconcile_needed"`
}

type ExternalReconcileRequest struct {
	ConnectionID string                     `json:"connection_id"`
	DocumentID   runtimeprotocol.DocumentID `json:"document_id"`
	Resolution   string                     `json:"resolution"`
}

type ExternalReconcileResult struct {
	ProviderVersion runtimeprotocol.ProviderVersionToken `json:"provider_version"`
	Converged       bool                                 `json:"converged"`
}

// ExternalLifecycleAdapter is a typed host handoff. Provider credentials,
// locators and SDK errors never cross this interface's result values.
type ExternalLifecycleAdapter interface {
	Connect(context.Context, ExternalConnectionRequest) desktopcontract.Result[ExternalConnection]
	Sync(context.Context, ExternalSyncRequest) desktopcontract.Result[ExternalSyncResult]
	Reconcile(context.Context, ExternalReconcileRequest) desktopcontract.Result[ExternalReconcileResult]
}

// ExternalStorageAdapter is the complete Desktop provider contract. Reconcile
// is preview/apply, writes are conditional, and credentials remain owned by
// the injected credential broker.
type ExternalStorageAdapter interface {
	ExternalLifecycleAdapter
	Inspect(context.Context, string) desktopcontract.Result[ExternalConnection]
	Refresh(context.Context, string) desktopcontract.Result[ExternalConnection]
	Disconnect(context.Context, string) desktopcontract.Result[ExternalConnection]
	SelectRemote(context.Context, ExternalRemoteSelectionRequest) desktopcontract.Result[ExternalBackendBinding]
	AcquireLease(context.Context, ExternalBackendBinding) desktopcontract.Result[ExternalLease]
	Write(context.Context, ExternalWriteRequest) desktopcontract.Result[ExternalWriteResult]
	PlanReconcile(context.Context, ExternalSyncRequest, bool) desktopcontract.Result[ExternalReconcilePlan]
	ApplyReconcile(context.Context, ExternalReconcilePlan, string) desktopcontract.Result[ExternalReconcileResult]
}

type ExternalPublicationIntent struct {
	Session  runtimeprotocol.RuntimeSessionRef    `json:"session"`
	Action   string                               `json:"action"`
	Binding  ExternalBackendBinding               `json:"binding"`
	Revision runtimeprotocol.CommittedRevisionRef `json:"revision"`
	Plan     *ExternalReconcilePlan               `json:"plan,omitempty"`
}

// ExternalPublicationGate is implemented by the Runtime/Access owner. It must
// re-evaluate current definition/state/asset/package/project-setting effects;
// an adapter is never allowed to self-authorize publication.
type ExternalPublicationGate interface {
	RevalidateExternalPublication(context.Context, ExternalPublicationIntent) desktopcontract.Result[struct{}]
}

// CapabilityNegotiator returns the generated common handshake produced from
// the actually wired adapters, providers and policy. The composition root
// validates it against the frozen Desktop manifest before publishing ready.
type CapabilityNegotiator interface {
	Negotiate(context.Context, desktopcontract.Manifest) (protocolcommon.HandshakeResult, error)
}

// RecoveryReporter is an optional backend-only diagnostic sink. It receives
// only closed failure values and never underlying errors, paths or content.
type RecoveryReporter interface {
	Report(context.Context, desktopcontract.Failure)
}

type staticActor struct{ actor accessprotocol.ActorRef }

func (s staticActor) ResolveLocalActor(context.Context) (accessprotocol.ActorRef, error) {
	return s.actor, nil
}

func safeLifecyclePublish(ctx context.Context, port desktopcontract.LifecyclePort, event desktopcontract.LifecycleEvent) (err error) {
	defer func() {
		if recover() != nil {
			err = errInjectedPanic
		}
	}()
	return port.Publish(ctx, event)
}

func safeResolveLocalActor(ctx context.Context, port desktopcontract.LocalActorPort) (result desktopcontract.Result[accessprotocol.ActorRef]) {
	defer func() {
		if recover() != nil {
			result = failed[accessprotocol.ActorRef](desktopcontract.FailureBackendPanic, desktopcontract.ComponentAccess, false, desktopcontract.RecoveryExit)
		}
	}()
	return port.ResolveLocalActor(ctx)
}

func safeResolveCredential(ctx context.Context, port desktopcontract.CredentialPort, ref desktopcontract.CredentialRef) (result desktopcontract.Result[[]byte]) {
	defer func() {
		if recover() != nil {
			result = failed[[]byte](desktopcontract.FailureBackendPanic, desktopcontract.ComponentAccess, false, desktopcontract.RecoveryExit)
		}
	}()
	return port.Resolve(ctx, ref)
}

func safeIssueLocalOwnerGrant(ctx context.Context, port desktopcontract.LocalOwnerGrantPort, request desktopcontract.LocalOwnerGrantRequest) (result desktopcontract.Result[accessprotocol.AuthoringGrantSnapshot]) {
	defer func() {
		if recover() != nil {
			result = failed[accessprotocol.AuthoringGrantSnapshot](desktopcontract.FailureBackendPanic, desktopcontract.ComponentAccess, false, desktopcontract.RecoveryExit)
		}
	}()
	return port.IssueLocalOwnerGrant(ctx, request)
}

func safeResolveDelegation(ctx context.Context, port desktopcontract.AgentDelegationPort, fence desktopcontract.DelegationFence) (result desktopcontract.Result[accesscore.Delegation]) {
	defer func() {
		if recover() != nil {
			result = failed[accesscore.Delegation](desktopcontract.FailureBackendPanic, desktopcontract.ComponentAccess, false, desktopcontract.RecoveryExit)
		}
	}()
	return port.Resolve(ctx, fence)
}

func safeNegotiate(ctx context.Context, port CapabilityNegotiator, manifest desktopcontract.Manifest) (value protocolcommon.HandshakeResult, err error) {
	defer func() {
		if recover() != nil {
			err = errInjectedPanic
		}
	}()
	return port.Negotiate(ctx, manifest)
}

func safeMCPStart(ctx context.Context, port desktopcontract.MCPTransportPort) (result desktopcontract.Result[struct{}]) {
	defer func() {
		if recover() != nil {
			result = failed[struct{}](desktopcontract.FailureBackendPanic, desktopcontract.ComponentMCPHost, false, desktopcontract.RecoveryExit)
		}
	}()
	return port.Start(ctx)
}

func safeMCPShutdown(ctx context.Context, port desktopcontract.MCPTransportPort) (result desktopcontract.Result[struct{}]) {
	defer func() {
		if recover() != nil {
			result = failed[struct{}](desktopcontract.FailureBackendPanic, desktopcontract.ComponentMCPHost, false, desktopcontract.RecoveryExit)
		}
	}()
	return port.Shutdown(ctx)
}

func safeReport(ctx context.Context, reporter RecoveryReporter, value desktopcontract.Failure) {
	defer func() { _ = recover() }()
	reporter.Report(ctx, value)
}
