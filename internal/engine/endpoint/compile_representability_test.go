// SPDX-License-Identifier: LicenseRef-LayerDraw-1.0

package endpoint

import (
	"bytes"
	"context"
	"encoding/json"
	"math"
	"reflect"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/dencyuinc/layerdraw/gen/go/engineprotocol"
	"github.com/dencyuinc/layerdraw/gen/go/protocolcommon"
	"github.com/dencyuinc/layerdraw/gen/go/semantic"
	"github.com/dencyuinc/layerdraw/internal/engine"
	"github.com/dencyuinc/layerdraw/internal/engine/internal/compiler/resolve"
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
	driver := newOversizedRejectionDriver(100_000)
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

func newOversizedRejectionDriver(count int) *oversizedRejectionDriver {
	diagnostics := make([]engine.Diagnostic, count)
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

func TestCompileResponseMappingMemoryIsBoundedAsDiagnosticsGrow(t *testing.T) {
	measure := func(count int) uint64 {
		t.Helper()
		value := []byte("project p \"P\" {}\n")
		request := compileRequest(value)
		driver := newOversizedRejectionDriver(count)
		dispatcher := newCompileDispatcher(driver)
		runtime.GC()
		var before, after runtime.MemStats
		runtime.ReadMemStats(&before)
		response, err := dispatcher.DispatchCompile(context.Background(), compileContext(t), request, sourceFor(request.Payload.ProjectSourceTree[0].Blob, value), &memoryBlobSink{})
		runtime.ReadMemStats(&after)
		if err != nil {
			t.Fatal(err)
		}
		assertControlOutputFailure(t, response, "control_output_bytes")
		runtime.KeepAlive(driver)
		return after.TotalAlloc - before.TotalAlloc
	}

	baseline := measure(100_000)
	stress := measure(1_000_000)
	t.Logf("endpoint allocations: diagnostics=100000 bytes=%d diagnostics=1000000 bytes=%d", baseline, stress)
	if stress > baseline+(4<<20) {
		t.Fatalf("endpoint allocations scale with rejected diagnostic count: baseline=%d stress=%d", baseline, stress)
	}
	if stress > 48<<20 {
		t.Fatalf("endpoint mapping exceeded bounded allocation budget: %d", stress)
	}
}

type cancellationMappingDriver struct {
	canonical engineCompileDriver
	snapshot  engine.Snapshot
	cancel    context.CancelFunc
}

func (driver cancellationMappingDriver) Describe() engine.Descriptor {
	return driver.canonical.Describe()
}

func (driver cancellationMappingDriver) CompileSnapshot(context.Context, engine.CompileInput) (engine.Snapshot, error) {
	go func() {
		time.Sleep(time.Millisecond)
		driver.cancel()
	}()
	return driver.snapshot, nil
}

func TestCancellationDuringDiagnosticMappingPrecedesInvariantFallback(t *testing.T) {
	const diagnosticCount = 40_000
	diagnostics := make([]engine.Diagnostic, diagnosticCount)
	for index := range diagnostics {
		diagnostics[index] = engine.Diagnostic{Arguments: map[string]string{}, Code: "LDL1001", MessageKey: "syntax.unexpected_token", Severity: "error"}
	}
	diagnostics[len(diagnostics)-1].Range = &resolve.SourceRange{StartByte: 2, EndByte: 1}
	value := []byte("project p \"P\" {}\n")
	request := compileRequest(value)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	driver := cancellationMappingDriver{
		canonical: engineCompileDriver{compiler: engine.New(engine.BuildInfo{})},
		snapshot:  engine.Snapshot{CompileOutput: engine.CompileOutput{Diagnostics: diagnostics}},
		cancel:    cancel,
	}
	response, err := newCompileDispatcher(driver).DispatchCompile(ctx, compileContext(t), request, sourceFor(request.Payload.ProjectSourceTree[0].Blob, value), &memoryBlobSink{})
	if err != nil {
		t.Fatal(err)
	}
	if response.Outcome != protocolcommon.OutcomeCancelled || response.Failure == nil || response.Failure.Code != FailureCompileCancelled || ctx.Err() == nil {
		t.Fatalf("cancellation lost to mapping fallback: response=%+v context=%v", response, ctx.Err())
	}
}

func TestCompileWireMeasurementMatchesWholeMarshalWithoutAllocatingIt(t *testing.T) {
	leaf := "<tag>&value\u2028"
	nested := semantic.DiagnosticArgumentValue{Kind: semantic.DiagnosticArgumentKindString, StringValue: &leaf}
	for range 8 {
		array := []semantic.DiagnosticArgumentValue{nested}
		nested = semantic.DiagnosticArgumentValue{Kind: semantic.DiagnosticArgumentKindArray, ArrayValue: &array}
	}
	details := protocolcommon.JsonObject{"html": {Kind: protocolcommon.JsonValueKindString, String: "<safe>&"}}
	response := compileRejectedEnvelope("wire-measure", protocolcommon.ReleaseVersion(engine.DevelopmentVersion), []semantic.Diagnostic{validDiagnostic(map[string]semantic.DiagnosticArgumentValue{"nested": nested})})
	response.Extensions = (*protocolcommon.Extensions)(&details)
	encoded, err := json.Marshal(response)
	if err != nil {
		t.Fatal(err)
	}
	stats, err := measureCompileWireJSON(response)
	if err != nil {
		t.Fatal(err)
	}
	if stats.bytes != int64(len(encoded)) || stats.depth != measuredJSONDepth(encoded) {
		t.Fatalf("wire measurement bytes=%d/%d depth=%d/%d", stats.bytes, len(encoded), stats.depth, measuredJSONDepth(encoded))
	}
}

func measuredJSONDepth(encoded []byte) int64 {
	var depth, maximum int64
	inString, escaped := false, false
	for _, value := range encoded {
		if inString {
			if escaped {
				escaped = false
			} else if value == '\\' {
				escaped = true
			} else if value == '"' {
				inString = false
			}
			continue
		}
		switch value {
		case '"':
			inString = true
		case '{', '[':
			depth++
			maximum = max(maximum, depth)
		case '}', ']':
			depth--
		}
	}
	return maximum
}

func TestCompileWireMeasurementCoversGeneratedJSONKindsAndErrors(t *testing.T) {
	type wireShape struct {
		Any      any            `json:"any"`
		Bytes    []byte         `json:"bytes"`
		Optional *string        `json:"optional,omitempty"`
		Strings  map[string]any `json:"strings"`
	}
	values := []any{
		nil,
		true,
		false,
		int64(-12),
		uint64(12),
		float32(1.25),
		float64(1.25),
		"\x00\b\f\n\r\t\\\"<>&\u2028\u2029日本語\xff",
		[]string(nil),
		[]string{"a", "b"},
		[2]string{"a", "b"},
		map[string]string(nil),
		map[string]string{"a": "b", "c": "d"},
		wireShape{Any: []any{"value", true}, Bytes: []byte{0, 1, 2}, Strings: map[string]any{"array": []string{}, "nil": nil}},
		protocolcommon.JsonValue{Kind: protocolcommon.JsonValueKindNull},
		protocolcommon.JsonValue{Kind: protocolcommon.JsonValueKindBoolean, Boolean: true},
		protocolcommon.JsonValue{Kind: protocolcommon.JsonValueKindBoolean},
		protocolcommon.JsonValue{Kind: protocolcommon.JsonValueKindString, String: "<value>"},
		protocolcommon.JsonValue{Kind: protocolcommon.JsonValueKindArray, Array: []protocolcommon.JsonValue{{Kind: protocolcommon.JsonValueKindString, String: "item"}}},
		protocolcommon.JsonValue{Kind: protocolcommon.JsonValueKindObject, Object: map[string]protocolcommon.JsonValue{"key": {Kind: protocolcommon.JsonValueKindNull}}},
	}
	for _, value := range values {
		encoded, err := json.Marshal(value)
		if err != nil {
			t.Fatalf("marshal %T: %v", value, err)
		}
		stats, err := measureCompileWireJSON(value)
		if err != nil {
			t.Fatalf("measure %T: %v", value, err)
		}
		if stats.bytes != int64(len(encoded)) || stats.depth != measuredJSONDepth(encoded) {
			t.Fatalf("measure %T bytes=%d/%d depth=%d/%d", value, stats.bytes, len(encoded), stats.depth, measuredJSONDepth(encoded))
		}
	}

	var dynamic any = "dynamic"
	if _, err := measureCompileWireValue(reflect.ValueOf(&dynamic).Elem()); err != nil {
		t.Fatal(err)
	}
	var pointer *string
	if stats, err := measureCompileWireJSON(pointer); err != nil || stats.bytes != 4 {
		t.Fatalf("nil pointer stats=%+v err=%v", stats, err)
	}
	for _, value := range []any{math.Inf(1), math.NaN(), map[int]string{1: "value"}, make(chan int)} {
		if _, err := measureCompileWireJSON(value); err == nil {
			t.Fatalf("unsupported %T was measured", value)
		}
	}
	if _, err := measureCompileWireJSON(protocolcommon.JsonValue{Kind: "invalid"}); err == nil {
		t.Fatal("invalid JsonValue was measured")
	}
	for _, value := range []protocolcommon.JsonValue{
		{Kind: protocolcommon.JsonValueKindArray, Array: []protocolcommon.JsonValue{{Kind: "invalid"}}},
		{Kind: protocolcommon.JsonValueKindObject, Object: map[string]protocolcommon.JsonValue{"invalid": {Kind: "invalid"}}},
	} {
		if _, err := measureCompileWireJSON(value); err == nil {
			t.Fatal("nested invalid JsonValue was measured")
		}
	}
	for _, value := range []any{[]chan int{make(chan int)}, map[string]chan int{"channel": make(chan int)}} {
		if _, err := measureCompileWireJSON(value); err == nil {
			t.Fatalf("nested unsupported %T was measured", value)
		}
	}

	budget := newCompileMappingBudget(1)
	if err := budget.claim("value"); err == nil || err.Error() == "" {
		t.Fatalf("budget exhaustion was not reported: %v", err)
	}
	if err := (*compileMappingBudget)(nil).claim("ignored"); err != nil {
		t.Fatal(err)
	}
	if err := newCompileMappingBudget(math.MaxInt64).claim(make(chan int)); err == nil {
		t.Fatal("budget accepted an unsupported value")
	}
	if addWireBytes(math.MaxInt64, 1) != math.MaxInt64 || addWireDepth(math.MaxInt64, 1) != math.MaxInt64 {
		t.Fatal("wire accounting did not saturate")
	}
	// The second call covers the cached field metadata path.
	if _, err := measureCompileWireJSON(wireShape{}); err != nil {
		t.Fatal(err)
	}
	type embedded struct{ Value string }
	type unusualShape struct {
		embedded
		hidden  string
		Ignored string `json:"-"`
		Default string
		Many    string `json:"many,omitempty,string"`
	}
	if fields := cachedCompileWireFields(reflect.TypeFor[unusualShape]()); len(fields) != 2 {
		t.Fatalf("unexpected cached field projection: %+v", fields)
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

type oversizedSuccessDriver struct {
	canonical engineCompileDriver
}

func (driver oversizedSuccessDriver) Describe() engine.Descriptor { return driver.canonical.Describe() }

func (driver oversizedSuccessDriver) CompileSnapshot(ctx context.Context, input engine.CompileInput) (engine.Snapshot, error) {
	snapshot, err := driver.canonical.CompileSnapshot(ctx, input)
	if err != nil {
		return engine.Snapshot{}, err
	}
	snapshot.StableAddresses = make([]string, 1_000_000)
	for index := range snapshot.StableAddresses {
		snapshot.StableAddresses[index] = "ldl:project:p:entity:e"
	}
	return snapshot, nil
}

func TestSuccessMappingUsesTheSameControlBudgetBeforePublication(t *testing.T) {
	value := []byte("project p \"P\" {}\n")
	request := compileRequest(value)
	sink := &memoryBlobSink{}
	driver := oversizedSuccessDriver{canonical: engineCompileDriver{compiler: engine.New(engine.BuildInfo{})}}
	response, err := newCompileDispatcher(driver).DispatchCompile(context.Background(), compileContext(t), request, sourceFor(request.Payload.ProjectSourceTree[0].Blob, value), sink)
	if err != nil {
		t.Fatal(err)
	}
	assertControlOutputFailure(t, response, "control_output_bytes")
	if sink.calls != 0 || len(sink.blobs) != 0 {
		t.Fatalf("oversized success was published: calls=%d blobs=%d", sink.calls, len(sink.blobs))
	}
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
	maxRequest := request
	maxRequest.RequestID = strings.Repeat("r", 128)
	if _, err := engineprotocol.EncodeCompileRequestEnvelope(maxRequest); err != nil {
		t.Fatalf("could not construct maximum-length valid request ID: err=%v", err)
	}
	overlongRequest := maxRequest
	overlongRequest.RequestID += "r"
	if _, err := engineprotocol.EncodeCompileRequestEnvelope(overlongRequest); err == nil {
		t.Fatal("generated request schema accepted an overlong request ID")
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
