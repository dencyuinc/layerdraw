// SPDX-License-Identifier: LicenseRef-LayerDraw-1.0

// Package mcphost maps canonical Engine and Runtime owner operations to MCP.
// It owns protocol adaptation, lifecycle, pagination and transport bounds. It
// deliberately owns no LDL, Query, View, Search, Access or Registry semantics.
package mcphost

import (
	"context"
	"encoding/json"
	"time"

	"github.com/dencyuinc/layerdraw/gen/go/accessprotocol"
)

type ErrorCode string

const (
	ErrorInvalidRequest        ErrorCode = "invalid_request"
	ErrorCapabilityUnavailable ErrorCode = "capability_unavailable"
	ErrorInvalidCursor         ErrorCode = "invalid_cursor"
	ErrorStaleBinding          ErrorCode = "stale_binding"
	ErrorResourceExhausted     ErrorCode = "resource_exhausted"
	ErrorCancelled             ErrorCode = "cancelled"
	ErrorTransport             ErrorCode = "transport_failure"
	ErrorOwnerFailure          ErrorCode = "owner_failure"
)

// Failure is intentionally closed. Provider messages, paths, source text,
// credentials and panic values never cross the MCP boundary.
type Failure struct {
	Code      ErrorCode `json:"code"`
	Retryable bool      `json:"retryable"`
}

// OwnerError is the only supported semantic-failure projection from an owner
// adapter. Detail text is intentionally neither required nor exposed.
type OwnerError struct{ Code ErrorCode }

func (e *OwnerError) Error() string { return string(e.Code) }

type Binding struct {
	DocumentID        string `json:"document_id"`
	RevisionDigest    string `json:"revision_digest"`
	AccessFingerprint string `json:"access_fingerprint"`
}

func (b Binding) valid() bool {
	return b.DocumentID != "" && b.RevisionDigest != "" && b.AccessFingerprint != ""
}

type OperationCapability struct {
	Enabled      bool            `json:"enabled"`
	InputSchema  json.RawMessage `json:"input_schema"`
	OutputSchema json.RawMessage `json:"output_schema"`
}

type ResourceCapability struct {
	URI         string          `json:"uri"`
	Name        string          `json:"name"`
	Description string          `json:"description"`
	MimeType    string          `json:"mime_type"`
	Schema      json.RawMessage `json:"schema"`
	Bound       bool            `json:"bound"`
}

// CapabilitySnapshot is produced by the trusted composition owner. The MCP
// host advertises only enabled operations and resources from this snapshot.
type CapabilitySnapshot struct {
	ManifestETag string                               `json:"manifest_etag"`
	Operations   map[string]OperationCapability       `json:"operations"`
	Resources    []ResourceCapability                 `json:"resources"`
	GrantSummary accessprotocol.AuthoringGrantSummary `json:"authoring_grant_summary"`
}

type OwnerRequest struct {
	RequestID    string
	Operation    string
	Arguments    json.RawMessage
	Continuation json.RawMessage
	Binding      *Binding
}

type OwnerResponse struct {
	Content    json.RawMessage
	NextCursor json.RawMessage
	Items      int
}

type ResourceRequest struct {
	URI          string
	Continuation json.RawMessage
	Binding      *Binding
}

type ResourceResponse struct {
	Content    json.RawMessage
	NextCursor json.RawMessage
	Items      int
	MimeType   string
}

// Owner is a typed adapter to the owning Engine/Runtime/application facades.
// Implementations must pass generated owner-protocol values, never raw LDL or
// provider queries, and remain responsible for final authorization/commit.
type Owner interface {
	Capabilities(context.Context) (CapabilitySnapshot, error)
	Invoke(context.Context, OwnerRequest) (OwnerResponse, error)
	ReadResource(context.Context, ResourceRequest) (ResourceResponse, error)
}

type Tool struct {
	Name         string          `json:"name"`
	Description  string          `json:"description"`
	InputSchema  json.RawMessage `json:"input_schema"`
	OutputSchema json.RawMessage `json:"output_schema"`
}

type CallToolRequest struct {
	Name      string          `json:"name"`
	RequestID string          `json:"request_id"`
	Arguments json.RawMessage `json:"arguments"`
	Cursor    string          `json:"cursor,omitempty"`
	Binding   *Binding        `json:"binding,omitempty"`
}

type CallToolResult struct {
	Content json.RawMessage `json:"content,omitempty"`
	Cursor  string          `json:"cursor,omitempty"`
	Failure *Failure        `json:"failure,omitempty"`
}

type Resource struct {
	URI         string `json:"uri"`
	Name        string `json:"name"`
	Description string `json:"description"`
	MimeType    string `json:"mime_type"`
}

type ReadResourceRequest struct {
	URI     string   `json:"uri"`
	Cursor  string   `json:"cursor,omitempty"`
	Binding *Binding `json:"binding,omitempty"`
}

type ReadResourceResult struct {
	Content  json.RawMessage `json:"content,omitempty"`
	MimeType string          `json:"mime_type,omitempty"`
	Cursor   string          `json:"cursor,omitempty"`
	Failure  *Failure        `json:"failure,omitempty"`
}

// Handler is the complete surface a local MCP transport may expose.
type Handler interface {
	ListTools(context.Context) ([]Tool, *Failure)
	CallTool(context.Context, CallToolRequest) CallToolResult
	ListResources(context.Context) ([]Resource, *Failure)
	ReadResource(context.Context, ReadResourceRequest) ReadResourceResult
}

type Transport interface {
	Start(context.Context, Handler) error
	Shutdown(context.Context) error
}

type Limits struct {
	MaxInputBytes  int
	MaxOutputBytes int
	MaxItems       int
	MaxJSONDepth   int
	MaxCursors     int
	CursorTTL      time.Duration
}

func DefaultLimits() Limits {
	return Limits{MaxInputBytes: 1 << 20, MaxOutputBytes: 8 << 20, MaxItems: 1000, MaxJSONDepth: 64, MaxCursors: 4096, CursorTTL: 10 * time.Minute}
}
