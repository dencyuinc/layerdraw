// SPDX-License-Identifier: LicenseRef-LayerDraw-1.0

package mcphost

import (
	"context"
	"errors"
	"sync"
)

// LocalTransport is the in-process transport embedded by Desktop. Native IPC
// and Wails adapters call this exact surface; it never launches a sibling MCP
// executable and never bypasses Host lifecycle or generation fencing.
type LocalTransport struct {
	mu         sync.RWMutex
	handler    Handler
	generation uint64
}

func (t *LocalTransport) Start(_ context.Context, handler Handler) error {
	if handler == nil {
		return errors.New("mcp local handler is nil")
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	if t.handler != nil {
		return errors.New("mcp local transport already started")
	}
	t.generation++
	t.handler = handler
	return nil
}
func (t *LocalTransport) Shutdown(context.Context) error {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.handler = nil
	return nil
}
func (t *LocalTransport) current() (Handler, error) {
	t.mu.RLock()
	defer t.mu.RUnlock()
	if t.handler == nil {
		return nil, errors.New("mcp local transport is stopped")
	}
	return t.handler, nil
}
func (t *LocalTransport) ListTools(ctx context.Context) ([]Tool, *Failure) {
	handler, err := t.current()
	if err != nil {
		return nil, fail(ErrorTransport, true)
	}
	return handler.ListTools(ctx)
}
func (t *LocalTransport) CallTool(ctx context.Context, request CallToolRequest) CallToolResult {
	handler, err := t.current()
	if err != nil {
		return CallToolResult{Failure: fail(ErrorTransport, true)}
	}
	return handler.CallTool(ctx, request)
}
func (t *LocalTransport) ListResources(ctx context.Context) ([]Resource, *Failure) {
	handler, err := t.current()
	if err != nil {
		return nil, fail(ErrorTransport, true)
	}
	return handler.ListResources(ctx)
}
func (t *LocalTransport) ReadResource(ctx context.Context, request ReadResourceRequest) ReadResourceResult {
	handler, err := t.current()
	if err != nil {
		return ReadResourceResult{Failure: fail(ErrorTransport, true)}
	}
	return handler.ReadResource(ctx, request)
}
