// SPDX-License-Identifier: LicenseRef-LayerDraw-1.0

package desktopapp

import (
	"context"
	"encoding/json"
	"path/filepath"
	"testing"

	"github.com/dencyuinc/layerdraw/gen/go/accessprotocol"
	"github.com/dencyuinc/layerdraw/gen/go/engineprotocol"
	"github.com/dencyuinc/layerdraw/gen/go/protocolcommon"
	"github.com/dencyuinc/layerdraw/gen/go/semantic"
	"github.com/dencyuinc/layerdraw/internal/desktopcontract"
	"github.com/dencyuinc/layerdraw/internal/mcphost"
)

type canonicalTransportStub struct{ started, stopped bool }

func (t *canonicalTransportStub) Start(_ context.Context, _ mcphost.Handler) error {
	t.started = true
	return nil
}
func (t *canonicalTransportStub) Shutdown(context.Context) error { t.stopped = true; return nil }

type canonicalOwnerStub struct{}
type mcpCapabilitySourceStub struct{ snapshot mcphost.CapabilitySnapshot }

func (s mcpCapabilitySourceStub) Snapshot(context.Context) (mcphost.CapabilitySnapshot, error) {
	return s.snapshot, nil
}

func (canonicalOwnerStub) Capabilities(context.Context) (mcphost.CapabilitySnapshot, error) {
	digest := protocolcommon.Digest("sha256:0000000000000000000000000000000000000000000000000000000000000000")
	return mcphost.CapabilitySnapshot{ManifestETag: protocolcommon.ManifestETag(digest), Operations: map[string]mcphost.OperationCapability{}, Resources: []mcphost.ResourceCapability{}, GrantSummary: accessprotocol.AuthoringGrantSummary{AccessFingerprint: digest, ConstrainedCapabilities: []semantic.AuthoringCapability{}, GrantedCapabilities: []semantic.AuthoringCapability{}, PolicyEtag: digest}}, nil
}
func (canonicalOwnerStub) Invoke(context.Context, mcphost.OwnerRequest) (mcphost.OwnerResponse, error) {
	return mcphost.OwnerResponse{Content: json.RawMessage(`{}`)}, nil
}
func (canonicalOwnerStub) ReadResource(context.Context, mcphost.ResourceRequest) (mcphost.ResourceResponse, error) {
	return mcphost.ResourceResponse{Content: json.RawMessage(`{}`), MimeType: "application/json"}, nil
}

func TestCanonicalMCPPortOwnsInProcessLifecycle(t *testing.T) {
	transport := &canonicalTransportStub{}
	host, err := mcphost.New(mcphost.Config{Owner: canonicalOwnerStub{}, Transport: transport})
	if err != nil {
		t.Fatal(err)
	}
	port := BindCanonicalMCPHost(host)
	if port == nil {
		t.Fatal("nil canonical port")
	}
	if result := port.Start(context.Background()); !result.Validate() || result.Outcome != protocolcommon.OutcomeSuccess || !transport.started {
		t.Fatalf("start=%+v", result)
	}
	if result := port.Shutdown(context.Background()); !result.Validate() || result.Outcome != protocolcommon.OutcomeSuccess || !transport.stopped {
		t.Fatalf("stop=%+v", result)
	}
}

func TestCanonicalConstructorRejectsMissingOrAmbiguousMCPHost(t *testing.T) {
	if _, err := NewCanonical(Config{}); err == nil {
		t.Fatal("missing host accepted")
	}
	host, err := mcphost.New(mcphost.Config{Owner: canonicalOwnerStub{}, Transport: &canonicalTransportStub{}})
	if err != nil {
		t.Fatal(err)
	}
	config := Config{MCPHost: host}
	config.HostPorts.MCP = mcpPortStub{}
	if _, err = NewCanonical(config); err == nil {
		t.Fatal("ambiguous MCP composition accepted")
	}
}

func TestCanonicalMCPPortMapsLifecycleFailures(t *testing.T) {
	if BindCanonicalMCPHost(nil) != nil {
		t.Fatal("nil host was bound")
	}
	transport := &canonicalTransportStub{}
	owner := canonicalOwnerStub{}
	host, err := mcphost.New(mcphost.Config{Owner: owner, Transport: transport})
	if err != nil {
		t.Fatal(err)
	}
	port := BindCanonicalMCPHost(host)
	if result := port.Shutdown(context.Background()); result.Outcome != protocolcommon.OutcomeSuccess {
		t.Fatalf("idempotent=%+v", result)
	}
	transportFailure := &failingCanonicalTransport{}
	host, err = mcphost.New(mcphost.Config{Owner: owner, Transport: transportFailure})
	if err != nil {
		t.Fatal(err)
	}
	port = BindCanonicalMCPHost(host)
	if result := port.Start(context.Background()); !result.Validate() || result.Outcome != protocolcommon.OutcomeFailed {
		t.Fatalf("start=%+v", result)
	}
}

type failingCanonicalTransport struct{}

func (*failingCanonicalTransport) Start(context.Context, mcphost.Handler) error {
	return context.Canceled
}
func (*failingCanonicalTransport) Shutdown(context.Context) error { return context.Canceled }

func TestCanonicalDesktopCompositionUsesGeneratedOwnerEnvelopeWithWailsParity(t *testing.T) {
	clients := completeClients(t)
	generation := engineprotocol.DocumentGeneration{DocumentHandle: engineprotocol.DocumentHandle{EndpointInstanceID: "fixture-endpoint", Value: "document_abcdefghijklmnop"}, Value: "7"}
	request := engineprotocol.ListModulesRequestEnvelope{Operation: engineprotocol.ListModulesRequestEnvelopeOperationValue, Payload: engineprotocol.ListModulesInput{DocumentGeneration: generation, Limits: engineprotocol.WorkbenchLimits{MaxItems: "10", MaxOutputBytes: "4096"}}, Protocol: engineprotocol.EngineProtocolRef{Name: engineprotocol.EngineProtocolRefNameValue, Version: engineprotocol.EngineProtocolRefVersionValue}, RequestID: "request"}
	requestBytes, err := engineprotocol.EncodeListModulesRequestEnvelope(request)
	if err != nil {
		t.Fatal(err)
	}
	response := engineprotocol.ListModulesResponseEnvelope{Diagnostics: []semantic.Diagnostic{}, EngineRelease: "1.0.0", Outcome: protocolcommon.OutcomeSuccess, Payload: &engineprotocol.ListModulesResult{DocumentGeneration: generation, Items: []engineprotocol.ModuleReadItem{}, Page: engineprotocol.ModulePageInfo{ReturnedBytes: "221", ReturnedItems: "0", Truncation: engineprotocol.TruncationOutcomeComplete}}, Protocol: request.Protocol, RequestID: request.RequestID}
	responseBytes, err := engineprotocol.EncodeListModulesResponseEnvelope(response)
	if err != nil {
		t.Fatal(err)
	}
	clients.Engine.ListModules = func(_ context.Context, exchange desktopcontract.Exchange) (desktopcontract.ExchangeResult, error) {
		if string(exchange.Control) != string(requestBytes) {
			t.Fatalf("generated request changed\n got %s\nwant %s", exchange.Control, requestBytes)
		}
		return desktopcontract.ExchangeResult{Operation: string(engineprotocol.ListModulesRequestEnvelopeOperationValue), Control: append([]byte(nil), responseBytes...), Blobs: []desktopcontract.Blob{}}, nil
	}
	digest := protocolcommon.Digest("sha256:0000000000000000000000000000000000000000000000000000000000000000")
	snapshot := mcphost.CapabilitySnapshot{ManifestETag: protocolcommon.ManifestETag(digest), Operations: map[string]mcphost.OperationCapability{string(engineprotocol.ListModulesRequestEnvelopeOperationValue): {Enabled: true, InputSchema: json.RawMessage(`{"type":"object"}`), OutputSchema: json.RawMessage(`{"type":"object"}`)}}, Resources: []mcphost.ResourceCapability{}, GrantSummary: accessprotocol.AuthoringGrantSummary{AccessFingerprint: digest, ConstrainedCapabilities: []semantic.AuthoringCapability{}, GrantedCapabilities: []semantic.AuthoringCapability{}, PolicyEtag: digest}}
	root := t.TempDir()
	config := testConfig(t, root, writeProject(t, root))
	config.Bindings = clients
	config.HostPorts.MCP = nil
	config.MCPCapabilities = mcpCapabilitySourceStub{snapshot: snapshot}
	config.Root = filepath.Join(root, "canonical-desktop")
	app, err := NewCanonical(config)
	if err != nil {
		t.Fatal(err)
	}
	if started := app.Start(context.Background()); started.Outcome != protocolcommon.OutcomeSuccess {
		t.Fatalf("start=%+v", started)
	}
	defer app.Shutdown(context.Background())
	direct, err := clients.Invoke(context.Background(), "EngineListModules", desktopcontract.Exchange{Operation: string(engineprotocol.ListModulesRequestEnvelopeOperationValue), Control: requestBytes, Blobs: []desktopcontract.Blob{}})
	if err != nil {
		t.Fatal(err)
	}
	result := app.MCPCallTool(context.Background(), mcphost.CallToolRequest{Name: "layerdraw.list_modules", RequestID: "request", Arguments: requestBytes, Binding: &mcphost.Binding{DocumentID: "document-1", RevisionDigest: digest, AccessFingerprint: digest}})
	if result.Failure != nil || string(result.Content) != string(direct.Control) {
		t.Fatalf("MCP=%+v direct=%s", result, direct.Control)
	}
	tools, failure := app.MCPListTools(context.Background())
	if failure != nil || len(tools) != 2 || tools[1].Name != "layerdraw.list_modules" {
		t.Fatalf("tools=%+v failure=%+v", tools, failure)
	}
}

func TestCanonicalMCPRoutesUseOnlyClosedDesktopOwnerCatalog(t *testing.T) {
	generated := map[string]bool{}
	for _, binding := range desktopcontract.GeneratedBindingTable() {
		generated[binding.Operation] = true
	}
	routes := mcphost.ToolRoutes()
	if len(routes) != 30 {
		t.Fatalf("routes=%d", len(routes))
	}
	unwired := map[string]bool{"layerdraw.serialize_export": true, "layerdraw.import_document": true, "layerdraw.export_document": true}
	for _, route := range routes {
		operations := append([]string{}, route.RequiredOperations...)
		if len(operations) == 0 && route.Operation != "" {
			operations = []string{route.Operation}
		}
		if unwired[route.Name] {
			if route.Operation != "" || route.PreviewOperation != "" || len(route.RequiredOperations) != 0 {
				t.Fatalf("unmerged owner falsely wired: %+v", route)
			}
			continue
		}
		if route.Operation == "" {
			t.Fatalf("canonical route missing owner operation: %+v", route)
		}
		for _, operation := range operations {
			if !generated[operation] {
				t.Fatalf("%s routes to non-catalog owner %s", route.Name, operation)
			}
		}
	}
}
