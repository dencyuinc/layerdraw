// SPDX-License-Identifier: LicenseRef-LayerDraw-1.0

package host

import (
	"context"

	"github.com/dencyuinc/layerdraw/internal/runtime"
	"github.com/dencyuinc/layerdraw/internal/runtime/port"
)

const (
	OperationSearch       = "runtime.search"
	OperationExecuteQuery = "runtime.execute_query"
	OperationAnalyzeGraph = "runtime.analyze_graph"
)

// ConsumerSearchSurface is the one framework-neutral instance shared by Wails
// bindings and MCP Host composition. Neither consumer can substitute ranking,
// Access filtering, plan execution, or capability semantics.
type ConsumerSearchSurface interface {
	Capabilities(context.Context) (runtime.SearchCapabilityManifest, error)
	Search(context.Context, runtime.SearchRequest) ([]byte, error)
	ExecuteQuery(context.Context, port.BoundExecutionRequest) ([]byte, error)
	ExecuteAnalysis(context.Context, port.BoundExecutionRequest) ([]byte, error)
}

func (e *Endpoint) SearchSurface() ConsumerSearchSurface { return e.search }
