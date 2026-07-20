// SPDX-License-Identifier: LicenseRef-LayerDraw-1.0

package desktopapp

import (
	"bytes"
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"sort"
	"strconv"
	"time"

	"github.com/dencyuinc/layerdraw/gen/go/accessprotocol"
	"github.com/dencyuinc/layerdraw/gen/go/protocolcommon"
	"github.com/dencyuinc/layerdraw/gen/go/runtimeprotocol"
	"github.com/dencyuinc/layerdraw/gen/go/semantic"
	accesscore "github.com/dencyuinc/layerdraw/internal/access"
	"github.com/dencyuinc/layerdraw/internal/desktopcontract"
	"github.com/dencyuinc/layerdraw/internal/mcphost"
)

type MCPTransportKind string

const MCPTransportLocal MCPTransportKind = "local"
const MCPConnectionProtocolVersion = "desktop-mcp-v1"

type MCPConnectionStatus string

const (
	MCPConnectionConnected MCPConnectionStatus = "connected"
	MCPConnectionRevoking  MCPConnectionStatus = "revoking"
	MCPConnectionRevoked   MCPConnectionStatus = "revoked"
	MCPConnectionExpired   MCPConnectionStatus = "expired"
	MCPConnectionRestarted MCPConnectionStatus = "host_restarted"
)

// MCPStatus contains only non-secret connection instructions. Native paths,
// environment values and transport credentials never cross the Wails binding.
type MCPStatus struct {
	Enabled      bool             `json:"enabled"`
	Transport    MCPTransportKind `json:"transport"`
	Instructions string           `json:"instructions"`
	Generation   uint64           `json:"generation"`
}

type MCPConnection struct {
	ConnectionID    string                               `json:"connection_id"`
	ClientID        string                               `json:"client_id"`
	SessionID       string                               `json:"session_id"`
	ProtocolVersion string                               `json:"protocol_version"`
	DocumentID      runtimeprotocol.DocumentID           `json:"document_id"`
	DelegationID    string                               `json:"delegation_id"`
	AgentID         string                               `json:"agent_id"`
	Capabilities    []semantic.AuthoringCapability       `json:"capabilities"`
	GrantSummary    accessprotocol.AuthoringGrantSummary `json:"grant_summary"`
	Permissions     accesscore.AgentPermissions          `json:"permissions"`
	ExpiresAt       protocolcommon.Rfc3339Time           `json:"expires_at"`
	Generation      protocolcommon.CanonicalUint64       `json:"generation"`
	Status          MCPConnectionStatus                  `json:"status"`
}

type MCPConnectRequest struct {
	ClientID        string                         `json:"client_id"`
	ProtocolVersion string                         `json:"protocol_version"`
	DocumentID      runtimeprotocol.DocumentID     `json:"document_id"`
	AgentID         string                         `json:"agent_id"`
	Capabilities    []semantic.AuthoringCapability `json:"capabilities"`
	Permissions     accesscore.AgentPermissions    `json:"permissions"`
	ExpiresAt       protocolcommon.Rfc3339Time     `json:"expires_at"`
	ConfirmApply    bool                           `json:"confirm_apply"`
}

func (a *Application) MCPStatus() MCPStatus {
	a.mcpMu.Lock()
	defer a.mcpMu.Unlock()
	return MCPStatus{Enabled: a.mcpEnabled, Transport: MCPTransportLocal, Instructions: "Connect through the LayerDraw Desktop local MCP client entrypoint.", Generation: a.mcpGeneration}
}

func (a *Application) SetMCPEnabled(ctx context.Context, enabled bool, transport MCPTransportKind) desktopcontract.Result[MCPStatus] {
	if transport != MCPTransportLocal {
		return failed[MCPStatus](desktopcontract.FailureMCPTransport, desktopcontract.ComponentMCPHost, false, desktopcontract.RecoveryConfigureAdapter)
	}
	a.mcpMu.Lock()
	if a.mcpEnabled == enabled {
		status := MCPStatus{Enabled: enabled, Transport: transport, Instructions: "Connect through the LayerDraw Desktop local MCP client entrypoint.", Generation: a.mcpGeneration}
		a.mcpMu.Unlock()
		return desktopcontract.Result[MCPStatus]{Outcome: protocolcommon.OutcomeSuccess, Value: status}
	}
	a.mcpMu.Unlock()
	a.mu.Lock()
	ready := a.state == desktopcontract.LifecycleReady
	a.mu.Unlock()
	if !ready {
		return failed[MCPStatus](desktopcontract.FailureReconnect, desktopcontract.ComponentMCPHost, true, desktopcontract.RecoveryReconnect)
	}
	var lifecycle desktopcontract.Result[struct{}]
	if enabled {
		lifecycle = safeMCPStart(ctx, a.config.HostPorts.MCP)
	} else {
		lifecycle = safeMCPShutdown(ctx, a.config.HostPorts.MCP)
	}
	if !lifecycle.Validate() || lifecycle.Outcome != protocolcommon.OutcomeSuccess {
		return failed[MCPStatus](desktopcontract.FailureMCPTransport, desktopcontract.ComponentMCPHost, true, desktopcontract.RecoveryReconnect)
	}
	a.mcpMu.Lock()
	a.mcpEnabled = enabled
	a.mcpGeneration++
	if !enabled {
		a.fenceMCPConnectionsLocked(MCPConnectionRestarted)
	}
	status := MCPStatus{Enabled: enabled, Transport: transport, Instructions: "Connect through the LayerDraw Desktop local MCP client entrypoint.", Generation: a.mcpGeneration}
	if err := a.mcpStore.save(a.mcpGeneration, a.mcpConnections); err != nil {
		a.mcpMu.Unlock()
		return failed[MCPStatus](desktopcontract.FailureMCPTransport, desktopcontract.ComponentMCPHost, true, desktopcontract.RecoveryRetry)
	}
	a.mcpMu.Unlock()
	a.mu.Lock()
	if enabled {
		found := false
		for _, id := range a.started {
			found = found || id == desktopcontract.ComponentMCPHost
		}
		if !found {
			a.started = append(a.started, desktopcontract.ComponentMCPHost)
		}
	} else {
		for index, id := range a.started {
			if id == desktopcontract.ComponentMCPHost {
				a.started = append(a.started[:index], a.started[index+1:]...)
				break
			}
		}
	}
	a.mu.Unlock()
	return desktopcontract.Result[MCPStatus]{Outcome: protocolcommon.OutcomeSuccess, Value: status}
}

func (a *Application) CreateMCPConnection(ctx context.Context, request MCPConnectRequest) desktopcontract.Result[MCPConnection] {
	expires, err := time.Parse(time.RFC3339Nano, string(request.ExpiresAt))
	if request.ProtocolVersion != MCPConnectionProtocolVersion {
		return failed[MCPConnection](desktopcontract.FailureMCPVersionMismatch, desktopcontract.ComponentMCPHost, false, desktopcontract.RecoveryUpgrade)
	}
	if err != nil || request.ClientID == "" || request.AgentID == "" || !expires.After(a.config.Now()) || (request.Permissions.Apply && !request.ConfirmApply) {
		return failed[MCPConnection](desktopcontract.FailureMCPScopeDenied, desktopcontract.ComponentAccess, false, desktopcontract.RecoveryReview)
	}
	a.mcpMu.Lock()
	if !a.mcpEnabled {
		a.mcpMu.Unlock()
		return failed[MCPConnection](desktopcontract.FailureMCPDisabled, desktopcontract.ComponentMCPHost, false, desktopcontract.RecoveryConfigureAdapter)
	}
	a.mcpMu.Unlock()
	connectionID, sessionID, delegationID := newMCPID("connection"), newMCPID("session"), newMCPID("delegation")
	if connectionID == "" || sessionID == "" || delegationID == "" {
		return failed[MCPConnection](desktopcontract.FailureMCPTransport, desktopcontract.ComponentMCPHost, true, desktopcontract.RecoveryRetry)
	}
	ref, ok := a.projects.existing(request.DocumentID)
	if !ok {
		return failed[MCPConnection](desktopcontract.FailureProjectMissing, desktopcontract.ComponentRuntime, false, desktopcontract.RecoveryLocate)
	}
	a.mu.Lock()
	host, actor := a.host, a.localActor
	a.mu.Unlock()
	if host == nil {
		return failed[MCPConnection](desktopcontract.FailureReconnect, desktopcontract.ComponentRuntime, true, desktopcontract.RecoveryReconnect)
	}
	ownerSession, err := host.SessionFor(ref)
	if err != nil {
		return failed[MCPConnection](desktopcontract.FailureReconnect, desktopcontract.ComponentRuntime, true, desktopcontract.RecoveryReconnect)
	}
	record, err := host.DelegateAgent(ctx, ownerSession, accesscore.Delegation{
		ID: delegationID, ParentActor: actor,
		Agent: accessprotocolActor(request.AgentID), DocumentID: string(request.DocumentID), LocalScopeID: string(ref.Scope.LocalScopeID),
		AuthoringCapabilities: append([]semantic.AuthoringCapability(nil), request.Capabilities...), Permissions: request.Permissions,
		IssuedAt: a.config.Now().UTC(), ExpiresAt: expires.UTC(),
	})
	if err != nil {
		return failed[MCPConnection](desktopcontract.FailureAgentDelegation, desktopcontract.ComponentAccess, false, desktopcontract.RecoveryReview)
	}
	delegated, err := host.OpenDelegatedDocument(ctx, request.DocumentID, record.ID)
	if err != nil {
		_ = host.RevokeDelegation(record.ID)
		return failed[MCPConnection](desktopcontract.FailureAgentDelegation, desktopcontract.ComponentAccess, false, desktopcontract.RecoveryReview)
	}
	grantSummary := delegated.Session.Open.AccessSummary
	if err := host.Close(context.WithoutCancel(ctx), delegated.Session); err != nil {
		_ = host.RevokeDelegation(record.ID)
		return failed[MCPConnection](desktopcontract.FailureAgentDelegation, desktopcontract.ComponentAccess, true, desktopcontract.RecoveryRetry)
	}
	connection := MCPConnection{ConnectionID: connectionID, ClientID: request.ClientID, SessionID: sessionID, ProtocolVersion: request.ProtocolVersion, DocumentID: request.DocumentID, DelegationID: record.ID, AgentID: record.Agent.ActorID, Capabilities: append([]semantic.AuthoringCapability(nil), record.AuthoringCapabilities...), GrantSummary: grantSummary, Permissions: record.Permissions, ExpiresAt: protocolcommon.Rfc3339Time(record.ExpiresAt.Format(time.RFC3339Nano)), Generation: protocolcommon.CanonicalUint64(strconv.FormatUint(record.Generation, 10)), Status: MCPConnectionConnected}
	a.mcpMu.Lock()
	a.mcpConnections[connection.ConnectionID] = connection
	if err := a.mcpStore.save(a.mcpGeneration, a.mcpConnections); err != nil {
		delete(a.mcpConnections, connection.ConnectionID)
		a.mcpMu.Unlock()
		_ = host.RevokeDelegation(record.ID)
		return failed[MCPConnection](desktopcontract.FailureAgentDelegation, desktopcontract.ComponentAccess, true, desktopcontract.RecoveryRetry)
	}
	a.mcpMu.Unlock()
	return desktopcontract.Result[MCPConnection]{Outcome: protocolcommon.OutcomeSuccess, Value: connection}
}

func accessprotocolActor(id string) accessprotocol.ActorRef {
	return accessprotocol.ActorRef{ActorID: id, Kind: "agent"}
}

func newMCPID(kind string) string {
	value := make([]byte, 18)
	if _, err := rand.Read(value); err != nil {
		return ""
	}
	return "mcp-" + kind + "-" + base64.RawURLEncoding.EncodeToString(value)
}

func (a *Application) ListMCPConnections() []MCPConnection {
	a.mcpMu.Lock()
	defer a.mcpMu.Unlock()
	now := a.config.Now()
	result := make([]MCPConnection, 0, len(a.mcpConnections))
	for id, value := range a.mcpConnections {
		if value.Status == MCPConnectionConnected {
			if expiry, err := time.Parse(time.RFC3339Nano, string(value.ExpiresAt)); err != nil || !now.Before(expiry) {
				value.Status = MCPConnectionExpired
				a.mcpConnections[id] = value
				a.cancelMCPCallsLocked(id)
			}
		}
		value.Capabilities = append([]semantic.AuthoringCapability(nil), value.Capabilities...)
		result = append(result, value)
	}
	_ = a.mcpStore.save(a.mcpGeneration, a.mcpConnections)
	sort.Slice(result, func(i, j int) bool { return result[i].ConnectionID < result[j].ConnectionID })
	return result
}

func (a *Application) RevokeMCPConnection(ctx context.Context, connectionID string) desktopcontract.Result[MCPConnection] {
	a.mcpMu.Lock()
	connection, ok := a.mcpConnections[connectionID]
	if !ok || connection.Status != MCPConnectionConnected {
		a.mcpMu.Unlock()
		return failed[MCPConnection](desktopcontract.FailureAgentDelegation, desktopcontract.ComponentAccess, false, desktopcontract.RecoveryReview)
	}
	connection.Status = MCPConnectionRevoking
	a.mcpConnections[connectionID] = connection
	a.cancelMCPCallsLocked(connectionID)
	a.mcpMu.Unlock()
	a.mu.Lock()
	host := a.host
	a.mu.Unlock()
	if host == nil || host.RevokeDelegation(connection.DelegationID) != nil {
		a.mcpMu.Lock()
		connection.Status = MCPConnectionConnected
		a.mcpConnections[connectionID] = connection
		a.mcpMu.Unlock()
		return failed[MCPConnection](desktopcontract.FailureAgentDelegation, desktopcontract.ComponentAccess, false, desktopcontract.RecoveryReview)
	}
	a.mcpMu.Lock()
	connection.Status = MCPConnectionRevoked
	a.mcpConnections[connectionID] = connection
	if err := a.mcpStore.save(a.mcpGeneration, a.mcpConnections); err != nil {
		a.mcpMu.Unlock()
		return failed[MCPConnection](desktopcontract.FailureAgentDelegation, desktopcontract.ComponentAccess, true, desktopcontract.RecoveryRetry)
	}
	a.mcpMu.Unlock()
	return desktopcontract.Result[MCPConnection]{Outcome: protocolcommon.OutcomeSuccess, Value: connection}
}

func (a *Application) RestartMCP(ctx context.Context) desktopcontract.Result[MCPStatus] {
	if result := a.SetMCPEnabled(ctx, false, MCPTransportLocal); result.Outcome != protocolcommon.OutcomeSuccess {
		return result
	}
	return a.SetMCPEnabled(ctx, true, MCPTransportLocal)
}

func (a *Application) fenceMCPConnectionsLocked(status MCPConnectionStatus) {
	for id, connection := range a.mcpConnections {
		if connection.Status == MCPConnectionConnected {
			a.cancelMCPCallsLocked(id)
			connection.Status = status
			a.mcpConnections[id] = connection
		}
	}
}

func (a *Application) MCPListConnectionTools(ctx context.Context, connectionID string) ([]mcphost.Tool, *mcphost.Failure) {
	connection, callCtx, failure := a.beginMCPCall(ctx, connectionID, "")
	if failure != nil {
		return nil, failure
	}
	defer callCtx.done()
	tools, hostFailure := a.MCPListTools(callCtx.Context)
	if hostFailure != nil {
		return nil, hostFailure
	}
	allowed := make(map[string]bool, len(tools))
	for _, route := range mcphost.ToolRoutes() {
		allowed[route.Name] = connectionAllows(connection, route.Permission)
	}
	result := make([]mcphost.Tool, 0, len(tools))
	for _, tool := range tools {
		if tool.Name == "layerdraw.get_capabilities" || allowed[tool.Name] {
			result = append(result, tool)
		}
	}
	return result, nil
}

func (a *Application) MCPCallConnectionTool(ctx context.Context, connectionID string, request mcphost.CallToolRequest) mcphost.CallToolResult {
	permission, ok := toolPermission(request.Name)
	if !ok && request.Name != "layerdraw.get_capabilities" {
		return mcphost.CallToolResult{Failure: &mcphost.Failure{Code: mcphost.ErrorCapabilityUnavailable}}
	}
	connection, callCtx, failure := a.beginMCPCall(ctx, connectionID, permission)
	if failure != nil {
		return mcphost.CallToolResult{Failure: failure}
	}
	defer callCtx.done()
	if request.Binding != nil && (request.Binding.DocumentID != connection.DocumentID || request.Binding.AccessFingerprint != connection.GrantSummary.AccessFingerprint) {
		return mcphost.CallToolResult{Failure: &mcphost.Failure{Code: mcphost.ErrorStaleBinding}}
	}
	if request.Name == "layerdraw.get_capabilities" {
		if request.Cursor != "" || !bytes.Equal(bytes.TrimSpace(request.Arguments), []byte("{}")) {
			return mcphost.CallToolResult{Failure: &mcphost.Failure{Code: mcphost.ErrorInvalidRequest}}
		}
		content, err := a.mcpConnectionCapabilities(callCtx.Context, connection)
		if err != nil {
			return mcphost.CallToolResult{Failure: &mcphost.Failure{Code: mcphost.ErrorOwnerFailure, Retryable: true}}
		}
		return mcphost.CallToolResult{Content: content}
	}
	return a.MCPCallTool(callCtx.Context, request)
}

func (a *Application) mcpConnectionCapabilities(ctx context.Context, connection MCPConnection) (json.RawMessage, error) {
	snapshot, err := a.config.MCPCapabilities.Snapshot(ctx)
	if err != nil {
		return nil, err
	}
	operations := make(map[string]mcphost.OperationCapability, len(snapshot.Operations))
	for operation, capability := range snapshot.Operations {
		operations[operation] = capability
	}
	snapshot.Operations = operations
	snapshot.Resources = append([]mcphost.ResourceCapability(nil), snapshot.Resources...)
	allowed := map[string]bool{}
	for _, route := range mcphost.ToolRoutes() {
		if !connectionAllows(connection, route.Permission) {
			continue
		}
		allowed[route.Operation], allowed[route.PreviewOperation] = true, true
		for _, operation := range route.RequiredOperations {
			allowed[operation] = true
		}
	}
	delete(allowed, "")
	for operation := range snapshot.Operations {
		if !allowed[operation] {
			delete(snapshot.Operations, operation)
		}
	}
	if !connection.Permissions.Read {
		snapshot.Resources = []mcphost.ResourceCapability{}
	}
	snapshot.GrantSummary = connection.GrantSummary
	snapshot.ManifestETag = "sha256:0000000000000000000000000000000000000000000000000000000000000000"
	content, err := json.Marshal(snapshot)
	if err != nil {
		return nil, err
	}
	digest := sha256.Sum256(content)
	snapshot.ManifestETag = protocolcommon.ManifestETag("sha256:" + hex.EncodeToString(digest[:]))
	return json.Marshal(snapshot)
}

type mcpCallContext struct {
	context.Context
	done func()
}

func (a *Application) beginMCPCall(ctx context.Context, connectionID string, permission mcphost.ToolPermission) (MCPConnection, mcpCallContext, *mcphost.Failure) {
	a.mcpMu.Lock()
	defer a.mcpMu.Unlock()
	connection, ok := a.mcpConnections[connectionID]
	if !ok || !a.mcpEnabled || connection.Status != MCPConnectionConnected {
		return MCPConnection{}, mcpCallContext{}, &mcphost.Failure{Code: mcphost.ErrorCapabilityUnavailable}
	}
	expires, err := time.Parse(time.RFC3339Nano, string(connection.ExpiresAt))
	if err != nil || !a.config.Now().Before(expires) {
		connection.Status = MCPConnectionExpired
		a.mcpConnections[connectionID] = connection
		a.cancelMCPCallsLocked(connectionID)
		return MCPConnection{}, mcpCallContext{}, &mcphost.Failure{Code: mcphost.ErrorCapabilityUnavailable}
	}
	if permission != "" && !connectionAllows(connection, permission) {
		return MCPConnection{}, mcpCallContext{}, &mcphost.Failure{Code: mcphost.ErrorCapabilityUnavailable}
	}
	callCtx, cancel := context.WithCancel(ctx)
	a.mcpCallSequence++
	id := a.mcpCallSequence
	if a.mcpCalls[connectionID] == nil {
		a.mcpCalls[connectionID] = map[uint64]context.CancelFunc{}
	}
	a.mcpCalls[connectionID][id] = cancel
	done := func() {
		cancel()
		a.mcpMu.Lock()
		delete(a.mcpCalls[connectionID], id)
		a.mcpMu.Unlock()
	}
	return connection, mcpCallContext{Context: callCtx, done: done}, nil
}

func (a *Application) cancelMCPCallsLocked(connectionID string) {
	for id, cancel := range a.mcpCalls[connectionID] {
		cancel()
		delete(a.mcpCalls[connectionID], id)
	}
}

func toolPermission(name string) (mcphost.ToolPermission, bool) {
	for _, route := range mcphost.ToolRoutes() {
		if route.Name == name {
			return route.Permission, true
		}
	}
	return "", false
}

func connectionAllows(connection MCPConnection, permission mcphost.ToolPermission) bool {
	switch permission {
	case mcphost.ToolPermissionRead:
		return connection.Permissions.Read
	case mcphost.ToolPermissionExport:
		return connection.Permissions.Export
	case mcphost.ToolPermissionPropose:
		return connection.Permissions.Propose
	case mcphost.ToolPermissionApply:
		return connection.Permissions.Apply
	default:
		return false
	}
}
