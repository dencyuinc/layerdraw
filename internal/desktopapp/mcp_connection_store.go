// SPDX-License-Identifier: LicenseRef-LayerDraw-1.0

package desktopapp

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"time"

	"github.com/dencyuinc/layerdraw/gen/go/accessprotocol"
)

const mcpConnectionFilename = "mcp-connections.json"

type mcpConnectionSnapshot struct {
	Version     int             `json:"version"`
	Generation  uint64          `json:"generation"`
	Connections []MCPConnection `json:"connections"`
}

type mcpConnectionStore struct{ root string }

func loadMCPConnectionStore(root string, now time.Time) (*mcpConnectionStore, map[string]MCPConnection, error) {
	store := &mcpConnectionStore{root: root}
	connections := map[string]MCPConnection{}
	directory, err := os.OpenRoot(root)
	if errors.Is(err, fs.ErrNotExist) {
		return store, connections, nil
	}
	if err != nil {
		return nil, nil, err
	}
	defer directory.Close()
	info, err := directory.Lstat(mcpConnectionFilename)
	if errors.Is(err, fs.ErrNotExist) {
		return store, connections, nil
	}
	if err != nil {
		return nil, nil, err
	}
	if !info.Mode().IsRegular() || info.Mode().Perm()&0o077 != 0 || info.Size() > 4<<20 {
		return nil, nil, errors.New("desktop MCP connection metadata is insecure")
	}
	data, err := directory.ReadFile(mcpConnectionFilename)
	if err != nil {
		return nil, nil, err
	}
	var snapshot mcpConnectionSnapshot
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	if decoder.Decode(&snapshot) != nil || snapshot.Version != 1 || snapshot.Connections == nil {
		return nil, nil, errors.New("desktop MCP connection metadata is invalid")
	}
	if err := decoder.Decode(&struct{}{}); !errors.Is(err, io.EOF) {
		return nil, nil, errors.New("desktop MCP connection metadata has trailing data")
	}
	for _, connection := range snapshot.Connections {
		if !validStoredMCPConnection(connection) || connections[connection.ConnectionID].ConnectionID != "" {
			return nil, nil, errors.New("desktop MCP connection metadata is invalid")
		}
		if connection.Status == MCPConnectionConnected || connection.Status == MCPConnectionRevoking {
			connection.Status = MCPConnectionRestarted
		}
		if expiry, parseErr := time.Parse(time.RFC3339Nano, string(connection.ExpiresAt)); parseErr != nil || !now.Before(expiry) {
			connection.Status = MCPConnectionExpired
		}
		connections[connection.ConnectionID] = connection
	}
	return store, connections, nil
}

func validStoredMCPConnection(value MCPConnection) bool {
	if value.ConnectionID == "" || value.ClientID == "" || value.SessionID == "" || value.ProtocolVersion != MCPConnectionProtocolVersion || value.DocumentID == "" || value.DelegationID == "" || value.AgentID == "" || value.Generation == "" {
		return false
	}
	if _, err := time.Parse(time.RFC3339Nano, string(value.ExpiresAt)); err != nil {
		return false
	}
	if _, err := accessprotocol.EncodeAuthoringGrantSummary(value.GrantSummary); err != nil {
		return false
	}
	switch value.Status {
	case MCPConnectionConnected, MCPConnectionRevoking, MCPConnectionRevoked, MCPConnectionExpired, MCPConnectionRestarted:
		return true
	default:
		return false
	}
}

func (s *mcpConnectionStore) save(generation uint64, values map[string]MCPConnection) error {
	connections := make([]MCPConnection, 0, len(values))
	for _, connection := range values {
		connections = append(connections, connection)
	}
	sort.Slice(connections, func(i, j int) bool { return connections[i].ConnectionID < connections[j].ConnectionID })
	data, err := json.Marshal(mcpConnectionSnapshot{Version: 1, Generation: generation, Connections: connections})
	if err != nil {
		return err
	}
	temporary, err := os.CreateTemp(s.root, ".mcp-connections-")
	if err != nil {
		return err
	}
	name := temporary.Name()
	defer os.Remove(name)
	if err := temporary.Chmod(0o600); err != nil {
		temporary.Close()
		return err
	}
	if _, err := temporary.Write(data); err != nil {
		temporary.Close()
		return err
	}
	if err := temporary.Sync(); err != nil {
		temporary.Close()
		return err
	}
	if err := temporary.Close(); err != nil {
		return err
	}
	if err := os.Rename(name, filepath.Join(s.root, mcpConnectionFilename)); err != nil {
		return err
	}
	directory, err := os.Open(s.root)
	if err != nil {
		return err
	}
	defer directory.Close()
	return directory.Sync()
}
