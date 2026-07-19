// SPDX-License-Identifier: LicenseRef-LayerDraw-1.0

package stdio

import (
	"context"
	"fmt"
	"io"
	"testing"
	"time"

	"github.com/dencyuinc/layerdraw/gen/go/engineprotocol"
	"github.com/dencyuinc/layerdraw/gen/go/protocolcommon"
	"github.com/dencyuinc/layerdraw/internal/engine/endpoint"
)

type portableTestEndpoint struct {
	plan              endpoint.DispatchPlan
	transportFailures int
}

func testDispatch(operation, requestID string) endpoint.DispatchResponse {
	return endpoint.DispatchResponse{Operation: operation, RequestID: requestID, Control: []byte(`{}`), Outcome: protocolcommon.OutcomeSuccess}
}
func (e *portableTestEndpoint) Handshake(context.Context, []byte) (endpoint.DispatchResponse, bool, error) {
	return testDispatch("runtime.handshake", "handshake"), true, nil
}
func (e *portableTestEndpoint) Supports(string) bool { return true }
func (e *portableTestEndpoint) Prepare(context.Context, string, []byte) (endpoint.DispatchPlan, *endpoint.DispatchResponse, error) {
	return e.plan, nil, nil
}
func (e *portableTestEndpoint) CancellationResponse(operation, requestID string) (endpoint.DispatchResponse, error) {
	return testDispatch(operation, requestID), nil
}
func (e *portableTestEndpoint) TransportResponse(operation, requestID string) (endpoint.DispatchResponse, error) {
	e.transportFailures++
	return testDispatch(operation, requestID), nil
}

type portableTestPlan struct {
	requirements []endpoint.BlobRequirement
	started      chan struct{}
	block        bool
	executed     bool
}

func (p *portableTestPlan) BlobRequirements() []endpoint.BlobRequirement { return p.requirements }
func (p *portableTestPlan) AdmissionBudget() endpoint.CompileAdmissionBudget {
	var bytes int64
	for _, requirement := range p.requirements {
		if requirement.Ref.Size == "1" {
			bytes++
		}
	}
	return endpoint.CompileAdmissionBudget{RequiredBlobCount: int64(len(p.requirements)), RequiredBlobBytes: bytes}
}
func (p *portableTestPlan) Abort() {}
func (p *portableTestPlan) Execute(context.Context, endpoint.BlobSource, endpoint.BlobSink) (engineprotocol.CompileResponseEnvelope, error) {
	return engineprotocol.CompileResponseEnvelope{}, io.EOF
}
func (p *portableTestPlan) ExecuteDispatch(context.Context, endpoint.BlobSource, endpoint.BlobSink) (endpoint.DispatchResponse, error) {
	p.executed = true
	if p.started != nil {
		close(p.started)
	}
	if p.block {
		select {}
	}
	return testDispatch("runtime.test", "request"), nil
}

type portableHarness struct {
	inputWriter  *io.PipeWriter
	outputReader *io.PipeReader
	encoder      *Encoder
	decoder      *Decoder
	done         chan error
}

func newPortableHarness(endpoint PortableEndpoint) *portableHarness {
	inputReader, inputWriter := io.Pipe()
	outputReader, outputWriter := io.Pipe()
	h := &portableHarness{inputWriter: inputWriter, outputReader: outputReader, encoder: NewEncoder(inputWriter), decoder: NewDecoder(outputReader), done: make(chan error, 1)}
	go func() {
		h.done <- ServePortable(context.Background(), inputReader, outputWriter, endpoint, DefaultSessionLimits())
	}()
	return h
}
func (h *portableHarness) close()            { _ = h.inputWriter.Close(); _ = h.outputReader.Close() }
func (h *portableHarness) write(frame Frame) { _ = h.encoder.WriteFrame(frame) }
func (h *portableHarness) readBundle() []Frame {
	frames := []Frame{}
	for {
		frame, err := h.decoder.ReadFrame()
		if err != nil {
			return frames
		}
		frames = append(frames, frame)
		if frame.Kind == KindBundleEnd {
			return frames
		}
	}
}
func (h *portableHarness) handshake() {
	h.write(Frame{Kind: KindRequestControl, StreamID: 1, Payload: []byte(`{"operation":"runtime.handshake","request_id":"handshake"}`)})
	_ = h.readBundle()
}

func TestServePortableRejectsOversizedFirstChunkBeforeUnsignedSubtraction(t *testing.T) {
	ref := protocolcommon.BlobRef{BlobID: "asset", Digest: "sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa", Lifetime: protocolcommon.BlobLifetimeRequest, MediaType: "application/octet-stream", Size: "1"}
	plan := &portableTestPlan{requirements: []endpoint.BlobRequirement{{Ref: ref, References: 1}}}
	portable := &portableTestEndpoint{plan: plan}
	h := newPortableHarness(portable)
	defer h.close()
	h.handshake()
	h.write(Frame{Kind: KindRequestControl, StreamID: 2, Payload: []byte(`{"operation":"runtime.test","request_id":"request"}`)})
	ready, err := h.decoder.ReadFrame()
	if err != nil || ready.Kind != KindRequestReady {
		t.Fatalf("ready=%v err=%v", ready.Kind, err)
	}
	h.write(Frame{Kind: KindBlobChunk, Flags: FlagFinal, StreamID: 2, Sequence: 1, Name: []byte("asset"), Payload: []byte{1, 2}})
	_ = h.readBundle()
	if portable.transportFailures != 1 || plan.executed {
		t.Fatalf("failures=%d executed=%v", portable.transportFailures, plan.executed)
	}
}

func TestServePortableCancellationDoesNotJoinNonCooperativeWorker(t *testing.T) {
	started := make(chan struct{})
	plan := &portableTestPlan{started: started, block: true}
	h := newPortableHarness(&portableTestEndpoint{plan: plan})
	defer h.close()
	h.handshake()
	h.write(Frame{Kind: KindRequestControl, StreamID: 2, Payload: []byte(`{"operation":"runtime.test","request_id":"request"}`)})
	ready, err := h.decoder.ReadFrame()
	if err != nil || ready.Kind != KindRequestReady {
		t.Fatalf("ready=%v err=%v", ready.Kind, err)
	}
	h.write(Frame{Kind: KindBundleEnd, StreamID: 2, Sequence: 1})
	select {
	case <-started:
	case <-time.After(time.Second):
		t.Fatal("worker did not start")
	}
	begin := time.Now()
	h.write(Frame{Kind: KindCancel, StreamID: 2})
	_ = h.readBundle()
	if elapsed := time.Since(begin); elapsed > 250*time.Millisecond {
		t.Fatalf("cancellation blocked for %v", elapsed)
	}
}

func TestServePortableStalledUploadHonorsRequestDeadline(t *testing.T) {
	ref := protocolcommon.BlobRef{BlobID: "asset", Digest: "sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa", Lifetime: protocolcommon.BlobLifetimeRequest, MediaType: "application/octet-stream", Size: "1"}
	plan := &portableTestPlan{requirements: []endpoint.BlobRequirement{{Ref: ref, References: 1}}}
	h := newPortableHarness(&portableTestEndpoint{plan: plan})
	defer h.close()
	h.handshake()

	deadline := time.Now().Add(75 * time.Millisecond).UTC().Format(time.RFC3339Nano)
	control := fmt.Sprintf(`{"operation":"runtime.test","request_id":"request","deadline_at":%q}`, deadline)
	h.write(Frame{Kind: KindRequestControl, StreamID: 2, Payload: []byte(control)})
	ready, err := h.decoder.ReadFrame()
	if err != nil || ready.Kind != KindRequestReady {
		t.Fatalf("ready=%v err=%v", ready.Kind, err)
	}

	completed := make(chan []Frame, 1)
	go func() { completed <- h.readBundle() }()
	select {
	case frames := <-completed:
		if len(frames) == 0 || frames[len(frames)-1].Kind != KindBundleEnd {
			t.Fatalf("deadline response frames=%v", frames)
		}
	case <-time.After(time.Second):
		t.Fatal("stalled upload ignored request deadline")
	}
}

func TestServePortableIgnoresLateTerminalCancellation(t *testing.T) {
	plan := &portableTestPlan{}
	h := newPortableHarness(&portableTestEndpoint{plan: plan})
	defer h.close()
	h.handshake()

	h.write(Frame{Kind: KindRequestControl, StreamID: 2, Payload: []byte(`{"operation":"runtime.test","request_id":"request"}`)})
	if ready, err := h.decoder.ReadFrame(); err != nil || ready.Kind != KindRequestReady {
		t.Fatalf("first ready=%v err=%v", ready.Kind, err)
	}
	h.write(Frame{Kind: KindBundleEnd, StreamID: 2, Sequence: 1})
	if frames := h.readBundle(); len(frames) == 0 || frames[len(frames)-1].Kind != KindBundleEnd {
		t.Fatalf("first response=%v", frames)
	}

	h.write(Frame{Kind: KindCancel, StreamID: 2})
	h.write(Frame{Kind: KindRequestControl, StreamID: 3, Payload: []byte(`{"operation":"runtime.test","request_id":"request-next"}`)})
	readyResult := make(chan readResult, 1)
	go func() {
		frame, err := h.decoder.ReadFrame()
		readyResult <- readResult{frame: frame, err: err}
	}()
	select {
	case result := <-readyResult:
		if result.err != nil || result.frame.Kind != KindRequestReady || result.frame.StreamID != 3 {
			t.Fatalf("next ready=%v err=%v", result.frame, result.err)
		}
	case <-time.After(time.Second):
		t.Fatal("late cancellation poisoned the next request")
	}
	h.write(Frame{Kind: KindBundleEnd, StreamID: 3, Sequence: 1})
	if frames := h.readBundle(); len(frames) == 0 || frames[len(frames)-1].Kind != KindBundleEnd {
		t.Fatalf("next response=%v", frames)
	}
}
