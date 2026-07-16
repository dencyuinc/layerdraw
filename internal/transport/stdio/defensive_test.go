// SPDX-License-Identifier: LicenseRef-LayerDraw-1.0

package stdio

import (
	"bytes"
	"context"
	"errors"
	"io"
	"strings"
	"testing"

	"github.com/dencyuinc/layerdraw/gen/go/engineprotocol"
	"github.com/dencyuinc/layerdraw/gen/go/protocolcommon"
	"github.com/dencyuinc/layerdraw/internal/engine/endpoint"
)

func dispatchResponseForTest(requestID string) endpoint.DispatchResponse {
	return endpoint.DispatchResponse{Operation: endpoint.OperationCompile, RequestID: requestID, Control: []byte(`{"request_id":"` + requestID + `"}`)}
}

func TestTerminalArbiterClaimsAreImmutable(t *testing.T) {
	t.Parallel()
	var nilArbiter *terminalArbiter
	if nilArbiter.claimExecutionPublication() ||
		nilArbiter.claimExecution(endpoint.DispatchResponse{}) ||
		nilArbiter.claimCancellation(endpoint.DispatchResponse{}) ||
		nilArbiter.claimTransport(endpoint.DispatchResponse{}) {
		t.Fatal("nil arbiter accepted a terminal claim")
	}
	if kind, _, set := nilArbiter.snapshot(); kind != terminalUndecided || set {
		t.Fatalf("nil snapshot = kind %d, set %t", kind, set)
	}

	execution := &terminalArbiter{}
	first := dispatchResponseForTest("first")
	second := dispatchResponseForTest("second")
	if !execution.claimExecutionPublication() ||
		execution.claimCancellation(second) || execution.claimTransport(second) ||
		!execution.claimExecution(first) || !execution.claimExecution(second) {
		t.Fatal("execution claim did not remain authoritative")
	}
	if kind, response, set := execution.snapshot(); kind != terminalExecution || !set || response.RequestID != first.RequestID {
		t.Fatalf("execution snapshot = kind %d, response %q, set %t", kind, response.RequestID, set)
	}

	cancellation := &terminalArbiter{}
	if !cancellation.claimCancellation(first) || cancellation.claimExecution(second) || cancellation.claimTransport(second) {
		t.Fatal("cancellation claim did not remain authoritative")
	}
	transport := &terminalArbiter{}
	if !transport.claimTransport(first) || transport.claimCancellation(second) || transport.claimExecutionPublication() {
		t.Fatal("transport claim did not remain authoritative")
	}
}

func TestServeRejectsIncompleteConfigurationAndLimits(t *testing.T) {
	t.Parallel()
	config := sessionConfig(t, SessionLimits{})
	tests := []struct {
		name   string
		ctx    context.Context
		input  io.Reader
		output io.Writer
		config SessionConfig
	}{
		{name: "nil context", input: bytes.NewReader(nil), output: io.Discard, config: config},
		{name: "nil input", ctx: context.Background(), output: io.Discard, config: config},
		{name: "nil output", ctx: context.Background(), input: bytes.NewReader(nil), config: config},
		{name: "nil descriptor", ctx: context.Background(), input: bytes.NewReader(nil), output: io.Discard, config: SessionConfig{Dispatcher: config.Dispatcher}},
		{name: "nil dispatcher", ctx: context.Background(), input: bytes.NewReader(nil), output: io.Discard, config: SessionConfig{Descriptor: config.Descriptor}},
	}
	for _, test := range tests {
		test := test
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			requireSessionError(t, Serve(test.ctx, test.input, test.output, test.config), SessionErrorConfiguration)
		})
	}

	invalid := DefaultSessionLimits()
	invalid.MaxConcurrentDispatch = invalid.MaxActiveStreams + 1
	config.Limits = invalid
	requireSessionError(t, Serve(context.Background(), bytes.NewReader(nil), io.Discard, config), SessionErrorConfiguration)
}

func TestFramingDefensiveBranches(t *testing.T) {
	t.Parallel()
	corruptPlan := ChunkPlan{Size: MaxChunkPayload + 1, FirstSequence: ^uint32(0), ChunkCount: 2}
	if _, err := corruptPlan.Chunk(0); err == nil {
		t.Fatal("overflowing derived chunk plan was accepted")
	}
	if err := NewBundleValidator().Accept(Frame{Kind: KindBlobChunk, Flags: FlagFinal, StreamID: 0, Sequence: 1, Name: []byte("x"), Payload: []byte("x")}); err == nil {
		t.Fatal("validator accepted an invalid frame")
	}
	if err := ValidateFrame(Frame{Kind: KindBlobChunk, Flags: FlagFinal, StreamID: 1, Sequence: 1, Name: []byte("x"), Payload: []byte("x"), Offset: ^uint64(0)}); err == nil {
		t.Fatal("frame offset overflow was accepted")
	}
	for _, frame := range []Frame{
		{Kind: KindBundleEnd, StreamID: 1, Sequence: 1, Name: []byte("x")},
		{Kind: KindBundleEnd, StreamID: 1, Sequence: 1, Offset: 1},
	} {
		if err := ValidateFrame(frame); err == nil {
			t.Fatalf("invalid bundle end was accepted: %#v", frame)
		}
	}
	if _, err := UnmarshalFrame([]byte{0}); err == nil {
		t.Fatal("truncated frame was accepted")
	}
	var nilEncoder *Encoder
	if err := nilEncoder.WriteFrames(nil); err == nil {
		t.Fatal("nil encoder accepted a frame bundle")
	}
	if _, err := MarshalFrame(Frame{Kind: KindClose, StreamID: 1}); err == nil {
		t.Fatal("marshal accepted an invalid frame")
	}
}

func TestSessionRejectsUnownedAndReplayedFrames(t *testing.T) {
	t.Parallel()
	s := newDefensiveSession(t, io.Discard)
	requireSessionError(t, s.accept(Frame{Kind: Kind(255)}), SessionErrorFraming)
	if err := s.accept(Frame{Kind: Kind(255), StreamID: 4}); err != nil {
		t.Fatal(err)
	}
	if err := s.accept(Frame{Kind: Kind(255), StreamID: 3}); err != nil {
		t.Fatal(err)
	}
	s.streams[4] = &requestStream{id: 4, phase: phaseTerminal}
	if err := s.accept(Frame{Kind: Kind(255), StreamID: 4}); err != nil {
		t.Fatal(err)
	}

	if err := s.acceptUnexpectedDirection(5); err != nil {
		t.Fatal(err)
	}
	requireSessionError(t, s.acceptUnexpectedDirection(5), SessionErrorFraming)
	s.streams[6] = &requestStream{id: 6, phase: phaseTerminal}
	s.highWater = 6
	requireSessionError(t, s.acceptUnexpectedDirection(6), SessionErrorFraming)

	requireSessionError(t, s.recoverableFrameError(Frame{}), SessionErrorFraming)
	if err := s.recoverableFrameError(Frame{Kind: KindRequestControl, StreamID: 7}); err != nil {
		t.Fatal(err)
	}
	requireSessionError(t, s.recoverableFrameError(Frame{Kind: KindRequestControl, StreamID: 7}), SessionErrorFraming)
	if err := s.recoverableFrameError(Frame{Kind: Kind(255), StreamID: 6}); err != nil {
		t.Fatal(err)
	}
	if err := s.recoverableFrameError(Frame{Kind: Kind(255), StreamID: 8}); err != nil {
		t.Fatal(err)
	}
}

func TestSessionDefensiveHelpers(t *testing.T) {
	t.Parallel()
	tooLong := strings.Repeat("x", int(MaxNameBytes)+1)
	if _, ok := uploadMetadataBytes([]endpoint.BlobRequirement{{Ref: protocolcommon.BlobRef{BlobID: tooLong}}}, ^uint64(0)); ok {
		t.Fatal("oversized upload metadata name was accepted")
	}
	if _, ok := uploadMetadataBytes([]endpoint.BlobRequirement{{Ref: protocolcommon.BlobRef{BlobID: "ab"}}}, 1); ok {
		t.Fatal("upload metadata budget overflow was accepted")
	}

	invalidAdmission := newDefensiveSession(t, io.Discard)
	invalidAdmission.queue = []*requestStream{{
		id: 1, reserved: 1,
		requirements: []endpoint.BlobRequirement{{Ref: protocolcommon.BlobRef{BlobID: "x", Size: "invalid"}}},
	}}
	requireSessionError(t, invalidAdmission.admit(), SessionErrorInvariant)

	blockedAdmission := newDefensiveSession(t, io.Discard)
	blockedAdmission.reserved = ^uint64(0)
	blockedAdmission.limits.MaxReservedBlobBytes = ^uint64(0)
	blockedAdmission.queue = []*requestStream{{id: 2, reserved: 1}}
	if err := blockedAdmission.admit(); err != nil || len(blockedAdmission.queue) != 1 {
		t.Fatalf("overflowing reservation changed admission: queue=%d err=%v", len(blockedAdmission.queue), err)
	}

	cleanup := newDefensiveSession(t, io.Discard)
	cleanup.releaseStream(nil)
	_, cancel := context.WithCancel(context.Background())
	stream := &requestStream{id: 3, requestID: "cleanup", controlBytes: 2, cancel: cancel, blobs: map[string][]byte{"x": {1}}}
	cleanup.streams[stream.id] = stream
	cleanup.requestIDs[stream.requestID] = stream
	cleanup.releaseStream(stream)
	if len(cleanup.streams) != 0 || len(cleanup.requestIDs) != 0 || stream.blobs != nil || stream.controlBytes != 0 {
		t.Fatal("stream state was retained after release")
	}

	aborted := newDefensiveSession(t, io.Discard)
	terminal := &requestStream{id: 4, phase: phaseTerminal}
	uploading := &requestStream{id: 5, phase: phaseUploading, reserved: 1}
	aborted.streams[4], aborted.streams[5] = terminal, uploading
	aborted.active, aborted.reserved, aborted.uploader = 1, 1, uploading
	aborted.abortAll()
	if !aborted.draining || aborted.active != 0 || aborted.reserved != 0 || aborted.uploader != nil || uploading.phase != phaseTerminal {
		t.Fatalf("abort did not clear upload state: %+v", aborted)
	}

	root, cancelRoot := context.WithCancel(context.Background())
	cancelRoot()
	draining := newDefensiveSession(t, io.Discard)
	draining.root = root
	if err := draining.drain(false); !errors.Is(err, context.Canceled) {
		t.Fatalf("drain error = %v, want context cancellation", err)
	}
}

func TestSessionOutputAndCaptureFailures(t *testing.T) {
	t.Parallel()
	s := newDefensiveSession(t, io.Discard)
	requireSessionError(t, s.writeStreamError(1, ""), SessionErrorInvariant)
	requireSessionError(t, s.writeStreamError(1, strings.Repeat("x", int(MaxStreamErrorBytes)+1)), SessionErrorInvariant)
	s.encode = NewEncoder(zeroWriter{})
	requireSessionError(t, s.writeStreamError(1, "failure"), SessionErrorOutput)

	s.encode = NewEncoder(io.Discard)
	requireSessionError(t, s.writeCompile(1, engineprotocol.CompileResponseEnvelope{}, nil), SessionErrorInvariant)
	response, err := s.config.Dispatcher.CompileTransportResponse("output", endpoint.CompileTransportResourceLimit)
	if err != nil {
		t.Fatal(err)
	}
	duplicates := []endpoint.OutputBlob{{Ref: protocolcommon.BlobRef{BlobID: "same"}}, {Ref: protocolcommon.BlobRef{BlobID: "same"}}}
	requireSessionError(t, s.writeCompile(1, response, duplicates), SessionErrorInvariant)
	s.encode = NewEncoder(zeroWriter{})
	requireSessionError(t, s.writeCompile(1, response, nil), SessionErrorOutput)

	definitions, err := (collectedSource{}).Definitions(context.Background())
	if err != nil || len(definitions) != 0 {
		t.Fatalf("nil collected source = %#v, %v", definitions, err)
	}
	definitions, err = (collectedSource{stream: &requestStream{
		requirements: []endpoint.BlobRequirement{{Ref: protocolcommon.BlobRef{BlobID: "missing"}}},
		blobs:        map[string][]byte{},
	}}).Definitions(context.Background())
	if err != nil || len(definitions) != 0 {
		t.Fatalf("missing collected source = %#v, %v", definitions, err)
	}

	var nilSink *captureSink
	if err := nilSink.Publish(context.Background(), nil); err == nil {
		t.Fatal("nil capture sink accepted output")
	}
	if err := (&captureSink{}).Publish(context.Background(), nil); err == nil {
		t.Fatal("capture sink without context accepted output")
	}
	cancelled, cancel := context.WithCancel(context.Background())
	cancel()
	if err := (&captureSink{ctx: cancelled}).Publish(cancelled, nil); !errors.Is(err, context.Canceled) {
		t.Fatalf("cancelled publish error = %v", err)
	}
	if err := (&captureSink{ctx: context.Background(), maximum: 1}).Publish(context.Background(), []endpoint.OutputBlob{{Bytes: []byte("xx")}}); err == nil {
		t.Fatal("capture sink exceeded output budget")
	}

	commitContext, cancelCommit := context.WithCancel(context.Background())
	cancelAtCommit := &captureSink{ctx: commitContext, maximum: 4, beforeCommit: cancelCommit}
	if err := cancelAtCommit.Publish(commitContext, []endpoint.OutputBlob{{Bytes: []byte("x")}}); !errors.Is(err, context.Canceled) {
		t.Fatalf("commit cancellation error = %v", err)
	}
	rejected := &captureSink{ctx: context.Background(), maximum: 4, commit: func() bool { return false }}
	if err := rejected.Publish(context.Background(), []endpoint.OutputBlob{{Bytes: []byte("x")}}); !errors.Is(err, context.Canceled) {
		t.Fatalf("rejected commit error = %v", err)
	}

	original := []byte("ok")
	accepted := &captureSink{ctx: context.Background(), maximum: 4, commit: func() bool { return true }}
	if err := accepted.Publish(context.Background(), []endpoint.OutputBlob{{Bytes: original}}); err != nil {
		t.Fatal(err)
	}
	original[0] = 'x'
	if got := accepted.take(); len(got) != 1 || string(got[0].Bytes) != "ok" || accepted.take() != nil {
		t.Fatalf("captured output = %#v", got)
	}
	if nilSink.take() != nil {
		t.Fatal("nil sink returned output")
	}
}

func newDefensiveSession(t *testing.T, output io.Writer) *session {
	t.Helper()
	limits := DefaultSessionLimits()
	return &session{
		config:     sessionConfig(t, limits),
		limits:     limits,
		root:       context.Background(),
		encode:     NewEncoder(output),
		streams:    make(map[uint64]*requestStream),
		requestIDs: make(map[string]*requestStream),
		results:    make(chan dispatchResult, limits.MaxConcurrentDispatch),
		deadlines:  make(chan uint64, limits.MaxActiveStreams),
	}
}

func requireSessionError(t *testing.T, err error, code string) {
	t.Helper()
	var sessionError *SessionError
	if !errors.As(err, &sessionError) || sessionError.Code != code {
		t.Fatalf("session error = %v, want %q", err, code)
	}
}
