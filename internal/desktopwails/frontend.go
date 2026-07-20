// SPDX-License-Identifier: LicenseRef-LayerDraw-1.0

package desktopwails

import (
	"context"
	"sync"

	"github.com/dencyuinc/layerdraw/internal/desktopapp"
	"github.com/dencyuinc/layerdraw/internal/desktopcontract"
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
