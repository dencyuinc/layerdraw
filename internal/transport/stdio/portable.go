// SPDX-License-Identifier: LicenseRef-LayerDraw-1.0

package stdio

import (
	"context"
	"encoding/json"
	"io"
	"math"
	"strconv"

	"github.com/dencyuinc/layerdraw/internal/engine/endpoint"
)

// PortableEndpoint is the transport-neutral host dispatch seam. Engine and
// Runtime protocol decoding remains in the endpoint; LDSP owns only framing,
// correlation, bounded blobs, deadlines, and cancellation.
type PortableEndpoint interface {
	Handshake(context.Context, []byte) (endpoint.DispatchResponse, bool, error)
	Supports(string) bool
	Prepare(context.Context, string, []byte) (endpoint.DispatchPlan, *endpoint.DispatchResponse, error)
	CancellationResponse(string, string) (endpoint.DispatchResponse, error)
	TransportResponse(string, string) (endpoint.DispatchResponse, error)
}

// ServePortable runs a bounded, single-flight LDSP connection. Single-flight
// is an explicit endpoint limit; request IDs and monotonically increasing
// stream IDs are still correlated independently and process restart mints a
// fresh endpoint generation.
func ServePortable(ctx context.Context, input io.Reader, output io.Writer, portable PortableEndpoint, limits SessionLimits) error {
	if ctx == nil || input == nil || output == nil || portable == nil {
		return &SessionError{Code: SessionErrorConfiguration}
	}
	limits, ok := normalizeSessionLimits(limits)
	if !ok {
		return &SessionError{Code: SessionErrorConfiguration}
	}
	decoder, encoder := NewDecoder(input), NewEncoder(output)
	reads := make(chan readResult, 1)
	readDone := make(chan struct{})
	defer close(readDone)
	go func() {
		for {
			frame, err := decoder.ReadFrame()
			select {
			case reads <- readResult{frame: frame, err: err}:
			case <-readDone:
				return
			}
			if err != nil {
				return
			}
		}
	}()
	var highWater uint64
	negotiated := false
	for {
		var read readResult
		select {
		case read = <-reads:
		case <-ctx.Done():
			return ctx.Err()
		}
		frame, err := read.frame, read.err
		if err == io.EOF {
			return nil
		}
		if err != nil {
			return &SessionError{Code: SessionErrorFraming}
		}
		if frame.Kind == KindClose {
			return nil
		}
		// CANCEL for an unknown or already-terminal stream is a no-op. In
		// particular, a terminal response racing a caller's cancellation must
		// not poison the next request on this connection.
		if frame.Kind == KindCancel {
			continue
		}
		if frame.Kind != KindRequestControl || frame.StreamID <= highWater {
			return &SessionError{Code: SessionErrorFraming}
		}
		highWater = frame.StreamID
		if uint64(len(frame.Payload)) > limits.MaxPendingControlBytes {
			return writePortableStreamError(encoder, frame.StreamID, "control_limit")
		}
		var route operationRoute
		if json.Unmarshal(frame.Payload, &route) != nil || endpoint.ValidateRequestID(route.RequestID) != nil {
			if err := writePortableStreamError(encoder, frame.StreamID, "invalid_control"); err != nil {
				return err
			}
			continue
		}
		requestCtx, cancel, err := endpoint.RequestContextFromControl(ctx, frame.Payload)
		if err != nil {
			if writeErr := writePortableStreamError(encoder, frame.StreamID, "invalid_deadline"); writeErr != nil {
				return writeErr
			}
			continue
		}
		if route.Operation == "runtime.handshake" {
			response, accepted, handshakeErr := portable.Handshake(requestCtx, frame.Payload)
			cancel()
			if handshakeErr != nil || writePortableDispatch(encoder, frame.StreamID, response, nil) != nil {
				return &SessionError{Code: SessionErrorInvariant}
			}
			negotiated = accepted
			continue
		}
		if !negotiated || !portable.Supports(route.Operation) {
			cancel()
			if err := writePortableStreamError(encoder, frame.StreamID, "unknown_operation"); err != nil {
				return err
			}
			continue
		}
		plan, terminal, prepareErr := portable.Prepare(requestCtx, route.Operation, frame.Payload)
		if prepareErr != nil {
			cancel()
			if err := writePortableStreamError(encoder, frame.StreamID, "endpoint_invariant"); err != nil {
				return err
			}
			continue
		}
		if terminal != nil {
			cancel()
			if err := writePortableDispatch(encoder, frame.StreamID, *terminal, nil); err != nil {
				return err
			}
			continue
		}
		response, blobs, executeErr := executePortable(requestCtx, cancel, reads, encoder, frame.StreamID, route, plan, limits, portable)
		if executeErr != nil {
			return executeErr
		}
		if err := writePortableDispatch(encoder, frame.StreamID, response, blobs); err != nil {
			return err
		}
	}
}

type portableResult struct {
	response endpoint.DispatchResponse
	blobs    []endpoint.OutputBlob
	err      error
}

func executePortable(ctx context.Context, cancel context.CancelFunc, reads <-chan readResult, encoder *Encoder, streamID uint64, route operationRoute, plan endpoint.DispatchPlan, limits SessionLimits, portable PortableEndpoint) (endpoint.DispatchResponse, []endpoint.OutputBlob, error) {
	defer cancel()
	requirements := plan.BlobRequirements()
	budget := plan.AdmissionBudget()
	if budget.RequiredBlobBytes < 0 || budget.RequiredBlobCount != int64(len(requirements)) || uint64(budget.RequiredBlobBytes) > limits.MaxReservedBlobBytes || len(requirements) > limits.MaxUploadBlobCount {
		plan.Abort()
		response, err := portable.TransportResponse(route.Operation, route.RequestID)
		return response, nil, err
	}
	if err := encoder.WriteFrame(Frame{Kind: KindRequestReady, StreamID: streamID}); err != nil {
		plan.Abort()
		return endpoint.DispatchResponse{}, nil, &SessionError{Code: SessionErrorOutput}
	}
	expected := make(map[string]uint64, len(requirements))
	collected := make(map[string][]byte, len(requirements))
	for _, requirement := range requirements {
		size, err := strconv.ParseUint(string(requirement.Ref.Size), 10, 64)
		if err != nil || size > uint64(math.MaxInt) {
			plan.Abort()
			return endpoint.DispatchResponse{}, nil, &SessionError{Code: SessionErrorInvariant}
		}
		expected[requirement.Ref.BlobID] = size
	}
	validator := NewBundleValidator()
	for {
		var read readResult
		select {
		case read = <-reads:
		case <-ctx.Done():
			plan.Abort()
			response, responseErr := portable.CancellationResponse(route.Operation, route.RequestID)
			return response, nil, responseErr
		}
		frame, err := read.frame, read.err
		if err != nil {
			plan.Abort()
			return endpoint.DispatchResponse{}, nil, &SessionError{Code: SessionErrorFraming}
		}
		if frame.Kind == KindCancel && frame.StreamID == streamID {
			plan.Abort()
			response, responseErr := portable.CancellationResponse(route.Operation, route.RequestID)
			return response, nil, responseErr
		}
		if frame.StreamID != streamID || (frame.Kind != KindBlobChunk && frame.Kind != KindBundleEnd) || validator.Accept(frame) != nil {
			plan.Abort()
			response, responseErr := portable.TransportResponse(route.Operation, route.RequestID)
			return response, nil, responseErr
		}
		if frame.Kind == KindBundleEnd {
			break
		}
		name := string(frame.Name)
		size, ok := expected[name]
		current := collected[name]
		payloadBytes := uint64(len(frame.Payload))
		if !ok || payloadBytes > size || uint64(len(current)) > size-payloadBytes {
			plan.Abort()
			response, responseErr := portable.TransportResponse(route.Operation, route.RequestID)
			return response, nil, responseErr
		}
		collected[name] = append(current, frame.Payload...)
	}
	for name, size := range expected {
		if uint64(len(collected[name])) != size {
			plan.Abort()
			response, err := portable.TransportResponse(route.Operation, route.RequestID)
			return response, nil, err
		}
	}
	result := make(chan portableResult, 1)
	go func() {
		stream := &requestStream{blobs: collected, requirements: requirements}
		sink := &captureSink{ctx: ctx, maximum: limits.MaxOutputBlobBytes, commit: func() bool { return true }}
		response, err := plan.ExecuteDispatch(ctx, collectedSource{stream: stream}, sink)
		result <- portableResult{response: response, blobs: sink.take(), err: err}
	}()
	select {
	case value := <-result:
		if value.err != nil {
			return endpoint.DispatchResponse{}, nil, &SessionError{Code: SessionErrorInvariant}
		}
		return value.response, value.blobs, nil
	case read := <-reads:
		if read.err != nil || read.frame.Kind != KindCancel || read.frame.StreamID != streamID {
			plan.Abort()
			return endpoint.DispatchResponse{}, nil, &SessionError{Code: SessionErrorFraming}
		}
		cancel()
		plan.Abort()
		response, err := portable.CancellationResponse(route.Operation, route.RequestID)
		return response, nil, err
	case <-ctx.Done():
		plan.Abort()
		response, err := portable.CancellationResponse(route.Operation, route.RequestID)
		return response, nil, err
	}
}

func writePortableDispatch(encoder *Encoder, streamID uint64, response endpoint.DispatchResponse, blobs []endpoint.OutputBlob) error {
	frames, err := responseBundle(streamID, response.Control, blobs)
	if err != nil {
		return &SessionError{Code: SessionErrorInvariant}
	}
	if err := encoder.WriteFrames(frames); err != nil {
		return &SessionError{Code: SessionErrorOutput}
	}
	return nil
}

func writePortableStreamError(encoder *Encoder, streamID uint64, code string) error {
	if err := encoder.WriteFrames([]Frame{{Kind: KindStreamError, StreamID: streamID, Name: []byte(code)}, {Kind: KindBundleEnd, StreamID: streamID, Sequence: 1}}); err != nil {
		return &SessionError{Code: SessionErrorOutput}
	}
	return nil
}
