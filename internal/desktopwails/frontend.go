// SPDX-License-Identifier: LicenseRef-LayerDraw-1.0

package desktopwails

import (
	"context"
	"encoding/json"
	"errors"
	"sync"

	"github.com/dencyuinc/layerdraw/gen/go/runtimeprotocol"
	reviewapp "github.com/dencyuinc/layerdraw/internal/application/review"
	"github.com/dencyuinc/layerdraw/internal/desktopapp"
	"github.com/dencyuinc/layerdraw/internal/desktopcontract"
	nativeexport "github.com/dencyuinc/layerdraw/internal/exporter"
	"github.com/dencyuinc/layerdraw/internal/registry"
)

// FrontendBridge is the context-free Wails surface consumed by generated
// JavaScript. Domain methods remain on desktopapp.Application; this façade
// supplies only the app-owned lifecycle context Wails cannot inject into bound
// method parameters.
type FrontendBridge struct {
	mu       sync.RWMutex
	ctx      context.Context
	app      *desktopapp.Application
	registry registryDispatcher
}

type registryDispatcher interface {
	DispatchRegistry(context.Context, []byte) []byte
}

func NewFrontendBridge(app *desktopapp.Application, dispatchers ...registryDispatcher) *FrontendBridge {
	bridge := &FrontendBridge{ctx: context.Background(), app: app}
	if len(dispatchers) != 0 {
		bridge.registry = dispatchers[0]
	}
	return bridge
}

func (b *FrontendBridge) setContext(ctx context.Context) {
	b.mu.Lock()
	defer b.mu.Unlock()
	if ctx == nil {
		ctx = context.Background()
	}
	b.ctx = ctx
}

func (b *FrontendBridge) context() context.Context {
	b.mu.RLock()
	defer b.mu.RUnlock()
	return b.ctx
}

func (b *FrontendBridge) State() desktopcontract.LifecycleState { return b.app.State() }

func (b *FrontendBridge) Invoke(method string, exchange desktopcontract.Exchange) desktopapp.BindingResult {
	return b.app.Invoke(b.context(), method, exchange)
}

// RegistryDispatch is the single typed Wails Registry transport. The generated
// method table remains authoritative; the frontend cannot select an unrelated
// owner method for a valid Registry operation.
func (b *FrontendBridge) RegistryDispatch(requestJSON string) string {
	requestBytes := []byte(requestJSON)
	var envelope struct {
		Operation registry.WireOperation `json:"operation"`
		RequestID string                 `json:"request_id"`
	}
	if err := json.Unmarshal(requestBytes, &envelope); err != nil {
		return string(registryWireFailure(registry.WireOperation("registry.invalid"), "", registry.FailureUnsupportedFormat, "wire_request"))
	}
	request, err := registry.DecodeWireRequest(requestBytes, envelope.Operation)
	if err != nil || b.registry == nil {
		code, subject := registry.FailureUnsupportedFormat, "wire_request"
		if err == nil {
			code, subject = registry.FailureUnavailable, "desktop_registry"
		}
		return string(registryWireFailure(envelope.Operation, envelope.RequestID, code, subject))
	}
	responseBytes := b.registry.DispatchRegistry(b.context(), requestBytes)
	response, err := registry.DecodeWireResponse(responseBytes, request.Operation)
	if err != nil || response.RequestID != request.RequestID {
		return string(registryWireFailure(request.Operation, request.RequestID, registry.FailureRepairRequired, "wire_response"))
	}
	return string(responseBytes)
}

func registryWireFailure(operation registry.WireOperation, requestID, code, subject string) []byte {
	response, _ := json.Marshal(registry.WireResponse{
		WireVersion: registry.RegistryWireVersion,
		Operation:   operation,
		RequestID:   requestID,
		Failure:     &registry.WireFailure{Code: code, Subject: subject, Actionable: true},
	})
	return response
}

func (b *FrontendBridge) ReviewSnapshot() (reviewapp.Snapshot, error) {
	value, err := b.app.ReviewSnapshot()
	if err != nil {
		return reviewapp.Snapshot{}, err
	}
	snapshot, ok := value.(reviewapp.Snapshot)
	if !ok {
		return reviewapp.Snapshot{}, errors.New("desktop Review snapshot contract mismatch")
	}
	return snapshot, nil
}

type ReviewCommentRequest struct {
	ProposalID string           `json:"proposal_id"`
	Generation uint64           `json:"generation"`
	CommentID  string           `json:"comment_id"`
	Body       string           `json:"body"`
	Target     reviewapp.Target `json:"target"`
}

func (b *FrontendBridge) ReviewComment(input ReviewCommentRequest) (reviewapp.Proposal, error) {
	target, err := json.Marshal(input.Target)
	if err != nil {
		return reviewapp.Proposal{}, err
	}
	value, err := b.app.ReviewComment(b.context(), desktopapp.ReviewCommentRequest{ProposalID: input.ProposalID, Generation: input.Generation, CommentID: input.CommentID, Body: input.Body, Target: target})
	return reviewProposal(value, err)
}

func (b *FrontendBridge) ReviewApproveAndApply(input desktopapp.ReviewApprovalRequest) (reviewapp.Proposal, error) {
	value, err := b.app.ReviewApproveAndApply(b.context(), input)
	return reviewProposal(value, err)
}

func (b *FrontendBridge) ReviewWithdraw(input struct {
	ProposalID string `json:"proposal_id"`
	Generation uint64 `json:"generation"`
}) (reviewapp.Proposal, error) {
	value, err := b.app.ReviewWithdraw(b.context(), input.ProposalID, input.Generation)
	return reviewProposal(value, err)
}

func reviewProposal(value any, err error) (reviewapp.Proposal, error) {
	if err != nil {
		return reviewapp.Proposal{}, err
	}
	proposal, ok := value.(reviewapp.Proposal)
	if !ok {
		return reviewapp.Proposal{}, errors.New("desktop Review proposal contract mismatch")
	}
	return proposal, nil
}

func (b *FrontendBridge) CreateProjectDialog(requestID string) desktopcontract.Result[desktopapp.ProjectOpenResult] {
	return b.app.CreateProjectDialog(b.context(), requestID)
}

func (b *FrontendBridge) OpenProjectDialog(requestID string) desktopcontract.Result[desktopapp.ProjectOpenResult] {
	return b.app.OpenProjectDialog(b.context(), requestID)
}

func (b *FrontendBridge) RecentProjects() desktopcontract.Result[[]desktopapp.RecentProject] {
	return b.app.RecentProjects()
}

func (b *FrontendBridge) ConnectExternal(request desktopapp.ExternalConnectionRequest) desktopcontract.Result[desktopapp.ExternalConnection] {
	return b.app.ConnectExternal(b.context(), request)
}

func (b *FrontendBridge) InspectExternal(connectionID string) desktopcontract.Result[desktopapp.ExternalConnection] {
	return b.app.InspectExternal(b.context(), connectionID)
}

func (b *FrontendBridge) RefreshExternal(connectionID string) desktopcontract.Result[desktopapp.ExternalConnection] {
	return b.app.RefreshExternal(b.context(), connectionID)
}

func (b *FrontendBridge) DisconnectExternal(connectionID string) desktopcontract.Result[desktopapp.ExternalConnection] {
	return b.app.DisconnectExternal(b.context(), connectionID)
}

func (b *FrontendBridge) SelectExternalRemote(request desktopapp.ExternalRemoteSelectionRequest) desktopcontract.Result[desktopapp.ExternalBackendBinding] {
	return b.app.SelectExternalRemote(b.context(), request)
}

func (b *FrontendBridge) AcquireExternalLease(session runtimeprotocol.RuntimeSessionRef, binding desktopapp.ExternalBackendBinding) desktopcontract.Result[desktopapp.ExternalLease] {
	return b.app.AcquireExternalLease(b.context(), session, binding)
}

func (b *FrontendBridge) PlanExternalReconcile(session runtimeprotocol.RuntimeSessionRef, request desktopapp.ExternalSyncRequest, restricted bool) desktopcontract.Result[desktopapp.ExternalReconcilePlan] {
	return b.app.PlanExternalReconcile(b.context(), session, request, restricted)
}

func (b *FrontendBridge) ApplyExternalReconcile(session runtimeprotocol.RuntimeSessionRef, plan desktopapp.ExternalReconcilePlan, resolution string) desktopcontract.Result[desktopapp.ExternalReconcileResult] {
	return b.app.ApplyExternalReconcile(b.context(), session, plan, resolution)
}

func (b *FrontendBridge) NativeExportProfiles() desktopcontract.Result[[]nativeexport.Profile] {
	return b.app.NativeExportProfiles()
}

func (b *FrontendBridge) SerializeNativeExport(input nativeexport.SerializeInput) desktopcontract.Result[desktopapp.NativeSerializeResult] {
	return b.app.SerializeNativeExport(b.context(), input)
}

func (b *FrontendBridge) PublishNativeExportDialog(input desktopapp.NativePublishRequest) desktopcontract.Result[desktopapp.NativePublishResult] {
	return b.app.PublishNativeExportDialog(b.context(), input)
}

func (b *FrontendBridge) ImportExternalDialog(input desktopapp.ExternalImportRequest) desktopcontract.Result[nativeexport.ImportPreview] {
	return b.app.ImportExternalDialog(b.context(), input)
}

func (b *FrontendBridge) MCPStatus() desktopapp.MCPStatus { return b.app.MCPStatus() }

func (b *FrontendBridge) SetMCPEnabled(enabled bool, transport desktopapp.MCPTransportKind) desktopcontract.Result[desktopapp.MCPStatus] {
	return b.app.SetMCPEnabled(b.context(), enabled, transport)
}

func (b *FrontendBridge) CreateMCPConnection(request desktopapp.MCPConnectRequest) desktopcontract.Result[desktopapp.MCPConnection] {
	return b.app.CreateMCPConnection(b.context(), request)
}

func (b *FrontendBridge) ListMCPConnections() []desktopapp.MCPConnection {
	return b.app.ListMCPConnections()
}

func (b *FrontendBridge) RevokeMCPConnection(connectionID string) desktopcontract.Result[desktopapp.MCPConnection] {
	return b.app.RevokeMCPConnection(b.context(), connectionID)
}

func (b *FrontendBridge) RestartMCP() desktopcontract.Result[desktopapp.MCPStatus] {
	return b.app.RestartMCP(b.context())
}
