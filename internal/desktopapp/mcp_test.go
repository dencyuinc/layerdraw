// SPDX-License-Identifier: LicenseRef-LayerDraw-1.0

package desktopapp

import (
	"context"
	"encoding/json"
	"fmt"
	"path/filepath"
	"strconv"
	"testing"

	"github.com/dencyuinc/layerdraw/gen/go/accessprotocol"
	"github.com/dencyuinc/layerdraw/gen/go/engineprotocol"
	"github.com/dencyuinc/layerdraw/gen/go/protocolcommon"
	"github.com/dencyuinc/layerdraw/gen/go/runtimeprotocol"
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
	next := engineprotocol.ModuleCursor{DocumentGeneration: generation, Value: "list_modules_cursor_abcdefghijklmnop"}
	responseBytes := encodeListModulesPage(t, request, nil, &next, engineprotocol.TruncationOutcomeItemLimit)
	secondBytes := encodeListModulesPage(t, request, nil, nil, engineprotocol.TruncationOutcomeComplete)
	overflowItems := make([]engineprotocol.ModuleReadItem, mcphost.DefaultLimits().MaxItems+1)
	for index := range overflowItems {
		overflowItems[index] = engineprotocol.ModuleReadItem{ByteLength: "1", Digest: testMCPDigest(), Module: semantic.ModuleRef{ModulePath: fmt.Sprintf("module/%04d.ldl", index), Origin: semantic.SourceOrigin{Kind: semantic.OriginKindProject}}}
	}
	overflowBytes := encodeListModulesPage(t, request, overflowItems, nil, engineprotocol.TruncationOutcomeComplete)
	overflow := false
	clients.Engine.ListModules = func(_ context.Context, exchange desktopcontract.Exchange) (desktopcontract.ExchangeResult, error) {
		decoded, decodeErr := engineprotocol.DecodeListModulesRequestEnvelope(exchange.Control)
		if decodeErr != nil {
			t.Fatalf("generated request invalid: %v", decodeErr)
		}
		control := responseBytes
		if overflow {
			control = overflowBytes
		} else if decoded.Payload.Cursor != nil {
			if *decoded.Payload.Cursor != next {
				t.Fatalf("injected cursor=%+v want=%+v", decoded.Payload.Cursor, next)
			}
			control = secondBytes
		}
		return desktopcontract.ExchangeResult{Operation: string(engineprotocol.ListModulesRequestEnvelopeOperationValue), Control: append([]byte(nil), control...), Blobs: []desktopcontract.Blob{}}, nil
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
	if result.Failure != nil || result.Cursor == "" || string(result.Content) != string(direct.Control) {
		t.Fatalf("MCP=%+v direct=%s", result, direct.Control)
	}
	rawCursorRequest := request
	rawCursorRequest.Payload.Cursor = &next
	rawCursorBytes, err := engineprotocol.EncodeListModulesRequestEnvelope(rawCursorRequest)
	if err != nil {
		t.Fatal(err)
	}
	rawCursor := app.MCPCallTool(context.Background(), mcphost.CallToolRequest{Name: "layerdraw.list_modules", RequestID: "raw-cursor", Arguments: rawCursorBytes, Binding: &mcphost.Binding{DocumentID: "document-1", RevisionDigest: digest, AccessFingerprint: digest}})
	if rawCursor.Failure == nil || rawCursor.Failure.Code != mcphost.ErrorInvalidCursor || len(rawCursor.Content) != 0 {
		t.Fatalf("raw inner cursor bypassed host: %+v", rawCursor)
	}
	second := app.MCPCallTool(context.Background(), mcphost.CallToolRequest{Name: "layerdraw.list_modules", RequestID: "request", Arguments: requestBytes, Cursor: result.Cursor, Binding: &mcphost.Binding{DocumentID: "document-1", RevisionDigest: digest, AccessFingerprint: digest}})
	if second.Failure != nil || second.Cursor != "" || string(second.Content) != string(secondBytes) {
		t.Fatalf("second=%+v", second)
	}
	overflow = true
	exhausted := app.MCPCallTool(context.Background(), mcphost.CallToolRequest{Name: "layerdraw.list_modules", RequestID: "overflow", Arguments: requestBytes, Binding: &mcphost.Binding{DocumentID: "document-1", RevisionDigest: digest, AccessFingerprint: digest}})
	if exhausted.Failure == nil || exhausted.Failure.Code != mcphost.ErrorResourceExhausted || len(exhausted.Content) != 0 {
		t.Fatalf("overflow=%+v", exhausted)
	}
	tools, failure := app.MCPListTools(context.Background())
	if failure != nil || len(tools) != 2 || tools[1].Name != "layerdraw.list_modules" {
		t.Fatalf("tools=%+v failure=%+v", tools, failure)
	}
}

func testMCPDigest() protocolcommon.Digest {
	return protocolcommon.Digest("sha256:0000000000000000000000000000000000000000000000000000000000000000")
}

func encodeListModulesPage(t *testing.T, request engineprotocol.ListModulesRequestEnvelope, items []engineprotocol.ModuleReadItem, cursor *engineprotocol.ModuleCursor, truncation engineprotocol.TruncationOutcome) []byte {
	t.Helper()
	if items == nil {
		items = []engineprotocol.ModuleReadItem{}
	}
	payload := engineprotocol.ListModulesResult{DocumentGeneration: request.Payload.DocumentGeneration, Items: items, Page: engineprotocol.ModulePageInfo{NextCursor: cursor, ReturnedBytes: "0", ReturnedItems: protocolcommon.CanonicalUint64(strconv.Itoa(len(items))), Truncation: truncation}}
	logical, err := json.Marshal(payload)
	if err != nil {
		t.Fatal(err)
	}
	payload.Page.ReturnedBytes = engineprotocol.LogicalResponseByteCount(strconv.Itoa(len(logical)))
	encoded, err := engineprotocol.EncodeListModulesResponseEnvelope(engineprotocol.ListModulesResponseEnvelope{Diagnostics: []semantic.Diagnostic{}, EngineRelease: "1.0.0", Outcome: protocolcommon.OutcomeSuccess, Payload: &payload, Protocol: request.Protocol, RequestID: request.RequestID})
	if err != nil {
		t.Fatal(err)
	}
	return encoded
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

func TestGeneratedPageAdapterCatalogIsClosedAndHandlesTerminalPages(t *testing.T) {
	engineDiagnostic := semantic.Diagnostic{Arguments: map[string]semantic.DiagnosticArgumentValue{}, Code: "LDL1801", MessageKey: "workbench_not_found_rejected", ProtocolVersion: 1, Related: []semantic.DiagnosticRelated{}, Severity: semantic.DiagnosticSeverityError}
	engineTerminal, err := engineprotocol.EncodeListModulesResponseEnvelope(engineprotocol.ListModulesResponseEnvelope{Diagnostics: []semantic.Diagnostic{engineDiagnostic}, EngineRelease: "1.0.0", Outcome: protocolcommon.OutcomeRejected, Protocol: engineprotocol.EngineProtocolRef{Name: engineprotocol.EngineProtocolRefNameValue, Version: engineprotocol.EngineProtocolRefVersionValue}, RequestID: "terminal"})
	if err != nil {
		t.Fatal(err)
	}
	runtimeDiagnostic := protocolcommon.ProtocolDiagnostic{Code: "runtime.rejected", Message: "rejected", Related: []protocolcommon.ProtocolDiagnosticRelated{}, Severity: protocolcommon.ProtocolDiagnosticSeverityError}
	runtimeTerminal, err := runtimeprotocol.EncodeListRevisionsResponseEnvelope(runtimeprotocol.ListRevisionsResponseEnvelope{Diagnostics: []protocolcommon.ProtocolDiagnostic{runtimeDiagnostic}, HostRelease: "1.0.0", Outcome: protocolcommon.OutcomeRejected, Protocol: runtimeprotocol.RuntimeProtocolRef{Name: runtimeprotocol.RuntimeProtocolRefNameValue, Version: "1.0"}, RequestID: "terminal"})
	if err != nil {
		t.Fatal(err)
	}
	want := map[string]bool{"engine.list_modules": true, "engine.find_symbols": true, "engine.read_declarations": true, "engine.read_rows": true, "engine.get_neighbors": true, "engine.inspect_subgraph": true, "engine.find_usages": true, "engine.list_references": true, "engine.read_references": true, "runtime.list_revisions": true}
	if len(mcpPageAdapters) != len(want) {
		t.Fatalf("adapters=%d", len(mcpPageAdapters))
	}
	for operation, adapter := range mcpPageAdapters {
		if !want[operation] {
			t.Fatalf("unexpected adapter %s", operation)
		}
		control := engineTerminal
		if operation == "runtime.list_revisions" {
			control = runtimeTerminal
		}
		items, cursor, inspectErr := adapter.inspect(control)
		if inspectErr != nil || items != 0 || len(cursor) != 0 {
			t.Fatalf("%s terminal page: items=%d cursor=%s err=%v", operation, items, cursor, inspectErr)
		}
	}
	if _, err = adaptMCPPageRequest("engine.preview_fragment", json.RawMessage(`{}`), json.RawMessage(`"cursor"`)); err == nil {
		t.Fatal("unsupported continuation accepted")
	}
	if items, cursor, err := inspectMCPPage("engine.preview_fragment", json.RawMessage(`{}`)); err != nil || items != 0 || cursor != nil {
		t.Fatalf("non-page result=%d,%s,%v", items, cursor, err)
	}
}
