// SPDX-License-Identifier: LicenseRef-LayerDraw-1.0

package wasm

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"sort"
	"sync"

	"github.com/dencyuinc/layerdraw/gen/go/engineprotocol"
	"github.com/dencyuinc/layerdraw/internal/engine/endpoint"
)

type sessionState uint8

const (
	sessionActive sessionState = iota
	sessionDisposed
	sessionCrashed
)

// RequestBlobs owns the complete outer attachment table for one request.
// Bind runs after #29 PrepareCompile and before Definitions is allowed to copy
// bytes into Go. Release must discard every transport reference on all paths.
type RequestBlobs interface {
	endpoint.BlobSource
	Count() int
	Bind([]endpoint.BlobRequirement, TransportLimits) *LocalFailure
	Release()
}

// EmptyRequestBlobs is the canonical blobless request source.
type EmptyRequestBlobs struct{}

func (EmptyRequestBlobs) Count() int { return 0 }

func (EmptyRequestBlobs) Bind(requirements []endpoint.BlobRequirement, _ TransportLimits) *LocalFailure {
	if len(requirements) != 0 {
		return localFailure(FailureTransferFailed)
	}
	return nil
}

func (EmptyRequestBlobs) Definitions(context.Context) ([]endpoint.BlobDefinition, error) {
	return []endpoint.BlobDefinition{}, nil
}

func (EmptyRequestBlobs) Release() {}

// ResponseBlob contains only transport identity and owned bytes. Digest,
// media type, size, and lifetime remain exclusively in the generated control.
type ResponseBlob struct {
	BlobID string
	Bytes  []byte
}

// Response owns one complete output publication until Release is called.
type Response struct {
	EndpointGeneration string
	Control            []byte
	Blobs              []ResponseBlob
}

// Release promptly drops all output bytes. It is idempotent.
func (response *Response) Release() {
	if response == nil {
		return
	}
	response.Control = nil
	for index := range response.Blobs {
		response.Blobs[index].Bytes = nil
	}
	response.Blobs = nil
}

// Session binds one endpoint generation to one negotiated context and permits
// at most one synchronous dispatch at a time.
type Session struct {
	mu                 sync.Mutex
	descriptor         *endpoint.Descriptor
	dispatcher         *endpoint.CompileDispatcher
	limits             TransportLimits
	generation         string
	state              sessionState
	inFlight           bool
	inFlightDone       chan struct{}
	handshakeAttempted bool
	negotiated         *endpoint.NegotiatedContext
	activePlan         *endpoint.CompilePlan
}

func NewSession(authority *endpoint.CompilerEndpoint, generation string, limits TransportLimits) (*Session, error) {
	if authority == nil || authority.Descriptor == nil || authority.Dispatcher == nil {
		return nil, fmt.Errorf("nil compiler endpoint authority")
	}
	if !validOpaqueString(generation, 128) {
		return nil, fmt.Errorf("invalid endpoint generation")
	}
	if err := validateTransportLimits(limits); err != nil {
		return nil, err
	}
	return &Session{
		descriptor: authority.Descriptor,
		dispatcher: authority.Dispatcher,
		limits:     limits,
		generation: generation,
	}, nil
}

func validateTransportLimits(limits TransportLimits) error {
	values := []int64{
		limits.MaxControlBytes,
		limits.MaxControlDepth,
		limits.MaxBlobIDBytes,
		limits.MaxBuffers,
		limits.MaxInputBlobBytes,
		limits.MaxInputTotalBytes,
		limits.MaxOutputBlobBytes,
		limits.MaxOutputTotalBytes,
		limits.MaxResponsePublishBytes,
	}
	for _, value := range values {
		if value <= 0 {
			return fmt.Errorf("transport limits must be positive")
		}
	}
	if limits.MaxControlBytes > engineprotocol.MaxWireJSONBytes || limits.MaxControlDepth > engineprotocol.MaxWireJSONDepth {
		return fmt.Errorf("transport control limits exceed generated authority")
	}
	if limits.MaxInputBlobBytes > limits.MaxInputTotalBytes || limits.MaxOutputBlobBytes > limits.MaxOutputTotalBytes {
		return fmt.Errorf("per-blob limit exceeds aggregate limit")
	}
	if limits.MaxOutputTotalBytes > limits.MaxResponsePublishBytes-limits.MaxControlBytes {
		return fmt.Errorf("response publication limit is incomplete")
	}
	return nil
}

// Limits returns the literal profile by value.
func (session *Session) Limits() TransportLimits {
	if session == nil {
		return TransportLimits{}
	}
	return session.limits
}

// Generation returns the opaque transport generation bound at initialization.
func (session *Session) Generation() string {
	if session == nil {
		return ""
	}
	return session.generation
}

// PreflightGeneration lets the outer Worker reject stale or terminal traffic
// before copying transferred control/blob bytes into Go linear memory.
func (session *Session) PreflightGeneration(generation string) *LocalFailure {
	if session == nil {
		return localFailure(FailureDisposed)
	}
	session.mu.Lock()
	defer session.mu.Unlock()
	if generation != session.generation {
		return localFailure(FailureStaleGeneration)
	}
	switch session.state {
	case sessionDisposed:
		return localFailure(FailureDisposed)
	case sessionCrashed:
		return localFailure(FailureCrashed)
	}
	if session.inFlight {
		return localFailure(FailureMalformedMessage)
	}
	return nil
}

// Dispatch performs one generated handshake or #29 PrepareCompile/Execute
// exchange. Every terminal response is encoded by generated Go codecs.
func (session *Session) Dispatch(ctx context.Context, generation string, control []byte, blobs RequestBlobs) (response Response, failure *LocalFailure) {
	if session == nil || ctx == nil || blobs == nil {
		return Response{}, localFailure(FailureMalformedMessage)
	}
	if int64(len(control)) > session.limits.MaxControlBytes {
		blobs.Release()
		return Response{}, localFailure(FailureTransferFailed)
	}
	if beginFailure := session.begin(generation); beginFailure != nil {
		blobs.Release()
		return Response{}, beginFailure
	}
	defer blobs.Release()
	defer func() {
		if recover() != nil {
			session.markCrashed()
			response.Release()
			response = Response{}
			failure = localFailure(FailureCrashed)
		}
		session.end()
	}()

	if request, err := engineprotocol.DecodeHandshakeRequestEnvelope(control); err == nil {
		if blobs.Count() != 0 {
			return Response{}, localFailure(FailureMalformedMessage)
		}
		return session.dispatchHandshake(ctx, generation, request)
	}
	request, err := engineprotocol.DecodeCompileRequestEnvelope(control)
	if err != nil {
		return Response{}, localFailure(FailureMalformedMessage)
	}
	return session.dispatchCompile(ctx, generation, request, blobs)
}

func (session *Session) dispatchHandshake(ctx context.Context, generation string, request engineprotocol.HandshakeRequestEnvelope) (Response, *LocalFailure) {
	session.mu.Lock()
	repeated := session.handshakeAttempted
	session.handshakeAttempted = true
	session.negotiated = nil
	session.mu.Unlock()
	var (
		result     engineprotocol.HandshakeResponseEnvelope
		negotiated *endpoint.NegotiatedContext
		err        error
	)
	if repeated {
		result, err = session.descriptor.RejectHandshakeConnectionState(request.RequestID)
	} else {
		result, negotiated, err = session.descriptor.Negotiate(ctx, request)
	}
	if err != nil {
		return Response{}, localFailure(FailureCrashed)
	}
	control, err := engineprotocol.EncodeHandshakeResponseEnvelope(result)
	if err != nil || int64(len(control)) > session.limits.MaxControlBytes {
		return Response{}, localFailure(FailureCrashed)
	}
	if negotiated != nil {
		session.mu.Lock()
		if session.state == sessionActive {
			session.negotiated = negotiated
		}
		session.mu.Unlock()
		return session.publishableResponse(Response{EndpointGeneration: generation, Control: control})
	}
	// A rejected/cancelled first handshake and every second handshake are the
	// one terminal generated response for this generation. Publication is
	// allowed once, but all later bridge calls observe terminal lifecycle state.
	session.terminateHandshakeGeneration()
	return Response{EndpointGeneration: generation, Control: control}, nil
}

func (session *Session) dispatchCompile(ctx context.Context, generation string, request engineprotocol.CompileRequestEnvelope, blobs RequestBlobs) (Response, *LocalFailure) {
	session.mu.Lock()
	negotiated := session.negotiated
	session.mu.Unlock()
	plan, terminal, err := session.dispatcher.PrepareCompile(ctx, negotiated, request)
	if err != nil {
		return Response{}, localFailure(FailureCrashed)
	}
	if terminal != nil {
		control, encodeErr := engineprotocol.EncodeCompileResponseEnvelope(*terminal)
		if encodeErr != nil || int64(len(control)) > session.limits.MaxControlBytes {
			return Response{}, localFailure(FailureCrashed)
		}
		return session.publishableResponse(Response{EndpointGeneration: generation, Control: control})
	}
	if plan == nil {
		return Response{}, localFailure(FailureCrashed)
	}
	session.setActivePlan(plan)
	defer session.clearActivePlan(plan)
	if bindFailure := blobs.Bind(plan.BlobRequirements(), session.limits); bindFailure != nil {
		plan.Abort()
		return Response{}, bindFailure
	}
	sink := &atomicOutputSink{limits: session.limits}
	result, executeErr := plan.Execute(ctx, blobs, sink)
	if executeErr != nil {
		sink.Release()
		return Response{}, localFailure(FailureCrashed)
	}
	control, encodeErr := engineprotocol.EncodeCompileResponseEnvelope(result)
	if encodeErr != nil || int64(len(control)) > session.limits.MaxControlBytes {
		sink.Release()
		return Response{}, localFailure(FailureCrashed)
	}
	if int64(len(control))+sink.totalBytes > session.limits.MaxResponsePublishBytes {
		sink.Release()
		return Response{}, localFailure(FailureTransferFailed)
	}
	response := Response{EndpointGeneration: generation, Control: control, Blobs: sink.Take()}
	return session.publishableResponse(response)
}

func (session *Session) begin(generation string) *LocalFailure {
	session.mu.Lock()
	defer session.mu.Unlock()
	if generation != session.generation {
		return localFailure(FailureStaleGeneration)
	}
	switch session.state {
	case sessionDisposed:
		return localFailure(FailureDisposed)
	case sessionCrashed:
		return localFailure(FailureCrashed)
	}
	if session.inFlight {
		return localFailure(FailureMalformedMessage)
	}
	session.inFlight = true
	session.inFlightDone = make(chan struct{})
	return nil
}

func (session *Session) end() {
	session.mu.Lock()
	done := session.inFlightDone
	session.inFlightDone = nil
	session.inFlight = false
	session.mu.Unlock()
	if done != nil {
		close(done)
	}
}

func (session *Session) terminateHandshakeGeneration() {
	session.mu.Lock()
	session.state = sessionDisposed
	session.negotiated = nil
	session.mu.Unlock()
}

func (session *Session) setActivePlan(plan *endpoint.CompilePlan) {
	session.mu.Lock()
	if session.state == sessionActive {
		session.activePlan = plan
	} else {
		plan.Abort()
	}
	session.mu.Unlock()
}

func (session *Session) clearActivePlan(plan *endpoint.CompilePlan) {
	session.mu.Lock()
	if session.activePlan == plan {
		session.activePlan = nil
	}
	session.mu.Unlock()
}

func (session *Session) publishableResponse(response Response) (Response, *LocalFailure) {
	session.mu.Lock()
	state := session.state
	session.mu.Unlock()
	if state == sessionActive {
		return response, nil
	}
	response.Release()
	if state == sessionDisposed {
		return Response{}, localFailure(FailureDisposed)
	}
	return Response{}, localFailure(FailureCrashed)
}

func (session *Session) markCrashed() {
	session.mu.Lock()
	session.state = sessionCrashed
	session.negotiated = nil
	plan := session.activePlan
	session.activePlan = nil
	session.mu.Unlock()
	if plan != nil {
		plan.Abort()
	}
}

// Dispose invalidates the generation before aborting any active compile.
// The owning host must terminate the dedicated Worker afterward.
func (session *Session) Dispose(generation string) *LocalFailure {
	if session == nil {
		return localFailure(FailureDisposed)
	}
	session.mu.Lock()
	if generation != session.generation {
		session.mu.Unlock()
		return localFailure(FailureStaleGeneration)
	}
	if session.state == sessionDisposed {
		done := session.inFlightDone
		session.mu.Unlock()
		if done != nil {
			<-done
		}
		return nil
	}
	session.state = sessionDisposed
	session.negotiated = nil
	plan := session.activePlan
	session.activePlan = nil
	done := session.inFlightDone
	session.mu.Unlock()
	if plan != nil {
		plan.Abort()
	}
	if done != nil {
		<-done
	}
	return nil
}

var errOutputLimit = errors.New("atomic output limit exceeded")

type atomicOutputSink struct {
	limits     TransportLimits
	blobs      []ResponseBlob
	totalBytes int64
}

func (sink *atomicOutputSink) Publish(ctx context.Context, input []endpoint.OutputBlob) error {
	if ctx == nil || ctx.Err() != nil {
		return context.Canceled
	}
	if int64(len(input)) > sink.limits.MaxBuffers {
		return errOutputLimit
	}
	ordered := slices.Clone(input)
	sort.SliceStable(ordered, func(left, right int) bool { return ordered[left].Ref.BlobID < ordered[right].Ref.BlobID })
	var total int64
	for index, blob := range ordered {
		if index > 0 && ordered[index-1].Ref.BlobID == blob.Ref.BlobID {
			return errOutputLimit
		}
		size := int64(len(blob.Bytes))
		if size > sink.limits.MaxOutputBlobBytes || total > sink.limits.MaxOutputTotalBytes-size {
			return errOutputLimit
		}
		total += size
	}
	staged := make([]ResponseBlob, len(ordered))
	for index, blob := range ordered {
		if ctx.Err() != nil {
			return context.Canceled
		}
		staged[index] = ResponseBlob{BlobID: blob.Ref.BlobID, Bytes: slices.Clone(blob.Bytes)}
	}
	sink.blobs = staged
	sink.totalBytes = total
	return nil
}

func (sink *atomicOutputSink) Take() []ResponseBlob {
	result := sink.blobs
	sink.blobs = nil
	sink.totalBytes = 0
	return result
}

func (sink *atomicOutputSink) Release() {
	for index := range sink.blobs {
		sink.blobs[index].Bytes = nil
	}
	sink.blobs = nil
	sink.totalBytes = 0
}
