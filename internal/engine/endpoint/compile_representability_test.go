// SPDX-License-Identifier: LicenseRef-LayerDraw-1.0

package endpoint

import (
	"bytes"
	"context"
	"strconv"
	"strings"
	"sync"
	"testing"

	"github.com/dencyuinc/layerdraw/gen/go/engineprotocol"
	"github.com/dencyuinc/layerdraw/gen/go/protocolcommon"
	"github.com/dencyuinc/layerdraw/gen/go/semantic"
	"github.com/dencyuinc/layerdraw/internal/engine"
)

func TestDispatchCompileLargeRejectionUsesStableControlFailureAndRecovers(t *testing.T) {
	value := []byte("project p \"P\" {}\n")
	request := compileRequest(value)
	releases := 0
	source := &memoryBlobSource{definitions: []BlobDefinition{{
		BlobID: request.Payload.ProjectSourceTree[0].Blob.BlobID,
		Owned: &OwnedBlob{Bytes: value, Release: func() {
			releases++
		}},
	}}}
	sink := &memoryBlobSink{}
	driver := newOversizedRejectionDriver()
	dispatcher := newCompileDispatcher(driver)
	negotiated := compileContext(t)

	response, err := dispatcher.DispatchCompile(context.Background(), negotiated, request, source, sink)
	if err != nil {
		t.Fatal(err)
	}
	assertControlOutputFailure(t, response, "control_output_bytes")
	if releases != 1 || source.calls != 1 || sink.calls != 0 || len(sink.blobs) != 0 {
		t.Fatalf("large rejection ownership/publication releases=%d source=%d sink=%d blobs=%d", releases, source.calls, sink.calls, len(sink.blobs))
	}
	if _, err := engineprotocol.EncodeCompileResponseEnvelope(response); err != nil {
		t.Fatalf("control fallback is not generated-encodable: %v", err)
	}

	valid := []byte("project recovered \"Recovered\" {}\n")
	validRequest := compileRequest(valid)
	validRequest.RequestID = "compile-after-control-exhaustion"
	validSink := &memoryBlobSink{}
	recovered, err := dispatcher.DispatchCompile(context.Background(), negotiated, validRequest, sourceFor(validRequest.Payload.ProjectSourceTree[0].Blob, valid), validSink)
	if err != nil {
		t.Fatal(err)
	}
	if recovered.Outcome != protocolcommon.OutcomeSuccess || recovered.Payload == nil || recovered.Failure != nil || validSink.calls != 1 {
		t.Fatalf("subsequent valid compile did not recover: response=%+v sink=%d", recovered, validSink.calls)
	}
	if driver.callCount() != 2 {
		t.Fatalf("compile driver calls=%d, want 2", driver.callCount())
	}
}

type oversizedRejectionDriver struct {
	canonical engineCompileDriver
	mu        sync.Mutex
	calls     int
	rejection engine.Snapshot
}

func newOversizedRejectionDriver() *oversizedRejectionDriver {
	diagnostics := make([]engine.Diagnostic, 100_000)
	for index := range diagnostics {
		diagnostics[index] = engine.Diagnostic{
			Arguments:  map[string]string{},
			Code:       "LDL1001",
			MessageKey: "syntax.unexpected_token",
			Severity:   "error",
		}
	}
	return &oversizedRejectionDriver{
		canonical: engineCompileDriver{compiler: engine.New(engine.BuildInfo{})},
		rejection: engine.Snapshot{CompileOutput: engine.CompileOutput{Diagnostics: diagnostics}},
	}
}

func (driver *oversizedRejectionDriver) Describe() engine.Descriptor {
	return driver.canonical.Describe()
}

func (driver *oversizedRejectionDriver) CompileSnapshot(ctx context.Context, input engine.CompileInput) (engine.Snapshot, error) {
	driver.mu.Lock()
	driver.calls++
	call := driver.calls
	driver.mu.Unlock()
	if call == 1 {
		return driver.rejection, nil
	}
	return driver.canonical.CompileSnapshot(ctx, input)
}

func (driver *oversizedRejectionDriver) callCount() int {
	driver.mu.Lock()
	defer driver.mu.Unlock()
	return driver.calls
}

func TestCompileResponseFallbackClassifiesSuccessControlAndInvariantFailures(t *testing.T) {
	value := []byte("project p \"P\" {}\n")
	request := compileRequest(value)
	dispatcher := NewCompileDispatcher(engine.New(engine.BuildInfo{}))
	negotiated := compileContext(t)
	baseline, err := dispatcher.DispatchCompile(context.Background(), negotiated, request, sourceFor(request.Payload.ProjectSourceTree[0].Blob, value), &memoryBlobSink{})
	if err != nil || baseline.Outcome != protocolcommon.OutcomeSuccess || baseline.Payload == nil {
		t.Fatalf("baseline response=%+v err=%v", baseline, err)
	}

	address := semantic.StableAddress("ldl:project:p:entity:e")
	baseline.Payload.StableAddresses = make([]semantic.StableAddress, 400_000)
	for index := range baseline.Payload.StableAddresses {
		baseline.Payload.StableAddresses[index] = address
	}
	plan := &CompilePlan{requestID: request.RequestID, release: protocolcommon.ReleaseVersion(engine.DevelopmentVersion)}
	first, err := plan.finalizeCompileResponse(context.Background(), baseline)
	if err != nil {
		t.Fatal(err)
	}
	assertControlOutputFailure(t, first, "control_output_bytes")
	second, err := plan.finalizeCompileResponse(context.Background(), baseline)
	if err != nil {
		t.Fatal(err)
	}
	firstBytes, _ := engineprotocol.EncodeCompileResponseEnvelope(first)
	secondBytes, _ := engineprotocol.EncodeCompileResponseEnvelope(second)
	if !bytes.Equal(firstBytes, secondBytes) {
		t.Fatal("control-output fallback is nondeterministic")
	}
	encodedRequest, err := engineprotocol.EncodeCompileRequestEnvelope(request)
	if err != nil {
		t.Fatal(err)
	}
	maxRequest := request
	maxRequest.RequestID += strings.Repeat("r", engineprotocol.MaxWireJSONBytes-len(encodedRequest))
	encodedRequest, err = engineprotocol.EncodeCompileRequestEnvelope(maxRequest)
	if err != nil || len(encodedRequest) != engineprotocol.MaxWireJSONBytes {
		t.Fatalf("could not construct maximum-size valid request: bytes=%d err=%v", len(encodedRequest), err)
	}
	maxCandidate := baseline
	maxCandidate.RequestID = maxRequest.RequestID
	maxRequestPlan := &CompilePlan{requestID: maxRequest.RequestID, release: protocolcommon.ReleaseVersion(engine.DevelopmentVersion)}
	maxRequestFallback, err := maxRequestPlan.finalizeCompileResponse(context.Background(), maxCandidate)
	if err != nil {
		t.Fatalf("valid maximum-size request could not construct a fallback: %v", err)
	}
	assertControlOutputFailure(t, maxRequestFallback, "control_output_bytes")
	if _, err := engineprotocol.EncodeCompileResponseEnvelope(maxRequestFallback); err != nil {
		t.Fatalf("maximum-size valid request fallback is not generated-encodable: %v", err)
	}

	cancelled, err := plan.finalizeCompileResponse(cancelledContext(), baseline)
	if err != nil || cancelled.Outcome != protocolcommon.OutcomeCancelled || cancelled.Failure == nil || cancelled.Failure.Code != FailureCompileCancelled {
		t.Fatalf("cancellation did not precede control exhaustion: response=%+v err=%v", cancelled, err)
	}
	abortedPlan := &CompilePlan{requestID: request.RequestID, release: protocolcommon.ReleaseVersion(engine.DevelopmentVersion)}
	abortedPlan.Abort()
	aborted, err := abortedPlan.finalizeCompileResponse(context.Background(), baseline)
	if err != nil || aborted.Outcome != protocolcommon.OutcomeCancelled || aborted.Failure == nil || aborted.Failure.Code != FailureCompileCancelled {
		t.Fatalf("abort did not precede control exhaustion: response=%+v err=%v", aborted, err)
	}

	invalid := compileRejectedEnvelope(request.RequestID, protocolcommon.ReleaseVersion(engine.DevelopmentVersion), []semantic.Diagnostic{{
		Arguments:       map[string]semantic.DiagnosticArgumentValue{},
		Code:            "invalid",
		MessageKey:      "invalid",
		ProtocolVersion: 1,
		Related:         []semantic.DiagnosticRelated{},
		Severity:        semantic.DiagnosticSeverityError,
	}})
	invariant, err := plan.finalizeCompileResponse(context.Background(), invalid)
	if err != nil {
		t.Fatal(err)
	}
	if invariant.Outcome != protocolcommon.OutcomeFailed || invariant.Failure == nil || invariant.Failure.Code != FailureCompileInvariant || invariant.Failure.Category != protocolcommon.ProtocolFailureCategoryInvariant || invariant.Payload != nil || len(invariant.Diagnostics) != 0 {
		t.Fatalf("invalid mapped response did not select invariant fallback: %+v", invariant)
	}

	leaf := "value"
	nested := semantic.DiagnosticArgumentValue{Kind: semantic.DiagnosticArgumentKindString, StringValue: &leaf}
	for range engineprotocol.MaxWireJSONDepth {
		array := []semantic.DiagnosticArgumentValue{nested}
		nested = semantic.DiagnosticArgumentValue{Kind: semantic.DiagnosticArgumentKindArray, ArrayValue: &array}
	}
	deep := compileRejectedEnvelope(request.RequestID, protocolcommon.ReleaseVersion(engine.DevelopmentVersion), []semantic.Diagnostic{validDiagnostic(map[string]semantic.DiagnosticArgumentValue{"nested": nested})})
	depthFailure, err := plan.finalizeCompileResponse(context.Background(), deep)
	if err != nil {
		t.Fatal(err)
	}
	assertControlOutputFailure(t, depthFailure, "control_output_depth")

	corrupt := &CompilePlan{requestID: request.RequestID, release: ""}
	if response, err := corrupt.finalizeCompileResponse(context.Background(), invalid); err == nil || response.Outcome != "" || response.Failure != nil || response.Payload != nil || response.Diagnostics != nil {
		t.Fatalf("impossible fallback corruption did not remain a caller error: response=%+v err=%v", response, err)
	}
}

func TestCompileResponseFallbackConcurrentDeterminism(t *testing.T) {
	invalid := compileRejectedEnvelope("concurrent-fallback", protocolcommon.ReleaseVersion(engine.DevelopmentVersion), []semantic.Diagnostic{{
		Arguments:       map[string]semantic.DiagnosticArgumentValue{},
		Code:            "invalid",
		MessageKey:      "invalid",
		ProtocolVersion: 1,
		Related:         []semantic.DiagnosticRelated{},
		Severity:        semantic.DiagnosticSeverityError,
	}})
	plan := &CompilePlan{requestID: "concurrent-fallback", release: protocolcommon.ReleaseVersion(engine.DevelopmentVersion)}
	const count = 32
	encoded := make([][]byte, count)
	var wait sync.WaitGroup
	for index := range count {
		wait.Add(1)
		go func() {
			defer wait.Done()
			response, err := plan.finalizeCompileResponse(context.Background(), invalid)
			if err != nil {
				t.Errorf("fallback: %v", err)
				return
			}
			encoded[index], err = engineprotocol.EncodeCompileResponseEnvelope(response)
			if err != nil {
				t.Errorf("encode: %v", err)
			}
		}()
	}
	wait.Wait()
	for index := 1; index < count; index++ {
		if !bytes.Equal(encoded[0], encoded[index]) {
			t.Fatalf("fallback %d differs", index)
		}
	}
}

func validDiagnostic(arguments map[string]semantic.DiagnosticArgumentValue) semantic.Diagnostic {
	return semantic.Diagnostic{
		Arguments: arguments, Code: "LDL0001", MessageKey: "ldl.test",
		ProtocolVersion: 1, Related: []semantic.DiagnosticRelated{}, Severity: semantic.DiagnosticSeverityError,
	}
}

func assertControlOutputFailure(t *testing.T, response engineprotocol.CompileResponseEnvelope, resource string) {
	t.Helper()
	if response.Outcome != protocolcommon.OutcomeFailed || response.Failure == nil || response.Failure.Code != FailureCompileControlOutput || response.Failure.Category != protocolcommon.ProtocolFailureCategoryResource || response.Payload != nil || len(response.Diagnostics) != 0 {
		t.Fatalf("invalid control-output fallback: %+v", response)
	}
	if response.Failure.SafeDetails == nil {
		t.Fatal("control-output fallback omitted safe accounting")
	}
	details := *response.Failure.SafeDetails
	expectedLimit := engineprotocol.MaxWireJSONBytes
	if resource == "control_output_depth" {
		expectedLimit = engineprotocol.MaxWireJSONDepth
	}
	if details["resource"].String != resource || details["limit"].String != strconv.Itoa(expectedLimit) {
		t.Fatalf("control-output accounting=%+v", details)
	}
	observed, err := strconv.ParseInt(details["observed"].String, 10, 64)
	if err != nil || observed <= int64(expectedLimit) {
		t.Fatalf("invalid observed control limit %q: %v", details["observed"].String, err)
	}
}
