// SPDX-License-Identifier: LicenseRef-LayerDraw-1.0

package mcphost

import (
	"bytes"
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"sync"
	"time"

	"github.com/dencyuinc/layerdraw/gen/go/accessprotocol"
)

type Config struct {
	Owner     Owner
	Transport Transport
	Limits    Limits
	Now       func() time.Time
}

type cursorBinding struct {
	tool, requestDigest, document, revision, access string
	continuation                                    json.RawMessage
	expires                                         time.Time
}

type Host struct {
	config     Config
	lifecycle  sync.Mutex
	mu         sync.Mutex
	running    bool
	generation uint64
	ctx        context.Context
	cancel     context.CancelFunc
	cursors    map[string]cursorBinding
	inflight   sync.WaitGroup
}

func New(config Config) (*Host, error) {
	if config.Owner == nil || config.Transport == nil {
		return nil, errors.New("mcp host composition is incomplete")
	}
	if config.Limits == (Limits{}) {
		config.Limits = DefaultLimits()
	}
	if config.Limits.MaxInputBytes <= 0 || config.Limits.MaxOutputBytes <= 0 || config.Limits.MaxItems <= 0 || config.Limits.MaxJSONDepth <= 0 || config.Limits.MaxCursors <= 0 || config.Limits.CursorTTL <= 0 {
		return nil, errors.New("mcp host limits are invalid")
	}
	if config.Now == nil {
		config.Now = time.Now
	}
	return &Host{config: config, cursors: map[string]cursorBinding{}}, nil
}

func (h *Host) Start(ctx context.Context) error {
	h.lifecycle.Lock()
	defer h.lifecycle.Unlock()
	h.mu.Lock()
	if h.running {
		h.mu.Unlock()
		return errors.New("mcp host already running")
	}
	h.mu.Unlock()
	if _, failure := h.capabilities(ctx); failure != nil {
		return errors.New("mcp owner capabilities unavailable")
	}
	hostCtx, cancel := context.WithCancel(context.Background())
	if err := safeTransportStart(ctx, h.config.Transport, h); err != nil {
		cancel()
		return errors.New("mcp transport failed")
	}
	h.mu.Lock()
	h.ctx, h.cancel = hostCtx, cancel
	h.running, h.generation = true, h.generation+1
	h.cursors = map[string]cursorBinding{}
	h.mu.Unlock()
	return nil
}

func (h *Host) Shutdown(ctx context.Context) error {
	h.lifecycle.Lock()
	defer h.lifecycle.Unlock()
	h.mu.Lock()
	if !h.running {
		h.mu.Unlock()
		return nil
	}
	h.running = false
	cancel := h.cancel
	h.cursors = map[string]cursorBinding{}
	h.mu.Unlock()
	cancel()
	transportErr := safeTransportShutdown(ctx, h.config.Transport)
	done := make(chan struct{})
	go func() { h.inflight.Wait(); close(done) }()
	select {
	case <-done:
	case <-ctx.Done():
		return context.Canceled
	}
	h.mu.Lock()
	h.cancel = nil
	h.ctx = nil
	h.mu.Unlock()
	if transportErr != nil {
		return errors.New("mcp transport failed")
	}
	return nil
}

func safeTransportStart(ctx context.Context, transport Transport, handler Handler) (err error) {
	defer func() {
		if recover() != nil {
			err = errors.New("mcp transport failed")
		}
	}()
	return transport.Start(ctx, handler)
}

func safeTransportShutdown(ctx context.Context, transport Transport) (err error) {
	defer func() {
		if recover() != nil {
			err = errors.New("mcp transport failed")
		}
	}()
	return transport.Shutdown(ctx)
}

func (h *Host) ListTools(ctx context.Context) ([]Tool, *Failure) {
	snapshot, failure := h.capabilities(ctx)
	if failure != nil {
		return nil, failure
	}
	tools := []Tool{{Name: "layerdraw.get_capabilities", Description: "Read effective Desktop capabilities and AuthoringGrantSummary.", InputSchema: objectSchema(), OutputSchema: objectSchema()}}
	for _, mapping := range toolCatalog {
		capability, ok := snapshot.Operations[mapping.operation]
		if !ok || !capability.Enabled {
			continue
		}
		tools = append(tools, Tool{Name: mapping.name, Description: mapping.description, InputSchema: clone(capability.InputSchema), OutputSchema: clone(capability.OutputSchema)})
	}
	return tools, nil
}

func (h *Host) CallTool(ctx context.Context, request CallToolRequest) (result CallToolResult) {
	requestCtx, done, failure := h.begin(ctx)
	if failure != nil {
		return CallToolResult{Failure: failure}
	}
	defer done()
	defer func() {
		if recover() != nil {
			result = CallToolResult{Failure: fail(ErrorOwnerFailure, false)}
		}
	}()
	if request.Name == "layerdraw.get_capabilities" {
		if request.Cursor != "" || !emptyObject(request.Arguments) {
			return CallToolResult{Failure: fail(ErrorInvalidRequest, false)}
		}
		snapshot, f := h.capabilities(requestCtx)
		if f != nil {
			return CallToolResult{Failure: f}
		}
		content, err := json.Marshal(snapshot)
		if err != nil || len(content) > h.config.Limits.MaxOutputBytes {
			return CallToolResult{Failure: fail(ErrorResourceExhausted, false)}
		}
		return CallToolResult{Content: content}
	}
	mapping, ok := mappingByName[request.Name]
	if !ok {
		return CallToolResult{Failure: fail(ErrorCapabilityUnavailable, false)}
	}
	if request.RequestID == "" || len(request.RequestID) > 128 || len(request.Arguments) > h.config.Limits.MaxInputBytes || !validJSON(request.Arguments, h.config.Limits.MaxJSONDepth) {
		return CallToolResult{Failure: fail(ErrorInvalidRequest, false)}
	}
	if mapping.bound && (request.Binding == nil || !request.Binding.valid()) {
		return CallToolResult{Failure: fail(ErrorStaleBinding, false)}
	}
	snapshot, f := h.capabilities(requestCtx)
	if f != nil {
		return CallToolResult{Failure: f}
	}
	capability, ok := snapshot.Operations[mapping.operation]
	if !ok || !capability.Enabled {
		return CallToolResult{Failure: fail(ErrorCapabilityUnavailable, false)}
	}
	digest := digestRequest(request.Name, request.Arguments)
	var continuation json.RawMessage
	if request.Cursor != "" {
		binding, ok := h.takeCursor(request.Cursor)
		if !ok || !cursorMatches(binding, mapping, digest, request.Binding) {
			return CallToolResult{Failure: fail(ErrorInvalidCursor, false)}
		}
		continuation = binding.continuation
	}
	response, err := h.config.Owner.Invoke(requestCtx, OwnerRequest{RequestID: request.RequestID, Operation: mapping.operation, Arguments: clone(request.Arguments), Continuation: continuation, Binding: cloneBinding(request.Binding)})
	if err != nil {
		return CallToolResult{Failure: mapOwnerError(requestCtx, err)}
	}
	if response.Items < 0 || response.Items > h.config.Limits.MaxItems || len(response.Content) > h.config.Limits.MaxOutputBytes || !validJSON(response.Content, h.config.Limits.MaxJSONDepth) {
		return CallToolResult{Failure: fail(ErrorResourceExhausted, false)}
	}
	result.Content = clone(response.Content)
	if len(response.NextCursor) > 0 {
		token, ok := h.issueCursor(mapping, digest, request.Binding, response.NextCursor)
		if !ok {
			return CallToolResult{Failure: fail(ErrorResourceExhausted, true)}
		}
		result.Cursor = token
	}
	return result
}

func (h *Host) ListResources(ctx context.Context) ([]Resource, *Failure) {
	snapshot, failure := h.capabilities(ctx)
	if failure != nil {
		return nil, failure
	}
	result := make([]Resource, 0, len(snapshot.Resources)+1)
	result = append(result, Resource{URI: "layerdraw://capabilities", Name: "Desktop capabilities", Description: "Effective capabilities and grant summary.", MimeType: "application/json"})
	for _, r := range snapshot.Resources {
		result = append(result, Resource{URI: r.URI, Name: r.Name, Description: r.Description, MimeType: r.MimeType})
	}
	return result, nil
}

func (h *Host) ReadResource(ctx context.Context, request ReadResourceRequest) (result ReadResourceResult) {
	requestCtx, done, failure := h.begin(ctx)
	if failure != nil {
		return ReadResourceResult{Failure: failure}
	}
	defer done()
	defer func() {
		if recover() != nil {
			result = ReadResourceResult{Failure: fail(ErrorOwnerFailure, false)}
		}
	}()
	if request.URI == "layerdraw://capabilities" {
		snapshot, f := h.capabilities(requestCtx)
		if f != nil {
			return ReadResourceResult{Failure: f}
		}
		content, err := json.Marshal(snapshot)
		if err != nil || len(content) > h.config.Limits.MaxOutputBytes {
			return ReadResourceResult{Failure: fail(ErrorResourceExhausted, false)}
		}
		return ReadResourceResult{Content: content, MimeType: "application/json"}
	}
	snapshot, f := h.capabilities(requestCtx)
	if f != nil {
		return ReadResourceResult{Failure: f}
	}
	var capability *ResourceCapability
	for i := range snapshot.Resources {
		if snapshot.Resources[i].URI == request.URI {
			capability = &snapshot.Resources[i]
			break
		}
	}
	if capability == nil {
		return ReadResourceResult{Failure: fail(ErrorCapabilityUnavailable, false)}
	}
	if capability.Bound && (request.Binding == nil || !request.Binding.valid()) {
		return ReadResourceResult{Failure: fail(ErrorStaleBinding, false)}
	}
	digest := digestRequest(request.URI, nil)
	var continuation json.RawMessage
	if request.Cursor != "" {
		binding, ok := h.takeCursor(request.Cursor)
		if !ok || binding.tool != request.URI || binding.requestDigest != digest || !sameBinding(binding, request.Binding) {
			return ReadResourceResult{Failure: fail(ErrorInvalidCursor, false)}
		}
		continuation = binding.continuation
	}
	response, err := h.config.Owner.ReadResource(requestCtx, ResourceRequest{URI: request.URI, Continuation: continuation, Binding: cloneBinding(request.Binding)})
	if err != nil {
		return ReadResourceResult{Failure: mapOwnerError(requestCtx, err)}
	}
	if response.Items < 0 || response.Items > h.config.Limits.MaxItems || len(response.Content) > h.config.Limits.MaxOutputBytes || !validJSON(response.Content, h.config.Limits.MaxJSONDepth) || response.MimeType != capability.MimeType {
		return ReadResourceResult{Failure: fail(ErrorResourceExhausted, false)}
	}
	result.Content, result.MimeType = clone(response.Content), capability.MimeType
	if len(response.NextCursor) > 0 {
		token, ok := h.issueCursor(toolMapping{name: request.URI, bound: capability.Bound}, digest, request.Binding, response.NextCursor)
		if !ok {
			return ReadResourceResult{Failure: fail(ErrorResourceExhausted, true)}
		}
		result.Cursor = token
	}
	return result
}

func (h *Host) begin(ctx context.Context) (context.Context, func(), *Failure) {
	h.mu.Lock()
	if !h.running {
		h.mu.Unlock()
		return nil, func() {}, fail(ErrorTransport, true)
	}
	base := h.ctx
	h.inflight.Add(1)
	h.mu.Unlock()
	combined, cancel := context.WithCancel(ctx)
	go func() {
		select {
		case <-base.Done():
			cancel()
		case <-combined.Done():
		}
	}()
	return combined, func() { cancel(); h.inflight.Done() }, nil
}

func (h *Host) capabilities(ctx context.Context) (snapshot CapabilitySnapshot, failure *Failure) {
	defer func() {
		if recover() != nil {
			failure = fail(ErrorOwnerFailure, false)
		}
	}()
	value, err := h.config.Owner.Capabilities(ctx)
	if err != nil {
		return CapabilitySnapshot{}, fail(ErrorOwnerFailure, true)
	}
	if value.ManifestETag == "" || !validSnapshot(value) {
		return CapabilitySnapshot{}, fail(ErrorOwnerFailure, false)
	}
	return value, nil
}

func validSnapshot(s CapabilitySnapshot) bool {
	if _, err := accessprotocol.EncodeAuthoringGrantSummary(s.GrantSummary); err != nil {
		return false
	}
	for _, c := range s.Operations {
		if c.Enabled && (!validJSON(c.InputSchema, 16) || !validJSON(c.OutputSchema, 16)) {
			return false
		}
	}
	seen := map[string]bool{}
	for _, r := range s.Resources {
		if r.URI == "" || r.URI == "layerdraw://capabilities" || r.Name == "" || r.MimeType == "" || seen[r.URI] || !validJSON(r.Schema, 16) {
			return false
		}
		seen[r.URI] = true
	}
	return true
}

func (h *Host) issueCursor(mapping toolMapping, digest string, b *Binding, next json.RawMessage) (string, bool) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.pruneCursors()
	if len(h.cursors) >= h.config.Limits.MaxCursors {
		return "", false
	}
	raw := make([]byte, 24)
	if _, err := rand.Read(raw); err != nil {
		return "", false
	}
	token := base64.RawURLEncoding.EncodeToString(raw)
	cb := cursorBinding{tool: mapping.name, requestDigest: digest, continuation: clone(next), expires: h.config.Now().Add(h.config.Limits.CursorTTL)}
	if b != nil {
		cb.document, cb.revision, cb.access = b.DocumentID, b.RevisionDigest, b.AccessFingerprint
	}
	h.cursors[token] = cb
	return token, true
}
func (h *Host) takeCursor(token string) (cursorBinding, bool) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.pruneCursors()
	binding, ok := h.cursors[token]
	if ok {
		delete(h.cursors, token)
	}
	return binding, ok
}
func (h *Host) pruneCursors() {
	now := h.config.Now()
	for token, b := range h.cursors {
		if !now.Before(b.expires) {
			delete(h.cursors, token)
		}
	}
}
func cursorMatches(c cursorBinding, m toolMapping, d string, b *Binding) bool {
	return c.tool == m.name && c.requestDigest == d && sameBinding(c, b)
}
func sameBinding(c cursorBinding, b *Binding) bool {
	if b == nil {
		return c.document == "" && c.revision == "" && c.access == ""
	}
	return c.document == b.DocumentID && c.revision == b.RevisionDigest && c.access == b.AccessFingerprint
}
func cloneBinding(b *Binding) *Binding {
	if b == nil {
		return nil
	}
	v := *b
	return &v
}
func clone(v json.RawMessage) json.RawMessage      { return append(json.RawMessage(nil), v...) }
func fail(code ErrorCode, retryable bool) *Failure { return &Failure{Code: code, Retryable: retryable} }
func mapOwnerError(ctx context.Context, err error) *Failure {
	if ctx.Err() != nil {
		return fail(ErrorCancelled, true)
	}
	var typed *OwnerError
	if errors.As(err, &typed) {
		switch typed.Code {
		case ErrorInvalidRequest, ErrorCapabilityUnavailable, ErrorInvalidCursor, ErrorStaleBinding, ErrorResourceExhausted, ErrorCancelled:
			return fail(typed.Code, typed.Code == ErrorCancelled)
		}
	}
	return fail(ErrorOwnerFailure, false)
}
func objectSchema() json.RawMessage {
	return json.RawMessage(`{"type":"object","additionalProperties":false}`)
}
func emptyObject(data []byte) bool {
	if len(data) == 0 {
		return true
	}
	var value map[string]json.RawMessage
	return json.Unmarshal(data, &value) == nil && value != nil && len(value) == 0
}
func digestRequest(name string, args []byte) string {
	return fmt.Sprintf("%s:%x", name, sha256Bytes(args))
}
func sha256Bytes(value []byte) [32]byte { return sha256.Sum256(value) }

func validJSON(data []byte, maxDepth int) bool {
	if len(data) == 0 {
		return false
	}
	var value any
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.UseNumber()
	if decoder.Decode(&value) != nil || decoder.Decode(&struct{}{}) != io.EOF {
		return false
	}
	return depth(value) <= maxDepth
}
func depth(value any) int {
	switch v := value.(type) {
	case []any:
		max := 1
		for _, x := range v {
			if d := 1 + depth(x); d > max {
				max = d
			}
		}
		return max
	case map[string]any:
		max := 1
		for _, x := range v {
			if d := 1 + depth(x); d > max {
				max = d
			}
		}
		return max
	default:
		return 0
	}
}
