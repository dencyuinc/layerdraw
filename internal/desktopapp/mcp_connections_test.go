// SPDX-License-Identifier: LicenseRef-LayerDraw-1.0

package desktopapp

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"runtime"
	"sync"
	"testing"
	"time"

	"github.com/dencyuinc/layerdraw/gen/go/accessprotocol"
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
	now := time.Now().UTC().Truncate(time.Second)
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
	request.ExpiresAt = protocolcommon.Rfc3339Time(now.Add(30 * time.Minute).Format(time.RFC3339Nano))
	proposal := app.CreateMCPConnection(context.Background(), request)
	if proposal.Outcome != protocolcommon.OutcomeSuccess {
		t.Fatalf("proposal connection=%+v failure=%+v", proposal, proposal.Failure)
	}
	denied := app.MCPCallConnectionTool(context.Background(), proposal.Value.ConnectionID, mcphost.CallToolRequest{Name: "layerdraw.apply_operations", RequestID: "apply", Arguments: []byte(`{}`)})
	if denied.Failure == nil || denied.Failure.Code != mcphost.ErrorCapabilityUnavailable {
		t.Fatalf("proposal-only apply=%+v", denied)
	}
	stale := app.MCPCallConnectionTool(context.Background(), proposal.Value.ConnectionID, mcphost.CallToolRequest{Name: "layerdraw.list_modules", RequestID: "read", Arguments: []byte(`{}`), Binding: &mcphost.Binding{DocumentID: request.DocumentID, RevisionDigest: proposal.Value.GrantSummary.AccessFingerprint, AccessFingerprint: "sha256:ffffffffffffffffffffffffffffffffffffffffffffffffffffffffffffffff"}})
	if stale.Failure == nil || stale.Failure.Code != mcphost.ErrorStaleBinding {
		t.Fatalf("stale binding=%+v", stale)
	}
	now = now.Add(31 * time.Minute)
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
	_, _, restored, err := loadMCPConnectionStore(config.Root, now)
	if err != nil || restored[created.Value.ConnectionID].Status != MCPConnectionRevoked || restored[proposal.Value.ConnectionID].Status != MCPConnectionExpired {
		t.Fatalf("restored=%+v err=%v", restored, err)
	}
}

func TestMCPConnectionToolsClampCapabilitiesToTheDelegatedGrant(t *testing.T) {
	digest := testMCPDigest()
	capability := mcphost.OperationCapability{Enabled: true, InputSchema: json.RawMessage(`{"type":"object"}`), OutputSchema: json.RawMessage(`{"type":"object"}`)}
	snapshot := mcphost.CapabilitySnapshot{
		ManifestETag: protocolcommon.ManifestETag(digest),
		Operations: map[string]mcphost.OperationCapability{
			"engine.list_modules": capability, "runtime.preview_operations": capability, "runtime.commit_operations": capability,
		},
		Resources:    []mcphost.ResourceCapability{{URI: "layerdraw://project/summary", Name: "summary", Description: "summary", MimeType: "application/json", Schema: json.RawMessage(`{"type":"object"}`)}},
		GrantSummary: accessprotocol.AuthoringGrantSummary{AccessFingerprint: digest, ConstrainedCapabilities: []semantic.AuthoringCapability{}, GrantedCapabilities: []semantic.AuthoringCapability{}, PolicyEtag: digest},
	}
	root := t.TempDir()
	config := testConfig(t, root, writeProject(t, root))
	config.Bindings = completeClients(t)
	config.HostPorts.MCP = nil
	config.MCPCapabilities = mcpCapabilitySourceStub{snapshot: snapshot}
	config.Root = filepath.Join(root, "canonical")
	app, err := NewCanonical(config)
	if err != nil {
		t.Fatal(err)
	}
	if result := app.Start(context.Background()); result.Outcome != protocolcommon.OutcomeSuccess {
		t.Fatalf("start=%+v", result)
	}
	defer app.Shutdown(context.Background())
	if result := app.SetMCPEnabled(context.Background(), true, MCPTransportLocal); result.Outcome != protocolcommon.OutcomeSuccess {
		t.Fatalf("enable=%+v", result)
	}
	connection := MCPConnection{
		ConnectionID: "connection", ClientID: "client", SessionID: "session", ProtocolVersion: MCPConnectionProtocolVersion,
		DocumentID: "document-1", DelegationID: "delegation", AgentID: "agent", GrantSummary: snapshot.GrantSummary,
		Permissions: accesscore.AgentPermissions{Read: true, Propose: true}, ExpiresAt: protocolcommon.Rfc3339Time(time.Now().Add(time.Hour).UTC().Format(time.RFC3339Nano)),
		Generation: "1", Status: MCPConnectionConnected,
	}
	app.mcpMu.Lock()
	app.mcpConnections[connection.ConnectionID] = connection
	app.mcpMu.Unlock()

	tools, failure := app.MCPListConnectionTools(context.Background(), connection.ConnectionID)
	if failure != nil {
		t.Fatalf("list tools=%+v", failure)
	}
	names := map[string]bool{}
	for _, tool := range tools {
		names[tool.Name] = true
	}
	if !names["layerdraw.get_capabilities"] || !names["layerdraw.list_modules"] || !names["layerdraw.preview_operations"] || names["layerdraw.apply_operations"] {
		t.Fatalf("clamped tools=%v", names)
	}
	result := app.MCPCallConnectionTool(context.Background(), connection.ConnectionID, mcphost.CallToolRequest{Name: "layerdraw.get_capabilities", Arguments: json.RawMessage(`{}`)})
	if result.Failure != nil {
		t.Fatalf("capabilities=%+v", result)
	}
	var clamped mcphost.CapabilitySnapshot
	if err := json.Unmarshal(result.Content, &clamped); err != nil {
		t.Fatal(err)
	}
	if _, ok := clamped.Operations["runtime.commit_operations"]; ok || len(clamped.Resources) != 1 || clamped.GrantSummary.AccessFingerprint != digest {
		t.Fatalf("clamped snapshot=%+v", clamped)
	}
	invalid := app.MCPCallConnectionTool(context.Background(), connection.ConnectionID, mcphost.CallToolRequest{Name: "layerdraw.get_capabilities", Arguments: json.RawMessage(`{"unexpected":true}`)})
	if invalid.Failure == nil || invalid.Failure.Code != mcphost.ErrorInvalidRequest {
		t.Fatalf("invalid capabilities request=%+v", invalid)
	}
	unknown := app.MCPCallConnectionTool(context.Background(), connection.ConnectionID, mcphost.CallToolRequest{Name: "layerdraw.unknown", Arguments: json.RawMessage(`{}`)})
	if unknown.Failure == nil || unknown.Failure.Code != mcphost.ErrorCapabilityUnavailable {
		t.Fatalf("unknown tool=%+v", unknown)
	}
	connection.Permissions.Read = false
	connection.Permissions.Propose = false
	app.mcpMu.Lock()
	app.mcpConnections[connection.ConnectionID] = connection
	app.mcpMu.Unlock()
	result = app.MCPCallConnectionTool(context.Background(), connection.ConnectionID, mcphost.CallToolRequest{Name: "layerdraw.get_capabilities", Arguments: json.RawMessage(`{}`)})
	clamped = mcphost.CapabilitySnapshot{}
	if result.Failure != nil || json.Unmarshal(result.Content, &clamped) != nil || len(clamped.Resources) != 0 || len(clamped.Operations) != 0 {
		t.Fatalf("no-read snapshot=%+v decoded=%+v", result, clamped)
	}
}

func TestMCPConnectionClosedFailureAndPermissionBranches(t *testing.T) {
	app, err := New(testConfig(t, t.TempDir(), ""))
	if err != nil {
		t.Fatal(err)
	}
	if result := app.SetMCPEnabled(context.Background(), true, MCPTransportLocal); result.Outcome != protocolcommon.OutcomeFailed {
		t.Fatalf("enabled before ready=%+v", result)
	}
	bad := MCPConnectRequest{ProtocolVersion: "old"}
	if result := app.CreateMCPConnection(context.Background(), bad); result.Failure == nil || result.Failure.Code != desktopcontract.FailureMCPVersionMismatch {
		t.Fatalf("version mismatch=%+v", result)
	}
	bad.ProtocolVersion = MCPConnectionProtocolVersion
	if result := app.CreateMCPConnection(context.Background(), bad); result.Failure == nil || result.Failure.Code != desktopcontract.FailureMCPScopeDenied {
		t.Fatalf("invalid grant=%+v", result)
	}
	bad.ClientID, bad.AgentID, bad.ExpiresAt = "client", "agent", protocolcommon.Rfc3339Time(time.Now().Add(time.Hour).Format(time.RFC3339Nano))
	if result := app.CreateMCPConnection(context.Background(), bad); result.Failure == nil || result.Failure.Code != desktopcontract.FailureMCPDisabled {
		t.Fatalf("disabled=%+v", result)
	}
	if result := app.RevokeMCPConnection(context.Background(), "missing"); result.Outcome != protocolcommon.OutcomeFailed {
		t.Fatalf("missing revoke=%+v", result)
	}
	if _, _, failure := app.beginMCPCall(context.Background(), "missing", ""); failure == nil {
		t.Fatal("missing connection call accepted")
	}
	for permission, allowed := range map[mcphost.ToolPermission]bool{
		mcphost.ToolPermissionRead: true, mcphost.ToolPermissionExport: true, mcphost.ToolPermissionPropose: true, mcphost.ToolPermissionApply: true, "unknown": false,
	} {
		if got := connectionAllows(MCPConnection{Permissions: accesscore.AgentPermissions{Read: true, Export: true, Propose: true, Apply: true}}, permission); got != allowed {
			t.Fatalf("permission %q=%v want %v", permission, got, allowed)
		}
	}
	if permission, ok := toolPermission("layerdraw.apply_operations"); !ok || permission != mcphost.ToolPermissionApply {
		t.Fatalf("apply permission=%q %v", permission, ok)
	}
	if _, ok := toolPermission("layerdraw.unknown"); ok {
		t.Fatal("unknown tool mapped")
	}
}

func TestMCPConnectionStoreRejectsUnsafeMetadataAndRestartsLiveRecords(t *testing.T) {
	now := time.Now().UTC().Truncate(time.Second)
	if _, _, connections, err := loadMCPConnectionStore(filepath.Join(t.TempDir(), "missing"), now); err != nil || len(connections) != 0 {
		t.Fatalf("missing store: connections=%v err=%v", connections, err)
	}
	root := t.TempDir()
	path := filepath.Join(root, mcpConnectionFilename)
	if err := os.Mkdir(path, 0o700); err != nil {
		t.Fatal(err)
	}
	if _, _, _, err := loadMCPConnectionStore(root, now); err == nil {
		t.Fatal("directory metadata accepted")
	}
	if err := os.Remove(path); err != nil {
		t.Fatal(err)
	}
	write := func(data []byte, mode os.FileMode) {
		t.Helper()
		if err := os.WriteFile(path, data, mode); err != nil {
			t.Fatal(err)
		}
		if err := os.Chmod(path, mode); err != nil {
			t.Fatal(err)
		}
	}
	write([]byte(`{"version":1,"generation":0,"connections":[]}`), 0o644)
	if _, _, _, err := loadMCPConnectionStore(root, now); runtime.GOOS != "windows" && err == nil {
		t.Fatal("world-readable Unix metadata accepted")
	} else if runtime.GOOS == "windows" && err != nil {
		t.Fatalf("private Windows ACL rejected because of synthetic mode bits: %v", err)
	}
	write([]byte(`{"version":2,"generation":0,"connections":[]}`), 0o600)
	if _, _, _, err := loadMCPConnectionStore(root, now); err == nil {
		t.Fatal("unknown metadata version accepted")
	}
	write([]byte(`{"version":1,"generation":0,"connections":[]} {}`), 0o600)
	if _, _, _, err := loadMCPConnectionStore(root, now); err == nil {
		t.Fatal("trailing metadata accepted")
	}
	digest := testMCPDigest()
	base := MCPConnection{
		ConnectionID: "connection", ClientID: "client", SessionID: "session", ProtocolVersion: MCPConnectionProtocolVersion,
		DocumentID: "document", DelegationID: "delegation", AgentID: "agent", Capabilities: []semantic.AuthoringCapability{},
		GrantSummary: accessprotocol.AuthoringGrantSummary{AccessFingerprint: digest, ConstrainedCapabilities: []semantic.AuthoringCapability{}, GrantedCapabilities: []semantic.AuthoringCapability{}, PolicyEtag: digest},
		Permissions:  accesscore.AgentPermissions{Read: true}, ExpiresAt: protocolcommon.Rfc3339Time(now.Add(time.Hour).Format(time.RFC3339Nano)), Generation: "1", Status: MCPConnectionConnected,
	}
	if !validStoredMCPConnection(base) {
		t.Fatal("valid record rejected")
	}
	invalid := base
	invalid.Status = "unknown"
	if validStoredMCPConnection(invalid) {
		t.Fatal("unknown status accepted")
	}
	invalid = base
	invalid.ExpiresAt = "invalid"
	if validStoredMCPConnection(invalid) {
		t.Fatal("invalid expiry accepted")
	}
	duplicate, err := json.Marshal(mcpConnectionSnapshot{Version: 1, Connections: []MCPConnection{base, base}})
	if err != nil {
		t.Fatal(err)
	}
	write(duplicate, 0o600)
	if _, _, _, err := loadMCPConnectionStore(root, now); err == nil {
		t.Fatal("duplicate connection accepted")
	}
	revoking := base
	revoking.ConnectionID, revoking.SessionID, revoking.DelegationID, revoking.Status = "revoking", "session-2", "delegation-2", MCPConnectionRevoking
	expired := base
	expired.ConnectionID, expired.SessionID, expired.DelegationID = "expired", "session-3", "delegation-3"
	expired.ExpiresAt = protocolcommon.Rfc3339Time(now.Add(-time.Second).Format(time.RFC3339Nano))
	data, err := json.Marshal(mcpConnectionSnapshot{Version: 1, Generation: 7, Connections: []MCPConnection{base, revoking, expired}})
	if err != nil {
		t.Fatal(err)
	}
	write(data, 0o600)
	_, generation, restored, err := loadMCPConnectionStore(root, now)
	if err != nil || generation != 7 || restored[base.ConnectionID].Status != MCPConnectionRestarted || restored[revoking.ConnectionID].Status != MCPConnectionRestarted || restored[expired.ConnectionID].Status != MCPConnectionExpired {
		t.Fatalf("restored=%+v err=%v", restored, err)
	}
}

func TestMCPDisableFencesConnectionsAndCancelsInflightCalls(t *testing.T) {
	root := t.TempDir()
	config := testConfig(t, root, writeProject(t, root))
	config.MCPExplicitControl = true
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
	connection := MCPConnection{ConnectionID: "connection", Status: MCPConnectionConnected, ExpiresAt: protocolcommon.Rfc3339Time(time.Now().Add(time.Hour).UTC().Format(time.RFC3339Nano))}
	app.mcpMu.Lock()
	app.mcpConnections[connection.ConnectionID] = connection
	app.mcpMu.Unlock()
	_, call, failure := app.beginMCPCall(context.Background(), connection.ConnectionID, "")
	if failure != nil {
		t.Fatalf("begin=%+v", failure)
	}
	if result := app.SetMCPEnabled(context.Background(), false, MCPTransportLocal); result.Outcome != protocolcommon.OutcomeSuccess || result.Value.Enabled {
		t.Fatalf("disable=%+v", result)
	}
	select {
	case <-call.Context.Done():
	default:
		t.Fatal("in-flight call was not cancelled")
	}
	connections := app.ListMCPConnections()
	if len(connections) != 1 || connections[0].Status != MCPConnectionRestarted {
		t.Fatalf("fenced=%+v", connections)
	}
	if result := app.SetMCPEnabled(context.Background(), false, MCPTransportLocal); result.Outcome != protocolcommon.OutcomeSuccess {
		t.Fatalf("idempotent disable=%+v", result)
	}
	call.done()

	app.mcpMu.Lock()
	connection.Status = MCPConnectionConnected
	app.mcpConnections[connection.ConnectionID] = connection
	app.mcpMu.Unlock()
	if result := app.RevokeMCPConnection(context.Background(), connection.ConnectionID); result.Outcome != protocolcommon.OutcomeFailed {
		t.Fatalf("revoke without canonical host=%+v", result)
	}
	if got := app.ListMCPConnections(); len(got) != 1 || got[0].Status != MCPConnectionConnected {
		t.Fatalf("failed revoke did not restore connection: %+v", got)
	}
}

func TestMCPGenerationRestoresAndShutdownFencesAcrossSameApplicationRestart(t *testing.T) {
	root := t.TempDir()
	config := testConfig(t, root, writeProject(t, root))
	config.MCPExplicitControl = true
	if err := os.MkdirAll(config.Root, 0o700); err != nil {
		t.Fatal(err)
	}
	store := &mcpConnectionStore{root: config.Root}
	if err := store.save(9, map[string]MCPConnection{}); err != nil {
		t.Fatal(err)
	}
	app, err := New(config)
	if err != nil {
		t.Fatal(err)
	}
	if got := app.MCPStatus().Generation; got != 9 {
		t.Fatalf("restored generation=%d", got)
	}
	if result := app.Start(context.Background()); result.Outcome != protocolcommon.OutcomeSuccess {
		t.Fatalf("start=%+v", result)
	}
	if result := app.SetMCPEnabled(context.Background(), true, MCPTransportLocal); result.Outcome != protocolcommon.OutcomeSuccess || result.Value.Generation != 10 {
		t.Fatalf("enable=%+v", result)
	}
	connection := MCPConnection{ConnectionID: "connection", ClientID: "client", SessionID: "session", ProtocolVersion: MCPConnectionProtocolVersion, DocumentID: "document", DelegationID: "delegation", AgentID: "agent", Generation: "1", ExpiresAt: protocolcommon.Rfc3339Time(time.Now().Add(time.Hour).UTC().Format(time.RFC3339Nano)), Status: MCPConnectionConnected}
	app.mcpMu.Lock()
	app.mcpConnections[connection.ConnectionID] = connection
	app.mcpMu.Unlock()
	_, call, failure := app.beginMCPCall(context.Background(), connection.ConnectionID, "")
	if failure != nil {
		t.Fatalf("begin=%+v", failure)
	}
	if result := app.Shutdown(context.Background()); result.Outcome != protocolcommon.OutcomeSuccess {
		t.Fatalf("shutdown=%+v", result)
	}
	if status := app.MCPStatus(); status.Enabled || status.Generation != 11 {
		t.Fatalf("shutdown status=%+v", status)
	}
	select {
	case <-call.Context.Done():
	default:
		t.Fatal("shutdown retained in-flight MCP call")
	}
	if got := app.ListMCPConnections(); len(got) != 1 || got[0].Status != MCPConnectionRestarted {
		t.Fatalf("shutdown connections=%+v", got)
	}
	if result := app.Start(context.Background()); result.Outcome != protocolcommon.OutcomeSuccess {
		t.Fatalf("same-application restart=%+v", result)
	}
	if result := app.SetMCPEnabled(context.Background(), true, MCPTransportLocal); result.Outcome != protocolcommon.OutcomeSuccess || result.Value.Generation != 12 {
		t.Fatalf("re-enable=%+v", result)
	}
	call.done()
}

func TestMCPTransitionsSerializeAndRollbackPersistenceFailure(t *testing.T) {
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
	var wait sync.WaitGroup
	results := make(chan desktopcontract.Result[MCPStatus], 8)
	for range 8 {
		wait.Add(1)
		go func() {
			defer wait.Done()
			results <- app.SetMCPEnabled(context.Background(), true, MCPTransportLocal)
		}()
	}
	wait.Wait()
	close(results)
	for result := range results {
		if result.Outcome != protocolcommon.OutcomeSuccess {
			t.Fatalf("concurrent enable=%+v", result)
		}
	}
	mu.Lock()
	if len(events) != 1 || events[0] != "start:mcp_host" {
		t.Fatalf("concurrent lifecycle=%v", events)
	}
	mu.Unlock()

	app.mcpStore.root = filepath.Join(root, "missing")
	if result := app.SetMCPEnabled(context.Background(), false, MCPTransportLocal); result.Outcome != protocolcommon.OutcomeFailed {
		t.Fatalf("disable persistence failure=%+v", result)
	}
	if status := app.MCPStatus(); !status.Enabled || status.Generation != 1 {
		t.Fatalf("failed disable changed state=%+v", status)
	}
	mu.Lock()
	if len(events) != 3 || events[1] != "stop:mcp_host" || events[2] != "start:mcp_host" {
		t.Fatalf("disable rollback lifecycle=%v", events)
	}
	mu.Unlock()

	app.mcpStore.root = root
	if result := app.SetMCPEnabled(context.Background(), false, MCPTransportLocal); result.Outcome != protocolcommon.OutcomeSuccess {
		t.Fatalf("disable=%+v", result)
	}
	app.mcpStore.root = filepath.Join(root, "missing")
	if result := app.SetMCPEnabled(context.Background(), true, MCPTransportLocal); result.Outcome != protocolcommon.OutcomeFailed {
		t.Fatalf("enable persistence failure=%+v", result)
	}
	if status := app.MCPStatus(); status.Enabled || status.Generation != 2 {
		t.Fatalf("failed enable changed state=%+v", status)
	}
}
