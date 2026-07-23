// SPDX-License-Identifier: LicenseRef-LayerDraw-1.0

package desktopwails

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"sync"

	"github.com/dencyuinc/layerdraw/gen/go/engineprotocol"
	"github.com/dencyuinc/layerdraw/gen/go/protocolcommon"
	"github.com/dencyuinc/layerdraw/gen/go/runtimeprotocol"
	"github.com/dencyuinc/layerdraw/gen/go/semantic"
	reviewapp "github.com/dencyuinc/layerdraw/internal/application/review"
	"github.com/dencyuinc/layerdraw/internal/desktopapp"
	"github.com/dencyuinc/layerdraw/internal/desktopcontract"
	engineendpoint "github.com/dencyuinc/layerdraw/internal/engine/endpoint"
	nativeexport "github.com/dencyuinc/layerdraw/internal/exporter"
	"github.com/dencyuinc/layerdraw/internal/localdocument"
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
	shell    *desktopapp.NativeShell
	registry registryDispatcher
	preview  editorPreviewDispatcher
}

type registryDispatcher interface {
	DispatchRegistry(context.Context, []byte) []byte
}

type editorPreviewDispatcher interface {
	PreviewEditor(context.Context, runtimeprotocol.PreviewOperationsInput) (localdocument.EditorPreviewResult, error)
	MaterializeProjectView(context.Context, runtimeprotocol.RuntimeSessionRef, string) (semantic.ViewData, error)
	ProjectDocumentGeneration(context.Context, runtimeprotocol.RuntimeSessionRef) (engineprotocol.DocumentGeneration, error)
	ProjectSubjects(context.Context, runtimeprotocol.RuntimeSessionRef) ([]semantic.SemanticSubject, error)
	ProjectStructure(context.Context, runtimeprotocol.RuntimeSessionRef) (engineendpoint.BridgeStructure, error)
}

func NewFrontendBridge(app *desktopapp.Application, dispatchers ...registryDispatcher) *FrontendBridge {
	bridge := &FrontendBridge{ctx: context.Background(), app: app}
	if len(dispatchers) != 0 {
		bridge.registry = dispatchers[0]
		bridge.preview, _ = dispatchers[0].(editorPreviewDispatcher)
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

func (b *FrontendBridge) ProjectPublication() (desktopapp.ProjectPublicationDTO, error) {
	return b.app.ProjectPublication(b.context())
}

func (b *FrontendBridge) PreviewEditor(input runtimeprotocol.PreviewOperationsInput) (localdocument.EditorPreviewResult, error) {
	if b.preview == nil {
		return localdocument.EditorPreviewResult{}, errors.New("desktop editor preview is unavailable")
	}
	return b.preview.PreviewEditor(b.context(), input)
}

// ProjectSubjects lists Engine-compiled semantic subjects for the session's
// working document (authoring schema and outline listings).
func (b *FrontendBridge) ProjectSubjects(session runtimeprotocol.RuntimeSessionRef) ([]semantic.SemanticSubject, error) {
	if b.preview == nil {
		return nil, errors.New("desktop subject listing is unavailable")
	}
	return b.preview.ProjectSubjects(b.context(), session)
}

// ProjectOpenSession hands the app-owned runtime session for the published
// project to the trusted frontend; the frontend adopts it instead of opening
// a parallel session.
func (b *FrontendBridge) ProjectOpenSession(input runtimeprotocol.OpenRuntimeDocumentInput) (runtimeprotocol.OpenRuntimeDocumentResult, error) {
	return b.app.ActiveProjectSession(input.DocumentID)
}

// ProjectStructure returns the master-document structure projection the
// Desktop Structure editor renders (layers, types, entities, relations).
func (b *FrontendBridge) ProjectStructure(session runtimeprotocol.RuntimeSessionRef) (engineendpoint.BridgeStructure, error) {
	if b.preview == nil {
		return engineendpoint.BridgeStructure{}, errors.New("desktop structure read is unavailable")
	}
	return b.preview.ProjectStructure(b.context(), session)
}

// ProjectDocumentGeneration binds Engine reads (find_symbols and friends) to
// the session's working document; the frontend treats the value as opaque.
func (b *FrontendBridge) ProjectDocumentGeneration(session runtimeprotocol.RuntimeSessionRef) (engineprotocol.DocumentGeneration, error) {
	if b.preview == nil {
		return engineprotocol.DocumentGeneration{}, errors.New("desktop document generation is unavailable")
	}
	return b.preview.ProjectDocumentGeneration(b.context(), session)
}

type ProjectViewMaterialization struct {
	ViewData     semantic.ViewData     `json:"view_data"`
	ViewDataHash protocolcommon.Digest `json:"view_data_hash"`
}

func (b *FrontendBridge) MaterializeProjectView(session runtimeprotocol.RuntimeSessionRef, address string) (ProjectViewMaterialization, error) {
	if b.preview == nil {
		return ProjectViewMaterialization{}, errors.New("desktop view materialization is unavailable")
	}
	viewData, err := b.preview.MaterializeProjectView(b.context(), session, address)
	if err != nil {
		return ProjectViewMaterialization{}, err
	}
	encoded, err := semantic.EncodeViewData(viewData)
	if err != nil {
		return ProjectViewMaterialization{}, err
	}
	digest := sha256.Sum256(encoded)
	return ProjectViewMaterialization{ViewData: viewData, ViewDataHash: protocolcommon.Digest(fmt.Sprintf("sha256:%x", digest))}, nil
}

func (b *FrontendBridge) Invoke(method string, exchange desktopcontract.Exchange) desktopapp.BindingResult {
	result := b.app.Invoke(b.context(), method, exchange)
	if result.Failure == nil && result.Value.Blobs == nil {
		result.Value.Blobs = []desktopcontract.Blob{}
	}
	return result
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

type ReviewWithdrawRequest struct {
	ProposalID string `json:"proposal_id"`
	Generation uint64 `json:"generation"`
}

func (b *FrontendBridge) ReviewWithdraw(input ReviewWithdrawRequest) (reviewapp.Proposal, error) {
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

// CloseCurrentProject closes the active project session so the shell returns
// to the hub without a process restart. Closing with no open session succeeds.
func (b *FrontendBridge) CloseCurrentProject() desktopcontract.Result[runtimeprotocol.CloseDocumentResult] {
	sessions := b.app.ActiveSessions()
	if len(sessions) == 0 {
		return desktopcontract.Result[runtimeprotocol.CloseDocumentResult]{Outcome: protocolcommon.OutcomeSuccess}
	}
	return b.app.CloseProject(b.context(), sessions[0])
}

func (b *FrontendBridge) OpenRecentProject(projectID string) desktopcontract.Result[desktopapp.ProjectOpenResult] {
	return b.app.OpenRecentProject(b.context(), runtimeprotocol.DocumentID(projectID))
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

// MCPClientConfig returns the copy-paste MCP client configuration for a
// connection created in Settings (the Desktop binary as a stdio server).
func (b *FrontendBridge) MCPClientConfig(connectionID string) string {
	return MCPClientConfigJSON(connectionID)
}

func (b *FrontendBridge) DeleteMCPConnection(connectionID string) desktopcontract.Result[desktopapp.MCPConnection] {
	return b.app.DeleteMCPConnection(connectionID)
}

func (b *FrontendBridge) SetMCPEnabled(enabled bool, transport desktopapp.MCPTransportKind) desktopcontract.Result[desktopapp.MCPStatus] {
	result := b.app.SetMCPEnabled(b.context(), enabled, transport)
	if result.Outcome == protocolcommon.OutcomeSuccess {
		b.persistMCPEnabled(enabled)
	}
	return result
}

// persistMCPEnabled mirrors the AI-connection switch into native settings so
// the next launch restores it; failures stay silent because the runtime state
// already changed and remains authoritative for this session.
func (b *FrontendBridge) persistMCPEnabled(enabled bool) {
	b.mu.Lock()
	shell := b.shell
	b.mu.Unlock()
	if shell == nil {
		return
	}
	current := shell.CurrentSettings(b.context())
	if current.Outcome != protocolcommon.OutcomeSuccess || current.Value.MCPEnabled == enabled {
		return
	}
	next := current.Value
	next.MCPEnabled = enabled
	_ = shell.UpdateSettings(b.context(), next)
}

// AttachNativeShell hands the bridge the settings owner; production Run wires
// it, probe compositions leave it absent.
func (b *FrontendBridge) attachNativeShell(shell *desktopapp.NativeShell) {
	b.mu.Lock()
	b.shell = shell
	b.mu.Unlock()
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
