// SPDX-License-Identifier: LicenseRef-LayerDraw-1.0

package desktopwails

import (
	"context"
	"sync"

	"github.com/dencyuinc/layerdraw/gen/go/runtimeprotocol"
	"github.com/dencyuinc/layerdraw/internal/desktopapp"
	"github.com/dencyuinc/layerdraw/internal/desktopcontract"
	nativeexport "github.com/dencyuinc/layerdraw/internal/exporter"
)

// FrontendBridge is the context-free Wails surface consumed by generated
// JavaScript. Domain methods remain on desktopapp.Application; this façade
// supplies only the app-owned lifecycle context Wails cannot inject into bound
// method parameters.
type FrontendBridge struct {
	mu  sync.RWMutex
	ctx context.Context
	app *desktopapp.Application
}

func NewFrontendBridge(app *desktopapp.Application) *FrontendBridge {
	return &FrontendBridge{ctx: context.Background(), app: app}
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
