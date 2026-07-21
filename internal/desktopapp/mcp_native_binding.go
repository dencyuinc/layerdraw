// SPDX-License-Identifier: LicenseRef-LayerDraw-1.0

package desktopapp

import (
	"encoding/json"

	"github.com/dencyuinc/layerdraw/gen/go/runtimeprotocol"
	"github.com/dencyuinc/layerdraw/internal/mcphost"
	"github.com/dencyuinc/layerdraw/internal/runtime/port"
)

var nativeMCPOperations = map[string]bool{
	"native.execute_search":   true,
	"native.execute_query":    true,
	"native.execute_analysis": true,
}

// validateNativeMCPBinding prevents a valid native owner envelope from being
// replayed behind a different MCP document, revision, or Access binding. The
// Runtime endpoint still performs the authoritative session authorization.
func validateNativeMCPBinding(request mcphost.OwnerRequest) error {
	if !nativeMCPOperations[request.Operation] {
		return nil
	}
	if request.Binding == nil {
		return &mcphost.OwnerError{Code: mcphost.ErrorStaleBinding}
	}
	var envelope struct {
		Operation string          `json:"operation"`
		Payload   json.RawMessage `json:"payload"`
	}
	if json.Unmarshal(request.Arguments, &envelope) != nil || envelope.Operation != request.Operation || len(envelope.Payload) == 0 {
		return &mcphost.OwnerError{Code: mcphost.ErrorInvalidRequest}
	}
	var payload struct {
		Session                *runtimeprotocol.RuntimeSessionRef `json:"session"`
		Snapshot               port.DocumentSnapshotRef           `json:"snapshot"`
		AccessProjectionDigest string                             `json:"access_projection_digest"`
	}
	if json.Unmarshal(envelope.Payload, &payload) != nil || payload.Session == nil {
		return &mcphost.OwnerError{Code: mcphost.ErrorInvalidRequest}
	}
	if payload.AccessProjectionDigest == "" {
		var goWire struct {
			AccessProjectionDigest string
		}
		if json.Unmarshal(envelope.Payload, &goWire) != nil {
			return &mcphost.OwnerError{Code: mcphost.ErrorInvalidRequest}
		}
		payload.AccessProjectionDigest = goWire.AccessProjectionDigest
	}
	binding := request.Binding
	if payload.Session.Scope.DocumentID != binding.DocumentID ||
		payload.Session.Scope.AccessFingerprint != binding.AccessFingerprint ||
		payload.Snapshot.HostDocumentID != string(binding.DocumentID) ||
		payload.Snapshot.DefinitionHash != string(binding.RevisionDigest) ||
		payload.AccessProjectionDigest != string(binding.AccessFingerprint) {
		return &mcphost.OwnerError{Code: mcphost.ErrorStaleBinding}
	}
	return nil
}
