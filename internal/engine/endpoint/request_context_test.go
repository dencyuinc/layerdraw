// SPDX-License-Identifier: LicenseRef-LayerDraw-1.0

package endpoint

import (
	"context"
	"strings"
	"testing"
	"time"
	"unicode/utf8"

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

func TestCompilerDescriptorCompositionAndTransportOutcomeMisuse(t *testing.T) {
	t.Parallel()
	compiler := engine.New(engine.BuildInfo{})
	descriptor, err := NewCompilerDescriptor(compiler, testReleaseDigest, "composed", []string{"stdio"}, DefaultLimitPolicy())
	if err != nil {
		t.Fatal(err)
	}
	if descriptor.EngineRelease() != engine.DevelopmentVersion || descriptor.SourceRevision() != engine.UnknownSourceRevision || descriptor.EndpointInstanceID() != "composed" {
		t.Fatalf("descriptor = %+v", descriptor)
	}
	if _, err := NewCompilerDescriptor(compiler, "invalid", "composed", []string{"stdio"}, DefaultLimitPolicy()); err == nil {
		t.Fatal("invalid composition metadata was accepted")
	}

	var nilDispatcher *CompileDispatcher
	if _, err := nilDispatcher.CompileTransportResponse("request", CompileTransportResourceLimit); err == nil {
		t.Fatal("nil dispatcher was accepted")
	}
	dispatcher := NewCompileDispatcher(compiler)
	if _, err := dispatcher.CompileTransportResponse("", CompileTransportResourceLimit); err == nil {
		t.Fatal("empty request ID was accepted")
	}
	if _, err := dispatcher.CompileTransportResponse("request", CompileTransportFailure(255)); err == nil {
		t.Fatal("unknown transport failure was accepted")
	}
	var nilDescriptor *Descriptor
	if _, err := nilDescriptor.RejectNegotiatedHandshake("request"); err == nil {
		t.Fatal("nil descriptor was accepted")
	}
	if _, err := descriptor.RejectNegotiatedHandshake(""); err == nil {
		t.Fatal("empty handshake request ID was accepted")
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

func TestValidateRequestIDExactBounds(t *testing.T) {
	t.Parallel()
	valid := []string{"a", strings.Repeat("a", MaxRequestIDCodePoints), strings.Repeat("😀", MaxRequestIDCodePoints)}
	for _, requestID := range valid {
		if err := ValidateRequestID(requestID); err != nil {
			t.Fatalf("valid ID bytes=%d runes=%d: %v", len(requestID), utf8.RuneCountInString(requestID), err)
		}
	}
	invalid := []string{"", strings.Repeat("a", MaxRequestIDCodePoints+1), strings.Repeat("😀", MaxRequestIDCodePoints) + "a", string([]byte{0xff})}
	for _, requestID := range invalid {
		if err := ValidateRequestID(requestID); err == nil {
			t.Fatalf("invalid ID bytes=%d was accepted", len(requestID))
		}
	}
}
