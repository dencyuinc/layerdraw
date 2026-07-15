// SPDX-License-Identifier: LicenseRef-LayerDraw-1.0

package wasm

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/dencyuinc/layerdraw/gen/go/protocolcommon"
	"github.com/dencyuinc/layerdraw/internal/engine/endpoint"
)

func TestFailureVocabularyIsClosedAndReturnedByValue(t *testing.T) {
	t.Parallel()
	definitions := FailureDefinitions()
	if len(definitions) != len(failureDefinitions) {
		t.Fatalf("failure definitions = %d, want %d", len(definitions), len(failureDefinitions))
	}
	definitions[0].Code = "mutated"
	if fresh := FailureDefinitions(); fresh[0].Code != FailureUnsupported {
		t.Fatalf("caller mutation changed failure authority: %+v", fresh[0])
	}

	deferred := false
	func() {
		defer func() { deferred = recover() != nil }()
		_ = localFailure("unknown")
	}()
	if !deferred {
		t.Fatal("unknown local failure code did not fail closed")
	}
}

func TestSessionDefensiveLifecycleStates(t *testing.T) {
	t.Parallel()
	session := newTestSession(t)
	session.state = sessionDisposed
	if failure := session.PreflightGeneration(session.generation); failure == nil || failure.Code != FailureDisposed {
		t.Fatalf("disposed preflight = %+v", failure)
	}

	session.state = sessionActive
	session.inFlight = true
	if failure := session.PreflightGeneration(session.generation); failure == nil || failure.Code != FailureMalformedMessage {
		t.Fatalf("in-flight preflight = %+v", failure)
	}
	session.inFlight = false

	session.state = sessionCrashed
	if failure := session.begin(session.generation); failure == nil || failure.Code != FailureCrashed {
		t.Fatalf("crashed begin = %+v", failure)
	}
	response := Response{Control: []byte("control"), Blobs: []ResponseBlob{{BlobID: "blob", Bytes: []byte("bytes")}}}
	if published, failure := session.publishableResponse(response); failure == nil || failure.Code != FailureCrashed || published.Control != nil || response.Blobs[0].Bytes != nil {
		t.Fatalf("crashed publication = %+v, failure=%+v", published, failure)
	}

	var nilSession *Session
	if failure := nilSession.Dispose("generation"); failure == nil || failure.Code != FailureDisposed {
		t.Fatalf("nil dispose = %+v", failure)
	}
	done := make(chan struct{})
	close(done)
	session.state = sessionDisposed
	session.inFlightDone = done
	if failure := session.Dispose(session.generation); failure != nil {
		t.Fatalf("repeated dispose = %+v", failure)
	}
}

func TestAtomicOutputSinkRejectsUnpublishableSets(t *testing.T) {
	t.Parallel()
	limits := BrowserTransportLimits()
	countLimited := &atomicOutputSink{limits: limits}
	countLimited.limits.MaxBuffers = 1
	if err := countLimited.Publish(context.Background(), []endpoint.OutputBlob{{}, {}}); !errors.Is(err, errOutputLimit) {
		t.Fatalf("buffer count error = %v", err)
	}

	duplicate := &atomicOutputSink{limits: limits}
	if err := duplicate.Publish(context.Background(), []endpoint.OutputBlob{
		{Ref: protocolcommon.BlobRef{BlobID: "same"}, Bytes: []byte("a")},
		{Ref: protocolcommon.BlobRef{BlobID: "same"}, Bytes: []byte("b")},
	}); !errors.Is(err, errOutputLimit) {
		t.Fatalf("duplicate blob error = %v", err)
	}

	cancelDuringCopy := &atomicOutputSink{limits: limits}
	if err := cancelDuringCopy.Publish(&secondCheckCancelledContext{}, []endpoint.OutputBlob{{
		Ref: protocolcommon.BlobRef{BlobID: "blob"}, Bytes: []byte("value"),
	}}); !errors.Is(err, context.Canceled) {
		t.Fatalf("copy cancellation error = %v", err)
	}
}

type secondCheckCancelledContext struct{ checks int }

func (*secondCheckCancelledContext) Deadline() (time.Time, bool) { return time.Time{}, false }
func (*secondCheckCancelledContext) Done() <-chan struct{}       { return nil }
func (*secondCheckCancelledContext) Value(any) any               { return nil }

func (ctx *secondCheckCancelledContext) Err() error {
	ctx.checks++
	if ctx.checks > 1 {
		return context.Canceled
	}
	return nil
}
