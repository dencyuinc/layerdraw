// SPDX-License-Identifier: LicenseRef-LayerDraw-1.0

package desktopwails

import (
	"bufio"
	"context"
	"encoding/json"
	"io"
	"net"
	"os"
	"path/filepath"
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
	if request.Name == "layerdraw.empty" {
		return mcphost.CallToolResult{}
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

// TestMCPSocketEdgePaths covers the long-path socket fallback, listen
// failures, malformed frames, notification skips, and tool-call failures.
func TestMCPSocketEdgePaths(t *testing.T) {
	long := filepath.Join(t.TempDir(), strings.Repeat("deep-state-root-", 8))
	if path := mcpSocketPath(long); !strings.Contains(path, os.TempDir()) || !strings.HasSuffix(path, ".sock") {
		t.Fatalf("long state root did not fall back to the temp socket: %q", path)
	}
	if err := os.MkdirAll(long, 0o700); err != nil {
		t.Fatal(err)
	}
	fallback, err := startMCPSocket(&fakeSocketApplication{}, long)
	if err != nil {
		t.Fatalf("fallback socket failed: %v", err)
	}
	fallback.close()
	if _, err := startMCPSocket(&fakeSocketApplication{}, "/nonexistent-layerdraw/x"); err == nil {
		t.Fatal("listening inside a missing directory succeeded")
	}

	root := t.TempDir()
	app := &fakeSocketApplication{}
	server, err := startMCPSocket(app, root)
	if err != nil {
		t.Fatal(err)
	}
	defer server.close()
	conn, reader := dialSocket(t, root)
	writeLine(t, conn, `{"layerdraw_connection":"mcp-connection-valid"}`)
	writeLine(t, conn, ``)
	writeLine(t, conn, `{not json`)
	response := readResponse(t, reader)
	if response["error"] == nil {
		t.Fatalf("malformed frame must return a parse error: %v", response)
	}
	writeLine(t, conn, `{"jsonrpc":"2.0","method":"tools/list"}`)
	writeLine(t, conn, `{"jsonrpc":"2.0","id":9,"method":"tools/call","params":{"name":"layerdraw.get_capabilities","arguments":{}}}`)
	if response = readResponse(t, reader); response["result"] == nil && response["error"] == nil {
		t.Fatalf("call after notification produced nothing: %v", response)
	}
	writeLine(t, conn, `{"jsonrpc":"2.0","id":10,"method":"ping"}`)
	if response = readResponse(t, reader); response["result"] == nil {
		t.Fatalf("ping must respond: %v", response)
	}
	writeLine(t, conn, `{"jsonrpc":"2.0","id":11,"method":"tools/call","params":{"name":""}}`)
	if response = readResponse(t, reader); response["error"] == nil {
		t.Fatalf("empty tool name must fail: %v", response)
	}
	writeLine(t, conn, `{"jsonrpc":"2.0","id":12,"method":"tools/call","params":{"name":"layerdraw.empty"}}`)
	response = readResponse(t, reader)
	if content := response["result"].(map[string]any)["content"].([]any)[0].(map[string]any)["text"]; content != "{}" {
		t.Fatalf("empty tool content fallback=%v", content)
	}

	conn.Close()

	// A connection the application refuses: list fails closed, calls report isError.
	denied, deniedReader := dialSocket(t, root)
	writeLine(t, denied, `{"layerdraw_connection":"mcp-connection-denied"}`)
	writeLine(t, denied, `{"jsonrpc":"2.0","id":1,"method":"tools/list"}`)
	if response = readResponse(t, deniedReader); response["error"] == nil {
		t.Fatalf("denied connection listed tools: %v", response)
	}
	writeLine(t, denied, `{"jsonrpc":"2.0","id":2,"method":"tools/call","params":{"name":"layerdraw.unknown"}}`)
	response = readResponse(t, deniedReader)
	call, ok := response["result"].(map[string]any)
	if !ok || call["isError"] != true {
		t.Fatalf("denied call must surface isError: %v", response)
	}
	denied.Close()
}

// TestRunMCPStdioBridgePipesAndFailsClosed covers the --mcp-stdio subcommand:
// dial failure without a running Desktop, and a full pipe teardown on stdin EOF.
func TestRunMCPStdioBridgePipesAndFailsClosed(t *testing.T) {
	if err := RunMCPStdioBridge(t.TempDir(), "mcp-connection-valid"); err == nil {
		t.Fatal("bridging without a running Desktop succeeded")
	}
	root := t.TempDir()
	server, err := startMCPSocket(&fakeSocketApplication{}, root)
	if err != nil {
		t.Fatal(err)
	}
	defer server.close()
	readEnd, writeEnd, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	originalStdin := os.Stdin
	os.Stdin = readEnd
	t.Cleanup(func() { os.Stdin = originalStdin })
	_ = writeEnd.Close()
	if err := RunMCPStdioBridge(root, "mcp-connection-valid"); err != nil && err != io.EOF {
		t.Fatalf("stdio bridge err=%v", err)
	}
}
