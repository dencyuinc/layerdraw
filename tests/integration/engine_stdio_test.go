// SPDX-License-Identifier: LicenseRef-LayerDraw-1.0

package integration_test

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"io"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/dencyuinc/layerdraw/gen/go/engineprotocol"
	"github.com/dencyuinc/layerdraw/gen/go/protocolcommon"
	"github.com/dencyuinc/layerdraw/internal/engine/endpoint"
	transport "github.com/dencyuinc/layerdraw/internal/transport/stdio"
)

func TestEngineStdioSubprocessHandshakeAndProject(t *testing.T) {
	t.Parallel()
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	command := exec.CommandContext(ctx, integrationEngineBinary(t), "stdio")
	stdin, err := command.StdinPipe()
	if err != nil {
		t.Fatal(err)
	}
	stdout, err := command.StdoutPipe()
	if err != nil {
		t.Fatal(err)
	}
	var stderr bytes.Buffer
	command.Stderr = &stderr
	if err := command.Start(); err != nil {
		t.Fatal(err)
	}
	encoder := transport.NewEncoder(stdin)
	decoder := transport.NewDecoder(stdout)

	handshake := integrationHandshake("hs")
	encodedHandshake, err := engineprotocol.EncodeHandshakeRequestEnvelope(handshake)
	if err != nil {
		t.Fatal(err)
	}
	if err := encoder.WriteFrame(transport.Frame{Kind: transport.KindRequestControl, StreamID: 1, Payload: encodedHandshake}); err != nil {
		t.Fatal(err)
	}
	handshakeControl := readFrame(t, decoder)
	if handshakeControl.Kind != transport.KindResponseControl || handshakeControl.StreamID != 1 {
		t.Fatalf("handshake control = %#v", handshakeControl)
	}
	handshakeResponse, err := engineprotocol.DecodeHandshakeResponseEnvelope(handshakeControl.Payload)
	if err != nil || handshakeResponse.Outcome != protocolcommon.OutcomeSuccess {
		t.Fatalf("handshake = %+v, %v", handshakeResponse, err)
	}
	if end := readFrame(t, decoder); end.Kind != transport.KindBundleEnd || end.Sequence != 1 {
		t.Fatalf("handshake end = %#v", end)
	}

	source := []byte("project p \"Project\" {}\n")
	compile := integrationCompile("compile", "source", source)
	encodedCompile, err := engineprotocol.EncodeCompileRequestEnvelope(compile)
	if err != nil {
		t.Fatal(err)
	}
	if err := encoder.WriteFrame(transport.Frame{Kind: transport.KindRequestControl, StreamID: 2, Payload: encodedCompile}); err != nil {
		t.Fatal(err)
	}
	if ready := readFrame(t, decoder); ready.Kind != transport.KindRequestReady || ready.StreamID != 2 {
		t.Fatalf("ready = %#v", ready)
	}
	if err := encoder.WriteFrames([]transport.Frame{
		{Kind: transport.KindBlobChunk, Flags: transport.FlagFinal, StreamID: 2, Sequence: 1, Name: []byte("source"), Payload: source},
		{Kind: transport.KindBundleEnd, StreamID: 2, Sequence: 2},
	}); err != nil {
		t.Fatal(err)
	}
	responseControl := readFrame(t, decoder)
	response, err := engineprotocol.DecodeCompileResponseEnvelope(responseControl.Payload)
	if err != nil || response.Outcome != protocolcommon.OutcomeSuccess || response.RequestID != "compile" {
		t.Fatalf("compile response = %+v, %v", response, err)
	}
	validator := transport.NewBundleValidator()
	for {
		frame := readFrame(t, decoder)
		if err := validator.Accept(frame); err != nil {
			t.Fatal(err)
		}
		if frame.Kind == transport.KindBundleEnd {
			break
		}
	}
	if err := encoder.WriteFrame(transport.Frame{Kind: transport.KindClose}); err != nil {
		t.Fatal(err)
	}
	if err := stdin.Close(); err != nil {
		t.Fatal(err)
	}
	if err := command.Wait(); err != nil {
		t.Fatalf("stdio process: %v; stderr=%q", err, stderr.String())
	}
	if stderr.Len() != 0 {
		t.Fatalf("stderr = %q", stderr.String())
	}
}

func TestEngineStdioControlOutputExhaustionTerminatesOnceAndRecovers(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	command := exec.CommandContext(ctx, integrationEngineBinary(t), "stdio")
	stdin, err := command.StdinPipe()
	if err != nil {
		t.Fatal(err)
	}
	stdout, err := command.StdoutPipe()
	if err != nil {
		t.Fatal(err)
	}
	var stderr bytes.Buffer
	command.Stderr = &stderr
	if err := command.Start(); err != nil {
		t.Fatal(err)
	}
	encoder, decoder := transport.NewEncoder(stdin), transport.NewDecoder(stdout)

	handshakeBytes, err := engineprotocol.EncodeHandshakeRequestEnvelope(integrationHandshake("budget-handshake"))
	if err != nil {
		t.Fatal(err)
	}
	if err := encoder.WriteFrame(transport.Frame{Kind: transport.KindRequestControl, StreamID: 1, Payload: handshakeBytes}); err != nil {
		t.Fatal(err)
	}
	if control := readFrame(t, decoder); control.Kind != transport.KindResponseControl || control.StreamID != 1 {
		t.Fatalf("handshake control = %#v", control)
	}
	if end := readFrame(t, decoder); end.Kind != transport.KindBundleEnd || end.StreamID != 1 || end.Sequence != 1 {
		t.Fatalf("handshake end = %#v", end)
	}

	largeSource := []byte("project p \"P\" {}\n" + strings.Repeat("entity_type e \"E\" {}", 10_000))
	if len(largeSource) != 200_017 {
		t.Fatalf("large source bytes = %d, want 200017", len(largeSource))
	}
	longPath := engineprotocol.CanonicalSourcePath(strings.Repeat("m", 252) + ".ldl")
	if len(longPath) != 256 {
		t.Fatalf("source path bytes = %d, want 256", len(longPath))
	}
	largeCompile := integrationCompile("control-budget", "large-source", largeSource)
	largeCompile.Payload.EntryPath = longPath
	largeCompile.Payload.ProjectSourceTree[0].Path = longPath
	largeBytes, err := engineprotocol.EncodeCompileRequestEnvelope(largeCompile)
	if err != nil {
		t.Fatal(err)
	}
	if err := encoder.WriteFrame(transport.Frame{Kind: transport.KindRequestControl, StreamID: 2, Payload: largeBytes}); err != nil {
		t.Fatal(err)
	}
	if ready := readFrame(t, decoder); ready.Kind != transport.KindRequestReady || ready.StreamID != 2 {
		t.Fatalf("large ready = %#v", ready)
	}
	if err := encoder.WriteFrames([]transport.Frame{
		{Kind: transport.KindBlobChunk, Flags: transport.FlagFinal, StreamID: 2, Sequence: 1, Name: []byte("large-source"), Payload: largeSource},
		{Kind: transport.KindBundleEnd, StreamID: 2, Sequence: 2},
	}); err != nil {
		t.Fatal(err)
	}
	terminal := readFrame(t, decoder)
	if terminal.Kind != transport.KindResponseControl || terminal.StreamID != 2 || uint64(len(terminal.Payload)) > transport.MaxControlPayload {
		t.Fatalf("bounded terminal = kind=%s stream=%d bytes=%d", terminal.Kind, terminal.StreamID, len(terminal.Payload))
	}
	largeResponse, err := engineprotocol.DecodeCompileResponseEnvelope(terminal.Payload)
	if err != nil || largeResponse.RequestID != "control-budget" || largeResponse.Outcome != protocolcommon.OutcomeFailed || largeResponse.Failure == nil || largeResponse.Failure.Code != endpoint.FailureCompileControlOutput || largeResponse.Failure.Category != protocolcommon.ProtocolFailureCategoryResource || largeResponse.Failure.Retryable || largeResponse.Payload != nil || len(largeResponse.Diagnostics) != 0 {
		t.Fatalf("large response = %+v, %v", largeResponse, err)
	}
	if end := readFrame(t, decoder); end.Kind != transport.KindBundleEnd || end.StreamID != 2 || end.Sequence != 1 {
		t.Fatalf("large terminal end = %#v", end)
	}

	recoverySource := []byte("project recovered \"Recovered\" {}\n")
	recovery := integrationCompile("after-control-budget", "recovery-source", recoverySource)
	recoveryBytes, err := engineprotocol.EncodeCompileRequestEnvelope(recovery)
	if err != nil {
		t.Fatal(err)
	}
	if err := encoder.WriteFrame(transport.Frame{Kind: transport.KindRequestControl, StreamID: 3, Payload: recoveryBytes}); err != nil {
		t.Fatal(err)
	}
	if ready := readFrame(t, decoder); ready.Kind != transport.KindRequestReady || ready.StreamID != 3 {
		t.Fatalf("recovery ready = %#v", ready)
	}
	if err := encoder.WriteFrames([]transport.Frame{
		{Kind: transport.KindBlobChunk, Flags: transport.FlagFinal, StreamID: 3, Sequence: 1, Name: []byte("recovery-source"), Payload: recoverySource},
		{Kind: transport.KindBundleEnd, StreamID: 3, Sequence: 2},
	}); err != nil {
		t.Fatal(err)
	}
	recoveryControl := readFrame(t, decoder)
	recoveryResponse, err := engineprotocol.DecodeCompileResponseEnvelope(recoveryControl.Payload)
	if err != nil || recoveryControl.Kind != transport.KindResponseControl || recoveryControl.StreamID != 3 || recoveryResponse.RequestID != "after-control-budget" || recoveryResponse.Outcome != protocolcommon.OutcomeSuccess {
		t.Fatalf("recovery response = %+v, frame=%#v, err=%v", recoveryResponse, recoveryControl, err)
	}
	validator := transport.NewBundleValidator()
	for {
		frame := readFrame(t, decoder)
		if err := validator.Accept(frame); err != nil {
			t.Fatal(err)
		}
		if frame.Kind == transport.KindBundleEnd {
			break
		}
	}

	if err := encoder.WriteFrame(transport.Frame{Kind: transport.KindClose}); err != nil {
		t.Fatal(err)
	}
	if err := stdin.Close(); err != nil {
		t.Fatal(err)
	}
	if err := command.Wait(); err != nil {
		t.Fatalf("stdio recovery process: %v; stderr=%q", err, stderr.String())
	}
	if stderr.Len() != 0 {
		t.Fatalf("stderr = %q", stderr.String())
	}
}

func TestEngineStdioFatalFrameRedactsStderr(t *testing.T) {
	t.Parallel()
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	secret := "do-not-print-this-source"
	command := exec.CommandContext(ctx, integrationEngineBinary(t), "stdio")
	command.Stdin = strings.NewReader("NOPE" + secret)
	var stdout, stderr bytes.Buffer
	command.Stdout, command.Stderr = &stdout, &stderr
	err := command.Run()
	if err == nil {
		t.Fatal("corrupt header exited successfully")
	}
	if stdout.Len() != 0 || strings.Contains(stderr.String(), secret) || strings.Contains(stderr.String(), "goroutine") || stderr.String() != "layerdraw-engine: stdio_framing\n" {
		t.Fatalf("stdout=%q stderr=%q", stdout.String(), stderr.String())
	}
}

func integrationEngineBinary(t *testing.T) string {
	t.Helper()
	binary := filepath.Join(t.TempDir(), "layerdraw-engine")
	command := exec.Command("go", "build", "-trimpath", "-o", binary, "../../cmd/layerdraw-engine")
	if output, err := command.CombinedOutput(); err != nil {
		t.Fatalf("build layerdraw-engine: %v\n%s", err, output)
	}
	return binary
}

func readFrame(t *testing.T, decoder *transport.Decoder) transport.Frame {
	t.Helper()
	frame, err := decoder.ReadFrame()
	if err != nil {
		if err == io.EOF {
			t.Fatal("unexpected protocol EOF")
		}
		t.Fatal(err)
	}
	return frame
}

func integrationHandshake(requestID string) engineprotocol.HandshakeRequestEnvelope {
	return engineprotocol.HandshakeRequestEnvelope{
		Operation: engineprotocol.HandshakeRequestEnvelopeOperationValue,
		Payload: protocolcommon.HandshakeRequest{
			ClientRelease: "1.0.0", OptionalCapabilities: []protocolcommon.CapabilityID{},
			Protocols:            []protocolcommon.ProtocolOffer{{Name: endpoint.ProtocolName, SupportedRange: "1.0..1.0", Versions: []protocolcommon.ProtocolVersionBinding{{Version: endpoint.ProtocolVersion, SchemaDigest: protocolcommon.Digest(engineprotocol.SchemaDigest)}}}},
			RequiredCapabilities: []protocolcommon.CapabilityID{endpoint.OperationCompile},
		},
		Protocol: engineprotocol.EngineProtocolRef{Name: engineprotocol.EngineProtocolRefNameValue, Version: engineprotocol.EngineProtocolRefVersionValue}, RequestID: requestID,
	}
}

func integrationCompile(requestID, blobID string, source []byte) engineprotocol.CompileRequestEnvelope {
	digest := sha256.Sum256(source)
	ref := protocolcommon.BlobRef{BlobID: blobID, Digest: protocolcommon.Digest("sha256:" + hex.EncodeToString(digest[:])), Lifetime: protocolcommon.BlobLifetimeRequest, MediaType: "text/plain; charset=utf-8", Size: protocolcommon.CanonicalUint64(strconv.Itoa(len(source)))}
	return engineprotocol.CompileRequestEnvelope{
		Operation: engineprotocol.CompileRequestEnvelopeOperationValue,
		Payload: engineprotocol.CompileInput{
			EntryPath: "document.ldl", InstalledPackTree: []engineprotocol.SourceFileInput{}, Mode: engineprotocol.CompileModeProject,
			ProjectSourceTree: []engineprotocol.SourceFileInput{{Path: "document.ldl", Blob: ref}}, ReferencedAssets: []engineprotocol.AssetInput{},
			ResolvedDependencies: engineprotocol.ResolvedDependencies{Format: engineprotocol.ResolvedDependenciesFormatValue, FormatVersion: 1, Language: 1, Installs: []engineprotocol.ResolvedPack{}}, ResourceLimits: engineprotocol.ResourceLimits{},
		},
		Protocol: engineprotocol.EngineProtocolRef{Name: engineprotocol.EngineProtocolRefNameValue, Version: engineprotocol.EngineProtocolRefVersionValue}, RequestID: requestID,
	}
}
