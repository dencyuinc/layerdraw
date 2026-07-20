// SPDX-License-Identifier: LicenseRef-LayerDraw-1.0

package desktopcontract

import (
	"context"

	"github.com/dencyuinc/layerdraw/gen/go/accessprotocol"
	"github.com/dencyuinc/layerdraw/gen/go/protocolcommon"
	accesscore "github.com/dencyuinc/layerdraw/internal/access"
)

type LifecycleState string

const (
	LifecycleStarting LifecycleState = "starting"
	LifecycleReady    LifecycleState = "ready"
	LifecycleDraining LifecycleState = "draining"
	LifecycleStopped  LifecycleState = "stopped"
	LifecycleRecovery LifecycleState = "recovery"
)

type LifecycleEvent struct {
	State LifecycleState `json:"state"`
}

type LifecyclePort interface {
	Publish(context.Context, LifecycleEvent) error
}

type WindowPort interface {
	Show(context.Context) error
	RequestClose(context.Context) error
}

type DialogKind string

const (
	DialogCreateProject DialogKind = "create_project"
	DialogOpenProject   DialogKind = "open_project"
	DialogImport        DialogKind = "import"
	DialogExport        DialogKind = "export"
)

type DialogRequest struct {
	Kind       DialogKind `json:"kind"`
	RequestID  string     `json:"request_id"`
	Extensions []string   `json:"extensions"`
}

type DialogSelection struct {
	// Token is an opaque host-issued reference. Native paths never cross the
	// frontend binding and are resolved by the storage adapter.
	Token string `json:"token"`
}

type NativeDialogPort interface {
	Select(context.Context, DialogRequest) Result[DialogSelection]
}

type CredentialRef struct {
	ID string `json:"id"`
}

type CredentialPort interface {
	Resolve(context.Context, CredentialRef) Result[[]byte]
}

type LocalActorPort interface {
	ResolveLocalActor(context.Context) Result[accessprotocol.ActorRef]
}

type LocalOwnerGrantRequest struct {
	Actor    accessprotocol.ActorRef          `json:"actor"`
	Scope    accessprotocol.HostResourceScope `json:"scope"`
	IssuedAt protocolcommon.Rfc3339Time       `json:"issued_at"`
}

type LocalOwnerGrantPort interface {
	IssueLocalOwnerGrant(context.Context, LocalOwnerGrantRequest) Result[accessprotocol.AuthoringGrantSnapshot]
}

type DelegationFence struct {
	DelegationID string                         `json:"delegation_id"`
	DocumentID   string                         `json:"document_id"`
	LocalScopeID string                         `json:"local_scope_id"`
	Generation   protocolcommon.CanonicalUint64 `json:"generation"`
}

type AgentDelegationPort interface {
	Delegate(context.Context, accessprotocol.AuthoringGrantSnapshot, accesscore.Delegation) Result[accesscore.Delegation]
	Resolve(context.Context, DelegationFence) Result[accesscore.Delegation]
	Revoke(context.Context, DelegationFence) Result[accesscore.DelegationSnapshot]
}

type MCPTransportPort interface {
	Start(context.Context) Result[struct{}]
	Shutdown(context.Context) Result[struct{}]
}

type AssetEmbedPort interface {
	FrontendManifest(context.Context) Result[[]FrontendAssetID]
}

// ShellPorts are the only responsibilities owned by Wails itself.
type ShellPorts struct {
	Lifecycle LifecyclePort
	Window    WindowPort
	Dialogs   NativeDialogPort
	Assets    AssetEmbedPort
}

// HostPorts are injected framework-neutral adapters. Semantic components
// consume their established owner contracts rather than Wails-specific types.
type HostPorts struct {
	Credentials CredentialPort
	LocalActor  LocalActorPort
	LocalOwner  LocalOwnerGrantPort
	Delegations AgentDelegationPort
	MCP         MCPTransportPort
}
