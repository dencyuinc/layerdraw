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
	"github.com/dencyuinc/layerdraw/gen/go/protocolcommon"
)

type Config struct {
	Owner     Owner
	Transport Transport
	Limits    Limits
	Now       func() time.Time
}

type cursorBinding struct {
	generation                                      uint64
	tool, requestDigest, document, revision, access string
	continuation                                    json.RawMessage
	expires                                         time.Time
}

type lifecycleState uint8

const (
	stateStopped lifecycleState = iota
	stateStarting
	stateReady
	stateDraining
)

type hostGeneration struct {
	id       uint64
	ctx      context.Context
	cancel   context.CancelFunc
	inflight sync.WaitGroup
	cursors  map[string]cursorBinding
}

type Host struct {
	config     Config
	lifecycle  sync.Mutex
	mu         sync.Mutex
	state      lifecycleState
	generation uint64
	current    *hostGeneration
}

func New(config Config) (*Host, error) {
	if config.Owner == nil || config.Transport == nil {
		return nil, errors.New("mcp host composition is incomplete")
	}
	if config.Limits == (Limits{}) {
		config.Limits = DefaultLimits()
	}
	if config.Limits.MaxInputBytes <= 0 || config.Limits.MaxOutputBytes <= 0 || config.Limits.MaxItems <= 0 || config.Limits.MaxJSONDepth <= 0 || config.Limits.MaxStringBytes <= 0 || config.Limits.MaxCapabilityBytes <= 0 || config.Limits.MaxCursors <= 0 || config.Limits.MaxCursorBytes <= 0 || config.Limits.CursorTTL <= 0 {
		return nil, errors.New("mcp host limits are invalid")
	}
	if config.Now == nil {
		config.Now = time.Now
	}
	return &Host{config: config, state: stateStopped}, nil
}

func (h *Host) Start(ctx context.Context) error {
	h.lifecycle.Lock()
	defer h.lifecycle.Unlock()
	h.mu.Lock()
	if h.state != stateStopped || h.current != nil {
		h.mu.Unlock()
		return errors.New("mcp host already running")
	}
	h.generation++
	hostCtx, cancel := context.WithCancel(context.Background())
	generation := &hostGeneration{id: h.generation, ctx: hostCtx, cancel: cancel, cursors: map[string]cursorBinding{}}
	h.current, h.state = generation, stateStarting
	h.mu.Unlock()
	if _, failure := h.probeCapabilities(ctx); failure != nil {
		h.rollbackStart(generation)
		return errors.New("mcp owner capabilities unavailable")
	}
	if err := safeTransportStart(ctx, h.config.Transport, h); err != nil {
		_ = safeTransportShutdown(context.WithoutCancel(ctx), h.config.Transport)
		h.rollbackStart(generation)
		return errors.New("mcp transport failed")
	}
	h.mu.Lock()
	if h.current != generation || h.state != stateStarting || ctx.Err() != nil {
		h.mu.Unlock()
		_ = safeTransportShutdown(context.WithoutCancel(ctx), h.config.Transport)
		h.rollbackStart(generation)
		return errors.New("mcp transport failed")
	}
	h.state = stateReady
	h.mu.Unlock()
	return nil
}

func (h *Host) rollbackStart(generation *hostGeneration) {
	generation.cancel()
	generation.inflight.Wait()
	h.mu.Lock()
	if h.current == generation {
		h.current, h.state = nil, stateStopped
	}
	h.mu.Unlock()
}

func (h *Host) Shutdown(ctx context.Context) error {
	h.lifecycle.Lock()
	defer h.lifecycle.Unlock()
	h.mu.Lock()
	if h.state == stateStopped && h.current == nil {
		h.mu.Unlock()
		return nil
	}
	if h.current == nil || (h.state != stateReady && h.state != stateDraining) {
		h.mu.Unlock()
		return errors.New("mcp host lifecycle is invalid")
	}
	generation := h.current
	h.state = stateDraining
	h.mu.Unlock()
	generation.cancel()
	transportErr := safeTransportShutdown(ctx, h.config.Transport)
	done := make(chan struct{})
	go func() { generation.inflight.Wait(); close(done) }()
	select {
	case <-done:
	case <-ctx.Done():
		return context.Canceled
	}
	if transportErr != nil {
		return errors.New("mcp transport failed")
	}
	h.mu.Lock()
	if h.current == generation {
		h.current, h.state = nil, stateStopped
	}
	h.mu.Unlock()
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
	requestCtx, generation, done, failure := h.begin(ctx)
	if failure != nil {
		return nil, failure
	}
	defer done()
	snapshot, failure := h.capabilitiesForGeneration(requestCtx, generation)
	if failure != nil {
		return nil, failure
	}
	tools := []Tool{{Name: "layerdraw.get_capabilities", Description: "Read effective Desktop capabilities and AuthoringGrantSummary.", InputSchema: objectSchema(), OutputSchema: objectSchema()}}
	for _, mapping := range toolCatalog {
		capability, ok := mappingCapability(snapshot, mapping)
		if !ok {
			continue
		}
		inputSchema := clone(capability.InputSchema)
		if mapping.previewOperation != "" {
			preview := snapshot.Operations[mapping.previewOperation]
			inputSchema = workflowSchema(preview.InputSchema, capability.InputSchema)
		}
		tools = append(tools, Tool{Name: mapping.name, Description: mapping.description, InputSchema: inputSchema, OutputSchema: clone(capability.OutputSchema)})
	}
	if len(tools) > h.config.Limits.MaxItems || !boundedEnvelope(tools, h.config.Limits.MaxOutputBytes, h.config.Limits.MaxJSONDepth) {
		return nil, fail(ErrorResourceExhausted, false)
	}
	if !h.publishable(generation) {
		return nil, fail(ErrorCancelled, true)
	}
	return tools, nil
}

func (h *Host) CallTool(ctx context.Context, request CallToolRequest) (result CallToolResult) {
	requestCtx, generation, done, failure := h.begin(ctx)
	if failure != nil {
		return CallToolResult{Failure: failure}
	}
	defer done()
	defer func() {
		if recover() != nil {
			result = CallToolResult{Failure: fail(ErrorOwnerFailure, false)}
		}
	}()
	if !validCallEnvelope(request, h.config.Limits) {
		return CallToolResult{Failure: fail(ErrorInvalidRequest, false)}
	}
	if request.Name == "layerdraw.get_capabilities" {
		if request.Cursor != "" || !emptyObject(request.Arguments) {
			return CallToolResult{Failure: fail(ErrorInvalidRequest, false)}
		}
		snapshot, f := h.capabilitiesForGeneration(requestCtx, generation)
		if f != nil {
			return CallToolResult{Failure: f}
		}
		content, err := json.Marshal(snapshot)
		if err != nil || !boundedEnvelope(CallToolResult{Content: content}, h.config.Limits.MaxOutputBytes, h.config.Limits.MaxJSONDepth) {
			return CallToolResult{Failure: fail(ErrorResourceExhausted, false)}
		}
		if !h.publishable(generation) {
			return CallToolResult{Failure: fail(ErrorCancelled, true)}
		}
		return CallToolResult{Content: content}
	}
	mapping, ok := mappingByName[request.Name]
	if !ok {
		return CallToolResult{Failure: fail(ErrorCapabilityUnavailable, false)}
	}
	if mapping.bound && (request.Binding == nil || !request.Binding.valid()) {
		return CallToolResult{Failure: fail(ErrorStaleBinding, false)}
	}
	snapshot, f := h.capabilitiesForGeneration(requestCtx, generation)
	if f != nil {
		return CallToolResult{Failure: f}
	}
	_, ok = mappingCapability(snapshot, mapping)
	if !ok {
		return CallToolResult{Failure: fail(ErrorCapabilityUnavailable, false)}
	}
	digest := digestRequest(request.Name, request.Arguments)
	var continuation json.RawMessage
	if request.Cursor != "" {
		binding, ok := h.takeCursor(generation, request.Cursor)
		if !ok || !cursorMatches(binding, mapping, digest, request.Binding) {
			return CallToolResult{Failure: fail(ErrorInvalidCursor, false)}
		}
		continuation = binding.continuation
	}
	response, err := h.invokeMapping(requestCtx, mapping, request, continuation)
	if err != nil {
		return CallToolResult{Failure: mapOwnerError(requestCtx, err)}
	}
	if !h.publishable(generation) {
		return CallToolResult{Failure: fail(ErrorCancelled, true)}
	}
	if response.Items < 0 || response.Items > h.config.Limits.MaxItems || !validJSONBounded(response.Content, h.config.Limits.MaxOutputBytes, h.config.Limits.MaxJSONDepth) || !validOptionalContinuation(response.NextCursor, h.config.Limits) {
		return CallToolResult{Failure: fail(ErrorResourceExhausted, false)}
	}
	result.Content = clone(response.Content)
	if len(response.NextCursor) > 0 {
		token, ok := h.issueCursor(generation, mapping, digest, request.Binding, response.NextCursor)
		if !ok {
			return CallToolResult{Failure: fail(ErrorResourceExhausted, true)}
		}
		result.Cursor = token
	}
	if !boundedEnvelope(result, h.config.Limits.MaxOutputBytes, h.config.Limits.MaxJSONDepth) {
		return CallToolResult{Failure: fail(ErrorResourceExhausted, false)}
	}
	if !h.publishable(generation) {
		return CallToolResult{Failure: fail(ErrorCancelled, true)}
	}
	return result
}

type workflowArguments struct {
	Preview json.RawMessage `json:"preview"`
	Commit  json.RawMessage `json:"commit"`
}

func (h *Host) invokeMapping(ctx context.Context, mapping toolMapping, request CallToolRequest, continuation json.RawMessage) (OwnerResponse, error) {
	if mapping.previewOperation == "" {
		return h.config.Owner.Invoke(ctx, OwnerRequest{RequestID: request.RequestID, Operation: mapping.operation, Arguments: clone(request.Arguments), Continuation: continuation, Binding: cloneBinding(request.Binding)})
	}
	var workflow workflowArguments
	if decodeExactJSON(request.Arguments, &workflow) != nil || !validJSONBounded(workflow.Preview, h.config.Limits.MaxInputBytes, h.config.Limits.MaxJSONDepth) || !validJSONBounded(workflow.Commit, h.config.Limits.MaxInputBytes, h.config.Limits.MaxJSONDepth) {
		return OwnerResponse{}, &OwnerError{Code: ErrorInvalidRequest}
	}
	preview, err := h.config.Owner.Invoke(ctx, OwnerRequest{RequestID: request.RequestID, Operation: mapping.previewOperation, Arguments: clone(workflow.Preview), Binding: cloneBinding(request.Binding)})
	if err != nil {
		return OwnerResponse{}, err
	}
	if preview.Items < 0 || preview.Items > h.config.Limits.MaxItems || !validJSONBounded(preview.Content, h.config.Limits.MaxOutputBytes, h.config.Limits.MaxJSONDepth) || len(preview.NextCursor) != 0 {
		return OwnerResponse{}, &OwnerError{Code: ErrorResourceExhausted}
	}
	if ctx.Err() != nil {
		return OwnerResponse{}, &OwnerError{Code: ErrorCancelled}
	}
	if preview.Outcome != "" && preview.Outcome != protocolcommon.OutcomeSuccess {
		return preview, nil
	}
	return h.config.Owner.Invoke(ctx, OwnerRequest{RequestID: request.RequestID, Operation: mapping.operation, Arguments: clone(workflow.Commit), Continuation: continuation, Binding: cloneBinding(request.Binding)})
}

func (h *Host) ListResources(ctx context.Context) ([]Resource, *Failure) {
	requestCtx, generation, done, failure := h.begin(ctx)
	if failure != nil {
		return nil, failure
	}
	defer done()
	snapshot, failure := h.capabilitiesForGeneration(requestCtx, generation)
	if failure != nil {
		return nil, failure
	}
	result := make([]Resource, 0, len(snapshot.Resources)+1)
	result = append(result, Resource{URI: "layerdraw://capabilities", Name: "Desktop capabilities", Description: "Effective capabilities and grant summary.", MimeType: "application/json"})
	for _, r := range snapshot.Resources {
		result = append(result, Resource{URI: r.URI, Name: r.Name, Description: r.Description, MimeType: r.MimeType})
	}
	if len(result) > h.config.Limits.MaxItems || !boundedEnvelope(result, h.config.Limits.MaxOutputBytes, h.config.Limits.MaxJSONDepth) {
		return nil, fail(ErrorResourceExhausted, false)
	}
	if !h.publishable(generation) {
		return nil, fail(ErrorCancelled, true)
	}
	return result, nil
}

func (h *Host) ReadResource(ctx context.Context, request ReadResourceRequest) (result ReadResourceResult) {
	requestCtx, generation, done, failure := h.begin(ctx)
	if failure != nil {
		return ReadResourceResult{Failure: failure}
	}
	defer done()
	defer func() {
		if recover() != nil {
			result = ReadResourceResult{Failure: fail(ErrorOwnerFailure, false)}
		}
	}()
	if !validResourceEnvelope(request, h.config.Limits) {
		return ReadResourceResult{Failure: fail(ErrorInvalidRequest, false)}
	}
	if request.URI == "layerdraw://capabilities" {
		snapshot, f := h.capabilitiesForGeneration(requestCtx, generation)
		if f != nil {
			return ReadResourceResult{Failure: f}
		}
		content, err := json.Marshal(snapshot)
		if err != nil || !boundedEnvelope(ReadResourceResult{Content: content, MimeType: "application/json"}, h.config.Limits.MaxOutputBytes, h.config.Limits.MaxJSONDepth) {
			return ReadResourceResult{Failure: fail(ErrorResourceExhausted, false)}
		}
		if !h.publishable(generation) {
			return ReadResourceResult{Failure: fail(ErrorCancelled, true)}
		}
		return ReadResourceResult{Content: content, MimeType: "application/json"}
	}
	snapshot, f := h.capabilitiesForGeneration(requestCtx, generation)
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
		binding, ok := h.takeCursor(generation, request.Cursor)
		if !ok || binding.tool != request.URI || binding.requestDigest != digest || !sameBinding(binding, request.Binding) {
			return ReadResourceResult{Failure: fail(ErrorInvalidCursor, false)}
		}
		continuation = binding.continuation
	}
	response, err := h.config.Owner.ReadResource(requestCtx, ResourceRequest{URI: request.URI, Continuation: continuation, Binding: cloneBinding(request.Binding)})
	if err != nil {
		return ReadResourceResult{Failure: mapOwnerError(requestCtx, err)}
	}
	if !h.publishable(generation) {
		return ReadResourceResult{Failure: fail(ErrorCancelled, true)}
	}
	if response.Items < 0 || response.Items > h.config.Limits.MaxItems || !validJSONBounded(response.Content, h.config.Limits.MaxOutputBytes, h.config.Limits.MaxJSONDepth) || !validOptionalContinuation(response.NextCursor, h.config.Limits) || response.MimeType != capability.MimeType {
		return ReadResourceResult{Failure: fail(ErrorResourceExhausted, false)}
	}
	result.Content, result.MimeType = clone(response.Content), capability.MimeType
	if len(response.NextCursor) > 0 {
		token, ok := h.issueCursor(generation, toolMapping{name: request.URI, bound: capability.Bound}, digest, request.Binding, response.NextCursor)
		if !ok {
			return ReadResourceResult{Failure: fail(ErrorResourceExhausted, true)}
		}
		result.Cursor = token
	}
	if !boundedEnvelope(result, h.config.Limits.MaxOutputBytes, h.config.Limits.MaxJSONDepth) {
		return ReadResourceResult{Failure: fail(ErrorResourceExhausted, false)}
	}
	if !h.publishable(generation) {
		return ReadResourceResult{Failure: fail(ErrorCancelled, true)}
	}
	return result
}

func (h *Host) begin(ctx context.Context) (context.Context, *hostGeneration, func(), *Failure) {
	h.mu.Lock()
	if h.current == nil || (h.state != stateStarting && h.state != stateReady) {
		h.mu.Unlock()
		return nil, nil, func() {}, fail(ErrorTransport, true)
	}
	generation := h.current
	generation.inflight.Add(1)
	h.mu.Unlock()
	combined, cancel := context.WithCancel(ctx)
	go func() {
		select {
		case <-generation.ctx.Done():
			cancel()
		case <-combined.Done():
		}
	}()
	return combined, generation, func() { cancel(); generation.inflight.Done() }, nil
}

func (h *Host) publishable(generation *hostGeneration) bool {
	h.mu.Lock()
	defer h.mu.Unlock()
	return generation != nil && h.current == generation && (h.state == stateStarting || h.state == stateReady) && generation.ctx.Err() == nil
}

func (h *Host) probeCapabilities(ctx context.Context) (snapshot CapabilitySnapshot, failure *Failure) {
	defer func() {
		if recover() != nil {
			failure = fail(ErrorOwnerFailure, false)
		}
	}()
	value, err := h.config.Owner.Capabilities(ctx)
	if err != nil {
		return CapabilitySnapshot{}, fail(ErrorOwnerFailure, true)
	}
	value, err = cloneSnapshot(value)
	if err != nil || !validSnapshot(value, h.config.Limits) {
		return CapabilitySnapshot{}, fail(ErrorOwnerFailure, false)
	}
	return value, nil
}

func (h *Host) capabilitiesForGeneration(ctx context.Context, generation *hostGeneration) (CapabilitySnapshot, *Failure) {
	value, failure := h.probeCapabilities(ctx)
	if failure != nil {
		return CapabilitySnapshot{}, failure
	}
	if !h.publishable(generation) {
		return CapabilitySnapshot{}, fail(ErrorCancelled, true)
	}
	return value, nil
}

func mappingCapability(snapshot CapabilitySnapshot, mapping toolMapping) (OperationCapability, bool) {
	if mapping.operation == "" {
		return OperationCapability{}, false
	}
	operations := mapping.requiredOperations
	if len(operations) == 0 {
		operations = []string{mapping.operation}
	}
	for _, operation := range operations {
		capability, ok := snapshot.Operations[operation]
		if !ok || !capability.Enabled {
			return OperationCapability{}, false
		}
	}
	capability, ok := snapshot.Operations[mapping.operation]
	return capability, ok && capability.Enabled
}

func validSnapshot(s CapabilitySnapshot, limits Limits) bool {
	if _, err := protocolcommon.EncodeManifestETag(s.ManifestETag); err != nil {
		return false
	}
	encoded, err := json.Marshal(s)
	if err != nil || len(encoded) > limits.MaxCapabilityBytes || !validJSON(encoded, limits.MaxJSONDepth+3) || len(s.Operations)+len(s.Resources) > limits.MaxItems {
		return false
	}
	if _, err := accessprotocol.EncodeAuthoringGrantSummary(s.GrantSummary); err != nil {
		return false
	}
	for operation, c := range s.Operations {
		if !validString(operation, limits.MaxStringBytes) || c.Enabled && (!validSchema(c.InputSchema, limits) || !validSchema(c.OutputSchema, limits)) {
			return false
		}
	}
	seen := map[string]bool{}
	for _, r := range s.Resources {
		if !validString(r.URI, limits.MaxStringBytes) || r.URI == "layerdraw://capabilities" || !validString(r.Name, limits.MaxStringBytes) || !validString(r.Description, limits.MaxStringBytes) || !validString(r.MimeType, limits.MaxStringBytes) || seen[r.URI] || !validSchema(r.Schema, limits) {
			return false
		}
		seen[r.URI] = true
	}
	return true
}
func cloneSnapshot(value CapabilitySnapshot) (CapabilitySnapshot, error) {
	encoded, err := json.Marshal(value)
	if err != nil {
		return CapabilitySnapshot{}, err
	}
	var result CapabilitySnapshot
	if err = json.Unmarshal(encoded, &result); err != nil {
		return CapabilitySnapshot{}, err
	}
	return result, nil
}

func (h *Host) issueCursor(generation *hostGeneration, mapping toolMapping, digest string, b *Binding, next json.RawMessage) (string, bool) {
	if generation == nil || len(next) > h.config.Limits.MaxCursorBytes || !validJSONBounded(next, h.config.Limits.MaxCursorBytes, h.config.Limits.MaxJSONDepth) {
		return "", false
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.current != generation || (h.state != stateStarting && h.state != stateReady) || generation.ctx.Err() != nil {
		return "", false
	}
	h.pruneCursors(generation)
	if len(generation.cursors) >= h.config.Limits.MaxCursors {
		return "", false
	}
	raw := make([]byte, 24)
	if _, err := rand.Read(raw); err != nil {
		return "", false
	}
	token := base64.RawURLEncoding.EncodeToString(raw)
	cb := cursorBinding{generation: generation.id, tool: mapping.name, requestDigest: digest, continuation: clone(next), expires: h.config.Now().Add(h.config.Limits.CursorTTL)}
	if b != nil {
		cb.document, cb.revision, cb.access = string(b.DocumentID), string(b.RevisionDigest), string(b.AccessFingerprint)
	}
	generation.cursors[token] = cb
	return token, true
}
func (h *Host) takeCursor(generation *hostGeneration, token string) (cursorBinding, bool) {
	if generation == nil || !validCursorToken(token) {
		return cursorBinding{}, false
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.current != generation {
		return cursorBinding{}, false
	}
	h.pruneCursors(generation)
	binding, ok := generation.cursors[token]
	if ok {
		delete(generation.cursors, token)
	}
	return binding, ok && binding.generation == generation.id
}
func (h *Host) pruneCursors(generation *hostGeneration) {
	now := h.config.Now()
	for token, b := range generation.cursors {
		if !now.Before(b.expires) {
			delete(generation.cursors, token)
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
	return c.document == string(b.DocumentID) && c.revision == string(b.RevisionDigest) && c.access == string(b.AccessFingerprint)
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
func workflowSchema(preview, commit json.RawMessage) json.RawMessage {
	value := map[string]any{"type": "object", "additionalProperties": false, "required": []string{"preview", "commit"}, "properties": map[string]json.RawMessage{"preview": clone(preview), "commit": clone(commit)}}
	encoded, _ := json.Marshal(value)
	return encoded
}
func decodeExactJSON(data []byte, destination any) error {
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(destination); err != nil {
		return err
	}
	if decoder.Decode(&struct{}{}) != io.EOF {
		return errors.New("trailing JSON")
	}
	return nil
}
func validString(value string, maximum int) bool { return value != "" && len(value) <= maximum }
func validCursorToken(value string) bool {
	if len(value) != 32 {
		return false
	}
	decoded, err := base64.RawURLEncoding.DecodeString(value)
	return err == nil && len(decoded) == 24
}
func validJSONBounded(data []byte, maximum, depth int) bool {
	return len(data) > 0 && len(data) <= maximum && validJSON(data, depth)
}
func validSchema(data []byte, limits Limits) bool {
	if !validJSONBounded(data, limits.MaxCapabilityBytes, limits.MaxJSONDepth) {
		return false
	}
	var value map[string]json.RawMessage
	return json.Unmarshal(data, &value) == nil && value != nil
}
func validOptionalContinuation(data []byte, limits Limits) bool {
	return len(data) == 0 || validJSONBounded(data, limits.MaxCursorBytes, limits.MaxJSONDepth)
}
func boundedEnvelope(value any, maximum, depth int) bool {
	encoded, err := json.Marshal(value)
	return err == nil && len(encoded) <= maximum && validJSON(encoded, depth)
}
func validCallEnvelope(request CallToolRequest, limits Limits) bool {
	if !validString(request.Name, limits.MaxStringBytes) || !validString(request.RequestID, 128) || request.Cursor != "" && !validCursorToken(request.Cursor) || request.Binding != nil && !request.Binding.valid() {
		return false
	}
	if request.Name == "layerdraw.get_capabilities" {
		if !emptyObject(request.Arguments) {
			return false
		}
	} else if !validJSONBounded(request.Arguments, limits.MaxInputBytes, limits.MaxJSONDepth) {
		return false
	}
	return boundedEnvelope(request, limits.MaxInputBytes, limits.MaxJSONDepth+2)
}
func validResourceEnvelope(request ReadResourceRequest, limits Limits) bool {
	return validString(request.URI, limits.MaxStringBytes) && (request.Cursor == "" || validCursorToken(request.Cursor)) && (request.Binding == nil || request.Binding.valid()) && boundedEnvelope(request, limits.MaxInputBytes, limits.MaxJSONDepth+2)
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
