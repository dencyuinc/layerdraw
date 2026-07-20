// SPDX-License-Identifier: LicenseRef-LayerDraw-1.0

// Package desktopapp composes the trusted in-process Desktop backend. It owns
// lifecycle and framework wiring only; capability semantics remain in their
// owner packages.
package desktopapp

import (
	"context"
	"errors"

	"github.com/dencyuinc/layerdraw/gen/go/accessprotocol"
	"github.com/dencyuinc/layerdraw/gen/go/protocolcommon"
	"github.com/dencyuinc/layerdraw/gen/go/runtimeprotocol"
	accesscore "github.com/dencyuinc/layerdraw/internal/access"
	"github.com/dencyuinc/layerdraw/internal/desktopcontract"
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

type ExternalConnectionRequest struct {
	ProviderID    string                        `json:"provider_id"`
	CredentialRef desktopcontract.CredentialRef `json:"credential_ref"`
}

type ExternalConnection struct {
	ConnectionID string `json:"connection_id"`
	ProviderID   string `json:"provider_id"`
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
