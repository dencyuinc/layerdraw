// SPDX-License-Identifier: LicenseRef-LayerDraw-1.0

package stdio

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/dencyuinc/layerdraw/gen/go/engineprotocol"
	"github.com/dencyuinc/layerdraw/gen/go/protocolcommon"
	"github.com/dencyuinc/layerdraw/internal/engine"
	"github.com/dencyuinc/layerdraw/internal/engine/endpoint"
)

const sessionTestDigest = "sha256:5555555555555555555555555555555555555555555555555555555555555555"

func TestSessionHandshakeCompileAndOrderlyClose(t *testing.T) {
	t.Parallel()
	source := []byte("project p \"Project\" {}\n")
	handshake := sessionHandshake("handshake-1")
	compile := sessionCompile("compile-1", "source", source)

	input := marshalFrames(t,
		controlFrame(t, 1, handshake),
		controlFrame(t, 2, compile),
		Frame{Kind: KindBlobChunk, Flags: FlagFinal, StreamID: 2, Sequence: 1, Name: []byte("source"), Payload: source},
		Frame{Kind: KindBundleEnd, StreamID: 2, Sequence: 2},
		Frame{Kind: KindClose},
	)
	var output bytes.Buffer
	if err := Serve(context.Background(), bytes.NewReader(input), &output, sessionConfig(t, SessionLimits{})); err != nil {
		t.Fatal(err)
	}
	frames := decodeFrames(t, output.Bytes())
	if len(frames) < 7 {
		t.Fatalf("response frames = %d, want handshake bundle, READY, and compile bundle", len(frames))
	}
	if frames[0].Kind != KindResponseControl || frames[0].StreamID != 1 || frames[1].Kind != KindBundleEnd {
		t.Fatalf("handshake bundle = %#v %#v", frames[0], frames[1])
	}
	handshakeResponse, err := engineprotocol.DecodeHandshakeResponseEnvelope(frames[0].Payload)
	if err != nil || handshakeResponse.Outcome != protocolcommon.OutcomeSuccess {
		t.Fatalf("handshake response = %+v, %v", handshakeResponse, err)
	}
	if frames[2].Kind != KindRequestReady || frames[2].StreamID != 2 {
		t.Fatalf("credit = %#v", frames[2])
	}
	if frames[3].Kind != KindResponseControl || frames[3].StreamID != 2 {
		t.Fatalf("compile control = %#v", frames[3])
	}
	compileResponse, err := engineprotocol.DecodeCompileResponseEnvelope(frames[3].Payload)
	if err != nil || compileResponse.Outcome != protocolcommon.OutcomeSuccess || compileResponse.RequestID != "compile-1" {
		t.Fatalf("compile response = %+v, %v", compileResponse, err)
	}
	validator := NewBundleValidator()
	for _, frame := range frames[4:] {
		if err := validator.Accept(frame); err != nil {
			t.Fatalf("response bundle: %v, frame=%#v", err, frame)
		}
	}
}

func TestSessionCompileBeforeHandshakeUsesGeneratedFailure(t *testing.T) {
	t.Parallel()
	request := sessionCompile("compile-unnegotiated", "source", []byte("x"))
	input := marshalFrames(t, controlFrame(t, 9, request), Frame{Kind: KindClose})
	var output bytes.Buffer
	if err := Serve(context.Background(), bytes.NewReader(input), &output, sessionConfig(t, SessionLimits{})); err != nil {
		t.Fatal(err)
	}
	frames := decodeFrames(t, output.Bytes())
	if len(frames) != 2 || frames[0].Kind != KindResponseControl || frames[1].Kind != KindBundleEnd {
		t.Fatalf("frames = %#v", frames)
	}
	response, err := engineprotocol.DecodeCompileResponseEnvelope(frames[0].Payload)
	if err != nil {
		t.Fatal(err)
	}
	if response.Outcome != protocolcommon.OutcomeFailed || response.Failure == nil || response.Failure.Code != endpoint.FailureCompileUnnegotiated {
		t.Fatalf("response = %+v", response)
	}
}

func TestSessionWorkbenchOpenDocumentUsesGeneratedOperationEnvelope(t *testing.T) {
	t.Parallel()
	source := []byte("project p \"Project\" {}\n")
	request := sessionOpenDocument("open-1", "source", source)
	input := marshalFrames(t,
		controlFrame(t, 1, sessionHandshake("hs")),
		controlFrame(t, 2, request),
		Frame{Kind: KindBlobChunk, Flags: FlagFinal, StreamID: 2, Sequence: 1, Name: []byte("source"), Payload: source},
		Frame{Kind: KindBundleEnd, StreamID: 2, Sequence: 2},
		Frame{Kind: KindClose},
	)
	var output bytes.Buffer
	if err := Serve(context.Background(), bytes.NewReader(input), &output, sessionConfig(t, SessionLimits{})); err != nil {
		t.Fatal(err)
	}
	frames := decodeFrames(t, output.Bytes())
	ready, terminal := false, false
	for _, frame := range frames {
		if frame.StreamID != 2 {
			continue
		}
		switch frame.Kind {
		case KindRequestReady:
			ready = true
		case KindResponseControl:
			terminal = true
			response, err := engineprotocol.DecodeOpenDocumentResponseEnvelope(frame.Payload)
			if err != nil {
				t.Fatal(err)
			}
			if response.RequestID != "open-1" || response.Outcome != protocolcommon.OutcomeSuccess || response.Payload == nil {
				t.Fatalf("open response = %+v", response)
			}
			if response.Payload.DocumentHandle.Value == "" || response.Payload.DocumentGeneration.Value == "" {
				t.Fatalf("open payload omitted document identity: %+v", response.Payload)
			}
		}
	}
	if !ready || !terminal {
		t.Fatalf("open_document did not complete through upload/response frames: %#v", frames)
	}
}

func TestSessionSecondHandshakeEndsGeneration(t *testing.T) {
	t.Parallel()
	source := []byte("project p \"Project\" {}\n")
	input := marshalFrames(t,
		controlFrame(t, 1, sessionHandshake("first")),
		controlFrame(t, 2, sessionHandshake("second")),
		controlFrame(t, 3, sessionCompile("must-not-run", "source", source)),
	)
	var output bytes.Buffer
	if err := Serve(context.Background(), bytes.NewReader(input), &output, sessionConfig(t, SessionLimits{})); err != nil {
		t.Fatal(err)
	}
	responses := 0
	for _, frame := range decodeFrames(t, output.Bytes()) {
		if frame.StreamID == 3 {
			t.Fatalf("post-handshake-generation frame = %#v", frame)
		}
		if frame.Kind == KindResponseControl {
			response, err := engineprotocol.DecodeHandshakeResponseEnvelope(frame.Payload)
			if err != nil {
				t.Fatal(err)
			}
			responses++
			if frame.StreamID == 2 && response.Outcome != protocolcommon.OutcomeRejected {
				t.Fatalf("second handshake = %+v", response)
			}
		}
	}
	if responses != 2 {
		t.Fatalf("handshake responses = %d", responses)
	}
}

func TestSessionRejectsUntrustworthyRequestIDWithoutPoisoning(t *testing.T) {
	t.Parallel()
	overlong := strings.Repeat("x", endpoint.MaxRequestIDCodePoints+1)
	payload := []byte(`{"operation":"engine.handshake","request_id":"` + overlong + `"}`)
	input := marshalFrames(t,
		Frame{Kind: KindRequestControl, StreamID: 1, Payload: payload},
		controlFrame(t, 2, sessionHandshake("valid")),
		Frame{Kind: KindClose},
	)
	var output bytes.Buffer
	if err := Serve(context.Background(), bytes.NewReader(input), &output, sessionConfig(t, SessionLimits{})); err != nil {
		t.Fatal(err)
	}
	frames := decodeFrames(t, output.Bytes())
	if len(frames) != 4 || frames[0].Kind != KindStreamError || string(frames[0].Name) != "invalid_request_id" || frames[2].Kind != KindResponseControl {
		t.Fatalf("frames = %#v", frames)
	}
}

func TestSessionPackSuccessAndDeterministicInputOrdering(t *testing.T) {
	t.Parallel()
	request, packSource, manifest := sessionPackCompile(t, "pack-compile")
	input := marshalFrames(t,
		controlFrame(t, 1, sessionHandshake("pack-hs")),
		controlFrame(t, 2, request),
		Frame{Kind: KindBlobChunk, Flags: FlagFinal, StreamID: 2, Sequence: 1, Name: []byte("pack-file"), Payload: packSource},
		Frame{Kind: KindBlobChunk, Flags: FlagFinal, StreamID: 2, Sequence: 2, Name: []byte("pack-manifest"), Payload: manifest},
		Frame{Kind: KindBundleEnd, StreamID: 2, Sequence: 3},
		Frame{Kind: KindClose},
	)
	var output bytes.Buffer
	if err := Serve(context.Background(), bytes.NewReader(input), &output, sessionConfig(t, SessionLimits{})); err != nil {
		t.Fatal(err)
	}
	frames := decodeFrames(t, output.Bytes())
	var response engineprotocol.CompileResponseEnvelope
	for _, frame := range frames {
		if frame.Kind == KindResponseControl && frame.StreamID == 2 {
			var err error
			response, err = engineprotocol.DecodeCompileResponseEnvelope(frame.Payload)
			if err != nil {
				t.Fatal(err)
			}
		}
	}
	if response.Outcome != protocolcommon.OutcomeSuccess || response.Payload == nil || response.Payload.NormalizedArtifact.Pack == nil || response.Payload.NormalizedArtifact.Project != nil {
		t.Fatalf("pack response = %+v", response)
	}
}

func TestSessionDrainableBadControlDoesNotDesynchronizeHandshake(t *testing.T) {
	t.Parallel()
	input := marshalFrames(t,
		Frame{Kind: KindRequestControl, StreamID: 3, Payload: []byte(`{"operation":"unknown","request_id":"x"}`)},
		controlFrame(t, 4, sessionHandshake("handshake-after-error")),
		Frame{Kind: KindClose},
	)
	var output bytes.Buffer
	if err := Serve(context.Background(), bytes.NewReader(input), &output, sessionConfig(t, SessionLimits{})); err != nil {
		t.Fatal(err)
	}
	frames := decodeFrames(t, output.Bytes())
	if len(frames) != 4 || frames[0].Kind != KindStreamError || string(frames[0].Name) != "unknown_operation" || frames[2].Kind != KindResponseControl {
		t.Fatalf("frames = %#v", frames)
	}
}

func TestSessionQueuedCancellationAndSingleCredit(t *testing.T) {
	t.Parallel()
	source := []byte("project p \"Project\" {}\n")
	limits := DefaultSessionLimits()
	limits.MaxConcurrentDispatch = 1
	input := marshalFrames(t,
		controlFrame(t, 1, sessionHandshake("hs")),
		controlFrame(t, 10, sessionCompile("first", "a", source)),
		controlFrame(t, 11, sessionCompile("second", "b", source)),
		Frame{Kind: KindCancel, StreamID: 11},
		Frame{Kind: KindBlobChunk, Flags: FlagFinal, StreamID: 10, Sequence: 1, Name: []byte("a"), Payload: source},
		Frame{Kind: KindBundleEnd, StreamID: 10, Sequence: 2},
		Frame{Kind: KindClose},
	)
	var output bytes.Buffer
	if err := Serve(context.Background(), bytes.NewReader(input), &output, sessionConfig(t, limits)); err != nil {
		t.Fatal(err)
	}
	frames := decodeFrames(t, output.Bytes())
	ready := 0
	responses := map[uint64]protocolcommon.Outcome{}
	for _, frame := range frames {
		if frame.Kind == KindRequestReady {
			ready++
			if frame.StreamID != 10 {
				t.Fatalf("unexpected credit = %#v", frame)
			}
		}
		if frame.Kind == KindResponseControl && frame.StreamID != 1 {
			response, err := engineprotocol.DecodeCompileResponseEnvelope(frame.Payload)
			if err != nil {
				t.Fatal(err)
			}
			responses[frame.StreamID] = response.Outcome
		}
	}
	if ready != 1 || responses[10] != protocolcommon.OutcomeSuccess || responses[11] != protocolcommon.OutcomeCancelled {
		t.Fatalf("ready=%d responses=%v", ready, responses)
	}
}

func TestSessionBlobFailuresPreserveLaterRequest(t *testing.T) {
	t.Parallel()
	validSource := []byte("project p \"Project\" {}\n")
	for _, test := range []struct {
		name     string
		bad      engineprotocol.CompileRequestEnvelope
		frames   []Frame
		wantCode string
	}{
		{
			name: "missing", bad: sessionCompile("bad", "bad-source", validSource),
			frames:   []Frame{{Kind: KindBundleEnd, StreamID: 2, Sequence: 1}},
			wantCode: endpoint.FailureCompileMissingBlob,
		},
		{
			name: "digest", bad: sessionCompile("bad", "bad-source", validSource),
			frames: []Frame{
				{Kind: KindBlobChunk, Flags: FlagFinal, StreamID: 2, Sequence: 1, Name: []byte("bad-source"), Payload: bytes.Repeat([]byte{'x'}, len(validSource))},
				{Kind: KindBundleEnd, StreamID: 2, Sequence: 2},
			},
			wantCode: endpoint.FailureCompileBlobDigestMismatch,
		},
		{
			name: "unreferenced", bad: sessionCompile("bad", "bad-source", validSource),
			frames: []Frame{
				{Kind: KindBlobChunk, Flags: FlagFinal, StreamID: 2, Sequence: 1, Name: []byte("aaa-unexpected"), Payload: validSource},
				{Kind: KindBundleEnd, StreamID: 2, Sequence: 2},
			},
			wantCode: endpoint.FailureCompileTransportProtocol,
		},
	} {
		test := test
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			inputFrames := []Frame{controlFrame(t, 1, sessionHandshake("hs")), controlFrame(t, 2, test.bad)}
			inputFrames = append(inputFrames, test.frames...)
			inputFrames = append(inputFrames,
				controlFrame(t, 3, sessionCompile("good", "good-source", validSource)),
				Frame{Kind: KindBlobChunk, Flags: FlagFinal, StreamID: 3, Sequence: 1, Name: []byte("good-source"), Payload: validSource},
				Frame{Kind: KindBundleEnd, StreamID: 3, Sequence: 2},
				Frame{Kind: KindClose},
			)
			var output bytes.Buffer
			if err := Serve(context.Background(), bytes.NewReader(marshalFrames(t, inputFrames...)), &output, sessionConfig(t, SessionLimits{})); err != nil {
				t.Fatal(err)
			}
			responses := map[uint64]engineprotocol.CompileResponseEnvelope{}
			for _, frame := range decodeFrames(t, output.Bytes()) {
				if frame.Kind == KindResponseControl && frame.StreamID != 1 {
					response, err := engineprotocol.DecodeCompileResponseEnvelope(frame.Payload)
					if err != nil {
						t.Fatal(err)
					}
					responses[frame.StreamID] = response
				}
			}
			if failure := responses[2].Failure; failure == nil || failure.Code != test.wantCode {
				t.Fatalf("bad response = %+v", responses[2])
			}
			if responses[3].Outcome != protocolcommon.OutcomeSuccess {
				t.Fatalf("later response = %+v", responses[3])
			}
		})
	}
}

func TestSessionDuplicateInflightRequestLeavesFirstUntouched(t *testing.T) {
	t.Parallel()
	source := []byte("project p \"Project\" {}\n")
	input := marshalFrames(t,
		controlFrame(t, 1, sessionHandshake("hs")),
		controlFrame(t, 2, sessionCompile("same", "a", source)),
		controlFrame(t, 3, sessionCompile("same", "b", source)),
		Frame{Kind: KindBlobChunk, Flags: FlagFinal, StreamID: 2, Sequence: 1, Name: []byte("a"), Payload: source},
		Frame{Kind: KindBundleEnd, StreamID: 2, Sequence: 2},
		Frame{Kind: KindClose},
	)
	var output bytes.Buffer
	if err := Serve(context.Background(), bytes.NewReader(input), &output, sessionConfig(t, SessionLimits{})); err != nil {
		t.Fatal(err)
	}
	responses := map[uint64]engineprotocol.CompileResponseEnvelope{}
	for _, frame := range decodeFrames(t, output.Bytes()) {
		if frame.Kind == KindResponseControl && frame.StreamID != 1 {
			response, err := engineprotocol.DecodeCompileResponseEnvelope(frame.Payload)
			if err != nil {
				t.Fatal(err)
			}
			responses[frame.StreamID] = response
		}
	}
	if responses[2].Outcome != protocolcommon.OutcomeSuccess || responses[3].Failure == nil || responses[3].Failure.Code != endpoint.FailureCompileDuplicateRequest {
		t.Fatalf("responses = %+v", responses)
	}
}

func TestSessionCancelAfterSealJoinsExecuteBeforePublication(t *testing.T) {
	source := []byte("project p \"Project\" {}\n")
	for _, test := range []struct {
		name      string
		deadline  bool
		afterSeal []Frame
		hookDelay time.Duration
	}{
		{name: "cancel", afterSeal: []Frame{{Kind: KindCancel, StreamID: 2}}, hookDelay: 50 * time.Millisecond},
		{name: "deadline", deadline: true, hookDelay: 75 * time.Millisecond},
	} {
		test := test
		t.Run(test.name, func(t *testing.T) {
			request := sessionCompile(test.name+"-after-seal", "source", source)
			if test.deadline {
				deadline := protocolcommon.Rfc3339Time(time.Now().UTC().Add(25 * time.Millisecond).Format(time.RFC3339Nano))
				request.DeadlineAt = &deadline
			}
			frames := []Frame{
				controlFrame(t, 1, sessionHandshake("hs")), controlFrame(t, 2, request),
				{Kind: KindBlobChunk, Flags: FlagFinal, StreamID: 2, Sequence: 1, Name: []byte("source"), Payload: source},
				{Kind: KindBundleEnd, StreamID: 2, Sequence: 2},
			}
			frames = append(frames, test.afterSeal...)
			frames = append(frames, Frame{Kind: KindClose})
			var output bytes.Buffer
			if err := serve(context.Background(), bytes.NewReader(marshalFrames(t, frames...)), &output, sessionConfig(t, SessionLimits{}), func() { time.Sleep(test.hookDelay) }); err != nil {
				t.Fatal(err)
			}
			found := false
			for _, frame := range decodeFrames(t, output.Bytes()) {
				if frame.Kind == KindResponseControl && frame.StreamID == 2 {
					response, err := engineprotocol.DecodeCompileResponseEnvelope(frame.Payload)
					if err != nil {
						t.Fatal(err)
					}
					if response.Outcome != protocolcommon.OutcomeCancelled || response.Failure == nil || response.Failure.Code != endpoint.FailureCompileCancelled {
						t.Fatalf("response = %+v", response)
					}
					found = true
				}
			}
			if !found {
				t.Fatal("cancelled request had no joined terminal response")
			}
		})
	}
}

func TestSessionCorrelatedFailureAfterSealJoinsExecuteBeforePublication(t *testing.T) {
	source := []byte("project p \"Project\" {}\n")
	input := marshalFrames(t,
		controlFrame(t, 1, sessionHandshake("hs")),
		controlFrame(t, 2, sessionCompile("framing-after-seal", "source", source)),
		Frame{Kind: KindBlobChunk, Flags: FlagFinal, StreamID: 2, Sequence: 1, Name: []byte("source"), Payload: source},
		Frame{Kind: KindBundleEnd, StreamID: 2, Sequence: 2},
		Frame{Kind: KindBlobChunk, Flags: FlagFinal, StreamID: 2, Sequence: 3, Name: []byte("source"), Payload: source},
		Frame{Kind: KindClose},
	)
	var output bytes.Buffer
	if err := serve(context.Background(), bytes.NewReader(input), &output, sessionConfig(t, SessionLimits{}), func() { time.Sleep(50 * time.Millisecond) }); err != nil {
		t.Fatal(err)
	}
	assertCorrelatedFailure(t, decodeFrames(t, output.Bytes()), 2, "framing-after-seal")
}

func TestSessionAdmissionLimitAndZeroByteFinal(t *testing.T) {
	t.Parallel()
	limits := DefaultSessionLimits()
	limits.MaxReservedBlobBytes = 1
	large := sessionCompile("large", "source", []byte("too large"))
	input := marshalFrames(t, controlFrame(t, 1, sessionHandshake("hs")), controlFrame(t, 2, large), Frame{Kind: KindClose})
	var output bytes.Buffer
	if err := Serve(context.Background(), bytes.NewReader(input), &output, sessionConfig(t, limits)); err != nil {
		t.Fatal(err)
	}
	frames := decodeFrames(t, output.Bytes())
	for _, frame := range frames {
		if frame.Kind == KindRequestReady && frame.StreamID == 2 {
			t.Fatal("oversized request received credit")
		}
		if frame.Kind == KindResponseControl && frame.StreamID == 2 {
			response, err := engineprotocol.DecodeCompileResponseEnvelope(frame.Payload)
			if err != nil || response.Failure == nil || response.Failure.Code != endpoint.FailureCompileTransportLimit {
				t.Fatalf("limit response = %+v, %v", response, err)
			}
		}
	}

	empty := sessionCompile("empty", "empty-source", nil)
	input = marshalFrames(t,
		controlFrame(t, 1, sessionHandshake("hs")), controlFrame(t, 2, empty),
		Frame{Kind: KindBlobChunk, Flags: FlagFinal, StreamID: 2, Sequence: 1, Name: []byte("empty-source")},
		Frame{Kind: KindBundleEnd, StreamID: 2, Sequence: 2}, Frame{Kind: KindClose},
	)
	output.Reset()
	if err := Serve(context.Background(), bytes.NewReader(input), &output, sessionConfig(t, SessionLimits{})); err != nil {
		t.Fatal(err)
	}
	found := false
	for _, frame := range decodeFrames(t, output.Bytes()) {
		if frame.Kind == KindResponseControl && frame.StreamID == 2 {
			response, err := engineprotocol.DecodeCompileResponseEnvelope(frame.Payload)
			if err != nil || response.Outcome != protocolcommon.OutcomeRejected {
				t.Fatalf("zero-byte response = %+v, %v", response, err)
			}
			found = true
		}
	}
	if !found {
		t.Fatal("zero-byte stream had no response")
	}
}

func TestSessionUnexpectedZeroByteBlobsTerminateWithoutRetention(t *testing.T) {
	limits := DefaultSessionLimits()
	config := sessionConfig(t, limits)
	root, cancelRoot := context.WithCancel(context.Background())
	var output bytes.Buffer
	s := &session{
		config: config, limits: limits, root: root, encode: NewEncoder(&output),
		streams: make(map[uint64]*requestStream), requestIDs: make(map[string]*requestStream),
		results:   make(chan dispatchResult, limits.MaxConcurrentDispatch),
		deadlines: make(chan uint64, limits.MaxActiveStreams),
	}
	t.Cleanup(func() { s.abortAll(); cancelRoot() })
	if err := s.acceptControl(controlFrame(t, 1, sessionHandshake("zero-retention-hs"))); err != nil {
		t.Fatal(err)
	}
	source := []byte("project p \"Project\" {}\n")
	if err := s.acceptControl(controlFrame(t, 2, sessionCompile("zero-retention", "required", source))); err != nil {
		t.Fatal(err)
	}
	stream := s.streams[2]
	if stream == nil || stream.phase != phaseUploading {
		t.Fatalf("stream not uploading: %#v", stream)
	}
	started := time.Now()
	const count = 50_000
	for index := 0; index < count; index++ {
		frame := Frame{
			Kind: KindBlobChunk, Flags: FlagFinal, StreamID: 2, Sequence: uint32(index + 1),
			Name: []byte(fmt.Sprintf("unexpected-%08d", index)),
		}
		if err := s.acceptUpload(frame); err != nil {
			t.Fatalf("frame %d: %v", index, err)
		}
		if index == 0 && (s.streams[2] != nil || stream.validator != nil || stream.blobs != nil || stream.expected != nil || stream.requirements != nil || stream.received != 0) {
			t.Fatalf("first unknown frame retained stream state: %+v", stream)
		}
	}
	if elapsed := time.Since(started); elapsed > 5*time.Second {
		t.Fatalf("50k terminal late frames took %s", elapsed)
	}
	if len(s.streams) != 0 || len(s.requestIDs) != 0 || s.controls != 0 || s.controlSum != 0 || s.active != 0 || s.reserved != 0 || len(s.queue) != 0 || s.uploader != nil {
		t.Fatalf("retained session state: streams=%d requests=%d controls=%d bytes=%d active=%d reserved=%d queue=%d uploader=%p", len(s.streams), len(s.requestIDs), s.controls, s.controlSum, s.active, s.reserved, len(s.queue), s.uploader)
	}
	assertCorrelatedFailure(t, decodeFrames(t, output.Bytes()), 2, "zero-retention")
}

func TestSessionUploadCountAndMetadataLimitsRejectWithoutState(t *testing.T) {
	pack, _, _ := sessionPackCompile(t, "count-limit")
	source := []byte("project p \"Project\" {}\n")
	for _, test := range []struct {
		name    string
		limits  func(*SessionLimits)
		request engineprotocol.CompileRequestEnvelope
	}{
		{
			name: "count", request: pack,
			limits: func(limits *SessionLimits) { limits.MaxUploadBlobCount = 1 },
		},
		{
			name: "metadata", request: sessionCompile("metadata-limit", "source-name", source),
			limits: func(limits *SessionLimits) { limits.MaxUploadMetadataBytes = 1 },
		},
	} {
		test := test
		t.Run(test.name, func(t *testing.T) {
			limits := DefaultSessionLimits()
			test.limits(&limits)
			config := sessionConfig(t, limits)
			root, cancelRoot := context.WithCancel(context.Background())
			var output bytes.Buffer
			s := &session{
				config: config, limits: limits, root: root, encode: NewEncoder(&output),
				streams: make(map[uint64]*requestStream), requestIDs: make(map[string]*requestStream),
				results:   make(chan dispatchResult, limits.MaxConcurrentDispatch),
				deadlines: make(chan uint64, limits.MaxActiveStreams),
			}
			t.Cleanup(func() { s.abortAll(); cancelRoot() })
			if err := s.acceptControl(controlFrame(t, 1, sessionHandshake(test.name+"-hs"))); err != nil {
				t.Fatal(err)
			}
			if err := s.acceptControl(controlFrame(t, 2, test.request)); err != nil {
				t.Fatal(err)
			}
			if len(s.streams) != 0 || len(s.requestIDs) != 0 || s.controls != 0 || s.controlSum != 0 || s.active != 0 || s.reserved != 0 || len(s.queue) != 0 || s.uploader != nil {
				t.Fatalf("limit rejection retained state: %+v", s)
			}
			response := compileResponseForStream(t, decodeFrames(t, output.Bytes()), 2)
			if response.Failure == nil || response.Failure.Code != endpoint.FailureCompileTransportLimit {
				t.Fatalf("response = %+v", response)
			}
		})
	}
}

func TestSessionBlobIDFrameBoundaryAcceptsExactMaximumBytes(t *testing.T) {
	t.Parallel()
	source := []byte("project p \"Project\" {}\n")
	for _, test := range []struct {
		name   string
		blobID string
	}{
		{name: "ascii", blobID: strings.Repeat("a", int(MaxNameBytes))},
		{name: "unicode", blobID: strings.Repeat("😀", int(MaxNameBytes)/len("😀"))},
	} {
		test := test
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			if len(test.blobID) != int(MaxNameBytes) {
				t.Fatalf("test blob ID bytes = %d", len(test.blobID))
			}
			input := marshalFrames(t,
				controlFrame(t, 1, sessionHandshake(test.name+"-boundary-hs")),
				controlFrame(t, 2, sessionCompile(test.name+"-boundary", test.blobID, source)),
				Frame{Kind: KindBlobChunk, Flags: FlagFinal, StreamID: 2, Sequence: 1, Name: []byte(test.blobID), Payload: source},
				Frame{Kind: KindBundleEnd, StreamID: 2, Sequence: 2},
				Frame{Kind: KindClose},
			)
			var output bytes.Buffer
			if err := Serve(context.Background(), bytes.NewReader(input), &output, sessionConfig(t, SessionLimits{})); err != nil {
				t.Fatal(err)
			}
			frames := decodeFrames(t, output.Bytes())
			ready := 0
			for _, frame := range frames {
				if frame.Kind == KindRequestReady && frame.StreamID == 2 {
					ready++
				}
			}
			if ready != 1 {
				t.Fatalf("REQUEST_READY count = %d", ready)
			}
			if response := compileResponseForStream(t, frames, 2); response.Outcome != protocolcommon.OutcomeSuccess {
				t.Fatalf("boundary response = %+v", response)
			}
		})
	}
}

func TestSessionBlobIDOverFrameBoundaryRejectsWithoutStateAndRecovers(t *testing.T) {
	source := []byte("project p \"Project\" {}\n")
	for _, test := range []struct {
		name   string
		blobID string
	}{
		{name: "ascii", blobID: strings.Repeat("a", int(MaxNameBytes)+1)},
		{name: "unicode", blobID: strings.Repeat("😀", int(MaxNameBytes)/len("😀")) + "a"},
	} {
		test := test
		t.Run(test.name, func(t *testing.T) {
			if len(test.blobID) != int(MaxNameBytes)+1 {
				t.Fatalf("test blob ID bytes = %d", len(test.blobID))
			}
			limits := DefaultSessionLimits()
			config := sessionConfig(t, limits)
			root, cancelRoot := context.WithCancel(context.Background())
			var output bytes.Buffer
			s := &session{
				config: config, limits: limits, root: root, encode: NewEncoder(&output),
				streams: make(map[uint64]*requestStream), requestIDs: make(map[string]*requestStream),
				results:   make(chan dispatchResult, limits.MaxConcurrentDispatch),
				deadlines: make(chan uint64, limits.MaxActiveStreams),
			}
			defer func() { s.abortAll(); cancelRoot() }()
			if err := s.acceptControl(controlFrame(t, 1, sessionHandshake(test.name+"-oversized-hs"))); err != nil {
				t.Fatal(err)
			}
			if err := s.acceptControl(controlFrame(t, 2, sessionCompile(test.name+"-oversized", test.blobID, source))); err != nil {
				t.Fatal(err)
			}
			if len(s.streams) != 0 || len(s.requestIDs) != 0 || s.controls != 0 || s.controlSum != 0 || s.active != 0 || s.reserved != 0 || len(s.queue) != 0 || s.uploader != nil {
				t.Fatalf("oversized blob ID retained state: streams=%d requests=%d controls=%d bytes=%d active=%d reserved=%d queue=%d uploader=%p", len(s.streams), len(s.requestIDs), s.controls, s.controlSum, s.active, s.reserved, len(s.queue), s.uploader)
			}
			frames := decodeFrames(t, output.Bytes())
			for _, frame := range frames {
				if frame.Kind == KindRequestReady && frame.StreamID == 2 {
					t.Fatal("oversized blob ID received upload credit")
				}
			}
			if response := compileResponseForStream(t, frames, 2); response.Failure == nil || response.Failure.Code != endpoint.FailureCompileTransportLimit {
				t.Fatalf("oversized response = %+v", response)
			}

			if err := s.acceptControl(controlFrame(t, 3, sessionCompile(test.name+"-recovery", "source", source))); err != nil {
				t.Fatal(err)
			}
			if err := s.acceptUpload(Frame{Kind: KindBlobChunk, Flags: FlagFinal, StreamID: 3, Sequence: 1, Name: []byte("source"), Payload: source}); err != nil {
				t.Fatal(err)
			}
			if err := s.acceptUpload(Frame{Kind: KindBundleEnd, StreamID: 3, Sequence: 2}); err != nil {
				t.Fatal(err)
			}
			var result dispatchResult
			select {
			case result = <-s.results:
			case <-time.After(5 * time.Second):
				t.Fatal("recovery dispatch did not finish")
			}
			if err := s.finishDispatch(result); err != nil {
				t.Fatal(err)
			}
			frames = decodeFrames(t, output.Bytes())
			if response := compileResponseForStream(t, frames, 3); response.Outcome != protocolcommon.OutcomeSuccess {
				t.Fatalf("recovery response = %+v", response)
			}
		})
	}
}

func TestSessionCommittedExecutionWinsLateFramingBeforeJoin(t *testing.T) {
	limits := DefaultSessionLimits()
	config := sessionConfig(t, limits)
	root, cancelRoot := context.WithCancel(context.Background())
	var output bytes.Buffer
	s := &session{
		config: config, limits: limits, root: root, encode: NewEncoder(&output),
		streams: make(map[uint64]*requestStream), requestIDs: make(map[string]*requestStream),
		results:   make(chan dispatchResult, limits.MaxConcurrentDispatch),
		deadlines: make(chan uint64, limits.MaxActiveStreams),
	}
	t.Cleanup(func() { s.abortAll(); cancelRoot() })
	if err := s.acceptControl(controlFrame(t, 1, sessionHandshake("terminal-hs"))); err != nil {
		t.Fatal(err)
	}
	source := []byte("project p \"Project\" {}\n")
	if err := s.acceptControl(controlFrame(t, 2, sessionCompile("terminal-first", "source", source))); err != nil {
		t.Fatal(err)
	}
	stream := s.streams[2]
	if err := s.acceptUpload(Frame{Kind: KindBlobChunk, Flags: FlagFinal, StreamID: 2, Sequence: 1, Name: []byte("source"), Payload: source}); err != nil {
		t.Fatal(err)
	}
	if err := stream.validator.Accept(Frame{Kind: KindBundleEnd, StreamID: 2, Sequence: 2}); err != nil {
		t.Fatal(err)
	}
	s.uploader = nil
	stream.phase = phaseDispatching
	sink := &captureSink{ctx: stream.ctx, maximum: limits.MaxOutputBlobBytes, commit: stream.terminal.claimExecutionPublication}
	response, err := stream.plan.Execute(stream.ctx, collectedSource{stream: stream}, sink)
	if err != nil || response.Outcome != protocolcommon.OutcomeSuccess {
		t.Fatalf("execute = %+v, %v", response, err)
	}
	stream.terminal.claimExecution(response)
	encodedResponse, ok := dispatchResponse(response)
	if !ok {
		t.Fatal("compile response did not encode")
	}
	result := dispatchResult{stream: stream, response: encodedResponse, blobs: sink.take()}
	if err := s.failCorrelatedStream(stream); err != nil {
		t.Fatal(err)
	}
	if kind, terminal, responseSet := stream.terminal.snapshot(); kind != terminalExecution || !responseSet || terminal.Outcome != protocolcommon.OutcomeSuccess || stream.ctx.Err() != nil {
		t.Fatalf("late framing changed terminal: kind=%d context=%v", kind, stream.ctx.Err())
	}
	if err := s.finishDispatch(result); err != nil {
		t.Fatal(err)
	}
	frames := decodeFrames(t, output.Bytes())
	if response := compileResponseForStream(t, frames, 2); response.Outcome != protocolcommon.OutcomeSuccess {
		t.Fatalf("published terminal = %+v", response)
	}
	controls, ends := 0, 0
	for _, frame := range frames {
		if frame.StreamID != 2 {
			continue
		}
		if frame.Kind == KindResponseControl {
			controls++
		}
		if frame.Kind == KindBundleEnd {
			ends++
		}
	}
	if controls != 1 || ends != 1 {
		t.Fatalf("terminal bundle controls=%d ends=%d", controls, ends)
	}
}

func TestSessionPostExecuteGapPublishesExactWinningTerminal(t *testing.T) {
	validSource := []byte("project p \"Project\" {}\n")
	for _, execution := range []struct {
		name        string
		declared    []byte
		uploaded    []byte
		wantOutcome protocolcommon.Outcome
		wantCode    string
	}{
		{name: "rejected", declared: nil, uploaded: nil, wantOutcome: protocolcommon.OutcomeRejected},
		{name: "failed", declared: validSource, uploaded: bytes.Repeat([]byte{'x'}, len(validSource)), wantOutcome: protocolcommon.OutcomeFailed, wantCode: endpoint.FailureCompileBlobDigestMismatch},
	} {
		execution := execution
		for _, event := range []struct {
			name        string
			apply       func(*session, *requestStream) error
			wantOutcome protocolcommon.Outcome
			wantCode    string
		}{
			{
				name: "cancellation", wantOutcome: protocolcommon.OutcomeCancelled, wantCode: endpoint.FailureCompileCancelled,
				apply: func(s *session, stream *requestStream) error { return s.cancelStream(stream, true) },
			},
			{
				name: "transport", wantOutcome: protocolcommon.OutcomeFailed, wantCode: endpoint.FailureCompileTransportProtocol,
				apply: func(s *session, stream *requestStream) error { return s.failCorrelatedStream(stream) },
			},
		} {
			event := event
			t.Run(execution.name+"/"+event.name, func(t *testing.T) {
				limits := DefaultSessionLimits()
				config := sessionConfig(t, limits)
				root, cancelRoot := context.WithCancel(context.Background())
				var output bytes.Buffer
				executeReturned := make(chan struct{})
				releaseClaim := make(chan struct{})
				released := false
				s := &session{
					config: config, limits: limits, root: root, encode: NewEncoder(&output),
					streams: make(map[uint64]*requestStream), requestIDs: make(map[string]*requestStream),
					results:   make(chan dispatchResult, limits.MaxConcurrentDispatch),
					deadlines: make(chan uint64, limits.MaxActiveStreams),
					beforeResultClaim: func() {
						close(executeReturned)
						<-releaseClaim
					},
				}
				defer func() {
					if !released {
						close(releaseClaim)
					}
					s.abortAll()
					cancelRoot()
				}()
				if err := s.acceptControl(controlFrame(t, 1, sessionHandshake(execution.name+"-"+event.name+"-hs"))); err != nil {
					t.Fatal(err)
				}
				requestID := execution.name + "-" + event.name
				if err := s.acceptControl(controlFrame(t, 2, sessionCompile(requestID, "source", execution.declared))); err != nil {
					t.Fatal(err)
				}
				stream := s.streams[2]
				if stream == nil || stream.phase != phaseUploading {
					t.Fatalf("stream = %#v", stream)
				}
				if err := s.acceptUpload(Frame{Kind: KindBlobChunk, Flags: FlagFinal, StreamID: 2, Sequence: 1, Name: []byte("source"), Payload: execution.uploaded}); err != nil {
					t.Fatal(err)
				}
				if err := s.acceptUpload(Frame{Kind: KindBundleEnd, StreamID: 2, Sequence: 2}); err != nil {
					t.Fatal(err)
				}
				select {
				case <-executeReturned:
				case <-time.After(5 * time.Second):
					t.Fatal("Execute did not reach the pre-claim hook")
				}
				if kind, _, responseSet := stream.terminal.snapshot(); kind != terminalUndecided || responseSet {
					t.Fatalf("non-publication result claimed before hook: kind=%d set=%v", kind, responseSet)
				}
				if err := event.apply(s, stream); err != nil {
					t.Fatal(err)
				}
				kind, terminal, responseSet := stream.terminal.snapshot()
				if !responseSet || terminal.Outcome != event.wantOutcome || terminal.Failure == nil || terminal.Failure.Code != event.wantCode {
					t.Fatalf("winner before join: kind=%d response=%+v set=%v", kind, terminal, responseSet)
				}
				close(releaseClaim)
				released = true
				var result dispatchResult
				select {
				case result = <-s.results:
				case <-time.After(5 * time.Second):
					t.Fatal("dispatch did not join")
				}
				if result.response.Outcome != execution.wantOutcome || (execution.wantCode != "" && (result.response.Failure == nil || result.response.Failure.Code != execution.wantCode)) {
					t.Fatalf("losing Execute response = %+v", result.response)
				}
				if err := s.finishDispatch(result); err != nil {
					t.Fatal(err)
				}
				frames := decodeFrames(t, output.Bytes())
				published := compileResponseForStream(t, frames, 2)
				if published.Outcome != event.wantOutcome || published.Failure == nil || published.Failure.Code != event.wantCode {
					t.Fatalf("published winner = %+v", published)
				}
				controls, ends := 0, 0
				for _, frame := range frames {
					if frame.StreamID == 2 && frame.Kind == KindResponseControl {
						controls++
					}
					if frame.StreamID == 2 && frame.Kind == KindBundleEnd {
						ends++
					}
				}
				if controls != 1 || ends != 1 {
					t.Fatalf("terminal bundle controls=%d ends=%d", controls, ends)
				}
			})
		}
	}
}

func TestTerminalArbiterLinearizesConcurrentEventsExactlyOnce(t *testing.T) {
	for iteration := 0; iteration < 1_000; iteration++ {
		var arbiter terminalArbiter
		execution := dispatchResponseForTest("execution")
		cancellation := dispatchResponseForTest("cancellation")
		transport := dispatchResponseForTest("transport")
		start := make(chan struct{})
		claims := make(chan bool, 3)
		var workers sync.WaitGroup
		workers.Add(3)
		go func() {
			defer workers.Done()
			<-start
			if iteration%2 == 0 {
				claims <- arbiter.claimExecution(execution)
				return
			}
			won := arbiter.claimExecutionPublication()
			if won {
				arbiter.claimExecution(execution)
			}
			claims <- won
		}()
		go func() {
			defer workers.Done()
			<-start
			claims <- arbiter.claimCancellation(cancellation)
		}()
		go func() {
			defer workers.Done()
			<-start
			claims <- arbiter.claimTransport(transport)
		}()
		close(start)
		workers.Wait()
		close(claims)
		winners := 0
		for won := range claims {
			if won {
				winners++
			}
		}
		kind, response, responseSet := arbiter.snapshot()
		wantRequestID := map[terminalKind]string{terminalExecution: "execution", terminalCancellation: "cancellation", terminalTransport: "transport"}[kind]
		if winners != 1 || kind == terminalUndecided || !responseSet || response.RequestID != wantRequestID {
			t.Fatalf("iteration %d: winners=%d kind=%d response=%+v set=%v", iteration, winners, kind, response, responseSet)
		}
	}
}

func TestSessionStreamReuseIsFatal(t *testing.T) {
	t.Parallel()
	input := marshalFrames(t,
		controlFrame(t, 1, sessionHandshake("one")),
		controlFrame(t, 1, sessionHandshake("two")),
	)
	var output bytes.Buffer
	err := Serve(context.Background(), bytes.NewReader(input), &output, sessionConfig(t, SessionLimits{}))
	var sessionError *SessionError
	if !errors.As(err, &sessionError) || sessionError.Code != SessionErrorFraming {
		t.Fatalf("error = %v", err)
	}
}

func TestSessionIncomingStreamErrorFailsClosedAndRecoversNextRequest(t *testing.T) {
	t.Parallel()
	source := []byte("project p \"Project\" {}\n")
	input := marshalFrames(t,
		Frame{Kind: KindStreamError, StreamID: 1, Name: []byte("peer-error")},
		controlFrame(t, 2, sessionHandshake("after-peer-error")),
		controlFrame(t, 3, sessionCompile("after-peer-error-compile", "source", source)),
		Frame{Kind: KindBlobChunk, Flags: FlagFinal, StreamID: 3, Sequence: 1, Name: []byte("source"), Payload: source},
		Frame{Kind: KindBundleEnd, StreamID: 3, Sequence: 2},
		Frame{Kind: KindClose},
	)
	var output bytes.Buffer
	if err := Serve(context.Background(), bytes.NewReader(input), &output, sessionConfig(t, SessionLimits{})); err != nil {
		t.Fatal(err)
	}
	frames := decodeFrames(t, output.Bytes())
	if len(frames) < 2 || frames[0].Kind != KindStreamError || frames[0].StreamID != 1 || string(frames[0].Name) != "unexpected_direction" || frames[1].Kind != KindBundleEnd {
		t.Fatalf("peer STREAM_ERROR terminal = %#v", frames)
	}
	if response := compileResponseForStream(t, frames, 3); response.Outcome != protocolcommon.OutcomeSuccess {
		t.Fatalf("later response = %+v", response)
	}
}

func TestSessionIncomingStreamErrorOnKnownStreamUsesCorrelatedTerminal(t *testing.T) {
	t.Parallel()
	source := []byte("project p \"Project\" {}\n")
	input := marshalFrames(t,
		controlFrame(t, 1, sessionHandshake("peer-known-hs")),
		controlFrame(t, 2, sessionCompile("peer-known", "source", source)),
		Frame{Kind: KindStreamError, StreamID: 2, Name: []byte("peer-error")},
		controlFrame(t, 3, sessionCompile("peer-known-later", "later", source)),
		Frame{Kind: KindBlobChunk, Flags: FlagFinal, StreamID: 3, Sequence: 1, Name: []byte("later"), Payload: source},
		Frame{Kind: KindBundleEnd, StreamID: 3, Sequence: 2},
		Frame{Kind: KindClose},
	)
	var output bytes.Buffer
	if err := Serve(context.Background(), bytes.NewReader(input), &output, sessionConfig(t, SessionLimits{})); err != nil {
		t.Fatal(err)
	}
	frames := decodeFrames(t, output.Bytes())
	assertCorrelatedFailure(t, frames, 2, "peer-known")
	if response := compileResponseForStream(t, frames, 3); response.Outcome != protocolcommon.OutcomeSuccess {
		t.Fatalf("later response = %+v", response)
	}
}

func TestSessionIncomingStreamErrorReplayIsFatal(t *testing.T) {
	t.Parallel()
	input := marshalFrames(t,
		controlFrame(t, 1, sessionHandshake("peer-replay-hs")),
		Frame{Kind: KindStreamError, StreamID: 1, Name: []byte("peer-replay")},
	)
	var output bytes.Buffer
	err := Serve(context.Background(), bytes.NewReader(input), &output, sessionConfig(t, SessionLimits{}))
	var sessionError *SessionError
	if !errors.As(err, &sessionError) || sessionError.Code != SessionErrorFraming {
		t.Fatalf("error = %v", err)
	}
	frames := decodeFrames(t, output.Bytes())
	if len(frames) != 2 || frames[0].Kind != KindResponseControl || frames[1].Kind != KindBundleEnd {
		t.Fatalf("replay emitted an ambiguous second terminal: %#v", frames)
	}
}

func TestSessionMalformedIncomingStreamErrorDrainsBodyAndRecovers(t *testing.T) {
	t.Parallel()
	invalid := Frame{Kind: KindStreamError, StreamID: 1, Sequence: 1, Name: []byte("peer-error")}
	input := append(rawHeader(invalid), invalid.Name...)
	input = append(input, marshalFrames(t, controlFrame(t, 2, sessionHandshake("after-malformed-peer-error")), Frame{Kind: KindClose})...)
	var output bytes.Buffer
	if err := Serve(context.Background(), bytes.NewReader(input), &output, sessionConfig(t, SessionLimits{})); err != nil {
		t.Fatal(err)
	}
	frames := decodeFrames(t, output.Bytes())
	if len(frames) != 4 || frames[0].Kind != KindStreamError || frames[0].StreamID != 1 || frames[1].Kind != KindBundleEnd || frames[2].Kind != KindResponseControl || frames[2].StreamID != 2 {
		t.Fatalf("frames = %#v", frames)
	}
}

func TestSessionRecoverableFrameAndStateErrors(t *testing.T) {
	t.Parallel()
	source := []byte("project p \"Project\" {}\n")
	prefix := marshalFrames(t, controlFrame(t, 1, sessionHandshake("hs")), controlFrame(t, 2, sessionCompile("compile", "source", source)))
	invalid := Frame{Kind: KindBlobChunk, StreamID: 2, Sequence: 1, Name: []byte("source"), Payload: []byte("short")}
	input := append(prefix, rawHeader(invalid)...)
	input = append(input, invalid.Name...)
	input = append(input, invalid.Payload...)
	input = append(input, marshalFrames(t,
		controlFrame(t, 3, sessionCompile("after-invalid-chunk", "later", source)),
		Frame{Kind: KindBlobChunk, Flags: FlagFinal, StreamID: 3, Sequence: 1, Name: []byte("later"), Payload: source},
		Frame{Kind: KindBundleEnd, StreamID: 3, Sequence: 2},
		Frame{Kind: KindClose},
	)...)
	var output bytes.Buffer
	if err := Serve(context.Background(), bytes.NewReader(input), &output, sessionConfig(t, SessionLimits{})); err != nil {
		t.Fatal(err)
	}
	frames := decodeFrames(t, output.Bytes())
	assertCorrelatedFailure(t, frames, 2, "compile")
	if response := compileResponseForStream(t, frames, 3); response.Outcome != protocolcommon.OutcomeSuccess {
		t.Fatalf("later response = %+v", response)
	}

	limits := DefaultSessionLimits()
	limits.MaxConcurrentDispatch = 1
	input = marshalFrames(t,
		controlFrame(t, 1, sessionHandshake("hs")),
		controlFrame(t, 2, sessionCompile("one", "a", source)),
		controlFrame(t, 3, sessionCompile("two", "b", source)),
		Frame{Kind: KindBlobChunk, Flags: FlagFinal, StreamID: 3, Sequence: 1, Name: []byte("b"), Payload: source},
		Frame{Kind: KindBlobChunk, Flags: FlagFinal, StreamID: 2, Sequence: 1, Name: []byte("a"), Payload: source},
		Frame{Kind: KindBundleEnd, StreamID: 2, Sequence: 2}, Frame{Kind: KindClose},
	)
	output.Reset()
	if err := Serve(context.Background(), bytes.NewReader(input), &output, sessionConfig(t, limits)); err != nil {
		t.Fatal(err)
	}
	frames = decodeFrames(t, output.Bytes())
	assertCorrelatedFailure(t, frames, 3, "two")
	if response := compileResponseForStream(t, frames, 2); response.Outcome != protocolcommon.OutcomeSuccess {
		t.Fatalf("credited response = %+v", response)
	}

	if got := (&SessionError{Code: SessionErrorOutput}).Error(); got != "layerdraw stdio: output" {
		t.Fatalf("safe error = %q", got)
	}
	var nilError *SessionError
	if nilError.Error() != "<nil>" {
		t.Fatal("nil safe error")
	}
}

func TestSessionCorrelatedUploadFailuresRecoverExactlyOnce(t *testing.T) {
	t.Parallel()
	source := []byte("project p \"Project\" {}\n")
	for _, test := range []struct {
		name   string
		frames []Frame
	}{
		{
			name:   "sequence",
			frames: []Frame{{Kind: KindBlobChunk, Flags: FlagFinal, StreamID: 2, Sequence: 2, Name: []byte("bad"), Payload: source}},
		},
		{
			name:   "offset",
			frames: []Frame{{Kind: KindBlobChunk, Flags: FlagFinal, StreamID: 2, Sequence: 1, Name: []byte("bad"), Payload: source, Offset: 1}},
		},
		{
			name:   "reserved bytes",
			frames: []Frame{{Kind: KindBlobChunk, Flags: FlagFinal, StreamID: 2, Sequence: 1, Name: []byte("bad"), Payload: append(append([]byte(nil), source...), 'x')}},
		},
	} {
		test := test
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			inputFrames := []Frame{
				controlFrame(t, 1, sessionHandshake("hs")),
				controlFrame(t, 2, sessionCompile("bad", "bad", source)),
			}
			inputFrames = append(inputFrames, test.frames...)
			inputFrames = append(inputFrames,
				controlFrame(t, 3, sessionCompile("later", "later", source)),
				Frame{Kind: KindBlobChunk, Flags: FlagFinal, StreamID: 3, Sequence: 1, Name: []byte("later"), Payload: source},
				Frame{Kind: KindBundleEnd, StreamID: 3, Sequence: 2},
				Frame{Kind: KindClose},
			)
			var output bytes.Buffer
			if err := Serve(context.Background(), bytes.NewReader(marshalFrames(t, inputFrames...)), &output, sessionConfig(t, SessionLimits{})); err != nil {
				t.Fatal(err)
			}
			frames := decodeFrames(t, output.Bytes())
			assertCorrelatedFailure(t, frames, 2, "bad")
			if response := compileResponseForStream(t, frames, 3); response.Outcome != protocolcommon.OutcomeSuccess {
				t.Fatalf("later response = %+v", response)
			}
		})
	}
}

func TestSessionHighWaterKeepsTerminalRegistryBounded(t *testing.T) {
	t.Parallel()
	var output bytes.Buffer
	s := &session{
		encode:     NewEncoder(&output),
		streams:    make(map[uint64]*requestStream),
		requestIDs: make(map[string]*requestStream),
	}
	payload := []byte(`{"operation":"engine.unknown","request_id":"bounded"}`)
	for streamID := uint64(1); streamID <= 4096; streamID++ {
		if err := s.acceptControl(Frame{Kind: KindRequestControl, StreamID: streamID, Payload: payload}); err != nil {
			t.Fatal(err)
		}
		if len(s.streams) != 0 || len(s.requestIDs) != 0 {
			t.Fatalf("retained terminal state at stream %d: streams=%d requests=%d", streamID, len(s.streams), len(s.requestIDs))
		}
	}
	if s.highWater != 4096 {
		t.Fatalf("high water = %d", s.highWater)
	}
	if err := s.acceptControl(Frame{Kind: KindRequestControl, StreamID: 4095, Payload: payload}); err == nil {
		t.Fatal("non-monotonic stream ID was accepted")
	}
}

func TestSessionAdmissionRejectionsRetainNoState(t *testing.T) {
	limits := DefaultSessionLimits()
	limits.MaxConcurrentDispatch = 1
	config := sessionConfig(t, limits)
	root, cancelRoot := context.WithCancel(context.Background())
	s := &session{
		config:     config,
		limits:     limits,
		root:       root,
		encode:     NewEncoder(io.Discard),
		streams:    make(map[uint64]*requestStream),
		requestIDs: make(map[string]*requestStream),
		results:    make(chan dispatchResult, limits.MaxConcurrentDispatch),
		deadlines:  make(chan uint64, limits.MaxActiveStreams),
	}
	t.Cleanup(func() {
		s.abortAll()
		cancelRoot()
	})

	if err := s.acceptControl(controlFrame(t, 1, sessionHandshake("hs"))); err != nil {
		t.Fatal(err)
	}
	source := []byte("project p \"Project\" {}\n")
	for index := 0; index < limits.MaxActiveStreams; index++ {
		requestID := fmt.Sprintf("stalled-%d", index)
		streamID := uint64(index + 2)
		if err := s.acceptControl(controlFrame(t, streamID, sessionCompile(requestID, requestID, source))); err != nil {
			t.Fatal(err)
		}
	}
	if len(s.streams) != limits.MaxActiveStreams || len(s.requestIDs) != limits.MaxActiveStreams || s.controls != limits.MaxActiveStreams || s.active != 1 || len(s.queue) != limits.MaxActiveStreams-1 {
		t.Fatalf("stalled baseline: streams=%d requests=%d controls=%d active=%d queued=%d", len(s.streams), len(s.requestIDs), s.controls, s.active, len(s.queue))
	}
	baselineControlSum, baselineReserved := s.controlSum, s.reserved
	runtime.GC()
	var before runtime.MemStats
	runtime.ReadMemStats(&before)
	baselineGoroutines := runtime.NumGoroutine()

	const rejected = 20000
	for index := 0; index < rejected; index++ {
		requestID := fmt.Sprintf("rejected-%d", index)
		streamID := uint64(limits.MaxActiveStreams + index + 2)
		if err := s.acceptControl(controlFrame(t, streamID, sessionCompile(requestID, "rejected", source))); err != nil {
			t.Fatal(err)
		}
		if index%1000 == 0 {
			if len(s.streams) != limits.MaxActiveStreams || len(s.requestIDs) != limits.MaxActiveStreams || s.controls != limits.MaxActiveStreams || s.controlSum != baselineControlSum || s.reserved != baselineReserved || s.active != 1 || len(s.queue) != limits.MaxActiveStreams-1 {
				t.Fatalf("retained rejected request %d: streams=%d requests=%d controls=%d bytes=%d reserved=%d active=%d queued=%d", index, len(s.streams), len(s.requestIDs), s.controls, s.controlSum, s.reserved, s.active, len(s.queue))
			}
			if s.requestIDs[requestID] != nil || s.streams[streamID] != nil {
				t.Fatalf("rejected request %d remains addressable", index)
			}
		}
	}
	if want := uint64(limits.MaxActiveStreams + rejected + 1); s.highWater != want {
		t.Fatalf("high water = %d, want %d", s.highWater, want)
	}
	runtime.GC()
	var after runtime.MemStats
	runtime.ReadMemStats(&after)
	heapGrowth := int64(after.HeapAlloc) - int64(before.HeapAlloc)
	objectGrowth := int64(after.HeapObjects) - int64(before.HeapObjects)
	if heapGrowth > 4<<20 || objectGrowth > 4096 {
		t.Fatalf("rejections retained heap state: bytes=%d objects=%d", heapGrowth, objectGrowth)
	}
	if growth := runtime.NumGoroutine() - baselineGoroutines; growth > 4 {
		t.Fatalf("rejections retained goroutines: growth=%d", growth)
	}
	runtime.KeepAlive(s)
}

func sessionConfig(t *testing.T, limits SessionLimits) SessionConfig {
	t.Helper()
	compiler := engine.New(engine.BuildInfo{})
	descriptor, err := endpoint.NewDescriptor(endpoint.DescriptorConfig{
		EngineRelease: engine.DevelopmentVersion, SourceRevision: engine.UnknownSourceRevision,
		ReleaseManifestDigest: sessionTestDigest, EndpointInstanceID: "stdio-test",
		Transports: []string{TransportID}, Limits: endpoint.DefaultLimitPolicy(),
	})
	if err != nil {
		t.Fatal(err)
	}
	return SessionConfig{Descriptor: descriptor, Dispatcher: endpoint.NewCompileDispatcher(compiler), Limits: limits}
}

func sessionHandshake(requestID string) engineprotocol.HandshakeRequestEnvelope {
	return engineprotocol.HandshakeRequestEnvelope{
		Operation: engineprotocol.HandshakeRequestEnvelopeOperationValue,
		Payload: protocolcommon.HandshakeRequest{
			ClientRelease: "1.0.0", OptionalCapabilities: []protocolcommon.CapabilityID{},
			Protocols:            []protocolcommon.ProtocolOffer{{Name: endpoint.ProtocolName, SupportedRange: "1.0..1.0", Versions: []protocolcommon.ProtocolVersionBinding{{Version: endpoint.ProtocolVersion, SchemaDigest: protocolcommon.Digest(engineprotocol.SchemaDigest)}}}},
			RequiredCapabilities: []protocolcommon.CapabilityID{endpoint.OperationCompile},
		},
		Protocol:  engineprotocol.EngineProtocolRef{Name: engineprotocol.EngineProtocolRefNameValue, Version: engineprotocol.EngineProtocolRefVersionValue},
		RequestID: requestID,
	}
}

func sessionCompile(requestID, blobID string, source []byte) engineprotocol.CompileRequestEnvelope {
	digest := sha256.Sum256(source)
	ref := protocolcommon.BlobRef{
		BlobID: blobID, Digest: protocolcommon.Digest("sha256:" + hex.EncodeToString(digest[:])),
		Lifetime: protocolcommon.BlobLifetimeRequest, MediaType: "text/plain; charset=utf-8",
		Size: protocolcommon.CanonicalUint64(strconv.Itoa(len(source))),
	}
	return engineprotocol.CompileRequestEnvelope{
		Operation: engineprotocol.CompileRequestEnvelopeOperationValue,
		Payload: engineprotocol.CompileInput{
			EntryPath: "document.ldl", InstalledPackTree: []engineprotocol.SourceFileInput{}, Mode: engineprotocol.CompileModeProject,
			ProjectSourceTree: []engineprotocol.SourceFileInput{{Path: "document.ldl", Blob: ref}}, ReferencedAssets: []engineprotocol.AssetInput{},
			ResolvedDependencies: engineprotocol.ResolvedDependencies{Format: engineprotocol.ResolvedDependenciesFormatValue, FormatVersion: 1, Language: 1, Installs: []engineprotocol.ResolvedPack{}},
			ResourceLimits:       engineprotocol.ResourceLimits{},
		},
		Protocol: engineprotocol.EngineProtocolRef{Name: engineprotocol.EngineProtocolRefNameValue, Version: engineprotocol.EngineProtocolRefVersionValue}, RequestID: requestID,
	}
}

func sessionOpenDocument(requestID, blobID string, source []byte) engineprotocol.OpenDocumentRequestEnvelope {
	compile := sessionCompile(requestID, blobID, source)
	return engineprotocol.OpenDocumentRequestEnvelope{
		Operation: engineprotocol.OpenDocumentRequestEnvelopeOperationValue,
		Payload: engineprotocol.OpenDocumentInput{
			CompileInput:    compile.Payload,
			RequestedLimits: engineprotocol.WorkbenchLimits{MaxItems: "100", MaxOutputBytes: "1000000"},
		},
		Protocol:  engineprotocol.EngineProtocolRef{Name: engineprotocol.EngineProtocolRefNameValue, Version: engineprotocol.EngineProtocolRefVersionValue},
		RequestID: requestID,
	}
}

func sessionPackCompile(t *testing.T, requestID string) (engineprotocol.CompileRequestEnvelope, []byte, []byte) {
	t.Helper()
	packSource := []byte("entity_type service \"Service\" {\n  representation shape rect\n}\nexport { service }\n")
	manifest, err := json.Marshal(map[string]any{"format": "layerdraw-pack", "format_version": 1, "id": "pub/schema", "name": "schema", "version": "1.0.0", "language": 1, "entry": "pack.ldl", "dependencies": map[string]any{}})
	if err != nil {
		t.Fatal(err)
	}
	fileRef := blobRef("pack-file", "text/plain; charset=utf-8", packSource)
	manifestRef := blobRef("pack-manifest", "application/json", manifest)
	root := engineprotocol.CanonicalPackSelector("pub/schema")
	request := engineprotocol.CompileRequestEnvelope{
		Operation: engineprotocol.CompileRequestEnvelopeOperationValue,
		Payload: engineprotocol.CompileInput{
			Mode: engineprotocol.CompileModePack, EntryPath: "pack.ldl", RootPackID: &root,
			ProjectSourceTree: []engineprotocol.SourceFileInput{}, InstalledPackTree: []engineprotocol.SourceFileInput{{Path: "pack/schema/pack.ldl", Blob: fileRef}},
			ResolvedDependencies: engineprotocol.ResolvedDependencies{Format: engineprotocol.ResolvedDependenciesFormatValue, FormatVersion: 1, Language: 1, Installs: []engineprotocol.ResolvedPack{{InstallName: "schema", CanonicalID: root, Version: "1.0.0", Digest: protocolcommon.Digest("sha256:" + strings.Repeat("a", 64)), Path: "pack/schema", Entry: "pack.ldl", Files: []engineprotocol.ResolvedPackFile{{Path: "pack.ldl", Digest: fileRef.Digest}}, Dependencies: []engineprotocol.ResolvedPackDependency{}, ManifestPath: "manifest.json", Manifest: manifestRef}}},
			ReferencedAssets:     []engineprotocol.AssetInput{}, ResourceLimits: engineprotocol.ResourceLimits{},
		},
		Protocol: engineprotocol.EngineProtocolRef{Name: engineprotocol.EngineProtocolRefNameValue, Version: engineprotocol.EngineProtocolRefVersionValue}, RequestID: requestID,
	}
	return request, packSource, manifest
}

func blobRef(id, mediaType string, value []byte) protocolcommon.BlobRef {
	digest := sha256.Sum256(value)
	return protocolcommon.BlobRef{BlobID: id, Digest: protocolcommon.Digest("sha256:" + hex.EncodeToString(digest[:])), Lifetime: protocolcommon.BlobLifetimeRequest, MediaType: mediaType, Size: protocolcommon.CanonicalUint64(strconv.Itoa(len(value)))}
}

func controlFrame(t *testing.T, streamID uint64, value any) Frame {
	t.Helper()
	var payload []byte
	var err error
	switch typed := value.(type) {
	case engineprotocol.HandshakeRequestEnvelope:
		payload, err = engineprotocol.EncodeHandshakeRequestEnvelope(typed)
	case engineprotocol.CompileRequestEnvelope:
		payload, err = engineprotocol.EncodeCompileRequestEnvelope(typed)
	case engineprotocol.OpenDocumentRequestEnvelope:
		payload, err = engineprotocol.EncodeOpenDocumentRequestEnvelope(typed)
	default:
		t.Fatalf("unknown control type %T", value)
	}
	if err != nil {
		t.Fatal(err)
	}
	return Frame{Kind: KindRequestControl, StreamID: streamID, Payload: payload}
}

func marshalFrames(t *testing.T, frames ...Frame) []byte {
	t.Helper()
	var result bytes.Buffer
	encoder := NewEncoder(&result)
	if err := encoder.WriteFrames(frames); err != nil {
		t.Fatal(err)
	}
	return result.Bytes()
}

func decodeFrames(t *testing.T, encoded []byte) []Frame {
	t.Helper()
	decoder := NewDecoder(bytes.NewReader(encoded))
	var frames []Frame
	for {
		frame, err := decoder.ReadFrame()
		if err == io.EOF {
			return frames
		}
		if err != nil {
			t.Fatal(err)
		}
		frames = append(frames, frame)
	}
}

func assertCorrelatedFailure(t *testing.T, frames []Frame, streamID uint64, requestID string) {
	t.Helper()
	controls, ends := 0, 0
	for _, frame := range frames {
		if frame.StreamID != streamID {
			continue
		}
		switch frame.Kind {
		case KindResponseControl:
			controls++
			response, err := engineprotocol.DecodeCompileResponseEnvelope(frame.Payload)
			if err != nil {
				t.Fatal(err)
			}
			if response.RequestID != requestID || response.Outcome != protocolcommon.OutcomeFailed || response.Failure == nil || response.Failure.Code != endpoint.FailureCompileTransportProtocol || response.Failure.Category != protocolcommon.ProtocolFailureCategoryTransport || response.Failure.Retryable {
				t.Fatalf("correlated response = %+v", response)
			}
		case KindBundleEnd:
			ends++
		case KindStreamError:
			t.Fatalf("correlated stream %d used STREAM_ERROR %q", streamID, frame.Name)
		}
	}
	if controls != 1 || ends != 1 {
		t.Fatalf("correlated stream %d controls=%d ends=%d", streamID, controls, ends)
	}
}

func compileResponseForStream(t *testing.T, frames []Frame, streamID uint64) engineprotocol.CompileResponseEnvelope {
	t.Helper()
	for _, frame := range frames {
		if frame.Kind == KindResponseControl && frame.StreamID == streamID {
			response, err := engineprotocol.DecodeCompileResponseEnvelope(frame.Payload)
			if err != nil {
				t.Fatal(err)
			}
			return response
		}
	}
	t.Fatalf("stream %d has no compile response", streamID)
	return engineprotocol.CompileResponseEnvelope{}
}
