// SPDX-License-Identifier: LicenseRef-LayerDraw-1.0

package desktopwails

import (
	"bufio"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/dencyuinc/layerdraw/internal/desktopapp"
	"github.com/dencyuinc/layerdraw/internal/mcphost"
)

// mcpSocketFileName is the well-known endpoint inside the Desktop state root.
// The stdio bridge subcommand (`--mcp-stdio <connection_id>`) dials it, so
// external MCP clients configure the Desktop executable as their server
// command and never touch the socket directly.
const mcpSocketFileName = "mcp.sock"

// mcpProtocolVersion is the MCP revision this transport speaks.
const mcpProtocolVersion = "2024-11-05"

// mcpSocketPath places the endpoint inside the state root, falling back to a
// digest-named socket in the system temp directory when the state root would
// exceed the platform's unix socket path limit (104 bytes on macOS).
func mcpSocketPath(stateRoot string) string {
	path := filepath.Join(stateRoot, mcpSocketFileName)
	if len(path) <= 100 {
		return path
	}
	digest := sha256.Sum256([]byte(stateRoot))
	return filepath.Join(os.TempDir(), fmt.Sprintf("layerdraw-mcp-%s.sock", hex.EncodeToString(digest[:8])))
}

// mcpSocketApplication is the exact application surface the socket transport
// consumes; connection gating stays inside the application (enable state,
// expiry, permissions, generation fencing).
type mcpSocketApplication interface {
	MCPListConnectionTools(ctx context.Context, connectionID string) ([]mcphost.Tool, *mcphost.Failure)
	MCPCallConnectionTool(ctx context.Context, connectionID string, request mcphost.CallToolRequest) mcphost.CallToolResult
}

type mcpSocketServer struct {
	app      mcpSocketApplication
	listener net.Listener
	ctx      context.Context
	cancel   context.CancelFunc
	wg       sync.WaitGroup

	mu    sync.Mutex
	conns map[net.Conn]struct{}
}

func startMCPSocket(app mcpSocketApplication, stateRoot string) (*mcpSocketServer, error) {
	path := mcpSocketPath(stateRoot)
	_ = os.Remove(path)
	listener, err := net.Listen("unix", path)
	if err != nil {
		return nil, err
	}
	if err := os.Chmod(path, 0o600); err != nil {
		_ = listener.Close()
		return nil, err
	}
	ctx, cancel := context.WithCancel(context.Background())
	server := &mcpSocketServer{app: app, listener: listener, ctx: ctx, cancel: cancel, conns: map[net.Conn]struct{}{}}
	server.wg.Add(1)
	go server.accept()
	return server, nil
}

func (s *mcpSocketServer) accept() {
	defer s.wg.Done()
	for {
		conn, err := s.listener.Accept()
		if err != nil {
			return
		}
		s.mu.Lock()
		s.conns[conn] = struct{}{}
		s.mu.Unlock()
		s.wg.Add(1)
		go func() {
			defer s.wg.Done()
			defer func() {
				_ = conn.Close()
				s.mu.Lock()
				delete(s.conns, conn)
				s.mu.Unlock()
			}()
			s.serve(conn)
		}()
	}
}

func (s *mcpSocketServer) close() {
	s.cancel()
	_ = s.listener.Close()
	s.mu.Lock()
	for conn := range s.conns {
		_ = conn.Close()
	}
	s.mu.Unlock()
	s.wg.Wait()
}

type jsonrpcRequest struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id"`
	Method  string          `json:"method"`
	Params  json.RawMessage `json:"params"`
}

type jsonrpcError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

type socketPreamble struct {
	Connection string `json:"layerdraw_connection"`
}

// serve speaks newline-delimited MCP JSON-RPC after a one-line preamble that
// names the UI-created connection this client was authorized as.
func (s *mcpSocketServer) serve(conn net.Conn) {
	reader := bufio.NewReaderSize(conn, 1<<20)
	writer := bufio.NewWriter(conn)
	writeMessage := func(value any) bool {
		encoded, err := json.Marshal(value)
		if err != nil {
			return false
		}
		if _, err := writer.Write(append(encoded, '\n')); err != nil {
			return false
		}
		return writer.Flush() == nil
	}

	preambleLine, err := readLine(reader, 16*1024)
	if err != nil {
		return
	}
	var preamble socketPreamble
	if json.Unmarshal(preambleLine, &preamble) != nil || preamble.Connection == "" {
		_ = writeMessage(map[string]any{"jsonrpc": "2.0", "id": nil, "error": jsonrpcError{Code: -32600, Message: "layerdraw connection preamble required"}})
		return
	}
	connectionID := preamble.Connection

	for {
		line, err := readLine(reader, 1<<22)
		if err != nil {
			return
		}
		if len(line) == 0 {
			continue
		}
		var request jsonrpcRequest
		if json.Unmarshal(line, &request) != nil {
			_ = writeMessage(map[string]any{"jsonrpc": "2.0", "id": nil, "error": jsonrpcError{Code: -32700, Message: "parse error"}})
			continue
		}
		if request.ID == nil {
			continue // notifications need no reply
		}
		ctx, cancel := context.WithTimeout(s.ctx, 60*time.Second)
		response := s.dispatch(ctx, connectionID, request)
		cancel()
		if !writeMessage(response) {
			return
		}
	}
}

func readLine(reader *bufio.Reader, limit int) ([]byte, error) {
	line, err := reader.ReadBytes('\n')
	if err != nil {
		return nil, err
	}
	if len(line) > limit {
		return nil, fmt.Errorf("mcp socket line exceeds %d bytes", limit)
	}
	return line[:len(line)-1], nil
}

func (s *mcpSocketServer) dispatch(ctx context.Context, connectionID string, request jsonrpcRequest) map[string]any {
	respond := func(result any) map[string]any {
		return map[string]any{"jsonrpc": "2.0", "id": request.ID, "result": result}
	}
	fail := func(code int, message string) map[string]any {
		return map[string]any{"jsonrpc": "2.0", "id": request.ID, "error": jsonrpcError{Code: code, Message: message}}
	}
	switch request.Method {
	case "initialize":
		return respond(map[string]any{
			"protocolVersion": mcpProtocolVersion,
			"capabilities":    map[string]any{"tools": map[string]any{}},
			"serverInfo":      map[string]any{"name": "LayerDraw Desktop", "version": "1"},
		})
	case "ping":
		return respond(map[string]any{})
	case "tools/list":
		tools, failure := s.app.MCPListConnectionTools(ctx, connectionID)
		if failure != nil {
			return fail(-32000, string(failure.Code))
		}
		wire := make([]map[string]any, 0, len(tools))
		for _, tool := range tools {
			entry := map[string]any{"name": tool.Name, "description": tool.Description}
			if len(tool.InputSchema) > 0 {
				entry["inputSchema"] = json.RawMessage(tool.InputSchema)
			}
			wire = append(wire, entry)
		}
		return respond(map[string]any{"tools": wire})
	case "tools/call":
		var params struct {
			Name      string          `json:"name"`
			Arguments json.RawMessage `json:"arguments"`
		}
		if json.Unmarshal(request.Params, &params) != nil || params.Name == "" {
			return fail(-32602, "invalid tools/call params")
		}
		arguments := params.Arguments
		if len(arguments) == 0 {
			arguments = json.RawMessage("{}")
		}
		result := s.app.MCPCallConnectionTool(ctx, connectionID, mcphost.CallToolRequest{
			Name: params.Name, RequestID: fmt.Sprintf("mcp-socket-%s", string(request.ID)), Arguments: arguments,
		})
		if result.Failure != nil {
			return respond(map[string]any{
				"isError": true,
				"content": []map[string]any{{"type": "text", "text": string(result.Failure.Code)}},
			})
		}
		text := string(result.Content)
		if text == "" {
			text = "{}"
		}
		return respond(map[string]any{
			"isError": false,
			"content": []map[string]any{{"type": "text", "text": text}},
		})
	default:
		return fail(-32601, "method not found")
	}
}

// RunMCPStdioBridge is the `--mcp-stdio <connection_id>` subcommand: it dials
// the running Desktop instance's MCP socket and pipes stdio through, so MCP
// clients configure the Desktop binary itself as a stdio server command.
func RunMCPStdioBridge(stateRoot string, connectionID string) error {
	conn, err := net.Dial("unix", mcpSocketPath(stateRoot))
	if err != nil {
		return fmt.Errorf("LayerDraw Desktop is not running or MCP socket unavailable: %w", err)
	}
	defer func() { _ = conn.Close() }()
	preamble, err := json.Marshal(socketPreamble{Connection: connectionID})
	if err != nil {
		return err
	}
	if _, err := conn.Write(append(preamble, '\n')); err != nil {
		return err
	}
	done := make(chan error, 2)
	go func() { _, err := io.Copy(conn, os.Stdin); done <- err }()
	go func() { _, err := io.Copy(os.Stdout, conn); done <- err }()
	return <-done
}

// MCPClientConfigJSON renders the copy-paste MCP client configuration for a
// UI-created connection (claude_desktop_config.json style).
func MCPClientConfigJSON(connectionID string) string {
	executable, err := os.Executable()
	if err != nil {
		executable = "LayerDraw"
	}
	config := map[string]any{
		"mcpServers": map[string]any{
			"layerdraw": map[string]any{
				"command": executable,
				"args":    []string{"--mcp-stdio", connectionID},
			},
		},
	}
	encoded, err := json.MarshalIndent(config, "", "  ")
	if err != nil {
		return ""
	}
	return string(encoded)
}

var _ mcpSocketApplication = (*desktopapp.Application)(nil)
