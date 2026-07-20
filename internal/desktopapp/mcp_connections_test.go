// SPDX-License-Identifier: LicenseRef-LayerDraw-1.0

package desktopapp

import (
	"context"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/dencyuinc/layerdraw/gen/go/protocolcommon"
	"github.com/dencyuinc/layerdraw/gen/go/semantic"
	accesscore "github.com/dencyuinc/layerdraw/internal/access"
	"github.com/dencyuinc/layerdraw/internal/desktopcontract"
	"github.com/dencyuinc/layerdraw/internal/mcphost"
)

func TestMCPExplicitControlDoesNotSilentlyStartAndFencesRestart(t *testing.T) {
	root := t.TempDir()
	config := testConfig(t, root, writeProject(t, root))
	config.MCPExplicitControl = true
	var mu sync.Mutex
	var events []string
	config.HostPorts.MCP = mcpPortStub{mu: &mu, events: &events}
	app, err := New(config)
	if err != nil {
		t.Fatal(err)
	}
	if result := app.Start(context.Background()); result.Outcome != protocolcommon.OutcomeSuccess {
		t.Fatalf("start=%+v", result)
	}
	if len(events) != 0 || app.MCPStatus().Enabled {
		t.Fatalf("MCP silently started: events=%v status=%+v", events, app.MCPStatus())
	}
	if result := app.SetMCPEnabled(context.Background(), true, MCPTransportLocal); result.Outcome != protocolcommon.OutcomeSuccess || !result.Value.Enabled {
		t.Fatalf("enable=%+v", result)
	}
	if result := app.RestartMCP(context.Background()); result.Outcome != protocolcommon.OutcomeSuccess || result.Value.Generation != 3 {
		t.Fatalf("restart=%+v", result)
	}
	if got := events; len(got) != 3 || got[0] != "start:mcp_host" || got[1] != "stop:mcp_host" || got[2] != "start:mcp_host" {
		t.Fatalf("lifecycle=%v", got)
	}
	if result := app.SetMCPEnabled(context.Background(), false, "tcp"); result.Outcome != protocolcommon.OutcomeFailed || result.Failure == nil || result.Failure.Code != desktopcontract.FailureMCPTransport {
		t.Fatalf("unsupported transport=%+v", result)
	}
}

func TestMCPConnectionsConstrainApplyRevokeExpireAndFence(t *testing.T) {
	now := desktopTestNow
	root := t.TempDir()
	config := testConfig(t, root, writeProject(t, root))
	config.MCPExplicitControl = true
	config.Now = func() time.Time { return now }
	app, err := New(config)
	if err != nil {
		t.Fatal(err)
	}
	if result := app.Start(context.Background()); result.Outcome != protocolcommon.OutcomeSuccess {
		t.Fatalf("start=%+v", result)
	}
	if result := app.SetMCPEnabled(context.Background(), true, MCPTransportLocal); result.Outcome != protocolcommon.OutcomeSuccess {
		t.Fatalf("enable=%+v", result)
	}
	opened := app.OpenProject(context.Background(), "opaque")
	if opened.Outcome != protocolcommon.OutcomeSuccess {
		t.Fatalf("open=%+v", opened)
	}
	request := MCPConnectRequest{
		ClientID: "codex", ProtocolVersion: MCPConnectionProtocolVersion, DocumentID: opened.Value.ProjectID,
		AgentID: "agent-a", Capabilities: []semantic.AuthoringCapability{semantic.AuthoringCapabilityGraphWrite},
		Permissions: accesscore.AgentPermissions{Read: true, Propose: true, Apply: true}, ExpiresAt: protocolcommon.Rfc3339Time(now.Add(time.Hour).Format(time.RFC3339Nano)),
	}
	if result := app.CreateMCPConnection(context.Background(), request); result.Outcome != protocolcommon.OutcomeFailed {
		t.Fatalf("unconfirmed apply accepted: %+v", result)
	}
	request.ConfirmApply = true
	created := app.CreateMCPConnection(context.Background(), request)
	if created.Outcome != protocolcommon.OutcomeSuccess || created.Value.Status != MCPConnectionConnected || created.Value.AgentID != "agent-a" {
		t.Fatalf("create=%+v", created)
	}
	if result := app.RevokeMCPConnection(context.Background(), created.Value.ConnectionID); result.Outcome != protocolcommon.OutcomeSuccess || result.Value.Status != MCPConnectionRevoked {
		t.Fatalf("revoke=%+v", result)
	}

	request.AgentID = "agent-b"
	request.Permissions.Apply, request.ConfirmApply = false, false
	request.ExpiresAt = protocolcommon.Rfc3339Time(now.Add(time.Minute).Format(time.RFC3339Nano))
	proposal := app.CreateMCPConnection(context.Background(), request)
	if proposal.Outcome != protocolcommon.OutcomeSuccess {
		t.Fatalf("proposal connection=%+v", proposal)
	}
	denied := app.MCPCallConnectionTool(context.Background(), proposal.Value.ConnectionID, mcphost.CallToolRequest{Name: "layerdraw.apply_operations", RequestID: "apply", Arguments: []byte(`{}`)})
	if denied.Failure == nil || denied.Failure.Code != mcphost.ErrorCapabilityUnavailable {
		t.Fatalf("proposal-only apply=%+v", denied)
	}
	stale := app.MCPCallConnectionTool(context.Background(), proposal.Value.ConnectionID, mcphost.CallToolRequest{Name: "layerdraw.list_modules", RequestID: "read", Arguments: []byte(`{}`), Binding: &mcphost.Binding{DocumentID: request.DocumentID, RevisionDigest: proposal.Value.GrantSummary.AccessFingerprint, AccessFingerprint: "sha256:ffffffffffffffffffffffffffffffffffffffffffffffffffffffffffffffff"}})
	if stale.Failure == nil || stale.Failure.Code != mcphost.ErrorStaleBinding {
		t.Fatalf("stale binding=%+v", stale)
	}
	now = now.Add(2 * time.Minute)
	connections := app.ListMCPConnections()
	statuses := map[string]MCPConnection{}
	for _, connection := range connections {
		statuses[connection.ConnectionID] = connection
	}
	if len(connections) != 2 || statuses[proposal.Value.ConnectionID].Status != MCPConnectionExpired || statuses[proposal.Value.ConnectionID].Permissions.Apply {
		t.Fatalf("connections=%+v", connections)
	}
	metadata := filepath.Join(config.Root, mcpConnectionFilename)
	if info, err := os.Stat(metadata); err != nil || info.Mode().Perm() != 0o600 {
		t.Fatalf("metadata mode: info=%v err=%v", info, err)
	}
	_, restored, err := loadMCPConnectionStore(config.Root, now)
	if err != nil || restored[created.Value.ConnectionID].Status != MCPConnectionRevoked || restored[proposal.Value.ConnectionID].Status != MCPConnectionExpired {
		t.Fatalf("restored=%+v err=%v", restored, err)
	}
}
