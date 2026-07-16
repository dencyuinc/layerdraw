// SPDX-License-Identifier: LicenseRef-LayerDraw-1.0

package stdio

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math"
	"sort"
	"strconv"
	"sync"

	"github.com/dencyuinc/layerdraw/gen/go/engineprotocol"
	"github.com/dencyuinc/layerdraw/internal/engine/endpoint"
)

const (
	TransportID = "stdio"

	DefaultMaxActiveStreams       = 64
	DefaultMaxPendingControlBytes = 16 << 20
	DefaultMaxConcurrentDispatch  = 4
	DefaultMaxReservedBlobBytes   = 576 << 20
	DefaultMaxOutputBlobBytes     = 512 << 20
	DefaultMaxUploadBlobCount     = 65_536
	DefaultMaxUploadMetadataBytes = 8 << 20
)

// SessionLimits bounds every connection-owned queue and byte reservoir.
type SessionLimits struct {
	MaxActiveStreams       int
	MaxPendingControlBytes uint64
	MaxConcurrentDispatch  int
	MaxReservedBlobBytes   uint64
	MaxOutputBlobBytes     uint64
	MaxUploadBlobCount     int
	MaxUploadMetadataBytes uint64
}

func DefaultSessionLimits() SessionLimits {
	return SessionLimits{
		MaxActiveStreams:       DefaultMaxActiveStreams,
		MaxPendingControlBytes: DefaultMaxPendingControlBytes,
		MaxConcurrentDispatch:  DefaultMaxConcurrentDispatch,
		MaxReservedBlobBytes:   DefaultMaxReservedBlobBytes,
		MaxOutputBlobBytes:     DefaultMaxOutputBlobBytes,
		MaxUploadBlobCount:     DefaultMaxUploadBlobCount,
		MaxUploadMetadataBytes: DefaultMaxUploadMetadataBytes,
	}
}

// SessionConfig contains the transport-neutral endpoint facades and fixed
// connection limits. It contains no compiler-domain policy.
type SessionConfig struct {
	Descriptor *endpoint.Descriptor
	Dispatcher *endpoint.CompileDispatcher
	Limits     SessionLimits
}

// SessionError is safe to print operationally. It never includes control,
// blob, compiler, filesystem, or underlying I/O data.
type SessionError struct{ Code string }

func (err *SessionError) Error() string {
	if err == nil {
		return "<nil>"
	}
	return "layerdraw stdio: " + err.Code
}

const (
	SessionErrorConfiguration = "configuration"
	SessionErrorFraming       = "framing"
	SessionErrorOutput        = "output"
	SessionErrorInvariant     = "invariant"
)

type streamPhase uint8

const (
	phaseQueued streamPhase = iota + 1
	phaseUploading
	phaseDispatching
	phaseTerminal
)

type terminalKind uint8

const (
	terminalUndecided terminalKind = iota
	terminalExecution
	terminalCancellation
	terminalTransport
)

// terminalArbiter is the stream's single linearization point. Execute result
// publication, cancellation/deadline, and correlated framing failures all
// compete here; the first terminal kind is immutable.
type terminalArbiter struct {
	mu          sync.Mutex
	kind        terminalKind
	response    endpoint.DispatchResponse
	responseSet bool
}

// claimExecutionPublication is the successful output sink's atomic commit
// point. The exact generated response is attached when Execute returns.
func (arbiter *terminalArbiter) claimExecutionPublication() bool {
	if arbiter == nil {
		return false
	}
	arbiter.mu.Lock()
	defer arbiter.mu.Unlock()
	if arbiter.kind == terminalUndecided {
		arbiter.kind = terminalExecution
	}
	return arbiter.kind == terminalExecution
}

// claimExecution linearizes a non-publication Execute result, or completes an
// already-committed publication with its exact generated response.
func (arbiter *terminalArbiter) claimExecution(raw any) bool {
	if arbiter == nil {
		return false
	}
	response, ok := dispatchResponse(raw)
	if !ok {
		return false
	}
	arbiter.mu.Lock()
	defer arbiter.mu.Unlock()
	if arbiter.kind == terminalUndecided {
		arbiter.kind = terminalExecution
	}
	if arbiter.kind != terminalExecution {
		return false
	}
	if !arbiter.responseSet {
		arbiter.response = response
		arbiter.responseSet = true
	}
	return true
}

func (arbiter *terminalArbiter) claimCancellation(raw any) bool {
	if arbiter == nil {
		return false
	}
	response, ok := dispatchResponse(raw)
	if !ok {
		return false
	}
	arbiter.mu.Lock()
	defer arbiter.mu.Unlock()
	if arbiter.kind != terminalUndecided {
		return false
	}
	arbiter.kind = terminalCancellation
	arbiter.response = response
	arbiter.responseSet = true
	return true
}

func (arbiter *terminalArbiter) claimTransport(raw any) bool {
	if arbiter == nil {
		return false
	}
	response, ok := dispatchResponse(raw)
	if !ok {
		return false
	}
	arbiter.mu.Lock()
	defer arbiter.mu.Unlock()
	if arbiter.kind != terminalUndecided {
		return false
	}
	arbiter.kind = terminalTransport
	arbiter.response = response
	arbiter.responseSet = true
	return true
}

func dispatchResponse(raw any) (endpoint.DispatchResponse, bool) {
	switch value := raw.(type) {
	case endpoint.DispatchResponse:
		return value, value.Control != nil
	case engineprotocol.CompileResponseEnvelope:
		encoded, err := engineprotocol.EncodeCompileResponseEnvelope(value)
		if err != nil {
			return endpoint.DispatchResponse{}, false
		}
		return endpoint.DispatchResponse{Operation: endpoint.OperationCompile, RequestID: value.RequestID, Control: encoded, Outcome: value.Outcome, Failure: value.Failure}, true
	default:
		return endpoint.DispatchResponse{}, false
	}
}

func (arbiter *terminalArbiter) snapshot() (terminalKind, endpoint.DispatchResponse, bool) {
	if arbiter == nil {
		return terminalUndecided, endpoint.DispatchResponse{}, false
	}
	arbiter.mu.Lock()
	defer arbiter.mu.Unlock()
	return arbiter.kind, arbiter.response, arbiter.responseSet
}

type requestStream struct {
	id           uint64
	requestID    string
	operation    string
	controlBytes uint64
	plan         endpoint.DispatchPlan
	ctx          context.Context
	cancel       context.CancelFunc
	phase        streamPhase
	validator    *BundleValidator
	blobs        map[string][]byte
	expected     map[string]uint64
	requirements []endpoint.BlobRequirement
	received     uint64
	reserved     uint64
	suppress     bool
	terminal     terminalArbiter
}

func (stream *requestStream) planOperation() string {
	if stream == nil || stream.operation == "" {
		return endpoint.OperationCompile
	}
	return stream.operation
}

type readResult struct {
	frame Frame
	err   error
}

type dispatchResult struct {
	stream   *requestStream
	response endpoint.DispatchResponse
	blobs    []endpoint.OutputBlob
	err      error
}

type session struct {
	config SessionConfig
	limits SessionLimits
	root   context.Context
	decode *Decoder
	encode *Encoder

	negotiated        *endpoint.NegotiatedContext
	streams           map[uint64]*requestStream
	highWater         uint64
	requestIDs        map[string]*requestStream
	queue             []*requestStream
	uploader          *requestStream
	active            int
	reserved          uint64
	controls          int
	controlSum        uint64
	results           chan dispatchResult
	deadlines         chan uint64
	draining          bool
	beforeSinkCommit  func()
	beforeResultClaim func()
}

// Serve runs one LDSP 1.0 connection until CLOSE or clean frame-boundary EOF.
// Fatal decoder/output errors poison the connection and are never resynced.
func Serve(ctx context.Context, input io.Reader, output io.Writer, config SessionConfig) (err error) {
	return serve(ctx, input, output, config, nil)
}

func serve(ctx context.Context, input io.Reader, output io.Writer, config SessionConfig, beforeSinkCommit func()) (err error) {
	if ctx == nil || input == nil || output == nil || config.Descriptor == nil || config.Dispatcher == nil {
		return &SessionError{Code: SessionErrorConfiguration}
	}
	limits, ok := normalizeSessionLimits(config.Limits)
	if !ok {
		return &SessionError{Code: SessionErrorConfiguration}
	}
	s := &session{
		config:           config,
		limits:           limits,
		root:             ctx,
		decode:           NewDecoder(input),
		encode:           NewEncoder(output),
		streams:          make(map[uint64]*requestStream),
		requestIDs:       make(map[string]*requestStream),
		results:          make(chan dispatchResult, limits.MaxConcurrentDispatch),
		deadlines:        make(chan uint64, limits.MaxActiveStreams),
		beforeSinkCommit: beforeSinkCommit,
	}
	defer func() {
		if recover() != nil {
			s.abortAll()
			err = &SessionError{Code: SessionErrorInvariant}
		}
	}()
	return s.run()
}

func normalizeSessionLimits(input SessionLimits) (SessionLimits, bool) {
	if input == (SessionLimits{}) {
		return DefaultSessionLimits(), true
	}
	if input.MaxActiveStreams <= 0 || input.MaxPendingControlBytes == 0 ||
		input.MaxConcurrentDispatch <= 0 || input.MaxReservedBlobBytes == 0 ||
		input.MaxOutputBlobBytes == 0 || input.MaxUploadBlobCount <= 0 ||
		input.MaxUploadMetadataBytes == 0 || input.MaxConcurrentDispatch > input.MaxActiveStreams {
		return SessionLimits{}, false
	}
	return input, true
}

func (s *session) run() error {
	reads := make(chan readResult, 1)
	stopReads := make(chan struct{})
	defer close(stopReads)
	go func() {
		defer func() {
			if recover() != nil {
				select {
				case reads <- readResult{err: newError(ErrorHeaderRead, StageHeader, true, nil)}:
				case <-stopReads:
				}
			}
		}()
		for {
			frame, err := s.decode.ReadFrame()
			select {
			case reads <- readResult{frame: frame, err: err}:
			case <-stopReads:
				return
			}
			if err != nil && (err == io.EOF || IsFatal(err)) {
				return
			}
		}
	}()

	for {
		select {
		case <-s.root.Done():
			s.abortAll()
			return s.drain(false)
		case streamID := <-s.deadlines:
			if stream := s.streams[streamID]; stream != nil && stream.phase != phaseTerminal && stream.ctx.Err() != nil {
				if err := s.cancelStream(stream, true); err != nil {
					s.abortAll()
					return &SessionError{Code: SessionErrorOutput}
				}
			}
		case result := <-s.results:
			if err := s.finishDispatch(result); err != nil {
				s.abortAll()
				return err
			}
		case read := <-reads:
			if read.err == io.EOF {
				return s.drain(true)
			}
			if read.err != nil {
				if IsFatal(read.err) {
					s.abortAll()
					return &SessionError{Code: SessionErrorFraming}
				}
				if err := s.recoverableFrameError(read.frame); err != nil {
					s.abortAll()
					return err
				}
				continue
			}
			if read.frame.Kind == KindClose {
				return s.drain(true)
			}
			if err := s.accept(read.frame); err != nil {
				s.abortAll()
				return err
			}
			if s.draining {
				return s.drain(true)
			}
		}
	}
}

func (s *session) accept(frame Frame) error {
	switch frame.Kind {
	case KindRequestControl:
		return s.acceptControl(frame)
	case KindBlobChunk, KindBundleEnd:
		return s.acceptUpload(frame)
	case KindCancel:
		if stream := s.streams[frame.StreamID]; stream != nil && stream.phase != phaseTerminal {
			return s.cancelStream(stream, true)
		}
		return nil
	case KindRequestReady, KindResponseControl, KindStreamError:
		return s.acceptUnexpectedDirection(frame.StreamID)
	default:
		if frame.StreamID == 0 {
			return &SessionError{Code: SessionErrorFraming}
		}
		if stream := s.streams[frame.StreamID]; stream != nil {
			if stream.phase == phaseTerminal {
				return nil
			}
			return s.failCorrelatedStream(stream)
		}
		if frame.StreamID <= s.highWater {
			return nil
		}
		s.highWater = frame.StreamID
		return s.writeStreamError(frame.StreamID, "unexpected_direction")
	}
}

func (s *session) acceptUnexpectedDirection(streamID uint64) error {
	if stream := s.streams[streamID]; stream != nil {
		if stream.phase == phaseTerminal {
			return &SessionError{Code: SessionErrorFraming}
		}
		return s.failCorrelatedStream(stream)
	}
	if streamID <= s.highWater {
		// An engine-to-client frame replayed by the client cannot be assigned a
		// second response bundle without violating sequence authority.
		return &SessionError{Code: SessionErrorFraming}
	}
	s.highWater = streamID
	return s.writeStreamError(streamID, "unexpected_direction")
}

func (s *session) recoverableFrameError(frame Frame) error {
	if frame.StreamID == 0 {
		return &SessionError{Code: SessionErrorFraming}
	}
	if frame.Kind == KindRequestControl {
		if frame.StreamID <= s.highWater {
			return &SessionError{Code: SessionErrorFraming}
		}
		s.highWater = frame.StreamID
		return s.writeStreamError(frame.StreamID, "invalid_frame")
	}
	if frame.Kind == KindRequestReady || frame.Kind == KindResponseControl || frame.Kind == KindStreamError {
		return s.acceptUnexpectedDirection(frame.StreamID)
	}
	if stream := s.streams[frame.StreamID]; stream != nil && stream.phase != phaseTerminal {
		return s.failCorrelatedStream(stream)
	}
	if frame.StreamID <= s.highWater {
		return nil
	}
	s.highWater = frame.StreamID
	return s.writeStreamError(frame.StreamID, "invalid_frame")
}

type operationRoute struct {
	Operation string `json:"operation"`
	RequestID string `json:"request_id"`
}

func (s *session) acceptControl(frame Frame) error {
	if frame.StreamID <= s.highWater {
		return &SessionError{Code: SessionErrorFraming}
	}
	s.highWater = frame.StreamID

	var route operationRoute
	if err := json.Unmarshal(frame.Payload, &route); err != nil || route.Operation == "" || route.RequestID == "" {
		return s.writeStreamError(frame.StreamID, "invalid_control")
	}
	if err := endpoint.ValidateRequestID(route.RequestID); err != nil {
		return s.writeStreamError(frame.StreamID, "invalid_request_id")
	}
	if active := s.requestIDs[route.RequestID]; active != nil && active.phase != phaseTerminal {
		if s.negotiated != nil && s.negotiated.SupportsOperation(route.Operation) {
			response, err := s.config.Dispatcher.DispatchTransportFailureResponse(route.Operation, route.RequestID, endpoint.DispatchRelease(s.negotiated, s.config.Descriptor), endpoint.CompileTransportDuplicateRequest)
			if err != nil {
				return &SessionError{Code: SessionErrorInvariant}
			}
			return s.writeDispatch(frame.StreamID, response, nil)
		}
		return s.writeStreamError(frame.StreamID, "duplicate_request_id")
	}

	switch route.Operation {
	case endpoint.OperationHandshake:
		return s.acceptHandshake(frame)
	case endpoint.OperationCompile:
		return s.acceptOperation(frame, route)
	default:
		if s.negotiated == nil || !s.negotiated.SupportsOperation(route.Operation) {
			return s.writeStreamError(frame.StreamID, "unknown_operation")
		}
		return s.acceptOperation(frame, route)
	}
}

func (s *session) acceptHandshake(frame Frame) error {
	request, err := engineprotocol.DecodeHandshakeRequestEnvelope(frame.Payload)
	if err != nil {
		return s.writeStreamError(frame.StreamID, "invalid_control")
	}
	ctx, cancel, err := endpoint.RequestContext(s.root, request.DeadlineAt)
	if err != nil {
		return s.writeStreamError(frame.StreamID, "invalid_deadline")
	}
	defer cancel()
	var response engineprotocol.HandshakeResponseEnvelope
	var negotiated *endpoint.NegotiatedContext
	if s.negotiated != nil {
		response, err = s.config.Descriptor.RejectNegotiatedHandshake(request.RequestID)
	} else {
		response, negotiated, err = s.config.Descriptor.Negotiate(ctx, request)
	}
	if err != nil {
		return &SessionError{Code: SessionErrorInvariant}
	}
	encoded, err := engineprotocol.EncodeHandshakeResponseEnvelope(response)
	if err != nil {
		return &SessionError{Code: SessionErrorInvariant}
	}
	if err := s.encode.WriteFrames(controlBundle(KindResponseControl, frame.StreamID, encoded)); err != nil {
		return &SessionError{Code: SessionErrorOutput}
	}
	if negotiated != nil {
		s.negotiated = negotiated
	} else if s.negotiated != nil {
		// A second handshake ends this connection generation; the established
		// context is never retained after its rejection is committed.
		s.negotiated = nil
		s.draining = true
	}
	return nil
}

func (s *session) acceptOperation(frame Frame, route operationRoute) error {
	ctx, cancel, err := endpoint.RequestContextFromControl(s.root, frame.Payload)
	if err != nil {
		return s.writeStreamError(frame.StreamID, "invalid_deadline")
	}
	controlBytes := uint64(len(frame.Payload))
	if s.controls >= s.limits.MaxActiveStreams ||
		s.controlSum > math.MaxUint64-controlBytes ||
		s.controlSum+controlBytes > s.limits.MaxPendingControlBytes {
		cancel()
		response, responseErr := s.config.Dispatcher.DispatchTransportFailureResponse(route.Operation, route.RequestID, endpoint.DispatchRelease(s.negotiated, s.config.Descriptor), endpoint.CompileTransportResourceLimit)
		if responseErr != nil {
			return &SessionError{Code: SessionErrorInvariant}
		}
		return s.writeDispatch(frame.StreamID, response, nil)
	}
	plan, terminal, err := s.config.Dispatcher.PrepareDispatch(ctx, s.negotiated, route.Operation, frame.Payload)
	if err != nil {
		cancel()
		return s.writeStreamError(frame.StreamID, "endpoint_invariant")
	}
	if terminal != nil {
		cancel()
		return s.writeDispatch(frame.StreamID, *terminal, nil)
	}
	requirements := plan.BlobRequirements()
	budget := plan.AdmissionBudget()
	metadataBytes, metadataOK := uploadMetadataBytes(requirements, s.limits.MaxUploadMetadataBytes)
	if budget.RequiredBlobBytes < 0 || budget.RequiredBlobCount != int64(len(requirements)) ||
		uint64(budget.RequiredBlobBytes) > s.limits.MaxReservedBlobBytes ||
		len(requirements) > s.limits.MaxUploadBlobCount || !metadataOK ||
		metadataBytes > s.limits.MaxUploadMetadataBytes {
		plan.Abort()
		cancel()
		response, responseErr := s.config.Dispatcher.DispatchTransportFailureResponse(route.Operation, route.RequestID, endpoint.DispatchRelease(s.negotiated, s.config.Descriptor), endpoint.CompileTransportResourceLimit)
		if responseErr != nil {
			return &SessionError{Code: SessionErrorInvariant}
		}
		return s.writeDispatch(frame.StreamID, response, nil)
	}
	stream := &requestStream{
		id: frame.StreamID, requestID: route.RequestID, operation: route.Operation, controlBytes: controlBytes,
		plan: plan, requirements: requirements, ctx: ctx, cancel: cancel, phase: phaseQueued,
		reserved: uint64(budget.RequiredBlobBytes),
	}
	s.streams[frame.StreamID] = stream
	s.requestIDs[route.RequestID] = stream
	s.controls++
	s.controlSum += stream.controlBytes
	go func(id uint64, done <-chan struct{}) {
		<-done
		select {
		case s.deadlines <- id:
		case <-s.root.Done():
		}
	}(stream.id, ctx.Done())
	s.queue = append(s.queue, stream)
	return s.admit()
}

func uploadMetadataBytes(requirements []endpoint.BlobRequirement, maximum uint64) (uint64, bool) {
	var total uint64
	for _, requirement := range requirements {
		length := uint64(len(requirement.Ref.BlobID))
		if length > uint64(MaxNameBytes) || total > math.MaxUint64-length || total+length > maximum {
			return 0, false
		}
		total += length
	}
	return total, true
}

func (s *session) admit() error {
	if s.draining || s.uploader != nil || s.active >= s.limits.MaxConcurrentDispatch || len(s.queue) == 0 {
		return nil
	}
	stream := s.queue[0]
	if s.reserved > math.MaxUint64-stream.reserved || s.reserved+stream.reserved > s.limits.MaxReservedBlobBytes {
		return nil
	}
	s.queue = s.queue[1:]
	stream.phase = phaseUploading
	stream.validator = NewBundleValidator()
	stream.blobs = make(map[string][]byte)
	stream.expected = make(map[string]uint64)
	for _, requirement := range stream.requirements {
		size, err := strconv.ParseUint(string(requirement.Ref.Size), 10, 64)
		if err != nil || size > uint64(^uint(0)>>1) {
			return &SessionError{Code: SessionErrorInvariant}
		}
		stream.expected[requirement.Ref.BlobID] = size
	}
	s.uploader = stream
	s.active++
	s.reserved += stream.reserved
	if err := s.encode.WriteFrame(Frame{Kind: KindRequestReady, StreamID: stream.id}); err != nil {
		return &SessionError{Code: SessionErrorOutput}
	}
	return nil
}

func (s *session) acceptUpload(frame Frame) error {
	stream := s.streams[frame.StreamID]
	if stream == nil {
		if frame.StreamID <= s.highWater {
			return nil // historical late frames are bounded and drained
		}
		s.highWater = frame.StreamID
		return s.writeStreamError(frame.StreamID, "unknown_stream")
	}
	if stream.phase == phaseTerminal {
		return nil
	}
	if stream != s.uploader || stream.phase != phaseUploading {
		return s.failCorrelatedStream(stream)
	}
	var name string
	var current []byte
	var length uint64
	if frame.Kind == KindBlobChunk {
		length = uint64(len(frame.Payload))
		if stream.received > math.MaxUint64-length || stream.received+length > stream.reserved {
			return s.failCorrelatedStream(stream)
		}
		name = string(frame.Name)
		expected, known := stream.expected[name]
		if !known {
			// Requirement membership is checked before BundleValidator can retain
			// the incoming name. Unknown zero-byte blobs therefore cannot consume
			// count or metadata state.
			return s.failCorrelatedStream(stream)
		}
		current = stream.blobs[name]
		if length > expected || uint64(len(current)) > expected-length {
			return s.failCorrelatedStream(stream)
		}
	}
	if err := stream.validator.Accept(frame); err != nil {
		return s.failCorrelatedStream(stream)
	}
	if frame.Kind == KindBlobChunk {
		expected := stream.expected[name]
		if current == nil {
			current = make([]byte, 0, int(expected))
		}
		stream.blobs[name] = append(current, frame.Payload...)
		stream.received += length
		return nil
	}

	s.uploader = nil
	stream.phase = phaseDispatching
	s.dispatch(stream)
	return s.admit()
}

func (s *session) dispatch(stream *requestStream) {
	go func() {
		source := collectedSource{stream: stream}
		sink := &captureSink{
			ctx: stream.ctx, maximum: s.limits.MaxOutputBlobBytes,
			beforeCommit: s.beforeSinkCommit, commit: stream.terminal.claimExecutionPublication,
		}
		response, err := stream.plan.ExecuteDispatch(stream.ctx, source, sink)
		if s.beforeResultClaim != nil {
			s.beforeResultClaim()
		}
		stream.terminal.claimExecution(response)
		blobs := sink.take()
		for name := range stream.blobs {
			delete(stream.blobs, name)
		}
		stream.requirements = nil
		s.results <- dispatchResult{stream: stream, response: response, blobs: blobs, err: err}
	}()
}

func (s *session) finishDispatch(result dispatchResult) error {
	stream := result.stream
	if s.active > 0 {
		s.active--
	}
	if s.reserved >= stream.reserved {
		s.reserved -= stream.reserved
	}
	if !stream.suppress {
		kind, terminal, responseSet := stream.terminal.snapshot()
		if !responseSet {
			s.releaseStream(stream)
			return &SessionError{Code: SessionErrorInvariant}
		}
		switch kind {
		case terminalTransport, terminalCancellation:
			if err := s.writeDispatch(stream.id, terminal, nil); err != nil {
				s.releaseStream(stream)
				return err
			}
		case terminalExecution:
			if result.err != nil {
				s.releaseStream(stream)
				return &SessionError{Code: SessionErrorInvariant}
			}
			if err := s.writeDispatch(stream.id, terminal, result.blobs); err != nil {
				s.releaseStream(stream)
				return err
			}
		default:
			s.releaseStream(stream)
			return &SessionError{Code: SessionErrorInvariant}
		}
	}
	stream.phase = phaseTerminal
	s.releaseStream(stream)
	return s.admit()
}

func (s *session) cancelStream(stream *requestStream, emit bool) error {
	if stream == nil || stream.phase == phaseTerminal {
		return nil
	}
	if stream.phase == phaseDispatching {
		if !emit {
			stream.suppress = true
			stream.cancel()
			stream.plan.Abort()
			return nil
		}
		response, err := s.config.Dispatcher.DispatchCancellationResponse(stream.planOperation(), stream.requestID, s.negotiated.EngineRelease())
		if err != nil {
			return &SessionError{Code: SessionErrorInvariant}
		}
		if !stream.terminal.claimCancellation(response) {
			return nil
		}
		stream.cancel()
		stream.plan.Abort()
		return nil
	}
	response, err := s.config.Dispatcher.DispatchCancellationResponse(stream.planOperation(), stream.requestID, s.negotiated.EngineRelease())
	if err != nil {
		return &SessionError{Code: SessionErrorInvariant}
	}
	if !stream.terminal.claimCancellation(response) {
		return nil
	}
	stream.cancel()
	stream.plan.Abort()
	switch stream.phase {
	case phaseQueued:
		s.removeQueued(stream)
	case phaseUploading:
		if s.uploader == stream {
			s.uploader = nil
		}
		if s.active > 0 {
			s.active--
		}
		if s.reserved >= stream.reserved {
			s.reserved -= stream.reserved
		}
	}
	stream.phase = phaseTerminal
	if emit {
		if err := s.writeDispatch(stream.id, response, nil); err != nil {
			return err
		}
	}
	s.releaseStream(stream)
	return s.admit()
}

func (s *session) failCorrelatedStream(stream *requestStream) error {
	if stream == nil || stream.phase == phaseTerminal || stream.suppress {
		return nil
	}
	response, err := s.config.Dispatcher.DispatchTransportResponse(stream.planOperation(), stream.requestID, s.negotiated.EngineRelease())
	if err != nil {
		return &SessionError{Code: SessionErrorInvariant}
	}
	if !stream.terminal.claimTransport(response) {
		return nil
	}
	if stream.phase == phaseDispatching {
		stream.cancel()
		stream.plan.Abort()
		return nil
	}
	stream.plan.Abort()
	stream.cancel()
	if stream.phase == phaseQueued {
		s.removeQueued(stream)
	}
	if stream.phase == phaseUploading {
		if s.uploader == stream {
			s.uploader = nil
		}
		if s.active > 0 {
			s.active--
		}
		if s.reserved >= stream.reserved {
			s.reserved -= stream.reserved
		}
	}
	stream.phase = phaseTerminal
	s.releaseStream(stream)
	if err := s.writeDispatch(stream.id, response, nil); err != nil {
		return err
	}
	return s.admit()
}

func (s *session) removeQueued(target *requestStream) {
	for index, stream := range s.queue {
		if stream == target {
			s.queue = append(s.queue[:index], s.queue[index+1:]...)
			return
		}
	}
}

func (s *session) releaseStream(stream *requestStream) {
	if stream == nil {
		return
	}
	if stream.cancel != nil {
		stream.cancel()
	}
	delete(s.streams, stream.id)
	if current := s.requestIDs[stream.requestID]; current == stream {
		delete(s.requestIDs, stream.requestID)
	}
	if stream.controlBytes != 0 {
		if s.controlSum >= stream.controlBytes {
			s.controlSum -= stream.controlBytes
		}
		stream.controlBytes = 0
		if s.controls > 0 {
			s.controls--
		}
	}
	for name := range stream.blobs {
		delete(stream.blobs, name)
	}
	stream.validator = nil
	stream.blobs = nil
	stream.requirements = nil
	stream.expected = nil
	stream.received = 0
}

func (s *session) abortAll() {
	s.draining = true
	for _, stream := range s.streams {
		if stream.phase == phaseTerminal {
			continue
		}
		stream.suppress = true
		if stream.cancel != nil {
			stream.cancel()
		}
		if stream.plan != nil {
			stream.plan.Abort()
		}
		if stream.phase != phaseDispatching {
			if stream.phase == phaseUploading {
				if s.active > 0 {
					s.active--
				}
				if s.reserved >= stream.reserved {
					s.reserved -= stream.reserved
				}
			}
			stream.phase = phaseTerminal
			s.releaseStream(stream)
		}
	}
	s.queue = nil
	s.uploader = nil
}

func (s *session) drain(orderly bool) error {
	s.draining = true
	// Pending-credit and partial-upload work is cancelled. Sealed dispatches
	// retain their request context and are allowed to publish.
	for _, stream := range s.streams {
		if stream.phase == phaseQueued || stream.phase == phaseUploading {
			if err := s.cancelStream(stream, orderly); err != nil {
				return err
			}
		}
	}
	for s.active > 0 {
		result := <-s.results
		if err := s.finishDispatch(result); err != nil {
			return err
		}
	}
	if !orderly && s.root.Err() != nil {
		return s.root.Err()
	}
	return nil
}

func (s *session) writeStreamError(streamID uint64, code string) error {
	if len(code) == 0 || len(code) > int(MaxStreamErrorBytes) {
		return &SessionError{Code: SessionErrorInvariant}
	}
	frames := []Frame{
		{Kind: KindStreamError, StreamID: streamID, Name: []byte(code)},
		{Kind: KindBundleEnd, StreamID: streamID, Sequence: 1},
	}
	if err := s.encode.WriteFrames(frames); err != nil {
		return &SessionError{Code: SessionErrorOutput}
	}
	return nil
}

func controlBundle(kind Kind, streamID uint64, payload []byte) []Frame {
	return []Frame{
		{Kind: kind, StreamID: streamID, Payload: payload},
		{Kind: KindBundleEnd, StreamID: streamID, Sequence: 1},
	}
}

func (s *session) writeDispatch(streamID uint64, response endpoint.DispatchResponse, blobs []endpoint.OutputBlob) error {
	frames, err := responseBundle(streamID, response.Control, blobs)
	if err != nil {
		return &SessionError{Code: SessionErrorInvariant}
	}
	if err := s.encode.WriteFrames(frames); err != nil {
		return &SessionError{Code: SessionErrorOutput}
	}
	return nil
}

func (s *session) writeCompile(streamID uint64, response engineprotocol.CompileResponseEnvelope, blobs []endpoint.OutputBlob) error {
	encoded, err := engineprotocol.EncodeCompileResponseEnvelope(response)
	if err != nil {
		return &SessionError{Code: SessionErrorInvariant}
	}
	return s.writeDispatch(streamID, endpoint.DispatchResponse{Operation: endpoint.OperationCompile, RequestID: response.RequestID, Control: encoded, Outcome: response.Outcome, Failure: response.Failure}, blobs)
}

func responseBundle(streamID uint64, control []byte, blobs []endpoint.OutputBlob) ([]Frame, error) {
	frames := []Frame{{Kind: KindResponseControl, StreamID: streamID, Payload: control}}
	ordered := append([]endpoint.OutputBlob(nil), blobs...)
	sort.Slice(ordered, func(left, right int) bool {
		return ordered[left].Ref.BlobID < ordered[right].Ref.BlobID
	})
	sequence := uint32(1)
	for index, blob := range ordered {
		if index > 0 && ordered[index-1].Ref.BlobID == blob.Ref.BlobID {
			return nil, errors.New("duplicate output blob")
		}
		plan, err := PlanChunks(uint64(len(blob.Bytes)), sequence)
		if err != nil {
			return nil, err
		}
		for chunkIndex := uint32(0); chunkIndex < plan.ChunkCount; chunkIndex++ {
			chunk, err := plan.Chunk(chunkIndex)
			if err != nil {
				return nil, err
			}
			end := chunk.Offset + chunk.Length
			flags := Flags(0)
			if chunk.Final {
				flags = FlagFinal
			}
			frames = append(frames, Frame{
				Kind: KindBlobChunk, Flags: flags, StreamID: streamID, Sequence: chunk.Sequence,
				Name: []byte(blob.Ref.BlobID), Payload: blob.Bytes[int(chunk.Offset):int(end)], Offset: chunk.Offset,
			})
		}
		sequence = plan.EndSequence
	}
	frames = append(frames, Frame{Kind: KindBundleEnd, StreamID: streamID, Sequence: sequence})
	return frames, nil
}

type collectedSource struct{ stream *requestStream }

func (source collectedSource) Definitions(context.Context) ([]endpoint.BlobDefinition, error) {
	if source.stream == nil {
		return []endpoint.BlobDefinition{}, nil
	}
	definitions := make([]endpoint.BlobDefinition, 0, len(source.stream.requirements))
	for _, requirement := range source.stream.requirements {
		blobID := requirement.Ref.BlobID
		bytes, found := source.stream.blobs[blobID]
		if !found {
			continue
		}
		definitions = append(definitions, endpoint.BlobDefinition{
			BlobID: blobID,
			Owned:  &endpoint.OwnedBlob{Bytes: bytes, Release: func() {}},
		})
	}
	return definitions, nil
}

type captureSink struct {
	mu           sync.Mutex
	ctx          context.Context
	maximum      uint64
	blobs        []endpoint.OutputBlob
	beforeCommit func()
	commit       func() bool
}

func (sink *captureSink) Publish(ctx context.Context, blobs []endpoint.OutputBlob) error {
	if ctx == nil || sink == nil || sink.ctx == nil {
		return fmt.Errorf("invalid output sink")
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	var total uint64
	for _, blob := range blobs {
		length := uint64(len(blob.Bytes))
		if total > math.MaxUint64-length || total+length > sink.maximum {
			return fmt.Errorf("output limit")
		}
		total += length
	}
	owned := make([]endpoint.OutputBlob, len(blobs))
	for index, blob := range blobs {
		owned[index] = blob
		owned[index].Bytes = append([]byte(nil), blob.Bytes...)
	}
	if sink.beforeCommit != nil {
		sink.beforeCommit()
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	if sink.commit != nil && !sink.commit() {
		return context.Canceled
	}
	sink.mu.Lock()
	sink.blobs = owned
	sink.mu.Unlock()
	return nil
}

func (sink *captureSink) take() []endpoint.OutputBlob {
	if sink == nil {
		return nil
	}
	sink.mu.Lock()
	defer sink.mu.Unlock()
	result := sink.blobs
	sink.blobs = nil
	return result
}
