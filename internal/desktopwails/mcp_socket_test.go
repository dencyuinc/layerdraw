// SPDX-License-Identifier: LicenseRef-LayerDraw-1.0

package desktopwails

import (
	"bufio"
	"context"
	"encoding/json"
	"net"
	"strings"
	"testing"
	"time"

	"github.com/dencyuinc/layerdraw/internal/mcphost"
)

type fakeSocketApplication struct {
	lastConnection string
	lastCall       mcphost.CallToolRequest
}

func (f *fakeSocketApplication) MCPListConnectionTools(_ context.Context, connectionID string) ([]mcphost.Tool, *mcphost.Failure) {
	f.lastConnection = connectionID
	if connectionID != "mcp-connection-valid" {
		return nil, &mcphost.Failure{Code: mcphost.ErrorCapabilityUnavailable}
	}
	return []mcphost.Tool{{Name: "layerdraw.get_capabilities", Description: "Capability snapshot", InputSchema: json.RawMessage(`{"type":"object"}`)}}, nil
}

func (f *fakeSocketApplication) MCPCallConnectionTool(_ context.Context, connectionID string, request mcphost.CallToolRequest) mcphost.CallToolResult {
	f.lastConnection = connectionID
	f.lastCall = request
	if request.Name == "layerdraw.get_capabilities" {
		return mcphost.CallToolResult{Content: json.RawMessage(`{"operations":{}}`)}
	}
	return mcphost.CallToolResult{Failure: &mcphost.Failure{Code: mcphost.ErrorCapabilityUnavailable}}
}

func dialSocket(t *testing.T, root string) (net.Conn, *bufio.Reader) {
	t.Helper()
	conn, err := net.DialTimeout("unix", mcpSocketPath(root), 2*time.Second)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = conn.Close() })
	return conn, bufio.NewReader(conn)
}

func writeLine(t *testing.T, conn net.Conn, value string) {
	t.Helper()
	if _, err := conn.Write([]byte(value + "\n")); err != nil {
		t.Fatal(err)
	}
}

func readResponse(t *testing.T, reader *bufio.Reader) map[string]any {
	t.Helper()
	line, err := reader.ReadString('\n')
	if err != nil {
		t.Fatal(err)
	}
	var decoded map[string]any
	if err := json.Unmarshal([]byte(line), &decoded); err != nil {
		t.Fatal(err)
	}
	return decoded
}

// TestMCPSocketServesInitializeToolsAndCalls covers the external transport:
// preamble-authorized clients complete the MCP handshake, list only the tools
// their connection allows, and receive text-content tool results.
func TestMCPSocketServesInitializeToolsAndCalls(t *testing.T) {
	root := t.TempDir()
	app := &fakeSocketApplication{}
	server, err := startMCPSocket(app, root)
	if err != nil {
		t.Fatal(err)
	}
	defer server.close()

	conn, reader := dialSocket(t, root)
	writeLine(t, conn, `{"layerdraw_connection":"mcp-connection-valid"}`)
	writeLine(t, conn, `{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":"2024-11-05"}}`)
	response := readResponse(t, reader)
	result, ok := response["result"].(map[string]any)
	if !ok || result["protocolVersion"] != mcpProtocolVersion {
		t.Fatalf("unexpected initialize response: %v", response)
	}

	writeLine(t, conn, `{"jsonrpc":"2.0","method":"notifications/initialized"}`)
	writeLine(t, conn, `{"jsonrpc":"2.0","id":2,"method":"tools/list"}`)
	response = readResponse(t, reader)
	if app.lastConnection != "mcp-connection-valid" {
		t.Fatalf("connection id not propagated: %q", app.lastConnection)
	}
	tools := response["result"].(map[string]any)["tools"].([]any)
	if len(tools) != 1 || tools[0].(map[string]any)["name"] != "layerdraw.get_capabilities" {
		t.Fatalf("unexpected tools: %v", tools)
	}
	if _, hasSchema := tools[0].(map[string]any)["inputSchema"]; !hasSchema {
		t.Fatal("inputSchema missing from wire tool")
	}

	writeLine(t, conn, `{"jsonrpc":"2.0","id":3,"method":"tools/call","params":{"name":"layerdraw.get_capabilities","arguments":{}}}`)
	response = readResponse(t, reader)
	call := response["result"].(map[string]any)
	if call["isError"] != false {
		t.Fatalf("unexpected call result: %v", call)
	}
	text := call["content"].([]any)[0].(map[string]any)["text"].(string)
	if !strings.Contains(text, "operations") {
		t.Fatalf("tool content not passed through: %q", text)
	}

	writeLine(t, conn, `{"jsonrpc":"2.0","id":4,"method":"nonsense/method"}`)
	response = readResponse(t, reader)
	if response["error"] == nil {
		t.Fatalf("unknown method must fail: %v", response)
	}
}

// TestMCPSocketRejectsMissingPreamble ensures unauthenticated clients cannot
// reach dispatch: the first line must name a UI-created connection.
func TestMCPSocketRejectsMissingPreamble(t *testing.T) {
	root := t.TempDir()
	server, err := startMCPSocket(&fakeSocketApplication{}, root)
	if err != nil {
		t.Fatal(err)
	}
	defer server.close()
	conn, reader := dialSocket(t, root)
	writeLine(t, conn, `{"jsonrpc":"2.0","id":1,"method":"initialize"}`)
	response := readResponse(t, reader)
	if response["error"] == nil {
		t.Fatalf("preamble-less client must be rejected: %v", response)
	}
}

// TestMCPClientConfigJSONNamesBridgeCommand pins the copyable client config to
// the stdio bridge contract.
func TestMCPClientConfigJSONNamesBridgeCommand(t *testing.T) {
	config := MCPClientConfigJSON("mcp-connection-abc")
	if !strings.Contains(config, `"--mcp-stdio"`) || !strings.Contains(config, "mcp-connection-abc") || !strings.Contains(config, `"mcpServers"`) {
		t.Fatalf("unexpected client config: %s", config)
	}
}
