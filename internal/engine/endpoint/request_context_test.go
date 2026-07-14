// SPDX-License-Identifier: LicenseRef-LayerDraw-1.0

package endpoint

import (
	"context"
	"testing"
	"time"

	"github.com/dencyuinc/layerdraw/gen/go/protocolcommon"
	"github.com/dencyuinc/layerdraw/internal/engine"
)

func TestRequestContextOwnsDeadlinePolicy(t *testing.T) {
	t.Parallel()
	deadline := protocolcommon.Rfc3339Time("2026-07-15T12:34:56.123456789Z")
	ctx, cancel, err := RequestContext(context.Background(), &deadline)
	if err != nil {
		t.Fatal(err)
	}
	defer cancel()
	got, ok := ctx.Deadline()
	want := time.Date(2026, 7, 15, 12, 34, 56, 123456789, time.UTC)
	if !ok || !got.Equal(want) {
		t.Fatalf("deadline = %v, %v; want %v", got, ok, want)
	}

	without, stop, err := RequestContext(context.Background(), nil)
	if err != nil {
		t.Fatal(err)
	}
	stop()
	if without.Err() != context.Canceled {
		t.Fatalf("child cancellation = %v", without.Err())
	}
}

func TestEndpointOwnsTransportStateOutcomes(t *testing.T) {
	t.Parallel()
	dispatcher := NewCompileDispatcher(engine.New(engine.BuildInfo{}))
	for reason, code := range map[CompileTransportFailure]string{
		CompileTransportDuplicateRequest: FailureCompileDuplicateRequest,
		CompileTransportResourceLimit:    FailureCompileTransportLimit,
	} {
		response, err := dispatcher.CompileTransportResponse("request", reason)
		if err != nil {
			t.Fatal(err)
		}
		if response.Outcome != protocolcommon.OutcomeFailed || response.Failure == nil || response.Failure.Code != code {
			t.Fatalf("reason %d response = %+v", reason, response)
		}
	}
	response, err := newTestDescriptor(t).RejectNegotiatedHandshake("second")
	if err != nil {
		t.Fatal(err)
	}
	if response.Outcome != protocolcommon.OutcomeRejected || len(response.Diagnostics) != 1 || response.Diagnostics[0].Code != DiagnosticHandshakeState {
		t.Fatalf("handshake state response = %+v", response)
	}
}

func TestRequestContextRejectsInvalidGeneratedTime(t *testing.T) {
	t.Parallel()
	invalid := protocolcommon.Rfc3339Time("2026-07-15T12:34:56+09:00")
	if _, _, err := RequestContext(context.Background(), &invalid); err == nil {
		t.Fatal("invalid non-Z deadline was accepted")
	}
	if _, _, err := RequestContext(nil, nil); err == nil {
		t.Fatal("nil parent was accepted")
	}
}
