// SPDX-License-Identifier: LicenseRef-LayerDraw-1.0

package desktopapp

import (
	"context"

	"github.com/dencyuinc/layerdraw/gen/go/protocolcommon"
	"github.com/dencyuinc/layerdraw/internal/desktopcontract"
	"github.com/dencyuinc/layerdraw/internal/mcphost"
)

// BindCanonicalMCPHost adapts the in-process canonical MCP Host to the Desktop
// lifecycle port. Desktop never starts or advertises a separate MCP binary.
func BindCanonicalMCPHost(host *mcphost.Host) desktopcontract.MCPTransportPort {
	if host == nil {
		return nil
	}
	return canonicalMCPPort{host: host}
}

type canonicalMCPPort struct{ host *mcphost.Host }

func (p canonicalMCPPort) Start(ctx context.Context) desktopcontract.Result[struct{}] {
	if p.host == nil || p.host.Start(ctx) != nil {
		return mcpFailure()
	}
	return desktopcontract.Result[struct{}]{Outcome: protocolcommon.OutcomeSuccess}
}

func (p canonicalMCPPort) Shutdown(ctx context.Context) desktopcontract.Result[struct{}] {
	if p.host == nil || p.host.Shutdown(ctx) != nil {
		return mcpFailure()
	}
	return desktopcontract.Result[struct{}]{Outcome: protocolcommon.OutcomeSuccess}
}

func mcpFailure() desktopcontract.Result[struct{}] {
	return desktopcontract.Result[struct{}]{Outcome: protocolcommon.OutcomeFailed, Failure: &desktopcontract.Failure{Code: desktopcontract.FailureMCPTransport, Component: desktopcontract.ComponentMCPHost, Retryable: true, Recovery: desktopcontract.RecoveryReconnect}}
}
