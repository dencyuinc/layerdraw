// SPDX-License-Identifier: LicenseRef-LayerDraw-1.0

package desktopwails

import (
	"context"
	"encoding/json"
	"path/filepath"
	"testing"

	"github.com/dencyuinc/layerdraw/gen/go/engineprotocol"
	"github.com/dencyuinc/layerdraw/gen/go/protocolcommon"
	"github.com/dencyuinc/layerdraw/gen/go/runtimeprotocol"
	"github.com/dencyuinc/layerdraw/internal/desktopapp"
	"github.com/dencyuinc/layerdraw/internal/desktopcontract"
	nativeexport "github.com/dencyuinc/layerdraw/internal/exporter"
	"github.com/dencyuinc/layerdraw/internal/registry"
)

type registryDispatchStub struct{ response []byte }

func (s registryDispatchStub) DispatchRegistry(context.Context, []byte) []byte { return s.response }

func TestFrontendRegistryDispatchIsRawStrictAndCorrelated(t *testing.T) {
	validResponse, err := json.Marshal(registry.WireResponse{
		WireVersion: registry.RegistryWireVersion, Operation: registry.WireListSources,
		RequestID: "request-a", OK: true, Value: json.RawMessage(`[]`),
	})
	if err != nil {
		t.Fatal(err)
	}
	bridge := NewFrontendBridge(nil, registryDispatchStub{response: validResponse})
	valid := `{"wire_version":"1.0","operation":"registry.list_sources","request_id":"request-a","input":{}}`
	var response registry.WireResponse
	if err := json.Unmarshal([]byte(bridge.RegistryDispatch(valid)), &response); err != nil || !response.OK || response.RequestID != "request-a" {
		t.Fatalf("valid response=%+v err=%v", response, err)
	}
	unknown := `{"wire_version":"1.0","operation":"registry.list_sources","request_id":"request-a","input":{},"smuggled":true}`
	if err := json.Unmarshal([]byte(bridge.RegistryDispatch(unknown)), &response); err != nil || response.Failure == nil || response.Failure.Code != registry.FailureUnsupportedFormat {
		t.Fatalf("unknown field response=%+v err=%v", response, err)
	}
	mismatched, err := json.Marshal(registry.WireResponse{
		WireVersion: registry.RegistryWireVersion, Operation: registry.WireListSources,
		RequestID: "request-b", OK: true, Value: json.RawMessage(`[]`),
	})
	if err != nil {
		t.Fatal(err)
	}
	bridge = NewFrontendBridge(nil, registryDispatchStub{response: mismatched})
	if err := json.Unmarshal([]byte(bridge.RegistryDispatch(valid)), &response); err != nil || response.RequestID != "request-a" || response.Failure == nil || response.Failure.Code != registry.FailureRepairRequired {
		t.Fatalf("correlation response=%+v err=%v", response, err)
	}
}

func TestFrontendBridgeExposesContextFreeGeneratedInvoke(t *testing.T) {
	base, err := NewSharedConfig(filepath.Join(t.TempDir(), "data"))
	if err != nil {
		t.Fatal(err)
	}
	application, err := Compose(base, &nativeStub{}, nil)
	if err != nil {
		t.Fatal(err)
	}
	bridge := NewFrontendBridge(application)
	bridge.setContext(context.Background())
	if started := application.Start(context.Background()); started.Outcome != protocolcommon.OutcomeSuccess {
		t.Fatalf("start: %+v", started)
	}
	defer application.Shutdown(context.Background())
	control, err := engineprotocol.EncodeHandshakeRequestEnvelope(engineprotocol.HandshakeRequestEnvelope{
		Operation: engineprotocol.HandshakeRequestEnvelopeOperationValue,
		Protocol:  engineprotocol.EngineProtocolRef{Name: engineprotocol.EngineProtocolRefNameValue, Version: engineprotocol.EngineProtocolRefVersionValue},
		RequestID: "frontend-bridge",
		Payload: protocolcommon.HandshakeRequest{ClientRelease: "0.0.0-dev",
			Protocols:            []protocolcommon.ProtocolOffer{{Name: "engine", SupportedRange: "1.0..1.0", Versions: []protocolcommon.ProtocolVersionBinding{{Version: "1.0", SchemaDigest: protocolcommon.Digest(engineprotocol.SchemaDigest)}}}},
			RequiredCapabilities: []protocolcommon.CapabilityID{}, OptionalCapabilities: []protocolcommon.CapabilityID{}},
	})
	if err != nil {
		t.Fatal(err)
	}
	result := bridge.Invoke("EngineHandshake", desktopcontract.Exchange{Operation: "engine.handshake", Control: control, Blobs: []desktopcontract.Blob{}})
	if result.Outcome != protocolcommon.OutcomeSuccess || !result.Validate() || bridge.State() != desktopcontract.LifecycleReady {
		t.Fatalf("invoke=%+v state=%s", result, bridge.State())
	}
}

func TestFrontendBridgeDelegatesProjectSurfaceWithFallbackContext(t *testing.T) {
	base, err := NewSharedConfig(filepath.Join(t.TempDir(), "data"))
	if err != nil {
		t.Fatal(err)
	}
	application, err := Compose(base, &nativeStub{}, nil)
	if err != nil {
		t.Fatal(err)
	}
	bridge := NewFrontendBridge(application)
	bridge.setContext(nil)
	if result := bridge.CreateProjectDialog(""); result.Outcome != protocolcommon.OutcomeFailed || result.Failure == nil {
		t.Fatalf("create project validation was not delegated: %+v", result)
	}
	if result := bridge.OpenProjectDialog(""); result.Outcome != protocolcommon.OutcomeFailed || result.Failure == nil {
		t.Fatalf("open project validation was not delegated: %+v", result)
	}
	if result := bridge.RecentProjects(); result.Outcome != protocolcommon.OutcomeSuccess || len(result.Value) != 0 {
		t.Fatalf("recent projects were not delegated: %+v", result)
	}
	if result := bridge.ConnectExternal(desktopapp.ExternalConnectionRequest{}); result.Failure == nil {
		t.Fatalf("connect external=%+v", result)
	}
	if result := bridge.InspectExternal(""); result.Failure == nil {
		t.Fatalf("inspect external=%+v", result)
	}
	if result := bridge.RefreshExternal(""); result.Failure == nil {
		t.Fatalf("refresh external=%+v", result)
	}
	if result := bridge.DisconnectExternal(""); result.Failure == nil {
		t.Fatalf("disconnect external=%+v", result)
	}
	if result := bridge.SelectExternalRemote(desktopapp.ExternalRemoteSelectionRequest{}); result.Failure == nil {
		t.Fatalf("select external=%+v", result)
	}
	if result := bridge.AcquireExternalLease(runtimeprotocol.RuntimeSessionRef{}, desktopapp.ExternalBackendBinding{}); result.Failure == nil {
		t.Fatalf("lease external=%+v", result)
	}
	if result := bridge.PlanExternalReconcile(runtimeprotocol.RuntimeSessionRef{}, desktopapp.ExternalSyncRequest{}, false); result.Failure == nil {
		t.Fatalf("plan external=%+v", result)
	}
	if result := bridge.ApplyExternalReconcile(runtimeprotocol.RuntimeSessionRef{}, desktopapp.ExternalReconcilePlan{}, ""); result.Failure == nil {
		t.Fatalf("apply external=%+v", result)
	}
	if result := bridge.NativeExportProfiles(); result.Outcome != protocolcommon.OutcomeSuccess || len(result.Value) == 0 {
		t.Fatalf("native export profiles=%+v", result)
	}
	if result := bridge.SerializeNativeExport(nativeexport.SerializeInput{}); result.Failure == nil {
		t.Fatalf("invalid native serialize=%+v", result)
	}
	if result := bridge.PublishNativeExportDialog(desktopapp.NativePublishRequest{}); result.Failure == nil {
		t.Fatalf("invalid native publish=%+v", result)
	}
	if result := bridge.ImportExternalDialog(desktopapp.ExternalImportRequest{}); result.Failure == nil {
		t.Fatalf("invalid external import=%+v", result)
	}
	if status := bridge.MCPStatus(); status.Enabled {
		t.Fatalf("MCP unexpectedly enabled: %+v", status)
	}
	if result := bridge.SetMCPEnabled(true, desktopapp.MCPTransportKind("invalid")); result.Failure == nil {
		t.Fatalf("invalid MCP transport=%+v", result)
	}
	if result := bridge.CreateMCPConnection(desktopapp.MCPConnectRequest{}); result.Failure == nil {
		t.Fatalf("invalid MCP connection=%+v", result)
	}
	if connections := bridge.ListMCPConnections(); len(connections) != 0 {
		t.Fatalf("unexpected MCP connections=%+v", connections)
	}
	if result := bridge.RevokeMCPConnection("missing"); result.Failure == nil {
		t.Fatalf("missing MCP revoke=%+v", result)
	}
	if result := bridge.RestartMCP(); result.Failure == nil {
		t.Fatalf("MCP restart before startup=%+v", result)
	}
}
