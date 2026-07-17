// SPDX-License-Identifier: LicenseRef-LayerDraw-1.0

package wasm

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"slices"
	"strconv"
	"sync"
	"testing"
	"time"

	"github.com/dencyuinc/layerdraw/gen/go/engineprotocol"
	"github.com/dencyuinc/layerdraw/gen/go/protocolcommon"
	"github.com/dencyuinc/layerdraw/internal/engine/endpoint"
)

const testReleaseManifestDigest = "sha256:5555555555555555555555555555555555555555555555555555555555555555"

func newTestSession(t *testing.T) *Session {
	t.Helper()
	authority, err := endpoint.NewCompilerEndpoint(endpoint.CompilerEndpointConfig{
		EngineRelease:         "0.0.0-dev",
		SourceRevision:        "unknown",
		ReleaseManifestDigest: testReleaseManifestDigest,
		EndpointInstanceID:    "wasm-test-endpoint",
		Transports:            []string{TransportID},
		Limits:                BrowserCompilerLimitPolicy(),
	})
	if err != nil {
		t.Fatal(err)
	}
	session, err := NewSession(authority, "generation-1", BrowserTransportLimits())
	if err != nil {
		t.Fatal(err)
	}
	return session
}

func handshakeBytes(t *testing.T, valid bool) []byte {
	t.Helper()
	digest := protocolcommon.Digest(engineprotocol.SchemaDigest)
	if !valid {
		digest = "sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
	}
	request := engineprotocol.HandshakeRequestEnvelope{
		Operation: engineprotocol.HandshakeRequestEnvelopeOperationValue,
		Payload: protocolcommon.HandshakeRequest{
			ClientRelease:        "0.0.0-dev",
			OptionalCapabilities: []protocolcommon.CapabilityID{},
			Protocols: []protocolcommon.ProtocolOffer{{
				Name:           endpoint.ProtocolName,
				SupportedRange: "1.0..1.0",
				Versions: []protocolcommon.ProtocolVersionBinding{{
					Version:      endpoint.ProtocolVersion,
					SchemaDigest: digest,
				}},
			}},
			RequiredCapabilities: []protocolcommon.CapabilityID{endpoint.OperationCompile},
		},
		Protocol: engineprotocol.EngineProtocolRef{
			Name:    engineprotocol.EngineProtocolRefNameValue,
			Version: engineprotocol.EngineProtocolRefVersionValue,
		},
		RequestID: "handshake-1",
	}
	value, err := engineprotocol.EncodeHandshakeRequestEnvelope(request)
	if err != nil {
		t.Fatal(err)
	}
	return value
}

func compileBytes(t *testing.T, source []byte) []byte {
	t.Helper()
	digest := sha256.Sum256(source)
	ref := protocolcommon.BlobRef{
		BlobID:    "source",
		Digest:    protocolcommon.Digest("sha256:" + hex.EncodeToString(digest[:])),
		Lifetime:  protocolcommon.BlobLifetimeRequest,
		MediaType: "text/plain; charset=utf-8",
		Size:      protocolcommon.CanonicalUint64(strconv.Itoa(len(source))),
	}
	request := engineprotocol.CompileRequestEnvelope{
		Operation: engineprotocol.CompileRequestEnvelopeOperationValue,
		Payload: engineprotocol.CompileInput{
			EntryPath:         "main.ldl",
			InstalledPackTree: []engineprotocol.SourceFileInput{},
			Mode:              engineprotocol.CompileModeProject,
			ProjectSourceTree: []engineprotocol.SourceFileInput{{Path: "main.ldl", Blob: ref}},
			ReferencedAssets:  []engineprotocol.AssetInput{},
			ResolvedDependencies: engineprotocol.ResolvedDependencies{
				Format:        engineprotocol.ResolvedDependenciesFormatValue,
				FormatVersion: 1,
				Installs:      []engineprotocol.ResolvedPack{},
				Language:      1,
			},
			ResourceLimits: engineprotocol.ResourceLimits{},
		},
		Protocol: engineprotocol.EngineProtocolRef{
			Name:    engineprotocol.EngineProtocolRefNameValue,
			Version: engineprotocol.EngineProtocolRefVersionValue,
		},
		RequestID: "compile-1",
	}
	value, err := engineprotocol.EncodeCompileRequestEnvelope(request)
	if err != nil {
		t.Fatal(err)
	}
	return value
}

func handshakeBytesWithDeadline(t *testing.T, deadline protocolcommon.Rfc3339Time) []byte {
	t.Helper()
	request, err := engineprotocol.DecodeHandshakeRequestEnvelope(handshakeBytes(t, true))
	if err != nil {
		t.Fatal(err)
	}
	request.DeadlineAt = &deadline
	value, err := engineprotocol.EncodeHandshakeRequestEnvelope(request)
	if err != nil {
		t.Fatal(err)
	}
	return value
}

func compileBytesWithDeadline(t *testing.T, source []byte, deadline protocolcommon.Rfc3339Time) []byte {
	t.Helper()
	request, err := engineprotocol.DecodeCompileRequestEnvelope(compileBytes(t, source))
	if err != nil {
		t.Fatal(err)
	}
	request.DeadlineAt = &deadline
	value, err := engineprotocol.EncodeCompileRequestEnvelope(request)
	if err != nil {
		t.Fatal(err)
	}
	return value
}

type memoryRequestBlobs struct {
	definitions []endpoint.BlobDefinition
	binds       int
	releases    int
	bound       bool
	definition  func(context.Context) ([]endpoint.BlobDefinition, error)
}

func (blobs *memoryRequestBlobs) Count() int { return len(blobs.definitions) }

func (blobs *memoryRequestBlobs) Bind(_ []endpoint.BlobRequirement, _ TransportLimits) *LocalFailure {
	blobs.binds++
	blobs.bound = true
	return nil
}

func (blobs *memoryRequestBlobs) Definitions(ctx context.Context) ([]endpoint.BlobDefinition, error) {
	if !blobs.bound {
		return nil, errors.New("unbound")
	}
	if blobs.definition != nil {
		return blobs.definition(ctx)
	}
	return slices.Clone(blobs.definitions), nil
}

func (blobs *memoryRequestBlobs) Release() { blobs.releases++ }

func sourceBlobs(value []byte) *memoryRequestBlobs {
	return &memoryRequestBlobs{definitions: []endpoint.BlobDefinition{{
		BlobID: "source",
		Owned:  &endpoint.OwnedBlob{Bytes: slices.Clone(value), Release: func() {}},
	}}}
}

func negotiateSession(t *testing.T, session *Session) {
	t.Helper()
	response, failure := session.Dispatch(context.Background(), "generation-1", handshakeBytes(t, true), EmptyRequestBlobs{})
	if failure != nil {
		t.Fatal(failure)
	}
	defer response.Release()
	decoded, err := engineprotocol.DecodeHandshakeResponseEnvelope(response.Control)
	if err != nil || decoded.Outcome != protocolcommon.OutcomeSuccess || decoded.Payload == nil {
		t.Fatalf("handshake response=%+v err=%v", decoded, err)
	}
	if !slices.Equal(decoded.Payload.CapabilityManifest.Transports, []string{TransportID}) || decoded.Payload.CapabilityManifest.Limits.MaxProjectSourceBytes.HardMaximum != "16777216" {
		t.Fatalf("wrong browser manifest: %+v", decoded.Payload.CapabilityManifest)
	}
}

func TestSessionGeneratedHandshakeCompileAndRelease(t *testing.T) {
	t.Parallel()
	session := newTestSession(t)
	negotiateSession(t, session)
	source := []byte("project p \"Project\" {}")
	blobs := sourceBlobs(source)
	response, failure := session.Dispatch(context.Background(), "generation-1", compileBytes(t, source), blobs)
	if failure != nil {
		t.Fatal(failure)
	}
	if blobs.binds != 1 || blobs.releases != 1 {
		t.Fatalf("input lifecycle binds=%d releases=%d", blobs.binds, blobs.releases)
	}
	decoded, err := engineprotocol.DecodeCompileResponseEnvelope(response.Control)
	if err != nil || decoded.Outcome != protocolcommon.OutcomeSuccess || decoded.Payload == nil || len(response.Blobs) < 2 {
		t.Fatalf("compile response=%+v blobs=%d err=%v", decoded, len(response.Blobs), err)
	}
	for index := 1; index < len(response.Blobs); index++ {
		if response.Blobs[index-1].BlobID >= response.Blobs[index].BlobID {
			t.Fatalf("output IDs are not sorted: %+v", response.Blobs)
		}
	}
	response.Release()
	if response.Control != nil || response.Blobs != nil {
		t.Fatal("response bytes were retained after release")
	}
}

func TestSessionHonorsGeneratedRequestDeadlines(t *testing.T) {
	t.Parallel()
	past := protocolcommon.Rfc3339Time(time.Now().UTC().Add(-time.Second).Format(time.RFC3339Nano))

	handshakeSession := newTestSession(t)
	handshakeResponse, failure := handshakeSession.Dispatch(context.Background(), "generation-1", handshakeBytesWithDeadline(t, past), EmptyRequestBlobs{})
	if failure != nil {
		t.Fatal(failure)
	}
	defer handshakeResponse.Release()
	handshake, err := engineprotocol.DecodeHandshakeResponseEnvelope(handshakeResponse.Control)
	if err != nil || handshake.Outcome != protocolcommon.OutcomeCancelled {
		t.Fatalf("expired handshake response=%+v err=%v", handshake, err)
	}

	operationSession := newTestSession(t)
	negotiateSession(t, operationSession)
	source := []byte("project p \"Project\" {}")
	blobs := sourceBlobs(source)
	compileResponse, failure := operationSession.Dispatch(context.Background(), "generation-1", compileBytesWithDeadline(t, source, past), blobs)
	if failure != nil {
		t.Fatal(failure)
	}
	defer compileResponse.Release()
	compile, err := engineprotocol.DecodeCompileResponseEnvelope(compileResponse.Control)
	if err != nil || compile.Outcome != protocolcommon.OutcomeCancelled || blobs.binds != 0 || blobs.releases != 1 {
		t.Fatalf("expired compile response=%+v binds=%d releases=%d err=%v", compile, blobs.binds, blobs.releases, err)
	}
}

func TestSessionUsesGeneratedTerminalResponsesWithoutBlobCopy(t *testing.T) {
	t.Parallel()
	session := newTestSession(t)
	source := []byte("project p \"Project\" {}")
	blobs := sourceBlobs(source)
	response, failure := session.Dispatch(context.Background(), "generation-1", compileBytes(t, source), blobs)
	if failure != nil {
		t.Fatal(failure)
	}
	defer response.Release()
	decoded, err := engineprotocol.DecodeCompileResponseEnvelope(response.Control)
	if err != nil || decoded.Outcome != protocolcommon.OutcomeFailed || decoded.Failure == nil || decoded.Failure.Code != endpoint.FailureCompileUnnegotiated {
		t.Fatalf("unexpected unnegotiated response=%+v err=%v", decoded, err)
	}
	if blobs.binds != 0 || blobs.releases != 1 {
		t.Fatalf("terminal response touched blobs: binds=%d releases=%d", blobs.binds, blobs.releases)
	}
}

func TestSecondHandshakeRejectsAndTerminatesGeneration(t *testing.T) {
	t.Parallel()
	session := newTestSession(t)
	negotiateSession(t, session)
	response, failure := session.Dispatch(context.Background(), "generation-1", handshakeBytes(t, false), EmptyRequestBlobs{})
	if failure != nil {
		t.Fatal(failure)
	}
	decoded, err := engineprotocol.DecodeHandshakeResponseEnvelope(response.Control)
	response.Release()
	if err != nil || decoded.Outcome != protocolcommon.OutcomeRejected || len(decoded.Diagnostics) != 1 || decoded.Diagnostics[0].Code != endpoint.DiagnosticHandshakeInvalidConnectionState {
		t.Fatalf("second handshake response=%+v err=%v", decoded, err)
	}
	source := []byte("project p \"Project\" {}")
	if _, failure = session.Dispatch(context.Background(), "generation-1", compileBytes(t, source), sourceBlobs(source)); failure == nil || failure.Code != FailureDisposed {
		t.Fatalf("terminal generation accepted compile: %+v", failure)
	}
}

func TestRejectedInitialHandshakeTerminatesGeneration(t *testing.T) {
	t.Parallel()
	session := newTestSession(t)
	response, failure := session.Dispatch(context.Background(), "generation-1", handshakeBytes(t, false), EmptyRequestBlobs{})
	if failure != nil {
		t.Fatal(failure)
	}
	decoded, err := engineprotocol.DecodeHandshakeResponseEnvelope(response.Control)
	response.Release()
	if err != nil || decoded.Outcome != protocolcommon.OutcomeRejected {
		t.Fatalf("initial rejection=%+v err=%v", decoded, err)
	}
	if _, failure := session.Dispatch(context.Background(), "generation-1", handshakeBytes(t, true), EmptyRequestBlobs{}); failure == nil || failure.Code != FailureDisposed {
		t.Fatalf("rejected generation allowed retry: %+v", failure)
	}
}

func TestSessionLocalFailureAndDisposeLifecycle(t *testing.T) {
	t.Parallel()
	session := newTestSession(t)
	blobs := &memoryRequestBlobs{}
	if _, failure := session.Dispatch(context.Background(), "stale", []byte("{}"), blobs); failure == nil || failure.Code != FailureStaleGeneration || blobs.releases != 1 {
		t.Fatalf("stale failure=%+v releases=%d", failure, blobs.releases)
	}
	if _, failure := session.Dispatch(context.Background(), "generation-1", []byte("{}"), EmptyRequestBlobs{}); failure == nil || failure.Code != FailureMalformedMessage {
		t.Fatalf("malformed failure=%+v", failure)
	}
	if failure := session.Dispose("stale"); failure == nil || failure.Code != FailureStaleGeneration {
		t.Fatalf("stale dispose=%+v", failure)
	}
	if failure := session.Dispose("generation-1"); failure != nil {
		t.Fatal(failure)
	}
	if failure := session.Dispose("generation-1"); failure != nil {
		t.Fatalf("dispose is not idempotent: %+v", failure)
	}
	if _, failure := session.Dispatch(context.Background(), "generation-1", handshakeBytes(t, true), EmptyRequestBlobs{}); failure == nil || failure.Code != FailureDisposed {
		t.Fatalf("disposed dispatch=%+v", failure)
	}
}

func TestSessionSingleFlightAndConcurrentDispose(t *testing.T) {
	session := newTestSession(t)
	negotiateSession(t, session)
	source := []byte("project p \"Project\" {}")
	started := make(chan struct{})
	blobs := sourceBlobs(source)
	blobs.definition = func(ctx context.Context) ([]endpoint.BlobDefinition, error) {
		close(started)
		<-ctx.Done()
		return nil, ctx.Err()
	}
	var wait sync.WaitGroup
	wait.Add(1)
	var firstFailure *LocalFailure
	go func() {
		defer wait.Done()
		response, failure := session.Dispatch(context.Background(), "generation-1", compileBytes(t, source), blobs)
		response.Release()
		firstFailure = failure
	}()
	<-started
	if _, failure := session.Dispatch(context.Background(), "generation-1", handshakeBytes(t, true), EmptyRequestBlobs{}); failure == nil || failure.Code != FailureMalformedMessage || failure.Retryable {
		t.Fatalf("concurrent dispatch=%+v", failure)
	}
	if failure := session.Dispose("generation-1"); failure != nil {
		t.Fatal(failure)
	}
	wait.Wait()
	if firstFailure == nil || firstFailure.Code != FailureDisposed {
		t.Fatalf("dispose did not suppress the in-flight publication: %+v", firstFailure)
	}
}

func TestAtomicOutputSinkCapsAndPublicationIsolation(t *testing.T) {
	t.Parallel()
	limits := BrowserTransportLimits()
	limits.MaxBuffers = 1
	limits.MaxOutputBlobBytes = 3
	limits.MaxOutputTotalBytes = 3
	limits.MaxResponsePublishBytes = limits.MaxControlBytes + 3
	sink := &atomicOutputSink{limits: limits}
	input := []endpoint.OutputBlob{{Ref: protocolcommon.BlobRef{BlobID: "a"}, Bytes: []byte("abc")}}
	if err := sink.Publish(context.Background(), input); err != nil {
		t.Fatal(err)
	}
	input[0].Bytes[0] = 'z'
	output := sink.Take()
	if string(output[0].Bytes) != "abc" {
		t.Fatalf("sink did not isolate output: %q", output[0].Bytes)
	}

	sink = &atomicOutputSink{limits: limits}
	if err := sink.Publish(context.Background(), []endpoint.OutputBlob{{Ref: protocolcommon.BlobRef{BlobID: "a"}, Bytes: []byte("abcd")}}); !errors.Is(err, errOutputLimit) || sink.blobs != nil {
		t.Fatalf("oversized output was partially published: err=%v blobs=%+v", err, sink.blobs)
	}
	cancelled, cancel := context.WithCancel(context.Background())
	cancel()
	if err := sink.Publish(cancelled, nil); !errors.Is(err, context.Canceled) {
		t.Fatalf("cancelled publish=%v", err)
	}
}

func TestTransportLimitValidationAndAuthority(t *testing.T) {
	t.Parallel()
	limits := BrowserTransportLimits()
	if limits.MaxControlBytes != engineprotocol.MaxWireJSONBytes || limits.MaxControlDepth != engineprotocol.MaxWireJSONDepth {
		t.Fatalf("control authority drift: %+v", limits)
	}
	if err := validateTransportLimits(limits); err != nil {
		t.Fatal(err)
	}
	invalid := limits
	invalid.MaxControlBytes++
	if err := validateTransportLimits(invalid); err == nil {
		t.Fatal("generated control maximum may not be raised")
	}
	invalid = limits
	invalid.MaxInputBlobBytes = invalid.MaxInputTotalBytes + 1
	if err := validateTransportLimits(invalid); err == nil {
		t.Fatal("invalid aggregate profile accepted")
	}
	invalid = limits
	invalid.MaxResponsePublishBytes = invalid.MaxControlBytes
	if err := validateTransportLimits(invalid); err == nil {
		t.Fatal("incomplete publication cap accepted")
	}
	invalid = limits
	invalid.MaxBuffers = 0
	if err := validateTransportLimits(invalid); err == nil {
		t.Fatal("zero transport limit accepted")
	}
	if validOpaqueString("", 128) || validOpaqueString("é", 1) || !validOpaqueString("世代-1", 128) {
		t.Fatal("generation grammar is not closed")
	}
}

func TestEndpointInstanceIdentityIsRuntimeMinted(t *testing.T) {
	t.Parallel()
	first, err := newEndpointInstanceID()
	if err != nil {
		t.Fatal(err)
	}
	second, err := newEndpointInstanceID()
	if err != nil {
		t.Fatal(err)
	}
	if first == second || len(first) != len("wasm-")+32 || first[:len("wasm-")] != "wasm-" || !validOpaqueString(first, 128) {
		t.Fatalf("endpoint identities are not fresh opaque values: first=%q second=%q", first, second)
	}
}

func TestEmptyBlobsSessionConstructionAndAccessors(t *testing.T) {
	t.Parallel()
	empty := EmptyRequestBlobs{}
	if failure := empty.Bind(nil, BrowserTransportLimits()); failure != nil {
		t.Fatal(failure)
	}
	if failure := empty.Bind([]endpoint.BlobRequirement{{}}, BrowserTransportLimits()); failure == nil || failure.Code != FailureTransferFailed {
		t.Fatalf("missing blob failure=%+v", failure)
	}
	definitions, err := empty.Definitions(context.Background())
	if err != nil || definitions == nil || len(definitions) != 0 || empty.Count() != 0 {
		t.Fatalf("empty definitions=%+v err=%v", definitions, err)
	}
	empty.Release()

	limits := BrowserTransportLimits()
	if session, err := NewSession(nil, "generation", limits); err == nil || session != nil {
		t.Fatalf("nil authority accepted: session=%+v err=%v", session, err)
	}
	authority, err := endpoint.NewCompilerEndpoint(endpoint.CompilerEndpointConfig{
		EngineRelease:         "0.0.0-dev",
		SourceRevision:        "unknown",
		ReleaseManifestDigest: testReleaseManifestDigest,
		EndpointInstanceID:    "constructor-test",
		Transports:            []string{TransportID},
		Limits:                BrowserCompilerLimitPolicy(),
	})
	if err != nil {
		t.Fatal(err)
	}
	if session, err := NewSession(authority, string([]byte{0xff}), limits); err == nil || session != nil {
		t.Fatalf("bad generation accepted: session=%+v err=%v", session, err)
	}
	limits.MaxBuffers = 0
	if session, err := NewSession(authority, "generation", limits); err == nil || session != nil {
		t.Fatalf("bad limits accepted: session=%+v err=%v", session, err)
	}
	session := newTestSession(t)
	if session.Generation() != "generation-1" || session.Limits() != BrowserTransportLimits() {
		t.Fatalf("session accessors drifted: generation=%s limits=%+v", session.Generation(), session.Limits())
	}
	var nilSession *Session
	if nilSession.Generation() != "" || nilSession.Limits() != (TransportLimits{}) {
		t.Fatal("nil session accessors are not safe")
	}
	var nilResponse *Response
	nilResponse.Release()
}

func TestSessionPreflightPanicAndTransferFailures(t *testing.T) {
	t.Parallel()
	session := newTestSession(t)
	if failure := session.PreflightGeneration("stale"); failure == nil || failure.Code != FailureStaleGeneration {
		t.Fatalf("stale preflight=%+v", failure)
	}
	if failure := session.PreflightGeneration("generation-1"); failure != nil {
		t.Fatal(failure)
	}
	if _, failure := session.Dispatch(nil, "generation-1", nil, EmptyRequestBlobs{}); failure == nil || failure.Code != FailureMalformedMessage {
		t.Fatalf("nil context failure=%+v", failure)
	}
	if _, failure := session.Dispatch(context.Background(), "generation-1", nil, nil); failure == nil || failure.Code != FailureMalformedMessage {
		t.Fatalf("nil blobs failure=%+v", failure)
	}
	oversized := make([]byte, session.limits.MaxControlBytes+1)
	blobs := &memoryRequestBlobs{}
	if _, failure := session.Dispatch(context.Background(), "generation-1", oversized, blobs); failure == nil || failure.Code != FailureTransferFailed || blobs.releases != 1 {
		t.Fatalf("control cap failure=%+v releases=%d", failure, blobs.releases)
	}
	blobs = sourceBlobs([]byte("x"))
	if _, failure := session.Dispatch(context.Background(), "generation-1", handshakeBytes(t, true), blobs); failure == nil || failure.Code != FailureMalformedMessage || blobs.releases != 1 {
		t.Fatalf("handshake blob failure=%+v releases=%d", failure, blobs.releases)
	}
	panicBlobs := &panicRequestBlobs{}
	if _, failure := session.Dispatch(context.Background(), "generation-1", handshakeBytes(t, true), panicBlobs); failure == nil || failure.Code != FailureCrashed || panicBlobs.releases != 1 {
		t.Fatalf("panic redaction=%+v releases=%d", failure, panicBlobs.releases)
	}
	if failure := session.PreflightGeneration("generation-1"); failure == nil || failure.Code != FailureCrashed {
		t.Fatalf("crashed preflight=%+v", failure)
	}

	var nilSession *Session
	if failure := nilSession.PreflightGeneration("generation"); failure == nil || failure.Code != FailureDisposed {
		t.Fatalf("nil preflight=%+v", failure)
	}
}

type panicRequestBlobs struct {
	releases int
}

func (*panicRequestBlobs) Count() int { panic("secret panic detail") }
func (*panicRequestBlobs) Bind([]endpoint.BlobRequirement, TransportLimits) *LocalFailure {
	return nil
}
func (*panicRequestBlobs) Definitions(context.Context) ([]endpoint.BlobDefinition, error) {
	return nil, nil
}
func (blobs *panicRequestBlobs) Release() { blobs.releases++ }

func TestSessionOutputCapReturnsGeneratedFailureWithoutPublication(t *testing.T) {
	t.Parallel()
	session := newTestSession(t)
	negotiateSession(t, session)
	session.limits.MaxOutputBlobBytes = 1
	session.limits.MaxOutputTotalBytes = 1
	session.limits.MaxResponsePublishBytes = session.limits.MaxControlBytes + 1
	source := []byte("project p \"Project\" {}")
	response, failure := session.Dispatch(context.Background(), "generation-1", compileBytes(t, source), sourceBlobs(source))
	if failure != nil {
		t.Fatal(failure)
	}
	defer response.Release()
	decoded, err := engineprotocol.DecodeCompileResponseEnvelope(response.Control)
	if err != nil || decoded.Outcome != protocolcommon.OutcomeFailed || decoded.Failure == nil || decoded.Failure.Code != endpoint.FailureCompileBlobSink || len(response.Blobs) != 0 {
		t.Fatalf("capped response=%+v blobs=%+v err=%v", decoded, response.Blobs, err)
	}

	sink := &atomicOutputSink{blobs: []ResponseBlob{{BlobID: "x", Bytes: []byte("secret")}}, totalBytes: 6}
	sink.Release()
	if sink.blobs != nil || sink.totalBytes != 0 {
		t.Fatal("sink release retained output")
	}
}
